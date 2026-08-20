// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"strings"

	"github.com/Zaba505/cpybkc/irpb"
)

// resolved refuses a field whose encoding leaves one of its five axes unset.
//
// This generator lays no bytes out and reads exactly one of the five. The
// charset decides whether an item's picture is drawn with [noCharset] beside it
// ([withCharset]) and whether a predicate's literals are spelled as text or as
// hex ([predicateResolved]); the sign convention, the byte order, the float
// format and the binary width staircase decide nothing this document draws.
// All five are refused all the same, and that is a decision docs/ir/SPEC.md,
// "Which consumers the rule binds" argues rather than this comment.
//
// Its short form is worth repeating here, because the alternative reading is
// the plausible one. A diagram is what an adopter consults to decide whether to
// trust a layout, so an axis defaulted here is a wrong fact handed to the
// person with nothing to check it against — the same error a reader makes, not
// a smaller one. And a rule binding only the axes a consumer happens to read
// today is a rule that narrows silently: the day the item table gains a column
// stating an item's byte order, the descriptors this generator accepts change
// with nothing in the diff saying so. A descriptor reaching any consumer with
// an axis unset is a bug in `resolve`, and refusing is how this one says so
// instead of drawing over it.
//
// Written here rather than imported from cmd/cpybkc-gen-go, which carries the
// same check under the same name. That is the reason this command's package
// comment gives for writing the version check, the diagnostic writer and the
// argument parser twice, and the reason [malformedError] repeats: a package
// both generators shared would be a convenience no third-party generator has,
// and being a generator with no such convenience is the thing this command
// exists to demonstrate. The duplication is the demonstration rather than an
// oversight in it — and this check is the one place that claim is load bearing,
// because a third-party generator has to arrive at the same refusal from the
// specification alone.
//
// item is what the refusal calls the field it is about, from [itemNamed]. It is
// threaded in rather than composed here because the failure is a bug in
// `resolve` that somebody has to go and find: "an item's encoding leaves byte
// order unresolved" is true of a copybook with three hundred elementary items
// and actionable on none of them, and both call sites are holding the node's
// identity when they ask.
func resolved(enc *irpb.Encoding, item string) error {
	if enc == nil {
		return malformed(item+" carries no encoding", axesRule)
	}

	unset := make([]string, 0, 5)

	if enc.GetCharset() == irpb.Charset_CHARSET_UNSPECIFIED {
		unset = append(unset, "charset")
	}

	if enc.GetSignConvention() == irpb.SignConvention_SIGN_CONVENTION_UNSPECIFIED {
		unset = append(unset, "sign convention")
	}

	if enc.GetByteOrder() == irpb.ByteOrder_BYTE_ORDER_UNSPECIFIED {
		unset = append(unset, "byte order")
	}

	if enc.GetFloatFormat() == irpb.FloatFormat_FLOAT_FORMAT_UNSPECIFIED {
		unset = append(unset, "float format")
	}

	// Named as docs/ir/SPEC.md names the axis rather than as the schema names
	// the field. `BinarySize` is what the message calls it and "binary width
	// staircase" is what the rule an adopter is about to go and read calls it,
	// and the diagnostic's whole job is to get them to that rule.
	if enc.GetBinarySize() == irpb.BinarySize_BINARY_SIZE_UNSPECIFIED {
		unset = append(unset, "binary width staircase")
	}

	if len(unset) == 0 {
		return nil
	}

	return malformed(fmt.Sprintf("the encoding of %s leaves %s unresolved", item, english(unset)), axesRule)
}

// itemNamed is what a refusal from [resolved] calls the item it is about: the
// name a table would show it under, and its node identifier where it has none.
//
// A FILLER has no name, so the identifier is not a fallback for a missing
// lookup here — it is the only thing there is to say about an item COBOL
// declined to name, and it is what the reader needs to find the node in an
// emitted descriptor.
func itemNamed(f *irpb.Field, id uint64) string {
	if name := strings.TrimSpace(nameOf(f.GetNames())); name != "" {
		return name
	}

	return fmt.Sprintf("node %d", id)
}

// axesRule is the `note:` line both refusals above carry.
//
// One sentence for the two of them because they are one rule broken two ways: a
// field with no encoding message at all and a field with an axis left at its
// zero value are the same producer bug seen from either side of whether the
// message was constructed.
const axesRule = "a producer must set all five axes on every field, and a consumer may not supply a default for one, " +
	"whether or not it lays bytes out; see docs/ir/SPEC.md, \"Which consumers the rule binds\""

// english joins a list the way a sentence does.
//
// Called with a non-empty list only, which every caller above guarantees by
// returning early on the empty one.
func english(items []string) string {
	switch len(items) {
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
	}
}
