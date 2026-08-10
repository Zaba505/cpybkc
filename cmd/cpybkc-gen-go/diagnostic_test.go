// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// TestReportWritesOneDiagnosticPerLine holds the format docs/plugin/SPEC.md
// fixes: a severity from a closed set of three opens the line, the separator is
// a colon and a single space, and no message carries a newline.
//
// cpybkc parses lines of that form into its structured log and surfaces
// anything else verbatim at warning, so a diagnostic that missed the form is
// not lost — it is filed a level above where its author put it. That is a
// reason to be careful rather than one to rely on.
func TestReportWritesOneDiagnosticPerLine(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	report(&stderr, &noteful{
		message: "ORDER-DETAIL.OD-QTY: USAGE COMP-3 is not supported by this generator",
		notes:   []string{"ORDER-DETAIL.OD-QTY: declared as PIC S9(5)V99"},
	})

	want := []string{
		"error: ORDER-DETAIL.OD-QTY: USAGE COMP-3 is not supported by this generator",
		"note: ORDER-DETAIL.OD-QTY: declared as PIC S9(5)V99",
	}

	if got := lines(stderr.String()); !slices.Equal(got, want) {
		t.Errorf("report wrote\n%q\nwant\n%q", got, want)
	}
}

// TestAMessageWithNewlinesBecomesNotes is the multi-line rule: a diagnostic
// needing more than one line is one `error:` line followed by `note:` lines,
// and never a second line no severity opens.
//
// The case is a wrapped error whose cause spans lines, which is not something
// the author of the message chose — so it is folded rather than trusted.
func TestAMessageWithNewlinesBecomesNotes(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	report(&stderr, fmt.Errorf("failed to read the descriptor: %w", errors.New("open it:\nno such file")))

	got := lines(stderr.String())

	if len(got) != 2 {
		t.Fatalf("report wrote %d lines, want 2: %q", len(got), got)
	}

	if !strings.HasPrefix(got[0], severityError+severitySeparator) {
		t.Errorf("the first line is %q, want an %s diagnostic", got[0], severityError)
	}

	if !strings.HasPrefix(got[1], severityNote+severitySeparator) {
		t.Errorf("the second line is %q, want a %s diagnostic", got[1], severityNote)
	}
}

// TestAnEmptyMessageIsStillSaidOutLoud keeps the one shape the contract refuses
// out of the output. A bare `error: ` says nothing a level could be attached
// to, and cpybkc treats it as text; a plugin exiting non-zero in silence is
// worse still.
func TestAnEmptyMessageIsStillSaidOutLoud(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	report(&stderr, errors.New(""))

	got := lines(stderr.String())

	if len(got) != 1 {
		t.Fatalf("report wrote %d lines for an error with no message, want 1: %q", len(got), got)
	}

	if strings.TrimSpace(strings.TrimPrefix(got[0], severityError+severitySeparator)) == "" {
		t.Errorf("report wrote %q, which says nothing", got[0])
	}
}

// noteful is an error carrying notes, as [unsupportedVersionError] does.
type noteful struct {
	message string
	notes   []string
}

func (e *noteful) Error() string { return e.message }

func (e *noteful) Notes() []string { return e.notes }

// lines is what was written, without the trailing empty element a final
// newline leaves.
func lines(written string) []string {
	written = strings.TrimSuffix(written, "\n")
	if written == "" {
		return nil
	}

	return strings.Split(written, "\n")
}
