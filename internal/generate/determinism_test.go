// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package generate

import (
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Zaba505/cpybkc/internal/plugin"
)

// docs/plugin/SPEC.md, "Determinism": the same generators, the same descriptor
// and the same options produce the same set of relative paths with
// byte-identical contents, whatever the machine underneath. The requirement is
// checked rather than asserted, and this is where the checking happens: the run
// below is made repeatedly, from surroundings that disagree about everything a
// generator can see of its machine through its environment and about every path
// it is handed, and every tree is compared against the first.
//
// What this half cannot vary is the same facts read through a call rather than
// through the environment — os.Hostname, os/user, os.Getwd, the clock. Two runs
// in one process share all of those, and varying them would mean a container
// per run for a property a comparison is the wrong instrument for anyway. The
// source scan in internal/plugin's determinism_test.go is what covers them, by
// refusing the calls outright, and neither half sees the whole thing.
//
// Repetition is what catches an order decided by a map iteration, because Go
// randomises that on every range — the record this package writes is a list of
// every file the run produced, and a list assembled from a map would come out
// in a different order most times it was written. It cannot catch a clock read
// either, since two runs a moment apart agree on the date.

// runs is how many times the project below is generated. Enough that an order
// left to Go's map iteration would have to be unlucky many times over to come
// out the same, and few enough that the check costs a second.
const runs = 8

// emitsAPackage is a generator that writes across two directories, in an order
// its own loop decides rather than a sorted one, and records the timestamp the
// runs agree about.
//
// More than one file on purpose: what a single-file run would fail to exercise
// is the order this package's record puts them in, which is the map iteration
// the repetition is here for.
const emitsAPackage = `mkdir -p "$4/nested"
for f in delta alpha foxtrot charlie bravo echo; do
	echo "package pkg // $f" > "$4/$f.go"
	echo "$f" > "$4/nested/$f.txt"
done
echo "$SOURCE_DATE_EPOCH" > "$4/generated_at"`

// emitsBeside is a second generator landing in the first one's directory, which
// is the ordinary case for a project generating one package from two of them.
const emitsBeside = `echo "package pkg // docs" > "$4/docs.go"`

// emitsItsHostname is a generator that breaks the rule, used to check that the
// comparison would see it.
const emitsItsHostname = `echo "$HOSTNAME" > "$4/host"`

// machine is a runner whose surroundings are stated rather than inherited, so
// that two runs can genuinely disagree about all of them: the user, the host,
// the home directory, the time zone, the locale and the working directory, each
// as a generator finds it in its environment, plus the plugin's own path, which
// differs because every directory here is the test's own.
//
// The axis that used to be the temporary directory is now [Runner.Scratch], and
// it varies with n through [generate]: since #184 that field is what decides
// where a run's scratch space goes and where, one level inside it, each
// invocation's descriptor directory goes. Those are the two absolute paths this
// package chooses, so they are what the comparison has to see differ.
//
// TMPDIR is still varied, and it is varied at a directory that does not exist.
// Nothing reads it any more — that is the claim — and a claim is worth a check:
// were either path put back on it, the run would fail outright on a parent that
// is not there, rather than quietly passing because two runs agreed about a
// directory neither of them was looking at.
//
// Environment-visible throughout, which is the limit of what a run in this
// process can vary; see the note at the top of this file for what covers the
// same facts read through a call.
//
// Everything varies with n except SOURCE_DATE_EPOCH, which is an input to
// generation rather than an accident of the machine: docs/plugin/SPEC.md makes
// it the one channel a timestamp may come from, so two runs that disagreed
// about it would be entitled to differ.
func machine(t *testing.T, n int) *Runner {
	t.Helper()

	id := strconv.Itoa(n)

	return &Runner{
		Plugins: &plugin.Runner{
			Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
			Env: []string{
				// PATH so that a shell script can reach the commands it calls,
				// and this process's because a stated one would be a claim
				// about where a machine keeps mkdir.
				"PATH=" + os.Getenv("PATH"),
				"HOSTNAME=host-" + id,
				"USER=person-" + id,
				"LOGNAME=person-" + id,
				"HOME=/home/person-" + id,
				"PWD=" + t.TempDir(),
				// A directory that is not there, on purpose: see the note
				// above. Nothing in cpybkc reads TMPDIR since #184, and a run
				// that started reading it again would fail here rather than
				// pass.
				"TMPDIR=" + filepath.Join(t.TempDir(), "no-temporary-directory-"+id),
				"TZ=" + []string{"UTC", "Australia/Eucla"}[n%2],
				"LANG=" + []string{"C", "tr_TR.UTF-8"}[n%2],
				"SOURCE_DATE_EPOCH=1700000000",
			},
		},
	}
}

// generate runs generators through the nth machine into a project of their own,
// and hands back what is in that project afterwards.
func generate(t *testing.T, n int, bodies ...string) map[string]string {
	t.Helper()

	project := t.TempDir()

	r := machine(t, n)
	r.Root = project

	// The axis that used to be TMPDIR. The run's scratch space is made here,
	// and each invocation's descriptor directory one level inside it, so this
	// is the field the absolute paths cpybkc chooses now come from — and it is
	// a different directory on every run.
	r.Scratch = project

	generators := make([]Generator, 0, len(bodies))

	for i, body := range bodies {
		generators = append(generators,
			generator(t, "gen"+strconv.Itoa(i), body, filepath.Join(project, "pkg")))
	}

	if err := r.Run(t.Context(), descriptor(), generators); err != nil {
		t.Fatalf("run %d: %v", n, err)
	}

	return tree(t, project)
}

func TestRunsFromMachinesThatDisagreeProduceOneTree(t *testing.T) {
	t.Parallel()

	first := generate(t, 0, emitsAPackage, emitsBeside)

	// The record is part of what is compared, and it has to be: it is a file
	// the project commits, so a list that came out in a different order would
	// be a diff on every regeneration.
	if _, written := first[RecordName]; !written {
		t.Fatalf("the run wrote no %s, so the comparison below covers less than it claims", RecordName)
	}

	// The timestamp the runs agree about reaches the generator through the
	// environment cpybkc was started with, which is the propagation
	// docs/plugin/SPEC.md requires of SOURCE_DATE_EPOCH (#47). Asserted here
	// rather than left to the comparison, because two runs that both saw
	// nothing would agree too.
	if got, want := first[filepath.Join("pkg", "generated_at")], "1700000000\n"; got != want {
		t.Errorf("the generator recorded SOURCE_DATE_EPOCH as %q, want %q", got, want)
	}

	for n := 1; n < runs; n++ {
		got := generate(t, n, emitsAPackage, emitsBeside)

		if maps.Equal(got, first) {
			continue
		}

		for path, content := range got {
			if was, produced := first[path]; !produced {
				t.Errorf("run %d produced %s, which the first run did not", n, path)
			} else if was != content {
				t.Errorf("run %d put %q in %s, and the first run put %q there", n, content, path, was)
			}
		}

		for path := range first {
			if _, produced := got[path]; !produced {
				t.Errorf("run %d produced no %s, which the first run did", n, path)
			}
		}
	}
}

func TestTheComparisonWouldSeeARunThatVariedWithItsMachine(t *testing.T) {
	t.Parallel()

	// A generator that embeds the hostname breaks the rule the test above
	// checks. If the two trees came out equal anyway, the machines the runs are
	// made from do not actually differ where a generator can see them, and the
	// comparison above would pass on output that was not deterministic at all.
	if maps.Equal(generate(t, 0, emitsItsHostname), generate(t, 1, emitsItsHostname)) {
		t.Error("two runs whose machines disagree produced one tree from a generator that embeds its hostname")
	}
}
