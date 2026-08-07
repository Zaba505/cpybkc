# The Resolved Intermediate Representation

> **Stub.** #16 writes this document. `Scope`, `Governing sources` and `Out of
> Scope` are settled (#15); the headings between them are its outline and are
> empty on purpose. See [CONVENTIONS.md](../CONVENTIONS.md).

## Overview

The IR is a data file's description after every question a copybook and a layout
left open has been answered: offsets computed, encodings applied, sequencing
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
field descriptors with their byte offsets, widths and applied encodings, record
structure, compiled discriminators, the compiled sequencing automaton, and
language-neutral names. Together with the protobuf schema that carries all of
it, the version field that identifies it, and the compatibility policy that
governs changing it.

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

### Conformance language

**MUST**, **MUST NOT**, **SHOULD** and **MAY** are normative requirements on the
IR schema and on every producer and consumer of it, interpreted as described in
[CONVENTIONS.md](../CONVENTIONS.md). Everything else is descriptive.

## Structure

<!-- #16: what a resolved layout is made of, top down — file, records, groups,
     elementary fields — and what each descriptor carries. #17 defines the
     protobuf that expresses it.

     Open, and settled here rather than in #17: whether that structure is a tree
     of nested messages or a flat set of typed nodes with references between
     them — the shape RDF takes, minus RDF's untyped triples. The strongly typed
     form of it in protobuf is one node message whose body is a oneof over the
     member types, so a consumer switches on a closed set the schema already
     enumerates rather than on a string. Nesting is easier to read; a node set
     carries a graph without flattening it, survives a member being added, and
     is the same shape the layout format is being argued into (see
     layout/SPEC.md's Overview). protobuf either way — the question is the
     message shape, not the encoding, and Why protobuf, and why no gRPC below
     is unaffected. -->

## Offsets and widths

<!-- #16: the IR carries pre-computed byte offsets and widths for every field,
     and no generator ever recomputes them. Covers SYNCHRONIZED slack (#34) and
     OCCURS DEPENDING ON (#35) as resolved facts rather than as rules a
     generator applies. Computed by #32.

     Weigh keeping the offset at all against carrying only ordering and width,
     from which it follows. This is where the IR parts company with the layout
     format, which excludes derived quantities outright: the IR is the resolved
     artifact and doing the sum once here is the reason generators exist in more
     than one language. Against that, an offset field is a fact stated twice
     that a producer can get wrong in a way no consumer can detect. If it stays,
     say so as a decision with that cost named, not by omission. -->

## The encoding profile, applied

<!-- #16: every field descriptor arrives with the four cobol-go axes already
     applied, so no generator holds a copy of the profile or re-derives a
     layout from it. Applied by #33. -->

## Names

<!-- #16: language-neutral — the original COBOL name plus an optional override
     (#30). Identifier munging is the generator's responsibility, not the IR's
     (#50). -->

## The sequencing automaton

<!-- #16: sequencing arrives as a compiled automaton, not as a source string a
     generator would have to parse. Compiled by #36. -->

## Discriminator predicates

<!-- #16: discrimination arrives compiled into predicates over field values.
     Compiled by #37. -->

## Versioning and compatibility

<!-- #16: the version field, the compatibility policy, and what constitutes a
     breaking change. Two-sided assertions between generated code and the codec
     it links are #53. -->

## Why protobuf, and why no gRPC

<!-- #16: protobuf is the schema language and nothing more — there is no
     service definition, no server and no port. The plugin contract cites this
     section rather than arguing it again. -->

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
| [Discriminator predicates](#discriminator-predicates) | #37 `resolve` |
| [Versioning and compatibility](#versioning-and-compatibility) | #17, #18 `ir` |
| [Why protobuf, and why no gRPC](#why-protobuf-and-why-no-grpc) | #17, #19 `ir` |
| Emitting the IR | #20, #21 `ir` |
