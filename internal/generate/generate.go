// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package generate

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/plugin"
	"github.com/Zaba505/cpybkc/irpb"
)

// scratchPattern is the prefix [os.MkdirTemp] builds the name of one run's
// scratch space from.
const scratchPattern = "cpybkc-out-"

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
// The zero value runs them: the generators are run by the zero
// [github.com/Zaba505/cpybkc/internal/plugin.Runner], the scratch directories
// are made wherever the system puts temporary files, everything merged is given
// this process's umask applied to the usual creation modes, and ownership is
// left as the merging process's.
type Runner struct {
	// Plugins runs the generator executables. Nil is the zero
	// [github.com/Zaba505/cpybkc/internal/plugin.Runner], read at the moment of
	// the run, so that a caller that configures logging after building a Runner
	// is still heard.
	Plugins *plugin.Runner

	// TempDir is where this run's scratch space is created. Empty is the
	// default [os.MkdirTemp] uses.
	//
	// It is worth setting to somewhere beside the project when the two are on
	// different filesystems: the merge copies rather than renames, so nothing
	// breaks when they differ, but the run then writes its whole output twice.
	TempDir string

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

	for _, generator := range generators {
		if generator.Out == "" {
			invalid.Fail(fmt.Errorf("the generator %s has no directory for its output to land in",
				strconv.Quote(generator.Name)))
		}
	}

	if invalid.Failed() {
		return invalid.Err()
	}

	root, err := os.MkdirTemp(r.TempDir, scratchPattern)
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
	// same directory even when TempDir was written relative.
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

	return r.merge(generators, invocations, root)
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
