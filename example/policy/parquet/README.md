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

Everything in this section is a **measurement**, and
[`memory_test.go`](memory_test.go) is where it is taken. A measurement nobody can
rerun is prose, and prose about heap stays plausible for a year while being wrong
the whole time — so the harness is committed beside the conversion, it runs in
`dagger call ci`, and every number below can be reproduced with one command:

```console
$ go test -run TestTheWriterMemoryModelHoldsItsShape -v
```

The readings first came out of
[#304](https://github.com/Zaba505/cpybkc/discussions/304), on a converter written
to produce them rather than to be read; what the harness does is take the same
three readings against **this** row type, on your machine. Two other things are
checked here and they are checks and not measurements: that the conversion's
constants are the ones the rule takes and that the number falls out of them
(`TestTheRowGroupBoundIsDerivedAndNotChosen`), and that C is the schema's leaves
and not a number written down (`TestTheColumnCountIsTheSchemaAndNotANumberWrittenDown`).
The run at millions of records is a *second* instrument beside this one —
[`scale_test.go`](scale_test.go), which sweeps the real conversion rather than
probing a writer — and it is not in `dagger call ci`. See
["Where each of these runs"](#where-each-of-these-runs).

**What CI gates is the shape and never a byte count.** The numbers are a property
of the machine, the Go version and the parquet-go pin, so `a == 1024` is not
something a pipeline can hold anybody to; what it holds is the model — that the
retained term is linear in closed row groups, that a row of this schema costs the
writer more than the record it came from, and that the curve of peak against rows
per row group has an interior bottom near where the rule puts it. The measured
values are logged beside the committed constants, so drift is visible in
`go test -v` without being a failure.

Two traps sit in front of anyone taking these readings by hand, and both report a
plausible number rather than an error, which is what makes them worth naming:

- **The writer has to stay reachable across the probe.** A `runtime.GC()` with the
  writer already unreachable collects the entire retained footer. #304 got 72 KB
  where 247 MB was live — small enough to believe, wrong by four orders of
  magnitude. `liveHeap` holds it with `runtime.KeepAlive`.
- **The sample has to land at a defined phase of the row group.** A probe that
  fires at an arbitrary fill reads the buffered term at an arbitrary fraction of
  itself, and on a sweep that halves R at every point that is a different fraction
  every time. The curve still looks like a curve. The harness samples at *m·R + 1*
  rows for the retained term and *m·R + R − 1* for the peak — the two phases at
  which "m row groups closed, and one row or R−1 rows in the open one" is true
  whichever write parquet-go closes a full row group on.

There is a third thing both probes rest on and neither could see: that row groups
are closing at all under the `a` probe, and that none is closing under the W one.
With the bytes merely discarded, "m row groups closed and their footers retained"
and "nothing ever flushed and the open group is still growing" produce the same
straight line, and the second reports an `a` that is really a fraction of W·R. A
row group's column chunks reach the sink when it closes and at no other time, so
the harness counts them: the count has to move at every sample of the `a` probe
and to stay at zero through the whole of the W one.

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

The harness measures it by writing at a **small** row group — sixteen rows — so
that the buffered term shrinks to a single row and the slope of live heap against
*closed row groups* is all footer. Over eight to sixty-four closed row groups of
this schema it reads **969 B** and the fit is straight to 1%, which is the term
the model wants and the one this section's arithmetic uses at 1,024.

It is measured at a small row group for a second reason as well as the first. What
a closed row group retains is a `ColumnChunk`, a `ColumnIndex` and an
`OffsetIndex` per column, and the last two carry **one entry per data page** — so
a row group big enough to spill more than one page per column folds a page count
into `a` and makes it a function of R. Sixteen rows of this schema is one page per
column, which is the regime the constant is quoted in.

#304 measured 975, 988 and 943 bytes per column per row group across schemas of
4, 16 and 32 columns — flat to 5% over an eight-fold range — and 1,878 to 2,960
on schemas whose values are wide, because the minimum and maximum are retained
twice, once in `Statistics` and once in the `ColumnIndex`. The 969 above is that
same number taken again, on a different machine, against this row type.

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

The harness measures W by writing one row group and never letting it close — the
bound is `math.MaxInt64`, which is parquet-go's own default and is exactly the
thing that makes an unbounded writer buffer the file it claims to stream. With
nothing flushed there are no closed row groups, the retained term is zero, and the
slope is all W. It reads **1,365 B a row** as a least-squares fit and **1,066** as
the chord across the same range, against the 1,256 the arithmetic uses.

**That spread is the reading and not an error bar.** What an open row group holds
is allocated *capacity*: a column buffer grows by doubling, so it overshoots what
is in it by up to a factor of two and then stands still until the next doubling,
and a page that reaches `PageBufferSize` spills and gives its buffer back. The
live heap is therefore a staircase that climbs, plateaus, and now and then falls —
reproducible to the byte across runs, which is what makes it a property of the
writer rather than noise. Two consequences worth carrying:

- **Measure W over a wide range or not at all.** A four-sample fit over 512–2,048
  rows read 709 B a row on the machine this was written on; thirty-two samples
  over 512–16,384 read 1,365. The first is a reading of whichever doubling it
  happened to straddle.
- **The peak is a band, not a line.** A row group sized at R holds somewhere
  between W·R and about 2·W·R depending on where its buffers last doubled, which
  is one more reason to size for the neighbourhood of R\* rather than for R\*.

So CI asserts of this term only that it grows and that it exceeds the record it
came from. The number is read off the log.

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
moves it. `convert.go` re-reports it with both.

**That wall is now reached rather than argued about**, though not here: 32,767 row
groups over 197 columns retain about 6.6 GB, which is not a thing to run. The
ledger conversion is fourteen columns wide, so the same 32,767 row groups cost it
about 465 MB and a second and a half, and
[its scale run drives a real writer into the cap](../../ledger/parquet/scale_test.go)
and holds the diagnostic to naming all three of the cap, the bound in force and
the record ceiling they imply. What is checked *here*, on every pull request, is
the wording — `TestTheRowGroupCapIsReportedWithTheFlagThatMovesIt` hands
[`tooManyRowGroups`](convert.go) an error to wrap. A wording is what rots; a wall
is what has to be hit once. **The ceiling and the linear heap
are one mistake with one fix, not two walls with two**: tiny row groups produce
both, and a row group sized anywhere near R\* produces neither.

### The bottom, observed

`R* = √(a·C·N/W)` is where the arithmetic says the two terms cross. The harness
does not take that on trust: it draws the curve, sampling peak at seven
rows-per-row-group halving down from N, each point writing the same N records and
sampled one row short of a flush. At N = 16,384 on the machine this was written
on:

```
R = 16384   peak = 22.0 MB
R =  8192   peak = 12.4 MB
R =  4096   peak =  7.9 MB
R =  2048   peak =  7.1 MB   <- observed minimum
R =  1024   peak =  7.6 MB
R =   512   peak = 10.5 MB
R =   256   peak = 16.4 MB
```

The rule, fed the `a` and `W` this same run measured, puts R\* at 1,514. The
observed bottom is at 2,048 — **a factor of 1.35, which is a 1.06× penalty on the
curve**, and the point either side of it is within 7% of the minimum. That is the
whole claim this section makes about the rule: it picks a neighbourhood.

Three things about that table are the reason CI can gate on it. The minimum is
*interior* — a sweep whose smallest reading is at an end has only seen one side of
the curve. Both ends stand well above the bottom, so the curve has a bottom rather
than being flat. And the observed argmin sits within a factor of four of the R\*
the run's own measurements predict. None of those is a byte count, and all three
fail on a model that has stopped being true.

Every reading includes the writer's fixed overhead — around 4.8 MB of column
buffer capacity for 197 columns — which is neither of the model's two terms and is
why the ratios above are smaller than the arithmetic alone would give. It is left
in rather than subtracted, because it is memory the process actually holds.

### And observed again at four and a half million, over the conversion itself

The sweep above is the harness's, at sixteen thousand records, and everything it
shows about the retained term is a rounding error at that N: the accumulated
footer never reaches a megabyte. [#315](https://github.com/Zaba505/cpybkc/issues/315)
is the run at a record count where it is the larger half, and it is a different
instrument — [`scale_test.go`](scale_test.go) sweeps `-rows-per-row-group` over
**the real conversion**, generating its input a record at a time, and probes the
live heap from inside the record source at the fullest moment of the open row
group.

At N = 4,500,002 records — 500,000 policy terms and their details, about 1.1 GB of
this extract — on the machine this was written on:

```
R =   3360   peak = 250.4 MB
R =   6720   peak = 133.7 MB
R =  13440   peak =  83.4 MB
R =  26880   peak =  75.0 MB   <- observed minimum
R =  53760   peak =  95.0 MB
R = 107520   peak =  99.3 MB
R = 215040   peak = 110.6 MB
```

> **The rule's answer is the measured best.** `R* = √(a·C·N/W)` at this N is
> **26,884** rows, and the sweep's bottom is the point at 26,880 — the same point,
> at a penalty of **1.000×**.

That is the claim [#304](https://github.com/Zaba505/cpybkc/discussions/304) made
on a sixteen-column schema and this repository has been quoting ever since,
confirmed on the 197-column one at a scale where being wrong would show. It is
also the answer to the obvious objection to the harness next door: that `a` and
`W` are measured under an arrangement, so the rule they feed might only be right
about the arrangement. Fitting the retained term back off *this* curve — seven
readings of a running conversion, nothing held open, nothing probed at a chosen
row group — gives **a ≈ 971 B** per column per row group, against the 969 the
harness reads directly and the 1,024 `convert.go` commits. It is a reading and it
moves a byte or two between runs; what is not a coincidence is which number it
lands on.

The sweep is centred on R\* rather than halving down from N, and that is not
tuning. Halving from four and a half million starts at one row group for the whole
file, which is the several gigabytes this entire section exists to avoid holding.

### The buffered term is linear only up to the page buffer

The run at scale found one thing the harness at its default N cannot see, and it
is visible in the right-hand half of that table: **peak stops growing like `W·R`
somewhere around forty thousand rows a group.** 215,040 rows should hold 258 MB of
open row group and holds 110.6 MB.

The cause is [`parquet-go`](https://github.com/parquet-go/parquet-go)'s page
buffer. A column's buffer is flushed into a page as soon as it reaches 98% of
`DefaultPageBufferSize`, which is 256 KB (`config.go:29`, `writer.go:262`), so a
column never holds more than a quarter of a megabyte of *raw* buffer — past that
an open row group carries encoded pages instead, and encoded is smaller. The knee
is where one column's share of a row, `W/C`, has filled that buffer:

```
knee ≈ 256 KiB · C / W  =  41,116 rows
```

Two things follow, and the second is why nothing in `convert.go` moved for it.

**The model over-states what a large row group costs, and it over-states it in the
safe direction.** A bound derived from `W·R` holds *less* than the arithmetic
promised, never more, so [the default](#the-row-group-derived) — 106,861 rows,
which is above the knee — sits inside its budget with room the derivation does not
know about. Re-deriving it against a two-regime model would be replacing a
conservative number with a tighter one, and this section's whole argument is that
the conservative direction is the one to be wrong in.

**And it does not move the bottom.** The knee is on the far side of R\* at every N
either conversion is sized for, because R\* is where the two terms are *equal* and
the retained term is still worth something there. What it changes is the shape of
the right-hand shoulder — which is why `scale_test.go` holds the two ends of the
sweep to different ratios, and says so.

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

### What the ledger conversion's run found about this one's W

[#315](https://github.com/Zaba505/cpybkc/issues/315) ran the same sweep over the
ledger conversion, whose `W` was derived the same way this one's is —
`5·(optional columns) + (the record) + 15` — and the sweep put its bottom a factor
of two off the predicted R\*. Measuring that schema's `W` directly found 128 B a
row against a derivation of 95, and the missing term is `parquet-go`'s byte-array
column buffers: they keep an `offsets` and a `lengths` slice per value
(`column_buffer_byte_array.go:14-18`), and the arithmetic models neither.

**That term is missing from the derivation on this page too**, and this
conversion gets away with it for a reason worth being explicit about rather than
lucky in. `recordBytes` is taken at LRECL, which on a `RECFM=FB` dataset is every
record's length and therefore an over-estimate on every row that carries a shorter
one — alphanumeric items arrive from the generated reader already trimmed, and a
record's FILLER is not a column at all. The two errors point opposite ways and
roughly cancel: the harness reads **1,365 B a row** against the **1,256** derived,
so the derivation is 8% low here where the ledger's was 26% low, and the sweep at
4.5 M records still lands on R\* exactly.

Eight percent low is still low, and `rowsPerRowGroup` is `memoryBudget / 2 / W`,
so the open row group at the default holds about 146 MB rather than the 134 MB
half-budget the derivation names — inside a 256 MiB budget, but by less than it
says. Correcting it is a change to a committed default and to every number derived
from it, which is a story rather than a paragraph; what belongs here is that the
gap is known, measured, and in the direction that spends budget rather than the
one that overruns it.

### Where each of these runs

Two instruments, two costs, and one line drawn between them.

| | what it measures | how long | run by |
|---|---|---|---|
| [`memory_test.go`](memory_test.go) | `a` and `W` directly, by probing a writer at controlled phases, and the shape of the curve at N = 16,384 | about two seconds | **`dagger call ci`**, on every pull request |
| [`scale_test.go`](scale_test.go) | peak against `-rows-per-row-group` over the real conversion, at millions of records | **about three minutes** at N = 4.5 M, and it is seven whole conversions | a person, with `-scale.records` |

**Nothing in `scale_test.go` runs in CI.** Every test there skips unless
`-scale.records` is passed, so the pipeline compiles the file and runs none of it,
and the runtime of an ordinary pull request does not move. That is the decision
[#315](https://github.com/Zaba505/cpybkc/issues/315) was asked to make, and the
two things it rules out are worth saying:

- **Not a required check.** The production run this all came from took 23 minutes
  for one conversion; seven of them is not a gate anybody would keep. A check
  contributors learn to re-run until it passes is worse than no check.
- **Not a build tag.** A tagged file is one the compiler and the linter in CI do
  not see either, and a scale run that has stopped compiling is discovered by the
  person who least wants to discover it — the one taking a reading before a
  release. A flag that defaults to zero costs a skip and keeps the compile.

Take the reading before a release, and whenever the `parquet-go` pin, the schema
or [`rowsPerRowGroup`](convert.go) moves:

```console
$ go test -run TestPeak -v -scale.records=4500002 -timeout=60m
```

What CI keeps is everything that fails when the *model* stops being true — the
shape assertions next door, and the two checks that `C` is the schema's leaf count
and the row group is arithmetic over it. What it gives up is knowing that the
numbers in this section are still the numbers, which is a thing to check on a
schedule and not on a pull request.

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
