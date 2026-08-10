// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package generate

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// prune removes the files the previous run recorded and this one does not
// produce.
//
// The set it works from is the previous record minus this run's output, and
// nothing else is ever considered. A file nobody recorded is a file a person
// wrote — that is the whole of the guarantee, and it is what lets a generator
// land its output in a directory that also holds hand-written source, up to and
// including the project's root.
//
// It runs after [plan] and before anything is written, which is the one moment
// that works. Before the plan, a run still refusable — a symlink in a scratch
// directory, two generators claiming one path — would have deleted files on its
// way to failing. After the write, a path that was a file last run and is a
// directory this run (or the other way about) would meet the stale entry during
// the merge and fail on it, every run, until somebody removed it by hand.
// Between the two, everything refusable has been refused and nothing has been
// written: the only faults left are the filesystem's, and a run that hits one
// leaves a tree a re-run repairs.
func (l *ledger) prune(ctx context.Context, planned []entry, generated []string) error {
	if l.root == "" || len(l.previous) == 0 {
		return nil
	}

	produced := make(map[string]struct{}, len(generated))
	for _, file := range generated {
		produced[file] = struct{}{}
	}

	var (
		faults  diag.List
		emptied []string
	)

	for _, file := range l.previous {
		if _, again := produced[file]; again {
			continue
		}

		dest := filepath.Join(l.root, filepath.FromSlash(file))

		removed, err := l.remove(ctx, file, dest)
		if err != nil {
			faults.Fail(err)

			continue
		}

		if removed {
			emptied = append(emptied, filepath.Dir(dest))
		}
	}

	if faults.Failed() {
		return faults.Err()
	}

	l.tidy(ctx, planned, emptied)

	return nil
}

// remove takes one file the previous run generated out of the project's tree,
// and reports whether it took anything.
//
// [os.Lstat] rather than [os.Stat], and only a regular file is removed. A
// recorded path that is now a directory or a symlink is somebody's doing: a
// person who replaced a generated file has taken it over, and cpybkc's claim on
// a path ends where a person's begins. It is reported rather than removed and
// rather than passed over in silence, because the alternative to saying so is a
// stale file that never goes away and never explains why.
func (l *ledger) remove(ctx context.Context, file, dest string) (bool, error) {
	info, err := os.Lstat(dest)

	switch {
	case errors.Is(err, fs.ErrNotExist):
		// Already gone — a person deleted it, or a previous run removed it and
		// could not say so. Nothing to do and nothing worth telling anybody.
		return false, nil
	case err != nil:
		return false, &PruneError{Path: file, Dest: dest, Err: err}
	}

	if !info.Mode().IsRegular() {
		l.log.LogAttrs(ctx, slog.LevelWarn,
			"a path a previous run generated is no longer a file and was left where it is",
			slog.String("path", file), slog.String("found", kind(info.Mode())))

		return false, nil
	}

	if err := os.Remove(dest); err != nil {
		return false, &PruneError{Path: file, Dest: dest, Err: err}
	}

	l.log.LogAttrs(ctx, slog.LevelInfo,
		"a file a previous run generated and this one does not was removed",
		slog.String("path", file))

	return true, nil
}

// tidy removes the directories pruning emptied, up to but never including the
// project's root.
//
// A generator that stops producing a package should not leave the package's
// directory behind — an empty directory in a diff is a thing a reviewer has to
// work out. Never the root, whatever pruning empties: that directory is the
// project, it holds the manifest and the record, and it is not something a
// generator was ever given.
//
// A directory this run is about to write into is left alone even when pruning
// empties it, and so is one that is not empty. The first is not tidying but
// churn — the merge would recreate the directory a moment later, with this
// run's mode in place of whatever a person had set on it. The second is the
// ordinary case and is how the walk upward ends.
//
// Nothing here fails the run. A directory that could not be removed is the
// project keeping an empty directory, and refusing a run over that would be
// this package holding output hostage to housekeeping.
func (l *ledger) tidy(ctx context.Context, planned []entry, emptied []string) {
	keep := wanted(planned)

	// The walk stops at the project's root, and at the filesystem's if a record
	// ever named a path that reached one — the second is unreachable through a
	// record this package wrote or would read, and it is here because a loop
	// that removes directories is not the place to be relying on that.
	for _, dir := range emptied {
		for dir != l.root && filepath.Dir(dir) != dir {
			if _, needed := keep[dir]; needed {
				break
			}

			if err := os.Remove(dir); err != nil {
				break
			}

			l.log.LogAttrs(ctx, slog.LevelInfo,
				"a directory pruning left empty was removed",
				slog.String("path", l.relative(dir)))

			dir = filepath.Dir(dir)
		}
	}
}

// wanted is every directory the merge is about to create or write into,
// including the ones above them, up to the filesystem's root.
//
// Built from the plan and not from the generators' output directories, for the
// reason [plan] keeps the two apart: a generator that produced nothing leaves
// the project exactly as it was, so the directory it was pointed at is not one
// this run wants there.
func wanted(planned []entry) map[string]struct{} {
	dirs := make(map[string]struct{}, len(planned))

	for _, e := range planned {
		dir := e.dest
		if !e.dir {
			dir = filepath.Dir(e.dest)
		}

		for {
			if _, held := dirs[dir]; held {
				break
			}

			dirs[dir] = struct{}{}

			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}

			dir = parent
		}
	}

	return dirs
}

// relative is a path as the record spells it, for a message about it: what a
// person reading their project recognises, rather than wherever the run's
// working directory happened to make it absolute from.
func (l *ledger) relative(path string) string {
	rel, err := filepath.Rel(l.root, path)
	if err != nil {
		return path
	}

	return filepath.ToSlash(rel)
}
