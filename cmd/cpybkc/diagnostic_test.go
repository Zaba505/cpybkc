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

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
	"github.com/Zaba505/cpybkc/internal/manifest"
)

// reported is what a caller holding err and a terminal sees.
func reported(err error) string {
	var stderr bytes.Buffer

	report(&stderr, err)

	return stderr.String()
}

// The goldens below are written out in full rather than assembled, because what
// they pin is the shape of the stream and not the wording of any one message.
// docs/cli/SPEC.md says as much: the wording is explicitly not a covered
// guarantee and the format around it is, so a golden built from the messages
// would agree with itself whatever the format became.

// brokenManifest is a manifest with three independent faults in it, in three
// different places: a field no manifest has, an option set written as a single
// string, and a required field that is not there at all.
//
// Nothing here depends on any two of them, which is the point — a manifest is
// written by hand and gets three things wrong at once, and a reader that
// stopped at the first would be a reader the adopter ran three times.
const brokenManifest = `{
  "output": "gen",
  "generators": [
    {"name": "go", "out": "gen/orders", "options": "verbose=true"}
  ]
}`

// goldenManifest is what a malformed manifest reads as.
//
// Every line names the manifest, the line and the column, and all three faults
// are there: docs/cli/SPEC.md requires more than one fault found in one pass to
// be reported together rather than one per run.
const goldenManifest = `error: cpybkc.json:2:3: a manifest has no field named "output"; it carries layout and generators
error: cpybkc.json:4:52: generators[0].options is an object of generator options, and this one is text
error: cpybkc.json:1:1: a manifest carries no layout; a project resolves its records against exactly one
`

// TestAMalformedManifestReportsEveryFaultWithItsPlace is the golden for the
// first of the four failures docs/cli/SPEC.md's stream has to carry.
func TestAMalformedManifestReportsEveryFaultWithItsPlace(t *testing.T) {
	t.Parallel()

	m, err := manifest.Read(manifest.Name, strings.NewReader(brokenManifest))
	if err == nil {
		t.Fatalf("the reader accepted a manifest with three faults in it: %+v", m)
	}

	if got := reported(err); got != goldenManifest {
		t.Errorf("standard error is not the golden\n got:\n%s\nwant:\n%s", got, goldenManifest)
	}
}

// brokenLayout is one layout with two independent faults in two places: a
// charset nobody has a table for, and an override naming no axis at all.
const brokenLayout = `(encoding
  (charset cp999)
  (sign-convention ebcdic)
  (byte-order big-endian)
  (float-format ieee-754))
(encoding-override (item ORDER-HEADER OH-AMOUNT))`

// goldenLayout is what an invalid layout reads as.
const goldenLayout = `error: orders.sexpr:2:12: charset is one of cp037, cp500, cp1047, cp1140 or ascii, and this one says "cp999"
error: orders.sexpr:6:1: the override on (item ORDER-HEADER OH-AMOUNT) states no axis; an override states at least one of charset, sign-convention, byte-order or float-format
`

// TestAnInvalidLayoutReportsTheSpanTheReaderResolved is the golden for the
// second, and it is the half of #31 this story is the last mile of: the span a
// reader computed is what lands on the terminal, in `file:line:column` form,
// rather than a description of where the fault is.
func TestAnInvalidLayoutReportsTheSpanTheReaderResolved(t *testing.T) {
	t.Parallel()

	file, err := layout.Parse("orders.sexpr", strings.NewReader(brokenLayout))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	profile, err := layoutmodel.ReadProfile(file)
	if err == nil {
		t.Fatalf("the reader accepted a layout with two faults in it: %+v", profile)
	}

	if got := reported(err); got != goldenLayout {
		t.Errorf("standard error is not the golden\n got:\n%s\nwant:\n%s", got, goldenLayout)
	}
}

// crossFile is the fault #31 exists for: one raised in the layout, about a
// copybook, with a place in each.
var crossFile = &diag.UndeclaredItemError{
	Pos:      diag.Span{File: "orders.sexpr", Line: 22, Column: 9},
	Path:     "cpy/orders.cpy",
	Item:     "ORDER-DETAIL",
	Copybook: diag.Span{File: "cpy/orders.cpy", Line: 41, Column: 8},
	Declares: []string{"OD-QUANTITY", "OD-PRICE"},
}

// goldenCrossFile is the shape docs/cli/SPEC.md writes out by hand: the fault
// under the place it is at, and the second file on a continuation line opening
// with exactly two spaces and carrying no severity of its own.
const goldenCrossFile = `error: orders.sexpr:22:9: the copybook "cpy/orders.cpy" declares no ORDER-DETAIL
  cpy/orders.cpy:41:8: it declares OD-QUANTITY and OD-PRICE
`

// TestACrossFileFaultCarriesBothPlaces is the criterion that a validation
// error's spans reach the terminal across a layout that references copybooks,
// and not just the one span the message leads with.
func TestACrossFileFaultCarriesBothPlaces(t *testing.T) {
	t.Parallel()

	if got := reported(crossFile); got != goldenCrossFile {
		t.Errorf("standard error is not the golden\n got:\n%s\nwant:\n%s", got, goldenCrossFile)
	}
}

// TestAContinuationLineIsNotADiagnostic holds the one rule that tells the two
// apart. A reader scanning a column of faults reads the unindented lines and
// finds one per thing to fix; a script matching `^error: ` has to find the same
// number.
func TestAContinuationLineIsNotADiagnostic(t *testing.T) {
	t.Parallel()

	stderr := reported(crossFile)

	for _, line := range strings.Split(strings.TrimRight(stderr, "\n"), "\n") {
		if strings.HasPrefix(line, continuationIndent) {
			for _, severity := range []string{severityError, severityWarning, severityNote} {
				if strings.HasPrefix(strings.TrimLeft(line, " "), severity+severitySeparator) {
					t.Errorf("the continuation line %q carries a severity of its own", line)
				}
			}

			continue
		}

		if !strings.HasPrefix(line, severityError+severitySeparator) {
			t.Errorf("%q opens with neither a severity nor the continuation indent", line)
		}
	}
}

// TestEveryFaultOfAJoinedErrorIsReported is the criterion stated against the
// shape the readers actually hand back: [errors.Join], which is what a reader
// that kept going after a fault returns.
func TestEveryFaultOfAJoinedErrorIsReported(t *testing.T) {
	t.Parallel()

	joined := errors.Join(
		&manifest.NotFoundError{Path: "cpybkc.json"},
		crossFile,
		errors.New("something with no diagnostic of its own"),
	)

	stderr := reported(joined)

	if got, want := strings.Count(stderr, severityError+severitySeparator), 3; got != want {
		t.Errorf("reported %d faults, want %d:\n%s", got, want, stderr)
	}

	if !strings.Contains(stderr, "something with no diagnostic of its own") {
		t.Errorf("the fault carrying no diagnostic was dropped:\n%s", stderr)
	}
}

// TestTheSameFailureIsReportedByteForByte is docs/cli/SPEC.md's determinism
// rule for this stream, asserted where it can be: one stage's faults, reported
// twice.
func TestTheSameFailureIsReportedByteForByte(t *testing.T) {
	t.Parallel()

	m, err := manifest.Read(manifest.Name, strings.NewReader(brokenManifest))
	if err == nil {
		t.Fatalf("the reader accepted a manifest with three faults in it: %+v", m)
	}

	first := reported(err)

	for range 8 {
		if got := reported(err); got != first {
			t.Fatalf("the same failure reported differently\nfirst:\n%s\nthen:\n%s", first, got)
		}
	}
}

// TestAFaultWithNoPlaceIsReportedWithoutOne covers the span a diagnostic has
// not got. An invented `0:0` is a position a reader would try to open, so a
// fault that names nowhere says what it has to say and nothing else.
func TestAFaultWithNoPlaceIsReportedWithoutOne(t *testing.T) {
	t.Parallel()

	want := severityError + severitySeparator + "the run was cancelled\n"

	if got := reported(errors.New("the run was cancelled")); got != want {
		t.Errorf("report wrote %q, want %q", got, want)
	}
}

// TestAContinuationWithNoPlaceCarriesOnlyItsNote is the second half of the
// continuation form docs/cli/SPEC.md fixes: a fault implicating something with
// no place to name still says what it has to say, indented, and still carries
// no severity.
func TestAContinuationWithNoPlaceCarriesOnlyItsNote(t *testing.T) {
	t.Parallel()

	err := &fault{diag.Diagnostic{
		Message: "the generator \"go\" was terminated by signal 9 (killed)",
		Spans: []diag.Span{
			{},
			{Note: "a generator that is killed is usually the machine running out of memory"},
		},
	}}

	want := severityError + severitySeparator + "the generator \"go\" was terminated by signal 9 (killed)\n" +
		continuationIndent + "a generator that is killed is usually the machine running out of memory\n"

	if got := reported(err); got != want {
		t.Errorf("report wrote %q, want %q", got, want)
	}
}

// fault is an error carrying whatever diagnostic a test needs, for the shapes
// no reader in this repository happens to raise today.
type fault struct{ diagnostic diag.Diagnostic }

func (f *fault) Error() string { return f.diagnostic.String() }

func (f *fault) Diagnostic() diag.Diagnostic { return f.diagnostic }
