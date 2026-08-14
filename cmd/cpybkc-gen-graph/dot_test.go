// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/irpb"
)

// TestTheGraphvizDocumentIsADigraphDrawnTheWayAStateDiagramIs is the shape of
// the rendering, which is the half of it a reader recognises before they read a
// word.
//
// A `digraph`, states as circles, an accepting state as a `doublecircle` and a
// `point` where a read begins: none of those is this generator's invention, and
// a reader who knows state diagrams and nothing about this project reads all
// four without being told. They are asserted together because each is only
// meaningful beside the others — a `doublecircle` says "accepting" only where an
// ordinary state is a circle.
func TestTheGraphvizDocumentIsADigraphDrawnTheWayAStateDiagramIs(t *testing.T) {
	t.Parallel()

	// Two accepting states and one that is not, so that "every accepting state
	// is doubled" is distinguishable from "the last one is".
	written := writtenGraphviz(t, &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			unframedFile(1, 2),
			stateNode(2, false, 30, 31),
			stateNode(3, true),
			stateNode(4, true),
			recordNode(5, "LEFT", ""),
			groupNode(21, "LEFT"),
			recordNode(6, "RIGHT", ""),
			groupNode(22, "RIGHT"),
			transitionNode(30, 5, 3),
			transitionNode(31, 6, 4),
		},
	})

	for _, want := range []string{
		dotOpen,
		dotIndent + dotStart + ` [shape="point"]`,
		dotIndent + dotStart + " -> s2",
		dotIndent + `s2 [shape="circle"]`,
		dotIndent + `s3 [shape="doublecircle"]`,
		dotIndent + `s4 [shape="doublecircle"]`,
	} {
		if !strings.Contains(written, want+"\n") {
			t.Errorf("the digraph does not carry %q:\n%s", want, written)
		}
	}

	// The start state does not accept, so end of input there is a truncated file
	// and the diagram may not draw it as a state a read may finish in.
	if strings.Contains(written, `s2 [shape="doublecircle"`) {
		t.Errorf("state 2 does not accept and is drawn as an accepting state:\n%s", written)
	}

	graphvizAccepts(t, written)
}

// TestAnEdgeLabelIsOneLeftJustifiedLinePerSection is the other half of the
// shape, and the reason this notation exists.
//
// Mermaid has no line break inside a transition label, so an edge carrying a
// record, a predicate, guards and bindings is one long line — and a state
// offering six alternatives is six of them, stacked, overrunning each other.
// Here each section is a line of its own ending in `\l`, which is Graphviz's
// left-justifying break: the sections line up down a left edge the eye can come
// back to.
func TestAnEdgeLabelIsOneLeftJustifiedLinePerSection(t *testing.T) {
	t.Parallel()

	// Everything a transition may carry, so that the label has all four
	// sections: a record, a predicate, a guard and a binding.
	written := writtenGraphviz(t, oneRecordAutomaton(
		integerRegister(60),
		equalPredicate(50, 101, "H"),
		positiveGuard(70, 60),
		decrementBinding(80, 60),
		edgeNode(30, 100, 2, predicateAt(50), []uint64{70}, []uint64{80}),
	))

	want := dotIndent + `s2 -> s2 [label="HEADER-RECORD\lwhen TYPE-CODE = 0x48\l` +
		`if r60 greater than zero\lthen r60 = r60 - 1\l"]`

	if !strings.Contains(written, want+"\n") {
		t.Errorf("the edge is not labelled %q:\n%s", want, written)
	}

	// `\n` centres the line it ends and `\l` left-justifies it, so a label that
	// broke its lines the other way would draw a ragged sentence with no left
	// edge — which is the shape being avoided.
	for _, line := range strings.Split(written, "\n") {
		if strings.Contains(line, "[label=") && strings.Contains(line, `\n`) {
			t.Errorf("the label %q breaks a line with a centring escape", line)
		}
	}
}

// TestTheTwoRenderingsAreTwoConsumersOfOneWalk is the acceptance criterion that
// nothing about the model moved: no fact reaches one rendering and not the
// other.
//
// Asserted over the facts a reader is checking rather than over the notation
// each is spelled in — the states, the records, the registers and what they
// hold, the predicates, guards and bindings on each transition, and every item's
// offset, width, usage and picture. What the two documents are allowed to differ
// in is how a thing is *drawn*; what they may not differ in is what is there.
func TestTheTwoRenderingsAreTwoConsumersOfOneWalk(t *testing.T) {
	t.Parallel()

	markdown := flattened(writtenDocument(t, countedAutomaton()))
	graphviz := flattened(writtenGraphviz(t, countedAutomaton()))

	facts := []string{
		// The automaton.
		"s2", "s3", "s4",
		"HEADER-RECORD", "DETAIL-RECORD", "SUMMARY-RECORD",
		"when TYPE-CODE = 0xC8", "when TYPE-CODE = 0xC4", "when TYPE-CODE = 0xE2",
		"if r20 greater than zero", "if r20 = 0 and r21 = 0xE8",
		"if r20 = 0 and r21 is one of 0xD5 or 0x40",
		"then r20 = DTL-COUNT and r21 = SUM-FLAG and r22 = TOTAL-COUNT",
		"then r20 = r20 - 1",
		"no predicate",

		// The framing, which a state machine cannot say.
		"the delimiter is 0x15",
		"terminator (one follows every record, the last included)",

		// The registers, and the transitions that write them.
		"r20", "r21", "r22", "an integer", "bytes",

		// The items, with the offsets this generator summed for them.
		"TYPE-CODE", "DTL-COUNT", "SUM-FLAG", "TOTAL-COUNT", "AMOUNT",
		"LINE.LINE-TEXT", "NOTE.NOTE-TEXT",
		"DISPLAY", "X(1)", "9(2)", "9(3)", "X(3)", "X(2)", "always",
	}

	for _, fact := range facts {
		if !strings.Contains(markdown, fact) {
			t.Errorf("the Markdown document does not carry %q", fact)
		}

		if !strings.Contains(graphviz, fact) {
			t.Errorf("the Graphviz document does not carry %q", fact)
		}
	}
}

// flattened is a document with every line break it draws turned back into a
// space, so that a fact can be looked for without knowing which notation broke
// which line where.
//
// Both notations break lines, and neither breaks them where the other does: a
// Graphviz label wraps its prose at [dotWrap] and ends every line with `\l`,
// where a Markdown paragraph is one line however long it is. Which of them a
// sentence is broken in is exactly what [TestTheTwoRenderingsAreTwoConsumersOfOneWalk]
// is not about.
func flattened(document string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(document, `\l`, " ")), " ")
}

// TestTheRecordTablesAreClusteredAndNothingRunsIntoThem is the acceptance
// criterion about where the item tables sit.
//
// The cluster is what keeps a page-high table from being ranked among the
// states it is about, and the absence of edges is what keeps the ranking
// meaningful at all: an edge from the transition that admits a record to the
// table of that record's items would cross the whole drawing to say what the
// edge's own label already says.
func TestTheRecordTablesAreClusteredAndNothingRunsIntoThem(t *testing.T) {
	t.Parallel()

	written := writtenGraphviz(t, countedAutomaton())

	for _, want := range []string{
		"subgraph " + dotRecordsCluster + " {",
		`record100 [shape="plaintext", label=<`,
		"<TABLE " + dotTableAttributes + ">",
		"<TR><TD COLSPAN=\"6\"><B>HEADER-RECORD</B></TD></TR>",
	} {
		if !strings.Contains(written, want) {
			t.Errorf("the record cluster does not carry %q:\n%s", want, written)
		}
	}

	// Every edge in the document, and none of them may name a table node. The
	// arrow is what an edge is, so this finds one wherever it was written.
	for _, line := range strings.Split(written, "\n") {
		if !strings.Contains(line, " -> ") || strings.Contains(line, "<TD>") {
			continue
		}

		if strings.Contains(line, "record") {
			t.Errorf("an edge runs into the record cluster: %q", line)
		}
	}

	graphvizAccepts(t, written)
}

// TestTheRecordClusterIsLeftOutWhereItWouldBeEmpty is `records=none` from the
// side the Graphviz document shows it: the pair of goldens says the tables are
// gone, and this says the box that would hold them is gone with them.
//
// A cluster with no node in it draws as an empty labelled box, which reads as a
// section this generator failed to fill in — the same failure a heading over an
// empty table is in the other notation.
func TestTheRecordClusterIsLeftOutWhereItWouldBeEmpty(t *testing.T) {
	t.Parallel()

	written := writtenGraphviz(t, countedAutomaton(), optFlag, recordsOption+"="+recordsNone)

	if strings.Contains(written, dotRecordsCluster) {
		t.Errorf("records=none drew a record cluster:\n%s", written)
	}

	// And the registers are still there, because they are not what the option
	// is about: `records=none` asks for the sequencing view, and how the
	// automaton remembers between records is part of that.
	if !strings.Contains(written, dotRegistersCluster) {
		t.Errorf("records=none dropped the register table, which is not what it asks for:\n%s", written)
	}

	graphvizAccepts(t, written)
}

// TestAStateNoPathReachesIsDrawnAndSaidToBeUnreachable is the same criterion
// [TestAnUnreachableStateIsDrawnAndSaidToBeUnreachable] holds of the Markdown
// document, in the notation this one has for it.
func TestAStateNoPathReachesIsDrawnAndSaidToBeUnreachable(t *testing.T) {
	t.Parallel()

	written := writtenGraphviz(t, &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			unframedFile(1, 2),
			stateNode(2, true),
			stateNode(9, true),
		},
	})

	if want := `s9 [shape="doublecircle", label="s9\n(unreachable)"]`; !strings.Contains(written, want) {
		t.Errorf("state 9 is reached by nothing and the digraph does not mark it:\n%s", written)
	}

	// The reachable state carries no such mark: a document that marked every
	// state would pass a test looking only for the word.
	if strings.Contains(written, `s2 [shape="doublecircle", label=`) {
		t.Errorf("the start state is marked unreachable:\n%s", written)
	}

	// And in the prose above the diagram too, because the mark says which state
	// and the sentence says why it is worth looking at.
	for _, want := range []string{"s9", "not reachable from the start state"} {
		if !strings.Contains(written, want) {
			t.Errorf("the document does not say %q:\n%s", want, written)
		}
	}

	graphvizAccepts(t, written)
}

// TestAnAcceptingStatesGuardsAreDrawnBesideIt is conditional acceptance, which
// is the one thing Mermaid's `--> [*]` edge carries that a `doublecircle` does
// not.
//
// A state that accepts only with the counter run down is how a file two records
// short is told from a complete one, and a doubled circle with nothing beside it
// would say the opposite. It is an `xlabel` — text Graphviz places beside a node
// — because a circle grows to fit whatever is written inside it, and a guard
// written in one draws a state larger than the rest of the diagram.
func TestAnAcceptingStatesGuardsAreDrawnBesideIt(t *testing.T) {
	t.Parallel()

	written := writtenGraphviz(t, oneRecordAutomaton(
		integerRegister(60),
		equalsIntegerGuard(70, 60, 0),
		guardedStateNode(2, true, []uint64{70}, 30),
		edgeNode(30, 100, 2, nil, nil, nil),
	))

	if want := `xlabel="accepts if r60 = 0\l"`; !strings.Contains(written, want) {
		t.Errorf("the accepting state's guards are not drawn beside it as %q:\n%s", want, written)
	}

	graphvizAccepts(t, written)
}

// TestAStringADotDocumentReactsToIsEscaped is the escaping rule, over both of
// the notations inside a Graphviz document: the quoted strings a label is, and
// the HTML-like markup a table is.
//
// The pair the acceptance criterion names is here: a value carrying a quote or a
// backslash, which are the two characters a quoted string is made of, and a
// COBOL name carrying neither — because a rule that escaped `ORDER-HEADER` would
// put backslashes in front of every name in every diagram this generator draws,
// which is the same failure by the opposite route.
func TestAStringADotDocumentReactsToIsEscaped(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		raw     string
		quoted  string
		markup  string
		unmoved bool
	}{
		{
			name: "a COBOL name reacting to nothing is written as it stands",
			raw:  "ORDER-HEADER", quoted: "ORDER-HEADER", markup: "ORDER-HEADER", unmoved: true,
		},
		{
			name: "a name that is not ASCII at all is written as it stands",
			raw:  "ÜBERWEISUNG", quoted: "ÜBERWEISUNG", markup: "ÜBERWEISUNG", unmoved: true,
		},
		{
			name: "a quote would end the string it is in",
			raw:  `SAY "HI"`, quoted: `SAY \"HI\"`, markup: "SAY &quot;HI&quot;",
		},
		{
			name: "a backslash is the escape character itself",
			raw:  `A\B`, quoted: `A\\B`, markup: `A\B`,
		},
		{
			// The reason a backslash is escaped rather than left alone: `\N` is
			// Graphviz's substitution for the node's own name, so this would
			// otherwise draw as `s2` — a label confidently saying something else
			// rather than one that fails to parse.
			name: "a backslash that would name the node instead",
			raw:  `PATH\NAME`, quoted: `PATH\\NAME`, markup: `PATH\NAME`,
		},
		{
			name: "a line of its own is flattened, as a Markdown cell flattens one",
			raw:  "line\nbreak", quoted: "line break", markup: "line break",
		},
		{name: "a carriage return the same", raw: "line\rbreak", quoted: "line break", markup: "line break"},
		{
			name: "a tag would be this emitter's own markup",
			raw:  "<B>bold</B>", quoted: "<B>bold</B>", markup: "&lt;B&gt;bold&lt;/B&gt;",
		},
		{
			name: "an entity would be a name with a piece missing",
			raw:  "AT&T", quoted: "AT&T", markup: "AT&amp;T",
		},
		{
			// Ordinary characters in both notations. Escaping one would put a
			// backslash in front of a reader for a character that does nothing
			// where it stands, which is what [markdownCell] refuses to do to a
			// pipe outside a table.
			name: "the characters another notation reacts to and this one does not",
			raw:  "a|b;c#d:e", quoted: "a|b;c#d:e", markup: "a|b;c#d:e",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := dotString(testCase.raw); got != testCase.quoted {
				t.Errorf("in a quoted string %q is written %q, want %q", testCase.raw, got, testCase.quoted)
			}

			if got := dotHTML(testCase.raw); got != testCase.markup {
				t.Errorf("in an HTML-like label %q is written %q, want %q", testCase.raw, got, testCase.markup)
			}

			if testCase.unmoved && dotString(testCase.raw) != testCase.raw {
				t.Errorf("%q is a name this notation reacts to nothing in, and it was escaped anyway", testCase.raw)
			}
		})
	}
}

// TestANameCarryingAGraphvizMetacharacterDoesNotBreakTheDocument is the same
// rule where it matters — in a whole document, over a name that reaches a label,
// a node's own name and a table at once.
//
// The property is that no name can change the structure of the document: the
// digraph holds the same lines whatever a record is called, the table is still a
// table, and Graphviz accepts the file. A label drawing a name wrongly is ugly;
// one that ends the string it is in is a file that does not parse, and that is
// the outcome ruled out here.
func TestANameCarryingAGraphvizMetacharacterDoesNotBreakTheDocument(t *testing.T) {
	t.Parallel()

	for _, named := range []string{
		`SAY "HI"`, `PATH\NAME`, `A\"B`, "<TABLE>", "AT&T", "line\nbreak", "TRAILER ", "ÜBERWEISUNG",
	} {
		t.Run(named, func(t *testing.T) {
			t.Parallel()

			written := writtenGraphviz(t, &irpb.Descriptor{
				Version: supportedIRVersion,
				Nodes: []*irpb.Node{
					unframedFile(1, 2),
					stateNode(2, true, 30),
					recordNode(4, named, ""),
					groupNode(20, named),
					transitionNode(30, 4, 2),
				},
			})

			// The edge label, with the name escaped for a quoted string, and the
			// record's table title with it escaped for markup. One name, two
			// notations, in one file.
			label := dotIndent + `s2 -> s2 [label="` + dotString(named) + `\l` + noPredicate + `\l"]`
			if !strings.Contains(written, label+"\n") {
				t.Errorf("the edge is not labelled %q:\n%s", label, written)
			}

			title := `<TD COLSPAN="1"><B>` + dotHTML(named) + "</B></TD>"
			if !strings.Contains(written, title) {
				t.Errorf("the record's table is not titled %q:\n%s", title, written)
			}

			graphvizAccepts(t, written)
		})
	}
}

// TestAQuotedLiteralIsEscapedWhereverItReachesTheDocument is the half of the
// escaping rule that a per-name escaping pass cannot reach.
//
// A predicate over an empty literal draws as `TYPE-CODE = ""`, and those quotes
// are the model's rather than a name's: [literalBytes] writes them past whatever
// escaping an emitter applies to a name, because no Mermaid label may carry a
// quote and a Graphviz label very much may. So this emitter escapes the composed
// phrase rather than the names inside it, and this is the case that says so.
func TestAQuotedLiteralIsEscapedWhereverItReachesTheDocument(t *testing.T) {
	t.Parallel()

	written := writtenGraphviz(t, oneRecordAutomaton(
		equalPredicate(50, 101, ""),
		edgeNode(30, 100, 2, predicateAt(50), nil, nil),
	))

	if want := `when TYPE-CODE = \"\"\l`; !strings.Contains(written, want) {
		t.Errorf("the predicate's empty literal is not escaped as %q:\n%s", want, written)
	}

	graphvizAccepts(t, written)
}

// TestALiteralCarryingAQuoteReachesTheDocumentAsBytes is the other direction of
// the same rule, and the reason a quote in somebody's data never becomes a quote
// in this document.
//
// [literalBytes] draws a literal as text only where every byte of it is
// printable ASCII that no notation reacts to, and a quote or a backslash is
// neither — so what reaches the label is the bytes, which say the same thing and
// need no convention for saying it.
func TestALiteralCarryingAQuoteReachesTheDocumentAsBytes(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "a quote", value: `"`, want: "when ENTRY.SUB.KIND = 0x22"},
		{name: "a backslash", value: `\`, want: "when ENTRY.SUB.KIND = 0x5C"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			written := writtenGraphviz(t, oneRecordAutomaton(
				encodedFieldNode(106, "KIND", 1, irpb.Charset_CHARSET_ASCII),
				equalPredicate(50, 106, testCase.value),
				edgeNode(30, 100, 2, predicateAt(50), nil, nil),
			))

			if !strings.Contains(written, testCase.want) {
				t.Errorf("the document does not carry %q:\n%s", testCase.want, written)
			}

			graphvizAccepts(t, written)
		})
	}
}

// TestARecordWithNoItemsIsSaidRatherThanDrawnEmpty is [mermaidNoItems]'s case in
// the other notation: a table with no rows reads as a generator that gave up
// halfway, and this is a record described as holding no bytes at all.
func TestARecordWithNoItemsIsSaidRatherThanDrawnEmpty(t *testing.T) {
	t.Parallel()

	written := writtenGraphviz(t, &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			unframedFile(1, 2),
			stateNode(2, true, 30),
			recordNode(4, "EMPTY-RECORD", ""),
			groupNode(20, "EMPTY-RECORD"),
			transitionNode(30, 4, 2),
		},
	})

	if !strings.Contains(written, dotHTML(dotNoItems)) {
		t.Errorf("a record holding no item is drawn as an empty table rather than said:\n%s", written)
	}

	graphvizAccepts(t, written)
}

// TestEveryGoldenIsValidGraphviz is the acceptance criterion that this
// generator's output is Graphviz rather than something that looks like it.
//
// Asserted by the tool rather than by a reading of it: a `.dot` Graphviz refuses
// is a failing test and not a rendering preference, and no assertion this
// package could write over the text would find an unbalanced quote or a table
// with a row the wrong width the way `dot` does in one call.
//
// # Why it skips
//
// Graphviz is not a dependency of this repository, of this generator, or of
// anything that consumes what it writes — the whole point of the notation is
// that the file is somebody else's to render. So a machine without it runs every
// other test in this package and skips this one, rather than failing over a tool
// nothing here requires. Where it is present, which is a developer's machine and
// any image with it installed, this is the assertion.
func TestEveryGoldenIsValidGraphviz(t *testing.T) {
	t.Parallel()

	if graphviz() == "" {
		t.Skip("dot is not on PATH, so the goldens cannot be put to Graphviz")
	}

	for name := range goldens() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(goldenDir, name+".dot")

			written, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading the golden: %v", err)
			}

			graphvizAccepts(t, string(written))
		})
	}
}

// graphviz is the `dot` on PATH, and the empty string where there is none.
func graphviz() string {
	found, err := exec.LookPath("dot")
	if err != nil {
		return ""
	}

	return found
}

// graphvizAccepts fails the test unless Graphviz parses and lays out the
// document, and does nothing at all where Graphviz is not installed.
//
// `-Tdot` rather than an image format: it runs the whole of the layout — which
// is what parses an HTML-like label — and needs none of the renderers an image
// would. The output goes nowhere, because what is being asserted is that the
// document was accepted and not what it draws.
//
// A non-zero exit is the failure, as the acceptance criterion has it. What
// Graphviz writes to standard error is reported with it rather than being a
// failure of its own: a missing font is a warning on some machines and says
// nothing about the document.
func graphvizAccepts(t *testing.T, document string) {
	t.Helper()

	if graphviz() == "" {
		return
	}

	path := filepath.Join(t.TempDir(), "graph.dot")

	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatalf("writing the document for Graphviz: %v", err)
	}

	said, err := exec.Command(graphviz(), "-Tdot", "-o", os.DevNull, path).CombinedOutput()
	if err != nil {
		t.Errorf("Graphviz refused the document: %v\n%s\n%s", err, said, document)
	}
}

// TestProseInALabelIsWrappedRatherThanDrawnOnOneLine is why [wrapped] exists.
//
// A Graphviz label is one line unless something breaks it, so a paragraph in one
// is a diagram as wide as the paragraph, with the automaton drawn somewhere in
// the middle of it at whatever size is left over.
func TestProseInALabelIsWrappedRatherThanDrawnOnOneLine(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		text string
		at   int
		want []string
	}{
		{name: "nothing", text: "", at: 10, want: nil},
		{name: "a line that fits", text: "one two", at: 10, want: []string{"one two"}},
		{name: "a line that does not", text: "one two three", at: 10, want: []string{"one two", "three"}},
		{
			// A record's name and a literal's bytes are the long words here, and
			// a name broken across two lines is one a reader cannot search their
			// copybook for.
			name: "a word longer than the column overruns it rather than breaking",
			text: "a VERY-LONG-COBOL-NAME b", at: 8,
			want: []string{"a", "VERY-LONG-COBOL-NAME", "b"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := wrapped(testCase.text, testCase.at)

			if len(got) != len(testCase.want) {
				t.Fatalf("%q wrapped at %d is %q, want %q", testCase.text, testCase.at, got, testCase.want)
			}

			for at, line := range testCase.want {
				if got[at] != line {
					t.Errorf("line %d is %q, want %q", at+1, got[at], line)
				}
			}
		})
	}
}
