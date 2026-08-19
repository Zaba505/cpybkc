// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"fmt"
	"strings"

	"github.com/Zaba505/cobol-go/picture"

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

// CharsetNoneError is `(charset none)` reaching an item whose bytes the charset
// does govern.
//
// `none` says an item's bytes are a payload rather than characters, and that is
// a reading only an alphanumeric DISPLAY item admits. On a zoned item the
// charset governs the digit zone and the byte values of a separate sign, so
// taking it away leaves the digits with nothing to be read through and the item
// with no reading at all. On an alphabetic or an edited item the PICTURE is
// itself a statement about characters — `PIC A` says letters and `PIC ZZ9.99`
// says where the zeroes are suppressed — so an item declared as one is
// characters by declaration and a payload cannot be what it holds.
//
// It is not raised where charset governs nothing. A packed, binary, COMP-6,
// floating-point, INDEX or POINTER item is unaffected by `none` exactly as it is
// unaffected by `cp037`, and that inertness is what lets an override name a
// group and reach the alphanumeric items under it without the group's numeric
// items becoming errors (docs/layout/SPEC.md, "A byte is not a character, and
// such an item has no charset", #275).
//
// A FILLER is not raised against either, whatever its PICTURE says. An item
// COBOL gives no data-name is one no accessor is generated for and no item
// reference can name, so its charset is never read and the diagnostic would name
// an item the adopter cannot address — leaving them nothing to do but delete the
// override that reaches the items they meant.
//
// It carries two spans, and the first is the layout's, for [LRECLExtentError]'s
// reason: the override is the line the adopter wrote and the copybook may not be
// theirs to change, so a diagnostic naming only the copybook names the half they
// cannot fix.
type CharsetNoneError struct {
	// Pos is the `encoding-override` form in the layout.
	Pos diag.Span

	// Item is the item's entry in the copybook.
	Item diag.Span

	// Record is the record being resolved.
	Record string

	// Name is the item the override reached, which is the elementary item
	// rather than the group the override may have named.
	Name string

	// Category is what the item's PICTURE declares it to be.
	Category picture.Category
}

// Error implements the error interface.
func (e *CharsetNoneError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
//
// The two categories fault for different reasons and the message says which,
// because they are fixed differently: a zoned item wants the override narrowed
// to the items that really are payload, and an edited or alphabetic one wants
// the copybook's own declaration believed.
func (e *CharsetNoneError) Diagnostic() diag.Diagnostic {
	fault := fmt.Sprintf("an item of category %s is characters by declaration", e.Category)
	if e.Category == picture.CategoryNumeric {
		fault = "a zoned decimal item's digit zone and separate sign are read through the charset," +
			" so it would be left with no reading at all"
	}

	item := e.Item
	item.Note = fmt.Sprintf("%s is declared here", e.Name)

	return diag.Diagnostic{
		Message: fmt.Sprintf("in record %s, charset none reaches %s, and %s", e.Record, e.Name, fault),
		Spans:   []diag.Span{e.Pos, item},
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

// UnstatedReadingError is a copybook carrying an `OCCURS DEPENDING ON` under a
// layout that did not say which reading its file was written under.
//
// The two readings are not two arithmetics over one shape: under `odoslide` the
// table's extent moves with the count and every item behind it moves with it,
// and under `noodoslide` the occurrences stand at the copybook's declared
// maximum, the items behind them are at constant offsets, and the count field
// governs no byte at all. Reading one file as the other is wrong at every item
// behind the table, silently, at every record.
//
// So there is nothing to fall back on and no side to take by default, which is
// what makes this a fault rather than a warning with a resolution attached. It
// names the table because that is where an adopter starts: the answer is a
// property of the compiler that wrote the file, spelled `ODOSLIDE`/`NOODOSLIDE`
// by Micro Focus, `odoslide` by GnuCOBOL and nothing at all by IBM Enterprise
// COBOL, which slides unconditionally.
type UnstatedReadingError struct {
	// Pos is the entry carrying the DEPENDING ON phrase.
	Pos diag.Span

	// Record is the record being resolved.
	Record string

	// Table is the repeating item, and Count the item its count is read from.
	Table string
	Count string
}

// Error implements the error interface.
func (e *UnstatedReadingError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *UnstatedReadingError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s, %s occurs a number of times read from %s, and the layout does not say whether an "+
				"item behind a table slides: write (copybook-reading (occurs-depending-on odoslide)) or "+
				"(copybook-reading (occurs-depending-on noodoslide)), whichever the compiler that wrote "+
				"the file was set to",
			e.Record, e.Table, e.Count),
		Spans: []diag.Span{e.Pos},
	}
}

// CountOccurrenceError is an `OCCURS DEPENDING ON` count that repeats, or that
// sits inside a group that repeats.
//
// Nothing in a repetition carries an occurrence number, so a reference that
// could reach a later occurrence names bytes the descriptor cannot say it meant
// — reading it as the first is a guess about intention that a consumer cannot
// detect is wrong (docs/ir/SPEC.md, "A reference names a field, not an
// occurrence of one").
//
// A count has a second reason the other two positions do not, and it is the one
// that decides the shape of everything above it: a count with a value per
// occurrence of its enclosing group is a group whose occurrences are not all the
// same width, so "the width of one occurrence times that count" stops being
// arithmetic and an aligned item inside such an occurrence needs a different
// number of padding bytes in each one — which a slack node, carrying a single
// width, cannot describe at all.
//
// The shape refused is the one an adopter expects: a count beside the table it
// governs, both inside a repeating group, meaning "the count in this occurrence
// governs this occurrence". It is an expectation rather than a file shape, and
// COBOL's own rules already turn it away.
type CountOccurrenceError struct {
	// Pos is the count field's entry in the copybook.
	Pos diag.Span

	// Record is the record being resolved.
	Record string

	// Count is the field the count is read from, and Table the repeating item
	// naming it.
	Count string
	Table string

	// Group is the innermost repeating group the count sits in, empty where
	// the count field is itself the thing that repeats.
	Group string
}

// Error implements the error interface.
func (e *CountOccurrenceError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *CountOccurrenceError) Diagnostic() diag.Diagnostic {
	repeats := fmt.Sprintf("%s repeats", e.Count)
	if e.Group != "" {
		repeats = fmt.Sprintf("%s sits in the repeating group %s", e.Count, e.Group)
	}

	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s, %s occurs a number of times read from %s, and %s: a count has one value per record, "+
				"because a count with one per occurrence makes the occurrences of its group different widths",
			e.Record, e.Table, e.Count, repeats),
		Spans: []diag.Span{e.Pos},
	}
}

// CountPositionError is an `OCCURS DEPENDING ON` count whose own position is not
// a constant, because some item ahead of it carries a repetition whose count is
// a reference.
//
// docs/ir/SPEC.md's "A count is in hand before the extent it decides" states two
// halves of one requirement — a count lies ahead of what it counts, and at a
// constant position — and this is the second. The first is refused before a
// record reaches this package at all: `cobol-go` will not lay out a copybook
// whose DEPENDING ON object is defined after the table it controls, because
// locating a trailing count needs the table's extent and the table's extent
// needs the count, and [Resolve] returns that fault unchanged rather than
// resolving the data-name a second time to restate it.
//
// A count behind some *other* variable table is readable — a walk in record
// order reaches that table's own count first — and is refused all the same. It
// is a count no compiler writes, Micro Focus states the rule for the clause
// flatly (*Data-name-1 must have a fixed location, and must not follow an item
// that contains an OCCURS DEPENDING ON clause*), and relaxing the restriction
// later costs no version while imposing it later would be breaking.
type CountPositionError struct {
	// Pos is the count field's entry in the copybook.
	Pos diag.Span

	// Record is the record being resolved.
	Record string

	// Count is the field the count is read from, and Table the repeating item
	// naming it.
	Count string
	Table string

	// Behind is the variable item the count sits behind.
	Behind string
}

// Error implements the error interface.
func (e *CountPositionError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *CountPositionError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s, %s occurs a number of times read from %s, and %s sits behind %s, whose own extent "+
				"moves with a count: a count lies ahead of what it counts and at a constant position",
			e.Record, e.Table, e.Count, e.Count, e.Behind),
		Spans: []diag.Span{e.Pos},
	}
}

// SharedCountBoundsError is two repeating items of one record naming one count
// whose declared ranges have no value in common.
//
// Sharing a count is admitted and is the plainest record with two variable
// tables in it: two tables under separate counts oblige a record to carry both
// count fields ahead of both tables, and one count ahead of two tables satisfies
// that with nothing arranged. A consumer decodes the field once and sizes both
// tables from that one value (docs/ir/SPEC.md, "One count may size two tables,
// and a writer refuses to choose").
//
// The declared bounds are the one place the second reference is not simply a
// second multiplication: each repetition carries its own, both bind the one
// value, and the range a record can actually carry is the overlap. Where the
// ranges do not overlap at all there is no count that sizes both tables, so
// every record of the descriptor is malformed data — and the alternative to
// rejecting the layout here is that diagnostic once per record for the life of
// the file.
type SharedCountBoundsError struct {
	// Pos is the count field's entry in the copybook.
	Pos diag.Span

	// Record is the record being resolved.
	Record string

	// Count is the field both repetitions name.
	Count string

	// Tables are the two repeating items, and Bounds the minimum and maximum
	// each one declares, in the same order.
	Tables []string
	Bounds [][2]int
}

// Error implements the error interface.
func (e *SharedCountBoundsError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *SharedCountBoundsError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s, %s occurs %s and %s occurs %s, and both counts are read from %s: no value sizes "+
				"both tables, so every record of this layout would be malformed",
			e.Record, e.Tables[0], occurrences(e.Bounds[0]), e.Tables[1], occurrences(e.Bounds[1]), e.Count),
		Spans: []diag.Span{e.Pos},
	}
}

// MissingCountError is [Record.At] handed no count for a table whose repetition
// names one.
//
// It is not an absent table and it is not zero occurrences. docs/ir/SPEC.md
// requires a consumer to report a count it cannot decode as a number rather than
// reading the group as absent, and a count field holding spaces is a real
// mainframe occurrence — read as zero it produces a record that parses and is
// wrong. So a count nobody supplied is refused here rather than defaulted, and a
// caller whose decode failed reports that failure instead of leaving the entry
// out.
//
// It points at no file, which it shares with [CountBoundsError] and with nothing
// else in this package: the copybook declared the table correctly and the layout
// chose a reading correctly, and what is wrong is one record's bytes.
type MissingCountError struct {
	// Record is the record being read.
	Record string

	// Count is the field the count is read from, and Table the repeating item
	// naming it.
	Count string
	Table string
}

// Error implements the error interface.
func (e *MissingCountError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *MissingCountError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s, %s occurs a number of times read from %s, and no count was decoded for it: a count "+
				"that would not decode is malformed data rather than zero occurrences",
			e.Record, e.Table, e.Count),
	}
}

// CountBoundsError is a decoded count outside the occurrences the copybook
// declared.
//
// The bounds are carried on a repetition for this check and for nothing else,
// and it is the only thing in reach that bounds a number decoded out of a file:
// a descriptor word stating the length a forbidden count implies agrees with the
// record's extent exactly, so the framing check passes on a record the copybook
// says cannot exist — which is what a file written against a later version of
// that copybook looks like. Under **unframed** there is nothing to disagree with
// it at all, and a corrupt count runs the read into the next record.
//
// A negative count is this fault and not one of its own. Every copybook's
// declared minimum is at least zero, so a negative value is below it, and what
// an adopter needs told is the range the count had to be in.
//
// One of these per repetition naming the count, rather than one per count.
// Two repeating items may name one field, each carries its own declared minimum
// and maximum, and both bind the one value, so a count admitted by one table and
// refused by the other is refused.
type CountBoundsError struct {
	// Record is the record being read.
	Record string

	// Count is the field the count is read from, and Table the repeating item
	// naming it.
	Count string
	Table string

	// Value is what was decoded, and Min and Max the occurrences the copybook
	// declares for this table.
	Value int
	Min   int
	Max   int
}

// Error implements the error interface.
func (e *CountBoundsError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *CountBoundsError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s, the count read from %s is %d, and %s occurs %s",
			e.Record, e.Count, e.Value, e.Table, occurrences([2]int{e.Min, e.Max})),
	}
}

// occurrences renders a declared range the way an OCCURS clause states one.
func occurrences(bounds [2]int) string {
	if bounds[0] == bounds[1] {
		return fmt.Sprintf("%d times", bounds[0])
	}

	return fmt.Sprintf("%d to %d times", bounds[0], bounds[1])
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
