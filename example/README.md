# A worked example

One artifact carried from a layout to bytes: the files a caller writes, the Go
package cpybkc writes for them, and tests over both.

Everything here is checked in, and the two halves are told apart by which of
them a person edited.

| What a caller writes | What cpybkc writes |
|---|---|
| [`cpybkc.json`](cpybkc.json) — the project manifest | [`ledger/`](ledger) — the generated Go package |
| [`ledger.sexpr`](ledger.sexpr) — the layout | [`cpybkc.gen.json`](cpybkc.gen.json) — the record of what was generated |
| [`header.cpy`](header.cpy), [`posting.cpy`](posting.cpy), [`trailer.cpy`](trailer.cpy) — the copybooks the layout names | |

`ledger/roundtrip_test.go` is neither. It is a test file, so it is not part of
the package the regeneration below pins, and it lives *inside* the generated
package rather than beside it because one of its assertions reaches a field
nothing outside can — see [Slack](#slack-and-why-a-test-lives-inside-the-package).

## Why this exists

Both halves of cpybkc were covered before this directory was, and they met
without touching. `internal/assemble`'s tests drive real layouts through the
real parser to a validated descriptor; the [golden
packages](../cmd/cpybkc-gen-go/internal) start from descriptors written out by
hand. Nothing asserted that the descriptors the goldens pin are descriptors
`assemble` would ever emit, and the path an adopter actually takes — write a
layout, generate, read a file — was the one path nothing ran end to end.

So [`regenerate_test.go`](regenerate_test.go)'s one test regenerates `ledger/`
and `cpybkc.gen.json` from the inputs beside it, through the real CLI and the
real generator built out of the tree under test, and holds every byte of the
result against what is checked in.
A change to any layer — the layout reader, the copybook reader, `resolve`,
`assemble`, the IR or the emitter — arrives as a diff somebody reviews rather
than as a fact buried in an assertion.

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
other. The register is on the path here too: a file whose header says six and
whose body holds five is reported as truncated rather than accepted.

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

## Regenerating

From the repository root:

```sh
go build -o "$(go env GOPATH)/bin/cpybkc-gen-go" ./cmd/cpybkc-gen-go
go run ./cmd/cpybkc --manifest example/cpybkc.json
```

The generator is found on `PATH` by name, which is the whole of how a generator
is identified — the manifest names `go`, and cpybkc looks for `cpybkc-gen-go`.

Then commit what changed. `go test ./example/...` is what fails if you do not:
it regenerates into a temporary directory and holds the result against what is
checked in, printing both sides, so the new bytes come out of the test's own
output.
