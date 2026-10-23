// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutschema

import (
	"fmt"
	"io"

	sexpr "github.com/z5labs/sexpr-go"
)

// BlockForm is the tag of the layout form whose body a definitions file is.
//
// It is the one tag this package names, and it names it because of what a
// definitions file is rather than because of anything a layout says:
// docs/layout/SPEC.md's "One schema, and a definitions file is one block's body"
// defines the file as exactly what a `definitions-block` holds after its name,
// checked against the same declarations. Everything else — which forms a body
// holds, what each takes, how many there must be — is still read from the
// schema, so an entry form added to it is checked in a file and in a block
// without a line changing here.
const BlockForm = "definitions-block"

// The two documents a check reads, as a diagnostic about a top level names
// them.
const (
	docLayout      = "a layout"
	docDefinitions = "a definitions file"
)

// fileStart is where a diagnostic about a document as a whole points: a form it
// is missing has no position of its own, and the start of the file is where
// the reader has to add one.
var fileStart = sexpr.Pos{Line: 1, Column: 1}

// ValidateDefinitions parses a definitions file from r under the name name and
// checks it against the schema, as one block's body.
//
// It is [Schema.Validate] for the other kind of document, and a parse failure is
// an error for the reason it is there.
func (s *Schema) ValidateDefinitions(name string, r io.Reader) ([]Diagnostic, error) {
	file, err := sexpr.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse the definitions file: %w", err)
	}

	return s.CheckDefinitions(name, file), nil
}

// CheckDefinitions holds a parsed definitions file to the schema and returns
// every diagnostic it finds, in the order the file states the forms they are
// about.
//
// The file's top-level forms are held to the declaration of a block's entries:
// the sort the [BlockForm]'s last argument is of, and that argument's arity.
// So a fault inside an entry is checked by the same declarations a block's is,
// and reads in the same words in a file as in a block. What differs is the top
// level, which names the document it is about: a form that is not an entry is
// a fault of a definitions file's, and says what was found — a block, a
// layout's form, or a form neither declares.
//
// A definitions file names no record, so nothing in one is a reference to
// check against the names a layout defines.
func (s *Schema) CheckDefinitions(name string, file *sexpr.File) []Diagnostic {
	check := s.newChecker(name)

	body, ok := s.blockBody()
	if !ok {
		check.report(fileStart, "the schema declares no %s form whose last argument repeats, so it says nothing %s may hold", BlockForm, docDefinitions)

		return check.diagnostics
	}

	entries := s.sorts[body.sort.name].forms
	count := 0

	for _, node := range file.Nodes {
		list, tag, ok := check.topLevelForm(node, docDefinitions)
		if !ok {
			continue
		}

		if decl, exists := s.contextual[contextKey{context: body.sort.name, tag: tag.Value}]; exists {
			count++

			check.form(list, decl)

			continue
		}

		_, layoutForm := s.contextual[contextKey{context: contextTopLevel, tag: tag.Value}]

		switch {
		case tag.Value == BlockForm:
			check.report(tag.Pos, "%s holds no %s: it is one block's body, and carries %s forms only", docDefinitions, BlockForm, and(entries))
		case layoutForm:
			check.report(tag.Pos, "form %q is a layout's, and %s carries %s forms only", tag.Value, docDefinitions, and(entries))
		default:
			check.report(tag.Pos, "unknown top-level form %q; %s carries %s", tag.Value, docDefinitions, and(entries))
		}
	}

	if !body.arity.admits(count) {
		check.report(fileStart, "%s carries %s %s form, and this one carries %d", docDefinitions, body.arity.label(), and(entries), count)
	}

	return check.diagnostics
}

// blockBody is the declaration of a block's entries: the [BlockForm]'s last
// argument, when it repeats and is of a declared sort. A schema without one —
// a test's, or one predating the form — declares nothing a definitions file
// may hold.
func (s *Schema) blockBody() (argDecl, bool) {
	decl, exists := s.contextual[contextKey{context: contextTopLevel, tag: BlockForm}]
	if !exists || len(decl.args) == 0 {
		return argDecl{}, false
	}

	last := decl.args[len(decl.args)-1]
	if !last.arity.repeatable() || last.sort.name == "" {
		return argDecl{}, false
	}

	if _, declared := s.sorts[last.sort.name]; !declared {
		return argDecl{}, false
	}

	return last, true
}
