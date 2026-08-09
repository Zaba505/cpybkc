// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"encoding/hex"
	"errors"
	"strings"

	"github.com/Zaba505/cpybkc/internal/layout"
)

// The tag a byte string is written with, wherever one appears.
const tagBytes = "bytes"

// ByteString is a run of bytes given literally: `(bytes "0D0A")`.
//
// docs/layout/SPEC.md's "Literals" is the whole of it — hexadecimal digits in
// pairs, taken as they are written, "with no charset and no padding". It is the
// one spelling in the format that says exactly what is in the file, which is why
// a framing's delimiter is written as one and never as a named character
// (docs/layout/SPEC.md's "A delimiter is bytes, and it has a placement").
//
// The zero value states nothing, which is what [ByteString.Stated] reports. A
// byte string of no bytes is a diagnostic rather than a stated empty one, so a
// value that was written always carries at least one byte.
type ByteString struct {
	// Pos is the `(bytes …)` form.
	Pos layout.Pos

	// Bytes is what the hexadecimal decoded to.
	Bytes []byte
}

// Stated reports whether a byte string was written at all.
func (b ByteString) Stated() bool { return len(b.Bytes) > 0 }

// String renders the byte string the way a layout writes it, which is what a
// diagnostic naming one quotes.
//
// The digits are upper case because that is how a dump prints them and how
// docs/layout/SPEC.md writes every example of one.
func (b ByteString) String() string {
	return `(bytes "` + strings.ToUpper(hex.EncodeToString(b.Bytes)) + `")`
}

// readByteString reads a `(bytes "…")` form.
//
// That the text is hexadecimal, and that a byte string of no bytes is a fault,
// are checked here because they are the two things schema/layout.sexpr says it
// leaves to the reader: the schema can declare the argument to be text and
// nothing more.
func readByteString(node layout.Node) (ByteString, error) {
	form, ok := node.(layout.Form)
	if !ok {
		return ByteString{}, &ByteStringError{Pos: node.Position(), Found: describe(node)}
	}

	if form.Tag != tagBytes {
		return ByteString{}, &ByteStringError{Pos: form.TagPos, Found: "form " + quote(form.Tag)}
	}

	if len(form.Elements) != 1 {
		found := "a byte string carrying nothing"
		if len(form.Elements) > 1 {
			found = "a byte string carrying several"
		}

		return ByteString{}, &ByteStringError{Pos: form.Pos, Found: found}
	}

	text, ok := form.Elements[0].(layout.Text)
	if !ok {
		return ByteString{}, &ByteStringError{Pos: form.Elements[0].Position(), Found: describe(form.Elements[0])}
	}

	if text.Value == "" {
		return ByteString{}, &ByteStringError{Pos: text.Pos, Found: "a byte string of no bytes"}
	}

	decoded, err := hex.DecodeString(text.Value)
	if err != nil {
		found := quote(text.Value) + ", which is not hexadecimal"
		if errors.Is(err, hex.ErrLength) {
			found = quote(text.Value) + ", which is an odd number of digits"
		}

		return ByteString{}, &ByteStringError{Pos: text.Pos, Found: found}
	}

	return ByteString{Pos: form.Pos, Bytes: decoded}, nil
}
