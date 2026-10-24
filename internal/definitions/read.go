// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package definitions

import (
	"fmt"
	"strings"

	cobol "github.com/Zaba505/cobol-go"
	sexpr "github.com/z5labs/sexpr-go"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
)

// FileBody is a definitions file's entry body: every one of its top-level
// forms, since a definitions file holds the body and nothing beside it.
func FileBody(file *layout.File) []layout.Node {
	body := make([]layout.Node, 0, len(file.Forms))
	for _, form := range file.Forms {
		body = append(body, form)
	}

	return body
}

// BlockBody is a `definitions-block`'s entry body: its forms after its name.
func BlockBody(block layout.Form) []layout.Node {
	if len(block.Elements) == 0 {
		return nil
	}

	return block.Elements[1:]
}

// Read reads an entry body the schema check has admitted, from a definitions
// file's top-level forms or a block's forms after its name.
//
// It returns every fault it found, joined, rather than the first, for the
// reason every reader in this repository does; each is assertable with
// [errors.As]. The faults are only those a declaration cannot state — see the
// package comment — and a body read with any of them returns no entries, as
// [github.com/Zaba505/cpybkc/internal/layout.Parse] returns no file: a body
// that was rejected has nothing anything downstream should read.
func Read(body []layout.Node) ([]Entry, error) {
	read := &reader{}
	entries := make([]Entry, 0, len(body))

	for _, node := range body {
		if entry, ok := read.entry(node); ok {
			entries = append(entries, entry)
		}
	}

	if read.Failed() {
		return nil, read.Err()
	}

	return entries, nil
}

// reader holds the faults one Read accumulates.
type reader struct {
	diag.List
}

// unchecked records a node the schema check would not have admitted.
func (r *reader) unchecked(node layout.Node, want string) {
	r.Fail(&UncheckedBodyError{Pos: node.Position(), Want: want})
}

// form narrows a node to a form with the given tag and at least min elements
// after it.
func (r *reader) form(node layout.Node, tag string, min int) (layout.Form, bool) {
	form, ok := node.(layout.Form)
	if !ok || (tag != "" && form.Tag != tag) || len(form.Elements) < min {
		want := "a form"
		if tag != "" {
			want = fmt.Sprintf("a (%s ...) form", tag)
		}

		r.unchecked(node, want)

		return layout.Form{}, false
	}

	return form, true
}

// entry reads one `(entry <level> <name> <clause> …)`.
func (r *reader) entry(node layout.Node) (Entry, bool) {
	form, ok := r.form(node, "entry", 2)
	if !ok {
		return Entry{}, false
	}

	level, ok := form.Elements[0].(layout.Int)
	if !ok {
		r.unchecked(form.Elements[0], "a level number")

		return Entry{}, false
	}

	entry := Entry{Pos: form.Pos, Level: Level{Pos: level.Pos, Value: level.Value}}

	name, nameOK := r.dataName(form.Elements[1])
	entry.Name = name

	clausesOK := true

	for _, element := range form.Elements[2:] {
		clause, ok := r.clause(element)
		if !ok {
			clausesOK = false

			continue
		}

		entry.Clauses = append(entry.Clauses, clause)
	}

	return entry, nameOK && clausesOK
}

// clause reads one of the twelve clause forms.
func (r *reader) clause(node layout.Node) (Clause, bool) {
	form, ok := r.form(node, "", 0)
	if !ok {
		return nil, false
	}

	switch form.Tag {
	case "redefines":
		name, ok := one(r, form, r.dataName)

		return Redefines{Pos: form.Pos, Name: name}, ok
	case "pic":
		picture, ok := one(r, form, func(node layout.Node) (Text, bool) { return r.text(node, KindPicture) })

		return Picture{Pos: form.Pos, Picture: picture}, ok
	case "usage":
		usage, ok := one(r, form, r.keyword)

		return Usage{Pos: form.Pos, Usage: usage}, ok
	case "value":
		return r.value(form)
	case "occurs":
		return r.occurs(form)
	case "sign":
		return r.sign(form)
	case "synchronized":
		return r.synchronized(form)
	case "justified":
		return Justified{Pos: form.Pos}, r.none(form)
	case "blank-when-zero":
		return BlankWhenZero{Pos: form.Pos}, r.none(form)
	case "global":
		return Global{Pos: form.Pos}, r.none(form)
	case "external":
		return External{Pos: form.Pos}, r.none(form)
	case "renames":
		renamed, ok := one(r, form, r.renamed)

		return Renames{Pos: form.Pos, Renamed: renamed}, ok
	default:
		r.unchecked(form, "one of the twelve clause forms")

		return nil, false
	}
}

// one reads a form taking exactly one argument, with read.
func one[T any](r *reader, form layout.Form, read func(layout.Node) (T, bool)) (T, bool) {
	if len(form.Elements) != 1 {
		r.unchecked(form, fmt.Sprintf("a (%s ...) form of one argument", form.Tag))

		var zero T

		return zero, false
	}

	return read(form.Elements[0])
}

// none reads a form taking no arguments, whose presence is the clause.
func (r *reader) none(form layout.Form) bool {
	if len(form.Elements) != 0 {
		r.unchecked(form, fmt.Sprintf("a (%s) form of no arguments", form.Tag))

		return false
	}

	return true
}

// value reads `(value <operand> …)`.
func (r *reader) value(form layout.Form) (Clause, bool) {
	if len(form.Elements) == 0 {
		r.unchecked(form, "a (value ...) form of one or more operands")

		return nil, false
	}

	value := Value{Pos: form.Pos}
	ok := true

	for _, element := range form.Elements {
		operand, read := r.operand(element, true)
		if !read {
			ok = false

			continue
		}

		value.Operands = append(value.Operands, operand)
	}

	return value, ok
}

// occursRank is the order an occurs holds its phrases in, which is the order
// `cobol-go` holds and prints them in.
var occursRank = map[string]int{"to": 0, "depending-on": 1, "key": 2, "indexed-by": 3}

// occurs reads `(occurs "<count>" <phrase> …)`, and holds its phrases to the
// order the grammar admits them in and to at most one of each but `key`.
func (r *reader) occurs(form layout.Form) (Clause, bool) {
	if len(form.Elements) == 0 {
		r.unchecked(form, "an (occurs ...) form with a count")

		return nil, false
	}

	count, ok := r.text(form.Elements[0], KindBound)
	occurs := Occurs{Pos: form.Pos, Count: count}

	var (
		seen = make(map[string]layout.Form)
		last *layout.Form
	)

	for _, element := range form.Elements[1:] {
		phrase, read := r.form(element, "", 0)
		if !read {
			ok = false

			continue
		}

		rank, known := occursRank[phrase.Tag]
		if !known {
			r.unchecked(phrase, "an occurs phrase: to, depending-on, key or indexed-by")

			ok = false

			continue
		}

		if first, repeated := seen[phrase.Tag]; repeated && phrase.Tag != "key" {
			r.Fail(&OccursPhraseError{Pos: phrase.Pos, Phrase: phrase.Tag, Other: first.Pos, OtherPhrase: first.Tag, Repeated: true})

			ok = false
		} else if last != nil && rank < occursRank[last.Tag] {
			r.Fail(&OccursPhraseError{Pos: phrase.Pos, Phrase: phrase.Tag, Other: last.Pos, OtherPhrase: last.Tag})

			ok = false
		}

		if _, repeated := seen[phrase.Tag]; !repeated {
			seen[phrase.Tag] = phrase
		}

		if last == nil || rank >= occursRank[last.Tag] {
			last = &phrase
		}

		if !r.phrase(&occurs, phrase) {
			ok = false
		}
	}

	return occurs, ok
}

// phrase reads one occurs phrase into occurs. A repeated phrase keeps the first,
// since the repeat has already been reported and nothing downstream reads a
// body that drew one.
func (r *reader) phrase(occurs *Occurs, phrase layout.Form) bool {
	switch phrase.Tag {
	case "to":
		bound, ok := one(r, phrase, func(node layout.Node) (Text, bool) { return r.text(node, KindBound) })
		if occurs.To == nil {
			occurs.To = &bound
		}

		return ok
	case "depending-on":
		name, ok := one(r, phrase, r.dataName)
		if occurs.DependingOn == nil {
			occurs.DependingOn = &name
		}

		return ok
	case "indexed-by":
		name, ok := one(r, phrase, r.dataName)
		if occurs.IndexedBy == nil {
			occurs.IndexedBy = &name
		}

		return ok
	default: // key
		if len(phrase.Elements) != 2 {
			r.unchecked(phrase, "a (key <order> <data-name>) phrase")

			return false
		}

		order, orderOK := r.keyword(phrase.Elements[0])
		name, nameOK := r.dataName(phrase.Elements[1])
		occurs.Keys = append(occurs.Keys, Key{Pos: phrase.Pos, Order: order, Name: name})

		return orderOK && nameOK
	}
}

// sign reads `(sign <position>)` or `(sign <position> SEPARATE)`.
func (r *reader) sign(form layout.Form) (Clause, bool) {
	if len(form.Elements) == 0 || len(form.Elements) > 2 {
		r.unchecked(form, "a (sign <position>) form, with SEPARATE or without")

		return nil, false
	}

	position, ok := r.keyword(form.Elements[0])
	sign := Sign{Pos: form.Pos, Edge: position}

	if len(form.Elements) == 2 {
		separate, read := r.keyword(form.Elements[1])
		sign.Separate = &separate
		ok = ok && read
	}

	return sign, ok
}

// synchronized reads `(synchronized)` or `(synchronized <direction>)`.
func (r *reader) synchronized(form layout.Form) (Clause, bool) {
	if len(form.Elements) > 1 {
		r.unchecked(form, "a (synchronized) form, with a direction or without")

		return nil, false
	}

	sync := Synchronized{Pos: form.Pos}

	if len(form.Elements) == 0 {
		return sync, true
	}

	direction, ok := r.keyword(form.Elements[0])
	sync.Direction = &direction

	return sync, ok
}

// renamed reads what a `renames` names: a data-name, or a range of them.
func (r *reader) renamed(node layout.Node) (Operand, bool) {
	if _, isText := node.(layout.Text); isText {
		r.unchecked(node, "a data-name or a (through ...) range")

		return nil, false
	}

	return r.operand(node, true)
}

// operand reads literal text, a data-name, an `(all <operand>)` or, where
// ranged is true, a `(through <from> <to>)` whose ends are themselves any of
// the first three.
func (r *reader) operand(node layout.Node, ranged bool) (Operand, bool) {
	switch node := node.(type) {
	case layout.Text:
		return r.text(node, KindLiteral)
	case layout.Form:
		if node.Tag == "through" && ranged {
			if len(node.Elements) != 2 {
				r.unchecked(node, "a (through <from> <to>) range")

				return nil, false
			}

			from, fromOK := r.operand(node.Elements[0], false)
			to, toOK := r.operand(node.Elements[1], false)

			return Through{Pos: node.Pos, From: from, To: to}, fromOK && toOK
		}

		if node.Tag == "all" {
			return r.all(node)
		}
	}

	return r.dataName(node)
}

// all reads `(all <operand>)`: literal text or a data-name, the one ALL
// repeats. A second `all` or a range inside one is the schema's to refuse, and
// whether `ALL` may precede what it holds is `cobol-go`'s.
func (r *reader) all(form layout.Form) (Operand, bool) {
	if len(form.Elements) != 1 {
		r.unchecked(form, "an (all <operand>) form of one argument")

		return nil, false
	}

	switch repeated := form.Elements[0].(type) {
	case layout.Text:
		text, ok := r.text(repeated, KindLiteral)

		return All{Pos: form.Pos, Repeated: text}, ok
	default:
		name, ok := r.dataName(repeated)

		return All{Pos: form.Pos, Repeated: name}, ok
	}
}

// dataName reads a COBOL word: a symbol, or `(word "<spelling>")` where the
// spelling would not read as one.
func (r *reader) dataName(node layout.Node) (DataName, bool) {
	switch node := node.(type) {
	case layout.Symbol:
		return DataName{Pos: node.Pos, Name: node.Value}, true
	case layout.Form:
		if node.Tag != "word" || len(node.Elements) != 1 {
			break
		}

		spelling, ok := node.Elements[0].(layout.Text)
		if !ok {
			break
		}

		name := DataName{Pos: node.Pos, Name: spelling.Value, Word: true}

		if ReadsAsSymbol(spelling.Value) {
			r.Fail(&NeedlessWordError{Pos: node.Pos, Spelling: spelling.Value})

			return name, false
		}

		return name, true
	}

	r.unchecked(node, "a data-name: a symbol, or (word \"<spelling>\")")

	return DataName{}, false
}

// ReadsAsSymbol reports whether sexpr-go reads spelling, alone, as exactly that
// symbol — which is when a `(word …)` around it is needless.
//
// It is exported because a layout's own positions that name a COBOL item take
// the same data-name sort an entry does, and one spelling rule read two ways
// would let a name be written bare in one place and only as a word in another.
func ReadsAsSymbol(spelling string) bool {
	file, err := sexpr.Parse(strings.NewReader(spelling))
	if err != nil || len(file.Nodes) != 1 {
		return false
	}

	symbol, ok := file.Nodes[0].(sexpr.Symbol)

	return ok && symbol.Value == spelling
}

// keyword reads an upper-case symbol from a closed set. Which values are
// admitted is `cobol-go`'s to judge, so none is refused here.
func (r *reader) keyword(node layout.Node) (Keyword, bool) {
	symbol, ok := node.(layout.Symbol)
	if !ok {
		r.unchecked(node, "a symbol")

		return Keyword{}, false
	}

	return Keyword{Pos: symbol.Pos, Value: symbol.Value}, true
}

// text reads a text and holds it to being exactly one COBOL token of kind.
func (r *reader) text(node layout.Node, kind TextKind) (Text, bool) {
	text, ok := node.(layout.Text)
	if !ok {
		r.unchecked(node, "text")

		return Text{}, false
	}

	read := Text{Pos: text.Pos, Value: text.Value}

	if reason := oneToken(text.Value, kind); reason != "" {
		r.Fail(&LiteralTextError{Pos: text.Pos, Text: text.Value, Kind: kind, Reason: reason})

		return read, false
	}

	return read, true
}

// oneToken reports why text is not exactly one COBOL token of kind, or "" when
// it is one.
//
// `cobol.Tokenize` is the judge of what a token is. A PICTURE character-string
// is a token only after PIC — the tokenizer reads one in that context and
// nowhere else — so a picture is tokenized behind a PIC, and the PIC is not
// counted.
func oneToken(text string, kind TextKind) string {
	source := text
	if kind == KindPicture {
		source = "PIC " + text
	}

	var tokens []cobol.Token

	for token, err := range cobol.Tokenize(strings.NewReader(source)) {
		if err != nil {
			return fmt.Sprintf("cobol.Tokenize refuses it: %v", err)
		}

		tokens = append(tokens, token)
	}

	if kind == KindPicture && len(tokens) > 0 {
		tokens = tokens[1:]
	}

	switch len(tokens) {
	case 0:
		return "it holds no token"
	case 1:
	default:
		values := make([]string, 0, len(tokens))
		for _, token := range tokens {
			values = append(values, fmt.Sprintf("%q", token.Value))
		}

		return fmt.Sprintf("it reads as %d tokens, %s", len(tokens), strings.Join(values, " "))
	}

	token := tokens[0]

	if string(token.Value) != text {
		return fmt.Sprintf("it reads as the token %q with more beside it", token.Value)
	}

	switch {
	case kind == KindPicture && token.Type == cobol.TokenPicture:
		return ""
	case kind == KindBound && token.Type == cobol.TokenNumber:
		return ""
	case kind == KindLiteral && token.Type == cobol.TokenString:
		// A prefix is case-insensitive to `cobol-go`, and kept by it as a kind
		// rather than as text, so it has one spelling here: the upper case
		// its printer writes and the converter therefore writes.
		if literalKind, _ := prefixed(text); literalKind != cobol.LiteralAlphanumeric && !strings.HasPrefix(text, literalKind.Prefix()) {
			return fmt.Sprintf("its prefix is written %s, as cobol-go prints it, and not %s",
				literalKind.Prefix(), text[:len(literalKind.Prefix())])
		}

		return ""
	case kind == KindLiteral && token.Type == cobol.TokenNumber:
		return ""
	case token.Type == cobol.TokenIdentifier:
		return "it reads as a COBOL word, which an entry body writes as a data-name rather than as text"
	default:
		return fmt.Sprintf("it reads as a %s token", strings.ToLower(token.Type.String()))
	}
}
