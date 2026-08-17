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
// All three the contract names are spelled here now. `warning` arrived with
// [warn]: a descriptor this generator emits a package for and cannot synthesize
// a checkable file for is a generation that succeeded with something missing
// from it, which is exactly what the middle severity is for. See README.md,
// "When a descriptor admits no checkable file".
const (
	severityError   = "error"
	severityWarning = "warning"
	severityNote    = "note"

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
	first, rest := messageLines(err, "the generator failed and said nothing about why")

	diagnostic(w, severityError, first)

	for _, line := range rest {
		diagnostic(w, severityNote, line)
	}
}

// warn writes a construct the generated tests do not cover as the diagnostics
// it owes: a `warning:` line naming the construct and why, the refusal's own
// lines under it as notes, and a last note saying what the descriptor produced
// anyway.
//
// The construct opens the line because that is the whole value of the
// diagnostic. *No test was generated* sends an adopter to read this generator;
// *the record type ORDER-RECORD, and here is what could not be laid out* sends
// them to the line of the copybook it is about.
//
// The closing note is not decoration either. A `warning:` naming only what is
// missing reads like a failure that did not quite fail, and the reader has to
// go and look at the output directory to find out that they have a package that
// reads and writes their file. See README.md, "When a descriptor admits no
// checkable file".
func warn(w io.Writer, one skipped) {
	first, rest := messageLines(one.why, "the generator wrote no case and said nothing about why")

	diagnostic(w, severityWarning, "no test was generated for "+one.construct+severitySeparator+first)

	for _, line := range rest {
		diagnostic(w, severityNote, line)
	}

	diagnostic(w, severityNote, one.kept+"; see "+refusalSection)
}

// messageLines is an error as a diagnostic writes it: the first line of its
// message, and everything that follows as note lines.
//
// Two things follow. Any line after the first of a message that arrived with
// newlines in it, which is what keeps a wrapped error from writing a second
// line that no severity opens, and the notes it carries as [noted].
//
// silence is what to say when the error's message is empty, and it is a
// parameter because the two severities are silent about different things: an
// `error:` is a generation that failed, and a `warning:` is a case that was not
// written. One sentence covering both would have to drop the verb, and the verb
// is the whole of what the line says.
func messageLines(err error, silence string) (string, []string) {
	message := err.Error()
	if strings.TrimSpace(message) == "" {
		// A bare `error: ` says nothing a level could be attached to, and
		// docs/plugin/SPEC.md has cpybkc treat it as text. An error with no
		// message is a bug in this program, and it is reported as one rather
		// than as silence beside a non-zero exit.
		message = silence + ", which is a bug in " + pluginName
	}

	lines := strings.Split(message, "\n")
	rest := append([]string(nil), lines[1:]...)

	var carrier noted
	if errors.As(err, &carrier) {
		for _, note := range carrier.Notes() {
			rest = append(rest, strings.Split(note, "\n")...)
		}
	}

	return lines[0], rest
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
	message = strings.TrimRight(message, " \t")
	if message == "" {
		return
	}

	_, _ = fmt.Fprint(w, severity+severitySeparator+message+"\n")
}
