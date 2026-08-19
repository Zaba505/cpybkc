// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"io"
	"regexp"
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

// TestTheFiveAxesAreTheDescriptorsAndNotADefault generates the same records
// under the other charset and checks that what changed is the Encoding and
// nothing else.
//
// None of the five has a default and every one of them fails silently when
// wrong, which is why codec has no usable zero-value Reader and why this
// function exists at all: a caller states all five in one call, out of the
// descriptor, rather than retyping them.
func TestTheFiveAxesAreTheDescriptorsAndNotADefault(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		encoding *irpb.Encoding
		want     []string
	}{
		"the mainframe's": {
			encoding: resolvedEncoding(),
			want:     []string{"codec.CP037()", "codec.SignEBCDIC", "binary.BigEndian", "codec.FloatHFP", "codec.BinarySize248"},
		},
		"a file converted to ASCII": {
			encoding: &irpb.Encoding{
				Charset:        irpb.Charset_CHARSET_ASCII,
				SignConvention: irpb.SignConvention_SIGN_CONVENTION_TRANSLATED_EBCDIC,
				ByteOrder:      irpb.ByteOrder_BYTE_ORDER_LITTLE_ENDIAN,
				FloatFormat:    irpb.FloatFormat_FLOAT_FORMAT_IEEE754,
				BinarySize:     irpb.BinarySize_BINARY_SIZE_1248,
			},
			want: []string{"codec.ASCII()", "codec.SignTranslatedEBCDIC", "binary.LittleEndian", "codec.FloatIEEE", "codec.BinarySize1248"},
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

			if err := generate(io.Discard, d, out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
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

		err := generate(io.Discard, d, t.TempDir(), options{packageName: goldenPackage, importPath: goldenImport})

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
			BinarySize:     irpb.BinarySize_BINARY_SIZE_248,
		},
		"the byte order": {
			Charset:        irpb.Charset_CHARSET_CP037,
			SignConvention: irpb.SignConvention_SIGN_CONVENTION_EBCDIC,
			ByteOrder:      irpb.ByteOrder_BYTE_ORDER_LITTLE_ENDIAN,
			FloatFormat:    irpb.FloatFormat_FLOAT_FORMAT_IBM_HFP,
			BinarySize:     irpb.BinarySize_BINARY_SIZE_248,
		},
		"the binary width staircase": {
			Charset:        irpb.Charset_CHARSET_CP037,
			SignConvention: irpb.SignConvention_SIGN_CONVENTION_EBCDIC,
			ByteOrder:      irpb.ByteOrder_BYTE_ORDER_BIG_ENDIAN,
			FloatFormat:    irpb.FloatFormat_FLOAT_FORMAT_IBM_HFP,
			BinarySize:     irpb.BinarySize_BINARY_SIZE_1248,
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

			err := generate(io.Discard, d, t.TempDir(), options{packageName: goldenPackage, importPath: goldenImport})

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

// TestAnItemNoCharsetGovernsIsBytesOnBothSides is the accessor pair a PIC X
// item carrying a payload takes, held against the source that calls them.
//
// ReadAlphanumeric and WriteAlphanumeric are what every other PIC X item takes
// and what this one may not: the first trims the charset's trailing spaces off
// a value whose 0x20 is a payload byte, and the second translates through a
// code page a payload was never in. ReadBytes and WriteBytes do neither.
func TestAnItemNoCharsetGovernsIsBytesOnBothSides(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "ORDER-RECORD", 2),
			group(2, "ORDER-RECORD", nil, 3, 4),
			alphanumeric(3, "CUSTOMER-NAME", 20),
			opaque(4, "STATUS-FLAG", 4),
		},
	}

	out := t.TempDir()

	if err := generate(io.Discard, d, out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	files := written(t, out)

	// The field the two accessors are on either side of.
	if !strings.Contains(files[recordsFile], "StatusFlag []byte") {
		t.Errorf("%s does not declare STATUS-FLAG as a []byte:\n%s", recordsFile, files[recordsFile])
	}

	source := files[codecFile]

	for _, want := range []string{
		// The read: the bytes as they stand, and the item's own width.
		"x.StatusFlag, err = r.ReadBytes(4)",

		// The write, on the three terms WriteBytes leaves to the caller
		// because it takes no width of its own.
		"case x.StatusFlag == nil:",
		"w.WriteBytes(zeroFill[:4])",
		"case len(x.StatusFlag) != 4:",
		"rather than truncating or padding them",
		"w.WriteBytes(x.StatusFlag)",

		// The run the nil arm slices out of has to be declared wide enough
		// for the item, which is the same mechanism a slack node sizes.
		"var zeroFill = make([]byte, 4)",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("%s does not carry %q:\n%s", codecFile, want, source)
		}
	}

	// The two accessors this item may not reach, named so that a regression to
	// them fails here rather than in a corpus entry.
	for _, refused := range []string{"ReadAlphanumeric(4)", "WriteAlphanumeric(x.StatusFlag"} {
		if strings.Contains(source, refused) {
			t.Errorf("%s reaches %s for an item no charset governs", codecFile, refused)
		}
	}
}

// TestAnItemNoCharsetGovernsIsNotAPartyToTheCharsetAgreement is the byte item
// beside a text item, which is the ordinary shape rather than an exotic one.
//
// A descriptor whose items disagree on an axis is refused, because codec
// carries one Encoding per Reader. An item stating that its bytes become
// characters under no code page makes no claim about the file's charset to
// disagree with, so it takes no part in that comparison — and the alternative,
// comparing it, would refuse the very case this charset was added for: a status
// flag sitting in a cp037 record.
func TestAnItemNoCharsetGovernsIsNotAPartyToTheCharsetAgreement(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "ORDER-RECORD", 2),
			group(2, "ORDER-RECORD", nil, 3, 4),
			opaque(3, "STATUS-FLAG", 4),
			alphanumeric(4, "CUSTOMER-NAME", 20),
		},
	}

	out := t.TempDir()

	if err := generate(io.Discard, d, out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	source := written(t, out)[codecFile]

	// The opaque item is first in the record, so a walk that read an encoding
	// off it would have taken none at all and then refused the text item for
	// disagreeing with it.
	if !strings.Contains(source, "codec.CP037()") {
		t.Errorf("%s does not name the charset the text item carries:\n%s", codecFile, source)
	}
}

// TestAGroupOverrideReachingEveryUsageUnderItStillGenerates is the shape an
// `encoding-override` naming a group actually produces.
//
// docs/layout/SPEC.md, docs/ir/SPEC.md and this command's README all say the
// same thing: `(charset none)` is inert on every usage the charset does not
// govern, because an override names an item and that item **MAY** be a group,
// and a group holds items of every usage. So the packed item below carries
// CHARSET_NONE and is a packed item still — it is read through its digits, not
// through a code page — and the descriptor it sits in resolves to the charset
// the items the charset does govern carry.
//
// It is pinned here because the per-field reading of the agreement generates
// for the alphanumeric item and refuses this: a packed field skipped by nothing
// and compared on all four axes disagrees with every text field beside it, and
// the whole descriptor is turned down over an item the charset was never read
// on.
func TestAGroupOverrideReachingEveryUsageUnderItStillGenerates(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "REGION-RECORD", 2),
			group(2, "REGION-RECORD", nil, 3, 4, 5),
			alphanumeric(3, "REG-CODE", 2),
			noCharset(packed(4, "REG-AMT", 4, 7, 2, true)),
			noCharset(binary(5, "REG-COUNT", 2, 4, true)),
		},
	}

	out := t.TempDir()

	if err := generate(io.Discard, d, out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	source := written(t, out)[codecFile]

	// The charset is the one item that stated one, and the other two are read
	// as the numbers they are rather than as bytes.
	for _, want := range []string{
		"codec.CP037()",
		"r.ReadPackedInt32(7)",
		"r.ReadBinaryInt16(4)",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("%s does not carry %q:\n%s", codecFile, want, source)
		}
	}
}

// TestAnItemNoCharsetGovernsIsStillHeldToTheOtherThreeAxes is the other
// direction of the same split, and the reason it is a split rather than a skip.
//
// Stating no charset is a statement about the charset and about nothing else. A
// packed item's sign convention and a binary item's byte order are read whether
// its charset is cp037, none, or anything else, and an `encoding-override` may
// set those axes too — so a field that dropped out of the agreement entirely
// would be a field whose byte order this generator never checked and whose
// numbers come back reversed with nothing reporting it.
func TestAnItemNoCharsetGovernsIsStillHeldToTheOtherThreeAxes(t *testing.T) {
	t.Parallel()

	item := noCharset(binary(4, "REG-COUNT", 2, 4, true))
	item.GetField().GetEncoding().ByteOrder = irpb.ByteOrder_BYTE_ORDER_LITTLE_ENDIAN

	d := &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "REGION-RECORD", 2),
			group(2, "REGION-RECORD", nil, 3, 4),
			alphanumeric(3, "REG-CODE", 2),
			item,
		},
	}

	err := generate(io.Discard, d, t.TempDir(), options{packageName: goldenPackage, importPath: goldenImport})

	var refusal *mixedEncodingError
	if !errors.As(err, &refusal) {
		t.Fatalf("generate returned %v, want a refusal", err)
	}

	for _, want := range []string{"REG-CODE", "REG-COUNT", "byte order"} {
		if !strings.Contains(refusal.Error(), want) {
			t.Errorf("the refusal reads %q and does not name %s", refusal, want)
		}
	}
}

// TestADescriptorEveryItemOfWhichCarriesNoCharsetDeclaresNoEncoding is the
// consequence of the rule above, taken to the end of its range.
//
// Where no item states a charset there is nothing to read an Encoding off, which
// is the answer a descriptor carrying no field at all already gives: no helper,
// and the caller passes their own. It is the right answer rather than a gap —
// the descriptor states no charset for the file its records live in, so there is
// none to hand anybody — and this is here because the alternative is a nil
// dereference somewhere in the walk rather than an omission anybody decided on.
func TestADescriptorEveryItemOfWhichCarriesNoCharsetDeclaresNoEncoding(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "ORDER-RECORD", 2),
			group(2, "ORDER-RECORD", nil, 3, 4),
			opaque(3, "STATUS-FLAG", 4),
			opaque(4, "REGION-CODE", 2),
		},
	}

	out := t.TempDir()

	if err := generate(io.Discard, d, out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	files := written(t, out)

	if strings.Contains(files[codecFile], "func "+encodingFunc+"()") {
		t.Errorf("%s declares %s for a descriptor that states nothing about the file:\n%s", codecFile, encodingFunc, files[codecFile])
	}

	// Both items are still fields, and both are still read and written: it is
	// the file-level profile that is absent, not the records.
	for _, want := range []string{"r.ReadBytes(4)", "r.ReadBytes(2)"} {
		if !strings.Contains(files[codecFile], want) {
			t.Errorf("%s does not carry %q:\n%s", codecFile, want, files[codecFile])
		}
	}

	// The generated tests lay their bytes out under the same profile, so a
	// descriptor that resolved none has no case to make rather than a case
	// made under a profile this generator picked.
	if _, written := files[recordsTestFile]; written {
		t.Errorf("%s was generated for a descriptor with no encoding to lay bytes out under", recordsTestFile)
	}
}

// TestADisplayItemCarryingNoCharsetAndNoAlphanumericPictureIsRefused is the
// producer bug the charset admits no reading of.
//
// docs/ir/SPEC.md puts CHARSET_NONE on a DISPLAY item of the alphanumeric
// category and on nothing else. On any other DISPLAY item the descriptor states
// two facts that cannot both be honoured — an edited item's storage is the
// edited characters, and a zoned item's digits are read through the charset's
// own digit zone — so it is refused rather than read one way or the other, for
// the reason every other refusal in this generator exists: either reading hands
// a caller a value nothing in the file disagrees with.
func TestADisplayItemCarryingNoCharsetAndNoAlphanumericPictureIsRefused(t *testing.T) {
	t.Parallel()

	for name, item := range map[string]*irpb.Node{
		"a zoned item":                zoned(3, "LINE-COUNT", 3, 3, 0, false),
		"an alphabetic item":          categorized(3, "REGION-NAME", 6, irpb.Category_CATEGORY_ALPHABETIC),
		"an alphanumeric-edited item": categorized(3, "MASKED-ID", 6, irpb.Category_CATEGORY_ALPHANUMERIC_EDITED),
		"a numeric-edited item":       categorized(3, "SHOWN-TOTAL", 8, irpb.Category_CATEGORY_NUMERIC_EDITED),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			item.GetField().GetEncoding().Charset = irpb.Charset_CHARSET_NONE

			d := &irpb.Descriptor{
				Version: supportedIRVersion,
				Nodes: []*irpb.Node{
					record(1, "ORDER-RECORD", 2),
					group(2, "ORDER-RECORD", nil, 3),
					item,
				},
			}

			err := generate(io.Discard, d, t.TempDir(), options{packageName: goldenPackage, importPath: goldenImport})

			var refusal *malformedError
			if !errors.As(err, &refusal) {
				t.Fatalf("generate returned %v, want a malformed descriptor", err)
			}

			original := item.GetField().GetNames().GetOriginal()
			category := categoryName(item.GetField().GetPicture().GetCategory())

			for _, want := range []string{original, category} {
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

	if err := generate(io.Discard, d, out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
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

	err := generate(io.Discard, d, t.TempDir(), options{packageName: goldenPackage, importPath: goldenImport})

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

	err := generate(io.Discard, d, t.TempDir(), options{packageName: goldenPackage, importPath: goldenImport})

	var refusal *malformedError
	if !errors.As(err, &refusal) {
		t.Fatalf("generate returned %v, want a malformed descriptor", err)
	}

	if !strings.Contains(refusal.Error(), "ORDER-TOTAL") {
		t.Errorf("the refusal reads %q and does not name the item", refusal)
	}
}

// TestAComp6ItemIsReadAndWrittenWithComp6Accessors is the pairing this
// generator got wrong: COMP-6 shared a case with PACKED-DECIMAL and was read
// with the packed accessors, which consume a sign nibble a COMP-6 item does not
// carry (#162).
//
// The assertion is over the generated source rather than over bytes because it
// is about which accessor is called; that the two accessors disagree over one
// file is TestTheTwoPackedReadingsOfTheSameBytesDifferByTheAccessorAlone's, and
// the corpus entry packed-comp6 is the two joined up through a compiled package.
//
// The packed forms are asserted absent as well as the COMP-6 ones present. A
// test that only looks for what it wants would pass on a generator that emitted
// both, and it is the *substitution* that is the defect here rather than an
// omission.
func TestAComp6ItemIsReadAndWrittenWithComp6Accessors(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		item    *irpb.Node
		want    []string
		unwant  []string
		comment string
	}{
		"four digits, an even count where the widths differ": {
			item: comp6(3, "QUANTITY", 2, 4, 0),
			want: []string{
				"Quantity int32",
				"r.ReadComp6Int32(4)",
				"w.WriteComp6Int32(x.Quantity, 4)",
			},
			unwant: []string{"ReadPackedInt32", "WritePackedInt32"},
		},
		"five digits, an odd count where they coincide": {
			item: comp6(3, "QUANTITY", 3, 5, 0),
			want: []string{
				"r.ReadComp6Int32(5)",
				"w.WriteComp6Int32(x.Quantity, 5)",
			},
			unwant: []string{"ReadPackedInt32", "WritePackedInt32"},
		},
		"eighteen digits, the int64 family": {
			item: comp6(3, "QUANTITY", 9, 18, 0),
			want: []string{
				"Quantity int64",
				"r.ReadComp6Int64(18)",
				"w.WriteComp6Int64(x.Quantity, 18)",
			},
			unwant: []string{"ReadPackedInt64", "WritePackedInt64"},
		},
		"nineteen digits, the big family": {
			item: comp6(3, "QUANTITY", 10, 19, 0),
			want: []string{
				"r.ReadComp6Big(19)",
				"w.WriteComp6Big(x.Quantity, 19)",
			},
			unwant: []string{"ReadPackedBig", "WritePackedBig"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			d := &irpb.Descriptor{
				Version: supportedIRVersion,
				Nodes: []*irpb.Node{
					record(1, "COUNT-RECORD", 2),
					group(2, "COUNT-RECORD", nil, 3),
					tc.item,
				},
			}

			out := t.TempDir()

			if err := generate(io.Discard, d, out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
				t.Fatalf("generate: %v", err)
			}

			files := written(t, out)
			source := files[recordsFile] + files[codecFile]

			for _, want := range tc.want {
				if !strings.Contains(source, want) {
					t.Errorf("the generated package does not contain %q\n%s", want, source)
				}
			}

			for _, unwant := range tc.unwant {
				if strings.Contains(source, unwant) {
					t.Errorf("the generated package reaches for %q, which reads a sign nibble a COMP-6 item does not carry\n%s", unwant, source)
				}
			}
		})
	}
}

// TestASignedComp6ItemIsRefused is the other half of parting the two usages.
//
// A COMP-6 field is one nibble per digit and nothing else, so an S on its
// picture describes a sign the bytes have no room for. docs/ir/SPEC.md makes
// that a producer's error, and a consumer that read it as unsigned anyway would
// be silently right about every value it was ever shown and wrong about the
// negative ones — so it is refused here instead, by name.
func TestASignedComp6ItemIsRefused(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "COUNT-RECORD", 2),
			group(2, "COUNT-RECORD", nil, 3),
			signedComp6(3, "ON-HAND", 2, 4),
		},
	}

	err := generate(io.Discard, d, t.TempDir(), options{packageName: goldenPackage, importPath: goldenImport})

	var refusal *malformedError
	if !errors.As(err, &refusal) {
		t.Fatalf("generate returned %v, want a malformed descriptor", err)
	}

	if !strings.Contains(refusal.Error(), "ON-HAND") {
		t.Errorf("the refusal reads %q and does not name the item", refusal)
	}
}

// TestTheReceiverIsTheManifestsAndNotThisGeneratorsChoice covers the one option
// that changes the source without changing what it does.
func TestTheReceiverIsTheManifestsAndNotThisGeneratorsChoice(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	if err := generate(io.Discard, ordersDescriptor(), out, options{packageName: goldenPackage, importPath: goldenImport, receiver: "o"}); err != nil {
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
		if err := generate(io.Discard, ordersDescriptor(), out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
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

	if err := generate(io.Discard, descriptorAt(supportedIRVersion), out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
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

	if _, err := c.width(2, "ENTRY", "x.Entry[i0]", decoding); err == nil {
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

			if err := generate(io.Discard, d, out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
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

	if err := generate(io.Discard, d, out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
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

			if err := generate(io.Discard, d, out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
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

// TestEveryBufferedTableOfARecordGetsASubCodecOfItsOwn is what makes hoisting
// the sub-codecs out of their loops safe.
//
// A sub-reader and a sub-writer used to be declared inside the block of the
// loop over the occurrences they served, where the name could not collide with
// anything. They now stand at the top of the method, so a record with more than
// one buffered table declares more than one of them in one scope, and a name
// that is a function of the nesting *depth* would be one identifier declared
// twice. The counter is what fixes that, and this descriptor is the shape that
// would show it broken: two tables side by side at one depth, and a third
// nested inside one of them.
//
// No golden package carries that shape — `orders` and `fixed` each declare a
// single, top-level ENTRY — so this is where it is held. It also pins the other
// half of the hoist: every sub-codec declared is rewound, so a table whose
// declaration is emitted and whose Reset is not fails here rather than
// generating a record that decodes every occurrence over the last one's bytes.
func TestEveryBufferedTableOfARecordGetsASubCodecOfItsOwn(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	if err := generate(io.Discard, nestedTablesDescriptor(), out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	source := written(t, out)[codecFile]

	// One direction at a time, by the type each declaration names: a sub-reader
	// is only ever declared in UnmarshalCOBOL and a sub-writer only ever in
	// MarshalCOBOL, so the type is what tells the two methods apart without
	// carving the file up.
	for _, typ := range []string{"Reader", "Writer"} {
		declared := regexp.MustCompile(`var (entry[0-9]+) \*codec\.`+typ).FindAllStringSubmatch(source, -1)

		if len(declared) != 3 {
			t.Errorf("this record carries three buffered tables and MarshalCOBOL/UnmarshalCOBOL declares %d codec.%s over them\n%s",
				len(declared), typ, source)

			continue
		}

		seen := make(map[string]struct{}, len(declared))

		for _, match := range declared {
			name := match[1]

			if _, again := seen[name]; again {
				t.Errorf("%s is declared twice as a codec.%s in one method, which is a record that does not compile\n%s",
					name, typ, source)
			}

			seen[name] = struct{}{}

			if !strings.Contains(source, name+".Reset(") {
				t.Errorf("%s is declared as a codec.%s and never rewound onto an occurrence\n%s", name, typ, source)
			}
		}
	}
}

// nestedTablesDescriptor is one record carrying three tables that each hold a
// variant: ALPHA and BETA side by side, and GAMMA inside ALPHA.
//
// Every one of them repeats and holds an alternation, which is what makes each
// an occurrence read whole before it is walked — the only thing a sub-codec is
// built for.
func nestedTablesDescriptor() *irpb.Descriptor {
	return &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "MANY-RECORD", 2),
			group(2, "MANY-RECORD", nil, 10, 20),

			group(10, "ALPHA", constant(2), 11, 30, 12),
			alphanumeric(11, "ALPHA-TYPE", 1),

			group(30, "GAMMA", constant(2), 31, 32),
			alphanumeric(31, "GAMMA-TYPE", 1),
			variant(32, armOf(52, 35), armOf(53, 36)),
			equals(52, 31, "D"),
			equals(53, 31, "S"),
			group(35, "GAMMA-DETAIL", nil, 37),
			alphanumeric(37, "GAMMA-SKU", 3),
			group(36, "GAMMA-SUMMARY", nil, 38),
			alphanumeric(38, "GAMMA-TEXT", 3),

			variant(12, armOf(50, 15), armOf(51, 16)),
			equals(50, 11, "D"),
			equals(51, 11, "S"),
			group(15, "ALPHA-DETAIL", nil, 17),
			alphanumeric(17, "ALPHA-SKU", 4),
			group(16, "ALPHA-SUMMARY", nil, 18),
			alphanumeric(18, "ALPHA-TEXT", 4),

			group(20, "BETA", constant(2), 21, 22),
			alphanumeric(21, "BETA-TYPE", 1),
			variant(22, armOf(54, 25), armOf(55, 26)),
			equals(54, 21, "D"),
			equals(55, 21, "S"),
			group(25, "BETA-DETAIL", nil, 27),
			alphanumeric(27, "BETA-SKU", 2),
			group(26, "BETA-SUMMARY", nil, 28),
			alphanumeric(28, "BETA-TEXT", 2),
		},
	}
}
