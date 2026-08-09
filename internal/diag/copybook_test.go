// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package diag

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
)

// TestMissingCopybookNamesThePathAndNothingElse holds the first of the two
// diagnostics docs/layout/SPEC.md's "A copybook that is not there, and an item
// that is not in it" requires by name.
//
// It names the path as the layout spells it and carries exactly one span: there
// is no file to point into, and a span invented for one would point at nothing.
// The path it was looked for under stays out of the message, which is SPEC.md's
// division — where a copybook was looked for is the CLI's to explain — and is
// why the read failure is kept for [errors.As] rather than printed.
func TestMissingCopybookNamesThePathAndNothingElse(t *testing.T) {
	t.Parallel()

	cause := &fs.PathError{Op: "open", Path: "/srv/cpy/order.cpy", Err: fs.ErrNotExist}
	err := &MissingCopybookError{
		Pos:  Span{File: "orders.sexpr", Line: 4, Column: 22},
		Path: "cpy/order.cpy",
		Err:  cause,
	}

	want := `orders.sexpr:4:22: there is no copybook to read at "cpy/order.cpy"`
	if got := err.Error(); got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}

	if spans := err.Diagnostic().Spans; len(spans) != 1 {
		t.Errorf("carries %d spans, want the one into the layout", len(spans))
	}

	if strings.Contains(err.Error(), "/srv/") {
		t.Error("the message names where the copybook was looked for, which is the CLI's to explain")
	}

	var path *fs.PathError
	if !errors.As(err, &path) || path != cause {
		t.Error("the read failure is not reachable with errors.As")
	}
}

// TestUndeclaredItemCarriesBothSpans is the acceptance criterion this issue
// exists for: an error implicating both a layout and a copybook shows both.
//
// The second span points at what is *there*, because an absent item has no
// position and the list an adopter needs is the list of the ones they could
// have meant.
func TestUndeclaredItemCarriesBothSpans(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		err  *UndeclaredItemError
		want string
	}{
		{
			name: "the 01-levels the copybook does declare",
			err: &UndeclaredItemError{
				Pos:      Span{File: "orders.sexpr", Line: 4, Column: 22},
				Path:     "cpy/order.cpy",
				Item:     "ORDER-HEADER-REC",
				Copybook: Span{File: "cpy/order.cpy", Line: 1, Column: 8},
				Declares: []string{"ORDER-REC", "ORDER-TRAILER-REC"},
			},
			want: "orders.sexpr:4:22: the copybook \"cpy/order.cpy\" declares no ORDER-HEADER-REC\n" +
				"  cpy/order.cpy:1:8: it declares ORDER-REC and ORDER-TRAILER-REC",
		},
		{
			name: "a copybook with nothing in it to point at",
			err: &UndeclaredItemError{
				Pos:      Span{File: "orders.sexpr", Line: 4, Column: 22},
				Path:     "cpy/empty.cpy",
				Item:     "ORDER-HEADER-REC",
				Copybook: Span{File: "cpy/empty.cpy"},
			},
			want: "orders.sexpr:4:22: the copybook \"cpy/empty.cpy\" declares no ORDER-HEADER-REC\n" +
				"  cpy/empty.cpy: it declares no 01-level at all",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.err.Error(); got != testCase.want {
				t.Errorf("rendered:\n%s\nwant:\n%s", got, testCase.want)
			}

			spans := testCase.err.Diagnostic().Spans
			if len(spans) != 2 {
				t.Fatalf("carries %d spans, want one into the layout and one into the copybook", len(spans))
			}

			if spans[0].File != "orders.sexpr" {
				t.Errorf("the first span is in %q, want the layout", spans[0].File)
			}

			if spans[1].File != testCase.err.Path {
				t.Errorf("the second span is in %q, want the copybook", spans[1].File)
			}
		})
	}
}

// TestCopybookDiagnosticsAreAssertable is the house pattern: a caller that has
// to know which fault it is asserts against the type, whether or not the fault
// was reported beside others.
func TestCopybookDiagnosticsAreAssertable(t *testing.T) {
	t.Parallel()

	undeclared := &UndeclaredItemError{Path: "cpy/order.cpy", Item: "ORDER-HEADER-REC"}
	err := errors.Join(errors.New("orders.sexpr:1:1: a layout carries exactly one framing form"), undeclared)

	var found *UndeclaredItemError
	if !errors.As(err, &found) || found != undeclared {
		t.Errorf("the fault is not assertable out of %v", err)
	}

	var missing *MissingCopybookError
	if errors.As(err, &missing) {
		t.Error("a copybook that is not there was found in a layout that has one")
	}
}
