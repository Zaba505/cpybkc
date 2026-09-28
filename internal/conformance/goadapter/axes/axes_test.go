// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package axes

import (
	"encoding/binary"
	"testing"

	"github.com/Zaba505/cobol-go/codec"

	"github.com/Zaba505/cpybkc/internal/conformance"
)

// TestCP1140IsCP037WithTheEuro holds the one code page this package carries to
// its definition: cp037 at every byte but 0x9F, the euro sign there, and no
// byte at all for the currency sign cp037 keeps at 0x9F.
//
// That last clause is the whole reason the page is here — it is the character
// docs/ir/SPEC.md names as one code page having and another lacking — so it is
// asserted rather than left to follow from the rest.
func TestCP1140IsCP037WithTheEuro(t *testing.T) {
	cp037, cp1140 := codec.CP037(), CP1140()

	for b := range 256 {
		got := cp1140.ToUnicode(byte(b))

		want := cp037.ToUnicode(byte(b))
		if b == 0x9F {
			want = '€'
		}

		if got != want {
			t.Errorf("cp1140 reads 0x%02X as %U, and it is %U", b, got, want)
		}

		back, ok := cp1140.FromUnicode(got)
		if !ok || back != byte(b) {
			t.Errorf("cp1140 writes %U as 0x%02X (%v), and it read it from 0x%02X", got, back, ok, b)
		}
	}

	if b, ok := cp1140.FromUnicode('¤'); ok {
		t.Errorf("cp1140 writes the currency sign as 0x%02X, and it has no byte for it", b)
	}

	if cp1140.Space() != 0x40 || cp1140.Name() != "cp1140" {
		t.Errorf("cp1140 is named %q with the space 0x%02X", cp1140.Name(), cp1140.Space())
	}
}

// TestReplaceReplacesWhatIsStatedAndNothingElse is the encoding a codec program
// hands a record's own decoder: the descriptor's, with each axis the entry
// states replaced.
func TestReplaceReplacesWhatIsStatedAndNothingElse(t *testing.T) {
	resolved := codec.Encoding{
		Charset:   codec.CP037(),
		Sign:      codec.SignEBCDIC,
		ByteOrder: binary.BigEndian,
		Float:     codec.FloatIEEE,
		Binary:    codec.BinarySize248,
	}

	got, err := Replace(resolved, &conformance.Axes{
		SignConvention: "translated-ebcdic",
		FloatFormat:    "hfp",
		BinarySize:     "1-2-4-8",
	})
	if err != nil {
		t.Fatalf("%v", err)
	}

	switch {
	case got.Charset.Name() != "cp037":
		t.Errorf("the charset is %s, and none was stated", got.Charset.Name())
	case got.Sign != codec.SignTranslatedEBCDIC:
		t.Errorf("the sign convention is %v, and translated-ebcdic was stated", got.Sign)
	case got.ByteOrder != binary.BigEndian:
		t.Errorf("the byte order is %v, and none was stated", got.ByteOrder)
	case got.Float != codec.FloatHFP:
		t.Errorf("the float format is %v, and hfp was stated", got.Float)
	case got.Binary != codec.BinarySize1248:
		t.Errorf("the staircase is %v, and 1-2-4-8 was stated", got.Binary)
	}
}

// TestASpellingThisAdapterCannotStateIsAnError is the rule that an axis is never
// substituted: reading under cp037 because cp500 was asked for would read most
// of a file correctly and its brackets wrongly.
func TestASpellingThisAdapterCannotStateIsAnError(t *testing.T) {
	for name, stated := range map[string]*conformance.Axes{
		"a code page with no table": {Charset: "cp500"},
		"a sign convention":         {SignConvention: "zoned"},
		"a byte order":              {ByteOrder: "middle-endian"},
		"a float format":            {FloatFormat: "ieee"},
		"a staircase":               {BinarySize: "248"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Replace(codec.Encoding{}, stated); err == nil {
				t.Errorf("%+v was accepted", *stated)
			}
		})
	}
}
