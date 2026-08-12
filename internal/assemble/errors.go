// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package assemble

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// The faults this package reports fall into two halves, and the halves carry
// different things because they are about different objects.
//
// A fault [Assemble] finds is about something an adopter wrote — a record the
// automaton admits and no `record` form defines, a discriminator naming an item
// the record it selects does not hold — so it carries a span into the copybook
// or the layout, exactly as `resolve`'s do.
//
// A fault [Validate] finds is about the descriptor, and a descriptor carries no
// positions: no node holds a line, a column or a file, for the reason none holds
// an offset. So what identifies one is the node's identifier and a sentence, and
// there are two types rather than a dozen because the identifier and the
// sentence are the whole of what a caller can act on. A malformed descriptor is
// a bug in this repository in any case, and what makes one findable is which
// node and what about it, not which struct it came back in.

// ErrNoFraming is returned by [Assemble] when it is handed no framing.
//
// A descriptor's file node carries the dataset's framing as one member of a
// closed set of four, and a layout **MUST** carry exactly one `framing` form
// (docs/layout/SPEC.md, "Physical framing"). There is no fifth member standing
// for a framing nobody stated, and inventing one here would be a default
// surviving into the one form of the layout that exists to carry none.
var ErrNoFraming = errors.New("assemble: the layout states no framing, and a file node carries one")

// ErrNoAutomaton is returned by [Assemble] when it is handed no automaton.
//
// The file node names the start state, so a descriptor without an automaton has
// no root for anything else to hang off.
var ErrNoAutomaton = errors.New("assemble: the layout states no sequencing, and a file node names a start state")

// ErrNoRecords is returned by [Assemble] when it is handed no record types.
//
// Every transition admits one, so a descriptor with no record node is a
// descriptor whose automaton cannot take a single step.
var ErrNoRecords = errors.New("assemble: the layout defines no record types")

// DuplicateRecordError is two record types handed over under one name.
//
// A name is how a transition says which record it admits, so two of them make
// every transition admitting that name ambiguous. It is the layout reader that
// ordinarily refuses a duplicate record name (docs/layout/SPEC.md, "Validation
// and diagnostics"); this catches the pair that reached here by another road,
// because assembling either one silently would put a record type in the
// descriptor that nothing in the layout asked for.
type DuplicateRecordError struct {
	// Record is the name given twice.
	Record string

	// Copybook is the second entry's copybook, which is the file the caller
	// would look in to tell the two apart.
	Copybook string
}

// Error implements the error interface.
func (e *DuplicateRecordError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *DuplicateRecordError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"two record types are named %s, so no transition can say which of them it admits", e.Record),
		Spans: []diag.Span{{File: e.Copybook}},
	}
}

// UnnamedRenameError is a [Rename] handed over without the record type it was
// written under.
//
// A rename is per record (docs/layout/SPEC.md, "Many records may name one
// copybook, and two may name one item"), so a rename naming no record reaches
// nothing. It is reported rather than ignored because the descriptor that comes
// out of ignoring it is well formed: every node carries its copybook name and
// none carries the override the caller asked for, which is a layout's `rename`
// forms silently doing nothing at all.
//
// It is the shape a caller written against the older [Rename] produces — one
// carrying an item and a substitute and no record — and that caller still
// compiles, so a diagnostic is the only thing standing between it and output
// nobody checks.
type UnnamedRenameError struct {
	// Substitute is the name the rename asked for, which is what a caller can
	// find in the layout that wrote it.
	Substitute string

	// Item is the copybook item it named, and is empty where it named none.
	Item string
}

// Error implements the error interface.
func (e *UnnamedRenameError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *UnnamedRenameError) Diagnostic() diag.Diagnostic {
	target := "a record type"
	if e.Item != "" {
		target = e.Item
	}

	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"the rename substituting %s for %s names no record type, and a rename is per record",
			e.Substitute, target),
		Spans: []diag.Span{{Note: "every rename carries the record its layout form was written under"}},
	}
}

// UnknownRecordError is a transition admitting a record type nothing defines.
//
// The automaton names the records its transitions admit, and every one of them
// **MUST** resolve to a record node in the same message (docs/ir/SPEC.md,
// "Identity, ordering and determinism"). A transition pointing nowhere is the
// one shape a consumer cannot recover from: it has bytes in front of it and no
// record type to read them as.
//
// A [Record] handed over carrying no resolved record reports the same fault,
// because it is the same one seen a step earlier: the name is in the list and
// nothing resolved to it.
type UnknownRecordError struct {
	// Record is the name the transition admits.
	Record string

	// Defined are the record types that were handed over, in the order they
	// were, which is what tells a caller whether the name is misspelled or
	// missing.
	Defined []string
}

// Error implements the error interface.
func (e *UnknownRecordError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *UnknownRecordError) Diagnostic() diag.Diagnostic {
	message := fmt.Sprintf("a transition admits record %s, which no record type resolved to", e.Record)
	if len(e.Defined) > 0 {
		message += ": the record types are " + list(e.Defined)
	}

	return diag.Diagnostic{Message: message}
}

// UnresolvedTargetError is a reference naming a copybook item the record it
// reads has no field node for.
//
// Three positions name a field of the record a transition admits — a
// discriminator predicate, a register binding, and an `OCCURS DEPENDING ON`
// count — and each is a reference the descriptor states by identifier. An item
// that laid out as no elementary node of that record has no identifier to state,
// which is what this reports rather than emitting a reference to nothing.
type UnresolvedTargetError struct {
	// Pos is the copybook entry the item was declared at.
	Pos diag.Span

	// Record is the record type the reference reads.
	Record string

	// Item is the item named.
	Item string

	// Position is what named it, in the message's own words — "the predicate
	// selecting it", "a binding of register 0".
	Position string
}

// Error implements the error interface.
func (e *UnresolvedTargetError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *UnresolvedTargetError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf(
			"%s names %s, which is no field of record %s", e.Position, e.Item, e.Record),
		Spans: []diag.Span{e.Pos},
	}
}

// DescriptorError is one thing wrong with an assembled descriptor as a whole:
// its version, how many roots it has, or the order of its node list.
//
// It carries no span, because a descriptor has none. See the note at the top of
// this file.
type DescriptorError struct {
	// Fault is what is wrong, as a sentence.
	Fault string
}

// Error implements the error interface.
func (e *DescriptorError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *DescriptorError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{Message: "the assembled descriptor " + e.Fault}
}

// NodeError is one thing wrong with one node of an assembled descriptor.
type NodeError struct {
	// ID is the node's identifier.
	ID uint64

	// Kind is what the node is — "field", "transition" — or "no kind" for a
	// node whose body was never set.
	Kind string

	// Fault is what is wrong with it, as a sentence.
	Fault string
}

// Error implements the error interface.
func (e *NodeError) Error() string { return e.Diagnostic().String() }

// Diagnostic is what the error says, and where.
func (e *NodeError) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{
		Message: fmt.Sprintf("node %d, a %s node, %s", e.ID, e.Kind, e.Fault),
	}
}

// list joins names the way a message does, so that a list of what a caller could
// have meant reads as a sentence rather than as a slice.
func list(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	}

	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
