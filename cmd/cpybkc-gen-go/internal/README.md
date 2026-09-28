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
`file_test.go` are **output**: the first is one case per record, one per variant
arm and one more per record carrying a table that may be absent, the second one
case per path through the automaton, each carrying the bytes
it reads as a literal, and `written` pins both byte for byte like every other
generated file. Everything else — `file_roundtrip_test.go` in all eight,
`record_roundtrip_test.go` in `orders`, `converted_roundtrip_test.go` in
`counted` and `fixed`, and the four `file_reuse_test.go` — is hand-written and
is skipped, because those assertions live *inside* each package for a reason
the generated ones do not have: the bytes retained for a slack node are
unexported, so a run of the wrong length is something only code in the package
can hand a writer, and the literals a reader or a writer compares are
unexported too, so which ones it holds under an encoding is something only
code in the package can ask.

The hand-written names carry `roundtrip` because `file_test.go` is the name
`cpybkc-gen-go` itself writes, for the file tier; see [the names, and which
side moves](../README.md#the-names-and-which-side-moves).

`file_reuse_test.go` in [`chunks`](chunks), [`orders`](orders),
[`counted`](counted) and [`sep`](sep) is the set that is not a round trip. They
hold what a reader and a writer may *cost*: `chunks` that one more record of a
file costs neither one more decoder nor one more encoder, `orders` that one
sub-decoder and one sub-encoder serve every occurrence of a table holding a
variant, and `counted` and `sep` that a framing which does not state a record's
length costs no decoder per record either. All of them assert in allocations
rather than in nanoseconds, because an allocation count is the same on every
machine and a duration is not, and all of them carry a benchmark beside each
assertion for the nanoseconds somebody reading a regression will want.

The last two are one claim on two arms of the emitter rather than the same
assertion twice. A reader under an unbounded framing rewinds its decoder onto
the *input* rather than onto bytes it holds, so those two also hold what the
rewind must not disturb: that codec's offsets stay counted from the start of the
record, and that a rewind reads nothing ahead of it, so the byte behind a
record's extent is still the framing's to read. `counted` binds a bytes register
and `sep` does not, which is the line of generated code that differs between
them — and it is on the margin, because the tap a bytes register is read through
escapes into the decoder.

## Why there is more than one

A descriptor carries one file node and a file node carries one framing, so what
the reader and the writer do with the bytes around a record cannot be exercised
from a package whose file node names a different one. There is therefore one
package per framing, and one per delimiter placement — and two more that are not
about a framing at all. `batched` is the state whose two transitions no literal
separates, where what a reader and a writer agree on is the order the state
carries and the check that spends it. `signs` is the literals a re-expression
moves by something other than a character, and the one golden whose
`literals.go` carries every file of [`emitted`](../emitted) but the float one,
so that each is compiled and linted in a generated package:

| Package | Framing | What it is for |
|---|---|---|
| [`orders`](orders) | descriptor-word | The record structs and their decode and encode methods, over every shape this generator emits, and a multi-record file through the automaton |
| [`counted`](counted) | delimited, terminator | `ir/SPEC.md`'s *Appendix: A counted run, as nodes* — registers, guards, bindings, and the four things it says a memoryless graph would not detect |
| [`fixed`](fixed) | unframed | A fixed-length dataset: one record type, no predicate, and the read-modify-write job the whole of *Slack survives a read* was written for |
| [`chunks`](chunks) | segmented | Reassembly, and a writer laying a record into as few segments as the largest allows |
| [`sep`](sep) | delimited, separator | A trailing delimiter announcing a record that is not there |
| [`opt`](opt) | delimited, optional terminator | Tuesday's file and Wednesday's, and a writer that emits the final delimiter rather than choosing whether to |
| [`batched`](batched) | unframed | `ir/SPEC.md`'s *A batch boundary is told by the order* — two discriminators at offsets that do not line up, and the writer refusing a detail its own reader would admit as a header |
| [`signs`](signs) | descriptor-word | `ir/SPEC.md`'s *Each axis carries a literal the way a file crosses it* — records told apart by a character, by the sign byte of a signed zoned digit and by a binary count, read and written under a converted encoding and a little-endian one, and refused under the convention that spells two of those literals alike |

Regenerate one whenever the emitter changes: the failure prints the whole of
both sides, so the new bytes come out of the test's own output.
