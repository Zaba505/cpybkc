// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"errors"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// A predicate is evaluated against a record nobody has identified yet, so what
// bounds it is not its own record's extent but the shortest record its state can
// put in front of a consumer. These hold docs/ir/SPEC.md's "A predicate never
// reads past the record in front of it" to that: the layouts it refuses, the
// nearly identical one it admits, and the two framings it is not run under
// because the file states each record's length there and the read is bounded
// instead (#94).
//
// The shape throughout is the ordinary one rather than an exotic one: a header
// whose type code is its first field beside a data record whose type code sits
// behind an account key every record of that type carries. That is the shape a
// worked example is built on, and the whole difficulty is that it looks right.
//
// None of these layouts is refused by the overlap rule any more. The two runs
// share no byte, and the order the state carries settles a pair like that
// (#332) — so this rule is the only thing left that refuses them, and the
// relaxations below are layouts that compile rather than layouts refused in
// other words. That is also what makes the check worth asking of every pair a
// state can offer rather than only of the overlapping ones (#325): it is no
// longer standing behind a check that would have refused the same layout anyway.

// The copybooks these tests discriminate. `reachHeader` is ten bytes with its
// type code at the front; `reachDetail` carries a twenty-byte key in front of
// its own, which puts the target twelve bytes past the end of the header; and
// `reachFront` is the same detail with the code moved to the front, which is the
// fix the diagnostic asks for.
const (
	reachHeader = `01 H-REC.
   05 H-TYPE PIC X(2).
   05 H-DATE PIC X(8).
`

	reachDetail = `01 D-REC.
   05 D-KEY PIC X(20).
   05 D-TYPE PIC X(2).
   05 D-AMOUNT PIC X(8).
`

	reachFront = `01 D-REC.
   05 D-TYPE PIC X(2).
   05 D-KEY PIC X(20).
   05 D-AMOUNT PIC X(8).
`

	// reachStraddle puts the target across the header's last byte: it begins
	// at byte 9 of a record and ends at byte 12, so two of its four bytes are
	// inside the ten the header has and two are not.
	reachStraddle = `01 D-REC.
   05 D-KEY PIC X(8).
   05 D-TYPE PIC X(4).
   05 D-AMOUNT PIC X(8).
`

	// reachVariable is a record whose extent moves with a count: four bytes of
	// fixed head and one to five four-byte cells, so eight bytes at its
	// shortest and twenty-four at the storage a compiler reserves for it.
	reachVariable = `01 V-REC.
   05 V-TYPE PIC X(2).
   05 V-N PIC 9(2).
   05 V-CELL PIC X(4) OCCURS 1 TO 5 TIMES DEPENDING ON V-N.
`

	// reachBehindCell carries its type code at bytes 9 and 10 of a record,
	// which is inside reachVariable at two cells and outside it at one.
	reachBehindCell = `01 D-REC.
   05 D-KEY PIC X(8).
   05 D-TYPE PIC X(2).
   05 D-AMOUNT PIC X(4).
`

	// reachNarrow is a record one byte long, all of it the type code. A
	// two-byte target at the same offset ends one byte past it.
	reachNarrow = `01 N-REC.
   05 N-TYPE PIC X(1).
`

	// reachWide keys on two bytes at that same offset, so the two runs
	// intersect without being identical.
	reachWide = `01 W-REC.
   05 W-TYPE PIC X(2).
   05 W-BODY PIC X(8).
`
)

// The framings, in the spellings a layout writes them. The first two leave the
// bound to the layout and the last two state each record's length.
const (
	delimitedFraming  = `(framing (recfm line-sequential) (delimiter (bytes "25")) (placement terminator))`
	fixedFraming      = `(framing (recfm FB) (lrecl 30))`
	descriptorFraming = `(framing (recfm VB) (lrecl 512))`
	segmentedFraming  = `(framing (recfm VBS) (max-segment 4096))`
)

// reachLayout is a state admitting a header and a detail, under the framing the
// caller names.
func reachLayout(framing string) string {
	return framing + `
(record HEADER (copybook "header.cpy" H-REC))
(record DETAIL (copybook "detail.cpy" D-REC))
(discriminate HEADER (equals (item HEADER H-TYPE) "HD"))
(discriminate DETAIL (equals (item DETAIL D-TYPE) "DT"))
(sequence (* (alt HEADER DETAIL)))`
}

// reachCopybooks binds the two record names to the header and the detail the
// caller chose.
func reachCopybooks(detail string) map[string]string {
	return map[string]string{"HEADER": reachHeader, "DETAIL": detail}
}

// reachFaultIn is the reach fault a compilation reported, and fails the test
// where it reported none.
func reachFaultIn(t *testing.T, err error) *PredicateReachError {
	t.Helper()

	var reach *PredicateReachError
	if !errors.As(err, &reach) {
		t.Fatalf("compiling reported %v, want a PredicateReachError", err)
	}

	return reach
}

// notHeldToTheReachRule asserts that a layout this rule does not run on
// compiles, and names the reach fault where one was reported anyway.
//
// It is what the relaxations assert. Nothing else refuses these layouts: the two
// discriminators read runs that share no byte, and the order the state carries
// resolves that pair (#332), so a relaxation is the difference between a
// compilation and a refusal rather than between two diagnostics.
func notHeldToTheReachRule(t *testing.T, err error) {
	t.Helper()

	var reach *PredicateReachError
	if errors.As(err, &reach) {
		t.Fatalf("the reach rule was run: %v", reach)
	}

	if err != nil {
		t.Fatalf("compiling reported %v, want a layout this rule does not run on", err)
	}
}

// TestAPredicateReachingPastAShorterRecordAtTheSameStateIsRejected is the whole
// rule, on the shape that makes it worth having.
//
// Both predicates are inside their own records and the layout is otherwise
// unremarkable. What is wrong is that the state can put a ten-byte header in
// front of a consumer while the detail's predicate is testing bytes 21 and 22 —
// the delimiter's bytes, or the next record's data, and on a file whose bytes
// happen to hold `DT` there the wrong transition is taken and a record that is
// exactly right is reported malformed.
func TestAPredicateReachingPastAShorterRecordAtTheSameStateIsRejected(t *testing.T) {
	t.Parallel()

	err := refused(t, reachLayout(delimitedFraming), reachCopybooks(reachDetail))
	reach := reachFaultIn(t, err)

	if reach.Record != "DETAIL" || reach.Item != "D-TYPE" || reach.Beside != "HEADER" {
		t.Errorf("the fault is about %s's %s beside %s, want DETAIL's D-TYPE beside HEADER",
			reach.Record, reach.Item, reach.Beside)
	}
	if reach.Ends != 22 || reach.Extent != 10 {
		t.Errorf("the target ends at byte %d against a record of %d bytes, want byte 22 against 10",
			reach.Ends, reach.Extent)
	}

	// The three things docs/ir/SPEC.md asks the diagnostic to name — the
	// record the target is in, the target, and the shorter record it would be
	// read past the end of — and how far past it reaches.
	for _, want := range []string{"DETAIL", "D-TYPE", "HEADER", "12 bytes past"} {
		if !strings.Contains(reach.Error(), want) {
			t.Errorf("the diagnostic does not name %s: %s", want, reach.Error())
		}
	}

	// Only the detail is at fault. The header's own target is inside every
	// record the state can admit, and naming the pair rather than the record
	// reaching would send an adopter to a copybook that is right.
	if strings.Contains(err.Error(), "the discriminator for record HEADER") {
		t.Errorf("the header is reported too, and its target is inside every record beside it: %v", err)
	}

	// And the generic fault is not reported beside the specific one: one pair
	// is one thing to fix.
	var ambiguity *SequenceAmbiguityError
	if errors.As(err, &ambiguity) {
		t.Errorf("the overlap rule reported the same pair as well: %v", ambiguity)
	}
}

// TestAPredicateInsideTheShortestRecordIsAnOrdinaryLayout is the same file with
// the type code moved to the front, which is where a type code usually is and is
// the fix the diagnostic asks for.
//
// The accepted case is pinned beside the rejected one because the rule's whole
// cost is this move, and a check that refused both would be one nobody could
// satisfy under a delimited framing.
func TestAPredicateInsideTheShortestRecordIsAnOrdinaryLayout(t *testing.T) {
	t.Parallel()

	a := compiled(t, reachLayout(delimitedFraming), reachCopybooks(reachFront))

	if got := len(a.Start.Transitions); got != 2 {
		t.Fatalf("the start state offers %d transitions, want the header and the detail", got)
	}
}

// TestATargetStraddlingTheBoundIsRejectedOnTheSameFooting is docs/ir/SPEC.md's
// "a target past it, and a target beginning inside it and ending beyond": what a
// consumer reads is the target's whole width, so two bytes inside the header and
// two outside it is as much a read past the record as four bytes wholly outside
// it.
func TestATargetStraddlingTheBoundIsRejectedOnTheSameFooting(t *testing.T) {
	t.Parallel()

	reach := reachFaultIn(t, refused(t, reachLayout(delimitedFraming), reachCopybooks(reachStraddle)))

	if reach.Ends != 12 || reach.Extent != 10 {
		t.Errorf("the target ends at byte %d against a record of %d bytes, want byte 12 against 10",
			reach.Ends, reach.Extent)
	}
	if !strings.Contains(reach.Error(), "2 bytes past") {
		t.Errorf("the diagnostic does not say how far past the end it reaches: %s", reach.Error())
	}
}

// TestTheRuleIsRelaxedWhereTheFramingStatesEachRecordsLength is the other half
// of the answer, and the reason the check is keyed on the framing rather than
// applied to every layout.
//
// Under **descriptor-word** and **segmented** the length of the record in front
// of the consumer is in hand before any predicate runs, so a target outside it
// does not match and no file is misread. The layout compiles there, and the
// difference is the whole of what the framing buys: the same two copybooks under
// a delimited framing are refused for a read this one bounds.
func TestTheRuleIsRelaxedWhereTheFramingStatesEachRecordsLength(t *testing.T) {
	t.Parallel()

	for name, framing := range map[string]string{
		"descriptor-word": descriptorFraming,
		"segmented":       segmentedFraming,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := compileLayout(t, reachLayout(framing), reachCopybooks(reachDetail))
			notHeldToTheReachRule(t, err)
		})
	}
}

// TestALayoutStatingNoFramingIsNotHeldToEitherMechanism, because which mechanism
// bounds a predicate is the framing's answer and an `lrecl` is not a substitute
// for it: the same number is a maximum under two framings and a requirement
// under one.
//
// A layout read from a file always carries a `framing` form, so this is a caller
// assembling a [Sequencing] by hand — which is the road every other test of this
// package takes, and why the rule changes none of their answers.
func TestALayoutStatingNoFramingIsNotHeldToEitherMechanism(t *testing.T) {
	t.Parallel()

	_, err := compileLayout(t, reachLayout(""), reachCopybooks(reachDetail))
	notHeldToTheReachRule(t, err)
}

// TestAFixedLengthDatasetBoundsThePredicateByItsLRECL is why docs/ir/SPEC.md
// says **unframed** carries the rule and rarely pays for it.
//
// Every record type of a fixed-length dataset accounts for one LRECL, and a
// record type whose items stop short carries the difference as slack — the bytes
// are in the file whatever the copybook says. So the header is thirty bytes in
// front of a consumer and not ten, and the detail's target at bytes 21 and 22 is
// inside it. Measuring the copybook's sum instead would report a read past the
// end of a record the dataset has already made long enough.
func TestAFixedLengthDatasetBoundsThePredicateByItsLRECL(t *testing.T) {
	t.Parallel()

	_, err := compileLayout(t, reachLayout(fixedFraming), reachCopybooks(reachDetail))
	notHeldToTheReachRule(t, err)
}

// TestTheShortestRecordIsMeasuredAtTheMinimumOccurrences is the *shortest* in
// the rule: a record whose extent moves with a count is as short as its
// copybook's declared minimum allows, and not as long as the storage a compiler
// reserves for it.
//
// The detail's target sits at bytes 9 and 10, which is inside the variable
// record at two cells and outside it at one — and one is a count the copybook
// admits, so a file holding one is a file the layout describes.
func TestTheShortestRecordIsMeasuredAtTheMinimumOccurrences(t *testing.T) {
	t.Parallel()

	source := delimitedFraming + `
(record VARIABLE (copybook "variable.cpy" V-REC))
(record DETAIL (copybook "detail.cpy" D-REC))
(discriminate VARIABLE (equals (item VARIABLE V-TYPE) "VR"))
(discriminate DETAIL (equals (item DETAIL D-TYPE) "DT"))
(sequence (* (alt VARIABLE DETAIL)))`

	copybooks := map[string]string{"VARIABLE": reachVariable, "DETAIL": reachBehindCell}

	reach := reachFaultIn(t, refused(t, source, copybooks))
	if reach.Beside != "VARIABLE" || reach.Extent != 8 {
		t.Errorf("the target is bounded by %s at %d bytes, want VARIABLE at 8 — the head and one cell",
			reach.Beside, reach.Extent)
	}

	// Under the non-sliding reading the same clause is a fixed table at the
	// declared maximum, so there is no shorter reading of the record to take
	// and every record of that type is twenty-four bytes.
	t.Run("under the non-sliding reading the table is fixed and nothing is short", func(t *testing.T) {
		t.Parallel()

		opts := sequencingOf(t, source, copybooks)
		opts.Reading = layoutmodel.NoODOSlide

		_, err := CompileSequence(opts)
		notHeldToTheReachRule(t, err)
	})
}

// TestRecordsAdmittedAtDifferentStatesAreNotPaired, because the rule is about
// what one state can put in front of a consumer and not about what the file
// contains: a detail that only ever follows a header is never a record a
// consumer is choosing between, so nothing about the header bounds its
// predicate.
//
// This is the pairing every rule about telling two records apart is over
// (docs/ir/SPEC.md, "When two match, and when none does"), and a rule keyed on
// the record set instead would refuse a layout no consumer can misread.
func TestRecordsAdmittedAtDifferentStatesAreNotPaired(t *testing.T) {
	t.Parallel()

	compiled(t, delimitedFraming+`
(record HEADER (copybook "header.cpy" H-REC))
(record DETAIL (copybook "detail.cpy" D-REC))
(discriminate HEADER (equals (item HEADER H-TYPE) "HD"))
(discriminate DETAIL (equals (item DETAIL D-TYPE) "DT"))
(sequence (seq HEADER (* DETAIL)))`, reachCopybooks(reachDetail))
}

// TestAPredicateReachingPastAShorterRecordIsRejectedWhereThePairIsUnambiguous is
// the case #325 opened, and the reason this check is asked of every pair a state
// can offer rather than only of the overlapping ones.
//
// NARROW is one byte and asks for `H` there; WIDE asks for `DD` at bytes zero
// and one. They disagree on the byte the two runs share, so no record satisfies
// both and the overlap rule has nothing to say about the pair. A consumer at
// that state still reads two bytes to test WIDE, and where a NARROW is in front
// of it the second of them is the delimiter's or the next record's.
//
// While the overlap test asked the two runs to be identical this layout was
// refused as an ambiguity, so nothing was lost by leaving the reach check inside
// that branch. Narrowing the test is what made the branch the wrong place for
// it.
func TestAPredicateReachingPastAShorterRecordIsRejectedWhereThePairIsUnambiguous(t *testing.T) {
	t.Parallel()

	source := delimitedFraming + `
(record NARROW (copybook "narrow.cpy" N-REC))
(record WIDE (copybook "wide.cpy" W-REC))
(discriminate NARROW (equals (item NARROW N-TYPE) "H"))
(discriminate WIDE (equals (item WIDE W-TYPE) "DD"))
(sequence (* (alt NARROW WIDE)))`

	err := refused(t, source, map[string]string{"NARROW": reachNarrow, "WIDE": reachWide})

	// One record cannot carry `H` and `D` at byte zero at once, so the pair is
	// not the ambiguity the coarser test called it.
	var ambiguity *SequenceAmbiguityError
	if errors.As(err, &ambiguity) {
		t.Fatalf("the pair was reported as an ambiguity: %v", ambiguity)
	}

	reach := reachFaultIn(t, err)

	if reach.Record != "WIDE" || reach.Beside != "NARROW" {
		t.Errorf("the fault is about %s's target beside %s, want WIDE's beside NARROW",
			reach.Record, reach.Beside)
	}
	if reach.Ends != 2 || reach.Extent != 1 {
		t.Errorf("the target ends at byte %d against a record of %d bytes, want byte 2 against 1",
			reach.Ends, reach.Extent)
	}
}

// TestTheBatchShapeIsAdmittedAndStillHeldToTheReachRule is the pairing #332
// leaves behind, asserted on the shape that story is about: a sequence of
// batches whose two discriminators sit at different offsets and different
// widths.
//
// The overlap rule no longer refuses it and this one still can, and the two are
// independent. What decides is not whether the runs meet — they never do here —
// but whether either target is read out of a record shorter than the target
// ends. So the first of these compiles with the pair left to the order, and the
// second is refused in this rule's own words, naming the record whose bytes
// would have been read past.
func TestTheBatchShapeIsAdmittedAndStillHeldToTheReachRule(t *testing.T) {
	t.Parallel()

	// A header keyed on three bytes at the front, and a batch detail keyed on
	// one byte behind a key of its own — different offsets and different
	// widths, which is the pair that has no run to share.
	const header = `01 B-HDR.
   05 B-TYPE PIC X(3).
   05 B-ID   PIC X(9).
`

	const inside = `01 B-DTL.
   05 D-KEY  PIC X(8).
   05 D-TYPE PIC X(1).
   05 D-AMT  PIC X(3).
`

	const past = `01 B-DTL.
   05 D-KEY  PIC X(20).
   05 D-TYPE PIC X(1).
   05 D-AMT  PIC X(3).
`

	source := delimitedFraming + `
(record HEADER (copybook "header.cpy" B-HDR))
(record DETAIL (copybook "detail.cpy" B-DTL))
(discriminate HEADER (equals (item HEADER B-TYPE) "HDR"))
(discriminate DETAIL (equals (item DETAIL D-TYPE) "D"))
(sequence (+ (seq HEADER (* DETAIL))))`

	t.Run("both targets are inside both records", func(t *testing.T) {
		t.Parallel()

		automaton := compiled(t, source, map[string]string{"HEADER": header, "DETAIL": inside})

		// The header's run begins at byte zero and the detail's at byte
		// eight, so the header's test is tried first — which is the order
		// that reads a batched file (#331).
		state := stateAdmitting(t, automaton, "HEADER")
		if got := admits(state); got[0] != "HEADER" {
			t.Errorf("the state tries %s first, want the discriminator reading the earlier byte: %v", got[0], got)
		}
	})

	t.Run("the detail's target is read past the end of a header", func(t *testing.T) {
		t.Parallel()

		reach := reachFaultIn(t, refused(t, source, map[string]string{"HEADER": header, "DETAIL": past}))

		if reach.Record != "DETAIL" || reach.Beside != "HEADER" || reach.Ends != 21 || reach.Extent != 12 {
			t.Errorf("the fault is %s's target ending at byte %d beside %s at %d bytes, want DETAIL's at 21 beside HEADER at 12",
				reach.Record, reach.Ends, reach.Beside, reach.Extent)
		}

		// And it is this rule's fault and not the overlap rule's: the pair
		// the order would otherwise have settled is still refused here, in
		// words that name the read rather than the pair (#332).
		var ambiguity *SequenceAmbiguityError
		if errors.As(reach, &ambiguity) {
			t.Errorf("the overlap rule reported the same pair as well: %v", ambiguity)
		}
	})
}
