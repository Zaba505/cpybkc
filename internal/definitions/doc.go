// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package definitions reads an entry body: the transcription of a copybook's
// entries that docs/layout/SPEC.md's "Definitions" chapter specifies.
//
// An entry body is one `entry` form per `cobol-go` DataDescriptionEntry, in
// source order, and it is held in one of two places — a definitions file, whose
// top-level forms are the body, or a `definitions-block` in a layout, whose
// forms after its name are. They are one production, so this package has one
// reader for both: [Read] takes the body's nodes, and [FileBody] and
// [BlockBody] say where those are in each kind of document. A fault the reader
// reports reads the same in a file and in a block, and only its span differs —
// a span into the definitions file for a file, and into the layout for a block.
//
// # What it reports, and what it leaves to others
//
// SPEC.md's "Who reports a fault in an entry body" splits the faults between
// three parties, and this package is the second:
//
//   - The schema check reports every fault a declaration states — an unknown
//     tag, a misplaced, missing or extra argument, a value outside a closed set.
//     That is [github.com/Zaba505/cpybkc/internal/layoutschema]'s, and [Read]
//     takes a body that check has admitted. It does not repeat any of it: a
//     shape the schema refuses is a programming error here, reported as an
//     [UncheckedBodyError] rather than diagnosed again in other words.
//   - This reader reports what a declaration cannot say: a `(word …)` around a
//     spelling that reads as a symbol anyway ([NeedlessWordError]), a text that
//     is not exactly one COBOL token of the kind its position holds
//     ([LiteralTextError]), and an `occurs` phrase written twice or out of the
//     order the grammar admits them ([OccursPhraseError]), which counts one
//     form against another.
//   - `cobol-go` judges what is left: whether an entry, printed and read back,
//     is the entry it was built from. [Judge] applies that rule and reports a
//     refusal as an [InadmissibleEntryError] at the entry; it says nothing of
//     its own about what an entry means or whether `cobol-go` would accept it.
//
// # The model
//
// [Read] returns [Entry] values in source order, and every entry, clause and
// leaf in them carries the [github.com/Zaba505/cpybkc/internal/layout.Pos] it
// was written at, so that whatever is built from them later can point back at
// the text. The model is the notation's, not `cobol-go`'s AST: a literal is the
// text the body wrote, and turning it into a `cobol-go` literal — classified by
// `cobol.Tokenize`, and positioned — is [Construct]'s.
//
// # Both directions
//
// [Write] is the converter: it writes the entries `cobol-go` parsed out of a
// copybook as an entry body in the canonical printed form, at a base depth, so
// that one writer emits a definitions file and a block's body. [Read] and
// [Construct] go back, and [Judge] holds what comes back to the validity rule.
// The round trip over every copybook the repository holds and a kitchen sink
// covering the grammar is what holds the two directions to each other (#401).
//
// # Who opens a file
//
// This package reads bodies it is handed and opens nothing.
// [github.com/Zaba505/cpybkc/internal/project] opens a definitions file a record
// names, holds it to the schema, and hands its body here to be read and judged
// before building it (#405). A `definitions-block` is opened by nothing: it is a
// form of the layout, and the same package hands its body here to be read and
// judged once the layout's schema check has admitted it (#409).
package definitions
