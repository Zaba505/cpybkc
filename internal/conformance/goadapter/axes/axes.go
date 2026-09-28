// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package axes turns the axes an entry is read under into what codec takes, for
// the codec program the Go adapter compiles beside each generated package.
//
// It is the one place the adapter reads an axis's spelling. The spellings are
// the corpus's ([github.com/Zaba505/cpybkc/internal/conformance.Axes]) and the
// values are codec's; nothing here decides what an axis does to a byte, which is
// the generated code's to get right and the corpus's to check.
package axes

import (
	"encoding/binary"
	"fmt"

	"github.com/Zaba505/cobol-go/codec"

	"github.com/Zaba505/cpybkc/internal/conformance"
	"github.com/Zaba505/cpybkc/irpb"
)

// Charset is the code page a spelling names.
//
// codec ships two, cp037 and the identity charset, and this adds the third a
// read axis needs: cp1140, which is cp037 with the euro sign where cp037 has the
// currency sign. A code page neither codec nor this package carries is an
// error rather than a substitution — reading a file under cp037 because it was
// asked for cp500 would read most of it correctly and its brackets wrongly.
func Charset(spelled string) (codec.Charset, error) {
	switch spelled {
	case "ascii":
		return codec.ASCII(), nil
	case "cp037":
		return codec.CP037(), nil
	case "cp1140":
		return CP1140(), nil
	default:
		return nil, fmt.Errorf("this adapter has no table for the charset %q", spelled)
	}
}

// SignConvention is the zoned sign convention a spelling names.
func SignConvention(spelled string) (codec.SignConvention, error) {
	switch spelled {
	case "ebcdic":
		return codec.SignEBCDIC, nil
	case "ascii-zone-37":
		return codec.SignASCIIZone37, nil
	case "translated-ebcdic":
		return codec.SignTranslatedEBCDIC, nil
	case "realia":
		return codec.SignRealia, nil
	default:
		return codec.SignUnset, fmt.Errorf("there is no sign convention %q", spelled)
	}
}

// ByteOrder is the byte order a spelling names.
func ByteOrder(spelled string) (binary.ByteOrder, error) {
	switch spelled {
	case "big-endian":
		return binary.BigEndian, nil
	case "little-endian":
		return binary.LittleEndian, nil
	default:
		return nil, fmt.Errorf("there is no byte order %q", spelled)
	}
}

// FloatFormat is the floating-point format a spelling names.
func FloatFormat(spelled string) (codec.FloatFormat, error) {
	switch spelled {
	case "ieee-754":
		return codec.FloatIEEE, nil
	case "hfp":
		return codec.FloatHFP, nil
	default:
		return codec.FloatUnset, fmt.Errorf("there is no floating-point format %q", spelled)
	}
}

// BinarySize is the binary width staircase a spelling names, spelled as codec
// spells its own.
func BinarySize(spelled string) (codec.BinarySize, error) {
	for _, size := range []codec.BinarySize{
		codec.BinarySize248, codec.BinarySize1248, codec.BinarySizeSmallest, codec.BinarySizeFull,
	} {
		if size.String() == spelled {
			return size, nil
		}
	}

	return codec.BinarySizeUnset, fmt.Errorf("there is no binary width staircase %q", spelled)
}

// Resolved is the encoding the descriptor's fields resolved, as one codec
// encoding: the charset of the first field that has one, and the other four
// axes of the first field.
//
// It is what a caller of a record's own decoder builds a codec.Reader from,
// which is the one road a staircase can reach a generated package by. The
// package's own Encoding is not used because a descriptor no item of which
// states a charset has none generated, and this program is compiled against
// every entry's package alike.
func Resolved(descriptor *irpb.Descriptor) (codec.Encoding, error) {
	var (
		enc   codec.Encoding
		found bool
	)

	for _, node := range descriptor.GetNodes() {
		field := node.GetField()
		if field == nil {
			continue
		}

		axes := field.GetEncoding()

		if !found {
			found = true

			var err error

			if enc.Sign, err = resolvedSign(axes.GetSignConvention()); err != nil {
				return enc, err
			}

			if enc.ByteOrder, err = resolvedByteOrder(axes.GetByteOrder()); err != nil {
				return enc, err
			}

			if enc.Float, err = resolvedFloat(axes.GetFloatFormat()); err != nil {
				return enc, err
			}

			if enc.Binary, err = resolvedBinarySize(axes.GetBinarySize()); err != nil {
				return enc, err
			}
		}

		if enc.Charset == nil && axes.GetCharset() != irpb.Charset_CHARSET_NONE {
			charset, err := resolvedCharset(axes.GetCharset())
			if err != nil {
				return enc, err
			}

			enc.Charset = charset
		}
	}

	if !found {
		return enc, fmt.Errorf("the descriptor carries no field to read an encoding off")
	}

	if enc.Charset == nil {
		return enc, fmt.Errorf("no field of the descriptor states a charset")
	}

	return enc, nil
}

// Replace is enc with every axis the entry states replaced.
func Replace(enc codec.Encoding, stated *conformance.Axes) (codec.Encoding, error) {
	if stated == nil {
		return enc, nil
	}

	var err error

	if stated.Charset != "" {
		if enc.Charset, err = Charset(stated.Charset); err != nil {
			return enc, err
		}
	}

	if stated.SignConvention != "" {
		if enc.Sign, err = SignConvention(stated.SignConvention); err != nil {
			return enc, err
		}
	}

	if stated.ByteOrder != "" {
		if enc.ByteOrder, err = ByteOrder(stated.ByteOrder); err != nil {
			return enc, err
		}
	}

	if stated.FloatFormat != "" {
		if enc.Float, err = FloatFormat(stated.FloatFormat); err != nil {
			return enc, err
		}
	}

	if stated.BinarySize != "" {
		if enc.Binary, err = BinarySize(stated.BinarySize); err != nil {
			return enc, err
		}
	}

	return enc, nil
}

func resolvedCharset(charset irpb.Charset) (codec.Charset, error) {
	switch charset {
	case irpb.Charset_CHARSET_ASCII:
		return codec.ASCII(), nil
	case irpb.Charset_CHARSET_CP037:
		return codec.CP037(), nil
	case irpb.Charset_CHARSET_CP1140:
		return CP1140(), nil
	default:
		return nil, fmt.Errorf("this adapter has no table for the charset %s", charset)
	}
}

func resolvedSign(sign irpb.SignConvention) (codec.SignConvention, error) {
	switch sign {
	case irpb.SignConvention_SIGN_CONVENTION_EBCDIC:
		return codec.SignEBCDIC, nil
	case irpb.SignConvention_SIGN_CONVENTION_ASCII_ZONE37:
		return codec.SignASCIIZone37, nil
	case irpb.SignConvention_SIGN_CONVENTION_TRANSLATED_EBCDIC:
		return codec.SignTranslatedEBCDIC, nil
	case irpb.SignConvention_SIGN_CONVENTION_REALIA:
		return codec.SignRealia, nil
	default:
		return codec.SignUnset, fmt.Errorf("the descriptor names the sign convention %s, which codec has no member for", sign)
	}
}

func resolvedByteOrder(order irpb.ByteOrder) (binary.ByteOrder, error) {
	switch order {
	case irpb.ByteOrder_BYTE_ORDER_BIG_ENDIAN:
		return binary.BigEndian, nil
	case irpb.ByteOrder_BYTE_ORDER_LITTLE_ENDIAN:
		return binary.LittleEndian, nil
	default:
		return nil, fmt.Errorf("the descriptor names the byte order %s, which codec has no member for", order)
	}
}

func resolvedFloat(format irpb.FloatFormat) (codec.FloatFormat, error) {
	switch format {
	case irpb.FloatFormat_FLOAT_FORMAT_IEEE754:
		return codec.FloatIEEE, nil
	case irpb.FloatFormat_FLOAT_FORMAT_IBM_HFP:
		return codec.FloatHFP, nil
	default:
		return codec.FloatUnset, fmt.Errorf("the descriptor names the floating-point format %s, which codec has no member for", format)
	}
}

func resolvedBinarySize(size irpb.BinarySize) (codec.BinarySize, error) {
	switch size {
	case irpb.BinarySize_BINARY_SIZE_248:
		return codec.BinarySize248, nil
	case irpb.BinarySize_BINARY_SIZE_1248:
		return codec.BinarySize1248, nil
	case irpb.BinarySize_BINARY_SIZE_SMALLEST:
		return codec.BinarySizeSmallest, nil
	case irpb.BinarySize_BINARY_SIZE_FULL:
		return codec.BinarySizeFull, nil
	default:
		return codec.BinarySizeUnset, fmt.Errorf("the descriptor names the binary width staircase %s, which codec has no member for", size)
	}
}

// euro is where cp1140 and cp037 part: cp1140 is IBM's cp037 with the euro sign
// in place of the currency sign, at the one byte they differ on.
const (
	euro         = 0x9F
	euroSign     = '€'
	currencySign = '¤'
)

// CP1140 is EBCDIC code page 1140: cp037, with the euro sign U+20AC at 0x9F
// where cp037 has the currency sign U+00A4, and no byte at all for the currency
// sign.
//
// codec carries no table for it, and docs/ir/SPEC.md's own example of a
// character one code page has and another lacks is the euro sign between these
// two — which is why this adapter carries the one byte of difference rather
// than a table: everything else is cp037's.
func CP1140() codec.Charset { return cp1140{} }

type cp1140 struct{}

func (cp1140) Name() string { return "cp1140" }

func (cp1140) ToUnicode(b byte) rune {
	if b == euro {
		return euroSign
	}

	return codec.CP037().ToUnicode(b)
}

func (cp1140) FromUnicode(r rune) (byte, bool) {
	switch r {
	case euroSign:
		return euro, true
	case currencySign:
		return 0, false
	default:
		return codec.CP037().FromUnicode(r)
	}
}

func (cp1140) Space() byte { return codec.CP037().Space() }
