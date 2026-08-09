// The published IR module. Its own go.mod rather than a package of
// github.com/Zaba505/cpybkc, so that importing the IR costs a plugin author the
// protobuf runtime and nothing the CLI happens to need; see doc.go.
//
// Keep this require list at exactly one entry. boundary_test.go fails the build
// if a second one appears, which is the only enforcement a module boundary has.
module github.com/Zaba505/cpybkc/irpb

// Deliberately behind the toolchain the repository develops on. This module is
// imported by generator plugins written by people who do not work here, and a go
// directive is a floor under every one of them: raising it to whatever this
// repository happens to build with would make an unrelated language feature in
// the CLI a reason a plugin author cannot compile a package of structs. It moves
// when something here needs it to, and the protobuf runtime's own floor — go
// 1.23 — is what it may not fall below.
go 1.24.0

require google.golang.org/protobuf v1.36.11
