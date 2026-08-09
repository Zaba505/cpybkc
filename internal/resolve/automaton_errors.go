// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// The faults compiling a sequencing expression reports.
//
// Every one names what docs/ir/SPEC.md and docs/layout/SPEC.md ask it to name —
// the record, the item, the enclosing group that repeats, the two records a
// state cannot tell apart — because the generic version of each has been
// observed to send a reader to the wrong file. "The automaton counts; it does
// not compute" says it in as many words: a layout needing what the register
// machinery does not do is told so, *rather than being told its discriminators
// overlap*.
//
// Most of them carry two spans, which is docs/layout/SPEC.md's "Every diagnostic
// carries a span, and some carry two": the layout is where the operator was
// written and the copybook is where the item it names is, and a message naming
// only one leaves the reader to find the other by hand.

// ErrNilSequence is returned by [CompileSequence] when it is handed no sequence.
var ErrNilSequence = errors.New("resolve: nil sequence")

// SequenceItemError is a `times` or a `when` whose item reference does not
// resolve to exactly one item of the record it is rooted at.
//
// The reference's own spelling is `layoutmodel`'s and has already been checked;
// what needs the copybook is whether the path names an item at all. A complete
// path that still names two is a reference nothing can resolve — duplicate data
// names are legal COBOL — and is reported rather than resolved to the first,
// because choosing would bind a register from bytes the adopter did not name.
type SequenceItemError struct {
	// Pos is the item reference in the layout.
	Pos diag.Span

	// Copybook is the copybook the record is bound to, where there is one to
	// point into.
	Copybook diag.Span

	// Operator is the operator the reference stands under, as a layout
	// spells it: `times` or `when`.
	Operator string

	// Item is the reference as the layout wrote it.
	Item layoutmodel.ItemRef

	// Record is the record it is rooted at.
	Record string

	// Found is what is wrong with it, in the message's own words.
	Found string
}

// Error implements the error interface.
func (e *SequenceItemError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *SequenceItemError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("the %s reads %s, and %s", e.Operator, e.Item, e.Found),
		Spans:   spans(e.Pos, e.Copybook, "the record "+e.Record+" is bound to this copybook"),
	}
}

// SequenceOccurrenceError is a `times` or a `when` naming an item that repeats,
// or one contained at any depth in a group that repeats.
//
// docs/ir/SPEC.md's "A reference names a field, not an occurrence of one" is the
// rule and this is one of the positions it binds: a binding names a field, no
// node carries an occurrence number, and an item with a value per occurrence is
// therefore a value the automaton has no way to name. Which occurrence of a
// header's table said how many details follow is not a question the IR has a
// spelling for, and answering it with the first is the reading that looks right
// and reads data belonging to the table rather than to the record (#76, #84).
type SequenceOccurrenceError struct {
	// Pos is the item reference in the layout.
	Pos diag.Span

	// Copybook is the item's entry in the copybook.
	Copybook diag.Span

	// Operator is the operator the reference stands under.
	Operator string

	// Record is the record the reference is rooted at, and Item the item it
	// names.
	Record string
	Item   string

	// Group is the innermost repeating group the item sits in, empty where
	// the item is itself the thing that repeats.
	Group string
}

// Error implements the error interface.
func (e *SequenceOccurrenceError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *SequenceOccurrenceError) Diagnostic() diag.Diagnostic {
	repeats := e.Item + " repeats"
	if e.Group != "" {
		repeats = e.Item + " sits in the repeating group " + e.Group
	}

	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"the %s reads %s of record %s, and %s: a register is bound from a field and nothing names an "+
				"occurrence of one, so there is no saying which occurrence the value would come from",
			e.Operator, e.Item, e.Record, repeats),
		Spans: spans(e.Pos, e.Copybook, "declared here"),
	}
}

// SequenceCountKindError is a `times` whose item is not one whose value decodes
// to an integer.
//
// docs/layout/SPEC.md requires it of the operator, and docs/ir/SPEC.md requires
// it of the register: an integer register holds a number decoded from its source
// field by that field's own encoding axes, and a producer must not bind a field
// whose value does not decode to the register's kind. A count that is a group,
// an alphanumeric item or a number with digits after the point has no reading
// that makes it a number of records.
type SequenceCountKindError struct {
	// Pos is the item reference in the layout.
	Pos diag.Span

	// Copybook is the item's entry in the copybook.
	Copybook diag.Span

	// Record is the record the reference is rooted at, and Item the item it
	// names.
	Record string
	Item   string

	// Found is what the item is instead, in the message's own words.
	Found string
}

// Error implements the error interface.
func (e *SequenceCountKindError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *SequenceCountKindError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"the times counts records by %s of record %s, and %s is %s: a count is held in a register as a "+
				"number, so the item it is read from has to be one",
			e.Item, e.Record, e.Item, e.Found),
		Spans: spans(e.Pos, e.Copybook, "declared here"),
	}
}

// UnboundRegisterError is a register read on some path that has not bound it.
//
// docs/ir/SPEC.md's "A register is read only where it has been written" requires
// every path from the start state to a read to pass through a binding on a
// transition taken **strictly earlier** than the reading one, and "A value the
// automaton has not read yet" is the exclusion it keeps: a consumer reads a
// stream forward, and one that could not decide a transition until a later
// record arrived would have to buffer an unbounded stretch of a file whose whole
// premise is that it does not fit in memory.
//
// Two layouts land here and the message tells them apart, because they send an
// adopter to different places. One reads a value out of a record that some path
// reaches the operator without having admitted — a trailer's count governing the
// records in front of it, or a header that may or may not have appeared. The
// other counts a run by a field of the record being counted, which
// [UnboundRegisterError.OnAdmitting] reports: the transition admitting the
// record binds the register at step 7 of the read loop, and both the guard that
// would read it and the extent it decides are wanted before then, so on the
// first admission the register holds nothing and on every later one it holds the
// previous record's value (#88).
type UnboundRegisterError struct {
	// Pos is the item reference in the layout.
	Pos diag.Span

	// Item is the reference as the layout wrote it, which is what names the
	// register: a register declares its kind and nothing else, so the value
	// it was to hold is what an adopter recognises it by.
	Item layoutmodel.ItemRef

	// Register is the record the value would have been read out of.
	Register string

	// Reader is the record admitted by the transition that reads it, empty
	// where what reads it is a state's acceptance rather than a transition.
	Reader string

	// State is the identifier of the state the read is at.
	State int

	// OnAdmitting reports that the reading transition is itself the one that
	// binds the register, out of the record it admits.
	OnAdmitting bool

	// register is the register's identifier, which is what makes one
	// operator one fault: an unbound count is read by every transition of
	// the run it bounds and by the acceptance of every state leaving it, and
	// four messages about one misspelled reference is three too many.
	//
	// It is unexported because a register's identifier is this package's
	// bookkeeping and not something a caller asserting on the fault has any
	// use for: what names the register to an adopter is
	// [UnboundRegisterError.Item].
	register int
}

// Error implements the error interface.
func (e *UnboundRegisterError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *UnboundRegisterError) Diagnostic() diag.Diagnostic {
	// The whole clause and not a fragment of one, because the two reads are
	// not the same part of speech: a transition reads a register and a state's
	// acceptance does too, and a sentence built to fit the first describes the
	// second as a transition that is not there.
	read := fmt.Sprintf("state %d's acceptance", e.State)
	if e.Reader != "" {
		read = "the transition admitting " + e.Reader
	}

	if e.OnAdmitting {
		return diag.Diagnostic{
			Message: fmt.Sprintf(
				"%s is read out of %s, and what reads it is %s, which is the only thing that binds it: "+
					"a binding applies after the record is admitted, so the value is wanted before there is one",
				e.Item, e.Register, read),
			Spans: []diag.Span{e.Pos},
		}
	}

	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"%s is read out of %s, and there is a path to %s on which no %s has been admitted: "+
				"a value the automaton has not read yet governs nothing",
			e.Item, e.Register, read, e.Register),
		Spans: []diag.Span{e.Pos},
	}
}

// SequenceAmbiguityError is two transitions leaving one state that can be
// eligible together and are selected by predicates that can both match one
// record.
//
// docs/ir/SPEC.md's "When two match, and when none does" refuses the pair rather
// than settling it, so that the question docs/layout/SPEC.md defers — what
// happens when two discriminators match — does not arise: such an IR is never
// produced.
//
// [SequenceAmbiguityError.Unguarded] is the case #80 folds into the same rule. A
// transition carrying no predicate matches every record, so it overlaps every
// transition leaving its state whose guards can hold at the same time as its
// own, and guards are then the only thing that can separate the two. The message
// says which of the two shapes it is, because the fixes differ: one is a
// discriminator to narrow and the other is a record type that offers nothing to
// tell it apart by.
type SequenceAmbiguityError struct {
	// Pos is where the second of the two record names was written, and First
	// where the first was.
	//
	// Two spans into one file, which is the shape a fault about a *pair*
	// takes: what is ambiguous is the point where two appearances meet, and
	// neither of them alone is the fault.
	Pos   diag.Span
	First diag.Span

	// State is the identifier of the state offering both.
	State int

	// Records are the records the two transitions admit.
	Records [2]string

	// Unguarded reports that one of the two carries no predicate at all.
	Unguarded bool

	// Guards reports that at least one of the two carries guards, so that the
	// message can say the guards do not separate them rather than leaving an
	// adopter to wonder whether they were read.
	Guards bool
}

// Error implements the error interface.
func (e *SequenceAmbiguityError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *SequenceAmbiguityError) Diagnostic() diag.Diagnostic {
	why := "their discriminators can both match one record"
	if e.Unguarded {
		why = "one of them carries no discriminator, so it matches every record"
	}

	separated := ""
	if e.Guards {
		separated = ", and their guards can hold at the same time"
	}

	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"at state %d the sequence admits %s and %s, and %s%s: a consumer reaching that point has no way "+
				"to tell which record is in front of it",
			e.State, e.Records[0], e.Records[1], why, separated),
		Spans: spans(e.Pos, e.First, "the other is admitted here"),
	}
}

// SequenceAcceptanceError is a point in the expression that would accept end of
// input under either of two different sets of guards.
//
// A state carries one list of acceptance guards and a list is a conjunction, so
// a disjunction of two conditions has nowhere to go. docs/ir/SPEC.md puts
// disjunction in the state — a second transition leaving it — and a state's
// acceptance is not a transition, so this is the one place the shape has no
// second half.
//
// It is reported rather than narrowed to one of the two. Taking the first would
// report a complete file as truncated whenever the other condition was the one
// that held, and dropping the guards would accept a file that stops short — and
// a file wrongly called complete is the failure guarded acceptance exists to
// prevent.
type SequenceAcceptanceError struct {
	// Pos is the `sequence` form in the layout.
	Pos diag.Span

	// What is the record whose state it is, or "the file" for the start
	// state.
	What string

	// Ways is how many conditions the point would accept under.
	Ways int

	// Guard is one of them, rendered, so that the message names something an
	// adopter can find in the expression.
	Guard string
}

// Error implements the error interface.
func (e *SequenceAcceptanceError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *SequenceAcceptanceError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"after %s the sequence is complete under %d different sets of guards, one of them %s: a state "+
				"accepts under one set of guards, and there is nowhere to write the second",
			e.What, e.Ways, e.Guard),
		Spans: []diag.Span{e.Pos},
	}
}

// spans is a fault's places: where it is, and the copybook it implicates where
// there is one to point into.
func spans(at, copybook diag.Span, note string) []diag.Span {
	if !copybook.Stated() {
		return []diag.Span{at}
	}

	copybook.Note = note

	return []diag.Span{at, copybook}
}

// copybookSpan is where in a record's copybook a field is, and the copybook
// itself where the fault is about no field in particular.
//
// It is [resolver.span]'s counterpart for a fault about a record the sequencing
// layer named: the path comes from that record's `record` form rather than from
// one set of [Options].
func copybookSpan(record SequencedRecord, field *copybook.Field) diag.Span {
	span := diag.Span{File: record.Copybook}
	if field != nil {
		span.Line, span.Column = field.Pos.Line, field.Pos.Column
	}
	return span
}

// renderGuards draws a guard list the way a message quotes one.
func renderGuards(guards []Guard) string {
	if len(guards) == 0 {
		return "no guard at all"
	}

	rendered := make([]string, 0, len(guards))
	for _, guard := range guards {
		rendered = append(rendered, renderGuard(guard))
	}

	return strings.Join(rendered, " and ")
}

// renderGuard draws one guard.
func renderGuard(guard Guard) string {
	switch guard.Test {
	case GuardPositive:
		return guard.Register.String() + " above zero"
	case GuardOneOf:
		literals := make([]string, 0, len(guard.Literals))
		for _, literal := range guard.Literals {
			literals = append(literals, literal.String())
		}

		return guard.Register.String() + " one of " + strings.Join(literals, ", ")
	case GuardEquals:
		if len(guard.Literals) == 0 {
			return guard.Register.String() + " equal to nothing"
		}

		return guard.Register.String() + " equal to " + guard.Literals[0].String()
	}

	return guard.Register.String() + " tested by nothing"
}
