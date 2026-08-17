# The golden package

Everything beside this file is output — the package `cpybkc-gen-go` writes for
the descriptor `record_test.go` builds, checked in byte for byte.
`TestTheGeneratedPackageIsTheGolden` regenerates it and holds every byte of the
result against these files, so a change to what this generator emits is a diff
somebody reviews rather than a fact buried in an assertion.

It is a Go **package** rather than a fixture under `testdata/`, and that is the
point of it. `go build ./...`, `go vet ./...` and golangci-lint all reach a
package and none of them reaches `testdata/`, so *generated code compiles* is
asserted here by the compiler this repository already runs, on every pull
request, rather than by a test that would have to invoke one.

Nothing imports it. It is `internal/` so that nothing outside this command can,
and it holds no hand-written **declaration** — one added here by hand would fail
the golden test, which is the right answer.

`record_roundtrip_test.go` and `file_roundtrip_test.go` are the exceptions and
are not those: they are hand-written, and `written` skips a `_test.go` file that
does not open with the `// Code generated … DO NOT EDIT.` header for that reason.
They are *inside* the package rather than beside it because some of the criteria
cannot be stated from outside — the bytes retained for a slack node are
unexported, so a run of the wrong length is something only code in this package
can hand a writer, an absent run and an empty one are told apart in a field
nobody else can set, and holding a record across a later read and asserting that
it still carries its own slack reaches the same field.

The two divide by layer. `record_roundtrip_test.go` asserts a *record* reading
and writing its own bytes; `file_roundtrip_test.go` asserts the layer above —
the framing around a record, the order records come in, and the two ends of a
file. The other five packages beside this one are the same assertions under the
other framings; [`../README.md`](../README.md) says which is which. Both carry
`roundtrip` in the name because `file_test.go` is a name `cpybkc-gen-go` itself
will write, for the file tier of the generated tests.

`records_test.go` **is** output and is pinned like every other file here: one
case per record type and per variant arm, each carrying that record's bytes as a
literal. `SHAPE-RECORD` is in the descriptor for it — no transition admits that
record, and it is where COMP-6, COMP-2, COMP-5 and the two USAGEs beside INDEX
that the IR derives no logical value for get a case the compiler and `go test
-race` actually run.

Regenerate it whenever the emitter changes: the failure prints the whole of both
sides, so the new bytes come out of the test's own output.
