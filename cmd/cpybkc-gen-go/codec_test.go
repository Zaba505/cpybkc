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
