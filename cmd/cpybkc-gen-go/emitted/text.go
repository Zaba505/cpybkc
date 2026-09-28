// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package emitted

import (
	"fmt"

	"github.com/Zaba505/cobol-go/codec"
)

// reexpressText is a literal compared against a text item — every byte of it
// a character under the charset from — as the same characters under the
// charset to.
//
// Byte by byte, which is how a file crosses a charset: a transfer rewrites
// each byte through the two code pages and keeps each character, including the
// padding the producer applied, which arrives as the new charset's own space.
// The width therefore does not change, and nothing about COBOL's comparison
// rules is applied here — they were applied when the literal was resolved
// (docs/ir/SPEC.md, "Each axis carries a literal the way a file crosses it").
//
// A charset that is the same value on both sides moves nothing and is answered
// without a translation, so a literal is never refused over an axis that did
// not change. Where the charset does change, the one failure is a character the
// charset to has no byte for, and that is a [literalError] for the refusal to
// name. A literal that comes out as the bytes it went in as — "01" between two
// EBCDIC code pages agreeing on every digit — is carried, not refused.
func reexpressText(lit []byte, from, to codec.Charset) ([]byte, error) {
	if sameCharset(from, to) {
		return lit, nil
	}

	out := make([]byte, len(lit))

	for i, b := range lit {
		r := from.ToUnicode(b)

		spelled, ok := to.FromUnicode(r)
		if !ok {
			return nil, literalError{
				axis:   "the charset " + to.Name(),
				reason: fmt.Sprintf("since it holds %q and %s has no byte for that character", r, to.Name()),
			}
		}

		out[i] = spelled
	}

	return out, nil
}
