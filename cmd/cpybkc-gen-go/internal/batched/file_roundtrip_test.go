// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The assertions over docs/ir/SPEC.md's "A batch boundary is told by the order":
// a file that is a sequence of batches, whose header and whose detail carry
// their type codes at offsets that do not line up.
//
// Nothing in the bytes separates the two transitions leaving the state after a
// header. Their runs share no byte, so no literal either record could carry
// tells them apart, and the order the state carries is the whole of it — try the
// header's test first and read anything that fails it as a detail, which is what
// the vendor reader this shape comes from does.
//
// What that costs the writer is the other half of these assertions. A detail
// whose account key opens with the header's literal satisfies the header's test
// at the header's own run, so this file's own reader admits it as a header and
// the batch splits in a place nobody asked for. The generated writer evaluates
// the earlier transition's predicate against the bytes it is about to emit and
// refuses such a record, which is docs/ir/SPEC.md's "A writer walks the same
// automaton" spending the order on the writing side too (#333).
package batched

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Zaba505/cobol-go/codec"
)

// laidOut is a record's bytes, written item by item. The framing is unframed, so
// a record is its extent and nothing stands around it.
func laidOut(t *testing.T, items func(*codec.Writer) error) []byte {
	t.Helper()

	var b bytes.Buffer

	w, err := codec.NewWriter(&b, Encoding())
	if err != nil {
		t.Fatalf("codec.NewWriter: %v", err)
	}

	if err := items(w); err != nil {
		t.Fatalf("laying the record out: %v", err)
	}

	return b.Bytes()
}

// headerBytes is one BATCH-HEADER: the type code that opens it, and what the
// batch is of.
func headerBytes(t *testing.T, name string) []byte {
	t.Helper()

	return laidOut(t, func(w *codec.Writer) error {
		if err := w.WriteAlphanumeric("HD", 2); err != nil {
			return err
		}

		return w.WriteAlphanumeric(name, 18)
	})
}

// detailBytes is one BATCH-DETAIL: an account key, then the type code ten bytes
// in, then a memo.
func detailBytes(t *testing.T, account, memo string) []byte {
	t.Helper()

	return laidOut(t, func(w *codec.Writer) error {
		if err := w.WriteAlphanumeric(account, 10); err != nil {
			return err
		}

		if err := w.WriteAlphanumeric("DT", 2); err != nil {
			return err
		}

		return w.WriteAlphanumeric(memo, 8)
	})
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

// write is every record written back out, through the generated writer, and the
// error the writer reported where it refused one.
func write(t *testing.T, records []Record) ([]byte, error) {
	t.Helper()

	var b bytes.Buffer

	w, err := NewWriter(&b, Encoding())
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	for _, rec := range records {
		if err := w.Write(rec); err != nil {
			return b.Bytes(), err
		}
	}

	if err := w.Close(); err != nil {
		return b.Bytes(), err
	}

	return b.Bytes(), nil
}

// TestASequenceOfBatchesRoundTrips is the shape the permission was written for:
// two batches, each a header and the details behind it, read back and written
// out again byte for byte.
func TestASequenceOfBatchesRoundTrips(t *testing.T) {
	t.Parallel()

	want := bytes.Join([][]byte{
		headerBytes(t, "APRIL POSTINGS"),
		detailBytes(t, "0000000001", "FIRST"),
		detailBytes(t, "0000000002", "SECOND"),
		headerBytes(t, "MAY POSTINGS"),
		detailBytes(t, "0000000003", "THIRD"),
	}, nil)

	records := read(t, want)
	if len(records) != 5 {
		t.Fatalf("the file holds five records and the reader produced %d", len(records))
	}

	for at, kind := range []bool{true, false, false, true, false} {
		_, header := records[at].(*BatchHeader)
		if header != kind {
			t.Fatalf("record %d read back as a %T", at+1, records[at])
		}
	}

	got, err := write(t, records)
	if err != nil {
		t.Fatalf("writing the batches back: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("the file does not write back the bytes it was read from\n got: % x\nwant: % x", got, want)
	}
}

// TestADetailForgingTheHeadersLiteralIsRefused is the check the permission is
// adopted with: the writer evaluates the transition ordered ahead of the one it
// took against the bytes it is about to emit, and reports a record that matches
// it rather than emitting one.
//
// The detail here is well formed in every other way — its own type code is where
// its own discriminator reads, and the transition admitting it is eligible — and
// it is refused all the same, because its account key opens with the two bytes
// the header's discriminator reads.
func TestADetailForgingTheHeadersLiteralIsRefused(t *testing.T) {
	t.Parallel()

	got, err := write(t, []Record{
		&BatchHeader{HdrType: "HD", HdrName: "APRIL POSTINGS"},
		&BatchDetail{DtlAccount: "HD00000001", DtlType: "DT", DtlMemo: "FORGED"},
	})
	if err == nil {
		t.Fatalf("a detail carrying the header's literal at the header's run was written out as % x", got)
	}

	if written := len(headerBytes(t, "APRIL POSTINGS")); len(got) != written {
		t.Errorf("the refused record left %d bytes behind and only the header's %d should have gone out", len(got)-written, written)
	}

	for _, says := range []string{
		"BATCH-DETAIL", // the record refused
		"HDR-TYPE",     // the item whose value those bytes would be read as
		"bytes 0:2",    // the run that did it
		"BATCH-HEADER", // the record type whose boundary it would forge
	} {
		if !strings.Contains(err.Error(), says) {
			t.Errorf("the refusal reads %q and does not name %s", err, says)
		}
	}
}

// TestTheForgedDetailIsWhatTheReaderWouldHaveAdmitted is why the refusal above
// is not pedantry.
//
// The same record, laid out by hand and read back, comes out as a BATCH-HEADER:
// the bytes matched a predicate the descriptor carries, so nothing reports an
// undescribed record, and the two record types share an extent, so the framing
// has nothing to disagree with either. The mis-split is not diagnosed anywhere
// downstream, which is what makes refusing it at the writer the only place it
// can be caught.
func TestTheForgedDetailIsWhatTheReaderWouldHaveAdmitted(t *testing.T) {
	t.Parallel()

	in := bytes.Join([][]byte{
		headerBytes(t, "APRIL POSTINGS"),
		detailBytes(t, "HD00000001", "FORGED"),
	}, nil)

	records := read(t, in)
	if len(records) != 2 {
		t.Fatalf("the file holds two records and the reader produced %d", len(records))
	}

	if _, header := records[1].(*BatchHeader); !header {
		t.Fatalf("a detail whose account key opens with the header's literal read back as a %T, and the writer's refusal is about a reader that admits it as a BatchHeader", records[1])
	}
}

// TestADetailWhoseAccountMerelyResemblesTheLiteralIsWritten holds the check to
// the run it is about.
//
// The header's discriminator reads bytes 0:2 and nothing else, so an account key
// carrying those two bytes anywhere but at its own first two is not a forgery
// and is not refused. A check keyed on the literal rather than on the run would
// refuse this record, and would refuse a file the layout describes.
func TestADetailWhoseAccountMerelyResemblesTheLiteralIsWritten(t *testing.T) {
	t.Parallel()

	want := bytes.Join([][]byte{
		headerBytes(t, "APRIL POSTINGS"),
		detailBytes(t, "00HD000001", "ORDINARY"),
	}, nil)

	got, err := write(t, []Record{
		&BatchHeader{HdrType: "HD", HdrName: "APRIL POSTINGS"},
		&BatchDetail{DtlAccount: "00HD000001", DtlType: "DT", DtlMemo: "ORDINARY"},
	})
	if err != nil {
		t.Fatalf("writing a detail whose account merely carries those bytes elsewhere: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("the file is not the bytes those two records lay out\n got: % x\nwant: % x", got, want)
	}
}
