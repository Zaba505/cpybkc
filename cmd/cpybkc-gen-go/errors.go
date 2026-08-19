// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"

	"github.com/Zaba505/cpybkc/irpb"
)

// malformedError is a descriptor that does not say what docs/ir/SPEC.md says a
// descriptor says.
//
// Every one of these is a bug upstream of this generator rather than something
// an adopter can fix in their copybook, and each carries a note naming the rule
// it broke — because the user reading it is holding a cpybkc that produced it
// and has no other way to tell a bug in the producer from one in their layout.
type malformedError struct {
	// What is the failure, as the `error:` line.
	What string

	// Rule is the requirement the descriptor broke, as the `note:` line.
	Rule string

	// Reference is the node identifier a reference did not resolve to, and zero
	// for every other way a descriptor is malformed.
	//
	// Carried so that this one shape is *distinguishable*, because it is the one
	// this generator never writes around: a reference to a node the message does
	// not contain is a descriptor that does not exist rather than a layout this
	// generator cannot synthesize bytes for, and [advisory] refuses it on that
	// basis wherever it is met. Everything else here is a sentence, and a
	// sentence is not something to branch on.
	Reference uint64
}

// malformed is a [malformedError], spelled at its use sites the way a sentence
// is rather than as a struct literal.
func malformed(what, rule string) error {
	return &malformedError{What: what, Rule: rule}
}

// unresolved is the commonest of them: a reference to an identifier no node
// carries.
func unresolved(id uint64) error {
	return &malformedError{
		What:      fmt.Sprintf("the descriptor references node %d, and carries no node with that identifier", id),
		Rule:      "every reference must resolve to a node in the same message, of a kind the referring position admits; see docs/ir/SPEC.md, \"Identity, ordering and determinism\"",
		Reference: id,
	}
}

// Error implements the error interface.
func (e *malformedError) Error() string {
	return "the descriptor is malformed: " + e.What
}

// Notes is what follows it as a `note:` diagnostic.
func (e *malformedError) Notes() []string { return []string{e.Rule} }

// colliding is one of the two items a collision is between: the copybook's name
// for it, and the rename the layout gave it where it gave one.
//
// The rename is carried because it is what was munged, and a collision naming
// only the copybook's two names would send an adopter to look at a copybook
// whose names do not collide — the rename in their layout is the half they can
// edit, and it is the half that has to be on the page.
type colliding struct {
	// Original is the name as the copybook spells it.
	Original string

	// Override is the rename, empty where the layout renamed nothing.
	Override string
}

// String is the item as a diagnostic names it.
func (c colliding) String() string {
	if c.Override == "" {
		return c.Original
	}

	return fmt.Sprintf("%s (renamed %s)", c.Original, c.Override)
}

// collisionError is two names that munge to one Go identifier.
//
// Reported rather than resolved. A generator that disambiguated silently would
// put a name in an adopter's source that their copybook does not contain and
// that a later copybook change would move, and the two items it stands for
// would be told apart by an ordering nobody wrote down.
type collisionError struct {
	// Go is the identifier both names produced.
	Go string

	// Cobol is the two items that produced it, in the order the descriptor
	// carries them.
	Cobol []colliding

	// Where is what the two names collide inside, as a phrase.
	Where string
}

// Error implements the error interface.
func (e *collisionError) Error() string {
	return fmt.Sprintf("%s and %s are both %s in Go, and they are both in %s",
		e.Cobol[0], e.Cobol[1], e.Go, e.Where)
}

// Notes is what follows it as a `note:` diagnostic.
func (e *collisionError) Notes() []string {
	return []string{
		"rename one of them in the layout, so that the name this generator munges is one you chose",
	}
}

// unmungeableError is a name this generator's munging does not turn into an
// exported Go identifier: one with no letter or digit in it at all, or one whose
// first is a digit.
type unmungeableError struct {
	// Kind is what sort of node carries the name — a record, a group, a field.
	Kind string

	// Cobol is the name as the copybook spells it.
	Cobol string

	// Override is the rename the layout gave it, empty where it gave none.
	//
	// It is what was munged where it is set, so it is what the diagnostic is
	// about: an adopter told to rename an item they have already renamed has
	// been sent somewhere they have been.
	Override string
}

// Error implements the error interface.
func (e *unmungeableError) Error() string {
	if e.Override != "" {
		return fmt.Sprintf("%s is the rename of the %s named %s, and there is no exported Go identifier in it",
			e.Override, e.Kind, e.Cobol)
	}

	return fmt.Sprintf("%s is the name of a %s, and there is no exported Go identifier in it", e.Cobol, e.Kind)
}

// Notes is what follows it as a `note:` diagnostic.
func (e *unmungeableError) Notes() []string {
	if e.Override != "" {
		return []string{
			"a Go identifier begins with a letter; the rename in your layout is the name this generator munges, so it is the one to change",
		}
	}

	return []string{
		"a Go identifier begins with a letter; rename the item in the layout so that the name this generator munges does too",
	}
}

// fillerError is an item COBOL gave no data-name that this generator has
// nowhere to put: a FILLER group that repeats, or a FILLER item whose number of
// occurrences is data.
//
// It is deliberately **not** a [malformedError]. The descriptor is exactly what
// docs/ir/SPEC.md admits — an item with no data-name carries no names message,
// and "Names" opens on what a *named* node carries — so a diagnostic sending
// the adopter to look for a producer bug would send them after an item they
// wrote correctly. What is refused is this generator's ability to name the
// occurrences, and the answer is in the copybook: give the item a data-name.
//
// The two cases share that cause. A FILLER has no name, so nothing emitted for
// it can be named: an elementary one is retained as bytes in a field named
// after nothing in the copybook, and a group's members are reached at the level
// above it. Neither answer survives an occurrence count — a run of retained
// bytes has one length per record, and members that moved up a level cannot
// move up once per occurrence.
type fillerError struct {
	// Kind is what the item is, with the article it takes: "a group" or "an
	// item". The article is carried rather than composed, because "a" and
	// "an" is one more thing for a diagnostic to get wrong in front of a user
	// and there are exactly two of them.
	Kind string

	// In is the group containing it, as the copybook names it. A FILLER has no
	// name of its own, so this is the name a reader has to go looking with —
	// and it is the *innermost named* group every time, whichever part of this
	// generator met the item first, because a name that moved with the order
	// the files happen to be emitted in would be one an adopter could not act
	// on.
	In string

	// Because is what about it cannot be placed, as a phrase.
	Because string
}

// Error implements the error interface.
func (e *fillerError) Error() string {
	return fmt.Sprintf("%s of %s carries no data-name and %s", e.Kind, e.In, e.Because)
}

// Notes is what follows it as a `note:` diagnostic.
func (e *fillerError) Notes() []string {
	return []string{
		"give the item a data-name in the copybook, and it becomes a field like any other",
		"an item COBOL names nothing is generated rather than refused wherever it can be — a FILLER is retained as bytes and a FILLER group's members are reached at the level above it — and neither of those has a name for one occurrence to differ from another by",
	}
}

// unsupportedCharsetError is a charset the IR names and cobol-go's codec has no
// table for.
//
// Refused on sight rather than substituted, which is the posture the IR schema
// takes for the same reason: the charset decides what an alphanumeric field's
// bytes spell and which byte values a digit and a separate sign take, and every
// one of those fails silently when wrong. A generator that emitted CP037 for a
// descriptor naming CP500 would produce a package that reads most of a file
// correctly.
type unsupportedCharsetError struct {
	// Charset is the charset the descriptor names.
	Charset irpb.Charset
}

// Error implements the error interface.
func (e *unsupportedCharsetError) Error() string {
	return fmt.Sprintf("the descriptor's items are in %s, and cobol-go's codec ships no table for it", charsetName(e.Charset))
}

// Notes is what follows it as a `note:` diagnostic.
func (e *unsupportedCharsetError) Notes() []string {
	return []string{
		"codec ships ASCII and cp037; a Charset is an interface, so cp500, cp1047 and cp1140 are an implementation away, and this generator will emit against one as soon as codec names it",
		"generating cp037 for another EBCDIC code page would read most of a file correctly and the bracket, currency and accent characters wrongly",
	}
}

// unsupportedBinarySizeError is a binary width staircase the descriptor names
// and this generator has no codec member for.
//
// Refused on sight rather than substituted, for [unsupportedCharsetError]'s
// reason carried one step further. A wrong charset spoils the field it governs
// and leaves every other field where it was; a wrong staircase leaves the COMP
// item's own bytes readable and moves everything behind it, so the package it
// would generate reads a file that is the right length and wrong from the first
// binary item onwards. There is no member near enough to another to be a
// sensible fallback: 2-4-8 and 1-2-4-8 differ by one byte on a two-digit item,
// which is precisely the difference nothing downstream can detect.
type unsupportedBinarySizeError struct {
	// Size is the staircase the descriptor names.
	Size irpb.BinarySize
}

// Error implements the error interface.
func (e *unsupportedBinarySizeError) Error() string {
	return fmt.Sprintf("the descriptor's binary items were compiled under %s, and this generator has no codec staircase for it", binarySizeName(e.Size))
}

// Notes is what follows it as a `note:` diagnostic.
func (e *unsupportedBinarySizeError) Notes() []string {
	return []string{
		"codec names 2-4-8, 1-2-4-8, 1--8 and full; a descriptor naming anything else was written against a newer IR than this generator reads",
		"generating one staircase for another would put every item behind the first COMP item at the wrong offset, which nothing in the file disagrees with",
	}
}

// mixedEncodingError is a descriptor whose items do not agree on the five
// encoding axes.
//
// codec carries an Encoding per Reader and per Writer rather than per field —
// the axes are properties of the file in hand — so a descriptor whose items
// disagree describes a file this generator has no single Encoding to read with.
// It is a shape resolve does not produce today, and refusing it is what keeps
// the generated Encoding a fact of the descriptor rather than of whichever item
// happened to be walked first.
type mixedEncodingError struct {
	// Axis is the axis two items disagree on.
	Axis string

	// First and Second are the items, as the copybook names them.
	First, Second string
}

// Error implements the error interface.
func (e *mixedEncodingError) Error() string {
	return fmt.Sprintf("%s and %s do not agree on the %s of the file they are in", e.First, e.Second, e.Axis)
}

// Notes is what follows it as a `note:` diagnostic.
func (e *mixedEncodingError) Notes() []string {
	return []string{
		"the five axes are properties of the file rather than of an item, and codec carries them on the Reader and the Writer; see docs/ir/SPEC.md, \"The encoding profile, applied\"",
	}
}
