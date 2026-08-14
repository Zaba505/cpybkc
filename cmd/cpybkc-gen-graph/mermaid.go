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

	if !g.admits() {
		// Said in the document rather than left to be noticed. An automaton
		// with no transition in it draws as a start state and nothing else,
		// which looks like a generator that failed halfway; the sentence is the
		// difference between "there is nothing here" and "there is nothing
		// there".
		b.WriteString("\nThis automaton admits no record: no state offers a transition, so nothing a reader of it does consumes bytes.\n")
	}

	if stranded := g.unreachable(); len(stranded) != 0 {
		b.WriteString("\n" + strandedSentence(stranded) + "\n")
	}

	b.WriteString("\n" + mermaidFenceOpen + "\n")
	b.WriteString(mermaidDiagram + "\n")
	b.WriteString(mermaidBody(g))
	b.WriteString(mermaidFenceClose + "\n")

	return b.String()
}

// strandedSentence says that some state nothing reaches is drawn anyway, and
// why that is worth a reader's attention.
func strandedSentence(stranded []state) string {
	named := make([]string, 0, len(stranded))
	for _, s := range stranded {
		named = append(named, mermaidState(s.id))
	}

	subject := fmt.Sprintf("States %s are", strings.Join(named, ", "))
	if len(named) == 1 {
		subject = fmt.Sprintf("State %s is", named[0])
	}

	return subject + " not reachable from the start state. Each is drawn anyway, marked *unreachable*:" +
		" a state no path arrives at is a bug in whatever compiled this automaton, and one nobody sees if the diagram leaves it out."
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
		line(&b, `state "%s (unreachable)" as %s`, mermaidState(s.id), mermaidState(s.id))
	}

	// The start state, as an edge from Mermaid's start pseudostate. That is the
	// notation's own way of saying where a read begins, so a reader who knows
	// state diagrams and not this project already knows what it means.
	line(&b, "[*] --> %s", mermaidState(g.start))

	for _, s := range g.states {
		for _, e := range s.edges {
			line(&b, "%s --> %s: %s", mermaidState(s.id), mermaidState(e.to), mermaidLabel(e.record))
		}

		// Acceptance as an edge to the end pseudostate, for the same reason:
		// `s --> [*]` is how a state diagram says that a read may finish here,
		// and docs/ir/SPEC.md's accepting state is exactly that. The guards
		// that may qualify it are #188's, and they hang off this edge.
		if s.accepts {
			line(&b, "%s --> [*]", mermaidState(s.id))
		}
	}

	return b.String()
}

// line writes one indented line of the diagram.
func line(b *strings.Builder, format string, args ...any) {
	fmt.Fprintf(b, mermaidIndent+format+"\n", args...)
}

// mermaidState is what the diagram calls a state: `s` and the state node's own
// identifier.
//
// The identifier rather than a name, because states carry identifiers and no
// names — and because it is what takes a reader who wants more than the diagram
// says to the right node of `cpybkc --emit-ir`. It is also, incidentally, the
// one part of this document that can never need escaping: a decimal number
// carries no metacharacter of any notation.
func mermaidState(id uint64) string { return fmt.Sprintf("s%d", id) }

// mermaidLabel is a record name as a transition label may carry it.
//
// # The rule
//
// A letter, a digit, a space, `-`, `_` or `.` is written as it stands, and
// every other rune becomes `#<code point>;` — Mermaid's own numeric escape.
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

	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.', r == ' ':
			b.WriteRune(r)
		default:
			fmt.Fprintf(&b, "#%d;", r)
		}
	}

	return b.String()
}
