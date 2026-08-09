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
// # The encoding is per field, and no default survives
//
// `codec` takes four encoding axes from its caller and defaults none of them,
// because every one fails silently when wrong. docs/ir/SPEC.md's "The encoding
// profile, applied" meets that requirement on the generator's behalf by putting
// all four on every field node and carrying no profile node for one to inherit
// from, and this package is where the pair a layout writes — one profile, and
// per-item overrides over it — becomes that (#33).
//
// So [Options.Encoding] is required and complete, an [EncodingOverride] applies
// over the encoding in effect where its item sits, and [Node.Encoding] on a
// field is the result. A record whose fields disagree about charset needs
// nothing special for it: the axes are per field, so the mixed file is the
// ordinary case here rather than an exception. Which axis actually governs a
// given field's bytes — charset does not touch packed decimal — is
// `codec/SPEC.md`'s question and is not asked here.
//
// The rest of what docs/ir/SPEC.md puts on a field node beside the axes — its
// USAGE, and the attributes that follow from its PICTURE — is not computed here
// and is not copied here. `cobol-go` has already inherited the USAGE down the
// entry tree and parsed the PICTURE, both onto the
// [github.com/Zaba505/cobol-go/copybook.Field] every field node carries, and a
// copy on the node would be a second answer to a question the copybook has
// answered. Spelling them into the descriptor's own members is #38's.
//
// # What this package leaves to the stories after it
//
// Compiling a layout's discriminator strategies into predicate nodes is #37's, a
// count read out of a record is #35's, and assembling nodes into a
// [github.com/Zaba505/cpybkc/irpb.Descriptor] with identifiers on them is #38's.
// A [Repetition] here therefore carries the count the layout was computed at
// beside the item a DEPENDING ON phrase names, and an [Arm] carries the
// [github.com/Zaba505/cpybkc/internal/layoutmodel.Strategy] a layout wrote
// rather than a compiled predicate.
//
// # Slack, and its four producers
//
// Every byte of a record that no item occupies is a slack node in the
// containment order, whatever left it uncovered (docs/ir/SPEC.md, "Slack is a
// node, not a rule"). Four things leave bytes uncovered and the node is the same
// for all four: the gap ahead of a SYNCHRONIZED item, the storage a REDEFINES
// alternative does not fill, the padding between one occurrence of a table and
// the next, and the tail of a record whose items stop short of the `lrecl` a
// fixed-length dataset states. Which boundary an aligned item sits on is
// `cobol-go`'s answer and is not recomputed here; what this package does with
// the bytes it left behind is the rest of the rule (#34).
//
// One node per *maximal* run, which is why the member list is merged rather than
// appended to. Two runs of different origin that abut are one node of the summed
// width, because "Slack survives a read" makes a consumer keep one run of bytes
// per node, so how this stage divides a run of eight would otherwise decide the
// shape of every record a generator emits.
//
// Two edges a run does not cross, and both are edges a merged node would belong
// to neither side of. An arm's, because an arm's extent is the variant's width
// and a node spanning the end of one arm and the bytes behind the variant would
// make that arm wider than its siblings. And a repeating group's, because a node
// inside one stands for a run per occurrence while a node behind it stands for
// one run in the record, and no single width is both.
//
// # LRECL is the one thing a framing says about a record's members
//
// [Options.Framing] reaches this package for one check and one node. Under a
// fixed-length dataset the next record begins a fixed distance on whatever the
// record was, so a record type accounts for all of LRECL: one shorter carries
// the difference as the trailing slack above, and one longer is a fault, since
// no node takes bytes away. A record type whose extent moves with a count meets
// that at one count and misses it at every other, and is rejected rather than
// padded (docs/ir/SPEC.md, "A variable record does not fit a fixed-length
// dataset", #92).
//
// Under the other three framings each record states its own length, so an
// `lrecl` is a maximum that is checked and never padded to.
package resolve
