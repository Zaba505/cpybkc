// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package generate

import (
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/plugin"
)

// Where a run puts the two directories it makes is #184's subject, and this
// file is what holds it: the run's scratch space, and the per-invocation
// directory the descriptor is written into. Both are inside the project the run
// was pointed at, neither is anywhere the operating system named, and neither
// is anywhere the merge looks.

// reports is a generator that writes down what it was handed and whether its
// output directory was empty when it arrived, under names of its own so that
// two of them can land in one directory.
//
// docs/plugin/SPEC.md forbids a plugin to derive anything from the descriptor's
// path, and this one does not: it copies the path out so that a *test* can say
// where cpybkc put it, which is a claim about cpybkc rather than a plugin
// hanging meaning on its surroundings.
//
// `$2` is the descriptor and `$4` the output directory, because the vector is
// `--descriptor <path> --out <dir>`.
func reports(name string) string {
	return `[ -z "$(ls -A "$4")" ] && echo empty > "$4/` + name + `.state" || echo dirty > "$4/` + name + `.state"
echo "$2" > "$4/` + name + `.descriptor"
echo "$4" > "$4/` + name + `.handed"
echo "package pkg // ` + name + `" > "$4/` + name + `.go"`
}

// quiet is a runner that says nothing, pointed at one project for everything it
// decides: where its record goes, and where its scratch space is made.
func quiet(t *testing.T, project string) *Runner {
	t.Helper()

	return &Runner{
		Plugins: &plugin.Runner{Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		Root:    project,
		Scratch: project,
	}
}

func TestTheDescriptorDirectoryIsOutsideEveryWalkTheMergeMakes(t *testing.T) {
	t.Parallel()

	// One project, two generators landing in one directory of it, which is the
	// arrangement a project generating a package from two of them has.
	project := t.TempDir()
	out := filepath.Join(project, "pkg")

	names := []string{"one", "two"}

	generators := make([]Generator, 0, len(names))
	for _, name := range names {
		generators = append(generators, generator(t, name, reports(name), out))
	}

	if err := run(t, quiet(t, project), generators...); err != nil {
		t.Fatalf("running the generators: %v", err)
	}

	// What each generator reported, read back out of the project. The merge is
	// what put these there, so this is what a generator saw rather than what
	// the test assumed it would see.
	descriptors := make([]string, 0, len(names))
	handed := make([]string, 0, len(names))

	for _, name := range names {
		descriptors = append(descriptors, reported(t, out, name+".descriptor"))
		handed = append(handed, reported(t, out, name+".handed"))

		// docs/plugin/SPEC.md: a plugin MAY assume the directory it is handed
		// is empty. Asserted here as well as where it has its own check,
		// because a descriptor directory landing beside that directory is what
		// this test is about and this is exactly what would break.
		if got := reported(t, out, name+".state"); got != "empty" {
			t.Errorf("the generator %s found its output directory %s, want it empty", name, got)
		}
	}

	if handed[0] == handed[1] {
		t.Errorf("both generators were handed %s, want a directory each", handed[0])
	}

	if descriptors[0] == descriptors[1] {
		t.Errorf("both generators were handed the descriptor at %s; docs/plugin/SPEC.md gives each invocation "+
			"a directory of its own", descriptors[0])
	}

	for i, descriptor := range descriptors {
		dir := filepath.Dir(descriptor)

		// The property the merge rests on. produced() walks the directory each
		// generator was handed and everything beneath one is output, so a
		// descriptor directory inside one would be merged into the project as
		// though a generator had written it.
		for _, handedTo := range handed {
			if within(handedTo, descriptor) {
				t.Errorf("the descriptor for %s is at %s, inside the output directory %s, which the merge walks",
					names[i], descriptor, handedTo)
			}
		}

		// And where it is instead: a sibling of the directory that generator
		// was handed, so both are one level inside the run's scratch space, and
		// nothing walks that level. The property above therefore holds by the
		// layout rather than by the directory being hidden or named carefully.
		if got, want := filepath.Dir(dir), filepath.Dir(handed[i]); got != want {
			t.Errorf("the descriptor directory for %s is in %s and the directory it was handed is in %s; they "+
				"are meant to be siblings inside the run's scratch space", names[i], got, want)
		}

		// Inside the project, which is the other half of #184: no part of a run
		// reaches a directory the operating system named.
		if !within(project, dir) {
			t.Errorf("the descriptor directory for %s is at %s, outside the project at %s", names[i], dir, project)
		}

		// docs/plugin/SPEC.md, "The descriptor's location and lifetime": the
		// file is removed with its directory once the generator has exited.
		if exists(t, dir) {
			t.Errorf("the descriptor directory for %s is still at %s after the run", names[i], dir)
		}
	}

	// And the project afterwards holds what the two generators produced and the
	// record of it, and nothing else — no scratch space, no descriptor
	// directory, and nothing left a level above the output.
	same(t, project, map[string]string{
		"pkg": "<dir>",

		filepath.Join("pkg", "one.state"):      "empty\n",
		filepath.Join("pkg", "one.descriptor"): descriptors[0] + "\n",
		filepath.Join("pkg", "one.handed"):     handed[0] + "\n",
		filepath.Join("pkg", "one.go"):         "package pkg // one\n",

		filepath.Join("pkg", "two.state"):      "empty\n",
		filepath.Join("pkg", "two.descriptor"): descriptors[1] + "\n",
		filepath.Join("pkg", "two.handed"):     handed[1] + "\n",
		filepath.Join("pkg", "two.go"):         "package pkg // two\n",

		RecordName: `{
  "version": 1,
  "files": [
    "pkg/one.descriptor",
    "pkg/one.go",
    "pkg/one.handed",
    "pkg/one.state",
    "pkg/two.descriptor",
    "pkg/two.go",
    "pkg/two.handed",
    "pkg/two.state"
  ]
}
`,
	})
}

func TestARunNeedsNoTemporaryDirectoryOfTheSystems(t *testing.T) {
	// Not parallel, because it states TMPDIR in this process's own
	// environment. That is the only way to check what *cpybkc* would do with
	// it: the determinism check varies TMPDIR in the environment a generator is
	// started with, which says nothing about os.MkdirTemp's default here.
	//
	// The project is taken before TMPDIR is moved, because testing makes its
	// base directory on the first t.TempDir call and that call is the one that
	// reads TMPDIR.
	project := t.TempDir()

	// A path with nothing at it. Every way of reaching an ambient temporary
	// directory ends at os.MkdirTemp or os.CreateTemp with no parent, and both
	// fail outright on a parent that is not there — so a read put back anywhere
	// in a run fails this test rather than passing quietly because two runs
	// agreed about a directory neither was looking at.
	t.Setenv("TMPDIR", filepath.Join(project, "no-such-temporary-directory"))

	out := filepath.Join(project, "pkg")

	if err := run(t, quiet(t, project),
		generator(t, "go", `echo "package pkg" > "$4/orders.go"`, out)); err != nil {
		t.Fatalf("a run whose TMPDIR names nothing failed with %v; since #184 no part of a run reads it", err)
	}

	if got, want := contents(t, filepath.Join(out, "orders.go")), "package pkg\n"; got != want {
		t.Errorf("the run produced %q, want %q", got, want)
	}
}

func TestARunWithNowhereToMakeItsScratchSpaceIsRefusedBeforeAnythingRuns(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	marker := filepath.Join(project, "ran")

	// The zero value of the field #184 made required. A Runner that has not
	// been told where to make its scratch space fails rather than falling back
	// to somewhere ambient, and it fails before a generator has run.
	r := &Runner{Plugins: &plugin.Runner{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}

	err := run(t, r, generator(t, "go", `echo ran > `+marker, filepath.Join(project, "pkg")))
	if err == nil {
		t.Fatal("a run with no Scratch was accepted, want it refused")
	}

	if !strings.Contains(err.Error(), "Scratch") {
		t.Errorf("the run failed with %v, which does not name the field that was not set", err)
	}

	if exists(t, marker) {
		t.Error("the generator ran, want the run refused before anything started")
	}
}

// within reports whether path is dir or anything beneath it.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)

	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

// reported is the one line a generator wrote into name, with the newline taken
// off: a path it was handed, or what it found where it was handed it.
func reported(t *testing.T, out, name string) string {
	t.Helper()

	return strings.TrimSuffix(contents(t, filepath.Join(out, name)), "\n")
}
