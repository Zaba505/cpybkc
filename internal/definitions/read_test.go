// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package definitions_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/definitions"
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
	"github.com/Zaba505/cpybkc/internal/layoutdoc"
	"github.com/Zaba505/cpybkc/internal/layoutschema"
)

// baseLayout is a layout the schema admits, for a block to be appended to.
const baseLayout = `(encoding
  (charset cp037)
  (sign-convention ebcdic)
  (byte-order big-endian)
  (float-format hfp))
(framing (recfm FB) (lrecl 80))
(record TRANSACTION (copybook "cpy/txn.cpy" TRANSACTION-REC))
(discriminate TRANSACTION single-record-type)
(sequence (* TRANSACTION))
`

// baseLines is how many lines baseLayout takes, so that a block's line can be
// stated beside the file's.
const baseLines = 9

// fileBody parses body as a definitions file, holds it to the schema, and
// returns its entry body.
func fileBody(t *testing.T, body string) []layout.Node {
	t.Helper()

	schema := published(t)

	diagnostics, err := schema.ValidateDefinitions("posting.cpy.sexpr", strings.NewReader(body))
	if err != nil {
		t.Fatalf("validate the definitions file: %v", err)
	}

	for _, diagnostic := range diagnostics {
		t.Fatalf("the schema refuses the definitions file: %s", diagnostic)
	}

	file, err := layout.Parse("posting.cpy.sexpr", strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse the definitions file: %v", err)
	}

	return definitions.FileBody(file)
}

// blockBody holds body as one block in a layout, holds the layout to the
// schema, and returns the block's entry body.
func blockBody(t *testing.T, body string) []layout.Node {
	t.Helper()

	source := baseLayout + "(definitions-block TRAILER\n" + body + ")\n"

	diagnostics, err := published(t).Validate("ledger.sexpr", strings.NewReader(source))
	if err != nil {
		t.Fatalf("validate the layout: %v", err)
	}

	for _, diagnostic := range diagnostics {
		t.Fatalf("the schema refuses the layout: %s", diagnostic)
	}

	file, err := layout.Parse("ledger.sexpr", strings.NewReader(source))
	if err != nil {
		t.Fatalf("parse the layout: %v", err)
	}

	blocks := blocksIn(file)
	if len(blocks) != 1 {
		t.Fatalf("the layout holds %d blocks, want 1", len(blocks))
	}

	return definitions.BlockBody(blocks[0])
}

// blocksIn is every definitions-block a layout holds.
func blocksIn(file *layout.File) []layout.Form {
	var blocks []layout.Form

	for _, form := range file.Forms {
		if form.Tag == layoutschema.BlockForm {
			blocks = append(blocks, form)
		}
	}

	return blocks
}

func published(t *testing.T) *layoutschema.Schema {
	t.Helper()

	schema, err := layoutschema.Published()
	if err != nil {
		t.Fatalf("load the published schema: %v", err)
	}

	return schema
}

// TestABodyReadsIntoAPositionedModel reads every clause form and every operand
// form, and requires each entry, clause and leaf to carry the position it was
// written at — which is what lets a fault found later, by the judge or by
// resolve, point back at the text.
func TestABodyReadsIntoAPositionedModel(t *testing.T) {
	t.Parallel()

	const body = `(entry 01 POSTING-RECORD (global) (external))
(entry 05 PST-TYPE (pic "X") (justified))
(entry 88 PST-IS-DEBIT (value "'A'" (through "'C'" "'F'") SPACE))
(entry 05 PST-COUNT (pic "9(2)") (usage COMP-3) (blank-when-zero))
(entry 05 PST-AMOUNT (pic "S9(7)V99") (sign TRAILING SEPARATE) (synchronized LEFT))
(entry 05 PST-ENTRY (occurs "0" (to "50") (depending-on PST-COUNT) (key ASCENDING PST-KEY) (key DESCENDING PST-KEY) (indexed-by PST-IX)))
(entry 10 PST-KEY (pic "X(4)") (value (word "nil")))
(entry 05 PST-OTHER (redefines PST-TYPE) (pic "X") (sign LEADING) (synchronized))
(entry 66 PST-ALL (renames (through PST-TYPE PST-AMOUNT)))
(entry 66 PST-ONE (renames PST-TYPE))
`

	entries, err := definitions.Read(fileBody(t, body))
	if err != nil {
		t.Fatalf("read:\n%s", diag.Render(err))
	}

	if len(entries) != 10 {
		t.Fatalf("read %d entries, want 10", len(entries))
	}

	if got := entries[2]; got.Level.Value != 88 || got.Name.Name != "PST-IS-DEBIT" || got.Name.Word {
		t.Errorf("entry 3 is level %d %q, want the 88 PST-IS-DEBIT", got.Level.Value, got.Name.Name)
	}

	value, ok := entries[2].Clauses[0].(definitions.Value)
	if !ok || len(value.Operands) != 3 {
		t.Fatalf("entry 3's clause is %#v, want a value of three operands", entries[2].Clauses[0])
	}

	if literal, ok := value.Operands[0].(definitions.Text); !ok || literal.Value != "'A'" {
		t.Errorf("the first operand is %#v, want the literal 'A'", value.Operands[0])
	}

	if through, ok := value.Operands[1].(definitions.Through); !ok || through.From.(definitions.Text).Value != "'C'" || through.To.(definitions.Text).Value != "'F'" {
		t.Errorf("the second operand is %#v, want 'C' through 'F'", value.Operands[1])
	}

	if word, ok := value.Operands[2].(definitions.DataName); !ok || word.Name != "SPACE" {
		t.Errorf("the third operand is %#v, want the word SPACE", value.Operands[2])
	}

	occurs, ok := entries[5].Clauses[0].(definitions.Occurs)
	if !ok {
		t.Fatalf("entry 6's clause is %#v, want an occurs", entries[5].Clauses[0])
	}

	if occurs.Count.Value != "0" || occurs.To == nil || occurs.To.Value != "50" ||
		occurs.DependingOn == nil || occurs.DependingOn.Name != "PST-COUNT" ||
		len(occurs.Keys) != 2 || occurs.Keys[1].Order.Value != "DESCENDING" ||
		occurs.IndexedBy == nil || occurs.IndexedBy.Name != "PST-IX" {
		t.Errorf("the occurs reads as %#v", occurs)
	}

	if sign, ok := entries[4].Clauses[1].(definitions.Sign); !ok || sign.Edge.Value != "TRAILING" || sign.Separate == nil {
		t.Errorf("entry 5's sign reads as %#v, want TRAILING SEPARATE", entries[4].Clauses[1])
	}

	if sign, ok := entries[7].Clauses[2].(definitions.Sign); !ok || sign.Separate != nil {
		t.Errorf("entry 8's sign reads as %#v, want LEADING alone", entries[7].Clauses[2])
	}

	if sync, ok := entries[7].Clauses[3].(definitions.Synchronized); !ok || sync.Direction != nil {
		t.Errorf("entry 8's synchronized reads as %#v, want no direction", entries[7].Clauses[3])
	}

	if word := entries[6].Clauses[1].(definitions.Value).Operands[0].(definitions.DataName); !word.Word || word.Name != "nil" {
		t.Errorf("(word \"nil\") reads as %#v", word)
	}

	if renames, ok := entries[8].Clauses[0].(definitions.Renames); !ok {
		t.Errorf("entry 9's clause is %#v, want a renames", entries[8].Clauses[0])
	} else if _, ok := renames.Renamed.(definitions.Through); !ok {
		t.Errorf("entry 9 renames %#v, want a range", renames.Renamed)
	}

	// Every entry, clause and leaf is where it was written.
	if pos := entries[5].Pos; pos != (layout.Pos{File: "posting.cpy.sexpr", Line: 6, Column: 1}) {
		t.Errorf("entry 6 is at %s, want posting.cpy.sexpr:6:1", pos)
	}

	if pos := occurs.To.Pos; pos.Line != 6 || pos.Column != strings.Index(strings.Split(body, "\n")[5], `"50"`)+1 {
		t.Errorf("the to bound is at %s", pos)
	}

	for _, entry := range entries {
		positioned(t, entry.Pos, entry.Level.Pos, entry.Name.Pos)

		for _, clause := range entry.Clauses {
			positioned(t, clause.Position())
		}
	}

	for _, operand := range value.Operands {
		positioned(t, operand.Position())
	}
}

// positioned requires every position to name the file, a line and a column.
func positioned(t *testing.T, positions ...layout.Pos) {
	t.Helper()

	for _, pos := range positions {
		if pos.File == "" || pos.Line == 0 || pos.Column == 0 {
			t.Errorf("a node carries no position: %#v", pos)
		}
	}
}

// TestAFileAndABlockReadAlike is the property one production buys: the same
// entries read from a file and from a block are the same model, apart from
// where each was written.
func TestAFileAndABlockReadAlike(t *testing.T) {
	t.Parallel()

	const body = `(entry 01 LEDGER-TRAILER)
(entry 05 LT-TYPE (pic "X") (value "'T'"))
(entry 05 LT-POSTING-COUNT (pic "9(7)") (usage COMP-3))
(entry 05 FILLER (pic "X(72)"))
`

	inFile, err := definitions.Read(fileBody(t, body))
	if err != nil {
		t.Fatalf("read the file:\n%s", diag.Render(err))
	}

	inBlock, err := definitions.Read(blockBody(t, body))
	if err != nil {
		t.Fatalf("read the block:\n%s", diag.Render(err))
	}

	if len(inFile) != len(inBlock) {
		t.Fatalf("the file reads as %d entries and the block as %d", len(inFile), len(inBlock))
	}

	for i := range inFile {
		if inFile[i].Name.Name != inBlock[i].Name.Name || inFile[i].Level.Value != inBlock[i].Level.Value || len(inFile[i].Clauses) != len(inBlock[i].Clauses) {
			t.Errorf("entry %d reads as %#v in the file and %#v in the block", i+1, inFile[i], inBlock[i])
		}

		if inBlock[i].Pos.File != "ledger.sexpr" || inBlock[i].Pos.Line != inFile[i].Pos.Line+baseLines+1 {
			t.Errorf("entry %d of the block is at %s, want the layout's line %d", i+1, inBlock[i].Pos, inFile[i].Pos.Line+baseLines+1)
		}
	}
}

// TestAReaderFaultReadsTheSameInAFileAndInABlock covers what a declaration
// cannot say, each fault at its span and in the same words in a file as in a
// block.
func TestAReaderFaultReadsTheSameInAFileAndInABlock(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		entry string
		// at is the text the fault's span points at, on the entry's line.
		at      string
		message string
		fault   any
	}{
		{
			name:    "a needless word",
			entry:   `(entry 88 A-SPACE (value (word "SPACE")))`,
			at:      `(word "SPACE")`,
			message: `(word "SPACE") is needless: SPACE reads as a symbol, and a data-name that does is written bare`,
			fault:   new(*definitions.NeedlessWordError),
		},
		{
			name:    "a needless word as a name",
			entry:   `(entry 05 (word "A-NAME") (pic "X"))`,
			at:      `(word "A-NAME")`,
			message: `(word "A-NAME") is needless`,
			fault:   new(*definitions.NeedlessWordError),
		},
		{
			name:    "two literals in one text",
			entry:   `(entry 88 A-CODE (value "'A' 'B'"))`,
			at:      `"'A' 'B'"`,
			message: `the text "'A' 'B'" is not exactly one COBOL literal: it reads as 2 tokens, "'A'" "'B'"`,
			fault:   new(*definitions.LiteralTextError),
		},
		{
			name:    "a prefixed literal",
			entry:   `(entry 88 A-LOW (value "X'00'"))`,
			at:      `"X'00'"`,
			message: `the text "X'00'" is not exactly one COBOL literal: it reads as 2 tokens`,
			fault:   new(*definitions.LiteralTextError),
		},
		{
			name:    "a word held as text",
			entry:   `(entry 05 A (pic "X") (value "SPACE"))`,
			at:      `"SPACE"`,
			message: `the text "SPACE" is not exactly one COBOL literal: it reads as a COBOL word`,
			fault:   new(*definitions.LiteralTextError),
		},
		{
			name:    "a literal and a space",
			entry:   `(entry 05 A (pic "X") (value "'A' "))`,
			at:      `"'A' "`,
			message: `the text "'A' " is not exactly one COBOL literal: it reads as the token "'A'" with more beside it`,
			fault:   new(*definitions.LiteralTextError),
		},
		{
			name:    "an empty literal text",
			entry:   `(entry 05 A (pic "X") (value ""))`,
			at:      `""`,
			message: `the text "" is not exactly one COBOL literal: it holds no token`,
			fault:   new(*definitions.LiteralTextError),
		},
		{
			name:    "a literal in a range",
			entry:   `(entry 88 A-RANGE (value (through "'A'" "'C' 'D'")))`,
			at:      `"'C' 'D'"`,
			message: `the text "'C' 'D'" is not exactly one COBOL literal`,
			fault:   new(*definitions.LiteralTextError),
		},
		{
			name:    "a picture and a clause in one text",
			entry:   `(entry 05 A (pic "X(3) OCCURS 4"))`,
			at:      `"X(3) OCCURS 4"`,
			message: `the text "X(3) OCCURS 4" is not exactly one PICTURE character-string: it reads as 3 tokens`,
			fault:   new(*definitions.LiteralTextError),
		},
		{
			name:    "a picture with its separator period",
			entry:   `(entry 05 A (pic "X."))`,
			at:      `"X."`,
			message: `the text "X." is not exactly one PICTURE character-string`,
			fault:   new(*definitions.LiteralTextError),
		},
		{
			name:    "a bound that is not a number",
			entry:   `(entry 05 A (occurs "'3'"))`,
			at:      `"'3'"`,
			message: `the text "'3'" is not exactly one numeric literal: it reads as a string token`,
			fault:   new(*definitions.LiteralTextError),
		},
		{
			name:    "a bound that is a word",
			entry:   `(entry 05 A (occurs "1" (to "MAX")))`,
			at:      `"MAX"`,
			message: `the text "MAX" is not exactly one numeric literal: it reads as a COBOL word`,
			fault:   new(*definitions.LiteralTextError),
		},
		{
			name:    "a to written twice",
			entry:   `(entry 05 A (occurs "1" (to "5") (to "9")))`,
			at:      `(to "9")`,
			message: `an occurs holds at most one (to ...), and this is a second`,
			fault:   new(*definitions.OccursPhraseError),
		},
		{
			name:    "an indexed-by written twice",
			entry:   `(entry 05 A (occurs "3" (indexed-by A-IX) (indexed-by A-JX)))`,
			at:      `(indexed-by A-JX)`,
			message: `an occurs holds at most one (indexed-by ...), and this is a second`,
			fault:   new(*definitions.OccursPhraseError),
		},
		{
			name:    "a phrase out of order",
			entry:   `(entry 05 A (occurs "3" (indexed-by A-IX) (key ASCENDING A-KEY)))`,
			at:      `(key ASCENDING A-KEY)`,
			message: `(key ...) is written after (indexed-by ...); an occurs holds to, depending-on, key and indexed-by in that order`,
			fault:   new(*definitions.OccursPhraseError),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			column := strings.Index(testCase.entry, testCase.at) + 1
			if column == 0 {
				t.Fatalf("%q is not in the entry", testCase.at)
			}

			got := map[string]diag.Diagnostic{}

			for where, body := range map[string][]layout.Node{
				"posting.cpy.sexpr": fileBody(t, testCase.entry+"\n"),
				"ledger.sexpr":      blockBody(t, testCase.entry),
			} {
				entries, err := definitions.Read(body)
				if err == nil {
					t.Fatalf("in %s, read an entry with a fault in it: %#v", where, entries)
				}

				if entries != nil {
					t.Errorf("in %s, a body with a fault returned entries", where)
				}

				if !errors.As(err, testCase.fault) {
					t.Errorf("in %s, the fault is not a %T:\n%s", where, testCase.fault, diag.Render(err))
				}

				diagnostics := diag.Diagnostics(err)
				if len(diagnostics) != 1 {
					t.Fatalf("in %s, want one diagnostic; got:\n%s", where, diag.Render(err))
				}

				if !strings.Contains(diagnostics[0].Message, testCase.message) {
					t.Errorf("in %s, the diagnostic is %q, want one saying %q", where, diagnostics[0].Message, testCase.message)
				}

				line := 1
				if where == "ledger.sexpr" {
					line = baseLines + 2
				}

				if at := diagnostics[0].Spans[0]; at.File != where || at.Line != line || at.Column != column {
					t.Errorf("in %s, the fault is at %s, want %s:%d:%d", where, at, where, line, column)
				}

				got[where] = diagnostics[0]
			}

			if got["posting.cpy.sexpr"].Message != got["ledger.sexpr"].Message {
				t.Errorf("one fault reads two ways:\n in a file:  %s\n in a block: %s", got["posting.cpy.sexpr"].Message, got["ledger.sexpr"].Message)
			}
		})
	}
}

// TestTextsHoldingOneTokenRead is the other side of the literal rule: a text
// holding exactly the token COBOL writes reads, whichever kind its position
// holds.
func TestTextsHoldingOneTokenRead(t *testing.T) {
	t.Parallel()

	const body = `(entry 01 R)
(entry 05 A (pic "S9(7)V99") (value "-12.50"))
(entry 05 B (pic "X(10)") (value "'it''s'"))
(entry 05 C (pic "ZZ9.99") (blank-when-zero))
(entry 05 D (pic "x(4)") (value "\"dq\""))
(entry 05 E (pic "9(5)") (value "+1"))
(entry 05 F (occurs "1" (to "10") (depending-on G)) (pic "X"))
(entry 05 G (pic "99") (value ZEROS))
`

	if _, err := definitions.Read(fileBody(t, body)); err != nil {
		t.Errorf("refused texts holding one token each:\n%s", diag.Render(err))
	}
}

// TestEveryFaultIsReportedRatherThanTheFirst is why Read returns a joined
// error.
func TestEveryFaultIsReportedRatherThanTheFirst(t *testing.T) {
	t.Parallel()

	const body = `(entry 01 R)
(entry 05 A (pic "X X") (value (word "SPACE")))
(entry 05 B (occurs "3" (to "4") (to "5")))
`

	_, err := definitions.Read(fileBody(t, body))

	if got := len(diag.Diagnostics(err)); got != 3 {
		t.Errorf("reported %d of the three faults:\n%s", got, diag.Render(err))
	}
}

// TestABodyTheSchemaWouldRefuseIsNotDiagnosedAgain is the split: a fault a
// declaration states is the schema check's, and a body read without that check
// is a caller's mistake rather than a second diagnostic in other words.
func TestABodyTheSchemaWouldRefuseIsNotDiagnosedAgain(t *testing.T) {
	t.Parallel()

	file, err := layout.Parse("posting.cpy.sexpr", strings.NewReader(`(entry 01 R (picture "X"))`+"\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	_, err = definitions.Read(definitions.FileBody(file))

	var unchecked *definitions.UncheckedBodyError
	if !errors.As(err, &unchecked) {
		t.Errorf("want an UncheckedBodyError; got:\n%s", diag.Render(err))
	}
}

// TestTheSpecsExamplesRead reads #395's two examples out of
// docs/layout/SPEC.md by heading — the definitions file, and the blocks of the
// layout holding its records three ways — and holds both to the schema and to
// this reader.
func TestTheSpecsExamplesRead(t *testing.T) {
	t.Parallel()

	file, err := layoutdoc.Example(layoutdoc.DefinitionsFileExample)
	if err != nil {
		t.Fatalf("read the SPEC's definitions file: %v", err)
	}

	entries, err := definitions.Read(fileBody(t, file))
	if err != nil {
		t.Fatalf("read the SPEC's definitions file:\n%s", diag.Render(err))
	}

	if len(entries) != 14 || entries[0].Name.Name != "POSTING-RECORD" {
		t.Errorf("the SPEC's definitions file reads as %d entries, the first %q; want 14, the first POSTING-RECORD", len(entries), entries[0].Name.Name)
	}

	example, err := layoutdoc.Example(layoutdoc.DefinitionsExample)
	if err != nil {
		t.Fatalf("read the SPEC's three-ways example: %v", err)
	}

	parsed, err := layout.Parse("SPEC.md", strings.NewReader(example))
	if err != nil {
		t.Fatalf("parse the SPEC's three-ways example: %v", err)
	}

	blocks := blocksIn(parsed)
	if len(blocks) == 0 {
		t.Fatal("the SPEC's three-ways example holds no definitions-block: the check would pass vacuously")
	}

	for _, block := range blocks {
		// The blocks are read as the layout holds them. Their shape is checked
		// against the schema by internal/layoutschema's
		// TestTheSpecsDefinitionsBlocksValidate, one block at a time, because
		// the example's records name their sources with `definitions`, which
		// #402 declares, and the layout as a whole is not one the schema admits
		// until then.
		entries, err := definitions.Read(definitions.BlockBody(block))
		if err != nil {
			t.Fatalf("read the SPEC's block:\n%s", diag.Render(err))
		}

		if len(entries) != 4 || entries[0].Name.Name != "LEDGER-TRAILER" {
			t.Errorf("the SPEC's block reads as %d entries; want the 4 of LEDGER-TRAILER", len(entries))
		}

		if entries[0].Pos.File != "SPEC.md" || entries[0].Pos.Line <= block.Pos.Line {
			t.Errorf("the block's first entry is at %s, want a line of the layout after the block opens", entries[0].Pos)
		}
	}
}
