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
and it holds no hand-written Go — a declaration added here by hand would fail
the golden test, which is the right answer.

Regenerate it whenever the emitter changes: the failure prints the whole of both
sides, so the new bytes come out of the test's own output.
