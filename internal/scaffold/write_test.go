// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package scaffold

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// layoutBytes and definitionsBytes are what a write puts somewhere. Their
// content does not matter to any test here; that they arrive whole and only
// where they were asked for does.
var (
	layoutBytes      = []byte(";; a scaffold\n(record A (definitions \"a.cpy.sexpr\" A))\n")
	definitionsBytes = []byte("; a definitions file\n(entry 01 A)\n")
)

// set is a run's files in dir: a layout, and two definitions files beside it.
func set(dir string) (Output, []Output) {
	return Output{Path: filepath.Join(dir, "layout.sexpr"), Bytes: layoutBytes}, []Output{
		{Path: filepath.Join(dir, "a.cpy.sexpr"), Bytes: definitionsBytes},
		{Path: filepath.Join(dir, "b.cpy.sexpr"), Bytes: definitionsBytes},
	}
}

// refused asserts that err is an occupied destination, and that the diagnostic
// names both spellings of dest.
func refused(t *testing.T, dest string, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("an occupied destination was written to")
	}

	var occupied *DestinationError
	if !errors.As(err, &occupied) {
		t.Fatalf("the fault is %v, want an occupied destination", err)
	}

	rendered := diag.Render(err)

	absolute, absErr := filepath.Abs(dest)
	if absErr != nil {
		t.Fatalf("resolving the destination: %v", absErr)
	}

	if !strings.Contains(rendered, dest) || !strings.Contains(rendered, absolute) {
		t.Errorf("the diagnostic names neither the path as derived nor the absolute path:\n%s", rendered)
	}
}

// leftovers is every entry in dir other than the ones named, which is how a
// temporary a failed write forgot to remove, or a file it forgot to take back,
// is caught.
func leftovers(t *testing.T, dir string, expected ...string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the destination's directory: %v", err)
	}

	var found []string

	for _, entry := range entries {
		if !slices.Contains(expected, entry.Name()) {
			found = append(found, entry.Name())
		}
	}

	return found
}

func TestAFreeSetIsWrittenInFull(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	layout, definitions := set(dir)

	if err := Write(t.Context(), layout, definitions); err != nil {
		t.Fatalf("writing the set: %v", err)
	}

	for _, out := range append(definitions, layout) {
		written, err := os.ReadFile(out.Path)
		if err != nil {
			t.Fatalf("reading back %s: %v", out.Path, err)
		}

		if !bytes.Equal(written, out.Bytes) {
			t.Errorf("wrote %q to %s, want %q", written, out.Path, out.Bytes)
		}
	}

	// The temporaries the bytes went through are gone: each is a second name
	// for its file for as long as the link takes, and no longer.
	if left := leftovers(t, dir, "layout.sexpr", "a.cpy.sexpr", "b.cpy.sexpr"); len(left) > 0 {
		t.Errorf("the write left %v behind", left)
	}
}

// The definitions files are linked first and the layout last, because the
// layout is the file that names the others.
func TestTheDefinitionsFilesAreLinkedBeforeTheLayout(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	layout, definitions := set(dir)

	var order []string

	w := writer{link: func(oldname, newname string) error {
		order = append(order, filepath.Base(newname))

		return os.Link(oldname, newname)
	}}

	if err := w.write(t.Context(), layout, definitions); err != nil {
		t.Fatalf("writing the set: %v", err)
	}

	if want := []string{"a.cpy.sexpr", "b.cpy.sexpr", "layout.sexpr"}; !slices.Equal(order, want) {
		t.Errorf("linked %v, want %v", order, want)
	}
}

func TestAFileAtTheLayoutsDestinationIsNeverReplaced(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	layout, definitions := set(dir)

	// A layout an adopter has edited: the derived half is recomputable and the
	// discriminators and the sequence are not, so this is the one
	// unrecoverable act the command could perform.
	edited := []byte("(sequence (seq A B))\n")
	if err := os.WriteFile(layout.Path, edited, 0o644); err != nil {
		t.Fatalf("preparing the destination: %v", err)
	}

	refused(t, layout.Path, Write(t.Context(), layout, definitions))

	kept, err := os.ReadFile(layout.Path)
	if err != nil {
		t.Fatalf("reading back the destination: %v", err)
	}

	if !bytes.Equal(kept, edited) {
		t.Errorf("the destination now holds %q, want what was there", kept)
	}

	// Checked before anything is written, so not even the definitions files
	// the layout would have named were linked.
	if left := leftovers(t, dir, "layout.sexpr"); len(left) > 0 {
		t.Errorf("a refused run left %v behind", left)
	}
}

// An occupied second destination, found by the check: nothing is written, the
// first destination included, and the file that was there is untouched.
func TestAnOccupiedSecondDestinationRefusesTheWholeSet(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	layout, definitions := set(dir)

	there := []byte("; somebody's own transcription\n")
	if err := os.WriteFile(definitions[1].Path, there, 0o644); err != nil {
		t.Fatalf("preparing the destination: %v", err)
	}

	refused(t, definitions[1].Path, Write(t.Context(), layout, definitions))

	if left := leftovers(t, dir, "b.cpy.sexpr"); len(left) > 0 {
		t.Errorf("a refused run left %v behind", left)
	}

	if kept, err := os.ReadFile(definitions[1].Path); err != nil || !bytes.Equal(kept, there) {
		t.Errorf("the occupied destination now holds %q (%v), want what was there", kept, err)
	}
}

// Every occupied destination is reported, not the first: an adopter re-running
// into a directory a run already wrote has the whole set to move.
func TestEveryOccupiedDestinationIsReported(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	layout, definitions := set(dir)

	for _, out := range append(definitions, layout) {
		if err := os.WriteFile(out.Path, nil, 0o644); err != nil {
			t.Fatalf("preparing %s: %v", out.Path, err)
		}
	}

	err := Write(t.Context(), layout, definitions)
	if got := len(diag.Diagnostics(err)); got != 3 {
		t.Errorf("reported %d occupied destinations, want all three:\n%s", got, diag.Render(err))
	}
}

// A second destination taken after the check and before its link — another
// process, or another run — fails at the link rather than being replaced, and
// the file this run had already linked is taken back.
func TestADestinationTakenAfterTheCheckFailsTheSetAndUndoesIt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	layout, definitions := set(dir)
	racer := []byte("; arrived during the run\n")

	w := writer{link: func(oldname, newname string) error {
		if newname == definitions[1].Path {
			if err := os.WriteFile(newname, racer, 0o644); err != nil {
				t.Fatalf("taking the destination: %v", err)
			}
		}

		return os.Link(oldname, newname)
	}}

	refused(t, definitions[1].Path, w.write(t.Context(), layout, definitions))

	if left := leftovers(t, dir, "b.cpy.sexpr"); len(left) > 0 {
		t.Errorf("a failed run left %v behind, and the first definitions file is what it linked", left)
	}

	if kept, err := os.ReadFile(definitions[1].Path); err != nil || !bytes.Equal(kept, racer) {
		t.Errorf("the file that arrived during the run now holds %q (%v), want what it wrote", kept, err)
	}
}

// A run cancelled partway through the set leaves the directory as it was found:
// what it had linked is removed, and nothing after the cancellation is linked.
func TestACancellationPartwayThroughTheSetRemovesWhatWasLinked(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	layout, definitions := set(dir)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	linked := 0

	w := writer{link: func(oldname, newname string) error {
		linked++

		// The signal arrives as the first file lands.
		if linked == 1 {
			cancel()
		}

		return os.Link(oldname, newname)
	}}

	if err := w.write(ctx, layout, definitions); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled run returned %v, want %v", err, context.Canceled)
	}

	if linked != 1 {
		t.Errorf("linked %d files, want the one before the cancellation and none after it", linked)
	}

	if left := leftovers(t, dir); len(left) > 0 {
		t.Errorf("a cancelled run left %v behind", left)
	}
}

func TestADirectoryAtTheDestinationIsRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	layout, definitions := set(dir)

	if err := os.Mkdir(layout.Path, 0o755); err != nil {
		t.Fatalf("preparing the destination: %v", err)
	}

	refused(t, layout.Path, Write(t.Context(), layout, definitions))
}

func TestASymbolicLinkIsRefusedAndNeverWrittenThrough(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	layout, definitions := set(dir)
	target := filepath.Join(dir, "target.sexpr")

	original := []byte("(record KEPT (definitions \"kept.cpy.sexpr\" KEPT))\n")
	if err := os.WriteFile(target, original, 0o644); err != nil {
		t.Fatalf("preparing the link's target: %v", err)
	}

	if err := os.Symlink(target, layout.Path); err != nil {
		t.Fatalf("preparing the link: %v", err)
	}

	refused(t, layout.Path, Write(t.Context(), layout, definitions))

	kept, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading back the link's target: %v", err)
	}

	if !bytes.Equal(kept, original) {
		t.Errorf("the write went through the link: the target now holds %q", kept)
	}
}

// A link that dangles is the case Stat would miss, which is why the check is
// Lstat: there is something at the path even though nothing is at the end of
// it, and writing through it would create a file somewhere nobody named.
func TestADanglingSymbolicLinkIsRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	layout, definitions := set(dir)
	target := filepath.Join(dir, "nowhere.sexpr")

	if err := os.Symlink(target, definitions[0].Path); err != nil {
		t.Fatalf("preparing the link: %v", err)
	}

	refused(t, definitions[0].Path, Write(t.Context(), layout, definitions))

	if _, err := os.Lstat(target); err == nil {
		t.Error("the write created the link's target")
	}
}

func TestAMissingParentDirectoryIsAFaultRatherThanATreeThisMakes(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "absent")
	layout, definitions := set(dir)

	err := Write(t.Context(), layout, definitions)

	var failed *WriteError
	if !errors.As(err, &failed) {
		t.Fatalf("the fault is %v, want a destination that could not be written", err)
	}

	if _, statErr := os.Stat(dir); statErr == nil {
		t.Error("the write created the missing directory")
	}
}

func TestNamingNoDestinationIsRefused(t *testing.T) {
	t.Parallel()

	if err := Write(t.Context(), Output{Bytes: layoutBytes}, nil); !errors.Is(err, ErrNoDestination) {
		t.Errorf("writing to nowhere gave %v, want %v", err, ErrNoDestination)
	}
}
