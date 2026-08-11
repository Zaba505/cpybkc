// Package main implements cpybkc's companion Dagger module: the published CLI
// image as a step in somebody else's pipeline, for a caller who would rather not
// write a Dockerfile.
//
//	dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
//	  image with-exec --use-entrypoint --args=--version stdout
//
// Nothing is installed on the host and no image is built: the published image is
// pulled and used.
//
// # This is a convenience, not a contract
//
// The contract is the published image. docs/container/SPEC.md says what the
// entrypoint is, which directory generators are discovered in, which UID the
// process runs as and where the IR schema lives, and it is that document a third
// party builds against. Everything here is those promises spelled as Dagger
// calls, and every one of them can be written by hand as
// `docker run --rm ghcr.io/zaba505/cpybkc:v0` instead.
//
// So this module gets no SPEC.md, deliberately (docs/CONVENTIONS.md, "What
// belongs here"). A specification for it would imply the contract is a property
// of the module — that a caller reaching for `docker run`, a Kubernetes Job or a
// Dockerfile were on a lesser path — when the module is the one thing in this
// repository that could be deleted without breaking a single promise cpybkc
// makes. What it needs to say, it says in this comment and in
// `dagger call --help`.
//
// It states nothing about the plugin CLI contract either. `cpybkc-gen-<name>`,
// the argument vector and the exit codes are docs/plugin/SPEC.md's, they hold
// with no container anywhere in the picture, and the only thing this module will
// ever know about them is the filename a generator is installed under.
//
// # The module ref is permanent public API
//
// This module is published as `github.com/Zaba505/cpybkc/daggerverse/cpybkc`,
// and a Dagger module ref is a directory path inside a tag of this repository.
// Renaming the directory would not deprecate the old ref, it would delete it:
// every caller pinning `…/daggerverse/cpybkc@v0.3.1` resolves that path inside
// the tag they named, so a rename breaks every unpinned caller at once and
// strands every pinned one on a path that will never move again. There is no
// redirect to leave behind and no deprecation period to serve.
//
// The name is therefore chosen once and kept. `daggerverse/<module>` is the
// house layout — it is where `github.com/z5labs/devex` publishes the two modules
// this repository's pipeline already depends on — and the directory is named for
// what the module drives rather than for what it does, so that a second module
// shipping from here later needs no rename of this one to sit beside it.
//
// The `.dagger/` module at the repository root is a different thing entirely: it
// runs this repository's own pipeline, it is published for nobody, and it knows
// about a checkout of cpybkc that this module never sees. What holds the two to
// one Dagger `engineVersion` is the local dependency edge the root `dagger.json`
// declares on this directory — an engine bump is then one commit touching both
// files, rather than two files nothing requires to agree.
//
// # What this does not do
//
// It does not build cpybkc from source, and it is not this repository's
// pipeline. `dagger call ci`, the image builds and the contract checks are the
// root module's.
//
// Composing a generator into the image (#63) is not here yet. What is here is
// running the CLI over a project — Generate, and Run for everything Generate
// does not curate — on top of the part they both start from: deciding which
// image runs, which is the choice a caller makes once and the one that has to be
// right before anything is built on it.
//
// # The curated surface, and why it is not the flag table
//
// Generate takes a source directory and a manifest, and that is the whole of
// what this module maps by name. It deliberately does not grow an argument per
// entry in docs/cli/SPEC.md's flag table: a module argument is public API for as
// long as the directory this module is published under exists, so a table
// mapped one-to-one would make every change to that table a change to this
// module's surface, and would have to express in Dagger arguments things a
// command line says better — a flag that replaces the action rather than
// configuring it, and a flag that may not appear beside another.
//
// Run is the other half of that decision, and the reason the first half can stay
// small. It takes the argument vector verbatim and hands back the container, so
// an uncurated flag — --emit-ir, --emit-ir-format, --version, --help, and
// whatever this document has not heard of — is reachable without this module
// having an opinion about it. The root pipeline's CliSurface check is what keeps
// the split honest: it fails when the CLI grows a flag that neither Generate nor
// Run is recorded as covering, so the two cannot drift quietly.
package main

import (
	"context"
	"fmt"

	"dagger/cpybkc/internal/argv"
	"dagger/cpybkc/internal/dagger"
	"dagger/cpybkc/internal/imageref"
)

// projectDir is where a caller's project is mounted, and the working directory
// the CLI is run from.
//
// It is this module's choice rather than the base-image contract's: that
// document pins the entrypoint, the plugin directory and the user, and sets no
// WORKDIR at all, leaving `docker run -v "$PWD:/src" -w /src …` to say where a
// project lives for the length of one command. The value matters to a caller
// only through Run, whose container is handed back with the project still at
// this path, so it is written into that function's documentation rather than
// left to be discovered.
const projectDir = "/src"

// contractUser is the UID:GID docs/container/SPEC.md pins the image to, used to
// own the mounted project when the container cannot say who it runs as.
//
// The container is asked first, and this is the fallback, because a caller may
// have passed --image. Owning the mount as somebody the process is not is a run
// that fails on the first file a generator writes, which is a long way from the
// argument that caused it.
const contractUser = "65532:65532"

// Cpybkc is one cpybkc image, plus the coordinates for resolving images related
// to it.
//
// A function on it is a builder returning a new Cpybkc, a terminal that runs
// something, or an accessor handing back what was resolved, so a call chain
// reads as the image being assembled and then used; nothing here mutates.
type Cpybkc struct {
	// Container is the image cpybkc runs in.
	// +private
	Container *dagger.Container

	// Repository and Version are the coordinates a later composition resolves
	// *other* images against — the companion generator images of #63, which are
	// published as `<repository>-gen-<name>:<version>`. They are kept rather than
	// consumed at construction because an override that reached only the first
	// pull would leave every later one reaching for ghcr.io from inside a network
	// that cannot see it, which is the air-gap requirement failing later and less
	// legibly than if the argument had never existed.
	//
	// They are deliberately *not* a description of what is in Container. When a
	// caller supplies their own container these still hold whatever --repository
	// and --version said, which for a caller who passed neither is the default
	// pair. That is why --version and --repository remain meaningful alongside
	// --image rather than being ignored: an air-gapped caller passing a container
	// from their own registry should pass --repository too, so that the generators
	// resolved later come from the same place.
	//
	// What this deliberately does not do is *infer* a release from a supplied
	// container. Nothing here can read a version off an arbitrary container, and
	// guessing one would produce exactly the untested pairing — a generator from
	// one release beside a CLI from another — that #63 exists to refuse. Whether
	// that mismatch is refused, warned about or required to be stated explicitly
	// is #63's to settle, and it needs these two fields to say anything about it
	// at all.
	// +private
	Repository string
	// +private
	Version string
}

// New selects the cpybkc release to run:
//
//	dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
//	  --version=v0.3.1 --repository=registry.internal/mirrors/cpybkc image
//
// The defaults pull the published base image, which carries the CLI and no
// generator at all — cpybkc's own generator reaches a user the way a stranger's
// does, as a separate image built `FROM` the base (docs/container/SPEC.md, "Why
// cpybkc's own generator is not in the base image"). So an image that generates
// anything is a composition the caller asked for rather than one the base image
// decided for them.
//
// image replaces the container that would otherwise be pulled, so passing it
// means version and repository name nothing that gets fetched here. They are not
// thereby inert: they stay on the module as the coordinates later compositions
// resolve companion images against (#63), which is why an air-gapped caller
// passing a container from their own registry should pass --repository beside it
// rather than instead of it.
func New(
	// The tag of the published cpybkc image to run.
	//
	// It defaults to the moving major tag "v0", which follows every release in
	// that major version: a caller who passes nothing keeps up with fixes and
	// stays inside the base-image contract's compatibility guarantees. Escalate
	// deliberately — "v0" follows releases, "v0.3.1" pins one release, and only a
	// digest pins the bytes. A digest is not a tag, so it goes to --image rather
	// than here.
	// +optional
	// +default="v0"
	version string,
	// The registry repository the image is pulled from, as `<host>/<path>` with no
	// tag.
	//
	// It is an argument because where the image lives is not something the
	// base-image contract promises — a mirror, an internal registry or an
	// air-gapped copy serving the same digests satisfies every requirement in that
	// document identically, and a caller behind one should not have to give up
	// this module to use it. Companion images derive from it by the rule this
	// project publishes them under, so redirecting this redirects the family
	// rather than the base image alone.
	// +optional
	// +default="ghcr.io/zaba505/cpybkc"
	repository string,
	// Run in this container instead of pulling one, replacing it entirely.
	//
	// It has to keep the promises the base-image contract makes — cpybkc as the
	// entrypoint, the plugin directory on PATH, a user that can execute what is in
	// it — because that is all this module drives it through. Nothing here checks
	// that, and nothing could: a container is not required to have come from a
	// registry at all.
	//
	// This is how cpybkc's own pipeline checks the module against the image it
	// just built rather than against the last release (#64), how a caller tries a
	// change to cpybkc before it ships, and how a build pins the image by digest —
	// a reference no tag argument can express, and the only one that pins bytes.
	// +optional
	image *dagger.Container,
) (*Cpybkc, error) {
	// Checked even when image is supplied and nothing is pulled from them, because
	// they are not decoration in that case either: #63 resolves companion images
	// against them, and a repository that is wrong is as wrong then as now. A
	// constructor that accepted them quietly would move the complaint to a call
	// the caller has not written yet.
	ref, err := imageref.Assemble(repository, version)
	if err != nil {
		return nil, err
	}
	if image == nil {
		image = dag.Container().From(ref)
	}
	return &Cpybkc{Container: image, Repository: repository, Version: version}, nil
}

// Image is the image this module resolved, for a caller who wants to do
// something with it other than what this module offers — run a cpybkc
// subcommand by hand, look at what is in it, or push it somewhere of their own:
//
//	dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
//	  image with-exec --use-entrypoint --args=--version stdout
//
// Note the --use-entrypoint. The image's entrypoint is the CLI and the literal
// value of it is not part of the contract (docs/container/SPEC.md, "The CLI's
// own path is not part of the contract"), so an exec that named a path instead
// would be reaching for the one thing that document reserves the right to move.
//
// Publishing the image elsewhere is a reasonable thing to do — it is the same
// image the registry served — but a copy is not a release of cpybkc and carries
// none of the signatures or attestations one does (docs/container/SPEC.md, "What
// a tag carries besides the image"), because those are attached to the digest
// this project published and not to the bytes.
func (m *Cpybkc) Image() *dagger.Container {
	return m.Container
}

// Generate runs cpybkc over a project and hands back the project as it should
// now be committed:
//
//	dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
//	  generate --source . export --path .
//
// Nothing is written to the host. The Directory that comes back is a value like
// any other, and exporting it is the caller's separate, explicit step — which is
// also what disposes of the ownership problem a bind mount has, where generated
// files land owned by whichever UID the container ran as and a host user who is
// not that UID owns none of their own project. Dagger's export writes as the
// person running it, so the image's pinned user never reaches the host.
//
// What comes back is the **whole project directory**, not only the files a
// generator produced. A generator's output lands where the manifest says, which
// is ordinarily inside the source tree, and a run also prunes what a previous
// run generated and no longer would — so the generated files alone cannot
// express half of what a run did. `generate --source . export --path .` is
// therefore the ordinary use, and it is a full statement of the run rather than
// an overlay that leaves deletions behind.
func (m *Cpybkc) Generate(
	ctx context.Context,
	// The project to generate over: the directory holding the manifest, the
	// layout it names and the copybooks that layout names.
	//
	// It is mounted whole rather than filtered down to what a run reads, because
	// which copybooks a run reads is a property of the layout — the manifest
	// carries no input list — and a module guessing at that set would be a second,
	// weaker answer to a question docs/cli/SPEC.md answers exactly.
	source *dagger.Directory,
	// The project manifest to read, relative to the root of source.
	//
	// It defaults to nothing, which leaves cpybkc reading `cpybkc.json` at the
	// root of the mounted project — the CLI's own default, applied by the CLI,
	// against the working directory this module puts the project at. There is no
	// upward search and no second manifest, so a project keeping its manifest
	// somewhere else says so here.
	//
	// A relative path resolves against that project root, because that is the
	// directory the CLI resolves a path typed on the command line against. It
	// cannot be "-": a manifest's own paths are relative to the directory holding
	// it, and a manifest arriving on a stream is in no directory.
	// +optional
	manifest string,
) (*dagger.Directory, error) {
	args, err := argv.Generate(manifest)
	if err != nil {
		return nil, err
	}

	project, err := m.project(ctx, source)
	if err != nil {
		return nil, err
	}

	return project.
		WithExec(args, dagger.ContainerWithExecOpts{UseEntrypoint: true}).
		Directory(projectDir), nil
}

// Run is the escape hatch: cpybkc invoked with an argument vector this module
// has no opinion about, in a container handed back whole.
//
//	dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
//	  run --source . --args=--emit-ir,- stdout
//
//	dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
//	  run --args=--help stdout
//
// It exists so that Generate does not have to grow an argument every time
// docs/cli/SPEC.md's flag table does. A flag that replaces the action rather
// than configuring it (--emit-ir is terminal: no generator runs and nothing is
// merged or pruned), one that may only appear beside another (--emit-ir-format
// without --emit-ir is a usage error), and one that answers a question about the
// program rather than about a project (--version, --help) are all things a
// command line states plainly and a set of Dagger arguments states badly.
//
// A container rather than a directory, because the uncurated invocations are the
// ones whose answer is not a tree. --emit-ir may write to standard output, and
// --version and --help write nothing else at all; a Directory return would make
// the escape hatch unable to reach exactly the flags it exists for. A caller who
// does want the tree takes it from the container, where the project is still
// mounted at /src:
//
//	dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
//	  run --source . --args=--manifest,build/cpybkc.json \
//	  directory --path=/src export --path .
//
// Nothing here is validated. A vector this module checked would be a second,
// unversioned reading of a contract the CLI already implements, and its
// diagnostics are better than anything guessed at from out here: what an
// unrecognised flag is, whether a flag may repeat and what a usage error exits
// with are all docs/cli/SPEC.md's, and the exec's failure carries them back
// verbatim.
func (m *Cpybkc) Run(
	ctx context.Context,
	// The argument vector, passed to the CLI exactly as written. The entrypoint
	// is the CLI itself, so this is everything after the command name and it
	// never names the command.
	args []string,
	// The project to run over, mounted at /src and made the working directory.
	//
	// It is optional because half the reason this function exists is the
	// invocations that have no project — `--version` and `--help` read nothing and
	// contact nothing. With no source the container is the image as it was
	// resolved, and cpybkc runs in whatever directory the image left it in.
	// +optional
	source *dagger.Directory,
) (*dagger.Container, error) {
	container := m.Container
	if source != nil {
		mounted, err := m.project(ctx, source)
		if err != nil {
			return nil, err
		}

		container = mounted
	}

	return container.WithExec(args, dagger.ContainerWithExecOpts{UseEntrypoint: true}), nil
}

// project mounts a caller's project at [projectDir] and stands in it.
//
// The mount is owned by whoever the container runs as, asked of the container
// rather than assumed, so that a caller who passed --image built on a derived
// image with its own user still gets a project their process can write into. A
// container that reports no user at all falls back to [contractUser], which is
// what the base-image contract pins and what an image satisfying it runs as; the
// alternative, leaving the mount root-owned, is a run that fails on the first
// file a generator writes.
func (m *Cpybkc) project(ctx context.Context, source *dagger.Directory) (*dagger.Container, error) {
	user, err := m.Container.User(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the user the cpybkc image runs as, which the mounted project has to be "+
			"writable by: %w", err)
	}

	if user == "" {
		user = contractUser
	}

	return m.Container.
		WithDirectory(projectDir, source, dagger.ContainerWithDirectoryOpts{Owner: user}).
		WithWorkdir(projectDir), nil
}
