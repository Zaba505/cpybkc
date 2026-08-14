// This file builds the published base image — the one docs/container/SPEC.md
// describes and strangers' Dockerfiles name paths inside — and checks it against
// every promise that document makes about one (#55).
//
// # Why the image is built here rather than by the GoApp archetype
//
// .dagger/main.go's package comment expected this story to be a change of
// factory: GoLib to GoApp, and the multi-platform image build arrives with it.
// It is not, and the reason is what GoApp's image *is*. That archetype produces
// one image per binary — a scratch image holding one executable at a path of its
// own choosing, with that path as the entrypoint and nothing else set: no PATH,
// no user, no second directory, no notion of an image built FROM it.
//
// cpybkc publishes a base. Four of the six promises docs/container/SPEC.md makes
// — the plugin directory, its membership of PATH, its ownership and the non-root
// user the process runs as — are not settings GoApp is missing so much as a
// different shape of image, and teaching it that shape would be teaching it this
// repository's plugin model, which is a contract in docs/ and belongs nowhere
// else. avroc reached the same conclusion for the same reason and built its
// image in its own module (avroc#166); what is left once the customization is
// taken out is the handful of Container calls below.
//
// What is *not* rebuilt here is anything about what "checked" means. fmt, vet,
// lint and `go test -race` still route through the Z5Labs standard by way of Ci,
// and the executable is still built by the shared devex Go module's Build — with
// the platform and CGO switches that module already takes. This file adds only
// the image layout the standard has no opinion about, which is why no upstream
// change was needed to reach it and why main.go's factory is still GoLib.
//
// # Why the contract is a check rather than a comment
//
// Every promise docs/container/SPEC.md makes is a field of an OCI image
// configuration or a file in the filesystem it describes, so every one of them
// is machine-checkable — and each is the kind of promise that breaks silently
// here and loudly somewhere else. An image whose PATH lost /usr/local/bin runs
// cpybkc perfectly and fails only in a stranger's repository, at the point where
// their generator is not found. ImageContract is that document's compatibility
// guarantees table executed rather than read.
//
// The images built FROM this one are not this repository's to write — that is
// the whole of #48's argument for shipping cpybkc-gen-go as a separate image —
// but one of them is: docs/container/SPEC.md's worked example, which
// worked_example.go now assembles onto the image this file produces rather than
// interpreting its final stage against a published tag.
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

// The published image's contract, as constants. Each one is a row of
// docs/container/SPEC.md's compatibility guarantees table, and changing one here
// without changing it there is the drift ImageContract exists to catch.
const (
	// pluginDir is the plugin directory: the one path in the image a derived
	// Dockerfile writes to, and the one directory on PATH.
	pluginDir = "/usr/local/bin"

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

	// executableMode is the mode every executable in the plugin directory has:
	// readable and executable by the image's user and by any UID a caller
	// overrides it with, which is the other half of what makes that override
	// ordinary.
	executableMode = 0o755

	// dataMode is the mode every shipped data file has: readable by everybody
	// and writable by nobody but root.
	//
	// Both halves are promises docs/container/SPEC.md makes about the IR schema
	// below. World-readable is what makes `--user $(id -u):$(id -g)` an ordinary
	// configuration for a generator that reads the descriptor set, since the
	// running UID is a number this image has never heard of. Writable only by
	// root is what makes "a derived image may copy it out, and MUST NOT modify
	// it in place" hold for the running process rather than only on paper.
	dataMode = 0o644

	// dirMode is the mode of every directory in the image.
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

// Image builds the published base image for one platform:
//
//	dagger call image --platform=linux/arm64 export --path=cpybkc.tar
//
// It is the image docs/container/SPEC.md describes, and every promise that
// document makes is made here: the CLI in /usr/local/bin with that directory on
// PATH, the CLI as the entrypoint with an empty Cmd, UID and GID 65532 owning
// the plugin directory and running the process, the IR schema under
// /usr/local/share/cpybkc in both published forms, and nothing else in the
// filesystem at all — not even a writable temporary directory, which cpybkc
// stopped needing in #184.
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

	return m.image(p), nil
}

// Publish pushes the image, as a multi-platform index over every platform this
// project ships for, and returns the digest-qualified reference the registry
// stored it under:
//
//	dagger call publish --address=ghcr.io/zaba505/cpybkc:v0.2.0 \
//	  --username=… --password=env://REGISTRY_PASSWORD
//
// One index rather than one push per platform, because docs/container/SPEC.md
// promises that every published tag resolves to a multi-platform index: a
// consumer's `FROM` line names a tag and their runtime picks the manifest for the
// architecture it is on.
//
// The variants are image's own containers, so what is published is what
// ImageContract checked rather than a second build that agrees with it today.
//
// It is a function of its own, and not folded into Release, so that pushing to a
// test registry by hand is something a person can do — a publish path only ever
// exercised by a release is one whose first real invocation is the one nobody
// can retry.
//
// The digest in the returned reference is what a release signs. Nothing here
// resolves a tag afterwards to find it: what gets signed has to be what was
// pushed.
//
// Uncached, like Release, and for a reason that bites hardest on the path this
// function exists for. This is the call that mutates a registry, and a cached
// result would hand a second invocation the reference the first one resolved
// without anything having been pushed — so retrying a release that failed
// halfway would report success over a tag that is still missing at the
// destination. A push is an effect, and an effect is not a value to memoise.
//
// +cache="never"
func (m *Cpybkc) Publish(
	ctx context.Context,
	// The full image reference to push, including the tag.
	address string,
	// The registry username to authenticate as. Both this and password are
	// needed for a registry that requires authentication, which is every
	// registry this project publishes to.
	// +optional
	username string,
	// The registry password or token to authenticate with.
	// +optional
	password *dagger.Secret,
) (string, error) {
	if address == "" {
		return "", errors.New("address is required: it is the full image reference to push, including the tag")
	}

	// Half a credential is refused rather than quietly dropped. Skipping the
	// authentication because one of the two is missing turns a typo into an
	// anonymous push, which fails at the registry with a message about
	// permissions rather than about the argument that was actually wrong — and on
	// a registry that happened to allow it, would not fail at all.
	switch {
	case username != "" && password == nil:
		return "", errors.New("username was given without password: both are needed to authenticate, and " +
			"publishing with neither pushes anonymously")
	case username == "" && password != nil:
		return "", errors.New("password was given without username: both are needed to authenticate, and " +
			"publishing with neither pushes anonymously")
	}

	platforms := imagePlatforms()

	variants := make([]*dagger.Container, 0, len(platforms))
	for _, p := range platforms {
		variants = append(variants, m.image(p))
	}

	c := dag.Container()
	if username != "" {
		c = c.WithRegistryAuth(address, username, password)
	}

	return c.Publish(ctx, address, dagger.ContainerPublishOpts{PlatformVariants: variants})
}

// ImageContract checks the built image against every promise
// docs/container/SPEC.md makes about it (#55).
//
// It is a check rather than a paragraph because each promise is depended on from
// a repository this project cannot see and breaks without breaking anything
// here: an image whose PATH lost the plugin directory runs cpybkc perfectly and
// fails at the point where somebody else's generator is not found.
//
// Four groups, all on the real image rather than on a description of it:
//
//   - The OCI configuration — Entrypoint, Cmd, User and PATH. WorkingDir is not
//     among them, because that document explicitly does not cover it: a caller
//     passes their own, and the invocation the document gives them says so.
//   - The filesystem, as an exact list of every path in it with its kind, its
//     owner and its mode. Exact rather than a spot check, because "the base is
//     scratch plus the files this document names" is the promise, and it is the
//     same assertion as no shell, no libc and no package manager.
//   - The IR schema it ships, byte for byte against the artifacts a release
//     publishes. The listing above says those paths are world-readable regular
//     files; this says they are the right bytes.
//   - How the executable in it was built, read out of the binary itself: CGO
//     off, -trimpath on, and the GOOS and GOARCH of the platform whose image it
//     landed in.
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
		if err := m.imageContractOn(ctx, p); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
		}
	}

	return errors.Join(errs...)
}

// image is the base image for one platform, and the one definition of it: Image,
// ImageContract and the worked example's final stage all come through here, so
// there is no arrangement in which the image a check passed is not the image
// somebody publishes, nor one in which a derived image is built on a base
// nobody checked.
//
// The order of the calls below is the order docs/container/SPEC.md states the
// promises in, which is not an accident worth preserving so much as one worth
// not breaking: WithEntrypoint clears Cmd, so it comes before the
// WithoutDefaultArgs that asserts Cmd is empty rather than after it.
func (m *Cpybkc) image(platform dagger.Platform) *dagger.Container {
	cli := dag.Directory().WithFile(
		cliBinary,
		m.binary(platform),
		dagger.DirectoryWithFileOpts{Permissions: executableMode},
	)

	return dag.Container(dagger.ContainerOpts{Platform: platform}).
		// The plugin directory, owned by the image's user so that a generator
		// copied in by a derived image lands somewhere that user can execute
		// from. The CLI goes here too; its own path is explicitly not part of
		// the contract, and this is simply the one directory the image has.
		WithDirectory(pluginDir, cli, dagger.ContainerWithDirectoryOpts{
			Owner: imageUser,
		}).
		// The IR schema, in both the forms a release publishes: the
		// FileDescriptorSet a plugin author decodes a descriptor against with no
		// codegen in their build, and the .proto sources for the build that
		// would rather compile them. Root-owned, unlike the plugin directory —
		// these are read-only data, and the running user having no way to
		// rewrite them is what makes "MUST NOT modify it in place" true of the
		// filesystem rather than only of the document.
		WithDirectory(irDir, m.irDirectory()).
		// And nothing else. In particular no writable temporary directory:
		// cpybkc makes its scratch space and each invocation's descriptor
		// directory inside the project it was pointed at (#184), so a run needs
		// nothing writable outside the tree the caller mounted, and a scratch
		// image with no /tmp in it generates exactly as well as one with.
		//
		// PATH is set outright rather than appended to, because a scratch image
		// has no PATH to append to. Only the guarantee that it contains the
		// plugin directory is covered; the rest of the value is not.
		WithEnvVariable("PATH", pluginDir).
		WithUser(imageUser).
		WithEntrypoint([]string{pluginDir + "/" + cliBinary}).
		WithoutDefaultArgs()
}

// binary builds the CLI for one platform.
//
// It is what Binary calls with no platform at all, so the executable a
// contributor exports and the one that lands in every published image are the
// same build with one argument different — including the CGO and -trimpath
// switches, whose reasons are Binary's doc comment and whose effect
// checkImageBuild reads back out of the artifact.
//
// The platform is a cross-compile by the toolchain container rather than a
// build under emulation. Nothing about a Go build needs the target's
// architecture to be executable, and paying qemu for every compile to learn
// what `go build` already knows would be minutes per platform per run.
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

// irDirectory is the contents of irDir: the FileDescriptorSet beside an include
// root holding the schema sources.
//
// The descriptor set is IrDescriptorSet's file — the same node the release
// workflow exports and attaches — rather than a second recipe producing bytes
// that agree today. That is the whole of what makes docs/container/SPEC.md's
// "byte-identical to the ir.binpb asset on the matching release" a property of
// the build instead of a promise somebody has to keep, and it is why the same
// bytes land in every platform's image: a FileDescriptorSet is a function of the
// schema and knows nothing about an architecture.
//
// Only .proto files are copied, naming what goes in rather than what stays out,
// which is the rule internal/tools/ir-protos already applies to the archive for
// the same reason: a generated file or an editor's leavings appearing under
// proto/ would otherwise be published as part of the contract.
//
// The modes are set here rather than inherited from the checkout because
// ImageContract's listing is exhaustive down to the mode, and a file's mode in a
// git checkout is the contributor's umask. Without the chmod the image would be
// a function of whose machine built it, and the contract check would fail for
// somebody whose umask is not 022 — on a file they had not touched. u=rwX,go=rX
// leaves directories traversable and files world-readable, which is what dataMode
// and dirMode say and what the promise about an overridden UID rests on.
func (m *Cpybkc) irDirectory() *dagger.Directory {
	const staging = "/staging"

	return dag.Go().
		Container(m.Source).
		WithFile(staging+"/"+irDescriptorSetFile, m.IrDescriptorSet()).
		WithDirectory(staging+"/"+irProtoDirName, m.Source.Directory(protoSource),
			dagger.ContainerWithDirectoryOpts{Include: []string{"**/*.proto"}}).
		WithExec([]string{"chmod", "-R", "u=rwX,go=rX", staging}).
		Directory(staging)
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

// imageContractOn checks one platform's image.
//
// Every group is run and every failure collected rather than stopping at the
// first: a change that broke the entrypoint most likely broke the user and the
// plugin directory too, and one run should say so.
func (m *Cpybkc) imageContractOn(ctx context.Context, platform dagger.Platform) error {
	protos, err := m.shippedProtos(ctx)
	if err != nil {
		return err
	}

	image := m.image(platform)

	errs := m.checkImageConfig(ctx, image)
	errs = append(errs, m.checkImageContents(ctx, image, baseImageContents(protos))...)
	errs = append(errs, m.checkImageBuild(ctx, image, platform)...)
	errs = append(errs, m.checkShippedIr(ctx, image)...)
	errs = append(errs, m.checkImageIsTheCLI(ctx, image)...)

	return errors.Join(errs...)
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
	// project has explicitly not made. What pins the entrypoint to the CLI is
	// behaviour rather than shape, in checkImageIsTheCLI.
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

	path, err := image.EnvVariable(ctx, "PATH")
	switch {
	case err != nil:
		errs = append(errs, fmt.Errorf("reading PATH: %w", err))
	case !slices.Contains(strings.Split(path, ":"), pluginDir):
		errs = append(errs, fmt.Errorf("PATH is %q, which does not contain the plugin directory %q", path,
			pluginDir))
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
// invocation whenever output lands in a bind mount, and a plugin directory or an
// executable readable only by 65532 would break it — which is a failure a
// caller sees and this repository never would.
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
// that produced it: a flag dropped from binary's GoBuildOpts fails here, and so
// would one dropped upstream in the shared Go module.
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
		WithExec([]string{"go", "version", "-m", mountedAt + pluginDir + "/" + cliBinary}).
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
		WithDirectory(wantAt+"/"+irProtoDirName, m.Source.Directory(protoSource),
			dagger.ContainerWithDirectoryOpts{Include: []string{"**/*.proto"}}).
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
// owner and the mode each one must have.
//
// It is exhaustive on purpose. docs/container/SPEC.md's "Shell or no shell" says
// the base is scratch plus the files that document names, and an exhaustive
// listing is the only form of that claim which stays true when somebody adds a
// file: a spot check for /bin/sh passes on an image carrying a busybox under
// another name.
//
// The parent directories are root-owned and that is intentional — only the
// plugin directory is promised to the image's user, and a root-owned parent at
// 0755 is what stops the running user replacing the tree above it. The IR
// schema is root-owned for the stronger form of the same reason: it is data a
// derived image may copy out and must not modify, and 0644 owned by root is
// that sentence enforced rather than asserted.
//
// There is no writable temporary directory in it, and that is the point of the
// listing being exhaustive: cpybkc makes its scratch space and each
// invocation's descriptor directory inside the project it was pointed at
// (#184), so nothing in the image needs one, and a stage that put one back
// would fail here rather than ship.
// protos is every schema source the image carries, relative to the include root
// — shippedProtos' answer, read out of the tree rather than named here.
func baseImageContents(protos []string) map[string]imageEntry {
	contents := map[string]imageEntry{}

	for _, name := range []string{"/usr", "/usr/local", "/usr/local/share", irDir, irProtoDir} {
		contents[name] = imageEntry{kindDir, 0, 0, dirMode}
	}

	contents[pluginDir] = imageEntry{kindDir, imageUID, imageGID, dirMode}
	contents[irDescriptorSetPath] = imageEntry{kindFile, 0, 0, dataMode}

	for _, name := range baseImageExecutables() {
		contents[pluginDir+"/"+name] = imageEntry{kindFile, imageUID, imageGID, executableMode}
	}

	// Every directory a schema file's own package puts it in, so that the
	// include root is listed the way it is shipped: cpybkc/ir/v1/ir.proto brings
	// three directories with it, and a flattened copy would be a file whose
	// FileDescriptorProto names a path this project does not publish.
	for _, name := range protos {
		full := irProtoDir + "/" + name
		contents[full] = imageEntry{kindFile, 0, 0, dataMode}

		for dir := path.Dir(full); dir != irProtoDir && dir != "/" && dir != "."; dir = path.Dir(dir) {
			contents[dir] = imageEntry{kindDir, 0, 0, dirMode}
		}
	}

	return contents
}

// baseImageExecutables is what ships in the base image's plugin directory: the
// CLI, and nothing else.
//
// cpybkc's own generator is not here, and its absence is a promise
// docs/container/SPEC.md makes outright. A base that carried cpybkc-gen-go would
// make the generator image #48 ships decorative — the generator would be
// reachable by a path nobody copied it to, which is precisely the private
// arrangement that leaves an extension mechanism untested by its own author.
func baseImageExecutables() []string {
	return []string{cliBinary}
}

// baseImageExecutablePaths is baseImageExecutables where they land, which is
// what an Entrypoint would have to name.
func baseImageExecutablePaths() []string {
	names := baseImageExecutables()
	paths := make([]string, 0, len(names))

	for _, name := range names {
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
// The difference between this and baseImageContents is exactly the set of files
// somebody copied in, and checking a derived image against it is how "the final
// stage only copies" becomes an assertion rather than a claim: a RUN that had
// somehow worked, or a second file nobody mentioned, is a path in the listing
// that nothing accounts for.
func derivedImageContents(base map[string]imageEntry, path string, entry imageEntry) map[string]imageEntry {
	contents := maps.Clone(base)
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
