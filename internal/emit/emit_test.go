// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package emit_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/emit"
	"github.com/Zaba505/cpybkc/irpb"
	"google.golang.org/protobuf/proto"
)

// descriptor builds the fixture every test below emits.
//
// It is built fresh on each call rather than shared, because two of the claims
// under test — that equal descriptors encode equally, and that two runs agree —
// are only worth anything if the values compared were assembled twice. A
// package-level variable would let a cached encoding satisfy both.
//
// It carries a node of most kinds, with the references between them wired the
// way a resolved layout would wire them, so that the encoding being exercised is
// a whole message rather than a version field.
func descriptor() *irpb.Descriptor {
	return &irpb.Descriptor{
		Version: irpb.IrVersion_IR_VERSION_1,
		Nodes: []*irpb.Node{
			{
				Id: 1,
				Kind: &irpb.Node_File{File: &irpb.File{
					Framing:      &irpb.File_Unframed{Unframed: &irpb.Unframed{}},
					StartStateId: 2,
				}},
			},
			{
				Id: 2,
				Kind: &irpb.Node_State{State: &irpb.State{
					Accepts:       true,
					TransitionIds: []uint64{3},
				}},
			},
			{
				Id: 3,
				Kind: &irpb.Node_Transition{Transition: &irpb.Transition{
					RecordId:    4,
					NextStateId: 2,
					BindingIds:  []uint64{8},
				}},
			},
			{
				Id: 4,
				Kind: &irpb.Node_Record{Record: &irpb.Record{
					RootId: 5,
					Names:  &irpb.Names{Original: "DETAIL-RECORD"},
				}},
			},
			{
				Id: 5,
				Kind: &irpb.Node_Group{Group: &irpb.Group{
					MemberIds: []uint64{6, 9},
					Names:     &irpb.Names{Original: "DETAIL"},
				}},
			},
			{
				Id: 6,
				Kind: &irpb.Node_Field{Field: &irpb.Field{
					Width: 8,
					Encoding: &irpb.Encoding{
						Charset:        irpb.Charset_CHARSET_ASCII,
						SignConvention: irpb.SignConvention_SIGN_CONVENTION_EBCDIC,
						ByteOrder:      irpb.ByteOrder_BYTE_ORDER_BIG_ENDIAN,
						FloatFormat:    irpb.FloatFormat_FLOAT_FORMAT_IBM_HFP,
					},
					Usage: irpb.Usage_USAGE_DISPLAY,
					Picture: &irpb.Picture{
						Category:     irpb.Category_CATEGORY_NUMERIC,
						Digits:       8,
						Scale:        -2,
						Signed:       true,
						SignPosition: irpb.SignPosition_SIGN_POSITION_TRAILING,
					},
					Names: &irpb.Names{Original: "DTL-AMOUNT"},
				}},
			},
			{
				Id:   7,
				Kind: &irpb.Node_Register{Register: &irpb.Register{Kind: irpb.RegisterKind_REGISTER_KIND_INTEGER}},
			},
			{
				Id: 8,
				Kind: &irpb.Node_Binding{Binding: &irpb.Binding{
					RegisterId: 7,
					Value:      &irpb.Binding_Decrement{Decrement: &irpb.Decrement{}},
				}},
			},
			{
				Id:   9,
				Kind: &irpb.Node_Slack{Slack: &irpb.Slack{Width: 2}},
			},
		},
	}
}

// TestMarshalIsDeterministic is the acceptance criterion at the encoder: equal
// descriptors produce equal bytes, every time.
//
// Both halves matter. Encoding one value repeatedly catches an encoder that
// varies between calls; encoding a value assembled a second time catches one
// that depends on how the message was built — a cached size, an interned
// string, the iteration order of anything a future schema adds — which is the
// case a consumer diffing two emissions of an unchanged layout actually hits.
func TestMarshalIsDeterministic(t *testing.T) {
	want, err := emit.Marshal(descriptor())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if len(want) == 0 {
		t.Fatal("encoding the fixture produced no bytes, so nothing below is comparing anything")
	}

	d := descriptor()
	for i := range 64 {
		got, err := emit.Marshal(d)
		if err != nil {
			t.Fatalf("marshal, call %d: %v", i, err)
		}

		if !bytes.Equal(got, want) {
			t.Fatalf("call %d encoded the same descriptor to different bytes: %d then %d", i, len(want), len(got))
		}
	}

	for i := range 64 {
		got, err := emit.Marshal(descriptor())
		if err != nil {
			t.Fatalf("marshal a rebuilt descriptor, call %d: %v", i, err)
		}

		if !bytes.Equal(got, want) {
			t.Fatalf("descriptor %d, rebuilt from the same values, encoded differently: %d bytes against %d", i, len(got), len(want))
		}
	}
}

// TestMarshalDistinguishesDescriptorsThatDiffer is the other direction, without
// which every byte comparison above would be satisfied by an encoder that
// returned a constant.
func TestMarshalDistinguishesDescriptorsThatDiffer(t *testing.T) {
	base, err := emit.Marshal(descriptor())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	changed := descriptor()
	changed.Nodes[5].GetField().Width = 9

	got, err := emit.Marshal(changed)
	if err != nil {
		t.Fatalf("marshal the changed descriptor: %v", err)
	}

	if bytes.Equal(got, base) {
		t.Fatal("a descriptor whose field width changed encoded to the same bytes, so the encoding is not carrying the change")
	}
}

// goldenJSON is the rendering of [descriptor], written out in full.
//
// Every part of it is something a reader depends on and a change to it is a
// change to what somebody is reading, which is what makes a golden the right
// shape here rather than a set of assertions about it:
//
//   - protobuf's own field names — start_state_id, not startStateId — so a name
//     on screen is the name in proto/cpybkc/ir/v1/ir.proto and in
//     docs/ir/SPEC.md.
//   - enums by name rather than by number, so CHARSET_ASCII reads as itself.
//   - identifiers as quoted strings, which is protojson's rendering of a 64-bit
//     integer and surprising enough to be worth pinning: a consumer of the
//     rendering that expected 1 rather than "1" learns it from here.
//   - two-space indentation, protobuf's field-number ordering, and one final
//     newline.
const goldenJSON = `{
  "version": "IR_VERSION_1",
  "nodes": [
    {
      "id": "1",
      "file": {
        "unframed": {},
        "start_state_id": "2"
      }
    },
    {
      "id": "2",
      "state": {
        "accepts": true,
        "transition_ids": [
          "3"
        ]
      }
    },
    {
      "id": "3",
      "transition": {
        "record_id": "4",
        "next_state_id": "2",
        "binding_ids": [
          "8"
        ]
      }
    },
    {
      "id": "4",
      "record": {
        "root_id": "5",
        "names": {
          "original": "DETAIL-RECORD"
        }
      }
    },
    {
      "id": "5",
      "group": {
        "member_ids": [
          "6",
          "9"
        ],
        "names": {
          "original": "DETAIL"
        }
      }
    },
    {
      "id": "6",
      "field": {
        "width": 8,
        "encoding": {
          "charset": "CHARSET_ASCII",
          "sign_convention": "SIGN_CONVENTION_EBCDIC",
          "byte_order": "BYTE_ORDER_BIG_ENDIAN",
          "float_format": "FLOAT_FORMAT_IBM_HFP"
        },
        "usage": "USAGE_DISPLAY",
        "picture": {
          "category": "CATEGORY_NUMERIC",
          "digits": 8,
          "scale": -2,
          "signed": true,
          "sign_position": "SIGN_POSITION_TRAILING"
        },
        "names": {
          "original": "DTL-AMOUNT"
        }
      }
    },
    {
      "id": "7",
      "register": {
        "kind": "REGISTER_KIND_INTEGER"
      }
    },
    {
      "id": "8",
      "binding": {
        "register_id": "7",
        "decrement": {}
      }
    },
    {
      "id": "9",
      "slack": {
        "width": 2
      }
    }
  ]
}
`

// TestMarshalJSONRendersTheGolden is the golden test the JSON form is worth
// having: the rendering of a known descriptor, byte for byte.
//
// It fails on a change to the field names, to the indentation, to the ordering
// and to protojson's own spacing, which is the point — every one of those is a
// change to a file people commit and diff, and none of them would fail a test
// that merely checked the output parsed.
func TestMarshalJSONRendersTheGolden(t *testing.T) {
	got, err := emit.MarshalJSON(descriptor())
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if string(got) != goldenJSON {
		t.Errorf("the rendering is not the golden\n got:\n%s\nwant:\n%s", got, goldenJSON)
	}

	if !json.Valid(got) {
		t.Errorf("the rendering is not valid JSON:\n%s", got)
	}
}

// TestMarshalJSONIsStableAcrossRuns is the acceptance criterion at the
// renderer, and it is the same pair of loops [TestMarshalIsDeterministic] runs
// for the binary form: rendering one value repeatedly catches a renderer that
// varies between calls, and rendering a value assembled again catches one that
// depends on how the message was built.
//
// What neither loop can catch is the whitespace protojson chooses per process,
// because both run inside one process. That property is asserted directly, on
// the function that supplies it, in normalize_test.go.
func TestMarshalJSONIsStableAcrossRuns(t *testing.T) {
	want, err := emit.MarshalJSON(descriptor())
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if len(want) == 0 {
		t.Fatal("rendering the fixture produced no bytes, so nothing below is comparing anything")
	}

	d := descriptor()
	for i := range 64 {
		got, err := emit.MarshalJSON(d)
		if err != nil {
			t.Fatalf("render, call %d: %v", i, err)
		}

		if !bytes.Equal(got, want) {
			t.Fatalf("call %d rendered the same descriptor differently:\n got:\n%s\nwant:\n%s", i, got, want)
		}
	}

	for i := range 64 {
		got, err := emit.MarshalJSON(descriptor())
		if err != nil {
			t.Fatalf("render a rebuilt descriptor, call %d: %v", i, err)
		}

		if !bytes.Equal(got, want) {
			t.Fatalf("descriptor %d, rebuilt from the same values, rendered differently:\n got:\n%s\nwant:\n%s", i, got, want)
		}
	}
}

// TestMarshalJSONDistinguishesDescriptorsThatDiffer is the other direction,
// without which every comparison above would be satisfied by a renderer that
// returned a constant.
func TestMarshalJSONDistinguishesDescriptorsThatDiffer(t *testing.T) {
	base, err := emit.MarshalJSON(descriptor())
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	changed := descriptor()
	changed.Nodes[5].GetField().Width = 9

	got, err := emit.MarshalJSON(changed)
	if err != nil {
		t.Fatalf("render the changed descriptor: %v", err)
	}

	if bytes.Equal(got, base) {
		t.Fatal("a descriptor whose field width changed rendered identically, so the rendering is not carrying the change")
	}
}

// TestTheTwoFormsAreNotInterchangeable states the thing the package
// documentation argues: JSON is a rendering, not a second encoding a consumer
// may be handed instead. A consumer that read one where it expected the other
// has to fail rather than half-succeed, and these bytes cannot be confused.
func TestTheTwoFormsAreNotInterchangeable(t *testing.T) {
	binary, err := emit.Marshal(descriptor())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	rendered, err := emit.MarshalJSON(descriptor())
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if bytes.Equal(binary, rendered) {
		t.Fatal("the two forms produced the same bytes")
	}

	if json.Valid(binary) {
		t.Error("the binary encoding parses as JSON, so a consumer could not tell which form it was handed")
	}

	if err := proto.Unmarshal(rendered, &irpb.Descriptor{}); err == nil {
		t.Error("the rendering decodes as a descriptor, so a consumer could not tell which form it was handed")
	}
}

// TestTheFormatFlagBindsToAFormat drives the format the way the command will:
// one flag, bound with flag.Var so that the parse is where a misspelling is
// caught.
//
// It is here for the reason [TestTheFlagBindsToADestination] is — the flag name
// and the two spellings are a contract with a command that does not exist yet
// (#42) — plus one this flag adds: the default is the canonical form, and a
// caller who says nothing must not silently receive the debug one.
func TestTheFormatFlagBindsToAFormat(t *testing.T) {
	testCases := []struct {
		name    string
		args    []string
		want    emit.Format
		wantErr bool
	}{
		{
			name: "not requested",
			args: nil,
			want: emit.FormatBinary,
		},
		{
			name: "binary, asked for by name",
			args: []string{"--" + emit.FormatFlag, emit.FormatBinary.String()},
			want: emit.FormatBinary,
		},
		{
			name: "json",
			args: []string{"--" + emit.FormatFlag + "=" + emit.FormatJSON.String()},
			want: emit.FormatJSON,
		},
		{
			name:    "a format this package does not produce",
			args:    []string{"--" + emit.FormatFlag, "yaml"},
			want:    emit.FormatBinary,
			wantErr: true,
		},
		{
			name:    "an empty format",
			args:    []string{"--" + emit.FormatFlag, ""},
			want:    emit.FormatBinary,
			wantErr: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			flags := flag.NewFlagSet("cpybkc", flag.ContinueOnError)
			flags.SetOutput(io.Discard)

			got := emit.FormatBinary
			flags.Var(&got, emit.FormatFlag, "write the resolved IR in this format")

			err := flags.Parse(testCase.args)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("parsing %v succeeded, so an unknown format reaches the emission", testCase.args)
				}
			} else if err != nil {
				t.Fatalf("parse %v: %v", testCase.args, err)
			}

			if got != testCase.want {
				t.Fatalf("--%s parsed to %q, want %q", emit.FormatFlag, got, testCase.want)
			}
		})
	}
}

// TestWriteRejectsAnUnknownFormat covers the format that did not come through
// the flag. Falling back to the binary form would hand a caller bytes in an
// encoding it did not ask for, and nothing downstream would say so.
func TestWriteRejectsAnUnknownFormat(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "ir")

	err := emit.Write(dest, io.Discard, descriptor(), emit.Format("yaml"))
	if err == nil {
		t.Fatal("writing in an unknown format succeeded")
	}

	if !strings.Contains(err.Error(), emit.FormatFlag) {
		t.Errorf("the error does not name --%s, so it does not say what to fix: %v", emit.FormatFlag, err)
	}

	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("the rejected write created %s anyway", dest)
	}
}

// formats pairs every [emit.Format] with the function that produces it, so that
// a test claiming something about "the emission" runs over both rather than
// over whichever one was written first.
//
// It is the guard on adding a third: a format that reaches [emit.Write] and not
// this map is a format nothing below checks ever gets to disk.
func formats() map[emit.Format]func(*irpb.Descriptor) ([]byte, error) {
	return map[emit.Format]func(*irpb.Descriptor) ([]byte, error){
		emit.FormatBinary: emit.Marshal,
		emit.FormatJSON:   emit.MarshalJSON,
	}
}

// TestWriteToAPathWritesTheEncodedDescriptor asserts the file holds what the
// format encoded and nothing else. Anything added on the way past — a framing,
// a second newline — is a file whose reader disagrees with this package about
// where the descriptor ends.
func TestWriteToAPathWritesTheEncodedDescriptor(t *testing.T) {
	for format, marshal := range formats() {
		t.Run(format.String(), func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "ir")

			if err := emit.Write(dest, io.Discard, descriptor(), format); err != nil {
				t.Fatalf("write: %v", err)
			}

			got, err := os.ReadFile(dest)
			if err != nil {
				t.Fatalf("read %s: %v", dest, err)
			}

			want, err := marshal(descriptor())
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			if !bytes.Equal(got, want) {
				t.Fatalf("the file is not the encoded descriptor: %d bytes on disk, %d bytes encoded", len(got), len(want))
			}
		})
	}
}

// TestWriteToStdoutWritesTheSameBytes covers the operand that gets a descriptor
// out of a container without a bind mount. The stream and the file have to be
// the same bytes, or a caller redirecting the one would be reading something
// subtly other than what a plugin is handed.
func TestWriteToStdoutWritesTheSameBytes(t *testing.T) {
	for format := range formats() {
		t.Run(format.String(), func(t *testing.T) {
			var out bytes.Buffer

			if err := emit.Write(emit.Stdout, &out, descriptor(), format); err != nil {
				t.Fatalf("write to %q: %v", emit.Stdout, err)
			}

			dest := filepath.Join(t.TempDir(), "ir")
			if err := emit.Write(dest, io.Discard, descriptor(), format); err != nil {
				t.Fatalf("write to a path: %v", err)
			}

			want, err := os.ReadFile(dest)
			if err != nil {
				t.Fatalf("read %s: %v", dest, err)
			}

			if !bytes.Equal(out.Bytes(), want) {
				t.Fatalf("the stream and the file disagree: %d bytes on the stream, %d in the file", out.Len(), len(want))
			}
		})
	}
}

// TestWriteToAPathLeavesTheStreamUntouched is the same requirement from the
// other side. A caller running `--emit-ir out.binpb` with its standard output
// going somewhere that matters must not find a descriptor in it as well.
func TestWriteToAPathLeavesTheStreamUntouched(t *testing.T) {
	for format := range formats() {
		t.Run(format.String(), func(t *testing.T) {
			var out bytes.Buffer

			dest := filepath.Join(t.TempDir(), "ir")
			if err := emit.Write(dest, &out, descriptor(), format); err != nil {
				t.Fatalf("write: %v", err)
			}

			if out.Len() != 0 {
				t.Fatalf("writing to %s also put %d bytes on the stream", dest, out.Len())
			}
		})
	}
}

// TestWriteIsReproducible is the acceptance criterion where a user meets it:
// two emissions of one descriptor are byte-identical files, so a descriptor
// committed beside a fixture stays a diff of the layout rather than of the run.
func TestWriteIsReproducible(t *testing.T) {
	for format := range formats() {
		t.Run(format.String(), func(t *testing.T) {
			dir := t.TempDir()

			first := filepath.Join(dir, "first")
			if err := emit.Write(first, io.Discard, descriptor(), format); err != nil {
				t.Fatalf("first write: %v", err)
			}

			second := filepath.Join(dir, "second")
			if err := emit.Write(second, io.Discard, descriptor(), format); err != nil {
				t.Fatalf("second write: %v", err)
			}

			a, err := os.ReadFile(first)
			if err != nil {
				t.Fatalf("read %s: %v", first, err)
			}

			b, err := os.ReadFile(second)
			if err != nil {
				t.Fatalf("read %s: %v", second, err)
			}

			if len(a) == 0 {
				t.Fatal("the first emission is empty")
			}

			if !bytes.Equal(a, b) {
				t.Fatalf("two emissions of one descriptor differ: %d bytes then %d", len(a), len(b))
			}
		})
	}
}

// TestWriteReplacesAnExistingFile keeps re-emitting the ordinary case. A layout
// changes and the descriptor beside it is regenerated; refusing that, or
// appending to it, would make the common path the one that needs an argument.
func TestWriteReplacesAnExistingFile(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "ir.binpb")

	if err := os.WriteFile(dest, bytes.Repeat([]byte{0xff}, 4096), 0o644); err != nil {
		t.Fatalf("seed %s: %v", dest, err)
	}

	if err := emit.Write(dest, io.Discard, descriptor(), emit.FormatBinary); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read %s: %v", dest, err)
	}

	want, err := emit.Marshal(descriptor())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("the replaced file is not the encoded descriptor: %d bytes on disk, %d encoded", len(got), len(want))
	}
}

// TestWriteReplacesAPathRatherThanTruncatingIt is how atomicity is observed
// from outside the package: a reader holding the old file open still sees the
// old bytes after the write, which is true of a rename onto the name and false
// of a truncate-and-write.
//
// It is the property docs/cli/SPEC.md asks for — "a path is written in full or
// not at all, so that nothing partial is ever left where another tool would read
// it" — stated as something a test can fail on. A write that truncated first
// would leave a descriptor that is a prefix of a descriptor for as long as the
// write took, and a consumer reading in that window meets a malformed message
// rather than the failed emission it is.
func TestWriteReplacesAPathRatherThanTruncatingIt(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "ir.binpb")

	seed := bytes.Repeat([]byte{0xff}, 4096)
	if err := os.WriteFile(dest, seed, 0o644); err != nil {
		t.Fatalf("seed %s: %v", dest, err)
	}

	before, err := os.Open(dest)
	if err != nil {
		t.Fatalf("open %s: %v", dest, err)
	}

	defer before.Close()

	if err := emit.Write(dest, io.Discard, descriptor(), emit.FormatBinary); err != nil {
		t.Fatalf("write: %v", err)
	}

	held, err := io.ReadAll(before)
	if err != nil {
		t.Fatalf("read the file that was open across the write: %v", err)
	}

	if !bytes.Equal(held, seed) {
		t.Errorf("the write replaced the contents under a reader that had the file open: %d bytes of %d survived",
			len(held), len(seed))
	}
}

// TestWriteLeavesNothingBesideTheDescriptor holds the other half of the same
// implementation: whatever a write puts beside the destination on the way is
// gone afterwards, whether it succeeded or failed.
//
// A temporary left behind is not a cosmetic fault. The destination is a path
// somebody typed, usually beside the layout it describes, and a run that
// littered a checked-out tree with half-written descriptors would have every one
// of them turn up in a diff.
func TestWriteLeavesNothingBesideTheDescriptor(t *testing.T) {
	t.Run("after a write that succeeded", func(t *testing.T) {
		dir := t.TempDir()
		dest := filepath.Join(dir, "ir.binpb")

		if err := emit.Write(dest, io.Discard, descriptor(), emit.FormatBinary); err != nil {
			t.Fatalf("write: %v", err)
		}

		if got := entries(t, dir); len(got) != 1 || got[0] != "ir.binpb" {
			t.Errorf("the directory holds %v, want the descriptor alone", got)
		}
	})

	t.Run("after a write that failed", func(t *testing.T) {
		dir := t.TempDir()

		// A destination that is an existing directory. The encoding succeeds and
		// so does everything up to the rename, which is the one failure that
		// reaches this function with a temporary already on disk.
		dest := filepath.Join(dir, "ir.binpb")
		if err := os.Mkdir(dest, 0o755); err != nil {
			t.Fatalf("make %s: %v", dest, err)
		}

		if err := emit.Write(dest, io.Discard, descriptor(), emit.FormatBinary); err == nil {
			t.Fatal("writing over a directory succeeded")
		}

		if got := entries(t, dir); len(got) != 1 || got[0] != "ir.binpb" {
			t.Errorf("a failed write left %v behind, want the directory it could not replace alone", got)
		}
	})
}

// TestWriteGivesTheDescriptorAnOrdinaryMode pins the mode a written descriptor
// carries.
//
// The temporary this package writes through is created 0600, and a descriptor
// inheriting that mode is one the next step of a pipeline — running as another
// user, which is the ordinary arrangement inside a container — cannot read. The
// mode is therefore set rather than inherited, and this is what says so.
func TestWriteGivesTheDescriptorAnOrdinaryMode(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "ir.binpb")

	if err := emit.Write(dest, io.Discard, descriptor(), emit.FormatBinary); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat %s: %v", dest, err)
	}

	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("the descriptor is mode %04o, want %04o", got, 0o644)
	}
}

// entries is the names in dir, sorted, for a message naming what is there.
func entries(t *testing.T, dir string) []string {
	t.Helper()

	found, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	names := make([]string, 0, len(found))
	for _, entry := range found {
		names = append(names, entry.Name())
	}

	sort.Strings(names)

	return names
}

// TestWriteRejectsBadDestinations keeps the failure modes failures. Silently
// writing nowhere is the one outcome a caller would not notice, and it is
// exactly what an unset flag looks like to this function.
func TestWriteRejectsBadDestinations(t *testing.T) {
	dir := t.TempDir()

	notADir := filepath.Join(dir, "file")
	if err := os.WriteFile(notADir, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("seed %s: %v", notADir, err)
	}

	testCases := []struct {
		name string
		dest string
		want string
	}{
		{
			name: "unset",
			dest: "",
			want: emit.Flag,
		},
		{
			name: "a directory that does not exist",
			dest: filepath.Join(dir, "nope", "ir.binpb"),
			want: "ir.binpb",
		},
		{
			name: "a parent that is not a directory",
			dest: filepath.Join(notADir, "ir.binpb"),
			want: "ir.binpb",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := emit.Write(testCase.dest, io.Discard, descriptor(), emit.FormatBinary)
			if err == nil {
				t.Fatalf("writing to %q succeeded", testCase.dest)
			}

			if !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("the error does not name %q, so it does not say what to fix: %v", testCase.want, err)
			}
		})
	}
}

// TestNoDescriptorIsAnError covers the one input protobuf would accept and
// encode to nothing at all. An emission that produced a zero-length file and
// reported success would send whoever ran it looking at their consumer, and the
// existing file it replaced on the way would already be gone — so the second
// half asserts that dest is untouched as well.
func TestNoDescriptorIsAnError(t *testing.T) {
	for format, marshal := range formats() {
		t.Run(format.String(), func(t *testing.T) {
			if _, err := marshal(nil); err == nil {
				t.Error("encoding no descriptor succeeded")
			}

			dest := filepath.Join(t.TempDir(), "ir")

			seed := bytes.Repeat([]byte{0xff}, 16)
			if err := os.WriteFile(dest, seed, 0o644); err != nil {
				t.Fatalf("seed %s: %v", dest, err)
			}

			if err := emit.Write(dest, io.Discard, nil, format); err == nil {
				t.Fatalf("writing no descriptor to %s succeeded", dest)
			}

			got, err := os.ReadFile(dest)
			if err != nil {
				t.Fatalf("read %s: %v", dest, err)
			}

			if !bytes.Equal(got, seed) {
				t.Errorf("the failed write replaced %s anyway: %d bytes where %d were", dest, len(got), len(seed))
			}
		})
	}
}

// TestWriteReportsAStreamItCannotWrite covers the redirection that fails —
// a full disk, a closed pipe — which is the failure mode the stdout operand
// adds and the one a caller redirecting into a file has to hear about.
func TestWriteReportsAStreamItCannotWrite(t *testing.T) {
	err := emit.Write(emit.Stdout, failingWriter{}, descriptor(), emit.FormatBinary)
	if err == nil {
		t.Fatal("writing to a stream that fails succeeded")
	}

	if !strings.Contains(err.Error(), "standard output") {
		t.Errorf("the error does not say where the write failed: %v", err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

// TestTheFlagBindsToADestination drives the operand the way the command will:
// one flag, whose value is handed to [emit.Write] unchanged.
//
// It is here because [emit.Flag] and [emit.Stdout] are a contract with a
// command that does not exist yet — the CLI's first command lands with the
// argument vector (#42) — and a constant nothing parses is a name that can be
// wrong without anything saying so.
func TestTheFlagBindsToADestination(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "ir.binpb")

	testCases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "a path",
			args: []string{"--" + emit.Flag, dest},
			want: dest,
		},
		{
			name: "standard output",
			args: []string{"--" + emit.Flag, emit.Stdout},
			want: emit.Stdout,
		},
		{
			name: "not requested",
			args: nil,
			want: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			flags := flag.NewFlagSet("cpybkc", flag.ContinueOnError)
			flags.SetOutput(io.Discard)

			got := flags.String(emit.Flag, "", "write the resolved IR to this path, or "+emit.Stdout+" for standard output")

			if err := flags.Parse(testCase.args); err != nil {
				t.Fatalf("parse %v: %v", testCase.args, err)
			}

			if *got != testCase.want {
				t.Fatalf("--%s parsed to %q, want %q", emit.Flag, *got, testCase.want)
			}
		})
	}
}
