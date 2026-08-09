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
