// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package scaffold

import (
	"errors"
	"fmt"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// ErrNoCopybooks is [Derive] handed nothing to derive from.
//
// It is an ordinary error rather than a [diag.Error] because it is not a fault
// in anybody's file: it says the caller asked for a scaffold over no copybooks,
// which the command line refuses as a usage error before this package is
// reached.
var ErrNoCopybooks = errors.New("there is no copybook to derive a scaffold from")

// ErrNoDestination is [Write] handed nowhere to put the layout, for
// [ErrNoCopybooks]'s reason.
var ErrNoDestination = errors.New("name the file to write the layout to; its definitions files go beside it")

// Every fault here leads with the copybook or the destination it is about and
// says nothing else about where it is. There is no layout under `init` — that is
// the whole point of the command — so a path has one spelling rather than two,
// and the one it has is the one that was typed on the command line, which is
// what the person running it can go and correct.

// SourceError is a copybook `init` was given that cannot be read as COBOL this
// build understands.
//
// The cause is carried on a continuation rather than reworded: what `cobol-go`
// found is more specific than anything this package could say about it, and a
// continuation is how a second line reaches standard error without being read as
// a diagnostic of its own.
type SourceError struct {
	// Path is the --copybook value as it was typed.
	Path string

	// Err is what the reader said.
	Err error
}

// Error implements error.
func (e *SourceError) Error() string { return e.Diagnostic().String() }

// Unwrap returns the underlying fault, so that a caller can assert on whatever
// `cobol-go` raised.
func (e *SourceError) Unwrap() error { return e.Err }

// Diagnostic implements [diag.Error].
func (e *SourceError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: "this is not a copybook this build can read",
		Spans:   []diag.Span{{File: e.Path}, {Note: e.Err.Error()}},
	}
}

// NoEntryError is a copybook that parsed and declares no entry at all.
//
// A copybook is a record description, and one holding nothing — comments, or
// nothing — has nothing to transcribe: the definitions file converted from it
// would be a header naming a copybook and no entry under it, which no record
// could name and the layout reader would refuse as not being a definitions file
// at all. docs/cli/SPEC.md, "`init`'s streams and exit codes", keeps it a
// refusal (#31) where a copybook declaring entries and no named 01-level is a
// note: that one is still transcribed whole.
type NoEntryError struct {
	// Path is the --copybook value as it was typed.
	Path string
}

// Error implements error.
func (e *NoEntryError) Error() string { return e.Diagnostic().String() }

// Diagnostic implements [diag.Error].
func (e *NoEntryError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: "this copybook declares no entry at all, so there is nothing in it to transcribe",
		Spans:   []diag.Span{{File: e.Path}},
	}
}

// TranscriptionError is a copybook that parsed and built and still cannot be
// written as a definitions file: one holding a node the writer has no form
// for, which no entry `cobol-go`'s parser builds holds at the pinned commit, so
// that a clause a later pin adds is refused rather than dropped.
//
// It is the copybook's to report rather than the writer's, because the writer
// knows the line and column it refused and not the file: a position in a
// copybook is a position in the file as it was typed, and that is what this
// adds to every span that has a line and no file.
type TranscriptionError struct {
	// Path is the --copybook value as it was typed.
	Path string

	// Err is what the writer refused.
	Err error
}

// Error implements error.
func (e *TranscriptionError) Error() string { return e.Diagnostic().String() }

// Unwrap returns the writer's refusal.
func (e *TranscriptionError) Unwrap() error { return e.Err }

// Diagnostic implements [diag.Error].
func (e *TranscriptionError) Diagnostic() diag.Diagnostic {
	var inner diag.Error
	if !errors.As(e.Err, &inner) {
		return diag.Diagnostic{
			Message: "this copybook cannot be transcribed as a definitions file",
			Spans:   []diag.Span{{File: e.Path}, {Note: e.Err.Error()}},
		}
	}

	d := inner.Diagnostic()
	spans := make([]diag.Span, 0, len(d.Spans)+1)

	for _, span := range d.Spans {
		if span.File == "" && span.Line != 0 {
			span.File = e.Path
		}

		spans = append(spans, span)
	}

	if len(spans) == 0 || !spans[0].Stated() {
		spans = append([]diag.Span{{File: e.Path}}, spans...)
	}

	return diag.Diagnostic{Message: d.Message, Spans: spans}
}

// ReadBackError is a definitions file that does not read back as the entries of
// the copybook it was converted from.
//
// docs/cli/SPEC.md, "`init`'s streams and exit codes": before anything is
// written, every definitions file is read back from the bytes about to be
// written and its entries compared with the copybook's, positions and comments
// cleared. When this fires it is cpybkc's own defect, and it is caught here
// rather than in the adopter's next run because a definitions file is never
// re-read from its copybook — a transcription that was wrong when it was written
// stays wrong in every run after.
type ReadBackError struct {
	// Path is the --copybook value as it was typed.
	Path string

	// Line and Column are the copybook's entry whose transcription differed,
	// and Item names it. All three are zero where no one entry is to blame — a
	// file that did not read at all, or read back as a different number of
	// entries.
	Line, Column int
	Item         string

	// Err is what the read said, where the read itself failed.
	Err error
}

// Error implements error.
func (e *ReadBackError) Error() string { return e.Diagnostic().String() }

// Unwrap returns what the read said.
func (e *ReadBackError) Unwrap() error { return e.Err }

// Diagnostic implements [diag.Error].
func (e *ReadBackError) Diagnostic() diag.Diagnostic {
	const defect = "; this is a defect in cpybkc, and nothing was written"

	if e.Item == "" {
		d := diag.Diagnostic{
			Message: "the definitions file converted from this copybook does not read back as its entries" + defect,
			Spans:   []diag.Span{{File: e.Path}},
		}

		if e.Err != nil {
			d.Spans = append(d.Spans, diag.Span{Note: e.Err.Error()})
		}

		return d
	}

	return diag.Diagnostic{
		Message: fmt.Sprintf("the definitions file converted from this copybook reads %s back as a different entry", e.Item) +
			defect,
		Spans: []diag.Span{{File: e.Path, Line: e.Line, Column: e.Column}},
	}
}

// DefinitionsNameError is two copybooks whose definitions files would have one
// name between them.
//
// The command line refuses this as a usage error before anything is opened
// (docs/cli/SPEC.md, "The vector `init` takes"), and the check is here as well
// because [Derive] is reachable without one: a set of files with two of one name
// in it is not one [Write] could put in a directory.
type DefinitionsNameError struct {
	// First and Second are the two --copybook values, as typed.
	First, Second string

	// Name is the name both derive.
	Name string
}

// Error implements error.
func (e *DefinitionsNameError) Error() string { return e.Diagnostic().String() }

// Diagnostic implements [diag.Error].
func (e *DefinitionsNameError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("the definitions files converted from this copybook and from %s would both be named %s",
			e.Second, e.Name),
		Spans: []diag.Span{{File: e.First}, {File: e.Second, Note: "this copybook derives the same name"}},
	}
}

// DestinationError is a destination with something already at it: the --out
// path, or a definitions file's place beside it.
//
// docs/cli/SPEC.md, "Where the scaffold is written, and why nothing is ever
// overwritten": whatever is there — a regular file, a directory, a symbolic
// link, including one that dangles — the run fails and nothing is written. Both
// paths are named because a relative path in a shared tree sends a reader to the
// wrong directory, and the path as derived is the one they can find from what
// they wrote.
type DestinationError struct {
	// Path is the destination as it was derived from what was typed, and
	// Absolute the path cpybkc looked at.
	Path     string
	Absolute string
}

// Error implements error.
func (e *DestinationError) Error() string { return e.Diagnostic().String() }

// Diagnostic implements [diag.Error].
func (e *DestinationError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: "there is already something here, and cpybkc never writes over what it finds; " +
			"remove it, or name somewhere else",
		Spans: []diag.Span{{File: e.Path}, {Note: "cpybkc looked at " + e.Absolute}},
	}
}

// WriteError is a destination that was free and still could not be written.
type WriteError struct {
	// Path is the destination as it was derived from what was typed.
	Path string

	// Err is what the write failed with.
	Err error
}

// Error implements error.
func (e *WriteError) Error() string { return e.Diagnostic().String() }

// Unwrap returns the underlying fault.
func (e *WriteError) Unwrap() error { return e.Err }

// Diagnostic implements [diag.Error].
func (e *WriteError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: "this file could not be written, and nothing else this run wrote was kept",
		Spans:   []diag.Span{{File: e.Path}, {Note: e.Err.Error()}},
	}
}
