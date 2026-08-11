// This file publishes a release: it decides from the release's own tag whether
// there is one, works out which tags it carries, pushes the multi-platform image
// under every one of them, hands the digest it resolved to sign.go's Attest, and
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
// So the module is handed the release's tag, and everything downstream is a
// function of that tag and the refs at HEAD. TagScheme runs the same derivation
// over a table of cases on every pull request, which is what makes the scheme
// something this repository checks rather than something it intends. What is
// left to the workflow is *which release* — the tag of the object that triggered
// it — and *where* — the registry repository and the credentials. Both are
// genuinely properties of the deployment and of the event rather than of the tag
// scheme, and the second is why docs/container/SPEC.md keeps the registry out of
// the contract.
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
// A release whose own tag is not a canonical version is none of those: it
// publishes nothing and succeeds, because "this is not a release of the image"
// is an answer rather than a fault. That is what lets the release workflow fire
// on every published release — including the IR module's `irpb/vX.Y.Z` — without
// a filter naming tag shapes in YAML, and it holds even when the two modules are
// cut from one commit, because what decides is the tag of the release object
// rather than whatever else points at the same tree.
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
	// The tag of the release being published — `github.event.release.tag_name`
	// on GitHub Actions. It is what decides whether this release is a release of
	// the image: a canonical `vX.Y.Z` is one, and the IR module's `irpb/vX.Y.Z`
	// is not. Given none, the refs at HEAD decide instead, which is what a run by
	// hand against a checkout wants.
	// +optional
	tag string,
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
	// Every credential the run needs is checked before the first byte moves, and
	// before the run has worked out whether there is anything to publish. A
	// publish that reached the registry and then found it had no way to sign
	// would leave a tag pointing at an unattested image, and this project's
	// contract says a published version tag is never repointed to correct that.
	//
	// Ahead of the decision rather than after it, so that the configuration is
	// checked by every run of this job and not only by the ones that publish. A
	// dropped `id-token: write`, an empty repository or a rotated secret would
	// otherwise pass green on every `irpb` release and be discovered by the
	// release that was supposed to push — which is the same "first real run is on
	// a tag" failure this file is written against.
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

	plan, refs, err := m.releasePlan(ctx, gitDir, tag)
	if err != nil {
		return "", err
	}

	if plan.version == "" {
		return fmt.Sprintf("%s is not a release of the image (refs at HEAD: %s); nothing published",
			releaseName(tag), strings.Join(refs, ", ")), nil
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
// decision about whether this release is a release of the image at all is made
// from the same input — the release's own tag. A release that is not one of the
// image — `irpb/v0.1.0`, the IR module's own tag — leaves the notes exactly as
// they were, which is what lets the release workflow run this step on every
// release without a filter naming tag shapes in YAML. Passing the same tag here
// as to Release is what keeps the two from disagreeing: the block is spliced
// into the notes of the release it describes, and into no other.
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
	// The tag of the release whose notes these are — the same one Release was
	// given. It decides whether this release is a release of the image; given
	// none, the refs at HEAD decide.
	// +optional
	tag string,
	// The notes the release carries now. Empty is a release whose notes are
	// still to be written.
	// +optional
	notes *dagger.File,
	// The image's repository, without a tag — `ghcr.io/zaba505/cpybkc`. Empty
	// leaves the pull reference out of the block.
	// +optional
	repository string,
) (string, error) {
	plan, _, err := m.releasePlan(ctx, gitDir, tag)
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
		tag     string
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
		// A canonical tag beside the remote refs a fetched checkout carries still
		// publishes: everything that is not a version tag is ignored rather than
		// treated as evidence of a mistake.
		{
			refs:    []string{"refs/tags/v0.2.0", "refs/heads/main", "refs/remotes/origin/main"},
			version: "v0.2.0",
			tags:    []string{"v0.2.0", "v0.2", "v0", "latest"},
		},
		// Both modules cut from one tree, which is the shape that decides whether
		// the release workflow's claim about `irpb/vX.Y.Z` is true. The refs are
		// identical in all three cases below and the answers are not, because what
		// decides is the release object's own tag rather than what else happens to
		// point at the same commit.
		{
			refs:    []string{"refs/tags/v0.2.0", "refs/tags/irpb/v0.1.0", "refs/heads/main"},
			tag:     "v0.2.0",
			version: "v0.2.0",
			tags:    []string{"v0.2.0", "v0.2", "v0", "latest"},
		},
		// The IR module's release reaches this and publishes nothing, rather than
		// finding the CLI's tag at HEAD and republishing the image under it.
		{
			refs: []string{"refs/tags/v0.2.0", "refs/tags/irpb/v0.1.0", "refs/heads/main"},
			tag:  "irpb/v0.1.0",
		},
		// With no tag given the refs decide, which is a run by hand rather than a
		// release object.
		{
			refs:    []string{"refs/tags/v0.2.0", "refs/tags/irpb/v0.1.0", "refs/heads/main"},
			version: "v0.2.0",
			tags:    []string{"v0.2.0", "v0.2", "v0", "latest"},
		},
		// A release whose tag is not the one this checkout is at would publish
		// bytes that tag was never cut from.
		{refs: []string{"refs/tags/v0.2.0", "refs/heads/main"}, tag: "v0.3.0", fails: true},
		// Build metadata cannot be spelled in an OCI tag, so a version carrying
		// it is refused rather than silently mangled into one that can — and a
		// prerelease carrying it is refused too, rather than taking the
		// prerelease's early return before the metadata is ever looked at.
		{refs: []string{"refs/tags/v0.2.0+build.5"}, fails: true},
		{refs: []string{"refs/tags/v0.3.0-rc.1+build.5"}, fails: true},
		// Two version tags at HEAD: which of them `latest` should follow is not a
		// question with a defensible answer, so it is an error and not a choice.
		// Naming one of them as the release does not settle it, because the moving
		// tags are shared between the two.
		{refs: []string{"refs/tags/v0.2.0", "refs/tags/v0.3.0"}, fails: true},
		{refs: []string{"refs/tags/v0.2.0", "refs/tags/v0.3.0"}, tag: "v0.3.0", fails: true},
	}

	var errs []error

	for _, c := range cases {
		plan, err := planRelease(c.refs, c.tag)

		if c.fails {
			if err == nil {
				errs = append(errs, fmt.Errorf("%v (release %q): planned %v, want an error",
					c.refs, releaseName(c.tag), plan.tags))
			}

			continue
		}

		if err != nil {
			errs = append(errs, fmt.Errorf("%v (release %q): %w", c.refs, releaseName(c.tag), err))

			continue
		}

		// Two independent assertions rather than a chain: a case that got both
		// the version and the tags wrong should say so twice, and the tags are the
		// half a reader of this table came for.
		if plan.version != c.version {
			errs = append(errs, fmt.Errorf("%v (release %q): version is %q, want %q",
				c.refs, releaseName(c.tag), plan.version, c.version))
		}

		if !slices.Equal(plan.tags, c.tags) {
			errs = append(errs, fmt.Errorf("%v (release %q): tags are %v, want %v",
				c.refs, releaseName(c.tag), plan.tags, c.tags))
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

	// Both plans go through versionTags rather than being written out as literals,
	// so that what the block says about a release is checked against the tags that
	// release would actually publish. A hand-built plan would let the block call a
	// stable release a prerelease, or the reverse, and nothing here would notice.
	plan := func(version string) releasePlan {
		tags, err := versionTags(version)
		if err != nil {
			errs = append(errs, fmt.Errorf("deriving the tags for %s: %w", version, err))

			return releasePlan{version: version}
		}

		return releasePlan{version: version, tags: tags}
	}

	release := plan("v0.2.0")
	prerelease := plan("v0.3.0-rc.1")

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
	//
	// Two independent assertions and not a chain: a block that both mentions
	// `latest` and fails to say it is a prerelease is wrong twice, and a chain
	// would report one of them and hide the other — including the case where the
	// first fires spuriously and silently retires the second.
	pre := releaseNotesBlock(prerelease, irVersion, "")
	if strings.Contains(pre, "`latest`") {
		errs = append(errs, fmt.Errorf("a prerelease's notes mention `latest`, which it does not move:\n%s", pre))
	}

	if !strings.Contains(pre, "prerelease") {
		errs = append(errs, fmt.Errorf("a prerelease's notes do not say so:\n%s", pre))
	}

	// And the stable release must not call itself one, which is the half a plan
	// built out of literals could never have caught.
	if strings.Contains(block, "prerelease") {
		errs = append(errs, fmt.Errorf("a stable release's notes call it a prerelease:\n%s", block))
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
func (m *Cpybkc) releasePlan(ctx context.Context, gitDir *dagger.Directory, tag string) (releasePlan, []string, error) {
	refs, err := headRefs(ctx, m.Source, gitDir)
	if err != nil {
		return releasePlan{}, nil, err
	}

	plan, err := planRelease(refs, tag)
	if err != nil {
		return releasePlan{}, refs, err
	}

	return plan, refs, nil
}

// releaseName is how a run with nothing to publish refers to what it was asked
// about, so that the report reads the same whether a tag was given or not.
func releaseName(tag string) string {
	if tag == "" {
		return "HEAD"
	}

	return tag
}

// planRelease returns what to publish, from the tag the release carries and the
// refs at HEAD.
//
// # Which of the two decides
//
// The tag is the release object that triggered the run, and where there is one it
// is what decides: a release is a release of the image when its own tag is a
// canonical version, and is not otherwise. Deciding from the refs at HEAD instead
// would be a different question wearing the same answer, and the two come apart
// exactly when one commit carries two releases — which this repository is built
// to do, cutting `vX.Y.Z` for the CLI and `irpb/vX.Y.Z` for the IR module from
// one tree. Publishing the IR module's release would then find the CLI's tag at
// HEAD, republish the image under it at a second point in time, and splice the
// image's notes into the IR module's release, which describes an image that
// release did not publish.
//
// Given no tag the refs at HEAD decide, which is what `dagger call release` run
// by hand against a checkout wants: there is no release object to name, and the
// commit is the only thing that can say what it is.
//
// A ref counts only if it is a tag whose name is a canonical version. Everything
// else — branches, remote refs, `refs/stash`, a tag named `nightly`, the IR
// module's `irpb/vX.Y.Z` — is ignored rather than rejected, because a release
// commit routinely carries several refs and none of the others is evidence of a
// mistake.
//
// # What is still an error
//
// Two canonical version tags at HEAD, whichever release triggered the run: which
// of them the moving tags should follow has no defensible answer, and a run that
// picked one would repoint a moving tag on a coin toss. The tag being given does
// not settle it, because the moving tags are shared between both.
//
// A tag that does not point at HEAD, too. The image is built from this checkout,
// so publishing it under a tag cut from somewhere else would put a name on bytes
// that name was never given to — and a published version tag is never repointed
// to take it back.
func planRelease(refs []string, tag string) (releasePlan, error) {
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

	if len(versions) > 1 {
		return releasePlan{}, fmt.Errorf("HEAD carries more than one version tag (%s): which release this is, and "+
			"which of them the moving tags should follow, is not a question this pipeline can answer",
			strings.Join(versions, ", "))
	}

	var version string

	switch {
	case tag == "":
		if len(versions) == 1 {
			version = versions[0]
		}
	default:
		// The release that triggered this run is a release of something else —
		// the IR module, most often. Publishing nothing is the answer rather than
		// a fault, which is what lets the workflow fire on every release without
		// a filter naming tag shapes in YAML.
		if _, ok := parseVersion(tag); !ok {
			return releasePlan{}, nil
		}

		if !slices.Contains(versions, tag) {
			return releasePlan{}, fmt.Errorf("the release's tag %q does not point at HEAD (refs: %s): the image is "+
				"built from this checkout, so publishing it under that tag would name bytes the tag was never cut from",
				tag, strings.Join(refs, ", "))
		}

		version = tag
	}

	if version == "" {
		return releasePlan{}, nil
	}

	tags, err := versionTags(version)
	if err != nil {
		return releasePlan{}, err
	}

	return releasePlan{version: version, tags: tags}, nil
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
//
// # What a pure function of one version cannot see
//
// The moving tags follow *this* release, because that is what
// docs/container/SPEC.md's table says they do — the minor tag moves on each
// patch, the major tag and the rolling tag on each release. Releasing out of
// order therefore walks them backwards: a backport `v0.1.5` cut after `v0.2.0`
// has shipped publishes `v0.1.5, v0.1, v0, latest`, and `v0` and `latest` land
// back on the older image. `v0` is the tag CONTRIBUTING.md tells a derived
// Dockerfile to pin, so that is not a small blast radius.
//
// Nothing here can catch it: this is a function of one version, and TagScheme
// runs it over refs rather than over a registry, so neither sees what is already
// published. Making the moving tags follow the *highest* released version rather
// than the most recent one is a change to the published tag table and not an
// implementation detail, so it belongs in that document before it belongs here.
// Until then the constraint is on the releaser: cut releases in ascending order,
// and cut a backport before the release that supersedes it.
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

	// Read off the version rather than inferred from the number of tags. That the
	// two agree is a property of versionTags today and not a fact about a
	// release, and this block is the one artifact a reader cannot check against
	// anything else — so it says "prerelease" when the version is one, and not
	// when the tag list happens to have a single entry.
	v, _ := parseVersion(plan.version)

	if v.prerelease != "" {
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
