// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package generate

import (
	"fmt"
	"io/fs"
	"strconv"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// No fault here points at a line of anything an adopter wrote, for the reason
// the plugin package's do not: what went wrong is a generator's doing or the
// machine's, and a span naming the layout or the manifest would send a reader
// to edit text that is not the mistake. Neither does one name the scratch
// directory it happened in — that directory is removed as the run ends, so a
// path into it is a place the reader cannot go and look. What is named instead
// is the generator, the path *it* chose for the file, and where in the project
// that path was to land, which is the whole of what a person needs to find the
// line of the plugin that wrote it.

// ScratchError is a run that could not be given somewhere for its generators to
// write.
//
// No generator ran, or one of them was about to and had nowhere to put its
// output. It is separate from a generator that ran and failed because nothing
// about it is the generator's doing: what failed is this process making a
// directory, and the fix is a full disk or a TMPDIR that is not writable.
type ScratchError struct {
	// Name is the generator whose directory it was, or empty where what could
	// not be made is the run's own scratch space.
	Name string

	// Err is what went wrong, from the filesystem.
	Err error
}

// Error implements the error interface.
func (e *ScratchError) Error() string { return e.Diagnostic().String() }

// Unwrap is the filesystem's error, so that a caller can ask what kind of
// failure it was.
func (e *ScratchError) Unwrap() error { return e.Err }

// Diagnostic is what the error says, and where.
func (e *ScratchError) Diagnostic() diag.Diagnostic {
	if e.Name == "" {
		return diag.Diagnostic{
			Message: fmt.Sprintf("this run could not be given anywhere for its generators to write: %v", e.Err),
		}
	}

	return diag.Diagnostic{
		Message: fmt.Sprintf("the generator %s could not be given a directory to write into: %v",
			strconv.Quote(e.Name), e.Err),
	}
}

// UnmergeableError is something a generator left in its directory that cpybkc
// will not put into a project's tree.
//
// docs/plugin/SPEC.md: a plugin's output is the files it writes beneath the
// directory it was handed, and it MUST NOT write outside that directory —
// including through a symlink it created for the purpose. cpybkc enforces that
// rather than trusting it, and this is the enforcement: a symlink is refused
// wherever it points rather than followed, and so is anything else that is
// neither a regular file nor a directory.
//
// It fails the whole run with nothing merged, so the fault costs the run and
// never a half-written tree.
type UnmergeableError struct {
	// Name is the generator that left it.
	Name string

	// Path is what the generator called it: the path relative to the directory
	// the generator was handed, which is the name a plugin author will
	// recognise. It is `.` where what the generator left is the directory
	// itself, replaced by something that is not one.
	Path string

	// Mode is what it turned out to be, as [io/fs.FileMode] renders a type.
	Mode fs.FileMode

	// Target is where it pointed, for a symlink, as the link was written.
	// Empty for anything else, and for a link that could not be read.
	Target string
}

// Error implements the error interface.
func (e *UnmergeableError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
//
// The second span is the rule rather than a second place, because there is no
// second place: the fault is a decision inside an executable this repository
// did not write, and what a reader needs is the sentence of the contract that
// decision broke.
func (e *UnmergeableError) Diagnostic() diag.Diagnostic {
	what := kind(e.Mode)
	note := "a generator's output is the files and the directories it leaves beneath the directory it was handed, and cpybkc merges nothing else"

	if e.Mode&fs.ModeSymlink != 0 {
		note = "a symlink is how a generator writes outside the directory it was handed, so cpybkc refuses one rather than following it"

		if e.Target != "" {
			what += " to " + strconv.Quote(e.Target)
		}
	}

	where := "at " + strconv.Quote(e.Path)
	if e.Path == "." {
		where = "in place of the directory it was handed"
	}

	return diag.Diagnostic{
		Message: fmt.Sprintf("the generator %s left %s %s, and its output was not merged",
			strconv.Quote(e.Name), what, where),
		Spans: []diag.Span{{}, {Note: note}},
	}
}

// kind names what the merge found, in the words a sentence about it uses.
//
// [io/fs.FileMode.String] renders a type as the letter `ls -l` prints, which is
// the right answer in a listing and the wrong one in a sentence: a diagnostic
// reading `the generator "go" left p---------` names the thing in a notation the
// reader has to already know. The default is deliberately vague rather than
// exhaustive — what a reader does about anything in this list is the same, and
// the list is the operating system's to extend.
func kind(mode fs.FileMode) string {
	switch {
	case mode&fs.ModeSymlink != 0:
		return "a symlink"
	case mode&fs.ModeDevice != 0:
		return "a device"
	case mode&fs.ModeNamedPipe != 0:
		return "a named pipe"
	case mode&fs.ModeSocket != 0:
		return "a socket"
	default:
		return "something that is neither a file nor a directory"
	}
}

// CollisionError is one place in a project's tree that two generators both
// produced.
//
// docs/plugin/SPEC.md: two generators producing the same output path is an
// error cpybkc raises before anything is merged, naming both, and a collision
// fails the run with nothing merged. Merging one of them would make what the
// project holds a function of which generator the merge reached last — and
// since a plugin is told nothing about the others and MUST NOT coordinate with
// them, neither of the two is the one that meant it.
//
// It is found in the same pass as an [UnmergeableError] and before anything is
// written, so a collision costs the run and never half a tree.
type CollisionError struct {
	// First and Second are the generators that produced it, in the order the
	// run declared them rather than the order they finished in: generators run
	// concurrently, and a fault naming whichever of them lost a race would
	// report the same inputs differently on different runs.
	First, Second string

	// FirstPath and SecondPath are what each of them called it, relative to the
	// directory that generator was handed — the name each plugin's author will
	// recognise.
	//
	// They are the same path except where the two output directories overlap
	// rather than coincide: a generator landing in `gen` that produced
	// `pkg/orders.go` and one landing in `gen/pkg` that produced `orders.go`
	// have produced one file between them.
	FirstPath, SecondPath string

	// Dest is where both were to land, in the project's tree.
	Dest string
}

// Error implements the error interface.
func (e *CollisionError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
//
// The last span is the rule rather than a place, for the reason
// [UnmergeableError.Diagnostic] gives: nothing in this repository decided this,
// and what a reader needs is what to do about two executables that disagree.
func (e *CollisionError) Diagnostic() diag.Diagnostic {
	message := fmt.Sprintf("the generators %s and %s both produced %s, and nothing was merged",
		strconv.Quote(e.First), strconv.Quote(e.Second), strconv.Quote(e.FirstPath))

	// Where the output directories overlap, the two names are two names for one
	// path, and a message quoting either alone would send half of its readers
	// looking for something their plugin never wrote.
	if e.FirstPath != e.SecondPath {
		message = fmt.Sprintf("%s, from the generator %s, and %s, from the generator %s, land in the same place, and nothing was merged",
			strconv.Quote(e.FirstPath), strconv.Quote(e.First),
			strconv.Quote(e.SecondPath), strconv.Quote(e.Second))
	}

	return diag.Diagnostic{
		Message: message,
		Spans: []diag.Span{
			{},
			{File: e.Dest, Note: "this is where both were to land"},
			{Note: "a path in a project's tree is one generator's, so cpybkc refuses the run rather than merging whichever it reached last: stop one of them producing it, or land the two in directories that do not overlap"},
		},
	}
}

// MergeError is output that could not be put into the project's tree.
//
// Everything the merge refuses of its own accord is an [UnmergeableError] or a
// [CollisionError] and is reported before anything is written, so this is the
// filesystem: a full disk, a directory that is not writable, a path in the
// project's tree where a file stands and a directory has to go. Unlike a
// refusal, it can be raised with part of the run's output already merged —
// which is why it names both the generator's own path and where that path was
// landing, so that a person can see how far the run got.
type MergeError struct {
	// Name is the generator whose output it was.
	Name string

	// Path is what the generator called it, relative to the directory it was
	// handed. Empty where the fault is about the output directory itself rather
	// than about anything in it.
	Path string

	// Dest is where it was to be written, in the project's tree.
	Dest string

	// Err is what went wrong, from the filesystem.
	Err error
}

// Error implements the error interface.
func (e *MergeError) Error() string { return e.Diagnostic().String() }

// Unwrap is the filesystem's error, so that a caller can ask what kind of
// failure it was.
func (e *MergeError) Unwrap() error { return e.Err }

// Diagnostic is what the error says, and where.
func (e *MergeError) Diagnostic() diag.Diagnostic {
	if e.Path == "" {
		return diag.Diagnostic{
			Message: fmt.Sprintf("the output of the generator %s could not be merged: %v",
				strconv.Quote(e.Name), e.Err),
			Spans: []diag.Span{{}, {File: e.Dest, Note: "this is the directory it was landing in"}},
		}
	}

	return diag.Diagnostic{
		Message: fmt.Sprintf("%s, from the generator %s, could not be merged: %v",
			strconv.Quote(e.Path), strconv.Quote(e.Name), e.Err),
		Spans: []diag.Span{{}, {File: e.Dest, Note: "this is where it was to be written"}},
	}
}
