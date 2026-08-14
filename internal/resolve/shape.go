// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import "github.com/Zaba505/cobol-go/copybook"

// Alternation is one run of a record's bytes that a REDEFINES describes more
// than one way.
//
// It is the unit `REDEFINES` is read in: an item, every description of it, and
// the one question that decides which resolution the clause takes — whether the
// run stands inside a group that repeats (docs/ir/SPEC.md, "Members never
// overlap, and `REDEFINES` is resolved away").
type Alternation struct {
	// Item is the redefined item: the first alternative, the one every
	// REDEFINES of it names.
	Item *copybook.Field

	// Alternatives are the descriptions of the run, the redefined item first
	// and each REDEFINES of it after it in declaration order. There are always
	// two or more — a run described once is not an alternation.
	Alternatives []*copybook.Field

	// InTable reports the run standing inside a group that repeats, which is
	// what makes it a variant chosen once per occurrence rather than one
	// record type per alternative.
	InTable bool
}

// Shape is what a copybook record decides on its own, before any layout has
// said anything about it.
//
// It is the half of a record a layout cannot state and does not need to be
// told: which runs of bytes carry more than one description, which of those are
// chosen per record and which per occurrence, how many record types the first
// kind multiply out to, and whether the record's length depends on its own
// data. A layout states the other half — what selects each of them — and
// [Resolve] is where the two meet.
type Shape struct {
	// Alternations are the record's runs carrying more than one description,
	// in containment order: outermost and earliest first.
	Alternations []Alternation

	// Combinations are the record types the alternations outside a repeating
	// group multiply out to, one entry per record type. Each entry names the
	// alternative chosen at each such run it passes through, in containment
	// order: outermost and earliest first, which is the order [Alternations] is
	// in.
	//
	// A record whose copybook writes no such run has exactly one entry, and
	// that entry is empty: one record type, chosen at nothing.
	//
	// **An entry is not positionally aligned with [Alternations], and two
	// entries need not be the same length.** A run nested inside one
	// alternative of another run only exists where that alternative was chosen,
	// so a combination that took a sibling passes through fewer runs and names
	// fewer alternatives. Reading a choice back to the run it was made at is
	// therefore a walk of the copybook and not an index into this slice; what
	// this promises is that the names are in containment order, which is what
	// docs/cli/SPEC.md, "How a combination's record name is chosen", builds a
	// name out of.
	Combinations [][]*copybook.Field

	// Tables are the items carrying an OCCURS DEPENDING ON, in source order.
	// It is empty for a record whose length is a constant.
	Tables []*copybook.Field
}

// Describe reads what record decides on its own: [Shape].
//
// It exists so that a caller with no layout in hand — `cpybkc init`, which
// derives a layout scaffold from copybooks and has none by construction
// (docs/cli/SPEC.md, "`init` scaffolds a layout from copybooks") — reads a
// copybook's REDEFINES clauses through the same rules [Resolve] applies rather
// than through a second reading of its own. The primitives are shared outright:
// the clustering of an item with the items redefining it, and the one question
// that decides which resolution a clause takes, are the functions [Resolve]
// calls. Two readings of one copybook is the failure docs/cli/SPEC.md, "Why
// this is a subcommand and not a script over the published schema", refuses a
// script outside the tool for, and it is no more acceptable inside it.
//
// What it does *not* do is resolve. There is no encoding profile, no framing,
// no reading of an OCCURS DEPENDING ON and no statement of what selects an
// alternative, because a scaffold is written before any of those exist — so no
// width is computed beyond the ones `cobol-go` needs to lay the record out, and
// none of the faults [Resolve] raises about a layout can be raised here. A
// copybook a layout has not been written for yet is not a copybook with a fault
// in it.
//
// The dialect is the caller's for [Resolve]'s reason: what a REDEFINES longer
// than what it redefines means is a compiler's answer and not this package's.
//
// It reports whatever `cobol-go` says is wrong with the record — a PICTURE it
// cannot read, a level sequence that does not nest, a REDEFINES the dialect
// refuses — and nothing of its own.
func Describe(record *copybook.Field, dialect copybook.Dialect) (Shape, error) {
	if record == nil {
		return Shape{}, ErrNilRecord
	}

	laid, err := copybook.NewLayout(record, dialect)
	if err != nil {
		return Shape{}, err
	}

	shape := Shape{}

	for _, table := range tables(laid.Items()) {
		shape.Tables = append(shape.Tables, table.Field)
	}

	shape.Alternations, shape.Combinations = describe(laid.Record)

	return shape, nil
}

// describe walks one item, returning the alternations under it and the
// combinations of the choices they admit.
//
// It mirrors the shape of [resolver.group] and [resolver.cluster], over the same
// clusters and the same [inTable] question, with everything those two compute
// about *nodes* left out — the encoding each member is resolved under, the slack
// the alternatives leave, the arms a variant becomes. What is left is the
// arithmetic: a cluster of one contributes its members' own choices, a cluster
// inside a repeating group contributes none because the choice is not a record
// type's, and any other cluster multiplies what came before it by its
// alternatives.
//
// The combination lists are in containment order — outermost and earliest first
// — which is the order [Resolve] documents and the order docs/cli/SPEC.md, "How
// a combination's record name is chosen", builds a name in.
func describe(item *copybook.Item) ([]Alternation, [][]*copybook.Field) {
	var alternations []Alternation

	// One combination, chosen at nothing, is what an item with no alternation
	// under it resolves to. It is the identity the products below are built
	// on, and it is why an elementary item needs no case of its own.
	combinations := [][]*copybook.Field{nil}

	for _, c := range clustersOf(item) {
		var choices [][]*copybook.Field

		switch {
		case len(c.members) == 1:
			nested, under := describe(c.members[0])

			alternations = append(alternations, nested...)
			choices = under

		case inTable(c.members[0]):
			// A redefine inside a repeating group is chosen once per
			// occurrence, so it multiplies nothing: it is one variant in
			// every record type rather than a record type per alternative.
			// Its members are still walked, because a redefine nested under
			// one is a variant of its own that a layout has to state.
			alternations = append(alternations, alternationOf(c, true))

			for _, member := range c.members {
				nested, _ := describe(member)

				alternations = append(alternations, nested...)
			}

			choices = [][]*copybook.Field{nil}

		default:
			alternations = append(alternations, alternationOf(c, false))

			for _, member := range c.members {
				nested, under := describe(member)

				alternations = append(alternations, nested...)

				// The member leads what was chosen beneath it, because the
				// run it describes contains them: containment order is the
				// order the alternations were listed in above, and a name
				// built from a combination has to read in the same one.
				for _, combination := range under {
					chosen := make([]*copybook.Field, 0, len(combination)+1)
					chosen = append(chosen, member.Field)
					chosen = append(chosen, combination...)

					choices = append(choices, chosen)
				}
			}
		}

		combinations = crossed(combinations, choices)
	}

	return alternations, combinations
}

// alternationOf is one cluster as the caller of [Describe] reads it.
func alternationOf(c cluster, table bool) Alternation {
	fields := make([]*copybook.Field, 0, len(c.members))
	for _, member := range c.members {
		fields = append(fields, member.Field)
	}

	return Alternation{Item: c.members[0].Field, Alternatives: fields, InTable: table}
}

// crossed multiplies the combinations found so far by the choices one cluster
// admits, leaving the later cluster varying fastest.
//
// Which varies fastest is not arbitrary and is not free to change: a scaffold is
// diffed against the one a later run produces (docs/cli/SPEC.md, "What the
// scaffold states, form by form"), and reordering the record types would make
// every one of them look edited.
func crossed(lists, choices [][]*copybook.Field) [][]*copybook.Field {
	if len(choices) == 0 {
		return lists
	}

	next := make([][]*copybook.Field, 0, len(lists)*len(choices))

	for _, list := range lists {
		for _, choice := range choices {
			joined := make([]*copybook.Field, 0, len(list)+len(choice))
			joined = append(joined, list...)
			joined = append(joined, choice...)

			next = append(next, joined)
		}
	}

	return next
}
