// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
)

// The tags this half of the record definitions layer reads.
const (
	tagCopybookReading     = "copybook-reading"
	childOccursDependingOn = "occurs-depending-on"
)

// Reading is which of the two vendor readings of `OCCURS DEPENDING ON` the file
// a layout describes was written under.
//
// It is the adopter's own spelling rather than a word this project invented:
// Micro Focus's directive is `ODOSLIDE`/`NOODOSLIDE` and GnuCOBOL carries the
// same switch as the dialect option `odoslide`, so an adopter looks the setting
// up in the compiler that wrote the file and finds it spelled this way. IBM
// Enterprise COBOL slides unconditionally and has no directive to name, which is
// why the value is not read off a dialect.
//
// The zero value is [ReadingUnstated], and it is a value rather than an absence
// because the thing `resolve` has to reject is a layout that says nothing while
// a bound copybook carries the clause. There is no default: the two readings put
// every item behind a table somewhere different and nothing in the file
// disagrees with the wrong one (docs/layout/SPEC.md, "The `OCCURS DEPENDING ON`
// reading is one statement per layout").
type Reading string

const (
	// ReadingUnstated is a layout carrying no `copybook-reading` form.
	ReadingUnstated Reading = ""

	// ODOSlide is IBM's layout, and Micro Focus's under `ODOSLIDE`: an item
	// behind a table begins at the byte after the last occurrence the count
	// states.
	ODOSlide Reading = "odoslide"

	// NoODOSlide is Micro Focus's `NOODOSLIDE` and GnuCOBOL's default: the
	// items behind a table keep fixed addresses beginning after the space
	// allocated for it at its maximum length.
	NoODOSlide Reading = "noodoslide"
)

// readings is the closed set the form admits, in the order docs/layout/SPEC.md
// writes them.
var readings = []Reading{ODOSlide, NoODOSlide}

// Stated reports whether the layout said which reading its file was written
// under.
func (r Reading) Stated() bool { return r != ReadingUnstated }

// Slides reports whether an item behind a variable-length table begins at the
// byte after the last occurrence the count states.
//
// It is false for [ReadingUnstated] as well as for [NoODOSlide], so a caller
// that asks this without having asked [Reading.Stated] first gets the reading
// that is safe to compute — a fixed table — rather than one silently taken as a
// default. Refusing an unstated reading is `resolve`'s, and it is refused
// against the copybook, where the table that needs the answer is.
func (r Reading) Slides() bool { return r == ODOSlide }

// String implements the [fmt.Stringer] interface.
func (r Reading) String() string {
	if r == ReadingUnstated {
		return "unstated"
	}

	return string(r)
}

// ReadCopybookReading reads the `OCCURS DEPENDING ON` reading out of a parsed
// layout, reporting [ReadingUnstated] for a layout that carries no
// `copybook-reading` form.
//
// An absent form is not a fault here, and that is the division of labour this
// reader is built on. Whether the layout needed one is a question about the
// copybooks it binds — only a record carrying the clause needs the answer — and
// this package never opens a copybook. So the layer reads what was written, and
// `resolve` rejects the layout that wrote nothing, naming the record and the
// table (docs/layout/SPEC.md, "The `OCCURS DEPENDING ON` reading is one
// statement per layout", #27, #35).
//
// It reports every fault it finds, joined, and returns [ReadingUnstated] when it
// found one, for [ReadProfile]'s reason: a reading an adopter cannot act on is
// worse than none.
//
// Top-level forms belonging to other layers are not read here and are not
// faults.
func ReadCopybookReading(file *layout.File) (Reading, error) {
	read := &readingReader{}
	reading := ReadingUnstated

	// Every `copybook-reading` form is read and not only the first, so that a
	// layout carrying two malformed ones is told about both rather than about
	// the count alone.
	var forms []layout.Form

	for _, form := range file.Forms {
		if form.Tag != tagCopybookReading {
			continue
		}

		forms = append(forms, form)

		one := read.reading(form)
		if len(forms) == 1 {
			reading = one
		}
	}

	// The count is a fact about the layout rather than about any one form, so
	// it is reported after everything the forms themselves were wrong about.
	// The second form is what the diagnostic points at: the first is a reading
	// an adopter meant, and the second is the line making it ambiguous.
	if len(forms) > 1 {
		read.Fail(&ReadingCountError{Pos: forms[1].Pos, First: forms[0].Pos, Count: len(forms)})
	}

	if read.Failed() {
		return ReadingUnstated, read.Err()
	}

	return reading, nil
}

// readingReader holds the state one [ReadCopybookReading] accumulates.
type readingReader struct {
	diag.List
}

// reading reads one `copybook-reading` form.
//
// The form carries exactly one child and the child carries exactly one symbol,
// which is a shape shallow enough that a stated-child map would be a map with
// one key in it. What replaces it is the count check below: a second
// `occurs-depending-on` is a form with two elements, and that is already the
// fault this reports.
func (r *readingReader) reading(form layout.Form) Reading {
	if len(form.Elements) != 1 {
		r.Fail(&ReadingFormError{
			Pos:   form.Pos,
			Takes: "one " + quote(childOccursDependingOn) + " child",
			Found: count(len(form.Elements)),
		})

		return ReadingUnstated
	}

	child, ok := form.Elements[0].(layout.Form)
	if !ok || child.Tag != childOccursDependingOn {
		r.Fail(&ChildError{
			Pos:    form.Elements[0].Position(),
			Form:   form.Tag,
			Found:  describe(form.Elements[0]),
			Admits: []string{childOccursDependingOn},
		})

		return ReadingUnstated
	}

	return r.value(child)
}

// value reads the one symbol the `occurs-depending-on` child is written with.
func (r *readingReader) value(child layout.Form) Reading {
	const takes = "one symbol naming its value"

	if len(child.Elements) != 1 {
		r.Fail(&ReadingFormError{Pos: child.Pos, Child: child.Tag, Takes: takes, Found: count(len(child.Elements))})

		return ReadingUnstated
	}

	symbol, ok := child.Elements[0].(layout.Symbol)
	if !ok {
		r.Fail(&ReadingFormError{
			Pos:   child.Elements[0].Position(),
			Child: child.Tag,
			Takes: takes,
			Found: describe(child.Elements[0]),
		})

		return ReadingUnstated
	}

	for _, reading := range readings {
		if Reading(symbol.Value) == reading {
			return reading
		}
	}

	r.Fail(&ReadingValueError{Pos: symbol.Pos, Value: symbol.Value, Admits: readingNames()})

	return ReadingUnstated
}

// readingNames is the set the `occurs-depending-on` child takes, as an adopter
// writes them.
func readingNames() []string {
	names := make([]string, 0, len(readings))
	for _, reading := range readings {
		names = append(names, string(reading))
	}

	return names
}
