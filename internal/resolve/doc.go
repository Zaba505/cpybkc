// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package resolve turns a copybook record into the shape the IR describes: an
// ordered containment of groups, fields, variants and slack, every elementary
// item carrying the byte width `cobol-go`'s codec/SPEC.md gives it.
//
// # Why the arithmetic is here and nowhere else
//
// Where a field sits in a record is COBOL knowledge — PICTURE, USAGE, the
// dialect's binary staircase, OCCURS, SYNCHRONIZED, REDEFINES — and a generator
// that works it out for itself is a second reading of that knowledge for the two
// to disagree about. docs/ir/SPEC.md's "Dereferencing is not recomputation"
// draws the line: adding up the widths in front of a field is arithmetic on the
// message in hand and every consumer does it, while deriving one of those widths
// from a PICTURE is COBOL and no consumer may. This package is where the second
// list is spent, exactly once, against
// [github.com/Zaba505/cobol-go/copybook], so that `cpybkc-gen-go` and a
// generator written in some other language cannot come to different answers.
//
// # No offset reaches the IR
//
// A [Node] carries a width and its place in a member list, and nothing carries a
// byte offset: docs/ir/SPEC.md's "Ordering and width, and no offset" settled
// that position is stated once, as ordering and width, so that a producer cannot
// state it a second time and be wrong in a way no consumer can detect (#16).
// [Record.Position] is the sum a consumer runs, kept here so that this package's
// own tests run it against the offsets `cobol-go` computed independently.
//
// The same rule shapes [Node.Width]: it is a method rather than a member, and it
// answers from the member list for a group and from the arms for a variant. A
// group's width is the sum of its members' and a variant's is its arms' common
// extent, and neither is a number this package could get wrong on its own.
//
// # REDEFINES is resolved away, two ways
//
// The IR has no REDEFINES, and which of two resolutions a clause takes is
// decided by the question the clause itself cannot answer: whether the choice is
// made once for a record or once for each entry of a table.
//
//   - Outside a repeating group, each alternative becomes its own record.
//     [Resolve] returns one [Record] per combination of alternatives, each with
//     its own containment order and a slack node covering the bytes of the
//     redefined item's storage that the alternative does not occupy.
//   - Inside one, at any depth, the alternative is chosen once per occurrence
//     and there is no record per occurrence for it to become, so it resolves to
//     a variant node instead: an ordered list of arms, each naming the predicate
//     that selects it and the item that is its body (docs/ir/SPEC.md, "A variant
//     is chosen once per occurrence").
//
// Which alternatives a variant has and what selects each one is a layout's to
// say and not a copybook's, so it arrives as [Redefine] values rather than being
// inferred. A [Redefine] naming a single alternative is the overlay an adopter
// wrote to read one item two ways: every occurrence takes it, and no variant is
// emitted at all.
//
// # What this package leaves to the stories after it
//
// The four encoding axes and a field's PICTURE attributes are #33's, compiling a
// layout's discriminator strategies into predicate nodes is #37's, a count read
// out of a record is #35's, and assembling nodes into a
// [github.com/Zaba505/cpybkc/irpb.Descriptor] with identifiers on them is #38's.
// A [Repetition] here therefore carries the count the layout was computed at
// beside the item a DEPENDING ON phrase names, and an [Arm] carries the
// [github.com/Zaba505/cpybkc/internal/layoutmodel.Strategy] a layout wrote
// rather than a compiled predicate.
//
// Slack is the one of those with a foot in both stories. Which alignments a
// dialect honours is `cobol-go`'s and the rest of the rule is #34's, but a
// record whose redefines have been resolved away has bytes no item occupies
// whatever a SYNCHRONIZED clause does, so this package emits a slack node for
// every byte of a group's extent its members leave uncovered, wherever the gap
// came from.
package resolve
