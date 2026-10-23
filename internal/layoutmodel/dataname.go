// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"github.com/Zaba505/cpybkc/internal/definitions"
	"github.com/Zaba505/cpybkc/internal/layout"
)

// tagWord is the form a data-name takes where `sexpr-go` would not read its
// spelling as a symbol.
const tagWord = "word"

// dataName reads a name in a position of the `data-name` sort: every layout
// position that names a COBOL item — an item reference's path, a source's
// top-level name, an arm's alternative and a `take-alternative`'s
// (docs/layout/SPEC.md, "The leaves: a level, a data-name and literal text").
//
// A data-name is a symbol, in the case the copybook wrote it, or
// `(word "<spelling>")` where `sexpr-go` would read the spelling as something
// other than that symbol — today only `nil`, which it reads as its empty value.
// The name returned is the spelling either way, so `(word "nil")` is the name
// `nil` and nothing downstream can tell how it was written. Its position is the
// symbol, or the `word` form.
//
// ok is false, with no error, when the node is neither: the position's own form
// error is the caller's to report, in the words it already uses. A `word`
// around a spelling that reads as a symbol anyway is a
// [definitions.NeedlessWordError] — the entry reader's fault, in the entry
// reader's words, because one name has one spelling wherever it is written.
func dataName(node layout.Node) (name string, pos layout.Pos, ok bool, err error) {
	switch node := node.(type) {
	case layout.Symbol:
		return node.Value, node.Pos, true, nil
	case layout.Form:
		if node.Tag != tagWord || len(node.Elements) != 1 {
			return "", layout.Pos{}, false, nil
		}

		spelling, isText := node.Elements[0].(layout.Text)
		if !isText {
			return "", layout.Pos{}, false, nil
		}

		if definitions.ReadsAsSymbol(spelling.Value) {
			return "", layout.Pos{}, false, &definitions.NeedlessWordError{Pos: node.Pos, Spelling: spelling.Value}
		}

		return spelling.Value, node.Pos, true, nil
	default:
		return "", layout.Pos{}, false, nil
	}
}

// spell is a data-name as a layout writes it, which is what a diagnostic
// quoting one has to say for the adopter to find it.
func spell(name string) string { return definitions.Spell(name) }
