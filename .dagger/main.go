// Package main implements cpybkc's root Dagger module: the one definition of
// this repository's pipeline, called by CI and by contributors alike.
//
// # What this module still owns, now that the archetype builds the image
//
// This module used to build cpybkc's published base image itself, sign it and
// derive its tag family, and about 2,000 of its lines existed for that reason
// alone. The argument was recorded here and in image.go at length: the Z5Labs
// standard pipeline's GoApp archetype published one image per binary — a scratch
// image holding one executable, with no PATH, no user and no second directory —
// and cpybkc publishes a *base*, a directory on PATH that other people's images
// copy into, owned by a pinned non-root user (#55, #58, #59).
//
// That premise is gone (#185). github.com/z5labs/devex/daggerverse/z5labs is now
// a chainable Go -> App -> Publish API, and every one of the things this
// repository needed is the archetype's own: /usr/local/bin is its fixed plugin
// directory with PATH composed from the same constant, 65532:65532 is its pinned
// non-root user, App.WithFile and App.WithDirectory contribute content that
// carries a document, and App.Publish derives the tag family, pushes one
// multi-platform index under every tag of it, signs the result recursively and
// attaches provenance and SBOMs. So the image, the publish, the tags and the
// signing are all the standard's, and this module states the version and the
// repository and gets out of the way.
//
// Four things did not move, and each is a fact about *this* project rather than
// about how an image gets built:
//
//   - The contract checks. ImageContract, WorkedExample, CompanionModule,
//     CliSurface, EngineLock, IrArtifacts, LayoutArtifact, IrDescriptorSet,
//     IrProtos, LayoutSchema, ProtoLint, ProtoGen and Build are assertions about
//     docs/container/SPEC.md, docs/plugin/SPEC.md and docs/cli/SPEC.md, and every
//     one of them holds against a container whoever built it. They are also the
//     evidence that adopting the archetype changed nothing a consumer can see
//     that this change did not choose to change, which is the only thing that
//     makes a change of this size reviewable.
//   - Whether this commit is a release. The archetype takes a version from its
//     caller and derives the tag family from it; reading the refs at HEAD to
//     decide whether there is a release here at all, refusing two version tags
//     and refusing `+build` metadata stay in release.go, because they are facts
//     about this repository's release process. TagScheme is what checks them.
//   - The release notes. ReleaseNotes, ReleaseNotesContract and the IR version
//     they state have no counterpart upstream and gain none: which IR version the
//     published image speaks is the one fact about a release a reader cannot
//     recover from its tag.
//   - Which four Go modules this repository holds, and that the IR schema under
//     proto/ is linted. The archetype has no opinion about either.
//
// # Why the release is decided here too
//
// release.go decides whether a commit is a release and what its notes say about
// the image (#59, #185). That decision is in the module and not in
// .github/workflows/release.yaml because the alternative is the release rule
// written down a second time, in a file that runs once per release and is
// exercised nowhere else. TagScheme and ReleaseNotesContract are in Ci for the
// same reason the artifact builds are: a recipe whose first real run is on a tag
// is one whose failure is a release that did not happen.
//
// # Why the stage functions exist alongside it
//
// GoChain.Ci exposes only the whole pipeline, and waiting on four stages to learn
// that one file is unformatted is not the loop to develop in. So Fmt, Vet, Lint
// and Test are here too. They are not a second implementation: each one drives
// the same github.com/z5labs/devex/daggerverse/go builder that the standard
// pipeline drives, with one stage enabled instead of four, against the same lint
// configuration. Both dependencies are pinned to a single devex commit for that
// reason — a bump has to move them together, or a stage run on its own stops
// being the stage Ci runs.
//
// # Why .git is bound apart from the source
//
// The archetype needs real git metadata: Go.App stamps the short HEAD SHA into
// every binary, annotates every image with the commit, its committer time and the
// origin, and calls requireGitWorkingTree before it builds anything. The check
// stages need the opposite — a tree that does not change when a commit is made,
// or fmt, vet, lint and test miss their cache on every commit.
//
// So .git is not in Source and is not folded back into it. It is New's own
// argument, with an ignore list of its own, and appSource is the one place the
// two are put together.
//
// The cost is a git *worktree*, whose .git is a file rather than a directory.
// Dagger resolves a +defaultPath argument when it constructs the module, before
// it knows which function was asked for, so *every* call fails there — not only
// the ones that build an image. CONTRIBUTING.md says so, because this
// repository's own backlog cycle develops in worktrees under .claude/worktrees/,
// and the remedy is an ordinary clone rather than a flag: what a worktree would
// have to hand over is the main repository's .git, which is a build identity that
// is not this tree's.
//
// # Why ProtoLint is not one of them
//
// ProtoLint is this repository's own, not the standard's: the Z5Labs Go
// pipeline has no protobuf stage to wrap, and the IR schema under proto/ is a
// contract third parties build against, so it is checked. It runs buf against
// the buf.yaml committed here, for the reason Lint is passed this repository's
// .golangci.yml — a configuration CI ignored would be a file contributors read
// and trusted while the pipeline enforced something else.
//
// Ci runs it alongside the standard rather than beside it in the workflow. A
// second `dagger call` in .github/workflows/ci.yaml would have been the other
// shape, and it costs the one property this module exists for: `dagger call ci`
// is the gate a contributor runs, and a check CI performs that the gate does
// not is an arrangement of local commands that passes while CI fails.
//
// # Why the standard is invoked four times
//
// This repository holds four Go modules: the CLI at the root, the published IR
// module at irpb/, the companion Dagger module at daggerverse/cpybkc/, and this
// pipeline itself at .dagger/. A nested go.mod is where `go test ./...` stops,
// so the source directory that reaches one of them reaches none of the others,
// and IrCi, CompanionCi and PipelineCi are the three further invocations that
// cover the three further modules. Each is the standard again and not a
// variation on it — same archetype, same lint configuration — because the module
// third-party generators import, the module strangers call, and the pipeline
// that judges everything else are the last three places this repository should
// be checking something bespoke.
//
// ProtoGen is the odd one out: it is not a check at all but the generator that
// produces irpb/ir.pb.go, kept here because a generation recipe living anywhere
// else is a second answer to how the committed stubs were made.
//
// # Why the CLI is built here as well as compiled
//
// The four check stages already compile cmd/cpybkc, because they run over
// ./... . Build is here for the two things they say nothing about: that the
// binary links CGO-free and that it is a single static file. Both are promises
// docs/container/SPEC.md rests the published image on — the image carries the
// executable and nothing a program needs to start — and neither is visible to
// `go vet` or to a test, because the toolchain container has the loader and the
// libc that the image will not.
//
// So Build compiles it with CGO off and runs it in an empty image, which is the
// smallest thing that can tell a static binary from a dynamic one without
// reading its headers. It lands with the CLI (#147) rather than with the image
// (#55) because it is a claim about the binary, and the binary exists first.
//
// # Why the image is built here too
//
// ImageContract builds the published base image on every platform this project
// ships for and checks it against docs/container/SPEC.md (#55). It is in Ci for
// the reason the artifact builds are: the image is a public contract named by
// path from repositories this project cannot see, so the pull request that moves
// a path is where that has to fail. image.go carries the argument for why the
// image is assembled in this module rather than by the standard's app archetype.
//
// # Why the release artifacts are built here
//
// IrDescriptorSet and IrProtos produce two of the three files a release
// attaches — the IR's FileDescriptorSet and the schema sources (#19) — and
// LayoutSchema produces the third, the published layout schema a shop's layout
// generator targets (#23). They are functions on this module for the same reason
// ProtoLint is: a recipe that only ever ran inside
// .github/workflows/release.yaml would be a build nobody can reproduce locally
// and one that first runs on a tag, where a failure is a release that did not
// happen. Here, `dagger call ir-descriptor-set` is a command a contributor runs,
// and IrArtifacts and LayoutArtifact put all three builds inside Ci so the
// recipes are exercised on every pull request rather than once per release.
//
// They are also the seam the container work builds on: #57 copies the same file
// into the published image, from this function rather than from a second
// invocation of protoc, which is what makes the release asset and the in-image
// copy two ways of getting one artifact.
//
// # Why nothing here checks the signature or the attestations any more
//
// Attest, Attestations, Sbom and Provenance are gone with sign.go (#185). Every
// published digest still carries a recursive signature, a signed SLSA provenance
// statement and an SBOM per platform — App.Publish refuses a run it cannot
// produce provenance for — but none of that is this repository's to render any
// more, so there is no predicate of its own left to check the shape of. The
// documents are checked upstream, by the archetype's own selftests, against the
// code that writes them; a table here would be a second statement of somebody
// else's rule, and the way that fails is the copy staying green after the
// original moved. What a consumer runs to find them is docs/container/SPEC.md's,
// and that document changed in the same commit.
//
// # Why a document is one of the stages
//
// WorkedExample builds the Dockerfile docs/container/SPEC.md hands an adopter
// (#54), and its own file says why at length. The short reason it belongs in Ci
// rather than in a release step is the one above turned around: that example is
// the first thing somebody outside this repository runs and the last thing
// anybody inside it would notice was broken, because no other stage reads it.
//
// Ci is what CI calls and what a contributor should run before pushing. The
// stage functions are for narrowing down what Ci reported.
package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"dagger/cpybkc/internal/dagger"
)

// The buf release ProtoLint runs, pinned by tag and by digest. The tag says
// which release it is and the digest is what actually resolves, so a retagged
// image cannot change what this repository lints against without the change
// appearing in a diff — the same promise dagger.json's dependency pins make.
const bufImage = "bufbuild/buf:1.72.0@sha256:65bd496a89c762ad7151ca9e7d885a45dacb3671a8e8ec39738b9f844d3405ea"

const (
	// cliPackage is the main package Binary builds, and cliBinary is what the
	// build is asked to call it. The name is the command's own — it is what a
	// person types, what docs/cli/SPEC.md's synopsis writes and what the
	// published image's entrypoint is — so it is stated rather than left to
	// `go build`'s naming.
	cliPackage = "./cmd/cpybkc"
	cliBinary  = "cpybkc"

	// cliBinaryPath is where Build puts it in the empty image it runs it in.
	// The image has no PATH to find it on, which is the point of running it
	// there at all.
	cliBinaryPath = "/cpybkc"
)

// Cpybkc is the root module type. Source and the lint configuration are bound
// once at construction so every function below checks the same tree the same
// way; there is no per-function source argument that could point somewhere else.
type Cpybkc struct {
	// +private
	Source *dagger.Directory
	// +private
	LintConfig *dagger.File
	// GitDir is the repository's .git, bound apart from Source so that the
	// check stages never see it. Only appSource folds it back in.
	// +private
	GitDir *dagger.Directory
}

// New binds the repository to the pipeline.
//
// source defaults to the repository root, so `dagger call ci` from a checkout
// needs no arguments. The ignore list drops what no check stage reads: .git is
// excluded because none of fmt, vet, lint or test looks at git metadata, and
// leaving it in would make every commit a cache miss for all four.
//
// It stays excluded now that the archetype builds the image (#185), rather than
// coming off the list as the comment here used to predict. The archetype does
// need real git metadata — Go.App refuses a tree without it, stamps the short
// HEAD SHA into every binary and annotates every image with the commit, its
// committer time and the origin — but it needs it on the one path that builds an
// App, and folding .git into Source would have bought that by making fmt, vet,
// lint and test miss their cache on every commit. gitDir is that metadata bound
// as its own input, and appSource is the only thing that puts the two back
// together.
//
// gitDir defaults to the repository's own .git. It is a *directory* rather than
// something derived, because git's own answers are what the archetype stamps and
// anything this module computed would be a build identity a caller could have
// supplied. Note that a git *worktree* has a .git **file** rather than a
// directory, so the default path resolves to nothing there and every call fails
// on it — see the package comment. CONTRIBUTING.md says to run from an ordinary
// clone.
//
// Its own ignore list is the other half of the caching argument, and it is not
// decoration. Everything read out of here is a function of the commit — HEAD, the
// refs pointing at it, the commit object's committer time, and the origin in
// config — while a .git directory also holds a great deal that is not: the reflog
// under logs/, FETCH_HEAD and ORIG_HEAD, the staging index, and the hooks. Folded
// in whole, an ordinary `git fetch` moves every one of those and the image stages
// miss their cache for reasons nothing in the tree explains. What is excluded is
// chosen to be things none of the four readers above touches, so dropping them
// costs nothing.
//
// It does **not** make two builds of one commit hash identically in general, and
// claiming that would be claiming more than a list of exclusions can deliver.
// objects/ has to stay — HEAD's own commit object is in it — so a `git gc` that
// repacks, or a fetch that adds a pack, still moves this input; so do packed-refs
// and refs/remotes/**, which a fetch of any branch rewrites. What the list buys is
// the cheapest and most frequent churn, which is the churn a contributor generates
// by working: the reflog moves on every checkout and commit, and the index on
// every `git add`.
//
// lintConfig defaults to the repository's own .golangci.yml. It is passed
// explicitly rather than left to the standard pipeline's bundled default so that
// the configuration committed to this repository is the configuration CI lints
// against — a .golangci.yml that CI ignored would be a file contributors read
// and trusted while the pipeline enforced something else.
//
// That file is written in the golangci-lint **v2** dialect, because v2 is the
// major the standard pipeline runs and a v2 binary refuses a v1 file outright,
// before any linter runs. The tool pin is the shared Go module's, and nothing
// here restates it.
func New(
	// +optional
	// +defaultPath="/"
	// +ignore=[".git", ".claude", "/bin", "/dist"]
	source *dagger.Directory,
	// +optional
	lintConfig *dagger.File,
	// +optional
	// +defaultPath="/.git"
	// +ignore=["logs", "hooks", "index", "FETCH_HEAD", "ORIG_HEAD", "COMMIT_EDITMSG"]
	gitDir *dagger.Directory,
) *Cpybkc {
	if lintConfig == nil {
		lintConfig = source.File(".golangci.yml")
	}

	return &Cpybkc{Source: source, LintConfig: lintConfig, GitDir: gitDir}
}

// goChain is the standard Go chain bound to a source tree and configured the one
// way this repository is checked.
//
// It is one helper rather than a call at each site because all four invocations
// have to agree about the lint configuration and about the race detector: a chain
// configured four times is four definitions of what "checked" means here, which
// is the thing wrapping the standard exists to prevent.
//
// WithTest(true) is written out rather than left to the default. The archetype's
// race detector is on unless a caller says otherwise, so the argument is redundant
// today and is stated anyway — "the race detector is on because this repository
// asked for it" is a different claim from "because nobody turned it off".
//
// The chain carries no .git: none of the four check stages reads git metadata,
// and the ignore list on New exists to keep them cache-stable. appChain is where
// the metadata arrives.
func (m *Cpybkc) goChain(source *dagger.Directory) *dagger.Z5LabsGoChain {
	return dag.Z5Labs().
		Go(source).
		WithLint(dagger.Z5LabsGoChainWithLintOpts{Config: m.LintConfig}).
		WithTest(true)
}

// appSource is the source tree an App is built from: this repository's, with its
// git metadata folded back in.
//
// This is the only place the two are put together, and that is the whole of
// #185's answer to a problem New's old comment predicted: the archetype stamps
// binaries and annotates images from the refs at HEAD, so it needs real git
// metadata, and the check stages need a tree that does not change when a commit
// is made. One argument, folded in on one path, gives both.
func (m *Cpybkc) appSource() *dagger.Directory {
	// A nil GitDir is handed on rather than dereferenced. It is not reachable
	// from the command line — the argument carries a default path — but it is
	// reachable from a module-to-module call and from any struct literal, and
	// Directory.WithDirectory asserts its argument is non-nil and *panics*, a
	// long way from whoever left it out. Returning the source unchanged instead
	// puts the failure where it belongs: the archetype refuses a tree with no
	// .git in it, in its own words, naming what it was looking for.
	//
	// That is also the message somebody running an image stage from a git
	// worktree gets, where .git is a file and the default path resolves to
	// nothing useful.
	if m.GitDir == nil {
		return m.Source
	}

	return m.Source.WithDirectory(".git", m.GitDir)
}

// Ci runs the whole pipeline: fmt, vet, golangci-lint and `go test -race`, as
// the Z5Labs standard defines them, over each of this repository's four Go
// modules, plus `buf lint` over the IR schema, a build of the CLI itself, a
// build of the three artifacts a release publishes, the published base image on
// every platform it ships for, the worked examples docs/container/SPEC.md hands
// an adopter, the release decision and the release notes a release is published
// under, the companion module's coverage of the CLI's flags, and that module's
// own functions driven over the image this run just built. This is the single entrypoint — CI is
// one `dagger call ci` and stays one, because a workflow step that reran any of
// these stages would be a second definition of them.
//
// The fifteen parts run concurrently and all are reported, for the reason the
// standard runs its own four that way: waiting on a Go stage to learn that the
// schema is unlintable, or the reverse, is a second push to find out about the
// second failure.
//
// It was sixteen until #185. Attestations checked the provenance predicate and
// the SBOM set this module used to render, and both are the archetype's now, so
// the stage went with sign.go rather than being kept as a check on somebody
// else's output.
//
// ImageContract is one of the fifteen, and since #185 that stage builds an App —
// so this call needs real git metadata and does not run from a git worktree. See
// New.
//
// +check
// +cache="session"
func (m *Cpybkc) Ci(ctx context.Context) error {
	var goErr, irErr, protoErr, buildErr, artifactErr, layoutErr, imageErr, exampleErr error
	var tagErr, notesErr, companionErr, engineErr, surfaceErr, moduleErr, pipelineErr error

	var wg sync.WaitGroup
	wg.Add(15)

	go func() {
		defer wg.Done()
		goErr = m.goChain(m.Source).Ci(ctx)
	}()

	go func() {
		defer wg.Done()
		irErr = m.IrCi(ctx)
	}()

	go func() {
		defer wg.Done()
		protoErr = m.ProtoLint(ctx)
	}()

	go func() {
		defer wg.Done()
		buildErr = m.Build(ctx)
	}()

	go func() {
		defer wg.Done()
		artifactErr = m.IrArtifacts(ctx)
	}()

	go func() {
		defer wg.Done()
		layoutErr = m.LayoutArtifact(ctx)
	}()

	go func() {
		defer wg.Done()
		// Every published platform, because the promise is about the index a
		// consumer pulls from and not about the one architecture this run
		// happens to be on.
		imageErr = m.ImageContract(ctx, "")
	}()

	go func() {
		defer wg.Done()
		exampleErr = m.WorkedExample(ctx)
	}()

	go func() {
		defer wg.Done()
		tagErr = m.TagScheme()
	}()

	go func() {
		defer wg.Done()
		notesErr = m.ReleaseNotesContract()
	}()

	go func() {
		defer wg.Done()
		companionErr = m.CompanionCi(ctx)
	}()

	go func() {
		defer wg.Done()
		engineErr = m.EngineLock(ctx)
	}()

	go func() {
		defer wg.Done()
		surfaceErr = m.CliSurface(ctx)
	}()

	go func() {
		defer wg.Done()
		moduleErr = m.CompanionModule(ctx)
	}()

	go func() {
		defer wg.Done()
		pipelineErr = m.PipelineCi(ctx)
	}()

	wg.Wait()

	return errors.Join(goErr, irErr, protoErr, buildErr, artifactErr, layoutErr, imageErr, exampleErr,
		tagErr, notesErr, companionErr, engineErr, surfaceErr, moduleErr, pipelineErr)
}

// IrCi runs the same standard pipeline over irpb/, the published IR module.
//
// It is a second call rather than a wider source directory because irpb/ is a
// separate Go module. `go test ./...` from the repository root stops at a nested
// go.mod and never descends into it, so without this stage the module third
// parties actually import — including the smoke test asserting they can — is the
// one part of the tree nothing checks. Two modules, two invocations of the
// standard; that is the cost of the boundary irpb/doc.go exists to keep.
//
// It is handed the same .golangci.yml, so the two modules are linted against one
// configuration rather than one each.
//
// +check
// +cache="session"
func (m *Cpybkc) IrCi(ctx context.Context) error {
	return m.goChain(m.Source.Directory("irpb")).Ci(ctx)
}

// pipelineModuleDir is this pipeline's own Go module — the one whose functions
// you are reading. It is a module rather than a package of the repository, so
// `go test ./...` from the root stops at its go.mod exactly as it stops at
// irpb/'s and the companion's.
const pipelineModuleDir = ".dagger"

// PipelineCi runs the standard Go pipeline over the pipeline's own module.
//
// It is a fourth call for the reason IrCi is a second and CompanionCi a third: a
// nested go.mod is where `go test ./...` stops. Until this existed, the pipeline
// that checks everything else in the repository was the one Go module nothing
// checked — and what made that worth fixing rather than noting is #62's
// CliSurface, whose reading of the CLI's flag constants lives in
// internal/surface precisely so that it can be tested, and whose tests would
// otherwise have run on a contributor's machine and nowhere else. A drift guard
// nothing exercises is a drift guard nobody finds out has stopped working.
//
// What it really runs is that package. The module's own package main imports the
// generated Dagger client, whose init panics without a session, so a test beside
// main cannot run under plain `go test` here any more than it can in the
// companion; keeping the readable part in a package that imports no Dagger is
// what turns these rules into something a test pins.
//
// It is handed the same .golangci.yml, so all four Go modules are linted against
// one configuration rather than one each.
//
// +check
// +cache="session"
func (m *Cpybkc) PipelineCi(ctx context.Context) error {
	return m.goChain(m.Source.Directory(pipelineModuleDir)).Ci(ctx)
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

// ProtoLint runs `buf lint` over proto/ against this repository's buf.yaml.
//
// buf rather than protoc: the schema's problems are naming and layout ones a
// compiler has no opinion about, and the rule that made this worth wiring up is
// ENUM_ZERO_VALUE_SUFFIX — an enum whose zero value is a real member is an
// encoding axis a producer can leave unset and a consumer cannot tell from one
// that was resolved, which is the failure docs/ir/SPEC.md's "The encoding
// profile, applied" spends a section refusing.
//
// The whole source is mounted rather than just proto/, because buf.yaml sits at
// the repository root and names the module below it. Nothing else in the tree
// is read.
//
// +check
// +cache="session"
func (m *Cpybkc) ProtoLint(ctx context.Context) error {
	_, err := dag.Container().
		From(bufImage).
		WithMountedDirectory("/src", m.Source).
		WithWorkdir("/src").
		WithExec([]string{"buf", "lint"}).
		Sync(ctx)
	return err
}

// ProtoGen regenerates the Go IR stubs from proto/ and returns irpb/ as it
// should look afterwards. Export it over the working tree to apply it:
//
//	dagger call proto-gen export --path=irpb
//
// The generated file is committed, like .dagger/'s codegen and for the same two
// reasons: the module has to build from a checkout alone, and a published module
// whose sources exist only after somebody has run a generator is not a module
// anyone can `go get`. What buf writes is decided by buf.gen.yaml, which pins the
// protoc-gen-go release; this function only supplies the container and the
// source, so `dagger call proto-gen` and a contributor's own `buf generate`
// cannot disagree.
//
// It returns the whole directory rather than the one .pb.go file so that a
// second message file added to proto/ arrives here without this signature
// changing. Exporting it is therefore a merge over irpb/, not a replacement of
// it — go.mod, doc.go and the tests are in the returned directory unchanged,
// because they were in the mounted source.
//
// The result is a Directory and not a check: whether the committed stubs match
// this output is a question for review of the same commit, in the way
// CONTRIBUTING.md already asks it of .dagger/. Ci does not regenerate, because a
// check that rewrites the tree it is checking is a pipeline stage with an
// opinion about your working directory.
func (m *Cpybkc) ProtoGen() *dagger.Directory {
	return dag.Container().
		From(bufImage).
		WithMountedDirectory("/src", m.Source).
		WithWorkdir("/src").
		WithExec([]string{"buf", "generate"}).
		Directory("/src/irpb")
}

// IrDescriptorSet builds the IR's protobuf FileDescriptorSet — the published
// ir.binpb — by compiling this repository and asking irpb for it:
//
//	dagger call ir-descriptor-set export --path=ir.binpb
//
// It is what the release workflow attaches to a release, and — the same node,
// not a second build of it — what image.go copies into the published image at
// /usr/local/share/cpybkc/ir.binpb (#57). Both forms are two ways of getting one
// artifact rather than two artifacts, which only holds while there is one
// recipe; this is it, and ImageContract compares the two on every run.
//
// It is a function on this module rather than steps in a workflow for the reason
// .github/workflows/ci.yaml already gives: repo-specific work lands here and is
// invoked with `dagger call`, so a contributor produces the same bytes CI does
// with the same command, and there is no second recipe living in YAML that
// nobody can run locally.
//
// dag.Go().Container is the escape hatch the standard Go module offers for
// exactly this — a command its typed helpers do not cover. Using it rather than
// a container of this module's own keeps the toolchain version read out of
// go.mod, where this repository's toolchain version already lives, instead of
// pinning a golang: tag here that could drift from it.
//
// The bytes are a function of the schema: irpb.MarshalFileDescriptorSet encodes
// deterministically and the file order is fixed by the protos' imports, so two
// runs over an unchanged tree produce an identical artifact. That is what makes
// the published file comparable across releases at all.
func (m *Cpybkc) IrDescriptorSet() *dagger.File {
	const out = "/out/ir.binpb"

	return dag.Go().
		Container(m.Source).
		WithExec([]string{"go", "run", "./internal/tools/ir-descriptor-set", "-o", out}).
		File(out)
}

// IrProtos builds the IR schema sources — the published ir-protos.tar.gz —
// preserving each file's path under proto/:
//
//	dagger call ir-protos export --path=ir-protos.tar.gz
//
// It is the other artifact a release carries, and it is for the consumer
// IrDescriptorSet is not for: the one whose build can run protoc and would
// rather generate bindings than decode dynamically. internal/tools/ir-protos
// carries the argument for why it is an archive and not the one .proto file.
//
// The published image carries the same sources unpacked, under
// /usr/local/share/cpybkc/proto/ (#57) — an archive would be one a stage with no
// shell could not open, and the layout inside it is exactly the include root the
// image needs anyway.
//
// Its bytes are a function of the schema too — every tar field the filesystem
// could have supplied is a constant and the entries are sorted — so the same
// commit archives to the same artifact.
func (m *Cpybkc) IrProtos() *dagger.File {
	const out = "/out/ir-protos.tar.gz"

	return dag.Go().
		Container(m.Source).
		WithExec([]string{"go", "run", "./internal/tools/ir-protos", "-o", out}).
		File(out)
}

// IrArtifacts builds both release artifacts and fails if either is empty.
//
// It is in Ci so that the recipe a release runs is exercised on every pull
// request. Without it the only thing that ever runs `go run
// ./internal/tools/ir-descriptor-set` in a container is the release workflow, on
// a tag, once — and a build tool that has never been run outside a test is one
// whose first real invocation is the one nobody can retry.
//
// The Go tests own the artifacts' contents and their determinism, which is why
// this asserts nothing about either. What it asserts is the part they cannot:
// that the two commands run to completion in a container built from go.mod and
// leave a file behind. An empty file is the failure worth naming, because it is
// what a tool that wrote its parent directory and exited would produce, and it
// uploads as happily as a good one.
//
// +check
// +cache="session"
func (m *Cpybkc) IrArtifacts(ctx context.Context) error {
	artifacts := map[string]*dagger.File{
		"ir.binpb":         m.IrDescriptorSet(),
		"ir-protos.tar.gz": m.IrProtos(),
	}

	names := make([]string, 0, len(artifacts))
	for name := range artifacts {
		names = append(names, name)
	}

	// Sorted, so that two runs failing on two different artifacts report them in
	// the same order rather than in whichever order the map produced.
	slices.Sort(names)

	var errs []error

	for _, name := range names {
		size, err := artifacts[name].Size(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to build %s: %w", name, err))

			continue
		}

		if size == 0 {
			errs = append(errs, fmt.Errorf("%s built to an empty file", name))
		}
	}

	return errors.Join(errs...)
}

// Binary builds the cpybkc executable itself — the one thing this repository
// exists to ship — CGO-free, so that it is a single statically linked file:
//
//	dagger call binary export --path=cpybkc
//
// CGO_ENABLED=0 is not an optimisation. docs/container/SPEC.md's image carries
// this binary and nothing else a program needs to start — no loader, no libc,
// no shell — so a dynamically linked cpybkc is one that cannot run in the image
// this project publishes (#55). -trimpath goes with it because the same
// document's reproducibility is a claim about the bytes, and a binary carrying
// the path it was compiled under is a build that depends on where it ran.
//
// It is a function on this module rather than steps in a workflow for the
// reason IrDescriptorSet is: a recipe that only ever ran inside a workflow
// would be a build nobody can reproduce locally.
//
// The engine's own platform, because that is what a contributor exporting one
// wants.
//
// It is no longer the recipe the published image uses. Since #185 the archetype
// compiles the CLI for every platform it publishes, with its own CGO, -trimpath
// and -ldflags switches — and ImageContract reads three of those back out of the
// executable that landed in the image, so the two agreeing is a check rather
// than an arrangement. What this function is still for is the loose executable:
// something a contributor exports and runs, and the input Build's empty-image
// smoke test needs.
func (m *Cpybkc) Binary() *dagger.File {
	return m.binary("")
}

// binary builds the CLI for one platform.
//
// The platform is a cross-compile by the toolchain container rather than a build
// under emulation. Nothing about a Go build needs the target's architecture to
// be executable, and paying qemu for every compile to learn what `go build`
// already knows would be minutes per platform per run.
func (m *Cpybkc) binary(platform dagger.Platform) *dagger.File {
	return dag.Go().
		Build(m.Source, dagger.GoBuildOpts{
			Pkg:          cliPackage,
			ArtifactName: cliBinary,
			Trimpath:     true,
			DisableCgo:   true,
			Platform:     string(platform),
		}).
		File(cliBinary)
}

// Build builds the CLI and runs it in an empty image.
//
// The Go stages already compile cmd/cpybkc — vet, lint and test all reach it
// through ./... — so what this adds is the two claims they cannot make. That
// the binary is CGO-free and statically linked, and that a single file is all
// of it: an image holding nothing but the executable has no loader to resolve
// an interpreter with and no libc to load, so a dynamically linked binary does
// not start, and the failure is the check rather than an image somebody
// publishes.
//
// --version is what it is run with because it is the one invocation
// docs/cli/SPEC.md requires to succeed without touching anything: it contacts
// nothing, reads no manifest, writes one line to standard output and exits 0.
// A binary that gets that far has linked, started and run its own main.
//
// One platform — the engine's own. Which platforms the published image carries
// is docs/container/SPEC.md's, and #55 is where that build is checked on each
// of them.
//
// +check
// +cache="session"
func (m *Cpybkc) Build(ctx context.Context) error {
	line, err := m.versionLine(ctx)
	if err != nil {
		return err
	}

	// devVersion, because this is the loose executable rather than the one the
	// archetype stamps: dag.Go().Build passes no -X at all, so what comes back is
	// the value cmd/cpybkc/version.go carries in the tree. Holding it to the
	// version the pipeline builds everything that is not a release under is what
	// keeps those two statements of "0.0.0-dev" from drifting apart — the source
	// default and this module's constant are the same string, and a build nobody
	// stamped has to be indistinguishable from an unreleased one that was.
	return checkVersionLine(line, devVersion)
}

// versionLine runs `cpybkc --version` in an image holding nothing but the
// binary, and returns what it wrote.
//
// Two callers want it for different reasons and one recipe serves both: Build
// wants to know that the binary starts at all in an image with no loader and no
// libc, and release.go's irVersion wants the IR version off the line, which is
// the number the release notes state. Reading that number out of the artifact is
// the whole point — a constant in the pipeline would be a second statement of
// what the assembler stamps.
func (m *Cpybkc) versionLine(ctx context.Context) (string, error) {
	line, err := dag.Container().
		WithFile(cliBinaryPath, m.Binary(), dagger.ContainerWithFileOpts{Permissions: 0o755}).
		WithExec([]string{cliBinaryPath, "--version"}).
		Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("cpybkc does not run in an image holding nothing but itself, which is the image it "+
			"ships in: %w", err)
	}

	return line, nil
}

// checkVersionLine reports whether line is what `cpybkc --version` writes for a
// build made under version.
//
// It is shared with the image contract, which runs the same invocation through
// the published image's entrypoint (#55): what "this is cpybkc answering" means
// is a property of the line rather than of which container it came out of, and a
// second spelling would be a second, weaker answer.
//
// # The version on the line, and why asserting it is not pinning a number
//
// This used to check the shape alone, on the argument that "asserting the
// version here would pin a release number in the pipeline". It would have, as a
// literal. What it takes instead is the version the build was *made for* — the
// caller's, devVersion for everything that is not a release and the release's
// own tag for the two containers Release is about to push — so nothing here
// names a release and the assertion still holds every build to the one number
// it was supposed to carry.
//
// That is the check #181 found missing. Since #185 the shared archetype
// cross-compiles the published CLI with `-X main.version=<version>`, and the
// linker silently ignores a stamp naming a constant: the pipeline appeared to be
// stamping a version while --version kept printing whatever the tree said, and
// v0.0.0 shipped identifying itself as an unreleased development build. Nothing
// compared the two, because the only two readings of the line in this module
// wanted its shape and its IR version.
//
// What this catches under devVersion is narrower than it looks, and worth
// stating exactly. devVersion is the same string the commands carry in their
// tree, so a stamp that landed and a stamp the linker dropped write the same
// line and both pass here. What fails is a reportedVersion that stopped
// dropping the tag's leading `v`, and a release whose binaries disagree with its
// tag. That the stamp lands *at all* is checked by building under a version
// nothing publishes — see checkVersionIsStamped, which is where the collision is
// escaped.
func checkVersionLine(line, version string) error {
	if !strings.HasPrefix(line, cliBinary+" ") || !strings.Contains(line, "IR version") {
		return fmt.Errorf("cpybkc --version wrote %q, and the line names the program, its version and the IR "+
			"version this build produces", line)
	}

	want := reportedVersion(version)

	if fields := strings.Fields(line); len(fields) < 2 || fields[1] != want {
		return fmt.Errorf("cpybkc --version wrote %q, and a build made under %s reports %q: the version a "+
			"release publishes is the version it was cut from, and a stamp the linker dropped looks exactly "+
			"like this", line, version, want)
	}

	return nil
}

// reportedVersion is the version a binary built under version reports.
//
// This module states a version as an OCI image tag — `v0.2.0`, because that is
// what docs/container/SPEC.md's tag table publishes, what planRelease reads off
// HEAD and what the archetype is handed — and both of this repository's
// commands report it as the SemVer 2.0.0 string their own contracts require,
// `0.2.0`. One leading `v` is the whole of the difference.
//
// This is the third spelling of that rule and the other two are in
// cmd/cpybkc/version.go and cmd/cpybkc-gen-go/version.go. They cannot be one:
// this module is a Go module of its own and cannot import either command, and
// the two commands cannot share it with each other because cmd/cpybkc-gen-go
// imports nothing of this repository beyond irpb by design. What pins the three
// together is the check above rather than an import — a spelling that drifted
// would fail the image contract on the next pull request, which is the same
// arrangement generatorRepository already lives under.
func reportedVersion(version string) string {
	return strings.TrimPrefix(version, "v")
}

// LayoutSchema builds the published layout schema — the released
// layout-schema.sexpr — by checking that this repository's schema loads and
// writing it out unchanged:
//
//	dagger call layout-schema export --path=layout-schema.sexpr
//
// It is the third asset a release attaches, and the one that is not about the
// IR. docs/layout/SPEC.md's "The published schema" is the contract: the set of
// form declarations a shop's layout generator targets, written in the notation
// it describes. An adopter validating what they generated runs cpybkc, which
// reads this same file.
//
// The bytes are the repository's schema/layout.sexpr byte for byte, because an
// adopter's diagnostic and this repository's have to be about the same text. So
// what the command adds is the check rather than a transformation, and
// internal/tools/layout-schema carries the argument for why that check happens
// on the way out as well as in the tests.
//
// It is a function on this module for the reason IrDescriptorSet is: a recipe
// that only ever ran inside .github/workflows/release.yaml would be a build
// nobody can reproduce locally and one that first runs on a tag, where a failure
// is a release that did not happen.
func (m *Cpybkc) LayoutSchema() *dagger.File {
	const out = "/out/layout-schema.sexpr"

	return dag.Go().
		Container(m.Source).
		WithExec([]string{"go", "run", "./internal/tools/layout-schema", "-o", out}).
		File(out)
}

// LayoutArtifact builds the layout schema and fails if it is empty.
//
// It is IrArtifacts' counterpart for the layout half of what a release carries,
// separate from it because the two are about different contracts and a run
// failing on one should say which. Everything IrArtifacts' comment says about
// why an artifact build belongs in Ci applies here unchanged, and the empty file
// is the same failure: it is what a tool that created its parent directory and
// exited would leave behind, and it uploads as happily as a good one.
//
// +check
// +cache="session"
func (m *Cpybkc) LayoutArtifact(ctx context.Context) error {
	size, err := m.LayoutSchema().Size(ctx)
	if err != nil {
		return fmt.Errorf("failed to build layout-schema.sexpr: %w", err)
	}

	if size == 0 {
		return fmt.Errorf("layout-schema.sexpr built to an empty file")
	}

	return nil
}

// stage returns the check builder the standard pipeline builds on, bound to
// this module's source and with no stage enabled yet. Callers enable the one
// they want. The Go toolchain version is left unset so the builder reads it from
// go.mod, which is where this repository's toolchain version lives and the only
// place it lives.
func (m *Cpybkc) stage() *dagger.GoCi {
	return dag.Go().Ci(m.Source)
}
