// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package diag

import (
	"fmt"
	"strconv"
)

// MissingCopybookError is a `copybook` child whose path names no file that can
// be read.
//
// It names the path as the layout spells it, which docs/layout/SPEC.md requires
// of it, and it carries one span: there is no file to point into, and a span
// invented for one would point at nothing. Where the path was looked for is the
// CLI's to explain, for the same reason resolving it is — so the cause is kept
// and reachable with [errors.As] rather than written into the message, which
// would name a path the adopter never wrote.
type MissingCopybookError struct {
	// Pos is the `copybook` child in the layout.
	Pos Span

	// Path is the path as the layout spells it.
	Path string

	// Err is what the read failed with.
	Err error
}

// Error implements the error interface.
func (e *MissingCopybookError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *MissingCopybookError) Diagnostic() Diagnostic {
	return Diagnostic{
		Message: fmt.Sprintf("there is no copybook to read at %q", e.Path),
		Spans:   []Span{e.Pos},
	}
}

// Unwrap gives up the read failure.
func (e *MissingCopybookError) Unwrap() error { return e.Err }

// UndeclaredItemError is a record's description — a copybook, a definitions
// file or a definitions block — that reads and does not declare the top-level item the record names.
//
// It is the cross-file diagnostic docs/layout/SPEC.md requires by name, in one
// shape whichever source holds the description ("A copybook that is not there,
// and an item that is not in it"): one span into the layout at the source, and
// one into the description. The
// second points at what is *there* — the `01`-levels the copybook does
// declare — because an absent item has no position, and the list an adopter
// needs in order to fix the record is the list of the ones they could have
// meant.
//
// A copybook declaring no `01`-level at all has nothing to point at but itself,
// and the span is then the file: leave [UndeclaredItemError.Copybook]'s line at
// zero and it renders as the file alone.
type UndeclaredItemError struct {
	// Pos is the record's source in the layout.
	Pos Span

	// Source is what the message calls the kind of description: "copybook",
	// "definitions file" or "definitions block". Empty reads as a copybook,
	// which is what every caller meant before a record could be held
	// anywhere else.
	Source string

	// Path is the description's path as the layout spells it, or the block's
	// name for a definitions block.
	Path string

	// Item is the top-level item as the layout spells it.
	Item string

	// Copybook is where in the copybook the second span points: the first
	// `01`-level it declares, or the file itself where it declares none.
	Copybook Span

	// Declares are the `01`-levels the copybook does declare, in the order it
	// declares them.
	Declares []string
}

// Error implements the error interface.
func (e *UndeclaredItemError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
//
// The second span's note is built from [UndeclaredItemError.Declares] here
// rather than taken from the caller, so that what the copybook half of the
// message says is a property of the error and not of what whoever raised it
// remembered to write.
func (e *UndeclaredItemError) Diagnostic() Diagnostic {
	copybook := e.Copybook

	if len(e.Declares) == 0 {
		copybook.Note = "it declares no 01-level at all"
	} else {
		copybook.Note = "it declares " + joinAnd(e.Declares)
	}

	source := e.Source
	if source == "" {
		source = "copybook"
	}

	// A block's name is a symbol of the layout and was never written in
	// quotes; a path is text, and is quoted as the layout writes it.
	named := strconv.Quote(e.Path)
	if source == "definitions block" {
		named = e.Path
	}

	return Diagnostic{
		Message: fmt.Sprintf("the %s %s declares no %s", source, named, e.Item),
		Spans:   []Span{e.Pos, copybook},
	}
}
