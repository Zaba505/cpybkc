// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"io"
	"strings"
)

// The diagnostic form docs/cli/SPEC.md fixes: everything cpybkc says about a
// run goes to standard error, one diagnostic per line, encoded in UTF-8, as
// `<severity>: <message>`.
//
// The severity set is closed and matched case-sensitively, and it is the plugin
// contract's set spelled the same way on purpose — a person watching one
// terminal sees one stream in one shape whether a line was written by cpybkc or
// relayed from a generator, and a script that greps for `^error: ` catches
// both.
//
// `warning` is the third severity that set names, and this program has nothing
// to say at it yet: it arrives with the relayed generator line, the spans and
// the continuation lines, which are all #150's. It is spelled where it is used
// rather than declared here for a caller that does not exist.
const (
	severityError = "error"
	severityNote  = "note"

	severitySeparator = ": "
)

// report writes err to w as the diagnostics a failed run owes its user.
//
// The error's own message is the `error:` line. A message that arrived with
// newlines in it — a wrapped error, most likely — has its remaining lines
// written as `note:` diagnostics, so that a fault cannot put a line on standard
// error that no severity opens.
func report(w io.Writer, err error) {
	message := err.Error()
	if strings.TrimSpace(message) == "" {
		// A bare `error: ` says nothing anybody can act on. An error with no
		// message is a bug in this program, and it is reported as one rather
		// than as silence beside a non-zero exit.
		message = "cpybkc failed and said nothing about why, which is a bug in cpybkc"
	}

	lines := strings.Split(message, "\n")

	diagnostic(w, severityError, lines[0])

	for _, line := range lines[1:] {
		diagnostic(w, severityNote, line)
	}
}

// diagnostic writes one line.
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
