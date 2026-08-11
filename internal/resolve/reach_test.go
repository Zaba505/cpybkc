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
// Every one of these layouts is refused by the overlap rule as well, and that is
// a property of the rule rather than of the tests — [compiler.reportReach] is
// where the argument is. So what the rejecting tests assert is *which*
// diagnostic an adopter gets, and they assert the generic one is not reported
// beside it.

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
	// at byte 9 of a record and ends at byte 12, so eight of its bytes are
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

// leftToTheOverlapRule asserts that a pair was reported as the ambiguity it also
// is, and not as a reach fault.
//
// It is what the relaxations assert. The pair is refused either way — two
// predicates over different runs of bytes are told apart by nothing — so what a
// relaxation changes is the diagnostic, and a test asserting the compilation
// failed would pass without the rule being relaxed at all.
func leftToTheOverlapRule(t *testing.T, err error) {
	t.Helper()

	var reach *PredicateReachError
	if errors.As(err, &reach) {
		t.Fatalf("the reach rule was run: %v", reach)
	}

	var ambiguity *SequenceAmbiguityError
	if !errors.As(err, &ambiguity) {
		t.Fatalf("compiling reported %v, want the overlap rule's own fault", err)
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
// consumer reads is the target's whole width, so eight bytes inside the header
// and two outside it is as much a read past the record as four bytes wholly
// outside it.
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
// does not match and no file is misread. The layout is still refused, because
// two predicates over different runs of bytes are told apart by nothing — but it
// is refused as the ambiguity it is, and not for a read that cannot happen
// there.
func TestTheRuleIsRelaxedWhereTheFramingStatesEachRecordsLength(t *testing.T) {
	t.Parallel()

	for name, framing := range map[string]string{
		"descriptor-word": descriptorFraming,
		"segmented":       segmentedFraming,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			leftToTheOverlapRule(t, refused(t, reachLayout(framing), reachCopybooks(reachDetail)))
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

	leftToTheOverlapRule(t, refused(t, reachLayout(""), reachCopybooks(reachDetail)))
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

	leftToTheOverlapRule(t, refused(t, reachLayout(fixedFraming), reachCopybooks(reachDetail)))
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
		leftToTheOverlapRule(t, err)
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
