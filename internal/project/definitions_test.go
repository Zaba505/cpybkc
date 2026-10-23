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
// posting is a definitions file and whose trailer is a block. A run that opened
// the copybook would report it missing, so a run reporting a layout fault alone
// is one that opened nothing.
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

// A block's name faults are the layout reader's, at the block's name, and a
// run reporting one opens nothing: the copybook the ledger names does not exist,
// and a run that opened it would have said so. A block no record names is
// still judged, and holding nothing wrong it adds nothing beside the reader's
// one diagnostic.
func TestABlocksNameFaultsAreTheReaders(t *testing.T) {
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
				t.Fatalf("want the reader's one diagnostic and nothing beside it; got:\n%s", diag.Render(err))
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
// and nothing is read past it: the adopter has a misspelled clause to fix, and
// whatever the block would have resolved to is not theirs to act on until it is
// well formed.
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
