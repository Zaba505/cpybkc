// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Zaba505/cpybkc/irpb"
)

// TestAHyphenatedNameIsDrawnAsTheCopybookSpellsIt is the case the escaping rule
// is chosen around rather than a corner of it.
//
// COBOL names are full of hyphens, so a rule that escaped `-` would render
// every record in every diagram this generator draws as `ORDER#45;HEADER`,
// which is a document nobody reads. It is safe to leave literal because what
// Mermaid's parser reacts to is the arrow `-->`, and `>` is not a rune the rule
// admits.
func TestAHyphenatedNameIsDrawnAsTheCopybookSpellsIt(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"ORDER-HEADER", "TRAILER-99", "A-B-C-D", "Detail line 2", "cust.addr"} {
		if got := mermaidLabel(name); got != name {
			t.Errorf("the label for %q is %q, and a name a diagram can carry is carried", name, got)
		}
	}
}

// TestARecordNameCarryingAMermaidMetacharacterDoesNotBreakTheDiagram is the
// acceptance criterion, asserted as the property the escaping rule actually
// buys rather than as the text it happens to produce.
//
// The property is that no name can change the *shape* of the diagram: whatever
// a record is called, the block holds exactly the lines the automaton has, each
// label sits on one line, and no rune outside the admitted set ever reaches the
// output. A renderer that does not decode `#58;` draws that label literally,
// which is ugly and still a diagram; a label carrying a raw `:` or a newline is
// a block that does not parse at all, and that is the outcome ruled out here.
func TestARecordNameCarryingAMermaidMetacharacterDoesNotBreakTheDiagram(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		named string
	}{
		{name: "an arrow", named: "A-->B"},
		{name: "the separator a label follows", named: "TXN: PAID"},
		{name: "a statement separator", named: "one;two"},
		{name: "a line of its own", named: "line\nbreak"},
		{name: "a carriage return", named: "line\rbreak"},
		{name: "the start and end pseudostate", named: "[*]"},
		{name: "a state declaration", named: `state "x" as y`},
		{name: "a directive", named: "%%{init: {}}%%"},
		{name: "a comment", named: "%% not a comment"},
		{name: "the escape itself", named: "#35;"},
		{name: "a note", named: "note right of s2: hello"},
		{name: "a composite state", named: "s2 { s3 }"},
		{name: "a name that is not ASCII at all", named: "ÜBERWEISUNG"},
		{name: "a trailing space", named: "TRAILER "},
		{name: "a leading space", named: " HEADER"},
		{name: "spaces at both ends and in the middle", named: "  two words  "},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			written := writtenDocument(t, &irpb.Descriptor{
				Version: supportedIRVersion,
				Nodes: []*irpb.Node{
					unframedFile(1, 2),
					stateNode(2, true, 30),
					recordNode(4, testCase.named, ""),
					groupNode(20, testCase.named),
					transitionNode(30, 4, 2),
				},
			})

			// One state, one transition to itself, and it accepts: three lines,
			// whatever the record is called. A label that ended its line early
			// or opened a construct of its own would change this count.
			body := fenced(t, written)

			want := []string{
				mermaidDiagram,
				mermaidIndent + "[*] --> s2",
				// The record's name, escaped, and behind it the rest of what the
				// transition carries — which here is a transition carrying no
				// predicate saying so.
				mermaidIndent + "s2 --> s2: " + mermaidLabel(testCase.named) + ", " + noPredicate,
				mermaidIndent + "s2 --> [*]",
			}

			if len(body) != len(want) {
				t.Fatalf("the diagram holds %d lines, want %d:\n%s", len(body), len(want), written)
			}

			for i, line := range want {
				if body[i] != line {
					t.Errorf("line %d of the diagram is %q, want %q", i+1, body[i], line)
				}
			}

			// And the rule itself, over the label the name became: every rune
			// of it is either inside a well-formed escape or one the rule
			// admits verbatim, and what it all decodes to is the name.
			label := mermaidLabel(testCase.named)

			if got := scanned(t, label); got != testCase.named {
				t.Errorf("the label for %q decodes to %q, and an escape stands for what it escaped", testCase.named, got)
			}
		})
	}
}

// TestTheStartStateAndEveryAcceptingStateAreMarked is two acceptance criteria
// that share a mechanism: Mermaid's start and end pseudostates.
//
// `[*] --> s` and `s --> [*]` are how a state diagram says "a read begins here"
// and "a read may finish here", so a reader who knows state diagrams and
// nothing about this project reads both without being told.
func TestTheStartStateAndEveryAcceptingStateAreMarked(t *testing.T) {
	t.Parallel()

	// Two accepting states and one that is not, so that "every accepting state
	// is marked" is distinguishable from "the last state is".
	written := writtenDocument(t, &irpb.Descriptor{
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

	for _, want := range []string{"[*] --> s2", "s3 --> [*]", "s4 --> [*]"} {
		if !strings.Contains(written, mermaidIndent+want+"\n") {
			t.Errorf("the diagram does not carry %q:\n%s", want, written)
		}
	}

	// State 2 does not accept, so end of input there is a truncated file and
	// the diagram may not say a read can finish in it.
	if strings.Contains(written, mermaidIndent+"s2 --> [*]\n") {
		t.Errorf("the diagram lets a read finish in state 2, which does not accept:\n%s", written)
	}
}

// TestAnUnreachableStateIsDrawnAndSaidToBeUnreachable is the visible half of
// "every state the descriptor carries is drawn": drawing it and leaving it
// looking like every other state would put a reader in front of a bug with
// nothing pointing at it.
func TestAnUnreachableStateIsDrawnAndSaidToBeUnreachable(t *testing.T) {
	t.Parallel()

	written := writtenDocument(t, &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			unframedFile(1, 2),
			stateNode(2, true),
			stateNode(9, true),
		},
	})

	if !strings.Contains(written, `state "s9 (unreachable)" as s9`) {
		t.Errorf("state 9 is reached by nothing and the diagram does not mark it:\n%s", written)
	}

	// And in the prose too, because the diagram's mark says which state and the
	// sentence says why it is worth looking at.
	for _, want := range []string{"s9", "not reachable from the start state"} {
		if !strings.Contains(written, want) {
			t.Errorf("the document does not say %q:\n%s", want, written)
		}
	}

	// The reachable state carries no such mark: a document that called every
	// state unreachable would pass a test looking only for the phrase.
	if strings.Contains(written, "s2 (unreachable)") {
		t.Errorf("the start state is marked unreachable:\n%s", written)
	}
}

// TestTheUnreachableSentenceAgreesWithHowManyThereAre is a small thing that is
// worth a test because it is the only prose in this document that counts
// something.
//
// The sentence is what a reader meets before the diagram, and one reading
// "States s9 are not reachable" is one they stop trusting.
func TestTheUnreachableSentenceAgreesWithHowManyThereAre(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		nodes   []*irpb.Node
		opens   string
		follows string
	}{
		{
			name:    "one",
			nodes:   []*irpb.Node{unframedFile(1, 2), stateNode(2, true), stateNode(9, true)},
			opens:   "State s9 is not reachable",
			follows: "It is drawn anyway",
		},
		{
			name:    "two",
			nodes:   []*irpb.Node{unframedFile(1, 2), stateNode(2, true), stateNode(8, true), stateNode(9, true)},
			opens:   "States s8, s9 are not reachable",
			follows: "Each is drawn anyway",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			written := writtenDocument(t, &irpb.Descriptor{Version: supportedIRVersion, Nodes: testCase.nodes})

			for _, want := range []string{testCase.opens, testCase.follows} {
				if !strings.Contains(written, want) {
					t.Errorf("the document does not read %q:\n%s", want, written)
				}
			}
		})
	}
}

// TestADocumentWithNothingWrongCarriesNoUnreachableProse keeps the sentence
// above from being boilerplate every document carries.
func TestADocumentWithNothingWrongCarriesNoUnreachableProse(t *testing.T) {
	t.Parallel()

	written := writtenDocument(t, ordersAutomaton(unframed4()))

	if strings.Contains(written, "unreachable") {
		t.Errorf("an automaton whose states are all reachable produced a document mentioning unreachable ones:\n%s", written)
	}
}

// admitted reports whether a rune may stand in a label **verbatim**.
//
// Written out here rather than shared with [mermaidLabel], deliberately: a test
// that called the implementation's own predicate would pass for any rule at
// all, including one that admitted everything.
//
// `#` and `;` are not in it, and that is the point of the split below. They are
// the two runes an escape is built out of, and admitting them outright — which
// this predicate used to — made a literal `;` in a label invisible to every
// assertion in this file, including the case named "a statement separator". A
// rune is now either part of a well-formed escape, which [scanned] consumes as
// a unit, or one of these; there is no third way to reach the output.
func admitted(r rune) bool {
	return r >= 'a' && r <= 'z' ||
		r >= 'A' && r <= 'Z' ||
		r >= '0' && r <= '9' ||
		r == '-' || r == '_' || r == '.' || r == ' '
}

// scanned walks a label, consuming a well-formed `#<decimal>;` escape wherever
// one stands and requiring every other rune to be one [admitted] allows, and
// answers with what the label decodes to.
//
// The two halves have to be asserted together. "Nothing dangerous reaches the
// output" is satisfied on its own by a rule that dropped every rune it did not
// like, and "the label decodes to the name" is satisfied on its own by a rule
// that escaped nothing at all.
func scanned(t *testing.T, label string) string {
	t.Helper()

	var b strings.Builder

	for at := 0; at < len(label); {
		if label[at] == '#' {
			// The shortest escape is `#0;`, so a `;` at offset 1 is a `#`
			// followed by nothing rather than an escape of anything.
			if end := strings.IndexByte(label[at:], ';'); end > 1 {
				if code, err := strconv.Atoi(label[at+1 : at+end]); err == nil {
					b.WriteRune(rune(code))
					at += end + 1

					continue
				}
			}
		}

		r, width := utf8.DecodeRuneInString(label[at:])

		if !admitted(r) {
			t.Errorf("the label %q carries %q outside an escape, and the rule admits no such rune verbatim", label, r)
		}

		b.WriteRune(r)
		at += width
	}

	return b.String()
}

// fenced is the lines of the document's `mermaid` block, without the fences.
func fenced(t *testing.T, document string) []string {
	t.Helper()

	_, after, opened := strings.Cut(document, mermaidFenceOpen+"\n")
	if !opened {
		t.Fatalf("the document carries no %s block:\n%s", mermaidFenceOpen, document)
	}

	body, _, closed := strings.Cut(after, mermaidFenceClose+"\n")
	if !closed {
		t.Fatalf("the document's %s block is never closed:\n%s", mermaidFenceOpen, document)
	}

	return strings.Split(strings.TrimSuffix(body, "\n"), "\n")
}
