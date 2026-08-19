module github.com/Zaba505/cpybkc

go 1.26.2

// The arrow between this repository's two modules, and it points this way on
// purpose: the CLI consumes the IR, the IR knows nothing about the CLI. irpb's
// own go.mod requires the protobuf runtime and nothing else, and
// irpb/module_test.go fails if that ever stops being true.
require github.com/Zaba505/cpybkc/irpb v0.0.0

require google.golang.org/protobuf v1.36.12

// The COBOL underneath everything this project resolves. `picture` parses a
// PICTURE character-string into the attributes that follow from it and
// `copybook` turns a copybook into a record with a byte width and a byte offset
// on every item, both per codec/SPEC.md — so the storage widths are read once,
// here, rather than a second time beside cobol-go's for the two repositories to
// disagree about. docs/ir/SPEC.md's "Dereferencing is not recomputation" is the
// rule that makes doing it once the whole point.
//
// Pinned to a commit because cobol-go carries no tag yet. It moves to a version
// as soon as one exists: a pseudo-version is a commit an adopter cannot reason
// about, and this is a dependency of the module `go install` builds the CLI
// from.
//
// This commit is the one that gave `codec.Encoding` its fifth axis: the binary
// width staircase a COMP item was compiled under (Zaba505/cobol-go#132). Before
// it, `codec` implemented 2/4/8/16 and nothing else, while `copybook` had
// already been computing widths under whichever staircase the caller's
// `Dialect` named — so the two agreed only because both of this repository's
// entry points hand `copybook` a hardcoded `IBMEnterprise()`. The axis is
// **required** and `Encoding.Validate` refuses the zero value, which is what
// turns that latent disagreement into a refusal a test can see rather than a
// record read at the wrong offsets.
//
// What this repository got from the move is therefore the ability to say where
// its binary widths came from, and #293 is where it says it: the staircase the
// descriptor was resolved under is carried across the IR as
// `Encoding.binary_size` and read back out at the generator, so a file resolved
// under one staircase can no longer be read under another. The move also brings
// `Reader.ResetStream` (Zaba505/cobol-go#131) and case-insensitive data-name
// matching (Zaba505/cobol-go#130); neither changed anything here.
require github.com/Zaba505/cobol-go v0.0.0-20260819200647-d75f1875ab37

// The grammar underneath the layout format. docs/layout/SPEC.md delegates the
// lexis and the parse of a layout file to it — what a symbol is, where a number
// ends, what a comment attaches to, and the line and column every diagnostic is
// built on — so this is the one reading of that grammar in the repository
// rather than a second one beside the document's.
require github.com/z5labs/sexpr-go v0.1.0

// Local until irpb carries its first tag. A module cannot be required at a
// version nobody has published, and irpb/v0.1.0 can only exist on a commit that
// already contains irpb — so the first release of the IR module is necessarily
// preceded by a window in which this line is how the two resolve.
//
// It comes out as soon as that tag exists, and the deadline is real rather than
// tidiness: `go install github.com/Zaba505/cpybkc/cmd/...@version` refuses a
// module whose go.mod carries a replace directive, so this line and the first
// installable CLI command cannot both exist.
replace github.com/Zaba505/cpybkc/irpb => ./irpb
