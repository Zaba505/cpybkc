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
// It parses the argument vector, answers --help and --version, finds the
// project's manifest, and runs every generator that manifest names over the one
// descriptor the layout and its copybooks resolve to. Then it turns whatever
// happened into an exit status and reports it. `--emit-ir` is the one thing on
// the vector this build does not do: a descriptor is resolved for every run, but
// writing one where the flag names is #149.
//
// # What is here, and what is not
//
// This file composes and reports, and nothing in it is pipeline behaviour.
// Reading the manifest, resolving the layout against the copybooks it names and
// assembling the descriptor are
// [github.com/Zaba505/cpybkc/internal/project]'s; finding an executable on PATH
// is [github.com/Zaba505/cpybkc/internal/plugin]'s; giving each generator a
// scratch directory, merging what they produced atomically, refusing two that
// collide over one path and pruning what a previous run generated are
// [github.com/Zaba505/cpybkc/internal/generate]'s. What is decided here is the
// order those run in, the two streams they report on, and the status a caller
// reads.
//
// Every fault reaches the user through [report], which renders the
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
// # Why the vector was settled before the pipeline behind it
//
// Not tidiness, and not because the vector is the easy part. The published
// image's entrypoint is this CLI and its Cmd is empty
// (docs/container/SPEC.md), so the arguments in somebody's `docker run` line
// are the arguments above, and a flag renamed here breaks a Dockerfile in a
// repository this project cannot see. That surface is harder to change than the
// code behind it, so it was settled — and checked — on its own, against a
// document, rather than as a side effect of whichever story first needed a
// binary to exist.
//
// # Why main is four lines
//
// [run] is the whole program with the exit path taken out, and it takes its
// streams and its context as parameters. All three are what let a whole
// invocation be tested as one: a test drives a run, reads what landed on each
// stream and asserts the status, without ending the test binary, without either
// stream being this process's own, and without signalling anything. os.Exit
// appears once, in main, and the status it is handed is decided in exactly one
// place ([statusOf]).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/Zaba505/cpybkc/internal/generate"
	"github.com/Zaba505/cpybkc/internal/plugin"
	"github.com/Zaba505/cpybkc/internal/project"
)

func main() {
	// The context a run is bounded by is built here, at the top, because
	// cancellation is a property of the process and not of any one stage:
	// docs/cli/SPEC.md requires SIGINT and SIGTERM to stop the run, leave the
	// project's output tree exactly as it was found, remove the scratch
	// directories and exit 1. Everything below takes the context and knows
	// nothing about signals.
	ctx, stop := cancellable(context.Background())
	defer stop()

	os.Exit(int(run(ctx, os.Args[1:], os.Stdout, os.Stderr)))
}

// run is one invocation: the argument vector in, the two streams written, and
// the exit status back.
//
// Standard output is the data channel and carries only what was asked for by
// name — the version line for --version, usage for --help, and the descriptor
// for `--emit-ir -`. Everything cpybkc says about a run goes to standard error.
// That is why a failure is reported here, where both streams are in hand,
// rather than by whatever raised it.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) status {
	// The log a generator's output reaches this stream through is built here,
	// beside the stream, because docs/cli/SPEC.md's rule is about standard
	// error and not about any one stage: a generator's line and a layout's
	// fault are the same stream in the same shape, and the place that knows
	// which writer that is is the place that decides both.
	err := execute(ctx, args, stdout, logger(stderr))
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
func execute(ctx context.Context, args []string, stdout io.Writer, log *slog.Logger) error {
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
		return perform(ctx, inv, log)
	}

	// Unreachable: parse returns one of the three answers above. It is a
	// failure rather than a panic because a fourth answer arriving here is a
	// bug in this program, and a bug is a run that failed.
	return errors.New("cpybkc did not understand what its own parser asked for, which is a bug in cpybkc")
}

// perform is the run itself: the manifest, the layout it names, the copybooks
// that layout names, and every generator the manifest asks for, run over the one
// descriptor they resolve to.
//
// There is no pipeline behaviour here. Locating and reading the manifest,
// resolving the layout against its copybooks and assembling the descriptor are
// [github.com/Zaba505/cpybkc/internal/project]'s; finding a generator on PATH is
// [github.com/Zaba505/cpybkc/internal/plugin]'s; giving each one a scratch
// directory, merging what they produced atomically, refusing two that collide
// and pruning what a previous run generated are
// [github.com/Zaba505/cpybkc/internal/generate]'s. What is decided here is the
// order, the environment those stages are given, and that every fault comes back
// as an error for [run] to report and [statusOf] to turn into a status.
//
// Nothing is written to standard output. Silence is success for a generating
// run by docs/cli/SPEC.md's own rule, and the exit status is the verdict.
//
// log is where a generator's output reaches standard error, in the form
// docs/cli/SPEC.md fixes for a relayed line. It is
// [github.com/Zaba505/cpybkc/internal/plugin.Runner]'s Log, and it arrives here
// rather than being built where the runner is because the writer it renders to
// is [run]'s standard error and not a stream this stage is entitled to choose.
// It is also [github.com/Zaba505/cpybkc/internal/generate.Runner]'s, which is
// how a file this run removed from a person's tree reaches the same stream: that
// line carries no generator name, and the absence of one is what says it is
// cpybkc's own.
func perform(ctx context.Context, inv invocation, log *slog.Logger) error {
	if inv.emitting() {
		return fmt.Errorf("this build cannot write the %s descriptor %s asked for: it resolves one and "+
			"generates from it, and writing one where %s names is #149",
			inv.emitIRFormat, emitIRFlag, emitIRFlag)
	}

	run, err := project.Load(inv.manifestPath())
	if err != nil {
		return err
	}

	// docs/cli/SPEC.md: PATH is one of the two environment variables a run
	// reads, and it is read here rather than inside the search so that the
	// search stays a function of its arguments.
	generators, err := run.Generators(os.Getenv("PATH"))
	if err != nil {
		return err
	}

	runner := &generate.Runner{
		Plugins: &plugin.Runner{Log: log},

		// The project's root is the directory holding its manifest, and it is
		// the one place the answer exists: a run that has not been told where
		// the root is prunes nothing, and a wrong guess is a run that deletes
		// something a person wrote.
		Root: run.Dir,
		Log:  log,
	}

	// TMPDIR is the other variable a run reads, and it is left to the standard
	// library to read: os.MkdirTemp already honours it, and naming it here
	// would be a second answer to where a run's scratch space goes.
	return cancelled(ctx, runner.Run(ctx, run.Descriptor, generators))
}
