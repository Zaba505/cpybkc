// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package cpybkc_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// cpybkc reads no ambient temporary directory (#184). A run's scratch space is
// made in the project it was pointed at, and each invocation's descriptor
// directory beside the output directory that invocation writes into, so nothing
// about a run needs a writable /tmp, a TMPDIR, or any other directory the
// machine happened to name.
//
// That is a property of the whole repository rather than of one package, so it
// is checked as one: the fields that used to make an ambient directory
// reachable are gone or required, and this is what says no new way of reaching
// one has appeared beside them. docs/cli/SPEC.md's "The environment" is the
// statement being kept true — PATH is the one variable a run reads — and
// docs/container/SPEC.md's "Shell or no shell" is what rests on it, since the
// published image is scratch plus the files that document names and there is no
// /tmp in it to reach.
//
// A parse rather than a grep, for internal/plugin's determinism scan's reason:
// the name a package is imported under is not the name in its path, so an alias
// would carry os.TempDir straight past a search for that text. Comments are not
// parsed at all, which is why the prose above and the several comments in this
// repository that name /tmp are not findings.
//
// # What holds the property, and what this only guards
//
// The types hold it. internal/plugin.Runner has no field for a temporary
// directory at all and puts each descriptor directory beside the output
// directory the invocation cannot be run without; internal/generate.Runner and
// internal/conformance/goadapter.Adapter each refuse a run whose directory field
// is empty, before a generator starts. Those are the three os.MkdirTemp call
// sites in this repository, and every one of them passes a variable.
//
// This scan cannot see any of them, and that is not a gap to be closed here:
// deciding whether an arbitrary expression can be empty is a type-checker in a
// test, and the three answers it would give are already given by the code. What
// the scan guards is the next call site — source written naively, where the
// ambient directory is asked for outright — and the ways of asking that a
// search for `os.TempDir` would miss. It is a fence around a decision the types
// have already made, not the decision itself.
//
// It does not resolve shadowing, does not follow a forbidden function used as a
// value (`f := os.TempDir`), and does not read a variable passed to os.Getenv.
// Generated files are scanned like any other, deliberately: a generated client
// that reached a temporary directory would reach one at run time exactly as
// hand-written source would, and an exemption written before anybody has seen
// such a case is an exemption nobody would ever revisit.

// searched is every tree held to the rule.
//
// The three the story names, and all of the source in each: internal/ is where
// a run's directories are made, cmd/ is what points a run at a project, and
// daggerverse/ is the module an adopter calls from their own pipeline, running
// against the same image. .dagger/ is not here — it is this repository's own
// build, it runs on a machine with a /tmp, and what it may put in the *image*
// is checked exhaustively by ImageContract instead.
var searched = []string{"cmd", "daggerverse", "internal"}

// readsAnAmbientDirectory is a call that reaches a temporary directory nobody
// named, by import path and the name reached through it.
//
// [os.TempDir] is the answer itself. io/ioutil's two are the pre-1.16 spelling
// of the same thing and are listed whatever they are passed, because the
// package is deprecated and neither belongs in new source at all.
var readsAnAmbientDirectory = map[string][]string{
	"os":        {"TempDir"},
	"io/ioutil": {"TempDir", "TempFile"},
}

// defaultsToAnAmbientDirectory is a call whose first argument is the directory
// to create in, and which falls back to [os.TempDir] when that argument is
// empty.
//
// Only an empty *literal* is a finding. An argument that is anything else is a
// directory the caller was given or chose, and whether it can be empty is a
// question about a type rather than about a call — which is why #184 answers it
// in the types: internal/plugin.Runner no longer has a field for one, and
// internal/generate.Runner's is required and refused when it is empty.
var defaultsToAnAmbientDirectory = map[string][]string{
	"os": {"CreateTemp", "MkdirTemp"},
}

// namesAnAmbientDirectory is a call that reads one out of the environment, by
// the name it would be asked for.
var namesAnAmbientDirectory = map[string][]string{
	"os": {"Getenv", "LookupEnv"},
}

// temporaryDirectoryVars is every environment variable that names one. TMPDIR
// is POSIX's; the other two are what a program ported from Windows reaches for.
var temporaryDirectoryVars = []string{"TEMP", "TMP", "TMPDIR"}

// temporaryDirectoryPaths is every path a literal would spell one as. A string
// equal to one of these, or beneath one, is a finding wherever it appears:
// there is no legitimate reason for this repository's source to name the
// machine's temporary directory at all.
var temporaryDirectoryPaths = []string{"/tmp", "/var/tmp"}

func TestNothingReadsAnAmbientTemporaryDirectory(t *testing.T) {
	t.Parallel()

	for _, tree := range searched {
		t.Run(tree, func(t *testing.T) {
			t.Parallel()

			if _, err := os.Stat(tree); err != nil {
				t.Fatalf("%s is held to the rule and is not there: %v", tree, err)
			}

			found, files := scan(t, tree)

			// A scan that walked nothing reports nothing, which is the one way
			// this check could pass without looking. The trees below hold
			// dozens of files each; one is enough to say the walk works.
			if files == 0 {
				t.Fatalf("the scan read no Go source under %s, so it found nothing by not looking", tree)
			}

			for _, finding := range found {
				t.Errorf("%s, and cpybkc reads no temporary directory it was not given (#184)", finding)
			}
		})
	}
}

// reachesOne is source with every way of reaching an ambient temporary
// directory in it, one per line, including two the text of `os.TempDir` would
// not find: an alias, and a package whose import is dotted.
const reachesOne = `package gen

import (
	"io/ioutil"
	stdos "os"
	"path/filepath"
)

func Scratch(parent string) (string, error) {
	_ = stdos.TempDir()
	_, _ = stdos.MkdirTemp("", "x-")
	_, _ = stdos.CreateTemp("", "x-")
	_ = stdos.Getenv("TMPDIR")
	_, _ = stdos.LookupEnv("TMPDIR")
	_, _ = ioutil.TempDir("", "x-")
	_, _ = ioutil.TempFile("", "x-")
	_ = filepath.Join("/tmp", "x")
	_ = "/var/tmp/x"

	return stdos.MkdirTemp(parent, "x-")
}
`

// dotted is the other way a call hides from a text search: the package's names
// are in the file's own scope, so nothing spells out which package TempDir came
// from.
const dotted = `package gen

import . "os"

func Scratch() string { return TempDir() }
`

// reachesNone is source the scan has to leave alone: a temporary directory made
// under a parent the caller supplied, a method whose name matches one of the
// forbidden ones on a receiver that is not a package, an environment variable
// that is not one of these, and a path that only starts like one.
const reachesNone = `package gen

import "os"

type fake struct{}

func (fake) TempDir() string { return "" }

const home = "/tmpl/templates"

func Scratch(f fake, parent string) (string, error) {
	_ = f.TempDir()
	_ = os.Getenv("PATH")
	_ = home

	return os.MkdirTemp(parent, "x-")
}
`

func TestTheScanSeesEveryWayOfReachingOne(t *testing.T) {
	t.Parallel()

	found, _ := scan(t, fixture(t, reachesOne))

	want := []string{
		`"/tmp"`,
		`"/var/tmp/x"`,
		"ioutil.TempDir",
		"ioutil.TempFile",
		"stdos.CreateTemp with no directory to create in",
		"stdos.Getenv(\"TMPDIR\")",
		"stdos.LookupEnv(\"TMPDIR\")",
		"stdos.MkdirTemp with no directory to create in",
		"stdos.TempDir",
	}

	// Compared by what each finding names rather than by where it was, so that
	// editing the fixture is not a two-place change.
	if got := slices.Sorted(slices.Values(named(found))); !slices.Equal(got, want) {
		t.Errorf("the scan found %v, want %v", got, want)
	}
}

func TestTheScanRefusesADotImportItCannotResolve(t *testing.T) {
	t.Parallel()

	found, _ := scan(t, fixture(t, dotted))

	want := []string{"a dot import of os"}

	if got := named(found); !slices.Equal(got, want) {
		t.Errorf("the scan found %v, want %v", got, want)
	}
}

func TestTheScanAcceptsSourceThatOnlyLooksLikeItReachesOne(t *testing.T) {
	t.Parallel()

	if found, _ := scan(t, fixture(t, reachesNone)); len(found) != 0 {
		t.Errorf("the scan found %v in source that reaches no temporary directory", found)
	}
}

// fixture is a directory holding src as one Go file, beside a test file the
// scan has to skip: a test may name a temporary directory, and one that could
// not would not be able to assert that nothing else does.
func fixture(t *testing.T, src string) string {
	t.Helper()

	dir := t.TempDir()

	for name, content := range map[string]string{
		"gen.go":      src,
		"gen_test.go": reachesOne,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	return dir
}

// scan reports everything under root's non-test Go files that reaches a
// temporary directory nobody named, as `file:line:col: what`, and how many
// files it read.
//
// The whole tree rather than one directory, because the rule is about three
// trees and not about a package: a new package added under one of them is held
// to it from the commit that adds it rather than from the commit where somebody
// remembered to list it.
func scan(t *testing.T, root string) ([]string, int) {
	t.Helper()

	fset := token.NewFileSet()

	var (
		found []string
		files int
	)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir():
			// testdata is what a test reads, and go/build ignores it for the
			// same reason: it is not source this repository compiles.
			if d.Name() == "testdata" {
				return fs.SkipDir
			}

			return nil
		case !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go"):
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}

		files++

		imported, dotted := imports(t, fset, file)
		found = append(found, dotted...)
		found = append(found, reaches(fset, file, imported)...)

		return nil
	})
	if err != nil {
		t.Fatalf("reading %s: %v", root, err)
	}

	slices.Sort(found)

	return found, files
}

// reaches is everything in one parsed file that reaches a temporary directory
// nobody named, given what each of its imports is called.
func reaches(fset *token.FileSet, file *ast.File, imported map[string]string) []string {
	var found []string

	report := func(pos token.Pos, what string) {
		found = append(found, fmt.Sprintf("%s: %s", fset.Position(pos), what))
	}

	ast.Inspect(file, func(n ast.Node) bool {
		// A literal naming the machine's temporary directory, wherever it is
		// written: as an argument, as a constant, as half of a concatenation.
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if value, err := strconv.Unquote(lit.Value); err == nil && isTemporaryPath(value) {
				report(lit.Pos(), lit.Value)
			}

			return true
		}

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		pkg, name, ok := qualified(call.Fun, imported)
		if !ok {
			return true
		}

		switch {
		case listed(readsAnAmbientDirectory, pkg, name):
			report(call.Pos(), called(call.Fun, name))

		case listed(defaultsToAnAmbientDirectory, pkg, name) && emptyString(call.Args):
			report(call.Pos(), called(call.Fun, name)+" with no directory to create in")

		case listed(namesAnAmbientDirectory, pkg, name):
			if v, ok := literal(call.Args); ok && slices.Contains(temporaryDirectoryVars, v) {
				report(call.Pos(), fmt.Sprintf("%s(%q)", called(call.Fun, name), v))
			}
		}

		return true
	})

	return found
}

// qualified is the import path and the name of a call written as `pkg.Name`,
// and whether it is one at all.
//
// A qualified identifier is a package name and nothing else, so a method call
// on a value that happens to be named for a package — a receiver called `os` —
// is not one. Shadowing is not resolved, for the reason internal/plugin's scan
// gives: the cost of missing a call made through a variable that took a
// package's name is a finding this scan does not report, and the cost of
// resolving it is a type-checker in a test.
func qualified(fun ast.Expr, imported map[string]string) (pkg, name string, ok bool) {
	selector, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}

	ident, ok := selector.X.(*ast.Ident)
	if !ok {
		return "", "", false
	}

	pkg, ok = imported[ident.Name]

	return pkg, selector.Sel.Name, ok
}

// called is how the call reads in the source, which is what a finding names:
// the identifier the package was imported under, and not its path.
func called(fun ast.Expr, name string) string {
	if selector, ok := fun.(*ast.SelectorExpr); ok {
		if ident, ok := selector.X.(*ast.Ident); ok {
			return ident.Name + "." + name
		}
	}

	return name
}

// listed reports whether pkg's name is on one of the lists above.
func listed(names map[string][]string, pkg, name string) bool {
	return slices.Contains(names[pkg], name)
}

// literal is the value of a call's first argument where it is a string
// literal.
func literal(args []ast.Expr) (string, bool) {
	if len(args) == 0 {
		return "", false
	}

	lit, ok := args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}

	value, err := strconv.Unquote(lit.Value)

	return value, err == nil
}

// emptyString reports whether a call's first argument is the empty string
// written out.
func emptyString(args []ast.Expr) bool {
	value, ok := literal(args)

	return ok && value == ""
}

// isTemporaryPath reports whether a literal spells the machine's temporary
// directory or something beneath it.
func isTemporaryPath(value string) bool {
	for _, dir := range temporaryDirectoryPaths {
		if value == dir || strings.HasPrefix(value, dir+"/") {
			return true
		}
	}

	return false
}

// imports is what each of file's imports is called where it is used, against
// the path it names, and the findings the import block is itself.
//
// A dot import of a watched package is a finding rather than an entry: it puts
// that package's names in the file's own scope, where nothing distinguishes
// `TempDir()` from a function of the file's, so it is refused instead of being
// resolved.
func imports(t *testing.T, fset *token.FileSet, file *ast.File) (map[string]string, []string) {
	t.Helper()

	var found []string

	names := map[string]string{}

	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("%s: an import path that is not a string: %v", fset.Position(spec.Pos()), err)
		}

		name := path[strings.LastIndex(path, "/")+1:]
		if spec.Name != nil {
			name = spec.Name.Name
		}

		switch name {
		case "_":
			// Imported for its side effects; nothing is called through it.
		case ".":
			if watched(path) {
				found = append(found, fmt.Sprintf("%s: a dot import of %s", fset.Position(spec.Pos()), path))
			}
		default:
			names[name] = path
		}
	}

	return names, found
}

// watched reports whether any of the lists above names path.
func watched(path string) bool {
	for _, names := range []map[string][]string{
		readsAnAmbientDirectory, defaultsToAnAmbientDirectory, namesAnAmbientDirectory,
	} {
		if _, ok := names[path]; ok {
			return true
		}
	}

	return false
}

// named is what each finding calls, with the position dropped.
func named(found []string) []string {
	names := make([]string, 0, len(found))

	for _, finding := range found {
		_, what, _ := strings.Cut(finding, ": ")
		names = append(names, what)
	}

	return names
}
