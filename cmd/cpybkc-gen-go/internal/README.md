# The golden packages

Everything under this directory is output — the packages `cpybkc-gen-go` writes
for the descriptors its tests build, checked in byte for byte. Two tests
regenerate them and hold every byte of the result against these files, so a
change to what this generator emits is a diff somebody reviews rather than a
fact buried in an assertion.

They are Go **packages** rather than fixtures under `testdata/`, and that is the
point of them. `go build ./...`, `go vet ./...` and golangci-lint all reach a
package and none of them reaches `testdata/`, so *generated code compiles* is
asserted here by the compiler this repository already runs, on every pull
request, rather than by a test that would have to invoke one.

Nothing imports them. They are `internal/` so that nothing outside this command
can, and each holds no hand-written **declaration** — one added by hand would
fail its golden test, which is the right answer.

Two kinds of `_test.go` file sit in each of them, and the `// Code generated …
DO NOT EDIT.` header is what tells them apart. `records_test.go` and
`file_test.go` are **output**: the first is one case per record and per variant
arm, the second one case per path through the automaton, each carrying the bytes
it reads as a literal, and `written` pins both byte for byte like every other
generated file. Everything else — `file_roundtrip_test.go` in all six, and
`record_roundtrip_test.go` in `orders` — is hand-written and is skipped, because
those assertions live *inside* each package for a reason the generated ones do
not have: the bytes retained for a slack node are unexported, so a run of the
wrong length is something only code in the package can hand a writer.

The hand-written names carry `roundtrip` because `file_test.go` is the name
`cpybkc-gen-go` itself writes, for the file tier; see [the names, and which
side moves](../README.md#the-names-and-which-side-moves).

## Why there is more than one

A descriptor carries one file node and a file node carries one framing, so what
the reader and the writer do with the bytes around a record cannot be exercised
from a package whose file node names a different one. There is therefore one
package per framing, and one per delimiter placement:

| Package | Framing | What it is for |
|---|---|---|
| [`orders`](orders) | descriptor-word | The record structs and their decode and encode methods, over every shape this generator emits, and a multi-record file through the automaton |
| [`counted`](counted) | delimited, terminator | `ir/SPEC.md`'s *Appendix: A counted run, as nodes* — registers, guards, bindings, and the four things it says a memoryless graph would not detect |
| [`fixed`](fixed) | unframed | A fixed-length dataset: one record type, no predicate, and the read-modify-write job the whole of *Slack survives a read* was written for |
| [`chunks`](chunks) | segmented | Reassembly, and a writer laying a record into as few segments as the largest allows |
| [`sep`](sep) | delimited, separator | A trailing delimiter announcing a record that is not there |
| [`opt`](opt) | delimited, optional terminator | Tuesday's file and Wednesday's, and a writer that emits the final delimiter rather than choosing whether to |

Regenerate one whenever the emitter changes: the failure prints the whole of
both sides, so the new bytes come out of the test's own output.
