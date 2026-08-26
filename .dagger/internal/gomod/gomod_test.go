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

			if got := gomod.RelativeRoot(testCase.dir); got != testCase.want {
				t.Errorf("RelativeRoot(%q) = %q, want %q", testCase.dir, got, testCase.want)
			}
		})
	}
}
