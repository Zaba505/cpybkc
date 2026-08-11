// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package project

import (
	"bytes"
	"os"
	"slices"

	cobol "github.com/Zaba505/cobol-go"
	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// binding is one `record` form with the copybook item it names, read.
type binding struct {
	// Record is the form, as the layout wrote it.
	Record layoutmodel.Record

	// Item is the copybook's top-level item the record is bound to, in a tree
	// of this record's own; see the package comment for why it is not shared
	// with another record naming the same item.
	Item *copybook.Field
}

// bindings are the records of one layout, bound, and what resolves an item
// reference against them.
type bindings struct {
	diag.List

	// dir is the directory the layout was read from, which is what every
	// copybook path in it is resolved against.
	dir string

	// bound is the bindings in the order the layout defines them.
	bound []binding

	// byName is the same bindings, by the name the layout gave each.
	byName map[string]binding
}

// source is one copybook file, read: the entries it holds, or why it holds
// none.
//
// It is kept rather than the fragment alone so that a file can be read once and
// still be reported against every record that named it. The two are separate
// questions: reading is about the file, and a diagnostic is about the line of
// the layout an adopter has to edit.
type source struct {
	// fragment is the copybook's entries, nil where reading it failed.
	fragment *cobol.Fragment

	// err is why it failed, nil where it did not.
	err error

	// missing reports that the file could not be opened at all, as against
	// opening and not being COBOL this build can read. They are different
	// faults with different fixes and different diagnostics.
	missing bool
}

// bind opens every copybook the layout's `record` forms name and binds each
// record to the top-level item inside it.
//
// A copybook file is read and parsed **once** however many records name it —
// the entries are the same whichever record asked for them, and a shared header
// is read by every record in a layout — and a fresh item tree is built for each
// of those records; see the package comment for why the tree is not shared.
//
// A file that could not be read is nevertheless reported **once per record that
// names it**, in the order the records are written. The read is not repeated,
// but the diagnostic is: two records bound to one missing copybook are two lines
// of the layout an adopter has to look at, and reporting only the first is the
// "run once per fault" this repository collects faults to avoid — they would fix
// the path on one line, run again, and be told about the next.
func bind(dir string, records []layoutmodel.Record) (*bindings, error) {
	b := &bindings{dir: dir, byName: make(map[string]binding, len(records))}

	read := make(map[string]source)

	for _, record := range records {
		path := at(dir, record.Path)

		file, already := read[path]
		if !already {
			file = readCopybook(path)
			read[path] = file
		}

		if file.err != nil {
			b.unreadable(record, path, file)

			continue
		}

		item := b.item(record, file.fragment)
		if item == nil {
			continue
		}

		bound := binding{Record: record, Item: item}
		b.bound = append(b.bound, bound)
		b.byName[record.Name] = bound
	}

	if b.Failed() {
		return nil, b.Err()
	}

	return b, nil
}

// readCopybook opens one copybook and parses it.
func readCopybook(path string) source {
	src, err := os.ReadFile(path)
	if err != nil {
		return source{err: err, missing: true}
	}

	// Fixed format, and see the package comment for why: nothing states a
	// reference format, and this is the one a copybook out of a mainframe
	// library is written in.
	file, err := cobol.Parse(bytes.NewReader(src), cobol.WithFragment(), cobol.WithSourceFormat(cobol.FixedFormat))
	if err != nil {
		return source{err: err}
	}

	if file.Fragment == nil {
		return source{err: errNoEntries}
	}

	return source{fragment: file.Fragment}
}

// unreadable reports a copybook one record could not be bound to.
//
// A file that is not there names both paths, which is the pair
// docs/cli/SPEC.md requires: the path **as the layout spells it**, which is what
// the adopter can find in their file, and the absolute path cpybkc opened,
// without which a relative path in a shared layout sends a reader to the wrong
// directory. Both are this record's own — the spelling comes from the form being
// reported, not from whichever record happened to read the file first.
func (b *bindings) unreadable(record layoutmodel.Record, path string, file source) {
	if file.missing {
		b.Fail(&CopybookError{
			Err: &diag.MissingCopybookError{
				Pos:  span(record.Copybook),
				Path: record.Path,
				Err:  file.err,
			},
			LookedIn: absolute(path),
		})

		return
	}

	b.Fail(&CopybookSourceError{Pos: span(record.Copybook), Path: record.Path, Err: file.err})
}

// item builds this record's own tree out of a parsed copybook and finds the
// top-level item the record names.
func (b *bindings) item(record layoutmodel.Record, fragment *cobol.Fragment) *copybook.Field {
	items, err := copybook.Build(fragment.Entries)
	if err != nil {
		b.Fail(&CopybookSourceError{Pos: span(record.Copybook), Path: record.Path, Err: err})

		return nil
	}

	declares := make([]string, 0, len(items))

	for _, item := range items {
		if item.Name == record.Item {
			return item
		}

		if !item.Filler {
			declares = append(declares, item.Name)
		}
	}

	// A copybook declaring no top-level item at all has nothing to point at but
	// itself, and a span with the file and no line renders as the file alone —
	// which is what docs/cli/SPEC.md's "or `file` alone where a line number
	// would point at nothing" is for.
	where := diag.Span{File: record.Path}
	if len(items) > 0 {
		where.Line, where.Column = items[0].Pos.Line, items[0].Pos.Column
	}

	b.Fail(&diag.UndeclaredItemError{
		Pos:      span(record.Copybook),
		Path:     record.Path,
		Item:     record.Item,
		Copybook: where,
		Declares: declares,
	})

	return nil
}

// field resolves an item reference to the copybook item it names, or reports
// why it names none.
//
// The reference is rooted at a record and carries one name per level down to
// the item, outermost first, with the top-level item's own name not repeated
// (docs/layout/SPEC.md, "An item reference"). So this is a walk down the
// children and nothing cleverer: a complete path has exactly one spelling, and
// there is no OF/IN qualification to work out.
//
// A path that names two items under one parent is refused rather than answered
// with the first. Duplicate data names are legal COBOL, and a reference matching
// two of them is one the adopter has to disambiguate — silently taking either
// would put a discriminator on whichever item this walk happened to meet first.
func (b *bindings) field(ref layoutmodel.ItemRef) *copybook.Field {
	// A reference rooted at a record nobody defines is the layer readers' fault
	// and is already reported by whichever of them read the form. Nothing is
	// added here, because a second message about it would name the same line
	// twice.
	bound, defined := b.byName[ref.Record]
	if !defined {
		return nil
	}

	found := bound.Item

	for _, name := range ref.Path {
		matches := matching(found, name)

		switch len(matches) {
		case 1:
			found = matches[0]
		case 0:
			b.Fail(&UnknownItemError{
				Pos:      span(ref.Pos),
				Ref:      ref,
				Name:     name,
				Path:     bound.Record.Path,
				Parent:   found,
				Declares: names(found),
			})

			return nil
		default:
			b.Fail(&AmbiguousItemError{
				Pos:     span(ref.Pos),
				Ref:     ref,
				Name:    name,
				Path:    bound.Record.Path,
				Matches: spansOf(bound.Record.Path, matches),
			})

			return nil
		}
	}

	return found
}

// matching is every child of parent carrying name.
//
// FILLER is not among them: an item with no data-name is one nothing can refer
// to, and a reference naming `FILLER` would otherwise match whichever unnamed
// item came first.
func matching(parent *copybook.Field, name string) []*copybook.Field {
	var matches []*copybook.Field

	for _, child := range parent.Children {
		if !child.Filler && child.Name == name {
			matches = append(matches, child)
		}
	}

	return matches
}

// names is what a group does declare, in source order, for the message about
// the name it does not.
//
// FILLER items are left out for [matching]'s reason: an item a reference cannot
// name is not one an adopter could have meant.
func names(parent *copybook.Field) []string {
	declared := make([]string, 0, len(parent.Children))

	for _, child := range parent.Children {
		if !child.Filler && !slices.Contains(declared, child.Name) {
			declared = append(declared, child.Name)
		}
	}

	return declared
}

// spansOf is where each of an ambiguous reference's matches was declared.
func spansOf(path string, matches []*copybook.Field) []diag.Span {
	spans := make([]diag.Span, 0, len(matches))

	for _, match := range matches {
		spans = append(spans, diag.Span{
			File:   path,
			Line:   match.Pos.Line,
			Column: match.Pos.Column,
			Note:   "it is declared here",
		})
	}

	return spans
}

// span is a layout position as a diagnostic carries one.
func span(pos layout.Pos) diag.Span {
	return diag.Span{File: pos.File, Line: pos.Line, Column: pos.Column}
}
