# The Conformance Corpus Format

## Overview

Nothing in cpybkc reads a data file. It is strictly a code generator, so the
only thing that ever decodes a record is the code a generator emitted, in
whatever language it emitted it — and nothing in a descriptor obliges two
generators handed the same bytes to agree about what those bytes hold. The
conformance corpus is the mechanism that holds them to each other: a set of
small files with the right answer written down, so that a generator written by
somebody who has never seen this repository can be asked the same question
`cpybkc-gen-go` is asked, and the two answers compared without a decoder in the
middle.

This document specifies the files that question and its answer are written in:
what an entry is made of, the value language a decoded record is written in, and
the answer document a runner writes on standard output. It is the half a third
party implements. It is deliberately language-neutral throughout — a comparison
of two answers needs neither the descriptor nor a decoder, which is what makes a
runner for a language this repository has never seen comparable by exactly the
same rules as its own.

What a value *means* is not here. How wide a `PIC S9(5) COMP-3` item is, which
nibble carries its sign, where one record ends and the next begins, and which
record type a run of bytes is are the [resolved IR](../ir/SPEC.md)'s and, below
it, [`cobol-go`'s `codec/SPEC.md`][codec-spec]'s. How a generator is found and
invoked is the [plugin contract](../plugin/SPEC.md)'s. Which entries the corpus
holds, why each of them is there and how to add one are the corpus's own, in
[`testdata/conformance/README.md`](../../testdata/conformance/README.md), which
is where the corpus documents itself.

What the bytes mean belongs there; how the answer to "what do they decode to" is
written down belongs here.

[codec-spec]: https://github.com/Zaba505/cobol-go/blob/main/codec/SPEC.md

### Scope

In scope: the directory an entry is, the members it holds and the one member
that is reserved; the value language every decoded record is written in, down to
the admissible spelling of each scalar; how a file the generated reader refused
is reported; what a runner is asked to do and what it is deliberately not asked
to do; and the answer document it writes.

Out of scope, with reasons, in [Out of Scope](#out-of-scope).

### Governing sources

- **[`ir/SPEC.md`](../ir/SPEC.md)**, *The resolved IR* — normative for the
  descriptor an entry carries and for every node kind the value language has to
  write down. The value language names groups, tables, variants and slack; what
  each of those *is* is defined there, and this document never restates one.
- **[`cobol-go`'s `codec/SPEC.md`][codec-spec]**, *the codec specification* —
  normative for what a field's bytes decode to, which is the answer an entry
  writes down. Its Appendix A is where most of the corpus's entries are derived
  from, and an entry that disagrees with it is a wrong entry.
- **RFC 8259**, *The JavaScript Object Notation (JSON) Data Interchange Format*
  — normative for every document specified here. It is what fixes that a member
  name is a string, that a document is one value, and what an implementation may
  assume about a JSON number, which is why the value language uses so few of
  them. <https://www.rfc-editor.org/rfc/rfc8259>
- **RFC 5234**, *Augmented BNF for Syntax Specifications: ABNF*, and **RFC
  7405**, *Case-Sensitive String Support in ABNF* — the notation the two
  grammars below are written in, and the reason they are written with `%s`
  literals: 5234's default is case-insensitive, and every spelling rule here is
  case-sensitive because the comparison is string equality.
  <https://www.rfc-editor.org/rfc/rfc5234> <https://www.rfc-editor.org/rfc/rfc7405>
- **RFC 4648**, *The Base16, Base32, and Base64 Data Encodings* — normative for
  the base64 form, and specifically for section 4's alphabet as against section
  5's. A run of bytes needs one spelling and this is where it is defined.
  <https://www.rfc-editor.org/rfc/rfc4648>
- **ISO/IEC 9899:2018**, *Programming languages — C*, 6.4.4.2 and the `%a`
  conversion of 7.21.6.1 — the hexadecimal floating notation a floating-point
  item is written in. Cited because the notation is C's rather than this
  project's: a form invented here would be one no implementation's library
  already reads or writes.
  <https://www.iso.org/standard/74528.html>
- **IEEE 754-2019**, *Standard for Floating-Point Arithmetic* — normative for
  what a `COMP-1` or `COMP-2` item holds under the IEEE profile, and for the
  distinction between a positive and a negative zero that the float form is
  required to preserve. <https://standards.ieee.org/ieee/754/6210/>
- **IBM z/Architecture Principles of Operation**, *Hexadecimal-Floating-Point
  Instructions* — normative for what the same item holds under the HFP profile.
  It is cited here for one property: HFP long carries a 56-bit fraction where an
  IEEE double carries 53, so a form that could only spell a double would be a
  form that cannot spell every value this corpus is about.
  <https://www.ibm.com/docs/en/zos/3.1.0?topic=set-hexadecimal-floating-point-instructions>

> **Ambiguity:** the sources do not overlap, and this document resolves nothing
> between them. Where it appears to contradict [`ir/SPEC.md`](../ir/SPEC.md)
> about what a descriptor carries, that document wins and this one has a bug;
> where it appears to contradict [`codec/SPEC.md`][codec-spec] about what bytes
> decode to, that document wins for the same reason. This document specifies how
> an answer is written down and never what the answer is — the one place the two
> could collide is a spelling rule that made a correct answer unwritable, and
> that is a defect in this document by definition.

### Conformance language

**MUST**, **MUST NOT**, **SHOULD** and **MAY** are normative requirements on a
corpus entry, on a runner, and on a harness that loads or compares either,
interpreted as described in [CONVENTIONS.md](../CONVENTIONS.md). Everything else
is descriptive.

## An entry

An entry is one directory, named for what it is about. The name is what every
diagnostic about the entry quotes, so it is the entry's identity and nothing
else is.

| File | What it is | Required |
|---|---|---|
| `entry.json` | What the entry is about, and the section its expected answer came from. | yes |
| `layout.sexpr` | The [layout](../layout/SPEC.md) the file is laid out by. | yes |
| `*.cpy` | The copybooks that layout names, at the paths it spells them. | one or more |
| `ir.json` | The [IR](../ir/SPEC.md) the layout and those copybooks resolve to. | yes |
| `input.bin` | The bytes of one file laid out that way. | yes |
| `values.json` | What those bytes decode to. | yes |
| `offsets.json` | Reserved. See [below](#offsetsjson-is-reserved). | no |

An entry directory **MUST** hold each required member, under exactly the name
above, and at least one file whose name ends in `.cpy`. It **MAY** hold
`offsets.json`. It **MUST NOT** hold any other file, and it **MUST NOT** hold a
subdirectory (#66).

A file the format has no place for is refused rather than ignored, which is the
rule the [project manifest](../plugin/SPEC.md) already applies to an unknown
field: a file somebody added to an entry in the expectation that something reads
it is worse than one whose author is told nothing does.

The member names are fixed by this document rather than declared in `entry.json`
because a tuple whose members could be anywhere is a tuple every reader has to
be told about, and a third-party runner should be able to open a file without
parsing something else first.

### `entry.json`

```json
{
  "description": "A fixed-length dataset of one record type: ...",
  "source": "cobol-go codec/SPEC.md, \"Storage Widths\"; docs/ir/SPEC.md, \"Physical framing\""
}
```

Both members are required, both are non-empty strings, and there is no third
member. An unknown member **MUST** be refused.

`source` is what a failing run prints beside the entry's name. Whoever reads
that report has to decide whether the generator is wrong or the entry is, and
that decision starts at where the expected answer came from — so an entry
recorded from what a generator printed, rather than derived from a
specification, is an entry that passes forever including through the bug it was
written to catch.

### `layout.sexpr` and the copybooks

The layout is read by [`layout/SPEC.md`](../layout/SPEC.md)'s rules and the
copybooks by the COBOL standard's; neither is restated here. Which copybooks the
layout names is the layout's business. What this document requires is only that
the files it names are in the directory beside it, at the paths it spells them,
so that an entry is self-contained: a runner clones the corpus and nothing else.

### `ir.json`

The descriptor, in the canonical JSON rendering — protobuf's own field names,
field-number order, two-space indent — which is what `cpybkc --emit-ir
--emit-ir-format json` writes and what [the rendering is
normalized](../ir/SPEC.md#the-rendering-is-normalized) specifies.

`ir.json` **MUST** be a descriptor that is valid under
[`ir/SPEC.md`](../ir/SPEC.md), and it **MUST** be byte for byte the canonical
rendering of the descriptor it carries. An entry is written and reviewed by
hand, so it is reviewed as a diff of its content; a rendering that varied would
make every review of an entry a review of its whitespace.

It is JSON rather than the binary form for the same reason: an entry is
authored by a person. A runner that would rather have the bytes a plugin is
handed — which is what [`plugin/SPEC.md`](../plugin/SPEC.md) says a generator
receives — encodes what it read, and any protobuf runtime turns one into the
other with the schema every release attaches.

The descriptor is hand-authored, which is what makes it an oracle rather than a
recording of what `resolve` happens to do today. An entry therefore states two
independent things: that the layout and its copybooks resolve to this
descriptor, which is a claim about a *producer*; and that this descriptor,
handed to a generator, produces code that decodes `input.bin` into
`values.json`, which is a claim about a *consumer*. Carrying both is what lets a
failure be attributed, because an author who disagrees with cpybkc about what a
layout means and one who disagrees about what a descriptor means have different
bugs.

### `input.bin`

The bytes of one file laid out the way the layout says. A file of no bytes is
admitted: an empty file is a file, and a layout whose sequencing expression
accepts nothing is exactly what one entry ought to be about.

### `values.json`

What those bytes decode to, in the value language below.

```json
{
  "records": [
    {
      "name": "ORDER-RECORD",
      "value": {
        "ORDER-ID": "A001",
        "ORDER-QTY": "42",
        "ORDER-LINE": [
          {"LINE-SKU": "SK1"},
          {"LINE-SKU": "SK2"}
        ]
      }
    }
  ]
}
```

A values document **MUST** carry `records`, an array of the records read in file
order, and **MAY** carry `failure`. It **MUST NOT** carry any other member, and
a reader **MUST** refuse one — a key an author wrote expecting it to mean
something is a typo, and a document that ignores it passes for the wrong reason.

Each record carries `name` and `value`, and both are required. `name`
**MUST NOT** be empty. `value` **MAY** be `""` or `{}` — an all-space
alphanumeric item and a group holding nothing but slack decode to exactly those,
so a rule refusing an empty value would refuse an answer the language elsewhere
requires.

`name` **MUST** be the record's name as the copybook spells it — the `original`
of the record node's [names](../ir/SPEC.md#names), never an identifier a
generator munged out of it. Two generators munge differently and a corpus keyed
to one of them would be a corpus only that one can pass.

`value` is what the record's top-level node holds, written as [the value
language](#the-value-language) says.

### `offsets.json` is reserved

An entry **MAY** carry a file named `offsets.json`, and a loader **MUST NOT**
refuse an entry that carries one. Its content is **not specified by this
document**, nothing reads it, and no entry in the corpus carries one. An entry
**SHOULD NOT** carry one until a story specifies what is in it.

The name is reserved now because reserving it is free now. Adding a member to a
published format is a coordinated change across every runner that validates an
entry's listing, and this format is still private; a generator that never opens
`input.bin` — a diagram, a schema translation, a documentation generator — runs
the same position sum a decoder runs and gets it wrong in the same way, so there
is a plausible second oracle here whose shape is an offset table. Whether the
corpus is where that oracle belongs is open, and that discussion is #193's, not
this document's.

## The value language

Every decoded value is JSON, and every scalar is a JSON string. The language is
small on purpose: the comparison of two answers is then structural, needs
neither the descriptor nor a decoder, and can be run by a harness that has never
heard of COBOL.

### Which form a value takes is decided by the descriptor

For an elementary item, the form is a function of the descriptor's `usage` and
`category` alone, in this order (#66):

1. `USAGE_COMP_1` or `USAGE_COMP_2` — [a floating-point
   item](#a-float-is-written-exactly-and-never-as-a-json-number).
2. `USAGE_INDEX`, `USAGE_POINTER` or `USAGE_NATIONAL` — [a run of
   bytes](#index-pointer-and-national-are-base64).
3. Otherwise `CATEGORY_NUMERIC` — [a number](#a-number-is-a-decimal-string).
4. Otherwise — `CATEGORY_ALPHABETIC`, `CATEGORY_ALPHANUMERIC`,
   `CATEGORY_NUMERIC_EDITED` or `CATEGORY_ALPHANUMERIC_EDITED` —
   [characters](#characters-and-why-trailing-spaces-do-not-survive).

Usage is read before category because the two floating-point usages carry
`CATEGORY_NUMERIC` and are not written as numbers, and because a national item
is characters in a character encoding this format does not decide. An
implementation that keyed on category alone would write a `COMP-1` as decimal
digits and be wrong for every value with a fraction.

### A group, a table and a variant

| The node | Written as |
|---|---|
| A group | An object, one member per node it holds, keyed by the copybook's name for that node. |
| An item that repeats | An array, one element per occurrence, in file order. |
| A variant | The one arm the occurrence carries, written as a member of the *enclosing* object; the variant contributes no key of its own, and an arm the occurrence does not hold has no key at all. |
| A slack node | Nothing at all. |

A repeating node is an array whether it is a group or an elementary item, and
whether it occurs a fixed number of times or a variable one; the array's length
is the number of occurrences the file actually holds. A repeating node
**MUST NOT** be written as anything but an array, including where it happens to
hold one occurrence — a value language in which a table of one and a scalar look
the same is one in which a generator that lost a table passes.

A variant occurrence carries exactly one arm, which is what [a variant is chosen
once per occurrence](../ir/SPEC.md#a-variant-is-chosen-once-per-occurrence)
requires. An arm the occurrence does not hold **MUST** have no key, rather than
a key whose value is `null`: a comparison then reports an unheld arm as the
selection difference it is, instead of as a value that differs.

The arms are members of the enclosing group's object and the variant's own name
appears nowhere in the document. That is a decision rather than an omission —
the name a copybook gives a variant is not a name the record's data has, and a
key for it would be a level of nesting no other generator could be expected to
invent identically. Its cost is stated rather than hidden: two arms of the same
name in one group — whether in one variant or in two — would be one key, and an
entry whose copybook does that is one this format cannot write down. No entry
does, and an entry that needed to would be the case for revisiting this
decision rather than for working around it.

### Slack is not a value

A slack node **MUST NOT** appear in a values document. Its bytes travel with the
record — [slack survives a read](../ir/SPEC.md#slack-survives-a-read) is what a
reader does with them and a writer puts them back — but they are not something
anybody decoded, and a value language that surfaced them would be asking two
generators to agree about padding rather than about data.

### Characters, and why trailing spaces do not survive

An item in any of the four character categories — `CATEGORY_ALPHABETIC`,
`CATEGORY_ALPHANUMERIC`, `CATEGORY_NUMERIC_EDITED` and
`CATEGORY_ALPHANUMERIC_EDITED` — is written as a JSON string of its characters,
after the file's charset has been applied, with every trailing space removed
(#66).

- A writer of a values document **MUST** remove every trailing U+0020 SPACE.
- It **MUST NOT** remove a leading or an interior space.
- An item holding nothing but spaces is therefore written `""`.
- It **MUST NOT** remove any other character — not a tab, not a NUL, not a
  character that a charset happens to render as blank.

Trailing spaces are how COBOL pads an alphanumeric item to its declared width,
and the padding is not data: `HDR-NAME PIC X(15)` holding `BATCH-0001` and five
spaces holds a ten-character name in a fifteen-character field, and every
generator that keeps the padding would have to agree with every other about a
detail none of them was told. The round trip is unaffected, because a writer
pads back out to the declared width from the value it was given.

The cost is stated rather than hidden: an item whose data genuinely ends in a
space cannot be told apart from one that was padded, and this format has no way
to write that item down. That is accepted. Space padding is universal in the
files this corpus is about, and a rule that preserved the padding would make
every entry's expected value depend on a width the value language does not
carry.

The rule covers the two **edited** categories as well, and that part is a
decision rather than a consequence of the argument above: a trailing blank in an
edited item can be produced by the PICTURE rather than by padding — the sign
position of `PIC 999-` holding a positive value, or an inserted `B` at the end
of the string. It is included anyway because the alternative is worse. A runner
would have to consult the category to know whether to trim, two implementations
would have to agree about which edit characters count as data, and the
distinction would be invisible in the file: an edited item padded to its width
and one whose edit ends in a blank are the same bytes. No entry in the corpus
holds an edited item today, so nothing changes; an entry that needs the trailing
blank of an edit to be visible is the case for reopening this, and it can state
the bytes in `input.bin` meanwhile.

The rule is stated in terms of the *characters*, not of the bytes, so it holds
identically for an EBCDIC file whose pad byte is `0x40` and an ASCII one whose
pad byte is `0x20`.

### A number is a decimal string

A numeric item — zoned, packed, `COMP-6`, binary or `COMP-5` — is written as a
JSON string of its decimal digits (#66).

```abnf
number  = "0" / [ "-" ] NONZERO *DIGIT
NONZERO = %x31-39   ; 1-9
```

- It **MUST NOT** be written as a JSON number. JSON numbers are doubles in most
  readers, and a `PIC S9(18)` item holds values a double cannot represent.
- It **MUST NOT** carry a leading `+`. One spelling per value, and the negative
  case already needs its sign.
- It **MUST NOT** carry a leading zero. `"07"` and `"7"` are the same value and
  a format that admitted both would compare two correct generators as
  disagreeing.
- It **MUST NOT** be `"-0"`. Zero is written `"0"` whatever sign the bytes
  carry, and a packed `00 0D` — a legitimate negative zero as a byte pattern —
  decodes to `"0"`.
- It **MUST NOT** carry a decimal point, an exponent, a space, a grouping
  separator, or any character outside the grammar above.
- The digits are the **unscaled** ones. An implied decimal point is not applied,
  for the same reason a generated accessor does not apply one: the point is in
  the descriptor, and applying it here would apply it twice by the time a caller
  saw the value.

Negative zero is excluded rather than admitted because a COBOL numeric item does
not have one. The standard makes the sign of zero insignificant — arithmetic
produces an unsigned zero and a comparison of `-0` against `+0` is equality — so
`"-0"` would be a spelling of a *byte pattern* rather than of a value, and two
generators would be compared on which sign nibble their source file happened to
carry. It would also make the writing direction unpassable: a writer given zero
emits the positive sign nibble its convention prescribes, so an entry demanding
`"-0"` back would demand bytes no correct writer produces.

An entry that is *about* a negative-zero byte pattern is still perfectly
writable. The pattern goes in `input.bin`, and what it decodes to is `"0"`.

### A float is written exactly, and never as a JSON number

A `COMP-1` or `COMP-2` item is written as a JSON string: either one of three
sentinels, or the exact value in hexadecimal significand notation (#194, #195).

```abnf
float       = %s"NaN" / infinity / hex
infinity    = [ "-" ] %s"Infinity"
hex         = [ "-" ] %s"0x" significand %s"p" sign exponent
significand = "0" / ( "1" [ "." 1*LOWHEX ] )
sign        = "+" / "-"
exponent    = "0" / NONZERO *DIGIT
LOWHEX      = DIGIT / %x61-66   ; 0-9 a-f
NONZERO     = %x31-39           ; 1-9
```

Every literal above is case-sensitive, in the `%s` notation RFC 7405 adds to
ABNF, because the default in RFC 5234 is case-*in*sensitive and a comparison
that is string equality cannot afford `0X1P+3` and `0x1p+3` to be two spellings
of one value.

The value is significand × 2^exponent, where the significand is read as a
hexadecimal fraction. `0x1p+0` is 1, `0x1.2p+3` is 9, `0x1p-5` is 0.03125, and
`-0x0p+0` is negative zero.

One value has one spelling, which is what lets a comparison be string equality:

- `x`, `p` and every hexadecimal digit **MUST** be lowercase, and the two
  sentinels are spelled exactly `NaN` and `Infinity`.
- The significand **MUST** be `0` if and only if the value is zero, and `1`
  otherwise. Every non-zero value, including a subnormal one, is normalized.
- The fraction **MUST NOT** end in `0`, and the `.` **MUST** be absent when
  there is no fraction.
- The exponent's sign **MUST** be written, even where it is `+`, and the
  exponent **MUST NOT** carry a leading zero.
- A zero exponent **MUST** be written `+0`; `p-0` is not admissible.
- Where the significand is `0` the exponent **MUST** be `+0`, so that zero is
  exactly `0x0p+0` and negative zero exactly `-0x0p+0`. A grammar that let the
  exponent of a zero vary would give one value the infinitely many spellings a
  stored exponent can take, and two correct generators would be reported as
  disagreeing about zero — the failure `"-0"` is excluded from [a
  number](#a-number-is-a-decimal-string) to avoid.
- `"NaN"` is every NaN. Its sign and its payload are **not** distinguished.
- A value **MUST NOT** be written as a JSON number, and **MUST NOT** be written
  in decimal at all.

Four things break under a JSON number, and each is why this form exists. NaN and
the infinities are not JSON numbers and most writers refuse them outright, which
turns a generator's correct answer into a harness that could not write a
document. `-0.0` and `0.0` compare equal under IEEE equality, so a generator
that lost the sign of a zero passes silently. An authored decimal like `0.1` is
not a `COMP-1` value at all, so every correct implementation would have to write
`0.10000000149011612` and an entry's author would have to compute it. And HFP
long carries a 56-bit fraction where an IEEE double carries 53, so there are
values a correct HFP decoder produces that no double spells — a form that went
through a double would be a form that cannot state them.

The notation is C's hexadecimal floating constant, which is `%a` in `printf`,
`strconv.FormatFloat(f, 'x', -1, 64)` in Go, `float.hex()` in Python and
`Double.toHexString` in Java. Every one of those spells the same values and none
of them spells them in exactly the canonical form above — Go pads the exponent
to two digits, Python writes a fixed-width fraction, Java omits the `+` — so an
implementation normalizes its library's output rather than printing it. That is
a few lines in any language, and the alternative was a form whose parser nobody
already has.

Hexadecimal rather than base64 of the stored bytes, which was the other
candidate: base64 of the bytes states what the *file* holds, and what an entry
has to state is what a *reader made of them*. Under
[`float-hfp-read-as-ieee`](../../testdata/conformance/float-hfp-read-as-ieee)
one run of bytes is read two ways on purpose — as HFP it is 1, as IEEE it is 9 —
and a form that echoed the bytes would make both readings identical and the
entry vacuous.

The five entries that predate this form —
[`float-ieee754`](../../testdata/conformance/float-ieee754),
[`float-ieee754-little-endian`](../../testdata/conformance/float-ieee754-little-endian),
[`float-hfp`](../../testdata/conformance/float-hfp), `float-hfp-read-as-ieee`
and [`float-ieee-read-as-hfp`](../../testdata/conformance/float-ieee-read-as-hfp)
— carry it, and
[`float-ieee754-special`](../../testdata/conformance/float-ieee754-special)
carries the four values that were the argument for it: a NaN and the two
infinities, which a JSON number cannot state at all, and a negative zero, which
it can state and cannot be told from a zero by (#195).

### `INDEX`, `POINTER` and `NATIONAL` are base64

An item whose usage is `USAGE_INDEX`, `USAGE_POINTER` or `USAGE_NATIONAL` is
written as a JSON string of its bytes in base64 (#66).

- The alphabet **MUST** be RFC 4648 section 4's — `A`–`Z`, `a`–`z`, `0`–`9`, `+`
  and `/`. The URL-safe alphabet of section 5 **MUST NOT** be used: these values
  never appear in a URL, and section 4's is what every language's default
  encoder produces.
- The encoding **MUST** be padded with `=` to a multiple of four characters.
- It **MUST NOT** carry a line break, a space, or any character outside the
  alphabet and the padding.
- The unused bits of the final quantum **MUST** be zero, which is the canonical
  encoding RFC 4648 section 3.5 describes.
- An item of no bytes is written `""`.

These three are bytes rather than characters because their content is not this
format's to interpret. An `INDEX` or a `POINTER` holds an implementation's own
representation, and a `NATIONAL` item holds a character encoding the descriptor
records and this document does not decide; base64 states exactly what was there
and claims nothing about what it means.

### A file the reader refused

A file the generated reader refuses is an answer, not a crash. The values
document carries a `failure` beside the records it did read:

```json
{
  "records": [ ... ],
  "failure": "the sign nibble is not one of the four the convention admits"
}
```

`failure` **MUST** be present exactly when the direction the document describes
did not complete, and absent otherwise.

- For a document describing a **read** — an entry's `values.json`, and the
  `decoded` half of an [answer](#the-answer-document) — that is a read that
  stopped before the end of the file, and `records` **MUST** hold the records
  read before it stopped, in file order.
- For the **`written`** half of an answer it is a writer that refused a record
  it was given, or a file it could not complete. `records` is then empty: the
  file was never finished, so nothing was read back out of it, and there is
  nothing for the entry to be compared against.

The text is a note for whoever reads the report and **MUST NOT** be compared: a
diagnostic is a generator's own wording in its own language, so an entry
demanding particular words would be an entry only one generator could pass. What
is compared is that reading failed, and that it failed after the records listed.

### Comparison is over the written form

A harness comparing two values documents **MUST** compare a scalar by its
written form — string equality — and **MUST NOT** decode either side into a
numeric type first (#196).

That is the whole reason the language spells everything exactly. Comparing
decoded numbers would make an eighteen-digit item equal to a different
eighteen-digit item that rounds to the same double, and — the case that has
actually bitten — would make a negative zero equal a positive one under IEEE
equality, so a generator that lost the sign of a zero would pass.

A loader **SHOULD** refuse a values document whose scalars are not in the forms
above, rather than leaving the difference to be reported by a comparison against
a generator (#196). An author who writes `"012"` where the format says `"12"`
should be told by the thing that read their file, not by a generator appearing
to disagree with them.

## What a runner does

A runner is the language-specific half, and there is one per generator language.
Given an entry it:

1. hands `ir.json` — as the binary encoding, which is what
   [`plugin/SPEC.md`](../plugin/SPEC.md) says a plugin is given — to the
   generator under test, with whatever options that generator needs;
2. compiles what came back;
3. reads `input.bin` with it, top to bottom;
4. where that read reached the end of the file, writes those records back out
   with the generated writer and reads *that* file with the generated reader;
5. writes an answer document on standard output.

A runner **MUST** write exactly one JSON document on standard output and nothing
else on that stream.

A runner **MUST NOT** exit non-zero because the generated reader refused the
file, or because the generated writer refused a record: those are answers, an
entry is allowed to expect one, and only the comparison knows whether this entry
did. A runner **MUST** exit non-zero when it could not produce an answer at all
— the generator would not run, its output would not compile, or the document
could not be written.

A runner is not asked to explain a difference. That is the comparison's job, and
a runner that argued its own case would be a runner whose report had to be
trusted.

### The answer document

```json
{
  "decoded": {
    "records": [ ... ]
  },
  "written": {
    "records": [ ... ]
  }
}
```

`decoded` is required and is what the generated reader made of `input.bin`.
`written` is optional and is what the generated reader makes of the file the
generated *writer* produced from those records. Both are values documents, in
exactly the form `values.json` is written in, and an unknown member **MUST** be
refused.

Both are compared against `values.json` — against the entry rather than against
each other, because a reader and a writer that are wrong the same way agree with
each other and only the entry knows what the file holds.

`written` **MUST** be absent in two cases: where the read did not reach the end
of the file, since a run that stopped at a failure holds no complete set of
records to write back; and where the generator emits no writer at all, which
[*Writing a file*](../ir/SPEC.md#writing-a-file) leaves to the generator. It is
present and carries a `failure` where the writer refused a record it was given.

### Why the writing direction is checked by reading, and not by comparing bytes

The obvious check is that the bytes the writer produced are `input.bin` again,
and it is the wrong check twice over.

[*Writing a file*](../ir/SPEC.md#writing-a-file) makes byte identity a claim
about a **record** and deliberately not about a file: under an optional
terminator a writer emits a final delimiter the input need not have carried, and
under segmented framing it lays a record into as few segments as the largest
allows, whatever the input did. A corpus demanding the input's bytes back would
fail two of the four framings by design.

It is wrong at the field level too, and the corpus already holds the case:
[`packed-ascii`](../../testdata/conformance/packed-ascii) carries the lenient
sign nibble `A`, which a reader admits as positive and a writer has no reason to
emit — it writes the `C` the convention prescribes. The same goes for every
encoding that admits more than one spelling of one value.

What *Writing a file* does make normative of a file is that a file a writer
produces is one that a reader built from the same descriptor reads back as the
records the writer was given. That holds for all four framings and every
encoding, it is what `written` states, and it needs nothing of a runner that the
reading direction did not already need.

## Out of Scope

### What a value means

How many bytes an item occupies, which nibble carries its sign, how a record is
framed, and which record type a run of bytes is are **not specified here**.

Reason: they are [`ir/SPEC.md`](../ir/SPEC.md)'s and, beneath it,
[`codec/SPEC.md`][codec-spec]'s. This document specifies how the answer is
written down, and it is useful precisely because it is independent of the answer
— a comparison of two values documents needs no decoder, which is what lets a
harness compare a runner for a language it knows nothing about. Restating any of
the byte-level rules here would produce a second description for the two to
disagree about.

### The corpus's own entries

Which entries exist, what each covers, which specification section it was
derived from, and how to add one are **not specified here**.

Reason: they are the corpus's, and the corpus documents itself where it lives
([`testdata/conformance/README.md`](../../testdata/conformance/README.md), and
[CONVENTIONS.md](../CONVENTIONS.md)'s *What belongs here*). The catalogue
changes every time an entry is added; this document changes when the format
does, which should be almost never. Binding them into one document would make
every new entry a change to a published interface.

### The content of `offsets.json`

The member name is reserved and its content is **not specified here**.

Reason: whether a corpus entry is the right home for an offset oracle at all is
an open question (#193), and specifying a member before deciding that would be
specifying it twice. What is cheap now and expensive later is the *name*, which
is why the name is reserved and nothing else is.

### How a generator is invoked

The argument vector, the `PATH` search, the exit codes and the option syntax are
**not specified here**.

Reason: they are the [plugin contract](../plugin/SPEC.md)'s (#39). A runner
invokes a generator through that contract like any other caller, and a second
description of it here would be one a plugin author could find and follow into
disagreement.

### Also out of scope

- **A test framework.** Nothing here says how a runner is built, what a harness
  reports, or what exit code a failing comparison produces. `go test
  ./internal/conformance/...` is this repository's answer and it is one answer
  among many.
- **A conformance level or a badge.** There is no partial conformance, no
  profile and no score. An entry either compares equal or it does not.
- **A wire protocol between a harness and a runner.** A runner is a process that
  writes a document on standard output, for the same reason a plugin is an
  executable rather than a server.
- **A descriptive generator's oracle.** A generator that never reads bytes has
  no values document to write, and what it should be held to instead is #193's.

## Appendix: The grammar corpus

[GRAMMAR.md](GRAMMAR.md) is every rule of [the value
language](#the-value-language) as a table of *a value, and the exact document
text that value is written as* — a variant arm, a slack node, an `INDEX`, a
`POINTER`, a `NATIONAL` item, both edited categories, every canonical float and
every spelling of a number a writer may not produce (#197).

It is a worked example of this section and never an addition to it: where the
two disagree, this document is right. What it buys is that a values-document
writer can be checked before a single entry is run, so that a JSON formatting
mistake is reportable as one instead of arriving as a generator that appears to
have decoded a record wrongly.

It is separate from this document because it is example text rather than
requirement, it grows a row whenever a construct wants one, and it is checked by
a test rather than by a reader — `internal/conformance/grammar_test.go` reads
those tables out of the file and holds this repository's own writer and loader
to every row.

## Appendix: Mapping to Stories

| Section | Implemented by |
|---|---|
| [An entry](#an-entry) | #66 `conformance` for the format and the loader that holds a directory to it |
| [`entry.json`](#entryjson) | #66 `conformance` |
| [`ir.json`](#irjson) | #66 `conformance`; the canonical rendering it is held to, #20 `ir` |
| [`values.json`](#valuesjson) | #66 `conformance` |
| [`offsets.json` is reserved](#offsetsjson-is-reserved) | #194 `conformance` for the reservation; the discussion it comes from is #193, and no story specifies its content |
| [Which form a value takes is decided by the descriptor](#which-form-a-value-takes-is-decided-by-the-descriptor) | #66 `conformance`; #194 `conformance` for stating it as a procedure over `usage` and `category` |
| [A group, a table and a variant](#a-group-a-table-and-a-variant) | #66 `conformance` |
| [Characters, and why trailing spaces do not survive](#characters-and-why-trailing-spaces-do-not-survive) | #66 `conformance` for the behaviour the corpus already expects; #194 `conformance` for deciding it and writing it down |
| [A number is a decimal string](#a-number-is-a-decimal-string) | #66 `conformance` for the form; #194 `conformance` for the canonical spelling, including negative zero; #196 `conformance` for the loader that enforces it |
| [A float is written exactly, and never as a JSON number](#a-float-is-written-exactly-and-never-as-a-json-number) | #194 `conformance` decides the form; #195 `conformance` writes and compares it, and migrates the float entries |
| [`INDEX`, `POINTER` and `NATIONAL` are base64](#index-pointer-and-national-are-base64) | #66 `conformance` for base64; #194 `conformance` for the alphabet and the padding; #196 `conformance` for enforcing them |
| [A file the reader refused](#a-file-the-reader-refused) | #66 `conformance` |
| [Comparison is over the written form](#comparison-is-over-the-written-form) | #68 `conformance` for the comparison; #195 and #196 `conformance` for it being over the written form |
| [What a runner does](#what-a-runner-does) | #68 `conformance` for the Go runner that implements it |
| [The answer document](#the-answer-document) | #68 `conformance` |
| [Why the writing direction is checked by reading](#why-the-writing-direction-is-checked-by-reading-and-not-by-comparing-bytes) | #68 `conformance`; the rule it rests on, *Writing a file*, #17 `ir` |
| [The grammar corpus](#appendix-the-grammar-corpus) | #197 `conformance` for [GRAMMAR.md](GRAMMAR.md), the writer it holds to it, and the test that reads one against the other |
| The corpus's entries, and what each covers | #67 `conformance`, and one story per entry since |
| This document | #194 `conformance` |
| Conventions this document follows | #15 `setup` |
