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
)

// describeOf reads the shape of the first record of a copybook source, under the
// dialect every other reader in this repository uses.
func describeOf(t *testing.T, src string) Shape {
	t.Helper()

	shape, err := Describe(recordOf(t, src), copybook.IBMEnterprise())
	if err != nil {
		t.Fatalf("describing the record: %v", err)
	}

	return shape
}

// chosenNames is what a combination chose, for a message and a comparison.
func chosenNames(combination []*copybook.Field) []string {
	chosen := make([]string, 0, len(combination))
	for _, field := range combination {
		chosen = append(chosen, field.Name)
	}

	return chosen
}

func TestARecordWithNoRedefinesIsOneRecordTypeChosenAtNothing(t *testing.T) {
	t.Parallel()

	shape := describeOf(t, `01 TXN.
   05 KIND PIC X.
   05 BODY PIC X(24).
`)

	if len(shape.Alternations) != 0 {
		t.Errorf("found %d alternations, want none", len(shape.Alternations))
	}

	if len(shape.Combinations) != 1 {
		t.Fatalf("resolved to %d record types, want one", len(shape.Combinations))
	}

	if got := chosenNames(shape.Combinations[0]); len(got) != 0 {
		t.Errorf("the one record type chose %v, want nothing", got)
	}
}

func TestTwoIndependentRedefinesMultiplyIntoRecordTypes(t *testing.T) {
	t.Parallel()

	// The worked example's shape: two independent runs, one described three
	// ways and one described twice, so six record types come out of one
	// 01-level.
	shape := describeOf(t, `01 POSTING.
   05 PST-BODY PIC X(24).
   05 PST-DEBIT REDEFINES PST-BODY PIC X(24).
   05 PST-CREDIT REDEFINES PST-BODY PIC X(24).
   05 PST-TAIL PIC X(8).
   05 PST-TAIL-REF REDEFINES PST-TAIL PIC X(8).
`)

	if len(shape.Alternations) != 2 {
		t.Fatalf("found %d alternations, want two", len(shape.Alternations))
	}

	for _, alternation := range shape.Alternations {
		if alternation.InTable {
			t.Errorf("%s was read as a variant, and nothing here repeats", alternation.Item.Name)
		}
	}

	if got := chosenNames(shape.Alternations[0].Alternatives); strings.Join(got, ",") != "PST-BODY,PST-DEBIT,PST-CREDIT" {
		t.Errorf("the first run is described by %v, want the redefined item first", got)
	}

	// Six, in the order that leaves the later run varying fastest: a scaffold
	// is diffed against the one a later run produces, so the order is fixed
	// rather than incidental.
	want := []string{
		"PST-BODY,PST-TAIL",
		"PST-BODY,PST-TAIL-REF",
		"PST-DEBIT,PST-TAIL",
		"PST-DEBIT,PST-TAIL-REF",
		"PST-CREDIT,PST-TAIL",
		"PST-CREDIT,PST-TAIL-REF",
	}

	if len(shape.Combinations) != len(want) {
		t.Fatalf("resolved to %d record types, want %d", len(shape.Combinations), len(want))
	}

	for at, combination := range shape.Combinations {
		if got := strings.Join(chosenNames(combination), ","); got != want[at] {
			t.Errorf("record type %d chose %s, want %s", at, got, want[at])
		}
	}
}

func TestARedefineInsideARepeatingGroupIsAVariantAndMultipliesNothing(t *testing.T) {
	t.Parallel()

	shape := describeOf(t, `01 ORDER.
   05 LINE-COUNT PIC 9(2).
   05 LINE OCCURS 10 TIMES.
      10 LN-BODY PIC X(12).
      10 LN-CARD REDEFINES LN-BODY PIC X(12).
`)

	if len(shape.Alternations) != 1 {
		t.Fatalf("found %d alternations, want one", len(shape.Alternations))
	}

	alternation := shape.Alternations[0]
	if !alternation.InTable {
		t.Error("a redefine inside a repeating group was read as an alternative")
	}

	if got := chosenNames(alternation.Alternatives); strings.Join(got, ",") != "LN-BODY,LN-CARD" {
		t.Errorf("the variant is described by %v, want LN-BODY then LN-CARD", got)
	}

	// The choice is made once per occurrence, so it is not a record type.
	if len(shape.Combinations) != 1 {
		t.Fatalf("resolved to %d record types, want one", len(shape.Combinations))
	}

	if got := chosenNames(shape.Combinations[0]); len(got) != 0 {
		t.Errorf("the one record type chose %v, want nothing", got)
	}
}

func TestARedefineOutsideATableIsStillOneWhereAnotherRunRepeats(t *testing.T) {
	t.Parallel()

	shape := describeOf(t, `01 ORDER.
   05 HDR-BODY PIC X(6).
   05 HDR-CARD REDEFINES HDR-BODY PIC X(6).
   05 LINE OCCURS 4 TIMES.
      10 LN-BODY PIC X(12).
      10 LN-CARD REDEFINES LN-BODY PIC X(12).
`)

	if len(shape.Alternations) != 2 {
		t.Fatalf("found %d alternations, want two", len(shape.Alternations))
	}

	if shape.Alternations[0].InTable {
		t.Error("the header's redefine was read as a variant")
	}

	if !shape.Alternations[1].InTable {
		t.Error("the line's redefine was read as an alternative")
	}

	if len(shape.Combinations) != 2 {
		t.Fatalf("resolved to %d record types, want two", len(shape.Combinations))
	}
}

func TestAnOccursDependingOnTableIsReported(t *testing.T) {
	t.Parallel()

	shape := describeOf(t, `01 ORDER.
   05 LINE-COUNT PIC 9(2).
   05 LINE OCCURS 1 TO 10 TIMES DEPENDING ON LINE-COUNT.
      10 LN-BODY PIC X(12).
`)

	if len(shape.Tables) != 1 {
		t.Fatalf("found %d tables, want one", len(shape.Tables))
	}

	if got := shape.Tables[0].Name; got != "LINE" {
		t.Errorf("the table is %s, want LINE", got)
	}
}

func TestARecordWithNoTableReportsNone(t *testing.T) {
	t.Parallel()

	shape := describeOf(t, `01 ORDER.
   05 LINE OCCURS 10 TIMES PIC X(4).
`)

	if len(shape.Tables) != 0 {
		t.Errorf("a fixed table was reported as an OCCURS DEPENDING ON: %v", shape.Tables)
	}
}

func TestDescribingNoRecordIsRefused(t *testing.T) {
	t.Parallel()

	if _, err := Describe(nil, copybook.IBMEnterprise()); !errors.Is(err, ErrNilRecord) {
		t.Errorf("describing nothing gave %v, want %v", err, ErrNilRecord)
	}
}

// A copybook a layout has not been written for yet is not a copybook with a
// fault in it: nothing an encoding profile, a framing or a discriminator would
// have said is required here, and none of the faults Resolve raises about a
// layout can be raised without one.
func TestDescribingNeedsNoLayout(t *testing.T) {
	t.Parallel()

	src := `01 ORDER.
   05 LINE-COUNT PIC 9(2).
   05 LINE OCCURS 1 TO 10 TIMES DEPENDING ON LINE-COUNT.
      10 LN-BODY PIC X(12).
      10 LN-CARD REDEFINES LN-BODY PIC X(12).
`

	if _, err := Describe(recordOf(t, src), copybook.IBMEnterprise()); err != nil {
		t.Fatalf("describing a record an unwritten layout would be refused for: %v", err)
	}

	// The same record through Resolve, which is what a layout is held to: an
	// unstated reading of the table is a fault there and says nothing here.
	if _, err := Resolve(recordOf(t, src), Options{Dialect: copybook.IBMEnterprise()}); err == nil {
		t.Error("resolving with no encoding profile and no reading succeeded")
	}
}
