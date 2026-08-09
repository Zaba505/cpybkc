// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"testing"
)

// A REDEFINES outside a repeating group is chosen once per record, so each
// alternative becomes its own record with its own containment order — and the
// bytes the copybook gave the alternatives jointly that this one does not use
// are slack, because splitting them is what discards the storage they shared.

// TestARedefineOutsideATableBecomesOneRecordPerAlternative is the resolution
// itself: two descriptions of one run of bytes leave, two records arrive, and
// neither carries anything a consumer would have to collapse.
func TestARedefineOutsideATableBecomesOneRecordPerAlternative(t *testing.T) {
	t.Parallel()

	records := resolveAll(t, `01 TXN.
   05 KIND PIC X.
   05 BODY PIC X(24).
   05 CARD REDEFINES BODY.
      10 NUMBER PIC X(16).
      10 EXPIRY PIC X(4).
   05 TRAILER PIC X(2).
`)

	if len(records) != 2 {
		t.Fatalf("resolved to %d records, want one per alternative", len(records))
	}

	for _, record := range records {
		if got := record.Extent(); got != 27 {
			t.Errorf("an alternative is %d bytes, want 27", got)
		}
		if got := positionOf(t, record, "TRAILER"); got != 25 {
			t.Errorf("TRAILER is at %d in an alternative, want 25", got)
		}
		if record.Find("BODY") != nil && record.Find("CARD") != nil {
			t.Error("an alternative carries both descriptions of the same bytes")
		}
	}

	if len(records[0].Alternatives) != 1 || records[0].Alternatives[0].Name != "BODY" {
		t.Fatalf("the first record chose %v, want BODY", records[0].Alternatives)
	}
	if len(records[1].Alternatives) != 1 || records[1].Alternatives[0].Name != "CARD" {
		t.Fatalf("the second record chose %v, want CARD", records[1].Alternatives)
	}

	// The second alternative describes 20 of the 24 bytes, and the four it
	// leaves are slack rather than absent: in a file written by a program
	// that used BODY they hold that program's data.
	card := records[1]
	if got := positionOf(t, card, "NUMBER"); got != 1 {
		t.Errorf("NUMBER is at %d, want 1", got)
	}
	if got := positionOf(t, card, "EXPIRY"); got != 17 {
		t.Errorf("EXPIRY is at %d, want 17", got)
	}
	if got := slackWidths(card.Root); len(got) != 1 || got[0] != 4 {
		t.Fatalf("the alternative's slack is %v bytes, want one run of 4", got)
	}
}

// TestChainedRedefinesAreOneRunOfBytesDescribedThreeWays holds a chain — B
// redefining A and C redefining B — to being one cluster with three
// alternatives rather than two clusters overlapping.
func TestChainedRedefinesAreOneRunOfBytesDescribedThreeWays(t *testing.T) {
	t.Parallel()

	records := resolveAll(t, `01 R.
   05 A PIC X(10).
   05 B REDEFINES A PIC X(6).
   05 C REDEFINES B PIC X(4).
   05 Z PIC X(2).
`)

	if len(records) != 3 {
		t.Fatalf("resolved to %d records, want 3", len(records))
	}
	for _, record := range records {
		if got := record.Extent(); got != 12 {
			t.Errorf("an alternative is %d bytes, want 12", got)
		}
		if got := positionOf(t, record, "Z"); got != 10 {
			t.Errorf("Z is at %d in an alternative, want 10", got)
		}
	}
	if got := slackWidths(records[1].Root); len(got) != 1 || got[0] != 4 {
		t.Errorf("the six-byte alternative's slack is %v, want one run of 4", got)
	}
	if got := slackWidths(records[2].Root); len(got) != 1 || got[0] != 6 {
		t.Errorf("the four-byte alternative's slack is %v, want one run of 6", got)
	}
}

// TestTwoRedefinesOutsideATableMultiplyTheAlternatives is the duplication the
// IR declines to soften: two independent choices in one record are four records,
// each carrying the whole of it.
func TestTwoRedefinesOutsideATableMultiplyTheAlternatives(t *testing.T) {
	t.Parallel()

	records := resolveAll(t, `01 R.
   05 A PIC X(4).
   05 B REDEFINES A PIC X(4).
   05 C PIC X(6).
   05 D REDEFINES C PIC X(6).
`)

	if len(records) != 4 {
		t.Fatalf("resolved to %d records, want 4", len(records))
	}
	want := [][]string{{"A", "C"}, {"A", "D"}, {"B", "C"}, {"B", "D"}}
	for i, record := range records {
		if len(record.Alternatives) != 2 {
			t.Fatalf("record %d chose %d alternatives, want 2", i, len(record.Alternatives))
		}
		for j, name := range want[i] {
			if record.Alternatives[j].Name != name {
				t.Errorf("record %d chose %s at %d, want %s", i, record.Alternatives[j].Name, j, name)
			}
		}
	}
}

// TestACopybookWithNoRedefineResolvesToOneRecord is the ordinary case, stated so
// that the enumeration above cannot quietly start applying to every copybook.
func TestACopybookWithNoRedefineResolvesToOneRecord(t *testing.T) {
	t.Parallel()

	records := resolveAll(t, "01 R.\n   05 A PIC X(4).\n   05 B PIC X(6).\n")
	if len(records) != 1 {
		t.Fatalf("resolved to %d records, want 1", len(records))
	}
	if len(records[0].Alternatives) != 0 {
		t.Fatalf("a record with no redefine chose %d alternatives", len(records[0].Alternatives))
	}
}

// slackWidths is every slack node of a subtree, in containment order, so that a
// test can say how many runs there are as well as how wide they are — one node
// per maximal run is a requirement, not an implementation detail.
func slackWidths(node *Node) []int {
	var widths []int
	var visit func(*Node)
	visit = func(n *Node) {
		if n.Kind == KindSlack {
			widths = append(widths, n.Width())
		}
		for _, member := range n.Members {
			visit(member)
		}
		for _, arm := range n.Arms {
			visit(arm.Body)
		}
	}
	visit(node)
	return widths
}
