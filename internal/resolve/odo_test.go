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

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// An `OCCURS DEPENDING ON` table is the one thing in a record whose extent is
// not a number the copybook states, and docs/ir/SPEC.md's "A variable record is
// a sum with a variable term" is what these hold this package to: the extent is
// the width of one occurrence times a count read out of the record, and the item
// behind it begins at the byte after the last occurrence that count states.
//
// Under the *other* reading of the same clause there is no variable table at all
// — "An item after a table slides, and the other reading is a fixed table" — so
// half of what follows is the same copybook resolved twice.

// varying is the shape every test here starts from: a count, a table sized by
// it, and an item behind the table for the count to move.
const varying = `01 R.
   05 N PIC 9(2).
   05 ENTRY OCCURS 1 TO 4 TIMES DEPENDING ON N PIC X(5).
   05 TRAILER PIC X(3).
`

// reading resolves a copybook source under one of the two readings, reporting
// what resolution said rather than failing on it, because half these tests are
// about the fault.
func reading(t *testing.T, src string, r layoutmodel.Reading) ([]*Record, error) {
	t.Helper()

	return Resolve(recordOf(t, src), Options{
		Copybook: "test.cpy",
		Dialect:  copybook.IBMEnterprise(),
		Encoding: mainframe(),
		Reading:  r,
	})
}

// slid resolves a copybook the caller expects to resolve under the sliding
// reading, which is IBM Enterprise COBOL's and unconditional there.
func slid(t *testing.T, src string) *Record {
	t.Helper()

	records, err := reading(t, src, layoutmodel.ODOSlide)
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("resolved to %d records, want 1", len(records))
	}

	return records[0]
}

// countOf is the field a repetition's count is read from, which is what a test
// keys [Counts] by.
func countOf(t *testing.T, record *Record, table string) *copybook.Field {
	t.Helper()

	node := record.Find(table)
	if node == nil || node.Repetition == nil || node.Repetition.DependingOn == nil {
		t.Fatalf("%s carries no count reference", table)
	}

	return node.Repetition.DependingOn
}

// TestASlidingTableCarriesTheReferenceAndTheCopybooksOwnBounds is the resolution
// under `odoslide`: the count is the field the DEPENDING ON phrase names, and
// the bounds are the copybook's `OCCURS integer-1 TO integer-2` unchanged.
func TestASlidingTableCarriesTheReferenceAndTheCopybooksOwnBounds(t *testing.T) {
	t.Parallel()

	record := slid(t, varying)

	repetition := record.Find("ENTRY").Repetition
	if repetition == nil || !repetition.Reference() {
		t.Fatalf("the table's repetition is %+v, want a count read out of the record", repetition)
	}
	if repetition.DependingOn.Name != "N" {
		t.Errorf("the count is read from %s, want N", repetition.DependingOn.Name)
	}
	if repetition.Min != 1 || repetition.Max != 4 {
		t.Errorf("the declared bounds are %d to %d, want the copybook's 1 to 4", repetition.Min, repetition.Max)
	}

	// The count the record stands at as it leaves resolution is the storage a
	// compiler reserves, which is the declared maximum.
	if repetition.Count != 4 {
		t.Errorf("the table stands at %d occurrences, want the declared maximum of 4", repetition.Count)
	}

	// The count is a field of the record being read, at any depth, and never a
	// reference reaching into another record: a DEPENDING ON phrase names an
	// item of the copybook record carrying it. A count that comes from a
	// header admitted earlier reaches a repetition as a register the automaton
	// bound (#36), which is a node this package does not build — so there is
	// no shape here for a cross-record reference to take.
	if record.Find(repetition.DependingOn.Name) == nil {
		t.Error("the count is not a field of the record being read")
	}
}

// TestAnItemBehindASlidingTableMovesWithTheCount is the whole of the model: no
// node carries an offset, the extent of the table is one occurrence times the
// count, and the item behind it is wherever that sum lands.
func TestAnItemBehindASlidingTableMovesWithTheCount(t *testing.T) {
	t.Parallel()

	record := slid(t, varying)
	count := countOf(t, record, "ENTRY")

	for _, tc := range []struct {
		occurrences int
		trailer     int
		extent      int
	}{
		{occurrences: 1, trailer: 2 + 5, extent: 2 + 5 + 3},
		{occurrences: 2, trailer: 2 + 10, extent: 2 + 10 + 3},
		{occurrences: 4, trailer: 2 + 20, extent: 2 + 20 + 3},
	} {
		at, err := record.At(Counts{count: tc.occurrences})
		if err != nil {
			t.Fatalf("reading at %d occurrences: %v", tc.occurrences, err)
		}

		if got := positionOf(t, at, "TRAILER"); got != tc.trailer {
			t.Errorf("at %d occurrences TRAILER is at %d, want %d", tc.occurrences, got, tc.trailer)
		}
		if got := at.Extent(); got != tc.extent {
			t.Errorf("at %d occurrences the record is %d bytes, want %d", tc.occurrences, got, tc.extent)
		}

		// The count itself is ahead of the table, so nothing moves it.
		if got := positionOf(t, at, "N"); got != 0 {
			t.Errorf("at %d occurrences N is at %d, want 0", tc.occurrences, got)
		}
	}

	// The record it was read from is untouched: [Record.At] hands back a copy,
	// so a caller holding one count's reading still holds the maximum.
	if got := record.Find("ENTRY").Repetition.Count; got != 4 {
		t.Errorf("reading at a count moved the record it was read from to %d occurrences", got)
	}
}

// TestAGroupThatVariesIsFollowedByFieldsThatMove is the same sum over a table
// whose occurrences are groups rather than elementary items, which is the shape
// a real layout has.
func TestAGroupThatVariesIsFollowedByFieldsThatMove(t *testing.T) {
	t.Parallel()

	record := slid(t, `01 R.
   05 N PIC 9(2).
   05 T OCCURS 1 TO 3 TIMES DEPENDING ON N.
      10 A PIC X(3).
      10 B PIC X(2).
   05 TRAILER PIC X(4).
   05 TAIL PIC X.
`)
	count := countOf(t, record, "T")

	at, err := record.At(Counts{count: 2})
	if err != nil {
		t.Fatalf("reading at 2 occurrences: %v", err)
	}

	// A position inside the table is the first occurrence's, which is where
	// the sum lands whatever the count.
	for _, tc := range []struct {
		name string
		want int
	}{
		{name: "N", want: 0},
		{name: "A", want: 2},
		{name: "B", want: 5},
		{name: "TRAILER", want: 2 + 10},
		{name: "TAIL", want: 2 + 10 + 4},
	} {
		if got := positionOf(t, at, tc.name); got != tc.want {
			t.Errorf("%s is at %d, want %d", tc.name, got, tc.want)
		}
	}

	if got := at.Extent(); got != 2+10+4+1 {
		t.Errorf("the record is %d bytes, want %d", got, 2+10+4+1)
	}
}

// TestTheSameCopybookResolvesTwoWaysUnderTheTwoReadings is the fork, met from
// both sides at once.
//
// Under `noodoslide` the bytes are a fixed table at the declared maximum beside a
// field saying how many entries the writing program filled: no reference, no
// bounds to check, a constant extent, and a count field resolved like any other
// field of the record. Under `odoslide` the same copybook is the variable record
// above. Nothing in the file distinguishes them, which is why the layout has to.
func TestTheSameCopybookResolvesTwoWaysUnderTheTwoReadings(t *testing.T) {
	t.Parallel()

	slides := slid(t, varying)

	records, err := reading(t, varying, layoutmodel.NoODOSlide)
	if err != nil {
		t.Fatalf("resolving under noodoslide: %v", err)
	}
	fixed := records[0]

	repetition := fixed.Find("ENTRY").Repetition
	if repetition == nil || repetition.Reference() {
		t.Fatalf("under noodoslide the table's repetition is %+v, want a constant", repetition)
	}
	if repetition.Count != 4 || repetition.Min != 4 || repetition.Max != 4 {
		t.Errorf(
			"under noodoslide the table occurs %d times with bounds %d to %d, want the declared maximum of 4 throughout",
			repetition.Count, repetition.Min, repetition.Max)
	}

	// The count field is still there and is an ordinary field: it governs no
	// byte, and a caller reads it to learn how many entries were filled.
	if node := fixed.Find("N"); node == nil || node.Kind != KindField || node.Repetition != nil {
		t.Errorf("under noodoslide N resolved to %+v, want an ordinary field", node)
	}

	// The two readings agree at the maximum and nowhere else, which is the
	// whole of why neither can be a default.
	if fixed.Extent() != slides.Extent() {
		t.Errorf("at the maximum the two readings give %d and %d bytes", fixed.Extent(), slides.Extent())
	}

	one, err := slides.At(Counts{countOf(t, slides, "ENTRY"): 1})
	if err != nil {
		t.Fatalf("reading the sliding record at one occurrence: %v", err)
	}
	if one.Extent() == fixed.Extent() {
		t.Errorf("at one occurrence both readings give %d bytes, and the readings differ there", one.Extent())
	}
}

// TestANonSlidingRecordHasNothingForTheCountRulesToBindOn is docs/ir/SPEC.md's
// "None of this reaches a record of a non-sliding file", met with the shape the
// sliding reading refuses.
//
// A count behind another variable table is rejected under `odoslide` and is
// nothing at all under `noodoslide`, because under that reading neither table is
// variable and neither count field governs a byte.
func TestANonSlidingRecordHasNothingForTheCountRulesToBindOn(t *testing.T) {
	t.Parallel()

	src := `01 R.
   05 M PIC 9(2).
   05 FIRST OCCURS 1 TO 5 TIMES DEPENDING ON M PIC X(4).
   05 N PIC 9(2).
   05 SECOND OCCURS 1 TO 3 TIMES DEPENDING ON N PIC X(2).
`

	if _, err := reading(t, src, layoutmodel.NoODOSlide); err != nil {
		t.Fatalf("resolving under noodoslide: %v", err)
	}

	_, err := reading(t, src, layoutmodel.ODOSlide)

	var position *CountPositionError
	if !errors.As(err, &position) {
		t.Fatalf("resolving under odoslide reported %v, want a CountPositionError", err)
	}
}

// TestANonSlidingRecordFitsAFixedLengthDataset is the other consequence of the
// resolution, and the escape #92 named: such a record has a constant extent, so
// it meets what a fixed-length dataset requires of its record types exactly as a
// record with no table does.
func TestANonSlidingRecordFitsAFixedLengthDataset(t *testing.T) {
	t.Parallel()

	src := `01 R.
   05 N PIC 9(2).
   05 ENTRY OCCURS 1 TO 4 TIMES DEPENDING ON N PIC X(5).
`

	records, err := Resolve(recordOf(t, src), Options{
		Copybook: "test.cpy",
		Dialect:  copybook.IBMEnterprise(),
		Encoding: mainframe(),
		Reading:  layoutmodel.NoODOSlide,
		Framing:  fixed(22),
	})
	if err != nil {
		t.Fatalf("resolving under noodoslide on a fixed-length dataset: %v", err)
	}
	if got := records[0].Extent(); got != 22 {
		t.Errorf("the record is %d bytes, want the lrecl of 22", got)
	}
}

// TestALayoutStatingNoReadingIsRejected: there is nothing to fall back on, and a
// default here is every item behind the table read at the wrong offset, at every
// record, with nothing in the file to disagree.
func TestALayoutStatingNoReadingIsRejected(t *testing.T) {
	t.Parallel()

	_, err := reading(t, varying, layoutmodel.ReadingUnstated)

	var unstated *UnstatedReadingError
	if !errors.As(err, &unstated) {
		t.Fatalf("resolving reported %v, want an UnstatedReadingError", err)
	}
	for _, want := range []string{"R", "ENTRY", "N", "odoslide", "noodoslide", "test.cpy"} {
		if !strings.Contains(unstated.Error(), want) {
			t.Errorf("the diagnostic does not name %s: %s", want, unstated.Error())
		}
	}
}

// TestACopybookWithNoTableNeedsNoReading is the other half of the arity: the
// setting is about tables, and requiring it of every layout would make it a
// setting about layouts that have none.
func TestACopybookWithNoTableNeedsNoReading(t *testing.T) {
	t.Parallel()

	if _, err := reading(t, "01 R.\n   05 A PIC X(4).\n   05 T PIC X OCCURS 3 TIMES.\n", layoutmodel.ReadingUnstated); err != nil {
		t.Fatalf("resolving a copybook with no DEPENDING ON: %v", err)
	}
}

// TestACountThatRepeatsIsRejected and its neighbour below are docs/ir/SPEC.md's
// "A reference names a field, not an occurrence of one": nothing carries an
// occurrence number, and a count with one value per occurrence makes the
// occurrences of its group different widths.
func TestACountThatRepeatsIsRejected(t *testing.T) {
	t.Parallel()

	_, err := reading(t, `01 R.
   05 N PIC 9(2) OCCURS 2 TIMES.
   05 ENTRY OCCURS 1 TO 5 TIMES DEPENDING ON N PIC X(4).
`, layoutmodel.ODOSlide)

	var occurrence *CountOccurrenceError
	if !errors.As(err, &occurrence) {
		t.Fatalf("resolving reported %v, want a CountOccurrenceError", err)
	}
	if occurrence.Group != "" {
		t.Errorf("the diagnostic names the enclosing group %s, and the count is what repeats", occurrence.Group)
	}
	for _, want := range []string{"R", "N", "ENTRY"} {
		if !strings.Contains(occurrence.Error(), want) {
			t.Errorf("the diagnostic does not name %s: %s", want, occurrence.Error())
		}
	}
}

// TestACountInsideARepeatingGroupIsRejected is the same rule met at depth, and
// the diagnostic names the group as well as the field: it is the group that
// gives the count a value per occurrence.
func TestACountInsideARepeatingGroupIsRejected(t *testing.T) {
	t.Parallel()

	_, err := reading(t, `01 R.
   05 G OCCURS 2 TIMES.
      10 N PIC 9(2).
   05 ENTRY OCCURS 1 TO 5 TIMES DEPENDING ON N PIC X(4).
`, layoutmodel.ODOSlide)

	var occurrence *CountOccurrenceError
	if !errors.As(err, &occurrence) {
		t.Fatalf("resolving reported %v, want a CountOccurrenceError", err)
	}
	if occurrence.Group != "G" {
		t.Errorf("the diagnostic names the enclosing group %q, want G", occurrence.Group)
	}
	for _, want := range []string{"R", "N", "ENTRY", "G"} {
		if !strings.Contains(occurrence.Error(), want) {
			t.Errorf("the diagnostic does not name %s: %s", want, occurrence.Error())
		}
	}
}

// TestACountBehindTheTableItSizesIsRejected is docs/ir/SPEC.md's "A count is in
// hand before the extent it decides", first half.
//
// The rejection is `cobol-go`'s rather than this package's, and deliberately so:
// locating a trailing count needs the table's extent and the table's extent
// needs the count, and the layout refuses that while it is being computed. What
// this pins is that the fault reaches a caller naming both the count and the
// table rather than as a width that came out wrong.
func TestACountBehindTheTableItSizesIsRejected(t *testing.T) {
	t.Parallel()

	_, err := reading(t, `01 R.
   05 ENTRY OCCURS 1 TO 5 TIMES DEPENDING ON N PIC X(4).
   05 N PIC 9(2).
`, layoutmodel.ODOSlide)
	if err == nil {
		t.Fatal("a record whose count sits behind the table it sizes resolved")
	}
	for _, want := range []string{"N", "ENTRY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the diagnostic does not name %s: %s", want, err)
		}
	}
}

// TestACountBehindAnotherVariableTableIsRejected is that section's second half:
// a count whose own position is the sum of the widths ahead of it, and one of
// those widths moves with a count of its own.
//
// It is readable — a walk in record order reaches the first table's count first
// — and it is refused all the same, because it is a count no compiler writes and
// relaxing the rule later costs no version.
func TestACountBehindAnotherVariableTableIsRejected(t *testing.T) {
	t.Parallel()

	_, err := reading(t, `01 R.
   05 M PIC 9(2).
   05 FIRST OCCURS 1 TO 5 TIMES DEPENDING ON M PIC X(4).
   05 N PIC 9(2).
   05 SECOND OCCURS 1 TO 3 TIMES DEPENDING ON N PIC X(2).
`, layoutmodel.ODOSlide)

	var position *CountPositionError
	if !errors.As(err, &position) {
		t.Fatalf("resolving reported %v, want a CountPositionError", err)
	}
	if position.Count != "N" || position.Table != "SECOND" || position.Behind != "FIRST" {
		t.Errorf(
			"the diagnostic is about count %s, table %s, behind %s; want N, SECOND and FIRST",
			position.Count, position.Table, position.Behind)
	}
	for _, want := range []string{"R", "N", "SECOND", "FIRST"} {
		if !strings.Contains(position.Error(), want) {
			t.Errorf("the diagnostic does not name %s: %s", want, position.Error())
		}
	}
}

// TestTwoTablesMayShareOneCount is docs/ir/SPEC.md's "One count may size two
// tables, and a writer refuses to choose": a reference is a reference, and the
// field it names does not become the property of the first item that named it.
//
// Reading never notices. A consumer decodes the field once and sizes both tables
// from that one value, and the tables need share neither a width per occurrence
// nor a declared range.
func TestTwoTablesMayShareOneCount(t *testing.T) {
	t.Parallel()

	record := slid(t, `01 R.
   05 N PIC 9(2).
   05 FIRST OCCURS 1 TO 5 TIMES DEPENDING ON N PIC X(4).
   05 SECOND OCCURS 1 TO 9 TIMES DEPENDING ON N PIC X(2).
   05 TRAILER PIC X(3).
`)

	count := countOf(t, record, "FIRST")
	if countOf(t, record, "SECOND") != count {
		t.Fatal("the two tables name two different count fields")
	}

	at, err := record.At(Counts{count: 3})
	if err != nil {
		t.Fatalf("reading at 3 occurrences: %v", err)
	}

	for _, tc := range []struct {
		name string
		want int
	}{
		{name: "FIRST", want: 2},
		{name: "SECOND", want: 2 + 12},
		{name: "TRAILER", want: 2 + 12 + 6},
	} {
		if got := positionOf(t, at, tc.name); got != tc.want {
			t.Errorf("at 3 occurrences %s is at %d, want %d", tc.name, got, tc.want)
		}
	}
	if got := at.Extent(); got != 2+12+6+3 {
		t.Errorf("at 3 occurrences the record is %d bytes, want %d", got, 2+12+6+3)
	}
}

// TestASharedCountIsCheckedAgainstEveryRepetitionNamingIt is the one place the
// second reference is not simply a second multiplication: each repetition
// carries its own declared bounds, both bind the one value, and the range a
// record can actually carry is the overlap rather than either.
func TestASharedCountIsCheckedAgainstEveryRepetitionNamingIt(t *testing.T) {
	t.Parallel()

	record := slid(t, `01 R.
   05 N PIC 9(2).
   05 NARROW OCCURS 1 TO 3 TIMES DEPENDING ON N PIC X(4).
   05 WIDE OCCURS 1 TO 9 TIMES DEPENDING ON N PIC X(2).
`)
	count := countOf(t, record, "NARROW")

	// Inside both ranges, so both tables are sized and nothing is reported.
	if _, err := record.At(Counts{count: 2}); err != nil {
		t.Fatalf("reading at 2 occurrences, which both tables admit: %v", err)
	}

	// Inside the second range and outside the first. Checking against one of
	// the two would read this as well-formed.
	_, err := record.At(Counts{count: 5})

	var bounds *CountBoundsError
	if !errors.As(err, &bounds) {
		t.Fatalf("reading at 5 occurrences reported %v, want a CountBoundsError", err)
	}
	if bounds.Table != "NARROW" || bounds.Value != 5 || bounds.Min != 1 || bounds.Max != 3 {
		t.Errorf("the diagnostic is %+v, want 5 against NARROW's 1 to 3", bounds)
	}
	if got := len(diag.Diagnostics(err)); got != 1 {
		t.Errorf("reading at 5 occurrences reported %d faults, want the one table that refuses it", got)
	}
}

// TestTwoTablesSharingACountWithDisjointRangesAreRejected: no value sizes both
// tables, so every record of the descriptor is malformed data, and the
// alternative to rejecting the layout is that diagnostic once per record for the
// life of the file.
func TestTwoTablesSharingACountWithDisjointRangesAreRejected(t *testing.T) {
	t.Parallel()

	_, err := reading(t, `01 R.
   05 N PIC 9(2).
   05 FIRST OCCURS 1 TO 3 TIMES DEPENDING ON N PIC X(4).
   05 SECOND OCCURS 5 TO 9 TIMES DEPENDING ON N PIC X(2).
`, layoutmodel.ODOSlide)

	var shared *SharedCountBoundsError
	if !errors.As(err, &shared) {
		t.Fatalf("resolving reported %v, want a SharedCountBoundsError", err)
	}
	if len(shared.Tables) != 2 || shared.Tables[0] != "FIRST" || shared.Tables[1] != "SECOND" {
		t.Errorf("the diagnostic names %v, want both repeating items", shared.Tables)
	}
	for _, want := range []string{"R", "N", "FIRST", "SECOND", "1 to 3", "5 to 9"} {
		if !strings.Contains(shared.Error(), want) {
			t.Errorf("the diagnostic does not name %s: %s", want, shared.Error())
		}
	}
}

// TestRangesThatMeetAtOneValueOverlap is the boundary of that rule: one value
// sizing both tables is a record a consumer can read, so the layout stands.
func TestRangesThatMeetAtOneValueOverlap(t *testing.T) {
	t.Parallel()

	record := slid(t, `01 R.
   05 N PIC 9(2).
   05 FIRST OCCURS 1 TO 3 TIMES DEPENDING ON N PIC X(4).
   05 SECOND OCCURS 3 TO 9 TIMES DEPENDING ON N PIC X(2).
`)

	if _, err := record.At(Counts{countOf(t, record, "FIRST"): 3}); err != nil {
		t.Fatalf("reading at the one count both tables admit: %v", err)
	}
}

// TestACountOutsideTheDeclaredBoundsIsMalformedData is the check the bounds are
// carried for, and the only thing in reach that bounds a number decoded out of a
// file: a descriptor word stating the length a forbidden count implies agrees
// with the extent exactly, so the framing check passes on a record the copybook
// says cannot exist.
func TestACountOutsideTheDeclaredBoundsIsMalformedData(t *testing.T) {
	t.Parallel()

	record := slid(t, varying)
	count := countOf(t, record, "ENTRY")

	for _, tc := range []struct {
		name  string
		value int
	}{
		{name: "below the minimum", value: 0},
		{name: "above the maximum", value: 5},
		// A count field holding spaces is the case that matters, and a
		// consumer that decoded one as a negative number reports it rather
		// than reading the table as absent.
		{name: "negative", value: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			at, err := record.At(Counts{count: tc.value})

			var bounds *CountBoundsError
			if !errors.As(err, &bounds) {
				t.Fatalf("reading at %d reported %v, want a CountBoundsError", tc.value, err)
			}
			if at != nil {
				t.Error("a record was handed back beside the fault")
			}
			if bounds.Value != tc.value || bounds.Min != 1 || bounds.Max != 4 {
				t.Errorf("the diagnostic is %+v, want %d against 1 to 4", bounds, tc.value)
			}
			for _, want := range []string{"R", "N", "ENTRY"} {
				if !strings.Contains(bounds.Error(), want) {
					t.Errorf("the diagnostic does not name %s: %s", want, bounds.Error())
				}
			}
		})
	}
}

// TestACountNobodyDecodedIsNotZeroOccurrences: reading a count field holding
// spaces as zero produces a record that parses and is wrong, so a count that is
// not in hand is refused rather than defaulted.
func TestACountNobodyDecodedIsNotZeroOccurrences(t *testing.T) {
	t.Parallel()

	record := slid(t, varying)

	_, err := record.At(Counts{})

	var missing *MissingCountError
	if !errors.As(err, &missing) {
		t.Fatalf("reading with no count reported %v, want a MissingCountError", err)
	}
	if missing.Count != "N" || missing.Table != "ENTRY" {
		t.Errorf("the diagnostic is about count %s and table %s, want N and ENTRY", missing.Count, missing.Table)
	}
}

// TestReadingARecordWithNoCountAtAllChangesNothing: every record of a
// non-sliding file is one of these, and so is every record with no table, so the
// empty map is the right argument for one.
func TestReadingARecordWithNoCountAtAllChangesNothing(t *testing.T) {
	t.Parallel()

	record := slid(t, "01 R.\n   05 A PIC X(4).\n   05 T PIC X OCCURS 3 TIMES.\n")

	at, err := record.At(Counts{})
	if err != nil {
		t.Fatalf("reading a record with no count reference: %v", err)
	}
	if at.Extent() != record.Extent() {
		t.Errorf("the record read at no counts is %d bytes, want the %d it already was", at.Extent(), record.Extent())
	}
	if got := at.Find("T").Repetition.Count; got != 3 {
		t.Errorf("the fixed table stands at %d occurrences, want the copybook's 3", got)
	}
}

// TestASlidingTableInsideAnArmIsStillRefused holds the reading to changing what
// a repetition is and nothing else: an arm's extent has to be a constant, and
// under `odoslide` a table inside one is not.
//
// Under the other reading it is, which is why this is a pair rather than a
// single assertion — the same copybook that cannot be described under one is
// ordinary under the other.
func TestASlidingTableInsideAnArmIsStillRefused(t *testing.T) {
	t.Parallel()

	src := `01 R.
   05 N PIC 9(2).
   05 T OCCURS 2 TIMES.
      10 K PIC X.
      10 A PIC X(12).
      10 B REDEFINES A.
         15 CELL OCCURS 1 TO 3 TIMES DEPENDING ON N PIC X(4).
`
	resolve := func(r layoutmodel.Reading) error {
		field := recordOf(t, src)

		_, err := Resolve(field, Options{
			Copybook: "test.cpy",
			Dialect:  copybook.IBMEnterprise(),
			Encoding: mainframe(),
			Reading:  r,
			Redefines: []Redefine{{
				Item: fieldNamed(t, field, "A"),
				Alternatives: []Alternative{
					{Name: "A", Predicate: selects("A", "T", "K")},
					{Name: "B", Predicate: selects("B", "T", "K")},
				},
			}},
		})

		return err
	}

	var variable *ArmVariableCountError
	if err := resolve(layoutmodel.ODOSlide); !errors.As(err, &variable) {
		t.Fatalf("resolving under odoslide reported %v, want an ArmVariableCountError", err)
	}

	if err := resolve(layoutmodel.NoODOSlide); err != nil {
		t.Fatalf("resolving under noodoslide, where the same arm is a fixed table: %v", err)
	}
}
