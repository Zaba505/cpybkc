// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layout

import "fmt"

// Pos is where something was written: the file, and the line and column within
// it.
//
// The line and the column are the grammar's, counted from one.
// [github.com/z5labs/sexpr-go] carries them on every token, which is the
// property docs/layout/SPEC.md's "Positions survive" chose the notation for.
// The file is added as the AST is built, because a reader parses bytes and has
// no name to attach to them.
type Pos struct {
	// File is the name the layout was read under. It is whatever the caller
	// passed [Parse] — a path, or something else naming the source — and is
	// empty only when the caller named nothing.
	File string

	// Line and Column are one-based, counted by the grammar.
	Line   int
	Column int
}

// String renders a position the way a compiler does, so that an editor and a
// terminal both know what to do with it. A position with no file renders as
// line and column alone rather than as a leading colon.
func (p Pos) String() string {
	if p.File == "" {
		return fmt.Sprintf("%d:%d", p.Line, p.Column)
	}

	return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Column)
}

// Node is one node of a layout AST.
//
// The set is closed — a [Form], and the four kinds of value a form's positions
// take — and the unexported method is what closes it: nothing outside this
// package can add a node kind, so a type switch over the five is exhaustive and
// stays exhaustive.
//
// Every node answers [Node.Position], which is the acceptance criterion this
// AST exists for and also a working convenience: a caller asking where
// something is never has to switch on its kind to find out.
type Node interface {
	// Position is where the node was written.
	Position() Pos

	// node keeps the set of node kinds closed to this package.
	node()
}

// Form is a tagged form: a tag naming a relation, and the nodes that follow it.
//
// `(discriminate ORDER-HEADER (equals (item ORDER-HEADER OH-REC-TYPE) "10"))`
// is one Form whose tag is `discriminate` and whose elements are a [Symbol] and
// another Form. Which of those elements are the form's arguments and which are
// its children is the published schema's to say, per tag, so this type keeps
// them in one slice in source order rather than splitting them on a rule it
// would be a second statement of.
type Form struct {
	// Pos is the opening parenthesis.
	Pos Pos

	// Tag names the relation the form states.
	Tag string

	// TagPos is the tag itself, which is a position of its own because a
	// diagnostic about a tag has to point at the tag rather than at the form
	// opened by it.
	TagPos Pos

	// Elements are the nodes after the tag, in source order.
	Elements []Node
}

// Position implements [Node].
func (f Form) Position() Pos { return f.Pos }

func (Form) node() {}

// Symbol is a bare name: a tag's argument, a record name, a member of a closed
// value set.
type Symbol struct {
	Pos   Pos
	Value string
}

// Position implements [Node].
func (s Symbol) Position() Pos { return s.Pos }

func (Symbol) node() {}

// Text is a string literal. docs/layout/SPEC.md calls the sort `text`, and this
// type is named for the sort rather than for the token so that a position's
// declared sort and the node standing in it read as the same word.
type Text struct {
	Pos   Pos
	Value string
}

// Position implements [Node].
func (t Text) Position() Pos { return t.Pos }

func (Text) node() {}

// Int is an integer literal. The sorts `number` and `positive-number` are both
// written as one.
type Int struct {
	Pos   Pos
	Value int64
}

// Position implements [Node].
func (i Int) Position() Pos { return i.Pos }

func (Int) node() {}

// Float is a non-integer number literal.
//
// No position in the format is declared to take one today, and the sort
// `number` admits it: a layout that writes `4096.5` where a count belongs is
// held to the sort by the schema, with a diagnostic naming the position, rather
// than being rejected here as something the AST cannot hold. That is the
// division this package keeps to — the reader builds what was written, and what
// may stand where is the schema's.
type Float struct {
	Pos   Pos
	Value float64
}

// Position implements [Node].
func (f Float) Position() Pos { return f.Pos }

func (Float) node() {}

// File is a parsed layout: the top-level forms, in the order the source states
// them.
//
// The order is kept because a diagnostic reads better in source order, not
// because anything in the format depends on it. A layout is a set of statements
// that refer to one another by name, and docs/layout/SPEC.md's "The layers stay
// separable" is explicit that nothing orders them.
type File struct {
	// Name is what the layout was read under, and what every [Pos] in it
	// carries.
	Name string

	// Forms are the layout's top-level forms.
	Forms []Form
}

// Start is the position a diagnostic about the file as a whole carries.
//
// A form a layout is missing has no position of its own, and the start of the
// file is where the reader has to add one.
func (f File) Start() Pos {
	return Pos{File: f.Name, Line: 1, Column: 1}
}

// Walk calls fn on n and then, unless fn returned false, on each of its
// elements in source order.
//
// It is here because every layer above this one walks the same tree, and a walk
// written once against the closed set of node kinds is one that cannot miss a
// kind added later.
func Walk(n Node, fn func(Node) bool) {
	if !fn(n) {
		return
	}

	form, ok := n.(Form)
	if !ok {
		return
	}

	for _, element := range form.Elements {
		Walk(element, fn)
	}
}
