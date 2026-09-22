// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package irpb_test

import (
	"testing"

	"github.com/Zaba505/cpybkc/irpb"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// armFullName and scheduleFullName name the two messages this file is about as
// a dynamic consumer names them — the protobuf package and the message, never a
// Go type — because the assertions below are about the published schema and not
// about the code generated from it.
const (
	armFullName      = "cpybkc.ir.v1.Arm"
	scheduleFullName = "cpybkc.ir.v1.Schedule"
)

// TestArmSelectorIsAChoice asserts the shape docs/ir/SPEC.md's "An arm may be
// selected by its position in the table" requires of the schema: the two ways
// an arm is selected are members of one oneof, so that an arm carrying both is
// not a descriptor a producer can emit.
//
// The rule matters more than most, because an arm carrying both is the one
// addition of this story a consumer built before it misreads in silence. It
// resolves the predicate it knows, evaluates it, and takes the arm in
// occurrences the schedule assigned elsewhere, with no reference left
// unresolved for anything to notice. A oneof makes that descriptor unsayable;
// two optional fields would only have made it forbidden.
func TestArmSelectorIsAChoice(t *testing.T) {
	arm := messageByName(t, armFullName)

	selector := arm.Oneofs().ByName("selector")
	if selector == nil {
		t.Fatalf("%s carries no oneof named selector; it carries %v", armFullName, oneofNames(arm.Oneofs()))
	}

	if selector.IsSynthetic() {
		t.Fatalf("%s's selector is a synthetic oneof — that is an optional field, not a choice", armFullName)
	}

	want := []protoreflect.Name{"predicate_id", "schedule"}
	got := fieldNames(selector.Fields())

	if len(got) != len(want) {
		t.Fatalf("%s's selector holds %v, want %v", armFullName, got, want)
	}

	for i, name := range want {
		if got[i] != name {
			t.Fatalf("%s's selector holds %v, want %v", armFullName, got, want)
		}
	}
}

// TestArmFieldNumbersAreUnmoved asserts that adding the schedule moved no
// number an already-published descriptor uses. Reusing or renumbering one is
// what ir.proto's IrVersion comment calls a change that advances the version,
// and this addition does not advance it: an arm written by an earlier release
// must decode against this schema unchanged.
func TestArmFieldNumbersAreUnmoved(t *testing.T) {
	arm := messageByName(t, armFullName)

	want := map[protoreflect.Name]protoreflect.FieldNumber{
		"predicate_id": 1,
		"group_id":     2,
		"field_id":     3,
		"schedule":     4,
	}

	fields := arm.Fields()
	if fields.Len() != len(want) {
		t.Fatalf("%s carries %d fields, want %d: %v", armFullName, fields.Len(), len(want), fieldNames(fields))
	}

	for name, number := range want {
		field := fields.ByName(name)
		if field == nil {
			t.Fatalf("%s carries no field named %s; it carries %v", armFullName, name, fieldNames(fields))
		}

		if field.Number() != number {
			t.Fatalf("%s's %s is field %d, want %d", armFullName, name, field.Number(), number)
		}
	}
}

// TestScheduleCarriesOccurrenceNumbers asserts that a schedule is a list of
// occurrence numbers and carries nothing else. The message exists only because
// protobuf admits no repeated member of a oneof; anything else appearing on it
// would be a fact about a scheduled arm stated somewhere no rule expects it.
func TestScheduleCarriesOccurrenceNumbers(t *testing.T) {
	schedule := messageByName(t, scheduleFullName)

	fields := schedule.Fields()
	if fields.Len() != 1 {
		t.Fatalf("%s carries %d fields, want 1: %v", scheduleFullName, fields.Len(), fieldNames(fields))
	}

	occurrences := fields.ByName("occurrence_numbers")
	if occurrences == nil {
		t.Fatalf("%s carries no field named occurrence_numbers; it carries %v", scheduleFullName, fieldNames(fields))
	}

	if !occurrences.IsList() {
		t.Fatalf("%s's occurrence_numbers is not repeated; a schedule is one or more occurrences", scheduleFullName)
	}

	if occurrences.Kind() != protoreflect.Uint32Kind {
		t.Fatalf("%s's occurrence_numbers is %s, want uint32 — the type a repetition's constant count already uses", scheduleFullName, occurrences.Kind())
	}
}

// TestSettingOneSelectorClearsTheOther is the choice observed from the wire
// rather than from the schema: a producer that sets a schedule over a predicate
// emits an arm carrying the schedule alone, because that is what a oneof does.
// It is the assertion a reader of ir.proto's comment would want checked, and it
// is cheap.
func TestSettingOneSelectorClearsTheOther(t *testing.T) {
	arm := &irpb.Arm{
		Selector: &irpb.Arm_PredicateId{PredicateId: 42},
		Body:     &irpb.Arm_GroupId{GroupId: 7},
	}

	arm.Selector = &irpb.Arm_Schedule{
		Schedule: &irpb.Schedule{OccurrenceNumbers: []uint32{1, 3, 5}},
	}

	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(arm)
	if err != nil {
		t.Fatalf("encode arm: %v", err)
	}

	var decoded irpb.Arm
	if err := proto.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode arm: %v", err)
	}

	if _, ok := decoded.GetSelector().(*irpb.Arm_Schedule); !ok {
		t.Fatalf("the decoded arm's selector is %T, want a schedule", decoded.GetSelector())
	}

	if got := decoded.GetPredicateId(); got != 0 {
		t.Fatalf("the decoded arm carries predicate %d beside its schedule; the oneof did not clear it", got)
	}

	if got := decoded.GetSchedule().GetOccurrenceNumbers(); len(got) != 3 {
		t.Fatalf("the decoded arm's schedule holds %v, want three occurrences", got)
	}

	if _, ok := decoded.GetBody().(*irpb.Arm_GroupId); !ok {
		t.Fatalf("the decoded arm's body is %T, want a group — the body is a separate choice", decoded.GetBody())
	}
}

// messageByName finds one message in the published FileDescriptorSet, by the
// same path a plugin author with no generated code takes. Reading the schema
// back out of what a release attaches is what makes these assertions about the
// published contract rather than about this module's own build.
func messageByName(t *testing.T, name protoreflect.FullName) protoreflect.MessageDescriptor {
	t.Helper()

	desc, err := newPublishedFiles(t).FindDescriptorByName(name)
	if err != nil {
		t.Fatalf("find %s in the published set: %v", name, err)
	}

	md, ok := desc.(protoreflect.MessageDescriptor)
	if !ok {
		t.Fatalf("%s resolved to %T, not a message", name, desc)
	}

	return md
}

func fieldNames(fields protoreflect.FieldDescriptors) []protoreflect.Name {
	names := make([]protoreflect.Name, 0, fields.Len())
	for i := range fields.Len() {
		names = append(names, fields.Get(i).Name())
	}

	return names
}

func oneofNames(oneofs protoreflect.OneofDescriptors) []protoreflect.Name {
	names := make([]protoreflect.Name, 0, oneofs.Len())
	for i := range oneofs.Len() {
		names = append(names, oneofs.Get(i).Name())
	}

	return names
}

// newPublishedFiles builds a type registry out of the published bytes and
// nothing else: marshal the set a release attaches, decode it, and resolve it.
// Every assertion that reads the schema goes through here, so that a set which
// stopped carrying a file fails the assertion rather than quietly falling back
// on the generated code sitting in the same module.
func newPublishedFiles(t *testing.T) *protoregistry.Files {
	t.Helper()

	irBinpb, err := irpb.MarshalFileDescriptorSet()
	if err != nil {
		t.Fatalf("marshal the published set: %v", err)
	}

	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(irBinpb, &set); err != nil {
		t.Fatalf("decode the published set: %v", err)
	}

	files, err := protodesc.NewFiles(&set)
	if err != nil {
		t.Fatalf("build a type registry from the published set: %v", err)
	}

	return files
}
