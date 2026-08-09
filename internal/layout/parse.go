// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layout

import (
	"errors"
	"fmt"
	"io"
	"os"

	sexpr "github.com/z5labs/sexpr-go"
)

// ParseFile reads the layout at path.
//
// The path is what every position in the returned [File] carries, so a
// diagnostic names the file an adopter has to open rather than describing where
// in an unnamed stream something went wrong.
func ParseFile(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open the layout: %w", err)
	}

	// Nothing is written, so a failure to close says nothing about whether the
	// bytes were read correctly; the parse below is what decides that.
	defer func() { _ = f.Close() }()

	return Parse(path, f)
}

// Parse reads a layout from r under the name name.
//
// It returns every fault it found, joined, rather than the first — a generated
// layout is generated wrong in the same way in many places at once, and each is
// still assertable with [errors.As]. The one exception is source the grammar
// itself rejects: that leaves no forms to have found anything else wrong with,
// so a [SyntaxError] is returned alone.
func Parse(name string, r io.Reader) (*File, error) {
	parsed, err := sexpr.Parse(r)
	if err != nil {
		return nil, &SyntaxError{Pos: positionOf(name, err), Err: err}
	}

	read := &reader{name: name}
	file := &File{Name: name}

	for _, node := range parsed.Nodes {
		form, ok := read.topLevel(node)
		if !ok {
			continue
		}

		file.Forms = append(file.Forms, form)
	}

	if len(read.errs) > 0 {
		// The half-built file is dropped rather than returned beside the
		// errors. A layout that was rejected has no AST anything downstream
		// should be reading, and handing one back invites a caller to read it.
		return nil, errors.Join(read.errs...)
	}

	return file, nil
}

// reader holds the state one parse accumulates: the name every position carries
// and the faults found so far.
type reader struct {
	name string
	errs []error
}

// pos lifts a grammar position into a layout one by naming the file it is in.
func (r *reader) pos(pos sexpr.Pos) Pos {
	return Pos{File: r.name, Line: pos.Line, Column: pos.Column}
}

// fail records a fault. Parsing continues after one, because the point of
// collecting them is to report the second.
func (r *reader) fail(err error) {
	r.errs = append(r.errs, err)
}

// topLevel converts one of the layout's top-level nodes, which may only be a
// form.
func (r *reader) topLevel(node sexpr.Node) (Form, bool) {
	if !r.admissible(node) {
		return Form{}, false
	}

	list, ok := node.(sexpr.List)
	if !ok {
		r.fail(&NotAFormError{Pos: r.pos(positionOfNode(node)), Found: describe(node)})

		return Form{}, false
	}

	return r.form(list)
}

// form converts a list into the tagged form a layout writes as one.
func (r *reader) form(list sexpr.List) (Form, bool) {
	// admissible has already refused an empty list and an improper one, so
	// there is an element zero and everything after it is an element rather
	// than a tail.
	tag, ok := list.Elements[0].(sexpr.Symbol)
	if !ok {
		r.fail(&UntaggedFormError{
			Pos:   r.pos(positionOfNode(list.Elements[0])),
			Found: describe(list.Elements[0]),
		})

		return Form{}, false
	}

	form := Form{
		Pos:    r.pos(list.Pos),
		Tag:    tag.Value,
		TagPos: r.pos(tag.Pos),
	}

	for _, element := range list.Elements[1:] {
		node, ok := r.node(element)
		if !ok {
			continue
		}

		form.Elements = append(form.Elements, node)
	}

	return form, true
}

// node converts one element of a form: a value, or a nested form.
func (r *reader) node(node sexpr.Node) (Node, bool) {
	if !r.admissible(node) {
		return nil, false
	}

	switch node := node.(type) {
	case sexpr.Symbol:
		return Symbol{Pos: r.pos(node.Pos), Value: node.Value}, true
	case sexpr.String:
		return Text{Pos: r.pos(node.Pos), Value: node.Value}, true
	case sexpr.Int:
		return Int{Pos: r.pos(node.Pos), Value: node.Value}, true
	case sexpr.Float:
		return Float{Pos: r.pos(node.Pos), Value: node.Value}, true
	case sexpr.List:
		form, ok := r.form(node)
		if !ok {
			return nil, false
		}

		return form, true
	default:
		// Unreachable while sexpr-go's node set is the one admissible knows,
		// and reported rather than dropped if that ever stops being true: a
		// node kind nobody has heard of is exactly what this package must not
		// pass over in silence.
		r.fail(&NotAFormError{Pos: r.pos(positionOfNode(node)), Found: describe(node)})

		return nil, false
	}
}

// admissible reports whether a node is one the layout format admits at all,
// recording a fault when it is not.
//
// These are the constructs docs/layout/SPEC.md's "What this document delegates"
// excludes: legal S-expressions with no meaning in a layout. Each is rejected by
// name rather than by failing to fit somewhere, because a construct with no
// meaning that nevertheless parses is a construct two generators will emit
// differently.
func (r *reader) admissible(node sexpr.Node) bool {
	switch node := node.(type) {
	case sexpr.Quote:
		r.fail(&ConstructError{Pos: r.pos(node.Pos), Construct: ConstructQuoteShorthand})
	case sexpr.Nil:
		r.fail(&ConstructError{Pos: r.pos(node.Pos), Construct: ConstructNil})
	case sexpr.Bool:
		r.fail(&ConstructError{Pos: r.pos(node.Pos), Construct: ConstructBoolean})
	case sexpr.List:
		if len(node.Elements) == 0 {
			r.fail(&ConstructError{Pos: r.pos(node.Pos), Construct: ConstructEmptyList})

			return false
		}

		if node.Tail != nil {
			r.fail(&ConstructError{Pos: r.pos(node.Pos), Construct: ConstructImproperList})

			return false
		}

		return true
	default:
		return true
	}

	return false
}

// positionOfNode is where a grammar node was written.
//
// [github.com/z5labs/sexpr-go]'s nodes carry a position as a field rather than
// behind a method, so asking one where it is means switching on its kind. The
// AST this package builds does not — [Node.Position] is why — and this function
// is the boundary between the two.
func positionOfNode(node sexpr.Node) sexpr.Pos {
	switch node := node.(type) {
	case sexpr.Symbol:
		return node.Pos
	case sexpr.String:
		return node.Pos
	case sexpr.Int:
		return node.Pos
	case sexpr.Float:
		return node.Pos
	case sexpr.Bool:
		return node.Pos
	case sexpr.Nil:
		return node.Pos
	case sexpr.List:
		return node.Pos
	case sexpr.Quote:
		return node.Pos
	default:
		return sexpr.Pos{}
	}
}
