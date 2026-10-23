// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutschema

import (
	"slices"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/layoutdoc"
	sexpr "github.com/z5labs/sexpr-go"
)

// baseLayout is a layout the schema admits and nothing else in it: a block
// appended to it is the only thing a diagnostic can be about.
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

// inBlock is a layout holding body as one block's entries.
func inBlock(body string) string {
	return baseLayout + "(definitions-block TRAILER\n" + body + ")\n"
}

// TestAnEntryIsNotATopLevelForm holds the vocabularies apart, which is what
// lets a file reached the wrong way be recognised as the other kind of document
// from its first form: a layout's top level never admits `entry`.
func TestAnEntryIsNotATopLevelForm(t *testing.T) {
	schema := publishedSchema(t)

	if slices.Contains(schema.TopLevelForms(), "entry") {
		t.Fatalf("the schema declares entry a top-level form: %v", schema.TopLevelForms())
	}

	if !slices.Contains(schema.TopLevelForms(), BlockForm) {
		t.Fatalf("the schema declares no %s top-level form: %v", BlockForm, schema.TopLevelForms())
	}

	diagnostics := check(t, schema, baseLayout+"(entry 01 LEDGER-TRAILER)\n")

	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, `unknown top-level form "entry"; a layout carries`) {
		t.Errorf("want one unknown-top-level-form diagnostic about entry; got:\n%s", strings.Join(messages(diagnostics), "\n"))
	}
}

// TestABlocksBodyAdmitsEntriesAloneOneOrMore is the shape of a block's
// body: `entry` forms, at least one, and nothing beside them.
func TestABlocksBodyAdmitsEntriesAloneOneOrMore(t *testing.T) {
	schema := publishedSchema(t)

	testCases := []struct {
		name    string
		layout  string
		refusal string
	}{
		{name: "one entry", layout: inBlock("(entry 01 LEDGER-TRAILER (pic \"X(80)\"))")},
		{name: "several entries", layout: inBlock("(entry 01 LEDGER-TRAILER)\n(entry 05 LT-TYPE (pic \"X\"))\n(entry 05 FILLER (pic \"X(79)\"))")},
		{name: "no entries", layout: baseLayout + "(definitions-block TRAILER)\n", refusal: `form "definitions-block" takes one or more for its entry argument, and this one has 0`},
		{name: "a clause where an entry belongs", layout: inBlock(`(pic "X")`), refusal: `entry of form "definitions-block" is (entry ...)`},
		{name: "a layout form where an entry belongs", layout: inBlock(`(record TRAILER (copybook "t.cpy" T))`), refusal: `entry of form "definitions-block" is (entry ...)`},
		{name: "a nested block", layout: inBlock(`(definitions-block INNER (entry 01 A))`), refusal: `entry of form "definitions-block" is (entry ...)`},
		{name: "a name that is not a symbol", layout: baseLayout + "(definitions-block \"TRAILER\" (entry 01 A))\n", refusal: `name of form "definitions-block" is a symbol`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			diagnostics := check(t, schema, testCase.layout)

			if testCase.refusal == "" {
				if len(diagnostics) != 0 {
					t.Errorf("refused a block the schema admits:\n%s", strings.Join(messages(diagnostics), "\n"))
				}

				return
			}

			if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, testCase.refusal) {
				t.Errorf("want one diagnostic saying %q; got:\n%s", testCase.refusal, strings.Join(messages(diagnostics), "\n"))
			}
		})
	}
}

// TestTheChildTagsDoNotGrow holds #395's promise about the declaration
// shape: every clause and operand form is contextual, so the tags reserved for
// child forms across every layout are the ones they were before entries
// arrived, and a clause tag such as `value` claims nothing outside an entry.
func TestTheChildTagsDoNotGrow(t *testing.T) {
	want := []string{
		"alternative",
		"blksize",
		"blocks",
		"byte-order",
		"charset",
		"copybook",
		"delimiter",
		"float-format",
		"lrecl",
		"max-segment",
		"occurs-depending-on",
		"placement",
		"recfm",
		"sign-convention",
	}

	if got := sorted(keys(publishedSchema(t).children)); !slices.Equal(got, want) {
		t.Errorf("the schema's child tags are\n %v\nwant\n %v", got, want)
	}
}

// TestADefinitionsFileIsCheckedAsOneBlocksBody covers what a definitions file
// may hold at its top level, and that a fault there names the document it is
// in rather than calling it a layout.
func TestADefinitionsFileIsCheckedAsOneBlocksBody(t *testing.T) {
	schema := publishedSchema(t)

	testCases := []struct {
		name string
		file string
		// refusals are fragments of the diagnostics the file draws, in order,
		// or none when it should draw none.
		refusals []string
	}{
		{name: "entries", file: "(entry 01 A)\n(entry 05 B (pic \"X\"))\n"},
		{
			name:     "a layout form",
			file:     "(entry 01 A (pic \"X\"))\n(framing (recfm FB) (lrecl 80))\n",
			refusals: []string{`form "framing" is a layout's, and a definitions file carries entry forms only`},
		},
		{
			name:     "a definitions block",
			file:     "(definitions-block TRAILER (entry 01 A (pic \"X\")))\n",
			refusals: []string{"a definitions file holds no definitions-block: it is one block's body", "a definitions file carries one or more entry form, and this one carries 0"},
		},
		{
			name:     "a form nothing declares",
			file:     "(entry 01 A)\n(entyr 05 B)\n",
			refusals: []string{`unknown top-level form "entyr"; a definitions file carries entry`},
		},
		{
			name:     "an empty file",
			file:     ";; nothing but a comment\n",
			refusals: []string{"a definitions file carries one or more entry form, and this one carries 0"},
		},
		{
			name:     "a bare symbol",
			file:     "(entry 01 A)\nB\n",
			refusals: []string{"a definitions file is a list of forms, and this is not one"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			diagnostics := checkDefinitions(t, schema, testCase.file)

			if len(diagnostics) != len(testCase.refusals) {
				t.Fatalf("want %d diagnostics; got:\n%s", len(testCase.refusals), strings.Join(messages(diagnostics), "\n"))
			}

			for i, refusal := range testCase.refusals {
				if !strings.Contains(diagnostics[i].Message, refusal) {
					t.Errorf("diagnostic %d is %q, want one saying %q", i, diagnostics[i].Message, refusal)
				}

				if strings.Contains(diagnostics[i].Message, "a layout carries") {
					t.Errorf("a diagnostic about a definitions file calls it a layout: %s", diagnostics[i])
				}
			}
		})
	}
}

// TestAShapeFaultReadsTheSameInAFileAndInABlock is the property one schema
// buys: the declarations are the same, so a fault inside an entry is reported
// in the same words wherever the entry is held, with only the span differing.
// One case per kind of fault a declaration states.
func TestAShapeFaultReadsTheSameInAFileAndInABlock(t *testing.T) {
	schema := publishedSchema(t)

	testCases := []struct {
		name    string
		entry   string
		refusal string
	}{
		{name: "an unknown clause", entry: `(entry 05 A (picture "X"))`, refusal: `clause of form "entry" is (redefines ...)`},
		{name: "a misplaced argument", entry: `(entry 05 A (pic X))`, refusal: `picture of form "pic" is text`},
		{name: "a missing argument", entry: `(entry 05 A (redefines))`, refusal: `form "redefines" takes exactly one for its redefined argument, and this one has 0`},
		{name: "a missing name", entry: `(entry 05)`, refusal: `form "entry" takes exactly one for its name argument, and this one has 0`},
		{name: "an argument too many", entry: `(entry 05 A (sign LEADING SEPARATE SEPARATE))`, refusal: `form "sign" takes position, separate, and this is one argument too many`},
		{name: "an operand on a presence clause", entry: `(entry 05 A (justified RIGHT))`, refusal: `form "justified" takes no arguments, and this is one argument too many`},
		{name: "a value outside a closed set", entry: `(entry 05 A (sign LEADING separate))`, refusal: `separate of form "sign" is one of SEPARATE`},
		{name: "a level that is not a positive number", entry: `(entry "05" A)`, refusal: `level of form "entry" is a positive integer`},
		{name: "a word that is not text", entry: `(entry 05 A (value (word SPACE)))`, refusal: `spelling of form "word" is text`},
		{name: "an unknown occurs phrase", entry: `(entry 05 A (occurs "3" (times "3")))`, refusal: `phrase of form "occurs" is (to ...), (depending-on ...), (key ...) or (indexed-by ...)`},
		{name: "a range in a range", entry: `(entry 88 A (value (through (through "'A'" "'B'") "'C'")))`, refusal: `from of form "through" is`},
		{name: "a boolean", entry: `(entry 05 A (pic #t))`, refusal: `picture of form "pic": no position in a layout takes a boolean`},
		{name: "nil as a name", entry: `(entry 05 nil)`, refusal: `name of form "entry": nil has no meaning in a layout`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			inFile := checkDefinitions(t, schema, testCase.entry+"\n")
			inLayout := check(t, schema, inBlock(testCase.entry))

			if len(inFile) != 1 || !strings.Contains(inFile[0].Message, testCase.refusal) {
				t.Fatalf("in a file, want one diagnostic saying %q; got:\n%s", testCase.refusal, strings.Join(messages(inFile), "\n"))
			}

			if len(inLayout) != 1 {
				t.Fatalf("in a block, want one diagnostic; got:\n%s", strings.Join(messages(inLayout), "\n"))
			}

			if inFile[0].Message != inLayout[0].Message {
				t.Errorf("one fault reads two ways:\n in a file:  %s\n in a block: %s", inFile[0].Message, inLayout[0].Message)
			}

			if inFile[0].Span.File != "posting.cpy.sexpr" || inLayout[0].Span.File != "ledger.sexpr" {
				t.Errorf("the spans name %q and %q, want the file each fault is in", inFile[0].Span.File, inLayout[0].Span.File)
			}
		})
	}
}

// TestAnOptionalOperandMayBeLeftOut is #396's fix met by the first published
// arguments that need it: SEPARATE on a sign, and a synchronization direction.
func TestAnOptionalOperandMayBeLeftOut(t *testing.T) {
	schema := publishedSchema(t)

	testCases := []struct {
		name    string
		entry   string
		refusal string
	}{
		{name: "synchronized with no direction", entry: `(entry 05 A (pic "S9(4)") (usage COMP) (synchronized))`},
		{name: "synchronized with a direction", entry: `(entry 05 A (pic "S9(4)") (usage COMP) (synchronized LEFT))`},
		{name: "a sign with no SEPARATE", entry: `(entry 05 A (pic "S9(4)") (sign LEADING))`},
		{name: "a sign with SEPARATE", entry: `(entry 05 A (pic "S9(4)") (sign TRAILING SEPARATE))`},
		{name: "a sign with SEPARATE twice", entry: `(entry 05 A (pic "S9(4)") (sign LEADING SEPARATE SEPARATE))`, refusal: "one argument too many"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			for where, diagnostics := range map[string][]Diagnostic{
				"in a file":  checkDefinitions(t, schema, testCase.entry+"\n"),
				"in a block": check(t, schema, inBlock(testCase.entry)),
			} {
				if testCase.refusal == "" {
					if len(diagnostics) != 0 {
						t.Errorf("%s, refused an entry the schema admits:\n%s", where, strings.Join(messages(diagnostics), "\n"))
					}

					continue
				}

				if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, testCase.refusal) {
					t.Errorf("%s, want one diagnostic saying %q; got:\n%s", where, testCase.refusal, strings.Join(messages(diagnostics), "\n"))
				}
			}
		})
	}
}

// TestEveryClauseValidates is every one of the twelve clause forms, and every
// operand form, in the spelling docs/layout/SPEC.md's "An entry, and one form
// per clause" gives it. A declaration the spec's spelling does not satisfy is
// the schema being wrong about the notation.
func TestEveryClauseValidates(t *testing.T) {
	const body = `(entry 01 POSTING-RECORD (global) (external))
(entry 05 PST-TYPE (pic "X") (justified))
(entry 88 PST-IS-DEBIT (value "'A'" (through "'C'" "'F'") SPACE))
(entry 05 PST-COUNT (pic "9(2)") (usage COMP-3) (blank-when-zero))
(entry 05 PST-AMOUNT (pic "S9(7)V99") (sign TRAILING SEPARATE) (synchronized LEFT))
(entry 05 PST-ENTRY (occurs "0" (to "50") (depending-on PST-COUNT) (key ASCENDING PST-KEY) (key DESCENDING PST-KEY) (indexed-by PST-IX)))
(entry 10 PST-KEY (pic "X(4)") (value (word "nil")))
(entry 05 PST-OTHER (redefines PST-TYPE) (pic "X"))
(entry 66 PST-ALL (renames (through PST-TYPE PST-AMOUNT)))
(entry 66 PST-ONE (renames PST-TYPE))
`

	schema := publishedSchema(t)

	for where, diagnostics := range map[string][]Diagnostic{
		"in a file":  checkDefinitions(t, schema, body),
		"in a block": check(t, schema, inBlock(body)),
	} {
		for _, diagnostic := range diagnostics {
			t.Errorf("%s: %s", where, diagnostic)
		}
	}
}

// TestTheSpecsDefinitionsFileValidates is the staleness gate over the first of
// #395's examples: the definitions file docs/layout/SPEC.md shows an adopter,
// read out of the document and checked as the file it is.
func TestTheSpecsDefinitionsFileValidates(t *testing.T) {
	example := specExample(t, layoutdoc.DefinitionsFileExample)

	diagnostics, err := publishedSchema(t).ValidateDefinitions("posting.cpy.sexpr", strings.NewReader(example))
	if err != nil {
		t.Fatalf("validate the SPEC's definitions file: %v", err)
	}

	for _, diagnostic := range diagnostics {
		t.Errorf("the schema rejects SPEC.md's own definitions file: %s", diagnostic)
	}
}

// TestTheSpecsDefinitionsBlocksValidate is the gate over the second: the
// `definitions-block` forms of the layout that holds its records three ways.
//
// Only the blocks are checked. The same layout's records name their sources
// with `definitions`, which #402 declares, so the layout as a whole is not one
// this schema admits yet; each block is held to its declaration on its own.
func TestTheSpecsDefinitionsBlocksValidate(t *testing.T) {
	example := specExample(t, layoutdoc.DefinitionsExample)

	file, err := sexpr.Parse(strings.NewReader(example))
	if err != nil {
		t.Fatalf("parse the SPEC's example: %v", err)
	}

	schema := publishedSchema(t)
	decl := schema.contextual[contextKey{context: contextTopLevel, tag: BlockForm}]
	blocks := 0

	for _, node := range file.Nodes {
		list, ok := node.(sexpr.List)
		if !ok || len(list.Elements) == 0 {
			continue
		}

		if tag, ok := list.Elements[0].(sexpr.Symbol); !ok || tag.Value != BlockForm {
			continue
		}

		blocks++

		check := schema.newChecker("SPEC.md")
		check.form(list, decl)

		for _, diagnostic := range check.diagnostics {
			t.Errorf("the schema rejects a block in SPEC.md's example: %s", diagnostic)
		}
	}

	if blocks == 0 {
		t.Fatal("the SPEC's example holds no definitions-block: the check would pass vacuously")
	}
}

// TestValidateDefinitionsFailsOnSourceThatIsNotSExpressions keeps a parse
// failure an error, as it is for a layout.
func TestValidateDefinitionsFailsOnSourceThatIsNotSExpressions(t *testing.T) {
	if _, err := publishedSchema(t).ValidateDefinitions("posting.cpy.sexpr", strings.NewReader("(entry 01")); err == nil {
		t.Error("ValidateDefinitions accepted source the grammar cannot parse")
	}
}

// TestASchemaWithNoBlockHoldsNothingInADefinitionsFile is a schema predating
// the form: it says nothing a definitions file may hold, and the check says so
// rather than admitting the file.
func TestASchemaWithNoBlockHoldsNothingInADefinitionsFile(t *testing.T) {
	schema, err := Load(strings.NewReader("(schema layout 1)\n(form f (in top-level) (arity zero-or-more) (layer encoding-profile))\n"))
	if err != nil {
		t.Fatalf("load the fixture schema: %v", err)
	}

	diagnostics := checkDefinitions(t, schema, "(entry 01 A)\n")

	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "the schema declares no definitions-block") {
		t.Errorf("want one diagnostic saying the schema declares no block; got:\n%s", strings.Join(messages(diagnostics), "\n"))
	}
}

// check validates a layout under the name ledger.sexpr.
func check(t *testing.T, schema *Schema, source string) []Diagnostic {
	t.Helper()

	diagnostics, err := schema.Validate("ledger.sexpr", strings.NewReader(source))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	return diagnostics
}

// checkDefinitions validates a definitions file under the name
// posting.cpy.sexpr.
func checkDefinitions(t *testing.T, schema *Schema, source string) []Diagnostic {
	t.Helper()

	diagnostics, err := schema.ValidateDefinitions("posting.cpy.sexpr", strings.NewReader(source))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	return diagnostics
}
