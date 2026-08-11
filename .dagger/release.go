// This file publishes a release: it decides from the refs at HEAD whether there
// is one, works out which tags it carries, pushes the multi-platform image under
// every one of them, hands the digest it resolved to sign.go's Attest, and
// renders the block of release notes that says which IR version the image just
// published speaks (#59).
//
// # Why the decision is here and not an `if:` on a job
//
// A workflow that decided would have to write the tag scheme down to do it — a
// `for tag in "$VERSION" "${VERSION%.*}" …` line, or a job-level `if:` naming
// the ref shape that counts as a release. That is a second place the scheme
// lives, beside docs/container/SPEC.md's tag table, in a file that runs once per
// release and is exercised nowhere else. The two drift in the direction nobody
// notices: a prerelease moves `latest`, or a patch release forgets to move `v0`,
// and both are discovered by somebody whose `FROM` line resolved to an image
// they did not ask for.
//
// So the module reads the refs at HEAD and everything downstream of that reading
// is a function of them. TagScheme runs the same derivation over a table of
// cases on every pull request, which is what makes the scheme something this
// repository checks rather than something it intends. What is left to the
// workflow is *where* — the registry repository and the credentials — which
// genuinely is a property of the deployment rather than of the release, and is
// the reason docs/container/SPEC.md keeps the registry out of the contract.
//
// # The three edge cases the plan settles
//
// Each of these is a question a release workflow otherwise discovers in
// production, at the one moment where this project's own contract forbids the
// obvious fix:
//
//   - A **prerelease** publishes its own full version tag and moves none of the
//     other three. `v0`, `v0.2` and `latest` are what a derived Dockerfile pins
//     to pick up fixes without an edit, and a release candidate is not a fix
//     anybody consented to be given.
//   - A version carrying **`+build` metadata** is refused rather than mangled.
//     An OCI tag is `[A-Za-z0-9_.-]`, which has no `+` in it, so dropping the
//     metadata would publish `v0.2.0` and `v0.2.0+build.5` under one tag — and a
//     published full version tag is never repointed.
//   - **Two version tags on one commit** is an error. Which of them `latest`
//     should follow has no defensible answer, and a pipeline that picked one
//     would repoint a moving tag on a coin toss.
//
// A commit carrying no version tag at all is none of those: it publishes nothing
// and succeeds, because "this commit is not a release" is an answer rather than
// a fault. That is what lets the release workflow fire on every published
// release — including the IR module's `irpb/vX.Y.Z`, which is not a release of
// the image — without a filter naming tag shapes in YAML.
//
// # Why re-running a release is safe
//
// docs/container/SPEC.md says a published full version tag is never repointed,
// and that sits oddly beside a job somebody may have to run twice. Both hold at
// once because the image is a function of the source: the binary is built
// -trimpath and CGO-free, the IR artifacts are byte-deterministic, and the image
// is assembled from those alone. A second run at the same tag therefore pushes
// the same bytes, the registry stores one manifest, and every tag lands back on
// the digest it already named — a repoint of a tag onto itself, which is no
// repoint at all. publishTags asserts exactly that, by requiring every tag of one
// release to resolve to one digest.
//
// What a re-run does add is another signature and another set of attestations on
// that digest. Those are additive by design — cosign attaches rather than
// replaces, and a consumer verifying finds more than one valid statement rather
// than a conflict — so a re-run costs a transparency log entry and nothing else.
//
// The notes block is the one part that would otherwise accumulate, since
// appending twice would say everything twice. It is delimited and regenerated
// rather than appended, so splicing it into a body that already carries one is a
// fixed point.
package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"dagger/cpybkc/internal/dagger"
)

const (
	// rollingTag is docs/container/SPEC.md's rolling tag, the one that moves on
	// every release.
	rollingTag = "latest"

	// notesOpen and notesClose delimit the block Release Notes maintains inside a
	// release's body. They are HTML comments because GitHub renders release notes
	// as Markdown and a comment is the one thing that survives rendering without
	// appearing in it.
	//
	// A delimited block is what makes the step idempotent: the notes are
	// regenerated between the markers rather than appended after whatever is
	// there, so a release job run twice produces one block and not two.
	notesOpen  = "<!-- cpybkc:image -->"
	notesClose = "<!-- /cpybkc:image -->"
)

// Release publishes the base image for the release at HEAD, signs the digest it
// resolved to and attaches its provenance and SBOMs (#59).
//
// Whether there is a release at all is decided here, from the refs at HEAD: a
// single canonical version tag pointing at HEAD is a release, and anything else
// — a branch alone, no tag, a tag that is not a version — is not. A run with no
// release publishes nothing and succeeds; a run with two version tags fails, for
// the reason this file's comment gives.
//
// repository is where to publish, without a tag — `ghcr.io/zaba505/cpybkc`. It
// is the caller's because a mirror or an internal registry serves the same
// release, and docs/container/SPEC.md holds the registry out of the contract for
// the same reason. Which tags exist is not the caller's, and is not spelled
// anywhere but versionTags.
//
// Signing goes through Attest rather than through a signing path of this
// function's own, so what a release signs is what `dagger call attestations`
// checks the shape of on every pull request.
//
// It returns a report naming the repository, the digest and the tags now
// pointing at it.
//
// +cache="never"
func (m *Cpybkc) Release(
	ctx context.Context,
	// The repository's git metadata. The refs at HEAD are what decide whether
	// this commit is a release, so a checkout without tags publishes nothing.
	// +defaultPath="/.git"
	gitDir *dagger.Directory,
	// The image's repository, without a tag — `ghcr.io/zaba505/cpybkc`.
	repository string,
	// The registry username to authenticate as.
	username string,
	// The registry password or token to authenticate with.
	password *dagger.Secret,
	// The CI provider's OIDC token request endpoint —
	// `ACTIONS_ID_TOKEN_REQUEST_URL` on GitHub Actions. Signing is keyless, and
	// cosign exchanges a token from it for the certificate that signs.
	idTokenRequestUrl string,
	// The bearer token for that endpoint — `ACTIONS_ID_TOKEN_REQUEST_TOKEN` on
	// GitHub Actions. A secret, because it mints identity tokens.
	idTokenRequestToken *dagger.Secret,
	// What ran this release, for the provenance predicate's builder — the
	// workflow reference on GitHub Actions. This module cannot know what invoked
	// it, and provenance that guessed would be provenance about nothing.
	builder string,
	// The run this release came from — a URL to the workflow run on GitHub
	// Actions.
	// +optional
	invocation string,
) (string, error) {
	plan, refs, err := m.releasePlan(ctx, gitDir)
	if err != nil {
		return "", err
	}

	if plan.version == "" {
		return fmt.Sprintf("no version tag points at HEAD (refs: %s); nothing published",
			strings.Join(refs, ", ")), nil
	}

	// Every credential the run needs is checked before the first byte moves. A
	// publish that reached the registry and then found it had no way to sign
	// would leave a tag pointing at an unattested image, and this project's
	// contract says a published version tag is never repointed to correct that.
	switch {
	case repository == "":
		return "", errors.New("repository is required: it is the image's full repository, without a tag")
	case username == "" || password == nil:
		return "", errors.New("username and password are both required: a release is pushed to a registry that authenticates")
	case idTokenRequestUrl == "" || idTokenRequestToken == nil:
		return "", errors.New("idTokenRequestUrl and idTokenRequestToken are both required: every published digest is signed, and signing exchanges a workload identity token")
	case builder == "":
		return "", errors.New("builder is required: the provenance predicate names what ran the release, and this module cannot know that")
	}

	digest, err := m.publishTags(ctx, repository, plan.tags, username, password)
	if err != nil {
		return "", err
	}

	attested, err := m.Attest(ctx, gitDir, repository+"@"+digest, username, password,
		idTokenRequestUrl, idTokenRequestToken, builder, plan.version, plan.tags, invocation)
	if err != nil {
		return "", err
	}

	// Attest's own report is appended whole rather than merged into this one. It
	// is what `dagger call attest` prints on its own, and a release reading
	// differently from the function it called would be a second account of one
	// event.
	var report strings.Builder
	fmt.Fprintf(&report, "%s\n  version:    %s\n  digest:     %s\n  tags:       %s\n\n",
		repository, plan.version, digest, strings.Join(plan.tags, ", "))
	report.WriteString(attested)

	return report.String(), nil
}

// ReleaseNotes returns a release's notes with the block naming the published
// image spliced into them (#59).
//
// The block states **which IR version the image speaks**, which is the one fact
// about a release a reader cannot recover from the tag. A generator refuses a
// descriptor written against an IR version it does not implement
// (docs/plugin/SPEC.md), and the person reading that refusal has to decide
// whether to upgrade the generator or pin the CLI — so the number the CLI in
// this image writes belongs where somebody looking at the release will find it.
// It is read out of the built CLI rather than restated here, so it cannot claim
// a version the image does not produce.
//
// The tags come from the same derivation Release publishes under, and the
// decision about whether this commit is a release at all comes from the same
// refs. A tagged commit that is not a release of the image — `irpb/v0.1.0`, the
// IR module's own tag — leaves the notes exactly as they were, which is what lets
// the release workflow run this step on every release without a filter naming
// tag shapes in YAML.
//
// repository is optional, for the same reason it is Release's argument rather
// than a constant: where the image is published is a property of the deployment.
// Given one, the block names the reference to pull; given none, it names the
// tags alone.
func (m *Cpybkc) ReleaseNotes(
	ctx context.Context,
	// The repository's git metadata, for the refs that decide what this release
	// is.
	// +defaultPath="/.git"
	gitDir *dagger.Directory,
	// The notes the release carries now. Empty is a release whose notes are
	// still to be written.
	// +optional
	notes *dagger.File,
	// The image's repository, without a tag — `ghcr.io/zaba505/cpybkc`. Empty
	// leaves the pull reference out of the block.
	// +optional
	repository string,
) (string, error) {
	plan, _, err := m.releasePlan(ctx, gitDir)
	if err != nil {
		return "", err
	}

	var body string
	if notes != nil {
		body, err = notes.Contents(ctx)
		if err != nil {
			return "", fmt.Errorf("reading the notes the release carries now: %w", err)
		}
	}

	// Not a release of the image: the notes come back untouched, so the step that
	// writes them back is a no-op rather than an error.
	if plan.version == "" {
		return body, nil
	}

	irVersion, err := m.irVersion(ctx)
	if err != nil {
		return "", err
	}

	return spliceNotes(body, releaseNotesBlock(plan, irVersion, repository))
}

// TagScheme is docs/container/SPEC.md's tag table executed rather than read.
//
// The table is the part of a release that cannot be checked by releasing. A tag
// that moved when it should not have is discovered by a consumer whose `FROM`
// line resolved to something they did not ask for, and by then the tag has been
// published and this project's own contract says it is never repointed. So the
// derivation is a pure function of the refs, and this runs it over the cases that
// matter — including the ones with no release in them, since "publishes nothing"
// is as much a promise as "publishes four tags".
//
// Every expected tag below is a literal, never one of this file's own constants.
// A table written in terms of rollingTag would move with it and pass on a release
// that had quietly started publishing something else under a different name,
// which is the one failure this check exists to catch.
//
// +check
// +cache="session"
func (m *Cpybkc) TagScheme() error {
	cases := []struct {
		refs    []string
		version string
		tags    []string
		fails   bool
	}{
		// A release: the full version tag, plus the three that move.
		{
			refs:    []string{"refs/tags/v0.2.0", "refs/heads/main"},
			version: "v0.2.0",
			tags:    []string{"v0.2.0", "v0.2", "v0", "latest"},
		},
		{
			refs:    []string{"refs/tags/v1.10.3"},
			version: "v1.10.3",
			tags:    []string{"v1.10.3", "v1.10", "v1", "latest"},
		},
		// A prerelease publishes itself and moves nothing: `v0`, `v0.2` and
		// `latest` are what a derived Dockerfile pins to get fixes, and a release
		// candidate is not a fix anybody asked to be given.
		{
			refs:    []string{"refs/tags/v0.3.0-rc.1"},
			version: "v0.3.0-rc.1",
			tags:    []string{"v0.3.0-rc.1"},
		},
		// Not a release of the image. A branch, no refs at all, a tag that is not
		// a version, and three tags that look like versions and are not canonical
		// all publish nothing rather than publishing something surprising.
		{refs: []string{"refs/heads/main"}},
		{refs: []string{}},
		{refs: []string{"refs/tags/nightly", "refs/heads/main"}},
		{refs: []string{"refs/tags/0.2.0"}},
		{refs: []string{"refs/tags/v0.2"}},
		{refs: []string{"refs/tags/v01.2.0"}},
		// The IR module's own tag, which is a release of something this pipeline
		// does not publish an image for. It has to be ignored rather than
		// rejected: the release workflow fires on every published release, and
		// this is the one that must pass through it publishing nothing.
		{refs: []string{"refs/tags/irpb/v0.1.0"}},
		// Build metadata cannot be spelled in an OCI tag, so a version carrying
		// it is refused rather than silently mangled into one that can.
		{refs: []string{"refs/tags/v0.2.0+build.5"}, fails: true},
		// Two version tags at HEAD: which of them `latest` should follow is not a
		// question with a defensible answer, so it is an error and not a choice.
		{refs: []string{"refs/tags/v0.2.0", "refs/tags/v0.3.0"}, fails: true},
	}

	var errs []error
	for _, c := range cases {
		plan, err := planRelease(c.refs)
		switch {
		case c.fails && err == nil:
			errs = append(errs, fmt.Errorf("%v: planned %v, want an error", c.refs, plan.tags))
		case c.fails:
		case err != nil:
			errs = append(errs, fmt.Errorf("%v: %w", c.refs, err))
		case plan.version != c.version:
			errs = append(errs, fmt.Errorf("%v: version is %q, want %q", c.refs, plan.version, c.version))
		case !slices.Equal(plan.tags, c.tags):
			errs = append(errs, fmt.Errorf("%v: tags are %v, want %v", c.refs, plan.tags, c.tags))
		}
	}

	return errors.Join(errs...)
}

// ReleaseNotesContract checks the block a release's notes carry, and the splice
// that puts it there (#59).
//
// Two things, and each is a failure a release would otherwise ship once and
// never take back:
//
//   - The block says what it is for. It names the IR version the image speaks —
//     the fact a reader cannot recover from the tag — and every tag the release
//     publishes, and it says of a prerelease that it moves none of them.
//   - Splicing is idempotent. A release job re-run appends nothing: splicing into
//     a body that already carries a block replaces that block, leaving everything
//     around it alone, and a second splice is a fixed point.
//
// What it does not check is the number itself against a constant. The IR version
// is read out of the built CLI at release time precisely so that there is one
// statement of it, and a literal here would be the second.
//
// +check
// +cache="session"
func (m *Cpybkc) ReleaseNotesContract() error {
	var errs []error

	const irVersion = 7

	release := releasePlan{version: "v0.2.0", tags: []string{"v0.2.0", "v0.2", "v0", "latest"}}
	prerelease := releasePlan{version: "v0.3.0-rc.1", tags: []string{"v0.3.0-rc.1"}}

	block := releaseNotesBlock(release, irVersion, "ghcr.io/zaba505/cpybkc")
	for _, want := range []string{
		"IR version 7",
		"v0.2.0", "`v0.2`", "`v0`", "`latest`",
		"ghcr.io/zaba505/cpybkc:v0.2.0",
		notesOpen, notesClose,
	} {
		if !strings.Contains(block, want) {
			errs = append(errs, fmt.Errorf("the release notes block does not mention %q:\n%s", want, block))
		}
	}

	// A prerelease's block has to say that it moves nothing, because a reader who
	// sees a release published and assumes `v0` followed it is exactly who this
	// sentence is for.
	pre := releaseNotesBlock(prerelease, irVersion, "")
	switch {
	case strings.Contains(pre, "`latest`"):
		errs = append(errs, fmt.Errorf("a prerelease's notes mention `latest`, which it does not move:\n%s", pre))
	case !strings.Contains(pre, "prerelease"):
		errs = append(errs, fmt.Errorf("a prerelease's notes do not say so:\n%s", pre))
	}

	// With no repository there is no reference to pull, and the block must not
	// invent one.
	if strings.Contains(pre, "ghcr.io") {
		errs = append(errs, fmt.Errorf("a block rendered without a repository names one anyway:\n%s", pre))
	}

	// The splice, over the three bodies a release's notes can be in.
	for _, c := range []struct {
		name string
		body string
	}{
		{"notes nobody has written yet", ""},
		{"notes somebody wrote", "## What changed\n\n- the parser got faster\n"},
		{"notes ending without a newline", "## What changed"},
	} {
		once, err := spliceNotes(c.body, block)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.name, err))

			continue
		}

		if c.body != "" && !strings.Contains(once, strings.TrimSpace(c.body)) {
			errs = append(errs, fmt.Errorf("%s: the splice dropped what was already there:\n%s", c.name, once))
		}

		if !strings.Contains(once, block) {
			errs = append(errs, fmt.Errorf("%s: the splice did not add the block:\n%s", c.name, once))
		}

		// The property the release job rests on: running it again changes
		// nothing. The second splice is given a *different* block, so a fixed
		// point cannot be reached by the block happening to be identical.
		twice, err := spliceNotes(once, releaseNotesBlock(prerelease, irVersion, ""))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s, spliced twice: %w", c.name, err))

			continue
		}

		if strings.Count(twice, notesOpen) != 1 || strings.Count(twice, notesClose) != 1 {
			errs = append(errs, fmt.Errorf("%s: splicing twice left %d blocks, want 1:\n%s",
				c.name, strings.Count(twice, notesOpen), twice))
		}

		if strings.Contains(twice, "v0.2.0") {
			errs = append(errs, fmt.Errorf("%s: splicing twice kept the first block's content:\n%s", c.name, twice))
		}
	}

	// A body carrying an opening marker with no closing one is refused rather
	// than appended to. Appending would leave two openings, and the next release
	// would splice over the wrong region — which is a release's notes rewritten
	// by a bug nobody could see coming.
	if _, err := spliceNotes("## What changed\n\n"+notesOpen+"\nhalf a block\n", block); err == nil {
		errs = append(errs, errors.New("a body with an unterminated block was spliced into rather than refused"))
	}

	return errors.Join(errs...)
}

// releasePlan is what the refs at HEAD decided: the version being released, and
// every tag that ends up pointing at it. A zero value is "this commit is not a
// release of the image".
type releasePlan struct {
	version string
	tags    []string
}

// releasePlan reads the refs at HEAD and plans the release they describe,
// returning the refs alongside it so that a run publishing nothing can say what
// it saw.
func (m *Cpybkc) releasePlan(ctx context.Context, gitDir *dagger.Directory) (releasePlan, []string, error) {
	refs, err := headRefs(ctx, m.Source, gitDir)
	if err != nil {
		return releasePlan{}, nil, err
	}

	plan, err := planRelease(refs)
	if err != nil {
		return releasePlan{}, refs, err
	}

	return plan, refs, nil
}

// planRelease reads the refs at HEAD and returns what to publish.
//
// A ref counts only if it is a tag whose name is a canonical version. Everything
// else — branches, remote refs, `refs/stash`, a tag named `nightly`, the IR
// module's `irpb/vX.Y.Z` — is ignored rather than rejected, because a release
// commit routinely carries several refs and none of the others is evidence of a
// mistake.
func planRelease(refs []string) (releasePlan, error) {
	var versions []string

	for _, ref := range refs {
		name, ok := strings.CutPrefix(strings.TrimSpace(ref), "refs/tags/")
		if !ok {
			continue
		}

		if _, ok := parseVersion(name); ok {
			versions = append(versions, name)
		}
	}

	switch len(versions) {
	case 0:
		return releasePlan{}, nil
	case 1:
	default:
		return releasePlan{}, fmt.Errorf("HEAD carries more than one version tag (%s): which release this is, and "+
			"which of them the moving tags should follow, is not a question this pipeline can answer",
			strings.Join(versions, ", "))
	}

	tags, err := versionTags(versions[0])
	if err != nil {
		return releasePlan{}, err
	}

	return releasePlan{version: versions[0], tags: tags}, nil
}

// semver is a parsed canonical version tag.
type semver struct {
	major      string
	minor      string
	patch      string
	prerelease string
	build      string
}

// parseVersion reads a canonical `vMAJOR.MINOR.PATCH` tag, with the optional
// prerelease and build parts semantic versioning allows.
//
// Canonical is meant strictly: `0.2.0` without the `v`, `v0.2` without the patch
// and `v01.2.0` with a leading zero are none of them versions, and a commit
// tagged with one of them publishes nothing. That is the safe direction — a tag
// this function does not recognise costs a release nobody meant to make, where
// one it recognised too eagerly would move `latest` onto something that was never
// a release.
func parseVersion(tag string) (semver, bool) {
	// Compiled per call rather than kept as package state: this runs a handful of
	// times per release and once per case in TagScheme, and a package-level
	// variable is a piece of shared state to reason about for no measurable gain.
	re := regexp.MustCompile(`^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z.-]+))?(?:\+([0-9A-Za-z.-]+))?$`)

	fields := re.FindStringSubmatch(tag)
	if fields == nil {
		return semver{}, false
	}

	return semver{
		major:      fields[1],
		minor:      fields[2],
		patch:      fields[3],
		prerelease: fields[4],
		build:      fields[5],
	}, true
}

// versionTags is docs/container/SPEC.md's tag table, as a function.
//
// A release publishes the full version tag, which never moves, and the three
// that do: the minor tag, the major tag and the rolling tag. A prerelease
// publishes its own tag and nothing else, for the reason this file's comment
// gives.
func versionTags(tag string) ([]string, error) {
	v, ok := parseVersion(tag)
	if !ok {
		return nil, fmt.Errorf("%q is not a canonical version tag", tag)
	}

	// An OCI tag is [A-Za-z0-9_.-], which has no `+` in it. Refusing is the only
	// honest answer: mangling the metadata away would publish `v0.2.0` and
	// `v0.2.0+build.5` under one name, and this project's contract says a
	// published version tag is never repointed.
	if v.build != "" {
		return nil, fmt.Errorf("version tag %q carries build metadata, which cannot be spelled in an OCI tag: "+
			"release it under a version without one", tag)
	}

	if v.prerelease != "" {
		return []string{tag}, nil
	}

	return []string{
		tag,
		"v" + v.major + "." + v.minor,
		"v" + v.major,
		rollingTag,
	}, nil
}

// publishTags pushes the image under every tag the release carries and returns
// the digest they all point at.
//
// Every tag is a separate push of the same content, so the registry stores one
// manifest and the tags are names for it. The digests are required to agree,
// which is the machine-checkable form of the promise the tag table makes: `v0.2`
// and `v0.2.0` are the same image, or one of them is lying. It is also what makes
// a re-run visibly safe — the second run pushes the same bytes, so every tag
// lands back on the digest it already named.
func (m *Cpybkc) publishTags(
	ctx context.Context,
	repository string,
	tags []string,
	username string,
	password *dagger.Secret,
) (string, error) {
	var digest string

	for _, tag := range tags {
		ref, err := m.Publish(ctx, repository+":"+tag, username, password)
		if err != nil {
			return "", fmt.Errorf("publishing %s:%s: %w", repository, tag, err)
		}

		_, pushed, ok := strings.Cut(ref, "@")
		if !ok {
			return "", fmt.Errorf("publishing %s:%s returned %q, which is not digest-qualified",
				repository, tag, ref)
		}

		switch {
		case digest == "":
			digest = pushed
		case pushed != digest:
			return "", fmt.Errorf("%s:%s published as %s but %s:%s published as %s: every tag of one release "+
				"names one image", repository, tag, pushed, repository, tags[0], digest)
		}
	}

	return digest, nil
}

// irVersion is the IR version the published image speaks, read out of the CLI
// that image ships.
//
// Out of the artifact rather than out of the source: `cpybkc --version` names
// the version its assembler stamps into every descriptor it writes, which is the
// number a plugin's refusal quotes. A constant here would be a second statement
// of it, and the day the two disagreed the release notes would be the ones that
// lied.
func (m *Cpybkc) irVersion(ctx context.Context) (int, error) {
	line, err := m.versionLine(ctx)
	if err != nil {
		return 0, err
	}

	fields := regexp.MustCompile(`IR version (\d+)`).FindStringSubmatch(line)
	if fields == nil {
		return 0, fmt.Errorf("cpybkc --version wrote %q, which names no IR version", line)
	}

	version, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, fmt.Errorf("cpybkc --version wrote %q, whose IR version is not a number: %w", line, err)
	}

	return version, nil
}

// releaseNotesBlock renders the block a release's notes carry.
//
// It is a pure function of the plan, the IR version and where the image was
// published, so ReleaseNotesContract can read it back without a registry, a git
// checkout or a release.
//
// repository empty leaves the pull reference out rather than guessing one: where
// the image lives is a property of the deployment, and a block naming `ghcr.io`
// on a mirror's release would be telling a reader to pull from somewhere this
// release did not go.
func releaseNotesBlock(plan releasePlan, irVersion int, repository string) string {
	var b strings.Builder

	b.WriteString(notesOpen)
	b.WriteString("\n## The image this release publishes\n\n")

	if repository != "" {
		fmt.Fprintf(&b, "```console\n$ docker pull %s:%s\n```\n\n", repository, plan.version)
	}

	fmt.Fprintf(&b, "**This image speaks IR version %d.** Every descriptor the `cpybkc` in it writes carries that\n"+
		"version, and a generator that implements a lower one refuses the descriptor rather than guessing — so a\n"+
		"plugin used beside this image has to implement IR version %d or higher.\n\n", irVersion, irVersion)

	quoted := make([]string, 0, len(plan.tags))
	for _, tag := range plan.tags {
		quoted = append(quoted, "`"+tag+"`")
	}

	if len(plan.tags) == 1 {
		fmt.Fprintf(&b, "This is a **prerelease**. It publishes %s and moves none of the tags a derived Dockerfile\n"+
			"pins to pick up fixes: a release candidate is not a fix anybody consented to be given.\n\n", quoted[0])
	} else {
		fmt.Fprintf(&b, "It is a multi-platform index over %s, published under %s. Only the first of those never\n"+
			"moves; the other three follow releases, which is what they are for.\n\n",
			strings.Join(platformNames(), " and "), strings.Join(quoted, ", "))
	}

	b.WriteString("What pinning each of those buys, and how to verify the signature and attestations on the digest\n" +
		"they resolve to, is in [the base-image contract](docs/container/SPEC.md).\n")
	b.WriteString(notesClose)

	return b.String()
}

// platformNames is the published platform set as a consumer reads it.
func platformNames() []string {
	names := make([]string, 0, len(imagePlatforms()))
	for _, p := range imagePlatforms() {
		names = append(names, string(p))
	}

	return names
}

// spliceNotes puts the block into a release's notes: replacing the one already
// there, or adding it at the end.
//
// Replacing rather than appending is the whole of why a release job can be run
// twice. Everything outside the markers is left exactly as it was, because the
// text around the block is somebody's release notes and this function is a
// pipeline step.
//
// A body carrying an opening marker with no closing one is refused. Appending to
// it would leave two openings, and the next release would splice over a region
// starting at the first — which is a release's notes rewritten by a bug that
// showed up one release after the one that caused it.
func spliceNotes(body, block string) (string, error) {
	open := strings.Index(body, notesOpen)
	closing := strings.Index(body, notesClose)

	switch {
	case open < 0 && closing < 0:
		trimmed := strings.TrimRight(body, "\n")
		if trimmed == "" {
			return block + "\n", nil
		}

		return trimmed + "\n\n" + block + "\n", nil
	case open < 0:
		return "", fmt.Errorf("the notes carry %q with no %q in front of it: the block cannot be replaced without "+
			"guessing where it starts", notesClose, notesOpen)
	case closing < 0:
		return "", fmt.Errorf("the notes carry %q with no %q after it: the block cannot be replaced without "+
			"guessing where it ends", notesOpen, notesClose)
	case closing < open:
		return "", fmt.Errorf("the notes carry %q before %q: the block cannot be replaced without guessing which "+
			"is which", notesClose, notesOpen)
	}

	return body[:open] + block + body[closing+len(notesClose):], nil
}

// headRefs is every ref pointing at HEAD, as git reports them.
func headRefs(ctx context.Context, source, gitDir *dagger.Directory) ([]string, error) {
	out, err := gitContainer(source, gitDir).
		WithExec([]string{"git", "for-each-ref", "--points-at", "HEAD", "--format=%(refname)"}).
		Stdout(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the refs at HEAD: %w", err)
	}

	var refs []string

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if ref := strings.TrimSpace(line); ref != "" {
			refs = append(refs, ref)
		}
	}

	return refs, nil
}
