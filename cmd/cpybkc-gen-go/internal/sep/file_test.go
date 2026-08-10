// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The assertions over a delimited dataset whose delimiter stands *between* two
// records: a file of n records carries n-1 of them, and nothing follows the
// last.
//
// What the placement buys is that the end of the file is checkable — a trailing
// delimiter announces a record that is not there — and what it costs the writer
// is that the delimiter goes in front of each record other than the first,
// because a writer does not learn which record is the last until its caller
// stops.
package sep

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Zaba505/cobol-go/codec"
)

// lineBytes is one LINE-RECORD, with no framing around it.
//
// Its amount is +152.50, which is the three bytes 15 25 0C — and 0x15 is this
// file's delimiter. A reader that searched the input for one would cut the
// record in half and emit a record that is wrong rather than absent.
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

// read is every record of in, through the generated reader.
func read(t *testing.T, in []byte) ([]Record, error) {
	t.Helper()

	r, err := NewReader(bytes.NewReader(in), Encoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	var out []Record

	for {
		rec, err := r.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}

		if err != nil {
			return out, err
		}

		out = append(out, rec)
	}
}

// TestASeparatedFileRoundTrips is the placement's own round trip: three records
// and two delimiters, and a reader that never looked for one.
func TestASeparatedFileRoundTrips(t *testing.T) {
	t.Parallel()

	want := bytes.Join([][]byte{
		lineBytes(t, "ONE"), lineBytes(t, "TWO"), lineBytes(t, "THREE"),
	}, []byte{0x15})

	records, err := read(t, want)
	if err != nil {
		t.Fatalf("reading the file: %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("the file holds three records and the reader produced %d", len(records))
	}

	for i, text := range []string{"ONE", "TWO", "THREE"} {
		line, ok := records[i].(*LineRecord)
		if !ok {
			t.Fatalf("record %d is a %T", i+1, records[i])
		}

		if line.LineText != text || line.LineAmount != 15250 {
			t.Errorf("record %d came back as %q and %d", i+1, line.LineText, line.LineAmount)
		}
	}

	var b bytes.Buffer

	w, err := NewWriter(&b, Encoding())
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	for _, rec := range records {
		if err := w.Write(rec); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("the file does not write back the bytes it was read from\n got: % x\nwant: % x", b.Bytes(), want)
	}
}

// TestATrailingDelimiterAnnouncesARecordThatIsNotThere is the detection this
// placement exists for. A consumer MUST report it rather than reading the file
// as complete.
func TestATrailingDelimiterAnnouncesARecordThatIsNotThere(t *testing.T) {
	t.Parallel()

	in := append(lineBytes(t, "ONE"), 0x15)

	_, err := read(t, in)
	if err == nil {
		t.Fatal("a file with a delimiter behind its last record was read as complete")
	}

	if !strings.Contains(err.Error(), "announces a record that is not there") {
		t.Errorf("the report reads %q and does not say what the trailing delimiter announces", err)
	}
}

// TestADelimiterThatIsNotWhereTheExtentEndsIsReported is "The extent governs,
// and framing is checked against it" from this side: the delimiter in front of
// a record is where the record before it ended, and bytes that are not the
// delimiter there are a disagreement.
func TestADelimiterThatIsNotWhereTheExtentEndsIsReported(t *testing.T) {
	t.Parallel()

	// One byte too many between the two records, so the second record's first
	// byte stands where the delimiter should.
	in := bytes.Join([][]byte{lineBytes(t, "ONE"), lineBytes(t, "TWO")}, []byte{0x15, 0x40})

	if _, err := read(t, in); err == nil {
		t.Fatal("a delimiter that is not where the extent ends was read as a well-formed file")
	}
}
