// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package project

import (
	"bytes"
	"os"
	"path/filepath"

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

		// A copybook holding a REDEFINES outside a repeating group resolves to
		// one record type per combination of alternatives, and nothing in the
		// layout format names them apart: a `record` form binds a name to an
		// `01`-level, and every alternative of that level is the same level. So
		// there is no rule for pairing the forms to the alternatives and no
		// spelling that would give two of them two names, and #164 is the story
		// that decides both. Refusing the layout is the honest answer until it
		// does — pairing them by position would be a rule this program invented
		// and an adopter could not read anywhere.
		if len(resolved) != 1 {
			faults.Fail(&AlternativesError{
				Pos:          span(b.Record.Pos),
				Record:       b.Record.Name,
				Path:         b.Record.Path,
				Item:         b.Record.Item,
				Alternatives: len(resolved),
			})

			continue
		}

		records = append(records, assemble.Record{
			Name:     b.Record.Name,
			Copybook: b.Record.Path,
			Resolved: resolved[0],
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

// substitutes resolves each `rename` to the copybook item it names.
//
// They are one list rather than keyed by record because `assemble` takes one:
// a rename reaches every node standing for that item, and an item belongs to
// one record's tree, so the list is already partitioned by the fields in it.
func (l *layers) substitutes(bound *bindings) []assemble.Rename {
	renames := make([]assemble.Rename, 0, len(l.renames))

	for _, rename := range l.renames {
		item := bound.field(rename.Item)
		if item == nil {
			continue
		}

		renames = append(renames, assemble.Rename{Item: item, Substitute: rename.Substitute})
	}

	return renames
}
