# The claim example

A healthcare claims extract carried from a layout to bytes: the files a caller
writes, the Go package and the diagram cpybkc writes for them, and tests over
both.

This is one of the examples under [`example/`](..), and it is a cpybkc project of
its own — [`cpybkc.json`](cpybkc.json) here names one layout and the generators
run over it, exactly as an adopter's own project does. [Why the manifest is per
example](../README.md#one-manifest-per-example) is the index's to say.

Everything here is checked in, and the two halves are told apart by which of
them a person edited.

| What a caller writes | What cpybkc writes |
|---|---|
| [`cpybkc.json`](cpybkc.json) — the project manifest | [`claim/`](claim) — the generated Go package |
| [`claim.sexpr`](claim.sexpr) — the layout | [`graph/graph.md`](graph/graph.md) — the generated diagram of the automaton |
| [`claim.cpy`](claim.cpy) — the copybook the layout names | [`cpybkc.gen.json`](cpybkc.gen.json) — the record of what was generated |

`claim/roundtrip_test.go` is neither. It is hand-written, and it lives *inside*
the generated package rather than beside it because one of its assertions reaches
a field nothing outside can — see [Slack, per
occurrence](#slack-per-occurrence). The regeneration below leaves it alone.

There is no `parquet/` here. What one of these claims becomes in a column store
is a real question and [`ledger/parquet/`](../ledger/parquet/README.md) and
[`policy/parquet/`](../policy/parquet/README.md) are where it is argued; this
example is about what happens *inside* one record, and answering both in one
directory would be answering neither.

## Why this exists

One construct in the layout format has a form of its own and was shown in no
worked example: a `REDEFINES` **inside a repeating group**.

Everywhere else a `REDEFINES` is resolved away. The copybook describes one run
of bytes several ways, the layout says which description a record is, and each
becomes its own record type chosen by its own `discriminate` — which is what
[`ledger/`](../ledger) does six times over one `01`-level. That resolution
cannot reach a redefine inside a table, because the choice is made **once per
occurrence** rather than once per record, and nine entries choosing between
three descriptions are not three record types. So the alternation survives, as
a variant, and [`discriminate-variant`](../../docs/layout/SPEC.md#a-discriminator-for-a-redefine-inside-a-table)
is where what chooses it is written.

Discussion [#340](https://github.com/Zaba505/cpybkc/discussions/340) is what the
gap cost. The reporter arrived with a table of redefined entries and wrote:

> From the docs and current behaviour, `discriminate-variant` appears to require
> each arm to be selected by an `equals`/`one-of` predicate on bytes within that
> occurrence.

They were right, and they had to derive it from the spec and from a diagnostic,
because there was no file to look at. [`example/README.md`](../README.md) says
*read the one whose file looks like yours*, and for this file shape there was
none.

## The file

A daily claims extract on a variable-length dataset (`recfm VB`), EBCDIC
throughout. Every record is a claim. A claim opens with the claim number and the
member it is for, then says how many line items arrived, and then carries that
many of them; behind the table sit the claim's total and its status.

A line item is a kind code, a charge, and twenty bytes of body described three
ways — as a professional service, as a pharmacy fill, or as a facility stay. The
kind code opens the line and is what says which.

```
CL CLM000000001 M000123456 5 P…  D…  F…  D…  I…  <total> A
                            ^ ^^^^^^^^^^^^^^^^^  ^^^^^^^^^
                            | five lines, five choices   the items behind
                            the count                    the table
```

## What is hard about it

### The choice is per occurrence, and there is no record type for it

[`claim.cpy`](claim.cpy) describes `CLN-PROFESSIONAL` three ways:

```cobol
           05  CLM-LINE                OCCURS 1 TO 9 TIMES
                                       DEPENDING ON CLM-LINE-COUNT.
               10  CLN-KIND            PIC X(1).
               10  CLN-CHARGE          PIC S9(7)V99 COMP-3.
               10  CLN-PROFESSIONAL.
                   …
               10  CLN-PHARMACY        REDEFINES CLN-PROFESSIONAL.
                   …
               10  CLN-FACILITY        REDEFINES CLN-PROFESSIONAL.
                   …
```

and [`claim.sexpr`](claim.sexpr) says what selects each:

```
(discriminate-variant (item CLAIM-RECORD CLM-LINE CLN-PROFESSIONAL)
  (arm CLN-PROFESSIONAL (equals (item CLAIM-RECORD CLM-LINE CLN-KIND) "P"))
  (arm CLN-PHARMACY     (equals (item CLAIM-RECORD CLM-LINE CLN-KIND) "D"))
  (arm CLN-FACILITY     (one-of (item CLAIM-RECORD CLM-LINE CLN-KIND) "F" "I")))
```

Three things in that form are worth reading slowly.

**The argument is the redefined item, not the group that repeats.** It names the
run being chosen for — the first of the three descriptions, the one every
`REDEFINES` of it names — and the fact that the group around it repeats is the
copybook's, not something the layout restates.

**Every arm's target sits inside the occurrence.** `CLN-KIND` opens the line, so
each line's predicate reads that line's own byte. This is the mirror of the rule
a record's discriminator obeys, and it is not a detail: a target outside the
table would read the same bytes for all nine lines and select the same arm nine
times, which is a choice made once per record and so a *record's* to make. The
[layout spec](../../docs/layout/SPEC.md#a-discriminator-for-a-redefine-inside-a-table)
refuses one, and `resolve` names the record, the table, the variant and the
target when it does.

**`CLN-PROFESSIONAL` is both the variant and an arm**, which is ordinary rather
than a trick. The base description is the first alternative over those bytes and
not a declaration standing in front of the real ones; a professional service is
what most of a claim's lines are, so it is what the copybook describes first.

The generated Go says the same thing in the one shape a caller touches:

```go
ClmLine []struct {
    ClnKind          string
    ClnCharge        int32
    ClnProfessional  *struct{ … }
    ClnPharmacy      *struct{ … }
    ClnFacility      *struct{ … }
}
```

One slice, one set of three pointers **per element**, exactly one of them
non-nil in each. That is what "chosen once per occurrence" is, and it is what no
number of record types could have expressed.

### Three arms because the file carries three alternatives

A layout names what a real extract carries. Were this file professional lines
only, the copybook's other two descriptions would be storage nothing fills, and
the form to write would be

```
(take-alternative (item CLAIM-RECORD CLM-LINE CLN-PROFESSIONAL) CLN-PROFESSIONAL)
```

— [not a two-armed variant](../../docs/layout/SPEC.md#every-occurrence-of-a-table-takes-one-alternative)
with a predicate invented to tell the lines the file has from a kind of line it
does not. That is a test on bytes that decide nothing, and the two-arm floor on
`discriminate-variant` exists to stop it being written. This extract genuinely
carries all three, so all three have arms.

`CLN-FACILITY` has one arm and two codes: `F` for an outpatient stay and `I` for
an inpatient one, which select one description between them. That is what
`one-of` is for, and two arms naming one alternative is refused.

### Slack, per occurrence

`CLN-PHARMACY` describes fourteen of the twenty bytes the run holds. The six
behind them are bytes no description of a pharmacy line covers — ordinary COBOL,
and the storage is there because `CLN-PROFESSIONAL` needs it.

cpybkc keeps them. [`docs/ir/SPEC.md`](../../docs/ir/SPEC.md)'s *Slack survives a
read* is the rule, and the generated struct carries an unexported `slack` field
inside `ClnPharmacy` — **one set per occurrence of the line**, because a slack
node inside a repeating group stands for a run per occurrence and not one run in
the record. A claim of nine pharmacy lines holds nine such runs, and each is
written back where it came from.

That is the assertion `claim/roundtrip_test.go` is inside the package for. A
reader that kept one run for the whole record, or a writer that carried the
first line's six bytes into the second, would still produce records whose every
exported field compared equal; the only way to see it is to write a claim whose
pharmacy lines carry *different* undescribed bytes and hold the file against
itself byte for byte.

### The `OCCURS DEPENDING ON` reading, and the framing that follows from it

`CLM-LINE` is an `OCCURS 1 TO 9 TIMES DEPENDING ON CLM-LINE-COUNT`, so the
layout **must** say which reading the compiler that wrote the file used, and
there is no default:

```
(copybook-reading
  (occurs-depending-on odoslide))
```

This example states `odoslide` — the reading IBM Enterprise COBOL applies
unconditionally, and the one under which `CLM-TOTAL-CHARGE` and `CLM-STATUS`
begin wherever the last line ended. `noodoslide` would describe a *different
file* out of the same copybook: nine line items always present, `CLM-LINE-COUNT`
an ordinary field beside them saying how many are meant, and every claim the same
266 bytes. Nothing in the bytes disagrees with the wrong one, which is why the
form is required rather than inferred.

The framing follows from it rather than being a separate taste. Under `odoslide`
a claim's extent moves with its count, and a variable-extent record type is
refused on a fixed-length dataset — so `(recfm VB)` and
`(occurs-depending-on odoslide)` stand or fall together. An adopter who changes
one and not the other is told so by `resolve` rather than by a misread file.

This is also the first example in this repository to carry an `OCCURS DEPENDING
ON` at all. [`ledger/`](../ledger)'s counted repetition is a `times` over
*records*, which is the automaton's register and a different mechanism entirely.

## The diagram

[`graph/graph.md`](graph/graph.md) is two halves and the second is the one to
read here. The automaton is three edges — a claim, then any number of claims —
because this file has one record type and nothing to tell apart between records.
The item table is where the work is:

```
| 25 | 26 × CLM-LINE-COUNT | CLM-LINE | — | — | occurs CLM-LINE-COUNT times (1 to 9) |
| 31 | 20 | CLM-LINE.*variant* | — | — | always |
| 31 | 20 | CLM-LINE.CLN-PROFESSIONAL | — | — | when CLM-LINE.CLN-KIND = 0xD7 |
| 31 | 20 | CLM-LINE.CLN-PHARMACY | — | — | when CLM-LINE.CLN-KIND = 0xC4 |
| 45 | 6 | CLM-LINE.CLN-PHARMACY.*slack* | — | — | always |
| 31 | 20 | CLM-LINE.CLN-FACILITY | — | — | when CLM-LINE.CLN-KIND is one of 0xC6 or 0xC9 |
| 25 + 26 × CLM-LINE-COUNT | 6 | CLM-TOTAL-CHARGE | … |
```

Four things an adopter should check against the file on their desk are all in
those rows. The three arms begin at the **same** offset, 31. The literals are
resolved to EBCDIC — `"P"` is `0xD7`, not `0x50` — which is the charset axis
doing its work and the commonest thing to get wrong by hand. The six bytes of
pharmacy slack are named as a row of their own. And the offset of
`CLM-TOTAL-CHARGE` carries a variable term rather than a number, which is
`odoslide` visible at a glance.

## The decisions this example makes

**A directory of its own rather than an extension of `ledger/`.** `ledger/` is
where the hard shapes live and this is a hard shape, so it was a real choice.
Against it: [`example/README.md`](../README.md) says adding an example is adding
a directory, examples are independent so that one being reworked does not put
every other's generated tree in the same diff, and `ledger/`'s copybook feeds a
worked Parquet conversion that a table of alternatives inside a record would
change the shape of — putting a column-store argument in the same diff as a
variant. The file shapes differ too: `ledger/` is a file whose *record types*
have to be told apart, and this is a file with one record type whose *entries*
do.

**An `OCCURS DEPENDING ON` rather than a fixed `OCCURS n TIMES`.** A variant
needs only a table, and a fixed one would have been the smaller example. It is an
ODO because the file discussion #340 arrived with is an ODO — `example/README.md`
promises an adopter the example whose file looks like theirs, and an example that
narrowed to a fixed table would still not have been that file. The cost is
admitted: this example carries the first ODO in `example/` as well as the first
variant, and the two are separable in the reading — the variant is the subject,
and the table is the scaffolding a variant cannot exist without.

**One record type.** No header, no trailer, no second copybook. What this example
is for happens inside a record, and `ledger/` and `policy/` are already the
examples of files whose record types have to be told apart. `(discriminate
CLAIM-RECORD single-record-type)` is written out even so, because every `record`
form is named by exactly one `discriminate` — which is what makes *this file has
nothing to test* a statement an adopter made rather than a gap.

## Regenerating

From the repository root:

```sh
go build -o "$(go env GOPATH)/bin/cpybkc-gen-go" ./cmd/cpybkc-gen-go
go build -o "$(go env GOPATH)/bin/cpybkc-gen-graph" ./cmd/cpybkc-gen-graph
go run ./cmd/cpybkc --manifest example/claim/cpybkc.json
```

Then commit what changed — [`example/regenerate_test.go`](../regenerate_test.go)
is what fails if you do not.
