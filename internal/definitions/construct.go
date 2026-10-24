// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package definitions

import (
	"strings"

	cobol "github.com/Zaba505/cobol-go"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
)

// Construct turns entries [Read] returned into the `cobol-go` entries they
// transcribe, which is what [Judge] holds to the validity rule and what
// `copybook.Build` folds exactly as it folds a copybook's.
//
// It is a function of its argument alone. Every [cobol.Pos] it sets — the
// entry's, its name's, each clause's, each word's and each literal's — is the
// line and column of the atom or form it came from, in whichever document the
// body was held in, so that a fault `copybook.Build` finds later can be pointed
// back at the text; `cobol.Pos` names no file, so which document that is stays
// the caller's to say, as it is for a copybook.
//
// What the notation leaves to `cobol-go` is built as written and left to the
// judge: a usage, a sign position or a direction it does not know, a literal
// where a data-name belongs. A literal text is classified the way `cobol-go`
// classifies the token — a [cobol.StringLiteral] when [cobol.Tokenize] reads it
// as a string, of the kind its prefix names, and a [cobol.NumericLiteral]
// otherwise; an `(all …)` is a [cobol.AllLiteral] around what it holds. `FILLER` is [cobol.Filler]
// at levels other than 66 and 88 and a name at those two, as docs/layout/SPEC.md
// states.
//
// The one fault it reports is a [KeyOrderError], since a key's order is the one
// closed-set value `cobol-go` holds as a boolean and so cannot be handed on.
func Construct(entries []Entry) ([]*cobol.DataDescriptionEntry, error) {
	var faults diag.List

	built := make([]*cobol.DataDescriptionEntry, 0, len(entries))

	for _, entry := range entries {
		built = append(built, construct(&faults, entry))
	}

	if faults.Failed() {
		return nil, faults.Err()
	}

	return built, nil
}

// construct builds one entry.
func construct(faults *diag.List, entry Entry) *cobol.DataDescriptionEntry {
	built := &cobol.DataDescriptionEntry{
		Pos:   pos(entry.Level.Pos),
		Level: int(entry.Level.Value),
	}

	if entry.Level.Value != 66 && entry.Level.Value != 88 && strings.EqualFold(entry.Name.Name, fillerName) {
		built.Filler = true
	} else {
		built.Name = toWord(entry.Name)
	}

	for _, clause := range entry.Clauses {
		built.Clauses = append(built.Clauses, constructClause(faults, clause))
	}

	return built
}

// constructClause builds one clause. The switch is exhaustive over the twelve
// clause forms [Clause] closes.
func constructClause(faults *diag.List, clause Clause) cobol.DataClause {
	switch c := clause.(type) {
	case Redefines:
		return &cobol.RedefinesClause{Pos: pos(c.Pos), Name: toWord(c.Name)}
	case Picture:
		return &cobol.PictureClause{Pos: pos(c.Pos), Picture: c.Picture.Value}
	case Usage:
		return &cobol.UsageClause{Pos: pos(c.Pos), Usage: c.Usage.Value}
	case Value:
		value := &cobol.ValueClause{Pos: pos(c.Pos)}

		for _, operand := range c.Operands {
			value.Values = append(value.Values, valueSpec(operand))
		}

		return value
	case Occurs:
		return constructOccurs(faults, c)
	case Sign:
		return &cobol.SignClause{Pos: pos(c.Pos), Position: c.Edge.Value, Separate: c.Separate != nil}
	case Synchronized:
		sync := &cobol.SynchronizedClause{Pos: pos(c.Pos)}
		if c.Direction != nil {
			sync.Direction = c.Direction.Value
		}

		return sync
	case Justified:
		return &cobol.JustifiedClause{Pos: pos(c.Pos)}
	case BlankWhenZero:
		return &cobol.BlankWhenZeroClause{Pos: pos(c.Pos)}
	case Global:
		return &cobol.GlobalClause{Pos: pos(c.Pos)}
	case External:
		return &cobol.ExternalClause{Pos: pos(c.Pos)}
	case Renames:
		renames := &cobol.RenamesClause{Pos: pos(c.Pos)}

		if through, ok := c.Renamed.(Through); ok {
			renames.From = operandWord(through.From)
			renames.Through = operandWord(through.To)
		} else {
			renames.From = operandWord(c.Renamed)
		}

		return renames
	default:
		// Unreachable: the set is closed to this package and every member is
		// above. A nil clause is the only other value, and Read builds none.
		return nil
	}
}

// constructOccurs builds an OCCURS clause.
func constructOccurs(faults *diag.List, c Occurs) *cobol.OccursClause {
	occurs := &cobol.OccursClause{Pos: pos(c.Pos), Min: numeric(c.Count)}

	if c.To != nil {
		occurs.Max = numeric(*c.To)
	}

	if c.DependingOn != nil {
		occurs.DependingOn = toWord(*c.DependingOn)
	}

	for _, key := range c.Keys {
		switch key.Order.Value {
		case "ASCENDING", "DESCENDING":
		default:
			faults.Fail(&KeyOrderError{Pos: key.Order.Pos, Order: key.Order.Value})
		}

		occurs.Keys = append(occurs.Keys, cobol.OccursKey{
			Pos:       pos(key.Pos),
			Ascending: key.Order.Value == "ASCENDING",
			Name:      toWord(key.Name),
		})
	}

	if c.IndexedBy != nil {
		occurs.IndexedBy = toWord(*c.IndexedBy)
	}

	return occurs
}

// valueSpec builds one value, or one range of them.
func valueSpec(operand Operand) cobol.ValueSpec {
	if through, ok := operand.(Through); ok {
		return cobol.ValueSpec{From: valueOperand(through.From), Through: valueOperand(through.To)}
	}

	return cobol.ValueSpec{From: valueOperand(operand)}
}

// valueOperand builds a literal, a word or an ALL literal.
func valueOperand(operand Operand) cobol.Type {
	switch o := operand.(type) {
	case Text:
		return literal(o)
	case DataName:
		return toWord(o)
	case All:
		return &cobol.AllLiteral{Pos: pos(o.Pos), Literal: valueOperand(o.Repeated)}
	default:
		// A range inside a range, which Read does not build. It is built as the
		// word its position names so the judge, rather than a panic, refuses it.
		return &cobol.Word{Pos: pos(operand.Position())}
	}
}

// operandWord builds a RENAMES operand, which `cobol-go` holds as a word. A
// text there is built as a word holding the text, so that the judge refuses it
// in `cobol-go`'s words rather than this package refusing it in its own.
func operandWord(operand Operand) *cobol.Word {
	switch o := operand.(type) {
	case DataName:
		return toWord(o)
	case Text:
		return &cobol.Word{Pos: pos(o.Pos), Value: o.Value}
	default:
		return &cobol.Word{Pos: pos(operand.Position())}
	}
}

// literal classifies a literal text by the first token [cobol.Tokenize] reads
// in it. A string token is a [cobol.StringLiteral] of the kind its prefix names
// — none, for an alphanumeric literal — holding its delimited body, which is
// how `cobol-go` holds the literal it parses out of the same text.
func literal(text Text) cobol.Type {
	for token, err := range cobol.Tokenize(strings.NewReader(text.Value)) {
		if err == nil && token.Type == cobol.TokenString {
			kind, body := prefixed(text.Value)

			return &cobol.StringLiteral{Pos: pos(text.Pos), Kind: kind, Value: body}
		}

		break
	}

	return numeric(text)
}

// literalKinds are the kinds a prefix may name, which [cobol.LiteralKind.Prefix]
// spells.
var literalKinds = []cobol.LiteralKind{
	cobol.LiteralHexadecimal,
	cobol.LiteralHexadecimalNumeric,
	cobol.LiteralOctalNumeric,
	cobol.LiteralNational,
	cobol.LiteralNullTerminated,
	cobol.LiteralBoolean,
}

// prefixed splits a string token into the kind its prefix names and its
// delimited body. A prefix is case-insensitive, as `cobol-go` reads one, and
// a token opening at its delimiter has none.
func prefixed(token string) (cobol.LiteralKind, string) {
	for _, kind := range literalKinds {
		prefix := kind.Prefix()
		if len(token) > len(prefix) && strings.EqualFold(token[:len(prefix)], prefix) {
			if delimiter := token[len(prefix)]; delimiter == '\'' || delimiter == '"' {
				return kind, token[len(prefix):]
			}
		}
	}

	return cobol.LiteralAlphanumeric, token
}

// numeric builds a numeric literal.
func numeric(text Text) *cobol.NumericLiteral {
	return &cobol.NumericLiteral{Pos: pos(text.Pos), Value: text.Value}
}

// toWord builds a word.
func toWord(name DataName) *cobol.Word {
	return &cobol.Word{Pos: pos(name.Pos), Value: name.Name}
}

// pos is a layout position as `cobol-go` holds one: the line and the column.
func pos(p layout.Pos) cobol.Pos {
	return cobol.Pos{Line: p.Line, Column: p.Column}
}
