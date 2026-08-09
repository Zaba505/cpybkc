// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"testing"

	"github.com/Zaba505/cobol-go/copybook"
)

// Any byte of a record that no item occupies appears in the containment order as
// a slack node, whatever left it uncovered. Which alignments a dialect honours
// is `cobol-go`'s and the rest of the rule is #34's; what is held here is the
// part the position sum cannot do without, which is that the members of a group
// add up to its extent exactly.

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
	records, err := Resolve(field, Options{Dialect: dialect})
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
