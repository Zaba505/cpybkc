// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// The diagnostic form docs/cli/SPEC.md fixes: everything cpybkc says about a
// run goes to standard error, one diagnostic per line, encoded in UTF-8, as
// `<severity>: <message>` — or `<severity>: <generator>: <message>` for a line
// that came from a generator.
//
// The severity set is closed and matched case-sensitively, and it is the plugin
// contract's set spelled the same way on purpose — a person watching one
// terminal sees one stream in one shape whether a line was written by cpybkc or
// relayed from a generator, and a script that greps for `^error: ` catches
// both.
//
// `warning` reaches this stream only from a generator: an `error:` cpybkc wrote
// itself fails the run, and a fault that did not fail the run is not something
// any stage of this pipeline raises today.
const (
	severityError   = "error"
	severityWarning = "warning"
	severityNote    = "note"

	// severitySeparator is a colon and a single space and nothing else. It is
	// also what separates a span from the message at it, and a generator's name
	// from the line it wrote, because docs/cli/SPEC.md spells all three the
	// same way.
	severitySeparator = ": "
)

// continuationIndent opens a line that carries a further place a fault
// implicates.
//
// Exactly two spaces, and it is the whole of what tells a continuation line
// from a diagnostic: docs/cli/SPEC.md requires a continuation to carry no
// severity and requires it not to be read as a diagnostic of its own, so the
// indent has to be enough on its own for a reader scanning a column of faults
// and for a script matching `^error: `.
const continuationIndent = "  "

// emptyMessage is what a fault with nothing to say is reported as.
//
// A bare `error: ` says nothing anybody can act on. An error with no message is
// a bug in this program, and it is reported as one rather than as silence
// beside a non-zero exit.
const emptyMessage = "cpybkc failed and said nothing about why, which is a bug in cpybkc"

// report writes err to w as the diagnostics a failed run owes its user.
//
// Every fault in err is reported, not the first: docs/cli/SPEC.md requires more
// than one fault found in one pass to come out together, and the readers in
// this repository already collect rather than stopping, so what arrives here is
// routinely an [errors.Join] of several. [diag.Diagnostics] is what walks that
// tree, in the order the faults were reported, which is also what makes this
// stream deterministic for a given failing input.
func report(w io.Writer, err error) {
	found := diag.Diagnostics(err)
	if len(found) == 0 {
		// Only reachable for a nil error, which is not a failed run. Reporting
		// it is cheaper than a stream that says a run failed and then says
		// nothing.
		diagnostic(w, severityError, emptyMessage)

		return
	}

	for _, d := range found {
		reportDiagnostic(w, d)
	}
}

// reportDiagnostic writes one fault: where it is, what it is, and every further
// place it implicates.
//
//	error: orders.sexpr:22:9: ORDER-DETAIL declares no item OD-QTY
//	  cpy/orders.cpy:41:8: it declares OD-QUANTITY and OD-PRICE
//
// The span leads the message because docs/cli/SPEC.md requires it to, in
// `file:line:column` form — or the file alone where a line number would point
// at nothing, which is [diag.Span.String]'s to decide rather than this
// function's.
//
// A message that arrived with newlines in it — a wrapped error, most likely —
// has its remaining lines written as `note:` diagnostics, so that a fault
// cannot put a line on standard error that no severity opens. They come after
// the continuation lines because the continuations belong to the fault the
// first line states, while a second line of a message is a second thing to say
// about it.
func reportDiagnostic(w io.Writer, d diag.Diagnostic) {
	message := d.Message
	if strings.TrimSpace(message) == "" {
		message = emptyMessage
	}

	lines := strings.Split(message, "\n")

	head := lines[0]
	if len(d.Spans) > 0 && d.Spans[0].Stated() {
		head = d.Spans[0].String() + severitySeparator + head
	}

	diagnostic(w, severityError, head)

	if len(d.Spans) > 1 {
		for _, span := range d.Spans[1:] {
			continuation(w, span)
		}
	}

	for _, line := range lines[1:] {
		diagnostic(w, severityNote, line)
	}
}

// continuation writes one further place a fault implicates: the place, what is
// at it, or — where there is nowhere to name — what it has to say and nothing
// else.
//
// Each line of it is indented separately. A note that arrived carrying a
// newline is still one continuation, and writing its second line unindented
// would put a line on this stream that reads as neither a diagnostic nor part
// of one.
func continuation(w io.Writer, span diag.Span) {
	text := span.Note

	if span.Stated() {
		text = span.String()

		if span.Note != "" {
			text += severitySeparator + span.Note
		}
	}

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			continue
		}

		_, _ = fmt.Fprint(w, continuationIndent+line+"\n")
	}
}

// diagnostic writes one line cpybkc wrote itself, which is why it carries no
// origin: docs/cli/SPEC.md makes the absence of a name what says a line is
// cpybkc's own.
//
// The write is unchecked because there is nowhere left to report a failure to
// write to the diagnostic channel to — the exit status is what the caller
// reads, and it is already about to say the run failed.
func diagnostic(w io.Writer, severity, message string) {
	message = strings.TrimRight(message, " \t")
	if message == "" {
		return
	}

	_, _ = fmt.Fprint(w, severity+severitySeparator+message+"\n")
}
