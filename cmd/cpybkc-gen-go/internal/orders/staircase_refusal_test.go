// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The refusal of a binary width staircase that is not the descriptor's.
//
// These live inside the golden package because the claim is about this
// package's own entry points: the record methods a caller's codec.Reader or
// codec.Writer reaches directly. The four axes a layout states may be replaced;
// the staircase is the one every offset here was computed under, so a caller
// handing another is refused before any byte is read or written — rather than
// being told, a record later, that the data is wrong (docs/ir/SPEC.md, "A
// binary item's width is the staircase, not the digits").
//
// NewReader and NewWriter are not among them. They take an option per axis a
// layout states and none for the staircase, so a reader or a writer this
// package builds is always under the descriptor's (#381).
package orders

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Zaba505/cobol-go/codec"
)

// otherStaircases is every staircase codec has that is not [Encoding]'s, by
// the name codec gives it.
//
// Two of them are the ones #382 was found with. Under `full` the first
// ORDER-RECORD used to fail on QUANTITY with a message about the value not
// fitting its field — a message about the data, for a mistake in the call.
// Under `1-2-4-8` the file used to read cleanly, and only because none of this
// descriptor's binary items has one or two digits, the one row where it and
// `2-4-8` differ.
func otherStaircases() map[string]codec.BinarySize {
	return map[string]codec.BinarySize{
		"full":    codec.BinarySizeFull,
		"1-2-4-8": codec.BinarySize1248,
		"1--8":    codec.BinarySizeSmallest,
	}
}

// under is [Encoding] with its staircase replaced.
func under(binary codec.BinarySize) codec.Encoding {
	enc := Encoding()
	enc.Binary = binary

	return enc
}

// assertStaircaseRefusal holds err to being the refusal of the staircase named
// got, and to nothing about the data.
func assertStaircaseRefusal(t *testing.T, where string, err error, got string) {
	t.Helper()

	if err == nil {
		t.Errorf("%s accepted the staircase %s, and every offset in this package was computed under 2-4-8", where, got)

		return
	}

	for _, want := range []string{
		"binary width staircase is " + got,
		"offsets were computed under the descriptor's, 2-4-8",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s's refusal reads %q and does not say %q", where, err, want)
		}
	}

	if strings.Contains(err.Error(), "does not fit") {
		t.Errorf("%s's refusal reads %q, which blames the data for a mistake in the call", where, err)
	}
}

// everyRecord is every record type this package declares, by the record's name.
func everyRecord() map[string]record {
	return map[string]record{
		"ORDER-RECORD":   &OrderRecord{},
		"TRAILER-RECORD": &TrailerRecord{},
		"SYNC-RECORD":    &SyncRecord{},
		"TABLE-RECORD":   &TableRecord{},
		"ENTRY-RECORD":   &EntryRecord{},
		"ADDR-RECORD":    &AddrRecord{},
		"SHAPE-RECORD":   &ShapeRecord{},
	}
}

// TestARecordMethodUnderAnotherStaircaseIsRefusedBeforeAnyItem is the
// record-level half, through the route a caller's own codec.Reader and
// codec.Writer still offer. Every record refuses it — TRAILER-RECORD holds no
// binary item at all, and the staircase is the descriptor's all the same.
func TestARecordMethodUnderAnotherStaircaseIsRefusedBeforeAnyItem(t *testing.T) {
	t.Parallel()

	// A whole ORDER-RECORD as the descriptor's encoding lays it out, which is
	// the record #382 was found reading.
	order := orderBytes(t, Encoding(), 2)

	for name, binary := range otherStaircases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for recordName, rec := range everyRecord() {
				src := &metered{src: bytes.NewReader(order)}

				r, err := codec.NewReader(src, under(binary))
				if err != nil {
					t.Fatalf("codec.NewReader: %v", err)
				}

				assertStaircaseRefusal(t, recordName+"'s UnmarshalCOBOL", rec.UnmarshalCOBOL(r), name)

				if src.read != 0 || r.Offset() != 0 {
					t.Errorf("%s's UnmarshalCOBOL read %d bytes, to offset %d, under a staircase it refused", recordName, src.read, r.Offset())
				}

				var dst bytes.Buffer

				w, err := codec.NewWriter(&dst, under(binary))
				if err != nil {
					t.Fatalf("codec.NewWriter: %v", err)
				}

				assertStaircaseRefusal(t, recordName+"'s MarshalCOBOL", rec.MarshalCOBOL(w), name)

				if dst.Len() != 0 || len(w.Bytes()) != 0 {
					t.Errorf("%s's MarshalCOBOL wrote % x under a staircase it refused", recordName, dst.Bytes())
				}
			}
		})
	}

	// The descriptor's own staircase reads the same record through the same
	// route, so what was refused above is the staircase and nothing else.
	var rec OrderRecord

	if out := roundTrip(t, Encoding(), &rec, order); !bytes.Equal(out, order) {
		t.Errorf("ORDER-RECORD under the descriptor's own staircase wrote back % x, and was read from % x", out, order)
	}
}

// uncomparable is a Charset a caller may legitimately hand codec whose dynamic
// type Go cannot compare: a struct carrying a slice.
type uncomparable struct {
	codec.Charset

	table []byte
}

// TestTheStaircaseIsComparedAloneAndNeverTheWholeEncoding holds the refusal
// to comparing codec.BinarySize values: an Encoding carrying a Charset whose
// type is not comparable is refused for its staircase and accepted without it,
// and neither panics — through a record's own methods, and through a reader
// and a writer handed that charset as an option.
func TestTheStaircaseIsComparedAloneAndNeverTheWholeEncoding(t *testing.T) {
	t.Parallel()

	charset := uncomparable{Charset: Encoding().Charset, table: []byte{0}}

	enc := Encoding()
	enc.Charset = charset

	other := enc
	other.Binary = codec.BinarySizeFull

	r, err := codec.NewReader(bytes.NewReader(nil), other)
	if err != nil {
		t.Fatalf("codec.NewReader: %v", err)
	}

	assertStaircaseRefusal(t, "UnmarshalCOBOL", new(EntryRecord).UnmarshalCOBOL(r), "full")

	w, err := codec.NewWriter(&bytes.Buffer{}, other)
	if err != nil {
		t.Fatalf("codec.NewWriter: %v", err)
	}

	assertStaircaseRefusal(t, "MarshalCOBOL", new(EntryRecord).MarshalCOBOL(w), "full")

	if _, err := NewReader(bytes.NewReader(nil), WithCharset(charset)); err != nil {
		t.Errorf("NewReader refused an uncomparable charset: %v", err)
	}

	if _, err := NewWriter(&bytes.Buffer{}, WithCharset(charset)); err != nil {
		t.Errorf("NewWriter refused an uncomparable charset: %v", err)
	}
}
