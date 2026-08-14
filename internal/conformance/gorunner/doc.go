// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package gorunner runs a conformance corpus entry through a Go generator: it
// invokes the generator on the entry's descriptor, compiles what came out,
// reads the entry's bytes with it, and writes those records back out with it
// (#66, #68).
//
// It is the Go half of the corpus and it is deliberately a package of its own.
// [github.com/Zaba505/cpybkc/internal/conformance] is the corpus — the tuple, the
// value language and the comparison — and every word of it is language-neutral,
// because a third-party generator in another language runs the same entries
// against its own output. What cannot be neutral is compiling: a Go toolchain,
// an import path and a package clause are not things a corpus entry can carry,
// so they live here, behind [Runner.Run], and a runner for another language is a
// sibling of this package rather than a change to it.
//
// # What a run does
//
//  1. Invokes the generator through
//     [github.com/Zaba505/cpybkc/internal/plugin], so the argument vector is the
//     one docs/plugin/SPEC.md fixes and the descriptor the generator reads is
//     [github.com/Zaba505/cpybkc/internal/emit.Marshal]'s bytes — the same ones
//     cpybkc hands a plugin in a real run.
//  2. Writes a driver beside the generated package: a main program that opens
//     the entry's bytes with the generated reader, walks each record it produces
//     against the descriptor, lays those records back out with the generated
//     writer, reads that file too, and writes the corpus's own answer document
//     on standard output.
//  3. Builds and runs that driver with the Go toolchain.
//
// The answer comes back as
// [github.com/Zaba505/cpybkc/internal/conformance.Answer], a values document per
// direction, and each is what the entry states too — so a caller compares them
// with [github.com/Zaba505/cpybkc/internal/conformance.CompareAnswer] and needs
// nothing from this package to interpret the result.
//
// # Why the writing direction is a second read and not a byte comparison
//
// docs/ir/SPEC.md's "Writing a file" makes byte identity a claim about a record
// and not about a file, and the corpus holds entries whose bytes a correct
// writer would not reproduce anyway — the lenient sign nibble of packed-ascii is
// one. [github.com/Zaba505/cpybkc/internal/conformance.Answer] carries the whole
// argument; what it means here is that the driver reads the file it wrote rather
// than comparing it, and that the records handed to the writer are the ones the
// reader produced rather than records rebuilt from the values, so that the bytes
// retained for a slack node travel with them.
//
// # Why the driver reads the descriptor rather than being written against it
//
// The driver could have been emitted with the copybook's names in it — one
// field access per item, spelled from the descriptor at the moment the driver
// was written. It reads the descriptor at run time instead, and walks the
// generated struct beside it by reflection, because the names in a values
// document have to come from somewhere and only two places have them: the
// descriptor, and this repository's own reading of how cpybkc-gen-go munges an
// identifier out of one. Taking them from the descriptor is what keeps the
// harness from carrying a second copy of that munging rule — a copy which,
// being a copy, would agree with the generator exactly until the day the rule
// changed, and then quietly compare the wrong fields.
//
// The walk itself is not in the template. It is
// [github.com/Zaba505/cpybkc/internal/conformance.WriteValue], which the driver
// calls, because it is about the *format* rather than about the run: code inside
// a template is checked by compiling a scratch program per entry and by nothing
// else, and the part of it that decides what a value looks like is the part
// docs/conformance/GRAMMAR.md holds to a table of exact text (#197). What stays
// in the template is what a run needs and a format cannot state — opening the
// entry's bytes with the generated reader, laying the records back out with the
// generated writer, and naming the generated package.
//
// One thing is taken from the generated source and it is the smallest thing
// that can be: which Go type stands for which record node. cmd/cpybkc-gen-go's
// README states that it emits one exported struct per record, in the order the
// descriptor's node list carries them, so the pairing is by position and the
// harness never spells an identifier the generator produced.
//
// # Where the scratch tree goes, and why it is inside the repository
//
// A generated package imports github.com/Zaba505/cobol-go/codec, so it has to be
// built somewhere a go.mod already requires it. Building it inside this module
// costs nothing and needs no network; building it in a temporary directory
// outside would need a module, a requirement and a checksum for every dependency
// the generated code pulls in, resolved at test time.
//
// So the tree goes under this package's own directory, in a directory whose
// name begins with an underscore. The go tool ignores such a directory when it
// matches ./..., which is what keeps a scratch package out of `go build ./...`,
// `go vet ./...` and the linter, while an explicit path still builds it. It is
// removed when the run ends.
package gorunner
