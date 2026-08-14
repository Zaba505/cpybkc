// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package conformance

import (
	"math"
	"strconv"
	"strings"
)

// The three sentinels a float that is not a number is written as, and the
// prefix and the exponent marker of the hexadecimal form. They are spelled
// here once because the value language is case-sensitive — docs/conformance/SPEC.md
// writes the grammar in RFC 7405's %s notation for exactly that reason — and a
// comparison that is string equality cannot afford 0X1P+3 and 0x1p+3 to be two
// spellings of one value.
const (
	notANumber        = "NaN"
	positiveInfinity  = "Infinity"
	negativeInfinity  = "-" + positiveInfinity
	hexPrefix         = "0x"
	hexExponentMarker = "p"
)

// FormatFloat writes a COMP-1 or COMP-2 value in the form the corpus's value
// language states: one of three sentinels, or the exact value in hexadecimal
// significand notation. See docs/conformance/SPEC.md, "A float is written
// exactly, and never as a JSON number", which this function is the Go reading
// of (#194, #195).
//
// It is never a JSON number. Four things break under one and each is why this
// form exists: NaN and the infinities are not JSON numbers and most writers
// refuse them outright, so an honest generator decoding an IEEE NaN would make
// a harness that could not write a document at all; a negative zero and a
// positive one compare equal under IEEE equality, so a generator that lost the
// sign of a zero would pass; an authored decimal is not a COMP-1 value, so an
// entry's author would have to compute the double an implementation must print;
// and HFP long carries more fraction bits than a double, so a form that went
// through a decimal double could not state every value a correct HFP decoder
// produces.
//
// One value has exactly one spelling, which is what lets the comparison be
// string equality. [strconv.FormatFloat] with 'x' writes the value exactly but
// not canonically — it pads the exponent to two digits, so 1 comes back as
// 0x1p+00 — so its output is normalized here rather than returned. Every other
// language's hexadecimal float formatter differs from the canonical form in a
// way of its own, which the spec says at length; each implementation normalizes
// its own.
//
// A float32 is passed through a float64, which is exact: every binary32 value
// is a binary64 value, and the hexadecimal form of the widened value is the
// hexadecimal form of the original.
func FormatFloat(f float64) string {
	switch {
	case math.IsNaN(f):
		// Every NaN, whatever its sign and its payload. The value language does
		// not distinguish them: an entry demanding a particular payload would be
		// an entry only the generators that preserve one could pass, and the
		// bits a NaN carries are not a decoded value.
		return notANumber
	case math.IsInf(f, 1):
		return positiveInfinity
	case math.IsInf(f, -1):
		return negativeInfinity
	}

	// The sign is taken off with [math.Signbit] rather than by testing f < 0,
	// which is false for a negative zero — the value this whole form exists to
	// be able to spell.
	sign := ""
	if math.Signbit(f) {
		sign, f = "-", -f
	}

	if f == 0 {
		// Zero has one spelling and not the infinitely many an unconstrained
		// exponent would give it, so that two correct generators are never
		// reported as disagreeing about zero.
		return sign + hexPrefix + "0" + hexExponentMarker + "+0"
	}

	// 'x' with a negative precision writes the smallest number of hexadecimal
	// digits that represents the value exactly, and normalizes every non-zero
	// value — including a subnormal one — to a significand of 1.
	significand, exponent, _ := strings.Cut(
		strings.TrimPrefix(strconv.FormatFloat(f, 'x', -1, 64), hexPrefix), hexExponentMarker)

	return sign + hexPrefix + canonicalSignificand(significand) +
		hexExponentMarker + canonicalExponent(exponent)
}

// canonicalSignificand drops a trailing zero from the fraction, and the point
// with it where nothing is left behind it.
//
// Go's shortest-exact rendering never writes one, since a trailing zero is
// never needed to represent a value exactly. It is dropped anyway because the
// canonical form forbids it and this function is where the form is stated: a
// rule enforced only by a property of somebody else's formatter is a rule that
// changes when that formatter does.
func canonicalSignificand(significand string) string {
	whole, fraction, ok := strings.Cut(significand, ".")
	if !ok {
		return significand
	}

	if fraction = strings.TrimRight(fraction, "0"); fraction == "" {
		return whole
	}

	return whole + "." + fraction
}

// canonicalExponent writes an exponent's sign always and its leading zeros
// never, which is where Go's rendering and the canonical form differ: Go pads
// to two digits, so 1 is 0x1p+00 and 0.03125 is 0x1p-05.
//
// An exponent of zero is written +0, never -0, so that one value keeps one
// spelling.
func canonicalExponent(exponent string) string {
	sign := "+"
	if rest, ok := strings.CutPrefix(exponent, "-"); ok {
		sign, exponent = "-", rest
	} else {
		exponent = strings.TrimPrefix(exponent, "+")
	}

	if digits := strings.TrimLeft(exponent, "0"); digits != "" {
		return sign + digits
	}

	return "+0"
}
