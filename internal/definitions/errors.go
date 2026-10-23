// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package definitions

import (
	"fmt"

	cobol "github.com/Zaba505/cobol-go"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
)

// NeedlessWordError is a `(word "<spelling>")` around a spelling that reads as
// a symbol anyway.
//
// docs/layout/SPEC.md's leaf rules give every data-name one spelling: a bare
// symbol, and `(word …)` only where `sexpr-go` would read the spelling as
// something else, which today is only `nil`. Two spellings for one name would
// be two texts for one transcription, so the needless one is a fault rather
// than a synonym. No declaration can say it, because what a spelling reads as
// is the grammar's, which is why it is this reader's and not the schema's.
type NeedlessWordError struct {
	// Pos is the `word` form.
	Pos layout.Pos

	// Spelling is the text it holds.
	Spelling string
}

// Error implements the error interface.
func (e *NeedlessWordError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *NeedlessWordError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("(word %q) is needless: %s reads as a symbol, and a data-name that does is written bare", e.Spelling, e.Spelling),
		Spans:   []diag.Span{span(e.Pos)},
	}
}

// TextKind is the kind of COBOL token a text position holds.
type TextKind int

const (
	// KindLiteral is a literal in a `value` or a `through`: an alphanumeric or
	// a numeric literal, delimiters included.
	KindLiteral TextKind = iota

	// KindPicture is a PICTURE character-string, as `pic` holds it.
	KindPicture

	// KindBound is an OCCURS bound, a numeric literal, as `occurs` and `to`
	// hold it.
	KindBound
)

// String names the kind as a diagnostic does.
func (k TextKind) String() string {
	switch k {
	case KindPicture:
		return "PICTURE character-string"
	case KindBound:
		return "numeric literal"
	default:
		return "COBOL literal"
	}
}

// LiteralTextError is a text that is not exactly one COBOL token of the kind
// its position holds.
//
// docs/layout/SPEC.md's leaf rules make every COBOL literal token, PICTURE
// string and OCCURS bound a text holding exactly that token as COBOL writes it.
// A text holding two tokens, none, a token and a space, or a word where a
// literal belongs is a transcription that does not say which token the copybook
// wrote, and the token is COBOL's to recognise — `cobol.Tokenize` decides, not
// a second lexis here.
type LiteralTextError struct {
	// Pos is the text.
	Pos layout.Pos

	// Text is what the text holds.
	Text string

	// Kind is the kind of token its position holds.
	Kind TextKind

	// Reason says what `cobol.Tokenize` read instead.
	Reason string
}

// Error implements the error interface.
func (e *LiteralTextError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *LiteralTextError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("the text %q is not exactly one %s: %s", e.Text, e.Kind, e.Reason),
		Spans:   []diag.Span{span(e.Pos)},
	}
}

// OccursPhraseError is an `occurs` phrase written twice where it may be written
// once, or out of the order the grammar admits the phrases in.
//
// An `occurs` holds its count and then `to`, `depending-on`, any number of
// `key` and `indexed-by`, in that order, which is the order `cobol-go` holds and
// prints them in, and `to`, `depending-on` and `indexed-by` at most once each
// (docs/layout/SPEC.md, "An entry, and one form per clause"). Both rules count
// one form against another, which no declaration states, so they are this
// reader's.
type OccursPhraseError struct {
	// Pos is the phrase that is repeated or out of place.
	Pos layout.Pos

	// Phrase is its tag.
	Phrase string

	// Other is the phrase it collides with: the first of the two for a repeat,
	// and the one it is written after for an order fault.
	Other layout.Pos

	// OtherPhrase is that phrase's tag.
	OtherPhrase string

	// Repeated reports a repeat rather than an order fault.
	Repeated bool
}

// Error implements the error interface.
func (e *OccursPhraseError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *OccursPhraseError) Diagnostic() diag.Diagnostic {
	if e.Repeated {
		other := span(e.Other)
		other.Note = "the first is here"

		return diag.Diagnostic{
			Message: fmt.Sprintf("an occurs holds at most one (%s ...), and this is a second", e.Phrase),
			Spans:   []diag.Span{span(e.Pos), other},
		}
	}

	other := span(e.Other)
	other.Note = fmt.Sprintf("the (%s ...) it follows", e.OtherPhrase)

	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"(%s ...) is written after (%s ...); an occurs holds to, depending-on, key and indexed-by in that order",
			e.Phrase, e.OtherPhrase,
		),
		Spans: []diag.Span{span(e.Pos), other},
	}
}

// UncheckedBodyError is a body [Read] was handed that the schema check would
// not have admitted.
//
// It is not a diagnostic about an adopter's text. [Read] takes a body
// [github.com/Zaba505/cpybkc/internal/layoutschema] has already checked, and
// every fault of shape is that check's to report, in the schema's words; one
// arriving here is a caller that skipped it. It is reported rather than
// recovered from so that the reader never invents a second message for a fault
// a declaration states.
type UncheckedBodyError struct {
	// Pos is the node that is not what the schema admits there.
	Pos layout.Pos

	// Want is what the position holds.
	Want string
}

// Error implements the error interface.
func (e *UncheckedBodyError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *UncheckedBodyError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("an entry body read without the schema check: this position holds %s", e.Want),
		Spans:   []diag.Span{span(e.Pos)},
	}
}

// span is a layout position as a diagnostic carries one.
func span(pos layout.Pos) diag.Span {
	return diag.Span{File: pos.File, Line: pos.Line, Column: pos.Column}
}

// UnknownNodeError is a node [Write] cannot transcribe: a clause or an operand
// of a kind it does not know, or one `cobol-go`'s parser never builds — a nil
// entry, a clause missing the operand it always carries, an unnamed 88.
//
// It is the failure that keeps the writer from dropping what it does not
// understand. A clause kind the pinned `cobol-go` gains on a pin bump arrives
// here until the notation has a form for it, which CONTRIBUTING's "Bumping the
// cobol-go pin" requires in the same change.
type UnknownNodeError struct {
	// Node is what could not be written.
	Node any

	// Entry is the entry it belongs to, or nil when the entry itself is the
	// node.
	Entry *cobol.DataDescriptionEntry
}

// Error implements the error interface.
func (e *UnknownNodeError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where. The span is the entry's line
// and column in the copybook, which the caller knows the name of and this does
// not.
func (e *UnknownNodeError) Diagnostic() diag.Diagnostic {
	d := diag.Diagnostic{Message: fmt.Sprintf("an entry body has no form for a %T", e.Node)}

	if e.Entry != nil {
		d.Message = fmt.Sprintf("an entry body has no form for a %T in %s", e.Node, entryLabel(e.Entry))
		d.Spans = []diag.Span{{Line: e.Entry.Pos.Line, Column: e.Entry.Pos.Column}}
	}

	return d
}

// MisreadValueError is a condition-name whose VALUE list the pinned `cobol-go`
// misreads, which conversion refuses rather than transcribing
// (docs/layout/SPEC.md, "A condition-name cobol-go misreads is refused, not
// transcribed").
type MisreadValueError struct {
	// Entry is the condition-name.
	Entry *cobol.DataDescriptionEntry

	// Word is the word `cobol-go` read as a value of its own: the prefix, ALL,
	// or ARE.
	Word *cobol.Word

	// Construct names what the copybook wrote.
	Construct string

	// Story is the `cobol-go` story that reads it, which lifts the refusal.
	Story string
}

// Error implements the error interface.
func (e *MisreadValueError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where: at the word, in the copybook.
func (e *MisreadValueError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"%s writes %s in its VALUE list, which the pinned cobol-go reads as other values than it holds, so it is not converted until %s",
			entryLabel(e.Entry), e.Construct, e.Story,
		),
		Spans: []diag.Span{{Line: e.Word.Pos.Line, Column: e.Word.Pos.Column}},
	}
}

// entryLabel names an entry in a message: its level and its name.
func entryLabel(entry *cobol.DataDescriptionEntry) string {
	switch {
	case entry.Name != nil:
		return fmt.Sprintf("the %02d %s", entry.Level, entry.Name.Value)
	default:
		return fmt.Sprintf("the %02d %s", entry.Level, fillerName)
	}
}

// KeyOrderError is a `(key <order> …)` whose order is neither ASCENDING nor
// DESCENDING.
//
// It is the one closed-set value [Construct] cannot hand to the judge: every
// other one `cobol-go` holds as the text it read, so printing an unknown one
// and reading it back is how the judge refuses it, but a key's order is a
// boolean there, and an order that is not ASCENDING would otherwise be built as
// DESCENDING and read back as itself.
type KeyOrderError struct {
	// Pos is the order.
	Pos layout.Pos

	// Order is what it holds.
	Order string
}

// Error implements the error interface.
func (e *KeyOrderError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *KeyOrderError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("a key's order is ASCENDING or DESCENDING, as cobol-go holds one, and %s is neither", e.Order),
		Spans:   []diag.Span{span(e.Pos)},
	}
}

// InadmissibleEntryError is an entry `cobol-go` does not read back as itself:
// docs/layout/SPEC.md's "cobol-go judges an entry" refusing it.
//
// The entry was printed with `cobol.Print` and the result read back in free
// format with `cobol.Parse(WithFragment())`. Either step refused it, or what it
// read back is a different entry, and the error quotes the printed COBOL so the
// reader sees what `cobol-go` was shown.
type InadmissibleEntryError struct {
	// Pos is the entry form.
	Pos layout.Pos

	// Printed is the COBOL `cobol.Print` wrote for the entry, without its line
	// end, or empty when it would not print it.
	Printed string

	// Reason is what `cobol-go` said: its error, or what it read back instead.
	Reason string
}

// Error implements the error interface.
func (e *InadmissibleEntryError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *InadmissibleEntryError) Diagnostic() diag.Diagnostic {
	message := fmt.Sprintf("cobol-go does not read this entry back as itself: %s", e.Reason)
	if e.Printed != "" {
		message = fmt.Sprintf("cobol-go does not read this entry back as itself: printed as %q, %s", e.Printed, e.Reason)
	}

	return diag.Diagnostic{Message: message, Spans: []diag.Span{span(e.Pos)}}
}
