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
// GoLib rather than GoApp, even now that cmd/cpybkc-gen-go is a main package
// (#48), the image is built (#55) and it is published (#59). What GoApp adds
// over GoLib is a multi-platform image build and the publish half of the
// standard, and the image story expected to reach the image by switching
// factories. It did not, and image.go's own comment says why at length: GoApp
// publishes one image per binary, and cpybkc publishes a *base* — a directory on
// PATH that other people's images copy into, owned by a pinned non-root user.
// Four of the six promises docs/container/SPEC.md makes are that shape rather
// than settings GoApp is missing.
//
// So the factory stays GoLib, and the four check stages still gate a pull
// request and still cover cmd/ because they run over ./... . The image is built
// by this module in image.go and published by it in release.go — the shape avroc
// arrived at for the same reason (avroc#166, avroc#168). The publish half went
// the same way as the image half once the image did: what a release pushes is
// the base this module assembles, under tags this module derives, and GoApp has
// no notion of either.
//
// # Why the release is decided here too
//
// release.go decides whether a commit is a release, which tags it carries and
// what its notes say about the image (#59). That decision is in the module and
// not in .github/workflows/release.yaml because the alternative is the tag scheme
// written down a second time, in a file that runs once per release and is
// exercised nowhere else. TagScheme and ReleaseNotesContract are in Ci for the
// same reason the artifact builds are: a recipe whose first real run is on a tag
// is one whose failure is a release that did not happen.
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
// # Why the standard is invoked twice
//
// This repository holds two Go modules: the CLI at the root and the published IR
// module at irpb/. A nested go.mod is where `go test ./...` stops, so the source
// directory that reaches one of them cannot reach the other, and IrCi is the
// second invocation that covers the second module. It is the standard again and
// not a variation on it — same archetype, same lint configuration — because the
// module the third-party generators actually import is the last place this
// repository should be checking something bespoke.
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
// # Why signing is here too, and why only half of it is checked
//
// Attest signs a published digest and attaches its provenance and SBOMs (#58),
// and sign.go carries the argument for what is attached to what. It is on this
// module for the reason the artifact builds are: a recipe that only ever ran
// inside .github/workflows/release.yaml would first run on a tag, where a
// failure is a release that did not happen.
//
// Only half of it can be a check. Producing a real signature needs an OIDC
// token, a registry and a public transparency log, none of which a pull request
// has, so Attestations checks what is a function of this repository — the
// provenance predicate's shape and the SBOM set — and says outright that the
// signature itself is first exercised by a release.
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
// the Z5Labs standard defines them, over each of this repository's two Go
// modules, plus `buf lint` over the IR schema, a build of the CLI itself, a
// build of the three artifacts a release publishes, the published base image on
// every platform it ships for, the worked examples docs/container/SPEC.md hands
// an adopter, the attestations a release attaches to what it publishes, the tag
// scheme and release notes a release is published under, and the companion
// module's coverage of the CLI's flags. This is the single entrypoint — CI is
// one `dagger call ci` and stays one, because a workflow step that reran any of
// these stages would be a second definition of them.
//
// The fourteen parts run concurrently and all are reported, for the reason the
// standard runs its own four that way: waiting on a Go stage to learn that the
// schema is unlintable, or the reverse, is a second push to find out about the
// second failure.
//
// +check
// +cache="session"
func (m *Cpybkc) Ci(ctx context.Context) error {
	var goErr, irErr, protoErr, buildErr, artifactErr, layoutErr, imageErr, exampleErr, attestErr error
	var tagErr, notesErr, companionErr, engineErr, surfaceErr error

	var wg sync.WaitGroup
	wg.Add(14)

	go func() {
		defer wg.Done()
		goErr = dag.Z5Labs().
			GoLib(m.Source, dagger.Z5LabsGoLibOpts{LintConfig: m.LintConfig}).
			Ci(ctx)
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
		attestErr = m.Attestations(ctx)
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

	wg.Wait()

	return errors.Join(goErr, irErr, protoErr, buildErr, artifactErr, layoutErr, imageErr, exampleErr, attestErr,
		tagErr, notesErr, companionErr, engineErr, surfaceErr)
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
	return dag.Z5Labs().
		GoLib(m.Source.Directory("irpb"), dagger.Z5LabsGoLibOpts{LintConfig: m.LintConfig}).
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
// wants. Every published image carries the same build with a platform argument
// (image.go's binary), so there is one recipe for the executable and not one per
// destination.
func (m *Cpybkc) Binary() *dagger.File {
	return m.binary("")
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

	return checkVersionLine(line)
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

// checkVersionLine reports whether line is what `cpybkc --version` writes.
//
// Only the line's shape. What it says is cmd/cpybkc's own test's business, and
// asserting the version here would pin a release number in the pipeline.
//
// It is shared with the image contract, which runs the same invocation through
// the published image's entrypoint (#55): what "this is cpybkc answering" means
// is a property of the line rather than of which container it came out of, and a
// second spelling would be a second, weaker answer.
func checkVersionLine(line string) error {
	if !strings.HasPrefix(line, cliBinary+" ") || !strings.Contains(line, "IR version") {
		return fmt.Errorf("cpybkc --version wrote %q, and the line names the program, its version and the IR "+
			"version this build produces", line)
	}

	return nil
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
