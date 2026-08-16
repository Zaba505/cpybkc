// Package main implements cpybkc's companion Dagger module: the published CLI
// image as a step in somebody else's pipeline, for a caller who would rather not
// write a Dockerfile.
//
//	dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
//	  with-generator --name hello --image ghcr.io/example/cpybkc-gen-hello:v1 \
//	  generate --source . export --path .
//
// Nothing is installed on the host and no Dockerfile is written: the published
// images are pulled, composed, and run.
//
// # An interface of its own, and why it still gets no SPEC.md
//
// This module is the cpybkc CLI daggerized (#253): a caller who reaches for it
// should be able to do what `cpybkc` does, by name. That makes it something a
// third party builds against rather than a convenience they could equally well
// do without — a function name, an argument and a default here are public API
// for as long as the directory this module is published under exists, which is
// for as long as the directory exists at all.
//
// It gets no SPEC.md all the same, and the reason is not that there would be
// nothing to specify. It is that everything there would be to specify is
// specified already: what a flag means is docs/cli/SPEC.md's, what the image
// promises is docs/container/SPEC.md's, and what a generator is handed is
// docs/plugin/SPEC.md's. A document here would be a second reading of those
// three, in another vocabulary and on another schedule, which is the drift the
// mapping is checked for rather than a defence against it. What is this module's
// own is the mapping — which function carries which flag — and that is written
// down in this repository's pipeline, where `dagger call cli-surface` fails on
// it, rather than in a document nothing can check (docs/CONVENTIONS.md, "What
// belongs here").
//
// Everything here can still be written by hand as
// `docker run --rm ghcr.io/zaba505/cpybkc:v0` or as a `COPY --from`, and a
// caller who prefers either is not on a lesser path. What the stance changes is
// what it means when the CLI can do something this module cannot: that is a gap
// to be filed, not a curation working as designed.
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
// # Composing a generator, and the Dockerfile it replaces
//
// The base image carries the CLI and no generator at all, so an image that
// generates anything is a composition. WithGenerator is that composition:
//
//	dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
//	  with-generator --name hello --image ghcr.io/example/cpybkc-gen-hello:v1 \
//	  generate --source . export --path .
//
// and the two lines it stands for are the final stage of
// docs/container/SPEC.md's worked example, written by hand:
//
//	FROM ghcr.io/zaba505/cpybkc:v0
//	COPY --from=ghcr.io/example/cpybkc-gen-hello:v1 --chown=65532:65532 --chmod=0755 \
//	     /usr/local/bin/cpybkc-gen-hello /usr/local/bin/cpybkc-gen-hello
//
// They are the same two instructions and the comparison is the point: the
// mechanism is `COPY --from`, this is not a second way of extending cpybkc, and
// a caller who prefers the Dockerfile is not on a lesser path. What the module
// saves is the build context, the registry to push the derived image to, and a
// Dockerfile to keep in step with a cpybkc release — a project whose manifest
// names three generators is three calls and one image that is never pushed
// anywhere.
//
// Two differences from that COPY line are deliberate rather than incidental.
// The module sets no owner where the Dockerfile writes `--chown=65532:65532`:
// the mode is what makes the file runnable, by the image's own UID and by any
// UID a caller overrides it with, while the owner is a property of the image
// this module was given rather than one it may assume. The Dockerfile can name
// 65532 because it also names the base image it is deriving from; a module
// handed a container through --image knows no such thing. And --image may be
// omitted, in which case the generator image is the one this project publishes
// beside the CLI — `<repository>-gen-<name>:<version>`, resolved against the same
// --repository and --version the CLI image came from, so a generator from one
// release never lands beside a CLI from another.
//
// WithGeneratorExecutable is the other half, and it takes a File rather than an
// image: a generator that has not been published yet, most often one the caller
// has just built in the same pipeline. A generator author needs it to check
// their plugin against a real cpybkc run before there is anything of theirs to
// pull.
//
// # Multi-platform derived builds
//
// A composed image is one platform, because a dagger.Container is. What this
// module guarantees is that it is *one* platform: WithGenerator reads the
// platform off the container it is composing into and pulls the generator image
// for that platform, so an arm64 base never quietly acquires an amd64 generator
// that fails at exec with the kernel's message rather than cpybkc's.
//
// --platform on the constructor is what makes the other platform reachable at
// all. Without it the base image is pulled for the engine's own platform, and an
// amd64 engine could only ever compose an amd64 image. With it, a derived
// multi-platform index is one composition per platform, published as variants:
//
//	variants := make([]*dagger.Container, 0, len(platforms))
//	for _, p := range platforms {
//		variants = append(variants, dag.
//			Cpybkc(dagger.CpybkcOpts{Platform: string(p)}).
//			WithGenerator("hello", dagger.CpybkcWithGeneratorOpts{
//				Image: dag.Container(dagger.ContainerOpts{Platform: p}).
//					From("ghcr.io/example/cpybkc-gen-hello:v1"),
//			}).
//			Image())
//	}
//	ref, err := dag.Container().Publish(ctx, address,
//		dagger.ContainerPublishOpts{PlatformVariants: variants})
//
// The generator image is built for p as well, which is the loop's one obligation
// and the thing WithGenerator refuses rather than lets past. Omitting the image
// and letting the published generator be pulled needs no such care, because the
// pull already follows the container's platform.
//
// The loop is the caller's rather than this module's because the index is: which
// platforms a derived image serves is a property of who will run it, and a module
// that decided it would be publishing this project's platform table as somebody
// else's.
//
// # What this does not do
//
// It does not build cpybkc from source, and it is not this repository's
// pipeline. `dagger call ci`, the image builds and the contract checks are the
// root module's.
//
// It does not build a generator either. WithGenerator copies one out of an image
// and WithGeneratorExecutable takes one already built; how a generator is
// compiled, and that it must come out statically linked for the image's platform,
// are the caller's (docs/container/SPEC.md's `CGO_ENABLED=0`). Nothing here can
// check that, which is why it is said rather than enforced.
//
// # The surface mirrors the CLI, and Run is the fallback
//
// Every capability cpybkc has should be reachable here as a function named for
// the command it belongs to, and every flag as an argument on that function.
// Generate takes a source directory and a manifest, and is the default action —
// what cpybkc does when nothing else is asked of it. Init takes a source
// directory and copybook paths and hands back the layout scaffold, which is
// `cpybkc init`. Those are the two commands there are, so the names are the
// CLI's rather than this module's: somebody who read docs/cli/SPEC.md should
// find what they read about here, under the name they read it under.
//
// A flag that replaces the default action outright gets a function of its own
// under the flag's own name, on the same grounds. EmitIr is `--emit-ir`: it
// takes the project and the manifest Generate takes, plus the encoding, and
// hands back the resolved descriptor rather than a generated tree. That is not a
// third command and it is not this module inventing a verb — it is where the
// mapping puts a flag whose whole meaning is that no generating happens
// (docs/cli/SPEC.md, "Emitting replaces generation"), and it is the same move
// #228 made for `init`. It also makes an illegal pairing unstateable rather than
// enforced: --emit-ir-format is a usage error without --emit-ir, and as this
// function's argument there is no call that names one without the other.
//
// That is a stance rather than an observation, and it replaced a good argument
// (#62, #253). This module used to curate deliberately — two functions, an
// escape hatch, and the reasoning that a module argument is public API for as
// long as the published ref exists, so a surface mapped one-to-one onto
// docs/cli/SPEC.md's flag table makes every change to that table a change to
// this module's. That cost is real, and mirroring pays it rather than avoids it.
// What decided it the other way is what the alternative cost: a caller who can
// do something with `cpybkc` and not with this module could not tell "not
// offered, on purpose" from "nobody has got to it yet", and neither could
// anybody reading this comment — so every gap was argued twice, once by whoever
// found it and once by whoever fixed it.
//
// Run is the fallback, not the plan. It takes the argument vector verbatim and
// hands back the container, so anything this module has not caught up with is
// still reachable today; what it is not is the intended route for a class of
// flag. A flag that reaches a caller through Run is recorded as an exception in
// this repository's pipeline, with the argument for it and, where it is a gap
// rather than a decision, the issue curating it — and `dagger call cli-surface`
// fails on one that is neither. Today that is --version, --help and -h, and all
// three stay: `dagger call --help` and the per-function documentation are the
// Dagger-native form of that question, and which release runs is something a
// caller states at New's --version rather than asks the CLI afterwards. No flag
// is on this function today for want of somebody curating it, which is the
// state that table is kept in order to be able to say.
//
// A function is still allowed to decline one *spelling* of a flag without
// declining the flag, and that stays deliberately. Init supplies --out itself,
// at a path inside the container the caller never types, and hands back the
// File; `--out -` is not offered, because a File-returning function cannot
// express a stream. EmitIr supplies --emit-ir's destination on the same terms
// and declines `--emit-ir -` for the same reason. A stream destination is the
// whole of that class, and both spellings stay Run's, which is the command being
// reachable rather than out of reach:
//
//	dagger call run --source . --args=init,--copybook,posting.cpy,--out,- stdout
//	dagger call run --source . --args=--emit-ir,- stdout
//
// One thing the flag table cannot say, and this comment therefore should. cpybkc
// passes its whole environment through to a generator, which is how
// docs/plugin/SPEC.md propagates SOURCE_DATE_EPOCH — and this module offers no
// way to state an environment, so that promise does not reach a Dagger caller
// (#252). It is a capability with no flag behind it, so cli-surface cannot see
// it and did not; it is named here because the flag table is a lower bound on
// what mirroring the CLI means, and a reader deserves to know the one place it
// is currently short.
package main

import (
	"context"
	"fmt"
	"strings"

	"dagger/cpybkc/internal/argv"
	"dagger/cpybkc/internal/dagger"
	"dagger/cpybkc/internal/generator"
	"dagger/cpybkc/internal/imageref"
)

// executableMode is the mode an installed generator lands with, and it is the
// derived Dockerfile's `--chmod=0755`.
//
// Readable and executable by the image's own UID and by any UID a caller
// overrides it with, which is what keeps running the composed image as the host
// user an ordinary configuration rather than a workaround. The base-image
// contract requires an executable copied into the plugin directory to be
// executable by the image's user; this is that requirement met, and it is met
// with permissions rather than with an owner for the reason
// WithGeneratorExecutable gives.
//
// It stays here rather than moving into internal/generator with the path,
// because a mode is not a name: that package answers what a generator is called
// and where it goes, which is what the two specs fix, and how permissively it is
// installed is this module's decision about a container.
const executableMode = 0o755

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

// scaffoldDir is where Init has cpybkc write the scaffold, and scaffoldPath is
// the file inside it.
//
// It is deliberately **outside** [projectDir]. `cpybkc init` replaces nothing:
// a destination that is occupied fails the run, whatever is in it
// (docs/cli/SPEC.md, "Nothing at <dest> is ever replaced"), and that rule is the
// one unrecoverable act this command could otherwise perform — the `discriminate`
// forms and the `sequence` in an edited layout are the part no copybook holds.
// Writing into a directory of this module's own, which nothing was mounted into,
// is what makes the rule hold here without the module reasoning about the
// caller's tree at all: there is nothing at the path, ever, whatever is in
// theirs.
//
// The caller never types either constant. The file comes back as a
// *dagger.File, and where it lands is what they name at export — which keeps
// *where the file goes is the one thing the adopter knows and cpybkc does not*
// true of this module as well as of the CLI. The name below is only what the
// file is called on the way out; the manifest's `layout` field names the
// adopter's copy, and this module has no business choosing that.
const (
	scaffoldDir  = "/scaffold"
	scaffoldPath = scaffoldDir + "/layout.sexpr"
)

// descriptorDir is where EmitIr has cpybkc write the run's resolved descriptor,
// and descriptorPath is the file inside it.
//
// Outside [projectDir] on [scaffoldDir]'s grounds, and the argument is the same
// one twice: a destination inside the mounted project is a path this module
// would have to reason about the caller's tree to choose safely, and an emitting
// run over a project that already holds a file at the chosen path is not a run
// anybody asked about. Nothing in the caller's tree is written either way — an
// emission is terminal, so nothing is merged or pruned (docs/cli/SPEC.md,
// "Emitting replaces generation") — and writing into a directory nothing was
// mounted into is what keeps that true of the emission as well.
//
// The name carries no extension, deliberately. Which encoding is in the file is
// the format argument's to say and both encodings land at this one path, so a
// name claiming one of them would be wrong for the other run. It is only what
// the file is called on the way out in any case: what it is called in the
// caller's tree is what they name at export, which is the same arrangement the
// scaffold has.
const (
	descriptorDir  = "/ir"
	descriptorPath = descriptorDir + "/descriptor"
)

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
	// Container is the image cpybkc runs in, with whatever generators have been
	// composed into it so far.
	// +private
	Container *dagger.Container

	// Repository and Version are the coordinates WithGenerator resolves *other*
	// images against — the companion generator images, published as
	// `<repository>-gen-<name>:<version>`. They are kept rather than consumed at
	// construction because an override that reached only the first pull would
	// leave every later one reaching for ghcr.io from inside a network that
	// cannot see it, which is the air-gap requirement failing later and less
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
	// guessing one would produce exactly the untested pairing WithGenerator's
	// default exists to avoid — a generator from one release beside a CLI from
	// another.
	//
	// Be exact about what that leaves, because it is less than it sounds. A caller
	// who passed --image and then lets WithGenerator pull a published generator
	// gets whatever --version holds, and --version has a default: pin the CLI by
	// digest and say nothing else, and the generator that arrives beside it is the
	// moving "v0" tag. That combination is not refused, because refusing it would
	// mean distinguishing an omitted --version from an explicit "v0", which a
	// Dagger default cannot express. It is said instead — here, and in --image's
	// own documentation, where the caller who pins a digest will meet it.
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
// thereby inert: they stay on the module as the coordinates WithGenerator
// resolves companion images against, which is why an air-gapped caller passing a
// container from their own registry should pass --repository beside it rather
// than instead of it.
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
	//
	// It pins this image and nothing else. A generator pulled afterwards by
	// with-generator follows --version, which defaults to the moving "v0" tag and
	// does not track whatever was pinned here, so a build that pins the CLI by
	// digest and wants the pairing to hold still passes --version beside it.
	// +optional
	image *dagger.Container,
	// Pull the image for this platform, as `GOOS/GOARCH`, instead of for the
	// engine's own.
	//
	// It is what makes a derived image for another architecture reachable at all:
	// a composition is one platform, and without this an amd64 engine could only
	// ever compose an amd64 image. Every generator composed in afterwards follows
	// it, so the platform is stated once, here, rather than on each call that
	// could contradict the last.
	//
	// A multi-platform derived image is therefore one composition per platform,
	// published as variants of one index — see this module's comment. That loop
	// belongs to the caller because the index does: which platforms a derived
	// image serves is a property of who will run it.
	//
	// Empty is the engine's own platform, which is what makes an ordinary
	// `dagger call` from a checkout do the obvious thing.
	// +optional
	platform string,
) (*Cpybkc, error) {
	// Checked even when image is supplied and nothing is pulled from them, because
	// they are not decoration in that case either: WithGenerator resolves companion
	// images against them, and a repository that is wrong is as wrong then as now.
	// A constructor that accepted them quietly would move the complaint to a call
	// the caller has not written yet.
	ref, err := imageref.Assemble(repository, version)
	if err != nil {
		return nil, err
	}

	// Refused rather than reconciled, because there is nothing to reconcile: a
	// container arrives already built for a platform, and this argument only ever
	// described a pull that is no longer happening. Silently ignoring it would be
	// the worse half of the choice — the caller would believe they had asked for
	// an architecture, and find out at exec time that they had not.
	//
	// It is a pure check rather than a comparison against the container's actual
	// platform on purpose: reading that would mean a network round trip in a
	// constructor, which is the same reason imageref validates the shape of a
	// reference and not its existence.
	if image != nil && platform != "" {
		return nil, fmt.Errorf(
			"--platform=%s was given beside --image: --platform says which platform to pull the cpybkc image for, "+
				"and --image replaces that pull entirely, so the two together state a platform for something that "+
				"is not being pulled — build the container for the platform you want and pass it to --image alone",
			platform)
	}

	if image == nil {
		// A zero Platform is omitted from the query, so this is exactly
		// dag.Container().From(ref) when the caller named no platform.
		image = dag.Container(dagger.ContainerOpts{Platform: dagger.Platform(platform)}).From(ref)
	}

	return &Cpybkc{Container: image, Repository: repository, Version: version}, nil
}

// WithGenerator adds one generator to the image by copying its executable out of
// a generator image:
//
//	dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
//	  with-generator --name hello --image ghcr.io/example/cpybkc-gen-hello:v1 \
//	  generate --source . export --path .
//
// This is the whole of adding a generator without writing a Dockerfile. The file
// is taken out of the image at the path the base-image contract promises, which
// is what `COPY --from` does, and it works just as well for a generator image
// this project has never heard of — that is what image is for. Repeated calls
// compose, so a project whose manifest names three generators is three calls and
// one image.
//
// With no image, the generator this project publishes for name is pulled:
// `<repository>-gen-<name>:<version>`, against the same coordinates the CLI image
// came from. A generator from one release beside a CLI from another is a pairing
// nobody tested, and defaulting to it is how somebody would end up in one without
// having said so.
//
// The generator is pulled for the platform the container being composed into was
// resolved for, not for the engine's. That is the whole of what this module does
// about multi-platform: composing an amd64 generator into an arm64 image produces
// an image that builds and pushes and then fails at exec with the kernel's
// message rather than cpybkc's, which is a long way from the call that caused it.
func (m *Cpybkc) WithGenerator(
	ctx context.Context,
	// The generator to add, by the `<name>` cpybkc.json asks for it by. Discovery
	// is by filename, so this is the name in cpybkc-gen-<name> and nothing else.
	name string,
	// Take the executable from this image instead of pulling the published
	// generator image for name.
	//
	// Any image carrying the generator in the plugin directory will do, including
	// one that was never published. It has to be for the same platform as the
	// image being composed into, which is checked, because a mismatch here is
	// checkable and its exec-time failure is not legible.
	// +optional
	image *dagger.Container,
) (*Cpybkc, error) {
	if err := generator.CheckName(name); err != nil {
		return nil, err
	}

	platform, err := m.Container.Platform(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the platform the cpybkc image was resolved for, which the %s generator "+
			"has to match: %w", name, err)
	}

	if image == nil {
		ref, err := imageref.Assemble(generator.Repository(m.Repository, name), m.Version)
		if err != nil {
			return nil, fmt.Errorf("resolving the published image for the %s generator: %w", name, err)
		}

		image = dag.Container(dagger.ContainerOpts{Platform: platform}).From(ref)
	} else {
		supplied, err := image.Platform(ctx)
		if err != nil {
			return nil, fmt.Errorf("reading the platform of the image the %s generator is taken from: %w", name, err)
		}

		// Refused rather than copied anyway. An executable for the wrong
		// architecture is exactly what the base-image contract's "statically linked
		// native executable for the image's platform" rules out, and unlike a bare
		// File a container states its platform, so this is one of the few places
		// that requirement can be checked at all rather than left to fail later.
		//
		// The escape hatch is named because a platform string is a comparison this
		// module could be wrong about — a variant spelling that runs perfectly well
		// is still not the same string — and a refusal with no way past it would be
		// this module deciding something the contract left to the caller.
		if supplied != platform {
			return nil, fmt.Errorf(
				"the image given for the %s generator is %s and the cpybkc image being composed into is %s: an "+
					"executable for another platform fails at exec time with the kernel's message rather than "+
					"cpybkc's, so it is refused here — compose from an image built for %s, or, if the two really "+
					"are compatible, take the file out of it yourself and pass it to with-generator-executable, "+
					"which states the match as your obligation (from a Dagger module that is one expression; from "+
					"the command line it is `file --path %s export` and a second call naming the exported path)",
				name, supplied, platform, platform, generator.Path(name))
		}
	}

	return m.WithGeneratorExecutable(name, image.File(generator.Path(name)))
}

// WithGeneratorExecutable adds one generator to the image from an executable
// file, for a generator that ships no image — most often one the caller has just
// built in the same pipeline:
//
//	dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
//	  with-generator-executable --name hello --executable ./cpybkc-gen-hello \
//	  generate --source . export --path .
//
// It is the generator author's path, before there is anything of theirs to pull.
// Checking a plugin against a real cpybkc run is the first thing they need and
// the last thing a published image can give them.
//
// The file lands as cpybkc-gen-<name> whatever it was called before, because
// discovery is by filename and nothing inside the executable is consulted.
//
// It has to be a statically linked native executable for the image's platform,
// and that is the caller's to meet rather than this module's to check: it is the
// same requirement docs/container/SPEC.md's worked example states as
// `CGO_ENABLED=0`, a File says nothing about what it is, and a dynamically linked
// or foreign-architecture generator fails at exec time with the kernel's message
// rather than cpybkc's. WithGenerator can check the platform half of that because
// a container states one; here there is nothing to read.
func (m *Cpybkc) WithGeneratorExecutable(
	// The generator's `<name>`, as cpybkc.json asks for it.
	name string,
	// The generator executable.
	executable *dagger.File,
) (*Cpybkc, error) {
	if err := generator.CheckName(name); err != nil {
		return nil, err
	}

	// Permissions and no owner, where the derived Dockerfile writes
	// `--chown=65532:65532` beside its `--chmod=0755`. The mode is what makes the
	// file runnable — by the image's own UID and by any UID a caller overrides it
	// with — while the owner is a property of the image this module was given
	// rather than one it is entitled to assume: a caller who passed --image may be
	// composing onto a base with a user of their own, and this module would be
	// overwriting their answer with the contract's. The Dockerfile is entitled to
	// the number because its FROM line names the image it is deriving from.
	next := *m
	next.Container = m.Container.WithFile(
		generator.Path(name),
		executable,
		dagger.ContainerWithFileOpts{Permissions: executableMode},
	)

	return &next, nil
}

// Image is the image this module resolved and composed, for a caller who wants
// to do something with it other than what this module offers — run a cpybkc
// subcommand by hand, look at what is in it, publish a derived image holding
// their generators, or make it one variant of a multi-platform index:
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

// Init scaffolds a layout from copybooks and hands back the file to edit:
//
//	dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
//	  init --source . --copybook posting.cpy export --path ledger.sexpr
//
// It is the first thing an adopter runs, before there is a manifest, a layout or
// a generator to speak of, and it is the run that made the case for mapping
// cpybkc's commands by name at all (#228): the one invocation somebody has
// nothing to copy from is a poor place to make them assemble an argument vector
// by hand.
//
// What comes back is **not a valid layout** and is not meant to be. `init`
// writes what the copybooks decide — a record per 01-level, an alternative per
// REDEFINES — and leaves the half no copybook holds as commented forms for a
// person to answer: which field discriminates, and in what order the records
// come. docs/cli/SPEC.md's `init` section is what that file says and what it
// deliberately does not.
//
// Nothing is written to the host, exactly as with Generate: the File that comes
// back is a value, and exporting it is the caller's separate step. What it is
// called is theirs — this module writes the scaffold to a path of its own inside
// the container, outside the mounted project and where nothing was mounted, so
// the run cannot land on something the caller already has, and the name it takes
// in their tree is the one they give `export`.
//
// *Where* it goes is theirs within one constraint, and it is the one thing here
// a caller cannot infer from the signature. The scaffold names each copybook by
// the path it was given, which is relative to the root of source, and a layout's
// own paths are relative to the layout — so the file belongs at that root, beside
// the copybooks it names. Exported into a subdirectory it is a layout whose
// copybook paths resolve nowhere, and nothing at export time will say so. A
// project that keeps its layout somewhere else moves the paths as it moves the
// file; they are the adopter's to edit, like the rest of what `init` leaves
// blank.
//
// The destination this module supplies is a file rather than a stream, so
// `--out -` is not offered here. A caller who wants the scaffold on standard
// output still has Run, which is the same arrangement --emit-ir has:
//
//	dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
//	  run --source . --args=init,--copybook,posting.cpy,--out,- stdout
func (m *Cpybkc) Init(
	ctx context.Context,
	// The project the copybooks are in: the directory their paths are relative
	// to, mounted at /src and made the working directory.
	//
	// It is required, unlike Run's, because a scaffolding run reads files by
	// definition. The whole directory is mounted rather than the named copybooks
	// alone, because that is what makes the paths below resolve as the adopter
	// typed them: --source is already how every other function on this module
	// takes the caller's tree, and a mount holding only the named files would be
	// a second, weaker answer to where a relative path points.
	source *dagger.Directory,
	// A copybook to read, as a path relative to the root of source. Repeat it
	// once per copybook, in the order the scaffold should hold their records in.
	//
	// Paths rather than files, because the path is data: docs/cli/SPEC.md has
	// the scaffold record each copybook's path *as it was typed*, and a layout's
	// own paths are relative to the layout. A file handed over on its own would
	// have cpybkc write a container path the adopter cannot find anywhere in
	// their tree.
	//
	// At least one is required, and none may be "-": a copybook on a stream has
	// no path for the scaffold to state. Everything else is the CLI's to refuse —
	// a value naming a directory is a run that failed rather than a line that
	// was wrong, and it says so with a diagnostic naming the path.
	//
	// One boundary belongs to the command line rather than to cpybkc: `dagger
	// call` renders a list argument as `--copybook a.cpy --copybook b.cpy` or as
	// one comma-separated `--copybook a.cpy,b.cpy`, so a copybook whose path
	// contains a comma cannot be spelled here. That one is Run's, where the
	// vector is passed through as written.
	copybook []string,
) (*dagger.File, error) {
	args, err := argv.Init(copybook, scaffoldPath)
	if err != nil {
		return nil, err
	}

	// Resolved once and used for both the project mount and the scaffold
	// directory below, which is the whole reason [Cpybkc.user] is a function:
	// two answers to who this image is would be two answers.
	user, err := m.user(ctx)
	if err != nil {
		return nil, err
	}

	scaffold, err := m.writable(m.mount(user, source), user, scaffoldDir)
	if err != nil {
		return nil, err
	}

	return scaffold.
		WithExec(args, dagger.ContainerWithExecOpts{UseEntrypoint: true}).
		File(scaffoldPath), nil
}

// EmitIr writes the run's resolved descriptor and hands back the file:
//
//	dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
//	  emit-ir --source . export --path descriptor.binpb
//
// It is `cpybkc --emit-ir`, and the name is that flag rather than a word of this
// module's own for the reason Init's is the CLI's verb: somebody who read
// docs/cli/SPEC.md should find what they read about here, under the name they
// read it under.
//
// The descriptor is what to attach to a bug report, against cpybkc or against
// any generator, and the call above is the one that produces it: in the default
// encoding it is exactly the bytes a plugin was handed — the equality the plugin
// contract rests reproducibility on — so reading it settles in one step whether
// a fault is the producer's or the consumer's. The JSON is a rendering of that
// same descriptor rather than the bytes themselves, so it belongs beside the
// binary file in an issue rather than instead of it. It is also available from
// the run that is broken: an emission is terminal, so no generator is resolved,
// nothing is merged into the project's tree and nothing is pruned from it
// (docs/cli/SPEC.md, "Emitting replaces generation"), which is why a project
// whose `generate` fails inside a generator can still emit the descriptor that
// generator was given.
//
// Nothing is written to the host, exactly as with Init: the File that comes back
// is a value, and exporting it is the caller's separate step. This module writes
// it to a path of its own inside the container, outside the mounted project and
// where nothing was mounted, so the run cannot land on something the caller
// already has.
//
// The one spelling this does not offer is `--emit-ir -`, which stays Run's for
// the reason `--out -` does — a File-returning function has no stream to hand
// back — and which is a spelling rather than a flag:
//
//	dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
//	  run --source . --args=--emit-ir,- stdout
//
// The illegal pairing docs/cli/SPEC.md names — `--emit-ir-format` without
// `--emit-ir` — is unstateable here rather than enforced. This function *is* the
// emission and the format is its argument, so there is no call that names a
// format without asking for one, and there is nothing left for the module to
// check.
func (m *Cpybkc) EmitIr(
	ctx context.Context,
	// The project to emit the descriptor of: the directory holding the manifest,
	// the layout it names and the copybooks that layout names.
	//
	// It is mounted whole rather than filtered down to what the run reads, for
	// Generate's reason: which copybooks are read is a property of the layout, and
	// a module guessing at that set would be a second, weaker answer to a question
	// docs/cli/SPEC.md answers exactly.
	source *dagger.Directory,
	// The project manifest to read, relative to the root of source.
	//
	// It is taken the way Generate takes it, and it is not a convenience: a run
	// resolves one descriptor, from the layout the manifest names and the
	// copybooks that layout names (docs/cli/SPEC.md, "Which descriptor is
	// emitted"), so *which* descriptor is emitted is exactly which manifest was
	// read. A project keeping its manifest somewhere else would otherwise be able
	// to generate through this module and not to emit what it generated from.
	//
	// It defaults to nothing, which leaves cpybkc reading `cpybkc.json` at the
	// root of the mounted project — the CLI's own default, applied by the CLI. A
	// relative path resolves against that project root, and it cannot be "-",
	// because a manifest's own paths are relative to the directory holding it and
	// a manifest arriving on a stream is in no directory.
	// +optional
	manifest string,
	// The encoding the descriptor is written in: `binary`, the canonical protobuf
	// wire encoding a generator is handed, or `json`, the normalized rendering a
	// person reads and pastes into an issue.
	//
	// It defaults to nothing, which is not a third encoding: an unnamed format
	// reaches the CLI as no --emit-ir-format at all, so what arrives is whatever
	// docs/cli/SPEC.md's default is — `binary` today — decided there rather than
	// restated here where the two could drift.
	//
	// A value that is neither spelling is passed through and refused by the CLI,
	// which names the spellings there are from the parser that decides them. That
	// is deliberate for the reason Run validates nothing: a second reading of that
	// contract out here is one that drifts, and this one would drift the day a
	// third encoding landed.
	// +optional
	format string,
) (*dagger.File, error) {
	args, err := argv.EmitIR(manifest, descriptorPath, format)
	if err != nil {
		return nil, err
	}

	user, err := m.user(ctx)
	if err != nil {
		return nil, err
	}

	emitting, err := m.writable(m.mount(user, source), user, descriptorDir)
	if err != nil {
		return nil, err
	}

	return emitting.
		WithExec(args, dagger.ContainerWithExecOpts{UseEntrypoint: true}).
		File(descriptorPath), nil
}

// writable hands back container with dir emptied and owned by user, which is
// what a curated function supplies the CLI when it chooses the destination
// itself.
//
// A dir inside [projectDir] is refused, and that is the whole reason this takes
// a path rather than closing over one. Emptying is destructive, and as an
// inlined block in one function it was safe by construction — the constant
// beside it was outside the mount, and could be read in the same screenful. As a
// helper taking a path it would empty whatever it was handed, so a later
// destination written one directory further up would delete part of the caller's
// project before the run, which is the one thing every comment around here says
// cannot happen. It is checked rather than asserted for that reason: *the
// destination is outside the mounted project* is a promise to a caller, and a
// promise held by nobody editing a constant is held by nothing.
//
// Emptied because WithDirectory *merges*: whatever the image already had there
// would survive it, and a caller may have passed --image, which this module is
// not entitled to have an opinion about the contents of. Without the removal,
// such a container carrying anything at the destination would fail the run on a
// path its caller never named and cannot see in the call — which for `init` is
// the one failure this path is arranged to make impossible, since a destination
// that is occupied fails a scaffolding run whatever is in it.
//
// Owned, because both files are written through a temporary file created beside
// the destination and linked into place — the write is atomic or it does not
// happen — so the process needs a directory it can create in, not merely a path
// nothing occupies.
//
// It is one function rather than two copies for the reason [Cpybkc.user] is:
// Init and EmitIr are making the same arrangement for the same reasons, and two
// statements of it would be two things to keep in step.
func (m *Cpybkc) writable(container *dagger.Container, user, dir string) (*dagger.Container, error) {
	if dir == projectDir || strings.HasPrefix(dir, projectDir+"/") {
		return nil, fmt.Errorf(
			"a destination this module supplies has to be outside the mounted project, and %q is inside %q: "+
				"emptying it would delete part of the caller's tree before the run", dir, projectDir)
	}

	return container.
		WithoutDirectory(dir).
		WithDirectory(dir, dag.Directory(), dagger.ContainerWithDirectoryOpts{Owner: user}), nil
}

// Run is the fallback: cpybkc invoked with an argument vector this module has no
// opinion about, in a container handed back whole.
//
//	dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
//	  run --source . --args=--emit-ir,- stdout
//
//	dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
//	  run --args=--help stdout
//
// It is not the intended route for anything. This module mirrors the CLI, so a
// capability's ordinary answer is an argument on the function named for the
// command it belongs to (#253), and three kinds of thing are left over for this
// function. A flag this module has not caught up with is reachable through it in
// the meantime, which is what keeps a gap an inconvenience rather than a wall. A
// spelling no Dagger type can express is reachable through it permanently: a
// destination that is a stream rather than a file — `--out -`, `--emit-ir -` —
// which a File-returning function cannot hand back. And a flag whose question
// has a Dagger-native answer that is not a function here stays on purpose, which
// is --version, --help and -h and is meant to be the whole of that class.
//
// Which flags arrive here today is stated in this module's package comment and
// recorded where a check can fail on it, in the root pipeline's
// companionRunExceptions: `dagger call cli-surface` fails when the CLI grows a
// flag that neither a named function nor an argued exception covers, so a flag
// landing on this function quietly is the one thing that cannot happen.
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
// The mount is owned by [Cpybkc.user], which is what keeps a run from failing on
// the first file a generator writes.
func (m *Cpybkc) project(ctx context.Context, source *dagger.Directory) (*dagger.Container, error) {
	user, err := m.user(ctx)
	if err != nil {
		return nil, err
	}

	return m.mount(user, source), nil
}

// mount is [Cpybkc.project] with the user already in hand, for a caller that
// needs it for something else as well and should not ask twice.
func (m *Cpybkc) mount(user string, source *dagger.Directory) *dagger.Container {
	return m.Container.
		WithDirectory(projectDir, source, dagger.ContainerWithDirectoryOpts{Owner: user}).
		WithWorkdir(projectDir)
}

// user is who the container runs as, and it is the owner every directory this
// module mounts or creates is given.
//
// It is asked of the container rather than assumed, so that a caller who passed
// --image built on a derived image with its own user still gets directories
// their process can write into. A container that reports no user at all falls
// back to [contractUser], which is what the base-image contract pins and what an
// image satisfying it runs as; the alternative, leaving a mount root-owned, is a
// run that fails on the first file it writes.
//
// It is a function of its own because two things need it now: the project mount,
// and the directory Init has the scaffold written into. A second copy of the
// fallback would be a second answer to who this image is.
func (m *Cpybkc) user(ctx context.Context) (string, error) {
	user, err := m.Container.User(ctx)
	if err != nil {
		return "", fmt.Errorf("reading the user the cpybkc image runs as, which the directories this module "+
			"mounts have to be writable by: %w", err)
	}

	if user == "" {
		return contractUser, nil
	}

	return user, nil
}
