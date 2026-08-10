// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The assertions over a fixed-length dataset: the framing that carries no bytes
// at all, one record type, and a transition carrying no predicate.
//
// The job these are written for is the most ordinary batch job there is — read
// a file, change one field of one record, write it back — and what it is here
// to catch is the bytes no item covers going missing while nothing fails. This
// record type carries both kinds: a tail its items stop short of, and a
// REDEFINES alternative shorter than its sibling, whose tail is the other
// alternative's payload.
//
// They live inside the package because one of them cannot be stated from
// outside: the retained runs are unexported.
package fixed

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Zaba505/cobol-go/codec"
)

// ledgerBytes is one LEDGER-RECORD whose two entries take the arms named.
//
// Every byte no item covers is neither a space nor a zero, so a writer filling
// one rather than emitting what was read is a failure rather than a
// coincidence.
func ledgerBytes(t *testing.T, id, arms string) []byte {
	t.Helper()

	var b bytes.Buffer

	w, err := codec.NewWriter(&b, Encoding())
	if err != nil {
		t.Fatalf("codec.NewWriter: %v", err)
	}

	if err := w.WriteAlphanumeric(id, 4); err != nil {
		t.Fatalf("LEDGER-ID: %v", err)
	}

	for i, code := range arms {
		if err := w.WriteAlphanumeric(string(code), 1); err != nil {
			t.Fatalf("ENTRY-TYPE: %v", err)
		}

		if code == 'D' {
			if err := w.WriteAlphanumeric("SKU", 4); err != nil {
				t.Fatalf("DETAIL-SKU: %v", err)
			}

			if err := w.WriteBinaryInt16(int16(i+1), 4, codec.Signed); err != nil {
				t.Fatalf("DETAIL-QTY: %v", err)
			}

			continue
		}

		if err := w.WriteAlphanumeric("SUM", 4); err != nil {
			t.Fatalf("SUMMARY-TEXT: %v", err)
		}

		// The other alternative's data, laid down by a program that used it.
		if err := w.WriteBytes([]byte{byte(0x90 + i), byte(0xa0 + i)}); err != nil {
			t.Fatalf("the arm's tail: %v", err)
		}
	}

	// The bytes this record's items stop short of, which on these files hold
	// whatever the program that wrote the file left there.
	if err := w.WriteBytes([]byte{0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6}); err != nil {
		t.Fatalf("the record's tail: %v", err)
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

// write is every record written back out, through the generated writer.
func write(t *testing.T, records []Record) []byte {
	t.Helper()

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

	return b.Bytes()
}

// TestAFileNoPredicateDiscriminatesRoundTrips is docs/ir/SPEC.md's "A
// transition may carry no predicate": a file with one record type has nothing
// for a predicate to test, and the state offering that transition offers
// nowhere else for a record to go.
func TestAFileNoPredicateDiscriminatesRoundTrips(t *testing.T) {
	t.Parallel()

	want := bytes.Join([][]byte{
		ledgerBytes(t, "AAAA", "DS"),
		ledgerBytes(t, "BBBB", "SD"),
		ledgerBytes(t, "CCCC", "DD"),
	}, nil)

	records := read(t, want)
	if len(records) != 3 {
		t.Fatalf("the file holds three records and the reader produced %d", len(records))
	}

	if got := write(t, records); !bytes.Equal(got, want) {
		t.Errorf("the file does not write back the bytes it was read from\n got: % x\nwant: % x", got, want)
	}
}

// TestReadModifyWriteLeavesEveryByteNoItemCovers is the job the whole of
// docs/ir/SPEC.md's "Slack survives a read" was written for.
//
// One field of one record changes and everything else comes back as it was —
// including the tail of the shorter REDEFINES alternative, which holds a
// payload no rule in this reader or this writer would report the loss of, and
// the padding a fixed-length dataset leaves behind every record.
func TestReadModifyWriteLeavesEveryByteNoItemCovers(t *testing.T) {
	t.Parallel()

	in := bytes.Join([][]byte{
		ledgerBytes(t, "AAAA", "DS"),
		ledgerBytes(t, "BBBB", "SS"),
	}, nil)

	records := read(t, in)

	second, ok := records[1].(*LedgerRecord)
	if !ok {
		t.Fatalf("the second record is a %T", records[1])
	}

	second.LedgerId = "ZZZZ"

	want := bytes.Join([][]byte{
		ledgerBytes(t, "AAAA", "DS"),
		ledgerBytes(t, "ZZZZ", "SS"),
	}, nil)

	if got := write(t, records); !bytes.Equal(got, want) {
		t.Errorf("a read-modify-write changed a byte no item covers\n got: % x\nwant: % x", got, want)
	}
}

// TestARecordTheCallerBuiltEmitsZeroBytesForItsSlack is the other side of that
// rule: a record its caller built rather than read has nothing to put back, and
// zero is the byte that names none.
//
// A character fill cannot be specified — charset is a property of a field and
// slack is not a field — so an adopter who wants those bytes to hold spaces
// declares a trailing item and supplies it.
func TestARecordTheCallerBuiltEmitsZeroBytesForItsSlack(t *testing.T) {
	t.Parallel()

	var built LedgerRecord

	built.LedgerId = "NEWW"

	for i := range built.Entry {
		built.Entry[i].EntryType = "D"
		built.Entry[i].EntryDetail = &struct {
			DetailSku string
			DetailQty int16
		}{DetailSku: "SKU", DetailQty: int16(i + 1)}
	}

	got := write(t, []Record{&built})

	if tail := got[len(got)-6:]; !bytes.Equal(tail, make([]byte, 6)) {
		t.Errorf("a constructed record's padding came out as % x, want six zero bytes", tail)
	}
}

// TestAFileEndingPartWayThroughARecordIsTruncated is the truncation rule under
// the one framing that has nothing to check a record against: the record's
// extent is what says where it ends, and an input that stops inside one is a
// file cut short rather than a record the layout does not describe.
func TestAFileEndingPartWayThroughARecordIsTruncated(t *testing.T) {
	t.Parallel()

	whole := ledgerBytes(t, "AAAA", "DS")

	r, err := NewReader(bytes.NewReader(whole[:len(whole)-3]), Encoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	_, err = r.Next()
	if err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("a record cut short read as %v", err)
	}

	if !strings.Contains(err.Error(), "part-way through record 1") {
		t.Errorf("the report reads %q and does not say where the file stops", err)
	}
}
