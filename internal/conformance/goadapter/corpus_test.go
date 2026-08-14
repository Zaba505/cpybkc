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
	"strings"
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
	case !strings.Contains(report.Adapter.Name, "cpybkc-gen-go"):
		t.Errorf("the adapter calls itself %q, and a report has to say which generator it drove", report.Adapter.Name)
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

	read, err := hold(t, adapting(t, root),
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

// TestWhatTheAdapterHoldsBetweenADecodeAndARoundtrip is the retention rule, in
// the only place it can be checked: from outside, over a conversation.
//
// An adapter holds the records of the most recent decode it answered ok: true,
// until the next decode or the end of the conversation — and no more than that.
// So a roundtrip of the entry before last is refused rather than answered out of
// records that are no longer the reader's most recent, which is a state reset
// that would stop holding silently.
//
// The last two frames are the other precondition: a read that stopped at a
// failure holds no complete set of records to write back. A conforming engine
// never sends that roundtrip, which is exactly why nothing else in this package
// reaches the branch that refuses it.
func TestWhatTheAdapterHoldsBetweenADecodeAndARoundtrip(t *testing.T) {
	root := repoRoot(t)

	entries, err := conformance.Load(conformance.CorpusPath(root))
	if err != nil {
		t.Fatalf("the conformance corpus: %v", err)
	}

	whole := readToTheEnd(t, entries, 2)
	stopped := readThatStops(t, entries)

	read, err := hold(t, adapting(t, root),
		frame(`{"id":1,"op":"hello","protocol":1}`),
		generate(t, map[string]*irpb.Descriptor{
			whole[0].Name: whole[0].Descriptor,
			whole[1].Name: whole[1].Descriptor,
			stopped.Name:  stopped.Descriptor,
		}),
		decoding(t, 3, whole[0]),
		asking(t, 4, "roundtrip", whole[0].Name),
		decoding(t, 5, whole[1]),
		asking(t, 6, "roundtrip", whole[0].Name),
		decoding(t, 7, stopped),
		asking(t, 8, "roundtrip", stopped.Name),
		frame(`{"id":9,"op":"bye"}`),
	)
	if err != nil {
		t.Fatalf("the adapter broke: %v", err)
	}

	if len(read) != 9 {
		t.Fatalf("nine requests were made and the adapter wrote %d frames", len(read))
	}

	if got := read[1]; !got.OK {
		t.Fatalf("the adapter could not generate for these three entries: %s", got.Error)
	}

	switch {
	case !read[2].OK:
		t.Fatalf("the adapter could not read %s: %s", whole[0].Name, read[2].Error)
	case !read[3].OK:
		t.Errorf("the adapter would not write back the records it had just read: %s", read[3].Error)
	case len(read[3].Written) == 0:
		t.Errorf("the roundtrip carries no written document")
	case !read[4].OK:
		t.Fatalf("the adapter could not read %s: %s", whole[1].Name, read[4].Error)
	case read[5].OK:
		t.Errorf("the adapter wrote back the records of %s after decoding %s, and it holds one entry's",
			whole[0].Name, whole[1].Name)
	case !read[6].OK:
		t.Fatalf("a file the generated reader refused is an answer and not a fault: %s", read[6].Error)
	case failure(t, read[6].Decoded) == "":
		t.Fatalf("%s was chosen because its read stops, and this one did not", stopped.Name)
	case read[7].OK:
		t.Errorf("the adapter wrote back a read that stopped at a failure, which holds no complete set of records")
	}
}

// TestTwoRecordNamesThatFoldAlikeStopThatEntry is the failure mode of the
// pairing that replaced the positional one.
//
// A Go type is paired with a record node by folding both names down to their
// letters and digits, so two records the generator gives different identifiers —
// CUSTOMER-ID becomes CustomerId and CustomerID stays CustomerID — can still
// fold alike. That is the one case the fold cannot tell apart, and the entry is
// stopped with a diagnostic rather than walked against whichever record was
// found first. Nothing else in the corpus reaches it, because no entry is named
// that way.
func TestTwoRecordNamesThatFoldAlikeStopThatEntry(t *testing.T) {
	root := repoRoot(t)

	entries, err := conformance.Load(conformance.CorpusPath(root))
	if err != nil {
		t.Fatalf("the conformance corpus: %v", err)
	}

	entry := twoRecords(t, entries)

	folded, ok := proto.Clone(entry.Descriptor).(*irpb.Descriptor)
	if !ok {
		t.Fatalf("a cloned descriptor is not a descriptor")
	}

	alike := []string{"CUSTOMER-ID", "CustomerID"}

	for _, node := range folded.GetNodes() {
		if record := node.GetRecord(); record != nil && len(alike) > 0 {
			record.Names = &irpb.Names{Original: alike[0]}
			alike = alike[1:]
		}
	}

	read, err := hold(t, adapting(t, root),
		frame(`{"id":1,"op":"hello","protocol":1}`),
		generate(t, map[string]*irpb.Descriptor{"folded": folded}),
		decoding(t, 3, &conformance.Entry{Name: "folded", Input: entry.Input}),
		frame(`{"id":4,"op":"bye"}`),
	)
	if err != nil {
		t.Fatalf("the adapter broke, and an entry it cannot pair costs one entry: %v", err)
	}

	if got := read[1]; !got.OK || len(got.Entries) != 1 || !got.Entries[0].OK {
		// The two names munge to two identifiers, so the generator has no
		// collision to refuse: the pairing is what cannot tell them apart.
		t.Fatalf("the generator refused code the fold was supposed to be asked about: %+v", got)
	}

	got := read[2]

	if got.OK {
		t.Fatalf("the adapter answered about an entry whose records it cannot tell apart")
	}

	if !strings.Contains(got.Error, "named alike") {
		t.Errorf("the refusal does not say the two records could not be told apart: %s", got.Error)
	}
}

// adapting is this repository's adapter, built against the generator in the tree
// under test and driven in this process rather than through a door.
func adapting(t *testing.T, root string) *goadapter.Adapter {
	t.Helper()

	return &goadapter.Adapter{Root: root, Name: "go", Generator: build(t, root, "./cmd/cpybkc-gen-go")}
}

// readToTheEnd is the first n entries the generated reader reads to the end of,
// which are the ones a roundtrip may be asked about.
func readToTheEnd(t *testing.T, entries []*conformance.Entry, n int) []*conformance.Entry {
	t.Helper()

	var found []*conformance.Entry

	for _, entry := range entries {
		if entry.Values.Failure == "" && len(entry.Values.Records) > 0 {
			found = append(found, entry)
		}

		if len(found) == n {
			return found
		}
	}

	t.Fatalf("the corpus holds fewer than %d entries whose read reaches the end of the file", n)

	return nil
}

// readThatStops is an entry the file itself stops the reader on, which is what a
// roundtrip has no complete set of records for.
func readThatStops(t *testing.T, entries []*conformance.Entry) *conformance.Entry {
	t.Helper()

	for _, entry := range entries {
		if entry.Values.Failure != "" {
			return entry
		}
	}

	t.Fatalf("the corpus holds no entry whose read stops at a failure")

	return nil
}

// twoRecords is an entry whose descriptor carries at least two record nodes,
// which is what it takes for two of them to be named alike.
func twoRecords(t *testing.T, entries []*conformance.Entry) *conformance.Entry {
	t.Helper()

	for _, entry := range entries {
		records := 0

		for _, node := range entry.Descriptor.GetNodes() {
			if node.GetRecord() != nil {
				records++
			}
		}

		if records >= 2 {
			return entry
		}
	}

	t.Fatalf("the corpus holds no entry carrying two record nodes")

	return nil
}

// failure is what a values document says stopped the read, or the empty string
// where it was read to the end.
func failure(t *testing.T, document json.RawMessage) string {
	t.Helper()

	var values struct {
		Failure string `json:"failure"`
	}

	if err := json.Unmarshal(document, &values); err != nil {
		t.Fatalf("the adapter answered with a document that is not one: %v", err)
	}

	return values.Failure
}

// decoding is a decode frame carrying an entry's bytes, which encoding/json
// writes as base64 exactly as the engine's own frame does.
func decoding(t *testing.T, id int, entry *conformance.Entry) string {
	t.Helper()

	return marshalled(t, map[string]any{"id": id, "op": "decode", "entry": entry.Name, "input": entry.Input})
}

// asking is a frame for an operation that names an entry and carries nothing
// else.
func asking(t *testing.T, id int, op, entry string) string {
	t.Helper()

	return marshalled(t, map[string]any{"id": id, "op": op, "entry": entry})
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

	return marshalled(t, req)
}

// marshalled is one request frame: one JSON object on one line.
func marshalled(t *testing.T, req map[string]any) string {
	t.Helper()

	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to write a request frame: %v", err)
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
