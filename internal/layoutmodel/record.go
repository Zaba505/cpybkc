// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
)

// The tags the binding half of the record definitions layer reads. `record` is
// the form; `copybook` is its one required child, and `alternative` the
// repeatable one that says which alternative of a record-level `REDEFINES` the
// form means.
const (
	childCopybook    = "copybook"
	childAlternative = "alternative"
)

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

	// Alternatives are the `alternative` children, in the order the form
	// writes them: one item reference per `REDEFINES` the bound `01`-level
	// carries outside a repeating group, naming the alternative this record
	// type is (docs/layout/SPEC.md, "Which alternative a record is").
	//
	// Nothing at this layer knows how many there should be, or whether a
	// reference names an alternative at all: both are facts about the copybook
	// and are `resolve`'s. What is checked here is what the layout decides on
	// its own — that each reference is rooted at the record carrying it, and
	// that no two of them name one item.
	//
	// The order is the layout's and carries no meaning. Two redefines name two
	// distinct runs of bytes, so which is written first decides nothing, and a
	// consumer pairing them by position would be inventing the rule this child
	// exists to state.
	Alternatives []ItemRef
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
// written as a name over one `copybook` child and any number of `alternative`
// children, that the path and the names are written at all, that every
// `alternative` names an item through the record carrying it and no two of them
// name one item, and that no two forms define one name. Everything else about a
// binding needs the file — whether the path names something that can be opened,
// whether what it opens declares the item, and how many alternatives its
// `01`-level has for the children to choose among — and is `resolve`'s or the
// CLI's, which is the one place that knows where a copybook is looked for.
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
	if len(form.Elements) < 2 {
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

	// Every half is read whatever the ones before it said, for the reason a
	// rename is held to every rule past the first it breaks: a record whose
	// name is already taken and whose copybook has no path is two things to fix
	// rather than one to discover on the next run.
	sound := r.binding(&record, child)
	sound = r.alternatives(&record, form) && sound

	return record, r.name(form, name) && sound
}

// alternatives reads the `alternative` children of a `record` form.
//
// They follow the `copybook` child, and every element past it is one: the form
// admits no other child, so a tag that is not `alternative` there is answered by
// the same message a wrong first child is.
//
// Two things are checked, and they are the two the layout decides on its own. A
// reference is rooted at a record, and an `alternative` naming an alternative of
// some *other* record's copybook is a statement about a copybook this record is
// not bound to — it would be read against the wrong item tree, and no diagnostic
// afterwards could say so. And two children naming one item choose one
// alternative twice, which leaves the record's other redefine unchosen while
// looking like it was answered.
func (r *recordReader) alternatives(record *Record, form layout.Form) bool {
	sound := true

	named := make(map[string]layout.Pos, len(form.Elements))

	for _, element := range form.Elements[2:] {
		child, ok := element.(layout.Form)
		if !ok || child.Tag != childAlternative {
			r.Fail(&ChildError{
				Pos:    element.Position(),
				Form:   form.Tag,
				Found:  describe(element),
				Admits: []string{childAlternative},
			})

			sound = false

			continue
		}

		if len(child.Elements) != 1 {
			r.Fail(&AlternativeFormError{Pos: child.Pos, Found: count(len(child.Elements))})

			sound = false

			continue
		}

		ref, err := readItemRef(child.Elements[0])
		if err != nil {
			r.Fail(err)

			sound = false

			continue
		}

		if ref.Record != record.Name {
			r.Fail(&AlternativeRootError{Pos: ref.Pos, Record: record.Name, Ref: ref})

			sound = false

			continue
		}

		if first, already := named[ref.identity()]; already {
			r.Fail(&DuplicateAlternativeError{Pos: child.Pos, First: first, Ref: ref})

			sound = false

			continue
		}

		named[ref.identity()] = child.Pos
		record.Alternatives = append(record.Alternatives, ref)
	}

	return sound
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
