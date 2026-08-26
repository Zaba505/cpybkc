# The worked example, converted to Parquet

An adopter's commonest reason for reading a copybook is to land the data
somewhere a data platform can query, and Parquet is the ordinary destination.
This is [the ledger extract beside it](../ledger.sexpr) converted, compiled, and
tested.

**It is a reference and not a recommendation.** Every one of the decisions below
is one you have to make for yourself on your own layout; this makes one honest
reading of them, says why, and marks the four where another adopter would
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
$ go run . -in ledger.dat -out /tmp/ledger
```

`-out` is created if it is not there. `ledger.dat` is *a dataset of these
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

## Two grains, so two files

A Parquet file carries exactly one schema. This extract has **two grains** — the
header and trailer are one row per file, the postings are one row each — so the
output is two files and the conversion is across both:

| table | grain | what it carries |
|---|---|---|
| `extract.parquet` | one row per file | `LEDGER-HEADER` × `LEDGER-TRAILER` |
| `posting.parquet` | one row per posting | the posting, with header context denormalized onto it |

An adopter who has not met this assumes one file and finds out late. It is the
first thing to check on your own layout: count the grains, not the record types.

## The decisions

### Grains stay separate; header *fields* do not

`hdr_ledger_id`, `hdr_period` and `hdr_currency` are copied onto **every**
posting row.

That is not the waste it looks like. In a columnar store a column that is
constant across a row group is one dictionary entry and an RLE run — a few bytes
— and it earns min/max statistics, so a query filtering on the period skips row
groups instead of joining. This is the whole of what makes `posting` usable on
its own.

### Trailer fields do not promote

`TRL-COUNT` and `TRL-NET` stay in `extract`.

They are *summaries of* the posting rows, so denormalizing them means
`SUM(trl_net)` silently returns the total times the row count — a wrong answer
that looks like a right one. `TestTrailerFieldsDoNotPromote` asserts the posting
schema carries no `trl_` column, because this is the decision that is cheapest to
undo by accident.

`extract` is also where the reconciliation belongs, and the conversion makes it:
`TRL-COUNT` against the rows actually written, and a mismatch is an error. It
runs **before either footer is written**, so a conversion that does not reconcile
leaves two files no reader will open rather than two a query would happily return
wrong answers from.

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

## The type mapping, on the items this example carries

The three amounts are the interesting case, and they need **no conversion at
all**:

| item | PICTURE | Go, as generated | Parquet |
|---|---|---|---|
| `TRL-NET` | `PIC S9(13)V99 COMP-3` | `int64`, unscaled | `DECIMAL(15,2)` |
| `PDB-AMOUNT` | `PIC S9(11)V99 COMP-3` | `int64`, unscaled | `DECIMAL(13,2)` |
| `PCR-AMOUNT` | `PIC S9(9)V99 COMP-3` | `int64`, unscaled | `DECIMAL(11,2)` |

`cpybkc-gen-go` writes a scaled item as [the unscaled integer with the scale in
the doc comment](../../cmd/cpybkc-gen-go/README.md), which is precisely what
`DECIMAL(p,s)` is. Nothing in `convert.go` divides by a hundred, and
`TestTheThreeAmountsRoundTripThroughDecimal` reads the written files back, checks
the unscaled values against what the reader produced, and checks the annotation
in the file's own schema — which is what a query engine reads, rather than the Go
struct tag.

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
| a **bounded batch** with an explicit flush | the row group grows for the whole file, and "streaming" is a comment | `TestEveryPostingReadIsAPostingWritten` |
| a **terminal flush** | the last partial batch is dropped, on every well-formed file | same |
| the **ordinal counted by the caller** | the diagnostic cannot say which record failed; `Reader`'s own is unexported | `TestAMappingErrorFailsTheConversion` |
| a **mapping error that fails the conversion** | a discarded error appends a zero row: empty account, zero amount, indistinguishable from data | same |
| `TRL-COUNT` **reconciled** | the file, the layout or the conversion is wrong and nothing says so | `TestTheTrailerCountIsReconciled` |

The terminal-flush test runs over **999 postings** — the most `HDR-COUNT`'s
`PIC 9(3)` can describe — because 999 is not a multiple of the batch size, and a
file of a whole number of batches is the one file a missing terminal flush does
not lose rows from.

## Memory

Peak memory is **the batch plus the open row group**, and not "constant". The
file is never held: what the conversion carries at any moment is the header, the
batch of rows in hand, and whatever `parquet-go` is buffering for the row group
behind it. Closing the row group with every batch is what keeps the second term
bounded by the first, and `TestEveryPostingReadIsAPostingWritten` asserts it by
opening the written file and requiring one row group per batch, none larger than
the batch.

`batchSize` is 64 here, chosen so that 999 postings fill fifteen batches and
leave a sixteenth partial one — so a missing flush of either kind is visible in a
test. **That is not a production
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
