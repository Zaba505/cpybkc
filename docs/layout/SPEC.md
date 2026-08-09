# The File Layout Format

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

The concrete notation those layers are written in is settled by [The surface
syntax](#the-surface-syntax), which is also where the argument for it is. What a
layout has to be able to say was settled first, and separately (#15).

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
- **`z5labs/sexpr-go`**, *the S-expression grammar underneath this format* —
  normative for the lexis and the parse of a layout file: what is a symbol, a
  number or a string and where one ends, what a comment is, how deep nesting may
  go, and the line and column every diagnostic in this document is built on
  (#22, #24). This document is the layer above that grammar and restates none of
  it; [What this document delegates](#what-this-document-delegates) is the list
  of concerns handed over, and [Tagged forms over
  S-expressions](#tagged-forms-over-s-expressions) is why the notation is this
  one. <https://github.com/z5labs/sexpr-go>

> **Ambiguity:** the IBM documentation describes one implementation of record
> formats, and files written by GnuCOBOL or Micro Focus on Linux do not always
> match it — line-delimited "RECFM=V" being the common divergence, and whether
> an item behind an `OCCURS DEPENDING ON` table slides with the table being the
> one that moves every byte behind it. Where they differ this document does
> **not** choose a winner; it records the fork as a setting and states who takes
> which side, which is the policy `codec/SPEC.md` already applies to the
> encoding axes.
>
> The grammar source overlaps none of the others. It governs how a layout file
> is *read as text*, and the three above govern what the text is about, so there
> is no disagreement between them to resolve.

### Conformance language

**MUST**, **MUST NOT**, **SHOULD** and **MAY** are normative requirements on the
layout format, on its published schema, and on any implementation reading a
layout file, interpreted as described in [CONVENTIONS.md](../CONVENTIONS.md).
Everything else is descriptive.

## The surface syntax

A layout is a set of **tagged forms** written as S-expressions, over
[`z5labs/sexpr-go`](https://github.com/z5labs/sexpr-go)'s grammar. Every
statement a layout makes is a top-level form — a tag naming a relation, followed
by the things it relates — and every reference to something named elsewhere is
written the same way wherever it appears.

```
(record ORDER-HEADER
  (copybook "cpy/orders.cpy" ORDER-HEADER-REC))

(discriminate ORDER-HEADER
  (equals (item ORDER-HEADER OH-REC-TYPE) "10"))
```

Two requirements bound the choice, and both were recorded before any candidate
was weighed. Adopters hold this metadata already — in DB2 catalogs, in JCL, in a
spreadsheet — and will generate layout files from it far more often than they
will hand-write one, so the notation needs a machine-readable contract a
generator can target; #23 publishes it. And what is being written down is a
graph over records, so the notation is judged on how directly it states an
edge: a syntax that can only nest forces the graph into key paths and
cross-references, and every reader then has to reconstruct it.

### Tagged forms over S-expressions

Against the second requirement, a tagged form states an edge as an edge. The tag
is the name of the relation, its arguments are the endpoints, and anything the
edge carries sits in the same form beside them. `(discriminate ORDER-HEADER
(equals (item ORDER-HEADER OH-REC-TYPE) "10"))` is one statement naming a
record, a field inside it and a value, and it is a top-level form rather than
something filed under one of the three. No relation is privileged and none is
made out of nesting, so an edge between two records reads the same as an edge
from a record to a field.

The claim is not that references disappear. `ORDER-HEADER` above is a name
resolved somewhere else, and it has to be — a graph written as text names its
endpoints. What changes is that the *relation* is named too, and named in a
position the published schema can speak about: the schema says, per tag, what
each argument must resolve to, which is what makes a reference checkable rather
than conventional. A key path in a tree-shaped file names an endpoint and leaves
the relation to be inferred from where the key sits.

Against the first requirement, the contract is a set of form declarations
published in the same notation (#23): one tag per form, its arguments, its
children and their arities, and what each reference must name. A generator
targeting it emits the notation it already has an emitter for, and a diagnostic
about the schema and a diagnostic about a layout are the same kind of thing with
the same kind of span on them.

That is a smaller claim than the alternative's and it is honest about what it
costs. An adopter cannot pick a validator off the shelf: cpybkc ships the one
that reads the published schema, and an adopter validating a generated layout
runs cpybkc to do it. The cost is small only because an adopter generating
layouts is already running cpybkc — it is the code generator the layout exists
to feed — and it would not be small for a format whose consumers were anybody
else's.

Three further properties decided the choice between two candidates that both met
the requirements.

**The layers stay separable.** [Scope](#scope) claims five layers that vary
independently, and a notation with a document root makes that claim false: the
layers become branches of one tree, and a shop's encoding profile cannot be a
file of its own without an include mechanism to graft it back on. Here every
statement is top-level and refers by name, so a profile written once is the same
form in every layout that uses it, and which files those forms were read from is
a question nothing in this document has to answer.

**Sequencing is a term, and a term is what this notation is made of.** The one
genuinely tree-shaped thing in a layout is the [sequencing](#sequencing)
expression — a regex-like algebra whose subexpressions nest arbitrarily. `(seq
HEADER (* DETAIL) TRAILER)` is that expression written directly. A notation with
no nesting at all would have to encode it as a chain of nodes with generated
names, which is the reconstruction the second requirement exists to avoid,
arriving from the opposite direction.

**Positions survive.** The grammar's parse carries a line and a column on every
token, which is what #24's positioned AST and #31's cross-file spans are built
out of.
A diagnostic that can point at the sub-form that is wrong, and at the copybook
item it names, is half of what this layer is for; see [Validation and
diagnostics](#validation-and-diagnostics).

[`z5labs/dfcad`](https://github.com/z5labs/dfcad) is the worked precedent — an
entity graph written as tagged forms over the same grammar, with its own
`SPEC.md` and its own registry of declarations in the same notation — and it is
cited as evidence that the shape carries a real graph rather than as a source
anything here is normative against.

### Why not a tree-shaped key/value serialization

YAML, JSON and TOML are excluded, and the exclusion is argued rather than
assumed. Each states containment directly and every other edge as a name
resolved somewhere else: a discriminator becomes a key under a record, or a
record name repeated inside a `discriminators` map, and which of those it is is
a convention the format invents and every reader then has to know. The reader is
reconstructing the graph, and the format cannot say what a key points at because
the key is a position rather than a relation.

[`ir/SPEC.md`](../ir/SPEC.md)'s [A node set, not a
tree](../ir/SPEC.md#a-node-set-not-a-tree) refused nesting on the resolved side
for that reason (#16). A source notation that nested would put the flattening
*between* the layout and the IR rather than removing it, so `resolve` would
become a translation between two shapes instead of a lowering within one — and
the shape a layout is written in would be the one shape in the pipeline nothing
had argued for.

None of the three returns as a candidate elsewhere in this document, including
as the published schema's own notation.

### Why not RDF

An RDF serialization such as Turtle was the strongest alternative, and it loses
on three counts rather than on taste. It meets both requirements on their face:
a triple *is* an edge, and SHACL and ShEx are standard shape languages an
adopter's existing tooling can run.

**An edge with anything on it needs an intermediate node.** RDF's edges are
binary, and nearly every statement in a layout carries more than two endpoints —
a discrimination names a record, a field and a value; a counted sequencing step
names a record and the field holding its count. Each becomes a node standing for
the edge, with a generated name and no referent in the file, and reading the
layout back means resolving those names. That is the reconstruction the second
requirement excludes tree notations for, and it lands on the statements that
carry the graph rather than on the ones that carry the flat data.

**The open-world reading is against every closed set here.** In RDF a predicate
a reader has never heard of is more information, not an error. The IR's set of
node kinds, the framings, the guard tests and the [discrimination
strategies](#discrimination) below are all closed precisely so that a member
nobody has heard of is a failure that can be detected rather than a string that
is silently ignored. `sh:closed` puts closure back, per shape, in the schema —
so closedness becomes a property of a document a validator may or may not have
been run with, rather than a property of the format. That is the wrong place for
it in a format whose whole purpose is to make a silent misreading loud.

**A shape report points at a node, not at a line.** RDF's data model is a set of
triples, and the serialization's positions are deliberately not part of it. A
Turtle parser that carried them would be a nonstandard Turtle parser, and a
SHACL report names the focus node it failed on. The diagnostics this format owes
(#24, #31) are spans into a file an adopter edits, and into the copybook a
failing reference reached; buying a standard validator by giving those up is a
bad trade for this layer.

### What this document delegates

The generic S-expression layer is `sexpr-go`'s and is not restated. Precisely
these concerns are delegated: tokenising and parsing; what is a symbol, a number
or a string and where one ends; string escapes; comment syntax and where a
comment attaches; the bound on nesting depth; and the line and column carried on
every token.

Several constructs are legal S-expressions and are **not** legal in a layout
file. Each is a diagnostic naming the construct and its position:

- **Improper lists.** A dotted pair — `(a b . c)` — never appears.
- **Quote shorthands.** `'x`, `` `x ``, `,x` and `,@x` have no meaning here.
- **`nil`, and the empty list `()`.** Absence is expressed by omitting a child.
  Every form has a tag.
- **Booleans.** No position in this format takes one; see [the `blocks`
  child](#lrecl-and-blksize-describe-the-dataset-not-the-stream) for the place
  a boolean was the obvious spelling and a closed value set was chosen instead.

Rejecting them keeps one spelling per layout. A construct with no meaning that
nevertheless parses is a construct two generators will emit differently, and
there is nothing for a diagnostic to say about it.

### Notation used below

Each form is given as a skeleton and a table of children.

- `<name>` is a placeholder for a value of the stated sort, and `…` after one
  stands for further arguments or children of the same kind.
- **Arity** is `1` (required, exactly one), `0..1` (optional, at most one),
  `1..n` (required, repeatable) or `0..n` (optional, repeatable). Where a
  child's arity depends on another child's value — every such case is in
  [Physical framing](#physical-framing) — the table gives the widest form and
  the prose beside it gives the condition.
- A child a form's table does not list is a diagnostic naming both the child and
  the form it appeared in. Repeating a child whose arity is `1` or `0..1` is a
  diagnostic naming both occurrences.

Every form's arguments before its children are positional, in the order given.

### The top-level forms

A layout is these forms and nothing else. Any other tag at the top level is a
diagnostic naming the tag and its position.

| Form | Arity | Layer |
|---|---|---|
| `encoding` | 1 | encoding profile |
| `encoding-override` | 0..n | encoding profile |
| `framing` | 1 | physical framing |
| `record` | 1..n | record definitions |
| `copybook-reading` | 0..1 | record definitions |
| `rename` | 0..n | record definitions |
| `discriminate` | one per `record` | discrimination |
| `discriminate-variant` | one per variant | discrimination |
| `sequence` | 1 | sequencing |

### An item reference

Every reference into a copybook is written the same way, wherever it appears:

```
(item <record-name> <name> …)
```

The first argument is the name of a `record` form. The names after it are the
path from that record's top-level item down to the item being named, one name
per level, outermost first, ending at the item itself. The top-level item's own
name is not repeated — the `record` form already stated it.

A reference names an item and not an occurrence of one. Where the path passes
through an item that repeats, or ends at one, what the reference means depends
on where it was written, and every position that admits one says so: a rename
reaches every occurrence, and the three positions that expect a *value* out of a
reference forbid it entirely, under
[`ir/SPEC.md`](../ir/SPEC.md#a-reference-names-a-field-not-an-occurrence-of-one)
(#84).

The path is complete rather than qualified in COBOL's `OF`/`IN` sense, which
would have let intermediate levels be skipped. Two reasons, and either decides
it. Duplicate data names are legal COBOL, so a name alone is not an identity —
which is why a rename has to name its target fully in the first place (#30) —
and a partial qualification would put the resolution rules for `OF`/`IN` into
this format, a second reading of COBOL beside `cobol-go`'s for the two to
disagree about. And a complete path has exactly one spelling, so two generators
emitting the same reference emit the same text.

A path naming no item, or naming more than one, is a diagnostic carrying a span
into the layout and a span into the copybook (#31).

### A layout states relationships and sizes, and nothing derived

A layout **MUST NOT** state a byte offset, a record length, a group's width, a
total width, or an occurrence count a copybook already fixes, and the format
gives it nowhere to write one. The reasoning is the [Overview](#overview)'s and
the computing is `resolve`'s (#32); what this subsection adds is that the
prohibition is a property of the notation rather than a rule an adopter is
trusted with. There is no `offset` child on any form and no `length` child on
any form, so a layout that states one is rejected as an unknown child rather
than believed and then contradicted.

`lrecl` and `blksize` are not exceptions to it, and the distinction is worth
stating exactly because it looks like one. Neither is derivable from anything a
layout says: they are numbers on the dataset an adopter has, written in the JCL
that allocated it, and a record type's extent is what this project computes. The
two are stated separately *so that they can be checked against each other* — the
whole value of `lrecl` is that it comes from somewhere else, and a check between
two statements of one fact would be a check that can only ever pass ([`lrecl`
and `blksize` describe the dataset, not the
stream](#lrecl-and-blksize-describe-the-dataset-not-the-stream)).

## The encoding profile

```
(encoding
  (charset <charset>)
  (sign-convention <convention>)
  (byte-order <order>)
  (float-format <format>))
```

| Child | Arity | Value |
|---|---|---|
| `charset` | 1 | A charset `codec/SPEC.md` names, spelled as it spells the code page — `cp037`, `cp500`, `cp1047`, `cp1140` — or `ascii` for the identity charset |
| `sign-convention` | 1 | `ebcdic`, `ascii-zone-37`, `translated-ebcdic`, `realia` |
| `byte-order` | 1 | `big-endian`, `little-endian` |
| `float-format` | 1 | `ieee-754`, `hfp` |

The four axes are `codec/SPEC.md`'s and this layer is a way of writing them
down. Which axis governs which item's bytes — charset does not touch packed
decimal — is answered there and not here. The charset values are not enumerated
in this document either: a code page added to that axis would otherwise have to
be added to this one before an adopter could name it.

The sign conventions are the one place a value is respelled, because
`codec/SPEC.md` names them as Go identifiers and a layout is data. The mapping
is this and nothing else:

| `codec/SPEC.md` | written in a layout |
|---|---|
| `SignEBCDIC` | `ebcdic` |
| `SignASCIIZone37` | `ascii-zone-37` |
| `SignTranslatedEBCDIC` | `translated-ebcdic` |
| `SignRealia` | `realia` |

### All four, always, with no default for any

A layout **MUST** carry exactly one `encoding` form and **MUST** state all four
axes on it. An implementation **MUST NOT** supply a default for a missing axis,
and **MUST** report the missing one by name.

The argument is `codec/SPEC.md`'s and this layer inherits it rather than
restating it: every one of the four fails *silently* when wrong, yielding a
plausible but incorrect value rather than an error, and none is recoverable from
the file with certainty. A default is therefore a guess with no failure mode
attached to it. Requiring the declaration turns an undetectable data-corruption
bug into a question asked while a layout is being read.

The axes are independent and are not a dialect flag. The combination real files
hit most often is one no compiler produces — a mainframe-written file converted
to ASCII, with ASCII characters, translated-EBCDIC signs and big-endian binary —
and it is expressible here only because the four are stated separately.

### An override is per item, and there is no second profile

```
(encoding-override <item-ref>
  <axis> …)
```

`<axis>` is one or more of the four children below, each spelled exactly as it
is on `encoding`. An override naming one axis leaves the other three as the
profile states them; one naming all four replaces the profile entirely for that
item.

| Child | Arity | Value |
|---|---|---|
| `charset` | 0..1 | as above |
| `sign-convention` | 0..1 | as above |
| `byte-order` | 0..1 | as above |
| `float-format` | 0..1 | as above |

An override replaces the profile's value for the item it names, axis by axis,
for that item alone. An override naming no axis at all is a diagnostic; so is a
second override naming the same item, since two would leave the order they were
written in deciding the answer.

The item reference **MAY** name a group, in which case the override reaches
every elementary item under it. It **MAY** name an item that repeats, and
reaches every occurrence — an encoding is a property of an item's bytes wherever
they sit, so the restriction the value-reading positions carry does not apply
here (#84).

Overrides are the whole of the mechanism: there is no second profile to inherit
from, no per-record profile, and no profile named and referred to by name. A
record whose fields disagree about charset is the ordinary case on these files
rather than an exception, and it is expressible with one profile and a handful
of overrides. What the resolved side does with the pair is
[`ir/SPEC.md`](../ir/SPEC.md#the-encoding-profile-applied)'s: resolution applies
the override over the profile and a field node carries the result, with no
profile surviving into the IR for anything to inherit from (#25, #33).

## Physical framing

```
(framing
  (recfm <recfm>)
  <child> …)
```

Which children a `framing` form admits, and which of them it requires, follows
from the `recfm` value: `lrecl` is required under `F` and `FB` and not admitted
under `line-sequential`, `max-segment` is required under `VBS` and nowhere else,
and `delimiter` and `placement` are required under `line-sequential` and nowhere
else. The two subsections below say which applies where and why; the table lists
them all.

| Child | Arity | Value |
|---|---|---|
| `recfm` | 1 | `F`, `FB`, `V`, `VB`, `VBS`, `U`, `line-sequential` |
| `lrecl` | 0..1 | a positive integer |
| `blksize` | 0..1 | a positive integer |
| `blocks` | 0..1 | `deblocked`, `in-stream` |
| `max-segment` | 0..1 | a positive integer |
| `delimiter` | 0..1 | a byte string |
| `placement` | 0..1 | `terminator`, `separator`, `optional-terminator` |

A layout **MUST** carry exactly one `framing` form.

### The adopter's spelling, and what each one resolves to

The values of `recfm` are the RECFM letters an adopter already has from their
own JCL, plus one spelling for the file that has no RECFM at all. They are kept
because they are what the adopter can look up; what a *consumer* does is the
IR's four framings, and `resolve` maps one to the other (#26):

| Written | Resolves to |
|---|---|
| `F`, `FB` | **unframed** |
| `V`, `VB` | **descriptor-word** |
| `VBS` | **segmented** |
| `line-sequential` | **delimited** |
| `U` | nothing; the layout is rejected |

The mapping is
[`ir/SPEC.md`](../ir/SPEC.md#four-framings-and-none-of-them-is-a-recfm)'s (#78),
read from this side. Blocking is absent from the right-hand column
because a blocked dataset is ordinary once it has arrived on a filesystem,
which is [`lrecl` and `blksize` describe the dataset, not the
stream](#lrecl-and-blksize-describe-the-dataset-not-the-stream).

`VBS` is the one spelling whose records may be split. Each segment carries a
segment descriptor word, and putting the segments back together into one run of
bytes is the consumer's before anything else in either document applies, so
nothing in a layout describes a record twice for being spanned. What a layout
adds for it is
[`max-segment`](#lrecl-and-blksize-describe-the-dataset-not-the-stream).

`line-sequential` is how the fork in [Governing sources](#governing-sources) is
recorded as a setting. IBM's V and VB are a record descriptor word in front of
each record; the line-delimited files GnuCOBOL and Micro Focus write, including
the ones their documentation calls RECFM=V, are a delimiter behind each record
and nothing in front. A layout says which of the two it has by which spelling it
writes, and neither vendor is the default.

`U` is admitted as a spelling in order to be rejected by name. A U-format
record's extent came from the physical block the access method read, and a byte
stream on a filesystem has lost it, so there is nothing to read a record's end
from — [`ir/SPEC.md`](../ir/SPEC.md#undefined-length-records) excludes it, and
`resolve` **MUST** reject a layout declaring it, naming the dataset rather than
reporting a generic framing error (#26). A format with no spelling for U would
leave the adopter who has one describing their file as something else, and
finding out at the first record.

A `recfm` value **MAY** carry a trailing `A` or `M` — `FBA`, `VBM` — and a
layout that writes one is rejected, naming the carriage control and saying where
it belongs instead. The control character is a byte of the record: it is
positioned like every other byte, it counts toward LRECL, and something may need
to read it. A framing setting would make it a framing byte, and framing bytes
belong to the dataset rather than to any record — no item covers one, no slack
node accounts for one, and no predicate ever sees one
([`ir/SPEC.md`](../ir/SPEC.md#physical-framing)). Declared as a leading item in
the copybook it is an ordinary item, described exactly once, in the place this
project already describes items.

### `lrecl` and `blksize` describe the dataset, not the stream

`lrecl` is **required** under `F` and `FB` and **MAY** be stated under `V`, `VB`
and `VBS`. It is not admitted under `line-sequential`, where the dataset has no
such number, and a layout stating one there is a diagnostic naming the spelling.

Under `F` and `FB` it is required because it is the only thing standing between
an adopter and a silent misalignment. The next record begins a fixed distance on
whatever the record was, so every record type **MUST** account for all of LRECL
— a record type whose items stop at 72 bytes of an 80-byte record carries the
remaining 8 as slack, and one that does not describes a file whose reader is
wrong from the second record onward with nothing in the file to disagree with it
(#26, #34). `resolve` checks each record type's extent against `lrecl` and
reports the difference it cannot account for.

One class of record type cannot meet that requirement at all and is rejected
rather than padded: a record type whose extent moves with a count has no single
number of bytes to pad, and
[`ir/SPEC.md`](../ir/SPEC.md#a-variable-record-does-not-fit-a-fixed-length-dataset)
refuses it (#92). The diagnostic says which of two shapes the record has,
because the two have different answers: a table with nothing behind it describes
the same bytes under the non-sliding reading, which
[`copybook-reading`](#the-occurs-depending-on-reading-is-one-statement-per-layout)
states; a table with items behind it does not describe them under either
reading, and what is left to the adopter is a record type per count value.

Under `V`, `VB` and `VBS` an `lrecl` is a maximum rather than a requirement, and
`resolve` checks each record type's greatest extent against it. It is optional
there because the record descriptor word already states each record's length, so
a layout without one is still readable; the check is worth having anyway,
because a maximum that the copybooks exceed is a copybook bound to the wrong
dataset.

`blksize` **MAY** be stated under `FB`, `VB` and `VBS` and is a diagnostic
elsewhere. It describes the dataset the file was extracted from, is checked —
under `FB` it **MUST** be a multiple of `lrecl`, and under `VB` and `VBS` it
**MUST** be at least `lrecl` plus the width of a block descriptor word, which
DFSMS defines and this document does not restate — and then reaches no IR node,
because the stream carries no blocks for a size to describe.

`blocks` is the child that says whether it does. Its only accepted value is
`deblocked`, which is also what its absence means; `in-stream` is admitted as a
spelling and rejected, saying that the transfer has to deblock. A stream that
still carries block descriptor words is a dataset image rather than a record
stream, and
[`ir/SPEC.md`](../ir/SPEC.md#block-descriptor-words-in-the-stream) excludes it
(#26). Carrying a child whose one useful value is the one nobody writes looks
like waste, and it is the same trade `U` is admitted under: an adopter whose
transfer preserved the blocks has a file that will be misread from its first
byte, and the choice is between a diagnostic naming the cause and a format in
which there was nothing to say.

`max-segment` is **required** under `VBS` and a diagnostic elsewhere. It is the
largest segment a writer may emit, and it is the one framing number that is not
a check: it reaches the IR's **segmented** framing and a writer obeys it
([`ir/SPEC.md`](../ir/SPEC.md#where-framing-is-consumed-and-where-it-is-emitted),
#78). A reader has no use for it, since every segment states its own length. It
is stated rather than derived because nothing a layout says implies it — a
record type's extent is what the record is, and how finely a writer chops one
into segments is a property of the dataset it is being written to. `blksize`
bounds it on the mainframe and does not determine it here, and `blksize` is
optional besides.

### A delimiter is bytes, and it has a placement

`delimiter` and `placement` are **required** under `line-sequential` and are a
diagnostic under every other spelling. Neither has a default.

A delimiter is written as a [byte string](#literals) and never as a named
character, a code point or a line-ending style:

```
(framing
  (recfm line-sequential)
  (delimiter (bytes "0D0A"))
  (placement terminator))
```

The argument is
[`ir/SPEC.md`](../ir/SPEC.md#a-delimiter-is-bytes-not-a-character)'s (#78) and
it applies to the spelling as much as to the resolved form: naming a character
would not have picked a byte even with a charset to resolve it against. A
mainframe line-delimited file ends its records with `0x15` or `0x25`, and cp037
and cp1047 disagree about which of those is which; the same file moved to Linux
ends them with `0x0A` and one that has been through Windows with `0x0D 0x0A`.
There is no file-level charset here for a named character to be resolved
against, deliberately, and a delimiter is not a field with axes of its own. A
byte string of no bytes is a diagnostic.

`placement` is one of three, and their meanings are
[`ir/SPEC.md`](../ir/SPEC.md#terminator-separator-and-the-last-record)'s
unchanged: `terminator`, a delimiter after every record including the last;
`separator`, a delimiter between two records and none after the last; and
`optional-terminator`, a delimiter after every record except that the file may
end without one. It has no default because the distinction is what makes the end
of a file checkable — a trailing delimiter under `separator` announces a record
that is not there, and a missing one under `terminator` is a truncated file —
and an adopter who guesses gives up a diagnostic without being told.

## Record definitions

```
(record <name>
  (copybook "<path>" <top-level-name>))
```

| Argument or child | Arity | Value |
|---|---|---|
| `<name>` | 1 | a symbol, unique among the layout's records |
| `copybook` | 1 | a path, and the name of the item in it this record is |

A record name is the layout's own identifier for a record type. It **MAY** be
the same symbol as the copybook item's name and does not have to be, and nothing
outside the layout ever sees it as an identity — the IR carries the copybook's
name and any [rename](#a-rename-substitutes-a-name-and-keeps-the-original)
beside it, never this one.

The `copybook` child names a path and a top-level item within it, and both are
required. The top-level item is the copybook's `01`-level of that name. A
copybook holding exactly one `01`-level does not make the second argument
redundant: a copybook that gains a second one later would otherwise change what
every layout naming it means, silently and in a file none of those layouts is
stored beside.

How the path resolves — which directories are searched, what an include path is
— is a CLI concern and not a property of the layout; see [Out of
Scope](#out-of-scope). What the copybook may contain is `cobol-go`'s and is not
restated here.

### Many records may name one copybook, and two may name one item

Two `record` forms **MAY** name the same path, and **MAY** name the same path
and the same top-level item. Only the record name is unique. Several records
over one copybook is the ordinary case — a shop keeps the record types of one
file in one copybook, an `01`-level apiece, and the
[appendix](#appendix-a-layout-end-to-end) is four of them — and nothing in this
document is decided by counting how many records a path is named by.

Two records over one *item* is the less obvious half, and it is admitted for a
file this document already describes: a header and a detail built to the same
`01`-level, told apart by where they sit and by a count
([Discrimination](#three-strategies-and-the-set-is-closed-for-v1)). Those are
two record types in every way this format has for telling record types apart —
each carries its own name, its own `discriminate` form and its own positions in
the sequencing expression — and refusing the spelling would leave that file
describable only by copying the `01`-level under a second name in the copybook,
so that a duplicate existed for the layout's benefit.

What makes it safe is that [an item reference](#an-item-reference) is rooted at
a **record name** and never at a copybook path. `(item ORDER-DETAIL OD-KEY)`
names the item as it is reached through `ORDER-DETAIL`, so two records over one
`01`-level are discriminated, renamed and encoding-overridden independently and
neither reaches the other. [A
rename](#a-rename-substitutes-a-name-and-keeps-the-original)'s at-most-one rule
counts per record for that reason: the item it may name once is the item
reached through the record it names, not the copybook item behind it.

Whether two paths name one copybook is a question about the files they resolve
to rather than about how they are spelled, and the resolving is the CLI's. No
rule here is stated over `copybook` children carrying equal strings.

### Composition is the copybook's, and `COPY` is where it is written

Many real files pair a shared header with a per-type body: every record opens
with the same handful of fields, and what follows them differs by record type.
The two halves commonly live in two copybooks, and this format does **not**
join them. A `record` form carries exactly one `copybook` child, there is no
second one, and a layout **MUST NOT** be read as concatenating anything.

What joins them is COBOL's own `COPY`, in a copybook the adopter writes:

```
01  ORDER-DETAIL-REC.
    COPY STDHDR.
    COPY ORDDTL.
```

which the layout then binds like any other item:

```
(record ORDER-DETAIL (copybook "cpy/order-detail.cpy" ORDER-DETAIL-REC))
```

Three lines, once per record type, in the language the composition is already
written in. Where a shop's composition lives in a program's `FD` rather than in
a copybook — which is where it usually lives, an `FD` being this text under
another heading — those lines are a copy of ones that exist rather than a new
statement about the file.

The format declines to do the joining itself for a reason that is not economy
of vocabulary. **Two `01`-levels laid end to end are not a COBOL construct, so
nothing says what the bytes at the seam are.** A `SYNCHRONIZED` item aligns
against the start of the record containing it. Under composition there is no
record containing it until something invents one, so cpybkc would be deciding
slack at a seam COBOL never defines, for a record no compiler ever laid out
(#34). That is `codec/SPEC.md`'s subject and not this document's ([Out of
Scope](#also-out-of-scope)). Written as `COPY`, the seam is inside one
`01`-level and every byte of it is the compiler's answer, unchanged.

Two smaller costs fall out of the same place. A composed record needs a
top-level item no copybook declares, and
[`ir/SPEC.md`](../ir/SPEC.md#the-node-kinds) gives a record node exactly one
whose name is the original COBOL name spelled as the copybook spells it
([Names](../ir/SPEC.md#names)) — which a synthesized group has not got, and the
record's own layout name is explicitly not it. And [an item
reference](#an-item-reference) omits the top-level item's name because the
`record` form has already stated it; with two of them a path would have to name
which, so the rule would hold for a record naming one copybook and not for a
record naming two, and the [published schema](#the-published-schema) could not
state the difference.

None of this makes the shared header invisible. It is a group inside each
record type rather than something standing outside them, which is the reading
[Discrimination](#three-strategies-and-the-set-is-closed-for-v1) already takes:
a type code in a header every alternative includes is an `equals` on an item
reference, written once per record because the reference is rooted at the
record.

### A copybook that is not there, and an item that is not in it

Both checks need the copybook, so both are `resolve`'s (#32) rather than the
layout reader's, and both are diagnostics this section requires by name.

A `copybook` child whose path names no file `resolve` can read is a diagnostic.
It **MUST** name the path as the layout spells it, and it carries a span into
the layout, at that child. It carries **no** second span: there is no file to
point into, and a span invented for one would point at nothing. Where the path
was looked for is the CLI's to explain, for the same reason the resolving is
the CLI's.

A copybook that reads and does not declare the top-level item the record names
is a diagnostic carrying two spans (#31): one into the layout at the `copybook`
child, and one into the copybook. It **MUST** name the item as the layout
spells it. The second span points at what is *there* — the `01`-levels the
copybook does declare — because an absent item has no position, and the list an
adopter needs in order to fix the record is the list of the ones they could
have meant. A file declaring no `01`-level at all has nothing to point at but
itself, and the span is the file.

Neither is satisfied by reporting that a record failed to resolve, and neither
is satisfied by naming only the layout. They are the two failures that arrive
before any item reference has been looked at, so they are also the two where a
generic message is likeliest to send a reader to the wrong file — which is the
whole of what [Every diagnostic carries a
span](#every-diagnostic-carries-a-span-and-some-carry-two) is protecting.

### A rename substitutes a name, and keeps the original

```
(rename <item-ref> "<name>")
```

A rename gives the item an override name, carried in the IR beside the original
rather than in place of it, so that generated code can still point back at the
copybook it came from
([`ir/SPEC.md`](../ir/SPEC.md#names)). The substitute is a string and is carried
verbatim: it is language-neutral, and no implementation **MAY** apply the casing
or identifier conventions of any language to it — turning a name into an
identifier is a generator's work (#30, #50).

At most one rename **MAY** name a given item; two would leave their order
deciding. The reference **MAY** name a group, and **MAY** name an item that
repeats, in which case the substitute is the name of the item and reaches every
occurrence of it. The substitute is a name and not an occurrence's, for the
reason [an item reference](#an-item-reference) names an item and not an
occurrence of one: a table of a hundred entries has one field there, named once.

### A substitute is a name nothing beside it already answers to

Two renames **MUST NOT** substitute one name for two items under one parent, and
a rename **MUST NOT** substitute a name an item beside its target already
carries. Each is a diagnostic carrying two spans — the substitute, and the
statement it collides with — because either half is a perfectly good rename on
its own, and what an adopter has to decide is which of the two items they meant
to call the name (#30).

The second holds even where that sibling is itself renamed. The original is
carried beside the substitute rather than in place of it
([`ir/SPEC.md`](../ir/SPEC.md#names)), so an item that has been renamed answers
to the name the copybook gave it still, and two items under one parent answering
to one name is the ambiguity a rename exists to remove. Swapping two names is
spelled by renaming both to names nothing under that parent carries.

Which items a target has beside it is the copybook's, so the rule divides where
[Validation and diagnostics](#validation-and-diagnostics) divides everything
else. The layout reader reports the halves the layout decides on its own — two
renames under one parent substituting one name, and a substitute equal to the
name of an item the layout itself references under that parent, an item the
copybook therefore has — and `resolve` reports a substitute colliding with a
name only the copybook knows about (#32).

### The `OCCURS DEPENDING ON` reading is one statement per layout

```
(copybook-reading
  (occurs-depending-on <reading>))
```

| Child | Arity | Value |
|---|---|---|
| `occurs-depending-on` | 1 | `odoslide`, `noodoslide` |

Where a bound copybook item carries an `OCCURS DEPENDING ON`, a layout **MUST**
carry this form, and `resolve` **MUST** reject one that does not, naming the
record and the table (#27, #35). There is no default.

The values are the adopter's own spelling: Micro Focus's directive is
`ODOSLIDE`/`NOODOSLIDE` and GnuCOBOL carries the same switch as the dialect
option `odoslide`, off by default and implied by `-std=ibm`, while IBM
Enterprise COBOL slides unconditionally and has no directive to name. An adopter
looks the setting up in the compiler that wrote the file, and finds it spelled
this way.

It is one statement for the layout rather than one per record because it is a
property of the compiler that wrote the file and not of any record in it. A
layout whose tables slide and whose other tables do not describes a file no
compiler produced.

There is no default because the two readings put every item behind a table in a
different place and nothing in the file disagrees with the wrong one. What each
resolves to is
[`ir/SPEC.md`](../ir/SPEC.md#an-item-after-a-table-slides-and-the-other-reading-is-a-fixed-table)'s
(#87): `odoslide` becomes a repetition whose count is a reference, and
`noodoslide` becomes a fixed table of the copybook's declared maximum with the
count field left an ordinary field beside it. The second is also the escape from
[`lrecl`](#lrecl-and-blksize-describe-the-dataset-not-the-stream)'s rejection of
a variable record on a fixed-length dataset, wherever a table has nothing behind
it.

## Discrimination

```
(discriminate <record-name> <strategy>)
```

Every `record` form **MUST** be named by exactly one `discriminate` form. A
record with none is a diagnostic, and so is a second one naming the same record.
Requiring it of every record is what makes "this record carries nothing to test"
a statement an adopter made rather than a gap in the file.

Discrimination is stated in **two** scopes and the strategies are one closed set
lowering into both. A `discriminate` chooses a record; a
[`discriminate-variant`](#a-discriminator-for-a-redefine-inside-a-table) chooses
an alternative inside one occurrence of a table, where a copybook redefines an
item inside a repeating group (#90). The strategies below are the set for both,
minus the one member the second scope has no use for; what differs between the
scopes is what the item reference **MUST** satisfy, and that difference is
stated where each scope is.

### Three strategies, and the set is closed for v1

| Strategy | Written | What it says |
|---|---|---|
| `equals` | `(equals <item-ref> <literal>)` | the item's value is the literal |
| `one-of` | `(one-of <item-ref> <literal> …)` | the item's value is one of the literals |
| `single-record-type` | `single-record-type` | the record carries nothing a predicate may test |

The set is closed. `equals` and `one-of` lower into the two members of the IR's
predicate set — `bytes-equal` and `bytes-one-of`, settled in
[`ir/SPEC.md`](../ir/SPEC.md#discriminator-predicates) with these strategies
(#28) — and `single-record-type` lowers into the absence of a predicate. A
fourth strategy is therefore a fourth member of a closed set in the IR, which is
a breaking change under
[`ir/SPEC.md`](../ir/SPEC.md#versioning-and-compatibility), *and* a change to
what a layout may say, which advances [the schema
version](#the-version-is-in-the-file-and-it-moves-with-the-format). Both prices
are paid at once, by everything that reads either interface, which is why the
set is settled before the first release rather than grown afterwards.

Both members are decidable by a writer, which is the other bound on what may
join them. A generator emitting records walks the same automaton a reader does
and evaluates a predicate against the record it is about to emit, from that
record's bytes and its own position in the automaton, rather than inverting one
into a value to fill in
([`ir/SPEC.md`](../ir/SPEC.md#a-writer-evaluates-a-predicate-it-never-inverts-one),
#79). Testing a field's bytes against one literal, or against a set of them, is
answerable from bytes the writer already holds. A strategy testing anything the
writer learns *after* the record is written is not, and [the strategies that are
not in the set](#the-strategies-that-are-not-in-the-set-and-where-each-one-went)
is where the one that fails on exactly that is refused.

Closed because a closed set can be *checked*. Overlap between two records that
may appear at the same point, exhaustiveness, and whether the item named exists
at a position a consumer can reach are all decidable over three strategies and
all reduce to "run it and see" over an expression language, which is what [Out
of Scope](#a-general-expression-language) excludes and why.

`single-record-type` is the name
[`ir/SPEC.md`](../ir/SPEC.md#a-transition-may-carry-no-predicate) uses for what
it lowers to — a transition carrying no predicate — and the name comes from its
commonest case, a file that is a run of one record type. It also covers the
record that has a shape but nothing distinguishing in it: a header and a detail
built to the same copybook, told apart by where they sit and by a count. Where
two records carrying it may appear at the same point and nothing separates them,
`resolve` rejects the layout, which is the same overlap check the other two get.

A record whose alternatives *could* be told apart by content **SHOULD** carry
`equals` or `one-of` even where the sequencing alone would select it. A record
admitted by a strategy that tests nothing is a record whose bytes are never
checked against anything, so a file of the wrong records reads as a file of
records — and under `F` and `FB` there is no framing to catch it either
([`ir/SPEC.md`](../ir/SPEC.md#a-transition-may-carry-no-predicate), #80).

Two shapes that look like separate strategies are the same one. A type code at a
fixed offset and a type code in a header copybook every alternative includes are
both `equals` on an item reference, because the shared header is a group inside
each record type rather than something standing outside them.

An `otherwise` or a default arm is deliberately absent. `single-record-type`
does not become one: it is not tried last, it does not catch what the others
miss, and the IR refuses the reading in as many words, because a state offering
two transitions that can both apply is a layout that was rejected before a
consumer saw it.

### The strategies that are not in the set, and where each one went

Five ways of telling record types apart were proposed for this format (#28).
Three of them are above, and the two shapes that name a field — a type code at a
fixed offset, and a type code in a header copybook every alternative includes —
are one strategy for the reason the paragraph above gives. The other two are not
in the set, and neither is an omission to be closed later.

**A record's length is not something a strategy may test**, and there is nowhere
in this format to write one. It is refused in the IR rather than here
([`ir/SPEC.md`](../ir/SPEC.md#a-predicate-always-names-a-field), #80): under
**descriptor-word** and **segmented** a length is in hand when discrimination
runs and it is a framing byte's value, which no predicate ever sees, and under
**unframed** and **delimited** there is no length to see at all, because a
record's end is its extent and the extent follows from the record type a
discriminator has not chosen yet. What refusing costs an adopter is a file whose
record types agree on every byte and differ only in how long they are, and
[`ir/SPEC.md`](../ir/SPEC.md#a-record-told-apart-only-by-its-length) states that
as an exclusion rather than leaving it to be found.

**Position is the sequencing expression's, not a strategy's.** A record type
that is a file's first is written as the first thing
[`sequence`](#sequencing)'s expression admits, and `resolve` compiles a start
state no transition re-enters
([`ir/SPEC.md`](../ir/SPEC.md#a-predicate-always-names-a-field), #36). Nothing
is written to say so and nothing needs to be: a record's position is which state
the automaton is in, which is what an automaton is for, and a strategy spelling
it would be a second statement of what the expression already said.

**Positional *last* is refused outright**, and the reason is the writer's rather
than the reader's. A writer walks the same automaton and evaluates a predicate
against the record it is about to emit
([`ir/SPEC.md`](../ir/SPEC.md#a-writer-evaluates-a-predicate-it-never-inverts-one),
#79) — and *last* is not a property of that record's bytes or of the writer's
position in the automaton. It becomes true only when the caller says there are
no more records, which is after the record has been written; the only way to
decide it at the moment of writing is to hold the record back until the next
call arrives, and buffering a record is exactly the streaming property a
file-level writer is required to keep (#52). A reader could decide it and a
writer could not, so the two would disagree about a file neither of them is
wrong about, and
[`ir/SPEC.md`](../ir/SPEC.md#the-last-record-of-a-stream) refuses the whole
notion at the layer where both are specified. An adopter whose trailer is
distinguishable by content writes `equals` on it; one whose trailer is not is
describing a file whose last record is knowable only by running out of input.

### What the item reference must satisfy

The item **MUST** be contained in the record the strategy is written on, at any
depth. Its reference is therefore rooted at that record's own name, which is the
half of the rule the layout reader checks: a `discriminate` on `ORDER-DETAIL`
whose item reference opens with `ORDER-HEADER` is a diagnostic naming both,
reported without a copybook because nothing in a copybook could make it true.
The other half — that the path names an item of that record at all — needs one
and is `resolve`'s (#31).

It **MUST NOT** repeat and **MUST NOT** sit inside a group that repeats (#84).
And no item ahead of it in the record **MAY** carry a repetition whose count is
a reference: its position **MUST** be constant within the record, and `resolve`
rejects a layout whose discriminator sits behind a variable item, naming the
record, the item and the variable item in front of it (#37, #84). A target must
also lie inside the shortest record its point in the sequence can put in front
of a consumer, which is `resolve`'s too and is
[`ir/SPEC.md`](../ir/SPEC.md#a-predicate-never-reads-past-the-record-in-front-of-it)'s
(#94).

Every rule in this subsection binds a strategy chosen for a *record*, which is
what lowers into a transition's predicate. A strategy chosen for an arm is
bounded differently and in one case oppositely, and [A discriminator for a
redefine inside a table](#a-discriminator-for-a-redefine-inside-a-table) states
which rules it keeps.

The reason is the read loop's order and it is
[`ir/SPEC.md`](../ir/SPEC.md#discriminator-predicates)'s: a discriminator is
evaluated *before* its record has been admitted, so a target whose position
depends on a count obliges a consumer to decode that count out of bytes it has
not identified. It costs a discriminator nothing it was using — the position of
a type code is a property of a record's shape rather than of its data.

### A discriminator for a redefine inside a table

```
(discriminate-variant <item-ref>
  (arm <name> <predicate>)
  (arm <name> <predicate>)
  …)
```

A `REDEFINES` is ordinarily resolved away: each alternative a discriminator can
select becomes its own record type, chosen by its own `discriminate`
([`ir/SPEC.md`](../ir/SPEC.md#members-never-overlap-and-redefines-is-resolved-away)).
That resolution cannot reach a redefine **inside a repeating group**, because
the alternative is chosen once per occurrence rather than once per record, and
ten entries choosing between two alternatives are not two record types
([`ir/SPEC.md`](../ir/SPEC.md#a-variant-is-chosen-once-per-occurrence), #90).
This form is where that choice is stated, and it is the only place a layout says
anything about a redefine at all.

The argument names the **variant**: an item reference to the item the copybook
redefines — the first alternative, the one every `REDEFINES` of it names. Each
`arm` names one alternative, by the name the copybook gives it, and the strategy
that selects it. Which names are alternatives at that position, and that the
group containing them repeats, are the copybook's and are `resolve`'s (#31,
#35); what is written here is the choice.

| Position | Sort | Arity | What it says |
|---|---|---|---|
| variant | item reference | 1 | the redefined item, inside a repeating group |
| `arm` | `(arm <name> <predicate>)` | 2..n | an alternative, and what selects it |

A `<predicate>` is `equals` or `one-of` — the same two strategies, testing the
same [literals](#literals), and no third. `single-record-type` is not among
them and there is no arm carrying nothing: an arm selected by nothing at all is
a default arm, and
[`ir/SPEC.md`](../ir/SPEC.md#a-predicate-on-an-arm-reads-one-occurrence) refuses
one in as many words. An occurrence matching no arm is reported by the
consumer, with a diagnostic distinguishable from a record no transition matched,
and an adopter whose entries carry a code the alternatives do not cover writes
an arm for it.

**An arm's target sits inside the occurrence, which is the mirror of the rule
above and not an exception to it.** The target **MUST** be contained, at any
depth, in the innermost repeating group containing the variant — the entry the
arm is being chosen for — and **MUST NOT** sit inside an arm that does not also
contain the variant, which rules out the variant's own alternatives and a
sibling variant's while admitting an enclosing one, where a copybook redefines a
redefinition. `resolve` rejects either, naming the record, the repeating group,
the variant and the target (#37, #90).

Two of the record-scope restrictions do **not** bind it, and both for their own
reasons. The target **MAY** repeat and **MAY** sit inside a group that repeats:
[`ir/SPEC.md`](../ir/SPEC.md#a-reference-names-a-field-not-an-occurrence-of-one)
refuses a reference carrying no occurrence to be read in, and an arm's predicate
is evaluated in the occurrence being walked, so it carries one (#84, #90). And
its position need not be constant: an arm runs against a record already
admitted, so a target behind an item whose extent moves with a count the record
carries is a target a consumer can already locate.

The layout reader checks the halves that need no copybook, and they are the ones
an adopter gets wrong while holding the copybook open. The variant reference and
every arm's target **MUST** be rooted at the same `record`. Every target
**MUST** stand under the same outermost group as the variant, which is what
"outside the occurrence" looks like from here — a target reaching into a header
elsewhere in the record shares no group with the variant and would select the
same arm in every occurrence, a choice made once per record and so a record's to
make. No target **MAY** descend through the variant or through any of its arms.
No two arms **MAY** name one alternative. And no two arms of one variant **MAY**
name one target and one literal, which is the smallest overlap and the one
decidable from the layout alone. Everything else about overlap is `resolve`'s,
because `"01"` and `(bytes "F0F1")` are the same bytes only once a charset and a
width have been applied.

**Two arms that can both match are refused, and nothing exempts a pair.** Arms
are checked against each other, over the alternatives of one variant, rather
than against the transitions of a state
([`ir/SPEC.md`](../ir/SPEC.md#a-predicate-on-an-arm-reads-one-occurrence)): they
are the choices at one position inside one occurrence, and no record type is
involved. There is no guard on an arm and no way to write one — a guard reads a
register, a register holds one value for the whole of a record's read, and a
value that is the same in every occurrence selects a record rather than an arm —
so the exemption a counted run of records relies on has no counterpart here, and
two overlapping arms are always a diagnostic.

One `discriminate-variant` **MUST** name each variant, and two naming one item
is a diagnostic like a second `discriminate` on one record. A variant that
nothing names is a diagnostic too, and it is `resolve`'s: a redefine inside a
repeating group is a fact about a copybook, and nothing in the layout says one
is there.

```
;; Each entry of a policy carries its own kind and a body redefined two ways.
;; The alternative is chosen once per occurrence, so it is not two record types.
(discriminate-variant (item POLICY PL-ENTRIES PL-BODY-MOTOR)
  (arm PL-BODY-MOTOR    (equals (item POLICY PL-ENTRIES PL-KIND) "M"))
  (arm PL-BODY-PROPERTY (one-of (item POLICY PL-ENTRIES PL-KIND) "P" "H")))
```

It is a form of its own rather than a second spelling of `discriminate` because
the two say different things about different subjects: one names a record and
carries one strategy, this names an item and carries an ordered list of them. A
single form taking either would make the arity rule — one per `record` — a rule
about which of two shapes was written, and the schema could not state even the
half of it that it states today.

### Literals

A literal is one of three, and which one it is decides what it is compared
against:

| Written | Is |
|---|---|
| `"01"` | text, resolved to bytes through the item's own charset and padded to the item's width |
| `12` | a number, resolved to bytes through the item's `PICTURE`, `USAGE` and axes |
| `(bytes "F0F1")` | bytes, taken literally, with no charset and no padding |

All three resolutions are `resolve`'s, and that is the split this document is
built on: a layout says which *value* tells the record apart, and the IR carries
the *bytes* a consumer compares. An adopter writing `"01"` on a `PIC X(2)` field
of an EBCDIC file has said something true about their data; working out that it
is `F0 F1` needs the charset, the width and the padding rule, and every one of
those is COBOL knowledge a generator must never have to hold.

A byte string is there for the field whose value is not text and not a number —
a flag byte carrying a bit pattern — and for the adopter who has a hex dump and
no PICTURE they trust. It is the one spelling that says exactly what is in the
file, and it is the one spelling that is wrong if the file is converted.

A literal wider than the item it is compared against is a diagnostic naming
both. A literal narrower is padded under the first two spellings and is a
diagnostic under the third, because bytes given literally are what the file
holds and there is nothing to pad them with that is not a guess.

## Sequencing

```
(sequence <expression>)
```

A layout **MUST** carry exactly one `sequence` form. Its expression is a
regex-like algebra over record names, and it is the whole of what the layout
says about the order records may appear in.

### The operators

| Written | Means |
|---|---|
| `<record-name>` | one record of that type |
| `(seq <e> …)` | each in turn, left to right |
| `(alt <e> …)` | exactly one of them |
| `(* <e>)` | zero or more |
| `(+ <e>)` | one or more |
| `(? <e>)` | zero or one |
| `(times <e> <item-ref>)` | exactly as many as the named item holds |
| `(when <item-ref> <literal> <e>)` | `<e>` only where the item holds that value |
| `(when <item-ref> (one-of <literal> …) <e>)` | `<e>` only where it holds one of them |

`seq` and `alt` take two or more subexpressions; the others take one, and
`times` and `when` take their reference and literal beside it. The set is closed
for the same reason the discrimination strategies are, and adding to it is the
same kind of change.

Every record name in the expression **MUST** be a `record` the layout defines,
and every `record` the layout defines **MUST** appear in the expression. A
record defined and never sequenced is a record type nothing can ever admit, and
saying so is cheaper than leaving an adopter to find that the file reader never
produces it.

The first record of a file is the first thing the expression admits, and nothing
is written to say so. `resolve` compiles a start state that no transition
re-enters, which is what makes position expressible at all
([`ir/SPEC.md`](../ir/SPEC.md#a-predicate-always-names-a-field), #36, #80).
There is no operator for the *last* record and there will not be one: a
consumer does not know a record is last until the input ends after it, which is
[`ir/SPEC.md`](../ir/SPEC.md#the-last-record-of-a-stream)'s exclusion.

An empty file is `(* <e>)` accepting and `(+ <e>)` not, which is the ordinary
reading of both and is worth stating because an adopter choosing between them is
choosing whether a zero-record extract is a valid file.

### Two operators read a value, and they are the automaton's memory

`times` and `when` are the only operators that read anything out of the file,
and they exist because ordinary files need them: a header carries a count saying
how many of another record type follow, or a flag saying whether a later record
type appears at all. Neither is expressible by a graph with no memory, and both
are in scope (#76).

```
(sequence
  (seq FILE-HEADER
       (* (seq ORDER-HEADER
               (times ORDER-DETAIL (item ORDER-HEADER OH-DETAIL-COUNT))))
       (when (item FILE-HEADER FH-TRAILER-FLAG) "Y" FILE-TRAILER)))
```

What they resolve to is
[`ir/SPEC.md`](../ir/SPEC.md#the-automaton-remembers-in-registers)'s registers,
bindings and guards (#77): the named item's value is bound into a register when
the record holding it is admitted, and the transitions that follow carry guards
reading it. The three forms above are the three guard tests that document
closes, and there is no fourth here because there is no fourth there — `times`
is the register holding an integer greater than zero, and `when` with a literal
and `when` with a `one-of` are the other two.

Both carry the same restrictions on their item reference, and both are
`resolve`'s to enforce:

- The item **MUST** be contained in a record the expression admits **strictly
  earlier**, on every path through the expression that reaches the operator. An
  item in the record being counted, or in one that may or may not have appeared,
  is a value the automaton has not read yet and is rejected naming the operator
  and the record (#36, #37, #88).
- The item **MUST NOT** repeat and **MUST NOT** sit inside a group that repeats
  (#84).
- Under `times` the item **MUST** be one whose value decodes to an integer.

A value that governs only the record it sits in needs neither operator. Two
records built to different copybooks and told apart by a flag are an `alt` of
two record names with a discriminator each, and the dependence on the value
becomes the state the automaton is in. Reaching for `when` there costs every
consumer in every language a register it did not need
([`ir/SPEC.md`](../ir/SPEC.md#when-a-value-becomes-a-state-and-when-it-becomes-a-register)).

### What the algebra deliberately cannot say

There is no arithmetic on a count, no comparison other than the three above, no
conjunction of two conditions on one operator, and no way to name a record's
position in the stream or its length. Each is a step toward the expression
language [Out of Scope](#a-general-expression-language) excludes, and each has a
shape that already works: a conjunction is a nested `when`, a disjunction over
values is `one-of`, and a disjunction over records is `alt`.

Nothing here says what happens when two records at one point cannot be told
apart, or what an automaton does when no transition matches. Those are the IR's
(#16), settled before this notation existed so that the meanings would not be
back-derived from the spelling.

## The published schema

The schema is the machine-readable contract [The surface
syntax](#the-surface-syntax)'s first requirement asks for: the thing a layout
generator targets and a validator checks. It is `layout-schema.sexpr`, an asset
on every release, and it is `schema/layout.sexpr` in this repository (#23).

### It is written in the notation it describes

The schema is a set of **form declarations**, written as tagged forms over
[`z5labs/sexpr-go`](https://github.com/z5labs/sexpr-go)'s grammar — the same
grammar, and the same shape, as the layouts it describes. That is the dialect,
and naming it is not a formality: a schema means nothing without the language it
is written in, and a consumer has to know what to reach for before it can read
one.

Its own vocabulary is nine tags, none of which appears in a layout: `schema`,
`sort`, `reference`, `form`, `in`, `arity`, `layer`, `argument` and `child`. A
`form` declaration states one layout form — where it may appear, how many times,
its positional arguments in order, and the children it admits with their
arities. A `sort` names what may stand in a position. A `reference` is a sort
whose member is a symbol naming an argument of another form, which is how the
schema says what a reference **MUST** name.

Writing it in the notation rather than in a standard schema language is the
trade [Tagged forms over
S-expressions](#tagged-forms-over-s-expressions) argues for and it buys two
things at that price. A generator targeting the schema emits the notation it
already has an emitter for and reads the contract with the parser it already
has. And a diagnostic about the schema and a diagnostic about a layout are the
same kind of thing with the same kind of span on them, because both are
positions into S-expression source.

Arities are spelled `exactly-one`, `at-most-one`, `one-or-more`, `zero-or-more`
and `two-or-more`, for this document's `1`, `0..1`, `1..n`, `0..n` and the two
or more that `seq` and `alt` take. They are symbols rather than the notation
above because `0..1` is not a lexeme the grammar admits — it opens like a number
and is a malformed one.

### The version is in the file, and it moves with the format

The schema carries a single monotonic integer version, stated by the `schema`
declaration it opens with. A consumer **MUST** read it before anything else, and
**MUST** refuse a schema whose version it does not know rather than proceeding
on the declarations it recognises.

It is in the file rather than in its name or its path because a release asset
travels alone: an adopter has the bytes and nothing around them, and a version
written in a path is a version the download loses. It is one integer and not a
major and a minor for the reason
[`ir/SPEC.md`](../ir/SPEC.md#the-version-field) gives for the IR's — the only
question a consumer can act on is whether it understands what is in front of it,
and a minor number exists to let it answer "not entirely, but I will continue
anyway".

The version advances for every change to what a layout may say, and there is no
additive change that leaves it alone. That is a property of the vocabulary
rather than an omission: every set in this format is closed, so a form, a child
or a value a reader has never seen is a diagnostic rather than something it can
ignore. It is the same standing cost
[`ir/SPEC.md`](../ir/SPEC.md#what-breaks-it) accepts for the flat node set,
arriving from the source side, and it is why the sets are settled before the
first release rather than grown afterwards. An edit changing no declaration — a
comment, a reordering — leaves the version alone, because nothing a generator or
a reader can observe has changed.

Version 1 is the format as it first ships, and it stands at 1 while that version
is being assembled. There is nothing to advance *from*: the number exists so
that a consumer holding a schema can refuse one it does not understand, and no
consumer holds a schema that was never released. A change to what a layout may
say, made before the first release, is the settling this section asks for rather
than an exception to it, and it is the standing
[`ir/SPEC.md`](../ir/SPEC.md#the-version-field) gives its own closed sets under
`IR_VERSION_1` (#28).

The version is not this document's. A spec carries no version number
([CONVENTIONS.md](../CONVENTIONS.md)); what is versioned is the interface, and
the schema is where that number lives for this one.

### What the schema does not say

The schema states what a layout is **shaped** like. A layout it accepts is
well-formed, not valid, and four classes of rule are deliberately outside it.

**Conditional arities.** `lrecl` is required under `F` and `FB`, `max-segment`
under `VBS`, `delimiter` and `placement` under `line-sequential`. The schema
declares the widest form, exactly as [Physical framing](#physical-framing)'s
table does, and the conditions are the layout reader's (#24).

**Spellings admitted in order to be rejected by name.** `U` and `(blocks
in-stream)` are members of their sets here, for the reason each is admitted at
all: a format with no spelling for them would leave the adopter who has one
describing their file as something else. What rejects them, with the diagnostic
each is owed, is the reader.

**Rules counting one form against another.** Exactly one `discriminate` per
`record`, one `discriminate-variant` per variant, two arms of one variant that
do not name one alternative, every `record` appearing somewhere in the
sequencing expression, an `encoding-override` naming at least one axis, at most
one `rename` per item and no two of them substituting one name under one parent.
A declaration is about one form and its positions, so a rule relating two of
them is the reader's. The one
cross-form statement the schema does make is the reference, and it is the
important one: a position declared to name a `record` **MUST** name one the
layout defines, which is what makes a reference checkable rather than
conventional.

**Everything a copybook decides**, which is
[Validation and diagnostics](#validation-and-diagnostics)'s second list entire.

None of the four is a gap to be closed later by growing the vocabulary. A schema
carrying a conditional arity would be a second statement of a rule the reader
already enforces, kept in step by hand, for the two to disagree about — and the
disagreement would surface as a layout the published schema accepts and the
reader does not, which is the failure [The S-expression
grammar](#the-s-expression-grammar) refuses one layer down.

## Validation and diagnostics

A layout is checked in two places, and which place a check lands in follows from
what it needs to know.

**The layout reader** (#24) has the layout and nothing else. It reports: a
lexical or grammatical error, from `sexpr-go`; an unknown top-level tag, an
unknown child, a repeated child whose arity forbids it, a missing required child
or form; a value outside a closed set — a `recfm`, a `placement`, a strategy,
an axis value; a duplicate record name; a record with no `discriminate` form or
two; a discriminator whose item reference is rooted at a record other than the
one it discriminates; a second `discriminate-variant` naming one variant; two
arms of one variant naming one alternative, or naming one target and one
literal; an arm whose target is rooted at another record, stands under another
outermost group than the variant, or descends through the variant or one of its
arms; a record name in the sequencing expression that no `record` form defines,
and a `record` the expression never names; a second `rename` naming one item, a
name substituted for two items under one parent, and a substitute equal to the
name of an item the layout itself references under that parent; a `blksize`,
`lrecl`, `max-segment` or `delimiter` under a spelling that does not admit it;
and the framing spellings rejected by name — `U`, a carriage-control suffix,
`(blocks in-stream)`.

**`resolve`** (#32–#38) has the copybooks too, and reports everything that needs
them: a `copybook` child naming a file it cannot read, and one naming a
top-level item the copybook it read does not declare (#27); an item reference
naming no item or more than one; a reference that
repeats or sits inside a repeating group where the position forbids it; a
discriminator behind a variable item; a rename whose substitute is the name of
an item beside its target that the layout itself never references; a literal
wider than the item it is compared against; a `times` or `when` whose item is
not admitted strictly earlier on every path; a record type whose extent does not
account for `lrecl` under `F` or `FB`, or exceeds it under the others; a
variable-extent record type on a fixed-length dataset; a copybook carrying an
`OCCURS DEPENDING ON` with no `copybook-reading` form to read it under; two
records at one point in the sequence whose discriminators can both match; an
arm's target outside the
innermost repeating group containing the variant, or inside an arm that does not
also contain it; two arms of one variant whose predicates can both match one
occurrence; and a redefine inside a repeating group that no
`discriminate-variant` names.

The split is not a policy. A check that needs a copybook cannot run before one
has been read, and a check that does not **SHOULD** run in the reader, so that a
layout with a misspelled tag is reported as a misspelled tag rather than as
whatever it fails to resolve into.

Most of the reader's list is [the published
schema](#the-published-schema)'s rather than a second reading of this document.
An unknown tag, an unknown child, a repeated child, a missing required child, a
value outside a closed set and a reference naming no record are each something a
declaration states, and the reader performs them by reading the schema rather
than by carrying a copy of the tables above. What is left to it is what a
declaration cannot say: the conditional arities, the spellings rejected by name,
and the rules counting one form against another.

### Every diagnostic carries a span, and some carry two

A diagnostic **MUST** name what it found and where, and **MUST NOT** report only
that a layout is invalid. Every one carries a span into the layout source — the
sub-form that is wrong, not the top-level form containing it — which is what
[Positions survive](#tagged-forms-over-s-expressions) buys and what #24's
positioned AST holds.

A diagnostic about something in a copybook carries a **second** span, into the
copybook and the item (#31). An item reference that does not resolve, an item
that repeats where a value was expected, a record type whose extent misses
`lrecl`, a table with items behind it on a fixed-length dataset: in every one of
those the layout and the copybook are both part of the answer, and a diagnostic
naming only one of them leaves the reader to find the other by hand.

Where a rule in this document says a diagnostic **MUST** name a particular
thing — the record and the table, the dataset, the item and the variable item in
front of it — that naming is a requirement on the message and not a suggestion.
Those rules exist because the generic version of the message has been observed
to send a reader looking in the wrong file.

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

`COPY` is part of that language, and it is where a shared header is joined to a
per-type body; [Composition is the
copybook's](#composition-is-the-copybooks-and-copy-is-where-it-is-written) is
why the joining is not a layout form.

`REDEFINES` is part of it too, and the one thing a layout says about a redefine
is which alternative to read — never what a redefine is, where its bytes are, or
which items overlay which. A redefine whose alternatives are whole record types
is told apart by an ordinary `discriminate`; one inside a repeating group is
told apart by
[`discriminate-variant`](#a-discriminator-for-a-redefine-inside-a-table), and
both name items the copybook declared and nothing the copybook did not.

### The S-expression grammar

What a symbol is, where a number ends, what a comment attaches to, how deep
nesting may go: **not specified here**.

Reason: they are [`z5labs/sexpr-go`](https://github.com/z5labs/sexpr-go)'s,
cited above, and [What this document
delegates](#what-this-document-delegates) is the list. A format that restated
its own grammar would be a second definition of one for the library and the
document to disagree about, and the disagreement would surface as a file the
published schema accepts and the reader does not.

### What the layers mean once resolved

The semantics of a discriminator, the behaviour when two of them match, and what
a sequencing expression accepts are **not specified here**.

Reason: they are [`ir/SPEC.md`](../ir/SPEC.md)'s (#16), which is written first
precisely so that the meanings are settled before the spelling. Splitting the
other way — syntax owning semantics — would make the IR a transcription of
whatever notation happened to be convenient, and would leave a second generator
with nowhere to look up what it is expected to do.

### Named encoding bundles

There is no `(encoding ibm-enterprise)` and no other name expanding to a
complete setting of the four axes, though `codec/SPEC.md` offers exactly such
bundles as constructors.

Reason: a bundle in a layout is a name whose expansion lives in another
repository. A layout that resolved to one set of axes before a `cobol-go`
release and another set afterwards would be the silent, undetectable failure the
four axes exist to prevent, and it would happen to a file nobody edited.
`codec/SPEC.md` permits a bundle because a caller writes it in code, in the same
program, against a version it is compiled with; a layout has neither of those
protections. An adopter who wants one writes four lines.

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
- **Layout file discovery and composition** — which files a layout's forms are
  read from, and in what order, is the same CLI concern. The format has no
  include form and needs none: every form is top-level and refers by name, so a
  shop's profile in a file of its own is the same statement wherever it is read.

## Appendix: A layout, end to end

An order file on a variable-length dataset: a file header, then a run of orders
each of which is a header followed by as many details as the header counts, then
a trailer the header says whether to expect.

```
;; The encoding profile — a property of this shop's mainframe, and the same
;; four lines in every layout it writes.
(encoding
  (charset cp037)
  (sign-convention ebcdic)
  (byte-order big-endian)
  (float-format hfp))

;; One field came from a partner in ASCII and was never converted.
(encoding-override (item ORDER-HEADER OH-PARTNER-REF)
  (charset ascii))

;; The dataset — a property of this file, out of the JCL that allocated it.
(framing
  (recfm VB)
  (lrecl 512)
  (blksize 27998))

;; The compiler that wrote it.
(copybook-reading
  (occurs-depending-on odoslide))

;; The record types.
(record FILE-HEADER  (copybook "cpy/orders.cpy" FILE-HEADER-REC))
(record ORDER-HEADER (copybook "cpy/orders.cpy" ORDER-HEADER-REC))
(record ORDER-DETAIL (copybook "cpy/orders.cpy" ORDER-DETAIL-REC))
(record FILE-TRAILER (copybook "cpy/orders.cpy" FILE-TRAILER-REC))

(rename (item ORDER-HEADER OH-KEY OH-CUST-NO) "CustomerNumber")

;; How to tell them apart. Every record carries exactly one of these.
(discriminate FILE-HEADER  (equals (item FILE-HEADER FH-REC-TYPE) "00"))
(discriminate ORDER-HEADER (equals (item ORDER-HEADER OH-REC-TYPE) "10"))
(discriminate ORDER-DETAIL (one-of (item ORDER-DETAIL OD-REC-TYPE) "20" "21"))
(discriminate FILE-TRAILER (equals (item FILE-TRAILER FT-REC-TYPE) "99"))

;; The order they may appear in.
(sequence
  (seq FILE-HEADER
       (* (seq ORDER-HEADER
               (times ORDER-DETAIL (item ORDER-HEADER OH-DETAIL-COUNT))))
       (when (item FILE-HEADER FH-TRAILER-FLAG) "Y" FILE-TRAILER)))
```

Every edge in it is a form. `discriminate` relates a record to an item and a
value; `times` relates a subexpression to the item counting it; `rename` relates
an item to a name; `encoding-override` relates an item to an axis. None of them
is filed under anything, so the profile could be its own file and the
discriminators another, and the layout would say exactly what it says here.

Nothing in it states a size that anything else states. `lrecl` and `blksize`
come from the dataset; every offset, extent and width is the copybook's and is
computed once by `resolve`.

## Appendix: Mapping to Stories

| Section | Implemented by |
|---|---|
| [The surface syntax](#the-surface-syntax) | #22 `layout` |
| [The encoding profile](#the-encoding-profile) | #25 `layout` |
| [Physical framing](#physical-framing) | #26 `layout` |
| [Record definitions](#record-definitions) | #27, #30 `layout`; `copybook-reading` by #35 `resolve` |
| [Discrimination](#discrimination) | #28 `layout` |
| [Sequencing](#sequencing) | #29 `layout`; the expression compiled to an automaton, and the rules on `times` and `when` that need a copybook, by #36 `resolve` |
| [The published schema](#the-published-schema) | #23 `layout` |
| [Validation and diagnostics](#validation-and-diagnostics) | #24, #31 `layout` |
