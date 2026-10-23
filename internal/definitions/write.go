// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package definitions

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	cobol "github.com/Zaba505/cobol-go"
	sexpr "github.com/z5labs/sexpr-go"
)

// FileDepth is the base depth a definitions file is written at, and BlockDepth
// the one a `definitions-block`'s entries are: docs/layout/SPEC.md's "The
// printed form init writes" puts a block's entries two spaces further in than a
// file's, which is one depth.
const (
	FileDepth  = 0
	BlockDepth = 1
)

// indentUnit is one depth of the canonical printed form.
const indentUnit = "  "

// Write writes entries, as `cobol-go` parsed them, as an entry body in the
// canonical printed form docs/layout/SPEC.md's "The printed form init writes"
// gives: one entry per line, two spaces of indentation per depth, single spaces
// between the level, the name and each clause, and the level as two digits.
//
// depth is the depth the body starts at — [FileDepth] for a definitions file
// and [BlockDepth] for a block's body — so that one writer emits both, and a
// block's lines are a file's lines two spaces further in.
//
// Every clause kind and every operand kind `cobol-go` can hold in an entry is
// one Write knows, by an exhaustive type switch; a node it does not know is an
// [UnknownNodeError] rather than a clause dropped from the transcription. An
// entry the pinned `cobol-go` misreads is refused with a [MisreadValueError],
// as SPEC.md's "A condition-name cobol-go misreads is refused, not transcribed"
// requires. Either way nothing is written: the body is built in full before any
// of it reaches w, so a refused copybook leaves no partial definitions file.
//
// The output is a function of entries and depth alone. Positions and comments
// are not read, except to recognise a prefixed literal, whose word and literal
// are told apart from two values only by touching.
func Write(w io.Writer, entries []*cobol.DataDescriptionEntry, depth int) error {
	var out bytes.Buffer

	depths := entryDepths(entries)

	for i, entry := range entries {
		line, err := entryText(entry)
		if err != nil {
			return err
		}

		out.WriteString(strings.Repeat(indentUnit, depth+depths[i]))
		out.WriteString(line)
		out.WriteByte('\n')
	}

	_, err := w.Write(out.Bytes())

	return err
}

// entryDepths is each entry's depth by docs/layout/SPEC.md's depth rule, which
// is the one `cobol-go`'s printer indents by:
//
//   - an entry at levels 01–49 is as deep as the number of entries enclosing
//     it, counted by level numbers alone;
//   - 66 and 77 are at depth 0;
//   - an 88 is one deeper than the nearest entry before it that is not an 88.
//
// It is restated rather than called because the printer does not export it
// (Zaba505/cobol-go#138); TestTheDepthIsThePrinters holds the two equal over
// every fixture.
func entryDepths(entries []*cobol.DataDescriptionEntry) []int {
	depths := make([]int, len(entries))

	var open []int // the levels of the 01–49 entries still enclosing

	parent := 0 // the depth of the nearest entry that is not an 88

	for i, entry := range entries {
		if entry == nil {
			continue
		}

		switch entry.Level {
		case 88:
			depths[i] = parent + 1
		case 66, 77:
			depths[i] = 0
			parent = 0
		default:
			for len(open) > 0 && open[len(open)-1] >= entry.Level {
				open = open[:len(open)-1]
			}

			depths[i] = len(open)
			open = append(open, entry.Level)
			parent = depths[i]
		}
	}

	return depths
}

// entryText is one entry's form, without its indentation.
func entryText(entry *cobol.DataDescriptionEntry) (string, error) {
	if entry == nil {
		return "", &UnknownNodeError{Node: entry}
	}

	name, err := entryName(entry)
	if err != nil {
		return "", err
	}

	if err := misread(entry); err != nil {
		return "", err
	}

	parts := []string{"(entry", fmt.Sprintf("%02d", entry.Level), name}

	for _, clause := range entry.Clauses {
		text, err := clauseText(entry, clause)
		if err != nil {
			return "", err
		}

		parts = append(parts, text)
	}

	return strings.Join(parts, " ") + ")", nil
}

// entryName is the entry's name as docs/layout/SPEC.md's "What is written
// follows from what cobol-go reads" writes it: FILLER for an unnamed item at a
// level other than 66 and 88, whether the copybook wrote FILLER or omitted the
// name, and the name as the copybook spelled it everywhere else.
func entryName(entry *cobol.DataDescriptionEntry) (string, error) {
	switch {
	case entry.Name != nil && !entry.Filler:
		return word(entry.Name.Value), nil
	case entry.Level == 66 || entry.Level == 88:
		// `cobol-go` requires a name at both and never sets Filler there, so an
		// entry without one is not a parse.
		return "", &UnknownNodeError{Node: entry, Entry: entry}
	default:
		return fillerName, nil
	}
}

// fillerName is how an unnamed item is written.
const fillerName = "FILLER"

// clauseText is one clause's form. The switch is exhaustive over the clause
// kinds of the pinned `cobol-go`, and TestTheWriterKnowsEveryClause holds it to
// that by reading `cobol-go`'s source.
func clauseText(entry *cobol.DataDescriptionEntry, clause cobol.DataClause) (string, error) {
	unknown := &UnknownNodeError{Node: clause, Entry: entry}

	switch c := clause.(type) {
	case *cobol.RedefinesClause:
		if c == nil || c.Name == nil {
			return "", unknown
		}

		return form("redefines", word(c.Name.Value)), nil
	case *cobol.PictureClause:
		if c == nil {
			return "", unknown
		}

		return form("pic", text(c.Picture)), nil
	case *cobol.UsageClause:
		if c == nil {
			return "", unknown
		}

		return form("usage", c.Usage), nil
	case *cobol.ValueClause:
		if c == nil || len(c.Values) == 0 {
			return "", unknown
		}

		operands := make([]string, 0, len(c.Values))

		for _, spec := range c.Values {
			operand, err := valueSpecText(entry, spec)
			if err != nil {
				return "", err
			}

			operands = append(operands, operand)
		}

		return form("value", operands...), nil
	case *cobol.OccursClause:
		return occursText(entry, c)
	case *cobol.SignClause:
		if c == nil {
			return "", unknown
		}

		if c.Separate {
			return form("sign", c.Position, "SEPARATE"), nil
		}

		return form("sign", c.Position), nil
	case *cobol.SynchronizedClause:
		if c == nil {
			return "", unknown
		}

		if c.Direction != "" {
			return form("synchronized", c.Direction), nil
		}

		return form("synchronized"), nil
	case *cobol.JustifiedClause:
		if c == nil {
			return "", unknown
		}

		return form("justified"), nil
	case *cobol.BlankWhenZeroClause:
		if c == nil {
			return "", unknown
		}

		return form("blank-when-zero"), nil
	case *cobol.GlobalClause:
		if c == nil {
			return "", unknown
		}

		return form("global"), nil
	case *cobol.ExternalClause:
		if c == nil {
			return "", unknown
		}

		return form("external"), nil
	case *cobol.RenamesClause:
		if c == nil || c.From == nil {
			return "", unknown
		}

		if c.Through != nil {
			return form("renames", form("through", word(c.From.Value), word(c.Through.Value))), nil
		}

		return form("renames", word(c.From.Value)), nil
	default:
		return "", unknown
	}
}

// valueSpecText is one value or one range of a VALUE clause.
func valueSpecText(entry *cobol.DataDescriptionEntry, spec cobol.ValueSpec) (string, error) {
	from, err := operandText(entry, spec.From)
	if err != nil {
		return "", err
	}

	if spec.Through == nil {
		return from, nil
	}

	to, err := operandText(entry, spec.Through)
	if err != nil {
		return "", err
	}

	return form("through", from, to), nil
}

// operandText is one VALUE operand. The switch is exhaustive over the value
// kinds of the pinned `cobol-go`: a literal is text and a word is a data-name,
// and an [cobol.Identifier], a [cobol.FunctionReference] or an
// [cobol.AddressOf] — which a procedure-division operand may be, and which no
// data-description entry the parser builds holds — is an [UnknownNodeError].
func operandText(entry *cobol.DataDescriptionEntry, operand cobol.Type) (string, error) {
	switch o := operand.(type) {
	case *cobol.StringLiteral:
		if o != nil {
			return text(o.Value), nil
		}
	case *cobol.NumericLiteral:
		if o != nil {
			return text(o.Value), nil
		}
	case *cobol.Word:
		if o != nil {
			return word(o.Value), nil
		}
	}

	return "", &UnknownNodeError{Node: operand, Entry: entry}
}

// occursText is an OCCURS clause, its phrases in the order `cobol-go` holds
// them.
func occursText(entry *cobol.DataDescriptionEntry, c *cobol.OccursClause) (string, error) {
	if c == nil || c.Min == nil {
		return "", &UnknownNodeError{Node: c, Entry: entry}
	}

	parts := []string{text(c.Min.Value)}

	if c.Max != nil {
		parts = append(parts, form("to", text(c.Max.Value)))
	}

	if c.DependingOn != nil {
		parts = append(parts, form("depending-on", word(c.DependingOn.Value)))
	}

	for _, key := range c.Keys {
		if key.Name == nil {
			return "", &UnknownNodeError{Node: key, Entry: entry}
		}

		order := "DESCENDING"
		if key.Ascending {
			order = "ASCENDING"
		}

		parts = append(parts, form("key", order, word(key.Name.Value)))
	}

	if c.IndexedBy != nil {
		parts = append(parts, form("indexed-by", word(c.IndexedBy.Value)))
	}

	return form("occurs", parts...), nil
}

// form is `(<tag> <argument> …)`.
func form(tag string, arguments ...string) string {
	if len(arguments) == 0 {
		return "(" + tag + ")"
	}

	return "(" + tag + " " + strings.Join(arguments, " ") + ")"
}

// word is a COBOL word as a data-name: the bare symbol, or `(word "<spelling>")`
// where `sexpr-go` would read the spelling as something other than that symbol
// — the reader's own test, so that what is written is what it reads.
func word(spelling string) string {
	if readsAsSymbol(spelling) {
		return spelling
	}

	return form("word", text(spelling))
}

// text is a COBOL token as text, quoted by `sexpr-go`'s own printer so that the
// escaping is the grammar's and not a second copy of it here.
func text(value string) string {
	var out strings.Builder

	// Printing one string cannot fail: only a symbol, a nil comment or a dotted
	// tail can, and none is here.
	_ = sexpr.Print(&out, &sexpr.File{Nodes: []sexpr.Node{sexpr.String{Value: value}}})

	return strings.TrimSuffix(out.String(), "\n")
}

// misread reports a condition-name whose VALUE list the pinned `cobol-go`
// reads as other values than the copybook wrote, which docs/layout/SPEC.md's
// "A condition-name cobol-go misreads is refused, not transcribed" requires
// conversion to refuse:
//
//   - a prefixed literal, `X'00'` or `N'A'`, which it reads as a word and a
//     literal that touch;
//   - an `ALL` literal, which it reads as the word ALL and the literal after it;
//   - `VALUES ARE`, which it reads with ARE as the first value.
//
// Only a condition-name's list is affected. An ordinary item's VALUE takes one
// operand, so the same text there is a parse failure and never reaches this.
func misread(entry *cobol.DataDescriptionEntry) error {
	if entry.Level != 88 {
		return nil
	}

	for _, clause := range entry.Clauses {
		value, ok := clause.(*cobol.ValueClause)
		if !ok || value == nil {
			continue
		}

		var operands []cobol.Type

		for _, spec := range value.Values {
			operands = append(operands, spec.From)
			if spec.Through != nil {
				operands = append(operands, spec.Through)
			}
		}

		for i, operand := range operands {
			w, ok := operand.(*cobol.Word)
			if !ok || w == nil {
				continue
			}

			switch {
			case i == 0 && strings.EqualFold(w.Value, "ARE"):
				return &MisreadValueError{Entry: entry, Word: w, Construct: "VALUES ARE", Story: "Zaba505/cobol-go#135"}
			case i+1 < len(operands) && strings.EqualFold(w.Value, "ALL"):
				return &MisreadValueError{Entry: entry, Word: w, Construct: "an ALL literal", Story: "Zaba505/cobol-go#142"}
			case i+1 < len(operands) && touches(w, operands[i+1]):
				return &MisreadValueError{Entry: entry, Word: w, Construct: "a prefixed literal", Story: "Zaba505/cobol-go#140"}
			}
		}
	}

	return nil
}

// touches reports whether a literal starts where a word ends, on its line —
// which is what tells `X'00'`, one prefixed literal, from `X '00'`, a word and a
// literal the copybook wrote as two.
func touches(w *cobol.Word, next cobol.Type) bool {
	literal, ok := next.(*cobol.StringLiteral)
	if !ok || literal == nil {
		return false
	}

	return literal.Pos.Line == w.Pos.Line && literal.Pos.Column == w.Pos.Column+len(w.Value)
}
