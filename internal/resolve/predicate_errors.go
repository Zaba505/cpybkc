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

// The faults compiling a discriminator into a predicate reports.
//
// Each names what docs/ir/SPEC.md asks it to name and refuses the generic
// version, because a discriminator goes wrong in five ways that look alike from
// a distance and send an adopter to five different files. A target in the wrong
// record is a layout mistake; a target inside a table is a copybook the adopter
// may not own; a target behind an `OCCURS DEPENDING ON` is a rule about the read
// loop's order; a literal that will not resolve is a spelling; and a strategy
// this format refuses is a file shape the IR cannot describe. "rather than
// reporting a generic reference error" is the spec's own phrase, and it appears
// against three of these.

// PredicateTargetError is a discriminator whose item reference does not name one
// item of the record it discriminates.
//
// docs/ir/SPEC.md's "Discriminator predicates" requires a producer to ensure the
// target is contained in the record its transition admits, at any depth, and
// never to name a field of any other. `layoutmodel` checks the half that needs
// no copybook — the reference's root — and this is the half that needs one, plus
// the same root check over a [Sequencing] a caller assembled by hand.
//
// A complete path that names two items is reported rather than resolved to the
// first, for [SequenceItemError]'s reason: duplicate data names are legal COBOL,
// and choosing would select a record on bytes the adopter did not name.
type PredicateTargetError struct {
	// Pos is the item reference in the layout.
	Pos diag.Span

	// Copybook is the copybook the record is bound to.
	Copybook diag.Span

	// Record is the record being discriminated, and Item the reference as the
	// layout wrote it.
	Record string
	Item   layoutmodel.ItemRef

	// Found is what is wrong with it, in the message's own words.
	Found string
}

// Error implements the error interface.
func (e *PredicateTargetError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *PredicateTargetError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"the discriminator for record %s tests %s, and %s: a predicate names a field contained in the "+
				"record its transition admits",
			e.Record, e.Item, e.Found),
		Spans: spans(e.Pos, e.Copybook, "the record "+e.Record+" is bound to this copybook"),
	}
}

// PredicateOccurrenceError is a record discriminator whose target repeats, or
// sits at any depth inside a group that repeats.
//
// docs/ir/SPEC.md's "A reference names a field, not an occurrence of one" is the
// rule and a transition's predicate is one of the positions it binds: no node
// carries an occurrence number, so a target in forty entries carries nothing
// saying which of the forty, and reading it as the first is a guess at an
// intention the descriptor cannot express (#84).
//
// An arm's predicate is outside this rule and is required to sit inside a
// repeating group, because it is evaluated in the occurrence being walked and
// there is nothing left to guess — [ArmTargetScopeError] is that scope's fault
// (#90).
type PredicateOccurrenceError struct {
	// Pos is the item reference in the layout.
	Pos diag.Span

	// Copybook is the item's entry in the copybook.
	Copybook diag.Span

	// Record is the record being discriminated, and Item the item the target
	// names.
	Record string
	Item   string

	// Group is the innermost repeating group the target sits in, empty where
	// the target is itself the thing that repeats.
	Group string
}

// Error implements the error interface.
func (e *PredicateOccurrenceError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *PredicateOccurrenceError) Diagnostic() diag.Diagnostic {
	repeats := e.Item + " repeats"
	if e.Group != "" {
		repeats = e.Item + " sits in the repeating group " + e.Group
	}

	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"the discriminator for record %s tests %s, and %s: a predicate names a field and nothing names "+
				"an occurrence of one, so there is no saying which entry the value would come from",
			e.Record, e.Item, repeats),
		Spans: spans(e.Pos, e.Copybook, "declared here"),
	}
}

// PredicatePositionError is a record discriminator whose target sits behind an
// item whose extent moves with a count.
//
// docs/ir/SPEC.md's "Discriminator predicates" requires the target's position to
// be constant within the record its transition would admit, and the reason is
// the read loop's order: a predicate is evaluated at step 3, before its record
// has been admitted, so a target whose position depends on a count obliges a
// consumer to decode that count out of bytes nobody has identified. The bytes
// may be another record type's, sending the read to an offset the layout never
// described, or they may not decode at all — and one consumer then condemns a
// well-formed file while another treats the predicate as not matching and reads
// it correctly (#84).
//
// An arm's predicate carries no such restriction, because it runs against a
// record already admitted (#90).
type PredicatePositionError struct {
	// Pos is the item reference in the layout.
	Pos diag.Span

	// Copybook is the target's entry in the copybook.
	Copybook diag.Span

	// Record is the record being discriminated, and Item the target.
	Record string
	Item   string

	// Behind is the variable item in front of the target, and Count the field
	// its occurrence count is read from.
	Behind string
	Count  string
}

// Error implements the error interface.
func (e *PredicatePositionError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *PredicatePositionError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"the discriminator for record %s tests %s, and %s lies in front of it with as many entries as %s "+
				"says: a predicate runs before its record has been admitted, so its target has to sit at the "+
				"same offset in every record of that type",
			e.Record, e.Item, e.Behind, e.Count),
		Spans: spans(e.Pos, e.Copybook, "declared here"),
	}
}

// PredicateLiteralError is a literal that cannot be resolved to the bytes a
// consumer compares.
//
// docs/ir/SPEC.md requires both members of the predicate set to carry their
// literals as bytes, already padded to the target's width, so that a consumer
// compares the whole of the target and never applies a COBOL comparison rule of
// its own. A literal wider than the item, a character the code page has no byte
// for, and a number against an item whose bytes do not follow from the encoding
// axes alone are the three ways that resolution fails, and each is reported
// rather than resolved to something plausible.
type PredicateLiteralError struct {
	// Pos is the literal in the layout.
	Pos diag.Span

	// Copybook is the item's entry in the copybook.
	Copybook diag.Span

	// Subject is what carried the literal, as the message names it: a
	// record's discriminator, an arm of a variant, or a `when`.
	Subject string

	// Literal is the literal as the layout wrote it, and Item the item it is
	// compared against.
	Literal layoutmodel.Literal
	Item    string

	// Found is why it will not resolve, in the message's own words.
	Found string
}

// Error implements the error interface.
func (e *PredicateLiteralError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *PredicateLiteralError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"%s compares %s against %s, and %s: a literal reaches a consumer as the bytes it compares, "+
				"padded to the width of the item",
			e.Subject, e.Item, e.Literal, e.Found),
		Spans: spans(e.Pos, e.Copybook, "declared here"),
	}
}

// RefusedStrategyError is a discriminator asking for something a predicate
// cannot test.
//
// Three strategies were proposed for the layout format and refused, and each is
// refused on its own terms rather than by an appeal to tidiness. A record's
// length is a framing byte's value under two framings and is not in the file at
// all under the other two ("A record told apart only by its length"). Positional
// *last* is refused because a writer does not know which record is last until
// its caller says there are no more, which is after that record has been written
// ("The last record of a stream"). And positional *first* is not refused: it
// lowers into the start state, and this fires only where the record is admitted
// somewhere else besides.
//
// The rejection is keyed on what the layout asks for, because that is the only
// thing `resolve` can test. Whether two records are *in fact* told apart by some
// field is a property of the adopter's data, so a rule keyed on "nothing a field
// can test" would have a diagnostic that was false whenever it fired.
type RefusedStrategyError struct {
	// Pos is the strategy in the layout.
	Pos diag.Span

	// Record is the record it was written for, and Strategy what it asked
	// for.
	Record   string
	Strategy string

	// Beside are the records this one has to be told apart from: those
	// admitted by a transition eligible at the same time as one admitting it.
	// It is empty where nothing has to be told from it.
	Beside []string

	// Found is why the strategy is refused, in the message's own words.
	Found string
}

// Error implements the error interface.
func (e *RefusedStrategyError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *RefusedStrategyError) Diagnostic() diag.Diagnostic {
	beside := ""
	if len(e.Beside) > 0 {
		beside = ", which has to be told apart from " + list(e.Beside)
	}

	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"record %s is discriminated by %s%s, and %s",
			e.Record, e.Strategy, beside, e.Found),
		Spans: []diag.Span{e.Pos},
	}
}

// UnnameableRecordError is a record every field of which sits inside a group
// that repeats, standing beside another record a consumer would have to tell it
// apart from.
//
// docs/ir/SPEC.md's "A record with nothing outside a table to name" is the whole
// of it: two transitions that can be eligible at once have to be told apart by a
// predicate, a predicate names a field contained in the record it admits, and a
// target inside a repeating group is refused — so a record with no field outside
// one leaves nothing to select it with. A variant does not rescue it, because an
// arm is evaluated only once the record has been admitted.
//
// It is narrower than the heading, and deliberately: what is refused is the
// record standing beside another at one state, not the record. A file of one
// record type, or a state whose alternatives a guard already separates, admits
// it under a transition carrying no predicate (#80).
type UnnameableRecordError struct {
	// Pos is where the record is admitted in the layout's expression.
	Pos diag.Span

	// Copybook is the record's entry in its copybook.
	Copybook diag.Span

	// State is the state offering both transitions.
	State int

	// Record is the record with nothing to name, and Beside the record it
	// would have to be told apart from.
	Record string
	Beside string
}

// Error implements the error interface.
func (e *UnnameableRecordError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *UnnameableRecordError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"at state %d the sequence admits %s and %s, and %s offers no target a predicate may name: every "+
				"field of it sits inside a group that repeats, and a predicate names a field that does not",
			e.State, e.Record, e.Beside, e.Record),
		Spans: spans(e.Pos, e.Copybook, "declared here"),
	}
}

// ZeroExtentError is a transition admitting a record whose extent is zero.
//
// docs/ir/SPEC.md's "A transition may carry no predicate" states the requirement
// where it became necessary. Before that section every admitted record held at
// least one field of non-zero width, because a predicate's target had to be one;
// once a transition may carry no predicate, nothing else forbids an empty group.
// Such a transition is a reader that emits records forever without advancing its
// read position (#80).
type ZeroExtentError struct {
	// Pos is the discriminator in the layout, which is the form naming the
	// record this is about.
	Pos diag.Span

	// Copybook is the record's entry in its copybook.
	Copybook diag.Span

	// Record is the record the transition admits, and Item the copybook item
	// it is bound to.
	Record string
	Item   string
}

// Error implements the error interface.
func (e *ZeroExtentError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *ZeroExtentError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"the sequence admits record %s and %s occupies no bytes: a transition consumes exactly one "+
				"record, so one admitting a record of no extent is a reader that never advances",
			e.Record, e.Item),
		Spans: spans(e.Pos, e.Copybook, "declared here"),
	}
}

// ArmTargetScopeError is an arm's predicate whose target does not stand where an
// arm's target may stand.
//
// docs/ir/SPEC.md's "A predicate on an arm reads one occurrence" binds it two
// ways, and both are about bytes that may not be there or may not be what they
// look like. A target outside the occurrence has the same bytes in every
// occurrence, so it selects the same arm in all of them — a choice made once per
// record, which is a record node's and not a variant's. A target inside a
// sibling arm has bytes only where that arm was selected, so reading it where it
// was not is reading one alternative's data as another's (#90).
//
// `layoutmodel` checks the halves that need no copybook — the reference's root,
// the outermost group, the variant itself and the arm's own name. What needs one
// is that the group the target shares with the variant is the *innermost
// repeating* one, which is this.
type ArmTargetScopeError struct {
	// Pos is the item reference in the layout.
	Pos diag.Span

	// Copybook is the redefined item's entry in the copybook.
	Copybook diag.Span

	// Record is the record being resolved, Group the innermost repeating group
	// the variant sits in, Redefined the item the arms redefine and
	// Alternative the arm carrying the predicate.
	Record      string
	Group       string
	Redefined   string
	Alternative string

	// Item is the target as the layout wrote it.
	Item layoutmodel.ItemRef

	// Found is what is wrong with it, in the message's own words.
	Found string
}

// Error implements the error interface.
func (e *ArmTargetScopeError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *ArmTargetScopeError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s the arm %s of %s is selected by a predicate testing %s, and %s: an arm's predicate "+
				"reads one occurrence of %s, so its target sits inside that occurrence",
			e.Record, e.Alternative, e.Redefined, e.Item, e.Found, e.Group),
		Spans: spans(e.Pos, e.Copybook, "the variant is here"),
	}
}

// ArmOverlapError is two arms of one variant that one occurrence can satisfy
// both of.
//
// docs/ir/SPEC.md's "A predicate on an arm reads one occurrence" is "When two
// match, and when none does" at this scope and for its reasons. There are no
// guards on an arm to exempt a pair — a guard reads a register, a register holds
// one value for the whole of a record's read, and a value that is the same in
// every occurrence selects a record rather than an arm — so every pair of arms
// is inside the rule (#90).
//
// `layoutmodel` refuses the smallest version of this without a copybook: two
// arms testing one item for one spelling of one literal. What needs a copybook
// is the rest — one literal written as text and another as bytes, or two that
// resolve to the same bytes under the item's charset and width — and this is it.
type ArmOverlapError struct {
	// Pos is the second arm in the layout, and First the arm it overlaps.
	Pos   diag.Span
	First diag.Span

	// Record is the record being resolved, Group the innermost repeating group
	// the variant sits in, and Redefined the item the arms redefine.
	Record    string
	Group     string
	Redefined string

	// Arms are the two alternatives, in the order the layout writes them.
	Arms [2]string

	// Value is the value one occurrence satisfies both with.
	Value Value
}

// Error implements the error interface.
func (e *ArmOverlapError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *ArmOverlapError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s the arms %s and %s of %s in %s are both selected by %s: an occurrence satisfying "+
				"both is one no consumer can choose an alternative for, and there is no default arm",
			e.Record, e.Arms[0], e.Arms[1], e.Redefined, e.Group, e.Value),
		Spans: spans(e.Pos, e.First, "the other arm is here"),
	}
}

// The two failures a consumer reports while it is reading, which are not faults
// in a descriptor at all.
//
// They are here because docs/ir/SPEC.md requires them to be told apart and the
// telling apart is worth doing once. An adopter sent to the first for the second
// spends the day on a record type they already have.

// UndescribedRecordError is a record no transition of the state matched.
//
// docs/ir/SPEC.md's "When two match, and when none does" requires a consumer to
// report it rather than skipping ahead to a transition that matches later or
// falling through to a default. There is no default, and a file containing an
// undescribed record is a file the layout is wrong about.
type UndescribedRecordError struct {
	// State is the state the consumer was in.
	State int

	// Records are the records that state admits, in evaluation order, which
	// is the list an adopter compares their file against.
	Records []string
}

// Error implements the error interface.
func (e *UndescribedRecordError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *UndescribedRecordError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"at state %d the record in front of the consumer is none of %s: the layout does not describe it",
			e.State, list(e.Records)),
	}
}

// GuardExcludedError is a record a transition's predicate matched, on a
// transition a guard made ineligible.
//
// docs/ir/SPEC.md's "When two match, and when none does" asks a consumer to
// report this instead of an undescribed record, naming the register the guard
// tested. The two send an adopter to different places: an undescribed record
// means the layout is missing a record type, while a detail arriving after its
// counter reached zero means the file and its own header disagree about how many
// there are. Only the wording differs, and it is the wording that saves a day.
//
// A transition carrying no predicate is never reported here. It would have
// matched anything, so it says nothing about the bytes in hand, and reporting it
// would displace the undescribed-record diagnostic exactly where that diagnostic
// is right (#80).
type GuardExcludedError struct {
	// State is the state the consumer was in, and Record the record the
	// excluded transition would have admitted.
	State  int
	Record string

	// Register is the register the guard tested, and Guard the test, rendered
	// as a message quotes one.
	Register string
	Guard    string
}

// Error implements the error interface.
func (e *GuardExcludedError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *GuardExcludedError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"at state %d the record in front of the consumer is a %s, and the transition admitting one is "+
				"not eligible: it holds while %s, and %s does not",
			e.State, e.Record, e.Guard, e.Register),
	}
}

// UnmatchedOccurrenceError is an occurrence of a table no arm of its variant
// selected.
//
// docs/ir/SPEC.md's "A predicate on an arm reads one occurrence" requires a
// consumer to report it and not to fall through to the last arm or leave the
// occurrence's items unset. There is no default arm and a producer cannot write
// one; an adopter whose entries carry a code the alternatives do not cover
// writes an arm for the residue, spelled as the test over the codes it covers.
type UnmatchedOccurrenceError struct {
	// Arms are the alternatives the variant offers, in evaluation order.
	Arms []string
}

// Error implements the error interface.
func (e *UnmatchedOccurrenceError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *UnmatchedOccurrenceError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"this entry is none of %s: the layout describes the record it sits in and not this entry of it",
			list(e.Arms)),
	}
}

// list renders several names the way a message names them.
func list(names []string) string {
	switch len(names) {
	case 0:
		return "nothing"
	case 1:
		return names[0]
	}

	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
