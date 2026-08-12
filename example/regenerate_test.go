// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The assertion that the committed example is what its committed inputs
// produce.
//
// Everything under this directory divides in two. The layout, the copybooks it
// names and the manifest are what a caller writes; `ledger/` and
// `cpybkc.gen.json` are what cpybkc writes for them. This test regenerates the
// second half from the first, through the real CLI and the real generator, and
// holds every byte of the result against what is checked in.
//
// It is byte for byte, and it is the whole tree rather than a sample of it,
// because the point of a committed example is that a change to any layer of the
// pipeline — the layout reader, the copybook reader, `resolve`, `assemble`, the
// IR or the emitter — arrives as a diff somebody reviews. A test asserting that
// generation merely *succeeds* would pass on a run that generated the wrong
// thing, which is the failure this whole directory exists to catch.
//
// Regenerate with the two lines README.md gives, and commit the result.
package example

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// The two halves of this directory, by name.
const (
	// manifestName is the project manifest, which is also the argument the
	// CLI is given.
	manifestName = "cpybkc.json"

	// outDir is the directory the manifest's one generator writes into.
	outDir = "ledger"

	// recordName is the record of what was generated, which cpybkc writes
	// beside the manifest and reads on the next run to prune what a layout
	// no longer describes.
	recordName = "cpybkc.gen.json"
)

// TestTheCommittedExampleIsWhatItsInputsGenerate regenerates the example from
// the inputs beside it and requires the result byte for byte.
func TestTheCommittedExampleIsWhatItsInputsGenerate(t *testing.T) {
	// No t.Parallel: this test sets PATH for the process, which is how the
	// generator it just built is the one the CLI finds.

	root := repoRoot(t)
	bin := t.TempDir()

	build(t, root, bin, "./cmd/cpybkc", "cpybkc")
	build(t, root, bin, "./cmd/cpybkc-gen-go", "cpybkc-gen-go")

	project := t.TempDir()
	for _, name := range inputs(t) {
		copyFile(t, name, filepath.Join(project, name))
	}

	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	cmd := exec.Command(filepath.Join(bin, executable("cpybkc")),
		"--manifest", filepath.Join(project, manifestName))

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generating the example: %v\n%s", err, out)
	}

	generated := generatedTree(t, project)
	committed := generatedTree(t, ".")

	for name, want := range committed {
		got, ok := generated[name]
		if !ok {
			t.Errorf("nothing was generated for %s, which the example carries", name)

			continue
		}

		if got != want {
			t.Errorf("the generated %s is not the one checked in\n got:\n%s\nwant:\n%s", name, got, want)
		}
	}

	for name := range generated {
		if _, ok := committed[name]; !ok {
			t.Errorf("%s was generated and the example does not carry it", name)
		}
	}
}

// inputs is what a caller writes: the manifest, the layout and the copybooks
// the layout names.
//
// It is a listing of this directory rather than a list written down, so that a
// copybook added to the example and never copied into the run is a failure
// rather than a file the assertion quietly stops covering.
func inputs(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the example: %v", err)
	}

	var found []string

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if name == manifestName || filepath.Ext(name) == ".sexpr" || filepath.Ext(name) == ".cpy" {
			found = append(found, name)
		}
	}

	slices.Sort(found)

	return found
}

// generatedTree is everything cpybkc writes for this project, by slash-separated
// path relative to dir: the record beside the manifest, and the package the one
// generator emits.
//
// A `_test.go` file is not one. The generated package holds no hand-written
// declaration — one added by hand would fail the test above, which is the right
// answer — but a test binary is not the package, and the round-trip assertions
// have to run inside it because a credit posting's retained slack is unexported.
func generatedTree(t *testing.T, dir string) map[string]string {
	t.Helper()

	found := map[string]string{}

	if body, err := os.ReadFile(filepath.Join(dir, recordName)); err == nil {
		found[recordName] = string(body)
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("reading %s: %v", filepath.Join(dir, recordName), err)
	}

	err := filepath.WalkDir(filepath.Join(dir, outDir), func(path string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case entry.IsDir():
			return nil
		case strings.HasSuffix(entry.Name(), "_test.go"):
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		found[filepath.ToSlash(rel)] = string(body)

		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("walking %s: %v", filepath.Join(dir, outDir), err)
	}

	return found
}

// build builds one of this repository's commands into bin.
//
// The generator is built under the name the CLI looks for on PATH, and the tree
// under test is what both are built from: a run against the generator somebody
// installed months ago would assert nothing about the pull request making it.
func build(t *testing.T, root, bin, pkg, name string) {
	t.Helper()

	cmd := exec.Command("go", "build", "-o", filepath.Join(bin, executable(name)), pkg)
	cmd.Dir = root

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building %s: %v\n%s", pkg, err, out)
	}
}

// executable is a command's file name on this operating system.
func executable(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}

	return name
}

// copyFile copies one input into the directory the run is made in.
//
// The run is made against a copy rather than against this directory because
// generating writes: a run in place would leave the working tree holding output
// that had not been reviewed, and a failure would be indistinguishable from
// somebody having edited the generated code by hand.
func copyFile(t *testing.T, from, to string) {
	t.Helper()

	body, err := os.ReadFile(from)
	if err != nil {
		t.Fatalf("reading %s: %v", from, err)
	}

	if err := os.WriteFile(to, body, 0o644); err != nil {
		t.Fatalf("writing %s: %v", to, err)
	}
}

// repoRoot is the directory holding go.mod, walked up to from this test's own.
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
			t.Fatalf("no go.mod above %s", dir)
		}

		dir = parent
	}
}
