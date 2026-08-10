// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The assertions over a delimited dataset under **optional terminator**: a
// delimiter follows every record, except that the file MAY end after the last
// one without.
//
// It is a member because real files need it — a shop's extract carries the
// final delimiter on Tuesday and not on Wednesday, out of the same program over
// the same data. What it gives up is stated with it: under it a final record
// whose bytes were cut off is indistinguishable from one whose delimiter was
// never written.
//
// A writer under it is not lenient at all. It emits the final delimiter rather
// than choosing whether to, because two writers left to decide produce two
// different files from one descriptor and one set of records.
package opt

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/Zaba505/cobol-go/codec"
)

// lineBytes is one LINE-RECORD, with no framing around it.
func lineBytes(t *testing.T, text string) []byte {
	t.Helper()

	var b bytes.Buffer

	w, err := codec.NewWriter(&b, Encoding())
	if err != nil {
		t.Fatalf("codec.NewWriter: %v", err)
	}

	if err := w.WriteAlphanumeric(text, 5); err != nil {
		t.Fatalf("LINE-TEXT: %v", err)
	}

	if err := w.WritePackedInt32(15250, 5, codec.Signed); err != nil {
		t.Fatalf("LINE-AMOUNT: %v", err)
	}

	return b.Bytes()
}

// terminated is the records with a delimiter behind each, and last says whether
// the file carries the final one.
func terminated(records [][]byte, last bool) []byte {
	var b bytes.Buffer

	for i, raw := range records {
		b.Write(raw)

		if last || i != len(records)-1 {
			b.WriteByte(0x15)
		}
	}

	return b.Bytes()
}

// read is every record of in, through the generated reader.
func read(t *testing.T, in []byte) []Record {
	t.Helper()

	r, err := NewReader(bytes.NewReader(in), Encoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	var out []Record

	for {
		rec, err := r.Next()
		if errors.Is(err, io.EOF) {
			return out
		}

		if err != nil {
			t.Fatalf("Next: %v", err)
		}

		out = append(out, rec)
	}
}

// TestBothTuesdaysFileAndWednesdaysRead is the whole point of the member: the
// same records with the final delimiter and without it are the same file.
func TestBothTuesdaysFileAndWednesdaysRead(t *testing.T) {
	t.Parallel()

	raw := [][]byte{lineBytes(t, "ONE"), lineBytes(t, "TWO")}

	for name, in := range map[string][]byte{
		"with the final delimiter":    terminated(raw, true),
		"without the final delimiter": terminated(raw, false),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			records := read(t, in)
			if len(records) != 2 {
				t.Fatalf("the file holds two records and the reader produced %d", len(records))
			}

			for i, text := range []string{"ONE", "TWO"} {
				line, ok := records[i].(*LineRecord)
				if !ok {
					t.Fatalf("record %d is a %T", i+1, records[i])
				}

				if line.LineText != text {
					t.Errorf("record %d came back as %q, want %q", i+1, line.LineText, text)
				}
			}
		})
	}
}

// TestAWriterEmitsTheFinalDelimiterRatherThanChoosingWhetherTo is the rule that
// makes a writer under this placement not lenient at all.
//
// A file read without its final delimiter is written back with one, which is
// the one way a file of this framing is not byte-identical — and it is
// deliberate, for the reason a writer never invents a discriminating value.
func TestAWriterEmitsTheFinalDelimiterRatherThanChoosingWhetherTo(t *testing.T) {
	t.Parallel()

	raw := [][]byte{lineBytes(t, "ONE"), lineBytes(t, "TWO")}

	for name, in := range map[string][]byte{
		"a file that carried one":  terminated(raw, true),
		"a file that carried none": terminated(raw, false),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var b bytes.Buffer

			w, err := NewWriter(&b, Encoding())
			if err != nil {
				t.Fatalf("NewWriter: %v", err)
			}

			for _, rec := range read(t, in) {
				if err := w.Write(rec); err != nil {
					t.Fatalf("Write: %v", err)
				}
			}

			if err := w.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			if want := terminated(raw, true); !bytes.Equal(b.Bytes(), want) {
				t.Errorf("the file came out as\n got: % x\nwant: % x", b.Bytes(), want)
			}
		})
	}
}

// TestARecordCutShortIsStillReported is what this placement does not give up.
// It cannot tell a final record whose delimiter was never written from one
// whose bytes were cut off — but a record that stops part-way through its own
// extent is still a file cut short, because the extent is what says where a
// record ends.
func TestARecordCutShortIsStillReported(t *testing.T) {
	t.Parallel()

	whole := terminated([][]byte{lineBytes(t, "ONE")}, false)

	r, err := NewReader(bytes.NewReader(whole[:len(whole)-2]), Encoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	_, err = r.Next()
	if err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("a record cut short read as %v", err)
	}
}
