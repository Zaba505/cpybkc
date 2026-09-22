// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The round-trip assertions over the code cpybkc generated for ../claim.sexpr.
//
// They live *inside* the generated package rather than beside it, for the
// reason `ledger/`'s do: a `_test.go` file is not part of the package, so it is
// not output and the regeneration test does not hold it against anything, and
// the assertion this example exists for cannot be stated from outside — a
// pharmacy line redefines its body six bytes short, so the bytes it retains for
// that run sit in an unexported field, one set of them per occurrence.
//
// What the generated tests already assert is one line of each kind. What is
// here is what only a hand-written test can say: that the choice is made once
// per line rather than once per record, that a claim of nine lines and a claim
// of two put the items behind the table in different places, and that every
// pharmacy line of one claim carries back its own six bytes rather than the
// first line's six repeated.
package claim

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Zaba505/cobol-go/codec"
)

// framed is one record behind the record descriptor word DFSMS defines, which
// is what `(recfm VB)` resolves to: two bytes of length counting the word
// itself, big-endian, and two reserved bytes that are zero.
func framed(raw []byte) []byte {
	stated := len(raw) + 4

	return append([]byte{byte(stated >> 8), byte(stated), 0, 0}, raw...)
}

// laidOut is the bytes of one record, written item by item through codec
// itself rather than through this package, so that what the generated methods
// are held against is not their own output.
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

// line is one occurrence of CLM-LINE: the kind code, the charge, and twenty
// bytes of body whichever description covers them.
type line struct {
	kind   string
	charge int32
	body   func(*codec.Writer) error
}

// professional is a CLN-PROFESSIONAL line, the description the copybook gives
// the run first and the one most of a claim's lines take.
func professional(provider, procedure, modifier string, units int32) line {
	return line{
		kind:   "P",
		charge: 1250,
		body: func(w *codec.Writer) error {
			if err := w.WriteAlphanumeric(provider, 10); err != nil {
				return err
			}

			if err := w.WriteAlphanumeric(procedure, 5); err != nil {
				return err
			}

			if err := w.WriteAlphanumeric(modifier, 2); err != nil {
				return err
			}

			return w.WriteZonedInt32(units, 3, codec.SignUnsigned)
		},
	}
}

// pharmacy is a CLN-PHARMACY line. It describes fourteen of the twenty bytes
// the run holds, so the six behind them are slack, and they are written here as
// bytes no item of the record describes.
//
// They are deliberately not spaces and deliberately different per line: a run
// that survives a read is only visibly surviving if it is not the padding a
// writer would have chosen, and a run retained *per occurrence* is only
// visibly per-occurrence if the second line's six differ from the first's.
func pharmacy(ndc string, days int32, slack []byte) line {
	return line{
		kind:   "D",
		charge: 4899,
		body: func(w *codec.Writer) error {
			if err := w.WriteAlphanumeric(ndc, 11); err != nil {
				return err
			}

			if err := w.WriteZonedInt32(days, 3, codec.SignUnsigned); err != nil {
				return err
			}

			return w.WriteBytes(slack)
		},
	}
}

// facility is a CLN-FACILITY line. kind is a parameter because two codes select
// this one description — `F` outpatient and `I` inpatient — which is what the
// arm's `one-of` says, and because a caller below writes a code that selects
// nothing.
func facility(kind, name, revenue, billType string, days int32) line {
	return line{
		kind:   kind,
		charge: 98750,
		body: func(w *codec.Writer) error {
			if err := w.WriteAlphanumeric(name, 10); err != nil {
				return err
			}

			if err := w.WriteAlphanumeric(revenue, 4); err != nil {
				return err
			}

			if err := w.WriteAlphanumeric(billType, 3); err != nil {
				return err
			}

			return w.WriteZonedInt32(days, 3, codec.SignUnsigned)
		},
	}
}

// claimBytes is one CLAIM-RECORD carrying the lines it was given.
//
// The count is written from the number of lines rather than passed in, which is
// what an OCCURS DEPENDING ON means on the writing side: the field is the
// descriptor's and not the caller's.
func claimBytes(t *testing.T, enc codec.Encoding, number string, lines ...line) []byte {
	t.Helper()

	return laidOut(t, enc, func(w *codec.Writer) error {
		if err := w.WriteAlphanumeric("CL", 2); err != nil {
			return err
		}

		if err := w.WriteAlphanumeric(number, 12); err != nil {
			return err
		}

		if err := w.WriteAlphanumeric("M000123456", 10); err != nil {
			return err
		}

		if err := w.WriteZonedInt32(int32(len(lines)), 1, codec.SignUnsigned); err != nil {
			return err
		}

		for _, l := range lines {
			if err := w.WriteAlphanumeric(l.kind, 1); err != nil {
				return err
			}

			if err := w.WritePackedInt32(l.charge, 9, codec.Signed); err != nil {
				return err
			}

			if err := l.body(w); err != nil {
				return err
			}
		}

		if err := w.WritePackedInt64(104899, 11, codec.Signed); err != nil {
			return err
		}

		return w.WriteAlphanumeric("A", 1)
	})
}

// slackOf is the six bytes the nth pharmacy line of the file below carries, and
// they are distinct per line so that a writer that carried the first line's run
// into every other one would be caught rather than merely suspected.
func slackOf(n byte) []byte {
	return []byte{0xe0 + n, 0xe1 + n, 0xe2 + n, 0xe3 + n, 0xe4 + n, 0xe5 + n}
}

// fileBytes is a whole claims extract: a claim of five lines mixing every
// description the copybook gives the run, and a claim of two, so that the items
// behind the table begin at a different offset in each.
func fileBytes(t *testing.T) []byte {
	t.Helper()

	enc := Encoding()

	var b bytes.Buffer

	b.Write(framed(claimBytes(t, enc, "CLM000000001",
		professional("PRV0000001", "99213", "25", 1),
		pharmacy("00093015001", 30, slackOf(0)),
		facility("F", "FAC0000001", "0450", "131", 0),
		pharmacy("55111012366", 90, slackOf(1)),
		facility("I", "FAC0000002", "0120", "111", 4),
	)))

	b.Write(framed(claimBytes(t, enc, "CLM000000002",
		professional("PRV0000002", "99214", "  ", 2),
		pharmacy("00378180177", 14, slackOf(2)),
	)))

	return b.Bytes()
}

// read is every record of in, through the generated reader.
func read(t *testing.T, enc codec.Encoding, in []byte) []Record {
	t.Helper()

	r, err := NewReader(bytes.NewReader(in), enc)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	var out []Record

	for {
		rec, err := r.Next()
		if errors.Is(err, io.EOF) {
			return out
		}

		if err != nil {
			t.Fatalf("Next: %v", err)
		}

		out = append(out, rec)
	}
}

// write is every record written back out, through the generated writer.
func write(t *testing.T, enc codec.Encoding, records []Record) []byte {
	t.Helper()

	var b bytes.Buffer

	w, err := NewWriter(&b, enc)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	for _, rec := range records {
		if err := w.Write(rec); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	return b.Bytes()
}

// claims is the file's records as CLAIM-RECORDs, since one record type is all
// this layout describes.
func claims(t *testing.T, records []Record) []*ClaimRecord {
	t.Helper()

	out := make([]*ClaimRecord, 0, len(records))

	for i, rec := range records {
		claim, ok := rec.(*ClaimRecord)
		if !ok {
			t.Fatalf("record %d is a %T, want a *ClaimRecord", i+1, rec)
		}

		out = append(out, claim)
	}

	return out
}

// TestAFileOfClaimsReadsBackAsTheFileItWas is the claim the worked example is
// for: a file whose lines take every description the copybook gives the
// redefined run reads to records and writes back to the bytes it came from.
//
// Bytes rather than values, and a whole file rather than a record, because the
// framing is on the path too: a record descriptor word states the length of the
// record behind it, and under `odoslide` that length is a different number for
// a claim of five lines and a claim of two.
func TestAFileOfClaimsReadsBackAsTheFileItWas(t *testing.T) {
	t.Parallel()

	want := fileBytes(t)

	records := read(t, Encoding(), want)
	if len(records) != 2 {
		t.Fatalf("the file holds two claims and the reader produced %d", len(records))
	}

	got := write(t, Encoding(), records)
	if !bytes.Equal(got, want) {
		t.Errorf("the file does not write back the bytes it was read from\n got: % x\nwant: % x", got, want)
	}
}

// TestAnAlternativeIsChosenOncePerLine is what a variant is, seen from the
// values: one claim, five lines, and three different descriptions among them.
//
// A REDEFINES outside a repeating group resolves into record types and this one
// cannot, because the five lines below are one record. So the assertion is that
// the arm varies *within* a record, which is the whole of the difference
// between `discriminate-variant` and the `alternative` children `ledger/`
// writes.
func TestAnAlternativeIsChosenOncePerLine(t *testing.T) {
	t.Parallel()

	first := claims(t, read(t, Encoding(), fileBytes(t)))[0]

	if len(first.ClmLine) != 5 {
		t.Fatalf("the first claim carries five lines and the reader produced %d", len(first.ClmLine))
	}

	// Which arm each line holds, named by the field that is non-nil. Exactly
	// one of the three is, in every line.
	for i, want := range []string{"CLN-PROFESSIONAL", "CLN-PHARMACY", "CLN-FACILITY", "CLN-PHARMACY", "CLN-FACILITY"} {
		held := []string{}

		if first.ClmLine[i].ClnProfessional != nil {
			held = append(held, "CLN-PROFESSIONAL")
		}

		if first.ClmLine[i].ClnPharmacy != nil {
			held = append(held, "CLN-PHARMACY")
		}

		if first.ClmLine[i].ClnFacility != nil {
			held = append(held, "CLN-FACILITY")
		}

		if len(held) != 1 {
			t.Errorf("line %d holds %d arms (%s), and an occurrence holds exactly one", i+1, len(held), strings.Join(held, ", "))

			continue
		}

		if held[0] != want {
			t.Errorf("line %d holds %s, want %s", i+1, held[0], want)
		}
	}

	// And the two codes that select one description select the same one, which
	// is what the arm's `one-of` says.
	if first.ClmLine[2].ClnKind == first.ClmLine[4].ClnKind {
		t.Fatalf("the two facility lines carry the same kind code %q, and the point of them is that two codes select one arm", first.ClmLine[2].ClnKind)
	}
}

// TestEveryPharmacyLineRetainsItsOwnUndescribedBytes is the half of the round
// trip a caller cannot see, and the reason this file is inside the package.
//
// CLN-PHARMACY is six bytes shorter than the run it redefines, and those six
// are retained per *occurrence*: a reader that kept one run for the record, or
// a writer that carried the first line's run into the second, would still
// produce records whose every field was equal.
func TestEveryPharmacyLineRetainsItsOwnUndescribedBytes(t *testing.T) {
	t.Parallel()

	first := claims(t, read(t, Encoding(), fileBytes(t)))[0]

	// The two pharmacy lines of the first claim, and the runs they were
	// written with.
	for _, at := range []struct {
		line int
		want []byte
	}{
		{line: 1, want: slackOf(0)},
		{line: 3, want: slackOf(1)},
	} {
		held := first.ClmLine[at.line].ClnPharmacy
		if held == nil {
			t.Fatalf("line %d is not a pharmacy line", at.line+1)
		}

		// The run is retained rather than merely declared. The field is an
		// array of one, so its length says nothing about the read; what does is
		// whether the read put anything in it, since a nil run is one the
		// record does not carry and an empty run is a run of no bytes.
		if held.slack[0] == nil {
			t.Fatalf("line %d retains no bytes for the run CLN-PHARMACY does not describe", at.line+1)
		}

		if !bytes.Equal(held.slack[0], at.want) {
			t.Errorf("line %d retains % x, want % x", at.line+1, held.slack[0], at.want)
		}
	}
}

// TestTheItemsBehindTheTableSlideWithTheCount is what `(occurs-depending-on
// odoslide)` decides, in the one place it is visible: CLM-TOTAL-CHARGE and
// CLM-STATUS sit behind the table, so a claim of five lines and a claim of two
// put them seventy-eight bytes apart.
//
// Under `noodoslide` the same copybook would describe a file of fixed 266-byte
// claims with those two items always at the same offset, and nothing in the
// bytes would disagree. So this is the assertion that the reading in the layout
// is the reading the file was written under.
func TestTheItemsBehindTheTableSlideWithTheCount(t *testing.T) {
	t.Parallel()

	read := claims(t, read(t, Encoding(), fileBytes(t)))

	for i, want := range []int{5, 2} {
		if got := len(read[i].ClmLine); got != want {
			t.Fatalf("claim %d carries %d lines, want %d", i+1, got, want)
		}

		// The two items behind the table, decoded out of a record whose extent
		// the count decided. A fixed reading would have found them at byte 259
		// in both claims, which in the second is past the end of the record.
		if got := read[i].ClmTotalCharge; got != 104899 {
			t.Errorf("claim %d: CLM-TOTAL-CHARGE is %d, want %d", i+1, got, 104899)
		}

		if got := read[i].ClmStatus; got != "A" {
			t.Errorf("claim %d: CLM-STATUS is %q, want %q", i+1, got, "A")
		}
	}

	// And the records really are different lengths, which is what a
	// fixed-length dataset could not have held and what makes `(recfm VB)`
	// above a consequence of the reading rather than a taste.
	//
	// Thirty-two bytes of claim either side of the table, and twenty-six per
	// line.
	for _, lines := range []int{5, 2} {
		got := len(claimBytes(t, Encoding(), "CLM000000001", linesOf(lines)...))

		if want := 32 + 26*lines; got != want {
			t.Errorf("a claim of %d lines lays out as %d bytes, want %d", lines, got, want)
		}
	}
}

// linesOf is n professional lines, for an assertion about extent that does not
// care which description a line takes.
func linesOf(n int) []line {
	out := make([]line, 0, n)

	for range n {
		out = append(out, professional("PRV0000001", "99213", "25", 1))
	}

	return out
}

// TestALineCarryingAKindNoArmSelectsIsReportedAgainstItsOccurrence is the
// negative half, and it is the diagnostic discussion #340 arrived holding.
//
// Three codes select the three descriptions the copybook gives the run and a
// fourth selects none. An occurrence matching no arm is a read-time failure
// rather than a static one — docs/layout/SPEC.md, "A discriminator for a
// redefine inside a table" — and the report has to say which line of which
// record, because a claim of nine lines otherwise leaves an operator to guess.
func TestALineCarryingAKindNoArmSelectsIsReportedAgainstItsOccurrence(t *testing.T) {
	t.Parallel()

	enc := Encoding()

	var b bytes.Buffer

	b.Write(framed(claimBytes(t, enc, "CLM000000003",
		professional("PRV0000001", "99213", "25", 1),
		// `Z` is not `P`, not `D`, and not one of `F` and `I`.
		facility("Z", "FAC0000001", "0450", "131", 0),
	)))

	r, err := NewReader(bytes.NewReader(b.Bytes()), enc)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	_, err = r.Next()
	if err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("a line matching no arm was admitted, and Next reported %v", err)
	}

	if !strings.Contains(err.Error(), "no arm of the alternation over CLN-PROFESSIONAL matches") {
		t.Errorf("the report reads %q and does not say the line matches no arm", err)
	}

	if !strings.Contains(err.Error(), "occurrence") || !strings.Contains(err.Error(), "CLM-LINE") {
		t.Errorf("the report reads %q and does not say which occurrence of which table it is about", err)
	}
}
