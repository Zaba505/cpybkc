// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"errors"
	"strings"
	"testing"

	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/layout"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// Discrimination reaches a generator compiled, and these hold the compilation to
// docs/ir/SPEC.md's "Discriminator predicates": a closed set of two tests, a
// target that is a field node and never a name or a position, literals that are
// already the bytes a consumer compares, and the four shapes `resolve` refuses
// rather than describing.
//
// Every test drives the real layout parser and the real copybook reader, as the
// automaton's do, so that nothing is asserted about a graph the readers would
// never have produced. The encoding profile is the mainframe one throughout,
// because what a literal resolves to is the whole point and cp037 is where `H`
// is not `0x48`.

// The copybooks these tests discriminate. `typed` carries its type code at its
// first byte; `prefixed` carries a header every alternative includes and its own
// code behind it, which is docs/ir/SPEC.md's mid-record discriminator; `tabular`
// has nothing outside its table to name.
const (
	typed = `01 T-REC.
   05 T-TYPE PIC X(3).
   05 T-BODY PIC X(10).
`

	prefixed = `01 P-REC.
   05 P-HEAD.
      10 P-KEY PIC X(6).
      10 P-SEQ PIC 9(2).
   05 P-TYPE PIC X(1).
   05 P-BODY PIC X(4).
`

	tabular = `01 A-REC.
   05 A-ENTRY OCCURS 5 TIMES.
      10 A-CODE PIC X(1).
      10 A-DATA PIC X(9).
`
)

// oneRecord is a layout of a single record type, discriminated as the caller
// says and sequenced as any number of that record.
func oneRecord(discriminator string) string {
	return `(record ONLY (copybook "only.cpy" T-REC))
(discriminate ONLY ` + discriminator + `)
(sequence (* ONLY))`
}

// predicateOf is the predicate on the one transition leaving the start state.
func predicateOf(t *testing.T, a *Automaton) *Predicate {
	t.Helper()

	if len(a.Start.Transitions) == 0 {
		t.Fatal("the start state offers no transition")
	}

	return a.Start.Transitions[0].Predicate
}

// compileHand compiles a sequence over records a test assembled itself, which is
// how a strategy the layout format has no spelling for reaches this package.
//
// docs/layout/SPEC.md closes the set at three, so `record-length` and
// `positional` are refused while a layout is being read and never reach
// `resolve` from a file. `resolve` refuses them all the same, in the words
// docs/ir/SPEC.md asks for, and this is the road that reaches those words.
func compileHand(t *testing.T, source string, records []SequencedRecord) (*Automaton, error) {
	t.Helper()

	file, err := layout.Parse("layout.sexpr", strings.NewReader(source))
	if err != nil {
		t.Fatalf("parsing the layout: %v", err)
	}

	sequence, err := layoutmodel.ReadSequence(file)
	if err != nil {
		t.Fatalf("reading the sequencing layer: %v", err)
	}

	return CompileSequence(Sequencing{
		Sequence: sequence,
		Dialect:  copybook.IBMEnterprise(),
		Reading:  layoutmodel.ODOSlide,
		Encoding: mainframe(),
		Records:  records,
	})
}

// TestEachStrategyLowersToOneMemberOfTheClosedSet is the whole of the mapping:
// `equals` and `one-of` are the two members, and `single-record-type` is the
// absence of a predicate rather than a third member testing nothing.
func TestEachStrategyLowersToOneMemberOfTheClosedSet(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		strategy string
		test     PredicateTest
		values   int
	}{
		"equals": {strategy: `(equals (item ONLY T-TYPE) "ABC")`, test: BytesEqual, values: 1},
		"one-of": {strategy: `(one-of (item ONLY T-TYPE) "ABC" "DEF")`, test: BytesOneOf, values: 2},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			a := compiled(t, oneRecord(test.strategy), map[string]string{"ONLY": typed})

			predicate := predicateOf(t, a)
			if predicate == nil {
				t.Fatal("the transition carries no predicate")
			}
			if predicate.Test != test.test {
				t.Errorf("the predicate is %s, want %s", predicate.Test, test.test)
			}
			if len(predicate.Values) != test.values {
				t.Errorf("the predicate carries %d values, want %d", len(predicate.Values), test.values)
			}
			if predicate.Target == nil || predicate.Target.Name != "T-TYPE" {
				t.Errorf("the predicate names %s, want the field node T-TYPE", itemName(predicate.Target))
			}
		})
	}

	t.Run("single-record-type", func(t *testing.T) {
		t.Parallel()

		a := compiled(t, oneRecord("single-record-type"), map[string]string{"ONLY": typed})
		if predicate := predicateOf(t, a); predicate != nil {
			t.Errorf("the transition carries %s, want no predicate at all", predicate)
		}
	})
}

// TestAPredicateCarriesItsLiteralsAsBytesPaddedToTheTarget is the requirement
// that no consumer decides whether `Y` matches `Y `: the bytes leave here padded
// to the item's width, in the file's own code page.
func TestAPredicateCarriesItsLiteralsAsBytesPaddedToTheTarget(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		strategy string
		want     []byte
	}{
		// cp037: `A` is 0xC1, and the two bytes T-TYPE has left over
		// are the code page's space rather than a NUL or a truncation.
		"text is translated and padded": {
			strategy: `(equals (item ONLY T-TYPE) "A")`,
			want:     []byte{0xC1, 0x40, 0x40},
		},
		"a byte string is what is in the file": {
			strategy: `(equals (item ONLY T-TYPE) (bytes "C1C2C3"))`,
			want:     []byte{0xC1, 0xC2, 0xC3},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			a := compiled(t, oneRecord(test.strategy), map[string]string{"ONLY": typed})

			predicate := predicateOf(t, a)
			if got := predicate.Values[0].Bytes; string(got) != string(test.want) {
				t.Errorf("the literal resolved to % X, want % X", got, test.want)
			}
			if !predicate.Matches(test.want) {
				t.Error("the predicate does not match the bytes it was compiled to")
			}
			if predicate.Matches(test.want[:2]) {
				t.Error("the predicate matches a prefix of its target, want the whole of it")
			}
		})
	}

	t.Run("a number is the digits of a zoned item", func(t *testing.T) {
		t.Parallel()

		a := compiled(t,
			`(record ONLY (copybook "only.cpy" P-REC))
(discriminate ONLY (equals (item ONLY P-HEAD P-SEQ) 7))
(sequence (* ONLY))`,
			map[string]string{"ONLY": prefixed})

		want := []byte{0xF0, 0xF7}
		if got := predicateOf(t, a).Values[0].Bytes; string(got) != string(want) {
			t.Errorf("the number resolved to % X, want % X", got, want)
		}
	})
}

// TestALiteralThatCannotBeResolvedIsReportedAndNotGuessed, because a producer
// emitting plausible bytes for a value it could not compute is the silent
// failure this whole project is arranged against.
func TestALiteralThatCannotBeResolvedIsReportedAndNotGuessed(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"wider than the item":       `(equals (item ONLY T-TYPE) "ABCD")`,
		"a byte string mismeasured": `(equals (item ONLY T-TYPE) (bytes "C1C2"))`,
		"a number against text":     `(equals (item ONLY T-TYPE) 12)`,
	}

	for name, strategy := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := refused(t, oneRecord(strategy), map[string]string{"ONLY": typed})

			var literal *PredicateLiteralError
			if !errors.As(err, &literal) {
				t.Fatalf("compiling reported %v, want a PredicateLiteralError", err)
			}
			if !strings.Contains(literal.Error(), "T-TYPE") {
				t.Errorf("the diagnostic does not name the item: %s", literal.Error())
			}
		})
	}
}

// TestADiscriminatorTestingAFieldOfAnotherRecordIsRejected, which is the half of
// docs/ir/SPEC.md's containment rule that needs a copybook, plus the half
// `layoutmodel` checks, reached by a caller that assembled a strategy itself.
func TestADiscriminatorTestingAFieldOfAnotherRecordIsRejected(t *testing.T) {
	t.Parallel()

	t.Run("a path naming no item of the record", func(t *testing.T) {
		t.Parallel()

		err := refused(t, oneRecord(`(equals (item ONLY T-MISSING) "A")`), map[string]string{"ONLY": typed})

		var target *PredicateTargetError
		if !errors.As(err, &target) {
			t.Fatalf("compiling reported %v, want a PredicateTargetError", err)
		}
		if !strings.Contains(target.Error(), "names no item") {
			t.Errorf("the diagnostic does not say the path names nothing: %s", target.Error())
		}
	})

	t.Run("a reference rooted at another record", func(t *testing.T) {
		t.Parallel()

		_, err := compileHand(t,
			`(record ONLY (copybook "only.cpy" T-REC))
(sequence (* ONLY))`,
			[]SequencedRecord{{
				Name:     "ONLY",
				Copybook: "only.cpy",
				Item:     recordOf(t, typed),
				Discriminator: layoutmodel.Strategy{
					Kind:     layoutmodel.Equals,
					Item:     layoutmodel.ItemRef{Record: "OTHER", Path: []string{"T-TYPE"}},
					Literals: []layoutmodel.Literal{{Kind: layoutmodel.TextLiteral, Text: "A"}},
				},
			}})

		var target *PredicateTargetError
		if !errors.As(err, &target) {
			t.Fatalf("compiling reported %v, want a PredicateTargetError", err)
		}
		if !strings.Contains(target.Error(), "OTHER") {
			t.Errorf("the diagnostic does not name the other record: %s", target.Error())
		}
	})
}

// TestADiscriminatorTargetInsideARepeatingGroupIsRejected names the record, the
// field and the enclosing group, and not a generic reference error: which of
// five entries the value would come from is the question nothing can answer.
func TestADiscriminatorTargetInsideARepeatingGroupIsRejected(t *testing.T) {
	t.Parallel()

	err := refused(t,
		`(record ONLY (copybook "only.cpy" A-REC))
(discriminate ONLY (equals (item ONLY A-ENTRY A-CODE) "X"))
(sequence (* ONLY))`,
		map[string]string{"ONLY": tabular})

	var occurrence *PredicateOccurrenceError
	if !errors.As(err, &occurrence) {
		t.Fatalf("compiling reported %v, want a PredicateOccurrenceError", err)
	}
	for _, want := range []string{"ONLY", "A-CODE", "A-ENTRY"} {
		if !strings.Contains(occurrence.Error(), want) {
			t.Errorf("the diagnostic does not name %s: %s", want, occurrence.Error())
		}
	}
}

// TestADiscriminatorTargetBehindAVariableItemIsRejected is the constant-position
// restriction, and the diagnostic names the variable item in front of the target
// because that is the thing the adopter has to move.
func TestADiscriminatorTargetBehindAVariableItemIsRejected(t *testing.T) {
	t.Parallel()

	src := `01 V-REC.
   05 V-N PIC 9(2).
   05 V-CELL PIC X(4) OCCURS 1 TO 9 TIMES DEPENDING ON V-N.
   05 V-TYPE PIC X(1).
`

	err := refused(t,
		`(record ONLY (copybook "only.cpy" V-REC))
(discriminate ONLY (equals (item ONLY V-TYPE) "X"))
(sequence (* ONLY))`,
		map[string]string{"ONLY": src})

	var position *PredicatePositionError
	if !errors.As(err, &position) {
		t.Fatalf("compiling reported %v, want a PredicatePositionError", err)
	}
	for _, want := range []string{"ONLY", "V-TYPE", "V-CELL", "V-N"} {
		if !strings.Contains(position.Error(), want) {
			t.Errorf("the diagnostic does not name %s: %s", want, position.Error())
		}
	}
}

// TestADiscriminatorBehindASharedPrefixIsAnOrdinaryLayout, because which record
// the position sum is over is settled before the predicate is evaluated rather
// than by it: a record whose second half is a set of alternatives is how a
// mainframe file is usually built.
func TestADiscriminatorBehindASharedPrefixIsAnOrdinaryLayout(t *testing.T) {
	t.Parallel()

	a := compiled(t,
		`(record ONLY (copybook "only.cpy" P-REC))
(discriminate ONLY (equals (item ONLY P-TYPE) "D"))
(sequence (* ONLY))`,
		map[string]string{"ONLY": prefixed})

	predicate := predicateOf(t, a)
	if predicate == nil || predicate.Target.Name != "P-TYPE" {
		t.Fatalf("the transition is selected by %s, want the type code behind the shared header", predicate)
	}
}

// TestARecordWithNothingOutsideATableToNameIsRejectedBesideASibling is
// docs/ir/SPEC.md's "A record with nothing outside a table to name", and the
// diagnostic says so rather than reporting an ambiguity an adopter would try to
// fix with a different literal.
func TestARecordWithNothingOutsideATableToNameIsRejectedBesideASibling(t *testing.T) {
	t.Parallel()

	err := refused(t,
		`(record TABULAR (copybook "a.cpy" A-REC))
(record TYPED (copybook "t.cpy" T-REC))
(discriminate TABULAR single-record-type)
(discriminate TYPED (equals (item TYPED T-TYPE) "ABC"))
(sequence (* (alt TABULAR TYPED)))`,
		map[string]string{"TABULAR": tabular, "TYPED": typed})

	var unnameable *UnnameableRecordError
	if !errors.As(err, &unnameable) {
		t.Fatalf("compiling reported %v, want an UnnameableRecordError", err)
	}
	if unnameable.Record != "TABULAR" || unnameable.Beside != "TYPED" {
		t.Errorf("the diagnostic is about %s beside %s, want TABULAR beside TYPED",
			unnameable.Record, unnameable.Beside)
	}
	if !strings.Contains(unnameable.Error(), "offers no target a predicate may name") {
		t.Errorf("the diagnostic does not say what is missing: %s", unnameable.Error())
	}
}

// TestARecordWithNothingOutsideATableToNameIsAdmittedWhereNothingIsBesideIt,
// which is why the rejection above is narrower than its heading: what is refused
// is the record standing beside another at one state, and not the record.
func TestARecordWithNothingOutsideATableToNameIsAdmittedWhereNothingIsBesideIt(t *testing.T) {
	t.Parallel()

	a := compiled(t,
		`(record TABULAR (copybook "a.cpy" A-REC))
(discriminate TABULAR single-record-type)
(sequence (* TABULAR))`,
		map[string]string{"TABULAR": tabular})

	if predicate := predicateOf(t, a); predicate != nil {
		t.Errorf("the transition carries %s, want no predicate at all", predicate)
	}
}

// TestARecordOfNoExtentIsRefusedBeforeATransitionCanAdmitOne is the requirement
// that no transition admits a record whose extent is zero, asserted where the
// refusal actually lands today.
//
// [compiler.checkExtent] states it in the requirement's own terms, and this
// records why it cannot fire from a copybook: the only shape that would lay out
// to nothing is a group with no elementary item under it, and `cobol-go` refuses
// that while it is laying the record out. The check is kept for
// [resolver.assertResolved]'s reason — the requirement is on what leaves this
// package rather than on what entered it, and a caller assembling a record by
// hand reaches this package without passing that reader.
func TestARecordOfNoExtentIsRefusedBeforeATransitionCanAdmitOne(t *testing.T) {
	t.Parallel()

	field := recordOf(t, "01 E-REC.\n   05 E-GROUP.\n")

	if _, err := copybook.NewLayout(field, copybook.IBMEnterprise()); err == nil {
		t.Fatal("a record occupying no bytes laid out, and nothing downstream would have refused it")
	}
}

// TestARecordLengthDiscriminatorIsRefusedNamingBothRecords. The rejection is
// keyed on what the layout asks for, because whether two records are *in fact*
// told apart by some field is a property of the adopter's data and a rule keyed
// on that would be false whenever it fired.
func TestARecordLengthDiscriminatorIsRefusedNamingBothRecords(t *testing.T) {
	t.Parallel()

	_, err := compileHand(t,
		`(record LONGER (copybook "p.cpy" P-REC))
(record SHORTER (copybook "t.cpy" T-REC))
(sequence (* (alt LONGER SHORTER)))`,
		[]SequencedRecord{
			{
				Name:          "LONGER",
				Copybook:      "p.cpy",
				Item:          recordOf(t, prefixed),
				Discriminator: layoutmodel.Strategy{Kind: strategyRecordLength},
			},
			{
				Name:          "SHORTER",
				Copybook:      "t.cpy",
				Item:          recordOf(t, typed),
				Discriminator: layoutmodel.Strategy{Kind: layoutmodel.SingleRecordType},
			},
		})

	var refusal *RefusedStrategyError
	if !errors.As(err, &refusal) {
		t.Fatalf("compiling reported %v, want a RefusedStrategyError", err)
	}
	for _, want := range []string{"LONGER", "SHORTER", "a record's length is not one"} {
		if !strings.Contains(refusal.Error(), want) {
			t.Errorf("the diagnostic does not carry %q: %s", want, refusal.Error())
		}
	}
}

// TestAPositionalLastDiscriminatorIsRefused, and the reason in the message is
// the writer's rather than the reader's: *last* becomes true only when the
// caller says there are no more records, which is after the record has been
// written.
func TestAPositionalLastDiscriminatorIsRefused(t *testing.T) {
	t.Parallel()

	_, err := compileHand(t,
		`(record ONLY (copybook "only.cpy" T-REC))
(sequence (* ONLY))`,
		[]SequencedRecord{{
			Name:     "ONLY",
			Copybook: "only.cpy",
			Item:     recordOf(t, typed),
			Discriminator: layoutmodel.Strategy{
				Kind:     strategyPositional,
				Literals: []layoutmodel.Literal{{Kind: layoutmodel.TextLiteral, Text: "last"}},
			},
		}})

	var refusal *RefusedStrategyError
	if !errors.As(err, &refusal) {
		t.Fatalf("compiling reported %v, want a RefusedStrategyError", err)
	}
	if !strings.Contains(refusal.Error(), "a writer does not know which record is last") {
		t.Errorf("the diagnostic does not give the writer's reason: %s", refusal.Error())
	}
}

// TestAPositionalFirstDiscriminatorLowersToTheStartState: nothing is added to
// express it, because a state is already what the automaton knows about
// position. What is checked is the other half — that the record is not also
// admitted somewhere further in, where being first says nothing about it.
func TestAPositionalFirstDiscriminatorLowersToTheStartState(t *testing.T) {
	t.Parallel()

	first := func() SequencedRecord {
		return SequencedRecord{
			Name:     "HEAD",
			Copybook: "t.cpy",
			Item:     recordOf(t, typed),
			Discriminator: layoutmodel.Strategy{
				Kind:     strategyPositional,
				Literals: []layoutmodel.Literal{{Kind: layoutmodel.TextLiteral, Text: "first"}},
			},
		}
	}

	body := SequencedRecord{
		Name:     "BODY",
		Copybook: "p.cpy",
		Item:     recordOf(t, prefixed),
		Discriminator: layoutmodel.Strategy{
			Kind:     layoutmodel.Equals,
			Item:     layoutmodel.ItemRef{Record: "BODY", Path: []string{"P-TYPE"}},
			Literals: []layoutmodel.Literal{{Kind: layoutmodel.TextLiteral, Text: "D"}},
		},
	}

	a, err := compileHand(t,
		`(record HEAD (copybook "t.cpy" T-REC))
(record BODY (copybook "p.cpy" P-REC))
(sequence (seq HEAD (* BODY)))`,
		[]SequencedRecord{first(), body})
	if err != nil {
		t.Fatalf("compiling a first record: %v", err)
	}

	if predicate := a.Start.Transitions[0].Predicate; predicate != nil {
		t.Errorf("the first record is selected by %s, want the start state and no predicate", predicate)
	}

	for _, state := range a.States {
		if state == a.Start {
			continue
		}
		for _, transition := range state.Transitions {
			if transition.To == a.Start {
				t.Errorf("state %d re-enters the start state, which is what makes *first* expressible", state.ID)
			}
		}
	}

	// The same strategy on a record the expression admits twice is refused,
	// because a state distinguishes position only while it is entered once.
	_, err = compileHand(t,
		`(record HEAD (copybook "t.cpy" T-REC))
(record BODY (copybook "p.cpy" P-REC))
(sequence (seq HEAD (* BODY) HEAD))`,
		[]SequencedRecord{first(), body})

	var refusal *RefusedStrategyError
	if !errors.As(err, &refusal) {
		t.Fatalf("compiling reported %v, want a RefusedStrategyError", err)
	}
}

// TestAPredicateIsCarriedEvenWhereGuardsAloneWouldSelect is docs/ir/SPEC.md's
// **SHOULD** kept rather than optimised away: a predicate the automaton does not
// need in order to choose is the only detection such a state has.
func TestAPredicateIsCarriedEvenWhereGuardsAloneWouldSelect(t *testing.T) {
	t.Parallel()

	a := compiled(t, countedRun, countedRunCopybooks())

	state := a.States[1]
	detail := transitionAdmitting(t, state, "DETAIL")

	if len(detail.Guards) == 0 {
		t.Fatal("the detail transition carries no guard, and this test is about one that does")
	}
	if detail.Predicate == nil {
		t.Error("the detail transition carries no predicate, and its guard alone would have selected it")
	}
}

// TestTwoSpellingsOfOneValueOverlap is what resolving the literals buys the
// overlap check. `layoutmodel` compares spellings and cannot see that `"A"` and
// `(bytes "C1")` are one value; with a copybook in hand they are, and the two
// transitions are two a record satisfies both of.
func TestTwoSpellingsOfOneValueOverlap(t *testing.T) {
	t.Parallel()

	err := refused(t,
		`(record FIRST (copybook "t.cpy" T-REC))
(record SECOND (copybook "u.cpy" T-REC))
(discriminate FIRST (equals (item FIRST T-TYPE) "A"))
(discriminate SECOND (equals (item SECOND T-TYPE) (bytes "C14040")))
(sequence (* (alt FIRST SECOND)))`,
		map[string]string{"FIRST": typed, "SECOND": typed})

	var ambiguity *SequenceAmbiguityError
	if !errors.As(err, &ambiguity) {
		t.Fatalf("compiling reported %v, want a SequenceAmbiguityError", err)
	}
}

// TestAGuardOverABytesRegisterCarriesItsLiteralPadded, so that no consumer
// decides whether `Y` matches `Y `. It is the predicate's rule reached from the
// other side, and it is why one package resolves both.
func TestAGuardOverABytesRegisterCarriesItsLiteralPadded(t *testing.T) {
	t.Parallel()

	a := compiled(t, countedRun, countedRunCopybooks())

	summary := transitionAdmitting(t, a.States[1], "SUMMARY")

	var flag *Guard
	for i, guard := range summary.Guards {
		if guard.Register.Kind == RegisterBytes {
			flag = &summary.Guards[i]
		}
	}
	if flag == nil {
		t.Fatalf("the summary transition is guarded by %s, want the flag among them", renderGuards(summary.Guards))
	}

	// SUM-FLAG is PIC X(1), so `Y` is one byte and there is nothing to pad —
	// what is asserted is that the guard carries bytes at all, and cp037's.
	if len(flag.Values) != 1 || string(flag.Values[0].Bytes) != "\xE8" {
		t.Errorf("the flag guard compares % X, want cp037's Y", flag.Values[0].Bytes)
	}
}

// TestAWiderFlagPadsItsGuardLiteral is the same rule where there is something to
// pad: a `PIC X(4)` flag compared against `Y` compares four bytes.
func TestAWiderFlagPadsItsGuardLiteral(t *testing.T) {
	t.Parallel()

	wide := `01 W-REC.
   05 W-TYPE PIC X(1).
   05 W-FLAG PIC X(4).
`

	a := compiled(t,
		`(record HEAD (copybook "w.cpy" W-REC))
(record BODY (copybook "t.cpy" T-REC))
(discriminate HEAD (equals (item HEAD W-TYPE) "H"))
(discriminate BODY (equals (item BODY T-TYPE) "ABC"))
(sequence (seq HEAD (when (item HEAD W-FLAG) "Y" BODY)))`,
		map[string]string{"HEAD": wide, "BODY": typed})

	body := transitionAdmitting(t, a.States[1], "BODY")
	if len(body.Guards) != 1 {
		t.Fatalf("the body transition carries %d guards, want the flag alone", len(body.Guards))
	}

	want := "\xE8\x40\x40\x40"
	if got := string(body.Guards[0].Values[0].Bytes); got != want {
		t.Errorf("the guard compares % X, want % X", got, want)
	}
}

// TestARecordNoTransitionMatchedIsReportedAsUndescribed, rather than skipping
// ahead to a transition that matches later or falling through to a default:
// there is no default, and a file containing an undescribed record is a file the
// layout is wrong about.
func TestARecordNoTransitionMatchedIsReportedAsUndescribed(t *testing.T) {
	t.Parallel()

	a := compiled(t, countedRun, countedRunCopybooks())

	_, err := a.Start.Select(RegisterFile{}, func(*Predicate) []byte { return []byte("\xE9") })

	var undescribed *UndescribedRecordError
	if !errors.As(err, &undescribed) {
		t.Fatalf("selecting reported %v, want an UndescribedRecordError", err)
	}
	if !strings.Contains(undescribed.Error(), "HEADER") {
		t.Errorf("the diagnostic does not list what the state does admit: %s", undescribed.Error())
	}
}

// TestAGuardExcludedTransitionIsReportedByItsRegister, because the two failures
// send an adopter to different places: an undescribed record means the layout is
// missing a record type, and a detail arriving after its counter reached zero
// means the file and its own header disagree about how many there are.
func TestAGuardExcludedTransitionIsReportedByItsRegister(t *testing.T) {
	t.Parallel()

	a := compiled(t, countedRun, countedRunCopybooks())

	// The counter has reached zero and the flag says no summary, so the only
	// eligible transition is the one admitting another header — and the bytes
	// in front of the consumer are a detail's.
	held := RegisterFile{
		1: number(layoutmodel.Literal{Kind: layoutmodel.NumberLiteral, Number: "0"}),
		2: {Bytes: []byte("\x40")},
	}

	_, err := a.States[1].Select(held, func(*Predicate) []byte { return []byte("\xC4") })

	var excluded *GuardExcludedError
	if !errors.As(err, &excluded) {
		t.Fatalf("selecting reported %v, want a GuardExcludedError", err)
	}
	if excluded.Record != "DETAIL" {
		t.Errorf("the diagnostic is about %s, want DETAIL", excluded.Record)
	}
	if !strings.Contains(excluded.Error(), "DTL-COUNT") {
		t.Errorf("the diagnostic does not name the register the guard tested: %s", excluded.Error())
	}
}
