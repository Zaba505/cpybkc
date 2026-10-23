// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package project_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/definitions"
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
	"github.com/Zaba505/cpybkc/internal/layoutschema"
	"github.com/Zaba505/cpybkc/internal/project"
	"github.com/Zaba505/cpybkc/internal/resolve"
)

// blockPostings is postings with its two records naming a block instead of the
// file, the block's opening line last; [inBlock] adds the body and closes it.
//
// Every source sits where postings' does — DEBIT's at 5:15 and CREDIT's at 6:16
// — so [sources] and [namedBy]'s positions hold for both. The block's first
// entry is on line 11, so a body line n is layout line 10+n, two columns
// further in than in a file.
const blockPostings = `(encoding
  (charset cp037) (sign-convention ebcdic)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 11))
(record DEBIT (definitions POSTING POSTING-RECORD))
(record CREDIT (definitions POSTING POSTING-RECORD))
(discriminate DEBIT (equals (item DEBIT PST-TYPE) "D"))
(discriminate CREDIT (equals (item CREDIT PST-TYPE) "C"))
(sequence (* (alt DEBIT CREDIT)))
(definitions-block POSTING
`

// blockLines is how many lines of the layout come before a block body's first.
const blockLines = 10

// inBlock is blockPostings holding body, a definitions file's text, as the
// block's body: every line two spaces further in, which is what moving a
// file's lines into a block is.
func inBlock(body string) string {
	var b strings.Builder

	b.WriteString(blockPostings)

	for line := range strings.SplitSeq(strings.TrimSuffix(body, "\n"), "\n") {
		b.WriteString("  " + line + "\n")
	}

	b.WriteString(")\n")

	return b.String()
}

// A record held in a block resolves, and two records naming one block resolve
// to two record types, each with a tree of its own, as two records over one
// copybook or one file do. A block opens nothing: there is no file beside the
// layout at all.
func TestARecordHeldInABlockResolves(t *testing.T) {
	t.Parallel()

	path, _ := ledgerProject(t, inBlock(posting), nil)

	run, err := project.Load(path)
	if err != nil {
		t.Fatalf("the project does not resolve:\n%s", diag.Render(err))
	}

	if got := records(run.Descriptor); got != 2 {
		t.Errorf("the descriptor carries %d record types, want 2", got)
	}

	if got := fieldNames(run.Descriptor)["PST-ACCOUNT"]; got != 2 {
		t.Errorf("the descriptor carries %d fields named PST-ACCOUNT, want one per record", got)
	}
}

// One layout may mix a copybook, a definitions file and a block, record by
// record, as docs/layout/SPEC.md's "Definitions held three ways" does.
func TestALayoutMixingACopybookAFileAndABlockResolves(t *testing.T) {
	t.Parallel()

	path, _ := ledgerProject(t, `(encoding
  (charset cp037) (sign-convention ebcdic)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 11))
(record LEDGER-HEADER (copybook "cpy/ledger-header.cpy" LEDGER-HEADER-REC))
(record POSTING (definitions "posting.cpy.sexpr" POSTING-RECORD))
(definitions-block TRAILER
  (entry 01 LEDGER-TRAILER)
    (entry 05 LT-TYPE (pic "X"))
    (entry 05 LT-POSTING-COUNT (pic "9(10)")))
(record LEDGER-TRAILER (definitions TRAILER LEDGER-TRAILER))
(discriminate LEDGER-HEADER (equals (item LEDGER-HEADER LH-TYPE) "H"))
(discriminate POSTING (equals (item POSTING PST-TYPE) "P"))
(discriminate LEDGER-TRAILER (equals (item LEDGER-TRAILER LT-TYPE) "T"))
(sequence (seq LEDGER-HEADER (* POSTING) LEDGER-TRAILER))
`, map[string]string{
		"posting.cpy.sexpr": posting,
		"cpy/ledger-header.cpy": fixed(
			"01  LEDGER-HEADER-REC.",
			"    05  LH-TYPE      PIC X.",
			"    05  LH-DATE      PIC X(10).",
		),
	})

	run, err := project.Load(path)
	if err != nil {
		t.Fatalf("the project does not resolve:\n%s", diag.Render(err))
	}

	if got := records(run.Descriptor); got != 3 {
		t.Errorf("the descriptor carries %d record types, want 3", got)
	}

	if got := fieldNames(run.Descriptor)["LT-POSTING-COUNT"]; got != 1 {
		t.Errorf("the descriptor carries %d fields named LT-POSTING-COUNT, want the block's one", got)
	}
}

// #404's word reaches a block's top-level name as it reaches a file's: an
// 01-level named `nil` is written `(word "nil")` in the entry and in the record
// naming it, and binds.
func TestAnItemNamedNilBindsInABlock(t *testing.T) {
	t.Parallel()

	path, _ := ledgerProject(t, `(encoding
  (charset cp037) (sign-convention ebcdic)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 4))
(record R (definitions B (word "nil")))
(definitions-block B
  (entry 01 (word "nil"))
    (entry 05 N-ID (pic "X(4)")))
(discriminate R single-record-type)
(sequence (* R))
`, nil)

	run, err := project.Load(path)
	if err != nil {
		t.Fatalf("the project does not resolve:\n%s", diag.Render(err))
	}

	var named []string

	for _, node := range run.Descriptor.GetNodes() {
		if record := node.GetRecord(); record != nil {
			named = append(named, record.GetNames().GetOriginal())
		}
	}

	if len(named) != 1 || named[0] != "nil" {
		t.Errorf("the descriptor's records are %q, want the one record nil", named)
	}
}

// A block that does not declare the top-level item a record names is the shared
// "declares no" shape, with the block's name bare as the layout writes it and
// both spans in the layout: one at the record's source, and one at the block's
// first entry listing the 01-levels it does declare.
func TestABlockDeclaringNoSuchItemListsWhatItDeclares(t *testing.T) {
	t.Parallel()

	// The records name POSTING-REC, which the block does not declare.
	path, layout := ledgerProject(t,
		strings.ReplaceAll(inBlock(posting+"(entry 01 REVERSAL-RECORD (pic \"X(11)\"))\n"),
			"POSTING POSTING-RECORD)", "POSTING POSTING-REC)"),
		nil)

	_, err := project.Load(path)

	var undeclared *diag.UndeclaredItemError
	if !errors.As(err, &undeclared) {
		t.Fatalf("want an UndeclaredItemError; got:\n%s", diag.Render(err))
	}

	diagnostics := diag.Diagnostics(err)
	if len(diagnostics) != 2 {
		t.Fatalf("want one diagnostic per record; got:\n%s", diag.Render(err))
	}

	for i, at := range sources(layout) {
		d := diagnostics[i]

		if want := `the definitions block POSTING declares no POSTING-REC`; d.Message != want {
			t.Errorf("diagnostic %d says %q, want %q", i, d.Message, want)
		}

		if len(d.Spans) != 2 || d.Spans[0] != at {
			t.Fatalf("diagnostic %d is at %v, want two spans, the first at %s", i, d.Spans, at)
		}

		second := d.Spans[1]
		if second.File != layout || second.Line != blockLines+1 {
			t.Errorf("the second span is %s, want the block's first entry, on line %d of %s", second, blockLines+1, layout)
		}

		if second.Note != "it declares POSTING-RECORD and REVERSAL-RECORD" {
			t.Errorf("the second span's note is %q, want the 01-levels the block declares", second.Note)
		}
	}
}

// faultInABody is one fault an entry body can hold, drawn the same way in a
// definitions file and in a block: line is the body's line 4 with the fault on
// it, and column is where the fault is on that line in a file.
type faultInABody struct {
	line   string
	column int
	is     func(error) bool
}

// faultsInABody are the faults the four parties docs/layout/SPEC.md's "Who
// reports a fault in an entry body" names report — the schema check, the entry
// reader, the judge, and `cobol-go` building and laying the entries out — one
// of each kind each finds.
var faultsInABody = map[string]faultInABody{
	"the schema check's misplaced clause": {
		line:   `  (entry 05 PST-AMOUNT (picture "9(4)"))`,
		column: 24,
		is:     isA[layoutschema.Diagnostic],
	},
	"the schema check's missing argument": {
		line:   `  (entry 05 PST-AMOUNT (pic))`,
		column: 24,
		is:     isA[layoutschema.Diagnostic],
	},
	"the entry reader's needless word": {
		line:   `  (entry 05 (word "PST-AMOUNT") (pic "9(4)"))`,
		column: 13,
		is:     isA[*definitions.NeedlessWordError],
	},
	"the entry reader's literal text": {
		line:   `  (entry 05 PST-AMOUNT (pic "9(4) X"))`,
		column: 29,
		is:     isA[*definitions.LiteralTextError],
	},
	"the judge's refusal": {
		line:   `  (entry 05 PST-AMOUNT (pic "9(4)") (usage FOO))`,
		column: 3,
		is:     isA[*definitions.InadmissibleEntryError],
	},
	"copybook.Build's bad PICTURE": {
		line:   `  (entry 05 PST-AMOUNT (pic "9("))`,
		column: 24,
		is:     isA[copybook.PictureError],
	},
	"copybook.Build's REDEFINES of nothing": {
		line:   `  (entry 05 PST-AMOUNT (redefines PST-NOTHING) (pic "X"))`,
		column: 10,
		is:     isA[copybook.RedefinesError],
	},
	"a layout fault's REDEFINES wider than what it redefines": {
		line:   `  (entry 05 PST-AMOUNT (redefines PST-ACCOUNT) (pic "X(12)"))`,
		column: 10,
		is:     isA[copybook.RedefinesError],
	},
}

// isA reports whether err holds a T.
func isA[T error](err error) bool {
	var found T

	return errors.As(err, &found)
}

// cobolPosition is how `cobol-go` writes a position into its own message, which
// is the one part of a fault's text that differs between a file and a block:
// the same entry is on another line of another file.
var cobolPosition = regexp.MustCompile(`(at )?line \d+, column \d+`)

// A fault in a block reads exactly as the same fault in a definitions file:
// the same words, reported once for the block however many records name it,
// with a span at each of them naming the record — and at its line and column
// in the layout rather than in the file. One test per kind of fault, each
// drawing it both ways and comparing the two.
func TestAFaultInABlockReadsAsInADefinitionsFile(t *testing.T) {
	t.Parallel()

	for name, test := range faultsInABody {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			body := posting + test.line + "\n"

			filePath, fileLayout := ledgerProject(t, postings, map[string]string{"posting.cpy.sexpr": body})
			blockPath, blockLayout := ledgerProject(t, inBlock(body), nil)

			_, fileErr := project.Load(filePath)
			_, blockErr := project.Load(blockPath)

			for _, err := range []error{fileErr, blockErr} {
				if !test.is(err) {
					t.Fatalf("want the fault found by its party; got:\n%s", diag.Render(err))
				}

				var fault *project.DefinitionsFaultError
				if !errors.As(err, &fault) {
					t.Fatalf("want a DefinitionsFaultError; got:\n%s", diag.Render(err))
				}
			}

			inFile, inBlock := diag.Diagnostics(fileErr), diag.Diagnostics(blockErr)
			if len(inFile) != 1 || len(inBlock) != 1 {
				t.Fatalf("want the fault once either way:\nin the file:\n%s\nin the block:\n%s",
					diag.Render(fileErr), diag.Render(blockErr))
			}

			if got, want := cobolPosition.ReplaceAllString(inBlock[0].Message, ""),
				cobolPosition.ReplaceAllString(inFile[0].Message, ""); got != want {
				t.Errorf("in a block the fault says\n%s\nwant what it says in a definitions file\n%s", got, want)
			}

			want := diag.Span{File: blockLayout, Line: blockLines + 4, Column: test.column + 2}
			if got := inBlock[0].Spans[0]; got != want {
				t.Errorf("in a block the fault is at %s, want %s", got, want)
			}

			if got := inFile[0].Spans[0]; got.File != opened(fileLayout) || got.Line != 4 || got.Column != test.column {
				t.Errorf("in a file the fault is at %s, want %s:4:%d", got, opened(fileLayout), test.column)
			}

			namedBy(t, inBlock[0].Spans[1:], blockLayout)

			for _, at := range inBlock[0].Spans[1:] {
				if !strings.HasPrefix(at.Note, "the definitions block is read as") {
					t.Errorf("a note says %q, want it to say the definitions block is read as the record's description", at.Note)
				}
			}
		})
	}
}

// A block no record names is an error at its name, and its entries are still
// read, judged and built: a fault inside it is reported beside the unnamed
// block, not on the run after the block is first named.
func TestABlockNoRecordNamesIsStillJudged(t *testing.T) {
	t.Parallel()

	path, layout := ledgerProject(t, inBlock(posting)+`(definitions-block REVERSAL
  (entry 01 REVERSAL-RECORD)
    (entry 05 RV-AMOUNT (pic "9(4)") (usage FOO)))
`, nil)

	_, err := project.Load(path)

	var unnamed *layoutmodel.UnnamedBlockError
	if !errors.As(err, &unnamed) {
		t.Fatalf("want an UnnamedBlockError; got:\n%s", diag.Render(err))
	}

	var judged *definitions.InadmissibleEntryError
	if !errors.As(err, &judged) {
		t.Fatalf("want the judge's refusal beside it; got:\n%s", diag.Render(err))
	}

	diagnostics := diag.Diagnostics(err)
	if len(diagnostics) != 2 {
		t.Fatalf("want the unnamed block and the fault inside it; got:\n%s", diag.Render(err))
	}

	if unnamed.Pos.File != layout || unnamed.Pos.Line != 15 || unnamed.Pos.Column != 20 {
		t.Errorf("the unnamed block is reported at %s, want its name, at %s:15:20", unnamed.Pos, layout)
	}

	spans := diagnostics[1].Spans
	if want := (diag.Span{File: layout, Line: 17, Column: 5}); spans[0] != want {
		t.Errorf("the fault inside it is at %s, want its entry, at %s", spans[0], want)
	}

	if len(spans) != 1 {
		t.Errorf("the fault in a block no record names names a record: %v", spans)
	}
}

// A diagnostic `resolve` raises about an item of a record held in a block
// points into the layout, at the block's entry, and says it is a definitions
// block.
func TestAResolveFaultPointsIntoTheBlock(t *testing.T) {
	t.Parallel()

	t.Run("an item reference naming nothing", func(t *testing.T) {
		t.Parallel()

		path, layout := ledgerProject(t, inBlock(posting)+"(encoding-override (item DEBIT PST-KIND) (charset none))\n", nil)

		_, err := project.Load(path)

		var unknown *project.UnknownItemError
		if !errors.As(err, &unknown) {
			t.Fatalf("want an UnknownItemError; got:\n%s", diag.Render(err))
		}

		spans := diag.Diagnostics(err)[0].Spans
		if len(spans) != 2 || spans[1].File != layout || spans[1].Line != blockLines+1 {
			t.Errorf("the second span is %v, want the 01-level, in the block, in %s", spans, layout)
		}
	})

	t.Run("a discriminator naming nothing", func(t *testing.T) {
		t.Parallel()

		path, layout := ledgerProject(t, strings.Replace(inBlock(posting), "(item DEBIT PST-TYPE)", "(item DEBIT PST-KIND)", 1), nil)

		_, err := project.Load(path)

		var target *resolve.PredicateTargetError
		if !errors.As(err, &target) {
			t.Fatalf("want a PredicateTargetError; got:\n%s", diag.Render(err))
		}

		spans := diag.Diagnostics(err)[0].Spans
		if len(spans) != 2 || spans[1].File != layout {
			t.Fatalf("the second span is %v, want the layout, %s", spans, layout)
		}

		if want := "the record DEBIT is bound to this definitions block"; spans[1].Note != want {
			t.Errorf("the second span's note is %q, want %q", spans[1].Note, want)
		}
	})

	t.Run("a record choosing no alternative", func(t *testing.T) {
		t.Parallel()

		path, _ := ledgerProject(t, inBlock(posting+"  (entry 05 PST-OTHER (redefines PST-ACCOUNT) (pic \"9(10)\"))\n"), nil)

		_, err := project.Load(path)

		var alternatives *project.AlternativesError
		if !errors.As(err, &alternatives) {
			t.Fatalf("want an AlternativesError; got:\n%s", diag.Render(err))
		}

		if rendered := diag.Render(err); !strings.Contains(rendered, "in the definitions block POSTING") {
			t.Errorf("the fault does not name the definitions block:\n%s", rendered)
		}
	})
}
