// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package engine_test

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/conformance"
	"github.com/Zaba505/cpybkc/internal/conformance/engine"
)

// TestAProvisionalEntryDisagreesAndTheRunDoesNotFail is the whole of what
// declaring an entry provisional buys (#207).
//
// The corpus's authoring rule is that an entry is derived from a specification
// and never recorded from the code it checks, because an entry recorded from
// its subject passes forever — including through the bug it was written to
// catch. The mirror of that rule is an entry authored from a misreading, which
// fails forever and tells every implementation it is wrong, and the failure it
// causes is not a red mark: it is a generator author who trusts the corpus,
// changes correct code to match a wrong oracle, and ships it.
//
// So a provisional entry's disagreement is reported and is not a verdict. All
// three halves of that are asserted, because any one of them alone would be a
// different behaviour: the run does not fail, the disagreement is in the report
// in the reader's face, and the entry is in no total.
func TestAProvisionalEntryDisagreesAndTheRunDoesNotFail(t *testing.T) {
	entries := corpus(t, "packed-ebcdic", "zoned-ebcdic")

	uncorroborated := entries[0]
	uncorroborated.Status = conformance.Provisional

	told := answers(t, entries)
	told[uncorroborated.Name] = entryScript{Decoded: wrong(t, uncorroborated, "P-SIGNED-POS")}

	open, _ := door(t, script{Entries: told})

	report, err := (&engine.Engine{Door: open}).Run(t.Context(), entries)
	if err != nil {
		t.Fatalf("the run could not be made: %v", err)
	}

	if report.Failed() {
		t.Errorf("a provisional entry disagreed and the run failed:\n%v", report)
	}

	if got := outcomes(report)[uncorroborated.Name]; got != engine.Mismatched {
		t.Errorf("the provisional entry came back %s, and the adapter answered a value the file does not hold:\n%v",
			got, report)
	}

	if passed, mismatched, faulted := report.Counts(); passed != 1 || mismatched != 0 || faulted != 0 {
		t.Errorf("the normative half of the corpus scored %d passed, %d disagreed and %d could not be asked, and "+
			"one normative entry was asked about:\n%v", passed, mismatched, faulted, report)
	}

	if agreed, disagreed, unanswered := report.ProvisionalCounts(); agreed != 0 || disagreed != 1 || unanswered != 0 {
		t.Errorf("the provisional half scored %d agreed, %d disagreed and %d could not be asked, and one "+
			"provisional entry disagreed:\n%v", agreed, disagreed, unanswered, report)
	}

	said := report.String()

	for _, says := range []string{
		"PROVISIONAL FAIL",                   // the disagreement, marked as no verdict
		uncorroborated.Name,                  // and about which entry
		"1 entries: 1 passed",                // the total the provisional entry is not in
		"1 provisional entries, in no total", // and the line it is in instead
	} {
		if !strings.Contains(said, says) {
			t.Errorf("the report is\n%s\nand it does not say %q", said, says)
		}
	}
}

// TestAProvisionalEntryThatAgreesIsStillInNoTotal is the direction that would
// go unnoticed.
//
// A provisional entry that disagrees is conspicuous; one that agrees would
// quietly raise the number an implementation reports having passed, and the
// entry would have scored a generator on an answer nobody has corroborated.
// Counting in no total is stated in both directions for that reason.
func TestAProvisionalEntryThatAgreesIsStillInNoTotal(t *testing.T) {
	entries := corpus(t, "packed-ebcdic", "zoned-ebcdic")
	entries[0].Status = conformance.Provisional

	open, _ := door(t, script{Entries: answers(t, entries)})

	report, err := (&engine.Engine{Door: open}).Run(t.Context(), entries)
	if err != nil {
		t.Fatalf("the run could not be made: %v", err)
	}

	if report.Failed() {
		t.Fatalf("the adapter answered what every entry states and the run failed:\n%v", report)
	}

	if passed, mismatched, faulted := report.Counts(); passed != 1 || mismatched != 0 || faulted != 0 {
		t.Errorf("the normative half scored %d passed, %d disagreed and %d could not be asked, and one normative "+
			"entry agreed:\n%v", passed, mismatched, faulted, report)
	}

	if agreed, disagreed, unanswered := report.ProvisionalCounts(); agreed != 1 || disagreed != 0 || unanswered != 0 {
		t.Errorf("the provisional half scored %d agreed, %d disagreed and %d could not be asked, and one "+
			"provisional entry agreed:\n%v", agreed, disagreed, unanswered, report)
	}

	if said := report.String(); !strings.Contains(said, "PROVISIONAL PASS") {
		t.Errorf("the report is\n%s\nand it does not mark the provisional entry that agreed", said)
	}
}

// TestAProvisionalEntryNothingCouldBeAskedAboutIsNotAFailureEither closes the
// third outcome.
//
// An entry the adapter could not be asked about at all is a fault rather than a
// disagreement, and it fails a run for a reason of its own — nothing was
// learned. On a provisional entry there was nothing to learn that anybody
// stands behind, so it is reported and it is not a verdict, exactly as a
// disagreement is. Left out, a provisional entry would be exempt from the
// verdict for one outcome and not the other, which is the sort of gap that is
// found by an implementation being failed by it.
func TestAProvisionalEntryNothingCouldBeAskedAboutIsNotAFailureEither(t *testing.T) {
	entries := corpus(t, "packed-ebcdic", "zoned-ebcdic")

	uncorroborated := entries[0]
	uncorroborated.Status = conformance.Provisional

	open, _ := door(t, script{
		GenerateFail: map[string]string{uncorroborated.Name: "this generator does not handle that construct"},
		Entries:      answers(t, entries),
	})

	report, err := (&engine.Engine{Door: open}).Run(t.Context(), entries)
	if err != nil {
		t.Fatalf("the run could not be made: %v", err)
	}

	if report.Failed() {
		t.Errorf("a provisional entry could not be asked about and the run failed:\n%v", report)
	}

	if got := outcomes(report)[uncorroborated.Name]; got != engine.Faulted {
		t.Errorf("the provisional entry came back %s, and the generator refused its descriptor:\n%v", got, report)
	}

	if agreed, disagreed, unanswered := report.ProvisionalCounts(); agreed != 0 || disagreed != 0 || unanswered != 1 {
		t.Errorf("the provisional half scored %d agreed, %d disagreed and %d could not be asked:\n%v",
			agreed, disagreed, unanswered, report)
	}

	if said := report.String(); !strings.Contains(said, "PROVISIONAL FAULT") {
		t.Errorf("the report is\n%s\nand it does not mark the provisional entry nothing was learned about", said)
	}
}

// TestARunOfNothingButProvisionalEntriesSaysSo is the shape a report has to be
// loudest about.
//
// Every line of it would otherwise read as a clean run: nothing disagreed,
// nothing faulted, and the total is zero out of zero. A reader who took that
// for a pass would be reading a conformance result about a corpus nobody stands
// behind, which is the one thing a report of no entries must not look like.
func TestARunOfNothingButProvisionalEntriesSaysSo(t *testing.T) {
	entries := corpus(t, "packed-ebcdic")
	entries[0].Status = conformance.Provisional

	open, _ := door(t, script{Entries: answers(t, entries)})

	report, err := (&engine.Engine{Door: open}).Run(t.Context(), entries)
	if err != nil {
		t.Fatalf("the run could not be made: %v", err)
	}

	if passed, mismatched, faulted := report.Counts(); passed != 0 || mismatched != 0 || faulted != 0 {
		t.Errorf("the normative half scored %d passed, %d disagreed and %d could not be asked, out of a run that "+
			"asked about no normative entry:\n%v", passed, mismatched, faulted, report)
	}

	if said := report.String(); !strings.Contains(said, "states nothing about conformance") {
		t.Errorf("the report is\n%s\nand it does not say that nothing it asked about is corroborated", said)
	}
}

// TestAnEntryThatDeclaresItselfProvisionalOnDiskIsExempt is the one path the
// tests above take a short cut around.
//
// They set the status in Go, which is the readable way to write them and leaves
// exactly one link untested: entry.json to the verdict. A regression that
// dropped the status while loading — a renamed member, a metadata struct that
// stopped carrying it — would leave every one of them passing, because they
// never read the file. So this one declares it where an author declares it, in
// entry.json, and asserts the far end: the run does not fail.
func TestAnEntryThatDeclaresItselfProvisionalOnDiskIsExempt(t *testing.T) {
	entry, err := conformance.LoadEntry(declaredProvisional(t, "packed-ebcdic"))
	if err != nil {
		t.Fatalf("%v", err)
	}

	if !entry.IsProvisional() {
		t.Fatalf("%s declares itself provisional in %s and did not load as one", entry.Name, conformance.MetadataName)
	}

	entries := []*conformance.Entry{entry}

	open, _ := door(t, script{Entries: map[string]entryScript{
		entry.Name: {Decoded: wrong(t, entry, "P-SIGNED-POS")},
	}})

	report, err := (&engine.Engine{Door: open}).Run(t.Context(), entries)
	if err != nil {
		t.Fatalf("the run could not be made: %v", err)
	}

	if report.Failed() {
		t.Errorf("an entry that declares itself provisional in %s disagreed and the run failed:\n%v",
			conformance.MetadataName, report)
	}

	if !report.StatesNothing() {
		t.Errorf("the run asked about nothing but a provisional entry and does not say it states nothing:\n%v", report)
	}
}

// declaredProvisional is a copy of a shipped entry with "provisional" written
// into its entry.json, in a directory of the test's own.
//
// A copy rather than a fixture of its own: what the case is about is the
// status, and an entry written for this test would be a second author's reading
// of every other member. The corpus's own entries are all normative and a test
// holds them that way, so the status has to be put there by the case that needs
// it.
func declaredProvisional(t *testing.T, name string) string {
	t.Helper()

	source := filepath.Join(conformance.CorpusPath(repoRoot(t)), name)

	dir := filepath.Join(t.TempDir(), name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("%v", err)
	}

	listing, err := os.ReadDir(source)
	if err != nil {
		t.Fatalf("%v", err)
	}

	for _, item := range listing {
		b, err := os.ReadFile(filepath.Join(source, item.Name()))
		if err != nil {
			t.Fatalf("%v", err)
		}

		if item.Name() == conformance.MetadataName {
			var held map[string]any
			if err := json.Unmarshal(b, &held); err != nil {
				t.Fatalf("%s: %v", conformance.MetadataName, err)
			}

			held["status"] = string(conformance.Provisional)

			if b, err = json.Marshal(held); err != nil {
				t.Fatalf("%s: %v", conformance.MetadataName, err)
			}
		}

		if err := os.WriteFile(filepath.Join(dir, item.Name()), b, 0o600); err != nil {
			t.Fatalf("%v", err)
		}
	}

	return dir
}

// wrong is the entry's own answer with one named item changed, which is a
// values document the adapter can be scripted with and the entry disagrees
// with.
//
// The entry's document is the starting point rather than an invented one, so
// that what the comparison reports is the one item this changed and not the
// shape of a record somebody wrote in a test.
func wrong(t *testing.T, entry *conformance.Entry, item string) []byte {
	t.Helper()

	held, ok := entry.Values.Records[0].Value.(map[string]any)
	if !ok {
		t.Fatalf("%s: the first record is not a group", entry.Name)
	}

	if _, carries := held[item]; !carries {
		t.Fatalf("%s: the first record carries no item called %s", entry.Name, item)
	}

	changed := maps.Clone(held)
	changed[item] = "99999"

	return marshalled(t, &conformance.Values{
		Records: []conformance.Record{{Name: entry.Values.Records[0].Name, Value: changed}},
	})
}
