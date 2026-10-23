// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package project

import (
	"bytes"
	"errors"
	"os"
	"slices"

	cobol "github.com/Zaba505/cobol-go"
	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/definitions"
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
	"github.com/Zaba505/cpybkc/internal/layoutschema"
)

// described is one definitions file, read: the entries it holds, or why it
// holds none.
//
// It is [source]'s counterpart for the other kind of file, kept for the same
// reason — a file is read once however many records name it — and it holds
// three kinds of failure apart, because each is reported differently:
//
//   - a file that is not there is about the line of the layout naming it, and
//     is reported once per record, as a missing copybook is;
//   - a file holding no entry at all is a layout, or nothing, named where a
//     definitions file belongs, and is also about the line naming it;
//   - a fault inside the file is about the file, and is reported once
//     however many records name it, with a span at each of theirs
//     (docs/layout/SPEC.md, "Who reports a fault in an entry body").
type described struct {
	// entries are the file's entries, built and judged, nil where reading it
	// failed.
	entries []*cobol.DataDescriptionEntry

	// missing is why the file could not be opened, nil where it could.
	missing error

	// empty reports that the file's top level holds no entry, and first is
	// where its first form is — or the file alone, where it holds none.
	empty bool
	first diag.Span

	// faults are the faults inside it, each one on its own, in the order the
	// checks found them.
	faults []error
}

// readDefinitions opens one definitions file and reads it as far as it reads.
//
// The stages are a layout's, in a layout's order, and each stops the file where
// it reports anything — docs/layout/SPEC.md's rule that a document a check has
// refused is read no further, applied per document (#397):
//
//  1. the grammar, which a layout is held to first as well;
//  2. whether the top level holds an entry at all, which is how a layout
//     reached through `definitions` is told apart from a definitions file
//     before the schema describes it as a hundred misplaced forms;
//  3. the body check (#400) — the published schema's declarations for one
//     block's body;
//  4. the entry reader and the judge (#401);
//  5. `copybook.Build`, once, so that a fault in how the entries nest is
//     reported once for the file rather than once per record building a
//     tree out of it.
//
// path is the file as the run opened it, and it is the name every position in
// the file carries: docs/layout/SPEC.md requires a span into a definitions file
// to name the file that way, so that a reader holding the diagnostic opens the
// file the run read.
func readDefinitions(path string, spelled string) described {
	src, err := os.ReadFile(path)
	if err != nil {
		return described{missing: err}
	}

	file, err := layout.Parse(path, bytes.NewReader(src))
	if err != nil {
		return described{faults: split(err)}
	}

	published, err := layoutschema.Published()
	if err != nil {
		return described{faults: []error{err}}
	}

	if first, empty := holdsNoEntry(file, published.BodyForms()); empty {
		return described{empty: true, first: first}
	}

	diagnostics, err := published.ValidateDefinitions(path, bytes.NewReader(src))
	if err != nil {
		return described{faults: []error{err}}
	}

	if len(diagnostics) > 0 {
		return described{faults: split(layoutschema.Err(diagnostics))}
	}

	entries, err := definitions.Read(definitions.FileBody(file))
	if err != nil {
		return described{faults: split(err)}
	}

	built, err := definitions.Judge(entries)
	if err != nil {
		return described{faults: split(err)}
	}

	if _, err := copybook.Build(built); err != nil {
		return described{faults: []error{buildFault(path, spelled, err)}}
	}

	return described{entries: built}
}

// holdsNoEntry reports whether a file's top level holds none of the forms a
// definitions file holds, and where its first form is — the file alone where
// it has none.
func holdsNoEntry(file *layout.File, body []string) (diag.Span, bool) {
	for _, form := range file.Forms {
		if slices.Contains(body, form.Tag) {
			return diag.Span{}, false
		}
	}

	if len(file.Forms) == 0 {
		return diag.Span{File: file.Name}, true
	}

	return span(file.Forms[0].TagPos), true
}

// split is each fault in err on its own, so that each can be reported with the
// records naming the file it is in.
func split(err error) []error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var faults []error
		for _, e := range joined.Unwrap() {
			faults = append(faults, split(e)...)
		}

		return faults
	}

	return []error{err}
}

// buildFault is what `cobol-go` said, building or laying out a definitions file's
// entries, as a fault at its position in that file.
//
// `cobol-go`'s errors carry a line and a column and no file, because
// `cobol.Pos` names none; the entries were built with the positions of the
// forms they came from, so the line and the column are the definitions file's,
// and the file is the one this run opened. Each error type that carries a
// position is named, and found with [errors.As], because nothing in `cobol-go`
// lets a position be asked for generically yet (Zaba505/cobol-go#137). Any
// other error has no position to point at, and is reported at the record's
// `definitions` source instead, with `cobol-go`'s message as a note.
func buildFault(path, spelled string, err error) *DefinitionsBuildError {
	fault := &DefinitionsBuildError{Path: spelled, Err: err}

	if pos, ok := cobolPos(err); ok && pos.Line > 0 {
		fault.Pos = diag.Span{File: path, Line: pos.Line, Column: pos.Column}
	}

	return fault
}

// cobolPos is the position a `cobol-go` build or layout error carries, for the
// types that carry one.
func cobolPos(err error) (cobol.Pos, bool) {
	for _, as := range []func(error) (cobol.Pos, bool){
		positioned[copybook.LevelError](func(e copybook.LevelError) cobol.Pos { return e.Pos }),
		positioned[copybook.LevelSequenceError](func(e copybook.LevelSequenceError) cobol.Pos { return e.Pos }),
		positioned[copybook.RenamesError](func(e copybook.RenamesError) cobol.Pos { return e.Pos }),
		positioned[copybook.PictureError](func(e copybook.PictureError) cobol.Pos { return e.Pos }),
		positioned[copybook.UsageError](func(e copybook.UsageError) cobol.Pos { return e.Pos }),
		positioned[copybook.LayoutError](func(e copybook.LayoutError) cobol.Pos { return e.Pos }),
		positioned[copybook.OccursError](func(e copybook.OccursError) cobol.Pos { return e.Pos }),
		positioned[copybook.RedefinesError](func(e copybook.RedefinesError) cobol.Pos { return e.Pos }),
		positioned[copybook.OccurrenceError](func(e copybook.OccurrenceError) cobol.Pos { return e.Pos }),
		positioned[copybook.DependingError](func(e copybook.DependingError) cobol.Pos { return e.Pos }),
	} {
		if pos, ok := as(err); ok {
			return pos, true
		}
	}

	return cobol.Pos{}, false
}

// positioned finds an error of type T in err and reads its position.
func positioned[T error](pos func(T) cobol.Pos) func(error) (cobol.Pos, bool) {
	return func(err error) (cobol.Pos, bool) {
		var found T
		if !errors.As(err, &found) {
			return cobol.Pos{}, false
		}

		return pos(found), true
	}
}

// fileFaults is every fault reported inside definitions files, by the file and
// by what it says, so that a fault found once per record — a layout fault in
// an `01`-level two records bind — is reported once, naming both.
type fileFaults map[fileFaultKey]*DefinitionsFaultError

// fileFaultKey is a fault's identity for that purpose: the file it is in, and
// what it renders as.
type fileFaultKey struct {
	file     string
	rendered string
}

// report reports fault, inside the definitions file at path, as found by a
// record whose `definitions` source is at — or adds that record to the one
// already reported, where the same fault was found before.
func (f fileFaults) report(faults *diag.List, path string, fault error, named Naming) {
	key := fileFaultKey{file: path, rendered: fault.Error()}

	if already, ok := f[key]; ok {
		if !slices.Contains(already.NamedBy, named) {
			already.NamedBy = append(already.NamedBy, named)
		}

		return
	}

	reported := &DefinitionsFaultError{Err: fault, NamedBy: []Naming{named}}
	f[key] = reported
	faults.Fail(reported)
}
