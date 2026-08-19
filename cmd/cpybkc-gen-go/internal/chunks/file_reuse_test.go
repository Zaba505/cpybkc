// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The assertion that a reader under a framing that states a record's length
// builds one decoder for the file rather than one per record.
//
// It is stated as a *marginal* cost, in allocations, because that is the one
// form of it a machine cannot disagree with: what one more record of this file
// costs is what its own values cost, and a decoder built for it would stand out
// as two more — the decoder, and the reader over bytes this one already held.
// Nanoseconds are orientation and belong in the benchmark below; the allocation
// count is the assertion, which is the lesson Zaba505/cobol-go#108 closed on.
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
