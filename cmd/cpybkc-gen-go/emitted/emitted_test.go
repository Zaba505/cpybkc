// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package emitted

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Zaba505/cobol-go/codec"
)

// mainframe is the encoding a dataset written by IBM Enterprise COBOL is read
// under, and converted is what a copybook-aware transfer to ASCII makes of the
// same file: ASCII characters, translated-EBCDIC signs, and everything else as
// it was.
var (
	mainframe = codec.IBMEnterprise()
	converted = func() codec.Encoding {
		enc := codec.IBMEnterprise()
		enc.Charset = codec.ASCII()
		enc.Sign = codec.SignTranslatedEBCDIC

		return enc
	}()
)

// under is mainframe with its sign convention replaced, under the charset
// asked for.
func under(charset codec.Charset, sign codec.SignConvention) codec.Encoding {
	enc := codec.IBMEnterprise()
	enc.Charset = charset
	enc.Sign = sign

	return enc
}

// TestAZonedSignByteCrossesACharsetByItsColumn is the case docs/ir/SPEC.md,
// "Each axis carries a literal the way a file crosses it", works through: a
// signed zoned literal whose sign byte carries the F zone, the zone a writer
// puts on an unsigned item and a reader of a signed one takes as +5.
//
// Re-expressed by value it would be +5 under translated-EBCDIC signs, which is
// 45 — `E`. A transfer writes 35, because it rewrites the byte and F5 is the
// character `5`. The column is what gets there, and C5 in the same position
// goes to 45 because it is in the other column: two bytes before, two after.
func TestAZonedSignByteCrossesACharsetByItsColumn(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		lit  []byte
		to   codec.Encoding
		want []byte
	}{
		{name: "F5 is 5 in ASCII", lit: []byte{0xF1, 0xF5}, to: converted, want: []byte("15")},
		{name: "C5 is E in ASCII", lit: []byte{0xF1, 0xC5}, to: converted, want: []byte("1E")},
		{name: "D5 is N in ASCII", lit: []byte{0xF1, 0xD5}, to: converted, want: []byte("1N")},
		{name: "D0 is a closing brace in ASCII", lit: []byte{0xF1, 0xD0}, to: converted, want: []byte("1}")},
		{name: "ascii-zone-3-7 spells positive and unsigned alike", lit: []byte{0xF1, 0xC5}, to: under(codec.ASCII(), codec.SignASCIIZone37), want: []byte("15")},
		{name: "F5 under ascii-zone-3-7", lit: []byte{0xF1, 0xF5}, to: under(codec.ASCII(), codec.SignASCIIZone37), want: []byte("15")},
		{name: "a negative under ascii-zone-3-7", lit: []byte{0xF1, 0xD5}, to: under(codec.ASCII(), codec.SignASCIIZone37), want: []byte{0x31, 0x75}},
		{name: "a negative zero under realia is a space", lit: []byte{0xF1, 0xD0}, to: under(codec.ASCII(), codec.SignRealia), want: []byte{0x31, 0x20}},
		{name: "a charset alone leaves the sign byte", lit: []byte{0xF1, 0xC5}, to: under(codec.ASCII(), codec.SignEBCDIC), want: []byte{0x31, 0xC5}},
		{name: "a sign convention alone leaves the digits", lit: []byte{0xF1, 0xC5}, to: under(codec.CP037(), codec.SignTranslatedEBCDIC), want: []byte{0xF1, 0x45}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := reexpressZoned(tc.lit, mainframe, tc.to, len(tc.lit)-1)
			if err != nil {
				t.Fatalf("reexpressZoned: %v", err)
			}

			if !bytes.Equal(got, tc.want) {
				t.Errorf("got % X, want % X", got, tc.want)
			}
		})
	}
}

// TestALeadingSignIsTheFirstByte holds signAt to meaning what it says: under
// SIGN LEADING the sign rides the first digit, and the last is a digit like any
// other.
func TestALeadingSignIsTheFirstByte(t *testing.T) {
	t.Parallel()

	got, err := reexpressZoned([]byte{0xD1, 0xF5}, mainframe, converted, 0)
	if err != nil {
		t.Fatalf("reexpressZoned: %v", err)
	}

	if want := []byte("J5"); !bytes.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestASeparateSignIsACharacter holds a SEPARATE sign to the charset: `+` is a
// character like the digits beside it, and no sign convention touches it.
func TestASeparateSignIsACharacter(t *testing.T) {
	t.Parallel()

	got, err := reexpressZoned([]byte{0x4E, 0xF4, 0xF2}, mainframe, converted, -1)
	if err != nil {
		t.Fatalf("reexpressZoned: %v", err)
	}

	if want := []byte("+42"); !bytes.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestASpaceInANumericFieldIsCarriedAsASpace is the byte that is not a digit:
// a file that leaves a numeric field blank holds the charset's space there, and
// a transfer carries it as a space.
func TestASpaceInANumericFieldIsCarriedAsASpace(t *testing.T) {
	t.Parallel()

	got, err := reexpressZoned([]byte{0x40, 0x40}, mainframe, converted, -1)
	if err != nil {
		t.Fatalf("reexpressZoned: %v", err)
	}

	if want := []byte("  "); !bytes.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestASignByteInNoColumnIsRefusedOverTheSignConvention is the lenient EBCDIC
// zone: E5 reads as +5 under the EBCDIC convention and no writer emits it, so
// it is in none of the three columns and there is nothing to carry.
func TestASignByteInNoColumnIsRefusedOverTheSignConvention(t *testing.T) {
	t.Parallel()

	_, err := reexpressZoned([]byte{0xF1, 0xE5}, mainframe, converted, 1)

	var refusal literalError
	if !errors.As(err, &refusal) {
		t.Fatalf("got %v, want a literalError", err)
	}

	if !strings.Contains(refusal.axis, "sign convention translated-ebcdic") {
		t.Errorf("the refusal names %q, and the axis it could not cross is the sign convention", refusal.axis)
	}

	if !strings.Contains(err.Error(), "0xE5") {
		t.Errorf("the refusal does not name the byte: %v", err)
	}
}

// TestASignByteInNoColumnIsCarriedWhereTheConventionDoesNotMove is the other
// half of that refusal: it is raised over the sign convention and only where
// the sign convention changes. A charset change alone carries the byte
// untouched.
func TestASignByteInNoColumnIsCarriedWhereTheConventionDoesNotMove(t *testing.T) {
	t.Parallel()

	got, err := reexpressZoned([]byte{0xF1, 0xE5}, mainframe, under(codec.ASCII(), codec.SignEBCDIC), 1)
	if err != nil {
		t.Fatalf("reexpressZoned: %v", err)
	}

	if want := []byte{0x31, 0xE5}; !bytes.Equal(got, want) {
		t.Errorf("got % X, want % X", got, want)
	}
}

// partial is a charset that spells the ASCII printable range and nothing else,
// standing in for the code page a literal's character is missing from.
type partial struct{}

func (partial) Name() string          { return "partial" }
func (partial) ToUnicode(b byte) rune { return rune(b) }
func (partial) Space() byte           { return 0x20 }
func (partial) FromUnicode(r rune) (byte, bool) {
	if r < 0x20 || r > 0x7E {
		return 0, false
	}

	return byte(r), true
}

// TestACharacterTheReadCharsetLacksIsRefusedOverTheCharset holds the one way a
// text literal fails: its characters are decoded through one code page, which
// is total, and encoded through another, which need not be.
func TestACharacterTheReadCharsetLacksIsRefusedOverTheCharset(t *testing.T) {
	t.Parallel()

	_, err := reexpressText([]byte{0x4A}, codec.CP037(), partial{})

	var refusal literalError
	if !errors.As(err, &refusal) {
		t.Fatalf("got %v, want a literalError", err)
	}

	if refusal.axis != "the charset partial" {
		t.Errorf("the refusal names %q", refusal.axis)
	}

	if !strings.Contains(err.Error(), "'¢'") {
		t.Errorf("the refusal does not name the character: %v", err)
	}
}

// TestTextCrossesCharsetsByCharacter is the ordinary case, and the one every
// multi-record file under a converted encoding needs: "01" under cp037 is
// F0 F1, and under ASCII it is 30 31.
func TestTextCrossesCharsetsByCharacter(t *testing.T) {
	t.Parallel()

	got, err := reexpressText([]byte{0xF0, 0xF1}, codec.CP037(), codec.ASCII())
	if err != nil {
		t.Fatalf("reexpressText: %v", err)
	}

	if !bytes.Equal(got, []byte("01")) {
		t.Errorf("got % X, want 30 31", got)
	}
}

// TestACharsetThatDidNotMoveIsNotTranslated is "where nothing moves, nothing is
// re-expressed": the literal handed in is the literal handed back, not a copy
// of it, so the default path allocates nothing and cannot be refused.
func TestACharsetThatDidNotMoveIsNotTranslated(t *testing.T) {
	t.Parallel()

	lit := []byte{0xFF, 0x00}

	got, err := reexpressText(lit, partial{}, partial{})
	if err != nil {
		t.Fatalf("a charset that did not move refused %q: %v", lit, err)
	}

	if &got[0] != &lit[0] {
		t.Error("a charset that did not move copied the literal")
	}
}

// TestABinaryLiteralCrossesAByteOrderReversed holds the one axis a literal can
// always cross.
func TestABinaryLiteralCrossesAByteOrderReversed(t *testing.T) {
	t.Parallel()

	lit := []byte{0x00, 0x00, 0x01, 0x02}

	if got := reexpressBinary(lit, binary.BigEndian, binary.LittleEndian); !bytes.Equal(got, []byte{0x02, 0x01, 0x00, 0x00}) {
		t.Errorf("got % X", got)
	}

	if got := reexpressBinary(lit, binary.BigEndian, binary.BigEndian); &got[0] != &lit[0] {
		t.Error("a byte order that did not move copied the literal")
	}

	native := binary.ByteOrder(binary.NativeEndian)
	if got := reexpressBinary(lit, binary.BigEndian, native); sameByteOrder(native, binary.BigEndian) != bytes.Equal(got, lit) {
		t.Errorf("NativeEndian is compared by what it does: got % X", got)
	}
}

// TestAFloatCrossesAFormatByValue and the three refusals beside it.
func TestAFloatCrossesAFormatByValue(t *testing.T) {
	t.Parallel()

	ieee := mainframe
	ieee.Float = codec.FloatIEEE

	hfpOne := []byte{0x41, 0x10, 0x00, 0x00}

	got, err := reexpressFloat(hfpOne, mainframe, ieee)
	if err != nil {
		t.Fatalf("reexpressFloat: %v", err)
	}

	if want := []byte{0x3F, 0x80, 0x00, 0x00}; !bytes.Equal(got, want) {
		t.Errorf("HFP 1.0 under IEEE: got % X, want % X", got, want)
	}

	little := ieee
	little.ByteOrder = binary.LittleEndian

	got, err = reexpressFloat([]byte{0x3F, 0x80, 0x00, 0x00}, ieee, little)
	if err != nil {
		t.Fatalf("reexpressFloat: %v", err)
	}

	if want := []byte{0x00, 0x00, 0x80, 0x3F}; !bytes.Equal(got, want) {
		t.Errorf("IEEE 1.0 little-endian: got % X, want % X", got, want)
	}

	// HFP spells no byte order, so a byte order is not an axis an HFP float
	// crosses.
	hfpLittle := mainframe
	hfpLittle.ByteOrder = binary.LittleEndian

	if got, err := reexpressFloat(hfpOne, mainframe, hfpLittle); err != nil || &got[0] != &hfpOne[0] {
		t.Errorf("an HFP float moved over a byte order: % X, %v", got, err)
	}
}

// TestAFloatTheReadFormatCannotHoldIsRefused covers the three ways a float
// fails: bytes that hold no number where they were resolved, a number the read
// format has no bytes for, and one it cannot hold exactly.
func TestAFloatTheReadFormatCannotHoldIsRefused(t *testing.T) {
	t.Parallel()

	ieee := mainframe
	ieee.Float = codec.FloatIEEE

	nan := make([]byte, 4)
	binary.BigEndian.PutUint32(nan, math.Float32bits(float32(math.NaN())))

	// 1 + 2^-23 needs every bit of a binary32 significand, and HFP short's
	// hexadecimal normalization leaves 21 behind a leading 1.
	fine := make([]byte, 4)
	binary.BigEndian.PutUint32(fine, math.Float32bits(1+0x1p-23))

	for name, lit := range map[string][]byte{"NaN": nan, "a number HFP rounds": fine} {
		_, err := reexpressFloat(lit, ieee, mainframe)

		var refusal literalError
		if !errors.As(err, &refusal) {
			t.Errorf("%s: got %v, want a literalError", name, err)

			continue
		}

		if refusal.axis != "the floating-point format ibm-hfp" {
			t.Errorf("%s: the refusal names %q", name, refusal.axis)
		}
	}

	// HFP's exponent range is far wider than binary32's.
	huge := []byte{0x7F, 0x10, 0x00, 0x00}
	if _, err := reexpressFloat(huge, mainframe, ieee); err == nil {
		t.Error("an HFP value no binary32 holds was re-expressed")
	}
}

// TestTwoLiteralsThatCameToOneByteStringAreNotApart holds the re-check the
// sign conventions make necessary: C5 and F5 are two literals under EBCDIC
// signs and one under ascii-zone-3-7.
func TestTwoLiteralsThatCameToOneByteStringAreNotApart(t *testing.T) {
	t.Parallel()

	if !literalsApart([]byte{0xC5}, 3, []byte{0xF5}, 3) {
		t.Error("two literals differing on their one shared byte are not apart")
	}

	if literalsApart([]byte("15"), 3, []byte("15"), 3) {
		t.Error("two literals that came to one byte string are apart")
	}

	// Overlapping runs rather than identical ones: the shared window is
	// bytes 1:2, where one holds 'B' and the other 'B'.
	if literalsApart([]byte("AB"), 0, []byte("BC"), 1) {
		t.Error("two literals agreeing over the one byte both read are apart")
	}

	if !literalsApart([]byte("A"), 0, []byte("B"), 1) {
		t.Error("two runs that share no byte are not a pair this check is about")
	}
}

// TestNotApartNamesBothSidesAndTheAxis holds the refusal to what
// docs/ir/SPEC.md requires of it.
func TestNotApartNamesBothSidesAndTheAxis(t *testing.T) {
	t.Parallel()

	err := literalsNotApart(mainframe, under(codec.ASCII(), codec.SignASCIIZone37),
		`"\xc5"`, "CODE-A", "RECORD-A", `"\xf5"`, "CODE-B", "RECORD-B")

	for _, want := range []string{
		`"\xc5"`, "CODE-A", "RECORD-A", `"\xf5"`, "CODE-B", "RECORD-B",
		"the charset ASCII and the sign convention ascii-zone-3-7",
		"no file under those axes tells", "not a fault in any file's data",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}
}

// TestCannotReexpressNamesTheLiteralTheFieldTheRecordAndTheAxis holds the
// other refusal to the same.
func TestCannotReexpressNamesTheLiteralTheFieldTheRecordAndTheAxis(t *testing.T) {
	t.Parallel()

	err := cannotReexpress(`the literal "\x4a"`, "FLAG", "HEADER", literalError{axis: "the charset partial", reason: "since it holds '¢'"})

	for _, want := range []string{
		`the literal "\x4a"`, "FLAG of HEADER", "the charset partial", "'¢'",
		"no file under those axes holds that value in FLAG", "not a fault in any file's data",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}
}

// uncomparable is a charset Go cannot compare with ==, which a caller's own
// table-driven charset routinely is. shifted spells byte 0x41 differently, so
// that two of them can be told apart by what they do.
type uncomparable struct {
	partial

	table   []rune
	shifted bool
}

func (u uncomparable) ToUnicode(b byte) rune {
	if u.shifted && b == 0x41 {
		return 'z'
	}

	return rune(b)
}

// TestTheCacheReExpressesOncePerEncoding holds the record-level methods to
// re-expressing once per encoding and not once per record — an encoding Go
// cannot compare with == included, which is compared by what its charset does
// rather than costing a panic or a re-expression per call.
func TestTheCacheReExpressesOncePerEncoding(t *testing.T) {
	t.Parallel()

	var (
		cache atomic.Pointer[[]reexpression[int]]
		calls int
	)

	count := func(codec.Encoding) (*int, error) {
		calls++

		return &calls, nil
	}

	for range 3 {
		if _, err := reexpressedUnder(&cache, converted, count); err != nil {
			t.Fatal(err)
		}
	}

	if calls != 1 {
		t.Errorf("one encoding was re-expressed under %d times", calls)
	}

	odd := converted
	odd.Charset = uncomparable{table: []rune{'x'}}

	for range 2 {
		if _, err := reexpressedUnder(&cache, odd, count); err != nil {
			t.Fatal(err)
		}
	}

	if calls != 2 {
		t.Errorf("an encoding Go cannot compare was re-expressed under %d times, want once", calls-1)
	}

	// A charset of that type spelling a byte differently is a different
	// encoding, and is re-expressed under once more.
	other := converted
	other.Charset = uncomparable{table: []rune{'x'}, shifted: true}

	if _, err := reexpressedUnder(&cache, other, count); err != nil {
		t.Fatal(err)
	}

	if calls != 3 {
		t.Errorf("a charset spelling a byte differently was held as the same encoding")
	}

	refused := errors.New("refused")

	for range 2 {
		if _, err := reexpressedUnder(&cache, mainframe, func(codec.Encoding) (*int, error) {
			calls++

			return nil, refused
		}); !errors.Is(err, refused) {
			t.Fatalf("got %v, want the refusal held", err)
		}
	}

	if calls != 4 {
		t.Errorf("a refusal was worked out %d times, want once", calls-3)
	}
}

// TestEveryRefusalNamesTheLiteralTheItemTheRecordAndTheAxis is each refusal
// docs/ir/SPEC.md, "What cannot be re-expressed is refused before any record is
// read", enumerates, as the message a reader or a writer returns when it is
// built: the helper's reason wrapped the way literals.go wraps it, and held to
// naming the literal, the item, the record and the axis, and to saying that no
// file under those axes holds the value.
func TestEveryRefusalNamesTheLiteralTheItemTheRecordAndTheAxis(t *testing.T) {
	t.Parallel()

	ieee := mainframe
	ieee.Float = codec.FloatIEEE

	nan := make([]byte, 4)
	binary.BigEndian.PutUint32(nan, math.Float32bits(float32(math.NaN())))

	fine := make([]byte, 4)
	binary.BigEndian.PutUint32(fine, math.Float32bits(1+0x1p-23))

	for name, tc := range map[string]struct {
		refuse func() error
		axis   string
	}{
		"a character the read charset has no byte for": {
			refuse: func() error { _, err := reexpressText([]byte{0x4A}, codec.CP037(), partial{}); return err },
			axis:   "the charset partial",
		},
		"a sign byte in no column of the resolved convention": {
			refuse: func() error { _, err := reexpressZoned([]byte{0xF1, 0xE5}, mainframe, converted, 1); return err },
			axis:   "the sign convention translated-ebcdic",
		},
		"a number the read float format has no bytes for": {
			refuse: func() error { _, err := reexpressFloat(nan, ieee, mainframe); return err },
			axis:   "the floating-point format ibm-hfp",
		},
		"a number the read float format cannot hold exactly": {
			refuse: func() error { _, err := reexpressFloat(fine, ieee, mainframe); return err },
			axis:   "the floating-point format ibm-hfp",
		},
		"bytes that are no number under the format they were resolved under": {
			refuse: func() error { _, err := reexpressFloat([]byte{0x7F, 0x10, 0x00, 0x00}, mainframe, ieee); return err },
			axis:   "the floating-point format ieee-754",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			why := tc.refuse()
			if why == nil {
				t.Fatal("nothing was refused")
			}

			err := cannotReexpress(`the literal "\xf1"`, "THE-ITEM", "THE-RECORD", why)

			for _, want := range []string{
				`the literal "\xf1"`, "THE-ITEM of THE-RECORD", tc.axis,
				"no file under those axes holds that value in THE-ITEM",
				"not a fault in any file's data",
			} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal reads %q and does not say %s", err, want)
				}
			}
		})
	}
}
