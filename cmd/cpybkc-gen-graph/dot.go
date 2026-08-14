// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"strconv"
	"strings"
)

// The Graphviz document's own vocabulary.
//
// A `digraph` because every edge of this diagram is a direction: a transition
// moves the automaton from one state to another and never both ways, and an
// undirected drawing of it would say that a file may be read backwards.
//
// The graph is named for this project rather than for the layout, because the
// descriptor states no name for the layout and the two paths this generator can
// see are ones it may not read a name out of (see [document]). `dot` takes a
// name or nothing here, and an anonymous digraph is one a reader cannot refer to
// from a document that embeds it.
const (
	dotOpen  = "digraph cpybkc {"
	dotClose = "}"

	dotIndent = "\t"
)

// dotHeading is the document's title, which is [mermaidHeading] without the
// Markdown that makes it a heading there.
const dotHeading = "The sequencing automaton"

// The two cluster subgraphs, and the sections they hold.
//
// A `cluster_` prefix is not decoration: it is what makes Graphviz draw the
// subgraph as a box with a label of its own and lay its contents out together,
// and a subgraph named anything else is a grouping the layout engine ignores.
const (
	dotRecordsCluster   = "cluster_records"
	dotRegistersCluster = "cluster_registers"

	dotRecords   = "Records"
	dotRegisters = "Registers"
)

// dotStart is the node marking where a read begins.
//
// A node rather than an attribute on the start state, because "a read begins
// here" is not a property of a state — it is the file node's `start_state_id`,
// and the state it names is an ordinary state in every other respect. Drawing it
// as an edge from a `point` is the same thing Mermaid's `[*] --> s2` says, in
// the notation Graphviz has for it, and it cannot collide with a state: those
// are `s` and a decimal.
const dotStart = "start"

// The shapes, which are the whole of what this diagram says about a state
// without words.
//
// `doublecircle` for an accepting state is the state-diagram convention a reader
// who knows automata and nothing about this project already reads. It replaces
// Mermaid's `s4 --> [*]` edge rather than adding to it: Graphviz has no end
// pseudostate, and a second `point` node standing for "the read may finish"
// would draw one edge per accepting state across the width of the diagram —
// which is the density this notation was reached for in the first place.
const (
	dotState     = "circle"
	dotAccepting = "doublecircle"

	// dotTableShape is what a node carrying an HTML-like table is. `plaintext`
	// is the shape with no shape: Graphviz draws the label and nothing around
	// it, which is what makes the table's own borders the only ones there are.
	dotTableShape = "plaintext"
)

// dotUnreachable is what marks a state no path from the start state arrives at,
// on the state itself.
//
// On the node's own label, where Mermaid puts it too, because it is a fact about
// the state rather than about what may be done there. The acceptance guards
// beside it are the other way round; see [dotStateAttributes].
const dotUnreachable = "(unreachable)"

// dotAccepts opens the phrase qualifying an accepting state's acceptance.
//
// The word is this emitter's because the position is: Mermaid hangs the guards
// off a `--> [*]` edge, whose arrow says what is being qualified, and a
// `doublecircle` with a phrase beside it says nothing until the phrase does.
const dotAccepts = "accepts "

// dotWrap is the column the prose in a graph or cluster label is wrapped at.
//
// Prose in a Graphviz label is one line unless something breaks it, and one line
// of a paragraph is a diagram as wide as the paragraph — with the automaton
// drawn somewhere in the middle of it at whatever size is left. The number is a
// reading width rather than a terminal's: these labels are read in a rendered
// image, and 96 columns is about as long a line as the eye tracks.
const dotWrap = 96

// dot is the whole Graphviz document for a graph.
//
// It is a function over [graph] and reads no descriptor, which is what makes it
// a second consumer of one walk rather than a second walk. Everything it draws
// is a field [read] filled in, and every sentence about the descriptor is the
// model's — so the two notations cannot come to disagree about what the
// automaton says, only about how it is drawn.
//
// # Why the escaping is applied to the composed phrase
//
// [mermaid] escapes each name and writes the words between them literally,
// because Mermaid's escape is an allow-list that would turn this generator's own
// `=` into `#61;`. Graphviz's is not: inside a quoted string only `"` and `\`
// mean anything, so escaping a whole composed phrase escapes exactly the
// characters a name could have smuggled in and leaves every connecting word
// alone. That is the stronger rule of the two — it also reaches the one thing
// per-name escaping cannot, a predicate's quoted literal, which the model writes
// past the escaping because no Mermaid label may carry a `"` and a Graphviz
// label very much may. So the model composes with [asIs] and this emitter
// escapes what comes back.
func dot(g *graph) string {
	var b strings.Builder

	b.WriteString(dotGeneratedBy + "\n\n")
	b.WriteString(dotOpen + "\n")
	b.WriteString(dotPreamble(g))
	b.WriteString(dotAutomaton(g))
	b.WriteString(dotRegisterTable(g))
	b.WriteString(dotRecordTables(g))
	b.WriteString(dotClose + "\n")

	return b.String()
}

// asIs is the escaping a phrase takes on its way out of the model and into a
// Graphviz document: none, because the phrase is escaped whole once it is
// composed. See [dot].
func asIs(name string) string { return name }

// The layout attributes, which are the whole of what this generator tells
// Graphviz about how to draw the diagram.
//
// `rankdir="LR"` because a file is read from its first record to its last, and
// left to right is the direction that reads as. It is also what makes a state
// offering many alternatives fan out down the page rather than across it, which
// is the shape this notation was reached for.
//
// The two separations are wider than Graphviz's own, which are 0.25 and 0.5 of
// an inch and are sized for a node label of a word or two. An edge label here is
// three or four lines several words wide — the record, the predicate, the guards
// and the bindings — and at the defaults the labels of two transitions leaving
// one state are drawn on top of each other, which is the failure Mermaid's
// stacked self-loops already have and the reason this notation exists.
const dotLayout = `rankdir="LR", nodesep="0.6", ranksep="1.2"`

// dotPreamble is the graph's own attributes: how it is laid out, the title and
// the prose above the diagram, and the defaults every node takes.
func dotPreamble(g *graph) string {
	var b strings.Builder

	b.WriteString(dotIndent + `graph [` + dotLayout + `, labelloc="t", labeljust="l", label="` +
		dotLabel(dotProse(g)) + `"]` + "\n")
	b.WriteString(dotIndent + `node [shape="` + dotState + `"]` + "\n")

	return b.String()
}

// dotProse is the document's title and the sentences above the diagram, one
// line per line of the label.
//
// All four sentences are the model's, not this emitter's: they are prose about
// the descriptor, they read the same in either notation, and each but the
// framing is the empty string when there is nothing to say.
func dotProse(g *graph) []string {
	lines := []string{dotHeading, ""}
	lines = append(lines, wrapped("Framing: "+g.framing.String()+".", dotWrap)...)

	for _, said := range []string{g.admitsNothing(), g.stranded(), g.unbound()} {
		if said == "" {
			continue
		}

		lines = append(lines, "")
		lines = append(lines, wrapped(said, dotWrap)...)
	}

	return lines
}

// dotAutomaton is the start marker, every state, and every transition between
// them.
//
// Declarations first and edges after, rather than each state's edges beneath it.
// Graphviz needs neither order; a person reading the source of a diagram does,
// and a list of every state the descriptor carries is the one place the
// unreachable ones are not buried among the edges.
func dotAutomaton(g *graph) string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(dotIndent + dotStart + ` [shape="point"]` + "\n")
	b.WriteString(dotIndent + dotStart + " -> " + stateName(g.start) + "\n")

	b.WriteString("\n")

	for _, s := range g.states {
		b.WriteString(dotIndent + stateName(s.id) + " [" + strings.Join(dotStateAttributes(s), ", ") + "]\n")
	}

	if !g.admits() {
		return b.String()
	}

	b.WriteString("\n")

	for _, s := range g.states {
		for _, e := range s.edges {
			b.WriteString(dotIndent + stateName(s.id) + " -> " + stateName(e.to) +
				` [label="` + dotLabel(e.sections(asIs)) + `"]` + "\n")
		}
	}

	return b.String()
}

// dotStateAttributes is one state's shape and whatever the state itself has to
// say.
//
// The acceptance guards go on an `xlabel` — text Graphviz places beside a node
// rather than inside it — and the unreachable mark goes in the node's own label.
// The split is not tidiness: a `circle` grows to fit whatever is written in it,
// and `accepts if r20 = 0 and r21 is one of 0xD5 or 0x40` written inside one
// draws a state larger than the rest of the diagram put together. `(unreachable)`
// is short enough to sit in the node, and it belongs there for the reason
// [dotUnreachable] gives — it is what the state *is*, where the guards are a
// condition on what may be done there.
func dotStateAttributes(s state) []string {
	shape := dotState
	if s.accepts {
		shape = dotAccepting
	}

	attributes := []string{`shape="` + shape + `"`}

	if !s.reachable {
		attributes = append(attributes,
			`label="`+dotString(stateName(s.id))+`\n`+dotString(dotUnreachable)+`"`)
	}

	if said := s.accepted(); said != "" {
		attributes = append(attributes, `xlabel="`+dotLabel([]string{dotAccepts + said})+`"`)
	}

	return attributes
}

// dotRegistersSaid is what the register cluster says before its table.
//
// The emitter's rather than the model's, as [mermaidRegistersSaid] is and for
// the same reason: it explains a table, and what it has to say about the first
// column — that an `r20` in an edge label is not a name anybody wrote — is the
// same fact worded for a reader looking at a box in a picture rather than at a
// section of a Markdown document. Written here rather than shared with that one
// because the Markdown emphasis and code spans in it are punctuation a Graphviz
// label draws literally.
const dotRegistersSaid = "A register is what the automaton remembers between records: a binding on a transition writes one," +
	" a guard reads one, and nothing else does either. Registers carry identifiers and no names, so each is r and its own" +
	" node identifier — the name the edge labels give it."

// The register table's columns, which are [mermaidRegisterTable]'s.
var dotRegisterHeader = []string{"Register", "Holds", "Bound by"}

// dotRegisterTable is the register cluster, and the empty string for a
// descriptor carrying no register.
//
// In a cluster of its own for the reason the record tables are in one: a
// plaintext node standing loose among the states is a node the layout engine
// ranks with them, and a table the size of this one ranked into the automaton
// pushes the states it is about off to one side. It carries no edge either — the
// third column names the edges the diagram draws, in words, and drawing them
// again as edges into a table would say it twice and wreck the ranking to do it.
func dotRegisterTable(g *graph) string {
	if len(g.registers) == 0 {
		return ""
	}

	rows := make([][]string, 0, len(g.registers))
	for _, r := range g.registers {
		rows = append(rows, []string{
			dotHTML(registerName(r.id)), dotHTML(r.holds), dotBinders(r.boundBy),
		})
	}

	return dotCluster(dotRegistersCluster, dotRegisters, dotRegistersSaid,
		dotTableNode("registers", "", dotRegisterHeader, rows))
}

// dotBinders is a register's third column: every transition that writes it, as
// the edge the diagram draws.
//
// One binder per line, which is what a cell of an HTML-like table has and a cell
// of a Markdown one does not. A register bound from four transitions is four
// short lines here and one long one there, and the difference between those two
// is most of why this notation exists.
func dotBinders(bound []binder) string {
	if len(bound) == 0 {
		// A word rather than an empty cell, as [mermaidBinders] writes one.
		// [graph.unbound] is where it is explained.
		return dotHTML("nothing")
	}

	printed := make([]string, 0, len(bound))
	for _, one := range bound {
		printed = append(printed,
			dotHTML(stateName(one.from)+" -> "+stateName(one.to)+" ("+one.record+")"))
	}

	return strings.Join(printed, "<BR/>")
}

// dotRecordsSaid is what the record cluster says before its tables.
//
// The emitter's rather than the model's, as [mermaidRecordsSaid] is, and it says
// the two things that one says: which columns are derived rather than read, and
// which side a reader comparing this against their copybook should believe. It
// is shorter because it is read inside a box in a picture rather than as prose
// above a table, and it is written here rather than shared because the emphasis
// and code spans that make the other one readable as Markdown are punctuation a
// Graphviz label draws literally.
//
// The rules are cited the way every diagnostic of this program cites one — the
// document's title and the section's, as text — because this file is written
// into somebody else's tree under `--out`, where a relative link is broken and
// an absolute one is a forge's URL baked into generated output.
const dotRecordsSaid = "Each record's items, in containment order, beginning at the first byte of the record's data." +
	" The offsets are summed here, not read: no IR node carries a byte offset — position is stated once, as ordering" +
	" and width, see docs/ir/SPEC.md, \"Ordering and width, and no offset\" — so every offset below is this generator's" +
	" own arithmetic over the widths ahead of the item, counting one occurrence of every group that encloses it and" +
	" repeats, and carrying a named term where that count is read at run time." +
	" The pictures are spelled here, not quoted: the IR carries no PICTURE character-string anywhere, only a category," +
	" a count of stored digit positions, a scale, and whether the item is signed and where its sign sits — so a picture" +
	" that does not match the copybook character for character is not necessarily a disagreement."

// The item tables' columns, which are [mermaidRecordHeader]'s.
var dotRecordHeader = []string{"Offset", "Width", "Item", "Usage", "Picture", "Present"}

// dotNoItems is what stands where a record's table would be when its top level
// holds nothing, and it is [mermaidNoItems] said again for a cell.
//
// A sentence rather than a table with no rows, for the reason that one is a
// sentence: a heading over an empty table looks like a generator that gave up
// halfway, and this is a record described as holding no bytes — a thing to look
// at rather than nothing to draw.
const dotNoItems = "This record's top level holds no item, so it describes no bytes at all."

// dotRecordTables is the record cluster, and the empty string where there is
// none: `records=none`, or an automaton that admits no record at all.
//
// No edge runs into it, and that is a decision rather than an omission. An edge
// from the transition that admits a record to the table of that record's items
// would cross the whole drawing, rank a page-high table beside the states, and
// say what the edge's own label already says — the record's name, which is how a
// reader finds the table.
func dotRecordTables(g *graph) string {
	if len(g.records) == 0 {
		return ""
	}

	var tables strings.Builder

	for at, r := range g.records {
		if at != 0 {
			tables.WriteString("\n")
		}

		tables.WriteString(dotRecordNode(r))
	}

	return dotCluster(dotRecordsCluster, dotRecords, dotRecordsSaid, tables.String())
}

// dotRecordNode is one record's table, as the node carrying it.
//
// The node is named for the record's identifier and not for its name, because a
// name is not identity: two records may share one, and two nodes sharing a name
// in a Graphviz document are one node.
func dotRecordNode(r recordTable) string {
	name := "record" + strconv.FormatUint(r.id, 10)
	title := dotHTML(r.name)

	if len(r.items) == 0 {
		return dotTableNode(name, title, nil, [][]string{{dotHTML(dotNoItems)}})
	}

	rows := make([][]string, 0, len(r.items))

	for _, one := range r.items {
		// Every cell but the item's is the model's phrase, composed with [asIs]
		// and escaped whole. The item's carries markup of this emitter's own, so
		// it is composed and escaped element by element; see [dotItem].
		rows = append(rows, []string{
			dotHTML(one.at.phrase(asIs)), dotHTML(one.extent.phrase(asIs)), dotItem(one),
			dotHTML(one.usage), dotHTML(one.picture), dotHTML(one.present.phrase(asIs)),
		})
	}

	return dotTableNode(name, title, dotRecordHeader, rows)
}

// dotItem is a row's Item cell: the item's path within the record, dotted.
//
// The path rather than the name alone, for the reason [mermaidItem] takes it,
// and with the same distinction between a name and a word. An unnamed node's
// word is written in italics and outside the escaping — `slack` is this
// generator saying that the descriptor named nothing there — and a copybook item
// actually called `slack` reaches the cell through [dotHTML], which cannot
// produce a tag, so no name can dress itself up as one of these words.
func dotItem(one item) string {
	printed := make([]string, 0, len(one.path))

	for _, step := range one.path {
		if step.supplied {
			printed = append(printed, "<I>"+dotHTML(step.name)+"</I>")

			continue
		}

		printed = append(printed, dotHTML(step.name))
	}

	return strings.Join(printed, ".")
}

// dotCluster is one labelled box holding one or more table nodes.
func dotCluster(cluster, heading, said, nodes string) string {
	var b strings.Builder

	b.WriteString("\n" + dotIndent + "subgraph " + cluster + " {\n")
	b.WriteString(dotIndent + dotIndent + `graph [labeljust="l", label="` +
		dotLabel(append([]string{heading, ""}, wrapped(said, dotWrap)...)) + `"]` + "\n\n")
	b.WriteString(nodes)
	b.WriteString(dotIndent + "}\n")

	return b.String()
}

// The table's borders: none around the table itself, one around each cell, and
// no space between two of them — which is the HTML-like spelling of the ruled
// grid a Markdown table renders as.
const dotTableAttributes = `BORDER="0" CELLBORDER="1" CELLSPACING="0"`

// dotTableNode is one HTML-like `<TABLE>` on a `shape=plaintext` node: an
// optional title row spanning the table, an optional header row, and the rows
// beneath them.
//
// Every cell arrives already escaped, and some arrive as markup — [dotItem]'s
// italics — so this composes and escapes nothing. That is the one place in this
// emitter where the "escape the composed phrase" rule of [dot] does not hold,
// and it is why every caller of this function escapes each cell as it builds it.
func dotTableNode(name, title string, header []string, rows [][]string) string {
	var b strings.Builder

	b.WriteString(dotIndent + dotIndent + name + ` [shape="` + dotTableShape + `", label=<` + "\n")
	b.WriteString(dotIndent + dotIndent + dotIndent + "<TABLE " + dotTableAttributes + ">\n")

	// The span is the whole table, so that a title and a row of headings line up
	// with the columns beneath them however many there are. Read off both, since
	// a table with no header row is one whose width only its rows state.
	columns := len(header)
	for _, row := range rows {
		if len(row) > columns {
			columns = len(row)
		}
	}

	across := strconv.Itoa(columns)

	if title != "" {
		b.WriteString(dotIndent + dotIndent + dotIndent + dotIndent +
			`<TR><TD COLSPAN="` + across + `"><B>` + title + "</B></TD></TR>\n")
	}

	if len(header) != 0 {
		b.WriteString(dotIndent + dotIndent + dotIndent + dotIndent + "<TR>")

		for _, cell := range header {
			b.WriteString("<TD><B>" + cell + "</B></TD>")
		}

		b.WriteString("</TR>\n")
	}

	for _, row := range rows {
		b.WriteString(dotIndent + dotIndent + dotIndent + dotIndent + "<TR>")

		for at, cell := range row {
			// A cell spanning the rest of the table where the row is short, which
			// is the sentence standing in for a record with no items. A row that
			// simply stopped early would draw a table with a hole in it.
			if len(row) < len(header) && at == len(row)-1 {
				b.WriteString(`<TD COLSPAN="` + across + `">` + cell + "</TD>")

				continue
			}

			b.WriteString("<TD>" + cell + "</TD>")
		}

		b.WriteString("</TR>\n")
	}

	b.WriteString(dotIndent + dotIndent + dotIndent + "</TABLE>\n")
	b.WriteString(dotIndent + dotIndent + ">]\n")

	return b.String()
}

// dotLabel is a multi-line, left-justified label: each line escaped, and each
// ending in `\l`.
//
// `\l` rather than `\n`, because Graphviz's `\n` centres the line it ends and
// `\l` left-justifies it. That is the acceptance criterion's own wording and it
// is the point of the notation: an edge label is a sentence in three sections
// and a centred one has no left edge for the eye to come back to, which is
// exactly the failure Mermaid's stacked self-loops already have.
//
// Every line ends in one, the last included. A label whose last line did not
// would left-justify every line but that one, which is the ragged shape this is
// avoiding drawn one line short of the whole way.
func dotLabel(lines []string) string {
	var b strings.Builder

	for _, one := range lines {
		b.WriteString(dotString(one) + `\l`)
	}

	return b.String()
}

// wrapped is a paragraph broken into lines no wider than at, on the spaces
// between words.
//
// A word longer than the column gets a line of its own and overruns it, rather
// than being broken: the long words in this document are a record's name and a
// literal's bytes, and a name broken across two lines is one a reader cannot
// search their copybook for.
func wrapped(text string, at int) []string {
	var (
		lines []string
		line  string
	)

	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) <= at:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}

	if line != "" {
		lines = append(lines, line)
	}

	return lines
}

// dotString is a string as a quoted Graphviz attribute may carry it.
//
// # The rule
//
// `"` and `\` are backslash-escaped, a character that would end the line is
// flattened to a space, and everything else is written as it stands.
//
// # Why that is the whole of it
//
// A Graphviz quoted string is not a grammar the way a Mermaid label is. `"` ends
// it, and `\` is the escape character — which matters twice over, because
// Graphviz also reads `\l`, `\n`, `\r` as line breaks and `\N`, `\G`, `\E`, `\T`,
// `\H` and `\L` as substitutions naming the node, graph or edge. A record called
// `PATH\NAME` would otherwise draw as the node's own name, which is a label that
// is confidently wrong rather than one that fails to parse. Doubling the
// backslash reaches all of those at once.
//
// Nothing else in a quoted string means anything: `<`, `>`, `&`, `;`, `|`, `#`
// and `:` are ordinary characters there, and escaping one would put a backslash
// in front of a reader for no reason. That is the same posture [markdownCell]
// takes towards a `|` outside a table — a name corrupted to protect this
// generator from a character that does nothing where it stands.
//
// A newline and a carriage return are flattened rather than escaped, as
// [markdownCell] flattens them. This label's line breaks are the emitter's, and
// a name carrying one of its own would put half of itself on a line the label
// did not ask for — an escaped `\n` would do the same thing more deliberately.
// Every other control character goes the same way: it draws as nothing at all,
// which in a cell reads as a name shorter than the one the copybook spells.
func dotString(s string) string {
	var b strings.Builder

	for _, r := range s {
		switch {
		case r == '"', r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20, r == 0x7F:
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}

	return b.String()
}

// dotHTML is a string as an HTML-like label may carry it.
//
// A different notation inside the same file, and so a different escaping. The
// text of an HTML-like label is markup: `<` opens a tag, `&` opens an entity,
// and a name carrying either would be read as this emitter's own structure —
// which for `<` is a document Graphviz refuses to parse, and for `&` a name with
// a piece missing. `>` is escaped with them because an unbalanced one is the
// half of a tag a reader cannot tell from the whole, and `"` because these
// cells sit inside a label delimited by `<` and `>` rather than by quotes and a
// reader of the source should not have to work out which rules apply where.
//
// Control characters are flattened as [dotString] flattens them, and for the
// same reason.
func dotHTML(s string) string {
	var b strings.Builder

	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r < 0x20, r == 0x7F:
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}

	return b.String()
}
