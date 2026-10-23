// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package scaffold

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// The copybooks below are written in the fixed source format, which is what a
// copybook out of a mainframe library is written in and the one format `init`
// reads. Writing them any other way here would test a parse the command never
// performs.
const (
	// header is one 01-level and no REDEFINES: one record type, named after the
	// 01-level itself.
	header = `       01  LEDGER-HEADER.
           05  HDR-TYPE            PIC X(2).
           05  HDR-COUNT           PIC 9(6).
`

	// posting is two independent runs, one described three ways and one twice:
	// six record types out of one 01-level.
	posting = `       01  POSTING-RECORD.
           05  PST-TYPE            PIC X(2).
           05  PST-BODY            PIC X(28).
           05  PST-DEBIT           REDEFINES PST-BODY PIC X(28).
           05  PST-CREDIT          REDEFINES PST-BODY PIC X(28).
           05  PST-TAIL            PIC X(8).
           05  PST-TAIL-REF        REDEFINES PST-TAIL PIC X(8).
`

	// table is a REDEFINES inside a repeating group, which is a variant chosen
	// once per occurrence and never a record type.
	table = `       01  ORDER-RECORD.
           05  ORD-COUNT           PIC 9(2).
           05  ORD-LINE            OCCURS 10 TIMES.
               10  LN-BODY         PIC X(12).
               10  LN-CARD         REDEFINES LN-BODY PIC X(12).
`

	// depending carries an OCCURS DEPENDING ON, which is the whole of what
	// raises a `copybook-reading`.
	depending = `       01  BATCH-RECORD.
           05  BAT-COUNT  PIC 9(2).
           05  BAT-LINE  OCCURS 1 TO 10 TIMES DEPENDING ON BAT-COUNT.
               10  BLN-BODY  PIC X(12).
`

	// optional is the one shape whose record types a copybook does not decide
	// on its own: a REDEFINES inside a group declared `OCCURS 0 TO 1 TIMES
	// DEPENDING ON`, which is a variant under `odoslide` and a record type per
	// alternative under `noodoslide` (#373).
	optional = `       01  OPTION-RECORD.
           05  OPT-COUNT  PIC 9(1).
           05  OPT-GROUP  OCCURS 0 TO 1 TIMES DEPENDING ON OPT-COUNT.
               10  OPT-BODY  PIC X(12).
               10  OPT-CARD  REDEFINES OPT-BODY PIC X(12).
`

	// work is a member holding nothing but a level-77 item: it parses, and
	// there is no record in it to write a `record` form over.
	work = `       77  WORK-COUNT              PIC 9(4).
`

	// pair is two 01-levels in one copybook, which is how declaration order
	// within a file is told from the order the copybooks were given.
	pair = `       01  SECOND-RECORD.
           05  SND-TYPE            PIC X(2).
       01  FIRST-RECORD.
           05  FST-TYPE            PIC X(2).
`

	// both is one 01-level carrying a redefine outside a repeating group and
	// one inside: several record types and a variant over them.
	both = `       01  BOTH-RECORD.
           05  BTH-BODY            PIC X(6).
           05  BTH-CARD            REDEFINES BTH-BODY PIC X(6).
           05  BTH-LINE            OCCURS 4 TIMES.
               10  BLN-BODY        PIC X(12).
               10  BLN-CARD        REDEFINES BLN-BODY PIC X(12).
`

	// filler describes a run of bytes a second way with an item that has no
	// data-name, which no item reference can spell. `cobol-go` refuses the
	// mirror image — a REDEFINES *of* a FILLER cannot name its target — so this
	// is the reachable half.
	filler = `       01  FILLER-RECORD.
           05  FLR-TYPE            PIC X(2).
           05  FLR-BODY            PIC X(6).
           05  FILLER              REDEFINES FLR-BODY PIC X(6).
`
)

// deriveOf is the scaffold one set of copybooks decides, or a fatal test.
func deriveOf(t *testing.T, books ...Copybook) *Scaffold {
	t.Helper()

	derived, err := Derive(books)
	if err != nil {
		t.Fatalf("deriving the scaffold: %v", err)
	}

	return derived
}

// book is one copybook named by a path a test chose.
func book(path, source string) Copybook {
	return Copybook{Path: path, Source: []byte(source)}
}

// recordNames is what the scaffold called each record type, in emission order.
func recordNames(s *Scaffold) []string {
	names := make([]string, 0, len(s.records))
	for _, r := range s.records {
		names = append(names, r.name)
	}

	return names
}

func TestOneRecordPerZeroOneLevelInTheOrderTheCopybooksWereGiven(t *testing.T) {
	t.Parallel()

	derived := deriveOf(t, book("a/header.cpy", header), book("b/order.cpy", table))

	want := []string{"LEDGER-HEADER", "ORDER-RECORD"}
	if got := recordNames(derived); !slices.Equal(got, want) {
		t.Errorf("derived %v, want %v", got, want)
	}

	// The definitions file beside the layout, named after the copybook's final
	// path element: the directory it was typed in is no part of it.
	if got := derived.records[0].source; got != "header.cpy.sexpr" {
		t.Errorf("the first record names the definitions file %q, want header.cpy.sexpr", got)
	}
}

func TestWithinOneCopybookTheOrderIsDeclarationOrder(t *testing.T) {
	t.Parallel()

	// Declaration order rather than any order of cpybkc's: the names here are
	// deliberately the wrong way round alphabetically, so a sort would show.
	derived := deriveOf(t, book("pair.cpy", pair))

	want := []string{"SECOND-RECORD", "FIRST-RECORD"}
	if got := recordNames(derived); !slices.Equal(got, want) {
		t.Errorf("derived %v, want %v", got, want)
	}
}

func TestThePathIsWrittenAsItWasTypedAndNeverResolved(t *testing.T) {
	t.Parallel()

	// A path handed to `init` from somewhere else entirely. The definitions
	// file's header names it unchanged — a path cpybkc rewrote on the adopter's
	// behalf is one they cannot find in what they typed — and the record names
	// the definitions file bare, beside the layout, so no path in the scaffold
	// depends on the directory the command was typed in.
	derived := deriveOf(t, book("../shared/copybooks/header.cpy", header))

	if got := string(derived.Bytes()); !strings.Contains(got, `(definitions "header.cpy.sexpr" LEDGER-HEADER)`) {
		t.Errorf("the record does not name the definitions file beside the layout:\n%s", got)
	}

	files := derived.Definitions()
	if len(files) != 1 {
		t.Fatalf("wrote %d definitions files over one copybook, want one", len(files))
	}

	if !strings.HasPrefix(string(files[0].Bytes), `; The entries of the copybook "../shared/copybooks/header.cpy"`) {
		t.Errorf("the definitions file's header does not name the copybook as typed:\n%s", files[0].Bytes)
	}
}

func TestNoAlternativeChildWhereTheCopybookWritesNoRedefines(t *testing.T) {
	t.Parallel()

	derived := deriveOf(t, book("header.cpy", header))

	if got := len(derived.records[0].alternatives); got != 0 {
		t.Errorf("a record over a copybook with no REDEFINES carries %d alternatives, want none", got)
	}

	if got := string(derived.Bytes()); strings.Contains(got, "(alternative") {
		t.Errorf("an alternative was written for a copybook that writes no REDEFINES:\n%s", got)
	}
}

func TestOneRecordPerCombinationWithAnAlternativeChildPerRedefine(t *testing.T) {
	t.Parallel()

	derived := deriveOf(t, book("posting.cpy", posting))

	want := []string{
		"POSTING-RECORD-PST-BODY-PST-TAIL",
		"POSTING-RECORD-PST-BODY-PST-TAIL-REF",
		"POSTING-RECORD-PST-DEBIT-PST-TAIL",
		"POSTING-RECORD-PST-DEBIT-PST-TAIL-REF",
		"POSTING-RECORD-PST-CREDIT-PST-TAIL",
		"POSTING-RECORD-PST-CREDIT-PST-TAIL-REF",
	}

	if got := recordNames(derived); !slices.Equal(got, want) {
		t.Fatalf("derived %v,\nwant %v", got, want)
	}

	for _, r := range derived.records {
		if len(r.alternatives) != 2 {
			t.Errorf("%s carries %d alternatives, want one per REDEFINES run", r.name, len(r.alternatives))
		}

		for _, alternative := range r.alternatives {
			// The reference is rooted at the record whose choice it is
			// stating, which is what makes it an ordinary item reference.
			if alternative.record != r.name {
				t.Errorf("%s carries an alternative rooted at %s", r.name, alternative.record)
			}
		}
	}

	if got := derived.records[2].alternatives[0].String(); got != "(item POSTING-RECORD-PST-DEBIT-PST-TAIL PST-DEBIT)" {
		t.Errorf("the alternative reads %s", got)
	}
}

func TestARedefineInsideARepeatingGroupIsNeverAnAlternative(t *testing.T) {
	t.Parallel()

	derived := deriveOf(t, book("order.cpy", table))

	if got := recordNames(derived); !slices.Equal(got, []string{"ORDER-RECORD"}) {
		t.Errorf("derived %v, want one record type", got)
	}

	if got := len(derived.records[0].alternatives); got != 0 {
		t.Errorf("a variant was written as %d alternative children", got)
	}

	if len(derived.variants) != 1 {
		t.Fatalf("raised %d variant discriminators, want one", len(derived.variants))
	}

	variant := derived.variants[0]
	if got := variant.item.String(); got != "(item ORDER-RECORD ORD-LINE LN-BODY)" {
		t.Errorf("the variant is %s, want the redefined item under the group that repeats", got)
	}

	if got := variant.arms; !slices.Equal(got, []string{"LN-BODY", "LN-CARD"}) {
		t.Errorf("the arms are %v, want one per alternative", got)
	}
}

// A variant is chosen once per occurrence, so it is one form however many
// record types the 01-level resolves to — and the reference it carries has to
// be rooted at one of them.
func TestAVariantIsOneFormOverAnLevelThatResolvesToSeveralRecordTypes(t *testing.T) {
	t.Parallel()

	derived := deriveOf(t, book("both.cpy", both))

	if got := len(derived.records); got != 2 {
		t.Fatalf("derived %d record types, want one per alternative of the redefine outside the table", got)
	}

	if got := len(derived.variants); got != 1 {
		t.Fatalf("raised %d variant discriminators, want one per redefine inside a repeating group", got)
	}

	// Rooted at the first record type over the 01-level, which is the one an
	// adopter reads the form beside; a layout carrying the others writes a form
	// of its own for each, which they are writing anyway.
	if got := derived.variants[0].item.String(); got != "(item BOTH-RECORD-BTH-BODY BTH-LINE BLN-BODY)" {
		t.Errorf("the variant is %s, want it rooted at the first record type", got)
	}

	if got := derived.variants[0].arms; !slices.Equal(got, []string{"BLN-BODY", "BLN-CARD"}) {
		t.Errorf("the arms are %v, want one per alternative", got)
	}

	// And the redefine outside the table is still an alternative on each
	// record, rather than being swallowed by the variant beside it.
	for _, r := range derived.records {
		if len(r.alternatives) != 1 {
			t.Errorf("%s carries %d alternatives, want one", r.name, len(r.alternatives))
		}
	}
}

// A layout names an alternative with an item reference, and a reference is a
// path of names, so an item with no data-name is one no layout could name. The
// record type choosing it is noted rather than written — a reference with a hole
// in it would parse and then be reported against a line cpybkc wrote — and the
// record type beside it, which chooses the named description, is written.
func TestAnUnnamedAlternativeIsNotedAndTheRecordBesideItIsWritten(t *testing.T) {
	t.Parallel()

	derived := deriveOf(t, book("filler.cpy", filler))

	if got, want := recordNames(derived), []string{"FILLER-RECORD-FLR-BODY"}; !slices.Equal(got, want) {
		t.Errorf("wrote the records %v, want %v", got, want)
	}

	note := noteNaming(t, derived, "no data-name")
	for _, want := range []string{"filler.cpy", "FILLER-RECORD", "line 4", "the record type choosing it is not written"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note does not carry %q:\n%s", want, note)
		}
	}
}

// unnamedVariant is a REDEFINES inside a repeating group whose alternative has
// no data-name, so the `arm` every record type over the 01-level is owed for it
// has nothing to name.
const unnamedVariant = `       01  CARD-RECORD.
           05  CRD-BODY            PIC X(2).
           05  CRD-ALT             REDEFINES CRD-BODY PIC X(2).
           05  CRD-LINE            OCCURS 2 TIMES.
               10  CLN-BODY        PIC X(4).
               10  FILLER          REDEFINES CLN-BODY PIC X(4).
`

// A redefine inside a repeating group is a variant every record type over the
// 01-level carries, so an unnamed alternative there withholds all of them, and
// the note names each by the name it would have had.
func TestAnUnnamedAlternativeInsideARepeatingGroupNotesEveryRecordOfItsLevel(t *testing.T) {
	t.Parallel()

	derived := deriveOf(t, book("header.cpy", header), book("card.cpy", unnamedVariant))

	if got, want := recordNames(derived), []string{"LEDGER-HEADER"}; !slices.Equal(got, want) {
		t.Errorf("wrote the records %v, want %v", got, want)
	}

	if len(derived.variants) != 0 {
		t.Errorf("raised %d variants over records that were not written", len(derived.variants))
	}

	note := noteNaming(t, derived, "no data-name")
	for _, want := range []string{"card.cpy", "CARD-RECORD-CRD-BODY", "CARD-RECORD-CRD-ALT", "line 6"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note does not carry %q:\n%s", want, note)
		}
	}
}

// noteNaming is the one note carrying marker, or a fatal test.
func noteNaming(t *testing.T, s *Scaffold, marker string) string {
	t.Helper()

	var found []string

	for _, note := range s.Notes() {
		if strings.Contains(note, marker) {
			found = append(found, note)
		}
	}

	if len(found) != 1 {
		t.Fatalf("found %d notes carrying %q, want one: %q", len(found), marker, s.Notes())
	}

	return found[0]
}

// `sequence` names every record once, and there is no shape it could take over
// none — so a scaffold over no copybooks is not an empty scaffold, it is a
// question that was not asked.
func TestDerivingFromNoCopybooksIsRefused(t *testing.T) {
	t.Parallel()

	if _, err := Derive(nil); !errors.Is(err, ErrNoCopybooks) {
		t.Errorf("deriving from nothing gave %v, want %v", err, ErrNoCopybooks)
	}
}

// docs/cli/SPEC.md: every combination deriving a shared name is noted and none
// of them is written. Writing one would be cpybkc choosing which of them the
// adopter meant, and disambiguating would be inventing a name.
func TestADerivedNameCollidingWithAnotherIsNotedAndNeitherIsWritten(t *testing.T) {
	t.Parallel()

	// Two copybooks declaring one 01-level name is the ordinary way to reach
	// this: duplicate data names are legal COBOL, and two spellings of one file
	// reach it too.
	derived := deriveOf(t, book("one.cpy", header), book("posting.cpy", posting), book("two.cpy", header))

	for _, name := range recordNames(derived) {
		if name == "LEDGER-HEADER" {
			t.Fatal("a record whose name another record type derives too was written")
		}
	}

	if got := len(derived.records); got != 6 {
		t.Errorf("wrote %d records, want posting.cpy's six, which nothing collides with", got)
	}

	var collisions []string

	for _, note := range derived.Notes() {
		if strings.Contains(note, "derives the record name LEDGER-HEADER") {
			collisions = append(collisions, note)
		}
	}

	if len(collisions) != 2 {
		t.Fatalf("noted %d of the two colliding record types: %q", len(collisions), derived.Notes())
	}

	for at, path := range []string{"one.cpy", "two.cpy"} {
		if !strings.HasPrefix(collisions[at], path+": LEDGER-HEADER") {
			t.Errorf("note %d does not lead with %s and the 01-level:\n%s", at, path, collisions[at])
		}

		// Each names the other, because the answer is to rename one of them
		// and the adopter cannot tell which without seeing the pair.
		for _, want := range []string{"LEDGER-HEADER in one.cpy", "LEDGER-HEADER in two.cpy", "none of them is written"} {
			if !strings.Contains(collisions[at], want) {
				t.Errorf("note %d does not carry %q:\n%s", at, want, collisions[at])
			}
		}
	}
}

// overlong is an 01-level that parses and builds and that cpybkc's resolution
// cannot lay out: under the dialect `init` reads with, a redefining item may not
// be longer than what it redefines anywhere but at level 01.
const overlong = `       01  LONG-RECORD.
           05  LNG-BODY            PIC X(2).
           05  LNG-WIDE            REDEFINES LNG-BODY PIC X(4).
       01  SHORT-RECORD.
           05  SHT-BODY            PIC X(2).
`

// A per-01 resolve.Describe failure is a statement about the record a layout
// would carry, not about the copybook, so it is noted and the 01-level beside
// it is written.
func TestAnLevelTheResolverCannotDescribeIsNotedAndTheRestWritten(t *testing.T) {
	t.Parallel()

	derived := deriveOf(t, book("long.cpy", overlong))

	if got, want := recordNames(derived), []string{"SHORT-RECORD"}; !slices.Equal(got, want) {
		t.Errorf("wrote the records %v, want %v", got, want)
	}

	note := noteNaming(t, derived, "cannot be described")
	for _, want := range []string{"long.cpy", "LONG-RECORD", "LNG-WIDE", "no record is written"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note does not carry %q:\n%s", want, note)
		}
	}
}

// nilRecord is an 01-level named `nil`, which is legal COBOL and which
// `sexpr-go` reads as its empty value rather than as a symbol, so a `record`
// form carrying it would not parse as the name it was derived as.
const nilRecord = `       01  nil.
           05  NIL-TYPE            PIC X(2).
       01  KEPT-RECORD.
           05  KPT-TYPE            PIC X(2).
`

func TestADerivedNameTheGrammarCannotReadAsASymbolIsNoted(t *testing.T) {
	t.Parallel()

	derived := deriveOf(t, book("nil.cpy", nilRecord))

	if got, want := recordNames(derived), []string{"KEPT-RECORD"}; !slices.Equal(got, want) {
		t.Errorf("wrote the records %v, want %v", got, want)
	}

	note := noteNaming(t, derived, "does not read back")
	for _, want := range []string{"nil.cpy", `"nil"`, "not written"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note does not carry %q:\n%s", want, note)
		}
	}

	if err := readLayout(t, string(derived.Bytes())); err == nil {
		t.Fatal("the scaffold read as a finished layout")
	}
}

// A copybook every 01-level of which is noted contributes no record form, and
// the run says so rather than leaving the reader to count.
func TestACopybookWhoseEveryLevelIsNotedContributesNoRecord(t *testing.T) {
	t.Parallel()

	derived := deriveOf(t, book("header.cpy", header), book("only-nil.cpy", "       01  nil.\n           05  N  PIC X.\n"))

	if got, want := recordNames(derived), []string{"LEDGER-HEADER"}; !slices.Equal(got, want) {
		t.Errorf("wrote the records %v, want %v", got, want)
	}

	note := noteNaming(t, derived, "no record is derived")
	if !strings.HasPrefix(note, "only-nil.cpy: ") {
		t.Errorf("the note does not lead with the copybook as it was typed:\n%s", note)
	}
}

func TestANoteNamesTheCopybookTheLevelTheRedefinesAndTheRecordTypes(t *testing.T) {
	t.Parallel()

	derived := deriveOf(t, book("header.cpy", header), book("posting.cpy", posting))

	notes := derived.Notes()
	if len(notes) != 1 {
		t.Fatalf("wrote %d notes, want one per 01-level resolving to more than one record type: %v", len(notes), notes)
	}

	// Three REDEFINES over two runs, six record types. The count is reported
	// and never bounded: a copybook is not refused for the number of record
	// types it resolves to.
	for _, want := range []string{"posting.cpy", "POSTING-RECORD", "3 REDEFINES", "6 record types"} {
		if !strings.Contains(notes[0], want) {
			t.Errorf("the note does not carry %q:\n%s", want, notes[0])
		}
	}
}

// TestANoteIsWrittenWhereTheReadingDecidesTheRecordTypes is the one part of a
// scaffold that is not a function of the copybooks alone.
//
// `init` holds no reading — which compiler wrote the file is not in any copybook
// — so the `record` forms it writes are one of the two answers, and the adopter
// is told which beside the `copybook-reading` question the same copybook raises.
// Saying nothing would leave them a file whose record forms silently disagree
// with the reading they are about to state.
func TestANoteIsWrittenWhereTheReadingDecidesTheRecordTypes(t *testing.T) {
	t.Parallel()

	derived := deriveOf(t, book("option.cpy", optional))

	// Two notes, and the pair is the point: the first says how many record
	// types the 01-level produced, and the second says that the number is the
	// reading's answer rather than the copybook's.
	notes := derived.Notes()
	if len(notes) != 2 {
		t.Fatalf("wrote %d notes, want the record-type count and the reading: %v", len(notes), notes)
	}
	if !strings.Contains(notes[0], "2 record types") {
		t.Errorf("the first note does not count the record types:\n%s", notes[0])
	}

	for _, want := range []string{
		"option.cpy", "OPTION-RECORD", "OPT-BODY", "OPT-GROUP", "odoslide", "noodoslide",
	} {
		if !strings.Contains(notes[1], want) {
			t.Errorf("the note does not carry %q:\n%s", want, notes[1])
		}
	}

	// The forms below it are the answer the note names: two record types, and
	// no `discriminate-variant`, which is what `noodoslide` makes of the
	// cluster.
	if got := recordNames(derived); len(got) != 2 {
		t.Errorf("the scaffold carries the record types %v, want two", got)
	}
	if rendered := string(derived.Bytes()); strings.Contains(rendered, "discriminate-variant") {
		t.Errorf("a variant was raised over a cluster the scaffold resolved to record types:\n%s", rendered)
	}
}

// TestNoSuchNoteIsWrittenForATableTheTwoReadingsAgreeAbout is the control: a
// declared maximum above one is a table under both readings, so the record types
// are the copybook's own and there is nothing to raise.
func TestNoSuchNoteIsWrittenForATableTheTwoReadingsAgreeAbout(t *testing.T) {
	t.Parallel()

	wider := strings.Replace(optional, "OCCURS 0 TO 1 TIMES", "OCCURS 0 TO 4 TIMES", 1)

	if notes := deriveOf(t, book("option.cpy", wider)).Notes(); len(notes) != 0 {
		t.Errorf("wrote %d notes over a table both readings agree about: %v", len(notes), notes)
	}
}

func TestCopybookReadingIsRaisedForAnOccursDependingOnAndNotOtherwise(t *testing.T) {
	t.Parallel()

	without := string(deriveOf(t, book("order.cpy", table)).Bytes())
	if strings.Contains(without, "copybook-reading") {
		t.Errorf("a copybook-reading was raised over a fixed table:\n%s", without)
	}

	with := string(deriveOf(t, book("order.cpy", depending)).Bytes())
	if !strings.Contains(with, "(copybook-reading") {
		t.Fatalf("no copybook-reading was raised over an OCCURS DEPENDING ON:\n%s", with)
	}

	// Both readings named, neither chosen: which compiler wrote the data is
	// not in the copybook.
	for _, want := range []string{"odoslide", "noodoslide", "(occurs-depending-on " + readingHole + ")"} {
		if !strings.Contains(with, want) {
			t.Errorf("the copybook-reading block does not carry %q:\n%s", want, with)
		}
	}
}

// A redefine inside a repeating group takes three forms and the scaffold raises
// one of them, so the other two are named where the raised one is.
//
// The omission this closes is the wall discussion #340 hit: an adopter whose
// entries carry roles rather than types has no byte to test, and a scaffold that
// offered only `discriminate-variant` left them writing a predicate over bytes
// that decide nothing. Which of the three applies is a reading of the file and
// never of the copybook, so the comment states all three and chooses none
// (#341, #346, #353).
func TestTheVariantCommentNamesEveryFormARedefineInATableTakes(t *testing.T) {
	t.Parallel()

	text := string(deriveOf(t, book("order.cpy", table)).Bytes())

	for _, want := range []string{
		";; (discriminate-variant (item ORDER-RECORD ORD-LINE LN-BODY)",
		";;   (schedule-variant <item-ref>",
		";;     (arm <name> <occurrence> ...)",
		";;   (take-alternative <item-ref> <name>)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the scaffold does not carry %q:\n%s", want, text)
		}
	}

	// The two named in prose are named and not raised: a scaffold carrying a
	// second form over one redefine would be two statements about it, and
	// uncommenting both is a diagnostic the adopter did not have to meet.
	if got := strings.Count(text, "(schedule-variant"); got != 1 {
		t.Errorf("schedule-variant stands %d times, want once, in the comment above the form raised:\n%s", got, text)
	}
}

func TestTheCommentedFormsCarryTheirSubjectsAndNoValues(t *testing.T) {
	t.Parallel()

	text := string(deriveOf(t, book("posting.cpy", posting), book("order.cpy", table)).Bytes())

	// Every commented form, with its subject filled in and its value a
	// placeholder. A `sequence` names every record once.
	want := []string{
		";; (encoding",
		";;   (charset " + charsetHole + ")",
		";; (framing",
		";;   (recfm " + recfmHole + "))",
		";; (rename POSTING-RECORD-PST-BODY-PST-TAIL " + substituteHole + ")",
		";; (discriminate POSTING-RECORD-PST-DEBIT-PST-TAIL-REF " + strategyHole + ")",
		";; (discriminate ORDER-RECORD " + strategyHole + ")",
		";; (discriminate-variant (item ORDER-RECORD ORD-LINE LN-BODY)",
		";;   (arm LN-CARD " + predicateHole + "))",
		";; (sequence",
		";;   (" + operatorHole,
	}

	for _, line := range want {
		if !strings.Contains(text, line) {
			t.Errorf("the scaffold does not carry %q:\n%s", line, text)
		}
	}

	// One rename per record over an 01-level with more than one record type,
	// and none for a record that stands alone.
	if strings.Contains(text, "(rename ORDER-RECORD") {
		t.Errorf("a rename was raised for the only record type over an 01-level:\n%s", text)
	}

	// Every record is sequenced once.
	for _, name := range recordNames(deriveOf(t, book("posting.cpy", posting), book("order.cpy", table))) {
		if got := strings.Count(text, ";;     "+name+"\n") + strings.Count(text, ";;     "+name+"))\n"); got != 1 {
			t.Errorf("%s is named %d times in the sequence, want once", name, got)
		}
	}
}

func TestTwoRunsOverOneSetOfCopybooksProduceByteIdenticalFiles(t *testing.T) {
	t.Parallel()

	books := []Copybook{book("header.cpy", header), book("posting.cpy", posting), book("order.cpy", depending)}

	first := deriveOf(t, books...).Bytes()
	second := deriveOf(t, books...).Bytes()

	if string(first) != string(second) {
		t.Error("two derivations over one set of copybooks differ, and a scaffold has to be diffable")
	}
}

func TestACopybookThatIsNotOneIsReportedByThePathThatWasTyped(t *testing.T) {
	t.Parallel()

	_, err := Derive([]Copybook{book("broken.cpy", "       01  PIC PIC PIC.\n")})
	if err == nil {
		t.Fatal("a copybook that is not COBOL was accepted")
	}

	if got := diag.Render(err); !strings.Contains(got, "broken.cpy") {
		t.Errorf("the diagnostic does not name the copybook:\n%s", got)
	}
}

// docs/cli/SPEC.md, "`init`'s streams and exit codes": a copybook no record
// derives from still gets its definitions file, and a note. A member holding
// nothing but level-77 work items is the ordinary way to arrive here.
func TestACopybookDeclaringNoNamedLevelIsTranscribedAndNoted(t *testing.T) {
	t.Parallel()

	derived := deriveOf(t, book("header.cpy", header), book("work.cpy", work))

	if got, want := recordNames(derived), []string{"LEDGER-HEADER"}; !slices.Equal(got, want) {
		t.Errorf("wrote the records %v, want %v", got, want)
	}

	files := derived.Definitions()
	if len(files) != 2 || files[1].Name != "work.cpy.sexpr" {
		t.Fatalf("wrote the definitions files %v, want one per copybook", files)
	}

	if !strings.Contains(string(files[1].Bytes), "(entry 77 WORK-COUNT") {
		t.Errorf("the definitions file does not carry the 77-level:\n%s", files[1].Bytes)
	}

	note := noteNaming(t, derived, "no record is derived")
	for _, want := range []string{"work.cpy: ", "declares no named 01-level", "still written"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note does not carry %q:\n%s", want, note)
		}
	}
}

// A copybook that declares no entry at all has nothing to transcribe, and stays
// a refusal (#31).
func TestACopybookDeclaringNoEntryAtAllIsReported(t *testing.T) {
	t.Parallel()

	_, err := Derive([]Copybook{book("empty.cpy", "      * a comment, and nothing it describes\n")})

	var missing *NoEntryError
	if !errors.As(err, &missing) {
		t.Fatalf("the fault is %v, want a copybook declaring no entry", err)
	}

	if missing.Path != "empty.cpy" {
		t.Errorf("the fault names %s, want empty.cpy", missing.Path)
	}
}

// A copybook that parses and does not build — a PICTURE nothing can read — is
// one that cannot be transcribed, and stays a refusal.
func TestACopybookThatDoesNotBuildIsReported(t *testing.T) {
	t.Parallel()

	_, err := Derive([]Copybook{book("header.cpy", header), book("bad.cpy", "       01  BAD-RECORD.\n           05  BAD-ITEM  PIC Q(2).\n")})

	var source *SourceError
	if !errors.As(err, &source) {
		t.Fatalf("the fault is %v, want a copybook this build cannot read", err)
	}

	if source.Path != "bad.cpy" {
		t.Errorf("the fault names %s, want bad.cpy", source.Path)
	}
}

func TestEveryFaultIsReportedRatherThanTheFirst(t *testing.T) {
	t.Parallel()

	_, err := Derive([]Copybook{book("first.cpy", "      * nothing\n"), book("second.cpy", "      * nothing\n")})
	if err == nil {
		t.Fatal("two unusable copybooks were accepted")
	}

	if got := len(diag.Diagnostics(err)); got != 2 {
		t.Errorf("reported %d faults over two bad copybooks, want both", got)
	}
}

// exampleCrossProduct is every combination `example/ledger/posting.cpy` admits,
// sorted: PST-BODY described three ways crossed with PST-TAIL described two,
// which is the six record types docs/cli/SPEC.md's `init` arithmetic counts.
//
// Written down rather than derived a second way. The point of pinning it is that
// a derivation which lost or duplicated a combination is caught, and a `want`
// computed by the code under test would agree with such a change.
var exampleCrossProduct = []string{
	"PST-BODY+PST-TAIL",
	"PST-BODY+PST-TAIL-REF",
	"PST-CREDIT+PST-TAIL",
	"PST-CREDIT+PST-TAIL-REF",
	"PST-DEBIT+PST-TAIL",
	"PST-DEBIT+PST-TAIL-REF",
}

// exampleBaseDescriptions are the two items in `example/ledger/posting.cpy` that a
// REDEFINES is written *over*: PST-BODY, which PST-DEBIT and PST-CREDIT redefine,
// and PST-TAIL, which PST-TAIL-REF does.
//
// They are ordinary COBOL declarations and `init` derives combinations naming
// them, correctly — a copybook cannot say which of its descriptions a file
// carries. What a mainframe-produced file holds is the redefinitions, which is
// why the worked example's layout names neither of these.
var exampleBaseDescriptions = []string{"PST-BODY", "PST-TAIL"}

// The measure docs/cli/SPEC.md sets: `example/ledger/posting.cpy` is six record
// types over one 01-level with twelve `alternative` children, and every one of
// them is recoverable from the copybook alone.
//
// The worked example's layout names **two** of those six — the two whose every
// alternative is a REDEFINES rather than a base description, which are the two a
// mainframe-produced extract carries. So the two sides are not equal and must not
// be asserted to be. What is asserted instead is both halves of that, and neither
// is weaker than the equality this replaced:
//
//   - the derivation is exactly [exampleCrossProduct], so a combination lost,
//     duplicated or renamed is still caught — it used to be caught by the layout
//     enumerating all six, and pinning the list is what carries that over;
//   - every combination the layout names is one the derivation offers. A layout
//     combination the scaffold did not derive is a combination `init` cannot
//     offer, which is the failure this test is for; a derived combination the
//     layout does not name is the adopter having chosen, which is what a layout
//     is for.
//
// What the layout also has that the scaffold does not is the names — a reading of
// what the file means — and the forms the scaffold leaves commented.
func TestTheWorkedExampleDerivesLedgersRecordsAndAlternatives(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "example", "ledger", "posting.cpy"))
	if err != nil {
		t.Fatalf("reading the worked example's copybook: %v", err)
	}

	// The path as `example/ledger/ledger.sexpr` spells it, which is what makes the
	// two comparable.
	derived := deriveOf(t, book("posting.cpy", string(source)))

	var got []string

	for _, record := range derived.records {
		// Every record names the definitions file posting.cpy converts to,
		// which is what the layout beside it would name once moved.
		if record.source != "posting.cpy.sexpr" {
			t.Errorf("%s names %s, want the definitions file posting.cpy.sexpr", record.name, record.source)
		}

		chosen := make([]string, 0, len(record.alternatives))
		for _, alternative := range record.alternatives {
			if len(alternative.path) == 0 {
				t.Fatalf("%s carries an alternative with no path, which no layout could name", record.name)
			}

			chosen = append(chosen, strings.Join(alternative.path, " "))
		}

		got = append(got, strings.Join(chosen, "+"))
	}

	// Compared as a set. Which combination is written first is the derivation's
	// own order and is not a statement about the copybook; which alternatives
	// are chosen is.
	slices.Sort(got)

	if !slices.Equal(got, exampleCrossProduct) {
		t.Fatalf("derived the combinations\n%v\nand posting.cpy admits\n%v", got, exampleCrossProduct)
	}

	want := workedExampleCombinations(t)

	if len(want) != 2 {
		t.Fatalf("the layout names %d combinations of posting.cpy, want the two whose alternatives are all REDEFINES", len(want))
	}

	for _, combination := range want {
		if !slices.Contains(got, combination) {
			t.Errorf("the layout names the combination\n%s\nwhich the scaffold does not derive; it derives\n%v", combination, got)
		}
	}
}

// TestTheWorkedExampleLayoutNamesNoBaseDescription is the property the worked
// example exists to demonstrate, asserted rather than reviewed by eye.
//
// A base description is the declaration a REDEFINES is written over. It is real
// COBOL and `init` scaffolds combinations naming it, but a file a mainframe
// produced carries the redefinitions and never the declaration underneath them —
// so a layout that names one describes bytes no extract holds.
//
// Without this, a combination naming PST-BODY or PST-TAIL could be added back to
// `example/ledger/ledger.sexpr` and every other test in this repository would
// still pass: the derivation offers all six, `resolve` raises no diagnostic on
// the four, and the generated tree would simply regenerate around it.
func TestTheWorkedExampleLayoutNamesNoBaseDescription(t *testing.T) {
	t.Parallel()

	for _, combination := range workedExampleCombinations(t) {
		for chosen := range strings.SplitSeq(combination, "+") {
			if slices.Contains(exampleBaseDescriptions, chosen) {
				t.Errorf("the layout's combination %s names %s, which is the base description of a redefined run: a mainframe-produced extract carries the REDEFINES and not the declaration they are written over",
					combination, chosen)
			}
		}
	}
}

// workedExampleCombinations is which description of each redefined run every
// `record` form in `example/ledger/ledger.sexpr` over `posting.cpy` means, in the
// layout's own order.
//
// The whole containment path rather than the leaf name: a reference into the
// wrong group has the same leaf, so comparing leaves would let a caller pass
// over a layout naming bytes nobody meant.
func workedExampleCombinations(t *testing.T) []string {
	t.Helper()

	file, err := layout.ParseFile(filepath.Join("..", "..", "example", "ledger", "ledger.sexpr"))
	if err != nil {
		t.Fatalf("parsing the worked example's layout: %v", err)
	}

	written, err := layoutmodel.ReadRecords(file)
	if err != nil {
		t.Fatalf("reading the worked example's records: %v", err)
	}

	var combinations []string

	for _, record := range written {
		if record.Path != "posting.cpy" {
			continue
		}

		chosen := make([]string, 0, len(record.Alternatives))
		for _, alternative := range record.Alternatives {
			chosen = append(chosen, strings.Join(alternative.Path, " "))
		}

		combinations = append(combinations, strings.Join(chosen, "+"))
	}

	return combinations
}

// The scaffold over a fixed set of copybooks, byte for byte. The golden files
// were written by the derivation as #406 left it and moved once, by #408, when
// every record's source became the definitions file beside the layout; the
// rest of each file is unchanged from #406's. Nothing here regenerates them,
// so a scaffold that moves is a test that fails and a diff that says how.
func TestTheScaffoldIsUnchangedForEveryCopybookAcceptedBefore(t *testing.T) {
	t.Parallel()

	ledger := make([]Copybook, 0, 3)

	for _, name := range []string{"header.cpy", "posting.cpy", "trailer.cpy"} {
		source, err := os.ReadFile(filepath.Join("..", "..", "example", "ledger", name))
		if err != nil {
			t.Fatalf("reading the worked example's copybook: %v", err)
		}

		ledger = append(ledger, Copybook{Path: name, Source: source})
	}

	for golden, books := range map[string][]Copybook{
		"ledger.golden.sexpr": ledger,
		"accepted.golden.sexpr": {
			book("header.cpy", header), book("posting.cpy", posting), book("order.cpy", table),
			book("batch.cpy", depending), book("option.cpy", optional), book("pair.cpy", pair),
			book("both.cpy", both),
		},
	} {
		want, err := os.ReadFile(filepath.Join("testdata", golden))
		if err != nil {
			t.Fatalf("reading the golden scaffold: %v", err)
		}

		if got := deriveOf(t, books...).Bytes(); string(got) != string(want) {
			t.Errorf("the scaffold differs from %s:\n%s", golden, got)
		}
	}

	// And the definitions files beside the ledger's, which are what an adopter
	// diffs when the vendor ships a new copybook.
	for _, file := range deriveOf(t, ledger...).Definitions() {
		want, err := os.ReadFile(filepath.Join("testdata", "ledger", file.Name))
		if err != nil {
			t.Fatalf("reading the golden definitions file: %v", err)
		}

		if string(file.Bytes) != string(want) {
			t.Errorf("%s differs from its golden file:\n%s", file.Name, file.Bytes)
		}
	}
}
