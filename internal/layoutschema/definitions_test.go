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

	want := `form "entry" is not a top-level form of a layout: it is written inside a definitions-block, or in a definitions file a record names`
	if len(diagnostics) != 1 || diagnostics[0].Message != want {
		t.Errorf("want one diagnostic saying %q; got:\n%s", want, strings.Join(messages(diagnostics), "\n"))
	}
}

// TestARunOfEntriesInALayoutIsOneDiagnostic is #402's: entries pasted among a
// layout's forms are one mistake per contiguous run, reported at the run's
// first entry, and a second run after another form is a second mistake.
func TestARunOfEntriesInALayoutIsOneDiagnostic(t *testing.T) {
	schema := publishedSchema(t)

	diagnostics := check(t, schema, baseLayout+`(entry 01 LEDGER-TRAILER)
  (entry 05 LT-TYPE (pic "X"))
  (entry 05 LT-COUNT (picture "9(7)"))
(rename TRANSACTION "Txn")
(entry 01 OTHER)
`)

	want := []struct {
		line    int
		message string
	}{
		{line: 10, message: `form "entry" is not a top-level form of a layout: it is written inside a definitions-block, or in a definitions file a record names, and this is the first of 3 written here in a row`},
		{line: 14, message: `form "entry" is not a top-level form of a layout: it is written inside a definitions-block, or in a definitions file a record names`},
	}

	if len(diagnostics) != len(want) {
		t.Fatalf("want %d diagnostics, one per run; got:\n%s", len(want), strings.Join(messages(diagnostics), "\n"))
	}

	for i, w := range want {
		if diagnostics[i].Message != w.message {
			t.Errorf("diagnostic %d says %q, want %q", i, diagnostics[i].Message, w.message)
		}

		if diagnostics[i].Span.Line != w.line || diagnostics[i].Span.Column != 2 {
			t.Errorf("diagnostic %d is at %s, want the run's first entry, at line %d, column 2", i, diagnostics[i].Span, w.line)
		}
	}
}

// TestAFormDeclaredElsewhereIsPlacedNotUnknown is the same rule driven by the
// declarations rather than by `entry`: a child form at the top level is told
// which form it is written in, and a tag declared nowhere is still unknown.
func TestAFormDeclaredElsewhereIsPlacedNotUnknown(t *testing.T) {
	schema := publishedSchema(t)

	for _, testCase := range []struct {
		name, form, want string
	}{
		{name: "a child form", form: "(lrecl 80)", want: `form "lrecl" is not a top-level form of a layout: it is written inside a framing`},
		{name: "a source", form: `(copybook "a.cpy" A)`, want: `form "copybook" is not a top-level form of a layout: it is written inside a record`},
		{name: "a clause", form: `(pic "X")`, want: `form "pic" is not a top-level form of a layout: it is written inside an entry`},
		{name: "a tag declared nowhere", form: "(profile)", want: `unknown top-level form "profile"; a layout carries`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			diagnostics := check(t, schema, baseLayout+testCase.form+"\n")

			if len(diagnostics) != 1 || !strings.HasPrefix(diagnostics[0].Message, testCase.want) {
				t.Errorf("want one diagnostic opening %q; got:\n%s", testCase.want, strings.Join(messages(diagnostics), "\n"))
			}
		})
	}
}

// TestABlockInsideARecordIsToldItIsTopLevel is the record-local block: the
// schema's diagnostic about the position gains a hint that a block is a
// top-level form and that a record names it, whether the block stands where
// the source belongs or among the record's children.
func TestABlockInsideARecordIsToldItIsTopLevel(t *testing.T) {
	schema := publishedSchema(t)

	const hint = "; definitions-block is a top-level form, and a record names one with (definitions ...)"

	for _, testCase := range []struct {
		name, record, want string
	}{
		{
			name:   "as the source",
			record: "(record TRAILER (definitions-block T (entry 01 A (pic \"X\"))))",
			want:   `source of form "record" is (copybook ...) or (definitions ...)` + hint,
		},
		{
			name:   "as a child",
			record: "(record TRAILER (definitions T A) (definitions-block T (entry 01 A (pic \"X\"))))",
			want:   `form "record" has no child "definitions-block"; it admits alternative` + hint,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			diagnostics := check(t, schema, baseLayout+testCase.record+"\n")

			found := false

			for _, diagnostic := range diagnostics {
				found = found || diagnostic.Message == testCase.want
			}

			if !found {
				t.Errorf("want a diagnostic saying %q; got:\n%s", testCase.want, strings.Join(messages(diagnostics), "\n"))
			}
		})
	}

	// A top-level form named from too many places for one to be the hint
	// gains the first half alone.
	diagnostics := check(t, schema, baseLayout+"(record TRAILER (record INNER (copybook \"a.cpy\" A)))\n")
	if len(diagnostics) != 1 || !strings.HasSuffix(diagnostics[0].Message, "; record is a top-level form") {
		t.Errorf("want one diagnostic ending in the bare hint; got:\n%s", strings.Join(messages(diagnostics), "\n"))
	}
}

// TestARecordNamesItsSourceInOneOfThreeSpellings is the `record-source` sort:
// a copybook, a definitions file by its path, or a block by its name, with the
// top-level name required in each.
func TestARecordNamesItsSourceInOneOfThreeSpellings(t *testing.T) {
	schema := publishedSchema(t)

	const block = "(definitions-block TRAILER (entry 01 TRAILER-REC (pic \"X(80)\")))\n"

	for _, testCase := range []struct {
		name, layout, refusal string
	}{
		{name: "a copybook", layout: baseLayout},
		{name: "a definitions file", layout: baseLayout + "(record POSTING (definitions \"posting.cpy.sexpr\" POSTING-RECORD))\n"},
		{name: "a block", layout: baseLayout + block + "(record TRAILER (definitions TRAILER TRAILER-REC))\n"},
		{name: "a block after the record naming it", layout: baseLayout + "(record TRAILER (definitions TRAILER TRAILER-REC))\n" + block},
		{name: "a block sharing a record's name", layout: baseLayout + "(definitions-block TRANSACTION (entry 01 T))\n(record T (definitions TRANSACTION T))\n"},
		{
			name:    "no top-level name",
			layout:  baseLayout + "(record POSTING (definitions \"posting.cpy.sexpr\"))\n",
			refusal: `form "definitions" takes exactly one for its top-level-name argument, and this one has 0`,
		},
		{
			name:    "a top-level name that is text",
			layout:  baseLayout + "(record POSTING (definitions \"posting.cpy.sexpr\" \"POSTING-RECORD\"))\n",
			refusal: `top-level-name of form "definitions" is a symbol`,
		},
		{
			name:    "an origin that is a number",
			layout:  baseLayout + "(record POSTING (definitions 3 POSTING-RECORD))\n",
			refusal: `origin of form "definitions" is text or the name of a definitions-block form this layout defines (it defines none)`,
		},
		{
			name:    "no source",
			layout:  baseLayout + "(record POSTING)\n",
			refusal: `form "record" takes exactly one for its source argument, and this one has 0`,
		},
		{
			name:    "two sources",
			layout:  baseLayout + "(record POSTING (copybook \"a.cpy\" A) (definitions \"b.cpy.sexpr\" B))\n",
			refusal: `form "record" has no child "definitions"; it admits alternative`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			diagnostics := check(t, schema, testCase.layout)

			if testCase.refusal == "" {
				for _, diagnostic := range diagnostics {
					t.Errorf("refused a source the schema admits: %s", diagnostic)
				}

				return
			}

			if len(diagnostics) != 1 || diagnostics[0].Message != testCase.refusal {
				t.Errorf("want one diagnostic saying %q; got:\n%s", testCase.refusal, strings.Join(messages(diagnostics), "\n"))
			}
		})
	}
}

// TestAReferenceFaultListsWhatTheLayoutDeclares is #402's change to every
// reference fault: it names what the layout does declare for that sort, in the
// order the layout declares it, blocks and records alike.
func TestAReferenceFaultListsWhatTheLayoutDeclares(t *testing.T) {
	schema := publishedSchema(t)

	for _, testCase := range []struct {
		name, layout, want string
	}{
		{
			name:   "a block nothing defines, among blocks",
			layout: baseLayout + "(definitions-block HEADER (entry 01 H))\n(definitions-block FOOTER (entry 01 F))\n(record TRAILER (definitions TRAILR T))\n",
			want:   `origin of form "definitions" is text or the name of a definitions-block form this layout defines (it defines HEADER and FOOTER)`,
		},
		{
			name:   "a block in a layout defining none",
			layout: baseLayout + "(record TRAILER (definitions TRAILR T))\n",
			want:   `origin of form "definitions" is text or the name of a definitions-block form this layout defines (it defines none)`,
		},
		{
			name:   "a record nothing defines",
			layout: baseLayout + "(record DETAIL (copybook \"d.cpy\" D))\n(discriminate DETAL single-record-type)\n",
			want:   `record of form "discriminate" is the name of a record form this layout defines (it defines TRANSACTION and DETAIL)`,
		},
		{
			name:   "a record nothing defines, beside one",
			layout: strings.Replace(baseLayout, "(sequence (* TRANSACTION))", "(sequence (* TRANSACTON))", 1),
			want:   `subexpression of form "*" is (seq ...), (alt ...), (* ...), (+ ...), (? ...), (times ...), (when ...) or the name of a record form this layout defines (it defines TRANSACTION)`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			diagnostics := check(t, schema, testCase.layout)

			found := false

			for _, diagnostic := range diagnostics {
				found = found || strings.HasPrefix(diagnostic.Message, testCase.want)
			}

			if !found {
				t.Errorf("want a diagnostic opening %q; got:\n%s", testCase.want, strings.Join(messages(diagnostics), "\n"))
			}
		})
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
//
// The set shrank once, by `copybook`, when #402 moved a record's source from a
// child into the `record-source` sort: a source is an argument of `record`
// now, and `copybook` and `definitions` are contextual forms in that sort.
func TestTheChildTagsDoNotGrow(t *testing.T) {
	want := []string{
		"alternative",
		"blksize",
		"blocks",
		"byte-order",
		"charset",
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

// TestTheSpecsDefinitionsLayoutValidates is the gate over the second: the
// layout that holds its records three ways, whole, now that #402 declares the
// `definitions` source its records name.
func TestTheSpecsDefinitionsLayoutValidates(t *testing.T) {
	example := specExample(t, layoutdoc.DefinitionsExample)

	diagnostics, err := publishedSchema(t).Validate("SPEC.md", strings.NewReader(example))
	if err != nil {
		t.Fatalf("validate the SPEC's example: %v", err)
	}

	for _, diagnostic := range diagnostics {
		t.Errorf("the schema rejects SPEC.md's layout holding definitions three ways: %s", diagnostic)
	}
}

// TestTheSpecsDefinitionsBlocksValidate holds each `definitions-block` of the
// same layout to its declaration on its own, so a fault in a block is told
// apart from one elsewhere in the layout.
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
