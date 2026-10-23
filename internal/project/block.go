// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package project

import (
	"errors"

	cobol "github.com/Zaba505/cobol-go"
	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/definitions"
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
	"github.com/Zaba505/cpybkc/internal/layoutschema"
	"github.com/Zaba505/cpybkc/internal/resolve"
)

// judged is every `definitions-block` of a layout, read, judged and built
// once, by its name: the entries it holds, or nil where a fault inside it has
// been reported.
type judged map[string][]*cobol.DataDescriptionEntry

// judgeBlocks reads, judges and builds every block of a layout, once each
// however many records name it (docs/layout/SPEC.md, "What a record may
// reach").
//
// The stages are a definitions file's after its body check, in its order, and
// each stops the block where it reports anything: the entry reader and the
// judge (#400, #401), then `copybook.Build`, once, so that a fault in how the
// entries nest is reported once for the block rather than once per record
// building a tree out of it. The body check itself is not repeated. A block is
// a form of the layout, and the run's whole-layout schema check has already
// held its every entry to the same declarations a file's body check reads
// (#397) — a layout it reported anything about never reaches here.
//
// Every block is judged, including one no record names. Such a block is an
// error at its name, which [layoutmodel.ReadRecords] reports, and its entries
// are still judged in full so that a fault inside it is reported beside that
// one rather than on the run after the block is first named
// (docs/layout/SPEC.md, "A block's name, and a block no record names").
//
// Each fault is reported once for its block, with a note at every record naming
// the block, as a fault inside a definitions file is.
func judgeBlocks(file *layout.File, blocks []layoutmodel.Block) (judged, error) {
	var faults diag.List

	named := blockNamings(file)
	read := make(judged, len(blocks))

	for _, block := range blocks {
		entries, errs := judgeBlock(block)

		for _, fault := range errs {
			faults.Fail(&DefinitionsFaultError{
				Err:     fault,
				Kind:    layoutmodel.DefinitionsBlockSource,
				NamedBy: named[block.Name],
			})
		}

		read[block.Name] = entries
	}

	return read, faults.Err()
}

// judgeBlock reads one block's entries, judges them and builds them once, and
// returns them, or each fault it found on its own.
func judgeBlock(block layoutmodel.Block) ([]*cobol.DataDescriptionEntry, []error) {
	entries, err := definitions.Read(block.Entries)
	if err != nil {
		return nil, split(err)
	}

	built, err := definitions.Judge(entries)
	if err != nil {
		return nil, split(err)
	}

	if _, err := copybook.Build(built); err != nil {
		return nil, []error{buildFault(blockAt(block.Name, block.Pos.File), err)}
	}

	return built, nil
}

// blockAt is the source of a record bound to the block named name, in the
// layout the run opened at path — the file every position in the block is in.
func blockAt(name, path string) resolve.Source {
	return resolve.Source{Kind: layoutmodel.DefinitionsBlockSource, Spelling: name, File: path}
}

// blockNamings is every record naming each block, by the block's name, in the
// order the layout writes them.
//
// It is read leniently, off the forms rather than off
// [layoutmodel.ReadRecords]' model, for two reasons. A fault inside a block is
// reported whether or not the layout's records read — a block no record names
// is itself a fault [layoutmodel.ReadRecords] reports, and its entries are
// judged beside it — and the schema check's faults inside a block are reported
// before any reader has run. A record counts as naming a block whenever its
// source is written `(definitions <symbol> …)`, which is the rule
// [layoutmodel.ReadRecords] reports an unnamed block by.
func blockNamings(file *layout.File) map[string][]Naming {
	named := make(map[string][]Naming)

	for _, form := range file.Forms {
		if form.Tag != "record" || len(form.Elements) < 2 {
			continue
		}

		record, ok := form.Elements[0].(layout.Symbol)
		if !ok {
			continue
		}

		source, ok := form.Elements[1].(layout.Form)
		if !ok || source.Tag != "definitions" || len(source.Elements) == 0 {
			continue
		}

		if block, ok := source.Elements[0].(layout.Symbol); ok {
			named[block.Value] = append(named[block.Value], Naming{Record: record.Value, Pos: span(source.Pos)})
		}
	}

	return named
}

// schemaFaults is what the whole-layout schema check reported, with each fault
// inside a `definitions-block` reported as a fault in that block: once, at its
// span in the layout, in the schema's words — the same words the body check
// uses for the same fault in a definitions file — and with a note at each
// record naming the block, as the body check's faults in a file carry one at
// each record naming the file.
//
// A fault is inside a block when the last top-level form beginning at or before
// its span is the block. Top-level forms do not overlap, and nothing a
// diagnostic points at lies between two of them, so that is the form holding
// it.
func schemaFaults(file *layout.File, diagnostics []layoutschema.Diagnostic) error {
	named := blockNamings(file)
	faults := make([]error, 0, len(diagnostics))

	for _, d := range diagnostics {
		block, ok := blockHolding(file, d.Span)
		if !ok {
			faults = append(faults, d)

			continue
		}

		faults = append(faults, &DefinitionsFaultError{
			Err:     d,
			Kind:    layoutmodel.DefinitionsBlockSource,
			NamedBy: named[block],
		})
	}

	return errors.Join(faults...)
}

// blockHolding is the name of the `definitions-block` at's span is inside, if
// it is inside one.
func blockHolding(file *layout.File, at diag.Span) (string, bool) {
	if at.File != file.Name || at.Line == 0 {
		return "", false
	}

	var holding *layout.Form

	for i, form := range file.Forms {
		if form.Pos.Line > at.Line || (form.Pos.Line == at.Line && form.Pos.Column > at.Column) {
			break
		}

		holding = &file.Forms[i]
	}

	if holding == nil || holding.Tag != layoutschema.BlockForm || len(holding.Elements) == 0 {
		return "", false
	}

	name, ok := holding.Elements[0].(layout.Symbol)
	if !ok {
		return "", false
	}

	return name.Value, true
}
