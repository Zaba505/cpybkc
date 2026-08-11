// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"strings"
	"testing"
)

// invoke drives one whole invocation and hands back what landed on each stream
// and the status the process would have exited with.
func invoke(args ...string) (stdout, stderr string, code status) {
	var out, errs bytes.Buffer

	code = run(args, &out, &errs)

	return out.String(), errs.String(), code
}

// TestHelpIsWrittenToStandardOutputAndSucceeds is docs/cli/SPEC.md's rule about
// which stream usage goes to. The distinction is the whole of what makes
// `cpybkc --help | less` work while keeping a failing run's output off the data
// channel.
func TestHelpIsWrittenToStandardOutputAndSucceeds(t *testing.T) {
	t.Parallel()

	for _, flag := range []string{helpFlag, shortHelpFlag} {
		stdout, stderr, code := invoke(flag)

		if code != statusOK {
			t.Errorf("%s exited %d, want %d", flag, code, statusOK)
		}

		if stdout != usage {
			t.Errorf("%s wrote %q to standard output, want the usage text", flag, stdout)
		}

		if stderr != "" {
			t.Errorf("%s wrote %q to standard error, and usage that was asked for is not a diagnostic", flag, stderr)
		}
	}
}

// TestUsageNamesTheSettledCommandSet holds the help text to the surface
// docs/cli/SPEC.md fixes: the three forms, the five flags, and no sixth.
//
// The wording is explicitly not a covered guarantee, so this asserts what is
// named rather than how it reads.
func TestUsageNamesTheSettledCommandSet(t *testing.T) {
	t.Parallel()

	stdout, _, _ := invoke(helpFlag)

	for _, want := range []string{
		manifestFlag, emitIRFlag, emitIRFormatFlag, versionFlag, helpFlag, shortHelpFlag,
		binaryFormat, jsonFormat, defaultManifest,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("usage does not name %s:\n%s", want, stdout)
		}
	}

	// No subcommand set, so no line offering one. Generating is what the
	// command does when nothing else is asked of it, and a usage text that
	// spelled a verb would be documenting a command set this document refuses.
	for _, refused := range []string{"--out", "--include", "--jobs", "--verbose", "--config", "Commands:"} {
		if strings.Contains(stdout, refused) {
			t.Errorf("usage offers %s, which cpybkc does not have:\n%s", refused, stdout)
		}
	}
}

// TestVersionWritesOneLineAndSucceeds holds --version to the form
// docs/cli/SPEC.md fixes: exactly one line naming the program, its own version
// and the IR version this build produces.
func TestVersionWritesOneLineAndSucceeds(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := invoke(versionFlag)

	if code != statusOK {
		t.Errorf("%s exited %d, want %d", versionFlag, code, statusOK)
	}

	if stderr != "" {
		t.Errorf("%s wrote %q to standard error", versionFlag, stderr)
	}

	if lines := strings.Count(stdout, "\n"); lines != 1 || !strings.HasSuffix(stdout, "\n") {
		t.Errorf("%s wrote %q, want exactly one line", versionFlag, stdout)
	}

	// Derived from the constants rather than written out, so that this asserts
	// the line names what the build is made of whatever those become. A literal
	// here would pass on the wrong numbers the day one of them moves.
	want := versionLine() + "\n"
	if stdout != want {
		t.Errorf("%s wrote %q, want %q", versionFlag, stdout, want)
	}

	for _, fact := range []string{programName, version, "IR version"} {
		if !strings.Contains(stdout, fact) {
			t.Errorf("the version line does not name %q: %q", fact, stdout)
		}
	}
}

// TestVersionCarriesNoBuildProvenance is what the line deliberately does not
// promise. A version number is what identifies a release and the rest is
// recoverable from it, so a commit, a build date or a Go version on this line
// would be surface nobody agreed to keep.
func TestVersionCarriesNoBuildProvenance(t *testing.T) {
	t.Parallel()

	stdout, _, _ := invoke(versionFlag)

	for _, absent := range []string{"go1.", "commit", "built"} {
		if strings.Contains(strings.ToLower(stdout), absent) {
			t.Errorf("the version line carries %q: %q", absent, stdout)
		}
	}
}

// TestAUsageErrorExitsTwoAndSaysSoOnStandardError is the status a caller can
// act on without knowing anything about the project, and the stream the
// diagnostic owes.
func TestAUsageErrorExitsTwoAndSaysSoOnStandardError(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := invoke("--out", "gen")

	if code != statusUsage {
		t.Errorf("an unrecognised flag exited %d, want %d", code, statusUsage)
	}

	// docs/cli/SPEC.md: cpybkc MUST NOT write a diagnostic to standard output
	// under any circumstance. A descriptor going to standard output shares that
	// stream with nothing, which is what keeps `--emit-ir -` pipeable.
	if stdout != "" {
		t.Errorf("a usage error wrote %q to standard output", stdout)
	}

	if !strings.HasPrefix(stderr, severityError+severitySeparator) {
		t.Errorf("the diagnostic reads %q, want an %s%s line", stderr, severityError, severitySeparator)
	}

	if !strings.Contains(stderr, "--out") {
		t.Errorf("the diagnostic does not name the flag that was refused: %q", stderr)
	}

	// Usage accompanies a usage error, on standard error, because the vector is
	// the one failure the command set in front of the reader is enough to fix.
	if !strings.Contains(stderr, "Usage:") {
		t.Errorf("a usage error was reported without usage: %q", stderr)
	}
}

// TestARunFailsUntilThePipelineIsWired is what this scaffold does with a line
// it understood.
//
// It fails rather than succeeding silently: silence is success for a generating
// run by docs/cli/SPEC.md's own rule, so a scaffold exiting 0 having generated
// nothing would be indistinguishable from a project whose generators all ran.
func TestARunFailsUntilThePipelineIsWired(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		nil,
		{manifestFlag, "projects/orders/cpybkc.json"},
		{emitIRFlag, standardOutput},
		{emitIRFlag, "orders.json", emitIRFormatFlag, jsonFormat},
	} {
		stdout, stderr, code := invoke(args...)

		if code != statusFailed {
			t.Errorf("%q exited %d, want %d", args, code, statusFailed)
		}

		// A run writes nothing to standard output — no progress, no timing, no
		// summary, and no line saying a run succeeded.
		if stdout != "" {
			t.Errorf("%q wrote %q to standard output, and a run writes nothing there", args, stdout)
		}

		if !strings.HasPrefix(stderr, severityError+severitySeparator) {
			t.Errorf("%q failed with %q, want an %s%s line", args, stderr, severityError, severitySeparator)
		}

		// A failure of the work is not a failure of the vector, so the command
		// set is not what the reader needs in front of them.
		if strings.Contains(stderr, "Usage:") {
			t.Errorf("%q was answered with usage, and the vector was understood: %q", args, stderr)
		}
	}
}

// TestEveryEnumeratedStatusIsReachable is the acceptance criterion stated as a
// test: docs/cli/SPEC.md enumerates three statuses and cpybkc exits with no
// other, so each has to be reachable from a real invocation and none may be
// dead.
func TestEveryEnumeratedStatusIsReachable(t *testing.T) {
	t.Parallel()

	reached := map[status][]string{}

	for _, args := range [][]string{
		{versionFlag},
		{helpFlag},
		nil,
		{"--nope"},
		{"an-operand"},
		{emitIRFormatFlag, jsonFormat},
	} {
		_, _, code := invoke(args...)
		reached[code] = append(reached[code], strings.Join(args, " "))
	}

	for _, want := range []status{statusOK, statusFailed, statusUsage} {
		if len(reached[want]) == 0 {
			t.Errorf("no invocation exits %d, and every status this document enumerates is reachable", want)
		}
	}

	for got, by := range reached {
		switch got {
		case statusOK, statusFailed, statusUsage:
		default:
			t.Errorf("`cpybkc %s` exits %d, which is not a status this document enumerates", by[0], got)
		}
	}
}

// TestEveryDiagnosticLineCarriesASeverity holds standard error to the
// diagnostic format: one diagnostic per line, `<severity>: <message>`, from the
// closed set of three matched case-sensitively.
//
// Usage is the one thing on that stream that is not a diagnostic, and it is
// there because docs/cli/SPEC.md puts it there; it is skipped by starting at
// the first blank line, which is where the diagnostics end and usage begins.
func TestEveryDiagnosticLineCarriesASeverity(t *testing.T) {
	t.Parallel()

	_, stderr, _ := invoke("--nope")

	diagnostics, _, _ := strings.Cut(stderr, "\n\n")

	for _, line := range strings.Split(strings.TrimRight(diagnostics, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "  "):
			// A continuation line carries no severity of its own, and the
			// two-space indent is what tells the two apart.
		case strings.HasPrefix(line, severityError+severitySeparator),
			strings.HasPrefix(line, severityNote+severitySeparator),
			strings.HasPrefix(line, "warning"+severitySeparator):
		default:
			t.Errorf("standard error carries %q, which opens with no severity", line)
		}
	}
}

// TestAnErrorWithNothingToSayIsReportedAsABug covers the one line report has to
// invent. A bare `error: ` says nothing anybody can act on, and cpybkc's
// diagnostics are the explanation its exit status is the verdict on.
func TestAnErrorWithNothingToSayIsReportedAsABug(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	report(&stderr, &usageError{})

	if !strings.HasPrefix(stderr.String(), severityError+severitySeparator) {
		t.Errorf("an empty error reported as %q", stderr.String())
	}

	if !strings.Contains(stderr.String(), "bug") {
		t.Errorf("an empty error reported as %q, and it is a bug in cpybkc", stderr.String())
	}
}

// TestAWrappedMessagesExtraLinesBecomeNotes keeps a multi-line message from
// putting a line on standard error that no severity opens.
func TestAWrappedMessagesExtraLinesBecomeNotes(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	report(&stderr, &usageError{message: "the first line\nthe second"})

	want := severityError + severitySeparator + "the first line\n" +
		severityNote + severitySeparator + "the second\n"

	if stderr.String() != want {
		t.Errorf("report wrote %q, want %q", stderr.String(), want)
	}
}
