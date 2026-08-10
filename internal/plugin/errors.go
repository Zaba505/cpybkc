// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package plugin

import (
	"fmt"
	"strconv"
	"strings"
	"syscall"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// No fault here carries a position in a file, and every one of them is still
// [github.com/Zaba505/cpybkc/internal/diag.Error]. A generator that is not on
// PATH is wrong about the machine rather than about a line: cpybkc.json can name
// a generator that is perfectly well spelled and simply not installed, so a
// diagnostic that pointed at the name would be pointing at text that is not the
// mistake. What these carry instead is every place the search did look, in the
// order it looked, which is the half an adopter cannot see for themselves. A
// generator that ran and failed is the same shape of fault one step later: the
// mistake is inside an executable this repository did not write, so what the
// diagnostic can offer is which generator it was and what it did.
//
// A caller that does hold a position — the stage that read the manifest — is
// free to say so around one of these. It is not said here, because a package
// that invented a span would make the two statements disagree the first time a
// name arrived from somewhere that is not a manifest.

// InvalidNameError is a generator name that cannot be a filename component.
//
// docs/plugin/SPEC.md: a `<name>` MUST be non-empty and MUST NOT contain a `/`,
// because the name is the suffix of the `cpybkc-gen-<name>` file that is
// searched for. The rest of what that section says about a name — lowercase
// ASCII letters, digits and hyphens — is a SHOULD and is not enforced anywhere:
// this repository reports what an adopter has to fix and has no channel for
// advice they may take or leave.
//
// [github.com/Zaba505/cpybkc/internal/manifest] refuses both names earlier, and
// with the line in cpybkc.json they were written at, but as two faults rather
// than one: a name carrying a `/` is its
// [github.com/Zaba505/cpybkc/internal/manifest.GeneratorNameError], and an
// empty one is the
// [github.com/Zaba505/cpybkc/internal/manifest.EmptyValueError] every field
// that has to carry something raises. The wording here deliberately matches the
// first of those, which is the one whose sentence is about a generator name;
// see the package comment for why both packages check at all.
type InvalidNameError struct {
	// Name is what was asked for.
	Name string
}

// Error implements the error interface.
func (e *InvalidNameError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *InvalidNameError) Diagnostic() diag.Diagnostic {
	const opening = "a generator name is the suffix of the " + Prefix + "<name> executable it resolves to, and "

	message := opening + quote(e.Name) + " contains a /"
	if e.Name == "" {
		message = opening + "this one is empty"
	}

	return diag.Diagnostic{Message: message}
}

// PassedOver is a file with the name of the executable that was being looked
// for, which was not a candidate for it.
//
// docs/plugin/SPEC.md makes a candidate a regular file carrying an execute bit
// and gives the search no way to report one that is neither: it continues, as a
// shell's does. Every one of them is kept so that [NotFoundError] can name it,
// because "the file is there and cpybkc did not take it" is a different problem
// from "the file is not there" and the fix — usually one chmod — is not one an
// adopter finds by rereading their PATH.
type PassedOver struct {
	// Path is the file, as PATH spells the directory it is in.
	Path string

	// Fault is why it was not a candidate, in the words the diagnostic shows
	// beside it.
	Fault string
}

// NotFoundError is a generator name that nothing on PATH resolves.
//
// It names the executable that was looked for rather than only the generator
// that was asked for, because those are two strings an adopter has to line up
// by hand otherwise: the manifest says `"name": "go"` and the file they have to
// install is called `cpybkc-gen-go`, and the gap between the two is where the
// mistake usually is.
type NotFoundError struct {
	// Name is the generator that was asked for.
	Name string

	// File is the executable that would have been it: Name under [Prefix], as
	// [Filename] spells it.
	File string

	// Searched are the PATH elements the search looked in, in order, as PATH
	// spelled them. Empty elements are not among them: an empty element names
	// no directory, and one listed as searched would be reporting a place
	// nobody wrote.
	Searched []string

	// PassedOver are the files of that name the search would not take, in the
	// order it met them.
	PassedOver []PassedOver
}

// Error implements the error interface.
func (e *NotFoundError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
//
// The first span names nowhere, which is what [diag.Span.Stated] is for: the
// fault is in the machine and not in a file. Everything after it is somewhere
// the search looked — each file it passed over, and then the directories
// themselves, in the order PATH wrote them.
func (e *NotFoundError) Diagnostic() diag.Diagnostic {
	spans := []diag.Span{{}}

	for _, passed := range e.PassedOver {
		spans = append(spans, diag.Span{
			File: passed.Path,
			Note: passed.Fault + ", so the search continued past it",
		})
	}

	searched := "PATH names no directory to search"
	if len(e.Searched) > 0 {
		searched = "PATH was searched in order: " + strings.Join(e.Searched, ", ")
	}

	return diag.Diagnostic{
		Message: fmt.Sprintf("there is no generator named %s on PATH; cpybkc looks for an executable named %s",
			quote(e.Name), e.File),
		Spans: append(spans, diag.Span{Note: searched}),
	}
}

// InvalidOptionError is an option key that cannot be half of a `--opt k=v`.
//
// docs/plugin/SPEC.md: everything before the first `=` of the argument is the
// key, so a key MUST be non-empty and MUST NOT contain one — a key carrying an
// `=` would reach the generator split somewhere the manifest did not write it,
// under a name nobody chose, and the generator would refuse a key its author
// had never heard of.
//
// [github.com/Zaba505/cpybkc/internal/manifest] refuses both earlier and with
// the line in cpybkc.json they were written at. This is the same MUST enforced
// for the second audience, exactly as [InvalidNameError] is: an option reaches
// an [Invocation] from wherever the caller had one, and one that arrived by
// another route would otherwise be passed on as an argument this contract does
// not define.
type InvalidOptionError struct {
	// Name is the generator the option was declared for.
	Name string

	// Key is what was written for the key.
	Key string
}

// Error implements the error interface.
func (e *InvalidOptionError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *InvalidOptionError) Diagnostic() diag.Diagnostic {
	opening := "an option key is the k of the --opt k=v the generator " + quote(e.Name) + " is passed, and "

	message := opening + quote(e.Key) + " contains an ="
	if e.Key == "" {
		message = opening + "this one is empty"
	}

	return diag.Diagnostic{Message: message}
}

// DescriptorError is a descriptor that could not be put where a generator could
// read it.
//
// The generator never ran: docs/plugin/SPEC.md requires the file to be written
// in full and closed first, so a failure here is one that happened before there
// was an invocation to fail. It is reported as the generator's all the same,
// because a run naming no generator leaves a user with several to look at.
type DescriptorError struct {
	// Name is the generator whose descriptor it was.
	Name string

	// Err is what went wrong, from the filesystem.
	Err error
}

// Error implements the error interface.
func (e *DescriptorError) Error() string { return e.Diagnostic().String() }

// Unwrap is the filesystem's error, so that a caller can ask what kind of
// failure it was.
func (e *DescriptorError) Unwrap() error { return e.Err }

// Diagnostic is what the error says, and where.
func (e *DescriptorError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("the descriptor for the generator %s could not be written, so it was not run: %v",
			quote(e.Name), e.Err),
	}
}

// StartError is a generator that could not be started.
//
// It is distinct from a non-zero exit because nothing ran: the executable
// resolved and then would not execute — an interpreter its `#!` line names and
// the machine has not got is the usual one, and it is reported by the kernel as
// a file that is not there, naming a file the adopter can see perfectly well.
type StartError struct {
	// Name is the generator that was to be run.
	Name string

	// File is the executable that was to run it, as [Resolve] spelled it.
	File string

	// Err is what went wrong, from the operating system.
	Err error
}

// Error implements the error interface.
func (e *StartError) Error() string { return e.Diagnostic().String() }

// Unwrap is the operating system's error.
func (e *StartError) Unwrap() error { return e.Err }

// Diagnostic is what the error says, and where.
func (e *StartError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("the generator %s could not be started: %v", quote(e.Name), e.Err),
		Spans:   []diag.Span{{}, {File: e.File, Note: "this is the executable that would have run"}},
	}
}

// ExitError is a generator that ran and exited non-zero.
//
// docs/plugin/SPEC.md: a non-zero exit means the invocation failed, cpybkc MUST
// fail the run naming the generator, and no meaning beyond failure is attached
// to the particular value — the small integers are already spoken for by a
// shell's 126 and 127 and by the 128-plus-signal a shell reports a killed
// process as. The number is carried because it is what the generator said and
// an author debugging one wants to see it, not because cpybkc read anything
// into it.
//
// Why the generator's own explanation is not in here: it was on standard error,
// and it has already reached the user through the log, attributed and at the
// level the plugin asked for. Holding it back to put it in this message would
// be surfacing a diagnostic when the process ended rather than when it was
// written, which is the one thing the contract says not to do with a plugin's
// stderr.
type ExitError struct {
	// Name is the generator that failed.
	Name string

	// File is the executable that ran, as [Resolve] spelled it.
	File string

	// Code is the exit status it exited with.
	Code int
}

// Error implements the error interface.
func (e *ExitError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *ExitError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("the generator %s failed: it exited %d", quote(e.Name), e.Code),
		Spans:   []diag.Span{{}, {File: e.File, Note: "this is the executable that ran"}},
	}
}

// SignalError is a generator that was killed rather than one that failed.
//
// docs/plugin/SPEC.md requires the two to be distinguishable, and this type is
// how: a caller asserts against it rather than reading a number out of an exit
// status. They need different responses — a non-zero exit is a bug in the
// generator, a signal is usually the run being cancelled or the machine running
// out of memory — and a report that flattened them would send the user looking
// in the wrong place, most often at a generator that did nothing wrong.
type SignalError struct {
	// Name is the generator that was killed.
	Name string

	// File is the executable that was running, as [Resolve] spelled it.
	File string

	// Signal is the signal that killed it.
	Signal syscall.Signal
}

// Error implements the error interface.
func (e *SignalError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
//
// The signal is named twice, by number and by the description the runtime gives
// it, because neither is enough on its own: "killed" does not say which signal
// and 9 does not say what happened. The name is not spelled SIGKILL, because a
// table of those in this repository would be a second answer to what a signal
// is called and would rot the first time a host defined one it did not have.
func (e *SignalError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("the generator %s was terminated by signal %d (%s)", quote(e.Name), int(e.Signal), e.Signal),
		Spans: []diag.Span{
			{},
			{File: e.File, Note: "this is the executable that was running"},
			{Note: "a generator that is killed is usually the run being cancelled or the machine running out of memory, rather than a fault in the generator"},
		},
	}
}

// quote renders a name the way a message names one, so that an empty one and a
// name with a space in it are both visible as what they are.
func quote(name string) string { return strconv.Quote(name) }
