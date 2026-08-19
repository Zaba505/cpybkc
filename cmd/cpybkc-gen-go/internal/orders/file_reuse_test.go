// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The assertions over decoders that are rewound rather than rebuilt: what the
// framing check counts in once the reader holds one decoder for the whole file,
// and what a table of variants costs once one sub-decoder serves every
// occurrence of it.
//
// They live inside the golden package for the reason the round-trip assertions
// beside them do, and they are here rather than in `chunks` because this
// descriptor is the one carrying a table that holds a variant — the call site
// with a multiplier on it, since an OCCURS 100 group holding a REDEFINES paid
// the per-occurrence price a hundred times per record.
package orders

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/Zaba505/cobol-go/codec"
)

// TestTheExtentOfARecordIsCountedFromTheStartOfIt is the framing check as it is
// now expressed: the extent is what the decoder consumed, which is its offset,
// and the offset counts from the last rewind.
//
// The record is the *second* of the file, and that is the whole point of it. A
// reader that built a decoder per record got record-relative offsets by
// accident, because each decoder began at zero; one that rewinds a single
// decoder gets them because a rewind sets the offset back to zero. Asserting
// the number on the second record is what tells those two apart from a reader
// that had simply stopped resetting anything — there, this would read as the
// extent of the file so far.
func TestTheExtentOfARecordIsCountedFromTheStartOfIt(t *testing.T) {
	t.Parallel()

	table := tableBytes(t, Encoding(), 3)

	// Three bytes of framing the record does not describe. The record decodes
	// whole and stops short of what the descriptor word states, which is the
	// disagreement this reports.
	stated := append(append([]byte{}, table...), 0x00, 0x00, 0x00)

	in := append(framed(orderBytes(t, Encoding(), 2)), framed(stated)...)

	r, err := NewReader(bytes.NewReader(in), Encoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	if _, err := r.Next(); err != nil {
		t.Fatalf("the first record: %v", err)
	}

	_, err = r.Next()
	if err == nil {
		t.Fatal("a descriptor word longer than the record it states the length of was read as a record")
	}

	want := fmt.Sprintf("the framing states record 2 is %d bytes and the extent of the record it admits is %d",
		len(stated), len(table))

	if err.Error() != want {
		t.Errorf("the report reads\n %q\nand the record's own lengths are\n %q", err, want)
	}
}

// TestACodecFaultInASecondRecordReportsAnOffsetWithinThatRecord is the same
// property one layer down, where it is not this generator's message but codec's
// own.
//
// codec stamps every read error with the offset it happened at, counted from
// the last rewind. A reader rewinding one decoder per record therefore reports
// where in *this record* the fault is, which is what somebody holding a copybook
// can act on; a reader that had stopped rewinding would report a position in the
// file, and the numbers would be indistinguishable from correct until a record
// went missing.
func TestACodecFaultInASecondRecordReportsAnOffsetWithinThatRecord(t *testing.T) {
	t.Parallel()

	first := framed(orderBytes(t, Encoding(), 2))

	// A TABLE-RECORD three bytes short of the record its own count describes.
	// Its tail is a table of one-byte items, so every byte the framing does
	// state is consumed before the read that runs out — the offset codec
	// reports is the whole of what is there.
	table := tableBytes(t, Encoding(), 3)
	short := table[:len(table)-3]

	r, err := NewReader(bytes.NewReader(append(first, framed(short)...)), Encoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	if _, err := r.Next(); err != nil {
		t.Fatalf("the first record: %v", err)
	}

	_, err = r.Next()
	if err == nil {
		t.Fatal("a record cut short by its own framing was read as a record")
	}

	if within := fmt.Sprintf("offset %d", len(short)); !strings.Contains(err.Error(), within) {
		t.Errorf("the report reads %q and does not put the fault at %q, within the record it is in", err, within)
	}

	// What the same fault would read as if the offset counted from the start of
	// the file rather than from the start of this record.
	if absolute := fmt.Sprintf("offset %d", len(first)+len(short)); strings.Contains(err.Error(), absolute) {
		t.Errorf("the report reads %q and puts the fault at %q, which is a position in the file", err, absolute)
	}
}

// tolerance is the slack the bound below is read with, which is what makes the
// comparison one of whole allocations rather than of floating point.
const tolerance = 0.5

// recordAllocations is what decoding one ENTRY-RECORD may cost.
//
// Eleven, of which exactly **one** is a decoder: the sub-decoder this record's
// method builds and rewinds onto each of its three occurrences. The other ten
// hold something the record hands back — each occurrence's own bytes, which an
// arm's predicate is evaluated against, the string ENTRY-TYPE decodes into, the
// alternative chosen for the occurrence and the values inside it.
//
// Before one sub-decoder served every occurrence it was sixteen: three
// decoders, and the three bytes.Readers wrapping bytes the record already held,
// one pair per occurrence. That is the multiplier this story removed, and on a
// real copybook's OCCURS 100 holding a REDEFINES it is two hundred allocations
// per record rather than six.
//
// An upper bound rather than an equality, so that codec allocating less for a
// field than it does today passes rather than failing on an improvement. What
// it catches is the regression: a decoder back inside the loop over occurrences
// costs two an occurrence whatever else changes.
const recordAllocations = 11

// TestATableOfVariantsBuildsOneDecoderForTheRecord is the call site the whole
// story turns on: a group that repeats and holds a variant is read one
// occurrence at a time, and the decoder over an occurrence's bytes was built
// once per occurrence rather than once per record.
//
// Not parallel, and deliberately: [testing.AllocsPerRun] counts the whole
// process's allocations, so a test measuring them cannot run beside one making
// them.
func TestATableOfVariantsBuildsOneDecoderForTheRecord(t *testing.T) {
	raw := entryBytes(t, "DSD")

	cr, err := codec.NewBytesReader(nil, Encoding())
	if err != nil {
		t.Fatalf("codec.NewBytesReader: %v", err)
	}

	allocs := testing.AllocsPerRun(20, func() {
		cr.Reset(raw)

		var x EntryRecord

		if err := x.UnmarshalCOBOL(cr); err != nil {
			t.Fatalf("UnmarshalCOBOL: %v", err)
		}
	})

	if allocs > recordAllocations+tolerance {
		t.Errorf("decoding an ENTRY-RECORD allocates %.2f times, and its three occurrences and the one decoder over them account for %d",
			allocs, recordAllocations)
	}
}

// BenchmarkDecodingATableOfVariants is the orientation the assertion above
// deliberately is not.
func BenchmarkDecodingATableOfVariants(b *testing.B) {
	raw := entryBytes(b, "DSD")

	cr, err := codec.NewBytesReader(nil, Encoding())
	if err != nil {
		b.Fatalf("codec.NewBytesReader: %v", err)
	}

	b.ReportAllocs()

	for b.Loop() {
		cr.Reset(raw)

		var x EntryRecord

		if err := x.UnmarshalCOBOL(cr); err != nil {
			b.Fatalf("UnmarshalCOBOL: %v", err)
		}
	}
}
