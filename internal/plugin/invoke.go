// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/emit"
	"github.com/Zaba505/cpybkc/irpb"
)

// The flags of the argument vector, spelled once.
//
// docs/plugin/SPEC.md, "Invocation": the vector is
// `cpybkc-gen-<name> --descriptor <path> --out <dir> [--opt k=v ...]` and
// nothing else. They are constants because each name is written by the code
// that builds the vector and read by the test that pins it, and a vector the
// tests agree with and a plugin does not is the one failure this contract
// cannot afford.
const (
	descriptorFlag = "--descriptor"
	outFlag        = "--out"
	optFlag        = "--opt"
)

// descriptorFile is what the descriptor is called inside the directory written
// for one invocation.
//
// docs/plugin/SPEC.md: a plugin MUST NOT derive anything from the file's name
// or its directory, so this name is implementation and not contract. It is
// spelled to be recognisable in an strace or a `ps` line all the same, because
// the one reader it is for is whoever is working out what cpybkc handed a
// generator.
const descriptorFile = "descriptor.bin"

// descriptorDirPattern is the prefix [os.MkdirTemp] builds the per-invocation
// directory's name from.
//
// It names cpybkc because the directory is made beside the invocation's output
// directory rather than in a temporary directory the operating system would
// eventually sweep, so a process killed outright leaves it where a person will
// find it — and what a person finds has to say what left it. The leading dot is
// [github.com/Zaba505/cpybkc/internal/generate]'s scratch pattern's: a survivor
// of a killed run is not something the go tool should try to compile.
const descriptorDirPattern = ".cpybkc-descriptor-"

// Option is one option a generator is invoked with, which reaches it as a
// single `--opt k=v` argument.
//
// It is this package's own rather than
// [github.com/Zaba505/cpybkc/internal/manifest.Option], for the reason
// [Resolve] takes a name rather than a manifest entry: an option reaches an
// invocation from wherever the caller had one, and a package that ran
// generators only for callers holding a parsed cpybkc.json could not be driven
// by a test or by a flag. The manifest is where a project's options are
// written, and it is the caller that carries them across.
type Option struct {
	// Key is the option's name, which docs/plugin/SPEC.md requires to be
	// non-empty and to contain no `=` — everything before the first `=` of the
	// argument is the key, so a key carrying one would be split somewhere the
	// manifest did not write.
	Key string

	// Value is what is passed for it. It may be empty and may contain further
	// `=` characters, both of which the contract allows.
	Value string
}

// argument is the option as it appears in the vector.
func (o Option) argument() string { return o.Key + "=" + o.Value }

// Invocation is one generator to run: the executable [Resolve] found, under the
// name it was asked for, with the directory it writes into and the options it
// was declared with.
type Invocation struct {
	// Name is the generator's name — the `<name>` a manifest asked for. It is
	// what every diagnostic about this invocation names, and what the lines the
	// generator writes are attributed to.
	Name string

	// Path is the executable to run, as [Resolve] spelled it.
	Path string

	// Out is the directory this generator writes its output into, which
	// reaches it as `--out`.
	//
	// It is the caller's: creating the scratch directory, enforcing that
	// nothing is written outside it and merging what is left in it are #43's
	// and #44's, and an invocation that created its own would be a second
	// answer to where a generator's output goes. What happens here is only that
	// the path is made absolute, because docs/plugin/SPEC.md requires the
	// vector to carry one.
	//
	// It is also where this package puts the directory holding the invocation's
	// descriptor: beside it, in its parent, for the whole of the invocation and
	// no longer (#184). That makes the descriptor's location a function of the
	// one directory an invocation cannot be run without, so a [Runner] has
	// nothing to be told about where temporary files go and no zero value of
	// one can reach an ambient directory. It costs the caller nothing it was
	// not already deciding: whoever chose a scratch directory for a generator
	// chose the tree the descriptor lands in at the same moment.
	Out string

	// Options are the options to pass, in the order they are to be passed —
	// which docs/plugin/SPEC.md fixes as the order the manifest declares them,
	// so that the vector is a function of the manifest rather than of a map
	// iteration.
	Options []Option
}

// Runner runs generators.
//
// The zero value runs them: the process's own environment is passed through,
// each invocation's descriptor directory is made beside the directory that
// invocation writes into, and what the generators write is surfaced through
// [slog.Default].
type Runner struct {
	// Log is where a generator's output is surfaced. A nil Log is
	// [slog.Default] read at the moment of the run, so that a caller that
	// configures the default logger after building a Runner is still heard.
	Log *slog.Logger

	// Env is the environment every generator is started with, in
	// [os/exec.Cmd.Env]'s form. Nil is this process's environment, which is
	// what docs/plugin/SPEC.md, "The environment", requires: cpybkc passes its
	// own environment through unchanged, and the propagation of
	// SOURCE_DATE_EPOCH that determinism rests on (#47) is that pass-through
	// rather than a variable this package names.
	//
	// It is a field rather than something read here so that a test can state an
	// environment instead of moving the one the test binary runs in; the same
	// reasoning makes [Resolve] take a PATH.
	Env []string
}

// Run runs every invocation against d, concurrently, and reports every one that
// failed.
//
// docs/plugin/SPEC.md, "Invocation": each generator is started once, with
// `--descriptor <path> --out <dir>` and one `--opt k=v` per declared option in
// the declared order, both paths absolute and no other argument. The descriptor
// is [github.com/Zaba505/cpybkc/internal/emit.Marshal]'s bytes — the same
// function --emit-ir writes, so that what a plugin is handed and what an author
// captured for the same inputs are byte-identical by construction rather than
// by two encoders agreeing.
//
// Every generator is run, even after one has failed. Nothing reaches the
// project's output tree until all of them have succeeded (#43, #44), so
// stopping early would buy nothing back and would make the set of failures a
// user is shown depend on which generator lost a race — a run reports what is
// wrong with it, in the order the invocations were declared, however many
// things that is.
//
// A generator that exits non-zero is an [ExitError] naming it, and one killed
// by a signal is a [SignalError], which is the distinction docs/plugin/SPEC.md
// requires: the first is a bug in the generator and the second is usually the
// run being cancelled or the machine running out of memory. A generator that
// exits zero after writing an `error:` diagnostic has succeeded — the exit
// status is the verdict and the diagnostics are the explanation.
//
// The context bounds the run: cancelling it kills every generator still
// running, which surfaces as the signal that killed them.
func (r *Runner) Run(ctx context.Context, d *irpb.Descriptor, invocations []Invocation) error {
	if len(invocations) == 0 {
		return nil
	}

	// Every invocation is checked before any generator starts. An option key
	// that cannot be half of `k=v`, or a name that cannot be a filename, is a
	// fault in what the caller assembled rather than something a generator
	// could report — and unlike an unrecognised key, which only the plugin
	// knows about (docs/plugin/SPEC.md, "Options"), it is knowable here.
	var invalid diag.List

	for _, invocation := range invocations {
		invalid.Fail(invocation.check())
	}

	if invalid.Failed() {
		return invalid.Err()
	}

	descriptor, err := emit.Marshal(d)
	if err != nil {
		return err
	}

	// Indexed rather than appended to under a lock: the faults are reported in
	// the order the invocations were declared, and an order that depended on
	// which generator finished first would make the same failing run read
	// differently twice.
	faults := make([]error, len(invocations))

	var running sync.WaitGroup

	for i, invocation := range invocations {
		running.Add(1)

		go func() {
			defer running.Done()

			faults[i] = r.invoke(ctx, invocation, descriptor)
		}()
	}

	running.Wait()

	var failed diag.List

	for _, fault := range faults {
		failed.Fail(fault)
	}

	return failed.Err()
}

// invoke runs one generator to completion and reports how it ended.
func (r *Runner) invoke(ctx context.Context, invocation Invocation, descriptor []byte) error {
	// docs/plugin/SPEC.md requires --out to be passed absolute, and this is
	// where a relative one written in a manifest becomes so. It is resolved
	// before the descriptor is written rather than after, so that a failure
	// here is reported as what it is and leaves nothing behind to remove: the
	// only way it fails is the process having no working directory to resolve
	// against, which is not something a descriptor could be at fault for.
	out, err := filepath.Abs(invocation.Out)
	if err != nil {
		return fmt.Errorf("the generator %s could not be given an absolute path to %s: %w",
			quote(invocation.Name), invocation.Out, err)
	}

	// docs/plugin/SPEC.md, "The descriptor's location and lifetime": the
	// descriptor goes in a directory cpybkc creates for this one invocation and
	// nothing else, is never shared between two generators or two runs, and is
	// removed with its directory once the generator has exited — whether it
	// exited zero or not. One directory per invocation is what makes the bytes
	// attributable; the removal is the whole of the file's lifetime.
	//
	// It is made beside the output directory rather than in whatever the system
	// calls its temporary directory (#184). The parent of an absolute --out is
	// a directory the caller already had to make, so cpybkc needs no writable
	// /tmp, reads no TMPDIR, and a Runner nobody configured cannot reach an
	// ambient directory. It is a *sibling* of the output directory and not a
	// child of it, because the one thing docs/plugin/SPEC.md promises about
	// --out is that the generator finds it empty.
	dir, err := os.MkdirTemp(filepath.Dir(out), descriptorDirPattern)
	if err != nil {
		return &DescriptorError{Name: invocation.Name, Err: err}
	}

	defer func() {
		// The removal is not reported. It happens after the generator has
		// exited, so there is nothing left to fail, and a run that succeeded
		// and could not tidy up afterwards has not failed at anything the user
		// asked for.
		_ = os.RemoveAll(dir)
	}()

	path, err := writeDescriptor(dir, descriptor)
	if err != nil {
		return &DescriptorError{Name: invocation.Name, Err: err}
	}

	return r.run(ctx, invocation, invocation.arguments(path, out))
}

// outputGrace is how long a generator's output is waited for once there is no
// generator left to write it.
//
// It is [os/exec.Cmd.WaitDelay], and what it bounds is a plugin that started
// something of its own and exited without waiting for it: the child inherits
// the pipes, so the read end stays open and a run would otherwise wait for a
// process it never started and cannot see. It also bounds the same delay after
// a cancelled run, where the generator has been killed and its own child has
// not.
//
// Five seconds because the delay is only ever reached by a plugin that has
// already misbehaved, and the cost of reaching it is paid once per such plugin;
// a shorter grace would start truncating the output of a generator writing a
// long explanation as it exits.
const outputGrace = 5 * time.Second

// run starts the generator, surfaces what it writes, and waits for it.
func (r *Runner) run(ctx context.Context, invocation Invocation, args []string) error {
	cmd := exec.CommandContext(ctx, invocation.Path, args...)
	cmd.Env = r.Env

	// Standard input is unused: docs/plugin/SPEC.md gives it a meaning only for
	// `--descriptor -`, which cpybkc never emits. A nil Stdin is the empty
	// file, so a generator that reads it anyway sees end of file rather than
	// whatever cpybkc was started with.
	cmd.Stdin = nil

	// The streams are pipes of this package's own rather than
	// [os/exec.Cmd.StdoutPipe]'s, and the difference is what happens when a
	// generator leaves a child holding its output. Wait is what closes the
	// pipes it hands out, and it is also what waits for the reads — so reading
	// those to end of file before calling Wait deadlocks against a grandchild
	// nobody is going to kill. With a pipe of ours, Wait bounds itself with
	// [outputGrace], and the reads end when this ends them.
	out, outWriter := io.Pipe()
	errs, errWriter := io.Pipe()

	cmd.Stdout = outWriter
	cmd.Stderr = errWriter
	cmd.WaitDelay = outputGrace

	log := r.logger().With(slog.String("generator", invocation.Name))

	// Both streams are read as the generator writes them, because
	// docs/plugin/SPEC.md requires a line to be surfaced when it is written
	// rather than held back until the process exits.
	var streams sync.WaitGroup

	streams.Add(2)

	go func() {
		defer streams.Done()

		surface(ctx, log, stdoutStream, out)
	}()

	go func() {
		defer streams.Done()

		surface(ctx, log, stderrStream, errs)
	}()

	if err := cmd.Start(); err != nil {
		closeStreams(outWriter, errWriter)
		streams.Wait()

		return &StartError{Name: invocation.Name, File: invocation.Path, Err: err}
	}

	waited := cmd.Wait()

	closeStreams(outWriter, errWriter)
	streams.Wait()

	// The generator exited zero and something it started was still holding the
	// pipes when the grace ran out. The exit status is the verdict, so that is
	// not a failed run (docs/plugin/SPEC.md, "Exit codes and diagnostics") —
	// but the output is short by however much that child had not written, and a
	// user reading an explanation with the end missing deserves to know which.
	if errors.Is(waited, exec.ErrWaitDelay) {
		log.LogAttrs(ctx, slog.LevelWarn,
			"this generator exited while something it started still held its output open; the rest of that output was not waited for")

		return nil
	}

	return failure(invocation, waited)
}

// closeStreams ends the reads, which is what makes the surfacing goroutines
// return once there is nothing left to write into them.
func closeStreams(writers ...*io.PipeWriter) {
	for _, w := range writers {
		// The only error [io.PipeWriter.Close] reports is that it was already
		// closed, which it was not.
		_ = w.Close()
	}
}

// failure reads what [os/exec.Cmd.Wait] said as the verdict on an invocation.
func failure(invocation Invocation, err error) error {
	if err == nil {
		return nil
	}

	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		// Not the generator's verdict but a failure to collect it — a pipe that
		// could not be closed, a process that could not be reaped. It is still
		// a failed invocation and still names the generator, because that is
		// what the user has to act on.
		return fmt.Errorf("the generator %s could not be waited for: %w", quote(invocation.Name), err)
	}

	// docs/plugin/SPEC.md, "Exit codes and diagnostics": a generator terminated
	// by a signal MUST be reported as terminated by that signal, and
	// distinguishably from one that exited non-zero. [os/exec.ExitError.ExitCode]
	// flattens the two into -1, so the wait status is read instead. It is a
	// POSIX wait status because cpybkc targets POSIX hosts and nothing else;
	// see docs/plugin/SPEC.md, "Host platform".
	if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return &SignalError{Name: invocation.Name, File: invocation.Path, Signal: status.Signal()}
	}

	return &ExitError{Name: invocation.Name, File: invocation.Path, Code: exit.ExitCode()}
}

// logger is where this run's output goes.
func (r *Runner) logger() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}

	return slog.Default()
}

// writeDescriptor puts the descriptor in dir and hands back the absolute path
// the generator is passed.
//
// docs/plugin/SPEC.md: the file MUST be written in full and closed before the
// generator is started, so that a plugin never observes a partial descriptor
// and needs no protocol to find out whether the bytes it can see are all of
// them. [os.WriteFile] is that, and the mode is the SHOULD beside it — a
// read-only file does not enforce the prohibition on writing to the descriptor,
// which a plugin running as the file's owner can undo, but it turns the
// accidental case into an error where the mistake is rather than into a
// descriptor quietly different from the one cpybkc wrote.
func writeDescriptor(dir string, descriptor []byte) (string, error) {
	path, err := filepath.Abs(filepath.Join(dir, descriptorFile))
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(path, descriptor, 0o444); err != nil {
		return "", err
	}

	return path, nil
}

// arguments is the vector the generator is run with.
//
// docs/plugin/SPEC.md: `--descriptor` and `--out` appear exactly once each,
// `--opt` once per declared option in the declared order, every flag in the
// separated form, and nothing else at all — no operand, and nothing taken from
// the environment. The vector is therefore a function of this invocation, which
// is what makes two runs of the same generator from different directories the
// same invocation.
func (inv Invocation) arguments(descriptor, out string) []string {
	args := make([]string, 0, 4+2*len(inv.Options))
	args = append(args, descriptorFlag, descriptor, outFlag, out)

	for _, option := range inv.Options {
		args = append(args, optFlag, option.argument())
	}

	return args
}

// check refuses an invocation that cannot be run as written.
func (inv Invocation) check() error {
	if err := checkName(inv.Name); err != nil {
		return err
	}

	for _, option := range inv.Options {
		if option.Key == "" || strings.Contains(option.Key, "=") {
			return &InvalidOptionError{Name: inv.Name, Key: option.Key}
		}
	}

	// Neither of these is an adopter's mistake — a manifest cannot express an
	// invocation with no executable to run or nowhere to write — so they are
	// plain errors rather than diagnostics. What they are is the caller having
	// assembled an invocation it never resolved.
	if inv.Path == "" {
		return fmt.Errorf("the generator %s has no executable to run; resolve the name first", quote(inv.Name))
	}

	if inv.Out == "" {
		return fmt.Errorf("the generator %s has no directory to write into", quote(inv.Name))
	}

	return nil
}
