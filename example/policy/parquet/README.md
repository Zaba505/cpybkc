# The wide sparse example, converted to Parquet

[The policy administration extract beside it](../pxtract.cpy) converted,
compiled, and tested: eleven record types, 197 columns, **one Parquet file**.

The [ledger conversion](../../ledger/parquet) is where the *schema design* is
argued — one table or several, what becomes of a parent grain, whether to mint a
key, where the semantics stop. That argument is not repeated here, and this
conversion follows it.

What this one is for is the other question, the one a sixteen-column file cannot
ask: **what does one wide sparse table cost to write, and how do you pay it?**
The answer is a rule with three inputs, all of which are in the copybook before
any Go is written, and a row-group size derived from it rather than chosen.

**It is a reference and not a recommendation**, and it is **not a library**.
`convert.go` is `package main` so that nothing here can be imported: a conversion
is a schema design, and a schema design is not a function of the source.

```console
$ GOMEMLIMIT=256MiB go run . -in pxtract.dat -out /tmp/pxtract/pxtract.parquet
```

`GOMEMLIMIT` is part of the invocation and not decoration; see
[GOMEMLIMIT](#gomemlimit-the-runtime-will-not-infer-it) below. `-out` names the
file, defaults to `pxtract.parquet` in the working directory, and the directory
it sits in is created if it is not there. It is clobbered whether or not the run
succeeds — `os.Create` truncates and a failed conversion removes what it
truncated, so that what is left is no file rather than a footerless one somebody
has to know to distrust.

`pxtract.dat` is *a dataset of these records*, and this repository commits none:
an extract is cp037 on a fixed-block dataset, which is not a thing to read in a
diff. `convert_test.go`'s `encoded` makes its fixtures through `policy.Writer`,
and that is also the shortest way to make yourself one.

## Its own Go module

`parquet-go` must not reach the root `go.mod` — the module `go install
github.com/Zaba505/cpybkc/cmd/cpybkc@version` builds, and from there a signed,
attested, distroless release image. [`example/policy/policy`](../policy) has no
`go.mod` of its own, so a conversion written beside it would land its dozen
transitive requires in the CLI's build list. The argument is
[the ledger conversion's](../../ledger/parquet/README.md#its-own-go-module-and-that-is-the-load-bearing-decision)
in full; `module_test.go` here is the same assertion over this module's own
`go.mod`, and it is a copy rather than something shared because a third module
existing so that two examples can assert three sentences is a worse tree than two
copies of them.

`dagger call example-parquet-ci` checks it, along with every other module nested
under `example/`. That stage globs for `go.mod` rather than naming a path, so
this module is covered by having been written.

## One file, and that is decided here rather than weighed

Every record type is one schema, and one schema here is one file.

The reason is what the format is *for*. Parquet is column-oriented to serve OLAP
lakes: a query touching five of four hundred columns reads five, a column null
for every row of a row group compresses to nothing, and a wide sparse table is
the primary thing that design exists to serve. The measurements in
[#304](https://github.com/Zaba505/cpybkc/discussions/304) bear it out from the
other side — the real 429-column file this example is scaled from compresses **6×
on disk** behind a 170 MB footer.

So sparsity is nearly free on read and on disk, and expensive only on the writer,
once, at conversion time. That is what makes "how much does this cost to write"
the whole subject and "should I write it" a question already answered.

### Splitting by record type is declined, and here is the read-side reason

Eleven narrow tables is a real option, and for some consumers it is better: a
premium table with nothing nullable in it is easier to read and much cheaper to
write. Measured on the production file, splitting is worth roughly **20× on write
time**, and an order of magnitude on peak memory if the tables are written one at
a time.

It is declined anyway, because **the lake wants one table**. Ask "every record
touching this policy term" of eleven tables and you have written a ten-way union;
ask it of one and it is a predicate. Ask "which policies have a claim and no
endorsement" and the merged table answers it in one scan of two columns. The
column count is what a query pays for and it pays for the columns it names, so
the nine record types it does not touch cost it nothing.

The write cost is real and it is stated in full below rather than left for an
adopter to discover. What must not happen is reading merging as the compromise:
it is the ordinary answer, and splitting is the exception for people whose
consumers genuinely want narrow tables.

### One table and one file are separate axes

**This conversion writes one file. That is not the same decision as writing one
table, and it is not an argument that partitioning is wrong.**

A lake table is normally one schema across many files — partitioned by date, by
carrier, by whatever the query pattern wants — and a converter that wrote a file
per cycle date would still be writing one table. Splitting by *record type* is
what this example declines; splitting one schema across *files* is a decision
about the shape of the output that this example deliberately does not make.

There are three reasons to make it that have nothing to do with what is argued
here — partitioning, parallel reads, and restartability, since a single file
holds its whole footer until `Close` and a crash at 99% loses the run rather than
the last file. Memory is **not** one of them: the arithmetic below sizes a single
file to 71 million records inside 256 MiB, and the ceiling moves with the budget.

## The schema

Eleven optional groups at the top level and nothing else: no key is minted,
nothing is denormalized onto anything, and every column of this file is a field
of `pxtract.cpy`.

```
px_file_header    9 columns      px_coverage      20
px_policy        20              px_premium       20
px_insured       20              px_claim         20
px_location      20              px_endorsement   20
px_vehicle       20              px_file_trailer   8
px_driver        20              ─────────────────────
                                 197 columns
```

**Exactly one group is non-nil on any row**, which is what keeps the record type
recoverable from a row without consulting anything else
(`TestARowIsOneRecordAndCarriesItsTypeWithIt`). The eleven groups are structure
and get no column of their own, so the file is 197 columns wide —
`TestTheColumnCountIsTheSchemaAndNotANumberWrittenDown` reads that number out of
a written file rather than trusting it, because it is an input to the arithmetic
below.

**The grain is one record.** This file has three grains and the merged table has
one row per record, so the file header and the trailer are rows like any other
rather than a denormalization or a second table. That is what a merged table buys
which the ledger's narrow one could not: there the trailer's control totals had
to be dropped entirely, because a summary denormalized onto every row makes
`SUM(...)` return the total times the row count. Here the trailer is one row, the
summary is on it once, and an aggregate over the column returns the file's own
number exactly once — `TestTheControlTotalsDoNotPromote`.

It has a cost, and it is the honest one to name: a detail row carries the policy
key and **not** the cycle date, so a query that wants both joins within the file
on the policy key, or partitions by cycle at the directory level. An adopter who
would rather pay nine more columns on every row than write that join should
denormalize the header — the ledger conversion is the worked version of exactly
that — and should know what it costs: every column added is five more bytes a row
in W below, before any value goes in it, on every row of the file.

**Nested, not flattened.** `px_premium.prm_written_amount`, not
`px_premium_prm_written_amount`. Flattening collapses a group `A-B` holding `C`
onto a group `A` holding `B-C`, so it needs a collision rule, and a collision rule
is a decision you will be making about somebody's field names at three in the
morning. It buys **no memory**, and that is worth saying because it looks like it
should: every leaf under an optional group is an optional column and pays its five
bytes a row either way.

### What is reconciled, and what deliberately is not

The trailer is "the control totals the receiving system balances the extract on",
and four of its eight items are checked before the footer is written — so a file
that does not balance leaves no readable table rather than one a query would
happily return wrong answers from.

| item | what happens to it |
|---|---|
| `FTR-POLICY-COUNT` | checked against the `PX-POLICY` records read |
| `FTR-DETAIL-COUNT` | checked against the eight detail types read |
| `FTR-CYCLE-DATE` | checked against `FHD-CYCLE-DATE` |
| `FTR-RUN-NUMBER` | checked against `FHD-RUN-NUMBER` |
| `FTR-WRITTEN-PREMIUM` | **not checked** — stored as read |
| `FTR-PAID-LOSS` | **not checked** — stored as read |
| `FTR-HASH-POLICY-NUMBER` | **not checked** — stored as read |

The three that are not checked are not an oversight, and
`TestTheMoneyTotalsAreNotReconciled` asserts they are not.

Nothing in `pxtract.cpy` says **which grain** `FTR-WRITTEN-PREMIUM` totals.
`PLC-WRITTEN-PREMIUM` is `S9(9)V99` on a policy term, `PRM-WRITTEN-AMOUNT` is
`S9(9)V99` on a premium transaction, and the trailer's own is `S9(13)V99` — one
name over two grains and two widths, which the [sibling
README](../README.md#what-this-teaches-that-ledger-cannot) already names as the
pair that must not be collapsed. A converter that picked one would fail on every
file that meant the other. `FTR-HASH-POLICY-NUMBER` is worse: it is a hash total
over `PIC X(12)`, so reproducing it means knowing how the producing system turns
an alphanumeric key into a number, and no copybook carries that.

This is the same line the ledger conversion draws and it lands on the other side
of it. There, `TRL-NET` admitted exactly one reading, the assumption was written
down, and the check failed loudly on the first file that disagreed. Here there is
no single reading to assume. **A check that has to invent a semantic is a check
that fails on files that are fine**, and a converter nobody trusts is worse than
one that stores a number and says it did not verify it.

The two counts are different in kind: `PX-POLICY` and the eight detail types are
record types the layout names, so counting them invents nothing.

## What it costs to write

Everything in this section is [#304](https://github.com/Zaba505/cpybkc/discussions/304)'s
measurement applied to this schema. None of it is measured *here* — a committed
harness is [#314](https://github.com/Zaba505/cpybkc/issues/314) and the run at
millions of records is [#315](https://github.com/Zaba505/cpybkc/issues/315). What
is checked here is that the conversion's constants are the ones the rule takes
and that the number falls out of them:
`TestTheRowGroupBoundIsDerivedAndNotChosen`.

### The rule

For **N** records at **R** records per row group over a schema of **C** columns
at **W** bytes a row:

> **peak(N, R) ≈ a·C·(N/R) + W·R**

The first term is footer you have already accumulated. A Parquet footer carries a
`ColumnChunk`, a `ColumnIndex` and an `OffsetIndex` **per column per row group**,
and none of it can be written until `Close` — `writeRowGroup` appends to
`w.rowGroups`, `w.columnIndexes` and `w.offsetIndexes` and nothing releases them.
The second term is the row group you are still holding, which `writerColumn.reset()`
drops every time one closes.

Both terms are the **format's**, not `parquet-go`'s. They pull opposite ways, so
there is a bottom, and the rule is what it says:

> **Size the row group so that the footer you have accumulated is the same size
> as the row group you are holding.**

Minimising over R gives `R* = √(a·C·N/W)` and `peak* = 2·√(a·C·W·N)`.

### a — what a closed row group retains, per column

**a ≈ 1 KB**, and read it as **1–3 KB**.

#304 measured 975, 988 and 943 bytes per column per row group across schemas of
4, 16 and 32 columns — flat to 5% over an eight-fold range — and 1,878 to 2,960
on schemas whose values are wide, because the minimum and maximum are retained
twice, once in `Statistics` and once in the `ColumnIndex`.

A kilobyte is the bottom of that range and it is the right end for this file: no
item in `pxtract.cpy` is wider than 40 bytes and most are under ten. Being wrong
by the whole range moves R\* and peak by √3, which the curve absorbs — see
[the neighbourhood](#it-does-not-have-to-be-hit-but-it-does-have-to-be-computed).

### W — and its per-column term, which is the larger correction

> **W ≈ 5·C_optional + the record's own bytes**

`parquet-go`'s `optionalColumnBuffer` holds `rows []int32` **and**
`definitionLevels []byte` (`column_buffer_optional.go:22`): **five bytes per row
per optional column, whether or not there is a value under it.** Definition
levels exist to say a value is absent, so absence is not free — it is the same
price as presence, paid in the writer's buffers.

Every one of this schema's 197 leaves sits under an optional group, so every one
of them is an optional column and every row pays for all 197:

```
5 × 197  =  985 B    definition levels and row offsets
            256 B    the record's own bytes, bounded by LRECL
             15 B    per-record overhead of the buffered form
          ───────
W    =    1,256 B a row
```

**Nearly four fifths of a row's cost is structure**, and on a detail record
carrying 20 values, 885 of those bytes are the 177 columns it has nothing for.
Against a **256-byte record** the writer's buffers are tracking the schema and
barely at all the data.

The record's bytes are taken at LRECL, which over-estimates every row —
alphanumeric items arrive from the generated reader already trimmed of their
`DISPLAY` padding, and a record's `FILLER` is not a column at all. Over-estimating
W sizes the row group *smaller*, which is the safe direction.

### Which makes peak linear in the column count

Where `5·C` dominates W, substituting into `peak* = 2√(a·C·W·N)` gives

> **peak\* ≈ 2·C·√(5·a·N)**

**Linear in C, not its square root.** For a wide sparse table the column count is
the whole story, and halving it halves peak memory. No writer option does that —
measured on the 429-column schema, `DataPageStatistics(false)` is **0%** (it
governs the deprecated page-header statistics, not the retained `ColumnIndex`),
`DictionaryMaxBytes` is **0%**, and `SkipPageBounds` over 415 string columns is
**4%**.

That is why this example's README says the width and the sibling's says the
sparsity: they are the same number.

### The row group, derived

`R* = √(a·C·N/W)` has **N** in it, so no single number is the optimum for every
extract. What a default can be is the optimum at the largest extract the budget
admits — where the two terms are equal *and* each is half the budget, so it is
the point at which the budget is exactly spent:

```
budget            B  =  256 MiB  =  268,435,456 B
columns           C  =  197                        the schema, checked against the file
per row           W  =  1,256 B                    5·C + 256 + 15
retained          a  =  1,024 B                    per column per row group

open row group       =  B / 2   =  134,217,728 B
default R            =  B / 2W  =  106,861 rows    = R* at maxRecords, below
```

`rowsPerRowGroup` in `convert.go` is written as that expression and not as its
value, so the arithmetic is in the code rather than beside it. **It is not
rounded**, deliberately: a round number reads as one somebody liked.

**It is the optimum at the ceiling and not at your N**, which is a real cost on a
smaller extract and is worked
[below](#it-does-not-have-to-be-hit-but-it-does-have-to-be-computed): at a
million records it holds 4.3× the memory the rule would. It stays inside the
budget everywhere, which is what a default owes; `-rows-per-row-group` is how you
take the other side of that trade.

256 MiB is an ordinary container limit for a batch job, and it is the only input
here that is a choice rather than a reading. Change it and everything below moves
with it.

### The record count at which this stops fitting

> **about 71 million records — roughly 18 GB of this extract**

The retained term is **linear in N**, so a conversion that fits today has a
record count at which it stops fitting, and this number exists for every budget
and every schema. Not knowing yours is how you find it on a Tuesday.

```
maxRecords = (B − W·R*) · R* / (a·C)  =  71,099,073 records
```

Past it, raise the budget and re-derive: peak grows as √N, so **quadrupling the
budget multiplies the ceiling by sixteen** — 1 GiB carries about 1.1 billion
records of this schema. Or halve the column count, which halves peak outright.

The other wall is a hard one and this clears it easily. A Parquet file holds at
most `MaxRowGroups = math.MaxInt16 = 32767` row groups, so a bound of R puts a
ceiling of 32767·R records on the file — 3.5 billion here, which is 49× the
memory ceiling. #304 met that wall first, at 2,097,088 rows with a 64-row bound,
and got *"the limit of 32767 row groups has been reached"* wrapped as *"flushing
64 posting rows"* — a true sentence naming neither the cap nor the thing that
moves it. `convert.go` re-reports it with both. **The ceiling and the linear heap
are one mistake with one fix, not two walls with two**: tiny row groups produce
both, and a row group sized anywhere near R\* produces neither.

### It does not have to be hit, but it does have to be computed

The curve is symmetric in log R. Being off by a factor of k multiplies peak by
(k + 1/k)/2 — **1.25× at a factor of two, 2.13× at four** — so the rule picks a
neighbourhood rather than a number, and 106,861 could be 100,000 for nothing.

What it does not tolerate is being read as **√N**, which is the misreading its
shape invites and which throws away C and W entirely. #304 measured that on its
own sixteen-column schema — not on the 429-column production file — at 46 M
records:

| | rows per row group | retained | peak |
|---|---:|---:|---:|
| R\* = √(a·C·N/W) | 105,000 | 18 MB | 56 MB |
| read as √N | 6,782 | 110 MB | 217 MB |

A factor of 15 the wrong way costs **6× on retained heap**. The model predicts
7.8× — `(k + 1/k)/2` at k = 15.5 — so it is the right order and not the right
number, which is what an `a` quoted as "1–3 KB" and a W taken at the record
length should be expected to give. The rule is a sizing rule and not a
simulation.

Two things do not carry over to this schema, and the arithmetic says why. C is
sixteen there and 197 here, and peak is linear in C — so **the same record count
over this file's columns wants gigabytes where that one wanted megabytes**. That
is exactly where splitting by record type, or rolling files, stops being
optional, and it is the reason a wide sparse table is the case that has to be
sized rather than assumed.

R\* is a function of the *extract*, not of the schema alone: N is one of the
rule's three inputs. So the number is a flag, `-rows-per-row-group`, whose default
is `R*` **at [`maxRecords`](#the-record-count-at-which-this-stops-fitting)** and
not at whatever N you have — the point that keeps the budget at the largest
extract the budget admits.

That is safe everywhere and optimal only at the top, and the gap is worth seeing
before you meet it. At a million records:

```
true R*  = √(a·C·N/W)  =   12,673 rows      peak ≈  32 MB
the default            =  106,861 rows      peak ≈ 136 MB

  of which  retained  =  a·C·(N/R)  =    1.9 MB
            buffered  =  W·R        =  134.2 MB
```

**4.3× the optimum, and all of it open row group** — 134 MB of buffers held to
amortize a footer that never reaches 2 MB. It fits the budget, which is what the
default is for. An adopter converting an extract far below the ceiling should
re-derive from this section and pass `-rows-per-row-group`; that is the whole
reason it is a flag.

### `GOMEMLIMIT`: the runtime will not infer it

**Go has no memory counterpart to the `GOMAXPROCS` it derives from cgroups.** Go
1.26 reads a cgroup CPU limit and sizes `GOMAXPROCS` from it
(`runtime/cgroup_linux.go`); `readGOMEMLIMIT` reads **the environment variable
only** (`mgcpacer.go:1416`) and defaults to unlimited.

So a conversion running in a 512 MiB container has a GC targeting twice the live
heap with no idea that 512 MiB exists, and the first thing it learns about the
limit is the OOM killer. The budget above is a budget for the *live* heap; the
collector needs to be told about it separately, by hand:

```console
$ GOMEMLIMIT=256MiB go run . -in pxtract.dat -out pxtract.parquet
```

Nothing in `convert.go` sets it. A program that set its own `GOMEMLIMIT` would be
overriding an operator who set one deliberately, and the budget it should use is
a property of where it is running rather than of what it is converting.

### The trap: `MaxRowsPerRowGroup` and a manual `Flush` are not complementary

**Use one.** Setting the option *and* keeping a caller-side batch with an explicit
`Flush` looks like belt and braces and is not: the flush closes the row group
unconditionally, so `rg.numRows` never reaches the cap and **the option never
fires**. The row group is the batch, whatever the option says.

Then raising the batch to raise the row group pays for it twice — once in
`parquet-go`'s buffers and once in a slice of Go structs. The converter #304 was
opened about did exactly this: it OOMed at a 100,000 batch and survived at
10,000, which reads as "large row groups cost memory" and is really "you bought
two of them".

`convert.go` has **no caller-side batch**. `rowsPerRowGroup` goes to
`parquet.MaxRowsPerRowGroup` and the bound is enforced inside
`GenericWriter.Write`: the row-group writer returns `ErrTooManyRowGroups` at the
cap (`writer.go:1036`), `Write` catches exactly that error, closes the group and
carries on (`writer.go:273-277`), and `Close` writes the last partial one.
`TestEveryRecordReadIsARowWritten` reads a written file back and requires one row
group per bound, none larger, over a record count that is not a multiple of it —
which is the only kind of file an unwritten last group loses rows from.

The one-row slice `convert` reuses is an argument to a call and not a batch: what
is in it has been written before the next record is read. A converter writing
millions would hand `Write` a longer slice — its per-call work is 197 column
visits a row, and a slice amortizes it — while still leaving the bound to
`parquet.MaxRowsPerRowGroup`. **The slice is an amortization and was never the
bound.**

### And the cost that is not memory

On 21 record types those columns cost **429 column visits per record against about
20**, and the production conversion runs in **23 minutes where the same input
mapped to NDJSON takes 1** — because NDJSON writes the fields that are *set*, and
Parquet materialises a value or a null for every column of every row.

That is the price of a table a query engine can skip through, and it is paid once.
It is worth knowing before you are surprised by it.

## The type mapping

Every scaled item is an annotation and **nothing here multiplies or divides by a
hundred**. `cpybkc-gen-go` writes a scaled item as the unscaled integer with the
scale in its doc comment, which is precisely what `DECIMAL(p,s)` is.

`TestEveryScaledItemIsAnnotatedDecimal` checks all twenty-one of them in the
file's own schema — which is what a query engine reads, rather than the Go struct
tag — and `TestTheScaledValuesRoundTripUnscaled` checks the integers under them.
All twenty-one rather than a representative two, because they do not share a
scale:

| item | PICTURE | Go, as generated | Parquet |
|---|---|---|---|
| `PLC-WRITTEN-PREMIUM` | `S9(9)V99 COMP-3` | `int64`, unscaled | `DECIMAL(11,2)` |
| `COV-RATE` | `S9(5)V9(5) COMP-3` | `int64`, unscaled | `DECIMAL(10,5)` |
| `PRM-COMMISSION-RATE` | `S9(3)V9(4) COMP-3` | `int32`, unscaled | `DECIMAL(7,4)` |
| `COV-DEDUCTIBLE` | `S9(7)V99 COMP-3` | `int32`, unscaled | `DECIMAL(9,2)` |
| `LOC-DISTANCE-TO-FIRE` | `9(3)V9` | `int32`, unscaled | `DECIMAL(4,1)` |
| `FTR-WRITTEN-PREMIUM` | `S9(13)V99 COMP-3` | `int64`, unscaled | `DECIMAL(15,2)` |

`LOC-DISTANCE-TO-FIRE` is the one to look at. It is `DISPLAY`, unsigned, and
scaled — a zoned item with an implied point one place in — so a converter that
took "is this scaled" from the *storage* rather than from the item would write it
as a plain integer and be wrong by a factor of ten on every row.

[The ledger conversion's four constructs this mapping does not
cover](../../ledger/parquet/README.md#four-things-this-example-does-not-carry-and-your-next-file-will)
— `COMP-5` bounded by its storage, a picture ending in `P` with a negative scale,
numeric-edited items that are strings, and `COMP-1`/`COMP-2` — apply here
unchanged. This file carries none of them either.

### The semantics stop at the copybook

`FHD-CYCLE-DATE PIC 9(8)` is plainly `YYYYMMDD` and it is written as an
**integer**. `FHD-CYCLE-TIME PIC 9(6)` holds `013000` and arrives as `13000`,
leading zero and all, because nothing in the copybook says it is a time.

Guessing here is how a postal code loses its leading zero and how a "date" that
is really a Julian day number becomes 1970. Add the semantics in the layer above
this one, where somebody can see the assumption being made.

Note also that alphanumeric items arrive from the generated reader already
trimmed of their `DISPLAY` padding, so `plc_state` is `"GA"` and not `"GA"`
behind spaces — `TestAlphanumericItemsArriveTrimmed`. That is `codec`'s decision
and not this conversion's; it is mentioned because a downstream join against a
system that kept the padding will not match.

## Not generated

Nothing here is written by cpybkc. This directory is not named in
[`example/policy/cpybkc.json`](../cpybkc.json), it is not recorded in
[`cpybkc.gen.json`](../cpybkc.gen.json), and
[`example/regenerate_test.go`](../../regenerate_test.go) does not touch it. The
two halves of an example are told apart by the `// Code generated … DO NOT EDIT.`
header, and `TestNothingHereIsMarkedGenerated` fails if a file here ever grows
one.
