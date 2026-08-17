# A worked example

One artifact carried from a layout to bytes: the files a caller writes, the Go
package and the diagram cpybkc writes for them, and tests over both.

Everything here is checked in, and the two halves are told apart by which of
them a person edited.

| What a caller writes | What cpybkc writes |
|---|---|
| [`cpybkc.json`](cpybkc.json) — the project manifest | [`ledger/`](ledger) — the generated Go package |
| [`ledger.sexpr`](ledger.sexpr) — the layout | [`graph/graph.md`](graph/graph.md) — the generated diagram of the automaton |
| [`header.cpy`](header.cpy), [`posting.cpy`](posting.cpy), [`trailer.cpy`](trailer.cpy) — the copybooks the layout names | [`cpybkc.gen.json`](cpybkc.gen.json) — the record of what was generated |

The manifest names **two** generators, `go` and `graph`, each with its own `out`
directory, because the path an adopter takes has two steps on it: generate, and
then *look at the graph* before trusting what was generated. It is also the only
project in this repository that runs more than one generator, which is what makes
it the place [one thing the plugin contract
asserts](#one-descriptor-two-generators) can be tested at all.

`ledger/roundtrip_test.go` is neither. It is hand-written, and it lives *inside*
the generated package rather than beside it because one of its assertions reaches
a field nothing outside can — see
[Slack](#slack-and-why-a-test-lives-inside-the-package). The regeneration below
leaves it alone.

`ledger/records_test.go` and `ledger/file_test.go` **are** generated, and they
are pinned like every other generated file. `cpybkc-gen-go` writes one case per
record type and per variant arm into the first, and one case per path through the
automaton into the second, each carrying the bytes it reads as a literal with the
item, the offset and the picture in the comment column — read against the layout
rather than against a file, which is the spot-check an adopter makes before
opening a real dataset. The two kinds of `_test.go` are told apart by the
`// Code generated … DO NOT EDIT.` header rather than by the suffix.

The file tier and [`graph/graph.md`](graph/graph.md) are the two halves of one
spot-check and are meant to be held against each other. The diagram answers
*which records, in which order, told apart on which bytes* about the descriptor:
`s77 --> s78: LEDGER-HEADER, when HDR-TYPE = 0xF0 0xF1, then r76 = HDR-COUNT` is
the edge, and `TestALedgerHeaderThenTwoDebitPostingsThenALedgerTrailer` is that
edge and the two it leads to as bytes — `0xf0 0xf1` at offset 0 of the first
record, `0xf0 0xf0 0xf2` in `HDR-COUNT`, and the two debit postings the register
it bound then admits. Seven cases cover all eight of the automaton's
discriminators; the fifty edges the diagram draws are fifty ways to arrive at
those eight, and [why one case per predicate rather than one per
edge](../cmd/cpybkc-gen-go/README.md#decided-the-file-tier-covers-by-predicate-not-by-edge)
is the whole of that decision.

## Why this exists

Both halves of cpybkc were covered before this directory was, and they met
without touching. `internal/assemble`'s tests drive real layouts through the
real parser to a validated descriptor; the [golden
packages](../cmd/cpybkc-gen-go/internal) start from descriptors written out by
hand. Nothing asserted that the descriptors the goldens pin are descriptors
`assemble` would ever emit, and the path an adopter actually takes — write a
layout, generate, read a file — was the one path nothing ran end to end.

So [`regenerate_test.go`](regenerate_test.go) regenerates `ledger/`,
`graph/graph.md` and `cpybkc.gen.json` from the inputs beside it, through the
real CLI and both real generators built out of the tree under test, and holds
every byte of the result against what is checked in.
A change to any layer — the layout reader, the copybook reader, `resolve`,
`assemble`, the IR or either emitter — arrives as a diff somebody reviews rather
than as a fact buried in an assertion. The diagram is the half of that a reviewer
can read without reading Go: a change to what `cpybkc-gen-graph` draws shows up
here as the picture changing.

Which generators *that test* covers is read out of `cpybkc.json` rather than
written down beside it — the names it builds, the executables it puts on `PATH`
and the directories it walks all come from the manifest — so a third generator
added here is covered by having been added, as long as it is a command in this
repository's `cmd/`.

Two things outside this directory do have to be told, and a third entry that
skipped them would fail rather than go uncovered.
[`.dagger/companion.go`](../.dagger/companion.go) composes the generators into an
image by hand and requires the plugin directory to hold exactly them — it cannot
read the manifest, because *how* a generator is installed is not in it: `go` has
a published image and `graph` does not. And `cmd/cpybkc-gen-<name>` has to exist
to be built.

The generated code is a Go **package** rather than a fixture under `testdata/`,
for the reason [the golden packages](../cmd/cpybkc-gen-go/internal/README.md)
are: `go build ./...`, `go vet ./...` and golangci-lint all reach a package and
none of them reaches `testdata/`, so *generated code compiles* is asserted by
the compiler this repository already runs.

## The file

A general-ledger extract on a variable-length dataset. A header, then the
postings it counts, then a trailer. `(recfm VB)` rather than `F`, so every
record sits behind the record descriptor word DFSMS defines and the framing
layer is on the path the round-trip test runs.

## What is hard about it

An adopter reads a worked example to find out whether their file is
describable, so the layout here is deliberately not a simple one. Three shapes
in it are ordinary in production files and each is the reason this example is
shaped the way it is.

### Six record types out of one `01`-level

`posting.cpy` describes one fifty-byte record and redefines two independent runs
inside it. `PST-BODY` is described three ways — as itself, as `PST-DEBIT` and as
`PST-CREDIT`. `PST-TAIL` is described two ways — as itself and as
`PST-TAIL-REF`. A `REDEFINES` outside a repeating group is resolved away into
one record type per alternative, and because the two runs are independent the
alternatives *multiply* rather than merely branch: **six** record types come out
of this one `01`-level.

The layout is where each of them is named. Every one of the six `record` forms
names the same copybook and the same `01`-level, and carries one `alternative`
child per redefined run saying which description of it that form means:

```
(record CREDIT-POSTING-REF
  (copybook "posting.cpy" POSTING-RECORD)
  (alternative (item CREDIT-POSTING-REF PST-CREDIT))
  (alternative (item CREDIT-POSTING-REF PST-TAIL-REF)))
```

The pairing is stated and never inferred — cpybkc will not decide which bytes a
record type describes by looking at which alternative its discriminator happens
to reach into.

All six carry one name in the IR, `POSTING-RECORD`, because that is what the
copybook calls the record each of them is. Whether one name on six record types
is a problem is a property of the target language, so the layout format does not
require them to be told apart; Go requires it, and `cpybkc-gen-go` refuses to
munge six record types into one identifier rather than picking. That is what the
six `rename` forms are for, and they are why the package holds `DebitPosting`,
`CreditPostingRef` and the rest.

### Discrimination at two different offsets

The header and the trailer are selected on the field they open with. A posting
is selected on `PST-TYPE`, which sits **twelve bytes in**, behind the account
number and sequence every posting shares. Both are `equals` on an item
reference: one strategy, two offsets.

#### Why the header counts the postings

Two records that may be admitted at the *same* point in a file cannot be
selected at two different offsets. A discriminator is evaluated before the
record in front of a consumer has been identified, so nothing rules out a record
carrying `99` in its first two bytes *and* `DA` in bytes twelve and thirteen —
and a state offering two transitions that can both apply is a layout `resolve`
rejects before a consumer ever sees it.

`(seq LEDGER-HEADER (* posting) LEDGER-TRAILER)` would be exactly that: after
the header, both a posting and the trailer are admissible. So the header states
how many postings follow, and the sequence counts them:

```
(sequence
  (seq LEDGER-HEADER
       (times (alt DEBIT-POSTING …) (item LEDGER-HEADER HDR-COUNT))
       LEDGER-TRAILER))
```

`times` reads `HDR-COUNT` into a register the automaton remembers in, and the
transitions out of the counting state carry guards over it. A posting is
admitted while the count is outstanding and the trailer once it is spent, so no
state offers both and the two offsets never have to be told apart from each
other. The register is on the path here too, and asserted from both ends: a file
whose header says six and whose body holds five is reported rather than returned
as the five records it managed, and a writer closed a posting short of its own
header count is reported rather than emitting the file its reader would have
complained about one build later.

The other way to write it is to put the two records at different points in the
sequence outright. Either way, the rule is about *states* and not about offsets:
what a layout may not do is ask a consumer to choose between two records by
reading two different places.

### Slack, and why a test lives inside the package

`PST-CREDIT` is four bytes shorter than the run it redefines, which is ordinary
COBOL and leaves four bytes no credit posting describes. Those bytes are read
and carried out again unchanged, which is what makes a credit posting written
back *byte*-identical rather than merely equal field by field.

They are retained in an unexported field, so the assertion that they survived a
read is one only code inside the generated package can make. That is why
`ledger/roundtrip_test.go` is inside `ledger/` rather than beside it.

## The diagram

[`graph/graph.md`](graph/graph.md) is what
[`cpybkc-gen-graph`](../cmd/cpybkc-gen-graph/) draws for this layout: the
sequencing automaton as a Mermaid `stateDiagram-v2`, the registers it remembers
in, and one table of items and offsets per record. It is the question an adopter
asks before they trust a layout — *the right records, in the right order, told
apart on the right bytes, at the right offsets* — asked of the descriptor rather
than of the file a person wrote.

Everything in it is checkable against [`ledger.sexpr`](ledger.sexpr) by eye, and
the point of it being here is that you should:

- **Header, postings, trailer, in that order.** `[*] --> s77` is where a read
  begins; the one edge out of it is `LEDGER-HEADER`. Everything after it loops
  among the postings, and the only way out is `LEDGER-TRAILER`. That is
  `(seq LEDGER-HEADER (times (alt …)) LEDGER-TRAILER)` drawn.
- **Six posting types, under a count.** Every state between the header and the
  trailer offers all six — `DEBIT-POSTING`, `DEBIT-POSTING-REF`,
  `CREDIT-POSTING`, `CREDIT-POSTING-REF`, `MEMO-POSTING`, `MEMO-POSTING-REF` —
  which is the six `record` forms the copybook's three `REDEFINES` over two
  independent runs resolve to, and the six names the `(alt …)` lists. Reading
  one of them arrives at a state of its own, and a posting may be followed by a
  posting of any type, so those seven states each carry the same seven
  transitions and the picture is the complete graph over them. That density is
  the layout's, not the drawing's.
- **`HDR-COUNT` in the register table.** The header's edge carries
  `then r76 = HDR-COUNT`; every posting edge carries `if r76 greater than zero,
  then r76 = r76 - 1`; the trailer's carries `if r76 = 0`. The **Registers**
  table names `r76` and every transition that binds it, because a register
  carries an identifier and no name — without it the guards on the edges say
  nothing.

  This is where the diagram is worth more than the prose above it. [Why the
  header counts the postings](#why-the-header-counts-the-postings) says no state
  offers both a posting and the trailer, and what is drawn is every posting state
  offering both. Both are true, and the guards are the difference: the
  alternatives are written at one state and are mutually exclusive on `r76`, so
  no *reachable* choice between them exists and the two offsets never have to be
  told apart. Read the guards, not the edge count.
- **Two offsets, in the item tables.** `PST-TYPE` is at offset **12** in every
  posting table, behind the ten-byte account key and the two-byte sequence every
  posting shares; `HDR-TYPE` and `TRL-TYPE` are both at offset **0**. Those are
  the eight `discriminate` forms, and the tables are where you check that the
  field a discriminator names is the field you meant.
- **The literals are bytes.** `when PST-TYPE = 0xC4 0xC1` is `"DA"` in cp037,
  which is the charset `(encoding (charset cp037) …)` declares. A diagram that
  printed `"DA"` would be printing ASCII for a field that is not in it.
- **`*slack*` in the credit tables.** Four bytes at offset 38 that no credit
  posting describes — `PST-CREDIT` being shorter than the run it redefines —
  drawn as a row of its own rather than as a gap between two offsets.

**The offsets are summed, not read.** No IR node carries one, so every number in
those tables is the generator's own arithmetic; the document says so itself. That
is why the tables are worth checking against a copybook rather than being taken
as a restatement of it.

`format=mermaid` is the manifest's choice and it is a judgment call: this
automaton is dense — nine states and fifty transitions — and
[`cpybkc-gen-graph`'s README](../cmd/cpybkc-gen-graph/README.md#which-rendering-to-reach-for)
reaches for `dot` at that density. Mermaid is what the example commits anyway,
because a worked example is read where it is checked in: a `.md` renders the
diagram on the forge, and its registers and item tables are Markdown tables a
reader can check by eye in the diff, where Graphviz's are HTML markup inside a
file that renders nowhere on its own. Changing `"format": "mermaid"` to `"dot"`
in [`cpybkc.json`](cpybkc.json) and regenerating is what it takes to see the
other one.

## One descriptor, two generators

`docs/plugin/SPEC.md` says a run assembles **one** descriptor and hands every
generator in it the same bytes — and that those bytes are what `--emit-ir`
writes for the same inputs, which is what makes reproducing a failing generation
by hand possible at all. With one generator in a manifest the equality has
nothing to hold between: there is no second set of bytes, so it is satisfied by
there being nothing to compare.

This is the first project in the repository to run two, so
[`regenerate_test.go`](regenerate_test.go)'s second test is the first place it
can fail. It puts a stub generator on `PATH` under both of the manifest's names —
one that copies the file at `--descriptor` into its own `--out` and writes
nothing else — runs cpybkc over this manifest, and requires the two copies that
land in the project to be byte-identical to each other and to what
`--emit-ir` writes for the same run.

What it does not show is the *one* in "one descriptor". Assembly is
deterministic, so a pipeline that assembled a fresh descriptor for each
generator would hand both stubs identical bytes and pass. Counting assemblies
would need them to be observable and nothing makes them so — and the equality,
not the count, is what a reader reproducing a generation by hand depends on.

Two more things a one-generator manifest could not show are covered beside it.

[`cpybkc.gen.json`](cpybkc.gen.json) records **both** generators' output, sorted
together, and it is that one record — not one per generator — which drives
stale-file pruning. That is what lets a generator *removed* from the manifest
have its output pruned, so a third test starts from a project whose record names
a stale file in every output directory and requires every one of them to be gone
and the record to come out as the one checked in. A pipeline keeping the record
per generator behaves identically with one generator and prunes the wrong
directory with two.

And a run that merges two scratch directories is one that fails whole if either
generator fails: nothing reaches the project tree until every generator has
exited zero, so a diagram is never written for a package that failed to
generate.

## Regenerating

From the repository root:

```sh
go build -o "$(go env GOPATH)/bin/cpybkc-gen-go" ./cmd/cpybkc-gen-go
go build -o "$(go env GOPATH)/bin/cpybkc-gen-graph" ./cmd/cpybkc-gen-graph
go run ./cmd/cpybkc --manifest example/cpybkc.json
```

Each generator is found on `PATH` by name, which is the whole of how a generator
is identified — the manifest names `go` and `graph`, and cpybkc looks for
`cpybkc-gen-go` and `cpybkc-gen-graph`.

The two `go build` lines are what a contributor to *this* repository runs, and
they are the route that works today. Working from a release instead, both
generators are published as images and neither needs a Go toolchain:

```console
$ docker pull ghcr.io/zaba505/cpybkc-gen-go:v0
$ docker pull ghcr.io/zaba505/cpybkc-gen-graph:v0
```

Either is copied into an image built `FROM ghcr.io/zaba505/cpybkc`, or composed
by the [companion Dagger module](../daggerverse/cpybkc/)'s `with-generator`,
which resolves those references for `go` and `graph` on its own. [Where this
project's own generators are
published](../docs/container/SPEC.md#where-this-projects-own-generators-are-published)
is the rule they follow, and [adding a
generator](../docs/container/SPEC.md#worked-example-adding-a-generator) is the
`COPY --from` it takes.

**Until the first release under that rule is cut, those references resolve to
nothing**, exactly as [the container contract
says](../docs/container/SPEC.md#where-this-projects-own-generators-are-published).
Before it, the two `go build` lines above are the way to regenerate this
example; they stay the way a contributor does it afterwards.

Then commit what changed. `go test ./example/...` is what fails if you do not:
it makes the same run into a temporary directory and holds the result against
what is checked in, naming the file the two disagree on.

What it prints of that file depends on how long it is. A short one — the diagram
is under two hundred lines — is printed whole, both sides, so that what a change
did to the picture is legible in the failure and the new bytes come out of the
test's own output. A long one is not: `ledger/file.go` alone is several thousand
lines, and a failure carrying two copies of it is one nobody reads, so that one
names the first line the two disagree on and leaves regenerating to the commands
above.
