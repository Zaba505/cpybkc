// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/layoutdoc"
)

// scheduled wraps a schedule in the least a layout has to say for the layer to
// be readable: the `record` its reference is rooted at, and the discriminator
// that record carries.
//
// The discriminator is there because "exactly one per record" is this reader's
// rule too, and a record without one would report a fault that has nothing to do
// with the schedule under test.
func scheduled(forms ...string) string {
	return oneRecord("ADDR", append([]string{"(discriminate ADDR single-record-type)"}, forms...)...)
}

// TestReadScheduleModelsTheLayer is the criterion this reader exists for: a
// variant settled by the position of an occurrence becomes a typed value naming
// each alternative and the entries it is taken for, with a position on every
// part of it.
func TestReadScheduleModelsTheLayer(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   []string
	}{
		{
			// SPEC.md's own shape: a table whose entries carry roles, one
			// occurrence apiece.
			name: "one occurrence per alternative",
			source: scheduled(strings.Join([]string{
				"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
				"  (arm ADR-HOME 1)",
				"  (arm ADR-WORK 2)",
				"  (arm ADR-MAIL 3))",
			}, "\n")),
			want: []string{
				"layout.sexpr:2:1 discriminate ADDR",
				"  layout.sexpr:2:20 single-record-type, which lowers into no predicate",
				"layout.sexpr:3:1 schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
				"  layout.sexpr:4:3 arm ADR-HOME: layout.sexpr:4:17 1",
				"  layout.sexpr:5:3 arm ADR-WORK: layout.sexpr:5:17 2",
				"  layout.sexpr:6:3 arm ADR-MAIL: layout.sexpr:6:17 3",
			},
		},
		{
			// An arm takes one or more, because a schedule taking one
			// alternative for several entries is the shape that has nowhere
			// else to go.
			name: "an alternative taken for several occurrences",
			source: scheduled(strings.Join([]string{
				"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
				"  (arm ADR-HOME 1 2 3)",
				"  (arm ADR-WORK 4 5))",
			}, "\n")),
			want: []string{
				"layout.sexpr:2:1 discriminate ADDR",
				"  layout.sexpr:2:20 single-record-type, which lowers into no predicate",
				"layout.sexpr:3:1 schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
				"  layout.sexpr:4:3 arm ADR-HOME: layout.sexpr:4:17 1 layout.sexpr:4:19 2 layout.sexpr:4:21 3",
				"  layout.sexpr:5:3 arm ADR-WORK: layout.sexpr:5:17 4 layout.sexpr:5:19 5",
			},
		},
		{
			// A schedule and a discriminator are two forms, not two spellings,
			// and two variants of one record may be settled one way each.
			name: "a schedule beside a discriminator on another variant",
			source: scheduled(
				strings.Join([]string{
					"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
					"  (arm ADR-HOME 1)",
					"  (arm ADR-WORK 2))",
				}, "\n"),
				strings.Join([]string{
					"(discriminate-variant (item ADDR ADR-ENTRY ADR-BODY-UK)",
					"  (arm ADR-BODY-UK (equals (item ADDR ADR-ENTRY ADR-COUNTRY) \"UK\"))",
					"  (arm ADR-BODY-US (equals (item ADDR ADR-ENTRY ADR-COUNTRY) \"US\")))",
				}, "\n"),
			),
			want: []string{
				"layout.sexpr:2:1 discriminate ADDR",
				"  layout.sexpr:2:20 single-record-type, which lowers into no predicate",
				"layout.sexpr:6:1 discriminate-variant (item ADDR ADR-ENTRY ADR-BODY-UK)",
				"  layout.sexpr:7:3 arm ADR-BODY-UK: layout.sexpr:7:20 equals (item ADDR ADR-ENTRY ADR-COUNTRY) \"UK\"",
				"  layout.sexpr:8:3 arm ADR-BODY-US: layout.sexpr:8:20 equals (item ADDR ADR-ENTRY ADR-COUNTRY) \"US\"",
				"layout.sexpr:3:1 schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
				"  layout.sexpr:4:3 arm ADR-HOME: layout.sexpr:4:17 1",
				"  layout.sexpr:5:3 arm ADR-WORK: layout.sexpr:5:17 2",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			read, err := discriminationOf(t, testCase.source)
			if err != nil {
				t.Fatalf("the reader rejected the layout: %v", err)
			}

			if got, want := renderDiscrimination(read), strings.Join(testCase.want, "\n"); got != want {
				t.Errorf("read as\n%s\nwant\n%s", got, want)
			}
		})
	}
}

// TestReadScheduleRejects is the other half of the reader: the checks
// docs/layout/SPEC.md assigns the layout, each reported where it is and in words
// that name the variant and the arm.
func TestReadScheduleRejects(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name: "a schedule on a record nobody defined",
			source: scheduled(strings.Join([]string{
				"(schedule-variant (item POLICY PL-ENTRIES PL-BODY)",
				"  (arm PL-BODY-MOTOR 1)",
				"  (arm PL-BODY-FLEET 2))",
			}, "\n")),
			want: []string{
				"layout.sexpr:3:19: form \"schedule-variant\" names record \"POLICY\", and the layout defines " +
					"no record of that name",
			},
		},
		{
			name: "a schedule on an item no repetition can reach",
			source: scheduled(strings.Join([]string{
				"(schedule-variant (item ADDR ADR-HOME)",
				"  (arm ADR-HOME 1)",
				"  (arm ADR-WORK 2))",
			}, "\n")),
			want: []string{
				"layout.sexpr:3:19: (item ADDR ADR-HOME) names an item directly under record \"ADDR\"'s " +
					"top-level item, and a redefine inside a repeating group sits deeper than that; a redefine " +
					"whose alternatives are whole record types is told apart by discriminate",
			},
		},
		{
			name:   "a schedule naming nothing at all",
			source: scheduled("(schedule-variant)"),
			want: []string{
				"layout.sexpr:3:1: a variant schedule is written (schedule-variant <item-ref> " +
					"(arm <name> <occurrence> ...) ...), and this is a variant schedule naming no item at all",
			},
		},
		{
			name: "a schedule with one arm",
			source: scheduled(strings.Join([]string{
				"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
				"  (arm ADR-HOME 1 2 3))",
			}, "\n")),
			want: []string{
				"layout.sexpr:3:1: the variant at (item ADDR ADR-ENTRY ADR-HOME) is scheduled with 1 arms, " +
					"and a variant carries at least two; a redefine every occurrence of which takes one " +
					"alternative is not a variant, and is written (take-alternative <item-ref> <name>)",
			},
		},
		{
			name: "an arm scheduled for nothing",
			source: scheduled(strings.Join([]string{
				"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
				"  (arm ADR-HOME 1)",
				"  (arm ADR-WORK))",
			}, "\n")),
			want: []string{
				"layout.sexpr:5:3: arm \"ADR-WORK\" of the variant at (item ADDR ADR-ENTRY ADR-HOME) is " +
					"scheduled for no occurrence, and an arm scheduled for nothing is an arm nothing selects",
			},
		},
		{
			name: "an occurrence counted from zero",
			source: scheduled(strings.Join([]string{
				"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
				"  (arm ADR-HOME 0)",
				"  (arm ADR-WORK 1))",
			}, "\n")),
			want: []string{
				"layout.sexpr:4:17: arm \"ADR-HOME\" of the variant at (item ADDR ADR-ENTRY ADR-HOME) is " +
					"scheduled for occurrence 0, and an occurrence is counted from one",
			},
		},
		{
			name: "an occurrence that is not a number at all",
			source: scheduled(strings.Join([]string{
				"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
				"  (arm ADR-HOME FIRST)",
				"  (arm ADR-WORK 2))",
			}, "\n")),
			want: []string{
				"layout.sexpr:4:17: a scheduled arm is written (arm <name> <occurrence> ...), and this has " +
					"the symbol \"FIRST\"",
			},
		},
		{
			name: "an occurrence with a fraction",
			source: scheduled(strings.Join([]string{
				"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
				"  (arm ADR-HOME 1.5)",
				"  (arm ADR-WORK 2))",
			}, "\n")),
			want: []string{
				"layout.sexpr:4:17: a scheduled arm is written (arm <name> <occurrence> ...), and this has " +
					"a number with a fraction",
			},
		},
		{
			name: "an arm carrying a predicate",
			source: scheduled(strings.Join([]string{
				"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
				"  (arm ADR-HOME (equals (item ADDR ADR-ENTRY ADR-KIND) \"H\"))",
				"  (arm ADR-WORK 2))",
			}, "\n")),
			want: []string{
				"layout.sexpr:4:17: a scheduled arm is written (arm <name> <occurrence> ...), and this has " +
					"form \"equals\"",
			},
		},
		{
			name: "two arms naming one alternative",
			source: scheduled(strings.Join([]string{
				"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
				"  (arm ADR-HOME 1)",
				"  (arm ADR-HOME 2))",
			}, "\n")),
			want: []string{
				"layout.sexpr:5:3: the variant at (item ADDR ADR-ENTRY ADR-HOME) names alternative " +
					"\"ADR-HOME\" twice, and names it first at layout.sexpr:4:3",
			},
		},
		{
			name: "one occurrence scheduled by two arms",
			source: scheduled(strings.Join([]string{
				"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
				"  (arm ADR-HOME 1 2)",
				"  (arm ADR-WORK 2 3))",
			}, "\n")),
			want: []string{
				"layout.sexpr:5:17: occurrence 2 of the variant at (item ADDR ADR-ENTRY ADR-HOME) is " +
					"scheduled by arms \"ADR-HOME\" and \"ADR-WORK\", and is scheduled first at " +
					"layout.sexpr:4:19; an occurrence takes exactly one alternative",
			},
		},
		{
			name: "one occurrence written twice inside one arm",
			source: scheduled(strings.Join([]string{
				"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
				"  (arm ADR-HOME 1 1)",
				"  (arm ADR-WORK 2))",
			}, "\n")),
			want: []string{
				"layout.sexpr:4:19: arm \"ADR-HOME\" of the variant at (item ADDR ADR-ENTRY ADR-HOME) is " +
					"scheduled for occurrence 1 twice, and is scheduled for it first at layout.sexpr:4:17; " +
					"an occurrence takes exactly one alternative",
			},
		},
		{
			name: "an arm's occurrences written out of order",
			source: scheduled(strings.Join([]string{
				"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
				"  (arm ADR-HOME 1 3 2)",
				"  (arm ADR-WORK 4))",
			}, "\n")),
			want: []string{
				"layout.sexpr:4:21: arm \"ADR-HOME\" of the variant at (item ADDR ADR-ENTRY ADR-HOME) " +
					"schedules occurrence 2 after occurrence 3 at layout.sexpr:4:19, and an arm's " +
					"occurrences are written in ascending order",
			},
		},
		{
			name: "arms written out of order",
			source: scheduled(strings.Join([]string{
				"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
				"  (arm ADR-HOME 3)",
				"  (arm ADR-WORK 1))",
			}, "\n")),
			want: []string{
				"layout.sexpr:5:17: arm \"ADR-WORK\" of the variant at (item ADDR ADR-ENTRY ADR-HOME) is " +
					"scheduled from occurrence 1 and follows arm \"ADR-HOME\", which is scheduled from " +
					"occurrence 3 at layout.sexpr:4:17; a schedule's arms are written in ascending order of " +
					"their first occurrence",
			},
		},
		{
			name: "a variant scheduled twice",
			source: scheduled(
				strings.Join([]string{
					"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
					"  (arm ADR-HOME 1)",
					"  (arm ADR-WORK 2))",
				}, "\n"),
				strings.Join([]string{
					"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
					"  (arm ADR-HOME 1)",
					"  (arm ADR-MAIL 2))",
				}, "\n"),
			),
			want: []string{
				"layout.sexpr:6:1: (item ADDR ADR-ENTRY ADR-HOME) is scheduled twice, and is scheduled " +
					"first at layout.sexpr:3:1; a variant carries exactly one schedule",
			},
		},
		{
			// The larger disagreement: one form says the alternative is chosen
			// by bytes and the other by position, and the order they were
			// written in would decide which.
			name: "a variant scheduled and discriminated",
			source: scheduled(
				strings.Join([]string{
					"(discriminate-variant (item ADDR ADR-ENTRY ADR-HOME)",
					"  (arm ADR-HOME (equals (item ADDR ADR-ENTRY ADR-KIND) \"H\"))",
					"  (arm ADR-WORK (equals (item ADDR ADR-ENTRY ADR-KIND) \"W\")))",
				}, "\n"),
				strings.Join([]string{
					"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
					"  (arm ADR-HOME 1)",
					"  (arm ADR-WORK 2))",
				}, "\n"),
			),
			want: []string{
				"layout.sexpr:6:1: (item ADDR ADR-ENTRY ADR-HOME) is named by \"schedule-variant\" and is " +
					"already named by \"discriminate-variant\" at layout.sexpr:3:1; exactly one form names a " +
					"redefine inside a repeating group",
			},
		},
		{
			name: "a variant scheduled and taken",
			source: scheduled(
				strings.Join([]string{
					"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
					"  (arm ADR-HOME 1)",
					"  (arm ADR-WORK 2))",
				}, "\n"),
				"(take-alternative (item ADDR ADR-ENTRY ADR-HOME) ADR-WORK)",
			),
			want: []string{
				"layout.sexpr:6:1: (item ADDR ADR-ENTRY ADR-HOME) is named by \"take-alternative\" and is " +
					"already named by \"schedule-variant\" at layout.sexpr:3:1; exactly one form names a " +
					"redefine inside a repeating group",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			read, err := discriminationOf(t, testCase.source)
			if err == nil {
				t.Fatalf("read as %s, want a fault", renderDiscrimination(read))
			}

			if read != nil {
				t.Errorf("a rejected layout yielded a discrimination layer: %s", renderDiscrimination(read))
			}

			if got, want := err.Error(), strings.Join(testCase.want, "\n"); got != want {
				t.Errorf("reported\n%s\nwant\n%s", got, want)
			}
		})
	}
}

// TestScheduleFaultsAreAssertable is the other requirement on a fault: a caller
// deciding what to do about one reaches for the type rather than for the text of
// the message.
func TestScheduleFaultsAreAssertable(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		assert func(*testing.T, error)
	}{
		{
			name: "a duplicate occurrence names both arms and the entry they share",
			source: scheduled(strings.Join([]string{
				"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
				"  (arm ADR-HOME 1 2)",
				"  (arm ADR-WORK 2))",
			}, "\n")),
			assert: func(t *testing.T, err error) {
				var fault *DuplicateOccurrenceError
				if !errors.As(err, &fault) {
					t.Fatalf("no DuplicateOccurrenceError in %v", err)
				}

				if fault.Arms != [2]string{"ADR-HOME", "ADR-WORK"} || fault.Occurrence != 2 {
					t.Errorf("the fault is on %v over occurrence %d, want both arms over 2", fault.Arms, fault.Occurrence)
				}
			},
		},
		{
			// The count is the arms the layout wrote. A second schedule whose
			// arm was refused for a reason of its own is that arm's fault and
			// not a shortage of arms (#342).
			name: "an arm count counts the arms the layout wrote",
			source: scheduled(
				strings.Join([]string{
					"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
					"  (arm ADR-HOME 1))",
				}, "\n"),
				strings.Join([]string{
					"(schedule-variant (item ADDR ADR-ENTRY ADR-BODY)",
					"  (arm ADR-BODY-ONE 1)",
					"  (arm ADR-BODY-TWO FIRST))",
				}, "\n"),
			),
			assert: func(t *testing.T, err error) {
				var fault *ScheduleArmCountError
				if !errors.As(err, &fault) {
					t.Fatalf("no ScheduleArmCountError in %v", err)
				}

				if fault.Count != 1 || fault.Variant.Name() != "ADR-HOME" {
					t.Errorf("the fault is on %s carrying %d arms, want (item ADDR ADR-ENTRY ADR-HOME) carrying 1",
						fault.Variant, fault.Count)
				}

				if got := strings.Count(err.Error(), " arms, and a variant carries at least two"); got != 1 {
					t.Errorf("%d variants are reported as carrying too few arms, want 1:\n%v", got, err)
				}
			},
		},
		{
			name: "an occurrence counted from zero names the variant and the arm",
			source: scheduled(strings.Join([]string{
				"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
				"  (arm ADR-HOME 0)",
				"  (arm ADR-WORK 1))",
			}, "\n")),
			assert: func(t *testing.T, err error) {
				var fault *OccurrenceValueError
				if !errors.As(err, &fault) {
					t.Fatalf("no OccurrenceValueError in %v", err)
				}

				if fault.Alternative != "ADR-HOME" || fault.Variant.Name() != "ADR-HOME" || fault.Occurrence != 0 {
					t.Errorf("the fault is on arm %q of %s over %d", fault.Alternative, fault.Variant, fault.Occurrence)
				}
			},
		},
		{
			name: "arms out of order name both and where each starts",
			source: scheduled(strings.Join([]string{
				"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
				"  (arm ADR-HOME 3)",
				"  (arm ADR-WORK 1))",
			}, "\n")),
			assert: func(t *testing.T, err error) {
				var fault *ScheduleOrderError
				if !errors.As(err, &fault) {
					t.Fatalf("no ScheduleOrderError in %v", err)
				}

				if fault.Arms != [2]string{"ADR-HOME", "ADR-WORK"} || fault.Occurrences != [2]int64{3, 1} {
					t.Errorf("the fault is on %v starting at %v, want ADR-HOME at 3 before ADR-WORK at 1",
						fault.Arms, fault.Occurrences)
				}
			},
		},
		{
			name: "a variant scheduled twice names the first schedule",
			source: scheduled(
				strings.Join([]string{
					"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
					"  (arm ADR-HOME 1)",
					"  (arm ADR-WORK 2))",
				}, "\n"),
				strings.Join([]string{
					"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
					"  (arm ADR-HOME 1)",
					"  (arm ADR-MAIL 2))",
				}, "\n"),
			),
			assert: func(t *testing.T, err error) {
				var fault *DuplicateScheduleError
				if !errors.As(err, &fault) {
					t.Fatalf("no DuplicateScheduleError in %v", err)
				}

				if fault.Variant.Name() != "ADR-HOME" {
					t.Errorf("the fault is on %s, want (item ADDR ADR-ENTRY ADR-HOME)", fault.Variant)
				}
			},
		},
		{
			// A schedule beside a discriminator is the pair that disagrees
			// about how the alternative is chosen, and it is the same fault two
			// forms of any two tags are.
			name: "a variant scheduled and discriminated names both tags",
			source: scheduled(
				strings.Join([]string{
					"(discriminate-variant (item ADDR ADR-ENTRY ADR-HOME)",
					"  (arm ADR-HOME (equals (item ADDR ADR-ENTRY ADR-KIND) \"H\"))",
					"  (arm ADR-WORK (equals (item ADDR ADR-ENTRY ADR-KIND) \"W\")))",
				}, "\n"),
				strings.Join([]string{
					"(schedule-variant (item ADDR ADR-ENTRY ADR-HOME)",
					"  (arm ADR-HOME 1)",
					"  (arm ADR-WORK 2))",
				}, "\n"),
			),
			assert: func(t *testing.T, err error) {
				var fault *RedefineNamedTwiceError
				if !errors.As(err, &fault) {
					t.Fatalf("no RedefineNamedTwiceError in %v", err)
				}

				if fault.Tag != "schedule-variant" || fault.FirstTag != "discriminate-variant" {
					t.Errorf("the fault is on %q after %q, want schedule-variant after discriminate-variant",
						fault.Tag, fault.FirstTag)
				}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := discriminationOf(t, testCase.source)
			if err == nil {
				t.Fatal("read without a fault, want one")
			}

			testCase.assert(t, err)
		})
	}
}

// TestTheSpecsScheduleExampleSchedules is the staleness gate over this form.
//
// The example under "A schedule for a redefine chosen by position" is the only
// place the document shows an adopter what a schedule looks like, and it is read
// out of the document rather than copied here for
// [TestTheSpecsVariantExampleDiscriminates]'s reason: a spelling the example
// writes and this reader does not read would otherwise be invisible until
// somebody pasted the example into a file.
func TestTheSpecsScheduleExampleSchedules(t *testing.T) {
	t.Parallel()

	const heading = "### A schedule for a redefine chosen by position"

	blocks, err := layoutdoc.Blocks(heading)
	if err != nil {
		t.Fatalf("read the layout SPEC: %v", err)
	}

	if len(blocks) < 2 {
		t.Fatalf("the %q subsection carries %d fenced blocks, want the skeleton and a layout", heading, len(blocks))
	}

	read, err := discriminationOf(t, scheduled(blocks[len(blocks)-1]))
	if err != nil {
		t.Fatalf("the reader rejects SPEC.md's own schedule example: %v", err)
	}

	if len(read.Schedules) != 1 {
		t.Fatalf("the example states %d schedules, want 1", len(read.Schedules))
	}

	want := map[string][]int64{"ADR-HOME": {1}, "ADR-WORK": {2}, "ADR-MAIL": {3}}

	schedule := read.Schedules[0]
	if len(schedule.Arms) != len(want) {
		t.Fatalf("the schedule carries %d arms, want %d", len(schedule.Arms), len(want))
	}

	for _, arm := range schedule.Arms {
		got := make([]int64, 0, len(arm.Occurrences))
		for _, occurrence := range arm.Occurrences {
			got = append(got, occurrence.Value)
		}

		if !slices.Equal(got, want[arm.Alternative]) {
			t.Errorf("arm %s is scheduled for %v, want %v", arm.Alternative, got, want[arm.Alternative])
		}
	}
}
