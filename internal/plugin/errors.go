// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package plugin

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// Neither fault here carries a position in a file, and both are still
// [github.com/Zaba505/cpybkc/internal/diag.Error]. A generator that is not on
// PATH is wrong about the machine rather than about a line: cpybkc.json can name
// a generator that is perfectly well spelled and simply not installed, so a
// diagnostic that pointed at the name would be pointing at text that is not the
// mistake. What these carry instead is every place the search did look, in the
// order it looked, which is the half an adopter cannot see for themselves.
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

// quote renders a name the way a message names one, so that an empty one and a
// name with a space in it are both visible as what they are.
func quote(name string) string { return strconv.Quote(name) }
