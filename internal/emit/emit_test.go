// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package emit_test

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/emit"
	"github.com/Zaba505/cpybkc/irpb"
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

// TestWriteToAPathWritesTheEncodedDescriptor asserts the file holds the
// encoding and nothing else. A trailing newline, a text framing, or anything
// else added on the way past would be a file no protobuf runtime can decode.
func TestWriteToAPathWritesTheEncodedDescriptor(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "ir.binpb")

	if err := emit.Write(dest, io.Discard, descriptor()); err != nil {
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
		t.Fatalf("the file is not the encoded descriptor: %d bytes on disk, %d bytes encoded", len(got), len(want))
	}
}

// TestWriteToStdoutWritesTheSameBytes covers the operand that gets a descriptor
// out of a container without a bind mount. The stream and the file have to be
// the same bytes, or a caller redirecting the one would be reading something
// subtly other than what a plugin is handed.
func TestWriteToStdoutWritesTheSameBytes(t *testing.T) {
	var out bytes.Buffer

	if err := emit.Write(emit.Stdout, &out, descriptor()); err != nil {
		t.Fatalf("write to %q: %v", emit.Stdout, err)
	}

	dest := filepath.Join(t.TempDir(), "ir.binpb")
	if err := emit.Write(dest, io.Discard, descriptor()); err != nil {
		t.Fatalf("write to a path: %v", err)
	}

	want, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read %s: %v", dest, err)
	}

	if !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("the stream and the file disagree: %d bytes on the stream, %d in the file", out.Len(), len(want))
	}
}

// TestWriteToAPathLeavesTheStreamUntouched is the same requirement from the
// other side. A caller running `--emit-ir out.binpb` with its standard output
// going somewhere that matters must not find a descriptor in it as well.
func TestWriteToAPathLeavesTheStreamUntouched(t *testing.T) {
	var out bytes.Buffer

	dest := filepath.Join(t.TempDir(), "ir.binpb")
	if err := emit.Write(dest, &out, descriptor()); err != nil {
		t.Fatalf("write: %v", err)
	}

	if out.Len() != 0 {
		t.Fatalf("writing to %s also put %d bytes on the stream", dest, out.Len())
	}
}

// TestWriteIsReproducible is the acceptance criterion where a user meets it:
// two emissions of one descriptor are byte-identical files, so a descriptor
// committed beside a fixture stays a diff of the layout rather than of the run.
func TestWriteIsReproducible(t *testing.T) {
	dir := t.TempDir()

	first := filepath.Join(dir, "first.binpb")
	if err := emit.Write(first, io.Discard, descriptor()); err != nil {
		t.Fatalf("first write: %v", err)
	}

	second := filepath.Join(dir, "second.binpb")
	if err := emit.Write(second, io.Discard, descriptor()); err != nil {
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
}

// TestWriteReplacesAnExistingFile keeps re-emitting the ordinary case. A layout
// changes and the descriptor beside it is regenerated; refusing that, or
// appending to it, would make the common path the one that needs an argument.
func TestWriteReplacesAnExistingFile(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "ir.binpb")

	if err := os.WriteFile(dest, bytes.Repeat([]byte{0xff}, 4096), 0o644); err != nil {
		t.Fatalf("seed %s: %v", dest, err)
	}

	if err := emit.Write(dest, io.Discard, descriptor()); err != nil {
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
			err := emit.Write(testCase.dest, io.Discard, descriptor())
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
	if _, err := emit.Marshal(nil); err == nil {
		t.Error("encoding no descriptor succeeded")
	}

	dest := filepath.Join(t.TempDir(), "ir.binpb")

	seed := bytes.Repeat([]byte{0xff}, 16)
	if err := os.WriteFile(dest, seed, 0o644); err != nil {
		t.Fatalf("seed %s: %v", dest, err)
	}

	if err := emit.Write(dest, io.Discard, nil); err == nil {
		t.Fatalf("writing no descriptor to %s succeeded", dest)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read %s: %v", dest, err)
	}

	if !bytes.Equal(got, seed) {
		t.Errorf("the failed write replaced %s anyway: %d bytes where %d were", dest, len(got), len(seed))
	}
}

// TestWriteReportsAStreamItCannotWrite covers the redirection that fails —
// a full disk, a closed pipe — which is the failure mode the stdout operand
// adds and the one a caller redirecting into a file has to hear about.
func TestWriteReportsAStreamItCannotWrite(t *testing.T) {
	err := emit.Write(emit.Stdout, failingWriter{}, descriptor())
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
