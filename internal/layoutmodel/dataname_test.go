// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/definitions"
	"github.com/Zaba505/cpybkc/internal/layout"
)

// dataNamePositions are the layout positions that name a COBOL item, each as a
// layout writing name there and the reading that hands the name back.
//
// They are the positions docs/layout/SPEC.md gives the data-name sort outside
// an entry body (#404): an item reference's path, `copybook`'s and
// `definitions`' top-level name, the alternative of both `arm` forms, and
// `take-alternative`'s alternative.
var dataNamePositions = []struct {
	name   string
	layout func(name string) string
	read   func(t *testing.T, source string) (string, error)
}{
	{
		name: "an item reference's path",
		layout: func(name string) string {
			return oneRecord("DETAIL", fmt.Sprintf("(discriminate DETAIL (equals (item DETAIL D-KEY %s) \"20\"))", name))
		},
		read: func(t *testing.T, source string) (string, error) {
			read, err := discriminationOf(t, source)
			if err != nil {
				return "", err
			}

			return read.Records[0].Strategy.Item.Name(), nil
		},
	},
	{
		name: "copybook's top-level name",
		layout: func(name string) string {
			return fmt.Sprintf("(record ORDER (copybook \"cpy/orders.cpy\" %s))", name)
		},
		read: recordItem,
	},
	{
		name: "a definitions file's top-level name",
		layout: func(name string) string {
			return fmt.Sprintf("(record POSTING (definitions \"posting.cpy.sexpr\" %s))", name)
		},
		read: recordItem,
	},
	{
		name: "a definitions block's top-level name",
		layout: func(name string) string {
			return fmt.Sprintf("(definitions-block TRAILER (entry 01 X))\n(record TRAILER (definitions TRAILER %s))", name)
		},
		read: recordItem,
	},
	{
		name: "a discriminated arm's alternative",
		layout: func(name string) string {
			return oneRecord("POLICY", strings.Join([]string{
				"(discriminate POLICY single-record-type)",
				"(discriminate-variant (item POLICY PL-ENTRIES PL-BODY-MOTOR)",
				"  (arm PL-BODY-MOTOR (equals (item POLICY PL-ENTRIES PL-KIND) \"M\"))",
				fmt.Sprintf("  (arm %s (equals (item POLICY PL-ENTRIES PL-KIND) \"P\")))", name),
			}, "\n"))
		},
		read: func(t *testing.T, source string) (string, error) {
			read, err := discriminationOf(t, source)
			if err != nil {
				return "", err
			}

			return read.Variants[0].Arms[1].Alternative, nil
		},
	},
	{
		name: "a scheduled arm's alternative",
		layout: func(name string) string {
			return scheduled(strings.Join([]string{
				"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
				"  (arm ADR-HOME 1)",
				fmt.Sprintf("  (arm %s 2))", name),
			}, "\n"))
		},
		read: func(t *testing.T, source string) (string, error) {
			read, err := discriminationOf(t, source)
			if err != nil {
				return "", err
			}

			return read.Schedules[0].Arms[1].Alternative, nil
		},
	},
	{
		name: "take-alternative's alternative",
		layout: func(name string) string {
			return oneRecord("ADDR",
				"(discriminate ADDR single-record-type)",
				fmt.Sprintf("(take-alternative (item ADDR ADR-ENTRY ADR-HOME) %s)", name),
			)
		},
		read: func(t *testing.T, source string) (string, error) {
			read, err := discriminationOf(t, source)
			if err != nil {
				return "", err
			}

			return read.Taken[0].Alternative, nil
		},
	},
}

// recordItem reads the last record a layout defines and hands back the
// top-level name it is bound to.
func recordItem(t *testing.T, source string) (string, error) {
	t.Helper()

	records, err := recordsOf(t, source)
	if err != nil {
		return "", err
	}

	return records[len(records)-1].Item, nil
}

// TestAWordIsTheNameItSpells is #404's criterion in every position: `nil` is a
// COBOL name `cobol-go` reads and `sexpr-go` does not read as a symbol, so a
// layout writes it `(word "nil")`, and the reader hands back the name `nil` —
// which is what lets an item named `nil` be bound and referenced at all.
func TestAWordIsTheNameItSpells(t *testing.T) {
	t.Parallel()

	for _, position := range dataNamePositions {
		t.Run(position.name, func(t *testing.T) {
			t.Parallel()

			got, err := position.read(t, position.layout(`(word "nil")`))
			if err != nil {
				t.Fatalf("the layout does not read:\n%v", err)
			}

			if got != "nil" {
				t.Errorf("read the name %q, want %q", got, "nil")
			}
		})
	}
}

// TestABareSymbolStillReads is the other half of "every existing layout reads
// unchanged": the symbol spelling is the ordinary one in every position the
// data-name sort was given.
func TestABareSymbolStillReads(t *testing.T) {
	t.Parallel()

	for _, position := range dataNamePositions {
		t.Run(position.name, func(t *testing.T) {
			t.Parallel()

			got, err := position.read(t, position.layout("PST-DEBIT"))
			if err != nil {
				t.Fatalf("the layout does not read:\n%v", err)
			}

			if got != "PST-DEBIT" {
				t.Errorf("read the name %q, want %q", got, "PST-DEBIT")
			}
		})
	}
}

// TestANeedlessWordGivesTheSymbolSpelling holds every position to the leaf
// rule's one spelling per name: `PST-DEBIT` reads as a symbol, so `(word
// "PST-DEBIT")` is a second spelling of it, refused in the entry reader's words
// with the spelling the adopter should write instead.
func TestANeedlessWordGivesTheSymbolSpelling(t *testing.T) {
	t.Parallel()

	const want = `(word "PST-DEBIT") is needless: PST-DEBIT reads as a symbol, and a data-name that does is written bare`

	for _, position := range dataNamePositions {
		t.Run(position.name, func(t *testing.T) {
			t.Parallel()

			source := position.layout(`(word "PST-DEBIT")`)

			got, err := position.read(t, source)
			if err == nil {
				t.Fatalf("the layout read, as the name %q", got)
			}

			var needless *definitions.NeedlessWordError
			if !errors.As(err, &needless) {
				t.Fatalf("want a NeedlessWordError; got:\n%v", err)
			}

			if msg := needless.Diagnostic().Message; msg != want {
				t.Errorf("the diagnostic says %q, want %q", msg, want)
			}

			// The span is the `word` form, which is what the adopter rewrites.
			column := strings.Index(lastLine(source), `(word "PST-DEBIT")`) + 1
			at := layout.Pos{File: "layout.sexpr", Line: strings.Count(source, "\n") + 1, Column: column}

			if needless.Pos != at {
				t.Errorf("the diagnostic is at %s, want %s", needless.Pos, at)
			}
		})
	}
}

// lastLine is the last line of source, which is where every position above
// writes the name under test.
func lastLine(source string) string {
	return source[strings.LastIndex(source, "\n")+1:]
}

// TestAWordInAPathIsQuotedAsWritten is what a diagnostic naming the item says:
// the reference an adopter can search their layout for, which for `nil` is the
// `word` form and not a bare `nil` they could not have written.
func TestAWordInAPathIsQuotedAsWritten(t *testing.T) {
	t.Parallel()

	source := oneRecord("DETAIL", `(discriminate DETAIL (equals (item DETAIL (word "nil") D-KEY) "20"))`)

	read, err := discriminationOf(t, source)
	if err != nil {
		t.Fatalf("the layout does not read:\n%v", err)
	}

	ref := read.Records[0].Strategy.Item
	if want := []string{"nil", "D-KEY"}; strings.Join(ref.Path, " ") != strings.Join(want, " ") {
		t.Errorf("the path is %q, want %q", ref.Path, want)
	}

	if got, want := ref.String(), `(item DETAIL (word "nil") D-KEY)`; got != want {
		t.Errorf("rendered as %s, want %s", got, want)
	}
}

// TestAWordHoldingNoTextIsTheFormsOwnFault is the shape a position's own error
// answers: a `word` that is not `(word "<spelling>")` names no data-name, and
// the message is the one that position gives anything else written there.
func TestAWordHoldingNoTextIsTheFormsOwnFault(t *testing.T) {
	t.Parallel()

	_, err := recordsOf(t, `(record ORDER (copybook "cpy/orders.cpy" (word nil-ish)))`)

	var form *CopybookFormError
	if !errors.As(err, &form) {
		t.Fatalf("want a CopybookFormError; got:\n%v", err)
	}

	if form.Found != `form "word"` {
		t.Errorf("the fault found %s, want form \"word\"", form.Found)
	}
}
