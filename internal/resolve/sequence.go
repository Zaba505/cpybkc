// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"slices"
	"strconv"

	"github.com/Zaba505/cobol-go/copybook"
	"github.com/Zaba505/cobol-go/picture"

	"github.com/Zaba505/cpybkc/internal/layout"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// This file is the half of the record automaton that needs a copybook: what a
// value-reading operator's item reference is held to, how the compiled graph is
// assembled out of the positions the expression walk left behind, and the two
// things proved over it before a byte of a file is read.
//
// The split is the one docs/layout/SPEC.md's "Validation and diagnostics"
// draws. `layoutmodel` has the layout and nothing else, so it holds a `times`
// or a `when` to the reference's own spelling and to the record it is rooted at.
// Everything past that — that the path names an item at all, that the item does
// not repeat, that a count decodes to a number, and that the record holding it
// was admitted strictly earlier on every path — needs the copybook and the
// compiled graph, and lands here (#36, #37, #88).

// record is the sequenced record of that name, and whether the layout defines
// one.
func (c *compiler) record(name string) (SequencedRecord, bool) {
	for _, record := range c.opts.Records {
		if record.Name == name {
			return record, true
		}
	}
	return SequencedRecord{}, false
}

// layoutOf is the copybook layout of one record, built once per record.
//
// A layout rather than the field tree, because what is asked of it is whether an
// item repeats or sits inside a group that does, and an OCCURS clause reaches
// [github.com/Zaba505/cobol-go/copybook.Item] rather than the field.
func (c *compiler) layoutOf(record SequencedRecord) *copybook.Layout {
	if already, ok := c.layouts[record.Name]; ok {
		return already
	}

	// A copybook this package cannot lay out is a fault [Resolve] reports
	// against the record itself, with the widths and the clause that made it
	// unresolvable. Reporting it a second time here would name the same
	// copybook line for a second reason, so what happens instead is that the
	// reference cannot be checked and the operator is reported as naming no
	// item — which is what a caller compiling a sequence over a record it
	// never resolved has done.
	built, err := copybook.NewLayout(record.Item, c.opts.Dialect)
	if err != nil {
		built = nil
	}

	c.layouts[record.Name] = built

	return built
}

// field resolves the item reference a `times` or a `when` wrote, and holds it to
// everything docs/ir/SPEC.md requires of a field a binding reads.
//
// It reports and returns nil rather than stopping, so that a layout whose two
// operators are both wrong is told about both.
func (c *compiler) field(e layoutmodel.Expression) *copybook.Field {
	record, known := c.record(e.Item.Record)
	if !known {
		// As in [compiler.name]: `layoutmodel` has already refused a
		// reference rooted at a record no `record` form defines.
		c.faults.Fail(&SequenceItemError{
			Pos:      layoutSpan(e.Item.Pos),
			Operator: string(e.Kind),
			Item:     e.Item,
			Record:   e.Item.Record,
			Found:    "the layout defines no such record",
		})

		return nil
	}

	built := c.layoutOf(record)
	if built == nil {
		c.faults.Fail(&SequenceItemError{
			Pos:      layoutSpan(e.Item.Pos),
			Operator: string(e.Kind),
			Item:     e.Item,
			Record:   record.Name,
			Copybook: copybookSpan(record, nil),
			Found:    "its copybook does not lay out",
		})

		return nil
	}

	matched := itemsAt(built.Record, e.Item.Path)
	if len(matched) != 1 {
		found := "it names no item of that record"
		if len(matched) > 1 {
			found = "it names " + strconv.Itoa(len(matched)) + " items of that record"
		}

		c.faults.Fail(&SequenceItemError{
			Pos:      layoutSpan(e.Item.Pos),
			Operator: string(e.Kind),
			Item:     e.Item,
			Record:   record.Name,
			Copybook: copybookSpan(record, nil),
			Found:    found,
		})

		return nil
	}

	item := matched[0]

	// docs/ir/SPEC.md, "A reference names a field, not an occurrence of one":
	// a binding names a field and nothing carries an occurrence number, so an
	// item with a value per occurrence is a value the automaton cannot name
	// (#76, #84).
	if group := enclosingTable(item); item.MaxOccurs > 1 || group != nil {
		c.faults.Fail(&SequenceOccurrenceError{
			Pos:      layoutSpan(e.Item.Pos),
			Copybook: copybookSpan(record, item.Field),
			Operator: string(e.Kind),
			Record:   record.Name,
			Item:     itemName(item.Field),
			Group:    groupName(group),
		})

		return nil
	}

	// docs/layout/SPEC.md, "Two operators read a value": under `times` the
	// item must be one whose value decodes to an integer. A `when` carries no
	// such rule — a bytes register holds its source field's bytes as they
	// appear, so a guard over one is a byte comparison whatever the item is.
	if e.Kind == layoutmodel.Times && !counts(item.Field) {
		c.faults.Fail(&SequenceCountKindError{
			Pos:      layoutSpan(e.Item.Pos),
			Copybook: copybookSpan(record, item.Field),
			Record:   record.Name,
			Item:     itemName(item.Field),
			Found:    describeCategory(item.Field),
		})

		return nil
	}

	return item.Field
}

// itemsAt is every item the path names, walking from the record's top-level item
// down one name per level, outermost first, with the top-level item's own name
// not repeated.
//
// Every match is returned rather than the first, because duplicate data names
// are legal COBOL and a complete path that still names two items is a reference
// nothing can resolve — which is a fault to report and not a choice to make.
func itemsAt(root *copybook.Item, path []string) []*copybook.Item {
	found := []*copybook.Item{root}

	for _, name := range path {
		var next []*copybook.Item

		for _, item := range found {
			for _, child := range item.Children {
				if !child.Field.Filler && child.Field.Name == name {
					next = append(next, child)
				}
			}
		}

		found = next
	}

	return found
}

// counts reports whether a field's value decodes to an integer, which is what a
// `times` needs of the item it counts.
//
// A category and a scale, and nothing about USAGE: a count may be zoned, packed
// or binary and is a number under all three, which is why the register holds a
// number rather than bytes.
func counts(field *copybook.Field) bool {
	return field != nil &&
		field.Kind != copybook.KindGroup &&
		field.Picture != nil &&
		field.Picture.Category == picture.CategoryNumeric &&
		field.Picture.Scale == 0
}

// describeCategory names what an item is, for a message about one that cannot be
// a count.
func describeCategory(field *copybook.Field) string {
	switch {
	case field == nil:
		return "nothing"
	case field.Kind == copybook.KindGroup:
		return "a group"
	case field.Picture == nil:
		return "an item with no PICTURE"
	case field.Picture.Category != picture.CategoryNumeric:
		return "a " + field.Picture.Category.String() + " item"
	case field.Picture.Scale != 0:
		return "a numeric item with " + strconv.Itoa(field.Picture.Scale) + " digits after the decimal point"
	}
	return "not a count"
}

// assemble turns the positions and the follows the walk left behind into the
// graph a consumer walks.
//
// Three things happen here and each is a rule about the whole automaton rather
// than about one node of the expression. Every transition admitting a record
// gains the bindings of every register read out of that record, since a register
// holds what the most recent binding put in it and the nearest preceding record
// is the one that bound it. Transitions whose own guards contradict each other
// are dropped, because a guard list that no register file satisfies is an edge
// nothing can take. And states nothing reaches are dropped with them.
func (c *compiler) assemble(top facts) *Automaton {
	states := make([]*State, len(c.positions)+1)
	for i := range states {
		states[i] = &State{ID: i}
	}

	start := states[0]
	c.edges(start, top.first, states)

	for at := range c.positions {
		c.edges(states[at+1], c.follow[at], states)
	}

	c.accept(start, top.null, "the file", c.opts.Sequence.Pos)
	for at, appeared := range c.positions {
		c.accept(states[at+1], exitsAt(top.last, at), appeared.record, appeared.pos)
	}

	automaton := &Automaton{Start: start, Registers: c.registers}
	for _, state := range reachable(start, states) {
		automaton.States = append(automaton.States, state)
	}

	return automaton
}

// edges builds one state's outgoing transitions out of the ways into the
// positions that may follow it.
func (c *compiler) edges(from *State, ways []entry, states []*State) {
	for _, way := range ways {
		if !satisfiable(way.guards) {
			// A way in whose guards contradict each other — a counted run
			// restarted where its own counter has reached zero, and nothing
			// between to rebind it — is an edge no register file admits. It
			// is dropped rather than emitted, because a consumer would
			// evaluate it on every record forever and a producer would have
			// to explain it.
			continue
		}

		to := states[way.at+1]
		record := c.positions[way.at]

		from.Transitions = append(from.Transitions, &Transition{
			Record:    record.record,
			Predicate: record.predicate,
			Guards:    way.guards,
			Bindings:  mergeBindings(way.binding, c.bindings(record.record)),
			To:        to,
		})
	}
}

// bindings are the register writes every transition admitting record applies:
// one per register read out of that record.
func (c *compiler) bindings(record string) []Binding {
	var applied []Binding

	for _, register := range c.registers {
		if register.Source.Record != record || register.Field == nil {
			continue
		}

		applied = append(applied, Binding{Register: register, Value: BindField, Field: register.Field})
	}

	return applied
}

// mergeBindings is the bindings of one transition, without two writes of one
// register.
//
// A transition never legitimately carries two: the only way to reach one is a
// run counted by a field of the very record being counted, which takes one off
// the register and rebinds it from the record in the same step. That layout is
// rejected by [compiler.prove], and dropping the second write here is what keeps
// the graph well formed long enough to say so.
func mergeBindings(lists ...[]Binding) []Binding {
	var merged []Binding
	written := make(map[int]bool)

	for _, list := range lists {
		for _, binding := range list {
			if written[binding.Register.ID] {
				continue
			}
			written[binding.Register.ID] = true
			merged = append(merged, binding)
		}
	}

	return merged
}

// accept records whether a state accepts end of input, and under what guards.
//
// A state carries one guard list and a guard list is a conjunction, so a state
// that would accept under either of two conditions is a disjunction the IR has
// nowhere to put. An unconditional way subsumes every other and is taken; two or
// more conditional ways are reported rather than narrowed to one, since choosing
// would either refuse a file the layout admits or admit one it does not.
func (c *compiler) accept(state *State, ways [][]Guard, what string, at layout.Pos) {
	if len(ways) == 0 {
		return
	}

	for _, way := range ways {
		if len(way) == 0 {
			state.Accepts = true

			return
		}
	}

	if len(ways) > 1 {
		c.faults.Fail(&SequenceAcceptanceError{
			Pos:   layoutSpan(at),
			What:  what,
			Ways:  len(ways),
			Guard: renderGuards(ways[0]),
		})
	}

	state.Accepts = true
	state.Acceptance = ways[0]
}

// exitsAt is the ways an expression is complete at one position, as acceptance
// guards.
func exitsAt(exits []exit, at int) [][]Guard {
	var ways [][]Guard

	for _, done := range exits {
		if done.at == at {
			ways = append(ways, done.guards)
		}
	}

	return ways
}

// reachable is the states a walk from the start state reaches, in identifier
// order.
//
// Identifier order rather than walk order because the identifiers already follow
// the expression, and a state list ordered by anything else would move when an
// unrelated part of the expression changed.
func reachable(start *State, states []*State) []*State {
	seen := map[int]bool{start.ID: true}
	queue := []*State{start}

	for len(queue) > 0 {
		state := queue[0]
		queue = queue[1:]

		for _, transition := range state.Transitions {
			if seen[transition.To.ID] {
				continue
			}
			seen[transition.To.ID] = true
			queue = append(queue, transition.To)
		}
	}

	var found []*State
	for _, state := range states {
		if seen[state.ID] {
			found = append(found, state)
		}
	}

	return found
}

// prove holds the automaton to docs/ir/SPEC.md's "A register is read only where
// it has been written": every path from the start state to a read of a register
// passes through a binding of that register on a transition taken **strictly
// earlier** than the reading one (#88).
//
// Strictly earlier is what the proof is over, and it is why the set a state is
// checked against is the one bound on the way *in*. A transition's bindings
// apply at step 7 of the read loop; its guards are evaluated at step 3 and the
// extent of the record it admits is computed at step 4. So a transition that
// binds a register it also reads reads the value the binding was about to
// replace, or nothing at all where no earlier transition bound one, and a
// producer that emitted it would be emitting the reading a layout's author did
// not mean.
// One fault per register and not one per read, because one operator is one
// mistake: a `times` whose count is not in hand puts an unbound read on the
// guard of every transition entering the run and on the acceptance of every
// state leaving it, and reporting each of them would describe one misspelled
// reference four times. The read reported is the most specific one found — the
// transition that binds the register it reads, where there is one — so that the
// message an adopter gets is the one naming the order rather than the path.
func (c *compiler) prove(a *Automaton) {
	bound := c.boundOnEntry(a)
	found := make(map[int]*UnboundRegisterError)

	for _, state := range a.States {
		for _, guard := range state.Acceptance {
			keepUnbound(found, unboundRead(bound[state.ID], guard.Register, state, nil))
		}

		for _, transition := range state.Transitions {
			for _, guard := range transition.Guards {
				keepUnbound(found, unboundRead(bound[state.ID], guard.Register, state, transition))
			}

			// Taking one off a counter reads it too, and reads it at the
			// same moment a guard does: the value written is the value on
			// entry to the state, less one.
			for _, binding := range transition.Bindings {
				if binding.Value == BindLessOne {
					keepUnbound(found, unboundRead(bound[state.ID], binding.Register, state, transition))
				}
			}
		}
	}

	for _, register := range a.Registers {
		if fault, ok := found[register.ID]; ok {
			c.faults.Fail(fault)
		}
	}
}

// keepUnbound records one register's fault, preferring the read that says most
// about it.
func keepUnbound(found map[int]*UnboundRegisterError, fault *UnboundRegisterError) {
	if fault == nil {
		return
	}

	kept, already := found[fault.register]
	if already && (kept.OnAdmitting || !fault.OnAdmitting) {
		return
	}

	found[fault.register] = fault
}

// boundOnEntry is, for each state, the registers bound on **every** path from
// the start state to it, not counting the transition being entered on.
//
// It is the greatest fixed point of the intersection: every state but the start
// begins holding every register and loses the ones some path reaches it without,
// which is what makes a cycle carrying a binding not prove itself.
func (c *compiler) boundOnEntry(a *Automaton) map[int]map[int]bool {
	all := make(map[int]bool, len(a.Registers))
	for _, register := range a.Registers {
		all[register.ID] = true
	}

	bound := make(map[int]map[int]bool, len(a.States))
	for _, state := range a.States {
		if state == a.Start {
			bound[state.ID] = map[int]bool{}

			continue
		}

		bound[state.ID] = maps(all)
	}

	for settled := false; !settled; {
		settled = true

		for _, state := range a.States {
			for _, transition := range state.Transitions {
				if transition.To == a.Start {
					continue
				}

				after := maps(bound[state.ID])
				for _, binding := range transition.Bindings {
					after[binding.Register.ID] = true
				}

				into := bound[transition.To.ID]
				for id := range into {
					if !after[id] {
						delete(into, id)
						settled = false
					}
				}
			}
		}
	}

	return bound
}

// maps copies a set, so that a state's own answer is never the one being
// narrowed.
func maps(of map[int]bool) map[int]bool {
	out := make(map[int]bool, len(of))
	for key, value := range of {
		out[key] = value
	}
	return out
}

// unboundRead is the fault for a register read where the way in did not bind it,
// and nil where the way in did.
//
// Which of two things it says follows from what the reading transition itself
// does. Where that transition binds the register out of the record it admits,
// the layout counted a run by a field of the record being counted — the shape
// docs/ir/SPEC.md's "A count is in hand before the extent it decides" refuses by
// name — and the message says so. Where it does not, the value simply belongs to
// a record some path has not read.
func unboundRead(bound map[int]bool, register *Register, state *State, on *Transition) *UnboundRegisterError {
	if register == nil || bound[register.ID] {
		return nil
	}

	admits := ""
	if on != nil {
		admits = on.Record
	}

	// The reading transition is the binding one exactly where it admits the
	// record the register is read out of. That is asked of the record rather
	// than of the bindings actually on the transition, because the two collide
	// in this one case: a transition that takes one off a counter and rebinds
	// it from the record it admits carries two writes of one register, which
	// [mergeBindings] has already refused to emit.
	return &UnboundRegisterError{
		Pos:         layoutSpan(register.Source.Pos),
		Item:        register.Source,
		Register:    register.Source.Record,
		Reader:      admits,
		State:       state.ID,
		OnAdmitting: on != nil && register.Field != nil && on.Record == register.Source.Record,
		register:    register.ID,
	}
}

// checkAmbiguity holds every state to docs/ir/SPEC.md's "When two match, and
// when none does": two transitions leaving one state must not be eligible
// together and selected by predicates that can both match one record.
//
// Guards narrow which pairs the rule is about. Two transitions whose guards
// cannot hold at the same time are never both eligible, and their predicates may
// overlap freely — which is what makes a counted run expressible at all, since
// the transition reading another detail and the one moving past them are
// selected by the very same test on the very same bytes and only the counter
// separates them.
func (c *compiler) checkAmbiguity(a *Automaton) {
	for _, state := range a.States {
		for i, first := range state.Transitions {
			for _, second := range state.Transitions[i+1:] {
				if !satisfiable(mergeGuards(first.Guards, second.Guards)) {
					continue
				}

				if !c.predicatesOverlap(first.Predicate, second.Predicate) {
					continue
				}

				c.faults.Fail(&SequenceAmbiguityError{
					Pos:       layoutSpan(c.positions[second.To.ID-1].pos),
					First:     layoutSpan(c.positions[first.To.ID-1].pos),
					State:     state.ID,
					Records:   [2]string{first.Record, second.Record},
					Unguarded: first.Predicate == nil || second.Predicate == nil,
					Guards:    len(first.Guards) > 0 || len(second.Guards) > 0,
				})
			}
		}
	}
}

// predicatesOverlap reports whether one record can satisfy both predicates.
//
// The test is whether one input can satisfy both, not whether the two read the
// same bytes, and docs/ir/SPEC.md says where the difference lands: "a state
// offering a transition keyed on a record's first field beside one keyed on its
// tenth is where the narrower reading lets a real ambiguity through, and
// predicates reading different fields at different positions overlap just as
// thoroughly as two reading one". So the question is asked of *positions* and
// not of items. Two predicates over one run of bytes are told apart by their
// literals; two over different runs are not told apart at all, because a record
// can carry an `S` at byte zero and an `X` at byte ten at the same time.
//
// It is a run of bytes rather than the copybook item because two records built
// to different copybooks is the ordinary case, and it is exactly the case
// docs/ir/SPEC.md's "A counted run, as nodes" is written in: a header, a detail
// and a summary each carry their own type code at their own copybook's first
// byte, and a rule keyed on the item would call three type codes at one offset
// an ambiguity.
//
// A transition carrying no predicate matches every record, so it overlaps
// everything (#80).
//
// Two literals are the same value where their spellings are, which is
// [layoutmodel.Literal.Identity]'s reading and is decidable from the layout
// alone. Resolving a literal to the bytes a consumer compares — through the
// item's charset, PICTURE and width — is #37's, and the comparison becomes one
// over bytes when it lands.
func (c *compiler) predicatesOverlap(a, b *layoutmodel.Strategy) bool {
	if a == nil || b == nil {
		return true
	}

	over, ok := c.target(a)
	against, againstOK := c.target(b)

	switch {
	case !ok || !againstOK:
		// A discriminator whose reference does not resolve is #37's fault to
		// report, and an ambiguity decided on one would name a pair of
		// records where the fault is a misspelled item.
		return false
	case over != against:
		return true
	}

	return slices.ContainsFunc(a.Literals, func(one layoutmodel.Literal) bool {
		return slices.ContainsFunc(b.Literals, func(other layoutmodel.Literal) bool {
			return one.Identity() == other.Identity()
		})
	})
}

// stretch is a run of a record's bytes: what a predicate tests.
type stretch struct{ at, width int }

// target is the run of bytes a predicate tests, and whether its reference
// resolves to exactly one item.
//
// The offsets are the ones the record's static layout gives, which is the
// declared maximum of every table in it. That a predicate's target sits at a
// constant position — so that the run is the same run in every record of that
// type — is #37's to hold a discriminator to.
func (c *compiler) target(s *layoutmodel.Strategy) (stretch, bool) {
	record, known := c.record(s.Item.Record)
	if !known {
		return stretch{}, false
	}

	built := c.layoutOf(record)
	if built == nil {
		return stretch{}, false
	}

	matched := itemsAt(built.Record, s.Item.Path)
	if len(matched) != 1 {
		return stretch{}, false
	}

	return stretch{at: matched[0].Offset, width: matched[0].Length}, true
}

// satisfiable reports whether some register file satisfies every guard in the
// list at once.
//
// It is the whole of what keeps overlap decidable, and it stays a question about
// literals and zero because of what a guard is not: a flat conjunction of three
// tests over a fixed set of declared registers, with no arithmetic in it beyond
// taking one off a counter and no comparison of one register against another
// (docs/ir/SPEC.md, "The automaton counts; it does not compute").
//
// One list at a time is enough for both callers. A transition's own guards
// contradicting each other is an edge nothing can take, and two transitions'
// guards contradicting each other is the pair the overlap rule is not about, and
// they are the same question asked of the concatenation.
func satisfiable(guards []Guard) bool {
	byRegister := make(map[int][]Guard)
	for _, guard := range guards {
		byRegister[guard.Register.ID] = append(byRegister[guard.Register.ID], guard)
	}

	for _, over := range byRegister {
		if !holdsTogether(over) {
			return false
		}
	}

	return true
}

// holdsTogether reports whether every guard over one register can hold at once.
//
// The values a register may hold are narrowed test by test: `equals` and
// `one-of` narrow to a set of literals, and `positive` drops the ones that are
// not numbers above zero. A literal this cannot read as a number survives
// `positive`, because a guard over a bytes register never carries one and a
// count literal that will not parse is a fault for whoever wrote it rather than
// a reason to call a state ambiguous.
func holdsTogether(guards []Guard) bool {
	var narrowed []layoutmodel.Literal
	bounded := false

	for _, guard := range guards {
		if guard.Test == GuardPositive {
			continue
		}

		if !bounded {
			narrowed, bounded = guard.Literals, true

			continue
		}

		narrowed = slices.DeleteFunc(slices.Clone(narrowed), func(one layoutmodel.Literal) bool {
			return !slices.ContainsFunc(guard.Literals, func(other layoutmodel.Literal) bool {
				return one.Identity() == other.Identity()
			})
		})
	}

	if !slices.ContainsFunc(guards, func(g Guard) bool { return g.Test == GuardPositive }) {
		return !bounded || len(narrowed) > 0
	}

	if !bounded {
		return true
	}

	return slices.ContainsFunc(narrowed, above(0))
}

// above is a literal that reads as a number greater than n.
func above(n int) func(layoutmodel.Literal) bool {
	return func(literal layoutmodel.Literal) bool {
		if literal.Kind != layoutmodel.NumberLiteral {
			return true
		}

		value, err := strconv.Atoi(literal.Number)

		return err != nil || value > n
	}
}
