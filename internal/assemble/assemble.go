// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package assemble

import (
	"math"

	"github.com/Zaba505/cobol-go/copybook"
	"google.golang.org/protobuf/proto"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
	"github.com/Zaba505/cpybkc/internal/resolve"
	"github.com/Zaba505/cpybkc/irpb"
)

// Version is the IR version this build produces: the value of the Version field
// on every descriptor [Assemble] returns.
//
// It is exported because it is a fact about the build rather than about this
// package: docs/cli/SPEC.md has `cpybkc --version` name "the IR version this
// build produces", and the only honest source for that is the constant the
// assembler stamps into the descriptor. A second constant beside the CLI would
// be a second statement of one fact, and the day the two disagreed the version
// line would be the one that lied.
//
// docs/ir/SPEC.md makes the version a single monotonic integer: an addition a
// consumer can ignore and still be correct about leaves it alone, and every
// addition it cannot advances it.
const Version = irpb.IrVersion_IR_VERSION_1

// Options are the resolved layout: everything a descriptor is assembled out of,
// and nothing that still needs resolving.
//
// Every member arrives already resolved against its copybook, for the reason
// [github.com/Zaba505/cpybkc/internal/resolve.Redefine] does: reading a layout
// is one step and resolving a copybook against it is another, and a package
// doing both would read a layout's spelling of an item in two places.
type Options struct {
	// Framing is the layout's physical framing, and is required: the file
	// node carries one member of a closed set of four, and there is no member
	// standing for a framing nobody stated.
	Framing *layoutmodel.Framing

	// Automaton is the compiled sequencing expression, and is required: the
	// file node names its start state.
	Automaton *resolve.Automaton

	// Records are the record types the automaton's transitions admit, in the
	// order their record nodes are to be assigned identifiers.
	//
	// One entry per record type and not per `record` form. A copybook holding
	// a REDEFINES outside a repeating group resolves to one
	// [github.com/Zaba505/cpybkc/internal/resolve.Record] per combination of
	// alternatives, each of which is its own record type with its own
	// discriminator (docs/ir/SPEC.md, "Members never overlap, and `REDEFINES`
	// is resolved away"), and which of them a given transition admits is a
	// question about the layout rather than about the resolved records. So the
	// pairing arrives made: a caller hands over one entry per name a
	// transition may admit, and this package neither splits nor merges them.
	Records []Record

	// Renames are the renames the layout wrote, resolved to the copybook items
	// they name.
	//
	// A rename substitutes a name and keeps the original, so what one
	// contributes is the override beside the name the copybook gave the item
	// (docs/ir/SPEC.md, "Names"). It reaches every node standing for that item,
	// in every record type that holds it: a rename is a name, and a name is per
	// item rather than per record.
	Renames []Rename
}

// Record is one record type of the descriptor.
type Record struct {
	// Name is the name a transition admits this record type by: the name the
	// layout's `record` form defines, or whatever a caller distinguishing two
	// alternatives of one copybook chose to call each.
	//
	// It does not reach the descriptor. A record node's names are the
	// copybook's, because docs/ir/SPEC.md's "Names" carries the name a
	// copybook spells and a layout-side binding is not one.
	Name string

	// Copybook is the copybook's path as the layout spells it, carried so
	// that a diagnostic about an item can name the file the fault is in.
	Copybook string

	// Resolved is the record itself.
	Resolved *resolve.Record
}

// Rename is one `rename` form, resolved.
type Rename struct {
	// Item is the copybook item renamed.
	Item *copybook.Field

	// Substitute is the name carried beside the original, exactly as the
	// layout wrote it. It is language-neutral and is not munged here: casing,
	// reserved words and what to do about a name that is not an identifier in
	// some target language are a generator's.
	Substitute string
}

// Assemble turns a resolved layout into the descriptor a generator plugin
// consumes.
//
// Identifiers are assigned by the traversal the package documentation states,
// the node list comes back in ascending identifier order, and identical inputs
// produce an identical descriptor — which
// [github.com/Zaba505/cpybkc/internal/emit.Marshal] turns into identical bytes
// (docs/ir/SPEC.md, "Identity, ordering and determinism", #38).
//
// The result is held to [Validate] before it is handed back, so nothing leaves
// here that a generator would have been the first to find wrong. A descriptor
// that failed either pass is not returned at all: a half-assembled one is a
// descriptor a caller can read, and there is no reading of a layout that was
// rejected.
//
// Every fault it finds is reported, joined with [errors.Join] and assertable
// with [errors.As], rather than the first.
func Assemble(opts Options) (*irpb.Descriptor, error) {
	switch {
	case opts.Framing == nil:
		return nil, ErrNoFraming
	case opts.Automaton == nil:
		return nil, ErrNoAutomaton
	case len(opts.Records) == 0:
		return nil, ErrNoRecords
	}

	a := &assembler{
		opts:       opts,
		byName:     make(map[string]*scope, len(opts.Records)),
		renames:    make(map[*copybook.Field]string, len(opts.Renames)),
		registers:  make(map[*resolve.Register]uint64),
		states:     make(map[*resolve.State]uint64),
		predicates: make(map[predicateKey]uint64),
	}

	for _, rename := range opts.Renames {
		if rename.Item != nil {
			a.renames[rename.Item] = rename.Substitute
		}
	}

	descriptor := a.assemble()
	if a.faults.Failed() {
		return nil, a.faults.Err()
	}

	if err := Validate(descriptor); err != nil {
		return nil, err
	}

	return descriptor, nil
}

// assembler holds the state one [Assemble] accumulates.
type assembler struct {
	opts   Options
	faults diag.List

	// nodes is the node list under construction. A node's identifier is its
	// index in it, which is what makes the list ascending by construction
	// rather than by a sort this package could forget to run.
	nodes []*irpb.Node

	// byName is the record types under the name a transition admits each by.
	byName map[string]*scope

	renames    map[*copybook.Field]string
	registers  map[*resolve.Register]uint64
	states     map[*resolve.State]uint64
	predicates map[predicateKey]uint64

	// pending are the predicate nodes whose bodies are filled once every
	// record's field nodes have identifiers. A predicate names a field of the
	// record it reads, and an arm's predicate is met while that record is
	// still being walked.
	pending []pendingPredicate
}

// scope is one record type while it is being assembled: the identifiers its
// nodes were given, and the copybook item each field node stands for.
//
// A field lookup is per record type and never across the descriptor, because
// two record types resolved from one copybook hold two field nodes for one
// copybook item — which is the duplication docs/ir/SPEC.md's "Members never
// overlap" accepts, seen from the side that has to resolve a reference through
// it.
type scope struct {
	name     string
	copybook string

	// record is the identifier of the record node itself, which is what a
	// transition admitting this record type points at.
	record uint64

	// nodes maps each resolved node to the identifier it was given, and
	// fields maps the copybook item behind each *field* node to the same.
	// Only field nodes are in fields: every reference that names an item
	// names an elementary one.
	nodes  map[*resolve.Node]uint64
	fields map[*copybook.Field]uint64
}

// predicateKey is what makes two references to one predicate the same node: the
// record whose bytes it reads, and the compiled predicate itself.
//
// The record is half of the key rather than the predicate alone, because a
// predicate names a field and a field node belongs to one record type. `resolve`
// compiles a record's discriminator once and hands the same node to every
// appearance of that record in the sequencing expression, so within one record
// the pointer is identity; across two it would be a reference into the wrong
// record's fields.
type predicateKey struct {
	record    *scope
	predicate *resolve.Predicate
}

// pendingPredicate is a predicate node awaiting its body.
type pendingPredicate struct {
	scope     *scope
	predicate *resolve.Predicate
	node      *irpb.Node
}

// reserve appends a node with the next identifier and returns it. The body is
// set by whoever reserved it, which is what lets a reference point forward.
func (a *assembler) reserve() *irpb.Node {
	node := &irpb.Node{Id: uint64(len(a.nodes))}
	a.nodes = append(a.nodes, node)

	return node
}

// assemble runs the traversal the package documentation states.
func (a *assembler) assemble() *irpb.Descriptor {
	file := a.reserve()

	for _, record := range a.opts.Records {
		a.record(record)
	}

	a.allocateRegisters()
	a.allocateStates()
	a.fillStates()
	a.fillPredicates()

	file.Kind = &irpb.Node_File{File: fileOf(a.opts.Framing, a.states[a.opts.Automaton.Start])}

	return &irpb.Descriptor{Version: Version, Nodes: a.nodes}
}

// record assembles one record type: the record node, then its tree.
//
// The tree is walked twice. The first walk hands out identifiers and records
// which copybook item each field node stands for; the second fills the bodies,
// by which time every reference into the record has an identifier to state. Two
// walks rather than one because a reference need not point backwards: an
// `OCCURS DEPENDING ON` count lies ahead of the table it sizes, but nothing in
// this package should depend on that being true, and the rule that makes it
// true is stated somewhere else entirely (docs/ir/SPEC.md, "A count is in hand
// before the extent it decides").
func (a *assembler) record(record Record) {
	if record.Resolved == nil || record.Resolved.Root == nil {
		a.faults.Fail(&UnknownRecordError{Record: record.Name, Defined: a.recordNames()})

		return
	}

	if _, already := a.byName[record.Name]; already {
		a.faults.Fail(&DuplicateRecordError{Record: record.Name, Copybook: record.Copybook})

		return
	}

	node := a.reserve()
	s := &scope{
		name:     record.Name,
		copybook: record.Copybook,
		record:   node.Id,
		nodes:    make(map[*resolve.Node]uint64),
		fields:   make(map[*copybook.Field]uint64),
	}
	a.byName[record.Name] = s

	a.allocate(s, record.Resolved.Root)
	a.fill(s, record.Resolved.Root)

	node.Kind = &irpb.Node_Record{Record: &irpb.Record{
		RootId: s.nodes[record.Resolved.Root],
		Names:  a.names(record.Resolved.Item),
	}}
}

// allocate hands an identifier to every node of a record's tree, in containment
// order, an arm's predicate ahead of the arm's body.
func (a *assembler) allocate(s *scope, node *resolve.Node) {
	s.nodes[node] = a.reserve().Id

	if node.Kind == resolve.KindField && node.Field != nil {
		s.fields[node.Field] = s.nodes[node]
	}

	for _, member := range node.Members {
		a.allocate(s, member)
	}

	for _, arm := range node.Arms {
		a.allocatePredicate(s, arm.Predicate)

		if arm.Body != nil {
			a.allocate(s, arm.Body)
		}
	}
}

// fill sets the body of every node of a record's tree.
func (a *assembler) fill(s *scope, node *resolve.Node) {
	at := a.nodes[s.nodes[node]]

	switch node.Kind {
	case resolve.KindGroup:
		members := make([]uint64, 0, len(node.Members))
		for _, member := range node.Members {
			a.fill(s, member)
			members = append(members, s.nodes[member])
		}

		at.Kind = &irpb.Node_Group{Group: &irpb.Group{
			MemberIds:  members,
			Names:      a.names(node.Field),
			Repetition: a.repetition(s, node.Repetition),
		}}
	case resolve.KindField:
		at.Kind = &irpb.Node_Field{Field: &irpb.Field{
			Width:      width(node.Width()),
			Encoding:   encodingOf(node.Encoding),
			Usage:      usageOf(node.Field),
			Picture:    pictureOf(node.Field),
			Names:      a.names(node.Field),
			Repetition: a.repetition(s, node.Repetition),
		}}
	case resolve.KindVariant:
		arms := make([]*irpb.Arm, 0, len(node.Arms))
		for _, arm := range node.Arms {
			arms = append(arms, a.arm(s, arm))
		}

		at.Kind = &irpb.Node_Variant{Variant: &irpb.Variant{Arms: arms}}
	case resolve.KindSlack:
		at.Kind = &irpb.Node_Slack{Slack: &irpb.Slack{Width: width(node.Width())}}
	}
}

// arm fills one arm of a variant: the predicate that selects it, and the group
// or field that is its body.
//
// The body reference says which of the two kinds it is rather than leaving a
// consumer to dereference an untyped identifier and find out, which is what the
// schema's oneof is for.
func (a *assembler) arm(s *scope, arm resolve.Arm) *irpb.Arm {
	built := &irpb.Arm{PredicateId: a.allocatePredicate(s, arm.Predicate)}

	if arm.Body == nil {
		return built
	}

	a.fill(s, arm.Body)

	id := s.nodes[arm.Body]
	if arm.Body.Kind == resolve.KindField {
		built.Body = &irpb.Arm_FieldId{FieldId: id}
	} else {
		built.Body = &irpb.Arm_GroupId{GroupId: id}
	}

	return built
}

// repetition is what an item that repeats carries, and nil for one that does
// not.
//
// A count that is a reference names a field of the record being read. It never
// names a register here: a count coming from a record admitted earlier is
// written `times` in a layout and compiles into the automaton's own memory
// rather than into a repetition (docs/ir/SPEC.md, "A variable record is a sum
// with a variable term"), so the register member of the schema's count is one
// this producer has nothing to put in yet.
func (a *assembler) repetition(s *scope, repetition *resolve.Repetition) *irpb.Repetition {
	if repetition == nil {
		return nil
	}

	if !repetition.Reference() {
		return &irpb.Repetition{Count: &irpb.Repetition_Constant{Constant: width(repetition.Count)}}
	}

	id, found := s.fields[repetition.DependingOn]
	if !found {
		a.faults.Fail(&UnresolvedTargetError{
			Pos:      a.span(s, repetition.DependingOn),
			Record:   s.name,
			Item:     itemName(repetition.DependingOn),
			Position: "the DEPENDING ON count of a table",
		})

		return nil
	}

	return &irpb.Repetition{Count: &irpb.Repetition_Variable{Variable: &irpb.VariableCount{
		Count:          &irpb.VariableCount_FieldId{FieldId: id},
		MinOccurrences: width(repetition.Min),
		MaxOccurrences: width(repetition.Max),
	}}}
}

// names is what a named node carries: the copybook's own name, and the
// substitute a rename asked for beside it.
//
// A node standing for no copybook item carries none, and neither does one
// standing for a FILLER: an item COBOL gives no data-name has no original for a
// substitute to stand beside, and docs/ir/SPEC.md's "Names" makes the original
// the member that **MUST** be present.
func (a *assembler) names(field *copybook.Field) *irpb.Names {
	if field == nil || field.Filler || field.Name == "" {
		return nil
	}

	names := &irpb.Names{Original: field.Name}
	if substitute, renamed := a.renames[field]; renamed {
		names.OverrideName = proto.String(substitute)
	}

	return names
}

// recordNames is every record type handed over, in the order they were, which is
// what a diagnostic about a name nothing defines lists.
func (a *assembler) recordNames() []string {
	found := make([]string, 0, len(a.opts.Records))
	for _, record := range a.opts.Records {
		found = append(found, record.Name)
	}

	return found
}

// span is where in a record's copybook an item is.
//
// A nil item has no line, and what comes back for one is the copybook itself,
// which diag renders as the file alone rather than as a line zero nothing can
// jump to.
func (a *assembler) span(s *scope, field *copybook.Field) diag.Span {
	span := diag.Span{}
	if s != nil {
		span.File = s.copybook
	}

	if field != nil {
		span.Line, span.Column = field.Pos.Line, field.Pos.Column
	}

	return span
}

// width narrows a count this repository holds as an int to the unsigned width
// the schema carries it in.
//
// Nothing that reaches here is negative or anywhere near four billion — a
// record's widths are bounded by its `lrecl` and an OCCURS count by the
// copybook — so both ends are clamped rather than reported: a number outside
// them is a defect upstream rather than something an adopter wrote, and what
// comes back is a width [Validate] then refuses on its own terms.
func width(n int) uint32 {
	switch {
	case n < 0:
		return 0
	case n > math.MaxUint32:
		return math.MaxUint32
	}

	return uint32(n)
}

// itemName is what a diagnostic calls a copybook item.
func itemName(field *copybook.Field) string {
	if field == nil {
		return "an item that is not there"
	}

	if field.Filler || field.Name == "" {
		return "a FILLER item"
	}

	return field.Name
}
