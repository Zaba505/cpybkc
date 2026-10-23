// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package project_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layoutschema"
	"github.com/Zaba505/cpybkc/internal/project"
	"github.com/Zaba505/cpybkc/internal/resolve"
)

// postings is a layout whose two records both name one definitions file, so
// that every fault inside the file has two records to be reported against and
// every fault about the line naming it has two lines.
//
// DEBIT's source is at 5:15 and CREDIT's at 6:16.
const postings = `(encoding
  (charset cp037) (sign-convention ebcdic)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 11))
(record DEBIT (definitions "posting.cpy.sexpr" POSTING-RECORD))
(record CREDIT (definitions "posting.cpy.sexpr" POSTING-RECORD))
(discriminate DEBIT (equals (item DEBIT PST-TYPE) "D"))
(discriminate CREDIT (equals (item CREDIT PST-TYPE) "C"))
(sequence (* (alt DEBIT CREDIT)))
`

// posting is the definitions file postings names, as #401's writer prints the
// copybook it transcribes.
const posting = `(entry 01 POSTING-RECORD)
  (entry 05 PST-TYPE (pic "X"))
  (entry 05 PST-ACCOUNT (pic "X(10)"))
`

// sources are where postings' two records name the file.
func sources(layout string) []diag.Span {
	return []diag.Span{
		{File: layout, Line: 5, Column: 15},
		{File: layout, Line: 6, Column: 16},
	}
}

// opened is the definitions file as the run opens it, which is the name every
// span into it carries: beside the layout, which is the directory a layout's
// paths are relative to.
func opened(layout string) string {
	return filepath.Join(filepath.Dir(layout), "posting.cpy.sexpr")
}

// A record held in a definitions file resolves, and two records naming one
// file resolve to two record types — each with a tree of its own, as two
// records over one copybook do.
func TestARecordHeldInADefinitionsFileResolves(t *testing.T) {
	t.Parallel()

	path, _ := ledgerProject(t, postings, map[string]string{"posting.cpy.sexpr": posting})

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

// One layout may mix a copybook and a definitions file, record by record.
func TestALayoutMixingACopybookAndADefinitionsFileResolves(t *testing.T) {
	t.Parallel()

	layout := strings.Replace(postings,
		`(record CREDIT (definitions "posting.cpy.sexpr" POSTING-RECORD))`,
		`(record CREDIT (copybook "posting.cpy" POSTING-RECORD))`, 1)

	path, _ := ledgerProject(t, layout, map[string]string{
		"posting.cpy.sexpr": posting,
		"posting.cpy": fixed(
			"01  POSTING-RECORD.",
			"    05  PST-TYPE     PIC X.",
			"    05  PST-ACCOUNT  PIC X(10).",
		),
	})

	run, err := project.Load(path)
	if err != nil {
		t.Fatalf("the project does not resolve:\n%s", diag.Render(err))
	}

	if got := records(run.Descriptor); got != 2 {
		t.Errorf("the descriptor carries %d record types, want 2", got)
	}
}

// #404's word reaches a definitions file's top-level name as it reaches a
// copybook's: an 01-level named `nil` is written `(word "nil")` in the entry
// and in the record naming it, and binds.
func TestAnItemNamedNilBindsInADefinitionsFile(t *testing.T) {
	t.Parallel()

	path, _ := ledgerProject(t, `(encoding
  (charset cp037) (sign-convention ebcdic)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 4))
(record R (definitions "x.cpy.sexpr" (word "nil")))
(discriminate R single-record-type)
(sequence (* R))
`, map[string]string{"x.cpy.sexpr": `(entry 01 (word "nil"))
  (entry 05 N-ID (pic "X(4)"))
`})

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

// A definitions file that is not there is reported once per record naming it,
// at that record's source, with the path as the layout spells it and the
// absolute path the run looked for on a continuation line — and with no second
// span, because there is no file to point into.
func TestAMissingDefinitionsFileNamesBothPaths(t *testing.T) {
	t.Parallel()

	path, layout := ledgerProject(t, postings, nil)

	_, err := project.Load(path)

	var missing *project.MissingDefinitionsError
	if !errors.As(err, &missing) {
		t.Fatalf("want a MissingDefinitionsError; got:\n%s", diag.Render(err))
	}

	diagnostics := diag.Diagnostics(err)
	if len(diagnostics) != 2 {
		t.Fatalf("want one diagnostic per record naming the file; got:\n%s", diag.Render(err))
	}

	for i, at := range sources(layout) {
		d := diagnostics[i]

		if want := `there is no definitions file to read at "posting.cpy.sexpr"`; d.Message != want {
			t.Errorf("diagnostic %d says %q, want %q", i, d.Message, want)
		}

		if len(d.Spans) != 2 || d.Spans[0] != at {
			t.Fatalf("diagnostic %d is at %v, want %s and a continuation line", i, d.Spans, at)
		}

		if d.Spans[1].Stated() || d.Spans[1].Note != "looked for "+opened(layout) {
			t.Errorf("diagnostic %d's continuation is %+v, want the absolute path and no place", i, d.Spans[1])
		}
	}

	if strings.Contains(diag.Render(err), "copybook") {
		t.Errorf("a missing definitions file is reported as a copybook:\n%s", diag.Render(err))
	}
}

// A definitions file that does not declare the top-level item a record names is
// docs/layout/SPEC.md's shared "declares no" shape: a span at the record's
// source, and one at the file's first entry listing the 01-levels it does
// declare, in the file as the run opened it.
func TestADefinitionsFileDeclaringNoSuchItemListsWhatItDeclares(t *testing.T) {
	t.Parallel()

	path, layout := ledgerProject(t, strings.ReplaceAll(postings, `" POSTING-RECORD)`, `" POSTING-REC)`),
		map[string]string{"posting.cpy.sexpr": posting + "(entry 01 REVERSAL-RECORD (pic \"X(11)\"))\n"})

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

		if want := `the definitions file "posting.cpy.sexpr" declares no POSTING-REC`; d.Message != want {
			t.Errorf("diagnostic %d says %q, want %q", i, d.Message, want)
		}

		if len(d.Spans) != 2 || d.Spans[0] != at {
			t.Fatalf("diagnostic %d is at %v, want two spans, the first at %s", i, d.Spans, at)
		}

		second := d.Spans[1]
		if second.File != opened(layout) || second.Line != 1 {
			t.Errorf("the second span is %s, want the file's first entry, in %s", second, opened(layout))
		}

		if second.Note != "it declares POSTING-RECORD and REVERSAL-RECORD" {
			t.Errorf("the second span's note is %q, want the 01-levels the file declares", second.Note)
		}
	}
}

// A file reached through `definitions` whose top level holds no entry is a
// layout, or nothing, named where a definitions file belongs. It is reported as
// that alone, with a span at the record's source and one at the file's first
// form — not as one misplaced form after another.
func TestALayoutNamedAsADefinitionsFileIsOneDiagnosticPerRecord(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		file string
		at   func(file string) diag.Span
	}{
		"a layout": {
			file: postings,
			at:   func(file string) diag.Span { return diag.Span{File: file, Line: 1, Column: 2} },
		},
		"an empty file": {
			file: ";; nothing here\n",
			at:   func(file string) diag.Span { return diag.Span{File: file} },
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path, layout := ledgerProject(t, postings, map[string]string{"posting.cpy.sexpr": test.file})

			_, err := project.Load(path)

			var named *project.NotDefinitionsError
			if !errors.As(err, &named) {
				t.Fatalf("want a NotDefinitionsError; got:\n%s", diag.Render(err))
			}

			diagnostics := diag.Diagnostics(err)
			if len(diagnostics) != 2 {
				t.Fatalf("want one diagnostic per record and nothing about the file's forms; got:\n%s", diag.Render(err))
			}

			for i, at := range sources(layout) {
				spans := diagnostics[i].Spans
				if len(spans) != 2 || spans[0] != at {
					t.Fatalf("diagnostic %d is at %v, want two spans, the first at %s", i, spans, at)
				}

				want := test.at(opened(layout))
				if got := (diag.Span{File: spans[1].File, Line: spans[1].Line, Column: spans[1].Column}); got != want {
					t.Errorf("the second span is %s, want %s", got, want)
				}
			}
		})
	}
}

// A definitions file holding a form that is not an entry — a block, or a
// layout's form — is the body check's diagnostic, at that form in the file,
// with a second span at the naming record's `definitions` source. It is
// reported once for the file, and with two records naming it there is a span
// at each.
func TestANonEntryFormInADefinitionsFileIsTheBodyChecksWithTheRecordsSpan(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		form string
		says string
	}{
		"a definitions block": {
			form: "(definitions-block TRAILER (entry 01 LEDGER-TRAILER (pic \"X\")))\n",
			says: "a definitions file holds no definitions-block",
		},
		"a layout's form": {
			form: "(sequence (* DEBIT))\n",
			says: `form "sequence" is a layout's`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path, layout := ledgerProject(t, postings, map[string]string{"posting.cpy.sexpr": posting + test.form})

			_, err := project.Load(path)

			var schema layoutschema.Diagnostic
			if !errors.As(err, &schema) {
				t.Fatalf("want the body check's diagnostic; got:\n%s", diag.Render(err))
			}

			diagnostics := diag.Diagnostics(err)
			if len(diagnostics) != 1 {
				t.Fatalf("want the one diagnostic, once for the file; got:\n%s", diag.Render(err))
			}

			d := diagnostics[0]
			if !strings.Contains(d.Message, test.says) {
				t.Errorf("the diagnostic says %q, want it to say %q", d.Message, test.says)
			}

			if want := (diag.Span{File: opened(layout), Line: 4, Column: 2}); d.Spans[0] != want {
				t.Errorf("the diagnostic is at %s, want the form, at %s", d.Spans[0], want)
			}

			namedBy(t, d.Spans[1:], layout)
		})
	}
}

// A fault inside a definitions file is reported once for the file, however many
// records name it, with a note at each of them saying which record's
// description it was being read as. That holds for every party that finds one:
// the grammar, the body check, the entry reader, the judge, `copybook.Build`,
// and a layout fault in an 01-level both records bind.
func TestAFaultInADefinitionsFileIsReportedOnceNamingEachRecord(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		file string
		at   diag.Span
	}{
		"the grammar's": {
			file: posting + "(entry 05 PST-AMOUNT\n",
			at:   diag.Span{Line: 4, Column: 11},
		},
		"the body check's": {
			file: posting + "  (entry 05 PST-AMOUNT (picture \"9(4)\"))\n",
			at:   diag.Span{Line: 4, Column: 24},
		},
		"the entry reader's": {
			file: posting + "  (entry 05 (word \"PST-AMOUNT\") (pic \"9(4)\"))\n",
			at:   diag.Span{Line: 4, Column: 13},
		},
		"the judge's": {
			file: posting + "  (entry 05 PST-AMOUNT (pic \"9(4)\") (usage FOO))\n",
			at:   diag.Span{Line: 4, Column: 3},
		},
		"copybook.Build's": {
			file: posting + "  (entry 05 PST-AMOUNT (pic \"9(\"))\n",
			at:   diag.Span{Line: 4, Column: 24},
		},
		"a layout fault's": {
			file: posting + "  (entry 05 PST-AMOUNT (redefines PST-ACCOUNT) (pic \"X(12)\"))\n",
			at:   diag.Span{Line: 4, Column: 10},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path, layout := ledgerProject(t, postings, map[string]string{"posting.cpy.sexpr": test.file})

			_, err := project.Load(path)

			var fault *project.DefinitionsFaultError
			if !errors.As(err, &fault) {
				t.Fatalf("want a DefinitionsFaultError; got:\n%s", diag.Render(err))
			}

			diagnostics := diag.Diagnostics(err)
			if len(diagnostics) != 1 {
				t.Fatalf("want the fault once for the file; got:\n%s", diag.Render(err))
			}

			rendered := diagnostics[0].String()
			test.at.File = opened(layout)

			if !strings.HasPrefix(rendered, test.at.String()+": ") {
				t.Errorf("the fault is\n%s\nwant it at %s", rendered, test.at)
			}

			namedBy(t, diagnostics[0].Spans[1:], layout)
		})
	}
}

// `cobol-go`'s build and layout faults name a line and a column and no file. In
// a definitions file the line and the column are the file's, because the
// entries were built with the positions of the forms they came from, and each
// fault is reported at that position in the file the run opened — found with
// errors.As, so that it is still the error `cobol-go` returned.
func TestCobolGosFaultsPointIntoTheDefinitionsFile(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		file string
		as   any
		line int
	}{
		"a level out of sequence": {
			file: "(entry 05 PST-TYPE (pic \"X\"))\n" + posting,
			as:   new(copybook.LevelSequenceError),
			line: 1,
		},
		"a bad PICTURE": {
			file: posting + "  (entry 05 PST-AMOUNT (pic \"9(\"))\n",
			as:   new(copybook.PictureError),
			line: 4,
		},
		"a RENAMES of nothing": {
			file: posting + "(entry 66 PST-ALIAS (renames PST-NOTHING))\n",
			as:   new(copybook.RenamesError),
			line: 4,
		},
		"a REDEFINES of nothing": {
			file: posting + "  (entry 05 PST-AMOUNT (redefines PST-NOTHING) (pic \"X\"))\n",
			as:   new(copybook.RedefinesError),
			line: 4,
		},
		"a REDEFINES wider than what it redefines": {
			file: posting + "  (entry 05 PST-AMOUNT (redefines PST-ACCOUNT) (pic \"X(12)\"))\n",
			as:   new(copybook.RedefinesError),
			line: 4,
		},
		"a USAGE its PICTURE cannot have": {
			file: posting + "  (entry 05 PST-AMOUNT (pic \"X\") (usage COMP))\n",
			as:   new(copybook.LayoutError),
			line: 4,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path, layout := ledgerProject(t, postings, map[string]string{"posting.cpy.sexpr": test.file})

			_, err := project.Load(path)
			if !errors.As(err, test.as) {
				t.Fatalf("want cobol-go's %T; got:\n%s", test.as, diag.Render(err))
			}

			var built *project.DefinitionsBuildError
			if !errors.As(err, &built) {
				t.Fatalf("want a DefinitionsBuildError; got:\n%s", diag.Render(err))
			}

			diagnostics := diag.Diagnostics(err)
			if len(diagnostics) != 1 {
				t.Fatalf("want the fault once; got:\n%s", diag.Render(err))
			}

			if at := diagnostics[0].Spans[0]; at.File != opened(layout) || at.Line != test.line {
				t.Errorf("the fault is at %s, want line %d of %s", at, test.line, opened(layout))
			}
		})
	}
}

// A diagnostic `resolve` raises about an item of a record held in a definitions
// file points into the file as the run opened it, and says it is a definitions
// file rather than a copybook.
func TestAResolveFaultPointsIntoTheDefinitionsFile(t *testing.T) {
	t.Parallel()

	t.Run("an item reference naming nothing", func(t *testing.T) {
		t.Parallel()

		path, layout := ledgerProject(t, postings+"(encoding-override (item DEBIT PST-KIND) (charset none))\n",
			map[string]string{"posting.cpy.sexpr": posting})

		_, err := project.Load(path)

		var unknown *project.UnknownItemError
		if !errors.As(err, &unknown) {
			t.Fatalf("want an UnknownItemError; got:\n%s", diag.Render(err))
		}

		spans := diag.Diagnostics(err)[0].Spans
		if len(spans) != 2 || spans[1].File != opened(layout) || spans[1].Line != 1 {
			t.Errorf("the second span is %v, want the 01-level, in %s", spans, opened(layout))
		}
	})

	t.Run("a discriminator naming nothing", func(t *testing.T) {
		t.Parallel()

		path, layout := ledgerProject(t, strings.Replace(postings, "(item DEBIT PST-TYPE)", "(item DEBIT PST-KIND)", 1),
			map[string]string{"posting.cpy.sexpr": posting})

		_, err := project.Load(path)

		var target *resolve.PredicateTargetError
		if !errors.As(err, &target) {
			t.Fatalf("want a PredicateTargetError; got:\n%s", diag.Render(err))
		}

		spans := diag.Diagnostics(err)[0].Spans
		if len(spans) != 2 || spans[1].File != opened(layout) {
			t.Fatalf("the second span is %v, want the definitions file, %s", spans, opened(layout))
		}

		if want := "the record DEBIT is bound to this definitions file"; spans[1].Note != want {
			t.Errorf("the second span's note is %q, want %q", spans[1].Note, want)
		}
	})

	t.Run("a record choosing no alternative", func(t *testing.T) {
		t.Parallel()

		path, _ := ledgerProject(t, postings, map[string]string{
			"posting.cpy.sexpr": posting + "  (entry 05 PST-OTHER (redefines PST-ACCOUNT) (pic \"9(10)\"))\n",
		})

		_, err := project.Load(path)

		var alternatives *project.AlternativesError
		if !errors.As(err, &alternatives) {
			t.Fatalf("want an AlternativesError; got:\n%s", diag.Render(err))
		}

		if rendered := diag.Render(err); !strings.Contains(rendered, `in the definitions file "posting.cpy.sexpr"`) {
			t.Errorf("the fault does not name the definitions file:\n%s", rendered)
		}
	})
}

// namedBy requires spans to be one per postings record, at its source, saying
// which record's description the file was read as.
func namedBy(t *testing.T, spans []diag.Span, layout string) {
	t.Helper()

	want := sources(layout)
	if len(spans) != len(want) {
		t.Fatalf("want a span at each record naming the file; got %v", spans)
	}

	for i, record := range []string{"DEBIT", "CREDIT"} {
		at := spans[i]
		if (diag.Span{File: at.File, Line: at.Line, Column: at.Column}) != want[i] {
			t.Errorf("span %d is at %s, want %s", i, at, want[i])
		}

		if !strings.Contains(at.Note, "record "+record+"'s description") {
			t.Errorf("span %d's note is %q, want it to name record %s", i, at.Note, record)
		}
	}
}
