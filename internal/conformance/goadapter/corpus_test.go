// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package goadapter_test

import (
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Zaba505/cpybkc/internal/conformance"
	"github.com/Zaba505/cpybkc/internal/conformance/engine"
	"github.com/Zaba505/cpybkc/internal/conformance/goadapter"
	"github.com/Zaba505/cpybkc/internal/emit"
	"github.com/Zaba505/cpybkc/irpb"
)

// TestTheGeneratedCodeReadsAndWritesEveryEntry is the corpus, run against this
// repository's own generator, in both directions — and run the way a stranger's
// generator is run.
//
// Nothing here knows that the adapter on the other end of those two pipes is
// ours. The engine starts a process, speaks docs/adapter/SPEC.md to it, and
// holds what came back against what each entry states; swapping the door's
// executable for an adapter somebody else wrote in another language is the whole
// of what it would take to run that generator against this corpus instead. That
// is the point of driving cpybkc-gen-go through the contract rather than through
// a harness of its own: if the contract cannot carry this generator, it cannot
// carry a third party's either, and this is where that is found out.
//
// It is an ordinary Go test and not a stage of its own, which is what makes it a
// gate on every platform CI runs on: `dagger call ci` runs `go test ./...` over
// this module, so the corpus is inside the one call CI makes rather than beside
// it. A conformance job of its own would be a second gate, and a platform added
// to the matrix would carry the first and not the second.
func TestTheGeneratedCodeReadsAndWritesEveryEntry(t *testing.T) {
	root := repoRoot(t)

	entries, err := conformance.Load(conformance.CorpusPath(root))
	if err != nil {
		t.Fatalf("the conformance corpus: %v", err)
	}

	report := ask(t, root, entries)

	// Every entry is named whatever became of it, because a corpus run whose
	// output shrinks as it goes wrong is one where a missing line and a passing
	// entry look the same.
	for _, result := range report.Results {
		t.Run(result.Entry, func(t *testing.T) {
			if result.Outcome != engine.Passed {
				t.Fatalf("%v", result.Err)
			}
		})
	}

	for _, note := range report.Notes {
		t.Errorf("the run: %s", note)
	}

	if report.Restarts > 0 {
		t.Errorf("%d fresh adapters were started after one broke, and this adapter should not break", report.Restarts)
	}

	if len(report.Results) != len(entries) {
		t.Errorf("the report carries %d results and the corpus holds %d entries", len(report.Results), len(entries))
	}
}

// TestTheAdapterDeclaresTheGeneratorItDrives is what a reader of a result wants
// to know and what nothing else in the conversation carries.
func TestTheAdapterDeclaresTheGeneratorItDrives(t *testing.T) {
	root := repoRoot(t)

	entries, err := conformance.Load(conformance.CorpusPath(root))
	if err != nil {
		t.Fatalf("the conformance corpus: %v", err)
	}

	report := ask(t, root, entries[:1])

	switch {
	case report.Adapter == nil:
		t.Fatalf("the run reports no adapter at all")
	case report.Adapter.Kind != "codec":
		t.Errorf("the adapter declared kind %q, and cpybkc-gen-go emits code that reads a file", report.Adapter.Kind)
	case !report.Adapter.Writes():
		t.Errorf("the adapter declared no writer, so only half of the corpus was asked")
	case report.Door == "":
		t.Errorf("the run reports no door, and a result is only as believable as the door that produced it")
	}
}

// TestAnEntryTheGeneratedCodeDisagreesWith asserts that the run above can fail:
// the same entry, with one item of the first record changed, is reported as a
// disagreement naming that item.
//
// It is here because a harness that generates, compiles, converses and compares
// is a harness with four places to accidentally compare nothing, and a passing
// corpus says the same thing in all four cases. The entry is mutated rather than
// shipped broken, so what is asserted is the mechanism and not a second corpus.
//
// A disagreement and not a fault: the distinction is most of the value of the
// contract, and a mutation that came back FAULT would say the adapter could not
// answer rather than that the answer was wrong.
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

	report := ask(t, root, []*conformance.Entry{entry})

	if len(report.Results) != 1 {
		t.Fatalf("one entry was asked about and the report carries %d results:\n%s", len(report.Results), report)
	}

	if got := report.Results[0]; got.Outcome != engine.Mismatched {
		t.Fatalf("the comparison reported %s against values the file does not hold:\n%s", got.Outcome, report)
	}
}

// TestAnEntryTheGeneratorRefusesCostsOnlyThatEntry is the per-entry half of
// generate: an entry whose descriptor the generator would not accept comes back
// ok: false with a diagnostic, and the adapter stays alive and serves the rest.
//
// The refusal is a real one — a record renamed to something no Go identifier can
// be munged out of, which cmd/cpybkc-gen-go refuses by name — rather than a
// generator faked for the occasion, so what is asserted is the path a broken
// entry actually takes.
//
// A decode of that entry afterwards is refused too, because the engine MUST NOT
// send one and an adapter that answered it anyway would be answering about code
// it does not have.
func TestAnEntryTheGeneratorRefusesCostsOnlyThatEntry(t *testing.T) {
	root := repoRoot(t)

	entries, err := conformance.Load(conformance.CorpusPath(root))
	if err != nil {
		t.Fatalf("the conformance corpus: %v", err)
	}

	good := entries[0]

	refused, ok := proto.Clone(good.Descriptor).(*irpb.Descriptor)
	if !ok {
		t.Fatalf("a cloned descriptor is not a descriptor")
	}

	renamed := false

	for _, node := range refused.GetNodes() {
		if record := node.GetRecord(); record != nil {
			// A name that opens with a digit has no exported Go identifier in
			// it, which the generator reports rather than working around.
			record.Names = &irpb.Names{Original: "9-NO-IDENTIFIER"}
			renamed = true

			break
		}
	}

	if !renamed {
		t.Fatalf("%s carries no record node to rename", good.Name)
	}

	adapter := &goadapter.Adapter{
		Root:      root,
		Name:      "go",
		Generator: build(t, root, "./cmd/cpybkc-gen-go"),
	}

	read, err := hold(t, adapter,
		frame(`{"id":1,"op":"hello","protocol":1}`),
		generate(t, map[string]*irpb.Descriptor{"good": good.Descriptor, "refused": refused}),
		frame(`{"id":3,"op":"decode","entry":"refused","input":""}`),
		frame(`{"id":4,"op":"bye"}`),
	)
	if err != nil {
		t.Fatalf("the adapter broke, and one entry's worth of failure costs one entry: %v", err)
	}

	if len(read) != 4 {
		t.Fatalf("four requests were made and the adapter wrote %d frames", len(read))
	}

	if got := read[1]; !got.OK {
		t.Fatalf("the adapter refused generate outright, and only one of its entries is refusable: %s", got.Error)
	}

	for _, result := range read[1].Entries {
		switch {
		case result.Entry == "good" && !result.OK:
			t.Errorf("the entry the generator accepted was reported as failing: %s", result.Error)
		case result.Entry == "refused" && result.OK:
			t.Errorf("the entry the generator refused was reported as having code")
		case result.Entry == "refused" && result.Error == "":
			t.Errorf("the refused entry says nothing about why")
		}
	}

	if len(read[1].Entries) != 2 {
		t.Errorf("generate named two entries and the response carries %d results", len(read[1].Entries))
	}

	if got := read[2]; got.OK {
		t.Errorf("the adapter answered a decode of an entry it has no code for")
	}
}

// generate is a generate frame carrying the descriptors it is given, in the
// binary encoding docs/plugin/SPEC.md hands a generator.
func generate(t *testing.T, entries map[string]*irpb.Descriptor) string {
	t.Helper()

	req := map[string]any{"id": 2, "op": "generate"}

	var carried []map[string]any

	// Sorted, so that two runs of this test send one frame: the results are
	// paired by name, and a map walked in its own order would put the same two
	// entries in two orders.
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		descriptor, err := emit.Marshal(entries[name])
		if err != nil {
			t.Fatalf("failed to write the descriptor for %s: %v", name, err)
		}

		carried = append(carried, map[string]any{"entry": name, "descriptor": descriptor})
	}

	req["entries"] = carried

	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to write the generate frame: %v", err)
	}

	return string(b) + "\n"
}

// ask runs the corpus through the engine, driving this repository's own adapter
// as the door's command.
//
// The adapter and the generator are both built from the tree under test, and
// neither is looked up on PATH: a corpus run is about the generator in this
// commit, and resolving a name would find whichever one the author of the commit
// happened to have installed.
func ask(t *testing.T, root string, entries []*conformance.Entry) *engine.Report {
	t.Helper()

	driving := &engine.Engine{
		Door: &engine.Command{
			Path: build(t, root, "./internal/conformance/goadapter/cmd/adapter"),
			Args: []string{
				"--root", root,
				"--generator", build(t, root, "./cmd/cpybkc-gen-go"),
			},
		},
	}

	report, err := driving.Run(t.Context(), entries)
	if err != nil {
		// A run that could not be attempted at all, which is this test's mistake
		// rather than the adapter's behaviour.
		t.Fatalf("the run could not be made: %v", err)
	}

	t.Logf("%s", report)

	return report
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
