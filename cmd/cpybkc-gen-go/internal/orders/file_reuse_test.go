// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The assertions over codecs that are rewound rather than rebuilt: what the
// framing check counts in once the reader holds one decoder for the whole file,
// what a table of variants costs to decode once one sub-decoder serves every
// occurrence of it, and what it costs to encode once one sub-encoder does.
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

	// Anchored on the whole of codec's phrase, colon and all, rather than on the
	// digits: a substring ending at a digit matches every longer number that
	// begins with it, so "offset 23" is in "offset 231" and both assertions
	// below could report a result they had not earned.
	if within := fmt.Sprintf("at byte offset %d:", len(short)); !strings.Contains(err.Error(), within) {
		t.Errorf("the report reads %q and does not put the fault at %q, within the record it is in", err, within)
	}

	// What the same fault would read as if the offset counted from the start of
	// the file rather than from the start of this record.
	if absolute := fmt.Sprintf("at byte offset %d:", len(first)+len(short)); strings.Contains(err.Error(), absolute) {
		t.Errorf("the report reads %q and puts the fault at %q, which is a position in the file", err, absolute)
	}
}

// TestARecordThatFailedLeavesNothingBehindInTheDecoder is the question one
// decoder for the whole file raises and one decoder per record could not: a
// record that failed part-way through leaves the decoder holding an offset, a
// slice and whatever its scratch buffers last held, and the next record is
// decoded through that same decoder.
//
// It is answered by where the rewind stands. [Reader.admit] resets before it
// decodes rather than after, so what a failed record left behind is dropped by
// the record after it whatever the failure was — which is the property this
// reads back, by failing the first record of a file and then reading the same
// record, well framed, through the reader that failed.
//
// The automaton is what makes that file legal: a transition is taken when a
// record is admitted, so a record that failed leaves the walk in the state it
// was in, and the state that admits an ORDER-RECORD admits the next one too.
func TestARecordThatFailedLeavesNothingBehindInTheDecoder(t *testing.T) {
	t.Parallel()

	order := orderBytes(t, Encoding(), 2)

	// The first record, cut short by three bytes of its own framing, so that it
	// fails inside codec with bytes consumed and an offset stopped part-way.
	cut := order[:len(order)-3]

	in := append(framed(cut), framed(order)...)

	r, err := NewReader(bytes.NewReader(in), Encoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	if _, err := r.Next(); err == nil {
		t.Fatal("a record cut short by its own framing was read as a record")
	}

	rec, err := r.Next()
	if err != nil {
		t.Fatalf("the record after a failed one: %v", err)
	}

	got, ok := rec.(*OrderRecord)
	if !ok {
		t.Fatalf("the record after a failed one came back as a %T", rec)
	}

	// Written back rather than compared field by field, because the bytes are
	// what a decoder carrying something over would get wrong: an offset the
	// rewind did not clear moves every item behind it.
	var out bytes.Buffer

	w, err := codec.NewWriter(&out, Encoding())
	if err != nil {
		t.Fatalf("codec.NewWriter: %v", err)
	}

	if err := got.MarshalCOBOL(w); err != nil {
		t.Fatalf("MarshalCOBOL: %v", err)
	}

	if !bytes.Equal(out.Bytes(), order) {
		t.Errorf("the record read after a failed one is\n % x\nand the record in the file is\n % x", out.Bytes(), order)
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
// one pair per occurrence. Six of the sixteen were the multiplier, and it is
// the multiplier this story removed — on a real copybook's OCCURS 100 holding a
// REDEFINES those same six are two hundred, and they become the one decoder
// this record now builds whatever the count is.
//
// It is a cost and not a correctness criterion. That an occurrence decodes to
// the right values, and that no occurrence's bytes bleed into the next through
// the scratch buffers they now share, is
// [TestATableOfVariantsWritesBackTheBytesItWasReadFrom] in
// record_roundtrip_test.go: it round-trips this exact input byte for byte, over
// four arm patterns, and each occurrence's retained slack is a different byte
// so that one standing in for another fails it.
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

// recordWriteAllocations is what encoding one ENTRY-RECORD may cost.
//
// Ten, of which exactly **one** is an encoder: the sub-encoder this record's
// method builds and rewinds onto its own buffer for each of its three
// occurrences. One more is that buffer, which the first occurrence grows and
// every occurrence after it writes into at the capacity it reached.
//
// Before one sub-encoder served every occurrence it was seventeen: three
// encoders, three bytes.Buffers escaping to the heap because an encoder was
// built over each, and the first backing array each of those buffers had to
// allocate — three allocations per occurrence rather than two for the record.
// Nine of the seventeen were the multiplier, and it is the multiplier this
// story removed: on a real copybook's OCCURS 100 holding a REDEFINES those
// nine are three hundred, and they become the two allocations this record now
// makes whatever the count is.
//
// It is a cost and not a correctness criterion. That the bytes written are the
// bytes the record was read from, occurrence by occurrence and slack run by
// slack run, is [TestATableOfVariantsWritesBackTheBytesItWasReadFrom] in
// record_roundtrip_test.go — which matters more here than it did on the
// reading side, because one buffer now serves every occurrence and a rewind
// that failed to truncate would leave the occurrence before it in front of the
// one being written.
//
// An upper bound rather than an equality, so that codec allocating less for a
// field than it does today passes rather than failing on an improvement. What
// it catches is the regression: an encoder back inside the loop over
// occurrences costs three an occurrence whatever else changes.
const recordWriteAllocations = 10

// TestATableOfVariantsBuildsOneEncoderForTheRecord is
// [TestATableOfVariantsBuildsOneDecoderForTheRecord] in the other direction,
// over the same call site: a group that repeats and holds a variant is laid out
// one occurrence at a time, and the encoder over an occurrence's bytes was
// built once per occurrence rather than once per record.
//
// Not parallel, and deliberately: [testing.AllocsPerRun] counts the whole
// process's allocations, so a test measuring them cannot run beside one making
// them.
func TestATableOfVariantsBuildsOneEncoderForTheRecord(t *testing.T) {
	x := entryRecord(t, "DSD")

	cw, err := codec.NewBytesWriter(nil, Encoding())
	if err != nil {
		t.Fatalf("codec.NewBytesWriter: %v", err)
	}

	allocs := testing.AllocsPerRun(20, func() {
		cw.Reset(cw.Bytes())

		if err := x.MarshalCOBOL(cw); err != nil {
			t.Fatalf("MarshalCOBOL: %v", err)
		}
	})

	if allocs > recordWriteAllocations+tolerance {
		t.Errorf("encoding an ENTRY-RECORD allocates %.2f times, and its three occurrences and the one encoder over them account for %d",
			allocs, recordWriteAllocations)
	}
}

// entryRecord is the ENTRY-RECORD [entryBytes] describes, decoded out of those
// bytes rather than built by hand: the arms it holds and the runs retained for
// its slack are what the copybook says they are only if a decode put them
// there.
func entryRecord(tb testing.TB, arms string) *EntryRecord {
	tb.Helper()

	cr, err := codec.NewBytesReader(entryBytes(tb, arms), Encoding())
	if err != nil {
		tb.Fatalf("codec.NewBytesReader: %v", err)
	}

	var x EntryRecord

	if err := x.UnmarshalCOBOL(cr); err != nil {
		tb.Fatalf("UnmarshalCOBOL: %v", err)
	}

	return &x
}

// BenchmarkEncodingATableOfVariants is the orientation the assertion above
// deliberately is not.
func BenchmarkEncodingATableOfVariants(b *testing.B) {
	x := entryRecord(b, "DSD")

	cw, err := codec.NewBytesWriter(nil, Encoding())
	if err != nil {
		b.Fatalf("codec.NewBytesWriter: %v", err)
	}

	b.ReportAllocs()

	for b.Loop() {
		cw.Reset(cw.Bytes())

		if err := x.MarshalCOBOL(cw); err != nil {
			b.Fatalf("MarshalCOBOL: %v", err)
		}
	}
}
