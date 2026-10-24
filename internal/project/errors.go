// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package project

import (
	"io/fs"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/copymember"
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
	"github.com/Zaba505/cpybkc/internal/resolve"
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

	// In is where in a member the fault is, for one `cobol-go` raised inside
	// text a `COPY` brought in, with the chain of `COPY` statements that
	// reached it; nil for a fault in the copybook's own text.
	In *diag.Span
}

// Error implements the error interface.
func (e *CopybookSourceError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
//
// A fault inside a member points into the member, at the line and column it
// has there, and names the member as its `copy-member` form spells it, with
// one more continuation line per `COPY` statement that reached it, innermost
// first (docs/cli/SPEC.md, "What a `COPY` fault looks like"): a line and a
// column alone, in `cobol-go`'s message, would be read as a place in the
// copybook the layout names, which says something else there.
func (e *CopybookSourceError) Diagnostic() diag.Diagnostic {
	cause := diag.Span{Note: e.Err.Error()}
	if e.In != nil {
		cause = *e.In
		cause.Note = e.Err.Error()
	}

	return diag.Diagnostic{
		Message: fmt.Sprintf("the copybook %s could not be read", strconv.Quote(e.Path)),
		Spans:   []diag.Span{e.Pos, cause},
	}
}

// Unwrap gives up the reading failure.
func (e *CopybookSourceError) Unwrap() error { return e.Err }

// MissingCopyMemberError is a member file a `copy-member` form names that cannot
// be opened, in the shape docs/cli/SPEC.md's "Finding the inputs" gives every
// missing file: the path as the form spells it, the span at that path in the
// layout, and where it was looked for on a continuation line naming no place.
type MissingCopyMemberError struct {
	// Pos is the form's path, in the layout.
	Pos diag.Span

	// Path is the path as the form spells it.
	Path string

	// LookedIn is the absolute path cpybkc opened.
	LookedIn string

	// Err is what opening it failed with.
	Err error
}

// Error implements the error interface.
func (e *MissingCopyMemberError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
//
// A file that is there and cannot be read — a directory, a file without
// permission, a failed read — is said to be that, with what reading it said,
// rather than missing: a reader told a file is missing goes and finds it where
// the message says it is not.
func (e *MissingCopyMemberError) Diagnostic() diag.Diagnostic {
	if e.Err != nil && !errors.Is(e.Err, fs.ErrNotExist) {
		return diag.Diagnostic{
			Message: "the copy member at " + e.Path + " could not be read",
			Spans:   []diag.Span{e.Pos, {Note: "looked for " + e.LookedIn}, {Note: e.Err.Error()}},
		}
	}

	return diag.Diagnostic{
		Message: "no copy member at " + e.Path,
		Spans:   []diag.Span{e.Pos, {Note: "looked for " + e.LookedIn}},
	}
}

// MemberFaultError is a fault `cobol-go` found laying out an entry a member
// contributed to a record bound to a copybook. It points into the member, at
// the entry's line and column there, with one continuation line per `COPY`
// that reached it (docs/cli/SPEC.md, "What a `COPY` fault looks like"): a line
// and a column alone would be read as a place in the copybook the layout
// names, which says something else there. A fault in the copybook's own text
// is reported as `cobol-go` wrote it, as it always has been.
type MemberFaultError struct {
	// At is the entry, in the member.
	At diag.Span

	// Err is what `cobol-go` said.
	Err error
}

// Error implements the error interface.
func (e *MemberFaultError) Error() string { return e.Diagnostic().String() }

// Unwrap gives up `cobol-go`'s fault.
func (e *MemberFaultError) Unwrap() error { return e.Err }

// Diagnostic is what the error says, and where.
func (e *MemberFaultError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{Message: e.Err.Error(), Spans: []diag.Span{e.At}}
}

// Unwrap gives up the open failure.
func (e *MissingCopyMemberError) Unwrap() error { return e.Err }

// UnreachedCopyMemberError is a `copy-member` form no `COPY` reaches, reported
// at its member (docs/layout/SPEC.md, "A `COPY` member is found where the
// layout says"): a member map entry nothing reads is more often a misspelled
// member than one kept for later. Its file is not opened.
type UnreachedCopyMemberError struct {
	// Pos is the form's member.
	Pos diag.Span

	// Member is the member.
	Member copymember.Member
}

// Error implements the error interface.
func (e *UnreachedCopyMemberError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *UnreachedCopyMemberError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("no COPY in a copybook a record names reaches the copy member %s", e.Member),
		Spans:   []diag.Span{e.Pos},
	}
}

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

	// Source is where the record's description is held.
	Source resolve.Source

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
	under := e.Source.Span(e.Parent)

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

	// Source is where the record's description is held.
	Source resolve.Source

	// Matches is where each item it matched is declared.
	Matches []diag.Span
}

// Error implements the error interface.
func (e *AmbiguousItemError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *AmbiguousItemError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("%s names %d items of %s, and a reference names one",
			e.Ref, len(e.Matches), e.Source),
		Spans: append([]diag.Span{e.Pos}, e.Matches...),
	}
}

// AlternativesError is a `record` form whose `alternative` children choose no
// combination of alternatives its copybook actually has.
//
// A copybook holding a REDEFINES outside a repeating group describes one run of
// bytes several ways, and `resolve` reads it the way docs/ir/SPEC.md says: one
// record type per combination of alternatives. Which of them a `record` form
// means is the layout's own statement — one `alternative` child per redefine,
// naming the alternative chosen (docs/layout/SPEC.md, "Which alternative a
// record is") — and this is the choice not landing on one.
//
// It covers every way that happens, because they are one fault from an
// adopter's side and one message answers all of them: a record over a redefined
// `01`-level carrying no children at all, one carrying too few or too many, one
// naming an item that is an alternative of a redefine *inside* a repeating group
// or no alternative at all, and one carrying a child over a copybook with no
// redefine in it. What the message does in each case is name what was chosen and
// list every combination there was to choose from, so that the fix is readable
// off the diagnostic rather than out of the copybook.
type AlternativesError struct {
	// Pos is the `record` form.
	Pos diag.Span

	// Record is the name it defines.
	Record string

	// Source is where the record's description is held.
	Source resolve.Source

	// Item is the top-level item it is bound to.
	Item string

	// Chosen is what the `alternative` children named, in the order the form
	// writes them.
	Chosen []string

	// Resolved is every combination the copybook resolved to, in the order
	// `resolve` enumerated them.
	Resolved [][]string
}

// Error implements the error interface.
func (e *AlternativesError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *AlternativesError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"record %s is bound to %s in %s, and chooses %s, which is none of the %d it describes",
			strconv.Quote(e.Record), strconv.Quote(e.Item), e.Source,
			chosenAs(e.Chosen), len(e.Resolved)),
		Spans: []diag.Span{
			e.Pos,
			{Note: "it describes " + offered(e.Resolved) +
				"; a record form carries one (alternative <item-ref>) child per REDEFINES outside a " +
				"repeating group, naming the alternative it is"},
		},
	}
}

// chosenAs is how a diagnostic reads back what a record chose.
func chosenAs(chosen []string) string {
	if len(chosen) == 0 {
		return "no alternative"
	}

	return strings.Join(quoteAll(chosen), " with ")
}

// offered is how a diagnostic lists the combinations there were to choose from.
//
// Each combination is one choice however many items it took, so they are
// rendered as one entry apiece rather than flattened — a copybook with two
// redefines offers four choices of two items and not eight of one.
func offered(resolved [][]string) string {
	if len(resolved) == 0 {
		return "nothing to choose"
	}

	entries := make([]string, 0, len(resolved))

	for _, combination := range resolved {
		if len(combination) == 0 {
			entries = append(entries, "no alternative")

			continue
		}

		entries = append(entries, strings.Join(quoteAll(combination), " with "))
	}

	return strings.Join(entries, ", ")
}

// quoteAll quotes every name of a combination, so that an item called `with`
// cannot read as the word joining two of them.
func quoteAll(names []string) []string {
	quoted := make([]string, 0, len(names))

	for _, name := range names {
		quoted = append(quoted, strconv.Quote(name))
	}

	return quoted
}

// DefinitionsFileAsLayoutError is a manifest's `layout` naming a definitions
// file: a document whose every top-level form is an entry.
//
// docs/layout/SPEC.md's "How a definitions file and a layout are told apart"
// asks for this one diagnostic, with a span at the manifest's `layout` and one
// at the file's first entry, and nothing else about the file. The schema check
// would report the entries as forms a layout does not carry at its top level,
// which is accurate about the wrong file and says nothing about the mistake:
// the manifest names the wrong kind of document.
type DefinitionsFileAsLayoutError struct {
	// Layout is the manifest's `layout` value.
	Layout diag.Span

	// Path is the layout's path as the manifest spells it.
	Path string

	// First is the file's first entry.
	First diag.Span
}

// Error implements the error interface.
func (e *DefinitionsFileAsLayoutError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *DefinitionsFileAsLayoutError) Diagnostic() diag.Diagnostic {
	first := e.First
	first.Note = "its top-level forms are entries and nothing else, beginning here"

	return diag.Diagnostic{
		Message: fmt.Sprintf("the manifest's layout, %s, is a definitions file rather than a layout; "+
			"a definitions file is named by a record, with (definitions %s <top-level-name>)",
			strconv.Quote(e.Path), strconv.Quote(e.Path)),
		Spans: []diag.Span{e.Layout, first},
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

		// The serial comma is deliberate: "a, b, and c" rather than "a, b and
		// c", so that a two-word item cannot read as two items.
		return strings.Join(items[:last], ", ") + ", and " + items[last]
	}
}

// MissingDefinitionsError is a record naming a definitions file that cannot be
// read.
//
// It is [CopybookError]'s shape for the other kind of file, which is what
// docs/layout/SPEC.md's "A copybook that is not there, and an item that is not
// in it" asks for — one shape whichever source a record names, so that a record
// moved from a copybook to a file fails the same way it failed before:
//
//	error: ledger.sexpr:22:5: there is no definitions file to read at "posting.cpy.sexpr"
//	  looked for /srv/ledger/posting.cpy.sexpr
//
// It carries one span, at the record's `definitions` source, and the absolute
// path on a continuation line naming no place: there is no file to point into,
// and a span invented for one would point at nothing. It is reported once per
// record naming the path, for the reason a missing copybook is — each is a line
// of the layout an adopter has to look at.
type MissingDefinitionsError struct {
	// Pos is the record's `definitions` source.
	Pos diag.Span

	// Path is the definitions file's path as the layout spells it.
	Path string

	// LookedIn is the absolute path cpybkc opened.
	LookedIn string

	// Err is what the read failed with.
	Err error
}

// Error implements the error interface.
func (e *MissingDefinitionsError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *MissingDefinitionsError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("there is no definitions file to read at %s", strconv.Quote(e.Path)),
		Spans:   []diag.Span{e.Pos, {Note: "looked for " + e.LookedIn}},
	}
}

// Unwrap gives up the read failure.
func (e *MissingDefinitionsError) Unwrap() error { return e.Err }

// NotDefinitionsError is a record naming, as a definitions file, a file whose
// top level holds no entry: a layout, or nothing at all, reached where a
// definitions file belongs.
//
// docs/layout/SPEC.md's "How a definitions file and a layout are told apart"
// asks for this one diagnostic, with a span at the record's `definitions`
// source and one at the named file's first form, and nothing else about the
// file. The body check would report every one of its forms as one a
// definitions file does not hold, which describes the wrong file accurately and
// the mistake — the record names the wrong kind of document — not at all.
type NotDefinitionsError struct {
	// Pos is the record's `definitions` source.
	Pos diag.Span

	// Record is the record's name.
	Record string

	// Path is the file's path as the layout spells it.
	Path string

	// First is the file's first form, or the file alone where it holds none.
	First diag.Span
}

// Error implements the error interface.
func (e *NotDefinitionsError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *NotDefinitionsError) Diagnostic() diag.Diagnostic {
	first := e.First
	if first.Line == 0 {
		first.Note = "it holds no form at all"
	} else {
		first.Note = "its top-level forms are not entries, beginning here"
	}

	return diag.Diagnostic{
		Message: fmt.Sprintf("record %s names %s as a definitions file, and it holds no entry: "+
			"a definitions file's top level is entry forms and nothing else, and a layout is named by the manifest",
			e.Record, strconv.Quote(e.Path)),
		Spans: []diag.Span{e.Pos, first},
	}
}

// DefinitionsBuildError is what `cobol-go` said building or laying out the
// entries of a definitions file or a block, at the position it carries.
//
// docs/layout/SPEC.md's promise for an entry body is that a mistake in it is
// reported at its line and column, which is better than a copybook's path
// gives: a PICTURE `cobol-go` cannot read, a level out of sequence, a REDEFINES
// of nothing. The position is in the definitions file for a file, and in the
// layout for a block. The message is `cobol-go`'s own, because what it found is
// more specific than anything this package could say about it, and the error
// is kept and reachable with [errors.As].
//
// An error of a type that carries no position has nowhere in the body to point,
// and Pos is then unstated: [DefinitionsFaultError] reports it at the record's
// `definitions` source, with `cobol-go`'s message as a note.
type DefinitionsBuildError struct {
	// Pos is where in the entry body the fault is — in the definitions file as
	// the run opened it, or in the layout for a block — or unstated where
	// `cobol-go` named no position.
	Pos diag.Span

	// Source is the definitions file or the block holding the entries.
	Source resolve.Source

	// Err is what `cobol-go` returned.
	Err error
}

// Error implements the error interface.
func (e *DefinitionsBuildError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *DefinitionsBuildError) Diagnostic() diag.Diagnostic {
	if e.Pos.Stated() {
		return diag.Diagnostic{Message: e.Err.Error(), Spans: []diag.Span{e.Pos}}
	}

	return diag.Diagnostic{
		Message: "cobol-go does not build " + e.Source.String(),
		Spans:   []diag.Span{{}, {Note: e.Err.Error()}},
	}
}

// Unwrap gives up what `cobol-go` returned.
func (e *DefinitionsBuildError) Unwrap() error { return e.Err }

// Naming is one record naming a definitions file or a block, as a fault inside
// it points back at it.
type Naming struct {
	// Record is the record's name.
	Record string

	// Pos is its `definitions` source.
	Pos diag.Span
}

// DefinitionsFaultError is a fault inside a definitions file or a block,
// reported once however many records name it.
//
// docs/layout/SPEC.md's "Who reports a fault in an entry body" requires every
// fault in a description to be reported once, at its span, rather than once per
// record that happened to name it — and a description is one step from the
// record, so a reader holding the diagnostic also needs to know which record's
// description it was being read as. That is what this adds: one span per record
// naming the file or the block, at its `definitions` source, carrying the
// record's name. A fault naming nowhere of its own — a `cobol-go` error with no
// position — is reported at the first of them instead, and the rest follow it.
//
// The fault itself reads the same whichever of the two holds the entries; only
// its span differs, into the definitions file for a file and into the layout
// for a block.
type DefinitionsFaultError struct {
	// Err is the fault, as whoever found it reported it.
	Err error

	// Kind is which of the two holds the entries, for what the notes call it.
	// The zero value reads as a definitions file.
	Kind layoutmodel.SourceKind

	// NamedBy are the records naming the file or the block, in the order the
	// layout writes them.
	NamedBy []Naming
}

// Error implements the error interface.
func (e *DefinitionsFaultError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *DefinitionsFaultError) Diagnostic() diag.Diagnostic {
	named := e.NamedBy

	// A fault that is not a diagnostic is the grammar's, whose text already
	// leads with the position it is at; it is rendered as that, with nothing
	// in front of it. Asserted on the error itself rather than through
	// [errors.As], for [diag.Diagnostics]' reason: a wrapper's text is what
	// the wrapper chose to say.
	d := diag.Diagnostic{Message: e.Err.Error(), Spans: []diag.Span{{}}}

	if carried, ok := e.Err.(diag.Error); ok {
		d = carried.Diagnostic()

		switch {
		case len(d.Spans) == 0 && len(named) > 0:
			d.Spans = []diag.Span{named[0].Pos}
			named = named[1:]
		case len(d.Spans) == 0:
			d.Spans = []diag.Span{{}}
		case !d.Spans[0].Stated() && len(named) > 0:
			d.Spans[0] = named[0].Pos
			named = named[1:]
		}
	}

	for _, n := range named {
		at := n.Pos
		at.Note = "the " + held(e.Kind) + " is read as record " + n.Record + "'s description"
		d.Spans = append(d.Spans, at)
	}

	return d
}

// Unwrap gives up the fault.
func (e *DefinitionsFaultError) Unwrap() error { return e.Err }

// held is what a fault's notes call the description it is in.
func held(kind layoutmodel.SourceKind) string {
	if kind == layoutmodel.DefinitionsBlockSource {
		return "definitions block"
	}

	return "definitions file"
}
