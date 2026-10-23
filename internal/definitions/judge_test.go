// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package definitions_test

import (
	"errors"
	"strings"
	"testing"

	cobol "github.com/Zaba505/cobol-go"

	"github.com/Zaba505/cpybkc/internal/definitions"
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
)

// read reads a body the schema admits, failing the test on a fault.
func read(t *testing.T, body []layout.Node) []definitions.Entry {
	t.Helper()

	entries, err := definitions.Read(body)
	if err != nil {
		t.Fatalf("read:\n%s", diag.Render(err))
	}

	return entries
}

// refusals is what Judge refused, every one an InadmissibleEntryError.
func refusals(t *testing.T, err error) []*definitions.InadmissibleEntryError {
	t.Helper()

	if err == nil {
		t.Fatal("Judge admitted the body, want a refusal")
	}

	var all []*definitions.InadmissibleEntryError

	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("Judge returned %v, want joined refusals", err)
	}

	for _, err := range joined.Unwrap() {
		var refusal *definitions.InadmissibleEntryError
		if !errors.As(err, &refusal) {
			t.Fatalf("Judge returned %v, want an InadmissibleEntryError", err)
		}

		all = append(all, refusal)
	}

	return all
}

// TestTheJudgeRefuses is #395's validity rule refusing what the notation's
// shape admits and `cobol-go` would not read back as itself. Each body passes
// the schema check and the reader, so the refusal is the judge's, and it is a
// diagnostic at the entry quoting the printed COBOL and what `cobol-go` said.
func TestTheJudgeRefuses(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		entry   string
		printed string
		reason  string
	}{
		"an 88 carrying a pic": {
			entry:   `(entry 88 IS-A (pic "X") (value "'A'"))`,
			printed: "88 IS-A PIC X VALUE 'A'.",
			reason:  "cobol.Parse refuses it",
		},
		"two literals in an ordinary item's value": {
			entry:   `(entry 05 CODE (pic "X") (value "'A'" "'B'"))`,
			printed: "05 CODE PIC X VALUE 'A' 'B'.",
			reason:  "cobol.Parse refuses it",
		},
		"a sign position in lower case": {
			entry:  `(entry 05 AMOUNT (pic "S9") (sign leading))`,
			reason: "cobol.Print refuses it",
		},
		"a usage cobol-go does not know": {
			entry:   `(entry 05 AMOUNT (pic "S9") (usage COMP-9))`,
			printed: "05 AMOUNT PIC S9 USAGE COMP-9.",
			reason:  "cobol.Parse refuses it",
		},
		"a name holding a period": {
			entry:   `(entry 05 A.B (pic "X"))`,
			printed: "05 A.B PIC X.",
		},
		"a direction in lower case": {
			entry:   `(entry 05 AMOUNT (pic "S9(4)") (usage COMP) (synchronized left))`,
			printed: "05 AMOUNT PIC S9(4) USAGE COMP SYNCHRONIZED left.",
			reason:  `which cobol-go reads back as "05 AMOUNT PIC S9(4) USAGE COMP SYNCHRONIZED LEFT."`,
		},
		"a literal renamed": {
			entry:   `(entry 66 ALIAS (renames (through "'A'" CODE)))`,
			printed: "66 ALIAS RENAMES 'A' THROUGH CODE.",
			reason:  "cobol.Parse refuses it",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			body := "(entry 01 REC)\n" + test.entry + "\n"

			_, err := definitions.Judge(read(t, fileBody(t, body)))

			got := refusals(t, err)
			if len(got) != 1 {
				t.Fatalf("Judge refused %d entries, want 1:\n%s", len(got), diag.Render(err))
			}

			refusal := got[0]

			if want := (layout.Pos{File: "posting.cpy.sexpr", Line: 2, Column: 1}); refusal.Pos != want {
				t.Errorf("the refusal is at %s, want %s", refusal.Pos, want)
			}

			if refusal.Printed != test.printed {
				t.Errorf("the refusal quotes %q, want %q", refusal.Printed, test.printed)
			}

			if !strings.Contains(refusal.Reason, test.reason) {
				t.Errorf("the refusal says %q, want it to say %q", refusal.Reason, test.reason)
			}

			if test.printed != "" && !strings.Contains(err.Error(), test.printed) {
				t.Errorf("the diagnostic reads %q, want it to quote the printed COBOL", err.Error())
			}
		})
	}
}

// TestTheJudgeRefusesAPictureHoldingAClause is `(pic "X(3) OCCURS 4")`, which
// prints as a picture and an OCCURS and reads back as both. The reader refuses
// the text first, as more than one token; the judge is shown it by building
// the model directly, to hold that the equality and not only the parse is
// what refuses it.
func TestTheJudgeRefusesAPictureHoldingAClause(t *testing.T) {
	t.Parallel()

	const body = "(entry 01 REC)\n(entry 05 CODE (pic \"X(3) OCCURS 4\"))\n"

	var literal *definitions.LiteralTextError
	if _, err := definitions.Read(fileBody(t, body)); !errors.As(err, &literal) {
		t.Errorf("the reader returned %v, want a LiteralTextError", err)
	}

	at := layout.Pos{File: "posting.cpy.sexpr", Line: 2, Column: 1}

	entries := []definitions.Entry{
		{Pos: layout.Pos{File: at.File, Line: 1, Column: 1}, Level: definitions.Level{Value: 1}, Name: definitions.DataName{Name: "REC"}},
		{
			Pos:     at,
			Level:   definitions.Level{Value: 5},
			Name:    definitions.DataName{Name: "CODE"},
			Clauses: []definitions.Clause{definitions.Picture{Picture: definitions.Text{Value: "X(3) OCCURS 4"}}},
		},
	}

	_, err := definitions.Judge(entries)

	got := refusals(t, err)
	if len(got) != 1 || got[0].Pos != at {
		t.Fatalf("Judge refused %v, want the 05 CODE alone", err)
	}

	if want := `which cobol-go reads back as "05 CODE PIC X(3) OCCURS 4 TIMES."`; got[0].Reason != want {
		t.Errorf("the refusal says %q, want %q", got[0].Reason, want)
	}
}

// TestTheJudgeReportsEveryRefusal requires one diagnostic per refused entry,
// not the first alone, and an admitted entry between them to draw none.
func TestTheJudgeReportsEveryRefusal(t *testing.T) {
	t.Parallel()

	const body = `(entry 01 REC)
(entry 05 A (pic "X") (usage COMP-9))
(entry 05 B (pic "X"))
(entry 05 C (pic "X") (sign trailing))
`

	_, err := definitions.Judge(read(t, fileBody(t, body)))

	got := refusals(t, err)

	if len(got) != 2 || got[0].Pos.Line != 2 || got[1].Pos.Line != 4 {
		t.Fatalf("Judge refused %+v, want lines 2 and 4", got)
	}
}

// TestAJudgedBlockPointsIntoTheLayout is the judge's span in a block: the
// entry's line in the layout, which is where the adopter wrote it.
func TestAJudgedBlockPointsIntoTheLayout(t *testing.T) {
	t.Parallel()

	const body = "  (entry 01 TRAILER)\n  (entry 05 T-CODE (pic \"X\") (usage COMP-9))\n"

	_, err := definitions.Judge(read(t, blockBody(t, body)))

	got := refusals(t, err)

	if want := (layout.Pos{File: "ledger.sexpr", Line: baseLines + 3, Column: 3}); len(got) != 1 || got[0].Pos != want {
		t.Fatalf("Judge refused %+v, want one refusal at %s", got, want)
	}
}

// TestAKeyOrderIsAscendingOrDescending is the one closed-set value Construct
// refuses rather than handing to the judge, since `cobol-go` holds it as a
// boolean and an unknown one would read back as DESCENDING.
func TestAKeyOrderIsAscendingOrDescending(t *testing.T) {
	t.Parallel()

	const body = "(entry 01 REC (occurs \"2\" (key ascending K)))\n(entry 05 K (pic \"X\"))\n"

	_, err := definitions.Construct(read(t, fileBody(t, body)))

	var order *definitions.KeyOrderError
	if !errors.As(err, &order) {
		t.Fatalf("Construct returned %v, want a KeyOrderError", err)
	}

	if order.Order != "ascending" || order.Pos.Line != 1 || order.Pos.Column != strings.Index(body, "ascending")+1 {
		t.Errorf("the refusal is %+v, want ascending at 1:%d", order, strings.Index(body, "ascending")+1)
	}
}

// TestConstructPositionsEveryNode requires every cobol.Pos Construct sets to
// be the atom or form it came from — in a definitions file and, for the same
// entries, in a layout — and literal text to be classified by cobol.Tokenize.
func TestConstructPositionsEveryNode(t *testing.T) {
	t.Parallel()

	const body = `(entry 01 REC (global))
(entry 05 CODE (pic "X") (value "'A'"))
(entry 88 IS-A (value (through "'A'" "'C'") SPACE "12"))
(entry 05 TABLE (occurs "1" (to "4") (depending-on CODE) (key ASCENDING CELL) (indexed-by IX)))
(entry 10 CELL (pic "X"))
(entry 05 FILLER (pic "X"))
(entry 66 FILLER (renames (through CODE TABLE)))
(entry 88 FILLER (value "'Z'"))
`

	lines := strings.Split(body, "\n")

	// at is where needle is written on line, as `cobol-go` holds a position.
	at := func(line int, needle string) cobol.Pos {
		column := strings.Index(lines[line-1], needle)
		if column < 0 {
			t.Fatalf("line %d holds no %q", line, needle)
		}

		return cobol.Pos{Line: line, Column: column + 1}
	}

	check := func(t *testing.T, built []*cobol.DataDescriptionEntry, shift cobol.Pos) {
		t.Helper()

		moved := func(p cobol.Pos) cobol.Pos {
			return cobol.Pos{Line: p.Line + shift.Line, Column: p.Column + shift.Column}
		}

		expect := func(what string, got, want cobol.Pos) {
			t.Helper()

			if got != moved(want) {
				t.Errorf("%s is at %d:%d, want %d:%d", what, got.Line, got.Column, moved(want).Line, moved(want).Column)
			}
		}

		expect("the 01's level", built[0].Pos, at(1, "01"))
		expect("the 01's name", built[0].Name.Pos, at(1, "REC"))
		expect("(global)", built[0].Clauses[0].(*cobol.GlobalClause).Pos, at(1, "(global)"))

		value := built[1].Clauses[1].(*cobol.ValueClause)
		expect("the 05's value", value.Pos, at(2, "(value"))

		literal, ok := value.Values[0].From.(*cobol.StringLiteral)
		if !ok {
			t.Fatalf("'A' is built as %T, want a StringLiteral", value.Values[0].From)
		}

		expect("'A'", literal.Pos, at(2, `"'A'"`))

		condition := built[2].Clauses[0].(*cobol.ValueClause)
		expect("the range's end", condition.Values[0].Through.(*cobol.StringLiteral).Pos, at(3, `"'C'"`))
		expect("SPACE", condition.Values[1].From.(*cobol.Word).Pos, at(3, "SPACE"))

		number, ok := condition.Values[2].From.(*cobol.NumericLiteral)
		if !ok {
			t.Fatalf("12 is built as %T, want a NumericLiteral", condition.Values[2].From)
		}

		expect("12", number.Pos, at(3, `"12"`))

		occurs := built[3].Clauses[0].(*cobol.OccursClause)
		expect("the count", occurs.Min.Pos, at(4, `"1"`))
		expect("the bound", occurs.Max.Pos, at(4, `"4"`))
		expect("depending-on's item", occurs.DependingOn.Pos, at(4, "CODE"))
		expect("the key", occurs.Keys[0].Pos, at(4, "(key"))
		expect("the key's item", occurs.Keys[0].Name.Pos, at(4, "CELL"))
		expect("the index", occurs.IndexedBy.Pos, at(4, "IX"))

		if !built[5].Filler || built[5].Name != nil {
			t.Errorf("the 05 FILLER is built as %+v, want Filler and no name", built[5])
		}

		renames := built[6].Clauses[0].(*cobol.RenamesClause)
		if built[6].Filler || built[6].Name == nil || built[6].Name.Value != "FILLER" {
			t.Errorf("the 66 FILLER is built as %+v, want a name", built[6])
		}

		expect("the renamed range's end", renames.Through.Pos, at(7, "TABLE"))

		if built[7].Filler || built[7].Name == nil || built[7].Name.Value != "FILLER" {
			t.Errorf("the 88 FILLER is built as %+v, want a name", built[7])
		}
	}

	t.Run("file", func(t *testing.T) {
		t.Parallel()

		built, err := definitions.Construct(read(t, fileBody(t, body)))
		if err != nil {
			t.Fatalf("construct:\n%s", diag.Render(err))
		}

		check(t, built, cobol.Pos{})
	})

	t.Run("block", func(t *testing.T) {
		t.Parallel()

		built, err := definitions.Construct(read(t, blockBody(t, body)))
		if err != nil {
			t.Fatalf("construct:\n%s", diag.Render(err))
		}

		check(t, built, cobol.Pos{Line: baseLines + 1})
	})
}
