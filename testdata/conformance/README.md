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
  into `values.json`.** That is a claim about a consumer, and it is what a
  generator author in any language can run today.

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
4. writes a values document, in exactly the form `values.json` is written in, on
   standard output.

The comparison is then a comparison of two values documents and needs neither the
descriptor nor a decoder, which is what makes a runner for a language this
repository has never seen comparable by the same rules.

Two things a runner is not asked for. It is not asked to explain a difference —
that is the comparison's — and it does not exit non-zero for a file the generated
reader refused: that is an answer, written as `failure`, because an entry is
allowed to expect one and only the comparison knows whether this entry did. A
non-zero exit means the runner itself failed: the generator would not run, its
output would not compile, or the runner could not write a document.

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

`go test ./internal/conformance/...` runs the corpus against `cpybkc-gen-go`
built from the tree under test. Making that a gate across the whole CI matrix,
and exercising the encode direction as well as decode, is #68.

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

| Entry | What it covers |
|---|---|
| [`orders-fixed`](orders-fixed) | A fixed-length ASCII dataset of one record type, with no framing around a record: an alphanumeric key, an unsigned zoned count, and a table of two occurrences. |

One entry is what #66 ships, because a format nothing has been written against is
a format nobody has checked. The vectors of `cobol-go`'s `codec/SPEC.md` Appendix
A — zoned, packed, binary and floating point, in both character sets, with the
negative cases — are #67.
