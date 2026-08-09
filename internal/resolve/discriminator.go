// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"slices"

	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// This file is the lowering: a layout's discriminator strategies compiled into
// the predicate nodes of predicate.go, in the two scopes docs/ir/SPEC.md admits
// one in.
//
// A record's strategy becomes the predicate every transition admitting that
// record carries, and an arm's becomes the predicate selecting that arm inside
// one occurrence of a table. The two share the node kind, the closed set and the
// literal resolution, and they part company over where a target may sit — which
// is why both halves are here rather than one of them beside the code that calls
// it. A rule that binds one scope and not the other is a rule an adopter is
// entitled to see stated beside its opposite.
//
// # Two stages, and why the refusals are the later one
//
// A fault about the *target* is reported while the expression is being walked,
// because it needs nothing but the record and its copybook, and a graph built
// over a discriminator nobody could resolve would report a second fault per
// transition carrying it.
//
// A fault about the *strategy* — a record's length, or where a record sits in
// the stream — is reported after the graph is assembled, because what
// docs/ir/SPEC.md asks those diagnostics to name is not in hand before then. A
// record-length discriminator is refused "naming both records", and which other
// record it would have had to be told apart from is a property of the states the
// record appears at. Positional *first* is not refused at all: it lowers into
// the start state, and what is checked is that the record is admitted there and
// nowhere else.

// The strategies that name no field, as docs/layout/SPEC.md's "The strategies
// that are not in the set" spells the two it refuses.
//
// A layout has no way to write either — `layoutmodel` closes the set at three
// and rejects a fourth tag while the file is being read — and they are named
// here all the same, for [resolver.assertResolved]'s reason. docs/ir/SPEC.md
// requires `resolve` to reject them by name, the requirement is on what leaves
// this package rather than on what entered it, and a caller assembling a
// [Sequencing] by hand reaches this package without passing the layout reader at
// all. The diagnostic is what an adopter gets either way.
const (
	// strategyRecordLength is a record told apart by how long it is.
	strategyRecordLength layoutmodel.StrategyKind = "record-length"

	// strategyPositional is a record told apart by where it sits in the
	// stream. Its literal says which end.
	strategyPositional layoutmodel.StrategyKind = "positional"
)

// positionalFirst is the literal that makes a positional strategy the file's
// first record, which is the one reading of it that lowers into something.
const positionalFirst = "first"

// discriminator lowers one record's strategy into the predicate every transition
// admitting that record carries, or nil where it lowers into none.
//
// Three strategies lower into no predicate and only one of the three is a fault
// here. `single-record-type` is the absence of a predicate; positional *first*
// is the start state, which the construction already gives; and a strategy this
// package refuses outright is left for [compiler.checkDiscrimination], which can
// name what the refusal has to name.
func (c *compiler) discriminator(record SequencedRecord) *Predicate {
	strategy := record.Discriminator
	if !strategy.Predicate() {
		return nil
	}

	// docs/layout/SPEC.md, "What the item reference must satisfy": the item is
	// contained in the record the strategy is written on. `layoutmodel` checks
	// the reference's own root and this checks the same thing of a strategy
	// that reached here by another road, because a predicate naming a field of
	// another record is the one shape the whole rule exists to refuse.
	if strategy.Item.Record != record.Name {
		c.faults.Fail(&PredicateTargetError{
			Pos:      layoutSpan(strategy.Item.Pos),
			Copybook: copybookSpan(record, nil),
			Record:   record.Name,
			Item:     strategy.Item,
			Found:    "it is rooted at record " + quote(strategy.Item.Record),
		})

		return nil
	}

	built := c.layoutOf(record)
	if built == nil {
		c.faults.Fail(&PredicateTargetError{
			Pos:      layoutSpan(strategy.Item.Pos),
			Copybook: copybookSpan(record, nil),
			Record:   record.Name,
			Item:     strategy.Item,
			Found:    "its copybook does not lay out",
		})

		return nil
	}

	matched := itemsAt(built.Record, strategy.Item.Path)
	if len(matched) != 1 {
		found := "it names no item of that record"
		if len(matched) > 1 {
			found = "it names " + plural(len(matched), "item") + " of that record"
		}

		c.faults.Fail(&PredicateTargetError{
			Pos:      layoutSpan(strategy.Item.Pos),
			Copybook: copybookSpan(record, nil),
			Record:   record.Name,
			Item:     strategy.Item,
			Found:    found,
		})

		return nil
	}

	target := matched[0]

	// docs/ir/SPEC.md, "Discriminator predicates": the target must not repeat
	// and must not be contained at any depth in a group that repeats, under the
	// rule "A reference names a field, not an occurrence of one" states for
	// every position naming a field (#84). An arm's target is outside this rule
	// and is *required* to sit inside one, which is where a variant is built.
	if group := enclosingTable(target); target.MaxOccurs > 1 || group != nil {
		c.faults.Fail(&PredicateOccurrenceError{
			Pos:      layoutSpan(strategy.Item.Pos),
			Copybook: copybookSpan(record, target.Field),
			Record:   record.Name,
			Item:     itemName(target.Field),
			Group:    groupName(group),
		})

		return nil
	}

	// docs/ir/SPEC.md, "Discriminator predicates", second restriction: the
	// target's position must be constant within the record its transition would
	// admit, so no item ahead of it may carry a repetition whose count is a
	// reference. It is asked under the sliding reading alone, for
	// [resolver.referenceCount]'s reason: on a non-sliding file the same clause
	// is a fixed table and there is no variable term to be behind.
	if c.opts.Reading.Slides() {
		if ahead := variableAhead(built.Items(), target); ahead != nil {
			c.faults.Fail(&PredicatePositionError{
				Pos:      layoutSpan(strategy.Item.Pos),
				Copybook: copybookSpan(record, target.Field),
				Record:   record.Name,
				Item:     itemName(target.Field),
				Behind:   itemName(ahead.Field),
				Count:    itemName(ahead.DependingOn.Field),
			})

			return nil
		}
	}

	values := c.values(record, strategy, target)
	if values == nil {
		return nil
	}

	test := BytesEqual
	if strategy.Kind == layoutmodel.OneOf {
		test = BytesOneOf
	}

	return &Predicate{Target: target.Field, Test: test, Values: values}
}

// values resolves a record discriminator's literals against the item it tests,
// reporting every one that cannot be resolved and returning nil where any could
// not.
//
// Every literal is resolved even after one has failed, so that a `one-of`
// carrying three misspellings is three things to fix rather than one to discover
// on the next run.
func (c *compiler) values(record SequencedRecord, strategy layoutmodel.Strategy, target *copybook.Item) []Value {
	resolver := literalResolver{
		field: target.Field,
		width: target.Length,
		axes:  axesOf(c.opts.Encoding, c.opts.EncodingOverrides, target.Field),
	}

	values := make([]Value, 0, len(strategy.Literals))
	failed := false

	for _, literal := range strategy.Literals {
		value, wrong := resolver.resolve(literal)
		if wrong != "" {
			failed = true

			c.faults.Fail(&PredicateLiteralError{
				Pos:      layoutSpan(literal.Pos),
				Copybook: copybookSpan(record, target.Field),
				Subject:  "the discriminator for record " + record.Name,
				Literal:  literal,
				Item:     itemName(target.Field),
				Found:    wrong,
			})

			continue
		}

		values = append(values, value)
	}

	if failed {
		return nil
	}

	return values
}

// guardValues resolves the literals a `when` compares its register against.
//
// A bytes register holds its source field's bytes as they appear in the record,
// so a guard over one compares bytes and its literals are resolved exactly as a
// predicate's are — padded to the item's width, so that a `when` over a
// `PIC X(4)` flag does not leave a consumer deciding whether `Y` matches `Y `.
// The register is bytes rather than a decoded value for the same reason a
// predicate tests bytes: neither needs to know any COBOL.
//
// A register whose item did not resolve carries no values. The fault is already
// reported against the operator, and a second one about its literals would name
// a line the adopter has one thing to fix on.
func (c *compiler) guardValues(e layoutmodel.Expression, register *Register) []Value {
	if register.Field == nil {
		return nil
	}

	record, known := c.record(e.Item.Record)
	if !known {
		return nil
	}

	built := c.layoutOf(record)
	if built == nil {
		return nil
	}

	matched := itemsAt(built.Record, e.Item.Path)
	if len(matched) != 1 {
		return nil
	}

	target := matched[0]
	resolver := literalResolver{
		field: target.Field,
		width: target.Length,
		axes:  axesOf(c.opts.Encoding, c.opts.EncodingOverrides, target.Field),
	}

	values := make([]Value, 0, len(e.Match.Literals))
	for _, literal := range e.Match.Literals {
		value, wrong := resolver.resolve(literal)
		if wrong != "" {
			c.faults.Fail(&PredicateLiteralError{
				Pos:      layoutSpan(literal.Pos),
				Copybook: copybookSpan(record, target.Field),
				Subject:  "the when over " + e.Item.String(),
				Literal:  literal,
				Item:     itemName(target.Field),
				Found:    wrong,
			})

			continue
		}

		values = append(values, value)
	}

	return values
}

// checkDiscrimination holds the assembled graph to the rules about a
// discriminator that need the graph: the two strategies docs/ir/SPEC.md refuses
// by name, the one it lowers into the start state, and the two shapes a
// transition carrying no predicate is refused in.
func (c *compiler) checkDiscrimination(a *Automaton) {
	for _, record := range c.opts.Records {
		c.refuseStrategy(a, record)
		c.checkFirst(a, record)
		c.checkExtent(a, record)
	}
}

// refuseStrategy reports the strategies that name neither a field nor nothing.
//
// Each is refused where docs/ir/SPEC.md refuses it and in the words that section
// uses, because the generic version — an unknown strategy — sends an adopter
// looking for a spelling mistake in a strategy they wrote deliberately.
func (c *compiler) refuseStrategy(a *Automaton, record SequencedRecord) {
	strategy := record.Discriminator
	switch {
	case strategy.Predicate() || strategy.Kind == layoutmodel.SingleRecordType:
		return

	case strategy.Kind == strategyRecordLength:
		c.faults.Fail(&RefusedStrategyError{
			Pos:      layoutSpan(strategy.Pos),
			Record:   record.Name,
			Strategy: string(strategy.Kind),
			Beside:   c.beside(a, record.Name),
			Found:    "a predicate tests a field's bytes, and a record's length is not one",
		})

	case strategy.Kind == strategyPositional && !positionalFirstStrategy(strategy):
		c.faults.Fail(&RefusedStrategyError{
			Pos:      layoutSpan(strategy.Pos),
			Record:   record.Name,
			Strategy: string(strategy.Kind),
			Beside:   c.beside(a, record.Name),
			Found:    "a writer does not know which record is last",
		})

	case strategy.Kind == strategyPositional:
		return

	default:
		c.faults.Fail(&RefusedStrategyError{
			Pos:      layoutSpan(strategy.Pos),
			Record:   record.Name,
			Strategy: string(strategy.Kind),
			Beside:   c.beside(a, record.Name),
			Found:    "a predicate tests a field's bytes and there is nothing else to select a record on",
		})
	}
}

// positionalFirstStrategy reports whether a positional strategy names the file's
// first record, which is the reading that lowers into the start state.
func positionalFirstStrategy(strategy layoutmodel.Strategy) bool {
	return len(strategy.Literals) == 1 &&
		strategy.Literals[0].Kind == layoutmodel.TextLiteral &&
		strategy.Literals[0].Text == positionalFirst
}

// checkFirst holds a record named as the file's first to being admitted there
// and nowhere else.
//
// docs/ir/SPEC.md's "A predicate always names a field" is the whole of it:
// position is the automaton's shape and not a test, the start state is what the
// automaton knows about position, and a state distinguishes position only while
// it is entered once. The construction already gives a start state no transition
// re-enters, so what is left to check is the other half — that the record the
// layout called first is not also admitted somewhere further in, where being
// first says nothing about it.
func (c *compiler) checkFirst(a *Automaton, record SequencedRecord) {
	if record.Discriminator.Kind != strategyPositional || !positionalFirstStrategy(record.Discriminator) {
		return
	}

	for _, state := range a.States {
		if state == a.Start {
			continue
		}

		for _, transition := range state.Transitions {
			if transition.Record != record.Name {
				continue
			}

			c.faults.Fail(&RefusedStrategyError{
				Pos:      layoutSpan(record.Discriminator.Pos),
				Record:   record.Name,
				Strategy: string(record.Discriminator.Kind),
				Beside:   c.beside(a, record.Name),
				Found: "the file's first record is the one the expression admits first, and this one" +
					" is admitted after another record besides",
			})

			return
		}
	}
}

// checkExtent refuses a transition admitting a record whose extent is zero.
//
// docs/ir/SPEC.md's "A transition may carry no predicate" states it where it
// became necessary: before that section every admitted record held at least one
// field of non-zero width, because a predicate's target had to be one. A
// zero-extent record on a transition that matches everything is a reader that
// emits records forever without advancing its read position, which is the
// unterminating walk "No epsilon transitions" refused at the front door (#80).
//
// It cannot fire against a copybook `cobol-go` laid out, and running it anyway
// is [resolver.assertResolved]'s argument: the only shape that occupies no bytes
// is a group with no elementary item under it, which that reader refuses by
// name, so the requirement is asserted over the result rather than assumed from
// the input. A dialect that grew a zero-width item, or a caller assembling a
// [SequencedRecord] by hand, would otherwise reach a consumer unchecked.
func (c *compiler) checkExtent(a *Automaton, record SequencedRecord) {
	built := c.layoutOf(record)
	if built == nil || built.Record.Total() > 0 {
		return
	}

	if !admitted(a, record.Name) {
		return
	}

	c.faults.Fail(&ZeroExtentError{
		Pos:      layoutSpan(record.Discriminator.Pos),
		Copybook: copybookSpan(record, record.Item),
		Record:   record.Name,
		Item:     itemName(record.Item),
	})
}

// admitted reports whether any transition of the graph admits the record.
func admitted(a *Automaton, record string) bool {
	for _, state := range a.States {
		for _, transition := range state.Transitions {
			if transition.Record == record {
				return true
			}
		}
	}

	return false
}

// beside is the records a record has to be told apart from: those admitted by a
// transition leaving a state it is admitted at, whose guards can hold at the
// same time as its own.
//
// Eligibility at the same time and not merely leaving the same state, which is
// the pairing every rule about telling two records apart is over
// (docs/ir/SPEC.md, "When two match, and when none does"). A record with nothing
// beside it is one nothing has to be told from, and the diagnostics that call
// this say so rather than naming an ambiguity that does not exist.
func (c *compiler) beside(a *Automaton, record string) []string {
	var found []string

	for _, state := range a.States {
		for _, first := range state.Transitions {
			if first.Record != record {
				continue
			}

			for _, second := range state.Transitions {
				if second == first || second.Record == record {
					continue
				}

				if !satisfiable(mergeGuards(first.Guards, second.Guards)) {
					continue
				}

				if !slices.Contains(found, second.Record) {
					found = append(found, second.Record)
				}
			}
		}
	}

	return found
}

// nameable reports whether a record offers a field a predicate may name: an
// elementary item that neither repeats nor sits at any depth inside a group that
// does.
//
// A record every field of which is inside a table cannot be discriminated, and
// docs/ir/SPEC.md's "A record with nothing outside a table to name" is why the
// rule is not softened for it: the first entry of a table is exactly where a
// discriminator looks right and reads a value belonging to the data rather than
// to the record's identity.
func nameable(built *copybook.Layout) bool {
	if built == nil {
		return false
	}

	for _, item := range built.Items() {
		if item.Field.Kind == copybook.KindGroup {
			continue
		}

		if item.MaxOccurs > 1 || enclosingTable(item) != nil {
			continue
		}

		return true
	}

	return false
}

// stretchOf is the run of a record's bytes a predicate tests.
//
// The offsets are the record's own static layout, which is what the target's
// position being constant makes safe: no item ahead of it carries a repetition
// whose count is a reference, so the run is the same run in every record of that
// type, and this is the sum docs/ir/SPEC.md's "Ordering and width" states rather
// than a number any node carries.
func (c *compiler) stretchOf(record string, predicate *Predicate) (stretch, bool) {
	if predicate == nil {
		return stretch{}, false
	}

	known, ok := c.record(record)
	if !ok {
		return stretch{}, false
	}

	built := c.layoutOf(known)
	if built == nil {
		return stretch{}, false
	}

	for _, item := range built.Items() {
		if item.Field == predicate.Target {
			return stretch{at: item.Offset, width: item.Length}, true
		}
	}

	return stretch{}, false
}

// axesOf is the four encoding axes governing one field's bytes: the profile with
// every override reaching the field applied over it, outermost first.
//
// The same fold [resolver.encoding] runs during the record walk, written as a
// function over an item's ancestry so that the automaton — which walks no record
// — reaches the same answer. Two readings of what an override applies to would
// be two answers about the bytes a literal resolves to.
func axesOf(profile layoutmodel.Axes, overrides []EncodingOverride, field *copybook.Field) layoutmodel.Axes {
	var ancestry []*copybook.Field
	for at := field; at != nil; at = at.Parent {
		ancestry = append(ancestry, at)
	}

	slices.Reverse(ancestry)

	axes := profile
	for _, at := range ancestry {
		if override := overrideFor(overrides, at); override != nil {
			axes = override.Axes.Over(axes)
		}
	}

	return axes
}

// overrideFor is the layout's override naming field, or nil where it wrote none.
//
// The first entry naming an item wins. `layoutmodel` already refuses a layout
// that overrides one item twice, so a second entry is a caller that assembled
// one by hand, and reporting it as the adopter's fault would name a line the
// adopter wrote correctly.
func overrideFor(overrides []EncodingOverride, field *copybook.Field) *EncodingOverride {
	for i := range overrides {
		if overrides[i].Item == field {
			return &overrides[i]
		}
	}

	return nil
}

// armPredicate lowers one arm's strategy into the predicate selecting it.
//
// Three of the rules a transition's target is held to do not reach here and one
// of them is reversed, and every difference comes from where the predicate is
// evaluated: inside one occurrence of a group, with the record already admitted
// (docs/ir/SPEC.md, "A predicate on an arm reads one occurrence", #90).
//
// The target **must** sit at any depth inside the innermost group that repeats
// and contains the variant — the group one occurrence of which the arm is being
// chosen for — which is the reverse of the rule refusing a transition's target
// inside a repeating group. That rule refuses a reference with no occurrence to
// be read in, and an arm's predicate is evaluated in the occurrence being
// walked, so nothing is guessed.
//
// It **must not** sit inside an arm that does not also contain the variant. A
// target outside the occurrence has the same bytes in every occurrence and so
// selects the same arm in all of them, which is a choice made once per record; a
// target inside a sibling arm has bytes only where that arm was selected.
//
// And its position needs no constancy at all: every byte of the occurrence is
// locatable before an arm is chosen, because the arms are of one extent and the
// record has already been admitted.
func (r *resolver) armPredicate(c cluster, table *copybook.Item, alternative Alternative) (*Predicate, *copybook.Item) {
	redefined := c.members[0]
	strategy := alternative.Predicate

	fail := func(found string) (*Predicate, *copybook.Item) {
		r.faults.Fail(&ArmTargetScopeError{
			Pos:         layoutSpan(strategy.Item.Pos),
			Copybook:    r.span(redefined.Field),
			Record:      r.record.Name,
			Group:       groupName(table),
			Redefined:   itemName(redefined.Field),
			Alternative: alternative.Name,
			Item:        strategy.Item,
			Found:       found,
		})

		return nil, nil
	}

	matched := itemsAt(r.layout.Record, strategy.Item.Path)
	if len(matched) != 1 {
		if len(matched) > 1 {
			return fail("it names " + plural(len(matched), "item") + " of that record")
		}

		return fail("it names no item of that record")
	}

	target := matched[0]

	if !within(target, table) {
		return fail("it sits outside " + groupName(table) + ", so it holds the same bytes in every entry")
	}

	for _, member := range c.members {
		if within(target, member) || target == member {
			return fail("it sits inside " + itemName(member.Field) +
				", which is an alternative of the variant and has bytes only where it was selected")
		}
	}

	resolver := literalResolver{
		field: target.Field,
		width: target.Length,
		axes:  r.axesOf(target),
	}

	values := make([]Value, 0, len(strategy.Literals))
	failed := false

	for _, literal := range strategy.Literals {
		value, wrong := resolver.resolve(literal)
		if wrong != "" {
			failed = true

			r.faults.Fail(&PredicateLiteralError{
				Pos:      layoutSpan(literal.Pos),
				Copybook: r.span(target.Field),
				Subject:  "the arm " + alternative.Name + " of " + itemName(redefined.Field),
				Literal:  literal,
				Item:     itemName(target.Field),
				Found:    wrong,
			})

			continue
		}

		values = append(values, value)
	}

	if failed {
		return nil, nil
	}

	test := BytesEqual
	if strategy.Kind == layoutmodel.OneOf {
		test = BytesOneOf
	}

	return &Predicate{Target: target.Field, Test: test, Values: values}, target
}

// checkArmOverlap refuses two arms of one variant that one occurrence can
// satisfy both of.
//
// docs/ir/SPEC.md's "When two match, and when none does" at this scope and for
// its reasons, with one difference: there are no guards on an arm to exempt a
// pair, so every pair is inside the rule. The test is the transitions' — whether
// one input can satisfy both — asked of one occurrence rather than of one
// record.
func (r *resolver) checkArmOverlap(c cluster, table *copybook.Item, arms []Arm, targets []*copybook.Item, spec *Redefine) {
	redefined := c.members[0]

	for i := range arms {
		for j := i + 1; j < len(arms); j++ {
			over := stretch{at: targets[i].Offset, width: targets[i].Length}
			against := stretch{at: targets[j].Offset, width: targets[j].Length}

			if !overlap(arms[i].Predicate, over, arms[j].Predicate, against) {
				continue
			}

			r.faults.Fail(&ArmOverlapError{
				Pos:       layoutSpan(armStrategy(spec, arms[j].Alternative).Pos),
				First:     layoutSpan(armStrategy(spec, arms[i].Alternative).Pos),
				Record:    r.record.Name,
				Group:     groupName(table),
				Redefined: itemName(redefined.Field),
				Arms:      [2]string{arms[i].Alternative, arms[j].Alternative},
				Value:     shared(arms[i].Predicate, arms[j].Predicate),
			})
		}
	}
}

// armStrategy is the strategy the layout wrote for one alternative, which is
// what a diagnostic about that arm points at.
func armStrategy(spec *Redefine, alternative string) layoutmodel.Strategy {
	for _, candidate := range spec.Alternatives {
		if candidate.Name == alternative {
			return candidate.Predicate
		}
	}

	return layoutmodel.Strategy{}
}

// shared is a value two overlapping predicates have in common, or the first of
// the earlier one where they overlap by reading different runs of bytes.
func shared(first, second *Predicate) Value {
	for _, one := range first.Values {
		for _, other := range second.Values {
			if one.Identity() == other.Identity() {
				return one
			}
		}
	}

	if len(first.Values) > 0 {
		return first.Values[0]
	}

	return Value{}
}

// within reports whether item is contained, at any depth, in group.
func within(item, group *copybook.Item) bool {
	if group == nil {
		return false
	}

	for parent := item.Parent; parent != nil; parent = parent.Parent {
		if parent == group {
			return true
		}
	}

	return false
}

// axesOf is the four encoding axes governing one item's bytes, as the record
// walk would compute them.
func (r *resolver) axesOf(item *copybook.Item) layoutmodel.Axes {
	return axesOf(r.opts.Encoding, r.opts.EncodingOverrides, item.Field)
}

// quote renders a name the way a diagnostic quotes one.
func quote(name string) string { return "`" + name + "`" }
