// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"strings"
	"testing"

	cobol "github.com/Zaba505/cobol-go"
	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// recordOf builds the first record of a copybook source, driving `cobol-go`'s
// real parser so that no test hand-assembles an AST the reader would never
// produce.
func recordOf(t *testing.T, src string) *copybook.Field {
	t.Helper()

	file, err := cobol.Parse(strings.NewReader(src), cobol.WithFragment())
	if err != nil {
		t.Fatalf("parsing the copybook: %v", err)
	}
	if file.Fragment == nil {
		t.Fatal("parsing the copybook: no fragment")
	}

	records, err := copybook.Build(file.Fragment.Entries)
	if err != nil {
		t.Fatalf("building the copybook: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("building the copybook: no records")
	}
	return records[0]
}

// fieldNamed finds a field of the record by name, for a test naming what a
// layout would name.
func fieldNamed(t *testing.T, record *copybook.Field, name string) *copybook.Field {
	t.Helper()

	var found *copybook.Field
	var walk func(*copybook.Field)
	walk = func(field *copybook.Field) {
		if found == nil && !field.Filler && field.Name == name {
			found = field
		}
		for _, child := range field.Children {
			walk(child)
		}
	}
	walk(record)

	if found == nil {
		t.Fatalf("the copybook declares no %s", name)
	}
	return found
}

// mainframe is the encoding profile every test here states unless it is testing
// the profile: the four axes a file written and read on a mainframe has.
//
// A test states one because [Resolve] requires one — `codec` defaults no axis
// and neither does this package — and states this one because a test about
// widths and containment should be reading the same profile as the next test
// about widths and containment.
func mainframe() layoutmodel.Axes {
	return layoutmodel.Axes{
		Charset:        layoutmodel.CP037,
		SignConvention: layoutmodel.SignEBCDIC,
		ByteOrder:      layoutmodel.BigEndian,
		FloatFormat:    layoutmodel.HFP,
	}
}

// resolveAll resolves a copybook source under IBM Enterprise COBOL, which is the
// dialect every test here states unless it is testing the dialect.
//
// The reading is that dialect's too: IBM Enterprise COBOL slides
// unconditionally, so a test about a copybook laid out under it reads its tables
// the way that compiler does. A test about the fork itself states its own
// (odo_test.go).
func resolveAll(t *testing.T, src string, redefines ...Redefine) []*Record {
	t.Helper()

	records, err := Resolve(recordOf(t, src), Options{
		Copybook:  "test.cpy",
		Dialect:   copybook.IBMEnterprise(),
		Encoding:  mainframe(),
		Reading:   layoutmodel.ODOSlide,
		Redefines: redefines,
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	return records
}

// resolveOne resolves a copybook the caller expects to have exactly one
// alternative.
func resolveOne(t *testing.T, src string, redefines ...Redefine) *Record {
	t.Helper()

	records := resolveAll(t, src, redefines...)
	if len(records) != 1 {
		t.Fatalf("resolved to %d records, want 1", len(records))
	}
	return records[0]
}

// selects is a strategy that lowers into a predicate, for an arm a test needs
// selected by something rather than by a particular thing. Compiling one into an
// IR predicate node is #37's, so what it tests is not this package's business.
func selects(value string) layoutmodel.Strategy {
	return layoutmodel.Strategy{
		Kind:     layoutmodel.Equals,
		Literals: []layoutmodel.Literal{{Kind: layoutmodel.TextLiteral, Text: value}},
	}
}

// widthOf is the resolved width of the field named name.
func widthOf(t *testing.T, record *Record, name string) int {
	t.Helper()

	node := record.Find(name)
	if node == nil {
		t.Fatalf("the resolved record holds no %s", name)
	}
	return node.Width()
}

// positionOf is the resolved position of the field named name.
func positionOf(t *testing.T, record *Record, name string) int {
	t.Helper()

	node := record.Find(name)
	if node == nil {
		t.Fatalf("the resolved record holds no %s", name)
	}
	at, ok := record.Position(node)
	if !ok {
		t.Fatalf("%s is not in the record it came from", name)
	}
	return at
}

func TestResolveRejectsNoRecordAtAll(t *testing.T) {
	t.Parallel()

	if _, err := Resolve(nil, Options{Dialect: copybook.IBMEnterprise()}); err != ErrNilRecord {
		t.Fatalf("Resolve(nil) = %v, want %v", err, ErrNilRecord)
	}
}

func TestResolveReportsACopybookItCannotLayOut(t *testing.T) {
	t.Parallel()

	// An elementary DISPLAY item with no PICTURE has no width, which is
	// `cobol-go`'s to report: this package does not measure storage a second
	// time, so it has nothing of its own to say about one.
	_, err := Resolve(recordOf(t, "01 R.\n   05 A.\n"), Options{Dialect: copybook.IBMEnterprise(), Encoding: mainframe()})
	if err == nil {
		t.Fatal("resolved a record whose width cannot be determined")
	}
}

// TestResolveOrdersMembersByRecordOrder holds the member list to the one thing
// it is: the order in which members occupy bytes. Ordering is data here, not a
// convention a consumer restores by sorting, because it is half of the only
// statement of where anything is.
func TestResolveOrdersMembersByRecordOrder(t *testing.T) {
	t.Parallel()

	record := resolveOne(t, `01 TXN.
   05 ID PIC X(4).
   05 AMT PIC S9(5) COMP-3.
   05 QTY PIC S9(4) COMP.
   05 NAME PIC X(6).
`)

	want := []string{"ID", "AMT", "QTY", "NAME"}
	got := make([]string, 0, len(record.Root.Members))
	for _, member := range record.Root.Members {
		got = append(got, member.Field.Name)
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("members are %v, want %v", got, want)
	}
}

// TestNodeWidthIsNotCarriedForAGroupOrAVariant is the structural half of
// docs/ir/SPEC.md's refusal to state a position twice: a group's width is the
// sum of its members' and a variant's is its arms' common extent, and neither is
// a number this package could store and get wrong.
func TestNodeWidthIsNotCarriedForAGroupOrAVariant(t *testing.T) {
	t.Parallel()

	record := resolveOne(t, `01 R.
   05 G.
      10 A PIC X(4).
      10 B PIC X(6).
`)

	group := record.Find("G")
	if group.width != 0 {
		t.Fatalf("the group carries a width of %d", group.width)
	}
	if got := group.Width(); got != 10 {
		t.Fatalf("the group's width is %d, want 10", got)
	}

	group.Members = group.Members[:1]
	if got := group.Width(); got != 4 {
		t.Fatalf("after dropping a member the group's width is %d, want 4", got)
	}
}

// TestRecordPositionCountsOneOccurrenceOfEveryEnclosingTable is the clause that
// costs nothing today and buys the arithmetic staying the same later: the sum
// lands on the first occurrence.
func TestRecordPositionCountsOneOccurrenceOfEveryEnclosingTable(t *testing.T) {
	t.Parallel()

	record := resolveOne(t, `01 R.
   05 HDR PIC X(4).
   05 T OCCURS 5 TIMES.
      10 K PIC X(2).
      10 V PIC X(3).
   05 TRAILER PIC X(6).
`)

	for _, tc := range []struct {
		name string
		want int
	}{
		{name: "HDR", want: 0},
		{name: "T", want: 4},
		{name: "K", want: 4},
		{name: "V", want: 6},
		{name: "TRAILER", want: 4 + 5*5},
	} {
		if got := positionOf(t, record, tc.name); got != tc.want {
			t.Errorf("%s is at %d, want %d", tc.name, got, tc.want)
		}
	}

	if got := record.Extent(); got != 4+5*5+6 {
		t.Fatalf("the record's extent is %d, want %d", got, 4+5*5+6)
	}
}

// TestRepetitionIsCarriedOnlyByAnItemThatRepeats, with the bounds a DEPENDING ON
// clause declared beside the count the layout was computed at. Turning the
// second into the IR's reference, and the extent that moves with it, is #35's;
// what this package needs from it is that the count is a reference at all.
func TestRepetitionIsCarriedOnlyByAnItemThatRepeats(t *testing.T) {
	t.Parallel()

	record := resolveOne(t, `01 R.
   05 N PIC 9(2).
   05 ONCE PIC X(4).
   05 FIXED PIC X(2) OCCURS 3 TIMES.
   05 VARYING PIC X(5) OCCURS 1 TO 6 TIMES DEPENDING ON N.
`)

	if node := record.Find("ONCE"); node.Repetition != nil {
		t.Error("an item with no OCCURS clause carries a repetition")
	}

	fixed := record.Find("FIXED").Repetition
	if fixed == nil || fixed.Count != 3 || fixed.Min != 3 || fixed.Max != 3 {
		t.Fatalf("the fixed table's repetition is %+v, want three occurrences with no bounds to check", fixed)
	}
	if fixed.Reference() {
		t.Error("the fixed table's count is a reference")
	}

	varying := record.Find("VARYING").Repetition
	if varying == nil || varying.Min != 1 || varying.Max != 6 {
		t.Fatalf("the varying table's bounds are %+v, want 1 to 6", varying)
	}
	if !varying.Reference() || varying.DependingOn.Name != "N" {
		t.Fatal("the varying table's count is not read from N")
	}
	// The layout is computed at the declared maximum, which is the storage a
	// compiler reserves for the table.
	if varying.Count != 6 {
		t.Errorf("the varying table was laid out at %d occurrences, want the declared maximum of 6", varying.Count)
	}
}

func TestRecordPositionReportsANodeItDoesNotHold(t *testing.T) {
	t.Parallel()

	record := resolveOne(t, "01 R.\n   05 A PIC X(4).\n")
	if _, ok := record.Position(&Node{Kind: KindField}); ok {
		t.Fatal("a node the record does not hold has a position in it")
	}
}
