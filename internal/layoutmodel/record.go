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

// The tags the binding half of the record definitions layer reads. `record` is
// the form; its source is `copybook` or `definitions`, and `alternative` is the
// repeatable child that says which alternative of a record-level `REDEFINES`
// the form means. `definitions-block` is the top-level form a block source
// names.
const (
	childCopybook       = "copybook"
	childDefinitions    = "definitions"
	childAlternative    = "alternative"
	tagDefinitionsBlock = "definitions-block"
)

// sourceTags are the two source forms, in the order a message names them.
var sourceTags = []string{childCopybook, childDefinitions}

// SourceKind is where a record's description is held: docs/layout/SPEC.md's "A
// record's source: a copybook, a definitions file or a block".
type SourceKind int

const (
	// CopybookSource is `(copybook "<path>" <top-level-name>)`: a copybook,
	// by its path.
	CopybookSource SourceKind = iota + 1

	// DefinitionsFileSource is `(definitions "<path>" <top-level-name>)`: a
	// definitions file, by its path.
	DefinitionsFileSource

	// DefinitionsBlockSource is `(definitions <block-name> <top-level-name>)`:
	// a `definitions-block` in the same layout, by its name.
	DefinitionsBlockSource
)

// String names the kind the way a message does.
func (k SourceKind) String() string {
	switch k {
	case CopybookSource:
		return "a copybook"
	case DefinitionsFileSource:
		return "a definitions file"
	case DefinitionsBlockSource:
		return "a definitions block"
	default:
		return "no source"
	}
}

// Record is one `record` form: the name it defines, and the item of its
// description it binds that name to.
//
// docs/layout/SPEC.md's "Record definitions" is the whole of it. A record is a
// name the rest of the layout refers to — a sequencing expression writes it, a
// discriminator names it, an item reference is rooted at it — and a source
// saying where its description is held, with the top-level item inside it that
// the name stands for. The source is one of three kinds: a copybook by its
// path, a definitions file by its path, or a `definitions-block` of this layout
// by its name.
//
// A path is carried exactly as the layout spells it and nothing has been
// resolved against anything. Where a file is looked for is the CLI's
// (docs/cli/SPEC.md, "Finding the inputs"), and a diagnostic naming a path the
// adopter never wrote sends them looking for a file they have not got — which
// is why every position here is kept beside the string, so that whoever opens
// the file can point back at the line that named it.
type Record struct {
	// Pos is the `record` form.
	Pos layout.Pos

	// Name is the name the form defines.
	Name string

	// NamePos is the name itself, which is what a diagnostic about the name
	// rather than about the binding points at.
	NamePos layout.Pos

	// Kind is which of the three sources the record names.
	Kind SourceKind

	// Source is the source form — `copybook` or `definitions` — which is the
	// span every diagnostic about the file, the block or the item it names
	// carries ([github.com/Zaba505/cpybkc/internal/diag.MissingCopybookError],
	// [github.com/Zaba505/cpybkc/internal/diag.UndeclaredItemError]).
	Source layout.Pos

	// Path is the copybook's or the definitions file's path as the layout
	// spells it, and empty for a block.
	Path string

	// PathPos is the path itself.
	PathPos layout.Pos

	// Block is the name of the `definitions-block` the record names, and
	// empty for a copybook or a definitions file.
	Block string

	// BlockPos is the block name itself.
	BlockPos layout.Pos

	// Item is the top-level item of the description this record is bound to,
	// as the layout spells it.
	Item string

	// ItemPos is the item name itself.
	ItemPos layout.Pos

	// Alternatives are the `alternative` children, in the order the form
	// writes them: one item reference per `REDEFINES` the bound `01`-level
	// carries outside a repeating group, naming the alternative this record
	// type is (docs/layout/SPEC.md, "Which alternative a record is").
	//
	// Nothing at this layer knows how many there should be, or whether a
	// reference names an alternative at all: both are facts about the
	// description and are `resolve`'s. What is checked here is what the layout
	// decides on its own — that each reference is rooted at the record carrying
	// it, and that no two of them name one item.
	//
	// The order is the layout's and carries no meaning. Two redefines name two
	// distinct runs of bytes, so which is written first decides nothing, and a
	// consumer pairing them by position would be inventing the rule this child
	// exists to state.
	Alternatives []ItemRef
}

// ReadRecords reads the record definitions out of a parsed layout, in the order
// the layout writes them.
//
// It reports every fault it finds, joined, and returns nothing when it found
// one, for [ReadProfile]'s reason: a binding an adopter cannot act on is worse
// than none.
//
// What it enforces is what a declaration cannot state and a description is not
// needed for: that a layout defines at least one record, that each `record` is
// written as a name over one source and any number of `alternative` children,
// that the path or the block name and the item are written at all, that every
// `alternative` names an item through the record carrying it and no two of them
// name one item, that no two forms define one name, that a block a record names
// is one the layout defines, and that every block the layout defines is named
// by some record. Everything else about a binding needs the description —
// whether the path names something that can be opened, whether what it holds
// declares the item, and how many alternatives its `01`-level has for the
// children to choose among — and is `resolve`'s or the CLI's, which is the one
// place that knows where a file is looked for.
//
// Two records naming one source and one item is not a fault and is the
// ordinary way a layout tells one `01`-level apart by where it sits
// (docs/layout/SPEC.md, "Many records may name one copybook, and two may name
// one item"). What is refused is the other direction: one name standing for two
// bindings, which would leave the order the forms are written in deciding which
// of them every reference meant.
//
// A block defined twice is [ReadBlocks]' to report, and not this reader's: the
// fault is about the blocks and not about any record.
//
// Top-level forms belonging to other layers are not read here and are not
// faults.
func ReadRecords(file *layout.File) ([]Record, error) {
	read := &recordReader{defined: make(map[string]layout.Pos), blocks: blockNames(file)}

	var records []Record

	for _, form := range file.Forms {
		if form.Tag != tagRecord {
			continue
		}

		record, ok := read.record(form)
		if !ok {
			continue
		}

		records = append(records, record)
	}

	read.unnamedBlocks(file)

	// The count is a fact about the layout rather than about any one form, so
	// it is reported after everything the forms themselves were wrong about. A
	// layout that defines no record describes no file: there is nothing for a
	// sequencing expression to name and nothing for a descriptor to carry.
	if len(records) == 0 && !read.Failed() {
		read.Fail(&NoRecordsError{File: file.Name})
	}

	if read.Failed() {
		return nil, read.Err()
	}

	return records, nil
}

// recordReader holds the state one [ReadRecords] accumulates.
type recordReader struct {
	diag.List

	// defined is where each name was first defined, which makes a second form
	// defining it reportable against the first.
	defined map[string]layout.Pos

	// blocks are the names the layout's `definitions-block` forms define, in
	// the order it defines them, which is what a block source is held to.
	blocks []string
}

// record reads one `record` form.
func (r *recordReader) record(form layout.Form) (Record, bool) {
	if len(form.Elements) < 2 {
		r.Fail(&RecordFormError{Pos: form.Pos, Found: shortfall(form.Elements)})

		return Record{}, false
	}

	name, ok := form.Elements[0].(layout.Symbol)
	if !ok {
		r.Fail(&RecordFormError{Pos: form.Elements[0].Position(), Found: describe(form.Elements[0])})

		return Record{}, false
	}

	child, ok := form.Elements[1].(layout.Form)
	if !ok || !slices.Contains(sourceTags, child.Tag) {
		r.Fail(&ChildError{
			Pos:    form.Elements[1].Position(),
			Form:   form.Tag,
			Found:  describe(form.Elements[1]),
			Admits: sourceTags,
		})

		return Record{}, false
	}

	record := Record{Pos: form.Pos, Name: name.Value, NamePos: name.Pos, Source: child.Pos}

	// Every half is read whatever the ones before it said, for the reason a
	// rename is held to every rule past the first it breaks: a record whose
	// name is already taken and whose copybook has no path is two things to fix
	// rather than one to discover on the next run.
	var sound bool

	if child.Tag == childCopybook {
		sound = r.binding(&record, child)
	} else {
		sound = r.definitions(&record, child)
	}

	sound = r.alternatives(&record, form) && sound

	return record, r.name(form, name) && sound
}

// alternatives reads the `alternative` children of a `record` form.
//
// They follow the source, and every element past it is one: the form
// admits no other child, so a tag that is not `alternative` there is answered by
// the same message a wrong first child is.
//
// Two things are checked, and they are the two the layout decides on its own. A
// reference is rooted at a record, and an `alternative` naming an alternative of
// some *other* record's copybook is a statement about a copybook this record is
// not bound to — it would be read against the wrong item tree, and no diagnostic
// afterwards could say so. And two children naming one item choose one
// alternative twice, which leaves the record's other redefine unchosen while
// looking like it was answered.
func (r *recordReader) alternatives(record *Record, form layout.Form) bool {
	sound := true

	named := make(map[string]layout.Pos, len(form.Elements))

	for _, element := range form.Elements[2:] {
		child, ok := element.(layout.Form)
		if !ok || child.Tag != childAlternative {
			r.Fail(&ChildError{
				Pos:    element.Position(),
				Form:   form.Tag,
				Found:  describe(element),
				Admits: []string{childAlternative},
			})

			sound = false

			continue
		}

		if len(child.Elements) != 1 {
			r.Fail(&AlternativeFormError{Pos: child.Pos, Found: count(len(child.Elements))})

			sound = false

			continue
		}

		ref, err := readItemRef(child.Elements[0])
		if err != nil {
			r.Fail(err)

			sound = false

			continue
		}

		if ref.Record != record.Name {
			r.Fail(&AlternativeRootError{Pos: ref.Pos, Record: record.Name, Ref: ref})

			sound = false

			continue
		}

		if first, already := named[ref.identity()]; already {
			r.Fail(&DuplicateAlternativeError{Pos: child.Pos, First: first, Ref: ref})

			sound = false

			continue
		}

		named[ref.identity()] = child.Pos
		record.Alternatives = append(record.Alternatives, ref)
	}

	return sound
}

// binding reads the `copybook` child: the path, and the top-level item in it.
func (r *recordReader) binding(record *Record, child layout.Form) bool {
	if len(child.Elements) != 2 {
		r.Fail(&CopybookFormError{Pos: child.Pos, Found: shortfall(child.Elements)})

		return false
	}

	path, ok := child.Elements[0].(layout.Text)
	if !ok {
		r.Fail(&CopybookFormError{Pos: child.Elements[0].Position(), Found: describe(child.Elements[0])})

		return false
	}

	item, itemPos, ok := r.topLevelName(child)
	if !ok {
		return false
	}

	record.Kind = CopybookSource
	record.Path, record.PathPos = path.Value, path.Pos
	record.Item, record.ItemPos = item, itemPos

	// A path of no characters names no file, and the diagnostic the CLI would
	// otherwise raise about it would say that a copybook at "" could not be
	// opened — a message naming nothing, about a file nobody wrote.
	if path.Value == "" {
		r.Fail(&EmptyCopybookPathError{Pos: path.Pos, Record: record.Name})

		return false
	}

	return true
}

// topLevelName reads a source's second argument, the top-level item it binds
// the record to, which is a COBOL name and so a data-name whichever source it
// is: a symbol, or `(word "<spelling>")` where the spelling would not read as
// one (#404). Anything else is the source's own form error.
func (r *recordReader) topLevelName(child layout.Form) (string, layout.Pos, bool) {
	node := child.Elements[1]

	name, pos, ok, err := dataName(node)
	if err != nil {
		r.Fail(err)

		return "", layout.Pos{}, false
	}

	if !ok {
		found := describe(node)

		if child.Tag == childCopybook {
			r.Fail(&CopybookFormError{Pos: node.Position(), Found: found})
		} else {
			r.Fail(&DefinitionsFormError{Pos: node.Position(), Found: found})
		}

		return "", layout.Pos{}, false
	}

	return name, pos, true
}

// definitions reads a `definitions` source: a definitions file by its path, or
// a block by its name, and the top-level item in either.
//
// Which of the two it is is the first argument's kind and nothing else — text
// is a path, a symbol is a block name — so neither spelling has a second
// reading, and anything else in that position is a [DefinitionsFormError].
func (r *recordReader) definitions(record *Record, child layout.Form) bool {
	if len(child.Elements) != 2 {
		r.Fail(&DefinitionsFormError{Pos: child.Pos, Found: shortfall(child.Elements)})

		return false
	}

	item, itemPos, ok := r.topLevelName(child)
	if !ok {
		return false
	}

	record.Item, record.ItemPos = item, itemPos

	switch origin := child.Elements[0].(type) {
	case layout.Text:
		record.Kind = DefinitionsFileSource
		record.Path, record.PathPos = origin.Value, origin.Pos

		// [EmptyCopybookPathError]'s reason, for the other kind of file.
		if origin.Value == "" {
			r.Fail(&EmptyDefinitionsPathError{Pos: origin.Pos, Record: record.Name})

			return false
		}

		return true
	case layout.Symbol:
		record.Kind = DefinitionsBlockSource
		record.Block, record.BlockPos = origin.Value, origin.Pos

		// The schema's reference fault, held here too for a caller reading
		// without the schema: a block name the layout does not define binds
		// the record to nothing.
		if !slices.Contains(r.blocks, origin.Value) {
			r.Fail(&UndefinedBlockError{Pos: origin.Pos, Record: record.Name, Block: origin.Value, Defined: r.blocks})

			return false
		}

		return true
	default:
		r.Fail(&DefinitionsFormError{Pos: child.Elements[0].Position(), Found: describe(child.Elements[0])})

		return false
	}
}

// unnamedBlocks reports every `definitions-block` no record names, at its name.
//
// A record counts as naming a block whenever its source is written
// `(definitions <symbol> …)`, whatever else is wrong with the record: a
// record whose item is misspelled still names the block, and reporting the
// block as unnamed beside it would send the adopter to the wrong form.
func (r *recordReader) unnamedBlocks(file *layout.File) {
	named := make(map[string]bool)

	for _, form := range file.Forms {
		if form.Tag != tagRecord || len(form.Elements) < 2 {
			continue
		}

		source, ok := form.Elements[1].(layout.Form)
		if !ok || source.Tag != childDefinitions || len(source.Elements) == 0 {
			continue
		}

		if block, ok := source.Elements[0].(layout.Symbol); ok {
			named[block.Value] = true
		}
	}

	for _, form := range file.Forms {
		if form.Tag != tagDefinitionsBlock || len(form.Elements) == 0 {
			continue
		}

		name, ok := form.Elements[0].(layout.Symbol)
		if !ok || named[name.Value] {
			continue
		}

		r.Fail(&UnnamedBlockError{Pos: name.Pos, Block: name.Value})
	}
}

// blockNames is every name a `definitions-block` defines, in source order and
// once each, read leniently: a block written wrong is [ReadBlocks]' to report.
func blockNames(file *layout.File) []string {
	var names []string

	for _, form := range file.Forms {
		if form.Tag != tagDefinitionsBlock || len(form.Elements) == 0 {
			continue
		}

		if name, ok := form.Elements[0].(layout.Symbol); ok && !slices.Contains(names, name.Value) {
			names = append(names, name.Value)
		}
	}

	return names
}

// name holds a record's name to the names already defined.
func (r *recordReader) name(form layout.Form, name layout.Symbol) bool {
	if first, already := r.defined[name.Value]; already {
		r.Fail(&DuplicateRecordError{Pos: form.Pos, First: first, Record: name.Value})

		return false
	}

	r.defined[name.Value] = form.Pos

	return true
}

// shortfall names what stood where two things belong.
//
// It is this file's rather than [count]'s because [count] answers for a
// position taking exactly one thing, where "one" is the answer and never the
// fault; every form here takes two, so one of something is a shortfall rather
// than the shape.
func shortfall(elements []layout.Node) string {
	switch {
	case len(elements) == 0:
		return "no value"
	case len(elements) == 1:
		return "one"
	default:
		return "several"
	}
}
