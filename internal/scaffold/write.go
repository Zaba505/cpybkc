// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package scaffold

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Stdout is the `--out` operand asking for the scaffold on standard output
// rather than in a file.
//
// POSIX's Utility Conventions give "-" this meaning as an operand, which is the
// reading `--emit-ir` and the plugin contract's `--descriptor` already take. A
// dash is therefore never a relative path here, and a caller wanting a file of
// that name spells it "./-".
const Stdout = "-"

// scaffoldPerm is the mode a scaffold is created with, before the process umask
// is applied to it: an ordinary text file the adopter is about to open in their
// editor.
const scaffoldPerm = 0o644

// tempAttempts is how many names [createBeside] will try before giving up.
//
// The names are counted rather than drawn at random, so this is not a collision
// bound: it is how many temporaries may be sitting beside one destination before
// a write fails. A loop that never gave up would hang on a directory that cannot
// be written to at all.
const tempAttempts = 64

// Write puts b where dest names: a filesystem path, or [Stdout], in which case
// the bytes go to out and out alone.
//
// # Nothing at dest is ever replaced
//
// Whatever is at the path — a regular file, a directory, a symbolic link,
// including one that dangles — the run fails, nothing is written, and the
// diagnostic names the path as it was typed beside the absolute path cpybkc
// looked at. Nothing is truncated, appended to or written through.
//
// That is the opposite of what [github.com/Zaba505/cpybkc/internal/emit] does
// with a descriptor, and deliberately: a descriptor is derived entirely from its
// inputs, so re-emitting after a layout changed is the whole point of the flag,
// while a layout carries the `discriminate` forms and the `sequence` that no
// copybook holds and nothing recovers. Overwriting one an adopter has edited is
// the single unrecoverable act available to this command, and there is no
// `--force` to permit it: a flag whose only purpose is to permit the
// unrecoverable is one that gets written into a script once and never
// reconsidered, and what it saves is one `rm`.
//
// # In full or not at all
//
// The bytes go to a temporary file beside dest and are linked onto it, so a
// write interrupted by a full disk leaves nothing at the destination rather than
// a prefix of a scaffold that parses as far as it goes. The link is what makes
// the refusal atomic as well as checked: [os.Link] fails rather than following a
// symlink or replacing a file, so a destination that appeared between the check
// and the write is refused exactly as one that was there all along.
//
// A path whose parent directory does not exist is an error rather than a
// directory this creates. The operand is a path somebody typed, where a missing
// parent is far more often a typo than an intention.
func Write(dest string, out io.Writer, b []byte) error {
	if dest == "" {
		return ErrNoDestination
	}

	if dest == Stdout {
		if _, err := out.Write(b); err != nil {
			return &WriteError{Path: dest, Err: err}
		}

		return nil
	}

	return writeFile(dest, b)
}

// writeFile puts b at dest, refusing whatever is already there.
func writeFile(dest string, b []byte) error {
	// Lstat rather than Stat, because a symbolic link is one of the things
	// that must not be written through and Stat would report whatever it
	// points at — or nothing at all, for one that dangles, which is exactly the
	// case the rule names.
	if _, err := os.Lstat(dest); err == nil {
		return occupied(dest)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return &WriteError{Path: dest, Err: err}
	}

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

	if err := write(temp, b); err != nil {
		return &WriteError{Path: dest, Err: err}
	}

	if err := os.Link(name, dest); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return occupied(dest)
		}

		return &WriteError{Path: dest, Err: err}
	}

	return nil
}

// write puts the bytes in the temporary and closes it.
//
// The flush is before the close because a full disk is reported at the flush
// rather than at the write where allocation is delayed: linking an unflushed
// file is how ENOSPC becomes a short scaffold and a successful return.
func write(temp *os.File, b []byte) error {
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
// The absolute path is what cpybkc looked at and the typed one is what the
// adopter can find in the line they wrote; a relative path in a shared tree
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
