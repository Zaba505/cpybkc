// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package generate

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The runs below are real: real generators writing real files, run twice
// against a real project directory, because what a stale-file test claims is
// what is on disk after the second run — and every part of that a fake could
// agree with this package about, it could agree with wrongly.

// project is a runner keeping its record in a project directory of the test's
// own, with what it says about pruning collected rather than printed.
func project(t *testing.T) (*Runner, string, *lines) {
	t.Helper()

	root := t.TempDir()

	said := &lines{}

	r := runner(t)
	r.Root = root
	r.Log = slog.New(slog.NewTextHandler(said, nil))

	return r, root, said
}

// lines is what a run said, kept so that a test can ask whether pruning was
// reported.
type lines struct {
	strings.Builder
}

// said reports whether anything the run wrote mentions every one of want.
func (l *lines) said(want ...string) bool {
	for _, phrase := range want {
		if !strings.Contains(l.String(), phrase) {
			return false
		}
	}

	return true
}

// record is what the project's record holds, as bytes.
func recorded(t *testing.T, root string) string {
	t.Helper()

	return contents(t, filepath.Join(root, RecordName))
}

// TestARecordRemovedFromALayoutTakesItsFileWithIt is the story: a layout
// declares two records, a generator produces a file for each, the layout loses
// one, and the file it produced goes with it. The generator here is the layout
// — what it emits is what a resolved one asked for — because the two runs have
// to differ in what they generate and in nothing else.
func TestARecordRemovedFromALayoutTakesItsFileWithIt(t *testing.T) {
	t.Parallel()

	r, root, said := project(t)
	out := filepath.Join(root, "gen")

	both := `echo A > "$4/order.go"
echo B > "$4/customer.go"`

	if err := run(t, r, generator(t, "go", both, out)); err != nil {
		t.Fatalf("the first run failed: %v", err)
	}

	same(t, out, map[string]string{"order.go": "A\n", "customer.go": "B\n"})

	// The layout loses CUSTOMER-REC, so the generator stops producing the file
	// it produced for it.
	one := `echo A > "$4/order.go"`

	if err := run(t, r, generator(t, "go", one, out)); err != nil {
		t.Fatalf("the second run failed: %v", err)
	}

	same(t, out, map[string]string{"order.go": "A\n"})

	if !said.said("gen/customer.go") {
		t.Errorf("the run said\n%s\nwant it to report removing gen/customer.go", said)
	}
}

// TestTheRecordHoldsEveryPathTheRunGenerated pins the file itself. It is
// committed, so it is read in diffs, and two runs producing one set of files
// have to produce one set of bytes or every regeneration is a change.
func TestTheRecordHoldsEveryPathTheRunGenerated(t *testing.T) {
	t.Parallel()

	r, root, _ := project(t)

	body := `mkdir -p "$4/pkg/orders"
echo A > "$4/pkg/orders/order.go"
echo B > "$4/orders.go"
mkdir "$4/nothing"`

	if err := run(t, r,
		generator(t, "go", body, filepath.Join(root, "gen")),
		generator(t, "docs", `echo C > "$4/orders.md"`, filepath.Join(root, "docs")),
	); err != nil {
		t.Fatalf("running the generators: %v", err)
	}

	// Sorted, slash-separated and relative to the project's root, across every
	// generator in the run rather than one record per generator: the record
	// outlives the manifest entry that produced any of it, which is what lets a
	// generator removed from the manifest have its output pruned.
	//
	// Directories are not in it. `gen/nothing` was produced and is not
	// recorded, because what the record holds is what a later run may remove,
	// and a directory goes when pruning empties it.
	want := `{
  "version": 1,
  "files": [
    "docs/orders.md",
    "gen/orders.go",
    "gen/pkg/orders/order.go"
  ]
}
`

	if got := recorded(t, root); got != want {
		t.Errorf("the record holds\n%s\nwant\n%s", got, want)
	}

	// Byte-identical for the same set of files, so that a record showing up in
	// a diff is a run that generated something different.
	if err := run(t, r,
		generator(t, "go", body, filepath.Join(root, "gen")),
		generator(t, "docs", `echo C > "$4/orders.md"`, filepath.Join(root, "docs")),
	); err != nil {
		t.Fatalf("running the generators again: %v", err)
	}

	if got := recorded(t, root); got != want {
		t.Errorf("a second run of the same generators recorded\n%s\nwant\n%s", got, want)
	}
}

// TestAFileNoRunRecordedIsNeverTouched is the guarantee the whole arrangement
// exists for. An output directory shared with hand-written source is the case
// that matters — it is what lets a generator be pointed at a package a person
// also works in.
func TestAFileNoRunRecordedIsNeverTouched(t *testing.T) {
	t.Parallel()

	r, root, _ := project(t)
	out := filepath.Join(root, "orders")

	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatalf("making the package directory: %v", err)
	}

	// Written by a person, in the directory the generator lands in, before
	// cpybkc has ever run over the project.
	handwritten := filepath.Join(out, "service.go")
	if err := os.WriteFile(handwritten, []byte("package orders\n"), 0o644); err != nil {
		t.Fatalf("writing the hand-written file: %v", err)
	}

	if err := run(t, r, generator(t, "go", `echo A > "$4/order.go"`, out)); err != nil {
		t.Fatalf("the first run failed: %v", err)
	}

	if err := run(t, r, generator(t, "go", `echo A > "$4/other.go"`, out)); err != nil {
		t.Fatalf("the second run failed: %v", err)
	}

	// order.go went, because the first run recorded it and the second did not
	// produce it. service.go stayed, because no run ever recorded it.
	same(t, out, map[string]string{"other.go": "A\n", "service.go": "package orders\n"})
}

// TestARunThatFindsNoRecordPrunesNothing is the missing-record rule, and it is
// the one that decides what pruning is allowed to infer: nothing. A first run
// over a tree that already holds output — a project generated by an older
// cpybkc, a record somebody deleted — leaves every bit of it alone.
func TestARunThatFindsNoRecordPrunesNothing(t *testing.T) {
	t.Parallel()

	r, root, said := project(t)
	out := filepath.Join(root, "gen")

	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatalf("making the output directory: %v", err)
	}

	// Output that looks exactly like this generator's, which is the whole
	// temptation: it is named the way generated files are named, and pruning
	// still may not touch it.
	stale := filepath.Join(out, "customer.go")
	if err := os.WriteFile(stale, []byte("// Code generated by cpybkc.\n"), 0o644); err != nil {
		t.Fatalf("writing the file the previous cpybkc left: %v", err)
	}

	if err := run(t, r, generator(t, "go", `echo A > "$4/order.go"`, out)); err != nil {
		t.Fatalf("the run failed: %v", err)
	}

	same(t, out, map[string]string{
		"order.go":    "A\n",
		"customer.go": "// Code generated by cpybkc.\n",
	})

	if said.said("customer.go") {
		t.Errorf("the run said\n%s\nwant it to say nothing about a file no record names", said)
	}

	// The record it writes covers what *this* run generated and nothing else,
	// so the file it left alone stays left alone on every run after this one
	// too.
	if strings.Contains(recorded(t, root), "customer.go") {
		t.Errorf("the record holds\n%s\nwant a file no run generated left out of it", recorded(t, root))
	}
}

// TestARunnerWithNoRootKeepsNoRecord is the zero value's answer. A run that has
// not been told where the project is cannot resolve a record's paths and must
// not guess at them, so it keeps none and prunes nothing.
func TestARunnerWithNoRootKeepsNoRecord(t *testing.T) {
	t.Parallel()

	r := runner(t)
	out := t.TempDir()

	if err := run(t, r, generator(t, "go", `echo A > "$4/order.go"`, out)); err != nil {
		t.Fatalf("the first run failed: %v", err)
	}

	if err := run(t, r, generator(t, "go", `echo B > "$4/customer.go"`, out)); err != nil {
		t.Fatalf("the second run failed: %v", err)
	}

	same(t, out, map[string]string{"order.go": "A\n", "customer.go": "B\n"})
}

// TestAPathAPersonTookOverIsLeftAloneAndReported is where cpybkc's claim on a
// path ends. Somebody replaced a generated file with a directory or a link, on
// purpose; removing it would be this package throwing away an arrangement it
// merely failed to understand.
func TestAPathAPersonTookOverIsLeftAloneAndReported(t *testing.T) {
	t.Parallel()

	tests := []struct {
		about string
		take  func(t *testing.T, path string)
		want  string
	}{
		{
			about: "a directory",
			take: func(t *testing.T, path string) {
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatalf("taking the path over: %v", err)
				}
			},
			want: "<dir>",
		},
		{
			about: "a symlink",
			take: func(t *testing.T, path string) {
				if err := os.Symlink("/etc/passwd", path); err != nil {
					t.Fatalf("taking the path over: %v", err)
				}
			},
			want: "<a symlink>",
		},
	}

	for _, test := range tests {
		t.Run(test.about, func(t *testing.T) {
			t.Parallel()

			r, root, said := project(t)
			out := filepath.Join(root, "gen")

			if err := run(t, r, generator(t, "go", `echo A > "$4/order.go"
echo B > "$4/customer.go"`, out)); err != nil {
				t.Fatalf("the first run failed: %v", err)
			}

			taken := filepath.Join(out, "customer.go")
			if err := os.Remove(taken); err != nil {
				t.Fatalf("removing the generated file: %v", err)
			}

			test.take(t, taken)

			if err := run(t, r, generator(t, "go", `echo A > "$4/order.go"`, out)); err != nil {
				t.Fatalf("the second run failed: %v", err)
			}

			same(t, out, map[string]string{"order.go": "A\n", "customer.go": test.want})

			if !said.said("gen/customer.go", "no longer a file") {
				t.Errorf("the run said\n%s\nwant it to report leaving gen/customer.go alone", said)
			}

			// The claim is released with it: the run recorded what it
			// generated, that path is not in it, and no later run will consider
			// the path again.
			if strings.Contains(recorded(t, root), "customer.go") {
				t.Errorf("the record holds\n%s\nwant the taken-over path left out of it", recorded(t, root))
			}
		})
	}
}

// TestADirectoryPruningEmptiesIsRemoved keeps a generator that stops producing
// a package from leaving the package's directory behind — an empty directory in
// a diff is a thing a reviewer has to work out.
func TestADirectoryPruningEmptiesIsRemoved(t *testing.T) {
	t.Parallel()

	r, root, _ := project(t)
	out := filepath.Join(root, "gen")

	if err := run(t, r, generator(t, "go", `mkdir -p "$4/pkg/orders"
echo A > "$4/pkg/orders/order.go"
echo B > "$4/keep.go"`, out)); err != nil {
		t.Fatalf("the first run failed: %v", err)
	}

	if err := run(t, r, generator(t, "go", `echo B > "$4/keep.go"`, out)); err != nil {
		t.Fatalf("the second run failed: %v", err)
	}

	// Both directories, not only the one holding the file: pruning walks up
	// until a directory still holds something.
	same(t, out, map[string]string{"keep.go": "B\n"})

	// Never the project's root, and never the output directory the run is still
	// writing into.
	if !exists(t, root) || !exists(t, out) {
		t.Errorf("pruning removed a directory the run was still using: %s or %s", root, out)
	}
}

// TestADirectoryPruningEmptiesIsKeptWhereSomethingElseIsInIt is the other half:
// a directory a person also keeps files in is not this package's to delete just
// because a generator once wrote into it.
func TestADirectoryPruningEmptiesIsKeptWhereSomethingElseIsInIt(t *testing.T) {
	t.Parallel()

	r, root, _ := project(t)
	out := filepath.Join(root, "gen")

	if err := run(t, r, generator(t, "go", `mkdir "$4/pkg"
echo A > "$4/pkg/order.go"
echo B > "$4/keep.go"`, out)); err != nil {
		t.Fatalf("the first run failed: %v", err)
	}

	handwritten := filepath.Join(out, "pkg", "service.go")
	if err := os.WriteFile(handwritten, []byte("package pkg\n"), 0o644); err != nil {
		t.Fatalf("writing the hand-written file: %v", err)
	}

	if err := run(t, r, generator(t, "go", `echo B > "$4/keep.go"`, out)); err != nil {
		t.Fatalf("the second run failed: %v", err)
	}

	same(t, out, map[string]string{
		"keep.go":        "B\n",
		"pkg":            "<dir>",
		"pkg/service.go": "package pkg\n",
	})
}

// TestAPathThatChangesFromAFileToADirectoryBetweenRuns is why pruning happens
// before anything is written. A generator that produced `orders.go` and now
// produces `orders/order.go` would otherwise meet its own stale file where the
// merge needs a directory, and fail there on every run until somebody removed
// it by hand.
func TestAPathThatChangesFromAFileToADirectoryBetweenRuns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		about  string
		first  string
		second string
		want   map[string]string
	}{
		{
			about:  "a file that becomes a directory",
			first:  `echo A > "$4/orders"`,
			second: `mkdir "$4/orders"; echo B > "$4/orders/order.go"`,
			want:   map[string]string{"orders": "<dir>", "orders/order.go": "B\n"},
		},
		{
			about:  "a directory that becomes a file",
			first:  `mkdir "$4/orders"; echo B > "$4/orders/order.go"`,
			second: `echo A > "$4/orders"`,
			want:   map[string]string{"orders": "A\n"},
		},
	}

	for _, test := range tests {
		t.Run(test.about, func(t *testing.T) {
			t.Parallel()

			r, root, _ := project(t)
			out := filepath.Join(root, "gen")

			if err := run(t, r, generator(t, "go", test.first, out)); err != nil {
				t.Fatalf("the first run failed: %v", err)
			}

			if err := run(t, r, generator(t, "go", test.second, out)); err != nil {
				t.Fatalf("the second run failed: %v", err)
			}

			same(t, out, test.want)
		})
	}
}

// TestARecordThisCpybkcDidNotWriteFailsTheRunBeforeAnythingStarts is the
// refusal. Pruning from a list that has been damaged is how a stale file
// becomes permanent, and a run that read one as far as it happened to parse
// would be deleting from a list it had guessed at.
func TestARecordThisCpybkcDidNotWriteFailsTheRunBeforeAnythingStarts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		about string
		held  string
	}{
		{about: "not JSON at all", held: "{"},
		{about: "a version nobody here knows", held: `{"version": 2, "files": []}`},
		{about: "a field nobody here writes", held: `{"version": 1, "files": [], "pruned": true}`},
		{about: "more than the one object a record is", held: `{"version": 1, "files": []}{"version": 1, "files": []}`},
		{about: "something appended to the record", held: `{"version": 1, "files": []}` + "\n<<<<<<< HEAD\n"},
		{about: "a path out of the project", held: `{"version": 1, "files": ["../../etc/passwd"]}`},
		{about: "an absolute path", held: `{"version": 1, "files": ["/etc/passwd"]}`},
	}

	for _, test := range tests {
		t.Run(test.about, func(t *testing.T) {
			t.Parallel()

			r, root, _ := project(t)

			if err := os.WriteFile(filepath.Join(root, RecordName), []byte(test.held), 0o644); err != nil {
				t.Fatalf("writing the record: %v", err)
			}

			marker := filepath.Join(t.TempDir(), "ran")

			err := run(t, r, generator(t, "go", `echo ran > `+marker, filepath.Join(root, "gen")))

			var fault *RecordError
			if !errors.As(err, &fault) {
				t.Fatalf("the run failed with %v, want a RecordError", err)
			}

			// Read before a generator is started, for the reason an invocation
			// is checked before one is: it is a fault in the project rather
			// than in the output, and finding it afterwards would mean
			// discovering it once the work was done.
			if exists(t, marker) {
				t.Error("the generator ran, want the run refused before anything started")
			}
		})
	}
}

// TestOutputThatCouldNeverBeRecordedFailsTheRun. A file cpybkc generates and
// does not record is one no later run will ever remove — the single failure
// pruning exists to prevent, and an invisible one, because the output is
// perfectly correct on the run that produced it.
func TestOutputThatCouldNeverBeRecordedFailsTheRun(t *testing.T) {
	t.Parallel()

	t.Run("output that lands outside the project", func(t *testing.T) {
		t.Parallel()

		r, root, _ := project(t)
		out := filepath.Join(t.TempDir(), "elsewhere")

		err := run(t, r, generator(t, "go", `echo A > "$4/order.go"`, out))

		var fault *UnrecordableError
		if !errors.As(err, &fault) {
			t.Fatalf("the run failed with %v, want an UnrecordableError", err)
		}

		if exists(t, out) {
			t.Errorf("%s was created by a refused run, want nothing written", out)
		}

		if exists(t, filepath.Join(root, RecordName)) {
			t.Error("a refused run wrote a record, want none")
		}
	})

	t.Run("a generator that produces the record itself", func(t *testing.T) {
		t.Parallel()

		r, root, _ := project(t)

		err := run(t, r, generator(t, "go", `echo A > "$4/`+RecordName+`"`, root))

		var fault *UnrecordableError
		if !errors.As(err, &fault) {
			t.Fatalf("the run failed with %v, want an UnrecordableError", err)
		}

		if exists(t, filepath.Join(root, RecordName)) {
			t.Error("the generator's file was merged over the record, want the run refused")
		}
	})
}

// TestARefusedRunPrunesNothing keeps pruning behind every refusal there is. A
// run that was going to fail anyway must not take a project's files with it on
// the way out.
func TestARefusedRunPrunesNothing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		about string
		body  string
	}{
		{about: "a generator that failed", body: `echo A > "$4/order.go"; exit 1`},
		{about: "a generator that left a symlink", body: `ln -s /etc/passwd "$4/link"`},
	}

	for _, test := range tests {
		t.Run(test.about, func(t *testing.T) {
			t.Parallel()

			r, root, _ := project(t)
			out := filepath.Join(root, "gen")

			if err := run(t, r, generator(t, "go", `echo A > "$4/order.go"
echo B > "$4/customer.go"`, out)); err != nil {
				t.Fatalf("the first run failed: %v", err)
			}

			before := recorded(t, root)

			if err := run(t, r, generator(t, "go", test.body, out)); err == nil {
				t.Fatal("the run succeeded, want it refused")
			}

			same(t, out, map[string]string{"order.go": "A\n", "customer.go": "B\n"})

			// And the record still describes the tree, so the run after this
			// one prunes from a list that is still true.
			if got := recorded(t, root); got != before {
				t.Errorf("a refused run rewrote the record as\n%s\nwant\n%s", got, before)
			}
		})
	}
}

// TestAStaleFileThatCannotBeRemovedFailsTheRun. Everything pruning declines to
// remove of its own accord is reported and carried; this is the filesystem
// saying no, and carrying that would leave a project whose record no longer
// describes it and a stale file nothing will ever mention again.
func TestAStaleFileThatCannotBeRemovedFailsTheRun(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root removes files out of a directory it has no write permission on")
	}

	r, root, _ := project(t)
	out := filepath.Join(root, "gen")

	if err := run(t, r, generator(t, "go", `mkdir "$4/pkg"
echo A > "$4/pkg/order.go"
echo B > "$4/keep.go"`, out)); err != nil {
		t.Fatalf("the first run failed: %v", err)
	}

	pkg := filepath.Join(out, "pkg")

	// Restored before the test's own directory is torn down, which is
	// registered earlier and so runs after this.
	t.Cleanup(func() {
		if err := os.Chmod(pkg, 0o755); err != nil {
			t.Errorf("restoring %s: %v", pkg, err)
		}
	})

	if err := os.Chmod(pkg, 0o555); err != nil {
		t.Fatalf("making the directory read-only: %v", err)
	}

	err := run(t, r, generator(t, "go", `echo B > "$4/keep.go"`, out))

	var fault *PruneError
	if !errors.As(err, &fault) {
		t.Fatalf("the run failed with %v, want a PruneError", err)
	}

	if got, want := fault.Path, "gen/pkg/order.go"; got != want {
		t.Errorf("the fault names %q, want %q", got, want)
	}
}

// TestTheRecordIsWrittenWithTheRunsOwnMode is the same claim the merged files
// carry, and it exists for the same case: cpybkc running as root in a container
// over a bind mount, where a record its user cannot edit is as much of a fault
// as generated output they cannot edit.
func TestTheRecordIsWrittenWithTheRunsOwnMode(t *testing.T) {
	t.Parallel()

	r, root, _ := project(t)

	mask := os.FileMode(0o027)
	r.Umask = &mask

	if err := run(t, r, generator(t, "go", `echo A > "$4/order.go"`, filepath.Join(root, "gen"))); err != nil {
		t.Fatalf("the run failed: %v", err)
	}

	info, err := os.Stat(filepath.Join(root, RecordName))
	if err != nil {
		t.Fatalf("reading the record: %v", err)
	}

	if got, want := info.Mode().Perm(), fileMode.Perm()&^mask; got != want {
		t.Errorf("the record is %v, want %v", got, want)
	}
}
