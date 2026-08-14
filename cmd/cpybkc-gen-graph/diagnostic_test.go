// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// TestADiagnosticIsOneLineOpeningWithItsSeverity is the form
// docs/plugin/SPEC.md fixes, and the whole of it: the severity opens the line,
// the separator is a colon and a single space, and the message carries no
// newline.
func TestADiagnosticIsOneLineOpeningWithItsSeverity(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	report(&stderr, errors.New("ORDER-DETAIL.OD-QTY: USAGE COMP-3 is not supported"))

	want := severityError + severitySeparator + "ORDER-DETAIL.OD-QTY: USAGE COMP-3 is not supported\n"
	if stderr.String() != want {
		t.Errorf("report wrote %q, want %q", stderr.String(), want)
	}
}

// TestEveryLineAfterTheFirstIsANote is how an error that needs more than one
// line is written. The contract admits no multi-line message, so the second
// line of a wrapped error is a `note:` rather than a line no severity opens —
// which cpybkc would file at warning as text it could not classify.
func TestEveryLineAfterTheFirstIsANote(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	report(&stderr, errors.New("the first line\nthe second\nthe third"))

	want := severityError + severitySeparator + "the first line\n" +
		severityNote + severitySeparator + "the second\n" +
		severityNote + severitySeparator + "the third\n"

	if stderr.String() != want {
		t.Errorf("report wrote %q, want %q", stderr.String(), want)
	}
}

// TestAnErrorsNotesFollowItAsNotes covers the [noted] carrier, which is how the
// version refusal says what to do about the mismatch it just named.
func TestAnErrorsNotesFollowItAsNotes(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	report(&stderr, &unsupportedVersionError{Descriptor: supportedIRVersion + 1})

	lines := strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("a refusal wrote %q, and it carries a note saying what to do about it", stderr.String())
	}

	if !strings.HasPrefix(lines[1], severityNote+severitySeparator) {
		t.Errorf("the refusal's second line is %q, want a %s%s line", lines[1], severityNote, severitySeparator)
	}
}

// TestAFailureAlwaysWritesAnErrorLine is the invariant the acceptance criteria
// state and the one a message this program did not compose could break: an
// `error:` line always accompanies a non-zero exit.
//
// Each of these is a message [report] is entitled to be handed and did not
// choose — a wrapped error whose text opens with a newline, one that is
// nothing but whitespace, one that is empty. None is reachable from an error
// this program builds today, and all three are what a failure looks like one
// `%w` away from somebody else's error type.
func TestAFailureAlwaysWritesAnErrorLine(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		message string
	}{
		{name: "an ordinary message", message: "something went wrong"},
		{name: "a message opening with a newline", message: "\nthe real message"},
		{name: "a message opening with a blank line of spaces", message: "   \nthe real message"},
		{name: "a message that is only whitespace", message: " \n\t\n "},
		{name: "an empty message", message: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer
			report(&stderr, errors.New(testCase.message))

			written := stderr.String()

			if !strings.HasPrefix(written, severityError+severitySeparator) {
				t.Fatalf("report wrote %q, and a failure opens with an %s%s line",
					written, severityError, severitySeparator)
			}

			first, _, _ := strings.Cut(written, "\n")
			if strings.TrimSpace(strings.TrimPrefix(first, severityError+severitySeparator)) == "" {
				t.Errorf("report wrote %q, and the contract requires a message after the severity", first)
			}
		})
	}
}
