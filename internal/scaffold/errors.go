// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package scaffold

import (
	"errors"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// ErrNoCopybooks is [Derive] handed nothing to derive from.
//
// It is an ordinary error rather than a [diag.Error] because it is not a fault
// in anybody's file: it says the caller asked for a scaffold over no copybooks,
// which the command line refuses as a usage error before this package is
// reached.
var ErrNoCopybooks = errors.New("there is no copybook to derive a scaffold from")

// ErrNoDestination is [Write] handed nowhere to put the scaffold, for
// [ErrNoCopybooks]'s reason.
var ErrNoDestination = errors.New("name a file to write the scaffold to, or " + Stdout + " for standard output")

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

// NoRecordError is a copybook that parsed and declares nothing a `record` form
// could be written over — no named 01-level, or no entry at all.
//
// A copybook is a record description, and one holding no named 01-level
// contributes no record to a scaffold. Reporting it is what stops a run
// succeeding over a file the adopter expected records out of — a member holding
// nothing but level-77 work items is the ordinary way to arrive here, and it
// parses perfectly well.
//
// It is a refusal only until definitions files exist (#408). docs/cli/SPEC.md,
// "`init`'s streams and exit codes", makes a copybook with entries and no named
// 01-level a note once there is a definitions file to write for it, and keeps
// refusing one that declares no entry at all; until then there is nothing at all
// to write for either, and a run that exited 0 having written nothing about a
// copybook it was given would read as success.
type NoRecordError struct {
	// Path is the --copybook value as it was typed.
	Path string
}

// Error implements error.
func (e *NoRecordError) Error() string { return e.Diagnostic().String() }

// Diagnostic implements [diag.Error].
func (e *NoRecordError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: "this copybook declares no named 01-level, so there is no record to derive from it",
		Spans:   []diag.Span{{File: e.Path}},
	}
}

// DestinationError is a --out path with something already at it.
//
// docs/cli/SPEC.md, "Where the scaffold is written, and why nothing is ever
// overwritten": whatever is there — a regular file, a directory, a symbolic
// link, including one that dangles — the run fails and nothing is written. Both
// paths are named because a relative path in a shared tree sends a reader to the
// wrong directory, and the path as typed is the one they can find in what they
// wrote.
type DestinationError struct {
	// Path is the --out value as it was typed, and Absolute the path cpybkc
	// looked at.
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
	// Path is the --out value as it was typed, or [Stdout].
	Path string

	// Err is what the write failed with.
	Err error
}

// Error implements error.
func (e *WriteError) Error() string { return e.Diagnostic().String() }

// Unwrap returns the underlying fault.
func (e *WriteError) Unwrap() error { return e.Err }

// Diagnostic implements [diag.Error].
//
// Standard output is named in the message rather than led with as a file,
// because "-" is not a place a reader can go and look at.
func (e *WriteError) Diagnostic() diag.Diagnostic {
	if e.Path == Stdout {
		return diag.Diagnostic{
			Message: "the scaffold could not be written to standard output",
			Spans:   []diag.Span{{Note: e.Err.Error()}},
		}
	}

	return diag.Diagnostic{
		Message: "the scaffold could not be written here",
		Spans:   []diag.Span{{File: e.Path}, {Note: e.Err.Error()}},
	}
}
