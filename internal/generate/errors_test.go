// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package generate

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// TestEveryFaultReadsTheSameThroughEitherEnd is what
// [github.com/Zaba505/cpybkc/internal/diag.Error] asks of a typed error: the
// message a caller prints and the diagnostic a caller inspects are built in one
// place, so the two cannot say different things.
func TestEveryFaultReadsTheSameThroughEitherEnd(t *testing.T) {
	t.Parallel()

	faults := []error{
		&ScratchError{Err: errors.New("no space left on device")},
		&ScratchError{Name: "go", Err: errors.New("no space left on device")},
		&UnmergeableError{Name: "go", Path: "orders.go", Mode: fs.ModeSymlink, Target: "/etc/passwd"},
		&UnmergeableError{Name: "go", Path: "orders.go", Mode: fs.ModeNamedPipe},
		&UnmergeableError{Name: "go", Path: ".", Mode: fs.ModeSymlink},
		&CollisionError{
			First: "go", FirstPath: "orders.go",
			Second: "docs", SecondPath: "orders.go",
			Dest: "gen/orders.go",
		},
		&CollisionError{
			First: "go", FirstPath: "gen/orders.go",
			Second: "docs", SecondPath: "orders.go",
			Dest: "gen/orders.go",
		},
		&CollisionError{
			First: "go", FirstPath: "gen",
			Second: "docs", SecondPath: ".",
			Dest: "gen",
		},
		&MergeError{Name: "go", Dest: "gen", Err: errors.New("permission denied")},
		&MergeError{Name: "go", Path: "orders.go", Dest: "gen/orders.go", Err: errors.New("permission denied")},
		&RecordError{Path: "/src/" + RecordName, Err: errors.New("unexpected end of JSON input")},
		&RecordError{Path: "/src/" + RecordName, Fault: "it declares version 2, and this cpybkc writes and reads version 1"},
		&RecordError{Path: "/src/" + RecordName, Writing: true, Err: errors.New("no space left on device")},
		&PruneError{Path: "gen/orders.go", Dest: "/src/gen/orders.go", Err: errors.New("permission denied")},
		&UnrecordableError{Name: "go", Path: "orders.go", Dest: "/elsewhere/orders.go", Root: "/src"},
		&UnrecordableError{Name: "go", Path: RecordName, Dest: "/src/" + RecordName, Root: "/src"},
	}

	for _, fault := range faults {
		carrier, ok := fault.(diag.Error)
		if !ok {
			t.Errorf("%T carries no diagnostic, want it to implement diag.Error", fault)

			continue
		}

		if got, want := fault.Error(), carrier.Diagnostic().String(); got != want {
			t.Errorf("%T reads as %q and renders as %q, want one wording", fault, got, want)
		}
	}
}

// TestARefusalSaysWhatWasLeftAndWhyItWasNotMerged is the golden the refusal is
// held to. A person reading it never sees the scratch directory — it is removed
// as the run ends — so what the message has to carry is the generator, the path
// that generator chose, and the rule it broke.
func TestARefusalSaysWhatWasLeftAndWhyItWasNotMerged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		about string
		fault *UnmergeableError
		want  string
	}{
		{
			about: "a symlink out of the directory",
			fault: &UnmergeableError{Name: "go", Path: "pkg/orders.go", Mode: fs.ModeSymlink, Target: "/etc/passwd"},
			want: `the generator "go" left a symlink to "/etc/passwd" at "pkg/orders.go", and its output was not merged
  a symlink is how a generator writes outside the directory it was handed, so cpybkc refuses one rather than following it`,
		},
		{
			about: "a symlink whose target could not be read",
			fault: &UnmergeableError{Name: "go", Path: "pkg/orders.go", Mode: fs.ModeSymlink},
			want: `the generator "go" left a symlink at "pkg/orders.go", and its output was not merged
  a symlink is how a generator writes outside the directory it was handed, so cpybkc refuses one rather than following it`,
		},
		{
			about: "the directory itself, replaced",
			fault: &UnmergeableError{Name: "go", Path: ".", Mode: fs.ModeSymlink | 0o777, Target: "/tmp"},
			want: `the generator "go" left a symlink to "/tmp" in place of the directory it was handed, and its output was not merged
  a symlink is how a generator writes outside the directory it was handed, so cpybkc refuses one rather than following it`,
		},
		{
			about: "something that is not a file at all",
			fault: &UnmergeableError{Name: "go", Path: "orders.go", Mode: fs.ModeNamedPipe},
			want: `the generator "go" left a named pipe at "orders.go", and its output was not merged
  a generator's output is the files and the directories it leaves beneath the directory it was handed, and cpybkc merges nothing else`,
		},
	}

	for _, test := range tests {
		if got := diag.Render(test.fault); got != test.want {
			t.Errorf("%s renders as\n%s\nwant\n%s", test.about, got, test.want)
		}
	}
}

// TestACollisionNamesBothGeneratorsAndWhatTheyProduced is what #44 asks of the
// message. A person reading it has two plugins and one path, neither plugin is
// the one that was wrong, and what they have to change is the manifest that
// asked both for it — so the fault names both, names the path each of them
// called it, and says where the two would have landed.
func TestACollisionNamesBothGeneratorsAndWhatTheyProduced(t *testing.T) {
	t.Parallel()

	const rule = "  a path in a project's tree is one generator's, so cpybkc refuses the run rather than merging whichever it reached last: stop one of them producing it, or land the two in directories that do not overlap"

	tests := []struct {
		about string
		fault *CollisionError
		want  string
	}{
		{
			about: "two generators landing in one directory",
			fault: &CollisionError{
				First: "go", FirstPath: "pkg/orders.go",
				Second: "docs", SecondPath: "pkg/orders.go",
				Dest: "/src/gen/pkg/orders.go",
			},
			want: `the generators "go" and "docs" both claim "/src/gen/pkg/orders.go", and nothing was merged
  the generator "go" produced it as "pkg/orders.go"
  the generator "docs" produced it as "pkg/orders.go"
` + rule,
		},
		{
			// Neither plugin's author would recognise the other's name for it,
			// so a message quoting one alone would send half the readers looking
			// for a path their generator never wrote.
			about: "output directories that overlap",
			fault: &CollisionError{
				First: "go", FirstPath: "pkg/orders.go",
				Second: "docs", SecondPath: "orders.go",
				Dest: "/src/gen/pkg/orders.go",
			},
			want: `the generators "go" and "docs" both claim "/src/gen/pkg/orders.go", and nothing was merged
  the generator "go" produced it as "pkg/orders.go"
  the generator "docs" produced it as "orders.go"
` + rule,
		},
		{
			// One of the two never produced anything at that path: it is where
			// the manifest told it to land its output, and a message saying it
			// produced a directory there would be describing a plugin's doing.
			about: "a file where the other was told to land its output",
			fault: &CollisionError{
				First: "go", FirstPath: "pkg",
				Second: "docs", SecondPath: ".",
				Dest: "/src/gen/pkg",
			},
			want: `the generators "go" and "docs" both claim "/src/gen/pkg", and nothing was merged
  the generator "go" produced it as "pkg"
  it has to be a directory for the generator "docs" to land its output
` + rule,
		},
	}

	for _, test := range tests {
		if got := diag.Render(test.fault); got != test.want {
			t.Errorf("%s renders as\n%s\nwant\n%s", test.about, got, test.want)
		}
	}
}

// TestAMergeFailureSaysHowFarTheRunGot keeps the two shapes apart: a fault about
// one file names it and where it was landing, and a fault about the output
// directory itself has no file to name and must not invent one.
func TestAMergeFailureSaysHowFarTheRunGot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		about string
		fault *MergeError
		want  string
	}{
		{
			about: "one file",
			fault: &MergeError{
				Name: "go",
				Path: "pkg/orders.go",
				Dest: "/src/gen/pkg/orders.go",
				Err:  errors.New("no space left on device"),
			},
			want: `"pkg/orders.go", from the generator "go", could not be merged: no space left on device
  /src/gen/pkg/orders.go: this is where it was to be written`,
		},
		{
			about: "the directory it was landing in",
			fault: &MergeError{Name: "go", Dest: "/src/gen", Err: errors.New("permission denied")},
			want: `the output of the generator "go" could not be merged: permission denied
  /src/gen: this is the directory it was landing in`,
		},
	}

	for _, test := range tests {
		if got := diag.Render(test.fault); got != test.want {
			t.Errorf("%s renders as\n%s\nwant\n%s", test.about, got, test.want)
		}
	}
}

// TestARecordFaultSaysWhoseFileItIs is what a person needs from a fault about a
// file they have never heard of and did not write. It leads with the direction
// — a record read against a record written, which fail for entirely unrelated
// reasons — and it says who keeps the file and what deleting it costs, because
// that is the fix for every fault in this list that is not a full disk.
func TestARecordFaultSaysWhoseFileItIs(t *testing.T) {
	t.Parallel()

	const provenance = "  cpybkc writes this file itself at the end of every run and reads it at the start of the next, to remove what it generated before and no longer does; deleting it is safe and costs one run's worth of stale files"

	tests := []struct {
		about string
		fault *RecordError
		want  string
	}{
		{
			about: "a record that would not parse",
			fault: &RecordError{Path: "/src/cpybkc.gen.json", Err: errors.New("unexpected end of JSON input")},
			want: `/src/cpybkc.gen.json: the record of what the last run generated could not be read: unexpected end of JSON input
` + provenance,
		},
		{
			about: "a record from a cpybkc that has not been written yet",
			fault: &RecordError{
				Path:  "/src/cpybkc.gen.json",
				Fault: "it declares version 2, and this cpybkc writes and reads version 1",
			},
			want: `/src/cpybkc.gen.json: the record of what the last run generated is not one this cpybkc wrote: it declares version 2, and this cpybkc writes and reads version 1
` + provenance,
		},
		{
			about: "a record that could not be written",
			fault: &RecordError{
				Path:    "/src/cpybkc.gen.json",
				Writing: true,
				Err:     errors.New("no space left on device"),
			},
			want: `/src/cpybkc.gen.json: the record of what this run generated could not be written: no space left on device
` + provenance,
		},
	}

	for _, test := range tests {
		if got := diag.Render(test.fault); got != test.want {
			t.Errorf("%s renders as\n%s\nwant\n%s", test.about, got, test.want)
		}
	}
}

// TestOutputThatCannotBeRecordedSaysWhichOfTheTwoWaysItIs keeps the two shapes
// apart, because they have different fixes: one is an output directory to move
// and the other is a filename to change.
func TestOutputThatCannotBeRecordedSaysWhichOfTheTwoWaysItIs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		about string
		fault *UnrecordableError
		want  string
	}{
		{
			about: "output outside the project",
			fault: &UnrecordableError{Name: "go", Path: "orders.go", Dest: "/elsewhere/orders.go", Root: "/src"},
			want: `the generator "go" produced "orders.go", which lands outside the project and cannot be recorded
  /elsewhere/orders.go: this is where it was to be written
  cpybkc records what it generated beneath "/src" so that a later run can remove what it no longer generates, and a file outside that could never be recorded or removed: land this generator's output inside the project`,
		},
		{
			about: "the record's own file",
			fault: &UnrecordableError{
				Name: "go", Path: "cpybkc.gen.json",
				Dest: "/src/cpybkc.gen.json", Root: "/src",
			},
			want: `the generator "go" produced "cpybkc.gen.json", which is the file cpybkc keeps its own record of a run in
  /src/cpybkc.gen.json: this is where it was to be written
  cpybkc rewrites that file at the end of every run, so what a generator put there would be thrown away: have it produce some other path`,
		},
	}

	for _, test := range tests {
		if got := diag.Render(test.fault); got != test.want {
			t.Errorf("%s renders as\n%s\nwant\n%s", test.about, got, test.want)
		}
	}
}

// TestAStaleFileThatCouldNotBeRemovedNamesItAsTheRecordSpellsIt. A person
// holding this fault is looking at their record and at their diff, and both of
// them spell the path relative to the project's root.
func TestAStaleFileThatCouldNotBeRemovedNamesItAsTheRecordSpellsIt(t *testing.T) {
	t.Parallel()

	fault := &PruneError{
		Path: "gen/pkg/orders.go",
		Dest: "/src/gen/pkg/orders.go",
		Err:  errors.New("permission denied"),
	}

	want := `"gen/pkg/orders.go", which a previous run generated and this one does not, could not be removed: permission denied
  /src/gen/pkg/orders.go: this is the file it is`

	if got := diag.Render(fault); got != want {
		t.Errorf("it renders as\n%s\nwant\n%s", got, want)
	}
}
