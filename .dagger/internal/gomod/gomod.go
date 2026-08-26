// Package gomod answers the two questions the pipeline asks about a local
// replace directive: does a go.mod carry this one exactly, and how far back up
// does a nested module have to point to reach the tree it sits in?
//
// It is a package of its own for the reason [surface] is: the pipeline's own
// package main imports the generated Dagger client, whose init panics without a
// session, so a test beside it cannot run under plain `go test`. This package
// imports no Dagger and takes file contents rather than a *dagger.Directory, so
// every rule below is pinned by a test.
//
// That matters here for the same reason it matters there, and it is not
// hypothetical. What is built on this is a guard over an example's nested
// go.mod, and the first version of that guard was a `strings.Contains` — which
// reported a directive re-pointed from `../..` to `../../wrong` as present,
// because the one is a prefix of the other. A guard with a hole fails by staying
// green.
//
// [surface]: https://github.com/Zaba505/cpybkc/tree/main/.dagger/internal/surface
package gomod

import (
	"path"
	"strings"
)

// RelativeRoot is the path from a directory back to the tree it sits in, written
// the way a go.mod replace directive writes one: a `..` per element of dir, and
// `.` for a directory that is the tree.
//
// It is here rather than beside its caller for the reason [HasReplacement] is,
// and it is the same class of mistake being guarded. A nested module moved one
// level deeper needs one more `..`, and a directive left a level short does not
// fail loudly — it names a directory that exists and is not the repository root,
// and `go mod edit -replace` would then write that one over the committed
// directive rather than refuse it. Path arithmetic nothing can test is how a
// pipeline ends up re-pointing a module at the wrong tree.
//
// dir is a slash-separated path, which is what a Dagger [Directory] hands back
// and what a go.mod writes whatever the host is.
//
// [Directory]: https://docs.dagger.io/api/reference/
func RelativeRoot(dir string) string {
	clean := strings.Trim(path.Clean(strings.TrimSpace(dir)), "/")
	if clean == "" || clean == "." {
		return "."
	}

	return strings.TrimSuffix(strings.Repeat("../", len(strings.Split(clean, "/"))), "/")
}

// HasReplacement reports whether contents carries the replacement spec, which is
// written the way a go.mod writes it: `<module> => <target>`, or
// `<module> <version> => <target> <version>`.
//
// Both forms a go.mod may put a directive in satisfy it — a `replace` line
// standing on its own, and a line inside a parenthesised `replace` block — since
// the two say the same thing and which one a file uses is `go mod tidy`'s
// business rather than a decision anybody made. Whitespace between the tokens is
// not significant and a trailing `//` comment is ignored, so a reformat does not
// turn a present directive into an absent one.
//
// Everything else is significant, and deliberately. A directive whose target has
// been re-pointed is not the directive asked for, a versioned replacement is not
// an unversioned one, and a module path that merely has the asked-for one as a
// prefix — `github.com/Zaba505/cpybkc/irpb` against `github.com/Zaba505/cpybkc`
// — is a different module.
func HasReplacement(contents, spec string) bool {
	want := normalize(spec)
	if want == "" {
		return false
	}

	for line := range strings.SplitSeq(contents, "\n") {
		if normalize(line) == want {
			return true
		}
	}

	return false
}

// normalize is one line of a go.mod reduced to the directive it states: its
// comment dropped, an opening `replace` keyword dropped, and every run of
// whitespace collapsed to one space.
//
// A line that states no replacement normalizes to something that cannot equal a
// normalized spec, because a spec carries `=>` and nothing else in a go.mod
// does.
func normalize(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		line = line[:i]
	}

	fields := strings.Fields(line)
	if len(fields) > 0 && fields[0] == "replace" {
		fields = fields[1:]
	}

	return strings.Join(fields, " ")
}
