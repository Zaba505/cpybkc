// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The round-trip assertions over the generated methods.
//
// They live inside the golden package rather than beside it, and that is the
// one thing in this directory that is not output. Two of the criteria cannot be
// stated from outside: the bytes retained for a slack node are unexported, so a
// run of the wrong length is something only code in this package can hand a
// writer, and an absent run and an empty one are told apart in a field nobody
// else can set. `written` in record_test.go skips a `_test.go` file for that
// reason, so the golden still holds no hand-written *declaration*.
//
// What they assert is #51's criterion — decode then encode reproduces the
// original bytes — and they assert it on **records**. A file is not
// byte-identical and docs/ir/SPEC.md's "Writing a file" says why; that is #52's
// to test, over framing this package knows nothing about.
//
// The original bytes are laid out here with codec's own accessors rather than
// written out as hex. That is independent of what is under test in the way that
// matters: what these tests exercise is the generated *walk* — which accessor,
// with which width, in which order, and what happens to the bytes no item
// covers — and a hand-written EBCDIC literal would be a second thing to get
// wrong without making the walk any more checked.
package orders

import (
	"bytes"
	"encoding/binary"
	"math/big"
	"strconv"
	"strings"
	"testing"

	"github.com/Zaba505/cobol-go/codec"
)

// ascii is the second of the two encodings these tests read under.
//
// [Encoding] is the descriptor's own and is EBCDIC throughout; this is the same
// records read as a file somebody converted. Both are exercised because neither
// is a property of the generated code: codec carries the four axes on the
// Reader and the Writer, so what the generated walk contributes is the widths
// and the order, and that has to hold under an encoding the descriptor did not
// name.
func ascii() codec.Encoding {
	return codec.Encoding{
		Charset:   codec.ASCII(),
		Sign:      codec.SignASCIIZone37,
		ByteOrder: binary.BigEndian,
		Float:     codec.FloatIEEE,
	}
}

// laidOut is a record's bytes, written item by item.
func laidOut(t *testing.T, enc codec.Encoding, items func(*codec.Writer) error) []byte {
	t.Helper()

	var b bytes.Buffer

	w, err := codec.NewWriter(&b, enc)
	if err != nil {
		t.Fatalf("codec.NewWriter: %v", err)
	}

	if err := items(w); err != nil {
		t.Fatalf("laying the record out: %v", err)
	}

	return b.Bytes()
}

// record is what both generated methods make of a type.
type record interface {
	codec.Unmarshaler
	codec.Marshaler
}

// roundTrip reads in into the record and writes it straight back out.
func roundTrip(t *testing.T, enc codec.Encoding, into record, in []byte) []byte {
	t.Helper()

	r, err := codec.NewReader(bytes.NewReader(in), enc)
	if err != nil {
		t.Fatalf("codec.NewReader: %v", err)
	}

	if err := into.UnmarshalCOBOL(r); err != nil {
		t.Fatalf("UnmarshalCOBOL: %v", err)
	}

	var out bytes.Buffer

	w, err := codec.NewWriter(&out, enc)
	if err != nil {
		t.Fatalf("codec.NewWriter: %v", err)
	}

	if err := into.MarshalCOBOL(w); err != nil {
		t.Fatalf("MarshalCOBOL: %v", err)
	}

	return out.Bytes()
}

// assertBytes is the criterion itself.
func assertBytes(t *testing.T, got, want []byte) {
	t.Helper()

	if !bytes.Equal(got, want) {
		t.Errorf("the record does not write back the bytes it was read from\n got: % x\nwant: % x", got, want)
	}
}

// orderBytes is one ORDER-RECORD: a fixed table, a variable one, and slack at
// two depths.
func orderBytes(t *testing.T, enc codec.Encoding, details int) []byte {
	t.Helper()

	return laidOut(t, enc, func(w *codec.Writer) error {
		if err := w.WriteZonedInt32(12345, 5, codec.SignUnsigned); err != nil {
			return err
		}

		if err := w.WriteAlphanumeric("ACME SUPPLIES", 20); err != nil {
			return err
		}

		// Neither a space nor a zero, so that a writer filling this run rather
		// than emitting what was read is a failure rather than a coincidence.
		if err := w.WriteBytes([]byte{0xab, 0xcd}); err != nil {
			return err
		}

		if err := w.WritePackedInt32(-1234567, 7, codec.Signed); err != nil {
			return err
		}

		for i, sku := range []string{"AAA", "BBB", "CCC"} {
			if err := w.WriteAlphanumeric(sku, 8); err != nil {
				return err
			}

			if err := w.WriteBinaryInt16(int16(i+1), 4, codec.Signed); err != nil {
				return err
			}

			if err := w.WriteBytes([]byte{byte(0xe0 + i)}); err != nil {
				return err
			}
		}

		if err := w.WriteZonedInt32(int32(details), 3, codec.SignUnsigned); err != nil {
			return err
		}

		for i := range details {
			if err := w.WriteAlphanumeric("DETAIL"+strconv.Itoa(i), 10); err != nil {
				return err
			}
		}

		return nil
	})
}

// TestARecordCarryingSlackWritesBackTheBytesItWasReadFrom is #51's criterion
// over the record the golden is built around, under both encodings.
//
// docs/ir/SPEC.md's "Slack survives a read" is what makes this a *byte*
// assertion rather than a field-by-field one: the two runs no item covers are
// neither spaces nor zeros here, so a writer that filled them rather than
// emitting what was retained fails this rather than passing it by luck.
func TestARecordCarryingSlackWritesBackTheBytesItWasReadFrom(t *testing.T) {
	t.Parallel()

	for name, enc := range map[string]codec.Encoding{"EBCDIC": Encoding(), "ASCII": ascii()} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for _, details := range []int{0, 1, 12} {
				want := orderBytes(t, enc, details)

				assertBytes(t, roundTrip(t, enc, &OrderRecord{}, want), want)
			}
		})
	}
}

// TestAFixedLengthRecordWritesBackItsAlignmentGapAndItsPadding is the other two
// things slack is made of, which the IR deliberately does not tell apart: a
// SYNCHRONIZED alignment gap ahead of a binary item, and the tail of a record
// whose items stop short of the dataset's LRECL.
//
// The gap holds bytes nobody wrote and the tail holds whatever the program that
// wrote the file left there, which on these files is spaces. A rule filling
// slack with a constant leaves the first alone and rewrites the second, and
// nothing fails while it happens — which is why this asserts the bytes.
func TestAFixedLengthRecordWritesBackItsAlignmentGapAndItsPadding(t *testing.T) {
	t.Parallel()

	for name, enc := range map[string]codec.Encoding{"EBCDIC": Encoding(), "ASCII": ascii()} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			want := laidOut(t, enc, func(w *codec.Writer) error {
				if err := w.WriteAlphanumeric("Y", 1); err != nil {
					return err
				}

				if err := w.WriteBytes([]byte{0x00, 0x11, 0x22}); err != nil {
					return err
				}

				if err := w.WriteBinaryInt32(123456789, 9, codec.Signed); err != nil {
					return err
				}

				return w.WriteBytes(bytes.Repeat([]byte{enc.Charset.Space()}, 8))
			})

			assertBytes(t, roundTrip(t, enc, &SyncRecord{}, want), want)
		})
	}
}

// TestARecordWithNoLogicalValueForSomeItemsWritesBackTheirBytes is
// TRAILER-RECORD, which carries the three shapes a generator has no number for
// — an item too wide for an int64, a float, and an INDEX the IR derives no
// value for at all — beside a numeric-edited item whose storage is characters.
func TestARecordWithNoLogicalValueForSomeItemsWritesBackTheirBytes(t *testing.T) {
	t.Parallel()

	for name, enc := range map[string]codec.Encoding{"EBCDIC": Encoding(), "ASCII": ascii()} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			total, ok := new(big.Int).SetString("-12345678901234567890", 10)
			if !ok {
				t.Fatal("the twenty-digit total is not a number")
			}

			want := laidOut(t, enc, func(w *codec.Writer) error {
				if err := w.WritePackedBig(total, 20, codec.Signed); err != nil {
					return err
				}

				if err := w.WriteFloat32(1.5); err != nil {
					return err
				}

				if err := w.WriteBytes([]byte{0x01, 0x02, 0x03, 0x04}); err != nil {
					return err
				}

				for range 2 {
					if err := w.WriteAlphanumeric("$1,234.56", 12); err != nil {
						return err
					}
				}

				return nil
			})

			assertBytes(t, roundTrip(t, enc, &TrailerRecord{}, want), want)
		})
	}
}

// tableBytes is one TABLE-RECORD with n occurrences of each of the three tables
// its one count sizes.
func tableBytes(t *testing.T, enc codec.Encoding, n int) []byte {
	t.Helper()

	return laidOut(t, enc, func(w *codec.Writer) error {
		if err := w.WriteZonedInt32(int32(n), 2, codec.SignUnsigned); err != nil {
			return err
		}

		for i := range n {
			if err := w.WriteAlphanumeric("L"+strconv.Itoa(i), 3); err != nil {
				return err
			}
		}

		for i := range n {
			if err := w.WriteAlphanumeric("R"+strconv.Itoa(i), 2); err != nil {
				return err
			}
		}

		for range n {
			for range n {
				if err := w.WriteAlphanumeric("B", 1); err != nil {
					return err
				}
			}
		}

		return nil
	})
}

// TestOneCountSizesEveryTableThatNamesIt is docs/ir/SPEC.md's "One count may
// size two tables, and a writer refuses to choose", from the reading side and
// the ordinary writing side: one field's value sizes three tables, one of which
// is inside a group repeating on that same count, and the record writes back
// the bytes it was read from.
func TestOneCountSizesEveryTableThatNamesIt(t *testing.T) {
	t.Parallel()

	for name, enc := range map[string]codec.Encoding{"EBCDIC": Encoding(), "ASCII": ascii()} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for _, n := range []int{0, 1, 4} {
				want := tableBytes(t, enc, n)

				assertBytes(t, roundTrip(t, enc, &TableRecord{}, want), want)
			}
		})
	}
}

// TestAWriterRefusesTwoNumbersOfOccurrencesForOneCount is the half of that
// section that costs something, and it is the whole of what a writer does about
// it: it reports.
//
// Picking is the shape being refused rather than an ergonomic loss. Either
// number sizes the other table wrong and slides every item behind it, and the
// record the writer's own reader recovers is not the record its caller handed
// over — out of a descriptor saying nothing is wrong.
func TestAWriterRefusesTwoNumbersOfOccurrencesForOneCount(t *testing.T) {
	t.Parallel()

	for name, disagree := range map[string]func(*TableRecord){
		"two tables side by side": func(x *TableRecord) {
			x.RightItem = x.RightItem[:1]
		},
		"a table inside a group repeating on the same count": func(x *TableRecord) {
			// The third occurrence is the one that is wrong, which is why the
			// comparison is over every repetition naming the count rather than
			// over a pair.
			x.Block[2].BlockItem = x.Block[2].BlockItem[:1]
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var x TableRecord

			r, err := codec.NewReader(bytes.NewReader(tableBytes(t, Encoding(), 3)), Encoding())
			if err != nil {
				t.Fatalf("codec.NewReader: %v", err)
			}

			if err := x.UnmarshalCOBOL(r); err != nil {
				t.Fatalf("UnmarshalCOBOL: %v", err)
			}

			disagree(&x)

			var out bytes.Buffer

			w, err := codec.NewWriter(&out, Encoding())
			if err != nil {
				t.Fatalf("codec.NewWriter: %v", err)
			}

			err = x.MarshalCOBOL(w)
			if err == nil {
				t.Fatal("MarshalCOBOL chose one of two numbers of occurrences")
			}

			for _, want := range []string{"TABLE-RECORD", "PAIR-COUNT", "rather than choosing"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal reads %q and does not name %s", err, want)
				}
			}
		})
	}
}

// TestAWriterEmitsACountAsTheNumberOfOccurrencesItWrites is the other half of
// what the descriptor determines: the count field's value *is* the number of
// occurrences, so what the caller left in the field is not what is emitted.
func TestAWriterEmitsACountAsTheNumberOfOccurrencesItWrites(t *testing.T) {
	t.Parallel()

	want := tableBytes(t, Encoding(), 2)

	var x TableRecord

	r, err := codec.NewReader(bytes.NewReader(want), Encoding())
	if err != nil {
		t.Fatalf("codec.NewReader: %v", err)
	}

	if err := x.UnmarshalCOBOL(r); err != nil {
		t.Fatalf("UnmarshalCOBOL: %v", err)
	}

	// A count disagreeing with what follows it is a record the writer's own
	// reader cannot walk, so this is overwritten rather than emitted.
	x.PairCount = 9

	var out bytes.Buffer

	w, err := codec.NewWriter(&out, Encoding())
	if err != nil {
		t.Fatalf("codec.NewWriter: %v", err)
	}

	if err := x.MarshalCOBOL(w); err != nil {
		t.Fatalf("MarshalCOBOL: %v", err)
	}

	assertBytes(t, out.Bytes(), want)
}

// TestACountOutsideItsDeclaredBoundsIsReported is the check every repetition
// naming a count makes against it, on both sides of the call.
func TestACountOutsideItsDeclaredBoundsIsReported(t *testing.T) {
	t.Parallel()

	t.Run("reading", func(t *testing.T) {
		t.Parallel()

		// Five occurrences declared where four is the maximum. The bytes behind
		// the count are the ones a four-occurrence record carries, so what is
		// wrong is the count and nothing else.
		in := tableBytes(t, Encoding(), 4)
		in[1] = laidOut(t, Encoding(), func(w *codec.Writer) error {
			return w.WriteZonedInt32(5, 2, codec.SignUnsigned)
		})[1]

		r, err := codec.NewReader(bytes.NewReader(in), Encoding())
		if err != nil {
			t.Fatalf("codec.NewReader: %v", err)
		}

		if err := (&TableRecord{}).UnmarshalCOBOL(r); err == nil {
			t.Error("UnmarshalCOBOL read a count outside the bounds every repetition naming it declares")
		}
	})

	t.Run("writing", func(t *testing.T) {
		t.Parallel()

		x := TableRecord{
			LeftItem:  make([]struct{ LeftText string }, 5),
			RightItem: make([]struct{ RightText string }, 5),
		}

		var out bytes.Buffer

		w, err := codec.NewWriter(&out, Encoding())
		if err != nil {
			t.Fatalf("codec.NewWriter: %v", err)
		}

		if err := x.MarshalCOBOL(w); err == nil {
			t.Error("MarshalCOBOL wrote a count its own reader is required to call malformed data")
		}
	})
}

// entryBytes is one ENTRY-RECORD whose three entries take the arms named.
//
// EBCDIC only, and deliberately: an arm's predicate compares the bytes the
// producer resolved, so the type code is 0xC4 and 0xE2 rather than a character.
// Reading these records under an ASCII encoding is reading a file that is not
// the file the descriptor describes, which is the axis that has no default for
// exactly this reason.
func entryBytes(t *testing.T, arms string) []byte {
	t.Helper()

	return laidOut(t, Encoding(), func(w *codec.Writer) error {
		for i, code := range arms {
			if err := w.WriteAlphanumeric(string(code), 1); err != nil {
				return err
			}

			if code == 'D' {
				if err := w.WriteAlphanumeric("SKU", 4); err != nil {
					return err
				}

				if err := w.WriteBinaryInt16(int16(i+1), 4, codec.Signed); err != nil {
					return err
				}

				continue
			}

			if err := w.WriteAlphanumeric("SUM", 4); err != nil {
				return err
			}

			// The tail of the shorter alternative, which holds the *other*
			// alternative's data — laid down by a program that used it, on a
			// record the one being read does not describe. Zero-filling it
			// destroys a payload no check in this reader or this writer would
			// report.
			if err := w.WriteBytes([]byte{byte(0x90 + i), byte(0xa0 + i)}); err != nil {
				return err
			}
		}

		return nil
	})
}

// TestATableOfVariantsWritesBackTheBytesItWasReadFrom is docs/ir/SPEC.md's "A
// variant is chosen once per occurrence" over a whole record: three entries, of
// which the middle one takes the other arm, and the arm that is shorter than
// its sibling carries the slack that makes the two cover the same bytes.
func TestATableOfVariantsWritesBackTheBytesItWasReadFrom(t *testing.T) {
	t.Parallel()

	for _, arms := range []string{"DDD", "DSD", "SSS", "SDS"} {
		want := entryBytes(t, arms)

		var x EntryRecord

		assertBytes(t, roundTrip(t, Encoding(), &x, want), want)

		for i, code := range arms {
			held := x.Entry[i]

			if (code == 'D') != (held.EntryDetail != nil) || (code == 'S') != (held.EntrySummary != nil) {
				t.Errorf("entry %d of %s holds detail=%v summary=%v", i, arms, held.EntryDetail != nil, held.EntrySummary != nil)
			}
		}
	}
}

// TestAnOccurrenceMatchingNoArmIsReportedAsItsOwnFailure is the rule that has
// no default arm, and the half of it that is easy to lose: the message.
//
// A record no transition matched is a record type the layout is missing; an
// occurrence no arm matched is a record the layout does describe carrying an
// entry it does not. An adopter sent to the first for the second spends the day
// on a record type they already have.
func TestAnOccurrenceMatchingNoArmIsReportedAsItsOwnFailure(t *testing.T) {
	t.Parallel()

	in := entryBytes(t, "DDD")

	// The second entry's type code, which no arm's predicate admits.
	in[7] = 0x5b

	r, err := codec.NewReader(bytes.NewReader(in), Encoding())
	if err != nil {
		t.Fatalf("codec.NewReader: %v", err)
	}

	err = (&EntryRecord{}).UnmarshalCOBOL(r)
	if err == nil {
		t.Fatal("UnmarshalCOBOL fell through to an arm for an occurrence no predicate selected")
	}

	for _, want := range []string{
		// Which record, which table, and which entry of it.
		"ENTRY-RECORD",
		"occurrence 1 of ENTRY",

		// And that this is a record type the layout has, so that it is not read
		// as the other failure.
		"the record type is one the layout describes",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the report reads %q and does not say %q", err, want)
		}
	}
}

// TestAWriterEvaluatesAnArmsPredicateAndNeverInvertsOne is the writing side of
// the same rule.
//
// The value an arm's predicate tests is the caller's. A writer that derived a
// satisfying value and stored it into the predicate's target would discard data
// the caller meant, so it checks and reports instead — and an occurrence
// satisfying no arm's predicate is reported rather than emitted.
func TestAWriterEvaluatesAnArmsPredicateAndNeverInvertsOne(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		break_ func(*EntryRecord)
		says   string
	}{
		"a type code no arm admits": {
			break_: func(x *EntryRecord) { x.Entry[1].EntryType = "?" },
			says:   "satisfying no arm's predicate",
		},
		"the arm the caller supplied is not the one its values select": {
			break_: func(x *EntryRecord) {
				x.Entry[1].EntrySummary = &struct {
					SummaryText string
					slack       [1][]byte
				}{SummaryText: "SUM"}
				x.Entry[1].EntryDetail = nil
			},
			says: "never derives a value satisfying one",
		},
		"an occurrence holding no arm at all": {
			break_: func(x *EntryRecord) { x.Entry[1].EntryDetail = nil },
			says:   "holds exactly one arm",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var x EntryRecord

			r, err := codec.NewReader(bytes.NewReader(entryBytes(t, "DDD")), Encoding())
			if err != nil {
				t.Fatalf("codec.NewReader: %v", err)
			}

			if err := x.UnmarshalCOBOL(r); err != nil {
				t.Fatalf("UnmarshalCOBOL: %v", err)
			}

			tc.break_(&x)

			var out bytes.Buffer

			w, err := codec.NewWriter(&out, Encoding())
			if err != nil {
				t.Fatalf("codec.NewWriter: %v", err)
			}

			err = x.MarshalCOBOL(w)
			if err == nil {
				t.Fatal("MarshalCOBOL emitted an occurrence its own reader could not walk")
			}

			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("the report reads %q and does not say %q", err, tc.says)
			}

			if !strings.Contains(err.Error(), "occurrence 1 of ENTRY") {
				t.Errorf("the report reads %q and does not say which occurrence it is about", err)
			}
		})
	}
}

// TestARecordTheCallerBuiltEmitsZeroBytesForItsSlack is the one place a writer
// invents bytes, and it is confined to bytes that were never in a file.
//
// A caller that builds a record out of a read one's values hands a writer a
// record carrying nothing, which is the ordinary shape of a job that transforms
// rather than edits.
func TestARecordTheCallerBuiltEmitsZeroBytesForItsSlack(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	w, err := codec.NewWriter(&out, Encoding())
	if err != nil {
		t.Fatalf("codec.NewWriter: %v", err)
	}

	if err := (&SyncRecord{SyncFlag: "Y"}).MarshalCOBOL(w); err != nil {
		t.Fatalf("MarshalCOBOL: %v", err)
	}

	want := laidOut(t, Encoding(), func(w *codec.Writer) error {
		if err := w.WriteAlphanumeric("Y", 1); err != nil {
			return err
		}

		if err := w.WriteBytes(make([]byte, 3)); err != nil {
			return err
		}

		if err := w.WriteBinaryInt32(0, 9, codec.Signed); err != nil {
			return err
		}

		return w.WriteBytes(make([]byte, 8))
	})

	assertBytes(t, out.Bytes(), want)
}

// TestARetainedRunOfTheWrongLengthIsReported is the rest of that rule, and the
// reason these tests are inside the package: a run of no bytes is a wrong
// length rather than an absent one, and the two are told apart in a field a
// caller cannot reach.
func TestARetainedRunOfTheWrongLengthIsReported(t *testing.T) {
	t.Parallel()

	for name, run := range map[string][]byte{
		"a run of no bytes":  {},
		"a run cut short":    {0x01},
		"a run run together": {0x01, 0x02, 0x03, 0x04},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			x := SyncRecord{SyncFlag: "Y"}
			x.slack[0] = run

			var out bytes.Buffer

			w, err := codec.NewWriter(&out, Encoding())
			if err != nil {
				t.Fatalf("codec.NewWriter: %v", err)
			}

			err = x.MarshalCOBOL(w)
			if err == nil {
				t.Fatal("MarshalCOBOL truncated or padded a retained run to fit")
			}

			if !strings.Contains(err.Error(), "rather than truncating or padding") {
				t.Errorf("the report reads %q and does not say what it refused to do", err)
			}
		})
	}
}

// TestTheRetainedBytesAreACopyAndNotAWindow is the lifetime half of "Slack
// survives a read", which conforms to every other rule when it is broken.
//
// A reader that kept a window onto the buffer it read into would satisfy
// "retain" and then hand every record the *next* record's slack, and no width
// check and no framing check can see it. So a record is held across a
// subsequent read and then written: a test that wrote each record inside the
// iteration that read it could not tell the difference.
func TestTheRetainedBytesAreACopyAndNotAWindow(t *testing.T) {
	t.Parallel()

	first := orderBytes(t, Encoding(), 1)

	// A second record whose every slack byte differs from the first's.
	second := append([]byte(nil), first...)
	for i, b := range second {
		if b == 0xab || b == 0xcd {
			second[i] = b ^ 0xff
		}
	}

	r, err := codec.NewReader(bytes.NewReader(append(append([]byte(nil), first...), second...)), Encoding())
	if err != nil {
		t.Fatalf("codec.NewReader: %v", err)
	}

	var held, next OrderRecord

	if err := held.UnmarshalCOBOL(r); err != nil {
		t.Fatalf("UnmarshalCOBOL: %v", err)
	}

	if err := next.UnmarshalCOBOL(r); err != nil {
		t.Fatalf("UnmarshalCOBOL: %v", err)
	}

	var out bytes.Buffer

	w, err := codec.NewWriter(&out, Encoding())
	if err != nil {
		t.Fatalf("codec.NewWriter: %v", err)
	}

	if err := held.MarshalCOBOL(w); err != nil {
		t.Fatalf("MarshalCOBOL: %v", err)
	}

	assertBytes(t, out.Bytes(), first)
}
