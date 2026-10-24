// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/Zaba505/cpybkc/internal/copymember"
	"github.com/Zaba505/cpybkc/internal/diag"
	scaffolding "github.com/Zaba505/cpybkc/internal/scaffold"
)

// scaffold is `cpybkc init`: the copybooks the line named, read, transcribed
// into a definitions file each, and the layout scaffold they decide, written
// where --out says with the definitions files beside it.
//
// # What is here, and what is not
//
// This stage opens the files, refuses the ones that are not copybooks it can be
// handed, reports what a derivation is owed on standard error, and hands the
// set of files to their destinations. What a copybook *decides* is not here:
// the transcription and its read-back, the record per 01-level, the
// alternative per REDEFINES outside a repeating group, the commented forms
// whose subject is computable and whose value is not, and the set of
// destinations that is never overwritten and is written whole or not at all are
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
// `init` writes nothing to standard output. Its output is a set of files, and
// `--out -` is a usage error, so there is no spelling under which any of them
// reaches the data channel. Everything this stage says is a diagnostic on
// standard error, including the notes it owes: an 01-level resolving to more
// than one record type is the size of the work in front of the reader, and
// docs/cli/SPEC.md holds every line cpybkc writes to being something they can
// act on.
//
// So do the notes about what was not written. A record type no layout could
// carry — an unnamed alternative, a name two record types derive, an 01-level
// the resolver cannot describe, a name the S-expression grammar would not read
// back — is left out of the scaffold and noted, and the run still exits 0: each
// is a statement about the layout rather than about the copybook, and the rest
// of the scaffold and every definitions file are no less written for it
// (docs/cli/SPEC.md, "How a combination's record name is chosen"). So is a
// copybook no record derives from, which is still transcribed.
//
// The notes go through the same log a generator's lines reach that stream
// through, at the level docs/plugin/SPEC.md maps `note:` to, because that log is
// where the one place that knows which writer standard error is put it. A line
// through it carrying no generator name is cpybkc's own, which is what
// docs/cli/SPEC.md makes the absence of a name mean.
func scaffold(ctx context.Context, inv invocation, log *slog.Logger) error {
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

	// A member's path is resolved against the working directory, as every
	// path typed on the command line is, and named as typed in every
	// diagnostic about it.
	members := make([]copymember.Form, 0, len(inv.copyMembers))
	for _, given := range inv.copyMembers {
		members = append(members, copymember.Form{Member: given.member, Path: given.path, File: given.path, Given: given.value})
	}

	derived, err := scaffolding.Derive(books, members...)
	if err != nil {
		// A refusal still owes the notes beside it: a --copy-member no COPY
		// named reads as the misspelling it most likely is next to the COPY
		// that was refused for want of it.
		var noted scaffolding.Noted
		if errors.As(err, &noted) {
			for _, note := range noted.Notes() {
				log.InfoContext(ctx, note)
			}
		}

		return err
	}

	for _, note := range derived.Notes() {
		log.InfoContext(ctx, note)
	}

	// A run cancelled between deriving and writing is a cancelled run and not a
	// written scaffold. Every file has been derived and read back by here,
	// which is what makes "no partial set" a property of the writer alone:
	// there is nothing to write until there is all of it.
	if err := ctx.Err(); err != nil {
		return cancelled(ctx, err)
	}

	// Beside the layout means in the directory --out names it in. The path is
	// joined rather than resolved, so a diagnostic about a definitions file
	// names it as it follows from what was typed.
	dir := filepath.Dir(inv.out)

	files := derived.Definitions()
	beside := make([]scaffolding.Output, 0, len(files))

	for _, file := range files {
		beside = append(beside, scaffolding.Output{Path: filepath.Join(dir, file.Name), Bytes: file.Bytes})
	}

	err = scaffolding.Write(ctx, scaffolding.Output{Path: inv.out, Bytes: derived.Bytes()}, beside)

	// The writer has already taken back whatever it linked; what is left is to
	// say the run was cancelled rather than that a write failed.
	if ctxErr := ctx.Err(); err != nil && ctxErr != nil && errors.Is(err, ctxErr) {
		return cancelled(ctx, err)
	}

	return err
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
