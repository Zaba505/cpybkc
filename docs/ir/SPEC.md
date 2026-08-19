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
the record's ordering, record structure, the physical framing that separates one
record from the next, compiled discriminators, the compiled sequencing automaton
and the values it carries forward between records, and language-neutral names.
Together with the protobuf schema that carries all of it, the version field that
identifies it, and the compatibility policy that governs changing it. And what a
consumer does with all of it in both directions: the code a generator emits to
read a data file, and the code it emits to write one.

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
- **IBM Enterprise COBOL for z/OS**, *Complex OCCURS DEPENDING ON*, against
  **Micro Focus's `OCCURS` clause** under `NOODOSLIDE` and **GnuCOBOL's
  `odoslide` compiler configuration** — normative for the two places a compiler
  may put an item that follows a variable-length table, which is the one thing
  this list is meant to disagree about. IBM's layout is unconditional: the
  location of an item following an `OCCURS DEPENDING ON` item is affected by
  the value of that clause's object. Micro Focus makes it a directive, and
  under `NOODOSLIDE` "all group items containing the table are considered as
  always having the maximum number of occurrences" and "the position of the
  data items following the table is not changed"; GnuCOBOL carries the same
  switch as a dialect option, off by default and implied by `-std=ibm`. [An
  item after a table slides, and the other reading is a fixed
  table](#an-item-after-a-table-slides-and-the-other-reading-is-a-fixed-table)
  is what this document does with the pair, and the Micro Focus sentence is
  cited because the resolution turns on it rather than on a preference (#87).
  That same Micro Focus page is normative for one thing outside the fork, where
  the two vendors agree: where the clause's own object may sit. *Data-name-1
  must have a fixed location, and must not follow an item that contains an
  OCCURS DEPENDING ON clause* is what [A count is in hand before the extent it
  decides](#a-count-is-in-hand-before-the-extent-it-decides) leans on when it
  refuses a count no compiler would place (#88). The IBM page is normative for
  one more thing the fork does not touch: a record may carry more than one
  variable-length table, an entry containing the clause followed by a
  non-subordinate entry containing another. Neither vendor restricts how many
  such entries may name one object — both restrict where the object sits — and
  that silence is what [One count may size two tables, and a writer refuses to
  choose](#one-count-may-size-two-tables-and-a-writer-refuses-to-choose) reads
  as permission. GnuCOBOL carries complex `OCCURS DEPENDING ON` as a dialect
  option rather than as a default, so the shape is IBM's before it is anybody
  else's (#89).
  <https://www.ibm.com/docs/en/cobol-zos/6.3.0?topic=tables-complex-occurs-depending>
  <https://www.microfocus.com/documentation/reuze/60d/lhpdf40f.htm>
  <https://superbol.eu/gnucobol/manual/chapter14.html>
- **z/OS DFSMS record formats** and ***Using Data Sets*** — normative for the
  record and segment descriptor words two of the framings in [Physical
  framing](#physical-framing) name, and so for the bytes a consumer reads and a
  writer emits around a record in those datasets. This document names the
  descriptor word and restates none of its layout.
  [`layout/SPEC.md`](../layout/SPEC.md) cites the same pair for the RECFM
  spelling an adopter writes; it is cited here for what those bytes are (#78).
  <https://www.ibm.com/docs/en/zos/3.1.0?topic=sets-record-formats>
  <https://www.ibm.com/docs/en/zos/3.1.0?topic=guide-using-data-sets>

> **Ambiguity:** four of these sources do not overlap — protobuf governs the
> container, cobol-go governs what is inside a record, DFSMS governs the
> descriptor words around one — so there is no conflict between them to
> resolve. Where the IR appears to disagree with `codec/SPEC.md` about a byte
> layout, `codec/SPEC.md` wins and the IR has a bug.
>
> The fifth entry is a pair that exists to disagree, and it is the only one in
> this list. IBM's Enterprise COBOL and Micro Focus's `NOODOSLIDE` put an item
> that follows a variable-length table in two different places, and both write
> files this project reads. Neither wins: the fork is recorded as a setting an
> adopter states, and it arrives here resolved rather than carried, in the
> manner the third paragraph below describes for the other one.
>
> They do not cover everything between them. `codec/SPEC.md` excludes record
> formats, descriptor words and line terminators explicitly, as concerning how
> records are delimited in a file rather than how an item is laid out inside
> one. DFSMS fills half of what that leaves; the other half has no source
> anywhere. Nothing says which byte ends a line-delimited record, which is why
> the file node carries one as bytes rather than as a name ([A delimiter is
> bytes, not a character](#a-delimiter-is-bytes-not-a-character)), and the rest
> of [Physical framing](#physical-framing) is this document's own (#26, #78).
>
> The fork `layout/SPEC.md` records as a setting — IBM's record formats against
> the line-delimited "RECFM=V" that GnuCOBOL and Micro Focus write — arrives
> here already resolved. A framing is what a consumer does, so a line-delimited
> file is the **delimited** framing and nothing in the IR is labelled `V`.
>
> The second fork those same vendors carry — whether an item after a
> variable-length table slides — arrives resolved by the same move, and further:
> not into a member of a set but into node shapes that were already here. A file
> written under the reading this document does not take has no variable-length
> table in it at all, so it resolves to a constant repetition and a field, and
> nothing in the IR is labelled `ODOSLIDE` ([An item after a table slides, and
> the other reading is a fixed
> table](#an-item-after-a-table-slides-and-the-other-reading-is-a-fixed-table),
> #87).

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
sit outside the group it counts, and where the count comes from an earlier
record it is a register the automaton bound rather than a field of the record
being read (#35, #77). A tree carries each of those as a name path for the
consumer to resolve, which puts name resolution — and the chance of two
generators resolving one differently — into every language anyone writes a
generator in.

The cost is real, and none of it is paid by the producer. A consumer **MUST**
index the node list by identifier before it can walk anything. The [JSON
rendering](#a-descriptor-is-readable-by-a-person) (#21) is markedly harder to
read than a nested one would be. And adding a
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
| **file** | The dataset's framing, as one member of the closed set in [Physical framing](#physical-framing) together with whatever that member carries. Plus the identifier of the automaton's start state. Exactly one node of this kind exists, and it is the root. |
| **record** | The identifier of the item that is the record's top level, and the record's names. |
| **group** | An ordered list of the identifiers of its members, its names, and its repetition. |
| **variant** | An ordered list of its **arms**, each naming the predicate that selects it and the group or field that is its body. Every arm covers the same bytes, so the list is an order of evaluation rather than of position. |
| **field** | An elementary item: its width, its four resolved encoding axes, its `USAGE`, the attributes that follow from its PICTURE — category, digits, scale, and whether and where a sign is held — its names, and its repetition. |
| **slack** | A width, and nothing else: bytes that are part of the record and belong to no item. |
| **predicate** | The identifier of the field it tests, and the test itself, as one member of a closed set. |
| **state** | Whether the state accepts, the identifiers of the guards qualifying that where it is conditional, and an ordered list of the identifiers of the transitions leaving it. |
| **transition** | The identifiers of the record it admits, the state it moves to, the predicate that selects it where it carries one, the guards that make it eligible, and the bindings it applies. |
| **register** | The kind of value it holds — bytes, or an integer — and nothing else. |
| **binding** | The identifier of the register it writes, and the value written, as one member of a closed set. |
| **guard** | The identifier of the register it tests, and the test itself, as one member of a closed set. |

### Identity, ordering and determinism

- A producer **MUST** assign identifiers by a deterministic traversal of the
  resolved layout, and **MUST** emit the node list in ascending identifier
  order. Identical inputs produce byte-identical IR (#38), and a flat list puts
  the identifiers and their order inside that promise alongside the contents.
- An identifier means identity and nothing else. A consumer **MUST NOT** infer
  containment, ordering or position from one.
- Every reference **MUST**, where it is present, resolve to a node in the same
  message, of a kind the referring position admits. Most positions admit exactly
  one; a repetition's count admits a field or a register (#77), and an arm's
  body admits a group or a field (#90). One position admits no reference at all
  — a transition's predicate ([A transition may carry no
  predicate](#a-transition-may-carry-no-predicate)) — and an absent reference
  **MUST** be distinguishable from every identifier a node may carry, so that a
  consumer never reads one as the other (#80).
- A member list **MUST** be in record order — the order in which the members
  occupy bytes. Ordering is data here, not a convention a consumer restores by
  sorting: [Offsets and widths](#offsets-and-widths) makes it the only statement
  of where anything is. A variant's arms are the one ordered list that is not a
  member list: they are alternatives over one run of bytes, so their order is an
  evaluation order and says nothing about position ([A variant is chosen once
  per occurrence](#a-variant-is-chosen-once-per-occurrence), #90).
- Containment **MUST** be acyclic, and an item **MUST** be named by exactly one
  member list or by exactly one arm. Transitions are under no such rule — a
  cycle there is a file that repeats a record, which is most files.
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

Mapping identifiers to nodes, inverting a member list, adding up the widths in
front of a field, and holding a value a binding told it to hold are none of
those. They are operations on the message in hand, they are identical in every
language, and they need no COBOL. The IR abolishes the first list, not the
second.

## Offsets and widths

A field node and a slack node each carry a width in bytes. A group's width is
the sum of its members' and is not separately carried. No node carries a byte
offset.

### Ordering and width, and no offset

A field's position within its record is the sum of the widths of everything
ahead of it in the containment order, measured from the first byte of the
record's data, counting exactly one occurrence of every group that encloses it
and repeats. Where something ahead of it repeats, what enters the sum is that
item's whole extent, which is the count's and is data-dependent with it ([A
variable record is a sum with a variable
term](#a-variable-record-is-a-sum-with-a-variable-term)). The IR **MUST NOT**
carry that sum as a field of its own; a consumer computes it (#32).

The occurrence that sum lands on is the first, and saying so costs nothing
today: [A reference names a field, not an occurrence of
one](#a-reference-names-a-field-not-an-occurrence-of-one) forbids every
reference that could reach a later one, so no conforming descriptor exercises
the clause. It is stated because of what it buys later. A rule admitting such a
reference would otherwise have to say which occurrence it meant, and that is a
fact a consumer must understand in order to stay correct — breaking, under
[Versioning and compatibility](#versioning-and-compatibility). Fixed here, the
arithmetic is the same arithmetic before and after, and relaxing the
prohibition costs no version at all (#84).

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

### `COMP-6` is not `PACKED-DECIMAL`

The `USAGE` enumeration is a set of *representations* rather than of spellings:
`COMP`, `COMP-4` and `BINARY` are one member because they are one byte layout
under every dialect this project supports. `COMP-6` looks like it belongs to
`PACKED-DECIMAL` the same way and does not, and this section says so out loud
because reading one as the other is a defect nothing in a file disagrees with.

`COMP-6` (GnuCOBOL, Micro Focus) is packed decimal **with no sign nibble at
all**. `codec/SPEC.md`'s *`COMP-6`* section is normative for its bytes; the two
consequences a producer and a consumer of this IR each have to act on are:

- **It is always unsigned.** There is nowhere in the field to record a sign. A
  producer **MUST NOT** emit a field of usage `COMP_6` whose picture is signed,
  and a consumer **MUST** refuse one rather than read it as unsigned — a
  descriptor that says `S` and bytes that cannot hold one disagree about the
  data, and the reading that discards the `S` is wrong on exactly the values it
  is silent about.
- **It is `ceil(digits / 2)` bytes**, where `PACKED-DECIMAL` of the same digit
  count is `ceil((digits + 1) / 2)`. The widths coincide at every odd digit
  count and differ by a byte at every even one, so a `PIC 9(4) COMP-6` item is
  two bytes where `PIC 9(4) COMP-3` is three. That width is carried on the field
  node like every other, per [Ordering and width, and no
  offset](#ordering-and-width-and-no-offset), and a consumer **MUST NOT**
  rederive it.

A consumer **MUST** read and write a `COMP_6` field with an accessor of its own,
and **MUST NOT** substitute a packed one. The substitution is not a rounding
error: at an even digit count it consumes a byte too many and shifts every field
behind it in the record, and at every digit count it takes the item's last digit
nibble for a sign. The reverse substitution is the same defect mirrored. Neither
is detectable from the bytes alone at an odd digit count, which is why the rule
is stated here rather than left to whichever accessor a generator author reaches
for first (#162).

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
under the rule below. That last needs saying as a requirement rather than as an
observation, because splitting the alternatives is what discards the storage the
copybook gave them jointly: a producer **MUST** emit, in each alternative's
record node, a slack node covering every byte of the redefined item's storage
that the alternative's own items do not occupy (#32, #34). Left declarative it
would be circular — a record's extent is the sum of its items *and* its slack,
so an alternative emitted without one has no byte that no item occupies, and
satisfies the rule below by having nothing to satisfy it about. Those bytes are
not padding: in a file written by a program that used another variant they hold
that variant's data, which is why [Slack survives a
read](#slack-survives-a-read) has a consumer keep them rather than fill them.
Two layouts overlaying the same bytes are two paths through the automaton, which
is the shape the IR already has. The alternative was a node
kind whose only purpose is to be collapsed by every consumer in every language
before it could compute a single offset.

The cost is duplication, and the IR does not soften it. A dozen record types
sharing a thirty-item prefix carry that prefix a dozen times, and nothing says
the copies are copies: a generator wanting to emit one shared header type over a
dozen independent ones has no reference telling it which fields correspond, and
[Names](#names) denies it the option of matching on names. Emitting the dozen is
always correct. Coalescing them is a generator's judgement, and two generators
may reasonably differ.

That resolution is per *record*, and there is one place it does not reach. An
alternative becomes a record node because a record is what a transition selects;
where the redefined item is contained, at any depth, in a group that repeats,
the alternative is selected once per *occurrence* instead, and there is no
record node per occurrence for it to become. So a producer **MUST** resolve a
redefine away into record nodes exactly where the redefined item is not
contained in a group that repeats, and **MUST** emit a variant node where it is
([A variant is chosen once per
occurrence](#a-variant-is-chosen-once-per-occurrence), #32, #90). One clause
with two resolutions needs its line drawn somewhere, and it is drawn at the
question the clause itself cannot answer: whether the choice is made once for a
record or once for each entry of a table.

The paragraph above still holds where it is read strictly. The clause reaches no
node either way — a variant carries alternatives over one run of bytes and not a
redefined item beside its redefiners, so nothing in the IR says which of them a
copybook wrote first, and there is still no `REDEFINES` for a consumer to
resolve. What a variant carries is the alternation, in the one place splitting
it into records cannot express it.

### A variant is chosen once per occurrence

A **variant** node stands where a copybook overlays alternatives on one run of
bytes inside a table. It carries an ordered list of **arms**, each naming the
predicate that selects it and the group or field that is its body. Every arm
begins at the variant's first byte, so the list is an order of evaluation and
not of position; the variant itself is one item of the list containing it, in
record order like any other (#90).

A consumer walking an occurrence and reaching a variant evaluates the arms'
predicates and takes the one that matches, and the occurrence then holds that
arm's items and none of the others' — the bytes the others describe are in the
record all the same, and belong to whichever arm was selected (#90).

An arm is a pair of references and not a node of its own. Nothing points at one,
so it needs no identity, and a kind for it would be a thirteenth a consumer
switches over in order to reach two identifiers. A repetition is already carried
that way — on the group or field it belongs to rather than as a node — and for
the same reason ([The node kinds](#the-node-kinds)).

A variant **MUST** be contained, at any depth, in a group that repeats, and a
producer **MUST NOT** emit one anywhere else. That is the whole of what the kind
is for: outside a table an alternative is chosen once per record and becomes a
record node, which is the resolution above, and this kind exists only for the
choice that resolution cannot make.

Confining it there is worth more than tidiness, and the saving is that no new
prohibition is needed in the three rules that name a field. A field inside an
arm is a field inside a group that repeats, so an `OCCURS DEPENDING ON` count, a
binding's source and a transition's own predicate already cannot reach one ([A
reference names a field, not an occurrence of
one](#a-reference-names-a-field-not-an-occurrence-of-one)) — and they are
forbidden there for the reason that governs here too, which is that whether such
a field has bytes at all depends on data the consumer has not read.

**Every arm covers the same bytes, and how many is a constant.** An arm's extent
**MUST** equal every other arm's of that variant; no item of an arm **MAY**
carry a repetition whose count is a reference, at any depth; and a producer
**MUST** emit inside each arm a slack node covering every byte of that extent
the arm's own items do not occupy, which is the requirement the record-level
resolution already carries, applied one level down (#32, #34). The variant's
width is that common extent and is not carried beside it, for the reason a
group's width is not. `resolve` **MUST** reject a layout whose alternatives
cannot be made to agree, naming the record, the repeating group, the redefined
item and the arm whose extent differs rather than reporting a generic width
error (#32, #35, #90).

That one requirement is doing all the work, and what it buys is that nothing
else in this document moves. A variant contributes a constant width to the sum
in [Ordering and width, and no
offset](#ordering-and-width-and-no-offset), so every item behind it sits where
it sat. Occurrences of the enclosing group stay equal width, which is the
property [A reference names a field, not an occurrence of
one](#a-reference-names-a-field-not-an-occurrence-of-one) is built on and never
has to hear about. And a record's extent does not move with the arms chosen
inside it, so the framing check at step 5 of the read loop compares the number
it always compared, and a record carrying a variant sits on a fixed-length
dataset like a record with no table in it ([A variable record does not fit a
fixed-length dataset](#a-variable-record-does-not-fit-a-fixed-length-dataset)).
A consumer that wants a record's length and nothing else evaluates no arm at
all.

**Two arms at least, and a predicate on every one.** A producer **MUST NOT**
emit a variant carrying fewer than two arms, or an arm carrying no predicate.
Neither is a narrowing. A redefine every occurrence of which takes one
alternative resolves to that alternative's items and no variant, the way a
record-level redefine whose layout names one alternative resolves to one record
node — the overlay an adopter wrote to read a packed field as bytes is that
shape, and it reaches the IR as whichever item the layout named. And an arm
carrying no predicate would match every occurrence, which under the overlap rule
in [A predicate on an arm reads one
occurrence](#a-predicate-on-an-arm-reads-one-occurrence) leaves it the only arm,
which is that same group again.

A variant carries no names and no repetition. The alternation has no name in the
copybook — the redefined item is the first arm and carries its own — so there is
none to carry, and what a generator calls the choice is its own work like every
other identifier ([Names](#names)). It repeats by sitting inside a group that
repeats, which is the only place it may sit.

**The alternative was enumeration, and the copybook does not bound it.**
Resolving these alternatives the way the section above resolves the others means
a record node per combination of arms, and the combinations multiply over the
occurrences rather than over the redefines: ten entries choosing between two
alternatives are one thousand and twenty-four record nodes, a five-hundred-entry
table is a number with no use for a name, and under `OCCURS DEPENDING ON` the
exponent is not in the copybook at all — it is decoded out of the record being
read, so `resolve` cannot know how many record nodes to emit. Nor would there be
anywhere to select one of them from. A transition admits a whole record and a
consumer takes one transition per record ([The sequencing
automaton](#the-sequencing-automaton)), so of ten independent choices inside one
record nine have nowhere to be made, and the predicate that would make them
names a field inside a repeating group, which the rule above forbids.

So a kind is added, and the sentence the section above used to refuse one is
worth reading again rather than quietly dropped. What it refused was a node kind
whose only purpose is to be collapsed by every consumer in every language before
it could compute a single offset. A variant is collapsible by nobody: the choice
is in the data, so a consumer implementing it is doing work no producer could
have done on its behalf. That is the test the sentence was applying, rather than
a preference for fewer kinds.

**What it costs a consumer** is a second place where a predicate is evaluated,
and that is charged in every language a generator is written in. Until now
discrimination happened at one point of the read loop — at a record boundary,
before anything was admitted. A variant puts it inside the walk of a record
already admitted, once per occurrence, and [A predicate on an arm reads one
occurrence](#a-predicate-on-an-arm-reads-one-occurrence) is that evaluation's
rules. They are deliberately the rules a transition's predicate already carries,
so what a generator author implements twice is one thing implemented twice
rather than two things. The generated shape costs something as well, and this
document does not say what: an occurrence stops being one flat set of items and
becomes a choice among them, so a language with no sum type spells it as a
discriminant beside the arms, and which of the two a caller sees is the
generator's like every other question about the call ([Also out of
scope](#also-out-of-scope)).

Read-modify-write over such a record keeps its bytes, and gives them up at the
same place every other record does. Each arm's slack covers what its items do
not, so an occurrence read and handed back unchanged writes the bytes it was
read from ([Slack survives a read](#slack-survives-a-read)). A caller that
switches an occurrence from one arm to another is building an occurrence rather
than editing one, and the bytes the old arm held are gone — which is the loss
that section already describes for converting a record from one alternative to
another, arriving here one level down and no worse.

**Settled now**, because a node kind is breaking after the first release under
[Versioning and compatibility](#versioning-and-compatibility), and the failure
is the one that section names: a consumer that has not heard of this kind sees
an unset choice where a producer wrote an alternation, and generates a reader
for a record it has silently misread rather than one that refuses. #17 fixes the
schema against twelve kinds rather than eleven, and against an arm that is a
pair of references rather than a node of its own (#17, #90).

### Slack is a node, not a rule

`codec/SPEC.md` warns that a reader **MUST NOT** assume a record is the simple
sum of its fields' widths where `SYNCHRONIZED` is present: slack bytes are
inserted ahead of an aligned item, and they move everything after them. That
warning is about a record described by a copybook. It does not apply to a record
described by the IR, and this is why.

Any byte of a record that no item occupies **MUST** appear in the containment
order as a slack node carrying its width, and a producer **MUST** emit one node
per maximal run of such bytes, so that two runs of different origin that abut
are one node and not two (#34). The second half costs nothing today and stops
costing something later: [Slack survives a
read](#slack-survives-a-read) makes a record carry one run of bytes per slack
node, so how a producer divides a run of eight into nodes would otherwise decide
the shape of every record a generator emits. Alignment then stops being a
rule a generator applies and becomes bytes a generator already has, and the sum
above is literally true with `SYNCHRONIZED` present. A generator **MUST NOT**
implement an alignment rule, because there is nothing left for one to do.

A run stops at the edge of an arm, whatever abuts it there. An arm's extent is
the variant's width ([A variant is chosen once per
occurrence](#a-variant-is-chosen-once-per-occurrence)), so uncovered bytes at
the end of an arm and uncovered bytes behind the variant are two runs and two
nodes however they meet: one node covering both would belong to neither list,
and would make one arm wider than its siblings (#34, #90).

Framing bytes are not slack. A descriptor word or a delimiter belongs to the
dataset rather than to the record's data, and it is described by the file node
([Physical framing](#physical-framing)); positions here are measured from the
first byte of the record's data, whatever stands in front of it.

A slack node says how many bytes and never which, and a consumer holds the bytes
themselves all the same. That is the section below, and it binds a reader rather
than a writer: a writer has nowhere to get those bytes from if the read did not
keep them.

### Slack survives a read

What turns on the answer is the most ordinary batch job there is — read a file,
change one field of one record, write it back — so it is settled here, beside
the node, rather than in [Writing a file](#writing-a-file), where a reader's
author has no reason to look (#82).

A consumer **MUST** retain the bytes of every slack node of a record it reads,
alongside the values of that record's items. What it retains **MUST** be those
bytes as they stood when the record was read, and **MUST** stay those bytes for
as long as the record can be handed to a writer: a consumer **MUST NOT** retain
them as a view of input it may overwrite, and **MUST NOT** re-read them from the
input when a writer asks. Retention is per occurrence, as an item's value is: a
slack node inside a group that repeats stands for one run of bytes in each
occurrence rather than one run in the record. A slack node inside an arm stands
for one run in each occurrence that selected its arm and for none in the others,
so the *k*th occurrence of such a node is the *k*th occurrence to select its arm
([A variant is chosen once per
occurrence](#a-variant-is-chosen-once-per-occurrence), #90).

The lifetime is normative because the alternative conforms to everything else. A
streaming reader over a reused buffer that keeps a window onto it satisfies a
rule saying only *what* to retain, and then every record it hands a writer
carries the *next* record's slack — an error no width check and no framing check
can see, on a file that reads back as the records that were written.

A writer pairs runs to nodes and occurrences by position: it **MUST** emit, for
the *k*th occurrence of a slack node, the *k*th run the record carries for that
node, and **MUST** emit the node's width exactly. A run of any other length is
an error a writer **MUST** report rather than truncate or pad to fit — and a run
of no bytes is one of those rather than an absent one, so a consumer **MUST**
keep an absent run and an empty one distinguishable wherever it carries a
record. Runs beyond a record's occurrences are discarded.

Position is what ties a run to its place, and nothing else does: a slack node
has no name, and the IR gives a caller no offset to recognise one by. So the
pairing is stated rather than left to follow from the retention rule, which
fixes only how many runs there are. A caller that reorders, removes or inserts
occurrences moves values and does not move runs with them, and two generators
left to decide that for themselves emit different files from one descriptor and
one set of caller operations.

Where a record carries no run for a slack node, or has more occurrences of one
than it carries runs, a writer fills what is left over, and [What the descriptor
determines, a writer
supplies](#what-the-descriptor-determines-a-writer-supplies) says with what.
Those bytes were never in a file, so there is nothing there to keep.

The bytes travel with the record. A generator **MUST NOT** oblige its caller to
supply them, or to carry them across from the record a reader handed it to the
record it hands a writer: a record read and passed back unchanged writes the
bytes it was read from, with nothing asked of the code in between. Whether they
are visible in the record's public shape is the generator's, like every other
question about the call ([Also out of scope](#also-out-of-scope)) — a generator
**MAY** surface them, for a caller diffing two files or logging what it did not
understand, and one that hides them entirely satisfies this section. What no
generator **MAY** do is make surfacing them the mechanism by which they survive.
A caller obliged to copy a field across is a caller that forgets.

What retention protects is a record that reaches a writer as the record a reader
produced. A caller that builds a *new* record out of a read one's values hands a
writer a record carrying nothing, and its slack is filled like any constructed
record's. That is the ordinary shape of a job that transforms rather than edits,
and it includes the one case this section would most like to have: converting a
record from one `REDEFINES` alternative to another is building a record, and a
program doing exactly that is what laid the sibling's payload down in the first
place. So the loss described below is not abolished, only moved to where a job
is making a new record rather than editing one — and it is not softened here. An
adopter whose job builds declares the bytes it means to keep as an item and
supplies them, which is the same answer [Four framings, and none of them is a
RECFM](#four-framings-and-none-of-them-is-a-recfm) gives for a fixed-length pad.

**What is in those bytes.** Three things produce slack and they are not the same
kind of thing. A `SYNCHRONIZED` alignment gap holds nothing anybody wrote: COBOL
says nowhere what is in one, and nothing downstream reads it. The bytes a
record's items do not reach in a fixed-length dataset ([Four framings, and none
of them is a RECFM](#four-framings-and-none-of-them-is-a-recfm)) hold whatever
the program that wrote the file left there, which on these files is spaces —
a run this section can size and pair to a node only because that record type's
extent is a constant ([A variable record does not fit a fixed-length
dataset](#a-variable-record-does-not-fit-a-fixed-length-dataset), #92). And
the tail of a `REDEFINES` alternative holds *another alternative's data*
([Members never overlap, and `REDEFINES` is resolved
away](#members-never-overlap-and-redefines-is-resolved-away)) — laid down by a
program that used the other variant, on a record the one being read does not
describe. That third one sits inside an arm where the alternatives are inside a
table, and is the same bytes for the same reason ([A variant is chosen once per
occurrence](#a-variant-is-chosen-once-per-occurrence)).

A rule filling slack with a constant leaves the first alone, which is the one it
was written for, and destroys the third while rewriting the second. What makes
retention worth a requirement on every consumer is that nothing fails while that
happens. A job reads a record, changes one field and writes it back: the
truncation rule does not fire, the no-match rule does not fire, and the file
reads back as the records that were written, because the bytes that went missing
belonged to no item of any record anybody read. What is gone is the other
variant's payload, and the program that put it there finds out later, from a
record this job never looked at. The fixed-length case is the same failure with
a smaller loss and a wider blast radius: a file whose padding was spaces on
Monday holds `0x00` on Tuesday, in every record a job passed over.

That failure is also why the rule lands before the first release rather than
after. Retention is a requirement on every consumer and not a field on a node,
so a consumer that has not heard of it cannot ignore it and stay correct — which
is [Versioning and compatibility](#versioning-and-compatibility)'s test for a
breaking change, whatever protobuf would make of one that alters no schema at
all.

**One answer for all three, so the node carries no kind.** The three could have
been told apart, by a member on the slack node saying which produced it, and
under retention nothing would read it. Retaining an alignment gap costs what
retaining anything costs and loses nothing, since what is retained is what was
there. A consumer filling one kind with zero and keeping the other two would
have to know which it held in order to write a file that differs from its input
in the one place nobody can name a better value for.

The member would not be free, either. It is a closed set, so a producer of slack
nobody anticipated is breaking to admit afterwards — and the question was posed
as a fork between two producers when the fixed-length pad, which is neither,
turns out to be the common one. Nor is attribution as crisp as three names make
it sound. A producer emits one slack node per maximal run of uncovered bytes
([Slack is a node, not a rule](#slack-is-a-node-not-a-rule)), and a `REDEFINES`
alternative shorter than its sibling and followed by an aligned item puts a tail
and an alignment gap into a single run of them. A kind would have to split that
node by cause, against the rule that made it one node, and a consumer would then
hold two runs of bytes where the file has one — bought for a distinction nothing
reads.

Retaining the record's whole bytes instead — every byte, covered by an item or
not — was the other alternative, and it fails the test this document keeps
applying. It states every byte twice, once as an item's value and once in the
run, so a writer handed a record whose caller changed one field has two answers
for those bytes and needs a rule saying which wins. Retaining exactly the bytes
no item covers leaves every byte of a record with one source.

The cost is memory, it lands on every consumer in every language, and it is not
softened here. A reader holds bytes most callers will never look at — eight in
every record of an eighty-byte fixed-length file — and it holds them per
occurrence where a slack node sits inside a table, so an alignment gap in a
five-hundred-entry table is five hundred runs and not one. What that costs turns
on a representation this document leaves to the generator, and its two ends are
far apart: a run held inline in a fixed-width field of the record costs
nothing a reader was not already paying, and one allocated per run per record
costs an allocation per run per record. It is charged on the reading side
because there is nowhere else to charge it: a writer holding a record and a
descriptor knows how many bytes no item covers and cannot know what was in them.

### A variable record is a sum with a variable term

An item that repeats carries its repetition: a constant count, or — for `OCCURS
DEPENDING ON` — a reference to the count, together with the minimum and maximum
number of occurrences the copybook declared. Its extent is the width of one
occurrence times that count, and the item behind it begins at the byte after
the last occurrence that count states ([An item after a table slides, and the
other reading is a fixed
table](#an-item-after-a-table-slides-and-the-other-reading-is-a-fixed-table)).

That reference **MUST** name either a field node contained in the record being
read, at any depth, or a register node the automaton has bound. It **MUST NOT**
name a field of another record (#35, #77). The withdrawn form was the honest
shape of the problem and not a solution to it: naming a field of a record the
consumer is no longer looking at names bytes it does not have, and no rule about
references gives those bytes back.

That reference **MUST NOT** name a field that repeats, or one contained at any
depth in a group that repeats, either (#35, #84). That is the rule [A reference
names a field, not an occurrence of
one](#a-reference-names-a-field-not-an-occurrence-of-one) states for every
position that names a field, and it is what keeps the multiplication above a
multiplication.

Both kinds carry one more restriction, and it is about when the value can be
read rather than about what it names: a field count lies ahead of the item it
counts and at a constant position, and a register count was bound by a
transition taken strictly earlier than the one admitting the record. [A count is
in hand before the extent it
decides](#a-count-is-in-hand-before-the-extent-it-decides) states both and
argues them (#88).

Neither kind is one-to-one. Two repeating items of one record **MAY** name the
same count, and a consumer decodes it once and sizes both of them from that one
value — so reading is where the sharing is invisible, and what it costs falls
on a writer. Both halves are [One count may size two tables, and a writer
refuses to
choose](#one-count-may-size-two-tables-and-a-writer-refuses-to-choose)'s (#89).

Where the count does come from an earlier record — a header saying how many
entries a later record's table holds, which is ordinary in production layouts —
the automaton binds that field into a register as it admits the header, and the
repetition names the register. Which occurrence governs, in a file holding a
thousand headers, is then not a question anyone has to answer twice: a register
holds what the most recent binding put in it, so the count is the one from the
nearest preceding record that bound it, along the path actually taken. [The
automaton remembers, in
registers](#the-automaton-remembers-in-registers) specifies that mechanism.

A consumer **MUST** report a count it cannot decode as a number, or one that is
negative, as malformed data, and **MUST NOT** read the group as absent instead
(#35). A count field holding spaces is a real mainframe occurrence, and reading
it as zero produces a record that parses and is wrong.

A count below the declared minimum or above the declared maximum is malformed
data too, and a consumer **MUST** report it rather than reading that many
occurrences (#35, #87). The bounds are carried for that check and for nothing
else: no other position in this document reads them, and a producer **MUST**
emit the copybook's own `OCCURS integer-1 TO integer-2` rather than bounds a
layout would prefer. Without them the check does not exist, because there is
nothing in a record to make it against, and the framing does not disagree with a
count the copybook forbids: a descriptor word stating the length that count
implies, or a delimiter standing where it ends, agrees with the extent exactly,
so the check in [The extent governs, and framing is checked against
it](#the-extent-governs-and-framing-is-checked-against-it) passes on a record
the copybook says cannot exist — which is what a file written against a later
version of that copybook looks like. The bounds are data the copybook already
states and the only thing in reach that bounds a number decoded out of a file.

Where the count is data-dependent, the sum above is data-dependent with it, and
that is the whole of the model. A generator emits an addition it performs while
reading rather than a constant it was handed. A carried offset could not have
described this without a second, different mechanism for every field following a
variable group; ordering and width describe it with none.

Not every file may hold such a record all the same, and what decides is the
framing rather than anything here: a dataset whose own stride is fixed requires
a record type's extent to be a constant, which is [A variable record does not
fit a fixed-length
dataset](#a-variable-record-does-not-fit-a-fixed-length-dataset)'s (#92).

### An item after a table slides, and the other reading is a fixed table

The sum above has a premise no node carries and every consumer acts on: an item
following a repeating item begins at the byte after the last occurrence the
count states. The table **slides**, and the items behind it slide with it. There
is one reading of this, every conforming file has it, and nothing in the IR says
which reading a file was written under (#87).

That is IBM Enterprise COBOL's layout and it is unconditional there. It is not
everyone's. Micro Focus makes it the `ODOSLIDE` directive, and under
`NOODOSLIDE` the items behind a table keep fixed addresses beginning after the
space allocated for it at its maximum length, with every group containing the
table considered as always having the maximum number of occurrences. GnuCOBOL
carries the same switch as a dialect option, off by default and implied by
`-std=ibm`. [`layout/SPEC.md`](../layout/SPEC.md) names GnuCOBOL and Micro Focus
among the producers whose files are in scope, so both sides of that fork are
files an adopter has, and a consumer reading one as the other is wrong at every
item behind the table — silently, at every record, and under **unframed** with
nothing in the file to disagree with it. Choosing by omission was what this
section found and is what it ends.

Neither vendor is excluded all the same, because the other reading is not a
second arithmetic. Read what `NOODOSLIDE` lays down and there is no
variable-length table in it: the occurrences are at their maximum, the items
behind them are at constant offsets, every group containing the table is its
maximum width and so is the record. Those are the bytes of a *fixed* table
beside a field saying how many entries the writing program filled — an ordinary
copybook shape this document has always described, with a constant repetition
and a field. So the fork is one the layout format records as a setting and
`resolve` resolves, exactly as it records a RECFM spelling and resolves a
framing. A layout that names a record carrying an `OCCURS DEPENDING ON` says
which reading its file was written under, and `resolve` **MUST** reject one that
does not, naming the record and the table rather than taking either side by
default (#27, #35). There is nothing to fall back on: the two readings put every
item behind the table somewhere different, and nothing in the file disagrees
with the wrong one. A sliding file's table then resolves to a repetition whose
count is a reference. A non-sliding file's table resolves to a repetition whose
count is the constant its copybook declared as the maximum, and the count field
to a field of the record like any other.

Carrying the reading instead was the obvious alternative, and what decides
against it is not tidiness but that the second arithmetic needs somewhere to put
bytes and this document has nowhere. Under it the bytes between the last
occurrence the count states and the end of the table's allocation are bytes no
item covers, which is slack ([Slack is a node, not a
rule](#slack-is-a-node-not-a-rule)) — and a slack node carries a width, while
that run is the maximum less the count, occurrences wide. Carrying the reading
therefore means a slack node whose width is data-dependent, or a second
retention channel beside slack's in every consumer in every language, for a run
of bytes a constant repetition already describes exactly. That cost is the same
wherever the member is put, so neither placement escapes it: on the file node it
is one statement a consumer switches a second arithmetic on, and on the
repetition it is that plus a descriptor no compiler could have produced — one
file whose tables slide and whose other tables do not, emitted by a producer
that is wrong in the way this document keeps refusing, with nothing in the file
to disagree with it.

Three things are given up with the resolution, and none is softened here.

**The count stops being the descriptor's.** On a non-sliding file the count
field governs no byte, so nothing in the IR points at it: a consumer hands its
caller the maximum occurrences and the caller reads the count field to learn how
many the writing program filled. [What the descriptor determines, a writer
supplies](#what-the-descriptor-determines-a-writer-supplies) then has nothing to
determine — a writer emits the maximum occurrences, and the count field's value
is its caller's like any other field's. That is the honest description of the
bytes rather than a concession. A count that moves nothing is an application's
fact about a fixed table, and a fixed table with a counter beside it is a
copybook shape this document has always carried without saying anything about
the counter.

**The copybook's bound stops being checkable.** The requirement above that a
count outside its declared minimum and maximum is malformed data has nothing
left to fire on, since the resolved form holds no count reference. The loss is
real and it is smaller than on the other side of the fork: a count out of bounds
misplaces every item behind the table under the sliding reading and misplaces
nothing here, so the file is still read correctly and what the caller has is a
number its own program has to distrust.

**The unused occurrences are values, not retained bytes.** Occurrences past the
count hold whatever the writing program left there, and under this resolution
they are items with widths and encodings rather than bytes a consumer keeps
([Slack survives a read](#slack-survives-a-read)). A shop whose unused entries
hold spaces where a PICTURE says packed decimal has entries that do not decode.
That is not a failure this reading introduces: it is the standing property of
every fixed table whose program filled three of five hundred entries, and
describing the non-sliding table any other way would give one run of bytes two
descriptions.

Two consequences fall out where a reader may go looking for them. A record of a
non-sliding file has a constant extent, so it meets the requirement a
fixed-length dataset places on its record types ([Four framings, and none of
them is a RECFM](#four-framings-and-none-of-them-is-a-recfm)) exactly as a
record with no table does; a *sliding* record may not sit on such a dataset at
all, which is [A variable record does not fit a fixed-length
dataset](#a-variable-record-does-not-fit-a-fixed-length-dataset)'s, and is why
that question was about half this fork rather than all of it (#92). The reading
this section records is the escape there, wherever a table has nothing behind
it. And a non-sliding record carries no repetition whose count is a
reference, so a discriminator **MAY** name a target behind its table, which
[Discriminator predicates](#discriminator-predicates) forbids only where
something ahead of the target is variable.

Settled now rather than later, for the reason its neighbours are. Carrying the
reading afterwards is an addition a consumer must understand in order to stay
correct, which [Versioning and
compatibility](#versioning-and-compatibility) counts as breaking however
protobuf would classify it — and the declared minimum and maximum are fields on
a repetition, which #17 fixes the schema against in the same change (#87).

### A reference names a field, not an occurrence of one

Three positions in this document name a field node and expect a value out of it:
a [predicate](#discriminator-predicates)'s target, an `OCCURS DEPENDING ON`
count, and the field a [binding](#the-automaton-remembers-in-registers) reads. A
field node carries its own repetition and a group carries its own, so each of
the three could name a target that repeats, or one sitting inside a group that
repeats. None of the three carries an occurrence number.

So none of them may name one. A reference in any of those three positions
**MUST NOT** name a field that repeats, or a field contained at any depth in a
group that repeats, and `resolve` **MUST** reject such a layout, naming the
record, the field and the enclosing group that repeats rather than reporting a
generic reference error (#35, #36, #37). #76 settled this for bindings; it is
the same rule in all three places, and this is the one place it is argued.

A fourth position names a field and is outside all of it, because it carries the
occurrence the other three lack. The predicate selecting an arm of a variant is
evaluated inside one occurrence of the group holding it, so its target **MUST**
be contained in a group that repeats — the very thing forbidden above — and
nothing is guessed by reading it, since the occurrence in question is the one
the consumer is walking. What follows refuses a reference that has no occurrence
to be read in; it is not about the word *repeats*, and [A predicate on an arm
reads one occurrence](#a-predicate-on-an-arm-reads-one-occurrence) is where the
fourth position's own rules are (#90).

The shared reason is not that the arithmetic is undefined. [Ordering and width,
and no offset](#ordering-and-width-and-no-offset) settles what the sum reaches —
the first occurrence — so a consumer following this document lands somewhere
definite. It is that the descriptor does not say the first occurrence is what
the layout *meant*. One reference against forty entries records an intention it
cannot express, and reading it as the first is a guess about that intention
rather than a reading of the descriptor. A guess is worst exactly where it looks
best: entry one's count taken for the table's count is a plausible answer a
consumer cannot detect is wrong, which is why the rule binds a binding as
tightly as it binds the other two.

COBOL offers nothing to resolve the intention with, either. Qualification is
[Names](#names)'s chain of enclosing nodes and names a group rather than an
occurrence; an occurrence is picked by a subscript, and a subscript cannot stand
where one would be needed, since the data-names in an `OCCURS` clause may be
qualified but not subscripted or indexed. That rule is cobol-go's root
`SPEC.md`'s to state and this document's to rely on. The copybook never carried
an occurrence here, so the IR is not dropping one.

The count has a second reason, and it is the half of the rule that decides the
shape of everything above it. A count contained in a group that repeats is a
count with one value per occurrence of that group, and then the occurrences are
not all the same width. Refusing that is what keeps a group's occurrences equal:
a count is a constant, a field occurring once in the record, or a register, and
a register holds one value for the whole of a record's read ([The automaton
remembers, in registers](#the-automaton-remembers-in-registers)) — so every
occurrence of a group is the same width whatever is nested inside it. A variant
nested inside one leaves that alone rather than being an exception to it: its
arms are of one constant extent, so an occurrence holding a variant is the width
of an occurrence holding a group ([A variant is chosen once per
occurrence](#a-variant-is-chosen-once-per-occurrence)). The other
half of the rule, that the count must not itself repeat, does no work here; it
is carried by the invention reason alone.

What equal width buys is not merely a tidier sum. "The width of one occurrence
times that count" becomes a running total rather than a closed form, which
costs a consumer a loop it is largely walking anyway; what it also costs is
[slack](#slack-is-a-node-not-a-rule), and that one is not a cost but an
impossibility. An aligned item inside a variable-width occurrence needs a
different number of padding bytes in each one, a slack node carries a single
width, and a generator **MUST NOT** implement an alignment rule to make up the
difference. The record could not be described at all.

Both halves of that argument are made against the reading [An item after a table
slides, and the other reading is a fixed
table](#an-item-after-a-table-slides-and-the-other-reading-is-a-fixed-table)
fixes, and they need it: "the width of one occurrence times that count" is the
sliding layout written as arithmetic, and asserting that every occurrence of a
group is the same width without saying which layout it is the same width under
is asserting it of one vendor's files and hoping (#87). The other side of the
fork never reaches this rule, because a non-sliding table resolves to a
repetition with no reference in it and a count field beside a fixed table is a
field.

The predicate has a second reason too, and it is that the target may not be
there at all. A predicate is evaluated before its record is admitted, against
bytes read as the record its transition would admit ([Where framing is consumed,
and where it is
emitted](#where-framing-is-consumed-and-where-it-is-emitted)). A count of zero
is well-formed wherever the copybook's declared minimum admits one, and a count
those bounds *do* exclude is no help here either, since every check this
document makes on a count runs against a record already admitted ([A variable
record is a sum with a variable
term](#a-variable-record-is-a-sum-with-a-variable-term)) — so a target inside
such a group has no bytes at all, and the sum of the widths ahead of it lands on
whatever follows the group instead. That is not an invented occurrence but a
misread of another item's bytes, on a record that is otherwise well-formed, at
the one moment the consumer has not yet decided what it is looking at.

The reading this refuses is worth naming, because it is the one an adopter
expects: a count sitting beside the table it governs, both inside a group that
repeats, meaning "the count in this occurrence governs this occurrence". It is
an expectation rather than a file shape. A copybook cannot write it — the object
of an `OCCURS DEPENDING ON` may not occupy storage inside the range of the table
it controls, nor be variably located, and a count in the second occurrence of an
enclosing group is both. That is cobol-go's root `SPEC.md`'s rule to state and
this document's to rely on; the compilers this project reads files from enforce
it, and the shape recurs in adopters' intentions rather than in their data. So
the refusal costs less than it appears to, and it lands where COBOL's own
already does.

What a copybook *can* write is the same table with its count hoisted out of the
repeating group, which is a documented form of complex `OCCURS DEPENDING ON` —
and that form this document admits unchanged, because a hoisted count occurs
once and the occurrences stay equal width. The rule turns away the shape the
compiler turns away and keeps the shape the compiler keeps.

Settled now rather than left open, because the direction that matters is not
cheaper later. Narrowing what a reference may name after the first release is a
breaking change, and so is widening it by carrying an occurrence number, which
is an addition a consumer must understand in order to stay correct ([Versioning
and compatibility](#versioning-and-compatibility)) however protobuf would
classify it. Relaxing the prohibition without carrying one is the exception, and
only because [Ordering and width, and no
offset](#ordering-and-width-and-no-offset) fixes the arithmetic now. What a
repetition's count reference admits is therefore the two node kinds [Identity,
ordering and determinism](#identity-ordering-and-determinism) already names and
no occurrence, and #17 fixes the schema against that (#84).

### A count is in hand before the extent it decides

[A variable record is a sum with a variable
term](#a-variable-record-is-a-sum-with-a-variable-term) has a premise about time
rather than about bytes: the count's value is available at the moment the extent
is needed. That moment is step 4 of the read loop, where the transition has been
taken and the record admitted ([Where framing is consumed, and where it is
emitted](#where-framing-is-consumed-and-where-it-is-emitted)). Two shapes
satisfy every requirement stated so far and cannot be read there, one for each
kind a count reference admits. Both are settled here (#88).

**A count that is a field lies ahead of what it counts, at a constant
position.** Where a repetition's count names a field node, that field's last
byte **MUST** lie ahead of the first byte of the item whose repetition names it,
in the record's order, and no item ahead of that field **MAY** carry a
repetition whose count is a reference. `resolve` **MUST** reject a layout
breaking either half, naming the record, the count field and the repeating item
rather than reporting a generic reference error (#35).

The first half is what makes the sum terminate. A record whose member list is a
table followed by the count that sizes it satisfies every requirement above: the
count is a field of the record being read, it does not repeat, and it is not
inside a group that repeats. Its position is still the sum of the widths ahead
of it, that sum includes the table's extent, and the table's extent is one
occurrence times the count — so decoding the count requires locating it and
locating it requires decoding it. [Identity, ordering and
determinism](#identity-ordering-and-determinism) makes *containment* acyclic;
nothing made the reference graph acyclic where it feeds a position sum. So the
descriptor stays internally consistent and the record's extent is undefined,
while every consumer is entitled to assume the sum terminates.

The escape a framed dataset appears to offer is refused. Under
**descriptor-word** and **segmented** the record's length is in hand by step 3,
so a consumer could count back from the record's end to a trailing count and
carry on. That makes the extent follow from the framing, which [The extent
governs, and framing is checked against
it](#the-extent-governs-and-framing-is-checked-against-it) has exactly
backwards, and it empties the check at step 5 into a comparison of the framing
against a number derived from it — the tautology [A predicate always names a
field](#a-predicate-always-names-a-field) names when it refuses a length
predicate. It would also work on two framings out of four, so one record
description would be readable on a dataset and unreadable on another, which is a
fork nobody stated. Under **unframed** and **delimited** there is nothing to
count back from at all.

The second half asks for more than termination needs, and it is taken for what
declining it would cost later. A count sitting behind some *other* variable item
is readable: a walk in record order reaches that item's own count first, so the
count behind it has a position by the time anything needs one, and the same
induction covers any depth of nesting. It is also a count no compiler writes.
Micro Focus states the rule for the `OCCURS` clause flatly — *Data-name-1 must
have a fixed location, and must not follow an item that contains an OCCURS
DEPENDING ON clause* — beside the restriction that the object may not occupy a
position within the table's range, which is the half [A reference names a field,
not an occurrence of
one](#a-reference-names-a-field-not-an-occurrence-of-one) already leans on. So
the narrower rule turns away no file anybody has.

It is also the answer that can be undone. Relaxing the second half later
costs no version, because the position sum is the same running sum before and
after — the escape [Ordering and width, and no
offset](#ordering-and-width-and-no-offset) names for the occurrence clause,
available here for the same reason and stated now so that it stays available.
Imposing it later makes a conforming descriptor stop conforming, which is
breaking under [Versioning and
compatibility](#versioning-and-compatibility).

A predicate's target carries the same constant-position restriction and does not
get it from here. A predicate is evaluated at step 3, before the record is
identified, so a variable position obliges a consumer to decode a count out of
bytes it has not decided the meaning of, and [Discriminator
predicates](#discriminator-predicates) argues it there. A count is read at step
4 with the record known, so nothing in the read loop forces the restriction on
it and the two paragraphs above are the whole of its case. The third position
that names a field — the field a
[binding](#the-automaton-remembers-in-registers) reads — carries neither
restriction and needs neither, since a binding applies at step 7 against a
record whose every position is already a number.

A writer is not the side that fails, which is why this is refused at the layout
rather than left to be noticed. A writer holds the number of occurrences its
caller gave it and could lay a trailing count down as readily as a leading one
([What the descriptor determines, a writer
supplies](#what-the-descriptor-determines-a-writer-supplies)). What it would
produce is a file its own reader cannot walk, out of a descriptor that says
nothing is wrong.

None of this reaches a record of a non-sliding file, which carries no count
reference for either half to bind ([An item after a table slides, and the other
reading is a fixed
table](#an-item-after-a-table-slides-and-the-other-reading-is-a-fixed-table)).

**A count that is a register was bound by an earlier transition.** A repetition
naming a register reads the register file as it stood on entry to the state,
exactly as a guard does: the extent is needed at step 4 and a transition's
bindings apply at step 7, so nothing a transition reads ever sees what that
transition writes. A repetition therefore **MUST NOT** name a register unless
every path from the start state to the transition admitting its record passes
through a binding of that register on a transition taken strictly earlier. A
binding on the admitting transition itself does not satisfy that, and [A
register is read only where it has been
written](#the-automaton-remembers-in-registers) is worded so that it cannot be
read as though it did (#36).

The shape being refused is a transition that admits a record, binds a register
from a field of that record, and admits a record whose own repetition names that
register. One reading of *passes through a binding first* lets it through, since
the path does reach a binding. What a consumer does with it is fixed by the step
order and is not what such a producer meant: on the first admission the register
holds nothing, and on every later one it holds the previous record's count. The
first is a malformed descriptor reported against IR whose producer believed it
conforming; the second is a record read at the wrong length, with nothing in the
file to disagree.

Nothing is lost by refusing it, because the shape has a spelling already. A
count that governs the record holding it is a field of that record, and the
repetition names that field — which is the whole of [A variable record is a sum
with a variable term](#a-variable-record-is-a-sum-with-a-variable-term)'s first
case. A register is for the other one, a value governing a record other than the
one it sits in ([When a value becomes a state, and when it becomes a
register](#when-a-value-becomes-a-state-and-when-it-becomes-a-register)), and
routing a count through one back into the record it was read from asks it to
hold a value before the read that fills it.

What stays admissible is a register bound earlier and rebound by the transition
that admits the record. A transition that takes one off a counter while
admitting a record whose table that same counter sizes reads the value the
counter held on entry to the state, before the subtraction, and every path
reached a binding of it before this transition was taken. That is not an
exception carved out for it: it is the sentence guards already carry, applied at
the one other place a register is read.

Both halves are narrowings, both are breaking after the first release, and
neither adds a field. A count reference stays the two node kinds [Identity,
ordering and determinism](#identity-ordering-and-determinism) already names, a
repetition gains nothing beside the bounds it carries, and #17 fixes the schema
against what it already had (#88).

### One count may size two tables, and a writer refuses to choose

Nothing above makes a count reference one-to-one. Two repeating items of one
record **MAY** name the same count, and so **MAY** any number of them: a
reference is a reference, and the field it names does not become the property of
the first item that named it (#89).

The shape is not an exotic one. It is the plainest record with two variable
tables in it, because two tables under separate counts oblige a record to carry
both count fields ahead of both tables — a count may not sit behind a variable
item ([A count is in hand before the extent it
decides](#a-count-is-in-hand-before-the-extent-it-decides)), and Micro Focus
states as much of the clause's own object, *data-name-1 must have a fixed
location, and must not follow an item that contains an OCCURS DEPENDING ON
clause*. One count ahead of two tables satisfies that with nothing arranged. The
pair is IBM's documented complex `OCCURS DEPENDING ON` either way, an entry
carrying the clause followed by a non-subordinate entry carrying another, and
neither vendor says how many entries may name one object. They say where the
object may sit, and one object named twice sits in one place.

Reading never notices. A consumer decodes the field once and sizes both tables
from that one value — two multiplications against one number where it expected
one against each of two — and the tables need not share a width per occurrence,
need not be adjacent, and need not declare the same bounds. Nothing above gains
a clause for it: [A variable record is a sum with a variable
term](#a-variable-record-is-a-sum-with-a-variable-term)'s sum is the same sum
with one of its terms read twice, and every consumer walking such a file walks
it identically.

The declared bounds are the one place the second reference is not simply a
second multiplication. Each repetition carries its own minimum and maximum and
both bind the one value, so a consumer **MUST** check the decoded count against
every repetition naming it and the range a record can actually carry is the
overlap of the declared ones rather than either. That is the existing check run
per repetition rather than a new one, and it is what carrying the bounds on the
repetition rather than beside the count buys.

Where the declared ranges do not overlap at all, no value sizes both tables, and
`resolve` **MUST** reject such a layout, naming the record, the count and both
repeating items rather than reporting a generic reference error (#35, #89). The
rejection turns away no file a consumer could have read: every record of such a
descriptor is malformed data under the check above, so what is refused is a
copybook whose two `OCCURS integer-1 TO integer-2` clauses cannot both hold, and
the alternative is that diagnostic once per record for the life of the file. The
check is over repetitions of one record, which is the whole of what it can be. A
count that is a register falls under it where two repetitions of one record name
that register, and under nothing where they sit in different records — the
register is free to hold a different value at each of those.

The writing side is where sharing costs something, and it is the only side.
[What the descriptor determines, a writer
supplies](#what-the-descriptor-determines-a-writer-supplies) makes an `OCCURS
DEPENDING ON` count the writer's to emit rather than its caller's — the field's
value *is* the number of occurrences — so two repetitions naming one field
determine that field twice. Where the caller's numbers agree they determine it
to one value and there is nothing to settle. Where they disagree there is no
value the record can carry: the first table's number sizes the second wrong and
slides every item behind it ([An item after a table slides, and the other
reading is a fixed
table](#an-item-after-a-table-slides-and-the-other-reading-is-a-fixed-table)),
the second's does the same to the first, and either way a writer that picks
emits a file its own reader mis-walks out of a descriptor saying nothing is
wrong. So it reports instead, and the requirement is stated where the
requirements on writers are.

A count that is a register shares without any of that, and the asymmetry is
worth stating rather than leaving to be re-derived. Nothing determines a
register from occurrences: it holds what a transition bound out of a record
already emitted, and the occurrences are what has to agree with it ([A writer
evaluates a guard, it never back-fills a
count](#a-writer-evaluates-a-guard-it-never-back-fills-a-count)). Two
repetitions naming one register are two comparisons against a value neither of
them sets, which is that requirement made twice and not a new one. The conflict
belongs to determination, and only a field count is determined.

Sharing is also not confined to two items side by side, which is why the
writer's comparison is stated over every repetition naming the count and not
over a pair. A table inside a group that itself repeats on the same count is
admissible — the count occurs once in the record, so [A reference names a field,
not an occurrence of
one](#a-reference-names-a-field-not-an-occurrence-of-one) admits it, and every
occurrence of the outer group is the same width because one value sizes all of
them. The numbers that have to agree are then one for the outer group and one
per occurrence of it, and a writer comparing two of them emits a record whose
third occurrence is short.

Refusing the shape at `resolve` was the other answer, and it is the direction
this document ordinarily prefers: narrowing what a descriptor may say is
breaking after the first release and widening it is free, so a rejection made
now can be lifted later at no version, which is the argument [A count is in hand
before the extent it
decides](#a-count-is-in-hand-before-the-extent-it-decides) took a narrowing on.
The other half of that argument decides here, and it decides the other way. The
narrowing there turned away a count no compiler writes; this one would turn away
a record IBM documents and an adopter has. A rejection that is cheap to lift is
still, until it is lifted, a file nobody can read.

A `resolve` rejection is a statement about the descriptor, besides, and the
descriptor is not what is in doubt. A file written under a shared count is read
identically by every consumer, at every record, in both directions — no fork, no
guess, and nothing in the file to disagree with. What can be wrong is one
caller's data on one call, which is where [Writing a file](#writing-a-file)
already puts every wrongness of that shape: a predicate the record does not
satisfy, a guard the register file contradicts, a number of occurrences outside
the declared bounds. Each is reported where the mistake is made, and none of
them is a reason to refuse a layout.

Nothing is added to carry the answer on either side of it. A count reference
stays the two node kinds [Identity, ordering and
determinism](#identity-ordering-and-determinism) already names, `resolve` checks
bounds it already holds, and a writer compares numbers of occurrences it already
has in hand — so #17 fixes the schema against what it had, and #51 and #52 gain
a diagnostic rather than a field (#89).

## Physical framing

A record's items say where its bytes sit relative to each other. Nothing above
says where the record itself sits. Between one record's last byte and the next
record's first there may be a descriptor word, a delimiter, or nothing at all,
and a consumer that assumes the wrong one reads every record after the first at
the wrong offset.

The file node answers that as the dataset's **framing**: one member of the
closed set below, together with whatever that member carries. Framing bytes
belong to the dataset and not to any record. No item covers them, they are not
slack ([Slack is a node, not a rule](#slack-is-a-node-not-a-rule)), and no
predicate ever sees one.

This is the one part of the IR with no single source above it. `codec/SPEC.md`
excludes record formats, descriptor words and line terminators by name; DFSMS
defines the descriptor words and is cited in [Governing
sources](#governing-sources) for them; and what ends a line-delimited record is
defined by nobody, which is the first thing this section has to deal with (#26,
#78).

### Four framings, and none of them is a RECFM

A file node's framing is one member of this closed set, and adding a member is a
breaking change under [Versioning and
compatibility](#versioning-and-compatibility). The set is settled here rather
than grown afterwards, for the reason the node kinds and the guard tests are.

| Framing | Carries | Where one record's bytes are |
|---|---|---|
| **unframed** | nothing | its extent, beginning at the byte after the record before it |
| **descriptor-word** | nothing | its extent, preceded by a record descriptor word |
| **segmented** | the largest segment a writer may emit | the concatenation of its segments' data, each segment preceded by a segment descriptor word |
| **delimited** | the delimiter's bytes and its placement | its extent, with the delimiter around it as the placement says |

*Extent* throughout means the sum of the widths of a record's items and slack in
containment order — the quantity [Offsets and widths](#offsets-and-widths)
already defines, and the only statement of a record's length this document
makes.

The members are framings rather than the RECFM letters an adopter writes in
JCL, because the reader of this document is a generator author who has never
seen a DD statement and needs to know what to *do*. Several of those letters
also say the same thing to a consumer, and one of them says nothing it can act
on at all. A layout file keeps the adopter's spelling, which is
[`layout/SPEC.md`](../layout/SPEC.md)'s stated policy for exactly the mirror
reason, and `resolve` maps it (#26):

| A dataset spelled | resolves to |
|---|---|
| RECFM F, FB | **unframed** |
| RECFM V, VB | **descriptor-word** |
| RECFM VBS | **segmented** |
| line sequential, and the line-delimited "RECFM=V" of GnuCOBOL and Micro Focus | **delimited** |
| RECFM U | nothing; the layout is rejected ([Undefined-length records](#undefined-length-records)) |

Blocking is absent from the right-hand column on purpose: a blocked dataset is
ordinary here, because what blocking is on the mainframe is not what arrives on
a filesystem. [Block descriptor words in the
stream](#block-descriptor-words-in-the-stream) is where that is argued.

**unframed** does not require two record types to have the same extent. The
read below works whether or not they agree, so a rule demanding it would forbid
only files that read correctly. What a fixed-length dataset *does* require is
that its record types account for all of LRECL, since the next record starts at
that fixed distance regardless: a record type whose items stop at 72 bytes of an
80-byte record carries the remaining 8 as slack, and a layout in which it does
not describes a file whose reader misaligns after the first record (#26, #34).
One class of record type cannot meet that requirement at all, and it is refused
here rather than left to be found: a record type whose extent moves with a count
has no single number of bytes to pad, which is [A variable record does not fit a
fixed-length dataset](#a-variable-record-does-not-fit-a-fixed-length-dataset)
(#92).

Those 8 bytes are then bytes no item covers, and what becomes of them is [Slack
survives a read](#slack-survives-a-read)'s: a record read from such a file
carries them and a writer puts them back, so a job that reads this file and
writes it out again leaves whatever the program before it left there. A record
its caller built rather than read has nothing to put back and gets zero bytes,
which is common enough on a fixed-length dataset to be worth stating where the
padding is introduced: an adopter who wants those 8 to hold spaces declares a
trailing item and supplies it, rather than leaving them to slack (#82).

### A variable record does not fit a fixed-length dataset

A descriptor whose file node's framing is **unframed** **MUST NOT** carry a
record type whose extent is data-dependent: no item of such a record type
**MAY** carry a repetition whose count is a reference, at any depth. `resolve`
**MUST** reject a layout that puts one on a fixed-length dataset, naming the
record and the repeating item rather than reporting a generic framing error
(#26, #35). Where the count comes from makes no difference — a register the
automaton bound moves a record's extent exactly as a field of the record does
([The automaton remembers, in
registers](#the-automaton-remembers-in-registers)) — and neither does where the
table sits, which decides only what the diagnostic says.

**unframed** is what RECFM F and FB resolve to and nothing else does, so this is
a rule about a dataset rather than about a framing's bytes. It is keyed on the
framing all the same, because it holds whatever LRECL a layout states and
whether or not it states one. On that dataset the next record begins a fixed
distance on whatever the record was, which turns *account for all of LRECL* into
a requirement that a record type's extent *be* LRECL. A record type of
constant extent meets it by carrying the difference as slack. One whose extent
is a fixed part plus a table plus a pad meets it at one count and misses it at
every other: behind four bytes of fixed items, a table of eight-byte entries in
an eighty-byte record leaves 36 bytes for the pad at five entries and 52 at
three, and a slack node carries one width ([Slack is a node, not a
rule](#slack-is-a-node-not-a-rule)). No number is both.

The read side breaks first, and it breaks on a file nobody wrote wrongly. Take
that record with its pad set to 36, so that a five-entry record is eighty bytes,
and a file whose first record holds three entries and whose second holds five. A
consumer takes the first record's end from its extent ([The extent governs, and
framing is checked against
it](#the-extent-governs-and-framing-is-checked-against-it)) and advances 64,
while the file advanced 80. **unframed** has nothing to check that against: it
is the framing that same section describes as the one where nothing in the file
disagrees with anything. So every record after the first is read at the wrong
offset, in silence, which is the failure [Physical framing](#physical-framing)
exists to prevent.

Letting the pad take up the difference is the other answer, and it is a
mechanism rather than a relaxation: a slack node whose width is LRECL less the
extent of the record admitted. Both halves of it are refused already, and either
alone decides it. The width would be data-dependent, which is the wall [An item
after a table slides, and the other reading is a fixed
table](#an-item-after-a-table-slides-and-the-other-reading-is-a-fixed-table)
hits from the other side and over the same run of bytes — a slack node carries a
width, and [Slack survives a read](#slack-survives-a-read) pairs one run to one
node by position, so a run whose length is not a number is a second retention
channel beside slack's in every consumer in every language. And LRECL would have
to sit on the file node, which [Lengths the file node does not
carry](#lengths-the-file-node-does-not-carry) refuses as a fact stated twice
that a consumer could do nothing with a disagreement about. Under this mechanism
it would not be a duplicate but a governor: the file node's number would decide
how many bytes a writer emits, the extent would stop being the only statement of
a record's length, and a consumer ignoring the new field would write short
records rather than ignore something it did not need.

The two directions do not cost the same afterwards, and that is what decides the
timing. Imposing the rule after the first release makes a descriptor that
conformed stop conforming, and a layout an adopter had already written stop
resolving. Widening it later asks nothing of an adopter and something of a
generator, and the something is small: the arithmetic a variable extent needs is
the arithmetic [A variable record is a sum with a variable
term](#a-variable-record-is-a-sum-with-a-variable-term) already requires under
the other three framings, so what a wider rule would take away is a generator's
licence to treat **unframed** as the framing whose extent can be computed once.
That may cost a version of its own, and it costs it where the bill is a
generator's to pay rather than every adopter's. If a spelling ever resolves to
**unframed** with no LRECL behind it — records packed back to back with nothing
between them — that is the change it would arrive in, to the mapping [Four
framings, and none of them is a
RECFM](#four-framings-and-none-of-them-is-a-recfm) carries (#92).

What an adopter does instead depends on where the table sits, and so does the
diagnostic: `resolve` **MUST** tell the two shapes below apart in what it
reports, rather than calling both of them a variable record (#26, #35).

**A table with nothing behind it** is rescued by the fork [An item after a table
slides, and the other reading is a fixed
table](#an-item-after-a-table-slides-and-the-other-reading-is-a-fixed-table)
records. Where no item follows the table — not in its own group, and not in any
group that contains it — the two readings put every item the count reaches at
the same offset, and differ only over the bytes past it, which are a pad under
one reading and unfilled occurrences under the other and the same bytes under
both. Stating the non-sliding reading resolves the table to a fixed one of the
copybook's declared maximum with the count field an ordinary field beside it,
and the record then has a constant extent that accounts for LRECL like any
other, provided LRECL reaches the table's full allocation; where it does not,
the ordinary LRECL check fires and says so. The diagnostic **MUST** name that
reading as the description that fits the same bytes. What the rescue costs is
stated where it is described — the copybook's bound stops being checkable, and
the entries past the count are values rather than retained bytes — and the
reading is one statement about a file rather than one per record (#27), so it
reaches a layout whose tables are all this shape and not one record of a layout
where they are not.

**A table with items behind it** has no rescue, because the two readings really
do put those items in different places and the file was written under one of
them. The diagnostic **MUST** name the item behind the table that moves with the
count, since that item is the whole of what makes the record undescribable.
Stating the other reading is not a way around the rejection, and nothing here
could make it one: a layout claiming a file does not slide when the compiler
that wrote it did is wrong at every item behind the table, on a fixed-length
dataset and everywhere else, which is the failure the fork was recorded to end.
What is left to the adopter is a record type per count value — a copybook apiece
with a constant repetition and a constant pad, told apart by a predicate on the
count field, which [Discriminator predicates](#discriminator-predicates) admits
wherever nothing variable stands in front of it. That is unrolling, and it is
bounded where the unrolling [The automaton remembers, in
registers](#the-automaton-remembers-in-registers) refuses by name is not: a
repetition declares a minimum and a maximum, so what it costs is a record type
per count the copybook allows rather than per value a `PIC` clause can hold. It
is still a copybook per count, and a five-hundred-entry table makes it absurd.
`resolve` does not do that unrolling on the adopter's behalf, and the reason is
not that it could not: a record type is what a generator turns into a type its
caller names, and inventing one per count value out of a copybook that declares
one record hands every generator in every language a set of record types the
adopter never wrote.

Three things this leaves alone. A data-dependent extent is ordinary under the
other three framings, where a record occupies its own extent and the framing
disagrees when it does not — a table whose length depends on a count is what a
variable-length dataset is for, and that is where most of them are. A record of
a non-sliding file has a constant extent and sits on a fixed-length dataset like
a record with no table in it ([An item after a table slides, and the other
reading is a fixed
table](#an-item-after-a-table-slides-and-the-other-reading-is-a-fixed-table)).
And two record *types* whose extents differ from one another are still not
forbidden by the framing, which is what [Four framings, and none of them is a
RECFM](#four-framings-and-none-of-them-is-a-recfm) says of it: what a record
type may not do is differ from itself.

Nothing is added to the schema for any of it. The rule says which descriptors
may exist rather than what a node carries, so #17 fixes the schema against the
same node shapes it would have fixed it against anyway (#17, #92).

### The extent governs, and framing is checked against it

Two things in a framed file say where a record ends: the record's extent, and
the framing around it. That is one fact stated twice, and everywhere else this
document has answered a duplicate by carrying it once. Here it cannot — the
framing bytes are in the file whether the IR mentions them or not — so what is
settled instead is which of the two a consumer obeys.

The extent governs. A consumer **MUST** take a record's end from its extent, and
**MUST NOT** determine it by searching the input for a delimiter, or by
preferring a descriptor word's length to it (#78). A consumer **MUST** then
check the framing against the extent and report a disagreement as malformed
data: a descriptor word whose length is not the extent of the record admitted,
or a delimiter that is not where the extent ends.

Scanning has to be refused by name, because it is the obvious way to write a
line-oriented reader and it is wrong on precisely the files this project exists
for. A `PIC S9(3)V99 COMP-3` field holding `+152.50` is the three bytes
`15 25 0C`. Both `0x15` and `0x25` are line delimiters on some mainframe code
page, so a reader scanning for one finds it inside the number and cuts the
record there — emitting a record that is wrong rather than absent, and failing
three records later, somewhere the corruption did not happen. A reader counting
the extent never reads those bytes as anything but the number they are.

Nothing is given up by refusing, because a record's extent is always derivable
by the time a consumer needs it. It is not derivable *before* the record is
identified — a state offering three transitions offers three extents — which is
why the order in [Where framing is consumed, and where it is
emitted](#where-framing-is-consumed-and-where-it-is-emitted) is normative:
predicates run first, against bytes at offsets within the record they would
admit and inside the record in front of the consumer whichever that turns out
to be ([A predicate never reads past the record in front of
it](#a-predicate-never-reads-past-the-record-in-front-of-it)), and the extent
follows from the transition taken. Even a variable record
keeps the extent inside the record's own bytes rather than the framing's ([A
variable record is a sum with a variable
term](#a-variable-record-is-a-sum-with-a-variable-term)). So the question of
whether a consumer **MAY** scan where an extent is derivable has nothing on the
other side of the "where", and the prohibition above is unconditional.

What the framing buys instead is detection, and the four differ sharply in how
much they give. A descriptor word or a delimiter that is not where the extent
says makes a wrong width, a mis-selected transition or a corrupt file an error
at the record it happened on. **unframed** gives none of that: nothing in the
file disagrees with anything, and a record read at the wrong offset misaligns
every record after it in silence. That is a property of the dataset rather than
a choice made here, and it is why checking a fixed-length dataset's own LRECL
against the widths a copybook implies is worth doing while a layout is still
being read (#26).

### A delimiter is bytes, not a character

A **delimited** file node carries the delimiter as literal bytes, and its width
is how many of them there are. It carries no named character, no code point and
no line-ending style. A producer **MUST NOT** emit a delimiter of no bytes, and
a consumer **MUST** compare it to the input as bytes and **MUST NOT** interpret
either side through a charset (#78).

Those bytes are outside the charset axis for the reason slack is outside it: the
axis is per field ([The encoding profile,
applied](#the-encoding-profile-applied)), a delimiter is not a field, and the IR
has no file-level charset for one to inherit from — deliberately, since a record
whose fields disagree about charset is the ordinary case here. [What the
descriptor determines, a writer
supplies](#what-the-descriptor-determines-a-writer-supplies) hits the same wall
from the other side when it fills a constructed record's slack with zero rather
than with a space. The difference is that slack could take the byte that names
nothing, and a delimiter cannot: it is the byte the file actually holds.

Naming a character would not have picked one byte even with a charset to resolve
it against. A mainframe line-delimited file ends its records with `0x15` or with
`0x25`, and cp037 and cp1047 — both pages `codec/SPEC.md`'s charset axis
enumerates — disagree about which of those is LF and which is NL. The same file
transferred to Linux ends them with `0x0A`, and one that has been through
Windows with `0x0D 0x0A`. A Go author reaching for the obvious default reaches
for `0x0A`; a mainframe shop reaching for the obvious default reaches for
`0x15`; neither is wrong about their own files, and a spec naming a character
would have made one of them wrong about the other's. Bytes are the resolved form
of that argument, and they are two bytes wide when the file needs two.

A delimiter is not required to be absent from a record's data, and on these
files it will not be: `0x15` appears inside any packed field holding a value
like the one above. That costs nothing, because no consumer looks for it.

### Terminator, separator, and the last record

A **delimited** file node carries the delimiter's **placement** beside its
bytes, one member of a closed set of three:

- **terminator** — a delimiter follows every record, the last included. A file
  of *n* records carries *n* of them.
- **separator** — a delimiter stands between two records. A file of *n* records
  carries *n*−1, and nothing follows the last record.
- **optional terminator** — a delimiter follows every record, except that the
  file **MAY** end after the last record without one.

The distinction is what makes the end of a file checkable. Under **separator** a
trailing delimiter announces a record that is not there, and a consumer **MUST**
report it rather than reading the file as complete. Under **terminator** a final
record with nothing behind it is a file that was cut short, and a consumer
**MUST** report it as truncated. Neither error is available to a consumer that
decided the question for itself, which is why it is carried.

**optional terminator** is a member because real files need it, not because the
question was hard. A shop's extract carries the final delimiter on Tuesday and
not on Wednesday, out of the same program over the same data; forced to choose,
an adopter picks whichever they saw first and the reader fails on the other day.
What the member gives up is stated with it: under it a final record whose bytes
were cut off is indistinguishable from a final record whose delimiter was never
written, so the truncation the other two catch at the last record is the one
this member cannot see. An adopter who can promise consistency is paid for
promising it, in a diagnostic.

It is not the consumer's judgement given back. Under all three members the file
node states what the file does and a consumer follows it, so two consumers
handed one descriptor read identically — and a writer under **optional
terminator** is not lenient at all, for the reason below.

### Where framing is consumed, and where it is emitted

The read loop in [The sequencing automaton](#the-sequencing-automaton) had no
step that touched framing, and without one a well-formed file fails. A trailing
delimiter left unconsumed is input, so a consumer is not at end of input, so it
evaluates the current state's transitions against a delimiter byte, so no
predicate matches, so it **MUST** report a record the layout does not describe —
for a file that is exactly right. Where the state's transition carries no
predicate ([A transition may carry no
predicate](#a-transition-may-carry-no-predicate)) the same file fares worse
still, since the delimiter is admitted as a record rather than reported. The
step is therefore normative, and so is where it sits.

Reading one record, in this order:

1. A consumer **MUST** test for end of input here, at a record boundary, before
   evaluating any transition. Reaching it is the case [The sequencing
   automaton](#the-sequencing-automaton) already governs: the file is complete
   where the state accepts and its acceptance guards hold, and truncated
   otherwise.
2. Consume the framing in front of the record — the descriptor word under
   **descriptor-word**, every segment descriptor of this record under
   **segmented**, the delimiter in front of every record other than the first
   under the **separator** placement. Under **segmented** a consumer **MUST**
   reassemble the segments' data into one run of bytes before going on, so that
   every rule in this document reads a record that is contiguous whether or not
   the file split it.
3. Evaluate the state's transitions as [The sequencing
   automaton](#the-sequencing-automaton) says — guards, then predicates, in the
   order the state carries them — against the record's data beginning here. A
   consumer **MUST NOT** evaluate a predicate against bytes outside the record
   in front of it, and what bounds one is [A predicate never reads past the
   record in front of
   it](#a-predicate-never-reads-past-the-record-in-front-of-it). Where the
   framing has already stated this record's length, a predicate whose target is
   not wholly within the record the framing bounds does not match — a target
   past it, and a target beginning inside it and ending beyond — and if no
   other matches either, this is a record the layout does not describe. Where
   the framing has stated nothing, that subsection puts every target the state
   can evaluate inside the shortest record the state can admit, so the bytes
   are there whenever the input holds a whole record.
4. Take the transition and admit the record. Its extent is known now, because
   which record it is is known now. Where that extent depends on a count, the
   count is a field lying ahead of the item it sizes or a register an earlier
   transition bound, and never anything this transition's own bindings write —
   those apply at step 7, below ([A count is in hand before the extent it
   decides](#a-count-is-in-hand-before-the-extent-it-decides)).
5. Check the framing against that extent, as [The extent governs, and framing is
   checked against
   it](#the-extent-governs-and-framing-is-checked-against-it) requires.
6. Consume the framing behind the record — the delimiter, under the
   **terminator** and **optional terminator** placements. Under **optional
   terminator** the input **MAY** be at its end instead, and a consumer **MUST**
   take that as the file ending rather than as a record cut short.
7. Apply the transition's bindings and move to the state it names.

Reaching end of input anywhere from step 2 to step 6 — part-way through a
record's extent, with a required delimiter unread, or with a predicate's target
past the last byte the input holds — is a truncated file, and a consumer
**MUST** report it as one rather than as a record the layout does not describe.
Step 3 is inside that range and is not an exception to it; what keeps it off a
well-formed file is the bound [A predicate never reads past the record in front
of it](#a-predicate-never-reads-past-the-record-in-front-of-it) places on every
target a state can evaluate. End of input is tested at step 1 and
nowhere else, which is what keeps a well-formed trailing delimiter away from
both the truncation rule and the no-match rule: by the time the test runs, the
delimiter has been consumed as the framing it is.

A writer emits the same bytes at the same points of the same walk ([A writer
walks the same automaton](#a-writer-walks-the-same-automaton)): the descriptor
word in front of each record, the delimiter behind each record under both
terminator placements, and the delimiter in front of each record other than the
first under **separator**. Emitting a separator in front rather than behind is
not the same rule restated — a writer does not learn which record is the last
until its caller stops, and one that waited to find out would be holding a
record it had already been given, which gives up the streaming property #52
requires.

Two writer rules follow from there being one file per descriptor. Under
**optional terminator** a writer **MUST** emit the final delimiter rather than
choosing whether to, because two writers left to decide produce two different
files from one descriptor and one set of records — the divergence [A writer
evaluates a predicate, it never inverts
one](#a-writer-evaluates-a-predicate-it-never-inverts-one) refuses, in the same
words. And under **segmented** a writer **MUST** emit each record in as few
segments as the largest segment allows, and **MUST NOT** emit a segment longer
than that. The largest segment is the one framing fact a writer needs and cannot
compute, which is why the file node carries it; a reader has no use for it,
since every segment states its own length.

### Lengths the file node does not carry

Three quantities a reader of the table above might go looking for are absent,
each for a reason this document has already made somewhere else.

**No record length, and no maximum record length.** Under **unframed** a
record's length is its extent, which the record nodes state; under the other
three the framing states it per record. A length on the file node would be that
fact a second time in the sense [Ordering and width, and no
offset](#ordering-and-width-and-no-offset) uses, with the additional property
that a consumer could do nothing with a disagreement — the extent governs, so a
conflicting length is a value a consumer has to ignore, and a value a consumer
has to ignore is not worth carrying. A length carried so that a writer could pad
a variable record out to it is a different proposal, and it is refused where
that record is ([A variable record does not fit a fixed-length
dataset](#a-variable-record-does-not-fit-a-fixed-length-dataset), #92). Checking
a dataset's declared LRECL against the widths a copybook implies is real work
all the same, and it is `resolve`'s, done while a layout is being read and
before an IR exists (#26).

**No block size.** The stream the IR describes carries no block descriptor words
([Block descriptor words in the
stream](#block-descriptor-words-in-the-stream)), so there is no block for a size
to describe.

**No descriptor-word width.** **descriptor-word** and **segmented** name the
descriptor word DFSMS defines, cited in [Governing
sources](#governing-sources) and not restated; its width comes with the
definition. A width carried beside it could hold a number describing no format
anyone has, and a producer emitting one would be describing a framing this
document does not specify.

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

### An item with no charset carries bytes, not characters

The charset axis has a value meaning that the item has none. A field carrying
it holds a payload rather than characters: a consumer **MUST** read and write
its bytes as they stand, **MUST** apply no translation to them, and **MUST**
strip and add no padding on either side (#275).

The axis is still resolved and still carried — this is a value on it, not a hole
in it — so the rule above that a producer sets all four and a consumer defaults
none is unchanged.

A field carrying it **MUST** be an item whose `USAGE` is `DISPLAY` and whose
category is alphanumeric, which is a `PIC X` item. A consumer **MUST** treat a
field carrying it on a DISPLAY item of any other category as a malformed
descriptor: an item with digits to read and no charset to read them through has
no reading at all, and one declared alphabetic or edited is characters by
declaration. On the usages charset does not govern — packed decimal, `COMP-6`,
binary, `COMP-5`, `COMP-1`, `COMP-2`, `INDEX`, `POINTER`, `NATIONAL` — it is
inert, exactly as a code page already is on them, because a layout may set the
axis on a group and a group holds items of every usage.

Which item this is cannot be derived from the copybook, which is why it is in
the IR at all. `PIC X(1)` is what a vendor's manual writes for a status flag
whose values are `0x01` to `0x03` and for a region identifier holding a
hexadecimal value, and it is also what it writes for two characters of text.
The copybook has no room to tell them apart; the layout is where an adopter says
so ([`layout/SPEC.md`](../layout/SPEC.md#a-byte-is-not-a-character-and-such-an-item-has-no-charset)),
and a field node is where that statement arrives resolved.

The value the reader produces is the bytes and their count is the field's width.
There is nothing else it could be: a byte item has no pad byte that is not also
a legal payload value, so a writer **MUST NOT** pad a value short of the width
or truncate one past it, and **MUST** refuse such a value instead. That is the
same rule [Slack survives a read](#slack-survives-a-read) already puts on a run
of bytes no item covers, reached from the other direction — those bytes are
retained rather than reconstructed for exactly this reason.

**Adding it did not advance the version.** [What breaks
it](#what-breaks-it) turns on whether a conforming consumer can ignore a change
and remain correct, and this is not a change such a consumer can ignore: one
that read the item as text would trim it, translate it, and be wrong about 236
of the 256 values a byte may hold. What makes it additive all the same is that
no consumer *can* ignore it. The charset axis is an enumeration a consumer
**MUST** refuse an unrecognised value of rather than falling back to one it
knows, so a generator built before this value existed meets it as a number it
does not have and refuses the descriptor on sight. The distinction is the one
that section already draws between a field, which an old consumer sees nothing
of, and a value, which arrives as itself and can be refused; a marker carried as
a new field on the field node would have been the first, and would have had to
advance the version.

## Names

Every named node carries the original COBOL name, spelled as the copybook spells
it, and **MAY** carry an override alongside it (#30). Both are language-neutral,
and a producer **MUST NOT** apply the casing or identifier conventions of any
language to either (#50).

The original **MUST** be present even where an override is. A rename substitutes
a name, and the substitute is carried beside the original rather than in place
of it, so that generated code can still point back at the copybook it came from.

**A record node resolved from a `REDEFINES` carries the `01`-level's name**, and
not the alternative's. [Members never overlap, and `REDEFINES` is resolved
away](#members-never-overlap-and-redefines-is-resolved-away) turns one
`01`-level into one record node per alternative, and every one of them describes
that `01`-level: `TXN-REC` is what the copybook calls the record each of them
is, and the alternative is which description of its bytes was taken. So several
record nodes of one descriptor **MAY** carry one original, and a producer
**MUST NOT** substitute an alternative's name for the `01`-level's to tell them
apart (#164).

Taking the alternative's name would read well for a copybook with one redefine
in it and answer nothing for a copybook with two, where the record nodes are one
per *combination* and no single alternative names any of them. A rule that
changed what a record node was called when a second `REDEFINES` was added to a
copybook would change it in a file no layout is stored beside.

What tells them apart is the override, which the layout supplies — [`layout/SPEC.md`](../layout/SPEC.md#a-rename-may-name-a-record)
spells it — and nothing here requires one. Two record nodes carrying one name
are a descriptor this document admits; whether that is a problem is the
consuming generator's, and `cpybkc-gen-go` refuses the pair it cannot munge into
two identifiers (#50).

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
transitions, in order; each transition names the record it admits, the state to
move to, the predicate that selects it where it carries one, and — where the
automaton has memory — the guards that make it eligible and the bindings it
applies. A consumer reads a file by consuming the framing in front of a record,
evaluating the current state's transitions in the order given, skipping any
whose guards do not all hold, taking the first of the rest that carries no
predicate or whose predicate matches, emitting the record that transition names,
consuming the framing behind it, applying that transition's bindings, and moving
to the state it names. [Where framing is
consumed, and where it is
emitted](#where-framing-is-consumed-and-where-it-is-emitted) states those two
steps exactly, and states that end of input is tested at a record boundary and
nowhere else. Writing one walks the same graph in the same order; [Writing a
file](#writing-a-file) states the difference.

Every transition consumes exactly one record. There is no transition that moves
without reading, and no way to test something without consuming — which is what
keeps a generated reader a loop over one record at a time, and is argued for
under [No epsilon
transitions](#no-epsilon-transitions-and-what-the-graph-pays-instead).

A transition **MUST NOT** be labelled with a record name (#36). A record is what
a transition *produces*, not what chooses it: a label naming the record would be
a test no consumer can run, since which record it is looking at is precisely the
thing it does not yet know.

What does select one is a predicate, a list of guards, or neither. A transition
**MAY** carry no predicate, and one that does not matches every record — it is
then selected by its guards alone, or, where it carries none of those either, by
being the only thing its state offers. That is not the record-name label back
again under another name. A label would assert which record the bytes in front
of the consumer are; a transition carrying no predicate asserts that its state
offers nowhere else for them to go, which is a fact about the automaton that
[A transition may carry no
predicate](#a-transition-may-carry-no-predicate) makes true rather than assumed
(#80). What such a transition does not do is move without reading, which is the
paragraph above.

A state carries whether it accepts, and an accepting state **MAY** qualify that
with guards: it accepts end of input only when all of them hold. A state that
does not accept **MUST NOT** carry one. A consumer reaching end of input in a
state that does not accept, or whose acceptance guards do not all hold, **MUST**
report the file as truncated rather than returning the records it managed to
read: an automaton whose accepting states nobody checks detects nothing.

Guarded acceptance is what makes the last iteration of a count detectable. A
state that reads details while a counter is positive would otherwise have to
accept unconditionally, and would then accept a file stopping three details
short of what its own header promised.

The automaton is a graph, and a cycle in it is ordinary — a header followed by
any number of details is a cycle. Ambiguity is not: `resolve` rejects an
ambiguous grammar when it compiles one (#36), so the automaton reaching a plugin
is unambiguous and a consumer is entitled to assume it. Which pairs of
transitions that test is over, once guards are present, is [When two
match](#when-two-match-and-when-none-does)'s.

### The automaton remembers, in registers

A file shape this project exists to read: a header carries a flag saying whether
a later record type appears at all, and a count saying how many of another type
follow. Both govern records other than the header, both are ordinary, and
neither is expressible by a graph with no memory. Value-dependent sequencing is
therefore **in scope**, and this is the mechanism (#76). How an adopter *spells*
a count that lives in a header is the layout format's (#22, #29); what one means
is here.

The automaton **MAY** carry values forward in **registers**. A register node
declares the kind of value it holds and nothing more. A **binding** on a
transition writes one. A **guard** on a transition reads one and decides whether
that transition is eligible at all. Nothing else reads or writes a register, and
no register is derived from anything: every value in one was put there by a
binding naming the field it came from.

**What a register holds.** A register's kind is one member of a closed set:
bytes, or an integer. A bytes register holds its source field's bytes as they
appear in the record, so a guard over one is a byte comparison and needs no
charset knowledge — the same reason a predicate tests bytes. An integer register
holds a number, decoded from the source field by that field's own four encoding
axes, because a count is arithmetic and the field holding one may be zoned,
packed or binary. A producer **MUST NOT** bind a field whose value does not
decode to the register's kind, and a consumer **MUST** report a source field it
cannot decode as that kind as malformed data rather than substituting a zero or
spaces.

**What a binding writes.** A binding names the register it writes and the value
written, one member of a closed set: the value of a field node contained in the
record the transition admits, or the register's own value less one. The second
member exists to count — a transition that admits one detail and takes one off
the counter is how a run of *n* records is read without *n* states. A producer
**MUST NOT** name a field of any other record in a binding, since the record the
transition admits is the one the consumer has bytes for, and **MUST NOT** put
two bindings writing one register on a single transition.

Two further restrictions on a binding, each closing a hole that would otherwise
be a silent wrong answer rather than an error. A binding **MUST NOT** name a
field that repeats, or one contained at any depth in a group that repeats — the
rule [A reference names a field, not an occurrence of
one](#a-reference-names-a-field-not-an-occurrence-of-one) states for every
position naming a field, and argues there (#76, #84). And a producer **MUST**
guard a transition taking one off a register so that the register cannot run
below zero; a consumer reaching one that would **MUST** report it rather than
wrapping or clamping, since a counter that has gone negative is a `resolve` bug
and every value read after it is fiction.

Bindings apply when the transition is taken, after the record is admitted, and
each reads the register file as it stood on entry to the state. So the order of
one transition's bindings is not significant, and a transition may take one off
a counter and rebind another register in the same step without the two
interfering. They are also the last thing a transition does — step 7 of the read
loop — so nothing that transition reads sees what it writes: not its guards,
evaluated at step 3, and not the extent of the record it admits, computed at
step 4 ([A count is in hand before the extent it
decides](#a-count-is-in-hand-before-the-extent-it-decides)).

**What a guard tests.** A guard names the register it tests and the test itself,
one member of a closed set of three: the register equals a carried literal, the
register is one of a carried set of literals, or the register holds an integer
greater than zero. Guards are evaluated before the record in front of the
consumer is examined at all, and against the register file as it stands on entry
to the state, so a guard never reads what its own transition binds. All of a
transition's guards **MUST** hold for it to be eligible, and their order is
therefore not significant. A transition carrying none is always eligible, which
is every transition in a file whose sequencing needs no memory.

A guard over a bytes register carries its literal already padded to the width of
the value it will be compared against, and a consumer **MUST** compare the whole
of that value rather than a prefix of it. Padding a literal out to a field's
width is a COBOL comparison rule, and applying it is the producer's work like
every other (#37) — a consumer left to decide whether `Y` matches `Y ` is a
consumer that decides differently in each language.

Three tests, and no fourth. Conjunction is the list, disjunction is a second
transition leaving the same state, and a state already *is* a disjunction — so
the set needs neither. Its membership was settled here rather than with a
layout-side strategy list, the way the [predicate
set](#discriminator-predicates)'s was (#22, #28), because a guard reads a value
this document's own machinery put in a register: there was no strategy list for
it to follow and nothing to wait for.

**A register is read only where it has been written.** A register is read in
three places, and every one of them reads the register file as it stood on entry
to the state: a guard on a transition, the acceptance guards of a state, and the
count of a repetition in the record a transition admits. A producer **MUST**
ensure that every path from the start state to a state whose acceptance guards
read a register, and every path to a transition that carries a guard reading one
or admits a record whose repetition names one, passes through a binding of that
register on a transition taken **strictly earlier** than the reading one (#36,
#37, #88). A transition that binds a register it also reads does not satisfy
that for itself, whichever of the two reads it makes: its bindings apply at step
7 of the read loop and both reads happen before then, so what it would read is
the value the binding was about to replace, or nothing at all where no earlier
transition bound one ([A count is in hand before the extent it
decides](#a-count-is-in-hand-before-the-extent-it-decides)). A consumer **MUST**
treat a read of a register nothing has bound as a malformed descriptor, and
**MUST NOT** supply a zero, an empty byte string, or the value of any other
register: an IR that reached a generator with a register read before it was
written is a bug in `resolve`, and the rule here is the one the [encoding
profile](#the-encoding-profile-applied) already applies to an unset axis.

**A register has no scope and no history.** There is one register file for the
whole read, a register holds what the most recent binding put in it, and nothing
saves or restores one. That is what answers the question a cross-record count
raises — which of a thousand headers governs (#77): the nearest preceding record
that bound the register, along the path actually taken.

The cost is that a generated reader is no longer a table walk with nothing
beside it. It carries a register file, and its loop grows two steps: check
guards before matching bytes, apply bindings after admitting a record. That is
still no COBOL and still nothing derived, which is the promise the automaton is
carried compiled for (#36), but it is more than a `switch` inside a `for`, and a
plugin author should expect to hold state. What it buys is a graph whose size
follows the layout instead of the data: a `PIC 9(4)` count is one register and
one guard, where unrolling it into states is ten thousand of them and everything
that hangs off each.

What a *writer* does with a bound register — a header count has to match the
records that follow it — is [A writer evaluates a guard, it never back-fills a
count](#a-writer-evaluates-a-guard-it-never-back-fills-a-count)'s, and the
answer there is the one this section predicts: it evaluates, and it fills in
nothing.

### When a value becomes a state, and when it becomes a register

An adopter can tell which shape a file needs by asking one question: does the
value govern the record it sits in, or a later one?

A flag deciding what the record *holding* it means becomes a branch, and needs
no register. Two transitions admit the same header and move to different states,
each selected by a predicate testing that flag; the dependence on the value has
become the state the automaton is in, and everything downstream of it is an
ordinary graph. This is the form that worked before this section existed, and it
is still the form to prefer: a producer **SHOULD** compile a value tested only
on the transition admitting the record that holds it as a branch rather than as
a register, because a register the graph does not need is memory every consumer
in every language carries for nothing.

A value governing a record *other* than the one it sits in becomes a register. A
count is always this shape — the header says four, and the fourth detail is four
records away. A flag gating a record type that is otherwise indistinguishable
from what would follow without it is this shape. And so is a second flag on one
header, which a branch cannot reach at all: a transition is selected by one
predicate testing one field, and there is nowhere to conjoin a second test onto
it.

Nothing falls between the two. A value in the record being discriminated that
governs only that record is a branch; anything a later record's presence, count
or extent depends on is a register; and a value in a record not yet read governs
nothing, for the reason [A value the automaton has not read
yet](#a-value-the-automaton-has-not-read-yet) gives.

### No epsilon transitions, and what the graph pays instead

A guarded transition consuming no record — an epsilon — was the obvious
alternative, and it is deliberately absent. With one, a counted group would
leave its loop by an epsilon guarded on the counter reaching zero, a conjunction
would be a chain of them, and conditional acceptance would be an epsilon into an
accepting state. The graph would be smaller and every transition would carry at
most one guard.

The cost falls on the consumer, which is the wrong place for it. An epsilon
turns "evaluate the transitions, take one, consume a record" into "follow
epsilons until none applies, then evaluate the transitions" — at end of input as
well, where a consumer would have to walk epsilons before it could decide
whether the file was truncated. It puts a second control-flow rule into every
generated reader in every language, and it obliges `resolve` to prove the
epsilon-only subgraph acyclic before a consumer may assume the walk terminates.
Sequencing arrives compiled precisely so that a plugin author implements as
little as possible.

So the graph pays instead. A segment that may be skipped forces the state ahead
of it to offer a transition for each record that can follow the skip, each
carrying the guards that say the skipped segments are done: more transitions and
more guards, all of them emitted by `resolve`, and none of them growing with the
width of a count. What a state carries follows the number of segments that can
be skipped past it, never the values in the file. That is the trade this
document keeps making — cost in the producer, which is one program, over cost in
the consumers, which are as many programs as there are languages.

## Discriminator predicates

Discrimination reaches a generator compiled too. A predicate is one member of a
closed set, it names the field node it tests, and what it tests is bytes — a
consumer evaluates one knowing no COBOL, and knowing nothing about what the
strategy that produced it was called in a layout file (#37).

Two things select on bytes, and they share this node kind and this closed set of
tests: a transition, choosing a record, and an arm of a variant, choosing an
alternative inside one occurrence of a table ([A variant is chosen once per
occurrence](#a-variant-is-chosen-once-per-occurrence)). Everything down to [A
predicate on an arm reads one
occurrence](#a-predicate-on-an-arm-reads-one-occurrence) is about the first, and
that subsection says which of it the second keeps (#90).

A predicate **MUST** name its target as a field node identifier and **MUST NOT**
name it as a field name. The record being discriminated is the record its
transition admits: a producer **MUST** ensure the target is contained in that
record, at any depth, and **MUST NOT** name a field of any other (#37). Where
those bytes are then follows from [Offsets and widths](#offsets-and-widths) —
the target's position is the sum of the widths ahead of it in that record, and
its extent is the target's own width.

The target **MUST NOT** repeat, and **MUST NOT** be contained at any depth in a
group that repeats, under the rule [A reference names a field, not an occurrence
of one](#a-reference-names-a-field-not-an-occurrence-of-one) states for every
position naming a field (#37, #84).

A predicate's target carries a second restriction: its position **MUST** be
constant within the record its transition would admit. No item ahead of it
**MAY** carry a repetition whose count is a reference, and `resolve` **MUST**
reject a layout whose discriminator sits behind one, naming the record, the
target and the variable item in front of it (#37, #84).

An `OCCURS DEPENDING ON` count carries the same restriction and reaches it by
another road, since a count is read after its record has been admitted and the
read loop does not force one on it ([A count is in hand before the extent it
decides](#a-count-is-in-hand-before-the-extent-it-decides), #88). The field a
binding reads carries neither. What follows is the predicate's own reason, and
it is the read loop's order.

This is what makes the read loop's order safe rather than merely stated. A
predicate is evaluated before its record is admitted, against bytes read as the
record its transition *would* admit ([Where framing is consumed, and where it is
emitted](#where-framing-is-consumed-and-where-it-is-emitted)) — so a target
whose position depends on a count obliges a consumer to decode that count out of
bytes it has not identified. Two failures follow, and neither is detectable. The
bytes may be another record type's, decoding to a number that sends the read to
an offset the layout never described — and nothing bounds that, because a
position that is not a number is a position the bound [A predicate never reads
past the record in front of
it](#a-predicate-never-reads-past-the-record-in-front-of-it) places cannot be
checked against: `resolve` computes that bound from the layout and would have
nothing here to compute it against, and under **unframed** and **delimited**
there is nothing at read time to stop the read either. Or the bytes may not
decode, and [A variable
record is a sum with a variable
term](#a-variable-record-is-a-sum-with-a-variable-term) requires a consumer to
report that as malformed data — so one consumer condemns a well-formed file
while another treats the predicate as not matching and reads it correctly. A
constant offset removes both, and costs a discriminator nothing it was using:
the position of a type code is a property of the record's shape, not of its
data.

Neither restriction costs the strategies anything. The two that name a field
name a type code at a fixed offset, or one in a header copybook every
alternative includes, and neither shape repeats or sits behind a variable item.
The ones that name no field do not lower into a predicate at all, which is [A
predicate always names a field](#a-predicate-always-names-a-field)'s (#80).

A predicate and a guard divide the two things a transition can be selected on,
and neither reaches into the other's half. A predicate **MUST NOT** name a
register, and a guard **MUST NOT** name a field node (#37). A predicate reads
the bytes in front of the consumer; a guard reads what the automaton
[remembers](#the-automaton-remembers-in-registers). Keeping them apart makes
"guards first, then bytes" a shape rather than a rule, and leaves the overlap
test below over predicates that all read the same record.

A guard is not the way a predicate stops naming a field, either, and nothing
else is. The set admits no member testing a record's length or where it sits in
the stream, and selecting a transition on nothing at all is the absence of a
predicate rather than a member of the set. The two subsections below settle both
(#80).

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
that only one language could run. Its membership is settled here, with those
strategies (#22, #28), and it is **two** tests:

| Test | Satisfied when |
|---|---|
| **bytes equal** | the target's bytes are the carried literal |
| **bytes one of** | the target's bytes are one of the carried literals |

Both carry their literals as bytes, already padded by the producer to the
target's width, so that a consumer compares the whole of the target rather than
a prefix of it and never applies a COBOL comparison rule of its own. A test
carrying no literal at all does not exist, and a test carrying the same literal
twice is a producer emitting one member's work as another's; neither is a member
here.

**bytes one of** is a member rather than a shorthand a producer expands. A
transition carries at most one predicate and an arm carries exactly one, so a
strategy admitting three type codes has nowhere to put three predicates — and
splitting it into three transitions to the same state would make a set of values
into a set of edges, which the overlap rule would then have to be taught to
forgive. One member with a list is decidable by exactly the same test as one
member with a literal: two of them overlap when their literal sets intersect,
which is a question about bytes and stays a question about bytes.

Three tests were proposed and refused, and each is refused where its reason is:
a record's length and where a record sits in the stream by [A predicate always
names a field](#a-predicate-always-names-a-field), and selecting on nothing at
all by [A transition may carry no
predicate](#a-transition-may-carry-no-predicate), which makes it the absence of
a predicate rather than a member. `layout/SPEC.md`'s `single-record-type` is
that absence and is not spelled here.

Both members meet the two bounds a member has to meet. Each names a field, which
the subsection below requires. And each is decidable by a writer against the
record it is about to emit ([A writer evaluates a predicate, it never inverts
one](#a-writer-evaluates-a-predicate-it-never-inverts-one), #79): the target is
a field of the record in the writer's hands, and comparing its bytes to a
literal — or to a list of them — asks nothing of what the writer will be handed
next.

A third member is a breaking change under [Versioning and
compatibility](#versioning-and-compatibility), which is why the set is settled
before the first release rather than grown afterwards.

### A predicate always names a field

A predicate names a field node and tests its bytes. Both members of the closed
set do (#22, #28), and there is no other thing about a record a transition can
be selected on (#80).

Three of the strategies proposed for the layout format test something else, and
each is refused on its own terms rather than by an appeal to tidiness.

**A record's length is not a predicate's to test**, and the two framings that
could supply one and the two that could not leave nothing between them. Under
**descriptor-word** and **segmented** a length is in hand when predicates run,
because the framing is consumed at step 2 of the read loop and predicates are
evaluated at step 3 ([Where framing is consumed, and where it is
emitted](#where-framing-is-consumed-and-where-it-is-emitted)) — but that length
is a framing byte's value, and [Physical
framing](#physical-framing) says no predicate ever sees one. Under **unframed**
and **delimited** there is no length to see: [The extent governs, and framing is
checked against it](#the-extent-governs-and-framing-is-checked-against-it) makes
a record's end its extent, and the extent follows from the transition taken, so
a predicate testing the length would have to know which record it is in order to
know the length that decides which record it is.

The framed half has a second count against it, and it is worth stating exactly
rather than grandly, because the obvious version of it is not true. What a
descriptor word buys is detection at the record it happened on: it disagrees
with the extent of the record admitted, and a wrong width or a mis-selected
transition is reported there. A predicate selecting on that length does not
abolish that check, and the claim that it does is wrong twice over — a length
naming no transition still matches none, so a corrupt descriptor word is
reported at step 3 instead of step 5, and a record whose extent varies with an
`OCCURS DEPENDING ON` count can still disagree with the length that selected it.
What the check loses is the one case where a descriptor word corrupts from one
described length into another: there the transition selected admits a record
whose extent *is* the length that selected it, step 5 is a tautology, and the
read runs on at the wrong offset to be reported some records later, somewhere
the corruption did not happen — which is the failure [The extent governs, and
framing is checked against
it](#the-extent-governs-and-framing-is-checked-against-it) names when it refuses
scanning. That section does not refuse a length predicate in as many words. It
requires the extent to govern the read, and under a length predicate it still
would; what changes is that the descriptor word acquires a second job upstream
of the extent, deciding which record it is, and the check that exists to catch a
descriptor word that is wrong stops being able to catch the wrong that matters
most.

What refusing costs an adopter is a file shape this document cannot describe,
and [A record told apart only by its
length](#a-record-told-apart-only-by-its-length) states it as an exclusion
rather than leaving it to be found.

**Where a record sits in the stream is the automaton's shape, not a test.** The
first record of a file is the one admitted by a transition leaving the start
state, and a state is already what the automaton knows about position: the start
state is entered before any record is read. Where a layout names a record as the
file's first, `resolve` **MUST** compile a start state that no transition
re-enters, since a state distinguishes position only while it is entered once
(#36, #80). Nothing is added to express that. A first record that is also
distinguishable by content carries an ordinary predicate besides, and one that
is not carries none, under the subsection below.

What position buys is exactly one thing — which state the automaton is in before
it reads — and that is enough only where the start state offers one transition,
or where its alternatives carry predicates. It is never enough to *choose*
between two content-indistinguishable first records, because no transition
leaving the start state may carry a guard: a guard reads a register, every
register must be bound on every path before it is read ([The automaton
remembers, in registers](#the-automaton-remembers-in-registers)), and no binding
precedes the first record. So an optional header that says nothing about itself
is refused like any other pair a state cannot separate. The asymmetry with the
*last* record is real but smaller than it looks, and it is that a writer knows
which record it is writing first.

The *last* record is not available on any terms, and [The last record of a
stream](#the-last-record-of-a-stream) is why.

**Selecting no record content at all is the absence of a predicate**, not a
member of the set that happens to name nothing. That distinction is not
bookkeeping: a member naming nothing would put an unset field reference inside
the predicate node, so every consumer's switch over the set would carry a member
with no target and every rule above that says *the target* would need a
qualifier. Absent, it stays one optional reference on the transition, and the
predicate node keeps the shape [The node kinds](#the-node-kinds) gives it — the
identifier of the field it tests, and the test itself.

Settled now rather than left to the change that fixes the membership, because a
member testing something other than a field's bytes is a new member of a closed
set and costs a version under [Versioning and
compatibility](#versioning-and-compatibility) — and because #17 fixes the schema
against this answer, where a predicate carries a field reference that is always
set rather than a reference beside a choice of what else a predicate might be
about.

### A transition may carry no predicate

A transition **MAY** carry no predicate. One that does not matches every record,
and is selected by its guards alone — or, where it carries none of those either,
by being the only transition its state offers. It consumes exactly one record
like every other transition and is not an epsilon ([No epsilon
transitions](#no-epsilon-transitions-and-what-the-graph-pays-instead)).

That is what the former requirement that every transition be selected by a
predicate gives up, and it gives it up because the requirement forbade files
rather than describing them. A file with one record type has nothing for a
predicate to test — the strategy #28 calls single-record-type is this and only
this. Nor has a state whose alternatives a guard already separates: the
transition reading another detail while a counter is positive and the one
starting a new group when it reaches zero are told apart by the counter, and on
a file whose header and detail are the same shape there is nothing else to tell
them apart by. Restricting the relaxation to a state with exactly one outgoing
transition would have made the second of those unrepresentable while buying
nothing, since the overlap rule already refuses the pair a guard does not
separate.

Overlap needs no second rule. A transition carrying no predicate matches every
record, so [When two match, and when none
does](#when-two-match-and-when-none-does) reaches it unextended: it **MUST** be
the only transition eligible wherever its guards can hold, and `resolve` rejects
a state offering it beside any transition whose guards can hold at the same
time. Guards are the only thing that can separate two of them. It is not a
default arm and **MUST NOT** be read as one — it is evaluated in the order the
state carries like every other transition, it is not tried last, and it does not
catch what the others miss, because where two transitions could both apply the
layout was rejected before a consumer saw it.

The cost is detection, and it lands where the file has none to give. A state at
which such a transition is eligible admits whatever bytes are in front of it as
the record that transition names, so the undescribed-record diagnostic cannot
fire there and a file of the wrong records reads as a file of records. Framing
still checks what it can: under **descriptor-word**, **segmented** and
**delimited** a record whose extent is not where the framing says is still
reported at the record it happened on ([The extent governs, and
framing is checked against
it](#the-extent-governs-and-framing-is-checked-against-it)), and under
**unframed** nothing is. That is a property of a file whose records carry
nothing saying what they are, and there is nothing here to invent one from: a
consumer that cannot tell two records apart by content cannot tell a wrong
record from a right one either.

The loss is not bounded to the record it happens on, and saying so is worth a
sentence because the arithmetic hides it. Such a transition **MAY** also carry a
binding, so a record admitted in error can write a register, and a register
decides how many records follow it and whether the state accepts. A group two
records short can therefore be absorbed by reading the next group's header as a
detail, leaving the counter at zero at end of input and the file reported
complete — a wrong answer no rule in this document fires on. So a producer
**SHOULD** carry a predicate on a transition whose record offers a target a
predicate may name, even where guards alone would select it: a predicate the
automaton does not need in order to choose is the only detection such a state
has (#80). The permission above is for the file that has nothing to test, not
for the descriptor that would rather not test it.

One thing a transition carrying no predicate does not relax is that it consumes
bytes. A producer **MUST NOT** emit a transition admitting a record whose extent
is zero, and `resolve` **MUST** reject a layout producing one, naming the
record. Nothing else forbids an empty group, and before this section a
predicate's target had to be a field contained in the record its transition
admits — so every admitted record held at least one field of non-zero width, and
the requirement is stated here because that is what stopped being true. A
zero-extent record on a transition that matches everything is a reader that
emits records forever without advancing its read position, which is the
unterminating walk [No epsilon
transitions](#no-epsilon-transitions-and-what-the-graph-pays-instead) refused at
the front door (#80).

A writer needs no rule of its own. It narrows to the transitions admitting the
record it was asked to write whose guards hold, and one carrying no predicate
matches the bytes it is about to emit as it matches any others ([A writer walks
the same automaton](#a-writer-walks-the-same-automaton)). The requirement [A
writer evaluates a predicate, it never inverts
one](#a-writer-evaluates-a-predicate-it-never-inverts-one) places on every
member of the predicate set is met here by there being no member to meet it
(#79).

Landing now rather than after the first release, because a consumer that has not
heard of this reads an unset predicate reference as a malformed descriptor and
refuses a conforming file. Relaxing the rule later is an addition a consumer
must understand in order to stay correct ([Versioning and
compatibility](#versioning-and-compatibility)), whatever protobuf would make of
a field that was optional on the wire all along, and #17 fixes the schema
against a reference that **MAY** be absent (#80).

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

Guards narrow which pairs that rule is about. Two transitions leaving one state
whose guards cannot hold at the same time are never both eligible, and their
predicates **MAY** overlap freely — which is what makes a counted run of records
expressible at all, since the transition reading another detail and the one
moving past them can be selected by the very same test on the very same bytes,
and only the counter separates them. For every other pair — both unguarded, or
guarded compatibly — the rule above stands unchanged and a producer **MUST NOT**
emit one.

A transition carrying no predicate ([A transition may carry no
predicate](#a-transition-may-carry-no-predicate)) is inside that rule rather
than beside it. It matches every record, so it overlaps every transition leaving
its state whose guards can hold at the same time as its own, and a producer
**MUST NOT** emit one where such a transition exists. That is the whole answer
for a transition not selected by record content, and it is the same answer as
for one that is: the question was never what a predicate reads, only whether one
input can satisfy two of them (#80).

That check stays decidable because of what a guard is not. A flat conjunction of
three tests over a fixed set of declared registers, with no arithmetic in it
beyond taking one off a counter and no comparison of one register against
another, makes "can these two guard lists hold at once" a question about
literals and zero. [The automaton counts; it does not
compute](#the-automaton-counts-it-does-not-compute) is where that restriction is
stated as a restriction.

A consumer **MAY** therefore stop at the first eligible transition that matches.
The evaluation order is normative all the same, so that two consumers handed the
same bytes do the same work in the same order and report the same thing when
something is wrong with them.

Where no transition matches, the input is a record the layout does not describe.
A consumer **MUST** report that rather than skipping ahead to a transition that
matches later or falling through to a default. There is no default, and a file
containing an undescribed record is a file the layout is wrong about. A state at
which a transition carrying no predicate is *eligible* never reaches this, since
that transition matches whatever is in front of it; what is given up along with
the diagnostic is stated where that transition is. A state that merely offers
one reaches it whenever that transition's guards exclude it, which is the case
the paragraph below is about.

Where a transition's predicate *would* have matched and its guards excluded it,
a consumer **SHOULD** report that instead, naming the register the guard tested.
A transition carrying no predicate is not one of those, and a consumer
**MUST NOT** report a guard on the strength of one. It would have matched
anything, so it says nothing about the bytes in hand — reporting it would
displace the
undescribed-record diagnostic exactly where that diagnostic is right: a state
offering a guard-excluded transition that carries no predicate, beside an
eligible one whose predicate genuinely failed, has a record the layout does not
describe and a counter that is doing its job (#80). The two failures send an
adopter to different places: an undescribed record
means the layout is missing a record type, while a detail arriving after its
counter reached zero means the file and its own header disagree about how many
there are. Both are reported and neither is skipped; only the wording differs,
and it is the wording that saves a day.

### A predicate never reads past the record in front of it

A predicate is evaluated at step 3, against a record nobody has identified yet.
The record its own transition *would* admit has an extent and the target sits
inside it; the record actually in front of the consumer may be a shorter one
another transition at the same state admits, and a target past that record's
last byte is a target in the bytes behind it — the framing's, or the next
record's data. A consumer **MUST NOT** evaluate a predicate against bytes
outside the record in front of it. Two mechanisms make that true, and which one
applies is decided by the framing (#78, #94).

The guarantee is about the bytes in front of the consumer rather than about
which record they turn out to be, and that is not a loose way of putting it. A
predicate exists to be evaluated against records its own transition does not
admit — that is what selecting one is — so a rule that a predicate reads only
*its own* record's bytes would forbid the evaluation instead of bounding it.
What may not happen is a predicate reading a byte no record in front of the
consumer covers.

**Where the framing states a length, it bounds the predicate.** Under
**descriptor-word** and **segmented** the length of the record in front of the
consumer is in hand at step 2, and step 3 spends it: a predicate whose target
is not wholly within the record the framing bounds does not match. That bound
is what makes a predicate naming a field the longer record has and the shorter
one does not available under those two framings, which is why [A record told
apart only by its length](#a-record-told-apart-only-by-its-length) states that
pattern as framing-conditional.

**Where it does not, the descriptor is bounded instead.** Under **unframed**
and **delimited** nothing in the file states a length before the transition is
taken: the extent follows from the transition, and finding a delimiter is
scanning, which [The extent governs, and framing is checked against
it](#the-extent-governs-and-framing-is-checked-against-it) refuses by name. So
the bound is placed on the layout rather than on the read. Where a file node's
framing is **unframed** or **delimited**, the last byte of a transition's
predicate target **MUST** lie within the shortest record any transition leaving
the same state can admit while this one is eligible — over the transitions
whose guards can hold at the same time as its own, which is the pairing [When
two match, and when none does](#when-two-match-and-when-none-does) already
tests over. `resolve` **MUST** reject a layout breaking it, naming the record
the target is in, the target, and the shorter record it would be read past the
end of, rather than reporting a generic reference error (#37, #94).

*Shortest* is a record's extent with every repetition whose count is a
reference taken at the minimum occurrences the copybook declared ([A variable
record is a sum with a variable
term](#a-variable-record-is-a-sum-with-a-variable-term)) — bounds a repetition
already carries, read here for a second purpose. A transition carrying no
predicate is inside the pairing and never narrows it, since the overlap rule
already forbids one beside any transition eligible at the same time ([A
transition may carry no predicate](#a-transition-may-carry-no-predicate)).

A predicate's own record is inside the rule and satisfies it already, which is
worth saying because it is why the rule is about the *other* records. The
target is contained in the record its transition admits, and no item ahead of
it carries a repetition whose count is a reference ([Discriminator
predicates](#discriminator-predicates)), so it sits ahead of every item whose
width moves and inside the shortest that record can be. What the rule adds is
the records the state can put in front of it instead.

Two failures close with it, and both are files that are exactly right.

**A well-formed file reported truncated.** A **delimited** file under the
terminator placement, whose state offers an 80-byte detail with its type code
at offset 40 and a 10-byte trailer with its type code at offset 0. The file's
last record is the trailer. The detail's predicate is evaluated first, against
a record with ten bytes in front of it and nothing behind, so a consumer
reaches end of input inside step 3 — which the read loop makes a truncated
file. One consumer condemns a file that is right; another reads *target past
the input* as *does not match* and reads it correctly, and both readings are
defensible. Under the rule the layout reaches neither of them: the detail's
one-byte target ends 31 bytes past the shortest record the state can admit, and
`resolve` says so.

**A predicate matching the next record's bytes.** The same framing, a 120-byte
record whose predicate names a field at offset 100 beside an 80-byte one whose
predicate names offset 0, the longer first in the state's order. On a
well-formed short record nothing bounds the first predicate, so it tests
absolute bytes 100 to 103 — the *next* record's data — and where those bytes
happen to hold the value it tests for, the wrong transition is taken. Step 5
then reports malformed data on a well-formed file, because the 120-byte extent
does not end where the delimiter stands. This is the worse of the two, because
it is the file's own data that decides: the same descriptor reads that layout's
files correctly until the day the next record's bytes at that offset hold what
the predicate was looking for.

The guarantee is over a file the layout describes, and it has to be. A
**delimited** record whose bytes stop before its extent puts a delimiter, and
then another record's data, where a predicate expects the record's own, and no
bound computed from the layout can know it — that file is caught a step later,
when the extent does not end where the delimiter stands ([A record shorter than
its extent](#a-record-shorter-than-its-extent)). What the rule buys is that a
file that *is* right is never read wrongly, which is the whole of what was
wrong with leaving the bound to the framing alone.

**unframed** carries the rule and rarely pays for it. A fixed-length dataset's
record types all account for one LRECL ([Four framings, and none of them is a
RECFM](#four-framings-and-none-of-them-is-a-recfm)) and a data-dependent extent
is refused there outright ([A variable record does not fit a fixed-length
dataset](#a-variable-record-does-not-fit-a-fixed-length-dataset)), so the
records a state can admit are of one length and a target inside one of them is
inside all of them. Where a layout's extents disagree the rule fires, and it
fires on a layout that was already describing a file whose reader misaligns
after the first record — the failure that LRECL requirement exists to prevent,
and the one **unframed** has nothing in the file to detect. The rejection
arrives at the layout rather than at the twelfth record.

What the rule costs is a discriminator sitting past the end of the shortest
record its state can admit, on a **delimited** file, and it is worth being
exact about what is left. The adopter moves the discriminator into the bytes
every record at that state has, which is where a type code usually is already —
at the front, or in a header copybook every alternative includes, the two
shapes [Discriminator predicates](#discriminator-predicates) checks the
proposed strategies against. Where the records genuinely agree on every byte
the shorter one has, what is left to tell them apart with is how long they are,
and that is refused already and on its own terms ([A record told apart only by
its length](#a-record-told-apart-only-by-its-length)). The two refusals meet
exactly: under **unframed** and **delimited** a record's length is not in hand
at step 3 to test, and it is not in hand to bound with either.

Four cheaper answers were available, and each fails.

**Bounding the read by finding the delimiter** is scanning under another name,
and it fails on the files this project exists for. A `PIC S9(3)V99 COMP-3`
field holding `+152.50` is the bytes `15 25 0C`, and `0x15` is a line delimiter
on some mainframe code pages, so a consumer bounding a record by the first
delimiter it finds bounds it inside a number — and a predicate whose target is
genuinely inside the record is then outside the bound and does not match. The
failure changes from a wrong record admitted to a right record refused, which
is not an improvement, and [The extent governs, and framing is checked against
it](#the-extent-governs-and-framing-is-checked-against-it) has refused the
mechanism already.

**Ordering a state's transitions shortest record first** looks like this rule
for free: try the short record's predicate, and where it matches, the long
record's is never evaluated and reads past nothing. It is refused because it
makes correctness depend on an order this document has twice said decides
nothing. A consumer **MAY** stop at the first eligible transition that matches
and is nowhere obliged to, so one that evaluates the rest to check what it was
handed still over-reads; the order is normative only so that two consumers do
the same work and report the same thing, and making it load-bearing would put
the wrong record in a conforming consumer's hands. It also buys nothing at a
state where a guard puts the long record's transition first.

**Bounding by the extent of the record the transition would admit** is the
bound already in place, and it is why nothing was noticed for as long as it was
not. The target is inside that record by construction, so the bound excludes
nothing whatever. A bound has to come from the record that is *there*, and at
step 3 there are two statements of that record and no third: the framing's,
where the framing makes one, and the layout's shortest, where it does not.

**A record length on the file node** would give a consumer a number to bound
with under **unframed**, and [Lengths the file node does not
carry](#lengths-the-file-node-does-not-carry) refuses it for reasons that do
not weaken here. Under **delimited** there is no one length for it to hold.

**Reaching end of input while evaluating a predicate is a truncated file**, and
the read loop's rule reaches step 3 like every other step between 2 and 6. A
consumer **MUST** report it as truncated and **MUST NOT** report it as a record
the layout does not describe: an input holding fewer bytes than the shortest
record its state can admit stopped part-way through a record, whatever record
that was going to be. Saying so is safe only because of the rule above. Without
it, running out of input at step 3 is what a well-formed file does at a short
last record, which is the first failure above; with it, every target the state
can evaluate is inside the shortest record, so a predicate runs out of input
only where a whole record's worth of it is not there.

**A target straddling the bound does not match**, and under one framing half it
cannot arise. Where the framing states a length, a target beginning inside the
record it bounds and ending past it is not wholly within it, and a consumer
**MUST NOT** compare the part that is there against the part of the literal
that fits: a predicate tests a field's bytes, a field cut in half is not that
field, and a comparison against a prefix is the one [The automaton remembers,
in registers](#the-automaton-remembers-in-registers) refuses for a guard over a
bytes register, arriving from the other side. Under **unframed** and
**delimited** no target
straddles anything, since the rule above puts its last byte inside the shortest
record.

None of this reaches an arm's predicate. An arm is chosen inside a record
already admitted, every byte of its occurrence is locatable before any arm is
chosen, and the arms of one variant are of one extent — so there is no shorter
occurrence for one to read past the end of ([A predicate on an arm reads one
occurrence](#a-predicate-on-an-arm-reads-one-occurrence), #90).

A writer needs nothing either. It evaluates the predicates of the transitions
admitting the record it was asked to write, against the record it holds ([A
writer evaluates a predicate, it never inverts
one](#a-writer-evaluates-a-predicate-it-never-inverts-one)), and every one of
those targets is a field of that record. The bound is a reader's because the
guessing is.

Both halves land now, and neither could land later for free. The read half
changes what a conforming consumer does with the same bytes — a predicate that
matched stops matching — so a consumer that has not heard of it disagrees with
one that has about which record a file holds, which is breaking under
[Versioning and compatibility](#versioning-and-compatibility) whatever the wire
format makes of it. The `resolve` half is a narrowing, and imposing a narrowing
after the first release makes a descriptor that conformed stop conforming,
while relaxing one later asks nothing of an adopter and something of a
producer — the direction [A count is in hand before the extent it
decides](#a-count-is-in-hand-before-the-extent-it-decides) chose for the same
reason. Nothing is added to the schema for any of it: the rule says which
descriptors may exist and what a consumer does with the ones that do, so #17
fixes the schema against the node shapes it already had (#17, #94).

### A predicate on an arm reads one occurrence

An arm of a variant is selected by a predicate node — the same kind, and the
same closed set of tests, as the one selecting a transition. What differs is the
bytes it reads and the rules binding its target, and both differences come from
where it is evaluated: inside one occurrence of a group, with the record already
admitted (#90).

Its target **MUST** be contained, at any depth, in the innermost group that
repeats and contains the variant — the group one occurrence of which the arm is
being chosen for. It **MUST NOT** be contained in an arm that does not also
contain the variant, which rules out the variant's own arms and any sibling
variant's and admits an arm enclosing it, where a copybook redefines a
redefinition. `resolve` **MUST** reject a layout breaking either half, naming
the record, the repeating group, the variant and the target rather than
reporting a generic reference error (#37, #90).

Both halves are about bytes that may not be there, or may not be what they look
like. A target outside the occurrence has bytes and they are the same bytes for
every occurrence, so it selects the same arm in all of them — a choice made once
per record, which is a record node's and not a variant's ([Members never
overlap, and `REDEFINES` is resolved
away](#members-never-overlap-and-redefines-is-resolved-away)). A target inside a
sibling arm has bytes only where that arm was selected, so reading it where it
was not is reading one alternative's data as another's, which is the failure the
whole of this document's treatment of `REDEFINES` is arranged around.

Its position is the sum this document already states, run from the first byte of
the occurrence rather than from the first byte of the record: the widths of
everything ahead of the target inside that occurrence, added to where the
occurrence begins. A consumer evaluating an arm is walking that table and is
already standing there, so this is [Dereferencing is not
recomputation](#dereferencing-is-not-recomputation)'s second list and not its
first.

**The rule against naming a field that repeats does not reach this position**,
and the reason is that rule's own. [A reference names a field, not an occurrence
of one](#a-reference-names-a-field-not-an-occurrence-of-one) refuses a reference
that names a field in forty entries while carrying nothing that says which of
the forty, because reading it as the first is a guess at an intention the
descriptor cannot express. An arm's predicate expresses it: it is evaluated in
an occurrence, so the occurrence is the one in hand and there is nothing left to
guess. What that rule refuses is a reference with no occurrence to be read in.

**Nor does the constant-position restriction**, and that reason is the read
loop's order. A transition's predicate must sit at a constant position because
it runs at step 3, before its record is admitted, so a position that moves with
a count obliges a consumer to decode that count out of bytes it has not yet
identified. An arm's predicate runs while a record already admitted is being
walked, and every byte of the occurrence is locatable before any arm is chosen
because the arms are of one extent. So a target **MAY** sit behind the variant,
and **MAY** sit behind an item of the occurrence whose own extent moves with a
count the record carries ahead of it.

**Nor does the bound on where a target may sit.** A transition's predicate may
be evaluated against a record shorter than the one it would admit, so what
bounds it is the shortest record its state can put in front of it ([A predicate
never reads past the record in front of
it](#a-predicate-never-reads-past-the-record-in-front-of-it)). An arm's is
evaluated in an occurrence already located, of a record already admitted, and
one occurrence of a group is the same width as another — so there is nothing
shorter for it to be read past the end of, and its containment in the
occurrence is the whole of the bound (#94).

An arm carries no guards, and there is nothing for one to do. A guard reads a
register, a register holds one value for the whole of a record's read ([The
automaton remembers, in registers](#the-automaton-remembers-in-registers)), and
a value that is the same in every occurrence selects a record rather than an arm
— which is what the rule on the target already says, arriving from the other
side.

**Two arms that can both match are refused; none matching is reported.** Two
arms of one variant **MUST NOT** be selected by predicates that can both match
the same occurrence, and `resolve` **MUST** reject a layout whose alternatives
overlap, naming the record, the repeating group and both arms (#37, #90). That
is [When two match, and when none does](#when-two-match-and-when-none-does) at
this scope and for its reasons, so the arms' order decides nothing and a
consumer **MAY** stop at the first arm that matches. The order is normative all
the same, so that two consumers do the same work in the same order and report
the same thing when something is wrong with the bytes.

Where no arm matches, a consumer **MUST** report it, and **MUST NOT** fall
through to the last arm or leave the occurrence's items unset. There is no
default arm and a producer cannot write one. What a consumer **MUST** do besides
is say which of two failures it has, because they send an adopter to different
places: a record no transition matched is a record type the layout is missing,
while an occurrence no arm matched is a record the layout does describe carrying
an entry it does not. An adopter sent to the first for the second spends the day
on a record type they already have. One whose entries carry a code the
alternatives do not cover writes an arm for the residue, spelled as the test
over the codes it covers — the set carries no member matching whatever is left,
and [Discriminator predicates](#discriminator-predicates) is where that is
settled (#22, #28).

## Writing a file

Every section above is written from the reading side — what a consumer does with
a descriptor when it has bytes and wants records. A generator emitting encode
methods (#51) and a file-level writer (#52) needs the other direction, and
whether that direction is inside this document's contract is answered here
rather than once per generator.

It is inside it. A file written against a descriptor and a file read against one
are the same file, and a document specifying only one of the two leaves the
other to be invented in every language a generator is written in — which is the
failure every section above is arranged to prevent. Whether a generator emits a
writer at all remains its own decision (#52); what one does when it exists is
this document's.

*Writer* below means code generated from a descriptor that turns records into a
file, as *reader* means the code the sections above describe. A file a writer
produces **MUST** be one that a reader built from the same descriptor reads back
as the records the writer was given, in the order it was given them.

Most of that is free, because almost nothing the IR carries has a direction.
Ordering and width give a field's position whether it is being read out of a
record or laid into one. The four axes govern encoding a value exactly as they
govern decoding one. A slack node's bytes are bytes on both sides. What does
have a direction is the automaton and the predicates driving it, because both
are stated as *tests*, and a test says how to recognise a record rather than how
to make one. That is what the first two subsections settle. The third settles
the two places where the bytes a writer emits are not its caller's to choose.
The fourth does both again for what the automaton remembers, where a guard is a
test like a predicate is and a count in a register is determined like a count in
a field (#76, #77).

Byte identity is a stronger claim, and this document makes it of a record and
not of a file. Bytes no item covers do survive a read ([Slack survives a
read](#slack-survives-a-read)), so a record read and written back unchanged is
byte-identical wherever encoding a value reproduces the bytes it was decoded
from — `codec/SPEC.md`'s question rather than this one's — and wherever every
item of it has a value a generator can use, which [Ordering and width, and no
offset](#ordering-and-width-and-no-offset) says is not every item: a
numeric-edited or `POINTER` item carries a width here and no value, so a writer
has no more source for its bytes than it had for slack, and this document has
not given it one.

A *file* is not byte-identical, and two of its own rules are why. Under
**optional terminator** a writer emits a final delimiter the input may not have
carried, and under **segmented** it lays a record into as few segments as the
largest allows whatever the input did ([Where framing is consumed, and where it
is emitted](#where-framing-is-consumed-and-where-it-is-emitted)). Both are
deliberate, and neither is slack's.

### A writer walks the same automaton

A writer begins in the start state the file node names, exactly as a reader
does. Its caller names a record and supplies the values of that record's items.

A writer **MUST** consider only the transitions leaving the current state that
admit the record it was asked to write and whose guards all hold, **MUST**
evaluate their predicates in the order the state carries them, and **MUST** take
the first that carries no predicate or whose predicate matches the bytes it is
about to emit. Evaluation order is normative here for the reason it is normative
for reading: two writers handed the same records do the same work in the same
order. Guards come first here as they come first there, and why they behave
identically in both
directions is [A writer evaluates a guard, it never back-fills a
count](#a-writer-evaluates-a-guard-it-never-back-fills-a-count)'s.

Narrowing to the transitions that admit the record is not the reader's algorithm
and has to be one, because the reader's does not run backwards. A reader has a
byte window and can try any predicate against it; a writer has a record, and a
predicate belonging to a transition admitting some *other* record names a field
at an offset the record in hand may not even reach. What makes the narrowed walk
land in the same place anyway is [When two match, and when none
does](#when-two-match-and-when-none-does): no two transitions leaving a state
can both match one input, so bytes satisfying the predicate of a transition
admitting this record satisfy no earlier transition's predicate, and the reader
arrives at the transition the writer took. That rule is load-bearing on this
side too, and without it a writer could emit a record its reader routes
somewhere else.

Two transitions may admit the same record and differ only in the state they move
to — a header deciding whether a later record type appears at all is written
that way. A writer needs no rule for that beyond the one above, and for the
reason reading needs none: a transition is never labelled with a record name, so
the predicates decide there as they decide everywhere else, and where neither of
the two carries one the guards do — nothing else could, since no two eligible
transitions leaving a state may both match ([When two match, and when none
does](#when-two-match-and-when-none-does)).

Where no transition matches, a writer **MUST** report it, and **MUST NOT** emit
the record anyway or take a transition whose predicate is false. The record does
not belong at this point in the file with the values it has, and a writer
emitting it produces a file whose reader reports an undescribed record ([When
two match, and when none does](#when-two-match-and-when-none-does)). Refusing
where the mistake is made costs one diagnostic; emitting costs a file somebody
has to read before anyone finds out.

When its caller has no more records, a writer **MUST** report a current state
that does not accept, or whose acceptance guards do not all hold, rather than
closing the file. A group that promised four details and was given three is
caught there. That is the truncation rule
of [The sequencing automaton](#the-sequencing-automaton) from the other side,
and it is there for the same reason: accepting states nobody checks detect
nothing, and a writer skipping the check emits the truncated file its reader
complains about one build later.

Framing is the file node's, and a writer emits it as part of this walk rather
than around it. [Where framing is consumed, and where it is
emitted](#where-framing-is-consumed-and-where-it-is-emitted) says which bytes go
where in each direction, and the walk above is what those steps are steps of.

### A writer evaluates a predicate, it never inverts one

A writer **MUST** evaluate a transition's predicate against the record it is
about to emit, and **MUST NOT** derive a value satisfying that predicate and
store it into the predicate's target. The discriminating value is the caller's;
a writer checks it and reports it when it is wrong.

The alternative is the shape most people expect, and it was worth taking
seriously: a writer knowing its transition is selected by *type code equals
`"D"`* could set the type code itself, and spare its caller a field whose one
correct value the descriptor already holds. It loses on four counts, and the
first decides it.

Only an equality test inverts uniquely. A test of the form *not equal*, *in
range* or *any of* — none of which the set has admitted yet, and any of which it
plausibly will — is satisfied by many values and singles out none, so a rule
that the writer supplies the value has no content across most of the set it
would govern.

Making it hold would mean carrying one, and a canonical satisfying byte string
beside each predicate is a fact stated twice in the sense [Ordering and width,
and no offset](#ordering-and-width-and-no-offset) uses — the predicate already
says which values satisfy it. That duplicate is at least checkable, a consumer
being able to evaluate the predicate against the value it was handed, which is
the only reason it was arguable rather than refused by that section outright.
Carried, it would still have nowhere to go. Its target is a field of the record,
and the caller supplies that field's value along with every other: a writer
overwriting it discards data the caller meant, and a writer that does not
ignores what it was given. A generator could hide the field instead, and then
its two halves disagree about the record's shape — the reading side surfacing a
field from the same descriptor that the writing side denies exists.

A rule varying by predicate kind is worse than either. Supplying the value where
the inverse is unique and checking it where it is not gives one call two
behaviours, selected by a property of the layout the caller cannot see: setting
the type code is load-bearing in one file's records and silently discarded in
another's.

And leaving each generator to derive a value rather than carrying one is the
divergence this document exists to abolish. Two languages' writers choose
different bytes for the same records, each file reads back correctly through its
own reader, nothing is wrong enough to fail, and the conformance corpus (#66) is
left comparing files that were never given a reason to agree.

The cost lands on the caller and is not softened here. A predicate that does not
pin its target down gives an application no help choosing a value — *not equal
to `"H"`* says what the field must not be and leaves the rest of its range open.
That is a property of the layout rather than of the IR: a discriminator not
determining a value describes a file whose value is the application's, and this
document is not the place to invent one on its behalf. A generator **MAY**
soften it at construction rather than at emission — a constructor pre-filling a
field where a predicate's inverse is unique is a default the caller can see and
change, and the bytes emitted are still the ones the caller holds. Ergonomics
over the contract are the generator's, the way [Names](#names) leaves identifier
munging to it.

The set's membership landed with the strategies that lower into it (#22, #28),
and this is the requirement it landed against. Every member **MUST** be
decidable by a writer against the record it is about to emit, from that record's
bytes and the writer's own position in the automaton, at the moment it emits it.
Weaker than invertibility, stricter than nothing, and it is what a writer can
actually meet. Both members meet it by comparing the target's bytes against
literals the descriptor carries, which asks the writer for nothing it does not
already hold — and the shape below is the one that would not have.

One shape fails it, and it is worth naming because it is the shape a
discriminator by position takes: a test for the *last* record in a stream. A
reader knows a record is the last one when the input ends; a writer does not
know until its caller says there are no more, which is after that record has
been written, and buffering until then gives up the streaming property #52
requires. It is not in the set, and it is not anywhere else either: [A predicate
always names a field](#a-predicate-always-names-a-field) settles that a member
tests a field's bytes, and [The last record of a
stream](#the-last-record-of-a-stream) refuses the shape outright (#80).

The requirement binds members, so a transition carrying no predicate has nothing
to meet it with and meets it — there is no value for a writer to decide about
([A transition may carry no
predicate](#a-transition-may-carry-no-predicate)).

An arm's predicate is evaluated the same way and inverted no more readily, once
per occurrence. A writer **MUST** evaluate the predicate of the arm its caller
supplied against the occurrence it is about to emit, **MUST NOT** derive a value
satisfying it and store it into that predicate's target, and **MUST** report an
occurrence whose values satisfy no arm's predicate rather than emitting it —
naming the record, the repeating group and which occurrence it was, since a
record whose third entry is wrong is not something its reader could tell anybody
about afterwards. Every argument above carries over unchanged, and the
member-decidability requirement is met at this scope by the same members meeting
it at the other. What a writer emits for that occurrence is the arm's items and
the arm's slack, which come to the variant's width whichever arm it was, so no
occurrence a writer emits is longer or shorter than another (#90).

### What the descriptor determines, a writer supplies

The rule above is narrower than *a writer never fills anything in*. A writer
supplies a value exactly where the descriptor determines one and refuses where
it determines a set. Inside a record the IR determines two; the framing bytes
around one are the file node's and are determined there ([Where framing is
consumed, and where it is
emitted](#where-framing-is-consumed-and-where-it-is-emitted)).

An `OCCURS DEPENDING ON` count is determined. The repeating item names the field
holding its count, and that field's value *is* the number of occurrences: a
writer **MUST** emit it as the number of occurrences it writes, and **MUST NOT**
emit a different value its caller left there. No choice among satisfying values
is being taken away from anybody, and a count disagreeing with what follows it
is a record the writer's own reader cannot walk. Where the count is not a field
of the record at all but a register the automaton bound (#35, #77), it is
determined too and the direction reverses: the register already holds a value,
so the occurrences are what has to agree with it. [A writer evaluates a guard,
it never back-fills a
count](#a-writer-evaluates-a-guard-it-never-back-fills-a-count) states that.

More than one repeating item of a record **MAY** name that one field, and then
it is determined more than once ([One count may size two tables, and a writer
refuses to
choose](#one-count-may-size-two-tables-and-a-writer-refuses-to-choose)). A
writer **MUST** report a caller supplying different numbers of occurrences for
two repetitions naming one count, and **MUST NOT** emit the record with either
number as the count. The comparison is over every repetition naming that field
rather than over a pair, because a table inside a group repeating on the same
count contributes one number per occurrence of that group. Picking is the shape
being refused rather than an ergonomic loss: one of the two tables is then sized
by a number that was never about it, every item behind that table slides with
it, and the record the writer's own reader recovers is not the record its caller
handed over. Reporting it names the record, the count and the repetitions whose
numbers disagree, on the call that made the mistake (#89).

Either way a writer **MUST** report a caller supplying a number of occurrences
outside the repetition's declared minimum and maximum, rather than emitting a
count its own reader is required to call malformed data ([A variable record is a
sum with a variable
term](#a-variable-record-is-a-sum-with-a-variable-term)) — the same trade the
paragraphs above keep making, one diagnostic where the mistake was made against
a file somebody has to read first (#87). None of this reaches a record of a
non-sliding file, which carries no count reference for a writer to determine:
its table is a fixed one and its count field is its caller's like every other
field ([An item after a table slides, and the other reading is a fixed
table](#an-item-after-a-table-slides-and-the-other-reading-is-a-fixed-table)).

Slack is determined by the record where the record was read, and here where it
was not. A writer emits the bytes retained for a slack node ([Slack survives a
read](#slack-survives-a-read)); where a record carries none, because its caller
built it rather than read it, a writer **MUST** emit zero bytes. A character
fill is the obvious alternative and cannot be specified — charset is per field
([The encoding profile, applied](#the-encoding-profile-applied)) and slack is
not a field, so there is no charset to resolve a space against. Zero is the byte
that names none. Carrying the fill on the slack node instead would move the same
invented constant one stage earlier, to where `resolve` has no source for it
either: neither a copybook nor a layout says what an alignment gap contains.

The invention is confined to bytes that were never in a file, so slack is no
longer what stands between a record carrying it and #51's round-trip criterion —
decode then encode reproduces the original bytes. What still stands there is
named in [Writing a file](#writing-a-file)'s preamble, and none of it is
slack's. A caller that wants a constructed record's slack to hold
something other than zero declares a trailing item and supplies it, as [Four
framings, and none of them is a
RECFM](#four-framings-and-none-of-them-is-a-recfm) says for the fixed-length
padding that raises this most often (#82).

### A writer evaluates a guard, it never back-fills a count

A writer carries a register file, exactly as a reader does, and fills it the
same way: after emitting a record, it applies the transition's bindings by
reading the values out of the record it has just emitted. A binding needs no
inverse, because the value it wants is one the caller supplied — a header's
count field is a field of the header like any other.

So the writing side of a guard is the reading side of one. A writer **MUST**
evaluate a transition's guards against its own register file before considering
that transition's predicate, and **MUST NOT** take one whose guards do not all
hold. Where narrowing to the transitions admitting the requested record leaves
none eligible, it reports, under the same rule and for the same reason as a
predicate that does not match: a caller writing a sixth detail after a header
saying five is told so at the sixth record, and told which register said
otherwise, rather than handed a file whose reader tells somebody else next week.

What a writer **MUST NOT** do is the thing everybody expects it to: hold the
records of a group, count them, and go back to fill in the count field of the
header it already emitted. Every argument [A writer evaluates a predicate, it
never inverts one](#a-writer-evaluates-a-predicate-it-never-inverts-one) makes
applies here unchanged — the value is the caller's, overwriting it discards what
the caller meant, and two languages' writers would choose differently — and two
more apply only here. Holding a group gives up the streaming property #52
requires, and it is unbounded in precisely the case a count exists for. And the
header is gone: a writer that has emitted a record cannot reach back into a
stream it does not own.

The determination runs the other way in one place, and [What the descriptor
determines, a writer
supplies](#what-the-descriptor-determines-a-writer-supplies) is where it is
named: a repetition whose count is a register. The register holds a value from a
record already emitted — bound by a transition taken strictly earlier and read
as it stood on entry to the state, exactly as a reader reads it ([A count is in
hand before the extent it
decides](#a-count-is-in-hand-before-the-extent-it-decides)) — so a writer
**MUST** emit exactly that many occurrences of the group, and **MUST** report a
caller supplying a different number rather than emitting a record its own reader
cannot walk. An `OCCURS DEPENDING ON` count in the same record is supplied by
the writer because the field is sitting there to be filled; a count in a
register was filled two records ago, and what has to agree with it is the data.

The requirement that section places on future members of the predicate set —
that every one be decidable by a writer from the record it is about to emit and
its own position in the automaton — the guard set meets already, and not by
luck. A guard reads nothing but the register file, and the register file is the
one thing a writer and a reader hold identically at the same point in a file.

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
- Adding a node kind, or a member to any other closed set — a predicate kind, a
  guard test, a register's value kind, the value a binding may write, a framing,
  a delimiter's placement. An old
  consumer sees an unset choice where a new one sees a member,
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

It is not the IR Go module's tag (#18) — `irpb/vX.Y.Z`, on
[`github.com/Zaba505/cpybkc/irpb`](../../irpb) — which follows Go's module rules
and moves for reasons, a dependency bump or a documentation fix, that say
nothing about the descriptor. It is not the two-sided assertion generated code carries
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
(#20), [rendered as JSON and read](#a-descriptor-is-readable-by-a-person) (#21),
committed, diffed, and replayed against a plugin by hand a year later. None of
those is available to a protocol.

## Reading a descriptor without generated code

The section above claims self-description. This is the artifact that delivers
it, and what a consumer may assume about it.

A `FileDescriptorSet` describing this schema is published with every release, as
the asset **`ir.binpb`** (#19). It is protobuf's own reflection type, encoded in
the protobuf binary wire format, and every protobuf runtime can build message
types out of one at run time. A consumer holding it needs no `.proto` file, no
compiler and no build step to decode a descriptor.

The schema sources are published beside it, as **`ir-protos.tar.gz`**, each file
at the path its protobuf package requires. That is for the consumer whose build
*can* run a compiler and would rather generate bindings; the two are
alternatives, and neither is derived from the other. The published image carries
both, at paths the [container contract](../container/SPEC.md#the-ir-schema-in-the-image)
fixes: the same `ir.binpb` bytes at `/usr/local/share/cpybkc/ir.binpb`, and the
same sources, unpacked, under `/usr/local/share/cpybkc/proto/` (#57). Each is
one artifact reachable two ways, not two artifacts, and that document's worked
example is a generator decoding a descriptor with nothing but the first of
them.

### What the set contains

The set **MUST** carry the file defining `cpybkc.ir.v1.Descriptor` and the
transitive closure of that file's imports, and **MUST NOT** carry anything else.
The root is the message a plugin is handed, so a schema file this project might
add that nothing reachable from `Descriptor` imports is not part of the IR, and
is not published as though it were.

It **MUST** be self-contained: every path named as a dependency by a file in the
set is a file the set also carries. A consumer's registry fails on the other
arrangement, and it fails naming a file the consumer has never heard of.

Files **MUST** appear in dependency order — a file only after every file it
imports. A consumer that walks the set once, building each file's types as it
goes, is the shape most dynamic protobuf APIs make easiest, and the other order
fails it at a type reference rather than at a file.

The encoded bytes **MUST** be a function of the schema alone: two builds of one
commit produce one artifact, and the artifact changes only when the schema does.
Otherwise a rebuild is indistinguishable from a change to the contract, and an
adopter diffing two releases learns nothing.

### What is deliberately absent

Comments and source positions. The set carries field numbers, types, names and
nesting — everything a decode needs — and no `SourceCodeInfo`, so it describes
the schema rather than the document. The prose is this specification's, and a
copy of it inside a binary artifact would be a second one to keep current. A
reader who wants the comments wants `ir-protos.tar.gz`.

### Reading one

Four calls, in every runtime that has them, whatever they are spelled:

1. Decode `ir.binpb` into a `FileDescriptorSet`.
2. Build a type registry — a file or descriptor pool — from it.
3. Look up `cpybkc.ir.v1.Descriptor` by name.
4. Make a dynamic message of that type and decode the file cpybkc passed as
   `--descriptor` into it.

Everything this document requires of a consumer still binds one that arrived
this way. In particular it **MUST** read the version field before anything else
and **MUST** refuse a version it does not understand ([The version
field](#the-version-field)) — a dynamic decode makes an unknown version easier
to read, not less binding, because nothing failed to compile on the way in.

The runnable form is `Example_readADescriptorWithoutGeneratedCode` in
[`irpb/descriptor_set_test.go`](../../irpb/descriptor_set_test.go). It is a test
rather than a listing here so that the example is checked on every run: the four
calls above are the shape, and a worked example that had rotted would be worse
than none.

## A descriptor is readable by a person

The encoding above is protobuf, which nobody reads. cpybkc **MUST** therefore
also render a descriptor as JSON, and that rendering is the debugging surface:
the answer to *what was this generator actually handed*, which is the first
question asked whenever generated code is wrong. It is what `--emit-ir` writes
when `--emit-ir-format json` asks for it (#21), to the same destinations the
binary form goes to.

It is also the interop path for a consumer whose language's protobuf support is
too weak to be worth fighting. That consumer is the same one
[Reading a descriptor without generated code](#reading-a-descriptor-without-generated-code)
serves, arriving with less: no code generation, and no dynamic decode either.

### The binary form is canonical

The rendering is **one-way**, and no consumer is entitled to be handed one. A
generator plugin is handed the binary encoding — what a plugin reads is the
[plugin contract](../plugin/SPEC.md)'s to fix (#39) — and a plugin accepting
either form would have to sniff which one it had, which is a decision about
bytes that every generator would then make for itself and some would make
differently. So a producer **MUST NOT** write a rendering where a descriptor is
called for, and a consumer **MUST NOT** rely on one to round-trip: nothing in
this document defines a JSON *encoding* of a descriptor, and every requirement
it makes is a requirement about the binary form.

That is why the two are not a pair of equals with a default between them. A
tool offering both **MUST** produce the binary form where nothing asks
otherwise, so that the form somebody depends on is the form they get by not
deciding, and the rendering is the one that has to be asked for by name (#21).

### The rendering is normalized

Two renderings of one descriptor **MUST** be byte-identical, and a rendering
**MUST NOT** vary with the build of the tool that produced it (#21). That is
[Identity, ordering and determinism](#identity-ordering-and-determinism)'s
promise carried one step further out, and it is here for the reason that
promise is: a rendering is a thing to commit beside a layout, diff against last
week's, and paste into an issue, and one that churned on an upgrade could not be
any of the three.

It is worth stating because the obvious implementation does not satisfy it. A
JSON printer is entitled to vary its insignificant whitespace, and Go's
`protojson` varies it deliberately — per binary, to stop anyone depending on its
exact output — so a rendering re-emitted after an upgrade would differ on every
line with nothing in it that anybody changed. cpybkc normalizes the whitespace
before writing.

A rendering **MUST** use protobuf's own field names — `start_state_id`, not
`startStateId`. Anyone reading a descriptor is reading it beside this document
and beside [`ir.proto`](../../proto/cpybkc/ir/v1/ir.proto), and a
lowerCamelCase rendering would make them translate every name back before they
could look one up.

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

### Undefined-length records

A dataset whose records carry no stated length — RECFM=U — has **no framing**
here, and `resolve` **MUST** reject a layout declaring one, naming the dataset
rather than reporting a generic framing error (#26).

Reason: a U-format record's extent is not in the file. It came from the physical
block the access method read, and a byte stream on a filesystem has lost it. A
framing member for U would describe a file no conforming consumer could read:
there would be nothing to check an extent against and nothing to say where the
next record starts, so the first record whose type was guessed wrong would take
the rest of the file with it in silence. Where every block of such a dataset is
in fact the same size, the file is a fixed-length one and the layout says so;
where they are not, the boundaries have to be put back by whatever writes the
stream.

### Block descriptor words in the stream

A stream whose records are grouped into blocks, each preceded by a block
descriptor word, is **not describable**, and `resolve` **MUST** reject a layout
declaring one, saying that the transfer has to deblock (#26). A blocked
*dataset* is ordinary — RECFM=VB resolves to **descriptor-word** and RECFM=FB
to **unframed** — because what blocking is on the mainframe is not what arrives.

Reason: the transfer paths that put a mainframe dataset on a filesystem deblock
it. What arrives from a transfer that preserves record boundaries is a run of
records with their descriptor words and no block descriptor words at all, and a
stream that still has them is a dataset image rather than a record stream.
Admitting them would put a second level of framing into every generated reader
in every language and a block size onto the file node for a writer to place them
by, for a file shape almost none of those readers will be handed. That is the
trade [No epsilon transitions, and what the graph pays
instead](#no-epsilon-transitions-and-what-the-graph-pays-instead) makes, with
the cost landing anywhere other than on the consumers, of which there are as
many as there are languages. Admitting them later costs a version under
[Versioning and compatibility](#versioning-and-compatibility), and that is the
price of being wrong about how these files arrive.

### A record shorter than its extent

A **delimited** file whose records stop early — trailing spaces dropped on the
way out, as GnuCOBOL's and Micro Focus's line-sequential organisations do — is
**not describable**. A consumer **MUST** report a delimiter found before the
record's extent as malformed data, and **MUST NOT** accept the short record by
filling in the bytes it never reached.

Reason: completing a short record means deciding what those bytes hold, and that
runs into the wall [A delimiter is bytes, not a
character](#a-delimiter-is-bytes-not-a-character) describes from the other side.
The answer would be a space, a space belongs to a charset, and charset is per
field — so a record cut in the middle of a field, or before a packed one, has no
answer rather than an awkward one. The fork is real and `layout/SPEC.md`'s
*Ambiguity* note already names it, so this is a deferral and not a denial: it
costs a version to admit later, because a consumer ignoring a field that says
the file is written this way reads the file wrong rather than reading it
incompletely. The diagnostic above is what keeps that bill visible — an adopter
whose files are this shape is told so at the first record, rather than finding
out from a field that is short one Tuesday in March.

### The automaton counts; it does not compute

The register machinery is a counter, not a calculator. There is no addition, no
scaling, no count that is the sum or the product of two fields, no comparison of
one register against another, and no guard comparing a register to a field of
the record in front of the consumer. `resolve` **MUST** reject a layout needing
any of them, and **MUST** name the record and the field involved rather than
reporting a generic ambiguity (#36, #37) — the pattern #35 already sets with its
explicit unsupported error.

Reason: two things have to stay true, and each of them fails at the same place.
Overlap between two transitions has to be decidable, and it is decidable only
while a guard is a flat conjunction of tests against literals and zero — a
register compared against a field, or against another register, turns the check
into "read a file and see". And a consumer has to evaluate the whole of it
identically in every language one is written in, which is the argument
`layout/SPEC.md` already makes when it closes discrimination against a general
expression language. A cross-record equality check — a detail's key matching its
header's — is a validation rule rather than a sequencing one, and adding it
later costs a version under [Versioning and
compatibility](#versioning-and-compatibility). That is a price worth paying over
guessing at its shape now, and the diagnostic above is what keeps the bill
visible: a layout that needs one is told so, instead of being told its
discriminators overlap.

### A record with nothing outside a table to name

A record every field of which is contained in a group that repeats — a record
whose whole body is one table — **cannot be discriminated**, and `resolve`
**MUST** reject a layout in which such a record has to be told apart from
another record admitted by a transition whose guards can hold at the same time,
naming the record and saying that it offers no target a predicate may name
(#37, #84, #80).

Reason: two transitions that can be eligible at once have to be told apart by a
predicate, a predicate names a field contained in the record it admits, and [A
reference names a field, not an occurrence of
one](#a-reference-names-a-field-not-an-occurrence-of-one) forbids a target
inside a repeating group. Where the record has no field outside one, those three
leave nothing to select it with. The rule is not softened for this case, because
the reason it exists does not weaken: the first entry of a table is exactly
where a discriminator looks right and reads a value belonging to the data rather
than to the record's identity. What the adopter does instead is
declare the discriminating bytes as a leading item outside the table, which is
what [Also out of scope](#also-out-of-scope) already says for a carriage-control
byte and for the same reason. Where the copybook genuinely cannot be changed,
what is left is a transition carrying no predicate ([A transition may carry no
predicate](#a-transition-may-carry-no-predicate)), which admits such a record
wherever nothing has to be told from it — a file of one record type, or a state
whose alternatives a guard already separates. That is why the rejection above is
narrower than this heading: what is refused is the record standing beside
another at one state, and not the record (#80).

A variant does not rescue this, and it is worth saying because it looks like it
should. It chooses an alternative inside an occurrence, and only once the record
has been admitted ([A variant is chosen once per
occurrence](#a-variant-is-chosen-once-per-occurrence)); what is missing here is
a target for the transition that would admit the record at all, and no arm is
evaluated until that transition has been taken (#90).

### A record told apart only by its length

A record told apart from another only by how long it is is **not describable**,
and `resolve` **MUST** reject a layout whose discriminator for a pair of
transitions is the records' length, naming both records and saying that
a predicate tests a field's bytes and a record's length is not one (#28, #37,
#80).

The rejection is keyed on what the layout asks for, because that is the only
thing `resolve` can test. Whether two records are *in fact* told apart by some
field is a property of the adopter's data, not of the copybook and the layout in
front of it — two records with a byte in common at a constant offset always
offer a field a predicate may name, and whether its value ever differs is
answered by the dataset. So a rule keyed on "nothing a field can test" would
have an antecedent `resolve` could never establish and a diagnostic that was
false whenever it fired, telling an adopter their records offer no nameable
field while the copybook in their other window shows several.

Nothing is lost by keying it that way, because a layout that reaches this shape
without asking for it is already refused: two transitions eligible at one state
and selected by nothing are two transitions carrying no predicate, which overlap
and which `resolve` rejects under [When two match, and when none
does](#when-two-match-and-when-none-does). This entry exists for the wording,
not for the rejection.

Reason: [A predicate always names a
field](#a-predicate-always-names-a-field) refuses a length test from both sides
at once — under **unframed** and **delimited** there is no length in hand to
test, and under **descriptor-word** and **segmented** the length is a framing
byte's value, which no predicate sees. That leaves the pair describable only
where the records differ somewhere a field reaches, which is the ordinary case:
a type code, a header copybook every alternative includes, or a field whose
value differs between them. Nothing weaker than that third one will do, and the
near-miss is worth naming because it looks like it should work: under
**descriptor-word** and **segmented** a predicate naming a field the longer
record has and the shorter one does not is not matched by a short record, since
the read loop makes a predicate whose target is not wholly within the record the
framing bounds not match. Under **unframed** and **delimited** that predicate is
not available at all: there is no length in hand to bound it with, so [A
predicate never reads past the record in front of
it](#a-predicate-never-reads-past-the-record-in-front-of-it) refuses the target
at the layout rather than letting it read the next record's bytes (#94). But it
settles only the longer record's transition. The
shorter one still needs a predicate of its own, and unless that predicate is
unsatisfiable by any input the longer transition admits, the two overlap on a
long record and `resolve` rejects the pair anyway. Being unsatisfiable there is
being a field whose value differs, which is where the sentence started. And
there is no arm that catches what the others missed, for the reason [A
transition may carry no
predicate](#a-transition-may-carry-no-predicate) gives.

Two transitions whose guards cannot hold at the same time are outside all of
this, as they are outside the overlap rule. A state offering one transition
guarded on a counter that admits an 80-byte record and another guarded on the
counter's exhaustion that admits a 133-byte one is not telling them apart by
length; it is telling them apart by the register, and [A transition may carry no
predicate](#a-transition-may-carry-no-predicate) admits it whatever the two
extents are.

Admitting the shape later means a member of a closed set that reads a framing
byte, which costs a version under [Versioning and
compatibility](#versioning-and-compatibility) and reopens what that member does
to the framing check. The diagnostic above is what keeps that bill visible: an
adopter whose file is this shape is told at the layout rather than at the
record.

### The last record of a stream

A transition selected by its record being the last one in the input is **not
describable**, and `resolve` **MUST** reject a layout asking for one, naming the
record and saying that a writer does not know which record is last (#28, #37,
#80).

Reason: [A writer evaluates a predicate, it never inverts
one](#a-writer-evaluates-a-predicate-it-never-inverts-one) already names the
failure. A reader knows a record is the last one when the input ends; a writer
does not know until its caller says there are no more, which is after that
record has been written, and buffering until then gives up the streaming
property #52 requires. The reader is not the reason, and the tempting version of
the argument — that a reader could not do it either — is only sometimes true. It
holds where the candidates differ in extent, since a reader would have to know
which record it is in order to know where to look for the end of the input; but
where they are the same length, "does the input end after this record" is a
question about bytes the reader can already locate, and it could answer it. What
it could not do is agree with a writer, and [Writing a file](#writing-a-file)
requires that a file written against a descriptor and a file read against one be
the same file.

What is refused is the distinction, not the file. A run of details ending in a
detail-shaped trailer is describable as a run of one record type, and reads and
writes back byte for byte; what the adopter gives up is a name for the last
record and the structural check that name would have bought. The refusal is
therefore the same narrowing its neighbours carry, and for the same reason.

What an adopter usually has instead is a trailer that says it is one, which is
an ordinary predicate, or a count in a header, which is a register and a guard
([The automaton remembers, in
registers](#the-automaton-remembers-in-registers)): a state whose counter has
reached zero admits the trailer and nothing else, and the file being over is
caught by guarded acceptance rather than by a test on the trailer's position.
Neither is available to a file with no header and no distinguishable trailer,
and that adopter's answer is the paragraph above — the trailer is a record like
the others, and which one it was is the application's, the way a trailer's count
is already ([A value the automaton has not read
yet](#a-value-the-automaton-has-not-read-yet)).

The first record is treated differently, and not arbitrarily: a writer knows
which record is first, so the asymmetry is the writer's knowledge and not a
preference. It is narrower than it looks all the same, and [A predicate always
names a field](#a-predicate-always-names-a-field) states its limit.

### A value the automaton has not read yet

Sequencing **MUST NOT** depend on a value in a record the consumer has not read.
A trailer's record count cannot govern how many records precede it, and a
repetition **MUST NOT** name a register unless every path binds it on a
transition taken strictly earlier than the one admitting the record the
repetition is in. A binding on that admitting transition is not one, since the
extent is needed before that binding applies ([A count is in hand before the
extent it decides](#a-count-is-in-hand-before-the-extent-it-decides), #88).

Reason: a consumer reads a stream forward, and one that could not decide a
transition until a later record arrived would have to buffer an unbounded
stretch of a file whose whole premise is that it does not fit in memory. Nor is
this a check left to run time: `resolve` proves every read of a register is
preceded, on every path, by a binding on an earlier transition (#36), so what
would have been a surprise halfway through a hundred-million-record file is a
layout rejected before a byte is read. Checking a trailer's count against the
records actually read is a generator's own business and nothing the automaton
needs to carry.

### Also out of scope

- **COBOL source syntax and byte-level data representation.** Both are
  `cobol-go`'s, cited above and never restated. The IR names an encoding; what
  the bytes of that encoding are is `codec/SPEC.md`'s answer.
- **Carriage control.** An ASA or machine control byte at the front of a record
  is a byte of the record, and an adopter who has one declares it as a leading
  item like any other — the extent accounts for it, and every rule in [Physical
  framing](#physical-framing) applies unchanged. A framing member for it would
  carry nothing a copybook cannot (#26).
- **A generated writer's API.** Whether records are written one at a time or in
  a batch, what type a stream has, and how a caller signals that there are no
  more records are the generator's (#52). [Writing a file](#writing-a-file)
  constrains the bytes and the walk, not the call — the same line
  [Names](#names) draws for identifier munging.
- **A transport.** protobuf here is a schema language. There is no gRPC service,
  no server, no port and no lifecycle; the descriptor reaches a plugin as a file
  on disk (#39).
- **The conformance corpus** (#66–#68), which is test infrastructure under
  `testdata/` and documented with the corpus.

## Appendix: A counted run, as nodes

The shape the scope decision was made for, written out: a header carrying a
detail count and a flag, a run of details the count governs, a summary record
the flag governs, and any number of such groups in one file. Two registers —
`count`, an integer; `flag`, bytes. The state names below are this appendix's;
states carry identifiers and no names.

**`start`** — does not accept.

- On a record whose type code is `H`: admit the header, bind `count` from its
  `DTL-COUNT` field and `flag` from its `SUM-FLAG` field, move to `group`.

**`group`** — accepts, guarded by `count` equal to zero and `flag` one of `N` or
a space.

- Guarded by `count` greater than zero. On a record whose type code is `D`:
  admit the detail, take one off `count`, stay in `group`.
- Guarded by `count` equal to zero and `flag` equal to `Y`. On a record whose
  type code is `S`: admit the summary, move to `summarised`.
- Guarded by `count` equal to zero and `flag` one of `N` or a space. On a record
  whose type code is `H`: admit the next header, rebind both registers, stay in
  `group`.

**`summarised`** — accepts.

- On a record whose type code is `H`: admit the next header, rebind both
  registers, move to `group`.

Four things this detects that a memoryless graph could not, and one it does not:

- **A file ending two details short.** End of input in `group` with `count` at
  two: the acceptance guards do not hold, so the file is truncated.
- **A missing summary where the flag says `Y`.** End of input in `group` with
  the flag guard failing — also truncated, and distinguishable from the file
  simply running out mid-run.
- **A sixth detail where the header said five.** In `group` with `count` at
  zero, the detail transition is ineligible and no other predicate matches. It
  was a guard on `count` that excluded the transition that would have matched,
  and that is what the consumer says rather than calling the record undescribed.
- **A summary where the flag says `N`.** The same failure, on `flag`.
- **A trailer count disagreeing with the records read.** Not this. Nothing above
  looks backwards, and nothing needs to; see [A value the automaton has not read
  yet](#a-value-the-automaton-has-not-read-yet).

No transition here is an epsilon, and none is a special case. Every one consumes
exactly one record, the guards on the third do what a chain of epsilons would
have done, and the whole of it stays the same size whether `DTL-COUNT` is a
`PIC 9(2)` or a `PIC 9(9)`.

**The same file with no type codes.** Every transition above is selected by one,
which makes this a poor example of the shape [A transition may carry no
predicate](#a-transition-may-carry-no-predicate) admits. Take the same file with
the type code deleted — header and detail the same length, and nothing in either
saying which it is. `start` keeps its single transition and drops its predicate.
`group` keeps all three and drops all three predicates, and stays legal, because
`count` greater than zero, `count` equal to zero with `flag` equal to `Y`, and
`count` equal to zero with `flag` one of `N` or a space cannot hold in pairs:
the guards alone select, which is what [When two match, and when none
does](#when-two-match-and-when-none-does) permits.

What that costs is the third and fourth detections in the list above. A sixth
detail where the header said five is admitted as the summary or as the next
header, whichever the flag selects, and nothing is reported — the record and the
one the layout expected are the same bytes to a consumer with nothing to test.
The first two survive, because they are end-of-input tests against the
acceptance guards rather than tests on a record. This is [A transition may carry
no predicate](#a-transition-may-carry-no-predicate)'s cost paragraph in one
worked file, and it is why a producer **SHOULD** keep the predicates where the
records offer them.

## Appendix: Mapping to Stories

| Section | Implemented by |
|---|---|
| [Structure](#structure) | #17, #80, #90 `ir`, #38 `resolve` |
| [Offsets and widths](#offsets-and-widths) | #32, #34, #35 `resolve`, #77, #82, #84, #87, #88, #89, #90 `ir` |
| [Physical framing](#physical-framing) | #78, #88, #92, #94 `ir`, #26 `layout`, #52 `gen-go` |
| [The encoding profile, applied](#the-encoding-profile-applied) | #33 `resolve`; an item that carries bytes rather than characters by #275 |
| [Names](#names) | #30 `layout`, #38 `resolve`; what a record node resolved from a `REDEFINES` is called, settled by #164 |
| [The sequencing automaton](#the-sequencing-automaton) | #36 `resolve`, #76, #77, #80, #84, #88 `ir` |
| [Discriminator predicates](#discriminator-predicates) | #28 `layout`, #37 `resolve`, #80, #84, #88, #90, #94 `ir` |
| [Writing a file](#writing-a-file) | #79, #80, #82, #88, #89, #90 `ir`, #51, #52 `gen-go` |
| [Versioning and compatibility](#versioning-and-compatibility) | #17, #18 `ir` |
| [Why protobuf, and why no gRPC](#why-protobuf-and-why-no-grpc) | #17, #19 `ir` |
| [Reading a descriptor without generated code](#reading-a-descriptor-without-generated-code) | #19 `ir`, #57 `container` |
| [A descriptor is readable by a person](#a-descriptor-is-readable-by-a-person) | #21 `ir` |
| The schema itself | #17 `ir` — [`proto/cpybkc/ir/v1/ir.proto`](../../proto/cpybkc/ir/v1/ir.proto) |
| The schema, as Go | #18 `ir` — [`irpb/`](../../irpb), a module of its own |
| The published artifacts | #19 `ir` — [`irpb/descriptor_set.go`](../../irpb/descriptor_set.go) computes the set, [`internal/tools/`](../../internal/tools) writes both files, `.dagger/main.go` builds them and `.github/workflows/release.yaml` attaches them |
| Assembling the IR | #38 `resolve` — [`internal/assemble/`](../../internal/assemble) turns a framing, a compiled automaton and one resolved record type per `record` form into one descriptor, assigning the identifiers by the traversal [Identity, ordering and determinism](#identity-ordering-and-determinism) asks for, and holds the result to a completeness pass before any generator is invoked |
| Emitting the IR | #20, #21 `ir` — [`internal/emit/`](../../internal/emit) encodes a descriptor deterministically, in either form, and writes it where `--emit-ir` names, a path or `-` for standard output |
| Conventions this document follows | #15 `setup` |
