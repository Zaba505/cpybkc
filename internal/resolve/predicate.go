// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"encoding/hex"
	"slices"
	"strconv"
	"strings"

	"github.com/Zaba505/cobol-go/codec"
	"github.com/Zaba505/cobol-go/copybook"
	"github.com/Zaba505/cobol-go/picture"

	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// This file is the discriminator, compiled: the closed set of two tests
// docs/ir/SPEC.md's "Discriminator predicates" admits, the resolution of a
// layout's literals into the bytes a consumer compares, and the walk a consumer
// runs over the result.
//
// # Two scopes, one node kind and one closed set
//
// Two things select on bytes and neither knows what the other is. A transition
// chooses a record, and an arm of a variant chooses an alternative inside one
// occurrence of a table. They share this node kind and these two tests, and they
// differ in the bytes they read and in the rules binding a target — which is
// docs/ir/SPEC.md's "A predicate on an arm reads one occurrence" (#90).
//
// A transition's predicate runs at step 3 of the read loop, before its record is
// admitted, so its target must sit at a position every record of that type has:
// outside every group that repeats, and ahead of every item whose extent moves
// with a count. An arm's runs inside a record already admitted, walking an
// occurrence that is already located, so both restrictions fall away and the
// opposite of the first one holds — an arm's target has to be *inside* the
// repeating group the variant sits in, because that is the occurrence it is
// evaluated in.
//
// # What a predicate never names
//
// A predicate names a field and a guard names a register, and neither reaches
// into the other's half (docs/ir/SPEC.md, "Discriminator predicates"). That is a
// property of the types here rather than a rule this file enforces: a
// [Predicate] carries a [github.com/Zaba505/cobol-go/copybook.Field] and has
// nowhere to put a register, and a [Guard] carries a [Register] and has nowhere
// to put a field. A layout has no spelling for a register either — the two
// operators that read one write an item reference and the register is what
// `resolve` allocates for it — so there is no layout to reject, and the split is
// kept by there being no way to write it down.
//
// The three strategies that name no field do not lower into a predicate at all,
// and each is refused where its reason is (#80). `single-record-type` is the
// absence of a predicate; a record's length is not a thing a predicate tests;
// positional *first* is the start state and positional *last* is refused
// outright, because a writer does not know which record is last.
//
// # No offset reaches a predicate
//
// A predicate names the field it tests and nothing else. Where those bytes are
// is the sum of the widths ahead of the field in the record, which is
// docs/ir/SPEC.md's "Ordering and width, and no offset" applying here as it
// applies everywhere: a producer that stated the position a second time could be
// wrong in a way no consumer can detect. The overlap check below needs a
// position and computes one from the record's own layout rather than reading one
// off the node, for exactly that reason.
//
// # The literals are bytes before they leave
//
// Both members carry their literals as bytes, padded by this package to the
// target's width, so that no consumer decides whether `Y` matches `Y ` and none
// applies a COBOL comparison rule of its own. The same resolution runs over a
// guard's literals for a bytes register, and for the same reason: a `when` over
// a `PIC X(4)` flag compares four bytes whatever the layout wrote.
//
// A literal is spelled three ways and each resolves differently
// (docs/layout/SPEC.md, "Literals"). `(bytes "F0F1")` is what is in the file and
// is taken as written; a text literal is translated through the item's charset
// and padded with that charset's space; a number is the digits of a zoned
// DISPLAY item, right-aligned and zero-filled. What is *not* resolved is a
// number against a packed, binary or signed item: those bytes follow from a SIGN
// clause and a byte order the four axes do not by themselves pin down, and this
// package refuses to guess bytes rather than emitting plausible ones. The
// diagnostic sends the adopter to `(bytes …)`, which is the spelling that exists
// for saying exactly what is in the file.

// PredicateTest is what a predicate tests, one member of the closed set of two
// docs/ir/SPEC.md's "Discriminator predicates" admits.
//
// There is no third and a third is a breaking change. The zero value is not a
// test: a [Predicate] this package hands back always names one.
type PredicateTest int

const (
	// BytesEqual is the target's bytes being the one carried literal.
	BytesEqual PredicateTest = iota + 1

	// BytesOneOf is the target's bytes being one of the carried literals.
	//
	// A member rather than a shorthand a producer expands into several
	// [BytesEqual] predicates: a transition carries at most one predicate and
	// an arm exactly one, so a strategy admitting three type codes has
	// nowhere to put three.
	BytesOneOf
)

// String implements the [fmt.Stringer] interface, rendering the test as
// docs/ir/SPEC.md's table names it.
func (t PredicateTest) String() string {
	switch t {
	case BytesEqual:
		return "bytes-equal"
	case BytesOneOf:
		return "bytes-one-of"
	}
	return "unknown"
}

// Predicate is a discriminator, compiled: the field whose bytes are tested and
// the test.
//
// It carries no position. Where the target's bytes are follows from the ordering
// and the widths of the record holding it, which is the one statement of
// position the IR makes (docs/ir/SPEC.md, "Ordering and width, and no offset").
//
// The target is a [github.com/Zaba505/cobol-go/copybook.Field] rather than an
// identifier for [Binding.Field]'s reason: this package resolves what a
// reference names and #38 assigns the identifiers every reference becomes.
type Predicate struct {
	// Target is the field the predicate tests. It is never nil: a member
	// naming nothing is not in the set, and selecting on nothing at all is the
	// absence of a predicate.
	Target *copybook.Field

	// Test is which of the two this is.
	Test PredicateTest

	// Values are what the target's bytes are compared against, in the order
	// the layout wrote them: exactly one under [BytesEqual] and at least one
	// under [BytesOneOf], each already the target's width in bytes.
	Values []Value
}

// Matches reports whether target's bytes satisfy the predicate.
//
// The bytes are the caller's to fetch, because the position they came from is
// the caller's to compute — a consumer walking a record already knows where it
// is standing, and handing this method the record would make it run the sum a
// second time (docs/ir/SPEC.md, "Dereferencing is not recomputation").
//
// A target that is not the width the predicate's values are is not compared
// against a prefix of one: it does not match. That is the read loop's rule for a
// target straddling the bound the framing places, arriving here as the one place
// the comparison happens.
func (p *Predicate) Matches(target []byte) bool {
	if p == nil {
		return true
	}

	for _, value := range p.Values {
		if string(value.Bytes) == string(target) {
			return true
		}
	}

	return false
}

// String renders the predicate the way a diagnostic names one.
func (p *Predicate) String() string {
	if p == nil {
		return "no predicate"
	}

	rendered := make([]string, 0, len(p.Values))
	for _, value := range p.Values {
		rendered = append(rendered, value.String())
	}

	return p.Test.String() + " " + itemName(p.Target) + " " + strings.Join(rendered, " ")
}

// Value is one literal a layout wrote, resolved to what a consumer compares.
//
// One type over two readings, as [Node] is one type over four kinds: a value
// compared against bytes carries [Value.Bytes], and one compared against a
// number — which only an integer register's guard ever is — carries
// [Value.Number] and no bytes. Which of the two applies follows from what is
// carrying the value and is never a question about the value itself.
//
// The literal the layout wrote is carried beside the resolution, because every
// diagnostic about a value quotes the adopter's own spelling of it and none of
// them quotes hex the adopter never typed.
type Value struct {
	// Literal is the layout's spelling, which is what a diagnostic quotes.
	Literal layoutmodel.Literal

	// Bytes are the bytes a consumer compares, already the width of the field
	// or register they are compared against. It is nil on a value an integer
	// register's guard carries.
	Bytes []byte

	// Number is what an integer register is compared against, and is
	// meaningless where Bytes is set.
	Number int64
}

// Identity is what makes two resolved values the same value.
//
// It is over the resolution and not over the spelling, which is the whole of
// what resolving buys the overlap checks: `"01"` written as text and
// `(bytes "F0F1")` written as bytes are one value on an EBCDIC file, and two
// transitions carrying them are two transitions one record satisfies both of.
// [layoutmodel.Literal.Identity] cannot see that and says so — it is the
// coarsest answer decidable from a layout alone, and this is the answer with a
// copybook in hand (#36, #37).
func (v Value) Identity() string {
	if v.Bytes == nil {
		return "number " + strconv.FormatInt(v.Number, 10)
	}

	return "bytes " + hex.EncodeToString(v.Bytes)
}

// sameValue reports what [Value.Identity] equality reports, without building
// either identity.
//
// The two answers agree by construction — a number and a run of bytes are never
// the same value, two numbers are the same value exactly when the numbers are,
// and two runs exactly when the bytes are. It exists because the identities are
// strings: [Guard.Holds] runs inside the consumer's selection loop, and
// rendering hex there allocates twice on every transition a consumer considers
// to answer a question about equality alone.
func sameValue(one, other Value) bool {
	if (one.Bytes == nil) != (other.Bytes == nil) {
		return false
	}

	if one.Bytes == nil {
		return one.Number == other.Number
	}

	return slices.Equal(one.Bytes, other.Bytes)
}

// String renders the value as the layout wrote it.
func (v Value) String() string { return v.Literal.String() }

// number is the value an integer register's guard compares against.
func number(literal layoutmodel.Literal) Value {
	parsed, err := strconv.ParseInt(literal.Number, 10, 64)
	if err != nil {
		// A count literal that will not parse is a fault for whoever wrote
		// it, and never a reason to call a state ambiguous: it is carried as
		// a value nothing equals rather than dropped.
		return Value{Literal: literal, Number: -1}
	}

	return Value{Literal: literal, Number: parsed}
}

// RegisterFile is what the automaton remembers between records: one value per
// register, keyed by [Register.ID].
//
// A register nobody has bound is absent, which is a state [compiler.prove] has
// already shown no guard can be evaluated in — every read is preceded on every
// path by a binding on an earlier transition. It is a map rather than a slice so
// that "absent" and "zero" are different answers, because a counter at zero is
// the ordinary end of a run and a counter nobody bound is a descriptor fault.
type RegisterFile map[int]Value

// Eligible reports whether every guard of the transition holds, and the first
// that does not where one does not.
//
// Guards are evaluated against the register file as it stands on entry to the
// state, before the record in front of the consumer is examined at all, so their
// order is not significant and a transition never reads what it binds.
func (t *Transition) Eligible(held RegisterFile) (bool, Guard) {
	for _, guard := range t.Guards {
		if !guard.Holds(held[guard.Register.ID]) {
			return false, guard
		}
	}

	return true, Guard{}
}

// Select is the transition of the state that admits the record in front of a
// consumer: the first eligible one carrying no predicate, or the first whose
// predicate matches.
//
// bytes hands back the target's bytes for a predicate about to be evaluated.
// The position they came from is the caller's, because a consumer walking a file
// already knows where it is standing and the sum that locates a field is
// arithmetic every consumer runs (docs/ir/SPEC.md, "Dereferencing is not
// recomputation").
//
// The walk is here for [Record.Position]'s reason. It is the walk a generated
// consumer runs, kept beside the descriptor it walks so that this package's own
// tests run it — and, more than that, so that the two failures docs/ir/SPEC.md
// distinguishes are distinguished in one place rather than in every generator.
// Where nothing matches, this is a record the layout does not describe; where a
// guard is what excluded the transition that would have matched, it names the
// register instead, because the two send an adopter to different places. A
// transition carrying no predicate is never the one named: it would have matched
// anything, so it says nothing about the bytes in hand.
func (s *State) Select(held RegisterFile, bytes func(*Predicate) []byte) (*Transition, error) {
	var excluded *Transition
	var by Guard

	// A transition carrying no predicate matches every record, and asking the
	// caller for the bytes of a target that does not exist is not how it says
	// so: `bytes` is called for a predicate and never for the absence of one.
	matches := func(predicate *Predicate) bool {
		return predicate == nil || predicate.Matches(bytes(predicate))
	}

	for _, transition := range s.Transitions {
		eligible, failed := transition.Eligible(held)
		if eligible {
			if matches(transition.Predicate) {
				return transition, nil
			}

			continue
		}

		if excluded != nil || transition.Predicate == nil {
			continue
		}

		if matches(transition.Predicate) {
			excluded, by = transition, failed
		}
	}

	if excluded != nil {
		return nil, &GuardExcludedError{
			State:    s.ID,
			Record:   excluded.Record,
			Register: by.Register.String(),
			Guard:    renderGuard(by),
		}
	}

	return nil, &UndescribedRecordError{State: s.ID, Records: admits(s)}
}

// SelectArm is the arm of a variant the occurrence in front of a consumer
// selects: the first whose predicate matches.
//
// There is no default arm and no falling through to the last one. Where no arm
// matches, that is reported — and reported as its own failure rather than as an
// undescribed record, because the two send an adopter to different places: a
// record no transition matched is a record type the layout is missing, while an
// occurrence no arm matched is a record the layout does describe carrying an
// entry it does not (docs/ir/SPEC.md, "A predicate on an arm reads one
// occurrence").
func (n *Node) SelectArm(bytes func(*Predicate) []byte) (*Arm, error) {
	for i := range n.Arms {
		if predicate := n.Arms[i].Predicate; predicate != nil && predicate.Matches(bytes(predicate)) {
			return &n.Arms[i], nil
		}
	}

	return nil, &UnmatchedOccurrenceError{Arms: alternatives(n)}
}

// admits is the records a state's transitions admit, in evaluation order and
// without repeating one.
func admits(state *State) []string {
	var records []string

	for _, transition := range state.Transitions {
		if !slices.Contains(records, transition.Record) {
			records = append(records, transition.Record)
		}
	}

	return records
}

// alternatives is what a variant's arms are called, in evaluation order.
func alternatives(variant *Node) []string {
	names := make([]string, 0, len(variant.Arms))
	for _, arm := range variant.Arms {
		names = append(names, arm.Alternative)
	}

	return names
}

// stretch is a run of a record's bytes: what a predicate tests.
type stretch struct{ at, width int }

// overlap reports whether one input can satisfy both predicates, each read at
// the run of bytes given for it.
//
// The test is whether one input can satisfy both and not whether the two read
// the same bytes, which is the distinction docs/ir/SPEC.md's "When two match,
// and when none does" draws: predicates reading different fields at different
// positions overlap just as thoroughly as two reading one, because a record can
// carry an `S` at byte zero and an `X` at byte ten at the same time. So two over
// different runs always overlap, and two over one run overlap exactly where
// their value sets intersect.
//
// A run of bytes rather than a field, because two records built to different
// copybooks is the ordinary case and is the case docs/ir/SPEC.md's "A counted
// run, as nodes" is written in: a header, a detail and a summary each carry
// their own type code at their own copybook's first byte, and a rule keyed on
// the field would call three type codes at one offset an ambiguity.
//
// A predicate that is absent matches every record, so it overlaps everything
// (#80).
func overlap(first *Predicate, at stretch, second *Predicate, against stretch) bool {
	if first == nil || second == nil {
		return true
	}

	if at != against {
		return true
	}

	return slices.ContainsFunc(first.Values, func(one Value) bool {
		return slices.ContainsFunc(second.Values, func(other Value) bool {
			return sameValue(one, other)
		})
	})
}

// literals resolves a strategy's literals against the field they are compared
// with, reporting the first that cannot be resolved.
//
// Every literal is resolved even after one has failed, because a strategy
// carrying three misspelled values is three things to fix rather than one to
// discover on the next run — the faults are collected by the caller, which knows
// what the strategy was written for.
type literalResolver struct {
	// field is the item the literals are compared against.
	field *copybook.Field

	// width is the bytes it occupies, which is what every value is padded to.
	width int

	// axes are the four encoding axes governing that item's bytes.
	axes layoutmodel.Axes
}

// resolve turns one literal into the bytes a consumer compares.
//
// What is wrong with a literal is returned as prose rather than as an error
// type, so that the caller can say whether a record's discriminator, an arm's
// predicate or a `when`'s guard was the thing carrying it — three positions with
// three different things to tell an adopter about the same misspelling.
func (r literalResolver) resolve(literal layoutmodel.Literal) (Value, string) {
	switch literal.Kind {
	case layoutmodel.BytesLiteral:
		return r.bytes(literal)
	case layoutmodel.TextLiteral:
		return r.text(literal)
	case layoutmodel.NumberLiteral:
		return r.digits(literal)
	}

	return Value{}, "it is not a literal this package can read"
}

// bytes resolves `(bytes "F0F1")`: exactly what is in the file, with no charset
// and no padding.
//
// It is the one spelling that says what the bytes are rather than what the value
// is, so a width that does not match the item's is the adopter having counted
// wrong — padding it would be this package deciding which end the missing bytes
// belong at.
func (r literalResolver) bytes(literal layoutmodel.Literal) (Value, string) {
	written := literal.Bytes.Bytes
	if len(written) != r.width {
		return Value{}, "it is " + plural(len(written), "byte") +
			" and " + itemName(r.field) + " is " + plural(r.width, "byte")
	}

	return Value{Literal: literal, Bytes: slices.Clone(written)}, ""
}

// text resolves `"01"`: through the item's charset, padded on the right to the
// item's width with that charset's space.
//
// Padding is the producer's under docs/ir/SPEC.md's "Discriminator predicates",
// and it is on the right because that is where COBOL leaves an alphanumeric item
// short of its PICTURE. A literal longer than the item is refused rather than
// truncated: a comparison against a prefix is the reading that reports a match
// on bytes the adopter never asked about.
func (r literalResolver) text(literal layoutmodel.Literal) (Value, string) {
	// An item the layout says carries no characters is reported as that rather
	// than as an item nobody stated a charset for. The two are opposite faults —
	// one is an unanswered question and this is the answer — and the way out of
	// this one is to write the bytes, which the message names.
	if r.axes.Charset == layoutmodel.None {
		return Value{}, itemName(r.field) + " carries no charset, so text has no bytes on it" +
			" — write what is in the file as (bytes \"…\")"
	}

	charset, exact := charsetOf(r.axes.Charset)
	if charset == nil {
		return Value{}, "the charset is not stated on " + itemName(r.field)
	}

	if len([]rune(literal.Text)) > r.width {
		return Value{}, "it is " + plural(len([]rune(literal.Text)), "character") +
			" and " + itemName(r.field) + " is " + plural(r.width, "byte")
	}

	out := make([]byte, 0, r.width)
	for _, r0 := range literal.Text {
		if !exact && !invariant(r0) {
			return Value{}, "the character " + strconv.QuoteRune(r0) +
				" is not one every EBCDIC code page spells alike, and `cobol-go`" +
				" carries no table for " + string(r.axes.Charset) + " yet"
		}

		encoded, ok := charset.FromUnicode(r0)
		if !ok {
			return Value{}, "the character " + strconv.QuoteRune(r0) +
				" has no byte in " + string(r.axes.Charset)
		}

		out = append(out, encoded)
	}

	for len(out) < r.width {
		out = append(out, charset.Space())
	}

	return Value{Literal: literal, Bytes: out}, ""
}

// digits resolves `12`: the digits of a zoned DISPLAY item, right-aligned and
// zero-filled to the item's width, through the charset's own digit bytes.
//
// It resolves for exactly the shape a discriminator has and refuses every other,
// which is a narrowing worth its diagnostic. The bytes of a number in a packed,
// binary or signed item follow from a SIGN clause and a byte order that the four
// encoding axes do not by themselves pin down, and a producer emitting plausible
// bytes for one would be making the silent-failure mistake this whole project is
// arranged against. `(bytes …)` is the spelling for saying what is in the file,
// and the diagnostic says so.
func (r literalResolver) digits(literal layoutmodel.Literal) (Value, string) {
	charset, _ := charsetOf(r.axes.Charset)
	if charset == nil {
		return Value{}, "the charset is not stated on " + itemName(r.field)
	}

	if reason := zonedUnsigned(r.field); reason != "" {
		return Value{}, "a number is resolved through the digits of a zoned item and " +
			itemName(r.field) + " " + reason + " — write the bytes as (bytes \"…\")"
	}

	if strings.ContainsAny(literal.Number, ".-") {
		return Value{}, "it is not a whole number and " + itemName(r.field) +
			" holds " + plural(r.width, "digit")
	}

	if len(literal.Number) > r.width {
		return Value{}, "it is " + plural(len(literal.Number), "digit") +
			" and " + itemName(r.field) + " holds " + plural(r.width, "digit")
	}

	out := make([]byte, 0, r.width)
	for i := len(literal.Number); i < r.width; i++ {
		encoded, _ := charset.FromUnicode('0')
		out = append(out, encoded)
	}

	for _, r0 := range literal.Number {
		encoded, ok := charset.FromUnicode(r0)
		if !ok {
			return Value{}, "the digit " + strconv.QuoteRune(r0) + " has no byte in " +
				string(r.axes.Charset)
		}

		out = append(out, encoded)
	}

	return Value{Literal: literal, Bytes: out}, ""
}

// zonedUnsigned says what stops a field being an unsigned zoned DISPLAY item, or
// the empty string where nothing does.
func zonedUnsigned(field *copybook.Field) string {
	switch {
	case field == nil || field.Kind == copybook.KindGroup:
		return "is not an elementary item"
	case field.Usage != copybook.UsageDisplay:
		return "is USAGE " + field.Usage.String()
	case field.Picture == nil:
		return "carries no PICTURE"
	case field.Picture.Category != picture.CategoryNumeric:
		return "is a " + field.Picture.Category.String() + " item"
	case field.Picture.Scale != 0:
		return "carries digits after the decimal point"
	case field.Picture.Signed:
		return "carries an operational sign"
	}

	return ""
}

// charsetOf is the byte table for one of the code pages a layout may name, and
// whether that table is the code page's own.
//
// `cobol-go` carries a hand-written table per code page and has written two of
// the five so far, because `codec` is forbidden from depending on
// golang.org/x/text. What is available for the other three is what
// `codec/SPEC.md` states normatively about all of them: "the digits `F0`–`F9`,
// the letters, and the space `40` are identical across all of them". So an
// EBCDIC code page with no table of its own resolves a literal through cp037's
// over exactly that subset, and a character outside it is reported rather than
// guessed — which is the same answer this package gives everywhere a byte is not
// derivable, and it costs a discriminator nothing, since a type code is digits
// and letters.
func charsetOf(charset layoutmodel.Charset) (codec.Charset, bool) {
	switch charset {
	case layoutmodel.ASCII:
		return codec.ASCII(), true
	case layoutmodel.CP037:
		return codec.CP037(), true
	case layoutmodel.CP500, layoutmodel.CP1047, layoutmodel.CP1140:
		return codec.CP037(), false
	}

	return nil, false
}

// invariant reports whether a character has the same byte in every EBCDIC code
// page the charset axis admits: a digit, a letter, or the space.
func invariant(r rune) bool {
	return r == ' ' ||
		(r >= '0' && r <= '9') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= 'a' && r <= 'z')
}

// plural renders a count and its unit, so that a message says one byte and two
// bytes rather than 1 byte(s).
func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}

	return strconv.Itoa(n) + " " + unit + "s"
}
