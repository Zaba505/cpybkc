// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package project

import (
	"path/filepath"

	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/generate"
	"github.com/Zaba505/cpybkc/internal/manifest"
	"github.com/Zaba505/cpybkc/internal/plugin"
	"github.com/Zaba505/cpybkc/irpb"
)

// Run is a project's manifest, read, with everything it names resolved.
//
// There is one descriptor on it and no way to hold a second, which is
// docs/cli/SPEC.md's "Which descriptor is emitted" made a property of the type:
// a manifest names one layout, a layout names the copybooks its records are in,
// and those two facts are the whole of what a run's descriptor is a function of.
// Every generator of the run is handed these bytes and `--emit-ir` writes them,
// so the equality the plugin contract rests reproducibility on holds by there
// being one value rather than by two agreeing.
type Run struct {
	// Manifest is the project's cpybkc.json, as it was written.
	Manifest *manifest.Manifest

	// Dir is the directory holding the manifest, which is the project's root:
	// the base every path the manifest states is resolved against, and where a
	// run keeps its record of what it generated.
	Dir string

	// Descriptor is what the layout and its copybooks resolved to.
	Descriptor *irpb.Descriptor
}

// Load reads the manifest at path and everything it names, and resolves the
// run's one descriptor.
//
// path is what a diagnostic about the manifest names, so it is the path the
// adopter typed or the default `cpybkc.json` in the working directory, rather
// than one made absolute on the way through. Where the file is looked for is
// the caller's: docs/cli/SPEC.md forbids an upward search, and a reader that
// searched would be a second answer to where a project's root is.
//
// Nothing is started and nothing is written. A run that fails here has touched
// nothing but the files it read, which is what lets `--emit-ir` and a
// generation run share the whole of this step.
func Load(path string) (*Run, error) {
	m, err := manifest.ReadFile(path)
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(path)

	d, err := resolveDescriptor(dir, m)
	if err != nil {
		return nil, err
	}

	return &Run{Manifest: m, Dir: dir, Descriptor: d}, nil
}

// Generators resolves every generator the manifest names to an executable on
// searchPath, in the order the manifest declares them.
//
// searchPath is a PATH — the value of the environment variable — and is passed
// rather than read for [github.com/Zaba505/cpybkc/internal/plugin.Resolve]'s
// reason: the search is then a function of its arguments, and a test states an
// environment instead of moving the one it runs in.
//
// Every generator that could not be resolved is reported rather than the first,
// against the line of the manifest that named it: a project that has installed
// none of its generators is one `go install` line short, not one per run.
func (r *Run) Generators(searchPath string) ([]generate.Generator, error) {
	var faults diag.List

	generators := make([]generate.Generator, 0, len(r.Manifest.Generators))

	for _, declared := range r.Manifest.Generators {
		path, err := plugin.Resolve(declared.Name, searchPath)
		if err != nil {
			faults.Fail(&GeneratorError{Pos: declared.Span, Name: declared.Name, Err: err})

			continue
		}

		generators = append(generators, generate.Generator{
			Name: declared.Name,
			Path: path,
			// docs/cli/SPEC.md: a relative path stated in a file is resolved
			// against the directory of that file, and `out` is stated in the
			// manifest.
			Out:     at(r.Dir, declared.Out),
			Options: options(declared.Options),
		})
	}

	if faults.Failed() {
		return nil, faults.Err()
	}

	return generators, nil
}

// options carries a manifest's options across to the ones an invocation is made
// with, in the order the manifest declared them — which docs/plugin/SPEC.md
// makes the order the argument vector carries them in.
func options(declared []manifest.Option) []plugin.Option {
	if len(declared) == 0 {
		return nil
	}

	carried := make([]plugin.Option, 0, len(declared))
	for _, option := range declared {
		carried = append(carried, plugin.Option{Key: option.Key, Value: option.Value})
	}

	return carried
}

// dialect is the compiler-side half of every copybook this package reads.
//
// See the package comment: no layout form states a dialect, a run needs one,
// and this is the one the files this project exists for were written by.
func dialect() copybook.Dialect { return copybook.IBMEnterprise() }

// at resolves a path stated in a file against the directory of that file.
//
// An absolute path is taken as it is written, which is the same rule read on a
// path that needs no base rather than an exception to it. A path is written
// slash-separated in a manifest and a layout, because both are files an adopter
// checks in and reads on whatever they are standing at, so it is turned into the
// host's own spelling before anything opens it.
func at(dir, path string) string {
	path = filepath.FromSlash(path)
	if filepath.IsAbs(path) {
		return path
	}

	return filepath.Join(dir, path)
}

// absolute is path as cpybkc opened it, for the "where it was looked for" a
// diagnostic about a file that would not open owes its reader.
//
// A path that cannot be made absolute is reported as it stands. The failure is
// the working directory having gone away underneath the process, and a
// diagnostic about a missing copybook is the wrong place to report it.
func absolute(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}

	return path
}
