// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The round-trip assertions over the code cpybkc generated for ../ledger.sexpr.
//
// They live *inside* the generated package rather than beside it, for the
// reason the golden packages' do: a `_test.go` file is not part of the package,
// so it is not output and the regeneration test does not hold it against
// anything, and one of the assertions below cannot be stated from outside — a
// credit posting redefines its body four bytes short, so the bytes it retains
// for that run sit in an unexported field and holding a record across a later
// read is something only code in this package can do.
//
// What they assert is the claim the example exists to make: a file of these
// records reads to values and writes back to the bytes it came from, through
// the framing and the automaton this layout describes rather than around them.
package ledger

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Zaba505/cobol-go/codec"
)

// framed is one record behind the record descriptor word DFSMS defines, which
// is what `(recfm VB)` resolves to: two bytes of length counting the word
// itself, big-endian, and two reserved bytes that are zero.
func framed(raw []byte) []byte {
	stated := len(raw) + 4

	return append([]byte{byte(stated >> 8), byte(stated), 0, 0}, raw...)
}

// laidOut is the bytes of one record, written item by item through codec
// itself rather than through this package, so that what the generated methods
// are held against is not their own output.
func laidOut(t *testing.T, enc codec.Encoding, items func(*codec.Writer) error) []byte {
	t.Helper()

	var b bytes.Buffer

	w, err := codec.NewWriter(&b, enc)
	if err != nil {
		t.Fatalf("codec.NewWriter: %v", err)
	}

	if err := items(w); err != nil {
		t.Fatalf("laying the record out: %v", err)
	}

	return b.Bytes()
}

// headerBytes is one LEDGER-HEADER announcing postings postings.
func headerBytes(t *testing.T, enc codec.Encoding, postings int32) []byte {
	t.Helper()

	return laidOut(t, enc, func(w *codec.Writer) error {
		if err := w.WriteAlphanumeric("01", 2); err != nil {
			return err
		}

		if err := w.WriteAlphanumeric("GL-MAIN", 10); err != nil {
			return err
		}

		if err := w.WriteZonedInt32(202601, 6, codec.SignUnsigned); err != nil {
			return err
		}

		if err := w.WriteAlphanumeric("USD", 3); err != nil {
			return err
		}

		return w.WriteZonedInt32(postings, 3, codec.SignUnsigned)
	})
}

// trailerBytes is one LEDGER-TRAILER.
func trailerBytes(t *testing.T, enc codec.Encoding, count int32) []byte {
	t.Helper()

	return laidOut(t, enc, func(w *codec.Writer) error {
		if err := w.WriteAlphanumeric("99", 2); err != nil {
			return err
		}

		if err := w.WriteZonedInt32(count, 6, codec.SignUnsigned); err != nil {
			return err
		}

		if err := w.WritePackedInt64(-987654321, 15, codec.Signed); err != nil {
			return err
		}

		return w.WriteAlphanumeric("", 8)
	})
}

// key is the ten-byte account number and the two-digit sequence every posting
// opens with, and the type code behind them. It is the shared prefix that puts
// this file's second discriminator twelve bytes in.
func key(w *codec.Writer, account string, sequence int32, code string) error {
	if err := w.WriteAlphanumeric(account, 10); err != nil {
		return err
	}

	if err := w.WriteZonedInt32(sequence, 2, codec.SignUnsigned); err != nil {
		return err
	}

	return w.WriteAlphanumeric(code, 2)
}

// debitBytes is one posting whose body is PST-DEBIT, with code selecting which
// description of PST-TAIL goes behind it.
func debitBytes(t *testing.T, enc codec.Encoding, code string, tail func(*codec.Writer) error) []byte {
	t.Helper()

	return laidOut(t, enc, func(w *codec.Writer) error {
		if err := key(w, "4001200000", 1, code); err != nil {
			return err
		}

		if err := w.WriteAlphanumeric("CC1000", 6); err != nil {
			return err
		}

		if err := w.WritePackedInt64(1234567, 13, codec.Signed); err != nil {
			return err
		}

		if err := w.WriteAlphanumeric("OFFICE SUPPLIES", 15); err != nil {
			return err
		}

		return tail(w)
	})
}

// creditBytes is one posting whose body is PST-CREDIT — four bytes shorter than
// the run it redefines, so the four behind it are slack, and they are written
// here as bytes no item of the record describes.
func creditBytes(t *testing.T, enc codec.Encoding, code string, tail func(*codec.Writer) error) []byte {
	t.Helper()

	return laidOut(t, enc, func(w *codec.Writer) error {
		if err := key(w, "4001200000", 2, code); err != nil {
			return err
		}

		if err := w.WriteAlphanumeric("BANK", 4); err != nil {
			return err
		}

		if err := w.WritePackedInt64(-1234567, 11, codec.Signed); err != nil {
			return err
		}

		if err := w.WriteAlphanumeric("REF-000012345", 14); err != nil {
			return err
		}

		// The four bytes PST-CREDIT leaves undescribed. They are deliberately
		// not spaces: a run of bytes that survives a read is only visibly
		// surviving if it is not the padding a writer would have chosen.
		if err := w.WriteBytes([]byte{0x01, 0x02, 0x03, 0x04}); err != nil {
			return err
		}

		return tail(w)
	})
}

// memoBytes is one posting described by the base PST-BODY.
func memoBytes(t *testing.T, enc codec.Encoding, code string, tail func(*codec.Writer) error) []byte {
	t.Helper()

	return laidOut(t, enc, func(w *codec.Writer) error {
		if err := key(w, "4001200000", 3, code); err != nil {
			return err
		}

		if err := w.WriteAlphanumeric("MEMO ONLY, NO POSTING", 28); err != nil {
			return err
		}

		return tail(w)
	})
}

// plainTail is PST-TAIL, the base description of the second redefined run.
func plainTail(w *codec.Writer) error {
	return w.WriteAlphanumeric("TAIL0001", 8)
}

// refTail is PST-TAIL-REF, the other one.
func refTail(w *codec.Writer) error {
	if err := w.WriteZonedInt32(7, 4, codec.SignUnsigned); err != nil {
		return err
	}

	return w.WriteZonedInt32(42, 4, codec.SignUnsigned)
}

// fileBytes is a whole ledger extract: the header, one posting of every one of
// the six record types the two redefined runs multiply out to, and the trailer.
func fileBytes(t *testing.T) []byte {
	t.Helper()

	enc := Encoding()

	var b bytes.Buffer

	for _, raw := range [][]byte{
		headerBytes(t, enc, 6),
		debitBytes(t, enc, "DA", plainTail),
		debitBytes(t, enc, "DB", refTail),
		creditBytes(t, enc, "CA", plainTail),
		creditBytes(t, enc, "CB", refTail),
		memoBytes(t, enc, "MA", plainTail),
		memoBytes(t, enc, "MB", refTail),
		trailerBytes(t, enc, 6),
	} {
		b.Write(framed(raw))
	}

	return b.Bytes()
}

// read is every record of in, through the generated reader.
func read(t *testing.T, enc codec.Encoding, in []byte) []Record {
	t.Helper()

	r, err := NewReader(bytes.NewReader(in), enc)
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

// write is every record written back out, through the generated writer.
func write(t *testing.T, enc codec.Encoding, records []Record) []byte {
	t.Helper()

	var b bytes.Buffer

	w, err := NewWriter(&b, enc)
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

	return b.Bytes()
}

// recordType is what a record is, as a diagnostic names it.
func recordType(v any) string {
	switch v.(type) {
	case *LedgerHeader:
		return "LedgerHeader"
	case *LedgerTrailer:
		return "LedgerTrailer"
	case *DebitPosting:
		return "DebitPosting"
	case *DebitPostingRef:
		return "DebitPostingRef"
	case *CreditPosting:
		return "CreditPosting"
	case *CreditPostingRef:
		return "CreditPostingRef"
	case *MemoPosting:
		return "MemoPosting"
	case *MemoPostingRef:
		return "MemoPostingRef"
	default:
		return "something else"
	}
}

// TestAMultiRecordFileReadsBackAsTheFileItWas is the claim the worked example
// is for: a file mixing every record type this layout describes reads to
// records and writes back to the bytes it came from.
//
// Bytes rather than values, and a whole file rather than a record, because the
// framing is on the path too: a record descriptor word states the length of the
// record behind it, so a writer that laid a record out differently would be
// visible here even where every field came back equal.
func TestAMultiRecordFileReadsBackAsTheFileItWas(t *testing.T) {
	t.Parallel()

	want := fileBytes(t)

	records := read(t, Encoding(), want)
	if len(records) != 8 {
		t.Fatalf("the file holds eight records and the reader produced %d", len(records))
	}

	// Every one of the six record types the two redefined runs multiply out
	// to, between the header and the trailer, and each one selected by the
	// type code twelve bytes into the record rather than by its position.
	for i, kind := range []any{
		(*LedgerHeader)(nil),
		(*DebitPosting)(nil), (*DebitPostingRef)(nil),
		(*CreditPosting)(nil), (*CreditPostingRef)(nil),
		(*MemoPosting)(nil), (*MemoPostingRef)(nil),
		(*LedgerTrailer)(nil),
	} {
		if got, want := recordType(records[i]), recordType(kind); got != want {
			t.Errorf("record %d came back as a %s, want a %s", i+1, got, want)
		}
	}

	got := write(t, Encoding(), records)
	if !bytes.Equal(got, want) {
		t.Errorf("the file does not write back the bytes it was read from\n got: % x\nwant: % x", got, want)
	}
}

// TestTheBytesACreditPostingDoesNotDescribeSurviveARead is the half of the
// round trip a caller cannot see: PST-CREDIT is four bytes shorter than the run
// it redefines, and a writer that chose padding for those four rather than
// carrying them would still produce a record whose every field was equal.
func TestTheBytesACreditPostingDoesNotDescribeSurviveARead(t *testing.T) {
	t.Parallel()

	records := read(t, Encoding(), fileBytes(t))

	credit, ok := records[3].(*CreditPosting)
	if !ok {
		t.Fatalf("record 4 is a %s, want a CreditPosting", recordType(records[3]))
	}

	if len(credit.slack) != 1 {
		t.Fatalf("the record retains %d runs of slack, want one", len(credit.slack))
	}

	if want := []byte{0x01, 0x02, 0x03, 0x04}; !bytes.Equal(credit.slack[0], want) {
		t.Errorf("the retained run is % x, want % x", credit.slack[0], want)
	}
}

// TestARecordMatchingNoAlternativeIsReportedAgainstItsOrdinal is the negative
// half. Six type codes select six record types and a seventh selects none, and
// a reader that admitted one anyway would decode a run of bytes as whichever
// record type happened to be first.
//
// The ordinal is the assertion. A file is read one record at a time and nothing
// else in the report says where in the file the trouble is, so a diagnostic
// naming the record type and not the position is one an operator cannot act on.
func TestARecordMatchingNoAlternativeIsReportedAgainstItsOrdinal(t *testing.T) {
	t.Parallel()

	enc := Encoding()

	var b bytes.Buffer

	// The header, one posting the layout describes, and then one carrying a
	// type code no `discriminate` form in the layout names.
	b.Write(framed(headerBytes(t, enc, 6)))
	b.Write(framed(debitBytes(t, enc, "DA", plainTail)))
	b.Write(framed(debitBytes(t, enc, "ZZ", plainTail)))

	r, err := NewReader(bytes.NewReader(b.Bytes()), enc)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	for range 2 {
		if _, err := r.Next(); err != nil {
			t.Fatalf("Next: %v", err)
		}
	}

	_, err = r.Next()
	if err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("a record matching no alternative was admitted, and Next reported %v", err)
	}

	if !strings.Contains(err.Error(), "record 3") {
		t.Errorf("the report reads %q and does not say which record of the file it is about", err)
	}

	if !strings.Contains(err.Error(), "the layout does not describe") {
		t.Errorf("the report reads %q and does not say the record is one the layout does not describe", err)
	}
}
