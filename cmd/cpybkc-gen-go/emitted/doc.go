// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package emitted is Go source cpybkc-gen-go copies, declaration for
// declaration, into the packages it generates.
//
// Everything else the generator writes is composed a line at a time, because
// what it says is a function of the descriptor: the struct a record node
// becomes, the switch a state becomes. What is here is not. Re-expressing a
// literal under the encoding a reader or writer was built with
// (docs/ir/SPEC.md, "A consumer may read under other axes, and re-expresses
// what it compares", #379, #380) is the same code in every package that
// compares one, and the only thing a descriptor decides about it is which of
// these files a package needs.
//
// So it is kept as a package of its own rather than as strings in the
// generator, and that is the whole reason this directory exists: the compiler,
// `go vet`, the linters and the tests beside these files reach it as Go. A
// helper held as a string is checked only by the golden packages that happen to
// need it, and the zoned sign case #379 works through is needed by none of
// them.
//
// # What the generator takes from each file
//
// Every file but this one is copied whole, below its import block, into the
// generated file that needs it — see literals.go in cpybkc-gen-go for which
// file a package needs and why. A file is therefore a unit: nothing in one may
// be left unused by a package that takes it, because the generated packages are
// linted with `unused` like every other package here, and nothing in one may
// name an identifier declared in another file except the ones held.go
// documents as always present. The identifiers are unexported, and lowercase is
// what keeps them clear of every identifier munged from a copybook name.
//
// This file is the one that is not copied, and it declares nothing.
package emitted
