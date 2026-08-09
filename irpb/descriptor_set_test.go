// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package irpb_test

import (
	"bytes"
	"fmt"
	"slices"
	"testing"

	"github.com/Zaba505/cpybkc/irpb"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

const (
	// irProtoPath is the schema's path as protobuf knows it — the import path a
	// FileDescriptorProto carries in its name field, which is the directory
	// layout under proto/ that buf.yaml's PACKAGE_DIRECTORY_MATCH enforces
	// rather than anything this module chose.
	irProtoPath = "cpybkc/ir/v1/ir.proto"

	// descriptorFullName is the one message a generator plugin is handed, as a
	// dynamic consumer names it: the protobuf package and the message, never a
	// Go type. It is what such a consumer looks up in the registry it builds
	// out of the published set.
	descriptorFullName = "cpybkc.ir.v1.Descriptor"
)

// TestFileDescriptorSetCarriesTheSchema is the guard that keeps every other
// assertion here from passing vacuously. An empty set satisfies "is
// self-contained", "is in dependency order" and "publishes no service", and it
// satisfies them by describing nothing.
func TestFileDescriptorSetCarriesTheSchema(t *testing.T) {
	names := fileNames(irpb.FileDescriptorSet())

	if len(names) == 0 {
		t.Fatal("the published FileDescriptorSet carries no files")
	}

	if !slices.Contains(names, irProtoPath) {
		t.Fatalf("the set does not carry the descriptor's own file %q; it carries %v", irProtoPath, names)
	}
}

// TestFileDescriptorSetIsInDependencyOrder asserts the ordering
// irpb.FileDescriptorSet's doc comment promises. A consumer that walks the set
// once, building each file's types as it goes — which is what most dynamic
// protobuf APIs make easy — fails outright on the other order, and it fails at
// the type reference rather than at the file, so the diagnostic points nowhere
// useful.
func TestFileDescriptorSetIsInDependencyOrder(t *testing.T) {
	var placed []string

	for _, file := range irpb.FileDescriptorSet().GetFile() {
		for _, dep := range file.GetDependency() {
			if !slices.Contains(placed, dep) {
				t.Errorf("%s is emitted before its import %s", file.GetName(), dep)
			}
		}

		placed = append(placed, file.GetName())
	}
}

// TestFileDescriptorSetIsSelfContained asserts the set resolves on its own. A
// set naming an import it does not carry is the classic way this artifact
// breaks: it decodes, it looks complete, and it fails only in the consumer's
// registry, with an error naming a file they have never heard of.
func TestFileDescriptorSetIsSelfContained(t *testing.T) {
	set := irpb.FileDescriptorSet()

	carried := make(map[string]struct{}, len(set.GetFile()))
	for _, file := range set.GetFile() {
		carried[file.GetName()] = struct{}{}
	}

	for _, file := range set.GetFile() {
		for _, dep := range file.GetDependency() {
			if _, ok := carried[dep]; !ok {
				t.Errorf("%s imports %s, which the set does not carry", file.GetName(), dep)
			}
		}
	}

	if _, err := protodesc.NewFiles(set); err != nil {
		t.Fatalf("the published set does not resolve on its own: %v", err)
	}
}

// TestPublishedSetHasNoServiceDefinition guards the decision docs/ir/SPEC.md's
// "Why protobuf, and why no gRPC" records. The IR defines no service, so
// publishing one in the IR's own self-description would contradict the
// specification in the artifact a plugin author reads instead of the
// specification.
func TestPublishedSetHasNoServiceDefinition(t *testing.T) {
	for _, file := range irpb.FileDescriptorSet().GetFile() {
		if services := file.GetService(); len(services) > 0 {
			t.Errorf("%s publishes %d service definition(s); the IR defines no service", file.GetName(), len(services))
		}
	}
}

// TestPublishedSetCarriesNoSourceCodeInfo asserts what irpb.FileDescriptorSet
// says is deliberately absent. SourceCodeInfo is the comments and the line and
// column spans of every declaration; it is not needed to decode anything, it
// would multiply the artifact's size, and it would put a copy of ir.proto's
// prose — which restates docs/ir/SPEC.md — inside a binary nobody would think
// to update.
func TestPublishedSetCarriesNoSourceCodeInfo(t *testing.T) {
	for _, file := range irpb.FileDescriptorSet().GetFile() {
		if info := file.GetSourceCodeInfo(); info != nil {
			t.Errorf("%s carries SourceCodeInfo (%d locations); the published set describes the schema, not the document", file.GetName(), len(info.GetLocation()))
		}
	}
}

// TestMarshalFileDescriptorSetIsDeterministic asserts what the artifact's
// consumers depend on: two encodings of the same schema produce the same bytes.
// The set is attached to a release and copied into the published image, so bytes
// that moved between two builds would make a rebuild indistinguishable from a
// change to the contract.
func TestMarshalFileDescriptorSetIsDeterministic(t *testing.T) {
	first, err := irpb.MarshalFileDescriptorSet()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if len(first) == 0 {
		t.Fatal("encoded an empty FileDescriptorSet")
	}

	second, err := irpb.MarshalFileDescriptorSet()
	if err != nil {
		t.Fatalf("marshal again: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("two encodings of the same set differ: %d bytes then %d bytes", len(first), len(second))
	}
}

// TestMarshalledSetIsWhatTheArtifactHolds checks the encoded form decodes back
// to the same set, which is the only thing standing between an artifact written
// to disk and one a consumer can use.
func TestMarshalledSetIsWhatTheArtifactHolds(t *testing.T) {
	b, err := irpb.MarshalFileDescriptorSet()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(b, &got); err != nil {
		t.Fatalf("decode the encoded set: %v", err)
	}

	if !proto.Equal(&got, irpb.FileDescriptorSet()) {
		t.Fatal("the encoded set does not decode back to the set that was encoded")
	}
}

// TestDynamicDecodeLeavesNothingUndescribed is the end-to-end half of the
// staleness gate. It decodes a descriptor through the published set alone and
// asserts two things a file-level comparison cannot: that nothing in the
// descriptor landed in the dynamic message's unknown fields — every byte this
// module writes is described by what is published — and that re-encoding the
// dynamic message reproduces the descriptor exactly, which is the property a
// consumer relies on when it reads a descriptor, edits nothing and hands it on.
func TestDynamicDecodeLeavesNothingUndescribed(t *testing.T) {
	descriptor, err := proto.MarshalOptions{Deterministic: true}.Marshal(exampleDescriptor())
	if err != nil {
		t.Fatalf("encode descriptor: %v", err)
	}

	msg := newDynamicDescriptor(t)
	if err := proto.Unmarshal(descriptor, msg); err != nil {
		t.Fatalf("decode descriptor dynamically: %v", err)
	}

	assertNoUnknownFields(t, msg.ProtoReflect())

	roundTripped, err := proto.MarshalOptions{Deterministic: true}.Marshal(msg)
	if err != nil {
		t.Fatalf("re-encode descriptor: %v", err)
	}

	if !bytes.Equal(descriptor, roundTripped) {
		t.Fatalf("dynamic round trip changed the descriptor: %d bytes in, %d bytes out", len(descriptor), len(roundTripped))
	}
}

// newDynamicDescriptor builds a message type for the IR's Descriptor out of the
// published bytes and nothing else — the exact path a plugin author with no
// generated code takes.
func newDynamicDescriptor(t *testing.T) *dynamicpb.Message {
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

	desc, err := files.FindDescriptorByName(descriptorFullName)
	if err != nil {
		t.Fatalf("find %s in the published set: %v", descriptorFullName, err)
	}

	md, ok := desc.(protoreflect.MessageDescriptor)
	if !ok {
		t.Fatalf("%s resolved to %T, not a message", descriptorFullName, desc)
	}

	return dynamicpb.NewMessage(md)
}

// assertNoUnknownFields walks a decoded message and reports any bytes the
// published set had no field for. Unknown fields are how a stale set fails
// quietly: the decode succeeds, the message looks plausible, and the parts the
// consumer never heard of are simply not there.
func assertNoUnknownFields(t *testing.T, msg protoreflect.Message) {
	t.Helper()

	if unknown := msg.GetUnknown(); len(unknown) > 0 {
		t.Errorf("%s carries %d bytes the published FileDescriptorSet does not describe", msg.Descriptor().FullName(), len(unknown))
	}

	msg.Range(func(fd protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		switch {
		case fd.IsList() && fd.Message() != nil:
			list := value.List()
			for i := range list.Len() {
				assertNoUnknownFields(t, list.Get(i).Message())
			}
		case fd.IsMap() && fd.MapValue().Message() != nil:
			value.Map().Range(func(_ protoreflect.MapKey, v protoreflect.Value) bool {
				assertNoUnknownFields(t, v.Message())

				return true
			})
		case !fd.IsList() && !fd.IsMap() && fd.Message() != nil:
			assertNoUnknownFields(t, value.Message())
		}

		return true
	})
}

// fileNames names what a set carried, for a diagnostic that says which files
// turned up rather than how many.
func fileNames(set *descriptorpb.FileDescriptorSet) []string {
	names := make([]string, 0, len(set.GetFile()))
	for _, file := range set.GetFile() {
		names = append(names, file.GetName())
	}

	return names
}

// exampleDescriptor is a descriptor of the shape cpybkc emits, kept as small as
// a conforming one can be: an unframed file, one accepting state reached by one
// transition, and a record whose top-level group holds a single DISPLAY field.
//
// It exercises every part of the schema a dynamic consumer meets on its first
// descriptor — the version enum, the flat node set, a oneof, a nested message
// and an optional string — so that a set which described any of them wrongly
// would fail the round trip above rather than decoding into a plausible-looking
// message.
func exampleDescriptor() *irpb.Descriptor {
	return &irpb.Descriptor{
		Version: irpb.IrVersion_IR_VERSION_1,
		Nodes: []*irpb.Node{
			{
				Id: 0,
				Kind: &irpb.Node_File{
					File: &irpb.File{
						Framing:      &irpb.File_Unframed{Unframed: &irpb.Unframed{}},
						StartStateId: 1,
					},
				},
			},
			{
				Id: 1,
				Kind: &irpb.Node_State{
					State: &irpb.State{
						Accepts:       true,
						TransitionIds: []uint64{2},
					},
				},
			},
			{
				Id: 2,
				Kind: &irpb.Node_Transition{
					Transition: &irpb.Transition{
						RecordId:    3,
						NextStateId: 1,
					},
				},
			},
			{
				Id: 3,
				Kind: &irpb.Node_Record{
					Record: &irpb.Record{
						RootId: 4,
						Names:  &irpb.Names{Original: "CUSTOMER-RECORD"},
					},
				},
			},
			{
				Id: 4,
				Kind: &irpb.Node_Group{
					Group: &irpb.Group{
						MemberIds: []uint64{5},
						Names:     &irpb.Names{Original: "CUSTOMER-RECORD"},
					},
				},
			},
			{
				Id: 5,
				Kind: &irpb.Node_Field{
					Field: &irpb.Field{
						Width: 8,
						Encoding: &irpb.Encoding{
							Charset:        irpb.Charset_CHARSET_CP037,
							SignConvention: irpb.SignConvention_SIGN_CONVENTION_EBCDIC,
							ByteOrder:      irpb.ByteOrder_BYTE_ORDER_BIG_ENDIAN,
							FloatFormat:    irpb.FloatFormat_FLOAT_FORMAT_IBM_HFP,
						},
						Usage: irpb.Usage_USAGE_DISPLAY,
						Picture: &irpb.Picture{
							Category: irpb.Category_CATEGORY_NUMERIC,
							Digits:   8,
						},
						Names: &irpb.Names{
							Original:     "CUST-ID",
							OverrideName: proto.String("CustomerID"),
						},
					},
				},
			},
		},
	}
}

// Example_readADescriptorWithoutGeneratedCode is the worked example
// docs/ir/SPEC.md's "Reading a descriptor without generated code" points at: a
// consumer reads a descriptor through the published FileDescriptorSet alone,
// with no code generated from proto/ anywhere in it.
//
// Everything below the marked line uses only protobuf's own runtime — the
// descriptor types, a registry built from the published bytes, and a dynamic
// message — so it transliterates directly into any language whose protobuf
// runtime can load a FileDescriptorSet, which is every one of them. In practice
// the consumer's first two lines are a read of the ir.binpb release asset and a
// read of the file cpybkc passed as --descriptor; they are spelled as in-process
// calls here so that the example runs as a test and cannot rot.
func Example_readADescriptorWithoutGeneratedCode() {
	// The producer half. cpybkc encodes a descriptor and publishes the set that
	// describes it; a plugin author writes neither of these two statements.
	descriptor, err := proto.Marshal(exampleDescriptor())
	if err != nil {
		panic(err)
	}

	irBinpb, err := irpb.MarshalFileDescriptorSet()
	if err != nil {
		panic(err)
	}

	// ---- The consumer half: no generated code past this line. ----

	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(irBinpb, &set); err != nil {
		panic(err)
	}

	files, err := protodesc.NewFiles(&set)
	if err != nil {
		panic(err)
	}

	desc, err := files.FindDescriptorByName("cpybkc.ir.v1.Descriptor")
	if err != nil {
		panic(err)
	}

	md := desc.(protoreflect.MessageDescriptor)

	msg := dynamicpb.NewMessage(md)
	if err := proto.Unmarshal(descriptor, msg); err != nil {
		panic(err)
	}

	// The IR version comes first, always: docs/ir/SPEC.md makes reading it
	// before anything else a consumer's first obligation, and a version this
	// consumer does not know is a descriptor it must refuse rather than walk.
	version := msg.Get(md.Fields().ByName("version")).Enum()
	fmt.Println("ir version:", md.Fields().ByName("version").Enum().Values().ByNumber(version).Name())

	// A descriptor is a flat set of nodes referring to each other by
	// identifier, so a consumer indexes it before it walks anything.
	nodes := msg.Get(md.Fields().ByName("nodes")).List()

	byID := make(map[uint64]protoreflect.Message, nodes.Len())
	for i := range nodes.Len() {
		node := nodes.Get(i).Message()
		byID[node.Get(node.Descriptor().Fields().ByName("id")).Uint()] = node
	}

	for i := range nodes.Len() {
		node := nodes.Get(i).Message()

		kind := node.WhichOneof(node.Descriptor().Oneofs().ByName("kind"))
		if kind == nil || kind.Name() != "record" {
			continue
		}

		record := node.Get(kind).Message()
		fmt.Println("record:", name(record))

		group := byID[record.Get(record.Descriptor().Fields().ByName("root_id")).Uint()]
		groupKind := group.Get(group.Descriptor().Fields().ByName("group")).Message()

		members := groupKind.Get(groupKind.Descriptor().Fields().ByName("member_ids")).List()
		for j := range members.Len() {
			member := byID[members.Get(j).Uint()]
			field := member.Get(member.Descriptor().Fields().ByName("field")).Message()

			width := field.Get(field.Descriptor().Fields().ByName("width")).Uint()
			fmt.Printf("  field %s: %d bytes\n", name(field), width)
		}
	}

	// Output:
	// ir version: IR_VERSION_1
	// record: CUSTOMER-RECORD
	//   field CUST-ID: 8 bytes
}

// name reads the copybook spelling out of any message carrying a Names.
//
// It is part of the consumer half above: the names field is a nested message
// with the original spelling in it, and reaching through one is the shape of
// almost every read a generator performs.
func name(msg protoreflect.Message) string {
	names := msg.Get(msg.Descriptor().Fields().ByName("names")).Message()

	return names.Get(names.Descriptor().Fields().ByName("original")).String()
}
