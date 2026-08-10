// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package assemble

import (
	"strconv"

	"google.golang.org/protobuf/proto"

	"github.com/Zaba505/cpybkc/internal/resolve"
	"github.com/Zaba505/cpybkc/irpb"
)

// This file is the automaton's half of the traversal: the registers, the
// states, and everything a transition hangs off.
//
// It is a separate half because the two halves resolve references differently.
// Inside a record every reference is to a node of that record, and the record
// is the scope. On the automaton side a reference is to a register, to a state,
// or *into* the record the transition admits — which is why a transition is the
// place the record scope is picked up, and why nothing here looks a field up
// without one.

// allocateRegisters gives every register a node, in the order the automaton
// allocated them.
//
// A register declares the kind of value it holds and nothing more. What
// `resolve` carries beside it — the item reference the layout wrote, the
// copybook field it resolved to — is read by the bindings and never reaches the
// node: a binding names where a value came from, so a register naming it too
// would be one fact stated twice.
func (a *assembler) allocateRegisters() {
	for _, register := range a.opts.Automaton.Registers {
		node := a.reserve()
		a.registers[register] = node.Id

		node.Kind = &irpb.Node_Register{Register: &irpb.Register{Kind: registerKindOf(register.Kind)}}
	}
}

// allocateStates gives every state a node, in the automaton's own order, before
// any transition is built.
//
// Before, because a transition names the state it moves to and a cycle there is
// ordinary — a header followed by any number of details is one — so there is no
// order in which every state's identifier is already known when its transitions
// are filled.
func (a *assembler) allocateStates() {
	for _, state := range a.opts.Automaton.States {
		a.states[state] = a.reserve().Id
	}
}

// fillStates fills every state, and builds the guards, transitions, predicates
// and bindings that hang off it.
func (a *assembler) fillStates() {
	for _, state := range a.opts.Automaton.States {
		node := a.nodes[a.states[state]]

		built := &irpb.State{Accepts: state.Accepts}
		for _, guard := range state.Acceptance {
			built.AcceptanceGuardIds = append(built.AcceptanceGuardIds, a.guard(guard))
		}

		for _, transition := range state.Transitions {
			built.TransitionIds = append(built.TransitionIds, a.transition(transition))
		}

		node.Kind = &irpb.Node_State{State: built}
	}
}

// transition builds one edge: the record it admits, the state it moves to, the
// predicate that selects it where it carries one, its guards and its bindings.
//
// The record's scope is picked up here and handed down, because everything this
// transition names in a record — its predicate's target, its bindings' sources —
// is a field of the record it admits and of no other.
func (a *assembler) transition(transition *resolve.Transition) uint64 {
	node := a.reserve()

	scope, admitted := a.byName[transition.Record]
	if !admitted {
		a.faults.Fail(&UnknownRecordError{Record: transition.Record, Defined: a.recordNames()})

		return node.Id
	}

	built := &irpb.Transition{
		RecordId:    scope.record,
		NextStateId: a.states[transition.To],
	}

	// A transition **MAY** carry no predicate, and the absence is the absent
	// reference rather than a member of the set testing nothing. Zero is an
	// ordinary identifier, which is why the schema spells this one `optional`
	// and why nothing here writes a sentinel (docs/ir/SPEC.md, "A transition
	// may carry no predicate").
	if transition.Predicate != nil {
		built.PredicateId = proto.Uint64(a.allocatePredicate(scope, transition.Predicate))
	}

	for _, guard := range transition.Guards {
		built.GuardIds = append(built.GuardIds, a.guard(guard))
	}

	for _, binding := range transition.Bindings {
		built.BindingIds = append(built.BindingIds, a.binding(scope, binding))
	}

	node.Kind = &irpb.Node_Transition{Transition: built}

	return node.Id
}

// guard builds one guard node: the register it reads and the test.
//
// Two transitions carrying the same test carry two guard nodes. Nothing in a
// descriptor reads the identity of a guard — a transition's guards **MUST** all
// hold, so their order and their sharing say nothing — and a producer that
// merged them would be asserting a sameness no consumer can act on.
func (a *assembler) guard(guard resolve.Guard) uint64 {
	node := a.reserve()

	built := &irpb.Guard{RegisterId: a.registers[guard.Register]}

	switch guard.Test {
	case resolve.GuardEquals:
		if len(guard.Values) > 0 {
			built.Test = &irpb.Guard_Equals{Equals: literalOf(guard.Register, guard.Values[0])}
		}
	case resolve.GuardOneOf:
		set := &irpb.LiteralSet{Values: make([]*irpb.Literal, 0, len(guard.Values))}
		for _, value := range guard.Values {
			set.Values = append(set.Values, literalOf(guard.Register, value))
		}

		built.Test = &irpb.Guard_OneOf{OneOf: set}
	case resolve.GuardPositive:
		built.Test = &irpb.Guard_GreaterThanZero{GreaterThanZero: &irpb.GreaterThanZero{}}
	}

	node.Kind = &irpb.Node_Guard{Guard: built}

	return node.Id
}

// literalOf is what a guard compares a register against.
//
// Which member is set follows the register's kind rather than what the value
// happens to carry, because the schema requires the two to match: a bytes
// register is compared as bytes and an integer register as a number, and a
// literal that disagreed with its register would be a comparison no consumer
// could run.
func literalOf(register *resolve.Register, value resolve.Value) *irpb.Literal {
	if register != nil && register.Kind == resolve.RegisterInteger {
		return &irpb.Literal{Value: &irpb.Literal_Integer{Integer: value.Number}}
	}

	return &irpb.Literal{Value: &irpb.Literal_BytesValue{BytesValue: value.Bytes}}
}

// binding builds one binding node: the register written, and the value written.
func (a *assembler) binding(s *scope, binding resolve.Binding) uint64 {
	node := a.reserve()

	built := &irpb.Binding{RegisterId: a.registers[binding.Register]}

	switch binding.Value {
	case resolve.BindField:
		id, found := s.fields[binding.Field]
		if !found {
			a.faults.Fail(&UnresolvedTargetError{
				Pos:      a.span(s, binding.Field),
				Record:   s.name,
				Item:     itemName(binding.Field),
				Position: "the binding of register " + strconv.Itoa(registerNumber(binding.Register)),
			})

			break
		}

		built.Value = &irpb.Binding_FieldId{FieldId: id}
	case resolve.BindLessOne:
		built.Value = &irpb.Binding_Decrement{Decrement: &irpb.Decrement{}}
	}

	node.Kind = &irpb.Node_Binding{Binding: built}

	return node.Id
}

// registerNumber is the register's own number, which is what a diagnostic about
// one quotes when the layout's spelling of it is not what went wrong.
func registerNumber(register *resolve.Register) int {
	if register == nil {
		return 0
	}

	return register.ID
}

// allocatePredicate gives a predicate a node, once per record type that reads
// it, and returns the identifier.
//
// The body is filled later. A predicate names a field, an arm's predicate is met
// while its record is still being walked, and a transition's is met after every
// record has been — so filling on sight would work for one of the two and not
// the other.
func (a *assembler) allocatePredicate(s *scope, predicate *resolve.Predicate) uint64 {
	if predicate == nil || s == nil {
		return 0
	}

	key := predicateKey{record: s, predicate: predicate}
	if id, already := a.predicates[key]; already {
		return id
	}

	node := a.reserve()
	a.predicates[key] = node.Id
	a.pending = append(a.pending, pendingPredicate{scope: s, predicate: predicate, node: node})

	return node.Id
}

// fillPredicates fills every predicate node, in the order the nodes were
// allocated.
//
// The order matters for the diagnostics and for nothing else: the identifiers
// were handed out during the traversal, so what happens here cannot move one.
func (a *assembler) fillPredicates() {
	for _, waiting := range a.pending {
		built := &irpb.Predicate{}

		id, found := waiting.scope.fields[waiting.predicate.Target]
		if !found {
			a.faults.Fail(&UnresolvedTargetError{
				Pos:      a.span(waiting.scope, waiting.predicate.Target),
				Record:   waiting.scope.name,
				Item:     itemName(waiting.predicate.Target),
				Position: "the predicate selecting it",
			})
		}

		built.FieldId = id

		switch waiting.predicate.Test {
		case resolve.BytesEqual:
			if len(waiting.predicate.Values) > 0 {
				built.Test = &irpb.Predicate_BytesEqual{
					BytesEqual: &irpb.BytesEqual{Value: waiting.predicate.Values[0].Bytes},
				}
			}
		case resolve.BytesOneOf:
			values := make([][]byte, 0, len(waiting.predicate.Values))
			for _, value := range waiting.predicate.Values {
				values = append(values, value.Bytes)
			}

			built.Test = &irpb.Predicate_BytesOneOf{BytesOneOf: &irpb.BytesOneOf{Values: values}}
		}

		waiting.node.Kind = &irpb.Node_Predicate{Predicate: built}
	}
}
