// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The assertions that a reader under a framing that states a record's length
// builds one decoder for the file rather than one per record, and that the
// writer beside it builds one encoder for the file rather than one per record.
//
// It is stated as a *marginal* cost, in allocations, because that is the one
// form of it a machine cannot disagree with: what one more record of this file
// costs is what its own values cost, and a decoder built for it would stand out
// as two more — the decoder, and the reader over bytes this one already held.
// Nanoseconds are orientation and belong in the benchmark below; the allocation
// count is the assertion, which is the lesson Zaba505/cobol-go#108 closed on.
//
// The two directions are stated the same way and are not the same claim: a
// reader under this framing holds a record's bytes before it decodes them, and
// a writer holds a record's bytes because a predicate is evaluated against what
// is about to go out. Neither fact implies the other, which is why both are
// here.
package chunks

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"testing"
)

// perRecordAllocations is what one more record of this file may cost.
//
// Four, and every one of them is this file's own rather than the decoder's: the
// CHUNK-RECORD the reader hands back, the two strings its items decode into,
// and the segment descriptor word this framing reads in front of the one
// segment each of these records is laid into. Before one decoder served the
// whole file it was six — a codec.Reader built for the record, and a
// bytes.Reader wrapping bytes the reader already held.
//
// An upper bound rather than an equality, so that codec allocating less for a
// field than it does today passes rather than failing on an improvement. What
// it catches is the regression: a decoder back on the margin takes this to six
// whatever else changes.
const perRecordAllocations = 4

// tolerance is the slack the bound is read with, which is what makes the
// comparison one of whole allocations rather than of floating point.
const tolerance = 0.5

// chunkFile is n CHUNK-RECORDs, each laid into one segment.
func chunkFile(tb testing.TB, n int) []byte {
	tb.Helper()

	var b bytes.Buffer

	for i := range n {
		b.Write(segmented(chunkBytes(tb, "C"+strconv.Itoa(i%10), "body"), 24))
	}

	return b.Bytes()
}

// drain reads every record of in and keeps none of them.
func drain(tb testing.TB, in []byte) {
	tb.Helper()

	r, err := NewReader(bytes.NewReader(in), Encoding())
	if err != nil {
		tb.Fatalf("NewReader: %v", err)
	}

	for {
		switch _, err := r.Next(); {
		case errors.Is(err, io.EOF):
			return
		case err != nil:
			tb.Fatalf("Next: %v", err)
		}
	}
}

// TestOneMoreRecordDoesNotCostOneMoreDecoder is what a framing that states a
// record's length buys beyond the check against the extent: the record's bytes
// are in hand before anything decodes them, so one decoder is rewound onto each
// record rather than built over it.
//
// The difference between two files of the same records is what isolates that. A
// reader's fixed costs — the buffered input, the one decoder, the run a record
// is reassembled into — are the same for both files and cancel, so what is left
// is what a record costs, and a decoder on that margin is what this fails on.
//
// Not parallel, and deliberately: [testing.AllocsPerRun] counts the whole
// process's allocations, so a test measuring them cannot run beside one making
// them.
func TestOneMoreRecordDoesNotCostOneMoreDecoder(t *testing.T) {
	const few, many = 4, 20

	short, long := chunkFile(t, few), chunkFile(t, many)

	fewer := testing.AllocsPerRun(20, func() { drain(t, short) })
	more := testing.AllocsPerRun(20, func() { drain(t, long) })

	marginal := (more - fewer) / float64(many-few)

	if marginal > perRecordAllocations+tolerance {
		t.Errorf("one more record of this file allocates %.2f times, and a record of it accounts for %d of them",
			marginal, perRecordAllocations)
	}
}

// BenchmarkReadingAFile is the orientation the assertion above deliberately is
// not: what a record of this file costs in nanoseconds on the machine in front
// of you.
func BenchmarkReadingAFile(b *testing.B) {
	in := chunkFile(b, 100)

	b.ReportAllocs()

	for b.Loop() {
		drain(b, in)
	}
}

// perRecordWriteAllocations is what one more record of this file may cost to
// write.
//
// Five, and every one of them is this file's own rather than the encoder's: the
// two runs codec translates this record's two alphanumeric items into, and the
// three segment descriptor words a 24-byte record laid into segments of eight
// stands in front of. Before one encoder served the whole file it was six — a
// codec.Writer built for the record, over a bytes.Buffer the writer held for it.
//
// The bytes.Buffer is not on that margin and never was, because it was one
// field of the Writer reused between records. What its removal buys is the
// allocation the *first* record made growing it, and one field of a type an
// adopter reads; what this constant records is the construction, which was on
// the margin and is what a regression would put back.
//
// An upper bound rather than an equality, so that codec allocating less for a
// field than it does today passes rather than failing on an improvement. What
// it catches is the regression: an encoder back on the margin takes this to six
// whatever else changes.
const perRecordWriteAllocations = 5

// chunkRecords is n CHUNK-RECORDs as values, which is what a caller hands the
// generated writer.
func chunkRecords(n int) []Record {
	out := make([]Record, n)

	for i := range out {
		out[i] = &ChunkRecord{ChunkId: "C" + strconv.Itoa(i%10), ChunkBody: "body"}
	}

	return out
}

// emit writes every record of recs and keeps none of the bytes. It is [drain]
// in the other direction, and io.Discard is the counterpart of dropping what a
// read produced: what a destination costs is not what is being counted.
func emit(tb testing.TB, recs []Record) {
	tb.Helper()

	w, err := NewWriter(io.Discard, Encoding())
	if err != nil {
		tb.Fatalf("NewWriter: %v", err)
	}

	for _, rec := range recs {
		if err := w.Write(rec); err != nil {
			tb.Fatalf("Write: %v", err)
		}
	}

	if err := w.Close(); err != nil {
		tb.Fatalf("Close: %v", err)
	}
}

// TestOneMoreRecordDoesNotCostOneMoreEncoder is the writing half of
// [TestOneMoreRecordDoesNotCostOneMoreDecoder], and it is isolated the same
// way: the difference between writing two files of the same records is what a
// record costs, since a writer's fixed costs — the one encoder and the buffer
// it lays every record into — are the same for both and cancel.
//
// The records are built outside the measurement rather than inside it. What is
// being counted is what writing one costs, and a caller who already holds the
// records they mean to write is the case a generated writer is for.
//
// Not parallel, and deliberately: [testing.AllocsPerRun] counts the whole
// process's allocations, so a test measuring them cannot run beside one making
// them.
func TestOneMoreRecordDoesNotCostOneMoreEncoder(t *testing.T) {
	const few, many = 4, 20

	short, long := chunkRecords(few), chunkRecords(many)

	fewer := testing.AllocsPerRun(20, func() { emit(t, short) })
	more := testing.AllocsPerRun(20, func() { emit(t, long) })

	marginal := (more - fewer) / float64(many-few)

	if marginal > perRecordWriteAllocations+tolerance {
		t.Errorf("one more record of this file allocates %.2f times to write, and a record of it accounts for %d of them",
			marginal, perRecordWriteAllocations)
	}
}

// BenchmarkWritingAFile is the orientation the assertion above deliberately is
// not: what writing a record of this file costs in nanoseconds on the machine
// in front of you.
func BenchmarkWritingAFile(b *testing.B) {
	recs := chunkRecords(100)

	b.ReportAllocs()

	for b.Loop() {
		emit(b, recs)
	}
}
