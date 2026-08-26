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
	"fmt"
	"path"
	"strings"
)

// ModuleDir is the directory a `**/go.mod` glob result names, as a path under
// root.
//
// Dagger globs a Directory and hands back paths relative to it, so finding the
// nested modules under `example/` yields `ledger/parquet/go.mod` and the caller
// wants `example/ledger/parquet`. That is two lines of string surgery, and two
// lines of string surgery over a path is what this package exists to keep out of
// a file no test can reach: a `go.mod` sitting directly under root globs as
// `go.mod`, and trimming the suffix off it leaves an empty string that composes
// into `root/` and then `root//go.mod` — which is neither a failure nor the path
// anybody meant, and would be printed back at whoever had to debug it.
//
// A match that does not name a go.mod is refused rather than guessed at. The
// caller passes a glob result, so a match that is not one means the pattern and
// this function disagree about what was being looked for.
func ModuleDir(root, match string) (string, error) {
	if path.Base(match) != "go.mod" {
		return "", fmt.Errorf("%q does not name a go.mod, so there is no module directory to take from it", match)
	}

	return path.Join(root, path.Dir(match)), nil
}

// RelativeRoot is the path from a directory back to the tree it sits in, written
// the way a go.mod replace directive writes one: a `..` per element of dir, and
// `.` for a directory that is the tree itself.
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
// and what a go.mod writes whatever the host is. It **must** be a relative path
// that stays inside the tree, and one that is not is an error rather than an
// answer — an absolute path, or one that climbs out with `..`, has no number of
// `..` that reaches the root, and returning a plausible one is precisely the
// silent wrong answer this package was split out to stop. Today's caller feeds
// it a glob result so none of them can occur; a guard with a hole fails by
// staying green, which is why the hole is closed anyway.
//
// [Directory]: https://docs.dagger.io/api/reference/
func RelativeRoot(dir string) (string, error) {
	trimmed := strings.TrimSpace(dir)
	if path.IsAbs(trimmed) {
		return "", fmt.Errorf("%q is absolute: a replace directive's target is relative to the go.mod that states it, so an absolute path has no root to point back to", dir)
	}

	clean := strings.Trim(path.Clean(trimmed), "/")
	if clean == "" || clean == "." {
		return ".", nil
	}

	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%q climbs out of the tree it is being measured against, so no number of `..` reaches its root", dir)
	}

	return strings.TrimSuffix(strings.Repeat("../", len(strings.Split(clean, "/"))), "/"), nil
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
