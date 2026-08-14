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
// It is still smaller than the automaton. Each record's items hang off these
// states and edges as [graph.records], read by the same walk and drawn by both
// emitters rather than found by a second read of the descriptor — which is how
// the predicates, guards, bindings and registers below arrived.
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

	// registers is every register node the descriptor carries, in ascending
	// identifier order, each with the transitions that write it.
	//
	// Every one, and not the ones something reads. A register nothing binds is
	// a bug in whatever compiled the automaton — docs/ir/SPEC.md makes reading
	// one malformed — and it is another one nobody sees unless the document
	// draws it.
	registers []register

	// records is one table per record the automaton admits, in the order the
	// states above first admit it, and empty where `records=none` asked for the
	// sequencing view alone.
	//
	// Empty is therefore two things — the option said no, or the automaton
	// admits no record — and neither wants a section. What it never means is an
	// empty table: a record whose top level holds no item is a [recordTable]
	// carrying no rows, and the document says so in words.
	records []recordTable
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

	// acceptance are the guards qualifying that acceptance, empty where it is
	// unconditional.
	//
	// Drawn rather than dropped, because guarded acceptance is what makes the
	// last iteration of a count detectable: a state that accepts only with the
	// counter at zero is how a file two details short is told from a complete
	// one, and an accepting state drawn as unconditional would tell a reader
	// the opposite of the truth.
	acceptance []guard

	// reachable is whether the walk from the start state arrives here.
	reachable bool

	// edges are the transitions leaving it, in the order State.transition_ids
	// gives them — which is the order a consumer evaluates them in, so a
	// diagram that reordered them would misdescribe which one wins.
	edges []edge
}

// accepted is the phrase qualifying an accepting state's acceptance, and the
// empty string where acceptance is unconditional.
func (s state) accepted() string {
	if len(s.acceptance) == 0 {
		return ""
	}

	return "if " + conjunction(s.acceptance)
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

	// recordID is that record node's own identifier.
	//
	// Carried beside the name and drawn nowhere, because [graph.readRecords]
	// tables each record once and a name is not identity — docs/ir/SPEC.md's
	// "Names" makes duplicate data names legal COBOL, so two records that share
	// a name are two records and deduplicating on the name would table only the
	// first of them.
	recordID uint64

	// predicate is what selects this transition on the bytes in front of the
	// reader, and says so where the transition carries none.
	predicate predicate

	// guards are what make it eligible at all, in the order the transition
	// carries them. All of them must hold, so their order is not significant
	// and the descriptor's is the only one there is to keep.
	guards []guard

	// bindings are what it writes into the register file when it is taken, in
	// the order the transition carries them.
	bindings []binding
}

// label is the whole of an edge's label: the record it admits, what selects it,
// what makes it eligible and what it remembers.
//
// # The order, and why the record comes first
//
// A person reading this diagram is following a file, so the record is the thing
// they are looking for and it is what a label opens with. Behind it the label
// reads in the order a consumer evaluates: `when` is the bytes, `if` is the
// register file, `then` is what the transition leaves behind. That is not quite
// the order the read loop runs in — guards are checked before the record is
// examined at all — and it is the order the sentence reads in, which is what a
// label is for.
//
// # esc
//
// Every name here came out of somebody's copybook and every connecting word is
// this generator's, so the escaping is applied to the first and not to the
// second. Sections are separated by a comma and a space, guards and bindings
// are joined by `and`, and a set of literals by `or` — three separators that
// cannot be confused for one another, which is what keeps a label with all
// three sections readable as three.
func (e edge) label(esc func(string) string) string {
	said := []string{esc(e.record), e.predicate.phrase(esc)}

	if len(e.guards) != 0 {
		said = append(said, "if "+conjunction(e.guards))
	}

	if len(e.bindings) != 0 {
		printed := make([]string, 0, len(e.bindings))
		for _, b := range e.bindings {
			printed = append(printed, b.phrase(esc))
		}

		said = append(said, "then "+strings.Join(printed, " and "))
	}

	return strings.Join(said, ", ")
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

// stateName is what a document calls a state: `s` and the state node's own
// identifier.
//
// The identifier rather than a name, because states carry identifiers and no
// names — and because it is what takes a reader who wants more than the diagram
// says to the right node of `cpybkc --emit-ir`. It is also, incidentally, the
// one part of any of these documents that can never need escaping: a decimal
// number carries no metacharacter of any notation.
//
// Which is why it is here rather than in an emitter. Two notations calling one
// state two things would make a reader who has both documents open unable to
// put them side by side.
func stateName(id uint64) string { return fmt.Sprintf("s%d", id) }

// admitsNothing is the sentence a document states when the automaton consumes
// no record at all, and the empty string when it consumes one.
//
// Said rather than left to be noticed. An automaton with no transition in it
// draws as a start state and nothing else, which looks like a generator that
// failed halfway; the sentence is the difference between "there is nothing
// here" and "there is nothing there".
func (g *graph) admitsNothing() string {
	if g.admits() {
		return ""
	}

	return "This automaton admits no record: no state offers a transition, so nothing a reader of it does consumes bytes."
}

// stranded is the sentence a document states about the states no path from the
// start state arrives at, and the empty string when every state is reachable.
//
// Here rather than in an emitter for the reason [framing.String] is: it is
// prose about the descriptor and not about a notation, both notations have the
// same thing to say, and a second emitter free to word it its own way is a
// second emitter free to word it less carefully.
func (g *graph) stranded() string {
	named := []string{}
	for _, s := range g.unreachable() {
		named = append(named, stateName(s.id))
	}

	if len(named) == 0 {
		return ""
	}

	subject, drawn := fmt.Sprintf("States %s are", strings.Join(named, ", ")), "Each is"
	if len(named) == 1 {
		subject, drawn = fmt.Sprintf("State %s is", named[0]), "It is"
	}

	return subject + " not reachable from the start state. " + drawn + " drawn anyway, marked *unreachable*:" +
		" a state no path arrives at is a bug in whatever compiled this automaton, and one nobody sees if the diagram leaves it out."
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
//
// # Why the options reach the walk
//
// `records=none` is not a section left out at the end; it is a walk that does
// not happen. Each record's containment order is read only where the document
// is going to draw it, for the reason [fieldPath] gives about a record's top
// level: refusing a descriptor over a dangling reference in the part of it this
// document does not draw would be refusing a diagram over something nobody
// looking at the diagram could see.
func read(d *irpb.Descriptor, opts options) (*graph, error) {
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

	// Last, because the register table's third column is the edges above: it is
	// read off the graph rather than off the descriptor, so the table and the
	// diagram cannot disagree about which transition binds what.
	if err := g.readRegisters(nodes); err != nil {
		return nil, err
	}

	// Last of all, and off the edges above rather than off the descriptor's
	// record nodes: what the document tables is what the automaton admits, so a
	// record node nothing admits is not a table nobody asked for.
	//
	// A switch with a failing default rather than an equality test, for the
	// reason [document]'s last arm is a failure: an equality test makes every
	// value that is not `all` mean `none`, which is a fall back to a member of a
	// closed set this generator recognises — the one thing docs/ir/SPEC.md says
	// a consumer must not do. [parse] admits exactly these two, so the last arm
	// cannot be reached from an argument vector; the way it *would* be reached
	// is a third value added to the set with no arm added here, and the
	// alternative to failing is a document silently missing a section somebody
	// asked for.
	switch opts.records {
	case recordsAll:
		if err := g.readRecords(nodes); err != nil {
			return nil, err
		}
	case recordsNone:
	default:
		return nil, fmt.Errorf("this generator has no reading of %s=%q, which is a bug in %s",
			recordsOption, opts.records, pluginName)
	}

	return g, nil
}

// readRegisters is every register node the descriptor carries, with the
// transitions the walk above found writing it.
func (g *graph) readRegisters(nodes nodeSet) error {
	at := map[uint64]int{}

	for _, node := range nodes.order {
		kind, ok := node.GetKind().(*irpb.Node_Register)
		if !ok {
			continue
		}

		holds, err := holdsOf(kind.Register, node.GetId())
		if err != nil {
			return err
		}

		at[node.GetId()] = len(g.registers)
		g.registers = append(g.registers, register{id: node.GetId(), holds: holds})
	}

	for _, s := range g.states {
		for _, e := range s.edges {
			for _, b := range e.bindings {
				// Every binding's register resolved on the way in, so this
				// lookup is expected to find one. It is read comma-ok anyway,
				// because a missing key answers zero and zero is a valid index:
				// the failure of that expectation would not be a crash but a
				// binding credited to whichever register happens to be first,
				// in a table that looks perfectly well formed.
				held, ok := at[b.register]
				if !ok {
					return unresolved(b.register, "the register a transition's binding writes")
				}

				g.registers[held].boundBy = append(g.registers[held].boundBy,
					binder{from: s.id, to: e.to, record: e.record})
			}
		}
	}

	return nil
}

// unbound is the sentence a document states about the registers no transition
// writes, and the empty string where every register is written.
//
// Here rather than in an emitter for the reason [graph.stranded] is: it is
// prose about the descriptor and the same in either notation. It says the same
// kind of thing, too — docs/ir/SPEC.md's "A register is read only where it has
// been written" makes reading a register nothing has bound a malformed
// descriptor, so a register with an empty third column is either a producer bug
// or a node nothing needed, and both are worth a reader's eye.
func (g *graph) unbound() string {
	named := []string{}

	for _, r := range g.registers {
		if len(r.boundBy) == 0 {
			named = append(named, registerName(r.id))
		}
	}

	if len(named) == 0 {
		return ""
	}

	subject, read := fmt.Sprintf("Registers %s are", strings.Join(named, ", ")), "each"
	if len(named) == 1 {
		subject, read = fmt.Sprintf("Register %s is", named[0]), "it"
	}

	return subject + " written by no transition. A register is read only where it has been written, so " + read +
		" is either a node nothing needed or a guard reading a value nothing put there — which is a malformed descriptor rather than an empty one."
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

	// Acceptance guards on a state that does not accept are refused rather than
	// dropped. docs/ir/SPEC.md carries them "empty where acceptance is
	// unconditional — which, on a state that does not accept at all, it is", so
	// a state carrying them and no acceptance is a producer that meant one of
	// the two and wrote the other. Drawing them nowhere would be the blank cell
	// this generator refuses everywhere else.
	if len(node.GetAcceptanceGuardIds()) != 0 && !node.GetAccepts() {
		return state{}, malformed(
			fmt.Sprintf("state %d does not accept and carries %d acceptance guards", id, len(node.GetAcceptanceGuardIds())),
			"a state's acceptance guards are empty where acceptance is unconditional, which on a state that does not accept at all it is; see docs/ir/SPEC.md, \"The sequencing automaton\"")
	}

	for _, guardID := range node.GetAcceptanceGuardIds() {
		g, err := guardAt(nodes, guardID, fmt.Sprintf("an acceptance guard of state %d", id))
		if err != nil {
			return state{}, err
		}

		s.acceptance = append(s.acceptance, g)
	}

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
// The label opens with the record the transition admits, which is what a person
// verifying a layout is reading the diagram for. That is not the record-name
// label docs/ir/SPEC.md's "The sequencing automaton" forbids: what it forbids is
// a transition *selected* by a record name, a test no consumer can run, and this
// is a drawing of what the transition produces once its predicate — resolved
// below, and drawn behind the name — has already chosen it.
func edgeAt(nodes nodeSet, id uint64) (edge, error) {
	node, ok := nodes.transition(id)
	if !ok {
		return edge{}, unresolved(id, "a transition")
	}

	record, ok := nodes.record(node.GetRecordId())
	if !ok {
		return edge{}, unresolved(node.GetRecordId(), fmt.Sprintf("the record transition %d admits", id))
	}

	// Whitespace counts as nothing, and not as a name. A record called " "
	// would pass an emptiness test, reach the emitter, and draw as an edge
	// whose label is a space — an unlabelled transition, which is the one thing
	// an edge in this diagram exists to say.
	name := nameOf(record.GetNames())
	if strings.TrimSpace(name) == "" {
		return edge{}, malformed(
			fmt.Sprintf("node %d is a record and carries no name a diagram could show", node.GetRecordId()),
			"every named node carries the original COBOL name, spelled as the copybook spells it; see docs/ir/SPEC.md, \"Names\"")
	}

	if _, ok := nodes.state(node.GetNextStateId()); !ok {
		return edge{}, unresolved(node.GetNextStateId(), fmt.Sprintf("the state transition %d moves to", id))
	}

	e := edge{to: node.GetNextStateId(), record: name, recordID: node.GetRecordId()}

	p, err := predicateOf(nodes, node, record)
	if err != nil {
		return edge{}, err
	}

	e.predicate = p

	for _, guardID := range node.GetGuardIds() {
		g, err := guardAt(nodes, guardID, fmt.Sprintf("a guard of transition %d", id))
		if err != nil {
			return edge{}, err
		}

		e.guards = append(e.guards, g)
	}

	for _, bindingID := range node.GetBindingIds() {
		b, err := bindingAt(nodes, bindingID, record)
		if err != nil {
			return edge{}, err
		}

		e.bindings = append(e.bindings, b)
	}

	return e, nil
}

// nameOf is the name a diagram gives a node: the rename an adopter asked for
// where they asked for one, and the copybook's own spelling otherwise.
//
// docs/ir/SPEC.md's "Names" carries the original beside an override rather than
// in place of it, so both are present and this chooses. The override wins
// because it is the name the person reading this diagram wrote in their layout,
// and the same choice is made of a record, a group and a field, so that a
// predicate's path, an edge's record and an item table's rows all name a node
// the one way.
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

// segmentDescriptorWidth is the width of the segment descriptor word DFSMS
// defines. The IR carries it nowhere, for the reason docs/ir/SPEC.md's "Lengths
// the file node does not carry" gives: the width comes with the definition, and
// one carried beside it could hold a number describing no format anyone has.
const segmentDescriptorWidth = 4

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
		f := framing{how: segmented, maxSegment: kind.Segmented.GetMaxSegmentSize()}

		// The same refusal cmd/cpybkc-gen-go makes, and for the same reason:
		// four of a segment's bytes are the segment descriptor word itself, so
		// a largest segment of four or fewer carries no data and a record would
		// never end. Stated here as well as there because a document reading
		// "no segment exceeds 0 bytes" would be describing a file nothing can
		// consume a byte of, in the same confident voice as the other three
		// framings — which is the "no honest picture to draw" case above.
		if f.maxSegment <= segmentDescriptorWidth {
			return framing{}, malformed(
				fmt.Sprintf("the file is segmented and its largest segment is %d bytes", f.maxSegment),
				"a segment carries a four-byte segment descriptor word and the data behind it, so a largest segment of four or fewer carries no data at all")
		}

		return f, nil
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
		if _, ok := nodes.by[node.GetId()]; ok {
			continue
		}

		// Both fields take the same first-wins decision, in the same pass. They
		// used to disagree, and the two ways that showed were both silent: a
		// duplicated unreachable state drawn twice, and — where the duplicate
		// was of another kind — a refusal reading "no such node of that kind",
		// which describes a dangling reference rather than the duplicated
		// identifier that actually happened.
		nodes.by[node.GetId()] = node
		nodes.order = append(nodes.order, node)
	}

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
