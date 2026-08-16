// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// invoke drives one whole invocation and hands back what landed on each stream
// and the status the process would have exited with.
func invoke(args ...string) (stdout, stderr string, code status) {
	var out, errs bytes.Buffer

	code = run(context.Background(), args, &out, &errs)

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
// docs/cli/SPEC.md fixes: the four forms, the one subcommand, and no flag this
// command does not have.
//
// The wording is explicitly not a covered guarantee, so this asserts what is
// named rather than how it reads.
func TestUsageNamesTheSettledCommandSet(t *testing.T) {
	t.Parallel()

	stdout, _, _ := invoke(helpFlag)

	// The verb is named now, and the two flags that reach it are named with it.
	// #214 implements the subcommand this document specified at #183, so a usage
	// text that omitted it would hide a command that runs — which is the inverse
	// of the fault the omission was avoiding while the parsing did not exist.
	// The synopsis carries the second form for the same reason: the forms are
	// the command set, so all four lines are the document's, line for line.
	for _, want := range []string{
		manifestFlag, emitIRFlag, emitIRFormatFlag, versionFlag, helpFlag, shortHelpFlag,
		binaryFormat, jsonFormat, defaultManifest,
		initSubcommand, copybookFlag, outFlag,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("usage does not name %s:\n%s", want, stdout)
		}
	}

	// The bare form still leads, because generating keeps it: it is what every
	// command line already written and every published image whose entrypoint is
	// this CLI already means by `cpybkc`.
	if !strings.Contains(stdout, "  cpybkc [--manifest") {
		t.Errorf("the synopsis does not lead with the bare form:\n%s", stdout)
	}

	// The at-most-once rule is stated with its one exception. --copybook is the
	// flag that repeats, and a summary asserting the rule without it would be
	// telling the reader the second synopsis line is a usage error.
	if strings.Contains(stdout, "at most once.") {
		t.Errorf("usage states the at-most-once rule without naming %s's exception:\n%s", copybookFlag, stdout)
	}

	// The flags docs/cli/SPEC.md's "Out of Scope" refuses by name, each with its
	// reason. A usage text offering one would document a flag the parser has no
	// case for.
	for _, refused := range []string{"--include", "--jobs", "--verbose", "--config"} {
		if strings.Contains(stdout, refused) {
			t.Errorf("usage offers %s, which cpybkc does not have:\n%s", refused, stdout)
		}
	}
}

// TestInitHasItsOwnUsage is docs/cli/SPEC.md's rule about which usage --help
// writes: the named subcommand's where the first argument is one, and the whole
// command's otherwise.
//
// A reader who has typed a verb has narrowed what they are asking about, so the
// answer is `init`'s flags and not an action they are not running.
func TestInitHasItsOwnUsage(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := invoke(initSubcommand, helpFlag)

	if code != statusOK {
		t.Errorf("`cpybkc %s %s` exited %d, want %d", initSubcommand, helpFlag, code, statusOK)
	}

	if stderr != "" {
		t.Errorf("`cpybkc %s %s` wrote %q to standard error, and usage that was asked for is not a diagnostic",
			initSubcommand, helpFlag, stderr)
	}

	if stdout != initUsage {
		t.Errorf("`cpybkc %s %s` wrote %q, want init's usage", initSubcommand, helpFlag, stdout)
	}

	for _, want := range []string{initSubcommand, copybookFlag, outFlag} {
		if !strings.Contains(stdout, want) {
			t.Errorf("init's usage does not name %s:\n%s", want, stdout)
		}
	}

	// The default action's input flags are usage errors under init, so offering
	// them here would document a line that is refused.
	for _, refused := range []string{manifestFlag, emitIRFlag, emitIRFormatFlag} {
		if strings.Contains(stdout, refused) {
			t.Errorf("init's usage offers %s, which is a usage error under %s:\n%s", refused, initSubcommand, stdout)
		}
	}

	// The whole command's usage is unchanged by the subcommand existing.
	if whole, _, _ := invoke(helpFlag); whole != usage {
		t.Errorf("`cpybkc %s` wrote %q, want the whole command's usage", helpFlag, whole)
	}

	// And it promises nothing this build cannot do. It carried a paragraph
	// saying `init` was not implemented for exactly as long as that was true
	// (#214); #215 wrote the derivation, so the paragraph went with it rather
	// than being left as a caveat nobody would notice was stale.
	if strings.Contains(stdout, "Not implemented") {
		t.Errorf("init's usage still disclaims a scaffold this build writes:\n%s", stdout)
	}
}

// TestAnUnknownVerbIsStillAnsweredByHelp is the courtesy docs/cli/SPEC.md
// extends to a reader who is in the middle of getting the line wrong, at the
// position an unrecognised verb is the commonest way to arrive at it: the
// question is answered, with the top-level usage, and the verb is not
// complained about.
func TestAnUnknownVerbIsStillAnsweredByHelp(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := invoke("bogus", helpFlag)

	if code != statusOK {
		t.Errorf("`cpybkc bogus %s` exited %d, want %d", helpFlag, code, statusOK)
	}

	if stdout != usage {
		t.Errorf("`cpybkc bogus %s` wrote %q, want the whole command's usage", helpFlag, stdout)
	}

	if stderr != "" {
		t.Errorf("`cpybkc bogus %s` wrote %q to standard error", helpFlag, stderr)
	}
}

// TestVersionIgnoresASubcommand is the other half of that rule. A build has one
// version whichever action was going to run, so --version does not vary with the
// head the way --help does.
func TestVersionIgnoresASubcommand(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := invoke(initSubcommand, versionFlag)

	if code != statusOK {
		t.Errorf("`cpybkc %s %s` exited %d, want %d", initSubcommand, versionFlag, code, statusOK)
	}

	if stderr != "" {
		t.Errorf("`cpybkc %s %s` wrote %q to standard error", initSubcommand, versionFlag, stderr)
	}

	if want := versionLine() + "\n"; stdout != want {
		t.Errorf("`cpybkc %s %s` wrote %q, want %q", initSubcommand, versionFlag, stdout, want)
	}
}

// TestAUsageErrorUnderInitIsAnsweredWithInitsUsage holds the other half of
// docs/cli/SPEC.md's rule about which usage is written: usage accompanies a
// usage error on standard error, and the one a reader needs is the one whose
// flags they were using.
func TestAUsageErrorUnderInitIsAnsweredWithInitsUsage(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := invoke(initSubcommand, copybookFlag, "posting.cpy")

	if code != statusUsage {
		t.Errorf("`cpybkc %s` with no %s exited %d, want %d", initSubcommand, outFlag, code, statusUsage)
	}

	if stdout != "" {
		t.Errorf("a usage error wrote %q to standard output", stdout)
	}

	if !strings.Contains(stderr, initUsage) {
		t.Errorf("a usage error under %s was answered with %q, want init's usage", initSubcommand, stderr)
	}

	if strings.Contains(stderr, emitIRFlag) {
		t.Errorf("a usage error under %s was answered with the default action's flags: %q", initSubcommand, stderr)
	}
}

// TestInitUnderstoodAndUnperformableIsAFailureOfTheWork is the exit status
// docs/cli/SPEC.md owes a line it read and could not carry out.
//
// Status 2 promises the vector was not understood and that cpybkc did nothing at
// all; status 0 promises a scaffold was written. A `--copybook` naming a file
// that is not there is neither — the line is well-formed and cpybkc had to open
// something to find out — so it is the 1 the document keeps for the faults in
// the work.
func TestInitUnderstoodAndUnperformableIsAFailureOfTheWork(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := invoke(initSubcommand,
		copybookFlag, filepath.Join(t.TempDir(), "absent.cpy"),
		outFlag, filepath.Join(t.TempDir(), "layout.sexpr"),
	)

	if code != statusFailed {
		t.Errorf("an understood %s line exited %d, want %d", initSubcommand, code, statusFailed)
	}

	if stdout != "" {
		t.Errorf("`cpybkc %s` wrote %q to standard output, and no scaffold was written", initSubcommand, stdout)
	}

	if !strings.HasPrefix(stderr, severityError+severitySeparator) {
		t.Errorf("the diagnostic reads %q, want an %s%s line", stderr, severityError, severitySeparator)
	}

	// A failure of the work is not a failure of the vector, so the command set
	// is not what the reader needs in front of them.
	if strings.Contains(stderr, "Usage:") {
		t.Errorf("`cpybkc %s` was answered with usage, and the vector was understood: %q", initSubcommand, stderr)
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

	for _, fact := range []string{programName, reportedVersion(), "IR version"} {
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
//
// The flag is one no action carries. `--out` used to stand here, and it is a
// real flag now — `init`'s — so it is a usage error for a different reason and
// reads differently; the two refusals are told apart in args_test.go, and this
// test wants the plain one.
func TestAUsageErrorExitsTwoAndSaysSoOnStandardError(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := invoke("--nope", "gen")

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

	if !strings.Contains(stderr, "--nope") {
		t.Errorf("the diagnostic does not name the flag that was refused: %q", stderr)
	}

	// Usage accompanies a usage error, on standard error, because the vector is
	// the one failure the command set in front of the reader is enough to fix.
	if !strings.Contains(stderr, "Usage:") {
		t.Errorf("a usage error was reported without usage: %q", stderr)
	}
}

// TestARunWithNoProjectToRunFails is what this command does with a line it
// understood and a project it cannot find.
//
// It fails rather than succeeding silently: silence is success for a generating
// run by docs/cli/SPEC.md's own rule, so a run exiting 0 having generated
// nothing would be indistinguishable from a project whose generators all ran.
// The manifest is the first thing a run reads and there is none in the
// directory a test runs in, so each of these is a run that got as far as
// looking. The two --emit-ir lines are the same failure: an emitting run reads
// the manifest exactly as a generating one does, and there is nothing to resolve
// a descriptor from.
func TestARunWithNoProjectToRunFails(t *testing.T) {
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

		// The subcommand reaches the same three and no fourth: a line it
		// understood and cannot yet perform is the run failing, and a flag
		// written under the wrong action is the vector.
		{initSubcommand, copybookFlag, "posting.cpy", outFlag, standardOutput},
		{initSubcommand, manifestFlag, defaultManifest},
		{copybookFlag, "posting.cpy"},
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

	for line := range strings.SplitSeq(strings.TrimRight(diagnostics, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, continuationIndent):
			// A continuation line carries no severity of its own, and the
			// two-space indent is what tells the two apart.
		case strings.HasPrefix(line, severityError+severitySeparator),
			strings.HasPrefix(line, severityNote+severitySeparator),
			strings.HasPrefix(line, severityWarning+severitySeparator):
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
