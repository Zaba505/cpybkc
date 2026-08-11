// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package manifest

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// Every fault here names a field by the path it is written at —
// `generators[1].options` — rather than by a description of where it is. A
// manifest is JSON and JSON has one way of saying where in a document something
// is; a message that invented a second one would make the adopter translate
// between them while holding the file open.
//
// All of them carry a [diag.Span] into the manifest, so a fault reads the same
// as one out of the layout reader and lands somewhere an editor can open.

// NotFoundError is a manifest that is not there.
//
// It is separated from every other reason a file will not open because it is the
// one an adopter meets by running cpybkc somewhere else, and what they need to
// be told is what the file is for rather than what errno was.
type NotFoundError struct {
	// Path is where the manifest was looked for.
	Path string
}

// Error implements the error interface.
func (e *NotFoundError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *NotFoundError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: "there is no manifest here; a project is driven by a " + Name +
			" naming its layout, the copybooks it reads and the generators to run",
		Spans: []diag.Span{{File: e.Path}},
	}
}

// SyntaxError is a manifest [encoding/json] would not read.
//
// It is the one fault that ends the walk: there is no way to know where the
// value that failed to parse was meant to end, so every fault after it would be
// invented by the parser rather than found in the file. Whatever had already
// been collected is reported beside it.
type SyntaxError struct {
	// Span is where the JSON stopped making sense.
	Span diag.Span

	// Fault is the sentence the message is, because the four ways a manifest
	// can fail to be JSON — malformed, truncated, empty, more than one value —
	// are four things to say rather than one wording with a hole in it.
	Fault string

	// Err is what [encoding/json] returned, where there was one. It is kept and
	// reachable with [errors.As] so that a caller can tell a truncated file
	// from a malformed one without reading the sentence.
	Err error
}

// Error implements the error interface.
func (e *SyntaxError) Error() string { return e.Diagnostic().String() }

// Unwrap gives up the error [encoding/json] returned.
func (e *SyntaxError) Unwrap() error { return e.Err }

// Diagnostic is what the error says, and where.
func (e *SyntaxError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{Message: e.Fault, Spans: []diag.Span{e.Span}}
}

// TypeError is a value written as the wrong kind of JSON.
//
// It says what the field is before saying what was found, because a manifest is
// written by hand and the useful half is what belongs there —
// `"generators": {"name": "go"}` is one entry where a list of them belongs, and
// the fix is a pair of brackets rather than a different entry.
type TypeError struct {
	// Span is the value.
	Span diag.Span

	// Field is the path it is written at.
	Field string

	// Want is what the field is, in the words the message uses.
	Want string

	// Found names the JSON that was written instead.
	Found string
}

// Error implements the error interface.
func (e *TypeError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *TypeError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("%s is %s, and this one is %s", e.Field, e.Want, e.Found),
		Spans:   []diag.Span{e.Span},
	}
}

// UnknownFieldError is a field no object of a manifest has.
//
// The message names the fields the object does admit, because every one of these
// is a spelling question the adopter can answer from the list: a plural written
// singular, a field of a generator entry written at the top level, a field that
// belongs to some other tool's manifest.
type UnknownFieldError struct {
	// Span is the field name.
	Span diag.Span

	// Field is what was written.
	Field string

	// In names the object it was written in.
	In string

	// Known are the fields that object admits, in the order a manifest writes
	// them.
	Known []string
}

// Error implements the error interface.
func (e *UnknownFieldError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *UnknownFieldError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("%s has no field named %s; it carries %s", e.In, quote(e.Field), list(e.Known)),
		Spans:   []diag.Span{e.Span},
	}
}

// RepeatedFieldError is one field written twice in one object.
//
// It carries both positions, because the fault is the pair: either line is a
// perfectly good statement of the field on its own, and what the adopter has to
// decide is which of the two they meant. JSON's own answer — the last one wins —
// is not one this package takes, since it would silently discard something they
// wrote.
type RepeatedFieldError struct {
	// Span is the second statement.
	Span diag.Span

	// First is the one before it.
	First diag.Span

	// Field is the name written twice.
	Field string

	// In names the object carrying both.
	In string
}

// Error implements the error interface.
func (e *RepeatedFieldError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *RepeatedFieldError) Diagnostic() diag.Diagnostic {
	first := e.First
	first.Note = "the first one is here"

	return diag.Diagnostic{
		Message: fmt.Sprintf("%s carries %s twice", e.In, quote(e.Field)),
		Spans:   []diag.Span{e.Span, first},
	}
}

// MissingFieldError is a required field an object does not carry.
//
// The span is the object rather than a position the field would have had,
// because that is where the adopter has to write it — a field a manifest is
// missing has no place of its own.
type MissingFieldError struct {
	// Span is the object that should have carried it.
	Span diag.Span

	// Field is the one nobody wrote.
	Field string

	// In names the object.
	In string

	// Fault is why the field is required, in the words the message closes with.
	Fault string
}

// Error implements the error interface.
func (e *MissingFieldError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *MissingFieldError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("%s carries no %s; %s", e.In, e.Field, e.Fault),
		Spans:   []diag.Span{e.Span},
	}
}

// EmptyValueError is a field written with nothing in it.
//
// It is a fault of its own rather than a missing field, because `"layout": ""`
// is a line the adopter wrote and meant something by: reporting it as absent
// would send them to write a field the file visibly already has.
type EmptyValueError struct {
	// Span is the empty value.
	Span diag.Span

	// Field is the path it is written at.
	Field string

	// Fault is why something is required there, in the words the message closes
	// with.
	Fault string
}

// Error implements the error interface.
func (e *EmptyValueError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *EmptyValueError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("%s is empty; %s", e.Field, e.Fault),
		Spans:   []diag.Span{e.Span},
	}
}

// GeneratorNameError is a generator name that cannot be a filename component.
//
// docs/plugin/SPEC.md: a name **MUST** be non-empty and **MUST NOT** contain a
// `/`, because the name is the suffix of the `cpybkc-gen-<name>` file that is
// searched for on PATH. The rest of what that section says about a name is a
// **SHOULD** and is not enforced here: this package reports what an adopter has
// to fix, and it has no channel for advice they may take or leave.
type GeneratorNameError struct {
	// Span is the name.
	Span diag.Span

	// Name is what was written.
	Name string
}

// Error implements the error interface.
func (e *GeneratorNameError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *GeneratorNameError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"a generator name is the suffix of the cpybkc-gen-<name> executable it resolves to, and %s contains a /",
			quote(e.Name)),
		Spans: []diag.Span{e.Span},
	}
}

// OptionKeyError is an option key a generator could not be handed.
//
// docs/plugin/SPEC.md: an option reaches a generator as a single `--opt k=v`
// argument, everything before the first `=` is the key, and a key **MUST NOT**
// be empty or contain an `=`. Both are reported here rather than by the plugin
// that would receive them, because the manifest is where such a key is written
// and a plugin handed one cannot tell which half the adopter meant.
type OptionKeyError struct {
	// Span is the key.
	Span diag.Span

	// Key is what was written.
	Key string
}

// Error implements the error interface.
func (e *OptionKeyError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *OptionKeyError) Diagnostic() diag.Diagnostic {
	message := fmt.Sprintf("an option key contains no =, and %s does", quote(e.Key))
	if e.Key == "" {
		message = "an option key is empty, and a generator is handed each option as k=v"
	}

	return diag.Diagnostic{Message: message, Spans: []diag.Span{e.Span}}
}

// quote renders a name the way a message names one, so that an empty one and a
// name with a space in it are both visible as what they are.
func quote(name string) string { return strconv.Quote(name) }

// list joins names the way a message does, so that the fields an object admits
// read as a sentence rather than as a slice.
func list(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}
