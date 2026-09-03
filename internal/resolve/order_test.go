// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"strings"
	"testing"

	"github.com/Zaba505/cobol-go/copybook"
)

// This file is docs/ir/SPEC.md's "Transitions are ordered by what they read":
// the order a state's transitions leave this package in, and the three things
// the rule says about it.

// TestTransitionsAreOrderedByTheOffsetEachDiscriminatorReads is the rule on the
// shape that made it worth stating — a header, then details until the next
// header — and on the state where the walk gets it backwards (#323, #331).
//
// The order the transitions arrive in is an artifact of nesting. `name`
// allocates position 0 for the header and 1 for the detail; the inner `(+
// DETAIL)` links the detail's back edge into `follow[1]` first, and only then
// does the outer `(+ ...)` link the way from a detail back to a header — so the
// state after a detail arrives carrying the detail's test ahead of the header's,
// which is the inverse of what reads such a file, and neither `(alt ...)` nor
// anything else in the layout can reach it, since the two competitors come from
// different nesting levels.
//
// What is asserted is that it does not leave that way. The header's type code is
// the record's first byte and the detail's is ten bytes in, so a consumer
// reaches the header's first and the state carries it first.
func TestTransitionsAreOrderedByTheOffsetEachDiscriminatorReads(t *testing.T) {
	t.Parallel()

	// The detail's leading ten bytes are the digits that make the pair
	// provably exclusive, so what is asserted here is an order and not a
	// refusal (docs/ir/SPEC.md, "When two match, and when none does", #330).
	assertRendering(t, compiled(t, batchSource, batchCopybooks("PIC 9(10)")), `
state 0
  on bytes-equal BAT-TYPE "H", admit HEADER, go to 1
state 1
  on bytes-equal BDT-TYPE "D", admit DETAIL, go to 2
state 2 accepts
  on bytes-equal BAT-TYPE "H", admit HEADER, go to 1
  on bytes-equal BDT-TYPE "D", admit DETAIL, go to 2
`)
}

// TestTwoRunsBeginningAtOneOffsetAreOrderedByWhereTheyEnd is the tie-break, and
// it is the first rule again rather than a second one: a consumer standing at
// the byte both runs begin at has read enough to decide the shorter test before
// it has read enough to decide the longer.
//
// The two runs share their first byte and the literals disagree there, which is
// the whole of what admits the pair — the shared window is intersected rather
// than required to be the whole of either run (docs/ir/SPEC.md, "When two match,
// and when none does", #325). The walk offers the wider of the two first, so the
// order asserted here is one only the tie-break produces.
func TestTwoRunsBeginningAtOneOffsetAreOrderedByWhereTheyEnd(t *testing.T) {
	t.Parallel()

	copybooks := map[string]string{
		"HEADER": `01 BAT-HDR.
   05 BAT-TYPE PIC X(1).
   05 BAT-BODY PIC X(20).
`,
		"DETAIL": `01 BDT-REC.
   05 BDT-TYPE PIC X(2).
   05 BDT-BODY PIC X(19).
`,
	}

	source := `(record HEADER (copybook "header.cpy" BAT-HDR))
(record DETAIL (copybook "detail.cpy" BDT-REC))
(discriminate HEADER (equals (item HEADER BAT-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL BDT-TYPE) "DD"))
(sequence (+ (seq HEADER (+ DETAIL))))`

	assertRendering(t, compiled(t, source, copybooks), `
state 0
  on bytes-equal BAT-TYPE "H", admit HEADER, go to 1
state 1
  on bytes-equal BDT-TYPE "DD", admit DETAIL, go to 2
state 2 accepts
  on bytes-equal BAT-TYPE "H", admit HEADER, go to 1
  on bytes-equal BDT-TYPE "DD", admit DETAIL, go to 2
`)
}

// TestATransitionCarryingNoPredicateKeepsItsPosition is the third thing the rule
// says, and it is a restraint rather than an order: such a transition reads no
// byte of the record, so there is no offset to order it by and it is left
// exactly where the walk put it.
//
// That it is left there rather than swept to the end is the point. A transition
// carrying no predicate is not a default arm, and docs/ir/SPEC.md's "A
// transition may carry no predicate" says so in those words — it is evaluated in
// the order the state carries like every other transition, it is not tried last,
// and it does not catch what the others miss (#80). Ordering it behind
// everything that reads a run would have written the opposite into every
// descriptor.
//
// The assertion is over [compiler.order] rather than over a compiled layout,
// because a legal state offering such a transition between two others cannot be
// written: it matches every record, so the overlap rule refuses the state unless
// guards separate the three — and guards are not what this rule is about.
func TestATransitionCarryingNoPredicateKeepsItsPosition(t *testing.T) {
	t.Parallel()

	copybooks := batchCopybooks("PIC 9(10)")
	header, detail := recordOf(t, copybooks["HEADER"]), recordOf(t, copybooks["DETAIL"])

	c := &compiler{
		opts: Sequencing{
			Dialect: copybook.IBMEnterprise(),
			Records: []SequencedRecord{
				{Name: "HEADER", Item: header},
				{Name: "DETAIL", Item: detail},
			},
		},
		layouts: make(map[string]*copybook.Layout),
	}

	// Ten bytes in, one byte in, and nothing at all — in the order the walk
	// is entitled to hand them over.
	late := &Transition{Record: "DETAIL", Predicate: &Predicate{Target: fieldNamed(t, detail, "BDT-TYPE")}}
	anything := &Transition{Record: "DETAIL"}
	early := &Transition{Record: "HEADER", Predicate: &Predicate{Target: fieldNamed(t, header, "BAT-TYPE")}}

	state := &State{Transitions: []*Transition{late, anything, early}}

	c.order(state)

	want := []*Transition{early, anything, late}
	for at := range state.Transitions {
		if state.Transitions[at] == want[at] {
			continue
		}

		t.Fatalf("the state carries %s, want the two that read a run exchanged and the one that reads none left where it was",
			renderTransitions(state))
	}
}

// renderTransitions draws one state's transitions, for a failure whose subject
// is the order they are in.
func renderTransitions(state *State) string {
	rendered := make([]string, 0, len(state.Transitions))
	for _, transition := range state.Transitions {
		rendered = append(rendered, renderTransition(transition))
	}

	return "[" + strings.Join(rendered, "; ") + "]"
}
