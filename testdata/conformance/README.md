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

Beside it is [`docs/conformance/GRAMMAR.md`](../../docs/conformance/GRAMMAR.md),
the value language as a table of a value against the exact text it is written
as. That is where a writer for a new language is checked before any entry here
is run, and it is where the constructs no entry covers — a variant arm, a slack
node, `INDEX`, `POINTER`, `NATIONAL` and both edited categories — are written
down (#197).

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
`internal/conformance/engine` asks a generator about it, by starting a process
and speaking the frames [`docs/adapter/SPEC.md`](../../docs/adapter/SPEC.md)
specifies over that process's standard input and standard output. The program on
the other end is an **adapter**, and the engine has no idea which language is
behind one.

`internal/conformance/goadapter` is this repository's own adapter, for
`cpybkc-gen-go`. It is driven through the public contract exactly as a stranger's
would be, and that is the point rather than a convenience: if the contract cannot
carry this generator, it cannot carry a third party's either, and the cost of
finding that out here is one refactor.

It invokes the generator through the same code path cpybkc invokes a plugin with,
writes a codec program beside the generated package, and compiles every entry's
in one invocation of the Go toolchain — which is what `generate` carrying the
whole corpus at once is for.

A codec program reads the descriptor at run time and walks the generated structs
beside it by reflection, rather than being emitted with the copybook's names
already spelled into it. The names in a values document have to come from
somewhere, and only two places have them: the descriptor, and a second copy of
how `cpybkc-gen-go` munges an identifier out of one. A copy would agree with the
generator exactly until the rule changed, and then compare the wrong fields
without failing. Which Go type stands for which record is settled by folding both
names down to their letters and digits and requiring exactly one match — the
weakest property of the munging rule that can still pair them, and one that fails
loudly rather than quietly when it does not hold.

The codec program outlives the `decode` it answered, holding the records its
reader produced until a `roundtrip` asks for them, because a record built from a
values document is a different record: *Slack survives a read* puts the bytes no
item covers on the record a reader produced, and a constructed one has a writer
fill them instead.

`cmd/cpybkc-conform` is the engine with a command line on it, and it is what
somebody outside this repository runs. Every release attaches it and this
directory together as `cpybkc-conformance.tar.gz` — [*The published
corpus*](../../docs/conformance/SPEC.md#the-published-corpus) is the archive's
layout and the digest rule that says whether a download is this corpus (#202) —
so checking a generator needs no clone, no registry and no container runtime.
Nothing here is committed for it: the digest is a function of the corpus, so
adding an entry below changes what a release publishes and nothing else.

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
5. Decide whether the entry is normative or provisional — see below. Most are
   normative and say nothing; an entry nothing can cross-check yet writes
   `"status": "provisional"`.
6. `go test ./internal/conformance/...`. The loader reports what is wrong with
   the entry, and the run reports what the generated code decoded where it
   differs from what you wrote.

The loader holds every scalar to the spelling the value language gives its form,
so a decimal string carrying a leading zero, a base64 value in the wrong
alphabet or padding, a float in the form some other language's formatter happens
to write, and a character item still padded to its width are all refused where
you wrote them (#196). That is deliberate: `"012"` and `"12"` are one value
written down twice, and left to the comparison it would surface as a generator
appearing to disagree with you about a number.

The rendering of `ir.json` is the one thing worth generating: write the content
by hand, then let the canonicalisation check tell you where the whitespace goes.

### A new entry may be provisional

Hand-authoring is what makes an entry an oracle, and it is also the one way an
entry goes wrong that nothing here can catch. An entry recorded from the code it
checks passes forever; an entry authored from a *misreading* fails forever, and
tells every implementation it is wrong. Nothing in a run distinguishes that from
a generator with a bug, and the entries where it is likeliest are the ones worth
most — a construct no implementation handles yet has nothing to disagree with
it, so the only defence is somebody reviewing a byte string computed by hand.

So an entry may declare `"status": "provisional"`, and then it runs, its result
is reported, and it counts in no total and fails nothing. The rule a harness
follows and the two things that promote one — two independent implementations
agreeing with it, or a second person re-deriving its answer from the
specification — are [*A provisional
entry*](../../docs/conformance/SPEC.md#a-provisional-entry).

**Every entry here is normative**, and a test says so, because an entry that
became provisional would stop counting and stop being able to fail a run without
any other test noticing. The status is for a *new* entry that cannot be
cross-checked yet, and today nothing is in that position: the corpus predates a
second implementation, and every entry in it was reviewed as normative. When one
is added provisionally, promoting it is an ordinary pull request — remove the
member, and say in the description which of the two things happened and who or
what did it.

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
| [`batch-ordered`](batch-ordered) | A batch with no trailer, whose two discriminators sit at different offsets and different widths: the pair only the order at that state separates, with the header's test first. |
| [`batch-disjoint`](batch-disjoint) | The same shape with the opposing items numeric `DISPLAY`, which proves the pair exclusive in both directions — so the state carries the *detail's* test first and nothing observes it. |
| [`batch-ordered-missplit`](batch-ordered-missplit) | `batch-ordered`'s layout over a detail carrying the header's literal at the header's offset: it is read as a header, the batch splits in the wrong place, and nothing diagnoses it. |
| [`batch-ordered-rdw`](batch-ordered-rdw) | The ordered pair behind the record descriptor word, which the framing changes nothing about. |
| [`delimited-terminator`](delimited-terminator) | A line-sequential file whose records end with `0x15`, two of them holding a packed amount whose own bytes are `0x15`. |
| [`delimited-optional-terminator`](delimited-optional-terminator) | The same three records in a file whose last record carries no delimiter, which a writer supplies: twenty-three bytes written back as twenty-four. |
| [`delimited-ascii-newline`](delimited-ascii-newline) | The delimited file an adopter on Linux has: ASCII, records ended by `0x0A`, two of them holding a binary count whose own bytes are `0x0A`. |
| [`segmented-spanning`](segmented-spanning) | `RECFM=VBS`: a record laid into as few segments as the largest allows, and one laid into more than it allows a writer, both spanning. |
| [`odo-sliding`](odo-sliding) | `OCCURS DEPENDING ON` under the sliding reading, in a counted run of records: two tables of different lengths, each with an item behind it. |
| [`sync-slack`](sync-slack) | `SYNCHRONIZED` alignment: two runs of bytes no item covers, of different widths, each with items in front of it and behind it. |
| [`alphanumeric-payload`](alphanumeric-payload) | A `PIC X` item that carries bytes rather than text: one `encoding-override` on the group they sit in, a status flag of `0x03`, a region byte of `0x93`, all 256 byte values in one item, and both pad bytes — beside text items still read as characters. |

Every entry derived from `cobol-go`'s `codec/SPEC.md` Appendix A cites the rows
it came from (#67). Fourteen are not derived from it — `float-ieee754-special`,
`batch-fixed`, `batch-rdw`, `batch-ordered`, `batch-disjoint`,
`batch-ordered-missplit`, `batch-ordered-rdw`, `delimited-terminator`,
`delimited-optional-terminator`, `delimited-ascii-newline`,
`segmented-spanning`, `odo-sliding`,
`sync-slack` and `alphanumeric-payload` — and the subsections
below say what each of them cites instead. The first subsection is about
something else: which entries Appendix A's vectors are paired into, which is a
question about the entries that *are* derived from it.

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

### The framing entries cite this repository, not Appendix A

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

`delimited-terminator`, `delimited-optional-terminator`, `delimited-ascii-newline`
and `segmented-spanning` are the other four, authored against the same two
documents. Three of them — the first two and `segmented-spanning` — cite
[`docs/conformance/SPEC.md`](../../docs/conformance/SPEC.md)'s *Why the writing
direction is checked by reading, and not by comparing bytes* as well, and they
are here because that section rests on them: it says a corpus demanding the
input's bytes back would fail two of the four framings by design, and until these
arrived neither of those two framings had an entry, so a runner that byte-compared
would have passed the whole corpus.

Two of those three are what the section describes, and each one's `input.bin` is
chosen to be a file no correct writer would produce. `delimited-optional-terminator`
ends without the delimiter the placement lets a file end without, and a writer
under that placement **MUST** emit it rather than choosing whether to, so
twenty-three bytes come back as twenty-four. `segmented-spanning` lays its second
record into three segments where the largest segment allows two, and a writer
**MUST** use as few as it allows, so sixty bytes come back as fifty-six. How a
writer spreads a record's bytes across that number of segments is not something
either specification decides, and neither entry holds it to one. In both the
records are the same records, which is the property the corpus checks.

`delimited-terminator` is the control on the pair: its file *is* what a writer
produces, so it is the delimited entry that would still pass a byte comparison,
and what it adds instead is a record whose data holds the delimiter. A
`PIC S9(3)V99 COMP-3` item holding +152.50 is the bytes `15 25 0C`, which is the
vector *The extent governs, and framing is checked against it* names, and `0x15`
is what ends a record in that file. That is the section carrying the rule the
record is aimed at — a consumer **MUST NOT** determine a record's end by
searching the input for a delimiter — so it is what all three delimited entries
cite. A consumer that searched would cut that record after four bytes; one that
counts the extent reads them as the number they are.

`delimited-ascii-newline` is that collision in the file an adopter is likeliest
to hand the tool first. Its records end with `0x0A`, which is what the delimiter
of the same file becomes once it has moved to Linux, and the entry is ASCII
because that is the file line-sequential means off the mainframe. Charset governs
items and never framing bytes, so an ASCII twin of `delimited-terminator` would
only restate what `packed-ascii` and `packed-ebcdic` state about an axis that
does not apply, and would not be worth an entry. What earns this one is that the
delimiter turns up inside an item in the likelier bytes: a `PIC S9(4) COMP` item
holding 10 is `00 0A` and one holding 2570 is `0A 0A`. A consumer that split the
twenty-one bytes on `0x0A` would come back with six pieces of five, no, four,
no, no and six bytes instead of three records of six; one that counts the extent
reads the counts that were written. Its third record's count is zero and its six
bytes carry no `0x0A` at all, which is the ordinary record the other two are
read against, and the one piece a splitting consumer still gets whole. The
bytes of that count are Appendix A.5's, which is what the entry cites beside the
sections above. Its delimiter is written
as a byte string and never as a named character or a line-ending style, which is
[`docs/layout/SPEC.md`](../../docs/layout/SPEC.md)'s *A delimiter is bytes, and
it has a placement*, and its file is a `terminator` file — so, like
`delimited-terminator`, it is what a writer produces and would still pass a byte
comparison. What it catches is the search.

What none of `delimited-terminator`, `delimited-optional-terminator` and
`segmented-spanning` states is what a **wrong** *writer* does. All three are
well-formed files, so a writer that omitted the final delimiter under
`optional-terminator`, or that copied its input's segmentation instead of
relaying it, would still write a file that reads back as these records and would
still pass. The byte counts above are properties of the entries rather than
assertions the corpus makes, and what those three actually catch is a runner that
compares bytes — which is the claim `docs/conformance/SPEC.md` makes for them and
the whole of it. What a wrong *consumer* does is a separate matter and is what
the two entries above hold a delimiter inside a record for.

These cases are named here because they are known gaps rather than covered
ground. `separator` has no entry, and neither does the trailing delimiter that
announces a record which is not there under it, nor the missing one that makes a
file truncated under `terminator`; each is an error-path entry, which is a
different claim from these and belongs with the other refusals. A multi-byte
delimiter has no entry either: `0D 0A`, what the same file ends its records with
once it has been through Windows, is the delimiter a consumer is likeliest to
meet that is more than one byte long, and all three delimited entries here carry
a delimiter of one.
And a segment control code of `0` — a complete, unspanned record inside a spanned
dataset — cannot occur in `segmented-spanning`, because its one record type is
longer than its largest segment; a second record type is what would reach it.

### The batched-order entries cite this repository, and carry both outcomes

`(+ (seq HEADER (* DATA)))` — a header, then details until the next header — is
the commonest shape a batched extract takes, and this format refused it until
#332: two records are eligible at the state after a header, and where the two
type codes sit at different offsets or different widths one record can satisfy
both tests. `batch-fixed` is the same construction with a trailer on the end,
and the trailer is exactly what makes it separable, so it exercises the easy
case and nothing else. Four entries carry the shape the format now admits, and
they are four rather than one because it has two outcomes and one of them has a
cost worth writing down.

`batch-ordered` and `batch-disjoint` are the pair. Both are a forty-byte header
and a forty-byte detail; both key on runs that share no byte and that differ in
offset *and* in width, a three-byte type code against a one-byte one ten bytes
away. What differs is the item covering each run **inside the other record**. In `batch-ordered` both are `PIC X`, so no byte is ruled out of either
and the copybooks prove nothing: the pair rests on the order, which is the
header's test first because its run begins at the earlier byte. In
`batch-disjoint` both are unsigned numeric `DISPLAY`, so every byte of each is
one of the charset's ten digit bytes, neither record can carry the other's
literal, and the two tests are exclusive — the proof
[`ir/SPEC.md`](../../docs/ir/SPEC.md#when-two-match-and-when-none-does) takes
from the copybooks, in both directions because one direction alone proves
nothing.

That the descriptor carries no marker saying which of the two a pair is, is the
point rather than an omission, and it is why the two entries are worth having
side by side: what says so is the copybooks. `batch-disjoint`'s state carries the
*detail's* transition first — the reverse of `batch-ordered`'s, because the
detail's run is the earlier one there — and the file reads the same either way,
because where the pair is proved exclusive the order at that state cannot be
observed at all. Where it is not proved, that order is the whole of what reads
the file.

`batch-ordered-missplit` is what resting on the order costs. Its third record is
a detail whose account number begins `HDR`, which is the header's literal at the
header's own offset, so the state's first test matches and the record is read as
a header: the batch splits in the wrong place, the detail's remaining items are
read as the header's, and the file is reported complete. **The entry records that
as the expected answer.** Nothing diagnoses it — those bytes matched a predicate
the descriptor carries, so the record is not an undescribed one, and both record
types share an extent, so the framing has nothing to disagree with either — and
an outcome no entry writes down is one two generators can disagree about while
both pass.

**Which framings they run under.** `batch-ordered-rdw` is the ordered pair behind
the record descriptor word, and the claim is worth its entry: the word in front of
a record is consumed before the automaton is handed the record's bytes, so a
framing carrying a length changes neither which runs the two predicates read nor
the order the state carries them in. `delimited` and `segmented` are skipped, and
what is skipped is a third and a fourth spelling of that same independence — each
would cost a hand-authored descriptor and pin nothing `batch-ordered-rdw` does
not, while the framings themselves are covered by four entries of their own. The
one thing a framing could add is not so much skipped as absent from the corpus
altogether: where two record types have *different* extents, the length in front
of a record disagrees with a mis-split and the framing catches what the order
cannot ([*The extent governs, and framing is checked against
it*](../../docs/ir/SPEC.md#the-extent-governs-and-framing-is-checked-against-it)).
All four entries keep the two extents equal, deliberately, because that is the
arrangement the mis-read survives — so that claim has no entry under any framing,
and it is a known gap rather than covered ground.

**The writer's half of the permission is not an entry, and could not be.** A
producer admitting this pair owes the check
[*A writer walks the same
automaton*](../../docs/ir/SPEC.md#a-writer-walks-the-same-automaton) requires: at
such a state a writer evaluates the predicates of the transitions ordered before
the one it took, against the bytes it is about to emit, and reports a record any
of them matches rather than emitting it. An entry cannot state that outcome. A
values document is one set of values and both directions are held to it, and its
`failure` is a read that stopped — so a writer refusing a record the reader read
cleanly has nowhere to be written down, which is [*A file the reader
refused*](../../docs/conformance/SPEC.md#a-file-the-reader-refused) seen from the
side it does not cover. The case lives beside the harness instead, as a fixture
at `internal/conformance/goadapter/testdata/writer-refusal` and a test that loads
it, and what that test asserts is what an entry would: the run disagrees, and the
disagreement is the writing direction's. Reaching it at all needs a record whose
bytes differ between being read and being written, which the lenient packed sign
nibble `A` supplies — `packed-ascii` is where this corpus states that it is
positive, and a writer emits `C`.

### The variable-table entry cites the two specs that decide it

`codec/SPEC.md` says of `OCCURS DEPENDING ON` only that it makes the record's
length variable, and it says so while placing record formats out of scope. Where
the items *behind* such a table go is therefore not a question Appendix A has a
vector for, and `odo-sliding` is authored against
[`docs/ir/SPEC.md`](../../docs/ir/SPEC.md)'s *An item after a table slides, and
the other reading is a fixed table* and
[`docs/layout/SPEC.md`](../../docs/layout/SPEC.md)'s *The `OCCURS DEPENDING ON`
reading is one statement per layout* — the fork, and the statement a layout
carries to settle it. The bytes of its items are still `codec/SPEC.md`'s, which
is what the entry cites beside them.

It is the corpus's only entry whose records are not all the same length, and
that is the point rather than a side effect. Every other entry's record extent is
a constant a consumer could have hardcoded; here the item behind the table is
six bytes further along in the second detail record than in the first — two
occurrences of three bytes, at record offset 5 against 11 — so a consumer that
resolved the table at its declared maximum, or at the first record's count,
reads `DTL-TAIL` out of the wrong bytes. That is what
[`packed-comp6`](packed-comp6) does for a one-byte overread, done for one whose
size is data.

It is also the corpus's only entry with a register in it. The header's count
governs the records that follow it rather than the record it sits in, which is
what makes it the automaton's memory — the register, the binding that writes it
and the guards that read it, from *The automaton remembers, in registers* and the
*Appendix: A counted run, as nodes* that names them. A file of two counts, one
per kind, is what puts both in one entry: `HDR-DETAIL-COUNT` becomes a register
because it counts records, and `DTL-LINE-COUNT` stays a field because it counts
occurrences inside the record holding it.

### The slack entry cites a warning rather than a vector

`codec/SPEC.md` is where `SYNCHRONIZED` is written down, and what it says about
it is a warning to a *reader*: a record is not the simple sum of its fields'
widths, because slack bytes are inserted ahead of an aligned item and move
everything after them. There is no vector to derive from that, and there could
not be — a vector states the bytes a value is written as, and COBOL says nowhere
what is in an alignment gap. So `sync-slack` is authored against the warning and
against the two sections that turn it into something a descriptor carries,
[`docs/ir/SPEC.md`](../../docs/ir/SPEC.md)'s *Slack is a node, not a rule* and
*Slack survives a read*. The bytes of its items are still `codec/SPEC.md`'s,
which is what the entry cites beside them.

It is the corpus's first entry to carry a `Slack` node. The record is twenty
bytes and its items cover sixteen: one byte ahead of a halfword-aligned `COMP`
item and three ahead of a fullword-aligned one, with items in front of each run
and behind it. That is what makes a dropped run visible rather than merely
present — a consumer that leaves the three-byte run out reads `SYN-AMOUNT` from
bytes 9 to 12, which is the three bytes of the run and then `SYN-AMOUNT`'s own
first byte, and `SYN-TAIL` behind it moves with it. Neither is a byte of
`SYN-CODE`, which ends at 8 and is read correctly either way: it is the items
*behind* a run that a dropped one moves, which is why both runs have one.

The two runs are of **different widths** on purpose. Retention pairs runs to
nodes by position and nothing else — a slack node has no name and the IR gives a
caller no offset to recognise one by — so a consumer that retains both runs and
hands them back in the other order is wrong in a way no value can show. Because
the widths differ it is caught anyway: a writer emits a node's width exactly and
reports a run of any other length rather than truncating or padding it, so the
swap arrives as a refused record.

What the round trip states here is that the records the reader produced went
back through the writer unchanged, that the writer emitted a run of exactly each
node's width for both nodes, and that the file it produced reads back as the
same two records. Under `recfm F` that is a real claim about the bytes: a record
written without its slack is four bytes short, and the second record of the file
is then read from the wrong place. It is also the first time the harness's own
decision to hand the reader's record objects to the writer, rather than
rebuilding them from a values document, has had anything riding on it.

What it does **not** state is that the retained bytes are the *same* bytes. A
consumer that keeps each run's width and fills it with zero writes a file that
differs from the one it read and decodes to these values all the same, and
[*Slack survives a read*](../../docs/ir/SPEC.md#slack-survives-a-read) says as
much in as many words: what makes retention worth a requirement is that nothing
fails while that happens. A values document cannot see it either, by
construction — [*Slack is not a
value*](../../docs/conformance/SPEC.md#slack-is-not-a-value) keeps those bytes
out of one so that two generators are held to agreeing about data rather than
about padding. Holding a generator to the bytes themselves is `cpybkc-gen-go`'s
own `internal/orders` round trip, which can reach the unexported runs this
corpus deliberately cannot.

What this entry can do is make the difference *legible*, and that is why the
four runs are written down here as well as laid into `input.bin`. `input.bin` is
the one member of an entry a diff cannot show, so a claim about its bytes is a
claim nobody reviewing a change can check unless it is also in text:

| | run at 3 (1 byte) | run at 9 (3 bytes) |
|---|---|---|
| record 1, `K01` | `e7` | `5a 6b 7c` |
| record 2, `K02` | `1f` | `c3 d4 e5` |

All four differ, none is zero and none is a space, so a generator that filled
them instead of keeping them is visible to somebody holding a written file
against this one — which is the most a corpus that compares values can offer.

### The payload entry cites the three specs that decide it, and no vector

Appendix A tabulates a byte string against the value it decodes to, and every
one of its alphanumeric rows is characters. There is no row for an item that is
not text, because whether a `PIC X` item is text is not a fact about the bytes
that a table of bytes could carry — it is a fact about the file, which the
adopter has and the copybook does not. So `alphanumeric-payload` is authored
fresh against [`docs/layout/SPEC.md`](../../docs/layout/SPEC.md)'s *A byte is
not a character, and such an item has no charset*,
[`docs/ir/SPEC.md`](../../docs/ir/SPEC.md)'s *An item with no charset carries
bytes, not characters*, and
[`docs/conformance/SPEC.md`](../../docs/conformance/SPEC.md)'s *An item with no
charset is a run of bytes as well* — the layer that spells it, the layer that
resolves it, and the layer that writes the value down.

Its bytes need no derivation, which is the entry's whole shape: a payload item's
value is the bytes of the file at its offset, so `input.bin` and `values.json`
are one fact written twice, once as bytes and once as base64. What has to be
checked by hand is that they are the same fact, and the entry is arranged so
that a wrong reader cannot accidentally produce the right answer. `TXN-BYTES`
holds all 256 values, so a generator that translated it through any charset —
including the identity one, which is what this file declares — differs
somewhere. `TXN-PAD` holds `0x20` and `0x40`, the bytes an ASCII and an EBCDIC
file pad an alphanumeric item with, so a generator that trimmed a payload loses
one of them and shortens the item. `TXN-STATUS` and `TXN-REGION` are the issue's
own two items, `0x03` and `0x93`: one of the twenty bytes a text conversion
leaves alone, and one that a text conversion moved and that `encoding/json`
writes as a control character no viewer draws.

`TXN-KEY` and `TXN-NAME` are the control. They are `PIC X` items in the same
record, under no override, and they are still characters — `TXN-NAME` is padded
to six bytes and comes back as two, which is the trailing-space rule holding
where it still applies. An entry of payload items alone would pass for a
generator that had stopped reading charsets altogether.

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
