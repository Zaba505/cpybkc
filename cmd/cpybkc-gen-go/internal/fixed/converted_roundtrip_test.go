// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The assertions over the same fixed-length dataset held under another
// encoding: the file a copybook-aware transfer to ASCII makes of it.
//
// This file has no transition predicate to re-express, and it is here for the
// other kind of literal: the type code each occurrence of ENTRY is told apart
// by, which the record methods compare and which was resolved as C4 and E2.
// Under ASCII those are `D` and `S` spelled 44 and 53, and a reader comparing
// the resolved bytes reports every entry as matching no arm. See
// docs/ir/SPEC.md, "A consumer may read under other axes, and re-expresses what
// it compares".
package fixed

import (
	"bytes"
	"testing"

	"github.com/Zaba505/cobol-go/codec"
)

// converted is what a copybook-aware transfer to ASCII makes of a file under
// [Encoding]: ASCII characters and translated-EBCDIC signs, with the binary
// items, the byte order, the float format and the staircase as they were.
func converted() codec.Encoding {
	enc := Encoding()
	enc.Charset = codec.ASCII()
	enc.Sign = codec.SignTranslatedEBCDIC

	return enc
}

// TestAFixedLengthDatasetRoundTripsUnderAConvertedEncoding is
// [TestAFileNoPredicateDiscriminatesRoundTrips] over the converted file, and
// what it adds is the arms: every entry is chosen by its type code under ASCII,
// and the bytes no item covers come back as they were read.
func TestAFixedLengthDatasetRoundTripsUnderAConvertedEncoding(t *testing.T) {
	t.Parallel()

	enc := converted()

	want := bytes.Join([][]byte{
		ledgerBytes(t, enc, "AAAA", "DS"),
		ledgerBytes(t, enc, "BBBB", "SD"),
		ledgerBytes(t, enc, "CCCC", "DD"),
	}, nil)

	records := readUnder(t, enc, want)
	if len(records) != 3 {
		t.Fatalf("the file holds three records and the reader produced %d", len(records))
	}

	for i, arms := range []string{"DS", "SD", "DD"} {
		ledger, ok := records[i].(*LedgerRecord)
		if !ok {
			t.Fatalf("record %d is a %T", i+1, records[i])
		}

		for j, code := range arms {
			entry := ledger.Entry[j]

			if (code == 'D') != (entry.EntryDetail != nil) || (code == 'S') != (entry.EntrySummary != nil) {
				t.Errorf("record %d, entry %d holds detail=%v summary=%v, and its type code is %c",
					i+1, j+1, entry.EntryDetail != nil, entry.EntrySummary != nil, code)
			}
		}
	}

	if got := writeUnder(t, enc, records); !bytes.Equal(got, want) {
		t.Errorf("the converted file does not write back the bytes it was read from\n got: % x\nwant: % x", got, want)
	}
}

// TestTheRecordMethodsReExpressUnderTheEncodingTheyAreHanded is the arms
// without the file around them: codec.Unmarshal and codec.Marshal hand the
// record methods nothing but a decoder or an encoder, and the encoding on it is
// what the type codes are compared under.
func TestTheRecordMethodsReExpressUnderTheEncodingTheyAreHanded(t *testing.T) {
	t.Parallel()

	for name, enc := range map[string]codec.Encoding{"EBCDIC": Encoding(), "converted": converted()} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			want := ledgerBytes(t, enc, "DDDD", "SD")

			var x LedgerRecord

			if err := codec.Unmarshal(enc, want, &x); err != nil {
				t.Fatalf("codec.Unmarshal: %v", err)
			}

			if x.Entry[0].EntrySummary == nil || x.Entry[1].EntryDetail == nil {
				t.Fatalf("the entries came back as %+v", x.Entry)
			}

			got, err := codec.Marshal(enc, &x)
			if err != nil {
				t.Fatalf("codec.Marshal: %v", err)
			}

			if !bytes.Equal(got, want) {
				t.Errorf("the record does not write back the bytes it was read from\n got: % x\nwant: % x", got, want)
			}
		})
	}
}

// TestTheRecordMethodsReExpressOncePerEncodingAndNotPerRecord holds the record
// methods to docs/ir/SPEC.md's "once, and before the first record": they are
// handed an encoding on every call, and under a converted one they cost what
// they cost under the layout's own — the literals are re-expressed the first
// time the package meets the encoding and held after that, so a record read
// under it allocates nothing a record read under [Encoding] does not.
func TestTheRecordMethodsReExpressOncePerEncodingAndNotPerRecord(t *testing.T) {
	allocations := func(enc codec.Encoding) float64 {
		raw := ledgerBytes(t, enc, "DDDD", "SD")

		r, err := codec.NewBytesReader(nil, enc)
		if err != nil {
			t.Fatalf("codec.NewBytesReader: %v", err)
		}

		var x LedgerRecord

		return testing.AllocsPerRun(100, func() {
			r.Reset(raw)

			if err := x.UnmarshalCOBOL(r); err != nil {
				t.Fatalf("UnmarshalCOBOL: %v", err)
			}
		})
	}

	own, other := allocations(Encoding()), allocations(converted())

	if other != own {
		t.Errorf("a record read under a converted encoding allocates %.2f times, and under the layout's own %.2f", other, own)
	}
}
