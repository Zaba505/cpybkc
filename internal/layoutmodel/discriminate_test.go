// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/layout"
)

// discriminationOf is the whole pipeline a caller runs: parse the source, then
// read the discrimination layer out of it.
func discriminationOf(t *testing.T, source string) (*Discrimination, error) {
	t.Helper()

	file, err := layout.Parse("layout.sexpr", strings.NewReader(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	return ReadDiscrimination(file)
}

// renderDiscrimination draws the layer the way the tests assert one: every
// discriminator in source order, the record or variant it is about, and the
// strategy under it with a position on each part.
//
// It is a rendering rather than a struct literal for [renderFraming]'s reason:
// what has to be right is the model *and* the position of every part of it, and
// a rendering carrying both fails with the whole model in the message.
func renderDiscrimination(d *Discrimination) string {
	var lines []string

	for _, record := range d.Records {
		lines = append(lines,
			fmt.Sprintf("%s discriminate %s", record.Pos, record.Record),
			"  "+renderStrategy(record.Strategy),
		)
	}

	for _, variant := range d.Variants {
		lines = append(lines, fmt.Sprintf("%s discriminate-variant %s", variant.Pos, variant.Variant))

		for _, arm := range variant.Arms {
			lines = append(lines, fmt.Sprintf("  %s arm %s: %s", arm.Pos, arm.Alternative, renderStrategy(arm.Predicate)))
		}
	}

	return strings.Join(lines, "\n")
}

// renderStrategy draws one strategy: where it was written, which member of the
// closed set it is, and — where it lowers into a predicate — what it tests
// against what.
func renderStrategy(s Strategy) string {
	if !s.Predicate() {
		return fmt.Sprintf("%s %s, which lowers into no predicate", s.Pos, s.Kind)
	}

	literals := make([]string, 0, len(s.Literals))
	for _, literal := range s.Literals {
		literals = append(literals, literal.String())
	}

	return fmt.Sprintf("%s %s %s %s", s.Pos, s.Kind, s.Item, strings.Join(literals, " "))
}

// oneRecord wraps a discrimination layer in the least a layout has to say for
// the layer to be readable: the `record` form the discriminator names.
//
// The record definitions layer is somebody else's (#27, #30) and this reader
// takes nothing from a `record` form but its name — which it needs, because
// "one discriminator per record" counts one form against another.
func oneRecord(name string, forms ...string) string {
	return strings.Join(append([]string{
		fmt.Sprintf("(record %s (copybook \"cpy/x.cpy\" %s-REC))", name, name),
	}, forms...), "\n")
}

// TestReadDiscriminationModelsTheLayer is the criterion this reader exists for:
// a layout's discrimination layer becomes a typed value naming what tells each
// record apart, out of a closed set, with a position on every part of it.
func TestReadDiscriminationModelsTheLayer(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name:   "a type code at a fixed offset",
			source: oneRecord("DETAIL", "(discriminate DETAIL (equals (item DETAIL D-REC-TYPE) \"20\"))"),
			want: []string{
				"layout.sexpr:2:1 discriminate DETAIL",
				"  layout.sexpr:2:22 equals (item DETAIL D-REC-TYPE) \"20\"",
			},
		},
		{
			// The shared header is a group inside each record type rather than
			// something standing outside them, so a type code in one is an
			// ordinary item reference and not a second strategy.
			name:   "a type code in a shared header copybook",
			source: oneRecord("DETAIL", "(discriminate DETAIL (equals (item DETAIL STD-HDR HDR-TYPE) \"20\"))"),
			want: []string{
				"layout.sexpr:2:1 discriminate DETAIL",
				"  layout.sexpr:2:22 equals (item DETAIL STD-HDR HDR-TYPE) \"20\"",
			},
		},
		{
			name:   "several type codes for one record",
			source: oneRecord("DETAIL", "(discriminate DETAIL (one-of (item DETAIL D-REC-TYPE) \"20\" \"21\"))"),
			want: []string{
				"layout.sexpr:2:1 discriminate DETAIL",
				"  layout.sexpr:2:22 one-of (item DETAIL D-REC-TYPE) \"20\" \"21\"",
			},
		},
		{
			name:   "a record carrying nothing to test",
			source: oneRecord("DETAIL", "(discriminate DETAIL single-record-type)"),
			want: []string{
				"layout.sexpr:2:1 discriminate DETAIL",
				"  layout.sexpr:2:22 single-record-type, which lowers into no predicate",
			},
		},
		{
			name:   "all three literal spellings",
			source: oneRecord("DETAIL", "(discriminate DETAIL (one-of (item DETAIL D-FLAG) \"Y\" 12 (bytes \"F0F1\")))"),
			want: []string{
				"layout.sexpr:2:1 discriminate DETAIL",
				"  layout.sexpr:2:22 one-of (item DETAIL D-FLAG) \"Y\" 12 (bytes \"F0F1\")",
			},
		},
		{
			name: "a redefine inside a repeating group",
			source: oneRecord("POLICY", strings.Join([]string{
				"(discriminate POLICY single-record-type)",
				"(discriminate-variant (item POLICY PL-ENTRIES PL-BODY-MOTOR)",
				"  (arm PL-BODY-MOTOR    (equals (item POLICY PL-ENTRIES PL-KIND) \"M\"))",
				"  (arm PL-BODY-PROPERTY (one-of (item POLICY PL-ENTRIES PL-KIND) \"P\" \"H\")))",
			}, "\n")),
			want: []string{
				"layout.sexpr:2:1 discriminate POLICY",
				"  layout.sexpr:2:22 single-record-type, which lowers into no predicate",
				"layout.sexpr:3:1 discriminate-variant (item POLICY PL-ENTRIES PL-BODY-MOTOR)",
				"  layout.sexpr:4:3 arm PL-BODY-MOTOR: layout.sexpr:4:25 equals (item POLICY PL-ENTRIES PL-KIND) \"M\"",
				"  layout.sexpr:5:3 arm PL-BODY-PROPERTY: layout.sexpr:5:25 one-of (item POLICY PL-ENTRIES PL-KIND) \"P\" \"H\"",
			},
		},
		{
			// An arm's target may sit behind the variant and inside the group
			// that repeats, which is what a record's discriminator may not do.
			// The rules are mirrors rather than exceptions: an arm is evaluated
			// in the occurrence being walked, so it carries one.
			name: "an arm's target may sit where a record's may not",
			source: oneRecord("POLICY", strings.Join([]string{
				"(discriminate POLICY single-record-type)",
				"(discriminate-variant (item POLICY PL-ENTRIES PL-BODY-MOTOR)",
				"  (arm PL-BODY-MOTOR    (equals (item POLICY PL-ENTRIES PL-TRAILER PL-KIND) \"M\"))",
				"  (arm PL-BODY-PROPERTY (equals (item POLICY PL-ENTRIES PL-TRAILER PL-KIND) \"P\")))",
			}, "\n")),
			want: []string{
				"layout.sexpr:2:1 discriminate POLICY",
				"  layout.sexpr:2:22 single-record-type, which lowers into no predicate",
				"layout.sexpr:3:1 discriminate-variant (item POLICY PL-ENTRIES PL-BODY-MOTOR)",
				"  layout.sexpr:4:3 arm PL-BODY-MOTOR: layout.sexpr:4:25 equals " +
					"(item POLICY PL-ENTRIES PL-TRAILER PL-KIND) \"M\"",
				"  layout.sexpr:5:3 arm PL-BODY-PROPERTY: layout.sexpr:5:25 equals " +
					"(item POLICY PL-ENTRIES PL-TRAILER PL-KIND) \"P\"",
			},
		},
		{
			// Nothing orders a layout's forms, so a discriminator written above
			// the record it names is the same statement as one written below it.
			name: "forms in any order",
			source: strings.Join([]string{
				"(discriminate DETAIL (equals (item DETAIL D-REC-TYPE) \"20\"))",
				"(record DETAIL (copybook \"cpy/x.cpy\" DETAIL-REC))",
			}, "\n"),
			want: []string{
				"layout.sexpr:1:1 discriminate DETAIL",
				"  layout.sexpr:1:22 equals (item DETAIL D-REC-TYPE) \"20\"",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			read, err := discriminationOf(t, testCase.source)
			if err != nil {
				t.Fatalf("read: %v", err)
			}

			if got, want := renderDiscrimination(read), strings.Join(testCase.want, "\n"); got != want {
				t.Errorf("read as\n%s\nwant\n%s", got, want)
			}
		})
	}
}

// TestEveryStrategyIsWritable walks the whole closed set, so that a strategy
// this reader cannot read is a failure here rather than a layout an adopter
// writes from the document and cannot use.
//
// It is the set docs/layout/SPEC.md closes, and the set the IR's predicate
// membership was settled against (#28): two strategies lower into a predicate
// and one lowers into the absence of one.
func TestEveryStrategyIsWritable(t *testing.T) {
	t.Parallel()

	written := map[StrategyKind]string{
		Equals:           "(equals (item DETAIL D-REC-TYPE) \"20\")",
		OneOf:            "(one-of (item DETAIL D-REC-TYPE) \"20\" \"21\")",
		SingleRecordType: "single-record-type",
	}

	for _, kind := range strategies {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			source, ok := written[kind]
			if !ok {
				t.Fatalf("the set carries %s and this test does not write one", kind)
			}

			read, err := discriminationOf(t, oneRecord("DETAIL", "(discriminate DETAIL "+source+")"))
			if err != nil {
				t.Fatalf("read: %v", err)
			}

			if got := read.Records[0].Strategy.Kind; got != kind {
				t.Fatalf("%s read as %s", source, got)
			}
		})
	}
}

// TestSingleRecordTypeLowersIntoNoPredicate is docs/ir/SPEC.md's "A transition
// may carry no predicate", read from the layout side.
//
// Selecting on nothing at all is the *absence* of a predicate and not a member
// of the predicate set testing nothing, so the question a caller compiling a
// discriminator asks is about the strategy rather than about whether an item
// reference happens to be set.
func TestSingleRecordTypeLowersIntoNoPredicate(t *testing.T) {
	t.Parallel()

	testCases := map[string]bool{
		"(equals (item DETAIL D-REC-TYPE) \"20\")":        true,
		"(one-of (item DETAIL D-REC-TYPE) \"20\" \"21\")": true,
		"single-record-type":                              false,
	}

	for source, want := range testCases {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			read, err := discriminationOf(t, oneRecord("DETAIL", "(discriminate DETAIL "+source+")"))
			if err != nil {
				t.Fatalf("read: %v", err)
			}

			strategy := read.Records[0].Strategy
			if got := strategy.Predicate(); got != want {
				t.Errorf("%s lowers into a predicate: %t, want %t", source, got, want)
			}

			if !want && (strategy.Item.Record != "" || len(strategy.Literals) != 0) {
				t.Errorf("%s carries %s and %v, want neither", source, strategy.Item, strategy.Literals)
			}
		})
	}
}

// TestReadDiscriminationRejects is the load-bearing half: a reader that accepts
// everything passes every test above.
//
// Each case is a layout the format does not admit, and the whole joined message
// is asserted rather than the first fault, because reporting every fault is the
// reader's other requirement.
func TestReadDiscriminationRejects(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name:   "a record nobody discriminated",
			source: oneRecord("DETAIL"),
			want: []string{
				"layout.sexpr:1:1: record \"DETAIL\" carries no discriminator; a record that carries nothing " +
					"to test says so, with \"single-record-type\"",
			},
		},
		{
			name: "a record discriminated twice",
			source: oneRecord("DETAIL",
				"(discriminate DETAIL (equals (item DETAIL D-REC-TYPE) \"20\"))",
				"(discriminate DETAIL (equals (item DETAIL D-REC-TYPE) \"21\"))",
			),
			want: []string{
				"layout.sexpr:3:1: record \"DETAIL\" is discriminated twice, and is discriminated first at " +
					"layout.sexpr:2:1; a record carries exactly one discriminator",
			},
		},
		{
			name:   "a discriminator on a record nobody defined",
			source: oneRecord("DETAIL", "(discriminate DETAIL single-record-type)", "(discriminate HEADER single-record-type)"),
			want: []string{
				"layout.sexpr:3:15: form \"discriminate\" names record \"HEADER\", and the layout defines no " +
					"record of that name",
			},
		},
		{
			name:   "a discriminator testing another record's item",
			source: oneRecord("DETAIL", "(discriminate DETAIL (equals (item HEADER H-REC-TYPE) \"20\"))"),
			want: []string{
				"layout.sexpr:2:30: the discriminator on record \"DETAIL\" tests (item HEADER H-REC-TYPE), " +
					"and a discriminator tests an item of the record it discriminates",
			},
		},
		{
			name:   "a strategy outside the closed set",
			source: oneRecord("DETAIL", "(discriminate DETAIL (record-length 80))"),
			want: []string{
				"layout.sexpr:2:23: a record is selected by equals, one-of or single-record-type, and this is " +
					"form \"record-length\"",
			},
		},
		{
			name:   "a strategy naming no literal",
			source: oneRecord("DETAIL", "(discriminate DETAIL (equals (item DETAIL D-REC-TYPE)))"),
			want: []string{
				"layout.sexpr:2:22: form \"equals\" takes one item reference and one literal, and this one has " +
					"an item and no literal",
			},
		},
		{
			name:   "two literals where equals takes one",
			source: oneRecord("DETAIL", "(discriminate DETAIL (equals (item DETAIL D-REC-TYPE) \"20\" \"21\"))"),
			want: []string{
				"layout.sexpr:2:60: form \"equals\" takes one item reference and one literal, and this one has several",
			},
		},
		{
			name:   "a literal that is none of the three spellings",
			source: oneRecord("DETAIL", "(discriminate DETAIL (equals (item DETAIL D-REC-TYPE) TWENTY))"),
			want: []string{
				"layout.sexpr:2:55: a literal is text, a number or (bytes \"<hex>\"), and this is the symbol \"TWENTY\"",
			},
		},
		{
			name:   "a discriminator that is not a discriminator",
			source: oneRecord("DETAIL", "(discriminate DETAIL)"),
			want: []string{
				"layout.sexpr:2:1: a discriminator is written (discriminate <record-name> <strategy>), and this has no value",
				"layout.sexpr:1:1: record \"DETAIL\" carries no discriminator; a record that carries nothing to " +
					"test says so, with \"single-record-type\"",
			},
		},
		{
			// A discriminator whose record is written as text is still a
			// discriminator, so the strategy under it is read and what is wrong
			// with the literal is reported in the same run.
			name:   "every fault is reported: the record, then the strategy under it",
			source: oneRecord("DETAIL", "(discriminate \"DETAIL\" (equals (item DETAIL D-REC-TYPE) TWENTY))"),
			want: []string{
				"layout.sexpr:2:15: a discriminator is written (discriminate <record-name> <strategy>), " +
					"and this has text",
				"layout.sexpr:2:57: a literal is text, a number or (bytes \"<hex>\"), and this is the symbol \"TWENTY\"",
				"layout.sexpr:1:1: record \"DETAIL\" carries no discriminator; a record that carries nothing to " +
					"test says so, with \"single-record-type\"",
			},
		},
		{
			name: "an arm selected by nothing at all",
			source: oneRecord("POLICY", "(discriminate POLICY single-record-type)", strings.Join([]string{
				"(discriminate-variant (item POLICY PL-ENTRIES PL-BODY-MOTOR)",
				"  (arm PL-BODY-MOTOR    (equals (item POLICY PL-ENTRIES PL-KIND) \"M\"))",
				"  (arm PL-BODY-PROPERTY single-record-type))",
			}, "\n")),
			want: []string{
				"layout.sexpr:5:25: an arm is selected by equals or one-of, and this is the symbol \"single-record-type\"",
				"layout.sexpr:3:1: the variant at (item POLICY PL-ENTRIES PL-BODY-MOTOR) carries 1 arms, and a " +
					"variant carries at least two; a redefine every occurrence of which takes one alternative is " +
					"not a variant",
			},
		},
		{
			name: "two arms naming one alternative",
			source: oneRecord("POLICY", "(discriminate POLICY single-record-type)", strings.Join([]string{
				"(discriminate-variant (item POLICY PL-ENTRIES PL-BODY-MOTOR)",
				"  (arm PL-BODY-MOTOR (equals (item POLICY PL-ENTRIES PL-KIND) \"M\"))",
				"  (arm PL-BODY-MOTOR (equals (item POLICY PL-ENTRIES PL-KIND) \"P\")))",
			}, "\n")),
			want: []string{
				"layout.sexpr:5:3: the variant at (item POLICY PL-ENTRIES PL-BODY-MOTOR) names alternative " +
					"\"PL-BODY-MOTOR\" twice, and names it first at layout.sexpr:4:3",
				"layout.sexpr:3:1: the variant at (item POLICY PL-ENTRIES PL-BODY-MOTOR) carries 1 arms, and a " +
					"variant carries at least two; a redefine every occurrence of which takes one alternative is " +
					"not a variant",
			},
		},
		{
			name: "two arms admitting one literal on one item",
			source: oneRecord("POLICY", "(discriminate POLICY single-record-type)", strings.Join([]string{
				"(discriminate-variant (item POLICY PL-ENTRIES PL-BODY-MOTOR)",
				"  (arm PL-BODY-MOTOR    (equals (item POLICY PL-ENTRIES PL-KIND) \"M\"))",
				"  (arm PL-BODY-PROPERTY (one-of (item POLICY PL-ENTRIES PL-KIND) \"P\" \"M\")))",
			}, "\n")),
			want: []string{
				"layout.sexpr:5:70: \"M\" on (item POLICY PL-ENTRIES PL-KIND) is admitted by arms " +
					"\"PL-BODY-MOTOR\" and \"PL-BODY-PROPERTY\" of the variant at " +
					"(item POLICY PL-ENTRIES PL-BODY-MOTOR), and is admitted first at layout.sexpr:4:66; " +
					"two arms that can both match one occurrence have nothing to tell them apart",
			},
		},
		{
			name: "an arm testing an item outside the occurrence",
			source: oneRecord("POLICY", "(discriminate POLICY single-record-type)", strings.Join([]string{
				"(discriminate-variant (item POLICY PL-ENTRIES PL-BODY-MOTOR)",
				"  (arm PL-BODY-MOTOR    (equals (item POLICY PL-HEADER PL-KIND) \"M\"))",
				"  (arm PL-BODY-PROPERTY (equals (item POLICY PL-ENTRIES PL-KIND) \"P\")))",
			}, "\n")),
			want: []string{
				"layout.sexpr:4:33: the arm on \"PL-BODY-MOTOR\" tests (item POLICY PL-HEADER PL-KIND), which is " +
					"outside \"PL-ENTRIES\", which is the outermost group the variant sits in; an arm's target sits " +
					"inside the occurrence it is chosen for",
				"layout.sexpr:3:1: the variant at (item POLICY PL-ENTRIES PL-BODY-MOTOR) carries 1 arms, and a " +
					"variant carries at least two; a redefine every occurrence of which takes one alternative is " +
					"not a variant",
			},
		},
		{
			name: "an arm testing an item inside the arm it selects",
			source: oneRecord("POLICY", "(discriminate POLICY single-record-type)", strings.Join([]string{
				"(discriminate-variant (item POLICY PL-ENTRIES PL-BODY-MOTOR)",
				"  (arm PL-BODY-MOTOR    (equals (item POLICY PL-ENTRIES PL-BODY-MOTOR PL-KIND) \"M\"))",
				"  (arm PL-BODY-PROPERTY (equals (item POLICY PL-ENTRIES PL-BODY-PROPERTY PL-KIND) \"P\")))",
			}, "\n")),
			want: []string{
				"layout.sexpr:4:33: the arm on \"PL-BODY-MOTOR\" tests " +
					"(item POLICY PL-ENTRIES PL-BODY-MOTOR PL-KIND), which is inside the variant itself; " +
					"an arm's target sits inside the occurrence it is chosen for",
				"layout.sexpr:5:33: the arm on \"PL-BODY-PROPERTY\" tests " +
					"(item POLICY PL-ENTRIES PL-BODY-PROPERTY PL-KIND), which is inside the arm it selects; " +
					"an arm's target sits inside the occurrence it is chosen for",
				"layout.sexpr:3:1: the variant at (item POLICY PL-ENTRIES PL-BODY-MOTOR) carries 0 arms, and a " +
					"variant carries at least two; a redefine every occurrence of which takes one alternative is " +
					"not a variant",
			},
		},
		{
			name: "a variant that cannot be inside a group that repeats",
			source: oneRecord("POLICY", "(discriminate POLICY single-record-type)", strings.Join([]string{
				"(discriminate-variant (item POLICY PL-BODY-MOTOR)",
				"  (arm PL-BODY-MOTOR    (equals (item POLICY PL-KIND) \"M\"))",
				"  (arm PL-BODY-PROPERTY (equals (item POLICY PL-KIND) \"P\")))",
			}, "\n")),
			want: []string{
				"layout.sexpr:3:23: (item POLICY PL-BODY-MOTOR) names an item directly under record \"POLICY\"'s " +
					"top-level item, and a variant sits inside a group that repeats; a redefine whose alternatives " +
					"are whole record types is told apart by discriminate",
			},
		},
		{
			name: "a variant discriminated twice",
			source: oneRecord("POLICY", "(discriminate POLICY single-record-type)", strings.Join([]string{
				"(discriminate-variant (item POLICY PL-ENTRIES PL-BODY-MOTOR)",
				"  (arm PL-BODY-MOTOR    (equals (item POLICY PL-ENTRIES PL-KIND) \"M\"))",
				"  (arm PL-BODY-PROPERTY (equals (item POLICY PL-ENTRIES PL-KIND) \"P\")))",
				"(discriminate-variant (item POLICY PL-ENTRIES PL-BODY-MOTOR)",
				"  (arm PL-BODY-MOTOR    (equals (item POLICY PL-ENTRIES PL-KIND) \"1\"))",
				"  (arm PL-BODY-PROPERTY (equals (item POLICY PL-ENTRIES PL-KIND) \"2\")))",
			}, "\n")),
			want: []string{
				"layout.sexpr:6:1: (item POLICY PL-ENTRIES PL-BODY-MOTOR) is discriminated twice, and is " +
					"discriminated first at layout.sexpr:3:1; a variant carries exactly one discriminator",
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

// TestDiscriminationFaultsAreAssertable is the other requirement on a fault: a
// caller deciding what to do about one reaches for the type rather than for the
// text of the message.
func TestDiscriminationFaultsAreAssertable(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		assert func(*testing.T, error)
	}{
		{
			name:   "a record with no discriminator names the record",
			source: oneRecord("DETAIL"),
			assert: func(t *testing.T, err error) {
				var fault *MissingDiscriminatorError
				if !errors.As(err, &fault) {
					t.Fatalf("no MissingDiscriminatorError in %v", err)
				}

				if fault.Record != "DETAIL" {
					t.Errorf("the fault is about %q, want \"DETAIL\"", fault.Record)
				}
			},
		},
		{
			name:   "a foreign target names both records",
			source: oneRecord("DETAIL", "(discriminate DETAIL (equals (item HEADER H-REC-TYPE) \"20\"))"),
			assert: func(t *testing.T, err error) {
				var fault *ForeignTargetError
				if !errors.As(err, &fault) {
					t.Fatalf("no ForeignTargetError in %v", err)
				}

				if fault.Record != "DETAIL" || fault.Item.Record != "HEADER" {
					t.Errorf("the fault is on %q testing %q, want \"DETAIL\" testing \"HEADER\"", fault.Record, fault.Item.Record)
				}
			},
		},
		{
			name:   "a strategy outside the set names the set it is outside",
			source: oneRecord("DETAIL", "(discriminate DETAIL (record-length 80))"),
			assert: func(t *testing.T, err error) {
				var fault *StrategyError
				if !errors.As(err, &fault) {
					t.Fatalf("no StrategyError in %v", err)
				}

				if fault.Subject != subjectRecord || len(fault.Admits) != len(strategies) {
					t.Errorf("the fault is on %q admitting %v, want %q admitting all three", fault.Subject, fault.Admits, subjectRecord)
				}
			},
		},
		{
			name: "an overlap names both arms and the literal they share",
			source: oneRecord("POLICY", "(discriminate POLICY single-record-type)", strings.Join([]string{
				"(discriminate-variant (item POLICY PL-ENTRIES PL-BODY-MOTOR)",
				"  (arm PL-BODY-MOTOR    (equals (item POLICY PL-ENTRIES PL-KIND) \"M\"))",
				"  (arm PL-BODY-PROPERTY (equals (item POLICY PL-ENTRIES PL-KIND) \"M\")))",
			}, "\n")),
			assert: func(t *testing.T, err error) {
				var fault *ArmOverlapError
				if !errors.As(err, &fault) {
					t.Fatalf("no ArmOverlapError in %v", err)
				}

				if fault.Arms != [2]string{"PL-BODY-MOTOR", "PL-BODY-PROPERTY"} || fault.Literal.Text != "M" {
					t.Errorf("the fault is on %v over %s, want both arms over \"M\"", fault.Arms, fault.Literal)
				}
			},
		},
		{
			name: "an arm's target names where it stands instead",
			source: oneRecord("POLICY", "(discriminate POLICY single-record-type)", strings.Join([]string{
				"(discriminate-variant (item POLICY PL-ENTRIES PL-BODY-MOTOR)",
				"  (arm PL-BODY-MOTOR    (equals (item OTHER PL-ENTRIES PL-KIND) \"M\"))",
				"  (arm PL-BODY-PROPERTY (equals (item POLICY PL-ENTRIES PL-KIND) \"P\")))",
			}, "\n")),
			assert: func(t *testing.T, err error) {
				var fault *ArmTargetError
				if !errors.As(err, &fault) {
					t.Fatalf("no ArmTargetError in %v", err)
				}

				if fault.Alternative != "PL-BODY-MOTOR" || !strings.Contains(fault.Found, "rooted at record") {
					t.Errorf("the fault is on %q, %s", fault.Alternative, fault.Found)
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

// TestTheSpecsWorkedExampleDiscriminates is the staleness gate over the layer.
//
// docs/layout/SPEC.md's "A layout, end to end" appendix is the layout the
// document shows an adopter, and it is read out of the document rather than
// copied here for [TestTheSpecsWorkedExampleFrames]'s reason: a strategy the
// example writes and this reader does not read would otherwise be invisible
// until somebody pasted the example into a file.
func TestTheSpecsWorkedExampleDiscriminates(t *testing.T) {
	t.Parallel()

	read, err := discriminationOf(t, specExample(t))
	if err != nil {
		t.Fatalf("the reader rejects SPEC.md's own worked example: %v", err)
	}

	want := map[string]StrategyKind{
		"FILE-HEADER":  Equals,
		"ORDER-HEADER": Equals,
		"ORDER-DETAIL": OneOf,
		"FILE-TRAILER": Equals,
	}

	if len(read.Records) != len(want) {
		t.Fatalf("the example discriminates %d records, want %d", len(read.Records), len(want))
	}

	for _, record := range read.Records {
		if got := record.Strategy.Kind; got != want[record.Record] {
			t.Errorf("%s is told apart by %s, want %s", record.Record, got, want[record.Record])
		}
	}
}

// TestTheSpecsVariantExampleDiscriminates is the same gate over the second scope
// a discriminator is written in.
//
// The example under "A discriminator for a redefine inside a table" is the only
// place the document shows an adopter what a variant discriminator looks like,
// and it names a record the snippet does not define — so the record is supplied
// here and nothing else is.
func TestTheSpecsVariantExampleDiscriminates(t *testing.T) {
	t.Parallel()

	example := specVariantExample(t)

	read, err := discriminationOf(t, oneRecord("POLICY", "(discriminate POLICY single-record-type)", example))
	if err != nil {
		t.Fatalf("the reader rejects SPEC.md's own variant example: %v", err)
	}

	if len(read.Variants) != 1 {
		t.Fatalf("the example states %d variants, want 1", len(read.Variants))
	}

	variant := read.Variants[0]
	if len(variant.Arms) != 2 {
		t.Fatalf("the variant carries %d arms, want 2", len(variant.Arms))
	}

	for _, arm := range variant.Arms {
		if !arm.Predicate.Predicate() {
			t.Errorf("arm %s is selected by %s, which lowers into no predicate", arm.Alternative, arm.Predicate.Kind)
		}
	}
}

// specVariantExample is the fenced example under SPEC.md's "A discriminator for
// a redefine inside a table", which is the last one in that subsection: the
// first is the form's skeleton, and a skeleton is placeholders rather than a
// layout.
func specVariantExample(t *testing.T) string {
	t.Helper()

	const heading = "### A discriminator for a redefine inside a table"

	spec, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "layout", "SPEC.md"))
	if err != nil {
		t.Fatalf("read the layout SPEC: %v", err)
	}

	_, subsection, found := strings.Cut(string(spec), heading+"\n")
	if !found {
		t.Fatalf("the layout SPEC has no %q subsection", heading)
	}

	if next := strings.Index(subsection, "\n### "); next >= 0 {
		subsection = subsection[:next]
	}

	blocks := strings.Split(subsection, "```")
	if len(blocks) < 5 {
		t.Fatalf("the %q subsection carries fewer than two fenced blocks", heading)
	}

	// The fences split the text into alternating prose and blocks, so the last
	// block is the second-to-last field.
	return blocks[len(blocks)-2]
}
