// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package generate

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/plugin"
	"github.com/Zaba505/cpybkc/irpb"
)

// scratchPattern is the prefix [os.MkdirTemp] builds the name of one run's
// scratch space from.
//
// It names cpybkc because the space is made under [Runner.Scratch] — for
// cpybkc's own runs, the project's root — rather than in a temporary directory
// the operating system would eventually sweep. A run killed outright, where the
// deferred removal never happens, leaves this directory in somebody's tree and
// in their `git status`, so the name has to say what left it and that it is
// nobody's output.
const scratchPattern = "cpybkc-scratch-"

// scratchMode is the mode a generator's own directory inside that space is
// created with: this process's and nothing else's.
//
// A generator is a child of this process and writes as the same user, so a
// private directory costs it nothing. What the mode buys is that a run's
// half-finished output is not readable by everyone on the machine while it is
// being produced — the project's tree gets [Runner.Umask]'s modes at the merge,
// and until then the files are nobody's business but the run's.
const scratchMode = 0o700

// Generator is one generator a run runs: the executable to run, the options to
// run it with, and the directory of the project's tree its output lands in.
//
// It is not a [github.com/Zaba505/cpybkc/internal/plugin.Invocation]; see the
// package comment for why the two are kept apart.
type Generator struct {
	// Name is the generator's name — the `<name>` a manifest asked for (#40).
	// It is what every diagnostic about this generator names, and what the
	// lines it writes are attributed to.
	Name string

	// Path is the executable to run, as
	// [github.com/Zaba505/cpybkc/internal/plugin.Resolve] spelled it.
	Path string

	// Out is the directory of the project's tree this generator's output lands
	// in — the `out` its manifest entry declared (#40). It may be relative, and
	// is resolved against the working directory the run is made from, exactly
	// as the path in the argument vector is.
	//
	// Two generators may name the same directory, and a project that generates
	// a package from two of them usually does. Two that also produce the same
	// path *within* it fail the run with nothing merged; see [CollisionError].
	Out string

	// Options are the options to pass, in the order they are to be passed —
	// the order the manifest declared them, which docs/plugin/SPEC.md makes the
	// order the argument vector carries them in.
	Options []plugin.Option
}

// Owner is a user and a group, as chown(2) takes them.
type Owner struct {
	// UID is the user every file and directory the merge creates is given.
	UID int

	// GID is the group it is given.
	GID int
}

// Runner runs the generators of one project.
//
// [Runner.Scratch] is required and everything else has a zero value that runs:
// the generators are run by the zero
// [github.com/Zaba505/cpybkc/internal/plugin.Runner], everything merged is
// given this process's umask applied to the usual creation modes, and ownership
// is left as the merging process's.
type Runner struct {
	// Plugins runs the generator executables. Nil is the zero
	// [github.com/Zaba505/cpybkc/internal/plugin.Runner], read at the moment of
	// the run, so that a caller that configures logging after building a Runner
	// is still heard.
	Plugins *plugin.Runner

	// Scratch is the directory this run's scratch space is created in, and is
	// required. A run has nowhere to put its generators' output without one, so
	// an empty Scratch fails the run before a generator is started rather than
	// falling back to somewhere ambient.
	//
	// That it is required is the whole of #184. What an empty field used to mean
	// was [os.MkdirTemp]'s default — TMPDIR, or the system's temporary directory
	// — and a field defaulting to that is what made an ambient directory
	// reachable at all. cpybkc's image carries no /tmp to reach, so the fallback
	// is deleted rather than repaired, and the type is what says so: no zero
	// value of this Runner can run.
	//
	// It is a field of its own rather than [Runner.Root] because the two answer
	// differently when unset. A run that has not been told where the project's
	// root is keeps no record and prunes nothing, which is an honest degraded
	// run; a run that has not been told where to put its scratch space has no
	// degraded form at all, and guessing is exactly what this field exists to
	// stop. cpybkc's own runs pass the project's root for both, which is what
	// puts the scratch space in the tree the run is already writing.
	//
	// The merge copies rather than renames, so Scratch and the project need not
	// share a filesystem — a caller whose tree is on a small or slow one may
	// point this somewhere else, at the cost of the run writing its output
	// twice. What it may not be is nothing.
	Scratch string

	// Umask is the file-creation mask taken out of the mode of everything the
	// merge creates. Nil is this process's own, read once per run.
	//
	// It is a field so that a caller — or a test — can state the mask instead
	// of moving the process's, which is a process-wide setting that every
	// generator started concurrently would inherit.
	Umask *fs.FileMode

	// Owner, when set, is the user and group given to every file and directory
	// the merge creates. Nil leaves them as the merging process's.
	Owner *Owner

	// Root is the project's root: the directory this run keeps its record of
	// what it generated in, and the directory every path in that record is
	// relative to. It may be relative, and is resolved against the working
	// directory the run is made from, exactly as a generator's Out is.
	//
	// Empty keeps no record and prunes nothing. That is the zero value, and it
	// is the honest answer rather than a degraded one: the record is a file at
	// a project's root, and a run that has not been told where that is must not
	// guess — a wrong guess is a run that deletes something a person wrote.
	// Whatever found the project's cpybkc.json knows where its root is, and
	// that is the one place the answer exists.
	Root string

	// Log is where pruning is reported: a file a previous run generated and
	// this one did not, removed, and a recorded path a person has since taken
	// over, left alone. Nil is [log/slog.Default] read at the moment of the
	// run, so that a caller that configures logging after building a Runner is
	// still heard.
	//
	// It is separate from the logger the generators' own output is surfaced
	// through, which is
	// [github.com/Zaba505/cpybkc/internal/plugin.Runner.Log]'s: a line here is
	// cpybkc saying what it did to a project's tree, and a line there is a
	// generator talking.
	Log *slog.Logger
}

// Run runs every generator against d, each in a private empty directory of its
// own, and merges what they left there into the project's tree.
//
// Nothing is written where a person would find it until every generator has
// exited zero: a run in which one failed returns that failure with the
// project's tree exactly as it found it, down to not creating an output
// directory that was not already there. A generator that fails after writing
// files has produced nothing, which is what its scratch directory being
// discarded means.
//
// The failure of the run is the plugin package's — a
// [github.com/Zaba505/cpybkc/internal/plugin.ExitError] for a generator that
// exited non-zero, a
// [github.com/Zaba505/cpybkc/internal/plugin.SignalError] for one that was
// killed, and all of them where more than one failed — because a generator that
// ran and failed is a fault about that generator and not about the merge that
// never happened.
//
// What the merge itself refuses is [UnmergeableError] — something a generator
// left that is neither a file nor a directory — and [CollisionError], one place
// in the project's tree that two generators both produced. Both are reported
// for every entry refused rather than for the first: a plugin that emits
// symlinks emits them by the directory, and a run that named one of them would
// be run once per symlink. Both are decided before anything is written, so
// either costs the run and never half a tree.
//
// The context bounds the generators, exactly as it bounds
// [github.com/Zaba505/cpybkc/internal/plugin.Runner.Run]. It does not interrupt
// the merge, which happens after every generator has already succeeded and is
// the step whose partial completion the whole arrangement exists to avoid.
func (r *Runner) Run(ctx context.Context, d *irpb.Descriptor, generators []Generator) error {
	if len(generators) == 0 {
		return nil
	}

	// Checked before anything is created, for the reason the plugin package
	// checks an invocation before starting a generator: a generator with
	// nowhere to put its output is a fault in what the caller assembled, and
	// finding it after a run would mean discovering it once the work was done.
	var invalid diag.List

	// Checked with them, and first, because it is the same kind of fault: a run
	// with nowhere to make its scratch space is a caller that assembled a Runner
	// it never finished, and there is no directory this package is entitled to
	// pick on its behalf (#184).
	if r.Scratch == "" {
		invalid.Fail(fmt.Errorf("this run has no directory to make its scratch space in; Scratch is required"))
	}

	for _, generator := range generators {
		if generator.Out == "" {
			invalid.Fail(fmt.Errorf("the generator %s has no directory for its output to land in",
				strconv.Quote(generator.Name)))
		}
	}

	if invalid.Failed() {
		return invalid.Err()
	}

	// Read here, before a generator has been started, for the same reason: a
	// record this cpybkc cannot read is a fault in the project rather than in
	// the output, and finding it after the run would mean discovering it once
	// the work was done.
	ledger, err := r.ledger()
	if err != nil {
		return err
	}

	root, err := os.MkdirTemp(r.Scratch, scratchPattern)
	if err != nil {
		return &ScratchError{Err: err}
	}

	// The scratch space is the run's and does not outlive it, whether the run
	// succeeded or not. It is not reported: by the time it is removed the
	// generators have all exited and the merge has been made or refused, so
	// there is nothing left for a failure to spoil.
	defer func() {
		_ = os.RemoveAll(root)
	}()

	// Made absolute so that the diagnostics and the argument vector name the
	// same directory even when Scratch was written relative.
	root, err = filepath.Abs(root)
	if err != nil {
		return &ScratchError{Err: err}
	}

	invocations, err := r.scratch(generators, root)
	if err != nil {
		return err
	}

	if err := r.plugins().Run(ctx, d, invocations); err != nil {
		return err
	}

	return r.merge(ctx, ledger, generators, invocations, root)
}

// scratch gives each generator its own empty directory and hands back the
// invocations that run them into it.
//
// The directory is named for the generator's position and not for the
// generator, because two entries of one manifest may name the same generator
// with different options and different output directories, and a directory
// named for a generator would then be two generators' — which is the one thing
// docs/plugin/SPEC.md says a scratch directory is not. Nothing reads the name:
// a plugin is told where to write and is entitled to derive nothing from the
// path it was told.
//
// Each generator's directory is one level down from root, and the merge walks
// those and never root itself. That is what makes room for
// [github.com/Zaba505/cpybkc/internal/plugin]'s per-invocation descriptor
// directories, which land beside them here (#184): they are inside this run's
// scratch space, outside every walk the merge makes, and removed with the space
// they are in — without being hidden, and without a rule anybody has to keep.
func (r *Runner) scratch(generators []Generator, root string) ([]plugin.Invocation, error) {
	invocations := make([]plugin.Invocation, len(generators))

	for i, generator := range generators {
		dir := filepath.Join(root, strconv.Itoa(i))

		if err := os.Mkdir(dir, scratchMode); err != nil {
			return nil, &ScratchError{Name: generator.Name, Err: err}
		}

		invocations[i] = plugin.Invocation{
			Name:    generator.Name,
			Path:    generator.Path,
			Out:     dir,
			Options: generator.Options,
		}
	}

	return invocations, nil
}

// plugins is what runs the generator executables.
func (r *Runner) plugins() *plugin.Runner {
	if r.Plugins != nil {
		return r.Plugins
	}

	return &plugin.Runner{}
}

// logger is where this run says what it did to the project's tree.
func (r *Runner) logger() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}

	return slog.Default()
}
