# The worked example, converted to Parquet

An adopter's commonest reason for reading a copybook is to land the data
somewhere a data platform can query, and Parquet is the ordinary destination.
This is [the ledger extract beside it](../ledger.sexpr) converted, compiled, and
tested.

**It is a reference and not a recommendation.** Every one of the decisions below
is one you have to make for yourself on your own layout; this makes one honest
reading of them, says why, and marks each place where another adopter would
reasonably choose differently. An adopter who reads it and does something else
has used it correctly.

It is also **not a library**. [#272](https://github.com/Zaba505/cpybkc/issues/272)
proposed a `cpybkc-gen-parquet` generator and was closed against it: the mapping
is not one answer per construct the way COBOL-to-Go is, it is a *schema design*,
and a schema design is not a function of the source. Eight independent decisions
came out of that thread and none of them is derivable from a descriptor. A
generator that picks for you is wrong for most adopters; one with an option per
axis is a configuration language. What survived is that the decisions are real
and unobvious, and that you meet all eight the first time you try this — so they
are written down here, next to code that compiles.

`convert.go` is `package main` for that reason. There is nothing here to import,
because there is nothing here that would be right to reuse.

```console
$ go run . -in ledger.dat -out /tmp/ledger/posting.parquet
```

`-out` names **the file**, not a directory, and the directory it sits in is
created if it is not there. It named a directory when there were two tables,
because the conversion was choosing both filenames; with one table there is no
name left for it to choose, and a flag that still named a directory would be
inventing `posting.parquet` inside it for you to go and find. It defaults to
`posting.parquet` in the working directory.

Two consequences of that change, both of which an adopter meets on the first run
after it:

- **A `-out` that is an existing directory is refused, by name.** Left alone it
  would surface as `is a directory` from `os.Create`, wrapped in a sentence about
  the *input* file — which sends you to read your extract over a mistake in a
  flag. `TestOutNamingADirectoryIsReportedAgainstTheRightFlag` asserts the
  message names `-out` and does not name `-in`.
- **`-out` is clobbered whether or not the run succeeds.** `os.Create` truncates,
  and a failed conversion then removes what it truncated, so a run over a bad
  extract destroys an earlier good output at the same path. That is the price of
  "a failed run leaves nothing a reader will open", and with the default being a
  bare `posting.parquet` in the working directory it is easy to meet. Write to a
  fresh path if the previous one matters.

`ledger.dat` is *a dataset of these
records*, and this repository does not commit one — a ledger extract is bytes in
cp037 behind DFSMS record descriptor words, which is not a thing to read in a
diff. `convert_test.go`'s `ledgerBytes` makes its fixtures through
`ledger.Writer`, and that is also the shortest way to make yourself one to run
this against.

## Its own Go module, and that is the load-bearing decision

`parquet-go` **must not** reach the root `go.mod`. That module is what
`go install github.com/Zaba505/cpybkc/cmd/cpybkc@version` builds; each of its
direct requires carries a paragraph beside it saying why the CLI needs it, and a
converter's dependency has no business in a signed, attested, distroless release
image. [`example/ledger`](../ledger) has no `go.mod` of its own, so anything
importing `parquet-go` from inside `example/` would land in the root module.

The precedent is [`irpb`](../../irpb), and it is precedent for exactly this
reason. `module_test.go` here is `irpb/module_test.go`'s argument applied to a
different constraint: nothing in the Go toolchain would fail if an import under
`example/` quietly added a require to the root `go.mod`, so a test reads that
file and says so. It finds it by following this module's own `replace` — the same
pointer the compiler follows — rather than by a relative path written down in the
test, so the assertion holds wherever the two modules sit relative to each other.

It is checked like every other Go module here, by a pipeline function of its
own: `dagger call example-parquet-ci`, beside `ir-ci`, `pipeline-ci` and
`companion-ci`. See [CONTRIBUTING.md](../../CONTRIBUTING.md#the-parquet-example-is-checked-like-any-other-go-module-here)
for what that stage has to do that the other four do not.

## Two grains, one table

A Parquet file carries exactly one schema, and this extract has **two grains** —
the header and trailer are one row per file, the postings are one row each. That
is real and it is the first thing to check on your own layout: count the grains,
not the record types. It does **not** follow that you write a file per grain.

The output is one table, `posting.parquet`, at the posting grain. The file-level
grain is spent rather than stored, and each of its fields goes somewhere:

| item | where it goes |
|---|---|
| `HDR-LEDGER-ID`, `HDR-PERIOD`, `HDR-CURRENCY` | denormalized onto every posting row |
| `HDR-COUNT` | nowhere — `ledger.Reader` already holds the body to it |
| `TRL-COUNT`, `TRL-NET` | reconciled against the rows written, then discarded |

The reason is that a file-level table needs a **key**, and this converter will
not mint one. A row of a file-level table with no key is a row that can only be
joined to its postings by having been read out of the same file — which is a join
a query engine cannot express, so in practice that table is either read on its own
or joined on the denormalized `hdr_ledger_id` and `hdr_period` that are *already
on every posting row*. The second table earns nothing the first does not already
carry, and it costs a second file, a second schema, and a second thing to keep in
step. See [No key is minted](#no-key-is-minted-and-here-is-why-one-is-not-needed)
— it is the same argument, arriving at a table instead of at a column.

So the rule this example ended up with, and the one to try first on your own
layout: **one table per data record type, with the parent's identifying fields
denormalized onto it, and the parent's summaries checked rather than kept.** A
file-level grain becomes a second table when it carries something that is neither
identifying nor a summary — a run date, a source system, an operator's comment,
anything a posting row would want and cannot be checked against the body. This
header carries no such field. Yours may.

## The decisions

### The header's *fields* travel, even though its grain gets no table

`hdr_ledger_id`, `hdr_period` and `hdr_currency` are copied onto **every**
posting row.

That is not the waste it looks like. In a columnar store a column that is
constant across a row group is one dictionary entry and an RLE run — a few bytes
— and it earns min/max statistics, so a query filtering on the period skips row
groups instead of joining. This is the whole of what makes the posting table usable
on its own — and, with no second table to fall back on, the whole of what keeps
the header readable at all.

### Trailer fields do not promote — they are checked and dropped

`TRL-COUNT` and `TRL-NET` are not columns anywhere.

They are *summaries of* the posting rows, so denormalizing them means
`SUM(trl_net)` silently returns the total times the row count — a wrong answer
that looks like a right one. `TestTrailerFieldsDoNotPromote` asserts the posting
schema carries no `trl_` column, because this is the decision that is cheapest to
undo by accident.

What a summary is *for* is checking, and the conversion makes both checks:

- `TRL-COUNT` against the rows actually written.
- `TRL-NET` against the posting amounts, accumulated as they are read.

Either mismatch is an error, and both run **before the footer is written**, so a
conversion that does not reconcile leaves a file no reader will open rather than
one a query would happily return wrong answers from. That ordering is the whole
trick: a Parquet file is its footer, so "fail before the footer" and "leave
nothing queryable" are the same sentence. On disk it goes one step further and
leaves no file at all — `write` removes the path it created, which
`TestAFailedRunLeavesNoFileBehind` asserts by reading the output directory back
rather than by inspecting a buffer.

`TRL-COUNT` is nearly free — the conversion is already counting rows. `TRL-NET`
costs an `int64` and one addition per posting, and it is worth it: a count agrees
on a file whose amounts are all wrong.

`HDR-COUNT` is a summary too, and this conversion does nothing with it at all.
`ledger.sexpr` reads it into a register — `(times … (item LEDGER-HEADER
HDR-COUNT))` — so a file whose body disagrees with its own header is reported by
`ledger.Reader` before a row ever reaches here. A converter checking it again
would be re-implementing the layout, and a column for it would be a number that
cannot disagree with `COUNT(*)`.

### No key is minted, and here is why one is not needed

The denormalized header fields, with `PST-ACCOUNT` and `PST-SEQUENCE`, are a
natural key for a posting row. Nothing here mints an ordinal column.

A minted ordinal is the fallback for a parent that carries **no distinguishing
field**, not the default. This layout does not need one; a layout shaped
`(* (seq BATCH-HEADER (* DATA) BATCH-TRAILER))` does, because `*` allocates no
register at all — there is no batch number to denormalize, so the only thing that
says which batch a detail belongs to is a number the converter invents. That
number is not in the file, so it is not stable across two runs over the same
dataset unless you make it so, and a downstream table joined on it is joined on
your converter's version rather than on the data.

The conversion does count an ordinal — `ledger.Reader` keeps one for its own
diagnostics and does not export it — but it is a *diagnostic* and never a column.
`TestAMappingErrorFailsTheConversion` asserts the failure says which record it
was.

### The sign the net assumes

**This conversion adds every posting amount with the sign the record stores it
under.** `PDB-AMOUNT` and `PCR-AMOUNT` are both `PIC S9(n)V99`; a debit arrives
positive and a credit negative, and `TRL-NET` is their plain sum.

That is an assumption, and the copybook does not make it. `posting.cpy` says the
amounts are signed and says nothing whatever about what a debit or a credit does
to a total. A layout that stores both as **magnitudes** and means the credit to
subtract is just as ordinary, and on such a file this conversion computes a net
that is wrong by twice the credits — and reports the mismatch rather than
accepting it, which is the outcome an adopter needs.
`TestTheNetTakesEachAmountWithTheSignTheRecordStoresIt` is that file, asserted.

If yours is the other kind, negate the credit arm of `postingRowOf` and say so
there. What you must not do is delete the reconciliation to make it pass.

### The scale is not a coincidence, and one day it will not hold

`TRL-NET` is `PIC S9(13)V99`, `PDB-AMOUNT` is `PIC S9(11)V99`, `PCR-AMOUNT` is
`PIC S9(9)V99`. All three carry **scale 2**, so the unscaled integers
`cpybkc-gen-go` produces are already in the same units and the accumulator is
plain `int64` addition. Nothing multiplies or divides by a hundred.

That is a property of this layout and not of decimals. A trailer keeping whole
currency units over postings keeping cents needs one side scaled here, and a
conversion that forgot would fail the reconciliation on every file — or pass it
on the one file whose net is zero. The precision has a bound of its own worth
noticing: `HDR-COUNT` is `PIC 9(3)`, so at most 999 amounts of at most thirteen
digits are summed, and `int64` holds nineteen. A layout with a wider count, or
wider amounts, is one where that has to be checked rather than argued.

### The postings: one table

The copybook's three `REDEFINES` over two independent runs admit **six** record
types out of one `01`-level. [`ledger.sexpr`](../ledger.sexpr) names **two** of
them, and that is the first thing to notice here: the schema is a function of the
*layout*, not of the copybook. The four it leaves out are the ones described by
`PST-BODY` or `PST-TAIL` — the base descriptions the two `REDEFINES` runs exist
to give storage to, which a file a mainframe produced does not carry.

`PST-TYPE` discriminates the two, and they are written as **one** table, with one
nullable column per description of each run the layout actually names:

```
pst_debit   pst_credit      the first run, PST-BODY, described two ways here
pst_tail_ref                the second run, PST-TAIL, described one way here
```

Two consequences, and both of them are decisions:

- Exactly one of `pst_debit` and `pst_credit` is present on any row, so the
  record type is recoverable from a row without consulting anything else —
  `TestTheMergedTableKeepsTheRecordTypeRecoverable`.
- `pst_tail_ref` is **required**, not optional. One description of a run in play
  is not a choice a row is making, and an optional column that is null on no row
  of any extract is one every query null-checks for nothing.
  `TestTheOnlyDescriptionOfATailIsARequiredColumn` asserts both that and the
  absence of a `pst_body` or `pst_tail` column.

Narrow the layout differently and this schema is different, which is the point.
Had the extract carried all six, the first run would be three nullable columns
and the second two.

The alternative is **narrow tables**, one per record type. It is a real option
and it is better for some consumers: a debit's amount and a credit's amount are
genuinely different columns, and a reader who only wants debits gets a table with
nothing nullable in it. Narrow tables cost you the ability to ask "every posting
on this account" without a union, and they multiply by the number of independent
redefined runs rather than adding — this layout would be two tables, one naming
all six combinations would be six, and a layout with three runs of three would be
twenty-seven.

### Nested, not flattened

`PST-DEBIT`, `PST-CREDIT` and `PST-TAIL-REF` are groups, and they are written as
groups: `pst_debit.pdb_amount`, not `pst_debit_pdb_amount`.

Flattening is not merely an ergonomic preference. It collapses a group `A-B`
containing `C` and a group `A` containing `B-C` onto the same path, so a
flattening converter needs a collision rule, and a collision rule is a decision
you will be making about somebody's field names at three in the morning. Nesting
has its own cost: some query engines and some ingestion tools are happier with
flat schemas, and if yours is one of them, flatten — with a rule, written down.

### The semantics stop at the copybook

`HDR-PERIOD PIC 9(6)` is plainly `YYYYMM`. It is written as an **integer**
anyway.

Nothing in the copybook says it is a date. Guessing here is how a zip code loses
its leading zero and how a "date" that is really a Julian day number becomes
1970. Where the semantics live is in the system that produced the file, and a
converter that reaches for them is a converter that is right until it is
catastrophically wrong. Add the semantics in the layer above this one, where
somebody can see the assumption being made.

Note also that alphanumeric items arrive from the generated reader already
trimmed of their `DISPLAY` padding, so `hdr_ledger_id` is `"GL-MAIN"` and not
`"GL-MAIN   "`. That is `codec`'s decision and not this conversion's; it is
mentioned because a downstream join against a system that kept the padding will
not match.

#### Where the line actually is, since the net crosses it

The sign convention above is a semantic the copybook does not carry, and this
conversion assumes one anyway. That looks like the opposite of this section, so
here is the line it is drawn on:

- **A semantic that changes what is stored is out.** `HDR-PERIOD` stays an
  integer. Nothing here writes a date, a currency-scaled amount, or a debit
  re-signed to somebody's convention. What lands in the file is what the reader
  produced, re-annotated.
- **A semantic that only *checks* what is stored can be in — stated, and
  falsifiable.** `TRL-NET` exists to be reconciled; a converter that reads it and
  says nothing has ignored the one thing the trailer is for. You cannot check it
  without assuming an arithmetic, so the assumption is written down here, written
  down at `postingRowOf`, and named in the error the check produces — and when it
  is wrong for your file, the run fails loudly on the first extract instead of
  being wrong quietly forever.

The distinction that matters is not "did the converter assume something" but
"what does the assumption do when it is wrong". A guessed date silently
mis-stores every row. A guessed sign fails a check on the first file.

## The type mapping, on the items this example carries

The two amount columns are the interesting case, and they need **no conversion at
all**:

| item | PICTURE | Go, as generated | Parquet |
|---|---|---|---|
| `PDB-AMOUNT` | `PIC S9(11)V99 COMP-3` | `int64`, unscaled | `DECIMAL(13,2)` |
| `PCR-AMOUNT` | `PIC S9(9)V99 COMP-3` | `int64`, unscaled | `DECIMAL(11,2)` |
| `TRL-NET` | `PIC S9(13)V99 COMP-3` | `int64`, unscaled | *no column — reconciled* |

`cpybkc-gen-go` writes a scaled item as [the unscaled integer with the scale in
the doc comment](../../cmd/cpybkc-gen-go/README.md), which is precisely what
`DECIMAL(p,s)` is. Nothing in `convert.go` divides by a hundred, and
`TestTheTwoAmountColumnsRoundTripThroughDecimal` reads the written file back,
checks the unscaled values against what the reader produced, and checks the
annotation in the file's own schema — which is what a query engine reads, rather
than the Go struct tag.

`TRL-NET` is in that table for an item that is never written, because the mapping
is what makes the reconciliation legal: all three are scale 2, so the accumulator
adds the unscaled integers as they stand. `DECIMAL(15,2)` is what it *would* be
annotated as, and knowing that is what tells you the addition needs no rescaling.

This is worth showing precisely because it looks like it should need a
conversion. Applying the scale here would apply it twice by the time a value
reached a caller.

## Four things this example does *not* carry, and your next file will

Each of these is a construct the mapping above does not cover, and each fails
quietly rather than loudly if you extend the table by eye.

- **`PIC 9(4) COMP-5` holds 65535.** A binary item is bounded by its *storage*
  and not by its PICTURE, so `DECIMAL(4,0)` overflows on a value the file is
  entitled to hold. `cmd/cpybkc-gen-go`'s README records this as [the defect the
  unsigned rows were added to fix](../../cmd/cpybkc-gen-go/README.md) — the old
  types read that value back as `-1`. Size the decimal from the storage, or use
  an integer column.
- **A picture ending in a run of `P` has a *negative* scale.** `PIC 9(3)PPP` is
  three stored digits with the point three places to their right, which
  `cobol-go`'s `picture` package resolves as `Digits: 3, Scale: -3`. Parquet
  requires `0 <= scale <= precision`, so that item dereferences to a schema no
  writer will accept. Multiply it out into an integer, or carry the scale beside
  the column.
- **Numeric-edited items carry digits and scale too.** The IR's field node
  carries [category, digits, scale and sign](../../docs/ir/SPEC.md) for every
  elementary item, so reading those attributes "straight out of the field node"
  gets you a plausible-looking `DECIMAL`. What the generated struct actually
  holds is [the edited text as it stands in the record](../../cmd/cpybkc-gen-go/README.md)
  — an edited item is a `string` — so that column receives `" 1,234.50"`. The IR
  says outright that such an item [carries a width and no value a generator can
  use](../../docs/ir/SPEC.md).
- **`COMP-1` and `COMP-2` are not ordinary numbers.** This project has already
  reversed a decision that treated them as one:
  [`testdata/conformance/README.md`](../../testdata/conformance/README.md)
  records the corpus moving its float entries to hexadecimal significand notation
  "exactly so that a NaN, an infinity and a negative zero can be written at all".
  Parquet has `FLOAT` and `DOUBLE` and they are the right targets — but a
  converter that routes them through the decimal path because they are "numeric"
  loses all three of those values.

## The loop, which is the part that is actually fiddly

#272's nine-line sample was wrong in ten ways on this very layout. Prose samples
rot and nothing catches them, so this one is compiled and each of the properties
it violated is a test:

| property | what goes wrong without it | test |
|---|---|---|
| the row group **bounded**, by `parquet.MaxRowsPerRowGroup` | `parquet-go` defaults the bound to `math.MaxInt64`, so one row group grows for the whole file and "streaming" is a comment | `TestEveryPostingReadIsAPostingWritten` |
| the writer **closed** | the last, partial row group is never written, on every file whose posting count is not a multiple of the bound — and there is no footer, so nothing opens the file at all | same |
| the **ordinal counted by the caller** | the diagnostic cannot say which record failed; `Reader`'s own is unexported | `TestAMappingErrorFailsTheConversion` |
| a **mapping error that fails the conversion** | a discarded error appends a zero row: empty account, zero amount, indistinguishable from data | same |
| `TRL-COUNT` **reconciled** | the file, the layout or the conversion is wrong and nothing says so | `TestTheTrailerCountIsReconciled` |
| `TRL-NET` **reconciled** | a count that agrees over amounts that do not; the accumulator is the only part of this that costs anything | `TestTheTrailerNetIsReconciled` |
| the **failed output removed** | a footerless file sits where a good one used to, and the next reader finds out | `TestAFailedRunLeavesNoFileBehind` |

Only the first of those is the library's to hold, and it is not the library's to
decide. The bound is enforced inside `GenericWriter.Write` — the row-group writer
returns `ErrTooManyRowGroups` at the cap, `Write` catches it and rotates the
group — but the *number* has to be passed, because the default is not a bound.
Closing the writer did not move anywhere: `Close` is a call this conversion
makes, and one it already owed for the footer, so an adopter who drops it loses
the whole file rather than the last thirty-nine rows.

The row-group test runs over **999 postings** — the most `HDR-COUNT`'s
`PIC 9(3)` can describe — because 999 is not a multiple of the bound, and a file
of a whole number of row groups is the one file an unwritten last group does not
lose rows from.

## Memory

Peak memory is **the open row group**, and not "constant". The file is never
held: what the conversion carries at any moment is the header, the one row it has
just mapped, and whatever `parquet-go` is buffering for the row group behind it.
There is no second buffer — a mapped row goes to the writer as it is produced —
and `TestEveryPostingReadIsAPostingWritten` asserts the term that is left by
opening the written file and requiring one row group per `rowsPerRowGroup` rows,
none larger.

Of the two properties the batch was holding, one is the library's now and the
other collapsed into a call that was already there. Bounding the row group is
`parquet.MaxRowsPerRowGroup`, enforced inside `GenericWriter.Write`; writing the
last, partial one is `Close`, which this conversion owed anyway for the footer.
Holding either of them a second time in a batch of the caller's own is what has
gone. What did **not** move is the decision: the default is
`DefaultMaxRowsPerRowGroup`, which is `math.MaxInt64`, so a writer handed no
option grows one row group for the whole file. That is what makes a conversion
buffer the file it claims to stream, and it is a line of code away in either
direction.

The batch was buying a second thing, and this gives that up deliberately.
`GenericWriter.Write` takes a *slice* so that its per-call work — resolving the
column buffers, descending both optional groups, checking each column against the
page size — is amortized across the rows in it, and one row per call amortizes it
across one. At 999 rows that is invisible, and handing a row over as it is
produced is what makes the loop readable. A converter writing millions would pass
`Write` a slice again while still leaving the bound to
`parquet.MaxRowsPerRowGroup` — which is the point of the whole change: the slice
is an amortization, and it was never the bound.

`rowsPerRowGroup` is 64 here, chosen so that 999 postings fill fifteen row groups
and leave a sixteenth partial one — so a bound nothing enforced, or a last group
nothing wrote, is visible in a test. **That is not a production
number.** A real converter sizes a row group in the tens or hundreds of thousands
of rows, because the row group is the unit a query engine skips and a file of
tiny ones pays footer metadata and a dictionary reset on every one. Raising it
raises the memory bound in exactly the same proportion, which is the trade this
paragraph exists to make visible.

## Not generated

Nothing here is written by cpybkc. This directory is not named in
[`example/cpybkc.json`](../cpybkc.json), it is not recorded in
[`example/cpybkc.gen.json`](../cpybkc.gen.json), and
[`example/regenerate_test.go`](../regenerate_test.go) does not touch it. The two
halves of `example/` are told apart by the `// Code generated … DO NOT EDIT.`
header, and `TestNothingHereIsMarkedGenerated` fails if a file here ever grows
one.
