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

	// The mirror of both checks above, on the second example. Without them only
	// the native example's layout is pinned to position 0 of its section, and
	// the converted one carries two subsections after its layout — which is
	// exactly where an illustrative snippet would land, and one landing above
	// the layout would be handed silently to every package that reads it.
	converted, err := Section(ConvertedExample)
	if err != nil {
		t.Fatalf("the converted example's section: %v", err)
	}

	if strings.Contains(converted, NativeExample) {
		t.Error("the converted example's section reaches back over the native one's heading")
	}

	if strings.Contains(converted, blocks[0]) {
		t.Error("the converted example's section carries the native example's layout, so one heading is reaching the other's")
	}

	convertedBlocks, err := Blocks(ConvertedExample)
	if err != nil {
		t.Fatalf("the converted example's blocks: %v", err)
	}

	if len(convertedBlocks) != 1 {
		t.Errorf("the converted example's section carries %d fenced blocks, want the one layout it is about", len(convertedBlocks))
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

// TestSectionIsBoundedByHeadingsAndNotByFencedText covers the failures the
// document does not currently produce, which is the only place they can be
// covered: [Section] and [Blocks] read one file that is checked into this
// repository, so every one of these would otherwise be an error path with no
// test and a document edit away from being live.
func TestSectionIsBoundedByHeadingsAndNotByFencedText(t *testing.T) {
	t.Parallel()

	const doc = "# Title\n" +
		"\n" +
		"## Wanted\n" +
		"\n" +
		"```\n" +
		"# not a heading, it is a comment in a snippet\n" +
		"(encoding (charset ascii))\n" +
		"```\n" +
		"\n" +
		"### Still inside\n" +
		"\n" +
		"tail\n" +
		"\n" +
		"## Next\n" +
		"\n" +
		"not wanted\n"

	body, err := sectionIn(doc, "## Wanted")
	if err != nil {
		t.Fatalf("sectionIn: %v", err)
	}

	if strings.Contains(body, "not wanted") {
		t.Error("the section ran on past the next heading of its own level")
	}

	if !strings.Contains(body, "### Still inside") || !strings.Contains(body, "tail") {
		t.Errorf("the section stopped before a deeper heading:\n%s", body)
	}

	blocks, err := blocksIn(body, "## Wanted")
	if err != nil {
		t.Fatalf("blocksIn: %v", err)
	}

	if len(blocks) != 1 {
		t.Fatalf("read %d fenced blocks, want 1: %q", len(blocks), blocks)
	}

	// The whole point of the fence tracking: the comment line is part of the
	// layout rather than the heading that ended the section before it.
	if !strings.Contains(blocks[0], "# not a heading") || !strings.Contains(blocks[0], "(encoding (charset ascii))") {
		t.Errorf("the fenced block lost its comment line or its layout:\n%s", blocks[0])
	}
}

// TestADocumentThatCannotBeReadUnambiguouslyIsAnError keeps every one of these
// an error rather than a plausible-looking answer. Each of them yields an empty
// or a merged layout, and an empty layout parses, validates and reads as
// nothing at all — so the check resting on it passes over nothing.
func TestADocumentThatCannotBeReadUnambiguouslyIsAnError(t *testing.T) {
	t.Parallel()

	sections := map[string]struct {
		doc     string
		heading string
	}{
		"a heading the document does not carry": {
			doc:     "## Elsewhere\n\nbody\n",
			heading: "## Wanted",
		},
		"a string that is not a heading": {
			doc:     "## Wanted\n\nbody\n",
			heading: "Wanted",
		},
		"number signs with no space after them": {
			doc:     "## Wanted\n\nbody\n",
			heading: "##Wanted",
		},
		"the same heading twice": {
			doc:     "## Wanted\n\nfirst\n\n### Other\n\n## Wanted\n\nsecond\n",
			heading: "## Wanted",
		},
	}

	for name, tc := range sections {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if body, err := sectionIn(tc.doc, tc.heading); err == nil {
				t.Errorf("sectionIn accepted %s and returned %q", name, body)
			}
		})
	}

	blocks := map[string]string{
		"no fenced block at all":            "prose and nothing else\n",
		"a fence that never closes":         "```\n(encoding)\n",
		"a fenced block with nothing in it": "```\n```\n",
		"a fenced block of whitespace":      "```\n   \n```\n",
	}

	for name, body := range blocks {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got, err := blocksIn(body, "## Wanted"); err == nil {
				t.Errorf("blocksIn accepted %s and returned %q", name, got)
			}
		})
	}
}
