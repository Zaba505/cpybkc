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
	"github.com/Zaba505/cpybkc/internal/layoutschema"
	"github.com/Zaba505/cpybkc/internal/manifest"
	"github.com/Zaba505/cpybkc/internal/project"
)

// schemaProject writes a project whose layout is body, over a copybook that
// declares ORDER-REC, and hands back the manifest's path and the layout's.
//
// The copybook is real so that a test here fails for what its layout says and
// for nothing else: a run held back by the schema opens no copybook, and one
// that was not held back would otherwise report a missing file instead of
// resolving.
func schemaProject(t *testing.T, body string) (string, string) {
	t.Helper()

	dir := t.TempDir()

	write(t, filepath.Join(dir, "orders.cpy"), fixed(
		"01  ORDER-REC.",
		"    05  ORDER-ID      PIC X(4).",
		"    05  ORDER-QTY     PIC X(4).",
	))

	layout := filepath.Join(dir, "orders.sexpr")
	write(t, layout, body)
	write(t, filepath.Join(dir, manifest.Name), `{"layout": "orders.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	return filepath.Join(dir, manifest.Name), layout
}

// orders is a layout every test here starts from, which resolves as it stands.
// Each test adds the one form it is about.
const orders = `(encoding
  (charset cp037) (sign-convention ebcdic)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 8))
(record ORDER (copybook "orders.cpy" ORDER-REC))
(discriminate ORDER single-record-type)
(sequence (* ORDER))
`

// schemaFaults is every fault in err that the schema check reported. A fault
// beside them that the schema check did not report fails the test, because a
// run that reported one is a run that read a layer it should not have.
func schemaFaults(t *testing.T, err error) []layoutschema.Diagnostic {
	t.Helper()

	var faults []layoutschema.Diagnostic

	var walk func(error)
	walk = func(err error) {
		if d, ok := err.(layoutschema.Diagnostic); ok {
			faults = append(faults, d)

			return
		}

		if joined, ok := err.(interface{ Unwrap() []error }); ok {
			for _, e := range joined.Unwrap() {
				walk(e)
			}

			return
		}

		t.Errorf("a fault the schema check did not report: %v", err)
	}

	walk(err)

	return faults
}

// TestTheBaseLayoutResolves is the floor under the tests below: each of them is
// this layout with one form added, so a fault they report is the added form's.
func TestTheBaseLayoutResolves(t *testing.T) {
	t.Parallel()

	path, _ := schemaProject(t, orders)

	if _, err := project.Load(path); err != nil {
		t.Fatalf("the base layout does not resolve:\n%s", diag.Render(err))
	}
}

// TestAMisspelledFormIsRefusedOnceInTheSchemasWords is the story's headline.
//
// Before the schema check ran on a run path, `renme` was skipped by every layer
// reader — each reads the tags it owns and nothing else — so a misspelled
// rename was a rename silently not applied. It is now one diagnostic, at the
// tag, naming it.
func TestAMisspelledFormIsRefusedOnceInTheSchemasWords(t *testing.T) {
	t.Parallel()

	path, layout := schemaProject(t, orders+`(renme ORDER "Order")
`)

	_, err := project.Load(path)
	if err == nil {
		t.Fatal("a layout carrying a misspelled rename resolves")
	}

	faults := schemaFaults(t, err)
	if len(faults) != 1 {
		t.Fatalf("got %d diagnostics, want one:\n%s", len(faults), diag.Render(err))
	}

	fault := faults[0]

	if want := (diag.Span{File: layout, Line: 8, Column: 2}); fault.Span != want {
		t.Errorf("the diagnostic is at %s, want the tag at %s", fault.Span, want)
	}

	if !strings.Contains(fault.Message, `"renme"`) {
		t.Errorf("the diagnostic does not name the tag: %s", fault)
	}
}

// TestTheSchemaCheckReachesNestedForms is the "whole layout" half of the first
// criterion: a child the schema does not admit is the schema's to report, not
// only a top-level tag. It is what lets a block's body, once the schema declares
// one (#400), be checked with no change to the run.
func TestTheSchemaCheckReachesNestedForms(t *testing.T) {
	t.Parallel()

	path, layout := schemaProject(t, strings.Replace(orders, "(lrecl 8)", "(lrecl 8) (lrec 8)", 1))

	_, err := project.Load(path)
	if err == nil {
		t.Fatal("a framing form carrying a misspelled child resolves")
	}

	faults := schemaFaults(t, err)
	if len(faults) != 1 {
		t.Fatalf("got %d diagnostics, want one:\n%s", len(faults), diag.Render(err))
	}

	if want := (diag.Span{File: layout, Line: 4, Column: 31}); faults[0].Span != want {
		t.Errorf("the diagnostic is at %s, want the child's tag at %s", faults[0].Span, want)
	}

	if !strings.Contains(faults[0].Message, `"lrec"`) {
		t.Errorf("the diagnostic does not name the child: %s", faults[0])
	}
}

// TestADiscriminatorNamingNoRecordIsTheSchemasReference is a fault both checks
// can see. A reference naming no record is something a declaration states, so
// it is the schema's alone, and the reader's [layoutmodel.UnknownRecordError] —
// which still exists, for a caller reading without the schema — is not reported
// beside it.
func TestADiscriminatorNamingNoRecordIsTheSchemasReference(t *testing.T) {
	t.Parallel()

	path, layout := schemaProject(t, orders+`(discriminate GHOST single-record-type)
`)

	_, err := project.Load(path)
	if err == nil {
		t.Fatal("a discriminator naming no record resolves")
	}

	var unknown *layoutmodel.UnknownRecordError
	if errors.As(err, &unknown) {
		t.Errorf("the reader reported the schema's fault a second time:\n%s", diag.Render(err))
	}

	faults := schemaFaults(t, err)
	if len(faults) != 1 {
		t.Fatalf("got %d diagnostics, want one:\n%s", len(faults), diag.Render(err))
	}

	if want := (diag.Span{File: layout, Line: 8, Column: 15}); faults[0].Span != want {
		t.Errorf("the diagnostic is at %s, want the name at %s", faults[0].Span, want)
	}

	if !strings.Contains(faults[0].Message, "record form this layout defines") {
		t.Errorf("the diagnostic is not the schema's reference fault: %s", faults[0])
	}
}

// TestASchemaFaultHoldsBackEveryReaderFault is the rule's second half: when the
// schema check reports anything, no layer is read, so a fault only a reader can
// find waits for the layout the schema admits.
//
// The reader-only fault is a second discriminator for one record, which counts
// one form against another and is therefore no declaration's to state. The
// first subtest shows it is a fault on its own, so its absence in the second is
// the rule and not a layout that was never wrong.
func TestASchemaFaultHoldsBackEveryReaderFault(t *testing.T) {
	t.Parallel()

	twice := orders + `(discriminate ORDER single-record-type)
`

	t.Run("the reader-only fault alone", func(t *testing.T) {
		t.Parallel()

		path, _ := schemaProject(t, twice)

		_, err := project.Load(path)

		var duplicate *layoutmodel.DuplicateDiscriminatorError
		if !errors.As(err, &duplicate) {
			t.Fatalf("a second discriminator for one record is not the reader's fault:\n%s", diag.Render(err))
		}
	})

	t.Run("beside a schema fault", func(t *testing.T) {
		t.Parallel()

		path, _ := schemaProject(t, twice+`(renme ORDER "Order")
`)

		_, err := project.Load(path)
		if err == nil {
			t.Fatal("a layout wrong twice over resolves")
		}

		faults := schemaFaults(t, err)
		if len(faults) != 1 {
			t.Fatalf("got %d diagnostics, want the schema's one:\n%s", len(faults), diag.Render(err))
		}

		if !strings.Contains(faults[0].Message, `"renme"`) {
			t.Errorf("the one diagnostic is not the misspelled tag: %s", faults[0])
		}
	})
}
