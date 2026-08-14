// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Zaba505/cpybkc/irpb"
)

// graph is the document this generator draws, as a model rather than as text:
// the file node's framing, where a read begins, and every state the descriptor
// carries with the transitions leaving it.
//
// # Why a model, and not a string builder
//
// There are two notations (README.md's `format` option) and one automaton. The
// walk that reads the automaton out of a descriptor is the part with rules in
// it — resolve by identifier, keep the transition order, do not drop a state
// nothing reaches — and a generator that walked once per notation would have
// two places for those rules to drift apart. So [read] is the walk, this is
// what it produces, and an emitter is a function over it: [mermaid] today, and
// the `dot` emitter of #190 as a second consumer of exactly this, with no walk
// of its own.
//
// It is deliberately smaller than the automaton. Predicates, guards, bindings
// and registers (#188) and each record's items (#189) hang off these states and
// edges in the stories after this one, and each is a field added here and read
// by both emitters rather than a second read of the descriptor.
type graph struct {
	// framing is the file node's framing, which the document states because
	// what stands between two records is part of what a person reading this
	// diagram is verifying.
	framing framing

	// start is the identifier of the state a read begins in, which is
	// File.start_state_id and never a state a walk picked out.
	start uint64

	// states is every state the descriptor carries: those a walk from [start]
	// reaches, in the order it reaches them, and then those it does not.
	//
	// Both, rather than the reachable ones alone. A state nothing reaches is a
	// bug in whatever compiled the automaton, and it is one a person looking at
	// this diagram can see only if it is drawn; dropping it silently would make
	// the diagram agree with a descriptor that is wrong.
	states []state
}

// state is one state of the automaton, as the diagram draws it.
type state struct {
	// id is the state node's own identifier, which is what the diagram names it
	// by: a reader who wants more than the diagram says goes to that node in
	// `cpybkc --emit-ir`, and a name this generator invented would not take
	// them there. States carry identifiers and no names.
	id uint64

	// accepts is whether reaching end of input here is a complete file.
	accepts bool

	// reachable is whether the walk from the start state arrives here.
	reachable bool

	// edges are the transitions leaving it, in the order State.transition_ids
	// gives them — which is the order a consumer evaluates them in, so a
	// diagram that reordered them would misdescribe which one wins.
	edges []edge
}

// edge is one transition: exactly one record consumed, and the state that
// leaves the automaton in.
type edge struct {
	// to is the identifier of the state it moves to.
	to uint64

	// record is the name of the record it admits, by docs/ir/SPEC.md's "Names":
	// the override where the layout gave one, and the copybook's own spelling
	// otherwise.
	//
	// It is the name and not an escaped label, because the two notations escape
	// differently and a model carrying one notation's escaping would be a model
	// the other could not use.
	record string
}

// admits reports whether any state offers a transition, which is whether this
// automaton consumes a record at all.
func (g *graph) admits() bool {
	for _, s := range g.states {
		if len(s.edges) != 0 {
			return true
		}
	}

	return false
}

// unreachable is the states no walk from the start state arrives at, in the
// order [read] put them in.
func (g *graph) unreachable() []state {
	var stranded []state

	for _, s := range g.states {
		if !s.reachable {
			stranded = append(stranded, s)
		}
	}

	return stranded
}

// read is the whole walk: it indexes the descriptor's nodes, reads the file
// node, and follows the automaton out of the state that file node names.
//
// Every reference is resolved by identifier and by the kind its position
// admits, and one that does not resolve is reported as a malformed descriptor
// rather than drawn around. A diagram is a thing somebody is about to trust,
// and the failure this generator exists to avoid — stated for the version check
// in this command's package comment — is a confident picture of an automaton it
// understood only in part.
func read(d *irpb.Descriptor) (*graph, error) {
	nodes := index(d)

	file, err := fileOf(d)
	if err != nil {
		return nil, err
	}

	how, err := framingOf(file)
	if err != nil {
		return nil, err
	}

	g := &graph{framing: how, start: file.GetStartStateId()}

	if _, ok := nodes.state(g.start); !ok {
		return nil, malformed(
			fmt.Sprintf("the file node begins in node %d, and the descriptor carries no state node with that identifier", g.start),
			"a file node names the state a read begins in; see docs/ir/SPEC.md, \"The sequencing automaton\"")
	}

	// Depth-first from the start state, following each state's transitions in
	// the order it carries them, so that the diagram is laid out along the path
	// a reader traces — a header, then what follows it, then what follows that.
	// Any total order would be deterministic, which is what
	// docs/plugin/SPEC.md's "Determinism" asks for; this one is also the order
	// the automaton is read in.
	seen := map[uint64]bool{}

	stranded, err := g.walk(nodes, g.start, seen)
	if err != nil {
		return nil, err
	}

	// Then the states the walk never arrived at, in ascending identifier order
	// — the descriptor's own order, and a total one over states no path
	// relates.
	for _, id := range stranded {
		s, err := stateAt(nodes, id, false)
		if err != nil {
			return nil, err
		}

		g.states = append(g.states, s)
	}

	return g, nil
}

// walk appends every state reachable from id to the graph, in the order it
// reaches them, and answers with the identifiers of the states it did not
// reach.
func (g *graph) walk(nodes nodeSet, id uint64, seen map[uint64]bool) ([]uint64, error) {
	// An explicit stack rather than recursion: the depth of this walk is the
	// depth of somebody's automaton, and a generator that overflowed its stack
	// on a large layout would fail in a way no diagnostic of this program
	// composes.
	stack := []uint64{id}

	for len(stack) > 0 {
		at := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if seen[at] {
			continue
		}

		seen[at] = true

		s, err := stateAt(nodes, at, true)
		if err != nil {
			return nil, err
		}

		g.states = append(g.states, s)

		// Pushed in reverse so that the first transition the state carries is
		// the first one popped, which is what makes this a pre-order walk in
		// the order a consumer evaluates transitions.
		for i := len(s.edges) - 1; i >= 0; i-- {
			if !seen[s.edges[i].to] {
				stack = append(stack, s.edges[i].to)
			}
		}
	}

	var stranded []uint64

	for _, node := range nodes.order {
		if _, ok := node.GetKind().(*irpb.Node_State); ok && !seen[node.GetId()] {
			stranded = append(stranded, node.GetId())
		}
	}

	return stranded, nil
}

// stateAt is one state node as the diagram draws it, with every transition
// leaving it resolved.
func stateAt(nodes nodeSet, id uint64, reachable bool) (state, error) {
	node, ok := nodes.state(id)
	if !ok {
		return state{}, unresolved(id, "a state")
	}

	s := state{id: id, accepts: node.GetAccepts(), reachable: reachable}

	for _, transition := range node.GetTransitionIds() {
		e, err := edgeAt(nodes, transition)
		if err != nil {
			return state{}, err
		}

		s.edges = append(s.edges, e)
	}

	return s, nil
}

// edgeAt is one transition node as the diagram draws it.
//
// The label is the record the transition admits, which is what a person
// verifying a layout is reading the diagram for. That is not the record-name
// label docs/ir/SPEC.md's "The sequencing automaton" forbids: what it forbids is
// a transition *selected* by a record name, a test no consumer can run, and
// this is a drawing of what the transition produces once its predicate — #188's
// — has already chosen it.
func edgeAt(nodes nodeSet, id uint64) (edge, error) {
	node, ok := nodes.transition(id)
	if !ok {
		return edge{}, unresolved(id, "a transition")
	}

	record, ok := nodes.record(node.GetRecordId())
	if !ok {
		return edge{}, unresolved(node.GetRecordId(), fmt.Sprintf("the record transition %d admits", id))
	}

	name := nameOf(record.GetNames())
	if name == "" {
		return edge{}, malformed(
			fmt.Sprintf("node %d is a record and carries no name at all", node.GetRecordId()),
			"every named node carries the original COBOL name, spelled as the copybook spells it; see docs/ir/SPEC.md, \"Names\"")
	}

	if _, ok := nodes.state(node.GetNextStateId()); !ok {
		return edge{}, unresolved(node.GetNextStateId(), fmt.Sprintf("the state transition %d moves to", id))
	}

	return edge{to: node.GetNextStateId(), record: name}, nil
}

// nameOf is the name a diagram gives a node: the rename an adopter asked for
// where they asked for one, and the copybook's own spelling otherwise.
//
// docs/ir/SPEC.md's "Names" carries the original beside an override rather than
// in place of it, so both are present and this chooses. The override wins
// because it is the name the person reading this diagram wrote in their layout;
// the original is still what the copybook says, and #189's item table is where
// it earns a column.
//
// Presence rather than emptiness decides. An override is explicitly optional in
// the schema, and an override set to the empty string is a producer saying
// something — that the name is nothing — rather than a producer that left the
// field alone.
func nameOf(n *irpb.Names) string {
	if n != nil && n.OverrideName != nil {
		return n.GetOverrideName()
	}

	return n.GetOriginal()
}

// framing is the file node's framing, in the form a document states it.
//
// The sentence is composed here rather than in an emitter because it is prose
// about the descriptor and not about a notation: both notations state the same
// four facts, and a second emitter that wrote its own would be free to state
// them differently.
type framing struct {
	// how is which of the four members the file node carries.
	how string

	// maxSegment is a segmented file's largest segment, in bytes.
	maxSegment uint32

	// delimiter and placement are a delimited file's, and the delimiter is
	// bytes rather than text: docs/ir/SPEC.md's "A delimiter is bytes, not a
	// character" has a consumer compare it as bytes and never interpret it
	// through a charset, and a document that printed it as characters would be
	// interpreting it through whichever charset the reader's eye supplied.
	delimiter []byte
	placement irpb.DelimiterPlacement
}

// The four members of the closed set, as a document names them.
const (
	unframed       = "unframed"
	descriptorWord = "descriptor word"
	segmented      = "segmented"
	delimited      = "delimited"
)

// String is the sentence a document states the framing as.
func (f framing) String() string {
	switch f.how {
	case unframed:
		return unframed + " — a record's bytes are its extent, and the record behind it begins at the byte after"
	case descriptorWord:
		return descriptorWord + " — every record is preceded by the record descriptor word DFSMS defines"
	case segmented:
		return fmt.Sprintf(
			"%s — a record is the concatenation of its segments, each preceded by a segment descriptor word, and no segment exceeds %d bytes",
			segmented, f.maxSegment)
	case delimited:
		return fmt.Sprintf("%s — the delimiter is %s, standing as a %s", delimited, hex(f.delimiter), placementOf(f.placement))
	default:
		// Unreachable: [framingOf] admits these four and refuses everything
		// else, so a framing reaching here is a fifth member added to the
		// schema with no arm added beside it. Written as a sentence rather than
		// as a panic, for the reason [document]'s default arm is a failure: the
		// alternative to saying so is a document that quietly states a framing
		// the descriptor does not carry.
		return "a framing this generator has no sentence for, which is a bug in " + pluginName
	}
}

// placementOf is where a delimited file's delimiter stands, as a phrase.
//
// Each says what it means for the end of a file, because that is what the
// placement is carried for: under a separator a trailing delimiter announces a
// record that is not there, and under a terminator a final record with nothing
// behind it is a file that was cut short.
func placementOf(p irpb.DelimiterPlacement) string {
	switch p {
	case irpb.DelimiterPlacement_DELIMITER_PLACEMENT_TERMINATOR:
		return "terminator (one follows every record, the last included)"
	case irpb.DelimiterPlacement_DELIMITER_PLACEMENT_SEPARATOR:
		return "separator (one stands between two records, and nothing follows the last)"
	case irpb.DelimiterPlacement_DELIMITER_PLACEMENT_OPTIONAL_TERMINATOR:
		return "optional terminator (one follows every record, except that the file may end after the last without one)"
	default:
		// Unreachable for the same reason as above: [framingOf] refuses a
		// placement outside the set before this is called.
		return "placement this generator has no phrase for, which is a bug in " + pluginName
	}
}

// hex is a delimiter as a document prints it: one `0x` byte per byte of it,
// separated by spaces.
//
// Bytes rather than characters, and never a named line ending. cp037 and cp1047
// disagree about which of 0x15 and 0x25 is a line feed, and the same file
// through Linux ends its records with 0x0A and through Windows with 0x0D 0x0A —
// so a document naming a character would tell one shop something untrue about
// the other's files.
func hex(b []byte) string {
	printed := make([]string, 0, len(b))
	for _, one := range b {
		printed = append(printed, fmt.Sprintf("0x%02X", one))
	}

	return strings.Join(printed, " ")
}

// fileOf is the descriptor's file node, refused where there is not exactly one.
func fileOf(d *irpb.Descriptor) (*irpb.File, error) {
	var file *irpb.File

	for _, node := range d.GetNodes() {
		kind, ok := node.GetKind().(*irpb.Node_File)
		if !ok {
			continue
		}

		if file != nil {
			return nil, malformed("the descriptor carries two file nodes",
				"exactly one node of kind File exists and it is the root; see docs/ir/SPEC.md, \"A node set, not a tree\"")
		}

		file = kind.File
	}

	if file == nil {
		return nil, malformed("the descriptor carries no file node",
			"exactly one node of kind File exists and it is the root; see docs/ir/SPEC.md, \"A node set, not a tree\"")
	}

	return file, nil
}

// framingOf reads the file node's framing into the form a document states, and
// refuses one it has nothing true to say about.
//
// Refused rather than drawn as "unstated", which is the other thing a diagram
// generator could do. The two failures are different in kind: a state nothing
// reaches is a well-formed descriptor whose contents are worth looking at, and
// the diagram draws it so that somebody does; a framing outside the closed set
// is a descriptor that does not say what a descriptor says, and there is no
// honest picture of it to draw.
func framingOf(file *irpb.File) (framing, error) {
	switch kind := file.GetFraming().(type) {
	case *irpb.File_Unframed:
		return framing{how: unframed}, nil
	case *irpb.File_DescriptorWord:
		return framing{how: descriptorWord}, nil
	case *irpb.File_Segmented:
		return framing{how: segmented, maxSegment: kind.Segmented.GetMaxSegmentSize()}, nil
	case *irpb.File_Delimited:
		f := framing{
			how:       delimited,
			delimiter: kind.Delimited.GetDelimiter(),
			placement: kind.Delimited.GetPlacement(),
		}

		if len(f.delimiter) == 0 {
			return framing{}, malformed("the file is delimited and its delimiter is no bytes at all",
				"a producer MUST NOT emit a delimiter of no bytes; see docs/ir/SPEC.md, \"A delimiter is bytes, not a character\"")
		}

		switch f.placement {
		case irpb.DelimiterPlacement_DELIMITER_PLACEMENT_TERMINATOR,
			irpb.DelimiterPlacement_DELIMITER_PLACEMENT_SEPARATOR,
			irpb.DelimiterPlacement_DELIMITER_PLACEMENT_OPTIONAL_TERMINATOR:
		default:
			return framing{}, malformed("the file is delimited and says nothing about where its delimiter stands",
				"a delimited file node carries the placement beside the bytes, and it is what makes the end of a file checkable; see docs/ir/SPEC.md, \"Terminator, separator, and the last record\"")
		}

		return f, nil
	default:
		return framing{}, malformed("the file node carries no framing",
			"the set is closed and a file node carries one member of it; see docs/ir/SPEC.md, \"Four framings, and none of them is a RECFM\"")
	}
}

// nodeSet is the descriptor's nodes indexed by identifier, which
// docs/ir/SPEC.md's "A node set, not a tree" requires of a consumer before it
// can walk anything.
type nodeSet struct {
	// by is every node, by identifier.
	by map[uint64]*irpb.Node

	// order is every node in ascending identifier order, which is the order a
	// descriptor carries them in and the order this generator falls back on
	// where no path relates two nodes.
	order []*irpb.Node
}

// index builds the [nodeSet].
func index(d *irpb.Descriptor) nodeSet {
	nodes := nodeSet{by: make(map[uint64]*irpb.Node, len(d.GetNodes()))}

	for _, node := range d.GetNodes() {
		// The first node with an identifier wins, and this generator does not
		// refuse a second: a producer assigns identifiers by a deterministic
		// traversal and two nodes sharing one is a bug no diagram is the place
		// to report. What matters here is that the choice is stated rather than
		// left to map iteration.
		if _, ok := nodes.by[node.GetId()]; !ok {
			nodes.by[node.GetId()] = node
		}
	}

	nodes.order = append(nodes.order, d.GetNodes()...)
	sort.SliceStable(nodes.order, func(i, j int) bool { return nodes.order[i].GetId() < nodes.order[j].GetId() })

	return nodes
}

// state is the state node with this identifier, where one is.
func (n nodeSet) state(id uint64) (*irpb.State, bool) {
	kind, ok := n.by[id].GetKind().(*irpb.Node_State)
	if !ok {
		return nil, false
	}

	return kind.State, true
}

// transition is the transition node with this identifier, where one is.
func (n nodeSet) transition(id uint64) (*irpb.Transition, bool) {
	kind, ok := n.by[id].GetKind().(*irpb.Node_Transition)
	if !ok {
		return nil, false
	}

	return kind.Transition, true
}

// record is the record node with this identifier, where one is.
func (n nodeSet) record(id uint64) (*irpb.Record, bool) {
	kind, ok := n.by[id].GetKind().(*irpb.Node_Record)
	if !ok {
		return nil, false
	}

	return kind.Record, true
}
