// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package resolve turns a copybook record into the shape the IR describes — an
// ordered containment of groups, fields, variants and slack, every elementary
// item carrying the byte width `cobol-go`'s codec/SPEC.md gives it — and a
// layout's sequencing expression into the record automaton that says in what
// order those records may appear.
//
// [Resolve] is the first and [CompileSequence] the second. They share this
// package because they spend the same two sources: a copybook read by
// `cobol-go`, and a layout read by
// [github.com/Zaba505/cpybkc/internal/layoutmodel]. They share nothing else —
// what is inside a record and what order records come in are different
// questions, and the second needs every record at once where the first needs
// one.
//
// # The automaton is compiled here for the reason the offsets are
//
// Sequencing reaches a generator as states and transitions and never as the
// expression they were compiled from (docs/ir/SPEC.md, "The sequencing
// automaton"). A regex-like algebra implemented independently in four languages
// is four slightly different algebras, exactly as four independent readings of a
// PICTURE are four different widths — so the algebra is spent here, once, and
// [CompileSequence]'s own comment is where the construction and the two proofs
// over it are argued (#36, #76, #77, #80, #88).
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
// It is asked about one value, and only because that value is a statement about
// the item rather than about how to read it. [layoutmodel.None] says the item's
// bytes are a payload and not characters, which an alphanumeric DISPLAY item's
// may be, a zoned or edited item's may not, and a packed or binary item's are
// not a question — so an override carrying it is held to the copybook here,
// where the copybook is open ([CharsetNoneError]).
//
// The rest of what docs/ir/SPEC.md puts on a field node beside the axes — its
// USAGE, and the attributes that follow from its PICTURE — is not computed here
// and is not copied here. `cobol-go` has already inherited the USAGE down the
// entry tree and parsed the PICTURE, both onto the
// [github.com/Zaba505/cobol-go/copybook.Field] every field node carries, and a
// copy on the node would be a second answer to a question the copybook has
// answered. Spelling them into the descriptor's own members is #38's.
//
// # A table's extent moves with a count, under one of the two readings
//
// [Options.Reading] is the fork docs/ir/SPEC.md's "An item after a table slides,
// and the other reading is a fixed table" records, and it reaches this package
// for the same reason a framing does: a layout states it and `resolve` resolves
// it. Under `odoslide` an `OCCURS DEPENDING ON` table becomes a [Repetition]
// whose count is a reference to the field the DEPENDING ON phrase names, and
// every item behind it begins at the byte after the last occurrence that count
// states. Under `noodoslide` the same clause describes a fixed table at the
// copybook's declared maximum, so it becomes a constant repetition and the count
// field is resolved as an ordinary field of the record with nothing pointing at
// it. There is no default: the two put every item behind the table somewhere
// different and nothing in the file disagrees with the wrong one, so a copybook
// carrying the clause under an unstated reading is rejected (#87).
//
// Both readings carry the copybook's own `OCCURS integer-1 TO integer-2` on the
// repetition, and under the sliding one they are what a consumer holds a decoded
// count to — a count outside them is malformed data. [Record.At] is that check
// and the sum that follows from it, run here so that this package's tests run it
// against the counts `cobol-go` resolves independently.
//
// What a count reference may name is held to before anything is laid out, and
// each rule is docs/ir/SPEC.md's: it may not name a field that repeats or one
// inside a group that repeats, because nothing carries an occurrence number and
// a count with a value per occurrence makes the occurrences of its group
// different widths (#84); it lies ahead of the item it counts and at a constant
// position, because otherwise locating it needs the extent it decides (#88); and
// where two repeating items of one record name it, their declared ranges have to
// overlap, because otherwise no count sizes both tables (#89). Those run on the
// copybook's items rather than on the resolved nodes, so a copybook with four
// REDEFINES alternatives reports each fault once rather than four times.
//
// # A discriminator is compiled, in two scopes and one closed set
//
// Discrimination reaches a generator compiled too, and a [Predicate] is what a
// layout's strategy becomes: one member of a closed set of two tests, naming the
// field node it tests and carrying its literals as the bytes a consumer
// compares. A consumer evaluates one knowing no COBOL and knowing nothing about
// what the strategy was called in a layout file (#37).
//
// Two things select on bytes and they share the node kind and the set. A
// [Transition] chooses a record, and an [Arm] of a variant chooses an
// alternative inside one occurrence of a table. What differs is where a target
// may sit, and every difference follows from when the predicate runs: a
// transition's runs before its record is admitted, so its target sits outside
// every repeating group and ahead of every item whose extent moves with a count,
// while an arm's runs inside an occurrence already located, so its target sits
// *inside* the repeating group and carries neither restriction (docs/ir/SPEC.md,
// "Discriminator predicates", "A predicate on an arm reads one occurrence", #84,
// #90).
//
// Three strategies name no field and none of them lowers into a member testing
// nothing. `single-record-type` is the absence of a predicate, positional
// *first* is the start state, and a record's length and positional *last* are
// refused with a diagnostic each (#80).
//
// The literals are resolved here and nowhere else. A [Guard] over a bytes
// register carries the same resolution for the same reason: one reading of what
// `"01"` is in a file is one answer for a consumer to act on, and two would be
// two.
//
// # What this package leaves to the stories after it
//
// Assembling nodes into a [github.com/Zaba505/cpybkc/irpb.Descriptor] with
// identifiers on them is #38's, and
// [github.com/Zaba505/cpybkc/internal/assemble] is where it landed. A
// [Repetition], a [Binding] and a [Predicate] here therefore name a
// [github.com/Zaba505/cobol-go/copybook.Field] rather than an identifier.
//
// A count that lives in another record never reaches [Resolve] at all, and that
// is the division rather than a gap: a DEPENDING ON phrase names an item of the
// copybook record carrying it, so every reference that function resolves is to a
// field of the record being read. Where a layout's count comes from a header
// admitted earlier it is written as a `times`, and [CompileSequence] binds that
// field into a [Register] as the transition admitting the header is taken —
// which is why a reference reaching across records is not a shape [Resolve] has
// to refuse (docs/ir/SPEC.md, "A variable record is a sum with a variable term",
// #77).
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
//
// # A predicate is bounded by the layout under two framings and by the read
// under the other two
//
// A predicate is evaluated before the record in front of the consumer has been
// identified, so a target past that record's last byte is a target in the
// framing's bytes or the next record's data. docs/ir/SPEC.md's "A predicate
// never reads past the record in front of it" makes that impossible two ways,
// and which one applies is the framing's (#94).
//
// Under **descriptor-word** and **segmented** the length of the record in front
// of the consumer is in hand before any predicate runs, so a target not wholly
// within it does not match and the bound is on the read. `resolve` refuses
// nothing there, and that is a decision rather than an omission: the strict form
// applied to all four framings would refuse the layouts those two framings exist
// to make expressible, a record told apart by a field the longer type has and
// the shorter one does not.
//
// Under **unframed** and **delimited** nothing in the file states a length
// before the transition is taken, so the bound is on the layout and
// [CompileSequence] enforces it: every transition's target lies inside the
// shortest record any transition leaving the same state can admit while this one
// is eligible, measured over the transitions whose guards can hold at once and
// with every table at its minimum occurrences. A layout breaking it is a
// [PredicateReachError] naming the record the target is in, the target and the
// shorter record it would be read past the end of.
//
// On a pair the overlap rule refuses it is a diagnostic rather than a second
// gate, and knowing which it is matters to anyone changing either rule: such a
// layout is refused anyway, so what the reach rule adds there is the message —
// which bytes a consumer would have read and out of which record, in place of a
// report that two discriminators could both match. That is why the specific
// message replaces the generic one rather than being reported beside it.
//
// Everywhere else it is a gate of its own, and that is most pairs now. Two
// predicates are told apart by the bytes their runs share (#325) and by what
// each record's copybook admits at the other's run (#330), and a pair whose runs
// share no byte is admitted whatever those say, on the order the state carries
// (#332) — so a pair can be perfectly readable and still have one of the two
// reach past the shorter record the state can put in front of a consumer.
// [compiler.reportReach] carries both readings, and is asked of every pair.
//
// [Sequencing.Framing] is what that is keyed on, and a caller stating no framing
// states neither mechanism, so the rule is not run. It cannot be inferred from
// an `lrecl`, which is a maximum under two framings and a requirement under one.
package resolve
