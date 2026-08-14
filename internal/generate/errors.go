// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package generate

import (
	"fmt"
	"io/fs"
	"path/filepath"
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
// directory, and the fix is a full disk or a [Runner.Scratch] that is not
// writable.
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
//
// A directory is in the list for pruning's sake rather than the merge's: a
// directory is something the merge makes and never something it refuses, but a
// recorded path that has *become* one is left alone and said so about, and the
// sentence saying so has to name what it found.
func kind(mode fs.FileMode) string {
	switch {
	case mode.IsDir():
		return "a directory"
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
// claim.
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
	// First and Second are the generators that claim it, in the order the run
	// declared them rather than the order they finished in: generators run
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
	//
	// Either is `.` where what that generator claims is not something it
	// produced at all but the output directory it was given, or a directory
	// above it — a path the merge has to create for the generator to land
	// anything, and the one claim on a project's tree a plugin had no say in.
	FirstPath, SecondPath string

	// Dest is the path in the project's tree both of them claim.
	Dest string
}

// Error implements the error interface.
func (e *CollisionError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
//
// One shape for every collision rather than a sentence per kind. The two sides
// are not alike — a file against a file, a file against a directory, a file
// against the directory another generator was told to land its output in — and
// a single sentence saying which was which would need a clause per combination.
// The path both claim is the message, each side says for itself what it wanted
// with it, and the last span is the rule rather than a place, for the reason
// [UnmergeableError.Diagnostic] gives.
func (e *CollisionError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("the generators %s and %s both claim %s, and nothing was merged",
			strconv.Quote(e.First), strconv.Quote(e.Second), strconv.Quote(e.Dest)),
		Spans: []diag.Span{
			{},
			{Note: claim(e.First, e.FirstPath)},
			{Note: claim(e.Second, e.SecondPath)},
			{Note: "a path in a project's tree is one generator's, so cpybkc refuses the run rather than merging whichever it reached last: stop one of them producing it, or land the two in directories that do not overlap"},
		},
	}
}

// claim is one side of a collision saying what it wanted with the path.
func claim(name, path string) string {
	if path == "." {
		return fmt.Sprintf("it has to be a directory for the generator %s to land its output",
			strconv.Quote(name))
	}

	return fmt.Sprintf("the generator %s produced it as %s", strconv.Quote(name), strconv.Quote(path))
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

// RecordError is the record of what a run generated, which could not be read,
// could not be written, or is not one this cpybkc wrote.
//
// The record is what makes pruning safe: only a file some run recorded is ever
// removed, so the list is the whole of what cpybkc claims to own. That is why a
// record it cannot read is a failed run rather than a run that quietly prunes
// nothing — the two look identical to a person and only one of them is honest,
// and the fix is one file to delete, which the diagnostic says.
//
// A record that is simply *not there* is not this: a first run has none, and a
// person who deleted theirs has said cpybkc owns nothing in their tree yet. It
// prunes nothing and says nothing.
type RecordError struct {
	// Path is the record file, as this run named it.
	Path string

	// Fault is what is wrong with the record itself — a version this cpybkc
	// does not know, a path it could not have written. Empty where what went
	// wrong is [RecordError.Err].
	Fault string

	// Err is what the filesystem or [encoding/json] returned. Nil where the
	// file was read perfectly well and [RecordError.Fault] says what it holds.
	Err error

	// Writing reports whether the run was writing this run's record rather
	// than reading the last one's. It is the direction and not the fault: the
	// two fail for entirely different reasons — a record nobody can parse
	// against a disk with nothing left on it — and a message that did not say
	// which would send a reader to look at the wrong one.
	Writing bool
}

// Error implements the error interface.
func (e *RecordError) Error() string { return e.Diagnostic().String() }

// Unwrap is the underlying error, so that a caller can ask what kind of failure
// it was. It is nil where the record was read and is the record that is wrong.
func (e *RecordError) Unwrap() error { return e.Err }

// Diagnostic is what the error says, and where.
//
// The last span is the file's provenance rather than a second place, because
// the thing a reader most needs to know about this file is that it is not one
// of theirs: it is cpybkc's, it is rewritten by every run, and deleting it
// costs a stale file one more run and nothing else.
func (e *RecordError) Diagnostic() diag.Diagnostic {
	message := fmt.Sprintf("the record of what the last run generated could not be read: %v", e.Err)

	switch {
	case e.Writing:
		message = fmt.Sprintf("the record of what this run generated could not be written: %v", e.Err)
	case e.Fault != "":
		message = "the record of what the last run generated is not one this cpybkc wrote: " + e.Fault
	}

	return diag.Diagnostic{
		Message: message,
		Spans: []diag.Span{
			{File: e.Path},
			{Note: "cpybkc writes this file itself at the end of every run and reads it at the start of the next, to remove what it generated before and no longer does; deleting it is safe and costs one run's worth of stale files"},
		},
	}
}

// PruneError is a file a previous run generated, which this run does not, and
// which could not be removed.
//
// Everything pruning declines to remove of its own accord — a recorded path
// that is now a directory, a symlink, or gone already — is reported to the user
// and is not a fault. This is the filesystem: a directory that is not writable,
// a file somebody else's process holds. It fails the run rather than being
// carried, because the alternative is a project whose record no longer
// describes it and a stale file nothing will ever mention again.
type PruneError struct {
	// Path is the file as the record spells it: relative to the project's root
	// and slash-separated, which is how a person reading their record and their
	// diff will find it.
	Path string

	// Dest is where that path is, in the project's tree.
	Dest string

	// Err is what went wrong, from the filesystem.
	Err error
}

// Error implements the error interface.
func (e *PruneError) Error() string { return e.Diagnostic().String() }

// Unwrap is the filesystem's error, so that a caller can ask what kind of
// failure it was.
func (e *PruneError) Unwrap() error { return e.Err }

// Diagnostic is what the error says, and where.
func (e *PruneError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("%s, which a previous run generated and this one does not, could not be removed: %v",
			strconv.Quote(e.Path), e.Err),
		Spans: []diag.Span{{}, {File: e.Dest, Note: "this is the file it is"}},
	}
}

// UnrecordableError is output a run produced that it cannot record, and so
// could never prune.
//
// Two ways to get one, and neither is a plugin's doing. A generator pointed at
// a directory outside the project's root lands files the record has no way to
// name — the record holds paths beneath the root, so that it means the same
// thing in a checkout as it did on the machine that wrote it. And a generator
// that produces the record's own file is producing something the end of the run
// overwrites.
//
// It fails the run before anything is written, rather than being merged and
// left out of the record. A file cpybkc generates and does not record is one no
// later run will ever remove, which is the single failure pruning exists to
// prevent — and it would be invisible, because the output would be perfectly
// correct on the run that produced it.
type UnrecordableError struct {
	// Name is the generator that produced it.
	Name string

	// Path is what that generator called it, relative to the directory it was
	// handed.
	Path string

	// Dest is where it was to land, in the project's tree.
	Dest string

	// Root is the project's root, which is what the record's paths are
	// relative to and what the destination has to be beneath.
	Root string
}

// Error implements the error interface.
func (e *UnrecordableError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
//
// Two shapes, because the two faults have different fixes: one is an output
// directory to move and the other is a filename to change. The last span is the
// rule rather than a place, for the reason [UnmergeableError.Diagnostic] gives.
func (e *UnrecordableError) Diagnostic() diag.Diagnostic {
	if e.Dest == filepath.Join(e.Root, RecordName) {
		return diag.Diagnostic{
			Message: fmt.Sprintf("the generator %s produced %s, which is the file cpybkc keeps its own record of a run in",
				strconv.Quote(e.Name), strconv.Quote(e.Path)),
			Spans: []diag.Span{
				{},
				{File: e.Dest, Note: "this is where it was to be written"},
				{Note: "cpybkc rewrites that file at the end of every run, so what a generator put there would be thrown away: have it produce some other path"},
			},
		}
	}

	return diag.Diagnostic{
		Message: fmt.Sprintf("the generator %s produced %s, which lands outside the project and cannot be recorded",
			strconv.Quote(e.Name), strconv.Quote(e.Path)),
		Spans: []diag.Span{
			{},
			{File: e.Dest, Note: "this is where it was to be written"},
			{Note: fmt.Sprintf("cpybkc records what it generated beneath %s so that a later run can remove what it no longer generates, and a file outside that could never be recorded or removed: land this generator's output inside the project",
				strconv.Quote(e.Root))},
		},
	}
}
