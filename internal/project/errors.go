// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package project

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// errNoEntries is a copybook that parsed and holds no data description entries
// at all.
//
// It is a sentinel rather than a type because there is nothing to carry: the
// file, the path and where the layout named it are already on the
// [CopybookSourceError] wrapping it.
var errNoEntries = errors.New("it holds no data description entries")

// MissingLayoutError is a manifest naming a layout that cannot be read.
//
// It carries the two paths docs/cli/SPEC.md requires of the copybook diagnostic
// it is modelled on, and for the same reason: the path **as the manifest spells
// it** is what the adopter can find in their file, and the absolute path cpybkc
// opened is the "where it was looked for" without which a relative path sends a
// reader to the wrong directory. A manifest's paths are relative to the
// manifest, so the two differ exactly when the manifest was not in the
// directory the command was run from — which is the case a reader needs the
// second line for.
//
// The absolute path is a continuation line naming no place rather than a second
// span. There is no file to point into: the fault is that there is not one.
type MissingLayoutError struct {
	// Path is the layout's path as the manifest spells it.
	Path string

	// LookedIn is the absolute path cpybkc opened.
	LookedIn string

	// Err is what the read failed with.
	Err error
}

// Error implements the error interface.
func (e *MissingLayoutError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *MissingLayoutError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("there is no layout to read at %s", strconv.Quote(e.Path)),
		Spans:   []diag.Span{{}, {Note: "looked for " + e.LookedIn}},
	}
}

// Unwrap gives up the read failure.
func (e *MissingLayoutError) Unwrap() error { return e.Err }

// CopybookError is a copybook that cannot be read, with where cpybkc looked for
// it.
//
// It is a wrapper rather than a second copybook error because the two halves
// belong to two places.
// [github.com/Zaba505/cpybkc/internal/diag.MissingCopybookError] is what the
// layout says — the path as it spells it, under the `copybook` child that
// spells it — and is raised wherever a layout is read; where that path was
// looked for is the CLI's, because resolving it is, and a diagnostic naming a
// path the adopter never wrote sends them looking for a file they have not got.
//
//	error: orders.sexpr:14:5: there is no copybook to read at "cpy/orders.cpy"
//	  looked for /srv/orders/cpy/orders.cpy
//
// The absolute path is on a continuation line naming no place rather than in a
// second span, which docs/layout/SPEC.md requires of this diagnostic by name:
// there is no file to point into, and a span invented for one would point at
// nothing.
type CopybookError struct {
	// Err is what the layout says.
	Err *diag.MissingCopybookError

	// LookedIn is the absolute path cpybkc opened.
	LookedIn string
}

// Error implements the error interface.
func (e *CopybookError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *CopybookError) Diagnostic() diag.Diagnostic {
	d := e.Err.Diagnostic()
	d.Spans = append(d.Spans, diag.Span{Note: "looked for " + e.LookedIn})

	return d
}

// Unwrap gives up what the layout says, so that a caller asserting against
// [github.com/Zaba505/cpybkc/internal/diag.MissingCopybookError] finds it
// through this.
func (e *CopybookError) Unwrap() error { return e.Err }

// CopybookSourceError is a copybook that is there and is not one this build can
// read.
//
// It is separate from the copybook not being there because the fixes are
// different: one is a path, and the other is a file whose COBOL the parser
// stopped at. The cause is kept rather than reworded, since what `cobol-go`
// found is more specific than anything this package could say about it, and it
// is reported on continuation lines so that a fault cannot put a line on
// standard error that no severity opens.
type CopybookSourceError struct {
	// Pos is the `copybook` child in the layout.
	Pos diag.Span

	// Path is the copybook's path as the layout spells it.
	Path string

	// Err is what reading it failed with.
	Err error
}

// Error implements the error interface.
func (e *CopybookSourceError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *CopybookSourceError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("the copybook %s could not be read", strconv.Quote(e.Path)),
		Spans:   []diag.Span{e.Pos, {Note: e.Err.Error()}},
	}
}

// Unwrap gives up the reading failure.
func (e *CopybookSourceError) Unwrap() error { return e.Err }

// UnknownItemError is an item reference naming an item its copybook does not
// declare.
//
// It is the cross-file diagnostic docs/layout/SPEC.md requires by name: one span
// into the layout at the reference, and one into the copybook at the group that
// was supposed to hold the item, carrying what that group does declare — because
// an absent item has no position, and the list an adopter needs in order to fix
// the reference is the list of the ones they could have meant.
type UnknownItemError struct {
	// Pos is the `(item …)` form in the layout.
	Pos diag.Span

	// Ref is the whole reference, which is what the message names: a path is
	// complete rather than qualified, so quoting the last name alone would
	// leave the reader to work out which of several like it was meant.
	Ref layoutmodel.ItemRef

	// Name is the name in the path that resolved to nothing.
	Name string

	// Path is the copybook's path as the layout spells it.
	Path string

	// Parent is the item the name was looked for under.
	Parent *copybook.Field

	// Declares are the names that item does declare, in source order.
	Declares []string
}

// Error implements the error interface.
func (e *UnknownItemError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
//
// The second span's note is built here rather than taken from whoever raised
// it, for the reason
// [github.com/Zaba505/cpybkc/internal/diag.UndeclaredItemError]'s is: what the
// copybook half of the message says is then a property of the error and not of
// what a caller remembered to write.
func (e *UnknownItemError) Diagnostic() diag.Diagnostic {
	under := diag.Span{
		File:   e.Path,
		Line:   e.Parent.Pos.Line,
		Column: e.Parent.Pos.Column,
	}

	switch {
	case len(e.Declares) == 0:
		under.Note = "it declares nothing below it"
	default:
		under.Note = "it declares " + and(e.Declares)
	}

	return diag.Diagnostic{
		Message: fmt.Sprintf("%s names no item: %s declares no %s",
			e.Ref, strconv.Quote(e.Parent.Name), strconv.Quote(e.Name)),
		Spans: []diag.Span{e.Pos, under},
	}
}

// AmbiguousItemError is an item reference matching two items under one parent.
//
// Duplicate data names are legal COBOL, so a copybook may declare two items of
// one name under one group, and a complete path does not tell them apart —
// docs/layout/SPEC.md's reference grammar has one name per level and no way to
// say which of two. Answering with the first would put a discriminator, a
// rename or an encoding override on whichever item the walk met first, silently,
// so the reference is refused and every match is named.
type AmbiguousItemError struct {
	// Pos is the `(item …)` form in the layout.
	Pos diag.Span

	// Ref is the whole reference.
	Ref layoutmodel.ItemRef

	// Name is the name in the path that matched more than one item.
	Name string

	// Path is the copybook's path as the layout spells it.
	Path string

	// Matches is where each item it matched is declared.
	Matches []diag.Span
}

// Error implements the error interface.
func (e *AmbiguousItemError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *AmbiguousItemError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("%s names %d items of the copybook %s, and a reference names one",
			e.Ref, len(e.Matches), strconv.Quote(e.Path)),
		Spans: append([]diag.Span{e.Pos}, e.Matches...),
	}
}

// AlternativesError is a `record` bound to an `01`-level that resolves to more
// than one record type.
//
// A copybook holding a REDEFINES outside a repeating group describes one run of
// bytes several ways, and `resolve` reads it the way docs/ir/SPEC.md says: one
// record type per combination of alternatives. Nothing in the layout format
// carries that across. A `record` form binds a name to an `01`-level and every
// alternative of that level is the same level, so there is no spelling that
// gives two of them two names and no rule saying which alternative a form meant.
//
// #164 is the story that decides both, and until it does this is refused rather
// than answered by a rule this program invented — pairing the forms to the
// alternatives by position is a rule an adopter could not read anywhere, and one
// they would only find out about from generated code naming the wrong fields.
type AlternativesError struct {
	// Pos is the `record` form.
	Pos diag.Span

	// Record is the name it defines.
	Record string

	// Path is the copybook's path as the layout spells it.
	Path string

	// Item is the top-level item it is bound to.
	Item string

	// Alternatives is how many record types the item resolved to.
	Alternatives int
}

// Error implements the error interface.
func (e *AlternativesError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *AlternativesError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"record %s is bound to %s in %s, which REDEFINES describes %d ways, "+
				"and nothing in this layout says which of them the record is",
			strconv.Quote(e.Record), strconv.Quote(e.Item), strconv.Quote(e.Path), e.Alternatives),
		Spans: []diag.Span{
			e.Pos,
			{Note: "a record form names an 01-level, and every alternative of it is that same level; " +
				"naming and selecting one is #164"},
		},
	}
}

// GeneratorError is a generator the manifest names that could not be resolved to
// an executable.
//
// It is a wrapper for [CopybookError]'s reason, from the other side: what went
// wrong is [github.com/Zaba505/cpybkc/internal/plugin]'s and is already a
// diagnostic naming the file it looked for and every directory it looked in,
// and what this adds is where the name was written — the line of the manifest
// the adopter has to edit, which is the one thing the search cannot know.
type GeneratorError struct {
	// Pos is the generator's entry in the manifest.
	Pos diag.Span

	// Name is the generator's name, as the manifest spells it.
	Name string

	// Err is what the search failed with.
	Err error
}

// Error implements the error interface.
func (e *GeneratorError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
//
// The search's own first span names nowhere — "the fault is in the machine and
// not in a file" — and this puts the manifest entry there instead of beside it,
// because a diagnostic leads with where the fault is and the place an adopter
// acts on it is the entry.
func (e *GeneratorError) Diagnostic() diag.Diagnostic {
	var carried diag.Error
	if !errors.As(e.Err, &carried) {
		return diag.Diagnostic{Message: e.Err.Error(), Spans: []diag.Span{e.Pos}}
	}

	d := carried.Diagnostic()

	if len(d.Spans) > 0 && !d.Spans[0].Stated() {
		d.Spans[0] = e.Pos

		return d
	}

	d.Spans = append([]diag.Span{e.Pos}, d.Spans...)

	return d
}

// Unwrap gives up the search failure.
func (e *GeneratorError) Unwrap() error { return e.Err }

// and joins a list the way a sentence does, so that a message naming what a
// group declares reads as one.
func and(items []string) string {
	switch len(items) {
	case 0:
		return "nothing"
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		last := len(items) - 1

		joined := ""
		for _, item := range items[:last] {
			joined += item + ", "
		}

		return joined + "and " + items[last]
	}
}
