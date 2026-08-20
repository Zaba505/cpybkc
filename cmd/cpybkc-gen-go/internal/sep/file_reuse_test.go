// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The same claim the counted package makes, on the other arm of the emitter
// that writes it: a reader under a framing which does not state a record's
// length builds one decoder for the file rather than one per record.
//
// Nothing here binds a bytes register, so the record is decoded straight off
// the buffered input rather than through a tap that keeps what it hands on.
// That is a different line of generated code and so a claim of its own, and the
// margin below is what tells the two apart — one allocation per record on this
// arm against two on that one, because the tap was on that margin as well as
// the decoder.
//
// The placement makes the read-ahead assertion sharper than a terminator can:
// under a separator a delimiter stands *between* two records and nothing
// follows the last, so a decoder that read one byte past a record's extent
// would eat the delimiter the next record is found behind. That is also why
// this arm is asserted from here rather than from fixed or opt, which are the
// other two packages it is emitted into: the three are the same generated
// lines, and this is the framing under which an over-read has the least room
// to hide.
package sep

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/Zaba505/cobol-go/codec"
)

// perRecordAllocations is what one more LINE-RECORD of this file may cost.
//
// Three, and none of them is the decoder's: what is left on the margin is the
// record the reader hands back and what codec decodes its items into. Before
// one decoder served the whole file it was four — a codec.Reader built for the
// record, over the buffered input the reader already held.
//
// An upper bound rather than an equality, so that codec allocating less for a
// field than it does today passes rather than failing on an improvement. What
// it catches is the regression: a decoder back on the margin takes this to four
// whatever else changes.
//
// It is a budget and not an attribution, and the difference matters when it
// fires. Everything on the margin is summed — the decoder this is about, the
// record, and whatever codec allocates for the items — so a decoder coming back
// while something else on the margin went away lands at three and passes. The
// attribution is the sentence above, checked by hand when the number moves;
// nothing here can make it an assertion, because the allocations are
// indistinguishable from outside the package that made them.
const perRecordAllocations = 3

// tolerance is the slack the bound is read with, which is what makes the
// comparison one of whole allocations rather than of floating point.
const tolerance = 0.5

// lineOf is one LINE-RECORD, with no framing around it.
//
// It is [lineBytes] over a [testing.TB], because the assertion here has a
// benchmark beside it and a benchmark is not a [testing.T].
func lineOf(tb testing.TB, text string) []byte {
	tb.Helper()

	var b bytes.Buffer

	w, err := codec.NewWriter(&b, Encoding())
	if err != nil {
		tb.Fatalf("codec.NewWriter: %v", err)
	}

	if err := w.WriteAlphanumeric(text, 5); err != nil {
		tb.Fatalf("LINE-TEXT: %v", err)
	}

	if err := w.WritePackedInt32(15250, 5, codec.Signed); err != nil {
		tb.Fatalf("LINE-AMOUNT: %v", err)
	}

	return b.Bytes()
}

// separatedFile is n LINE-RECORDs with n-1 delimiters between them, which is
// every byte a file under this placement carries.
func separatedFile(tb testing.TB, n int) []byte {
	tb.Helper()

	parts := make([][]byte, n)

	for i := range parts {
		parts[i] = lineOf(tb, "LINE")
	}

	return bytes.Join(parts, []byte{0x15})
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

// TestOneMoreRecordDoesNotCostOneMoreDecoder isolates what a record costs the
// way the chunks and counted packages do: a reader's fixed costs — the buffered
// input and the one decoder — are the same for two files of the same records
// and cancel, so the difference between them is the margin, and a decoder built
// per record is what shows up on it.
//
// Not parallel, and deliberately: [testing.AllocsPerRun] counts the whole
// process's allocations, so a test measuring them cannot run beside one making
// them.
func TestOneMoreRecordDoesNotCostOneMoreDecoder(t *testing.T) {
	const few, many = 4, 20

	short, long := separatedFile(t, few), separatedFile(t, many)

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
	in := separatedFile(b, 100)

	b.ReportAllocs()

	for b.Loop() {
		drain(b, in)
	}
}

// TestCodecDiagnosticsStayRecordRelative is the property the rewind is worth
// nothing without: it puts [codec.Reader.Offset] back to zero, so an offset
// codec reports is counted from the start of the record it is reading rather
// than from the start of the file — which is the offset an adopter can find in
// a copybook. On a record that is not the first the two differ: this record
// begins at byte 18 of the file and the fault is at byte 7 of the record.
//
// The message is held whole rather than searched for a number, because what
// must not change on this path is the diagnostic an adopter reads.
func TestCodecDiagnosticsStayRecordRelative(t *testing.T) {
	t.Parallel()

	in := separatedFile(t, 3)

	// The sign nibble of the third record's LINE-AMOUNT, which is the last byte
	// of the file: under this placement nothing stands behind the last record.
	in[len(in)-1] = 0xff

	_, err := read(t, in)
	if err == nil {
		t.Fatal("a record whose packed field holds a digit nibble of F was read")
	}

	const want = "reading record 3: LINE-RECORD: reading LINE-AMOUNT: at byte offset 7: " +
		"invalid packed decimal digit nibble F: digit nibbles are 0-9"

	if err.Error() != want {
		t.Errorf("the diagnostic is\n got: %s\nwant: %s", err, want)
	}
}

// TestARewindReadsNothingAheadOfTheRecord is what lets the framing and the
// record share one input across a rewind.
//
// codec does not buffer, so a decoder rewound onto this reader's input has
// never read further than the last field it was asked for. Under this placement
// the byte behind a record is the delimiter the next one is found behind, so a
// decoder that read even one byte past a record's extent would take it — and
// the bytes behind the last record are still there to be read as the record
// they are not, which is what this holds: three records come back, and the
// fourth is reported as a file cut short rather than never being seen at all.
//
// The two cases are the distinction the whole path turns on, and this is the
// placement where it is easiest to lose: nothing stands behind the last record,
// so "the file ended" and "the last record was cut short" are the same byte
// position. A record cut part-way through a field ends it with
// io.ErrUnexpectedEOF and one cut at a field boundary ends it with io.EOF, and
// neither is reported as the io.EOF a caller stops on — a %v that became a %w
// would make a truncated file indistinguishable from a complete one.
func TestARewindReadsNothingAheadOfTheRecord(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		trail []byte
		want  string
	}{
		{
			name:  "part-way through a field",
			trail: []byte{0x15, 0xd3, 0xc9},
			want:  "the file ends part-way through record 4: LINE-RECORD: reading LINE-TEXT: at byte offset 2: unexpected EOF",
		},
		{
			name:  "at a field boundary",
			trail: []byte{0x15, 0xd3, 0xc9, 0xd5, 0xc5, 0x40},
			want:  "the file ends part-way through record 4: LINE-RECORD: reading LINE-AMOUNT: at byte offset 5: EOF",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			records, err := read(t, append(separatedFile(t, 3), tc.trail...))
			if err == nil {
				t.Fatal("the bytes behind the last record were read as the end of the file")
			}

			if errors.Is(err, io.EOF) {
				t.Error("a file cut short reports io.EOF, which is what a complete file reports")
			}

			if len(records) != 3 {
				t.Errorf("the file's three records came back as %d", len(records))
			}

			if err.Error() != tc.want {
				t.Errorf("the diagnostic is\n got: %s\nwant: %s", err, tc.want)
			}
		})
	}
}
