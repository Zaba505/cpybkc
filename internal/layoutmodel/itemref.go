// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"strings"

	"github.com/Zaba505/cpybkc/internal/layout"
)

// ItemRef is a reference into a copybook: `(item <record-name> <name> …)`.
//
// docs/layout/SPEC.md's "An item reference" is the whole of it. The first name
// is a `record` form's; the rest is the path from that record's top-level item
// down to the item named, one name per level, outermost first, with the
// top-level item's own name not repeated.
//
// Whether the path names an item at all is `resolve`'s (#31, #32) — that needs
// the copybook, which nothing at this layer has. What is known here is what was
// written.
type ItemRef struct {
	// Pos is the `(item …)` form.
	Pos layout.Pos

	// Record is the name of the `record` form the path starts at.
	Record string

	// Path is the names below it, outermost first, ending at the item named. It
	// always carries at least one name: a reference to a record's top-level item
	// and no field is not a spelling the format has.
	Path []string
}

// Name is the item's own name, as the copybook spells it: the last name on the
// path, which is the one the reference ends at.
//
// It is the name every other name in the reference qualifies, and so the one a
// [Rename] substitutes for. The zero [ItemRef] has none — a reference always
// carries at least one name, and a value that does not is one nobody read.
func (r ItemRef) Name() string {
	if len(r.Path) == 0 {
		return ""
	}

	return r.Path[len(r.Path)-1]
}

// String renders the reference the way a layout writes it, which is what a
// diagnostic naming an item quotes.
func (r ItemRef) String() string {
	return "(item " + strings.Join(append([]string{r.Record}, r.Path...), " ") + ")"
}

// identity is what makes two references the same item.
//
// It is the spelling, and that is exact rather than approximate: SPEC.md's "An
// item reference" requires the path to be complete rather than qualified in
// COBOL's OF/IN sense, and gives as one of its two reasons that "a complete path
// has exactly one spelling". Two references to one item are therefore the same
// text, so text is an identity here and would not have been under qualification.
func (r ItemRef) identity() string { return r.String() }

// readItemRef reads an `(item …)` form.
//
// Everything about the shape is checked, because this is the only reading of a
// reference in this package and a caller handed an [ItemRef] is entitled to
// assume its record and its path are names somebody wrote.
func readItemRef(node layout.Node) (ItemRef, error) {
	form, ok := node.(layout.Form)
	if !ok {
		return ItemRef{}, &ItemReferenceError{Pos: node.Position(), Found: describe(node)}
	}

	if form.Tag != tagItem {
		return ItemRef{}, &ItemReferenceError{Pos: form.TagPos, Found: "form " + quote(form.Tag)}
	}

	if len(form.Elements) < 2 {
		found := "a reference naming nothing"
		if len(form.Elements) == 1 {
			found = "a reference naming a record and no item below it"
		}

		return ItemRef{}, &ItemReferenceError{Pos: form.Pos, Found: found}
	}

	ref := ItemRef{Pos: form.Pos}

	for i, element := range form.Elements {
		symbol, ok := element.(layout.Symbol)
		if !ok {
			return ItemRef{}, &ItemReferenceError{Pos: element.Position(), Found: describe(element)}
		}

		if i == 0 {
			ref.Record = symbol.Value

			continue
		}

		ref.Path = append(ref.Path, symbol.Value)
	}

	return ref, nil
}
