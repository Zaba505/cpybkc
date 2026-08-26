// These tests are the whole reason this package exists apart from the pipeline
// that uses it. What is built on it is a guard, and a guard is the one kind of
// check whose failure mode is staying green — so every shape it accepts, and
// every shape it must refuse, is a row here rather than a sentence in a doc
// comment.
//
// The `re-pointed target` row is the one that was a real hole: the first version
// of the guard was `strings.Contains`, and `../..` is a prefix of `../../wrong`,
// so a replace silently moved to the wrong directory read as present. It was
// found by sabotaging a committed go.mod and watching the stage pass.
package gomod_test

import (
	"strings"
	"testing"

	"dagger/cpybkc/internal/gomod"
)

const spec = "github.com/Zaba505/cpybkc => ../.."

func TestHasReplacement(t *testing.T) {
	cases := []struct {
		name     string
		contents string
		want     bool
	}{
		{
			name:     "on its own line",
			contents: "module m\n\ngo 1.26.2\n\nreplace github.com/Zaba505/cpybkc => ../..\n",
			want:     true,
		},
		{
			name:     "inside a replace block",
			contents: "replace (\n\tgithub.com/Zaba505/cpybkc => ../..\n\tgithub.com/Zaba505/cpybkc/irpb => ../../irpb\n)\n",
			want:     true,
		},
		{
			name:     "whitespace between the tokens is not significant",
			contents: "replace\tgithub.com/Zaba505/cpybkc   =>    ../..\n",
			want:     true,
		},
		{
			name:     "a trailing comment is ignored",
			contents: "replace github.com/Zaba505/cpybkc => ../.. // until irpb carries a tag\n",
			want:     true,
		},
		{
			name:     "a leading comment is not a directive",
			contents: "// replace github.com/Zaba505/cpybkc => ../..\n",
			want:     false,
		},
		{
			name:     "absent",
			contents: "module m\n\ngo 1.26.2\n\nrequire github.com/Zaba505/cpybkc v0.0.0\n",
			want:     false,
		},
		{
			name:     "re-pointed target",
			contents: "replace github.com/Zaba505/cpybkc => ../../wrong\n",
			want:     false,
		},
		{
			name:     "target that the asked-for one is a prefix of, deeper",
			contents: "replace github.com/Zaba505/cpybkc => ../../../..\n",
			want:     false,
		},
		{
			name:     "a longer module path is a different module",
			contents: "replace github.com/Zaba505/cpybkc/irpb => ../..\n",
			want:     false,
		},
		{
			name:     "a versioned replacement is a different directive",
			contents: "replace github.com/Zaba505/cpybkc v0.0.0 => ../..\n",
			want:     false,
		},
		{
			name:     "replaced by a module rather than a directory",
			contents: "replace github.com/Zaba505/cpybkc => example.com/fork v1.2.3\n",
			want:     false,
		},
		{
			name:     "the module named without a replacement",
			contents: "require github.com/Zaba505/cpybkc v0.0.0 // indirect\n",
			want:     false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := gomod.HasReplacement(c.contents, spec); got != c.want {
				t.Errorf("HasReplacement(%q, %q) = %v, want %v", c.contents, spec, got, c.want)
			}
		})
	}
}

// TestAnEmptySpecMatchesNothing is the degenerate case, and it is here because
// the answer that would be convenient is the wrong one: a spec nobody supplied
// must not read as satisfied by the blank lines every go.mod has.
func TestAnEmptySpecMatchesNothing(t *testing.T) {
	if gomod.HasReplacement("module m\n\ngo 1.26.2\n", "") {
		t.Error("an empty spec was reported as present")
	}
}

// TestRelativeRoot pins the arithmetic a nested module's replace directive is
// written with. A level too few names a directory that exists and is not the
// root, which is the failure that would not announce itself.
func TestRelativeRoot(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		dir  string
		want string
	}{
		"the tree itself":     {dir: ".", want: "."},
		"empty":               {dir: "", want: "."},
		"one level":           {dir: "irpb", want: ".."},
		"two levels":          {dir: "example/parquet", want: "../.."},
		"three levels":        {dir: "example/ledger/parquet", want: "../../.."},
		"a trailing slash":    {dir: "example/ledger/parquet/", want: "../../.."},
		"an unclean path":     {dir: "./example//ledger/parquet", want: "../../.."},
		"surrounding spaces":  {dir: "  example/ledger/parquet  ", want: "../../.."},
		"a dot-dot in a path": {dir: "example/ledger/../parquet", want: "../.."},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := gomod.RelativeRoot(testCase.dir)
			if err != nil {
				t.Fatalf("RelativeRoot(%q): %v", testCase.dir, err)
			}

			if got != testCase.want {
				t.Errorf("RelativeRoot(%q) = %q, want %q", testCase.dir, got, testCase.want)
			}
		})
	}
}

// TestRelativeRootRefusesAPathThatIsNotInsideTheTree is the other half, and it
// is the half worth having: for each of these there is no number of `..` that
// reaches a root, and every one of them has a plausible-looking answer a
// silently-total function would hand back.
func TestRelativeRootRefusesAPathThatIsNotInsideTheTree(t *testing.T) {
	t.Parallel()

	for name, dir := range map[string]string{
		"the parent":            "..",
		"a sibling of the tree": "../parquet",
		"cleaning outside":      "example/../../parquet",
		"absolute":              "/example/ledger/parquet",
		"absolute at the root":  "/",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := gomod.RelativeRoot(dir)
			if err == nil {
				t.Fatalf("RelativeRoot(%q) = %q, want an error: a path outside the tree has no root to point back to", dir, got)
			}

			if !strings.Contains(err.Error(), dir) {
				t.Errorf("RelativeRoot(%q) refused without naming the path: %v", dir, err)
			}
		})
	}
}

// TestModuleDir pins the other half of the path arithmetic: what a `**/go.mod`
// glob result names.
//
// The `at the root` row is the one this exists for. A go.mod directly under the
// globbed directory comes back as a bare `go.mod`, and trimming the suffix off
// it leaves an empty string that composes into a trailing slash and then a
// doubled separator — not a failure, and not the path anybody meant.
func TestModuleDir(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		root  string
		match string
		want  string
	}{
		"nested twice":    {root: "example", match: "ledger/parquet/go.mod", want: "example/ledger/parquet"},
		"nested once":     {root: "example", match: "parquet/go.mod", want: "example/parquet"},
		"at the root":     {root: "example", match: "go.mod", want: "example"},
		"a leading slash": {root: "example", match: "/ledger/parquet/go.mod", want: "example/ledger/parquet"},
		"an unclean root": {root: "example/", match: "ledger/parquet/go.mod", want: "example/ledger/parquet"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := gomod.ModuleDir(testCase.root, testCase.match)
			if err != nil {
				t.Fatalf("ModuleDir(%q, %q): %v", testCase.root, testCase.match, err)
			}

			if got != testCase.want {
				t.Errorf("ModuleDir(%q, %q) = %q, want %q", testCase.root, testCase.match, got, testCase.want)
			}
		})
	}
}

// TestModuleDirRefusesAMatchThatIsNotAGoMod: the caller passes a glob result, so
// a match that is not a go.mod means the pattern and this function disagree
// about what was being looked for — which is a bug to report rather than a
// directory to take.
func TestModuleDirRefusesAMatchThatIsNotAGoMod(t *testing.T) {
	t.Parallel()

	for name, match := range map[string]string{
		"a directory":        "ledger/parquet",
		"another file":       "ledger/parquet/go.sum",
		"a prefix":           "ledger/parquet/go.mod.bak",
		"empty":              "",
		"a suffix elsewhere": "ledger/go.mod/inner.txt",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got, err := gomod.ModuleDir("example", match); err == nil {
				t.Errorf("ModuleDir(%q, %q) = %q, want an error", "example", match, got)
			}
		})
	}
}
