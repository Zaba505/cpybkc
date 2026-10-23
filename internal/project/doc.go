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
// [github.com/Zaba505/cpybkc/internal/layoutschema] holds it to the published
// schema,
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
// and `definitions` paths are relative to the **layout**. Nothing is resolved
// against a search path, a variable or a project root — there is no
// `--include`, no `-I` and no `COPYPATH` — because a search path makes one
// layout name different copybooks on two machines with no difference between them that any file records, and
// generated code that compiles with the wrong offsets is the one failure a file
// library cannot detect.
//
// An absolute path is taken as it is written. That is the same rule read on a
// path that needs no base, rather than an exception to it.
//
// # Where the schema check runs
//
// Between the grammar and the layer readers, over the whole layout (#397). A
// layout is parsed by [github.com/Zaba505/cpybkc/internal/layout] first, then
// held to the published schema by
// [github.com/Zaba505/cpybkc/internal/layoutschema] — the schema embedded from
// the bytes of schema/layout.sexpr, which a release publishes as
// layout-schema.sexpr — and only a layout the schema admits reaches
// [github.com/Zaba505/cpybkc/internal/layoutmodel]. That covers every run path,
// generation and `--emit-ir` alike, because both go through [Load].
//
// Each fault is reported once, under one rule, which is docs/layout/SPEC.md's
// "Validation and diagnostics":
//
//   - A fault a declaration states — an unknown tag or child, a misplaced or
//     extra argument, a repeated or missing child, a value outside a closed set,
//     a reference naming nothing — is the schema check's alone, in the
//     schema's text.
//   - When the schema check reports anything, the run reports exactly those
//     diagnostics and reads no layer. So a layer reader never reports a fault a
//     declaration states, and a fault only a reader can find is reported on the
//     run after the schema's are fixed.
//   - The layer readers keep their own checks, because a caller that reads a
//     layout without the schema still needs them, so no reader code is removed
//     for this. On a run, what a reader reports is only what a declaration
//     cannot say.
//
// The grammar runs first rather than beside the schema for the same reason. The
// constructs it refuses by name — a quote shorthand, nil, a boolean, the empty
// list, an improper list — are ones the schema check refuses too, and a layout
// the grammar rejects is returned with the grammar's faults before the schema
// has seen it.
//
// # A record held in a definitions file
//
// A record may name a definitions file as its source (docs/layout/SPEC.md,
// "Definitions"), and a run resolves it to the descriptor the copybook it was
// converted from gives, byte for byte (#405). The file is read once however
// many records name it, at its path relative to the layout, and in a layout's
// order: the grammar, then whether its top level holds an entry at all, then
// the body check against the published schema (#400), then the entry reader and
// the judge (#401), then `copybook.Build`. A stage that reports anything stops
// the file there, which is #397's rule applied per document. A fresh item tree
// is built for each record naming the file, as for a copybook.
//
// Every fault inside the file is reported once for the file, at its position in
// the file as the run opened it, with a span at each record naming it — a
// [DefinitionsFaultError]. That includes `cobol-go`'s build and layout faults,
// whose line and column are the file's because the entries were built with the
// positions of the forms they came from ([DefinitionsBuildError]). Faults about
// the line naming the file are reported per record, as a copybook's are: a
// file that is not there ([MissingDefinitionsError]), a file holding no entry
// ([NotDefinitionsError]), and a file not declaring the item the record names.
//
// # A record held in a block
//
// A record may name a `definitions-block` in its own layout instead, and a run
// resolves it to the descriptor the copybook the block's entries were converted
// from gives, byte for byte (#409). A block is a form of the layout, so it is
// opened by nothing and checked with the layout: the whole-layout schema check
// holds its every entry to the declarations a definitions file's body check
// reads (#397), and once the layer readers have read the layout, each block is
// read by the entry reader, judged and built once however many records name it
// ([judgeBlocks]) — before any copybook or definitions file is opened. A fresh
// item tree is built for each record naming the block, and its top-level item
// is looked up as in a file or a copybook.
//
// A fault inside a block reads exactly as the same fault in a definitions file,
// and differs only in its span, which is in the layout. It is reported once for
// the block, as a [DefinitionsFaultError] with a span at each record naming the
// block — the schema check's faults inside a block included, which are carried
// that way so that they can name those records too. A block no record names is
// an error at its name, and its entries are judged all the same, so a fault
// inside it is reported beside that one. A block not declaring the item a
// record names is the same "declares no" diagnostic as a file's, naming the
// block bare, with both spans in the layout.
//
// A layout that is all entries — a definitions file named as the manifest's
// `layout` — is classified before the schema check and reported as that alone,
// with a [DefinitionsFileAsLayoutError] carrying a span at the manifest's
// `layout` and one at the file's first entry.
//
// # Which files a run reads
//
// Exactly the copybooks and definitions files the layout's `record` forms name
// (#157), and nothing for a block, which is a form of the layout already read. The manifest lists
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
