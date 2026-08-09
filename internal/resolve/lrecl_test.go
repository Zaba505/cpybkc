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

// A fixed-length dataset gives every record the same number of bytes, so a
// record type whose items stop short of LRECL ends in a run that belongs to no
// item — the fourth producer of slack, beside an alignment gap, a REDEFINES tail
// and a table's stride padding, and the same kind of node as any of them once
// emitted (docs/layout/SPEC.md, "`lrecl` and `blksize` describe the dataset, not
// the stream").

// fixed is a framing over a fixed-length dataset stating lrecl, which is the
// framing under which a record type's extent has to *be* that number.
func fixed(lrecl int64) *layoutmodel.Framing {
	return &layoutmodel.Framing{
		Pos:   layout.Pos{File: "layout.sexpr", Line: 3, Column: 3},
		RECFM: layoutmodel.RECFMFB,
		LRECL: layoutmodel.Size{Pos: layout.Pos{File: "layout.sexpr", Line: 5, Column: 5}, Value: lrecl},
	}
}

// variable is a framing over a variable-length dataset stating lrecl, where the
// same number is a maximum rather than a requirement: each record's own
// descriptor word states its length.
func variable(lrecl int64) *layoutmodel.Framing {
	framing := fixed(lrecl)
	framing.RECFM = layoutmodel.RECFMVB
	return framing
}

// framed resolves a copybook source under a framing, reporting what resolution
// said rather than failing on it, because half these tests are about the fault.
func framed(t *testing.T, src string, framing *layoutmodel.Framing) ([]*Record, error) {
	t.Helper()

	return Resolve(recordOf(t, src), Options{
		Copybook: "test.cpy",
		Dialect:  copybook.IBMEnterprise(),
		Encoding: mainframe(),
		Framing:  framing,
	})
}

// TestARecordShortOfLRECLCarriesTheDifferenceAsSlack is the trailing pad: the
// bytes are in the file whatever the copybook says, so they are in the record's
// containment order too, and the next record starts after them.
func TestARecordShortOfLRECLCarriesTheDifferenceAsSlack(t *testing.T) {
	t.Parallel()

	records, err := framed(t, `01 R.
   05 A PIC X(40).
   05 B PIC X(32).
`, fixed(80))
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	record := records[0]

	if got := slackWidths(record.Root); len(got) != 1 || got[0] != 8 {
		t.Fatalf("the record's slack is %v, want one run of 8", got)
	}
	if got := record.Extent(); got != 80 {
		t.Fatalf("the record is %d bytes, want the lrecl of 80", got)
	}

	// It is the *last* member, because the bytes it stands for are the last
	// bytes of the record.
	if last := record.Root.Members[len(record.Root.Members)-1]; last.Kind != KindSlack {
		t.Errorf("the record ends in a %s, want the pad", last.Kind)
	}
}

// TestTheLRECLPadDoesNotJoinARunInsideARepeatingGroup is the edge the
// maximal-run rule does not cross, met from the pad's side rather than an arm's.
//
// The alignment slack that pads the table's occurrence out to its stride stands
// for one run of bytes in *each* occurrence, and the pad stands for one run at
// the end of the record. They abut in the last occurrence and nowhere else, so a
// node covering both would be a node of a different width per occurrence — and
// would make the last entry of the table wider than the ones in front of it.
func TestTheLRECLPadDoesNotJoinARunInsideARepeatingGroup(t *testing.T) {
	t.Parallel()

	records, err := framed(t, `01 R.
   05 T OCCURS 2 TIMES.
      10 A PIC S9(9) COMP SYNCHRONIZED.
      10 B PIC X.
`, fixed(24))
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	record := records[0]

	if got := slackWidths(record.Root); len(got) != 2 || got[0] != 3 || got[1] != 8 {
		t.Fatalf("the record's slack is %v, want a run of 3 inside the table and a pad of 8", got)
	}
	if got := record.Extent(); got != 24 {
		t.Fatalf("the record is %d bytes, want the lrecl of 24", got)
	}
}

// TestEveryAlternativeOfARecordIsPaddedToLRECL: splitting a REDEFINES into
// records leaves each one its own length, and each one is a record type of the
// dataset, so each one accounts for LRECL on its own.
func TestEveryAlternativeOfARecordIsPaddedToLRECL(t *testing.T) {
	t.Parallel()

	records, err := Resolve(recordOf(t, `01 R.
   05 KIND PIC X.
   05 WIDE PIC X(20).
   05 NARROW REDEFINES WIDE PIC X(6).
`), Options{
		Copybook: "test.cpy",
		Dialect:  copybook.IBMEnterprise(),
		Encoding: mainframe(),
		Framing:  fixed(40),
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("resolved to %d records, want 2", len(records))
	}

	// The first alternative fills the redefined item's storage and meets only
	// the pad; the second leaves 14 bytes of it, and those bytes abut the pad,
	// so the two are one run of 33.
	for _, tc := range []struct {
		alternative string
		want        []int
	}{
		{alternative: "WIDE", want: []int{19}},
		{alternative: "NARROW", want: []int{33}},
	} {
		record := records[0]
		if tc.alternative == "NARROW" {
			record = records[1]
		}

		if got := slackWidths(record.Root); len(got) != len(tc.want) || got[0] != tc.want[0] {
			t.Errorf("reading %s, the slack is %v, want %v", tc.alternative, got, tc.want)
		}
		if got := record.Extent(); got != 40 {
			t.Errorf("reading %s, the record is %d bytes, want the lrecl of 40", tc.alternative, got)
		}
	}
}

// TestARecordLongerThanAnExactLRECLIsReported is the direction no slack node can
// fix: bytes cannot be taken away, and a record type longer than the dataset's
// records overruns the next one.
func TestARecordLongerThanAnExactLRECLIsReported(t *testing.T) {
	t.Parallel()

	_, err := framed(t, "01 R.\n   05 A PIC X(96).\n", fixed(80))

	var extent *LRECLExtentError
	if !errors.As(err, &extent) {
		t.Fatalf("resolving reported %v, want an LRECLExtentError", err)
	}
	if extent.Extent != 96 || extent.LRECL != 80 {
		t.Errorf("the diagnostic says %d bytes against %d, want 96 against 80", extent.Extent, extent.LRECL)
	}
	if extent.Bound != layoutmodel.LRECLExact {
		t.Errorf("the diagnostic reports a %s bound, want exact", extent.Bound)
	}
	for _, want := range []string{"R", "layout.sexpr:5:5", "test.cpy"} {
		if !strings.Contains(extent.Error(), want) {
			t.Errorf("the diagnostic does not name %s: %s", want, extent.Error())
		}
	}
}

// TestARecordLongerThanAMaximumLRECLIsReported: under a variable-length dataset
// the same number is a maximum, and a copybook that exceeds it is a copybook
// bound to the wrong dataset.
func TestARecordLongerThanAMaximumLRECLIsReported(t *testing.T) {
	t.Parallel()

	_, err := framed(t, "01 R.\n   05 A PIC X(96).\n", variable(80))

	var extent *LRECLExtentError
	if !errors.As(err, &extent) {
		t.Fatalf("resolving reported %v, want an LRECLExtentError", err)
	}
	if extent.Bound != layoutmodel.LRECLMaximum {
		t.Errorf("the diagnostic reports a %s bound, want maximum", extent.Bound)
	}
}

// TestARecordShortOfAMaximumLRECLIsNotPadded is the difference between the two
// bounds, and it is why they are two: a variable-length record states its own
// length, so there is no distance for a pad to make up.
func TestARecordShortOfAMaximumLRECLIsNotPadded(t *testing.T) {
	t.Parallel()

	records, err := framed(t, "01 R.\n   05 A PIC X(40).\n", variable(80))
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	if got := slackWidths(records[0].Root); len(got) != 0 {
		t.Errorf("the record carries slack of %v under a maximum lrecl", got)
	}
	if got := records[0].Extent(); got != 40 {
		t.Errorf("the record is %d bytes, want the 40 its items occupy", got)
	}
}

// TestAVariableExtentDoesNotFitAFixedLengthDataset is #92's rule: a record type
// of a fixed part, a table and a pad meets LRECL at one count and misses it at
// every other, and a slack node carries one width.
func TestAVariableExtentDoesNotFitAFixedLengthDataset(t *testing.T) {
	t.Parallel()

	_, err := framed(t, `01 R.
   05 N PIC 9(2).
   05 ENTRY OCCURS 1 TO 5 TIMES DEPENDING ON N PIC X(8).
`, fixed(80))

	var variable *VariableExtentError
	if !errors.As(err, &variable) {
		t.Fatalf("resolving reported %v, want a VariableExtentError", err)
	}
	for _, want := range []string{"R", "ENTRY", "N", "FB"} {
		if !strings.Contains(variable.Error(), want) {
			t.Errorf("the diagnostic does not name %s: %s", want, variable.Error())
		}
	}
}

// TestAVariableExtentIsRefusedOnAFixedDatasetStatingNoLRECL: the rule is about
// the dataset and not about the number, so it holds whether or not a layout
// states one.
func TestAVariableExtentIsRefusedOnAFixedDatasetStatingNoLRECL(t *testing.T) {
	t.Parallel()

	framing := fixed(0)
	framing.LRECL = layoutmodel.Size{}

	_, err := framed(t, `01 R.
   05 N PIC 9(2).
   05 ENTRY OCCURS 1 TO 5 TIMES DEPENDING ON N PIC X(8).
`, framing)

	var variable *VariableExtentError
	if !errors.As(err, &variable) {
		t.Fatalf("resolving reported %v, want a VariableExtentError", err)
	}
}

// TestAVariableExtentIsAdmittedOnAVariableLengthDataset is the other side of it:
// there the record's descriptor word states its length, so an extent that moves
// with a count is the ordinary case.
func TestAVariableExtentIsAdmittedOnAVariableLengthDataset(t *testing.T) {
	t.Parallel()

	if _, err := framed(t, `01 R.
   05 N PIC 9(2).
   05 ENTRY OCCURS 1 TO 5 TIMES DEPENDING ON N PIC X(8).
`, variable(80)); err != nil {
		t.Fatalf("resolving: %v", err)
	}
}

// TestARecordIsResolvedWithNoFramingAtAll is the default every other test in
// this package runs under, asserted once: nothing about a record's members
// depends on a framing, and a caller with none to state states none.
func TestARecordIsResolvedWithNoFramingAtAll(t *testing.T) {
	t.Parallel()

	records, err := framed(t, "01 R.\n   05 A PIC X(40).\n", nil)
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	if got := records[0].Extent(); got != 40 {
		t.Fatalf("the record is %d bytes, want 40", got)
	}
	if got := slackWidths(records[0].Root); len(got) != 0 {
		t.Errorf("the record carries slack of %v with no framing stated", got)
	}
}
