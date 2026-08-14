// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// Door is how an adapter process is started.
//
// docs/adapter/SPEC.md leaves the argument vector, the working directory, the
// environment and whether the process is local at all out of the contract, on
// the grounds that they are the door's rather than the contract's: one engine
// runs a command and another spawns a container, and both drive one
// implementation of the conversation precisely because the contract begins
// after the process exists. This interface is that seam, and [Command] and
// [Image] are the two doors this package ships through it (#203).
type Door interface {
	// Open starts one adapter process. A door that cannot start one at all is
	// an error, and it costs the run: there is no conversation to have.
	Open(ctx context.Context) (Process, error)

	// Describe is what this door adds, for the report to quote.
	//
	// It is the door's own words and not the engine's, because an engine MUST
	// NOT report a result as though it carried a guarantee its door did not
	// provide — network isolation, a read-only root and a wall-clock cap are
	// properties of a door and never of the contract, and a run through a door
	// that provided none of them is the author's own working result rather than
	// one they can hand to somebody else.
	Describe() string
}

// Process is one started adapter: the two streams the conversation runs over,
// what it wrote to standard error, and the two ways it ends.
type Process interface {
	// Stdin is where request frames are written. Closing it is end of input,
	// which an adapter treats as a bye it has already answered.
	Stdin() io.WriteCloser

	// Stdout is where response frames are read.
	Stdout() io.Reader

	// Diagnostics is whatever the door captured of the adapter's standard
	// error, which an engine SHOULD quote beside a fault and MUST NOT parse. It
	// is read after the process has ended.
	Diagnostics() string

	// Wait waits for the process to exit and reports a non-zero exit as an
	// error. It is called once.
	Wait() error

	// Kill ends the process without waiting for it to be polite. An adapter
	// killed by the engine is not in violation of the contract and is under no
	// obligation to catch a signal, unwind, or write anything on its way out.
	Kill()
}

// Command is the door that runs a command.
//
// It is the low bar docs/adapter/SPEC.md sets deliberately: the first adapter
// somebody writes can be a shell script that pipes lines through a program they
// already have, and a contract whose smallest conforming implementation needed
// a Dockerfile and a registry would be one most people evaluate by reading
// about it. What it does not add is isolation of any kind, and [Command.Describe]
// says so, because a result is only as believable as the door that produced it.
type Command struct {
	// Path is the executable to run. It is a path rather than a name to look up,
	// for the reason
	// [github.com/Zaba505/cpybkc/internal/conformance/goadapter.Adapter] names
	// its generator by one: a run is usually against something just built from
	// the tree under test, and resolving a name would find whichever one the
	// author happened to have installed.
	Path string

	// Args are the arguments after the executable, which are the door's and not
	// the contract's.
	Args []string

	// Dir is the working directory, and an empty one is the engine's own.
	Dir string

	// Env is the environment, in [os/exec.Cmd.Env]'s form. Nil is this
	// process's environment.
	Env []string
}

// Open starts the command.
func (c *Command) Open(ctx context.Context) (Process, error) {
	if c.Path == "" {
		return nil, fmt.Errorf("the door has no executable to run")
	}

	// CommandContext rather than Command so that a cancelled run kills the
	// adapter: the conversation's own deadlines bound each operation, and this
	// bounds the whole run against a caller that gave up on it.
	cmd := exec.CommandContext(ctx, c.Path, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = c.Env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to open the adapter's standard input: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to open the adapter's standard output: %w", err)
	}

	// Standard error is free-form and this engine never parses it. It is
	// captured because a fault is much easier to read beside whatever the
	// adapter said as it happened — which, for an adapter that redirected its
	// own standard output the way the contract recommends, is where every stray
	// print in its dependencies ends up too.
	diagnostics := &diagnostics{}
	cmd.Stderr = diagnostics

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start the adapter %s: %w", c.Path, err)
	}

	return &process{cmd: cmd, stdin: stdin, stdout: stdout, diagnostics: diagnostics}, nil
}

// Describe says what this door provides, which is nothing beyond a process.
func (c *Command) Describe() string {
	return fmt.Sprintf("the command %s, run directly: no network isolation, no read-only root and no resource cap",
		strings.Join(append([]string{c.Path}, c.Args...), " "))
}

// process is a running command, as the conversation sees it.
type process struct {
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      io.Reader
	diagnostics *diagnostics
}

func (p *process) Stdin() io.WriteCloser { return p.stdin }
func (p *process) Stdout() io.Reader     { return p.stdout }
func (p *process) Diagnostics() string   { return p.diagnostics.String() }
func (p *process) Wait() error           { return p.cmd.Wait() }

// Kill ends the process. The error is not reported: the only one it has is that
// the process has already exited, which is the outcome being asked for.
func (p *process) Kill() {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}

// diagnosticsLimit is how much of an adapter's standard error is kept.
//
// It is bounded because an adapter that has gone wrong is exactly the one that
// prints without stopping, and the report it would otherwise fill is the thing
// somebody has to read to find out what it did. The first bytes are kept rather
// than the last: what a broken adapter says first is usually what broke it, and
// what it says afterwards is usually the same line again.
const diagnosticsLimit = 64 << 10

// diagnostics is the bounded capture of one adapter's standard error.
//
// It is written by the goroutine os/exec runs for the stream and read by the
// engine after the process has ended, which are different goroutines with no
// happens-before between them but the mutex.
type diagnostics struct {
	mu   sync.Mutex
	held bytes.Buffer
	over bool
}

func (d *diagnostics) Write(b []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if room := diagnosticsLimit - d.held.Len(); room > 0 {
		d.held.Write(b[:min(room, len(b))])
	}

	if d.held.Len() >= diagnosticsLimit {
		d.over = true
	}

	// Every byte is reported as written whether or not it was kept. A short
	// write would stop os/exec copying the stream, which would block the
	// adapter on its own standard error — a hang caused by this engine having
	// decided it had read enough.
	return len(b), nil
}

func (d *diagnostics) String() string {
	d.mu.Lock()
	defer d.mu.Unlock()

	held := d.held.String()
	if d.over {
		held += "\n(the rest of the adapter's standard error was not kept)"
	}

	return held
}
