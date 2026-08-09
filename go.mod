module github.com/Zaba505/cpybkc

go 1.26.2

// The arrow between this repository's two modules, and it points this way on
// purpose: the CLI consumes the IR, the IR knows nothing about the CLI. irpb's
// own go.mod requires the protobuf runtime and nothing else, and
// irpb/module_test.go fails if that ever stops being true.
require github.com/Zaba505/cpybkc/irpb v0.0.0

require google.golang.org/protobuf v1.36.11

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
