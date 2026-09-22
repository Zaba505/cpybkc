// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The round-trip assertions over the code cpybkc generated for ../member.sexpr.
//
// They live *inside* the generated package rather than beside it, for the
// reason `claim/`'s do: a `_test.go` file is not part of the package, so it is
// not output and the regeneration test does not hold it against anything, and
// one assertion here cannot be stated from outside — a mailing address
// describes thirty-four of the sixty-one bytes an entry holds, so the bytes it
// retains for the rest sit in an unexported field.
//
// What the generated tests already assert is one member of one address, which
// is the first arm of the schedule and no more. What is here is what only a
// hand-written test can say: that which arm an entry holds is decided by where
// the entry sits and by nothing in its bytes, that the count decides how many
// of the scheduled arms a record reaches rather than which arm any of them is,
// that the entry the schedule gave the short alternative carries its own
// undescribed bytes back out, and that a caller who fills in an arm the
// schedule did not assign is reported rather than picked between.
package member

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

// laidOut is the bytes of one record, written item by item through codec itself
// rather than through this package, so that what the generated methods are held
// against is not their own output.
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

// entry is one occurrence of MBR-ADDRESS: sixty-one bytes, whichever of the
// three descriptions covers them.
//
// It carries no code saying which, and that is the whole point of the example.
// The constructors below are named for the roles the enrolment screen writes in
// order, and the only thing that makes an entry a work address is that it is
// the second one.
type entry func(*codec.Writer) error

// home is an ADR-HOME entry, the description the copybook gives the run first.
func home(street, city, region, postal string) entry {
	return func(w *codec.Writer) error {
		if err := w.WriteAlphanumeric(street, 30); err != nil {
			return err
		}

		if err := w.WriteAlphanumeric(city, 20); err != nil {
			return err
		}

		if err := w.WriteAlphanumeric(region, 2); err != nil {
			return err
		}

		return w.WriteAlphanumeric(postal, 9)
	}
}

// work is an ADR-WORK entry. It describes the whole sixty-one bytes as well,
// which is why nothing about an entry's width says which of the two it is.
func work(employer, street, region, postal string) entry {
	return func(w *codec.Writer) error {
		if err := w.WriteAlphanumeric(employer, 30); err != nil {
			return err
		}

		if err := w.WriteAlphanumeric(street, 20); err != nil {
			return err
		}

		if err := w.WriteAlphanumeric(region, 2); err != nil {
			return err
		}

		return w.WriteAlphanumeric(postal, 9)
	}
}

// mail is an ADR-MAIL entry. It describes thirty-four of the sixty-one bytes
// the run holds, so the twenty-seven behind them are slack, and they are
// written here as bytes no item of the record describes.
//
// They are deliberately not spaces and deliberately different per member: a run
// that survives a read is only visibly surviving if it is not the padding a
// writer would have chosen, and a run belonging to the occurrence it was read
// from is only visibly so if the second member's run differs from the first's.
func mail(box, city, region string, slack []byte) entry {
	return func(w *codec.Writer) error {
		if err := w.WriteAlphanumeric(box, 12); err != nil {
			return err
		}

		if err := w.WriteAlphanumeric(city, 20); err != nil {
			return err
		}

		if err := w.WriteAlphanumeric(region, 2); err != nil {
			return err
		}

		return w.WriteBytes(slack)
	}
}

// memberBytes is one MEMBER-RECORD carrying the entries it was given.
//
// The count is written from the number of entries rather than passed in, which
// is what an OCCURS DEPENDING ON means on the writing side: the field is the
// descriptor's and not the caller's.
func memberBytes(t *testing.T, enc codec.Encoding, id string, entries ...entry) []byte {
	t.Helper()

	return laidOut(t, enc, func(w *codec.Writer) error {
		if err := w.WriteAlphanumeric(id, 10); err != nil {
			return err
		}

		if err := w.WriteAlphanumeric("MEMBER "+id, 30); err != nil {
			return err
		}

		if err := w.WriteZonedInt32(int32(len(entries)), 1, codec.SignUnsigned); err != nil {
			return err
		}

		for _, e := range entries {
			if err := e(w); err != nil {
				return err
			}
		}

		return w.WriteAlphanumeric("A", 1)
	})
}

// slackOf is the twenty-seven bytes the nth mailing address of the file below
// carries, and they are distinct per member so that a writer that carried one
// member's run into another would be caught rather than merely suspected.
func slackOf(n byte) []byte {
	out := make([]byte, 27)

	for i := range out {
		out[i] = byte(0x40+i) + n
	}

	return out
}

// fileBytes is a whole membership extract: a member who gave all three
// addresses, a member who gave two and a member who gave one, so that the item
// behind the table begins at a different offset in each and so that the arms
// reached differ between records while the schedule does not.
func fileBytes(t *testing.T) []byte {
	t.Helper()

	enc := Encoding()

	var b bytes.Buffer

	b.Write(framed(memberBytes(t, enc, "M000000001",
		home("14 ORCHARD LANE", "SPRINGFIELD", "IL", "627010000"),
		work("NORTHWIND MUTUAL", "1 PLAZA DRIVE", "IL", "627020000"),
		mail("PO BOX 41", "SPRINGFIELD", "IL", slackOf(0)),
	)))

	b.Write(framed(memberBytes(t, enc, "M000000002",
		home("9 KESTREL WAY", "DECATUR", "IL", "625210000"),
		work("MACON COUNTY SCHOOLS", "600 EAST WOOD", "IL", "625220000"),
	)))

	b.Write(framed(memberBytes(t, enc, "M000000003",
		home("77 MILL ROAD", "PEORIA", "IL", "616020000"),
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
func write(t *testing.T, enc codec.Encoding, records []Record) ([]byte, error) {
	t.Helper()

	var b bytes.Buffer

	w, err := NewWriter(&b, enc)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	for _, rec := range records {
		if err := w.Write(rec); err != nil {
			return nil, err
		}
	}

	if err := w.Close(); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

// members is the file's records as MEMBER-RECORDs, since one record type is all
// this layout describes.
func members(t *testing.T, records []Record) []*MemberRecord {
	t.Helper()

	out := make([]*MemberRecord, 0, len(records))

	for i, rec := range records {
		member, ok := rec.(*MemberRecord)
		if !ok {
			t.Fatalf("record %d is a %T, want a *MemberRecord", i+1, rec)
		}

		out = append(out, member)
	}

	return out
}

// TestAFileOfMembersReadsBackAsTheFileItWas is the claim the worked example is
// for: a file whose entries take every description the copybook gives the
// redefined run reads to records and writes back to the bytes it came from.
//
// Bytes rather than values, and a whole file rather than a record, because the
// framing is on the path too: a record descriptor word states the length of the
// record behind it, and under `odoslide` that length is a different number for
// a member of three addresses and a member of one.
func TestAFileOfMembersReadsBackAsTheFileItWas(t *testing.T) {
	t.Parallel()

	want := fileBytes(t)

	records := read(t, Encoding(), want)
	if len(records) != 3 {
		t.Fatalf("the file holds three members and the reader produced %d", len(records))
	}

	got, err := write(t, Encoding(), records)
	if err != nil {
		t.Fatalf("writing the file back: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("the file does not write back the bytes it was read from\n got: % x\nwant: % x", got, want)
	}
}

// heldBy is the arms entry j of a member holds, named. Exactly one of the three
// is non-nil in every entry, and this is what says which.
//
// The entry type is the anonymous struct the generator writes inside the slice,
// so it is reached through the record rather than named here.
func heldBy(m *MemberRecord, j int) []string {
	held := []string{}

	if m.MbrAddress[j].AdrHome != nil {
		held = append(held, "ADR-HOME")
	}

	if m.MbrAddress[j].AdrWork != nil {
		held = append(held, "ADR-WORK")
	}

	if m.MbrAddress[j].AdrMail != nil {
		held = append(held, "ADR-MAIL")
	}

	return held
}

// TestTheArmAnEntryHoldsIsTheOneItsPositionAssigns is what a scheduled variant
// is, seen from the values.
//
// `claim/` asserts the same shape the other way round: there the arm follows a
// code inside the occurrence, and two lines of one claim carrying the same code
// carry the same arm wherever they sit. Here there is no code, and the
// assertion is that position alone decides — the first member's three entries
// are a home, a work and a mailing address in that order, and they are so
// because of where they sit.
//
// The count decides how many of the scheduled arms a record reaches and nothing
// else: the second member reaches the first two, the third only the first, and
// no entry of any of them is a different arm than its position assigns.
func TestTheArmAnEntryHoldsIsTheOneItsPositionAssigns(t *testing.T) {
	t.Parallel()

	read := members(t, read(t, Encoding(), fileBytes(t)))

	for i, want := range [][]string{
		{"ADR-HOME", "ADR-WORK", "ADR-MAIL"},
		{"ADR-HOME", "ADR-WORK"},
		{"ADR-HOME"},
	} {
		if got := len(read[i].MbrAddress); got != len(want) {
			t.Fatalf("member %d carries %d addresses, want %d", i+1, got, len(want))
		}

		for j, arm := range want {
			held := heldBy(read[i], j)

			if len(held) != 1 {
				t.Errorf("member %d entry %d holds %d arms (%s), and an occurrence holds exactly one",
					i+1, j+1, len(held), strings.Join(held, ", "))

				continue
			}

			if held[0] != arm {
				t.Errorf("member %d entry %d holds %s, want %s", i+1, j+1, held[0], arm)
			}
		}
	}
}

// TestTheMailingAddressRetainsTheBytesNoItemDescribes is the half of the round
// trip a caller cannot see, and the reason this file is inside the package.
//
// ADR-MAIL is twenty-seven bytes shorter than the run it redefines, and those
// twenty-seven are retained by the occurrence they were read from. What is
// different here from `claim/` is which occurrences have them: there it is
// whichever lines carried a `D`, which is the data's to say, and here it is
// entry three of every member who gave three addresses and no other entry of
// anybody, which is the schedule's.
func TestTheMailingAddressRetainsTheBytesNoItemDescribes(t *testing.T) {
	t.Parallel()

	read := members(t, read(t, Encoding(), fileBytes(t)))

	held := read[0].MbrAddress[2].AdrMail
	if held == nil {
		t.Fatalf("the first member's third entry is not a mailing address")
	}

	// The run is retained rather than merely declared. The field is an array of
	// one, so its length says nothing about the read; what does is whether the
	// read put anything in it, since a nil run is one the record does not carry
	// and an empty run is a run of no bytes.
	if held.slack[0] == nil {
		t.Fatalf("the mailing address retains no bytes for the run ADR-MAIL does not describe")
	}

	if want := slackOf(0); !bytes.Equal(held.slack[0], want) {
		t.Errorf("the mailing address retains % x, want % x", held.slack[0], want)
	}

	// And the two members who gave no mailing address have no such run
	// anywhere, which is the schedule saying so rather than the bytes: every
	// entry they carry is an arm that describes the whole sixty-one.
	for i, member := range read[1:] {
		for j, at := range member.MbrAddress {
			if at.AdrMail != nil {
				t.Errorf("member %d entry %d is a mailing address, and the schedule assigns one only to entry 3", i+2, j+1)
			}
		}
	}
}

// TestTheItemBehindTheTableSlidesWithTheCount is what `(occurs-depending-on
// odoslide)` decides, in the one place it is visible: MBR-STATUS sits behind
// the table, so a member of three addresses and a member of one put it
// one hundred and twenty-two bytes apart.
//
// Under `noodoslide` the same copybook would describe a file of fixed 225-byte
// members with that item always at the same offset, and nothing in the bytes
// would disagree. The schedule above would be the same text either way, which
// is the claim docs/layout/SPEC.md makes for it; this is the assertion that the
// reading in the layout is the reading the file was written under.
func TestTheItemBehindTheTableSlidesWithTheCount(t *testing.T) {
	t.Parallel()

	read := members(t, read(t, Encoding(), fileBytes(t)))

	for i, want := range []int{3, 2, 1} {
		if got := len(read[i].MbrAddress); got != want {
			t.Fatalf("member %d carries %d addresses, want %d", i+1, got, want)
		}

		// The item behind the table, decoded out of a record whose extent the
		// count decided. A fixed reading would have found it at byte 224 in
		// every member, which in the third is past the end of the record.
		if got := read[i].MbrStatus; got != "A" {
			t.Errorf("member %d: MBR-STATUS is %q, want %q", i+1, got, "A")
		}
	}

	// And the records really are different lengths, which is what a
	// fixed-length dataset could not have held and what makes `(recfm VB)` in
	// the layout a consequence of the reading rather than a taste.
	//
	// Forty-two bytes of member either side of the table, and sixty-one per
	// address.
	for _, addresses := range []int{3, 2, 1} {
		got := len(memberBytes(t, Encoding(), "M000000001", entriesOf(addresses)...))

		if want := 42 + 61*addresses; got != want {
			t.Errorf("a member of %d addresses lays out as %d bytes, want %d", addresses, got, want)
		}
	}
}

// entriesOf is n entries, for an assertion about extent that does not care
// which description an entry takes — every arm of this variant is sixty-one
// bytes wide, which is what a variant is.
func entriesOf(n int) []entry {
	out := make([]entry, 0, n)

	for range n {
		out = append(out, home("14 ORCHARD LANE", "SPRINGFIELD", "IL", "627010000"))
	}

	return out
}

// TestAnEntryFilledInAgainstTheScheduleIsReported is the negative half, and it
// is the one a scheduled variant states differently from a byte-selected one.
//
// `claim/`'s negative case is a *reader's*: a line carrying a kind code no arm
// selects is a file that does not match its layout, and it is found at read
// time. A schedule cannot fail that way at all — every occurrence of the table
// is covered exactly once or `resolve` refuses the layout, so there is no
// entry a reader can meet that matches no arm.
//
// What is left is a writer's, and it is the mirror image: the arm is the
// descriptor's, so a caller who fills in the arm the schedule did not assign is
// told so rather than having one of the two picked for them. The report names
// the occurrence, the arm the schedule assigns and the arm the record holds,
// because a caller who built the record from a map otherwise has nothing to go
// on.
func TestAnEntryFilledInAgainstTheScheduleIsReported(t *testing.T) {
	t.Parallel()

	records := read(t, Encoding(), fileBytes(t))

	first := members(t, records)[0]

	// A home address in entry three, where the schedule assigns the mailing
	// address. Nothing about the values is wrong — it is an ADR-HOME with every
	// item filled in, and it is sixty-one bytes wide like every other arm.
	first.MbrAddress[2].AdrMail = nil
	first.MbrAddress[2].AdrHome = first.MbrAddress[0].AdrHome

	_, err := write(t, Encoding(), records)
	if err == nil {
		t.Fatalf("an entry holding an arm the schedule did not assign was emitted, and Write reported no error")
	}

	if !strings.Contains(err.Error(), "the schedule assigns ADR-MAIL") {
		t.Errorf("the report reads %q and does not say which arm the schedule assigns", err)
	}

	if !strings.Contains(err.Error(), "occurrence") || !strings.Contains(err.Error(), "MBR-ADDRESS") {
		t.Errorf("the report reads %q and does not say which occurrence of which table it is about", err)
	}
}
