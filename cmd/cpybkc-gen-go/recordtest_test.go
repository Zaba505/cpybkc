// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"slices"
	"strconv"
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

	if err := generate(io.Discard, ordersDescriptor(), out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
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

	if err := generate(io.Discard, empty, out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
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

	err := generate(io.Discard, ordersDescriptor(), t.TempDir(), options{packageName: goldenPackage})
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

	if err := generate(io.Discard, ordersDescriptor(), out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	source := written(t, out)[recordsTestFile]

	// Six records, one of which — ENTRY-RECORD — carries an alternation of two
	// arms, and one of which — SHAPE-RECORD — no transition admits.
	for _, name := range []string{
		"func TestOrderRecordReadsBackTheBytesItWasReadFrom(",
		"func TestTrailerRecordReadsBackTheBytesItWasReadFrom(",
		"func TestSyncRecordReadsBackTheBytesItWasReadFrom(",
		"func TestTableRecordReadsBackTheBytesItWasReadFrom(",
		"func TestEntryRecordReadsBackTheBytesItWasReadFrom(",
		"func TestEntryRecordHoldingEntrySummaryReadsBackTheBytesItWasReadFrom(",
		"func TestShapeRecordReadsBackTheBytesItWasReadFrom(",
	} {
		if !strings.Contains(source, name) {
			t.Errorf("no case is named %s", strings.TrimSuffix(strings.TrimPrefix(name, "func "), "("))
		}
	}

	if got, want := strings.Count(source, "\nfunc Test"), 7; got != want {
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

	if err := generate(io.Discard, ordersDescriptor(), out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
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
		if err := generate(io.Discard, ordersDescriptor(), out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
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

	if !strings.Contains(source, "in := []byte(\"AAAAA\" + // LINE-TEXT @0 X(5)\n\t\t\"FF\" + // LINE-CODE @5 X(2)") {
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

// TestAnAsciiDescriptorEscapesTheBytesThatAreNotCharacters is the half of the
// charset-aware spelling a mixed record turns on.
//
// A case's bytes are one literal and a literal has one spelling, so a `COMP`
// item inside an ASCII record reads as escapes inside the string rather than
// moving the whole record to hex. A record whose *charset* is ASCII is a record
// whose readable items read; the items that are not characters under any charset
// are the ones a reader was going to check against a hex dump either way.
func TestAnAsciiDescriptorEscapesTheBytesThatAreNotCharacters(t *testing.T) {
	t.Parallel()

	source := spelling(t, irpb.Charset_CHARSET_ASCII)

	if !strings.Contains(source, `"\xff\xf8") // LINE-COUNT @7 S9(4) BINARY`) {
		t.Errorf("an item that is not characters under any charset did not read as escapes:\n%s", source)
	}

	if strings.Contains(source, "in := []byte{") {
		t.Errorf("a mixed record moved the whole literal to hex:\n%s", source)
	}
}

// TestACaseForAnItemNoCharsetGovernsIsMadeOfBytesNoCharsetSurvives is what
// makes the generated case an assertion rather than a coincidence.
//
// A case whose payload happened to be printable ASCII would pass whether the
// item went through ReadBytes or ReadAlphanumeric, because a run of letters
// survives a code page and a trim unchanged. So the synthesized value carries
// the things that do not: bytes above 0x7F a charset table translates, a 0x20
// the trailing-space trim deletes, and a 0x00 no run of characters would hold.
//
// Pinned at the narrow widths as well as at six. A one-byte `PIC X` status flag
// is the item this charset was added for, and a run that only makes its claims
// from six bytes up makes none of them there: a case for such an item would be
// a run a code page leaves alone, or worse, the zeros a writer emits for a
// record carrying no value, which no case could tell a written value from.
func TestACaseForAnItemNoCharsetGovernsIsMadeOfBytesNoCharsetSurvives(t *testing.T) {
	t.Parallel()

	// The widths each claim first has room for: every width holds a byte a
	// charset would translate, two bytes hold the space as well, and three hold
	// all three.
	for _, width := range []int{1, 2, 6} {
		body := payloadValue(0, width)

		if len(body) != width {
			t.Fatalf("the payload for a %d-byte item is %d bytes", width, len(body))
		}

		if bytes.Equal(body, make([]byte, width)) {
			t.Errorf("the payload for a %d-byte item is the zero fill a writer emits for a record that carries no value", width)
		}

		high := false

		for _, b := range body {
			if b > 0x7F {
				high = true
			}
		}

		if !high {
			t.Errorf("every byte of the payload % x is below 0x80, so a charset table would leave it alone", body)
		}

		for _, claim := range []struct {
			byte  byte
			from  int
			about string
		}{
			{byte: 0x20, from: 2, about: "the byte the trailing-space trim deletes"},
			{byte: 0x00, from: 3, about: "the byte no run of characters would hold"},
		} {
			if width < claim.from || bytes.Contains(body, []byte{claim.byte}) {
				continue
			}

			t.Errorf("the payload % x is %d bytes and carries no 0x%02x, which is %s", body, width, claim.byte, claim.about)
		}
	}

	// And the case that carries it. The literal is the bytes the item writes
	// rather than bytes this test believes it writes: [synth.write] drives the
	// same codec call the generated encoder will.
	d := &irpb.Descriptor{Version: supportedIRVersion, Nodes: []*irpb.Node{
		record(1, "LINE-RECORD", 2),
		group(2, "LINE-RECORD", nil, 3, 4),
		alphanumeric(3, "LINE-TEXT", 5),
		opaque(4, "LINE-FLAGS", 6),
	}}

	out := t.TempDir()

	if err := generate(io.Discard, d, out, options{packageName: "line", importPath: "example.com/line"}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	source := written(t, out)[recordsTestFile]

	// Laid down after a five-byte text item, so the payload is the one for
	// offset 5 rather than the one checked above.
	want := "if want := " + byteSlice(payloadValue(5, 6)) + "; !bytes.Equal(record.LineFlags, want)"
	if !strings.Contains(source, want) {
		t.Errorf("%s does not assert the payload as %s:\n%s", recordsTestFile, want, source)
	}
}

// spelling is the record tier of a three-item descriptor read under charset.
//
// Three items rather than one: the concatenation is what carries the comment
// column in the string spelling, and the binary item is the run that is not
// characters under any charset.
func spelling(t *testing.T, charset irpb.Charset) string {
	t.Helper()

	items := []*irpb.Node{
		alphanumeric(3, "LINE-TEXT", 5),
		alphanumeric(4, "LINE-CODE", 2),
		binary(5, "LINE-COUNT", 2, 4, true),
	}

	for _, item := range items {
		item.GetField().GetEncoding().Charset = charset
	}

	d := &irpb.Descriptor{Version: supportedIRVersion, Nodes: append([]*irpb.Node{
		record(1, "LINE-RECORD", 2),
		group(2, "LINE-RECORD", nil, 3, 4, 5),
	}, items...)}

	out := t.TempDir()

	if err := generate(io.Discard, d, out, options{packageName: "line", importPath: "example.com/line"}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	return written(t, out)[recordsTestFile]
}

// TestAPackageNamedAfterOneOfTheCasesOwnIdentifiersStillCompiles is the shape a
// generated package's name being the adopter's makes possible.
//
// `package bytes`, `package testing`, `package codec` and `package big` are all
// names this file already spends — on an import, or on a local a case declares —
// and every one of them is a package this generator has to be able to write
// tests for rather than one it may refuse. Where the name is spent the import
// takes an alias, and the parser is what says the result is Go.
func TestAPackageNamedAfterOneOfTheCasesOwnIdentifiersStillCompiles(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"bytes", "testing", "codec", "big", "record", "in", "out"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			d := &irpb.Descriptor{Version: supportedIRVersion, Nodes: []*irpb.Node{
				record(1, "LINE-RECORD", 2),
				group(2, "LINE-RECORD", nil, 3),
				alphanumeric(3, "LINE-TEXT", 5),
			}}

			// Both spellings of the path: one whose last element is the package's
			// own name, which is what an adopter's tree usually looks like, and one
			// whose is not.
			for _, path := range []string{"example.com/x/" + name, "example.com/x/gen"} {
				out := t.TempDir()

				if err := generate(io.Discard, d, out, options{packageName: name, importPath: path}); err != nil {
					t.Fatalf("generate into %s: %v", path, err)
				}

				source := written(t, out)[recordsTestFile]

				if _, err := parser.ParseFile(token.NewFileSet(), recordsTestFile, source, 0); err != nil {
					t.Fatalf("the record tier of package %s at %s is not Go: %v\n%s", name, path, err, source)
				}

				if strings.Count(source, strconv.Quote(path)) != 1 {
					t.Errorf("package %s at %s does not import itself exactly once:\n%s", name, path, source)
				}
			}
		})
	}
}

// TestEveryItemShapeThisGeneratorEmitsIsSynthesized names the shapes the record
// tier has to lay bytes down for, and where each of them is checked.
//
// This is a listing rather than the check itself, and that is the point: every
// one of these is in a golden package, so the compiler sees the Go type the
// emitted assertion gives it and `go test -race` runs the round trip over the
// bytes. A test asserting only that the source *mentions* an item would pass a
// wrong Go type and a value the item cannot hold; the goldens do not.
func TestEveryItemShapeThisGeneratorEmitsIsSynthesized(t *testing.T) {
	t.Parallel()

	// Every item [emitter.fieldType] has a row for, and the two shapes that are
	// not items at all, against the record of internal/orders that carries one.
	shapes := map[string]string{
		"alphanumeric DISPLAY":   "CUSTOMER-NAME",
		"numeric DISPLAY":        "ORDER-ID",
		"numeric-edited":         "PRINTED-TOTAL(1)",
		"PACKED-DECIMAL":         "ORDER-TOTAL",
		"PACKED-DECIMAL, big":    "GRAND-TOTAL",
		"COMP-6":                 "TALLY",
		"BINARY":                 "QUANTITY",
		"COMP-5":                 "WIDE-COUNT",
		"COMP-5, unsigned":       "UNSIGNED-COUNT",
		"COMP-1":                 "EXCHANGE-RATE",
		"COMP-2":                 "RATE",
		"INDEX":                  "TABLE-INDEX",
		"POINTER":                "ANCHOR",
		"NATIONAL":               "WIDE-TEXT",
		"a fixed OCCURS":         "LINE-ITEM(1).SKU",
		"an OCCURS DEPENDING ON": "DETAIL(1).DETAIL-TEXT",
		"a variant arm":          "ENTRY(1).ENTRY-DETAIL.DETAIL-SKU",
		"a nameless item":        "FILLER",
		"a slack node":           "(slack)",
	}

	source, ok := written(t, goldenDir)[recordsTestFile]
	if !ok {
		t.Fatalf("%s carries no %s", goldenDir, recordsTestFile)
	}

	for shape, item := range shapes {
		if !strings.Contains(source, item+" @") {
			t.Errorf("no run of a case's literal is annotated with %s, which is where %s is synthesized", item, shape)
		}
	}
}

// golden is the directories [machineGoldens] names, sorted so that a failure
// reads the same way twice.
func golden() []string {
	dirs := make([]string, 0, len(machineGoldens))
	for dir := range machineGoldens {
		dirs = append(dirs, dir)
	}

	slices.Sort(dirs)

	return dirs
}

// TestATableInsideAnArmIsLaidOutAgainstTheCountTheDecoderReads is the second of
// the two ways a case's bytes could disagree with the walk that reads them.
//
// An occurrence holding a variant is read whole before any of it is decoded, and
// the width of that read is summed from the **first** arm whichever arm the
// occurrence turns out to hold. So a count feeding a table inside an arm governs
// the layout of every case over that record — and a synthesizer that only
// counted the tables outside a variant would lay down an occurrence of the wrong
// length and fail on `UnmarshalCOBOL` with a message naming neither cause.
func TestATableInsideAnArmIsLaidOutAgainstTheCountTheDecoderReads(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{Version: supportedIRVersion, Nodes: []*irpb.Node{
		record(1, "SEG-RECORD", 2),
		group(2, "SEG-RECORD", nil, 3, 4),
		zoned(3, "N-COUNT", 1, 1, 0, false),
		group(4, "SEG", constant(2), 5, 6),
		alphanumeric(5, "SEG-TYPE", 1),
		variant(6, armOf(7, 9), armOf(8, 11)),
		equals(7, 5, "\xc1"),
		equals(8, 5, "\xc2"),
		group(9, "SEG-A", nil, 10),
		group(10, "ITEM", depending(3, 0, 2), 13),
		group(11, "SEG-B", nil, 12),
		alphanumeric(12, "SEG-B-TEXT", 2),
		alphanumeric(13, "ITEM-TEXT", 2),
	}}

	out := t.TempDir()

	if err := generate(io.Discard, d, out, options{packageName: "seg", importPath: "example.com/seg"}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	source := written(t, out)[recordsTestFile]

	// One occurrence of ITEM, so N-COUNT holds 1 — and every case over this
	// record holds the same 1, because the occurrence's width is summed from
	// SEG-A whichever arm the case selects.
	if got := strings.Count(source, `t.Errorf("N-COUNT: got %d, want %d", record.NCount, 1)`); got != 2 {
		t.Errorf("N-COUNT is asserted against 1 in %d of the 2 cases:\n%s", got, source)
	}

	if !strings.Contains(source, "if len(record.Seg[0].SegA.Item) != 1 {") {
		t.Errorf("the table inside the arm is not laid out with the occurrence its count states:\n%s", source)
	}
}

// TestACountFieldADiscriminatorPinsIsLaidOutAtTheLiteralsOwnNumber is the first
// of them.
//
// A predicate states the bytes a field holds outright, and the generated decoder
// reads that field's number of occurrences out of those bytes. So where one
// field is both a discriminator and an OCCURS DEPENDING ON count, the literal's
// own number is the number of occurrences the case is laid out with — a case
// that chose its own would be a case whose literal it cannot read back.
func TestACountFieldADiscriminatorPinsIsLaidOutAtTheLiteralsOwnNumber(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{Version: supportedIRVersion, Nodes: []*irpb.Node{
		{Id: 1, Kind: &irpb.Node_File{File: &irpb.File{
			Framing:      &irpb.File_DescriptorWord{DescriptorWord: &irpb.DescriptorWord{}},
			StartStateId: 2,
		}}},
		{Id: 2, Kind: &irpb.Node_State{State: &irpb.State{TransitionIds: []uint64{3}}}},
		{Id: 4, Kind: &irpb.Node_State{State: &irpb.State{Accepts: true}}},
		{Id: 3, Kind: &irpb.Node_Transition{Transition: &irpb.Transition{
			RecordId: 5, NextStateId: 4, PredicateId: new(uint64(9)),
		}}},

		// The discriminator is the count: three occurrences, spelled as the
		// EBCDIC digit the predicate tests for.
		equals(9, 7, "\xf3"),

		record(5, "RUN-RECORD", 6),
		group(6, "RUN-RECORD", nil, 7, 8),
		zoned(7, "RUN-COUNT", 1, 1, 0, false),
		group(8, "RUN", depending(7, 0, 5), 10),
		alphanumeric(10, "RUN-TEXT", 2),
	}}

	out := t.TempDir()

	if err := generate(io.Discard, d, out, options{packageName: "run", importPath: "example.com/run"}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	source := written(t, out)[recordsTestFile]

	if !strings.Contains(source, `t.Errorf("RUN-COUNT: got %d, want %d", record.RunCount, 3)`) {
		t.Errorf("RUN-COUNT is not asserted against the number its predicate pins it to:\n%s", source)
	}

	if !strings.Contains(source, "if len(record.Run) != 3 {") {
		t.Errorf("the table is not laid out with the occurrences its discriminated count states:\n%s", source)
	}
}
