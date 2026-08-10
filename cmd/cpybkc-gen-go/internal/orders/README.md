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

`roundtrip_test.go` is the exception and is not one of those: it is a test file,
so it is not part of the package the golden pins, and `written` skips a
`_test.go` file for that reason. It is *inside* the package rather than beside
it because two of #51's criteria cannot be stated from outside — the bytes
retained for a slack node are unexported, so a run of the wrong length is
something only code in this package can hand a writer, and an absent run and an
empty one are told apart in a field nobody else can set.

Regenerate it whenever the emitter changes: the failure prints the whole of both
sides, so the new bytes come out of the test's own output.
