// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Zaba505/cpybkc/irpb"
)

// fileMachineFile is the file the reader and the writer are written to.
//
// A file of its own for the reason records.go and codec.go are files of their
// own: what is in it is one thing the descriptor says. records.go is the record
// nodes as types, codec.go is those types moving their own bytes, and this is
// the file node and the automaton — the framing around a record and the order
// records come in.
const fileMachineFile = "file.go"

// The identifiers the file-level reader and writer occupy at package scope.
//
// Every identifier munged from a copybook name is exported, so these five are
// the only place this generator can collide with one. A collision is reported
// rather than worked around, which is what records.go already does with two
// items that munge alike: an adopter renames the record in their layout and
// gets a name they chose.
const (
	recordInterface = "Record"
	readerType      = "Reader"
	writerType      = "Writer"
	newReaderFunc   = "NewReader"
	newWriterFunc   = "NewWriter"
)

// framing is the file node's framing, as the emitter switches over it.
type framing int

const (
	unframed framing = iota
	descriptorWord
	segmented
	delimited
)

// bounded reports whether the framing states a record's length in front of the
// record, so that a consumer holds the record's bytes before it evaluates a
// predicate against them.
//
// The two halves of the read loop that differ turn on exactly this: under a
// bounded framing a predicate whose target is not wholly inside the record the
// framing bounds does not match, and under an unbounded one the bytes are there
// whenever the input holds a whole record, so a target that runs off the end is
// a truncated file. See docs/ir/SPEC.md, "Where framing is consumed, and where
// it is emitted".
func (f framing) bounded() bool { return f == descriptorWord || f == segmented }

// filer writes one descriptor's file-level reader and writer.
type filer struct {
	*emitter

	// opts is the invocation's options, for the receiver the methods declare.
	opts options

	// file is the descriptor's file node: the framing and the start state.
	file *irpb.File

	// how is the framing, and what it carries.
	how        framing
	delimiter  []byte
	placement  irpb.DelimiterPlacement
	maxSegment uint32

	// states is every state node, in ascending identifier order, and index is
	// each one's position in it. The position is what the generated code holds,
	// so that a state is a small integer rather than a map lookup.
	states []*irpb.Node
	index  map[uint64]int

	// registers is every register node in ascending identifier order.
	registers []*irpb.Node

	// lookahead is the most bytes a predicate of any state reads, which is how
	// far the reader has to see in front of a record it has not identified.
	lookahead int

	// keepsBytes is whether any binding writes a bytes register, which is the
	// one thing that obliges the reader to hold the record's own bytes as well
	// as its values.
	keepsBytes bool

	// comparesBytes is whether anything in the generated file compares byte
	// strings, which is the whole of what it imports "bytes" for. See
	// [filer.survey], which settles it.
	comparesBytes bool
}

// fileImports is what every generated file of this kind imports.
//
// Each is used by something the file always declares: bufio and io by the
// reader, errors by the end-of-input test, and codec by both directions. Two
// are conditional. Only the diagnostic naming the record types a state expected
// uses strings, and a file whose every state offers one unconditional
// transition has no such diagnostic to make; only a comparison of byte strings
// uses bytes, and a file whose framing needs no delimiter and whose automaton
// carries neither a predicate nor a guard over a bytes register makes none. See
// [filer.survey] for the second.
var fileImports = []string{"bufio", "errors", "fmt", "io", codecImport}

// fileMachine is the source of [fileMachineFile] for this descriptor, or the
// empty string where this descriptor's automaton admits no record — because it
// carries no file node, or because no state offers a transition.
//
// The reader walks the automaton over a record stream and the writer walks the
// same automaton in the other direction. Neither is a table this generator
// interprets at run time: the states, their transitions and the predicates
// selecting them are emitted as Go, so that what an adopter reads is the walk
// their layout describes rather than an engine with their descriptor inside it.
func fileMachine(d *irpb.Descriptor, opts options) (string, error) {
	e, err := newEmitter(d)
	if err != nil {
		return "", err
	}

	f := &filer{
		emitter: e,
		opts:    opts,
		index:   make(map[uint64]int),
	}

	if err := f.collect(d); err != nil {
		return "", err
	}

	// A descriptor whose automaton admits nothing describes no record stream,
	// whether because it carries no file node or because no state offers a
	// transition. A reader and a writer over it would be a pair of types with
	// nothing to read or write, which is the test records.go already applies to
	// a descriptor carrying no record node.
	if f.file == nil || !f.admits() {
		return "", nil
	}

	body, imports, err := f.emit()
	if err != nil {
		return "", err
	}

	var b strings.Builder

	line(&b, "%s", generatedBy)
	line(&b, "")
	line(&b, "package %s", f.opts.packageName)
	line(&b, "")
	line(&b, "import (")

	for _, path := range imports {
		line(&b, "%q", path)
	}

	line(&b, ")")
	b.WriteString(body)

	return b.String(), nil
}

// admits reports whether any state offers a transition, which is whether this
// automaton consumes a record at all.
func (f *filer) admits() bool {
	for _, node := range f.states {
		if len(node.GetState().GetTransitionIds()) != 0 {
			return true
		}
	}

	return false
}

// collect indexes the nodes this file is emitted from: the file node, the
// states in ascending identifier order and the registers in the same order.
//
// Ascending identifier order rather than the order a walk from the start state
// reaches them, because it is the order the descriptor itself is in and it is a
// total order over every state whether the automaton reaches it or not. A walk
// would leave an unreachable state out of the switch and its records out of the
// writer, which turns a producer bug into generated code that silently cannot
// read a file.
func (f *filer) collect(d *irpb.Descriptor) error {
	for _, node := range d.GetNodes() {
		switch kind := node.GetKind().(type) {
		case *irpb.Node_File:
			if f.file != nil {
				return malformed("the descriptor carries two file nodes",
					"exactly one node of kind File exists and it is the root; see docs/ir/SPEC.md, \"A node set, not a tree\"")
			}

			f.file = kind.File
		case *irpb.Node_State:
			f.states = append(f.states, node)
		case *irpb.Node_Register:
			f.registers = append(f.registers, node)
		}
	}

	if f.file == nil {
		return nil
	}

	sort.Slice(f.states, func(i, j int) bool { return f.states[i].GetId() < f.states[j].GetId() })
	sort.Slice(f.registers, func(i, j int) bool { return f.registers[i].GetId() < f.registers[j].GetId() })

	for i, state := range f.states {
		f.index[state.GetId()] = i
	}

	if _, ok := f.index[f.file.GetStartStateId()]; !ok {
		return malformed(fmt.Sprintf("the file node begins in node %d, which is not a state node", f.file.GetStartStateId()),
			"a file node names the state a read begins in; see docs/ir/SPEC.md, \"The sequencing automaton\"")
	}

	return f.framing()
}

// framing reads the file node's framing into the form the emitter switches
// over, and refuses one it could not emit bytes for.
func (f *filer) framing() error {
	switch kind := f.file.GetFraming().(type) {
	case *irpb.File_Unframed:
		f.how = unframed
	case *irpb.File_DescriptorWord:
		f.how = descriptorWord
	case *irpb.File_Segmented:
		f.how = segmented
		f.maxSegment = kind.Segmented.GetMaxSegmentSize()

		// Four of those bytes are the segment descriptor word itself, so a
		// largest segment of four carries no data and a record would never
		// end.
		if f.maxSegment <= segmentDescriptorWidth {
			return malformed(fmt.Sprintf("the file is segmented and its largest segment is %d bytes", f.maxSegment),
				"a segment carries a four-byte segment descriptor word and the data behind it, so a largest segment of four or fewer carries no data at all")
		}
	case *irpb.File_Delimited:
		f.how = delimited
		f.delimiter = kind.Delimited.GetDelimiter()
		f.placement = kind.Delimited.GetPlacement()

		if len(f.delimiter) == 0 {
			return malformed("the file is delimited and its delimiter is no bytes at all",
				"a producer MUST NOT emit a delimiter of no bytes; see docs/ir/SPEC.md, \"A delimiter is bytes, not a character\"")
		}

		switch f.placement {
		case irpb.DelimiterPlacement_DELIMITER_PLACEMENT_TERMINATOR,
			irpb.DelimiterPlacement_DELIMITER_PLACEMENT_SEPARATOR,
			irpb.DelimiterPlacement_DELIMITER_PLACEMENT_OPTIONAL_TERMINATOR:
		default:
			return malformed("the file is delimited and says nothing about where its delimiter stands",
				"a delimited file node carries the placement beside the bytes, and it is what makes the end of a file checkable; see docs/ir/SPEC.md, \"Terminator, separator, and the last record\"")
		}
	default:
		return malformed("the file node carries no framing",
			"the set is closed and a file node carries one member of it; see docs/ir/SPEC.md, \"Four framings, and none of them is a RECFM\"")
	}

	return nil
}

// segmentDescriptorWidth is the width of the segment descriptor word DFSMS
// defines, and of the record descriptor word beside it. Both are four bytes and
// the IR carries neither, for the reason docs/ir/SPEC.md's "Lengths the file
// node does not carry" gives: the width comes with the definition, and a width
// carried beside it could hold a number describing no format anyone has.
const segmentDescriptorWidth = 4

// transition is one edge of the automaton as the generated code takes it.
type transition struct {
	// node is the transition node itself.
	node *irpb.Transition

	// record is the record node it admits, and typ is that record's Go type.
	record *irpb.Record
	typ    string

	// match is the Go expression testing its predicate against a window of
	// bytes, empty where the transition carries no predicate.
	match string

	// reads is how far into the record its predicate looks, in bytes.
	reads int

	// next is the position in [filer.states] of the state it moves to.
	next int
}

// transitionsOf resolves a state's transitions in the order the state carries
// them, which is the order both directions evaluate them in.
func (f *filer) transitionsOf(state *irpb.State) ([]transition, error) {
	out := make([]transition, 0, len(state.GetTransitionIds()))

	for _, id := range state.GetTransitionIds() {
		node, ok := f.nodes[id]
		if !ok {
			return nil, unresolved(id)
		}

		t := node.GetTransition()
		if t == nil {
			return nil, malformed(fmt.Sprintf("node %d leaves a state and is not a transition node", id),
				"a state's transition list names transition nodes; see docs/ir/SPEC.md, \"The node kinds\"")
		}

		recordNode, ok := f.nodes[t.GetRecordId()]
		if !ok {
			return nil, unresolved(t.GetRecordId())
		}

		record := recordNode.GetRecord()
		if record == nil {
			return nil, malformed(fmt.Sprintf("node %d is what a transition admits and is not a record node", t.GetRecordId()),
				"every transition consumes exactly one record; see docs/ir/SPEC.md, \"The sequencing automaton\"")
		}

		typ, err := recordName(record.GetNames())
		if err != nil {
			return nil, err
		}

		next, ok := f.index[t.GetNextStateId()]
		if !ok {
			return nil, malformed(fmt.Sprintf("a transition moves to node %d, which is not a state node", t.GetNextStateId()),
				"a transition names the state to move to; see docs/ir/SPEC.md, \"The sequencing automaton\"")
		}

		match, reads, err := f.predicate(t, record)
		if err != nil {
			return nil, err
		}

		if reads > f.lookahead {
			f.lookahead = reads
		}

		out = append(out, transition{node: t, record: record, typ: typ, match: match, reads: reads, next: next})
	}

	return out, nil
}

// predicate is the Go expression testing a transition's predicate against a
// window of the bytes in front of the reader, and how far into the record that
// window has to reach.
//
// The target's offset is a constant, and this is where a descriptor saying
// otherwise is refused: a target whose position moved with a count would oblige
// a consumer to decode that count out of bytes it has not identified yet, which
// is exactly the position step 3 of the read loop is in.
func (f *filer) predicate(t *irpb.Transition, record *irpb.Record) (string, int, error) {
	if t.PredicateId == nil {
		// A transition MAY carry no predicate, and one that does not matches
		// every record. It is not a fall-through: it is evaluated in the order
		// the state carries it like every other transition, and what it gives
		// up is detection rather than order. See docs/ir/SPEC.md, "A transition
		// may carry no predicate".
		return "", 0, nil
	}

	node, ok := f.nodes[t.GetPredicateId()]
	if !ok {
		return "", 0, unresolved(t.GetPredicateId())
	}

	predicate := node.GetPredicate()
	if predicate == nil {
		return "", 0, malformed(fmt.Sprintf("node %d selects a transition and is not a predicate node", t.GetPredicateId()),
			"a transition names the predicate that selects it where it carries one; see docs/ir/SPEC.md, \"Discriminator predicates\"")
	}

	targetNode, ok := f.nodes[predicate.GetFieldId()]
	if !ok {
		return "", 0, unresolved(predicate.GetFieldId())
	}

	field := targetNode.GetField()
	if field == nil {
		return "", 0, malformed(fmt.Sprintf("node %d is a predicate's target and is not a field node", predicate.GetFieldId()),
			"a predicate always names a field; see docs/ir/SPEC.md, \"A predicate always names a field\"")
	}

	if field.GetRepetition() != nil {
		return "", 0, malformed(fmt.Sprintf("%s is the target of a transition's predicate and repeats", originalOf(targetNode)),
			"a predicate names a field, not an occurrence of one; see docs/ir/SPEC.md, \"A reference names a field, not an occurrence of one\"")
	}

	c := &coder{emitter: f.emitter}

	at, found, err := c.offsetOf(record.GetRootId(), predicate.GetFieldId(), "", encoding)
	if err != nil {
		return "", 0, err
	}

	if !found {
		return "", 0, malformed(fmt.Sprintf("%s is the target of a transition's predicate and is not in the record that transition admits", originalOf(targetNode)),
			"a producer MUST ensure the target is contained in the record the referring transition admits, at any depth; see docs/ir/SPEC.md, \"A predicate always names a field\"")
	}

	if len(at.terms) != 0 {
		return "", 0, malformed(fmt.Sprintf("%s is the target of a transition's predicate and sits behind a table whose length is data", originalOf(targetNode)),
			"a predicate's target MUST have a constant position within the record, since a consumer evaluates it before it has identified the record; see docs/ir/SPEC.md, \"A predicate never reads past the record in front of it\"")
	}

	end := at.fixed + int(field.GetWidth())
	slice := fmt.Sprintf("b[%d:%d]", at.fixed, end)

	switch test := predicate.GetTest().(type) {
	case *irpb.Predicate_BytesEqual:
		return fmt.Sprintf("bytes.Equal(%s, []byte(%s))", slice, strconv.Quote(string(test.BytesEqual.GetValue()))), end, nil
	case *irpb.Predicate_BytesOneOf:
		if len(test.BytesOneOf.GetValues()) < 2 {
			return "", 0, malformed("a one-of predicate carries fewer than two literals",
				"a producer MUST carry at least two and MUST NOT carry the same literal twice; see docs/ir/SPEC.md, \"Discriminator predicates\"")
		}

		tests := make([]string, 0, len(test.BytesOneOf.GetValues()))

		for _, value := range test.BytesOneOf.GetValues() {
			tests = append(tests, fmt.Sprintf("bytes.Equal(%s, []byte(%s))", slice, strconv.Quote(string(value))))
		}

		return "(" + strings.Join(tests, " || ") + ")", end, nil
	default:
		return "", 0, malformed("a predicate carries no test",
			"the set is closed and a predicate carries one member of it; see docs/ir/SPEC.md, \"Discriminator predicates\"")
	}
}

// register is the field the generated reader and writer hold a register in, and
// held is the companion recording whether anything has bound it.
//
// Two fields rather than a pointer or a sentinel, because docs/ir/SPEC.md makes
// reading a register nothing has bound a malformed descriptor rather than a
// zero: a consumer MUST NOT supply a zero, an empty byte string or the value of
// any other register, so the unbound state has to be one the generated code can
// see.
func register(id uint64) string { return fmt.Sprintf("register%d", id) }

func held(id uint64) string { return fmt.Sprintf("register%dBound", id) }

// registerKind is the Go type a register holds, and the phrase a diagnostic
// names its kind with.
func (f *filer) registerKind(id uint64) (string, error) {
	node, ok := f.nodes[id]
	if !ok {
		return "", unresolved(id)
	}

	r := node.GetRegister()
	if r == nil {
		return "", malformed(fmt.Sprintf("node %d is a register a binding or a guard names and is not a register node", id),
			"a binding names the register it writes and a guard the register it reads; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
	}

	switch r.GetKind() {
	case irpb.RegisterKind_REGISTER_KIND_BYTES:
		return "[]byte", nil
	case irpb.RegisterKind_REGISTER_KIND_INTEGER:
		return "int64", nil
	default:
		return "", malformed(fmt.Sprintf("register node %d says nothing about what it holds", id),
			"a register's kind is bytes or an integer; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
	}
}
