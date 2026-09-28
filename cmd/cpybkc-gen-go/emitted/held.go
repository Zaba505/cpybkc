// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package emitted

import (
	"encoding/binary"
	"sync/atomic"

	"github.com/Zaba505/cobol-go/codec"
)

// reexpression is one re-expression a package has made: the encoding it was
// made under, and what came of it — the literals, or the refusal.
//
// A refusal is held as well as a success. It is a statement about the
// descriptor and the encoding and about nothing a record holds, so the answer
// the second time is the answer the first time, and working it out again per
// record is exactly the per-record re-expression docs/ir/SPEC.md forbids.
type reexpression[T any] struct {
	enc  codec.Encoding
	lits *T
	err  error
}

// reexpressionsKept is how many encodings a package remembers re-expressing
// under.
//
// A process reading one file under one encoding needs one. The number is not a
// performance claim about anything larger — a process juggling more encodings
// than this at once re-expresses again when one falls out, which is correct
// and merely slower — and it is small because every lookup walks it.
const reexpressionsKept = 4

// reexpressedUnder is the literals under enc, re-expressed by under the first
// time a package meets enc and read back from cache every time after.
//
// It exists for the record-level methods. UnmarshalCOBOL and MarshalCOBOL are
// codec's interfaces and take nothing but a codec.Reader or codec.Writer, so
// there is nowhere to hand them literals a caller re-expressed once; what they
// have is the encoding, on every call. A file-level reader and writer
// re-express once, when they are built, and reach this for that one time —
// which is also what puts their encoding here before the first record-level
// call asks.
//
// The cache is a copy-on-write list behind one atomic pointer rather than a map
// behind a lock: an encoding is not a comparable key in general (see
// [sameAxes]), a lookup is on every record of a converted file and a store is
// on the first, and two goroutines that miss together both re-express and both
// store, which costs a repetition and nothing else — the answer is a function
// of the encoding, so either store is the right one.
func reexpressedUnder[T any](cache *atomic.Pointer[[]reexpression[T]], enc codec.Encoding, under func(codec.Encoding) (*T, error)) (*T, error) {
	if known := cache.Load(); known != nil {
		for _, one := range *known {
			if sameAxes(one.enc, enc) {
				return one.lits, one.err
			}
		}
	}

	lits, err := under(enc)

	next := []reexpression[T]{{enc: enc, lits: lits, err: err}}
	if known := cache.Load(); known != nil {
		next = append(next, (*known)[:min(len(*known), reexpressionsKept-1)]...)
	}

	cache.Store(&next)

	return lits, err
}

// sameAxes reports whether two encodings agree on the four axes a layout states
// — charset, sign convention, byte order and float format — which are the four
// a literal is re-expressed through.
//
// The staircase is not compared. It is not an axis a literal moves along
// (docs/ir/SPEC.md, "The staircase is not an axis a consumer may replace"), and
// refusing one that is not the descriptor's is a check of its own, not this.
func sameAxes(a, b codec.Encoding) bool {
	return a.Sign == b.Sign && a.Float == b.Float && sameCharset(a.Charset, b.Charset) && sameByteOrder(a.ByteOrder, b.ByteOrder)
}

// sameCharset reports whether two charsets spell every byte alike.
//
// Compared as values where Go can compare them, which is every charset codec
// ships and almost every other, and by what they do where it cannot. A charset
// is an interface a caller may implement, and Go compares two interface values
// holding the same dynamic type by comparing the values — which panics where
// that type holds a slice, a map or a func, and a caller's charset built around
// a lookup table is exactly that type. Answering "different" for it would
// re-express the literals on every record the record methods read, since those
// ask again on every call; answering with a panic would take down a reader for
// handing it an encoding codec itself accepts. So such a pair is compared by its
// name, its space and the character each of the 256 bytes spells, which is
// everything a re-expression reads of a charset.
func sameCharset(a, b codec.Charset) bool {
	if same, ok := compared(a, b); ok {
		return same
	}

	if a.Name() != b.Name() || a.Space() != b.Space() {
		return false
	}

	for c := range 256 {
		if a.ToUnicode(byte(c)) != b.ToUnicode(byte(c)) {
			return false
		}
	}

	return true
}

// sameByteOrder reports whether two byte orders put the bytes of a value in the
// same order.
//
// By what they do rather than by value, for the reason [sameCharset] falls back
// to it and one more: binary.NativeEndian is a type of its own that agrees with
// one of the other two, and a literal cares only which.
//
// The two are asked to read the same two bytes rather than to write a value,
// because the record methods ask this on every call and a buffer handed to an
// interface method escapes: reading a package-level array allocates nothing.
func sameByteOrder(a, b binary.ByteOrder) bool {
	return a.Uint16(byteOrderProbe[:]) == b.Uint16(byteOrderProbe[:])
}

// byteOrderProbe is the two bytes [sameByteOrder] reads: 1 under a
// little-endian order and 256 under a big-endian one.
var byteOrderProbe = [2]byte{1, 0}

// compared is a == b where Go can compare the two, and ok false where it
// cannot rather than a panic.
func compared(a, b any) (same, ok bool) {
	defer func() {
		if recover() != nil {
			same, ok = false, false
		}
	}()

	return a == b, true
}
