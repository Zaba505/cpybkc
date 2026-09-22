// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"slices"

	"github.com/Zaba505/cpybkc/internal/layout"
)

// ScheduledVariant is one `schedule-variant` form: a redefine inside a repeating
// group whose entries carry roles rather than types, and the occurrences each
// alternative is taken for.
//
// docs/layout/SPEC.md's "A schedule for a redefine chosen by position" is the
// whole of it. It is the second of the two ways a variant is settled, and the
// one for a table no byte of whose entries says which alternative an entry is:
// entry one is the home address, entry two the work address, and the position
// decides. That is why there is no predicate anywhere under it — a strategy here
// would be a test on bytes that decide nothing.
type ScheduledVariant struct {
	// Pos is the `schedule-variant` form.
	Pos layout.Pos

	// Variant is the item the copybook redefines — the first alternative, the
	// one every `REDEFINES` of it names.
	Variant ItemRef

	// Arms are the alternatives, in the order the layout writes them, which is
	// ascending by first occurrence. There are always at least two on a value
	// handed back, no two name one alternative, and no occurrence is named
	// twice across all of them.
	Arms []ScheduledArm
}

// ScheduledArm is one alternative of a scheduled variant, and the occurrences it
// is taken for.
type ScheduledArm struct {
	// Pos is the `arm` form.
	Pos layout.Pos

	// Alternative is the name the copybook gives this alternative. That the
	// name is one the copybook declares at the variant's position is `resolve`'s
	// (#31, #35).
	Alternative string

	// Occurrences are the entries of the table this alternative is taken for,
	// counted from one and in strictly ascending order. There is always at
	// least one on a value handed back: an arm scheduled for nothing is an arm
	// nothing selects.
	Occurrences []Occurrence
}

// Occurrence is one entry of a repeating group, named by its position.
//
// It carries the position it was written at as well as its value, because a
// diagnostic about a schedule points at the number rather than at the arm: an
// adopter editing one of six occurrences on a line needs the column.
type Occurrence struct {
	// Pos is the number.
	Pos layout.Pos

	// Value is which entry it is, counted from one.
	Value int64
}

// scheduledAt is where an occurrence was first scheduled, and by which arm.
//
// The arm is kept beside the position because the two arms are what makes the
// duplicate readable: an occurrence scheduled by two alternatives is a different
// mistake from one written twice inside a single arm, and the message says which
// of the two it is looking at.
type scheduledAt struct {
	pos layout.Pos
	arm string
}

// schedule reads one `schedule-variant` form.
//
// It makes the checks docs/layout/SPEC.md assigns the layout reader and no
// others. Everything about coverage — that the schedule reaches every occurrence
// the table can hold, exactly once, from one to the repetition's declared
// maximum — needs the copybook and is `resolve`'s, and the check this form exists
// to make possible is that one.
func (r *discriminationReader) schedule(into *Discrimination, form layout.Form) {
	if len(form.Elements) == 0 {
		r.Fail(&ScheduleFormError{Pos: form.Pos, Found: "a variant schedule naming no item at all"})

		return
	}

	item, err := readItemRef(form.Elements[0])
	if err != nil {
		r.Fail(err)

		return
	}

	schedule := ScheduledVariant{Pos: form.Pos, Variant: item}

	r.variantItem(form, item)

	// The occurrences are counted over the whole variant rather than over each
	// arm, because SPEC.md's rule is that no occurrence is named twice by one
	// variant "whether by two arms or twice within one arm". One map is what
	// makes those the same diagnostic.
	scheduled := make(map[int64]scheduledAt)

	for _, element := range form.Elements[1:] {
		arm, ok := r.scheduledArm(element, item, scheduled)
		if !ok {
			continue
		}

		if first, already := scheduledArmNamed(schedule.Arms, arm.Alternative); already {
			r.Fail(&DuplicateArmError{Pos: arm.Pos, First: first, Variant: item, Alternative: arm.Alternative})

			continue
		}

		r.ascending(schedule.Arms, arm, item)

		schedule.Arms = append(schedule.Arms, arm)
	}

	// The count is the arms the layout wrote, not the ones that survived being
	// read, for [discriminationReader.variant]'s reason (#342): an arm refused
	// for a reason of its own has been reported against that arm already, and a
	// count derived from the refusals says the variant carries fewer arms than
	// the adopter can see in the file.
	written := len(form.Elements) - 1
	if written < 2 {
		r.Fail(&ScheduleArmCountError{Pos: form.Pos, Variant: item, Count: written})

		return
	}

	// A schedule an arm of which was refused is not carried into the model, so
	// that [ScheduledVariant.Arms]' own statement — that the arms are the ones
	// the layout writes — holds of every value.
	if len(schedule.Arms) != written {
		return
	}

	into.Schedules = append(into.Schedules, schedule)
}

// scheduledArm reads one `(arm <name> <occurrence> …)`.
//
// It is the second sort of `arm`, and not the one
// [discriminationReader.arm] reads: there is no predicate on it, because a
// scheduled arm tests nothing.
func (r *discriminationReader) scheduledArm(
	node layout.Node,
	variant ItemRef,
	scheduled map[int64]scheduledAt,
) (ScheduledArm, bool) {
	form, ok := node.(layout.Form)
	if !ok {
		r.Fail(&ScheduledArmFormError{Pos: node.Position(), Found: describe(node)})

		return ScheduledArm{}, false
	}

	if form.Tag != tagArm {
		r.Fail(&ScheduledArmFormError{Pos: form.TagPos, Found: "form " + quote(form.Tag)})

		return ScheduledArm{}, false
	}

	if len(form.Elements) == 0 {
		r.Fail(&ScheduledArmFormError{Pos: form.Pos, Found: "no value"})

		return ScheduledArm{}, false
	}

	name, ok := form.Elements[0].(layout.Symbol)
	if !ok {
		r.Fail(&ScheduledArmFormError{Pos: form.Elements[0].Position(), Found: describe(form.Elements[0])})

		return ScheduledArm{}, false
	}

	if len(form.Elements) == 1 {
		r.Fail(&EmptyScheduledArmError{Pos: form.Pos, Variant: variant, Alternative: name.Value})

		return ScheduledArm{}, false
	}

	arm := ScheduledArm{Pos: form.Pos, Alternative: name.Value}

	// Every occurrence is read whatever the ones before it said, for the reason
	// every reader here collects: a schedule written out of order is written out
	// of order in several places at once.
	sound := true

	for _, element := range form.Elements[1:] {
		occurrence, ok := r.occurrence(element, variant, name.Value)
		if !ok {
			sound = false

			continue
		}

		if !r.occurrenceFollows(arm, occurrence, variant) || !r.occurrenceFree(scheduled, occurrence, arm, variant) {
			sound = false

			continue
		}

		scheduled[occurrence.Value] = scheduledAt{pos: occurrence.Pos, arm: name.Value}
		arm.Occurrences = append(arm.Occurrences, occurrence)
	}

	if !sound {
		return ScheduledArm{}, false
	}

	return arm, true
}

// occurrence reads one entry position.
//
// A number with a fraction is refused as a shape rather than as a value, the way
// a framing size is: [describe] would call it "a number", which is what the
// position takes.
func (r *discriminationReader) occurrence(node layout.Node, variant ItemRef, alternative string) (Occurrence, bool) {
	switch value := node.(type) {
	case layout.Int:
		if value.Value <= 0 {
			r.Fail(&OccurrenceValueError{
				Pos:         value.Pos,
				Variant:     variant,
				Alternative: alternative,
				Occurrence:  value.Value,
			})

			return Occurrence{}, false
		}

		return Occurrence{Pos: value.Pos, Value: value.Value}, true
	case layout.Float:
		r.Fail(&ScheduledArmFormError{Pos: value.Pos, Found: "a number with a fraction"})
	default:
		r.Fail(&ScheduledArmFormError{Pos: node.Position(), Found: describe(node)})
	}

	return Occurrence{}, false
}

// occurrenceFollows reports an occurrence written before one already on this
// arm.
//
// Equal is not this fault. An arm's occurrences are *strictly* ascending, so an
// occurrence equal to the last one is a duplicate — which is what makes a
// duplicate inside an arm unambiguously a diagnostic rather than an ordering
// that happens to hold (docs/layout/SPEC.md, "A schedule for a redefine chosen
// by position").
func (r *discriminationReader) occurrenceFollows(arm ScheduledArm, occurrence Occurrence, variant ItemRef) bool {
	last := len(arm.Occurrences)
	if last == 0 || occurrence.Value >= arm.Occurrences[last-1].Value {
		return true
	}

	r.Fail(&OccurrenceOrderError{
		Pos:         occurrence.Pos,
		First:       arm.Occurrences[last-1].Pos,
		Variant:     variant,
		Alternative: arm.Alternative,
		Occurrences: [2]int64{arm.Occurrences[last-1].Value, occurrence.Value},
	})

	return false
}

// occurrenceFree reports an occurrence this variant has already been scheduled
// for, by this arm or by an earlier one.
func (r *discriminationReader) occurrenceFree(
	scheduled map[int64]scheduledAt,
	occurrence Occurrence,
	arm ScheduledArm,
	variant ItemRef,
) bool {
	first, already := scheduled[occurrence.Value]
	if !already {
		return true
	}

	r.Fail(&DuplicateOccurrenceError{
		Pos:        occurrence.Pos,
		First:      first.pos,
		Variant:    variant,
		Arms:       [2]string{first.arm, arm.Alternative},
		Occurrence: occurrence.Value,
	})

	return false
}

// ascending reports an arm written before the one above it in the schedule.
//
// Arms are written in ascending order of their first occurrence, which is what
// makes two layouts describing one file one text. Equal first occurrences cannot
// reach here: the second of them is a duplicate, and the arm carrying it was
// refused.
func (r *discriminationReader) ascending(read []ScheduledArm, arm ScheduledArm, variant ItemRef) {
	if len(read) == 0 || len(arm.Occurrences) == 0 {
		return
	}

	previous := read[len(read)-1]
	if len(previous.Occurrences) == 0 || arm.Occurrences[0].Value > previous.Occurrences[0].Value {
		return
	}

	r.Fail(&ScheduleOrderError{
		Pos:         arm.Occurrences[0].Pos,
		First:       previous.Occurrences[0].Pos,
		Variant:     variant,
		Arms:        [2]string{previous.Alternative, arm.Alternative},
		Occurrences: [2]int64{previous.Occurrences[0].Value, arm.Occurrences[0].Value},
	})
}

// scheduledArmNamed reports where an alternative was first named, among the arms
// already read.
func scheduledArmNamed(read []ScheduledArm, alternative string) (layout.Pos, bool) {
	index := slices.IndexFunc(read, func(arm ScheduledArm) bool { return arm.Alternative == alternative })
	if index < 0 {
		return layout.Pos{}, false
	}

	return read[index].Pos, true
}
