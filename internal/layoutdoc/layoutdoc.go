// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutdoc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The headings of the worked examples in docs/layout/SPEC.md, each matched as a
// whole line.
//
// One constant per example, because the heading is what locates it: a package
// naming the example it wants names one of these and never a position among the
// document's fenced blocks.
const (
	// NativeExample is the layout for a file as the mainframe holds it —
	// cp037 characters, EBCDIC signs, and a single field that arrived from a
	// partner in ASCII and was never converted.
	NativeExample = "## Appendix: A layout, end to end"

	// ConvertedExample is the layout for a file of the same shop's that an
	// EBCDIC-to-ASCII conversion delivered: ASCII characters,
	// translated-EBCDIC signs, big-endian binary, and the items the conversion
	// did not reach. It is the combination "All four, always, with no default
	// for any" calls the one real files hit most often.
	ConvertedExample = "## Appendix: A converted file, end to end"
)

// fence opens and closes a Markdown code block in this document.
const fence = "```"

// specPath is the document, relative to the repository root.
var specPath = filepath.Join("docs", "layout", "SPEC.md")

// Example returns the layout under heading: the first fenced block in that
// section.
//
// A worked example's section carries exactly one fenced block and that block is
// the layout — which is a rule about how the document is written, and is
// asserted rather than assumed by this package's own tests. First rather than
// only because [Blocks] serves the sections that illustrate a form and then
// show it used, and one function that means "the layout here" is easier to read
// at a call site than an index.
func Example(heading string) (string, error) {
	blocks, err := Blocks(heading)
	if err != nil {
		return "", err
	}

	// Blocks already refuses an empty result, and this says so at the point a
	// reader would otherwise have to go and confirm it — a later relaxation of
	// that guard is then an error here rather than a panic.
	if len(blocks) == 0 {
		return "", fmt.Errorf("layoutdoc: %q carries no fenced code block", heading)
	}

	return blocks[0], nil
}

// Blocks returns every fenced code block under heading, in the order the
// document writes them.
//
// A section with no block, a block that is empty and a fence that is never
// closed are each an error rather than a result. All three are the same hazard:
// an empty layout parses, validates and reads as nothing at all, so a check
// resting on one passes over nothing and says so nowhere.
func Blocks(heading string) ([]string, error) {
	body, err := Section(heading)
	if err != nil {
		return nil, err
	}

	return blocksIn(body, heading)
}

// blocksIn is [Blocks] over a body already read, which is what makes its
// failures reachable from a test.
func blocksIn(body, where string) ([]string, error) {
	var (
		blocks []string
		block  []string
		open   bool
	)

	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), fence) {
			if open {
				if strings.TrimSpace(strings.Join(block, "\n")) == "" {
					return nil, fmt.Errorf("layoutdoc: a fenced code block under %q is empty", where)
				}

				blocks = append(blocks, strings.Join(block, "\n"))
				block = nil
			}

			open = !open

			continue
		}

		if open {
			block = append(block, line)
		}
	}

	if open {
		return nil, fmt.Errorf("layoutdoc: a fenced code block under %q is not closed", where)
	}

	if len(blocks) == 0 {
		return nil, fmt.Errorf("layoutdoc: %q carries no fenced code block", where)
	}

	return blocks, nil
}

// Section returns the text of the document under heading, up to the next
// heading at the same level or above.
//
// Stopping at the same level is what makes one heading per example safe: an
// example added after another one ends the section before it, so what an
// existing heading extracts does not move.
func Section(heading string) (string, error) {
	text, err := specText()
	if err != nil {
		return "", err
	}

	return sectionIn(text, heading)
}

// sectionIn is [Section] over a document already read, which is what makes its
// failures reachable from a test.
func sectionIn(text, heading string) (string, error) {
	level := headingLevel(heading)
	if level == 0 {
		return "", fmt.Errorf("layoutdoc: %q is not a Markdown heading", heading)
	}

	var (
		body  []string
		found bool
		open  bool
	)

	for line := range strings.SplitSeq(text, "\n") {
		// Fenced text is not the document's structure. A section bounded
		// without tracking this ends on the first `# ` comment in a shell or
		// YAML snippet, which is the same silently-truncated section
		// [headingLevel] exists to prevent, reached by another road.
		if strings.HasPrefix(strings.TrimSpace(line), fence) {
			open = !open
		}

		if !open && line == heading {
			if found {
				return "", fmt.Errorf("layoutdoc: %s carries the heading %q more than once", specPath, heading)
			}

			found = true

			continue
		}

		if !found {
			continue
		}

		if !open {
			if next := headingLevel(line); next > 0 && next <= level {
				break
			}
		}

		body = append(body, line)
	}

	if !found {
		return "", fmt.Errorf("layoutdoc: %s carries no heading %q", specPath, heading)
	}

	return strings.Join(body, "\n"), nil
}

// headingLevel is the number of leading number signs on an ATX heading, and 0
// for a line that is not one.
//
// The space matters and is not pedantry. This document wraps its prose, and a
// sentence closing a parenthesis on an issue reference begins a line with
// "#35); what is written here is the choice." — which a rule reading only the
// leading number signs takes for a level-one heading and ends the section on,
// several paragraphs before the layout it was asked for. Markdown requires the
// space, and requiring it here is what tells a heading from a sentence that
// happens to start with an issue number.
func headingLevel(line string) int {
	level := strings.IndexFunc(line, func(r rune) bool { return r != '#' })
	if level <= 0 {
		return 0
	}

	if rest := line[level:]; rest != "" && !strings.HasPrefix(rest, " ") {
		return 0
	}

	return level
}

// specText returns the whole of docs/layout/SPEC.md.
//
// Unexported, because a caller handed the document reads it however it likes,
// and three packages reading it however they liked is what this package was
// made to end.
func specText() (string, error) {
	root, err := repoRoot()
	if err != nil {
		return "", err
	}

	b, err := os.ReadFile(filepath.Join(root, specPath))
	if err != nil {
		return "", fmt.Errorf("layoutdoc: read %s: %w", specPath, err)
	}

	return string(b), nil
}

// repoRoot walks up from the working directory to the directory holding go.mod,
// which for this module is the repository root and so the directory docs/ sits
// in. A test's working directory is its own package directory, which is what
// makes this the right answer from any of them.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("layoutdoc: working directory: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("layoutdoc: no go.mod above the working directory")
		}

		dir = parent
	}
}
