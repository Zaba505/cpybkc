// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Zaba505/cpybkc/internal/layout"
)

// faults is the fault list a layer reader accumulates.
//
// Every reader here collects rather than returning the first, for the reason
// [github.com/Zaba505/cpybkc/internal/layout]'s parser gives: a generated layout
// is generated wrong in the same way in many places at once, and a reader that
// reports one fault per run is a reader run once per fault. It is one type
// rather than a field on each reader so that "keep reading after a fault" is
// decided once.
type faults struct {
	errs []error
}

// fail records a fault. Reading continues after one, because the point of
// collecting them is to report the second.
func (f *faults) fail(err error) {
	f.errs = append(f.errs, err)
}

// ProfileCountError is a layout that does not carry exactly one `encoding` form.
//
// Both halves are faults an adopter can act on and neither has a default:
// docs/layout/SPEC.md requires exactly one, a layout carrying none states no
// axes at all, and a layout carrying two leaves the order they were written in
// deciding which one governs the file.
type ProfileCountError struct {
	// Pos is the second `encoding` form where there is one, and the start of
	// the file where there is none — a form a layout is missing has no position
	// of its own, and the start of the file is where the adopter has to add it.
	Pos layout.Pos

	// Count is how many the layout carries.
	Count int
}

// Error implements the error interface.
func (e *ProfileCountError) Error() string {
	return fmt.Sprintf("%s: a layout carries exactly one encoding form, and this one carries %d", e.Pos, e.Count)
}

// MissingAxisError is an axis the encoding profile does not state.
//
// It names the axis, which docs/layout/SPEC.md requires: "An implementation MUST
// NOT supply a default for a missing axis, and MUST report the missing one by
// name." One of these is reported per missing axis rather than one naming them
// all, so that a profile missing two axes is two things to fix rather than one
// message to read twice.
//
// An axis is missing when the profile says nothing about it. An axis stated with
// a value the axis does not admit is an [AxisValueError] and not this: the line
// is there, and calling it absent would send an adopter to write one they have
// already written.
type MissingAxisError struct {
	// Pos is the `encoding` form, which is where the axis has to be written.
	Pos layout.Pos

	// Axis is the one nobody stated.
	Axis Axis
}

// Error implements the error interface.
func (e *MissingAxisError) Error() string {
	return fmt.Sprintf(
		"%s: the encoding profile states no %s; all four axes are required and none of them has a default",
		e.Pos, e.Axis,
	)
}

// AxisValueError is a value written for an axis that the axis does not admit.
//
// The message names the whole set, because every one of these is a spelling
// question an adopter can answer from it — a code page nobody has a table for, a
// sign convention spelled as `codec/SPEC.md`'s Go identifier rather than as a
// layout spells it, `little` for `little-endian`.
type AxisValueError struct {
	// Pos is the value, not the form carrying it.
	Pos layout.Pos

	// Axis is the axis it was written for.
	Axis Axis

	// Value is what was written.
	Value string
}

// Error implements the error interface.
func (e *AxisValueError) Error() string {
	return fmt.Sprintf("%s: %s is one of %s, and this one says %s", e.Pos, e.Axis, and(e.Axis.Values()), quote(e.Value))
}

// AxisFormError is an axis form that does not carry exactly one symbol naming
// its value.
//
// `(charset)` states nothing, `(charset cp037 cp500)` states two things, and
// `(charset "cp037")` writes a code page as text where the format writes it as a
// symbol. None of the three is a value with something wrong with it, so none is
// an [AxisValueError].
type AxisFormError struct {
	// Pos is the form.
	Pos layout.Pos

	// Axis is the axis the form states.
	Axis Axis

	// Found names what was written instead, so the message says what it found
	// and not only what it wanted.
	Found string
}

// Error implements the error interface.
func (e *AxisFormError) Error() string {
	return fmt.Sprintf("%s: form %s takes one symbol naming its value, and this one has %s", e.Pos, quote(e.Axis.String()), e.Found)
}

// RepeatedAxisError is one axis stated twice in one form.
//
// It carries both positions, because the fault is the pair: either line is a
// perfectly good statement of the axis on its own, and what an adopter has to
// decide is which of the two they meant.
type RepeatedAxisError struct {
	// Pos is the second statement.
	Pos layout.Pos

	// First is the one before it.
	First layout.Pos

	// Axis is the axis stated twice.
	Axis Axis

	// Form is the tag of the form carrying both.
	Form string
}

// Error implements the error interface.
func (e *RepeatedAxisError) Error() string {
	return fmt.Sprintf("%s: form %s states %s twice, and states it first at %s", e.Pos, quote(e.Form), e.Axis, e.First)
}

// ChildError is something written among a form's children that is not one of the
// axes it admits.
//
// It covers both shapes the fault takes — a form whose tag names no axis, and an
// element that is not a form at all — because what is wrong with each is the
// same thing: the position admits one of four children and holds something else.
type ChildError struct {
	// Pos is the child, or the element standing where one belongs.
	Pos layout.Pos

	// Form is the tag of the form the child was written in.
	Form string

	// Found names what was written.
	Found string

	// Admits is what the position takes, named in the message so that a
	// misspelled axis is one search away from the spelling that works.
	Admits []string
}

// Error implements the error interface.
func (e *ChildError) Error() string {
	return fmt.Sprintf("%s: form %s admits %s, and this is %s", e.Pos, quote(e.Form), and(e.Admits), e.Found)
}

// EmptyOverrideError is an `encoding-override` that names no axis.
//
// docs/layout/SPEC.md makes it a diagnostic by name, and "What the schema does
// not say" files it under the rules a declaration cannot state: the schema
// declares each axis `at-most-one` on this form, and "at least one of the four"
// is a statement about the four together.
//
// An override states an axis when it carries one of the four forms, whatever
// that form turned out to say. One whose only axis was rejected on its own
// account is an [AxisValueError] or an [AxisFormError] alone, because the layout
// plainly names an axis and this message would deny it.
type EmptyOverrideError struct {
	// Pos is the `encoding-override` form.
	Pos layout.Pos

	// Item is the item it named.
	Item ItemRef
}

// Error implements the error interface.
func (e *EmptyOverrideError) Error() string {
	return fmt.Sprintf(
		"%s: the override on %s states no axis; an override states at least one of %s",
		e.Pos, e.Item, and(axisNames()),
	)
}

// DuplicateOverrideError is a second `encoding-override` naming an item another
// one already named.
//
// docs/layout/SPEC.md gives the reason and this message repeats it: two
// overrides on one item "would leave the order they were written in deciding the
// answer", and the order forms are written in is not something this format lets
// anything depend on.
type DuplicateOverrideError struct {
	// Pos is the second override.
	Pos layout.Pos

	// First is the one before it.
	First layout.Pos

	// Item is the item both name.
	Item ItemRef
}

// Error implements the error interface.
func (e *DuplicateOverrideError) Error() string {
	return fmt.Sprintf(
		"%s: %s is overridden twice, and is overridden first at %s; two overrides on one item would leave the order they were written in deciding the answer",
		e.Pos, e.Item, e.First,
	)
}

// ItemReferenceError is a reference that is not `(item <record-name> <name> …)`.
//
// It is about the spelling alone. Whether the path names an item, and whether
// the record it starts at is one the layout defines, are the published schema's
// and `resolve`'s respectively — neither is answerable from the reference.
type ItemReferenceError struct {
	// Pos is the reference, or the part of it that is wrong.
	Pos layout.Pos

	// Found names what was written.
	Found string
}

// Error implements the error interface.
func (e *ItemReferenceError) Error() string {
	return fmt.Sprintf("%s: an item reference is written (item <record-name> <name> ...), and this is %s", e.Pos, e.Found)
}

// FramingCountError is a layout that does not carry exactly one `framing` form.
//
// docs/layout/SPEC.md requires exactly one. A layout carrying none says nothing
// about how the file is framed, and one carrying two leaves the order they were
// written in deciding which dataset the copybooks are bound to.
type FramingCountError struct {
	// Pos is the second `framing` form where there is one, and the start of the
	// file where there is none.
	Pos layout.Pos

	// Count is how many the layout carries.
	Count int
}

// Error implements the error interface.
func (e *FramingCountError) Error() string {
	return fmt.Sprintf("%s: a layout carries exactly one framing form, and this one carries %d", e.Pos, e.Count)
}

// MissingRECFMError is a `framing` form that states no `recfm`.
//
// It is the one child no record format conditions, because it *is* the record
// format: every other rule in the layer is keyed on it, and there is no default
// for the same reason there is no default for an encoding axis.
type MissingRECFMError struct {
	// Pos is the `framing` form, which is where the record format has to be
	// written.
	Pos layout.Pos
}

// Error implements the error interface.
func (e *MissingRECFMError) Error() string {
	return fmt.Sprintf(
		"%s: a framing states one recfm, and this one states none; the rest of the framing follows from it",
		e.Pos,
	)
}

// RequiredChildError is a `framing` child the record format requires and the
// layout does not state.
//
// The three are docs/layout/SPEC.md's: `lrecl` under `F` and `FB`, because it is
// the only thing standing between an adopter and a silent misalignment;
// `max-segment` under `VBS`, because nothing a layout says implies it and a
// writer cannot compute it; and `delimiter` and `placement` under
// `line-sequential`, neither of which has a default.
type RequiredChildError struct {
	// Pos is the `framing` form, which is where the child has to be written.
	Pos layout.Pos

	// Child is the tag nobody wrote.
	Child string

	// RECFM is the record format requiring it, which is the other half of the
	// fault: the same child is optional under another spelling.
	RECFM RECFM
}

// Error implements the error interface.
func (e *RequiredChildError) Error() string {
	return fmt.Sprintf("%s: recfm %s requires %s, and this framing states none", e.Pos, e.RECFM, quote(e.Child))
}

// UnadmittedChildError is a `framing` child the record format does not admit.
//
// It names the record format, which docs/layout/SPEC.md requires of the case it
// argues in full — an `lrecl` under `line-sequential`, "where the dataset has no
// such number", is "a diagnostic naming the spelling". A child that says nothing
// about the file the layout describes is a layout describing two files at once,
// and which of them was meant is not something to guess at.
type UnadmittedChildError struct {
	// Pos is the child that should not be there.
	Pos layout.Pos

	// Child is its tag.
	Child string

	// RECFM is the record format that does not admit it.
	RECFM RECFM
}

// Error implements the error interface.
func (e *UnadmittedChildError) Error() string {
	return fmt.Sprintf("%s: recfm %s admits no %s, and this framing states one", e.Pos, e.RECFM, quote(e.Child))
}

// RepeatedChildError is one `framing` child stated twice.
//
// Like [RepeatedAxisError] it carries both positions, because the fault is the
// pair: either line is a perfectly good statement on its own, and what an
// adopter has to decide is which of the two they meant.
type RepeatedChildError struct {
	// Pos is the second statement.
	Pos layout.Pos

	// First is the one before it.
	First layout.Pos

	// Child is the tag stated twice.
	Child string

	// Form is the tag of the form carrying both.
	Form string
}

// Error implements the error interface.
func (e *RepeatedChildError) Error() string {
	return fmt.Sprintf("%s: form %s states %s twice, and states it first at %s", e.Pos, quote(e.Form), quote(e.Child), e.First)
}

// FramingValueError is a value written for a `framing` child that the child does
// not admit.
//
// It is [AxisValueError]'s counterpart in this layer and names the whole set for
// the same reason: every one of these is a spelling question an adopter can
// answer from the message.
type FramingValueError struct {
	// Pos is the value, not the form carrying it.
	Pos layout.Pos

	// Child is the tag it was written under.
	Child string

	// Value is what was written.
	Value string

	// Admits is the set the child takes.
	Admits []string
}

// Error implements the error interface.
func (e *FramingValueError) Error() string {
	return fmt.Sprintf("%s: %s is one of %s, and this one says %s", e.Pos, e.Child, and(e.Admits), quote(e.Value))
}

// FramingFormError is a `framing` child that does not carry exactly one value of
// the sort it takes.
//
// `(lrecl)` states nothing, `(lrecl 80 80)` states two things, and
// `(lrecl "80")` writes a byte count as text. None of the three is a value with
// something wrong with it, so none is a [FramingValueError].
type FramingFormError struct {
	// Pos is the form, or the element standing where its value belongs.
	Pos layout.Pos

	// Child is the tag.
	Child string

	// Takes names what the position takes.
	Takes string

	// Found names what was written instead.
	Found string
}

// Error implements the error interface.
func (e *FramingFormError) Error() string {
	return fmt.Sprintf("%s: form %s takes %s, and this one has %s", e.Pos, quote(e.Child), e.Takes, e.Found)
}

// SizeError is a byte count that is not positive.
//
// A framing's numbers are lengths of things that exist, so zero and negative are
// not small sizes: they are a number nothing in the dataset could have.
type SizeError struct {
	// Pos is the number.
	Pos layout.Pos

	// Child is the tag it was written under.
	Child string

	// Value is what was written.
	Value int64
}

// Error implements the error interface.
func (e *SizeError) Error() string {
	return fmt.Sprintf("%s: %s is a positive number of bytes, and this one says %d", e.Pos, e.Child, e.Value)
}

// UndefinedLengthError is RECFM U, which docs/ir/SPEC.md's "Undefined-length
// records" excludes.
//
// It names the dataset rather than reporting a generic framing error, which both
// specs require of it: an adopter who has one needs to know that the problem is
// the record format they were given and not the layout they wrote.
type UndefinedLengthError struct {
	// Pos is the `recfm` value.
	Pos layout.Pos
}

// Error implements the error interface.
func (e *UndefinedLengthError) Error() string {
	return fmt.Sprintf(
		"%s: recfm U is a dataset whose record extents came from the physical blocks the access method read, "+
			"and a byte stream on a filesystem has lost them; where every block was in fact the same size the file "+
			"is a fixed-length one and the layout says F or FB, and where they were not the boundaries have to be "+
			"put back by whatever writes the stream",
		e.Pos,
	)
}

// CarriageControlError is a record format carrying an ASA or machine carriage
// control — `FBA`, `VBM`.
//
// docs/layout/SPEC.md requires the diagnostic to name the carriage control and
// say where it belongs instead. The control character is a byte of the record:
// it is positioned like every other byte, it counts toward LRECL, and something
// may need to read it. A framing setting would make it a framing byte, and no
// item covers one, no slack node accounts for one and no predicate ever sees
// one.
type CarriageControlError struct {
	// Pos is the `recfm` value.
	Pos layout.Pos

	// Value is what was written, in full.
	Value string

	// Control names which carriage control it carries.
	Control string

	// RECFM is the record format underneath it, which is what the layout says
	// once the control character is declared where it belongs.
	RECFM RECFM
}

// Error implements the error interface.
func (e *CarriageControlError) Error() string {
	return fmt.Sprintf(
		"%s: recfm %s carries %s carriage control, and a control character is a byte of the record rather than "+
			"a byte of the framing; declare it as a leading item in the copybook and write recfm %s",
		e.Pos, quote(e.Value), e.Control, e.RECFM,
	)
}

// BlockedStreamError is `(blocks in-stream)`, which docs/ir/SPEC.md's "Block
// descriptor words in the stream" excludes.
//
// A blocked *dataset* is ordinary — `FB` resolves to **unframed** and `VB` to
// **descriptor-word** — because what blocking is on the mainframe is not what
// arrives. What is refused is a stream that still carries the block descriptor
// words, which is a dataset image rather than a record stream.
type BlockedStreamError struct {
	// Pos is the `blocks` value.
	Pos layout.Pos
}

// Error implements the error interface.
func (e *BlockedStreamError) Error() string {
	return fmt.Sprintf(
		"%s: blocks in-stream is a dataset image rather than a record stream, and the transfer has to deblock it; "+
			"a blocked dataset is otherwise ordinary and needs nothing said about it",
		e.Pos,
	)
}

// BlockSizeNotAMultipleError is a `blksize` under `FB` that is not a whole
// number of records.
//
// A fixed-length block holds whole records, so a block size that is not a
// multiple of the record length describes a dataset nothing allocated: one of
// the two numbers came from somewhere else.
type BlockSizeNotAMultipleError struct {
	// Pos is the `blksize` value.
	Pos layout.Pos

	// RECFM is the record format the rule is keyed on.
	RECFM RECFM

	// BlockSize and LRECL are the two numbers, so that the message names both.
	BlockSize int64
	LRECL     int64
}

// Error implements the error interface.
func (e *BlockSizeNotAMultipleError) Error() string {
	return fmt.Sprintf(
		"%s: under recfm %s a blksize holds whole records and is a multiple of lrecl, and %d is not a multiple of %d",
		e.Pos, e.RECFM, e.BlockSize, e.LRECL,
	)
}

// BlockSizeTooSmallError is a `blksize` under `VB` or `VBS` that cannot hold one
// record.
//
// A block carries a block descriptor word in front of the records in it, so a
// block size below the record length plus that word describes a dataset in which
// the longest record does not fit in a block.
type BlockSizeTooSmallError struct {
	// Pos is the `blksize` value.
	Pos layout.Pos

	// RECFM is the record format the rule is keyed on.
	RECFM RECFM

	// BlockSize and LRECL are the two numbers.
	BlockSize int64
	LRECL     int64
}

// Error implements the error interface.
func (e *BlockSizeTooSmallError) Error() string {
	return fmt.Sprintf(
		"%s: under recfm %s a blksize is at least lrecl plus the %d bytes of a block descriptor word, "+
			"and %d is less than %d",
		e.Pos, e.RECFM, blockDescriptorWord, e.BlockSize, e.LRECL+blockDescriptorWord,
	)
}

// ByteStringError is a literal that is not `(bytes "<hex>")`.
//
// It covers every shape the fault takes, including the two schema/layout.sexpr
// names as the reader's: text that is not hexadecimal digits in pairs, and a
// byte string of no bytes. A delimiter of no bytes would be a delimiter nothing
// in the file could be found at.
type ByteStringError struct {
	// Pos is the literal, or the part of it that is wrong.
	Pos layout.Pos

	// Found names what was written.
	Found string
}

// Error implements the error interface.
func (e *ByteStringError) Error() string {
	return fmt.Sprintf(
		"%s: a byte string is written (bytes \"<hex>\"), hexadecimal digits in pairs, and this is %s",
		e.Pos, e.Found,
	)
}

// What a strategy was written for, which is the half of a diagnostic about one
// that says which set the position takes.
const (
	subjectRecord = "a record"
	subjectArm    = "an arm"
)

// DiscriminateFormError is a `discriminate` that is not
// `(discriminate <record-name> <strategy>)`.
//
// It is about the shape alone. Whether the record is one the layout defines is
// an [UnknownRecordError], and whether the strategy is one of the three is a
// [StrategyError]: both need the form to have been read first.
type DiscriminateFormError struct {
	// Pos is the form, or the part of it that is wrong.
	Pos layout.Pos

	// Found names what was written.
	Found string
}

// Error implements the error interface.
func (e *DiscriminateFormError) Error() string {
	return fmt.Sprintf(
		"%s: a discriminator is written (discriminate <record-name> <strategy>), and this has %s",
		e.Pos, e.Found,
	)
}

// UnknownRecordError is a form naming a record no `record` form defines.
//
// docs/layout/SPEC.md files it under the rules relating one form to another: the
// published schema states it as a reference, and this package states it again
// because it assumes nothing about the schema. A discriminator on a record
// nobody defined tells nothing apart — it is the unreachable half of the layer's
// completeness rule, the other half being a record nobody discriminated.
type UnknownRecordError struct {
	// Pos is the name, not the form carrying it.
	Pos layout.Pos

	// Record is what was named.
	Record string

	// Form is the tag it was named under, so that the message says which
	// statement is about a record that is not there.
	Form string
}

// Error implements the error interface.
func (e *UnknownRecordError) Error() string {
	return fmt.Sprintf(
		"%s: form %s names record %s, and the layout defines no record of that name",
		e.Pos, quote(e.Form), quote(e.Record),
	)
}

// DuplicateDiscriminatorError is a second `discriminate` naming a record another
// one already named.
//
// It carries both positions for [DuplicateOverrideError]'s reason: either form
// is a perfectly good discriminator on its own, and what an adopter has to
// decide is which of the two they meant. Nothing orders a layout's forms, so
// there is no first one to prefer.
type DuplicateDiscriminatorError struct {
	// Pos is the second discriminator.
	Pos layout.Pos

	// First is the one before it.
	First layout.Pos

	// Record is the record both name.
	Record string
}

// Error implements the error interface.
func (e *DuplicateDiscriminatorError) Error() string {
	return fmt.Sprintf(
		"%s: record %s is discriminated twice, and is discriminated first at %s; "+
			"a record carries exactly one discriminator",
		e.Pos, quote(e.Record), e.First,
	)
}

// MissingDiscriminatorError is a `record` no `discriminate` names.
//
// docs/layout/SPEC.md requires one of every record, and the reason is what the
// requirement buys: it makes "this record carries nothing to test" a statement
// an adopter made — `single-record-type`, written out — rather than a gap in the
// file that reads the same way.
type MissingDiscriminatorError struct {
	// Pos is the `record` form, which is the record an adopter has to write a
	// discriminator for. A form a layout is missing has no position of its own.
	Pos layout.Pos

	// Record is its name.
	Record string
}

// Error implements the error interface.
func (e *MissingDiscriminatorError) Error() string {
	return fmt.Sprintf(
		"%s: record %s carries no discriminator; a record that carries nothing to test says so, "+
			"with %s",
		e.Pos, quote(e.Record), quote(string(SingleRecordType)),
	)
}

// ForeignTargetError is a record's discriminator testing an item of another
// record.
//
// It is the half of docs/layout/SPEC.md's "The item MUST be contained in the
// record the strategy is written on" that needs no copybook: an item reference
// is rooted at a record name, and a reference rooted at another record names
// bytes this discriminator will never be evaluated against, whatever the
// copybooks turn out to hold.
type ForeignTargetError struct {
	// Pos is the item reference.
	Pos layout.Pos

	// Item is the reference.
	Item ItemRef

	// Record is the record being discriminated.
	Record string
}

// Error implements the error interface.
func (e *ForeignTargetError) Error() string {
	return fmt.Sprintf(
		"%s: the discriminator on record %s tests %s, and a discriminator tests an item of the record it discriminates",
		e.Pos, quote(e.Record), e.Item,
	)
}

// StrategyError is something written where a strategy belongs that is not one of
// the set that position admits.
//
// The two sets differ by one member and the message names the one that applies:
// a record admits `single-record-type` and an arm does not, because an arm
// selected by nothing at all is the default arm docs/ir/SPEC.md refuses.
type StrategyError struct {
	// Pos is what was written, or the tag of the form that was.
	Pos layout.Pos

	// Subject is what the strategy would have selected.
	Subject string

	// Found names what was written.
	Found string

	// Admits is the set the position takes.
	Admits []string
}

// Error implements the error interface.
func (e *StrategyError) Error() string {
	return fmt.Sprintf("%s: %s is selected by %s, and this is %s", e.Pos, e.Subject, and(e.Admits), e.Found)
}

// StrategyFormError is a strategy form that does not carry an item and the
// literals it takes.
//
// `(equals)` states nothing, `(equals (item R F))` names an item and no value to
// compare it against, and `(equals (item R F) "1" "2")` is two literals in the
// position that takes one — which is `one-of`, spelled as `equals`.
type StrategyFormError struct {
	// Pos is the form, or the element standing where nothing belongs.
	Pos layout.Pos

	// Kind is the strategy it states.
	Kind StrategyKind

	// Found names what was written.
	Found string
}

// Error implements the error interface.
func (e *StrategyFormError) Error() string {
	takes := "one item reference and one literal"
	if e.Kind == OneOf {
		takes = "one item reference and at least one literal"
	}

	return fmt.Sprintf("%s: form %s takes %s, and this one has %s", e.Pos, quote(string(e.Kind)), takes, e.Found)
}

// LiteralError is something written where a literal belongs that is none of the
// three spellings.
//
// A byte string with something wrong inside it is a [ByteStringError] instead:
// the position plainly holds a literal, and the fault is what it says.
type LiteralError struct {
	// Pos is what was written.
	Pos layout.Pos

	// Found names it.
	Found string
}

// Error implements the error interface.
func (e *LiteralError) Error() string {
	return fmt.Sprintf(
		"%s: a literal is text, a number or (bytes \"<hex>\"), and this is %s",
		e.Pos, e.Found,
	)
}

// VariantFormError is a `discriminate-variant` that names no item at all.
//
// A reference that is written and wrong is an [ItemReferenceError]; this is the
// form carrying nothing where the variant belongs.
type VariantFormError struct {
	// Pos is the form.
	Pos layout.Pos

	// Found names what was written.
	Found string
}

// Error implements the error interface.
func (e *VariantFormError) Error() string {
	return fmt.Sprintf(
		"%s: a variant discriminator is written (discriminate-variant <item-ref> (arm <name> <predicate>) ...), "+
			"and this is %s",
		e.Pos, e.Found,
	)
}

// VariantDepthError is a variant reference that cannot name one.
//
// A variant sits inside a group that repeats. A reference carrying a single name
// names an item directly under the record's top-level item, whose only ancestor
// is that item — and a record does not repeat, so no copybook can make such a
// reference name a variant. A redefine at that depth is told apart by an
// ordinary `discriminate`, because its alternatives are whole record types.
type VariantDepthError struct {
	// Pos is the reference.
	Pos layout.Pos

	// Variant is what was written.
	Variant ItemRef
}

// Error implements the error interface.
func (e *VariantDepthError) Error() string {
	return fmt.Sprintf(
		"%s: %s names an item directly under record %s's top-level item, and a variant sits inside a group "+
			"that repeats; a redefine whose alternatives are whole record types is told apart by discriminate",
		e.Pos, e.Variant, quote(e.Variant.Record),
	)
}

// DuplicateVariantError is a second `discriminate-variant` naming an item
// another one already named.
//
// It is [DuplicateDiscriminatorError] at the other scope and for the same
// reason: two statements of which arm an occurrence takes would leave the order
// they were written in deciding the answer.
type DuplicateVariantError struct {
	// Pos is the second variant discriminator.
	Pos layout.Pos

	// First is the one before it.
	First layout.Pos

	// Variant is the item both name.
	Variant ItemRef
}

// Error implements the error interface.
func (e *DuplicateVariantError) Error() string {
	return fmt.Sprintf(
		"%s: %s is discriminated twice, and is discriminated first at %s; a variant carries exactly one discriminator",
		e.Pos, e.Variant, e.First,
	)
}

// VariantArmCountError is a variant discriminator carrying fewer than two arms.
//
// A variant is an alternation, and an alternation with one arm is the redefine
// every occurrence of which takes one alternative — which docs/ir/SPEC.md
// resolves to that alternative's items with no variant at all.
type VariantArmCountError struct {
	// Pos is the `discriminate-variant` form.
	Pos layout.Pos

	// Variant is the item it names.
	Variant ItemRef

	// Count is how many arms it carries. It counts the arms that were read, so
	// a variant whose second arm is malformed is reported against that arm and
	// against this rule, and not twice against this one.
	Count int
}

// Error implements the error interface.
func (e *VariantArmCountError) Error() string {
	return fmt.Sprintf(
		"%s: the variant at %s carries %d arms, and a variant carries at least two; "+
			"a redefine every occurrence of which takes one alternative is not a variant",
		e.Pos, e.Variant, e.Count,
	)
}

// ArmFormError is an arm that is not `(arm <name> <predicate>)`.
type ArmFormError struct {
	// Pos is the form, or the part of it that is wrong.
	Pos layout.Pos

	// Found names what was written.
	Found string
}

// Error implements the error interface.
func (e *ArmFormError) Error() string {
	return fmt.Sprintf("%s: an arm is written (arm <name> <predicate>), and this has %s", e.Pos, e.Found)
}

// DuplicateArmError is two arms of one variant naming one alternative.
//
// Two arms over one alternative are two statements of when to read the same
// bytes the same way, and which of them applies would be decided by the order
// they were written in.
type DuplicateArmError struct {
	// Pos is the second arm.
	Pos layout.Pos

	// First is the one before it.
	First layout.Pos

	// Variant is the variant both are arms of.
	Variant ItemRef

	// Alternative is the name both give.
	Alternative string
}

// Error implements the error interface.
func (e *DuplicateArmError) Error() string {
	return fmt.Sprintf(
		"%s: the variant at %s names alternative %s twice, and names it first at %s",
		e.Pos, e.Variant, quote(e.Alternative), e.First,
	)
}

// ArmTargetError is an arm's target standing where an arm's target may not.
//
// docs/layout/SPEC.md's rule is that the target sits inside the innermost
// repeating group containing the variant, and not inside an arm that does not
// also contain it. The halves needing a copybook are `resolve`'s; the ones this
// reports are the ones an adopter gets wrong while holding the copybook open — a
// target in another record, a target in a header elsewhere in this one, and a
// target inside the alternatives themselves.
type ArmTargetError struct {
	// Pos is the item reference.
	Pos layout.Pos

	// Alternative is the arm it selects.
	Alternative string

	// Item is the target.
	Item ItemRef

	// Variant is the variant the arm belongs to.
	Variant ItemRef

	// Found says where the target stands instead.
	Found string
}

// Error implements the error interface.
func (e *ArmTargetError) Error() string {
	return fmt.Sprintf(
		"%s: the arm on %s tests %s, which is %s; an arm's target sits inside the occurrence it is chosen for",
		e.Pos, quote(e.Alternative), e.Item, e.Found,
	)
}

// ArmOverlapError is two arms of one variant testing one item for one literal.
//
// It is the smallest overlap and the only one decidable from the layout alone.
// Two arms that can both match one occurrence are refused because there is
// nothing to break the tie: an arm carries no guard, the order arms are written
// in decides nothing, and there is no default arm to fall through to.
type ArmOverlapError struct {
	// Pos is the literal on the second arm.
	Pos layout.Pos

	// First is the same literal on the first.
	First layout.Pos

	// Variant is the variant both are arms of.
	Variant ItemRef

	// Arms are the two alternatives, in the order the layout writes them.
	Arms [2]string

	// Item is the target both test.
	Item ItemRef

	// Literal is the value both admit.
	Literal Literal
}

// Error implements the error interface.
func (e *ArmOverlapError) Error() string {
	return fmt.Sprintf(
		"%s: %s on %s is admitted by arms %s and %s of the variant at %s, and is admitted first at %s; "+
			"two arms that can both match one occurrence have nothing to tell them apart",
		e.Pos, e.Literal, e.Item, quote(e.Arms[0]), quote(e.Arms[1]), e.Variant, e.First,
	)
}

// SequenceCountError is a layout that does not carry exactly one `sequence`
// form.
//
// docs/layout/SPEC.md requires exactly one, and it is the whole of what the
// layout says about the order records may appear in. A layout carrying none says
// nothing about that order, and one carrying two leaves the order they were
// written in deciding which orders the file admits.
type SequenceCountError struct {
	// Pos is the second `sequence` form where there is one, and the start of the
	// file where there is none.
	Pos layout.Pos

	// Count is how many the layout carries.
	Count int
}

// Error implements the error interface.
func (e *SequenceCountError) Error() string {
	return fmt.Sprintf("%s: a layout carries exactly one sequence form, and this one carries %d", e.Pos, e.Count)
}

// SequenceFormError is a `sequence` that does not carry exactly one expression.
//
// `(sequence)` states no order at all, and `(sequence HEADER TRAILER)` is two
// expressions where one belongs — which is `seq`, spelled by leaving it out.
type SequenceFormError struct {
	// Pos is the form.
	Pos layout.Pos

	// Found names what was written.
	Found string
}

// Error implements the error interface.
func (e *SequenceFormError) Error() string {
	return fmt.Sprintf(
		"%s: a sequence is written (sequence <expression>), one expression, and this one has %s",
		e.Pos, e.Found,
	)
}

// ExpressionError is something written where a sequencing expression belongs
// that is neither a record name nor one of the seven operators.
//
// The message names the operators and not the record name, because a position
// naming a record is the one shape nobody gets wrong by reaching for a form that
// does not exist: every one of these is a layout that wrote `(one-of …)`, `(*)`
// spelled `(star …)`, or a literal where a term belongs.
type ExpressionError struct {
	// Pos is what was written, or the tag of the form that was.
	Pos layout.Pos

	// Found names it.
	Found string

	// Admits is the set of operators, named in the message so that a misspelled
	// one is one search away from the spelling that works.
	Admits []string
}

// Error implements the error interface.
func (e *ExpressionError) Error() string {
	return fmt.Sprintf(
		"%s: a sequencing expression is a record name or one of %s, and this is %s",
		e.Pos, and(e.Admits), e.Found,
	)
}

// ExpressionFormError is an operator that does not carry what it takes.
//
// `(seq HEADER)` is a concatenation of one, which is the subexpression itself;
// `(* A B)` is two subexpressions under an operator that repeats one, which is
// `(* (seq A B))` or `(seq (* A) B)` and not something to guess between; and a
// `times` or a `when` missing its reference is an operator that reads a value
// out of nothing.
type ExpressionFormError struct {
	// Pos is the form.
	Pos layout.Pos

	// Kind is the operator it states.
	Kind ExpressionKind

	// Found names what was written.
	Found string
}

// Error implements the error interface.
func (e *ExpressionFormError) Error() string {
	return fmt.Sprintf("%s: form %s takes %s, and this one has %s", e.Pos, quote(string(e.Kind)), expressionTakes(e.Kind), e.Found)
}

// expressionTakes names what an operator carries, for the message above.
func expressionTakes(kind ExpressionKind) string {
	switch kind {
	case Seq, Alt:
		return "two or more subexpressions"
	case Times:
		return "one subexpression and the item counting it"
	case When:
		return "an item reference, a value and one subexpression"
	default:
		return "one subexpression"
	}
}

// MatchFormError is a `(one-of …)` in a `when` that names no literal.
//
// A `when` selecting on nothing at all admits its subexpression under no value
// and refuses it under every one, which is the subexpression left out.
type MatchFormError struct {
	// Pos is the form.
	Pos layout.Pos

	// Found names what was written.
	Found string
}

// Error implements the error interface.
func (e *MatchFormError) Error() string {
	return fmt.Sprintf(
		"%s: a when tests a value against a literal or (one-of <literal> ...), and this has %s",
		e.Pos, e.Found,
	)
}

// UnsequencedRecordError is a `record` the sequencing expression never names.
//
// docs/layout/SPEC.md requires every record to appear in the expression, and
// gives the reason this message repeats: a record type nothing can ever admit is
// one a file reader will never produce, and saying so is cheaper than leaving an
// adopter to find that out. It is [MissingDiscriminatorError]'s counterpart in
// this layer — the completeness rule counting one form against another, from the
// other end.
type UnsequencedRecordError struct {
	// Pos is the `record` form, which is the record an adopter has to sequence.
	// A record missing from an expression has no position inside it.
	Pos layout.Pos

	// Record is its name.
	Record string
}

// Error implements the error interface.
func (e *UnsequencedRecordError) Error() string {
	return fmt.Sprintf(
		"%s: record %s appears nowhere in the sequencing expression, and a record type nothing can ever "+
			"admit is one nothing will ever produce",
		e.Pos, quote(e.Record),
	)
}

// axisNames is the four axes as a layout spells them, for a message that has to
// list them.
func axisNames() []string {
	names := make([]string, 0, len(allAxes))

	for _, axis := range allAxes {
		names = append(names, axis.String())
	}

	return names
}

// describe names a layout node the way a message refers to it.
func describe(node layout.Node) string {
	switch node := node.(type) {
	case layout.Form:
		return "form " + quote(node.Tag)
	case layout.Symbol:
		return "the symbol " + quote(node.Value)
	case layout.Text:
		return "text"
	case layout.Int, layout.Float:
		return "a number"
	default:
		return "something else"
	}
}

// quote renders a name the way every message here quotes one.
func quote(value string) string { return strconv.Quote(value) }

// and joins a list the way a sentence does, so that a message naming what is
// admitted reads as one.
func and(items []string) string {
	switch len(items) {
	case 0:
		return "nothing"
	case 1:
		return items[0]
	case 2:
		return items[0] + " or " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " or " + items[len(items)-1]
	}
}
