// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/irpb"
)

// TestAPredicateIsDrawnOnTheEdgeItSelects is the first thing this half of the
// diagram is for.
//
// An edge naming only the record it admits answers "in what order" and nothing
// else. What a person checking a layout needs beside that is *why* the edge is
// taken, and a predicate is the whole of that answer for the bytes: the field
// it names, by the path they wrote in their own copybook, and the test it makes
// of it.
func TestAPredicateIsDrawnOnTheEdgeItSelects(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		nodes []*irpb.Node
		want  string
	}{
		{
			name:  "equal to one literal",
			nodes: []*irpb.Node{equalPredicate(50, 101, "\xc8")},
			want:  "when TYPE-CODE = 0xC8",
		},
		{
			name:  "one of a set",
			nodes: []*irpb.Node{oneOfPredicate(50, 101, "\xc4", "\xe2")},
			want:  "when TYPE-CODE is one of 0xC4 or 0xE2",
		},
		{
			// The path within the record, since a field two groups down is one
			// the reader finds by the names they wrote and not by a node
			// identifier this generator did not invent either.
			name:  "a field two groups down",
			nodes: []*irpb.Node{equalPredicate(50, 106, "\xc8")},
			want:  "when ENTRY.SUB.KIND = 0xC8",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			g := drawn(t, oneRecordAutomaton(append(testCase.nodes,
				edgeNode(30, 100, 2, predicateAt(50), nil, nil))...))

			if got := g.states[0].edges[0].label(mermaidLabel); !strings.Contains(got, testCase.want) {
				t.Errorf("the edge reads %q, and does not carry %q", got, testCase.want)
			}
		})
	}
}

// TestATransitionCarryingNoPredicateSaysSo is docs/ir/SPEC.md's "A transition
// may carry no predicate", which is a meaning and not a gap.
//
// Such a transition matches every record and gives up the undescribed-record
// diagnostic at the state offering it, so the reader most needs to see it where
// it is easiest to draw as nothing at all. It also has to be distinguishable
// from a predicate that happens to be trivial: an empty literal is a test a
// consumer runs and this is no test at all.
func TestATransitionCarryingNoPredicateSaysSo(t *testing.T) {
	t.Parallel()

	none := drawn(t, oneRecordAutomaton(edgeNode(30, 100, 2, nil, nil, nil)))
	trivial := drawn(t, oneRecordAutomaton(
		equalPredicate(50, 101, ""),
		edgeNode(30, 100, 2, predicateAt(50), nil, nil),
	))

	said, other := none.states[0].edges[0].label(mermaidLabel), trivial.states[0].edges[0].label(mermaidLabel)

	if !strings.Contains(said, noPredicate) {
		t.Errorf("a transition carrying no predicate is drawn %q, and does not say so", said)
	}

	if strings.Contains(other, noPredicate) {
		t.Errorf("a transition carrying a predicate over an empty literal is drawn %q, as though it carried none", other)
	}

	if !strings.Contains(other, `TYPE-CODE = ""`) {
		t.Errorf("a predicate over an empty literal is drawn %q, and the literal is not visible in it", other)
	}
}

// TestALiteralIsBytesUnlessItIsKnownToBeText is the rule that keeps a label
// faithful to a file this generator cannot read.
//
// A literal is bytes, and whether a byte is a character is a question only a
// charset answers. `0x40` is `@` in ASCII and a space in cp037, so a document
// that read a byte as text because it happened to be printable ASCII would
// print `"@"` for a literal that is a space in somebody's file — which is the
// mangling this rule exists to prevent, arrived at without ever naming a
// charset. Text is therefore a fact established from the target field's own
// encoding, and quotes are what make the producer's trailing pad visible where
// it is established.
func TestALiteralIsBytesUnlessItIsKnownToBeText(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		value string
		text  bool
		want  string
	}{
		{name: "a literal under a charset this generator cannot name is bytes", value: "Y", want: "0x59"},
		{
			// The byte the rule is really about: printable ASCII, and a space
			// in every EBCDIC code page this project supports.
			name:  "a byte two charsets disagree about is never guessed at",
			value: "\x40",
			want:  "0x40",
		},
		{name: "an ASCII literal is text", value: "Y", text: true, want: `"Y"`},
		{
			// The pair the criterion is about: `Y` padded to two bytes is not
			// `Y`, and a document that trimmed the pad would say it was.
			name:  "the producer's trailing pad is visible",
			value: "Y ",
			text:  true,
			want:  `"Y "`,
		},
		{name: "an empty literal is empty text and not a blank", value: "", want: `""`},
		{
			// Printable, ASCII, and still bytes: a quote inside quotes would
			// need an escape convention, and the bytes say the same thing
			// without one.
			name:  "a literal carrying a character a notation reacts to is bytes",
			value: `Y"`,
			text:  true,
			want:  "0x59 0x22",
		},
		{
			name:  "a literal carrying a byte that is not printable at all is bytes",
			value: "\x00\x15",
			text:  true,
			want:  "0x00 0x15",
		},
		{
			// The quoted form is written past every escaping pass, so a literal
			// that could become an arrow is one that must not be written as
			// text. `guard.phrase` refuses to write `>` at all for the same
			// reason, and this is the same reasoning applied where a literal
			// rather than a phrase carries the character.
			name:  "a literal that could grow a transition arrow is bytes",
			value: "-->",
			text:  true,
			want:  "0x2D 0x2D 0x3E",
		},
		{
			// A Mermaid label's text is markup — it is how `<br/>` works in one
			// — so these would render as markup rather than as the value
			// somebody is checking.
			name:  "a literal that could become markup is bytes",
			value: "a&b",
			text:  true,
			want:  "0x61 0x26 0x62",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := literalBytes([]byte(testCase.value), testCase.text); got != testCase.want {
				t.Errorf("the literal %q is drawn %s, want %s", testCase.value, got, testCase.want)
			}
		})
	}
}

// TestATargetInASCIIIsTheOnlyLiteralDrawnAsText holds the same rule where it is
// decided rather than where it is applied: on the target field's encoding,
// which is the only charset any of this is in reach of.
func TestATargetInASCIIIsTheOnlyLiteralDrawnAsText(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		charset irpb.Charset
		want    string
	}{
		{name: "ascii", charset: irpb.Charset_CHARSET_ASCII, want: `when ENTRY.SUB.KIND = "H"`},
		{name: "cp037", charset: irpb.Charset_CHARSET_CP037, want: "when ENTRY.SUB.KIND = 0x48"},
		{name: "unset", charset: irpb.Charset_CHARSET_UNSPECIFIED, want: "when ENTRY.SUB.KIND = 0x48"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			g := drawn(t, oneRecordAutomaton(
				encodedFieldNode(106, "KIND", 1, testCase.charset),
				equalPredicate(50, 106, "H"),
				edgeNode(30, 100, 2, predicateAt(50), nil, nil),
			))

			if got := g.states[0].edges[0].label(mermaidLabel); !strings.Contains(got, testCase.want) {
				t.Errorf("the edge reads %q, and does not carry %q", got, testCase.want)
			}
		})
	}
}

// TestAGuardsLiteralIsAlwaysBytes is the half of the rule with no charset in
// reach at all.
//
// A guard reads a register, a bytes register holds its source field's bytes as
// they appear in the record, and a register node declares its kind and nothing
// more — so there is no encoding to consult, not even the one a predicate's
// target carries. Bytes is the only honest rendering left, and it is what this
// asserts so that a later convenience cannot quietly make it a guess.
func TestAGuardsLiteralIsAlwaysBytes(t *testing.T) {
	t.Parallel()

	g := drawn(t, oneRecordAutomaton(
		bytesRegister(21), equalsBytesGuard(60, 21, "Y "),
		edgeNode(30, 100, 2, nil, []uint64{60}, nil),
	))

	if want := "if r21 = 0x59 0x20"; !strings.Contains(g.states[0].edges[0].label(mermaidLabel), want) {
		t.Errorf("the edge reads %q, and does not carry %q", g.states[0].edges[0].label(mermaidLabel), want)
	}
}

// TestEveryGuardTestIsDrawnOverEitherRegisterKind is the closed set of three,
// and the two kinds of value they read.
//
// A guard is what makes a transition eligible at all, and the three tests say
// different things about the same register: a document that drew two of them
// alike would make a reader believe a count was checked for a value when it was
// checked for being positive. A literal's kind mirrors the register's, which is
// why `0` and `0x00` may not print the same way either.
func TestEveryGuardTestIsDrawnOverEitherRegisterKind(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		guard *irpb.Node
		want  string
	}{
		{name: "equals, over an integer", guard: equalsIntegerGuard(60, 20, 0), want: "r20 = 0"},
		{name: "equals, over bytes", guard: equalsBytesGuard(60, 21, "\xe8"), want: "r21 = 0xE8"},
		{
			name:  "one of, over bytes",
			guard: oneOfBytesGuard(60, 21, "\xd5", "\x40"),
			want:  "r21 is one of 0xD5 or 0x40",
		},
		{
			// The counted descriptor carries no such guard, and the schema
			// admits one: a literal set mirrors the register's kind like a
			// single literal does.
			name:  "one of, over an integer",
			guard: oneOfIntegerGuard(60, 20, 1, 2),
			want:  "r20 is one of 1 or 2",
		},
		{name: "greater than zero", guard: positiveGuard(60, 20), want: "r20 greater than zero"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			g := drawn(t, oneRecordAutomaton(
				integerRegister(20), bytesRegister(21), testCase.guard,
				edgeNode(30, 100, 2, nil, []uint64{60}, nil),
			))

			if got := g.states[0].edges[0].label(mermaidLabel); !strings.Contains(got, "if "+testCase.want) {
				t.Errorf("the edge reads %q, and does not carry the guard %q", got, testCase.want)
			}
		})
	}
}

// TestAGuardsLiteralMustBeTheKindItsRegisterHolds is the check the schema
// splits Literal into two members to make possible.
//
// Both kinds draw as a sentence this document knows how to write — `r20 = 0`
// and `r21 = 0x00` are each well formed on their face — so a literal of the
// wrong kind is not a rendering that fails, it is one that says something the
// descriptor does not. docs/ir/SPEC.md words it as a producer MUST, and a
// consumer that does not check is a consumer the two members bought nothing.
func TestAGuardsLiteralMustBeTheKindItsRegisterHolds(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		nodes []*irpb.Node
		says  string
	}{
		{
			name:  "an integer literal against a bytes register",
			nodes: []*irpb.Node{bytesRegister(21), equalsIntegerGuard(60, 21, 5)},
			says:  "guard 60 compares a register that holds bytes against an integer",
		},
		{
			name:  "a bytes literal against an integer register",
			nodes: []*irpb.Node{integerRegister(20), equalsBytesGuard(60, 20, "\x00")},
			says:  "guard 60 compares a register that holds an integer against bytes",
		},
		{
			// The set is checked literal by literal, so one of the wrong kind
			// among several of the right kind is still refused.
			name:  "one literal of the wrong kind in a set",
			nodes: []*irpb.Node{bytesRegister(21), mixedOneOfGuard(60, 21)},
			says:  "guard 60 compares a register that holds bytes against an integer",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := read(oneRecordAutomaton(append(testCase.nodes,
				edgeNode(30, 100, 2, nil, []uint64{60}, nil))...))

			if err == nil {
				t.Fatal("read accepted a guard whose literal is not the kind its register holds")
			}

			if !strings.Contains(err.Error(), testCase.says) {
				t.Errorf("the refusal reads %q, and does not say %q", err, testCase.says)
			}
		})
	}
}

// TestTheGuardsOfOneTransitionAreDrawnAsAConjunction is what the list means.
//
// docs/ir/SPEC.md has all of a transition's guards hold for it to be eligible,
// so two of them are an `and` and never a choice. A document that separated
// them with a comma would leave a reader to guess which, and the guess that
// makes a diagram wrong — reading them as alternatives — is the one a comma
// invites.
func TestTheGuardsOfOneTransitionAreDrawnAsAConjunction(t *testing.T) {
	t.Parallel()

	g := drawn(t, oneRecordAutomaton(
		integerRegister(20), bytesRegister(21),
		equalsIntegerGuard(60, 20, 0), equalsBytesGuard(61, 21, "\xe8"),
		edgeNode(30, 100, 2, nil, []uint64{60, 61}, nil),
	))

	if want := "if r20 = 0 and r21 = 0xE8"; !strings.Contains(g.states[0].edges[0].label(mermaidLabel), want) {
		t.Errorf("the edge reads %q, and its two guards are not drawn as %q",
			g.states[0].edges[0].label(mermaidLabel), want)
	}
}

// TestAcceptanceGuardsAreDrawnOnTheAcceptingState is the criterion an accepting
// state cannot be drawn without.
//
// Guarded acceptance is what makes the last iteration of a count detectable — a
// file two details short is an end of input where the counter is not zero — and
// an accepting state drawn as though it accepted unconditionally would tell a
// reader the opposite of the truth about the file they are checking.
func TestAcceptanceGuardsAreDrawnOnTheAcceptingState(t *testing.T) {
	t.Parallel()

	written := writtenDocument(t, countedAutomaton())

	if want := "s3 --> [*]: if r20 = 0 and r21 is one of 0xD5 or 0x40"; !strings.Contains(written, want) {
		t.Errorf("the document does not draw the guarded acceptance %q:\n%s", want, written)
	}

	// And an unconditional one stays unconditional: state 4 accepts with no
	// guard at all, and a label there would be a condition the descriptor does
	// not carry.
	if want := "    s4 --> [*]\n"; !strings.Contains(written, want) {
		t.Errorf("the document does not draw the unguarded acceptance %q:\n%s", want, written)
	}
}

// TestAStateThatDoesNotAcceptMayNotCarryAcceptanceGuards is the other side of
// the same field.
//
// docs/ir/SPEC.md carries acceptance guards "empty where acceptance is
// unconditional — which, on a state that does not accept at all, it is". So a
// state carrying them without accepting is a producer that meant one of the two
// and wrote the other, and there is nowhere honest to draw them: the state has
// no acceptance for them to qualify.
func TestAStateThatDoesNotAcceptMayNotCarryAcceptanceGuards(t *testing.T) {
	t.Parallel()

	_, err := read(&irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			unframedFile(1, 2),
			guardedStateNode(2, false, []uint64{60}),
			integerRegister(20),
			positiveGuard(60, 20),
		},
	})

	if err == nil {
		t.Fatal("read accepted a state that does not accept and carries acceptance guards")
	}

	if !strings.Contains(err.Error(), "does not accept and carries 1 acceptance guards") {
		t.Errorf("the refusal reads %q, and does not say what the state carries", err)
	}
}

// TestBindingsAreDrawnOnTheEdgeThatAppliesThem covers both members of the
// closed set.
//
// A binding is the only thing that writes a register, so an edge that applied
// one silently would leave the register table's third column unexplained. The
// decrement is drawn as the subtraction it is because a Decrement node carries
// nothing at all — not even which register — and a reader shown only its name
// would have to already know what it does.
func TestBindingsAreDrawnOnTheEdgeThatAppliesThem(t *testing.T) {
	t.Parallel()

	g := drawn(t, oneRecordAutomaton(
		integerRegister(20), bytesRegister(21),
		fieldBinding(70, 20, 102), decrementBinding(71, 21),
		edgeNode(30, 100, 2, nil, nil, []uint64{70, 71}),
	))

	if want := "then r20 = DTL-COUNT and r21 = r21 - 1"; !strings.Contains(g.states[0].edges[0].label(mermaidLabel), want) {
		t.Errorf("the edge reads %q, and does not carry %q",
			g.states[0].edges[0].label(mermaidLabel), want)
	}
}

// TestTheRegisterTableNamesEveryRegisterAndWhatBindsIt is the table's whole
// reason for being.
//
// A register has no name, so an `r20` in an edge label is an identifier and
// nothing else until something says what it holds and where its value comes
// from. The table is that something, and it lists every register node — a
// register nothing binds included, since docs/ir/SPEC.md makes reading one
// malformed and a table that dropped it would hide the bug rather than show it.
func TestTheRegisterTableNamesEveryRegisterAndWhatBindsIt(t *testing.T) {
	t.Parallel()

	written := writtenDocument(t, countedAutomaton())

	for _, want := range []string{
		mermaidRegisters,
		"| Register | Holds | Bound by |",
		"| r20 | an integer | `s2 --> s3` (HEADER-RECORD), `s3 --> s3` (DETAIL-RECORD), `s3 --> s3` (HEADER-RECORD), `s4 --> s3` (HEADER-RECORD) |",
		"| r21 | bytes | `s2 --> s3` (HEADER-RECORD), `s3 --> s3` (HEADER-RECORD), `s4 --> s3` (HEADER-RECORD) |",
		"| r22 | an integer | `s2 --> s3` (HEADER-RECORD), `s3 --> s3` (HEADER-RECORD), `s4 --> s3` (HEADER-RECORD) |",
	} {
		if !strings.Contains(written, want) {
			t.Errorf("the register table does not carry %q:\n%s", want, written)
		}
	}
}

// TestARegisterNoTransitionBindsIsDrawnAndSaidSo is the case the table exists
// to make visible rather than to tidy away.
//
// docs/ir/SPEC.md's "A register is read only where it has been written" makes a
// read of a register nothing has bound a malformed descriptor. This generator
// does not prove that — it would have to walk every path to every reader — so
// it draws what it can see and says what that means, which is the same posture
// it takes towards a state nothing reaches.
func TestARegisterNoTransitionBindsIsDrawnAndSaidSo(t *testing.T) {
	t.Parallel()

	written := writtenDocument(t, oneRecordAutomaton(
		integerRegister(20), positiveGuard(60, 20),
		edgeNode(30, 100, 2, nil, []uint64{60}, nil),
	))

	for _, want := range []string{"| r20 | an integer | nothing |", "Register r20 is written by no transition"} {
		if !strings.Contains(written, want) {
			t.Errorf("the document does not carry %q:\n%s", want, written)
		}
	}
}

// TestARecordNameInTheRegisterTableIsEscapedAsMarkdown is the table's half of
// the rule the diagram has for its labels.
//
// A cell is two contexts at once: table syntax, where `|` ends the cell, and
// ordinary Markdown inline text, where a backtick opens a code span that
// swallows the rest of the row and `<` opens raw HTML. The record name sits
// outside the backticks quoting the edge beside it, so it is in that inline
// context with nothing around it to protect it — and a record name is the
// copybook's and may hold anything.
func TestARecordNameInTheRegisterTableIsEscapedAsMarkdown(t *testing.T) {
	t.Parallel()

	written := writtenDocument(t, &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			unframedFile(1, 2),
			stateNode(2, true, 30),
			integerRegister(20), fieldBinding(70, 20, 101),
			edgeNode(30, 100, 2, nil, nil, []uint64{70}),
			recordOf(100, 105, "A`B|C<D*E_F"),
			groupNode(105, "ROOT", 101),
			fieldNode(101, "DTL-COUNT", 2),
		},
	})

	if want := "(A\\`B\\|C\\<D\\*E\\_F)"; !strings.Contains(written, want) {
		t.Errorf("the register table does not carry the escaped name %q:\n%s", want, written)
	}

	// The row is still one row: an unescaped `|` would have made it three
	// columns of something else.
	for _, line := range strings.Split(written, "\n") {
		if !strings.HasPrefix(line, "| r20 ") {
			continue
		}

		if got := strings.Count(line, "|") - strings.Count(line, `\|`); got != 4 {
			t.Errorf("the register row is %q, and it holds %d unescaped cell separators rather than 4", line, got)
		}
	}
}

// TestADescriptorCarryingNoRegisterHasNoRegisterSection keeps the section off
// the document most layouts produce.
//
// Sequencing that needs no memory carries no register node, and a heading over
// a table with no rows would be a section about nothing on every one of those
// documents.
func TestADescriptorCarryingNoRegisterHasNoRegisterSection(t *testing.T) {
	t.Parallel()

	written := writtenDocument(t, ordersAutomaton(unframed4()))

	if strings.Contains(written, mermaidRegisters) {
		t.Errorf("a descriptor carrying no register produced a register section:\n%s", written)
	}
}

// TestAReferenceInThisHalfOfTheAutomatonThatDoesNotResolveIsRefused is the
// posture of the rest of this generator, applied to the references this story
// added.
//
// Each of these is a bug in whatever produced the descriptor and each names the
// identifier it could not resolve, because a blank cell in a diagram somebody is
// about to trust is worse than no diagram: it reads as a transition that tests
// nothing, or a register that holds nothing, rather than as a descriptor that
// does not say what a descriptor says.
func TestAReferenceInThisHalfOfTheAutomatonThatDoesNotResolveIsRefused(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		nodes []*irpb.Node
		says  string
	}{
		{
			name:  "a predicate that is not there",
			nodes: []*irpb.Node{edgeNode(30, 100, 2, predicateAt(50), nil, nil)},
			says:  "the predicate selecting a transition names node 50",
		},
		{
			name: "a predicate that is not a predicate",
			nodes: []*irpb.Node{
				fieldNode(50, "NOT-A-PREDICATE", 1),
				edgeNode(30, 100, 2, predicateAt(50), nil, nil),
			},
			says: "the predicate selecting a transition names node 50",
		},
		{
			name: "a predicate carrying no test",
			nodes: []*irpb.Node{
				{Id: 50, Kind: &irpb.Node_Predicate{Predicate: &irpb.Predicate{FieldId: 101}}},
				edgeNode(30, 100, 2, predicateAt(50), nil, nil),
			},
			says: "predicate 50 carries no test",
		},
		{
			name:  "a one-of predicate carrying one literal",
			nodes: []*irpb.Node{oneOfPredicate(50, 101, "\xc8"), edgeNode(30, 100, 2, predicateAt(50), nil, nil)},
			says:  "predicate 50 tests membership of a set of 1 literals",
		},
		{
			// The other half of the same rule. Left unchecked it draws as
			// `is one of 0xC8 or 0xC8`, which is a test no reader can act on.
			name:  "a one-of predicate carrying the same literal twice",
			nodes: []*irpb.Node{oneOfPredicate(50, 101, "\xc8", "\xc8"), edgeNode(30, 100, 2, predicateAt(50), nil, nil)},
			says:  "predicate 50 tests membership of a set carrying the same literal twice",
		},
		{
			// Not "the field is not in the record", which would name the
			// predicate's target and blame the containment rule for a bug in
			// the record's own member list.
			name: "a record whose member list names a node that is not there",
			nodes: []*irpb.Node{
				equalPredicate(50, 900, "\xc8"), fieldNode(900, "ELSEWHERE", 1),
				recordOf(200, 205, "GAPPY-RECORD"), groupNode(205, "GAPPY-RECORD", 206),
				edgeNode(30, 200, 2, predicateAt(50), nil, nil),
			},
			says: "a member of a record a transition admits names node 206",
		},
		{
			name: "an arm of a variant whose body is not there",
			nodes: []*irpb.Node{
				equalPredicate(50, 900, "\xc8"), fieldNode(900, "ELSEWHERE", 1),
				recordOf(200, 205, "VARIED-RECORD"), groupNode(205, "VARIED-RECORD", 206),
				{Id: 206, Kind: &irpb.Node_Variant{Variant: &irpb.Variant{
					Arms: []*irpb.Arm{{PredicateId: 50, Body: &irpb.Arm_GroupId{GroupId: 207}}},
				}}},
				edgeNode(30, 200, 2, predicateAt(50), nil, nil),
			},
			says: "the body of an arm of a variant in a record a transition admits names node 207",
		},
		{
			name:  "a predicate whose target is not a field",
			nodes: []*irpb.Node{equalPredicate(50, 105, "\xc8"), edgeNode(30, 100, 2, predicateAt(50), nil, nil)},
			says:  "the field predicate 50 tests names node 105",
		},
		{
			name:  "a predicate whose target is not in the record the transition admits",
			nodes: []*irpb.Node{equalPredicate(50, 900, "\xc8"), fieldNode(900, "ELSEWHERE", 1), edgeNode(30, 100, 2, predicateAt(50), nil, nil)},
			says:  "that field is not in the record the transition admits",
		},
		{
			name:  "a guard that is not there",
			nodes: []*irpb.Node{edgeNode(30, 100, 2, nil, []uint64{60}, nil)},
			says:  "a guard of transition 30 names node 60",
		},
		{
			name: "a guard reading a register that is not a register",
			nodes: []*irpb.Node{
				positiveGuard(60, 101),
				edgeNode(30, 100, 2, nil, []uint64{60}, nil),
			},
			says: "the register guard 60 reads names node 101",
		},
		{
			name: "a guard carrying no test",
			nodes: []*irpb.Node{
				integerRegister(20),
				{Id: 60, Kind: &irpb.Node_Guard{Guard: &irpb.Guard{RegisterId: 20}}},
				edgeNode(30, 100, 2, nil, []uint64{60}, nil),
			},
			says: "guard 60 carries no test",
		},
		{
			name: "a guard testing membership of nothing",
			nodes: []*irpb.Node{
				bytesRegister(21), oneOfBytesGuard(60, 21),
				edgeNode(30, 100, 2, nil, []uint64{60}, nil),
			},
			says: "guard 60 tests membership of an empty set of literals",
		},
		{
			name: "a guard comparing a register against a literal with no value",
			nodes: []*irpb.Node{
				integerRegister(20), emptyLiteralGuard(60, 20),
				edgeNode(30, 100, 2, nil, []uint64{60}, nil),
			},
			says: "guard 60 compares a register against a literal that carries no value",
		},
		{
			name:  "a binding that is not there",
			nodes: []*irpb.Node{edgeNode(30, 100, 2, nil, nil, []uint64{70})},
			says:  "a transition's binding names node 70",
		},
		{
			name: "a binding writing a register that is not a register",
			nodes: []*irpb.Node{
				fieldBinding(70, 101, 102),
				edgeNode(30, 100, 2, nil, nil, []uint64{70}),
			},
			says: "the register binding 70 writes names node 101",
		},
		{
			name: "a binding that says nothing about what it writes",
			nodes: []*irpb.Node{
				integerRegister(20),
				{Id: 70, Kind: &irpb.Node_Binding{Binding: &irpb.Binding{RegisterId: 20}}},
				edgeNode(30, 100, 2, nil, nil, []uint64{70}),
			},
			says: "binding 70 writes a register and says nothing about what it writes",
		},
		{
			name: "a binding reading a field that is not in the record the transition admits",
			nodes: []*irpb.Node{
				integerRegister(20), fieldBinding(70, 20, 900), fieldNode(900, "ELSEWHERE", 1),
				edgeNode(30, 100, 2, nil, nil, []uint64{70}),
			},
			says: "that field is not in the record the transition admits",
		},
		{
			name: "a register that says nothing about what it holds",
			nodes: []*irpb.Node{
				{Id: 20, Kind: &irpb.Node_Register{Register: &irpb.Register{}}},
				edgeNode(30, 100, 2, nil, nil, nil),
			},
			says: "register node 20 says nothing about what it holds",
		},
		{
			// A second record, since a descriptor's first node with an
			// identifier is the one that wins and the fixture's own record is
			// well formed.
			name: "a record whose top level is not a group",
			nodes: []*irpb.Node{
				equalPredicate(50, 101, "\xc8"),
				recordOf(200, 101, "ODD-RECORD"),
				edgeNode(30, 200, 2, predicateAt(50), nil, nil),
			},
			says: "the top level of a record a transition admits names node 101",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := read(oneRecordAutomaton(testCase.nodes...))
			if err == nil {
				t.Fatal("read accepted a descriptor that does not say what a descriptor says")
			}

			if !strings.Contains(err.Error(), testCase.says) {
				t.Errorf("the refusal reads %q, and does not say %q", err, testCase.says)
			}

			var carrier noted
			if !errors.As(err, &carrier) || len(carrier.Notes()) == 0 {
				t.Errorf("the refusal %q carries no note naming the rule it is about", err)
			}
		})
	}
}

// TestAMemberListThatContainsItselfIsAWalkThatStops is the one shape the path
// walk could have hung on.
//
// A member list states containment downward, so a descriptor cannot legally
// carry a group beneath itself — and a diagram generator is not the place to
// find out that a producer emitted one by running until the stack is gone. The
// field is refused as one the record does not carry, which is what it is.
func TestAMemberListThatContainsItselfIsAWalkThatStops(t *testing.T) {
	t.Parallel()

	_, err := read(&irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			unframedFile(1, 2),
			stateNode(2, true, 30),
			edgeNode(30, 100, 2, predicateAt(50), nil, nil),
			equalPredicate(50, 900, "\xc8"),
			fieldNode(900, "ELSEWHERE", 1),
			recordOf(100, 105, "LOOP-RECORD"),
			groupNode(105, "LOOP-RECORD", 106),
			groupNode(106, "INNER", 105),
		},
	})

	if err == nil {
		t.Fatal("read accepted a predicate naming a field the record does not carry")
	}

	if !strings.Contains(err.Error(), "that field is not in the record the transition admits") {
		t.Errorf("the refusal reads %q, and does not say the field is not in the record", err)
	}
}

// TestAFieldPathIsEscapedByTheEmitterAndNotByTheModel is the split that lets
// one wording serve two notations.
//
// The model composes the sentence and the emitter escapes the names in it, so a
// copybook name carrying a metacharacter cannot reach a diagram unescaped —
// which is the property [mermaidLabel] exists for, applied to the names this
// story put on an edge rather than only to the record's.
func TestAFieldPathIsEscapedByTheEmitterAndNotByTheModel(t *testing.T) {
	t.Parallel()

	g := drawn(t, oneRecordAutomaton(
		equalPredicate(50, 106, "\xc8"),
		fieldNode(106, "KIND:CODE", 1),
		edgeNode(30, 100, 2, predicateAt(50), nil, nil),
	))

	if want := "when ENTRY.SUB.KIND#58;CODE = 0xC8"; !strings.Contains(g.states[0].edges[0].label(mermaidLabel), want) {
		t.Errorf("the edge reads %q, and does not carry the escaped path %q",
			g.states[0].edges[0].label(mermaidLabel), want)
	}

	// The model itself carries the name as the copybook spells it, since the
	// other notation escapes differently.
	if got := g.states[0].edges[0].predicate.field; got != "ENTRY.SUB.KIND:CODE" {
		t.Errorf("the model carries the path %q, and the copybook spells it ENTRY.SUB.KIND:CODE", got)
	}
}

// countedAutomaton is docs/ir/SPEC.md's "Appendix: A counted run, as nodes", as
// a descriptor: a header binding a detail count and a flag, a run of details the
// count governs, a summary the flag governs, and any number of such groups in
// one file.
//
// It is the descriptor `cmd/cpybkc-gen-go/internal/counted` is generated from,
// node for node, and it is the golden for this story because it is the one
// automaton that exercises every part of it at once — a field binding and a
// decrement, all three guard tests, guarded acceptance, a transition carrying no
// predicate, and three registers with different sets of transitions writing
// them.
func countedAutomaton() *irpb.Descriptor {
	return &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			fileNode(1, &irpb.File{
				Framing: &irpb.File_Delimited{Delimited: &irpb.Delimited{
					Delimiter: []byte{0x15},
					Placement: irpb.DelimiterPlacement_DELIMITER_PLACEMENT_TERMINATOR,
				}},
				StartStateId: 2,
			}),

			// start — does not accept.
			stateNode(2, false, 10),

			// group — accepts, guarded by the count being zero and the flag
			// being one of N or a space.
			guardedStateNode(3, true, []uint64{30, 31}, 11, 12, 13),

			// summarised — accepts, unconditionally.
			stateNode(4, true, 14, 15),

			edgeNode(10, 100, 3, predicateAt(50), nil, []uint64{40, 41, 42}),
			edgeNode(11, 110, 3, predicateAt(51), []uint64{32}, []uint64{43}),
			edgeNode(12, 120, 4, predicateAt(52), []uint64{30, 33}, nil),
			edgeNode(13, 100, 3, predicateAt(50), []uint64{30, 31}, []uint64{40, 41, 42}),

			// A transition carrying no predicate, standing in front of one that
			// carries one and excluded by a guard that never holds.
			edgeNode(14, 110, 3, nil, []uint64{34}, nil),
			edgeNode(15, 100, 3, predicateAt(50), nil, []uint64{40, 41, 42}),

			integerRegister(20),
			bytesRegister(21),
			integerRegister(22),

			equalsIntegerGuard(30, 20, 0),
			oneOfBytesGuard(31, 21, "\xd5", "\x40"),
			positiveGuard(32, 20),
			equalsBytesGuard(33, 21, "\xe8"),
			equalsBytesGuard(34, 21, "\xe9"),

			fieldBinding(40, 20, 102),
			fieldBinding(41, 21, 103),
			fieldBinding(42, 22, 104),
			decrementBinding(43, 20),

			equalPredicate(50, 101, "\xc8"),
			equalPredicate(51, 111, "\xc4"),
			equalPredicate(52, 121, "\xe2"),

			recordOf(100, 105, "HEADER-RECORD"),
			groupNode(105, "HEADER-RECORD", 101, 102, 103, 104),
			fieldNode(101, "TYPE-CODE", 1),
			fieldNode(102, "DTL-COUNT", 2),
			fieldNode(103, "SUM-FLAG", 1),
			fieldNode(104, "TOTAL-COUNT", 2),

			recordOf(110, 115, "DETAIL-RECORD"),
			groupNode(115, "DETAIL-RECORD", 111, 112),
			fieldNode(111, "TYPE-CODE", 1),
			fieldNode(112, "AMOUNT", 3),

			recordOf(120, 125, "SUMMARY-RECORD"),
			groupNode(125, "SUMMARY-RECORD", 121, 122, 124),
			fieldNode(121, "TYPE-CODE", 1),
			groupNode(122, "LINE", 123),
			fieldNode(123, "LINE-TEXT", 3),
			groupNode(124, "NOTE", 126),
			fieldNode(126, "NOTE-TEXT", 2),
		},
	}
}

// oneRecordAutomaton is one accepting state admitting one record, with the
// nodes a test cares about carried beside it.
//
// The record is nested rather than flat — `HEADER-RECORD` holding `ENTRY`
// holding `SUB` holding `KIND` — so that a path within a record is something
// these fixtures can be wrong about. The transition is the caller's, because
// what a test of this half of the automaton varies is what hangs off the
// transition.
//
// The caller's nodes come first, so a test that carries a node with one of
// these identifiers replaces it: a descriptor's first node with an identifier
// is the one that wins, and that is what lets a test say "the same record, but
// this field is in ASCII" without restating the record. It also means the
// fixture below is well formed on its own — every member list resolves — which
// is what the walk over it is entitled to assume when a test is about something
// else entirely.
func oneRecordAutomaton(nodes ...*irpb.Node) *irpb.Descriptor {
	carried := []*irpb.Node{
		unframedFile(1, 2),
		stateNode(2, true, 30),

		recordOf(100, 105, "HEADER-RECORD"),
		groupNode(105, "HEADER-RECORD", 101, 102, 103),
		fieldNode(101, "TYPE-CODE", 1),
		fieldNode(102, "DTL-COUNT", 2),
		groupNode(103, "ENTRY", 104),
		groupNode(104, "SUB", 106),
		fieldNode(106, "KIND", 1),
	}

	return &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes:   append(append([]*irpb.Node{}, nodes...), carried...),
	}
}

// guardedStateNode is a state whose acceptance the guards named qualify.
func guardedStateNode(id uint64, accepts bool, acceptance []uint64, transitions ...uint64) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_State{State: &irpb.State{
		Accepts:            accepts,
		AcceptanceGuardIds: acceptance,
		TransitionIds:      transitions,
	}}}
}

// edgeNode is a transition carrying everything a transition may carry.
func edgeNode(id, admits, to uint64, predicate *uint64, guards, bindings []uint64) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Transition{Transition: &irpb.Transition{
		RecordId:    admits,
		NextStateId: to,
		PredicateId: predicate,
		GuardIds:    guards,
		BindingIds:  bindings,
	}}}
}

// predicateAt is a transition's predicate reference, which is the one reference
// in the schema absence is a meaning for — so it is a pointer and never a
// sentinel, because zero is an ordinary identifier.
func predicateAt(id uint64) *uint64 { return &id }

// equalPredicate and oneOfPredicate are the two members of the predicate set.
func equalPredicate(id, field uint64, value string) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Predicate{Predicate: &irpb.Predicate{
		FieldId: field,
		Test:    &irpb.Predicate_BytesEqual{BytesEqual: &irpb.BytesEqual{Value: []byte(value)}},
	}}}
}

func oneOfPredicate(id, field uint64, values ...string) *irpb.Node {
	set := &irpb.BytesOneOf{}
	for _, value := range values {
		set.Values = append(set.Values, []byte(value))
	}

	return &irpb.Node{Id: id, Kind: &irpb.Node_Predicate{Predicate: &irpb.Predicate{
		FieldId: field,
		Test:    &irpb.Predicate_BytesOneOf{BytesOneOf: set},
	}}}
}

// integerRegister and bytesRegister are the two kinds a register may hold.
func integerRegister(id uint64) *irpb.Node {
	return registerNode(id, irpb.RegisterKind_REGISTER_KIND_INTEGER)
}

func bytesRegister(id uint64) *irpb.Node {
	return registerNode(id, irpb.RegisterKind_REGISTER_KIND_BYTES)
}

func registerNode(id uint64, kind irpb.RegisterKind) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Register{Register: &irpb.Register{Kind: kind}}}
}

// The three guard tests and no fourth, over either kind of register.
func equalsIntegerGuard(id, reg uint64, value int64) *irpb.Node {
	return guardNode(id, reg, func(g *irpb.Guard) {
		g.Test = &irpb.Guard_Equals{Equals: &irpb.Literal{Value: &irpb.Literal_Integer{Integer: value}}}
	})
}

func equalsBytesGuard(id, reg uint64, value string) *irpb.Node {
	return guardNode(id, reg, func(g *irpb.Guard) {
		g.Test = &irpb.Guard_Equals{Equals: &irpb.Literal{
			Value: &irpb.Literal_BytesValue{BytesValue: []byte(value)},
		}}
	})
}

func oneOfBytesGuard(id, reg uint64, values ...string) *irpb.Node {
	set := &irpb.LiteralSet{}
	for _, value := range values {
		set.Values = append(set.Values, &irpb.Literal{
			Value: &irpb.Literal_BytesValue{BytesValue: []byte(value)},
		})
	}

	return guardNode(id, reg, func(g *irpb.Guard) { g.Test = &irpb.Guard_OneOf{OneOf: set} })
}

func oneOfIntegerGuard(id, reg uint64, values ...int64) *irpb.Node {
	set := &irpb.LiteralSet{}
	for _, value := range values {
		set.Values = append(set.Values, &irpb.Literal{Value: &irpb.Literal_Integer{Integer: value}})
	}

	return guardNode(id, reg, func(g *irpb.Guard) { g.Test = &irpb.Guard_OneOf{OneOf: set} })
}

func positiveGuard(id, reg uint64) *irpb.Node {
	return guardNode(id, reg, func(g *irpb.Guard) {
		g.Test = &irpb.Guard_GreaterThanZero{GreaterThanZero: &irpb.GreaterThanZero{}}
	})
}

// mixedOneOfGuard tests membership of a set carrying one literal of each kind,
// which is a set at most one member of which matches its register.
func mixedOneOfGuard(id, reg uint64) *irpb.Node {
	return guardNode(id, reg, func(g *irpb.Guard) {
		g.Test = &irpb.Guard_OneOf{OneOf: &irpb.LiteralSet{Values: []*irpb.Literal{
			{Value: &irpb.Literal_BytesValue{BytesValue: []byte("\xd5")}},
			{Value: &irpb.Literal_Integer{Integer: 3}},
		}}}
	})
}

// emptyLiteralGuard compares a register against a literal carrying neither
// member, which is a producer that wrote the comparison and not the value.
func emptyLiteralGuard(id, reg uint64) *irpb.Node {
	return guardNode(id, reg, func(g *irpb.Guard) {
		g.Test = &irpb.Guard_Equals{Equals: &irpb.Literal{}}
	})
}

// guardNode is a guard node carrying one of the three tests.
//
// The test is applied to a built node rather than passed in, because the oneof
// wrapper interface protoc-gen-go declares for it is unexported and a test file
// outside irpb has no name for it.
func guardNode(id, reg uint64, apply func(*irpb.Guard)) *irpb.Node {
	g := &irpb.Guard{RegisterId: reg}

	apply(g)

	return &irpb.Node{Id: id, Kind: &irpb.Node_Guard{Guard: g}}
}

// fieldBinding writes a register from a field of the record the transition
// admits, and decrementBinding takes one off it.
func fieldBinding(id, reg, field uint64) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Binding{Binding: &irpb.Binding{
		RegisterId: reg, Value: &irpb.Binding_FieldId{FieldId: field},
	}}}
}

func decrementBinding(id, reg uint64) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Binding{Binding: &irpb.Binding{
		RegisterId: reg, Value: &irpb.Binding_Decrement{Decrement: &irpb.Decrement{}},
	}}}
}

// fieldNode is an elementary item, with the name a path is built out of.
//
// It carries no encoding, which is a charset this generator cannot name and so
// a literal it prints as bytes — the case every fixture here but
// [encodedFieldNode]'s wants.
func fieldNode(id uint64, original string, width uint32) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Field{Field: &irpb.Field{
		Width: width,
		Names: &irpb.Names{Original: original},
	}}}
}

// encodedFieldNode is the same item with a charset on it, which is the one
// thing that decides whether a predicate's literals are drawn as text.
func encodedFieldNode(id uint64, original string, width uint32, charset irpb.Charset) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Field{Field: &irpb.Field{
		Width:    width,
		Names:    &irpb.Names{Original: original},
		Encoding: &irpb.Encoding{Charset: charset},
	}}}
}
