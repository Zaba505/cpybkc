// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Zaba505/cobol-go/copybook"
)

// The widths below are read off `cobol-go`'s codec/SPEC.md, "Storage Widths",
// through its Appendix A test vectors: every item Appendix A gives bytes for
// appears here with the number of bytes it gives. Reading them from the vectors
// rather than from the prose is deliberate — a width is right when the bytes a
// reader has to consume are that many, and Appendix A is where those bytes are
// written down. The conformance corpus (#66) takes the same vectors the other
// way round, as data through a generated reader.

// TestWidthsFollowTheStorageWidthVectors resolves one item per Appendix A row
// and holds its width to the bytes the row gives it.
func TestWidthsFollowTheStorageWidthVectors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		picture string
		vector  string
		want    int
	}{
		// A.1 and A.2, zoned decimal. A separate sign is a byte of its
		// own and an overpunched one is not, which is the whole of the
		// difference between the last two rows.
		{name: "signed zoned", picture: "PIC S9(5)", vector: "A.1 `F1 F2 F3 F4 C5`", want: 5},
		{name: "unsigned zoned", picture: "PIC 9(5)", vector: "A.2 `F1 F2 F3 F4 F5`", want: 5},
		{
			name:    "zoned with a leading separate sign",
			picture: "PIC S9(5) SIGN LEADING SEPARATE",
			vector:  "A.2 `60 F1 F2 F3 F4 F5`",
			want:    6,
		},
		{
			name:    "zoned with a trailing separate sign",
			picture: "PIC S9(5) SIGN TRAILING SEPARATE",
			vector:  "A.2 `F1 F2 F3 F4 F5 4E`",
			want:    6,
		},
		{
			name:    "zoned with a leading overpunched sign",
			picture: "PIC S9(5) SIGN LEADING",
			vector:  "A.2 `D1 F2 F3 F4 F5`",
			want:    5,
		},

		// A.4, packed decimal: one nibble per digit plus the sign
		// nibble, rounded up to a byte. Scale is not stored, which is
		// why S9(3)V99 is the same three bytes as S9(5).
		{name: "packed five digits", picture: "PIC S9(5) COMP-3", vector: "A.4 `12 34 5C`", want: 3},
		{name: "packed unsigned", picture: "PIC 9(5) COMP-3", vector: "A.4 `12 34 5F`", want: 3},
		{name: "packed four digits", picture: "PIC S9(4) COMP-3", vector: "A.4 `01 23 4D`", want: 3},
		{name: "packed with a scale", picture: "PIC S9(3)V99 COMP-3", vector: "A.4 `12 34 5D`", want: 3},

		// A.5, binary: the width is a function of the *digit count* and
		// not of the value, which is the staircase a generator must
		// never rediscover.
		{name: "binary four digits", picture: "PIC S9(4) COMP", vector: "A.5 `04 D2`", want: 2},
		{name: "binary nine digits", picture: "PIC S9(9) COMP", vector: "A.5 `07 5B CD 15`", want: 4},
		{name: "binary unsigned four digits", picture: "PIC 9(4) COMP", vector: "A.5 `FF FF`", want: 2},

		// A.6, floating point: a width fixed by the usage, with no
		// PICTURE to read it from.
		{name: "single-precision float", picture: "COMP-1", vector: "A.6 `3F 80 00 00`", want: 4},
		{name: "double-precision float", picture: "COMP-2", vector: "A.6 `3F F0 00 00 00 00 00 00`", want: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			record := resolveOne(t, fmt.Sprintf("01 R.\n   05 A %s.\n", tc.picture))
			if got := widthOf(t, record, "A"); got != tc.want {
				t.Fatalf("%s resolved to %d bytes, want %d from %s", tc.picture, got, tc.want, tc.vector)
			}
		})
	}
}

// TestBinaryWidthsFollowTheDigitStaircase walks the whole staircase rather than
// the two rungs Appendix A happens to use, because the rung a digit count lands
// on is exactly the knowledge docs/ir/SPEC.md forbids a generator to have.
func TestBinaryWidthsFollowTheDigitStaircase(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		digits int
		want   int
	}{
		{digits: 1, want: 2},
		{digits: 4, want: 2},
		{digits: 5, want: 4},
		{digits: 9, want: 4},
		{digits: 10, want: 8},
		{digits: 18, want: 8},
	} {
		t.Run(fmt.Sprintf("%d digits", tc.digits), func(t *testing.T) {
			t.Parallel()

			record := resolveOne(t, fmt.Sprintf("01 R.\n   05 A PIC S9(%d) COMP.\n", tc.digits))
			if got := widthOf(t, record, "A"); got != tc.want {
				t.Fatalf("PIC S9(%d) COMP resolved to %d bytes, want %d", tc.digits, got, tc.want)
			}
		})
	}
}

// TestTheMixedRecordSumsToItsVectorLength is Appendix A.7 end to end: four
// items of four usages, the record they add up to, and where each one starts in
// the bytes the vector gives.
func TestTheMixedRecordSumsToItsVectorLength(t *testing.T) {
	t.Parallel()

	record := resolveOne(t, `01 TXN.
   05 ID PIC X(4).
   05 AMT PIC S9(5) COMP-3.
   05 QTY PIC S9(4) COMP.
   05 NAME PIC X(6).
`)

	// `C1 F1 F2 F3` `12 34 5D` `04 D2` `E6 C9 C4 C7 C5 E3`
	for _, tc := range []struct {
		name  string
		at    int
		width int
	}{
		{name: "ID", at: 0, width: 4},
		{name: "AMT", at: 4, width: 3},
		{name: "QTY", at: 7, width: 2},
		{name: "NAME", at: 9, width: 6},
	} {
		if got := positionOf(t, record, tc.name); got != tc.at {
			t.Errorf("%s is at %d, want %d", tc.name, got, tc.at)
		}
		if got := widthOf(t, record, tc.name); got != tc.width {
			t.Errorf("%s is %d bytes, want %d", tc.name, got, tc.width)
		}
	}

	if got := record.Extent(); got != 15 {
		t.Fatalf("the record is %d bytes, want the vector's 15", got)
	}
}

// TestWidthOnlyItemsKeepTheFieldsBehindThemInPlace is the requirement that an
// item carrying no logical value a generator can use still carries a width.
//
// Numeric-edited, INDEX and POINTER are the three of the four this repository's
// dependency models; a `NATIONAL` item is not something `cobol-go`'s copybook
// reader has a usage for, so there is no width for it to get wrong and nothing
// here can assert one. The property being held is the same for all of them: the
// item is a term of the sum whether or not anything can be decoded out of it.
func TestWidthOnlyItemsKeepTheFieldsBehindThemInPlace(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		picture string
		want    int
	}{
		{name: "numeric-edited", picture: "PIC ZZZ,ZZ9.99-", want: 11},
		{name: "alphanumeric-edited", picture: "PIC XXBXXBXX", want: 8},
		{name: "index", picture: "USAGE INDEX", want: 4},
		{name: "pointer", picture: "USAGE POINTER", want: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			record := resolveOne(t, fmt.Sprintf(`01 R.
   05 BEFORE PIC X(3).
   05 OPAQUE %s.
   05 AFTER PIC X(5).
`, tc.picture))

			if got := widthOf(t, record, "OPAQUE"); got != tc.want {
				t.Fatalf("%s resolved to %d bytes, want %d", tc.picture, got, tc.want)
			}
			if got := positionOf(t, record, "AFTER"); got != 3+tc.want {
				t.Fatalf("the field behind it is at %d, want %d", got, 3+tc.want)
			}
			if got := record.Extent(); got != 3+tc.want+5 {
				t.Fatalf("the record is %d bytes, want %d", got, 3+tc.want+5)
			}
		})
	}
}

// TestPositionsAgreeWithTheOffsetsTheCopybookWasLaidOutAt is the cross-check
// that matters most, and it is a cross-check rather than a restatement: the
// resolved record carries no offset at all, so the left-hand side is the sum
// over widths and ordering that docs/ir/SPEC.md makes a consumer run, and the
// right-hand side is the offset `cobol-go` computed while walking the copybook.
// The two arrive by different routes and have to meet.
func TestPositionsAgreeWithTheOffsetsTheCopybookWasLaidOutAt(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		`01 FLAT.
   05 A PIC X(3).
   05 B PIC S9(7)V99 COMP-3.
   05 C PIC S9(9) COMP.
   05 D PIC 9(4).
`,
		`01 NESTED.
   05 HDR.
      10 KIND PIC X.
      10 SEQ PIC 9(6).
   05 BODY.
      10 LINE OCCURS 4 TIMES.
         15 SKU PIC X(8).
         15 QTY PIC S9(4) COMP.
      10 TOTAL PIC S9(9)V99 COMP-3.
   05 TRAILER PIC X(2).
`,
		`01 DEEP.
   05 A OCCURS 3 TIMES.
      10 B OCCURS 2 TIMES.
         15 C PIC X(5).
         15 D PIC 9(3).
      10 E PIC X.
   05 F PIC X(9).
`,
	} {
		t.Run(strings.Fields(src)[1], func(t *testing.T) {
			t.Parallel()

			field := recordOf(t, src)
			layout, err := copybook.NewLayout(field, copybook.IBMEnterprise())
			if err != nil {
				t.Fatalf("laying the copybook out: %v", err)
			}

			records, err := Resolve(field, Options{Dialect: copybook.IBMEnterprise()})
			if err != nil {
				t.Fatalf("resolving: %v", err)
			}
			if len(records) != 1 {
				t.Fatalf("resolved to %d records, want 1", len(records))
			}
			record := records[0]

			for _, item := range layout.Items() {
				if item.Field.Filler || item == layout.Record {
					continue
				}
				node := record.Find(item.Field.Name)
				if node == nil {
					t.Fatalf("the resolved record holds no %s", item.Field.Name)
				}
				at, ok := record.Position(node)
				if !ok {
					t.Fatalf("%s has no position in the record it came from", item.Field.Name)
				}
				if at != item.Offset {
					t.Errorf("%s summed to %d, laid out at %d", item.Field.Name, at, item.Offset)
				}
				if got := node.Extent(); got != item.Total() {
					t.Errorf("%s covers %d bytes, laid out over %d", item.Field.Name, got, item.Total())
				}
			}

			if got := record.Extent(); got != layout.Length {
				t.Fatalf("the record summed to %d, laid out at %d", got, layout.Length)
			}
		})
	}
}

// TestEveryByteOfARecordIsCoveredOnce is the premise the position sum rests on,
// asserted rather than assumed: no two members of a group overlap, and no byte
// of the record is left out of the containment order.
func TestEveryByteOfARecordIsCoveredOnce(t *testing.T) {
	t.Parallel()

	record := resolveOne(t, `01 R.
   05 A PIC X(3).
   05 G OCCURS 2 TIMES.
      10 B PIC S9(4) COMP.
      10 C PIC X.
   05 D PIC X(4).
`)

	var check func(*Node)
	check = func(node *Node) {
		if node.Kind != KindGroup {
			return
		}
		sum := 0
		for _, member := range node.Members {
			sum += member.Extent()
			check(member)
		}
		if sum != node.Width() {
			t.Errorf("a group's members cover %d bytes of its %d", sum, node.Width())
		}
	}
	check(record.Root)
}
