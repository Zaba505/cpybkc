// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package generate

import (
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Zaba505/cpybkc/internal/plugin"
	"github.com/Zaba505/cpybkc/irpb"
)

// The generators below are shell scripts, for the reason the plugin package's
// tests give: docs/plugin/SPEC.md makes one a first-class plugin, and a test
// whose fixture was a compiled binary would be testing a toolchain rather than
// a contract. They are real processes writing to a real filesystem, because
// what is under test here is what is on disk after a run — which is the one
// thing a fake could agree with this package about and be wrong.
//
// A generator's argument vector is `--descriptor <path> --out <dir>`, so `$4`
// is the directory it was handed. Every script below reads it there rather than
// from the environment, which is also the assertion that the directory a plugin
// is handed is the scratch directory and never the project's tree.

// generator writes an executable plugin named for name, running body, and hands
// back the [Generator] that lands what it writes in out.
func generator(t *testing.T, name, body, out string) Generator {
	t.Helper()

	path := filepath.Join(t.TempDir(), plugin.Filename(name))

	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	return Generator{Name: name, Path: path, Out: out}
}

// descriptor is the message every run below hands over: a whole descriptor
// rather than a version field, so that what a generator is handed is what a
// resolved layout would produce.
func descriptor() *irpb.Descriptor {
	return &irpb.Descriptor{
		Version: irpb.IrVersion_IR_VERSION_1,
		Nodes: []*irpb.Node{
			{
				Id: 1,
				Kind: &irpb.Node_File{File: &irpb.File{
					Framing:      &irpb.File_Unframed{Unframed: &irpb.Unframed{}},
					StartStateId: 2,
				}},
			},
			{
				Id:   2,
				Kind: &irpb.Node_State{State: &irpb.State{Accepts: true}},
			},
		},
	}
}

// runner is a runner whose scratch space is the test's own, so that a test can
// assert against what is left in it, and whose generators' output goes
// somewhere a test need not read.
func runner(t *testing.T) *Runner {
	t.Helper()

	return &Runner{
		Plugins: &plugin.Runner{
			Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
			TempDir: t.TempDir(),
		},
		TempDir: t.TempDir(),
	}
}

// run runs generators through r against [descriptor].
func run(t *testing.T, r *Runner, generators ...Generator) error {
	t.Helper()

	return r.Run(t.Context(), descriptor(), generators)
}

// tree is every path beneath root, relative and in sorted order, with what is
// at it: a file's contents, or what kind of thing it is where it is not a file.
//
// A whole tree at once rather than one assertion per path, because most of what
// these tests claim is about what is *not* there as much as what is.
func tree(t *testing.T, root string) map[string]string {
	t.Helper()

	found := map[string]string{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == root {
			return err
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		switch {
		case d.IsDir():
			found[rel] = "<dir>"
		case !d.Type().IsRegular():
			found[rel] = "<" + kind(d.Type()) + ">"
		default:
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			found[rel] = string(b)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("reading %s: %v", root, err)
	}

	return found
}

// same reports whether a tree is the one wanted, and says how it differs when
// it is not.
func same(t *testing.T, root string, want map[string]string) {
	t.Helper()

	got := tree(t, root)

	if maps.Equal(got, want) {
		return
	}

	for _, path := range slices.Sorted(maps.Keys(want)) {
		if got[path] != want[path] {
			t.Errorf("%s holds %q, want %q", path, got[path], want[path])
		}
	}

	for _, path := range slices.Sorted(maps.Keys(got)) {
		if _, wanted := want[path]; !wanted {
			t.Errorf("%s holds %q, and nothing was to be there", path, got[path])
		}
	}
}

// exists reports whether there is anything at path at all.
func exists(t *testing.T, path string) bool {
	t.Helper()

	_, err := os.Lstat(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("reading %s: %v", path, err)
	}

	return err == nil
}

// contents is what is in the file at path.
func contents(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	return string(b)
}

// empty reports whether a directory has nothing in it.
func empty(t *testing.T, dir string) bool {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	return len(entries) == 0
}

// records is a generator that says whether the directory it was handed was
// empty, and which directory it was.
const records = `[ -z "$(ls -A "$4")" ] && echo empty > "$4/state" || echo dirty > "$4/state"
echo "$4" > "$4/where"`

func TestEachGeneratorIsHandedAPrivateEmptyDirectoryOfItsOwn(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	first := filepath.Join(project, "one")
	second := filepath.Join(project, "two")

	if err := run(t, runner(t),
		generator(t, "one", records, first),
		generator(t, "two", records, second),
	); err != nil {
		t.Fatalf("running the generators: %v", err)
	}

	// docs/plugin/SPEC.md: a plugin MAY assume the directory exists, is
	// writable, and is empty. The last is what makes the files an invocation
	// produced mechanically equal to the files in the directory afterwards,
	// which #44 and #45 both rest on.
	for _, out := range []string{first, second} {
		if got := contents(t, filepath.Join(out, "state")); got != "empty\n" {
			t.Errorf("the generator landing in %s found its directory %s, want it empty", out, got)
		}
	}

	handed := map[string]string{
		first:  contents(t, filepath.Join(first, "where")),
		second: contents(t, filepath.Join(second, "where")),
	}

	// Private, and never the project's tree: a generator writing where its
	// output lands is the arrangement this package exists to replace.
	if handed[first] == handed[second] {
		t.Errorf("both generators were handed %s, want a directory each", handed[first])
	}

	for out, dir := range handed {
		if dir == out+"\n" {
			t.Errorf("the generator was handed %s, which is where its output lands", out)
		}
	}
}

func TestNothingReachesTheProjectTreeUntilEveryGeneratorHasSucceeded(t *testing.T) {
	t.Parallel()

	out := filepath.Join(t.TempDir(), "project")

	// docs/plugin/SPEC.md: one generator's failure fails the whole run, and
	// nothing is merged until every generator has succeeded — so the one that
	// exited zero has produced nothing either.
	err := run(t, runner(t),
		generator(t, "good", `echo A > "$4/orders.go"`, out),
		generator(t, "bad", `echo B > "$4/broken.go"
echo "error: OD-QTY: COMP-3 is not supported" >&2
exit 1`, out),
	)

	var exit *plugin.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("the run failed with %v, want a plugin.ExitError", err)
	}

	if exists(t, out) {
		t.Errorf("%s was created by a failed run, want the tree left as it was found: %v", out, tree(t, out))
	}
}

func TestAGeneratorThatFailsAfterWritingFilesLeavesTheTreeAsItFoundIt(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	// Something already in the project's tree, so that "untouched" is a claim
	// about what is there and not only about what is not.
	if err := os.WriteFile(filepath.Join(out, "orders.go"), []byte("package orders\n"), 0o644); err != nil {
		t.Fatalf("writing the file that was already there: %v", err)
	}

	err := run(t, runner(t), generator(t, "go", `echo half > "$4/orders.go"
mkdir "$4/pkg"
echo half > "$4/pkg/order.go"
exit 2`, out))

	var exit *plugin.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("the run failed with %v, want a plugin.ExitError", err)
	}

	if got, want := exit.Code, 2; got != want {
		t.Errorf("the generator was reported as exiting %d, want %d", got, want)
	}

	same(t, out, map[string]string{"orders.go": "package orders\n"})
}

func TestASuccessfulRunMergesEveryGeneratorsOutput(t *testing.T) {
	t.Parallel()

	out := filepath.Join(t.TempDir(), "project", "gen")

	if err := run(t, runner(t),
		generator(t, "go", `mkdir -p "$4/pkg/orders"
echo A > "$4/pkg/orders/order.go"
echo B > "$4/orders.go"
mkdir "$4/nothing"`, out),
		generator(t, "docs", `echo C > "$4/orders.md"`, out),
	); err != nil {
		t.Fatalf("running the generators: %v", err)
	}

	// The output directory and every directory above it are made by the merge:
	// a project that has never been generated into has none of them, and a run
	// that asked the user to make one first would be a step nobody wrote down.
	same(t, out, map[string]string{
		"pkg":                 "<dir>",
		"pkg/orders":          "<dir>",
		"pkg/orders/order.go": "A\n",
		"orders.go":           "B\n",
		"orders.md":           "C\n",
		"nothing":             "<dir>",
	})
}

func TestTheScratchSpaceDoesNotOutliveTheRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		about string
		body  string
	}{
		{about: "a run that succeeded", body: `echo A > "$4/orders.go"`},
		{about: "a run that failed", body: `echo A > "$4/orders.go"; exit 1`},
		{about: "a run refused at the merge", body: `ln -s /etc/passwd "$4/link"`},
	}

	for _, test := range tests {
		t.Run(test.about, func(t *testing.T) {
			t.Parallel()

			r := runner(t)
			out := filepath.Join(t.TempDir(), "project")

			_ = run(t, r, generator(t, "go", test.body, out))

			if !empty(t, r.TempDir) {
				t.Errorf("%s left %v behind", test.about, tree(t, r.TempDir))
			}
		})
	}
}

func TestWhatAGeneratorWritesOutsideItsDirectoryIsNotOutput(t *testing.T) {
	t.Parallel()

	r := runner(t)
	out := filepath.Join(t.TempDir(), "project")

	// docs/plugin/SPEC.md forbids a plugin to write outside the directory it
	// was handed, through `..` or through an absolute path. Neither is blocked
	// — a plugin is a process and writes where its user can — and neither needs
	// to be: only what is beneath the directory is ever read, so what it wrote
	// elsewhere is not output, and the run produces what the contract says it
	// produces.
	escaped := filepath.Join(t.TempDir(), "escaped.go")

	if err := run(t, r, generator(t, "go", `echo out > "$4/../escaped.go"
echo out > `+escaped+`
echo kept > "$4/orders.go"`, out)); err != nil {
		t.Fatalf("running the generator: %v", err)
	}

	same(t, out, map[string]string{"orders.go": "kept\n"})

	// The one it wrote into the scratch space went with the scratch space, and
	// the one it wrote where it liked stayed where it wrote it — outside the
	// project's tree, which is the claim.
	if !empty(t, r.TempDir) {
		t.Errorf("the scratch space still holds %v", tree(t, r.TempDir))
	}

	if !exists(t, escaped) {
		t.Errorf("%s is gone; this package does not tidy up after a plugin, it declines to merge it", escaped)
	}
}

func TestARunWithNoGeneratorsIsNotAFailure(t *testing.T) {
	t.Parallel()

	if err := run(t, runner(t)); err != nil {
		t.Errorf("a run with no generators failed with %v", err)
	}
}

func TestAGeneratorWithNowhereForItsOutputIsRefusedBeforeAnythingRuns(t *testing.T) {
	t.Parallel()

	r := runner(t)

	marker := filepath.Join(t.TempDir(), "ran")

	generator := generator(t, "go", `echo ran > `+marker, "")

	if err := run(t, r, generator); err == nil {
		t.Fatal("a generator with no output directory was accepted, want it refused")
	}

	if exists(t, marker) {
		t.Error("the generator ran, want the run refused before anything started")
	}

	if !empty(t, r.TempDir) {
		t.Errorf("the refused run left %v behind", tree(t, r.TempDir))
	}
}
