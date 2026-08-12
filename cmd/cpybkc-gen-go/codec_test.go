// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/irpb"
)

// TestTheMethodsAreCodecsOwnInterfaces is why UnmarshalCOBOL and MarshalCOBOL
// are the names.
//
// codec declares Unmarshaler and Marshaler and hands each of them a Reader or a
// Writer that already knows the Encoding, so a generated record satisfying them
// is one codec.Unmarshal and codec.Marshal take — and #52's file-level reader
// and writer have a shape to call rather than one to invent.
func TestTheMethodsAreCodecsOwnInterfaces(t *testing.T) {
	t.Parallel()

	source := written(t, goldenDir)[codecFile]

	for _, want := range []string{
		"func (x *OrderRecord) UnmarshalCOBOL(r *codec.Reader) error {",
		"func (x *OrderRecord) MarshalCOBOL(w *codec.Writer) error {",
		"codec.Unmarshaler",
		"codec.Marshaler",
		"(*OrderRecord)(nil)",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("%s does not contain %q", codecFile, want)
		}
	}

	// Nothing in the hot path reflects: the one thing a walk over an anonymous
	// struct type would otherwise want reflection for is a make and a new, and
	// both are type inference here.
	if strings.Contains(source, "reflect.") {
		t.Errorf("%s reaches for reflection", codecFile)
	}
}

// TestEveryArgumentOfAnAccessorComesFromTheIR walks the calls the golden makes
// against the descriptor it came from.
//
// The widths, the digit counts, the sign position and the signedness are the
// resolved IR's, and this is what says so: each is spelled out here from the
// descriptor rather than derived, so a generator that started recomputing one
// from a PICTURE would have to change this file too.
func TestEveryArgumentOfAnAccessorComesFromTheIR(t *testing.T) {
	t.Parallel()

	source := written(t, goldenDir)[codecFile]

	for _, want := range []string{
		// Zoned: the digit count, and where the sign sits — never the width.
		"r.ReadZonedInt32(5, codec.SignUnsigned)",
		"w.WriteZonedInt32(x.OrderID, 5, codec.SignUnsigned)",

		// Packed and binary: the digit count, and whether the PICTURE carries
		// an S, which is the one thing the value being written cannot say.
		"r.ReadPackedInt32(7)",
		"w.WritePackedInt32(x.OrderTotal, 7, codec.Signed)",
		"r.ReadBinaryInt16(4)",
		"w.WriteBinaryInt16(x.LineItem[i0].Quantity, 4, codec.Signed)",

		// An item too wide for an int64 takes the big family on both sides.
		"r.ReadPackedBig(20)",

		// Alphanumeric and raw: the width in bytes, which is what those are.
		"r.ReadAlphanumeric(20)",
		"r.ReadBytes(4)",

		// Floating point takes neither a width nor a digit count: the usage
		// alone fixes the format.
		"r.ReadFloat32()",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("%s does not contain %q", codecFile, want)
		}
	}
}

// TestTheFourAxesAreTheDescriptorsAndNotADefault generates the same records
// under the other charset and checks that what changed is the Encoding and
// nothing else.
//
// None of the four has a default and every one of them fails silently when
// wrong, which is why codec has no usable zero-value Reader and why this
// function exists at all: a caller states all four in one call, out of the
// descriptor, rather than retyping them.
func TestTheFourAxesAreTheDescriptorsAndNotADefault(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		encoding *irpb.Encoding
		want     []string
	}{
		"the mainframe's": {
			encoding: resolvedEncoding(),
			want:     []string{"codec.CP037()", "codec.SignEBCDIC", "binary.BigEndian", "codec.FloatHFP"},
		},
		"a file converted to ASCII": {
			encoding: &irpb.Encoding{
				Charset:        irpb.Charset_CHARSET_ASCII,
				SignConvention: irpb.SignConvention_SIGN_CONVENTION_TRANSLATED_EBCDIC,
				ByteOrder:      irpb.ByteOrder_BYTE_ORDER_LITTLE_ENDIAN,
				FloatFormat:    irpb.FloatFormat_FLOAT_FORMAT_IEEE754,
			},
			want: []string{"codec.ASCII()", "codec.SignTranslatedEBCDIC", "binary.LittleEndian", "codec.FloatIEEE"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			item := alphanumeric(3, "CUSTOMER-NAME", 20)
			item.GetField().Encoding = tc.encoding

			d := &irpb.Descriptor{
				Version: supportedIRVersion,
				Nodes: []*irpb.Node{
					record(1, "ORDER-RECORD", 2),
					group(2, "ORDER-RECORD", nil, 3),
					item,
				},
			}

			out := t.TempDir()

			if err := generate(d, out, options{packageName: goldenPackage}); err != nil {
				t.Fatalf("generate: %v", err)
			}

			source := written(t, out)[codecFile]

			for _, want := range tc.want {
				if !strings.Contains(source, want) {
					t.Errorf("%s does not state the descriptor's %q", codecFile, want)
				}
			}
		})
	}
}

// TestACharsetCodecShipsNoTableForIsRefused is the posture the IR schema
// already takes for the same reason: a code page is refused on sight rather
// than substituted, because a substitute reads most of a file correctly.
func TestACharsetCodecShipsNoTableForIsRefused(t *testing.T) {
	t.Parallel()

	for _, charset := range []irpb.Charset{
		irpb.Charset_CHARSET_CP500,
		irpb.Charset_CHARSET_CP1047,
		irpb.Charset_CHARSET_CP1140,
	} {
		item := alphanumeric(3, "CUSTOMER-NAME", 20)
		item.GetField().GetEncoding().Charset = charset

		d := &irpb.Descriptor{
			Version: supportedIRVersion,
			Nodes: []*irpb.Node{
				record(1, "ORDER-RECORD", 2),
				group(2, "ORDER-RECORD", nil, 3),
				item,
			},
		}

		err := generate(d, t.TempDir(), options{packageName: goldenPackage})

		var refusal *unsupportedCharsetError
		if !errors.As(err, &refusal) {
			t.Fatalf("generate returned %v for %s, want a refusal", err, charsetName(charset))
		}

		if !strings.Contains(refusal.Error(), charsetName(charset)) {
			t.Errorf("the refusal reads %q and does not name the charset", refusal)
		}

		if len(refusal.Notes()) == 0 {
			t.Error("the refusal says nothing about what to do instead")
		}
	}
}

// TestItemsThatDisagreeAboutTheFileTheyAreInAreRefused is the other side of the
// same axis.
//
// The four are properties of the file rather than of an item, and codec carries
// them on the Reader and the Writer, so a descriptor whose items disagree
// describes a file there is no single Encoding to read.
func TestItemsThatDisagreeAboutTheFileTheyAreInAreRefused(t *testing.T) {
	t.Parallel()

	for name, second := range map[string]*irpb.Encoding{
		"the charset": {
			Charset:        irpb.Charset_CHARSET_ASCII,
			SignConvention: irpb.SignConvention_SIGN_CONVENTION_EBCDIC,
			ByteOrder:      irpb.ByteOrder_BYTE_ORDER_BIG_ENDIAN,
			FloatFormat:    irpb.FloatFormat_FLOAT_FORMAT_IBM_HFP,
		},
		"the byte order": {
			Charset:        irpb.Charset_CHARSET_CP037,
			SignConvention: irpb.SignConvention_SIGN_CONVENTION_EBCDIC,
			ByteOrder:      irpb.ByteOrder_BYTE_ORDER_LITTLE_ENDIAN,
			FloatFormat:    irpb.FloatFormat_FLOAT_FORMAT_IBM_HFP,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			item := alphanumeric(4, "SKU", 8)
			item.GetField().Encoding = second

			d := &irpb.Descriptor{
				Version: supportedIRVersion,
				Nodes: []*irpb.Node{
					record(1, "ORDER-RECORD", 2),
					group(2, "ORDER-RECORD", nil, 3, 4),
					alphanumeric(3, "CUSTOMER-NAME", 20),
					item,
				},
			}

			err := generate(d, t.TempDir(), options{packageName: goldenPackage})

			var refusal *mixedEncodingError
			if !errors.As(err, &refusal) {
				t.Fatalf("generate returned %v, want a refusal", err)
			}

			for _, want := range []string{"CUSTOMER-NAME", "SKU"} {
				if !strings.Contains(refusal.Error(), want) {
					t.Errorf("the refusal reads %q and does not name %s", refusal, want)
				}
			}
		})
	}
}

// TestATableCountedByARegisterReadsTheOccurrencesTheRecordAlreadyCarries is
// where this story stops and #52 begins.
//
// A register holds what a transition bound out of a record already read, and a
// record's own methods carry no register file — the automaton is the file-level
// reader's. So the table is read with as many occurrences as the record it was
// handed already has, which is what lets that reader size it from the register
// and then hand the record over. Nothing is refused and no count is invented.
func TestATableCountedByARegisterReadsTheOccurrencesTheRecordAlreadyCarries(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "DETAIL-RECORD", 2),
			group(2, "DETAIL-RECORD", nil, 3),
			group(3, "DETAIL", &irpb.Repetition{Count: &irpb.Repetition_Variable{Variable: &irpb.VariableCount{
				Count:          &irpb.VariableCount_RegisterId{RegisterId: 9},
				MinOccurrences: 1, MaxOccurrences: 8,
			}}}, 4),
			alphanumeric(4, "DETAIL-TEXT", 10),
			{Id: 9, Kind: &irpb.Node_Register{Register: &irpb.Register{Kind: irpb.RegisterKind_REGISTER_KIND_INTEGER}}},
		},
	}

	out := t.TempDir()

	if err := generate(d, out, options{packageName: goldenPackage}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	source := written(t, out)[codecFile]

	if !strings.Contains(source, "for i0 := range x.Detail {") {
		t.Errorf("%s does not read the occurrences the record carries\n%s", codecFile, source)
	}

	// Nothing sizes the slice, because nothing in this record says how many
	// there are.
	if strings.Contains(source, resizedHelper+"(") {
		t.Errorf("%s sized a table from a count no record carries\n%s", codecFile, source)
	}

	// The declared bounds still bind on the way out, because they are the
	// copybook's rather than the automaton's.
	if !strings.Contains(source, "occurs 1 to 8 times") {
		t.Errorf("%s does not check the bounds the repetition declares\n%s", codecFile, source)
	}
}

// TestARecordThatWouldBeCalledEncodingIsRefused covers the one identifier this
// generator declares at package scope that a copybook could also produce.
func TestARecordThatWouldBeCalledEncodingIsRefused(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "ENCODING", 2),
			group(2, "ENCODING", nil, 3),
			alphanumeric(3, "CUSTOMER-NAME", 20),
		},
	}

	err := generate(d, t.TempDir(), options{packageName: goldenPackage})

	var collision *collisionError
	if !errors.As(err, &collision) {
		t.Fatalf("generate returned %v, want a collision", err)
	}

	if collision.Go != encodingFunc {
		t.Errorf("the collision is about %s, want %s", collision.Go, encodingFunc)
	}
}

// TestASignedDisplayItemWithNoSignPositionIsRefused is the axis a zoned
// accessor takes that no other numeric accessor does.
//
// Position comes from the copybook and says which byte carries the sign and how
// wide the field is, so a signed DISPLAY item read as unsigned is not a wrong
// number but a wrong width: a SEPARATE sign taken for a digit shifts every
// later field of the record.
func TestASignedDisplayItemWithNoSignPositionIsRefused(t *testing.T) {
	t.Parallel()

	item := zoned(3, "ORDER-TOTAL", 7, 7, 2, true)
	item.GetField().GetPicture().SignPosition = irpb.SignPosition_SIGN_POSITION_UNSPECIFIED

	d := &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "ORDER-RECORD", 2),
			group(2, "ORDER-RECORD", nil, 3),
			item,
		},
	}

	err := generate(d, t.TempDir(), options{packageName: goldenPackage})

	var refusal *malformedError
	if !errors.As(err, &refusal) {
		t.Fatalf("generate returned %v, want a malformed descriptor", err)
	}

	if !strings.Contains(refusal.Error(), "ORDER-TOTAL") {
		t.Errorf("the refusal reads %q and does not name the item", refusal)
	}
}

// TestTheReceiverIsTheManifestsAndNotThisGeneratorsChoice covers the one option
// that changes the source without changing what it does.
func TestTheReceiverIsTheManifestsAndNotThisGeneratorsChoice(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	if err := generate(ordersDescriptor(), out, options{packageName: goldenPackage, receiver: "o"}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	source := written(t, out)[codecFile]

	if !strings.Contains(source, "func (o *OrderRecord) UnmarshalCOBOL(r *codec.Reader) error {") {
		t.Errorf("%s does not declare the receiver the manifest named", codecFile)
	}

	if strings.Contains(source, "func (x *OrderRecord)") {
		t.Errorf("%s declares the default receiver beside the one it was given", codecFile)
	}
}

// TestGeneratingTwiceWritesTheSameBytes is docs/plugin/SPEC.md's "Determinism"
// held over the file this story adds. Map iteration is the usual way to lose
// it, and there are three maps behind these methods.
func TestGeneratingTwiceWritesTheSameBytes(t *testing.T) {
	t.Parallel()

	first, second := t.TempDir(), t.TempDir()

	for _, out := range []string{first, second} {
		if err := generate(ordersDescriptor(), out, options{packageName: goldenPackage}); err != nil {
			t.Fatalf("generate: %v", err)
		}
	}

	one, two := written(t, first), written(t, second)

	for name, want := range one {
		if two[name] != want {
			t.Errorf("two runs over one descriptor wrote different %s", name)
		}
	}
}

// TestADescriptorCarryingNoRecordWritesNoMethods keeps the method file
// something a descriptor produced, like the record file beside it.
func TestADescriptorCarryingNoRecordWritesNoMethods(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	if err := generate(descriptorAt(supportedIRVersion), out, options{packageName: goldenPackage}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	if _, ok := written(t, out)[codecFile]; ok {
		t.Errorf("a descriptor carrying no record node wrote %s", codecFile)
	}
}

// TestAWidthIsSummedForAMalformedVariantWithoutPanicking is the same refusal
// from the other side.
//
// Every other path reaches a variant through the arms' fields, which are
// refused before a width is ever summed. This one does not have to: a consumer
// that wants a record's length evaluates no arm at all, so the sum is the one
// place a variant carrying fewer than two arms would be answered with a panic
// rather than with a diagnostic.
func TestAWidthIsSummedForAMalformedVariantWithoutPanicking(t *testing.T) {
	t.Parallel()

	c := &coder{emitter: &emitter{
		nodes: map[uint64]*irpb.Node{
			1: group(1, "ENTRY", constant(2), 2),
			2: variant(2),
		},
	}}

	if _, err := c.width(2, "x.Entry[i0]", decoding); err == nil {
		t.Error("a variant carrying no arm was given a width")
	}
}

// TestABinaryItemsSignSelectsTheAccessorAndNotJustItsArgument is the read side
// of the one axis the digit count cannot supply.
//
// A binary item stores two's complement, where the top bit is a digit in an
// unsigned item and the sign in a signed one. FF FF is 65535 read as an
// unsigned two-byte item and -1 read as a signed one, and codec's own
// documentation is explicit that the difference is not recoverable from the
// bytes — so which accessor is called is what says which the copybook declared.
// The pair below is the same PICTURE with and without an S, and the two rows
// name different accessors and different Go types on both sides of the codec.
func TestABinaryItemsSignSelectsTheAccessorAndNotJustItsArgument(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		item *irpb.Node
		want []string
	}{
		"a signed COMP item": {
			item: binary(3, "QUANTITY", 2, 4, true),
			want: []string{
				"Quantity int16",
				"r.ReadBinaryInt16(4)",
				"w.WriteBinaryInt16(x.Quantity, 4, codec.Signed)",
			},
		},
		"an unsigned COMP item": {
			item: binary(3, "QUANTITY", 2, 4, false),
			want: []string{
				"Quantity uint64",
				"r.ReadBinaryUint64(4)",
				"w.WriteBinaryUint64(x.Quantity, 4, codec.Unsigned)",
			},
		},
		"a signed COMP-5 item": {
			item: comp5(3, "QUANTITY", 2, 4, true),
			want: []string{
				"Quantity int16",
				"r.ReadComp5Int16(4)",
				"w.WriteComp5Int16(x.Quantity, 4, codec.Signed)",
			},
		},
		"an unsigned COMP-5 item": {
			item: comp5(3, "QUANTITY", 2, 4, false),
			want: []string{
				"Quantity uint64",
				"r.ReadComp5Uint64(4)",
				"w.WriteComp5Uint64(x.Quantity, 4, codec.Unsigned)",
			},
		},

		// Nine digits is four bytes and eighteen is eight, and an unsigned item
		// takes the same accessor at every one of those widths: codec ships no
		// narrower unsigned reader than a uint64 for a binary item.
		"an unsigned COMP item of nine digits": {
			item: binary(3, "QUANTITY", 4, 9, false),
			want: []string{
				"Quantity uint64",
				"r.ReadBinaryUint64(9)",
				"w.WriteBinaryUint64(x.Quantity, 9, codec.Unsigned)",
			},
		},
		"an unsigned COMP item of eighteen digits": {
			item: binary(3, "QUANTITY", 8, 18, false),
			want: []string{
				"Quantity uint64",
				"r.ReadBinaryUint64(18)",
				"w.WriteBinaryUint64(x.Quantity, 18, codec.Unsigned)",
			},
		},

		// Above eighteen there is no unsigned accessor at all: sixteen bytes is
		// the Big family's, signed or not.
		"an unsigned COMP item of nineteen digits": {
			item: binary(3, "QUANTITY", 16, 19, false),
			want: []string{
				"r.ReadBinaryBig(19)",
				"w.WriteBinaryBig(x.Quantity, 19, codec.Unsigned)",
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			d := &irpb.Descriptor{
				Version: supportedIRVersion,
				Nodes: []*irpb.Node{
					record(1, "ORDER-RECORD", 2),
					group(2, "ORDER-RECORD", nil, 3),
					tc.item,
				},
			}

			out := t.TempDir()

			if err := generate(d, out, options{packageName: goldenPackage}); err != nil {
				t.Fatalf("generate: %v", err)
			}

			files := written(t, out)
			source := files[recordsFile] + files[codecFile]

			for _, want := range tc.want {
				if !strings.Contains(source, want) {
					t.Errorf("the generated package does not contain %q\n%s", want, source)
				}
			}
		})
	}
}

// TestTheGeneratorReachesCodecsUnsignedBinaryAccessors is the claim the story
// this test came with was about, stated over a whole package rather than over
// one item: an unsigned binary item used to be read with a signed accessor, and
// the unsigned half of codec's binary surface was unreached.
func TestTheGeneratorReachesCodecsUnsignedBinaryAccessors(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "COUNTS", 2),
			group(2, "COUNTS", nil, 3, 4),
			binary(3, "COMP-COUNT", 2, 4, false),
			comp5(4, "COMP5-COUNT", 2, 4, false),
		},
	}

	out := t.TempDir()

	if err := generate(d, out, options{packageName: goldenPackage}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	source := written(t, out)[codecFile]

	for _, want := range []string{
		"ReadBinaryUint64", "WriteBinaryUint64",
		"ReadComp5Uint64", "WriteComp5Uint64",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("%s never calls %s\n%s", codecFile, want, source)
		}
	}

	// And none of the signed ones, because neither item carries an S. The
	// accessors are named in full rather than matched on the family alone: a
	// bare "Int64" is a substring of plenty of generated code that has nothing
	// to do with which accessor was chosen.
	for _, unwanted := range []string{
		"ReadBinaryInt16", "ReadBinaryInt32", "ReadBinaryInt64", "ReadBinaryBig",
		"ReadComp5Int16", "ReadComp5Int32", "ReadComp5Int64", "ReadComp5Big",
		"WriteBinaryInt16", "WriteBinaryInt32", "WriteBinaryInt64", "WriteBinaryBig",
		"WriteComp5Int16", "WriteComp5Int32", "WriteComp5Int64", "WriteComp5Big",
	} {
		if strings.Contains(source, unwanted) {
			t.Errorf("%s reads an unsigned item through a %s accessor\n%s", codecFile, unwanted, source)
		}
	}
}

// TestAnUnsignedBinaryRegisterSourceIsRangeCheckedRatherThanWidenedSilently
// pins the one place the unsigned family does not simply widen.
//
// A register holds an int64, and every signed binary item fits one. An unsigned
// one takes a uint64, and a COMP-5 item is bounded by its storage rather than
// by the decimal range its PICTURE declares — which is the whole reason this
// story reads it unsigned — so an unsigned PIC 9(18) COMP-5 is eight bytes and
// reads back anything up to 2^64 - 1. Converting that to int64 unchecked binds
// a negative number from a positive one, and every guard downstream then tests
// a value the file never held. The reader refuses it instead.
func TestAnUnsignedBinaryRegisterSourceIsRangeCheckedRatherThanWidenedSilently(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		source  *irpb.Node
		guarded bool
	}{
		"an unsigned COMP-5 register source": {source: comp5(102, "DTL-COUNT", 8, 18, false), guarded: true},
		"an unsigned COMP register source":   {source: binary(102, "DTL-COUNT", 8, 18, false), guarded: true},

		// The signed items of the same width widen for free, so a range check
		// on them would be a comparison no value can fail.
		"a signed COMP-5 register source": {source: comp5(102, "DTL-COUNT", 8, 18, true)},
		"a signed COMP register source":   {source: binary(102, "DTL-COUNT", 8, 18, true)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			d := &irpb.Descriptor{
				Version: supportedIRVersion,
				Nodes: []*irpb.Node{
					{Id: 1, Kind: &irpb.Node_File{File: &irpb.File{
						Framing:      &irpb.File_Unframed{Unframed: &irpb.Unframed{}},
						StartStateId: 2,
					}}},
					{Id: 2, Kind: &irpb.Node_State{State: &irpb.State{
						Accepts: true, TransitionIds: []uint64{10},
					}}},
					edge(10, 100, 2, nil, nil, []uint64{40}),
					counter(20),
					binds(40, 20, 102),

					record(100, "HEADER-RECORD", 105),
					group(105, "HEADER-RECORD", nil, 101, 102),
					alphanumeric(101, "TYPE-CODE", 1),
					tc.source,
				},
			}

			out := t.TempDir()

			if err := generate(d, out, options{packageName: goldenPackage}); err != nil {
				t.Fatalf("generate: %v", err)
			}

			source := written(t, out)[fileMachineFile]

			// Whichever it is, the register is still bound, and still bound
			// through an int64 conversion.
			if !strings.Contains(source, "r.register20 = int64(") {
				t.Fatalf("%s never binds the register\n%s", fileMachineFile, source)
			}

			// 9223372036854775807 is math.MaxInt64, written out because the
			// generated file reaches it as an untyped constant.
			check := "> 9223372036854775807 {"
			if got := strings.Contains(source, check); got != tc.guarded {
				t.Errorf("%s contains %q = %v, want %v\n%s", fileMachineFile, check, got, tc.guarded, source)
			}
		})
	}
}
