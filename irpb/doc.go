// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package irpb is the Go form of the resolved IR every cpybkc generator plugin
// is handed.
//
// It is the one package a third-party generator is expected to import, which is
// why it lives at an importable path rather than under internal/: the IR is a
// contract, and a contract the only audience for it cannot import is not one.
// [docs/ir/SPEC.md] is the contract itself, and it is normative for what every
// node means; this package is only its spelling in Go.
//
// # Why this is a module and not a package
//
// The import path alone would have been enough to make the types reachable. It
// would not have been enough to make them cheap. A package inside
// github.com/Zaba505/cpybkc puts every dependency that module ever acquires —
// the layout parser's, the plugin runner's, whatever the CLI grows — into the
// build list of a plugin author who wanted twelve node kinds and a Marshal. So
// the boundary is a module boundary: irpb requires google.golang.org/protobuf
// and nothing else, ModuleDependsOnlyOnTheProtobufRuntime asserts it, and the
// arrow points one way. The CLI depends on this module; this module depends on
// no part of the CLI and never will.
//
// # Versions, of which there are two
//
// The Go module tag this package is released under is irpb/vX.Y.Z, moves for
// Go's reasons — a dependency bump, a documentation fix — and says nothing about
// the descriptor. The IR's own version is [Descriptor]'s version field, a single
// monotonic integer, and reading it before anything else is a consumer's first
// obligation. One IR version outlives many module tags, and the tags are pushed
// independently of the CLI's own vX.Y.Z for exactly that reason.
//
// Should the schema ever break its wire format and become package cpybkc.ir.v2,
// that is a breaking change to every Go consumer, so it arrives here as module
// github.com/Zaba505/cpybkc/irpb/v2 under Go's own major-version rule. The
// protobuf package's version suffix and the Go module's major version are then
// the same number, without either having to be kept in step with the other by
// hand.
//
// # Generated code
//
// ir.pb.go is generated from proto/cpybkc/ir/v1/ir.proto by protoc-gen-go, as
// buf.gen.yaml pins it. Do not edit it — change the .proto and run
// `dagger call proto-gen export --path=irpb`, in the same commit.
//
// [docs/ir/SPEC.md]: https://github.com/Zaba505/cpybkc/blob/main/docs/ir/SPEC.md
package irpb
