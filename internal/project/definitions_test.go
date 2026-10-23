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
	"github.com/Zaba505/cpybkc/internal/manifest"
	"github.com/Zaba505/cpybkc/internal/project"
)

// A layout may hold a `definitions-block`, and this build checks it and stops
// there: nothing resolves a record held in one until #409, so each block is
// refused at its tag, saying so, rather than read past.
//
// The record beside the blocks names a copybook that does not exist. A run that
// opened it would report it missing, so a run reporting the blocks alone is one
// that opened nothing.
func TestADefinitionsBlockIsRefusedAtItsTagAndNothingIsOpened(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	layout := filepath.Join(dir, "ledger.sexpr")

	write(t, layout, `(encoding
  (charset cp037) (sign-convention ebcdic)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 80))
(record LEDGER (copybook "missing.cpy" LEDGER-REC))
(discriminate LEDGER single-record-type)
(sequence (* LEDGER))
(definitions-block TRAILER
  (entry 01 LEDGER-TRAILER)
    (entry 05 LT-TYPE (pic "X")))
(definitions-block HEADER
  (entry 01 LEDGER-HEADER (pic "X(80)")))
`)
	write(t, filepath.Join(dir, manifest.Name), `{"layout": "ledger.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	run, err := project.Load(filepath.Join(dir, manifest.Name))
	if err == nil {
		t.Fatalf("the project resolved a layout holding blocks nothing resolves: %v", run.Descriptor)
	}

	var missing *diag.MissingCopybookError
	if errors.As(err, &missing) {
		t.Errorf("the run opened the layout's copybook:\n%s", diag.Render(err))
	}

	diagnostics := diag.Diagnostics(err)
	if len(diagnostics) != 2 {
		t.Fatalf("want one diagnostic per block; got:\n%s", diag.Render(err))
	}

	for i, want := range []struct {
		name string
		at   diag.Span
	}{
		{name: "TRAILER", at: diag.Span{File: layout, Line: 8, Column: 2}},
		{name: "HEADER", at: diag.Span{File: layout, Line: 11, Column: 2}},
	} {
		if diagnostics[i].Spans[0] != want.at {
			t.Errorf("block %s is refused at %s, want its tag at %s", want.name, diagnostics[i].Spans[0], want.at)
		}

		if !strings.Contains(diagnostics[i].Message, want.name) {
			t.Errorf("the refusal does not name block %s: %s", want.name, diagnostics[i].Message)
		}
	}

	var refused *project.DefinitionsBlockError
	if !errors.As(err, &refused) {
		t.Fatalf("no DefinitionsBlockError in:\n%s", diag.Render(err))
	}

	if rendered := diag.Render(err); !strings.Contains(rendered, "definitions blocks are not resolved by this build") {
		t.Errorf("the refusal does not say blocks are unresolved:\n%s", rendered)
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
