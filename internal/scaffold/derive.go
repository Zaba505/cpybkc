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

	"github.com/Zaba505/cpybkc/internal/definitions"
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
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

// String renders the reference as the layout format spells it: the record name,
// which is the layout's own symbol, and then each COBOL name on the path as a
// data-name — bare where it reads as a symbol, and `(word "<spelling>")` where
// it would not (docs/layout/SPEC.md, "The leaves: a level, a data-name and
// literal text").
func (r reference) String() string {
	names := make([]string, 0, len(r.path)+1)
	names = append(names, r.record)

	for _, name := range r.path {
		names = append(names, definitions.Spell(name))
	}

	return "(item " + strings.Join(names, " ") + ")"
}

// Notes are the `note:` diagnostics the run owes: one per 01-level that resolved
// to more than one record type, and one per REDEFINES whose resolution the
// `OCCURS DEPENDING ON` reading decides.
//
// They are here rather than written from here because docs/cli/SPEC.md puts
// every line cpybkc says about a run on standard error and this package holds
// neither stream. The first kind names the copybook, the 01-level, how many
// REDEFINES outside a repeating group it carries and how many record types they
// produce: it is the size of the work in front of the reader, which is a thing
// they have to act on rather than discover in the file. The second names the
// copybook, the 01-level, the redefined item and the group it sits in, and says
// which of the two readings the record forms below it were written under — it is
// the one part of a scaffold that is not a function of the copybooks alone
// (#373).
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

// reading is which reading of `OCCURS DEPENDING ON` the derivation is read
// under, and there is nothing for it to be but unstated.
//
// A scaffold is what an adopter writes a layout *from*, and the reading is one
// of the questions it raises rather than one of the answers it carries: which
// compiler wrote the file is not in any copybook, and `init` is handed copybooks
// and nothing else. So there is no value to pass here, and passing one would be
// this package choosing on the adopter's behalf the one thing the scaffold's own
// `copybook-reading` comment says it cannot.
//
// [github.com/Zaba505/cpybkc/internal/resolve.Describe] is explicit about what
// an unstated reading answers — a fixed table, which is
// [github.com/Zaba505/cpybkc/internal/layoutmodel.Reading.Slides]'s safe answer
// — and about the one alternation that answer decides differently, which reaches
// here as [github.com/Zaba505/cpybkc/internal/resolve.Alternation.ReadingDecides]
// and is raised as a note (#373).
func reading() layoutmodel.Reading { return layoutmodel.ReadingUnstated }

// Derive reads every copybook and returns the scaffold they decide.
//
// The copybooks are taken in the order they were given, and their 01-levels in
// declaration order, because that is the order the `record` forms are written in
// and a scaffold has to be byte-identical between two runs over one set of
// copybooks.
//
// What it cannot write a record for, it notes, and the derivation goes on
// (docs/cli/SPEC.md, "How a combination's record name is chosen"): an unnamed
// REDEFINES alternative, a derived record name two record types share, an
// 01-level [resolve.Describe] cannot describe, and a derived record name the
// S-expression grammar would not read back as itself. Every one of those is a
// statement about the layout and never about the copybook, so it withholds the
// record it is about and nothing else. A copybook every 01-level of which was
// noted contributes no record form, and a note says so.
//
// What fails the run is a copybook that cannot be read at all — one that does
// not parse or build, or that declares no named 01-level — and every such fault
// is reported rather than the first: a set of copybooks goes wrong in the same
// way in more than one file at once, and a reader that stopped would be run once
// per copybook. Nothing is returned where anything failed — a scaffold missing
// the records of the copybook that could not be read is a file whose `sequence`
// is short and whose incompleteness reads exactly like the incompleteness that
// is deliberate. Until the definitions files land (#408) these stay refusals, as
// docs/cli/SPEC.md, "`init`'s streams and exit codes", stages them.
func Derive(books []Copybook) (*Scaffold, error) {
	// A scaffold over no copybooks is not an empty scaffold: there is no
	// copybook to have noted anything about, and so nothing to say why it holds
	// no record. The command line already refuses this as a usage error —
	// `--copybook` is required and appears at least once — and the check is
	// here as well because this function is reachable without one.
	if len(books) == 0 {
		return nil, ErrNoCopybooks
	}

	var faults diag.List

	// Every 01-level is read before any record is written, because whether a
	// record name collides is a property of the whole set of copybooks: the
	// first record type to derive a name cannot be written until it is known
	// that no later one derives it too.
	var levels []level

	read := make([]Copybook, 0, len(books))

	for _, book := range books {
		items, err := readRecords(book)
		if err != nil {
			faults.Fail(err)

			continue
		}

		for _, item := range items {
			levels = append(levels, describeLevel(len(read), book, item))
		}

		read = append(read, book)
	}

	if faults.Failed() {
		return nil, faults.Err()
	}

	s := new(Scaffold)
	s.take(read, levels)

	return s, nil
}

// readRecords parses one copybook and returns the 01-levels a `record` form can
// be written over.
//
// Level-77 items and an 01-level with no data-name are not among them. A
// level-77 is a standalone work item rather than a record description, and an
// unnamed 01-level is one no `record` form could name; a copybook holding
// nothing else declares no record, which is reported rather than passed over.
func readRecords(book Copybook) ([]*copybook.Field, error) {
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

// level is one named 01-level, read: what it would derive, and what the run
// already owes a note about before any name has been compared with another.
type level struct {
	// at is which of the copybooks read the 01-level is in. It is an index
	// rather than the path because two copybooks may be named by one path when
	// the caller is not the command line, which refuses that as a duplicate.
	at   int
	book Copybook
	item *copybook.Field

	// shape is what [resolve.Describe] answered. It is the zero value where
	// Describe failed, which leaves nothing to derive.
	shape resolve.Shape

	// notes are the lines owed about this 01-level that no other 01-level can
	// change: the combination count, the reading that decides a record type, an
	// 01-level that could not be described, and an unnamed alternative.
	notes []string

	// candidates are the record types the 01-level resolves to that a layout
	// could name, in the order [resolve.Describe] gave the combinations.
	candidates []record

	// unspellable is set where every record type of the 01-level carries an
	// item no layout could name, so none of them is a candidate.
	unspellable bool
}

// describeLevel reads one 01-level through [resolve.Describe] and decides
// everything about it that does not depend on another 01-level.
func describeLevel(at int, book Copybook, item *copybook.Field) level {
	read := level{at: at, book: book, item: item}

	shape, err := resolve.Describe(item, dialect(), reading())
	if err != nil {
		// A note rather than a fault. The copybook parsed and built, so it is
		// transcribable; it is cpybkc's own resolution that cannot lay this
		// 01-level out, which is a statement about the record a layout would
		// carry and not about the copybook.
		read.notes = append(read.notes, fmt.Sprintf(
			"%s: %s cannot be described as a record (%v), so no record is written over it",
			book.Path, item.Name, err,
		))

		return read
	}

	read.shape = shape

	// More than one record type over one 01-level is what raises a rename and
	// what the run owes a note about. The note is written before the records
	// it is about rather than after them: both counts are known as soon as the
	// shape is, and a copybook whose combinations are numerous enough to be
	// worth warning about is the one where the work that follows this line is
	// the expensive part.
	if len(shape.Combinations) > 1 {
		read.notes = append(read.notes, fmt.Sprintf(
			"%s: %s carries %d REDEFINES outside a repeating group, which resolve to %d record types",
			book.Path, item.Name, redefines(shape), len(shape.Combinations),
		))
	}

	// The one part of a shape no copybook decides, raised where it stands
	// rather than folded into the count above. A REDEFINES inside a group
	// declared `OCCURS 0 TO 1 TIMES DEPENDING ON` is a variant under
	// `odoslide` and a record type per alternative under `noodoslide`, and
	// `init` holds no reading to settle it with — so the record forms below are
	// one of the two answers and the adopter has to be told which, beside the
	// `copybook-reading` question the scaffold already raises.
	//
	// It is a note rather than a fault for that form's reason: a copybook a
	// layout has not been written for yet is not a copybook with a fault in it,
	// and refusing here would make `init` unusable on exactly the copybooks it
	// is most wanted for.
	for _, alternation := range shape.Alternations {
		if !alternation.ReadingDecides {
			continue
		}

		read.notes = append(read.notes, fmt.Sprintf(
			"%s: %s redefines %s inside %s, a group that repeats under odoslide and does not under noodoslide; "+
				"the record forms below are the noodoslide answer, and under odoslide this is one variant instead",
			book.Path, item.Name, alternation.Item.Name, alternation.Table.Name,
		))
	}

	read.unspellable = read.unnamedVariant()
	if read.unspellable {
		return read
	}

	// The combinations choosing an item no layout could name, grouped by that
	// item so that one unnamed alternative is one note however many record
	// types choose it.
	var (
		holes    []*copybook.Field
		choosing = make(map[*copybook.Field]int)
	)

	for _, combination := range shape.Combinations {
		if hole := unreachableIn(item, combination); hole != nil {
			if choosing[hole] == 0 {
				holes = append(holes, hole)
			}

			choosing[hole]++

			continue
		}

		name := recordName(item.Name, combination)

		derived := record{name: name, path: book.Path, item: item.Name}
		for _, alternative := range combination {
			derived.alternatives = append(derived.alternatives, reference{
				record: name,
				path:   pathTo(item, alternative),
			})
		}

		read.candidates = append(read.candidates, derived)
	}

	for _, hole := range holes {
		read.notes = append(read.notes, fmt.Sprintf(
			"%s: %s describes a run of bytes with an item that has no data-name, at line %d, column %d, "+
				"and a layout names an alternative with an item reference, so %s",
			book.Path, item.Name, hole.Pos.Line, hole.Pos.Column, unwrittenCount(choosing[hole]),
		))
	}

	return read
}

// unnamedVariant reports a REDEFINES inside a repeating group that a layout
// could not spell, and notes every record type of the 01-level as unwritten.
//
// A redefine inside a repeating group is one variant in every record type over
// the 01-level rather than a choice any one of them makes, so every one of them
// would carry an `arm`, or a variant reference, with a hole in it. That is every
// record selecting it, in docs/cli/SPEC.md's words, which here is all of them.
func (l *level) unnamedVariant() bool {
	for _, alternation := range l.shape.Alternations {
		if !alternation.InTable {
			continue
		}

		hole := unreachableIn(l.item, alternation.Alternatives)
		if hole == nil {
			continue
		}

		names := make([]string, 0, len(l.shape.Combinations))
		for _, combination := range l.shape.Combinations {
			names = append(names, recordName(l.item.Name, combination))
		}

		l.notes = append(l.notes, fmt.Sprintf(
			"%s: %s describes a run of bytes inside a repeating group with an item that has no data-name, "+
				"at line %d, column %d, and every record type over %s carries that variant, "+
				"so none of them is written: %s",
			l.book.Path, l.item.Name, hole.Pos.Line, hole.Pos.Column, l.item.Name, strings.Join(names, ", "),
		))

		return true
	}

	return false
}

// unwrittenCount says how many record types an unnamed alternative withheld.
//
// They are counted rather than named because there is no name to give them: the
// name of a combination is built out of the names of what it chose, and one of
// those is missing.
func unwrittenCount(n int) string {
	if n == 1 {
		return "the record type choosing it is not written"
	}

	return fmt.Sprintf("the %d record types choosing it are not written", n)
}

// take writes every candidate no other 01-level contends for, and notes the
// rest.
//
// A name is written only where exactly one record type derived it. Writing the
// first and noting the others would be cpybkc choosing which of them the
// adopter meant, and disambiguating would be inventing a name whose only
// property is that it differs from another invented one, so every record type
// deriving a shared name is noted and none is written.
func (s *Scaffold) take(books []Copybook, levels []level) {
	// Who derived each name, in the order they were met. Read and never ranged
	// over, which is what keeps the derivation a function of its inputs. A
	// name the grammar would not read back is not entered: it is noted for
	// that reason and not written either way.
	derivers := make(map[string][]string)

	for _, l := range levels {
		for _, candidate := range l.candidates {
			if !definitions.ReadsAsSymbol(candidate.name) {
				continue
			}

			derivers[candidate.name] = append(derivers[candidate.name], l.item.Name+" in "+l.book.Path)
		}
	}

	written := make([]bool, len(books))

	for _, l := range levels {
		if len(l.shape.Tables) > 0 {
			s.tables = true
		}

		s.notes = append(s.notes, l.notes...)

		several := len(l.shape.Combinations) > 1
		root := ""

		for _, candidate := range l.candidates {
			if !definitions.ReadsAsSymbol(candidate.name) {
				s.notes = append(s.notes, fmt.Sprintf(
					"%s: %s derives the record name %q, which the S-expression grammar does not read back as "+
						"that symbol, so the record is not written",
					l.book.Path, l.item.Name, candidate.name,
				))

				continue
			}

			if all := derivers[candidate.name]; len(all) > 1 {
				s.notes = append(s.notes, fmt.Sprintf(
					"%s: %s derives the record name %s, which %d record types derive (%s), "+
						"so none of them is written",
					l.book.Path, l.item.Name, candidate.name, len(all), strings.Join(all, ", "),
				))

				continue
			}

			s.records = append(s.records, candidate)
			written[l.at] = true

			if several {
				s.renamed = append(s.renamed, candidate.name)
			}

			if root == "" {
				root = candidate.name
			}
		}

		// A redefine inside a repeating group is one variant however many
		// record types the 01-level resolves to, so the reference is rooted at
		// the first of them written; where a layout carries the others it
		// carries a form of its own for each, which is a thing the adopter is
		// writing anyway. Where none was written there is no record for the
		// reference to be rooted at, and no variant to raise.
		if root == "" {
			continue
		}

		for _, alternation := range l.shape.Alternations {
			if !alternation.InTable {
				continue
			}

			arms := make([]string, 0, len(alternation.Alternatives))
			for _, alternative := range alternation.Alternatives {
				arms = append(arms, alternative.Name)
			}

			s.variants = append(s.variants, variant{
				item: reference{record: root, path: pathTo(l.item, alternation.Item)},
				arms: arms,
			})
		}
	}

	// A copybook no record was derived from still says so. Its notes above
	// name each 01-level and why, and this is the line that tells the reader
	// the copybook contributes nothing to the file rather than leaving them to
	// count.
	for at, book := range books {
		if !written[at] {
			s.notes = append(s.notes, fmt.Sprintf(
				"%s: no record is derived from this copybook, because every 01-level it declares is noted",
				book.Path,
			))
		}
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

// unreachableIn is the first item in chosen that a layout could not name, or
// nil where every one of them can be named.
//
// An item with no data-name — one written FILLER, or one whose name was simply
// omitted — has no spelling in an item reference or in an `arm`, and neither has
// an unnamed group between it and the 01-level. Finding it here is what keeps a
// reference cpybkc wrote from being reported against the adopter later.
//
// chosen is a combination outside a repeating group, each of whose alternatives
// a record names with an `alternative` child, or the alternatives of one
// redefine inside a repeating group, which a variant names with an `arm` each.
func unreachableIn(root *copybook.Field, chosen []*copybook.Field) *copybook.Field {
	for _, target := range chosen {
		for at := target; at != nil && at != root; at = at.Parent {
			if unnamed(at) {
				return at
			}
		}
	}

	return nil
}

// unnamed reports an item nothing in a layout can refer to.
//
// Both halves are tested rather than only the empty name. `cobol-go` documents
// Name as empty for a FILLER item, so today the second is implied by the first —
// but Filler is the field that *means* "this item has no data-name", and
// [readRecords] already tests the pair when it decides which top-level items are
// records. A guard whose whole job is to catch an item no reference can name is
// the wrong place to depend on two spellings of that staying in agreement.
func unnamed(field *copybook.Field) bool { return field.Name == "" || field.Filler }
