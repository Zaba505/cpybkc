// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package descriptive_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Zaba505/cpybkc/internal/conformance"
	"github.com/Zaba505/cpybkc/internal/conformance/engine"
)

// TestADescriptiveGeneratorIsNotApplicableAndTheRunIsClean is the whole corpus,
// put to a real descriptive adapter through a real process, and declined.
//
// The engine's own tests already assert this against a scripted adapter. What
// this adds is the half nothing else covers: that a generator which is not a
// conformance subject can actually be handed to the framework — built,
// started, spoken to over two pipes — and comes back not applicable. Until
// there was an adapter to say `kind: "descriptive"`, the only descriptive
// generator this repository has, cmd/cpybkc-gen-graph, could not be run at all,
// and "never reported as failing" was true of nothing.
//
// The two shapes asserted against are the ones the story exists to prevent: a
// descriptive generator scored zero out of the whole corpus, and one declining
// every entry in turn. Neither is true, and both read as failures.
func TestADescriptiveGeneratorIsNotApplicableAndTheRunIsClean(t *testing.T) {
	root := repoRoot(t)

	entries, err := conformance.Load(conformance.CorpusPath(root))
	if err != nil {
		t.Fatalf("the conformance corpus: %v", err)
	}

	if len(entries) == 0 {
		t.Fatalf("the conformance corpus holds no entries, and a run that asked about none proves nothing")
	}

	driving := &engine.Engine{
		Door: &engine.Command{
			Path: build(t, root, "./internal/conformance/descriptive/cmd/adapter"),
			Args: []string{"--name", "graph"},
		},
	}

	report, err := driving.Run(t.Context(), entries)
	if err != nil {
		// A run that could not be attempted at all, which is this test's mistake
		// rather than the adapter's behaviour.
		t.Fatalf("the run could not be made: %v", err)
	}

	t.Logf("%s", report)

	if !report.NotApplicable {
		t.Fatalf("the run is not reported as not applicable, and the generator behind it emits a diagram:\n%v", report)
	}

	if report.Failed() {
		t.Errorf("a descriptive generator is reported as failing:\n%v", report)
	}

	if len(report.Results) != 0 {
		t.Errorf("a descriptive generator was scored against %d of the corpus's entries:\n%v",
			len(report.Results), report)
	}

	if passed, mismatched, faulted := report.Counts(); passed != 0 || mismatched != 0 || faulted != 0 {
		t.Errorf("a descriptive generator scored %d passed, %d disagreed and %d could not be asked, out of a corpus "+
			"it was never asked about", passed, mismatched, faulted)
	}

	for _, note := range report.Notes {
		t.Errorf("the run: %s", note)
	}

	if report.Restarts > 0 {
		t.Errorf("%d fresh adapters were started after one broke, and this adapter should not break", report.Restarts)
	}

	if report.Adapter == nil {
		t.Fatalf("the report says nothing about the adapter, and the handshake is where a kind is declared")
	}

	if report.Adapter.Kind != "descriptive" {
		t.Errorf("the report calls the adapter %s, and it declared itself descriptive", report.Adapter)
	}
}

// build builds one program from the tree under test and says where it put it.
func build(t *testing.T, root, pkg string) string {
	t.Helper()

	name := filepath.Base(pkg)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	path := filepath.Join(t.TempDir(), name)

	built := exec.Command("go", "build", "-o", path, pkg)
	built.Dir = root

	if out, err := built.CombinedOutput(); err != nil {
		t.Fatalf("failed to build %s: %v\n%s", pkg, err, out)
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
