# The conformance corpus

A set of small files with the right answer written down, so that two generators
handed one descriptor can be shown to decode one file the same way.

cpybkc is strictly a code generator: there is no `convert` and no `synth`, so
nothing in this repository reads a data file except the code a generator emitted.
That leaves nothing to hold `cpybkc-gen-go` and a third-party generator to each
other — [`docs/ir/SPEC.md`](../../docs/ir/SPEC.md) names the hazard twice, once
for the position sum every consumer runs and may get wrong on its own, and once
for two writers free to choose different bytes for one record — and a shared
corpus is the mechanism that closes it, which is what protobuf's conformance
suite is and why.

Every entry here is authored by hand. Slower growth is the trade, and what it
buys is that each entry is traceable to the section it was derived from rather
than to a run of the code it is supposed to be checking (#67).

## Why the format is documented here and not under `docs/`

[`docs/CONVENTIONS.md`](../../docs/CONVENTIONS.md), *What belongs here*, admits a
document under `docs/` when it specifies an interface a third party builds
against and the interface is harder to change than the code behind it. Test
infrastructure documents itself where it lives, and this is that document: the
corpus is a `testdata/` format, run by a harness in this repository and by
third-party generators against their own output.

So there are no bolded conformance keywords below. The four specs use them; this
file describes a directory of files, and a rule here is enforced by the loader
rather than by a reader deciding what a **MUST** binds.

## An entry

One directory per entry, named for what it is about. The directory holds five
members and the copybooks the layout names, and nothing else — a file the format
has no place for is refused rather than ignored, for the reason the project
manifest refuses an unknown field.

| File | What it is |
|---|---|
| `entry.json` | What the entry is about, and the section its expected answer came from. |
| `layout.sexpr` | The [layout](../../docs/layout/SPEC.md) the file is laid out by. |
| `*.cpy` | The copybooks that layout names, at the paths it spells them. |
| `ir.json` | The [IR](../../docs/ir/SPEC.md) the layout and those copybooks resolve to. |
| `input.bin` | The bytes of one file laid out that way. |
| `values.json` | What those bytes decode to. |

An entry therefore states two independent things, and they are checked by
different readers:

- **The layout and its copybooks resolve to `ir.json`.** That is a claim about a
  producer. The descriptor is hand-authored, so it is an oracle rather than a
  recording of what `resolve` happens to do today, and the pipeline that will
  check it against a real resolution is the CLI's (#148).
- **`ir.json`, handed to a generator, produces code that decodes `input.bin`
  into `values.json` — and writes those records back into a file that decodes
  to `values.json` again.** That is a claim about a consumer, in both
  directions, and it is what a generator author in any language can run today
  (#68).

Carrying both is what lets a failure be attributed. A generator author who
disagrees with cpybkc about what a *layout* means and one who disagrees about
what a *descriptor* means have different bugs, and an entry carrying three of the
four members could not tell them apart.

### `entry.json`

```json
{
  "description": "A fixed-length dataset of one record type: ...",
  "source": "cobol-go codec/SPEC.md, \"Storage Widths\"; docs/ir/SPEC.md, \"Physical framing\""
}
```

Both are required and there is no third key. `source` is what a failing run
prints beside the entry's name: whoever reads the report has to decide whether
the generator is wrong or the entry is, and that decision starts at where the
expected answer came from.

### `ir.json`

The descriptor, in the canonical JSON rendering — protobuf's own field names,
field-number order, two-space indent — which is exactly what
`cpybkc --emit-ir --emit-ir-format json` writes and what
`internal/emit.MarshalJSON` produces. The loader re-renders what it read and
refuses a file that is not byte for byte the same, so an entry is reviewed as a
diff of its content rather than of its whitespace.

JSON rather than the binary form because an entry is written and reviewed by
hand. A runner that would rather have the bytes a plugin is handed encodes what
it read: the binary encoding is canonical for a *plugin*, and any protobuf
runtime turns one into the other with the schema, which every release attaches as
`ir.binpb` and `ir-protos.tar.gz`.

### `values.json`

The records the file holds, in file order.

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

`name` is the record's name as the copybook spells it — the `original` of the
record node's names, never an identifier a generator munged out of it. `value` is
what the record's top-level node holds.

The value language is small, and every part of it is language-neutral on purpose:

| The node | Written as |
|---|---|
| A group | An object, one key per member, keyed by the copybook's name for it. |
| An item that repeats | An array, one element per occurrence, in order. |
| Alphabetic, alphanumeric and both edited categories | A JSON string of the characters, after the charset has been applied. |
| Any numeric item — zoned, packed or binary | A JSON string of the decimal digits, with a leading `-` where it is negative, and no leading zeros. |
| `COMP-1` and `COMP-2` | A JSON number. |
| `INDEX`, `POINTER` and `NATIONAL` | A JSON string of the bytes in base64. |
| A variant | One key per arm in the enclosing object, of which the occurrence carries exactly one; an arm the occurrence does not hold has no key at all. |
| A slack node | Nothing. Its bytes travel with the record and are not a value anybody decoded. |

A number is a string rather than a JSON number because JSON numbers are doubles
in most readers, and a `PIC S9(18)` item holds values a double cannot represent.
The digits are the *unscaled* ones — an implied decimal point is not applied, for
the same reason a generated accessor does not apply one: the point is in the
descriptor, and applying it here would apply it twice by the time a caller saw
the value.

A file the generated reader refuses carries a `failure` beside the records it did
read:

```json
{
  "records": [ ... ],
  "failure": "the sign nibble is not one of the four the convention admits"
}
```

What is compared is that reading failed, and that it failed after the records
listed. The text is a note for whoever reads the report and is deliberately not
compared: a diagnostic is a generator's own wording in its own language, so an
entry demanding particular words would be an entry only one generator could pass.

## What a runner does

A runner is the language-specific half, and there is one per generator language.
Given an entry it:

1. hands `ir.json` — as the binary encoding, which is what
   [`docs/plugin/SPEC.md`](../../docs/plugin/SPEC.md) says a plugin is given — to
   the generator under test, with whatever options that generator needs;
2. compiles what came back;
3. reads `input.bin` with it, top to bottom;
4. where that read reached the end of the file, writes those records back out
   with the generated writer and reads *that* file with the generated reader;
5. writes an answer document on standard output, carrying a values document for
   each direction.

The comparison is then a comparison of values documents and needs neither the
descriptor nor a decoder, which is what makes a runner for a language this
repository has never seen comparable by the same rules.

Two things a runner is not asked for. It is not asked to explain a difference —
that is the comparison's — and it does not exit non-zero for a file the generated
reader refused, or for a record the generated writer refused: those are answers,
written as `failure`, because an entry is allowed to expect one and only the
comparison knows whether this entry did. A non-zero exit means the runner itself
failed: the generator would not run, its output would not compile, or the runner
could not write a document.

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

`decoded` is what the generated reader made of `input.bin`. `written` is what the
generated reader makes of the file the generated *writer* produced from those
records. Both are values documents, in exactly the form `values.json` is written
in, and both are compared against `values.json` — against the entry rather than
against each other, because a reader and a writer that are wrong the same way
agree with each other and only the entry knows what the file holds.

`written` is absent in two cases: where the read did not reach the end of the
file, since a run that stopped at a failure holds no complete set of records to
write back; and where the generator emits no writer at all, which
[`docs/ir/SPEC.md`](../../docs/ir/SPEC.md)'s *Writing a file* leaves to the
generator. It is present and carries a `failure` where the writer refused a
record it was given.

### Why the writing direction is checked by reading, and not by comparing bytes

The obvious check is that the bytes the writer produced are `input.bin` again,
and it is the wrong check twice over.

*Writing a file* makes byte identity a claim about a **record** and deliberately
not about a file: under an optional terminator a writer emits a final delimiter
the input need not have carried, and under segmented framing it lays a record
into as few segments as the largest allows, whatever the input did. A corpus
demanding the input's bytes back would fail two of the four framings by design.

It is wrong at the field level too, and this corpus already holds the case:
[`packed-ascii`](packed-ascii) carries the lenient sign nibble `A`, which a
reader admits as positive and a writer has no reason to emit — it writes the `C`
the convention prescribes. The same goes for every encoding that admits more than
one spelling of one value. Demanding the bytes back would make those entries
unpassable by a correct generator, and dropping them would cost the corpus
exactly the vectors it was seeded from.

What *Writing a file* does make normative of a file is that a file a writer
produces is one that a reader built from the same descriptor reads back as the
records the writer was given. That holds for all four framings and every
encoding, it is what `written` states, and it needs nothing of a runner that the
reading direction did not already need.

## What this repository runs

`internal/conformance` is the corpus — the loader, the value language and the
comparison — and every word of it is language-neutral.
`internal/conformance/gorunner` is the Go runner: it invokes a generator through
the same code path cpybkc invokes a plugin with, writes a driver beside the
generated package, and builds and runs it with the Go toolchain.

The driver reads the descriptor at run time and walks the generated structs
beside it by reflection, rather than being emitted with the copybook's names
already spelled into it. The names in a values document have to come from
somewhere, and only two places have them: the descriptor, and a second copy of
how `cpybkc-gen-go` munges an identifier out of one. A copy would agree with the
generator exactly until the rule changed, and then compare the wrong fields
without failing.

It hands the records the reader produced straight to the writer rather than
rebuilding them from the values beside them, because a record built from a values
document is a different record: *Slack survives a read* puts the bytes no item
covers on the record a reader produced, and a constructed one has a writer fill
them instead.

`go test ./internal/conformance/...` runs the corpus against `cpybkc-gen-go`
built from the tree under test, in both directions and over every entry (#68).

It is an ordinary Go test rather than a stage of its own, and that is what makes
it a gate on every platform CI runs on. `dagger call ci` runs `go test` over this
module, so the corpus is inside the one call CI makes rather than beside it: a
platform added to the matrix carries the corpus with it, and a conformance job of
its own would be a second gate that the new platform would silently not have. The
matrix is one platform today, and
[#56](https://github.com/Zaba505/cpybkc/issues/56) is why it holds no big-endian
host — cpybkc runs on a developer's machine, and a big-endian *file* is a
property of the data, which is what the byte-order entries here read on whatever
platform they are run.

## Adding an entry

1. Make a directory named for what the entry is about.
2. Write the copybook and the layout, then the descriptor they resolve to. The
   descriptor is authored, not generated: that is what makes it an oracle.
3. Write the bytes, and the values they decode to, from the specification —
   not from what a generator printed. An entry recorded from the code it checks
   passes forever, including through the bug it was written to catch.
4. Cite the section in `entry.json`.
5. `go test ./internal/conformance/...`. The loader reports what is wrong with
   the entry, and the run reports what the generated code decoded where it
   differs from what you wrote.

The rendering of `ir.json` is the one thing worth generating: write the content
by hand, then let the canonicalisation check tell you where the whitespace goes.

## The entries

The shape of the corpus is one entry per *encoding*, because the four axes are
properties of a file rather than of an item: a generated package states them once
for the file it reads, so two conventions are two files and two entries. That is
why the zoned rows of Appendix A come back as four entries rather than one record
with four items in it.

| Entry | What it covers |
|---|---|
| [`orders-fixed`](orders-fixed) | A fixed-length ASCII dataset of one record type, with no framing around a record: an alphanumeric key, an unsigned zoned count, and a table of two occurrences. |
| [`zoned-ascii-zone37`](zoned-ascii-zone37) | Zoned decimal under the native ASCII convention: a trailing overpunch of either sign, an unsigned item, and both separate-sign positions. |
| [`zoned-ebcdic`](zoned-ebcdic) | The same items in EBCDIC, with the leading overpunch Appendix A gives no ASCII row for. |
| [`zoned-ascii-translated`](zoned-ascii-translated) | ASCII characters with the sign bytes an EBCDIC-to-ASCII text conversion produces. |
| [`zoned-ascii-realia`](zoned-ascii-realia) | The CA Realia convention, whose positive value is spelled exactly as an unsigned one. |
| [`zoned-invalid-digit-nibble`](zoned-invalid-digit-nibble) | A sign byte whose digit nibble is `B`, read under `ascii-zone-37`. |
| [`zoned-invalid-sign-translated`](zoned-invalid-sign-translated) | An `ascii-zone-37` sign byte read under `translated-ebcdic`. |
| [`zoned-invalid-zone`](zoned-invalid-zone) | A Realia sign byte read under `ascii-zone-37`. |
| [`zoned-ebcdic-bytes-under-ascii`](zoned-ebcdic-bytes-under-ascii) | An EBCDIC field read as ASCII, which fails at the first digit rather than at the sign. |
| [`zoned-ascii-bytes-under-ebcdic`](zoned-ascii-bytes-under-ebcdic) | The mirror of it. |
| [`packed-ascii`](packed-ascii) | Packed decimal: both signs, unsigned, an even digit count, a scaled item and the lenient sign nibble `A`. |
| [`packed-ebcdic`](packed-ebcdic) | The same bytes and the same values in a file declared EBCDIC, which is what charset-invariance means. |
| [`packed-invalid-sign`](packed-invalid-sign) | A sign nibble of `5`, which is a digit and means no sign. |
| [`packed-invalid-digit`](packed-invalid-digit) | A digit nibble of `A`. |
| [`packed-invalid-pad`](packed-invalid-pad) | A non-zero pad nibble on an item of an even digit count. |
| [`binary-big-endian`](binary-big-endian) | Binary integers: a positive, a negative, a zero, and the four-to-five digit width step. |
| [`binary-little-endian`](binary-little-endian) | The same values, the other byte order. |
| [`binary-byte-order-detected`](binary-byte-order-detected) | A big-endian field read little-endian, caught by the range `PIC S9(4)` declares. |
| [`binary-unsigned-comp5`](binary-unsigned-comp5) | `FF FF` under two PICTUREs: 65535 in a `COMP-5` item with no `S`, and -1 in one with an `S`. |
| [`float-ieee754`](float-ieee754) | `COMP-1` and `COMP-2` holding 1.0 as IEEE 754, big-endian. |
| [`float-ieee754-little-endian`](float-ieee754-little-endian) | `COMP-1` holding 1.0 with its bytes reversed. |
| [`float-hfp`](float-hfp) | The same two items in IBM hexadecimal floating point. |
| [`float-hfp-read-as-ieee`](float-hfp-read-as-ieee) | HFP 1.0 read as IEEE, which is 9.0 and is not an error. |
| [`float-ieee-read-as-hfp`](float-ieee-read-as-hfp) | IEEE 1.0 read as HFP, which is 0.03125 and is not an error. |
| [`mixed-record-ebcdic`](mixed-record-ebcdic) | Appendix A.7's mixed record as an IBM Enterprise COBOL file. |
| [`mixed-record-ascii`](mixed-record-ascii) | The same record as a Micro Focus ASCII file, little-endian. |
| [`mixed-record-converted`](mixed-record-converted) | The same record after a correct, copybook-aware conversion from EBCDIC. |
| [`batch-fixed`](batch-fixed) | Three record types told apart by a type code and ordered by a sequencing expression, end to end. |
| [`batch-rdw`](batch-rdw) | The same batch behind the record descriptor word `RECFM=VB` puts in front of each record. |

Every entry above `batch-fixed` is derived from `cobol-go`'s `codec/SPEC.md`
Appendix A and cites the rows it came from (#67). The two below it are not, and
the next two subsections are why.

### Where "in both character sets" applies, and where it does not

Appendix A's vectors do not all vary along charset, and pairing every one of them
into an ASCII entry and an EBCDIC entry would have produced entries traceable to
no row. So the pairing follows the axis the vector actually varies along:

- **A.1, A.2, A.3 and A.7 pair by charset**, and each is here twice.
- **A.4 is charset-invariant**, and the spec says so. It is here twice anyway,
  as `packed-ascii` and `packed-ebcdic`, whose `input.bin` and `values.json` are
  byte for byte the same file: the invariance is the claim, and two entries
  differing only in the axis that does not apply are how a corpus states one.
- **A.5 varies by byte order** and **A.6 by float format**, not by charset. Their
  pairs are `binary-big-endian`/`binary-little-endian` and
  `float-ieee754`/`float-hfp`.

### The framing entry cites this repository, not Appendix A

Appendix A is entirely field- and record-level — A.1 to A.6 are single fields and
A.7 is one 15-byte record with no framing bytes — and `codec/SPEC.md` places
record formats out of scope in as many words. There is therefore no vector to
derive a record descriptor word from, and `batch-rdw` is authored fresh against
[`docs/layout/SPEC.md`](../../docs/layout/SPEC.md)'s *Physical framing* and
[`docs/ir/SPEC.md`](../../docs/ir/SPEC.md)'s *Four framings, and none of them is a
RECFM*.

It still cites a section, which is what the rule is for. What it does not cite is
`codec/SPEC.md`, and that is a property of the source rather than a gap in the
entry: framing is this repository's layer, so this repository's specs are where
its right answer is written down. `batch-fixed` is in the same position, and both
carry Appendix A.7's record inside them so that the bytes of a *record* stay
traceable to a vector even where the bytes around it are not.

### One row of Appendix A that is deliberately not an entry

- **`PIC 9(4) COMP-6`, `12 34`.** `cobol-go`'s `codec` ships no COMP-6 accessor,
  and `cpybkc-gen-go` reads a `COMP-6` item with the packed ones — which consume
  a sign nibble a COMP-6 item does not carry. An entry for it would fail on the
  generator rather than on the corpus, which is a defect to report and not a
  vector to seed.

That is the corpus doing its job one step early: what it found is a
disagreement between the generator and the specification, found while an entry
was being written rather than after one was.

The **`PIC 9(4) COMP`, 65535, `FF FF`** row stood here beside it for the same
reason and no longer does. `cpybkc-gen-go` selected its binary accessor by digit
count alone, so an unsigned four-digit item was read as a signed `int16` and
65535 came back as -1; [#163](https://github.com/Zaba505/cpybkc/issues/163) is
that defect, and [`binary-unsigned-comp5`](binary-unsigned-comp5) is the vector
it was found by, seeded once the generator could read it. The row is
`TRUNC(BIN)`/`COMP-5` only — 65535 is outside four decimal digits — so the entry
declares `COMP-5` and needs the unsigned accessor as well as the `COMP-5` one.
