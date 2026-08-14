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

## Where the format is written down, and what is written down here

The format is [`docs/conformance/SPEC.md`](../../docs/conformance/SPEC.md): what
an entry is made of, the value language a decoded record is written in, and the
answer document a runner writes. It is a spec under `docs/` rather than a
section of this file because a third-party generator author has to implement it,
which is the test [`docs/CONVENTIONS.md`](../../docs/CONVENTIONS.md), *What
belongs here*, applies — while this repository held the only reader of the
format, prose here was enough, and the day a second implementation exists every
unstated rule becomes a coordinated migration (#194).

What stays here is the corpus rather than the format: why it exists, why every
entry is hand-authored, which entries there are and what each was derived from,
and how to add one. Those change every time an entry is added and are nobody
else's interface.

So there are no bolded conformance keywords below. The specs use them; this file
describes a directory of entries, and where it says what a member holds it is
recalling the spec rather than defining it.

## An entry

One directory per entry, named for what it is about. It holds five members and
the copybooks the layout names; that list, the one reserved name it may also
carry, and what each member is held to are [*An
entry*](../../docs/conformance/SPEC.md#an-entry).

An entry states two independent things, and they are checked by different
readers:

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

### The members, and the value language

What each member holds, and the language a decoded record is written in — a
group, a table, a variant, characters, a number, a float, a run of bytes, and a
file the reader refused — are [*An
entry*](../../docs/conformance/SPEC.md#an-entry) and [*The value
language*](../../docs/conformance/SPEC.md#the-value-language). Four rules moved
with it and are now decided (#194). Three were never written here at all, which
is the argument for the move rather than a slip: whether trailing spaces on an
alphanumeric item survive, how a decimal string may be spelled and whether a
negative zero is one, and which base64 alphabet and padding. The fourth is a
reversal — this file said a `COMP-1` or `COMP-2` item was a JSON number, and the
spec says it is a string in hexadecimal significand notation, exactly so that a
NaN, an infinity and a negative zero can be written at all. The corpus's five
float entries were migrated to it and `float-ieee754-special` was added beside
them, carrying the four values that were the argument for the change: a NaN,
both infinities, and a negative zero (#195).

`values.json` is one half of the comparison and a runner's answer document is
the other, written in exactly the same language. What a runner is asked to do,
what it is deliberately not asked to do, and why the writing direction is
checked by reading rather than by comparing bytes are [*What a runner
does*](../../docs/conformance/SPEC.md#what-a-runner-does).

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
| [`packed-comp6`](packed-comp6) | `COMP-6`, which is packed with no sign nibble: both parities of the digit count, and an alphanumeric item behind them that only stays in place if neither was read a byte too wide. |
| [`comp6-invalid-digit`](comp6-invalid-digit) | A `COMP-6` field ending in `C`, which is a sign nibble and so not a digit — what a `COMP-3` field read at a `COMP-6` offset looks like. |
| [`binary-big-endian`](binary-big-endian) | Binary integers: a positive, a negative, a zero, and the four-to-five digit width step. |
| [`binary-little-endian`](binary-little-endian) | The same values, the other byte order. |
| [`binary-byte-order-detected`](binary-byte-order-detected) | A big-endian field read little-endian, caught by the range `PIC S9(4)` declares. |
| [`binary-unsigned-comp5`](binary-unsigned-comp5) | `FF FF` under two PICTUREs: 65535 in a `COMP-5` item with no `S`, and -1 in one with an `S`. |
| [`float-ieee754`](float-ieee754) | `COMP-1` and `COMP-2` holding 1.0 as IEEE 754, big-endian. |
| [`float-ieee754-little-endian`](float-ieee754-little-endian) | `COMP-1` holding 1.0 with its bytes reversed. |
| [`float-hfp`](float-hfp) | The same two items in IBM hexadecimal floating point. |
| [`float-hfp-read-as-ieee`](float-hfp-read-as-ieee) | HFP 1.0 read as IEEE, which is 9.0 and is not an error. |
| [`float-ieee-read-as-hfp`](float-ieee-read-as-hfp) | IEEE 1.0 read as HFP, which is 0.03125 and is not an error. |
| [`float-ieee754-special`](float-ieee754-special) | IEEE 754's special values in four `COMP-1` items: a NaN, both infinities, and a negative zero. |
| [`mixed-record-ebcdic`](mixed-record-ebcdic) | Appendix A.7's mixed record as an IBM Enterprise COBOL file. |
| [`mixed-record-ascii`](mixed-record-ascii) | The same record as a Micro Focus ASCII file, little-endian. |
| [`mixed-record-converted`](mixed-record-converted) | The same record after a correct, copybook-aware conversion from EBCDIC. |
| [`batch-fixed`](batch-fixed) | Three record types told apart by a type code and ordered by a sequencing expression, end to end. |
| [`batch-rdw`](batch-rdw) | The same batch behind the record descriptor word `RECFM=VB` puts in front of each record. |

Every entry derived from `cobol-go`'s `codec/SPEC.md` Appendix A cites the rows
it came from (#67). Three are not derived from it — `float-ieee754-special`,
`batch-fixed` and `batch-rdw` — and the last two subsections below say what each
cites instead. The first of the three is about something else: which entries
Appendix A's vectors are paired into, which is a question about the entries that
*are* derived from it.

### Where "in both character sets" applies, and where it does not

Appendix A's vectors do not all vary along charset, and pairing every one of them
into an ASCII entry and an EBCDIC entry would have produced entries traceable to
no row. So the pairing follows the axis the vector actually varies along:

- **A.1, A.2, A.3 and A.7 pair by charset**, and each is here twice.
- **A.4 is charset-invariant**, and the spec says so. It is here twice anyway,
  as `packed-ascii` and `packed-ebcdic`, whose `input.bin` and `values.json` are
  byte for byte the same file: the invariance is the claim, and two entries
  differing only in the axis that does not apply are how a corpus states one.
  A.4's `COMP-6` rows are the exception, and `packed-comp6` is here once: the
  claim is stated by the pair above, restating it costs a second entry for
  nothing, and `COMP-6` is an extension of the compilers that do not run on the
  mainframe — so the one charset it is in is ASCII.
- **A.5 varies by byte order** and **A.6 by float format**, not by charset. Their
  pairs are `binary-big-endian`/`binary-little-endian` and
  `float-ieee754`/`float-hfp`.

### The special-values entry cites IEEE 754, not Appendix A

A.6 gives one vector per float format at the value 1.0. A NaN, an infinity and a
negative zero are properties of the format rather than values anybody would
tabulate at, so there is no row to derive `float-ieee754-special` from and it is
authored fresh against IEEE 754-2019's binary32 interchange format. What it
states about the *corpus* — that each of the four is written as one of the
sentinels or as a hexadecimal significand, and that the negative zero is not the
zero — is [`docs/conformance/SPEC.md`](../../docs/conformance/SPEC.md)'s, which
is the section it cites beside the standard (#195).

It is `ieee-754` and has no HFP pair, which is the one place a float entry does
not pair. HFP has no encoding for a NaN or an infinity at all, so the bytes that
would make the pair do not exist; `codec/SPEC.md` says so, and it is why that
format is a weak signal for telling the two apart in the first place.

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

### No row of Appendix A is deliberately absent, and two once were

Every row of Appendix A is now an entry. This section used to list the ones that
were not, and both of them were held out for the same reason: the vector was
correct, the generator could not read it, and an entry would have failed on the
generator rather than on the corpus. Each is here because the defect it found was
fixed, and each is kept written down because the sequence is the point — the
corpus did its job one step early both times, catching a disagreement between the
generator and the specification while an entry was being written rather than
after one was.

The **`PIC 9(4) COMP`, 65535, `FF FF`** row was the first. `cpybkc-gen-go`
selected its binary accessor by digit count alone, so an unsigned four-digit item
was read as a signed `int16` and 65535 came back as -1;
[#163](https://github.com/Zaba505/cpybkc/issues/163) is that defect, and
[`binary-unsigned-comp5`](binary-unsigned-comp5) is the vector it was found by,
seeded once the generator could read it. The row is `TRUNC(BIN)`/`COMP-5` only —
65535 is outside four decimal digits — so the entry declares `COMP-5` and needs
the unsigned accessor as well as the `COMP-5` one.

The two **`COMP-6`** rows were the second. `cpybkc-gen-go` read a `COMP-6` item
with the packed accessors, which consume a sign nibble a `COMP-6` item does not
carry, so `PIC 9(4) COMP-6` was read a byte too wide and every field behind it
moved; [#162](https://github.com/Zaba505/cpybkc/issues/162) is that defect and
[`packed-comp6`](packed-comp6) is the entry. It carries both rows — the even
digit count, where the two usages differ by a byte, and the odd one, where they
do not — and an alphanumeric item behind them, which is what turns the width
error into a visible one. A.4's COMP-6 negative tests came with it as
[`comp6-invalid-digit`](comp6-invalid-digit), and that entry is the odd digit
count's only defence: where the two widths coincide, nothing but the nibbles can
tell a `COMP-3` field read as `COMP-6` from a `COMP-6` field.
