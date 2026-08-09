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

	"github.com/Zaba505/cpybkc/internal/layoutschema"
)

// TestRunPublishesTheSchemaOnDisk is the staleness gate: the artifact a release
// attaches is the file this repository holds to docs/layout/SPEC.md, byte for
// byte. A published schema that had been reformatted, or built from somewhere
// else, would be a second spelling of the contract for an adopter's diagnostic
// and this repository's to disagree about.
func TestRunPublishesTheSchemaOnDisk(t *testing.T) {
	root := repoRoot(t)
	out := filepath.Join(t.TempDir(), "layout-schema.sexpr")

	if err := run([]string{"-schema", layoutschema.SchemaPath(root), "-o", out}); err != nil {
		t.Fatalf("run: %v", err)
	}

	want, err := os.ReadFile(layoutschema.SchemaPath(root))
	if err != nil {
		t.Fatalf("read the published schema: %v", err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read %s: %v", out, err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("the artifact is %d bytes and the schema is %d: the published file is the schema unchanged", len(got), len(want))
	}
}

// TestRunRefusesASchemaThatWillNotLoad is why this is a program rather than an
// upload of a file. A schema with a hole in it parses as S-expressions and
// publishes as happily as a good one, and the hole is invisible until a
// generator falls into it — by which time the artifact has shipped.
func TestRunRefusesASchemaThatWillNotLoad(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "broken.sexpr")
	out := filepath.Join(dir, "layout-schema.sexpr")

	// Parses, and declares a form into a sort that does not list it.
	const broken = "(schema layout 1)\n(sort strategy (symbol single-record-type))\n(form equals (in strategy))\n"

	if err := os.WriteFile(source, []byte(broken), 0o644); err != nil {
		t.Fatalf("write the broken schema: %v", err)
	}

	if err := run([]string{"-schema", source, "-o", out}); err == nil {
		t.Fatal("run published a schema that does not load")
	}

	if _, err := os.Stat(out); err == nil {
		t.Error("run wrote an artifact for a schema it refused")
	}
}

// TestRunRejectsBadUsage keeps the failure modes failures, for the reason
// ir-protos' copy of this test gives: a release job that uploads nothing and
// reports success is the one outcome nobody notices.
func TestRunRejectsBadUsage(t *testing.T) {
	testCases := []struct {
		name string
		args []string
	}{
		{name: "no output path", args: nil},
		{name: "empty output path", args: []string{"-o", ""}},
		{name: "unexpected operand", args: []string{"-o", filepath.Join(t.TempDir(), "a.sexpr"), "extra"}},
		{name: "missing schema", args: []string{"-o", filepath.Join(t.TempDir(), "b.sexpr"), "-schema", filepath.Join(t.TempDir(), "nowhere.sexpr")}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := run(testCase.args); err == nil {
				t.Error("run accepted arguments it should have refused")
			}
		})
	}
}

// repoRoot walks up from the test's working directory to the directory holding
// go.mod, which for this module is the repository root and so the directory
// schema/ sits in.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found above %q", dir)
		}

		dir = parent
	}
}
