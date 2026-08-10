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
		&MergeError{Name: "go", Dest: "gen", Err: errors.New("permission denied")},
		&MergeError{Name: "go", Path: "orders.go", Dest: "gen/orders.go", Err: errors.New("permission denied")},
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
			want: `the generators "go" and "docs" both produced "pkg/orders.go", and nothing was merged
  /src/gen/pkg/orders.go: this is where both were to land
` + rule,
		},
		{
			// Neither plugin's author would recognise the other's name for it,
			// so quoting one alone would send half the readers looking for a
			// path their generator never wrote.
			about: "output directories that overlap",
			fault: &CollisionError{
				First: "go", FirstPath: "pkg/orders.go",
				Second: "docs", SecondPath: "orders.go",
				Dest: "/src/gen/pkg/orders.go",
			},
			want: `"pkg/orders.go", from the generator "go", and "orders.go", from the generator "docs", land in the same place, and nothing was merged
  /src/gen/pkg/orders.go: this is where both were to land
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
