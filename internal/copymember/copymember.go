// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package copymember is the one resolver that turns a `COPY` statement into a
// file: docs/cli/SPEC.md's "Where a `COPY` member is found".
//
// A copybook that writes `COPY STDHDR.` is read with STDHDR's text in place of
// the statement, and `cobol-go` does the expanding. What it cannot know is
// which file STDHDR is, and it asks through the [cobol.CopyBookResolver] it is
// handed. cpybkc's answer is the file a member map names for that member and
// no other: there is no search, no directory a member is looked for in and no
// suffix tried after its name.
//
// The map is built by whoever has one to build it from — a run from its
// layout's `copy-member` forms, `init` from its `--copy-member` flags — and
// handed to [New]. Both then hand the [Resolver] to every `cobol.Parse` of
// copybook text through [cobol.WithCopyBooks], so that a copybook converted by
// `init` and the same copybook read by a run are expanded by the same rules
// into the same entries.
//
// # Matching
//
// A `COPY` names a member when the text-names are equal and the library-names
// are equal, each compared without regard to case, or when neither has a
// library-name ([Member.Names]). There is no fallback between the two shapes:
// `COPY STDHDR OF PAYLIB.` is not satisfied by a member STDHDR, and `COPY
// STDHDR.` is not satisfied by STDHDR OF PAYLIB.
//
// # Reading
//
// Each member file is read at most once per [Resolver], however many `COPY`
// statements name it, and however many members name the file: the bytes read
// are the bytes every expansion of it uses. A member nothing asks for is never
// read at all, which is what lets a layout's unreached `copy-member` form be
// reported with its file left unopened.
package copymember

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	cobol "github.com/Zaba505/cobol-go"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// Member is a member as a `COPY` statement names it: a text-name, and the
// library-name of an `OF` or `IN` phrase, or "" where the statement gave none.
//
// A text-name a `COPY` writes as a literal is held by its content, without its
// delimiters, because `cobol-go` hands a resolver the name that way and does
// not say which way it was written: the word and the literal are one member.
type Member struct {
	Name    string
	Library string
}

// String spells the member as a `COPY` statement writes it: `STDHDR`, or
// `STDHDR OF PAYLIB`.
func (m Member) String() string {
	if m.Library == "" {
		return m.Name
	}

	return m.Name + " OF " + m.Library
}

// Names reports whether a `COPY` naming name, in library, names m: the
// text-names equal and the library-names equal, without regard to case, or
// neither with a library-name.
func (m Member) Names(name, library string) bool {
	if !strings.EqualFold(m.Name, name) {
		return false
	}

	if m.Library == "" || library == "" {
		return m.Library == "" && library == ""
	}

	return strings.EqualFold(m.Library, library)
}

// Same reports whether m and other are one member under that matching, which
// is what makes two `copy-member` forms, or two `--copy-member` values, name
// one member.
func (m Member) Same(other Member) bool { return m.Names(other.Name, other.Library) }

// Form is one entry of a member map: a member, and the file holding it.
type Form struct {
	// Member is the member the form names.
	Member Member

	// Path is the file's path as whoever stated it spells it: a layout's
	// `copy-member` form, or a `--copy-member` value as typed. It is what
	// every diagnostic about the file, and every span into it, names.
	Path string

	// File is the path the resolver opens: Path, resolved against the
	// directory of whatever stated it.
	File string
}

// Resolver is the one resolver: a member map, and the member files it has read.
//
// It is not safe for concurrent use; a run and `init` each parse their
// copybooks one after another.
type Resolver struct {
	forms   []Form
	reached []bool
	read    map[string]read
	open    func(string) ([]byte, error)
}

// read is one member file, read: its bytes, or why it has none.
type read struct {
	src []byte
	err error
}

// New is a resolver over forms, which reads a member's file with [os.ReadFile]
// the first time a `COPY` names it.
//
// No two forms may name one member ([Member.Same]); the caller states that
// fault before building a resolver, at the place the two were written, and
// where two do the first is the one a `COPY` reads.
func New(forms []Form) *Resolver {
	return NewWith(forms, os.ReadFile)
}

// NewWith is [New] reading member files with open, which a test hands a
// filesystem it can count the reads of.
func NewWith(forms []Form, open func(string) ([]byte, error)) *Resolver {
	return &Resolver{
		forms:   append([]Form(nil), forms...),
		reached: make([]bool, len(forms)),
		read:    make(map[string]read),
		open:    open,
	}
}

// Forms are the member map, in the order it was given.
func (r *Resolver) Forms() []Form { return append([]Form(nil), r.forms...) }

// Lookup is the index of the form a `COPY` naming name in library names, and
// false where none does.
func (r *Resolver) Lookup(name, library string) (int, bool) {
	for i, form := range r.forms {
		if form.Member.Names(name, library) {
			return i, true
		}
	}

	return 0, false
}

// Reached reports whether some `COPY` has named the form at i so far.
func (r *Resolver) Reached(i int) bool { return i >= 0 && i < len(r.reached) && r.reached[i] }

// ResolveCopyBook implements [cobol.CopyBookResolver]: the bytes of the file
// the form naming the member holds, read once.
//
// A `COPY` naming a member no form names is an [UnnamedError], and a file that
// cannot be read is an [UnreadableError]. Both wrap [fs.ErrNotExist], which is
// how a resolver tells `cobol-go` that it has no such copybook; `cobol-go`
// wraps either in a [cobol.CopyBookNotFoundError] at the `COPY` statement.
func (r *Resolver) ResolveCopyBook(name, library string) ([]byte, error) {
	i, ok := r.Lookup(name, library)
	if !ok {
		return nil, &UnnamedError{Member: Member{Name: name, Library: library}, Named: r.members()}
	}

	r.reached[i] = true

	form := r.forms[i]

	file, already := r.read[form.File]
	if !already {
		src, err := r.open(form.File)
		file = read{src: src, err: err}
		r.read[form.File] = file
	}

	if file.err != nil {
		return nil, &UnreadableError{Form: form, Err: file.err}
	}

	return file.src, nil
}

// members are the members the map names, in its order.
func (r *Resolver) members() []Member {
	members := make([]Member, 0, len(r.forms))
	for _, form := range r.forms {
		members = append(members, form.Member)
	}

	return members
}

// UnnamedError is a `COPY` naming a member no form of the map names.
type UnnamedError struct {
	// Member is the member as the `COPY` named it.
	Member Member

	// Named are the members the map does name, in its order.
	Named []Member
}

// Error implements the error interface.
func (e *UnnamedError) Error() string {
	return fmt.Sprintf("COPY %s names no member the map names", e.Member)
}

// Unwrap gives up [fs.ErrNotExist]: the resolver has no such copybook.
func (e *UnnamedError) Unwrap() error { return fs.ErrNotExist }

// UnreadableError is a member file that cannot be read.
type UnreadableError struct {
	// Form is the form naming the file.
	Form Form

	// Err is what reading it failed with.
	Err error
}

// Error implements the error interface.
func (e *UnreadableError) Error() string {
	return fmt.Sprintf("no copy member at %s: %v", e.Form.Path, e.Err)
}

// Unwrap gives up the read failure, which wraps [fs.ErrNotExist] for a file
// that is not there, and is made to for one that is there and cannot be read,
// since either way the resolver has no copybook to give.
func (e *UnreadableError) Unwrap() []error {
	if errors.Is(e.Err, fs.ErrNotExist) {
		return []error{e.Err}
	}

	return []error{e.Err, fs.ErrNotExist}
}

// Fault is how a [cobol.CopyBookNotFoundError] or a [cobol.CopyBookCycleError]
// raised while a copybook was parsed through a [Resolver] is told apart: the
// `COPY` statement that drew it, and which of the three faults it is. A parse
// error of any other kind is no `COPY` fault, and is false.
type Fault struct {
	// At is the `COPY` statement, whose position names the text it is written
	// in.
	At cobol.Pos

	// Unnamed is set for a `COPY` naming a member no form names.
	Unnamed *UnnamedError

	// Unreadable is set for a member file that cannot be read.
	Unreadable *UnreadableError

	// Cycle is set for a `COPY` naming a member already open.
	Cycle *cobol.CopyBookCycleError
}

// FaultOf classifies err.
func FaultOf(err error) (Fault, bool) {
	var cycle cobol.CopyBookCycleError
	if errors.As(err, &cycle) {
		return Fault{At: cycle.Pos, Cycle: &cycle}, true
	}

	var notFound cobol.CopyBookNotFoundError
	if !errors.As(err, &notFound) {
		return Fault{}, false
	}

	fault := Fault{At: notFound.Pos}

	var unnamed *UnnamedError
	if errors.As(notFound.Err, &unnamed) {
		fault.Unnamed = unnamed

		return fault, true
	}

	var unreadable *UnreadableError
	if errors.As(notFound.Err, &unreadable) {
		fault.Unreadable = unreadable

		return fault, true
	}

	return Fault{}, false
}

// Span is where pos is, as a diagnostic points at it.
//
// A position in the source a copybook's own file holds is in file, which is
// that file as the caller names it in a diagnostic. A position in text a
// `COPY` brought in is in the member's file, named as its form spells it, at
// the line and column the text has there — and the span's [diag.Span.Via] is
// one span per `COPY` statement that reached it, innermost first, each at
// that statement in the file it is written in and noting the member it
// copied (docs/cli/SPEC.md, "What a `COPY` fault looks like"). A span into
// the copybook that wrote the `COPY`, at the position the entry has in the
// member, would point at a line that says something else.
//
// A line of zero is no position, and is file alone, as a span with no line is
// everywhere.
func (r *Resolver) Span(file string, pos cobol.Pos) diag.Span {
	at := r.place(file, pos)

	chain := pos.CopyChain()
	if len(chain) == 0 {
		return at
	}

	via := make([]diag.Span, 0, len(chain))

	copied := pos.Text

	for _, statement := range chain {
		step := r.place(file, statement)
		step.Note = "copied by COPY " + Member{Name: copied.Name, Library: copied.Library}.String()
		via = append(via, step)
		copied = statement.Text
	}

	at.Via = &diag.Chain{Spans: via}

	return at
}

// File is the file a position is in, as a diagnostic names it: file for the
// copying source's own, and the member's file as its form spells it for text a
// `COPY` brought in.
func (r *Resolver) File(file string, pos cobol.Pos) string {
	if pos.Text == nil {
		return file
	}

	if i, ok := r.Lookup(pos.Text.Name, pos.Text.Library); ok {
		return r.forms[i].Path
	}

	// Unreachable through Parse: a text a COPY brought in was asked of this
	// resolver, and only a form it names is ever handed back. The member's
	// own name is what is left to say.
	return Member{Name: pos.Text.Name, Library: pos.Text.Library}.String()
}

// place is a position's own span, with no chain.
func (r *Resolver) place(file string, pos cobol.Pos) diag.Span {
	at := diag.Span{File: r.File(file, pos)}
	if pos.Line > 0 {
		at.Line, at.Column = pos.Line, pos.Column
	}

	return at
}
