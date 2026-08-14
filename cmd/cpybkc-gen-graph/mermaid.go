// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"strings"
)

// The Markdown around the diagram.
//
// A Markdown document holding a fenced `mermaid` block rather than a bare
// `.mmd`, because the notation's whole advantage is that a forge renders it
// where it stands: an adopter checking a layout in is looking at the diagram in
// a pull request, and a file they would have to run something over first is a
// file they do not look at.
const (
	mermaidHeading = "# The sequencing automaton"

	mermaidFenceOpen  = "```mermaid"
	mermaidFenceClose = "```"

	mermaidDiagram = "stateDiagram-v2"
)

// mermaidIndent is what a line inside the diagram is indented by. Mermaid does
// not require it; a person reading the document does.
const mermaidIndent = "    "

// mermaid is the whole Markdown document for a graph.
//
// It is a function over [graph] and reads no descriptor, which is what makes
// #190's `dot` emitter a second consumer of one walk rather than a second walk.
func mermaid(g *graph) string {
	var b strings.Builder

	b.WriteString(mermaidGeneratedBy + "\n\n")
	b.WriteString(mermaidHeading + "\n\n")

	// The framing before the diagram, because it is the question the diagram
	// does not answer: the states and edges say which records come in which
	// order, and what stands between two of them in the bytes is part of what a
	// person is verifying and appears nowhere in a state machine.
	b.WriteString("**Framing:** " + g.framing.String() + ".\n")

	// Both sentences are the model's, not this emitter's: they are prose about
	// the descriptor, they read the same in either notation, and each is the
	// empty string when there is nothing to say.
	for _, said := range []string{g.admitsNothing(), g.stranded()} {
		if said != "" {
			b.WriteString("\n" + said + "\n")
		}
	}

	b.WriteString("\n" + mermaidFenceOpen + "\n")
	b.WriteString(mermaidDiagram + "\n")
	b.WriteString(mermaidBody(g))
	b.WriteString(mermaidFenceClose + "\n")

	return b.String()
}

// mermaidBody is the diagram's lines, after `stateDiagram-v2` and before the
// closing fence.
func mermaidBody(g *graph) string {
	var b strings.Builder

	// The declarations first, so that a state carrying a description has it
	// before anything refers to it. Mermaid does not require the order; a
	// person reading the source of the diagram does, and it keeps the
	// unreachable states from being buried among the edges.
	for _, s := range g.unreachable() {
		line(&b, `state "%s (unreachable)" as %s`, stateName(s.id), stateName(s.id))
	}

	// The start state, as an edge from Mermaid's start pseudostate. That is the
	// notation's own way of saying where a read begins, so a reader who knows
	// state diagrams and not this project already knows what it means.
	line(&b, "[*] --> %s", stateName(g.start))

	for _, s := range g.states {
		for _, e := range s.edges {
			line(&b, "%s --> %s: %s", stateName(s.id), stateName(e.to), mermaidLabel(e.record))
		}

		// Acceptance as an edge to the end pseudostate, for the same reason:
		// `s --> [*]` is how a state diagram says that a read may finish here,
		// and docs/ir/SPEC.md's accepting state is exactly that. The guards
		// that may qualify it are #188's, and they hang off this edge.
		if s.accepts {
			line(&b, "%s --> [*]", stateName(s.id))
		}
	}

	return b.String()
}

// line writes one indented line of the diagram.
func line(b *strings.Builder, format string, args ...any) {
	fmt.Fprintf(b, mermaidIndent+format+"\n", args...)
}

// mermaidLabel is a record name as a transition label may carry it.
//
// # The rule
//
// A letter, a digit, `-`, `_`, `.` or a space between two of those is written
// as it stands, and every other rune becomes `#<code point>;` — Mermaid's own
// numeric escape.
//
// A space at either end of the name is escaped rather than written, because
// there it is invisible in the rendered diagram and the only thing it can
// produce is a source line ending in whitespace — a spurious diff in a golden
// the first time a producer emits one. Escaping it keeps the name faithful: a
// renderer decoding `#32;` shows the space that was there.
//
// # Why that set
//
// `-` is in it because COBOL names are full of hyphens and a diagram spelling
// `ORDER#45;HEADER` is one nobody reads. It is safe to leave literal: what
// Mermaid's parser reacts to is the arrow `-->`, and `>` is not in the set, so
// no escaped name can grow one.
//
// # Why an allow-list, and what the escape actually buys
//
// The set is what may pass rather than what may not, because the failure being
// designed against is a metacharacter nobody thought of. A deny-list is a guess
// about a renderer's grammar that is wrong the first time the grammar grows a
// character; an allow-list is wrong only in being conservative, and the cost of
// that is a name rendered as an escape rather than a diagram that will not
// parse.
//
// That is the property this buys, and it is worth stating exactly, because a
// numeric escape is a claim about a renderer. Where Mermaid decodes `#58;` the
// label reads as the copybook spells it. Where it does not — an older renderer,
// or one that never implemented the escape — the label reads `#58;` literally,
// which is ugly and is still a diagram that draws. What no name can do is end
// the transition's line, open a comment, or turn itself into an arrow, because
// the runes that do those things are not in the set and never reach the output.
func mermaidLabel(name string) string {
	var b strings.Builder

	// Where the name stops being a run of spaces at each end. A space is a
	// space wherever it stands, so byte offsets are enough to tell an interior
	// one from an edge one.
	lead := len(name) - len(strings.TrimLeft(name, " "))
	trail := len(strings.TrimRight(name, " "))

	for at, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case r == ' ' && at >= lead && at < trail:
			b.WriteRune(r)
		default:
			fmt.Fprintf(&b, "#%d;", r)
		}
	}

	return b.String()
}
