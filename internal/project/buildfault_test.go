// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package project

import (
	"errors"
	"fmt"
	"testing"

	cobol "github.com/Zaba505/cobol-go"
	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// Every `cobol-go` error type that carries a position is pointed at that
// position in the definitions file, found through errors.As — so wrapped as
// well as bare — and every other error falls back to the naming record's
// `definitions` source, with `cobol-go`'s message as a note. Most of the ten
// cannot be drawn from a definitions file through a run, because the judge
// refuses what `cobol-go`'s parser does before `copybook.Build` sees it; the
// list is held here instead, one type at a time.
func TestEveryPositionedCobolGoErrorPointsIntoTheFile(t *testing.T) {
	t.Parallel()

	at := cobol.Pos{Line: 7, Column: 3}

	for _, err := range []error{
		copybook.LevelError{Pos: at, Level: 50},
		copybook.LevelSequenceError{Pos: at, Level: 5, Reason: "has no record"},
		copybook.RenamesError{Pos: at, Name: "A", Reason: "renames nothing"},
		copybook.PictureError{Pos: at, Name: "A", Source: "9(", Err: errors.New("unterminated")},
		copybook.UsageError{Pos: at, Name: "A", Usage: "FOO"},
		copybook.LayoutError{Pos: at, Name: "A", Reason: "is not laid out"},
		copybook.OccursError{Pos: at, Name: "A", Reason: "occurs nothing"},
		copybook.RedefinesError{Pos: at, Name: "A", Reason: "redefines nothing"},
		copybook.OccurrenceError{Pos: at, Name: "A", Occurs: 2, Index: 3},
		copybook.DependingError{Pos: at, Name: "A", DependsOn: "N", Reason: "depends on nothing"},
	} {
		for _, wrapped := range []error{err, fmt.Errorf("laying out the record: %w", err)} {
			t.Run(fmt.Sprintf("%T", wrapped), func(t *testing.T) {
				t.Parallel()

				fault := buildFault(definitionsAt("posting.cpy.sexpr", "/srv/ledger/posting.cpy.sexpr"), wrapped)

				if want := (diag.Span{File: "/srv/ledger/posting.cpy.sexpr", Line: 7, Column: 3}); fault.Pos != want {
					t.Errorf("%T is at %s, want %s", err, fault.Pos, want)
				}

				d := fault.Diagnostic()
				if d.Message != wrapped.Error() || len(d.Spans) != 1 {
					t.Errorf("the diagnostic is %+v, want cobol-go's message at the one span", d)
				}

				if !errors.Is(fault, err) {
					t.Errorf("cobol-go's %T is not reachable through the fault", err)
				}
			})
		}
	}
}

// An error with no position is reported at the naming record's `definitions`
// source, with `cobol-go`'s message as a note, and the records after it each
// get a span of their own.
func TestAnUnpositionedCobolGoErrorFallsBackToTheRecord(t *testing.T) {
	t.Parallel()

	cause := copybook.DialectError{Field: "BinarySize", Reason: "names no staircase"}

	named := []Naming{
		{Record: "DEBIT", Pos: diag.Span{File: "ledger.sexpr", Line: 5, Column: 15}},
		{Record: "CREDIT", Pos: diag.Span{File: "ledger.sexpr", Line: 6, Column: 16}},
	}

	err := &DefinitionsFaultError{
		Err:     buildFault(definitionsAt("posting.cpy.sexpr", "/srv/ledger/posting.cpy.sexpr"), cause),
		Kind:    layoutmodel.DefinitionsFileSource,
		NamedBy: named,
	}

	d := err.Diagnostic()

	if want := `cobol-go does not build the definitions file "posting.cpy.sexpr"`; d.Message != want {
		t.Errorf("the message is %q, want %q", d.Message, want)
	}

	if len(d.Spans) != 3 {
		t.Fatalf("want the record, the note and the second record; got %+v", d.Spans)
	}

	if d.Spans[0] != named[0].Pos {
		t.Errorf("the fault is at %s, want the first record's source, %s", d.Spans[0], named[0].Pos)
	}

	if d.Spans[1].Stated() || d.Spans[1].Note != cause.Error() {
		t.Errorf("the note is %+v, want cobol-go's message and no place", d.Spans[1])
	}

	if d.Spans[2].Line != 6 || d.Spans[2].Note != "the definitions file is read as record CREDIT's description" {
		t.Errorf("the last span is %+v, want the second record", d.Spans[2])
	}

	var dialect copybook.DialectError
	if !errors.As(err, &dialect) {
		t.Error("cobol-go's error is not reachable through the fault")
	}
}

// In a block the same fallback reads the same way, naming the block bare as the
// layout writes it, and each note says the definitions block was read as the
// record's description.
func TestAnUnpositionedCobolGoErrorInABlockFallsBackToTheRecord(t *testing.T) {
	t.Parallel()

	cause := copybook.DialectError{Field: "BinarySize", Reason: "names no staircase"}

	named := []Naming{
		{Record: "DEBIT", Pos: diag.Span{File: "ledger.sexpr", Line: 5, Column: 15}},
		{Record: "CREDIT", Pos: diag.Span{File: "ledger.sexpr", Line: 6, Column: 16}},
	}

	err := &DefinitionsFaultError{
		Err:     buildFault(blockAt("POSTING", "ledger.sexpr"), cause),
		Kind:    layoutmodel.DefinitionsBlockSource,
		NamedBy: named,
	}

	d := err.Diagnostic()

	if want := "cobol-go does not build the definitions block POSTING"; d.Message != want {
		t.Errorf("the message is %q, want %q", d.Message, want)
	}

	if len(d.Spans) != 3 || d.Spans[0] != named[0].Pos {
		t.Fatalf("want the first record, the note and the second record; got %+v", d.Spans)
	}

	if d.Spans[2].Note != "the definitions block is read as record CREDIT's description" {
		t.Errorf("the last span's note is %q, want it to name the block and the second record", d.Spans[2].Note)
	}
}
