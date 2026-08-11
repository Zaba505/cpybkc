// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// The objects a manifest is made of, named the way a message refers to one, and
// the fields each of them admits.
//
// The sets are here rather than beside the code that switches on them because
// they are what a diagnostic offers an adopter who wrote a field name nobody
// knows: the switch and the list an unknown field is reported against are the
// same set, written once.
const (
	manifestObject  = "a manifest"
	generatorObject = "a generator entry"
	optionObject    = "a generator's option set"
)

var (
	manifestFields  = []string{"layout", "generators"}
	generatorFields = []string{"name", "out", "options"}
)

// What a message says a field is for, once, so that the sentence a missing
// field produces and the sentence an empty one produces cannot drift.
const (
	layoutFault     = "a project resolves its records against exactly one"
	generatorsFault = "there is nothing for a run to do without one"
	nameFault       = "a generator is found on PATH as cpybkc-gen-<name>"
	outFault        = "its output lands in the directory out names"
)

// parser walks a manifest as a stream of JSON tokens.
//
// [encoding/json] would unmarshal one into a struct in a line, and that is not
// enough twice over: a struct field cannot keep the order a generator's options
// were written in, which docs/plugin/SPEC.md requires be preserved, and an
// unmarshalling error carries a byte offset that has already lost which field
// it was in. Walking the tokens keeps both — the order because the walk is the
// file's order, and the position because every token's offset is known as it is
// read.
type parser struct {
	file string
	src  []byte

	// lines are the byte offsets each line of src starts at, so that turning an
	// offset into a line and a column is a search rather than a second pass
	// over the file per fault.
	lines []int

	dec    *json.Decoder
	faults diag.List
}

func newParser(file string, src []byte) *parser {
	lines := []int{0}

	for offset, b := range src {
		if b == '\n' {
			lines = append(lines, offset+1)
		}
	}

	return &parser{
		file:  file,
		src:   src,
		lines: lines,
		dec:   json.NewDecoder(bytes.NewReader(src)),
	}
}

// manifest reads the whole file: the top-level object, and every field of it.
//
// The error it returns is the fatal one — malformed JSON, which ends the walk.
// Everything else is recorded in p.faults and reading continues.
func (p *parser) manifest() (*Manifest, error) {
	open, span, err := p.token()
	if err != nil {
		return nil, err
	}

	if !isOpen(open, '{') {
		p.faults.Fail(&TypeError{Span: span, Field: manifestObject, Want: "a JSON object", Found: found(open)})

		return nil, p.discardAfter(open)
	}

	m := &Manifest{File: p.file}
	seen := map[string]diag.Span{}

	for p.dec.More() {
		key, keySpan, err := p.key()
		if err != nil {
			return nil, err
		}

		if first, repeated := seen[key]; repeated {
			p.faults.Fail(&RepeatedFieldError{Span: keySpan, First: first, Field: key, In: manifestObject})

			if err := p.discard(); err != nil {
				return nil, err
			}

			continue
		}

		seen[key] = keySpan

		switch key {
		case "layout":
			m.Layout, _, err = p.requiredText("layout", "the layout file written as text", layoutFault)
		case "generators":
			m.Generators, err = p.generators()
		default:
			p.faults.Fail(&UnknownFieldError{Span: keySpan, Field: key, In: manifestObject, Known: manifestFields})

			err = p.discard()
		}

		if err != nil {
			return nil, err
		}
	}

	if _, _, err := p.token(); err != nil {
		return nil, err
	}

	p.required(span, seen, manifestObject, [][2]string{
		{"layout", layoutFault},
		{"generators", generatorsFault},
	})

	return m, nil
}

// generators reads the list of generator entries.
func (p *parser) generators() ([]Generator, error) {
	open, span, err := p.token()
	if err != nil {
		return nil, err
	}

	if !isOpen(open, '[') {
		p.faults.Fail(&TypeError{
			Span: span, Field: "generators", Want: "a list of generator entries", Found: found(open),
		})

		return nil, p.discardAfter(open)
	}

	var (
		generators []Generator
		entries    int
	)

	for ; p.dec.More(); entries++ {
		generator, ok, err := p.generator(fmt.Sprintf("generators[%d]", entries))
		if err != nil {
			return nil, err
		}

		if ok {
			generators = append(generators, generator)
		}
	}

	if _, _, err := p.token(); err != nil {
		return nil, err
	}

	// The count rather than len(generators): a list holding two entries this
	// package could not read is a list with two things wrong with it, and
	// reporting it as empty as well would send the adopter to write entries
	// they have already written.
	if entries == 0 {
		p.faults.Fail(&EmptyValueError{Span: span, Field: "generators", Fault: generatorsFault})
	}

	return generators, nil
}

// generator reads one entry of the generators list, under the field path a
// diagnostic about it names — `generators[1]`.
//
// The bool is whether there is an entry to keep. An entry that is not an object
// at all leaves nothing behind; one that is an object with faults in it is kept,
// because the faults are already recorded and a half-read entry beside them
// makes the rest of the list's positions read correctly.
func (p *parser) generator(field string) (Generator, bool, error) {
	open, span, err := p.token()
	if err != nil {
		return Generator{}, false, err
	}

	if !isOpen(open, '{') {
		p.faults.Fail(&TypeError{
			Span: span, Field: field, Want: "a generator entry, which is a JSON object", Found: found(open),
		})

		return Generator{}, false, p.discardAfter(open)
	}

	generator := Generator{Span: span}
	seen := map[string]diag.Span{}

	for p.dec.More() {
		key, keySpan, err := p.key()
		if err != nil {
			return Generator{}, false, err
		}

		if first, repeated := seen[key]; repeated {
			p.faults.Fail(&RepeatedFieldError{Span: keySpan, First: first, Field: key, In: generatorObject})

			if err := p.discard(); err != nil {
				return Generator{}, false, err
			}

			continue
		}

		seen[key] = keySpan

		switch key {
		case "name":
			var nameSpan diag.Span

			generator.Name, nameSpan, err = p.requiredText(
				field+".name", "a generator name written as text", nameFault)
			if strings.Contains(generator.Name, "/") {
				p.faults.Fail(&GeneratorNameError{Span: nameSpan, Name: generator.Name})
			}
		case "out":
			generator.Out, _, err = p.requiredText(
				field+".out", "a directory path written as text", outFault)
		case "options":
			generator.Options, err = p.options(field + ".options")
		default:
			p.faults.Fail(&UnknownFieldError{Span: keySpan, Field: key, In: generatorObject, Known: generatorFields})

			err = p.discard()
		}

		if err != nil {
			return Generator{}, false, err
		}
	}

	if _, _, err := p.token(); err != nil {
		return Generator{}, false, err
	}

	p.required(span, seen, generatorObject, [][2]string{
		{"name", nameFault},
		{"out", outFault},
	})

	return generator, true, nil
}

// options reads a generator's options, keeping the order they were written in.
func (p *parser) options(field string) ([]Option, error) {
	open, span, err := p.token()
	if err != nil {
		return nil, err
	}

	if !isOpen(open, '{') {
		p.faults.Fail(&TypeError{
			Span: span, Field: field, Want: "an object of generator options", Found: found(open),
		})

		return nil, p.discardAfter(open)
	}

	var options []Option

	seen := map[string]diag.Span{}

	for p.dec.More() {
		key, keySpan, err := p.key()
		if err != nil {
			return nil, err
		}

		repeated := false

		if first, ok := seen[key]; ok {
			p.faults.Fail(&RepeatedFieldError{Span: keySpan, First: first, Field: key, In: optionObject})

			repeated = true
		} else {
			seen[key] = keySpan
		}

		// docs/plugin/SPEC.md: a key MUST NOT be empty and MUST NOT contain an
		// `=`, because everything before the first `=` of `--opt k=v` is the
		// key. A manifest is where such a key is written, so it is where the
		// fault is reported rather than in the plugin that would be handed an
		// argument nobody can split.
		usable := key != "" && !strings.Contains(key, "=")
		if !usable {
			p.faults.Fail(&OptionKeyError{Span: keySpan, Key: key})
		}

		// An option value may be empty — the plugin contract says so — so this
		// is the plain read rather than the required one.
		value, _, ok, err := p.text(field+"."+key, "an option value written as text")
		if err != nil {
			return nil, err
		}

		if ok && usable && !repeated {
			options = append(options, Option{Key: key, Value: value})
		}
	}

	if _, _, err := p.token(); err != nil {
		return nil, err
	}

	return options, nil
}

// required reports every field of known that the object at span did not carry.
//
// The pairs are the field and the clause the message closes with, in the order
// a manifest writes them, so that an object missing two of them is two things
// to fix in the order they would be written rather than in a map's.
func (p *parser) required(span diag.Span, seen map[string]diag.Span, in string, known [][2]string) {
	for _, field := range known {
		if _, ok := seen[field[0]]; !ok {
			p.faults.Fail(&MissingFieldError{Span: span, Field: field[0], In: in, Fault: field[1]})
		}
	}
}

// requiredText reads a string value that has to carry something.
//
// An empty one is a fault rather than an absent field: `"layout": ""` is a line
// the adopter wrote and meant something by, and treating it as unwritten would
// report it as missing from a manifest it is visibly in.
func (p *parser) requiredText(field, want, fault string) (string, diag.Span, error) {
	value, span, ok, err := p.text(field, want)
	if err != nil || !ok {
		return "", span, err
	}

	if value == "" {
		p.faults.Fail(&EmptyValueError{Span: span, Field: field, Fault: fault})
	}

	return value, span, nil
}

// text reads a string value. The bool is whether one was there; anything else is
// reported and skipped, so that the fields after it are still read.
func (p *parser) text(field, want string) (string, diag.Span, bool, error) {
	tok, span, err := p.token()
	if err != nil {
		return "", span, false, err
	}

	value, ok := tok.(string)
	if !ok {
		p.faults.Fail(&TypeError{Span: span, Field: field, Want: want, Found: found(tok)})

		return "", span, false, p.discardAfter(tok)
	}

	return value, span, true, nil
}

// key reads an object's field name.
func (p *parser) key() (string, diag.Span, error) {
	tok, span, err := p.token()
	if err != nil {
		return "", span, err
	}

	key, ok := tok.(string)
	if !ok {
		// Unreachable through [encoding/json], which produces a string token
		// for an object key or an error for anything else. It is here rather
		// than as a panic because a fault in a file an adopter wrote is
		// reported to them even when this package cannot explain it.
		return "", span, &SyntaxError{
			Span:  span,
			Fault: "the manifest has " + found(tok) + " where a field name belongs",
		}
	}

	return key, span, nil
}

// trailing reports anything written after the manifest's one top-level object.
func (p *parser) trailing() {
	span := p.next()

	tok, err := p.dec.Token()

	switch {
	case errors.Is(err, io.EOF):
		return
	case err != nil:
		p.faults.Fail(p.syntaxError(span, err))
	default:
		p.faults.Fail(&SyntaxError{
			Span: span,
			Fault: "the manifest carries " + found(tok) +
				" after the object it opens with; a manifest is one JSON object and nothing else",
		})
	}
}

// token reads the next JSON token and the place it starts.
func (p *parser) token() (json.Token, diag.Span, error) {
	span := p.next()

	tok, err := p.dec.Token()
	if err != nil {
		return nil, span, p.syntaxError(span, err)
	}

	return tok, span, nil
}

// discard reads the next value and throws it away, whatever it is.
func (p *parser) discard() error {
	tok, _, err := p.token()
	if err != nil {
		return err
	}

	return p.discardAfter(tok)
}

// discardAfter reads whatever is left of the value tok opened.
//
// A scalar is already whole and leaves nothing to read; an object or a list is
// read to its own closing delimiter, counting depth, so that a field this
// package could not use does not leave the walk standing inside it.
func (p *parser) discardAfter(tok json.Token) error {
	delim, ok := tok.(json.Delim)
	if !ok || (delim != '{' && delim != '[') {
		return nil
	}

	for depth := 1; depth > 0; {
		next, _, err := p.token()
		if err != nil {
			return err
		}

		if d, ok := next.(json.Delim); ok {
			switch d {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}

	return nil
}

// next is where the token about to be read starts.
//
// [encoding/json] reports the offset it has reached, which is the end of the
// token before this one; the punctuation and the whitespace between the two are
// skipped here so that a diagnostic points at the value rather than at the comma
// in front of it.
func (p *parser) next() diag.Span {
	offset := int(p.dec.InputOffset())

	for offset < len(p.src) && isSeparator(p.src[offset]) {
		offset++
	}

	return p.spanAt(offset)
}

// spanAt is the place in the manifest that byte offset names.
func (p *parser) spanAt(offset int) diag.Span {
	if offset < 0 {
		offset = 0
	}

	if offset > len(p.src) {
		offset = len(p.src)
	}

	line := sort.Search(len(p.lines), func(i int) bool { return p.lines[i] > offset }) - 1

	return diag.Span{
		File: p.file,
		Line: line + 1,

		// Runes rather than bytes, because the column is for a person looking
		// at the line in an editor and a copybook path is not always ASCII.
		Column: utf8.RuneCount(p.src[p.lines[line]:offset]) + 1,
	}
}

// syntaxError is what [encoding/json] refused, said where it happened.
func (p *parser) syntaxError(span diag.Span, err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return &SyntaxError{
			Span:  p.spanAt(len(p.src)),
			Fault: "the manifest ends before the JSON in it does",
			Err:   err,
		}
	}

	// The span is the walk's own — where the token that would not read starts —
	// rather than the offset [encoding/json] puts on a *json.SyntaxError. That
	// offset is counted differently by the two paths a Token call can fail on,
	// one of them naming the offending byte and the other the byte after it, so
	// a diagnostic built from it points one column off depending on which of
	// them raised the fault. Where the reader had reached is exact whichever
	// path it was, and it is the same place the message is about.
	return &SyntaxError{Span: span, Fault: "the manifest is not valid JSON: " + err.Error(), Err: err}
}

// isSeparator reports whether b is punctuation or whitespace between two tokens.
func isSeparator(b byte) bool {
	switch b {
	case ' ', '\t', '\r', '\n', ',', ':':
		return true
	default:
		return false
	}
}

// isOpen reports whether tok opens the object or the list want does.
func isOpen(tok json.Token, want json.Delim) bool {
	delim, ok := tok.(json.Delim)

	return ok && delim == want
}

// found names a JSON token the way a message refers to it.
func found(tok json.Token) string {
	switch value := tok.(type) {
	case json.Delim:
		switch value {
		case '{':
			return "an object"
		case '[':
			return "a list"
		case '}':
			return "the end of an object"
		case ']':
			return "the end of a list"
		}
	case string:
		return "text"
	case float64, json.Number:
		return "a number"
	case bool:
		return "a boolean"
	case nil:
		return "null"
	}

	return "something else"
}
