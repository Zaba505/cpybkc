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

	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// An arm of a variant is selected by a predicate of the same kind and the same
// closed set as a transition's, and these hold that scope to docs/ir/SPEC.md's
// "A predicate on an arm reads one occurrence": a target inside the occurrence
// being walked, no constant-position rule, no rule against sitting in a
// repeating group, and the overlap test a state's transitions get.
//
// The rules that reverse are the interesting ones, and each has a test here
// beside the transition-scope test that asserts its opposite in predicate_test.go.

// nested is a table whose entry carries a redefine inside a redefine: the outer
// alternation chooses how the entry is shaped and the inner one chooses inside
// the arm that contains it, which is the one arm an arm's target may sit in
// besides the occurrence at large.
const nested = `01 N-REC.
   05 N-HDR PIC X(3).
   05 N-ENTRY OCCURS 3 TIMES.
      10 N-KIND PIC X(1).
      10 N-OUTER.
         15 N-INNER-KIND PIC X(1).
         15 N-INNER PIC X(6).
         15 N-INNER-ALT REDEFINES N-INNER PIC X(6).
      10 N-OUTER-ALT REDEFINES N-OUTER PIC X(7).
      10 N-TAG PIC X(2).
`

// armsOf is the arms of the one variant in a resolved record.
func armsOf(t *testing.T, record *Record) []Arm {
	t.Helper()

	variant := variantIn(record.Root)
	if variant == nil {
		t.Fatal("the record holds no variant")
	}

	return variant.Arms
}

// TestAnArmIsSelectedByATypeCodeInsideTheEntry is the ordinary shape: a code at
// the front of an occurrence saying which alternative the rest of it is.
//
// The target sits inside the group that repeats, which is the reverse of the
// rule binding a transition's predicate — and the reason is that rule's own. A
// reference with no occurrence to be read in is what it refuses, and an arm's
// predicate is evaluated in the occurrence being walked.
func TestAnArmIsSelectedByATypeCodeInsideTheEntry(t *testing.T) {
	t.Parallel()

	records, err := resolveTable(t, tableSource, func(field *copybook.Field) []Redefine {
		return []Redefine{{
			Item: fieldNamed(t, field, "BODY"),
			Alternatives: []Alternative{
				{Name: "BODY", Predicate: selects("B")},
				{Name: "SPLIT", Predicate: selects("S")},
			},
		}}
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	arms := armsOf(t, records[0])
	for _, arm := range arms {
		if arm.Predicate == nil {
			t.Fatalf("the arm %s is selected by nothing", arm.Alternative)
		}
		if arm.Predicate.Test != BytesEqual {
			t.Errorf("the arm %s is selected by %s, want the bytes-equal member",
				arm.Alternative, arm.Predicate.Test)
		}
		if arm.Predicate.Target.Name != "CODE" {
			t.Errorf("the arm %s names %s, want CODE", arm.Alternative, itemName(arm.Predicate.Target))
		}
	}

	// cp037's B and S, each one byte because CODE is PIC X.
	if got := string(arms[0].Predicate.Values[0].Bytes); got != "\xC2" {
		t.Errorf("the first arm compares % X, want cp037's B", got)
	}
	if got := string(arms[1].Predicate.Values[0].Bytes); got != "\xE2" {
		t.Errorf("the second arm compares % X, want cp037's S", got)
	}
}

// TestAnArmTargetMaySitBehindTheVariant, because an arm runs against a record
// already admitted and every byte of the occurrence is locatable before an arm
// is chosen — the arms are of one extent, so nothing about where the variant
// ends depends on which arm was taken.
func TestAnArmTargetMaySitBehindTheVariant(t *testing.T) {
	t.Parallel()

	records, err := resolveTable(t, tableSource, func(field *copybook.Field) []Redefine {
		return []Redefine{{
			Item: fieldNamed(t, field, "BODY"),
			Alternatives: []Alternative{
				{Name: "BODY", Predicate: selects("B", "ENTRY", "TAG")},
				{Name: "SPLIT", Predicate: selects("S", "ENTRY", "TAG")},
			},
		}}
	})
	if err != nil {
		t.Fatalf("resolving an arm selected by a field behind the variant: %v", err)
	}

	if got := armsOf(t, records[0])[0].Predicate.Target.Name; got != "TAG" {
		t.Errorf("the arm names %s, want TAG", got)
	}
}

// TestANestedVariantsSelectorSitsInTheArmContainingIt is the one arm an arm's
// target may sit inside: the rule rules out the variant's own arms and any
// sibling variant's, and admits an arm enclosing it, where a copybook redefines
// a redefinition.
func TestANestedVariantsSelectorSitsInTheArmContainingIt(t *testing.T) {
	t.Parallel()

	records, err := resolveTable(t, nested, func(field *copybook.Field) []Redefine {
		return []Redefine{
			{
				Item: fieldNamed(t, field, "N-OUTER"),
				Alternatives: []Alternative{
					{Name: "N-OUTER", Predicate: selects("O", "N-ENTRY", "N-KIND")},
					{Name: "N-OUTER-ALT", Predicate: selects("A", "N-ENTRY", "N-KIND")},
				},
			},
			{
				Item: fieldNamed(t, field, "N-INNER"),
				Alternatives: []Alternative{
					// The selector sits inside N-OUTER, which is the arm
					// containing this variant.
					{Name: "N-INNER", Predicate: selects("I", "N-ENTRY", "N-OUTER", "N-INNER-KIND")},
					{Name: "N-INNER-ALT", Predicate: selects("J", "N-ENTRY", "N-OUTER", "N-INNER-KIND")},
				},
			},
		}
	})
	if err != nil {
		t.Fatalf("resolving a nested variant: %v", err)
	}

	variants := 0
	records[0].Walk(func(node *Node) {
		if node.Kind != KindVariant {
			return
		}

		variants++
		for _, arm := range node.Arms {
			if arm.Predicate == nil {
				t.Errorf("the arm %s is selected by nothing", arm.Alternative)
			}
		}
	})

	if variants != 2 {
		t.Errorf("the record holds %d variants, want the outer one and the one inside its arm", variants)
	}
}

// TestAnArmTargetInsideASiblingArmIsRejected: those bytes exist only where that
// alternative was selected, so reading them where it was not is reading one
// alternative's data as another's — the failure this document's whole treatment
// of REDEFINES is arranged around.
func TestAnArmTargetInsideASiblingArmIsRejected(t *testing.T) {
	t.Parallel()

	_, err := resolveTable(t, tableSource, func(field *copybook.Field) []Redefine {
		return []Redefine{{
			Item: fieldNamed(t, field, "BODY"),
			Alternatives: []Alternative{
				{Name: "BODY", Predicate: selects("B", "ENTRY", "SPLIT", "LEFT")},
				{Name: "SPLIT", Predicate: selects("S", "ENTRY", "CODE")},
			},
		}}
	})

	var scope *ArmTargetScopeError
	if !errors.As(err, &scope) {
		t.Fatalf("resolving reported %v, want an ArmTargetScopeError", err)
	}
	for _, want := range []string{"R", "ENTRY", "BODY", "SPLIT"} {
		if !strings.Contains(scope.Error(), want) {
			t.Errorf("the diagnostic does not name %s: %s", want, scope.Error())
		}
	}
}

// TestAnArmTargetOutsideTheRepeatingGroupIsRejected: it has the same bytes in
// every occurrence, so it selects the same arm in all of them — which is a
// choice made once per record, and a record node's rather than a variant's.
func TestAnArmTargetOutsideTheRepeatingGroupIsRejected(t *testing.T) {
	t.Parallel()

	_, err := resolveTable(t, tableSource, func(field *copybook.Field) []Redefine {
		return []Redefine{{
			Item: fieldNamed(t, field, "BODY"),
			Alternatives: []Alternative{
				{Name: "BODY", Predicate: selects("B", "HDR")},
				{Name: "SPLIT", Predicate: selects("S", "HDR")},
			},
		}}
	})

	var scope *ArmTargetScopeError
	if !errors.As(err, &scope) {
		t.Fatalf("resolving reported %v, want an ArmTargetScopeError", err)
	}
	if !strings.Contains(scope.Error(), "the same bytes in every entry") {
		t.Errorf("the diagnostic does not say why: %s", scope.Error())
	}
}

// TestTwoArmsWhosePredicatesOverlapAreRejected is "When two match, and when none
// does" at this scope. There are no guards on an arm to exempt a pair, so every
// pair is inside the rule — and the test is over the resolved bytes, so two
// spellings of one value are one value here as they are for two transitions.
func TestTwoArmsWhosePredicatesOverlapAreRejected(t *testing.T) {
	t.Parallel()

	spelledAsBytes := layoutmodel.Strategy{
		Kind:     layoutmodel.Equals,
		Item:     layoutmodel.ItemRef{Record: "R", Path: []string{"ENTRY", "CODE"}},
		Literals: []layoutmodel.Literal{{Kind: layoutmodel.BytesLiteral, Bytes: layoutmodel.ByteString{Bytes: []byte{0xC2}}}},
	}

	_, err := resolveTable(t, tableSource, func(field *copybook.Field) []Redefine {
		return []Redefine{{
			Item: fieldNamed(t, field, "BODY"),
			Alternatives: []Alternative{
				{Name: "BODY", Predicate: selects("B")},
				{Name: "SPLIT", Predicate: spelledAsBytes},
			},
		}}
	})

	var overlapping *ArmOverlapError
	if !errors.As(err, &overlapping) {
		t.Fatalf("resolving reported %v, want an ArmOverlapError", err)
	}
	for _, want := range []string{"R", "ENTRY", "BODY", "SPLIT"} {
		if !strings.Contains(overlapping.Error(), want) {
			t.Errorf("the diagnostic does not name %s: %s", want, overlapping.Error())
		}
	}
}

// TestAnOccurrenceMatchingNoArmIsReportedDistinctly, because the two failures
// send an adopter to different places: a record no transition matched is a
// record type the layout is missing, and an occurrence no arm matched is a
// record the layout does describe carrying an entry it does not.
func TestAnOccurrenceMatchingNoArmIsReportedDistinctly(t *testing.T) {
	t.Parallel()

	records, err := resolveTable(t, tableSource, func(field *copybook.Field) []Redefine {
		return []Redefine{{
			Item: fieldNamed(t, field, "BODY"),
			Alternatives: []Alternative{
				{Name: "BODY", Predicate: selects("B")},
				{Name: "SPLIT", Predicate: selects("S")},
			},
		}}
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	variant := variantIn(records[0].Root)
	if _, err := variant.SelectArm(func(*Predicate) []byte { return []byte("\xE9") }); err == nil {
		t.Fatal("an occurrence matching no arm selected one, want it reported")
	} else {
		var unmatched *UnmatchedOccurrenceError
		if !errors.As(err, &unmatched) {
			t.Fatalf("selecting reported %v, want an UnmatchedOccurrenceError", err)
		}

		var undescribed *UndescribedRecordError
		if errors.As(err, &undescribed) {
			t.Error("an unmatched occurrence reads as an undescribed record, and the two are different faults")
		}
	}

	// The arms are evaluated in the order the variant carries, and a consumer
	// may stop at the first that matches.
	arm, err := variant.SelectArm(func(*Predicate) []byte { return []byte("\xE2") })
	if err != nil {
		t.Fatalf("selecting the arm a matching occurrence names: %v", err)
	}
	if arm.Alternative != "SPLIT" {
		t.Errorf("the occurrence selected %s, want SPLIT", arm.Alternative)
	}
}
