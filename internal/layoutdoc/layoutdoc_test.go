// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutdoc

import (
	"strings"
	"testing"
)

// TestEveryWorkedExampleIsFound is the check the three reading packages rest
// on. Each of them names a heading and expects a layout; a heading the document
// no longer carries is a rename nobody noticed, and it is a failure here rather
// than three identical failures further out.
func TestEveryWorkedExampleIsFound(t *testing.T) {
	t.Parallel()

	for _, heading := range []string{NativeExample, ConvertedExample} {
		example, err := Example(heading)
		if err != nil {
			t.Errorf("%q: %v", heading, err)

			continue
		}

		if !strings.Contains(example, "(encoding\n") {
			t.Errorf("the example under %q states no encoding profile:\n%s", heading, example)
		}
	}
}

// TestOneExamplePerSectionKeepsTheOthersWhereTheyAre is the property that makes
// a heading per example the right answer, asserted rather than assumed: an
// example added to the document ends the section before it, so what an existing
// heading extracts cannot move.
//
// The failure this rules out is silent. A second layout placed inside the first
// one's section would be found by no heading of its own, and one placed above it
// would be handed to every package that asked for the first.
func TestOneExamplePerSectionKeepsTheOthersWhereTheyAre(t *testing.T) {
	t.Parallel()

	native, err := Section(NativeExample)
	if err != nil {
		t.Fatalf("the native example's section: %v", err)
	}

	if strings.Contains(native, ConvertedExample) {
		t.Error("the native example's section runs on into the converted one, so the two are not separated by their headings")
	}

	blocks, err := Blocks(NativeExample)
	if err != nil {
		t.Fatalf("the native example's blocks: %v", err)
	}

	if len(blocks) != 1 {
		t.Errorf("the native example's section carries %d fenced blocks, want the one layout it is about", len(blocks))
	}

	converted, err := Example(ConvertedExample)
	if err != nil {
		t.Fatalf("the converted example: %v", err)
	}

	if strings.Contains(native, converted) || strings.Contains(converted, blocks[0]) {
		t.Error("the two examples extract to overlapping text, so one heading is reaching the other's layout")
	}
}

// TestAHeadingTheDocumentDoesNotCarryIsAnError keeps a missing section an error
// rather than an empty string. An empty layout parses, validates and reads as
// nothing at all, so a reading that returned one would turn every check resting
// on it into a check of nothing.
func TestAHeadingTheDocumentDoesNotCarryIsAnError(t *testing.T) {
	t.Parallel()

	if _, err := Section("## Appendix: A heading nobody wrote"); err == nil {
		t.Error("Section accepted a heading the document does not carry")
	}

	if _, err := Example("not a heading at all"); err == nil {
		t.Error("Example accepted a string that is not a Markdown heading")
	}
}

// TestASentenceStartingWithAnIssueNumberIsNotAHeading is a regression, and the
// reason a section is bounded by [headingLevel] rather than by a leading number
// sign.
//
// docs/layout/SPEC.md wraps its prose at the same width everywhere, and one
// wrapped line under "A discriminator for a redefine inside a table" begins
// "#35); what is written here is the choice." A rule counting only the number
// signs reads that as a level-one heading and hands back a section ending
// several paragraphs before the layout it was asked for — which is a fenced
// block that silently is not there rather than an error.
func TestASentenceStartingWithAnIssueNumberIsNotAHeading(t *testing.T) {
	t.Parallel()

	const heading = "### A discriminator for a redefine inside a table"

	body, err := Section(heading)
	if err != nil {
		t.Fatalf("%q: %v", heading, err)
	}

	if !strings.Contains(body, "#35); what is written here is the choice.") {
		t.Fatalf("the wrapped line this test is about is no longer in %q; the regression it guards has moved", heading)
	}

	blocks, err := Blocks(heading)
	if err != nil {
		t.Fatalf("%q: %v", heading, err)
	}

	if len(blocks) != 2 {
		t.Errorf("%q carries %d fenced blocks, want the skeleton and the layout after it", heading, len(blocks))
	}

	for _, line := range []string{"#", "#not a heading", "###nospace"} {
		if got := headingLevel(line); got != 0 {
			t.Errorf("headingLevel(%q) is %d, want 0 — it is not an ATX heading", line, got)
		}
	}

	if got := headingLevel("## Appendix: A layout, end to end"); got != 2 {
		t.Errorf("headingLevel of a level-two heading is %d, want 2", got)
	}
}
