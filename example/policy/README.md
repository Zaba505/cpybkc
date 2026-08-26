# The policy example

A daily policy administration extract carried from a layout to bytes. It is the
**wide, sparse** example: eleven record types, one copybook member, and a
generated package whose record structs hold 197 fields between them, no more
than twenty of which any single record carries.

This is one of the examples under [`example/`](..), and it is a cpybkc project of
its own — [`cpybkc.json`](cpybkc.json) here names one layout and the generators
run over it, exactly as an adopter's own project does. [Why the manifest is per
example](../README.md#one-manifest-per-example) is the index's to say.

| What a caller writes | What cpybkc writes |
|---|---|
| [`cpybkc.json`](cpybkc.json) — the project manifest | [`policy/`](policy) — the generated Go package |
| [`policy.sexpr`](policy.sexpr) — the layout | [`graph/graph.md`](graph/graph.md) — the generated diagram of the automaton |
| [`pxtract.cpy`](pxtract.cpy) — the one copybook the layout names | [`cpybkc.gen.json`](cpybkc.gen.json) — the record of what was generated |

Nothing here is hand-written except those three inputs and this file. There is
no `roundtrip_test.go` beside the generated package and no `parquet/` module
under it: the first exists in [`ledger/`](../ledger) because a credit posting's
retained slack is unexported and no test outside the package can reach it, and
this example has no slack to reach; the second is [the story after this
one](https://github.com/Zaba505/cpybkc/issues/313).

## What this teaches that `ledger/` cannot

[`ledger/`](../ledger) is the **deep** example. Six record types come out of one
`01`-level by three `REDEFINES` over two independent runs, discrimination happens
at two different offsets, a header counts the records that follow it, and slack a
credit posting never describes survives a round trip. Every one of those is a
shape that is hard to *describe*, and the file is sixteen columns wide.

Sixteen columns is too narrow to exhibit what a real extract costs. This example
is the other axis, and it is deliberately plain on every axis `ledger/` is hard
on — no `REDEFINES`, one discriminator offset, no register, no slack — so that
the only thing that varies between the two examples is the one thing this one is
for.

Five things follow from that, and none of them is visible in a sixteen-column
file.

**A merged table is wider than the fields the file holds.** The eleven record
types declare 197 fields between them, and only 127 of those are distinct
concepts. The other **70 columns are a field some other record type also declares
under its own prefix** — `PLC-POLICY-NUMBER`, `INS-POLICY-NUMBER`,
`LOC-POLICY-NUMBER` and six more. COBOL has no inheritance, so a shared key is
repeated per `01`-level, and a merged table cannot collapse them because they are
not the same data-name.

Not every one of the 70 is the *same* field, and one pair in this copybook is
deliberately not: `PLC-WRITTEN-PREMIUM` is `S9(9)V99` and `FTR-WRITTEN-PREMIUM`
is `S9(13)V99`, a policy's premium against a file control total. They share a
name and neither the width nor the grain, and a converter that collapsed them
would be wrong to — which is the same reason none of the other 69 may be
collapsed either. That is exactly the 60–80 duplicated fields
[#304](https://github.com/Zaba505/cpybkc/discussions/304) measured on the
production file this example is scaled from, arrived at the same way.

**Every row is nine tenths empty.** The widest record type populates 20 of 197
columns; the file header populates 9 and the trailer 8. There is no row anywhere
in this file that fills more than 10.2% of the table it merges into.

**The cost of a column is paid on the rows that do not have it.**
[#304](https://github.com/Zaba505/cpybkc/discussions/304) measured about five
bytes per row per optional column in the Parquet writer's buffers, paid on values
that are *absent* — definition and repetition levels exist whether or not there
is a value under them. At 197 columns a detail row carries 20 values and pays for
177 absences, which is roughly 885 bytes of buffer against a **256-byte record**.
The writer's peak memory is then tracking the schema rather than the data, and it
is linear in the column count with the row count held fixed. [The column count
was chosen for that](#how-many-record-types-and-why-eleven).

**A file's record types can be its `01`-levels.** `ledger/` shows the other
arrangement, and both are real. This one is what produces sparsity at width: nine
detail types that share a key and share almost nothing else.

**FB is a framing too.** `ledger/` is `VB`, so every record sits behind a record
descriptor word stating its length. This dataset is `FB`, where nothing states a
length and a record's extent *is* its width — which is why every `01`-level in
[`pxtract.cpy`](pxtract.cpy) ends in a `FILLER` out to LRECL, and why the
generated package's reader is the unframed one.

It has one consequence worth knowing before an adopter writes this file rather
than reads it. `filler` is unexported, so a record *built* in Go carries no bytes
for its padding and the writer emits zeros — 198 of the file header's 256 bytes.
A COBOL program writing the same record would have moved spaces and emitted
`0x40`s. A record that was **read** carries the bytes it was read from and
emits those instead, so a round trip is unaffected; it is only synthesis that
differs, and `codec.go`'s `zeroFill` says why zero rather than a space is the
right byte to choose when there is nothing to choose from.

## The file

`PXTRACT` is what a P&C policy administration system unloads nightly for the data
warehouse: a file header, then every policy in the book, then a file trailer.
Behind each policy come its own detail records — the named insureds, the insured
locations, the vehicles, the drivers, the coverages, the premium transactions,
the claims and the endorsements — in whatever order the unload wrote them.

`RECFM=FB`, `LRECL=256`, `BLKSIZE=27904`, EBCDIC (`cp037`), packed decimal for
every money field. 27904 is the half-track block a 3390 takes and is 109 records
of 256; `resolve` checks that LRECL divides it — that it is a whole number of
records — and then drops it, because a stream that has arrived on a filesystem
carries no blocks.

Sparsity is a property of the *book of business*, not of the format. An
automobile policy carries vehicles and drivers and no locations; a homeowners
policy carries locations and neither. Nothing tells a reader which it will be,
so the table has to hold both.

## The two decisions this example makes

Neither is settled by [`docs/layout/SPEC.md`](../../docs/layout/SPEC.md), which
is why they are argued here rather than assumed.

### One `01`-level with `REDEFINES`, or several `01`s with a type code

**Several `01`s.** Real files carry both arrangements and `ledger/` is already
the first, so this example would teach nothing new by repeating it — and, more
than that, the second arrangement is the one that *produces the shape this
example is for*.

The reason is the key. Under one `01`-level, every record type is a description
of the same storage, so the fields they share are one field: `PST-ACCOUNT` is at
offset 0 for a debit posting and for a credit posting because it is the same
item. There is nothing to duplicate and the merged table is as wide as the
widest record type. Under several `01`-levels there is no shared storage at all,
so the shared key is *declared once per record type* under a prefix of its own,
and the merged table is as wide as the sum of them.

Ninety-five of this file's 197 columns are a field some *other* record type also
carries, and the 70 above are what is left once each of those concepts is counted
once. None of them can be collapsed by a converter, because
`PLC-POLICY-NUMBER` and `INS-POLICY-NUMBER` are two data-names.

It also decides the discriminator offset, which is why this layout has one and
`ledger/` has two. A type code that opens the record is the natural spelling when
each record type is its own `01`-level — the code is the first field the
`01`-level declares and nothing sits in front of it. `ledger/`'s postings get
their second offset from the opposite arrangement: the alternatives of one
`01`-level all begin with the key they share, so the code that tells them apart
has to sit behind it.

`docs/layout/SPEC.md` calls the arrangement here [the ordinary
case](../../docs/layout/SPEC.md#many-records-may-name-one-copybook-and-two-may-name-one-item)
— "a shop keeps the record types of one file in one copybook, an `01`-level
apiece" — and this is the first example in the repository that is it.

### How many record types, and why eleven

**Eleven: a header, a trailer and nine detail types, at 197 columns.** The
production file is 21 types over 429 columns. The count here is scaled down from
that, and there are two properties it has to keep. Each detail type adds twenty
columns, so the whole question is how many detail types:

| Detail types | Record types | Columns | Duplicated columns | Absent-value cost per row |
|---:|---:|---:|---:|---:|
| 1 | 3 | 37 | 6 | 85 B — 0.3× the record |
| 2 | 4 | 57 | 12 | 185 B — 0.7× |
| 3 | 5 | 77 | 21 | 285 B — 1.1× |
| 5 | 7 | 117 | 36 | 485 B — 1.9× |
| 7 | 9 | 157 | 49 | 685 B — 2.7× |
| 8 | 10 | 177 | 58 | 785 B — 3.1× |
| **9** | **11** | **197** | **70** | **885 B — 3.5×** |

**The first property is that absences cost more than the record.** A row costs
about five bytes per absent optional column and a record of this file is 256
bytes, so the writer's buffers stop being about the data once

```
(columns − 20) × 5 > 256,   i.e. columns > 71
```

Below that a wide table is an inefficiency; above it, peak writer memory is
governed by the schema and the row-group size and barely at all by what is in
the file. `ledger/` sits at 0.8× and is on the wrong side by construction, which
is what [#304](https://github.com/Zaba505/cpybkc/discussions/304) said and what
this example is here to make demonstrable.

But that line is crossed at **three** detail types, and it is therefore *not*
what fixes the count. Saying so is the point: a five-record-type example would
already be past the threshold, and it would still not be this file.

**The second property is the duplication, and that is the binding one.** The
production file carries 60–80 fields duplicated across its types, and the table
above is the only place the count is actually pinned: eight detail types give 58
duplicated columns, which is below the band, and nine give 70, which is inside
it. Nine is the first count that lands there. That is why the ratio ends up at
3.5× rather than at some number chosen for being large — the ratio is a
consequence of the count, and the count is a consequence of the duplication.

The bound from above is the diff. Every generated byte of an example is checked
in and reviewed, and the generated tree here is already 12,394 lines against
`ledger/`'s 2,593. Twenty-one record types would more than double that again for
nothing the eleven do not already show, both properties being met at nine. So
eleven is the smallest count meeting both, and it is a copybook a person can
read end to end in one sitting — 295 lines, of which 208 are a `PIC` clause.

The production file sits at about 9.7× on the first property. This one is less
extreme and on the same side of the line, which is the honest claim.

The overlap between the types was fixed the same way. Each detail type repeats a
five-field policy key and declares fifteen fields of its own, so **43** of the 70
duplicated columns are that key — the record-type code, the carrier, the policy
number, the term effective date and the sequence number, written once per record
type — and the other **27** are concepts two or more types happen to share: a
location number on four types, a coverage code on four, a name and a birth date
on the two types that describe people. That is the shape of the real file's
duplication rather than a number copied from it.

## What the copybook is, and what it is not

[`pxtract.cpy`](pxtract.cpy) is written as the mainframe would hold it: fixed-form
COBOL in the source columns, uppercase, banner comments, a `FILLER` closing every
`01`-level out to LRECL, and prefixes on every data-name because two `01`-levels
in one member may not share one.

It has not been edited to make the layout tidier, and there is one place that
shows. `COV-LOCATION-NUMBER` and `COV-UNIT-NUMBER` are both present on every
coverage record and at most one of them is ever meaningful — a coverage hangs off
a location, or off a vehicle, or off the policy itself. A copybook designed for
this layout would have made that a `REDEFINES` or a variant; a copybook that
arrived from a policy administration system has two numeric fields and a
convention that the inapplicable one is zero. The layout narrows and the copybook
never moves: the copybook is [the record's
description](../../docs/layout/SPEC.md#overview) and it arrives from the system
that writes the file, so describing what it says is cpybkc's job and improving it
is nobody's. An example whose copybook was tidied to make its layout read better
would be an example of a file that does not exist.

## The diagram

[`graph/graph.md`](graph/graph.md) is the automaton, and at this width it is
worth looking at for a reason `ledger/`'s is not: it is **big**, and its size is
the sequencing expression's rather than the file's. Ten of the eleven record
types are admissible at the point behind any detail record, so the inner
repetition draws eight states each carrying ten outgoing edges. There are 197
fields in this file and not one of them appears in the diagram except the eleven
type codes, because the automaton is about which record comes next and nothing
else.

Held against [`policy.sexpr`](policy.sexpr) it is the check an adopter makes
before trusting a generated reader: every edge is a record type the layout
admits at that point, every edge carries the EBCDIC bytes of its type code
(`0xD7 0xD3 0xC3` is `PLC` in `cp037`), and no state offers two edges that could
both match.

## Regenerating

From the repository root:

```sh
go build -o "$(go env GOPATH)/bin/cpybkc-gen-go" ./cmd/cpybkc-gen-go
go build -o "$(go env GOPATH)/bin/cpybkc-gen-graph" ./cmd/cpybkc-gen-graph
go run ./cmd/cpybkc --manifest example/policy/cpybkc.json
```

Then commit what changed. [`../regenerate_test.go`](../regenerate_test.go) is
what fails if you do not: it regenerates this example from the inputs beside it
and holds every byte of the result against what is checked in.
