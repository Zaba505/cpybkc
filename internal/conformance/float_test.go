// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package conformance

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"testing"
)

// TestFormatFloatWritesTheCanonicalForm walks every rule
// docs/conformance/SPEC.md, "A float is written exactly, and never as a JSON
// number", states about the spelling of a value.
//
// The expected strings are written out rather than derived, because deriving
// one from the same formatter the function calls would assert that the function
// agrees with itself. Each is read from the grammar and the MUSTs beside it.
func TestFormatFloatWritesTheCanonicalForm(t *testing.T) {
	tests := map[string]struct {
		value float64
		want  string
	}{
		// The three sentinels, which are why the form is a string at all.
		"a NaN":                    {value: math.NaN(), want: "NaN"},
		"a NaN with the sign bit":  {value: -math.NaN(), want: "NaN"},
		"a NaN carrying a payload": {value: math.Float64frombits(0x7ff8000000000123), want: "NaN"},
		"a positive infinity":      {value: math.Inf(1), want: "Infinity"},
		"a negative infinity":      {value: math.Inf(-1), want: "-Infinity"},

		// Zero has one spelling per sign, and the exponent of a zero is +0
		// whatever a stored exponent happens to hold.
		"a zero":          {value: 0, want: "0x0p+0"},
		"a negative zero": {value: math.Copysign(0, -1), want: "-0x0p+0"},

		// The values the corpus's five migrated float entries carry.
		"one":       {value: 1, want: "0x1p+0"},
		"nine":      {value: 9, want: "0x1.2p+3"},
		"a 32nd":    {value: 0.03125, want: "0x1p-5"},
		"minus two": {value: -2, want: "-0x1p+1"},

		// The exponent's sign is always written and its leading zeros never,
		// which is the whole of what Go's rendering gets wrong: it pads to two
		// digits, so these come back as p+00, p+10 and p-04 from strconv.
		"an exponent Go pads":               {value: 1024, want: "0x1p+10"},
		"a fraction that needs every bit":   {value: 0.1, want: "0x1.999999999999ap-4"},
		"a float32 widened to a float64":    {value: float64(float32(0.1)), want: "0x1.99999ap-4"},
		"the largest float64":               {value: math.MaxFloat64, want: "0x1.fffffffffffffp+1023"},
		"a subnormal float64, normalized":   {value: math.SmallestNonzeroFloat64, want: "0x1p-1074"},
		"a subnormal float32, normalized":   {value: float64(math.Float32frombits(1)), want: "0x1p-149"},
		"a value with a two-digit exponent": {value: -1e-300, want: "-0x1.56e1fc2f8f359p-997"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := FormatFloat(test.value); got != test.want {
				t.Fatalf("the form is %q and the specification writes it %q", got, test.want)
			}
		})
	}
}

// TestFormatFloatIsAlwaysTheGrammar holds a spread of values to the ABNF the
// spec writes the form in, which the table above checks one spelling at a time.
//
// It is here because the rules that fire rarely — the fraction that must not
// end in a zero, the exponent that must not lead with one — fire on values
// nobody thinks to tabulate, and a grammar is what covers the ones nobody did.
func TestFormatFloatIsAlwaysTheGrammar(t *testing.T) {
	// The grammar, plus the two MUSTs that constrain a spelling across the
	// parts of it a production cannot see: a zero exponent is written +0 and
	// never -0, and where the significand is 0 the exponent is +0.
	grammar := regexp.MustCompile(
		`^(NaN|-?Infinity|-?0x(0p\+0|1(\.[0-9a-f]*[1-9a-f])?p(\+0|[+-][1-9][0-9]*)))$`)

	values := []float64{
		math.NaN(), math.Inf(1), math.Inf(-1), 0, math.Copysign(0, -1),
		1, -1, 2, 9, 0.03125, 0.1, 1.0 / 3.0, 1024, -1e-300, 1e300,
		math.MaxFloat64, -math.MaxFloat64,
		math.SmallestNonzeroFloat64, float64(math.Float32frombits(1)),
		float64(float32(0.1)), math.Pi, -math.Pi,
	}

	for i := range 1024 {
		values = append(values, float64(i)/7, -float64(i)*1e10)
	}

	for _, value := range values {
		form := FormatFloat(value)

		if !grammar.MatchString(form) {
			t.Errorf("%v is written %q, which the grammar does not admit", value, form)
		}

		// Exact both ways: the form states the value and nothing beside it. A
		// canonical spelling that lost a bit would still match the grammar.
		if math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}

		read, err := strconv.ParseFloat(form, 64)
		if err != nil {
			t.Errorf("%q does not read back as a float: %v", form, err)

			continue
		}

		if read != value || math.Signbit(read) != math.Signbit(value) {
			t.Errorf("%q reads back as %v, and it was written for %v", form, read, value)
		}
	}
}

// TestTheNormalizersCarryTheirOwnRules exercises the two helpers directly.
//
// Both hold rules that [FormatFloat] cannot reach them with today: Go's
// shortest-exact rendering never writes a trailing zero in the fraction, and it
// never writes an exponent of -0. Those branches exist because the canonical
// form forbids both and this package is where the form is stated rather than
// where somebody else's formatter is trusted — and code that exists to survive
// a change in that formatter is exactly the code a table-driven test of
// [FormatFloat] leaves uncovered, so it is covered here instead.
func TestTheNormalizersCarryTheirOwnRules(t *testing.T) {
	t.Run("the significand", func(t *testing.T) {
		tests := map[string]string{
			"1":      "1",
			"1.2":    "1.2",
			"1.20":   "1.2",
			"1.000":  "1",
			"1.":     "1",
			"0":      "0",
			"1.0a0b": "1.0a0b",
		}

		for significand, want := range tests {
			if got := canonicalSignificand(significand); got != want {
				t.Errorf("%q normalizes to %q and the form writes it %q", significand, got, want)
			}
		}
	})

	t.Run("the exponent", func(t *testing.T) {
		tests := map[string]string{
			"+0":    "+0",
			"+00":   "+0",
			"-0":    "+0",
			"-00":   "+0",
			"0":     "+0",
			"+03":   "+3",
			"-05":   "-5",
			"+10":   "+10",
			"+1023": "+1023",
			"-1074": "-1074",
			"":      "+0",
		}

		for exponent, want := range tests {
			if got := canonicalExponent(exponent); got != want {
				t.Errorf("%q normalizes to %q and the form writes it %q", exponent, got, want)
			}
		}
	})
}

// TestFormatFloatSeparatesTheSignsOfZero is the defect the form was changed
// for, asserted on its own rather than left to be inferred from the table.
//
// Under the JSON number this form replaced the two compared equal, because Go's
// != on an any holding a float64 is IEEE equality — so a generator that lost the
// sign of a zero passed, and the sign of a zero is a real distinction in both
// IEEE 754 and HFP.
func TestFormatFloatSeparatesTheSignsOfZero(t *testing.T) {
	positive, negative := FormatFloat(0), FormatFloat(math.Copysign(0, -1))

	if positive == negative {
		t.Fatalf("both zeros are written %q", positive)
	}
}

// TestFormatFloatIsWritableAsJSON asserts the other defect: encoding/json
// refuses a NaN and an infinity as a number, so a driver returning one made the
// harness fail to write a document at all — which the corpus format defines as
// the harness breaking rather than as a conformance failure, and which would
// have been reported against the generator that correctly decoded it.
func TestFormatFloatIsWritableAsJSON(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := json.Marshal(value); err == nil {
			t.Errorf("%v marshals as a JSON number, and this test is about it not doing so", value)
		}

		if _, err := json.Marshal(FormatFloat(value)); err != nil {
			t.Errorf("%v is written %q, which does not marshal: %v", value, FormatFloat(value), err)
		}
	}
}
