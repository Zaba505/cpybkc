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

	if got := derived.records[0].path; got != "a/header.cpy" {
		t.Errorf("the first record names the copybook %q, want the path as it was typed", got)
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

	// A path a layout's own reader would resolve against the layout, handed to
	// `init` from somewhere else entirely. cpybkc writes it back unchanged: a
	// path it rewrote on the adopter's behalf is one they cannot find in what
	// they typed.
	derived := deriveOf(t, book("../shared/copybooks/header.cpy", header))

	if got := string(derived.Bytes()); !strings.Contains(got, `(copybook "../shared/copybooks/header.cpy" LEDGER-HEADER)`) {
		t.Errorf("the copybook child does not carry the path as typed:\n%s", got)
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
// path of names, so an item with no data-name is one no layout could name.
// Refusing beats writing a reference with a hole in it that would parse and then
// be reported against a line cpybkc wrote.
func TestARedefineOverAnItemWithNoDataNameIsRefused(t *testing.T) {
	t.Parallel()

	_, err := Derive([]Copybook{book("filler.cpy", filler)})
	if err == nil {
		t.Fatal("a REDEFINES over an unnamed item was accepted")
	}

	var unnameable *UnnamedAlternativeError
	if !errors.As(err, &unnameable) {
		t.Fatalf("the fault is %v, want an alternative no layout could name", err)
	}

	if got := diag.Render(err); !strings.Contains(got, "filler.cpy") || !strings.Contains(got, "FILLER-RECORD") {
		t.Errorf("the diagnostic names neither the copybook nor the 01-level:\n%s", got)
	}
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

func TestADerivedNameCollidingWithAnotherFailsTheRunNamingBoth(t *testing.T) {
	t.Parallel()

	// Two copybooks declaring one 01-level name is the ordinary way to reach
	// this: duplicate data names are legal COBOL, and two spellings of one file
	// reach it too.
	_, err := Derive([]Copybook{book("one.cpy", header), book("two.cpy", header)})
	if err == nil {
		t.Fatal("two record types deriving one name were accepted")
	}

	var collision *NameCollisionError
	if !errors.As(err, &collision) {
		t.Fatalf("the fault is %v, want a name collision", err)
	}

	rendered := diag.Render(err)
	for _, want := range []string{"LEDGER-HEADER", "one.cpy", "two.cpy"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the diagnostic does not name %s:\n%s", want, rendered)
		}
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

func TestACopybookDeclaringNoZeroOneLevelIsReported(t *testing.T) {
	t.Parallel()

	_, err := Derive([]Copybook{book("work.cpy", work)})
	if err == nil {
		t.Fatal("a copybook declaring no 01-level was accepted")
	}

	var missing *NoRecordError
	if !errors.As(err, &missing) {
		t.Fatalf("the fault is %v, want a missing record", err)
	}
}

func TestEveryFaultIsReportedRatherThanTheFirst(t *testing.T) {
	t.Parallel()

	_, err := Derive([]Copybook{book("first.cpy", work), book("second.cpy", work)})
	if err == nil {
		t.Fatal("two unusable copybooks were accepted")
	}

	if got := len(diag.Diagnostics(err)); got != 2 {
		t.Errorf("reported %d faults over two bad copybooks, want both", got)
	}
}

// The measure docs/cli/SPEC.md sets: `example/posting.cpy` is six record types
// over one 01-level with twelve `alternative` children, and every one of them is
// recoverable from the copybook alone.
//
// The worked example's layout names **two** of those six — the two whose every
// alternative is a REDEFINES rather than a base description, which are the two a
// mainframe-produced extract carries. So the two sides are not equal and must not
// be asserted to be: what holds is that the derivation is the whole cross product
// and that everything the layout names is in it. A layout combination the scaffold
// did not derive would be a combination `init` cannot offer, which is the failure
// this test is for; a derived combination the layout does not name is the adopter
// having chosen, which is what a layout is for.
//
// What the layout also has that the scaffold does not is the names — a reading of
// what the file means — and the forms the scaffold leaves commented.
func TestTheWorkedExampleDerivesLedgersRecordsAndAlternatives(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "example", "posting.cpy"))
	if err != nil {
		t.Fatalf("reading the worked example's copybook: %v", err)
	}

	// The path as `example/ledger.sexpr` spells it, which is what makes the
	// two comparable.
	derived := deriveOf(t, book("posting.cpy", string(source)))

	file, err := layout.ParseFile(filepath.Join("..", "..", "example", "ledger.sexpr"))
	if err != nil {
		t.Fatalf("parsing the worked example's layout: %v", err)
	}

	written, err := layoutmodel.ReadRecords(file)
	if err != nil {
		t.Fatalf("reading the worked example's records: %v", err)
	}

	var want []string

	for _, record := range written {
		if record.Path != "posting.cpy" {
			continue
		}

		chosen := make([]string, 0, len(record.Alternatives))
		for _, alternative := range record.Alternatives {
			// The whole containment path rather than the leaf name. A
			// reference into the wrong group has the same leaf, so comparing
			// leaves would let this test pass over a scaffold naming bytes
			// nobody meant.
			chosen = append(chosen, strings.Join(alternative.Path, " "))
		}

		want = append(want, strings.Join(chosen, "+"))
	}

	var got []string

	for _, record := range derived.records {
		chosen := make([]string, 0, len(record.alternatives))
		for _, alternative := range record.alternatives {
			if len(alternative.path) == 0 {
				t.Fatalf("%s carries an alternative with no path, which no layout could name", record.name)
			}

			chosen = append(chosen, strings.Join(alternative.path, " "))
		}

		got = append(got, strings.Join(chosen, "+"))
	}

	if len(got) != 6 {
		t.Fatalf("derived %d record types from posting.cpy, want the six the two independent runs multiply out to", len(got))
	}

	if len(want) != 2 {
		t.Fatalf("the layout names %d combinations of posting.cpy, want the two whose alternatives are all REDEFINES", len(want))
	}

	// Compared as sets. Which combination is written first is the layout
	// author's and the derivation's own order, and neither is a statement
	// about the copybook; which alternatives are chosen is.
	slices.Sort(got)

	for _, combination := range want {
		if !slices.Contains(got, combination) {
			t.Errorf("the layout names the combination\n%s\nand the scaffold derived only\n%v", combination, got)
		}
	}
}
