// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package manifest

import (
	"errors"
	"testing"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// somewhere is a position the faults below carry, so that a rendering is
// asserted with a span in it rather than as a bare sentence.
var somewhere = diag.Span{File: Name, Line: 3, Column: 7}

// TestEveryFaultReadsTheSameThroughEitherEnd is what
// [github.com/Zaba505/cpybkc/internal/diag.Error] asks of a typed error: the
// message a caller prints and the diagnostic a caller inspects are built in one
// place, so the two cannot say different things.
//
// It is a table of every fault this package raises rather than a check applied
// to the ones a parse happens to produce, because the pairing is a property of
// each type and a type added without it would go unnoticed until something
// printed one.
func TestEveryFaultReadsTheSameThroughEitherEnd(t *testing.T) {
	faults := []error{
		&NotFoundError{Path: Name},
		&SyntaxError{Span: somewhere, Fault: "the manifest is not valid JSON: something"},
		&TypeError{Span: somewhere, Field: "layout", Want: "text", Found: "a number"},
		&UnknownFieldError{Span: somewhere, Field: "layoutt", In: manifestObject, Known: manifestFields},
		&RepeatedFieldError{Span: somewhere, First: somewhere, Field: "layout", In: manifestObject},
		&MissingFieldError{Span: somewhere, Field: "layout", In: manifestObject, Fault: layoutFault},
		&EmptyValueError{Span: somewhere, Field: "layout", Fault: layoutFault},
		&GeneratorNameError{Span: somewhere, Name: "z5labs/go"},
		&OptionKeyError{Span: somewhere, Key: "a=b"},
	}

	for _, fault := range faults {
		carrier, ok := fault.(diag.Error)
		if !ok {
			t.Errorf("%T carries no diagnostic, want it to implement diag.Error", fault)

			continue
		}

		if got, want := fault.Error(), carrier.Diagnostic().String(); got != want {
			t.Errorf("%T reads as %q and renders as %q, want one wording", fault, got, want)
		}

		if got := carrier.Diagnostic().Spans; len(got) == 0 || !got[0].Stated() {
			t.Errorf("%T points at %v, want a place in the manifest", fault, got)
		}
	}
}

// TestASyntaxFaultGivesUpWhatItWraps is what lets a caller tell one reason a
// manifest would not parse from another without reading the sentence.
func TestASyntaxFaultGivesUpWhatItWraps(t *testing.T) {
	wrapped := errors.New("something")

	if got := errors.Unwrap(&SyntaxError{Span: somewhere, Fault: "x", Err: wrapped}); got != wrapped {
		t.Errorf("the fault gave up %v, want %v", got, wrapped)
	}

	// A syntax fault the reader raised itself — an empty manifest — wraps
	// nothing, and unwrapping it has to say so rather than panic.
	if got := errors.Unwrap(&SyntaxError{Span: somewhere, Fault: "x"}); got != nil {
		t.Errorf("a fault wrapping nothing gave up %v, want nothing", got)
	}
}

func TestListReadsAsASentence(t *testing.T) {
	tests := []struct {
		names []string
		want  string
	}{
		{names: nil, want: ""},
		{names: []string{"layout"}, want: "layout"},
		{names: []string{"layout", "generators", "extras"}, want: "layout, generators and extras"},
		{names: manifestFields, want: "layout and generators"},
		{names: generatorFields, want: "name, out and options"},
	}

	for _, test := range tests {
		if got := list(test.names); got != test.want {
			t.Errorf("%q reads as %q, want %q", test.names, got, test.want)
		}
	}
}
