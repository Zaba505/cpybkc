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
// This commit is the one that gave every position cobol-go reports from
// expanded `COPY` text the library text it is in and the chain of `COPY`
// statements that reached it (Zaba505/cobol-go#153). Before it, a `cobol.Pos`
// was a line and a column and nothing else, so an entry a member contributed
// carried a position in a file nothing named; #423 needs that provenance to
// point a fault inside a member into the member, which docs/cli/SPEC.md's
// "Where a `COPY` member is found" requires.
//
// The move also brings what landed in cobol-go between that and the commit
// before it, the one that gave `codec.Encoding` its binary width staircase
// (Zaba505/cobol-go#132), which #293 carries across the IR as
// `Encoding.binary_size`. Four of them change what a copybook reads as, and the
// Definitions notation takes each up in the same change: `VALUES ARE` read with
// `ARE` as noise (Zaba505/cobol-go#145), a prefixed literal such as `X'00'`
// read as one token (Zaba505/cobol-go#150), a word that begins with a digit
// read as a word (Zaba505/cobol-go#151), and `ALL` read with the literal it
// repeats (Zaba505/cobol-go#152). The rest are used where they replace
// something this repository restated: one method for an error's position
// (Zaba505/cobol-go#147), the printer's entry depths (Zaba505/cobol-go#148) and
// its entry check (Zaba505/cobol-go#149), with the reference format the printer
// writes stated (Zaba505/cobol-go#146). The byte domain of an item
// (Zaba505/cobol-go#144) changes nothing here.
//
// A definitions file transcribes this commit's data-description grammar clause
// for clause, so a bump that adds or reshapes a clause trips the guard in
// internal/definitions, and CONTRIBUTING.md's "Bumping the cobol-go pin" says
// what the same change has to extend before it goes green.
require github.com/Zaba505/cobol-go v0.0.0-20261024041309-28b9286a090c

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
