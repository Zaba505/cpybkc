// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// The image door's defaults, which are what a run that asked for nothing in
// particular gets.
const (
	// DefaultRuntime is the container runtime the image door drives. It is a
	// name looked up on PATH rather than a path — the opposite of
	// [Command.Path]'s rule, and for the opposite reason: the adapter is
	// usually something just built from the tree under test, where finding
	// "whichever one is installed" is the failure, and a container runtime is a
	// system tool where finding the installed one is the whole point. `podman`
	// is the other value this door is written against; both spell every flag
	// below the same way.
	DefaultRuntime = "docker"

	// DefaultImageTimeout is the wall-clock cap on one adapter process started
	// through the image door.
	//
	// It is longer than [DefaultBuildDeadline] on purpose, and by a wide margin.
	// The engine's own deadlines bound one operation, and the longest of them is
	// generate, which may run a compiler over the whole corpus; a wall-clock cap
	// shorter than that would kill a container in the middle of a legal build
	// and report every entry as a fault — an adapter failing the corpus because
	// the door it came through would not wait for it.
	DefaultImageTimeout = 30 * time.Minute

	// DefaultMemory is the memory cap, in the runtime's own notation. Swap is
	// pinned to the same number, so the cap is a cap rather than a threshold at
	// which the run starts swapping.
	DefaultMemory = "2g"

	// DefaultProcesses is the process cap, which is what stops a broken adapter
	// forking until the host stops responding.
	DefaultProcesses = 256

	// DefaultScratch is the size of the writable /tmp the door mounts.
	//
	// A read-only root with nowhere to write at all would refuse most real
	// adapters: a generator emits code and something compiles it, and both want
	// a directory. One tmpfs, in memory, inside the container's own mount
	// namespace, is the smallest thing that lets that work while leaving nothing
	// behind and touching no host path.
	DefaultScratch = "1g"
)

// removeGrace bounds the removal of a container the door is done with. It is
// short because nothing waits on the answer: the container is being taken away,
// and a runtime that will not answer in this long is a fact about the host
// rather than about the run.
const removeGrace = 30 * time.Second

// Image is the door that runs an adapter from a container image.
//
// It is [Command]'s sibling and not its replacement, and the difference between
// them is the whole of why both exist. Both drive one implementation of the
// conversation — the contract begins after the process exists
// (docs/adapter/SPEC.md, "A process is the unit, and a container is a door onto
// it"), so this door is a matter of building an argument vector and handing it
// to the same [Command] behind it.
//
// What this door adds is everything that makes a result believable to somebody
// who did not produce it: no network, a read-only root, a memory cap, a process
// cap and a wall-clock bound on the whole conversation. None of those is a
// property of the contract, which is why they are stated by [Image.Describe]
// and quoted into the report rather than assumed of every run. A result
// produced through [Command] is the author's own working result; a result
// produced through this door is one they can hand to somebody else.
//
// Neither is a conformance claim a third party should be asked to trust
// without qualification: a run computed on the claimant's machine, against a
// corpus they downloaded, by a program they built, is a self-report whichever
// door produced it. That is a fine thing to publish, labelled as one.
type Image struct {
	// Reference is the image to run, in whatever notation the runtime accepts.
	// It is required.
	//
	// The image's entrypoint is the adapter: this door starts the image and
	// speaks the contract to it, and what runs inside is the image's business
	// exactly as the executable is [Command]'s.
	Reference string

	// Args are the arguments after the image reference, which the runtime hands
	// to the container as its own argument vector. They are the door's and not
	// the contract's, exactly as [Command.Args] are.
	Args []string

	// Runtime is the container runtime, as a name looked up on PATH. Empty is
	// [DefaultRuntime].
	Runtime string

	// Env is the environment of the *runtime client* — the `docker` or `podman`
	// process this door starts — and nil is this process's own. It is how a run
	// reaches a daemon that is not the default one.
	//
	// It is emphatically not the container's environment, and nothing here
	// passes one to the other. An adapter that could read the host's
	// environment would be an adapter whose answers depended on the machine it
	// ran on, which is the property this door exists to remove.
	Env []string

	// Timeout is the wall-clock cap on one adapter process. Zero is
	// [DefaultImageTimeout].
	//
	// It bounds the container rather than an operation: the engine's own
	// deadlines already bound every request and kill an adapter that misses
	// one, and this is what bounds an adapter that is answering promptly and
	// forever, or one whose runtime never handed the engine the process it
	// thought it had.
	Timeout time.Duration

	// Memory is the memory cap in the runtime's notation, and empty is
	// [DefaultMemory]. Processes is the process cap, and a value below one is
	// [DefaultProcesses]. Scratch is the size of the writable /tmp, and empty is
	// [DefaultScratch].
	//
	// They are settable rather than fixed because a cap with no way past it is
	// a cap people get past by not using the door at all, and this door is the
	// one worth using. What keeps that honest is that [Image.Describe] reports
	// the numbers actually used, so a report can never claim a bound the run did
	// not have.
	Memory    string
	Processes int
	Scratch   string
}

// Open starts one container and hands back the conversation with it.
func (i *Image) Open(ctx context.Context) (Process, error) {
	if i.Reference == "" {
		return nil, fmt.Errorf("the door has no image to run")
	}

	// Resolved here rather than left to os/exec, so that a machine with no
	// container runtime on it says so before a container name is minted and a
	// deadline is armed — and says which name it looked for.
	path, err := exec.LookPath(i.runtime())
	if err != nil {
		return nil, fmt.Errorf("failed to find the container runtime %s: %w", i.runtime(), err)
	}

	name, err := containerName()
	if err != nil {
		return nil, err
	}

	// The wall clock, armed before the process exists so that a runtime which
	// hangs before it has started anything is bounded too.
	ctx, cancel := context.WithTimeout(ctx, i.timeout())

	inner, err := (&Command{Path: path, Args: i.argv(name), Env: i.Env}).Open(ctx)
	if err != nil {
		cancel()

		return nil, fmt.Errorf("failed to run the image %s: %w", i.Reference, err)
	}

	return &container{
		Process: inner,
		runtime: path,
		env:     i.Env,
		name:    name,
		timeout: i.timeout(),
		ctx:     ctx,
		cancel:  cancel,
	}, nil
}

// Describe says what this door provides, in the numbers this run actually used.
func (i *Image) Describe() string {
	said := fmt.Sprintf("the image %s, run by %s with no network, a read-only root", i.Reference, i.runtime())

	return fmt.Sprintf("%s and a %s tmpfs at /tmp, capped at %s of memory and %d processes, "+
		"under a %s wall-clock bound", said, i.scratch(), i.memory(), i.processes(), i.timeout())
}

// argv is the runtime's argument vector for one container.
//
// Every flag is written joined to its value, which makes the image reference
// the first argument after `run` that does not begin with a dash. That is worth
// having: the reference and everything after it belong to the container, and a
// vector where the boundary can be read off is one a reader of a failing run
// can check by eye.
func (i *Image) argv(name string) []string {
	args := []string{
		"run",

		// Removed when it exits, and named so that it can be removed when it
		// does not. See [container.remove].
		"--rm",
		"--name=" + name,

		// Standard input attached, and deliberately no TTY. A pseudo-terminal
		// echoes what is written to it and rewrites line endings, and the
		// conversation is newline-delimited JSON in both directions: a TTY would
		// feed the engine's own request frames back to it as though the adapter
		// had sent them.
		"--interactive",

		// The guarantees. An adapter has no reason to reach the network — the
		// bytes travel in the frame and never as a path (docs/adapter/SPEC.md,
		// "The bytes travel in the frame, and not as a path") — so a door that
		// removes the network removes with it every way a result could depend on
		// something that was not in the corpus.
		"--network=none",
		"--read-only",
		"--tmpfs=/tmp:rw,nosuid,nodev,size=" + i.scratch(),
		"--memory=" + i.memory(),
		"--memory-swap=" + i.memory(),
		"--pids-limit=" + strconv.Itoa(i.processes()),

		// Neither is a bound the report claims, and both are cheap: an adapter
		// is a program that reads two pipes, so no capability it might be
		// granted is one it needs, and nothing it runs has any business gaining
		// privileges it was not started with.
		"--cap-drop=ALL",
		"--security-opt=no-new-privileges",
	}

	return append(append(args, i.Reference), i.Args...)
}

func (i *Image) runtime() string {
	if i.Runtime != "" {
		return i.Runtime
	}

	return DefaultRuntime
}

func (i *Image) timeout() time.Duration {
	if i.Timeout > 0 {
		return i.Timeout
	}

	return DefaultImageTimeout
}

func (i *Image) memory() string {
	if i.Memory != "" {
		return i.Memory
	}

	return DefaultMemory
}

func (i *Image) processes() int {
	if i.Processes > 0 {
		return i.Processes
	}

	return DefaultProcesses
}

func (i *Image) scratch() string {
	if i.Scratch != "" {
		return i.Scratch
	}

	return DefaultScratch
}

// containerName is a name for one container, unique to it.
//
// Random rather than derived from the run, because two engines against the same
// image on one host is an ordinary thing to do — a CI job checking two
// generators, or an author comparing a change against the version before it —
// and a name collision would have one run remove the other's container.
func containerName() (string, error) {
	var b [8]byte

	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("failed to name the adapter's container: %w", err)
	}

	return "cpybkc-conform-" + hex.EncodeToString(b[:]), nil
}

// container is one running adapter container, as the conversation sees it.
//
// It is the [Command] process underneath with the two things a container needs
// on top: the wall clock, and the removal that goes with it.
type container struct {
	Process

	runtime string
	env     []string
	name    string
	timeout time.Duration

	ctx    context.Context
	cancel context.CancelFunc

	once sync.Once
}

// Wait waits for the container to end, and takes it away if it was still there.
//
// The removal is the part a plain command does not need. `run --rm` cleans up a
// container that exited on its own, and the case it does not cover is the one
// this door has to get right: killing an attached client does not stop what it
// was attached to, so a wall clock that expired — or a caller who gave up —
// leaves a container running an adapter nobody is talking to any more. That is
// exactly the resource leak the caps exist to prevent, arriving by the door
// that promised them.
func (c *container) Wait() error {
	err := c.Process.Wait()

	// Read before the cancel below, which would otherwise make every ended
	// conversation look like one that was cut short.
	expired := c.ctx.Err()

	c.cancel()

	if expired == nil {
		return err
	}

	c.remove()

	if errors.Is(expired, context.DeadlineExceeded) {
		return fmt.Errorf("the adapter's container was taken away after the door's %s wall-clock bound: %w",
			c.timeout, err)
	}

	return err
}

// Kill ends the container, rather than the client attached to it.
func (c *container) Kill() {
	c.remove()
	c.Process.Kill()
	c.cancel()
}

// remove takes the container away, once, and best effort.
//
// Nothing is reported: the only outcomes are a container that is gone, which is
// what was asked for, and a runtime that could not be asked, which has already
// cost the run everything it is going to. The context is a fresh one because
// this runs precisely when the run's own is done with.
func (c *container) remove() {
	c.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), removeGrace)
		defer cancel()

		cmd := exec.CommandContext(ctx, c.runtime, "rm", "--force", "--volumes", c.name)
		cmd.Env = c.env

		_ = cmd.Run()
	})
}
