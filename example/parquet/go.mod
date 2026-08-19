// The worked Parquet conversion, and its own module for one reason: parquet-go
// must not reach the root go.mod.
//
// That module is what `go install github.com/Zaba505/cpybkc/cmd/cpybkc@version`
// builds. Every one of its requires has a paragraph beside it saying why the CLI
// needs it, and a converter's dependency has no business in a signed, attested,
// distroless release image. example/ledger has no go.mod of its own, so anything
// importing parquet-go from inside example/ would land in the root module —
// which is what this file exists to prevent.
//
// module_test.go fails the build if parquet-go ever appears in the root go.mod,
// which is the only enforcement a module boundary has. The precedent is irpb,
// whose own module_test.go keeps its require list at one entry for the same kind
// of reason.
module github.com/Zaba505/cpybkc/example/parquet

go 1.26.2

require (
	github.com/Zaba505/cpybkc v0.0.0
	github.com/parquet-go/parquet-go v0.30.1
)

require (
	github.com/Zaba505/cobol-go v0.0.0-20260815031026-444b99aad1b5 // indirect
	github.com/andybalholm/brotli v1.1.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/parquet-go/bitpack v1.0.0 // indirect
	github.com/parquet-go/jsonlite v1.0.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.21 // indirect
	github.com/twpayne/go-geom v1.6.1 // indirect
	golang.org/x/sys v0.44.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

// The tree beside this one, rather than a published version, because the whole
// point of compiling this instead of writing it in a README is that it is held
// against the generated package as it stands. A version pin would let
// example/ledger's API move and leave this conversion compiling against the one
// before it, which is the rot a checked-in worked example exists to catch.
replace github.com/Zaba505/cpybkc => ../..

// The root module's own replace of the IR module is not inherited: a replace
// directive in a dependency's go.mod is ignored, and only the main module's is
// read. So the same line is restated here. It comes out when irpb carries its
// first tag, at the same time as the root module's.
replace github.com/Zaba505/cpybkc/irpb => ../../irpb
