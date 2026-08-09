// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"fmt"
	"strings"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// A fault here names as much of the record, the repeating group the variant sits
// in, the redefined item and the arm as the fault is about. docs/ir/SPEC.md asks
// for those by name rather than for a generic width error, and the reason is
// what an adopter does next — a message saying two extents differ sends them
// looking through a copybook for two numbers, and one naming the entry, the
// table it is in and the alternative that does not fit sends them to the line.
//
// The two that name fewer name fewer because they are about fewer.
// [ArmCountError] is about a variant that has no arms, so there is no arm to
// name, and [UnknownAlternativeError] is about a name that matches nothing under
// the redefined item, which is true wherever that item sits.

// ArmExtentError is an arm that needs more bytes than the item it redefines.
//
// Every arm of a variant covers the same bytes, and a shorter alternative is
// padded with slack to the extent the redefined item reserved. A longer one
// cannot be made to agree the same way: the storage an occurrence has is what
// the copybook gave the redefined item, and a dialect lenient enough to let a
// redefinition grow the group holding it cannot grow one entry of a table
// without moving every entry after it.
type ArmExtentError struct {
	// Pos is the arm's entry in the copybook.
	Pos diag.Span

	// Record is the record being resolved.
	Record string

	// Group is the innermost repeating group the variant sits in.
	Group string

	// Redefined is the item the copybook redefines: the variant's first
	// alternative, and what fixes its extent.
	Redefined string

	// Arm is the alternative that does not fit.
	Arm string

	// Extent is the bytes the arm needs, and Want the bytes it has.
	Extent int
	Want   int
}

// Error implements the error interface.
func (e *ArmExtentError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *ArmExtentError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s, %s of the repeating group %s occupies %d bytes, more than the %d bytes of %s it redefines, so the arms of the variant cannot be made to agree",
			e.Record, e.Arm, e.Group, e.Extent, e.Want, e.Redefined),
		Spans: []diag.Span{e.Pos},
	}
}

// ArmVariableCountError is an item inside an arm whose repetition count is read
// out of the record rather than written in the copybook.
//
// A variant contributes a constant term to the position sum, which is what keeps
// every item behind it where it was and every occurrence of the enclosing group
// the same width. An OCCURS ... DEPENDING ON inside an arm makes the arm's
// extent move with data, and there is then no constant for the arms to agree on.
type ArmVariableCountError struct {
	// Pos is the entry carrying the DEPENDING ON phrase.
	Pos diag.Span

	// Record is the record being resolved.
	Record string

	// Group is the innermost repeating group the variant sits in.
	Group string

	// Redefined is the item the copybook redefines.
	Redefined string

	// Arm is the alternative the item is in.
	Arm string

	// Item is the repeating item, and Count the item its count is read from.
	Item  string
	Count string
}

// Error implements the error interface.
func (e *ArmVariableCountError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *ArmVariableCountError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s, %s under the arm %s of the variant on %s in the repeating group %s occurs a number of times read from %s, and an arm's extent has to be a constant",
			e.Record, e.Item, e.Arm, e.Redefined, e.Group, e.Count),
		Spans: []diag.Span{e.Pos},
	}
}

// ArmPredicateError is an arm of a variant with two or more alternatives that
// nothing selects.
//
// An arm chosen by nothing would match every occurrence, which leaves it the
// only arm — which is the single-alternative redefine, and that resolves to its
// items with no variant at all.
type ArmPredicateError struct {
	// Pos is the arm's entry in the copybook.
	Pos diag.Span

	// Record is the record being resolved.
	Record string

	// Group is the innermost repeating group the variant sits in.
	Group string

	// Redefined is the item the copybook redefines.
	Redefined string

	// Arm is the alternative nothing selects.
	Arm string
}

// Error implements the error interface.
func (e *ArmPredicateError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *ArmPredicateError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s, the arm %s of the variant on %s in the repeating group %s is selected by nothing",
			e.Record, e.Arm, e.Redefined, e.Group),
		Spans: []diag.Span{e.Pos},
	}
}

// ArmCountError is a layout that names a redefine inside a repeating group and
// then names no alternative of it.
//
// Naming one is not this fault and the message says so: one alternative says
// every occurrence takes it, and resolves to its items with no variant at all.
// Naming none says nothing, which leaves the alternation unresolved in exactly
// the way naming the item was meant to settle.
type ArmCountError struct {
	// Pos is the redefined item's entry in the copybook.
	Pos diag.Span

	// Record is the record being resolved.
	Record string

	// Group is the innermost repeating group the variant sits in.
	Group string

	// Redefined is the item the copybook redefines.
	Redefined string
}

// Error implements the error interface.
func (e *ArmCountError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *ArmCountError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s, nothing is named as an alternative of %s in the repeating group %s: name one to say every occurrence takes it, or two or more with a predicate on each to tell them apart",
			e.Record, e.Redefined, e.Group),
		Spans: []diag.Span{e.Pos},
	}
}

// UndiscriminatedRedefineError is a REDEFINES inside a repeating group that the
// layout says nothing about.
//
// The one thing a layout says about a redefine is which alternative to read, and
// inside a repeating group that is a `discriminate-variant` form. Reading the
// copybook's own first alternative instead would be a default, and a default
// here is a record read as the wrong alternative in every entry of the table
// with nothing in the file to disagree with it.
type UndiscriminatedRedefineError struct {
	// Pos is the redefined item's entry in the copybook.
	Pos diag.Span

	// Record is the record being resolved.
	Record string

	// Group is the innermost repeating group the redefine sits in.
	Group string

	// Redefined is the item the copybook redefines.
	Redefined string

	// Names are the items redefining it, which are the alternatives the
	// layout has to choose among.
	Names []string
}

// Error implements the error interface.
func (e *UndiscriminatedRedefineError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *UndiscriminatedRedefineError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s, nothing says which alternative of %s to read in the repeating group %s: %s redefines it, and a redefine inside a table is chosen once per occurrence",
			e.Record, e.Redefined, e.Group, joinAnd(e.Names)),
		Spans: []diag.Span{e.Pos},
	}
}

// UnknownAlternativeError is a layout naming an alternative the copybook does
// not declare over those bytes.
type UnknownAlternativeError struct {
	// Pos is the redefined item's entry in the copybook.
	Pos diag.Span

	// Record is the record being resolved.
	Record string

	// Redefined is the item the copybook redefines.
	Redefined string

	// Alternative is the name the layout wrote.
	Alternative string

	// Names are the alternatives the copybook does declare, in the order it
	// declares them.
	Names []string
}

// Error implements the error interface.
func (e *UnknownAlternativeError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
//
// The list of alternatives is built from [UnknownAlternativeError.Names] here
// rather than taken from the caller, so that what the message says the copybook
// declares is a property of the error and not of what whoever raised it
// remembered to write. It is in the message rather than in a span's note because
// there is one place to point at — the redefined item — and a note belongs to
// the second span of a diagnostic that names two.
func (e *UnknownAlternativeError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s, no alternative of %s is named %s: the copybook declares %s",
			e.Record, e.Redefined, e.Alternative, joinAnd(e.Names)),
		Spans: []diag.Span{e.Pos},
	}
}

// joinAnd renders a list the way a sentence does.
func joinAnd(names []string) string {
	switch len(names) {
	case 0:
		return "nothing"
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
