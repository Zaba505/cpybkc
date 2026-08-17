// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/irpb"
)

// The four cases #266 owed an answer, each with the answer it got.
//
//  1. **No finite accepting path.** The automaton offers no walk from its start
//     state to a state that accepts. Warned about; see
//     [TestAnAutomatonThatNeverAcceptsIsWarnedAboutRatherThanRefused].
//  2. **A charset codec ships no table for.** The one hard error, because the
//     *generated code* could not read that file either; see
//     [TestAnUnsupportedCharsetStillFailsTheGeneration].
//  3. **An item codec cannot lay out.** Warned about, naming the item; see
//     [TestAnItemTheSynthesizerCannotLayOutCostsItsRecordsCaseAndNothingElse].
//  4. **A count that cannot be satisfied.** Warned about, naming the count and
//     the record it sizes; see
//     [TestACountNoNumberSatisfiesCostsTheCasesThatNeedIt].
//
// README.md, "When a descriptor admits no checkable file", is where the argument
// is. What every one of these asserts beyond the diagnostic is the same thing:
// the four files that are the package are still there, and `run` still succeeds.

// TestAnAutomatonThatNeverAcceptsIsWarnedAboutRatherThanRefused is case 1.
//
// A descriptor whose automaton offers no path to an accepting state has no file
// to show, so there is nothing for the file tier to write. It is not a
// generation to refuse: the reader and the writer are emitted from the same
// automaton and are exactly what they would have been.
//
// The shape is not one `resolve` produces — see [filetest.unreachable] for why —
// and it is checked because nothing screens a descriptor in front of a plugin.
func TestAnAutomatonThatNeverAcceptsIsWarnedAboutRatherThanRefused(t *testing.T) {
	t.Parallel()

	d := fixedDescriptor()

	for _, node := range d.GetNodes() {
		if node.GetState() != nil {
			node.GetState().Accepts = false
		}
	}

	out, stderr := generated(t, d, options{packageName: "ledger", importPath: "example.com/ledger"})

	// The package is whole. That is the half of this decision an adopter cares
	// about: they asked for a reader and a writer and they have one.
	if _, ok := out[fileMachineFile]; !ok {
		t.Errorf("%s was not written for a descriptor whose automaton never accepts", fileMachineFile)
	}

	if _, ok := out[fileTestFile]; ok {
		t.Errorf("a file tier was written for an automaton no file walks:\n%s", out[fileTestFile])
	}

	saidAbout(t, stderr, 1, "the automaton", "offers no path from its start state to a state that accepts")
}

// TestAnUnsupportedCharsetStillFailsTheGeneration is case 2, and the one case
// the recommendation does not reach.
//
// A charset cobol-go's codec ships no table for is a charset the *generated
// code* cannot read the file in either, so there is nothing to keep: the
// generation fails, as it always has, with the diagnostic
// [unsupportedCharsetError] already composed for it. What this pins is that it
// is that one refusal and not a second one saying the same thing differently —
// the synthesizer has its own charset table, and two refusals over one fact is
// how an adopter ends up reading both and believing they are about two.
func TestAnUnsupportedCharsetStillFailsTheGeneration(t *testing.T) {
	t.Parallel()

	d := countedDescriptor()

	for _, node := range d.GetNodes() {
		if field := node.GetField(); field != nil {
			field.GetEncoding().Charset = irpb.Charset_CHARSET_CP500
		}
	}

	out := t.TempDir()

	var stderr bytes.Buffer

	err := generate(&stderr, d, out, options{packageName: goldenPackage, importPath: goldenImport})

	var refusal *unsupportedCharsetError
	if !errors.As(err, &refusal) {
		t.Fatalf("generate returned %v, want the charset refusal", err)
	}

	if refusal.Charset != irpb.Charset_CHARSET_CP500 {
		t.Errorf("the refusal is about %v, want cp500", refusal.Charset)
	}

	// Refused means nothing written and nothing warned about: a warning claims a
	// package was generated anyway, and none was.
	if entries, err := os.ReadDir(out); err != nil {
		t.Fatalf("reading the output directory: %v", err)
	} else if len(entries) != 0 {
		t.Errorf("the charset refusal left %d files beneath --out, want none", len(entries))
	}

	if stderr.Len() != 0 {
		t.Errorf("the charset refusal wrote a warning as well:\n%s", stderr.String())
	}
}

// TestAnItemTheSynthesizerCannotLayOutCostsItsRecordsCaseAndNothingElse is case
// 3, and with it the partial-coverage decision.
//
// Every usage the IR carries is one the synthesizer lays out today — COMP-1 and
// COMP-2 through codec's float accessors, INDEX, POINTER and NATIONAL as the
// bytes the IR says they are, and the rest through the same table the emitted
// accessor is chosen from. An item this generator does not know is refused by
// [emitter.fieldType], which the struct emitter reads first, so it never reaches
// a synthesizer at all. What is left for case 3 is an item whose *value* cannot
// be synthesized, and the reachable one is a discriminator literal that is not
// the width of the field it tests.
//
// The record it selects loses its case. Its siblings keep theirs — a record type
// this generator cannot lay out says nothing about the one beside it — and the
// file that is written names the one that is missing.
func TestAnItemTheSynthesizerCannotLayOutCostsItsRecordsCaseAndNothingElse(t *testing.T) {
	t.Parallel()

	// SUMMARY-RECORD is selected on TYPE-CODE, which is one byte wide, against a
	// literal of two.
	d := &irpb.Descriptor{Version: supportedIRVersion, Nodes: withNodes(func(nodes []*irpb.Node) []*irpb.Node {
		for _, node := range nodes {
			if node.GetId() == 52 {
				node.GetPredicate().GetBytesEqual().Value = []byte("\xe2\xe2")
			}
		}

		return nodes
	})}

	out, stderr := generated(t, d, options{packageName: goldenPackage, importPath: goldenImport})

	tests, ok := out[recordsTestFile]
	if !ok {
		t.Fatalf("no record tier at all was written for a descriptor whose other record types lay out")
	}

	// The siblings keep their cases, which is the whole of the partial-coverage
	// decision: a record type with no case is a warning and not a reason to throw
	// away the five that have one.
	for _, want := range []string{"HeaderRecord", "DetailRecord"} {
		if !strings.Contains(tests, want) {
			t.Errorf("the record tier carries no case for %s, and it lays out:\n%s", want, tests)
		}
	}

	if strings.Contains(tests, "func TestSummaryRecord") {
		t.Errorf("a case was written for the record type this generator cannot lay out:\n%s", tests)
	}

	// The item, by the name the copybook spells: `TYPE-CODE is 1 byte wide` is a
	// line of the copybook to go and read, and *could not generate tests* is not.
	saidAbout(t, stderr, 2, "SUMMARY-RECORD", "TYPE-CODE", "1 byte wide, against a literal of 2")

	// And in the file, where the scrollback cannot lose it.
	if !strings.Contains(tests, "SUMMARY-RECORD") {
		t.Errorf("the record tier does not name the record type it could not cover:\n%s", tests)
	}
}

// TestACountNoNumberSatisfiesCostsTheCasesThatNeedIt is case 4: the shape
// docs/ir/SPEC.md's "One count may size two tables, and a writer refuses to
// choose" is about, met at generation time instead of at run time.
//
// One register sizes LINE and NOTE. Given bounds with no number in common there
// is no path through the automaton that admits the record carrying them, so the
// file tier loses that case and says which count it was about — the register,
// the item that binds it, and the two tables and their bounds.
//
// The record tier keeps its case, and that is not an oversight. A record laid
// out on its own has no automaton in front of it and no register bound, so each
// table is sized from its own declared minimum; what cannot be reconciled is a
// *file*, which is exactly the tier that refuses. A count contradicting itself
// inside one record is [synth.chooseCounts]'s refusal and costs that record's
// case instead.
func TestACountNoNumberSatisfiesCostsTheCasesThatNeedIt(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{Version: supportedIRVersion, Nodes: withNodes(func(nodes []*irpb.Node) []*irpb.Node {
		for _, node := range nodes {
			if node.GetId() == 124 {
				node.GetGroup().Repetition = counted(22, 5, 9)
			}
		}

		return nodes
	})}

	out, stderr := generated(t, d, options{packageName: goldenPackage, importPath: goldenImport})

	for _, name := range []string{generatedFile, recordsFile, codecFile, fileMachineFile} {
		if _, ok := out[name]; !ok {
			t.Errorf("%s was not written for a descriptor whose count no number satisfies", name)
		}
	}

	saidAbout(t, stderr, 1, "TOTAL-COUNT", "LINE occurs 0 to 4 times", "NOTE occurs 5 to 9 times")
}

// TestTheFourFilesAreWhatTheyWouldHaveBeen is the rule that keeps this decision
// from being a licence to change the output.
//
// The refusal path changes what is *added* — a case, or a whole tier — and never
// what already exists. Held against the emitters rather than against a golden,
// because a golden agrees with whatever was generated last and this is a claim
// about two code paths agreeing.
func TestTheFourFilesAreWhatTheyWouldHaveBeen(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{Version: supportedIRVersion, Nodes: withNodes(func(nodes []*irpb.Node) []*irpb.Node {
		for _, node := range nodes {
			if node.GetId() == 124 {
				node.GetGroup().Repetition = counted(22, 5, 9)
			}
		}

		return nodes
	})}

	opts := options{packageName: goldenPackage, importPath: goldenImport}

	out, _ := generated(t, d, opts)

	structs, err := records(d, opts)
	if err != nil {
		t.Fatalf("records: %v", err)
	}

	methods, err := codecMethods(d, opts)
	if err != nil {
		t.Fatalf("codecMethods: %v", err)
	}

	machine, err := fileMachine(d, opts)
	if err != nil {
		t.Fatalf("fileMachine: %v", err)
	}

	for name, source := range map[string]string{
		recordsFile: structs, codecFile: methods, fileMachineFile: machine,
	} {
		want, err := format.Source([]byte(source))
		if err != nil {
			t.Fatalf("formatting %s: %v", name, err)
		}

		if out[name] != string(want) {
			t.Errorf("%s is not what the emitter produced for this descriptor:\n got:\n%s\nwant:\n%s",
				name, out[name], want)
		}
	}
}

// TestARefusalIsTheSameFilesAndTheSameLinesEveryRun is docs/plugin/SPEC.md's
// determinism, carried through a refusal.
//
// Both halves, because a skipped construct adds a second output nothing else in
// this package pins: the diagnostics. A warning order that came from a map walk
// would be a diff in whatever reads them, and cpybkc reads them.
func TestARefusalIsTheSameFilesAndTheSameLinesEveryRun(t *testing.T) {
	t.Parallel()

	descriptor := func() *irpb.Descriptor {
		return &irpb.Descriptor{Version: supportedIRVersion, Nodes: withNodes(func(nodes []*irpb.Node) []*irpb.Node {
			for _, node := range nodes {
				if node.GetId() == 124 {
					node.GetGroup().Repetition = counted(22, 5, 9)
				}
			}

			return nodes
		})}
	}

	opts := options{packageName: goldenPackage, importPath: goldenImport}

	first, said := generated(t, descriptor(), opts)

	if said == "" {
		t.Fatal("the descriptor this test is built on generated no diagnostics at all")
	}

	for range 10 {
		again, saidAgain := generated(t, descriptor(), opts)

		if saidAgain != said {
			t.Fatalf("two runs wrote different diagnostics\n got:\n%s\nwant:\n%s", saidAgain, said)
		}

		if len(again) != len(first) {
			t.Fatalf("two runs wrote %d and %d files", len(first), len(again))
		}

		for name, source := range first {
			if again[name] != source {
				t.Fatalf("%s differs between two runs of one refusing descriptor", name)
			}
		}
	}
}

// TestTheGoldenDescriptorsSaySomethingOnlyWhenTheyHaveTo keeps the softening
// from spreading.
//
// A refusal that became a warning is a refusal nothing fails on, so a descriptor
// that started skipping a case would generate a smaller directory and pass every
// test that reads it. This is the guard: the descriptors this package pins
// generate in silence, and a warning appearing over one of them is a case that
// stopped being written.
func TestTheGoldenDescriptorsSaySomethingOnlyWhenTheyHaveTo(t *testing.T) {
	t.Parallel()

	for dir, descriptor := range fileTierDescriptors {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()

			name := dir[strings.LastIndex(dir, "/")+1:]

			if _, said := generated(t, descriptor(), options{packageName: name, importPath: goldenModule + dir}); said != "" {
				t.Errorf("%s generated with something to say, and every construct it carries is one this generator covers:\n%s", dir, said)
			}
		})
	}
}

// TestTheWarningsAreCappedAndSayTheyWereCapped is the answer to a copybook whose
// every record type carries the same unsynthesizable item.
//
// The lines go to a terminal beside every other generator cpybkc ran, so the
// list is capped and the cap is announced — a truncated list nobody is told
// about is worse than a long one. The generated file is where the whole list
// lives, and the note says so.
func TestTheWarningsAreCappedAndSayTheyWereCapped(t *testing.T) {
	t.Parallel()

	var skips []skipped

	for at := range reportedSkips + 3 {
		skips = append(skips, skippedRecord(fmt.Sprintf("RECORD-%d", at), fmt.Sprintf("Record%d", at),
			errors.New("it holds an item this generator cannot lay out")))
	}

	var stderr bytes.Buffer

	reportSkips(&stderr, skips)

	written := lines(stderr.String())

	warnings := 0

	for _, line := range written {
		if strings.HasPrefix(line, severityWarning+severitySeparator) {
			warnings++
		}
	}

	// One per construct named in full, and one more saying how many were not.
	if warnings != reportedSkips+1 {
		t.Errorf("%d warnings were written for %d skipped constructs, want %d",
			warnings, len(skips), reportedSkips+1)
	}

	if !strings.Contains(stderr.String(), plural(len(skips)-reportedSkips, "further construct")) {
		t.Errorf("the diagnostics do not say how many constructs were left unnamed:\n%s", stderr.String())
	}

	if !strings.Contains(stderr.String(), refusalSection) {
		t.Errorf("the diagnostics do not say where the whole list is:\n%s", stderr.String())
	}
}

// TestEveryDiagnosticAWarningWritesKeepsTheContractsForm holds
// docs/plugin/SPEC.md's diagnostic form over the severity this program only
// started writing with #266: one line each, a severity from the closed set of
// three opening it, and no line that no severity opens.
func TestEveryDiagnosticAWarningWritesKeepsTheContractsForm(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	warn(&stderr, skippedGoal("the automaton", &uncoverableError{
		What: "the automaton offers no path from its start state to a state that accepts",
		Rule: "a rule\nspanning two lines",
	}))

	written := lines(stderr.String())

	if len(written) == 0 {
		t.Fatal("warn wrote nothing")
	}

	if !strings.HasPrefix(written[0], severityWarning+severitySeparator) {
		t.Errorf("the first line is %q, want a %s diagnostic", written[0], severityWarning)
	}

	for _, line := range written[1:] {
		if !strings.HasPrefix(line, severityNote+severitySeparator) {
			t.Errorf("%q opens with no severity, and every line of the contract's form does", line)
		}
	}
}

// generated is one generation's output directory as text, by file name, and
// everything it said on standard error.
//
// Through [generate] rather than through a tier, because what these tests are
// about is the whole of the answer — which files came out, and what was said
// about the ones that did not.
func generated(t *testing.T, d *irpb.Descriptor, opts options) (map[string]string, string) {
	t.Helper()

	out := t.TempDir()

	var stderr bytes.Buffer

	if err := generate(&stderr, d, out, opts); err != nil {
		t.Fatalf("generate: %v", err)
	}

	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatalf("reading the output directory: %v", err)
	}

	files := make(map[string]string, len(entries))

	for _, entry := range entries {
		b, err := os.ReadFile(filepath.Join(out, entry.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", entry.Name(), err)
		}

		files[entry.Name()] = string(b)
	}

	return files, stderr.String()
}

// saidAbout asserts that the generation wrote exactly want `warning:` lines,
// that between them they name each phrase, and that every other line is a note.
//
// The count is asserted rather than left open because the two tiers refuse
// independently: a construct that costs a record its case and a file its path
// says so twice, once per file that lost something, and a test that counted
// nothing could not tell that from the same fact reported twice for one file.
func saidAbout(t *testing.T, stderr string, want int, phrases ...string) {
	t.Helper()

	var warnings []string

	for _, line := range lines(stderr) {
		switch {
		case strings.HasPrefix(line, severityWarning+severitySeparator):
			warnings = append(warnings, line)
		case strings.HasPrefix(line, severityNote+severitySeparator):
		default:
			t.Errorf("%q opens with no severity, and every line of the contract's form does", line)
		}
	}

	if len(warnings) != want {
		t.Fatalf("%d warnings were written, want %d:\n%s", len(warnings), want, stderr)
	}

	for _, phrase := range phrases {
		if !strings.Contains(stderr, phrase) {
			t.Errorf("the diagnostics do not name %q:\n%s", phrase, stderr)
		}
	}

	// Every warning owes the sentence that says the adopter has a package, which
	// is the half that keeps it from reading as a failure.
	if got := strings.Count(stderr, "generated as usual"); got != want {
		t.Errorf("%d of %d warnings say what the descriptor produced anyway:\n%s", got, want, stderr)
	}
}
