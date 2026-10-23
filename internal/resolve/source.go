// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"strconv"

	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// Source is where a record's description is held, carried so that a diagnostic
// about an item can name the file the fault is in and say what kind of file it
// is.
//
// A record's description is held in one of three places (docs/layout/SPEC.md,
// "A record's source: a copybook, a definitions file or a block"), and a
// diagnostic about one of its items points into whichever holds it. The three
// differ in two ways that a path alone cannot carry, which is why this is a
// value rather than the string it replaced:
//
//   - what a message calls it — "the copybook", "the definitions file", "the
//     definitions block" — so that a record moved from a copybook to a file is
//     not told about a copybook it no longer has;
//   - which file its positions are in. A span into a copybook names the path
//     as the layout spells it, as it always has; a span into a definitions file
//     names the file as the run opened it; and a span into a block names the
//     layout, because the block is a form of it (docs/layout/SPEC.md, "Every
//     diagnostic carries a span, and some carry two").
//
// Nothing here is resolved against anything. Both strings are whatever the
// caller that read the layout decided, and the caller is the one that knows
// where it opened what.
type Source struct {
	// Kind is which of the three sources the record names. The zero value
	// states none, and reads as a copybook: that is what every caller meant
	// before a record could be held anywhere else.
	Kind layoutmodel.SourceKind

	// Spelling is how the layout names the source: the path as it spells it,
	// for a copybook or a definitions file, and the block's name for a block.
	Spelling string

	// File is the file every position in the description is in, and what a
	// span into it names.
	File string
}

// CopybookAt is the source of a record bound to the copybook at path, as the
// layout spells it — which is also the file its positions are reported in.
func CopybookAt(path string) Source {
	return Source{Kind: layoutmodel.CopybookSource, Spelling: path, File: path}
}

// Noun is what a message calls the kind of source: "copybook", "definitions
// file" or "definitions block".
func (s Source) Noun() string { return noun(s.Kind) }

// String names the source the way a message does: the kind, then the path
// quoted, or the block's name bare, since a block name is a symbol and was never
// written in quotes.
func (s Source) String() string {
	if s.Kind == layoutmodel.DefinitionsBlockSource {
		return "the " + s.Noun() + " " + s.Spelling
	}

	return "the " + s.Noun() + " " + strconv.Quote(s.Spelling)
}

// Span is where in the description a field is, and the description's file
// itself for no field.
//
// A field with no line — a node this package introduced stands for none — is
// the file alone, which diag renders as the file rather than as a line zero
// nothing can jump to.
func (s Source) Span(field *copybook.Field) diag.Span {
	span := diag.Span{File: s.File}
	if field != nil {
		span.Line, span.Column = field.Pos.Line, field.Pos.Column
	}

	return span
}

// noun is what a message calls a kind of source.
func noun(kind layoutmodel.SourceKind) string {
	switch kind {
	case layoutmodel.DefinitionsFileSource:
		return "definitions file"
	case layoutmodel.DefinitionsBlockSource:
		return "definitions block"
	default:
		return "copybook"
	}
}
