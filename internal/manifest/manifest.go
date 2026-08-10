// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// Name is the file a project keeps its manifest in.
//
// It is a constant rather than a string typed out at each call site because the
// name appears in a diagnostic, in the README and in whatever eventually looks
// for the file, and a project is driven by exactly one of them.
const Name = "cpybkc.json"

// Manifest is a project's cpybkc.json, read.
//
// Everything in it is what the file said, in the order the file said it. A path
// is the string that was written and nothing has been resolved against
// anything; see the package comment for why that is left to whatever opens the
// file.
type Manifest struct {
	// File is the name the manifest was read under, and is what every
	// diagnostic about it names.
	File string

	// Inputs are the copybooks every generator reads, in the order they were
	// written. A manifest that names none is not a fault: which copybooks a
	// record is in is the layout's to say (docs/layout/SPEC.md, "Binding a
	// record to a copybook"), and this list is the project's statement of what
	// a run reads.
	Inputs []string

	// Layout is the layout file the run resolves against.
	Layout string

	// Generators are the generators to run, in the order they were declared.
	Generators []Generator
}

// InputsFor is every copybook g reads: the manifest's inputs, then g's own, in
// the order they were written.
//
// The two are merged rather than the entry's replacing the manifest's, because
// the top-level list is what a project shares and an entry's list is what one
// generator needs beyond it — a generator that had to restate the shared list to
// add one file would be a list kept in step by hand.
//
// A path named in both lists is kept once, at the position the first mention
// gives it. Reading one copybook twice is not a second copybook, and the
// duplicate is what a shared list plus a specific one produces honestly; a
// repeat *within* one list is a different thing and is a fault [Read] reports.
func (m *Manifest) InputsFor(g Generator) []string {
	var merged []string

	seen := make(map[string]struct{}, len(m.Inputs)+len(g.Inputs))

	for _, path := range slices.Concat(m.Inputs, g.Inputs) {
		if _, duplicate := seen[path]; duplicate {
			continue
		}

		seen[path] = struct{}{}
		merged = append(merged, path)
	}

	return merged
}

// Generator is one entry of a manifest's generators list.
type Generator struct {
	// Span is where the entry was written, so that a fault found about a
	// generator after the manifest was read — a name that resolves to no
	// executable (#41), two entries writing the same output file (#44) — can
	// still point at the line the adopter has to edit.
	Span diag.Span

	// Name is the generator's name, which resolves to the cpybkc-gen-<Name>
	// executable on PATH (#41). It is non-empty and contains no `/`; the rest
	// of docs/plugin/SPEC.md's advice on a name is a SHOULD and is left alone,
	// because this package reports faults an adopter must fix and has no
	// channel for one they may.
	Name string

	// Out is the directory this generator's output lands in.
	Out string

	// Inputs are the copybooks this generator reads beyond the manifest's.
	// [Manifest.InputsFor] is the merged list; this is what the entry itself
	// declared.
	Inputs []string

	// Options are the generator's options, in the order the manifest declares
	// them, which is the order docs/plugin/SPEC.md requires them to be passed
	// in.
	Options []Option
}

// Option is one option a generator is invoked with, which reaches it as a
// single `--opt k=v` argument.
type Option struct {
	// Key is the option's name. It is non-empty and contains no `=`, which is
	// what lets `k=v` be split on its first one.
	Key string

	// Value is what the manifest wrote for it, and may be empty.
	Value string
}

// ReadFile reads the manifest at path.
//
// The path is what every diagnostic names, so it is the path an adopter typed
// or the one a project's root was joined onto rather than one this package
// invented. Looking for the file — which directory a run starts from, whether a
// parent is searched — belongs to the caller: a reader that searched would be a
// second answer to where a project's root is.
func ReadFile(path string) (*Manifest, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, &NotFoundError{Path: path}
		}

		return nil, fmt.Errorf("%s: %w", path, err)
	}

	return parse(path, src)
}

// Read reads a manifest from r, under the name every diagnostic about it will
// carry.
//
// The whole of it is read before anything is parsed, because a diagnostic
// carries a line and a column and both are counted over the bytes the manifest
// is; a manifest is a file a person wrote and is small enough that this costs
// nothing worth arguing about.
func Read(name string, r io.Reader) (*Manifest, error) {
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	return parse(name, src)
}

// parse walks src as a manifest, reporting every fault it found.
func parse(file string, src []byte) (*Manifest, error) {
	p := newParser(file, src)

	if len(bytes.TrimSpace(src)) == 0 {
		return nil, &SyntaxError{
			Span:  p.spanAt(0),
			Fault: "the manifest is empty; it is a JSON object naming the layout, the copybooks and the generators to run",
		}
	}

	m, fatal := p.manifest()
	if fatal != nil {
		p.faults.Fail(fatal)

		return nil, p.faults.Err()
	}

	p.trailing()

	if p.faults.Failed() {
		return nil, p.faults.Err()
	}

	return m, nil
}
