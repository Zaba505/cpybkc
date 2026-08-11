// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Command cpybkc is the executable a person runs.
//
//	cpybkc [--manifest <path>] [--emit-ir <dest> [--emit-ir-format <format>]]
//	cpybkc --version
//	cpybkc --help
//
// As it stands it is the outermost layer of that command and nothing behind it:
// it parses the argument vector, answers --help and --version, turns a fault
// into an exit status, and reports whatever failed. It reads no manifest,
// resolves no layout, emits no descriptor and starts no generator, and a run
// that asks for any of that fails with a diagnostic saying so. Finding the
// manifest and resolving it is #148, and --emit-ir is #149.
//
// What those stages will have to say for themselves already has a stream and a
// shape. Every fault reaches the user through [report], which renders the
// [github.com/Zaba505/cpybkc/internal/diag] diagnostics the readers of this
// repository raise — each under the file, line and column it is at, with the
// second file a cross-file fault implicates on a continuation line, and all of
// them rather than the first. A generator's own lines reach the same stream in
// the same shape through [logger].
//
// docs/cli/SPEC.md is the contract, and this command implements it rather than
// restating it. The command set, the argument vector, where the manifest is
// looked for, what arrives on each stream, the diagnostic format, the exit
// statuses and what --version prints are all that document's.
//
// # Why the outermost layer lands on its own
//
// Not tidiness, and not because the vector is the easy part. The published
// image's entrypoint is this CLI and its Cmd is empty
// (docs/container/SPEC.md), so the arguments in somebody's `docker run` line
// are the arguments above, and a flag renamed here breaks a Dockerfile in a
// repository this project cannot see. That surface is harder to change than the
// code behind it, so it is settled — and checked — on its own, against a
// document, rather than as a side effect of whichever story first needed a
// binary to exist.
//
// # Why main is three lines
//
// [run] is the whole program with the exit path taken out, and it takes its
// streams as parameters. Both are what let the argument vector be tested as a
// vector: a test drives a whole invocation, reads what landed on each stream
// and asserts the status, without ending the test binary and without either
// stream being this process's own. os.Exit appears once, in main, and the
// status it is handed is decided in exactly one place ([statusOf]).
package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
)

func main() {
	os.Exit(int(run(os.Args[1:], os.Stdout, os.Stderr)))
}

// run is one invocation: the argument vector in, the two streams written, and
// the exit status back.
//
// Standard output is the data channel and carries only what was asked for by
// name — the version line for --version, usage for --help, and the descriptor
// for `--emit-ir -`. Everything cpybkc says about a run goes to standard error.
// That is why a failure is reported here, where both streams are in hand,
// rather than by whatever raised it.
func run(args []string, stdout, stderr io.Writer) status {
	// The log a generator's output reaches this stream through is built here,
	// beside the stream, because docs/cli/SPEC.md's rule is about standard
	// error and not about any one stage: a generator's line and a layout's
	// fault are the same stream in the same shape, and the place that knows
	// which writer that is is the place that decides both.
	err := execute(args, stdout, logger(stderr))
	if err == nil {
		return statusOK
	}

	report(stderr, err)

	// docs/cli/SPEC.md: usage goes to standard error when it accompanies a
	// usage error. A vector cpybkc could not understand is the one failure a
	// caller can fix without knowing anything about the project, and the
	// command set is what they need in front of them to fix it.
	//
	// A blank line first, because usage is the one thing on this stream that is
	// not a diagnostic and nothing about it should read as one — a reader, and
	// a script scanning for `^error: `, both see where the diagnostics ended.
	if errors.As(err, new(*usageError)) {
		_, _ = io.WriteString(stderr, "\n")

		writeUsage(stderr)
	}

	return statusOf(err)
}

// execute performs what the line asked for, and writes to standard output only
// what was asked for by name.
func execute(args []string, stdout io.Writer, log *slog.Logger) error {
	inv, err := parse(args)
	if err != nil {
		return err
	}

	switch inv.answer {
	case answerHelp:
		writeUsage(stdout)

		return nil
	case answerVersion:
		_, _ = io.WriteString(stdout, versionLine()+"\n")

		return nil
	case answerRun:
		return perform(inv, log)
	}

	// Unreachable: parse returns one of the three answers above. It is a
	// failure rather than a panic because a fourth answer arriving here is a
	// bug in this program, and a bug is a run that failed.
	return errors.New("cpybkc did not understand what its own parser asked for, which is a bug in cpybkc")
}

// perform is the run itself, and it is the half of this command that #148,
// #149 and #150 build.
//
// It fails rather than succeeding silently. A scaffold that exited 0 having
// generated nothing would be a binary that reports success to whatever CI step
// runs it — silence is success by docs/cli/SPEC.md's own rule for a generating
// run, so an unwired pipeline exiting 0 is indistinguishable from a project
// whose generators all ran. Status 1 with a diagnostic naming the story is the
// honest answer, and it is a status the document already enumerates.
//
// log is where a generator's output reaches standard error, in the form
// docs/cli/SPEC.md fixes for a relayed line. It is
// [github.com/Zaba505/cpybkc/internal/plugin.Runner]'s Log, and it arrives here
// rather than being built where the runner is because the writer it renders to
// is [run]'s standard error and not a stream this stage is entitled to choose.
// Nothing in this build starts a generator, so nothing in this build writes
// through it yet; #148 is what does.
func perform(inv invocation, log *slog.Logger) error {
	if inv.emitting() {
		return fmt.Errorf("this build cannot write the %s descriptor %s asked for: it parses the command "+
			"line and answers %s and %s, and it has not read %s (#149)",
			inv.emitIRFormat, emitIRFlag, versionFlag, helpFlag, inv.manifestPath())
	}

	return fmt.Errorf("this build generates nothing: it parses the command line and answers %s and %s, "+
		"and it has not read %s (#148)", versionFlag, helpFlag, inv.manifestPath())
}
