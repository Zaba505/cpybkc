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
// block's lines are a file's lines two spaces further in. A negative depth is
// an error.
//
// Every clause kind and every operand kind `cobol-go` can hold in an entry is
// one Write knows, by an exhaustive type switch; a node it does not know is an
// [UnknownNodeError] rather than a clause dropped from the transcription, and
// nothing is written: the body is built in full before any of it reaches w, so
// a refused copybook leaves no partial definitions file.
//
// The output is a function of entries and depth alone. Positions and comments
// are not read.
func Write(w io.Writer, entries []*cobol.DataDescriptionEntry, depth int) error {
	if depth < 0 {
		return fmt.Errorf("an entry body is written at depth %d, and a depth is never negative", depth)
	}

	var out bytes.Buffer

	// The depth is the printer's own, by docs/layout/SPEC.md's depth rule:
	// `cobol-go` exports the function its printer indents by, so the writer
	// and the printer cannot disagree about where an entry sits.
	depths := cobol.EntryDepths(entries)

	for i, entry := range entries {
		line, err := entryText(entry)
		if err != nil {
			return err
		}

		out.WriteString(strings.Repeat(indentUnit, depth+depths[i]))
		out.WriteString(line)
		out.WriteByte('\n')
	}

	// WriteTo reports a short write as io.ErrShortWrite, which a bare Write
	// returning fewer bytes and no error would not.
	_, err := out.WriteTo(w)

	return err
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
// kinds of the pinned `cobol-go`: a literal is text, a word is a data-name, an
// ALL literal is `(all <operand>)` around the literal or the figurative
// constant it repeats, and an [cobol.Identifier], a [cobol.FunctionReference]
// or an [cobol.AddressOf] — which a procedure-division operand may be, and
// which no data-description entry the parser builds holds — is an
// [UnknownNodeError].
//
// A literal's text is the token as COBOL writes it: a prefixed literal's
// prefix, then its delimited body. `cobol-go` holds the two apart — the kind
// in [cobol.StringLiteral.Kind] and the body, delimiters included, in Value —
// and does not keep the case the prefix was written in, so the prefix is
// written as its printer writes it, in upper case.
func operandText(entry *cobol.DataDescriptionEntry, operand cobol.Type) (string, error) {
	switch o := operand.(type) {
	case *cobol.StringLiteral:
		if o != nil {
			return text(o.Kind.Prefix() + o.Value), nil
		}
	case *cobol.AllLiteral:
		if o != nil && o.Literal != nil {
			repeated, err := operandText(entry, o.Literal)
			if err != nil {
				return "", err
			}

			return form("all", repeated), nil
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

// Spell is a COBOL word as a data-name is written, wherever one is: the bare
// symbol, or `(word "<spelling>")` where `sexpr-go` would read the spelling as
// something other than that symbol. A layout's diagnostics quote the names its
// item positions hold through it, so that what they quote is what was written.
func Spell(spelling string) string { return word(spelling) }

// word is a COBOL word as a data-name: the bare symbol, or `(word "<spelling>")`
// where `sexpr-go` would read the spelling as something other than that symbol
// — the reader's own test, so that what is written is what it reads.
func word(spelling string) string {
	if ReadsAsSymbol(spelling) {
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
