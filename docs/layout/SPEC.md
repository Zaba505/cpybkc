# The File Layout Format

> **Stub.** #22 writes this document. `Scope`, `Governing sources` and `Out of
> Scope` are settled (#15); the headings between them are its outline and are
> empty on purpose. See [CONVENTIONS.md](../CONVENTIONS.md).

## Overview

A copybook describes a record. It cannot say which records a file contains, in
what order they may appear, how to tell one from another, how the file is framed
on disk, or how any of it is encoded — and every one of those is needed before a
single byte can be read. The layout format is where an adopter states them.

Nearly all of that is a *relationship*. Which record may follow which, which
field decides between two of them, which may repeat and which may not: each is a
statement about how records stand to one another, and together they are a graph
over the file's records. That graph is the part a copybook has no way to hold
and the part a generator cannot work without. The flat per-field data beside it
— a name, a width — a copybook already carries.

Because the format states relationships, it states sizes and stops. An offset is
the sum of the widths before it; a record's length is the sum of its fields'.
Writing either down as well would be a second statement of a fact already made,
kept in step by hand, for the two to disagree about. What is derivable is
derived — by `resolve`, once, into the IR.

It is the source an adopter writes; the [resolved IR](../ir/SPEC.md) is what it
becomes. The split is load-bearing: this document owns how a discriminator or a
sequencing expression is **spelled**, and the IR owns what one **means**. A
question about what happens when two discriminators match is an IR question even
though the discriminators are written here.

The record itself remains the copybook's, and the byte layout of the fields
inside it remains [`cobol-go`'s `codec/SPEC.md`][codec-spec]'s. Field layout
questions belong there; questions about the file around the records belong here.

[codec-spec]: https://github.com/Zaba505/cobol-go/blob/main/codec/SPEC.md

### Scope

In scope: the document an adopter writes to describe a data file, as five
separable layers — the encoding profile, physical framing, record definitions,
discrimination, and sequencing — together with what each layer can express, the
rules that make a layout valid or invalid, the diagnostics an invalid one
produces, and the machine-readable schema that checks all of it.

Five layers rather than one schema because they vary independently: a shop's
encoding profile is a property of its mainframe and is the same across every
file, while framing is a property of the dataset and discrimination is a
property of the application.

The concrete notation those layers are written in is **not** settled here. What
a layout has to be able to say is #15's; which syntax says it is #22's, decided
against [The surface syntax](#the-surface-syntax).

Out of scope, with reasons, in [Out of Scope](#out-of-scope).

### Governing sources

- **z/OS DFSMS record formats** and ***Using Data Sets*** — normative for
  RECFM F/FB/V/VB/VBS/U, for the RDW and BDW that variable and blocked formats
  carry, and for how LRECL and BLKSIZE constrain them. This is the vocabulary
  adopters already have from their own JCL, so the format uses their spelling
  rather than inventing one.
  <https://www.ibm.com/docs/en/zos/3.1.0?topic=sets-record-formats>
  <https://www.ibm.com/docs/en/zos/3.1.0?topic=guide-using-data-sets>
- **`cobol-go`'s `codec/SPEC.md`** — normative for the four axes an encoding
  profile declares (charset, sign convention, byte order, float format) and for
  the rule that none of them has a default. The profile layer is a way of
  writing that structure down, not a second definition of it.
  <https://github.com/Zaba505/cobol-go/blob/main/codec/SPEC.md>
- **`cobol-go`'s root `SPEC.md`** — normative for the copybook syntax a record
  definition binds to, and so for what counts as a resolvable field name.
  <https://github.com/Zaba505/cobol-go/blob/main/SPEC.md>

No source is listed here for the surface syntax, because none has been chosen.
Whichever notation #22 lands on brings its own grammar with it, and that grammar
joins this list in the same change — a syntax whose normative definition is left
unnamed is a syntax every implementation reads slightly differently.

> **Ambiguity:** the IBM documentation describes one implementation of record
> formats, and files written by GnuCOBOL or Micro Focus on Linux do not always
> match it — line-delimited "RECFM=V" being the common divergence. Where they
> differ this document does **not** choose a winner; it records the fork as a
> setting and states who takes which side, which is the policy `codec/SPEC.md`
> already applies to the encoding axes.

### Conformance language

**MUST**, **MUST NOT**, **SHOULD** and **MAY** are normative requirements on the
layout format, on its published schema, and on any implementation reading a
layout file, interpreted as described in [CONVENTIONS.md](../CONVENTIONS.md).
Everything else is descriptive.

## The surface syntax

<!-- #22: the notation, and the argument for it. Two requirements bound the
     choice. First, adopters hold this metadata already — in DB2 catalogs, in
     JCL, in a spreadsheet — and will generate layout files from it rather than
     hand-write them, so whatever the notation is needs a machine-readable
     contract they can target; #23 publishes that schema. Second, the thing
     being written down is a graph over records (see the Overview), so the
     notation is judged on how directly it states an edge — a syntax that can
     only nest forces the graph into key paths and cross-references, and every
     reader then has to reconstruct it.

     Candidates, neither settled: an S-expression form of tagged nodes and
     references, as z5labs/dfcad writes its entity graph over z5labs/sexpr-go
     (https://github.com/z5labs/dfcad, SPEC.md there is the worked example);
     or YAML with a published JSON Schema. Whichever wins, name its grammar in
     Governing sources in the same change. -->

## The encoding profile

<!-- #22: the four cobol-go axes, caller-declared with no default for any of
     them. Every one fails silently when wrong, which is the argument
     codec/SPEC.md makes and this layer inherits. Modelled by #25. -->

## Physical framing

<!-- #22: RECFM F/FB/V/VB/VBS/U, RDW and BDW, LRECL and BLKSIZE, line
     delimiters, carriage control. Modelled by #26. -->

## Record definitions

<!-- #22: binding a record name to a copybook and an 01-level within it.
     Includes fully-qualified renames (#30). Modelled by #27. -->

## Discrimination

<!-- #22: a closed set of strategies for v1 — see Out of Scope for why the set
     is closed. Implemented by #28. -->

## Sequencing

<!-- #22: a regex-like algebra over record names. Parsed by #29, compiled to an
     automaton by #36. -->

## Validation and diagnostics

<!-- #22: what makes a layout invalid, and the cross-file source spans an error
     carries back to the layout and the copybook it came from (#24, #31). -->

## Out of Scope

### A general expression language

Discrimination and sequencing are closed forms. There is no place in a layout
file to write an arbitrary expression, call a function, or embed a script, and
adding one is **not** a planned extension.

Reason: two things break at once. Static analysis goes first — a closed set of
discriminator strategies can be checked for overlap, for exhaustiveness, and for
referring only to fields that exist at the offsets they claim, and an expression
language reduces every one of those checks to "run it and see". Portability goes
second: the resolved IR carries discrimination as a compiled predicate that
every generator, in every language, must evaluate identically, and an expression
language would either need an interpreter in each of them or would make the IR
carry source that only one of them could run. The closed set is what lets the
IR stay data.

### The copybook language

Level numbers, `PIC` clauses, `OCCURS`, `REDEFINES` — the contents of a copybook
are **not specified here**.

Reason: they are `cobol-go`'s, cited above. A layout file binds a record name to
a copybook and an `01`-level inside it and stops there. Restating any of the
copybook language would be a second reading of COBOL for the two repositories to
disagree about, which is the outcome depending on `cobol-go` exists to prevent.

### What the layers mean once resolved

The semantics of a discriminator, the behaviour when two of them match, and what
a sequencing expression accepts are **not specified here**.

Reason: they are [`ir/SPEC.md`](../ir/SPEC.md)'s (#16), which is written first
precisely so that the meanings are settled before the spelling. Splitting the
other way — syntax owning semantics — would make the IR a transcription of
whatever notation happened to be convenient, and would leave a second generator
with nowhere to look up what it is expected to do.

### Also out of scope

- **Byte-level field representation.** `codec/SPEC.md`'s, cited above. The
  encoding profile declares the axes; what the bytes then are is answered there.
- **Derived quantities.** An offset, a record length, a total width — anything
  computable from the sizes and the ordering a layout already states is not
  something a layout file also carries. The reasoning is in the
  [Overview](#overview); the computing is `resolve`'s (#32).
- **Resolution** (#32–#38) — the stage that turns a layout and a copybook into
  IR is an implementation, and its contract is `ir/SPEC.md`.
- **The `cpybkc.json` project manifest** (#40), which says which generators to
  run over which layouts. It is a build-configuration file, not a description of
  a data file, and the two have different lifetimes.
- **Copybook discovery** — where copybook files are found and how include paths
  resolve is a CLI concern, not a property of the layout.

## Appendix: Mapping to Stories

| Section | Implemented by |
|---|---|
| [The surface syntax](#the-surface-syntax) | #22, #23 `layout` |
| [The encoding profile](#the-encoding-profile) | #25 `layout` |
| [Physical framing](#physical-framing) | #26 `layout` |
| [Record definitions](#record-definitions) | #27, #30 `layout` |
| [Discrimination](#discrimination) | #28 `layout` |
| [Sequencing](#sequencing) | #29 `layout` |
| [Validation and diagnostics](#validation-and-diagnostics) | #24, #31 `layout` |
