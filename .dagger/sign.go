// This file signs what a release publishes and attaches the two attestations a
// consumer can check without any prior arrangement with this project: a SLSA v1
// provenance statement, and an SPDX SBOM per executable per platform (#58).
//
// # What is signed, and what is attested
//
// docs/container/SPEC.md's "What a tag carries besides the image" is the promise
// these functions keep, and it is precise about two things that are easy to get
// wrong and impossible for a consumer to notice afterwards.
//
// The **signature is recursive**. A published tag resolves to a multi-platform
// index, and signing the index alone leaves the per-platform manifest a
// consumer's runtime actually pulls unsigned — while `cosign verify` against the
// tag still passes, because it verified the index the tag named. That is a gap
// worth closing rather than documenting, so `cosign sign --recursive` covers the
// index and every manifest beneath it.
//
// The **attestations are not**. Provenance and SBOMs go on the published index
// digest and on nothing else. They are statements about the release, and the
// release is the index; a per-platform manifest carrying its own copy would be
// three answers to one question, and a consumer who found one on a manifest
// would have no way to tell which was authoritative. That document says so
// outright, so that nobody goes looking for a provenance statement on the
// manifest their runtime happened to pull.
//
// Everything names a **digest**, never a tag. Three of this project's four
// published tags move by design, and an attestation about a name that moves says
// nothing. Attest refuses a reference that is not digest-qualified rather than
// resolving one itself: the digest that gets signed has to be the digest that
// was pushed, not whatever the tag has come to mean by the time cosign looks.
//
// # Why cosign, and why keyless
//
// The promise is only worth what the verifying command is worth. cosign's
// keyless flow is the one a consumer already has: `cosign verify` with a
// certificate identity and issuer is a command somebody can run without this
// project having published a key, rotated it, or asked anybody to trust a key
// distribution channel. The signing identity is the release workflow itself,
// certified for the length of one run by the public sigstore CA from the OIDC
// token the CI provider mints, and recorded in a public transparency log — so
// verification asks *what built this image* rather than *who holds a secret*.
//
// cosign is built by `go install` at a pinned module version rather than pulled
// as a tool image. The toolchain is the one dag.Go() already provides, and the
// pin is a module version rather than a tag somebody can repoint — which matters
// more here than for buf, since this is the binary that speaks for the project's
// identity. It is the shape avroc arrived at for the same reason (avroc#168).
//
// # Why a pull request cannot check the signature, and what it checks instead
//
// Producing a real signature needs three things a pull request does not have: an
// OIDC token from the CI provider, a registry to push to, and an entry in a
// public transparency log. A check that faked any of them would be checking the
// fake. So Attestations checks the half that is a function of this repository
// rather than of the release environment — that the provenance predicate is
// well-formed SLSA v1 saying what it claims to say, and that the SBOM set is one
// valid document per executable per platform tied to that platform's own binary.
//
// What is left unchecked until a release runs is the signing itself, and that is
// stated rather than papered over: #59 is what first calls Attest with something
// published, and the first release is where a wrong flag would show up.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"dagger/cpybkc/internal/dagger"
)

const (
	// cosignPackage is the signing tool, pinned to a module version. `go install`
	// refuses an unpinned package, which is the property that makes the binary
	// this release signs with the binary the pin names.
	//
	// renovate: datasource=go depName=github.com/sigstore/cosign/v2
	cosignPackage = "github.com/sigstore/cosign/v2/cmd/cosign@v2.6.5"

	// cosignPath is where the built binary lands in the signing container.
	cosignPath = "/usr/local/bin/cosign"

	// buildType names this pipeline in the provenance predicate. SLSA requires a
	// URI, and it points at the file that implements it rather than at a
	// specification somebody would have to be told about separately: what "built
	// by cpybkc's release" means is what is written here.
	buildType = "https://github.com/Zaba505/cpybkc/blob/main/.dagger/sign.go"

	// provenanceType and sbomType are cosign's names for the two attestation
	// shapes, and they are what a consumer passes to `cosign verify-attestation
	// --type`. slsaprovenance1 is SLSA v1, the version renderProvenance emits;
	// spdxjson is the SPDX 2.3 JSON the shared Go module produces.
	provenanceType = "slsaprovenance1"
	sbomType       = "spdxjson"

	// attestationDir is where predicates are staged inside the signing
	// container. Nothing reads them afterwards; they exist because cosign takes
	// a predicate as a path.
	attestationDir = "/attestations"

	// digestPrefix is what makes a reference digest-qualified. Only sha256 is
	// accepted, which is the algorithm every registry in use writes; a reference
	// naming another one is refused rather than passed through, because a digest
	// this pipeline cannot recognise is one it cannot promise it signed.
	digestPrefix = "sha256:"
)

// Sbom renders the SPDX 2.3 documents a release attaches — one per executable
// per platform:
//
//	dagger call sbom export --path=sbom
//	dagger call sbom --platform=linux/arm64 export --path=sbom
//
// One per executable per platform rather than one per image, because each
// document is tied to the SHA-256 of the binary it describes. A single document
// for an image published on two platforms would name one of those binaries and
// be wrong about the other, and a consumer matching an advisory against it would
// be matching against the wrong build.
//
// The documents describe the very binaries the published image ships: they are
// rendered from image.go's build rather than from a rebuild of the same source,
// so a change to how the executable is compiled moves the SBOM with it.
//
// platform restricts the set to one of the published platforms; empty renders
// every one of them, which is what a release attaches.
func (m *Cpybkc) Sbom(
	ctx context.Context,
	// Render for this platform alone, as `GOOS/GOARCH` — one of the published
	// platforms. Empty covers all of them.
	// +optional
	platform string,
) (*dagger.Directory, error) {
	platforms, err := releasePlatforms(platform)
	if err != nil {
		return nil, err
	}

	dir := dag.Directory()
	for _, p := range platforms {
		documents, err := m.sbomDocuments(p)
		if err != nil {
			return nil, err
		}

		for name, doc := range documents {
			dir = dir.WithFile(name, doc)
		}
	}

	return dir, nil
}

// Provenance renders the SLSA v1 provenance predicate a release attaches to one
// published image:
//
//	dagger call provenance --image=ghcr.io/zaba505/cpybkc@sha256:… \
//	  --builder=https://github.com/Zaba505/cpybkc/.github/workflows/release.yaml@refs/tags/v0.2.0
//
// It is the predicate alone rather than a whole in-toto statement, because
// cosign builds the statement and fills in the subject from the digest it is
// signing. That one field is the one that must not be taken on this side's word.
//
// What it claims is deliberately narrow. Every field is read out of the
// repository or handed in by the run — the commit, the origin, the workflow —
// and nothing is asserted about hermeticity or reproducibility that this
// pipeline does not check. A predicate claiming more than it knew would be worse
// than none, because a consumer would act on it.
func (m *Cpybkc) Provenance(
	ctx context.Context,
	// The repository's git metadata. The commit and the origin the predicate
	// names are read from it, so they describe what was actually built rather
	// than what somebody meant to build.
	// +defaultPath="/.git"
	gitDir *dagger.Directory,
	// The published image, digest-qualified — `ghcr.io/zaba505/cpybkc@sha256:…`.
	// A tag is refused: an attestation about a name that moves says nothing.
	image string,
	// What ran this release, for the predicate's builder — the workflow
	// reference on GitHub Actions. This module cannot know what invoked it, and
	// provenance that guessed would be provenance about nothing.
	builder string,
	// The release version the digest was published as, if there is one.
	// +optional
	version string,
	// The tags now pointing at the digest, if any.
	// +optional
	tags []string,
	// The run this release came from — a URL to the workflow run on GitHub
	// Actions.
	// +optional
	invocation string,
) (*dagger.File, error) {
	facts, err := m.provenanceFacts(ctx, gitDir, image, builder, version, tags, invocation, time.Now().UTC())
	if err != nil {
		return nil, err
	}

	return provenanceFile(facts)
}

// Attest signs a published image digest with cosign and attaches its provenance
// and SBOMs (#58).
//
// It is the seam #59's release publishing comes through: that story resolves
// which tags a release carries and pushes them, and hands the digest they all
// point at to this function. Nothing here decides what to publish or where —
// where to publish is a property of the deployment, and a mirror or an internal
// registry serves the same release.
//
// The signature comes first and the attestations follow, deliberately. The
// signature is the claim the other two are read under, and a digest carrying
// attestations nobody signed is worse than one carrying none, because from a
// distance it looks verified.
//
// It returns a report naming what was signed and what was attached.
//
// +cache="never"
func (m *Cpybkc) Attest(
	ctx context.Context,
	// The repository's git metadata, for the provenance predicate's commit and
	// origin.
	// +defaultPath="/.git"
	gitDir *dagger.Directory,
	// The published image, digest-qualified — `ghcr.io/zaba505/cpybkc@sha256:…`.
	image string,
	// The registry username to authenticate as.
	username string,
	// The registry password or token to authenticate with.
	password *dagger.Secret,
	// The CI provider's OIDC token request endpoint —
	// `ACTIONS_ID_TOKEN_REQUEST_URL` on GitHub Actions. cosign exchanges a token
	// from it for the short-lived certificate that signs.
	idTokenRequestUrl string,
	// The bearer token for that endpoint — `ACTIONS_ID_TOKEN_REQUEST_TOKEN` on
	// GitHub Actions. A secret, because it mints identity tokens.
	idTokenRequestToken *dagger.Secret,
	// What ran this release, for the provenance predicate's builder — the
	// workflow reference on GitHub Actions.
	builder string,
	// The release version the digest was published as, if there is one.
	// +optional
	version string,
	// The tags now pointing at the digest, if any.
	// +optional
	tags []string,
	// The run this release came from — a URL to the workflow run on GitHub
	// Actions.
	// +optional
	invocation string,
) (string, error) {
	repository, digest, err := splitDigest(image)
	if err != nil {
		return "", err
	}

	// Every credential is checked before the first byte moves. A signature that
	// found halfway through that it had no way to authenticate would leave a
	// published digest partly attested, and this project's contract says a
	// published version tag is never repointed to correct it.
	switch {
	case username == "" || password == nil:
		return "", errors.New("username and password are both required: signatures and attestations are pushed to the registry holding the image")
	case idTokenRequestUrl == "" || idTokenRequestToken == nil:
		return "", errors.New("idTokenRequestUrl and idTokenRequestToken are both required: signing is keyless, and it exchanges a workload identity token for the certificate that signs")
	}

	startedOn := time.Now().UTC()

	facts, err := m.provenanceFacts(ctx, gitDir, image, builder, version, tags, invocation, startedOn)
	if err != nil {
		return "", err
	}

	predicate, err := provenanceFile(facts)
	if err != nil {
		return "", err
	}

	signer := m.cosign(idTokenRequestUrl, idTokenRequestToken, registryHost(repository), username, password, startedOn)

	// --recursive because what is published is an index over every platform, and
	// signing the index alone leaves the manifest a consumer's runtime actually
	// pulls unsigned while `cosign verify` against the tag still passes.
	const predicateAt = attestationDir + "/provenance.json"
	c := signer.
		WithExec([]string{"cosign", "sign", "--recursive", image}).
		WithFile(predicateAt, predicate).
		WithExec([]string{"cosign", "attest", "--type", provenanceType, "--predicate", predicateAt, image})

	var attached []string
	for _, p := range imagePlatforms() {
		documents, err := m.sbomDocuments(p)
		if err != nil {
			return "", err
		}

		for _, name := range slices.Sorted(maps.Keys(documents)) {
			at := attestationDir + "/" + name
			c = c.
				WithFile(at, documents[name]).
				WithExec([]string{"cosign", "attest", "--type", sbomType, "--predicate", at, image})
			attached = append(attached, name)
		}
	}

	if _, err := c.Sync(ctx); err != nil {
		return "", fmt.Errorf("signing and attesting %s: %w", image, err)
	}

	var report strings.Builder
	fmt.Fprintf(&report, "%s\n  digest:     %s\n", repository, digest)
	fmt.Fprintf(&report, "  signature:  the index and each per-platform manifest under it\n")
	fmt.Fprintf(&report, "  provenance: %s, from %s at %s\n", provenanceType, facts.Source, facts.Commit)
	fmt.Fprintf(&report, "  sboms:      %s (%s)\n", sbomType, strings.Join(attached, ", "))

	return report.String(), nil
}

// Attestations checks what a release attaches, as far as it can be checked
// without a registry, an OIDC issuer and a transparency log (#58).
//
// Two halves, both functions of this repository rather than of the release
// environment:
//
//   - The provenance predicate, over a table of facts. It has to be well-formed
//     SLSA v1 and to say what docs/container/SPEC.md claims a consumer will find
//     in it, and it has to refuse the inputs that would make it a lie — a tag
//     rather than a digest, and a release nobody has named a builder for.
//   - The SBOM set: one document per executable per platform, each valid SPDX
//     2.3 naming that executable and carrying that platform's own binary
//     checksum. The two platforms' documents are required to *differ*, which is
//     the machine-checkable form of "per platform" — a single document attached
//     twice would satisfy every other assertion here.
//
// What it does not check is the signature, because producing one needs an OIDC
// token, a registry and a public log, and a check that faked any of the three
// would be checking the fake. See this file's comment.
//
// +check
// +cache="session"
func (m *Cpybkc) Attestations(ctx context.Context) error {
	return errors.Join(m.checkProvenance(), m.checkSboms(ctx))
}

// checkProvenance renders the predicate over a table of facts and reads back
// every field docs/container/SPEC.md tells a consumer they will find.
//
// The expected values below are literals rather than this file's own constants.
// A table written in terms of buildType would move with it and pass on a
// predicate that had quietly started claiming to have been built by something
// else, which is the one failure a table like this exists to catch.
func (m *Cpybkc) checkProvenance() error {
	facts := provenanceFacts{
		Repository: "ghcr.io/zaba505/cpybkc",
		Digest:     "sha256:" + strings.Repeat("ab", 32),
		Version:    "v0.2.0",
		Tags:       []string{"v0.2.0", "v0.2", "v0", "latest"},
		Source:     "https://github.com/Zaba505/cpybkc",
		Commit:     strings.Repeat("cd", 20),
		Builder:    "https://github.com/Zaba505/cpybkc/.github/workflows/release.yaml@refs/tags/v0.2.0",
		Invocation: "https://github.com/Zaba505/cpybkc/actions/runs/1",
		StartedOn:  time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC),
	}

	body, err := renderProvenance(facts)
	if err != nil {
		return err
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		return fmt.Errorf("the provenance predicate is not valid JSON: %w", err)
	}

	var errs []error
	for path, want := range map[string]any{
		"buildDefinition.buildType":                               "https://github.com/Zaba505/cpybkc/blob/main/.dagger/sign.go",
		"buildDefinition.externalParameters.repository":           "ghcr.io/zaba505/cpybkc",
		"buildDefinition.externalParameters.version":              "v0.2.0",
		"buildDefinition.resolvedDependencies.0.uri":              "git+https://github.com/Zaba505/cpybkc",
		"buildDefinition.resolvedDependencies.0.digest.gitCommit": strings.Repeat("cd", 20),
		"runDetails.builder.id":                                   "https://github.com/Zaba505/cpybkc/.github/workflows/release.yaml@refs/tags/v0.2.0",
		"runDetails.metadata.invocationId":                        "https://github.com/Zaba505/cpybkc/actions/runs/1",
		"runDetails.metadata.startedOn":                           "2026-02-03T04:05:06Z",
	} {
		if err := hasField(got, path, want); err != nil {
			errs = append(errs, err)
		}
	}

	// The two lists, which carry the facts a consumer reads the tags off. They
	// are compared as slices rather than by hasField because JSON decodes them
	// into []any and a string comparison would pass on a list of one.
	if err := hasStrings(got, "buildDefinition.externalParameters.tags", []string{"v0.2.0", "v0.2", "v0", "latest"}); err != nil {
		errs = append(errs, err)
	}

	platforms := make([]string, 0, len(imagePlatforms()))
	for _, p := range imagePlatforms() {
		platforms = append(platforms, string(p))
	}
	if err := hasStrings(got, "buildDefinition.externalParameters.platforms", platforms); err != nil {
		errs = append(errs, err)
	}

	// A release that named no version and no tags still produces a predicate,
	// and the keys it has nothing to say about are absent rather than empty. A
	// predicate asserting `"version": ""` is a claim about a release nobody made.
	bare := facts
	bare.Version = ""
	bare.Tags = nil

	body, err = renderProvenance(bare)
	if err != nil {
		errs = append(errs, err)
	} else {
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			errs = append(errs, fmt.Errorf("the provenance predicate is not valid JSON: %w", err))
		}
		for _, path := range []string{
			"buildDefinition.externalParameters.version",
			"buildDefinition.externalParameters.tags",
		} {
			if _, ok := field(got, path); ok {
				errs = append(errs, fmt.Errorf("%s is present on a release that named none: an empty claim is still a claim", path))
			}
		}
	}

	// The refusals. Each of these is a way of producing a predicate that reads
	// perfectly and means nothing, and each has to be an error rather than a
	// default.
	for _, refusal := range []struct {
		why   string
		facts provenanceFacts
	}{
		{"a reference with no digest", func() provenanceFacts { f := facts; f.Digest = ""; return f }()},
		{"no builder", func() provenanceFacts { f := facts; f.Builder = ""; return f }()},
		{"no source commit", func() provenanceFacts { f := facts; f.Commit = ""; return f }()},
		{"no source repository", func() provenanceFacts { f := facts; f.Source = ""; return f }()},
	} {
		if _, err := renderProvenance(refusal.facts); err == nil {
			errs = append(errs, fmt.Errorf("a predicate was rendered for %s, which should be an error", refusal.why))
		}
	}

	// And the reference itself, which is where a tag would get in — and, below
	// it, where a digest that is the right shape and the wrong bytes would.
	// Every one of these reaches cosign if it is let through, and is reported
	// there against a registry round trip: an error naming the wrong layer of
	// the problem, at the one moment in a release where that costs most.
	for _, image := range []string{
		"ghcr.io/zaba505/cpybkc:v0.2.0",
		"ghcr.io/zaba505/cpybkc",
		"ghcr.io/zaba505/cpybkc@md5:" + strings.Repeat("ab", 16),
		"ghcr.io/zaba505/cpybkc@sha256:",
		"ghcr.io/zaba505/cpybkc@sha256:" + strings.Repeat("zz", 32),
		"ghcr.io/zaba505/cpybkc@sha256:" + strings.Repeat("AB", 32),
		"@sha256:" + strings.Repeat("ab", 32),
	} {
		if _, _, err := splitDigest(image); err == nil {
			errs = append(errs, fmt.Errorf("%q was accepted as a digest-qualified reference: what a release signs has to be what it published", image))
		}
	}

	// The one that has to be accepted, taken apart the way the predicate and the
	// report both read it. A refusal table with no acceptance beside it passes
	// just as well when nothing is accepted at all.
	repository, digest, err := splitDigest(facts.Repository + "@" + facts.Digest)
	switch {
	case err != nil:
		errs = append(errs, fmt.Errorf("a well-formed digest-qualified reference was refused: %w", err))
	case repository != facts.Repository || digest != facts.Digest:
		errs = append(errs, fmt.Errorf("a digest-qualified reference split into %q and %q, want %q and %q", repository, digest, facts.Repository, facts.Digest))
	}

	return errors.Join(errs...)
}

// checkSboms builds the documents a release attaches and reads each one back.
//
// It renders them through Sbom rather than through a recipe of its own, so what
// is checked here is what a release attaches and not a second set of documents
// that agree with them today.
func (m *Cpybkc) checkSboms(ctx context.Context) error {
	dir, err := m.Sbom(ctx, "")
	if err != nil {
		return err
	}

	names, err := dir.Entries(ctx)
	if err != nil {
		return fmt.Errorf("listing the SBOM set: %w", err)
	}

	var want []string
	for _, p := range imagePlatforms() {
		for _, executable := range baseImageExecutables() {
			want = append(want, sbomName(executable, p))
		}
	}
	slices.Sort(want)
	slices.Sort(names)

	if !slices.Equal(names, want) {
		return fmt.Errorf("the SBOM set is %v, want %v: a release attaches one document per executable per platform", names, want)
	}

	var errs []error
	checksums := map[string]string{}

	for _, name := range names {
		body, err := dir.File(name).Contents(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("reading %s: %w", name, err))
			continue
		}

		var doc struct {
			SPDXVersion string `json:"spdxVersion"`
			Name        string `json:"name"`
			Packages    []struct {
				SPDXID    string `json:"SPDXID"`
				Checksums []struct {
					Algorithm     string `json:"algorithm"`
					ChecksumValue string `json:"checksumValue"`
				} `json:"checksums"`
			} `json:"packages"`
		}
		if err := json.Unmarshal([]byte(body), &doc); err != nil {
			errs = append(errs, fmt.Errorf("%s is not valid JSON: %w", name, err))
			continue
		}

		// SPDX 2.3, which is the revision the tooling a consumer already runs
		// ingests. A document nothing can parse is not an SBOM.
		if doc.SPDXVersion != "SPDX-2.3" {
			errs = append(errs, fmt.Errorf("%s declares %q, want SPDX-2.3", name, doc.SPDXVersion))
		}

		executable, _, _ := strings.Cut(name, "-")
		if doc.Name != executable {
			errs = append(errs, fmt.Errorf("%s describes %q, want %q: the document a release attaches names the executable it is about", name, doc.Name, executable))
		}

		// The subject package's checksum is what ties the document to a build
		// rather than merely placing it beside one.
		var sum string
		for _, pkg := range doc.Packages {
			if pkg.SPDXID != "SPDXRef-Binary" {
				continue
			}
			for _, checksum := range pkg.Checksums {
				if checksum.Algorithm == "SHA256" {
					sum = checksum.ChecksumValue
				}
			}
		}
		if sum == "" {
			errs = append(errs, fmt.Errorf("%s carries no SHA256 checksum for its subject: a document that does not name the bytes it describes cannot be verified against them", name))
			continue
		}

		if other, ok := checksums[sum]; ok {
			errs = append(errs, fmt.Errorf("%s and %s describe the same bytes: one document per executable per platform means each names its own binary", other, name))
		}
		checksums[sum] = name
	}

	return errors.Join(errs...)
}

// sbomDocuments is the SPDX documents for one platform, keyed by the name they
// are attached under.
func (m *Cpybkc) sbomDocuments(platform dagger.Platform) (map[string]*dagger.File, error) {
	binaries, err := m.releaseBinaries(platform)
	if err != nil {
		return nil, err
	}

	documents := map[string]*dagger.File{}
	for name, binary := range binaries {
		documents[sbomName(name, platform)] = dag.Go().Spdx(binary, m.Source)
	}

	return documents, nil
}

// releaseBinaries is every executable a published image ships, for one platform,
// resolved to the build that produces it.
//
// The list is baseImageExecutables' — the same one ImageContract checks the
// filesystem against — so an SBOM set cannot describe a different set of
// executables from the one in the image. An executable added there without a
// build named here is an error rather than a document quietly not attached: a
// release attaching an incomplete SBOM set looks exactly like one attaching a
// complete one.
func (m *Cpybkc) releaseBinaries(platform dagger.Platform) (map[string]*dagger.File, error) {
	binaries := map[string]*dagger.File{}

	for _, name := range baseImageExecutables() {
		switch name {
		case cliBinary:
			binaries[name] = m.binary(platform)
		default:
			return nil, fmt.Errorf("no build is named for %q, which the base image ships: an executable added to baseImageExecutables needs one here too, or a release would attach an SBOM set that does not describe the image", name)
		}
	}

	return binaries, nil
}

// sbomName is what one document is attached and exported as.
//
// The platform is in the name because the set is one document per executable per
// platform and they are attached to a single digest, so two documents for one
// executable would otherwise collide. The separator is `-` rather than `/`
// because these are file names in a flat directory.
func sbomName(executable string, platform dagger.Platform) string {
	return executable + "-" + strings.ReplaceAll(string(platform), "/", "-") + ".spdx.json"
}

// releasePlatforms resolves a platform argument against the published set, the
// way ImageContract does: empty is every one of them, and a value that is not
// published is an error rather than a build nobody ships.
func releasePlatforms(platform string) ([]dagger.Platform, error) {
	platforms := imagePlatforms()
	if platform == "" {
		return platforms, nil
	}

	if !slices.Contains(platforms, dagger.Platform(platform)) {
		return nil, fmt.Errorf("platform %q is not one this repository publishes: %v", platform, platforms)
	}

	return []dagger.Platform{dagger.Platform(platform)}, nil
}

// cosign is the container every signature and attestation is produced in.
//
// It is a container that has certificate authorities and a shell — the signing
// flow talks to a public CA, a transparency log and the registry — and which is
// emphatically not the published image, since that one deliberately has none of
// those things.
//
// COSIGN_YES is what makes the flow non-interactive: without it cosign asks for
// confirmation before writing to the public transparency log, and a release
// would hang waiting for somebody who is not there.
func (m *Cpybkc) cosign(
	idTokenRequestUrl string,
	idTokenRequestToken *dagger.Secret,
	host, username string,
	password *dagger.Secret,
	startedOn time.Time,
) *dagger.Container {
	return dag.Go().
		Container(dag.Directory()).
		WithFile(cosignPath, dag.Go().Install(cosignPackage), dagger.ContainerWithFileOpts{
			Permissions: executableMode,
		}).
		// The instant this release started, in the environment so that every
		// signing command below is a different command on every run. Signing is
		// side-effecting — it writes to a registry and to a public transparency
		// log — and an exec whose arguments match a previous one is one Dagger is
		// entitled to skip. A release that silently skipped its own signatures
		// would report success having signed nothing.
		WithEnvVariable("CPYBKC_ATTEST_STARTED_ON", startedOn.Format(time.RFC3339Nano)).
		WithEnvVariable("COSIGN_YES", "true").
		// The two halves of the workload identity exchange, in the environment
		// cosign's GitHub Actions provider reads them from. The token is a secret
		// variable rather than a plain one because it mints identity tokens for
		// this repository.
		WithEnvVariable("ACTIONS_ID_TOKEN_REQUEST_URL", idTokenRequestUrl).
		WithSecretVariable("ACTIONS_ID_TOKEN_REQUEST_TOKEN", idTokenRequestToken).
		WithSecretVariable("REGISTRY_PASSWORD", password).
		// Through stdin rather than on the command line: an argument is visible
		// to anything that can list processes in the container, and a token that
		// can write packages is worth the extra line.
		WithExec([]string{
			"sh", "-c",
			`printf '%s' "$REGISTRY_PASSWORD" | cosign login "$1" --username "$2" --password-stdin`,
			"sh", host, username,
		})
}

// provenanceFacts is everything the predicate says, gathered before anything is
// rendered so that one release's statements agree about the commit, the origin
// and the run that produced them.
type provenanceFacts struct {
	Repository string
	Digest     string
	Version    string
	Tags       []string
	Source     string
	Commit     string
	Builder    string
	Invocation string
	StartedOn  time.Time
}

// provenanceFacts reads what the predicate needs out of the checkout and the
// arguments, and refuses anything it cannot state truthfully.
func (m *Cpybkc) provenanceFacts(
	ctx context.Context,
	gitDir *dagger.Directory,
	image, builder, version string,
	tags []string,
	invocation string,
	startedOn time.Time,
) (provenanceFacts, error) {
	repository, digest, err := splitDigest(image)
	if err != nil {
		return provenanceFacts{}, err
	}

	commit, err := headCommit(ctx, m.Source, gitDir)
	if err != nil {
		return provenanceFacts{}, err
	}

	source, err := sourceURI(ctx, m.Source, gitDir)
	if err != nil {
		return provenanceFacts{}, err
	}

	return provenanceFacts{
		Repository: repository,
		Digest:     digest,
		Version:    version,
		Tags:       tags,
		Source:     source,
		Commit:     commit,
		Builder:    builder,
		Invocation: invocation,
		StartedOn:  startedOn,
	}, nil
}

// provenanceFile renders the predicate into a file for cosign to read.
func provenanceFile(facts provenanceFacts) (*dagger.File, error) {
	body, err := renderProvenance(facts)
	if err != nil {
		return nil, err
	}

	const name = "provenance.json"

	return dag.Directory().WithNewFile(name, string(body)).File(name), nil
}

// renderProvenance renders the SLSA v1 provenance predicate for one published
// digest.
//
// Absent facts are absent keys rather than empty ones. A predicate asserting
// `"version": ""` claims a release that nobody made, and a consumer reading a
// key cannot tell a claim of nothing from an absence of one.
func renderProvenance(facts provenanceFacts) ([]byte, error) {
	switch {
	case facts.Repository == "" || facts.Digest == "":
		return nil, errors.New("the provenance predicate names a digest-qualified image, and this one has none")
	case facts.Builder == "":
		return nil, errors.New("builder is required: the predicate names what ran the release, and this module cannot know that")
	case facts.Source == "" || facts.Commit == "":
		return nil, errors.New("the source repository and the commit are both required: a provenance statement that cannot name what it was built from is not one")
	}

	platforms := make([]string, 0, len(imagePlatforms()))
	for _, p := range imagePlatforms() {
		platforms = append(platforms, string(p))
	}

	external := map[string]any{
		"repository": facts.Repository,
		"platforms":  platforms,
	}
	if facts.Version != "" {
		external["version"] = facts.Version
	}
	if len(facts.Tags) > 0 {
		external["tags"] = facts.Tags
	}

	metadata := map[string]any{"startedOn": facts.StartedOn.Format(time.RFC3339)}
	if facts.Invocation != "" {
		metadata["invocationId"] = facts.Invocation
	}

	predicate := map[string]any{
		"buildDefinition": map[string]any{
			"buildType":          buildType,
			"externalParameters": external,
			"internalParameters": map[string]any{},
			"resolvedDependencies": []any{
				map[string]any{
					"uri":    "git+" + facts.Source,
					"digest": map[string]any{"gitCommit": facts.Commit},
				},
			},
		},
		"runDetails": map[string]any{
			"builder":  map[string]any{"id": facts.Builder},
			"metadata": metadata,
		},
	}

	body, err := json.MarshalIndent(predicate, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("rendering the provenance predicate for %s: %w", facts.Repository, err)
	}

	return append(body, '\n'), nil
}

// splitDigest takes a digest-qualified reference apart, and refuses one that is
// not.
//
// A tag is the reference this has to refuse. Three of the four tags a release
// publishes move by design, so an attestation attached to one would be a
// statement about whatever that name comes to mean — which is no statement at
// all. Resolving a tag here rather than refusing it would be worse still: what
// gets signed has to be what was pushed, and a tag resolved a second time is a
// second answer.
func splitDigest(image string) (repository, digest string, err error) {
	repository, digest, ok := strings.Cut(image, "@")
	switch {
	case image == "":
		return "", "", errors.New("image is required: it is the published image, digest-qualified — ghcr.io/zaba505/cpybkc@sha256:…")
	case !ok:
		return "", "", fmt.Errorf("%q is not digest-qualified: sign the digest a publish returned, never a tag, because three of this project's four tags move by design", image)
	case repository == "":
		return "", "", fmt.Errorf("%q names a digest with no repository in front of it", image)
	case !strings.HasPrefix(digest, digestPrefix):
		return "", "", fmt.Errorf("%q is not a sha256 digest: a digest this pipeline cannot recognise is one it cannot promise it signed", digest)
	case len(digest) != len(digestPrefix)+64:
		return "", "", fmt.Errorf("%q is not a sha256 digest: 64 hex characters are expected after the prefix", digest)
	}

	// The characters, not only how many of them there are. A reference carrying
	// the right shape and the wrong alphabet reaches cosign, which reports it
	// against a registry round trip somewhere further down — an error naming the
	// wrong layer of the problem, at the one moment in a release where the
	// operator has least appetite for one.
	for _, r := range digest[len(digestPrefix):] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return "", "", fmt.Errorf("%q is not a sha256 digest: %q is not a lowercase hex character", digest, r)
		}
	}

	return repository, digest, nil
}

// headCommit is the full SHA at HEAD, which is what the provenance names as the
// source it was built from. Full rather than abbreviated: an abbreviation is a
// prefix somebody has to resolve against a repository they may not have.
func headCommit(ctx context.Context, source, gitDir *dagger.Directory) (string, error) {
	out, err := gitContainer(source, gitDir).
		WithExec([]string{"git", "rev-parse", "HEAD"}).
		Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("reading the commit at HEAD: %w", err)
	}

	return strings.TrimSpace(out), nil
}

// sourceURI is where the source came from, as the provenance's resolved
// dependency. It is read from the checkout rather than passed in, so it names
// the repository that was actually built and not the one somebody meant.
func sourceURI(ctx context.Context, source, gitDir *dagger.Directory) (string, error) {
	out, err := gitContainer(source, gitDir).
		WithExec([]string{"git", "config", "--get", "remote.origin.url"}).
		Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("reading the source repository's origin: %w", err)
	}

	// The `.git` suffix is how a clone URL is written and not part of the
	// repository's identity; dropping it is what makes the provenance's URI the
	// one a person would recognise.
	return strings.TrimSuffix(strings.TrimSpace(out), ".git"), nil
}

// gitContainer is a container holding the source with its git metadata grafted
// back on.
//
// New drops .git from the source it binds, because none of the check stages
// reads git metadata and leaving it in would make every commit a cache miss for
// all of them. Provenance does read it, so it arrives as its own argument and is
// mounted rather than folded into the source — which keeps a release's need from
// costing every other stage its cache.
func gitContainer(source, gitDir *dagger.Directory) *dagger.Container {
	return dag.Go().Container(source).WithMountedDirectory("/src/.git", gitDir)
}

// registryHost is the host part of a repository reference, which is what cosign
// authenticates against. `ghcr.io/zaba505/cpybkc` is a repository on `ghcr.io`;
// everything after the first slash is a path within it.
func registryHost(repository string) string {
	host, _, _ := strings.Cut(repository, "/")
	return host
}

// field reads a dotted path out of a decoded JSON document. A numeric element
// indexes an array, so a resolved dependency is reachable as
// `buildDefinition.resolvedDependencies.0.uri`.
func field(doc map[string]any, path string) (any, bool) {
	var node any = doc

	for _, name := range strings.Split(path, ".") {
		switch v := node.(type) {
		case map[string]any:
			child, ok := v[name]
			if !ok {
				return nil, false
			}
			node = child
		case []any:
			i, err := strconv.Atoi(name)
			if err != nil || i < 0 || i >= len(v) {
				return nil, false
			}
			node = v[i]
		default:
			return nil, false
		}
	}

	return node, true
}

// hasField asserts one dotted path holds one value.
func hasField(doc map[string]any, path string, want any) error {
	got, ok := field(doc, path)
	if !ok {
		return fmt.Errorf("the provenance predicate has no %s, and a consumer is told to read one there", path)
	}
	if got != want {
		return fmt.Errorf("%s is %v, want %v", path, got, want)
	}

	return nil
}

// hasStrings asserts one dotted path holds one list of strings, in order.
func hasStrings(doc map[string]any, path string, want []string) error {
	node, ok := field(doc, path)
	if !ok {
		return fmt.Errorf("the provenance predicate has no %s, and a consumer is told to read one there", path)
	}

	list, ok := node.([]any)
	if !ok {
		return fmt.Errorf("%s is %T, want a list", path, node)
	}

	got := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			return fmt.Errorf("%s holds a %T, want strings only", path, item)
		}
		got = append(got, s)
	}

	if !slices.Equal(got, want) {
		return fmt.Errorf("%s is %v, want %v", path, got, want)
	}

	return nil
}
