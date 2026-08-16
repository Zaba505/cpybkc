// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// The diagnostic form docs/plugin/SPEC.md fixes: one line on standard error,
// UTF-8, `<severity>: <message>`, with the severity opening the line and the
// separator a colon and a single space.
//
// `warning` is the third severity the contract names and this program has
// nothing to say at it yet; it is spelled where it is used rather than declared
// here for a caller that does not exist.
const (
	severityError = "error"
	severityNote  = "note"

	severitySeparator = ": "
)

// noted is an error carrying lines that follow it as `note:` diagnostics.
//
// docs/plugin/SPEC.md admits no multi-line message: a diagnostic that needs
// more than one line is written as one `error:` line followed by `note:` lines.
// This is how an error says what its extra lines are without composing them
// into a message the writer would then have to take apart.
type noted interface {
	Notes() []string
}

// report writes err to w as the diagnostics a plugin's failure owes its user.
//
// The error's own message is the `error:` line and everything after it is a
// `note:` line — the notes an error carries as [noted], and any line after the
// first of a message that arrived with newlines in it, which is what keeps a
// wrapped error from writing a second line that no severity opens. cpybkc
// records such a line verbatim at warning rather than discarding it, so the
// cost of getting this wrong is a diagnostic filed under the wrong level rather
// than one lost; that is a reason to be careful, not a reason to rely on it.
func report(w io.Writer, err error) {
	message := err.Error()
	if strings.TrimSpace(message) == "" {
		// A bare `error: ` says nothing a level could be attached to, and
		// docs/plugin/SPEC.md has cpybkc treat it as text. An error with no
		// message is a bug in this program, and it is reported as one rather
		// than as silence beside a non-zero exit.
		message = "the generator failed and said nothing about why, which is a bug in " + pluginName
	}

	lines := strings.Split(message, "\n")

	// The `error:` line is the first line [diagnostic] would actually write,
	// rather than the first line there is. They differ for a message opening
	// with a newline or with a line of spaces: that line is dropped, and taking
	// it as the error line would leave a failure whose diagnostics open with
	// `note:` while the process still exits non-zero — which is the one shape
	// docs/plugin/SPEC.md forbids, since cpybkc would file the whole failure at
	// info.
	//
	// Nothing in this program composes such a message today. It is guarded
	// anyway for the reason the empty message above is: a message is a wrapped
	// error away from being a shape this function did not choose.
	first := 0
	for first < len(lines) && blank(lines[first]) {
		first++
	}

	// Unreachable: the message has something on it, checked above, so some line
	// is not blank. Written as a condition rather than as an index because the
	// alternative is a panic in a failure path.
	if first == len(lines) {
		first = 0
	}

	diagnostic(w, severityError, lines[first])

	for _, line := range lines[first+1:] {
		diagnostic(w, severityNote, line)
	}

	var carrier noted
	if errors.As(err, &carrier) {
		for _, note := range carrier.Notes() {
			for line := range strings.SplitSeq(note, "\n") {
				diagnostic(w, severityNote, line)
			}
		}
	}
}

// diagnostic writes one line.
//
// An empty message is dropped rather than written: the contract requires a
// message, and a severity with nothing after it is a line cpybkc classifies as
// text.
//
// The write itself is unchecked because there is nowhere left to report a
// failure to write to the diagnostic channel — the exit status is what the
// caller reads, and it is already about to say the run failed.
func diagnostic(w io.Writer, severity, message string) {
	if blank(message) {
		return
	}

	_, _ = fmt.Fprint(w, severity+severitySeparator+strings.TrimRight(message, " \t")+"\n")
}

// blank reports whether a line is one [diagnostic] would drop.
//
// It is the trailing whitespace [diagnostic] strips and nothing more, so that
// "the line report chose" and "the line diagnostic wrote" cannot come apart —
// a looser test here would pick an error line that was then dropped, which is
// the failure this pair is written to avoid.
func blank(line string) bool {
	return strings.TrimRight(line, " \t") == ""
}
