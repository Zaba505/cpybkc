// Package main implements cpybkc's root Dagger module: the one definition of
// this repository's pipeline, called by CI and by contributors alike.
//
// # Why this wraps the Z5Labs standard pipeline
//
// Ci does not implement fmt, vet, lint and `go test -race`. It hands the source
// to github.com/z5labs/devex/daggerverse/z5labs and lets that module's GoLib
// archetype run them, which is the same standard z5labs/dfcad runs. A
// reimplementation here would be a second definition of what "checked" means in
// a Z5Labs repository, and two definitions drift: a stage added to the standard
// would silently not apply to cpybkc, and a difference in how a stage is invoked
// would show up as this repository disagreeing with every other one for reasons
// nobody wrote down. Wrapping costs one dependency and keeps that impossible.
//
// GoLib rather than GoApp because cpybkc is a library: there is no main package
// to compile, so the multi-platform image build and the publish half of the
// standard have nothing to act on. When a command lands under cmd/, the
// archetype moves to GoApp and Ci gains the build and publish stages with it;
// that is a change to which factory this file calls, not a change to what the
// pipeline is.
//
// # Why the stage functions exist alongside it
//
// GoLib exposes only the whole pipeline, and waiting on four stages to learn
// that one file is unformatted is not the loop to develop in. So Fmt, Vet, Lint
// and Test are here too. They are not a second implementation: each one drives
// the same github.com/z5labs/devex/daggerverse/go builder that the standard
// pipeline drives, with one stage enabled instead of four, against the same lint
// configuration. Both dependencies are pinned to a single devex commit for that
// reason — a bump has to move them together, or a stage run on its own stops
// being the stage Ci runs.
//
// Ci is what CI calls and what a contributor should run before pushing. The
// stage functions are for narrowing down what Ci reported.
package main

import (
	"context"

	"dagger/cpybkc/internal/dagger"
)

// Cpybkc is the root module type. Source and the lint configuration are bound
// once at construction so every function below checks the same tree the same
// way; there is no per-function source argument that could point somewhere else.
type Cpybkc struct {
	// +private
	Source *dagger.Directory
	// +private
	LintConfig *dagger.File
}

// New binds the repository to the pipeline.
//
// source defaults to the repository root, so `dagger call ci` from a checkout
// needs no arguments. The ignore list drops what no check stage reads: .git is
// excluded because none of fmt, vet, lint or test looks at git metadata, and
// leaving it in would make every commit a cache miss for all four. That changes
// if the archetype ever becomes GoApp — it stamps binaries from the refs at HEAD
// and does need real git metadata, so .git has to come off this list in the same
// change that switches factories.
//
// lintConfig defaults to the repository's own .golangci.yml. It is passed
// explicitly rather than left to the standard pipeline's bundled default so that
// the configuration committed to this repository is the configuration CI lints
// against — a .golangci.yml that CI ignored would be a file contributors read
// and trusted while the pipeline enforced something else.
//
// That file is written in golangci-lint v1 syntax because the standard pipeline
// pins v1.64.8 and offers no way to override it: GoLib takes a config but not a
// version. v1 refuses a v2 config outright rather than ignoring what it does not
// understand, so a v2 file here would fail the lint stage on every run. See
// z5labs/devex#374, and .golangci.yml's own comment.
func New(
	// +optional
	// +defaultPath="/"
	// +ignore=[".git", ".claude", "/bin", "/dist"]
	source *dagger.Directory,
	// +optional
	lintConfig *dagger.File,
) *Cpybkc {
	if lintConfig == nil {
		lintConfig = source.File(".golangci.yml")
	}
	return &Cpybkc{Source: source, LintConfig: lintConfig}
}

// Ci runs the whole pipeline: fmt, vet, golangci-lint and `go test -race`, as
// the Z5Labs standard defines them. This is the single entrypoint — CI is one
// `dagger call ci` and stays one, because a workflow step that reran any of
// these stages would be a second definition of them.
//
// +check
// +cache="session"
func (m *Cpybkc) Ci(ctx context.Context) error {
	return dag.Z5Labs().
		GoLib(m.Source, dagger.Z5LabsGoLibOpts{LintConfig: m.LintConfig}).
		Ci(ctx)
}

// Fmt reports any file that gofmt would rewrite, as a diff.
//
// +check
// +cache="session"
func (m *Cpybkc) Fmt(ctx context.Context) error {
	return m.stage().WithFmt().Check(ctx)
}

// Vet runs `go vet ./...`.
//
// +check
// +cache="session"
func (m *Cpybkc) Vet(ctx context.Context) error {
	return m.stage().WithVet().Check(ctx)
}

// Lint runs golangci-lint over ./... against this repository's .golangci.yml.
//
// +check
// +cache="session"
func (m *Cpybkc) Lint(ctx context.Context) error {
	return m.stage().
		WithLint(dagger.GoCiWithLintOpts{Config: m.LintConfig}).
		Check(ctx)
}

// Test runs `go test -race ./...`. The race detector is on here for the same
// reason it is on in Ci: a race that only the pipeline looks for is one found
// after the change is pushed rather than before.
//
// +check
// +cache="session"
func (m *Cpybkc) Test(ctx context.Context) error {
	return m.stage().
		WithTest(dagger.GoCiWithTestOpts{Race: true}).
		Check(ctx)
}

// stage returns the check builder the standard pipeline builds on, bound to
// this module's source and with no stage enabled yet. Callers enable the one
// they want. The Go toolchain version is left unset so the builder reads it from
// go.mod, which is where this repository's toolchain version lives and the only
// place it lives.
func (m *Cpybkc) stage() *dagger.GoCi {
	return dag.Go().Ci(m.Source)
}
