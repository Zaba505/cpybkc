// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/plugin"
	"github.com/Zaba505/cpybkc/irpb"
)

// The generators below are shell scripts, for the reason
// internal/plugin/invoke_test.go gives: docs/plugin/SPEC.md makes one a
// first-class plugin, and what is under test here is what a real process's
// output looks like once it has been through the contract's parsing and out
// onto this command's standard error. A fake would let this package agree with
// itself about a stream neither half ever wrote to.

// generatorName is what the manifest calls the generator every test below runs,
// and it is the name docs/cli/SPEC.md puts between the severity and the message
// on a relayed line.
const generatorName = "go"

// descriptor is the message a run hands over. It is the smallest whole one,
// because what these tests are about is what came back out on standard error.
func descriptor() *irpb.Descriptor {
	return &irpb.Descriptor{
		Version: irpb.IrVersion_IR_VERSION_1,
		Nodes: []*irpb.Node{
			{
				Id: 1,
				Kind: &irpb.Node_File{File: &irpb.File{
					Framing:      &irpb.File_Unframed{Unframed: &irpb.Unframed{}},
					StartStateId: 2,
				}},
			},
			{
				Id:   2,
				Kind: &irpb.Node_State{State: &irpb.State{Accepts: true}},
			},
		},
	}
}

// runGenerator runs one generator whose body is body, with its output rendered the
// way this command renders it, and hands back that standard error, the
// executable it ran and the run's verdict.
//
// The path comes back because a fault naming it names one under a directory the
// operating system chose; a golden carrying that would be a golden that passes
// on one machine, so [anonymise] is what a golden is compared against.
func runGenerator(t *testing.T, body string) (stderr, executable string, err error) {
	t.Helper()

	dir := t.TempDir()

	path := filepath.Join(dir, plugin.Filename(generatorName))
	if writeErr := writeGenerator(path, "#!/bin/sh\n"+body+"\n"); writeErr != nil {
		t.Fatalf("writing %s: %v", path, writeErr)
	}

	var out bytes.Buffer

	runner := &plugin.Runner{
		Log: logger(&out),
		// PATH is stated rather than inherited because Env is either the whole
		// environment or nothing, and a shell script needs the commands it
		// calls.
		Env: []string{"PATH=" + os.Getenv("PATH")},
	}

	err = runner.Run(t.Context(), descriptor(), []plugin.Invocation{
		{Name: generatorName, Path: path, Out: t.TempDir()},
	})

	return out.String(), path, err
}

// anonymise takes the executable's path back out of a stream, so that what a
// golden pins is everything about the diagnostic except where a test happened
// to put a file.
func anonymise(stream, executable string) string {
	return strings.ReplaceAll(stream, executable, "<generator>")
}

// chatty is a generator writing one line of every kind docs/cli/SPEC.md's relay
// table has a row for, and then exiting zero.
const chatty = `echo 'error: ORDER-DETAIL.OD-QTY: USAGE COMP-3 is not supported by this generator' >&2
echo 'note: ORDER-DETAIL.OD-QTY: declared as PIC S9(5)V99' >&2
echo 'warning: ORDER-TRAILER: no accessor was generated' >&2
echo 'panic: runtime error: index out of range [3]' >&2
echo 'wrote gen/orders/orders.go'`

// goldenChatty is what the four lines [chatty] wrote to standard error read as
// once they are cpybkc's, in the order that generator wrote them.
//
// The severity is never changed on the way through, the generator's name stands
// between the severity and the message, and the line that was not a diagnostic
// is relayed at the level docs/plugin/SPEC.md argues for rather than dropped:
// an unrecognised line on standard error at warning, one level above the note a
// plugin writes deliberately.
const goldenChatty = `error: go: ORDER-DETAIL.OD-QTY: USAGE COMP-3 is not supported by this generator
note: go: ORDER-DETAIL.OD-QTY: declared as PIC S9(5)V99
warning: go: ORDER-TRAILER: no accessor was generated
warning: go: panic: runtime error: index out of range [3]
`

// goldenChattyStdout is the one line [chatty] wrote to its standard output.
//
// It is held apart from the golden above because the two streams are read
// concurrently, so where it lands among the others is not something
// docs/cli/SPEC.md fixes. What it says is: standard output is untidiness rather
// than breakage — the contract makes writing there a SHOULD NOT — so it is
// relayed at note, verbatim, and attributed the same way.
const goldenChattyStdout = "note: go: wrote gen/orders/orders.go"

// TestAGeneratorsDiagnosticsAreRelayedNamedAndAtTheirOwnSeverity is the golden
// for the third of the four failures docs/cli/SPEC.md's stream has to carry.
func TestAGeneratorsDiagnosticsAreRelayedNamedAndAtTheirOwnSeverity(t *testing.T) {
	t.Parallel()

	stderr, _, err := runGenerator(t, chatty)
	if err != nil {
		t.Fatalf("a generator that exited zero failed the run: %v", err)
	}

	// The standard output line is taken out and checked on its own; what is
	// left is standard error, which is fixed line for line.
	var fromStderr []string

	found := false

	for _, line := range lines(stderr) {
		if line == goldenChattyStdout {
			found = true

			continue
		}

		fromStderr = append(fromStderr, line)
	}

	if !found {
		t.Errorf("what the generator wrote to standard output was not relayed as %q:\n%s", goldenChattyStdout, stderr)
	}

	if got := strings.Join(fromStderr, "\n") + "\n"; got != goldenChatty {
		t.Errorf("standard error is not the golden\n got:\n%s\nwant:\n%s", got, goldenChatty)
	}
}

// TestNothingAGeneratorWroteIsRelayedToStandardOutput is the rule that keeps
// `--emit-ir -` pipeable. A generator's output is surfaced on cpybkc's standard
// error and nowhere else, because cpybkc's standard output belongs to whoever
// is reading it.
func TestNothingAGeneratorWroteIsRelayedToStandardOutput(t *testing.T) {
	t.Parallel()

	stderr, _, err := runGenerator(t, chatty)
	if err != nil {
		t.Fatalf("a generator that exited zero failed the run: %v", err)
	}

	if !strings.Contains(stderr, "wrote gen/orders/orders.go") {
		t.Errorf("a line the generator wrote to standard output reached neither stream:\n%s", stderr)
	}
}

// TestARelayedErrorDoesNotFailARunAndAWarningDoesNotEither is the severity rule
// docs/cli/SPEC.md states twice: an `error:` cpybkc wrote itself fails the run,
// a `warning:` or a `note:` never changes the status, and a *relayed* `error:`
// is the generator's explanation rather than its verdict — the exit status is
// the verdict.
func TestARelayedErrorDoesNotFailARunAndAWarningDoesNotEither(t *testing.T) {
	t.Parallel()

	stderr, _, err := runGenerator(t, `echo 'error: ORDER-DETAIL: nothing was generated for this record' >&2
echo 'warning: ORDER-TRAILER: no accessor was generated' >&2
exit 0`)
	if err != nil {
		t.Fatalf("a generator that exited zero after an error diagnostic failed the run: %v", err)
	}

	if statusOf(err) != statusOK {
		t.Errorf("a generator's error diagnostic changed the exit status to %d", statusOf(err))
	}

	for _, want := range []string{
		"error: go: ORDER-DETAIL: nothing was generated for this record",
		"warning: go: ORDER-TRAILER: no accessor was generated",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("standard error does not carry %q:\n%s", want, stderr)
		}
	}
}

// goldenCrash is what a generator that explains itself and then dies reads as:
// its own lines first, at its own severities, and then cpybkc's verdict on the
// invocation with the executable that was running named on a continuation line.
const goldenCrash = `error: go: ORDER-DETAIL.OD-QTY: USAGE COMP-3 is not supported by this generator
warning: go: panic: runtime error: index out of range [3]
error: the generator "go" failed: it exited 2
  <generator>: this is the executable that ran
`

// TestAGeneratorCrashIsReportedWithWhatItSaidFirst is the golden for the fourth
// failure, and it is the one that shows the two halves of this stream in one
// place: a generator's lines are written as they arrive rather than held until
// it exits, so the explanation reaches the user above the verdict on it.
func TestAGeneratorCrashIsReportedWithWhatItSaidFirst(t *testing.T) {
	t.Parallel()

	stderr, executable, err := runGenerator(t, `echo 'error: ORDER-DETAIL.OD-QTY: USAGE COMP-3 is not supported by this generator' >&2
echo 'panic: runtime error: index out of range [3]' >&2
exit 2`)
	if err == nil {
		t.Fatal("a generator that exited 2 did not fail the run")
	}

	if statusOf(err) != statusFailed {
		t.Errorf("a crashed generator exited %d, want %d", statusOf(err), statusFailed)
	}

	got := anonymise(stderr+reported(err), executable)
	if got != goldenCrash {
		t.Errorf("standard error is not the golden\n got:\n%s\nwant:\n%s", got, goldenCrash)
	}
}

// TestAGeneratorKilledIsDistinguishableFromOneThatExited is docs/cli/SPEC.md's
// requirement that the two be told apart in the diagnostic, where the
// difference is actionable: a non-zero exit is a bug in the generator and a
// signal is usually a cancelled run or a machine out of memory.
func TestAGeneratorKilledIsDistinguishableFromOneThatExited(t *testing.T) {
	t.Parallel()

	_, _, err := runGenerator(t, `kill -TERM $$; sleep 30`)
	if err == nil {
		t.Fatal("a generator killed by SIGTERM did not fail the run")
	}

	stderr := reported(err)

	if !strings.Contains(stderr, "terminated by signal") {
		t.Errorf("a killed generator was not reported as killed:\n%s", stderr)
	}

	if strings.Contains(stderr, "it exited") {
		t.Errorf("a killed generator was reported as having exited:\n%s", stderr)
	}

	// The reason it is worth telling apart is on a continuation line, which
	// names no place at all — there is nowhere to point at, and an invented one
	// would be a position a reader would try to open.
	if !strings.Contains(stderr, "\n"+continuationIndent+"a generator that is killed is usually") {
		t.Errorf("the fault implicates nothing a reader can act on:\n%s", stderr)
	}
}

// TestEveryRelayedLineOpensWithASeverity is the invariant a script reading this
// stream depends on, asserted against output nobody wrote for it: a generator
// writing blank lines, indented lines, a bare severity and a severity with no
// space after it.
//
// docs/plugin/SPEC.md puts the last three on the text side of the line and says
// none of them is discarded, so each has to come out carrying a severity of
// cpybkc's choosing rather than not coming out at all.
func TestEveryRelayedLineOpensWithASeverity(t *testing.T) {
	t.Parallel()

	stderr, _, err := runGenerator(t, `echo '' >&2
echo 'error: ' >&2
echo 'error:something' >&2
echo '    at frame 3' >&2
echo 'ERROR: shouting' >&2`)
	if err != nil {
		t.Fatalf("a generator that exited zero failed the run: %v", err)
	}

	written := lines(stderr)
	if len(written) != 5 {
		t.Fatalf("five lines were written and %d came out:\n%s", len(written), stderr)
	}

	for _, line := range written {
		switch {
		case strings.HasPrefix(line, severityError+severitySeparator),
			strings.HasPrefix(line, severityWarning+severitySeparator),
			strings.HasPrefix(line, severityNote+severitySeparator):
		default:
			t.Errorf("standard error carries %q, which opens with no severity", line)
		}
	}
}

// TestALineThatIsNotADiagnosticIsRelayedVerbatim is docs/plugin/SPEC.md's rule
// for the line cpybkc could not classify: it is surfaced verbatim and
// attributed, and verbatim includes the whitespace a generator ended it with.
//
// An indented frame of a stack trace and a line of trailing padding are the two
// this is about. Tidying either would report output the generator did not
// produce, in the one situation — a generator failing in a way its author did
// not anticipate — where what it actually wrote is all a reader has.
func TestALineThatIsNotADiagnosticIsRelayedVerbatim(t *testing.T) {
	t.Parallel()

	stderr, _, err := runGenerator(t, `printf 'at frame 3   \n' >&2
printf '\tgoroutine 1 [running]:  \n' >&2`)
	if err != nil {
		t.Fatalf("a generator that exited zero failed the run: %v", err)
	}

	want := "warning: go: at frame 3   \n" +
		"warning: go: \tgoroutine 1 [running]:  \n"

	if stderr != want {
		t.Errorf("standard error is %q, want %q", stderr, want)
	}
}

// TestABlankLineIsRelayedAsTheSeverityAndTheNameAlone is the one line with
// nothing to preserve. It is still relayed, because a generator's output is
// never discarded, and the separator's trailing space goes with the message it
// had nothing to separate.
func TestABlankLineIsRelayedAsTheSeverityAndTheNameAlone(t *testing.T) {
	t.Parallel()

	stderr, _, err := runGenerator(t, `echo '' >&2`)
	if err != nil {
		t.Fatalf("a generator that exited zero failed the run: %v", err)
	}

	if got, want := stderr, "warning: go:\n"; got != want {
		t.Errorf("standard error is %q, want %q", got, want)
	}
}

// TestALineCpybkcWroteItselfCarriesNoName is what the absence of a name means.
// docs/cli/SPEC.md makes it the whole of how a reader tells cpybkc's own line
// from a generator's, so a record with no generator attribute has to come out
// with nothing standing between the severity and the message.
func TestALineCpybkcWroteItselfCarriesNoName(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	log := logger(&out)

	log.Warn("the run is taking longer than usual")
	log.With(slog.String(generatorKey, "go")).Warn("this one is the generator's")

	want := "warning: the run is taking longer than usual\n" +
		"warning: go: this one is the generator's\n"

	if out.String() != want {
		t.Errorf("the log rendered as %q, want %q", out.String(), want)
	}
}

// TestAGroupedAttributeIsNotAGeneratorName covers the one way a name could be
// invented. Inside a group `generator` is a different key, and a line
// attributed to whatever happened to be called that in some nested group would
// name a generator that wrote nothing.
func TestAGroupedAttributeIsNotAGeneratorName(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	logger(&out).WithGroup("run").With(slog.String(generatorKey, "go")).Error("something failed")

	if got, want := out.String(), "error: something failed\n"; got != want {
		t.Errorf("the log rendered as %q, want %q", got, want)
	}
}

// TestEveryLevelMapsOntoTheClosedSetOfThree holds the mapping in both
// directions: docs/plugin/SPEC.md sends `error:` to error, `warning:` to
// warning and `note:` to info, and this is what a reader sees when each of them
// comes back.
func TestEveryLevelMapsOntoTheClosedSetOfThree(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		level slog.Level
		want  string
	}{
		{slog.LevelError, severityError},
		{slog.LevelError + 4, severityError},
		{slog.LevelWarn, severityWarning},
		{slog.LevelInfo, severityNote},
		{slog.LevelDebug, severityNote},
	} {
		if got := severityOf(tc.level); got != tc.want {
			t.Errorf("level %v renders as %q, want %q", tc.level, got, tc.want)
		}
	}
}

// lines is a stream as a reader reads it, with the trailing newline dropped.
func lines(stream string) []string {
	stream = strings.TrimSuffix(stream, "\n")
	if stream == "" {
		return nil
	}

	return strings.Split(stream, "\n")
}
