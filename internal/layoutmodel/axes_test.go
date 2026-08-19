// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"slices"
	"testing"
)

// TestCharsetRegistryIsBounded is the property the charset axis is an
// enumeration for: a code page is either one somebody wrote a table for or it is
// not, and there is no third answer for a layout to be believed about.
func TestCharsetRegistryIsBounded(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		value string
		want  Charset
		found bool
	}{
		{name: "the US/Canada code page", value: "cp037", want: CP037, found: true},
		{name: "the international code page", value: "cp500", want: CP500, found: true},
		{name: "the Latin-1 code page", value: "cp1047", want: CP1047, found: true},
		{name: "cp037 with the euro sign", value: "cp1140", want: CP1140, found: true},
		{name: "the identity charset", value: "ascii", want: ASCII, found: true},
		{name: "a Windows code page nobody has a table for", value: "cp1252"},
		{name: "a Unicode encoding, which is not a code page", value: "utf-8"},
		{name: "the right code page under the wrong name", value: "ebcdic-us"},
		{name: "a code page written with the case a title uses", value: "CP037"},
		{name: "the statement that an item has no characters, which is not a code page", value: "none"},
		{name: "nothing at all", value: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			charset, found := LookupCharset(testCase.value)
			if found != testCase.found {
				t.Fatalf("LookupCharset(%q) found=%v, want %v", testCase.value, found, testCase.found)
			}

			if charset != testCase.want {
				t.Errorf("LookupCharset(%q) = %q, want %q", testCase.value, charset, testCase.want)
			}
		})
	}
}

// TestCharsetsIsACopy keeps the registry the one place a code page is added:
// a caller that could write to what it was handed would be a second place, and
// one that only some of the process can see.
func TestCharsetsIsACopy(t *testing.T) {
	t.Parallel()

	handed := Charsets()
	handed[0] = "cp1252"

	if charset, found := LookupCharset("cp1252"); found {
		t.Errorf("writing to the returned slice added %q to the registry", charset)
	}

	if _, found := LookupCharset("cp037"); !found {
		t.Error("writing to the returned slice removed cp037 from the registry")
	}
}

// TestAxisValuesAreTheClosedSets holds each axis to what docs/layout/SPEC.md's
// table says it admits, in the order the table states it, because that order is
// what every diagnostic naming the set is read in.
//
// Each axis is held in both positions, because the set is a function of both:
// `none` is a statement about one item, so an `encoding-override` admits it and
// the `encoding` profile does not, and an axis that admitted the same values in
// both would pass a test that only asked about one of them.
func TestAxisValuesAreTheClosedSets(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		axis     Axis
		tag      string
		want     []string
		override []string
	}{
		{
			axis:     AxisCharset,
			tag:      "charset",
			want:     []string{"cp037", "cp500", "cp1047", "cp1140", "ascii"},
			override: []string{"cp037", "cp500", "cp1047", "cp1140", "ascii", "none"},
		},
		{
			axis:     AxisSignConvention,
			tag:      "sign-convention",
			want:     []string{"ebcdic", "ascii-zone-37", "translated-ebcdic", "realia"},
			override: []string{"ebcdic", "ascii-zone-37", "translated-ebcdic", "realia"},
		},
		{
			axis:     AxisByteOrder,
			tag:      "byte-order",
			want:     []string{"big-endian", "little-endian"},
			override: []string{"big-endian", "little-endian"},
		},
		{
			axis:     AxisFloatFormat,
			tag:      "float-format",
			want:     []string{"ieee-754", "hfp"},
			override: []string{"ieee-754", "hfp"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.tag, func(t *testing.T) {
			t.Parallel()

			if got := testCase.axis.String(); got != testCase.tag {
				t.Errorf("axis renders as %q, want %q", got, testCase.tag)
			}

			if got := testCase.axis.Values(false); !slices.Equal(got, testCase.want) {
				t.Errorf("the profile admits %v, want %v", got, testCase.want)
			}

			if got := testCase.axis.Values(true); !slices.Equal(got, testCase.override) {
				t.Errorf("an override admits %v, want %v", got, testCase.override)
			}
		})
	}
}

// TestOnlyAnOverrideAdmitsCharsetNone is the positional half stated on its own,
// because it is the whole reason [Axis.Values] takes a position: `none` says one
// item's bytes are a payload, and the profile names no item for that to be about.
func TestOnlyAnOverrideAdmitsCharsetNone(t *testing.T) {
	t.Parallel()

	if AxisCharset.admits(string(None), false) {
		t.Error("the encoding profile admits charset none, which is a statement about one item")
	}

	if !AxisCharset.admits(string(None), true) {
		t.Error("an encoding-override does not admit charset none")
	}

	// The registry stays the published set of code pages, which is what a
	// caller asking which tables exist is asking about.
	if slices.Contains(Charsets(), None) {
		t.Error("none is in the code-page registry, and it is not a code page")
	}
}

// TestAxesOverAppliesAxisByAxis is docs/layout/SPEC.md's "An override naming one
// axis leaves the other three as the profile states them; one naming all four
// replaces the profile entirely for that item".
func TestAxesOverAppliesAxisByAxis(t *testing.T) {
	t.Parallel()

	base := Axes{
		Charset:        CP037,
		SignConvention: SignEBCDIC,
		ByteOrder:      BigEndian,
		FloatFormat:    HFP,
	}

	testCases := []struct {
		name     string
		override Axes
		want     Axes
	}{
		{
			name: "an override naming nothing changes nothing",
			want: base,
		},
		{
			name:     "one axis moves and the other three do not",
			override: Axes{Charset: ASCII},
			want: Axes{
				Charset:        ASCII,
				SignConvention: SignEBCDIC,
				ByteOrder:      BigEndian,
				FloatFormat:    HFP,
			},
		},
		{
			name:     "the combination real files hit, which no compiler produces",
			override: Axes{Charset: ASCII, SignConvention: SignTranslatedEBCDIC},
			want: Axes{
				Charset:        ASCII,
				SignConvention: SignTranslatedEBCDIC,
				ByteOrder:      BigEndian,
				FloatFormat:    HFP,
			},
		},
		{
			name: "all four replace the profile entirely",
			override: Axes{
				Charset:        CP1047,
				SignConvention: SignRealia,
				ByteOrder:      LittleEndian,
				FloatFormat:    IEEE754,
			},
			want: Axes{
				Charset:        CP1047,
				SignConvention: SignRealia,
				ByteOrder:      LittleEndian,
				FloatFormat:    IEEE754,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.override.Over(base); got != testCase.want {
				t.Errorf("applied as %+v, want %+v", got, testCase.want)
			}

			if base.Charset != CP037 {
				t.Error("applying an override wrote to the profile it was applied over")
			}
		})
	}
}

// TestAxesCompleteAndMissing is what makes "no axis is ever silently defaulted" a
// question about a value: an [Axes] either states four axes or names the ones it
// does not.
func TestAxesCompleteAndMissing(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		axes     Axes
		complete bool
		missing  []Axis
	}{
		{
			name:    "the zero value states nothing",
			missing: []Axis{AxisCharset, AxisSignConvention, AxisByteOrder, AxisFloatFormat},
		},
		{
			name:    "three of four is not a profile",
			axes:    Axes{Charset: CP037, SignConvention: SignEBCDIC, FloatFormat: HFP},
			missing: []Axis{AxisByteOrder},
		},
		{
			name: "all four",
			axes: Axes{
				Charset:        CP037,
				SignConvention: SignEBCDIC,
				ByteOrder:      BigEndian,
				FloatFormat:    HFP,
			},
			complete: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.axes.Complete(); got != testCase.complete {
				t.Errorf("Complete() = %v, want %v", got, testCase.complete)
			}

			if got := testCase.axes.Missing(); !slices.Equal(got, testCase.missing) {
				t.Errorf("Missing() = %v, want %v", got, testCase.missing)
			}
		})
	}
}
