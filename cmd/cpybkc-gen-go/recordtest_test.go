// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/irpb"
)

// TestTheRecordTierIsWrittenForEveryDescriptorCarryingARecord is the emission
// rule, which is the whole of what decides whether this file is written.
//
// There is no `tests=` option and nothing else turns it off: the record tier is
// written exactly when records.go is, because an adopter who has to discover a
// flag is an adopter who never gets the spot-check the literal exists to be.
// See README.md, "Decided: the test files are written unconditionally".
func TestTheRecordTierIsWrittenForEveryDescriptorCarryingARecord(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	if err := generate(ordersDescriptor(), out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	files := written(t, out)

	if _, ok := files[recordsTestFile]; !ok {
		t.Fatalf("no %s was written for a descriptor carrying five records", recordsTestFile)
	}

	if _, ok := files[recordsFile]; !ok {
		t.Errorf("%s was written and %s was not, and the two go together", recordsTestFile, recordsFile)
	}
}

// TestADescriptorCarryingNoRecordWritesNoRecordTier is the other half of that
// rule: a tier with no record to make a case out of has nothing to write, and a
// file holding a package clause and no case says nothing doc.go does not.
func TestADescriptorCarryingNoRecordWritesNoRecordTier(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	empty := &irpb.Descriptor{Version: supportedIRVersion}

	if err := generate(empty, out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	if _, ok := written(t, out)[recordsTestFile]; ok {
		t.Errorf("%s was written for a descriptor carrying no record node", recordsTestFile)
	}
}

// TestADescriptorCarryingARecordAndNoImportPathIsRefused is the one thing this
// generator cannot derive and will not guess.
//
// The generated tests are an external test package and reach the package beside
// them by importing it, by path. `--out` is a scratch directory cpybkc creates
// and discards, so it names neither the module nor where the files end up.
// Refused rather than answered with a guess, and refused rather than answered by
// quietly not writing the tier: a silent skip is the switch README.md says this
// generator does not have.
func TestADescriptorCarryingARecordAndNoImportPathIsRefused(t *testing.T) {
	t.Parallel()

	err := generate(ordersDescriptor(), t.TempDir(), options{packageName: goldenPackage})
	if err == nil {
		t.Fatalf("a descriptor carrying a record generated with no %s", importPathOption)
	}

	if !strings.Contains(err.Error(), importPathOption) {
		t.Errorf("the refusal does not name the option to set: %v", err)
	}
}

// TestTheGeneratedCasesCarryNoHelperVariableOrFixture is #263's style rule, held
// to by the parser rather than by a reading.
//
// No package-level variable, no helper function, no shared fixture and no
// testdata: a generated file amortises nothing, because a machine writes every
// case and nobody edits any of them, so what is left of a helper is its cost —
// paid by whoever reads a failure they did not write, one jump away from the
// line that failed and the bytes that caused it.
func TestTheGeneratedCasesCarryNoHelperVariableOrFixture(t *testing.T) {
	t.Parallel()

	for _, dir := range append(golden(), goldenDir) {
		source, ok := written(t, dir)[recordsTestFile]
		if !ok {
			continue
		}

		if strings.Contains(source, "testdata") {
			t.Errorf("%s/%s names testdata, and a case's bytes are inline", dir, recordsTestFile)
		}

		file, err := parser.ParseFile(token.NewFileSet(), recordsTestFile, source, 0)
		if err != nil {
			t.Fatalf("%s/%s is not Go: %v", dir, recordsTestFile, err)
		}

		for _, decl := range file.Decls {
			switch node := decl.(type) {
			case *ast.GenDecl:
				if node.Tok != token.IMPORT {
					t.Errorf("%s/%s declares a %s at package level", dir, recordsTestFile, node.Tok)
				}
			case *ast.FuncDecl:
				if node.Recv != nil || !strings.HasPrefix(node.Name.Name, "Test") {
					t.Errorf("%s/%s declares %s, which is not a case", dir, recordsTestFile, node.Name.Name)
				}
			}
		}
	}
}

// TestEveryRecordAndEveryVariantArmGetsACase is the coverage criterion.
//
// One case per record type and one per arm: an arm is a discriminator path, and
// a discriminator no case covers is one whose spelling an adopter finds out
// about from a production file. The count is one per record plus one per arm
// beyond the first, because the record's own case is what takes the first.
func TestEveryRecordAndEveryVariantArmGetsACase(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	if err := generate(ordersDescriptor(), out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	source := written(t, out)[recordsTestFile]

	// Five records, one of which — ENTRY-RECORD — carries an alternation of two
	// arms.
	for _, name := range []string{
		"func TestOrderRecordReadsBackTheBytesItWasReadFrom(",
		"func TestTrailerRecordReadsBackTheBytesItWasReadFrom(",
		"func TestSyncRecordReadsBackTheBytesItWasReadFrom(",
		"func TestTableRecordReadsBackTheBytesItWasReadFrom(",
		"func TestEntryRecordReadsBackTheBytesItWasReadFrom(",
		"func TestEntryRecordHoldingEntrySummaryReadsBackTheBytesItWasReadFrom(",
	} {
		if !strings.Contains(source, name) {
			t.Errorf("no case is named %s", strings.TrimSuffix(strings.TrimPrefix(name, "func "), "("))
		}
	}

	if got, want := strings.Count(source, "\nfunc Test"), 6; got != want {
		t.Errorf("the record tier carries %d cases, and the descriptor asks for %d", got, want)
	}
}

// TestACaseSelectingAnArmHoldsTheLiteralThatArmsPredicateRequires is the
// generation-time predicate inversion, seen from the outside.
//
// The two cases over ENTRY-RECORD differ in the byte ENTRY-TYPE holds, and each
// holds the byte its own arm's predicate tests for. Nothing emitted inverts a
// predicate — docs/ir/SPEC.md's "A writer evaluates a predicate, it never
// inverts one" binds a writer against a record its caller built, and there is no
// caller here — and these are the bytes that make the emitted writer's check
// pass rather than fail.
func TestACaseSelectingAnArmHoldsTheLiteralThatArmsPredicateRequires(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	if err := generate(ordersDescriptor(), out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	source := written(t, out)[recordsTestFile]

	first, second, cut := strings.Cut(source, "func TestEntryRecordHoldingEntrySummaryReadsBackTheBytesItWasReadFrom(")
	if !cut {
		t.Fatalf("the descriptor's second arm has no case")
	}

	if !strings.Contains(first, `record.Entry[0].EntryType != "D"`) {
		t.Errorf("the case for the first arm does not hold ENTRY-TYPE at the literal that arm's predicate requires")
	}

	if !strings.Contains(second, `record.Entry[0].EntryType != "S"`) {
		t.Errorf("the case for the second arm does not hold ENTRY-TYPE at the literal that arm's predicate requires")
	}
}

// TestTheRecordTierIsDeterministic is docs/plugin/SPEC.md's "Determinism" over
// the one file of this generator's output whose values are chosen rather than
// copied.
//
// Nothing in a chosen value comes from the clock, the environment or a path:
// every one of them is a function of the item's own picture and its position in
// the record, which is why the rule is a rule and not a table of numbers.
func TestTheRecordTierIsDeterministic(t *testing.T) {
	t.Parallel()

	first, second := t.TempDir(), t.TempDir()

	for _, out := range []string{first, second} {
		if err := generate(ordersDescriptor(), out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
			t.Fatalf("generate: %v", err)
		}
	}

	if one, two := written(t, first)[recordsTestFile], written(t, second)[recordsTestFile]; one != two {
		t.Errorf("two runs over one descriptor wrote different record tiers\n got:\n%s\nwant:\n%s", two, one)
	}
}

// TestAnAsciiDescriptorSpellsItsBytesAsAReadableString is half of the
// charset-aware spelling, and TestAnEbcdicDescriptorSpellsItsBytesAsHex is the
// other.
//
// The rule keys on the descriptor's charset rather than on each item's kind,
// because a case's bytes are one literal and a literal has one spelling. For an
// ASCII file the string is readable the way the file is; for an EBCDIC one the
// slice is the honest spelling and pastes into `hexdump` beside the dataset.
func TestAnAsciiDescriptorSpellsItsBytesAsAReadableString(t *testing.T) {
	t.Parallel()

	source := spelling(t, irpb.Charset_CHARSET_ASCII)

	if !strings.Contains(source, "in := []byte(\"AAAAA\" + // LINE-TEXT @0 X(5)\n\t\t\"FF\") // LINE-CODE @5 X(2)") {
		t.Errorf("an ASCII descriptor did not spell its bytes as a string:\n%s", source)
	}

	if strings.Contains(source, "in := []byte{") {
		t.Errorf("an ASCII descriptor spelled its bytes as hex:\n%s", source)
	}
}

func TestAnEbcdicDescriptorSpellsItsBytesAsHex(t *testing.T) {
	t.Parallel()

	source := spelling(t, irpb.Charset_CHARSET_CP037)

	if !strings.Contains(source, "0xc1, 0xc1, 0xc1, 0xc1, 0xc1, // LINE-TEXT @0 X(5)") {
		t.Errorf("an EBCDIC descriptor did not spell its bytes as annotated hex:\n%s", source)
	}

	if strings.Contains(source, `in := []byte("`) {
		t.Errorf("an EBCDIC descriptor spelled its bytes as a string:\n%s", source)
	}
}

// spelling is the record tier of a one-item descriptor read under charset.
func spelling(t *testing.T, charset irpb.Charset) string {
	t.Helper()

	text, code := alphanumeric(3, "LINE-TEXT", 5), alphanumeric(4, "LINE-CODE", 2)

	text.GetField().GetEncoding().Charset = charset
	code.GetField().GetEncoding().Charset = charset

	d := &irpb.Descriptor{Version: supportedIRVersion, Nodes: []*irpb.Node{
		record(1, "LINE-RECORD", 2),
		group(2, "LINE-RECORD", nil, 3, 4),
		text,
		code,
	}}

	out := t.TempDir()

	if err := generate(d, out, options{packageName: "line", importPath: "example.com/line"}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	return written(t, out)[recordsTestFile]
}

// TestEveryItemShapeThisGeneratorEmitsIsSynthesized is the criterion the
// goldens cannot state on their own.
//
// Between them the six pin alphanumeric, zoned DISPLAY, COMP-3, binary COMP,
// COMP-1, INDEX, a numeric-edited item, a fixed OCCURS, an OCCURS DEPENDING ON,
// a variant and its arms, a slack node and an item the copybook gives no
// data-name — and the compiler runs those cases, so those shapes are checked by
// bytes rather than by a reading. The three left over are here, and the
// assertion is the strong one available without a second module: the literal is
// exactly as wide as the sum of the widths the descriptor states, which is the
// arithmetic a wrong accessor or a missed item would break.
func TestEveryItemShapeThisGeneratorEmitsIsSynthesized(t *testing.T) {
	t.Parallel()

	comp2 := numericItem(4, "RATE", irpb.Usage_USAGE_COMP_2, 8, 0, 0, false)
	comp2.GetField().Picture = nil

	pointer := &irpb.Node{Id: 7, Kind: &irpb.Node_Field{Field: &irpb.Field{
		Width: 4, Encoding: resolvedEncoding(), Usage: irpb.Usage_USAGE_POINTER,
		Names: &irpb.Names{Original: "ANCHOR"},
	}}}

	national := &irpb.Node{Id: 8, Kind: &irpb.Node_Field{Field: &irpb.Field{
		Width: 6, Encoding: resolvedEncoding(), Usage: irpb.Usage_USAGE_NATIONAL,
		Names: &irpb.Names{Original: "WIDE-TEXT"},
	}}}

	d := &irpb.Descriptor{Version: supportedIRVersion, Nodes: []*irpb.Node{
		record(1, "SHAPE-RECORD", 2),
		group(2, "SHAPE-RECORD", nil, 3, 4, 5, 6, 7, 8),
		comp6(3, "TALLY", 3, 5, 0),
		comp2,
		comp5(5, "WIDE-COUNT", 8, 18, true),
		comp5(6, "UNSIGNED-COUNT", 2, 4, false),
		pointer,
		national,
	}}

	out := t.TempDir()

	if err := generate(d, out, options{packageName: "shape", importPath: "example.com/shape"}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	source := written(t, out)[recordsTestFile]

	// One assertion per item, and one comment column naming it.
	for _, item := range []string{"TALLY", "RATE", "WIDE-COUNT", "UNSIGNED-COUNT", "ANCHOR", "WIDE-TEXT"} {
		if !strings.Contains(source, item+" @") {
			t.Errorf("no run of the literal is annotated with %s", item)
		}

		if !strings.Contains(source, `t.Errorf("`+item+`: got`) {
			t.Errorf("no assertion names %s", item)
		}
	}

	_, body, cut := strings.Cut(source, "in := []byte{")
	if !cut {
		t.Fatalf("the case carries no literal:\n%s", source)
	}

	literal, _, _ := strings.Cut(body, "\n\t}")

	// 3 + 8 + 8 + 2 + 4 + 6, which is what the descriptor states and what a
	// wrong accessor would not produce.
	if got, want := strings.Count(literal, "0x"), 31; got != want {
		t.Errorf("the literal is %d bytes wide and the descriptor states %d:\n%s", got, want, source)
	}
}

// golden is the directories [machineGoldens] names, sorted so that a failure
// reads the same way twice.
func golden() []string {
	dirs := make([]string, 0, len(machineGoldens))
	for dir := range machineGoldens {
		dirs = append(dirs, dir)
	}

	return dirs
}
