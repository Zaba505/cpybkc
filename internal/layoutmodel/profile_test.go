// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/layout"
)

// profile is the whole pipeline a caller runs: parse the source, then read the
// encoding layer out of it.
func profile(t *testing.T, source string) (*Profile, error) {
	t.Helper()

	file, err := layout.Parse("layout.sexpr", strings.NewReader(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	return ReadProfile(file)
}

// render draws a profile the way the tests assert one: the axes in SPEC.md's
// order, then the overrides in the order the layout writes them, each with the
// position it was read at.
//
// It is a rendering rather than a struct literal for the reason
// [github.com/Zaba505/cpybkc/internal/layout]'s tests give: what has to be right
// is the model *and* the position of every part of it, and a rendering carrying
// both fails with the whole model in the message.
func render(p *Profile) string {
	lines := []string{fmt.Sprintf("%s encoding", p.Pos)}
	lines = append(lines, renderAxes(p.Axes)...)

	for _, override := range p.Overrides {
		lines = append(lines, fmt.Sprintf("%s override %s", override.Pos, override.Item))
		lines = append(lines, renderAxes(override.Axes)...)
	}

	return strings.Join(lines, "\n")
}

// renderAxes draws the axes a value states, indented under whatever states them.
func renderAxes(axes Axes) []string {
	var lines []string

	for _, axis := range axes.Stated() {
		lines = append(lines, fmt.Sprintf("  %s %s", axis, axes.value(axis)))
	}

	return lines
}

// TestReadProfileModelsTheEncodingLayer is the criterion this reader exists for:
// a layout's encoding layer becomes a typed value naming what the layout said,
// with a position on every part of it.
func TestReadProfileModelsTheEncodingLayer(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name: "the four axes and nothing else",
			source: strings.Join([]string{
				"(encoding",
				"  (charset cp037)",
				"  (sign-convention ebcdic)",
				"  (byte-order big-endian)",
				"  (float-format hfp))",
			}, "\n"),
			want: []string{
				"layout.sexpr:1:1 encoding",
				"  charset cp037",
				"  sign-convention ebcdic",
				"  byte-order big-endian",
				"  float-format hfp",
			},
		},
		{
			name: "the axes may be written in any order",
			source: strings.Join([]string{
				"(encoding",
				"  (float-format ieee-754)",
				"  (byte-order little-endian)",
				"  (sign-convention ascii-zone-37)",
				"  (charset ascii))",
			}, "\n"),
			want: []string{
				"layout.sexpr:1:1 encoding",
				"  charset ascii",
				"  sign-convention ascii-zone-37",
				"  byte-order little-endian",
				"  float-format ieee-754",
			},
		},
		{
			name: "an override states one axis and leaves the rest",
			source: strings.Join([]string{
				"(encoding",
				"  (charset cp037)",
				"  (sign-convention ebcdic)",
				"  (byte-order big-endian)",
				"  (float-format hfp))",
				"(encoding-override (item ORDER-HEADER OH-PARTNER-REF)",
				"  (charset ascii))",
			}, "\n"),
			want: []string{
				"layout.sexpr:1:1 encoding",
				"  charset cp037",
				"  sign-convention ebcdic",
				"  byte-order big-endian",
				"  float-format hfp",
				"layout.sexpr:6:1 override (item ORDER-HEADER OH-PARTNER-REF)",
				"  charset ascii",
			},
		},
		{
			name: "an override naming all four replaces the profile for its item",
			source: strings.Join([]string{
				"(encoding",
				"  (charset cp037)",
				"  (sign-convention ebcdic)",
				"  (byte-order big-endian)",
				"  (float-format hfp))",
				"(encoding-override (item ORDER-DETAIL OD-CONVERTED)",
				"  (charset ascii)",
				"  (sign-convention translated-ebcdic)",
				"  (byte-order little-endian)",
				"  (float-format ieee-754))",
			}, "\n"),
			want: []string{
				"layout.sexpr:1:1 encoding",
				"  charset cp037",
				"  sign-convention ebcdic",
				"  byte-order big-endian",
				"  float-format hfp",
				"layout.sexpr:6:1 override (item ORDER-DETAIL OD-CONVERTED)",
				"  charset ascii",
				"  sign-convention translated-ebcdic",
				"  byte-order little-endian",
				"  float-format ieee-754",
			},
		},
		{
			name: "an override may name a group, deep in a record",
			source: strings.Join([]string{
				"(encoding",
				"  (charset cp500)",
				"  (sign-convention realia)",
				"  (byte-order big-endian)",
				"  (float-format hfp))",
				"(encoding-override (item ORDER-HEADER OH-KEY OH-CUST-NO)",
				"  (sign-convention ascii-zone-37))",
			}, "\n"),
			want: []string{
				"layout.sexpr:1:1 encoding",
				"  charset cp500",
				"  sign-convention realia",
				"  byte-order big-endian",
				"  float-format hfp",
				"layout.sexpr:6:1 override (item ORDER-HEADER OH-KEY OH-CUST-NO)",
				"  sign-convention ascii-zone-37",
			},
		},
		{
			name: "the other layers are not this reader's and are not faults",
			source: strings.Join([]string{
				"(framing (recfm VB) (lrecl 512))",
				"(encoding",
				"  (charset cp1140)",
				"  (sign-convention ebcdic)",
				"  (byte-order big-endian)",
				"  (float-format hfp))",
				"(record FILE-HEADER (copybook \"cpy/orders.cpy\" FILE-HEADER-REC))",
			}, "\n"),
			want: []string{
				"layout.sexpr:2:1 encoding",
				"  charset cp1140",
				"  sign-convention ebcdic",
				"  byte-order big-endian",
				"  float-format hfp",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			read, err := profile(t, testCase.source)
			if err != nil {
				t.Fatalf("read profile: %v", err)
			}

			if got, want := render(read), strings.Join(testCase.want, "\n"); got != want {
				t.Errorf("read as\n%s\nwant\n%s", got, want)
			}
		})
	}
}

// TestReadProfileAcceptsEveryAdmittedValue holds the reader to the sets the axes
// admit, member by member, so that a value SPEC.md names and this package does
// not read is a failure here rather than a layout an adopter cannot write.
func TestReadProfileAcceptsEveryAdmittedValue(t *testing.T) {
	t.Parallel()

	for _, axis := range allAxes {
		for _, value := range axis.Values() {
			t.Run(fmt.Sprintf("%s %s", axis, value), func(t *testing.T) {
				t.Parallel()

				// The axis under test is written last, over a profile that
				// already states all four, which is the same reading either way
				// round and keeps the source one line per axis.
				source := strings.Join([]string{
					"(encoding",
					"  (charset cp037)",
					"  (sign-convention ebcdic)",
					"  (byte-order big-endian)",
					"  (float-format hfp))",
					fmt.Sprintf("(encoding-override (item R F) (%s %s))", axis, value),
				}, "\n")

				read, err := profile(t, source)
				if err != nil {
					t.Fatalf("read profile: %v", err)
				}

				if len(read.Overrides) != 1 {
					t.Fatalf("read %d overrides, want 1", len(read.Overrides))
				}

				if got := read.Overrides[0].Axes.value(axis); got != value {
					t.Errorf("%s read as %q, want %q", axis, got, value)
				}
			})
		}
	}
}

// TestReadProfileRejects is the load-bearing half: a reader that accepts
// everything passes the tests above.
//
// Each case states the message it expects in full, because the message is what
// an adopter acts on — docs/layout/SPEC.md requires a diagnostic to "name what
// it found and where", and an assertion on the type alone would pass on a
// message that names neither.
func TestReadProfileRejects(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name:   "a layout with no encoding form",
			source: "(framing (recfm VB))",
			want:   []string{"layout.sexpr:1:1: a layout carries exactly one encoding form, and this one carries 0"},
		},
		{
			name: "a second encoding form",
			source: strings.Join([]string{
				"(encoding (charset cp037) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))",
				"(encoding (charset ascii) (sign-convention ascii-zone-37) (byte-order little-endian) (float-format ieee-754))",
			}, "\n"),
			want: []string{"layout.sexpr:2:1: a layout carries exactly one encoding form, and this one carries 2"},
		},
		{
			name:   "an axis nobody stated is named, and is not defaulted",
			source: "(encoding (charset cp037) (float-format hfp))",
			want: []string{
				"layout.sexpr:1:1: the encoding profile states no sign-convention; all four axes are required and none of them has a default",
				"layout.sexpr:1:1: the encoding profile states no byte-order; all four axes are required and none of them has a default",
			},
		},
		{
			name:   "a code page nobody has a table for",
			source: "(encoding (charset cp1252) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))",
			want: []string{
				"layout.sexpr:1:20: charset is one of cp037, cp500, cp1047, cp1140 or ascii, and this one says \"cp1252\"",
				"layout.sexpr:1:1: the encoding profile states no charset; all four axes are required and none of them has a default",
			},
		},
		{
			name:   "a sign convention spelled as codec/SPEC.md's Go identifier",
			source: "(encoding (charset cp037) (sign-convention SignEBCDIC) (byte-order big-endian) (float-format hfp))",
			want: []string{
				"layout.sexpr:1:44: sign-convention is one of ebcdic, ascii-zone-37, translated-ebcdic or realia, and this one says \"SignEBCDIC\"",
				"layout.sexpr:1:1: the encoding profile states no sign-convention; all four axes are required and none of them has a default",
			},
		},
		{
			name:   "a byte order abbreviated",
			source: "(encoding (charset cp037) (sign-convention ebcdic) (byte-order little) (float-format hfp))",
			want: []string{
				"layout.sexpr:1:64: byte-order is one of big-endian or little-endian, and this one says \"little\"",
				"layout.sexpr:1:1: the encoding profile states no byte-order; all four axes are required and none of them has a default",
			},
		},
		{
			name:   "an axis form with no value",
			source: "(encoding (charset) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))",
			want: []string{
				"layout.sexpr:1:11: form \"charset\" takes one symbol naming its value, and this one has no value",
				"layout.sexpr:1:1: the encoding profile states no charset; all four axes are required and none of them has a default",
			},
		},
		{
			name:   "an axis form with two",
			source: "(encoding (charset cp037 cp500) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))",
			want: []string{
				"layout.sexpr:1:11: form \"charset\" takes one symbol naming its value, and this one has several",
				"layout.sexpr:1:1: the encoding profile states no charset; all four axes are required and none of them has a default",
			},
		},
		{
			name:   "a code page written as text",
			source: "(encoding (charset \"cp037\") (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))",
			want: []string{
				"layout.sexpr:1:20: form \"charset\" takes one symbol naming its value, and this one has text",
				"layout.sexpr:1:1: the encoding profile states no charset; all four axes are required and none of them has a default",
			},
		},
		{
			name:   "one axis stated twice",
			source: "(encoding (charset cp037) (charset cp500) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))",
			want:   []string{"layout.sexpr:1:27: form \"encoding\" states charset twice, and states it first at layout.sexpr:1:11"},
		},
		{
			name:   "a child that is not an axis",
			source: "(encoding (charset cp037) (lrecl 512) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))",
			want: []string{
				"layout.sexpr:1:28: form \"encoding\" admits charset, sign-convention, byte-order or float-format, and this is form \"lrecl\"",
			},
		},
		{
			name:   "a child that is not a form at all",
			source: "(encoding cp037 (charset cp037) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))",
			want: []string{
				"layout.sexpr:1:11: form \"encoding\" admits charset, sign-convention, byte-order or float-format, and this is the symbol \"cp037\"",
			},
		},
		{
			name: "an override naming no axis",
			source: strings.Join([]string{
				"(encoding (charset cp037) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))",
				"(encoding-override (item ORDER-HEADER OH-PARTNER-REF))",
			}, "\n"),
			want: []string{
				"layout.sexpr:2:1: the override on (item ORDER-HEADER OH-PARTNER-REF) states no axis; an override states at least one of charset, sign-convention, byte-order or float-format",
			},
		},
		{
			name: "two overrides on one item",
			source: strings.Join([]string{
				"(encoding (charset cp037) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))",
				"(encoding-override (item ORDER-HEADER OH-PARTNER-REF) (charset ascii))",
				"(encoding-override (item ORDER-HEADER OH-PARTNER-REF) (charset cp500))",
			}, "\n"),
			want: []string{
				"layout.sexpr:3:1: (item ORDER-HEADER OH-PARTNER-REF) is overridden twice, and is overridden first at layout.sexpr:2:1; two overrides on one item would leave the order they were written in deciding the answer",
			},
		},
		{
			name: "an override naming nothing at all",
			source: strings.Join([]string{
				"(encoding (charset cp037) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))",
				"(encoding-override)",
			}, "\n"),
			want: []string{
				"layout.sexpr:2:1: an item reference is written (item <record-name> <name> ...), and this is an override naming no item at all",
			},
		},
		{
			name: "an override whose reference names a record and no item",
			source: strings.Join([]string{
				"(encoding (charset cp037) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))",
				"(encoding-override (item ORDER-HEADER) (charset ascii))",
			}, "\n"),
			want: []string{
				"layout.sexpr:2:20: an item reference is written (item <record-name> <name> ...), and this is a reference naming a record and no item below it",
			},
		},
		{
			name: "an override naming its item as text",
			source: strings.Join([]string{
				"(encoding (charset cp037) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))",
				"(encoding-override \"ORDER-HEADER.OH-PARTNER-REF\" (charset ascii))",
			}, "\n"),
			want: []string{
				"layout.sexpr:2:20: an item reference is written (item <record-name> <name> ...), and this is text",
			},
		},
		{
			name: "a fault under a misspelled reference is reported too",
			source: strings.Join([]string{
				"(encoding (charset cp037) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))",
				"(encoding-override (item) (charset cp1252))",
			}, "\n"),
			want: []string{
				"layout.sexpr:2:20: an item reference is written (item <record-name> <name> ...), and this is a reference naming nothing",
				"layout.sexpr:2:36: charset is one of cp037, cp500, cp1047, cp1140 or ascii, and this one says \"cp1252\"",
			},
		},
		{
			name: "every fault is reported, in the order the layout states them",
			source: strings.Join([]string{
				"(encoding-override (item R F) (byte-order sideways))",
				"(encoding (charset cp037) (sign-convention ebcdic))",
			}, "\n"),
			want: []string{
				"layout.sexpr:1:43: byte-order is one of big-endian or little-endian, and this one says \"sideways\"",
				"layout.sexpr:1:1: the override on (item R F) states no axis; an override states at least one of charset, sign-convention, byte-order or float-format",
				"layout.sexpr:2:1: the encoding profile states no byte-order; all four axes are required and none of them has a default",
				"layout.sexpr:2:1: the encoding profile states no float-format; all four axes are required and none of them has a default",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			read, err := profile(t, testCase.source)
			if err == nil {
				t.Fatalf("read as %s, want a fault", render(read))
			}

			if read != nil {
				t.Errorf("a rejected layout yielded a profile: %s", render(read))
			}

			if got, want := err.Error(), strings.Join(testCase.want, "\n"); got != want {
				t.Errorf("reported\n%s\nwant\n%s", got, want)
			}
		})
	}
}

// TestFaultsAreAssertable is the other requirement on a fault: a caller deciding
// what to do about one reaches for the type rather than for the text of the
// message.
func TestFaultsAreAssertable(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		assert func(*testing.T, error)
	}{
		{
			name:   "a missing axis names which one",
			source: "(encoding (charset cp037) (sign-convention ebcdic) (byte-order big-endian))",
			assert: func(t *testing.T, err error) {
				var fault *MissingAxisError
				if !errors.As(err, &fault) {
					t.Fatalf("no MissingAxisError in %v", err)
				}

				if fault.Axis != AxisFloatFormat {
					t.Errorf("missing axis is %s, want float-format", fault.Axis)
				}
			},
		},
		{
			name:   "an unsupported code page carries what was written",
			source: "(encoding (charset utf-8) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))",
			assert: func(t *testing.T, err error) {
				var fault *AxisValueError
				if !errors.As(err, &fault) {
					t.Fatalf("no AxisValueError in %v", err)
				}

				if fault.Axis != AxisCharset || fault.Value != "utf-8" {
					t.Errorf("fault is %s %q, want charset \"utf-8\"", fault.Axis, fault.Value)
				}
			},
		},
		{
			name: "a duplicate override carries both positions",
			source: strings.Join([]string{
				"(encoding (charset cp037) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))",
				"(encoding-override (item R F) (charset ascii))",
				"(encoding-override (item R F) (charset cp500))",
			}, "\n"),
			assert: func(t *testing.T, err error) {
				var fault *DuplicateOverrideError
				if !errors.As(err, &fault) {
					t.Fatalf("no DuplicateOverrideError in %v", err)
				}

				if fault.First.Line != 2 || fault.Pos.Line != 3 {
					t.Errorf("fault is at %s, first at %s; want lines 3 and 2", fault.Pos, fault.First)
				}
			},
		},
		{
			name:   "a layout with no profile",
			source: "(framing (recfm VB))",
			assert: func(t *testing.T, err error) {
				var fault *ProfileCountError
				if !errors.As(err, &fault) {
					t.Fatalf("no ProfileCountError in %v", err)
				}

				if fault.Count != 0 {
					t.Errorf("count is %d, want 0", fault.Count)
				}
			},
		},
		{
			name: "an override that states nothing",
			source: strings.Join([]string{
				"(encoding (charset cp037) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))",
				"(encoding-override (item R F))",
			}, "\n"),
			assert: func(t *testing.T, err error) {
				var fault *EmptyOverrideError
				if !errors.As(err, &fault) {
					t.Fatalf("no EmptyOverrideError in %v", err)
				}

				if fault.Item.Record != "R" {
					t.Errorf("item is %s, want one on record R", fault.Item)
				}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := profile(t, testCase.source)
			if err == nil {
				t.Fatal("read cleanly, want a fault")
			}

			testCase.assert(t, err)
		})
	}
}

// TestTheSpecsWorkedExampleReads is the staleness gate over the layer.
//
// docs/layout/SPEC.md's "A layout, end to end" appendix is the layout the
// document shows an adopter, and it is read out of the document rather than
// copied here for the reason
// [github.com/Zaba505/cpybkc/internal/layout]'s tests read it: an encoding form
// the example writes and this reader does not read would otherwise be invisible
// until somebody pasted the example into a file.
//
// What it asserts is the appendix's own claim — a shop's mainframe is EBCDIC and
// one partner field never was — which is the whole of the layer in one layout.
func TestTheSpecsWorkedExampleReads(t *testing.T) {
	t.Parallel()

	read, err := profile(t, specExample(t))
	if err != nil {
		t.Fatalf("the reader rejects SPEC.md's own worked example: %v", err)
	}

	want := Axes{
		Charset:        CP037,
		SignConvention: SignEBCDIC,
		ByteOrder:      BigEndian,
		FloatFormat:    HFP,
	}

	if read.Axes != want {
		t.Errorf("the profile reads as %+v, want %+v", read.Axes, want)
	}

	if len(read.Overrides) != 1 {
		t.Fatalf("read %d overrides out of the appendix, want 1", len(read.Overrides))
	}

	override := read.Overrides[0]
	if got := override.Item.String(); got != "(item ORDER-HEADER OH-PARTNER-REF)" {
		t.Errorf("the override is on %s, want (item ORDER-HEADER OH-PARTNER-REF)", got)
	}

	// The partner reference is ASCII characters in a file that is EBCDIC
	// everywhere else, and nothing else about that field moves.
	applied := want
	applied.Charset = ASCII

	if got := read.Applied(override); got != applied {
		t.Errorf("the override applies as %+v, want %+v", got, applied)
	}
}

// specExample returns the layout in docs/layout/SPEC.md's "A layout, end to end"
// appendix: the first fenced block under that heading.
//
// [github.com/Zaba505/cpybkc/internal/layout]'s tests read the same block, with
// their own copy of this. Two are a duplication worth leaving until a third
// package wants one, at which point what they share is a helper and not three
// readings of a document.
func specExample(t *testing.T) string {
	t.Helper()

	spec, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "layout", "SPEC.md"))
	if err != nil {
		t.Fatalf("read the layout SPEC: %v", err)
	}

	_, appendix, found := strings.Cut(string(spec), "## Appendix: A layout, end to end")
	if !found {
		t.Fatal("the layout SPEC has no \"A layout, end to end\" appendix")
	}

	_, block, found := strings.Cut(appendix, "```\n")
	if !found {
		t.Fatal("the appendix carries no fenced code block")
	}

	example, _, found := strings.Cut(block, "```")
	if !found {
		t.Fatal("the appendix's fenced code block is not closed")
	}

	return example
}

// repoRoot walks up from the test's working directory to the directory holding
// go.mod, which for this module is the repository root and so the directory
// docs/ sits in.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory")
		}

		dir = parent
	}
}
