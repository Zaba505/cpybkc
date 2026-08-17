// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The round-trip assertions over the generated file-level reader and writer.
//
// They live inside the golden package for the reason roundtrip_test.go's do:
// a `_test.go` file is not part of the package the golden pins, and one of the
// criteria below cannot be stated from outside — the bytes retained for a slack
// node are unexported, so holding a record across a later read and asserting
// that it still carries its own slack is something only code in this package
// can do.
//
// What roundtrip_test.go asserts is a *record* reading and writing its own
// bytes. What these assert is the layer above: the framing around a record, the
// order records come in, and the two ends of a file.
package orders

import (
	"bytes"
	"errors"
	"io"
	"math/big"
	"testing"

	"github.com/Zaba505/cobol-go/codec"
)

// framed is one record with the record descriptor word DFSMS defines in front
// of it, which is what this descriptor's framing puts there.
func framed(raw []byte) []byte {
	stated := len(raw) + 4

	return append([]byte{byte(stated >> 8), byte(stated), 0, 0}, raw...)
}

// syncBytes is one SYNC-RECORD: an alignment gap ahead of a binary item, and
// the tail of a record whose items stop short of the dataset's LRECL.
func syncBytes(t *testing.T, enc codec.Encoding) []byte {
	t.Helper()

	return laidOut(t, enc, func(w *codec.Writer) error {
		if err := w.WriteAlphanumeric("Y", 1); err != nil {
			return err
		}

		if err := w.WriteBytes([]byte{0x00, 0x11, 0x22}); err != nil {
			return err
		}

		if err := w.WriteBinaryInt32(123456789, 9, codec.Signed); err != nil {
			return err
		}

		return w.WriteBytes(bytes.Repeat([]byte{enc.Charset.Space()}, 8))
	})
}

// trailerBytes is one TRAILER-RECORD: the three shapes a generator has no
// number for, beside a numeric-edited item whose storage is characters.
func trailerBytes(t *testing.T, enc codec.Encoding) []byte {
	t.Helper()

	total, ok := new(big.Int).SetString("-12345678901234567890", 10)
	if !ok {
		t.Fatal("the twenty-digit total is not a number")
	}

	return laidOut(t, enc, func(w *codec.Writer) error {
		if err := w.WritePackedBig(total, 20, codec.Signed); err != nil {
			return err
		}

		if err := w.WriteFloat32(1.5); err != nil {
			return err
		}

		if err := w.WriteBytes([]byte{0x01, 0x02, 0x03, 0x04}); err != nil {
			return err
		}

		for range 2 {
			if err := w.WriteAlphanumeric("$1,234.56", 12); err != nil {
				return err
			}
		}

		return nil
	})
}

// fileBytes is a whole file of this descriptor's five record types, in the one
// order its automaton admits them.
//
// EBCDIC only, because entryBytes is: an arm's predicate compares the bytes the
// producer resolved, so reading these records under another encoding is reading
// a file that is not the file the descriptor describes.
func fileBytes(t *testing.T) []byte {
	t.Helper()

	enc := Encoding()

	var b bytes.Buffer

	for _, raw := range [][]byte{
		orderBytes(t, enc, 2),
		tableBytes(t, enc, 3),
		syncBytes(t, enc),
		entryBytes(t, "DSD"),
		trailerBytes(t, enc),
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

// TestAMultiRecordFileReadsBackAsTheFileItWas is the criterion over a whole
// file: every record type this descriptor carries, in the order its automaton
// admits them, with the framing around each.
//
// Bytes rather than values, because docs/ir/SPEC.md's "Slack survives a read"
// makes a record read and written back unchanged byte-identical, and because
// under this framing a file is byte-identical too: a record descriptor word
// states the length of the record behind it and nothing else is invented.
func TestAMultiRecordFileReadsBackAsTheFileItWas(t *testing.T) {
	t.Parallel()

	want := fileBytes(t)

	records := read(t, Encoding(), want)
	if len(records) != 5 {
		t.Fatalf("the file holds five records and the reader produced %d", len(records))
	}

	for i, kind := range []any{
		(*OrderRecord)(nil), (*TableRecord)(nil), (*SyncRecord)(nil),
		(*EntryRecord)(nil), (*TrailerRecord)(nil),
	} {
		if got, want := recordType(records[i]), recordType(kind); got != want {
			t.Errorf("record %d came back as a %s, want a %s", i+1, got, want)
		}
	}

	assertBytes(t, write(t, Encoding(), records), want)
}

// recordType is what a record is, as a diagnostic names it.
func recordType(v any) string {
	switch v.(type) {
	case *OrderRecord:
		return "OrderRecord"
	case *TableRecord:
		return "TableRecord"
	case *SyncRecord:
		return "SyncRecord"
	case *EntryRecord:
		return "EntryRecord"
	case *TrailerRecord:
		return "TrailerRecord"
	default:
		return "something else"
	}
}

// TestAFileEndingBeforeTheAutomatonAcceptsIsTruncated is the truncation rule:
// an automaton whose accepting states nobody checks detects nothing.
func TestAFileEndingBeforeTheAutomatonAcceptsIsTruncated(t *testing.T) {
	t.Parallel()

	whole := fileBytes(t)

	// The first record only, which leaves the automaton in a state that does
	// not accept.
	first := whole[:4+int(whole[0])<<8+int(whole[1])-4]

	r, err := NewReader(bytes.NewReader(first), Encoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	if _, err := r.Next(); err != nil {
		t.Fatalf("Next: %v", err)
	}

	_, err = r.Next()
	if err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("a file ending one record in read as complete, and reported %v", err)
	}

	if !bytes.Contains([]byte(err.Error()), []byte("describes a record to come")) {
		t.Errorf("the report reads %q and does not say the file is not finished", err)
	}
}

// TestAWriterClosedBeforeTheAutomatonAcceptsIsReported is that rule from the
// other side. A group that promised four details and was given three is caught
// there, and a writer skipping the check emits the truncated file its reader
// complains about one build later.
func TestAWriterClosedBeforeTheAutomatonAcceptsIsReported(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer

	w, err := NewWriter(&b, Encoding())
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	if err := w.Write(&OrderRecord{}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := w.Close(); err == nil {
		t.Fatal("a writer closed one record into a five-record file closed it")
	}
}

// TestAWriterRefusesARecordNoTransitionAdmitsHere is docs/ir/SPEC.md's "A
// writer walks the same automaton": where no transition matches, a writer
// reports rather than emitting the record anyway.
//
// Refusing where the mistake is made costs one diagnostic; emitting costs a
// file somebody has to read before anyone finds out.
func TestAWriterRefusesARecordNoTransitionAdmitsHere(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer

	w, err := NewWriter(&b, Encoding())
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	// The automaton begins by admitting an ORDER-RECORD, and this is the
	// record it admits last.
	err = w.Write(&TrailerRecord{})
	if err == nil {
		t.Fatal("a writer emitted a record no transition leaving the start state admits")
	}

	if !bytes.Contains([]byte(err.Error()), []byte("TRAILER-RECORD")) {
		t.Errorf("the refusal reads %q and does not name the record", err)
	}

	if b.Len() != 0 {
		t.Errorf("a refused record put %d bytes in the file", b.Len())
	}
}

// TestAReaderReportsARecordDescriptorWordThatIsNotTheExtent is the detection a
// framed file buys over an unframed one.
//
// The extent governs and the framing is checked against it, so a descriptor
// word whose length is not the extent of the record it admits is an error at
// the record it happened on rather than a misalignment discovered three records
// later.
func TestAReaderReportsARecordDescriptorWordThatIsNotTheExtent(t *testing.T) {
	t.Parallel()

	for name, adjust := range map[string]int{"one byte short": -1, "one byte long": 1} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			raw := orderBytes(t, Encoding(), 2)
			in := framed(raw)

			stated := len(raw) + 4 + adjust
			in[0], in[1] = byte(stated>>8), byte(stated)

			r, err := NewReader(bytes.NewReader(in), Encoding())
			if err != nil {
				t.Fatalf("NewReader: %v", err)
			}

			if _, err := r.Next(); err == nil {
				t.Fatal("a record descriptor word disagreeing with the extent was read as a record")
			}
		})
	}
}

// TestTheWholeFileIsNeverMaterialised is the streaming property, asserted the
// only way a test can: the reader is handed a source that refuses to give up
// more than one record's bytes before that record has been produced.
func TestTheWholeFileIsNeverMaterialised(t *testing.T) {
	t.Parallel()

	in := fileBytes(t)

	src := &metered{src: bytes.NewReader(in)}

	r, err := NewReader(src, Encoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	if _, err := r.Next(); err != nil {
		t.Fatalf("Next: %v", err)
	}

	// bufio reads ahead, so the assertion is that the whole file was not
	// consumed rather than that nothing beyond the first record was: a reader
	// that materialised the file would have read all of it.
	if src.read >= len(in) {
		t.Errorf("producing the first record read %d bytes of a %d-byte file", src.read, len(in))
	}
}

// metered is an io.Reader counting what has been taken from it.
type metered struct {
	src  io.Reader
	read int
}

// Read implements io.Reader.
func (m *metered) Read(p []byte) (int, error) {
	// One record's worth at a time, so that a reader taking the whole file has
	// to ask for it rather than being handed it.
	if len(p) > 64 {
		p = p[:64]
	}

	n, err := m.src.Read(p)
	m.read += n

	return n, err
}

// TestARecordHeldAcrossALaterReadStillCarriesItsOwnSlack is
// docs/ir/SPEC.md's "Slack survives a read" at the lifetime it states, which is
// the half a fixture writing each record inside the iteration that read it
// cannot detect.
//
// A streaming reader over a reused buffer that keeps a window onto it satisfies
// a rule saying only *what* to retain, and then every record it hands a writer
// carries the *next* record's slack — an error no width check and no framing
// check can see, on a file that reads back as the records that were written.
func TestARecordHeldAcrossALaterReadStillCarriesItsOwnSlack(t *testing.T) {
	t.Parallel()

	in := fileBytes(t)

	r, err := NewReader(bytes.NewReader(in), Encoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	first, err := r.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}

	order, ok := first.(*OrderRecord)
	if !ok {
		t.Fatalf("the first record is a %T", first)
	}

	held := append([]byte(nil), order.slack[0]...)

	// Every record behind it, so that any buffer the reader reuses has been
	// overwritten several times over by the time the first record is written.
	for {
		if _, err := r.Next(); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("Next: %v", err)
		}
	}

	if !bytes.Equal(order.slack[0], held) {
		t.Errorf("the first record's slack is % x after reading the rest of the file, and was % x", order.slack[0], held)
	}

	var out bytes.Buffer

	w, err := codec.NewWriter(&out, Encoding())
	if err != nil {
		t.Fatalf("codec.NewWriter: %v", err)
	}

	if err := order.MarshalCOBOL(w); err != nil {
		t.Fatalf("MarshalCOBOL: %v", err)
	}

	assertBytes(t, out.Bytes(), orderBytes(t, Encoding(), 2))
}

// TestNoPredicateIsEvaluatedBeyondTheLengthTheFramingStates is
// docs/ir/SPEC.md's step 3 under a framing that bounds a record: a predicate
// whose target is not wholly within the record the framing bounds does not
// match, and where no other matches either, this is a record the layout does
// not describe.
//
// It is not a read past the end of the record and it is not a truncated file.
// The framing said how long this record is, and the layout describes no record
// of that length here.
func TestNoPredicateIsEvaluatedBeyondTheLengthTheFramingStates(t *testing.T) {
	t.Parallel()

	enc := Encoding()

	var b bytes.Buffer

	b.Write(framed(orderBytes(t, enc, 2)))
	b.Write(framed(tableBytes(t, enc, 3)))

	// A record the descriptor word states is no bytes at all, where the
	// automaton is about to evaluate a predicate on the first byte of a
	// SYNC-RECORD.
	b.Write([]byte{0, 4, 0, 0})

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
		t.Fatalf("a record shorter than the predicate's target read as %v", err)
	}

	for _, want := range []string{"does not describe", "SYNC-RECORD"} {
		if !bytes.Contains([]byte(err.Error()), []byte(want)) {
			t.Errorf("the report reads %q and does not name %s", err, want)
		}
	}
}
