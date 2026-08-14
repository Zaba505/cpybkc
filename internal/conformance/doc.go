// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package conformance reads the conformance corpus and compares a runner's
// answer against what an entry expects.
//
// The corpus is testdata/conformance/, its format is docs/conformance/SPEC.md,
// and this package is the reading of that specification which this repository
// runs. An entry is the tuple the spec describes: the layout and the copybooks
// it names, the IR they resolve to, the bytes of a file laid out that way, and
// the values those bytes decode to (#66). Which entries there are and what each
// was derived from stay with the corpus, in that directory's README.md.
//
// # Why a corpus exists at all
//
// cpybkc is a code generator and nothing else, so there is no command of its
// own that reads a data file. What decodes bytes is the code a generator
// emitted, in whatever language it emitted it, and nothing about the IR obliges
// two generators to agree about a byte they were both handed. docs/ir/SPEC.md
// names that hazard twice — a position sum every consumer runs and every
// consumer may get wrong on its own, and two writers free to choose different
// bytes for one record — and both times it names this corpus as what catches
// it. A shared set of files with the answer written down is the only mechanism
// that can: it is what protobuf's conformance suite is, and for the same
// reason.
//
// # The two halves, and which one is here
//
// An entry states two independent things, and they are checked by different
// callers.
//
//   - The layout and its copybooks resolve to the entry's IR. That is a claim
//     about a *producer*, and the pipeline that would check it is the CLI's
//     (#148); the descriptor an entry carries is hand-authored, so it is an
//     oracle rather than a recording of what resolve happens to do today.
//   - The IR, handed to a generator, produces code that decodes the entry's
//     bytes into the entry's values — and writes those records back into a file
//     that decodes to the entry's values again. That is a claim about a
//     *consumer*, in both directions, it is what
//     [github.com/Zaba505/cpybkc/internal/conformance/gorunner] checks for the
//     Go generator, and it is what makes the corpus useful to a third party who
//     has neither this repository's resolver nor its language. [Answer] carries
//     both answers and says why the writing direction is checked by reading
//     rather than by comparing bytes (#68).
//
// Both halves read the same entry, which is the point of carrying the IR in the
// tuple rather than deriving it: a generator author who disagrees with cpybkc
// about what a layout means and a generator author who disagrees about what a
// descriptor means have different bugs, and an entry that carried only three of
// the four members could not tell them apart.
//
// # What Load checks, and what it declines to
//
// Loading an entry parses every member and holds it to what the format
// requires: entry.json carries a description and a source and nothing else,
// ir.json is a descriptor that passes
// [github.com/Zaba505/cpybkc/internal/assemble.Validate] and is written in the
// canonical rendering [github.com/Zaba505/cpybkc/internal/emit.MarshalJSON]
// produces, values.json names records the descriptor carries, and the directory
// holds no file the format has no place for.
//
// It does not check that the values are the *right* values for the bytes.
// Nothing here decodes anything: that is what a runner does, and a loader that
// decoded a record would be a second implementation of the thing the corpus
// exists to test. A stray file is refused for the reason the project manifest
// refuses an unknown field — a file somebody added to an entry directory
// expecting it to be read is worse than one they are told about.
package conformance
