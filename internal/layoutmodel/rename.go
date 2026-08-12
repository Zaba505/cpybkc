// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"slices"

	"github.com/Zaba505/cpybkc/internal/diag"
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

	// Item is what is renamed, where the target is an item. It names its target
	// in full: a reference is rooted at a record and carries one name per level
	// down to the item, which is what makes it an identity where a bare name is
	// not — duplicate data names are legal COBOL, and a rename that named one
	// would not say which item it meant.
	//
	// It is the zero reference where the target is a record; [Rename.Record]
	// says which of the two this is.
	Item ItemRef

	// Record is the record renamed, where the target is a record rather than an
	// item, and is empty otherwise.
	//
	// A record's own name in the IR is the one its copybook `01`-level carries
	// (docs/ir/SPEC.md, "Names"), and that is the one item an item reference
	// cannot reach — the `record` form has already stated it, so a path never
	// repeats it. So the target is written as a bare record name
	// (docs/layout/SPEC.md, "A rename may name a record"), and this is where it
	// lands.
	Record string

	// RecordPos is the record name itself, and is the zero position for an item
	// rename.
	RecordPos layout.Pos

	// Substitute is the name to carry beside the original, exactly as the
	// layout wrote it.
	Substitute string

	// SubstitutePos is the string itself, which is what a diagnostic about the
	// name rather than about the target points at.
	SubstitutePos layout.Pos
}

// NamesRecord reports whether the rename names a record rather than an item
// inside one.
func (r Rename) NamesRecord() bool { return r.Record != "" }

// Original is the name the copybook gives the renamed item, which a substitute
// stands beside and never replaces.
//
// A record rename has none here: the name it stands beside is the record's
// `01`-level, which the `copybook` child names and this model does not carry.
func (r Rename) Original() string {
	if r.NamesRecord() {
		return ""
	}

	return r.Item.Name()
}

// target is what makes two renames name one thing: the reference's spelling for
// an item, and the record name for a record.
//
// The `record ` prefix is what keeps the two apart, and it is load-bearing
// rather than decorative — one map holds both, and without it a rename on a
// record would read as a rename on an item spelled the same way. It is not
// enough that an item's identity happens to open with `(item `: that is
// [ItemRef.String]'s business, and a key relying on it would break here when
// that rendering changed.
func (r Rename) target() string {
	if r.NamesRecord() {
		return "record " + r.Record
	}

	return r.Item.identity()
}

// ReadRenames reads the renames out of a parsed layout, in the order the layout
// writes them.
//
// It reports every fault it finds, joined, and returns nothing when it found
// one, for [ReadProfile]'s reason: a rename an adopter cannot act on is worse
// than none. A layout carrying no `rename` form carries no renames and is not a
// fault — the form's arity is zero or more.
//
// What it enforces is what a declaration cannot state and a copybook is not
// needed for: that a rename names an item as a reference rather than as a bare
// name, that the record it names or is rooted at is one the layout defines, that
// at most one rename names a given item or a given record, and the collisions
// decidable from the layout alone — two renames substituting one name for two
// items under one parent or for two records, a substitute equal to the name of
// an item the layout itself references under that parent, and a substitute equal
// to the top-level item another record is bound to. The rest of the collision
// rule needs the copybook's sibling list and is `resolve`'s, as is whether the
// path names an item at all (#31, #32).
//
// A rename **MAY** name a record instead of an item, and that spelling is a bare
// record name rather than a reference (docs/layout/SPEC.md, "A rename may name a
// record"). It is not an exception to the paragraph below: a record's own name
// is stated by its `record` form, so there is nothing about it for a path to be
// ambiguous over.
//
// Nothing here restricts what a rename may name below the record. The reference
// **MAY** name a group, and **MAY** name an item that repeats or sit inside
// one, in which case the substitute is the name of the item and reaches every
// occurrence of it: a rename is a name, and a name is per item rather than per
// occurrence. Whether a given path is either of those needs the copybook, so a
// rule keyed on it could not be enforced here even if the format had one.
//
// What a *reference* cannot name is the record's top-level item, and that is the
// reference grammar's rather than this reader's: a path carries the names below
// the top-level item and the `record` form has already stated that one. Which is
// why renaming that item is spelled by naming the record.
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

	if read.Failed() {
		return nil, read.Err()
	}

	return renames, nil
}

// renameReader holds the state one [ReadRenames] accumulates.
type renameReader struct {
	diag.List

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
		r.Fail(&RenameFormError{Pos: form.Pos, Found: renameShortfall(form.Elements)})

		return Rename{}, false
	}

	rename := Rename{Pos: form.Pos}

	// A record name is a symbol where an item reference is a form, so which of
	// the two spellings was written is decided by the shape of the first
	// element and never by looking a name up. A symbol that is not a record the
	// layout defines is a rename on a record that is not there, which is the
	// message an adopter needs; reading it as a malformed item reference would
	// send them to the reference grammar instead.
	if symbol, ok := form.Elements[0].(layout.Symbol); ok {
		rename.Record, rename.RecordPos = symbol.Value, symbol.Pos
	} else {
		item, err := readItemRef(form.Elements[0])
		if err != nil {
			r.Fail(err)

			return Rename{}, false
		}

		rename.Item = item
	}

	name, ok := form.Elements[1].(layout.Text)
	if !ok {
		r.Fail(&RenameFormError{Pos: form.Elements[1].Position(), Found: describe(form.Elements[1])})

		return Rename{}, false
	}

	rename.Substitute, rename.SubstitutePos = name.Value, name.Pos

	// Every check below is run whatever the ones before it said, for the reason
	// a discriminator's strategy is read past a misspelled record name: a rename
	// rooted at a record nobody defined and substituting a name a sibling
	// carries is two things to fix rather than one to discover on the next run.
	sound := r.target(form, rename)

	if name.Value == "" {
		r.Fail(&EmptyRenameError{Pos: name.Pos, Item: rename.Item, Record: rename.Record})

		// A name of no characters collides with nothing, and every message the
		// collision rules could produce about it would name the empty string.
		return Rename{}, false
	}

	return rename, r.collisions(read, rename) && sound
}

// target holds what a rename names to what can be checked without a copybook:
// that the record it names, or is rooted at, is one the layout defines, and that
// nothing has been renamed twice.
func (r *renameReader) target(form layout.Form, rename Rename) bool {
	sound := true

	named := rename.Record
	if !rename.NamesRecord() {
		named = rename.Item.Record
	}

	if !slices.ContainsFunc(r.records, func(record recordDefinition) bool { return record.name == named }) {
		pos := rename.RecordPos
		if !rename.NamesRecord() {
			pos = rename.Item.Pos
		}

		r.Fail(&UnknownRecordError{Pos: pos, Record: named, Form: form.Tag})

		sound = false
	}

	if first, already := r.renamed[rename.target()]; already {
		if rename.NamesRecord() {
			r.Fail(&DuplicateRecordRenameError{Pos: form.Pos, First: first, Record: rename.Record})
		} else {
			r.Fail(&DuplicateRenameError{Pos: form.Pos, First: first, Item: rename.Item})
		}

		return false
	}

	if r.renamed == nil {
		r.renamed = make(map[string]layout.Pos)
	}

	r.renamed[rename.target()] = form.Pos

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
	if rename.NamesRecord() {
		return r.recordCollisions(read, rename)
	}

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
		r.Fail(&RenameCollisionError{
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
		r.Fail(&RenameShadowsSiblingError{
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

// recordCollisions reports a substitute for a record's name that another record
// already answers to.
//
// A record node has no parent, so there are no siblings to be ambiguous among
// and the sibling rules above do not carry over. What carries over is the
// ambiguity they remove, between the names two record types answer to in one
// descriptor, and two halves of it are decidable from the layout alone: a name
// substituted for two records, and a name equal to the `01`-level another record
// is bound to — which is the name that record answers to, because the original
// is carried beside a substitute rather than in place of it (docs/ir/SPEC.md,
// "Names").
//
// A record is not its own collision under either check. A rename substituting
// the name of the `01`-level its own record is bound to says the same thing
// twice and leaves every name where it was, and a second rename on one record is
// reported as the duplicate it is.
func (r *renameReader) recordCollisions(read []Rename, rename Rename) bool {
	sound := true

	earlier := slices.IndexFunc(read, func(other Rename) bool {
		return other.NamesRecord() &&
			other.Substitute == rename.Substitute &&
			other.Record != rename.Record
	})
	if earlier >= 0 {
		r.Fail(&RecordRenameCollisionError{
			Pos:     rename.SubstitutePos,
			First:   read[earlier].SubstitutePos,
			Records: [2]string{read[earlier].Record, rename.Record},
			Name:    rename.Substitute,
		})

		sound = false
	}

	bound := slices.IndexFunc(r.records, func(record recordDefinition) bool {
		return record.name != rename.Record && record.item == rename.Substitute
	})
	if bound >= 0 {
		r.Fail(&RecordRenameShadowsError{
			Pos:    rename.SubstitutePos,
			First:  r.records[bound].itemPos,
			Record: rename.Record,
			Other:  r.records[bound].name,
			Name:   rename.Substitute,
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

// renameShortfall names what a `rename` form carries where a target and a name
// belong.
func renameShortfall(elements []layout.Node) string {
	switch {
	case len(elements) == 0:
		return "no value"
	case len(elements) > 2:
		return "several"
	default:
		if _, ok := elements[0].(layout.Text); ok {
			return "a name and no target"
		}

		return "a target and no name"
	}
}
