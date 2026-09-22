// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"slices"
	"strconv"

	"github.com/Zaba505/cobol-go/copybook"
)

// Schedule is the occurrences of a repeating group that one alternative is
// taken for: the selector of an arm chosen by its position in the table rather
// than by the bytes in front of it.
//
// docs/ir/SPEC.md's "An arm may be selected by its position in the table" is the
// whole of it, and the file that asks for it is the table whose entries carry
// roles rather than types — a count says how many entries arrived, entry one is
// the home address, entry two the work address, and no byte of an entry says
// which it is. There is nothing for a predicate to test there, so the position
// is the selector.
//
// A schedule resolves to no node and dereferences nothing. The numbers are
// positions in the table the arm is being chosen for, not subscripts on a name,
// which is why docs/ir/SPEC.md's "A reference names a field, not an occurrence
// of one" is not widened by one.
type Schedule struct {
	// Occurrences are the entries of the table this arm is taken for, counted
	// from one.
	//
	// int64 rather than int because a number in a layout is one, and the
	// diagnostic for an occurrence outside 1..M has to name what the adopter
	// wrote rather than what narrowing it to a machine word made of it.
	Occurrences []int64
}

// byPosition reports whether a redefine's alternatives are selected by the
// position of an occurrence rather than by its bytes.
//
// One arm carrying a schedule is enough to answer, because every arm of one
// variant carries the same kind of selector and [resolver.checkSelectors] is
// what holds them to it. Asking this way rather than asking every arm is what
// lets a mixed variant be reported as a mixed variant and then resolved no
// further, instead of being read as one kind and faulted arm by arm for not
// being the other.
func byPosition(spec *Redefine) bool {
	return slices.ContainsFunc(spec.Alternatives, func(a Alternative) bool { return a.Schedule != nil })
}

// checkSelectors holds every arm of one variant to the same kind of selector.
//
// docs/ir/SPEC.md's "One kind of selector per variant" is the rule, and the
// reason it is a rule rather than a pair of checks: exhaustiveness and overlap
// are different questions for the two kinds — one is decided statically over
// 1..M and the other is a property of bytes — so a mixed variant would need both
// checks and a third rule saying which wins where a predicate matches an
// occurrence a schedule also covers.
//
// An arm carrying neither selector is not mixing anything and is not this
// fault's: it is [ArmPredicateError], the arm selected by nothing.
func (r *resolver) checkSelectors(c cluster, table *copybook.Item, spec *Redefine) bool {
	var scheduled, tested []string

	for _, alternative := range spec.Alternatives {
		switch {
		case alternative.Schedule != nil:
			scheduled = append(scheduled, alternative.Name)
		case alternative.Predicate.Predicate():
			tested = append(tested, alternative.Name)
		}
	}

	if len(scheduled) == 0 || len(tested) == 0 {
		return true
	}

	r.faults.Fail(&MixedSelectorError{
		Pos:       r.span(c.members[0].Field),
		Record:    r.record.Name,
		Group:     groupName(table),
		Redefined: itemName(c.members[0].Field),
		Scheduled: scheduled,
		Tested:    tested,
	})

	return false
}

// checkCoverage holds a scheduled variant's arms to covering 1..M exactly once,
// for M the declared maximum number of occurrences of the table the variant sits
// in.
//
// This is the check the whole form waits on the copybook for, because M is the
// copybook's. And it is the check that has to be made here or nowhere: a
// schedule is read statically, so a scheduled variant cannot produce the
// "occurrence no arm matched" failure at read time at all, and a schedule with a
// hole in it would be a table an adopter reads with an entry silently missing
// rather than a file a consumer refuses.
//
// M is the *declared* maximum under both readings of an OCCURS DEPENDING ON, so
// the answer is the same whichever one the layout states — which is what keeps
// the mechanism available under both (#346). Under a sliding reading the
// occurrences beyond the count are not in the record and are not read; their
// arms are simply not taken, and that is not a failure either.
//
// Four diagnostics rather than one, because they send an adopter to four
// different places: the entry they forgot to describe, the two descriptions they
// wrote for one entry, the arm nothing ever selects, and the number that names
// no entry of this table at all.
func (r *resolver) checkCoverage(c cluster, table *copybook.Item, spec *Redefine) bool {
	redefined := c.members[0]
	maximum := int64(table.MaxOccurs)
	sound := true

	// Keyed by occurrence and holding the arms that scheduled it, because the
	// duplicate is the interesting case and it is the arms that make it
	// readable: an occurrence scheduled by two alternatives is a different
	// mistake from one written twice inside a single arm, and the message says
	// which of the two it is looking at.
	carried := make(map[int64][]string, table.MaxOccurs)

	for _, alternative := range spec.Alternatives {
		if alternative.Schedule == nil || len(alternative.Schedule.Occurrences) == 0 {
			r.faults.Fail(&EmptyScheduleError{
				Pos:       r.span(armField(c, alternative.Name, redefined)),
				Record:    r.record.Name,
				Group:     groupName(table),
				Redefined: itemName(redefined.Field),
				Arm:       alternative.Name,
			})

			sound = false

			continue
		}

		for _, occurrence := range alternative.Schedule.Occurrences {
			if occurrence < 1 || occurrence > maximum {
				r.faults.Fail(&ScheduleRangeError{
					Pos:        r.span(armField(c, alternative.Name, redefined)),
					Record:     r.record.Name,
					Group:      groupName(table),
					Redefined:  itemName(redefined.Field),
					Arm:        alternative.Name,
					Occurrence: occurrence,
					Maximum:    table.MaxOccurs,
				})

				sound = false

				continue
			}

			carried[occurrence] = append(carried[occurrence], alternative.Name)
		}
	}

	var uncovered []int64

	// Walked from one rather than over the map, because a map has no order and
	// two runs of this package must report one layout the same way round.
	for occurrence := int64(1); occurrence <= maximum; occurrence++ {
		switch arms := carried[occurrence]; {
		case len(arms) == 0:
			uncovered = append(uncovered, occurrence)
		case len(arms) > 1:
			r.faults.Fail(&ScheduleOverlapError{
				Pos:        r.span(redefined.Field),
				Record:     r.record.Name,
				Group:      groupName(table),
				Redefined:  itemName(redefined.Field),
				Arms:       distinct(arms),
				Occurrence: occurrence,
			})

			sound = false
		}
	}

	if len(uncovered) > 0 {
		r.faults.Fail(&ScheduleCoverageError{
			Pos:         r.span(redefined.Field),
			Record:      r.record.Name,
			Group:       groupName(table),
			Redefined:   itemName(redefined.Field),
			Occurrences: uncovered,
			Maximum:     table.MaxOccurs,
		})

		sound = false
	}

	return sound
}

// armField is the copybook entry a diagnostic about one arm points at: the item
// the alternative names, and the redefined item where the layout names an
// alternative the copybook does not declare.
//
// The second half is not a fallback for a fault nobody reported.
// [UnknownAlternativeError] is reported for that name by the walk itself, and
// what this keeps is the span: a diagnostic about the schedule beside it still
// opens the copybook at the cluster the adopter is describing rather than at the
// file with no line at all.
func armField(c cluster, alternative string, redefined *copybook.Item) *copybook.Field {
	if member := c.find(alternative); member != nil {
		return member.Field
	}

	return redefined.Field
}

// distinct is the arms that carry one occurrence, named once each and in the
// order they scheduled it.
//
// An arm that wrote one number twice appears once, which is what makes the
// message able to say that a single arm scheduled it twice instead of naming
// that arm as two.
func distinct(arms []string) []string {
	found := make([]string, 0, len(arms))

	for _, arm := range arms {
		if !slices.Contains(found, arm) {
			found = append(found, arm)
		}
	}

	return found
}

// occurrenceNumbers renders occurrence numbers the way a message names them.
func occurrenceNumbers(occurrences []int64) []string {
	rendered := make([]string, 0, len(occurrences))

	for _, occurrence := range occurrences {
		rendered = append(rendered, strconv.FormatInt(occurrence, 10))
	}

	return rendered
}
