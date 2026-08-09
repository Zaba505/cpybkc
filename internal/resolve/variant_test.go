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

// A REDEFINES inside a repeating group is chosen once per *occurrence*, and
// there is no record per occurrence for it to become. So it resolves to a
// variant, and every rule below is there to keep that variant a constant term of
// the position sum — which is what lets the rest of docs/ir/SPEC.md not move for
// it.

// tableSource is one repeating group holding a redefine and a field behind it,
// which is the smallest copybook the whole of this file is about.
const tableSource = `01 R.
   05 HDR PIC X(3).
   05 ENTRY OCCURS 4 TIMES.
      10 CODE PIC X.
      10 BODY PIC X(8).
      10 SPLIT REDEFINES BODY.
         15 LEFT PIC X(4).
         15 RIGHT PIC X(4).
      10 TAG PIC X(2).
   05 TRAILER PIC X(5).
`

// resolveTable resolves a copybook with the redefines the caller builds over
// its own copybook field, which is the two-step a layout takes: the item
// reference is resolved against the copybook, then the copybook is resolved.
func resolveTable(t *testing.T, src string, build func(*copybook.Field) []Redefine) ([]*Record, error) {
	t.Helper()

	field := recordOf(t, src)
	return Resolve(field, Options{
		Copybook:  "test.cpy",
		Dialect:   copybook.IBMEnterprise(),
		Encoding:  mainframe(),
		Reading:   layoutmodel.ODOSlide,
		Redefines: build(field),
	})
}

// TestARedefineInsideATableBecomesAVariant is the resolution: one node, two
// arms, each naming what selects it and the item that is its body.
func TestARedefineInsideATableBecomesAVariant(t *testing.T) {
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
	if len(records) != 1 {
		t.Fatalf("resolved to %d records, want 1: a variant is not two record types", len(records))
	}

	record := records[0]
	variant := variantIn(record.Root)
	if variant == nil {
		t.Fatal("the record holds no variant")
	}
	if len(variant.Arms) != 2 {
		t.Fatalf("the variant has %d arms, want 2", len(variant.Arms))
	}
	if variant.Arms[0].Alternative != "BODY" || variant.Arms[1].Alternative != "SPLIT" {
		t.Fatalf("the arms are %s and %s, want BODY and SPLIT",
			variant.Arms[0].Alternative, variant.Arms[1].Alternative)
	}
	for _, arm := range variant.Arms {
		if !arm.Predicate.Predicate() {
			t.Errorf("the arm %s is selected by nothing", arm.Alternative)
		}
		if got := arm.Body.Extent(); got != 8 {
			t.Errorf("the arm %s covers %d bytes, want the variant's 8", arm.Alternative, got)
		}
	}
}

// TestAVariantCarriesNoWidthNoNamesAndNoRepetition holds the node to what
// docs/ir/SPEC.md says it carries and nothing else: the width is the arms'
// common extent, the alternation has no name in the copybook, and it repeats by
// sitting inside the group that does.
func TestAVariantCarriesNoWidthNoNamesAndNoRepetition(t *testing.T) {
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
	if variant.width != 0 {
		t.Errorf("the variant carries a width of %d", variant.width)
	}
	if variant.Field != nil {
		t.Errorf("the variant carries the name %s", variant.Field.Name)
	}
	if variant.Repetition != nil {
		t.Error("the variant carries a repetition of its own")
	}
	if got := variant.Width(); got != 8 {
		t.Errorf("the variant's width is %d, want the arms' common extent of 8", got)
	}
}

// TestAVariantSitsOnlyInsideAGroupThatRepeats is the placement rule read the
// other way: the same copybook with the OCCURS taken off resolves to record
// types and not to a variant, whatever the layout says about it.
func TestAVariantSitsOnlyInsideAGroupThatRepeats(t *testing.T) {
	t.Parallel()

	src := strings.Replace(tableSource, "ENTRY OCCURS 4 TIMES.", "ENTRY.", 1)
	records, err := resolveTable(t, src, func(field *copybook.Field) []Redefine {
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
	if len(records) != 2 {
		t.Fatalf("resolved to %d records, want one per alternative", len(records))
	}
	for _, record := range records {
		if variantIn(record.Root) != nil {
			t.Error("a redefine outside a repeating group became a variant")
		}
	}
}

// TestAVariantIsAConstantTermOfThePositionSum is the requirement doing all the
// work: with the variant in place every item behind it sits where it sat, and
// every occurrence of the enclosing group is the width it was.
//
// It is asserted against the same copybook with the redefine deleted, so the
// comparison is with a record that never had an alternation rather than with
// numbers a test wrote down.
func TestAVariantIsAConstantTermOfThePositionSum(t *testing.T) {
	t.Parallel()

	withVariant, err := resolveTable(t, tableSource, func(field *copybook.Field) []Redefine {
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

	plain := resolveOne(t, `01 R.
   05 HDR PIC X(3).
   05 ENTRY OCCURS 4 TIMES.
      10 CODE PIC X.
      10 BODY PIC X(8).
      10 TAG PIC X(2).
   05 TRAILER PIC X(5).
`)

	for _, name := range []string{"HDR", "ENTRY", "CODE", "TAG", "TRAILER"} {
		got, want := positionOf(t, withVariant[0], name), positionOf(t, plain, name)
		if got != want {
			t.Errorf("%s is at %d with a variant and %d without one", name, got, want)
		}
	}

	entry, plainEntry := withVariant[0].Find("ENTRY"), plain.Find("ENTRY")
	if entry.Width() != plainEntry.Width() {
		t.Errorf("an occurrence is %d bytes with a variant and %d without one",
			entry.Width(), plainEntry.Width())
	}
	if withVariant[0].Extent() != plain.Extent() {
		t.Errorf("the record is %d bytes with a variant and %d without one",
			withVariant[0].Extent(), plain.Extent())
	}
}

// TestAShorterArmCarriesTheSlackItsItemsDoNotOccupy is the record-level rule one
// level down, and the slack sits inside the arm: a run stops at the edge of one,
// so bytes at the end of an arm and bytes behind the variant are two nodes
// however they meet.
func TestAShorterArmCarriesTheSlackItsItemsDoNotOccupy(t *testing.T) {
	t.Parallel()

	src := `01 R.
   05 ENTRY OCCURS 3 TIMES.
      10 WIDE PIC X(10).
      10 NARROW REDEFINES WIDE PIC X(4).
      10 TAG PIC X(2).
`
	records, err := resolveTable(t, src, func(field *copybook.Field) []Redefine {
		return []Redefine{{
			Item: fieldNamed(t, field, "WIDE"),
			Alternatives: []Alternative{
				{Name: "WIDE", Predicate: selects("W")},
				{Name: "NARROW", Predicate: selects("N")},
			},
		}}
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	variant := variantIn(records[0].Root)
	wide, narrow := variant.Arms[0], variant.Arms[1]

	if got := slackWidths(wide.Body); len(got) != 0 {
		t.Errorf("the arm that fills the extent carries slack of %v", got)
	}
	if got := slackWidths(narrow.Body); len(got) != 1 || got[0] != 6 {
		t.Fatalf("the shorter arm's slack is %v, want one run of 6", got)
	}
	if narrow.Body.Kind != KindGroup {
		t.Fatalf("the shorter arm's body is a %s, and an elementary item has nowhere to put slack", narrow.Body.Kind)
	}
	if narrow.Body.Field != nil {
		t.Errorf("the group holding the shorter arm carries the name %s", narrow.Body.Field.Name)
	}
	if got := narrow.Body.Extent(); got != 10 {
		t.Errorf("the shorter arm covers %d bytes, want the variant's 10", got)
	}
	if got := positionOf(t, records[0], "TAG"); got != 10 {
		t.Errorf("the item behind the variant is at %d, want 10", got)
	}
}

// TestARedefineTakingOneAlternativeEveryOccurrenceEmitsNoVariant is the overlay
// an adopter wrote to read one item two ways: the layout names one alternative,
// and it reaches the resolved record as that alternative's items.
func TestARedefineTakingOneAlternativeEveryOccurrenceEmitsNoVariant(t *testing.T) {
	t.Parallel()

	records, err := resolveTable(t, tableSource, func(field *copybook.Field) []Redefine {
		return []Redefine{{
			Item:         fieldNamed(t, field, "BODY"),
			Alternatives: []Alternative{{Name: "SPLIT"}},
		}}
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	record := records[0]
	if variantIn(record.Root) != nil {
		t.Fatal("a redefine every occurrence of which takes one alternative became a variant")
	}
	if record.Find("BODY") != nil {
		t.Error("the alternative the layout did not name reached the record")
	}
	if got := positionOf(t, record, "LEFT"); got != 4 {
		t.Errorf("LEFT is at %d, want 4", got)
	}
	if got := positionOf(t, record, "TAG"); got != 12 {
		t.Errorf("TAG is at %d, want 12", got)
	}
}

// TestAVariantIsRejectedWhenItsAlternativesCannotBeMadeToAgree is the diagnostic
// docs/ir/SPEC.md asks for by name: an arm that needs more bytes than the item
// it redefines, reported with the record, the repeating group, the redefined
// item and the arm rather than as a generic width error.
func TestAVariantIsRejectedWhenItsAlternativesCannotBeMadeToAgree(t *testing.T) {
	t.Parallel()

	src := `01 R.
   05 ENTRY OCCURS 3 TIMES.
      10 SHORT PIC X(4).
      10 LONG REDEFINES SHORT PIC X(9).
      10 TAG PIC X(2).
`
	field := recordOf(t, src)
	_, err := Resolve(field, Options{
		Copybook: "test.cpy",
		// A lenient dialect is what lets an oversized redefinition
		// reach this package at all; a strict one refuses it while the
		// copybook is being laid out.
		Dialect:  lenient(),
		Encoding: mainframe(),
		Redefines: []Redefine{{
			Item: fieldNamed(t, field, "SHORT"),
			Alternatives: []Alternative{
				{Name: "SHORT", Predicate: selects("S")},
				{Name: "LONG", Predicate: selects("L")},
			},
		}},
	})

	var extent *ArmExtentError
	if !errors.As(err, &extent) {
		t.Fatalf("resolving reported %v, want an ArmExtentError", err)
	}
	for _, want := range []string{"R", "ENTRY", "SHORT", "LONG"} {
		if !strings.Contains(extent.Error(), want) {
			t.Errorf("the diagnostic does not name %s: %s", want, extent.Error())
		}
	}
	if extent.Extent != 9 || extent.Want != 4 {
		t.Errorf("the diagnostic says %d bytes against %d, want 9 against 4", extent.Extent, extent.Want)
	}
}

// TestAVariantIsRejectedWhenAnArmsExtentMovesWithData is the other half of "how
// many is a constant": an OCCURS DEPENDING ON inside an arm has no constant for
// the arms to agree on.
func TestAVariantIsRejectedWhenAnArmsExtentMovesWithData(t *testing.T) {
	t.Parallel()

	src := `01 R.
   05 N PIC 9(2).
   05 ENTRY OCCURS 3 TIMES.
      10 FIXED PIC X(20).
      10 VARYING REDEFINES FIXED.
         15 CELL PIC X(4) OCCURS 1 TO 5 TIMES DEPENDING ON N.
      10 TAG PIC X(2).
`
	_, err := resolveTable(t, src, func(field *copybook.Field) []Redefine {
		return []Redefine{{
			Item: fieldNamed(t, field, "FIXED"),
			Alternatives: []Alternative{
				{Name: "FIXED", Predicate: selects("F")},
				{Name: "VARYING", Predicate: selects("V")},
			},
		}}
	})

	var counted *ArmVariableCountError
	if !errors.As(err, &counted) {
		t.Fatalf("resolving reported %v, want an ArmVariableCountError", err)
	}
	for _, want := range []string{"R", "ENTRY", "FIXED", "VARYING", "CELL", "N"} {
		if !strings.Contains(counted.Error(), want) {
			t.Errorf("the diagnostic does not name %s: %s", want, counted.Error())
		}
	}
}

// TestAnArmSelectedByNothingIsRejected is the rule that there is no default arm:
// one selected by nothing would match every occurrence, which leaves it the only
// arm, which is the single-alternative redefine and not a variant.
func TestAnArmSelectedByNothingIsRejected(t *testing.T) {
	t.Parallel()

	_, err := resolveTable(t, tableSource, func(field *copybook.Field) []Redefine {
		return []Redefine{{
			Item: fieldNamed(t, field, "BODY"),
			Alternatives: []Alternative{
				{Name: "BODY", Predicate: selects("B")},
				{Name: "SPLIT"},
			},
		}}
	})

	var predicate *ArmPredicateError
	if !errors.As(err, &predicate) {
		t.Fatalf("resolving reported %v, want an ArmPredicateError", err)
	}
	if predicate.Arm != "SPLIT" {
		t.Errorf("the diagnostic names %s, want SPLIT", predicate.Arm)
	}
}

// TestASingleRecordTypeStrategyIsNotAPredicate is the same rule reached the
// other way: the one strategy that lowers into the *absence* of a predicate
// cannot select an arm.
func TestASingleRecordTypeStrategyIsNotAPredicate(t *testing.T) {
	t.Parallel()

	_, err := resolveTable(t, tableSource, func(field *copybook.Field) []Redefine {
		return []Redefine{{
			Item: fieldNamed(t, field, "BODY"),
			Alternatives: []Alternative{
				{Name: "BODY", Predicate: selects("B")},
				{Name: "SPLIT", Predicate: layoutmodel.Strategy{Kind: layoutmodel.SingleRecordType}},
			},
		}}
	})

	var predicate *ArmPredicateError
	if !errors.As(err, &predicate) {
		t.Fatalf("resolving reported %v, want an ArmPredicateError", err)
	}
}

// TestARedefineInsideATableTheLayoutSaysNothingAboutIsRejected refuses the
// default. Reading the copybook's own first alternative would be a record read
// as the wrong alternative in every entry of the table, with nothing in the file
// to disagree with it.
func TestARedefineInsideATableTheLayoutSaysNothingAboutIsRejected(t *testing.T) {
	t.Parallel()

	_, err := resolveTable(t, tableSource, func(*copybook.Field) []Redefine { return nil })

	var undiscriminated *UndiscriminatedRedefineError
	if !errors.As(err, &undiscriminated) {
		t.Fatalf("resolving reported %v, want an UndiscriminatedRedefineError", err)
	}
	for _, want := range []string{"R", "ENTRY", "BODY", "SPLIT"} {
		if !strings.Contains(undiscriminated.Error(), want) {
			t.Errorf("the diagnostic does not name %s: %s", want, undiscriminated.Error())
		}
	}
}

// TestAnAlternativeTheCopybookDoesNotDeclareIsRejected, with the ones it does
// declare in the message, because the list an adopter needs is the list of the
// ones they could have meant.
func TestAnAlternativeTheCopybookDoesNotDeclareIsRejected(t *testing.T) {
	t.Parallel()

	_, err := resolveTable(t, tableSource, func(field *copybook.Field) []Redefine {
		return []Redefine{{
			Item: fieldNamed(t, field, "BODY"),
			Alternatives: []Alternative{
				{Name: "BODY", Predicate: selects("B")},
				{Name: "HALVES", Predicate: selects("H")},
			},
		}}
	})

	var unknown *UnknownAlternativeError
	if !errors.As(err, &unknown) {
		t.Fatalf("resolving reported %v, want an UnknownAlternativeError", err)
	}
	if !strings.Contains(unknown.Error(), "SPLIT") {
		t.Errorf("the diagnostic does not list the alternatives that do exist: %s", unknown.Error())
	}
}

// TestARedefineTheLayoutNamesNoAlternativeOfIsRejected, and the message says
// what naming one and naming two would each have meant — naming one is the
// overlay above and not a fault, so a diagnostic that said "a variant needs two"
// would send an adopter to write a predicate they may not need.
func TestARedefineTheLayoutNamesNoAlternativeOfIsRejected(t *testing.T) {
	t.Parallel()

	_, err := resolveTable(t, tableSource, func(field *copybook.Field) []Redefine {
		return []Redefine{{Item: fieldNamed(t, field, "BODY")}}
	})

	var count *ArmCountError
	if !errors.As(err, &count) {
		t.Fatalf("resolving reported %v, want an ArmCountError", err)
	}
	if !strings.Contains(count.Error(), "name one to say every occurrence takes it") {
		t.Errorf("the diagnostic does not say that naming one alternative is allowed: %s", count.Error())
	}
}

// TestEveryFaultIsReported holds this package to the repository's rule that a
// reader reports every fault it found rather than the first: a copybook and the
// layout over it go wrong in the same way in several places at once.
func TestEveryFaultIsReported(t *testing.T) {
	t.Parallel()

	src := `01 R.
   05 ENTRY OCCURS 2 TIMES.
      10 A PIC X(4).
      10 B REDEFINES A PIC X(4).
      10 C PIC X(6).
      10 D REDEFINES C PIC X(6).
`
	_, err := resolveTable(t, src, func(*copybook.Field) []Redefine { return nil })

	if got := len(strings.Split(strings.TrimSpace(err.Error()), "\n")); got != 2 {
		t.Fatalf("resolving reported %d faults, want both: %v", got, err)
	}
}

// lenient is a dialect that lets a redefinition be larger than what it
// redefines, which is what a compiler configured to downgrade that diagnostic to
// a warning does.
func lenient() copybook.Dialect {
	dialect := copybook.IBMEnterprise()
	dialect.Redefines = copybook.RedefinesLenient
	return dialect
}

// variantIn is the first variant node of a subtree, or nil.
func variantIn(node *Node) *Node {
	if node.Kind == KindVariant {
		return node
	}
	for _, member := range node.Members {
		if found := variantIn(member); found != nil {
			return found
		}
	}
	for _, arm := range node.Arms {
		if found := variantIn(arm.Body); found != nil {
			return found
		}
	}
	return nil
}
