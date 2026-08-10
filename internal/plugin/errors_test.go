// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package plugin

import (
	"testing"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// TestEveryFaultReadsTheSameThroughEitherEnd is what
// [github.com/Zaba505/cpybkc/internal/diag.Error] asks of a typed error: the
// message a caller prints and the diagnostic a caller inspects are built in one
// place, so the two cannot say different things.
//
// Unlike the manifest reader's faults, neither of these points at a file. A
// generator that is not installed is wrong about the machine rather than about
// a line, and a span invented here would send an adopter to edit text that is
// not the mistake; see the commentary at the top of errors.go.
func TestEveryFaultReadsTheSameThroughEitherEnd(t *testing.T) {
	faults := []error{
		&InvalidNameError{Name: "z5labs/go"},
		&InvalidNameError{},
		&NotFoundError{Name: "go", File: Filename("go")},
		&NotFoundError{
			Name:       "go",
			File:       Filename("go"),
			Searched:   []string{"/home/dev/bin", "/usr/local/bin"},
			PassedOver: []PassedOver{{Path: "/home/dev/bin/cpybkc-gen-go", Fault: "it carries no execute bit"}},
		},
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
	}
}

// TestANameThatCannotBeAFilenameSaysWhichWayItCannot keeps the two refusals
// apart: an empty name and a name with a `/` in it are one MUST met from two
// sides, and a message that read the same for both would tell an adopter who
// wrote nothing that what they wrote contains a slash.
func TestANameThatCannotBeAFilenameSaysWhichWayItCannot(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{
			name: "",
			want: "a generator name is the suffix of the cpybkc-gen-<name> executable it resolves to, " +
				"and this one is empty",
		},
		{
			name: "z5labs/go",
			want: "a generator name is the suffix of the cpybkc-gen-<name> executable it resolves to, " +
				`and "z5labs/go" contains a /`,
		},
	}

	for _, test := range tests {
		fault := &InvalidNameError{Name: test.name}

		if got := diag.Render(fault); got != test.want {
			t.Errorf("%q renders as\n%s\nwant\n%s", test.name, got, test.want)
		}
	}
}

// TestAnUnresolvableNameRendersEverywhereItLooked is the golden the fault is
// held to. The message names the executable, one indented line per file the
// search would not take says why it would not, and the last line is the search
// itself — the half an adopter cannot see for themselves.
func TestAnUnresolvableNameRendersEverywhereItLooked(t *testing.T) {
	tests := []struct {
		about string
		fault *NotFoundError
		want  string
	}{
		{
			about: "nothing of that name anywhere",
			fault: &NotFoundError{
				Name:     "go",
				File:     Filename("go"),
				Searched: []string{"/home/dev/bin", "/usr/local/bin"},
			},
			want: `there is no generator named "go" on PATH; cpybkc looks for an executable named cpybkc-gen-go
  PATH was searched in order: /home/dev/bin, /usr/local/bin`,
		},
		{
			about: "one of them there and not taken",
			fault: &NotFoundError{
				Name:     "go",
				File:     Filename("go"),
				Searched: []string{"/home/dev/bin", "/usr/local/bin"},
				PassedOver: []PassedOver{
					{Path: "/home/dev/bin/cpybkc-gen-go", Fault: "it carries no execute bit"},
					{Path: "/usr/local/bin/cpybkc-gen-go", Fault: "it is a directory"},
				},
			},
			want: `there is no generator named "go" on PATH; cpybkc looks for an executable named cpybkc-gen-go
  /home/dev/bin/cpybkc-gen-go: it carries no execute bit, so the search continued past it
  /usr/local/bin/cpybkc-gen-go: it is a directory, so the search continued past it
  PATH was searched in order: /home/dev/bin, /usr/local/bin`,
		},
		{
			about: "a PATH with nowhere in it",
			fault: &NotFoundError{Name: "go", File: Filename("go")},
			want: `there is no generator named "go" on PATH; cpybkc looks for an executable named cpybkc-gen-go
  PATH names no directory to search`,
		},
	}

	for _, test := range tests {
		if got := diag.Render(test.fault); got != test.want {
			t.Errorf("%s renders as\n%s\nwant\n%s", test.about, got, test.want)
		}
	}
}
