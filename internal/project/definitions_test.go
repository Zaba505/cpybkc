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

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
	"github.com/Zaba505/cpybkc/internal/manifest"
	"github.com/Zaba505/cpybkc/internal/project"
)

// ledger is a layout whose header is a copybook that does not exist, whose
// posting is a definitions file and whose trailer and summary are blocks. A run
// that opened the copybook would report it missing, so a run reporting the
// definitions alone is one that opened nothing.
const ledger = `(encoding
  (charset cp037) (sign-convention ebcdic)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 80))
(record LEDGER (copybook "missing.cpy" LEDGER-REC))
(record POSTING (definitions "posting.cpy.sexpr" POSTING-RECORD))
(record TRAILER (definitions TRAILER LEDGER-TRAILER))
(discriminate LEDGER (equals (item LEDGER LR-TYPE) "L"))
(discriminate POSTING (equals (item POSTING PST-TYPE) "P"))
(discriminate TRAILER (equals (item TRAILER LT-TYPE) "T"))
(sequence (seq LEDGER (* POSTING) TRAILER))
(definitions-block TRAILER
  (entry 01 LEDGER-TRAILER)
    (entry 05 LT-TYPE (pic "X")))
`

// ledgerProject writes a project over layout, with whatever else is named in
// files written beside it, and hands back the manifest's path and the
// layout's.
func ledgerProject(t *testing.T, layout string, files map[string]string) (string, string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.sexpr")

	write(t, path, layout)
	write(t, filepath.Join(dir, manifest.Name), `{"layout": "ledger.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	for name, body := range files {
		write(t, filepath.Join(dir, name), body)
	}

	return filepath.Join(dir, manifest.Name), path
}

// A record may name a definitions file or a block, and this build reads it and
// stops there: nothing resolves a record held either way until #405 and #409,
// so each `definitions` source is refused at the source, saying so, and each
// block at its tag, as #400 refused it.
//
// The definitions file is never opened. It is absent in one run and present
// and unreadable as entries in the other, and both runs report the same thing:
// a run that opened it would have had something to say about it.
func TestADefinitionsSourceIsRefusedAtItAndNothingIsOpened(t *testing.T) {
	t.Parallel()

	for name, files := range map[string]map[string]string{
		"the file absent":  nil,
		"the file present": {"posting.cpy.sexpr": "this is not an entry body ((("},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path, layout := ledgerProject(t, ledger, files)

			run, err := project.Load(path)
			if err == nil {
				t.Fatalf("the project resolved records held in definitions nothing resolves: %v", run.Descriptor)
			}

			rendered := diag.Render(err)

			var missing *diag.MissingCopybookError
			if errors.As(err, &missing) || strings.Contains(rendered, "no copybook") {
				t.Errorf("the run looked for a copybook:\n%s", rendered)
			}

			want := []struct {
				at   diag.Span
				says string
			}{
				{at: diag.Span{File: layout, Line: 6, Column: 17}, says: `record POSTING is held in the definitions file "posting.cpy.sexpr", and this build does not resolve definitions files or blocks`},
				{at: diag.Span{File: layout, Line: 7, Column: 17}, says: `record TRAILER is held in the definitions block TRAILER, and this build does not resolve definitions files or blocks`},
				{at: diag.Span{File: layout, Line: 12, Column: 2}, says: "the definitions block TRAILER is not resolved by this build"},
			}

			diagnostics := diag.Diagnostics(err)
			if len(diagnostics) != len(want) {
				t.Fatalf("want one diagnostic per source and per block; got:\n%s", rendered)
			}

			for i, w := range want {
				if diagnostics[i].Spans[0] != w.at || diagnostics[i].Message != w.says {
					t.Errorf("diagnostic %d is %s: %s\nwant %s: %s", i, diagnostics[i].Spans[0], diagnostics[i].Message, w.at, w.says)
				}
			}

			var source *project.DefinitionsSourceError
			if !errors.As(err, &source) {
				t.Errorf("no DefinitionsSourceError in:\n%s", rendered)
			}

			var block *project.DefinitionsBlockError
			if !errors.As(err, &block) {
				t.Errorf("no DefinitionsBlockError in:\n%s", rendered)
			}
		})
	}
}

// A block's own faults are the layout reader's, and are reported as that rather
// than as a block this build does not resolve: the adopter has a block to
// rename or a record to point at it, and "this build does not resolve one" is
// not theirs to act on until the layout is right.
func TestABlocksFaultsAreTheReadersAndNotRefusals(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		layout string
		want   any
		at     diag.Span
	}{
		"a block no record names": {
			layout: ledger + "(definitions-block SUMMARY\n  (entry 01 LEDGER-SUMMARY (pic \"X(80)\")))\n",
			want:   new(*layoutmodel.UnnamedBlockError),
			at:     diag.Span{Line: 15, Column: 20},
		},
		"a block defined twice": {
			layout: ledger + "(definitions-block TRAILER\n  (entry 01 LEDGER-TRAILER (pic \"X(80)\")))\n",
			want:   new(*layoutmodel.DuplicateBlockError),
			at:     diag.Span{Line: 15, Column: 20},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path, layout := ledgerProject(t, test.layout, nil)

			_, err := project.Load(path)
			if !errors.As(err, test.want) {
				t.Fatalf("want a %T; got:\n%s", test.want, diag.Render(err))
			}

			diagnostics := diag.Diagnostics(err)
			if len(diagnostics) != 1 {
				t.Fatalf("want the reader's one diagnostic and no refusal beside it; got:\n%s", diag.Render(err))
			}

			test.at.File = layout
			if got := diagnostics[0].String(); !strings.HasPrefix(got, test.at.String()+": ") {
				t.Errorf("the fault is %q, want it at the block's name, %s", got, test.at)
			}
		})
	}
}

// A manifest whose layout is a definitions file is told so in one diagnostic,
// with a span at the manifest's `layout` and one at the file's first entry, and
// nothing else about the file: not one misplaced entry per run, and not a
// missing encoding, framing, record or sequence.
func TestADefinitionsFileNamedAsTheLayoutIsOneDiagnostic(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	layout := filepath.Join(dir, "posting.cpy.sexpr")

	write(t, layout, `;; The vendor's posting record.
(entry 01 POSTING-RECORD)
  (entry 05 PST-TYPE (pic "X"))
  (entry 05 PST-ACCOUNT (pic "X(10)"))
`)
	write(t, filepath.Join(dir, manifest.Name), `{
  "layout": "posting.cpy.sexpr",
  "generators": [{"name": "go", "out": "gen"}]
}`)

	_, err := project.Load(filepath.Join(dir, manifest.Name))

	var named *project.DefinitionsFileAsLayoutError
	if !errors.As(err, &named) {
		t.Fatalf("want a DefinitionsFileAsLayoutError; got:\n%s", diag.Render(err))
	}

	diagnostics := diag.Diagnostics(err)
	if len(diagnostics) != 1 {
		t.Fatalf("want one diagnostic; got:\n%s", diag.Render(err))
	}

	spans := diagnostics[0].Spans
	if len(spans) != 2 {
		t.Fatalf("want two spans; got:\n%s", diag.Render(err))
	}

	if want := (diag.Span{File: filepath.Join(dir, manifest.Name), Line: 2, Column: 13}); spans[0] != want {
		t.Errorf("the first span is %s, want the manifest's layout, at %s", spans[0], want)
	}

	if spans[1].File != layout || spans[1].Line != 2 || spans[1].Column != 2 {
		t.Errorf("the second span is %s, want the file's first entry, at %s:2:2", spans[1], layout)
	}

	if !strings.Contains(diagnostics[0].Message, "is a definitions file rather than a layout") {
		t.Errorf("the diagnostic does not say what was found: %s", diagnostics[0].Message)
	}
}

// A layout whose entries sit among its other forms is a layout, and the
// schema's to report: one diagnostic for the run of entries, and no claim that
// the whole file is a definitions file.
func TestEntriesAmongALayoutsFormsAreTheSchemas(t *testing.T) {
	t.Parallel()

	path, _ := schemaProject(t, orders+`(entry 01 ORDER-TRAILER)
  (entry 05 OT-COUNT (pic "9(4)"))
`)

	_, err := project.Load(path)

	var named *project.DefinitionsFileAsLayoutError
	if errors.As(err, &named) {
		t.Fatalf("a layout holding entries among its forms reads as a definitions file:\n%s", diag.Render(err))
	}

	faults := schemaFaults(t, err)
	if len(faults) != 1 || !strings.Contains(faults[0].Message, `form "entry" is not a top-level form of a layout`) {
		t.Errorf("want the schema's one diagnostic about the run of entries; got:\n%s", diag.Render(err))
	}
}

// A block whose entries the schema refuses is reported in the schema's words,
// and not refused as unresolved beside it: the adopter has a misspelled clause
// to fix, and "this build does not resolve one" is not theirs to act on until
// the block is well formed.
func TestAMisshapenBlockIsTheSchemasAlone(t *testing.T) {
	t.Parallel()

	path, _ := schemaProject(t, orders+`(definitions-block TRAILER
  (entry 01 ORDER-TRAILER (picture "X(8)")))
`)

	_, err := project.Load(path)
	if err == nil {
		t.Fatal("a layout holding a misshapen block resolves")
	}

	faults := schemaFaults(t, err)
	if len(faults) != 1 || !strings.Contains(faults[0].Message, `clause of form "entry"`) {
		t.Errorf("want the schema's one diagnostic about the clause; got:\n%s", diag.Render(err))
	}
}
