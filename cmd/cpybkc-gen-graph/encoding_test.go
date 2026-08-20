// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/irpb"
)

// TestAnItemLeavingAnEncodingAxisUnsetIsRefused is #297's decision held where
// it costs something: this generator lays no bytes out, and refuses anyway.
//
// docs/ir/SPEC.md, "Which consumers the rule binds" makes the axis rule bind
// every consumer rather than only the ones that read bytes, and the argument
// for it is that a diagram is what an adopter checks a layout against. So the
// axis nothing here draws is refused exactly as the one it draws is: a rule
// that bound only the axes a consumer happens to read would narrow silently the
// day this document gained a column stating one.
//
// The table walks the five one at a time. Clearing them one at a time rather
// than all at once is what makes the diagnostic's naming of the axis a thing
// the test checks rather than a thing it prints.
func TestAnItemLeavingAnEncodingAxisUnsetIsRefused(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		clear func(*irpb.Encoding)
		axis  string
	}{
		{
			name:  "charset",
			clear: func(e *irpb.Encoding) { e.Charset = irpb.Charset_CHARSET_UNSPECIFIED },
			axis:  "charset",
		},
		{
			name:  "sign convention",
			clear: func(e *irpb.Encoding) { e.SignConvention = irpb.SignConvention_SIGN_CONVENTION_UNSPECIFIED },
			axis:  "sign convention",
		},
		{
			name:  "byte order",
			clear: func(e *irpb.Encoding) { e.ByteOrder = irpb.ByteOrder_BYTE_ORDER_UNSPECIFIED },
			axis:  "byte order",
		},
		{
			name:  "float format",
			clear: func(e *irpb.Encoding) { e.FloatFormat = irpb.FloatFormat_FLOAT_FORMAT_UNSPECIFIED },
			axis:  "float format",
		},
		{
			// Named as docs/ir/SPEC.md names the axis and not as the schema
			// names the field: the diagnostic's job is to get a reader to the
			// rule, and the rule calls it the staircase.
			name:  "binary width staircase",
			clear: func(e *irpb.Encoding) { e.BinarySize = irpb.BinarySize_BINARY_SIZE_UNSPECIFIED },
			axis:  "binary width staircase",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := read(oneRecordAutomaton(
				edgeNode(30, 100, 2, nil, nil, nil),
				unresolvedFieldNode(102, "DTL-COUNT", 2, testCase.clear),
			), defaults())

			if err == nil {
				t.Fatalf("read drew a table over an item whose %s the descriptor never resolved", testCase.axis)
			}

			if !strings.Contains(err.Error(), testCase.axis) {
				t.Errorf("the refusal reads %q, and does not name the axis that is unset", err)
			}

			if !strings.Contains(err.Error(), "unresolved") {
				t.Errorf("the refusal reads %q, and does not say the axis is unresolved", err)
			}

			// The item as well as the axis. A refusal naming neither the item
			// nor its identifier is true of every field in the record and
			// actionable on none of them, and the axis assertions above would
			// pass just as well without it.
			if !strings.Contains(err.Error(), "DTL-COUNT") {
				t.Errorf("the refusal reads %q, and does not name the item it is about", err)
			}
		})
	}
}

// TestAnItemCarryingNoEncodingAtAllIsRefused is the same producer bug seen from
// the other side of whether the message was constructed.
//
// A field with no encoding message answers every axis with its zero value, so a
// consumer reading one axis off it gets a plausible answer to a question the
// descriptor never answered. It is refused as its own sentence rather than as
// five unset axes, because "carries no encoding" is what somebody holding the
// descriptor will see and "leaves five axes unresolved" is a reading of it.
func TestAnItemCarryingNoEncodingAtAllIsRefused(t *testing.T) {
	t.Parallel()

	_, err := read(oneRecordAutomaton(
		edgeNode(30, 100, 2, nil, nil, nil),
		&irpb.Node{Id: 102, Kind: &irpb.Node_Field{Field: &irpb.Field{
			Width:   2,
			Usage:   irpb.Usage_USAGE_DISPLAY,
			Picture: &irpb.Picture{Category: irpb.Category_CATEGORY_ALPHANUMERIC},
			Names:   &irpb.Names{Original: "DTL-COUNT"},
		}}},
	), defaults())

	if err == nil {
		t.Fatal("read drew a table over an item carrying no encoding at all")
	}

	if !strings.Contains(err.Error(), "DTL-COUNT carries no encoding") {
		t.Errorf("the refusal reads %q, and does not say which item carries no encoding", err)
	}
}

// TestAPredicatesTargetLeavingAnAxisUnsetIsRefused is the second of the two
// places this generator reads a field's encoding, and the one the item tables'
// option does not switch off.
//
// A predicate's literals are spelled as text or as hex from the target field's
// charset, so an unresolved charset there is not a missing cell — it is a
// literal drawn in a notation this document chose for itself. That is the
// default docs/ir/SPEC.md forbids, and it is the case that makes the rule bind
// a consumer laying no bytes out: the row is wrong in a way nobody reading the
// diagram can see, and the diagram is what they are checking the layout with.
//
// Held under `records=none` deliberately. Under the default the item table
// would refuse the same field first and this test would pass without the check
// in [predicateResolved] existing at all.
func TestAPredicatesTargetLeavingAnAxisUnsetIsRefused(t *testing.T) {
	t.Parallel()

	sequencing := options{records: recordsNone}.defaulted()

	_, err := read(oneRecordAutomaton(
		edgeNode(30, 100, 2, predicateAt(50), nil, nil),
		equalPredicate(50, 106, "H"),
		unresolvedFieldNode(106, "KIND", 1, func(e *irpb.Encoding) {
			e.Charset = irpb.Charset_CHARSET_UNSPECIFIED
		}),
	), sequencing)

	if err == nil {
		t.Fatal("read spelled a predicate's literal from a charset the descriptor never resolved")
	}

	if !strings.Contains(err.Error(), "charset") {
		t.Errorf("the refusal reads %q, and does not name the axis that is unset", err)
	}

	if !strings.Contains(err.Error(), "KIND") {
		t.Errorf("the refusal reads %q, and does not name the item it is about", err)
	}
}

// TestAnUnresolvedItemIsOnlyRefusedWhereItIsRead is the limit on the rule, and
// the reason [read] takes the options rather than the emitter alone.
//
// docs/ir/SPEC.md's "Which consumers the rule binds" attaches the duty to
// reading a field's encoding rather than to holding a descriptor: refusing over
// a part of the message this document does not describe would be refusing on
// somebody else's behalf, and would make the diagnostic depend on which
// sections the caller asked for. So an item no table draws and no predicate
// tests is passed over under `records=none`, exactly as
// [TestAMalformedItemIsOnlyReadWhereItIsDrawn] has a field carrying no USAGE
// passed over.
//
// The same descriptor is refused under the default, which is what keeps this a
// statement about where the check runs rather than about whether it does.
func TestAnUnresolvedItemIsOnlyRefusedWhereItIsRead(t *testing.T) {
	t.Parallel()

	// KIND is reached by the containment walk and by nothing else: no predicate
	// tests it and no binding reads it.
	broken := oneRecordAutomaton(
		edgeNode(30, 100, 2, nil, nil, nil),
		unresolvedFieldNode(106, "KIND", 1, func(e *irpb.Encoding) {
			e.ByteOrder = irpb.ByteOrder_BYTE_ORDER_UNSPECIFIED
		}),
	)

	if _, err := read(broken, defaults()); err == nil {
		t.Error("read drew a table over an item whose byte order the descriptor never resolved")
	}

	sequencing := options{records: recordsNone}.defaulted()

	if _, err := read(broken, sequencing); err != nil {
		t.Errorf("records=none refused a descriptor over an item it does not draw: %v", err)
	}
}

// TestAResolvedDescriptorIsDrawn is the control the four above need.
//
// Every one of them asserts a refusal, and a [resolved] that refused everything
// would satisfy all four. This is the fixture corpus's ordinary field, drawn.
func TestAResolvedDescriptorIsDrawn(t *testing.T) {
	t.Parallel()

	if _, err := read(oneRecordAutomaton(edgeNode(30, 100, 2, nil, nil, nil)), defaults()); err != nil {
		t.Errorf("read refused a descriptor whose every axis is resolved: %v", err)
	}
}

// unresolvedFieldNode is [fieldNode] with one axis of its encoding put back to
// unset, which is the descriptor no producer may emit and this generator must
// refuse.
//
// It clears an axis of a resolved encoding rather than building an encoding
// with one axis set, so that a sixth axis added to the message arrives here as
// a fixture that is still whole apart from the one thing each case is about.
func unresolvedFieldNode(id uint64, original string, width uint32, clear func(*irpb.Encoding)) *irpb.Node {
	node := fieldNode(id, original, width)
	clear(node.GetField().GetEncoding())

	return node
}
