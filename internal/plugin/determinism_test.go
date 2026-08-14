// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package plugin

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// docs/plugin/SPEC.md, "Determinism": two invocations of one generator, given a
// byte-identical descriptor and an identical option list, produce byte-identical
// output — whatever the wall-clock time, the hostname, the user, the working
// directory, the locale and the environment beyond SOURCE_DATE_EPOCH.
//
// Two halves of that are checked here, and neither sees the whole thing.
// Repetition is the other half and lives where output is produced
// (internal/generate's determinism_test.go): it catches an order decided by a
// map iteration, because Go randomises that on every range, and it cannot catch
// a clock read, since two runs a moment apart agree on the date. What is here is
// the propagation the requirement rests on, and a scan of the source for the
// calls repetition cannot catch.

// sourceDateEpochVar is the one variable a generator may take a time from.
//
// docs/plugin/SPEC.md makes it the single permitted channel rather than one
// convention among several, and cpybkc MUST propagate it when it is set in
// cpybkc's own environment (#47).
const sourceDateEpochVar = "SOURCE_DATE_EPOCH"

func TestSourceDateEpochReachesAGeneratorFromCpybkcsOwnEnvironment(t *testing.T) {
	// Not parallel, because it states a variable in this process's own
	// environment, which is the only way to assert the propagation.
	// TestTheEnvironmentReachesTheGeneratorUnchanged asserts the other side of
	// it — that a stated [Runner.Env] arrives unchanged — and a test that
	// stated one here would be asserting that cpybkc passes on what a caller
	// handed it rather than that it passes on what it was started with.
	t.Setenv(sourceDateEpochVar, "1700000000")

	out := t.TempDir()
	invocation := generator(t, "go", `echo "$SOURCE_DATE_EPOCH" > "$4/epoch"`, out)

	log, _ := recorder()

	// A nil Env: this process's environment, which is what a run made by
	// anything but a test has.
	r := &Runner{Log: log}

	if err := r.Run(t.Context(), descriptor(), []Invocation{invocation}); err != nil {
		t.Fatalf("running the generator: %v", err)
	}

	if got, want := lines(t, filepath.Join(out, "epoch"))[0], "1700000000"; got != want {
		t.Errorf("the generator saw %s=%q, want %q", sourceDateEpochVar, got, want)
	}
}

// decidesOutput is every package whose source is held to the determinism rule:
// the ones that decide what a run writes, from the descriptor a generator is
// handed to the bytes that land in a project's tree.
//
// docs/plugin/SPEC.md holds "the generators in this repository" to the rule, and
// cpybkc-gen-go is the first of them (#48); what it generates from the
// descriptor is #49–#53, and it is on this list from the commit that scaffolded
// it rather than from the one that gave it something to emit. The internal
// packages beside it are the rest of the path a run's bytes take, from the
// descriptor a generator is handed to what lands in a project's tree.
//
// TestEveryGeneratorThisRepositoryShipsIsOnThatList is what adds the next
// generator here, rather than somebody remembering to. cpybkc-gen-graph (#186)
// is the second, and arrived the same way the first did — in the commit that
// scaffolded it, while it still drew nothing.
//
// A list rather than a scan of everything, because a command that resolves a
// plugin against PATH has to read the environment to do it, and a check that
// had to exempt cpybkc's own command is a check nobody would trust.
var decidesOutput = []string{
	"cmd/cpybkc-gen-go",
	"cmd/cpybkc-gen-graph",
	"internal/assemble",
	"internal/emit",
	"internal/generate",
	"internal/plugin",
}

// forbidden is what a package on that list may not call, by import path and
// name. A nil name list is the whole package.
//
// Each entry is one of the inputs docs/plugin/SPEC.md says output must not vary
// with, reached through the call that reads it: the clock, the environment, the
// working directory, the host, the user, and a random value. A duration is not a
// clock read, which is why the entry for time names the three functions that ask
// what time it is and not the package.
var forbidden = map[string][]string{
	"crypto/rand":  nil,
	"math/rand":    nil,
	"math/rand/v2": nil,
	"os":           {"Environ", "Getenv", "Getwd", "Hostname", "LookupEnv"},
	"os/user":      nil,
	"time":         {"Now", "Since", "Until"},
}

func TestNothingThatDecidesOutputReadsTheClockOrItsSurroundings(t *testing.T) {
	t.Parallel()

	for _, pkg := range decidesOutput {
		t.Run(pkg, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join("..", "..", filepath.FromSlash(pkg))

			if _, err := os.Stat(dir); err != nil {
				t.Fatalf("%s is on the list of packages that decide output and is not there: %v", pkg, err)
			}

			for _, finding := range scan(t, dir) {
				t.Errorf("%s, and output may not vary with what that reads", finding)
			}
		})
	}
}

func TestEveryGeneratorThisRepositoryShipsIsOnThatList(t *testing.T) {
	t.Parallel()

	// The rule docs/plugin/SPEC.md states is about the generators in this
	// repository, and the list above is a list somebody has to remember to add
	// to. This is what remembers: a command whose name makes it a generator —
	// the cpybkc-gen-<name> convention [Prefix] spells — is held to the
	// determinism rule from the commit that adds it, rather than from the
	// commit where somebody noticed.
	commands := filepath.Join("..", "..", "cmd")

	entries, err := os.ReadDir(commands)
	if err != nil {
		// cmd/ holds cpybkc-gen-go (#48) and will hold whatever ships beside
		// it, but a missing directory is nothing to report — the check is
		// about generators that exist. Anything else about it is.
		if !os.IsNotExist(err) {
			t.Fatalf("reading %s: %v", commands, err)
		}

		return
	}

	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), Prefix) {
			continue
		}

		pkg := "cmd/" + entry.Name()
		if !slices.Contains(decidesOutput, pkg) {
			t.Errorf("%s is a generator this repository ships and is not held to the determinism rule; add it to decidesOutput", pkg)
		}
	}
}

// nondeterministic is source the scan has to see through: the calls are made
// under an alias, and one of them is in a package whose name is not the last
// element of its path.
const nondeterministic = `package gen

import (
	"os"
	clock "time"
	"math/rand/v2"
)

func Header() string {
	return clock.Now().String() + os.Getenv("USER") + string(rune(rand.Int()))
}
`

// deterministic is source the scan has to leave alone: a duration, an
// environment variable named in a string, and a call whose selector matches a
// forbidden one on a receiver that is not a package.
const deterministic = `package gen

import (
	"time"
)

type ticker struct{}

func (ticker) Now() string { return "" }

const grace = 5 * time.Second

const epoch = "SOURCE_DATE_EPOCH"

func Header(t ticker) string { return t.Now() + epoch + grace.String() }
`

func TestTheScanSeesACallAnAliasWouldHide(t *testing.T) {
	t.Parallel()

	got := scan(t, fixture(t, nondeterministic))

	// The positions are the fixture's, so the findings are compared by what
	// they name rather than by where they were: a line number in the assertion
	// would make editing the fixture a two-place change.
	want := []string{"clock.Now", "os.Getenv", "rand.Int"}

	if named := slices.Sorted(slices.Values(names(got))); !slices.Equal(named, want) {
		t.Errorf("the scan found %v, want %v", named, want)
	}
}

func TestTheScanAcceptsSourceThatOnlyLooksLikeItReadsTheClock(t *testing.T) {
	t.Parallel()

	if found := scan(t, fixture(t, deterministic)); len(found) != 0 {
		t.Errorf("the scan found %v in source that reads nothing", found)
	}
}

// fixture is a directory holding src as one Go file, and a test file the scan
// has to skip — a test may read the clock, and one that could not would not be
// able to assert that nothing else does.
func fixture(t *testing.T, src string) string {
	t.Helper()

	dir := t.TempDir()

	for name, content := range map[string]string{
		"gen.go":      src,
		"gen_test.go": nondeterministic,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	return dir
}

// scan reports every call in dir's non-test Go files that a package deciding
// output may not make, as `file:line:col: pkg.Name`.
//
// It is a parse rather than a grep because the name a package is imported under
// is not the name in its path: an alias, or a major-version suffix, would carry
// a forbidden call past a search for the text of one. The import block is what
// says which is which, so it is read first and the selectors are resolved
// against it.
func scan(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	fset := token.NewFileSet()

	var found []string

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		path := filepath.Join(dir, name)

		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}

		imported, dotted := imports(t, fset, file)
		found = append(found, dotted...)

		ast.Inspect(file, func(n ast.Node) bool {
			selector, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			// A qualified identifier is a package name and nothing else, so a
			// method call on a value named for a package — a receiver called
			// `rand`, a field called `os` — is not one. Shadowing is not
			// resolved: the cost of missing a call made through a variable that
			// took a package's name is a finding this scan does not report, and
			// the cost of resolving it is a type-checker in a test.
			ident, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}

			pkg, ok := imported[ident.Name]
			if !ok {
				return true
			}

			names, watched := forbidden[pkg]
			if !watched || (names != nil && !slices.Contains(names, selector.Sel.Name)) {
				return true
			}

			found = append(found, fmt.Sprintf("%s: %s.%s",
				fset.Position(selector.Pos()), ident.Name, selector.Sel.Name))

			return true
		})
	}

	slices.Sort(found)

	return found
}

// imports is what each of file's imports is called where it is used, against the
// path it names, and the findings the import block is itself.
//
// A dot import of a watched package is a finding rather than an entry: it puts
// the package's names in the file's own scope, where nothing distinguishes
// `Now()` from a function of the file's, so it is refused instead of being
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

		name := packageName(path)
		if spec.Name != nil {
			name = spec.Name.Name
		}

		switch name {
		case "_":
			// Imported for its side effects; nothing is called through it.
		case ".":
			if _, watched := forbidden[path]; watched {
				found = append(found, fmt.Sprintf("%s: a dot import of %s",
					fset.Position(spec.Pos()), path))
			}
		default:
			names[name] = path
		}
	}

	return names, found
}

// packageName is what a path is called where it is imported without an alias:
// the last element of the path, or the one before it where that is a
// major-version suffix — math/rand/v2 is imported as rand.
func packageName(path string) string {
	parts := strings.Split(path, "/")

	last := parts[len(parts)-1]
	if len(parts) == 1 || len(last) < 2 || last[0] != 'v' {
		return last
	}

	if _, err := strconv.Atoi(last[1:]); err != nil {
		return last
	}

	return parts[len(parts)-2]
}

// names is what each finding calls, with the position dropped.
func names(found []string) []string {
	called := make([]string, 0, len(found))

	for _, finding := range found {
		_, call, _ := strings.Cut(finding, ": ")
		called = append(called, call)
	}

	return called
}
