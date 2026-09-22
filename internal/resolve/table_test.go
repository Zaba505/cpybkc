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

// "Does this item repeat?" is asked at seven places in this package, and at one
// declared maximum the answer is not the copybook's. These are that maximum
// (#373).
//
// `OCCURS 0 TO 1 TIMES DEPENDING ON` is a table under `odoslide` — the count
// read at run time says whether the group is in the record at all — and a fixed
// table of a single occurrence at a constant offset under `noodoslide`, which is
// an ordinary group. So every rule below is the same copybook asked twice, and
// the pair is what each test is about: a rule that fired under both readings
// would be refusing a record the second reading lays out at constant offsets,
// and one that fired under neither is the bug this file was written for.
//
// [repeats] is the one statement of the question and odo_test.go holds the
// repetition it produces. What is held here is every *other* site's use of it.

// optionalRedefine is the shape the whole of this file is about: a group that is
// present in a record or is absent from it, holding a REDEFINES.
//
// Under `odoslide` the REDEFINES is inside a table, so it is a variant chosen
// once per occurrence. Under `noodoslide` it is outside one, so it is two record
// types. There is no third answer, and nothing in the copybook picks between
// them.
const optionalRedefine = `01 R.
   05 N PIC 9(1).
   05 OPT OCCURS 0 TO 1 TIMES DEPENDING ON N.
      10 CODE PIC X(1).
      10 BODY PIC X(8).
      10 SPLIT REDEFINES BODY.
         15 LEFT PIC X(4).
         15 RIGHT PIC X(4).
   05 TRAILER PIC X(5).
`

// resolveReading resolves a copybook source under one stated reading, with the
// redefines the caller builds over its own copybook field.
//
// It reports what resolution said rather than failing on it, because half of
// these tests are about the fault.
func resolveReading(
	t *testing.T,
	src string,
	r layoutmodel.Reading,
	build func(*copybook.Field) []Redefine,
) ([]*Record, error) {
	t.Helper()

	field := recordOf(t, src)

	var redefines []Redefine
	if build != nil {
		redefines = build(field)
	}

	return Resolve(field, Options{
		Copybook:  "test.cpy",
		Dialect:   copybook.IBMEnterprise(),
		Encoding:  mainframe(),
		Reading:   r,
		Redefines: redefines,
	})
}

// bodyOrSplit is the layout's statement about optionalRedefine's cluster: two
// alternatives, each selected by the code byte of the occurrence in front of it.
func bodyOrSplit(t *testing.T) func(*copybook.Field) []Redefine {
	return func(field *copybook.Field) []Redefine {
		return []Redefine{{
			Item: fieldNamed(t, field, "BODY"),
			Alternatives: []Alternative{
				{Name: "BODY", Predicate: selects("B", "OPT", "CODE")},
				{Name: "SPLIT", Predicate: selects("S", "OPT", "CODE")},
			},
		}}
	}
}

// TestARedefineInsideAGroupWhoseDeclaredMaximumIsOneIsAVariant is the site the
// story is named for, and the shape it makes reachable for the first time: until
// #373 no variant node was ever built inside a table whose declared maximum is
// one, because the cluster was never seen to be inside a table at all.
func TestARedefineInsideAGroupWhoseDeclaredMaximumIsOneIsAVariant(t *testing.T) {
	t.Parallel()

	records, err := resolveReading(t, optionalRedefine, layoutmodel.ODOSlide, bodyOrSplit(t))
	if err != nil {
		t.Fatalf("resolving under odoslide: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("resolved to %d records, want 1: a variant is not two record types", len(records))
	}

	record := records[0]

	variant := variantIn(record.Root)
	if variant == nil {
		t.Fatal("the record holds no variant: the cluster was not seen to be inside a table")
	}
	if len(variant.Arms) != 2 {
		t.Fatalf("the variant has %d arms, want 2", len(variant.Arms))
	}
	if variant.Arms[0].Alternative != "BODY" || variant.Arms[1].Alternative != "SPLIT" {
		t.Errorf("the arms are %s and %s, want BODY and SPLIT",
			variant.Arms[0].Alternative, variant.Arms[1].Alternative)
	}

	// The variant sits inside the repetition #372 gave the group, and the two
	// facts are one fact: an arm chosen once per occurrence needs occurrences
	// to be chosen for.
	group := record.Find("OPT")
	if group == nil || group.Repetition == nil || !group.Repetition.Reference() {
		t.Fatalf("OPT carries the repetition %+v, want a count read out of the record", group.Repetition)
	}
	if variantIn(group) == nil {
		t.Error("the variant is not inside the table whose occurrence chooses it")
	}
}

// TestTheOtherReadingLeavesThatRedefineARecordTypePerAlternative is the same
// copybook and the same cluster under `noodoslide`, and it is why the site had
// to reach the reading rather than be scoped the way #372's guard was.
//
// A fixed table of one occurrence puts its items at constant positions, so every
// alternative of the REDEFINES is a description of one run of the *record* —
// which is a record type per alternative and no variant at all.
func TestTheOtherReadingLeavesThatRedefineARecordTypePerAlternative(t *testing.T) {
	t.Parallel()

	records, err := resolveReading(t, optionalRedefine, layoutmodel.NoODOSlide, bodyOrSplit(t))
	if err != nil {
		t.Fatalf("resolving under noodoslide: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("resolved to %d records, want 2: one per alternative", len(records))
	}

	for _, record := range records {
		if variant := variantIn(record.Root); variant != nil {
			t.Errorf("a record type carries a variant: %+v", variant)
		}
	}

	chosen := []string{records[0].Alternatives[0].Name, records[1].Alternatives[0].Name}
	if chosen[0] != "BODY" || chosen[1] != "SPLIT" {
		t.Errorf("the record types chose %v, want BODY then SPLIT", chosen)
	}
}

// TestAnUndiscriminatedRedefineInsideSuchAGroupNamesTheGroup is the second
// `resolve.go` site: which group the variant repeats inside is read the same way
// the cluster's own question is, so a diagnostic about it names the group the
// adopter has to go and look at.
func TestAnUndiscriminatedRedefineInsideSuchAGroupNamesTheGroup(t *testing.T) {
	t.Parallel()

	_, err := resolveReading(t, optionalRedefine, layoutmodel.ODOSlide, nil)

	var undiscriminated *UndiscriminatedRedefineError
	if !errors.As(err, &undiscriminated) {
		t.Fatalf("resolving reported %v, want an UndiscriminatedRedefineError", err)
	}
	if undiscriminated.Redefined != "BODY" || undiscriminated.Group != "OPT" {
		t.Errorf("the fault is about %s in %s, want BODY in OPT",
			undiscriminated.Redefined, undiscriminated.Group)
	}
}

// TestACountInsideAGroupWhoseDeclaredMaximumIsOneIsRefused is `odo.go`'s
// `checkCounts`, and it is the reachable hazard the story opens with: a count
// with one value per occurrence of the group it sits in is a group whose
// occurrences are not all the same width, and the extent sum stops being
// arithmetic.
//
// The declared maximum of the *enclosing* group is what is varied, because at
// two this was already refused and at one it was accepted — one rule, read two
// ways, on copybooks that differ in a single character.
func TestACountInsideAGroupWhoseDeclaredMaximumIsOneIsRefused(t *testing.T) {
	t.Parallel()

	// The story's own copybook: a count, a group sized by it, a count inside
	// *that* group, and a table sized by the inner count.
	source := func(maximum string) string {
		return `01 R.
   05 N PIC 9(1).
   05 G OCCURS 0 TO ` + maximum + ` TIMES DEPENDING ON N.
      10 M PIC 9(1).
   05 T OCCURS 1 TO 4 TIMES DEPENDING ON M PIC X(2).
`
	}

	for _, maximum := range []string{"1", "2"} {
		t.Run("a declared maximum of "+maximum, func(t *testing.T) {
			t.Parallel()

			_, err := resolveReading(t, source(maximum), layoutmodel.ODOSlide, nil)

			var occurrence *CountOccurrenceError
			if !errors.As(err, &occurrence) {
				t.Fatalf("resolving reported %v, want a CountOccurrenceError", err)
			}
			if occurrence.Count != "M" || occurrence.Table != "T" || occurrence.Group != "G" {
				t.Errorf("the fault names count %q, table %q and group %q, want M, T and G",
					occurrence.Count, occurrence.Table, occurrence.Group)
			}
		})
	}
}

// TestTheOtherReadingCarriesNoCountForThatRuleToBind is the pair of the rule
// above, and it holds the scoping rather than the rule.
//
// `checkCounts` runs under the sliding reading alone, because a non-sliding
// record carries no count reference for any of it to bind: M is a field beside
// two fixed tables and G is a group at a constant offset. Nothing about that
// copybook is wrong once the file was written by a compiler that reads it that
// way.
func TestTheOtherReadingCarriesNoCountForThatRuleToBind(t *testing.T) {
	t.Parallel()

	records, err := resolveReading(t, `01 R.
   05 N PIC 9(1).
   05 G OCCURS 0 TO 1 TIMES DEPENDING ON N.
      10 M PIC 9(1).
   05 T OCCURS 1 TO 4 TIMES DEPENDING ON M PIC X(2).
`, layoutmodel.NoODOSlide, nil)
	if err != nil {
		t.Fatalf("resolving under noodoslide: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("resolved to %d records, want 1", len(records))
	}

	// 1 + 1 + 4 × 2: both tables stand at their declared maximum, and the
	// record is the one width a fixed-length dataset requires of it.
	if got, want := records[0].Extent(), 1+1+4*2; got != want {
		t.Errorf("the record is %d bytes, want a constant %d", got, want)
	}
}

// discriminated is a record whose type code is outside the optional group and
// whose key is inside it, for the two sites that name an item of a record.
const discriminated = `01 D-REC.
   05 D-TYPE PIC X(1).
   05 D-COUNT PIC 9(1).
   05 D-OPT OCCURS 0 TO 1 TIMES DEPENDING ON D-COUNT.
      10 D-KEY PIC X(3).
`

// compileUnder compiles a layout under one stated reading, reporting what
// compilation said rather than failing on it.
func compileUnder(
	t *testing.T,
	source string,
	copybooks map[string]string,
	r layoutmodel.Reading,
) (*Automaton, error) {
	t.Helper()

	opts := sequencingOf(t, source, copybooks)
	opts.Reading = r

	return CompileSequence(opts)
}

// TestADiscriminatorTargetInsideSuchAGroupIsRefusedUnderTheSlidingReading is
// `discriminator.go`'s target check: docs/ir/SPEC.md's "A reference names a
// field, not an occurrence of one", asked of the item a predicate reads.
//
// Under `odoslide` D-KEY is one occurrence of a table that a record may carry no
// occurrence of at all, so a predicate over it resolves against bytes the record
// need not have — which is the hazard docs/ir/SPEC.md states for a discriminator
// target in as many words, and it is the reason this site could not be left
// reading the declared maximum.
func TestADiscriminatorTargetInsideSuchAGroupIsRefusedUnderTheSlidingReading(t *testing.T) {
	t.Parallel()

	source := `(record ONLY (copybook "d.cpy" D-REC))
(discriminate ONLY (equals (item ONLY D-OPT D-KEY) "ABC"))
(sequence (* ONLY))`

	_, err := compileUnder(t, source, map[string]string{"ONLY": discriminated}, layoutmodel.ODOSlide)

	var occurrence *PredicateOccurrenceError
	if !errors.As(err, &occurrence) {
		t.Fatalf("compiling reported %v, want a PredicateOccurrenceError", err)
	}
	if occurrence.Item != "D-KEY" || occurrence.Group != "D-OPT" {
		t.Errorf("the fault names item %q in group %q, want D-KEY in D-OPT",
			occurrence.Item, occurrence.Group)
	}
}

// TestThatSameTargetIsAdmittedUnderTheOtherReading is the pair: under
// `noodoslide` the group is a fixed table of one occurrence, D-KEY is at byte
// two of every record of that type, and there is nothing for the rule to refuse.
func TestThatSameTargetIsAdmittedUnderTheOtherReading(t *testing.T) {
	t.Parallel()

	source := `(record ONLY (copybook "d.cpy" D-REC))
(discriminate ONLY (equals (item ONLY D-OPT D-KEY) "ABC"))
(sequence (* ONLY))`

	a, err := compileUnder(t, source, map[string]string{"ONLY": discriminated}, layoutmodel.NoODOSlide)
	if err != nil {
		t.Fatalf("compiling under noodoslide: %v", err)
	}

	predicate := predicateOf(t, a)
	if predicate == nil || predicate.Target.Name != "D-KEY" {
		t.Fatalf("the transition is selected by %v, want D-KEY", predicate)
	}
}

// TestASequenceBindingInsideSuchAGroupIsRefusedUnderTheSlidingReading is
// `sequence.go`'s binding target, which asks the same question of the item an
// operator reads a value from: nothing in a sequencing expression carries an
// occurrence number, so an item with a value per occurrence has no spelling.
func TestASequenceBindingInsideSuchAGroupIsRefusedUnderTheSlidingReading(t *testing.T) {
	t.Parallel()

	const counted = `01 HDR-REC.
   05 HDR-TYPE PIC X(1).
   05 HDR-FLAG PIC 9(1).
   05 HDR-OPT OCCURS 0 TO 1 TIMES DEPENDING ON HDR-FLAG.
      10 DTL-COUNT PIC 9(2).
`

	source := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(sequence (seq HEADER (times DETAIL (item HEADER HDR-OPT DTL-COUNT))))`

	copybooks := map[string]string{"HEADER": counted, "DETAIL": detail}

	_, err := compileUnder(t, source, copybooks, layoutmodel.ODOSlide)

	var occurrence *SequenceOccurrenceError
	if !errors.As(err, &occurrence) {
		t.Fatalf("compiling reported %v, want a SequenceOccurrenceError", err)
	}
	if occurrence.Item != "DTL-COUNT" || occurrence.Group != "HDR-OPT" {
		t.Errorf("the fault names item %q in group %q, want DTL-COUNT in HDR-OPT",
			occurrence.Item, occurrence.Group)
	}

	// And the pair, in the same test because it is one copybook and one
	// expression: under the other reading the count is at byte two of every
	// header, which is a value the automaton can bind.
	if _, err := compileUnder(t, source, copybooks, layoutmodel.NoODOSlide); err != nil {
		t.Fatalf("compiling under noodoslide: %v", err)
	}
}

// TestNameableAnswersUnderTheReading is `discriminator.go`'s `nameable`, the
// site that decides whether a record offers a field a predicate could name at
// all.
//
// The first case is the shape an adopter actually writes, and what it pins is a
// consequence worth writing down rather than a change: a record carrying an
// `OCCURS DEPENDING ON` always has its count outside every table — a count that
// did not would be `checkCounts`' fault — so such a record offers that field
// whichever reading is stated, and this site cannot refuse a record over the
// construct the story is about.
//
// The second is what actually reaches the reading, and it is asked of [nameable]
// directly because there is no copybook `Resolve` admits in which every
// elementary item sits inside a table. `Resolve` refuses this one for where the
// count is; `nameable` is asked before any of that, and it has to answer the
// reading it is running under rather than the copybook's declared maximum.
func TestNameableAnswersUnderTheReading(t *testing.T) {
	t.Parallel()

	const counted = `01 U-REC.
   05 U-COUNT PIC 9(1).
   05 U-OPT OCCURS 0 TO 1 TIMES DEPENDING ON U-COUNT.
      10 U-BODY PIC X(4).
`

	const everythingInATable = `01 V-REC.
   05 V-ENTRY OCCURS 5 TIMES.
      10 V-COUNT PIC 9(1).
   05 V-OPT OCCURS 0 TO 1 TIMES DEPENDING ON V-COUNT.
      10 V-BODY PIC X(4).
`

	for name, test := range map[string]struct {
		copybook string
		reading  layoutmodel.Reading
		want     bool
	}{
		"the count is outside the group, read odoslide": {
			copybook: counted, reading: layoutmodel.ODOSlide, want: true,
		},
		"the count is outside the group, read noodoslide": {
			copybook: counted, reading: layoutmodel.NoODOSlide, want: true,
		},
		"every elementary item is inside a table, read odoslide": {
			copybook: everythingInATable, reading: layoutmodel.ODOSlide, want: false,
		},
		"every elementary item is inside a table, read noodoslide": {
			copybook: everythingInATable, reading: layoutmodel.NoODOSlide, want: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := nameable(layoutOf(t, test.copybook), test.reading); got != test.want {
				t.Errorf("the record offers a target: %v, want %v", got, test.want)
			}
		})
	}
}

// TestDescribeAnswersHowManyRecordTypesUnderTheReading is `shape.go`, and it is
// the site with the largest blast radius of the seven: it is what decides how
// many record types a copybook produces, and therefore how many `record` forms a
// layout owes and how many names reach a generated API.
func TestDescribeAnswersHowManyRecordTypesUnderTheReading(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		reading      layoutmodel.Reading
		combinations int
		inTable      bool
	}{
		"odoslide":   {reading: layoutmodel.ODOSlide, combinations: 1, inTable: true},
		"noodoslide": {reading: layoutmodel.NoODOSlide, combinations: 2, inTable: false},

		// A caller holding no reading gets the answer
		// [layoutmodel.Reading.Slides] documents as the safe one — a fixed
		// table — which is `noodoslide`'s. It is not a default taken silently:
		// it arrives flagged, below.
		"unstated": {reading: layoutmodel.ReadingUnstated, combinations: 2, inTable: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			shape := describeUnder(t, optionalRedefine, test.reading)

			if len(shape.Combinations) != test.combinations {
				t.Errorf("the copybook produces %d record types, want %d",
					len(shape.Combinations), test.combinations)
			}

			if len(shape.Alternations) != 1 {
				t.Fatalf("the copybook carries %d alternations, want 1", len(shape.Alternations))
			}

			alternation := shape.Alternations[0]

			if alternation.InTable != test.inTable {
				t.Errorf("the run stands inside a table: %v, want %v",
					alternation.InTable, test.inTable)
			}

			// The flag is the reading-dependence itself, so it is the same
			// under all three: it says the two readings disagree, not which
			// one was asked.
			if !alternation.ReadingDecides {
				t.Error("the alternation does not report that the reading decided it")
			}
			if alternation.Table == nil || alternation.Table.Name != "OPT" {
				t.Errorf("the alternation names the group %v, want OPT", alternation.Table)
			}
		})
	}
}

// TestDescribeLeavesAnOrdinaryTableAloneUnderBothReadings is the control the
// flag above needs: a declared maximum of one is the only place the two readings
// disagree about what a table is, so a table declared above one carries no flag
// and answers the same way twice.
func TestDescribeLeavesAnOrdinaryTableAloneUnderBothReadings(t *testing.T) {
	t.Parallel()

	const wider = `01 R.
   05 N PIC 9(1).
   05 OPT OCCURS 0 TO 4 TIMES DEPENDING ON N.
      10 BODY PIC X(8).
      10 SPLIT REDEFINES BODY PIC X(8).
   05 TRAILER PIC X(5).
`

	for _, r := range []layoutmodel.Reading{
		layoutmodel.ODOSlide,
		layoutmodel.NoODOSlide,
		layoutmodel.ReadingUnstated,
	} {
		shape := describeUnder(t, wider, r)

		if len(shape.Combinations) != 1 {
			t.Errorf("under %s the copybook produces %d record types, want 1",
				r, len(shape.Combinations))
		}
		if len(shape.Alternations) != 1 {
			t.Fatalf("under %s the copybook carries %d alternations, want 1", r, len(shape.Alternations))
		}
		if alternation := shape.Alternations[0]; !alternation.InTable || alternation.ReadingDecides {
			t.Errorf("under %s the alternation is inTable=%v readingDecides=%v, want true and false",
				r, alternation.InTable, alternation.ReadingDecides)
		}
	}
}

// TestAScheduledVariantInsideSuchAGroupIsRefusedForWantOfOccurrences states the
// one question the newly reachable shape leaves open, rather than leaving the
// coverage check to answer it by accident.
//
// A variant chosen by the *position* of an occurrence is held to covering
// 1..*M* exactly once, for *M* the table's declared maximum. At *M* = 1 there is
// one occurrence to cover and a variant needs two arms, so one of the two is
// always either scheduling an occurrence the table has not got or scheduling the
// one occurrence a sibling already took. There is no assignment of two arms over
// one occurrence that covers it exactly once, so the form is refused here for
// every layout that writes it — which is a statement about the shape and not
// about the particular schedule a test happened to pick.
//
// A variant chosen by the bytes of an occurrence is the shape that *is*
// admissible, and it is the one the rest of this file resolves.
func TestAScheduledVariantInsideSuchAGroupIsRefusedForWantOfOccurrences(t *testing.T) {
	t.Parallel()

	scheduled := func(first, second []int64) func(*copybook.Field) []Redefine {
		return func(field *copybook.Field) []Redefine {
			return []Redefine{{
				Item: fieldNamed(t, field, "BODY"),
				Alternatives: []Alternative{
					{Name: "BODY", Schedule: &Schedule{Occurrences: first}},
					{Name: "SPLIT", Schedule: &Schedule{Occurrences: second}},
				},
			}}
		}
	}

	for name, build := range map[string]func(*copybook.Field) []Redefine{
		"the second arm names an occurrence the table has not got": scheduled([]int64{1}, []int64{2}),
		"both arms name the one occurrence there is":               scheduled([]int64{1}, []int64{1}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := resolveReading(t, optionalRedefine, layoutmodel.ODOSlide, build)
			if err == nil {
				t.Fatal("a scheduled variant over a table of one occurrence resolved")
			}

			var (
				outOfRange *ScheduleRangeError
				overlap    *ScheduleOverlapError
			)
			if !errors.As(err, &outOfRange) && !errors.As(err, &overlap) {
				t.Fatalf("resolving reported %v, want the schedule refused over the table's one occurrence", err)
			}
			if !strings.Contains(err.Error(), "OPT") {
				t.Errorf("the diagnostic does not name the group: %s", err)
			}
		})
	}
}
