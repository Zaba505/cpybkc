// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package diag

import (
	"errors"
	"fmt"
	"testing"
)

// TestSpanRendersWhereSomethingIs pins the four shapes a span takes.
//
// The third and the fourth are the ones that matter. A copybook declaring no
// `01`-level has nothing to point at but itself, so a span with a file and no
// line renders as the file — docs/layout/SPEC.md's "the span is the file" — and
// a span naming nowhere renders as nothing, because an invented `0:0` is a
// position a reader would try to open.
func TestSpanRendersWhereSomethingIs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		span Span
		want string
	}{
		{
			name: "a file, a line and a column",
			span: Span{File: "orders.sexpr", Line: 4, Column: 22},
			want: "orders.sexpr:4:22",
		},
		{
			name: "a file with nothing in it to point at",
			span: Span{File: "cpy/order.cpy"},
			want: "cpy/order.cpy",
		},
		{
			name: "a place in a source nobody named",
			span: Span{Line: 7, Column: 3},
			want: "7:3",
		},
		{
			name: "nowhere at all",
			span: Span{},
			want: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.span.String(); got != testCase.want {
				t.Errorf("rendered %q, want %q", got, testCase.want)
			}

			if got := testCase.span.Stated(); got != (testCase.want != "") {
				t.Errorf("Stated is %t on a span rendering %q", got, testCase.want)
			}
		})
	}
}

// TestDiagnosticRendersEveryPlaceItImplicates is the acceptance criterion this
// package exists for: a fault that implicates two files says so, and says it in
// one shape.
func TestDiagnosticRendersEveryPlaceItImplicates(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		diagnostic Diagnostic
		want       string
	}{
		{
			name: "one place",
			diagnostic: Diagnostic{
				Message: "form \"encoding\" states charset twice",
				Spans:   []Span{{File: "orders.sexpr", Line: 3, Column: 3}},
			},
			want: "orders.sexpr:3:3: form \"encoding\" states charset twice",
		},
		{
			name: "a layout and a copybook",
			diagnostic: Diagnostic{
				Message: "the copybook \"cpy/order.cpy\" declares no ORDER-HEADER-REC",
				Spans: []Span{
					{File: "orders.sexpr", Line: 4, Column: 22},
					{File: "cpy/order.cpy", Line: 1, Column: 8, Note: "it declares ORDER-REC"},
				},
			},
			want: "orders.sexpr:4:22: the copybook \"cpy/order.cpy\" declares no ORDER-HEADER-REC\n" +
				"  cpy/order.cpy:1:8: it declares ORDER-REC",
		},
		{
			name: "a copybook with nothing in it to point at",
			diagnostic: Diagnostic{
				Message: "the copybook \"cpy/empty.cpy\" declares no ORDER-HEADER-REC",
				Spans: []Span{
					{File: "orders.sexpr", Line: 4, Column: 22},
					{File: "cpy/empty.cpy", Note: "it declares no 01-level at all"},
				},
			},
			want: "orders.sexpr:4:22: the copybook \"cpy/empty.cpy\" declares no ORDER-HEADER-REC\n" +
				"  cpy/empty.cpy: it declares no 01-level at all",
		},
		{
			name: "more places than two",
			diagnostic: Diagnostic{
				Message: "the item reference (item ORDER-HEADER AMOUNT) names two items",
				Spans: []Span{
					{File: "orders.sexpr", Line: 9, Column: 5},
					{File: "cpy/order.cpy", Line: 4, Column: 9, Note: "one of them is here"},
					{File: "cpy/order.cpy", Line: 8, Column: 9, Note: "and one of them is here"},
				},
			},
			want: "orders.sexpr:9:5: the item reference (item ORDER-HEADER AMOUNT) names two items\n" +
				"  cpy/order.cpy:4:9: one of them is here\n" +
				"  cpy/order.cpy:8:9: and one of them is here",
		},
		{
			name:       "a message with nowhere to put it",
			diagnostic: Diagnostic{Message: "the layout could not be read"},
			want:       "the layout could not be read",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.diagnostic.String(); got != testCase.want {
				t.Errorf("rendered:\n%s\nwant:\n%s", got, testCase.want)
			}
		})
	}
}

// TestListAccumulatesRatherThanFailingFast is the criterion [List] exists for.
// A pass that stopped at the first fault would be a pass run once per fault,
// and each fault it did report has to stay assertable on its own.
func TestListAccumulatesRatherThanFailingFast(t *testing.T) {
	t.Parallel()

	var list List

	if list.Failed() {
		t.Error("an empty list reports a fault")
	}

	if err := list.Err(); err != nil {
		t.Errorf("an empty list yields %v, want nil", err)
	}

	first := &MissingCopybookError{Path: "cpy/order.cpy"}
	second := &UndeclaredItemError{Path: "cpy/order.cpy", Item: "ORDER-HEADER-REC"}

	list.Fail(first)
	list.Fail(second)

	if !list.Failed() {
		t.Error("a list with two faults in it reports none")
	}

	err := list.Err()

	var missing *MissingCopybookError
	if !errors.As(err, &missing) || missing != first {
		t.Errorf("the first fault is not assertable out of %v", err)
	}

	var undeclared *UndeclaredItemError
	if !errors.As(err, &undeclared) || undeclared != second {
		t.Errorf("the second fault is not assertable out of %v", err)
	}
}

// TestDiagnosticsWalksEveryFaultAJoinCarries is why the walk is written out
// rather than left to [errors.As]: As finds the first diagnostic in a tree, and
// a caller printing faults wants all of them, in the order they were reported.
func TestDiagnosticsWalksEveryFaultAJoinCarries(t *testing.T) {
	t.Parallel()

	if got := Diagnostics(nil); got != nil {
		t.Errorf("a nil error yields %v, want nothing", got)
	}

	err := errors.Join(
		&MissingCopybookError{
			Pos:  Span{File: "orders.sexpr", Line: 4, Column: 22},
			Path: "cpy/order.cpy",
		},
		errors.New("orders.sexpr:6:3: form \"framing\" states recfm twice"),
		errors.Join(
			&UndeclaredItemError{
				Pos:      Span{File: "orders.sexpr", Line: 9, Column: 22},
				Path:     "cpy/detail.cpy",
				Item:     "ORDER-DETAIL-REC",
				Copybook: Span{File: "cpy/detail.cpy", Line: 1, Column: 5},
				Declares: []string{"ORDER-DTL-REC"},
			},
		),
	)

	want := []string{
		"there is no copybook to read at \"cpy/order.cpy\"",
		"orders.sexpr:6:3: form \"framing\" states recfm twice",
		"the copybook \"cpy/detail.cpy\" declares no ORDER-DETAIL-REC",
	}

	found := Diagnostics(err)
	if len(found) != len(want) {
		t.Fatalf("found %d diagnostics, want %d:\n%s", len(found), len(want), Render(err))
	}

	for i, diagnostic := range found {
		if diagnostic.Message != want[i] {
			t.Errorf("diagnostic %d says %q, want %q", i, diagnostic.Message, want[i])
		}
	}
}

// TestDiagnosticsKeepsTheWrapperAWrapperWrote is the other half of the walk. An
// error wrapping a diagnostic is reported as its wrapper wrote it: the wrapping
// is something a caller chose to say, and reaching inside would report a fault
// under a description nobody wrote.
func TestDiagnosticsKeepsTheWrapperAWrapperWrote(t *testing.T) {
	t.Parallel()

	inner := &MissingCopybookError{
		Pos:  Span{File: "orders.sexpr", Line: 4, Column: 22},
		Path: "cpy/order.cpy",
	}

	found := Diagnostics(fmt.Errorf("failed to resolve ORDER-HEADER: %w", inner))
	if len(found) != 1 {
		t.Fatalf("found %d diagnostics, want 1", len(found))
	}

	want := "failed to resolve ORDER-HEADER: orders.sexpr:4:22: there is no copybook to read at \"cpy/order.cpy\""
	if found[0].Message != want {
		t.Errorf("the diagnostic says %q, want %q", found[0].Message, want)
	}
}

// goldenRender is the rendering of the whole of one rejected layout, written
// out in full.
//
// It is the pinned output docs/layout/SPEC.md's "Every diagnostic carries a
// span, and some carry two" is checked against: three faults reported together
// rather than one per run, every one of them naming the file it is in, and the
// one about a copybook naming the copybook as well as the layout. A golden is
// the right shape for it because every part of the rendering — the order, the
// indent, the colon after a span — is something somebody reads.
const goldenRender = `orders.sexpr:2:12: charset is one of cp037, cp500, cp1047, cp1140 or ascii, and this one says "cp999"
orders.sexpr:5:22: there is no copybook to read at "cpy/order.cpy"
orders.sexpr:9:22: the copybook "cpy/detail.cpy" declares no ORDER-DETAIL-REC
  cpy/detail.cpy:1:8: it declares ORDER-DTL-REC and ORDER-TRAILER-REC`

// TestRenderIsTheGolden pins what a caller with a joined error and a terminal
// sees.
func TestRenderIsTheGolden(t *testing.T) {
	t.Parallel()

	err := errors.Join(
		errors.New(`orders.sexpr:2:12: charset is one of cp037, cp500, cp1047, cp1140 or ascii, and this one says "cp999"`),
		&MissingCopybookError{
			Pos:  Span{File: "orders.sexpr", Line: 5, Column: 22},
			Path: "cpy/order.cpy",
			Err:  errors.New("open /srv/cpy/order.cpy: no such file or directory"),
		},
		&UndeclaredItemError{
			Pos:      Span{File: "orders.sexpr", Line: 9, Column: 22},
			Path:     "cpy/detail.cpy",
			Item:     "ORDER-DETAIL-REC",
			Copybook: Span{File: "cpy/detail.cpy", Line: 1, Column: 8},
			Declares: []string{"ORDER-DTL-REC", "ORDER-TRAILER-REC"},
		},
	)

	if got := Render(err); got != goldenRender {
		t.Errorf("the rendering is not the golden\n got:\n%s\nwant:\n%s", got, goldenRender)
	}

	if got := Render(nil); got != "" {
		t.Errorf("a nil error renders as %q, want nothing", got)
	}
}

// TestJoinAndReadsAsASentence covers the joining a message does when it lists what
// an adopter could have meant.
func TestJoinAndReadsAsASentence(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		items []string
		want  string
	}{
		{items: nil, want: ""},
		{items: []string{"ORDER-REC"}, want: "ORDER-REC"},
		{items: []string{"ORDER-REC", "ORDER-TRAILER-REC"}, want: "ORDER-REC and ORDER-TRAILER-REC"},
		{
			items: []string{"ORDER-REC", "ORDER-DTL-REC", "ORDER-TRAILER-REC"},
			want:  "ORDER-REC, ORDER-DTL-REC and ORDER-TRAILER-REC",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.want, func(t *testing.T) {
			t.Parallel()

			if got := joinAnd(testCase.items); got != testCase.want {
				t.Errorf("joined to %q, want %q", got, testCase.want)
			}
		})
	}
}
