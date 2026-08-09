// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"fmt"
	"slices"
	"testing"

	"github.com/Zaba505/cobol-go/copybook"
)

// Any byte of a record that no item occupies appears in the containment order as
// a slack node, whatever left it uncovered. Which alignments a dialect honours is
// `cobol-go`'s and is asserted here as vectors rather than derived; what is held
// here is the part the position sum cannot do without, which is that the members
// of a group add up to its extent exactly. The fourth producer of slack — the
// tail of a record short of `lrecl` — is in lrecl_test.go, beside the check it
// comes out of.

// TestAlignmentReachesTheRecordAsBytes is the sum being literally true with
// SYNCHRONIZED present: the bytes an aligned item is pushed forward by are in
// the member list, so a generator has nothing left to align.
func TestAlignmentReachesTheRecordAsBytes(t *testing.T) {
	t.Parallel()

	record := resolveOne(t, `01 R.
   05 A PIC X.
   05 B PIC S9(9) COMP SYNCHRONIZED.
   05 C PIC X(3).
`)

	if got := positionOf(t, record, "B"); got != 4 {
		t.Fatalf("the aligned item is at %d, want 4", got)
	}
	if got := slackWidths(record.Root); len(got) != 1 || got[0] != 3 {
		t.Fatalf("the record's slack is %v, want one run of 3", got)
	}
	if got := positionOf(t, record, "C"); got != 8 {
		t.Errorf("the item behind it is at %d, want 8", got)
	}
	if got := record.Extent(); got != 11 {
		t.Errorf("the record is %d bytes, want 11", got)
	}
}

// TestAlignmentVectorsAtEachBinaryWidth walks every boundary a SYNCHRONIZED item
// sits on, because a generator implements no alignment rule at all: the bytes
// reach it in the member list, so a boundary this stage gets wrong is one
// nothing downstream is looking for.
//
// One byte in front of the item is the vector. It is the offset that is wrong
// for every boundary at once, so the slack a boundary of n needs is n-1 and the
// item lands on n, and one table covers the whole staircase rather than a case
// per width with a different lead-in.
func TestAlignmentVectorsAtEachBinaryWidth(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		picture  string
		width    int
		boundary int
	}{
		// The binary staircase under IBM Enterprise COBOL's 2/4/8, each
		// rung on the boundary of the machine datum it is held in.
		{name: "binary halfword", picture: "PIC S9(4) COMP", width: 2, boundary: 2},
		{name: "binary fullword", picture: "PIC S9(9) COMP", width: 4, boundary: 4},
		{name: "binary doubleword", picture: "PIC S9(18) COMP", width: 8, boundary: 8},

		// Floating point, index and pointer take their boundary from the
		// usage and the dialect rather than from a PICTURE.
		{name: "single-precision float", picture: "COMP-1", width: 4, boundary: 4},
		{name: "double-precision float", picture: "COMP-2", width: 8, boundary: 8},
		{name: "index", picture: "USAGE INDEX", width: 4, boundary: 4},
		{name: "pointer", picture: "USAGE POINTER", width: 4, boundary: 4},

		// The two usages the clause is syntax-checked on and has no effect
		// for: characters and nibbles have no machine boundary to sit on,
		// so the item stays where it was and there is no slack at all.
		{name: "display", picture: "PIC X(3)", width: 3, boundary: 1},
		{name: "packed decimal", picture: "PIC S9(5) COMP-3", width: 3, boundary: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			record := resolveOne(t, fmt.Sprintf(`01 R.
   05 LEAD PIC X.
   05 A %s SYNCHRONIZED.
   05 TRAIL PIC X.
`, tc.picture))

			if got, want := slackWidths(record.Root), gap(tc.boundary); !slices.Equal(got, want) {
				t.Fatalf("%s left slack of %v, want %v ahead of a %d-byte boundary", tc.picture, got, want, tc.boundary)
			}
			if got := positionOf(t, record, "A"); got != tc.boundary {
				t.Errorf("%s is at %d, want the %d-byte boundary", tc.picture, got, tc.boundary)
			}
			if got := widthOf(t, record, "A"); got != tc.width {
				t.Errorf("%s is %d bytes, want %d", tc.picture, got, tc.width)
			}
			if got := record.Extent(); got != tc.boundary+tc.width+1 {
				t.Errorf("the record is %d bytes, want %d", got, tc.boundary+tc.width+1)
			}
		})
	}
}

// TestAlignmentVectorsAtEachSmallestBinaryWidth is the same staircase under a
// dialect whose binary widths are not powers of two, which is the only way to
// reach the rounding in the boundary rule at all: a three-byte item sits on a
// fullword and a five-byte item on a doubleword, so the boundary is not the
// width and a generator deriving one from the other would be wrong on half the
// staircase.
func TestAlignmentVectorsAtEachSmallestBinaryWidth(t *testing.T) {
	t.Parallel()

	dialect := copybook.IBMEnterprise()
	dialect.Binary = copybook.BinarySizeSmallest

	for _, tc := range []struct {
		digits   int
		width    int
		boundary int
	}{
		{digits: 2, width: 1, boundary: 1},
		{digits: 4, width: 2, boundary: 2},
		{digits: 6, width: 3, boundary: 4},
		{digits: 9, width: 4, boundary: 4},
		{digits: 11, width: 5, boundary: 8},
		{digits: 14, width: 6, boundary: 8},
		{digits: 16, width: 7, boundary: 8},
		{digits: 18, width: 8, boundary: 8},
	} {
		t.Run(fmt.Sprintf("%d digits", tc.digits), func(t *testing.T) {
			t.Parallel()

			field := recordOf(t, fmt.Sprintf(`01 R.
   05 LEAD PIC X.
   05 A PIC S9(%d) COMP SYNCHRONIZED.
   05 TRAIL PIC X.
`, tc.digits))
			records, err := Resolve(field, Options{Dialect: dialect, Encoding: mainframe()})
			if err != nil {
				t.Fatalf("resolving: %v", err)
			}
			record := records[0]

			if got, want := slackWidths(record.Root), gap(tc.boundary); !slices.Equal(got, want) {
				t.Fatalf("%d digits left slack of %v, want %v ahead of a %d-byte boundary", tc.digits, got, want, tc.boundary)
			}
			if got := positionOf(t, record, "A"); got != tc.boundary {
				t.Errorf("%d digits is at %d, want the %d-byte boundary", tc.digits, got, tc.boundary)
			}
			if got := widthOf(t, record, "A"); got != tc.width {
				t.Errorf("%d digits is %d bytes, want %d", tc.digits, got, tc.width)
			}
		})
	}
}

// gap is the slack a boundary leaves behind the one byte in front of the item:
// one run of boundary-1, and no run at all where the item aligns to a byte.
func gap(boundary int) []int {
	if boundary <= 1 {
		return nil
	}
	return []int{boundary - 1}
}

// TestTwoRunsOfDifferentOriginThatAbutAreOneNode: how a producer divides a run
// of three into nodes would otherwise decide the shape of every record a
// generator emits, because a record carries one run of bytes per slack node.
func TestTwoRunsOfDifferentOriginThatAbutAreOneNode(t *testing.T) {
	t.Parallel()

	records := resolveAll(t, `01 R.
   05 A PIC X(3).
   05 B REDEFINES A PIC X.
   05 C PIC S9(9) COMP SYNCHRONIZED.
`)
	if len(records) != 2 {
		t.Fatalf("resolved to %d records, want 2", len(records))
	}

	// The first alternative fills its three bytes and meets only the byte
	// the alignment skipped.
	if got := slackWidths(records[0].Root); len(got) != 1 || got[0] != 1 {
		t.Fatalf("the first alternative's slack is %v, want one run of 1", got)
	}

	// The second leaves two bytes of the redefined item's storage, and the
	// alignment leaves the byte after them. They abut, so they are one node.
	if got := slackWidths(records[1].Root); len(got) != 1 || got[0] != 3 {
		t.Fatalf("the second alternative's slack is %v, want one run of 3", got)
	}
	if got := positionOf(t, records[1], "C"); got != 4 {
		t.Errorf("the aligned item is at %d, want 4", got)
	}
}

// TestARepeatingItemPaddedToItsStrideCarriesThePaddingInsideItself is the one
// place an elementary item has nowhere to put a slack node, so it becomes a
// group that repeats and the padding is a member of it.
//
// It takes a dialect whose binary widths are not powers of two to reach at all:
// a three-byte item on a four-byte boundary is one byte short of its own stride,
// and every occurrence after the first would start a byte early without it.
func TestARepeatingItemPaddedToItsStrideCarriesThePaddingInsideItself(t *testing.T) {
	t.Parallel()

	dialect := copybook.IBMEnterprise()
	dialect.Binary = copybook.BinarySizeSmallest

	field := recordOf(t, `01 R.
   05 T PIC S9(5) COMP SYNCHRONIZED OCCURS 3 TIMES.
   05 Z PIC X(2).
`)
	records, err := Resolve(field, Options{Dialect: dialect, Encoding: mainframe()})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	record := records[0]

	table := record.Find("T")
	if table.Repetition != nil {
		t.Error("the item carries the repetition as well as the group padding it")
	}
	if got := table.Width(); got != 3 {
		t.Errorf("the item is %d bytes, want the storage width of 3", got)
	}

	holder := record.Root.Members[0]
	if holder.Kind != KindGroup {
		t.Fatalf("the padded item is held by a %s, want a group", holder.Kind)
	}
	if holder.Repetition == nil || holder.Repetition.Count != 3 {
		t.Fatal("the group holding it does not repeat three times")
	}
	if got := holder.Width(); got != 4 {
		t.Errorf("one occurrence is %d bytes, want the stride of 4", got)
	}
	if got := positionOf(t, record, "Z"); got != 12 {
		t.Errorf("the item behind the table is at %d, want 12", got)
	}
}

// TestARepeatingGroupPaddedToItsStrideCarriesThePaddingAmongItsMembers is the
// same padding one level up, where the group already holds members and the
// padding is simply the last of them.
func TestARepeatingGroupPaddedToItsStrideCarriesThePaddingAmongItsMembers(t *testing.T) {
	t.Parallel()

	record := resolveOne(t, `01 R.
   05 T OCCURS 3 TIMES.
      10 A PIC S9(9) COMP SYNCHRONIZED.
      10 B PIC X.
   05 Z PIC X(2).
`)

	table := record.Find("T")
	if got := table.Width(); got != 8 {
		t.Fatalf("one occurrence is %d bytes, want the stride of 8", got)
	}
	if got := slackWidths(table); len(got) != 1 || got[0] != 3 {
		t.Fatalf("the occurrence's slack is %v, want one run of 3", got)
	}
	if got := positionOf(t, record, "Z"); got != 24 {
		t.Errorf("the item behind the table is at %d, want 24", got)
	}
}
