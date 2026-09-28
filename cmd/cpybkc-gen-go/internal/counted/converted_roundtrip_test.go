// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The assertions over the same counted run held under another encoding: the
// file a copybook-aware transfer to ASCII makes of it, read and written by the
// package generated for the EBCDIC one.
//
// Every literal this package compares — a record's type code, and the flag a
// guard holds a bytes register to — is resolved under cp037. Read under ASCII,
// "H" is 48 rather than C8, and the flag the acceptance guard admits as a space
// is 20 rather than 40, which is `@` in ASCII: comparing the resolved bytes
// refuses the first record of the file, and admits a flag nobody wrote. See
// docs/ir/SPEC.md, "A consumer may read under other axes, and re-expresses what
// it compares".
package counted

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Zaba505/cobol-go/codec"
)

// converted is what a copybook-aware transfer to ASCII makes of a file under
// [Encoding]: ASCII characters and translated-EBCDIC signs, with the packed
// items, the byte order, the float format and the staircase as they were.
func converted() codec.Encoding {
	enc := Encoding()
	enc.Charset = codec.ASCII()
	enc.Sign = codec.SignTranslatedEBCDIC

	return enc
}

// convertibleRun is the file TestACountedRunReadsAndWritesBackTheFileItWas reads,
// laid out through codec under enc rather than through this package.
func convertibleRun(t *testing.T, enc codec.Encoding) []byte {
	t.Helper()

	return joined(
		headerBytes(t, enc, 2, "Y", 2),
		detailBytes(t, enc),
		detailBytes(t, enc),
		summaryBytes(t, enc, 2),
		headerBytes(t, enc, 0, " ", 0),
	)
}

// TestACountedRunReadsAndWritesBackUnderAConvertedEncoding is the whole
// appendix again, as the extract a transfer converted to ASCII: the same
// records in the same order, told apart by the same type codes and the same
// flag, with the characters rewritten.
func TestACountedRunReadsAndWritesBackUnderAConvertedEncoding(t *testing.T) {
	t.Parallel()

	want := convertibleRun(t, converted())

	if bytes.Equal(want, convertibleRun(t, Encoding())) {
		t.Fatal("the converted file is the EBCDIC one, so this reads nothing another encoding spelled")
	}

	records, err := readUnder(t, converted(), want)
	if err != nil {
		t.Fatalf("reading the converted file: %v", err)
	}

	kinds := make([]string, 0, len(records))
	for _, rec := range records {
		switch rec.(type) {
		case *HeaderRecord:
			kinds = append(kinds, "header")
		case *DetailRecord:
			kinds = append(kinds, "detail")
		case *SummaryRecord:
			kinds = append(kinds, "summary")
		}
	}

	if got := strings.Join(kinds, " "); got != "header detail detail summary header" {
		t.Fatalf("the converted file read back as %s", got)
	}

	var b bytes.Buffer

	w, err := NewWriter(&b, converted())
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
		t.Errorf("the converted file does not write back the bytes it was read from\n got: % x\nwant: % x", b.Bytes(), want)
	}
}

// TestAnAtSignIsNotASpaceUnderAConvertedEncoding is the failure that is not
// loud. The acceptance guard admits a flag of N or a space, and the space was
// resolved as 40 — which under ASCII is `@`. A reader comparing the resolved
// bytes accepts a file ending on a header whose flag is `@` as complete; the
// flag is not one the layout names, so the file is not.
func TestAnAtSignIsNotASpaceUnderAConvertedEncoding(t *testing.T) {
	t.Parallel()

	_, err := readUnder(t, converted(), headerBytes(t, converted(), 0, "@", 0))
	if err == nil {
		t.Fatal("a flag of @ under ASCII was read as the space the layout names")
	}

	if !strings.Contains(err.Error(), "not complete") || !strings.Contains(err.Error(), `" "`) {
		t.Errorf("the report reads %q, and names neither the incomplete file nor the space the flag had to hold", err)
	}
}

// TestTheLayoutsOwnEncodingReExpressesNothing pins the default path: under
// [Encoding] the reader, the writer and the record methods compare the literals
// the descriptor resolved, and nothing is re-expressed or held for it.
func TestTheLayoutsOwnEncodingReExpressesNothing(t *testing.T) {
	t.Parallel()

	lits, err := literalsFor(Encoding())
	if err != nil {
		t.Fatalf("literalsFor: %v", err)
	}

	if lits != &resolvedLiterals {
		t.Error("the descriptor's own encoding re-expressed the literals it resolved")
	}

	r, err := NewReader(bytes.NewReader(nil), Encoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	w, err := NewWriter(&bytes.Buffer{}, Encoding())
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	if r.lits != &resolvedLiterals || w.lits != &resolvedLiterals {
		t.Error("a reader or a writer under the descriptor's own encoding compares against re-expressed literals")
	}

	// The staircase is not one of the axes a literal moves along, so a caller
	// holding one that differs is not handed re-expressed literals over it.
	other := Encoding()
	other.Binary = codec.BinarySize1248

	if lits, err := literalsFor(other); err != nil || lits != &resolvedLiterals {
		t.Errorf("a staircase other than the descriptor's re-expressed the literals: %v", err)
	}

	for _, one := range []struct {
		name string
		got  []byte
		want string
	}{
		{"the header's type code", resolvedLiterals.lit5, "\xc8"},
		{"the flag's space", resolvedLiterals.lit2, "\x40"},
	} {
		if string(one.got) != one.want {
			t.Errorf("%s is % x, and the descriptor resolved % x", one.name, one.got, one.want)
		}
	}
}

// withoutY is ASCII with one character missing, standing in for a code page a
// layout's literal has no byte in.
type withoutY struct{ codec.Charset }

func (withoutY) Name() string { return "ASCII-without-Y" }

func (c withoutY) FromUnicode(r rune) (byte, bool) {
	if r == 'Y' {
		return 0, false
	}

	return c.Charset.FromUnicode(r)
}

// TestALiteralTheReadCharsetCannotSpellIsRefusedWhenTheReaderIsBuilt holds the
// refusal to where docs/ir/SPEC.md puts it — building the reader and the
// writer, before any record — and to what it names: the literal, the item it is
// compared against, the record holding that item, and the axis.
func TestALiteralTheReadCharsetCannotSpellIsRefusedWhenTheReaderIsBuilt(t *testing.T) {
	t.Parallel()

	enc := converted()
	enc.Charset = withoutY{codec.ASCII()}

	_, readErr := NewReader(bytes.NewReader(convertibleRun(t, converted())), enc)
	_, writeErr := NewWriter(&bytes.Buffer{}, enc)

	for name, err := range map[string]error{"NewReader": readErr, "NewWriter": writeErr} {
		if err == nil {
			t.Errorf("%s built under a charset with no Y, and the layout compares a flag against Y", name)

			continue
		}

		for _, want := range []string{
			`the literal "\xe8", "Y" under cp037`, "SUM-FLAG", "HEADER-RECORD", "the charset ASCII-without-Y",
			"no file under those axes holds that value", "not a fault in any file's data",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s's refusal reads %q and does not say %s", name, err, want)
			}
		}
	}
}
