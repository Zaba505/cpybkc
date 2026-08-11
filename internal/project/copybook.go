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

// bind opens every copybook the layout's `record` forms name and binds each
// record to the top-level item inside it.
//
// A copybook file is read and parsed once however many records name it, and a
// fresh item tree is built for each of them; see the package comment for why the
// tree is not shared.
//
// Every fault is reported rather than the first, in the order the records are
// written: a layout generated against the wrong directory names every copybook
// wrongly at once, and a reader that stopped at the first would be run once per
// record.
func bind(dir string, records []layoutmodel.Record) (*bindings, error) {
	b := &bindings{dir: dir, byName: make(map[string]binding, len(records))}

	// One parse per file. The entries are the same whichever record asked for
	// them, and a copybook a shared header is COPY'd into is read by every
	// record in the layout.
	parsed := make(map[string]*cobol.Fragment)

	for _, record := range records {
		path := at(dir, record.Path)

		fragment, read := parsed[path]
		if !read {
			fragment = b.read(record, path)
			parsed[path] = fragment
		}

		if fragment == nil {
			continue
		}

		item := b.item(record, fragment)
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

// read opens one copybook and parses it.
//
// The path is reported as the layout spells it, with the absolute path cpybkc
// opened beside it, which is the pair docs/cli/SPEC.md requires: the first is
// what the adopter can find in their layout, and the second is the "where it was
// looked for" without which a relative path in a shared layout sends a reader
// to the wrong directory.
func (b *bindings) read(record layoutmodel.Record, path string) *cobol.Fragment {
	src, err := os.ReadFile(path)
	if err != nil {
		b.Fail(&CopybookError{
			Err: &diag.MissingCopybookError{
				Pos:  span(record.Copybook),
				Path: record.Path,
				Err:  err,
			},
			LookedIn: absolute(path),
		})

		return nil
	}

	// Fixed format, and see the package comment for why: nothing states a
	// reference format, and this is the one a copybook out of a mainframe
	// library is written in.
	file, err := cobol.Parse(bytes.NewReader(src), cobol.WithFragment(), cobol.WithSourceFormat(cobol.FixedFormat))
	if err != nil {
		b.Fail(&CopybookSourceError{Pos: span(record.Copybook), Path: record.Path, Err: err})

		return nil
	}

	if file.Fragment == nil {
		b.Fail(&CopybookSourceError{
			Pos:  span(record.Copybook),
			Path: record.Path,
			Err:  errNoEntries,
		})

		return nil
	}

	return file.Fragment
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
