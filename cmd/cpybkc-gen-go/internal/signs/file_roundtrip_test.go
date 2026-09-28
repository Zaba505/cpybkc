// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The assertions over literals a re-expression moves by something other than a
// character: a file whose records are told apart by a character, by the sign
// byte of a signed zoned digit and by the first byte of a binary count, read
// and written under encodings other than the one its literals were resolved
// under.
//
// Every file below is laid out through codec under the encoding it is read
// with, and never through this package, so what the reader and the writer are
// held against is what a file under that encoding holds rather than their own
// output. See docs/ir/SPEC.md, "A consumer may read under other axes, and
// re-expresses what it compares" and the three sections after it.
package signs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Zaba505/cobol-go/codec"
)

// converted is what a copybook-aware transfer to ASCII makes of a file under
// [Encoding]: ASCII characters and translated-EBCDIC signs, with the binary
// items, the float format and the staircase as they were.
func converted() codec.Encoding {
	enc := Encoding()
	enc.Charset = codec.ASCII()
	enc.Sign = codec.SignTranslatedEBCDIC

	return enc
}

// swapped is converted with its binary items little-endian: the same records
// written by a program on a little-endian host, which no transfer produces.
func swapped() codec.Encoding {
	enc := converted()
	enc.ByteOrder = binary.LittleEndian

	return enc
}

// native is what a program writing ASCII natively writes: the ascii-zone-3-7
// convention, which spells a positive sign byte as the plain digit.
func native() codec.Encoding {
	enc := converted()
	enc.Sign = codec.SignASCIIZone37

	return enc
}

// framed is one record behind the record descriptor word DFSMS defines.
func framed(raw []byte) []byte {
	stated := len(raw) + 4

	return append([]byte{byte(stated >> 8), byte(stated), 0, 0}, raw...)
}

// laidOut is one record's bytes under enc, written item by item through codec.
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

	return framed(b.Bytes())
}

// fileUnder is one record of each of the four types, laid out under enc.
func fileUnder(t *testing.T, enc codec.Encoding) []byte {
	t.Helper()

	return bytes.Join([][]byte{
		laidOut(t, enc, func(w *codec.Writer) error {
			if err := w.WriteAlphanumeric("5", 1); err != nil {
				return err
			}

			return w.WriteAlphanumeric("TEXT", 4)
		}),
		laidOut(t, enc, func(w *codec.Writer) error {
			if err := w.WriteZonedInt32(5, 1, codec.SignTrailing); err != nil {
				return err
			}

			return w.WriteAlphanumeric("PLUS", 4)
		}),
		laidOut(t, enc, func(w *codec.Writer) error {
			if err := w.WriteZonedInt32(-5, 1, codec.SignTrailing); err != nil {
				return err
			}

			return w.WriteAlphanumeric("MINS", 4)
		}),
		laidOut(t, enc, func(w *codec.Writer) error {
			if err := w.WriteBinaryInt16(7, 4, codec.Signed); err != nil {
				return err
			}

			return w.WriteAlphanumeric("CNT7", 4)
		}),
	}, nil)
}

// TestAFileToldApartBySignsRoundTripsUnderEveryEncodingThatHoldsThemApart is
// the file under the encoding its literals were resolved under, as a transfer
// to ASCII converted it, and as a little-endian program wrote it: each read to
// the four record types in order and written back to the bytes it came from.
func TestAFileToldApartBySignsRoundTripsUnderEveryEncodingThatHoldsThemApart(t *testing.T) {
	t.Parallel()

	for name, enc := range map[string]codec.Encoding{
		"layout":        Encoding(),
		"converted":     converted(),
		"little-endian": swapped(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			want := fileUnder(t, enc)

			r, err := NewReader(bytes.NewReader(want), enc)
			if err != nil {
				t.Fatalf("NewReader: %v", err)
			}

			var kinds []string

			var b bytes.Buffer

			w, err := NewWriter(&b, enc)
			if err != nil {
				t.Fatalf("NewWriter: %v", err)
			}

			for {
				rec, err := r.Next()
				if errors.Is(err, io.EOF) {
					break
				}

				if err != nil {
					t.Fatalf("Next: %v", err)
				}

				switch rec.(type) {
				case *TextRecord:
					kinds = append(kinds, "text")
				case *PositiveRecord:
					kinds = append(kinds, "positive")
				case *NegativeRecord:
					kinds = append(kinds, "negative")
				case *CountRecord:
					kinds = append(kinds, "count")
				}

				if err := w.Write(rec); err != nil {
					t.Fatalf("Write: %v", err)
				}
			}

			if err := w.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			if got := strings.Join(kinds, " "); got != "text positive negative count" {
				t.Errorf("the file read back as %s", got)
			}

			if !bytes.Equal(b.Bytes(), want) {
				t.Errorf("the file does not write back the bytes it was read from\n got: % x\nwant: % x", b.Bytes(), want)
			}
		})
	}
}

// TestAConventionSpellingTwoLiteralsAlikeIsRefusedWhenTheReaderIsBuilt is the
// overlap `resolve` proved and a re-expression lost. The character `5` and the
// digit +5 are F5 and C5 under cp037 and the EBCDIC convention, and 35 and 35
// under ASCII and ascii-zone-3-7, so no file under those axes tells a
// TEXT-RECORD from a POSITIVE-RECORD — and the reader and the writer say so
// when they are built, naming both literals, both items, both records and the
// axes that moved.
func TestAConventionSpellingTwoLiteralsAlikeIsRefusedWhenTheReaderIsBuilt(t *testing.T) {
	t.Parallel()

	_, readErr := NewReader(bytes.NewReader(nil), native())
	_, writeErr := NewWriter(&bytes.Buffer{}, native())

	for name, err := range map[string]error{"NewReader": readErr, "NewWriter": writeErr} {
		if err == nil {
			t.Errorf("%s built under a convention that spells two of its literals alike", name)

			continue
		}

		for _, want := range []string{
			`the literal "\xf5", "5" under cp037`, "CODE of TEXT-RECORD",
			`the literal "\xc5"`, "KIND of POSITIVE-RECORD",
			"the charset ASCII and the sign convention ascii-zone-3-7",
			"no file under those axes tells a TEXT-RECORD from a POSITIVE-RECORD",
			"not a fault in any file's data",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s's refusal reads %q and does not say %s", name, err, want)
			}
		}
	}
}

// TestAnFZoneCrossesACharsetAsATransferWritesIt is the case docs/ir/SPEC.md,
// "Each axis carries a literal the way a file crosses it", works through, on
// the helper this package re-expresses its sign bytes with: a signed zoned
// `(bytes …)` literal whose sign byte carries the F zone.
//
// F5 and C5 are both +5 to a reader of a signed item under the EBCDIC
// convention. By value both would come out as 45 under translated-EBCDIC signs;
// a transfer rewrites the bytes and writes 35 for the one and 45 for the other,
// and so does this.
func TestAnFZoneCrossesACharsetAsATransferWritesIt(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		lit  byte
		want byte
	}{
		{lit: 0xF5, want: 0x35},
		{lit: 0xC5, want: 0x45},
		{lit: 0xD5, want: 0x4E},
	} {
		got, err := reexpressZoned([]byte{tc.lit}, resolvedAxes(converted()), converted(), 0)
		if err != nil {
			t.Fatalf("% X: %v", tc.lit, err)
		}

		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("% X came out as % X, and a transfer writes % X", tc.lit, got, tc.want)
		}
	}
}

// TestTheLayoutsOwnEncodingComparesTheResolvedLiterals pins the default path
// in the package whose literals move furthest: under [Encoding], nothing is
// re-expressed and the reader holds the literals the descriptor resolved.
func TestTheLayoutsOwnEncodingComparesTheResolvedLiterals(t *testing.T) {
	t.Parallel()

	r, err := NewReader(bytes.NewReader(nil), Encoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	if r.lits != &resolvedLiterals {
		t.Error("a reader under the descriptor's own encoding compares against re-expressed literals")
	}

	lits, err := literalsFor(swapped())
	if err != nil {
		t.Fatalf("literalsFor: %v", err)
	}

	if !bytes.Equal(lits.lit4, []byte{0x07, 0x00}) {
		t.Errorf("the count's literal under a little-endian encoding is % X, want 07 00", lits.lit4)
	}

	again, err := literalsFor(swapped())
	if err != nil || again != lits {
		t.Errorf("a second reader under one encoding re-expressed the literals again: %v", err)
	}
}
