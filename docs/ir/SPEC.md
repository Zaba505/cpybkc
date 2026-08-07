# The Resolved Intermediate Representation

## Overview

The IR is a data file's description after every question a copybook and a layout
left open has been answered: widths resolved, encodings applied, sequencing
compiled. It is what every generator plugin consumes, in any language, and it is
the *only* thing they consume. A generator never reads a copybook, never reads a
layout file, and never recomputes anything the IR already carries.

That makes it the keystone of the four specs, and the reason it is written
first. Fixing what a discriminator or a sequencing expression *means* before the
[layout format](../layout/SPEC.md) fixes how one is spelled keeps the semantics
from being back-derived from whatever notation that format lands on, and keeps
every generator from having to agree on a second reading of the same file.

It is distinct from the [layout format](../layout/SPEC.md), which is the source
an adopter writes, and from the [plugin contract](../plugin/SPEC.md), which
governs how these bytes reach an executable and says nothing about what is in
them. Syntax questions belong in the layout spec, delivery questions belong in
the plugin spec, and what the thing *is* belongs here.

### Scope

In scope: what a resolved file layout contains and what each part of it means —
field descriptors with their widths, their applied encodings and their place in
the record's ordering, record structure, compiled discriminators, the compiled
sequencing automaton, and language-neutral names. Together with the protobuf
schema that carries all of it, the version field that identifies it, and the
compatibility policy that governs changing it.

Out of scope, with reasons, in [Out of Scope](#out-of-scope).

### Governing sources

- **Protocol Buffers Language Guide (proto3)**, *including "Updating A Message
  Type"* — normative for the schema language and for which schema edits are
  wire-compatible. That section is the floor this document's own compatibility
  policy is built on: protobuf says what a decoder tolerates, and the IR's
  policy says what cpybkc additionally promises on top of it.
  <https://protobuf.dev/programming-guides/proto3/>
- **`descriptor.proto`** — the definition of `FileDescriptorSet`, which is what
  plugin authors working in a language with no protobuf codegen read the IR
  through (#19).
  <https://github.com/protocolbuffers/protobuf/blob/main/src/google/protobuf/descriptor.proto>
- **`cobol-go`'s `codec/SPEC.md`** — normative for the four encoding axes a
  resolved field descriptor carries, and for the byte-level meaning of every
  `USAGE` the IR can name. This document references it and restates none of it.
  <https://github.com/Zaba505/cobol-go/blob/main/codec/SPEC.md>
- **`cobol-go`'s root `SPEC.md`** — normative for COBOL source syntax, and so
  for the form of the original names the IR carries alongside any override.
  <https://github.com/Zaba505/cobol-go/blob/main/SPEC.md>

> **Ambiguity:** these sources do not overlap — protobuf governs the container,
> cobol-go governs what is inside it — so there is no conflict here to resolve.
> Where the IR appears to disagree with `codec/SPEC.md` about a byte layout,
> `codec/SPEC.md` wins and the IR has a bug.
>
> They do not cover everything between them. `codec/SPEC.md` excludes record
> formats, descriptor words and line terminators explicitly, as concerning how
> records are delimited in a file rather than how an item is laid out inside
> one. Physical framing therefore has no governing source above it, and what
> the file node carries of it is this document's own (#26).

### Conformance language

**MUST**, **MUST NOT**, **SHOULD** and **MAY** are normative requirements on the
IR schema and on every producer and consumer of it, interpreted as described in
[CONVENTIONS.md](../CONVENTIONS.md). Everything else is descriptive.

## Structure

A resolved layout is a single protobuf message carrying two things: the
[version](#versioning-and-compatibility) identifying the contract it was written
against, and a list of **nodes**. Everything else hangs off the one node of kind
**file** — which the message does not separately point at, because a root
identifier beside a kind that occurs exactly once would be the first fact this
document states twice.

### A node set, not a tree

The IR **MUST** carry structure as a flat set of typed nodes with references
between them, and **MUST NOT** express it by nesting one message inside another.
Every node carries an identifier and a body that is one member of the closed set
of kinds below, and every structural relationship — a group's members, the field
a predicate tests, the state a transition moves to, the item whose value an
`OCCURS DEPENDING ON` counts — is a reference to an identifier.

Nesting was the alternative, and what it buys is readability: a nested message
prints as the record it describes. It loses on the shape of the thing being
described, because a file is not a tree. Sequencing is a graph with cycles in it
— that is what makes a file a stream rather than a fixed list. A discriminator
points at a field in the record it selects. An `OCCURS DEPENDING ON` object may
sit outside the group it counts, and in real layouts in another record entirely
(#35). A tree carries each of those as a name path for the consumer to resolve,
which puts name resolution — and the chance of two generators resolving one
differently — into every language anyone writes a generator in.

The cost is real, and none of it is paid by the producer. A consumer **MUST**
index the node list by identifier before it can walk anything. The JSON debug
form (#21) is markedly harder to read than a nested one would be. And adding a
kind to the set is a breaking change rather than an addition, for the reason
[Versioning and compatibility](#versioning-and-compatibility) gives. The set is
closed to buy something specific for that price: a consumer switches over
members the schema enumerates, so a member it has never heard of is a failure it
can detect rather than a string it silently ignores.

### The node kinds

What each kind carries is normative. How it is spelled in protobuf — message
names, field names, field numbers — is #17's.

| Kind | Carries |
|---|---|
| **file** | The physical framing of the dataset: the record format, the record length or maximum length, the block size where one applies, and whether records are preceded by a descriptor word or ended by a delimiter. Plus the identifier of the automaton's start state. Exactly one node of this kind exists, and it is the root. |
| **record** | The identifier of the item that is the record's top level, and the record's names. |
| **group** | An ordered list of the identifiers of its members, its names, and its repetition. |
| **field** | An elementary item: its width, its four resolved encoding axes, its `USAGE`, the attributes that follow from its PICTURE — category, digits, scale, and whether and where a sign is held — its names, and its repetition. |
| **slack** | A width, and nothing else: bytes that are part of the record and belong to no item. |
| **predicate** | The identifier of the field it tests, and the test itself, as one member of a closed set. |
| **state** | Whether the state accepts, and an ordered list of the identifiers of the transitions leaving it. |
| **transition** | The identifiers of the predicate that selects it, the record it admits, and the state it moves to. |

### Identity, ordering and determinism

- A producer **MUST** assign identifiers by a deterministic traversal of the
  resolved layout, and **MUST** emit the node list in ascending identifier
  order. Identical inputs produce byte-identical IR (#38), and a flat list puts
  the identifiers and their order inside that promise alongside the contents.
- An identifier means identity and nothing else. A consumer **MUST NOT** infer
  containment, ordering or position from one.
- Every reference **MUST** resolve to a node in the same message, of the kind
  the referring position requires.
- A member list **MUST** be in record order — the order in which the members
  occupy bytes. Ordering is data here, not a convention a consumer restores by
  sorting: [Offsets and widths](#offsets-and-widths) makes it the only statement
  of where anything is.
- Containment **MUST** be acyclic, and an item **MUST** be a member of exactly
  one group. Transitions are under no such rule — a cycle there is a file that
  repeats a record, which is most files.
- Containment is stated once, downward. A consumer needing a parent — to
  qualify a name, say — inverts the member lists while it indexes. Carrying the
  upward reference too would be one fact written twice, which is the rule the
  rest of this document is built on.

### Dereferencing is not recomputation

The overview promises that no generator recomputes what the IR has resolved, and
a consumer of a node set does have to build an index and walk it. Those are
different things, and the line between them is worth drawing exactly, because
every section below leans on it.

What a generator **MUST NOT** do is *derive* a resolved fact: a width from a
PICTURE, a byte layout from an encoding profile, an alignment from a
`SYNCHRONIZED` clause, an automaton from a sequencing expression, a target from
a name. Each is COBOL knowledge. Each is specified somewhere a generator author
may never have read. Each is a place where two languages' generators diverge
without either one failing.

Mapping identifiers to nodes, inverting a member list, and adding up the widths
in front of a field are none of those. They are operations on the message in
hand, they are identical in every language, and they need no COBOL. The IR
abolishes the first list, not the second.

## Offsets and widths

A field node and a slack node each carry a width in bytes. A group's width is
the sum of its members' and is not separately carried. No node carries a byte
offset.

### Ordering and width, and no offset

A field's position within its record is the sum of the widths of everything
ahead of it in the containment order, measured from the first byte of the
record's data. The IR **MUST NOT** carry that sum as a field of its own; a
consumer computes it (#32).

This is the one place the IR was free to differ from the layout format, which
excludes derived quantities outright, and it declines to. The case for carrying
the offset is real, and is why the question was open at all: doing the sum once,
here, is a large part of why generators can exist in more than one language. The
case against won. An offset field is a fact stated twice, and a producer that
gets it wrong is wrong in a way no consumer can detect — the descriptor stays
internally consistent, and every generator in every language faithfully emits
the same wrong reader. Stating position once, as ordering and width, makes that
failure unrepresentable.

The cost is named rather than hidden: every consumer runs the same small sum,
and every consumer is free to get it wrong on its own. That is a bug in one
generator, which the conformance corpus (#66) is there to catch, instead of a
bug in the descriptor that every generator reproduces perfectly.

Widths come from `codec/SPEC.md`'s *Storage Widths*, cited and not restated.
Items carrying no logical value a generator can use — numeric-edited, national,
`INDEX`, `POINTER` — still carry a width, so that the sum stays correct across
them (#32).

A record carries no length either, for the same reason: it is the sum of what is
in it.

### Members never overlap, and `REDEFINES` is resolved away

The sum has a premise: no two members of a group occupy the same byte. A member
list **MUST NOT** contain two items whose extents overlap (#32).

The premise needs stating because COBOL has a clause for breaking it.
`REDEFINES` overlays one item on another, and a producer that lists a redefined
item and its redefiners as members in the order a copybook writes them emits a
record summing to more than its own length. The descriptor stays internally
consistent, no consumer can detect the error, and the failure [Ordering and
width](#ordering-and-width-and-no-offset) calls unrepresentable is
representable after all.

So `REDEFINES` does not reach the IR. It is not a node kind, not a field on one
and not a reference: a producer **MUST** resolve it away (#32), and the graph
carries what is left. Each alternative a discriminator can select becomes its
own record node with its own containment order, the items those alternatives
share appear in each, and bytes the chosen alternative does not occupy are slack
under the rule below. Two layouts overlaying the same bytes are two paths
through the automaton, which is the shape the IR already has. The alternative
was a node kind whose only purpose is to be collapsed by every consumer in every
language before it could compute a single offset.

The cost is duplication, and the IR does not soften it. A dozen record types
sharing a thirty-item prefix carry that prefix a dozen times, and nothing says
the copies are copies: a generator wanting to emit one shared header type over a
dozen independent ones has no reference telling it which fields correspond, and
[Names](#names) denies it the option of matching on names. Emitting the dozen is
always correct. Coalescing them is a generator's judgement, and two generators
may reasonably differ.

### Slack is a node, not a rule

`codec/SPEC.md` warns that a reader **MUST NOT** assume a record is the simple
sum of its fields' widths where `SYNCHRONIZED` is present: slack bytes are
inserted ahead of an aligned item, and they move everything after them. That
warning is about a record described by a copybook. It does not apply to a record
described by the IR, and this is why.

Any byte of a record that no item occupies **MUST** appear in the containment
order as a slack node carrying its width (#34). Alignment then stops being a
rule a generator applies and becomes bytes a generator already has, and the sum
above is literally true with `SYNCHRONIZED` present. A generator **MUST NOT**
implement an alignment rule, because there is nothing left for one to do.

Framing bytes are not slack. A record descriptor word belongs to the dataset,
not to the record's data, and it is described by the file node; positions here
are measured from the first byte after it.

### A variable record is a sum with a variable term

An item that repeats carries its repetition: a constant count, or — for `OCCURS
DEPENDING ON` — a reference to the field node holding the count, which **MAY**
be a field of another record (#35). Its extent is the width of one occurrence
times that count.

Where the count is data-dependent, the sum above is data-dependent with it, and
that is the whole of the model. A generator emits an addition it performs while
reading rather than a constant it was handed. A carried offset could not have
described this without a second, different mechanism for every field following a
variable group; ordering and width describe it with none.

## The encoding profile, applied

`codec/SPEC.md` requires all four axes of an encoding — charset, sign
convention, byte order, float format — from its caller and forbids a default for
any of them, because every one fails silently when wrong. The IR is where that
requirement is met on the generator's behalf.

Every field node **MUST** carry all four axes, resolved, and a producer
**MUST NOT** leave one unset (#33). A consumer **MUST NOT** supply a default for
a missing axis, and **MUST** treat a field missing one as a malformed descriptor
rather than filling it in: an IR that reached a generator with an axis
unresolved is a bug in `resolve`, and papering over it is exactly how a
silently-failing setting produces a silent failure.

There is no profile node for a field to inherit from. The layout format has a
profile layer and per-field overrides (#25); resolution applies the second over
the first, and the result is what a field carries. Carrying the profile as well
would state every axis twice, and a profile sitting in the IR is an invitation
to inherit from it — which is a default, and the whole value of the resolved
form is that no default survives into it.

Two things then follow without being separately specified. A record whose fields
disagree about charset needs no special treatment: the axes are per field, so
the mixed record `codec/SPEC.md` describes is the ordinary case here rather than
an exception. And which axes actually govern a given field's bytes — charset
does not touch packed decimal — is `codec/SPEC.md`'s answer, not a second table
in this document. The IR names the axes; what the bytes are is answered there.

## Names

Every named node carries the original COBOL name, spelled as the copybook spells
it, and **MAY** carry an override alongside it (#30). Both are language-neutral,
and a producer **MUST NOT** apply the casing or identifier conventions of any
language to either (#50).

The original **MUST** be present even where an override is. A rename substitutes
a name, and the substitute is carried beside the original rather than in place
of it, so that generated code can still point back at the copybook it came from.

A name is local. Qualification — COBOL's `OF`/`IN` form, whose grammar is
cobol-go's root `SPEC.md`'s — is the chain of enclosing nodes, and the IR
carries no materialized qualified path for the same reason it carries no
offsets. A consumer needing one walks the member lists it has already inverted.

A name is not identity. A consumer **MUST** resolve a reference by identifier
and **MUST NOT** look a node up by name. Duplicate data names are legal COBOL,
which is why a rename has to name its target fully qualified in the first place
(#30) and why nothing downstream of resolution matches on names at all.

Turning a name into an identifier in some target language is the generator's
work, and this document says nothing about it beyond leaving it possible.
Casing, reserved words, collisions after munging: all of it is `cpybkc-gen-go`'s
(#50), and none of its answers ports to a generator for a language with
different rules — which is the reason they are not here.

## The sequencing automaton

Sequencing reaches a generator compiled. The IR **MUST** carry it as state and
transition nodes and **MUST NOT** carry the expression it was compiled from
(#36). No consumer parses a grammar and no generator author implements one,
which is the point: a regex-like algebra implemented independently in four
languages is four slightly different algebras, and the disagreements surface as
files one generator reads and another rejects.

The file node names the start state. Each state carries its outgoing
transitions, in order; each transition names the predicate that selects it, the
record it admits, and the state to move to. A consumer reads a file by
evaluating the current state's transitions in the order given, taking the first
whose predicate matches, emitting the record that transition names, and moving
to the state it names.

A transition **MUST** be selected by a predicate and **MUST NOT** be labelled
with a record name (#36). A record is what a transition *produces*, not what
chooses it — a consumer cannot know which record it is looking at until a
predicate has told it.

A state carries whether it accepts. A consumer reaching end of input in a
non-accepting state **MUST** report the file as truncated rather than returning
the records it managed to read: an automaton whose accepting states nobody
checks detects nothing.

The automaton is a graph, and a cycle in it is ordinary — a header followed by
any number of details is a cycle. Ambiguity is not: `resolve` rejects an
ambiguous grammar when it compiles one (#36), so the automaton reaching a plugin
is unambiguous and a consumer is entitled to assume it.

## Discriminator predicates

Discrimination reaches a generator compiled too. A predicate is one member of a
closed set, it names the field node it tests, and what it tests is bytes — a
consumer evaluates one knowing no COBOL, and knowing nothing about what the
strategy that produced it was called in a layout file (#37).

A predicate **MUST** name its target as a field node identifier and **MUST NOT**
name it as a field name. The record being discriminated is the record its
transition admits: a producer **MUST** ensure the target is contained in that
record, at any depth, and **MUST NOT** name a field of any other (#37). Where
those bytes are then follows from [Offsets and widths](#offsets-and-widths) —
the target's position is the sum of the widths ahead of it in that record, and
its extent is the target's own width.

A consumer evaluates a predicate against the bytes at the current read position,
read as the record that transition would admit, and consumes nothing until a
transition is taken. Which record the sum is over is therefore settled before
the predicate is evaluated rather than by it, and a discriminator sitting in the
middle of a record — behind a prefix its alternatives share, which is how a
record whose second half is a set of alternatives is usually built — is an
ordinary layout rather than a special case.

The set is closed for the reason `layout/SPEC.md` gives when it closes the
strategies that lower into it — a closed set can be checked for overlap and for
exhaustiveness, and it is what lets the IR stay data instead of carrying source
that only one language could run. Its membership is settled when those
strategies are (#22, #28) and lands in this section in that change, the way the
layout format's grammar joins its governing sources when its syntax is chosen.
It is settled before the first release rather than grown afterwards, because
under [Versioning and compatibility](#versioning-and-compatibility) a new member
is a breaking change.

### When two match, and when none does

Two transitions leaving the same state **MUST NOT** be selected by predicates
that can both match the same record; `resolve` rejects a layout whose
discriminators overlap (#37). The test is whether one input can satisfy both,
not whether the two read the same bytes: a state offering a transition keyed on
a record's first field beside one keyed on its tenth is where the narrower
reading lets a real ambiguity through, and predicates reading different fields
at different positions overlap just as thoroughly as two reading one. So the
question `layout/SPEC.md` defers to this document — what happens when two
discriminators match — has an answer at the one place it is cheap: it does not
arise, because such an IR is never produced.

A consumer **MAY** therefore stop at the first predicate that matches. The
evaluation order is normative all the same, so that two consumers handed the
same bytes do the same work in the same order and report the same thing when
something is wrong with them.

Where no transition's predicate matches, the input is a record the layout does
not describe. A consumer **MUST** report that rather than skipping ahead to a
transition that matches later or falling through to a default. There is no
default, and a file containing an undescribed record is a file the layout is
wrong about.

## Versioning and compatibility

### The version field

The IR carries a single monotonic integer version. A producer **MUST** set it. A
consumer **MUST** read it before anything else, and **MUST** refuse an IR whose
version it does not know rather than proceeding on the parts it recognises.

One integer, and not a major and a minor. The only question a consumer can act
on is whether it understands the descriptor in front of it, and a minor number
exists to let it answer "not entirely, but I will continue anyway" — which is
the failure this section exists to prevent. The cost of one integer is that
there is no way to say *newer, but you are fine*. That case turns out to be
covered by not arising: a change a consumer is genuinely fine with is an
additive one, and an additive one leaves the version alone.

### What may change within a version

Within a version, every edit to the schema **MUST** be wire-compatible in the
sense of the protobuf language guide's *Updating A Message Type*, and a consumer
**MUST** ignore fields it does not recognise.

### What breaks it

A change is breaking when a conforming consumer cannot ignore it and remain
correct. That is a statement about consequence rather than mechanism, and it is
deliberately stricter than protobuf's own rule: protobuf says what a decoder can
still parse, and this says what a generator can still be right about.

Breaking, and requiring the version to advance:

- Removing a field, or reusing its number for anything else.
- Changing what an existing field means, including narrowing or widening the
  values it may hold.
- Adding a node kind, or a member to any other closed set — a predicate kind,
  for one. An old consumer sees an unset choice where a new one sees a member,
  and generates code for a file it has silently misread. This is the standing
  cost of the flat node set, and it is why the kinds are enumerated before the
  first release instead of after it.
- Any addition a consumer must understand in order to stay correct, whether or
  not protobuf would call it compatible.

Not breaking: adding a field that a consumer ignoring it still handles the
descriptor correctly without.

The reversal that makes this work is worth stating on its own. A consumer
**MUST** ignore an unknown *field* and **MUST** fail on an unknown *member of a
closed set*. The two rules point in opposite directions on purpose: a field it
has never seen is information it did not need, while a choice it has never seen
is a fact about the data that it cannot represent at all.

### What this version is not

It is not the IR Go module's tag (#18), which follows Go's module rules and
moves for reasons — a dependency bump, a documentation fix — that say nothing
about the descriptor. It is not the two-sided assertion generated code carries
against the `codec` it links (#53), which binds a generator's output to a
library at another layer entirely. One IR version can outlive many of both.

## Why protobuf, and why no gRPC

protobuf is the schema language because the IR's consumers are third parties
writing in languages nobody here chooses. Java, C#, Python, C++, Rust and Go all
have first-class protobuf support, and reach across unknown languages is the
only axis that matters for a format whose entire purpose is to be read by a
program this project did not write (#17). Avro's one real advantage would have
been self-description; shipping a `FileDescriptorSet` closes it, and a plugin
author whose language has weak protobuf tooling reads the IR dynamically with no
build step at all (#19).

And it is the schema language and nothing else. **There is no service
definition, no server, no port and no lifecycle.** A resolved descriptor is a
value: cpybkc writes it, a plugin reads it, and how it travels between them is
the [plugin contract](../plugin/SPEC.md)'s (#39). The sentence is here because
"protobuf" reads as "RPC" to most people who have met it, and a plugin author
who arrives expecting to implement a service should learn otherwise from the
document that defines the message rather than by reading to the end and finding
no service in it.

What that buys is an IR that is a thing at rest. It can be written to a file
(#20), converted to JSON and read (#21), committed, diffed, and replayed against
a plugin by hand a year later. None of those is available to a protocol.

## Out of Scope

### Layout source syntax

The keys an adopter types, their nesting, their defaults, and the validation
errors a bad layout file produces are **not specified here**.

Reason: the IR is the resolved artifact, and its whole value is that no default
survives into it. A document specifying both the source and the resolved form
would have to describe every setting twice — once as written and once as
resolved — and the two descriptions would drift, which is exactly the gap this
split exists to prevent. The layout format is
[`layout/SPEC.md`](../layout/SPEC.md) (#22). What the syntax there *means* is
still this document's, and that asymmetry is deliberate.

### Generator-specific options

The `--opt k=v` pairs a plugin accepts, and their meanings, are **not part of
the IR**.

Reason: an option in the IR would make the resolved descriptor depend on which
generators a project happens to run, so the same data file would resolve to
different IR in two projects, and the conformance corpus (#66) could not compare
them. Options travel with the invocation instead, in
[`plugin/SPEC.md`](../plugin/SPEC.md) (#39).

### How the IR is produced

The algorithm that turns a copybook and a layout into IR is **not specified**.

Reason: `resolve` (#32–#38) is an implementation, and its correctness criterion
is that its output satisfies this document. Writing the algorithm down as well
would be a second description of the same requirement, drifting from the code at
the first optimisation, and no third party builds against it.

### Also out of scope

- **COBOL source syntax and byte-level data representation.** Both are
  `cobol-go`'s, cited above and never restated. The IR names an encoding; what
  the bytes of that encoding are is `codec/SPEC.md`'s answer.
- **A transport.** protobuf here is a schema language. There is no gRPC service,
  no server, no port and no lifecycle; the descriptor reaches a plugin as a file
  on disk (#39).
- **The conformance corpus** (#66–#68), which is test infrastructure under
  `testdata/` and documented with the corpus.

## Appendix: Mapping to Stories

| Section | Implemented by |
|---|---|
| [Structure](#structure) | #17 `ir` |
| [Offsets and widths](#offsets-and-widths) | #32, #34, #35 `resolve` |
| [The encoding profile, applied](#the-encoding-profile-applied) | #33 `resolve` |
| [Names](#names) | #30 `layout`, #38 `resolve` |
| [The sequencing automaton](#the-sequencing-automaton) | #36 `resolve` |
| [Discriminator predicates](#discriminator-predicates) | #28 `layout`, #37 `resolve` |
| [Versioning and compatibility](#versioning-and-compatibility) | #17, #18 `ir` |
| [Why protobuf, and why no gRPC](#why-protobuf-and-why-no-grpc) | #17, #19 `ir` |
| Emitting the IR | #20, #21 `ir` |
| Conventions this document follows | #15 `setup` |
