// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package scaffold

import (
	"errors"
	"fmt"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// ErrNoCopybooks is [Derive] handed nothing to derive from.
//
// It is an ordinary error rather than a [diag.Error] because it is not a fault
// in anybody's file: it says the caller asked for a scaffold over no copybooks,
// which the command line refuses as a usage error before this package is
// reached.
var ErrNoCopybooks = errors.New("there is no copybook to derive a scaffold from")

// ErrNoDestination is [Write] handed nowhere to put the scaffold, for
// [ErrNoCopybooks]'s reason.
var ErrNoDestination = errors.New("name a file to write the scaffold to, or " + Stdout + " for standard output")

// Every fault here leads with the copybook or the destination it is about and
// says nothing else about where it is. There is no layout under `init` — that is
// the whole point of the command — so a path has one spelling rather than two,
// and the one it has is the one that was typed on the command line, which is
// what the person running it can go and correct.

// SourceError is a copybook `init` was given that cannot be read as COBOL this
// build understands.
//
// The cause is carried on a continuation rather than reworded: what `cobol-go`
// found is more specific than anything this package could say about it, and a
// continuation is how a second line reaches standard error without being read as
// a diagnostic of its own.
type SourceError struct {
	// Path is the --copybook value as it was typed.
	Path string

	// Err is what the reader said.
	Err error
}

// Error implements error.
func (e *SourceError) Error() string { return e.Diagnostic().String() }

// Unwrap returns the underlying fault, so that a caller can assert on whatever
// `cobol-go` raised.
func (e *SourceError) Unwrap() error { return e.Err }

// Diagnostic implements [diag.Error].
func (e *SourceError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: "this is not a copybook this build can read",
		Spans:   []diag.Span{{File: e.Path}, {Note: e.Err.Error()}},
	}
}

// NoRecordError is a copybook that parsed and declares nothing a `record` form
// could be written over.
//
// A copybook is a record description, and one holding no named 01-level
// contributes no record to a scaffold. Reporting it is what stops a run
// succeeding over a file the adopter expected records out of — a member holding
// nothing but level-77 work items is the ordinary way to arrive here, and it
// parses perfectly well.
type NoRecordError struct {
	// Path is the --copybook value as it was typed.
	Path string
}

// Error implements error.
func (e *NoRecordError) Error() string { return e.Diagnostic().String() }

// Diagnostic implements [diag.Error].
func (e *NoRecordError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: "this copybook declares no named 01-level, so there is no record to derive from it",
		Spans:   []diag.Span{{File: e.Path}},
	}
}

// NameCollisionError is two record types whose derived names are one symbol.
//
// docs/cli/SPEC.md, "How a combination's record name is chosen", requires the
// run to fail naming both rather than disambiguating: duplicate data names are
// legal COBOL, so this is reachable, and the only way past it is a name whose
// single property is that it differs from another invented one. Both places are
// named because the answer is to rename one of them, and the adopter cannot tell
// which without seeing the pair.
type NameCollisionError struct {
	// Name is the symbol both record types derived.
	Name string

	// Path and Item are the copybook and 01-level of the record type already
	// carrying it; Other and OtherItem are the ones that collided with it.
	Path      string
	Item      string
	Other     string
	OtherItem string
}

// Error implements error.
func (e *NameCollisionError) Error() string { return e.Diagnostic().String() }

// Diagnostic implements [diag.Error].
func (e *NameCollisionError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"the record name %s is derived twice, here from the 01-level %s and again from %s below, "+
				"and a layout cannot carry one name on two record types",
			e.Name, e.Item, e.OtherItem,
		),
		Spans: []diag.Span{
			{File: e.Path},
			{File: e.Other, Note: "and again here"},
		},
	}
}

// UnnamedAlternativeError is a REDEFINES over an item a layout could not name.
//
// A layout says which alternative a record is with an item reference, and a
// reference is a path of names — so an alternative written FILLER, or one under
// a group whose data-name was omitted, is one no layout can name. The scaffold
// refuses rather than writing a reference with a hole in it, which would parse
// and then be reported against a line cpybkc wrote.
type UnnamedAlternativeError struct {
	// Path is the --copybook value as it was typed, and Item the 01-level.
	Path string
	Item string

	// Line and Column are where the unnamed item is declared.
	Line   int
	Column int
}

// Error implements error.
func (e *UnnamedAlternativeError) Error() string { return e.Diagnostic().String() }

// Diagnostic implements [diag.Error].
func (e *UnnamedAlternativeError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"%s overlays a run of bytes with an item that has no data-name, "+
				"and a layout names an alternative with an item reference",
			e.Item,
		),
		Spans: []diag.Span{{File: e.Path, Line: e.Line, Column: e.Column}},
	}
}

// DestinationError is a --out path with something already at it.
//
// docs/cli/SPEC.md, "Where the scaffold is written, and why nothing is ever
// overwritten": whatever is there — a regular file, a directory, a symbolic
// link, including one that dangles — the run fails and nothing is written. Both
// paths are named because a relative path in a shared tree sends a reader to the
// wrong directory, and the path as typed is the one they can find in what they
// wrote.
type DestinationError struct {
	// Path is the --out value as it was typed, and Absolute the path cpybkc
	// looked at.
	Path     string
	Absolute string
}

// Error implements error.
func (e *DestinationError) Error() string { return e.Diagnostic().String() }

// Diagnostic implements [diag.Error].
func (e *DestinationError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: "there is already something here, and cpybkc never writes over what it finds; " +
			"remove it, or name somewhere else",
		Spans: []diag.Span{{File: e.Path}, {Note: "cpybkc looked at " + e.Absolute}},
	}
}

// WriteError is a destination that was free and still could not be written.
type WriteError struct {
	// Path is the --out value as it was typed, or [Stdout].
	Path string

	// Err is what the write failed with.
	Err error
}

// Error implements error.
func (e *WriteError) Error() string { return e.Diagnostic().String() }

// Unwrap returns the underlying fault.
func (e *WriteError) Unwrap() error { return e.Err }

// Diagnostic implements [diag.Error].
//
// Standard output is named in the message rather than led with as a file,
// because "-" is not a place a reader can go and look at.
func (e *WriteError) Diagnostic() diag.Diagnostic {
	if e.Path == Stdout {
		return diag.Diagnostic{
			Message: "the scaffold could not be written to standard output",
			Spans:   []diag.Span{{Note: e.Err.Error()}},
		}
	}

	return diag.Diagnostic{
		Message: "the scaffold could not be written here",
		Spans:   []diag.Span{{File: e.Path}, {Note: e.Err.Error()}},
	}
}
