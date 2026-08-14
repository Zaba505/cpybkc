// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/Zaba505/cpybkc/internal/diag"
	scaffolding "github.com/Zaba505/cpybkc/internal/scaffold"
)

// scaffold is `cpybkc init`: the copybooks the line named, read, and the layout
// scaffold they decide, written where --out says.
//
// # What is here, and what is not
//
// This stage opens the files, refuses the ones that are not copybooks it can be
// handed, reports what a derivation is owed on standard error, and hands the
// bytes to a destination. What a copybook *decides* is not here: the record per
// 01-level, the alternative per REDEFINES outside a repeating group, the
// commented forms whose subject is computable and whose value is not, and the
// destination that is never overwritten are
// [github.com/Zaba505/cpybkc/internal/scaffold]'s, and the REDEFINES reading
// underneath them is [github.com/Zaba505/cpybkc/internal/resolve]'s — the same
// one a layout is resolved through, so that a copybook has one reading in this
// tool rather than two.
//
// The package is imported under a name of its own because this function already
// has the obvious one. Which is the right way round: `init` is what a person
// types, so it is what the command's own file is called, and the package it
// calls is a detail of how.
//
// # Why a directory is a status 1 and not a 2
//
// A --copybook value naming a directory fails the run rather than the vector.
// It is not decidable from the line — cpybkc has to look at the path to learn
// what is there — and docs/cli/SPEC.md draws the statuses on exactly that line:
// a 2 promises that cpybkc did nothing at all, and by the time a directory is
// known to be one, files have been opened. It is stated apart from a copybook
// that cannot be opened because a directory opens perfectly well, and reading
// one would make the scaffold a function of a directory's contents — a copybook
// dropped in beside the others later changing what `init` produces, with no file
// recording the difference.
//
// # The two streams
//
// The scaffold is data and reaches standard output when, and only when, `--out
// -` asked for it there. Everything else this stage says is a diagnostic on
// standard error, including the notes it owes: an 01-level resolving to more
// than one record type is the size of the work in front of the reader, and
// docs/cli/SPEC.md holds every line cpybkc writes to being something they can
// act on.
//
// The notes go through the same log a generator's lines reach that stream
// through, at the level docs/plugin/SPEC.md maps `note:` to, because that log is
// where the one place that knows which writer standard error is put it. A line
// through it carrying no generator name is cpybkc's own, which is what
// docs/cli/SPEC.md makes the absence of a name mean.
func scaffold(ctx context.Context, inv invocation, stdout io.Writer, log *slog.Logger) error {
	// A run cancelled before it starts is a cancelled run. The check is here
	// for the reason it is on the emitting path: there is no output tree to
	// leave alone, but there is a file about to appear at a path somebody
	// named, and a signal that arrived first is the answer they are owed rather
	// than whatever the stage would otherwise have said.
	if err := ctx.Err(); err != nil {
		return cancelled(ctx, err)
	}

	books, err := copybooks(inv.copybooks)
	if err != nil {
		return err
	}

	derived, err := scaffolding.Derive(books)
	if err != nil {
		return err
	}

	for _, note := range derived.Notes() {
		log.InfoContext(ctx, note)
	}

	// A run cancelled between deriving and writing is a cancelled run and not a
	// written scaffold. The whole file has been derived by here, which is what
	// makes "no partial file" and "no partial output" one property rather than
	// two: there is nothing to write until there is all of it.
	if err := ctx.Err(); err != nil {
		return cancelled(ctx, err)
	}

	return scaffolding.Write(inv.out, stdout, derived.Bytes())
}

// copybooks opens every file the line named, in the order it named them.
//
// Every fault is reported rather than the first. A person adopting cpybkc types
// a `--copybook` per file in one line, and mistyping two of them is two things
// to fix rather than two runs.
func copybooks(paths []string) ([]scaffolding.Copybook, error) {
	var faults diag.List

	books := make([]scaffolding.Copybook, 0, len(paths))

	for _, path := range paths {
		// Stat rather than opening and finding out, because a directory opens
		// perfectly well and reading one yields a fault about the read rather
		// than about what was named.
		info, err := os.Stat(path)
		if err != nil {
			faults.Fail(&unopenableCopybookError{Path: path, Err: err})

			continue
		}

		if info.IsDir() {
			faults.Fail(&copybookDirectoryError{Path: path})

			continue
		}

		source, err := os.ReadFile(path)
		if err != nil {
			faults.Fail(&unopenableCopybookError{Path: path, Err: err})

			continue
		}

		books = append(books, scaffolding.Copybook{Path: path, Source: source})
	}

	if faults.Failed() {
		return nil, faults.Err()
	}

	return books, nil
}

// unopenableCopybookError is a --copybook value that could not be read.
//
// The path is named as it was typed, which is what the person running the
// command can go and correct: there is no layout under `init` for a second
// spelling to have come from.
type unopenableCopybookError struct {
	Path string
	Err  error
}

// Error implements error.
func (e *unopenableCopybookError) Error() string { return e.Diagnostic().String() }

// Unwrap returns the underlying fault.
func (e *unopenableCopybookError) Unwrap() error { return e.Err }

// Diagnostic implements [diag.Error].
func (e *unopenableCopybookError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("failed to read the copybook %s: %v", e.Path, e.Err),
		Spans:   []diag.Span{{File: e.Path}},
	}
}

// copybookDirectoryError is a --copybook value naming a directory.
//
// There is no extension convention to fall back on — `.cpy`, `.cbl`, `.cob` and
// no extension at all are all in use — so any rule for which files in a
// directory are copybooks is one cpybkc invented and no adopter can predict. A
// shell already expands a glob into the flags, in a line a reviewer can read,
// which is what the message points at.
type copybookDirectoryError struct {
	Path string
}

// Error implements error.
func (e *copybookDirectoryError) Error() string { return e.Diagnostic().String() }

// Diagnostic implements [diag.Error].
func (e *copybookDirectoryError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("%s is a directory, and %s takes one copybook per %s; "+
			"expand the directory in your shell if you meant every file in it", e.Path, copybookFlag, copybookFlag),
		Spans: []diag.Span{{File: e.Path}},
	}
}
