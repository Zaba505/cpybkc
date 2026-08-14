// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package scaffold

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// scaffoldBytes is what a write puts somewhere. Its content does not matter to
// any test here; that it arrives whole and only where it was asked for does.
var scaffoldBytes = []byte(";; a scaffold\n(record A (copybook \"a.cpy\" A))\n")

// refused asserts that dest was left exactly as it was found, and that the
// diagnostic names both spellings of it.
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
		t.Errorf("the diagnostic names neither the path as typed nor the absolute path:\n%s", rendered)
	}
}

// leftovers is every entry in dir other than the ones named, which is how a
// temporary a failed write forgot to remove is caught.
func leftovers(t *testing.T, dir string, expected ...string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the destination's directory: %v", err)
	}

	var found []string

	for _, entry := range entries {
		if !contains(expected, entry.Name()) {
			found = append(found, entry.Name())
		}
	}

	return found
}

func contains(names []string, name string) bool {
	for _, each := range names {
		if each == name {
			return true
		}
	}

	return false
}

func TestAFreeDestinationIsWrittenInFull(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dest := filepath.Join(dir, "layout.sexpr")

	if err := Write(dest, io.Discard, scaffoldBytes); err != nil {
		t.Fatalf("writing the scaffold: %v", err)
	}

	written, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading back the scaffold: %v", err)
	}

	if !bytes.Equal(written, scaffoldBytes) {
		t.Errorf("wrote %q, want %q", written, scaffoldBytes)
	}

	// The temporary the bytes went through is gone: it is a second name for
	// the file for as long as the link takes, and no longer.
	if left := leftovers(t, dir, "layout.sexpr"); len(left) > 0 {
		t.Errorf("the write left %v behind", left)
	}
}

func TestAFileAtTheDestinationIsNeverReplaced(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dest := filepath.Join(dir, "layout.sexpr")

	// A layout an adopter has edited: the derived half is recomputable and the
	// discriminators and the sequence are not, so this is the one
	// unrecoverable act the command could perform.
	edited := []byte("(sequence (seq A B))\n")
	if err := os.WriteFile(dest, edited, 0o644); err != nil {
		t.Fatalf("preparing the destination: %v", err)
	}

	refused(t, dest, Write(dest, io.Discard, scaffoldBytes))

	kept, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading back the destination: %v", err)
	}

	if !bytes.Equal(kept, edited) {
		t.Errorf("the destination now holds %q, want what was there", kept)
	}

	if left := leftovers(t, dir, "layout.sexpr"); len(left) > 0 {
		t.Errorf("the refusal left %v behind", left)
	}
}

func TestADirectoryAtTheDestinationIsRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dest := filepath.Join(dir, "layout.sexpr")

	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatalf("preparing the destination: %v", err)
	}

	refused(t, dest, Write(dest, io.Discard, scaffoldBytes))
}

func TestASymbolicLinkIsRefusedAndNeverWrittenThrough(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere.sexpr")
	dest := filepath.Join(dir, "layout.sexpr")

	kept := []byte("(sequence (seq A B))\n")
	if err := os.WriteFile(target, kept, 0o644); err != nil {
		t.Fatalf("preparing the link's target: %v", err)
	}

	if err := os.Symlink(target, dest); err != nil {
		t.Skipf("this filesystem does not do symbolic links: %v", err)
	}

	refused(t, dest, Write(dest, io.Discard, scaffoldBytes))

	through, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading back the link's target: %v", err)
	}

	if !bytes.Equal(through, kept) {
		t.Errorf("the link's target now holds %q, want what was there", through)
	}
}

func TestADanglingSymbolicLinkIsRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dest := filepath.Join(dir, "layout.sexpr")

	// Nothing at the other end, so a Stat would report the destination as
	// free. It is not: something is at the path, and the rule is about the
	// path.
	if err := os.Symlink(filepath.Join(dir, "nothing.sexpr"), dest); err != nil {
		t.Skipf("this filesystem does not do symbolic links: %v", err)
	}

	refused(t, dest, Write(dest, io.Discard, scaffoldBytes))
}

// Not parallel, because it changes the working directory: "-" is never a
// relative path, and the only way to say so is to look at the directory a
// relative path would have landed in.
func TestStandardOutputTakesTheBytesAndNoFileIsMade(t *testing.T) {
	dir := t.TempDir()

	t.Chdir(dir)

	var out bytes.Buffer

	if err := Write(Stdout, &out, scaffoldBytes); err != nil {
		t.Fatalf("writing the scaffold to standard output: %v", err)
	}

	if !bytes.Equal(out.Bytes(), scaffoldBytes) {
		t.Errorf("wrote %q, want %q", out.Bytes(), scaffoldBytes)
	}

	if left := leftovers(t, dir); len(left) > 0 {
		t.Errorf("a file named %v was made for a stream destination", left)
	}
}

func TestAMissingParentDirectoryIsAFaultRatherThanATreeThisMakes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dest := filepath.Join(dir, "nowhere", "layout.sexpr")

	err := Write(dest, io.Discard, scaffoldBytes)
	if err == nil {
		t.Fatal("a destination under a directory that does not exist was written")
	}

	var failed *WriteError
	if !errors.As(err, &failed) {
		t.Errorf("the fault is %v, want a failed write", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "nowhere")); err == nil {
		t.Error("the missing parent directory was created")
	}
}

func TestNamingNoDestinationIsRefused(t *testing.T) {
	t.Parallel()

	if err := Write("", io.Discard, scaffoldBytes); err == nil {
		t.Error("a scaffold with nowhere to go was accepted")
	}
}
