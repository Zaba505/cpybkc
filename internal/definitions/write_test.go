// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package definitions_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	cobol "github.com/Zaba505/cobol-go"

	"github.com/Zaba505/cpybkc/internal/definitions"
)

// write is entries written at depth, failing the test on an error.
func write(t *testing.T, entries []*cobol.DataDescriptionEntry, depth int) string {
	t.Helper()

	var out bytes.Buffer

	if err := definitions.Write(&out, entries, depth); err != nil {
		t.Fatalf("write: %v", err)
	}

	return out.String()
}

// TestTheWriterWritesTheAppendix holds the writer to docs/layout/SPEC.md's
// "Appendix: A definitions file", which is in the canonical printed form: the
// appendix's copybook, written, is the appendix's text.
func TestTheWriterWritesTheAppendix(t *testing.T) {
	t.Parallel()

	const copybook = `01 POSTING-RECORD.
   05 PST-TYPE PIC X.
      88 PST-IS-DEBIT VALUE 'D'.
      88 PST-IS-CREDIT VALUE 'C'.
   05 PST-ACCOUNT PIC X(10).
   05 PST-DEBIT.
      10 PST-DR-AMOUNT PIC S9(9)V99 COMP-3.
      10 PST-DR-REASON PIC X(4) VALUE SPACES.
   05 PST-CREDIT REDEFINES PST-DEBIT.
      10 PST-CR-AMOUNT PIC S9(9)V99 USAGE IS COMP-3.
      10 PIC X(4).
   05 PST-REFS OCCURS 3 TIMES.
      10 PST-REF PIC X(5).
   05 FILLER PIC X(44).
`

	const want = `(entry 01 POSTING-RECORD)
  (entry 05 PST-TYPE (pic "X"))
    (entry 88 PST-IS-DEBIT (value "'D'"))
    (entry 88 PST-IS-CREDIT (value "'C'"))
  (entry 05 PST-ACCOUNT (pic "X(10)"))
  (entry 05 PST-DEBIT)
    (entry 10 PST-DR-AMOUNT (pic "S9(9)V99") (usage COMP-3))
    (entry 10 PST-DR-REASON (pic "X(4)") (value SPACES))
  (entry 05 PST-CREDIT (redefines PST-DEBIT))
    (entry 10 PST-CR-AMOUNT (pic "S9(9)V99") (usage COMP-3))
    (entry 10 FILLER (pic "X(4)"))
  (entry 05 PST-REFS (occurs "3"))
    (entry 10 PST-REF (pic "X(5)"))
  (entry 05 FILLER (pic "X(44)"))
`

	if got := write(t, parseFree(t, copybook), definitions.FileDepth); got != want {
		t.Errorf("the appendix's copybook writes as\n%s\nwant\n%s", got, want)
	}
}

// TestTheWriterIsDeterministic writes every fixture twice, and from a second
// parse, and requires one text.
func TestTheWriterIsDeterministic(t *testing.T) {
	t.Parallel()

	for _, fixture := range fixtures(t) {
		first := write(t, fixture.Entries, definitions.FileDepth)
		second := write(t, fixture.Entries, definitions.FileDepth)

		if first != second {
			t.Errorf("%s writes two texts", fixture.Name)
		}
	}
}

// TestABlockBodyIsTheFileIndented is the base depth's property: for every
// fixture, the body written for a block is the file's lines two spaces further
// in, line for line.
func TestABlockBodyIsTheFileIndented(t *testing.T) {
	t.Parallel()

	for _, fixture := range fixtures(t) {
		file := strings.Split(strings.TrimSuffix(write(t, fixture.Entries, definitions.FileDepth), "\n"), "\n")
		block := strings.Split(strings.TrimSuffix(write(t, fixture.Entries, definitions.BlockDepth), "\n"), "\n")

		if len(file) != len(block) {
			t.Errorf("%s writes %d lines for a file and %d for a block", fixture.Name, len(file), len(block))

			continue
		}

		for i := range file {
			if block[i] != "  "+file[i] {
				t.Errorf("%s line %d is %q in a block, want %q", fixture.Name, i+1, block[i], "  "+file[i])
			}
		}
	}
}

// TestTheDepthIsThePrinters holds the writer's indentation to the depth
// `cobol-go`'s printer gives each entry, for every fixture: the printer indents
// four spaces per depth and the canonical form two, and the depths are one
// rule (docs/layout/SPEC.md, "The printed form init writes").
func TestTheDepthIsThePrinters(t *testing.T) {
	t.Parallel()

	for _, fixture := range fixtures(t) {
		var printed bytes.Buffer

		if err := cobol.Print(&printed, &cobol.File{Fragment: &cobol.Fragment{Entries: fixture.Entries}}); err != nil {
			t.Fatalf("print %s: %v", fixture.Name, err)
		}

		var printerDepths []int

		for line := range strings.SplitSeq(printed.String(), "\n") {
			trimmed := strings.TrimLeft(line, " ")
			if trimmed == "" || strings.HasPrefix(trimmed, "*>") {
				continue
			}

			printerDepths = append(printerDepths, (len(line)-len(trimmed))/4)
		}

		written := strings.Split(strings.TrimSuffix(write(t, fixture.Entries, definitions.FileDepth), "\n"), "\n")

		if len(written) != len(printerDepths) {
			t.Fatalf("%s: the printer wrote %d entries and the writer %d", fixture.Name, len(printerDepths), len(written))
		}

		for i, line := range written {
			depth := (len(line) - len(strings.TrimLeft(line, " "))) / 2
			if depth != printerDepths[i] {
				t.Errorf("%s entry %d is at depth %d, and cobol-go's printer puts it at %d: %s", fixture.Name, i+1, depth, printerDepths[i], line)
			}
		}
	}
}

// alienClause is a clause kind the writer does not know. Embedding a known one
// gives it `cobol.DataClause`'s method, which is how a clause a pin bump adds
// would arrive: satisfying the interface and matching no case.
type alienClause struct{ *cobol.GlobalClause }

// TestTheWriterRefusesWhatItDoesNotKnow is the exhaustive switch's failure: a
// clause kind, or an operand kind, the writer has no form for is an error and
// writes nothing, never a transcription missing it.
func TestTheWriterRefusesWhatItDoesNotKnow(t *testing.T) {
	t.Parallel()

	tests := map[string]*cobol.DataDescriptionEntry{
		"an unknown clause": {
			Level:   1,
			Name:    &cobol.Word{Value: "REC"},
			Clauses: []cobol.DataClause{&cobol.GlobalClause{}, alienClause{&cobol.GlobalClause{}}},
		},
		"an identifier as a value": {
			Level: 1,
			Name:  &cobol.Word{Value: "REC"},
			Clauses: []cobol.DataClause{&cobol.ValueClause{Values: []cobol.ValueSpec{{
				From: &cobol.Identifier{Name: &cobol.Word{Value: "OTHER"}},
			}}}},
		},
		"a nil clause": {
			Level:   1,
			Name:    &cobol.Word{Value: "REC"},
			Clauses: []cobol.DataClause{(*cobol.PictureClause)(nil)},
		},
		"an unnamed condition-name": {
			Level:   88,
			Clauses: []cobol.DataClause{&cobol.ValueClause{Values: []cobol.ValueSpec{{From: &cobol.Word{Value: "SPACE"}}}}},
		},
	}

	for name, entry := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			err := definitions.Write(&out, []*cobol.DataDescriptionEntry{{Level: 1, Name: &cobol.Word{Value: "FIRST"}}, entry}, definitions.FileDepth)

			var unknown *definitions.UnknownNodeError
			if !errors.As(err, &unknown) {
				t.Fatalf("Write returned %v, want an UnknownNodeError", err)
			}

			if out.Len() != 0 {
				t.Errorf("Write wrote %q before refusing, want nothing", out.String())
			}
		})
	}
}

// TestConversionRefusesAConditionNameCobolGoMisreads holds the writer to
// docs/layout/SPEC.md's "A condition-name cobol-go misreads is refused, not
// transcribed": each of the three constructs is refused, naming the entry and
// the `cobol-go` story that lifts it, and a word and a literal the copybook
// wrote apart are transcribed as the two values they are.
func TestConversionRefusesAConditionNameCobolGoMisreads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, condition, story string
	}{
		{"a prefixed literal", "88 IS-NUL VALUE X'00'.", "Zaba505/cobol-go#140"},
		{"a national literal in a range", "88 IS-NAT VALUE 'A' THRU N'Z'.", "Zaba505/cobol-go#140"},
		{"an ALL literal", "88 IS-STARS VALUE ALL '*'.", "Zaba505/cobol-go#142"},
		{"VALUES ARE", "88 IS-AB VALUES ARE 'A' 'B'.", "Zaba505/cobol-go#135"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			entries := parseFree(t, "01 REC.\n   05 CODE PIC X.\n      "+test.condition+"\n")

			err := definitions.Write(&bytes.Buffer{}, entries, definitions.FileDepth)

			var misread *definitions.MisreadValueError
			if !errors.As(err, &misread) {
				t.Fatalf("Write returned %v, want a MisreadValueError", err)
			}

			if misread.Story != test.story {
				t.Errorf("the refusal names %s, want %s", misread.Story, test.story)
			}

			if message := err.Error(); !strings.Contains(message, test.story) || !strings.Contains(message, strings.Fields(test.condition)[1]) {
				t.Errorf("the refusal reads %q, want it to name the entry and %s", message, test.story)
			}
		})
	}

	t.Run("a word and a literal written apart", func(t *testing.T) {
		t.Parallel()

		entries := parseFree(t, "01 REC.\n   05 CODE PIC X.\n      88 IS-X VALUE X '00'.\n")

		const want = "(entry 01 REC)\n  (entry 05 CODE (pic \"X\"))\n    (entry 88 IS-X (value X \"'00'\"))\n"
		if got := write(t, entries, definitions.FileDepth); got != want {
			t.Errorf("wrote\n%s\nwant\n%s", got, want)
		}
	})
}
