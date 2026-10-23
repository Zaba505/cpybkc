// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package scaffold

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// scaffoldPerm is the mode every file `init` writes is created with, before the
// process umask is applied to it: an ordinary text file the adopter is about to
// open in their editor.
const scaffoldPerm = 0o644

// tempAttempts is how many names [createBeside] will try before giving up.
//
// The names are counted rather than drawn at random, so this is not a collision
// bound: it is how many temporaries may be sitting beside one destination before
// a write fails. A loop that never gave up would hang on a directory that cannot
// be written to at all.
const tempAttempts = 64

// Output is one file a run writes: where, and what.
type Output struct {
	// Path is the destination as it was derived from what was typed — the
	// --out value for the layout, and the directory it names joined with the
	// definitions file's name for each of those. It is what a diagnostic
	// names beside the absolute path cpybkc looked at.
	Path string

	// Bytes are the file's whole contents.
	Bytes []byte
}

// Write puts a run's whole set of files where they belong: every definitions
// file, and then the layout that names them (docs/cli/SPEC.md, "Where the
// scaffold is written, and why nothing is ever overwritten").
//
// # Nothing at any destination is ever replaced
//
// Every destination is checked before anything is written. Whatever is at one —
// a regular file, a directory, a symbolic link, including one that dangles — the
// run fails, nothing is written, and there is a diagnostic per occupied
// destination naming the path as it was derived beside the absolute path cpybkc
// looked at. Nothing is truncated, appended to or written through.
//
// That is the opposite of what [github.com/Zaba505/cpybkc/internal/emit] does
// with a descriptor, and deliberately: a descriptor is derived entirely from its
// inputs, so re-emitting after a layout changed is the whole point of the flag,
// while a layout carries the `discriminate` forms and the `sequence` that no
// copybook holds and nothing recovers. A definitions file is held to the same
// rule although it is recomputable, because it is the one file an adopter whose
// copybook cannot be committed has no copybook to recompute from. There is no
// `--force`: a flag whose only purpose is to permit the unrecoverable is one
// that gets written into a script once and never reconsidered, and what it saves
// is one `rm`.
//
// # The whole set or none of it
//
// Each file goes to a temporary file beside its destination and is then linked
// onto it, the definitions files first and the layout last. [os.Link] fails
// rather than following a symlink or replacing a file, so a destination that
// appeared between the check and the write is refused exactly as one that was
// there all along. A write interrupted by a full disk leaves nothing at that
// destination rather than a prefix that parses as far as it goes.
//
// Any failure, and a cancellation of ctx before the last link, removes every
// file this run linked, so the directory is left as it was found: never
// definitions files without their layout, and never a layout naming a
// definitions file that is not there. The layout is last because it is the file
// that names the others — a directory holding definitions files and no layout
// reads as a run that did not finish, and the removal makes that a moment rather
// than a state.
//
// A path whose parent directory does not exist is an error rather than a
// directory this creates. The operand is a path somebody typed, where a missing
// parent is far more often a typo than an intention.
func Write(ctx context.Context, layout Output, definitions []Output) error {
	return writer{link: os.Link}.write(ctx, layout, definitions)
}

// writer is [Write] with the one step a test has to be able to come between: the
// link that puts a file at its destination.
//
// A destination taken after the check and a cancellation arriving partway
// through the set are both things that happen between two links, and neither
// can be arranged from outside a call without a seam here.
type writer struct {
	link func(oldname, newname string) error
}

// write is [Write].
func (w writer) write(ctx context.Context, layout Output, definitions []Output) error {
	if layout.Path == "" {
		return ErrNoDestination
	}

	set := make([]Output, 0, len(definitions)+1)
	set = append(set, definitions...)
	set = append(set, layout)

	if err := free(set); err != nil {
		return err
	}

	var linked []string

	// Every return below that is not the last one leaves the directory as it
	// was found. Removed in reverse, so that the layout, were it ever reached,
	// goes before the files it names.
	undo := func() {
		for _, path := range slices.Backward(linked) {
			_ = os.Remove(path)
		}
	}

	for _, out := range set {
		// Checked before each link rather than once: a run cancelled partway
		// through the set is a cancelled run, and the files it had already
		// linked are what it has to take back.
		if err := ctx.Err(); err != nil {
			undo()

			return err
		}

		if err := w.writeFile(out.Path, out.Bytes); err != nil {
			undo()

			return err
		}

		linked = append(linked, out.Path)
	}

	return nil
}

// free checks every destination before anything is written, and reports every
// one that is taken rather than the first: an adopter re-running `init` into a
// directory it already wrote has the whole set to move, and learning it a file
// at a time is a run per file.
func free(set []Output) error {
	var faults diag.List

	for _, out := range set {
		// Lstat rather than Stat, because a symbolic link is one of the things
		// that must not be written through and Stat would report whatever it
		// points at — or nothing at all, for one that dangles, which is
		// exactly the case the rule names.
		if _, err := os.Lstat(out.Path); err == nil {
			faults.Fail(occupied(out.Path))
		} else if !errors.Is(err, fs.ErrNotExist) {
			faults.Fail(&WriteError{Path: out.Path, Err: err})
		}
	}

	return faults.Err()
}

// writeFile puts b at dest through a temporary file beside it.
func (w writer) writeFile(dest string, b []byte) error {
	temp, err := createBeside(dest)
	if err != nil {
		return &WriteError{Path: dest, Err: err}
	}

	name := temp.Name()

	// Removed on every return, including the successful one, where the link
	// has already put the bytes at the destination and this drops the second
	// name for them. A process killed outright between the create and the link
	// leaves it behind; the leading dot is what keeps that out of the way of
	// anything reading the directory.
	defer func() { _ = os.Remove(name) }()

	if err := flush(temp, b); err != nil {
		return &WriteError{Path: dest, Err: err}
	}

	if err := w.link(name, dest); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return occupied(dest)
		}

		return &WriteError{Path: dest, Err: err}
	}

	return nil
}

// flush puts the bytes in the temporary and closes it.
//
// The sync is before the close because a full disk is reported at the sync
// rather than at the write where allocation is delayed: linking an unsynced file
// is how ENOSPC becomes a short file and a successful return.
func flush(temp *os.File, b []byte) error {
	if _, err := temp.Write(b); err != nil {
		_ = temp.Close()

		return err
	}

	if err := temp.Sync(); err != nil {
		_ = temp.Close()

		return err
	}

	return temp.Close()
}

// occupied is the refusal, carrying both spellings of the path.
//
// The absolute path is what cpybkc looked at and the derived one is what the
// adopter can find from the line they wrote; a relative path in a shared tree
// sends a reader to the wrong directory without the pair. A path that cannot be
// made absolute is reported as itself rather than failing the failure.
func occupied(dest string) error {
	absolute, err := filepath.Abs(dest)
	if err != nil {
		absolute = dest
	}

	return &DestinationError{Path: dest, Absolute: absolute}
}

// createBeside makes a file next to dest that nothing else holds, at the mode a
// scaffold is created with.
//
// It is [os.CreateTemp] with two differences. CreateTemp fixes the mode at 0600,
// which is not a mode this package is entitled to give somebody's layout; and it
// picks its names with a random number, which would make what a run writes a
// function of something other than its inputs. O_EXCL is what the names are
// counted around: a create that loses a race fails rather than opening what is
// already there, so two runs writing beside one destination cannot share a
// temporary and a name anybody guessed cannot be pre-created for this to write
// through.
func createBeside(dest string) (*os.File, error) {
	dir, base := filepath.Split(dest)

	for attempt := range tempAttempts {
		name := filepath.Join(dir, fmt.Sprintf(".%s.%d.tmp", base, attempt))

		file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, scaffoldPerm)

		switch {
		case err == nil:
			return file, nil
		case errors.Is(err, fs.ErrExist):
			continue
		default:
			return nil, err
		}
	}

	return nil, fmt.Errorf("every one of the %d temporary names beside it is taken, which is usually %d runs "+
		"killed mid-write leaving a .%s.<n>.tmp behind", tempAttempts, tempAttempts, base)
}
