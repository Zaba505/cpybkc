// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package emitted

import (
	"encoding/binary"
	"slices"
)

// reexpressBinary is a literal compared against a binary item — COMP, COMP-4,
// BINARY or COMP-5 — resolved under the byte order from, as a file under the
// byte order to spells it.
//
// By value, because a file under another byte order was written from values by
// a program encoding them its own way rather than converted from this one. The
// width is fixed and the value is the same integer, so the answer is the same
// bytes reversed — which is exact for every bit pattern, including one the
// item's PICTURE does not admit, and is why this is the one axis a literal can
// always cross and the one helper here that returns no error.
//
// The two orders are compared by what they do rather than by value; see
// [sameByteOrder].
func reexpressBinary(lit []byte, from, to binary.ByteOrder) []byte {
	if sameByteOrder(from, to) {
		return lit
	}

	out := slices.Clone(lit)
	slices.Reverse(out)

	return out
}
