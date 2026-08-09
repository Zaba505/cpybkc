// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"fmt"
	"strings"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
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

// IncompleteProfileError is a record handed to [Resolve] with an encoding
// profile stating fewer than all four axes.
//
// `codec/SPEC.md` requires all four from its caller and forbids a default for
// any of them, because every one fails *silently* when it is wrong — a charset
// yields a plausible string, a byte order a plausible number — and none is
// recoverable from the file with certainty. Completing the profile here would be
// that guess made once and applied to every field of the record.
//
// It points at no file, which is the one fault in this package that does not.
// The profile is written in a layout rather than in a copybook, and
// `layoutmodel` has already refused a layout that states three axes, with the
// line: one that reaches here is a caller who assembled [Options] by hand, so a
// span would name a copybook that is not wrong or a layout line that says the
// right thing.
type IncompleteProfileError struct {
	// Record is the record being resolved.
	Record string

	// Axes are the axes the profile does not state, in docs/layout/SPEC.md's
	// order.
	Axes []layoutmodel.Axis
}

// Error implements the error interface.
func (e *IncompleteProfileError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *IncompleteProfileError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"the encoding profile for record %s states no %s, and an encoding axis has no default",
			e.Record, joinAnd(axisNames(e.Axes))),
	}
}

// UnresolvedEncodingError is a field that left resolution with an encoding axis
// unset.
//
// It is the completeness assertion docs/ir/SPEC.md's "The encoding profile,
// applied" asks a producer for, and it is about this package rather than about
// anything an adopter wrote: a complete profile with overrides applied over it
// is complete, so nothing a layout or a copybook can say produces one. It exists
// because the requirement is on what reaches a generator, where the only repair
// available is the one that document forbids — a consumer **MUST** treat a field
// missing an axis as a malformed descriptor and **MUST NOT** fill it in — so an
// axis that escapes here escapes for good.
type UnresolvedEncodingError struct {
	// Pos is the field's entry in the copybook.
	Pos diag.Span

	// Record is the record being resolved.
	Record string

	// Item is the field the axis is unset on.
	Item string

	// Axes are the axes it does not state, in docs/layout/SPEC.md's order.
	Axes []layoutmodel.Axis
}

// Error implements the error interface.
func (e *UnresolvedEncodingError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *UnresolvedEncodingError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s, %s was resolved with no %s: every field carries all four encoding axes, and this is a bug in cpybkc rather than in the copybook or the layout",
			e.Record, e.Item, joinAnd(axisNames(e.Axes))),
		Spans: []diag.Span{e.Pos},
	}
}

// LRECLExtentError is a record type whose extent the dataset's `lrecl` does not
// admit.
//
// It is only ever the record that is too long. A record type that stops short of
// an exact `lrecl` is padded rather than reported — those bytes are in the file
// whatever the copybook says, and carrying them as slack is what makes the next
// record start where the dataset puts it — so what is left here is a record type
// with more bytes than the dataset has room for, which no slack node can take
// away.
//
// The two bounds are one error and not two because an adopter fixes them the
// same way, and the message says which it is: under an exact bound every record
// type accounts for all of `lrecl`, and under a maximum one the dataset admits
// anything up to it.
//
// It carries two spans, and the first is the layout's. The number comes from the
// dataset the adopter wrote down and the extent from a copybook they may not
// own, so a diagnostic naming only the copybook names the half they cannot
// change.
type LRECLExtentError struct {
	// Pos is the `lrecl` form in the layout.
	Pos diag.Span

	// Item is the record's entry in the copybook.
	Item diag.Span

	// Record is the record being resolved.
	Record string

	// Alternatives are the alternatives chosen at each redefine outside a
	// repeating group, and are empty for a copybook holding none. They are
	// what tells one resolution of a record from another, which share a name.
	Alternatives []string

	// Bound is what the framing requires of the extent.
	Bound layoutmodel.LRECLBound

	// Extent is the bytes the record type occupies, and LRECL the length the
	// dataset states.
	Extent int
	LRECL  int
}

// Error implements the error interface.
func (e *LRECLExtentError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *LRECLExtentError) Diagnostic() diag.Diagnostic {
	requirement := "and a record type of a fixed-length dataset accounts for all of it and no more"
	if e.Bound == layoutmodel.LRECLMaximum {
		requirement = "and the dataset admits no record longer than that"
	}

	item := e.Item
	item.Note = fmt.Sprintf("%s is declared here", e.Record)

	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"record %s occupies %d bytes, the dataset's lrecl is %d, %s",
			recordNamed(e.Record, e.Alternatives), e.Extent, e.LRECL, requirement),
		Spans: []diag.Span{e.Pos, item},
	}
}

// VariableExtentError is a record type whose extent moves with a count, on a
// fixed-length dataset.
//
// On such a dataset the next record begins a fixed distance on whatever the
// record was, so a record type has one extent and not one per count. A record
// type of a fixed part, a table and a pad meets that at one count and misses it
// at every other, and the pad cannot take up the difference because a slack node
// carries a width (docs/ir/SPEC.md, "A variable record does not fit a
// fixed-length dataset", #92).
//
// It names the record and the repeating item, which that section asks for by
// name rather than a generic framing error: the adopter's way out is a record
// type per count value, and finding it starts at the table.
//
// It is keyed on the framing rather than on the `lrecl`, because the rule holds
// whether or not a layout states one. Under RECFM F and FB a layout must state
// one, so in practice the two arrive together; the check does not depend on it.
type VariableExtentError struct {
	// Pos is the `framing` form in the layout.
	Pos diag.Span

	// Item is the repeating item's entry in the copybook.
	Item diag.Span

	// Record is the record being resolved.
	Record string

	// RECFM is the record format the layout writes, which is what makes the
	// dataset fixed-length.
	RECFM layoutmodel.RECFM

	// Table is the repeating item, and Count the item its count is read from.
	Table string
	Count string
}

// Error implements the error interface.
func (e *VariableExtentError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *VariableExtentError) Diagnostic() diag.Diagnostic {
	item := e.Item
	item.Note = fmt.Sprintf("%s is declared here", e.Table)

	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s, %s occurs a number of times read from %s, and under recfm %s every record of the dataset is the same length",
			e.Record, e.Table, e.Count, e.RECFM),
		Spans: []diag.Span{e.Pos, item},
	}
}

// recordNamed is what a message calls a record, with the alternatives that tell
// one resolution of it from another where the copybook has any.
func recordNamed(record string, alternatives []string) string {
	if len(alternatives) == 0 {
		return record
	}
	return fmt.Sprintf("%s, reading %s", record, joinAnd(alternatives))
}

// axisNames is what a message calls a list of axes: the tag a layout writes each
// one as, which is the word an adopter would have typed.
func axisNames(axes []layoutmodel.Axis) []string {
	names := make([]string, 0, len(axes))
	for _, axis := range axes {
		names = append(names, axis.String())
	}
	return names
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
