// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package emitted

import (
	"fmt"
	"math"
	"strconv"

	"github.com/Zaba505/cobol-go/codec"
)

// reexpressFloat is a literal compared against a floating-point item — COMP-1
// or COMP-2 — resolved under from, as a file under to spells it.
//
// By value, because a file under another float format was written from values
// by a program encoding them in its own format, and nothing a transfer does
// converts one. It is also the only reading that can work: IBM hexadecimal
// floating point spells one value more than one way, and the program that
// wrote the file under to wrote its own spelling (docs/ir/SPEC.md, "Each axis
// carries a literal the way a file crosses it").
//
// Two axes reach a float: its format, and — under IEEE 754 only — the byte
// order, since codec reads HFP big-endian whatever that axis says. Where
// neither moved the literal is carried as it is.
//
// Three things refuse, each before a record is read: bytes that are no number
// under the format they were resolved under, a number to has no bytes for — an
// infinity or a NaN under HFP, or a magnitude past its range — and a number to
// cannot hold exactly. The last is the one that could have been rounded
// instead, and is not: a literal rounded to the nearest number the read format
// holds asks for a value no record under those axes carries there.
func reexpressFloat(lit []byte, from, to codec.Encoding) ([]byte, error) {
	if from.Float == to.Float && (from.Float != codec.FloatIEEE || sameByteOrder(from.ByteOrder, to.ByteOrder)) {
		return lit, nil
	}

	axis := "the floating-point format " + to.Float.String()
	if from.Float == to.Float {
		axis = "the byte order " + fmt.Sprint(to.ByteOrder)
	}

	r, err := codec.NewBytesReader(lit, from)
	if err != nil {
		return nil, literalError{axis: axis, reason: fmt.Sprintf("since the encoding it was resolved under does not validate: %v", err)}
	}

	w, err := codec.NewBytesWriter(nil, to)
	if err != nil {
		return nil, literalError{axis: axis, reason: fmt.Sprintf("since those axes do not validate: %v", err)}
	}

	var value, held string

	switch len(lit) {
	case 4:
		v, err := r.ReadFloat32()
		if err != nil {
			return nil, literalError{axis: axis, reason: fmt.Sprintf("since its bytes are no number a COMP-1 item holds under %s, the format it was resolved under: %v", from.Float, err)}
		}

		if err := w.WriteFloat32(v); err != nil {
			return nil, literalError{axis: axis, reason: fmt.Sprintf("since it holds %s, which %s has no bytes for", floatLiteral(float64(v), 32), to.Float)}
		}

		back, err := codec.NewBytesReader(w.Bytes(), to)
		if err == nil {
			if got, err := back.ReadFloat32(); err == nil && math.Float32bits(got) == math.Float32bits(v) {
				return w.Bytes(), nil
			} else if err == nil {
				value, held = floatLiteral(float64(v), 32), floatLiteral(float64(got), 32)
			}
		}
	case 8:
		v, err := r.ReadFloat64()
		if err != nil {
			return nil, literalError{axis: axis, reason: fmt.Sprintf("since its bytes are no number a COMP-2 item holds under %s, the format it was resolved under: %v", from.Float, err)}
		}

		if err := w.WriteFloat64(v); err != nil {
			return nil, literalError{axis: axis, reason: fmt.Sprintf("since it holds %s, which %s has no bytes for", floatLiteral(v, 64), to.Float)}
		}

		back, err := codec.NewBytesReader(w.Bytes(), to)
		if err == nil {
			if got, err := back.ReadFloat64(); err == nil && math.Float64bits(got) == math.Float64bits(v) {
				return w.Bytes(), nil
			} else if err == nil {
				value, held = floatLiteral(v, 64), floatLiteral(got, 64)
			}
		}
	default:
		return nil, literalError{axis: axis, reason: fmt.Sprintf("since it is %d bytes, and a floating-point item is 4 or 8", len(lit))}
	}

	if held == "" {
		return nil, literalError{axis: axis, reason: "since what it holds does not read back from the bytes " + to.Float.String() + " spells it with"}
	}

	return nil, literalError{axis: axis, reason: fmt.Sprintf("since it holds %s and the nearest number %s holds is %s", value, to.Float, held)}
}

// floatLiteral is a number as a refusal shows it: every digit it needs to be
// told from its neighbours, and no more.
func floatLiteral(v float64, bits int) string {
	return strconv.FormatFloat(v, 'g', -1, bits)
}
