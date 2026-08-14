// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Zaba505/cpybkc/irpb"
)

// TestTheWalkBeginsAtTheStateTheFileNodeNames is the one thing about the order
// of this diagram that is not this generator's choice.
//
// The start state is File.start_state_id and never the lowest-numbered state,
// the first state node in the message, or a state a walk happened to arrive at
// first. A descriptor whose start state is neither of the other two is what
// tells those apart.
func TestTheWalkBeginsAtTheStateTheFileNodeNames(t *testing.T) {
	t.Parallel()

	// State 7 is the last state node in the message and the highest-numbered
	// one, and it is where this file begins.
	g := drawn(t, &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			unframedFile(1, 7),
			stateNode(3, false, 10),
			recordNode(4, "TAIL", ""),
			groupNode(20, "TAIL"),
			transitionNode(10, 4, 3),
			stateNode(7, false, 10),
		},
	})

	if g.start != 7 {
		t.Errorf("the walk begins at state %d, and the file node begins in state 7", g.start)
	}

	if len(g.states) == 0 || g.states[0].id != 7 {
		t.Fatalf("the states are %v, and the one the file node names is drawn first", ids(g.states))
	}
}

// TestIdentifierZeroIsAnOrdinaryIdentifier is docs/ir/SPEC.md's rule that zero
// is not a sentinel, from the side this generator could most easily have broken
// it: the start state is a plain reference, so a walk that read "0" as "unset"
// would refuse a conforming descriptor whose first node happens to be numbered
// from zero.
//
// The behaviour is load bearing and was, until this test, held by nothing: it
// comes out of a map lookup being nil-safe rather than out of anything written
// down, and it is exactly what a later `if id == 0` would silently undo.
func TestIdentifierZeroIsAnOrdinaryIdentifier(t *testing.T) {
	t.Parallel()

	g := drawn(t, &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			stateNode(0, true, 1),
			unframedFile(3, 0),
			recordNode(2, "ONLY", ""),
			groupNode(18, "ONLY"),
			transitionNode(1, 2, 0),
		},
	})

	if g.start != 0 {
		t.Errorf("the walk begins at state %d, and the file node begins in state 0", g.start)
	}

	if got, want := ids(g.states), []uint64{0}; !same(got, want) {
		t.Fatalf("the states drawn are %v, want %v", got, want)
	}

	if len(g.states[0].edges) != 1 || g.states[0].edges[0].record != "ONLY" {
		t.Errorf("state 0 is drawn with %d edges, and it admits ONLY", len(g.states[0].edges))
	}
}

// TestAStartStateOfZeroThatIsNotThereIsStillRefused is the other half of the
// rule above, and the reason the first half is not simply "treat 0 as fine".
//
// A file node naming state 0 where no node carries that identifier is a
// dangling reference like any other. Zero being ordinary means it is resolved
// like any other, not that it is waved through.
func TestAStartStateOfZeroThatIsNotThereIsStillRefused(t *testing.T) {
	t.Parallel()

	_, err := read(&irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes:   []*irpb.Node{unframedFile(1, 0), stateNode(2, true)},
	}, defaults())

	if err == nil {
		t.Fatal("read accepted a file node beginning in a state the descriptor does not carry")
	}

	if !strings.Contains(err.Error(), "begins in node 0") {
		t.Errorf("the refusal reads %q, and does not name the node it could not resolve", err)
	}
}

// TestTwoNodesSharingAnIdentifierAreOneNode holds the first-wins rule this
// generator states for duplicate identifiers.
//
// It is worth a test because the rule used to be applied to the index and not
// to the ordering built beside it, and both ways that showed were silent rather
// than loud: a duplicated unreachable state drawn twice, and — where the
// duplicate was of another kind — a refusal describing a dangling reference
// that had not happened.
func TestTwoNodesSharingAnIdentifierAreOneNode(t *testing.T) {
	t.Parallel()

	g := drawn(t, &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			unframedFile(1, 2),
			stateNode(2, true),

			// Reached by nothing, and carried twice.
			stateNode(9, true),
			stateNode(9, true),
		},
	})

	if got, want := ids(g.states), []uint64{2, 9}; !same(got, want) {
		t.Errorf("the states drawn are %v, want %v — one node per identifier", got, want)
	}
}

// TestEveryStateTheDescriptorCarriesIsDrawn is the criterion that keeps this a
// diagram of the descriptor rather than of the part of it a walk could get to.
//
// A state nothing reaches is a bug in whatever compiled the automaton, and it
// is one nobody sees unless the diagram draws it — so it is drawn, after the
// reachable states and marked as what it is.
func TestEveryStateTheDescriptorCarriesIsDrawn(t *testing.T) {
	t.Parallel()

	g := drawn(t, &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			unframedFile(1, 2),
			stateNode(2, true),

			// Reached by nothing: no transition anywhere names 9, and 8 is
			// reachable only from 9.
			stateNode(8, true),
			stateNode(9, false, 30),
			recordNode(4, "STRAY", ""),
			groupNode(20, "STRAY"),
			transitionNode(30, 4, 8),
		},
	})

	if got, want := ids(g.states), []uint64{2, 8, 9}; !same(got, want) {
		t.Fatalf("the states drawn are %v, want %v — the reachable one, then the rest in ascending order", got, want)
	}

	if got, want := ids(g.unreachable()), []uint64{8, 9}; !same(got, want) {
		t.Errorf("the states marked unreachable are %v, want %v", got, want)
	}

	// Their transitions are drawn too. A state nothing reaches still admits
	// records, and a diagram that drew the state and dropped its edges would
	// understate what the producer got wrong.
	for _, s := range g.states {
		if s.id != 9 {
			continue
		}

		if len(s.edges) != 1 || s.edges[0].record != "STRAY" {
			t.Errorf("the unreachable state 9 is drawn with %d edges, and it admits STRAY", len(s.edges))
		}
	}
}

// TestTransitionsAreDrawnInTheOrderTheStateCarriesThem is docs/ir/SPEC.md's
// evaluation order, which is the order the edges have to appear in: a consumer
// takes the first transition whose predicate matches, so a diagram that sorted
// them would say a different one wins.
//
// The identifiers descend so that the order the state states cannot be confused
// with ascending identifier order, which is what every other list in this
// generator falls back on.
func TestTransitionsAreDrawnInTheOrderTheStateCarriesThem(t *testing.T) {
	t.Parallel()

	g := drawn(t, &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			unframedFile(1, 2),
			stateNode(2, true, 32, 31, 30),
			recordNode(4, "FIRST", ""),
			groupNode(20, "FIRST"),
			recordNode(5, "SECOND", ""),
			groupNode(21, "SECOND"),
			recordNode(6, "THIRD", ""),
			groupNode(22, "THIRD"),
			transitionNode(30, 6, 2),
			transitionNode(31, 5, 2),
			transitionNode(32, 4, 2),
		},
	})

	drawnEdges := make([]string, 0, len(g.states[0].edges))
	for _, e := range g.states[0].edges {
		drawnEdges = append(drawnEdges, e.record)
	}

	if want := "FIRST,SECOND,THIRD"; strings.Join(drawnEdges, ",") != want {
		t.Errorf("the edges are drawn %v, and the state evaluates its transitions %s", drawnEdges, want)
	}
}

// TestAnEdgeIsNamedByTheOverrideWhereTheLayoutGaveOne is docs/ir/SPEC.md's
// "Names" from the only side a diagram can assert it.
//
// The original is carried beside an override rather than in place of it, so
// both are on the record node and something has to choose. The override is the
// name the person reading this diagram wrote in their own layout, and it is
// also the only thing telling apart two record nodes resolved from one
// `01`-level, which carry one original between them.
func TestAnEdgeIsNamedByTheOverrideWhereTheLayoutGaveOne(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		original string
		override string
		want     string
	}{
		{name: "no override", original: "ORDER-RECORD", want: "ORDER-RECORD"},
		{name: "an override", original: "TXN-REC", override: "Payment", want: "Payment"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			g := drawn(t, &irpb.Descriptor{
				Version: supportedIRVersion,
				Nodes: []*irpb.Node{
					unframedFile(1, 2),
					stateNode(2, true, 30),
					recordNode(4, testCase.original, testCase.override),
					groupNode(20, testCase.original),
					transitionNode(30, 4, 2),
				},
			})

			if got := g.states[0].edges[0].record; got != testCase.want {
				t.Errorf("the edge is named %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestTheDocumentStatesTheFramingTheFileNodeCarries covers each of the four
// members, since what stands between two records is part of what somebody
// reading this diagram is verifying and appears nowhere in a state machine.
//
// The assertions are over the sentence's facts rather than its wording: the
// delimiter's bytes as bytes, the placement's name, the largest segment's
// number. Holding the whole sentence is the goldens' job.
func TestTheDocumentStatesTheFramingTheFileNodeCarries(t *testing.T) {
	t.Parallel()

	for _, testCase := range framings() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			written := writtenDocument(t, ordersAutomaton(testCase.file))

			for _, want := range testCase.states {
				if !strings.Contains(written, want) {
					t.Errorf("the document does not state %q:\n%s", want, written)
				}
			}
		})
	}
}

// TestEveryPlacementIsAPhraseOfItsOwn keeps the three apart. They differ only
// in what stands at the end of the file, which is exactly the thing a person
// checks a delimited layout for, and two of them rendered as one sentence would
// be a document that agreed with a descriptor it was not describing.
func TestEveryPlacementIsAPhraseOfItsOwn(t *testing.T) {
	t.Parallel()

	said := map[string]irpb.DelimiterPlacement{}

	for _, placement := range []irpb.DelimiterPlacement{
		irpb.DelimiterPlacement_DELIMITER_PLACEMENT_TERMINATOR,
		irpb.DelimiterPlacement_DELIMITER_PLACEMENT_SEPARATOR,
		irpb.DelimiterPlacement_DELIMITER_PLACEMENT_OPTIONAL_TERMINATOR,
	} {
		phrase := placementOf(placement)

		if already, seen := said[phrase]; seen {
			t.Errorf("%v and %v are both %q", already, placement, phrase)
		}

		said[phrase] = placement
	}
}

// TestADescriptorWhoseAutomatonAdmitsNoRecordSaysSo is the empty case, and the
// point is that it is not an empty file.
//
// An automaton with no transition in it draws as a start state and nothing
// else, which is indistinguishable from a generator that gave up halfway. The
// sentence is what makes it a document about a layout rather than a document
// about a failure.
func TestADescriptorWhoseAutomatonAdmitsNoRecordSaysSo(t *testing.T) {
	t.Parallel()

	written := writtenDocument(t, &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes:   []*irpb.Node{unframedFile(1, 2), stateNode(2, true)},
	})

	if !strings.Contains(written, "admits no record") {
		t.Errorf("a descriptor admitting no record produced a document that does not say so:\n%s", written)
	}

	// Still a diagram, and still one naming the start state: what the layout
	// has is a file of no records, and that is a shape to look at rather than
	// nothing to draw.
	for _, want := range []string{mermaidDiagram, "[*] --> s2"} {
		if !strings.Contains(written, want) {
			t.Errorf("the document does not carry %q:\n%s", want, written)
		}
	}
}

// TestADescriptorThatDoesNotSayWhatADescriptorSaysIsRefused is the posture this
// generator takes everywhere its package comment argues for: a picture somebody
// is about to trust is not one to draw out of references that did not resolve.
//
// Each of these is a bug in whatever produced the descriptor, and each is
// reported with a note naming the rule it broke — because the user reading it
// is holding the cpybkc that produced it and has no other way to tell that from
// a bug in their own layout.
func TestADescriptorThatDoesNotSayWhatADescriptorSaysIsRefused(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		nodes []*irpb.Node
		says  string
	}{
		{
			name:  "no file node",
			nodes: []*irpb.Node{stateNode(2, true)},
			says:  "no file node",
		},
		{
			name:  "two file nodes",
			nodes: []*irpb.Node{unframedFile(1, 2), unframedFile(3, 2), stateNode(2, true)},
			says:  "two file nodes",
		},
		{
			name:  "a start state that is not a state",
			nodes: []*irpb.Node{unframedFile(1, 4), recordNode(4, "NOT-A-STATE", "")},
			says:  "begins in node 4",
		},
		{
			name:  "a transition that is not a transition",
			nodes: []*irpb.Node{unframedFile(1, 2), stateNode(2, true, 30)},
			says:  "a transition names node 30",
		},
		{
			name:  "a record that is not a record",
			nodes: []*irpb.Node{unframedFile(1, 2), stateNode(2, true, 30), transitionNode(30, 99, 2)},
			says:  "admits names node 99",
		},
		{
			name: "a next state that is not a state",
			nodes: []*irpb.Node{
				unframedFile(1, 2), stateNode(2, true, 30), recordNode(4, "R", ""), groupNode(20, "R"), transitionNode(30, 4, 99),
			},
			says: "moves to names node 99",
		},
		{
			name: "a record with no name at all",
			nodes: []*irpb.Node{
				unframedFile(1, 2), stateNode(2, true, 30), recordNode(4, "", ""), groupNode(20, ""), transitionNode(30, 4, 2),
			},
			says: "carries no name",
		},
		{
			// Not the same as the empty string, and refused for the same
			// reason: space is a rune a label may carry, so a name of nothing
			// but spaces would reach the diagram as an edge with no label —
			// which is the one thing an edge here exists to say.
			name: "a record whose name is nothing but whitespace",
			nodes: []*irpb.Node{
				unframedFile(1, 2), stateNode(2, true, 30), recordNode(4, "  ", ""), groupNode(20, "  "), transitionNode(30, 4, 2),
			},
			says: "carries no name",
		},
		{
			name: "a segmented file whose largest segment carries no data",
			nodes: []*irpb.Node{
				fileNode(1, &irpb.File{
					StartStateId: 2,
					Framing:      &irpb.File_Segmented{Segmented: &irpb.Segmented{MaxSegmentSize: 0}},
				}),
				stateNode(2, true),
			},
			says: "largest segment is 0 bytes",
		},
		{
			// Four is the segment descriptor word itself, so a segment of four
			// carries the word and nothing behind it.
			name: "a segmented file whose largest segment is the descriptor word",
			nodes: []*irpb.Node{
				fileNode(1, &irpb.File{
					StartStateId: 2,
					Framing:      &irpb.File_Segmented{Segmented: &irpb.Segmented{MaxSegmentSize: 4}},
				}),
				stateNode(2, true),
			},
			says: "largest segment is 4 bytes",
		},
		{
			name:  "a file node with no framing",
			nodes: []*irpb.Node{fileNode(1, &irpb.File{StartStateId: 2}), stateNode(2, true)},
			says:  "no framing",
		},
		{
			name: "a delimited file with no delimiter",
			nodes: []*irpb.Node{
				fileNode(1, &irpb.File{
					StartStateId: 2,
					Framing: &irpb.File_Delimited{Delimited: &irpb.Delimited{
						Placement: irpb.DelimiterPlacement_DELIMITER_PLACEMENT_TERMINATOR,
					}},
				}),
				stateNode(2, true),
			},
			says: "no bytes at all",
		},
		{
			name: "a delimited file that does not say where its delimiter stands",
			nodes: []*irpb.Node{
				fileNode(1, &irpb.File{
					StartStateId: 2,
					Framing:      &irpb.File_Delimited{Delimited: &irpb.Delimited{Delimiter: []byte{0x0A}}},
				}),
				stateNode(2, true),
			},
			says: "where its delimiter stands",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := read(&irpb.Descriptor{Version: supportedIRVersion, Nodes: testCase.nodes}, defaults())
			if err == nil {
				t.Fatal("read accepted a descriptor that does not say what a descriptor says")
			}

			if !strings.Contains(err.Error(), testCase.says) {
				t.Errorf("the refusal reads %q, and does not say %q", err, testCase.says)
			}

			// A note naming the rule, so that a user holding the producer can
			// tell a bug in it from a bug in their layout.
			var carrier noted
			if !errors.As(err, &carrier) || len(carrier.Notes()) == 0 {
				t.Errorf("the refusal %q carries no note naming the rule it is about", err)
			}
		})
	}
}

// TestARefusedDescriptorLeavesNoDocumentBehind is the other half of the same
// posture, at the level cpybkc sees: a plugin that refused after writing would
// leave an output directory cpybkc is entitled to merge, holding a diagram of a
// layout the generator could not read.
func TestARefusedDescriptorLeavesNoDocumentBehind(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{Version: supportedIRVersion, Nodes: []*irpb.Node{stateNode(2, true)}}

	out := t.TempDir()

	if err := run(vector(t, marshal(t, d), out), nothing()); err == nil {
		t.Fatal("run drew a diagram of a descriptor with no file node")
	}

	if entries, err := os.ReadDir(out); err != nil {
		t.Fatalf("reading the output directory: %v", err)
	} else if len(entries) != 0 {
		t.Errorf("a refused descriptor left %d files beneath --out", len(entries))
	}
}

// TestTheSameAutomatonDrawsTheSameBytes is docs/plugin/SPEC.md's "Determinism"
// where this story could most easily have broken it: the walk indexes the nodes
// by identifier, and a walk that had read a map's iteration order into its
// output would produce a different document on most runs.
//
// Ten runs rather than two, because a map with a dozen keys agrees with itself
// often enough that a single repetition proves little.
func TestTheSameAutomatonDrawsTheSameBytes(t *testing.T) {
	t.Parallel()

	d := ordersAutomaton(unframed4())

	first := writtenDocument(t, d)

	for range 10 {
		again, ok := proto.Clone(d).(*irpb.Descriptor)
		if !ok {
			t.Fatal("cloning the descriptor produced something else")
		}

		if written := writtenDocument(t, again); written != first {
			t.Fatalf("two runs over one descriptor drew different documents\n got:\n%s\nwant:\n%s", written, first)
		}
	}
}

// framed is one of the four framings, as the tests and the goldens both name
// it.
//
// The file node is carried whole rather than as its framing member, because the
// interface those members satisfy is not one irpb exports — a test may name
// [irpb.File_Unframed] and may not name the type of the field it goes in.
type framed struct {
	// name is what the test and the golden file are called.
	name string

	// file is the file node itself, framing and all.
	file *irpb.File

	// states is what the framing's sentence has to say, in facts rather than
	// in wording.
	states []string
}

// framings is the four members, and it is what makes "one golden per framing" a
// list with one place to add a fifth if the schema ever grows one.
func framings() []framed {
	return []framed{
		{
			name:   "unframed",
			file:   unframed4(),
			states: []string{"unframed"},
		},
		{
			name:   "descriptor-word",
			file:   &irpb.File{Framing: &irpb.File_DescriptorWord{DescriptorWord: &irpb.DescriptorWord{}}},
			states: []string{"descriptor word"},
		},
		{
			name: "segmented",
			file: &irpb.File{Framing: &irpb.File_Segmented{
				Segmented: &irpb.Segmented{MaxSegmentSize: 32760},
			}},
			states: []string{"segmented", "32760"},
		},
		{
			name: "delimited",
			file: &irpb.File{Framing: &irpb.File_Delimited{Delimited: &irpb.Delimited{
				Delimiter: []byte{0x0D, 0x0A},
				Placement: irpb.DelimiterPlacement_DELIMITER_PLACEMENT_SEPARATOR,
			}}},
			// The bytes as bytes. docs/ir/SPEC.md's "A delimiter is bytes, not
			// a character" is a rule about what a consumer compares, and a
			// document printing `CRLF` would have named a character for a file
			// whose bytes it does not know.
			states: []string{"delimited", "0x0D 0x0A", "separator"},
		},
	}
}

// ordersAutomaton is the automaton every golden is drawn from: a header, any
// number of details, and a trailer that ends the file.
//
// One automaton across all four framings, so that the only difference between
// two goldens is the sentence stating the framing — which is what makes those
// four files readable as one fact each rather than four documents to compare by
// eye.
//
// It carries the three things a diagram of an automaton has to get right: a
// start state that is not the lowest-numbered node, a cycle, and a state
// offering two transitions in an order that is not their identifier order. One
// record carries an override and two do not.
//
// Each record holds a couple of items, so that the four goldens show the table
// beneath the diagram as well as the diagram. They are deliberately plain —
// constant offsets, no table, no variant — because what varies across these
// four files is the framing sentence and nothing else, and the item tables that
// exercise this generator's arithmetic are [variableGolden]'s.
func ordersAutomaton(file *irpb.File) *irpb.Descriptor {
	file.StartStateId = 2

	return &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			fileNode(1, file),

			stateNode(2, false, 10),
			stateNode(3, false, 11, 12),
			stateNode(7, true),

			recordNode(4, "ORDER-HEADER", ""),
			recordNode(5, "ORDER-DETAIL", "DETAIL-LINE"),
			recordNode(6, "ORDER-TRAILER", ""),

			transitionNode(10, 4, 3),
			transitionNode(11, 5, 3),
			transitionNode(12, 6, 7),

			groupNode(20, "ORDER-HEADER", 40, 41),
			fieldNode(40, "ORDER-NUMBER", 8),
			numericFieldNode(41, "LINE-COUNT", 3, 3),

			groupNode(21, "ORDER-DETAIL", 42, 43),
			fieldNode(42, "PART-NUMBER", 10),
			numericFieldNode(43, "QUANTITY", 4, 4),

			groupNode(22, "ORDER-TRAILER", 44),
			numericFieldNode(44, "ORDER-TOTAL", 9, 9),
		},
	}
}

// unframed4 is the framing three quarters of these tests use, as a file node.
func unframed4() *irpb.File {
	return &irpb.File{Framing: &irpb.File_Unframed{Unframed: &irpb.Unframed{}}}
}

// fileNode is a file node, and unframedFile is the unframed one these tests
// reach for when the framing is not what they are about.
func fileNode(id uint64, f *irpb.File) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_File{File: f}}
}

func unframedFile(id, start uint64) *irpb.Node {
	f := unframed4()
	f.StartStateId = start

	return fileNode(id, f)
}

// state is a state node, with the transitions it carries in the order it
// carries them.
func stateNode(id uint64, accepts bool, transitions ...uint64) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_State{State: &irpb.State{
		Accepts:       accepts,
		TransitionIds: transitions,
	}}}
}

// transition is a transition node: the record it admits and the state it moves
// to.
func transitionNode(id, admits, to uint64) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Transition{Transition: &irpb.Transition{
		RecordId:    admits,
		NextStateId: to,
	}}}
}

// record is a record node, with the rename a layout gave it where the test
// gives one.
//
// The override is set as present only where it is non-empty, which is the
// distinction [nameOf] turns on: a layout that renamed nothing leaves the field
// absent, and this helper may not be the thing that blurs the two.
func recordNode(id uint64, original, override string) *irpb.Node {
	names := &irpb.Names{Original: original}
	if override != "" {
		names.OverrideName = proto.String(override)
	}

	return &irpb.Node{Id: id, Kind: &irpb.Node_Record{Record: &irpb.Record{
		RootId: id + 16,
		Names:  names,
	}}}
}

// recordOf is a record node whose top level the test names, which is what a
// fixture carrying a path within a record needs: [recordNode]'s derived
// identifier is a convenience for the fixtures that never walk into one.
func recordOf(id, root uint64, original string) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Record{Record: &irpb.Record{
		RootId: root,
		Names:  &irpb.Names{Original: original},
	}}}
}

// groupNode is a group: a record's top level, or an item beneath one holding
// other items. Members where a fixture walks into it, and none where it is
// there so that the fixture is a descriptor rather than the part of one a test
// happens to read.
func groupNode(id uint64, original string, members ...uint64) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Group{Group: &irpb.Group{
		MemberIds: members,
		Names:     &irpb.Names{Original: original},
	}}}
}

// defaults is the options an invocation that states none resolves to, which is
// what every test but the ones about an option itself reads a descriptor under.
//
// Taken through [options.defaulted] rather than written out, so that a change to
// a default lands here rather than leaving these tests reading a descriptor
// under options no invocation produces.
func defaults() options { return options{}.defaulted() }

// drawn is the graph [read] makes of a descriptor.
func drawn(t *testing.T, d *irpb.Descriptor) *graph {
	t.Helper()

	g, err := read(d, defaults())
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	return g
}

// writtenDocument is the whole document a run over this descriptor writes, in the
// default notation, taken through [run] rather than through the emitter — so
// that what these tests hold is what a file beneath `--out` holds.
//
// opts are appended to the argument vector as they stand, so a test states an
// option the way a manifest does rather than by reaching past [parse] into an
// [options] value no invocation could have produced.
func writtenDocument(t *testing.T, d *irpb.Descriptor, opts ...string) string {
	t.Helper()

	out := t.TempDir()

	if err := run(append(vector(t, marshal(t, d), out), opts...), nothing()); err != nil {
		t.Fatalf("run: %v", err)
	}

	return contents(t, filepath.Join(out, mermaidFile))
}

// ids is the identifiers of a list of states, for a failure that has to say
// which states were drawn.
func ids(states []state) []uint64 {
	drawn := make([]uint64, 0, len(states))
	for _, s := range states {
		drawn = append(drawn, s.id)
	}

	return drawn
}

// same reports whether two identifier lists are the same list in the same
// order.
func same(got, want []uint64) bool {
	if len(got) != len(want) {
		return false
	}

	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}
