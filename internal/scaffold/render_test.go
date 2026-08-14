// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package scaffold

import (
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// readLayout parses text as a layout and reads every layer of it, collecting the
// faults rather than stopping at the first.
//
// It is the layout reader as [github.com/Zaba505/cpybkc/internal/project] runs
// it, which is the point: what a scaffold is missing is reported by the reader
// that will have to accept the finished layout, and never by a second
// description of what a layout needs.
func readLayout(t *testing.T, text string) error {
	t.Helper()

	file, err := layout.Parse("scaffold.sexpr", strings.NewReader(text))
	if err != nil {
		t.Fatalf("the scaffold does not parse, which is the one thing it has to do:\n%v\n\n%s", err, text)
	}

	var faults diag.List

	_, err = layoutmodel.ReadRecords(file)
	faults.Fail(err)

	_, err = layoutmodel.ReadProfile(file)
	faults.Fail(err)

	_, err = layoutmodel.ReadFraming(file)
	faults.Fail(err)

	_, err = layoutmodel.ReadDiscrimination(file)
	faults.Fail(err)

	_, err = layoutmodel.ReadSequence(file)
	faults.Fail(err)

	_, err = layoutmodel.ReadRenames(file)
	faults.Fail(err)

	_, err = layoutmodel.ReadCopybookReading(file)
	faults.Fail(err)

	return faults.Err()
}

// uncomment strips the comment marks from the first commented form whose tag is
// tag, leaving everything else as it was.
//
// It is the gesture the scaffold is written for and the one the placeholders
// exist to survive: an adopter works down the reader's checklist by deleting
// `;;`, and the file has to report what they have not filled in rather than
// accepting it.
func uncomment(t *testing.T, text, tag string) string {
	t.Helper()

	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))

	depth := 0
	found := false

	for _, line := range lines {
		body, isComment := strings.CutPrefix(line, commentPrefix)

		switch {
		case isComment && depth > 0:
			out = append(out, body)
			depth += strings.Count(body, "(") - strings.Count(body, ")")
		case isComment && !found && opens(body, tag):
			found = true

			out = append(out, body)
			depth += strings.Count(body, "(") - strings.Count(body, ")")
		default:
			out = append(out, line)
		}
	}

	if !found {
		t.Fatalf("the scaffold carries no commented %s form:\n%s", tag, text)
	}

	return strings.Join(out, "\n")
}

// opens reports whether a comment's text is the first line of a form tagged tag.
//
// A form written on one line has its argument after a space and one broken over
// several has nothing after the tag at all, so both spellings are matched — and
// the space is what keeps `discriminate` from matching `discriminate-variant`.
func opens(body, tag string) bool {
	return body == "("+tag || strings.HasPrefix(body, "("+tag+" ")
}

// A fresh scaffold is the adopter's next checklist, and the whole of it comes
// out at once: docs/cli/SPEC.md, "The scaffold is deliberately incomplete".
func TestTheScaffoldParsesAndTheReaderReportsTheWholeRemainder(t *testing.T) {
	t.Parallel()

	derived := deriveOf(t, book("header.cpy", header), book("posting.cpy", posting))

	err := readLayout(t, string(derived.Bytes()))
	if err == nil {
		t.Fatal("the layout reader accepted a scaffold, which is not a layout")
	}

	rendered := diag.Render(err)

	// The missing encoding, the missing framing, one missing discriminator per
	// record, and the missing sequence — reported together rather than one per
	// run.
	for _, want := range []string{"encoding", "framing", "sequence"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the reader said nothing about the missing %s:\n%s", want, rendered)
		}
	}

	discriminators := strings.Count(rendered, "carries no discriminator")
	if discriminators != len(derived.records) {
		t.Errorf("the reader reported %d records with no discriminator, want one per record (%d):\n%s",
			discriminators, len(derived.records), rendered)
	}
}

// A placeholder that happened to be a legal value would let an adopter succeed
// with a layout they never finished writing, which is the one failure the
// incompleteness is there to prevent.
func TestEveryPlaceholderUncommentedIsAFaultTheReaderNames(t *testing.T) {
	t.Parallel()

	// Every commented form the scaffold can carry, over copybooks that reach
	// all of them: a record type per combination raises a rename, a variant
	// raises a discriminate-variant, and an OCCURS DEPENDING ON raises a
	// copybook-reading.
	text := string(deriveOf(t,
		book("posting.cpy", posting),
		book("order.cpy", table),
		book("lines.cpy", depending),
	).Bytes())

	for _, test := range []struct {
		form        string
		placeholder string
	}{
		{"encoding", charsetHole},
		{"framing", recfmHole},
		{"copybook-reading", readingHole},
		{"rename", substituteHole},
		{"discriminate", strategyHole},
		{"discriminate-variant", predicateHole},
		{"sequence", operatorHole},
	} {
		t.Run(test.form, func(t *testing.T) {
			t.Parallel()

			err := readLayout(t, uncomment(t, text, test.form))
			if err == nil {
				t.Fatalf("an uncommented %s with nothing filled in was accepted", test.form)
			}

			if got := diag.Render(err); !strings.Contains(got, test.placeholder) {
				t.Errorf("the reader did not name %s:\n%s", test.placeholder, got)
			}
		})
	}
}

// docs/cli/SPEC.md fixes the membership and the order of what a scaffold
// carries: it is the order docs/layout/SPEC.md tables the top-level forms in, so
// that a form the adopter uncomments is already where the format's own examples
// put it.
func TestTheFormsComeInTheOrderTheLayoutFormatTablesThem(t *testing.T) {
	t.Parallel()

	text := string(deriveOf(t,
		book("posting.cpy", posting),
		book("order.cpy", table),
		book("lines.cpy", depending),
	).Bytes())

	order := []string{
		"(encoding",
		"(framing",
		"(record ",
		"(copybook-reading",
		"(rename ",
		"(discriminate ",
		"(discriminate-variant ",
		"(sequence",
	}

	at := 0

	for _, form := range order {
		found := strings.Index(text[at:], form)
		if found < 0 {
			t.Fatalf("the scaffold does not carry %q, or carries it out of order:\n%s", form, text)
		}

		// Past the match, not to it: advancing only to the start would let the
		// next search re-scan text this one has already accepted, which asserts
		// non-decreasing positions rather than an order.
		at += found + len(form)
	}
}

// The one thing a scaffold has to be is readable as S-expressions, whatever its
// copybooks are called.
//
// A POSIX path may hold any byte but a slash and a NUL, so a newline or a tab in
// one is rare and legal — and writing it raw would produce the single failure
// the incompleteness is not allowed to be: a file reported as one lexical fault
// instead of as the checklist it is meant to be.
func TestAPathWithACharacterTheGrammarEscapesStillParses(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		path string
		want string
	}{
		{"a quote and a backslash", `odd"name\.cpy`, `"odd\"name\\.cpy"`},
		{"a newline and a tab", "odd\nname\t.cpy", `"odd\nname\t.cpy"`},
		{"a control character with no short escape", "odd\x01name.cpy", `"odd\u0001name.cpy"`},
		{"a non-ASCII name, written as itself", "copies/naïve.cpy", `"copies/naïve.cpy"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			text := string(deriveOf(t, book(test.path, header)).Bytes())

			if err := readLayout(t, text); err == nil {
				t.Fatal("the layout reader accepted a scaffold")
			}

			if !strings.Contains(text, "(copybook "+test.want+" LEDGER-HEADER)") {
				t.Errorf("the path was not written as %s:\n%s", test.want, text)
			}
		})
	}
}
