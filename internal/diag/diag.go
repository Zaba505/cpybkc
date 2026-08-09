// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package diag

import (
	"errors"
	"fmt"
	"strings"
)

// Span is one place a diagnostic points at: a file, and where in it.
//
// The line and the column are one-based and are the reader's of whatever file
// this is a place in — the layout grammar's for a layout, the copybook reader's
// for a copybook. The file is what makes the two comparable, and is why a
// position that cannot name one stops being usable exactly when a diagnostic
// names two.
type Span struct {
	// File is the name the file was read under, as the thing that read it
	// spells it. For a copybook that is the path the layout states, not a path
	// resolved against anything: docs/layout/SPEC.md leaves where a copybook was
	// looked for to the CLI, and a diagnostic naming a path the adopter never
	// wrote sends them looking for a file they have not got.
	File string

	// Line and Column are one-based.
	//
	// Line is zero where the file is all there is to point at. That is not a
	// missing position: a copybook declaring no `01`-level at all has nothing
	// in it to name, and SPEC.md's "A copybook that is not there, and an item
	// that is not in it" makes the file itself the span in that case.
	Line   int
	Column int

	// Note is what is at this place, in the message's own words — "it declares
	// ORDER-REC and ORDER-TRAILER-REC". It is what a span after the first is
	// for: the first span is where the fault is and the message says what, and
	// every one after it has to say why it is being shown.
	Note string
}

// Stated reports whether the span names anywhere at all.
func (s Span) Stated() bool { return s.File != "" || s.Line != 0 }

// String renders a span the way a compiler does, so that an editor and a
// terminal both know what to do with it.
//
// A span with a file and no line is the file alone rather than a line zero
// nothing can jump to, and a span with neither renders as nothing at all — an
// invented `0:0` is a position a reader would try to open.
func (s Span) String() string {
	switch {
	case s.File != "" && s.Line != 0:
		return fmt.Sprintf("%s:%d:%d", s.File, s.Line, s.Column)
	case s.File != "":
		return s.File
	case s.Line != 0:
		return fmt.Sprintf("%d:%d", s.Line, s.Column)
	default:
		return ""
	}
}

// Diagnostic is one thing wrong, what it is, and every place it is.
//
// Spans[0] is where the fault is — the sub-form that is wrong, not the
// top-level form containing it, which is what SPEC.md's "Every diagnostic
// carries a span" requires. Every span after it is somewhere else the fault
// implicates, carrying a [Span.Note] saying why it is being shown: the copybook
// an item is not in, the statement a rename collides with, each item an
// ambiguous reference matched.
//
// There is no severity. Everything reported through this type is something an
// adopter has to fix.
type Diagnostic struct {
	// Message is what was found, with no position in it and no trailing
	// newline. The position is [Diagnostic.String]'s to write, once, so that
	// every diagnostic in this repository leads with its span in the same
	// shape.
	Message string

	// Spans are the places the fault implicates, the first being where it is.
	Spans []Span
}

// String renders a diagnostic: the message under the span it is at, and one
// indented line per further place it implicates.
//
//	orders.sexpr:4:22: the copybook "cpy/order.cpy" declares no ORDER-HEADER-REC
//	  cpy/order.cpy:1:8: it declares ORDER-REC and ORDER-TRAILER-REC
//
// The continuation lines are indented because the whole of it is one fault: a
// reader scanning a column of diagnostics reads the unindented lines and finds
// one per thing to fix, and the second file is there for the one they stop at.
func (d Diagnostic) String() string {
	if len(d.Spans) == 0 {
		return d.Message
	}

	var b strings.Builder

	if d.Spans[0].Stated() {
		b.WriteString(d.Spans[0].String())
		b.WriteString(": ")
	}

	b.WriteString(d.Message)

	for _, span := range d.Spans[1:] {
		b.WriteString("\n  ")

		if span.Stated() {
			b.WriteString(span.String())

			if span.Note != "" {
				b.WriteString(": ")
			}
		}

		b.WriteString(span.Note)
	}

	return b.String()
}

// Error is an error that carries a diagnostic.
//
// A typed error implements it by building its [Diagnostic] in one place and
// rendering that from Error, so that what a caller reads and what a caller
// inspects cannot say different things. Every fault stays assertable with
// [errors.As] against its own type, which is what this repository's callers
// switch on; this interface is for the caller that wants to print a fault
// rather than to know which one it is.
type Error interface {
	error

	// Diagnostic is what the error says, and where.
	Diagnostic() Diagnostic
}

// List accumulates the faults one pass found.
//
// Every reader in this repository collects rather than returning the first: a
// generated layout is generated wrong in the same way in many places at once,
// and a reader that reports one fault per run is a reader run once per fault.
// It is one type here rather than a field on each reader so that "keep reading
// after a fault" is decided once.
type List struct {
	errs []error
}

// Fail records a fault. Reading continues after one, because the point of
// collecting them is to report the second.
func (l *List) Fail(err error) {
	l.errs = append(l.errs, err)
}

// Failed reports whether anything was recorded.
func (l *List) Failed() bool { return len(l.errs) > 0 }

// Err is every fault recorded, joined, or nil if there were none.
//
// [errors.Join] rather than a type of this package's own: a joined error is
// traversed by [errors.As], so a caller asserting against one fault finds it
// without knowing it was reported beside others.
func (l *List) Err() error { return errors.Join(l.errs...) }

// Diagnostics is every diagnostic in err, in the order it was reported.
//
// It walks what [errors.Join] built rather than asserting against it, because
// [errors.As] finds the first match in a tree and a caller printing faults
// wants all of them.
//
// An error carrying no diagnostic contributes one with its own text as the
// message and no span. That is what makes this readable over a tree the layer
// readers built: their faults already lead with the position they carry, so
// they render through here as they render themselves, and a fault that names a
// second file is the one that needs this package to say so. An error wrapping a
// diagnostic is its wrapper's text for the same reason — the wrapping is
// something a caller chose to say, and dropping it to reach inside would report
// a fault under a description nobody wrote.
func Diagnostics(err error) []Diagnostic {
	if err == nil {
		return nil
	}

	// The assertions below are against err itself rather than through
	// [errors.As], which is the whole point: As descends a tree and stops at
	// the first match, and this walk is here to reach every one of them.
	if carrier, ok := err.(Error); ok {
		return []Diagnostic{carrier.Diagnostic()}
	}

	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var found []Diagnostic

		for _, e := range joined.Unwrap() {
			found = append(found, Diagnostics(e)...)
		}

		return found
	}

	return []Diagnostic{{Message: err.Error()}}
}

// Render draws every diagnostic in err, one fault per unindented line.
//
// It is what a caller with a joined error and a terminal wants, and what the
// golden tests pin. A nil error renders as nothing rather than as an empty
// line.
func Render(err error) string {
	found := Diagnostics(err)
	lines := make([]string, 0, len(found))

	for _, diagnostic := range found {
		lines = append(lines, diagnostic.String())
	}

	return strings.Join(lines, "\n")
}

// joinAnd joins names the way a message does, so that a list of things an adopter
// could have meant reads as a sentence rather than as a slice.
func joinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
	}
}
