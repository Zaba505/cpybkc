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
- **z/OS DFSMS record formats** and ***Using Data Sets*** — normative for the
  record and segment descriptor words two of the framings in [Physical
  framing](#physical-framing) name, and so for the bytes a consumer reads and a
  writer emits around a record in those datasets. This document names the
  descriptor word and restates none of its layout.
  [`layout/SPEC.md`](../layout/SPEC.md) cites the same pair for the RECFM
  spelling an adopter writes; it is cited here for what those bytes are (#78).
  <https://www.ibm.com/docs/en/zos/3.1.0?topic=sets-record-formats>
  <https://www.ibm.com/docs/en/zos/3.1.0?topic=guide-using-data-sets>

> **Ambiguity:** these sources do not overlap — protobuf governs the container,
> cobol-go governs what is inside a record, DFSMS governs the descriptor words
> around one — so there is no conflict here to resolve. Where the IR appears to
> disagree with `codec/SPEC.md` about a byte layout, `codec/SPEC.md` wins and
> the IR has a bug.
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
| **file** | The dataset's framing, as one member of the closed set in [Physical framing](#physical-framing) together with whatever that member carries. Plus the identifier of the automaton's start state. Exactly one node of this kind exists, and it is the root. |
| **record** | The identifier of the item that is the record's top level, and the record's names. |
| **group** | An ordered list of the identifiers of its members, its names, and its repetition. |
| **field** | An elementary item: its width, its four resolved encoding axes, its `USAGE`, the attributes that follow from its PICTURE — category, digits, scale, and whether and where a sign is held — its names, and its repetition. |
| **slack** | A width, and nothing else: bytes that are part of the record and belong to no item. |
| **predicate** | The identifier of the field it tests, and the test itself, as one member of a closed set. |
| **state** | Whether the state accepts, the identifiers of the guards qualifying that where it is conditional, and an ordered list of the identifiers of the transitions leaving it. |
| **transition** | The identifiers of the predicate that selects it, the guards that make it eligible, the record it admits, the bindings it applies, and the state it moves to. |
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
- Every reference **MUST** resolve to a node in the same message, of a kind the
  referring position admits. Most positions admit exactly one; a repetition's
  count admits a field or a register (#77).
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

Framing bytes are not slack. A descriptor word or a delimiter belongs to the
dataset rather than to the record's data, and it is described by the file node
([Physical framing](#physical-framing)); positions here are measured from the
first byte of the record's data, whatever stands in front of it.

### A variable record is a sum with a variable term

An item that repeats carries its repetition: a constant count, or — for `OCCURS
DEPENDING ON` — a reference to the count. Its extent is the width of one
occurrence times that count.

That reference **MUST** name either a field node contained in the record being
read, at any depth, or a register node the automaton has bound. It **MUST NOT**
name a field of another record (#35, #77). The withdrawn form was the honest
shape of the problem and not a solution to it: naming a field of a record the
consumer is no longer looking at names bytes it does not have, and no rule about
references gives those bytes back.

That reference **MUST NOT** name a field that repeats, or one contained in a
group that repeats, either (#35, #84). That is the rule [A reference names a
field, not an occurrence of
one](#a-reference-names-a-field-not-an-occurrence-of-one) states for every
position that names a field, and it is what keeps the multiplication above a
multiplication.

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

Where the count is data-dependent, the sum above is data-dependent with it, and
that is the whole of the model. A generator emits an addition it performs while
reading rather than a constant it was handed. A carried offset could not have
described this without a second, different mechanism for every field following a
variable group; ordering and width describe it with none.

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
record and the field rather than reporting a generic reference error (#35, #36,
#37). #76 settled this for bindings; it is the same rule in all three places and
it is stated once here.

The shared reason is that a lone reference against forty entries is a value a
consumer would have to invent, and two consumers invent differently: the first
occurrence, the last, or whichever bytes the offset sum happens to land on.
COBOL's own qualification does not settle it either. `OF`/`IN` names the group
an item belongs to, and it is a subscript that picks an occurrence out of a
table — so a reference resolved the way [Names](#names) requires, by identifier
and never by name, has nothing to take an occurrence from even where the
copybook had one.

The count has a second reason, and it decides the shape of everything above it.
A count that could repeat is a count with one value per occurrence of its
enclosing group, and then the occurrences of that group are not all the same
width. "The width of one occurrence times that count" stops being arithmetic and
becomes a walk over each occurrence in turn, carried out by every consumer in
every language, and the sum that gives every field its position goes with it.
Forbidding a repeating count is exactly the rule that keeps that identity true:
a count is either a field occurring once in the record or a register, and a
register holds one value for the whole of a record's read ([The automaton
remembers, in registers](#the-automaton-remembers-in-registers)), so a group's
occurrences are all the same width whatever is nested inside them.

The predicate has a second reason too, and it is that the target may not be
there at all. A predicate is evaluated before its record is admitted, against
bytes read as the record its transition would admit ([Where framing is consumed,
and where it is
emitted](#where-framing-is-consumed-and-where-it-is-emitted)). A count of zero
is not malformed — only a count that is negative or that does not decode is ([A
variable record is a sum with a variable
term](#a-variable-record-is-a-sum-with-a-variable-term)) — so a target inside
such a group has no bytes at all, and the sum of the widths ahead of it lands on
whatever follows the group instead. That is not an invented occurrence but a
misread of another item's bytes, on a record that is otherwise well-formed, at
the one moment the consumer has not yet decided what it is looking at.

The reading that was refused is worth naming, because it is a real one. A count
sitting beside the table it governs, both inside a group that repeats, means
"the count in this occurrence governs this occurrence" — which is what a
copybook writing them that way says, and it needs no occurrence number, only the
occurrence being read. Admitting it costs the same-width identity above, a
second coordinate on a reference, and an occurrence path threaded through every
consumer's walk of every record; and it would still leave the predicate case
unanswered, since a predicate runs where there is no occurrence being read. An
adopter whose layout is that shape is told so at the record and the field, which
is the trade every exclusion here makes: a named refusal over a silent
misreading.

Settled now rather than left open, because it is not cheaper later in either
direction. Narrowing what a reference may name after the first release is a
breaking change, and widening it is one too — an occurrence number is an
addition a consumer must understand in order to stay correct, which [Versioning
and compatibility](#versioning-and-compatibility) counts as breaking however
protobuf would classify it. What a repetition's count reference admits is
therefore two node kinds and no occurrence, and #17 fixes the schema against
that (#84).

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

**unframed** does not require every record type to have the same extent. The
read below works whether or not they agree, so a rule demanding it would forbid
only files that read correctly. What a fixed-length dataset *does* require is
that its record types account for all of LRECL, since the next record starts at
that fixed distance regardless: a record type whose items stop at 72 bytes of an
80-byte record carries the remaining 8 as slack, and a layout in which it does
not describes a file whose reader misaligns after the first record (#26, #34).
Those 8 bytes are then bytes a writer fills in rather than bytes its caller
supplies ([What the descriptor determines, a writer
supplies](#what-the-descriptor-determines-a-writer-supplies)), which is a
consequence worth knowing before it is met: an adopter who wants them to hold
spaces declares a trailing item and supplies it, rather than leaving them to
slack. Whether slack should carry more than a width is #82's, and fixed-length
padding is the shape that makes that question common rather than rare.

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
admit, and the extent follows from the transition taken. Even a variable record
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
from the other side when it fills slack with zero rather than with a space. The
difference is that slack could take the byte that names nothing, and a delimiter
cannot: it is the byte the file actually holds.

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
for a file that is exactly right. The step is therefore normative, and so is
where it sits.

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
   order the state carries them — against the record's data beginning here.
   Where the framing has already stated this record's length, a consumer
   **MUST NOT** evaluate a predicate against bytes beyond it: a predicate whose
   target lies outside the record the framing bounds does not match, and if no
   other matches either, this is a record the layout does not describe.
4. Take the transition and admit the record. Its extent is known now, because
   which record it is is known now.
5. Check the framing against that extent, as [The extent governs, and framing is
   checked against
   it](#the-extent-governs-and-framing-is-checked-against-it) requires.
6. Consume the framing behind the record — the delimiter, under the
   **terminator** and **optional terminator** placements. Under **optional
   terminator** the input **MAY** be at its end instead, and a consumer **MUST**
   take that as the file ending rather than as a record cut short.
7. Apply the transition's bindings and move to the state it names.

Reaching end of input anywhere from step 2 to step 6 — part-way through a
record's extent, or with a required delimiter unread — is a truncated file, and
a consumer **MUST** report it as one. End of input is tested at step 1 and
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
has to ignore is not worth carrying. Checking a dataset's declared LRECL against
the widths a copybook implies is real work all the same, and it is `resolve`'s,
done while a layout is being read and before an IR exists (#26).

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
record it admits, the state to move to, and — where the automaton has memory —
the guards that make it eligible and the bindings it applies. A consumer reads a
file by consuming the framing in front of a record, evaluating the current
state's transitions in the order given, skipping any whose guards do not all
hold, taking the first of the rest whose predicate matches, emitting the record
that transition names, consuming the framing behind it, applying that
transition's bindings, and moving to the state it names. [Where framing is
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

A transition **MUST** be selected by a predicate and **MUST NOT** be labelled
with a record name (#36). A record is what a transition *produces*, not what
chooses it — a consumer cannot know which record it is looking at until a
predicate has told it.

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
field that repeats, or one contained in a group that repeats — the rule [A
reference names a field, not an occurrence of
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
interfering.

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
the set needs neither. Its membership is settled here rather than deferred the
way the predicate set's is (#22, #28), because a guard reads a value this
document's own machinery put in a register: there is no layout-side strategy
list for it to follow and nothing to wait for.

**A register is read only where it has been written.** A producer **MUST**
ensure that every path from the start state to a guard reading a register, to a
state whose acceptance guards read one, or to a repetition naming one, passes
through a binding of that register first (#36, #37). A consumer **MUST** treat a
read of a register nothing has bound as a malformed descriptor, and **MUST NOT**
supply a zero, an empty byte string, or the value of any other register: an IR
that reached a generator with a register read before it was written is a bug in
`resolve`, and the rule here is the one the [encoding
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

A predicate **MUST** name its target as a field node identifier and **MUST NOT**
name it as a field name. The record being discriminated is the record its
transition admits: a producer **MUST** ensure the target is contained in that
record, at any depth, and **MUST NOT** name a field of any other (#37). Where
those bytes are then follows from [Offsets and widths](#offsets-and-widths) —
the target's position is the sum of the widths ahead of it in that record, and
its extent is the target's own width.

The target **MUST NOT** repeat, and **MUST NOT** be contained in a group that
repeats, under the rule [A reference names a field, not an occurrence of
one](#a-reference-names-a-field-not-an-occurrence-of-one) states for every
position naming a field (#37, #84). Nothing is given up by that, because a
discriminator does not sit in a table. Of the strategies lowering into this set,
the two that name a field both name one in the prefix a record's alternatives
share — a type code at a fixed offset, and a type code in a header copybook
every alternative includes — and neither shape repeats; the three that name no
field at all are #80's question rather than this one's (#28).

A predicate and a guard divide the two things a transition can be selected on,
and neither reaches into the other's half. A predicate **MUST NOT** name a
register, and a guard **MUST NOT** name a field node (#37). A predicate reads
the bytes in front of the consumer; a guard reads what the automaton
[remembers](#the-automaton-remembers-in-registers). Keeping them apart makes
"guards first, then bytes" a shape rather than a rule, and leaves the overlap
test below over predicates that all read the same record.

A guard is not the way a predicate stops naming a field, either. Whether the
predicate set admits a member testing something other than a field's bytes — a
record's length, where it sits in the stream, nothing at all — is #80's
question, and this section leaves it exactly where it was.

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
is a breaking change. What may land is bounded from the writing side as well, by
[A writer evaluates a predicate, it never inverts
one](#a-writer-evaluates-a-predicate-it-never-inverts-one).

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

That check stays decidable because of what a guard is not. A flat conjunction of
three tests over a fixed set of declared registers, with no arithmetic in it
beyond taking one off a counter and no comparison of one register against
another, makes "can these two guard lists hold at once" a question about
literals and zero. [The automaton counts; it does not
compute](#the-automaton-counts-it-does-not-compute) is where that restriction is
stated as a restriction.

A consumer **MAY** therefore stop at the first eligible predicate that matches.
The evaluation order is normative all the same, so that two consumers handed the
same bytes do the same work in the same order and report the same thing when
something is wrong with them.

Where no transition's predicate matches, the input is a record the layout does
not describe. A consumer **MUST** report that rather than skipping ahead to a
transition that matches later or falling through to a default. There is no
default, and a file containing an undescribed record is a file the layout is
wrong about.

Where a transition's predicate *would* have matched and its guards excluded it,
a consumer **SHOULD** report that instead, naming the register the guard tested.
The two failures send an adopter to different places: an undescribed record
means the layout is missing a record type, while a detail arriving after its
counter reached zero means the file and its own header disagree about how many
there are. Both are reported and neither is skipped; only the wording differs,
and it is the wording that saves a day.

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

Byte identity is a stronger claim, and this document does not make it. Bytes no
item covers are not carried through a read, which [What the descriptor
determines, a writer
supplies](#what-the-descriptor-determines-a-writer-supplies) takes up; and
whether a value has exactly one valid encoding is `codec/SPEC.md`'s question
rather than this one's.

### A writer walks the same automaton

A writer begins in the start state the file node names, exactly as a reader
does. Its caller names a record and supplies the values of that record's items.

A writer **MUST** consider only the transitions leaving the current state that
admit the record it was asked to write and whose guards all hold, **MUST**
evaluate their predicates in the order the state carries them, and **MUST** take
the first whose predicate matches the bytes it is about to emit. Evaluation
order is normative here for the reason it is normative for reading: two writers
handed the same records do the same work in the same order. Guards come first
here as they come first there, and why they behave identically in both
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
reason reading needs none: a transition is selected by a predicate and never
labelled with a record name, so the predicates decide there as they decide
everywhere else.

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

The set's membership lands with the strategies that lower into it (#22, #28),
and this is the requirement it lands against. Every member **MUST** be decidable
by a writer against the record it is about to emit, from that record's bytes and
the writer's own position in the automaton, at the moment it emits it. Weaker
than invertibility, stricter than nothing, and it is what a writer can actually
meet.

One shape fails it, and it is worth naming because it is the shape a
discriminator by position takes: a test for the *last* record in a stream. A
reader knows a record is the last one when the input ends; a writer does not
know until its caller says there are no more, which is after that record has
been written, and buffering until then gives up the streaming property #52
requires. Whether such a test belongs to the set at all is #80's — whichever way
that lands, it cannot land as a predicate a writer evaluates.

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

Slack is determined here, because nothing else determines it. A slack node
carries a width and no content ([Slack is a node, not a
rule](#slack-is-a-node-not-a-rule)), and a writer still has to put something in
those bytes: a writer **MUST** emit them as zero bytes. A character fill is the
obvious alternative and cannot be specified — charset is per field ([The
encoding profile, applied](#the-encoding-profile-applied)) and slack is not a
field, so there is no charset to resolve a space against. Zero is the byte that
names none. Carrying the fill on the slack node instead would move the same
invented constant one stage earlier, to where `resolve` has no source for it
either: neither a copybook nor a layout says what an alignment gap contains.

The cost is where a round trip stops being byte-exact, and it is stated rather
than left to be discovered. Bytes no item covers do not survive a read — a
`SYNCHRONIZED` alignment gap, or the tail of a `REDEFINES` alternative that
resolution turned to slack ([Members never overlap, and `REDEFINES` is resolved
away](#members-never-overlap-and-redefines-is-resolved-away)) — so a record read
and written back is byte-identical only where its items cover every byte of it.
#51's round-trip criterion is where somebody meets this. It is a limit of what a
width alone can carry, not a bug in a generator.

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
record already emitted, so a writer **MUST** emit exactly that many occurrences
of the group, and **MUST** report a caller supplying a different number rather
than emitting a record its own reader cannot walk. An `OCCURS DEPENDING ON`
count in the same record is supplied by the writer because the field is sitting
there to be filled; a count in a register was filled two records ago, and what
has to agree with it is the data.

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

### A value the automaton has not read yet

Sequencing **MUST NOT** depend on a value in a record the consumer has not read.
A trailer's record count cannot govern how many records precede it, and a
repetition **MUST NOT** name a register no path binds before it.

Reason: a consumer reads a stream forward, and one that could not decide a
transition until a later record arrived would have to buffer an unbounded
stretch of a file whose whole premise is that it does not fit in memory. Nor is
this a check left to run time: `resolve` proves every read of a register is
preceded, on every path, by a binding of it (#36), so what would have been a
surprise halfway through a hundred-million-record file is a layout rejected
before a byte is read. Checking a trailer's count against the records actually
read is a generator's own business and nothing the automaton needs to carry.

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

## Appendix: Mapping to Stories

| Section | Implemented by |
|---|---|
| [Structure](#structure) | #17 `ir` |
| [Offsets and widths](#offsets-and-widths) | #32, #34, #35 `resolve`, #77, #84 `ir` |
| [Physical framing](#physical-framing) | #78 `ir`, #26 `layout`, #52 `gen-go` |
| [The encoding profile, applied](#the-encoding-profile-applied) | #33 `resolve` |
| [Names](#names) | #30 `layout`, #38 `resolve` |
| [The sequencing automaton](#the-sequencing-automaton) | #36 `resolve`, #76, #77 `ir` |
| [Discriminator predicates](#discriminator-predicates) | #28 `layout`, #37 `resolve`, #84 `ir` |
| [Writing a file](#writing-a-file) | #51, #52 `gen-go` |
| [Versioning and compatibility](#versioning-and-compatibility) | #17, #18 `ir` |
| [Why protobuf, and why no gRPC](#why-protobuf-and-why-no-grpc) | #17, #19 `ir` |
| Emitting the IR | #20, #21 `ir` |
| Conventions this document follows | #15 `setup` |
