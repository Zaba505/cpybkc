// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package irpb

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// FileDescriptorSet returns the IR's self-description: the protobuf
// FileDescriptorSet describing a [Descriptor], and so the thing that lets a
// consumer decode one with no generated code at all.
//
// It exists for the plugin author whose language has weak protobuf tooling, or
// none in the build. protobuf's one real disadvantage against a self-describing
// format is that a reader normally needs the schema compiled in ahead of time; a
// FileDescriptorSet closes it, because every runtime worth the name can build a
// message type out of one at run time and decode against it. [docs/ir/SPEC.md]'s
// "Why protobuf, and why no gRPC" is where that trade is argued and "Reading a
// descriptor without generated code" is what this function ships.
//
// # Why it is computed rather than committed
//
// The set is derived, on every call, from the descriptors protoc-gen-go compiled
// into this package — the same descriptors this package marshals a descriptor
// with. There is no .binpb checked into the repository and no protoc invocation
// anybody has to remember, so there is no second copy of the IR that could
// describe a version of it that no longer exists. Changing
// proto/cpybkc/ir/v1/ir.proto and regenerating ir.pb.go changes what this
// returns in the same commit, because it is one input read twice rather than two
// artifacts kept in step by hand.
//
// # What is in it, and what is deliberately not
//
// [Descriptor] is the only root. It is what a plugin is handed — the IR defines
// exactly one message a generator consumes — so the set is its file plus the
// transitive closure of that file's imports, and nothing else. Naming the root
// as a type rather than as a path is what keeps that true: a file added to
// proto/ that nothing reachable from [Descriptor] imports is not published, and
// the test that fails when one appears is in the module that can see the
// directory.
//
// Comments and source positions are not in it. protoc-gen-go does not compile
// SourceCodeInfo into the descriptors this is derived from, so what is published
// describes the schema and not the document: field numbers, types, names and
// nesting, which is everything a decode needs and nothing a reader of
// ir.proto would go looking for. [docs/ir/SPEC.md] is the prose, and a copy of
// it embedded in an artifact would be a second one to keep current.
//
// # Order
//
// Files are emitted in dependency order: a file appears only after every file it
// imports. That is what `protoc --include_imports` produces and what a consumer
// walking the set linearly — building each file's types as it goes, which is the
// shape of most dynamic protobuf APIs — needs in order to resolve a type
// reference the first time it meets one. Within that, the order is fixed by the
// import declarations themselves, so the output is a function of the schema and
// not of a map iteration.
//
// [docs/ir/SPEC.md]: https://github.com/Zaba505/cpybkc/blob/main/docs/ir/SPEC.md
func FileDescriptorSet() *descriptorpb.FileDescriptorSet {
	set := &descriptorpb.FileDescriptorSet{}
	appendFileWithImports(set, make(map[string]struct{}), descriptorFileDescriptor())
	return set
}

// MarshalFileDescriptorSet encodes [FileDescriptorSet] into the protobuf binary
// wire encoding, which is the form every dynamic protobuf runtime reads a
// FileDescriptorSet in and the form the published ir.binpb artifact holds.
//
// The bytes are deterministic. The artifact is attached to a release and copied
// into the published image (#57), and a set whose bytes moved between two builds
// of the same schema would make every rebuild look like a change to the
// contract. Field order is protobuf's own, the file order is fixed by
// [FileDescriptorSet], and the deterministic option pins the one construct — a
// map field — whose encoding would otherwise follow Go's randomised map
// iteration.
func MarshalFileDescriptorSet() ([]byte, error) {
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(FileDescriptorSet())
	if err != nil {
		return nil, fmt.Errorf("failed to encode the IR FileDescriptorSet: %w", err)
	}

	return b, nil
}

// descriptorFileDescriptor returns the file [Descriptor] is defined in.
//
// It is read off the message rather than named as a string so that the root of
// the set is the descriptor type itself. A path like "cpybkc/ir/v1/ir.proto"
// written here would be a second place the IR's shape is recorded, and would go
// on compiling after the file it names had been renamed or the message moved out
// of it.
func descriptorFileDescriptor() protoreflect.FileDescriptor {
	return new(Descriptor).ProtoReflect().Descriptor().ParentFile()
}

// appendFileWithImports appends fd's imports, transitively and depth first,
// before appending fd itself — which is what puts the result in the dependency
// order [FileDescriptorSet] documents.
//
// seen is keyed by path and marked on entry, so a file reached through two
// import chains is emitted once, at its first and deepest position. Marking on
// entry rather than on append also means a cyclic import graph terminates
// instead of recursing forever; protobuf forbids one, and relying on that to
// stay true is a worse bargain than one map write.
func appendFileWithImports(set *descriptorpb.FileDescriptorSet, seen map[string]struct{}, fd protoreflect.FileDescriptor) {
	if _, ok := seen[fd.Path()]; ok {
		return
	}

	seen[fd.Path()] = struct{}{}

	imports := fd.Imports()
	for i := range imports.Len() {
		appendFileWithImports(set, seen, imports.Get(i).FileDescriptor)
	}

	set.File = append(set.File, protodesc.ToFileDescriptorProto(fd))
}
