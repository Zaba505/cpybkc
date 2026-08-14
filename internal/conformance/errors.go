// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package conformance

import (
	"errors"
	"fmt"
)

// EntryError is a fault in one entry, naming the entry it is in.
//
// The entry's name leads because a corpus failure is read against a directory
// listing: what an author needs first is which entry to open. Everything wrong
// with one entry is one of these, carrying a joined error, rather than several
// — an entry is a tuple and it is loaded or it is not.
type EntryError struct {
	// Entry is the entry's directory name.
	Entry string

	// Err is what is wrong with it, which is [errors.Join]'s value where more
	// than one thing is.
	Err error
}

func (e *EntryError) Error() string {
	return fmt.Sprintf("conformance entry %s: %v", e.Entry, e.Err)
}

func (e *EntryError) Unwrap() error { return e.Err }

// MismatchError is a runner's answer disagreeing with what an entry expects.
//
// It carries the entry and the source the entry cites as well as the
// disagreement, because a corpus failure is read by somebody who has to decide
// whether their generator is wrong or the entry is, and that decision starts at
// the section the expected answer was derived from (#68).
type MismatchError struct {
	// Entry is the entry's directory name.
	Entry string

	// Source is what the entry cites as the origin of its expected answer.
	Source string

	// Err is the disagreement, which is [errors.Join]'s value where a runner
	// disagreed in more than one place.
	Err error
}

func (e *MismatchError) Error() string {
	return fmt.Sprintf("conformance entry %s (%s): %v", e.Entry, e.Source, e.Err)
}

func (e *MismatchError) Unwrap() error { return e.Err }

// PathError is one disagreement [Compare] found, carrying the path through the
// record it is at as well as the sentence a reader sees.
//
// The message is the sentence and nothing else: the path is already in it,
// spelled for whoever is reading a report, and a wrapper that said it twice
// would be a wrapper every report had to be edited around. What the type adds
// is the same path in a form a caller can key on, so that a caller holding the
// descriptor can say where the descriptor puts the item that disagreed — its
// offset, its width, its usage and its charset — beside the disagreement
// (#199).
//
// That caller is the engine, and it is the only thing in the system that can do
// it: the adapter was never told what was expected, and this package compares
// documents rather than bytes. Recovering the path by parsing the sentence back
// out of an error string would work exactly until somebody improved the
// sentence, which is the kind of coupling a type costs nothing to replace.
type PathError struct {
	// Path is where in the values document the disagreement is, in the spelling
	// [Compare] uses throughout: `record 1 ORDER-RECORD.ORDER-ID`, with an
	// occurrence written `[0]` and counted from zero as the document writes it.
	Path string

	// Err is the disagreement, whose message already names Path.
	Err error
}

func (e *PathError) Error() string { return e.Err.Error() }

func (e *PathError) Unwrap() error { return e.Err }

// RunError is an entry a runner could not answer about at all: the generator
// would not run, what it produced would not compile, or the runner could not be
// driven.
//
// It carries the entry and its source for the reason [MismatchError] does, and
// it is a separate type because the two send a reader somewhere different. A
// mismatch is a disagreement about bytes, and whoever reads it decides whether
// the generator or the entry is wrong. This one is the corpus failing to ask
// the question, so nothing has been learned about either — and a run that
// reported it as a disagreement would have a generator author reading a spec
// section about a claim that was never tested (#68).
type RunError struct {
	// Entry is the entry's directory name.
	Entry string

	// Source is what the entry cites as the origin of its expected answer.
	Source string

	// Err is what stopped the run.
	Err error
}

func (e *RunError) Error() string {
	return fmt.Sprintf("conformance entry %s (%s) could not be run: %v", e.Entry, e.Source, e.Err)
}

func (e *RunError) Unwrap() error { return e.Err }

// joined reports every fault at once, and nothing at all where there are none.
//
// It exists so that the readers above can accumulate into a slice and hand it
// over without each of them deciding what an empty slice means.
func joined(faults []error) error {
	if len(faults) == 0 {
		return nil
	}

	return errors.Join(faults...)
}
