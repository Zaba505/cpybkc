// This file publishes a release: it decides from the release's own tag whether
// there is one, hands the archetype the version and the repository to publish it
// under, and renders the block of release notes that says which IR version the
// image just published speaks (#59, #185).
//
// # Why the decision is here and not an `if:` on a job
//
// A workflow that decided would have to write the release rule down to do it — a
// job-level `if:` naming the ref shape that counts as a release, or a
// `for tag in "$VERSION" "${VERSION%.*}" …` line back when the tag family was
// this repository's too. That is a second place the rule lives, in a file that
// runs once per release and is exercised nowhere else. The two drift in the
// direction nobody notices: a release that publishes when it should not have, or
// one that publishes under a version nobody tagged, is discovered by somebody
// whose `FROM` line resolved to an image they did not ask for.
//
// So the module is handed the release's tag, and everything downstream is a
// function of that tag and the refs at HEAD. TagScheme runs the same derivation
// over a table of cases on every pull request, which is what makes the rule
// something this repository checks rather than something it intends. What is
// left to the workflow is *which release* — the tag of the object that triggered
// it — and *where* — the registry, the repository and the credentials. All of
// those are genuinely properties of the deployment and of the event rather than
// of the release rule, and the last is why docs/container/SPEC.md keeps the
// registry out of the contract.
//
// # What moved to the archetype, and what did not
//
// The tag *family* moved (#185). Which tags a version implies — `v0.2.0` also
// coming to name `v0.2`, `v0` and `latest`, a prerelease moving none of them —
// is the archetype's now, derived from the version a caller states, checked by
// its own table over its own literals, and published as one manifest list pushed
// once with every tag pointing at the one digest. So did the signature, the
// provenance and the SBOMs: every published digest carries a recursive cosign
// signature, a signed SLSA provenance statement whose build identity comes from
// the exchanged OIDC token's claims rather than from anything this module could
// have asserted, and an SPDX and a CycloneDX document per platform describing
// the whole image. `versionTags`, `publishTags` and the whole of sign.go are
// gone with them.
//
// What did not move is the sentence "this release is a release of the image".
// The archetype takes a version from its caller and has no opinion about where
// one comes from, and *this repository* releases by tagging a commit — and cuts
// two kinds of release from one tree, `vX.Y.Z` for the CLI and `irpb/vX.Y.Z` for
// the IR module. So a release whose own tag is a canonical version is a release
// of the image; one whose tag is not publishes nothing and succeeds, because
// "this is not a release of the image" is an answer rather than a fault; two
// canonical version tags at HEAD is an error, because which of them the moving
// tags should follow has no defensible answer; and a version carrying `+build`
// metadata is refused, because `+` cannot be spelled in an OCI tag and mangling
// it away would publish two releases under one name. planRelease is all four,
// and TagScheme is what checks them.
//
// `+build` is refused at both ends after #185, since Go.App refuses it too. That
// is belt and braces worth keeping rather than a duplicate to delete: the
// refusal here happens before a credential is used and names this repository's
// own rule, and a release that reached the archetype to find out would have
// authenticated to a registry first.
//
// # Why the contract check is a gate inside Release
//
// docs/container/SPEC.md's guarantees are checked by ImageContract, and the
// release workflow used to call it as a step of its own before calling this.
// That worked while this module built the image, because both calls came through
// one `image` function; it stops meaning what it said once the container is the
// archetype's, because the guarantee that the container a check read is *the
// very container that will be pushed* holds only within one chained call. Two
// separate `dagger call` invocations are a second, cache-identical build that
// merely agrees with the first.
//
// A published version tag is never repointed here, so "the image was checked,
// and then an identical one was pushed" is not a property worth resting a
// release on. The check is therefore a gate inside this function, over the App
// this function is about to publish, and a failure means nothing was pushed. The
// standalone `dagger call image-contract` stays, because a pull request has no
// release to gate and wants the check on its own.
//
// # Why re-running a release is safe
//
// docs/container/SPEC.md says a published full version tag is never repointed,
// and that sits oddly beside a job somebody may have to run twice. Both hold at
// once because the image is a function of the source: the binary is built
// -trimpath and CGO-free and stamped from the commit rather than from the clock,
// the IR artifacts are byte-deterministic, and the image is assembled from those
// alone. A second run at the same tag therefore pushes the same bytes, the
// registry stores one manifest, and every tag lands back on the digest it
// already named — a repoint of a tag onto itself, which is no repoint at all.
//
// What a re-run does add is another signature and another set of attestations on
// that digest. Those are additive by design — a signature is attached rather
// than replaced, and a consumer verifying finds more than one valid statement
// rather than a conflict — so a re-run costs a transparency log entry and
// nothing else.
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
	// notesOpen and notesClose delimit the block ReleaseNotes maintains inside a
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

// releaseImage is one image a release publishes: the App it is built from, the
// repository it goes to, and the contract check it is gated by.
//
// A named type rather than the anonymous struct this used to be a slice of,
// because the generator entries are now assembled in a loop over
// [publishedGenerators] and an anonymous type cannot be appended to from one.
//
// The check is the same shape for every image — see checkBaseImage and
// checkGeneratorImage — so the gate below asks all of them the same question and
// no entry can be gated by less than the others.
type releaseImage struct {
	app        *dagger.Z5LabsApp
	repository string
	check      func(context.Context, *dagger.Container, dagger.Platform, string) []error
}

// Release publishes the images for the release at HEAD (#59, #185, #180, #230).
//
// Three of them, over the same platforms and under the same release's tags: the
// base image, the generator image carrying cpybkc's own Go generator, and the
// generator image carrying the diagram generator the worked example's second
// half runs. Neither generator is an extra artifact bolted on beside the base —
// each is the base App with one more application composed into it, so each is
// signed, attested and tagged by having been published the same way rather than
// by a second arrangement here. Where each goes is derived from where the base
// went; see generatorRepository.
//
// Whether there is a release at all is decided here, from the release's own tag
// and the refs at HEAD: a canonical version tag pointing at HEAD is a release,
// and anything else — a branch alone, no tag, the IR module's `irpb/vX.Y.Z` — is
// not. A run with no release publishes nothing and succeeds; a run with two
// version tags fails, for the reason this file's comment gives.
//
// registry is the registry alone — `ghcr.io` — and repository is the image's
// path within it — `zaba505/cpybkc`. They are two arguments because the
// archetype separates them, and it separates them so that a mirror or an
// internal registry serves the same release by changing one of the two and
// nothing else. docs/container/SPEC.md holds the registry out of the contract
// for the same reason.
//
// Which tags exist is not the caller's and is no longer this module's either:
// App.Publish derives the family from the version and pushes one multi-platform
// index under every tag of it.
//
// Every published image is signed and carries provenance and SBOMs, and none of
// that is optional: the archetype refuses a publish it cannot produce provenance
// for. That is why the OIDC arguments are required here and why there is no
// `dagger call publish` beside this one — the only caller with a token to
// exchange is the release workflow. A contributor who wants the image on their
// own machine exports it: `dagger call image export --path ./cpybkc.tar`, then
// `docker load`.
//
// There is no builder or invocation argument any more. Every identifying field
// in the provenance comes out of the exchanged OIDC token's claims, because
// anything a caller could have supplied attests to nothing.
//
// The git metadata is New's, not an argument here. It used to be one, because
// the release was the only thing in this module that read git; since #185 every
// image build reads it too, so New binds it once and this reads m.GitDir. Two
// independent inputs for one fact is what that avoids, and the way it would have
// gone wrong is specific: `dagger call --git-dir=A release --git-dir=B` was
// accepted, and would have taken the release decision from one tree while the
// archetype stamped and annotated the image from the other.
//
// It returns a report naming the repository and every reference published,
// pinned to the digest it resolved to.
//
// +cache="never"
func (m *Cpybkc) Release(
	ctx context.Context,
	// The tag of the release being published — `github.event.release.tag_name`
	// on GitHub Actions. It is what decides whether this release is a release of
	// the image: a canonical `vX.Y.Z` is one, and the IR module's `irpb/vX.Y.Z`
	// is not. Given none, the refs at HEAD decide instead, which is what a run by
	// hand against a checkout wants.
	// +optional
	tag string,
	// The registry to publish to, without a repository path — `ghcr.io`.
	registry string,
	// The base image's repository within that registry, without a tag —
	// `zaba505/cpybkc`. The generator image's is derived from it, so moving this
	// one moves the whole family.
	repository string,
	// The registry username to authenticate as.
	username string,
	// The registry password or token to authenticate with.
	password *dagger.Secret,
	// The CI provider's OIDC token request endpoint —
	// `ACTIONS_ID_TOKEN_REQUEST_URL` on GitHub Actions. The publish exchanges a
	// token from it for the identity that signs, so a run that publishes needs
	// it.
	idTokenRequestUrl string,
	// The bearer token for that endpoint — `ACTIONS_ID_TOKEN_REQUEST_TOKEN` on
	// GitHub Actions. A secret, because it mints identity tokens.
	idTokenRequestToken *dagger.Secret,
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
	case registry == "":
		return "", errors.New("registry is required: it is the registry alone, such as `ghcr.io`, and never a repository path")
	case repository == "":
		return "", errors.New("repository is required: it is the image's path within the registry, without a tag")
	case username == "" || password == nil:
		return "", errors.New("username and password are both required: a release is pushed to a registry that authenticates")
	case idTokenRequestUrl == "" || idTokenRequestToken == nil:
		return "", errors.New("idTokenRequestUrl and idTokenRequestToken are both required: every published digest is signed, and signing exchanges a workload identity token")
	}

	version, refs, err := m.releasePlan(ctx, tag)
	if err != nil {
		return "", err
	}

	if version == "" {
		return fmt.Sprintf("%s is not a release of the image (refs at HEAD: %s); nothing published",
			releaseName(tag), strings.Join(refs, ", ")), nil
	}

	// The images this release publishes, and the repository each goes to. Each
	// generator's is derived rather than an argument for the reason
	// generatorRepository gives: a mirror redirects the whole family by moving
	// the CLI image, and a caller who could name them independently could publish
	// a generator the companion module's default would never look for.
	//
	// Assembled from [publishedGenerators] rather than written out, so a
	// generator added to that list is one this release publishes. The alternative
	// is a generator that ImageContract checks on every pull request and that no
	// release ever pushes — which is the gap #230 closed for `graph` and would
	// reopen for the next one.
	//
	// # The generators are first, and the order is the mitigation
	//
	// This slice is iterated for the gate and again for the push, so its order is
	// the push order. The generators lead because their repositories are the ones
	// that might not be writable: the first release under #180 *creates*
	// `<repository>-gen-<name>`, and a registry that will accept a push to an
	// existing package does not necessarily accept the creation of a new one. So
	// the questions that have never been answered are asked first, while the
	// answers still cost nothing.
	//
	// Ordering is a mitigation and not a fix, because there is no fix available
	// here — see the push loop below.
	images := make([]releaseImage, 0, len(publishedGenerators())+1)

	for _, g := range publishedGenerators() {
		images = append(images, releaseImage{
			app:        m.generatorApp(version, g),
			repository: generatorRepository(repository, g.name),
			check: func(
				ctx context.Context,
				image *dagger.Container,
				platform dagger.Platform,
				version string,
			) []error {
				return m.checkGeneratorImage(ctx, image, platform, version, g)
			},
		})
	}

	images = append(images, releaseImage{
		app:        m.baseApp(version),
		repository: repository,
		check:      m.checkBaseImage,
	})

	// The gate. Nothing is pushed until **every** image has been checked against
	// docs/container/SPEC.md on every platform they are published for, and each is
	// checked on the containers its own App carries — see this file's comment for
	// why that is not the same as having run `dagger call image-contract` a moment
	// earlier.
	//
	// checkBaseImage and checkGeneratorImage rather than sequences assembled here,
	// so that a check added to ImageContract is a check this gate acquires.
	//
	// All of them are gated before any is pushed, rather than each image being
	// checked and then published in turn, so that an image failing its contract
	// cannot be discovered after another one is already out.
	//
	// That rules out one cause of a half-published release and **not** the
	// general case, which is worth saying plainly because the arrangement looks
	// stronger than it is. The pushes below are sequential across three
	// repositories and a registry has no transaction spanning them, so anything
	// that fails at push time — a credential, a rate limit, a repository that
	// cannot be created — still leaves the earlier images published under tags
	// this project's contract says are never repointed.
	//
	// # This is also where a version that disagrees with the tag is refused
	//
	// Each check is handed the version, and each holds the binaries in its image
	// to it: the CLI's --version line and the version each generator's refusal
	// names, all against the tag this release was cut from (#181). Here rather
	// than in a check of its own, because there is exactly one moment at which
	// the version a binary reports and the version a release is being cut under
	// are both in hand, and it is this one — before anything is pushed, over the
	// very containers that would be. A check of its own would have to rebuild
	// every image to ask, and would be a second place a release can be refused
	// from.
	//
	// It costs nothing at a release to say so, and it is worth saying: the same
	// two functions run on every pull request under devVersion, so what this gate
	// adds is the *comparison against the tag* rather than a check that has never
	// been exercised until now.
	var errs []error
	for _, image := range images {
		for _, platform := range imagePlatforms() {
			for _, err := range image.check(ctx, image.app.Container(platform), platform, version) {
				errs = append(errs, fmt.Errorf("%s %s: %w", image.repository, platform, err))
			}
		}
	}

	if err := errors.Join(errs...); err != nil {
		return "", fmt.Errorf("refusing to publish %s: %w", version, err)
	}

	var report strings.Builder

	fmt.Fprintf(&report, "%s\n  version:    %s\n", registry, version)

	// One publish per image, in the order the slice above fixes, and the failure
	// of one leaves the ones before it published. What makes that survivable is
	// that this whole function is safe to run again: each image is a function of
	// the source, so a re-run pushes the same bytes and every tag lands back on
	// the digest it already named. So the recovery from a half-published release
	// is to fix whatever refused the push and re-run the job — not to hand-push
	// the missing image, and never to repoint a tag.
	//
	// The report is written as it goes rather than at the end, and it is returned
	// beside the error rather than discarded, precisely so a half-published
	// release says how far it got. An error alone would leave whoever is holding
	// it unable to tell a release that pushed nothing from one that pushed two of
	// its three images.
	for _, image := range images {
		published, err := image.app.
			WithRegistry(registry, username, password).
			WithOidc(idTokenRequestUrl, idTokenRequestToken).
			Publish(ctx, []string{image.repository})
		if err != nil {
			return report.String(), fmt.Errorf("publishing %s/%s: %w", registry, image.repository, err)
		}

		fmt.Fprintf(&report, "  %s\n", image.repository)
		for _, ref := range published {
			fmt.Fprintf(&report, "    %s\n", ref)
		}

		// Read back per image rather than over the union. The three properties are
		// each about *one* release of *one* repository — every tag naming one
		// digest most of all — and checking a merged list would let a generator
		// that published nothing but its version tag hide behind the base's
		// moving ones.
		if err := checkPublished(version, published); err != nil {
			return report.String(), fmt.Errorf("%s: %w", image.repository, err)
		}
	}

	return report.String(), nil
}

// checkPublished holds what the publish actually did to what this repository
// tells a consumer it does.
//
// It is an assertion over the **result**, and that is what makes it legitimate
// rather than a second copy of the archetype's tag table. Restating the
// derivation here — v0.2.0 also names v0.2, v0 and latest — would be somebody
// else's rule written down twice, and the copy is the one that stays green after
// the original moves. Reading back what was published is the opposite: it cannot
// agree with a family that changed, because it never says what the family is.
//
// Three properties, and each is something docs/container/SPEC.md publishes to
// strangers:
//
//   - Every published reference is digest-qualified and they all name **one**
//     digest. That is the machine-checkable form of the tag table's promise that
//     v0.2 and v0.2.0 are the same image, and it is the one assertion the
//     retired publishTags made that nothing upstream restates in this
//     repository's terms.
//   - The version tag is among them. A release that published only moving tags
//     would leave the one reference this project promises never moves absent.
//   - A prerelease publishes exactly that one reference and a stable release
//     publishes more than one. That is the *shape* of the table — "a release
//     moves the other tags, a prerelease moves none" — without naming which they
//     are, so it stays true across a change to the family and false the moment
//     the distinction stops being made.
//
// It runs after the push, because there is nothing to read before it. It cannot
// unpublish, and it is not pretending to: what it turns is a contract that has
// quietly become false into a release that goes red, which is the difference
// between finding out here and finding out from a consumer whose FROM line
// resolved to something they did not ask for.
func checkPublished(version string, published []string) error {
	if len(published) == 0 {
		return fmt.Errorf("publishing %s reported no references at all", version)
	}

	var errs []error

	var digest string
	for _, ref := range published {
		_, pushed, ok := strings.Cut(ref, "@")
		if !ok {
			errs = append(errs, fmt.Errorf("%s is not digest-qualified: what a release signs has to be what was "+
				"pushed, and a tag is not a build", ref))

			continue
		}

		switch {
		case digest == "":
			digest = pushed
		case pushed != digest:
			errs = append(errs, fmt.Errorf("%s names %s where %s names %s: every tag of one release names one image",
				ref, pushed, published[0], digest))
		}
	}

	if !slices.ContainsFunc(published, func(ref string) bool {
		name, _, _ := strings.Cut(ref, "@")

		return strings.HasSuffix(name, ":"+version)
	}) {
		errs = append(errs, fmt.Errorf("nothing was published under %s itself (%s): it is the one tag this project "+
			"promises never moves", version, strings.Join(published, ", ")))
	}

	v, _ := parseVersion(version)

	switch {
	case v.prerelease != "" && len(published) != 1:
		errs = append(errs, fmt.Errorf("the prerelease %s published %d references (%s): a release candidate moves "+
			"none of the tags a derived Dockerfile pins to pick up fixes",
			version, len(published), strings.Join(published, ", ")))
	case v.prerelease == "" && len(published) == 1:
		errs = append(errs, fmt.Errorf("the release %s published only %s: a release moves the tags a derived "+
			"Dockerfile pins to pick up fixes, and this one moved none of them", version, published[0]))
	}

	return errors.Join(errs...)
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
// The decision about whether this release is a release of the image at all is
// made from the same input Release makes it from — the release's own tag. A
// release that is not one of the image — `irpb/v0.1.0`, the IR module's own tag
// — leaves the notes exactly as they were, which is what lets the release
// workflow run this step on every release without a filter naming tag shapes in
// YAML. Passing the same tag here as to Release is what keeps the two from
// disagreeing: the block is spliced into the notes of the release it describes,
// and into no other.
//
// reference is optional, for the same reason Release's registry and repository
// are arguments rather than constants: where the image is published is a
// property of the deployment. Given one, the block names the reference to pull;
// given none, it names the version alone.
//
// It is one argument here and two there, and the names differ so a call site
// cannot confuse them. Release takes a registry and a repository because the
// archetype separates them, so its `repository` is the path *within* a registry
// — `zaba505/cpybkc`. What goes in a `docker pull` line is the whole thing, so
// this one takes the whole thing and is called `reference`. Spelling both
// `repository` was the arrangement in which a workflow passing the wrong one of
// two adjacent variables produces release notes sending readers to Docker Hub,
// and nothing anywhere goes red.
func (m *Cpybkc) ReleaseNotes(
	ctx context.Context,
	// The tag of the release whose notes these are — the same one Release was
	// given. It decides whether this release is a release of the image; given
	// none, the refs at HEAD decide.
	// +optional
	tag string,
	// The notes the release carries now. Empty is a release whose notes are
	// still to be written.
	// +optional
	notes *dagger.File,
	// The image's full reference, registry included and tag excluded —
	// `ghcr.io/zaba505/cpybkc`. Empty leaves the pull reference out of the block.
	// +optional
	reference string,
) (string, error) {
	version, _, err := m.releasePlan(ctx, tag)
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
	if version == "" {
		return body, nil
	}

	irVersion, err := m.irVersion(ctx)
	if err != nil {
		return "", err
	}

	return spliceNotes(body, releaseNotesBlock(version, irVersion, reference))
}

// TagScheme is the half of the release decision this repository still makes,
// executed rather than read.
//
// What it covers is what planRelease answers — whether a release is a release of
// the image at all, and which version it is — since #180 where the generator
// image that release publishes beside the base goes, and since #181 what the
// binaries in those images report their version as. All three are decisions this
// repository makes and the archetype does not.
//
// The tag *family* that version implies is the archetype's since #185, and is
// checked by its own table over its own literals — restating it here would be a
// second copy of somebody else's rule, which is exactly what this file exists to
// avoid having.
// docs/container/SPEC.md's tag table says where each half is checked, so that a
// reader concludes neither that the table is unenforced nor that this repository
// enforces it.
//
// This is the part of a release that cannot be checked by releasing. A commit
// that published when it should not have, or published under a version nobody
// tagged, is discovered afterwards, and by then the tag is out and this
// project's own contract says it is never repointed.
//
// Every expected value below is a literal, never one of this file's own
// constants, so the check cannot move with the code it checks.
//
// +check
// +cache="session"
func (m *Cpybkc) TagScheme() error {
	cases := []struct {
		refs    []string
		tag     string
		version string
		fails   bool
	}{
		// A release: a single canonical version tag at HEAD.
		{refs: []string{"refs/tags/v0.2.0", "refs/heads/main"}, version: "v0.2.0"},
		{refs: []string{"refs/tags/v1.10.3"}, version: "v1.10.3"},
		// A prerelease is a release of the image too. What it does *not* do —
		// move the tags a derived Dockerfile pins to pick up fixes — is the
		// archetype's rule now, and its table is where that is checked.
		{refs: []string{"refs/tags/v0.3.0-rc.1"}, version: "v0.3.0-rc.1"},
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
		},
		// A release whose tag is not the one this checkout is at would publish
		// bytes that tag was never cut from.
		{refs: []string{"refs/tags/v0.2.0", "refs/heads/main"}, tag: "v0.3.0", fails: true},
		// Build metadata cannot be spelled in an OCI tag, so a version carrying
		// it is refused rather than silently mangled into one that can — and a
		// prerelease carrying it is refused too. The archetype refuses it as well;
		// this end refuses it before a credential has been used and in this
		// repository's own words.
		{refs: []string{"refs/tags/v0.2.0+build.5"}, fails: true},
		{refs: []string{"refs/tags/v0.3.0-rc.1+build.5"}, fails: true},
		// Two version tags at HEAD: which of them the moving tags should follow is
		// not a question with a defensible answer, so it is an error and not a
		// choice. Naming one of them as the release does not settle it, because
		// the moving tags are shared between the two.
		{refs: []string{"refs/tags/v0.2.0", "refs/tags/v0.3.0"}, fails: true},
		{refs: []string{"refs/tags/v0.2.0", "refs/tags/v0.3.0"}, tag: "v0.3.0", fails: true},
	}

	var errs []error

	for _, c := range cases {
		version, err := planRelease(c.refs, c.tag)

		if c.fails {
			if err == nil {
				errs = append(errs, fmt.Errorf("%v (release %q): planned %q, want an error",
					c.refs, releaseName(c.tag), version))
			}

			continue
		}

		if err != nil {
			errs = append(errs, fmt.Errorf("%v (release %q): %w", c.refs, releaseName(c.tag), err))

			continue
		}

		if version != c.version {
			errs = append(errs, fmt.Errorf("%v (release %q): version is %q, want %q",
				c.refs, releaseName(c.tag), version, c.version))
		}
	}

	// Where the generator images go, which is the other half of the release
	// decision since #180: a release states a version *and* the repositories that
	// version is published to, and only one of the two is an argument.
	//
	// The literals matter more here than anywhere else in this function, because
	// this rule is written down twice — daggerverse/cpybkc/internal/generator's
	// Repository is the other spelling, in a Go module this one cannot import.
	// Its TestRepository pins the same answers. So the rows below are not a
	// tautology over one line of code: they are one end of a drift guard whose
	// other end is a test in another module, and the failure they rule out is a
	// release publishing where the companion module's default never looks.
	for _, c := range []struct{ repository, name, want string }{
		{"zaba505/cpybkc", "go", "zaba505/cpybkc-gen-go"},
		// The second generator this repository publishes (#230), spelled out for
		// the same reason and against the same other end: `with-generator graph`
		// with no --image resolves that module's answer, and this is the one a
		// release pushes to.
		{"zaba505/cpybkc", "graph", "zaba505/cpybkc-gen-graph"},
		// A mirror redirects the whole family by moving the CLI image alone,
		// which is the property that makes the rule derived rather than constant.
		// One generator is enough to pin it: what varies here is the repository
		// and not the name, and the rows above are what pin the names.
		{"mirrors/cpybkc", "go", "mirrors/cpybkc-gen-go"},
	} {
		if got := generatorRepository(c.repository, c.name); got != c.want {
			errs = append(errs, fmt.Errorf("the generator image for %q beside %q publishes to %q, want %q",
				c.name, c.repository, got, c.want))
		}
	}

	// And which generators those rows are supposed to cover, pinned as a literal
	// against [publishedGenerators].
	//
	// Without it the rows above are a check on a rule and not on this release: a
	// third generator added to that list would be published to a repository no
	// literal here has ever been compared against, and the drift guard whose other
	// end is another module's test would silently stop covering the family. Which
	// generators exist is deliberately outside docs/container/SPEC.md's contract,
	// so nothing else fails when the set changes — this is what makes the change
	// arrive as a decision rather than as a release.
	var names []string
	for _, g := range publishedGenerators() {
		names = append(names, g.name)
	}

	if want := []string{"go", "graph"}; !slices.Equal(names, want) {
		errs = append(errs, fmt.Errorf("a release publishes a generator image for %v, want %v: add the new one to "+
			"the rows above, to the release notes block and to docs/container/SPEC.md's table", names, want))
	}

	// What the binaries a release publishes say their version is, which is the
	// third thing a version decides (#181). A release states one version and it
	// reaches three places: the tags the image family is published under, the
	// repositories those tags live in, and the line each executable answers with.
	//
	// The literals matter here for the reason they matter above. This rule is
	// written down three times — cmd/cpybkc/version.go and
	// cmd/cpybkc-gen-go/version.go carry the other two spellings, in packages
	// this module cannot import and which deliberately do not import each other —
	// and each of those has its own test pinning the same answers. The image
	// contract is what compares them on a real binary; this is what says, in the
	// place the version is derived, what the answer was supposed to be.
	for _, c := range []struct{ version, want string }{
		{"v0.2.0", "0.2.0"},
		// A prerelease, because that is the shape a release candidate's tag takes
		// and the one where a naive "everything after the first dot" would differ.
		{"v0.3.0-rc.1", "0.3.0-rc.1"},
		// The version everything that is not a release is built under. It has to
		// come out as the `0.0.0-dev` docs/cli/SPEC.md requires of a build made
		// outside a release, which is what lets this check run on a pull request.
		{devVersion, "0.0.0-dev"},
	} {
		if got := reportedVersion(c.version); got != c.want {
			errs = append(errs, fmt.Errorf("a build made under %q reports version %q, want %q",
				c.version, got, c.want))
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
//     the fact a reader cannot recover from the tag — and the version it
//     publishes, and it says of a prerelease that it moves none of the tags a
//     derived Dockerfile pins.
//   - Splicing is idempotent. A release job re-run appends nothing: splicing into
//     a body that already carries a block replaces that block, leaving everything
//     around it alone, and a second splice is a fixed point.
//
// What it does not check is the number itself against a constant. The IR version
// is read out of the built CLI at release time precisely so that there is one
// statement of it, and a literal here would be the second.
//
// It does not enumerate the tag family either, and neither does the block. Which
// tags a version implies is the archetype's rule since #185; a block listing
// them would be this repository restating it, and a check over that list would
// go green while the real family moved.
//
// +check
// +cache="session"
func (m *Cpybkc) ReleaseNotesContract() error {
	var errs []error

	const irVersion = 7

	block := releaseNotesBlock("v0.2.0", irVersion, "ghcr.io/zaba505/cpybkc")
	for _, want := range []string{
		"IR version 7",
		"`v0.2.0`",
		"ghcr.io/zaba505/cpybkc:v0.2.0",
		// The generator images, named beside the base they are published with
		// (#180, #230). Literals rather than generatorRepository's answers, like
		// every other expectation here: a check that derived them the way the
		// block does would go green on a block that had stopped naming a
		// generator at all.
		"ghcr.io/zaba505/cpybkc-gen-go:v0.2.0",
		"ghcr.io/zaba505/cpybkc-gen-graph:v0.2.0",
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
	// It used to be checked by looking for `latest` in the prerelease's block,
	// which was one half of a two-sided assertion while the stable block listed
	// the tag family. The block names no tag but the version now, so that half
	// became unfalsifiable for every input — a check that cannot fail, sitting
	// under a comment describing it as one that could. What replaced it is the
	// property that is still checkable and still the one a reader depends on: the
	// two blocks say *different* things about the moving tags, so a block that
	// stopped distinguishing a prerelease from a release fails here whichever way
	// round it collapsed.
	//
	// Two independent assertions and not a chain, for the reason the pair below
	// are: a block that is wrong twice should say so twice, and a chain would
	// report one and hide the other.
	pre := releaseNotesBlock("v0.3.0-rc.1", irVersion, "")
	if !strings.Contains(pre, "prerelease") {
		errs = append(errs, fmt.Errorf("a prerelease's notes do not say so:\n%s", pre))
	}

	if movingTagsSentence(pre) == movingTagsSentence(block) {
		errs = append(errs, fmt.Errorf("a prerelease and a release say the same thing about the tags a derived "+
			"Dockerfile pins:\n%s", pre))
	}

	// And the stable release must not call itself one, which is the half a block
	// that read its own tag list rather than its version could never have caught.
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
		twice, err := spliceNotes(once, releaseNotesBlock("v0.3.0-rc.1", irVersion, ""))
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

// releasePlan reads the refs at HEAD and decides which version this release
// publishes, returning the refs alongside it so that a run publishing nothing
// can say what it saw. An empty version is "this is not a release of the image".
func (m *Cpybkc) releasePlan(ctx context.Context, tag string) (string, []string, error) {
	// Refused rather than dereferenced, for the reason appSource returns the
	// source unchanged on the same input: Container.WithMountedDirectory asserts
	// its argument is non-nil and *panics*, a long way from whoever left it out.
	// It is unreachable from the command line, because New's argument carries a
	// default path, and reachable from a module-to-module call or a struct
	// literal — and unlike the image path there is nothing to fall back to, since
	// the whole question this answers is what the refs at HEAD say.
	if m.GitDir == nil {
		return "", nil, errors.New("this module was constructed with no git directory, and whether a commit is a " +
			"release is read out of the refs at HEAD: pass gitDir to the constructor")
	}

	refs, err := headRefs(ctx, m.Source, m.GitDir)
	if err != nil {
		return "", nil, err
	}

	version, err := planRelease(refs, tag)
	if err != nil {
		return "", refs, err
	}

	return version, refs, nil
}

// releaseName is how a run with nothing to publish refers to what it was asked
// about, so that the report reads the same whether a tag was given or not.
func releaseName(tag string) string {
	if tag == "" {
		return "HEAD"
	}

	return tag
}

// planRelease returns the version to publish, from the tag the release carries
// and the refs at HEAD. An empty version is "this is not a release of the
// image".
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
//
// And a version carrying `+build` metadata. An OCI tag is `[A-Za-z0-9_.-]`,
// which has no `+` in it, so dropping the metadata would publish `v0.2.0` and
// `v0.2.0+build.5` under one tag. The archetype refuses it too; refusing here as
// well costs one branch and means the refusal arrives before a credential has
// been used, in this repository's own words.
func planRelease(refs []string, tag string) (string, error) {
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
		return "", fmt.Errorf("HEAD carries more than one version tag (%s): which release this is, and "+
			"which of them the moving tags should follow, is not a question this pipeline can answer",
			strings.Join(versions, ", "))
	}

	var version string

	if tag == "" {
		// No release object to name, so the refs at HEAD are the only thing that
		// can say what this commit is.
		if len(versions) == 1 {
			version = versions[0]
		}
	} else {
		// The release that triggered this run is a release of something else —
		// the IR module, most often. Publishing nothing is the answer rather than
		// a fault, which is what lets the workflow fire on every release without
		// a filter naming tag shapes in YAML.
		if _, ok := parseVersion(tag); !ok {
			return "", nil
		}

		if !slices.Contains(versions, tag) {
			return "", fmt.Errorf("the release's tag %q does not point at HEAD (refs: %s): the image is "+
				"built from this checkout, so publishing it under that tag would name bytes the tag was never cut from",
				tag, strings.Join(refs, ", "))
		}

		version = tag
	}

	if version == "" {
		return "", nil
	}

	if v, _ := parseVersion(version); v.build != "" {
		return "", fmt.Errorf("version tag %q carries build metadata, which cannot be spelled in an OCI tag: "+
			"release it under a version without one", version)
	}

	return version, nil
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
// It is a pure function of the version, the IR version and where the image was
// published, so ReleaseNotesContract can read it back without a registry, a git
// checkout or a release.
//
// It names **every** image a release publishes (#180, #230), and each
// generator's reference is derived from the base's rather than passed in — one
// argument for the whole family, so a mirror's release notes cannot name the
// mirror for one and ghcr.io for another. That the generators exist at all is
// the fact this half of the block is for: `v0.0.0` published the base alone, and
// the one documented way to get a generator was a reference that answered
// NAME_UNKNOWN.
//
// The pull lines come from [publishedGenerators] so a generator added there is
// one this block names. The prose beside them does not, and that is deliberate:
// it says how many images there are and what they are for, which is a sentence
// somebody has to rewrite rather than a list to extend — and TagScheme is what
// fails when the set changes without anybody having done so.
//
// It names the version and no other tag. Which tags a version implies is the
// shared pipeline's rule since #185, and a block enumerating them would be this
// repository restating somebody else's table in the one artifact a reader cannot
// check against anything — so it points at docs/container/SPEC.md's table
// instead, which is where that rule is written down and where the note saying
// who enforces it lives.
//
// What it does still say for itself is whether this is a prerelease, because
// that is a property of the version rather than of the family: a reader who sees
// a release published and assumes the moving tags followed it is exactly who
// that sentence is for.
//
// reference empty leaves the pull reference out rather than guessing one: where
// the image lives is a property of the deployment, and a block naming `ghcr.io`
// on a mirror's release would be telling a reader to pull from somewhere this
// release did not go.
func releaseNotesBlock(version string, irVersion int, reference string) string {
	var b strings.Builder

	b.WriteString(notesOpen)
	b.WriteString("\n## The images this release publishes\n\n")

	if reference != "" {
		b.WriteString("```console\n")
		fmt.Fprintf(&b, "$ docker pull %s:%s\n", reference, version)

		for _, g := range publishedGenerators() {
			fmt.Fprintf(&b, "$ docker pull %s:%s\n", generatorRepository(reference, g.name), version)
		}

		b.WriteString("```\n\n")
	}

	fmt.Fprintf(&b, "Three images: the cpybkc CLI, and `%s` and `%s`, this project's own generators. A\n"+
		"generator reaches you the way a stranger's does — an image to `COPY --from`, never something the\n"+
		"base image carries — and both are published from this same release, over the same platforms and\n"+
		"under the same tags.\n\n", generatorExecutable, graphGeneratorExecutable)

	fmt.Fprintf(&b, "**This image speaks IR version %d.** Every descriptor the `cpybkc` in it writes carries that\n"+
		"version, and a generator that implements a lower one refuses the descriptor rather than guessing — so a\n"+
		"plugin used beside this image has to implement IR version %d or higher.\n\n", irVersion, irVersion)

	v, _ := parseVersion(version)

	if v.prerelease != "" {
		fmt.Fprintf(&b, "This is a **prerelease**. It publishes `%s` and moves none of the tags a derived Dockerfile\n"+
			"pins to pick up fixes: a release candidate is not a fix anybody consented to be given.\n\n", version)
	} else {
		fmt.Fprintf(&b, "It is a multi-platform index over %s, published under `%s` and under the moving tags that\n"+
			"follow a release. Only the version tag never moves.\n\n",
			strings.Join(platformNames(), " and "), version)
	}

	b.WriteString("Which tags a release publishes, what pinning each of them buys, and how to verify the signature\n" +
		"and the attestations on the digest they resolve to, is in " +
		"[the base-image contract](docs/container/SPEC.md).\n")
	b.WriteString(notesClose)

	return b.String()
}

// movingTagsSentence is the part of a block that talks about the tags a derived
// Dockerfile pins, reduced to something two blocks can be compared on.
//
// The whole line rather than a keyword, because what is being checked is that a
// prerelease and a release say *different* things about the moving tags, and a
// keyword search is how two different sentences come to look identical.
func movingTagsSentence(block string) string {
	for _, line := range strings.Split(block, "\n") {
		if strings.Contains(line, "moving tags") || strings.Contains(line, "moves none") {
			return strings.TrimSpace(line)
		}
	}

	return ""
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

// gitContainer is a container holding the source with its git metadata grafted
// back on.
//
// New drops .git from the source it binds, because none of the check stages
// reads git metadata and leaving it in would make every commit a cache miss for
// all of them. The release decision does read it, so it is mounted rather than
// folded into the source — which keeps a release's need from costing every other
// stage its cache. appSource is the other half of the same argument, for the
// path that builds an image.
func gitContainer(source, gitDir *dagger.Directory) *dagger.Container {
	return dag.Go().Container(source).WithMountedDirectory("/src/.git", gitDir)
}
