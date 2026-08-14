// This file holds the published base image to docs/container/SPEC.md — the
// image strangers' Dockerfiles name paths inside — and it no longer builds one
// (#55, #185).
//
// # Why the image is no longer built here
//
// It was, and the argument was recorded here at length: the Z5Labs standard
// pipeline's GoApp archetype produced one image per binary — a scratch image
// holding one executable at a path of its own choosing, with no PATH, no user,
// no second directory and no notion of an image built FROM it — while cpybkc
// publishes a *base*, and four of the six promises docs/container/SPEC.md makes
// are that shape rather than settings GoApp was missing.
//
// That premise is gone (#185). The refactored archetype's plugin directory *is*
// /usr/local/bin, with PATH composed from the same constant so the two cannot
// drift; its runtime user *is* 65532:65532, fixed at build time with no seam to
// move it; and content arrives through App.WithFile and App.WithDirectory, each
// carrying a document about what it is. So the image is Go.App plus this
// repository's two contributions, and the handful of Container calls that used
// to assemble it are deleted rather than wrapped.
//
// # What that changed for a consumer, and what it did not
//
// Three promises moved and are recorded in docs/container/SPEC.md rather than
// here: the plugin directory no longer exists in the base image (nothing is in
// it, and the archetype refuses a contribution to a directory the PATH resolves
// against), the CLI's own path is /app/cpybkc rather than /usr/local/bin/cpybkc,
// and the IR schema is 65532-owned and read-only rather than root-owned 0644.
// The first was a covered guarantee and the document changed; the second was
// never one — *The CLI's own path is not part of the contract* was written for
// exactly this day — and the third is a hardening whose covered halves
// (world-readable file, world-traversable tree, not modifiable in place) all
// survive.
//
// # Why the contract is a check rather than a comment
//
// Every promise docs/container/SPEC.md makes is a field of an OCI image
// configuration or a file in the filesystem it describes, so every one of them
// is machine-checkable — and each is the kind of promise that breaks silently
// here and loudly somewhere else. An image whose PATH lost /usr/local/bin runs
// cpybkc perfectly and fails only in a stranger's repository, at the point where
// their generator is not found. ImageContract is that document's compatibility
// guarantees table executed rather than read, and it matters *more* now that the
// bytes are somebody else's: it is the evidence that adopting the archetype
// changed nothing a consumer can see that #185 did not choose to change.
//
// The images built FROM this one are not this repository's to write — that is
// the whole of #48's argument for shipping cpybkc-gen-go as a separate image —
// but one of them is: docs/container/SPEC.md's worked example, which
// worked_example.go assembles onto the image this file hands back.
package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"

	"dagger/cpybkc/internal/dagger"
)

// The published image's contract, as constants. Each one is either a row of
// docs/container/SPEC.md's compatibility guarantees table — changing one here
// without changing it there is the drift ImageContract exists to catch — or a
// path the archetype fixes that the exhaustive listing has to be able to name.
const (
	// pluginDir is the plugin directory: the one path in the image a derived
	// Dockerfile writes to, and the one directory on PATH that anything is
	// expected to be found in.
	//
	// The archetype fixes the same value, composed into PATH from the same
	// constant on its side. It is read here rather than imported because the
	// archetype exposes no constant for it, which is the point of a contract
	// check: the day the plugin directory moves, this listing is what says so.
	//
	// The base image does not contain it. Nothing is in it — cpybkc's own
	// generator is shipped as a separate image (#48) — and the archetype refuses
	// a contribution to any directory the PATH resolves against, so there is
	// nothing to create it. A derived image's COPY creates it, which is what the
	// worked example checks.
	pluginDir = "/usr/local/bin"

	// appDir is where the archetype puts an application's own binary, and
	// homeDir is what it points HOME at. Neither is cpybkc's to choose and
	// neither is in docs/container/SPEC.md: they are here because the filesystem
	// listing is exhaustive, so a path the image carries has to be named
	// somewhere even when nothing may depend on it.
	appDir  = "/app"
	homeDir = "/home/nonroot"

	// homeMode is the mode HOME's directory has: traversable and readable,
	// writable by nobody, and root-owned so the refusal survives a caller
	// overriding the runtime user. cpybkc writes nothing under HOME — its
	// scratch space and each invocation's descriptor go in the project it was
	// pointed at (#184) — so an empty read-only directory is the whole of what
	// this row means.
	homeMode = 0o555

	// tmpDir is what the archetype points TMPDIR at, and the image deliberately
	// does not contain it: the deployment mounts a tmpfs or an emptyDir there if
	// it wants one. cpybkc needs none, which is what #184 settled, and the
	// exhaustive listing below is what holds the image to still not carrying
	// one.
	tmpDir = "/tmp"

	// imageUID and imageGID are the pinned non-root identity the image runs as.
	// They are numbers rather than a name because the image has no /etc/passwd
	// for a name to resolve against, and because a derived Dockerfile's
	// COPY --chown and a Kubernetes securityContext are both written against
	// the number.
	imageUID = 65532
	imageGID = 65532

	// imageUser is that identity in the form the OCI configuration's User field,
	// Dagger's owner arguments and a COPY --chown all take.
	imageUser = "65532:65532"

	// overrideUser is an arbitrary other identity, used to check the promise
	// that the image does not require its own: a caller writing output into a
	// bind mount is told to pass `--user $(id -u):$(id -g)`, and that has to be
	// an ordinary configuration rather than a workaround.
	overrideUser = "1234:1234"

	// executableMode is the mode every executable in the image has: readable and
	// executable by the image's user and by any UID a caller overrides it with,
	// which is the other half of what makes that override ordinary.
	//
	// It is 0555 rather than the 0755 this pipeline used to write, because the
	// archetype owns it now: every byte it puts in an image is read-only, on the
	// grounds that an image whose files the application can rewrite is one whose
	// published digest stops describing what is running.
	executableMode = 0o555

	// derivedExecutableMode is what docs/container/SPEC.md's derived Dockerfiles
	// write: `COPY --chown=65532:65532 --chmod=0755`.
	//
	// It is a separate constant from executableMode, and the two came apart in
	// #185 rather than by oversight. The mode of a byte *this* pipeline puts in
	// an image is the archetype's and is read-only; the mode of a byte an adopter
	// copies into their own derived image is theirs, and 0755 is what this
	// project's document has always told them to write. Collapsing the two would
	// make an upstream hardening a breaking change to somebody else's Dockerfile,
	// for no gain to anybody: what the plugin contract actually needs is an
	// execute bit every UID can use, which both modes have.
	derivedExecutableMode = 0o755

	// contributedFileMode and contributedDirMode are what a contributed file and
	// a contributed tree land with. They are the archetype's, for
	// executableMode's reason, and the halves docs/container/SPEC.md depends on
	// are the world-anything ones: a caller who overrides the UID reads the same
	// files and traverses the same directories.
	//
	// 0444 is also the stronger form of "a derived image may copy it out, and
	// MUST NOT modify it in place" — it denies the owner too, where the 0644
	// this pipeline used to write only denied everybody else.
	contributedFileMode = 0o444
	contributedDirMode  = 0o555

	// dirMode is the mode of a directory the archetype creates implicitly, on
	// the way to somewhere else. Nothing is promised about these and nothing may
	// depend on them; they are listed because the listing is exhaustive.
	dirMode = 0o755

	// irDir is where the IR schema ships, and the two paths under it are the
	// ones docs/container/SPEC.md fixes for a consumer (#57).
	//
	// /usr/local/share for the same reason the plugin directory is
	// /usr/local/bin: it is the conventional destination for architecture-
	// independent data belonging to a locally installed program, so a reader who
	// has never opened that document guesses it correctly, and a `COPY --from`
	// naming it reads naturally. The cpybkc component is what keeps a derived
	// image's own share of that directory from colliding with this one's.
	irDir = "/usr/local/share/cpybkc"

	// irDescriptorSetFile is the protobuf FileDescriptorSet describing
	// cpybkc.ir.v1.Descriptor, under the name the release asset already has —
	// they are two ways of getting one artifact rather than two artifacts, and a
	// second name for it would be the first step towards two.
	irDescriptorSetFile = "ir.binpb"

	// irProtoDirName is the schema sources, and it is a directory rather than an
	// archive because it is an include root: every file sits at the path its
	// protobuf package requires, so `protoc -I` is pointed straight at it.
	irProtoDirName = "proto"

	irDescriptorSetPath = irDir + "/" + irDescriptorSetFile
	irProtoDir          = irDir + "/" + irProtoDirName

	// protoSource is the schema root in this repository, which is what both
	// shipped forms are cut from.
	protoSource = "proto"

	// contributionLicense is the SPDX expression the two contributed documents
	// carry. Both are this repository's own schema, published under this
	// repository's licence, and it is stated rather than left out because a
	// document that says nothing about the licence of what it describes is one an
	// adopter's compliance scan has to guess at.
	contributionLicense = "MIT"

	// devVersion is the version every build that is not a release publishes
	// under, and therefore the version every check builds.
	//
	// The archetype requires one: it stamps main.version into the binary, puts
	// the value in an OCI annotation, and refuses a version that could not be an
	// image tag. A release states its own — release.go's planRelease, from the
	// refs at HEAD — and everything else is not a release and says so, rather
	// than borrowing the last one's number. Nothing in docs/container/SPEC.md
	// covers an annotation, so this value is invisible to a consumer; it exists
	// so that "which release is this" has an answer that is never wrong.
	devVersion = "v0.0.0-dev"
)

// imagePlatforms is the set of platforms the image is published for, and the
// single definition of it in this module.
//
// Two, and the same two avroc publishes. cpybkc runs on the machines a modern
// codebase is developed on; it is not deployed to the mainframe whose files it
// reads, which is what #56 settled when it was closed as not planned, and
// docs/container/SPEC.md's platform table says the same. Big-endian *files* stay
// fully in scope — that fork is a property of the data, not of the host — and
// they are decoded by the test suite on the platforms below.
func imagePlatforms() []dagger.Platform {
	return []dagger.Platform{"linux/amd64", "linux/arm64"}
}

// Image hands back the published base image for one platform:
//
//	dagger call image --platform=linux/arm64 export --path=cpybkc.tar
//
// That is also how a contributor gets the image onto their own machine, since
// there is no `dagger call publish` any more: export the tarball and
// `docker load` it. The archetype's publish is signed and attested by
// construction and refuses a run it cannot produce provenance for, so the only
// caller that can reach it is the release workflow holding an OIDC token.
//
// It is the image docs/container/SPEC.md describes, and every promise that
// document makes is made in it: the CLI as the entrypoint with an empty Cmd, the
// plugin directory on PATH, UID and GID 65532 running the process, the IR schema
// under /usr/local/share/cpybkc in both published forms, and nothing else in the
// filesystem at all — no shell, no libc, no writable temporary directory.
//
// No generator is in it, and that absence is a promise rather than an omission:
// cpybkc-gen-go (#48-#53) reaches a user the way a stranger's generator does, as
// an image built FROM this one. A base carrying its own generator would publish
// the same bytes and quietly stop testing the extension mechanism.
//
// platform defaults to the engine's own, which is what makes `dagger call image`
// useful from a checkout; a value must be one of the published platforms, so a
// typo is an error rather than an image nobody publishes.
func (m *Cpybkc) Image(
	ctx context.Context,
	// Build for this platform, as `GOOS/GOARCH` — one of the published
	// platforms. Empty builds for the engine's own platform.
	// +optional
	platform string,
) (*dagger.Container, error) {
	p, err := imagePlatform(ctx, platform)
	if err != nil {
		return nil, err
	}

	return m.baseImage(p), nil
}

// baseApp is the published base image as the archetype's application: the CLI
// compiled from this repository, plus the two things cpybkc contributes to it.
//
// This is the one construction of it. ImageContract, the worked example, the
// companion checks and Release all reach the image through here, so there is no
// arrangement in which the image a check passed is not the image somebody
// publishes, nor one in which a derived image is built on a base nobody checked.
//
// version is a parameter rather than devVersion outright so that Release passes
// the release's own and the check stages pass devVersion, and so that the two
// cannot become different code paths — the container a contract check reads is
// built exactly as the container a release pushes is.
//
// Each contribution carries a document, because the archetype requires one:
// every byte that enters an image is described by an SPDX statement, and the
// per-platform SBOM a release attaches is assembled out of them. They are made
// with Z5labs.FileDocument and Z5labs.DirectoryDocument rather than hand-rolled
// here — those are the helpers devex#409 settled for content whose ecosystem has
// no module able to produce a document of its own, which protobuf schema
// sources and a FileDescriptorSet are. Neither states a version: the IR schema's
// version is a field inside the descriptor rather than a release number of the
// content, and inventing one would put a number in an adopter's SBOM that
// nothing in this repository could ever be checked against.
func (m *Cpybkc) baseApp(version string) *dagger.Z5LabsApp {
	set := m.IrDescriptorSet()
	protos := m.irProtoTree()

	return m.appChain().
		App(version, dagger.Z5LabsGoChainAppOpts{
			Pkg:       cliPackage,
			Platforms: imagePlatforms(),
		}).
		WithFile(irDescriptorSetPath, set, dag.Z5Labs().FileDocument(set, dagger.Z5LabsFileDocumentOpts{
			License: contributionLicense,
			Name:    "cpybkc-ir-descriptor-set",
		})).
		WithDirectory(irProtoDir, protos, dag.Z5Labs().DirectoryDocument(protos, "cpybkc-ir-protos",
			dagger.Z5LabsDirectoryDocumentOpts{License: contributionLicense}))
}

// baseImage is one platform's base image, for the checks and for the stages that
// run cpybkc rather than publish it.
//
// It is an accessor onto the App and not an image build: the container it hands
// back is the one the archetype assembled and the one a publish would push,
// which is the whole reason the checks are allowed to read it.
func (m *Cpybkc) baseImage(platform dagger.Platform) *dagger.Container {
	return m.baseApp(devVersion).Container(platform)
}

// appChain is the Go chain an App is built from — the source with its git
// metadata folded in, and nothing configured about the check stages.
//
// The lint and test configuration deliberately does not travel here. Ci runs the
// checks and App builds; a chain configured for both would make a change to the
// lint version a change to the image's cache key.
func (m *Cpybkc) appChain() *dagger.Z5LabsGoChain {
	return dag.Z5Labs().Go(m.appSource())
}

// irProtoTree is the include root the image carries: every .proto under proto/,
// at the path its own protobuf package requires.
//
// Only .proto files are copied, naming what goes in rather than what stays out,
// which is the rule internal/tools/ir-protos already applies to the archive for
// the same reason: a generated file or an editor's leavings appearing under
// proto/ would otherwise be published as part of the contract.
//
// No chmod. The modes used to be set here because the listing is exhaustive down
// to the mode and a file's mode in a git checkout is the contributor's umask;
// the archetype normalizes a contributed tree to 0555 throughout, so the image
// is a function of the source rather than of whose machine built it for a better
// reason than this pipeline had.
func (m *Cpybkc) irProtoTree() *dagger.Directory {
	return m.Source.Directory(protoSource).
		Filter(dagger.DirectoryFilterOpts{Include: []string{"**/*.proto"}})
}

// ImageContract checks the built image against every promise
// docs/container/SPEC.md makes about it (#55).
//
// It is a check rather than a paragraph because each promise is depended on from
// a repository this project cannot see and breaks without breaking anything
// here: an image whose PATH lost the plugin directory runs cpybkc perfectly and
// fails at the point where somebody else's generator is not found. Since #185 it
// is also the evidence that moving onto the shared archetype changed nothing a
// consumer can see that the change did not choose to change.
//
// Five groups, all on the real image rather than on a description of it:
//
//   - The OCI configuration — Entrypoint, Cmd, User and the three pinned
//     environment variables. WorkingDir is not among them, because that document
//     explicitly does not cover it: a caller passes their own, and the
//     invocation the document gives them says so.
//   - The filesystem, as an exact list of every path in it with its kind, its
//     owner and its mode. Exact rather than a spot check, because "the base is
//     scratch plus the files this document names" is the promise, and it is the
//     same assertion as no shell, no libc and no package manager.
//   - How the executable in it was built, read out of the binary itself: CGO
//     off, -trimpath on, and the GOOS and GOARCH of the platform whose image it
//     landed in.
//   - The IR schema it ships, byte for byte against the artifacts a release
//     publishes.
//   - The entrypoint being the CLI, by running it — twice, as the image's own
//     user and as an arbitrary other one.
//
// platform restricts the check to one of the published platforms; empty runs
// every one of them, and every failure is reported rather than the first,
// because "it holds on amd64 and not on arm64" is the finding.
//
// +check
// +cache="session"
func (m *Cpybkc) ImageContract(
	ctx context.Context,
	// Run the check on this platform alone, as `GOOS/GOARCH` — one of the
	// published platforms. Empty covers all of them.
	// +optional
	platform string,
) error {
	platforms := imagePlatforms()
	if platform != "" {
		if !slices.Contains(platforms, dagger.Platform(platform)) {
			return fmt.Errorf("platform %q is not one this repository publishes: %v", platform, platforms)
		}

		platforms = []dagger.Platform{dagger.Platform(platform)}
	}

	var errs []error
	for _, p := range platforms {
		if err := errors.Join(m.checkBaseImage(ctx, m.baseImage(p), p)...); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
		}
	}

	return errors.Join(errs...)
}

// checkBaseImage holds one platform's base image to docs/container/SPEC.md.
//
// It is a function rather than a sequence inside ImageContract because Release
// runs it too, over the very containers it is about to push — see release.go for
// why that is not the same as having run `dagger call image-contract` a moment
// earlier. Two callers, one list: a check added here is a check the release gate
// acquires, where a hand-copied sequence would let one be added and the gate
// stop short of it.
//
// Every group is run and every failure collected rather than stopping at the
// first: a change that broke the entrypoint most likely broke the user and the
// plugin directory too, and one run should say so.
func (m *Cpybkc) checkBaseImage(
	ctx context.Context,
	image *dagger.Container,
	platform dagger.Platform,
) []error {
	protos, err := m.shippedProtos(ctx)
	if err != nil {
		return []error{err}
	}

	errs := m.checkImageConfig(ctx, image)
	errs = append(errs, m.checkImageContents(ctx, image, baseImageContents(protos))...)
	errs = append(errs, m.checkImageBuild(ctx, image, platform)...)
	errs = append(errs, m.checkShippedIr(ctx, image)...)
	errs = append(errs, m.checkImageIsTheCLI(ctx, image)...)

	return errs
}

// shippedProtos is every .proto the image carries, as a path relative to the
// include root, sorted.
//
// Read out of the source tree rather than listed here, so that a second .proto
// arrives in the image and in the contract at once. The alternative — a constant
// naming ir.proto — would make the exhaustive listing fail on the commit that
// added a schema file rather than on the one that shipped it wrongly, which is a
// check that punishes the wrong change.
func (m *Cpybkc) shippedProtos(ctx context.Context) ([]string, error) {
	names, err := m.Source.Directory(protoSource).Glob(ctx, "**/*.proto")
	if err != nil {
		return nil, fmt.Errorf("listing the schema sources under %s/: %w", protoSource, err)
	}

	if len(names) == 0 {
		return nil, fmt.Errorf("no .proto files under %s/: the image would ship an empty include root", protoSource)
	}

	slices.Sort(names)

	return names, nil
}

// checkImageConfig checks the fields of the OCI image configuration a derived
// image inherits.
func (m *Cpybkc) checkImageConfig(ctx context.Context, image *dagger.Container) []error {
	var errs []error

	// The structural half of the entrypoint guarantee. Exactly one element,
	// because "the arguments a caller passes to docker run are cpybkc's
	// arguments" is only true when there is nothing in Entrypoint for them to
	// arrive after; and that element has to be an executable the image actually
	// ships, because Entrypoint is otherwise free to name a path that is not
	// there and fail at run time in somebody else's pipeline.
	//
	// It is deliberately not compared to a literal, since the CLI's own path is
	// implementation detail and pinning it here would make a promise this
	// project has explicitly not made — which is exactly what let #185 move it
	// from the plugin directory to /app without breaking anybody. What pins the
	// entrypoint to the CLI is behaviour rather than shape, in
	// checkImageIsTheCLI.
	//
	// The base image's executables rather than the checked image's, because this
	// runs on derived images too and a derived image inherits its Entrypoint: an
	// image whose Entrypoint had become the generator it copied in would be a
	// different program wearing cpybkc's filesystem, which is exactly the edit
	// docs/container/SPEC.md forbids.
	executables := baseImageExecutablePaths()
	entrypoint, err := image.Entrypoint(ctx)
	switch {
	case err != nil:
		errs = append(errs, fmt.Errorf("reading Entrypoint: %w", err))
	case len(entrypoint) == 0:
		errs = append(errs, errors.New("the image's Entrypoint is empty: a derived image inherits no program"))
	case len(entrypoint) > 1:
		errs = append(errs, fmt.Errorf("the image's Entrypoint is %v, want exactly one element: a caller's "+
			"arguments are cpybkc's arguments, and anything else here would come before them", entrypoint))
	case !slices.Contains(executables, entrypoint[0]):
		errs = append(errs, fmt.Errorf("the image's Entrypoint is %v, which is not one of the executables the "+
			"image ships (%v)", entrypoint, executables))
	}

	args, err := image.DefaultArgs(ctx)
	switch {
	case err != nil:
		errs = append(errs, fmt.Errorf("reading Cmd: %w", err))
	case len(args) != 0:
		errs = append(errs, fmt.Errorf("the image's Cmd is %v, want empty: a caller's arguments are cpybkc's "+
			"arguments", args))
	}

	user, err := image.User(ctx)
	switch {
	case err != nil:
		errs = append(errs, fmt.Errorf("reading User: %w", err))
	case user != imageUser:
		errs = append(errs, fmt.Errorf("the image's User is %q, want %q", user, imageUser))
	}

	// All three of the pinned environment variables, not PATH alone. The
	// archetype fixes HOME and TMPDIR beside it, on the grounds that what a
	// process reads out of them is otherwise the *runtime's* choice rather than
	// the image's — so a variable silently dropped would make one digest behave
	// differently under two container runtimes. Only PATH's guarantee is a
	// covered one, and only the part of it that says the plugin directory is
	// there; the other two are asserted because the image sets them, in the same
	// spirit as the filesystem listing being exhaustive.
	errs = append(errs, m.checkImageEnv(ctx, image)...)

	return errs
}

// checkImageEnv checks the three environment variables the image pins.
//
// PATH is checked for containing the plugin directory rather than for equalling
// anything: docs/container/SPEC.md covers that one guarantee and explicitly not
// the rest of the value, so an assertion on the whole string would be this check
// making a promise the document does not.
//
// HOME and TMPDIR are checked for naming the directories the listing accounts
// for. TMPDIR names a path the image does not contain, and that is deliberate
// rather than a fault: the deployment mounts a tmpfs or an emptyDir there if it
// wants one, and cpybkc reads no ambient temporary directory at all since #184.
// Both halves are checked, and by different checks — that the variable names
// /tmp is here, and that the image does not carry one is the exhaustive listing's.
func (m *Cpybkc) checkImageEnv(ctx context.Context, image *dagger.Container) []error {
	var errs []error

	value, err := image.EnvVariable(ctx, "PATH")
	switch {
	case err != nil:
		errs = append(errs, fmt.Errorf("reading PATH: %w", err))
	case !slices.Contains(strings.Split(value, ":"), pluginDir):
		errs = append(errs, fmt.Errorf("PATH is %q, which does not contain the plugin directory %q", value,
			pluginDir))
	}

	for _, want := range []struct{ name, value string }{
		{"HOME", homeDir},
		{"TMPDIR", tmpDir},
	} {
		got, err := image.EnvVariable(ctx, want.name)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("reading %s: %w", want.name, err))
		case got != want.value:
			errs = append(errs, fmt.Errorf("%s is %q, want %q: what a process reads out of it is a property of "+
				"the image rather than of the runtime that started it", want.name, got, want.value))
		}
	}

	return errs
}

// checkImageIsTheCLI runs the entrypoint and requires cpybkc's version line
// back, as the image's own user and then as an arbitrary other one.
//
// The behavioural half of the entrypoint guarantee: docs/container/SPEC.md
// promises that a caller's arguments are cpybkc's arguments, and --version is
// the one invocation docs/cli/SPEC.md requires to succeed without touching
// anything — it contacts nothing, reads no manifest, writes one line and exits
// 0. An entrypoint repointed at some other executable would answer differently
// or not at all.
//
// The second run is the other promise in the same command. "The image MUST NOT
// require its own UID" is what makes `--user $(id -u):$(id -g)` the recommended
// invocation whenever output lands in a bind mount, and an executable readable
// only by 65532 would break it — which is a failure a caller sees and this
// repository never would.
func (m *Cpybkc) checkImageIsTheCLI(ctx context.Context, image *dagger.Container) []error {
	var errs []error

	for _, user := range []string{imageUser, overrideUser} {
		line, err := image.
			WithUser(user).
			WithExec([]string{"--version"}, dagger.ContainerWithExecOpts{UseEntrypoint: true}).
			Stdout(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("running --version through the image's entrypoint as user %s: %w",
				user, err))

			continue
		}

		if err := checkVersionLine(line); err != nil {
			errs = append(errs, fmt.Errorf("as user %s, %w", user, err))
		}
	}

	return errs
}

// checkImageBuild reads the build settings out of the executable in the image
// and requires the ones the contract rests on.
//
// `go version -m` is the binary describing its own build, so this is the claim
// checked against the artifact rather than against the source of the pipeline
// that produced it. Since #185 nothing in this repository passes those flags —
// the archetype compiles the CLI — which makes this check more load-bearing
// rather than less: it is now the only thing standing between an upstream change
// of build flags and an image that cannot start.
//
// Three settings, and each one is a criterion of #55 that nothing else can see:
//
//   - CGO_ENABLED=0 is what makes the executable a single static file. The
//     image has no loader and no libc, so a dynamically linked one does not
//     start; Build already runs the engine-platform binary in an empty image,
//     and this is the same claim for the platforms that cannot be run without
//     emulation.
//   - -trimpath=true is what makes the build reproducible. Without it the
//     binary carries the directory it was compiled in, so two builds of one
//     commit in two checkouts produce two different images.
//   - GOOS and GOARCH are what makes the multi-platform index real. A
//     cross-compile that silently fell back to the engine's own architecture
//     would produce an index whose arm64 manifest holds an amd64 executable,
//     which fails for the first person to pull it on the platform they asked
//     for and for nobody here.
func (m *Cpybkc) checkImageBuild(
	ctx context.Context,
	image *dagger.Container,
	platform dagger.Platform,
) []error {
	const mountedAt = "/image"

	goos, goarch, ok := strings.Cut(string(platform), "/")
	if !ok {
		return []error{fmt.Errorf("platform %q is not GOOS/GOARCH", platform)}
	}

	// Read in a container that has a Go toolchain, over the image's filesystem
	// mounted as data. Nothing is run *in* the image, which is the point of the
	// image having no shell — and the binary being read may be for another
	// architecture, which `go version -m` does not care about because it reads
	// the file rather than executing it.
	out, err := dag.Go().
		Container(m.Source).
		WithMountedDirectory(mountedAt, image.Rootfs()).
		WithExec([]string{"go", "version", "-m", mountedAt + cliPath()}).
		Stdout(ctx)
	if err != nil {
		return []error{fmt.Errorf("reading the build settings of the executable in the image: %w", err)}
	}

	settings := buildSettings(out)

	want := []struct {
		setting string
		value   string
		why     string
	}{
		{"CGO_ENABLED", "0", "the image has no libc and no loader, so a dynamically linked cpybkc does not start"},
		{"-trimpath", "true", "without it the binary carries the directory it was compiled in, and the image is " +
			"no longer a function of the source alone"},
		{"GOOS", goos, "the image would carry an executable for another operating system"},
		{"GOARCH", goarch, "the index's manifest for this platform would carry an executable for another one"},
	}

	var errs []error
	for _, w := range want {
		got, ok := settings[w.setting]
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("the executable in the image states no %s; it has to be %s, because %s",
				w.setting, w.value, w.why))
		case got != w.value:
			errs = append(errs, fmt.Errorf("the executable in the image was built with %s=%s, want %s: %s",
				w.setting, got, w.value, w.why))
		}
	}

	return errs
}

// checkShippedIr compares the IR schema in the image, byte for byte, against the
// artifacts a release publishes.
//
// docs/container/SPEC.md promises the descriptor set in the image is identical
// to the ir.binpb asset on the matching release — two ways of getting one
// artifact rather than two artifacts — and the same of the sources against the
// tree ir-protos.tar.gz is cut from. That promise is what lets a build fetch
// whichever is cheaper and stop; two artifacts that merely agreed today would
// make the choice a gamble on nobody having changed one recipe.
//
// The listing check beside this one is about the modes and owners of those
// paths, and it would pass on a stale descriptor set with the right mode. This
// one is about the bytes, and it would pass on a correct file nobody could read.
// Neither subsumes the other.
//
// `diff --recursive --brief` rather than a comparison per file, because "Only in
// …" is the finding for a schema file the image gained or lost, and a
// file-by-file loop would only ever check the files somebody remembered to name.
// It runs in the toolchain container over the image's filesystem mounted as
// data: the image has no shell, and the executable being compared may be for
// another architecture, which is irrelevant to a byte comparison.
func (m *Cpybkc) checkShippedIr(ctx context.Context, image *dagger.Container) []error {
	const (
		mountedAt = "/image"
		wantAt    = "/want"
	)

	_, err := dag.Go().
		Container(m.Source).
		WithMountedDirectory(mountedAt, image.Rootfs()).
		WithFile(wantAt+"/"+irDescriptorSetFile, m.IrDescriptorSet()).
		WithDirectory(wantAt+"/"+irProtoDirName, m.irProtoTree()).
		WithExec([]string{"diff", "--recursive", "--brief", wantAt, mountedAt + irDir}).
		Sync(ctx)
	if err != nil {
		return []error{fmt.Errorf("the IR schema under %s is not the artifacts this repository publishes; the "+
			"descriptor set in the image and the ir.binpb asset on a release are one artifact reachable two ways, "+
			"and the sources are the tree ir-protos.tar.gz is cut from: %w", irDir, err)}
	}

	return nil
}

// buildSettings reads `go version -m` output into the settings it reports.
//
// Every line but the first is a tab-indented `<key>\t<value>` pair, and the ones
// this file wants are under the `build` key, each of them a single
// `<setting>=<value>` field. A line shaped any other way is not a build setting
// and is skipped rather than guessed at.
func buildSettings(out string) map[string]string {
	settings := map[string]string{}

	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "build" {
			continue
		}

		if setting, value, ok := strings.Cut(fields[1], "="); ok {
			settings[setting] = value
		}
	}

	return settings
}

// baseImageContents is every path the base image is allowed to contain, with the
// kind, the owner and the mode each one must have.
//
// It is exhaustive on purpose. docs/container/SPEC.md's "Shell or no shell" says
// the base is scratch plus the files that document names, and an exhaustive
// listing is the only form of that claim which stays true when somebody adds a
// file: a spot check for /bin/sh passes on an image carrying a busybox under
// another name. It is also the whole of what makes "adopting the archetype
// changed nothing a consumer can see" checkable rather than asserted, so every
// row below was re-derived against the image the archetype builds rather than
// edited where the old listing failed.
//
// There is no /usr/local/bin row, and its absence is the change #185 made to a
// covered guarantee. Nothing is in the plugin directory — cpybkc's own generator
// ships as its own image — and the archetype refuses a contribution to any
// directory the PATH resolves against, so nothing creates it. A derived image's
// COPY creates it, which is what derivedImageContents accounts for and what the
// worked example builds and checks on every pull request. docs/container/SPEC.md
// says so now.
//
// There is no /tmp row either, and that is the point of the listing being
// exhaustive: cpybkc makes its scratch space and each invocation's descriptor
// directory inside the project it was pointed at (#184), so nothing in the image
// needs one, and a stage that put one back would fail here rather than ship. The
// image advertises TMPDIR=/tmp all the same — that is the archetype's
// standardized environment, checked by checkImageEnv — and a deployment that
// wants one mounts it.
//
// protos is every schema source the image carries, relative to the include root
// — shippedProtos' answer, read out of the tree rather than named here.
func baseImageContents(protos []string) map[string]imageEntry {
	contents := map[string]imageEntry{
		// The application's own directory and the CLI in it. Neither path is
		// covered by docs/container/SPEC.md — that document says the CLI's own
		// path is implementation detail and a derived image reaches it through
		// the entrypoint — and both are listed here anyway, because the listing
		// is exhaustive or it is nothing. appDir is root-owned so the running
		// user cannot unlink the binary it is about to exec.
		appDir:    {kindDir, 0, 0, dirMode},
		cliPath(): {kindFile, imageUID, imageGID, executableMode},

		// HOME, empty and read-only. cpybkc writes nothing under it.
		"/home": {kindDir, 0, 0, dirMode},
		homeDir: {kindDir, 0, 0, homeMode},

		// The chain above the IR schema, created implicitly by the copies that
		// land inside it and therefore root-owned. Nothing may depend on these
		// rows; they are here because the listing is exhaustive.
		"/usr":             {kindDir, 0, 0, dirMode},
		"/usr/local":       {kindDir, 0, 0, dirMode},
		"/usr/local/share": {kindDir, 0, 0, dirMode},
		irDir:              {kindDir, 0, 0, dirMode},

		// The IR schema itself, in both the forms a release publishes: the
		// FileDescriptorSet a plugin author decodes a descriptor against with no
		// codegen in their build, and the .proto sources for the build that
		// would rather compile them. Both are 65532-owned and read-only, which
		// is the archetype's treatment of every contributed byte — and the
		// stronger form of "a derived image may copy it out, and MUST NOT modify
		// it in place", since 0444 denies the owner too.
		irDescriptorSetPath: {kindFile, imageUID, imageGID, contributedFileMode},
		irProtoDir:          {kindDir, imageUID, imageGID, contributedDirMode},
	}

	// Every directory a schema file's own package puts it in, so that the
	// include root is listed the way it is shipped: cpybkc/ir/v1/ir.proto brings
	// three directories with it, and a flattened copy would be a file whose
	// FileDescriptorProto names a path this project does not publish.
	for _, name := range protos {
		full := irProtoDir + "/" + name
		contents[full] = imageEntry{kindFile, imageUID, imageGID, contributedDirMode}

		for dir := path.Dir(full); dir != irProtoDir && dir != "/" && dir != "."; dir = path.Dir(dir) {
			contents[dir] = imageEntry{kindDir, imageUID, imageGID, contributedDirMode}
		}
	}

	return contents
}

// cliPath is where the CLI lands inside the image.
//
// It is derived from the archetype's layout rather than promised: appDir plus
// the executable's name, which the archetype takes from go.mod's module path.
// Nothing outside this module may depend on it — docs/container/SPEC.md's "The
// CLI's own path is not part of the contract" — and the checks depend on it only
// to be exhaustive about the filesystem and to recognise the entrypoint.
func cliPath() string {
	return appDir + "/" + cliBinary
}

// baseImageExecutables is what ships in the base image's plugin directory:
// nothing at all.
//
// cpybkc's own generator is not here, and its absence is a promise
// docs/container/SPEC.md makes outright. A base that carried cpybkc-gen-go would
// make the generator image #48 ships decorative — the generator would be
// reachable by a path nobody copied it to, which is precisely the private
// arrangement that leaves an extension mechanism untested by its own author.
//
// The CLI is not here either, and that is #185: the archetype puts an
// application's own binary in appDir and names it absolutely in the entrypoint,
// so the plugin directory is for what an extension adds and for nothing else.
// docs/container/SPEC.md already said the CLI's path was implementation detail,
// which is what made that free to change.
func baseImageExecutables() []string {
	return nil
}

// baseImageExecutablePaths is every executable the base image ships, where it
// lands, which is what an Entrypoint would have to name.
func baseImageExecutablePaths() []string {
	paths := []string{cliPath()}
	for _, name := range baseImageExecutables() {
		paths = append(paths, pluginDir+"/"+name)
	}

	return paths
}

// imageEntry is one path's expected kind, ownership and mode.
type imageEntry struct {
	kind string
	uid  int
	gid  int
	mode int
}

// The kinds `find -printf %y` reports for what this image is allowed to hold. A
// third — `l`, a symbolic link — is deliberately not here: docs/container/SPEC.md
// requires the files it names to be regular files, because a `COPY --from`
// naming a symlink copies the link and a runtime resolving one inside an image
// that has nothing else in it resolves a dangling name.
const (
	kindFile = "f"
	kindDir  = "d"
)

func (e imageEntry) String() string {
	kind := e.kind
	if kind == "" {
		kind = "?"
	}

	return fmt.Sprintf("%s %d:%d %04o", kind, e.uid, e.gid, e.mode)
}

// checkImageContents compares every path in an image against a listing of what
// it is allowed to hold.
//
// The listing is produced by one `find` in a container that has one, over the
// image's root filesystem mounted as data. It cannot be produced by running
// anything *in* the image, which is the point of the image having no shell, and
// a Dagger entries walk would report the paths without their owners — and
// ownership is half of what is being checked.
//
// want is given outright rather than derived, because the worked example's
// expected listing is a function of the document: that Dockerfile states the
// owner and the mode of the file it copies in, and the check that builds it
// reads both out of the committed text rather than assuming this file's
// constants.
func (m *Cpybkc) checkImageContents(
	ctx context.Context,
	image *dagger.Container,
	want map[string]imageEntry,
) []error {
	const mountedAt = "/image"

	// Numeric %U and %G rather than %u and %g: the listing container has no
	// passwd entry for 65532, so the symbolic forms would print the number
	// anyway on a good day and a name on a bad one.
	//
	// %y is the entry's kind, and it is here because "a regular file" is one of
	// the promises: find reports a symbolic link as `l` and does not follow it,
	// so a shipped file replaced by a link to one is a kind mismatch naming the
	// path rather than a mode that happens not to match.
	listing, err := dag.Go().
		Container(m.Source).
		WithMountedDirectory(mountedAt, image.Rootfs()).
		WithExec([]string{"find", mountedAt, "-mindepth", "1", "-printf", `%U %G %m %y %p\n`}).
		Stdout(ctx)
	if err != nil {
		return []error{fmt.Errorf("listing the image filesystem: %w", err)}
	}

	var errs []error

	got := make(map[string]imageEntry, len(want))
	for _, line := range strings.Split(strings.TrimSpace(listing), "\n") {
		if line == "" {
			continue
		}

		entry, path, err := parseFindLine(line, mountedAt)
		if err != nil {
			errs = append(errs, err)

			continue
		}

		got[path] = entry
	}

	for path, wantEntry := range want {
		gotEntry, ok := got[path]
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("%s: missing from the image", path))
		case gotEntry != wantEntry:
			errs = append(errs, fmt.Errorf("%s: is %v, want %v", path, gotEntry, wantEntry))
		}
	}

	for path := range got {
		if _, ok := want[path]; !ok {
			errs = append(errs, fmt.Errorf("%s: present in the image and not in the contract; the base is scratch "+
				"plus the files docs/container/SPEC.md names, plus whatever a derived image copied in, and nothing "+
				"else", path))
		}
	}

	// Sorted, because a map walk would order the failures differently on every
	// run and make two reports of one break look like two breaks.
	slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })

	return errs
}

// parseFindLine reads one `%U %G %m %y %p` line and strips the mount prefix, so
// that the paths compared are the paths inside the image.
func parseFindLine(line, prefix string) (imageEntry, string, error) {
	fields := strings.Fields(line)
	if len(fields) != 5 {
		return imageEntry{}, "", fmt.Errorf("unreadable listing line %q", line)
	}

	uid, err := strconv.Atoi(fields[0])
	if err != nil {
		return imageEntry{}, "", fmt.Errorf("unreadable uid in %q: %w", line, err)
	}

	gid, err := strconv.Atoi(fields[1])
	if err != nil {
		return imageEntry{}, "", fmt.Errorf("unreadable gid in %q: %w", line, err)
	}

	mode, err := strconv.ParseInt(fields[2], 8, 32)
	if err != nil {
		return imageEntry{}, "", fmt.Errorf("unreadable mode in %q: %w", line, err)
	}

	return imageEntry{fields[3], uid, gid, int(mode)}, strings.TrimPrefix(fields[4], prefix), nil
}

// derivedImageContents is the base image's listing plus one file copied into the
// plugin directory, which is what an image built FROM this one is allowed to
// hold.
//
// The plugin directory comes with it, because since #185 the base image does not
// have one: a COPY into /usr/local/bin creates it, and its owner and mode are
// the builder's rather than anything this repository chose. That is exactly the
// promise docs/container/SPEC.md gave up in this change, and the reason giving
// it up costs nothing is here in executable form — the directory a COPY creates
// is traversable by every UID, so the executable inside it is reachable by the
// image's user and by an overridden one.
//
// The difference between this and baseImageContents is otherwise exactly the set
// of files somebody copied in, and checking a derived image against it is how
// "the final stage only copies" becomes an assertion rather than a claim: a RUN
// that had somehow worked, or a second file nobody mentioned, is a path in the
// listing that nothing accounts for.
func derivedImageContents(base map[string]imageEntry, path string, entry imageEntry) map[string]imageEntry {
	contents := maps.Clone(base)
	contents[pluginDir] = imageEntry{kindDir, 0, 0, dirMode}
	contents[path] = entry

	return contents
}

// imagePlatform resolves the platform argument Image takes: empty is the
// engine's own, and anything else has to be a platform this repository actually
// publishes, so that a typo is an error rather than an image nobody ships.
func imagePlatform(ctx context.Context, platform string) (dagger.Platform, error) {
	if platform == "" {
		return dag.DefaultPlatform(ctx)
	}

	platforms := imagePlatforms()
	if !slices.Contains(platforms, dagger.Platform(platform)) {
		return "", fmt.Errorf("platform %q is not one this repository publishes: %v", platform, platforms)
	}

	return dagger.Platform(platform), nil
}
