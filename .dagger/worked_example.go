// This file checks docs/container/SPEC.md's worked examples: the multi-stage
// Dockerfiles a stranger copies out of that document to add a generator cpybkc
// has never heard of (#54), and to read the IR out of the descriptor set the
// image ships (#57).
//
// # Why the pipeline reads a document
//
// The extension mechanism is only real if somebody who has never read this
// repository can follow it end to end, and the worked example is the only place
// that claim is made in a form they can run. A worked example that has stopped
// working is worse than none at all: it is the first thing an adopter tries and
// the last thing anybody here would notice was broken, because nothing in the Go
// build, the tests or the release artifacts reads it. It can rot through a dozen
// releases in perfect silence.
//
// So the Dockerfile in the document is the input to this check rather than a
// copy of it — the fenced block is extracted from docs/container/SPEC.md itself,
// and an edit that breaks it fails CI on the pull request that made it.
// Extracted rather than committed beside the document for the same reason: a
// Dockerfile in a testdata directory would be the thing that is checked while
// the thing in the document is the thing people read, and the two would drift
// exactly as far apart as nobody notices.
//
// # Why the final stage is replayed rather than built
//
// `FROM ghcr.io/zaba505/cpybkc:v0` names a *published* image, and a pull request
// has to check the base it just built rather than the one on the registry: the
// published tag is last release's image, and a change that moved the plugin
// directory would pass against it and break the first adopter to pull the next
// release. There is no way to point a Dockerfile's FROM at a container that
// exists only inside the pipeline.
//
// So the build stage — every line that compiles the generator, including the
// heredocs that make the example runnable with an empty build context and the
// CGO_ENABLED=0 that makes the result run in a scratch image — is handed to the
// builder exactly as committed, and the final stage is *replayed*: its FROM is
// required to name the published base, and its COPY is read for the flags and
// the paths the document itself wrote and then applied to the base image
// image.go builds (#55). What comes out is a real derived image, checked against
// the base's own contract plus the one file that COPY moved.
//
// Replaying is only honest if it cannot silently diverge, so the parser refuses
// anything it does not know how to replay: an instruction other than COPY in the
// final stage is an error naming it, rather than a line that quietly goes
// unchecked. A worked example that grows an ENV has to teach this function what
// an ENV means before CI will accept it.
package main

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"

	"dagger/cpybkc/internal/dagger"
)

const (
	// containerSpec is the document this check reads, and the only source of the
	// Dockerfile it builds.
	containerSpec = "docs/container/SPEC.md"

	// The sections a Dockerfile is taken from, each matched as a whole line. The
	// fenced block under one of these is a block a stranger is handed and runs;
	// a fenced Dockerfile anywhere else in the document is an illustration and
	// not this check's business.
	//
	// The first adds a generator cpybkc has never heard of, which is the
	// extension mechanism. The second reads the IR out of the descriptor set the
	// image ships, which is the other thing a plugin author has to be able to do
	// with the image alone (#57) — and it is a worked example rather than a
	// paragraph for the same reason the first one is: the claim is that the
	// shipped file is sufficient, and only a program that decoded a descriptor
	// with nothing else makes it.
	addAGeneratorHeading = "## Worked example: adding a generator"
	readTheIrHeading     = "## Worked example: reading the IR without generated code"

	// publishedBaseImage is the repository half of the reference the final stage
	// is required to name — without a tag, because which tag the document tells
	// an adopter to pin is docs/container/SPEC.md's decision and a check that
	// fixed it here would fail the day that advice changed.
	publishedBaseImage = "ghcr.io/zaba505/cpybkc"

	// generatorPrefix is what docs/plugin/SPEC.md's discovery rule makes of a
	// generator's name, and so what the copied executable has to be called for
	// cpybkc to find it at all.
	generatorPrefix = "cpybkc-gen-"

	// ownGenerator is this project's own generator. The worked example exists to
	// show a generator cpybkc has never heard of being added, so an example that
	// drifted into copying cpybkc-gen-go would be demonstrating the mechanism
	// against the one plugin whose absence from the base image nobody would
	// notice.
	ownGenerator = "go"

	// minimalDescriptor is a cpybkc IR descriptor in the protobuf wire format,
	// written out by hand: tag byte 0x08 is field 1 as a varint — Descriptor's
	// version — and 0x01 is IR_VERSION_1. Nothing else, because a descriptor
	// stating a version and carrying no nodes is the smallest one the IR permits
	// and every obligation the example generator has attaches to the version.
	//
	// By hand rather than by importing irpb and marshalling one, which would be
	// two lines shorter. The example exists to show that the shipped descriptor
	// set is *sufficient* — that a consumer with no generated code can decode —
	// and a check that built its input out of the generated types would be
	// resting the whole demonstration on the tier the demonstration is about
	// avoiding. Two bytes anybody can verify against proto/cpybkc/ir/v1/ir.proto
	// is the input that keeps that honest.
	minimalDescriptor = "\x08\x01"

	// irVersionName is what the example generator has to get out of those two
	// bytes: not the number 1, which it could have printed without reading
	// anything, but the enum value's name — which exists only in the descriptor
	// set the image ships and nowhere in the descriptor it was handed.
	irVersionName = "IR_VERSION_1"
)

// workedExampleChecks is the set of examples this check reads out of the
// document, and what each one earns beyond the common three groups.
//
// extra is nil where the example makes no claim past building and extending the
// image, and it is deliberately per-example rather than a flag: the IR reader's
// extra claim is that it *works*, which is a different kind of assertion from
// anything the Dockerfile's shape can carry.
func workedExampleChecks() []workedExampleCheck {
	return []workedExampleCheck{
		{heading: addAGeneratorHeading},
		{heading: readTheIrHeading, extra: (*Cpybkc).checkReadsTheShippedIr},
	}
}

// workedExampleCheck is one section of the document and the checks it earns.
type workedExampleCheck struct {
	heading string
	extra   func(m *Cpybkc, ctx context.Context, e *workedExample, derived *dagger.Container) error
}

// WorkedExample is docs/container/SPEC.md's worked examples checked rather than
// read (#54, #57): for each one it extracts the Dockerfile from that document,
// builds its build stage against an empty build context, and reads its final
// stage for the promises the document makes about one.
//
// Two examples, and they demonstrate the two halves of what the image is for: a
// generator cpybkc has never heard of arriving on PATH, and the IR being decoded
// out of the descriptor set the image ships. The second earns one check the
// first does not — it is run, because "the shipped file is sufficient" is a
// claim only a program that used it can make. See checkReadsTheShippedIr.
//
// Three groups, and each one is a sentence in that document that would otherwise
// only be a sentence:
//
//   - The Dockerfile is what the document says it is. Two stages, the second of
//     them FROM the published base, containing no RUN — the COPY-only rule the
//     absence of a shell imposes — and copying exactly one executable, named
//     cpybkc-gen-<name> for a name that is not cpybkc's own generator, into the
//     plugin directory with the owner and the mode the document requires.
//   - It builds. The build stage is handed to the builder as committed, so a
//     heredoc that no longer parses, a Go program that no longer compiles, a
//     module path that no longer resolves or a toolchain that has moved on fails
//     here.
//   - What it builds can run in a scratch image: the executable the final stage
//     copies is present at the path that stage reads from, carries an execute
//     bit, and is statically linked. That last one is the CGO_ENABLED=0 claim,
//     and it is checked rather than grepped for, because what matters is the
//     property of the file and not the spelling of the line that produced it.
//   - The final stage assembles onto the image this pipeline builds (#55). What
//     the document's COPY says — the path, the owner and the mode — is applied
//     to the base image image.go produces, and the result is checked against the
//     base's own contract plus that one file. This is the half that was
//     interpreted rather than built while there was no base to build against.
//
// One platform — the engine's own. What is under test is a document, and the
// document is platform-agnostic; the platform-specific claims are the image's,
// and ImageContract is where they get checked on every platform this project
// publishes for.
//
// +check
// +cache="session"
func (m *Cpybkc) WorkedExample(ctx context.Context) error {
	var errs []error

	// Every example, and every one of them reported: two examples that stopped
	// building are one edit to the document and not two visits to CI.
	for _, check := range workedExampleChecks() {
		if err := m.workedExampleAt(ctx, check); err != nil {
			errs = append(errs, fmt.Errorf("%q: %w", check.heading, err))
		}
	}

	return errors.Join(errs...)
}

// workedExampleAt runs one section's checks.
//
// checkExtendsTheImage hands back the derived image it assembled so that extra
// runs against the same container the contract passed over, rather than against
// a second assembly of it that could differ.
func (m *Cpybkc) workedExampleAt(ctx context.Context, check workedExampleCheck) error {
	example, err := m.workedExample(ctx, check.heading)
	if err != nil {
		return err
	}

	built := example.build()

	derived, extendsErr := m.checkExtendsTheImage(ctx, example, built)

	errs := []error{
		example.rules(),
		example.checkBuilds(ctx, built),
		extendsErr,
	}

	// Only against an image that was actually assembled: an example whose COPY
	// could not be replayed has already been reported, and running a generator
	// that is not in the image would bury that finding under a second one.
	if check.extra != nil && derived != nil {
		errs = append(errs, check.extra(m, ctx, example, derived))
	}

	return errors.Join(errs...)
}

// checkReadsTheShippedIr runs the example generator inside the derived image and
// requires it to have decoded a descriptor through the shipped descriptor set
// alone (#57).
//
// This is the claim the Dockerfile's shape cannot make. Everything else about
// this example is identical to the first one — two stages, one static executable
// copied into the plugin directory — and all of that would pass on a generator
// that read the descriptor set's path and ignored it. What separates them is
// whether the file at /usr/local/share/cpybkc/ir.binpb is present, readable and
// sufficient, and the only thing that answers is a program that used it.
//
// Three promises fall out of the one exec:
//
//   - The descriptor set is sufficient. The generator was built with no
//     dependency on this repository's Go module, so the message type it decodes
//     with came out of the shipped file or it did not exist. The output has to
//     name the IR version, which is an enum value name that appears in the
//     descriptor set and not in the two bytes the generator was handed.
//   - It is readable by an overridden UID. The exec runs as an arbitrary other
//     user, which is the invocation docs/container/SPEC.md recommends whenever
//     output lands in a bind mount, and a shipped file readable only by 65532
//     would fail here rather than in an adopter's pipeline.
//   - The output directory works. It is created by this check, at a path the
//     image does not carry, owned by the running user. That the *check* has to
//     provide it is the point rather than a detail: the image has no writable
//     directory of its own, because cpybkc needs none — its scratch space and
//     each invocation's descriptor directory are made inside the project it was
//     pointed at (#184), which here is nothing but a place to run a generator by
//     hand. A generator invoked the ordinary way is handed a directory cpybkc
//     already made, in a tree the caller already mounted.
func (m *Cpybkc) checkReadsTheShippedIr(
	ctx context.Context,
	e *workedExample,
	derived *dagger.Container,
) error {
	// A directory this check adds to the container, and the only writable one
	// in it. Not /tmp: the image has none since #184, and reintroducing one
	// here — even only for the exec — would be this check quietly restoring the
	// thing the story deleted, so an image that had lost it would still pass.
	// Not the plugin directory either, which the document's own COPY owns, and
	// not a mounted source directory, which would make the check depend on the
	// host's filesystem instead of on the image.
	const (
		workspace  = "/example"
		descriptor = workspace + "/descriptor.binpb"
		out        = workspace + "/out"
	)

	generator := e.copies[0].operands[1]

	// Not through the entrypoint: cpybkc is what would normally exec a
	// generator, and what is under test is the generator reading the image's own
	// files, so it is run directly at the path the document's COPY put it.
	ran := derived.
		WithUser(overrideUser).
		WithNewFile(descriptor, minimalDescriptor, dagger.ContainerWithNewFileOpts{
			Permissions: contributedFileMode,
		}).
		WithDirectory(out, dag.Directory(), dagger.ContainerWithDirectoryOpts{
			Owner: overrideUser,
		}).
		WithExec([]string{generator, "--descriptor", descriptor, "--out", out})

	written, err := ran.Directory(out).Entries(ctx)
	if err != nil {
		return fmt.Errorf("running %s in the derived image as user %s, against a descriptor stating %s: %w",
			generator, overrideUser, irVersionName, err)
	}

	if len(written) != 1 {
		return fmt.Errorf("%s wrote %d files under its output directory (%v); the example writes one, and a "+
			"generator that wrote none decoded nothing", generator, len(written), written)
	}

	got, err := ran.File(path.Join(out, written[0])).Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading what %s wrote: %w", generator, err)
	}

	if !strings.Contains(got, irVersionName) {
		return fmt.Errorf("%s wrote %q, which does not name %s; that name is in the descriptor set the image "+
			"ships and not in the descriptor the generator was handed, so output without it is output the "+
			"shipped file was not read for", generator, got, irVersionName)
	}

	return nil
}

// workedExample is the document's Dockerfile, parsed and judged.
type workedExample struct {
	// dockerfile is the fenced block verbatim, as it appears in the document.
	dockerfile string
	// buildStage is everything before the final FROM: the stages that compile
	// the generator, with the document's own preamble kept, since the heredocs
	// depend on the syntax directive it carries.
	buildStage string
	// base is the image reference the final stage is FROM, tag and all.
	base string
	// copies is every COPY in the final stage, and other holds every instruction
	// there that is not one — which is a fault, reported by rules.
	copies []copyInstruction
	other  []string
	// stages is the set of stage names the build half defines, so that a
	// --from naming a stage that does not exist is caught here rather than by a
	// builder error nobody reads.
	stages map[string]bool
}

// copyInstruction is one COPY in the final stage, split into what it is flagged
// with and what it moves.
type copyInstruction struct {
	flags    map[string]string
	operands []string
}

// mode is the file mode --chmod names, read as octal the way a Dockerfile means
// it.
func (c copyInstruction) mode() (int, error) {
	mode, err := strconv.ParseInt(c.flags["chmod"], 8, 32)
	if err != nil {
		return 0, fmt.Errorf("the final stage's COPY is --chmod=%q, which is not a file mode: %w",
			c.flags["chmod"], err)
	}

	return int(mode), nil
}

// owner is the UID and GID --chown names.
//
// Numbers only, because the image has no /etc/passwd for a name to resolve
// against — which is the reason docs/container/SPEC.md pins the identity as a
// number in the first place, and why a `--chown=cpybkc` in the example would be
// a line that fails in a stranger's build rather than in this one.
func (c copyInstruction) owner() (int, int, error) {
	chown := c.flags["chown"]

	user, group, ok := strings.Cut(chown, ":")
	if !ok {
		return 0, 0, fmt.Errorf("the final stage's COPY is --chown=%q, which names no group; the image has no "+
			"passwd file, so both halves are numbers", chown)
	}

	uid, err := strconv.Atoi(user)
	if err != nil {
		return 0, 0, fmt.Errorf("the final stage's COPY is --chown=%q, whose user is not a UID: %w", chown, err)
	}

	gid, err := strconv.Atoi(group)
	if err != nil {
		return 0, 0, fmt.Errorf("the final stage's COPY is --chown=%q, whose group is not a GID: %w", chown, err)
	}

	return uid, gid, nil
}

// workedExample reads the Dockerfile out of the document's named section and
// parses it.
func (m *Cpybkc) workedExample(ctx context.Context, heading string) (*workedExample, error) {
	spec, err := m.Source.File(containerSpec).Contents(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", containerSpec, err)
	}

	dockerfile, err := workedExampleDockerfile(spec, heading)
	if err != nil {
		return nil, err
	}
	return parseWorkedExample(dockerfile)
}

// workedExampleDockerfile returns the fenced dockerfile block under the given
// heading, verbatim.
func workedExampleDockerfile(spec, heading string) (string, error) {
	lines := strings.Split(spec, "\n")

	i := 0
	for ; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " ") == heading {
			break
		}
	}
	if i == len(lines) {
		return "", fmt.Errorf("%s: no %q heading; this check reads the Dockerfile out of that section",
			containerSpec, heading)
	}

	for ; i < len(lines); i++ {
		if lines[i] == "```dockerfile" {
			break
		}
		if strings.HasPrefix(lines[i], "## ") && strings.TrimRight(lines[i], " ") != heading {
			return "", fmt.Errorf("%s: %q holds no ```dockerfile block; the worked example is what this check builds",
				containerSpec, heading)
		}
	}
	if i == len(lines) {
		return "", fmt.Errorf("%s: %q holds no ```dockerfile block; the worked example is what this check builds",
			containerSpec, heading)
	}

	i++
	start := i
	for ; i < len(lines); i++ {
		if lines[i] == "```" {
			return strings.Join(lines[start:i], "\n") + "\n", nil
		}
	}
	return "", fmt.Errorf("%s: the worked example's ```dockerfile block is never closed", containerSpec)
}

// parseWorkedExample splits the Dockerfile at its final FROM and reads the stage
// that follows.
func parseWorkedExample(dockerfile string) (*workedExample, error) {
	lines := strings.Split(dockerfile, "\n")

	var froms []int
	for n, line := range lines {
		if fields := strings.Fields(line); len(fields) > 0 && strings.EqualFold(fields[0], "FROM") {
			froms = append(froms, n)
		}
	}
	if len(froms) == 0 {
		return nil, fmt.Errorf("%s: the worked example has no FROM instruction", containerSpec)
	}
	if len(froms) == 1 {
		return nil, fmt.Errorf("%s: the worked example is a single stage; it is a multi-stage Dockerfile, "+
			"because the stage built FROM the cpybkc image cannot compile anything",
			containerSpec)
	}

	final := froms[len(froms)-1]
	stages := map[string]bool{}
	for _, n := range froms[:len(froms)-1] {
		if name, ok := stageName(strings.Fields(lines[n])); ok {
			stages[name] = true
		}
	}

	fields := strings.Fields(lines[final])
	if len(fields) < 2 {
		return nil, fmt.Errorf("%s: the worked example's final FROM names no image", containerSpec)
	}

	example := &workedExample{
		dockerfile: dockerfile,
		buildStage: strings.Join(lines[:final], "\n") + "\n",
		base:       fields[1],
		stages:     stages,
	}

	for _, instruction := range instructions(lines[final+1:]) {
		fields := strings.Fields(instruction)
		if len(fields) == 0 {
			continue
		}
		if !strings.EqualFold(fields[0], "COPY") {
			example.other = append(example.other, fields[0])
			continue
		}
		copied := copyInstruction{flags: map[string]string{}}
		for _, field := range fields[1:] {
			if !strings.HasPrefix(field, "--") {
				copied.operands = append(copied.operands, field)
				continue
			}
			name, value, _ := strings.Cut(strings.TrimPrefix(field, "--"), "=")
			copied.flags[name] = value
		}
		example.copies = append(example.copies, copied)
	}
	return example, nil
}

// stageName returns the name a FROM line gives its stage.
func stageName(fields []string) (string, bool) {
	for i := 1; i+1 < len(fields); i++ {
		if strings.EqualFold(fields[i], "AS") {
			return fields[i+1], true
		}
	}
	return "", false
}

// instructions joins a stage's continued lines and drops its comments and its
// blank lines, so that a rule below reads one instruction at a time however the
// document chose to wrap it.
func instructions(lines []string) []string {
	var out []string
	var current string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if current == "" && (trimmed == "" || strings.HasPrefix(trimmed, "#")) {
			continue
		}
		if before, ok := strings.CutSuffix(trimmed, `\`); ok {
			current += before + " "
			continue
		}
		out = append(out, current+trimmed)
		current = ""
	}
	if current != "" {
		out = append(out, current)
	}
	return out
}

// rules reports every way the document's Dockerfile is not the Dockerfile the
// document describes. They are reported together rather than one per run,
// because an example with two things wrong with it is one edit and not two.
func (e *workedExample) rules() error {
	var errs []error

	repository, tag, _ := strings.Cut(e.base, ":")
	switch {
	case repository != publishedBaseImage:
		errs = append(errs, fmt.Errorf("the final stage is FROM %s; it has to be FROM %s, "+
			"because the worked example is the published image being extended",
			e.base, publishedBaseImage))
	case tag == "":
		errs = append(errs, fmt.Errorf("the final stage is FROM %s with no tag; a derived Dockerfile pins one, "+
			"and which one to pin is what the tags section is for", e.base))
	}

	for _, instruction := range e.other {
		if strings.EqualFold(instruction, "RUN") {
			errs = append(errs, errors.New("the final stage carries a RUN; the image has no shell, so a stage "+
				"built FROM it cannot run anything and extension is COPY-only"))
			continue
		}
		errs = append(errs, fmt.Errorf("the final stage carries a %s, which this check does not know how to "+
			"replay; the document permits it — ENV, CMD and LABEL only edit metadata — so what is missing is "+
			"the rule that checks one, not the instruction",
			strings.ToUpper(instruction)))
	}

	if len(e.copies) != 1 {
		errs = append(errs, fmt.Errorf("the final stage carries %d COPY instructions; the worked example adds "+
			"exactly one generator", len(e.copies)))
		return errors.Join(errs...)
	}

	copied := e.copies[0]
	if from := copied.flags["from"]; !e.stages[from] {
		errs = append(errs, fmt.Errorf("the final stage's COPY is --from=%q, which names no earlier stage; "+
			"the generator is compiled in one of them", from))
	}
	if chown := copied.flags["chown"]; chown != imageUser {
		errs = append(errs, fmt.Errorf("the final stage's COPY is --chown=%q; it has to be --chown=%s, "+
			"the UID and GID the image runs as", chown, imageUser))
	}
	// The mode rather than its spelling: --chmod=755 and --chmod=0755 are the
	// same instruction to a builder, and a rule that insisted on one of them
	// would be this check having an opinion the document does not. What a
	// malformed value earns is mode's own error, which names the value and says
	// what was wrong with it, rather than being folded into the rule below.
	switch mode, err := copied.mode(); {
	case err != nil:
		errs = append(errs, err)
	case mode != derivedExecutableMode:
		errs = append(errs, fmt.Errorf("the final stage's COPY is --chmod=%q, which is mode %04o; it has to be "+
			"%04o, because cpybkc discovers only a file carrying an execute bit",
			copied.flags["chmod"], mode, derivedExecutableMode))
	}

	if len(copied.operands) != 2 {
		errs = append(errs, fmt.Errorf("the final stage's COPY moves %d paths; it copies one executable to one "+
			"destination", len(copied.operands)))
		return errors.Join(errs...)
	}

	source, destination := copied.operands[0], copied.operands[1]
	if dir := path.Dir(destination); dir != pluginDir {
		errs = append(errs, fmt.Errorf("the final stage copies into %s; the plugin directory is %s, and nothing "+
			"else in the image is on PATH", dir, pluginDir))
	}
	name := path.Base(destination)
	switch {
	case !strings.HasPrefix(name, generatorPrefix):
		errs = append(errs, fmt.Errorf("the final stage copies %s into the plugin directory; cpybkc resolves a "+
			"generator called <name> to %s<name>, so nothing would ever run it", name, generatorPrefix))
	case strings.TrimPrefix(name, generatorPrefix) == "":
		errs = append(errs, fmt.Errorf("the final stage copies %q, which names no generator", name))
	case strings.TrimPrefix(name, generatorPrefix) == ownGenerator:
		errs = append(errs, fmt.Errorf("the final stage copies %s; the worked example adds a generator cpybkc "+
			"has never heard of, which is the whole thing it demonstrates", name))
	}
	if path.Base(source) != name {
		errs = append(errs, fmt.Errorf("the final stage copies %s to %s; a generator renamed on its way in is a "+
			"generator whose build produced the wrong name", source, destination))
	}

	return errors.Join(errs...)
}

// build hands the build half of the Dockerfile to the builder exactly as
// committed, against a context holding nothing but that Dockerfile.
//
// One node, shared by every check that needs what it produced, so the executable
// the final stage is assembled from is the same one the linking check passed
// over rather than a second build of it.
func (e *workedExample) build() *dagger.Container {
	return dag.Directory().
		WithNewFile("Dockerfile", e.buildStage).
		DockerBuild(dagger.DirectoryDockerBuildOpts{Dockerfile: "Dockerfile"})
}

// checkBuilds asks the built stage for the file the final stage copies out of
// it, and requires it to be a static executable.
func (e *workedExample) checkBuilds(ctx context.Context, built *dagger.Container) error {
	if len(e.copies) != 1 || len(e.copies[0].operands) != 2 {
		// rules has already reported this; there is no source path to look for.
		return nil
	}
	source := e.copies[0].operands[0]

	// One exec, so that a build stage which produced nothing reports that rather
	// than three variations on it. ldd exits non-zero for a static executable and
	// prints its libraries for a dynamic one, which is the difference
	// CGO_ENABLED=0 makes and the reason a plugin runs at all in an image with no
	// libc.
	script := fmt.Sprintf(`set -e
test -f %[1]q || { echo "the build stage produced no %[1]s, which is what the final stage copies" >&2; exit 1; }
test -x %[1]q || { echo "%[1]s carries no execute bit" >&2; exit 1; }
if ldd %[1]q >/dev/null 2>&1; then
  echo "%[1]s is dynamically linked; it needs a loader and a libc the image does not have" >&2
  ldd %[1]q >&2
  exit 1
fi`, source)

	if _, err := built.WithExec([]string{"sh", "-c", script}).Sync(ctx); err != nil {
		return fmt.Errorf("%s: the worked example: %w", containerSpec, err)
	}
	return nil
}

// checkExtendsTheImage assembles the final stage onto the base image this
// pipeline builds, and checks what comes out (#55).
//
// This is the half the file comment above called interpreted rather than built.
// `FROM ghcr.io/zaba505/cpybkc:v0` names a published image and a Dockerfile's
// FROM cannot be pointed at a container that exists only inside the pipeline, so
// the instruction is replayed instead: the COPY's own path, owner and mode are
// applied to image.go's base, which is what a builder would do with them, and
// the result is a real derived image to make assertions about. Replaying rather
// than building is also why parseWorkedExample refuses an instruction it does
// not know — a line that cannot be replayed is a line this check would otherwise
// silently skip.
//
// A pull request checks the base it just built, not the one on the registry.
// That is the whole reason this is worth doing at all: the published tag is
// last release's image, and a change that moved the plugin directory would pass
// against it and break the first adopter to pull the next release.
//
// Two assertions, and between them they are four of the five things
// docs/container/SPEC.md calls the contract rather than the example:
//
//   - The derived image is the base plus exactly one file, at the path and with
//     the owner and mode the document's COPY named. Exhaustive, so a stage that
//     had somehow run something, or copied a second file, is a path nothing
//     accounts for — which is the COPY-only rule as an assertion.
//   - It is still cpybkc. The document says ENTRYPOINT is untouched, so the
//     derived image answers --version exactly as the base does, as its own user
//     and as an overridden one.
//
// The fifth, CGO_ENABLED=0, is checkBuilds' — a property of the file, checked
// where it is produced.
func (m *Cpybkc) checkExtendsTheImage(
	ctx context.Context,
	e *workedExample,
	built *dagger.Container,
) (*dagger.Container, error) {
	if len(e.copies) != 1 || len(e.copies[0].operands) != 2 {
		// rules has already reported this; there is no COPY to replay.
		return nil, nil
	}
	copied := e.copies[0]
	source, destination := copied.operands[0], copied.operands[1]

	// A --chmod or a --chown the document did not write properly is rules'
	// finding — an unreadable mode fails its rule and so does any --chown that
	// is not the image's user. There is nothing to replay here, and a value this
	// check invented would be a promise the document did not make.
	mode, err := copied.mode()
	if err != nil {
		return nil, nil
	}
	uid, gid, err := copied.owner()
	if err != nil {
		return nil, nil
	}

	// The engine's own platform: the build stage compiled the generator for the
	// machine the pipeline is running on, so that is the only base it can be
	// copied onto. Which platforms the base is published for is ImageContract's.
	platform, err := dag.DefaultPlatform(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the engine's platform: %w", err)
	}

	protos, err := m.shippedProtos(ctx)
	if err != nil {
		return nil, err
	}

	derived := m.baseImage(platform).WithFile(destination, built.File(source), dagger.ContainerWithFileOpts{
		Owner:       copied.flags["chown"],
		Permissions: mode,
	})

	want := derivedImageContents(baseImageContents(protos), destination, imageEntry{kindFile, uid, gid, mode})

	errs := m.checkImageContents(ctx, derived, want)
	errs = append(errs, m.checkImageIsTheCLI(ctx, derived, devVersion)...)

	if err := errors.Join(errs...); err != nil {
		return derived, fmt.Errorf("%s: the worked example's final stage, built onto the image this pipeline "+
			"produces: %w", containerSpec, err)
	}
	return derived, nil
}
