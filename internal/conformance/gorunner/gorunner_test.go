// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package gorunner_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Zaba505/cpybkc/internal/conformance"
	"github.com/Zaba505/cpybkc/internal/conformance/gorunner"
)

// TestTheGeneratedCodeDecodesEveryEntry is the corpus, run against this
// repository's own generator.
//
// It is the whole of what a conformance run is: the entry's descriptor goes to
// cpybkc-gen-go, what came back is compiled, the entry's bytes are read with it,
// and the values that came out are held against the values the entry states.
// Nothing here knows what the records are — the entry says, and a generator in
// another language is checked by the same two calls (#66).
//
// A failure names the entry and the section the entry cites, because whoever
// reads it has to decide whether the generator is wrong or the entry is, and
// that decision starts at where the expected answer came from.
func TestTheGeneratedCodeDecodesEveryEntry(t *testing.T) {
	root := repoRoot(t)

	entries, err := conformance.Load(conformance.CorpusPath(root))
	if err != nil {
		t.Fatalf("the conformance corpus: %v", err)
	}

	runner := &gorunner.Runner{
		Root:      root,
		Name:      "go",
		Generator: buildGenerator(t, root),
	}

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			got, err := runner.Run(t.Context(), entry)
			if err != nil {
				t.Fatalf("%v", err)
			}

			if err := conformance.Compare(entry.Values, got); err != nil {
				t.Fatalf("%v", &conformance.MismatchError{Entry: entry.Name, Source: entry.Source, Err: err})
			}
		})
	}
}

// TestAnEntryTheGeneratedCodeDisagreesWith asserts that the run above can fail:
// the same entry, with one item of the first record changed, is reported as a
// disagreement naming that item.
//
// It is here because a harness that compiles, runs and compares is a harness
// with three places to accidentally compare nothing, and a passing corpus says
// the same thing in all three cases. The entry is mutated rather than shipped
// broken, so what is asserted is the mechanism and not a second corpus.
func TestAnEntryTheGeneratedCodeDisagreesWith(t *testing.T) {
	root := repoRoot(t)

	entries, err := conformance.Load(conformance.CorpusPath(root))
	if err != nil {
		t.Fatalf("the conformance corpus: %v", err)
	}

	entry := entries[0]
	if len(entry.Values.Records) == 0 {
		t.Fatalf("%s carries no record to disagree about", entry.Name)
	}

	held, ok := entry.Values.Records[0].Value.(map[string]any)
	if !ok {
		t.Fatalf("%s: the first record is not a group", entry.Name)
	}

	for name, value := range held {
		if _, ok := value.(string); ok {
			held[name] = "not what the file holds"

			break
		}
	}

	runner := &gorunner.Runner{Root: root, Name: "go", Generator: buildGenerator(t, root)}

	got, err := runner.Run(t.Context(), entry)
	if err != nil {
		t.Fatalf("%v", err)
	}

	if err := conformance.Compare(entry.Values, got); err == nil {
		t.Fatal("the comparison passed against values the file does not hold")
	}
}

// buildGenerator builds cpybkc-gen-go from the tree under test.
//
// From the tree and not from PATH: a corpus run is about the generator in this
// commit, and resolving a name would find whichever one the author of the commit
// happened to have installed.
func buildGenerator(t *testing.T, root string) string {
	t.Helper()

	name := "cpybkc-gen-go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	path := filepath.Join(t.TempDir(), name)

	build := exec.Command("go", "build", "-o", path, "./cmd/cpybkc-gen-go")
	build.Dir = root

	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("failed to build the generator: %v\n%s", err, out)
	}

	return path
}

// repoRoot walks up from the test's working directory to the directory holding
// go.mod, which for this module is the repository root and so the directory the
// corpus sits in.
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
