// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// refusalSection is where the whole of this decision is written down, named by
// every diagnostic that acts on it.
const refusalSection = `cmd/cpybkc-gen-go/README.md, "When a descriptor admits no checkable file"`

// skipped is one construct the generated tests do not cover, and why.
//
// The decision this carries is README.md's, and the short of it is that a
// descriptor this generator can emit a package for and cannot synthesize a
// checkable file for gets the package. A test is a spot-check of a package, and
// refusing to write the package over a spot-check would make the spot-check a
// *cost* of adopting rather than a benefit — the exact inversion the generated
// tests exist to avoid. So the four files are written, and what is missing is
// said out loud twice: on standard error at `warning`, and in the generated test
// file itself where the terminal's scrollback cannot lose it.
//
// The one exception is a charset cobol-go's codec ships no table for, which
// fails the generation as it always has: the *generated code* could not read
// that file either, so there is nothing to keep. See [advisory].
type skipped struct {
	// construct is the layout construct with no case, as both the diagnostic
	// and the generated file name it: a record type, a predicate, or the
	// automaton. Never "the generation" — a message saying only that the tests
	// are incomplete sends its reader to this generator when the answer is a
	// line of their copybook.
	construct string

	// why is the refusal the synthesizer met. Its message and its notes are
	// written under the warning, so a refusal says here exactly what it would
	// have said had it failed the generation.
	why error

	// kept is what the descriptor produced anyway, as the closing note names
	// it.
	kept string

	// recorded is whether the generated test file this skip belongs to was
	// written, and therefore names it too.
	//
	// It decides whether the skip may be dropped by the cap. A skip the file
	// names has a second home and can be truncated on standard error without
	// being lost; a skip belonging to a tier that ended up with no case at all
	// has only the terminal, and truncating that one loses the construct's name
	// for good. See [reportSkips].
	recorded bool
}

// recording marks each skip with whether the tier that produced it wrote a file
// naming it.
func recording(skips []skipped, wrote bool) []skipped {
	for at := range skips {
		skips[at].recorded = wrote
	}

	return skips
}

// summary is the skip as the one line a generated file's comment carries.
//
// One line, because the comment is a list and a list whose items run to
// paragraphs is not one. The notes under the warning are where the rest is, and
// the terminal is where a reader who wants them is already looking.
func (s skipped) summary() string {
	first, _ := messageLines(s.why, "this generator wrote no case and said nothing about why")

	return s.construct + severitySeparator + first
}

// skippedRecord is a record type the record tier could not make a case out of.
//
// Named by the copybook rather than by the Go type it becomes, with the Go type
// beside it. The copybook name is the half an adopter can act on — it is what
// their layout and their copybook both spell — and the Go type is what they are
// looking at in the generated directory, so a diagnostic carrying only one of
// them makes somebody translate.
func skippedRecord(cobol, typ string, why error) skipped {
	return skipped{
		construct: fmt.Sprintf("the record type %s (%s)", cobol, typ),
		why:       why,
		kept:      "its struct, its decoder and its encoder are generated as usual",
	}
}

// skippedGoal is something the file tier covers and could not reach: a
// transition predicate, or the automaton itself.
func skippedGoal(construct string, why error) skipped {
	return skipped{
		construct: construct,
		why:       why,
		kept:      "the file-level reader and writer are generated as usual",
	}
}

// uncoverableError is a goal no file this generator can lay out reaches.
//
// Its message deliberately does not name the construct: [warn] has already
// opened the line with it, and a sentence that named it twice is a sentence
// read twice. What is left is why, which is the half that differs between a
// predicate behind a guard nothing satisfies and an automaton with no accepting
// state at all.
type uncoverableError struct {
	// What is why no file reaches it, as the rest of the `warning:` line.
	What string

	// Rule is what a reader is to conclude, as the `note:` line — and, where
	// candidate paths were tried and refused, what each of them could not
	// satisfy.
	Rule string
}

// Error implements the error interface.
func (e *uncoverableError) Error() string { return e.What }

// Notes is what follows it as a `note:` diagnostic.
func (e *uncoverableError) Notes() []string { return []string{e.Rule} }

// advisory reports whether a refusal met while synthesizing the generated tests
// is one this generator writes the package around, rather than one it refuses
// the whole generation on.
//
// Everything is, but for the two below. The rule is deliberately that way round,
// and the reason is the order [generate] composes in: the four files that *are*
// the package are composed before either tier of tests, so a descriptor that
// reaches a synthesizer at all is one this generator has already emitted a
// reader and a writer for. A failure only the synthesizer meets is therefore a
// failure to synthesize a file to check them with — not a failure to describe
// the layout — and withholding the package over it would hand the adopter
// nothing in place of something.
//
// # Why this is not an allow-list
//
// The safer-looking shape is the other one: name the refusals that *are*
// advisory and fail on everything else, so that a fatal condition added to the
// synthesizer later cannot ship as a warning without somebody deciding it
// should. It cannot be written today, and the reason is worth knowing before
// anyone tries. `synth.go` raises the great majority of its refusals as
// [malformedError], and it raises both kinds that way — *this predicate's
// literal is not the width of the field it tests* is a producer bug, and *no one
// number of occurrences is inside every bound declared for this count* is a
// layout with no checkable file, and they are the same type. An allow-list
// therefore has to reclassify every one of those sites, and a site put on the
// wrong side of it in the direction the allow-list makes cheap — calling an
// unsynthesizable layout a producer bug — restores exactly the failure this
// story removed: an adopter losing their reader over a spot-check.
//
// So the classification is a *deny-list of shapes that are never about the
// bytes*, it is short, and it is pinned:
// [TestEveryRefusalThisPackageRaisesHasADecidedClassification] enumerates every
// error type this package defines, so one added without a line there fails.
//
// The three:
//
//   - [unsupportedCharsetError], because it is the one refusal that is *also*
//     about the generated code — a charset codec has no table for is a charset
//     the emitted reader cannot read the file in either. It is refused on sight
//     where the accessors are emitted (see [charsetCall]) and so never reaches a
//     synthesizer today; this check is what keeps that true if it ever does, and
//     is why the charset case has one diagnostic rather than two saying the same
//     thing differently.
//   - [unsupportedBinarySizeError], for exactly that reason a second time. A
//     binary width staircase codec has no member for is one the emitted reader
//     cannot lay a record out under, so there is no generated package to warn
//     about a spot-check in; see [binarySize].
//   - a [malformedError] carrying a Reference, which is [unresolved]: a
//     reference to a node the message does not contain. There is no layout there
//     to be unable to synthesize — the descriptor does not describe one — so the
//     answer is the refusal it always was, rather than a warning about a
//     construct nobody can go and look at.
func advisory(err error) bool {
	var charset *unsupportedCharsetError
	if errors.As(err, &charset) {
		return false
	}

	var binary *unsupportedBinarySizeError
	if errors.As(err, &binary) {
		return false
	}

	var malformed *malformedError
	if errors.As(err, &malformed) && malformed.Reference != 0 {
		return false
	}

	return true
}

// reportedSkips is how many constructs a generation may drop from standard
// error before it stops naming them and says how many are left.
//
// Capped because the diagnostics are read in a terminal beside every other
// generator cpybkc ran, and a copybook whose every record type carries an item
// this generator cannot synthesize would otherwise bury all of them. Ten is
// enough that the ordinary case — one record type, or two — is never truncated,
// and small enough that the pathological one stays readable.
const reportedSkips = 10

// reportSkips writes what the generated tests do not cover.
//
// In the order the tiers were composed in, which is the record tier and then the
// file tier, so that two runs over one descriptor write the same lines in the
// same order — docs/plugin/SPEC.md's determinism reaches the diagnostics as well
// as the output.
//
// The cap applies only to a skip the generated file *also* names. That is what
// makes truncating one safe: the terminal is scrollback and the file is checked
// in, so a construct named in both loses nothing by being dropped from the
// first. A tier that skipped every construct it had writes no file, and those
// skips have nowhere else to be named — so they are written whatever the count,
// and the cap falls on the ones that are recoverable. The alternative was a
// count where a name should have been, for the only constructs a reader could
// not go and look up.
func reportSkips(w io.Writer, skips []skipped) {
	var written, dropped int

	for _, one := range skips {
		if one.recorded && written >= reportedSkips {
			dropped++

			continue
		}

		warn(w, one)

		written++
	}

	if dropped == 0 {
		return
	}

	diagnostic(w, severityWarning,
		fmt.Sprintf("%s of this layout have no generated test either", plural(dropped, "further construct")))
	diagnostic(w, severityNote,
		"each of them is named in the generated test file it belongs to, which is why this list rather than that one is the one that stops; see "+refusalSection)
}

// uncovered is the paragraph a generated test file carries naming what it does
// not cover.
//
// In the file as well as on standard error, because the two outlive each other
// by different amounts. The terminal a generation ran in is scrollback, and this
// directory is checked in — so a copybook that grows an item this generator
// cannot synthesize turns into a line a reviewer sees in a diff, which is where
// a fact about the descriptor belongs. It is the same argument that puts the
// per-item comment column beside a case's bytes.
func uncovered(skips []skipped) string {
	var b strings.Builder

	b.WriteString(wrapped("Not everything the layout describes has a case in this file. What is missing, " +
		"and what this generator could not lay out — every one of these is read and " +
		"written by the package beside this file all the same:"))

	for _, one := range skips {
		b.WriteString("\n\n")
		b.WriteString(bulleted(one.summary()))
	}

	return b.String()
}

// bulleted is one line of [uncovered] as a doc comment's list item: a hanging
// indent under `  - `, wrapped where the rest of the comment wraps.
//
// The indent is for more than the eye. gofmt reads a generated doc comment and
// reflows it, and `  - ` opens a list item whose continuation lines have to be
// indented to stay part of it: a second line at column zero would be reflowed
// into a paragraph of its own, and one construct would read as two.
func bulleted(text string) string {
	const (
		marker = "  - "
		indent = "    "
	)

	var (
		b    strings.Builder
		line int
	)

	b.WriteString(marker)

	for at, word := range strings.Fields(text) {
		switch {
		case at == 0:
			line = len(marker) + len(word)
		case line+1+len(word) > commentWidth:
			b.WriteString("\n")
			b.WriteString(indent)

			line = len(indent) + len(word)
		default:
			b.WriteString(" ")

			line += 1 + len(word)
		}

		b.WriteString(word)
	}

	return b.String()
}
