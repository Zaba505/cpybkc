// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Zaba505/cpybkc/irpb"
)

// TestRunWritesThePublishedBytes asserts the whole of what this program is for:
// the file on disk is what irpb publishes, byte for byte. Anything this tool did
// to the bytes on the way past — a trailing newline, a re-encode with different
// options — would make the released artifact differ from the set every test in
// irpb checks.
func TestRunWritesThePublishedBytes(t *testing.T) {
	out := filepath.Join(t.TempDir(), "ir.binpb")

	if err := run([]string{"-o", out}); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read %s: %v", out, err)
	}

	want, err := irpb.MarshalFileDescriptorSet()
	if err != nil {
		t.Fatalf("marshal the published set: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("the written artifact is not the published set: %d bytes on disk, %d bytes published", len(got), len(want))
	}
}

// TestRunIsReproducible is the acceptance criterion, checked where the artifact
// is actually produced. irpb already asserts that two encodings agree; this
// asserts that two runs of the program that writes the release asset agree too,
// which is the claim a consumer comparing two downloads is relying on.
func TestRunIsReproducible(t *testing.T) {
	dir := t.TempDir()

	first := filepath.Join(dir, "first", "ir.binpb")
	if err := run([]string{"-o", first}); err != nil {
		t.Fatalf("first run: %v", err)
	}

	second := filepath.Join(dir, "second", "ir.binpb")
	if err := run([]string{"-o", second}); err != nil {
		t.Fatalf("second run: %v", err)
	}

	a := readFile(t, first)
	b := readFile(t, second)

	if len(a) == 0 {
		t.Fatal("the artifact is empty")
	}

	if !bytes.Equal(a, b) {
		t.Fatalf("two runs over the same schema produced different artifacts: %d bytes then %d bytes", len(a), len(b))
	}
}

// TestRunCreatesTheParentDirectory covers the case the pipeline actually hits:
// an output path under a directory that does not exist yet, because the
// container's filesystem has no /out until something makes one.
func TestRunCreatesTheParentDirectory(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out", "nested", "ir.binpb")

	if err := run([]string{"-o", out}); err != nil {
		t.Fatalf("run: %v", err)
	}

	if _, err := os.Stat(out); err != nil {
		t.Fatalf("stat %s: %v", out, err)
	}
}

// TestRunRejectsBadUsage keeps the failure modes failures. A missing -o that
// silently wrote nothing would leave a release job green with no asset to
// upload, which is the one outcome nobody would notice.
func TestRunRejectsBadUsage(t *testing.T) {
	testCases := []struct {
		name string
		args []string
	}{
		{name: "no output path", args: nil},
		{name: "empty output path", args: []string{"-o", ""}},
		{name: "unexpected operand", args: []string{"-o", filepath.Join(t.TempDir(), "ir.binpb"), "extra"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := run(testCase.args); err == nil {
				t.Error("run accepted arguments it should have refused")
			}
		})
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return b
}
