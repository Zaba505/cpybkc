// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package assemble

import (
	cobol "github.com/Zaba505/cobol-go"
	"github.com/Zaba505/cobol-go/copybook"
	"github.com/Zaba505/cobol-go/picture"

	"github.com/Zaba505/cpybkc/internal/layoutmodel"
	"github.com/Zaba505/cpybkc/internal/resolve"
	"github.com/Zaba505/cpybkc/irpb"
)

// This file is the spelling: each resolved value written into the member the
// schema gives it. Nothing here decides anything — `resolve` settled the axes, a
// layout settled the framing, and `cobol-go` parsed the PICTURE and inherited
// the USAGE — and every function is a total map from one closed set onto
// another.
//
// A value outside the set it maps from comes back as that member's unspecified
// zero rather than as a guess, and [Validate] refuses a descriptor carrying one.
// That is the only shape a mapping failure can take: a producer that quietly
// picked a neighbouring code page would be exactly the silent failure the four
// axes exist to prevent.

// fileOf is the dataset node: its physical framing, as one member of the closed
// set of four, and the state the read begins in.
//
// The layout's RECFM is mapped rather than carried, because docs/ir/SPEC.md's
// "Four framings, and none of them is a RECFM" keeps the adopter's spelling on
// the layout side and hands a consumer the four physical shapes: `F` and `FB`
// are one shape, `V` and `VB` another, and a consumer implementing RECFM would
// be implementing a JCL vocabulary it has no other use for.
//
// It carries no `lrecl`, no block size and no descriptor-word width. Each is
// absent for a reason docs/ir/SPEC.md's "Lengths the file node does not carry"
// gives, and the shortest of them is that a consumer takes a record's end from
// its extent — so a length here is a value a consumer would have to ignore. The
// one size any framing carries is a segmented dataset's largest segment, which
// is a bound on a *writer* rather than a length anything is read against.
//
// The framing is set rather than returned because the member set of a oneof is
// an unexported interface of the generated package: the four members are
// exported and each can be assigned, and naming their common type outside irpb
// is not something Go admits.
func fileOf(framing *layoutmodel.Framing, startState uint64) *irpb.File {
	file := &irpb.File{StartStateId: startState}

	switch framing.Kind() {
	case layoutmodel.Unframed:
		file.Framing = &irpb.File_Unframed{Unframed: &irpb.Unframed{}}
	case layoutmodel.DescriptorWord:
		file.Framing = &irpb.File_DescriptorWord{DescriptorWord: &irpb.DescriptorWord{}}
	case layoutmodel.Segmented:
		file.Framing = &irpb.File_Segmented{Segmented: &irpb.Segmented{
			MaxSegmentSize: width(int(framing.MaxSegment.Value)),
		}}
	case layoutmodel.Delimited:
		file.Framing = &irpb.File_Delimited{Delimited: &irpb.Delimited{
			Delimiter: framing.Delimiter.Bytes,
			Placement: placementOf(framing.Placement),
		}}
	}

	return file
}

// placementOf is where a delimited file's delimiter stands relative to the
// records it separates.
func placementOf(placement layoutmodel.Placement) irpb.DelimiterPlacement {
	switch placement {
	case layoutmodel.Terminator:
		return irpb.DelimiterPlacement_DELIMITER_PLACEMENT_TERMINATOR
	case layoutmodel.Separator:
		return irpb.DelimiterPlacement_DELIMITER_PLACEMENT_SEPARATOR
	case layoutmodel.OptionalTerminator:
		return irpb.DelimiterPlacement_DELIMITER_PLACEMENT_OPTIONAL_TERMINATOR
	}

	return irpb.DelimiterPlacement_DELIMITER_PLACEMENT_UNSPECIFIED
}

// encodingOf is the five resolved axes, on the field they govern.
//
// All five, on every field, with no profile node for one to inherit from: the
// pair a layout writes — one profile and per-item overrides over it — was
// applied by `resolve`, and what a field carries is the result
// (docs/ir/SPEC.md, "The encoding profile, applied").
//
// Four of them arrive as [layoutmodel.Axes], which is what a layout author
// wrote, and the fifth arrives on its own because nobody wrote it: the binary
// width staircase is the dialect's, and it reaches here as the staircase
// `resolve` actually laid the record out under rather than as a setting read a
// second time. Taking it as a parameter beside the axes is what keeps that
// true — an `encodingOf` that reached for a dialect itself would be a second
// reading of the same decision, and the failure this whole story is about is
// two readings of it disagreeing.
func (a *assembler) encodingOf(axes layoutmodel.Axes, binary copybook.BinarySize) *irpb.Encoding {
	return &irpb.Encoding{
		Charset:        charsetOf(axes.Charset),
		SignConvention: signConventionOf(axes.SignConvention),
		ByteOrder:      byteOrderOf(axes.ByteOrder),
		FloatFormat:    floatFormatOf(axes.FloatFormat),
		BinarySize:     a.binarySizeOf(binary),
	}
}

// binarySizeOf is the width staircase a compiler applies to USAGE BINARY items,
// as the IR spells it.
//
// The mapping is total over `copybook`'s declared members, and anything else
// is a **fault** rather than a value. There is no arm answering a staircase for
// a member this build does not know: a plausible default is exactly what makes
// a wrong staircase invisible, and the widths in hand were already laid out
// under whatever it was.
//
// Two different things reach the fault and both are reported the same way. One
// is a caller that assembled a record `resolve` never produced —
// `copybook.NewLayout` refuses an undeclared staircase before a width is
// computed, so no resolved record carries one. The other, and the one worth
// the fault, is `copybook` gaining a staircase this switch has not been taught:
// the record resolves perfectly, its widths are real, and the descriptor would
// otherwise go out carrying an unset axis — reported by [unresolved] against
// the *field*, which sends a reader to look at an item that is not what is
// wrong. Naming the dialect member here is what keeps that diagnosis pointing
// at the mapping.
func (a *assembler) binarySizeOf(binary copybook.BinarySize) irpb.BinarySize {
	switch binary {
	case copybook.BinarySize248:
		return irpb.BinarySize_BINARY_SIZE_248
	case copybook.BinarySize1248:
		return irpb.BinarySize_BINARY_SIZE_1248
	case copybook.BinarySizeSmallest:
		return irpb.BinarySize_BINARY_SIZE_SMALLEST
	case copybook.BinarySizeFull:
		return irpb.BinarySize_BINARY_SIZE_FULL
	}

	a.faults.Fail(&UnknownBinarySizeError{Binary: binary})

	return irpb.BinarySize_BINARY_SIZE_UNSPECIFIED
}

// charsetOf is the code page governing alphanumeric data, the digit zone of
// zoned decimal, and the byte values of a separate sign — or the statement that
// the item has no characters for one to govern.
//
// [layoutmodel.None] maps like any other member of the set. It is a value of the
// axis rather than a hole in it, so a field carrying it carries CHARSET_NONE and
// not the unspecified zero: the zero is nobody having answered, which
// docs/ir/SPEC.md makes a malformed descriptor a consumer refuses, and mapping
// the two together would spell an answer as its own absence.
func charsetOf(charset layoutmodel.Charset) irpb.Charset {
	switch charset {
	case layoutmodel.None:
		return irpb.Charset_CHARSET_NONE
	case layoutmodel.CP037:
		return irpb.Charset_CHARSET_CP037
	case layoutmodel.CP500:
		return irpb.Charset_CHARSET_CP500
	case layoutmodel.CP1047:
		return irpb.Charset_CHARSET_CP1047
	case layoutmodel.CP1140:
		return irpb.Charset_CHARSET_CP1140
	case layoutmodel.ASCII:
		return irpb.Charset_CHARSET_ASCII
	}

	return irpb.Charset_CHARSET_UNSPECIFIED
}

// signConventionOf is how an overpunched sign is spelled in a zoned decimal
// byte.
func signConventionOf(convention layoutmodel.SignConvention) irpb.SignConvention {
	switch convention {
	case layoutmodel.SignEBCDIC:
		return irpb.SignConvention_SIGN_CONVENTION_EBCDIC
	case layoutmodel.SignASCIIZone37:
		return irpb.SignConvention_SIGN_CONVENTION_ASCII_ZONE37
	case layoutmodel.SignTranslatedEBCDIC:
		return irpb.SignConvention_SIGN_CONVENTION_TRANSLATED_EBCDIC
	case layoutmodel.SignRealia:
		return irpb.SignConvention_SIGN_CONVENTION_REALIA
	}

	return irpb.SignConvention_SIGN_CONVENTION_UNSPECIFIED
}

// byteOrderOf is the order of the bytes in a binary integer.
func byteOrderOf(order layoutmodel.ByteOrder) irpb.ByteOrder {
	switch order {
	case layoutmodel.BigEndian:
		return irpb.ByteOrder_BYTE_ORDER_BIG_ENDIAN
	case layoutmodel.LittleEndian:
		return irpb.ByteOrder_BYTE_ORDER_LITTLE_ENDIAN
	}

	return irpb.ByteOrder_BYTE_ORDER_UNSPECIFIED
}

// floatFormatOf is the representation of a floating point item.
func floatFormatOf(format layoutmodel.FloatFormat) irpb.FloatFormat {
	switch format {
	case layoutmodel.IEEE754:
		return irpb.FloatFormat_FLOAT_FORMAT_IEEE754
	case layoutmodel.HFP:
		return irpb.FloatFormat_FLOAT_FORMAT_IBM_HFP
	}

	return irpb.FloatFormat_FLOAT_FORMAT_UNSPECIFIED
}

// usageOf is the item's USAGE, resolved to the one its bytes are in rather than
// to the alias the copybook spelled.
//
// `cobol-go` keeps every spelling the grammar admits distinct, because which
// representation a spelling selects is a property of the dialect and belongs
// beside the codec rather than in the record tree. The IR takes the other side
// deliberately: a generator switches over representations, and COMP, COMP-4 and
// BINARY are one representation under every dialect this project supports. So
// the aliases collapse here, which is the last place they can without a
// generator author having to know that they are aliases.
func usageOf(field *copybook.Field) irpb.Usage {
	if field == nil {
		return irpb.Usage_USAGE_UNSPECIFIED
	}

	switch field.Usage {
	case copybook.UsageDisplay:
		return irpb.Usage_USAGE_DISPLAY
	case copybook.UsagePackedDecimal, copybook.UsageComp3:
		return irpb.Usage_USAGE_PACKED_DECIMAL
	case copybook.UsageBinary, copybook.UsageComp, copybook.UsageComp4:
		return irpb.Usage_USAGE_BINARY
	case copybook.UsageComp5:
		return irpb.Usage_USAGE_COMP_5
	// COMP-6 is the one member of this list that is not an alias of anything.
	// It is a GnuCOBOL and Micro Focus extension — packed with no sign nibble —
	// and it collapses into no other member: `PIC 9(4) COMP-6` is two bytes
	// where `PIC 9(4) COMP-3` is three, so a descriptor naming PACKED-DECIMAL
	// for it would be one byte wide and one accessor wrong at once.
	case copybook.UsageComp6:
		return irpb.Usage_USAGE_COMP_6
	case copybook.UsageComp1:
		return irpb.Usage_USAGE_COMP_1
	case copybook.UsageComp2:
		return irpb.Usage_USAGE_COMP_2
	case copybook.UsageIndex:
		return irpb.Usage_USAGE_INDEX
	case copybook.UsagePointer:
		return irpb.Usage_USAGE_POINTER
	}

	return irpb.Usage_USAGE_UNSPECIFIED
}

// pictureOf is what the PICTURE character-string and the SIGN clause resolved
// to, and nil where the item has no PICTURE at all.
//
// Nothing is derived here. `cobol-go` parsed the character-string into its
// category, its digit count, its scale and whether it carries an operational
// sign, and reading it a second time would be a second answer to a question
// codec/SPEC.md's "From PICTURE to Attributes" has already settled. What this
// adds is the one attribute that is not the picture's — where the sign sits,
// which the SIGN clause says and which is inherited exactly as USAGE is.
func pictureOf(field *copybook.Field) *irpb.Picture {
	if field == nil || field.Picture == nil {
		return nil
	}

	return &irpb.Picture{
		Category:     categoryOf(field.Picture.Category),
		Digits:       width(field.Picture.Digits),
		Scale:        int32(field.Picture.Scale),
		Signed:       field.Picture.Signed,
		SignPosition: signPositionOf(field),
	}
}

// categoryOf is the category the set of symbols in the picture fixes.
func categoryOf(category picture.Category) irpb.Category {
	switch category {
	case picture.CategoryNumeric:
		return irpb.Category_CATEGORY_NUMERIC
	case picture.CategoryAlphabetic:
		return irpb.Category_CATEGORY_ALPHABETIC
	case picture.CategoryAlphanumeric:
		return irpb.Category_CATEGORY_ALPHANUMERIC
	case picture.CategoryNumericEdited:
		return irpb.Category_CATEGORY_NUMERIC_EDITED
	case picture.CategoryAlphanumericEdited:
		return irpb.Category_CATEGORY_ALPHANUMERIC_EDITED
	}

	return irpb.Category_CATEGORY_UNSPECIFIED
}

// signPositionOf is where an operational sign is held, and unspecified where the
// question does not arise.
//
// It does not arise for an unsigned item, and it does not arise for a USAGE the
// SIGN clause has no effect on — which is every usage other than DISPLAY, since
// packed and binary items hold their sign where their representation puts it.
// An absent clause is TRAILING, non-separate: that is COBOL's default and it is
// the one the item's width already reflects, because a separate sign costs a
// byte and `cobol-go` counted it.
func signPositionOf(field *copybook.Field) irpb.SignPosition {
	if !field.Picture.Signed || field.Usage != copybook.UsageDisplay {
		return irpb.SignPosition_SIGN_POSITION_UNSPECIFIED
	}

	clause := inheritedSign(field)
	if clause == nil {
		return irpb.SignPosition_SIGN_POSITION_TRAILING
	}

	if clause.Position == "LEADING" {
		if clause.Separate {
			return irpb.SignPosition_SIGN_POSITION_LEADING_SEPARATE
		}

		return irpb.SignPosition_SIGN_POSITION_LEADING
	}

	if clause.Separate {
		return irpb.SignPosition_SIGN_POSITION_TRAILING_SEPARATE
	}

	return irpb.SignPosition_SIGN_POSITION_TRAILING
}

// inheritedSign is the SIGN clause governing a field: its own, or the nearest
// one on a group above it, and nil where neither states one.
//
// The clause may be written on the item or on any group above it, applying to
// every signed numeric DISPLAY item subordinate to it, so the nearest one wins —
// the same inheritance USAGE follows, which `cobol-go` has already run for
// USAGE and does not expose for this.
func inheritedSign(field *copybook.Field) *cobol.SignClause {
	for item := field; item != nil; item = item.Parent {
		if item.Entry == nil {
			continue
		}

		for _, clause := range item.Entry.Clauses {
			if sign, ok := clause.(*cobol.SignClause); ok {
				return sign
			}
		}
	}

	return nil
}

// registerKindOf is what a register holds: the source field's bytes as they
// appear in the record, or a number decoded from it by that field's own axes.
func registerKindOf(kind resolve.RegisterKind) irpb.RegisterKind {
	switch kind {
	case resolve.RegisterBytes:
		return irpb.RegisterKind_REGISTER_KIND_BYTES
	case resolve.RegisterInteger:
		return irpb.RegisterKind_REGISTER_KIND_INTEGER
	}

	return irpb.RegisterKind_REGISTER_KIND_UNSPECIFIED
}
