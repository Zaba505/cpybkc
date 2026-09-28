// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package emitted

import (
	"fmt"

	"github.com/Zaba505/cobol-go/codec"
)

// reexpressZoned is a literal compared against a zoned decimal item, resolved
// under from, as a file under to spells it.
//
// signAt is the byte of the literal that carries an overpunched sign — the
// first under SIGN LEADING, the last under SIGN TRAILING — and -1 where the
// item carries none: an unsigned item, and one whose sign is SEPARATE, which is
// a character like any other.
//
// Two axes govern a zoned item and they do not compete for a byte
// (docs/ir/SPEC.md, "Each axis carries a literal the way a file crosses it").
// The sign-carrying byte is the sign convention's, whose table spells the whole
// byte; every other byte is a digit, or the character a file leaves in a blank
// numeric field, and is the charset's. So the charset moves every byte but that
// one, character by character as [reexpressText] moves text, and the sign
// convention moves that one by its column.
//
// Each axis moves only where it changed. A literal survives a charset change
// with its sign byte untouched and a sign change with its digits untouched, and
// neither is refused over an axis that did not move.
func reexpressZoned(lit []byte, from, to codec.Encoding, signAt int) ([]byte, error) {
	out := make([]byte, len(lit))
	copy(out, lit)

	if !sameCharset(from.Charset, to.Charset) {
		for i := range lit {
			if i == signAt {
				continue
			}

			spelled, err := reexpressText(lit[i:i+1], from.Charset, to.Charset)
			if err != nil {
				return nil, err
			}

			out[i] = spelled[0]
		}
	}

	if signAt < 0 || from.Sign == to.Sign {
		return out, nil
	}

	b := lit[signAt]

	digit, column, ok := signColumnsOf(from).find(b)
	if !ok {
		return nil, literalError{
			axis: "the sign convention " + to.Sign.String(),
			reason: fmt.Sprintf("since its sign byte, 0x%02X, is in none of the %s convention's columns: it is no digit carrying a sign that a writer under %s emits, so there is nothing to carry into another",
				b, from.Sign, from.Sign),
		}
	}

	spelled, ok := signColumnsOf(to).at(digit, column)
	if !ok {
		return nil, literalError{
			axis:   "the sign convention " + to.Sign.String(),
			reason: fmt.Sprintf("since its sign byte is digit %d in the %s column, and that convention spells no byte for digit %d in that column under the charset %s", digit, column, digit, to.Charset.Name()),
		}
	}

	out[signAt] = spelled

	return out, nil
}

// signColumn is which of a sign convention's three columns a sign-carrying
// byte is in: the byte a writer puts on a positive signed item, the one it puts
// on a negative one, or the plain digit a writer puts on an unsigned item and a
// reader of a signed one takes as non-negative.
type signColumn int

const (
	positiveColumn signColumn = iota
	negativeColumn
	unsignedColumn
)

// String names a column for a refusal.
func (c signColumn) String() string {
	switch c {
	case positiveColumn:
		return "positive"
	case negativeColumn:
		return "negative"
	default:
		return "unsigned"
	}
}

// signColumns is the three columns of one encoding's sign convention, as the
// bytes codec reads and writes under it: signColumns[column][digit], and -1
// where the column spells no byte for that digit.
//
// # Why the column and not the value
//
// A signed zoned item reads both F5 and C5 as +5 under the EBCDIC convention,
// and a copybook-aware transfer to ASCII writes 35 for the one and 45 for the
// other: two bytes, because they were two bytes before. Re-expressing through
// the value would ask for 45 for both, and a `one of` holding the two would
// collapse into one literal. The column keeps what the file keeps, and it does
// it from the conventions' own tables rather than from a transfer, so it lands
// the same way on a file no transfer produced (docs/ir/SPEC.md, "Each axis
// carries a literal the way a file crosses it").
//
// # Why they are asked of codec rather than written down
//
// The tables are codec/SPEC.md's, and codec does not export them. They are
// asked of it instead, through the accessors every generated package already
// calls: the positive and negative columns are the sign byte codec writes for
// +1d and -1d — two digits, so that the negative zero the trailing byte of -10
// carries is reachable, which no one-digit value is — and the unsigned column
// is the charset's own digit wherever codec reads that digit, in a signed
// item's sign position, as non-negative. That last is the one column codec
// never writes into a signed item, and it is the column the EBCDIC F zone and
// the translated-EBCDIC 3 zone sit in. Where a convention reads the digit as
// positive — zone 3 under ascii-zone-3-7 and realia — the two columns spell one
// byte, and [signColumns.find] reads it as positive, which is what a writer
// under that convention means by it on a signed item.
type signColumns [3][10]int

// signColumnsOf is the columns of enc's sign convention under enc's charset.
//
// Nothing here can fail for an encoding a reader or a writer was built with,
// because building one validated it; an entry codec will not produce is -1
// rather than an error, and it is a refusal only if a literal needs it.
func signColumnsOf(enc codec.Encoding) signColumns {
	var cols signColumns

	for c := range cols {
		for d := range cols[c] {
			cols[c][d] = -1
		}
	}

	w, err := codec.NewBytesWriter(nil, enc)
	if err != nil {
		return cols
	}

	for d := range 10 {
		for c, sign := range [2]int32{1, -1} {
			w.Reset(nil)

			if err := w.WriteZonedInt32(sign*int32(10+d), 2, codec.SignTrailing); err != nil {
				continue
			}

			cols[c][d] = int(w.Bytes()[1])
		}
	}

	one, ok := enc.Charset.FromUnicode('1')
	if !ok {
		return cols
	}

	for d := range 10 {
		digit, ok := enc.Charset.FromUnicode(rune('0' + d))
		if !ok {
			continue
		}

		r, err := codec.NewBytesReader([]byte{one, digit}, enc)
		if err != nil {
			continue
		}

		if v, err := r.ReadZonedInt32(2, codec.SignTrailing); err == nil && v == int32(10+d) {
			cols[unsignedColumn][d] = int(digit)
		}
	}

	return cols
}

// find is the digit and the column the byte b spells in, searched positive
// first, then negative, then unsigned — so that a byte two columns share is
// read as the column a writer emits.
func (cols signColumns) find(b byte) (byte, signColumn, bool) {
	for c := range cols {
		for d, spelled := range cols[c] {
			if spelled == int(b) {
				return byte(d), signColumn(c), true
			}
		}
	}

	return 0, 0, false
}

// at is the byte that spells digit d in column c.
func (cols signColumns) at(d byte, c signColumn) (byte, bool) {
	spelled := cols[c][d]

	return byte(spelled), spelled >= 0
}
