// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"errors"
	"slices"

	"github.com/Zaba505/cpybkc/internal/layout"
)

// The tag this half of the record definitions layer reads. `record` is read for
// its name alone, by [recordNames], because a rename is rooted at one.
const tagRename = "rename"

// Rename is one `rename` form: an item, and the name substituted for the one
// the copybook gave it.
//
// docs/layout/SPEC.md's "A rename substitutes a name, and keeps the original"
// is the whole of it. Both names survive — [Rename.Original] is the copybook's
// and [Rename.Substitute] is the adopter's — because docs/ir/SPEC.md's "Names"
// carries the substitute *beside* the original rather than in place of it, so
// that generated code can still point back at the copybook it came from. There
// is nowhere in this model for a name to be replaced.
//
// The substitute is language-neutral and is carried verbatim. Casing, reserved
// words and what to do about a name that is not an identifier in some target
// language are a generator's (#30, #50), and applying any of them here would
// put one language's rules in the layer every language's generator reads.
type Rename struct {
	// Pos is the `rename` form.
	Pos layout.Pos

	// Item is what is renamed. It names its target in full: a reference is
	// rooted at a record and carries one name per level down to the item, which
	// is what makes it an identity where a bare name is not — duplicate data
	// names are legal COBOL, and a rename that named one would not say which
	// item it meant.
	Item ItemRef

	// Substitute is the name to carry beside the original, exactly as the
	// layout wrote it.
	Substitute string

	// SubstitutePos is the string itself, which is what a diagnostic about the
	// name rather than about the item points at.
	SubstitutePos layout.Pos
}

// Original is the name the copybook gives the renamed item, which a substitute
// stands beside and never replaces.
func (r Rename) Original() string { return r.Item.Name() }

// ReadRenames reads the renames out of a parsed layout, in the order the layout
// writes them.
//
// It reports every fault it finds, joined, and returns nothing when it found
// one, for [ReadProfile]'s reason: a rename an adopter cannot act on is worse
// than none. A layout carrying no `rename` form carries no renames and is not a
// fault — the form's arity is zero or more.
//
// What it enforces is what a declaration cannot state and a copybook is not
// needed for: that a rename names its target as a reference rather than as a
// bare name, that the record it is rooted at is one the layout defines, that at
// most one rename names a given item, and the collisions decidable from the
// layout alone — two renames substituting one name for two items under one
// parent, and a substitute equal to the name of an item the layout itself
// references under that parent. The rest of the collision rule needs the
// copybook's sibling list and is `resolve`'s, as is whether the path names an
// item at all (#31, #32).
//
// Nothing here restricts what a rename may name below the record. The reference
// **MAY** name a group, and **MAY** name an item that repeats or sit inside
// one, in which case the substitute is the name of the item and reaches every
// occurrence of it: a rename is a name, and a name is per item rather than per
// occurrence. Whether a given path is either of those needs the copybook, so a
// rule keyed on it could not be enforced here even if the format had one.
//
// What a rename cannot name is the record's top-level item, and that is the
// reference grammar's rather than this reader's: a path carries the names below
// the top-level item and the `record` form has already stated that one.
//
// Top-level forms belonging to other layers are not read here and are not
// faults, but their item references are: an item the layout names anywhere is
// an item the copybook has, which is what makes a substitute colliding with one
// answerable without reading the copybook.
func ReadRenames(file *layout.File) ([]Rename, error) {
	read := &renameReader{records: recordNames(file), named: itemsNamed(file)}

	var renames []Rename

	for _, form := range file.Forms {
		if form.Tag != tagRename {
			continue
		}

		rename, ok := read.rename(renames, form)
		if !ok {
			continue
		}

		renames = append(renames, rename)
	}

	if len(read.errs) > 0 {
		return nil, errors.Join(read.errs...)
	}

	return renames, nil
}

// renameReader holds the state one [ReadRenames] accumulates.
type renameReader struct {
	faults

	// records are the records the layout defines, which is what makes a rename
	// rooted at a record nobody defined answerable.
	records []recordDefinition

	// named is every item reference the layout writes, which is the sibling set
	// this layer has: an item nothing in the layout mentions is one only the
	// copybook knows about.
	named []ItemRef

	// renamed is where each item was first renamed, which makes a second rename
	// on one item reportable against the first.
	renamed map[string]layout.Pos
}

// rename reads one `rename` form, against the renames already read.
func (r *renameReader) rename(read []Rename, form layout.Form) (Rename, bool) {
	if len(form.Elements) != 2 {
		r.fail(&RenameFormError{Pos: form.Pos, Found: renameShortfall(form.Elements)})

		return Rename{}, false
	}

	item, err := readItemRef(form.Elements[0])
	if err != nil {
		r.fail(err)

		return Rename{}, false
	}

	name, ok := form.Elements[1].(layout.Text)
	if !ok {
		r.fail(&RenameFormError{Pos: form.Elements[1].Position(), Found: describe(form.Elements[1])})

		return Rename{}, false
	}

	rename := Rename{Pos: form.Pos, Item: item, Substitute: name.Value, SubstitutePos: name.Pos}

	// Every check below is run whatever the ones before it said, for the reason
	// a discriminator's strategy is read past a misspelled record name: a rename
	// rooted at a record nobody defined and substituting a name a sibling
	// carries is two things to fix rather than one to discover on the next run.
	sound := r.target(form, item)

	if name.Value == "" {
		r.fail(&EmptyRenameError{Pos: name.Pos, Item: item})

		// A name of no characters collides with nothing, and every message the
		// collision rules could produce about it would name the empty string.
		return Rename{}, false
	}

	return rename, r.collisions(read, rename) && sound
}

// target holds the item a rename names to what can be checked without a
// copybook: the record it is rooted at, and the renames already read.
func (r *renameReader) target(form layout.Form, item ItemRef) bool {
	sound := true

	if !slices.ContainsFunc(r.records, func(record recordDefinition) bool { return record.name == item.Record }) {
		r.fail(&UnknownRecordError{Pos: item.Pos, Record: item.Record, Form: form.Tag})

		sound = false
	}

	if first, already := r.renamed[item.identity()]; already {
		r.fail(&DuplicateRenameError{Pos: form.Pos, First: first, Item: item})

		return false
	}

	if r.renamed == nil {
		r.renamed = make(map[string]layout.Pos)
	}

	r.renamed[item.identity()] = form.Pos

	return sound
}

// collisions reports a substitute that another name under one parent already
// answers to.
//
// Two of them are decidable from the layout alone. A name substituted for two
// items under one parent is one, and a name equal to the name of an item the
// layout itself references under that parent is the other — an item a layout
// names is an item the copybook has, whatever the copybook turns out to say
// about the rest of them.
//
// The second holds even where that sibling is itself renamed. The original is
// carried beside the substitute rather than in place of it
// (docs/ir/SPEC.md's "Names"), so a renamed sibling still answers to the name
// the copybook gave it, and two nodes under one parent answering to one name is
// the ambiguity a rename exists to remove. Swapping two names is spelled by
// renaming both to names nothing under that parent carries.
//
// An item is not its own sibling under either check. A rename substituting the
// name its target already has is admitted — it says the same thing twice and
// leaves every name where it was — and a second rename on one item is reported
// as the duplicate it is and not as a collision between an item and itself.
func (r *renameReader) collisions(read []Rename, rename Rename) bool {
	sound := true

	// An item is not its own sibling here either: two renames on one item are a
	// [DuplicateRenameError] already, and a collision message about them would
	// name that item twice and read as though two items were involved.
	earlier := slices.IndexFunc(read, func(other Rename) bool {
		return other.Substitute == rename.Substitute &&
			other.Item.identity() != rename.Item.identity() &&
			siblings(other.Item, rename.Item)
	})
	if earlier >= 0 {
		r.fail(&RenameCollisionError{
			Pos:   rename.SubstitutePos,
			First: read[earlier].SubstitutePos,
			Items: [2]ItemRef{read[earlier].Item, rename.Item},
			Name:  rename.Substitute,
		})

		sound = false
	}

	sibling := slices.IndexFunc(r.named, func(other ItemRef) bool {
		return siblings(other, rename.Item) &&
			other.identity() != rename.Item.identity() &&
			other.Name() == rename.Substitute
	})
	if sibling >= 0 {
		r.fail(&RenameShadowsSiblingError{
			Pos:     rename.SubstitutePos,
			First:   r.named[sibling].Pos,
			Item:    rename.Item,
			Sibling: r.named[sibling],
			Name:    rename.Substitute,
		})

		sound = false
	}

	return sound
}

// siblings reports whether two references name items under one parent.
//
// It is the reference's spelling and nothing else, which is what makes it
// answerable here: a path is complete rather than qualified, so two items are
// siblings exactly when they are reached through the same record by the same
// names down to the last one. A reference carrying no name below the record is
// nobody's sibling — no reader hands one back, and the zero value is not an
// item.
func siblings(a, b ItemRef) bool {
	if len(a.Path) == 0 || len(b.Path) == 0 || a.Record != b.Record {
		return false
	}

	return slices.Equal(a.Path[:len(a.Path)-1], b.Path[:len(b.Path)-1])
}

// itemsNamed gathers every item reference the layout writes, anywhere in it, in
// source order.
//
// It walks every top-level form rather than the record definitions layer's,
// because what it is after is the items a layout is *evidence* for: a
// discriminator's target, a `times`'s count and an override's item are all
// items the copybook declares, and a rename substituting one of their names
// under one parent collides with it as surely as with another rename's target.
//
// A reference this cannot read is skipped rather than reported. Every position
// admitting one belongs to a layer that reads it, and a second message about it
// here would name the same line twice.
func itemsNamed(file *layout.File) []ItemRef {
	var named []ItemRef

	for _, form := range file.Forms {
		layout.Walk(form, func(node layout.Node) bool {
			inner, ok := node.(layout.Form)
			if !ok || inner.Tag != tagItem {
				return true
			}

			if ref, err := readItemRef(inner); err == nil {
				named = append(named, ref)
			}

			// A reference carries names and nothing else, so there is nothing
			// below one to walk into.
			return false
		})
	}

	return named
}

// renameShortfall names what a `rename` form carries where an item and a name
// belong.
func renameShortfall(elements []layout.Node) string {
	switch {
	case len(elements) == 0:
		return "no value"
	case len(elements) > 2:
		return "several"
	default:
		if _, ok := elements[0].(layout.Text); ok {
			return "a name and no item"
		}

		return "an item and no name"
	}
}
