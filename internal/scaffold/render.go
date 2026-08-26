// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package scaffold

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The two shapes a line of a scaffold takes.
//
// A comment is the only place a layout has for text its reader ignores
// (docs/layout/SPEC.md, "Tagged forms over S-expressions"), and it is therefore
// where everything cpybkc cannot state goes. Two semicolons rather than one,
// which is what the format's own examples and the ledger example's layout use.
const (
	commentMark   = ";;"
	commentPrefix = commentMark + " "

	// indent is one level of a form's children. Two spaces, which is what the
	// layout format is written with everywhere else in this repository.
	indent = "  "
)

// The placeholders. Each is a symbol rather than a value of the sort the
// position takes, which is what makes uncommenting a form without filling it in
// a fault the layout reader reports naming the placeholder rather than a layout
// that quietly means something nobody wrote.
//
// The spelling is docs/layout/SPEC.md's own skeleton notation, so a reader who
// has the format in front of them meets the same convention in both. It is
// explicitly not covered by the compatibility guarantees, which is what leaves
// the wording of everything around it free as well.
const (
	charsetHole        = "<charset>"
	signConventionHole = "<sign-convention>"
	byteOrderHole      = "<byte-order>"
	floatFormatHole    = "<float-format>"
	recfmHole          = "<recfm>"
	readingHole        = "<reading>"
	substituteHole     = "<substitute>"
	strategyHole       = "<strategy>"
	predicateHole      = "<predicate>"
	operatorHole       = "<operator>"
)

// Bytes renders the scaffold.
//
// The forms come in the order docs/layout/SPEC.md tables the top-level forms in,
// so that a form the adopter uncomments is already where the format's own
// examples put it, and a scaffold carries every item of that order its copybooks
// reach and nothing else.
//
// It is a function of the scaffold and of nothing else — no clock, no
// environment, no map iterated — so two runs of one build over one set of
// copybooks produce byte-identical files. That is a requirement rather than a
// hoped-for property: a scaffold that reordered itself between runs would be
// undiffable in exactly the review where an adopter is deciding what to keep.
func (s *Scaffold) Bytes() []byte {
	var out strings.Builder

	s.header(&out)
	s.encoding(&out)
	s.framing(&out)
	s.definitions(&out)
	s.reading(&out)
	s.renames(&out)
	s.discriminators(&out)
	s.variantDiscriminators(&out)
	s.sequence(&out)

	return []byte(out.String())
}

// header says what the file is, what was derived and what is left.
func (s *Scaffold) header(out *strings.Builder) {
	comment(out,
		"A layout scaffold, written by `cpybkc init` from the copybooks it was given.",
		"",
		"What is uncommented below is what those copybooks decide: one record per",
		"01-level, and one alternative child per REDEFINES outside a repeating group.",
		"What is commented is what a copybook does not decide and you do -- the",
		"encoding, the physical framing, what tells one record type from another, and",
		"the order they may appear in.",
		"",
		"So this is not a layout yet. Run cpybkc against it and everything still",
		"missing is reported at once, each at the place it belongs; work down that",
		"list, uncomment the form it names and fill in every <placeholder>. A",
		"placeholder left as it stands is reported by name, so a form uncommented and",
		"left half-written is never quietly accepted.",
	)
}

// encoding raises the four axes and states none of them.
//
// The values are not listed even for the three closed axes. A listed value reads
// as a recommendation, and the charset axis is deliberately open-ended, so any
// list here goes stale the release a code page is added.
func (s *Scaffold) encoding(out *strings.Builder) {
	blank(out)
	comment(out, "The encoding profile: all four axes, always, with no default for any.")
	comment(out,
		"(encoding",
		indent+"(charset "+charsetHole+")",
		indent+"(sign-convention "+signConventionHole+")",
		indent+"(byte-order "+byteOrderHole+")",
		indent+"(float-format "+floatFormatHole+"))",
	)
}

// framing raises the record format and says that the rest of the form follows
// from it.
func (s *Scaffold) framing(out *strings.Builder) {
	blank(out)
	comment(out,
		"How the dataset frames a record. Which other children `framing` takes",
		"follows from the record format chosen: an lrecl under F and FB, a maximum",
		"segment under VBS, a delimiter and its placement under line-sequential.",
	)
	comment(out,
		"(framing",
		indent+"(recfm "+recfmHole+"))",
	)
}

// definitions writes the `record` forms, which are the whole of what this
// command states outright.
func (s *Scaffold) definitions(out *strings.Builder) {
	blank(out)
	comment(out,
		"The record types these copybooks resolve to. Each names the copybook and the",
		"01-level it is a description of and -- where a REDEFINES outside a repeating",
		"group gives that 01-level more than one description -- which one it means.",
	)

	for _, r := range s.records {
		if len(r.alternatives) == 0 {
			line(out, "(record "+r.name+" "+r.copybookChild()+")")

			continue
		}

		line(out, "(record "+r.name)
		line(out, indent+r.copybookChild())

		for at, alternative := range r.alternatives {
			closing := ""
			if at == len(r.alternatives)-1 {
				closing = ")"
			}

			line(out, indent+"(alternative "+alternative.String()+")"+closing)
		}
	}
}

// copybookChild is the `copybook` child naming the file as it was typed and the
// 01-level inside it.
func (r record) copybookChild() string {
	return "(copybook " + quote(r.path) + " " + r.item + ")"
}

// reading raises the OCCURS DEPENDING ON question, when and only when some
// copybook carries the clause, and names both readings without choosing one.
func (s *Scaffold) reading(out *strings.Builder) {
	if !s.tables {
		return
	}

	blank(out)
	comment(out,
		"A copybook here carries an OCCURS DEPENDING ON, so the layout has to say",
		"which reading the program that wrote your file used: `odoslide`, under which",
		"every item behind such a table moves with the count, or `noodoslide`, under",
		"which the table is fixed at the copybook's declared maximum and the count is",
		"an ordinary field beside it. It is a property of that compiler, and no",
		"copybook holds it.",
	)
	comment(out,
		"(copybook-reading",
		indent+"(occurs-depending-on "+readingHole+"))",
	)
}

// renames raises one question per record over an 01-level that resolved to more
// than one record type, and answers none of them.
//
// No rename is emitted, only a commented one. A rename substitutes the name the
// IR carries — the name a generator turns into an identifier in somebody's
// public API — and a machine-invented substitute would land there permanently.
func (s *Scaffold) renames(out *strings.Builder) {
	if len(s.renamed) == 0 {
		return
	}

	blank(out)
	comment(out,
		"Several record types over one 01-level carry one name between them -- the",
		"one that 01-level gives them -- until a rename says otherwise, and a",
		"generator that cannot spell two of them alike refuses the collision rather",
		"than munging it. What each should be called is a reading of what the file",
		"means, and no copybook holds it.",
	)

	for _, name := range s.renamed {
		comment(out, "(rename "+name+" "+substituteHole+")")
	}
}

// discriminators raises one `discriminate` per record and states no strategy.
func (s *Scaffold) discriminators(out *strings.Builder) {
	blank(out)
	comment(out,
		"What selects each record type: exactly one of these per record, including",
		"for a record carrying nothing to test -- requiring it is what makes that a",
		"statement you made rather than a gap in the file.",
	)

	for _, r := range s.records {
		comment(out, "(discriminate "+r.name+" "+strategyHole+")")
	}
}

// variantDiscriminators raises one `discriminate-variant` per redefine inside a
// repeating group, with the variant and its arms filled in.
func (s *Scaffold) variantDiscriminators(out *strings.Builder) {
	if len(s.variants) == 0 {
		return
	}

	blank(out)
	comment(out,
		"A REDEFINES inside a repeating group is chosen once per occurrence rather",
		"than once per record, so it is a variant with an arm per alternative and",
		"never an alternative child. Which names are alternatives there is the",
		"copybook's; what selects each one is yours.",
	)

	for _, v := range s.variants {
		lines := make([]string, 0, len(v.arms)+1)
		lines = append(lines, "(discriminate-variant "+v.item.String())

		for at, arm := range v.arms {
			closing := ""
			if at == len(v.arms)-1 {
				closing = ")"
			}

			lines = append(lines, indent+"(arm "+arm+" "+predicateHole+")"+closing)
		}

		comment(out, lines...)
	}
}

// sequence names every record once and states no order.
//
// The operator is a placeholder rather than one of the format's, and that is the
// point: which of `seq`, `alt`, `*`, `+`, `?`, `times` and `when` describes a
// file is not in a copybook, and a `seq` written here would be an order cpybkc
// invented sitting one keystroke away from being believed.
func (s *Scaffold) sequence(out *strings.Builder) {
	blank(out)
	comment(out,
		"The order records may appear in. Every record is named once below and the",
		"operator is deliberately left blank: this states no order, and which of",
		"seq, alt, *, +, ?, times and when describes your file is yours to choose.",
	)

	lines := make([]string, 0, len(s.records)+2)
	lines = append(lines, "(sequence", indent+"("+operatorHole)

	for at, r := range s.records {
		closing := ""
		if at == len(s.records)-1 {
			closing = "))"
		}

		lines = append(lines, indent+indent+r.name+closing)
	}

	comment(out, lines...)
}

// comment writes each line as a comment, so that everything cpybkc cannot state
// is text the reader of a layout ignores.
//
// A line with nothing in it becomes a bare comment mark rather than an empty
// line, which is what keeps a paragraph one block a reader can uncomment or
// delete in one gesture.
func comment(out *strings.Builder, lines ...string) {
	for _, text := range lines {
		if text == "" {
			line(out, commentMark)

			continue
		}

		line(out, commentPrefix+text)
	}
}

// blank separates two blocks of the file.
func blank(out *strings.Builder) { out.WriteString("\n") }

// line writes one line of the scaffold.
func line(out *strings.Builder, text string) {
	out.WriteString(text)
	out.WriteString("\n")
}

// quote renders a path as the string literal a `copybook` child takes.
//
// The escape set is the grammar's own — the one `sexpr-go`'s printer writes and
// its tokenizer reads back — because the scaffold has to parse whatever a
// copybook is called, and a POSIX path may hold any byte but a slash and a NUL.
// A newline in a path is rare and it is legal, and writing one raw would produce
// the single failure the incompleteness is not allowed to be: a file reported as
// one lexical fault instead of as the checklist it is meant to be.
//
// Nothing beyond that set is touched. A non-ASCII path goes out as itself rather
// than as a run of escapes, because the path is written as it was typed and one
// cpybkc re-spelled would be one the adopter cannot find in the command line
// they wrote.
func quote(path string) string {
	var out strings.Builder

	out.Grow(len(path) + 2)
	out.WriteByte('"')

	for _, r := range path {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		default:
			// Every other control character has no short escape in this
			// grammar, and a byte that is not valid UTF-8 would not survive
			// being written raw, so both go out as \uXXXX. unicode.IsControl
			// covers DEL and the C1 range as well as C0.
			if unicode.IsControl(r) || r == utf8.RuneError {
				_, _ = fmt.Fprintf(&out, `\u%04X`, r)

				continue
			}

			out.WriteRune(r)
		}
	}

	out.WriteByte('"')

	return out.String()
}
