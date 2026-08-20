// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The assertions that a reader under a framing which does *not* state a
// record's length builds one decoder for the file rather than one per record,
// and that rewinding it onto the input leaves the input where building one over
// it did.
//
// The reader in the chunks package makes the same claim under the other kind
// of framing, and the two are not the same claim. There a record's bytes
// are in hand before anything decodes them, so the decoder is rewound onto
// bytes; here the framing says nothing about where a record ends, so the
// decoder is rewound onto the input the framing itself is read from, and what
// has to hold is that a rewind neither reads ahead nor drops what it has not
// read. Three things below are that property, from three sides.
//
// This is also the arm that keeps a record's own bytes: a binding here writes a
// bytes register, so the record is decoded through a tap that copies what it
// hands on. The tap is hoisted onto the reader rather than built per record,
// and the count below is why — it escapes into the decoder, so one per record
// is one allocation per record however cheap the decoder became.
package counted

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/Zaba505/cobol-go/codec"
)

// perRecordAllocations is what one more DETAIL-RECORD of this file may cost.
//
// Two, and neither is the decoder's: what is left on the margin is the record
// the reader hands back and what codec decodes its items into. Before one
// decoder served the whole file it was four — a codec.Reader built for the
// record, and the recording tap that Reader read the record's bytes through,
// which escaped into it and so could not be hoisted separately.
//
// An upper bound rather than an equality, so that codec allocating less for a
// field than it does today passes rather than failing on an improvement. What
// it catches is the regression: a decoder back on the margin takes this to four
// whatever else changes.
const perRecordAllocations = 2

// tolerance is the slack the bound is read with, which is what makes the
// comparison one of whole allocations rather than of floating point.
const tolerance = 0.5

// laid is a record's bytes, written item by item, with the delimiter behind it
// — which is where a terminator stands.
//
// It is [laidOut] over a [testing.TB], because the assertions here have a
// benchmark beside each of them and a benchmark is not a [testing.T].
func laid(tb testing.TB, items func(*codec.Writer) error) []byte {
	tb.Helper()

	var b bytes.Buffer

	w, err := codec.NewWriter(&b, Encoding())
	if err != nil {
		tb.Fatalf("codec.NewWriter: %v", err)
	}

	if err := items(w); err != nil {
		tb.Fatalf("laying the record out: %v", err)
	}

	b.Write([]byte{0x15})

	return b.Bytes()
}

// countedRun is one group: a header stating how many details follow it, and
// those details. Its flag is N, so no summary record stands behind them and the
// state the run ends in accepts.
//
// Each detail's amount is +152.50, which is the three bytes 15 25 0C — the
// first of them this file's delimiter, so a reader that read one byte further
// than a record's extent would find the wrong thing where the terminator is.
func countedRun(tb testing.TB, details int) []byte {
	tb.Helper()

	parts := [][]byte{laid(tb, func(w *codec.Writer) error {
		if err := w.WriteAlphanumeric("H", 1); err != nil {
			return err
		}

		if err := w.WriteZonedInt32(int32(details), 2, codec.SignUnsigned); err != nil {
			return err
		}

		if err := w.WriteAlphanumeric("N", 1); err != nil {
			return err
		}

		return w.WriteZonedInt32(0, 2, codec.SignUnsigned)
	})}

	for range details {
		parts = append(parts, laid(tb, func(w *codec.Writer) error {
			if err := w.WriteAlphanumeric("D", 1); err != nil {
				return err
			}

			return w.WritePackedInt32(15250, 5, codec.Signed)
		}))
	}

	return bytes.Join(parts, nil)
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

// TestOneMoreRecordDoesNotCostOneMoreDecoder is what a rewind onto a stream
// buys this framing, which is what it could not have before codec had one: the
// record's bytes are never held, so there is nothing to rewind a decoder onto
// with [codec.Reader.Reset], and the decoder is rewound onto the input instead.
//
// The difference between two files of the same records is what isolates that. A
// reader's fixed costs — the buffered input, the one decoder, the tap it reads
// through — are the same for both files and cancel, so what is left is what a
// record costs, and a decoder on that margin is what this fails on.
//
// Not parallel, and deliberately: [testing.AllocsPerRun] counts the whole
// process's allocations, so a test measuring them cannot run beside one making
// them.
func TestOneMoreRecordDoesNotCostOneMoreDecoder(t *testing.T) {
	const few, many = 4, 20

	short, long := countedRun(t, few), countedRun(t, many)

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
	in := countedRun(b, 99)

	b.ReportAllocs()

	for b.Loop() {
		drain(b, in)
	}
}

// TestCodecDiagnosticsStayRecordRelative is the property the rewind is worth
// nothing without.
//
// A rewind puts [codec.Reader.Offset] back to zero, so an offset codec reports
// is counted from the start of the record it is reading rather than from the
// start of the file — which is the offset an adopter can find in a copybook.
// One decoder for the whole file could have made every one of these a file
// offset, and on a record that is not the first the two differ: this record
// begins at byte 12 of the file and the fault is at byte 3 of the record.
//
// The message is held whole rather than searched for a number. What must not
// change on this path is the diagnostic an adopter reads, and an assertion that
// merely finds "3" somewhere in it would pass on a sentence rewritten around
// it.
func TestCodecDiagnosticsStayRecordRelative(t *testing.T) {
	t.Parallel()

	in := countedRun(t, 2)

	// The sign nibble of the second DETAIL-RECORD, which is the third record of
	// the file and the last byte in front of its terminator.
	in[len(in)-2] = 0xff

	_, err := read(t, in)
	if err == nil {
		t.Fatal("a record whose packed field holds a digit nibble of F was read")
	}

	const want = "reading record 3: DETAIL-RECORD: reading AMOUNT: at byte offset 3: " +
		"invalid packed decimal digit nibble F: digit nibbles are 0-9"

	if err.Error() != want {
		t.Errorf("the diagnostic is\n got: %s\nwant: %s", err, want)
	}
}

// TestAShortFileIsStillReportedAtTheRecordItRanOutIn is the other half of the
// same property, and it carries the distinction the whole path turns on: a
// record whose first byte is missing ends the field with io.EOF, and one cut
// part-way through a field ends it with io.ErrUnexpectedEOF. Both are the file
// being cut short rather than the file ending, and neither wraps io.EOF where a
// caller can reach it.
func TestAShortFileIsStillReportedAtTheRecordItRanOutIn(t *testing.T) {
	t.Parallel()

	whole := countedRun(t, 2)

	for _, tc := range []struct {
		name string
		cut  int
		want string
	}{
		{
			name: "part-way through a field",
			cut:  2,
			want: "the file ends part-way through record 3: DETAIL-RECORD: reading AMOUNT: at byte offset 3: unexpected EOF",
		},
		{
			name: "at a field boundary",
			cut:  4,
			want: "the file ends part-way through record 3: DETAIL-RECORD: reading AMOUNT: at byte offset 1: EOF",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := read(t, whole[:len(whole)-tc.cut])
			if err == nil {
				t.Fatal("a file cut short was read as complete")
			}

			if errors.Is(err, io.EOF) {
				t.Error("a file cut short reports io.EOF, which is what a complete file reports")
			}

			if err.Error() != tc.want {
				t.Errorf("the diagnostic is\n got: %s\nwant: %s", err, tc.want)
			}
		})
	}
}

// TestARewindReadsNothingAheadOfTheRecord is the property that lets the framing
// and the record share one input across a rewind, asserted from the far side of
// the last record.
//
// codec does not buffer, so a decoder rewound onto this reader's input has
// never read further than the last field it was asked for — and the whole of
// this framing depends on it, because the terminator behind a record is drawn
// off the same input the record was. Bytes behind the last terminator are
// therefore still there to be read as the record they are not, which is what
// this holds: three records come back, and the fourth is reported as a record
// the layout does not describe rather than never being seen at all.
func TestARewindReadsNothingAheadOfTheRecord(t *testing.T) {
	t.Parallel()

	in := append(countedRun(t, 2), 0xc1, 0xc2, 0x15)

	records, err := read(t, in)
	if err == nil {
		t.Fatal("the bytes behind the last record were read as the end of the file")
	}

	if len(records) != 3 {
		t.Errorf("the file's three records came back as %d", len(records))
	}

	const want = "record 4 is a record the layout does not describe, and the automaton expected one of HEADER-RECORD"

	if err.Error() != want {
		t.Errorf("the diagnostic is\n got: %s\nwant: %s", err, want)
	}
}
