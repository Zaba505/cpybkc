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

	// dirMode is the mode of every directory in the image.
	dirMode = 0o755

	// tmpDir is a writable temporary directory, and it is the one thing in the
	// image that docs/container/SPEC.md deliberately does not cover — it lists
	// the directory under implementation detail rather than as a guarantee.
	//
	// It is here because cpybkc writes each invocation's descriptor into a
	// directory it creates under os.TempDir, and hands each generator an output
	// directory it created the same way, so a scratch image without one fails
	// every generation with an error naming /tmp. tmpDirMode is 1777 —
	// root-owned, world-writable and sticky — which is the ordinary arrangement
	// and the one that keeps working when a caller overrides the UID.
	tmpDir     = "/tmp"
	tmpDirMode = 0o1777
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
// the plugin directory and running the process, a writable temporary directory,
// and nothing else in the filesystem at all.
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
//   - The filesystem, as an exact list of every path in it with its owner and
//     its mode. Exact rather than a spot check, because "the base is scratch
//     plus the files this document names" is the promise, and it is the same
//     assertion as no shell, no libc and no package manager.
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
		// A temporary directory, because cpybkc writes each invocation's
		// descriptor into one. Not owned by the image's user: 1777 is what makes
		// it usable by whichever UID the container is actually running as.
		WithDirectory("/", m.tmpDirectory()).
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

// tmpDirectory is a directory holding nothing but `tmp`, with mode 1777, for
// grafting onto the image's root.
//
// It is staged by a real mkdir in a container that has one rather than by
// WithDirectory's permissions argument, because that argument sets the mode of
// the files written *into* a directory and not the mode of the directory
// itself — which would leave /tmp root-owned at 0755, and every generation
// failing with `permission denied` the moment the process is not root. That is
// a fault the filesystem listing catches and no configuration check would.
//
// The toolchain container is the one dag.Go() already builds from this
// repository's go.mod, rather than an image of this file's choosing: it is
// pulled by every other stage anyway, so staging one directory costs nothing
// beyond the exec.
func (m *Cpybkc) tmpDirectory() *dagger.Directory {
	const staging = "/staging"

	return dag.Go().
		Container(m.Source).
		WithExec([]string{"install", "-d", "-m", "1777", staging + tmpDir}).
		Directory(staging)
}

// imageContractOn checks one platform's image.
//
// Every group is run and every failure collected rather than stopping at the
// first: a change that broke the entrypoint most likely broke the user and the
// plugin directory too, and one run should say so.
func (m *Cpybkc) imageContractOn(ctx context.Context, platform dagger.Platform) error {
	image := m.image(platform)

	errs := m.checkImageConfig(ctx, image)
	errs = append(errs, m.checkImageContents(ctx, image, baseImageContents())...)
	errs = append(errs, m.checkImageBuild(ctx, image, platform)...)
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
// 0755 is what stops the running user replacing the tree above it.
func baseImageContents() map[string]imageEntry {
	contents := map[string]imageEntry{
		"/usr":       {0, 0, dirMode},
		"/usr/local": {0, 0, dirMode},
		pluginDir:    {imageUID, imageGID, dirMode},
		tmpDir:       {0, 0, tmpDirMode},
	}

	for _, name := range baseImageExecutables() {
		contents[pluginDir+"/"+name] = imageEntry{imageUID, imageGID, executableMode}
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

// imageEntry is one path's expected ownership and mode.
type imageEntry struct {
	uid  int
	gid  int
	mode int
}

func (e imageEntry) String() string {
	return fmt.Sprintf("%d:%d %04o", e.uid, e.gid, e.mode)
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
	listing, err := dag.Go().
		Container(m.Source).
		WithMountedDirectory(mountedAt, image.Rootfs()).
		WithExec([]string{"find", mountedAt, "-mindepth", "1", "-printf", `%U %G %m %p\n`}).
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

// parseFindLine reads one `%U %G %m %p` line and strips the mount prefix, so
// that the paths compared are the paths inside the image.
func parseFindLine(line, prefix string) (imageEntry, string, error) {
	fields := strings.Fields(line)
	if len(fields) != 4 {
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

	return imageEntry{uid, gid, int(mode)}, strings.TrimPrefix(fields[3], prefix), nil
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
func derivedImageContents(path string, entry imageEntry) map[string]imageEntry {
	contents := maps.Clone(baseImageContents())
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
