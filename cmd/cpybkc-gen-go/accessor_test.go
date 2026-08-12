// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"errors"
	"testing"

	// Aliased because binary is a helper of this package's tests: the item this
	// file is about is what a copybook calls BINARY.
	order "encoding/binary"

	"github.com/Zaba505/cobol-go/codec"
)

// unsignedEncoding is the four axes the vectors below are read under.
//
// Byte order is the only one of the four a binary item is sensitive to, and it
// is big-endian here because that is the column Appendix A's FF FF row is in.
// The other three are stated because none of them has a default.
func unsignedEncoding() codec.Encoding {
	return codec.Encoding{
		Charset:   codec.ASCII(),
		Sign:      codec.SignASCIIZone37,
		ByteOrder: order.BigEndian,
		Float:     codec.FloatIEEE,
	}
}

// TestTheTwoReadingsOfTheSameBytesDifferByTheAccessorAlone is the premise the
// binary family selection rests on, pinned against the bytes it is about.
//
// cobol-go's codec documents that FF FF is 65535 read as an unsigned two-byte
// item and -1 read as a signed one, and that the difference is not recoverable
// from the bytes: which accessor is called is what says which the copybook
// declared. This generator's job is to call the one the PICTURE named, so this
// test is what makes that job have a consequence — the two readings are shown
// to differ over one file, rather than being asserted to in prose.
//
// It exercises codec rather than generated code because it is about the
// accessors, and which accessor a generated package calls is
// TestABinaryItemsSignSelectsTheAccessorAndNotJustItsArgument's. The corpus
// entry binary-unsigned-comp5 is the two of them joined up, through a generated
// package that is compiled and run.
func TestTheTwoReadingsOfTheSameBytesDifferByTheAccessorAlone(t *testing.T) {
	t.Parallel()

	stored := []byte{0xFF, 0xFF}

	// The unsigned reading: PIC 9(4) COMP-5, which is what the copybook that
	// stored 65535 in two bytes declared.
	r, err := codec.NewReader(bytes.NewReader(stored), unsignedEncoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	unsigned, err := r.ReadComp5Uint64(4)
	if err != nil {
		t.Fatalf("ReadComp5Uint64(4): %v", err)
	}

	if unsigned != 65535 {
		t.Errorf("PIC 9(4) COMP-5 over FF FF is %d, want 65535", unsigned)
	}

	// The signed reading of the very same bytes, which is the accessor this
	// generator used to emit for both.
	r, err = codec.NewReader(bytes.NewReader(stored), unsignedEncoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	signed, err := r.ReadComp5Int16(4)
	if err != nil {
		t.Fatalf("ReadComp5Int16(4): %v", err)
	}

	if signed != -1 {
		t.Errorf("PIC S9(4) COMP-5 over FF FF is %d, want -1", signed)
	}

	if uint64(signed) == unsigned {
		t.Error("the two readings of FF FF agree, and the whole point of choosing between them is that they do not")
	}
}

// TestAnUnsignedComp5ItemRoundTripsAtTheTopOfItsRange is the value going back
// into the bytes it came from.
//
// Reading 65535 correctly is half of it: a record read and written back has to
// produce the same field, and it does so only where the writer takes the value
// in the type the reader returned. That is why an unsigned binary item's Go
// type is uint64 on both sides rather than the int16 the digit count alone
// would have given it — an int16 has nowhere to put the value.
func TestAnUnsignedComp5ItemRoundTripsAtTheTopOfItsRange(t *testing.T) {
	t.Parallel()

	stored := []byte{0xFF, 0xFF}

	r, err := codec.NewReader(bytes.NewReader(stored), unsignedEncoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	held, err := r.ReadComp5Uint64(4)
	if err != nil {
		t.Fatalf("ReadComp5Uint64(4): %v", err)
	}

	var out bytes.Buffer

	w, err := codec.NewWriter(&out, unsignedEncoding())
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	if err := w.WriteComp5Uint64(held, 4, codec.Unsigned); err != nil {
		t.Fatalf("WriteComp5Uint64(%d, 4, codec.Unsigned): %v", held, err)
	}

	if !bytes.Equal(out.Bytes(), stored) {
		t.Errorf("65535 written back is % X, want % X", out.Bytes(), stored)
	}
}

// TestTheTwoPackedReadingsOfTheSameBytesDifferByTheAccessorAlone is the COMP-6
// premise: the same bytes, the two accessors, and a disagreement about the value
// *and* about how many bytes were consumed.
//
// A COMP-6 item is packed with no sign nibble, so `PIC 9(4) COMP-6` is two bytes
// where `PIC 9(4) COMP-3` is three. The record here is that item holding 234
// followed by a `PIC S9(3) COMP-3` item holding +45 — six digits of data in
// three bytes — and the packed reading of the first item swallows the second
// one's byte and returns a number built out of both.
//
// The leading digit is zero on purpose. A non-zero one puts a digit in the
// nibble a packed reader checks for zero padding, and the substitution then
// fails loudly on the first field it is tried on; codec/SPEC.md's "COMP-6" is
// explicit that this is luck rather than a guarantee. What needs pinning is the
// half that is not loud, because a reader that is only wrong sometimes is a
// reader nobody catches.
func TestTheTwoPackedReadingsOfTheSameBytesDifferByTheAccessorAlone(t *testing.T) {
	t.Parallel()

	stored := []byte{0x02, 0x34, 0x5C}

	r, err := codec.NewReader(bytes.NewReader(stored), unsignedEncoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	held, err := r.ReadComp6Int32(4)
	if err != nil {
		t.Fatalf("ReadComp6Int32(4): %v", err)
	}

	if held != 234 {
		t.Errorf("PIC 9(4) COMP-6 over 02 34 is %d, want 234", held)
	}

	if r.Offset() != 2 {
		t.Errorf("PIC 9(4) COMP-6 consumed %d bytes, want the 2 the item occupies", r.Offset())
	}

	// The same bytes through the accessor this generator used to emit for both
	// usages.
	r, err = codec.NewReader(bytes.NewReader(stored), unsignedEncoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	packed, err := r.ReadPackedInt32(4)
	if err != nil {
		t.Fatalf("ReadPackedInt32(4): %v", err)
	}

	if packed == held {
		t.Errorf("both accessors read %d, and the whole point of choosing between them is that they do not", held)
	}

	if packed != 2345 {
		t.Errorf("PIC 9(4) COMP-3 over 02 34 5C is %d, want the 2345 it builds out of two items", packed)
	}

	if r.Offset() != 3 {
		t.Errorf("the packed accessor consumed %d bytes of a two-byte item, want 3", r.Offset())
	}
}

// TestAComp6ItemRoundTripsByteIdentically is the value going back into the
// bytes it came from, over both parities of the digit count.
//
// Byte identity is the assertion rather than value equality because a COMP-6
// field has a nibble in it that no value can be read out of: the pad nibble an
// odd digit count carries. A writer that filled it with anything but zero would
// round-trip every value correctly and still write a file the next reader
// refuses.
func TestAComp6ItemRoundTripsByteIdentically(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		digits int
		stored []byte
		want   int32
	}{
		// Appendix A.4's two COMP-6 rows.
		"an even digit count fills its bytes exactly": {digits: 4, stored: []byte{0x12, 0x34}, want: 1234},
		"an odd digit count carries a pad nibble":     {digits: 3, stored: []byte{0x01, 0x23}, want: 123},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r, err := codec.NewReader(bytes.NewReader(tc.stored), unsignedEncoding())
			if err != nil {
				t.Fatalf("NewReader: %v", err)
			}

			held, err := r.ReadComp6Int32(tc.digits)
			if err != nil {
				t.Fatalf("ReadComp6Int32(%d): %v", tc.digits, err)
			}

			if held != tc.want {
				t.Errorf("% X read as %d digits is %d, want %d", tc.stored, tc.digits, held, tc.want)
			}

			var out bytes.Buffer

			w, err := codec.NewWriter(&out, unsignedEncoding())
			if err != nil {
				t.Fatalf("NewWriter: %v", err)
			}

			if err := w.WriteComp6Int32(held, tc.digits); err != nil {
				t.Fatalf("WriteComp6Int32(%d, %d): %v", held, tc.digits, err)
			}

			if !bytes.Equal(out.Bytes(), tc.stored) {
				t.Errorf("%d written back is % X, want % X", held, out.Bytes(), tc.stored)
			}
		})
	}
}

// TestAnUnsignedFourDigitCompItemIsOutOfRangeAtTheTopOfTwoBytes is the other
// half of why COMP-5 is the usage the corpus entry declares.
//
// TRUNC(STD) confines a COMP item to the decimal range of its PICTURE, and
// 65535 is outside four decimal digits however the item is signed. So the
// unsigned accessor is necessary for that value and not sufficient: the row it
// comes from is a TRUNC(BIN) row, and reading it needs the COMP-5 accessor too.
func TestAnUnsignedFourDigitCompItemIsOutOfRangeAtTheTopOfTwoBytes(t *testing.T) {
	t.Parallel()

	r, err := codec.NewReader(bytes.NewReader([]byte{0xFF, 0xFF}), unsignedEncoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	if _, err := r.ReadBinaryUint64(4); err == nil {
		t.Error("PIC 9(4) COMP read FF FF under TRUNC(STD), where 65535 is outside four decimal digits")
	} else {
		var out codec.BinaryRangeError
		if !errors.As(err, &out) {
			t.Errorf("ReadBinaryUint64(4) over FF FF is %v, want a BinaryRangeError", err)
		}
	}
}
