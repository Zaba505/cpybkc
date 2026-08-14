// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package scaffold

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	cobol "github.com/Zaba505/cobol-go"
	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/resolve"
)

// Copybook is one `--copybook` value: the path as it was typed, and the bytes
// found at it.
//
// The path is carried as typed and is written into the scaffold's `copybook`
// child unchanged. A layout's own paths are relative to the layout, so a path
// that reached cpybkc from a different directory than the scaffold's is the
// adopter's to correct — and a path cpybkc rewrote on their behalf would be one
// they cannot find in what they typed.
type Copybook struct {
	// Path is the --copybook value, exactly as it was written on the command
	// line.
	Path string

	// Source is the file's bytes.
	Source []byte
}

// Scaffold is the layout scaffold one set of copybooks decides.
//
// It is a value with no filesystem in it: [Scaffold.Bytes] renders it and
// [Write] puts it somewhere, and neither can be reached by accident from the
// other. That is what makes "the file is written in full or not at all" and
// "nothing reaches standard output until the whole scaffold has been derived"
// one property rather than two — there is nothing to write until the derivation
// is over.
type Scaffold struct {
	// records are the record types, in the order the copybooks were named and,
	// within one copybook, in declaration order.
	records []record

	// variants are the redefines inside a repeating group, in the order they
	// were met.
	variants []variant

	// renamed are the records over an 01-level that resolved to more than one
	// record type, which are the ones a rename is raised for.
	renamed []string

	// tables reports that some copybook carries an OCCURS DEPENDING ON, which
	// is the whole of what decides whether a `copybook-reading` is raised.
	tables bool

	// notes are the lines the run owes standard error, in the order the
	// 01-levels they are about were met.
	notes []string
}

// record is one `record` form: what it is called, which copybook and 01-level it
// is over, and which description of each redefined run it means.
type record struct {
	name         string
	path         string
	item         string
	alternatives []reference
}

// variant is one `discriminate-variant` form: the redefined item inside a
// repeating group, and the alternatives an arm is owed for.
type variant struct {
	item reference
	arms []string
}

// reference is an item reference as a layout writes one: rooted at a record, and
// carrying one name per level down to the item with the 01-level's own name not
// repeated (docs/layout/SPEC.md, "An item reference").
type reference struct {
	record string
	path   []string
}

// String renders the reference as the layout format spells it.
func (r reference) String() string {
	return "(item " + strings.Join(append([]string{r.record}, r.path...), " ") + ")"
}

// Notes are the `note:` diagnostics the run owes, one per 01-level that resolved
// to more than one record type.
//
// They are here rather than written from here because docs/cli/SPEC.md puts
// every line cpybkc says about a run on standard error and this package holds
// neither stream. Each names the copybook, the 01-level, how many REDEFINES
// outside a repeating group it carries and how many record types they produce:
// it is the size of the work in front of the reader, which is a thing they have
// to act on rather than discover in the file.
func (s *Scaffold) Notes() []string { return slices.Clone(s.notes) }

// dialect is the compiler whose answers the derivation is read under.
//
// The same one [github.com/Zaba505/cpybkc/internal/project] resolves under, for
// the reason the derivation shares its REDEFINES reading at all: a scaffold
// derived under one dialect and resolved under another would disagree with
// itself about a copybook neither of them changed. What the dialect decides here
// is narrow — whether a redefining item longer than what it redefines is legal —
// and it decides nothing about which forms the scaffold carries.
func dialect() copybook.Dialect { return copybook.IBMEnterprise() }

// Derive reads every copybook and returns the scaffold they decide.
//
// The copybooks are taken in the order they were given, and their 01-levels in
// declaration order, because that is the order the `record` forms are written in
// and a scaffold has to be byte-identical between two runs over one set of
// copybooks.
//
// Every fault it finds is reported rather than the first: a set of copybooks
// goes wrong in the same way in more than one file at once, and a reader that
// stopped would be run once per copybook. Nothing is returned where anything
// failed — a scaffold missing the records of the copybook that could not be read
// is a file whose `sequence` is short and whose incompleteness reads exactly
// like the incompleteness that is deliberate.
func Derive(books []Copybook) (*Scaffold, error) {
	var faults diag.List

	s := new(Scaffold)

	// The names already derived, so that a second record type deriving one of
	// them is refused naming both. It is read and never ranged over, which is
	// what keeps the derivation a function of its inputs.
	claimed := make(map[string]record)

	for _, book := range books {
		items, err := read(book)
		if err != nil {
			faults.Fail(err)

			continue
		}

		for _, item := range items {
			shape, err := resolve.Describe(item, dialect())
			if err != nil {
				faults.Fail(&SourceError{Path: book.Path, Err: err})

				continue
			}

			s.take(&faults, claimed, book, item, shape)
		}
	}

	if faults.Failed() {
		return nil, faults.Err()
	}

	return s, nil
}

// read parses one copybook and returns the 01-levels a `record` form can be
// written over.
//
// Level-77 items and an 01-level with no data-name are not among them. A
// level-77 is a standalone work item rather than a record description, and an
// unnamed 01-level is one no `record` form could name; a copybook holding
// nothing else declares no record, which is reported rather than passed over.
func read(book Copybook) ([]*copybook.Field, error) {
	// Fixed format, which is what a copybook out of a mainframe library is
	// written in and what every other reader in this repository assumes.
	// Nothing states a reference format, and `init` is handed a file rather
	// than a project, so there is nowhere else the answer could come from.
	file, err := cobol.Parse(
		bytes.NewReader(book.Source),
		cobol.WithFragment(),
		cobol.WithSourceFormat(cobol.FixedFormat),
	)
	if err != nil {
		return nil, &SourceError{Path: book.Path, Err: err}
	}

	if file.Fragment == nil {
		return nil, &NoRecordError{Path: book.Path}
	}

	items, err := copybook.Build(file.Fragment.Entries)
	if err != nil {
		return nil, &SourceError{Path: book.Path, Err: err}
	}

	records := make([]*copybook.Field, 0, len(items))

	for _, item := range items {
		if item.Level == 1 && !item.Filler && item.Name != "" {
			records = append(records, item)
		}
	}

	if len(records) == 0 {
		return nil, &NoRecordError{Path: book.Path}
	}

	return records, nil
}

// take turns one 01-level's shape into the forms it decides.
func (s *Scaffold) take(
	faults *diag.List,
	claimed map[string]record,
	book Copybook,
	item *copybook.Field,
	shape resolve.Shape,
) {
	if len(shape.Tables) > 0 {
		s.tables = true
	}

	if unnamed := unreachable(item, shape); unnamed != nil {
		faults.Fail(&UnnamedAlternativeError{
			Path:   book.Path,
			Item:   item.Name,
			Line:   unnamed.Pos.Line,
			Column: unnamed.Pos.Column,
		})

		return
	}

	// More than one record type over one 01-level is what raises a rename and
	// what the run owes a note about. Both are decided once, here, rather than
	// per record: they are properties of the 01-level.
	several := len(shape.Combinations) > 1

	root := ""

	for _, combination := range shape.Combinations {
		name := recordName(item.Name, combination)

		if held, taken := claimed[name]; taken {
			faults.Fail(&NameCollisionError{
				Name:      name,
				Path:      held.path,
				Item:      held.item,
				Other:     book.Path,
				OtherItem: item.Name,
			})

			continue
		}

		derived := record{name: name, path: book.Path, item: item.Name}
		for _, alternative := range combination {
			derived.alternatives = append(derived.alternatives, reference{
				record: name,
				path:   pathTo(item, alternative),
			})
		}

		claimed[name] = derived
		s.records = append(s.records, derived)

		if several {
			s.renamed = append(s.renamed, name)
		}

		if root == "" {
			root = name
		}
	}

	// A redefine inside a repeating group is one variant however many record
	// types the 01-level resolves to, so the reference is rooted at the first
	// of them; where a layout carries the others it carries a form of its own
	// for each, which is a thing the adopter is writing anyway.
	if root != "" {
		for _, alternation := range shape.Alternations {
			if !alternation.InTable {
				continue
			}

			arms := make([]string, 0, len(alternation.Alternatives))
			for _, alternative := range alternation.Alternatives {
				arms = append(arms, alternative.Name)
			}

			s.variants = append(s.variants, variant{
				item: reference{record: root, path: pathTo(item, alternation.Item)},
				arms: arms,
			})
		}
	}

	if several {
		s.notes = append(s.notes, fmt.Sprintf(
			"%s: %s carries %d REDEFINES outside a repeating group, which resolve to %d record types",
			book.Path, item.Name, redefines(shape), len(shape.Combinations),
		))
	}
}

// recordName is the symbol one combination is called, per docs/cli/SPEC.md, "How
// a combination's record name is chosen".
//
// One record type over an 01-level is the 01-level's own name. More than one is
// the 01-level's name followed by the name of each chosen alternative, in the
// order the redefines appear in the copybook, joined by a single hyphen. The
// names are long and nobody would choose them, which is affordable because a
// record name is the layout's own identifier and nothing outside the layout ever
// sees it as an identity — it has to be unique, deterministic, and traceable
// back to the bytes it selects, and numbering is what fails at the third.
func recordName(item string, combination []*copybook.Field) string {
	parts := make([]string, 0, len(combination)+1)
	parts = append(parts, item)

	for _, alternative := range combination {
		parts = append(parts, alternative.Name)
	}

	return strings.Join(parts, "-")
}

// redefines is how many REDEFINES clauses outside a repeating group the record
// carries.
//
// A run described three ways carries two of them, which is what the copybook
// writes and what the reader counts when they go and look.
func redefines(shape resolve.Shape) int {
	count := 0

	for _, alternation := range shape.Alternations {
		if !alternation.InTable {
			count += len(alternation.Alternatives) - 1
		}
	}

	return count
}

// pathTo is the item reference path from the 01-level down to field: one name
// per level, outermost first, with the 01-level's own name not repeated.
func pathTo(root, field *copybook.Field) []string {
	var path []string

	for at := field; at != nil && at != root; at = at.Parent {
		path = append(path, at.Name)
	}

	slices.Reverse(path)

	return path
}

// unreachable is the first item in shape that a layout could not write a
// reference to, or nil where every one of them can be named.
//
// An alternative or a variant with no data-name — one written FILLER, or one
// whose name was simply omitted — has no spelling in an item reference, and
// neither has an unnamed group between it and the 01-level. Finding it here is
// what keeps a reference cpybkc wrote from being reported against the adopter
// later.
func unreachable(root *copybook.Field, shape resolve.Shape) *copybook.Field {
	for _, alternation := range shape.Alternations {
		targets := alternation.Alternatives
		if alternation.InTable {
			targets = []*copybook.Field{alternation.Item}
		}

		for _, target := range targets {
			for at := target; at != nil && at != root; at = at.Parent {
				if at.Name == "" {
					return at
				}
			}
		}
	}

	return nil
}
