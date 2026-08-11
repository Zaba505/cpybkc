// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

//go:build unix

// The properties in this file are the ones a POSIX filesystem has and Windows
// does not, so they are asserted where they hold rather than skipped at runtime
// where they do not.
//
// Three of them come from one implementation decision — a descriptor is written
// to a temporary beside its destination and renamed onto it — and each is a
// thing that decision could have got wrong:
//
//   - a rename replaces a name rather than the file behind it, which is what a
//     reader holding the old file open observes and what makes the write atomic;
//   - a rename onto a symlink would replace the link, so the link is resolved
//     first and the file it names is what gets replaced;
//   - the mode of a file created on the way has to be the mode the destination
//     would have carried anyway, umask and all.
//
// On Windows none of the three is observable in the same terms: renaming over a
// file another handle has open fails outright, symlinks need a privilege an
// ordinary build agent does not have, and a mode is synthesized from the
// read-only attribute so 0600 and 0644 are not distinguishable. What that
// platform does instead is stated in internal/emit's own documentation rather
// than asserted here.
package emit_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Zaba505/cpybkc/internal/emit"
)

// TestWriteReplacesAPathRatherThanTruncatingIt is how atomicity is observed from
// outside the package: a reader holding the old file open still sees the old
// bytes after the write, which is true of a rename onto the name and false of a
// truncate-and-write.
//
// It is the property docs/cli/SPEC.md asks for — "a path is written in full or
// not at all, so that nothing partial is ever left where another tool would read
// it" — stated as something a test can fail on. A write that truncated first
// would leave a descriptor that is a prefix of a descriptor for as long as the
// write took, and a consumer reading in that window meets a malformed message
// rather than the failed emission it is.
func TestWriteReplacesAPathRatherThanTruncatingIt(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "ir.binpb")

	seed := bytes.Repeat([]byte{0xff}, 4096)
	if err := os.WriteFile(dest, seed, 0o644); err != nil {
		t.Fatalf("seed %s: %v", dest, err)
	}

	before, err := os.Open(dest)
	if err != nil {
		t.Fatalf("open %s: %v", dest, err)
	}

	defer before.Close()

	if err := emit.Write(dest, io.Discard, descriptor(), emit.FormatBinary); err != nil {
		t.Fatalf("write: %v", err)
	}

	held, err := io.ReadAll(before)
	if err != nil {
		t.Fatalf("read the file that was open across the write: %v", err)
	}

	if !bytes.Equal(held, seed) {
		t.Errorf("the write replaced the contents under a reader that had the file open: %d bytes of %d survived",
			len(held), len(seed))
	}
}

// TestWriteFollowsASymlinkRatherThanReplacingIt keeps a destination that is a
// link meaning what it meant before the write became atomic.
//
// Writing straight to the path wrote through the link, so a project that keeps
// its descriptor as a link into a shared or generated directory kept the link. A
// rename onto the link would replace it with a regular file — silently, since a
// successful emission says nothing — and the next run would write somewhere the
// project no longer points at.
func TestWriteFollowsASymlinkRatherThanReplacingIt(t *testing.T) {
	dir := t.TempDir()

	target := filepath.Join(dir, "real.binpb")
	if err := os.WriteFile(target, []byte("replaced"), 0o644); err != nil {
		t.Fatalf("seed %s: %v", target, err)
	}

	link := filepath.Join(dir, "ir.binpb")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("link %s: %v", link, err)
	}

	if err := emit.Write(link, io.Discard, descriptor(), emit.FormatBinary); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("stat %s: %v", link, err)
	}

	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is no longer a symlink; the write replaced the link rather than the file it names", link)
	}

	want, err := emit.Marshal(descriptor())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("the file the link names is not the encoded descriptor: %d bytes on disk, %d encoded",
			len(got), len(want))
	}
}

// TestWriteGivesTheDescriptorTheModeWritingToItDirectlyWould is the mode this
// package is entitled to give somebody's file, in the only terms that survive a
// change of umask: the same one the file would have carried if the bytes had
// gone straight to it.
//
// Both halves are what passing a mode to [os.WriteFile] meant, and a temporary
// created 0600 and chmod-ed to 0644 afterwards gets both wrong — it publishes a
// world-readable descriptor to somebody running under a restrictive umask, and
// it flattens a mode somebody had set on purpose. Neither is a decision an
// atomic write has any reason to be making.
func TestWriteGivesTheDescriptorTheModeWritingToItDirectlyWould(t *testing.T) {
	t.Run("a destination that was not there", func(t *testing.T) {
		dir := t.TempDir()

		// The reference is written by the call this function replaced, in the
		// same directory in the same process, so whatever umask the run has is
		// applied to both.
		reference := filepath.Join(dir, "reference")
		if err := os.WriteFile(reference, []byte("reference"), 0o644); err != nil {
			t.Fatalf("write %s: %v", reference, err)
		}

		dest := filepath.Join(dir, "ir.binpb")
		if err := emit.Write(dest, io.Discard, descriptor(), emit.FormatBinary); err != nil {
			t.Fatalf("write: %v", err)
		}

		if got, want := perm(t, dest), perm(t, reference); got != want {
			t.Errorf("the descriptor is mode %04o, and writing straight to it would have made it %04o", got, want)
		}
	})

	t.Run("a destination that was already there", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "ir.binpb")

		// A mode nothing would arrive at by accident, and a restrictive one, so
		// that a write which reset it would be widening somebody's file.
		if err := os.WriteFile(dest, []byte("replaced"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", dest, err)
		}

		if err := os.Chmod(dest, 0o600); err != nil {
			t.Fatalf("chmod %s: %v", dest, err)
		}

		if err := emit.Write(dest, io.Discard, descriptor(), emit.FormatBinary); err != nil {
			t.Fatalf("write: %v", err)
		}

		if got := perm(t, dest); got != 0o600 {
			t.Errorf("re-emitting changed the mode of an existing descriptor to %04o, want %04o", got, 0o600)
		}
	})
}

// perm is the permission bits of the file at path.
func perm(t *testing.T, path string) os.FileMode {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}

	return info.Mode().Perm()
}
