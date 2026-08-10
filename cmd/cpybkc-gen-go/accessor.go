// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"strings"

	"github.com/Zaba505/cpybkc/irpb"
)

// encodingFunc is the function the generated package declares for the four
// axes this descriptor resolved.
const encodingFunc = "Encoding"

// readCall is the one codec accessor an elementary item is read with.
//
// The accessor is selected by the item's USAGE and by how many digits its
// PICTURE declares, and every argument it takes comes out of the IR: the width,
// the digit count, and where the sign sits. Nothing here re-derives an
// attribute from a PICTURE — resolve did that once, and docs/ir/SPEC.md's
// "Dereferencing is not recomputation" is why a second reading of it does not
// happen in a generator.
func (c *coder) readCall(f *irpb.Field, rdr string) (string, error) {
	if err := resolved(f.GetEncoding()); err != nil {
		return "", err
	}

	switch f.GetUsage() {
	case irpb.Usage_USAGE_COMP_1:
		return rdr + ".ReadFloat32()", nil
	case irpb.Usage_USAGE_COMP_2:
		return rdr + ".ReadFloat64()", nil
	case irpb.Usage_USAGE_INDEX, irpb.Usage_USAGE_POINTER, irpb.Usage_USAGE_NATIONAL:
		return fmt.Sprintf("%s.ReadBytes(%d)", rdr, f.GetWidth()), nil
	case irpb.Usage_USAGE_DISPLAY:
		if f.GetPicture().GetCategory() != irpb.Category_CATEGORY_NUMERIC {
			if _, err := c.fieldType(f); err != nil {
				return "", err
			}

			return fmt.Sprintf("%s.ReadAlphanumeric(%d)", rdr, f.GetWidth()), nil
		}

		position, err := signPosition(f)
		if err != nil {
			return "", err
		}

		return fmt.Sprintf("%s.ReadZoned%s(%d, %s)", rdr, decimalFamily(f.GetPicture().GetDigits()), f.GetPicture().GetDigits(), position), nil
	case irpb.Usage_USAGE_PACKED_DECIMAL, irpb.Usage_USAGE_COMP_6:
		if err := numeric(f); err != nil {
			return "", err
		}

		return fmt.Sprintf("%s.ReadPacked%s(%d)", rdr, decimalFamily(f.GetPicture().GetDigits()), f.GetPicture().GetDigits()), nil
	case irpb.Usage_USAGE_BINARY:
		if err := numeric(f); err != nil {
			return "", err
		}

		return fmt.Sprintf("%s.ReadBinary%s(%d)", rdr, binaryFamily(f.GetPicture().GetDigits()), f.GetPicture().GetDigits()), nil
	case irpb.Usage_USAGE_COMP_5:
		if err := numeric(f); err != nil {
			return "", err
		}

		return fmt.Sprintf("%s.ReadComp5%s(%d)", rdr, binaryFamily(f.GetPicture().GetDigits()), f.GetPicture().GetDigits()), nil
	default:
		return "", malformed(fmt.Sprintf("an item carries USAGE %d, which this generator does not know", int32(f.GetUsage())),
			"docs/ir/SPEC.md requires a consumer to refuse a member of a closed set it does not recognise rather than fall back to one it does")
	}
}

// writeCall is the accessor the same item is written with.
//
// The writers take one thing the readers do not, and it is the one thing the
// value being written cannot say: whether the item's PICTURE carries an S.
// Writing a signed item as unsigned discards the sign of every negative value
// and writing an unsigned one as signed stores a C where a reader expects an F,
// so it comes from the IR on every call rather than from the number in hand.
func (c *coder) writeCall(f *irpb.Field, value, wtr string) (string, error) {
	if err := resolved(f.GetEncoding()); err != nil {
		return "", err
	}

	switch f.GetUsage() {
	case irpb.Usage_USAGE_COMP_1:
		return fmt.Sprintf("%s.WriteFloat32(%s)", wtr, value), nil
	case irpb.Usage_USAGE_COMP_2:
		return fmt.Sprintf("%s.WriteFloat64(%s)", wtr, value), nil
	case irpb.Usage_USAGE_INDEX, irpb.Usage_USAGE_POINTER, irpb.Usage_USAGE_NATIONAL:
		return fmt.Sprintf("%s.WriteBytes(%s)", wtr, value), nil
	case irpb.Usage_USAGE_DISPLAY:
		if f.GetPicture().GetCategory() != irpb.Category_CATEGORY_NUMERIC {
			if _, err := c.fieldType(f); err != nil {
				return "", err
			}

			return fmt.Sprintf("%s.WriteAlphanumeric(%s, %d)", wtr, value, f.GetWidth()), nil
		}

		position, err := signPosition(f)
		if err != nil {
			return "", err
		}

		return fmt.Sprintf("%s.WriteZoned%s(%s, %d, %s)", wtr, decimalFamily(f.GetPicture().GetDigits()), value, f.GetPicture().GetDigits(), position), nil
	case irpb.Usage_USAGE_PACKED_DECIMAL, irpb.Usage_USAGE_COMP_6:
		if err := numeric(f); err != nil {
			return "", err
		}

		return fmt.Sprintf("%s.WritePacked%s(%s, %d, %s)", wtr, decimalFamily(f.GetPicture().GetDigits()), value, f.GetPicture().GetDigits(), signedness(f)), nil
	case irpb.Usage_USAGE_BINARY:
		if err := numeric(f); err != nil {
			return "", err
		}

		return fmt.Sprintf("%s.WriteBinary%s(%s, %d, %s)", wtr, binaryFamily(f.GetPicture().GetDigits()), value, f.GetPicture().GetDigits(), signedness(f)), nil
	case irpb.Usage_USAGE_COMP_5:
		if err := numeric(f); err != nil {
			return "", err
		}

		return fmt.Sprintf("%s.WriteComp5%s(%s, %d, %s)", wtr, binaryFamily(f.GetPicture().GetDigits()), value, f.GetPicture().GetDigits(), signedness(f)), nil
	default:
		return "", malformed(fmt.Sprintf("an item carries USAGE %d, which this generator does not know", int32(f.GetUsage())),
			"docs/ir/SPEC.md requires a consumer to refuse a member of a closed set it does not recognise rather than fall back to one it does")
	}
}

// rawWidth reports whether the item's field holds the bytes themselves, which
// is the one Go type a writer has to check the length of: every other one is as
// wide as the item by construction, and a []byte is whatever the caller put in
// it.
// It is read off the USAGE rather than off the Go type [emitter.fieldType]
// gives, because asking for that type is what tells the struct emitter a file
// imports math/big — and this file imports it only where it writes a count too
// wide for an int64.
func (c *coder) rawWidth(f *irpb.Field) (bool, error) {
	switch f.GetUsage() {
	case irpb.Usage_USAGE_INDEX, irpb.Usage_USAGE_POINTER, irpb.Usage_USAGE_NATIONAL:
		return true, nil
	default:
		return false, nil
	}
}

// decimalFamily is which of codec's zoned and packed accessors reads an item of
// that many digits, which is the family whose Go type [emitter.decimal] gave
// the field.
func decimalFamily(digits uint32) string {
	switch {
	case digits <= 9:
		return "Int32"
	case digits <= 18:
		return "Int64"
	default:
		return "Big"
	}
}

// binaryFamily is the same for a binary item, which has one more step because a
// binary item of four digits or fewer occupies two bytes.
func binaryFamily(digits uint32) string {
	if digits <= 4 {
		return "Int16"
	}

	return decimalFamily(digits)
}

// signPosition is codec's name for where a zoned item keeps its sign.
//
// It is required on the reading side as well as the writing one, and it is what
// makes a SEPARATE item digits+1 bytes wide, so a descriptor that leaves it
// unset on a signed DISPLAY item is refused rather than read as unsigned: a
// separate sign taken for a digit shifts every later field of the record.
func signPosition(f *irpb.Field) (string, error) {
	switch f.GetPicture().GetSignPosition() {
	case irpb.SignPosition_SIGN_POSITION_LEADING:
		return "codec.SignLeading", nil
	case irpb.SignPosition_SIGN_POSITION_TRAILING:
		return "codec.SignTrailing", nil
	case irpb.SignPosition_SIGN_POSITION_LEADING_SEPARATE:
		return "codec.SignLeadingSeparate", nil
	case irpb.SignPosition_SIGN_POSITION_TRAILING_SEPARATE:
		return "codec.SignTrailingSeparate", nil
	default:
		if f.GetPicture().GetSigned() {
			return "", malformed(fmt.Sprintf("%s is a signed DISPLAY item and its encoding says nothing about where the sign sits", f.GetNames().GetOriginal()),
				"a signed DISPLAY item carries a sign position; see docs/ir/SPEC.md and the SignPosition enum")
		}

		return "codec.SignUnsigned", nil
	}
}

// signedness is codec's name for whether the item's PICTURE carries an S.
func signedness(f *irpb.Field) string {
	if f.GetPicture().GetSigned() {
		return "codec.Signed"
	}

	return "codec.Unsigned"
}

// profile is the [encodingFunc] declaration for this descriptor, or the empty
// string where it carries no field for one to be read off.
//
// The four axes come from the IR and are refused where two items disagree.
// codec carries an Encoding per Reader and per Writer rather than per field,
// because the axes are properties of the file rather than of an item, so a
// descriptor whose items disagree describes a file there is no single Encoding
// to read.
func (c *coder) profile(d *irpb.Descriptor) (string, error) {
	var (
		enc  *irpb.Encoding
		from string
	)

	for _, node := range d.GetNodes() {
		field := node.GetField()
		if field == nil {
			continue
		}

		if err := resolved(field.GetEncoding()); err != nil {
			return "", err
		}

		if enc == nil {
			enc, from = field.GetEncoding(), originalOf(node)

			continue
		}

		if axis := disagreement(enc, field.GetEncoding()); axis != "" {
			return "", &mixedEncodingError{Axis: axis, First: from, Second: originalOf(node)}
		}
	}

	if enc == nil {
		return "", nil
	}

	charset, err := charsetCall(enc.GetCharset())
	if err != nil {
		return "", err
	}

	c.imports["encoding/binary"] = struct{}{}

	doc := commentLines(fmt.Sprintf(`%s is the byte-level interpretation of the files these records live in:
the four axes as the layout declared them and resolve resolved them.

None of the four has a default and every one of them fails silently when
wrong, so codec has no usable zero-value Reader and this function is how a
caller states all four at once:

	r, err := codec.NewReader(f, %s())

It is a value a caller passes rather than one anything applies on its own. A
file this descriptor describes that was converted to another character set is
read by passing a different Encoding, not by regenerating.`, encodingFunc, encodingFunc))

	return doc + fmt.Sprintf(`func %s() codec.Encoding {
return codec.Encoding{
Charset: %s,
Sign: %s,
ByteOrder: %s,
Float: %s,
}
}`, encodingFunc, charset, signConvention(enc.GetSignConvention()), byteOrder(enc.GetByteOrder()), floatFormat(enc.GetFloatFormat())), nil
}

// disagreement is the first of the four axes two encodings differ on, or the
// empty string where they agree.
func disagreement(a, b *irpb.Encoding) string {
	switch {
	case a.GetCharset() != b.GetCharset():
		return "character set"
	case a.GetSignConvention() != b.GetSignConvention():
		return "zoned sign convention"
	case a.GetByteOrder() != b.GetByteOrder():
		return "byte order"
	case a.GetFloatFormat() != b.GetFloatFormat():
		return "floating-point format"
	default:
		return ""
	}
}

// charsetCall is the codec constructor for a charset, and a refusal for one
// codec ships no table for.
func charsetCall(charset irpb.Charset) (string, error) {
	switch charset {
	case irpb.Charset_CHARSET_CP037:
		return "codec.CP037()", nil
	case irpb.Charset_CHARSET_ASCII:
		return "codec.ASCII()", nil
	default:
		return "", &unsupportedCharsetError{Charset: charset}
	}
}

// charsetName is a charset as a diagnostic names it.
func charsetName(charset irpb.Charset) string {
	switch charset {
	case irpb.Charset_CHARSET_CP037:
		return "cp037"
	case irpb.Charset_CHARSET_CP500:
		return "cp500"
	case irpb.Charset_CHARSET_CP1047:
		return "cp1047"
	case irpb.Charset_CHARSET_CP1140:
		return "cp1140"
	case irpb.Charset_CHARSET_ASCII:
		return "ASCII"
	default:
		return "a charset the descriptor does not name"
	}
}

// signConvention is codec's name for how an overpunched sign is spelled.
func signConvention(sign irpb.SignConvention) string {
	switch sign {
	case irpb.SignConvention_SIGN_CONVENTION_EBCDIC:
		return "codec.SignEBCDIC"
	case irpb.SignConvention_SIGN_CONVENTION_ASCII_ZONE37:
		return "codec.SignASCIIZone37"
	case irpb.SignConvention_SIGN_CONVENTION_TRANSLATED_EBCDIC:
		return "codec.SignTranslatedEBCDIC"
	default:
		return "codec.SignRealia"
	}
}

// byteOrder is the standard library's name for a byte order.
func byteOrder(order irpb.ByteOrder) string {
	if order == irpb.ByteOrder_BYTE_ORDER_LITTLE_ENDIAN {
		return "binary.LittleEndian"
	}

	return "binary.BigEndian"
}

// floatFormat is codec's name for a floating-point format.
func floatFormat(format irpb.FloatFormat) string {
	if format == irpb.FloatFormat_FLOAT_FORMAT_IEEE754 {
		return "codec.FloatIEEE"
	}

	return "codec.FloatHFP"
}

// assertions is the compile-time statement that every record satisfies both of
// codec's interfaces.
//
// It costs nothing and it is what makes the interfaces load-bearing: a method
// renamed or a signature drifted is a build failure in the generated package
// rather than a call site somewhere else that stops compiling for reasons that
// do not name this file.
func assertions(names []string) string {
	var b strings.Builder

	b.WriteString(commentLines(`Every record reads and writes itself, which is codec's Unmarshaler and
Marshaler. Asserted here so that a drift in either interface fails in the
file that implements it.`))
	b.WriteString("var (\n")

	for _, name := range names {
		fmt.Fprintf(&b, "_ codec.Unmarshaler = (*%s)(nil)\n", name)
		fmt.Fprintf(&b, "_ codec.Marshaler = (*%s)(nil)\n", name)
	}

	b.WriteString(")")

	return b.String()
}

// zeroFillDeclaration is the run of zero bytes a writer emits for a slack node
// the record it was handed carries no run for.
func zeroFillDeclaration(width uint32) string {
	return commentLines(fmt.Sprintf(`%s is what a writer emits for a slack node of a record its caller built
rather than read: zero bytes, %d of them at the most, sliced to the node's own
width.

Zero rather than a space, because charset is a property of a field and slack
is not a field, so there is no charset to resolve a space against and zero is
the byte that names none. Those bytes were never in a file, so nothing is
being overwritten — a record that was read carries the bytes it was read
from and they are emitted instead. See docs/ir/SPEC.md, "What the descriptor
determines, a writer supplies".`, zeroFillHelper, width)) +
		fmt.Sprintf("var %s = make([]byte, %d)", zeroFillHelper, width)
}

// resizedDeclaration is how a table sized by an OCCURS DEPENDING ON count gets
// that many occurrences.
func resizedDeclaration() string {
	return commentLines(fmt.Sprintf(`%s is s with n zero occurrences in it.

s is read for its type as much as for its capacity, and that is why this is a
function rather than a make: a group inside a record is an anonymous struct
type, which has no name to write inside a make. Type inference supplies what
there is no identifier for, and nothing here reflects.`, resizedHelper)) +
		fmt.Sprintf(`func %s[T any](s []T, n int) []T {
if n <= cap(s) {
s = s[:n]
clear(s)

return s
}

return make([]T, n)
}`, resizedHelper)
}

// freshDeclaration is how an occurrence takes the arm a variant's predicate
// selected.
func freshDeclaration() string {
	return commentLines(fmt.Sprintf(`%s is a zeroed arm to decode into, reusing the one the record already holds
where it holds one.

It exists for the reason %s does: an arm's body is an anonymous struct type,
so there is no name for a new to take, and the pointer's own type is what
supplies it.`, freshHelper, resizedHelper)) +
		fmt.Sprintf(`func %s[T any](p *T) *T {
if p != nil {
var zero T
*p = zero

return p
}

return new(T)
}`, freshHelper)
}
