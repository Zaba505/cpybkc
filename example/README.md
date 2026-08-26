# Worked examples

Real files carried from a layout to bytes, checked in whole: the inputs a caller
writes, the packages and diagrams cpybkc writes for them, and — where there is
one — what somebody *does* with the result afterwards.

Each example is a directory. Read the one whose file looks like yours.

| Example | The file it describes | What it is here to show |
|---|---|---|
| [`ledger/`](ledger) | a general-ledger extract on a variable-length dataset (`recfm VB`) | six record types out of one `01`-level and the two a real file carries, discrimination at two different offsets, a counted repetition, `REDEFINES` slack surviving a round trip, and a worked Parquet conversion of the result |
| [`policy/`](policy) | a daily policy administration extract on a fixed-block dataset (`recfm FB`) | eleven record types as eleven `01`-levels in one copybook member, a type code at one offset, three grains of nesting with no register, and the width and sparsity a real extract has — 197 fields, 70 of them a field some other record type also declares under its own prefix, and no record carrying more than twenty |

Adding one is adding a directory: everything below reads the tree rather than a
list, so a new example is covered by having been written.

## One manifest per example

Each example carries its **own** `cpybkc.json`, beside the layout it names. There
is no manifest at this level, and nothing here is shared between examples.

The alternative was one `cpybkc.json` at `example/` naming several layouts, and
it was not chosen because that file is not a file with that shape. [The project
manifest](../README.md#the-project-manifest) is one layout, the generators run
over it, and the `cpybkc.gen.json` record cpybkc writes beside it; [`--manifest`
names one of them](../docs/cli/SPEC.md#finding-the-manifest), and every path
inside it is relative to its own directory. Naming several layouts in one file
would mean inventing a second shape for the manifest, in the one directory of
this repository whose whole job is to show an adopter what their own project
looks like — and a worked example that can only be run by a manifest format the
CLI does not have is not a worked example.

So each directory here is exactly what `cpybkc init` derives and exactly what an
adopter checks in: `cpybkc.json`, the layout, the copybooks, the generated
directories, and the record. It is also what makes them independent — an example
whose layout is being reworked does not put every other example's generated tree
in the same diff, and `--manifest` names one of them rather than the tree.

The cost is a `cpybkc.json` per example rather than one file, and duplication is
what would make that a cost. There is none: two examples have different layouts,
different generators and different options, so the only thing the second manifest
repeats is its shape.

## What is asserted, and where

[`regenerate_test.go`](regenerate_test.go) is at *this* level rather than inside
an example, and it is the only file here that is not part of one. It walks every
directory carrying a `cpybkc.json`, regenerates that example from the inputs
beside it — through the real CLI and the real generators, built out of the tree
under test — and holds every byte of the result against what is checked in. An
example that drifts names itself: the failure is a sub-test called after the
directory.

It also makes two assertions a single-generator project could not. That every
generator in a run is handed the same descriptor bytes, and that those are the
bytes `--emit-ir` writes for the same inputs — which needs an example whose
manifest names two, and fails if the tree has stopped holding one. And that the
one `cpybkc.gen.json` per project, rather than one per generator, is what prunes
a file a layout no longer describes.

Two things outside this directory have to be told about a new example even so,
and both fail loudly rather than going uncovered. A generator it names has to
exist as `cmd/cpybkc-gen-<name>`, and [`.dagger/companion.go`](../.dagger/companion.go)
composes the generators into an image by hand because *how* a generator is
installed is not in a manifest.

A conversion written beside an example — `ledger/parquet/` is the one there is —
is a Go module of its own, so that its dependencies never reach the root
`go.mod`. `go test ./example/...` does not run it and neither does the
regeneration above; `dagger call example-parquet-ci` is what does, over every
nested module under this directory.

## Regenerating

From the repository root, once per example. For `ledger/`:

```sh
go build -o "$(go env GOPATH)/bin/cpybkc-gen-go" ./cmd/cpybkc-gen-go
go build -o "$(go env GOPATH)/bin/cpybkc-gen-graph" ./cmd/cpybkc-gen-graph
go run ./cmd/cpybkc --manifest example/ledger/cpybkc.json
```

The `go build` lines are the generators that example's manifest names, found on
`PATH` by name; another example naming another generator builds that one instead.
Then commit what changed — `go test ./example/...` is what fails if you do not,
and each example's own README says what its inputs mean.
