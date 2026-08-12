// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package project

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"

	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/assemble"
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
	"github.com/Zaba505/cpybkc/internal/manifest"
	"github.com/Zaba505/cpybkc/internal/resolve"
	"github.com/Zaba505/cpybkc/irpb"
)

// resolveDescriptor reads the layout the manifest names and resolves it, with
// the copybooks it names, into the run's one descriptor.
//
// The stages run in the order docs/cli/SPEC.md's diagnostics come out in, and
// each is complete before the next begins: every layer of the layout is read
// before a copybook is opened, every copybook is opened before an item
// reference is looked up, and every record is resolved before the sequencing
// expression is compiled. That is not an optimisation. A stage reports every
// fault it found rather than the first, so finishing one before starting the
// next is what turns a layout that is wrong in six places into one editing
// session — and running a later stage over a model an earlier one rejected
// would report faults invented by this program rather than found in the file.
func resolveDescriptor(dir string, m *manifest.Manifest) (*irpb.Descriptor, error) {
	// docs/cli/SPEC.md: a relative path stated in a file is resolved against
	// the directory of that file, and `layout` is stated in the manifest.
	path := at(dir, m.Layout)

	file, err := readLayout(path, m.Layout)
	if err != nil {
		return nil, err
	}

	read, err := readLayers(file)
	if err != nil {
		return nil, err
	}

	// A layout's `copybook` path is relative to the **layout**, not to the
	// manifest. That is what makes a layout portable: a layout and the
	// copybooks it names travel together, so a project vendoring somebody
	// else's layout does not have to rewrite the paths inside it.
	bound, err := bind(filepath.Dir(path), read.records)
	if err != nil {
		return nil, err
	}

	return read.assemble(bound)
}

// readLayout opens a layout and parses it.
//
// The name every position in it carries is the path as this run opened it,
// because that is the path an adopter can hand to an editor. The path the
// manifest spelled is what a diagnostic about the file itself names, for the
// reason a copybook's is: a message naming a path nobody wrote sends the reader
// looking for a file they have not got.
func readLayout(path, spelling string) (*layout.File, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, &MissingLayoutError{Path: spelling, LookedIn: absolute(path), Err: err}
	}

	return layout.Parse(path, bytes.NewReader(src))
}

// layers is a layout, read: every layer of it, and nothing resolved against a
// copybook.
type layers struct {
	records        []layoutmodel.Record
	profile        *layoutmodel.Profile
	framing        *layoutmodel.Framing
	discrimination *layoutmodel.Discrimination
	sequence       *layoutmodel.Sequence
	renames        []layoutmodel.Rename
	reading        layoutmodel.Reading
}

// readLayers reads every layer of a parsed layout.
//
// All seven are read whatever the ones before them said, and the faults are
// reported together. The layers vary independently — docs/layout/SPEC.md's
// "Five layers rather than one schema" is that argument — so a layout with a
// misspelled charset and an unsequenced record is wrong in two layers at once,
// and a reader that stopped at the first would be run once per layer.
func readLayers(file *layout.File) (*layers, error) {
	var (
		read   layers
		faults diag.List
		err    error
	)

	read.records, err = layoutmodel.ReadRecords(file)
	faults.Fail(err)

	read.profile, err = layoutmodel.ReadProfile(file)
	faults.Fail(err)

	read.framing, err = layoutmodel.ReadFraming(file)
	faults.Fail(err)

	read.discrimination, err = layoutmodel.ReadDiscrimination(file)
	faults.Fail(err)

	read.sequence, err = layoutmodel.ReadSequence(file)
	faults.Fail(err)

	read.renames, err = layoutmodel.ReadRenames(file)
	faults.Fail(err)

	read.reading, err = layoutmodel.ReadCopybookReading(file)
	faults.Fail(err)

	if faults.Failed() {
		return nil, faults.Err()
	}

	return &read, nil
}

// assemble resolves every record against its copybook, compiles the sequencing
// expression and assembles the descriptor.
func (l *layers) assemble(bound *bindings) (*irpb.Descriptor, error) {
	overrides, flat := l.overrides(bound)
	redefines := l.redefines(bound)
	renames := l.substitutes(bound)

	if bound.Failed() {
		return nil, bound.Err()
	}

	records, sequenced, err := l.resolve(bound, overrides, redefines)
	if err != nil {
		return nil, err
	}

	automaton, err := resolve.CompileSequence(resolve.Sequencing{
		Sequence:          l.sequence,
		Dialect:           dialect(),
		Reading:           l.reading,
		Framing:           l.framing,
		Encoding:          l.profile.Axes,
		EncodingOverrides: flat,
		Records:           sequenced,
	})
	if err != nil {
		return nil, err
	}

	return assemble.Assemble(assemble.Options{
		Framing:   l.framing,
		Automaton: automaton,
		Records:   records,
		Renames:   renames,
	})
}

// resolve resolves each record against the copybook item it is bound to.
//
// Every record is resolved and every fault reported, rather than the first: a
// copybook and the layout over it go wrong in the same way in several records
// at once, which is the reason `resolve` itself collects.
func (l *layers) resolve(
	bound *bindings,
	overrides map[string][]resolve.EncodingOverride,
	redefines map[string][]resolve.Redefine,
) ([]assemble.Record, []resolve.SequencedRecord, error) {
	var faults diag.List

	records := make([]assemble.Record, 0, len(bound.bound))
	sequenced := make([]resolve.SequencedRecord, 0, len(bound.bound))

	for _, b := range bound.bound {
		resolved, err := resolve.Resolve(b.Item, resolve.Options{
			Copybook:          b.Record.Path,
			Dialect:           dialect(),
			Framing:           l.framing,
			Reading:           l.reading,
			Redefines:         redefines[b.Record.Name],
			Encoding:          l.profile.Axes,
			EncodingOverrides: overrides[b.Record.Name],
		})
		if err != nil {
			faults.Fail(err)

			continue
		}

		chosen := l.choose(bound, &faults, b, resolved)
		if chosen == nil {
			continue
		}

		records = append(records, assemble.Record{
			Name:     b.Record.Name,
			Copybook: b.Record.Path,
			Resolved: chosen,
		})

		sequenced = append(sequenced, resolve.SequencedRecord{
			Name:          b.Record.Name,
			Copybook:      b.Record.Path,
			Item:          b.Item,
			Discriminator: l.discriminator(b.Record.Name),
		})
	}

	if faults.Failed() {
		return nil, nil, faults.Err()
	}

	return records, sequenced, nil
}

// choose picks the one record type a `record` form means out of what its
// copybook resolved to.
//
// A copybook holding a REDEFINES outside a repeating group resolves to one
// record type per combination of alternatives (docs/ir/SPEC.md, "Members never
// overlap, and `REDEFINES` is resolved away"), and the `alternative` children
// are where the layout says which of them the form is (docs/layout/SPEC.md,
// "Which alternative a record is", #164).
//
// The match is on the *set* of items chosen and not on their order. Two
// redefines describe two distinct runs of bytes, so the order the children are
// written in decides nothing, and matching by position would make a layout mean
// something different when its two lines were swapped.
//
// A copybook with no redefine at all resolves to one record type choosing
// nothing, and the empty set matches it — so an ordinary record needs no
// `alternative` child and a form carrying one over such a copybook is reported
// by the same message a wrong choice is. Nothing here guesses: a record whose
// children match no combination is refused, with every combination named, rather
// than resolved to whichever one this program met first.
func (l *layers) choose(
	bound *bindings,
	faults *diag.List,
	b binding,
	resolved []*resolve.Record,
) *resolve.Record {
	chosen := make([]*copybook.Field, 0, len(b.Record.Alternatives))

	for _, ref := range b.Record.Alternatives {
		item := bound.field(ref)
		if item == nil {
			// The reference names no item, and `bind` has already said so
			// against the copybook it looked in. A second message here would
			// name the same line twice.
			return nil
		}

		chosen = append(chosen, item)
	}

	for _, candidate := range resolved {
		if sameAlternatives(candidate.Alternatives, chosen) {
			return candidate
		}
	}

	faults.Fail(&AlternativesError{
		Pos:      span(b.Record.Pos),
		Record:   b.Record.Name,
		Path:     b.Record.Path,
		Item:     b.Record.Item,
		Chosen:   alternativeNames(chosen),
		Resolved: combinations(resolved),
	})

	return nil
}

// sameAlternatives reports whether two choices are the same set of items.
//
// Pointer identity is the test because both sides come out of one item tree:
// `bind` builds a fresh tree per record and resolves that record's references
// against it, so two references to one item are one pointer and two items with
// one COBOL name are two. Comparing names would make a copybook's duplicate data
// names — legal COBOL — into one alternative.
func sameAlternatives(resolved, chosen []*copybook.Field) bool {
	if len(resolved) != len(chosen) {
		return false
	}

	for _, item := range chosen {
		if !slices.Contains(resolved, item) {
			return false
		}
	}

	return true
}

// alternativeNames is what a diagnostic calls a set of chosen items.
func alternativeNames(items []*copybook.Field) []string {
	names := make([]string, 0, len(items))

	for _, item := range items {
		names = append(names, itemName(item))
	}

	return names
}

// combinations is every set of alternatives a copybook resolved to, in the order
// `resolve` enumerated them, which is what a record choosing none of them is
// offered instead.
func combinations(resolved []*resolve.Record) [][]string {
	found := make([][]string, 0, len(resolved))

	for _, record := range resolved {
		found = append(found, alternativeNames(record.Alternatives))
	}

	return found
}

// itemName is what a diagnostic calls one copybook item.
func itemName(item *copybook.Field) string {
	if item == nil || item.Filler || item.Name == "" {
		return "a FILLER item"
	}

	return item.Name
}

// discriminator is the strategy the layout's `discriminate` form chose for a
// record.
//
// There is exactly one per record on a [layoutmodel.Discrimination] handed
// back — a record with none and a record with two are both faults the reader
// reported — so the zero strategy this returns for a name it does not hold is
// unreachable from a layout that was read soundly.
func (l *layers) discriminator(record string) layoutmodel.Strategy {
	for _, discriminator := range l.discrimination.Records {
		if discriminator.Record == record {
			return discriminator.Strategy
		}
	}

	return layoutmodel.Strategy{}
}

// overrides resolves each `encoding-override` to the copybook item it names,
// both keyed by the record its reference is rooted at and as one list in the
// order the layout writes them.
//
// Resolving a record takes the keyed form because an item tree belongs to one
// record here: two records over one `01`-level have two trees, so an override
// written for one of them names a field the other's tree does not hold. That is
// docs/layout/SPEC.md's independence made mechanical — "neither reaches the
// other".
//
// Compiling the sequencing expression takes the flat form, and it is built here
// rather than by walking the map afterwards because a map has no order: a list
// assembled from one would make the descriptor a function of Go's map
// iteration, which is the one thing a generated file library cannot be.
func (l *layers) overrides(bound *bindings) (map[string][]resolve.EncodingOverride, []resolve.EncodingOverride) {
	var flat []resolve.EncodingOverride

	overrides := make(map[string][]resolve.EncodingOverride)

	for _, override := range l.profile.Overrides {
		item := bound.field(override.Item)
		if item == nil {
			continue
		}

		resolved := resolve.EncodingOverride{Item: item, Axes: override.Axes}

		overrides[override.Item.Record] = append(overrides[override.Item.Record], resolved)
		flat = append(flat, resolved)
	}

	return overrides, flat
}

// redefines resolves each `discriminate-variant` to the copybook item it names,
// keyed by the record its reference is rooted at.
//
// A variant is a redefine *inside* a repeating group: the alternative is chosen
// once per occurrence rather than once per record, which is why it arrives as
// input to resolving a record instead of multiplying the record types the way a
// redefine outside one does.
func (l *layers) redefines(bound *bindings) map[string][]resolve.Redefine {
	redefines := make(map[string][]resolve.Redefine)

	for _, variant := range l.discrimination.Variants {
		item := bound.field(variant.Variant)
		if item == nil {
			continue
		}

		alternatives := make([]resolve.Alternative, 0, len(variant.Arms))
		for _, arm := range variant.Arms {
			alternatives = append(alternatives, resolve.Alternative{
				Name:      arm.Alternative,
				Predicate: arm.Predicate,
			})
		}

		record := variant.Variant.Record
		redefines[record] = append(redefines[record], resolve.Redefine{Item: item, Alternatives: alternatives})
	}

	return redefines
}

// substitutes resolves each `rename` to what it names: a copybook item, or the
// record type itself.
//
// Every one carries the record it was written under, because a rename is per
// record (docs/layout/SPEC.md, "Many records may name one copybook, and two may
// name one item"). Two records over one `01`-level hold two item trees here, so
// the fields alone would in fact partition — but that is this package's
// arrangement rather than `assemble`'s contract, and a rename saying which
// record it belongs to is the difference between independence that holds and
// independence that happens to (#164).
//
// A rename naming a record contributes no item: the name it stands beside is the
// record node's, which is the `01`-level's, and that is the one item an item
// reference cannot reach.
func (l *layers) substitutes(bound *bindings) []assemble.Rename {
	renames := make([]assemble.Rename, 0, len(l.renames))

	for _, rename := range l.renames {
		if rename.NamesRecord() {
			renames = append(renames, assemble.Rename{
				Record:     rename.Record,
				Substitute: rename.Substitute,
			})

			continue
		}

		item := bound.field(rename.Item)
		if item == nil {
			continue
		}

		renames = append(renames, assemble.Rename{
			Record:     rename.Item.Record,
			Item:       item,
			Substitute: rename.Substitute,
		})
	}

	return renames
}
