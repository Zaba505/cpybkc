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
