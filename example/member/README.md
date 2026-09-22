# The member example

A membership extract carried from a layout to bytes: the files a caller writes,
the Go package and the diagram cpybkc writes for them, and tests over both.

This is one of the examples under [`example/`](..), and it is a cpybkc project of
its own — [`cpybkc.json`](cpybkc.json) here names one layout and the generators
run over it, exactly as an adopter's own project does. [Why the manifest is per
example](../README.md#one-manifest-per-example) is the index's to say.

Everything here is checked in, and the two halves are told apart by which of
them a person edited.

| What a caller writes | What cpybkc writes |
|---|---|
| [`cpybkc.json`](cpybkc.json) — the project manifest | [`member/`](member) — the generated Go package |
| [`member.sexpr`](member.sexpr) — the layout | [`graph/graph.md`](graph/graph.md) — the generated diagram of the automaton |
| [`member.cpy`](member.cpy) — the copybook the layout names | [`cpybkc.gen.json`](cpybkc.gen.json) — the record of what was generated |

`member/roundtrip_test.go` is neither. It is hand-written, and it lives *inside*
the generated package rather than beside it because one of its assertions
reaches a field nothing outside can — see [Slack, and whose it
is](#slack-and-whose-it-is). The regeneration below leaves it alone.

There is no `parquet/` here, for the reason [`claim/`](../claim) has none: what a
record like this becomes in a column store is argued in
[`ledger/parquet/`](../ledger/parquet/README.md) and
[`policy/parquet/`](../policy/parquet/README.md), and this example is about what
happens *inside* one record.

## Why this exists

Read this one **beside [`claim/`](../claim)**. The two are a pair, and between
them they are the two ways a `REDEFINES` inside a repeating group is settled.

A redefine is normally resolved away: the copybook describes one run of bytes
several ways, the layout says which description a record is, and each becomes
its own record type — which is what [`ledger/`](../ledger) does six times over
one `01`-level. That resolution cannot reach a redefine inside a table, because
the choice is made **once per occurrence** rather than once per record. So the
alternation survives as a variant, and something has to say which arm an
occurrence takes.

In `claim/` the entries carry **types**, and a kind code inside each line says
which: a [`discriminate-variant`](../../docs/layout/SPEC.md#a-discriminator-for-a-redefine-inside-a-table)
gives every arm a predicate over that code, and the file decides.

Here the entries carry **roles**, and no byte of an entry says anything at all.
A member's addresses are written in the order the enrolment screen asks for
them: home first, then work, then mailing. Entry one is the home address because
it is the first. There is nothing for a predicate to test, so the layout says
where each alternative sits instead — a
[`schedule-variant`](../../docs/layout/SPEC.md#a-schedule-for-a-redefine-chosen-by-position).

Discussion [#340](https://github.com/Zaba505/cpybkc/discussions/340) is what the
gap cost, and it is this file shape the reporter arrived with:

> the structure is positional: a count indicates how many entries are present,
> and occurrence 1/2/3... maps to specific variant arms in order.

They had to derive that from the spec and from a diagnostic, because there was
no file to look at. [`example/README.md`](../README.md) says *read the one whose
file looks like yours*, and for this file shape there was none.

## The file

A membership extract on a variable-length dataset (`recfm VB`), EBCDIC
throughout. Every record is a member. A member opens with their id and name,
then says how many addresses they gave, and then carries that many of them;
behind the table sits the member's status.

An address entry is sixty-one bytes described three ways — as a home address, as
a work address, or as a mailing address. Nothing in those sixty-one bytes says
which.

```
M000000001 MEMBER M000000001        3 <home>       <work>       <mail>       A
                                    ^ ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^  ^
                                    | three entries, three roles, in order   |
                                    the count                    the item behind
                                                                 the table
```

## What is hard about it

### Nothing in the record says which alternative an entry is

[`member.cpy`](member.cpy) describes `ADR-HOME` three ways:

```cobol
           05  MBR-ADDRESS             OCCURS 1 TO 3 TIMES
                                       DEPENDING ON MBR-ADDRESS-COUNT.
               10  ADR-HOME.
                   …
               10  ADR-WORK REDEFINES ADR-HOME.
                   …
               10  ADR-MAIL REDEFINES ADR-HOME.
                   …
```

and there is no `ADR-KIND` in front of them, because the file that this describes
has none. [`member.sexpr`](member.sexpr) says which entry is which:

```
(schedule-variant (item MEMBER-RECORD MBR-ADDRESS ADR-HOME)
  (arm ADR-HOME 1)
  (arm ADR-WORK 2)
  (arm ADR-MAIL 3))
```

Four things in that form are worth reading slowly.

**The argument is the redefined item, not the group that repeats**, exactly as
`discriminate-variant`'s is. It names the run being chosen for — the first of the
three descriptions, the one every `REDEFINES` of it names — and the fact that the
group around it repeats is the copybook's.

**An arm names occurrences, and there is no predicate anywhere.** A variant is
scheduled or discriminated and never both, which is why this is a form of its own
rather than a second shape of `arm` inside the other: a writer taking an arm from
its caller for one entry and from the descriptor for the next is what two mixable
shapes would have meant.

**The roles are named, so the three lines read as the schedule the enrolment
screen enforces rather than as arithmetic over a table.** `(arm ADR-WORK 2)` says
*the second address a member gives is their work address*. That sentence is the
rule, it is true of the extract, and it is nowhere in the file.

**An arm may name several occurrences** — `(arm ADR-HOME 1 2 3)` is legal, and it
is what a table of twenty entries whose first five take one alternative needs.
This extract has one role per entry, so each arm names one.

The generated Go says the same thing in the one shape a caller touches:

```go
MbrAddress []struct {
    AdrHome *struct{ … }
    AdrWork *struct{ … }
    AdrMail *struct{ … }
}
```

One slice, one set of three pointers **per element**, exactly one of them non-nil
in each — the same shape `claim/` generates, because a variant is a variant
whichever way its arm is selected. What differs is the doc comment the generator
writes on each: *this arm is occurrence 2 of the table*, rather than a predicate.

### What the schedule being static takes away, and what it gives back

The schedule is checked before a byte is read. `resolve` requires it to cover
every occurrence the table can hold, exactly once, from one to the repetition's
declared maximum, and rejects one that does not — naming the record, the
repeating group, the variant and the occurrence numbers with no arm.

So **an entry matching no arm is not a failure a reader of this file can meet.**
That is the one thing a schedule takes away from the byte-selected form:
`claim/`'s reader refuses a line whose kind code selects nothing, and there is no
counterpart here because the layout could not have been loaded if an entry had no
arm.

What it gives back is the whole file shape. A `discriminate-variant` could not
describe this extract at all: the obvious target for a predicate is
`MBR-ADDRESS-COUNT`, and that sits *outside* the table, so it reads the same
bytes for every entry and would select the same arm three times — a choice made
once per record, and a record's to make. That refusal is exactly the second wall
discussion #340 hit.

The writer keeps the same rule from the other side. The arm is the descriptor's,
so a caller who fills in `AdrHome` in the third entry is **reported** rather than
having one of the two picked for them, and the report names the occurrence, the
arm the schedule assigns and the arm the record holds.

### Slack, and whose it is

`ADR-MAIL` describes thirty-four of the sixty-one bytes the run holds. The
twenty-seven behind them are bytes no description of a mailing address covers —
ordinary COBOL, and the storage is there because `ADR-HOME` needs it.

cpybkc keeps them. [`docs/ir/SPEC.md`](../../docs/ir/SPEC.md)'s *Slack survives a
read* is the rule, and the generated struct carries an unexported `slack` field
inside `AdrMail`, one set per occurrence of the entry.

**Which occurrences have such a run is the difference between the two forms,
stated in bytes.** In `claim/` it is whichever lines carried a `D`, which is the
data's to say and varies record by record. Here it is entry three of every member
who gave three addresses and no entry of anybody else — a fact about the
descriptor, readable off the layout without opening the file.

That is the assertion `member/roundtrip_test.go` is inside the package for. A
reader that kept one run for the whole record, or a writer that carried one
member's twenty-seven bytes into another's, would still produce records whose
every exported field compared equal; the only way to see it is to write members
whose mailing addresses carry *different* undescribed bytes and hold the file
against itself byte for byte.

### The `OCCURS DEPENDING ON` reading, and what it does not decide

`MBR-ADDRESS` is an `OCCURS 1 TO 3 TIMES DEPENDING ON MBR-ADDRESS-COUNT`, so the
layout **must** say which reading the compiler that wrote the file used, and
there is no default:

```
(copybook-reading
  (occurs-depending-on odoslide))
```

This example states `odoslide` — the reading IBM Enterprise COBOL applies
unconditionally, and the one under which `MBR-STATUS` begins wherever the last
entry ended, at byte 224 in a member of three addresses and byte 102 in a member
of one. `noodoslide` would describe a *different file* out of the same copybook:
three entries always present, `MBR-ADDRESS-COUNT` an ordinary field beside them
saying how many are meant, and every member the same 225 bytes. Nothing in the
bytes disagrees with the wrong one, which is why the form is required rather than
inferred.

The framing follows from the reading rather than being a separate taste. Under
`odoslide` a member's extent moves with their count, and a variable-extent record
type is refused on a fixed-length dataset — so `(recfm VB)` and
`(occurs-depending-on odoslide)` stand or fall together.

**The schedule does not follow from it, and that is deliberate.** The three arms
above would be the same text under `noodoslide`, on a `recfm FB` dataset, and
under a fixed `OCCURS 3 TIMES` with no reading to state at all. Which reading
applies is a property of the *extract* — of the compiler that wrote it — while
the copybook is the same copybook, so a mechanism available under one reading and
not the other would make one copybook describable or not according to which
compiler wrote the file. [`docs/layout/SPEC.md`](../../docs/layout/SPEC.md#a-schedule-for-a-redefine-chosen-by-position)
states that as *available whichever way the table's count is read*, and
[`docs/ir/SPEC.md`](../../docs/ir/SPEC.md#an-arm-may-be-selected-by-its-position-in-the-table)
is where the argument is made.

What the reading does change is how many of the scheduled arms a given record
reaches. The first member in the round-trip test reaches all three, the second
the first two, the third only the first — and which occurrence takes which arm is
the same in all of them.

## The diagram

[`graph/graph.md`](graph/graph.md) is two halves and the second is the one to read
here. The automaton is three edges — a member, then any number of members —
because this file has one record type and nothing to tell apart between records.
The item table is where the work is:

```
| 41 | 61 × MBR-ADDRESS-COUNT | MBR-ADDRESS | — | — | occurs MBR-ADDRESS-COUNT times (1 to 3) |
| 41 | 61 | MBR-ADDRESS.*variant* | — | — | always |
| 41 | 61 | MBR-ADDRESS.ADR-HOME | — | — | in occurrence 1 of the table |
| 41 | 61 | MBR-ADDRESS.ADR-WORK | — | — | in occurrence 2 of the table |
| 41 | 61 | MBR-ADDRESS.ADR-MAIL | — | — | in occurrence 3 of the table |
| 75 | 27 | MBR-ADDRESS.ADR-MAIL.*slack* | — | — | always |
| 41 + 61 × MBR-ADDRESS-COUNT | 1 | MBR-STATUS | … |
```

Four things an adopter should check against the file on their desk are in those
rows. The **Present** column carries `in occurrence 2 of the table` where
`claim/`'s carries `when CLM-LINE.CLN-KIND = 0xD7` — a position rather than a
test, which is the whole difference between the two examples visible in one
column. The three arms begin at the **same** offset, 41. The twenty-seven bytes
of mailing-address slack are named as a row of their own, under the arm whose
position the schedule fixed. And the offset of `MBR-STATUS` carries a variable
term rather than a number, which is `odoslide` visible at a glance.

## The decisions this example makes

**A directory of its own rather than a second variant inside `claim/`.** The two
examples are a pair and share a construct, so putting both in one directory was a
real option. Against it: [`example/README.md`](../README.md) says adding an
example is adding a directory, and an adopter arriving with a positional table
should find *the one whose file looks like theirs* rather than a second section of
an example about a kind code. The file shapes differ too — a claims extract whose
line items carry types and a membership extract whose addresses carry roles are
not one file — and the two READMEs can each be read alone while still pointing at
each other.

**An `OCCURS DEPENDING ON` rather than a fixed `OCCURS 3 TIMES`.** A schedule
needs only a table, and a fixed one would have been the smaller example. It is an
ODO because the file discussion #340 arrived with is an ODO, and because the fact
worth showing is the one the mechanism was made to be independent of: that the
same three arms are legal under either reading. An example with no reading to
state could not have shown that at all.

**One role per entry rather than an arm naming several.** `(arm ADR-HOME 1 2 3)`
is legal and is the case a table of twenty entries needs, but it is not
discussion #340's file and it reads as arithmetic where this reads as roles. The
repeated-arm case is covered in the conformance corpus — `schedule-sliding`
carries `(arm ADR-WORK 2 3)` — rather than here.

**One record type.** No header, no trailer, no second copybook. What this example
is for happens inside a record, and `ledger/` and `policy/` are already the
examples of files whose record types have to be told apart. `(discriminate
MEMBER-RECORD single-record-type)` is written out even so, because every `record`
form is named by exactly one `discriminate` — which is what makes *this file has
nothing to test* a statement an adopter made rather than a gap.

## Regenerating

From the repository root:

```sh
go build -o "$(go env GOPATH)/bin/cpybkc-gen-go" ./cmd/cpybkc-gen-go
go build -o "$(go env GOPATH)/bin/cpybkc-gen-graph" ./cmd/cpybkc-gen-graph
go run ./cmd/cpybkc --manifest example/member/cpybkc.json
```

Then commit what changed — [`example/regenerate_test.go`](../regenerate_test.go)
is what fails if you do not.
