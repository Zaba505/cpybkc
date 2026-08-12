// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"errors"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/layout"
)

// recordsOf is the whole pipeline a caller runs: parse the source, then read the
// record definitions out of it.
func recordsOf(t *testing.T, source string) ([]Record, error) {
	t.Helper()

	file, err := layout.Parse("layout.sexpr", strings.NewReader(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	return ReadRecords(file)
}

// TestARecordBindsANameToACopybookItem is the layer in one form: a name, the
// file it is in, and the item inside it.
func TestARecordBindsANameToACopybookItem(t *testing.T) {
	t.Parallel()

	records, err := recordsOf(t, "(record ORDER\n  (copybook \"cpy/orders.cpy\" ORDER-REC))\n")
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("the layout defines %d records, want 1", len(records))
	}

	record := records[0]
	if record.Name != "ORDER" {
		t.Errorf("the record is named %q, want %q", record.Name, "ORDER")
	}
	if record.Path != "cpy/orders.cpy" {
		t.Errorf("the record is bound to %q, want %q", record.Path, "cpy/orders.cpy")
	}
	if record.Item != "ORDER-REC" {
		t.Errorf("the record is bound to the item %q, want %q", record.Item, "ORDER-REC")
	}

	// Every part carries its own position, because the CLI reports the file and
	// the item against different lines: a copybook that will not open is the
	// `copybook` child's fault and a name nothing defines is the name's.
	for name, pos := range map[string]layout.Pos{
		"the form":     record.Pos,
		"the name":     record.NamePos,
		"the copybook": record.Copybook,
		"the path":     record.PathPos,
		"the item":     record.ItemPos,
	} {
		if pos.File != "layout.sexpr" || pos.Line == 0 {
			t.Errorf("%s is at %s, want a position in the layout", name, pos)
		}
	}
}

// TestRecordsComeBackInSourceOrder is what makes a descriptor a function of the
// layout: identifiers are assigned in the order the record forms are written,
// so a reader handing them back in any other order would make the IR depend on
// something the file does not say.
func TestRecordsComeBackInSourceOrder(t *testing.T) {
	t.Parallel()

	records, err := recordsOf(t, `(record HEADER  (copybook "a.cpy" HDR-REC))
(record DETAIL  (copybook "b.cpy" DTL-REC))
(record TRAILER (copybook "a.cpy" TRL-REC))
`)
	if err != nil {
		t.Fatalf("records: %v", err)
	}

	var names []string
	for _, record := range records {
		names = append(names, record.Name)
	}

	want := []string{"HEADER", "DETAIL", "TRAILER"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("the records read as %v, want %v", names, want)
	}
}

// TestTwoRecordsMayNameOneCopybookItem is docs/layout/SPEC.md's "Many records
// may name one copybook, and two may name one item": one `01`-level named twice
// and told apart by where it sits is the ordinary shape, not a fault.
func TestTwoRecordsMayNameOneCopybookItem(t *testing.T) {
	t.Parallel()

	records, err := recordsOf(t, `(record ORDER-OPEN  (copybook "cpy/orders.cpy" ORDER-REC))
(record ORDER-CLOSE (copybook "cpy/orders.cpy" ORDER-REC))
`)
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("the layout defines %d records, want 2", len(records))
	}
}

// TestOneNameStandsForOneBinding is the other direction, and the one that is
// refused: two forms defining one name would leave the order they are written
// in deciding which binding a reference to that name meant.
func TestOneNameStandsForOneBinding(t *testing.T) {
	t.Parallel()

	_, err := recordsOf(t, `(record ORDER (copybook "a.cpy" ORDER-REC))
(record ORDER (copybook "b.cpy" ORDER-REC))
`)

	var duplicate *DuplicateRecordError
	if !errors.As(err, &duplicate) {
		t.Fatalf("a name defined twice reads as %v, want a DuplicateRecordError", err)
	}
	if duplicate.Record != "ORDER" {
		t.Errorf("the fault names %q, want %q", duplicate.Record, "ORDER")
	}
	if duplicate.First.Line == 0 {
		t.Error("the fault does not say where the name was first defined")
	}
}

// TestALayoutDefiningNoRecordIsAFault is the form's arity, and it is not
// arithmetic: a layout defines the record types a file is made of.
func TestALayoutDefiningNoRecordIsAFault(t *testing.T) {
	t.Parallel()

	_, err := recordsOf(t, "(sequence (* ORDER))\n")

	var none *NoRecordsError
	if !errors.As(err, &none) {
		t.Fatalf("a layout defining no record reads as %v, want a NoRecordsError", err)
	}
	if none.File != "layout.sexpr" {
		t.Errorf("the fault names %q, want the layout", none.File)
	}
}

// TestAMalformedRecordIsReportedAgainstThePartThatIsWrong walks the shapes the
// form takes when it is written wrong, each answered by the message about the
// part rather than by one about the form.
func TestAMalformedRecordIsReportedAgainstThePartThatIsWrong(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		source string
		want   any
	}{
		"a name and no binding":  {"(record ORDER)\n", new(*RecordFormError)},
		"a binding and no name":  {"(record (copybook \"a.cpy\" ORDER-REC))\n", new(*RecordFormError)},
		"a name that is text":    {"(record \"ORDER\" (copybook \"a.cpy\" ORDER-REC))\n", new(*RecordFormError)},
		"a child of another tag": {"(record ORDER (rename \"a\"))\n", new(*ChildError)},
		"a path written bare":    {"(record ORDER (copybook a.cpy ORDER-REC))\n", new(*CopybookFormError)},
		"an item written as text": {
			"(record ORDER (copybook \"a.cpy\" \"ORDER-REC\"))\n", new(*CopybookFormError),
		},
		"a binding naming no item": {"(record ORDER (copybook \"a.cpy\"))\n", new(*CopybookFormError)},
		"a path of no characters": {
			"(record ORDER (copybook \"\" ORDER-REC))\n", new(*EmptyCopybookPathError),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			records, err := recordsOf(t, test.source)
			if err == nil {
				t.Fatalf("%s reads as %d sound records, want a fault", name, len(records))
			}
			if !errors.As(err, test.want) {
				t.Fatalf("%s reads as %v, want a %T", name, err, test.want)
			}
		})
	}
}

// TestEveryFaultIsReported is the rule every reader in this package keeps: a
// generated layout is generated wrong in the same way in many places at once,
// and a reader that reports one fault per run is a reader run once per fault.
func TestEveryFaultIsReported(t *testing.T) {
	t.Parallel()

	_, err := recordsOf(t, `(record ORDER (copybook "a.cpy"))
(record DETAIL (copybook 3 DTL-REC))
`)

	var form *CopybookFormError
	if !errors.As(err, &form) {
		t.Fatalf("two malformed bindings read as %v, want a CopybookFormError", err)
	}

	if got := len(strings.Split(err.Error(), "\n")); got != 2 {
		t.Errorf("two malformed bindings report %d faults, want 2: %v", got, err)
	}
}

// TestOtherLayersAreNotRead is the division every reader here keeps: a form
// belonging to another layer is not this reader's to report, because a second
// message about it would name the same line twice.
func TestOtherLayersAreNotRead(t *testing.T) {
	t.Parallel()

	records, err := recordsOf(t, `(encoding (charset nonsense))
(record ORDER (copybook "a.cpy" ORDER-REC))
(discriminate ORDER single-record-type)
`)
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("the layout defines %d records, want 1", len(records))
	}
}

// TestARecordChoosesItsAlternativesByName is docs/layout/SPEC.md's "Which
// alternative a record is", read: the children arrive in the order the form
// writes them, each carrying the reference the layout wrote.
func TestARecordChoosesItsAlternativesByName(t *testing.T) {
	t.Parallel()

	records, err := recordsOf(t, `(record TXN
  (copybook "cpy/txn.cpy" TXN-REC)
  (alternative (item TXN TXN-PURCHASE))
  (alternative (item TXN TXN-BODY TXN-CARD)))
`)
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("the layout defines %d records, want 1", len(records))
	}

	chosen := make([]string, 0, 2)
	for _, ref := range records[0].Alternatives {
		chosen = append(chosen, ref.String())
	}

	want := []string{"(item TXN TXN-PURCHASE)", "(item TXN TXN-BODY TXN-CARD)"}
	if strings.Join(chosen, " ") != strings.Join(want, " ") {
		t.Errorf("the record chooses %v, want %v", chosen, want)
	}
}

// TestARecordWithNoAlternativeChoosesNone is the ordinary case, stated so that
// the reading above cannot start applying to every record.
func TestARecordWithNoAlternativeChoosesNone(t *testing.T) {
	t.Parallel()

	records, err := recordsOf(t, "(record ORDER (copybook \"cpy/orders.cpy\" ORDER-REC))\n")
	if err != nil {
		t.Fatalf("records: %v", err)
	}

	if len(records[0].Alternatives) != 0 {
		t.Errorf("a record with no alternative child chooses %v", records[0].Alternatives)
	}
}

// TestReadRecordsRejectsAlternatives is the half of the choice the reader
// decides without a copybook. How many alternatives there are to choose among is
// `resolve`'s and is not here.
func TestReadRecordsRejectsAlternatives(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "an alternative rooted at another record",
			source: `(record TXN (copybook "cpy/txn.cpy" TXN-REC)
  (alternative (item OTHER TXN-PURCHASE)))`,
			want: "layout.sexpr:2:16: record \"TXN\" chooses alternative (item OTHER TXN-PURCHASE), which is " +
				"rooted at record \"OTHER\"; an alternative is an item of the record choosing it",
		},
		{
			name: "one alternative chosen twice",
			source: `(record TXN (copybook "cpy/txn.cpy" TXN-REC)
  (alternative (item TXN TXN-PURCHASE))
  (alternative (item TXN TXN-PURCHASE)))`,
			want: "layout.sexpr:3:3: alternative (item TXN TXN-PURCHASE) is chosen twice, and is chosen " +
				"first at layout.sexpr:2:3; a record chooses each alternative once",
		},
		{
			name: "an alternative carrying no reference",
			source: `(record TXN (copybook "cpy/txn.cpy" TXN-REC)
  (alternative))`,
			want: "layout.sexpr:2:3: an alternative is written (alternative <item-ref>), and this has no value",
		},
		{
			name: "a child the form does not admit",
			source: `(record TXN (copybook "cpy/txn.cpy" TXN-REC)
  (copybook "cpy/other.cpy" OTHER-REC))`,
			want: "layout.sexpr:2:3: form \"record\" admits alternative, and this is form \"copybook\"",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			read, err := recordsOf(t, testCase.source)
			if err == nil {
				t.Fatalf("the reader accepts %s", testCase.source)
			}

			if read != nil {
				t.Errorf("the reader hands back records beside the fault: %+v", read)
			}

			if got := err.Error(); got != testCase.want {
				t.Errorf("the reader reports\n%s\nwant\n%s", got, testCase.want)
			}
		})
	}
}
