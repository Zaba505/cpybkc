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
	mermaidHeading   = "# The sequencing automaton"
	mermaidRegisters = "## Registers"
	mermaidRecords   = "## Records"

	mermaidFenceOpen  = "```mermaid"
	mermaidFenceClose = "```"

	mermaidDiagram = "stateDiagram-v2"
)

// mermaidRegistersSaid is what the register section says before its table.
//
// The sentence is the emitter's and not the model's, unlike the two beside the
// diagram: it explains a table, and a notation with nowhere to put a table has
// nothing to explain. What it has to say is why the first column is an
// identifier — an `r20` in an edge label is not a name anybody wrote, and
// without this table it is not a thing a reader can follow either.
const mermaidRegistersSaid = "A register is what the automaton remembers between records: a binding on a transition writes one," +
	" a guard reads one, and nothing else does either. Registers carry identifiers and no names, so each is `r` and its own" +
	" node identifier — the name the edge labels above give it."

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

	// All three sentences are the model's, not this emitter's: they are prose
	// about the descriptor, they read the same in either notation, and each is
	// the empty string when there is nothing to say.
	for _, said := range []string{g.admitsNothing(), g.stranded(), g.unbound()} {
		if said != "" {
			b.WriteString("\n" + said + "\n")
		}
	}

	b.WriteString("\n" + mermaidFenceOpen + "\n")
	b.WriteString(mermaidDiagram + "\n")
	b.WriteString(mermaidBody(g))
	b.WriteString(mermaidFenceClose + "\n")

	b.WriteString(mermaidRegisterTable(g))
	b.WriteString(mermaidRecordTables(g))

	return b.String()
}

// mermaidRecordsSaid is what the record section says before its tables.
//
// It is the emitter's rather than the model's, like [mermaidRegistersSaid] and
// unlike the three sentences above the diagram: it explains a table, and a
// notation with nowhere to put a table has nothing to explain.
//
// What it has to say is which of these columns are the descriptor's and which
// are this generator's, because two of them are derived and neither says so on
// its face. A reader comparing this against a copybook has to know which side
// is authoritative before a disagreement means anything — and if they guess
// wrong they will go and change the copybook.
//
// The rules it names are cited the way every diagnostic of this program cites
// one — the document's title and the section's, as text. A Markdown link would
// have to be relative to this repository or absolute to a forge, and this file
// is written into somebody else's tree under `--out`: the first is broken
// wherever it lands, and the second is a URL baked into generated output.
const mermaidRecordsSaid = "Each record's items, in containment order, beginning at the first byte of the record's data." +
	"\n\n**The offsets are summed here, not read.** No IR node carries a byte offset:" +
	" position is stated once, as ordering and width — see docs/ir/SPEC.md, \"Ordering and width, and no offset\" —" +
	" so that a producer cannot state it a second time and be wrong in a way no consumer can detect." +
	" Every offset below is therefore this generator's own arithmetic over the widths ahead of the item," +
	" counting one occurrence — the first — of every group that encloses it and repeats." +
	" Where something ahead of an item repeats a number of times that is read at run time," +
	" there is no number to print and the offset carries a variable term instead, naming the count." +
	"\n\n**The pictures are spelled here, not quoted.** The IR carries no PICTURE character-string anywhere." +
	" It carries a category, the number of stored digit positions, the scale, whether the item is signed and where its sign sits," +
	" and the picture column is this generator's spelling of those five facts —" +
	" so `S9(5)V9(2)` may not be the text the copybook wrote for an item it describes exactly." +
	" An edited item's editing characters are not carried at all, so its category is named and nothing of it is spelled." +
	" The length of an alphabetic or alphanumeric picture is the item's width in bytes," +
	" which is its character count for every charset the IR admits."

// mermaidNoItems is what stands where a record's table would be when its top
// level holds nothing.
//
// A sentence rather than a table with no rows, for the reason
// [graph.admitsNothing] is a sentence: a heading over an empty table looks like
// a generator that gave up halfway, and this is a record described as holding
// no bytes — which is a thing to look at rather than nothing to draw.
const mermaidNoItems = "This record's top level holds no item, so it describes no bytes at all."

// The table's columns.
const (
	mermaidRecordHeader = "| Offset | Width | Item | Usage | Picture | Present |"
	mermaidRecordRule   = "| --- | --- | --- | --- | --- | --- |"
)

// mermaidRecordTables is the record section, and the empty string where there
// is none: `records=none`, or an automaton that admits no record at all.
func mermaidRecordTables(g *graph) string {
	if len(g.records) == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString("\n" + mermaidRecords + "\n\n")
	b.WriteString(mermaidRecordsSaid + "\n")

	for _, r := range g.records {
		// A heading is an inline position and not a cell, so the name is escaped
		// as one: a `|` is an ordinary character here and escaping it would put
		// a backslash in front of the reader.
		b.WriteString("\n### " + markdownInline(r.name) + "\n\n")

		if len(r.items) == 0 {
			b.WriteString(mermaidNoItems + "\n")

			continue
		}

		b.WriteString(mermaidRecordHeader + "\n")
		b.WriteString(mermaidRecordRule + "\n")

		for _, one := range r.items {
			// The three composed cells are the model's and are escaped by this
			// emitter, which is what each is passed [markdownCell] for. The
			// usage and the picture are this generator's own vocabulary — a
			// closed set of USAGE names, and a picture built out of `9`, `A`,
			// `X`, `P`, `V`, `S` and parentheses — so neither carries a
			// character a cell reacts to and neither is escaped.
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
				one.at.phrase(markdownCell), one.extent.phrase(markdownCell), mermaidItem(one),
				one.usage, one.picture, one.present.phrase(markdownCell))
		}
	}

	return b.String()
}

// mermaidItem is a row's Item cell: the item's path within the record, dotted.
//
// The path rather than the name alone, because containment is what the reader
// is checking and a column of bare names says nothing about which group an item
// is in. It is the same convention an edge label's predicate takes, for the same
// reason and with the same omission — the record's own top level is the heading
// above the table, and repeating it on every row would say it once per item.
//
// An unnamed node's word is written outside the escaping rather than inside it,
// and emphasised. `*slack*` is this generator saying that the descriptor named
// nothing here, and a copybook item actually called `slack` renders upright
// beside it — the distinction is the emphasis, and it is a real one rather than
// a strong one. What makes it safe is the other direction: a copybook name
// carrying its own asterisks reaches the cell through [markdownCell], which
// escapes them, so no name can dress itself up as one of these words.
func mermaidItem(one item) string {
	printed := make([]string, 0, len(one.path))

	for _, step := range one.path {
		if step.supplied {
			printed = append(printed, "*"+step.name+"*")

			continue
		}

		printed = append(printed, markdownCell(step.name))
	}

	return strings.Join(printed, ".")
}

// mermaidRegisterTable is the register section, and the empty string for a
// descriptor carrying no register.
//
// Omitted rather than written empty. A layout whose sequencing needs no memory
// carries no register node at all, and a heading over a table with no rows
// would be a section about nothing on the document every such layout produces —
// which is most of them.
func mermaidRegisterTable(g *graph) string {
	if len(g.registers) == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString("\n" + mermaidRegisters + "\n\n")
	b.WriteString(mermaidRegistersSaid + "\n\n")
	b.WriteString("| Register | Holds | Bound by |\n")
	b.WriteString("| --- | --- | --- |\n")

	for _, r := range g.registers {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", registerName(r.id), r.holds, mermaidBinders(r.boundBy))
	}

	return b.String()
}

// mermaidBinders is a register's third column: every transition that writes it,
// as the edge the diagram above draws.
//
// The edge and the record rather than the transition node's identifier, because
// the reader's next move is to look at the picture: `s2 --> s3` is a line they
// can find there and `t10` is not drawn anywhere.
func mermaidBinders(bound []binder) string {
	if len(bound) == 0 {
		// A word rather than an empty cell, which reads as a table this
		// generator failed to fill in. [graph.unbound] is where it is explained.
		return "nothing"
	}

	printed := make([]string, 0, len(bound))
	for _, one := range bound {
		printed = append(printed,
			fmt.Sprintf("`%s --> %s` (%s)", stateName(one.from), stateName(one.to), markdownCell(one.record)))
	}

	return strings.Join(printed, ", ")
}

// markdownCell is a name as a table cell may carry it.
//
// A record name is the copybook's and may hold anything, and a cell is two
// contexts at once. As table syntax, `|` ends the cell and a newline ends the
// row. As ordinary Markdown *inline* text — which is what a cell's contents are
// — a backtick opens a code span that swallows the rest of the row, `*` and `_`
// become emphasis, `[` and `]` an unresolved link, and `<` and `&` raw HTML.
// The record name sits outside the backticks that quote the edge beside it, so
// it is in that inline context with nothing around it to protect it.
//
// Each is backslash-escaped rather than dropped or replaced, because CommonMark
// admits a backslash escape in front of any ASCII punctuation and a reader has
// to be able to see the name the copybook spells. Only the characters that
// actually open something are escaped: a COBOL name is mostly hyphens, and
// `HEADER\-RECORD` in a diff would be this generator protecting itself from a
// character that does nothing.
//
// The same posture as [mermaidLabel] takes towards the diagram, arrived at
// separately because it is a different notation. Nothing here is shared with
// it: an escape one accepts is a literal backslash to the other.
//
// # Why there are two of these
//
// Everything above is true of any *inline* position — a heading, a paragraph, a
// cell — and is [markdownInline]. The last pair is true of a table cell alone:
// `|` ends a cell and a newline ends a row, and neither means anything in a
// heading. Escaping a `|` there would render a literal `\|` in front of the
// reader, which is this generator corrupting a name to protect itself from a
// character that does nothing where it stands.
var (
	markdownInlineEscapes = strings.NewReplacer(
		`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`,
		"<", `\<`, ">", `\>`, "&", `\&`, "~", `\~`,
		"\n", " ", "\r", " ",
	)

	markdownCellEscapes = strings.NewReplacer(
		`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`,
		"<", `\<`, ">", `\>`, "&", `\&`, "~", `\~`, "|", `\|`,
		"\n", " ", "\r", " ",
	)
)

// markdownCell is a name in a table cell, and markdownInline one anywhere else
// inline — a heading, or prose.
//
// A newline is flattened to a space in both. It ends a row in a cell, and in a
// heading it ends the heading and leaves the rest of the name standing as
// ordinary text, which is the same class of failure by a different route.
func markdownCell(name string) string { return markdownCellEscapes.Replace(name) }

func markdownInline(name string) string { return markdownInlineEscapes.Replace(name) }

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
			// The label is composed by the model and escaped by this emitter,
			// which is what [mermaidLabel] is passed in for: the wording is the
			// same in either notation and the escaping is not.
			line(&b, "%s --> %s: %s", stateName(s.id), stateName(e.to), e.label(mermaidLabel))
		}

		// Acceptance as an edge to the end pseudostate, for the same reason:
		// `s --> [*]` is how a state diagram says that a read may finish here,
		// and docs/ir/SPEC.md's accepting state is exactly that. Its guards
		// hang off that edge, labelling it the way a guarded transition is
		// labelled — because that is what conditional acceptance is.
		if s.accepts {
			if said := s.accepted(); said != "" {
				line(&b, "%s --> [*]: %s", stateName(s.id), said)
			} else {
				line(&b, "%s --> [*]", stateName(s.id))
			}
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
