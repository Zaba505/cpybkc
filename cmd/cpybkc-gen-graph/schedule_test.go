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

// armScheduledAt and armScheduledGroupAt are the two shapes of an arm the
// layout chooses by its position in the table: a field body and a group one.
//
// Beside [armAt] and [armGroupAt] rather than replacing them, because the pair
// is the point. The two kinds of selector are a choice in the schema, every arm
// of one variant carries the same member of it, and a fixture that could only
// spell one of them could not state the mixed variant these tests refuse.
func armScheduledAt(occurrences []uint32, field uint64) *irpb.Arm {
	return &irpb.Arm{
		Selector: scheduleOf(occurrences),
		Body:     &irpb.Arm_FieldId{FieldId: field},
	}
}

func armScheduledGroupAt(occurrences []uint32, group uint64) *irpb.Arm {
	return &irpb.Arm{
		Selector: scheduleOf(occurrences),
		Body:     &irpb.Arm_GroupId{GroupId: group},
	}
}

func scheduleOf(occurrences []uint32) *irpb.Arm_Schedule {
	return &irpb.Arm_Schedule{Schedule: &irpb.Schedule{OccurrenceNumbers: occurrences}}
}

// scheduledAutomaton is the descriptor the scheduled golden is drawn from:
// discussion #340's shape, beside the shape it is not.
//
// SCHEDULE-RECORD is the layout that discussion describes — a table whose count
// says how many entries are present, and a variant inside it whose arms are
// chosen by which occurrence the reader is in rather than by any byte of that
// occurrence. Occurrence 1 is an opening entry and occurrences 2 and 3 are
// adjustments, which is a schedule covering 1..3 exactly once, as
// docs/ir/SPEC.md's "An arm may be selected by its position in the table"
// requires of the arms of one variant.
//
// BYTES-RECORD is the same shape chosen the other way, and it is in this
// descriptor rather than in one of its own so that the two presence columns
// land in one document. What the arms of a variant are told apart by is the
// last column and nothing else — they have no names of their own — so "a
// schedule reads differently from a predicate" is a claim best made where a
// reader can see both without opening a second file.
func scheduledAutomaton() *irpb.Descriptor {
	return &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			unframedFile(1, 2),

			stateNode(2, false, 10),
			stateNode(3, true, 11),

			edgeNode(10, 100, 3, predicateAt(50), nil, nil),
			edgeNode(11, 200, 3, predicateAt(51), nil, nil),

			equalPredicate(50, 101, "\xc1"),
			equalPredicate(51, 201, "\xc2"),

			// The arms of BYTES-RECORD's variant, each reading a field of the
			// occurrence the variant sits in.
			equalPredicate(60, 211, "\xc1"),
			equalPredicate(61, 211, "\xc3"),

			recordOf(100, 105, "SCHEDULE-RECORD"),
			groupNode(105, "SCHEDULE-RECORD", 101, 102, 110),
			fieldNode(101, "REC-TYPE", 1),
			numericFieldNode(102, "ENTRY-COUNT", 1, 1),

			repeatingGroupNode(110, "ENTRIES", fieldCount(102, 1, 3), 111),
			variantNode(111,
				armScheduledGroupAt([]uint32{1}, 112),
				armScheduledAt([]uint32{2, 3}, 115)),
			groupNode(112, "OPENING", 113, 114),
			fieldNode(113, "OPENING-CODE", 2),
			numericFieldNode(114, "OPENING-BALANCE", 4, 4),
			fieldNode(115, "ADJUSTMENT", 6),

			recordOf(200, 205, "BYTES-RECORD"),
			groupNode(205, "BYTES-RECORD", 201, 210),
			fieldNode(201, "REC-KIND", 1),

			repeatingGroupNode(210, "ITEMS", constantCount(3), 211, 212),
			fieldNode(211, "ITEM-KIND", 1),
			variantNode(212, armGroupAt(60, 213), armAt(61, 215)),
			groupNode(213, "CASH", 214),
			numericFieldNode(214, "CASH-AMOUNT", 4, 4),
			fieldNode(215, "CHEQUE-REF", 4),
		},
	}
}

// TestAScheduledArmSaysWhichOccurrencesTakeIt is the first criterion: an arm
// chosen by its position says so in the column that says what makes a row
// present, and says it in numbers.
//
// The numbers rather than a word meaning "positionally", because what a reader
// holding discussion #340's layout is checking is which occurrence holds which
// alternative. A cell reading "chosen by position" would be true of every arm
// of the variant and would answer nothing they came with.
func TestAScheduledArmSaysWhichOccurrencesTakeIt(t *testing.T) {
	t.Parallel()

	rowCount(t, scheduledAutomaton(), "SCHEDULE-RECORD", 8)

	rows := tabled(t, scheduledAutomaton(), "SCHEDULE-RECORD")

	for name, want := range map[string]string{
		// The singular and the plural, which is why the two arms are scheduled
		// for one occurrence and for two: `occurrences 1` in a column somebody
		// is scanning reads as a generator that did not consider the case.
		"ENTRIES.OPENING":    "in occurrence 1 of the table",
		"ENTRIES.ADJUSTMENT": "in occurrences 2 and 3 of the table",

		// The variant itself is present whenever the occurrence is, and the
		// items inside an arm are present whenever the arm is. Only the arms
		// carry a selector.
		"ENTRIES.*variant*":            "always",
		"ENTRIES.OPENING.OPENING-CODE": "always",
	} {
		row, ok := rows[name]
		if !ok {
			t.Fatalf("the table for SCHEDULE-RECORD carries no row for %s", name)
		}

		if row.present != want {
			t.Errorf("%s is present %q, want %q", name, row.present, want)
		}
	}

	// And every arm still begins at the variant's first byte: how an arm is
	// chosen says nothing about where it sits.
	for _, name := range []string{"ENTRIES.*variant*", "ENTRIES.OPENING", "ENTRIES.ADJUSTMENT"} {
		if got := rows[name].at; got != "2" {
			t.Errorf("%s begins at %q, and every arm of the variant begins at byte 2", name, got)
		}
	}
}

// TestAScheduleReadsDifferentlyFromAPredicate is the rest of that criterion:
// the two kinds of selector are told apart by reading the cell, not by knowing
// which descriptor it came from.
//
// Held over one descriptor carrying both, because the failure this guards
// against is a phrase that is merely *accurate* about a schedule while reading
// like a test of bytes — `when occurrence = 1` would pass an assertion that the
// cell is non-empty and would send a reader looking in the occurrence for a
// field called `occurrence`.
func TestAScheduleReadsDifferentlyFromAPredicate(t *testing.T) {
	t.Parallel()

	scheduled := tabled(t, scheduledAutomaton(), "SCHEDULE-RECORD")
	bytes := tabled(t, scheduledAutomaton(), "BYTES-RECORD")

	for name, want := range map[string]string{
		"ITEMS.CASH":       "when ITEMS.ITEM-KIND = 0xC1",
		"ITEMS.CHEQUE-REF": "when ITEMS.ITEM-KIND = 0xC3",
	} {
		row, ok := bytes[name]
		if !ok {
			t.Fatalf("the table for BYTES-RECORD carries no row for %s", name)
		}

		if row.present != want {
			t.Errorf("%s is present %q, want %q", name, row.present, want)
		}
	}

	for _, name := range []string{"ENTRIES.OPENING", "ENTRIES.ADJUSTMENT"} {
		if strings.HasPrefix(scheduled[name].present, "when ") {
			t.Errorf("%s is present %q, which is how an arm selected by bytes reads", name, scheduled[name].present)
		}
	}
}

// TestAScheduledArmIsNotReadAsAPredicateReference is the second criterion, at
// the place it would fail.
//
// A scheduled arm sets no predicate reference, and `GetPredicateId` on one
// answers zero. Zero is an ordinary identifier in this schema and not a
// sentinel, so a consumer reading the field rather than the choice dereferences
// node zero — which in a descriptor this project writes exists and is the File
// node, because assemble reserves the root before any record. The file node is
// deliberately node zero here for that reason: a generator with this defect
// refuses the descriptor naming node 0, and one without it draws the table.
func TestAScheduledArmIsNotReadAsAPredicateReference(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			unframedFile(0, 2),
			stateNode(2, true, 10),
			edgeNode(10, 100, 2, nil, nil, nil),

			recordOf(100, 105, "ROOT-AT-ZERO"),
			groupNode(105, "ROOT-AT-ZERO", 101, 110),
			fieldNode(101, "REC-TYPE", 1),

			repeatingGroupNode(110, "ENTRIES", constantCount(2), 111),
			variantNode(111,
				armScheduledAt([]uint32{1}, 112),
				armScheduledAt([]uint32{2}, 113)),
			fieldNode(112, "FIRST-ENTRY", 3),
			fieldNode(113, "SECOND-ENTRY", 3),
		},
	}

	rows := tabled(t, d, "ROOT-AT-ZERO")

	for name, want := range map[string]string{
		"ENTRIES.FIRST-ENTRY":  "in occurrence 1 of the table",
		"ENTRIES.SECOND-ENTRY": "in occurrence 2 of the table",
	} {
		if got := rows[name].present; got != want {
			t.Errorf("%s is present %q, want %q", name, got, want)
		}
	}
}

// TestAVariantMixingSelectorsIsRefused is the other half of the second
// criterion: a descriptor `resolve` refuses to emit is reported here rather
// than drawn half-way.
//
// Re-checked in this generator for the reason the two-arm minimum is — it
// refuses a malformed descriptor whatever produced it — and because the
// alternative is a single column carrying two different questions, half of its
// arms naming the bytes that select them and half naming the occurrence that
// does, over a table that cannot be both.
func TestAVariantMixingSelectorsIsRefused(t *testing.T) {
	t.Parallel()

	_, err := read(oneRecordAutomaton(
		edgeNode(30, 100, 2, nil, nil, nil),
		groupNode(105, "HEADER-RECORD", 101, 400),
		variantNode(400, armScheduledAt([]uint32{1}, 401), armAt(60, 402)),
		fieldNode(401, "BY-POSITION", 2),
		fieldNode(402, "BY-BYTES", 2),
		equalPredicate(60, 101, "\xc1"),
	), defaults())

	if err == nil {
		t.Fatal("read accepted a variant whose arms are chosen two different ways")
	}

	if !strings.Contains(err.Error(), "is selected by a predicate and its first arm by its position in the table") {
		t.Errorf("the refusal reads %q, and does not name the two kinds of selector", err)
	}
}

// TestAnArmCarryingNoSelectorIsRefused is the third shape of the same
// criterion.
//
// An arm of a variant carrying neither member of the choice is the descriptor
// the field-reading consumer cannot tell from a scheduled one — both answer
// zero for the predicate reference — so it gets a refusal of its own that says
// what is missing rather than one naming node zero.
func TestAnArmCarryingNoSelectorIsRefused(t *testing.T) {
	t.Parallel()

	_, err := read(oneRecordAutomaton(
		edgeNode(30, 100, 2, nil, nil, nil),
		groupNode(105, "HEADER-RECORD", 101, 400),
		variantNode(400,
			&irpb.Arm{Body: &irpb.Arm_FieldId{FieldId: 401}},
			&irpb.Arm{Body: &irpb.Arm_FieldId{FieldId: 402}}),
		fieldNode(401, "ONE-ARM", 2),
		fieldNode(402, "OTHER-ARM", 2),
	), defaults())

	if err == nil {
		t.Fatal("read accepted an arm carrying no selector at all")
	}

	if !strings.Contains(err.Error(), "says nothing about what selects it") {
		t.Errorf("the refusal reads %q, and does not say that the arm names no selector", err)
	}
}

// TestAScheduleThatSelectsNothingIsRefused covers the two things a producer
// owes a schedule's numbers, both of which this generator draws and therefore
// checks.
//
// An empty schedule is an arm nothing selects rather than an arm selected by
// nothing, and it would draw as a cell with a hole in the middle of a sentence.
// A schedule out of ascending order is what says inside one arm what coverage
// says across two, and drawing one would present a producer bug as a layout
// somebody has to make sense of.
func TestAScheduleThatSelectsNothingIsRefused(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		arm  *irpb.Arm
		says string
	}{
		"no occurrence at all": {
			arm:  armScheduledAt(nil, 401),
			says: "is scheduled for no occurrence at all",
		},
		"out of ascending order": {
			arm:  armScheduledAt([]uint32{3, 1}, 401),
			says: "is scheduled for occurrence 1 after occurrence 3",
		},
		"the same occurrence twice": {
			arm:  armScheduledAt([]uint32{2, 2}, 401),
			says: "is scheduled for occurrence 2 after occurrence 2",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := read(oneRecordAutomaton(
				edgeNode(30, 100, 2, nil, nil, nil),
				groupNode(105, "HEADER-RECORD", 101, 400),
				variantNode(400, test.arm, armScheduledAt([]uint32{9}, 402)),
				fieldNode(401, "ONE-ARM", 2),
				fieldNode(402, "OTHER-ARM", 2),
			), defaults())

			if err == nil {
				t.Fatal("read accepted a schedule that selects nothing")
			}

			if !strings.Contains(err.Error(), test.says) {
				t.Errorf("the refusal reads %q, and does not say %q", err, test.says)
			}
		})
	}
}
