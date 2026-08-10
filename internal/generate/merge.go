// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package generate

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/plugin"
)

// The modes everything the merge creates is given, before this run's umask is
// taken out of them.
//
// They are the modes an ordinary program creating a file and a directory asks
// for, and not the modes the generator's own files carry. A plugin writes into
// a scratch directory under whatever umask the process — or the container — it
// ran in happened to have, so carrying its modes through would make the
// permissions of a file in a checked-out project a function of where it was
// generated. What is carried across is one bit of the plugin's intent: a file
// it made executable is merged executable, because a generator that emits a
// script means it.
const (
	fileMode fs.FileMode = 0o666
	execMode fs.FileMode = 0o777
	dirMode  fs.FileMode = 0o777
)

// probeName is the file the run's umask is read with, inside the run's own
// scratch space and never inside a generator's directory.
const probeName = ".umask"

// entry is one thing a generator left in its scratch directory, and where it is
// to land.
type entry struct {
	// generator is the generator that produced it, for a diagnostic about it.
	generator string

	// source is where it is now, inside the scratch directory.
	source string

	// root is the generator's output directory, absolute: the boundary between
	// the path a person wrote and the path a plugin produced, which is what
	// [merger.mkdirAll] treats the two halves of differently.
	root string

	// dest is where it is to be, inside the project's tree.
	dest string

	// path is the same destination relative to the generator's output
	// directory: what the plugin called the file, which is the half of the
	// destination a plugin author recognises.
	path string

	// dir reports whether it is a directory rather than a file.
	dir bool

	// exec reports whether the plugin made the file executable.
	exec bool
}

// merge puts what the generators left in their scratch directories into the
// project's tree, and takes out what a previous run put there and this one does
// not produce.
//
// Two passes, and the split is the point. The first reads every generator's
// directory and refuses everything it will not merge — what cannot be merged at
// all, what two generators both produced, and what this run could produce but
// could never record — with nothing written; the second writes. A single pass
// would have the first generator's files in the project's tree by the time the
// second generator's symlink was found, which is the half-generated tree the
// whole arrangement exists to avoid.
//
// Pruning goes between the two, which is the one place it fits: everything
// refusable has been refused, so nothing that deletes a file can still turn out
// to be a run that fails for a reason of its own; and nothing has been written,
// so a path that was a file last run and is a directory this one — or the other
// way about — has the stale entry taken out from under it before the merge ever
// meets it.
func (r *Runner) merge(
	ctx context.Context,
	ledger *ledger,
	generators []Generator,
	invocations []plugin.Invocation,
	root string,
) error {
	planned, err := plan(generators, invocations)
	if err != nil {
		return err
	}

	generated, err := ledger.generated(planned)
	if err != nil {
		return err
	}

	if err := ledger.prune(ctx, planned, generated); err != nil {
		return err
	}

	umask, err := r.umask(root)
	if err != nil {
		return err
	}

	m := &merger{umask: umask, owner: r.Owner}

	for _, e := range planned {
		if err := m.write(e); err != nil {
			// The first failure ends the merge rather than being collected.
			// Everything refusable was refused in the pass above, so a fault
			// here is the filesystem — a full disk, a directory that is not
			// writable — and carrying on would put more of the run into a tree
			// already known to be half of one.
			return &MergeError{Name: e.generator, Path: e.path, Dest: e.dest, Err: err}
		}
	}

	// Last, so that a record is only ever written for a merge that finished.
	// One that failed partway leaves the previous run's record in place, which
	// still names every file this package has ever put in the tree — the ones
	// pruning has already removed have simply gone, and a recorded path that is
	// not there prunes nothing and says nothing. So the next run's list is
	// complete, and re-running after fixing whatever the filesystem objected to
	// is what repairs the tree.
	return ledger.write(m, generated)
}

// plan reads what every generator produced, in the order they were declared,
// refuses what cannot be merged, and refuses what two of them produced between
// them.
//
// Every fault is collected rather than the first returned: a plugin that emits
// symlinks emits them by the directory, and a run that named one of them would
// be a run made once per symlink.
func plan(generators []Generator, invocations []plugin.Invocation) ([]entry, error) {
	var (
		faults  diag.List
		planned []entry
		claims  []entry
	)

	for i, generator := range generators {
		out, err := filepath.Abs(generator.Out)
		if err != nil {
			faults.Fail(&MergeError{Name: generator.Name, Dest: generator.Out, Err: err})

			continue
		}

		produced, err := produced(generator.Name, invocations[i].Out, out)
		faults.Fail(err)

		// The claims are the entries plus the directories the merge has to
		// create to reach them, in one list and in the run's own order, because
		// a generator's hold on the project's tree is wider than the files it
		// produced. They are kept apart from the plan rather than merged into
		// it: a directory the merge creates on the way to an entry is created
		// because that entry needs it, and a generator that produced nothing at
		// all leaves the project exactly as it was.
		claims = append(claims, required(generator.Name, out)...)
		claims = append(claims, produced...)

		planned = append(planned, produced...)
	}

	if faults.Failed() {
		return nil, faults.Err()
	}

	// After the refusals rather than beside them. A generator whose output was
	// refused contributed no entries to compare against, so a collision report
	// built from what is left would be a claim about a run this one is not: the
	// paths of a refused generator are unknown here, not absent. Nothing is lost
	// by reporting one fault at a time — the run fails either way, and it fails
	// before anything is written either way.
	if err := collide(claims); err != nil {
		return nil, err
	}

	return planned, nil
}

// required is the directories one generator needs there to be before any of its
// output can land: the output directory the manifest gave it, and every
// directory above that.
//
// They are claims on the project's tree as much as a produced file is, and
// leaving them out leaves a hole exactly where the merge cannot recover from
// one. A generator landing in `gen` that produces the *file* `pkg`, beside one
// landing in `gen/pkg`, is two generators disagreeing about what `gen/pkg` is —
// and with only produced entries compared, nothing would notice until
// [merger.mkdirRoot] met the file, with the first generator's output already
// written.
//
// Every directory above the output directory and not only the directory itself,
// because [merger.mkdirRoot] creates those too, and a file standing where one of
// them has to go fails the merge in the same way and just as late.
func required(name, out string) []entry {
	var claims []entry

	for path := out; ; path = filepath.Dir(path) {
		claims = append(claims, entry{generator: name, root: out, dest: path, path: ".", dir: true})

		if parent := filepath.Dir(path); parent == path {
			return claims
		}
	}
}

// collide is every place in the project's tree that two generators both claim.
//
// docs/plugin/SPEC.md: cpybkc MUST have resolved every generator's output
// before it writes any of it, and a collision MUST fail the run with nothing
// merged. Both halves are this function's position rather than its content: it
// runs over every generator's claims at once, which is the only place the second
// claimant of a path is knowable, and it runs before [merger.write] has been
// called at all.
//
// The key is the destination and not the path a generator chose, because the
// two are different questions. One relative path under two output directories
// is two files, and two relative paths under directories that overlap are one;
// what would actually be written over is the destination, so the destination is
// what is compared.
//
// Every collision is reported rather than the first, for the reason every
// refusal is: two generators that produce one path usually produce a directory
// of them, and a run naming one would be a run made once per file.
func collide(claims []entry) error {
	var faults diag.List

	// Walked in the order the claims were made — the order the run declared the
	// generators, and lexical within each — so the pair a collision names is the
	// same pair on every run of the same inputs. Generators run concurrently,
	// and naming the one that lost a race is the one thing generated output
	// cannot do.
	claimed := make(map[string]entry, len(claims))

	for _, e := range claims {
		held, taken := claimed[e.dest]
		if !taken {
			claimed[e.dest] = e

			continue
		}

		// Two generators asking for one directory is not a collision, and is
		// what two of them landing in the same place ordinarily do: a directory
		// carries nothing to disagree about, and the files inside it are
		// compared here in their own right. A directory against a *file* is a
		// collision — one of the two would have to stop being what its plugin
		// made it.
		if held.dir && e.dir {
			continue
		}

		faults.Fail(&CollisionError{
			First:      held.generator,
			FirstPath:  held.path,
			Second:     e.generator,
			SecondPath: e.path,
			Dest:       e.dest,
		})
	}

	return faults.Err()
}

// produced is everything one generator left beneath scratch, as entries landing
// under out.
//
// The walk does not follow symlinks — neither into one that names a directory
// nor through one that names a file — so nothing outside scratch is ever read.
// That is the whole of the enforcement docs/plugin/SPEC.md asks for against `..`
// and against an absolute path: a plugin that wrote through either wrote
// somewhere this walk does not look, so it produced no output rather than
// output cpybkc has to reason about.
func produced(name, scratch, out string) ([]entry, error) {
	// The directory itself is checked before it is walked, because a plugin
	// that removed it and put a symlink in its place would otherwise be walked
	// as a thing with nothing in it and reported as having produced nothing.
	// It is a fault about the plugin and it is worth saying so.
	root, err := os.Lstat(scratch)
	if err != nil {
		return nil, &MergeError{Name: name, Dest: out, Err: err}
	}

	if !root.IsDir() {
		return nil, &UnmergeableError{Name: name, Path: ".", Mode: root.Mode(), Target: target(scratch)}
	}

	var (
		faults  diag.List
		planned []entry
	)

	err = filepath.WalkDir(scratch, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if path == scratch {
			return nil
		}

		rel, err := filepath.Rel(scratch, path)
		if err != nil {
			return err
		}

		found := entry{generator: name, source: path, root: out, dest: filepath.Join(out, rel), path: rel}

		switch {
		case d.IsDir():
			found.dir = true
		case d.Type().IsRegular():
			info, err := d.Info()
			if err != nil {
				return err
			}

			found.exec = info.Mode().Perm()&0o111 != 0
		default:
			// A symlink, a device, a socket, a fifo. None of them is a file the
			// project's tree can hold, and the first of them is how a plugin
			// leaves the directory it was given, so it is refused rather than
			// followed or copied.
			faults.Fail(&UnmergeableError{Name: name, Path: rel, Mode: d.Type(), Target: target(path)})

			return nil
		}

		planned = append(planned, found)

		return nil
	})
	if err != nil {
		faults.Fail(&MergeError{Name: name, Dest: out, Err: err})
	}

	if faults.Failed() {
		return nil, faults.Err()
	}

	return planned, nil
}

// target is where a symlink pointed, for the diagnostic that refuses it, and
// nothing at all for anything else.
//
// It is read rather than resolved: what a diagnostic can say is what the plugin
// wrote, and resolving it would name a file the plugin never mentioned — or,
// for a link into a directory that is about to be removed, name nothing.
func target(path string) string {
	read, err := os.Readlink(path)
	if err != nil {
		return ""
	}

	return read
}

// umask is the file-creation mask this run applies to everything it creates.
func (r *Runner) umask(root string) (fs.FileMode, error) {
	if r.Umask != nil {
		return *r.Umask & fs.ModePerm, nil
	}

	return probeUmask(root)
}

// probeUmask asks the kernel what this process's umask is by creating a file
// under it.
//
// Not syscall.Umask, whose only way to read the mask is to set it: it is a
// process-wide setting, generators are started concurrently and inherit it, and
// a run that read the mask that way could hand a generator a mask nobody chose
// — for exactly as long as it took to put the old one back. Creating a file
// with every permission bit asked for and reading back what it got is the same
// question with nothing set, and it is the mask as the filesystem applies it
// rather than as the process declares it.
func probeUmask(dir string) (fs.FileMode, error) {
	path := filepath.Join(dir, probeName)

	probe, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fs.ModePerm)
	if err != nil {
		return 0, &ScratchError{Err: err}
	}

	defer func() {
		_ = os.Remove(path)
	}()

	info, err := probe.Stat()

	if closed := probe.Close(); err == nil {
		err = closed
	}

	if err != nil {
		return 0, &ScratchError{Err: err}
	}

	return fs.ModePerm &^ info.Mode().Perm(), nil
}

// merger writes the plan, and is where the mode and the ownership of everything
// in a project's generated output is decided.
type merger struct {
	umask fs.FileMode
	owner *Owner
}

// write puts one entry where it is to land.
func (m *merger) write(e entry) error {
	if e.dir {
		return m.mkdirAll(e.root, e.dest)
	}

	if err := m.mkdirAll(e.root, filepath.Dir(e.dest)); err != nil {
		return err
	}

	from, err := os.Open(e.source)
	if err != nil {
		return err
	}

	defer func() {
		_ = from.Close()
	}()

	return m.copy(from, e.dest, e.exec)
}

// mkdirAll makes sure there is a directory at path, which is the generator's
// output directory or somewhere beneath it, and at everything between the two.
//
// The two halves are examined differently, and that is the whole of this
// function. Above and at the output directory the walk follows links; beneath
// it, it does not. Which half a component is in decides who chose it: the
// output directory and its parents are the path a person wrote in the manifest,
// and everything below is a path a plugin produced.
func (m *merger) mkdirAll(root, path string) error {
	if path == root {
		return m.mkdirRoot(path)
	}

	if err := m.mkdirAll(root, filepath.Dir(path)); err != nil {
		return err
	}

	return m.mkdir(path)
}

// mkdirRoot makes sure there is a directory at the output directory and at
// every parent of it, creating the ones that are missing.
//
// [os.Stat], so a component that is a symlink to a directory is followed: a
// project reached through one — /tmp on a Mac, a checkout under a symlinked
// home, an output directory a person deliberately pointed elsewhere — is the
// path the manifest named, and writing there is writing where it asked. It is
// not the case [mkdir] guards against, which is a link *underneath* that path.
//
// [os.MkdirAll] would do the creating and could not say which directories it
// had made, and a merge that applied a mode to a directory that was already
// there would be changing the project's tree rather than adding to it.
func (m *merger) mkdirRoot(path string) error {
	switch info, err := os.Stat(path); {
	case err == nil && info.IsDir():
		return nil
	case err == nil:
		return &fs.PathError{Op: "mkdir", Path: path, Err: syscall.ENOTDIR}
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}

	if parent := filepath.Dir(path); parent != path {
		if err := m.mkdirRoot(parent); err != nil {
			return err
		}
	}

	return m.create(path)
}

// mkdir makes sure there is a directory at one path beneath the output
// directory.
//
// [os.Lstat] rather than [os.Stat], and the difference is an escape. A symlink
// standing where a plugin's path needs a directory — `pkg` pointing at /etc,
// left by a previous run of something else or by a plugin that got its output
// merged before this rule existed — reads to [os.Stat] as a perfectly good
// directory, and every file the run wrote beneath it would go through the link
// and out of the project's tree. It is refused instead, in the filesystem's own
// vocabulary, because the caller is holding a [MergeError] that already says
// which generator and which path.
//
// Refused rather than replaced: this is not a path the merge is writing but one
// it is descending, and removing a directory a person put there — or a link
// they pointed somewhere on purpose — would be this package throwing away an
// arrangement it merely failed to understand.
func (m *merger) mkdir(path string) error {
	switch info, err := os.Lstat(path); {
	case err == nil && info.IsDir():
		return nil
	case err == nil:
		return &fs.PathError{Op: "mkdir", Path: path, Err: syscall.ENOTDIR}
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}

	return m.create(path)
}

// create makes one directory and settles what it is.
func (m *merger) create(path string) error {
	if err := os.Mkdir(path, dirMode); err != nil {
		// Another generator's entry got there first, in a run where two of them
		// write into one directory. That is the directory being there, which is
		// what was wanted.
		if errors.Is(err, fs.ErrExist) {
			return nil
		}

		return err
	}

	return m.permit(atPath(path), dirMode)
}

// copy puts one file in the project's tree.
//
// A reader rather than a path, because this run puts two kinds of file there:
// what a generator produced, opened out of its scratch directory, and the run's
// own record, which is bytes in hand and never a file anywhere else. Everything
// below is the same for both — a destination removed rather than opened, an
// exclusive create, a mode settled through the descriptor — and the one thing
// this package must not have two of is the place a file enters a project's
// tree.
func (m *merger) copy(from io.Reader, dest string, exec bool) error {
	// Whatever is at the destination is removed rather than opened. A previous
	// run's file would be truncated either way; a *symlink* left there by a
	// person, or by something else that writes into this directory, would be
	// followed — and the run would then write its output through a link to
	// somewhere nobody asked for. Removing first is what makes the exclusive
	// create below true rather than optimistic.
	if err := os.Remove(dest); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	// Created readable and writable by this process alone and given its real
	// mode once it holds the whole file: a merged file is never briefly
	// readable by more of the machine than it ends up being, and never visible
	// at its final mode while it is still half of itself.
	to, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}

	if err := m.fill(to, from, exec); err != nil {
		_ = to.Close()

		return err
	}

	return to.Close()
}

// fill writes the file's contents and settles what it is.
func (m *merger) fill(to *os.File, from io.Reader, exec bool) error {
	if _, err := io.Copy(to, from); err != nil {
		return err
	}

	mode := fileMode
	if exec {
		mode = execMode
	}

	return m.permit(to, mode)
}

// permitted is something the merge can settle the mode and the ownership of: an
// open file, by its descriptor, or a directory, by its name.
type permitted interface {
	Chmod(mode fs.FileMode) error
	Chown(uid, gid int) error
}

// atPath is a directory, addressed the only way a directory can be — by name,
// because the merge does not hold one open.
type atPath string

// Chmod implements [permitted].
func (p atPath) Chmod(mode fs.FileMode) error { return os.Chmod(string(p), mode) }

// Chown implements [permitted].
func (p atPath) Chown(uid, gid int) error { return os.Chown(string(p), uid, gid) }

// permit is the one place a generated file's mode and ownership are decided.
//
// Every file and every directory the merge creates passes through here and
// nothing else does, which is what makes "output ownership and the umask are
// applied in one place" (#43) a property of this package rather than a
// convention its callers keep. A file is settled through its own open
// descriptor, so nothing between the create and the chmod can substitute a
// different file for it.
func (m *merger) permit(target permitted, mode fs.FileMode) error {
	if err := target.Chmod(mode &^ m.umask); err != nil {
		return err
	}

	if m.owner == nil {
		return nil
	}

	return target.Chown(m.owner.UID, m.owner.GID)
}
