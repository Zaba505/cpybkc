// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutschema

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/Zaba505/cpybkc/internal/diag"
	sexpr "github.com/z5labs/sexpr-go"
)

// Diagnostic is one thing wrong with a layout, and where.
//
// The position is the sub-form that is wrong rather than the top-level form
// containing it, which is what docs/layout/SPEC.md's "Every diagnostic carries a
// span" requires of every diagnostic this project emits. There is no severity:
// the schema describes what a layout may be, so everything reported here is a
// layout the schema does not admit.
type Diagnostic struct {
	// Span is where the fault is: the file the layout was checked under, and
	// the line and the column within it.
	//
	// The file is carried rather than left to the caller because a diagnostic
	// naming a line and a column alone stops being usable the moment there are
	// two files, which is what
	// [github.com/Zaba505/cpybkc/internal/diag]'s cross-file spans are about.
	Span diag.Span

	Message string
}

// String renders a diagnostic as one line, opening with the position for the
// reason every compiler does: the file an adopter has to edit is the first
// thing they need from it.
//
// It renders through [github.com/Zaba505/cpybkc/internal/diag] rather than
// formatting a line of its own, so that a fault the schema found and a fault a
// reader found read the same in a terminal.
func (d Diagnostic) String() string {
	return diag.Diagnostic{Message: d.Message, Spans: []diag.Span{d.Span}}.String()
}

// Validate parses a layout from r under the name name and checks it against the
// schema.
//
// The name is what every diagnostic carries — a path, or something else naming
// the source — for the same reason
// [github.com/Zaba505/cpybkc/internal/layout.Parse] takes one: a check reads
// bytes and has no name to attach to them, and a fault an adopter can act on
// names the file they have to open.
//
// A parse failure is an error rather than a diagnostic. It comes from the
// grammar this format delegates to, carries that package's own position, and
// stops there being a layout to check at all — so reporting it beside a list of
// forms the checker never saw would claim a coverage this call does not have.
func (s *Schema) Validate(name string, r io.Reader) ([]Diagnostic, error) {
	file, err := sexpr.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse the layout: %w", err)
	}

	return s.Check(name, file), nil
}

// Check holds a parsed layout to the schema and returns every diagnostic it
// finds, in the order the layout states the forms they are about.
func (s *Schema) Check(name string, file *sexpr.File) []Diagnostic {
	check := &checker{schema: s, name: name, declared: make(map[string][]string)}

	check.collectReferents(file)
	check.topLevel(file)

	return check.diagnostics
}

// checker holds the state one Check accumulates.
type checker struct {
	schema      *Schema
	diagnostics []Diagnostic

	// name is what the layout was checked under, and is on every span the
	// check reports.
	name string

	// declared holds, per reference sort, the names that sort may name. It is
	// gathered from the whole layout before anything is checked, so that a
	// reference to a record defined further down the file resolves — the forms
	// of a layout are a set, and nothing in the format orders them.
	declared map[string][]string
}

// report records a diagnostic.
func (c *checker) report(pos sexpr.Pos, format string, args ...any) {
	c.diagnostics = append(c.diagnostics, Diagnostic{Span: c.span(pos), Message: fmt.Sprintf(format, args...)})
}

// span lifts a grammar position into a diagnostic one by naming the file it is
// in.
func (c *checker) span(pos sexpr.Pos) diag.Span {
	return diag.Span{File: c.name, Line: pos.Line, Column: pos.Column}
}

// collectReferents gathers the names every reference sort may name.
//
// It is driven by the (reference ...) declarations rather than by knowing that
// a layout has records, so a second kind of reference added to the schema is
// collected without a line changing here.
func (c *checker) collectReferents(file *sexpr.File) {
	for _, name := range c.schema.sortNames() {
		decl := c.schema.sorts[name]
		if !decl.isReference() {
			continue
		}

		form, exists := c.schema.contextual[contextKey{context: contextTopLevel, tag: decl.refForm}]
		if !exists {
			continue
		}

		index := slices.IndexFunc(form.args, func(arg argDecl) bool { return arg.name == decl.refArg })
		if index < 0 {
			continue
		}

		for _, node := range file.Nodes {
			list, ok := node.(sexpr.List)
			if !ok || len(list.Elements) <= index+1 {
				continue
			}

			if tag, ok := list.Elements[0].(sexpr.Symbol); !ok || tag.Value != decl.refForm {
				continue
			}

			if referent, ok := list.Elements[index+1].(sexpr.Symbol); ok {
				c.declared[name] = append(c.declared[name], referent.Value)
			}
		}
	}
}

// topLevel checks each of the layout's top-level forms, and then how many of
// each it carries.
func (c *checker) topLevel(file *sexpr.File) {
	counts := make(map[string]int)

	for _, node := range file.Nodes {
		list, ok := c.formNode(node, "a layout is a list of forms")
		if !ok {
			continue
		}

		tag, ok := list.Elements[0].(sexpr.Symbol)
		if !ok {
			c.report(posOf(list.Elements[0]), "a form opens with a symbol naming it")

			continue
		}

		decl, exists := c.schema.contextual[contextKey{context: contextTopLevel, tag: tag.Value}]
		if !exists {
			c.report(tag.Pos, "unknown top-level form %q; a layout carries %s", tag.Value, and(c.schema.TopLevelForms()))

			continue
		}

		counts[tag.Value]++

		c.form(list, decl)
	}

	for _, decl := range c.schema.topLevel {
		if decl.arity.admits(counts[decl.tag]) {
			continue
		}

		// A missing form has no position of its own, so the diagnostic carries
		// the start of the file: that is where the reader has to add one.
		c.report(sexpr.Pos{Line: 1, Column: 1}, "a layout carries %s %s form, and this one carries %d", decl.arity.label(), decl.tag, counts[decl.tag])
	}
}

// form checks one form against its declaration: its positional arguments in
// order, then the children that follow them.
func (c *checker) form(list sexpr.List, decl *formDecl) {
	rest := c.arguments(list, decl)

	if len(decl.children) == 0 {
		for _, extra := range rest {
			c.report(posOf(extra), "form %q takes %s, and this is one argument too many", decl.tag, arguments(decl))
		}

		return
	}

	c.children(rest, list.Pos, decl)
}

// arguments checks the positional arguments of a form and returns whatever
// followed them.
//
// They are consumed in order by count, which is what [checkArgumentOrder]
// guarantees is possible: every argument but the last takes exactly one node,
// and a repeatable last argument takes everything left. A form that admits
// children has no repeatable argument, so what is left when this returns is its
// children and nothing else.
func (c *checker) arguments(list sexpr.List, decl *formDecl) []sexpr.Node {
	rest := list.Elements[1:]

	for i, arg := range decl.args {
		want := 1
		if i == len(decl.args)-1 && arg.arity.repeatable() {
			want = len(rest)
		}

		if have := min(want, len(rest)); want > len(rest) || !arg.arity.admits(have) {
			c.report(list.Pos, "form %q takes %s for its %s argument, and this one has %d", decl.tag, arg.arity.label(), arg.name, have)

			return nil
		}

		for _, node := range rest[:want] {
			c.value(node, arg.sort, fmt.Sprintf("%s of form %q", arg.name, decl.tag))
		}

		rest = rest[want:]
	}

	return rest
}

// children checks the children that follow a form's arguments.
func (c *checker) children(nodes []sexpr.Node, pos sexpr.Pos, decl *formDecl) {
	counts := make(map[string]int)

	for _, node := range nodes {
		list, ok := c.formNode(node, fmt.Sprintf("form %q takes children", decl.tag))
		if !ok {
			continue
		}

		tag, ok := list.Elements[0].(sexpr.Symbol)
		if !ok {
			c.report(posOf(list.Elements[0]), "a form opens with a symbol naming it")

			continue
		}

		child, exists := c.schema.children[tag.Value]
		if _, admitted := decl.childArity[tag.Value]; !exists || !admitted {
			c.report(tag.Pos, "form %q has no child %q; it admits %s", decl.tag, tag.Value, and(decl.children))

			continue
		}

		counts[tag.Value]++

		c.form(list, child)
	}

	for _, tag := range decl.children {
		if decl.childArity[tag].admits(counts[tag]) {
			continue
		}

		c.report(pos, "form %q takes %s %s, and this one has %d", decl.tag, decl.childArity[tag].label(), tag, counts[tag])
	}
}

// value checks one node against the sort its position admits. where names the
// position, so that a diagnostic says which argument of which form is wrong
// rather than only what was found.
func (c *checker) value(node sexpr.Node, ref sortRef, where string) {
	if !c.admissible(node, where) {
		return
	}

	if ref.name == "" {
		symbol, ok := node.(sexpr.Symbol)
		if !ok || !slices.Contains(ref.values, symbol.Value) {
			c.report(posOf(node), "%s is one of %s", where, and(ref.values))
		}

		return
	}

	c.namedSort(node, ref.name, where)
}

// namedSort checks a node against a primitive or a declared sort.
func (c *checker) namedSort(node sexpr.Node, name string, where string) {
	if c.satisfies(node, name) {
		return
	}

	c.report(posOf(node), "%s is %s", where, c.describe(name))
}

// satisfies reports whether node stands in a position of the named sort. It
// reports rather than diagnoses, because a sort that includes another has to be
// able to try a member and move on.
func (c *checker) satisfies(node sexpr.Node, name string) bool {
	switch name {
	case sortSymbol:
		_, ok := node.(sexpr.Symbol)

		return ok
	case sortText:
		_, ok := node.(sexpr.String)

		return ok
	case sortNumber:
		switch node.(type) {
		case sexpr.Int, sexpr.Float:
			return true
		default:
			return false
		}
	case sortPositiveNumber:
		number, ok := node.(sexpr.Int)

		return ok && number.Value > 0
	}

	decl, exists := c.schema.sorts[name]
	if !exists {
		return false
	}

	if decl.isReference() {
		symbol, ok := node.(sexpr.Symbol)

		return ok && slices.Contains(c.declared[name], symbol.Value)
	}

	if symbol, ok := node.(sexpr.Symbol); ok {
		if slices.Contains(decl.symbols, symbol.Value) {
			return true
		}
	}

	if list, ok := node.(sexpr.List); ok && len(list.Elements) > 0 {
		if tag, ok := list.Elements[0].(sexpr.Symbol); ok && slices.Contains(decl.forms, tag.Value) {
			// The form is checked here rather than by the caller, because this
			// is the only point at which the sort that admitted it is known,
			// and the sort is what says which declaration of a tag applies.
			c.form(list, c.schema.contextual[contextKey{context: name, tag: tag.Value}])

			return true
		}
	}

	for _, included := range decl.include {
		if c.satisfies(node, included) {
			return true
		}
	}

	return false
}

// describe renders what a sort admits, for a diagnostic that has to say what
// was expected rather than only that something was not it.
func (c *checker) describe(name string) string {
	switch name {
	case sortSymbol:
		return "a symbol"
	case sortText:
		return "text"
	case sortNumber:
		return "a number"
	case sortPositiveNumber:
		return "a positive integer"
	}

	decl, exists := c.schema.sorts[name]
	if !exists {
		return fmt.Sprintf("of sort %q, which the schema does not declare", name)
	}

	if decl.isReference() {
		return fmt.Sprintf("the name of a %s form this layout defines", decl.refForm)
	}

	var admits []string

	for _, tag := range decl.forms {
		admits = append(admits, "("+tag+" ...)")
	}

	admits = append(admits, decl.symbols...)

	for _, included := range decl.include {
		admits = append(admits, c.describe(included))
	}

	return and(admits)
}

// admissible reports whether a node is one the layout format admits at all,
// diagnosing it when it is not.
//
// These are the constructs docs/layout/SPEC.md's "What this document delegates"
// excludes: legal S-expressions with no meaning in a layout. Each is rejected by
// name rather than by failing to match a sort, because "a construct with no
// meaning that nevertheless parses is a construct two generators will emit
// differently, and there is nothing for a diagnostic to say about it".
func (c *checker) admissible(node sexpr.Node, where string) bool {
	switch node := node.(type) {
	case sexpr.Quote:
		c.report(node.Pos, "%s: a quote shorthand has no meaning in a layout", where)
	case sexpr.Nil:
		c.report(node.Pos, "%s: nil has no meaning in a layout; absence is expressed by omitting a child", where)
	case sexpr.Bool:
		c.report(node.Pos, "%s: no position in a layout takes a boolean", where)
	case sexpr.List:
		if len(node.Elements) == 0 {
			c.report(node.Pos, "%s: the empty list has no meaning in a layout; every form has a tag", where)

			return false
		}

		if node.Tail != nil {
			c.report(node.Pos, "%s: an improper list has no meaning in a layout", where)

			return false
		}

		return true
	default:
		return true
	}

	return false
}

// formNode narrows a node to the list a form is, diagnosing everything a form
// cannot be. context opens the message, so that the same rejection reads
// differently at the top level and inside a form.
func (c *checker) formNode(node sexpr.Node, context string) (sexpr.List, bool) {
	if !c.admissible(node, context) {
		return sexpr.List{}, false
	}

	list, ok := node.(sexpr.List)
	if !ok {
		c.report(posOf(node), "%s, and this is not one", context)

		return sexpr.List{}, false
	}

	return list, true
}

// arguments renders a form's argument list the way a diagnostic names it.
func arguments(decl *formDecl) string {
	if len(decl.args) == 0 {
		return "no arguments"
	}

	names := make([]string, 0, len(decl.args))

	for _, arg := range decl.args {
		if arg.arity.repeatable() {
			names = append(names, arg.name+" ...")

			continue
		}

		names = append(names, arg.name)
	}

	return strings.Join(names, ", ")
}

// and joins a list the way a sentence does, so that a diagnostic naming what is
// admitted reads as one.
func and(items []string) string {
	switch len(items) {
	case 0:
		return "nothing"
	case 1:
		return items[0]
	case 2:
		return items[0] + " or " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " or " + items[len(items)-1]
	}
}
