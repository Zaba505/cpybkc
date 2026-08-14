// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package engine_test

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/Zaba505/cpybkc/internal/conformance"
	"github.com/Zaba505/cpybkc/internal/conformance/engine"
)

// TestTheEngineDrivesAnAdapterThroughTheContract is the conversation
// docs/adapter/SPEC.md specifies, held with a real process over real pipes.
//
// The entries are chosen for the two shapes an answer takes: a file read to its
// end, which is asked in both directions, and a file the reader refused, which
// is an answer and not a fault — a large fraction of the corpus expects
// precisely that, and an engine that reported one as a fault would fail those
// entries by being right about them.
func TestTheEngineDrivesAnAdapterThroughTheContract(t *testing.T) {
	entries := corpus(t, "packed-ebcdic", "packed-invalid-sign", "zoned-ebcdic")

	open, sent := door(t, script{Entries: answers(t, entries)})

	report, err := (&engine.Engine{Door: open}).Run(t.Context(), entries)
	if err != nil {
		t.Fatalf("the run could not be made: %v", err)
	}

	if report.Failed() {
		t.Fatalf("the adapter answered what every entry states and the run failed:\n%v", report)
	}

	if report.Adapter == nil || report.Adapter.Name != "the fake adapter" {
		t.Fatalf("the report does not say what answered it: %v", report.Adapter)
	}

	if report.Restarts != 0 {
		t.Errorf("the run started %d fresh adapters, and nothing broke", report.Restarts)
	}

	// The conversation, in the order the contract fixes it: the handshake
	// first, every descriptor at once next, then one entry at a time, and the
	// writing direction only where the read reached the end of the file.
	ops := operations(t, sent)

	want := []string{
		"hello", "generate",
		"decode", "roundtrip", // packed-ebcdic
		"decode",              // packed-invalid-sign, whose read stops at a failure
		"decode", "roundtrip", // zoned-ebcdic
		"bye",
	}

	if strings.Join(ops, ",") != strings.Join(want, ",") {
		t.Errorf("the engine sent %v, and the contract's conversation is %v", ops, want)
	}
}

// TestTheIdentifierIsStrictlyIncreasingFromOne pins the one thing that turns a
// stream which has silently desynchronised into an error at the frame where it
// happened rather than a wrong answer several entries later.
func TestTheIdentifierIsStrictlyIncreasingFromOne(t *testing.T) {
	entries := corpus(t, "packed-ebcdic", "zoned-ebcdic")

	open, sent := door(t, script{Entries: answers(t, entries)})

	if _, err := (&engine.Engine{Door: open}).Run(t.Context(), entries); err != nil {
		t.Fatalf("the run could not be made: %v", err)
	}

	for i, frame := range frames(t, sent) {
		var got struct {
			ID int `json:"id"`
		}

		if err := json.Unmarshal([]byte(frame), &got); err != nil {
			t.Fatalf("the engine sent something that is not a frame: %q", frame)
		}

		if got.ID != i+1 {
			t.Fatalf("request %d carries id %d, and identifiers start at 1 and increase by one", i+1, got.ID)
		}
	}
}

// TestTheAdapterIsNeverGivenTheExpectedValues is the rule the whole corpus
// rests on: an adapter holding the expected answers is self-grading, which is
// not a weaker check than an independent one but a different one — it measures
// whether the adapter's author could reproduce a document they were handed, and
// nothing in a passing result would distinguish that from a decoder.
//
// What is asserted is every frame the engine sent, against every scalar every
// entry states — at every depth, in every record, and including the text of a
// failure, since a leak one group down is the same leak. The descriptor and the
// input bytes are asserted to be there too, because a run that sent nothing at
// all would pass the first assertion.
func TestTheAdapterIsNeverGivenTheExpectedValues(t *testing.T) {
	entries := corpus(t, "packed-ebcdic", "zoned-ebcdic", "packed-invalid-sign", "batch-rdw")

	open, sent := door(t, script{Entries: answers(t, entries)})

	if _, err := (&engine.Engine{Door: open}).Run(t.Context(), entries); err != nil {
		t.Fatalf("the run could not be made: %v", err)
	}

	said := transcript(t, sent)

	looked := 0

	for _, entry := range entries {
		for _, spelled := range scalars(entry.Values) {
			// A short value is passed over rather than searched for. The frames
			// carry the descriptor and the input as base64, so a one or two
			// character value occurs in them by coincidence — and a test that
			// failed on a coincidence is a test somebody deletes.
			if len(spelled) < 3 {
				continue
			}

			looked++

			if strings.Contains(said, spelled) {
				t.Errorf("the engine sent %q, which is part of what %s states", spelled, entry.Name)
			}
		}
	}

	if looked == 0 {
		t.Fatal("no expected value was long enough to look for, so this test asserted nothing")
	}

	if !strings.Contains(said, `"descriptor"`) || !strings.Contains(said, `"input"`) {
		t.Error("the engine sent neither a descriptor nor any input, so it asked the adapter nothing")
	}
}

// scalars is every value a values document states, at every depth, and the text
// of its failure.
func scalars(values *conformance.Values) []string {
	var (
		found []string
		walk  func(value any)
	)

	walk = func(value any) {
		switch held := value.(type) {
		case map[string]any:
			for _, one := range held {
				walk(one)
			}
		case []any:
			for _, one := range held {
				walk(one)
			}
		case string:
			found = append(found, held)
		}
	}

	for _, record := range values.Records {
		walk(record.Value)
	}

	if values.Failure != "" {
		found = append(found, values.Failure)
	}

	return found
}

// TestAnAdapterThatCrashesOnOneEntryDoesNotCostTheRest is the fault isolation
// the corpus is unusable without.
//
// The conversation with a crashed adapter is over — a stream whose framing is in
// doubt cannot be resynchronised by anything the receiver can see — so what the
// engine owes the run is a fresh process on the entries that were left, not a
// report that stops at the crash.
func TestAnAdapterThatCrashesOnOneEntryDoesNotCostTheRest(t *testing.T) {
	entries := corpus(t, "packed-ebcdic", "zoned-ebcdic", "packed-invalid-sign")

	open, _ := door(t, script{
		Entries: answers(t, entries),
		Break:   map[string]string{"zoned-ebcdic": breakCrash},
	})

	report, err := (&engine.Engine{Door: open}).Run(t.Context(), entries)
	if err != nil {
		t.Fatalf("the run could not be made: %v", err)
	}

	// Every entry is reported, which is the assertion the outcomes below rest
	// on: an entry the run dropped altogether is the failure this test is about,
	// and a lookup of a name that is not there says nothing on its own.
	if len(report.Results) != len(entries) {
		t.Fatalf("the run reported %d of %d entries:\n%v", len(report.Results), len(entries), report)
	}

	outcomes := outcomes(report)

	if outcomes["packed-ebcdic"] != engine.Passed || outcomes["packed-invalid-sign"] != engine.Passed {
		t.Errorf("the entries either side of the crash did not pass:\n%v", report)
	}

	if outcomes["zoned-ebcdic"] != engine.Faulted {
		t.Errorf("the entry the adapter crashed on is %v, and nothing was learned about it", outcomes["zoned-ebcdic"])
	}

	if report.Restarts != 1 {
		t.Errorf("the run started %d fresh adapters, and one crash needs one", report.Restarts)
	}

	// An engine SHOULD capture the adapter's standard error and quote it beside
	// a fault, because a broken adapter's own words are usually the only
	// explanation there is.
	if said := reported(report, "zoned-ebcdic"); !strings.Contains(said, "about to crash") {
		t.Errorf("the fault is %q, and it does not quote what the adapter said as it died", said)
	}
}

// TestThePerCaseDeadlineIsTheEngines asserts that an adapter which hangs on one
// entry costs that entry and not the run.
//
// The deadline is the engine's because an adapter that gave up on a slow entry
// would turn one slow entry into a broken adapter and cost everything after it.
// The fake adapter here never answers and never exits, so what ends it is the
// deadline and the kill behind it — and the run finishing at all is the
// assertion.
func TestThePerCaseDeadlineIsTheEngines(t *testing.T) {
	entries := corpus(t, "packed-ebcdic", "zoned-ebcdic", "packed-invalid-sign")

	open, _ := door(t, script{
		Entries: answers(t, entries),
		Break:   map[string]string{"zoned-ebcdic": breakHang},
	})

	// Two seconds rather than something tighter because this deadline bounds
	// the handshake too, and a handshake here is a process spawn: a fork, a Go
	// runtime starting up and a script being read, twice over, since the hang
	// costs a restart. What the test needs is only that the hang resolves in a
	// fraction of the suite's own wall clock, and a budget that also has to
	// cover an exec on a loaded machine is a budget that fails for a reason
	// this test is not about.
	report, err := (&engine.Engine{Door: open, Deadline: 2 * time.Second}).Run(t.Context(), entries)
	if err != nil {
		t.Fatalf("the run could not be made: %v", err)
	}

	outcomes := outcomes(report)

	if outcomes["zoned-ebcdic"] != engine.Faulted {
		t.Errorf("the entry the adapter hung on is %v", outcomes["zoned-ebcdic"])
	}

	if said := reported(report, "zoned-ebcdic"); !strings.Contains(said, "did not answer") {
		t.Errorf("the fault is %q, and it does not say the adapter ran out of time", said)
	}

	if outcomes["packed-invalid-sign"] != engine.Passed {
		t.Errorf("the entry behind the hang did not pass:\n%v", report)
	}
}

// TestAnAdapterThatStopsReadingIsNotAHangTheEngineWaitsOut bounds the half of
// an operation that is easy to leave unbounded.
//
// An adapter that answers the handshake and then stops attending to its
// standard input — one that deadlocked in its own startup — is not an adapter
// that fails to answer. It is one the engine cannot finish writing to, and the
// generate frame carries every entry's descriptor at once, so it is comfortably
// the frame that fills a pipe. A deadline on the answer alone would never be
// reached, because the request it was waiting to have answered was never sent.
//
// The corpus is doubled until the frame cannot fit in any pipe buffer a system
// might have — a megabyte or so, against Linux's default of 64 KiB — because a
// write that happens to fit succeeds and tests the read deadline instead.
func TestAnAdapterThatStopsReadingIsNotAHangTheEngineWaitsOut(t *testing.T) {
	entries := corpus(t)

	for len(entries) < 1024 {
		entries = append(entries, entries...)
	}

	open, _ := door(t, script{DeafAfterHello: true})

	report, err := (&engine.Engine{
		Door: open, Deadline: 2 * time.Second, BuildDeadline: 2 * time.Second,
	}).Run(t.Context(), entries)
	if err != nil {
		t.Fatalf("the run could not be made: %v", err)
	}

	if len(report.Results) != len(entries) {
		t.Fatalf("the run reported %d of %d entries", len(report.Results), len(entries))
	}

	if !strings.Contains(reported(report, entries[0].Name), "did not read") {
		t.Errorf("the fault is %q, and the adapter stopped reading rather than stopped answering",
			reported(report, entries[0].Name))
	}

	if report.Restarts != 0 {
		t.Errorf("the run started %d fresh adapters over a generate that would block again", report.Restarts)
	}
}

// TestAMismatchNamesTheFieldAndItsPosition is the difference between a
// framework somebody uses twice and one they use fifty times.
//
// The engine holds the descriptor and the bytes, so it can say which item
// disagreed, where the position sum puts it, how wide it is and what encoding
// it was read under — and quote the bytes it was read from. Nothing else in the
// system can: the adapter was never told what was expected, and the corpus
// package compares documents rather than bytes.
func TestAMismatchNamesTheFieldAndItsPosition(t *testing.T) {
	entries := corpus(t, "packed-ebcdic")
	entry := entries[0]

	const item = "P-SIGNED-POS"

	held, ok := entry.Values.Records[0].Value.(map[string]any)
	if !ok {
		t.Fatalf("%s: the first record is not a group", entry.Name)
	}

	wrong := maps.Clone(held)
	wrong[item] = "99999"

	decoded := marshalled(t, &conformance.Values{
		Records: []conformance.Record{{Name: entry.Values.Records[0].Name, Value: wrong}},
	})

	open, _ := door(t, script{Entries: map[string]entryScript{
		entry.Name: {Decoded: decoded, Written: decoded},
	}})

	report, err := (&engine.Engine{Door: open}).Run(t.Context(), entries)
	if err != nil {
		t.Fatalf("the run could not be made: %v", err)
	}

	if outcomes(report)[entry.Name] != engine.Mismatched {
		t.Fatalf("the adapter answered a value the file does not hold and the run did not report a mismatch:\n%v", report)
	}

	said := reported(report, entry.Name)

	for _, says := range []string{
		item,                      // which item disagreed
		"at offset 0 of record 1", // where the descriptor puts it
		"3 bytes",                 // and how wide it is
		"usage PACKED_DECIMAL",    // under which encoding it was read
		"charset ",
		"input.bin 0x0000..0x0002: 12 34 5c", // and the bytes themselves
	} {
		if !strings.Contains(said, says) {
			t.Errorf("the report is\n%s\nand it does not say %q", said, says)
		}
	}
}

// TestAReadOnlyAdapterIsNotFailedForOmittingTheWriteDirection holds the engine
// to the rule that keeps a legal generator from failing every positive entry.
//
// docs/ir/SPEC.md's "Writing a file" leaves emitting a writer to the generator,
// so a generator that emits a reader and no writer is conformant — and an
// engine that demanded the writing direction of every adapter would report,
// once per entry, a missing answer to a question the specification never
// obliged anybody to answer.
func TestAReadOnlyAdapterIsNotFailedForOmittingTheWriteDirection(t *testing.T) {
	entries := corpus(t, "packed-ebcdic", "zoned-ebcdic")

	open, sent := door(t, script{
		Capabilities: map[string]bool{},
		Entries:      answers(t, entries),
	})

	report, err := (&engine.Engine{Door: open}).Run(t.Context(), entries)
	if err != nil {
		t.Fatalf("the run could not be made: %v", err)
	}

	if report.Failed() {
		t.Fatalf("a read-only adapter answered every entry's reading direction and the run failed:\n%v", report)
	}

	// The engine MUST NOT send roundtrip to an adapter that did not declare the
	// capability, which is a stronger statement than not failing it for the
	// answer being absent.
	for _, op := range operations(t, sent) {
		if op == "roundtrip" {
			t.Fatal("the engine asked a read-only adapter to write a file back")
		}
	}

	// A run by a read-only adapter is a smaller claim than a run by a full one,
	// and an engine SHOULD say which it was.
	if said := report.String(); !strings.Contains(said, "read-only") {
		t.Errorf("the report is\n%s\nand it does not say the claim is a smaller one", said)
	}
}

// TestAnAdapterThatBreaksTheFramingCostsOneEntry walks the ways a stream stops
// being a conversation.
//
// Every one of them is a hazard docs/adapter/SPEC.md names: a library that
// greeted the world on standard output, a blank line, a carriage return a
// receiver MUST refuse rather than trim, and a frame answering a request nobody
// sent. Each costs the entry it happened on and the run carries on, which is
// the same isolation a crash gets.
func TestAnAdapterThatBreaksTheFramingCostsOneEntry(t *testing.T) {
	tests := map[string]struct {
		broke string
		says  string
	}{
		"a greeting on standard output": {broke: breakGarbage, says: "not a frame"},
		"a blank line":                  {broke: breakBlank, says: "blank line"},
		"a carriage return":             {broke: breakCR, says: "carriage return"},
		"an answer to nothing":          {broke: breakWrongID, says: "answered request"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			entries := corpus(t, "packed-ebcdic", "zoned-ebcdic", "packed-invalid-sign")

			open, _ := door(t, script{
				Entries: answers(t, entries),
				Break:   map[string]string{"zoned-ebcdic": test.broke},
			})

			report, err := (&engine.Engine{Door: open, Deadline: 5 * time.Second}).Run(t.Context(), entries)
			if err != nil {
				t.Fatalf("the run could not be made: %v", err)
			}

			outcomes := outcomes(report)

			if outcomes["zoned-ebcdic"] != engine.Faulted {
				t.Errorf("the entry the framing broke on is %v", outcomes["zoned-ebcdic"])
			}

			if said := reported(report, "zoned-ebcdic"); !strings.Contains(said, test.says) {
				t.Errorf("the fault is %q, and it does not say %q", said, test.says)
			}

			if outcomes["packed-ebcdic"] != engine.Passed || outcomes["packed-invalid-sign"] != engine.Passed {
				t.Errorf("the entries either side of it did not pass:\n%v", report)
			}
		})
	}
}

// TestTheHandshakeCostsTheWholeRun walks what an engine refuses before it has
// asked anything.
//
// A fault at the handshake is the one fault that costs the whole run rather
// than one entry: there is no conversation left to have. Every entry is
// reported as a fault, because an entry nobody asked about is not an entry that
// passed.
func TestTheHandshakeCostsTheWholeRun(t *testing.T) {
	two := 2

	tests := map[string]struct {
		told script
		says string
	}{
		"a version the adapter does not speak": {
			told: script{RefuseHello: "this adapter speaks protocol 2", Protocol: &two},
			says: "refused the handshake",
		},
		"a version it agreed to and does not speak": {
			told: script{Protocol: &two},
			says: "stating protocol 2",
		},
		"a kind this engine has never heard of": {
			told: script{Kind: "oracular"},
			says: `declared kind "oracular"`,
		},
		"no capabilities at all": {
			told: script{OmitCapabilities: true},
			says: "declared no capabilities",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			entries := corpus(t, "packed-ebcdic", "zoned-ebcdic")

			told := test.told
			told.Entries = answers(t, entries)

			open, sent := door(t, told)

			report, err := (&engine.Engine{Door: open}).Run(t.Context(), entries)
			if err != nil {
				t.Fatalf("the run could not be made: %v", err)
			}

			for _, entry := range entries {
				if outcomes(report)[entry.Name] != engine.Faulted {
					t.Errorf("%s was not reported as a fault, and the handshake never completed:\n%v", entry.Name, report)
				}

				if said := reported(report, entry.Name); !strings.Contains(said, test.says) {
					t.Errorf("the fault is %q, and it does not say %q", said, test.says)
				}
			}

			for _, op := range operations(t, sent) {
				if op != "hello" {
					t.Errorf("the engine sent %s after a handshake that failed", op)
				}
			}
		})
	}
}

// TestADescriptiveAdapterIsNotAConformanceSubject asserts the one thing an
// engine owes a generator that emits a diagram rather than a codec: it is asked
// nothing and reported as not applicable.
//
// The two shapes that must not happen are a descriptive generator scored zero
// out of the whole corpus, and one declining every entry in turn. Neither is
// true, both read as failures, and what such a generator should be held to
// instead is an open question (#193, #201) that asking it the wrong one would
// not answer.
func TestADescriptiveAdapterIsNotAConformanceSubject(t *testing.T) {
	entries := corpus(t, "packed-ebcdic", "zoned-ebcdic")

	open, sent := door(t, script{Kind: "descriptive", Capabilities: map[string]bool{}})

	report, err := (&engine.Engine{Door: open}).Run(t.Context(), entries)
	if err != nil {
		t.Fatalf("the run could not be made: %v", err)
	}

	if !report.NotApplicable {
		t.Fatalf("the run is not reported as not applicable:\n%v", report)
	}

	if report.Failed() || len(report.Results) != 0 {
		t.Errorf("a descriptive generator was scored against the corpus:\n%v", report)
	}

	want := []string{"hello", "bye"}
	if got := operations(t, sent); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the engine sent %v to a descriptive adapter, and it may send %v", got, want)
	}
}

// TestGenerate walks the two halves of the operation that costs the most.
//
// An entry the generator would not accept, or whose generated code would not
// compile, is a fault against that entry and the adapter stays alive to serve
// the rest. A generate the adapter did not serve at all is every entry lost —
// and the engine MUST NOT send decode for any of them, which is what the
// transcript is asserted for.
func TestGenerate(t *testing.T) {
	t.Run("an entry the adapter has no code for costs that entry", func(t *testing.T) {
		entries := corpus(t, "packed-ebcdic", "zoned-ebcdic")

		open, sent := door(t, script{
			Entries:      answers(t, entries),
			GenerateFail: map[string]string{"zoned-ebcdic": "OCCURS DEPENDING ON is not supported"},
		})

		report, err := (&engine.Engine{Door: open}).Run(t.Context(), entries)
		if err != nil {
			t.Fatalf("the run could not be made: %v", err)
		}

		if outcomes(report)["zoned-ebcdic"] != engine.Faulted {
			t.Errorf("an entry the adapter cannot generate for is not a fault:\n%v", report)
		}

		if outcomes(report)["packed-ebcdic"] != engine.Passed {
			t.Errorf("the entry it can generate for did not pass:\n%v", report)
		}

		if said := transcript(t, sent); strings.Contains(said, `"op":"decode","entry":"zoned-ebcdic"`) {
			t.Error("the engine asked about an entry the adapter said it had no code for")
		}
	})

	t.Run("a generate the adapter did not serve costs the run", func(t *testing.T) {
		entries := corpus(t, "packed-ebcdic", "zoned-ebcdic")

		open, sent := door(t, script{
			Entries:        answers(t, entries),
			GenerateRefuse: "this adapter could not read the request",
		})

		report, err := (&engine.Engine{Door: open}).Run(t.Context(), entries)
		if err != nil {
			t.Fatalf("the run could not be made: %v", err)
		}

		for _, entry := range entries {
			if outcomes(report)[entry.Name] != engine.Faulted {
				t.Errorf("%s survived a generate nobody served:\n%v", entry.Name, report)
			}
		}

		for _, op := range operations(t, sent) {
			if op == "decode" || op == "roundtrip" {
				t.Errorf("the engine sent %s after a generate the adapter did not serve", op)
			}
		}

		if report.Restarts != 0 {
			t.Errorf("the run started %d fresh adapters over a generate that would fail again", report.Restarts)
		}
	})
}

// TestAnEntryTheAdapterRefusedIsAFaultAndNotAMismatch keeps the two apart.
//
// A mismatch is a disagreement about bytes, and whoever reads it decides
// whether the generator or the entry is wrong. A refusal is the corpus failing
// to ask the question, so nothing has been learned about either — and a run
// reporting it as a disagreement would send a generator author to a
// specification section about a claim that was never tested.
func TestAnEntryTheAdapterRefusedIsAFaultAndNotAMismatch(t *testing.T) {
	entries := corpus(t, "packed-ebcdic", "zoned-ebcdic")

	told := answers(t, entries)
	told["packed-ebcdic"] = entryScript{DecodeRefuse: "the generated reader panicked"}

	open, _ := door(t, script{Entries: told})

	report, err := (&engine.Engine{Door: open}).Run(t.Context(), entries)
	if err != nil {
		t.Fatalf("the run could not be made: %v", err)
	}

	if outcomes(report)["packed-ebcdic"] != engine.Faulted {
		t.Errorf("an entry the adapter refused is not a fault:\n%v", report)
	}

	if said := reported(report, "packed-ebcdic"); !strings.Contains(said, "could not be run") {
		t.Errorf("the fault is %q, and it does not read as one", said)
	}

	if outcomes(report)["zoned-ebcdic"] != engine.Passed {
		t.Errorf("the entry behind the refusal did not pass:\n%v", report)
	}

	if report.Restarts != 0 {
		t.Errorf("a refusal restarted the adapter %d times, and a refusal leaves it working", report.Restarts)
	}
}

// TestARunThatCannotBeAttempted asserts that the caller's own mistakes are
// errors rather than reports: a report saying nothing failed is a report about
// a run that happened.
func TestARunThatCannotBeAttempted(t *testing.T) {
	entries := corpus(t, "packed-ebcdic")

	if _, err := (&engine.Engine{}).Run(t.Context(), entries); err == nil {
		t.Error("an engine with no door ran")
	}

	open, _ := door(t, script{})

	if _, err := (&engine.Engine{Door: open}).Run(t.Context(), nil); err == nil {
		t.Error("an engine with nothing to ask about ran")
	}
}

// outcomes is what became of each entry, by name.
func outcomes(report *engine.Report) map[string]engine.Outcome {
	held := make(map[string]engine.Outcome, len(report.Results))

	for _, result := range report.Results {
		held[result.Entry] = result.Outcome
	}

	return held
}

// reported is what the report says about one entry.
func reported(report *engine.Report, entry string) string {
	for _, result := range report.Results {
		if result.Entry != entry || result.Err == nil {
			continue
		}

		return result.Err.Error()
	}

	return ""
}

// frames is every request frame the engine sent, in order.
func frames(t *testing.T, path string) []string {
	t.Helper()

	var sent []string

	for _, line := range strings.Split(transcript(t, path), "\n") {
		if line != "" {
			sent = append(sent, line)
		}
	}

	return sent
}

// operations is the op of every request frame the engine sent, in order.
func operations(t *testing.T, path string) []string {
	t.Helper()

	var ops []string

	for _, frame := range frames(t, path) {
		var got struct {
			Op string `json:"op"`
		}

		if err := json.Unmarshal([]byte(frame), &got); err != nil {
			t.Fatalf("the engine sent something that is not a frame: %q", frame)
		}

		ops = append(ops, got.Op)
	}

	return ops
}
