// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layout

import (
	"errors"
	"fmt"

	sexpr "github.com/z5labs/sexpr-go"
)

// SyntaxError is source the S-expression grammar rejected.
//
// It is the one failure that leaves no layout at all, so [Parse] returns it
// alone: there are no forms to have found anything else wrong with. The
// grammar's own error is kept and reachable with [errors.As], because it says
// which token was unexpected and what was expected instead, and this repository
// does not restate the grammar.
type SyntaxError struct {
	// Pos is where the grammar stopped. Its file is always set; its line and
	// column are the grammar's own, and are zero for the errors it raises
	// without one.
	Pos Pos

	// Err is the error [github.com/z5labs/sexpr-go] returned.
	Err error
}

// Error implements the error interface.
func (e *SyntaxError) Error() string {
	return fmt.Sprintf("%s: %v", e.Pos, e.Err)
}

// Unwrap gives up the grammar's error.
func (e *SyntaxError) Unwrap() error { return e.Err }

// Construct names an S-expression construct that has no meaning in a layout.
//
// The set is docs/layout/SPEC.md's "What this document delegates": legal
// S-expressions the layout format excludes by name. It is closed, because the
// document's list is.
type Construct int

// The constructs a layout may not contain.
const (
	// ConstructImproperList is a dotted pair — `(a b . c)`.
	ConstructImproperList Construct = iota

	// ConstructQuoteShorthand is `'x`, `` `x ``, `,x` or `,@x`.
	ConstructQuoteShorthand

	// ConstructNil is `nil`.
	ConstructNil

	// ConstructEmptyList is `()`.
	ConstructEmptyList

	// ConstructBoolean is `#t`, `#true`, `#f` or `#false`.
	ConstructBoolean
)

// String names the construct.
func (c Construct) String() string {
	switch c {
	case ConstructImproperList:
		return "an improper list"
	case ConstructQuoteShorthand:
		return "a quote shorthand"
	case ConstructNil:
		return "nil"
	case ConstructEmptyList:
		return "the empty list"
	case ConstructBoolean:
		return "a boolean"
	default:
		return fmt.Sprintf("Construct(%d)", int(c))
	}
}

// reason is what the message says after naming the construct: why the format
// has no place for it, where docs/layout/SPEC.md gives one.
func (c Construct) reason() string {
	switch c {
	case ConstructNil:
		return "; absence is expressed by omitting a child"
	case ConstructEmptyList:
		return "; every form has a tag"
	default:
		return ""
	}
}

// ConstructError is a legal S-expression with no meaning in a layout.
//
// It names the construct rather than reporting that something did not match,
// which is docs/layout/SPEC.md's requirement on it: each of these is "a
// diagnostic naming the construct and its position".
type ConstructError struct {
	Pos       Pos
	Construct Construct
}

// Error implements the error interface.
func (e *ConstructError) Error() string {
	return fmt.Sprintf("%s: %s has no meaning in a layout%s", e.Pos, e.Construct, e.Construct.reason())
}

// NotAFormError is a node written where the format admits only a form.
//
// A layout is its top-level forms and nothing else, so a bare symbol, a string
// or a number written at the top level is this rather than a form with a tag
// nobody declared.
type NotAFormError struct {
	Pos Pos

	// Found names what was written instead, so that the message says what it
	// found and not only what it wanted.
	Found string
}

// Error implements the error interface.
func (e *NotAFormError) Error() string {
	return fmt.Sprintf("%s: a layout is a set of forms, and this is %s", e.Pos, e.Found)
}

// UntaggedFormError is a form that does not open with a symbol naming it.
//
// Every statement a layout makes is a tag and the things it relates, so
// `("record" ORDER-HEADER)` is not a form whose tag happens to be text: it is a
// form with no tag, and there is nothing to look the rest of it up under.
type UntaggedFormError struct {
	// Pos is the element standing where the tag belongs, rather than the form
	// opened by it — the sub-form that is wrong is what a span points at.
	Pos Pos

	// Found names what was written where the tag belongs.
	Found string
}

// Error implements the error interface.
func (e *UntaggedFormError) Error() string {
	return fmt.Sprintf("%s: a form opens with a symbol naming it, and this one opens with %s", e.Pos, e.Found)
}

// describe names a grammar node the way a message refers to it.
func describe(node sexpr.Node) string {
	switch node.(type) {
	case sexpr.Symbol:
		return "a symbol"
	case sexpr.String:
		return "text"
	case sexpr.Int, sexpr.Float:
		return "a number"
	case sexpr.List:
		return "a form"
	default:
		return "something else"
	}
}

// positionOf is where the grammar's error happened, under the file it was
// reading.
//
// Every error [github.com/z5labs/sexpr-go] returns carries a position and none
// of them share an interface that exposes it, so this is a switch over the set
// rather than an assertion against one type. An error it does not know yields
// the file and no line, which is worse than a full position and better than
// dropping the file too.
func positionOf(name string, err error) Pos {
	pos := Pos{File: name}

	var (
		depth      sexpr.MaxDepthExceededError
		endOfInput sexpr.UnexpectedEndOfTokensError
		token      sexpr.UnexpectedTokenError
		number     sexpr.NumberRangeError
		character  sexpr.UnexpectedCharacterError
		comment    sexpr.UnterminatedCommentError
		text       sexpr.UnterminatedStringError
		numeral    sexpr.InvalidNumberError
		escape     sexpr.InvalidEscapeError
	)

	switch {
	case errors.As(err, &depth):
		pos.Line, pos.Column = depth.Pos.Line, depth.Pos.Column
	case errors.As(err, &endOfInput):
		pos.Line, pos.Column = endOfInput.Pos.Line, endOfInput.Pos.Column
	case errors.As(err, &token):
		pos.Line, pos.Column = token.Actual.Pos.Line, token.Actual.Pos.Column
	case errors.As(err, &number):
		pos.Line, pos.Column = number.Pos.Line, number.Pos.Column
	case errors.As(err, &character):
		pos.Line, pos.Column = character.Pos.Line, character.Pos.Column
	case errors.As(err, &comment):
		pos.Line, pos.Column = comment.Pos.Line, comment.Pos.Column
	case errors.As(err, &text):
		pos.Line, pos.Column = text.Pos.Line, text.Pos.Column
	case errors.As(err, &numeral):
		pos.Line, pos.Column = numeral.Pos.Line, numeral.Pos.Column
	case errors.As(err, &escape):
		pos.Line, pos.Column = escape.Pos.Line, escape.Pos.Column
	}

	return pos
}
