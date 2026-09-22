// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// An arm may be selected by the position of the occurrence it is being chosen
// for rather than by that occurrence's bytes, and every check that needs the
// copybook lands here: the number of occurrences a table can hold is the
// copybook's, and the coverage of a schedule is checked against it.
//
// The one that matters most is exhaustiveness. A schedule is read statically, so
// a scheduled variant cannot produce the "occurrence no arm matched" failure at
// read time at all — a schedule with a hole in it is caught here or never.

// scheduledSource is discussion #340's shape: a count, a table of three entries
// whose body is described three ways, and a byte behind the body that says
// nothing about which way.
//
// `OCCURS 1 TO 3 TIMES DEPENDING ON` rather than a fixed table, because the
// declared maximum is the same three under both readings and that is what makes
// the mechanism available under both. A test that wanted the fixed table would
// be testing the reading and not the schedule.
const scheduledSource = `01 R.
   05 ADR-COUNT PIC 9(2).
   05 ADR-ENTRY OCCURS 1 TO 3 TIMES DEPENDING ON ADR-COUNT.
      10 ADR-HOME.
         15 ADR-H-LINE PIC X(8).
      10 ADR-WORK REDEFINES ADR-HOME.
         15 ADR-W-COMPANY PIC X(4).
         15 ADR-W-LINE PIC X(4).
      10 ADR-MAIL REDEFINES ADR-HOME.
         15 ADR-M-LINE PIC X(6).
      10 ADR-TAG PIC X(2).
   05 ADR-TRAILER PIC X(5).
`

// resolveScheduled resolves [scheduledSource] under one reading with the
// redefines the caller builds over its own copybook field.
func resolveScheduled(
	t *testing.T,
	reading layoutmodel.Reading,
	build func(*copybook.Field) []Redefine,
) ([]*Record, error) {
	t.Helper()

	field := recordOf(t, scheduledSource)

	return Resolve(field, Options{
		Copybook:  "test.cpy",
		Dialect:   copybook.IBMEnterprise(),
		Encoding:  mainframe(),
		Reading:   reading,
		Redefines: build(field),
	})
}

// schedule is the selector of an arm chosen by position.
func schedule(occurrences ...int64) *Schedule {
	return &Schedule{Occurrences: occurrences}
}

// oneEntryEach is the layout discussion #340 describes: entry one is the home
// address, entry two the work address, entry three the mailing address.
func oneEntryEach(t *testing.T) func(*copybook.Field) []Redefine {
	return func(field *copybook.Field) []Redefine {
		return []Redefine{{
			Item: fieldNamed(t, field, "ADR-HOME"),
			Alternatives: []Alternative{
				{Name: "ADR-HOME", Schedule: schedule(1)},
				{Name: "ADR-WORK", Schedule: schedule(2)},
				{Name: "ADR-MAIL", Schedule: schedule(3)},
			},
		}}
	}
}

// TestAScheduledVariantResolvesUnderBothReadings is the resolution, and the
// statement #346 settled: the mechanism is not available under one reading of an
// OCCURS DEPENDING ON and refused under the other.
//
// The two readings really are two here, and the repetition below says so — a
// count read out of the record under `odoslide` and the declared maximum as a
// constant under `noodoslide`. What does not move between them is the number the
// schedule is checked against, which is what makes one answer right for both.
func TestAScheduledVariantResolvesUnderBothReadings(t *testing.T) {
	t.Parallel()

	for _, reading := range []layoutmodel.Reading{layoutmodel.ODOSlide, layoutmodel.NoODOSlide} {
		t.Run(string(reading), func(t *testing.T) {
			t.Parallel()

			records, err := resolveScheduled(t, reading, oneEntryEach(t))
			if err != nil {
				t.Fatalf("resolving: %v", err)
			}
			if len(records) != 1 {
				t.Fatalf("resolved to %d records, want 1: a variant is not three record types", len(records))
			}

			record := records[0]

			table := nodeNamed(record.Root, "ADR-ENTRY")
			if table == nil {
				t.Fatal("the record holds no ADR-ENTRY group")
			}
			if got := table.Repetition.Reference(); got != reading.Slides() {
				t.Errorf("under %s the table's count is a reference: %v, want %v", reading, got, reading.Slides())
			}

			variant := variantIn(record.Root)
			if variant == nil {
				t.Fatal("the record holds no variant")
			}

			want := []struct {
				alternative string
				occurrences []int64
			}{
				{"ADR-HOME", []int64{1}},
				{"ADR-WORK", []int64{2}},
				{"ADR-MAIL", []int64{3}},
			}

			if len(variant.Arms) != len(want) {
				t.Fatalf("the variant has %d arms, want %d", len(variant.Arms), len(want))
			}

			for at, arm := range variant.Arms {
				if arm.Alternative != want[at].alternative {
					t.Errorf("arm %d is %s, want %s", at+1, arm.Alternative, want[at].alternative)
				}
				if arm.Schedule == nil {
					t.Fatalf("the arm %s carries no schedule", arm.Alternative)
				}
				if !slices.Equal(arm.Schedule.Occurrences, want[at].occurrences) {
					t.Errorf("the arm %s is scheduled for %v, want %v",
						arm.Alternative, arm.Schedule.Occurrences, want[at].occurrences)
				}
				if arm.Predicate != nil {
					t.Errorf("the arm %s carries a predicate as well as a schedule", arm.Alternative)
				}
			}
		})
	}
}

// TestAScheduledArmAgreesOnOneExtentAndCarriesItsOwnSlack is the rule that keeps
// every occurrence of the enclosing group the same width, and nothing about
// selecting an arm by position relaxes it.
//
// ADR-MAIL is the short alternative: six bytes of the eight the copybook gave
// the run, so the two it does not describe are a slack node inside its arm
// rather than bytes the item behind it slides over.
func TestAScheduledArmAgreesOnOneExtentAndCarriesItsOwnSlack(t *testing.T) {
	t.Parallel()

	records, err := resolveScheduled(t, layoutmodel.ODOSlide, oneEntryEach(t))
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	variant := variantIn(records[0].Root)

	for _, arm := range variant.Arms {
		if got := arm.Body.Extent(); got != 8 {
			t.Errorf("the arm %s covers %d bytes, want the variant's 8", arm.Alternative, got)
		}
	}

	if got := slackWidths(variant.Arms[0].Body); len(got) != 0 {
		t.Errorf("the arm that fills the extent carries slack of %v", got)
	}
	if got := slackWidths(variant.Arms[2].Body); len(got) != 1 || got[0] != 2 {
		t.Errorf("the short arm's slack is %v, want one run of 2", got)
	}
}

// TestAVariantMixingSelectorsIsRejected is docs/ir/SPEC.md's "One kind of
// selector per variant".
//
// Exhaustiveness and overlap are different questions for the two kinds, so a
// mixed variant would need both checks and a third rule saying which wins where
// a predicate matches an occurrence a schedule also covers. The diagnostic names
// the arms of each kind, because either half could be the one the adopter meant.
func TestAVariantMixingSelectorsIsRejected(t *testing.T) {
	t.Parallel()

	_, err := resolveScheduled(t, layoutmodel.ODOSlide, func(field *copybook.Field) []Redefine {
		return []Redefine{{
			Item: fieldNamed(t, field, "ADR-HOME"),
			Alternatives: []Alternative{
				{Name: "ADR-HOME", Schedule: schedule(1)},
				{Name: "ADR-WORK", Schedule: schedule(2)},
				{Name: "ADR-MAIL", Predicate: selects("M", "ADR-ENTRY", "ADR-TAG")},
			},
		}}
	})

	var mixed *MixedSelectorError
	if !errors.As(err, &mixed) {
		t.Fatalf("no MixedSelectorError in: %v", err)
	}

	if !slices.Equal(mixed.Scheduled, []string{"ADR-HOME", "ADR-WORK"}) {
		t.Errorf("the arms chosen by position are %v, want ADR-HOME and ADR-WORK", mixed.Scheduled)
	}
	if !slices.Equal(mixed.Tested, []string{"ADR-MAIL"}) {
		t.Errorf("the arms chosen by bytes are %v, want ADR-MAIL", mixed.Tested)
	}

	namesAll(t, mixed.Diagnostic().Message, "R", "ADR-ENTRY", "ADR-HOME")
}

// TestAnOccurrenceNoArmIsScheduledForIsRejected is the check the whole form
// waits on the copybook for, and the one that has to be made here or never.
func TestAnOccurrenceNoArmIsScheduledForIsRejected(t *testing.T) {
	t.Parallel()

	_, err := resolveScheduled(t, layoutmodel.ODOSlide, func(field *copybook.Field) []Redefine {
		return []Redefine{{
			Item: fieldNamed(t, field, "ADR-HOME"),
			Alternatives: []Alternative{
				{Name: "ADR-HOME", Schedule: schedule(1)},
				{Name: "ADR-WORK", Schedule: schedule(2)},
			},
		}}
	})

	var uncovered *ScheduleCoverageError
	if !errors.As(err, &uncovered) {
		t.Fatalf("no ScheduleCoverageError in: %v", err)
	}

	if !slices.Equal(uncovered.Occurrences, []int64{3}) {
		t.Errorf("the fault names occurrences %v, want just the third", uncovered.Occurrences)
	}
	if uncovered.Maximum != 3 {
		t.Errorf("the fault says the table holds %d entries, want 3", uncovered.Maximum)
	}

	namesAll(t, uncovered.Diagnostic().Message, "R", "ADR-ENTRY", "ADR-HOME")
}

// TestAnOccurrenceScheduledTwiceIsRejected is docs/ir/SPEC.md's "When two match,
// and when none does" at this scope, decided from the layout rather than from a
// file.
//
// Two arms carrying one number and one arm carrying it twice are the same fault
// and different messages: an occurrence scheduled by two alternatives sends an
// adopter to the two descriptions they wrote for one entry, and one written
// twice inside a single arm sends them to a list they meant to be ascending.
func TestAnOccurrenceScheduledTwiceIsRejected(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		schedule func(*copybook.Field) []Redefine
		arms     []string
		says     string
	}{
		{
			name: "by two arms",
			schedule: func(field *copybook.Field) []Redefine {
				return []Redefine{{
					Item: fieldNamed(t, field, "ADR-HOME"),
					Alternatives: []Alternative{
						{Name: "ADR-HOME", Schedule: schedule(1, 2)},
						{Name: "ADR-WORK", Schedule: schedule(2)},
						{Name: "ADR-MAIL", Schedule: schedule(3)},
					},
				}}
			},
			arms: []string{"ADR-HOME", "ADR-WORK"},
			says: "ADR-HOME and ADR-WORK",
		},
		{
			name: "twice within one arm",
			schedule: func(field *copybook.Field) []Redefine {
				return []Redefine{{
					Item: fieldNamed(t, field, "ADR-HOME"),
					Alternatives: []Alternative{
						{Name: "ADR-HOME", Schedule: schedule(1, 1)},
						{Name: "ADR-WORK", Schedule: schedule(2)},
						{Name: "ADR-MAIL", Schedule: schedule(3)},
					},
				}}
			},
			arms: []string{"ADR-HOME"},
			says: "twice",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := resolveScheduled(t, layoutmodel.ODOSlide, test.schedule)

			var twice *ScheduleOverlapError
			if !errors.As(err, &twice) {
				t.Fatalf("no ScheduleOverlapError in: %v", err)
			}

			if !slices.Equal(twice.Arms, test.arms) {
				t.Errorf("the fault names the arms %v, want %v", twice.Arms, test.arms)
			}

			message := twice.Diagnostic().Message
			if !strings.Contains(message, test.says) {
				t.Errorf("the message does not say %q: %s", test.says, message)
			}

			namesAll(t, message, "R", "ADR-ENTRY", "ADR-HOME")
		})
	}
}

// TestAnArmScheduledForNothingIsRejected is the arm nothing selects, which is
// not the same thing as an arm selected by nothing.
//
// It would let a variant satisfy "two arms at least" while only one of them is
// ever taken. Where every occurrence really is one alternative's, the layout
// says so with a single alternative and no variant is emitted at all.
func TestAnArmScheduledForNothingIsRejected(t *testing.T) {
	t.Parallel()

	_, err := resolveScheduled(t, layoutmodel.ODOSlide, func(field *copybook.Field) []Redefine {
		return []Redefine{{
			Item: fieldNamed(t, field, "ADR-HOME"),
			Alternatives: []Alternative{
				{Name: "ADR-HOME", Schedule: schedule(1, 2, 3)},
				{Name: "ADR-WORK", Schedule: schedule()},
			},
		}}
	})

	var empty *EmptyScheduleError
	if !errors.As(err, &empty) {
		t.Fatalf("no EmptyScheduleError in: %v", err)
	}

	if empty.Arm != "ADR-WORK" {
		t.Errorf("the fault is on the arm %s, want ADR-WORK", empty.Arm)
	}

	namesAll(t, empty.Diagnostic().Message, "R", "ADR-ENTRY", "ADR-HOME")
}

// TestAnOccurrenceOutsideTheTableIsRejected is the number that names no entry:
// a schedule written for a table of four against a copybook that declares three.
//
// The message quotes the number the adopter wrote and the maximum the copybook
// declares, because which of the two files moved is the question they have to
// answer.
func TestAnOccurrenceOutsideTheTableIsRejected(t *testing.T) {
	t.Parallel()

	for _, occurrence := range []int64{0, 4} {
		t.Run(strings.ReplaceAll(occurrenceNumbers([]int64{occurrence})[0], "-", "minus "), func(t *testing.T) {
			t.Parallel()

			_, err := resolveScheduled(t, layoutmodel.ODOSlide, func(field *copybook.Field) []Redefine {
				return []Redefine{{
					Item: fieldNamed(t, field, "ADR-HOME"),
					Alternatives: []Alternative{
						{Name: "ADR-HOME", Schedule: schedule(1, 2, 3)},
						{Name: "ADR-WORK", Schedule: schedule(occurrence)},
					},
				}}
			})

			var outside *ScheduleRangeError
			if !errors.As(err, &outside) {
				t.Fatalf("no ScheduleRangeError in: %v", err)
			}

			if outside.Occurrence != occurrence {
				t.Errorf("the fault names occurrence %d, want %d", outside.Occurrence, occurrence)
			}
			if outside.Maximum != 3 {
				t.Errorf("the fault says the table holds %d entries, want 3", outside.Maximum)
			}

			namesAll(t, outside.Diagnostic().Message, "R", "ADR-ENTRY", "ADR-HOME")
		})
	}
}

// TestASingleScheduledAlternativeEmitsNoVariant is the floor two arms still are.
//
// A schedule covering every occurrence with one alternative is that
// alternative's items and no variant, which is the same collapse the
// byte-selected form already makes: nothing is being chosen, so there is no arm
// for a schedule to select and no alternation to carry it under.
func TestASingleScheduledAlternativeEmitsNoVariant(t *testing.T) {
	t.Parallel()

	records, err := resolveScheduled(t, layoutmodel.ODOSlide, func(field *copybook.Field) []Redefine {
		return []Redefine{{
			Item: fieldNamed(t, field, "ADR-HOME"),
			Alternatives: []Alternative{
				{Name: "ADR-MAIL", Schedule: schedule(1, 2, 3)},
			},
		}}
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	record := records[0]

	if variantIn(record.Root) != nil {
		t.Error("the record holds a variant, and nothing is being chosen")
	}
	if nodeNamed(record.Root, "ADR-M-LINE") == nil {
		t.Error("the alternative's items are not in the record")
	}
	if nodeNamed(record.Root, "ADR-H-LINE") != nil {
		t.Error("an alternative the layout did not name is in the record")
	}

	// The bytes the alternative does not describe are still an occurrence's, so
	// everything behind the table sits where the copybook puts it.
	if got := positionOf(t, record, "ADR-TAG"); got != 10 {
		t.Errorf("the item behind the redefine is at %d, want 10", got)
	}
}

// namesAll asserts that a message names the record, the repeating group and the
// variant, which is what docs/ir/SPEC.md asks of every diagnostic here: a
// message saying only that a schedule does not cover a table sends an adopter
// looking through two files for it.
func namesAll(t *testing.T, message string, want ...string) {
	t.Helper()

	for _, name := range want {
		if !strings.Contains(message, name) {
			t.Errorf("the message does not name %s: %s", name, message)
		}
	}
}

// nodeNamed is the first node under root carrying the copybook name, or nil
// where none does.
func nodeNamed(root *Node, name string) *Node {
	if root == nil {
		return nil
	}

	if root.Field != nil && !root.Field.Filler && root.Field.Name == name {
		return root
	}

	for _, member := range root.Members {
		if found := nodeNamed(member, name); found != nil {
			return found
		}
	}

	for _, arm := range root.Arms {
		if found := nodeNamed(arm.Body, name); found != nil {
			return found
		}
	}

	return nil
}
