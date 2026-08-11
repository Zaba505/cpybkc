// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package project turns a project's cpybkc.json into the run it describes: the
// one descriptor its layout resolves to, and the generators to run over it.
//
// It is the seam between the readers and the pipeline. Everything it does is
// already implemented somewhere else — [github.com/Zaba505/cpybkc/internal/manifest]
// reads the file, [github.com/Zaba505/cpybkc/internal/layout] and
// [github.com/Zaba505/cpybkc/internal/layoutmodel] read the layout,
// [github.com/Zaba505/cpybkc/internal/resolve] resolves a copybook against it,
// [github.com/Zaba505/cpybkc/internal/assemble] assembles the descriptor and
// [github.com/Zaba505/cpybkc/internal/plugin] finds the executables — and what
// is here is the order they are called in, the paths they are called with and
// the copybooks nobody else opens.
//
// # Why it is a package and not the command
//
// docs/cli/SPEC.md's contract is a command line, a stream and an exit status,
// and `cmd/cpybkc` is the thing that keeps it: it reads the vector, decides the
// status and writes the diagnostics. None of that is testable against a
// descriptor, and all of this is — a layout resolves to a descriptor or to a
// list of faults whether or not anybody typed anything. Keeping the two apart
// is also what keeps the rule #148 states about itself true: `main` composes and
// reports, and the composition it reports on is here.
//
// # Where a path is looked for
//
// Two base directories and no third one, which is docs/cli/SPEC.md's "A path is
// relative to the file that states it": the manifest's `layout` and each
// generator's `out` are relative to the **manifest**, and a layout's `copybook`
// path is relative to the **layout**. Nothing is resolved against a search path,
// a variable or a project root — there is no `--include`, no `-I` and no
// `COPYPATH` — because a search path makes one layout name different copybooks
// on two machines with no difference between them that any file records, and
// generated code that compiles with the wrong offsets is the one failure a file
// library cannot detect.
//
// An absolute path is taken as it is written. That is the same rule read on a
// path that needs no base, rather than an exception to it.
//
// # Which copybooks a run reads
//
// Exactly the ones the layout's `record` forms name (#157). The manifest lists
// none and constrains none: a layout naming a copybook is never a fault for not
// having been declared, because there is no declaration for it to be missing
// from. The only fault about a copybook is one that cannot be opened, or one
// that opens and does not declare the item a record names, and both are
// reported against the line of the layout that named it — with the absolute path
// cpybkc opened on a continuation line, which is the "where it was looked for"
// docs/layout/SPEC.md requires the CLI to explain.
//
// # One tree per record, not one per file
//
// A copybook file is read once and its entries are assembled into a fresh item
// tree for **each** `record` form bound to it. Two records over one `01`-level
// are ordinary (docs/layout/SPEC.md, "Many records may name one copybook, and
// two may name one item"), and that section requires them to be "discriminated,
// renamed and encoding-overridden independently and neither reaches the other".
// The mechanisms that carry those are keyed on the copybook item —
// `assemble`'s renames are a map on the field, and `resolve`'s encoding
// overrides name one — so sharing one tree between two records would make a
// rename written for one of them reach the other. Building the tree twice is
// what makes the independence the format promises a property of the data rather
// than of what each consumer remembers to check.
//
// # What is decided here that no document states
//
// Two things, and both are named rather than buried, because a decision nobody
// wrote down is one the next reader has to rediscover:
//
//   - A copybook is read as **fixed-format** COBOL source. Nothing in
//     docs/cli/SPEC.md or docs/layout/SPEC.md states a reference format, and
//     `cobol-go` has no detection to defer to. Fixed format is what a copybook
//     out of a mainframe library is written in, it is what every copybook in
//     this repository's own conformance corpus is written in, and a free-format
//     reader rejects all of them at the comment indicator in column 7.
//   - The copybook **dialect** is IBM Enterprise COBOL's. A dialect decides the
//     binary width staircase, whether SYNCHRONIZED inserts slack and what an
//     oversized REDEFINES is, and no layout form states one — so there is
//     nothing to read it from and a run needs an answer. It is the dialect the
//     files this project exists for were written by.
//
// Both belong in a document eventually; neither is a thing this package invented
// a syntax for in the meantime.
package project
