// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"fmt"
	"slices"
)

// Axis is one of the four encoding axes.
//
// The set is `cobol-go`'s `codec/SPEC.md`'s and is closed: four settings have to
// be known before a byte of a data file can be read, every one of them fails
// silently when wrong, and none is recoverable from the file with certainty.
// docs/layout/SPEC.md's "The encoding profile" is the spelling of them, and this
// type is that spelling as data.
type Axis int

// The four axes, in the order docs/layout/SPEC.md's table states them, which is
// also the order every list of them is rendered in.
const (
	// AxisCharset governs alphanumeric character data, the digit zone of zoned
	// decimal, and the byte values of a separate sign.
	AxisCharset Axis = iota

	// AxisSignConvention governs how an overpunched sign is spelled.
	AxisSignConvention

	// AxisByteOrder governs binary integers.
	AxisByteOrder

	// AxisFloatFormat governs floating point.
	AxisFloatFormat
)

// allAxes is the four, in SPEC.md's order. A caller wanting them takes
// [Axes.Missing] or [Axes.Stated]; this is what those and the reader iterate.
var allAxes = []Axis{AxisCharset, AxisSignConvention, AxisByteOrder, AxisFloatFormat}

// String is the tag a layout writes the axis as.
//
// It is the layout spelling rather than a prose name because every message this
// package produces is read beside the file that provoked it: an adopter told
// that `sign-convention` is missing can search for the word they would have
// typed.
func (a Axis) String() string {
	switch a {
	case AxisCharset:
		return "charset"
	case AxisSignConvention:
		return "sign-convention"
	case AxisByteOrder:
		return "byte-order"
	case AxisFloatFormat:
		return "float-format"
	default:
		return fmt.Sprintf("Axis(%d)", int(a))
	}
}

// Values is what the axis admits, in the order docs/layout/SPEC.md states them.
//
// It is what a diagnostic names when a layout writes something else, which is
// why it is a method on the axis rather than four lists the messages reach for
// by hand: an axis and the set it admits are one fact, and a message naming the
// wrong set is a message that sends an adopter looking in the wrong place.
func (a Axis) Values() []string {
	switch a {
	case AxisCharset:
		values := make([]string, 0, len(charsets))
		for _, charset := range charsets {
			values = append(values, string(charset))
		}

		return values
	case AxisSignConvention:
		return []string{
			string(SignEBCDIC),
			string(SignASCIIZone37),
			string(SignTranslatedEBCDIC),
			string(SignRealia),
		}
	case AxisByteOrder:
		return []string{string(BigEndian), string(LittleEndian)}
	case AxisFloatFormat:
		return []string{string(IEEE754), string(HFP)}
	default:
		return nil
	}
}

// admits reports whether value is one the axis takes.
func (a Axis) admits(value string) bool {
	for _, admitted := range a.Values() {
		if admitted == value {
			return true
		}
	}

	return false
}

// Charset is a character set the charset axis admits.
//
// The set is bounded and every member is named here. `cobol-go`'s `codec` is
// forbidden from depending on golang.org/x/text, so every code page it supports
// is a table somebody wrote by hand — which makes the axis an enumeration and
// not an open string, and makes a layout naming a code page nobody has written a
// table for something to reject while a layout is being read rather than
// something to discover against a data file.
//
// A code page added to `codec/SPEC.md`'s charset axis is added here, and here
// alone: schema/layout.sexpr deliberately declares `charset` as an open symbol
// so that this registry is the only place in this repository the members are
// written down. It is still a change to what a layout may say, so it advances
// the published schema's version under docs/layout/SPEC.md's "The version is in
// the file, and it moves with the format", even though no declaration in that
// file moves with it.
type Charset string

// The code pages a layout may name. The EBCDIC ones are spelled as
// `codec/SPEC.md` spells them; `ascii` is the identity charset.
const (
	// CP037 is the US/Canada EBCDIC code page.
	CP037 Charset = "cp037"

	// CP500 is the international EBCDIC code page.
	CP500 Charset = "cp500"

	// CP1047 is the Latin-1/Open Systems EBCDIC code page.
	CP1047 Charset = "cp1047"

	// CP1140 is cp037 with the euro sign.
	CP1140 Charset = "cp1140"

	// ASCII is the identity charset, for a file that was written in ASCII or
	// converted to it.
	ASCII Charset = "ascii"
)

// charsets is the registry, in the order docs/layout/SPEC.md names them: the
// EBCDIC code pages by number, then the identity charset.
var charsets = []Charset{CP037, CP500, CP1047, CP1140, ASCII}

// Charsets is every code page the charset axis admits.
//
// The slice is a copy, so a caller cannot edit the registry by writing to what
// it was handed.
func Charsets() []Charset {
	return slices.Clone(charsets)
}

// LookupCharset resolves a code page written in a layout, reporting whether it
// is one this project supports.
func LookupCharset(value string) (Charset, bool) {
	for _, charset := range charsets {
		if string(charset) == value {
			return charset, true
		}
	}

	return "", false
}

// SignConvention is how an overpunched sign is spelled in a zoned decimal byte.
//
// The four are `codec/SPEC.md`'s, respelled: it names them as Go identifiers and
// a layout is data. docs/layout/SPEC.md's "The encoding profile" carries the
// mapping, and it is the one place either spelling is translated into the other.
type SignConvention string

// The sign conventions a layout may name.
const (
	// SignEBCDIC is codec/SPEC.md's SignEBCDIC: the one mainframe convention.
	SignEBCDIC SignConvention = "ebcdic"

	// SignASCIIZone37 is codec/SPEC.md's SignASCIIZone37: the native ASCII
	// convention, written by a program that never saw a mainframe.
	SignASCIIZone37 SignConvention = "ascii-zone-37"

	// SignTranslatedEBCDIC is codec/SPEC.md's SignTranslatedEBCDIC: what an
	// EBCDIC-to-ASCII text conversion leaves behind.
	SignTranslatedEBCDIC SignConvention = "translated-ebcdic"

	// SignRealia is codec/SPEC.md's SignRealia.
	SignRealia SignConvention = "realia"
)

// ByteOrder is the order of the bytes in a binary integer.
type ByteOrder string

// The byte orders a layout may name.
const (
	// BigEndian is the mainframe order, and what a converted file usually keeps.
	BigEndian ByteOrder = "big-endian"

	// LittleEndian is the native order on the platforms that write ASCII files.
	LittleEndian ByteOrder = "little-endian"
)

// FloatFormat is the representation of a floating point item.
type FloatFormat string

// The float formats a layout may name.
const (
	// IEEE754 is the distributed-platform format.
	IEEE754 FloatFormat = "ieee-754"

	// HFP is IBM hexadecimal floating point.
	HFP FloatFormat = "hfp"
)

// Axes is a setting of the four encoding axes, each of which may be unstated.
//
// The zero value of every axis is invalid and means "nobody said". That is
// `codec/SPEC.md`'s requirement on the axes — each "MUST have an invalid zero
// value" — carried into the source side, and it is what lets this type stand for
// both of the things a layout writes: a profile, whose four axes are all stated,
// and an override, which states as few as one.
//
// Which of the two a value is, is a question [Axes.Complete] answers, and
// [ReadProfile] answers it before handing anything back: the [Axes] on a
// [Profile] is always complete and the one on an [Override] never is empty.
type Axes struct {
	// Charset is the code page, or the empty string where none is stated.
	Charset Charset

	// SignConvention is how an overpunched sign is spelled, or the empty
	// string.
	SignConvention SignConvention

	// ByteOrder is the order of the bytes in a binary integer, or the empty
	// string.
	ByteOrder ByteOrder

	// FloatFormat is the representation of a floating point item, or the empty
	// string.
	FloatFormat FloatFormat
}

// value is what the axis says, or the empty string where it says nothing.
func (a Axes) value(axis Axis) string {
	switch axis {
	case AxisCharset:
		return string(a.Charset)
	case AxisSignConvention:
		return string(a.SignConvention)
	case AxisByteOrder:
		return string(a.ByteOrder)
	case AxisFloatFormat:
		return string(a.FloatFormat)
	default:
		return ""
	}
}

// set states an axis. The value has already been held to [Axis.admits], which is
// what makes the conversion here safe.
func (a *Axes) set(axis Axis, value string) {
	switch axis {
	case AxisCharset:
		a.Charset = Charset(value)
	case AxisSignConvention:
		a.SignConvention = SignConvention(value)
	case AxisByteOrder:
		a.ByteOrder = ByteOrder(value)
	case AxisFloatFormat:
		a.FloatFormat = FloatFormat(value)
	}
}

// Stated is the axes this value states, in docs/layout/SPEC.md's order.
func (a Axes) Stated() []Axis {
	var stated []Axis

	for _, axis := range allAxes {
		if a.value(axis) != "" {
			stated = append(stated, axis)
		}
	}

	return stated
}

// Missing is the axes this value does not state, in docs/layout/SPEC.md's order.
//
// It is what a diagnostic about an incomplete profile names, one error per axis,
// because SPEC.md requires an implementation to "report the missing one by
// name": an adopter told that their profile is incomplete has to open the file
// to find out which line to write.
func (a Axes) Missing() []Axis {
	var missing []Axis

	for _, axis := range allAxes {
		if a.value(axis) == "" {
			missing = append(missing, axis)
		}
	}

	return missing
}

// Complete reports whether all four axes are stated.
func (a Axes) Complete() bool {
	return len(a.Missing()) == 0
}

// Over applies a over base, axis by axis: an axis a states replaces base's, and
// an axis it does not leaves base's alone.
//
// This is docs/layout/SPEC.md's "An override naming one axis leaves the other
// three as the profile states them; one naming all four replaces the profile
// entirely for that item", and it is here because it is a fact about the two
// values rather than about the resolution that will use it. Which items an
// override reaches — a group reaches every elementary item under it, a repeating
// item every occurrence — needs a copybook and is `resolve`'s (#33).
func (a Axes) Over(base Axes) Axes {
	applied := base

	for _, axis := range a.Stated() {
		applied.set(axis, a.value(axis))
	}

	return applied
}
