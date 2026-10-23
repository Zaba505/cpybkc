// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package definitions

import "github.com/Zaba505/cpybkc/internal/layout"

// Entry is one `(entry <level> <name> <clause> …)`: one data-description entry
// of a copybook, transcribed.
type Entry struct {
	// Pos is the entry form.
	Pos layout.Pos

	// Level is the level number.
	Level Level

	// Name is the entry's name: `FILLER`, or the data-name the copybook wrote.
	// Which of the two FILLER is at a given level is the converter's, by
	// docs/layout/SPEC.md's "What is written follows from what cobol-go reads".
	Name DataName

	// Clauses are the entry's clauses, in the order they were written.
	Clauses []Clause
}

// Level is an entry's level number, as written.
type Level struct {
	Pos   layout.Pos
	Value int64
}

// DataName is a COBOL word: a data-name, a condition-name, an index-name, or a
// word in a VALUE or THROUGH operand, figurative constants included.
type DataName struct {
	// Pos is the symbol, or the `(word …)` form when it was written as one.
	Pos layout.Pos

	// Name is the word, in the case the body wrote it.
	Name string

	// Word reports that it was written `(word "<spelling>")`, which the reader
	// admits only where the spelling would not read as a symbol.
	Word bool
}

// Text is a COBOL token held as text: a literal, a PICTURE character-string or
// an OCCURS bound, delimiters included.
type Text struct {
	Pos   layout.Pos
	Value string
}

// Keyword is an upper-case symbol from a closed set `cobol-go` judges — a
// usage, a sign position, a synchronization direction, a key order — or
// SEPARATE on a sign.
type Keyword struct {
	Pos   layout.Pos
	Value string
}

// Operand is what a `value`, a `renames` or either end of a `through` holds:
// [Text], a [DataName], or, where a range is admitted, a [Through].
type Operand interface {
	// Position is where the operand was written.
	Position() layout.Pos

	operand()
}

// Through is `(through <from> <to>)`, a range of values or the range a RENAMES
// names. Which operands it may hold where it stands is `cobol-go`'s to judge.
type Through struct {
	Pos      layout.Pos
	From, To Operand
}

// Position implements [Operand].
func (t Text) Position() layout.Pos { return t.Pos }

// Position implements [Operand].
func (d DataName) Position() layout.Pos { return d.Pos }

// Position implements [Operand].
func (t Through) Position() layout.Pos { return t.Pos }

func (Text) operand()     {}
func (DataName) operand() {}
func (Through) operand()  {}

// Clause is one clause of an entry: one of the twelve forms
// docs/layout/SPEC.md's "An entry, and one form per clause" gives, one per
// clause kind of `cobol-go`'s data-description grammar.
//
// The set is closed by the unexported method, so a type switch over the twelve
// is exhaustive, which the converter's has to be.
type Clause interface {
	// Position is the clause form.
	Position() layout.Pos

	clause()
}

// Redefines is `(redefines <data-name>)`.
type Redefines struct {
	Pos  layout.Pos
	Name DataName
}

// Picture is `(pic "<picture>")`.
type Picture struct {
	Pos     layout.Pos
	Picture Text
}

// Usage is `(usage <usage>)`.
type Usage struct {
	Pos   layout.Pos
	Usage Keyword
}

// Value is `(value <operand> …)`.
type Value struct {
	Pos      layout.Pos
	Operands []Operand
}

// Occurs is `(occurs "<count>" (to …) (depending-on …) (key …) … (indexed-by …))`.
//
// The phrases are held as `cobol-go` holds them. When `to` is present the count
// is the minimum and `to` the maximum, as in COBOL.
type Occurs struct {
	Pos         layout.Pos
	Count       Text
	To          *Text
	DependingOn *DataName
	Keys        []Key
	IndexedBy   *DataName
}

// Key is one `(key <order> <data-name>)` phrase of an [Occurs].
type Key struct {
	Pos   layout.Pos
	Order Keyword
	Name  DataName
}

// Sign is `(sign <position>)` or `(sign <position> SEPARATE)`.
type Sign struct {
	Pos layout.Pos

	// Edge is the sign position, LEADING or TRAILING as `cobol-go` judges. It
	// is not called Position because every clause answers [Clause.Position]
	// with where it was written.
	Edge Keyword

	// Separate is SEPARATE, or nil where the body wrote none.
	Separate *Keyword
}

// Synchronized is `(synchronized)` or `(synchronized <direction>)`.
type Synchronized struct {
	Pos layout.Pos

	// Direction is the direction, or nil where the body wrote none.
	Direction *Keyword
}

// Justified is `(justified)`.
type Justified struct{ Pos layout.Pos }

// BlankWhenZero is `(blank-when-zero)`.
type BlankWhenZero struct{ Pos layout.Pos }

// Global is `(global)`.
type Global struct{ Pos layout.Pos }

// External is `(external)`.
type External struct{ Pos layout.Pos }

// Renames is `(renames <data-name>)` or `(renames (through <from> <to>))`.
type Renames struct {
	Pos layout.Pos

	// Renamed is a [DataName] or a [Through].
	Renamed Operand
}

// Position implements [Clause].
func (c Redefines) Position() layout.Pos { return c.Pos }

// Position implements [Clause].
func (c Picture) Position() layout.Pos { return c.Pos }

// Position implements [Clause].
func (c Usage) Position() layout.Pos { return c.Pos }

// Position implements [Clause].
func (c Value) Position() layout.Pos { return c.Pos }

// Position implements [Clause].
func (c Occurs) Position() layout.Pos { return c.Pos }

// Position implements [Clause].
func (c Sign) Position() layout.Pos { return c.Pos }

// Position implements [Clause].
func (c Synchronized) Position() layout.Pos { return c.Pos }

// Position implements [Clause].
func (c Justified) Position() layout.Pos { return c.Pos }

// Position implements [Clause].
func (c BlankWhenZero) Position() layout.Pos { return c.Pos }

// Position implements [Clause].
func (c Global) Position() layout.Pos { return c.Pos }

// Position implements [Clause].
func (c External) Position() layout.Pos { return c.Pos }

// Position implements [Clause].
func (c Renames) Position() layout.Pos { return c.Pos }

func (Redefines) clause()     {}
func (Picture) clause()       {}
func (Usage) clause()         {}
func (Value) clause()         {}
func (Occurs) clause()        {}
func (Sign) clause()          {}
func (Synchronized) clause()  {}
func (Justified) clause()     {}
func (BlankWhenZero) clause() {}
func (Global) clause()        {}
func (External) clause()      {}
func (Renames) clause()       {}
