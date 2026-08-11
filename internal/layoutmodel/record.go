// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
)

// The tag the binding half of the record definitions layer reads. `record` is
// the form; `copybook` is its one child.
const childCopybook = "copybook"

// Record is one `record` form: the name it defines, and the copybook item it
// binds that name to.
//
// docs/layout/SPEC.md's "Record definitions" is the whole of it. A record is a
// name the rest of the layout refers to — a sequencing expression writes it, a
// discriminator names it, an item reference is rooted at it — and a copybook
// path with the top-level item inside it that the name stands for.
//
// The path is carried exactly as the layout spells it and nothing has been
// resolved against anything. Where a copybook is looked for is the CLI's
// (docs/cli/SPEC.md, "Finding the inputs"), and a diagnostic naming a path the
// adopter never wrote sends them looking for a file they have not got — which
// is why every position here is kept beside the string, so that whoever opens
// the file can point back at the line that named it.
type Record struct {
	// Pos is the `record` form.
	Pos layout.Pos

	// Name is the name the form defines.
	Name string

	// NamePos is the name itself, which is what a diagnostic about the name
	// rather than about the binding points at.
	NamePos layout.Pos

	// Copybook is the `copybook` child, which is the span every diagnostic
	// about the file or the item it names carries
	// ([github.com/Zaba505/cpybkc/internal/diag.MissingCopybookError],
	// [github.com/Zaba505/cpybkc/internal/diag.UndeclaredItemError]).
	Copybook layout.Pos

	// Path is the copybook's path as the layout spells it.
	Path string

	// PathPos is the path itself.
	PathPos layout.Pos

	// Item is the copybook's top-level item this record is bound to, as the
	// layout spells it.
	Item string

	// ItemPos is the item name itself.
	ItemPos layout.Pos
}

// ReadRecords reads the record definitions out of a parsed layout, in the order
// the layout writes them.
//
// It reports every fault it finds, joined, and returns nothing when it found
// one, for [ReadProfile]'s reason: a binding an adopter cannot act on is worse
// than none.
//
// What it enforces is what a declaration cannot state and a copybook is not
// needed for: that a layout defines at least one record, that each `record` is
// written as a name over one `copybook` child, that the path and the names are
// written at all, and that no two forms define one name. Everything else about
// a binding needs the file — whether the path names something that can be
// opened, and whether what it opens declares the item — and is the CLI's, which
// is the one place that knows where a copybook is looked for.
//
// Two records naming one copybook and one item is not a fault and is the
// ordinary way a layout tells one `01`-level apart by where it sits
// (docs/layout/SPEC.md, "Many records may name one copybook, and two may name
// one item"). What is refused is the other direction: one name standing for two
// bindings, which would leave the order the forms are written in deciding which
// of them every reference meant.
//
// Top-level forms belonging to other layers are not read here and are not
// faults.
func ReadRecords(file *layout.File) ([]Record, error) {
	read := &recordReader{defined: make(map[string]layout.Pos)}

	var records []Record

	for _, form := range file.Forms {
		if form.Tag != tagRecord {
			continue
		}

		record, ok := read.record(form)
		if !ok {
			continue
		}

		records = append(records, record)
	}

	// The count is a fact about the layout rather than about any one form, so
	// it is reported after everything the forms themselves were wrong about. A
	// layout that defines no record describes no file: there is nothing for a
	// sequencing expression to name and nothing for a descriptor to carry.
	if len(records) == 0 && !read.Failed() {
		read.Fail(&NoRecordsError{File: file.Name})
	}

	if read.Failed() {
		return nil, read.Err()
	}

	return records, nil
}

// recordReader holds the state one [ReadRecords] accumulates.
type recordReader struct {
	diag.List

	// defined is where each name was first defined, which makes a second form
	// defining it reportable against the first.
	defined map[string]layout.Pos
}

// record reads one `record` form.
func (r *recordReader) record(form layout.Form) (Record, bool) {
	if len(form.Elements) != 2 {
		r.Fail(&RecordFormError{Pos: form.Pos, Found: shortfall(form.Elements)})

		return Record{}, false
	}

	name, ok := form.Elements[0].(layout.Symbol)
	if !ok {
		r.Fail(&RecordFormError{Pos: form.Elements[0].Position(), Found: describe(form.Elements[0])})

		return Record{}, false
	}

	child, ok := form.Elements[1].(layout.Form)
	if !ok || child.Tag != childCopybook {
		r.Fail(&ChildError{
			Pos:    form.Elements[1].Position(),
			Form:   form.Tag,
			Found:  describe(form.Elements[1]),
			Admits: []string{childCopybook},
		})

		return Record{}, false
	}

	record := Record{Pos: form.Pos, Name: name.Value, NamePos: name.Pos, Copybook: child.Pos}

	// The two halves are read whatever the other said, for the reason a rename
	// is held to every rule past the first it breaks: a record whose name is
	// already taken and whose copybook has no path is two things to fix rather
	// than one to discover on the next run.
	sound := r.binding(&record, child)

	return record, r.name(form, name) && sound
}

// binding reads the `copybook` child: the path, and the top-level item in it.
func (r *recordReader) binding(record *Record, child layout.Form) bool {
	if len(child.Elements) != 2 {
		r.Fail(&CopybookFormError{Pos: child.Pos, Found: shortfall(child.Elements)})

		return false
	}

	path, ok := child.Elements[0].(layout.Text)
	if !ok {
		r.Fail(&CopybookFormError{Pos: child.Elements[0].Position(), Found: describe(child.Elements[0])})

		return false
	}

	item, ok := child.Elements[1].(layout.Symbol)
	if !ok {
		r.Fail(&CopybookFormError{Pos: child.Elements[1].Position(), Found: describe(child.Elements[1])})

		return false
	}

	record.Path, record.PathPos = path.Value, path.Pos
	record.Item, record.ItemPos = item.Value, item.Pos

	// A path of no characters names no file, and the diagnostic the CLI would
	// otherwise raise about it would say that a copybook at "" could not be
	// opened — a message naming nothing, about a file nobody wrote.
	if path.Value == "" {
		r.Fail(&EmptyCopybookPathError{Pos: path.Pos, Record: record.Name})

		return false
	}

	return true
}

// name holds a record's name to the names already defined.
func (r *recordReader) name(form layout.Form, name layout.Symbol) bool {
	if first, already := r.defined[name.Value]; already {
		r.Fail(&DuplicateRecordError{Pos: form.Pos, First: first, Record: name.Value})

		return false
	}

	r.defined[name.Value] = form.Pos

	return true
}

// shortfall names what stood where two things belong.
//
// It is this file's rather than [count]'s because [count] answers for a
// position taking exactly one thing, where "one" is the answer and never the
// fault; both forms here take two, so one of something is a shortfall rather
// than the shape.
func shortfall(elements []layout.Node) string {
	switch {
	case len(elements) == 0:
		return "no value"
	case len(elements) == 1:
		return "one"
	default:
		return "several"
	}
}
