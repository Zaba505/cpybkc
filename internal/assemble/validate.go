// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package assemble

import (
	"encoding/hex"
	"fmt"
	"slices"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/irpb"
)

// Validate holds a descriptor to what docs/ir/SPEC.md requires of one before a
// generator is invoked, and reports everything it finds wrong.
//
// [Assemble] runs it over its own output, so a descriptor this repository
// produced has already passed. It is exported for the other caller: a command
// about to hand a descriptor to a plugin, which is where "before any generator
// is invoked" is a statement about a process rather than about a function (#38).
//
// # What it checks
//
// Everything a consumer is entitled to assume and cannot check for itself
// without failing halfway through a walk:
//
//   - The version is set, and names a release. A descriptor whose version is
//     zero is a descriptor whose producer did not set one.
//   - Exactly one file node exists, identifiers are unique, and the node list is
//     in ascending identifier order.
//   - Every reference resolves to a node in the same message, of a kind the
//     referring position admits.
//   - Every node is reachable from the file node, and every item is named by
//     exactly one member list, one arm, or one record's top level — which is
//     also what makes containment acyclic.
//   - No closed set carries its unspecified zero: not an encoding axis, not a
//     USAGE, not a register's kind, not a delimiter's placement.
//   - Every field states all four encoding axes. docs/ir/SPEC.md makes this a
//     requirement on the producer and a refusal on the consumer, because each
//     of the four fails silently when wrong: a charset yields a plausible
//     string and a byte order a plausible number, with nothing in the file to
//     disagree.
//
// # What it does not check
//
// Anything that is arithmetic on the message rather than a property of it. A
// group's width, a field's position, whether two arms of a variant come to the
// same extent: a consumer computes each of those from what it was handed, and a
// second computation here would be a second answer to a question the descriptor
// states once (docs/ir/SPEC.md, "Dereferencing is not recomputation").
//
// It also does not check the COBOL. Whether a width follows from a PICTURE and
// whether an automaton follows from a sequencing expression are `resolve`'s, and
// a pass re-deriving either would be the second reading of that knowledge the
// whole project exists to avoid.
//
// Every fault is reported, joined with [errors.Join] and assertable with
// [errors.As], rather than the first: a descriptor assembled wrong is wrong in
// the same way in many places at once.
func Validate(d *irpb.Descriptor) error {
	if d == nil {
		return &DescriptorError{Fault: "is not there"}
	}

	v := &validator{nodes: make(map[uint64]*irpb.Node, len(d.GetNodes()))}

	v.version(d)
	v.index(d)
	v.bodies()
	v.containment()

	// Reachability is a statement about a descriptor whose references all
	// resolve. Over one whose references do not, every node the broken
	// reference stood in front of is unreachable *because* of it, and
	// reporting a dozen consequences beside the cause buries the cause. So it
	// runs last and only over a descriptor nothing else was wrong with.
	if !v.faults.Failed() {
		v.reachability()
	}

	return v.faults.Err()
}

// validator holds the state one [Validate] accumulates.
type validator struct {
	faults diag.List

	// nodes is the node list indexed by identifier, which is the index every
	// consumer builds before it can walk anything.
	nodes map[uint64]*irpb.Node

	// order is the identifiers in the order the list carried them, so that
	// every pass runs in a fixed order and reports its faults in one.
	order []uint64

	// root is the file node's identifier, and rooted whether there is exactly
	// one to have.
	root   uint64
	rooted bool

	// edges are the references out of each node, which is what reachability
	// walks. contained counts the references that are *containment* — a member
	// list, an arm's body, a record's top level — which is a stricter rule.
	edges     map[uint64][]uint64
	contained map[uint64]int
}

// version holds the descriptor to a version a release names.
func (v *validator) version(d *irpb.Descriptor) {
	version := d.GetVersion()
	if version == irpb.IrVersion_IR_VERSION_UNSPECIFIED {
		v.faults.Fail(&DescriptorError{Fault: "states no version, so nothing says which contract it was written against"})

		return
	}

	if _, named := irpb.IrVersion_name[int32(version)]; !named {
		v.faults.Fail(&DescriptorError{
			Fault: fmt.Sprintf("states version %d, which no release of the IR names", version),
		})
	}
}

// index builds the identifier index, and holds the node list to being unique,
// ascending, and rooted at exactly one file node.
func (v *validator) index(d *irpb.Descriptor) {
	v.edges = make(map[uint64][]uint64, len(d.GetNodes()))
	v.contained = make(map[uint64]int, len(d.GetNodes()))

	var previous uint64
	var files []uint64

	for at, node := range d.GetNodes() {
		if node == nil {
			v.faults.Fail(&DescriptorError{Fault: fmt.Sprintf("carries nothing at position %d of its node list", at)})

			continue
		}

		if _, already := v.nodes[node.GetId()]; already {
			v.faults.Fail(&NodeError{
				ID:    node.GetId(),
				Kind:  kindOf(node),
				Fault: "shares its identifier with another node, and an identifier is identity",
			})

			continue
		}

		if at > 0 && node.GetId() <= previous {
			v.faults.Fail(&NodeError{
				ID:   node.GetId(),
				Kind: kindOf(node),
				Fault: fmt.Sprintf(
					"stands behind node %d in the node list, which docs/ir/SPEC.md requires to be in ascending identifier order",
					previous),
			})
		}

		previous = node.GetId()
		v.nodes[node.GetId()] = node
		v.order = append(v.order, node.GetId())

		if node.GetFile() != nil {
			files = append(files, node.GetId())
		}
	}

	switch len(files) {
	case 1:
		v.root, v.rooted = files[0], true
	case 0:
		v.faults.Fail(&DescriptorError{Fault: "carries no file node, and the file node is the root everything hangs off"})
	default:
		v.faults.Fail(&DescriptorError{
			Fault: fmt.Sprintf("carries %d file nodes, and exactly one exists", len(files)),
		})
	}
}

// bodies holds every node to what its own kind requires of it.
func (v *validator) bodies() {
	for _, id := range v.order {
		node := v.nodes[id]

		switch body := node.GetKind().(type) {
		case *irpb.Node_File:
			v.file(id, body.File)
		case *irpb.Node_Record:
			v.record(id, body.Record)
		case *irpb.Node_Group:
			v.group(id, body.Group)
		case *irpb.Node_Variant:
			v.variant(id, body.Variant)
		case *irpb.Node_Field:
			v.field(id, body.Field)
		case *irpb.Node_Slack:
			v.slack(id, body.Slack)
		case *irpb.Node_Predicate:
			v.predicate(id, body.Predicate)
		case *irpb.Node_State:
			v.state(id, body.State)
		case *irpb.Node_Transition:
			v.transition(id, body.Transition)
		case *irpb.Node_Register:
			v.register(id, body.Register)
		case *irpb.Node_Binding:
			v.binding(id, body.Binding)
		case *irpb.Node_Guard:
			v.guard(id, body.Guard)
		default:
			v.faults.Fail(&NodeError{
				ID:    id,
				Kind:  "kindless",
				Fault: "carries no body, and every node is one member of the closed set of twelve kinds",
			})
		}
	}
}

// reference resolves one reference and records the edge, faulting where it names
// no node or a node of a kind the position does not admit.
func (v *validator) reference(from uint64, position string, to uint64, admits ...string) {
	v.edges[from] = append(v.edges[from], to)

	target, found := v.nodes[to]
	if !found {
		v.fault(from, "%s names node %d, which is not in this descriptor", position, to)

		return
	}

	if kind := kindOf(target); !slices.Contains(admits, kind) {
		v.fault(from, "%s names node %d, a %s node, and admits only %s", position, to, kind, list(admits))
	}
}

// contain records a reference that is also containment, which is under the
// stricter rule: exactly one member list, arm or record top level names an item.
func (v *validator) contain(from uint64, position string, to uint64, admits ...string) {
	v.reference(from, position, to, admits...)
	v.contained[to]++
}

// fault records one thing wrong with one node.
func (v *validator) fault(id uint64, format string, args ...any) {
	kind := "kindless"
	if node, found := v.nodes[id]; found {
		kind = kindOf(node)
	}

	v.faults.Fail(&NodeError{ID: id, Kind: kind, Fault: fmt.Sprintf(format, args...)})
}

func (v *validator) file(id uint64, file *irpb.File) {
	switch framing := file.GetFraming().(type) {
	case *irpb.File_Delimited:
		if len(framing.Delimited.GetDelimiter()) == 0 {
			v.fault(id, "states a delimited dataset whose delimiter is no bytes at all")
		}

		if framing.Delimited.GetPlacement() == irpb.DelimiterPlacement_DELIMITER_PLACEMENT_UNSPECIFIED {
			v.fault(id, "states a delimited dataset and does not say where the delimiter stands")
		}
	case nil:
		v.fault(id, "states no framing, and a framing is one member of a closed set of four")
	}

	v.reference(id, "the start state", file.GetStartStateId(), "state")
}

func (v *validator) record(id uint64, record *irpb.Record) {
	v.names(id, record.GetNames(), true)
	v.contain(id, "the record's top level", record.GetRootId(), "group")

	// Only where the top level really is a group. A root naming some other
	// kind is already reported by the reference above, and an empty member
	// list read off a node that has none would report that fault a second time
	// under a description that is not what is wrong with it.
	if top, found := v.nodes[record.GetRootId()]; found && top.GetGroup() != nil &&
		len(top.GetGroup().GetMemberIds()) == 0 {
		v.fault(id, "holds no items, and no transition may admit a record whose extent is zero")
	}
}

func (v *validator) group(id uint64, group *irpb.Group) {
	v.names(id, group.GetNames(), false)

	for at, member := range group.GetMemberIds() {
		v.contain(id, fmt.Sprintf("member %d", at+1), member, "group", "variant", "field", "slack")
	}

	v.repetition(id, group.GetRepetition())
}

func (v *validator) variant(id uint64, variant *irpb.Variant) {
	if len(variant.GetArms()) == 0 {
		v.fault(id, "carries no arms, and an alternation over nothing is not a choice a consumer can make")
	}

	for at, arm := range variant.GetArms() {
		where := fmt.Sprintf("arm %d", at+1)

		v.reference(id, where+"'s predicate", arm.GetPredicateId(), "predicate")

		switch body := arm.GetBody().(type) {
		case *irpb.Arm_GroupId:
			v.contain(id, where+"'s body", body.GroupId, "group")
		case *irpb.Arm_FieldId:
			v.contain(id, where+"'s body", body.FieldId, "field")
		case nil:
			v.fault(id, "%s names no body", where)
		}
	}
}

func (v *validator) field(id uint64, field *irpb.Field) {
	v.names(id, field.GetNames(), false)

	if field.GetWidth() == 0 {
		v.fault(id, "occupies no bytes, and an elementary item occupies at least one")
	}

	if missing := unresolved(field.GetEncoding()); len(missing) > 0 {
		v.fault(id, "states no %s, and all four encoding axes are stated on every field", list(missing))
	}

	if field.GetUsage() == irpb.Usage_USAGE_UNSPECIFIED {
		v.fault(id, "states no USAGE")
	}

	if picture := field.GetPicture(); picture != nil && picture.GetCategory() == irpb.Category_CATEGORY_UNSPECIFIED {
		v.fault(id, "carries a PICTURE of no category")
	}

	v.repetition(id, field.GetRepetition())
}

func (v *validator) slack(id uint64, slack *irpb.Slack) {
	if slack.GetWidth() == 0 {
		v.fault(id, "covers no bytes, and slack is one node per maximal run of them")
	}
}

func (v *validator) predicate(id uint64, predicate *irpb.Predicate) {
	v.reference(id, "the field it tests", predicate.GetFieldId(), "field")

	switch test := predicate.GetTest().(type) {
	case *irpb.Predicate_BytesEqual:
		if len(test.BytesEqual.GetValue()) == 0 {
			v.fault(id, "compares against no bytes at all")
		}
	case *irpb.Predicate_BytesOneOf:
		values := test.BytesOneOf.GetValues()
		if len(values) < 2 {
			v.fault(id, "offers %d literals, and a one-of test carries at least two", len(values))
		}

		if duplicate, twice := repeated(values); twice {
			v.fault(id, "carries the literal %s twice, which is a value tested twice", quoteBytes(duplicate))
		}
	case nil:
		v.fault(id, "carries no test, and a predicate testing nothing is the absence of one")
	}
}

func (v *validator) state(id uint64, state *irpb.State) {
	if !state.GetAccepts() && len(state.GetAcceptanceGuardIds()) > 0 {
		v.fault(id, "does not accept and still qualifies its acceptance with guards")
	}

	for at, guard := range state.GetAcceptanceGuardIds() {
		v.reference(id, fmt.Sprintf("acceptance guard %d", at+1), guard, "guard")
	}

	for at, transition := range state.GetTransitionIds() {
		v.reference(id, fmt.Sprintf("transition %d", at+1), transition, "transition")
	}
}

func (v *validator) transition(id uint64, transition *irpb.Transition) {
	v.reference(id, "the record it admits", transition.GetRecordId(), "record")
	v.reference(id, "the state it moves to", transition.GetNextStateId(), "state")

	if transition.PredicateId != nil {
		v.reference(id, "the predicate selecting it", transition.GetPredicateId(), "predicate")
	}

	for at, guard := range transition.GetGuardIds() {
		v.reference(id, fmt.Sprintf("guard %d", at+1), guard, "guard")
	}

	var written []uint64

	for at, binding := range transition.GetBindingIds() {
		v.reference(id, fmt.Sprintf("binding %d", at+1), binding, "binding")

		node, found := v.nodes[binding]
		if !found || node.GetBinding() == nil {
			continue
		}

		register := node.GetBinding().GetRegisterId()
		if slices.Contains(written, register) {
			v.fault(id, "applies two bindings writing register node %d, and nothing orders them", register)
		}

		written = append(written, register)
	}
}

func (v *validator) register(id uint64, register *irpb.Register) {
	if register.GetKind() == irpb.RegisterKind_REGISTER_KIND_UNSPECIFIED {
		v.fault(id, "does not say what it holds")
	}
}

func (v *validator) binding(id uint64, binding *irpb.Binding) {
	v.reference(id, "the register it writes", binding.GetRegisterId(), "register")

	switch value := binding.GetValue().(type) {
	case *irpb.Binding_FieldId:
		v.reference(id, "the field it reads", value.FieldId, "field")
	case nil:
		v.fault(id, "writes no value, and every value in a register was put there by a binding naming where it came from")
	}
}

// guard holds a guard to reading a register the descriptor carries, and to a
// test that agrees with what that register holds.
//
// The agreement is checked here rather than left to a consumer, because a
// mismatch is a comparison nothing can run: a bytes literal against a register
// holding a number has no reading at all, and *greater than zero* has none
// against a register holding bytes.
func (v *validator) guard(id uint64, guard *irpb.Guard) {
	v.reference(id, "the register it tests", guard.GetRegisterId(), "register")

	// The unspecified kind stands for "not known here", which covers the
	// register naming no node and the register that does not say what it
	// holds. Both are already reported against the node they are wrong on, and
	// a second fault about the literals that read them would be that fault
	// again under a description of something else.
	held := irpb.RegisterKind_REGISTER_KIND_UNSPECIFIED
	if register, found := v.nodes[guard.GetRegisterId()]; found {
		held = register.GetRegister().GetKind()
	}

	switch test := guard.GetTest().(type) {
	case *irpb.Guard_Equals:
		v.literal(id, "the literal it compares against", test.Equals, held)
	case *irpb.Guard_OneOf:
		values := test.OneOf.GetValues()
		if len(values) == 0 {
			v.fault(id, "compares against an empty set")
		}

		for at, value := range values {
			v.literal(id, fmt.Sprintf("literal %d", at+1), value, held)
		}
	case *irpb.Guard_GreaterThanZero:
		if held == irpb.RegisterKind_REGISTER_KIND_BYTES {
			v.fault(id, "tests a register holding bytes for being greater than zero, and only an integer is a number")
		}
	case nil:
		v.fault(id, "carries no test, and the set of three has no member testing nothing")
	}
}

// literal holds a guard's literal to carrying a value, and to carrying the one
// the register it is compared against holds.
func (v *validator) literal(id uint64, position string, literal *irpb.Literal, held irpb.RegisterKind) {
	switch literal.GetValue().(type) {
	case *irpb.Literal_BytesValue:
		if held == irpb.RegisterKind_REGISTER_KIND_INTEGER {
			v.fault(id, "%s carries bytes, and the register it is compared against holds an integer", position)
		}
	case *irpb.Literal_Integer:
		if held == irpb.RegisterKind_REGISTER_KIND_BYTES {
			v.fault(id, "%s carries a number, and the register it is compared against holds bytes", position)
		}
	default:
		v.fault(id, "%s carries no value", position)
	}
}

// repetition holds an item's repetition to naming a count.
func (v *validator) repetition(id uint64, repetition *irpb.Repetition) {
	if repetition == nil {
		return
	}

	switch count := repetition.GetCount().(type) {
	case *irpb.Repetition_Constant:
		if count.Constant == 0 {
			v.fault(id, "repeats zero times, and an item that does not repeat carries no repetition")
		}
	case *irpb.Repetition_Variable:
		variable := count.Variable

		switch where := variable.GetCount().(type) {
		case *irpb.VariableCount_FieldId:
			v.reference(id, "the count of its occurrences", where.FieldId, "field")
		case *irpb.VariableCount_RegisterId:
			v.reference(id, "the count of its occurrences", where.RegisterId, "register")
		case nil:
			v.fault(id, "repeats a variable number of times and does not say where the count is read from")
		}

		if variable.GetMaxOccurrences() < variable.GetMinOccurrences() {
			v.fault(id, "declares between %d and %d occurrences, which is no range at all",
				variable.GetMinOccurrences(), variable.GetMaxOccurrences())
		}

		if variable.GetMaxOccurrences() == 0 {
			v.fault(id, "declares a maximum of zero occurrences, which no count can satisfy")
		}
	case nil:
		v.fault(id, "carries a repetition that states no count")
	}
}

// names holds a named node's names to carrying an original.
//
// The original **MUST** be present even where an override is: a rename
// substitutes a name, and the substitute is carried beside the original rather
// than in place of it, so that generated code can still point back at the
// copybook it came from. A node with no names at all is a node COBOL gave no
// data-name — a FILLER, or one this producer introduced — except on a record,
// where the copybook's top-level item always has one.
func (v *validator) names(id uint64, names *irpb.Names, required bool) {
	if names == nil {
		if required {
			v.fault(id, "carries no names, and a record node carries the copybook's own")
		}

		return
	}

	if names.GetOriginal() == "" {
		v.fault(id, "carries an override and no original name for it to stand beside")
	}
}

// containment holds every item to being named exactly once.
//
// Exactly one member list, one arm or one record's top level names it. Two would
// put one run of bytes in two places, none makes it unreachable, and the rule
// together with reachability is what makes containment acyclic without a walk
// looking for a cycle: every cycle either leaves a node named twice or leaves
// the whole of it unreachable.
func (v *validator) containment() {
	for _, id := range v.order {
		kind := kindOf(v.nodes[id])
		if !slices.Contains([]string{"group", "variant", "field", "slack"}, kind) {
			continue
		}

		switch v.contained[id] {
		case 1:
		case 0:
			v.fault(id, "is contained by nothing, and every item is named by one member list, one arm or one record")
		default:
			v.fault(id, "is contained %d times, and no two members of a group may occupy the same bytes", v.contained[id])
		}
	}
}

// reachability holds every node to hanging off the file node.
//
// Everything hangs off the one node of kind file, which the message does not
// separately point at. A node nothing reaches is a node no consumer will ever
// index its way to, and a producer that emitted one has emitted a record type,
// a state or a register that the layout does not actually describe.
func (v *validator) reachability() {
	if !v.rooted {
		return
	}

	seen := map[uint64]bool{v.root: true}
	queue := []uint64{v.root}

	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]

		for _, to := range v.edges[at] {
			if seen[to] {
				continue
			}

			seen[to] = true
			queue = append(queue, to)
		}
	}

	for _, id := range v.order {
		if !seen[id] {
			v.fault(id, "is not reachable from the file node, so nothing in the descriptor describes it")
		}
	}
}

// unresolved is the encoding axes a field left unstated, in the order
// docs/layout/SPEC.md names them.
func unresolved(encoding *irpb.Encoding) []string {
	var missing []string

	if encoding.GetCharset() == irpb.Charset_CHARSET_UNSPECIFIED {
		missing = append(missing, "charset")
	}

	if encoding.GetSignConvention() == irpb.SignConvention_SIGN_CONVENTION_UNSPECIFIED {
		missing = append(missing, "sign convention")
	}

	if encoding.GetByteOrder() == irpb.ByteOrder_BYTE_ORDER_UNSPECIFIED {
		missing = append(missing, "byte order")
	}

	if encoding.GetFloatFormat() == irpb.FloatFormat_FLOAT_FORMAT_UNSPECIFIED {
		missing = append(missing, "float format")
	}

	return missing
}

// repeated reports the first literal a one-of test carries twice.
func repeated(values [][]byte) ([]byte, bool) {
	for at, value := range values {
		for _, against := range values[at+1:] {
			if slices.Equal(value, against) {
				return value, true
			}
		}
	}

	return nil, false
}

// quoteBytes renders a literal the way a dump prints one, which is the only
// rendering that is right whatever charset the file is in.
func quoteBytes(value []byte) string {
	return "0x" + hex.EncodeToString(value)
}

// kindOf is what a node is, in the words docs/ir/SPEC.md's table uses.
func kindOf(node *irpb.Node) string {
	switch node.GetKind().(type) {
	case *irpb.Node_File:
		return "file"
	case *irpb.Node_Record:
		return "record"
	case *irpb.Node_Group:
		return "group"
	case *irpb.Node_Variant:
		return "variant"
	case *irpb.Node_Field:
		return "field"
	case *irpb.Node_Slack:
		return "slack"
	case *irpb.Node_Predicate:
		return "predicate"
	case *irpb.Node_State:
		return "state"
	case *irpb.Node_Transition:
		return "transition"
	case *irpb.Node_Register:
		return "register"
	case *irpb.Node_Binding:
		return "binding"
	case *irpb.Node_Guard:
		return "guard"
	}

	return "kindless"
}
