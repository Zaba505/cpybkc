// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package generate

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// mode is the mode of whatever is at path, without following a link to it.
func mode(t *testing.T, path string) fs.FileMode {
	t.Helper()

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	return info.Mode()
}

// owner is the user and the group of whatever is at path.
func owner(t *testing.T, path string) Owner {
	t.Helper()

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	// cpybkc targets POSIX hosts and nothing else; see docs/plugin/SPEC.md,
	// "Host platform". A test that went through a portable interface would be
	// asserting against one that does not carry an owner.
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("%s reports ownership as %T, want a syscall.Stat_t", path, info.Sys())
	}

	return Owner{UID: int(stat.Uid), GID: int(stat.Gid)}
}

// umask is a mask to hand a [Runner], stated rather than read: the process's own
// is a process-wide setting, and a test that moved it would move it for every
// other test running beside it.
//
// Kept rather than inlined to `new(expr)`: its other call site passes an untyped
// constant, which `new` cannot take without a conversion, so inlining would
// leave `new(test.mask)` beside `new(fs.FileMode(0o022))` for the same idea.
func umask(mask fs.FileMode) *fs.FileMode { return &mask } //nolint:modernize // named on purpose; see above

// tight is a generator that writes under a umask of its own, so that what lands
// in the project's tree is visibly not what the plugin created.
const tight = `umask 077
echo A > "$4/orders.go"
echo B > "$4/generate.sh"
chmod 700 "$4/generate.sh"
mkdir "$4/pkg"`

func TestTheModesAreThisRunsRatherThanThePlugins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		about string
		mask  fs.FileMode
		want  map[string]fs.FileMode
	}{
		{
			about: "the usual mask",
			mask:  0o022,
			want: map[string]fs.FileMode{
				".":             0o755,
				"orders.go":     0o644,
				"generate.sh":   0o755,
				"pkg":           0o755,
				"pkg/orders.go": 0o644,
			},
		},
		{
			about: "a mask that keeps the output to its owner",
			mask:  0o077,
			want: map[string]fs.FileMode{
				".":             0o700,
				"orders.go":     0o600,
				"generate.sh":   0o700,
				"pkg":           0o700,
				"pkg/orders.go": 0o600,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.about, func(t *testing.T) {
			t.Parallel()

			r := runner(t)
			r.Umask = umask(test.mask) //nolint:modernize // [umask] is kept, so its call sites are too

			out := filepath.Join(t.TempDir(), "project", "gen")

			if err := run(t, r, generator(t, "go", tight+`
echo C > "$4/pkg/orders.go"`, out)); err != nil {
				t.Fatalf("running the generator: %v", err)
			}

			for path, want := range test.want {
				if got := mode(t, filepath.Join(out, path)).Perm(); got != want {
					t.Errorf("%s came out %o, want %o", path, got, want)
				}
			}
		})
	}
}

func TestWithNoMaskStatedThisProcessesOwnIsWhatApplies(t *testing.T) {
	t.Parallel()

	// The mask is not read by setting it — see probeUmask — so the assertion is
	// against what this process gets when it creates a file the ordinary way.
	// That is the same question asked of the same kernel, and it holds whatever
	// mask the machine running the tests happens to have.
	reference := filepath.Join(t.TempDir(), "reference")

	created, err := os.OpenFile(reference, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		t.Fatalf("creating %s: %v", reference, err)
	}

	if err := created.Close(); err != nil {
		t.Fatalf("closing %s: %v", reference, err)
	}

	out := filepath.Join(t.TempDir(), "project")

	if err := run(t, runner(t), generator(t, "go", tight, out)); err != nil {
		t.Fatalf("running the generator: %v", err)
	}

	if got, want := mode(t, filepath.Join(out, "orders.go")).Perm(), mode(t, reference).Perm(); got != want {
		t.Errorf("the merged file came out %o, want the %o this process creates a file with", got, want)
	}
}

func TestOwnershipIsGivenToEverythingTheMergeCreates(t *testing.T) {
	t.Parallel()

	// Somebody this process can give a file to. Where the tests run as root —
	// which is how the pipeline runs them — that is anybody, and asking for a
	// user this process is not makes the claim a real one; where they run as a
	// person, the only user they can give a file to is themselves.
	want := Owner{UID: os.Geteuid(), GID: os.Getegid()}
	if want.UID == 0 {
		want = Owner{UID: 65534, GID: 65534}
	}

	r := runner(t)
	r.Owner = &want

	out := filepath.Join(t.TempDir(), "project", "gen")

	if err := run(t, r, generator(t, "go", `mkdir "$4/pkg"
echo A > "$4/pkg/orders.go"`, out)); err != nil {
		t.Fatalf("running the generator: %v", err)
	}

	// The output directory, the directory the plugin made beneath it, and the
	// file: everything the merge created and nothing it did not.
	for _, path := range []string{".", "pkg", "pkg/orders.go"} {
		if got := owner(t, filepath.Join(out, path)); got != want {
			t.Errorf("%s came out owned by %d:%d, want %d:%d", path, got.UID, got.GID, want.UID, want.GID)
		}
	}
}

func TestADirectoryThatWasAlreadyThereKeepsItsMode(t *testing.T) {
	t.Parallel()

	// A merge adds to a project's tree; it does not restate what the person who
	// made the directory decided about it.
	out := t.TempDir()

	if err := os.Chmod(out, 0o777); err != nil {
		t.Fatalf("setting the mode of %s: %v", out, err)
	}

	r := runner(t)
	r.Umask = umask(0o022)

	if err := run(t, r, generator(t, "go", `echo A > "$4/orders.go"`, out)); err != nil {
		t.Fatalf("running the generator: %v", err)
	}

	if got, want := mode(t, out).Perm(), fs.FileMode(0o777); got != want {
		t.Errorf("the directory that was already there came out %o, want the %o it was left at", got, want)
	}
}

func TestASymlinkIsRefusedRatherThanFollowed(t *testing.T) {
	t.Parallel()

	elsewhere := filepath.Join(t.TempDir(), "elsewhere.go")

	if err := os.WriteFile(elsewhere, []byte("untouched\n"), 0o644); err != nil {
		t.Fatalf("writing the file outside the run: %v", err)
	}

	tests := []struct {
		about string
		body  string
	}{
		{
			about: "one pointing out of the directory it was handed",
			body:  `ln -s ` + elsewhere + ` "$4/orders.go"`,
		},
		{
			about: "one pointing inside it",
			body: `echo A > "$4/real.go"
ln -s real.go "$4/orders.go"`,
		},
		{
			about: "one in place of the directory it was handed",
			body: `rmdir "$4"
ln -s ` + filepath.Dir(elsewhere) + ` "$4"`,
		},
	}

	for _, test := range tests {
		t.Run(test.about, func(t *testing.T) {
			t.Parallel()

			out := filepath.Join(t.TempDir(), "project")

			err := run(t, runner(t), generator(t, "go", test.body, out))

			var refused *UnmergeableError
			if !errors.As(err, &refused) {
				t.Fatalf("the run failed with %v, want an UnmergeableError", err)
			}

			if refused.Mode&fs.ModeSymlink == 0 {
				t.Errorf("what was refused is reported as %s, want a symlink", refused.Mode)
			}

			// Refused, and refused before anything was written: a run that
			// merged the files beside the link and then stopped would be the
			// half-generated tree the scratch directory exists to prevent.
			if exists(t, out) {
				t.Errorf("%s was created by a refused run: %v", out, tree(t, out))
			}

			if got := contents(t, elsewhere); got != "untouched\n" {
				t.Errorf("the file outside the run reads %q, want it untouched", got)
			}
		})
	}
}

func TestEveryEntryTheMergeRefusesIsReported(t *testing.T) {
	t.Parallel()

	out := filepath.Join(t.TempDir(), "project")

	// A plugin that emits symlinks emits them by the directory. A run that
	// named one of them would be a run made once per symlink.
	err := run(t, runner(t),
		generator(t, "one", `ln -s /etc/passwd "$4/a.go"
ln -s /etc/passwd "$4/b.go"`, out),
		generator(t, "two", `ln -s /etc/passwd "$4/c.go"`, out),
	)

	if got, want := len(diag.Diagnostics(err)), 3; got != want {
		t.Errorf("the run reported %d faults, want %d:\n%s", got, want, diag.Render(err))
	}
}

func TestAnythingThatIsNotAFileOrADirectoryIsRefused(t *testing.T) {
	t.Parallel()

	out := filepath.Join(t.TempDir(), "project")

	// A named pipe is the one of these a test can make without privileges, and
	// what it stands for is the rule: the merge takes files and directories and
	// leaves everything else where it found it.
	err := run(t, runner(t), generator(t, "go", `mkfifo "$4/orders.go"`, out))

	var refused *UnmergeableError
	if !errors.As(err, &refused) {
		t.Fatalf("the run failed with %v, want an UnmergeableError", err)
	}

	if got, want := refused.Path, "orders.go"; got != want {
		t.Errorf("the fault names %q, want %q", got, want)
	}

	if exists(t, out) {
		t.Errorf("%s was created by a refused run: %v", out, tree(t, out))
	}
}

func TestTwoGeneratorsProducingOnePathFailTheRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		about string

		// scenario is the generators of one run, landing beneath project, and
		// the fault the run is to report. It is built inside the test because
		// every path in it is the test's own temporary directory's.
		scenario func(t *testing.T, project string) ([]Generator, CollisionError)
	}{
		{
			about: "two generators landing in one directory",
			scenario: func(t *testing.T, project string) ([]Generator, CollisionError) {
				out := filepath.Join(project, "gen")

				return []Generator{
						generator(t, "one", `echo A > "$4/orders.go"`, out),
						generator(t, "two", `echo B > "$4/orders.go"`, out),
					}, CollisionError{
						First: "one", FirstPath: "orders.go",
						Second: "two", SecondPath: "orders.go",
						Dest: filepath.Join(out, "orders.go"),
					}
			},
		},
		{
			// The two plugins agree about nothing: one of them produced
			// `gen/orders.go` and the other `orders.go`, and they are one file
			// because the directories the manifest gave them overlap.
			about: "output directories that overlap rather than coincide",
			scenario: func(t *testing.T, project string) ([]Generator, CollisionError) {
				out := filepath.Join(project, "gen")

				return []Generator{
						generator(t, "one", `mkdir "$4/gen"
echo A > "$4/gen/orders.go"`, project),
						generator(t, "two", `echo B > "$4/orders.go"`, out),
					}, CollisionError{
						First: "one", FirstPath: "gen/orders.go",
						Second: "two", SecondPath: "orders.go",
						Dest: filepath.Join(out, "orders.go"),
					}
			},
		},
		{
			// Not a file against a file, and still one path two generators both
			// produced: merging either would decide for a plugin what its own
			// output is.
			about: "a file where the other generator produced a directory",
			scenario: func(t *testing.T, project string) ([]Generator, CollisionError) {
				out := filepath.Join(project, "gen")

				return []Generator{
						generator(t, "one", `echo A > "$4/pkg"`, out),
						generator(t, "two", `mkdir "$4/pkg"
echo B > "$4/pkg/orders.go"`, out),
					}, CollisionError{
						First: "one", FirstPath: "pkg",
						Second: "two", SecondPath: "pkg",
						Dest: filepath.Join(out, "pkg"),
					}
			},
		},
		{
			// The second generator produced nothing at that path and could not
			// have: it is the output directory the manifest gave it, which the
			// merge creates on its behalf. Compared as produced entries alone,
			// the two would agree right up until the merge tried to make a
			// directory of the first generator's file — with that file written.
			about: "a file where the other was told to land its output",
			scenario: func(t *testing.T, project string) ([]Generator, CollisionError) {
				out := filepath.Join(project, "gen")

				return []Generator{
						generator(t, "one", `echo A > "$4/pkg"`, out),
						generator(t, "two", `echo B > "$4/orders.go"`, filepath.Join(out, "pkg")),
					}, CollisionError{
						First: "one", FirstPath: "pkg",
						Second: "two", SecondPath: ".",
						Dest: filepath.Join(out, "pkg"),
					}
			},
		},
		{
			// The same fault a directory further up: what the merge has to
			// create to reach an output directory is every directory above it,
			// and a file standing in the way of one of those fails just as late.
			about: "a file where the other's output directory has to be reached through",
			scenario: func(t *testing.T, project string) ([]Generator, CollisionError) {
				out := filepath.Join(project, "gen")

				return []Generator{
						generator(t, "one", `echo A > "$4/pkg"`, out),
						generator(t, "two", `echo B > "$4/orders.go"`, filepath.Join(out, "pkg", "orders")),
					}, CollisionError{
						First: "one", FirstPath: "pkg",
						Second: "two", SecondPath: ".",
						Dest: filepath.Join(out, "pkg"),
					}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.about, func(t *testing.T) {
			t.Parallel()

			project := filepath.Join(t.TempDir(), "project")
			generators, want := test.scenario(t, project)

			err := run(t, runner(t), generators...)

			var collision *CollisionError
			if !errors.As(err, &collision) {
				t.Fatalf("the run failed with %v, want a CollisionError", err)
			}

			if *collision != want {
				t.Errorf("the fault is %+v, want %+v", *collision, want)
			}

			// Refused before anything was written. A run that merged what did
			// not collide and then stopped would leave the half-generated tree
			// the two passes exist to prevent, and would leave it differently
			// depending on which generator got there first.
			if exists(t, project) {
				t.Errorf("%s was created by a refused run: %v", project, tree(t, project))
			}
		})
	}
}

func TestTwoGeneratorsMayProduceOneDirectory(t *testing.T) {
	t.Parallel()

	// A directory carries nothing for two generators to disagree about, and two
	// of them landing in one place ordinarily both need `pkg`. What is one
	// generator's each is the files inside it.
	out := filepath.Join(t.TempDir(), "project", "gen")

	if err := run(t, runner(t),
		generator(t, "one", `mkdir "$4/pkg"
echo A > "$4/pkg/orders.go"`, out),
		generator(t, "two", `mkdir "$4/pkg"
echo B > "$4/pkg/invoices.go"`, out),
	); err != nil {
		t.Fatalf("running the generators: %v", err)
	}

	same(t, out, map[string]string{
		"pkg":             "<dir>",
		"pkg/orders.go":   "A\n",
		"pkg/invoices.go": "B\n",
	})
}

func TestEveryCollisionIsReported(t *testing.T) {
	t.Parallel()

	project := filepath.Join(t.TempDir(), "project")
	out := filepath.Join(project, "gen")

	// Two generators that produce one path usually produce a directory of them
	// — a manifest that asked both for the same package asked for every file in
	// it — and a run that named one would be a run made once per file.
	err := run(t, runner(t),
		generator(t, "one", `echo A > "$4/orders.go"
echo A > "$4/invoices.go"`, out),
		generator(t, "two", `echo B > "$4/orders.go"
echo B > "$4/invoices.go"`, out),
	)

	if got, want := len(diag.Diagnostics(err)), 2; got != want {
		t.Errorf("the run reported %d faults, want %d:\n%s", got, want, diag.Render(err))
	}

	if exists(t, project) {
		t.Errorf("%s was created by a refused run: %v", project, tree(t, project))
	}
}

func TestACollisionNamesTheGeneratorsInTheOrderTheyWereDeclared(t *testing.T) {
	t.Parallel()

	out := filepath.Join(t.TempDir(), "project")

	// Generators run concurrently, so the one that produced a path first is not
	// the one that finished first. A fault naming whichever of them lost the
	// race would report the same unchanged inputs differently on different runs
	// — so the generator the run declared first is named first, however long it
	// takes to produce anything.
	err := run(t, runner(t),
		generator(t, "slow", `sleep 1
echo A > "$4/orders.go"`, out),
		generator(t, "quick", `echo B > "$4/orders.go"`, out),
	)

	var collision *CollisionError
	if !errors.As(err, &collision) {
		t.Fatalf("the run failed with %v, want a CollisionError", err)
	}

	if got, want := collision.First, "slow"; got != want {
		t.Errorf("the fault names %q first, want %q, which the run declared first", got, want)
	}
}

func TestARerunReplacesWhatItProducedAndNeverWritesThroughALink(t *testing.T) {
	t.Parallel()

	elsewhere := filepath.Join(t.TempDir(), "elsewhere.go")

	if err := os.WriteFile(elsewhere, []byte("untouched\n"), 0o644); err != nil {
		t.Fatalf("writing the file outside the run: %v", err)
	}

	out := t.TempDir()

	if err := os.WriteFile(filepath.Join(out, "orders.go"), []byte("the run before\n"), 0o644); err != nil {
		t.Fatalf("writing the previous run's output: %v", err)
	}

	// A link where a generated file goes, left by a person or by something else
	// that writes into this directory. Opening it would write the run's output
	// through it, into a file nobody named.
	if err := os.Symlink(elsewhere, filepath.Join(out, "order.go")); err != nil {
		t.Fatalf("writing the link: %v", err)
	}

	if err := run(t, runner(t), generator(t, "go", `echo new > "$4/orders.go"
echo new > "$4/order.go"`, out)); err != nil {
		t.Fatalf("running the generator: %v", err)
	}

	same(t, out, map[string]string{"orders.go": "new\n", "order.go": "new\n"})

	if got := contents(t, elsewhere); got != "untouched\n" {
		t.Errorf("the file the link pointed at reads %q, want it untouched", got)
	}
}

func TestALinkWhereADirectoryHasToGoIsNotWrittenThrough(t *testing.T) {
	t.Parallel()

	// The escape a merge that resolved its path components with os.Stat would
	// make: `pkg` in the project's tree is a link to somewhere else entirely,
	// os.Stat reads it as a perfectly good directory, and every file the plugin
	// produced beneath `pkg` goes through it.
	elsewhere := t.TempDir()
	out := t.TempDir()

	if err := os.Symlink(elsewhere, filepath.Join(out, "pkg")); err != nil {
		t.Fatalf("writing the link: %v", err)
	}

	err := run(t, runner(t), generator(t, "go", `mkdir "$4/pkg"
echo A > "$4/pkg/orders.go"`, out))

	var merge *MergeError
	if !errors.As(err, &merge) {
		t.Fatalf("the run failed with %v, want a MergeError", err)
	}

	if !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("the fault is %v, want it to carry the filesystem's own", err)
	}

	same(t, elsewhere, map[string]string{})
}

func TestAnOutputDirectoryReachedThroughALinkIsFollowed(t *testing.T) {
	t.Parallel()

	// The other half of the same rule. A person's project reached through a
	// link — /tmp on a Mac, a checkout under a symlinked home, an output
	// directory pointed somewhere on purpose — is the path the manifest named,
	// and writing there is writing where it asked.
	actual := t.TempDir()
	link := filepath.Join(t.TempDir(), "project")

	if err := os.Symlink(actual, link); err != nil {
		t.Fatalf("writing the link: %v", err)
	}

	if err := run(t, runner(t), generator(t, "go", `mkdir "$4/pkg"
echo A > "$4/pkg/orders.go"`, filepath.Join(link, "gen"))); err != nil {
		t.Fatalf("running the generator: %v", err)
	}

	same(t, actual, map[string]string{
		"gen":               "<dir>",
		"gen/pkg":           "<dir>",
		"gen/pkg/orders.go": "A\n",
	})
}

func TestAFileWhereADirectoryHasToGoIsAFaultAndNotASilence(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	if err := os.WriteFile(filepath.Join(out, "pkg"), []byte("not a directory\n"), 0o644); err != nil {
		t.Fatalf("writing the file in the way: %v", err)
	}

	err := run(t, runner(t), generator(t, "go", `mkdir "$4/pkg"
echo A > "$4/pkg/orders.go"`, out))

	var merge *MergeError
	if !errors.As(err, &merge) {
		t.Fatalf("the run failed with %v, want a MergeError", err)
	}

	if !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("the fault is %v, want it to carry the filesystem's own", err)
	}
}
