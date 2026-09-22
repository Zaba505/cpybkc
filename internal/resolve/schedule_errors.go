// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"fmt"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// A fault here names the record, the repeating group the variant sits in and the
// item the copybook redefines, for the reason errors.go states of the arm
// family: a message saying that a schedule does not cover a table sends an
// adopter looking through two files for it, and one naming the entry, the table
// it is in and the alternative that does not fit sends them to the line.
//
// The span is the copybook's, which is the same span every other fault about an
// arm carries. The numbers are the layout's and the message quotes them, but the
// thing the two files disagree about is how many entries the table has — and
// that is written in the copybook, where the adopter has to go to find out
// whether the schedule is wrong or the OCCURS is.

// MixedSelectorError is a variant whose arms are not all selected the same way:
// some by the bytes of the occurrence in front of them and some by which
// occurrence that is.
//
// docs/ir/SPEC.md's "One kind of selector per variant" is the rule. Two things
// decide it. Exhaustiveness and overlap are different questions for the two
// kinds — one is decided statically over 1..M and the other is a property of
// bytes — so a mixed variant would need both checks and a third rule saying
// which wins where a predicate matches an occurrence a schedule also covers. And
// the arm is the caller's under one kind and the descriptor's under the other,
// so a mixed variant would give one call two behaviours selected by a property
// of the layout the caller cannot see.
type MixedSelectorError struct {
	// Pos is the redefined item's entry in the copybook.
	Pos diag.Span

	// Record is the record being resolved.
	Record string

	// Group is the innermost repeating group the variant sits in.
	Group string

	// Redefined is the item the copybook redefines.
	Redefined string

	// Scheduled are the arms chosen by position, and Tested the arms chosen
	// by the bytes in front of them. Both are named, because either half
	// could be the one the adopter meant and a message naming one of them
	// has already decided which.
	Scheduled []string
	Tested    []string
}

// Error implements the error interface.
func (e *MixedSelectorError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *MixedSelectorError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s, the variant on %s in the repeating group %s mixes two kinds of selector: %s %s chosen by position and %s %s chosen by the bytes of an occurrence, and every arm of one variant has to be chosen the same way",
			e.Record, e.Redefined, e.Group,
			list(e.Scheduled), was(len(e.Scheduled)),
			list(e.Tested), was(len(e.Tested))),
		Spans: []diag.Span{e.Pos},
	}
}

// EmptyScheduleError is an arm of a scheduled variant that is taken for no
// occurrence at all.
//
// That is not an arm selected by nothing but an arm nothing selects, and it
// would let a variant satisfy "two arms at least" while only one of them is ever
// taken (docs/ir/SPEC.md, "An arm may be selected by its position in the
// table"). Where every occurrence really is one alternative's, the layout says
// so with a single alternative and no variant is emitted at all.
type EmptyScheduleError struct {
	// Pos is the arm's entry in the copybook.
	Pos diag.Span

	// Record is the record being resolved.
	Record string

	// Group is the innermost repeating group the variant sits in.
	Group string

	// Redefined is the item the copybook redefines.
	Redefined string

	// Arm is the alternative nothing is scheduled for.
	Arm string
}

// Error implements the error interface.
func (e *EmptyScheduleError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *EmptyScheduleError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s, the arm %s of the variant on %s in the repeating group %s is scheduled for no occurrence, so nothing ever takes it",
			e.Record, e.Arm, e.Redefined, e.Group),
		Spans: []diag.Span{e.Pos},
	}
}

// ScheduleRangeError is an occurrence number that names no entry of the table
// the variant sits in.
//
// The table holds M entries and a schedule names them from one, so a number
// outside 1..M is an entry that is not there — a schedule written for a table of
// four against a copybook that declares three, most often, which is the
// diagnostic that says which of the two files moved.
type ScheduleRangeError struct {
	// Pos is the arm's entry in the copybook.
	Pos diag.Span

	// Record is the record being resolved.
	Record string

	// Group is the innermost repeating group the variant sits in.
	Group string

	// Redefined is the item the copybook redefines.
	Redefined string

	// Arm is the alternative the number was written on.
	Arm string

	// Occurrence is the number, and Maximum the entries the table declares.
	Occurrence int64
	Maximum    int
}

// Error implements the error interface.
func (e *ScheduleRangeError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *ScheduleRangeError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s, the arm %s of the variant on %s is scheduled for occurrence %d, and the repeating group %s holds %s",
			e.Record, e.Arm, e.Redefined, e.Occurrence, e.Group, plural(e.Maximum, "occurrence")),
		Spans: []diag.Span{e.Pos},
	}
}

// ScheduleOverlapError is one occurrence of a table that two arms are scheduled
// for, or that one arm is scheduled for twice.
//
// It is docs/ir/SPEC.md's "When two match, and when none does" at this scope and
// decided from the layout rather than from a file: nothing is evaluated per
// occurrence here, so there is no tie for a consumer to break and no file that
// could make it break one way rather than the other.
type ScheduleOverlapError struct {
	// Pos is the redefined item's entry in the copybook.
	Pos diag.Span

	// Record is the record being resolved.
	Record string

	// Group is the innermost repeating group the variant sits in.
	Group string

	// Redefined is the item the copybook redefines.
	Redefined string

	// Arms are the alternatives scheduled for it, named once each. One name
	// is an arm that wrote the number twice, which is a different mistake
	// from two arms writing it once.
	Arms []string

	// Occurrence is the entry they disagree about.
	Occurrence int64
}

// Error implements the error interface.
func (e *ScheduleOverlapError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *ScheduleOverlapError) Diagnostic() diag.Diagnostic {
	if len(e.Arms) == 1 {
		return diag.Diagnostic{
			Message: fmt.Sprintf(
				"in record %s, the arm %s of the variant on %s in the repeating group %s is scheduled for occurrence %d twice, and an occurrence is taken by one arm",
				e.Record, e.Arms[0], e.Redefined, e.Group, e.Occurrence),
			Spans: []diag.Span{e.Pos},
		}
	}

	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s, occurrence %d of the repeating group %s is scheduled by %s, %s of the variant on %s, and an occurrence is taken by one arm",
			e.Record, e.Occurrence, e.Group, list(e.Arms), plural(len(e.Arms), "arm"), e.Redefined),
		Spans: []diag.Span{e.Pos},
	}
}

// ScheduleCoverageError is an occurrence of a table that no arm of its variant
// is scheduled for.
//
// This is the diagnostic the whole form waits on the copybook for. A schedule is
// checked statically, so a scheduled variant cannot produce the "occurrence no
// arm matched" failure at read time at all: an occurrence left out here is one
// an adopter reads with nothing describing it and nothing to say so, which is
// why it is caught here or never.
type ScheduleCoverageError struct {
	// Pos is the redefined item's entry in the copybook.
	Pos diag.Span

	// Record is the record being resolved.
	Record string

	// Group is the innermost repeating group the variant sits in.
	Group string

	// Redefined is the item the copybook redefines.
	Redefined string

	// Occurrences are the entries no arm is scheduled for, ascending.
	Occurrences []int64

	// Maximum is the entries the table declares.
	Maximum int
}

// Error implements the error interface.
func (e *ScheduleCoverageError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *ScheduleCoverageError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"in record %s, the arms of the variant on %s schedule no alternative for %s %s of the repeating group %s, which holds %s",
			e.Record, e.Redefined,
			unit(len(e.Occurrences), "occurrence"), list(occurrenceNumbers(e.Occurrences)),
			e.Group, plural(e.Maximum, "occurrence")),
		Spans: []diag.Span{e.Pos},
	}
}

// was is the verb a message uses of a list of names, so that one arm is and two
// arms are.
func was(n int) string {
	if n == 1 {
		return "is"
	}

	return "are"
}

// unit is a unit named without its count, for a message that goes on to list the
// values themselves.
func unit(n int, name string) string {
	if n == 1 {
		return name
	}

	return name + "s"
}
