// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package generate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// RecordName is the file a project keeps the record of its last run in, beside
// the manifest that drove it.
//
// It is a constant rather than a string typed out at each call site for the
// reason [github.com/Zaba505/cpybkc/internal/manifest.Name] is one: the name
// appears in a diagnostic, in whatever eventually writes a project's
// .gitignore, and in the file a person finds in their diff. A project keeps
// exactly one of them.
const RecordName = "cpybkc.gen.json"

// recordVersion is the version this cpybkc writes and the only one it reads.
//
// It is a number in the file rather than a shape inferred from what is there,
// so that a record written by a later cpybkc is refused by name instead of
// being read as far as it happens to parse — a misread record is a list of
// files this run would delete.
const recordVersion = 1

// record is the file's shape: the version it was written under, and every path
// the run that wrote it generated.
//
// The paths are relative to the project's root, slash-separated whatever the
// host separates with, and sorted — so two runs that produce the same set of
// files produce a byte-identical file, and a record that shows up in a diff is
// a run that generated something different.
type record struct {
	Version int      `json:"version"`
	Files   []string `json:"files"`
}

// ledger is one run's bookkeeping: where the project's root is, what the
// previous run recorded there, and where pruning is reported.
//
// A ledger with no root keeps no record and prunes nothing. That is a
// [Runner] whose Root is unset, and it is the honest answer rather than a
// degraded one: the record is a file at a project's root, and a run that has
// not been told where that is cannot write one, cannot resolve the paths in one,
// and must not guess — guessing wrong is a run that deletes a file somebody
// wrote.
type ledger struct {
	// root is the project's root, absolute, or empty for a run that keeps no
	// record.
	root string

	// previous is what the last run recorded it had generated, root-relative
	// and slash-separated as the file holds it. Nil where there was no record,
	// which is the case a first run and a deleted record are both in.
	previous []string

	// log is where pruning is reported.
	log *slog.Logger
}

// ledger opens this run's bookkeeping, reading what the last run left.
//
// It is read before any generator is started, for the reason an invocation is
// checked before one is: a record this cpybkc cannot read is a fault in the
// project and not in the output, and finding it after a run would mean
// discovering it once the work was done.
func (r *Runner) ledger() (*ledger, error) {
	l := &ledger{log: r.logger()}

	if r.Root == "" {
		return l, nil
	}

	root, err := filepath.Abs(r.Root)
	if err != nil {
		return nil, &RecordError{Path: filepath.Join(r.Root, RecordName), Err: err}
	}

	l.root = root

	l.previous, err = readRecord(filepath.Join(root, RecordName))
	if err != nil {
		return nil, err
	}

	return l, nil
}

// readRecord is what the run recorded at path generated, or nothing at all
// where there is no record there.
//
// A record that is not there is not a fault and prunes nothing: a first run has
// none, and a person who deleted theirs has said that cpybkc owns none of what
// is in their tree. The cost of that is one stale file surviving one more run,
// and what it buys is that no run ever decides for itself which files look
// generated.
//
// Everything else about the file is a fault, and fails the run rather than
// being read past. The alternative to refusing a record this cpybkc does not
// understand is pruning from a list it has guessed at, and the list is what a
// run deletes.
func readRecord(path string) ([]string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}

		return nil, &RecordError{Path: path, Err: err}
	}

	var held record

	decoder := json.NewDecoder(bytes.NewReader(src))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&held); err != nil {
		return nil, &RecordError{Path: path, Err: err}
	}

	if held.Version != recordVersion {
		return nil, &RecordError{
			Path: path,
			Fault: fmt.Sprintf("it declares version %d, and this cpybkc writes and reads version %d",
				held.Version, recordVersion),
		}
	}

	for _, file := range held.Files {
		// A path this cpybkc could not have written, which means the file has
		// been edited or is not a cpybkc record at all. Every entry is a file
		// pruning may remove, so one that reaches outside the project — an
		// absolute path, a `..`, an empty string — is refused rather than
		// skipped: a record that is wrong about one path is not a record this
		// run should be deleting from at all.
		if !filepath.IsLocal(filepath.FromSlash(file)) {
			return nil, &RecordError{
				Path:  path,
				Fault: fmt.Sprintf("it names %s, which is not a path beneath the project's root", strconv.Quote(file)),
			}
		}
	}

	return held.Files, nil
}

// generated is every file this run produces, root-relative, slash-separated and
// sorted: the list the record is written from and the list pruning subtracts.
//
// Directories are not in it. What a record holds is what a later run may
// remove, and a directory is removed because pruning emptied it rather than
// because it was generated — a directory a person also keeps files in is not
// cpybkc's to delete just because a generator once wrote into it.
func (l *ledger) generated(planned []entry) ([]string, error) {
	if l.root == "" {
		return nil, nil
	}

	var faults diag.List

	files := []string{}

	for _, e := range planned {
		if e.dir {
			continue
		}

		rel, err := filepath.Rel(l.root, e.dest)
		if err != nil || !filepath.IsLocal(rel) || rel == RecordName {
			faults.Fail(&UnrecordableError{Name: e.generator, Path: e.path, Dest: e.dest, Root: l.root})

			continue
		}

		files = append(files, filepath.ToSlash(rel))
	}

	if faults.Failed() {
		return nil, faults.Err()
	}

	slices.Sort(files)

	return files, nil
}

// write puts this run's record where the next run will read it.
//
// Through the merger, so that the record gets this run's umask and this run's
// ownership exactly as the generated files beside it do. It is a file in a
// person's project and it goes into their commit; a record they cannot edit,
// because cpybkc ran as root in a container over a bind mount, is the same
// fault as generated output they cannot edit.
func (l *ledger) write(m *merger, generated []string) error {
	if l.root == "" {
		return nil
	}

	path := filepath.Join(l.root, RecordName)

	src, err := json.MarshalIndent(record{Version: recordVersion, Files: generated}, "", "  ")
	if err != nil {
		return &RecordError{Path: path, Writing: true, Err: err}
	}

	// A trailing newline, because this is a file a person commits and every
	// other line-oriented tool they own expects one.
	src = append(src, '\n')

	if err := m.copy(bytes.NewReader(src), path, false); err != nil {
		return &RecordError{Path: path, Writing: true, Err: err}
	}

	return nil
}
