// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/layout"
)

// sequenceOf is the whole pipeline a caller runs: parse the source, then read
// the sequencing layer out of it.
func sequenceOf(t *testing.T, source string) (*Sequence, error) {
	t.Helper()

	file, err := layout.Parse("layout.sexpr", strings.NewReader(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	return ReadSequence(file)
}

// sequenced wraps an expression in the least a layout has to say for the layer
// to be readable: one `record` form per record it names.
//
// The records come first and one to a line, so that the `sequence` form always
// starts at line len(records)+1 and every position in the expression is the
// column it was written at.
func sequenced(expression string, records ...string) string {
	lines := make([]string, 0, len(records)+1)

	for _, record := range records {
		lines = append(lines, fmt.Sprintf("(record %s (copybook \"cpy/x.cpy\" %s-REC))", record, record))
	}

	return strings.Join(append(lines, expression), "\n")
}

// renderSequence draws the layer the way the tests assert one: the `sequence`
// form, then its expression as a tree with a position, a kind and whatever the
// kind carries on every node.
//
// It is a rendering rather than a struct literal for [renderDiscrimination]'s
// reason: what has to be right is the model *and* the position of every part of
// it, and a rendering carrying both fails with the whole model in the message.
func renderSequence(s *Sequence) string {
	return strings.Join(append([]string{fmt.Sprintf("%s sequence", s.Pos)}, renderExpression(s.Expression, 1)...), "\n")
}

// renderExpression draws one node and everything under it, indented by depth.
func renderExpression(e Expression, depth int) []string {
	head := fmt.Sprintf("%s%s %s", strings.Repeat("  ", depth), e.Pos, e.Kind)

	switch e.Kind {
	case RecordName:
		head += " " + e.Record
	case Times:
		head += " " + e.Item.String()
	case When:
		head += " " + e.Item.String() + " " + e.Match.String()
	}

	lines := []string{head}
	for _, sub := range e.Sub {
		lines = append(lines, renderExpression(sub, depth+1)...)
	}

	return lines
}

// withoutPositions is the same expression with every position dropped, which is
// what makes two readings of one term comparable.
//
// Everything else is kept, including which of the two spellings a `when`'s value
// was written in and which of the three spellings each literal was: those are
// the parts of the model a printer could silently normalise away, and they are
// the parts a round trip is worth asserting over.
func withoutPositions(e Expression) Expression {
	e.Pos = layout.Pos{}
	e.Item.Pos = layout.Pos{}
	e.Match.Pos = layout.Pos{}

	literals := make([]Literal, 0, len(e.Match.Literals))

	for _, literal := range e.Match.Literals {
		literal.Pos = layout.Pos{}
		literal.Bytes.Pos = layout.Pos{}
		literals = append(literals, literal)
	}

	e.Match.Literals = literals

	sub := make([]Expression, 0, len(e.Sub))
	for _, one := range e.Sub {
		sub = append(sub, withoutPositions(one))
	}

	e.Sub = sub

	return e
}

// TestReadSequenceModelsTheLayer is the criterion this reader exists for: a
// layout's sequencing expression becomes a typed term over the closed set of
// operators, with a position on every node of it.
func TestReadSequenceModelsTheLayer(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name:   "a file of one record type",
			source: sequenced("(sequence (* DETAIL))", "DETAIL"),
			want: []string{
				"layout.sexpr:2:1 sequence",
				"  layout.sexpr:2:11 *",
				"    layout.sexpr:2:14 record-name DETAIL",
			},
		},
		{
			name:   "a header, a body and a trailer",
			source: sequenced("(sequence (seq HEADER (+ DETAIL) TRAILER))", "HEADER", "DETAIL", "TRAILER"),
			want: []string{
				"layout.sexpr:4:1 sequence",
				"  layout.sexpr:4:11 seq",
				"    layout.sexpr:4:16 record-name HEADER",
				"    layout.sexpr:4:23 +",
				"      layout.sexpr:4:26 record-name DETAIL",
				"    layout.sexpr:4:34 record-name TRAILER",
			},
		},
		{
			// Grouping is the notation's own: a subexpression standing where a
			// record name may stand is a group, and nothing is written to say so.
			name:   "a repeated group of two record types",
			source: sequenced("(sequence (* (seq ORDER OPTION)))", "ORDER", "OPTION"),
			want: []string{
				"layout.sexpr:3:1 sequence",
				"  layout.sexpr:3:11 *",
				"    layout.sexpr:3:14 seq",
				"      layout.sexpr:3:19 record-name ORDER",
				"      layout.sexpr:3:25 record-name OPTION",
			},
		},
		{
			name:   "either of two record types at one point",
			source: sequenced("(sequence (alt DEBIT CREDIT))", "DEBIT", "CREDIT"),
			want: []string{
				"layout.sexpr:3:1 sequence",
				"  layout.sexpr:3:11 alt",
				"    layout.sexpr:3:16 record-name DEBIT",
				"    layout.sexpr:3:22 record-name CREDIT",
			},
		},
		{
			name:   "an optional trailer",
			source: sequenced("(sequence (seq DETAIL (? TRAILER)))", "DETAIL", "TRAILER"),
			want: []string{
				"layout.sexpr:3:1 sequence",
				"  layout.sexpr:3:11 seq",
				"    layout.sexpr:3:16 record-name DETAIL",
				"    layout.sexpr:3:23 ?",
				"      layout.sexpr:3:26 record-name TRAILER",
			},
		},
		{
			// The count is a field of a record the expression admits earlier,
			// which is the whole of what makes this expressible at all: the
			// register is read from the header and the details are counted
			// against it.
			name:   "as many details as the header counts",
			source: sequenced("(sequence (seq HEADER (times DETAIL (item HEADER H-COUNT))))", "HEADER", "DETAIL"),
			want: []string{
				"layout.sexpr:3:1 sequence",
				"  layout.sexpr:3:11 seq",
				"    layout.sexpr:3:16 record-name HEADER",
				"    layout.sexpr:3:23 times (item HEADER H-COUNT)",
				"      layout.sexpr:3:30 record-name DETAIL",
			},
		},
		{
			name:   "a trailer the header says whether to expect",
			source: sequenced("(sequence (seq HEADER (when (item HEADER H-FLAG) \"Y\" TRAILER)))", "HEADER", "TRAILER"),
			want: []string{
				"layout.sexpr:3:1 sequence",
				"  layout.sexpr:3:11 seq",
				"    layout.sexpr:3:16 record-name HEADER",
				"    layout.sexpr:3:23 when (item HEADER H-FLAG) \"Y\"",
				"      layout.sexpr:3:54 record-name TRAILER",
			},
		},
		{
			name:   "a trailer any of several flag values calls for",
			source: sequenced("(sequence (seq HEADER (when (item HEADER H-FLAG) (one-of \"Y\" \"T\") TRAILER)))", "HEADER", "TRAILER"),
			want: []string{
				"layout.sexpr:3:1 sequence",
				"  layout.sexpr:3:11 seq",
				"    layout.sexpr:3:16 record-name HEADER",
				"    layout.sexpr:3:23 when (item HEADER H-FLAG) (one-of \"Y\" \"T\")",
				"      layout.sexpr:3:67 record-name TRAILER",
			},
		},
		{
			// A conjunction of two conditions is a nested `when`, which SPEC.md
			// gives as the shape that already works in place of an operator
			// taking two.
			name: "two conditions on one subexpression",
			source: sequenced(
				"(sequence (seq HEADER (when (item HEADER H-FLAG) \"Y\" (when (item HEADER H-KIND) \"A\" TRAILER))))",
				"HEADER", "TRAILER",
			),
			want: []string{
				"layout.sexpr:3:1 sequence",
				"  layout.sexpr:3:11 seq",
				"    layout.sexpr:3:16 record-name HEADER",
				"    layout.sexpr:3:23 when (item HEADER H-FLAG) \"Y\"",
				"      layout.sexpr:3:54 when (item HEADER H-KIND) \"A\"",
				"        layout.sexpr:3:85 record-name TRAILER",
			},
		},
		{
			// A record may stand at more than one point in the expression.
			// Nothing in SPEC.md says otherwise, and a file whose trailer is the
			// same record type as its header is an ordinary one.
			name:   "one record type at two points",
			source: sequenced("(sequence (seq MARKER DETAIL MARKER))", "MARKER", "DETAIL"),
			want: []string{
				"layout.sexpr:3:1 sequence",
				"  layout.sexpr:3:11 seq",
				"    layout.sexpr:3:16 record-name MARKER",
				"    layout.sexpr:3:23 record-name DETAIL",
				"    layout.sexpr:3:30 record-name MARKER",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			read, err := sequenceOf(t, testCase.source)
			if err != nil {
				t.Fatalf("read the sequence: %v", err)
			}

			if got, want := renderSequence(read), strings.Join(testCase.want, "\n"); got != want {
				t.Errorf("the sequence reads as\n%s\nwant\n%s", got, want)
			}
		})
	}
}

// TestEveryOperatorIsWritable is the closed set from the other side: every
// member of it is something a layout can say, and the reader reads each as
// itself.
//
// A member nothing can write is a member of a set nobody agreed on, and the set
// is the one thing about this layer that #36 compiles against.
func TestEveryOperatorIsWritable(t *testing.T) {
	t.Parallel()

	written := map[ExpressionKind]string{
		RecordName: "(sequence DETAIL)",
		Seq:        "(sequence (seq DETAIL DETAIL))",
		Alt:        "(sequence (alt DETAIL DETAIL))",
		ZeroOrMore: "(sequence (* DETAIL))",
		OneOrMore:  "(sequence (+ DETAIL))",
		Optional:   "(sequence (? DETAIL))",
		Times:      "(sequence (times DETAIL (item DETAIL D-COUNT)))",
		When:       "(sequence (when (item DETAIL D-FLAG) \"Y\" DETAIL))",
	}

	for _, kind := range append([]ExpressionKind{RecordName}, operators...) {
		source, ok := written[kind]
		if !ok {
			t.Fatalf("%s is a member of the set with no layout in this test writing one", kind)
		}

		read, err := sequenceOf(t, sequenced(source, "DETAIL"))
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}

		if read.Expression.Kind != kind {
			t.Errorf("%s reads as %s", kind, read.Expression.Kind)
		}
	}
}

// TestSequenceRoundTrips is the printer's criterion: an expression printed and
// read back is the expression that was printed.
//
// Positions are dropped before the comparison and nothing else is — the layout's
// own line breaks are not part of the term, and the printer puts one on a line —
// so what this asserts is that no part of the model is lost or normalised on the
// way out and back.
func TestSequenceRoundTrips(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		expression string
		records    []string
	}{
		{
			name:       "a record name alone",
			expression: "(sequence DETAIL)",
			records:    []string{"DETAIL"},
		},
		{
			name:       "every operator at once",
			expression: "(sequence (seq HEADER (* (alt (+ DETAIL) (? OPTION))) TRAILER))",
			records:    []string{"HEADER", "DETAIL", "OPTION", "TRAILER"},
		},
		{
			name:       "a count read out of an earlier record",
			expression: "(sequence (seq HEADER (times DETAIL (item HEADER H-KEY H-COUNT))))",
			records:    []string{"HEADER", "DETAIL"},
		},
		{
			name:       "a presence test against one literal",
			expression: "(sequence (seq HEADER (when (item HEADER H-FLAG) \"Y\" TRAILER)))",
			records:    []string{"HEADER", "TRAILER"},
		},
		{
			name:       "a presence test against several literals",
			expression: "(sequence (seq HEADER (when (item HEADER H-FLAG) (one-of \"Y\" \"T\") TRAILER)))",
			records:    []string{"HEADER", "TRAILER"},
		},
		{
			// A single literal under `one-of` is not the same term as that
			// literal on its own: they lower into the two different guard tests,
			// so a printer that dropped the wrapper would change what the layout
			// says.
			name:       "one literal written as a set of one",
			expression: "(sequence (seq HEADER (when (item HEADER H-FLAG) (one-of \"Y\") TRAILER)))",
			records:    []string{"HEADER", "TRAILER"},
		},
		{
			// The three literal spellings resolve differently, and which one was
			// written is part of the term for the same reason.
			name:       "the three spellings a literal takes",
			expression: "(sequence (seq HEADER (when (item HEADER H-FLAG) (one-of \"Y\" 1 (bytes \"F0F1\")) TRAILER)))",
			records:    []string{"HEADER", "TRAILER"},
		},
		{
			name:       "a term the layout wrote across several lines",
			expression: "(sequence\n  (seq HEADER\n       (* DETAIL)\n       TRAILER))",
			records:    []string{"HEADER", "DETAIL", "TRAILER"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			first, err := sequenceOf(t, sequenced(testCase.expression, testCase.records...))
			if err != nil {
				t.Fatalf("read the sequence: %v", err)
			}

			printed := first.String()

			second, err := sequenceOf(t, sequenced(printed, testCase.records...))
			if err != nil {
				t.Fatalf("read what the printer wrote — %s: %v", printed, err)
			}

			if got := second.String(); got != printed {
				t.Errorf("printing twice gives\n%s\nand\n%s", printed, got)
			}

			want := withoutPositions(first.Expression)
			if got := withoutPositions(second.Expression); !reflect.DeepEqual(got, want) {
				t.Errorf("the expression read back is\n%+v\nwant\n%+v", got, want)
			}
		})
	}
}

// TestReadSequenceRejects is the load-bearing half: a reader that accepts
// everything passes every test above.
//
// Each case is a layout the format does not admit, and the whole joined message
// is asserted rather than the first fault, because reporting every fault is the
// reader's other requirement.
func TestReadSequenceRejects(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name:   "a layout that sequences nothing",
			source: sequenced("(record DETAIL (copybook \"cpy/x.cpy\" DETAIL-REC))"),
			want: []string{
				"layout.sexpr:1:1: a layout carries exactly one sequence form, and this one carries 0",
			},
		},
		{
			name:   "a layout that sequences twice",
			source: sequenced("(sequence (* DETAIL))\n(sequence (+ DETAIL))", "DETAIL"),
			want: []string{
				"layout.sexpr:3:1: a layout carries exactly one sequence form, and this one carries 2",
			},
		},
		{
			name:   "a sequence carrying no expression",
			source: sequenced("(sequence)", "DETAIL"),
			want: []string{
				"layout.sexpr:2:1: a sequence is written (sequence <expression>), one expression, and this one " +
					"has no value",
				"layout.sexpr:1:1: record \"DETAIL\" appears nowhere in the sequencing expression, and a record " +
					"type nothing can ever admit is one nothing will ever produce",
			},
		},
		{
			name:   "a sequence carrying two expressions",
			source: sequenced("(sequence HEADER DETAIL)", "HEADER", "DETAIL"),
			want: []string{
				"layout.sexpr:3:1: a sequence is written (sequence <expression>), one expression, and this one " +
					"has several",
			},
		},
		{
			name:   "a record the expression never names",
			source: sequenced("(sequence (* DETAIL))", "DETAIL", "TRAILER"),
			want: []string{
				"layout.sexpr:2:1: record \"TRAILER\" appears nowhere in the sequencing expression, and a record " +
					"type nothing can ever admit is one nothing will ever produce",
			},
		},
		{
			name:   "an expression naming a record nobody defined",
			source: sequenced("(sequence (seq DETAIL TRAILER))", "DETAIL"),
			want: []string{
				"layout.sexpr:2:23: form \"seq\" names record \"TRAILER\", and the layout defines no record of " +
					"that name",
			},
		},
		{
			name:   "an operator outside the closed set",
			source: sequenced("(sequence (repeat DETAIL 4))", "DETAIL"),
			want: []string{
				"layout.sexpr:2:12: a sequencing expression is a record name or one of seq, alt, *, +, ?, times " +
					"or when, and this is form \"repeat\"",
				"layout.sexpr:1:1: record \"DETAIL\" appears nowhere in the sequencing expression, and a record " +
					"type nothing can ever admit is one nothing will ever produce",
			},
		},
		{
			name:   "a literal where a term belongs",
			source: sequenced("(sequence (seq DETAIL \"TRAILER\"))", "DETAIL"),
			want: []string{
				"layout.sexpr:2:23: a sequencing expression is a record name or one of seq, alt, *, +, ?, times " +
					"or when, and this is text",
				"layout.sexpr:2:11: form \"seq\" takes two or more subexpressions, and this one has one",
			},
		},
		{
			name:   "a concatenation of one",
			source: sequenced("(sequence (seq DETAIL))", "DETAIL"),
			want: []string{
				"layout.sexpr:2:11: form \"seq\" takes two or more subexpressions, and this one has one",
			},
		},
		{
			name:   "an alternation of none",
			source: sequenced("(sequence (alt))", "DETAIL"),
			want: []string{
				"layout.sexpr:2:11: form \"alt\" takes two or more subexpressions, and this one has none",
				"layout.sexpr:1:1: record \"DETAIL\" appears nowhere in the sequencing expression, and a record " +
					"type nothing can ever admit is one nothing will ever produce",
			},
		},
		{
			name:   "a repetition of two subexpressions",
			source: sequenced("(sequence (* DETAIL TRAILER))", "DETAIL", "TRAILER"),
			want: []string{
				"layout.sexpr:3:11: form \"*\" takes one subexpression, and this one has 2",
			},
		},
		{
			name:   "a count with no item counting it",
			source: sequenced("(sequence (seq HEADER (times DETAIL)))", "HEADER", "DETAIL"),
			want: []string{
				"layout.sexpr:3:23: form \"times\" takes one subexpression and the item counting it, and this " +
					"one has a subexpression and no count",
				"layout.sexpr:3:11: form \"seq\" takes two or more subexpressions, and this one has one",
			},
		},
		{
			// The arity is one fault and what stands inside the operator is
			// another, and both are reported: a `times` carrying an item
			// reference alone is missing its subexpression, and the reference is
			// still held to the records the layout defines.
			name:   "a count and no subexpression",
			source: sequenced("(sequence (seq HEADER (times (item ORDER O-COUNT))))", "HEADER", "DETAIL"),
			want: []string{
				"layout.sexpr:3:23: form \"times\" takes one subexpression and the item counting it, and this " +
					"one has a count and no subexpression",
				"layout.sexpr:3:30: form \"times\" names record \"ORDER\", and the layout defines no record of " +
					"that name",
				"layout.sexpr:3:11: form \"seq\" takes two or more subexpressions, and this one has one",
				"layout.sexpr:2:1: record \"DETAIL\" appears nowhere in the sequencing expression, and a record " +
					"type nothing can ever admit is one nothing will ever produce",
			},
		},
		{
			// The same, at the operator taking three things rather than two: the
			// item is read in the position it stands in, so the record it names
			// is checked even though the `when` is short a subexpression.
			name:   "a presence test short a subexpression and rooted at no record",
			source: sequenced("(sequence (seq HEADER (when (item ORDER O-FLAG) \"Y\")))", "HEADER"),
			want: []string{
				"layout.sexpr:2:23: form \"when\" takes an item reference, a value and one subexpression, and " +
					"this one has an item and a value and no subexpression",
				"layout.sexpr:2:29: form \"when\" names record \"ORDER\", and the layout defines no record of " +
					"that name",
				"layout.sexpr:2:11: form \"seq\" takes two or more subexpressions, and this one has one",
			},
		},
		{
			name:   "a count read out of a record nobody defined",
			source: sequenced("(sequence (seq HEADER (times DETAIL (item ORDER O-COUNT))))", "HEADER", "DETAIL"),
			want: []string{
				"layout.sexpr:3:37: form \"times\" names record \"ORDER\", and the layout defines no record of " +
					"that name",
			},
		},
		{
			// What the copybook says the record holds is `resolve`'s, and a
			// reference naming a record and no item below it is not: there is no
			// copybook in which that spells a field.
			name:   "a count read out of no field at all",
			source: sequenced("(sequence (seq HEADER (times DETAIL (item HEADER))))", "HEADER", "DETAIL"),
			want: []string{
				"layout.sexpr:3:37: an item reference is written (item <record-name> <name> ...), and this is a " +
					"reference naming a record and no item below it",
				"layout.sexpr:3:11: form \"seq\" takes two or more subexpressions, and this one has one",
			},
		},
		{
			name:   "a presence test with no subexpression",
			source: sequenced("(sequence (seq HEADER (when (item HEADER H-FLAG) \"Y\")))", "HEADER", "TRAILER"),
			want: []string{
				"layout.sexpr:3:23: form \"when\" takes an item reference, a value and one subexpression, and " +
					"this one has an item and a value and no subexpression",
				"layout.sexpr:3:11: form \"seq\" takes two or more subexpressions, and this one has one",
				"layout.sexpr:2:1: record \"TRAILER\" appears nowhere in the sequencing expression, and a record " +
					"type nothing can ever admit is one nothing will ever produce",
			},
		},
		{
			name:   "a presence test against nothing",
			source: sequenced("(sequence (seq HEADER (when (item HEADER H-FLAG) (one-of) TRAILER)))", "HEADER", "TRAILER"),
			want: []string{
				"layout.sexpr:3:50: a when tests a value against a literal or (one-of <literal> ...), and this " +
					"has no literal",
				"layout.sexpr:3:11: form \"seq\" takes two or more subexpressions, and this one has one",
			},
		},
		{
			name:   "a presence test against a record name",
			source: sequenced("(sequence (seq HEADER (when (item HEADER H-FLAG) TRAILER TRAILER)))", "HEADER", "TRAILER"),
			want: []string{
				"layout.sexpr:3:50: a literal is text, a number or (bytes \"<hex>\"), and this is the symbol " +
					"\"TRAILER\"",
				"layout.sexpr:3:11: form \"seq\" takes two or more subexpressions, and this one has one",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			read, err := sequenceOf(t, testCase.source)
			if err == nil {
				t.Fatalf("the reader accepts %s", testCase.source)
			}

			if read != nil {
				t.Errorf("the reader hands back a sequence beside the fault: %+v", read)
			}

			if got, want := err.Error(), strings.Join(testCase.want, "\n"); got != want {
				t.Errorf("the reader reports\n%s\nwant\n%s", got, want)
			}
		})
	}
}

// TestSequenceFaultsAreAssertable is the other requirement on a fault: a caller
// deciding what to do about one reaches for the type rather than for the text of
// the message.
func TestSequenceFaultsAreAssertable(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		assert func(*testing.T, error)
	}{
		{
			name:   "a layout with no sequence counts none",
			source: sequenced("(record DETAIL (copybook \"cpy/x.cpy\" DETAIL-REC))"),
			assert: func(t *testing.T, err error) {
				var fault *SequenceCountError
				if !errors.As(err, &fault) {
					t.Fatalf("no SequenceCountError in %v", err)
				}

				if fault.Count != 0 {
					t.Errorf("the fault counts %d sequences, want 0", fault.Count)
				}
			},
		},
		{
			name:   "an unsequenced record names the record",
			source: sequenced("(sequence (* DETAIL))", "DETAIL", "TRAILER"),
			assert: func(t *testing.T, err error) {
				var fault *UnsequencedRecordError
				if !errors.As(err, &fault) {
					t.Fatalf("no UnsequencedRecordError in %v", err)
				}

				if fault.Record != "TRAILER" {
					t.Errorf("the fault is about %q, want \"TRAILER\"", fault.Record)
				}
			},
		},
		{
			name:   "an undefined record name names the operator it stands under",
			source: sequenced("(sequence (seq DETAIL TRAILER))", "DETAIL"),
			assert: func(t *testing.T, err error) {
				var fault *UnknownRecordError
				if !errors.As(err, &fault) {
					t.Fatalf("no UnknownRecordError in %v", err)
				}

				if fault.Record != "TRAILER" || fault.Form != tagSeq {
					t.Errorf("the fault is %q under %q, want \"TRAILER\" under \"seq\"", fault.Record, fault.Form)
				}
			},
		},
		{
			name:   "an operator outside the set names the set it is outside",
			source: sequenced("(sequence (repeat DETAIL 4))", "DETAIL"),
			assert: func(t *testing.T, err error) {
				var fault *ExpressionError
				if !errors.As(err, &fault) {
					t.Fatalf("no ExpressionError in %v", err)
				}

				if !reflect.DeepEqual(fault.Admits, operatorNames()) {
					t.Errorf("the fault admits %v, want %v", fault.Admits, operatorNames())
				}
			},
		},
		{
			name:   "a malformed operator names which one it is",
			source: sequenced("(sequence (* DETAIL TRAILER))", "DETAIL", "TRAILER"),
			assert: func(t *testing.T, err error) {
				var fault *ExpressionFormError
				if !errors.As(err, &fault) {
					t.Fatalf("no ExpressionFormError in %v", err)
				}

				if fault.Kind != ZeroOrMore {
					t.Errorf("the fault is about %s, want %s", fault.Kind, ZeroOrMore)
				}
			},
		},
		{
			name:   "a value set with nothing in it is a match fault",
			source: sequenced("(sequence (seq HEADER (when (item HEADER H-FLAG) (one-of) TRAILER)))", "HEADER", "TRAILER"),
			assert: func(t *testing.T, err error) {
				var fault *MatchFormError
				if !errors.As(err, &fault) {
					t.Fatalf("no MatchFormError in %v", err)
				}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := sequenceOf(t, testCase.source)
			if err == nil {
				t.Fatalf("the reader accepts %s", testCase.source)
			}

			testCase.assert(t, err)
		})
	}
}

// TestTheSpecsWorkedExampleSequences is the staleness gate over the layer.
//
// docs/layout/SPEC.md's "A layout, end to end" appendix is the layout the
// document shows an adopter, and it is read out of the document rather than
// copied here for [TestTheSpecsWorkedExampleDiscriminates]'s reason: an operator
// the example writes and this reader does not read would otherwise be invisible
// until somebody pasted the example into a file.
//
// It is also the one example in the document carrying both value-reading
// operators, which is what #76 put in scope and the reason this layer has them.
func TestTheSpecsWorkedExampleSequences(t *testing.T) {
	t.Parallel()

	read, err := sequenceOf(t, specExample(t))
	if err != nil {
		t.Fatalf("the reader rejects SPEC.md's own worked example: %v", err)
	}

	want := "(sequence (seq FILE-HEADER (* (seq ORDER-HEADER " +
		"(times ORDER-DETAIL (item ORDER-HEADER OH-DETAIL-COUNT)))) " +
		"(when (item FILE-HEADER FH-TRAILER-FLAG) \"Y\" FILE-TRAILER)))"

	if got := read.String(); got != want {
		t.Errorf("the example sequences as\n%s\nwant\n%s", got, want)
	}
}
