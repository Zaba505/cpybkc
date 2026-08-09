// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/layout"
)

// framingOf is the whole pipeline a caller runs: parse the source, then read the
// physical framing layer out of it.
func framingOf(t *testing.T, source string) (*Framing, error) {
	t.Helper()

	file, err := layout.Parse("layout.sexpr", strings.NewReader(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	return ReadFraming(file)
}

// renderFraming draws a framing the way the tests assert one: what the layout
// said, in docs/layout/SPEC.md's order, with the framing it resolves to beside
// the record format and a position on every part that carries one.
//
// It is a rendering rather than a struct literal for [render]'s reason: what has
// to be right is the model *and* the position of every part of it, and a
// rendering carrying both fails with the whole model in the message.
func renderFraming(f *Framing) string {
	lines := []string{
		fmt.Sprintf("%s framing", f.Pos),
		fmt.Sprintf("  recfm %s", f.RECFM),
		fmt.Sprintf("  resolves to %s", f.Kind()),
	}

	if f.LRECL.Stated() {
		lines = append(lines, fmt.Sprintf("  %s lrecl %d", f.LRECL.Pos, f.LRECL.Value))
	}

	if f.BlockSize.Stated() {
		lines = append(lines, fmt.Sprintf("  %s blksize %d", f.BlockSize.Pos, f.BlockSize.Value))
	}

	lines = append(lines, fmt.Sprintf("  blocks %s", f.Blocks))

	if f.MaxSegment.Stated() {
		lines = append(lines, fmt.Sprintf("  %s max-segment %d", f.MaxSegment.Pos, f.MaxSegment.Value))
	}

	if f.Delimiter.Stated() {
		lines = append(lines, fmt.Sprintf("  %s delimiter %s", f.Delimiter.Pos, f.Delimiter))
	}

	if f.Placement != "" {
		lines = append(lines, fmt.Sprintf("  placement %s", f.Placement))
	}

	return strings.Join(lines, "\n")
}

// minimalFraming is the least a layout may say under a record format: the
// children that record format requires, and nothing it merely admits.
func minimalFraming(recfm RECFM) string {
	switch recfm {
	case RECFMF, RECFMFB:
		return fmt.Sprintf("(framing (recfm %s) (lrecl 80))", recfm)
	case RECFMVBS:
		return fmt.Sprintf("(framing (recfm %s) (max-segment 4096))", recfm)
	case LineSequential:
		return fmt.Sprintf("(framing (recfm %s) (delimiter (bytes \"0A\")) (placement terminator))", recfm)
	default:
		return fmt.Sprintf("(framing (recfm %s))", recfm)
	}
}

// TestReadFramingModelsThePhysicalLayer is the criterion this reader exists for:
// a layout's framing layer becomes a typed value naming what the layout said and
// what it resolves to, with a position on every part of it.
func TestReadFramingModelsThePhysicalLayer(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name: "a fixed-length blocked dataset",
			source: strings.Join([]string{
				"(framing",
				"  (recfm FB)",
				"  (lrecl 80)",
				"  (blksize 800))",
			}, "\n"),
			want: []string{
				"layout.sexpr:1:1 framing",
				"  recfm FB",
				"  resolves to unframed",
				"  layout.sexpr:3:10 lrecl 80",
				"  layout.sexpr:4:12 blksize 800",
				"  blocks deblocked",
			},
		},
		{
			// The record descriptor word states each record's length, so a
			// layout without an lrecl is still readable.
			name:   "a variable-length dataset need not state an lrecl",
			source: "(framing (recfm V))",
			want: []string{
				"layout.sexpr:1:1 framing",
				"  recfm V",
				"  resolves to descriptor-word",
				"  blocks deblocked",
			},
		},
		{
			name: "a spanned dataset carries the largest segment a writer may emit",
			source: strings.Join([]string{
				"(framing",
				"  (recfm VBS)",
				"  (lrecl 27994)",
				"  (blksize 27998)",
				"  (blocks deblocked)",
				"  (max-segment 4096))",
			}, "\n"),
			want: []string{
				"layout.sexpr:1:1 framing",
				"  recfm VBS",
				"  resolves to segmented",
				"  layout.sexpr:3:10 lrecl 27994",
				"  layout.sexpr:4:12 blksize 27998",
				"  blocks deblocked",
				"  layout.sexpr:6:16 max-segment 4096",
			},
		},
		{
			name: "a line-sequential file carries its delimiter as bytes",
			source: strings.Join([]string{
				"(framing",
				"  (recfm line-sequential)",
				"  (delimiter (bytes \"0D0A\"))",
				"  (placement optional-terminator))",
			}, "\n"),
			want: []string{
				"layout.sexpr:1:1 framing",
				"  recfm line-sequential",
				"  resolves to delimited",
				"  blocks deblocked",
				"  layout.sexpr:3:14 delimiter (bytes \"0D0A\")",
				"  placement optional-terminator",
			},
		},
		{
			name: "the other layers are not this reader's and are not faults",
			source: strings.Join([]string{
				"(encoding",
				"  (charset cp037)",
				"  (sign-convention ebcdic)",
				"  (byte-order big-endian)",
				"  (float-format hfp))",
				"(framing (recfm VB) (lrecl 512))",
				"(record FILE-HEADER (copybook \"cpy/orders.cpy\" FILE-HEADER-REC))",
			}, "\n"),
			want: []string{
				"layout.sexpr:6:1 framing",
				"  recfm VB",
				"  resolves to descriptor-word",
				"  layout.sexpr:6:28 lrecl 512",
				"  blocks deblocked",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			read, err := framingOf(t, testCase.source)
			if err != nil {
				t.Fatalf("read framing: %v", err)
			}

			if got, want := renderFraming(read), strings.Join(testCase.want, "\n"); got != want {
				t.Errorf("read as\n%s\nwant\n%s", got, want)
			}
		})
	}
}

// TestEveryRECFMResolvesOntoAFraming walks the whole set a `recfm` admits, so
// that a spelling docs/layout/SPEC.md names and this package does not map is a
// failure here rather than a layout an adopter cannot write.
//
// RECFM U is the one member with no framing, and it is asserted as such rather
// than skipped: that it resolves to nothing is the reason it is rejected.
func TestEveryRECFMResolvesOntoAFraming(t *testing.T) {
	t.Parallel()

	want := map[RECFM]FramingKind{
		RECFMF:         Unframed,
		RECFMFB:        Unframed,
		RECFMV:         DescriptorWord,
		RECFMVB:        DescriptorWord,
		RECFMVBS:       Segmented,
		LineSequential: Delimited,
	}

	for _, recfm := range recfms {
		t.Run(string(recfm), func(t *testing.T) {
			t.Parallel()

			kind, ok := recfm.Framing()

			if recfm == RECFMU {
				if ok {
					t.Fatalf("recfm U resolves to %s, want nothing", kind)
				}

				return
			}

			if !ok {
				t.Fatalf("recfm %s resolves to nothing", recfm)
			}

			if kind != want[recfm] {
				t.Errorf("recfm %s resolves to %s, want %s", recfm, kind, want[recfm])
			}

			read, err := framingOf(t, minimalFraming(recfm))
			if err != nil {
				t.Fatalf("the least a layout may say under recfm %s is rejected: %v", recfm, err)
			}

			if read.Kind() != want[recfm] {
				t.Errorf("the framing reads as %s, want %s", read.Kind(), want[recfm])
			}
		})
	}
}

// TestLRECLBound is the layout-side half of the check `resolve` performs against
// a record type's extent (#33–#35): the same number means "account for all of
// it" on a fixed-length dataset and "no more than this" on a variable-length
// one, and reading it as the wrong one is the misalignment the layer exists to
// prevent.
func TestLRECLBound(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   LRECLBound
	}{
		{
			name:   "under F every record type accounts for all of it",
			source: "(framing (recfm F) (lrecl 80))",
			want:   LRECLExact,
		},
		{
			name:   "under FB too",
			source: "(framing (recfm FB) (lrecl 80) (blksize 800))",
			want:   LRECLExact,
		},
		{
			name:   "under V it is a maximum",
			source: "(framing (recfm V) (lrecl 512))",
			want:   LRECLMaximum,
		},
		{
			name:   "under VBS it is a maximum",
			source: "(framing (recfm VBS) (lrecl 512) (max-segment 4096))",
			want:   LRECLMaximum,
		},
		{
			name:   "a variable-length dataset may state none",
			source: "(framing (recfm VB))",
			want:   LRECLUnstated,
		},
		{
			name:   "a line-sequential file has none to state",
			source: minimalFraming(LineSequential),
			want:   LRECLUnstated,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			read, err := framingOf(t, testCase.source)
			if err != nil {
				t.Fatalf("read framing: %v", err)
			}

			if got := read.LRECLBound(); got != testCase.want {
				t.Errorf("lrecl bound is %s, want %s", got, testCase.want)
			}
		})
	}
}

// TestTheDelimiterIsTheBytesThatWereWritten is docs/layout/SPEC.md's "A
// delimiter is bytes, and it has a placement": none of the four delimiters real
// files carry is a default, and each one is the bytes the layout wrote and not a
// character resolved through anything.
func TestTheDelimiterIsTheBytesThatWereWritten(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		hex  string
		want []byte
	}{
		{hex: "15", want: []byte{0x15}},
		{hex: "25", want: []byte{0x25}},
		{hex: "0A", want: []byte{0x0A}},
		{hex: "0D0A", want: []byte{0x0D, 0x0A}},
		{hex: "0d0a", want: []byte{0x0D, 0x0A}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.hex, func(t *testing.T) {
			t.Parallel()

			source := fmt.Sprintf(
				"(framing (recfm line-sequential) (delimiter (bytes %q)) (placement separator))",
				testCase.hex,
			)

			read, err := framingOf(t, source)
			if err != nil {
				t.Fatalf("read framing: %v", err)
			}

			if got := read.Delimiter.Bytes; string(got) != string(testCase.want) {
				t.Errorf("delimiter reads as % X, want % X", got, testCase.want)
			}

			if read.Placement != Separator {
				t.Errorf("placement reads as %q, want separator", read.Placement)
			}
		})
	}
}

// TestEveryPlacementIsWritable holds the reader to the three placements, so that
// one docs/layout/SPEC.md names and this package does not read is a failure here
// rather than an adopter unable to say what their file does.
func TestEveryPlacementIsWritable(t *testing.T) {
	t.Parallel()

	for _, placement := range placements {
		t.Run(string(placement), func(t *testing.T) {
			t.Parallel()

			source := fmt.Sprintf(
				"(framing (recfm line-sequential) (delimiter (bytes \"0A\")) (placement %s))",
				placement,
			)

			read, err := framingOf(t, source)
			if err != nil {
				t.Fatalf("read framing: %v", err)
			}

			if read.Placement != placement {
				t.Errorf("placement reads as %q, want %q", read.Placement, placement)
			}
		})
	}
}

// TestReadFramingRejects is the load-bearing half: a reader that accepts
// everything passes the tests above.
//
// Each case states the message it expects in full, because the message is what
// an adopter acts on — docs/layout/SPEC.md requires a diagnostic to "name what
// it found and where", and an assertion on the type alone would pass on a
// message that names neither.
func TestReadFramingRejects(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name:   "a layout with no framing form",
			source: "(encoding (charset cp037))",
			want:   []string{"layout.sexpr:1:1: a layout carries exactly one framing form, and this one carries 0"},
		},
		{
			name: "a second framing form",
			source: strings.Join([]string{
				"(framing (recfm FB) (lrecl 80))",
				"(framing (recfm V))",
			}, "\n"),
			want: []string{"layout.sexpr:2:1: a layout carries exactly one framing form, and this one carries 2"},
		},
		{
			// Nothing below the recfm can be judged without it, so the missing
			// record format is the whole diagnostic rather than the first of
			// six about children whose rules have nothing to key on.
			name:   "a framing that states no recfm",
			source: "(framing (lrecl 80))",
			want: []string{
				"layout.sexpr:1:1: a framing states one recfm, and this one states none; the rest of the framing follows from it",
			},
		},
		{
			name:   "RECFM U names the dataset rather than reporting a generic framing error",
			source: "(framing (recfm U) (lrecl 80))",
			want: []string{
				"layout.sexpr:1:17: recfm U is a dataset whose record extents came from the physical blocks the access " +
					"method read, and a byte stream on a filesystem has lost them; where every block was in fact the same " +
					"size the file is a fixed-length one and the layout says F or FB, and where they were not the " +
					"boundaries have to be put back by whatever writes the stream",
			},
		},
		{
			name:   "an ASA carriage control is named, and so is where it belongs",
			source: "(framing (recfm FBA) (lrecl 80))",
			want: []string{
				"layout.sexpr:1:17: recfm \"FBA\" carries an ASA carriage control, and a control character is a byte of " +
					"the record rather than a byte of the framing; declare it as a leading item in the copybook and " +
					"write recfm FB",
			},
		},
		{
			name:   "a machine carriage control is named the same way",
			source: "(framing (recfm VBM))",
			want: []string{
				"layout.sexpr:1:17: recfm \"VBM\" carries a machine carriage control, and a control character is a byte " +
					"of the record rather than a byte of the framing; declare it as a leading item in the copybook and " +
					"write recfm VB",
			},
		},
		{
			name:   "a record format nobody has",
			source: "(framing (recfm FIXED))",
			want: []string{
				"layout.sexpr:1:17: recfm is one of F, FB, V, VB, VBS, U or line-sequential, and this one says \"FIXED\"",
			},
		},
		{
			name:   "a fixed-length dataset that states no lrecl",
			source: "(framing (recfm F))",
			want:   []string{"layout.sexpr:1:1: recfm F requires \"lrecl\", and this framing states none"},
		},
		{
			name: "an lrecl on a line-sequential file, where the dataset has no such number",
			source: strings.Join([]string{
				"(framing",
				"  (recfm line-sequential)",
				"  (lrecl 80)",
				"  (delimiter (bytes \"0A\"))",
				"  (placement terminator))",
			}, "\n"),
			want: []string{"layout.sexpr:3:3: recfm line-sequential admits no \"lrecl\", and this framing states one"},
		},
		{
			name:   "a blksize on an unblocked dataset",
			source: "(framing (recfm F) (lrecl 80) (blksize 800))",
			want:   []string{"layout.sexpr:1:31: recfm F admits no \"blksize\", and this framing states one"},
		},
		{
			name:   "a spanned dataset that states no max-segment",
			source: "(framing (recfm VBS))",
			want:   []string{"layout.sexpr:1:1: recfm VBS requires \"max-segment\", and this framing states none"},
		},
		{
			name:   "a max-segment on a dataset that is not spanned",
			source: "(framing (recfm VB) (max-segment 4096))",
			want:   []string{"layout.sexpr:1:21: recfm VB admits no \"max-segment\", and this framing states one"},
		},
		{
			name:   "a line-sequential file that says nothing about its delimiter",
			source: "(framing (recfm line-sequential))",
			want: []string{
				"layout.sexpr:1:1: recfm line-sequential requires \"delimiter\", and this framing states none",
				"layout.sexpr:1:1: recfm line-sequential requires \"placement\", and this framing states none",
			},
		},
		{
			name: "a delimiter and a placement on a dataset that has neither",
			source: strings.Join([]string{
				"(framing",
				"  (recfm VB)",
				"  (delimiter (bytes \"0A\"))",
				"  (placement terminator))",
			}, "\n"),
			want: []string{
				"layout.sexpr:3:3: recfm VB admits no \"delimiter\", and this framing states one",
				"layout.sexpr:4:3: recfm VB admits no \"placement\", and this framing states one",
			},
		},
		{
			name:   "a blksize that is not a whole number of fixed-length records",
			source: "(framing (recfm FB) (lrecl 80) (blksize 27998))",
			want: []string{
				"layout.sexpr:1:41: under recfm FB a blksize holds whole records and is a multiple of lrecl, " +
					"and 27998 is not a multiple of 80",
			},
		},
		{
			name:   "a blksize with no room for a block descriptor word",
			source: "(framing (recfm VB) (lrecl 512) (blksize 512))",
			want: []string{
				"layout.sexpr:1:42: under recfm VB a blksize is at least lrecl plus the 4 bytes of a block descriptor " +
					"word, and 512 is less than 516",
			},
		},
		{
			name:   "a stream that still carries block descriptor words",
			source: "(framing (recfm FB) (lrecl 80) (blocks in-stream))",
			want: []string{
				"layout.sexpr:1:40: blocks in-stream is a dataset image rather than a record stream, and the transfer " +
					"has to deblock it; a blocked dataset is otherwise ordinary and needs nothing said about it",
			},
		},
		{
			name:   "a blocks value that is neither",
			source: "(framing (recfm FB) (lrecl 80) (blocks blocked))",
			want:   []string{"layout.sexpr:1:40: blocks is one of deblocked or in-stream, and this one says \"blocked\""},
		},
		{
			name:   "a placement nobody has",
			source: "(framing (recfm line-sequential) (delimiter (bytes \"0A\")) (placement trailing))",
			want: []string{
				"layout.sexpr:1:70: placement is one of terminator, separator or optional-terminator, " +
					"and this one says \"trailing\"",
			},
		},
		{
			name: "a delimiter written as text rather than as bytes",
			source: strings.Join([]string{
				"(framing",
				"  (recfm line-sequential)",
				"  (delimiter \"0A\")",
				"  (placement terminator))",
			}, "\n"),
			want: []string{
				"layout.sexpr:3:14: a byte string is written (bytes \"<hex>\"), hexadecimal digits in pairs, " +
					"and this is text",
			},
		},
		{
			name: "a delimiter of no bytes",
			source: strings.Join([]string{
				"(framing",
				"  (recfm line-sequential)",
				"  (delimiter (bytes \"\"))",
				"  (placement terminator))",
			}, "\n"),
			want: []string{
				"layout.sexpr:3:21: a byte string is written (bytes \"<hex>\"), hexadecimal digits in pairs, " +
					"and this is a byte string of no bytes",
			},
		},
		{
			name: "a delimiter of half a byte",
			source: strings.Join([]string{
				"(framing",
				"  (recfm line-sequential)",
				"  (delimiter (bytes \"0\"))",
				"  (placement terminator))",
			}, "\n"),
			want: []string{
				"layout.sexpr:3:21: a byte string is written (bytes \"<hex>\"), hexadecimal digits in pairs, " +
					"and this is \"0\", which is an odd number of digits",
			},
		},
		{
			name: "a delimiter that is not hexadecimal",
			source: strings.Join([]string{
				"(framing",
				"  (recfm line-sequential)",
				"  (delimiter (bytes \"0G\"))",
				"  (placement terminator))",
			}, "\n"),
			want: []string{
				"layout.sexpr:3:21: a byte string is written (bytes \"<hex>\"), hexadecimal digits in pairs, " +
					"and this is \"0G\", which is not hexadecimal",
			},
		},
		{
			name:   "an lrecl of no bytes",
			source: "(framing (recfm F) (lrecl 0))",
			want:   []string{"layout.sexpr:1:27: lrecl is a positive number of bytes, and this one says 0"},
		},
		{
			name:   "a byte count written as text",
			source: "(framing (recfm F) (lrecl \"80\"))",
			want: []string{
				"layout.sexpr:1:27: form \"lrecl\" takes one positive number of bytes, and this one has text",
			},
		},
		{
			name:   "a byte count with no value",
			source: "(framing (recfm F) (lrecl))",
			want: []string{
				"layout.sexpr:1:20: form \"lrecl\" takes one positive number of bytes, and this one has no value",
			},
		},
		{
			name:   "a byte count with a fraction",
			source: "(framing (recfm F) (lrecl 80.5))",
			want: []string{
				"layout.sexpr:1:27: form \"lrecl\" takes one positive number of bytes, and this one has a number with a fraction",
			},
		},
		{
			name:   "a record format with no value",
			source: "(framing (recfm) (lrecl 80))",
			want: []string{
				"layout.sexpr:1:10: form \"recfm\" takes one symbol naming its value, and this one has no value",
			},
		},
		{
			name:   "one child stated twice",
			source: "(framing (recfm FB) (lrecl 80) (lrecl 100))",
			want: []string{
				"layout.sexpr:1:32: form \"framing\" states \"lrecl\" twice, and states it first at layout.sexpr:1:21",
			},
		},
		{
			name:   "a child belonging to another layer",
			source: "(framing (recfm FB) (lrecl 80) (charset cp037))",
			want: []string{
				"layout.sexpr:1:33: form \"framing\" admits recfm, lrecl, blksize, blocks, max-segment, delimiter or " +
					"placement, and this is form \"charset\"",
			},
		},
		{
			name:   "a child that is not a form at all",
			source: "(framing FB (recfm FB) (lrecl 80))",
			want: []string{
				"layout.sexpr:1:10: form \"framing\" admits recfm, lrecl, blksize, blocks, max-segment, delimiter or " +
					"placement, and this is the symbol \"FB\"",
			},
		},
		{
			// The delimiter is stated, so it is not also a delimiter nobody
			// wrote: the layout plainly names one and the fault is what it says.
			name: "every fault is reported: what the children say, then what the record format requires",
			source: strings.Join([]string{
				"(framing",
				"  (recfm line-sequential)",
				"  (lrecl 80)",
				"  (delimiter (bytes \"\")))",
			}, "\n"),
			want: []string{
				"layout.sexpr:4:21: a byte string is written (bytes \"<hex>\"), hexadecimal digits in pairs, " +
					"and this is a byte string of no bytes",
				"layout.sexpr:3:3: recfm line-sequential admits no \"lrecl\", and this framing states one",
				"layout.sexpr:1:1: recfm line-sequential requires \"placement\", and this framing states none",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			read, err := framingOf(t, testCase.source)
			if err == nil {
				t.Fatalf("read as %s, want a fault", renderFraming(read))
			}

			if read != nil {
				t.Errorf("a rejected layout yielded a framing: %s", renderFraming(read))
			}

			if got, want := err.Error(), strings.Join(testCase.want, "\n"); got != want {
				t.Errorf("reported\n%s\nwant\n%s", got, want)
			}
		})
	}
}

// TestFramingFaultsAreAssertable is the other requirement on a fault: a caller
// deciding what to do about one reaches for the type rather than for the text of
// the message.
func TestFramingFaultsAreAssertable(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		assert func(*testing.T, error)
	}{
		{
			name:   "an undefined-length dataset is its own fault",
			source: "(framing (recfm U))",
			assert: func(t *testing.T, err error) {
				var fault *UndefinedLengthError
				if !errors.As(err, &fault) {
					t.Fatalf("no UndefinedLengthError in %v", err)
				}
			},
		},
		{
			name:   "a carriage control carries what was written and what was meant",
			source: "(framing (recfm FBA) (lrecl 80))",
			assert: func(t *testing.T, err error) {
				var fault *CarriageControlError
				if !errors.As(err, &fault) {
					t.Fatalf("no CarriageControlError in %v", err)
				}

				if fault.Value != "FBA" || fault.RECFM != RECFMFB {
					t.Errorf("fault is %q under recfm %s, want \"FBA\" under FB", fault.Value, fault.RECFM)
				}
			},
		},
		{
			name:   "a blocked stream is its own fault",
			source: "(framing (recfm FB) (lrecl 80) (blocks in-stream))",
			assert: func(t *testing.T, err error) {
				var fault *BlockedStreamError
				if !errors.As(err, &fault) {
					t.Fatalf("no BlockedStreamError in %v", err)
				}
			},
		},
		{
			name:   "a required child names itself and the record format requiring it",
			source: "(framing (recfm VBS))",
			assert: func(t *testing.T, err error) {
				var fault *RequiredChildError
				if !errors.As(err, &fault) {
					t.Fatalf("no RequiredChildError in %v", err)
				}

				if fault.Child != "max-segment" || fault.RECFM != RECFMVBS {
					t.Errorf("fault is %q under recfm %s, want \"max-segment\" under VBS", fault.Child, fault.RECFM)
				}
			},
		},
		{
			name:   "an unadmitted child does the same",
			source: "(framing (recfm F) (lrecl 80) (blksize 800))",
			assert: func(t *testing.T, err error) {
				var fault *UnadmittedChildError
				if !errors.As(err, &fault) {
					t.Fatalf("no UnadmittedChildError in %v", err)
				}

				if fault.Child != "blksize" || fault.RECFM != RECFMF {
					t.Errorf("fault is %q under recfm %s, want \"blksize\" under F", fault.Child, fault.RECFM)
				}
			},
		},
		{
			name:   "a block size that is not a multiple carries both numbers",
			source: "(framing (recfm FB) (lrecl 80) (blksize 27998))",
			assert: func(t *testing.T, err error) {
				var fault *BlockSizeNotAMultipleError
				if !errors.As(err, &fault) {
					t.Fatalf("no BlockSizeNotAMultipleError in %v", err)
				}

				if fault.BlockSize != 27998 || fault.LRECL != 80 {
					t.Errorf("fault is blksize %d against lrecl %d, want 27998 against 80", fault.BlockSize, fault.LRECL)
				}
			},
		},
		{
			name:   "a block size with no room for a descriptor word does too",
			source: "(framing (recfm VBS) (lrecl 512) (blksize 512) (max-segment 512))",
			assert: func(t *testing.T, err error) {
				var fault *BlockSizeTooSmallError
				if !errors.As(err, &fault) {
					t.Fatalf("no BlockSizeTooSmallError in %v", err)
				}

				if fault.BlockSize != 512 || fault.LRECL != 512 {
					t.Errorf("fault is blksize %d against lrecl %d, want 512 against 512", fault.BlockSize, fault.LRECL)
				}
			},
		},
		{
			name:   "a layout with no framing",
			source: "(encoding (charset cp037))",
			assert: func(t *testing.T, err error) {
				var fault *FramingCountError
				if !errors.As(err, &fault) {
					t.Fatalf("no FramingCountError in %v", err)
				}

				if fault.Count != 0 {
					t.Errorf("count is %d, want 0", fault.Count)
				}
			},
		},
		{
			name:   "a delimiter that is not a byte string",
			source: "(framing (recfm line-sequential) (delimiter 10) (placement terminator))",
			assert: func(t *testing.T, err error) {
				var fault *ByteStringError
				if !errors.As(err, &fault) {
					t.Fatalf("no ByteStringError in %v", err)
				}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := framingOf(t, testCase.source)
			if err == nil {
				t.Fatal("read cleanly, want a fault")
			}

			testCase.assert(t, err)
		})
	}
}

// TestTheSpecsWorkedExampleFrames is the staleness gate over the layer.
//
// docs/layout/SPEC.md's "A layout, end to end" appendix is the layout the
// document shows an adopter, and it is read out of the document rather than
// copied here for [TestTheSpecsWorkedExampleReads]'s reason: a framing child the
// example writes and this reader does not read would otherwise be invisible
// until somebody pasted the example into a file.
//
// What it asserts is the appendix's own claim — an order file on a
// variable-length dataset, out of the JCL that allocated it.
func TestTheSpecsWorkedExampleFrames(t *testing.T) {
	t.Parallel()

	read, err := framingOf(t, specExample(t))
	if err != nil {
		t.Fatalf("the reader rejects SPEC.md's own worked example: %v", err)
	}

	if read.RECFM != RECFMVB || read.Kind() != DescriptorWord {
		t.Errorf("the framing reads as recfm %s resolving to %s, want VB resolving to descriptor-word", read.RECFM, read.Kind())
	}

	if read.LRECL.Value != 512 || read.BlockSize.Value != 27998 {
		t.Errorf("the dataset reads as lrecl %d blksize %d, want 512 and 27998", read.LRECL.Value, read.BlockSize.Value)
	}

	// The record descriptor word states each record's length, so the appendix's
	// lrecl is what the copybooks may not exceed rather than what they must be.
	if got := read.LRECLBound(); got != LRECLMaximum {
		t.Errorf("the lrecl binds as %s, want maximum", got)
	}

	if read.Blocks != Deblocked {
		t.Errorf("the stream reads as %q, want deblocked", read.Blocks)
	}
}
