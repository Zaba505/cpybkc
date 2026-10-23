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

What a record *is* remains COBOL's. It is described by a copybook, or by a
transcription of a copybook's entries that the layout holds or names — a
[definitions file or a `definitions-block`](#definitions), for the adopter whose
copybooks cannot be committed — and whichever holds it, `cobol-go` decides what
each entry means. This document owns how an entry is **spelled** and nothing
about what it says. The byte layout of the fields inside a record remains
[`cobol-go`'s `codec/SPEC.md`][codec-spec]'s. Field layout questions belong
there; questions about the file around the records belong here.

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

  That document declares **five** settings a reader needs, and the profile
  writes four of them. The fifth is the binary width staircase — how many bytes
  a `COMP` item of *n* digits occupies — and it is missing here on purpose: the
  other four are properties of the *bytes in hand*, which is what an adopter
  holding a file can answer, while the staircase is a property of the *compiler
  that produced the file*, which is a dialect setting. See [The binary width
  staircase is the compiler's, not the
  file's](#the-binary-width-staircase-is-the-compilers-not-the-files).
  <https://github.com/Zaba505/cobol-go/blob/main/codec/SPEC.md>
- **`cobol-go`'s root `SPEC.md`** — normative for the copybook syntax a record
  definition binds to, and so for what counts as a resolvable field name. It is
  normative for an [entry body](#definitions) as well: the entries and clauses
  one transcribes are that document's data-description grammar, and whether an
  entry is admissible is decided by `cobol-go`'s parser rather than by anything
  written here ([cobol-go judges an
  entry](#cobol-go-judges-an-entry-and-nothing-here-says-what-one-means)).
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
>
> An entry body is the one place two lexes meet, and they meet without
> overlapping. `sexpr-go` reads the file first and its rules are applied
> whole; a COBOL word is then the symbol `sexpr-go` read, and a COBOL literal
> is the content of a `sexpr-go` string, with that grammar's escapes already
> undone. Neither grammar is asked to read the other's text. Where the two
> would disagree about one spelling — a COBOL word `sexpr-go` reads as
> something other than a symbol — the word is written in a form that says so,
> and [the leaf rules](#the-leaves-a-level-a-data-name-and-literal-text) are
> the whole of that list.

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

All four are refused everywhere, an [entry body](#definitions) included:
`#t`, `#f`, `nil` and `()` have no meaning in an entry either. What the
Booleans bullet and the [`charset none`
argument](#a-byte-is-not-a-character-and-such-an-item-has-no-charset) against
a form whose only content is its own presence govern is a layout's **own**
forms. An entry body follows the entry rules wherever it is held, inside a
`definitions-block` included, and there a clause COBOL writes with no operand
is a form with no arguments — `(global)` — because that is what the copybook
said ([Presence, and an operand that may be
absent](#presence-and-an-operand-that-may-be-absent)). It is a transcription
of a clause, not a flag the format invented, and it is the one spelling the
clause has.

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
| `definitions-block` | 0..n | record definitions |
| `discriminate` | one per `record` | discrimination |
| `discriminate-variant` | one per variant | discrimination |
| `schedule-variant` | one per variant | discrimination |
| `take-alternative` | one per redefine taking one alternative | discrimination |
| `sequence` | 1 | sequencing |

### An item reference

Every reference into a copybook is written the same way, wherever it appears:

```
(item <record-name> <data-name> …)
```

The first argument is the name of a `record` form. The names after it are the
path from that record's top-level item down to the item being named, one name
per level, outermost first, ending at the item itself. The top-level item's own
name is not repeated — the `record` form already stated it.

Each name in the path is a COBOL name, so it is a
[data-name](#the-leaves-a-level-a-data-name-and-literal-text): a symbol in the
case the copybook wrote it, or `(word "nil")` for the one spelling the grammar
would read as something else (#404). The record name before them is the
layout's own identifier and stays a symbol.

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

The prohibition governs the layout's **own** forms. A `PICTURE` or an `OCCURS`
count in an [entry body](#definitions), in a definitions file or a
`definitions-block` alike, is not a layout restating a copybook: it is the
record's description making its own statement, exactly as the copybook it was
transcribed from made it, and it is the only place that statement is written.
No form in an entry body states an offset, a width or a length either
([cobol-go judges an
entry](#cobol-go-judges-an-entry-and-nothing-here-says-what-one-means)).

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
| `charset` | 1 | A charset `codec/SPEC.md` names, spelled as it spells the code page — `cp037`, `cp500`, `cp1047`, `cp1140` — or `ascii` for the identity charset. Never `none`, which is an item's statement and not a file's |
| `sign-convention` | 1 | `ebcdic`, `ascii-zone-37`, `translated-ebcdic`, `realia` |
| `byte-order` | 1 | `big-endian`, `little-endian` |
| `float-format` | 1 | `ieee-754`, `hfp` |

These four axes are `codec/SPEC.md`'s and this layer is a way of writing them
down. Which axis governs which item's bytes — charset does not touch packed
decimal — is answered there and not here. The charset values are not enumerated
in this document either: a code page added to that axis would otherwise have to
be added to this one before an adopter could name it.

They are four of that document's five. The one it declares and this form does
not carry is the binary width staircase, and [the section
below](#the-binary-width-staircase-is-the-compilers-not-the-files) is why.

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
| `charset` | 0..1 | as above, or `none` — [A byte is not a character, and such an item has no charset](#a-byte-is-not-a-character-and-such-an-item-has-no-charset) |
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

### A byte is not a character, and such an item has no charset

```
(encoding-override <item-ref>
  (charset none))
```

`none` is a fifth value of the `charset` axis and the only value an override
admits that the profile does not. It says that the item's bytes are a payload
rather than characters: a consumer **MUST** read and write them as they stand,
**MUST** apply no translation to them, and **MUST** strip and add no padding
(#275).

A `PIC X` item is routinely used to carry one. A status flag whose vendor manual
gives its values as `01`, `02` and `03` holds those bytes and not those two
digits, and a region identifier documented as holding the hexadecimal value of a
region holds one byte of any of 256. Read through a charset each becomes a
character nobody can print, compare or write into a document, and the
trailing-space rule that makes a fixed-width text field read back as the value
written into it deletes a payload byte of `0x20` outright. Neither loss is
reported and neither is recoverable.

Every axis is declared because it fails silently when it is wrong ([All four,
always, with no default for any](#all-four-always-with-no-default-for-any)), and
this is that failure reached by another road: a converted region byte reads as
some other plausible byte for 236 of the 256 values it may hold, with no error
and no warning. The remedy is the one that rule already prescribes — say so in
the layout — and the axis to say it on is the one that answers how an item's
bytes become characters. For this item the answer is that they do not.

**It is a value on this axis and not a fifth axis.** A fifth axis would have to
be stated on every `encoding` form by the rule above, which turns a property of
one item into a declaration every layout that already exists has to add — and
the thing being stated is *the absence of a claim about one item*, which is not
a shape an axis takes. `codec/SPEC.md` does declare a fifth setting, and it is
not this one: it is the binary width staircase, which is a property of the
compiler rather than of the bytes and is [not written in a layout at
all](#the-binary-width-staircase-is-the-compilers-not-the-files). Nor is a
charset of none a form of its own beside `rename`: `rename` is about names and
this is about bytes, and among a layout's own forms a form whose only content
is its own presence is the boolean this format does not take ([What this
document delegates](#what-this-document-delegates)). A clause in an [entry
body](#definitions) is not one of those forms: it transcribes what a copybook
wrote, and follows the entry rules.

**It is written on an override and never on the profile.** A layout stating
`(charset none)` on its `encoding` form is a diagnostic. A file has a character
set even where an item in it carries no characters — its text items and the
digit zone of its zoned items are read through one — and a profile stating none
would leave every item in the file with no reading rather than exempting the one
item the adopter meant.

**Which items it may reach.** The reference **MAY** name a group, and reaches
every elementary item beneath it, and **MAY** name an item that repeats, and
reaches every occurrence; both are the override rule above and neither is
special here. Charset governs alphanumeric character data, the digit zone of
zoned decimal and the byte values of a separate sign, and nothing else, so
`none` arriving at a packed, binary or floating-point item under a group is
inert there exactly as `cp037` already is. Arriving at a **zoned** item it is a
diagnostic naming that item: charset does govern those bytes, and an item with
digits to read and no charset to read them through has no reading at all.
Arriving at an alphabetic or an edited item it is a diagnostic for the opposite
reason — `PIC A` and `PIC ZZ9.99` describe what the characters are, so an item
declared as either of them is characters by declaration.

What the resolved side carries, and what a generator does with it, is
[`ir/SPEC.md`](../ir/SPEC.md#an-item-with-no-charset-carries-bytes-not-characters)'s.

### The binary width staircase is the compiler's, not the file's

`codec/SPEC.md` declares five settings a reader needs and the `encoding` profile
writes four. The fifth is the width staircase applied to `BINARY`, `COMP`,
`COMP-4` and `COMP-5` items: `PIC S9(2) COMP` is **two** bytes under IBM
Enterprise COBOL and **one** under GnuCOBOL's default, and the widths run
2/4/8/16, 1/2/4/8/16, smallest-that-fits, or always-eight depending on the
compiler and its options.

A layout **MUST NOT** state it, and there is no form for one. The reason is the
line [All four, always, with no default for
any](#all-four-always-with-no-default-for-any) already draws: the four axes are
answerable by an adopter holding the file — what its characters are, how its
signs are spelled, which end its integers start at, which floating-point format
it is in — and each is a property of those bytes. The staircase is not. It is a
property of the program that *wrote* the file, fixed when that program was
compiled, and an adopter reads it off the compiler and its options rather than
off the extract in front of them. Putting a compiler setting on a form that
describes bytes would make the profile two things at once, which is the
distinction [All four, always, with no default for
any](#all-four-always-with-no-default-for-any) draws when it says the axes are
independent and are not a dialect flag.

Where it comes from instead is the **dialect** — the same setting that decides
whether `SYNCHRONIZED` inserts slack and how wide a `USAGE INDEX` item is — and
resolution carries it into the IR as a resolved fact about every field, which is
`ir/SPEC.md`'s [A binary item's width is the staircase, not the
digits](../ir/SPEC.md#a-binary-items-width-is-the-staircase-not-the-digits).
A layout that could state a staircase and a dialect that states a different one
would be two answers to one question with nothing to choose between them, so
this format states none.

**How a layout says which dialect its copybooks were compiled under is not yet
part of this format.** Until it is, an implementation resolves under one it
picks, and it is `ir/SPEC.md` that requires it to *say* which — a descriptor
carries the staircase its widths were computed under, so a consumer never has to
assume one.

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
  <source>
  (alternative <item-ref>) …)
```

| Argument or child | Arity | Value |
|---|---|---|
| `<name>` | 1 | a symbol, unique among the layout's records |
| `<source>` | 1 | where the record's description is held, and the name of the item in it this record is; see [A record's source](#a-records-source-a-copybook-a-definitions-file-or-a-block) |
| `alternative` | 0..n | one alternative of a `REDEFINES` outside a repeating group; see [Which alternative a record is](#which-alternative-a-record-is) |

A record name is the layout's own identifier for a record type. It **MAY** be
the same symbol as the name of the top-level item it binds and does not have to
be, and nothing outside the layout ever sees it as an identity — the IR carries
the top-level item's name, as the record's description spells it, and any
[rename](#a-rename-substitutes-a-name-and-keeps-the-original) beside it, never
this one.

### A record's source: a copybook, a definitions file or a block

A record's **description** is the run of entries its top-level item is declared
in. Every record has exactly one, and the record's source says where it is held.
There are two source forms, and the second has two spellings:

```
(copybook "<path>" <top-level-name>)
(definitions "<path>" <top-level-name>)
(definitions <block-name> <top-level-name>)
```

- **`copybook`** names a copybook by its path, relative to the layout, exactly
  as it always has.
- **`definitions` with text** names a [definitions file](#definitions) by its
  path, relative to the layout:

  ```
  (record DEBIT-POSTING
    (definitions "posting.cpy.sexpr" POSTING-RECORD)
    (alternative (item DEBIT-POSTING PST-DEBIT)))
  ```

- **`definitions` with a symbol** names a
  [`definitions-block`](#the-definitions-block-form) in this layout, by the
  block's name:

  ```
  (record LEDGER-TRAILER (definitions TRAILER LEDGER-TRAILER))
  ```

Which of the two `definitions` means is decided by the argument's kind and by
nothing else: text is a path, a symbol is a block name. A path is never a
symbol and a block name is never text, so neither spelling has a second
reading.

The source is written where the `copybook` child always went. `copybook` is
unchanged — its spelling, its arguments and everything it resolves to — so a
layout written before definitions existed says exactly what it said. Each
record has **exactly one** source, and one layout **MAY** mix copybook, file
and block, record by record, as [Definitions held three
ways](#appendix-definitions-held-three-ways) does.

**The top-level name is required for every source**, and it is spelled as
`copybook`'s is: a [data-name](#the-leaves-a-level-a-data-name-and-literal-text),
naming the `01`-level of that name in the record's description. A description
holding exactly one `01`-level does not make it redundant.

Reason: a description that later gains a second `01`-level would otherwise
change what every record naming it means, silently — in a copybook or a
definitions file none of those layouts is stored beside, and in a block as
much as anywhere, since a block is edited by whoever edits the layout and an
added `01` beside the one a record meant is the same edit there. And a layout
has one spelling for each statement it makes ([What this document
delegates](#what-this-document-delegates)): a record naming its item in one
source and leaving it implied in another would be two spellings of one
binding.

How a path resolves — which directories are searched, what an include path is
— is a CLI concern and not a property of the layout; see [Out of
Scope](#out-of-scope). What a copybook may contain is `cobol-go`'s and is not
restated here, and what a definitions file or a block may contain is
[Definitions](#definitions)'.

### The schema shape of a source

The [published schema](#the-published-schema) declares the source as a sort of
two forms, and `record` takes it as an argument rather than as a child:

```
(sort record-source (form copybook) (form definitions))
```

- `record` takes `(argument source record-source exactly-one)` in place of
  `(child copybook exactly-one)`;
- `definitions`' first argument is of `(sort definitions-origin text
  block-name)`, with `(reference block-name definitions-block name)`, so a
  symbol there **MUST** name a block the layout defines;
- its second argument is a data-name, as `copybook`'s is.

This section leaves [The top-level forms](#the-top-level-forms) alone: a
source is written inside a `record`, never at the top level.
[The version paragraph](#the-version-is-in-the-file-and-it-moves-with-the-format)
lists `definitions` beside `schedule-variant` and `take-alternative`, so the
schema stays at version 1. The declarations land with #402; no schema file
changes here.

### What a record may reach

Five rules bound what a source may name, and together they are why a run never
has to follow a chain of files.

- **Only a definitions file is imported, and only by a record.** There is no
  top-level import form, and no form but `definitions` names a file of entries.

  Reason: each record states its own source where it states its top-level item,
  so the one place a reader looks to learn what a record is describes it whole.
  A layout-wide import would put a second statement about a record's
  description at a distance from the record, and a name the record used would
  then mean whatever the imports in scope declared (the owner's decision on
  discussion #394).

- **A definitions file names no file and holds no block. A block names no file
  and holds no block.** Each is an [entry body](#an-entry-body-and-the-two-places-it-is-held)
  and nothing else.

  Reason: with nothing inside a description able to name another, there are no
  transitive imports, so there are no cycles to detect, no order to read files
  in, and no question of whether two spellings reached along two chains name
  one file. A run reads the layout and then, one step away, the descriptions its
  records name, and stops.

- **Several records naming one path read it once. Several records naming one
  block have it judged once.**

  Reason: a description is one file or one form, and what is in it is one
  statement however many records bind it. Reading it once is what makes every
  fault inside it [reported once](#who-reports-a-fault-in-an-entry-body), at its
  span, rather than once per record that happened to name it.

- **Block names are unique within the layout, and are a name space of their
  own.** A block **MAY** share its symbol with a record or with an item.

  Reason: no position admits both a block name and a record name — a block name
  is admitted only as `definitions`' first argument, and nothing else is admitted
  there — so the two can never be read as each other, and making an adopter
  rename a block `TRAILER` because a record is also called `TRAILER` would buy
  nothing.

- **Block names are scoped to the one layout.** A block **MAY** come before or
  after the records that name it. Definitions shared between layouts live in a
  file.

  Reason: a layout is a set of forms, and no rule in it depends on the order they
  are written in, so a reference to a block ahead of it is as good as one behind
  it. A block is a form of the layout that holds it, so another layout has no way
  to name it, and the format offers none: a file is the shared spelling, with a
  path both layouts can state.

Two spellings follow from those rules and are refused. **Record-local entries**
— `entry` forms written inside a `record`, or a `definitions-block` written
there — are not a source: a description is held in a file or a block and named,
never written in place, so a record has one shape whichever source it uses.
And **a block reached from another layout** is not a reference this format can
write: a symbol in `definitions` names a block in this layout, or it names
nothing and is a fault.

### A block's name, and a block no record names

Each block fault is reported at its span.

- **A block name defined twice** is the layout reader's diagnostic, with two
  spans: at the second definition, and at the first. Either block is a good
  definition on its own, and what the adopter has to decide is which of the two
  the name was meant for.
- **A record naming a block the layout does not define** is the schema's
  reference fault, at the symbol. It lists the blocks the layout does define, as
  every reference fault lists its referents (#402).
- **A block no record names** is an error at its name. A block exists to be a
  record's description, and one nothing binds is a description of a record the
  layout does not have — which is more often a misspelled `definitions` argument
  than a block kept for later, and an error says so where a warning would scroll
  past (the owner's decision on discussion #394). The block's entries are still
  read, checked, judged and built in that run, so every fault inside it is
  reported beside this one rather than after it is first named.

### Three sources, one descriptor

**A record bound to a copybook, the same record bound to the definitions file
`init` converts from that copybook, and the same record bound to a block holding
that file's entries MUST resolve to byte-identical descriptors.** Everything
else in the layout is held equal, and only the source differs.

This is the property the [Definitions](#definitions) chapter exists to give: an
adopter moving a record from a copybook they cannot commit to a definitions file
they can, or from a file into the layout, changes where the entries are written
and nothing a generator is handed. It is checkable rather than hoped for — two
runs, two `--emit-ir` files, one `cmp` — and [Which descriptor is
emitted](../cli/SPEC.md#which-descriptor-is-emitted) cites it as part of what a
descriptor is a function of.

### Many records may name one copybook, and two may name one item

Two `record` forms **MAY** name the same description, and **MAY** name the same
description and the same top-level item — by whichever source holds it: one
copybook, one definitions file or one block. Only the record name is unique.
What holds for a copybook below holds for the other two unchanged, and the
copybook is the case written out because it is the one most layouts have.
Several records
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
a **record name** and never at a copybook path, a file or a block. `(item ORDER-DETAIL OD-KEY)`
names the item as it is reached through `ORDER-DETAIL`, so two records over one
`01`-level are discriminated, renamed and encoding-overridden independently and
neither reaches the other. [A
rename](#a-rename-substitutes-a-name-and-keeps-the-original)'s at-most-one rule
counts per record for that reason: the item it may name once is the item
reached through the record it names, not the item in the description behind
it. A rename is therefore **per record and not per item**, and an
implementation keying one on the description's item alone has one substitute
where this section promises two (#164).

Whether two paths name one copybook or one definitions file is a question about
the files they resolve to rather than about how they are spelled, and the
resolving is the CLI's. No rule here is stated over sources carrying equal
strings. Two records naming one block name one block, since a block name is
the layout's own identifier and the layout holds one block of each name.

### Which alternative a record is

A `REDEFINES` outside a repeating group is resolved away into one record type
per alternative
([`ir/SPEC.md`](../ir/SPEC.md#members-never-overlap-and-redefines-is-resolved-away)),
and a `record` form names an `01`-level rather than an alternative of one. The
`alternative` child is where the layout says which of them the form means:

```
(record PURCHASE
  (copybook "txn.cpy" TXN-REC)
  (alternative (item PURCHASE TXN-PURCHASE)))

(record REFUND
  (copybook "txn.cpy" TXN-REC)
  (alternative (item REFUND TXN-REFUND)))
```

The reference names the alternative as an ordinary [item
reference](#an-item-reference), rooted at the record whose choice it is stating.
It is a reference rather than a bare name for the reason every other position
takes one: duplicate data names are legal COBOL, so a name alone is not an
identity, and a redefine may sit at any depth under the `01`-level.

A `record` form **MUST** carry exactly one `alternative` child per `REDEFINES`
the copybook writes into its `01`-level outside a repeating group, and **MUST
NOT** carry one where the copybook writes none. Two independent redefines are
two children, in either order — each names a distinct run of bytes, so nothing
depends on which is written first — and together they select exactly one of the
combinations [`ir/SPEC.md`](../ir/SPEC.md#members-never-overlap-and-redefines-is-resolved-away)
enumerates. How many a copybook writes, and whether a reference names an
alternative at all, are `resolve`'s: both need the copybook.

**Narrowing is this child's job, and `take-alternative`'s, wherever the entries
are held.** A record whose description is an [entry body](#definitions) rather
than a copybook says which alternative it is in exactly this way, and a table's
redefine is still settled by
[`take-alternative`](#every-occurrence-of-a-table-takes-one-alternative) or one
of the two variant forms. An entry body is a transcription of every entry its
copybook declares, the base of a redefined run included, and it is never edited
to narrow: deleting the alternatives a file does not carry from a definitions
file would make it a description of a record no copybook declares, and the
statement that only one alternative is ever in the data would then be written
nowhere a reader looks for it.

**The pairing is stated and never inferred.** Matching a record form to the
alternative containing its discriminator's target is a rule that works on the
shape a discriminator reaches into and answers nothing for an alternative no
`discriminate` form touches, and pairing by the order the forms are written in
is a rule an adopter could not read anywhere. Either would be a program's
convention deciding which bytes a record type describes, silently, and changing
its answer when a copybook gains a redefine in a file no layout is stored beside.

A redefine **inside** a repeating group is not this. The alternative there is
chosen once per occurrence rather than once per record, so it is a
[`discriminate-variant`](#a-discriminator-for-a-redefine-inside-a-table) and
never an `alternative` child; a layout naming one here is naming an item that is
not a record-level alternative, and is a diagnostic on those terms.

### Composition is the copybook's, and `COPY` is where it is written

Many real files pair a shared header with a per-type body: every record opens
with the same handful of fields, and what follows them differs by record type.
The two halves commonly live in two copybooks, and this format does **not**
join them. A `record` form carries exactly one source, there is no second one,
and a layout **MUST NOT** be read as concatenating anything. That holds for
every source alike: the record's description is one copybook, one definitions
file or one block, and its top-level item is one `01`-level in it.

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

A record composed with `COPY` and held in a definitions file or a block is held
as its **expanded** entries: the `01`-level and every entry the members it
copies contribute, in the order the expansion puts them, as one entry body.
`COPY` is a statement in a copybook and not an entry, so there is no form for
it in an entry body, and a block cannot concatenate — it is one `01`-level's
entries, exactly as a file is. Where a `COPY` member is found when the copybook
is read is #407's.

The format declines to do the joining itself for a reason that is not economy
of vocabulary. **Two `01`-levels laid end to end are not a COBOL construct, so
nothing says what the bytes at the seam are.** A `SYNCHRONIZED` item aligns
against the start of the record containing it. Under composition there is no
record containing it until something invents one, so cpybkc would be deciding
slack at a seam COBOL never defines, for a record no compiler ever laid out
(#34). That is `codec/SPEC.md`'s subject and not this document's ([Out of
Scope](#also-out-of-scope)). Written as `COPY`, the seam is inside one
`01`-level and every byte of it is the compiler's answer, unchanged — and
expanded into a file or a block, it is still inside that one `01`-level.

Two smaller costs fall out of the same place. A composed record needs a
top-level item no description declares, and
[`ir/SPEC.md`](../ir/SPEC.md#the-node-kinds) gives a record node exactly one
whose name is the original COBOL name spelled as the copybook spells it
([Names](../ir/SPEC.md#names)) — which a synthesized group has not got, and the
record's own layout name is explicitly not it. And [an item
reference](#an-item-reference) omits the top-level item's name because the
`record` form has already stated it; with two of them a path would have to name
which, so the rule would hold for a record naming one description and not for a
record naming two, and the [published schema](#the-published-schema) could not
state the difference.

None of this makes the shared header invisible. It is a group inside each
record type rather than something standing outside them, which is the reading
[Discrimination](#three-strategies-and-the-set-is-closed-for-v1) already takes:
a type code in a header every alternative includes is an `equals` on an item
reference, written once per record because the reference is rooted at the
record.

### A copybook that is not there, and an item that is not in it

Both checks need the record's description, so both are `resolve`'s (#32) rather
than the layout reader's, and both are diagnostics this section requires by
name. They take one shape for all three sources, so that a record moved from a
copybook to a file or a block fails the same way it failed before.

A source whose path names no file `resolve` can read is a diagnostic. It
**MUST** name the path as the layout spells it, and it carries a span into the
layout, at the source. It carries **no** second span: there is no file to
point into, and a span invented for one would point at nothing. Where the path
was looked for is the CLI's to explain, for the same reason the resolving is
the CLI's. For a copybook the message is `no copybook at cpy/orders.cpy`, and
for a definitions file it is

```
no definitions file at posting.cpy.sexpr
  looked for /srv/ledger/posting.cpy.sexpr
```

with the absolute path on a continuation line, as [Finding the
inputs](../cli/SPEC.md#finding-the-inputs) states for both. A block opens
nothing, so the missing-file diagnostic never applies to one: a block name the
layout does not define is the schema's reference fault ([A block's
name](#a-blocks-name-and-a-block-no-record-names)).

A description that reads and does not declare the top-level item the record
names is a diagnostic carrying two spans (#31), in one shape whichever source
holds it:

```
the <copybook|definitions file|definitions block> <name> declares no <item>
```

`<name>` is the path as the layout spells it, for a copybook or a file, and the
block's name for a block; `<item>` is the item as the layout spells it, which
the diagnostic **MUST** name. The first span is into the layout, at the source.
The second is at the description's first top-level entry and lists the names it
does declare — the `01`-levels there — because an absent item has no position,
and the list an adopter needs in order to fix the record is the list of the ones
they could have meant. This is the copybook's shape as cpybkc already reports it
(`internal/diag/copybook.go`), carried to the other two sources unchanged. A
copybook declaring no `01`-level at all has nothing to point at but itself, and
the span is the file. A definitions file or a block always holds at least one
entry ([An entry body](#an-entry-body-and-the-two-places-it-is-held)), so the
second span always has an entry to point at.

Neither is satisfied by reporting that a record failed to resolve, and neither
is satisfied by naming only the layout. They are the two failures that arrive
before any item reference has been looked at, so they are also the two where a
generic message is likeliest to send a reader to the wrong file — which is the
whole of what [Every diagnostic carries a
span](#every-diagnostic-carries-a-span-and-some-carry-two) is protecting.

### A rename substitutes a name, and keeps the original

```
(rename <item-ref> "<name>")
(rename <record-name> "<name>")
```

A rename gives its target an override name, carried in the IR beside the
original rather than in place of it, so that generated code can still point back
at the copybook it came from
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

A rename is **per record**, not per copybook item. Its target is the item as it
is reached through the record the reference is rooted at, so a rename written
under one of two records over one `01`-level does not reach the other — which is
[Many records may name one
copybook](#many-records-may-name-one-copybook-and-two-may-name-one-item)'s
independence, and is a requirement on an implementation rather than a
consequence of how it happens to hold its copybook items (#164).

### A rename may name a record

The second spelling takes a **record name** where the first takes a reference,
and it substitutes a name for the record type's own — the name the IR carries on
a record node, which is the one the `01`-level in its description gives it,
whichever source holds that description
([`ir/SPEC.md`](../ir/SPEC.md#names)):

```
(record PURCHASE (copybook "txn.cpy" TXN-REC) (alternative (item PURCHASE TXN-PURCHASE)))
(record REFUND   (copybook "txn.cpy" TXN-REC) (alternative (item REFUND   TXN-REFUND)))

(rename PURCHASE "TXN-PURCHASE-REC")
(rename REFUND   "TXN-REFUND-REC")
```

It is a record name rather than `(item PURCHASE)` because [an item
reference](#an-item-reference) does not repeat the top-level item's own name —
the `record` form has already stated it — so the one item whose name a record
node carries is the one item a reference cannot reach. Admitting a reference
naming a record and no item below it would put two meanings on a spelling every
other position in this format refuses.

It is the same form as the first spelling, and not a new one, because it says
exactly what the first says: a substitute carried beside an original that
survives. A record's own name is a name like any other, and the alternative was
a second form differing from `rename` in nothing but what stands in its first
position.

The rules carry over unchanged in the shape they can. At most one rename **MAY**
name a given record. Two renames **MUST NOT** substitute one name for two
records, and a rename **MUST NOT** substitute the name of the top-level item
another record binds, by whichever source — the name a `copybook` or a
`definitions` form spells as its second argument. Both are decidable from the
layout alone, and
both the same ambiguity [A substitute is a name nothing beside it already
answers to](#a-substitute-is-a-name-nothing-beside-it-already-answers-to)
removes between siblings. There is no parent for a record node to have siblings
under, so no rule beyond those two is stated.

**Nothing requires a record to be renamed**, including two records over one
`01`-level, which carry one name between them until a rename says otherwise
([`ir/SPEC.md`](../ir/SPEC.md#names)). Whether one name on two record types is a
problem is a property of the target language: a generator munging both into one
identifier refuses and says which two collided (#50), and a generator that
qualifies its output has nothing to refuse. Requiring the rename here would put
one language's collision rule in the layer every language's generator reads.

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

## Definitions

An adopter whose vendor's copybooks cannot be committed still needs checked-in
text that says everything those copybooks said, or regeneration depends on files
no checked-in file records (discussion #394). This chapter is that text's
notation. It is one production, an **entry body**, which may be held in a file
of its own or inside the layout, and which transcribes `cobol-go`'s parse of a
copybook entry for entry. What each entry *means* stays `cobol-go`'s; what is
settled here is how one is spelled, so that cpybkc and any second implementation
read the same text the same way (#395, #394).

### An entry body, and the two places it is held

An entry body is one top-level `entry` form per `cobol-go`
`DataDescriptionEntry`, in source order, and nothing else.

| Form | Arity | What it is |
|---|---|---|
| `entry` | 1..n | one data-description entry, as [An entry, and one form per clause](#an-entry-and-one-form-per-clause) spells it |

An entry body is held in exactly one of two places:

- **a definitions file**, a `.sexpr` document whose top-level forms are exactly
  one entry body;
- **a [`definitions-block`](#the-definitions-block-form)** in a layout,
  `(definitions-block <name> <entry> …)`, whose body is exactly what a
  definitions file holds, token for token.

A block holds no nested block, no path and no header. A definitions file holds
no block, and no header either: it is the entries and nothing beside them, so a
file's text and a block's body are interchangeable by moving lines, and a
reader of one is a reader of the other. Which of the two a record's description
is held in is the record's to say, by its [source](#a-records-source-a-copybook-a-definitions-file-or-a-block):
`(definitions "<path>" …)` for a file and `(definitions <block-name> …)` for a
block.

**Entries are flat.** An entry body is a list, never a tree: the hierarchy of a
record comes from its level numbers, which `copybook.Build` folds into groups
exactly as it folds a copybook's, and indentation means nothing to a reader.
Nesting the forms would have been a second statement of the hierarchy beside the
level numbers, for the two to disagree about — and a copybook states it once, in
the numbers. The [printed form](#the-printed-form-init-writes) indents for the
eye and for nothing else.

### The `definitions-block` form

```
(definitions-block <name>
  <entry> …)
```

| Argument | Arity | Value |
|---|---|---|
| `<name>` | 1 | a symbol, the layout's own identifier for the block |
| `<entry>` | 1..n | an `entry` form; the block's body is an [entry body](#an-entry-body-and-the-two-places-it-is-held) |

A `definitions-block` is a top-level form, `0..n` of them per layout, in the
record-definitions layer. Its name is a symbol and is always one: it is the
layout's own identifier, like a record's, and never a COBOL name, so it is never
written `(word …)`.

The tag is `definitions-block` rather than `definitions` because `definitions`
is the [record source](#a-records-source-a-copybook-a-definitions-file-or-a-block),
and a diagnostic that names a form has to name one thing. A record names a block
by giving its name as `definitions`' first argument; block names are a name
space of their own, scoped to the layout, and a block no record names is an
error ([What a record may reach](#what-a-record-may-reach), [A block's name, and
a block no record names](#a-blocks-name-and-a-block-no-record-names)).

It is a row of [The top-level forms](#the-top-level-forms), and was added there
with its declaration in `schema/layout.sexpr`, in #400, because a test holds that
table and the schema equal. A run checks a block against the schema and refuses
it at its tag, saying definitions blocks are not resolved by this build, until
#409 resolves a record held in one; it opens nothing for it.

### How a definitions file and a layout are told apart

A layout and a definitions file are both `.sexpr` documents, and neither
carries a header form saying which it is. Nothing inside one needs to.

**A file's kind is set by how it is reached.** A manifest's `layout` names a
layout, and a text `definitions` argument names a definitions file. A run never
opens a `.sexpr` document it has not been told the kind of, so it never has to
guess one. A block is not a third kind of file: it is a form of its layout, read
with the layout and opened by nothing.

**The top-level vocabularies stay disjoint**, so a file reached the wrong way
is recognisable as the other kind from its first form:

- a definitions file's top level is `entry` and nothing else;
- a layout's top level never admits `entry` — an entry appears in a layout only
  inside a `definitions-block`;
- `definitions-block` never appears in a definitions file.

Reason: a header would be a second statement of a fact the route already
fixes, for the two to disagree about, and it would be one more line `init`
writes and an adopter moving entries into a block has to delete. Disjoint
vocabularies give the same certainty for free, and they are what lets a file's
text and a block's body be interchangeable by moving lines.

What a run reports when the route and the content disagree names what was
found, not merely what was expected:

- **A layout whose top-level forms are one or more `entry` forms and nothing
  else** — a definitions file named as the manifest's layout. The run says so, with a span at the manifest's
  `layout` and a span at the file's first entry, and reports nothing else about
  that file: a hundred unknown-top-level-form diagnostics, one per entry, would
  describe the wrong file accurately and the mistake not at all.
- **`entry` forms among a layout's other forms.** The schema check reports one
  diagnostic per contiguous run of them, at its first entry, saying that entries
  go inside a `definitions-block` or in a definitions file a record names. One
  per run rather than one per entry, because a run of entries pasted into a
  layout is one mistake.
- **A `definitions-block` inside a record** — a record-local block. The schema's
  diagnostic there, a form the position does not admit, with a hint that a block
  is top-level and a record names it.
- **A file reached through `definitions "<path>"` whose top level holds no
  `entry`** — a layout named as a definitions file. The run says so, with a span
  at the record's `definitions` source in the naming layout and a span at the
  named file's first form.
- **A definitions file holding a `definitions-block`, or any other form that is
  not `entry`.** A diagnostic at that form, in the definitions file, with a
  second span at the naming record's `definitions` source — the file is a
  description and holds entries only, and the second span is how the reader
  finds which record's description it was being read as.

### An entry, and one form per clause

```
(entry <level> <name> <clause> …)
```

| Argument | Arity | Value |
|---|---|---|
| `<level>` | 1 | the level number, a [number](#the-leaves-a-level-a-data-name-and-literal-text) |
| `<name>` | 1 | `FILLER`, or the entry's [data-name](#the-leaves-a-level-a-data-name-and-literal-text); see [What is written follows from what cobol-go reads](#what-is-written-follows-from-what-cobol-go-reads) |
| `<clause>` | 0..n | one form per clause the entry carries, in source order |

Every production of `cobol-go`'s data-description grammar at the commit cpybkc
pins has exactly one spelling: one form per clause kind, twelve of them.

| `cobol-go` | COBOL | Written |
|---|---|---|
| `RedefinesClause` | `REDEFINES` | `(redefines PST-DEBIT)` |
| `PictureClause` | `PICTURE`, `PIC` | `(pic "S9(7)V99")` |
| `UsageClause` | `USAGE`, or a usage written alone | `(usage COMP-3)` |
| `ValueClause` | `VALUE`, `VALUES` | `(value "'A'" (through "'C'" "'F'") SPACE)` |
| `OccursClause` | `OCCURS` | `(occurs "0" (to "50") (depending-on PST-COUNT) (key ASCENDING PST-KEY) (indexed-by PST-IX))` |
| `SignClause` | `SIGN` | `(sign LEADING)`, `(sign TRAILING SEPARATE)` |
| `SynchronizedClause` | `SYNCHRONIZED`, `SYNC` | `(synchronized)`, `(synchronized LEFT)` |
| `JustifiedClause` | `JUSTIFIED`, `JUST` | `(justified)` |
| `BlankWhenZeroClause` | `BLANK WHEN ZERO` | `(blank-when-zero)` |
| `GlobalClause` | `GLOBAL` | `(global)` |
| `ExternalClause` | `EXTERNAL` | `(external)` |
| `RenamesClause` | `RENAMES` | `(renames PST-ACCOUNT)`, `(renames (through PST-TYPE PST-ACCOUNT))` |

Noise words and alternate keyword spellings are not written. The optional
`IS`, `TIMES`, `ON`, `KEY`, `WHEN` and `CHARACTER`, and `RIGHT` after
`JUSTIFIED`, carry nothing `cobol-go` keeps; a keyword of several words is one
tag (`depending-on`, `indexed-by`, `blank-when-zero`); and `PIC` and `PICTURE`,
`SYNC` and `SYNCHRONIZED`, `JUST` and `JUSTIFIED`, `THRU` and `THROUGH`, `VALUE`
and `VALUES` are one clause apiece in its parse. So each clause has one form,
and two generators transcribing one copybook write one text. `ARE` after
`VALUES` is the one noise word the pinned `cobol-go` does not read as noise, and
[A condition-name cobol-go misreads is refused, not
transcribed](#a-condition-name-cobol-go-misreads-is-refused-not-transcribed) is
what follows.

`through` is one form wherever it appears: `(through <from> <to>)`, a range in a
`value` and the range a `renames` names. Which operands it may hold in which
position — literals in a condition-name's range, data-names in a `RENAMES` — is
`cobol-go`'s to judge, not a second rule here.

A `value` holds one or more operands, each literal text, a data-name, or a
`through`. That an ordinary item carries one value and a condition-name may
carry several is `cobol-go`'s too.

An `occurs` holds its count, then its phrases — `to`, `depending-on`, any number
of `key`, and `indexed-by` — in that order, which is the order `cobol-go` holds
and prints them in. `to`, `depending-on` and `indexed-by` appear at most once
each. Each `key` is one phrase naming one key, `(key <order> <data-name>)`, as
`cobol-go` holds it.

### The leaves: a level, a data-name and literal text

The leaf rules are stated once, here, and hold in every entry.

**A level is a number:** `01`, `05`, `66`, `77`, `88`. Which levels exist, and
which may follow which, is `cobol-go`'s.

**Every COBOL word is a data-name.** That covers data-names, condition-names
and index-names, and any word in a `VALUE` or `THROUGH` operand, figurative
constants included — `SPACE`, `ZEROS`, `HIGH-VALUES`. A data-name is a symbol,
in the case the copybook wrote it. It is written

```
(word "<spelling>")
```

only where `sexpr-go` would read the spelling as something other than that
symbol, which today means only `nil`: `sexpr-go` reads `nil`, in lower case,
as its empty value, and every other COBOL word `cobol-go` reads is a symbol to
it. A `word` around a spelling that reads as a symbol is a diagnostic, so that
every data-name has one spelling.

**Every COBOL literal token, PICTURE string and OCCURS bound is text** holding
exactly that token as COBOL writes it, delimiters included: `"'A'"`, `"-12.50"`,
`"S9(7)V99"`, `"50"`. The token is the lexeme `cobol-go` holds — a literal a
fixed-format copybook continued across lines is the one token `cobol-go` joined
it into — and a text holding anything other than exactly one COBOL token is a
diagnostic. A COBOL numeric literal is text rather than a `sexpr-go` number
because the token is COBOL's: `cobol-go` keeps its lexeme raw, sign and trailing
zeros included, and a number read by `sexpr-go`'s rules would be a second lexis
deciding what the copybook wrote.

**The operands of the four closed sets are upper-case symbols:** the `USAGE`,
the `SIGN` position, the `SYNCHRONIZED` direction, and the `KEY` order —
`COMP-3`, `LEADING`, `LEFT`, `ASCENDING`. They are upper case because `cobol-go`
upper-cases them, so a copybook's `comp-3` is `COMP-3` in its parse and in the
transcription. Which values are admitted is `cobol-go`'s to judge, not a list in
this document: a usage `cobol-go` gains is one a definitions file may write the
same day. `SEPARATE` on a `sign` is written as that one upper-case symbol. No
other word is case-folded.

### What is written follows from what cobol-go reads

Order and repetition are not choices this notation makes. Each is a consequence
of transcribing everything `cobol-go` reads.

- **Entries, and the clauses within each entry, are written in source order.**
  `cobol-go` holds both in the order the copybook wrote them, and an entry body
  that reordered them would be a different parse.
- **A clause the copybook repeats is repeated.** `cobol-go` parses a repeated
  clause and `copybook.Build` reads the first, so dropping a later one would
  drop a clause `cobol-go` read — and an entry whose meaning depended on which
  duplicate a transcription kept would be one whose meaning was the
  transcription's.
- **At levels other than 66 and 88, an unnamed item is written `FILLER`**,
  whether the copybook wrote `FILLER` or omitted the name. `cobol-go` reads
  `FILLER` there as a keyword and without regard to case, so no data-name at
  those levels is spelled `FILLER`.
- **At 66 and 88 a name is always a name.** `cobol-go` holds a name spelled
  `FILLER` there as a name, so it is written as the copybook spelled it.

### Presence, and an operand that may be absent

A clause COBOL writes with no operand is a form with no arguments: `(global)`,
`(justified)`, `(blank-when-zero)`, `(synchronized)`. The form's presence is the
clause, as the keyword's presence is in the copybook.

An optional operand — `SEPARATE` on a `sign`, a direction on a `synchronized` —
is the form's `at-most-one` last argument, and absent where the copybook wrote
none. The dialect's `0..1` already means exactly this. The schema checker's
refusal of an absent one is a defect, which #396 fixes; the dialect does not
change.

Neither is the boolean [What this document
delegates](#what-this-document-delegates) refuses, and that section says why:
its rule governs a layout's own forms, and an entry body follows these rules
wherever it is held, inside a block included. `#t`, `#f`, `nil` and `()` stay
refused everywhere.

### cobol-go judges an entry, and nothing here says what one means

Validity is `cobol-go`'s to judge. An entry is admissible exactly when printing
the `DataDescriptionEntry` cpybkc built from it with `cobol.Print`, and reading
the result back in free format with `cobol.Parse(WithFragment())`, gives back
that entry, with positions and comments cleared. This holds in a definitions
file and in a block alike, and it is the whole of what an entry has to satisfy
beyond its shape (#401).

That rule makes `cobol-go`'s parser the judge without making this document a
second grammar. An entry `cobol-go` would not read back as itself — an 88
carrying a `pic`, a usage it does not know, a name it would read as a keyword
— is one whose meaning would be cpybkc's reading of it rather than `cobol-go`'s,
and the point of a transcription is that there is one reader.

What an admissible entry *means* is cited, never restated: the hierarchy, USAGE
inheritance, widths, `REDEFINES`, `OCCURS DEPENDING ON` and `SYNCHRONIZED` slack
are `copybook.Build`'s and `NewLayout`'s, applied to these entries exactly as
they are applied to a copybook's. No form for an offset, a width or a length
exists, in an entry body or anywhere else ([A layout states relationships and
sizes, and nothing derived](#a-layout-states-relationships-and-sizes-and-nothing-derived)).

### The printed form `init` writes

A definitions file has a canonical printed form, and it is the form `init`
writes:

- one entry per line;
- two spaces of indentation per depth;
- the level, the name and each clause separated by single spaces;
- levels as two digits.

The depth is the one `cobol-go`'s printer gives an entry, spelled out here so
that a second implementation can write it without reading that printer:

- an entry at levels 01–49 is as deep as the number of entries enclosing it,
  counted by level numbers alone — a 01–49 entry encloses the entries after it
  until one at its level or lower;
- 66 and 77 are at depth 0;
- an 88 is one deeper than the nearest entry before it that is not an 88.

`copybook.Build`'s tree is not the source of the depth, because it holds 88s and
66s outside its children; the level numbers are.

A block's canonical form is the file's, with every line indented two more
spaces. The block opens on its own line, `(definitions-block <name>`, and closes
on its last entry's line. `init` writes no block, and nothing checks this form:
it is what a person moving a file's entries into a layout is advised to write,
and any other whitespace reads the same. The [definitions
file](#appendix-a-definitions-file) and the [block in a
layout](#appendix-definitions-held-three-ways) appendices are both in it.

### What a transcription leaves out

Four things a copybook carries are not transcribed, each for its reason.

- **Copybook comments.** Reason: `cobol-go` keeps them and nothing below it
  reads them; the validity rule above clears them before it compares. A
  definitions file or a block **MAY** carry `;` comments of its own, and nothing
  reads those either.
- **Noise words.** Reason: `cobol-go`'s parse holds none of them, so there is
  nothing to transcribe, and writing them would give one clause two spellings.
- **Reference-format columns** — the sequence area, the indicator area and the
  identification area of a fixed-format copybook. Reason: they are where the
  copybook's text sat, not statements about the record, and `cobol-go` reads
  them away. An entry body has no columns, and the judge reads it back in free
  format.
- **The difference between `FILLER` and an omitted name.** Reason:
  `copybook.Build` makes both one unnamed item, so nothing downstream of the
  parser can observe the difference, and one spelling for one meaning is the
  rule everywhere else in this format.

**Every copybook cpybkc can read today** is one that both
`cobol.Parse(WithFragment(), WithSourceFormat(FixedFormat))` and
`copybook.Build` accept and that declares at least one entry. Every such
copybook converts to an entry body that `cobol-go` reads back as the same
entries (#401), with the one exception the next section states and argues.
What the pinned `cobol-go` does not read at all is [out of
scope](#what-the-pinned-cobol-go-does-not-read), with its reasons.

### A condition-name cobol-go misreads is refused, not transcribed

Most constructs the pinned `cobol-go` cannot read are refused through the parse
failure, and so never reach a transcription. A condition-name's `VALUE` list is
the exception. There the pinned `cobol-go` reads `X'00'`, `N'A'` and `ALL '*'`
each as two values, a word and a literal, and reads `VALUES ARE 'A' 'B'` as
three, with `ARE` the first; `copybook.Build` accepts all four. A transcription
of what `cobol-go` read would be `(value X "'00'")` and
`(value ARE "'A'" "'B'")`.

Conversion **MUST NOT** write such an entry. Where a copybook writes a
condition-name's `VALUE` list with a prefixed literal, an `ALL` literal or the
phrase `VALUES ARE`, conversion refuses that copybook, naming the entry and the
`cobol-go` story that reads the construct: Zaba505/cobol-go#140 for a prefixed
literal, #142 for `ALL`, #135 for `VALUES ARE`. It writes no definitions file
for it. How `init` reports a refusal beside the copybooks it does convert is
#403's.

Reason: a transcription is the one place the misreading would outlive the fix.
A copybook is re-read at every pin, so the day `cobol-go` reads `X'00'` as one
literal, every record bound to that copybook reads it so. A definitions file is
checked-in text that is never re-read from its copybook — that is what it is
for — and `(value X "'00'")` would go on stating, as the copybook's own, a
reading `cobol-go` has been fixed to stop making. Nothing would then tell it
apart from a value list an adopter wrote by hand, and an entry body printing
`VALUE X '00'`, with its space, is one `cobol-go` will rightly go on reading as
two values. Refusing keeps the rule a reader can predict: conversion carries no
prefixed literal, no `ALL` literal and no `VALUES ARE` until `cobol-go` reads
them, in this position as in every other, and the refusal says which story
lifts it. An entry body cannot write one either, since `"X'00'"` is two tokens
to the pinned `cobol-go` and so not literal text. Condition-names reach no
descriptor today, so a record bound to the copybook itself loses nothing a run
emits in the meantime.

### Where the rest of it lands

This chapter is the notation and nothing built on it. The stories filed with it
under discussion #394 carry the rest:

- #396, the schema checker admitting an absent `at-most-one` argument;
- #397, a run checking a layout against the published schema;
- #398, what may break before 1.0.0;
- #399, how a record names a definitions file or a block, and what a run reads;
- #400, the schema declarations and the reader for an entry body;
- #401, the converter, the judge, and the round trip over every copybook;
- #402, a record naming a file or a block;
- #403, how `init` writes a definitions file per copybook;
- #404, the data-name sort wherever a layout names an item;
- #405 and #409, resolving a record held in a file and in a block;
- #406 and #408, `init` scaffolding records and writing definitions files;
- #407, where a `COPY` member is found;
- #410, a worked example shipping definitions files and no copybook.

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

A variant is the one of the two that may also be settled without testing
anything. Where a table's entries carry roles rather than types — entry one the
home address, entry two the work address — the alternative is decided by the
occurrence's **position**, and
[`schedule-variant`](#a-schedule-for-a-redefine-chosen-by-position) is where
that is written. It carries no strategy and is not a third scope for the set
below; it is the second of the two ways a variant may be settled, and a variant
is settled by exactly one of them (#346, discussion #340).

A redefine inside a table that is **not** a variant carries no strategy either,
and is neither of those forms. Where every occurrence takes one alternative
there is nothing to choose between, so there is no variant to settle and no
scope for a strategy to run in;
[`take-alternative`](#every-occurrence-of-a-table-takes-one-alternative) is
where that is said, and it is the third and last thing a layout says about a
redefine inside a repeating group (#341).

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
miss, and the IR refuses the reading in as many words, because a record admitted
by a strategy that tests nothing may not sit at a state beside a record that is
eligible with it.

Nor is there a form saying to try one discriminator first and read what it does
not match as the other record. No spelling is reserved for one — not a fourth
strategy, not a modifier on `discriminate`, and not a rule reading the order of
the `discriminate` forms or of an `alt`'s operands — and there is nothing to
write, because the order is not the layout's to choose. Where two records
eligible at one point key on runs of bytes that share none, the resolved
automaton evaluates them in the order a reader reaches those bytes in, which the
copybooks decide and which neither the layout nor the shape of its sequencing
can reach
([`ir/SPEC.md`](../ir/SPEC.md#transitions-are-ordered-by-what-they-read)).

That is not the `otherwise` this section refuses, and the difference is worth
being exact about, because it was read as one and refused as one until #332. An
`otherwise` catches what the others missed, so what it admits depends on every
arm beside it and on nothing about its own record; the ordered pair is two tests
that each say what they are looking for, tried in an order derived from the
records themselves. What it *does* share with an `otherwise` is the cost: the
order is a convention a layout asserts about its data rather than a proof that
the two tests cannot both hold, and a record satisfying the earlier test is read
as that record with no diagnostic anywhere. So the layout still says nothing
about ordering, and what an adopter is choosing when they write two such
discriminators is set out at [A batch boundary, told by the
order](#a-batch-boundary-told-by-the-order). Two records keying on runs that *do*
share a byte are refused where their literals agree over it, which is a clash
the literals are the place to fix
([`ir/SPEC.md`](../ir/SPEC.md#when-two-match-and-when-none-does)).

### A batch boundary, told by the order

`(+ (seq HEADER (* DATA)))` — a header, then details until the next header — is
the commonest shape a batched extract takes, and it is an ordinary layout. At
the state after a `HEADER` the sequence admits `DATA`, by another pass of the
inner `*`, and `HEADER` again, by the outer `+`'s back edge, so both records are
eligible and something has to tell them apart.

Where the two type codes sit at one offset and one width, `equals` on each
provably excludes the other and there is nothing more to say. This section is
about the file where they do not, because that file is common and its copybooks
usually cannot be changed:

```
01  BATCH-HEADER.
    05  BH-TYPE          PIC X(3).
    05  BH-BATCH-NO      PIC 9(6).
    05  BH-DATE          PIC X(8).
    05  BH-FILLER        PIC X(23).

01  BATCH-DETAIL.
    05  BD-ACCOUNT       PIC X(10).
    05  BD-TYPE          PIC X(1).
    05  BD-AMOUNT        PIC 9(11).
    05  BD-FILLER        PIC X(18).
```

```
(record HEADER (copybook "batch.cpy" BATCH-HEADER))
(record DETAIL (copybook "batch.cpy" BATCH-DETAIL))

(discriminate HEADER (equals (item HEADER BH-TYPE) "HDR"))
(discriminate DETAIL (equals (item DETAIL BD-TYPE) "D"))

(sequence (+ (seq HEADER (* DETAIL))))
```

The header is told by three bytes at the front and the detail by one byte behind
its account number, so the two runs never meet: bytes 0–2 in a header and byte
10 in a detail. No literal either record could carry separates them, because a
record carries `HDR` at bytes 0–2 and a `D` at byte 10 at the same time. What
reads the file is trying the header's test first, and that is what the resolved
automaton does — not because this layout said so, but because the header's run
begins at the earlier byte and transitions are evaluated in the order a reader
reaches their bytes
([`ir/SPEC.md`](../ir/SPEC.md#transitions-are-ordered-by-what-they-read)).

**What the layout is asserting, by writing it.** That no detail carries `HDR` in
the first three bytes of its account number. Nothing checks that: `resolve`
cannot, because the account number is `PIC X` and every byte is a byte it may
hold, and a consumer cannot, because such a detail matches a predicate the
descriptor carries — so it is not an undescribed record, and where the two
records are the same width the framing has nothing to disagree with either. The
batch simply splits in the wrong place, and the file is reported complete
([`ir/SPEC.md`](../ir/SPEC.md#a-batch-boundary-is-told-by-the-order)).

That is the whole of what is given up, and it is given up because the
alternative was refusing a file the adopter has (discussion #323, reversing
#324). Four constructions make the pair provably exclusive instead, and a layout
that can write **one** of them **SHOULD**:

- **a type code at a common offset and width in both records**, which makes the
  two literals name the same run, so `equals` on each provably excludes the
  other;
- **an item with a bounded value set** at the offset and width of the other
  record's discriminator, which `one-of` can name to line the two runs up;
- **a count in the header**, written `(times …)` on the inner repetition, which
  separates the two transitions by a guard instead of by bytes ([Two operators
  read a value, and they are the automaton's
  memory](#two-operators-read-a-value-and-they-are-the-automatons-memory));
- **a trailer ending each batch**, `(+ (seq HEADER (* DATA) TRAILER))`, which
  moves the back edge to a state where `HEADER` is the only record admitted.

The copybooks often supply the first without anybody writing anything. Where the
ten bytes a header carries at the detail's type-code offset are a numeric
`DISPLAY` item — a batch number, a date, a sequence — a header can never hold
the detail's literal there, and where the same holds in the other direction the
two tests are proved exclusive from the copybooks alone and the order at that
state cannot be observed
([`ir/SPEC.md`](../ir/SPEC.md#when-two-match-and-when-none-does)). What is left
resting on the order is the pair where at least one of those items is `PIC X`,
which is the pair written out above.

Two things are still refused here, and neither is about the order. A pair whose
runs *do* share a byte and whose literals agree over it is a clash in the
literals, and `resolve` names both records and both runs. And a discriminator
reading past the end of the shortest record its state can put in front of a
consumer is refused whatever the other record does, under the framings that
leave the bound to the layout — a detail keyed behind a twenty-byte account
number, beside a twelve-byte header, is that fault and not this permission
([`ir/SPEC.md`](../ir/SPEC.md#a-predicate-never-reads-past-the-record-in-front-of-it)).


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

**Position *inside a table* is not a strategy either, and it is not refused.**
An alternative taken by occurrence one and another by occurrence two is chosen
by where the entry sits rather than by anything in its bytes, so no strategy
here could express it and none is added: it is written as a
[`schedule-variant`](#a-schedule-for-a-redefine-chosen-by-position) instead, a
form of its own carrying occurrence numbers and no test at all
([`ir/SPEC.md`](../ir/SPEC.md#an-arm-may-be-selected-by-its-position-in-the-table),
#346). It reaches a variant and nothing else, because an occurrence is the only
place a position of that kind exists; a *record*'s position in the stream is the
next paragraph's and is settled differently.

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
  (arm <data-name> <predicate>)
  (arm <data-name> <predicate>)
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
| `arm` | `(arm <data-name> <predicate>)` | 2..n | an alternative, by its [data-name](#the-leaves-a-level-a-data-name-and-literal-text), and what selects it |

A `<predicate>` is `equals` or `one-of` — the same two strategies, testing the
same [literals](#literals), and no third. `single-record-type` is not among
them and there is no arm carrying nothing: an arm selected by nothing at all is
a default arm, and
[`ir/SPEC.md`](../ir/SPEC.md#a-predicate-on-an-arm-reads-one-occurrence) refuses
one in as many words. An occurrence matching no arm is reported by the
consumer, with a diagnostic distinguishable from a record no transition matched,
and an adopter whose entries carry a code the alternatives do not cover writes
an arm for it.

**A layout may say that every occurrence of a table takes one alternative, and
this is not the form it says it in.** It is written as a
[`take-alternative`](#every-occurrence-of-a-table-takes-one-alternative), which
names the redefine and the one alternative and carries no arms at all — the
ordinary case where only one of a copybook's overlaid descriptions is ever in
the data, and the case this form's own arity refuses (#341, discussion #340).
The two-arm floor below stands because of it: one arm carrying a predicate
would be a test on bytes that decide nothing, and one arm carrying none would
be the default arm the paragraph above refuses, so a variant with one arm is
neither spelling of a statement that has a spelling of its own.

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

Exactly one form **MUST** name each redefine inside a repeating group, and it is
this one, a [`schedule-variant`](#a-schedule-for-a-redefine-chosen-by-position)
or a [`take-alternative`](#every-occurrence-of-a-table-takes-one-alternative) —
two forms naming one item is a diagnostic like a second `discriminate` on one
record, whether the two are of one tag or of two (#346, #341). A redefine that
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

### A schedule for a redefine chosen by position

```
(schedule-variant <item-ref>
  (arm <data-name> <occurrence> …)
  (arm <data-name> <occurrence> …)
  …)
```

Some tables carry roles rather than types. A count says how many entries
arrived, entry one is the home address, entry two the work address, entry three
the mailing address, and no byte of an entry says which it is — so there is
nothing for a strategy to test, and the alternative is decided by where the
entry sits. This form is where that is written, and it is the second of the two
ways a variant is settled (#346, discussion #340).

The argument names the **variant**, exactly as
[`discriminate-variant`](#a-discriminator-for-a-redefine-inside-a-table) does:
an item reference to the item the copybook redefines. Each `arm` names one
alternative and the occurrences it is taken for, counted from one.

| Position | Sort | Arity | What it says |
|---|---|---|---|
| variant | item reference | 1 | the redefined item, inside a repeating group |
| `arm` | `(arm <data-name> <occurrence> …)` | 2..n | an alternative, by its [data-name](#the-leaves-a-level-a-data-name-and-literal-text), and the occurrences it is taken for |

An arm takes **one or more** occurrences, because a schedule that takes one
alternative for several entries is the shape that has nowhere else to go: the
alternative to this form is unrolling the table into one member per occurrence,
and two unrolled members taking one alternative would carry one copybook name
between them, which
[`ir/SPEC.md`](../ir/SPEC.md#an-arm-may-be-selected-by-its-position-in-the-table)
refuses a producer inventing the difference for.

**It is available whichever way the table's count is read.** A layout stating
`odoslide` and one stating `noodoslide` may both carry this form
([The `OCCURS DEPENDING ON` reading is one
statement per layout](#the-occurs-depending-on-reading-is-one-statement-per-layout)).
Which reading applies is a property of the extract rather than of the copybook,
and it must not also decide whether the copybook can be described; the argument
is `ir/SPEC.md`'s and is made where the mechanism is.

**A variant is scheduled or discriminated, never both.** No variant **MAY** be
named by a `schedule-variant` and a `discriminate-variant`, and within this form
there is no arm carrying a predicate: a variant whose arms were selected two
different ways would need both the static check below and the byte-overlap check
the other form carries, and a writer would then take an arm from its caller for
one entry and from the descriptor for the next. That is the whole reason the two
are separate forms rather than a second shape of `arm` inside one —
[`ir/SPEC.md`](../ir/SPEC.md#an-arm-may-be-selected-by-its-position-in-the-table)
makes the argument, and two forms let the schema state the half of it that is
structural instead of leaving all of it to the reader, which is the same trade
[A discriminator for a redefine inside a
table](#a-discriminator-for-a-redefine-inside-a-table) makes when it refuses to
be a second spelling of `discriminate`. The tag says which as well: nothing here
discriminates anything, so nothing here is spelled `discriminate-`.

`arm` is the same tag in a different sort, as
[`one-of`](#three-strategies-and-the-set-is-closed-for-v1) already is in a
strategy and in a `when`. Which one is written is decided by the form it sits
in, and neither is reachable where the other belongs.

The layout reader checks the halves that need no copybook. The variant reference
**MUST** be rooted at a `record`. Every occurrence **MUST** be a positive
number, and every arm **MUST** name at least one — the schema states that half
as the `one-or-more` arity on the position, and an arm scheduled for nothing is
an arm nothing selects. No two arms **MAY** name one alternative. And no
occurrence **MAY** be named twice by one variant, whether by two arms or twice
within one arm: that is the overlap rule of the other form, decidable here from
the layout alone because a number is a number whatever charset the file is in.
Arms **MUST** be written in ascending order of their first occurrence, and each
arm's occurrences in **strictly** ascending order, which is what leaves a
duplicate inside an arm unambiguously a diagnostic and what makes two layouts
describing one file one text.

Everything else needs the copybook and is `resolve`'s: that each name is an
alternative the copybook declares at that position, that the group containing
them repeats, and — the check this form exists to make possible — that the
schedule covers every occurrence the table can hold, exactly once, from one to
the repetition's declared maximum. A schedule leaving an entry uncovered is
rejected there, naming the record, the repeating group, the variant and the
occurrence numbers with no arm. Because that check is static, an entry matching
no arm is not a failure a consumer can meet at read time for this form, which is
the one thing it takes away from the byte-selected one
([`ir/SPEC.md`](../ir/SPEC.md#an-arm-may-be-selected-by-its-position-in-the-table)).

```
;; Each entry of the table has a role rather than a type: entry 1 is the home
;; address, entry 2 the work address, entry 3 the mailing address, and nothing
;; in an entry's bytes says which of the three it is.
(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)
  (arm ADR-HOME 1)
  (arm ADR-WORK 2)
  (arm ADR-MAIL 3))
```

### Every occurrence of a table takes one alternative

```
(take-alternative <item-ref> <data-name>)
```

A redefine inside a repeating group is not always a choice. In a
mainframe-produced file the item that gets redefined is routinely not in the
data at all — only one of the redefinitions ever arrives, and the base exists to
declare the storage they share — so a copybook writing `ADR-HOME` redefined once
by `ADR-WORK` inside a table describes a file every entry of which is an
`ADR-WORK`. This form is where that is written (#341, discussion #340).

The argument names the redefine, exactly as the two forms above do: an item
reference to the item the copybook redefines. The name beside it is the one
alternative every occurrence takes, by the name the copybook gives it — which
**MAY** be the redefined item's own, where what the file carries is the base and
none of the redefinitions.

| Position | Sort | Arity | What it says |
|---|---|---|---|
| redefine | item reference | 1 | the redefined item, inside a repeating group |
| alternative | [data-name](#the-leaves-a-level-a-data-name-and-literal-text) | 1 | the alternative every occurrence takes |

**What it says is that there is no variant**, and that is why it is not a
one-armed `discriminate-variant`. A redefine every occurrence of which takes one
alternative resolves to that alternative's items with no variant node at all,
the way a record-level redefine whose layout names one alternative resolves to
one record node
([`ir/SPEC.md`](../ir/SPEC.md#a-variant-is-chosen-once-per-occurrence)) — so
nothing here is discriminated, nothing is scheduled and nothing is an
alternation. Spelling it as one arm of `discriminate-variant` would put
`discriminate-` on a form that chooses nothing, which is the argument that makes
[`schedule-variant`](#a-schedule-for-a-redefine-chosen-by-position) a form of
its own, and it would turn that form's two-arm arity into a rule about which of
two shapes was written — a conditional the schema could not state at all, where
today it states the arity outright.

It is the table's counterpart of [Which alternative a record
is](#which-alternative-a-record-is) and not a second spelling of it. An
`alternative` child says which of the record types a record-level redefine
resolved into a `record` form means, and the choice is the record's identity;
this says which alternative every entry of a table is, and the bytes never leave
the record they are in. A layout naming an item inside a repeating group in an
`alternative` child is the diagnostic that section states, unchanged.

The layout reader checks the halves that need no copybook. The reference
**MUST** be rooted at a `record` the layout defines, and **MUST** carry at least
two names, because a redefine this form can name sits inside a group that
repeats — the same floor
[`discriminate-variant`](#a-discriminator-for-a-redefine-inside-a-table)'s
reference stands on. And exactly one form **MUST** name each redefine inside a
repeating group, this one included: a redefine that both takes one alternative
and tells two apart would leave the order the forms were written in deciding
which it does.

Everything else needs the copybook and is `resolve`'s: that the reference names
an item the copybook redefines, that the group containing it repeats, and that
the name beside it is an alternative declared over those bytes. A redefine no
form names is `resolve`'s too, and this form is the third of the three answers
its diagnostic asks for.

```
;; Only ADR-WORK is ever in the file. ADR-HOME is the storage the copybook
;; declares it over, and no byte of an entry tells the two apart because no
;; entry is ever an ADR-HOME.
(take-alternative (item ADDR ADR-ENTRY ADR-HOME) ADR-WORK)
```

What it replaces is the only thing an adopter could write before it: two arms
and a predicate invented to tell `ADR-WORK` apart from an alternative the file
never carries. That is a test on bytes that decide nothing, which is the
collapse the arm rules exist to prevent — and it was what a format whose own
diagnostics advise naming one alternative left an adopter to write, having
nowhere to write the thing they were advised to (#341).

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
the *bytes* a consumer compares — the bytes a file under this layout's own
encoding holds there. An adopter writing `"01"` on a `PIC X(2)` field of an
EBCDIC file has said something true about their data; working out that it is
`F0 F1` needs the charset, the width and the padding rule. The width and the
padding rule are COBOL knowledge — which items pad, on which side, and what a
literal shorter than its item means — and a generator must never have to hold
it: it arrives applied.

What a generator may still do is carry those bytes onto the same records held
under other axes — the extract a transfer converted to ASCII, say — and a
generator offering that re-expresses each literal once, before it reads or
writes a record
([`ir/SPEC.md`](../ir/SPEC.md#a-consumer-may-read-under-other-axes-and-re-expresses-what-it-compares),
#379). That is not the knowledge the sentence above keeps from it. Re-expression
moves a byte from one code page to another, reads a sign byte's column in
`codec/SPEC.md`'s table, reverses a binary item's bytes or re-encodes a float:
facts about bytes under an encoding, which a generator already holds to read any
field at all. It applies no comparison rule of COBOL's, because the padding it
would have needed is already in the literal and a space re-expresses as a space.

A byte string is there for the field whose value is not text and not a number —
a flag byte carrying a bit pattern — and for the adopter who has a hex dump and
no PICTURE they trust. It says exactly what is in the file under this layout's
encoding, which is what every literal says once resolved: the IR does not record
how a literal was spelled, so a consumer reading under other axes re-expresses a
byte string by its field's axes like any other, and is not wrong about a
converted file for having been given one. A flag byte in a text item is carried
through the charset with the text around it, as the transfer that rewrote the
file carried it. The item whose bytes a transfer left alone is the one to
declare with `(charset none)` ([A byte is not a character, and such an item has
no charset](#a-byte-is-not-a-character-and-such-an-item-has-no-charset)), and a
literal compared against it is never re-expressed, since nothing about that
item's bytes is.

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

The `only` in `when`'s two rows is the whole of what it says, and it is a
permission rather than a requirement. `<e>` **MAY** appear where the item holds
the value and **MUST NOT** appear where it does not; nothing says `<e>` **MUST**
appear where it does (#144). An adopter reading a flag as *the record that
follows is a `SPECIAL`* is reading the commoner intention and not the operator,
and [A `when` permits a record, and never requires
one](#a-when-permits-a-record-and-never-requires-one) is why the operator has
the weaker of the two meanings and what to write where a file means the
stronger.

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
- The item **MUST NOT** be one the copybook gives an `OCCURS`, and **MUST NOT**
  sit at any depth inside a group that has one (#84). A register is bound from a
  field and nothing anywhere names an occurrence of one, so an item with a value
  per occurrence is a value the automaton has no spelling for
  ([`ir/SPEC.md`](../ir/SPEC.md#a-reference-names-a-field-not-an-occurrence-of-one)).
  This is a restriction on the copybook and not on the expression: how often the
  record *holding* the item is admitted is not restricted at all, so a record
  inside a `+`, a `*` or a `times` may carry the item another operator reads
  like any other, and the register is rebound every time that record is
  admitted (#144). Which occurrence governs is then the nearest preceding one
  along the path actually taken, which is
  [`ir/SPEC.md`](../ir/SPEC.md#the-automaton-remembers-in-registers)'s register
  file having no scope and no history.
- Under `times` the item **MUST** be one whose value decodes to an integer.

A value that governs only the record it sits in needs neither operator. Two
records built to different copybooks and told apart by a flag are an `alt` of
two record names with a discriminator each, and the dependence on the value
becomes the state the automaton is in. Reaching for `when` there costs every
consumer in every language a register it did not need
([`ir/SPEC.md`](../ir/SPEC.md#when-a-value-becomes-a-state-and-when-it-becomes-a-register)).

### A `when` permits a record, and never requires one

A production shape this format meets early: a data record carries a flag, and
where the flag holds one value the record that follows it is a particular type.
`when` says half of that. Where the item does not hold the value the guarded
records are not admissible, which is the half a guard can carry. Where it does,
they are eligible alongside everything else the point already admitted — so a
file whose flag says a `SPECIAL` follows and whose next record is an ordinary
one is a file this format reads without complaint (#144).

What it lowers to is why, and the shortfall is one guard rather than one
operator. A guard is one of the three tests
[`ir/SPEC.md`](../ir/SPEC.md#the-automaton-remembers-in-registers) closes, and
none of them is a negation. `(when <item-ref> <literal> <e>)` therefore compiles
into a guard on the transitions admitting `<e>` and into nothing at all on the
transitions that skip it: there is no test for *the item does not hold that
value* to put on them. Requiring `<e>` means making every other transition out
of that point ineligible, and excluding is the thing the guard set cannot do.
[A `when` that requires the record it
governs](#a-when-that-requires-the-record-it-governs) is where that was weighed
and why the fourth test is not coming.

**Where the governing value is the record's own discriminating item, the
stronger meaning is already sayable**, and it costs no register and no guard at
all. Two record names over one copybook, told apart by that item, put the
automaton in a different state for each — and the state a flagged record leads
to offers only what must follow it:

```
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(record DETAIL-FLAGGED (copybook "dtl.cpy" DTL-REC))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(discriminate DETAIL-FLAGGED (equals (item DETAIL-FLAGGED DTL-TYPE) "F"))
(sequence (seq HEADER (+ (alt DETAIL (seq DETAIL-FLAGGED SPECIAL))) TRAILER))
```

That is the paragraph above carried one step further than
[`ir/SPEC.md`](../ir/SPEC.md#when-a-value-becomes-a-state-and-when-it-becomes-a-register)
reads it. The value governs a *later* record, which that section calls a
register — and it becomes a state anyway, because the record holding it can be
told apart by it, and a record type is the strongest thing this format has for
saying what may come next. [Many records may name one
copybook](#many-records-may-name-one-copybook-and-two-may-name-one-item) is what
makes the two names cost nothing but two names.

The precondition is exact and `resolve` enforces it as an overlap rather than as
a missing feature: the flag has to be the item the two records are told apart
by. Where it is a second field beside the type code, telling `DETAIL` from
`DETAIL-FLAGGED` needs one test and telling either of them from whatever else
the point admits needs another, and a discriminator is one test on one item —
the conjunction [What the algebra deliberately cannot
say](#what-the-algebra-deliberately-cannot-say) refuses. There the stronger
meaning is not sayable, `when` says the weaker one, and the check the file was
carrying is one the layout throws away. That is a real loss, and it is stated
here rather than left to be found because the reading an adopter brings to a
flag is the meaning this operator does not have.

### A guard on a repetition guards every way into its body

A `when` guards the transitions that *enter* the expression it wraps, and a
repetition has two ways in: the one from outside it, and the one from the end of
a pass back to the start of the next. Which of them a guard lands on follows
from where the `when` is written, and both spellings are legal (#144):

- `(when <item-ref> <literal> (+ <e>))` guards the way in. The transitions
  entering the repetition carry the guard and the back edge does not, so the
  value decides whether the run happens and nothing about how long it is.
- `(+ (when <item-ref> <literal> <e>))` guards every way in, the back edge
  included, because the `when` wraps the whole of the repeated body and every
  transition admitting that body's first record is a transition entering `<e>`.

`*` and `?` read the same way. `times` carries its own guard on both ways in,
because a count that stopped being tested after the first pass would not be a
count.

There is no third spelling, and in particular **there is no way to guard a
repetition's back edge and leave the way in unguarded.** The algebra names
records, not edges: a record name is one position in the compiled automaton
however many transitions reach it, and a `when` wrapping that name guards all of
them. A layout wanting the back edge alone is asking for two entries to one
position to be told apart, and telling them apart is making them two positions —
which is the `alt` of two record names the section above ends on.

One consequence is worth stating on its own, because the diagnostic for it names
the read loop rather than the repetition. Since a guard on the back edge is a
guard on the way in as well, its item **MUST** satisfy the strictly-earlier rule
above at the way in — and an item of the repeated record itself never does,
because on the first pass nothing has bound it yet. `(+ (when (item DETAIL
NEXT-FLAG) "X" DETAIL))` is rejected for that reason and for nothing to do with
`+`; the same reference under a `when` governing some *other* record inside the
same repetition is admitted, and is the ordinary way a flag on a repeating
record is written.

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

An arity means the same thing on an argument as on a child: an `at-most-one`
argument may be left out. Arguments are matched by position, in the order they
are declared, so the declarations are held to two rules that keep that matching
free of guesses, and a schema breaking either is refused when it is loaded.
Only the last argument of a form may repeat or be optional — an earlier one
would leave two readings of the same list. And a form whose last argument
repeats or is optional admits no children, because nothing would say where its
arguments stop, or whether the node after the others is the argument or the
first child (#396).

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
is being assembled. A change to what a layout may say, made before the first
release, is the settling this section asks for rather than an exception to it,
and it is the standing [`ir/SPEC.md`](../ir/SPEC.md#the-version-field) gives its
own closed sets under `IR_VERSION_1` (#28). What ends the period is not defined
twice here: [`ir/SPEC.md`](../ir/SPEC.md#while-ir_version_1-is-being-assembled)
names the event, the project's first `v1.0.0` release, and *the first release*
means that in this document too (#347).

What a consumer has in the meantime is not nothing. `layout-schema.sexpr` has
been a release asset since `v0.0.1`, so an adopter does hold a schema while the
period is open, and the declarations a release added are a diff of two assets
they already have — the same poorer signal
[`ir/SPEC.md`](../ir/SPEC.md#while-ir_version_1-is-being-assembled) offers for
the IR over the same period, and for the same reason. `schedule-variant` is
added on those terms (#346), and `take-alternative` is added on the same ones
(#341), and so are `definitions-block`, `entry`, every clause and operand form
of an [entry body](#definitions), `data-name` and `word` (#395, #400), and so
are `definitions`, the `record-source` sort `record` takes it in, and the
`definitions-origin` sort and `block-name` reference beneath it (#399, #402):
each is a change to what a layout may say, made while version 1 is being settled, and each
leaves the number where it is. Pinning the cpybkc that reads
the layouts a generator writes is what closes the gap until `v1.0.0`.

The version is not this document's. A spec carries no version number
([CONVENTIONS.md](../CONVENTIONS.md)); what is versioned is the interface, and
the schema is where that number lives for this one.

### One schema, and a definitions file is one block's body

There is one schema. `schema/layout.sexpr` declares `definitions-block`,
`entry`, every clause and operand form an [entry body](#definitions) holds, and
the `data-name` sort and its `word` form, beside everything else a layout may
say. A definitions file has no schema and no version of its own: it is checked
as one block's body, against the same declarations, so a fault in its shape
reads the same in a file as in a block.

A second schema would have been a second version number for one notation, and
the moment the two moved apart a definitions file valid in one layout's
vocabulary and not in another's would be possible. A block's body and a file
are one production, so they are one set of declarations.

The declarations take this shape:

- a block's entries are `definitions-block`'s repeating last argument, of a
  sort whose one form is a contextual `entry`;
- an entry's clauses are `entry`'s repeating last argument, of a `clause` sort;
- every clause and operand form is contextual, declared into a sort. No form
  the [Definitions](#definitions) chapter adds is a child form, so the set of
  tags the schema reserves for child forms across every layout does not grow,
  and a clause tag such as `value` claims nothing outside an entry;
- a closed-set operand — a usage, a sign position, a synchronization direction,
  a key order — is `symbol`, and its values are not listed;
- no new sort reuses a name the layout already declares. `literal` is the
  discriminator literal's, so an entry's literal text is a sort of another
  name.

The schema stays `(schema layout 1)`, under [the rule for changes made while
version 1 is being
settled](#the-version-is-in-the-file-and-it-moves-with-the-format). The
declarations are in `schema/layout.sexpr` itself, added by #400 after this
chapter (#395) stated them, and no second schema file or release asset is
introduced for them. A definitions file is checked by holding its top-level
forms to the declaration of a block's entries, and a diagnostic about its top
level names a definitions file where one about a layout's names a layout.

### What the schema does not say

The schema states what a layout is **shaped** like. A layout it accepts is
well-formed, not valid, and five classes of rule are deliberately outside it.

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
`record`, exactly one of `discriminate-variant`, `schedule-variant` and
`take-alternative` per redefine inside a repeating group, two arms of one
variant that do not name one alternative, every `record` appearing somewhere in the
sequencing expression, an `encoding-override` naming at least one axis, at most
one `rename` per item and no two of them substituting one name under one parent,
one `definitions-block` per name, and every block named by some record.
A declaration is about one form and its positions, so a rule relating two of
them is the reader's. The one
cross-form statement the schema does make is the reference, and it is the
important one: a position declared to name a `record` **MUST** name one the
layout defines, which is what makes a reference checkable rather than
conventional.

**Everything a copybook decides**, which is
[Validation and diagnostics](#validation-and-diagnostics)'s second list entire.

**Everything `cobol-go` judges** in an entry body: which levels, usages, sign
positions, synchronization directions and key orders exist, which clauses an
entry at a given level may carry, which operands a `through` or a `value` may
hold where it stands, and every other question [cobol-go judges an
entry](#cobol-go-judges-an-entry-and-nothing-here-says-what-one-means) hands to
the parser. The closed sets are among them: a list of usages in the schema would
be a second statement of `cobol-go`'s, for the two to disagree about the day
`cobol-go` gains one.

None of the five is a gap to be closed later by growing the vocabulary. A schema
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
one it discriminates; a second form naming one redefine inside a repeating
group, whether the two are of one tag or of two; a `take-alternative`, a
variant discriminator or a schedule whose reference names an item directly under
a record's top-level item, which no copybook can make a redefine inside a table;
two arms of one variant naming one alternative, or naming one target and one
literal; an arm whose target is rooted at another record, stands under another
outermost group than the variant, or descends through the variant or one of its
arms; a scheduled arm taken for no occurrence, an occurrence that is not counted
from one, one occurrence scheduled twice by one variant whether by two arms or
twice within one arm, and a schedule whose arms or whose occurrences within an
arm are not in ascending order; a record name in the sequencing expression that no `record` form defines,
and a `record` the expression never names; an `alternative` child whose
reference is rooted at another record, and two of them naming one item; a second
`rename` naming one item or one record, a
name substituted for two items under one parent or for two records, a substitute
equal to the
name of an item the layout itself references under that parent, and a substitute
equal to the top-level item another record binds, by whichever source; a block
name defined twice, and a block no record names; a `blksize`,
`lrecl`, `max-segment` or `delimiter` under a spelling that does not admit it;
and the framing spellings rejected by name — `U`, a carriage-control suffix,
`(blocks in-stream)`.

**`resolve`** (#32–#38) has the copybooks too, and reports everything that needs
them: a source naming a copybook or a definitions file it cannot read, and one
naming a top-level item the description it read — a copybook, a definitions
file or a block — does not declare (#27); a file reached through `definitions`
that is not a definitions file; an item reference
naming no item or more than one; a `record` form whose `alternative` children do
not name exactly one alternative per `REDEFINES` its `01`-level carries outside
a repeating group, and one naming an item that is no such alternative; a
reference that
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
occurrence; a `take-alternative` naming an item the copybook does not redefine,
or an alternative it does not declare over those bytes; a variant mixing an arm
chosen by the position of an occurrence with one chosen by its bytes; a schedule
whose arms do not cover 1..*M* exactly once, for *M* the table's declared
maximum under either reading — an occurrence no arm is scheduled for, an
occurrence two arms are or one arm is twice, an arm scheduled for no occurrence
at all, and an occurrence number outside 1..*M*; and a redefine inside a
repeating group that no `discriminate-variant`, `schedule-variant` or
`take-alternative` names.

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

Each fault is reported once, under one rule. A fault a declaration states — an
unknown tag or child, a misplaced or extra argument, a repeated or missing
child, a value outside a closed set, a reference naming nothing — is the schema
check's alone, and is reported in the schema's words. A run checks the layout
against the published schema after the grammar and before any layer reader
reads it, over the whole layout and every form nested in it. When that check
reports anything, the run reports exactly those diagnostics and reads no layer,
so a layer reader never reports a fault a declaration states: a layout carrying
one schema fault and one fault only a reader can find reports the schema fault,
and the other waits for the run after it is fixed. The readers keep their own
checks for a caller that reads a layout without the schema, and on a run what
they report is only what a declaration cannot say.

The cost of the rule is that second run, and it is the cheaper half. The
alternative is one fault reported twice in two vocabularies — a `discriminate`
naming no record would come out once as the schema's reference that names
nothing and again as the reader's record the layout does not define — and a
reader of two messages about one mistake fixes one of them and goes looking for
the other.

### Who reports a fault in an entry body

An [entry body](#definitions) is checked under the same split, by three parties
rather than two, because the third thing an entry needs to know about is
`cobol-go`'s parser rather than a copybook:

- **The schema check** reports every fault a declaration states: an unknown
  tag, a misplaced argument, a missing one, and a second operand where at most
  one is admitted, such as `(sign LEADING SEPARATE SEPARATE)`. Its text is the
  same in a definitions file and in a block, because the declarations are the
  same ([One schema, and a definitions file is one block's
  body](#one-schema-and-a-definitions-file-is-one-blocks-body)).
- **The entry reader** reports what a declaration cannot say: a `word` around a
  spelling that reads as a symbol; a literal text that is not exactly one COBOL
  token; and, in an `occurs`, a `to`, `depending-on` or `indexed-by` written
  twice or a phrase out of [the order the grammar admits
  them](#an-entry-and-one-form-per-clause) — a rule counting one form against
  another, which is the reader's everywhere in this format.
- **The judge** reports an entry `cobol-go` refuses: one that does not read back
  as itself under [the validity
  rule](#cobol-go-judges-an-entry-and-nothing-here-says-what-one-means), with
  the COBOL it printed and `cobol-go`'s message.

Each fault is reported at its span, and once per definitions file or block,
however many records name it. The span is in the definitions file for a fault
in a file, and in the layout for a fault in a block. None of the three needs a
copybook, so all three run before `resolve` touches a record held this way —
a misspelled clause is reported as a misspelled clause and not as whatever it
fails to resolve into, which is the argument the split above makes for a
layout.

### Every diagnostic carries a span, and some carry two

A diagnostic **MUST** name what it found and where, and **MUST NOT** report only
that a layout is invalid. Every one carries a span into the layout source — the
sub-form that is wrong, not the top-level form containing it — which is what
[Positions survive](#tagged-forms-over-s-expressions) buys and what #24's
positioned AST holds.

A diagnostic about something in a record's description carries a **second**
span, into the description and the item (#31). An item reference that does not
resolve, an item that repeats where a value was expected, a record type whose
extent misses `lrecl`, a table with items behind it on a fixed-length dataset:
in every one of those the layout and the description are both part of the
answer, and a diagnostic naming only one of them leaves the reader to find the
other by hand.

The second span points into the record's description **whichever source holds
it**, and names the file the way an adopter can open it:

- a span into a copybook names the copybook, as it always has;
- a span into a definitions file names the file **as the run opened it**, so
  that a reader holding the diagnostic can open the same file the run read;
- a span into a block names **the layout**, as the run opened it, because the
  block is a form of the layout and its entries are lines in that file.

A record moved from a copybook to a file or a block therefore keeps every
diagnostic it had, pointing at the same entry in the place that entry now lives.

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

### A `when` that requires the record it governs

There is no operator saying a record **MUST** follow where an item holds a
value, and no negated guard to build one out of. `when` permits and does not
require, and that is the meaning it keeps ([A `when` permits a record, and never
requires one](#a-when-permits-a-record-and-never-requires-one), #144).

Reason: it takes two additions rather than one, and the first does not pay for
itself even with the second.

The first is a fourth guard test. Requiring a record means making every other
transition out of that point ineligible, which means a test for *the item does
not hold that value* — a negation, and the IR's guard set is three tests and no
fourth ([`ir/SPEC.md`](../ir/SPEC.md#the-automaton-remembers-in-registers)).
Adding a member to a closed set is breaking under
[`ir/SPEC.md`](../ir/SPEC.md#what-breaks-it) whatever else it costs, and those
sets are enumerated before the first release rather than grown afterwards for
the reason that section gives: an old consumer sees an unset choice where a new
one sees a member, and generates code for a file it has silently misread. The
price is a test every generator in every language implements permanently, and
the shape that motivates it has a spelling already wherever the flag is the
record's own discriminating item.

The second is a way to name an edge, and it is what makes the first not enough
on its own. The case that wants requiring most is a flag on a *repeating* record
governing what follows that occurrence, and the transition a negation would have
to exclude there is the repetition's back edge — which no `when` reaches without
also wrapping the record that binds the flag, which the strictly-earlier rule
then refuses ([A guard on a repetition guards every way into its
body](#a-guard-on-a-repetition-guards-every-way-into-its-body)). So a fourth
guard test would have to arrive beside an operator naming transitions rather
than records: a different algebra, and a step toward [A general expression
language](#a-general-expression-language) taken for one shape.

What an adopter writes instead is stated where the loss is — the two record
names where the flag is the discriminating item, and a check of their own beside
the generated reader where it is not.

### The copybook language

A layout now spells a copybook's entries — [Definitions](#definitions) is that
notation — and what it spells is the whole of what it says about the copybook
language. Three things stay out.

**What an entry means is not specified here.** The hierarchy the level numbers
make, what a `PICTURE` or a `USAGE` says, how wide an item is, what `REDEFINES`,
`OCCURS DEPENDING ON` and `SYNCHRONIZED` do to the bytes around them: none of
it is stated in this document, for an entry body or for a copybook.

Reason: they are `cobol-go`'s, cited above, through `copybook.Build` and
`NewLayout`. Restating any of the copybook language would be a second reading of
COBOL for the two repositories to disagree about, which is the outcome
depending on `cobol-go` exists to prevent — and a transcription whose meaning
was restated here could disagree with the copybook it was transcribed from.

**Which entries are valid is not specified here.** No list of levels, usages,
clause combinations or operand kinds is kept in this document or in the schema.

Reason: `cobol-go`'s parser judges an entry ([cobol-go judges an
entry](#cobol-go-judges-an-entry-and-nothing-here-says-what-one-means)). A
second statement of the rules would admit an entry `cobol-go` refuses, or
refuse one it reads, and either would make an entry body mean something other
than the copybook it stands for.

**A copybook's source form is not transcribed.** Its comments, its noise words,
its reference-format columns, and whether an unnamed item said `FILLER`.

Reason: none of them reaches anything `cobol-go` builds, and each is given in
full at [What a transcription leaves out](#what-a-transcription-leaves-out).

`COPY` is part of that language, and it is where a shared header is joined to a
per-type body; [Composition is the
copybook's](#composition-is-the-copybooks-and-copy-is-where-it-is-written) is
why the joining is not a layout form.

`REDEFINES` is part of it too. An entry body transcribes one as `(redefines …)`,
which is the copybook's statement carried over; beyond that, the one thing a
layout says about a redefine is which alternative to read — never what a
redefine is, where its bytes are, or which items overlay which. A redefine whose
alternatives are whole record types is told apart by an ordinary
`discriminate`; one inside a repeating group is told apart by
[`discriminate-variant`](#a-discriminator-for-a-redefine-inside-a-table) or
[`schedule-variant`](#a-schedule-for-a-redefine-chosen-by-position), or is told
to take one alternative by
[`take-alternative`](#every-occurrence-of-a-table-takes-one-alternative). All of
them name items the record's description declared and nothing it did not.

### What the pinned cobol-go does not read

Four constructs a real copybook may hold are not read by the `cobol-go` cpybkc
pins, and so are in no copybook [cpybkc can read
today](#what-a-transcription-leaves-out) and in no entry body. Most positions
refuse them through the parse failure; a condition-name's `VALUE` list does not,
and [A condition-name cobol-go misreads is refused, not
transcribed](#a-condition-name-cobol-go-misreads-is-refused-not-transcribed) is
the rule there.

**`COPY` without a resolver.** A copybook that writes `COPY` is parsed with no
way to find the member it names.

Reason: where a member is found is a question about the files a run reads, and
#407 settles it on the CLI's side. A transcription of a copybook whose `COPY`
was never expanded would be a transcription of part of a record.

**Prefixed literals**, such as `X'00'` and `N'AB'` (Zaba505/cobol-go#140).

Reason: the pinned tokenizer reads the prefix as a word and the rest as a
second literal. Until it reads one token, there is no token for literal text to
hold, and writing the two it reads would transcribe a misreading.

**`ALL` literals**, such as `ALL '*'` (Zaba505/cobol-go#142).

Reason: the same — the pinned parser reads `ALL` as a word beside its literal,
and a transcription can only be as right as the parse it transcribes.

**Words that begin with a digit**, such as `1000-INIT` or `1ST-FIELD`
(Zaba505/cobol-go#141).

Reason: the pinned tokenizer does not read them as words. Each also begins like
a `sexpr-go` number, so when `cobol-go` does read them, the [leaf
rules](#the-leaves-a-level-a-data-name-and-literal-text) will have a second
spelling `sexpr-go` reads as something other than a symbol, and `(word …)` is
already the form that holds one.

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

### Undoing a character-set conversion

A layout says what an item's bytes **are**. It does not say what they were
before something else rewrote them, and there is no way to declare that a byte
item arrived through an EBCDIC-to-ASCII conversion and should be read back
through that conversion's inverse. Adding one is **not** a planned extension.

Reason: the `translated-ebcdic` sign convention looks like the precedent for it
and is not. That value names how a sign is spelled in the file in hand — four
bytes, each with a sign the convention assigns it — so reading one is a lookup
over the small closed set of values the item may legally hold, and the file
supplies every fact the lookup needs. A byte item holds any of 256 values and
has no legal set to look them up in, so undoing a conversion over one is not a
reading but a preimage. It needs the code page the converter used, and it is
defined at all only if that converter was bijective and eight-bit clean, which
the ones that write `?` for a byte they cannot map are not. A layout cannot
state either fact about a file it did not watch being produced, and a
declaration whose correctness rests on a claim nobody can check is the silent
failure the four axes exist to prevent, wearing the clothes of a fix for it.

The adopter's answer is a file rather than a layout. An extract read back
through the code page its converter used — `iconv` is the usual tool — or asked
for again unconverted is the file this format describes, and it is one an
adopter can check against the vendor's manual byte for byte before any layout
reads it. Until then cpybkc reports what the file holds: a region byte delivered
as `0x93` comes back as `0x93`, which is the true statement about the file in
hand and the only one a reader can act on.

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
- **Copybook and definitions file discovery** — where a copybook or a
  definitions file is found and how its path resolves is a CLI concern, not a
  property of the layout.
- **Layout file discovery and composition** — composing one layout's own forms
  from several files is out of scope. Which file a layout's forms are read from
  is the CLI's, and the answer is one file.

  Reason: every form is top-level and refers by name, so a shop's profile in a
  file of its own is the same statement wherever it is read, and pasting it is
  the whole of the reuse it needs. An include form would make a layout's meaning
  depend on files it reaches through other files, which is the transitive
  import [What a record may reach](#what-a-record-may-reach) refuses for
  definitions too. A definitions file a record names is not composition: it
  holds one record's description, not a layout's forms, and nothing in it can
  name a further file.

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

A second file of the same shop's, delivered through an EBCDIC-to-ASCII
conversion rather than as the dataset it was written to, is [A converted file,
end to end](#appendix-a-converted-file-end-to-end). It is a different file with
its own copybooks and its own records — what carries across is the vocabulary,
and the four encoding lines are where the two disagree.

## Appendix: A converted file, end to end

A settlement file — a different file from the one above, from the same shop —
delivered as a converted extract rather than as the dataset it was written to: a
header, a counted run of details, and a trailer.

It is here because of what an adopter holding one arrives with. A copybook says
`PIC S9(5)`, and a reader shows them `1234E` — or `1234{` for a value ending in
a positive zero and `1234}` for a negative one. A field that should be a number
reads as a letter or as punctuation. Nothing is corrupt, nothing is misaligned,
and the character is not a delimiter: it is the sign, and the four lines at the
top of this layout are what say so.

This is also the combination [All four, always, with no default for
any](#all-four-always-with-no-default-for-any) calls the one real files hit most
often. No compiler produces it — ASCII characters, EBCDIC signs and mainframe
byte order arrive together only by way of a conversion — and it is expressible
at all because the four axes are stated separately.

```
;; The encoding profile — a property of this *file*, not of the mainframe that
;; wrote it. The transfer rewrote the characters and left everything else as it
;; found it, and each of the four lines below records one half of that.
(encoding
  (charset ascii)
  (sign-convention translated-ebcdic)
  (byte-order big-endian)
  (float-format hfp))

;; The conversion was copybook-aware and skipped this item, which holds a
;; region code — one byte of any of 256 — rather than characters.
(encoding-override (item SETTLE-DETAIL SD-REGION)
  (charset none))

;; A block the sending system reads back as it wrote it, so the transfer was
;; told to leave it alone. It is the only EBCDIC left in the file, characters
;; and signs both.
(encoding-override (item SETTLE-HEADER SH-PASSTHROUGH)
  (charset cp037)
  (sign-convention ebcdic))

;; The dataset the extract came off, out of the JCL that allocated it. The
;; conversion rewrote character data inside each record and moved no record
;; boundary, so these three numbers are the dataset's and are what they were.
(framing
  (recfm VB)
  (lrecl 512)
  (blksize 27998))

;; The compiler that wrote it.
(copybook-reading
  (occurs-depending-on odoslide))

;; The record types.
(record SETTLE-HEADER  (copybook "cpy/settle.cpy" SETTLE-HEADER-REC))
(record SETTLE-DETAIL  (copybook "cpy/settle.cpy" SETTLE-DETAIL-REC))
(record SETTLE-TRAILER (copybook "cpy/settle.cpy" SETTLE-TRAILER-REC))

(rename (item SETTLE-DETAIL SD-AMT) "SettlementAmount")

;; How to tell them apart. The type byte is a character, so it is ASCII here
;; and would have been EBCDIC in the dataset — the literal is written as the
;; file spells it and the charset axis is what resolves it.
(discriminate SETTLE-HEADER  (equals (item SETTLE-HEADER SH-REC-TYPE) "H"))
(discriminate SETTLE-DETAIL  (equals (item SETTLE-DETAIL SD-REC-TYPE) "D"))
(discriminate SETTLE-TRAILER (equals (item SETTLE-TRAILER ST-REC-TYPE) "T"))

;; The order they may appear in.
(sequence
  (seq SETTLE-HEADER
       (times SETTLE-DETAIL (item SETTLE-HEADER SH-DETAIL-COUNT))
       (? SETTLE-TRAILER)))
```

### Why the field reads as punctuation

An EBCDIC signed zoned decimal carries its sign in the zone nibble of its last
byte: `0xC5` is a positive 5 and `0xD5` a negative one, and `0xC0` and `0xD0`
are a positive and a negative zero. A text conversion rewrites those bytes along
with every other byte of character data, and cp037 and cp1047 both send `0xC5`
to `E`, `0xD5` to `N`, `0xC0` to `{` and `0xD0` to `}`. The digits come through
unharmed, because `0xF0`–`0xF9` are `0`–`9` on the other side; the sign does
not, because there was no ASCII character for it to become.

`translated-ebcdic` is that outcome named, and it is why the sign convention is
an axis of its own rather than something the charset implies. `ascii` on its own
would say the digits are ASCII and leave the sign byte to be read as the letter
it now looks like; `ebcdic` on its own would say the file was never converted.
The file in hand is neither, and only two axes stated separately can say so.

A `"` where a number was expected is a different finding and a worse one. `0x7F`
is an ordinary byte in a `COMP` item holding 127 and in a `COMP-3` item ending
in an unsigned 7, and it is `"` in both cp037 and cp1047 — so punctuation
appearing inside a *binary* or *packed* field is the fingerprint of a conversion
that rewrote bytes which were never characters. No setting of these four axes
describes that file, and [Undoing a character-set
conversion](#undoing-a-character-set-conversion) is why. The extract above came
off a copybook-aware conversion, which is what makes its packed and binary items
byte for byte what the mainframe wrote.

### What differs from the native layout

[A layout, end to end](#appendix-a-layout-end-to-end) is a different file — an
order file, its own copybooks, its own record names — and it is worth saying so
rather than leaving a reader to diff the two and find it. What the two share is
the *shape*: a header, a counted run of details, an optional trailer, told apart
by a type item, sequenced by the same operators, and framed by the same
`recfm`/`lrecl`/`blksize` vocabulary out of the JCL that allocated each dataset.
Neither layout says anything about the other's records, and nothing here is a
statement that carries between them.

What differs is the `encoding` form and the overrides under it, and that
difference does not follow from anything else either layout writes. Both files
came off the same kind of dataset and both are described with the same forms;
one arrived as the mainframe holds it and one arrived through a conversion, and
the only place a layout can say which is those four lines. That is the claim
[All four, always, with no default for
any](#all-four-always-with-no-default-for-any) makes — the four axes are
independent and are not a dialect flag — shown twice rather than argued once.

The overrides are the mirror of each other. There, a native EBCDIC file carries
a single field that arrived from a partner in ASCII and was never converted.
Here, a converted ASCII file carries the two the conversion did *not* reach: one
whose bytes are a payload and have no charset at all, and one the transfer was
told to pass through, which is still EBCDIC characters with EBCDIC signs. In
both directions the exception is per item, both are stated with the same form,
and neither is a second profile.

## Appendix: A definitions file

`posting.cpy.sexpr`, the entries of a vendor's posting copybook that its licence
keeps out of the repository, in the [printed form `init`
writes](#the-printed-form-init-writes). The record carries a type code with two
condition-names, a body redefined two ways, a small fixed table and a filler to
an 80-byte record.

```
(entry 01 POSTING-RECORD)
  (entry 05 PST-TYPE (pic "X"))
    (entry 88 PST-IS-DEBIT (value "'D'"))
    (entry 88 PST-IS-CREDIT (value "'C'"))
  (entry 05 PST-ACCOUNT (pic "X(10)"))
  (entry 05 PST-DEBIT)
    (entry 10 PST-DR-AMOUNT (pic "S9(9)V99") (usage COMP-3))
    (entry 10 PST-DR-REASON (pic "X(4)") (value SPACES))
  (entry 05 PST-CREDIT (redefines PST-DEBIT))
    (entry 10 PST-CR-AMOUNT (pic "S9(9)V99") (usage COMP-3))
    (entry 10 FILLER (pic "X(4)"))
  (entry 05 PST-REFS (occurs "3"))
    (entry 10 PST-REF (pic "X(5)"))
  (entry 05 FILLER (pic "X(44)"))
```

Every line is one entry and every entry is a top-level form; the indentation is
the [depth rule](#the-printed-form-init-writes) and nothing reads it. The two
88s sit one deeper than `PST-TYPE`, the nearest entry before them that is not an
88. `(redefines PST-DEBIT)` is the copybook's own statement, carried over, and
what it does to the bytes is `copybook.Build`'s; which of the two alternatives a
record is, is said in the layout, by the `alternative` children the [next
appendix](#appendix-definitions-held-three-ways) writes. The literal text
`"'D'"` holds the COBOL token with its apostrophes, and `SPACES` is a data-name,
as every COBOL word is.

## Appendix: Definitions held three ways

A ledger file whose three kinds of record are described three ways, record by
record: the header by the shop's own copybook, which is committed; the postings
by the definitions file above, because their copybook cannot be; and the
trailer, three items long, by a `definitions-block` in the layout itself. The
record forms name their sources in the spelling [A record's
source](#a-records-source-a-copybook-a-definitions-file-or-a-block) settles.

```
(encoding
  (charset cp037)
  (sign-convention ebcdic)
  (byte-order big-endian)
  (float-format hfp))

(framing
  (recfm FB)
  (lrecl 80))

;; The shop's own copybook, committed beside the layout.
(record LEDGER-HEADER (copybook "cpy/ledger-header.cpy" LEDGER-HEADER-REC))

;; The vendor's copybook cannot be committed, so its entries are a definitions
;; file. One 01-level, redefined two ways, and a record for each alternative.
(record DEBIT-POSTING
  (definitions "posting.cpy.sexpr" POSTING-RECORD)
  (alternative (item DEBIT-POSTING PST-DEBIT)))

(record CREDIT-POSTING
  (definitions "posting.cpy.sexpr" POSTING-RECORD)
  (alternative (item CREDIT-POSTING PST-CREDIT)))

;; Three items, written where they are used.
(definitions-block TRAILER
  (entry 01 LEDGER-TRAILER)
    (entry 05 LT-TYPE (pic "X"))
    (entry 05 LT-POSTING-COUNT (pic "9(7)"))
    (entry 05 FILLER (pic "X(72)")))

(record LEDGER-TRAILER (definitions TRAILER LEDGER-TRAILER))

(discriminate LEDGER-HEADER  (equals (item LEDGER-HEADER LH-TYPE) "H"))
(discriminate DEBIT-POSTING  (equals (item DEBIT-POSTING PST-TYPE) "D"))
(discriminate CREDIT-POSTING (equals (item CREDIT-POSTING PST-TYPE) "C"))
(discriminate LEDGER-TRAILER (equals (item LEDGER-TRAILER LT-TYPE) "T"))

(sequence
  (seq LEDGER-HEADER
       (* (alt DEBIT-POSTING CREDIT-POSTING))
       LEDGER-TRAILER))
```

Nothing after the record forms knows which source a record was described by. An
item reference is rooted at a record name and reaches into its description the
same way whether that is a copybook, a file or a block, so the discriminators
and the sequencing read exactly as they would over three copybooks. That is the
point of the split: where the entries are held is a fact about the record's
source and about nothing downstream of it.

The block is in the canonical printed form for a block — the file's form, two
spaces further in, opening on its own line and closing on its last entry's
line. Its name, `TRAILER`, is the layout's own identifier and not a COBOL name;
`LEDGER-TRAILER` after it, in the record form, is the `01`-level inside it. The
two postings name one file, and a run reads it once ([What a record may
reach](#what-a-record-may-reach)). The block sits before the record naming it
here and could as well sit after it. A run reads the layout,
`cpy/ledger-header.cpy` and `posting.cpy.sexpr`, and opens nothing for the
trailer.

## Appendix: Mapping to Stories

| Section | Implemented by |
|---|---|
| [The surface syntax](#the-surface-syntax) | #22 `layout`; the data-name sort in an item reference, and the delegated refusals governing a layout's own forms while an entry body follows the entry rules, by #395 against discussion #394 |
| [The encoding profile](#the-encoding-profile) | #25 `layout`; an item that carries bytes rather than text, and the conversion residue left out beside it, by #275; a worked example of the converted file the section calls the most common, by #273; why the binary width staircase `codec/SPEC.md` declares fifth is not one of these four, by #293; that a form whose only content is its presence is refused among a layout's own forms and not in an entry body, by #395 |
| [Physical framing](#physical-framing) | #26 `layout` |
| [Record definitions](#record-definitions) | #27, #30 `layout`; `copybook-reading` by #35 `resolve`; which alternative a `record` form is, a rename naming a record, and a rename being per record, settled by #164; that an alternative is chosen the same way whichever source holds a record's entries, the data-name sort in the copybook's top-level name, and the [prohibition on derived quantities](#a-layout-states-relationships-and-sizes-and-nothing-derived) governing a layout's own forms, by #395 against discussion #394; a record's source — a copybook, a definitions file or a block — what a record may reach, block names and their faults, the one shape of a missing description or item across the three sources, composition carried to a file or a block, and the three sources resolving to byte-identical descriptors, settled by #399 against discussion #394; the `definitions-block` top-level form declared by #400 against discussion #394; the `definitions` form declared and read by #402, and resolved by #405 for a file and #409 for a block |
| [Definitions](#definitions) | the notation for an entry body, held in a definitions file or a `definitions-block`, its leaf rules, its canonical printed form, what it leaves out, and the refusal of a condition-name `cobol-go` misreads, settled by #395 against discussion #394; the schema declarations, the check of a definitions file as one block's body, the entry reader, and a run refusing a block at its tag until #409 resolves one, by #400 against discussion #394, the absent `at-most-one` argument by #396, the converter, the judge and the round trip over every copybook by #401 against discussion #394, a record naming a file or a block by #399 and #402, how a definitions file and a layout are told apart, and what each is reported as when reached the wrong way, by #399 against discussion #394, and the data-name sort in a layout's item positions by #404 |
| [Discrimination](#discrimination) | #28 `layout`; an arm's and a `take-alternative`'s alternative written as a data-name, by #395 against discussion #394; the strategies lowered into IR predicates, the literals resolved to bytes, and the rules on a target that need a copybook, by #37 `resolve`; that neither a strategy nor the order two are written in becomes a default arm, refused by #324 and reopened by #332 for the batch shape whose two discriminators read runs sharing no byte, against discussion #323; the schedule that settles a variant by the position of an occurrence rather than by its bytes, added by #346 against discussion #340; the spelling for a redefine every occurrence of which takes one alternative, which is not a variant at all, added by #341; what a literal's bytes are once a consumer may read under axes other than the layout's, and what `(bytes …)` says then, by #379 against discussion #378 |
| [Sequencing](#sequencing) | #29 `layout`; the expression compiled to an automaton, and the rules on `times` and `when` that need a copybook, by #36 `resolve`; what a `when` does and does not require, and where a guard lands on a repetition, settled by #144 against the compiler #36 had already produced |
| [The published schema](#the-published-schema) | #23 `layout`; one schema for a layout and an entry body, a definitions file checked as one block's body, and everything `cobol-go` judges left outside it, by #395 against discussion #394, declared in `schema/layout.sexpr` and the definitions-file check added by #400 against discussion #394; an `at-most-one` argument that may be left out, and the argument-order rules the loader enforces, by #396 for #394; the `record-source` sort and `definitions-origin`, stated by #399 against discussion #394 and declared by #402 |
| [Validation and diagnostics](#validation-and-diagnostics) | #24, #31 `layout`; who reports a fault in an entry body — the schema check, the entry reader or the judge — and where its span points, by #395 against discussion #394; the schema check run before any layer reader, and each fault reported once under one rule, by #397; the schema check's and the entry reader's halves of an entry body's faults, with one text in a file and in a block, by #400 against discussion #394; a record's second span pointing into its description whichever source holds it, a file named as the run opened it and a block as the layout, by #399 against discussion #394 |
