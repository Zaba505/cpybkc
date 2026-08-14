# Contributing to cpybkc

## The pipeline

fmt, vet, golangci-lint, `go test -race`, `buf lint`, the three artifacts a
release attaches, the published base image and the worked example in the
base-image contract are defined once, in the root Dagger module under
[`.dagger/`](.dagger/). CI calls that module and so do you, which is the point:
there is no arrangement of local commands that passes while CI fails, because
they are the same functions.

Run the whole thing before pushing:

```sh
dagger call ci
```

That is the same call CI makes, with no arguments on either side. It runs the
four Go stages in parallel and reports every failure, not just the first, and it
runs the schema lint, the IR module's own stages, a build of [the CLI
itself](#building-the-cli), a build of the [release
artifacts](#the-release-artifacts), a build of [the published
image](#building-the-image) on every platform it ships for, a build of the
[base-image contract](docs/container/SPEC.md)'s worked example alongside them.

`ci` builds an image, so the module needs real git metadata — which means no
`dagger call` runs from a git worktree. See [The pipeline does not run from a git
worktree](#the-pipeline-does-not-run-from-a-git-worktree).

### Running one stage

`dagger call ci` is the gate; the individual stages are for narrowing down what
it reported.

```sh
dagger call fmt           # gofmt, reported as a diff of what it would rewrite
dagger call vet           # go vet ./...
dagger call lint          # golangci-lint over ./... against .golangci.yml
dagger call test          # go test -race ./...
dagger call proto-lint    # buf lint over proto/ against buf.yaml
dagger call ir-ci         # the whole standard again, over the irpb/ module
dagger call build         # builds cpybkc CGO-free and runs it in an empty image
dagger call ir-artifacts  # builds the two IR artifacts a release attaches
dagger call layout-artifact  # builds the layout schema a release attaches
dagger call image-contract   # builds the published image, checks its contract
dagger call worked-example   # builds docs/container/SPEC.md's worked example
```

The first four are not a second definition of the pipeline. Each drives the same
builder `ci` drives with one stage enabled instead of four, against the same lint
configuration, so a stage that passes on its own passes inside `ci`. `proto-lint`
is the one `ci` calls directly rather than through the standard, because the
standard has no protobuf stage to wrap; see [Linting the IR
schema](#linting-the-ir-schema).

`ir-ci` is not a stage but the whole standard a second time, over the second Go
module; see [The IR module](#the-ir-module) for why there is one. `build`,
`ir-artifacts`, `layout-artifact` and `image-contract` are not stages either; see
[Building the CLI](#building-the-cli), [The release
artifacts](#the-release-artifacts) and [Building the image](#building-the-image).

`worked-example` is the odd one out: it builds a document. The multi-stage
Dockerfile in [the base-image contract](docs/container/SPEC.md#worked-example-adding-a-generator)
is the first thing an adopter runs and the last thing anybody here would notice
had broken, since no other stage reads it, so `ci` extracts it from that file,
builds it, and replays its final stage onto the image `image-contract` just
checked. Edit that example and this is the stage that will tell you about it.

`dagger check` runs them all as a checklist, if you would rather see them
together than pick one.

### The pipeline does not run from a git worktree

**Run `dagger call` from an ordinary clone.** From a git worktree every call
fails, including the ones that have nothing to do with an image:

```
failed to sync: failed to create local fs: failed to create base fs:
stat …/.git: not a directory
```

The reason is one line of git's design meeting one line of Dagger's. The shared
pipeline archetype stamps the commit into every binary it builds and annotates
every image with the commit, its committer time and the repository's origin, so
it needs real git metadata; the root module binds that as its own argument,
`--git-dir`, defaulting to `/.git`. In an ordinary clone that is a directory. **In
a worktree it is a *file*** pointing back at the main repository. Dagger resolves
a `+defaultPath` argument when it constructs the module, before it knows which
function you asked for, so the failure lands on `dagger call fmt` exactly as it
lands on `dagger call image-contract`.

There is no flag that fixes it. What a worktree would need is the main
repository's `.git`, and handing that over would mean stamping an image from one
tree while building it from another — a build identity that is not this tree's,
which is the whole thing the argument exists to prevent.

This is not a footnote for this repository: its own backlog cycle develops every
issue in a per-issue worktree under `.claude/worktrees/`, so the gate a
contributor is expected to run before pushing is the one that cannot run where
they are working. Clone the repository somewhere else, copy the tree over, and run
`dagger call ci` there.

### Building the CLI

```sh
dagger call binary export --path=cpybkc   # the executable itself
dagger call build                         # the check `ci` runs over it
```

The four Go stages already compile `cmd/cpybkc`, since they run over `./...`, so
`build` is not there to find a compile error. It is there for the two things they
say nothing about: that the binary links with `CGO_ENABLED=0` and that it is a
single static file. Both are what [the base-image
contract](docs/container/SPEC.md) rests the published image on — that image
carries the executable and nothing a program needs to start — and neither is
visible to `go vet` or to a test, because the toolchain container has the loader
and the libc the image will not.

So `build` compiles it CGO-free and runs `cpybkc --version` in an *empty* image.
Nothing else is in there: a binary needing an interpreter or a libc does not
start at all, and that failure is this check rather than an image somebody
publishes.

### Building the image

```sh
# the image itself, for one platform
dagger call image --platform=linux/arm64 export --path=cpybkc.tar
dagger call image-contract                         # the check `ci` runs
dagger call image-contract --platform=linux/amd64  # one platform of it
```

The published image is a **public contract**. A Dockerfile in somebody else's
repository says `FROM ghcr.io/zaba505/cpybkc:v0` and names a path inside it, so
[the base-image contract](docs/container/SPEC.md) is a document strangers depend
on and `image-contract` is that document's compatibility guarantees table
executed rather than read: the entrypoint, `Cmd`, the user, `PATH`, an exhaustive
listing of every path in the filesystem with its kind, owner and mode, the build
settings the executable itself reports, a byte comparison of the IR schema the
image ships against the artifacts a release attaches, the entrypoint answering
`--version` as the image's own user and as an overridden one, and the version
each of the two executables reports being the version the image was built for
(#181).

It runs on every platform the image is published for, and reports each platform's
failures separately — "it holds on amd64 and not on arm64" is the finding. The
foreign-platform legs are cross-compiled by the toolchain container; only the
runs *through* the entrypoint are emulated, and they are one `--version` each.

The image is **not** assembled here any more. It used to be, on the argument that
the standard pipeline's app archetype published one image per binary and had no
notion of a base other people build `FROM`; the refactored archetype's plugin
directory, non-root user and content contributions are exactly what this project
needed, so the image is `Go.App` plus this repository's two contributions and
`.dagger/image.go` is now only the contract check.
[`.dagger/main.go`](.dagger/main.go)'s package comment carries what this module
still owns and why.

There is no `dagger call publish`. The archetype's publish is signed and attested
by construction and refuses a run it cannot produce provenance for, so the only
caller that can reach it is the release workflow holding an OIDC token. To get
the image onto your own machine, export it and load it:

```sh
dagger call image --platform=linux/amd64 export --path=cpybkc.tar
docker load < cpybkc.tar
```

### Getting the tools

The pipeline needs the Dagger CLI and a container runtime (Docker or Podman).
Nothing else — not even a Go toolchain, since every stage runs in a container
built from the version in `go.mod`.

```sh
curl -fsSL https://dl.dagger.io/dagger/install.sh \
  | DAGGER_VERSION="$(jq -r .engineVersion dagger.json | tr -d v)" \
    BIN_DIR="$HOME/.local/bin" sh
```

The version comes out of `dagger.json` rather than being typed in, because the
`engineVersion` the module declares is what CI installs too. A CLI that
provisions a different engine is a difference between your machine and CI, which
is the one thing this module exists to prevent.

### After changing the module

`.dagger/dagger.gen.go` and `.dagger/internal/` are generated **and committed**.
Regenerate them after editing `.dagger/main.go` or `dagger.json`, and commit the
result alongside the change:

```sh
dagger develop
```

They are committed rather than ignored because Dagger is moving to requiring
generated code in the tree by v1.0, and because it means the module builds from
a checkout alone instead of only after somebody has run `dagger develop`.
`.dagger/.gitattributes` marks them `linguist-generated`, so they stay collapsed
in diffs and out of the repository's language statistics.

A pull request whose `.dagger/main.go` or `dagger.json` moved without the
generated files moving with it is a tree that does not build. Bumping a
dependency pin counts: `dagger develop` rewrites `.dagger/internal/dagger/` from
the dependency's schema, so the pin and the generated bindings have to land in
the same commit.

## Why the module wraps the Z5Labs standard pipeline

`ci` does not implement the four stages. It hands the source to
[`github.com/z5labs/devex/daggerverse/z5labs`](https://github.com/z5labs/devex/tree/main/daggerverse/z5labs)
and lets that module's `Go` chain run them — the same standard `z5labs/dfcad`
runs.

Reimplementing them here would be a second definition of what "checked" means in
a Z5Labs repository, and two definitions drift. A stage added to the standard
would silently not apply to cpybkc; a difference in how a stage is invoked would
show up as this repository disagreeing with every other one, for reasons nobody
wrote down. Wrapping costs one dependency and makes that impossible. The full
reasoning, including why the stage functions are not a fork of it, is in
[`.dagger/main.go`](.dagger/main.go)'s package comment.

The image and the publish come from the same place. They did not use to: the
archetype published one image per binary and had no notion of a base other
people build `FROM`, so this module built the image, derived the tag family and
signed the result itself — about 2,000 lines of it. The refactored `Go` → `App` →
`Publish` chain removed that premise, and cpybkc adopted it whole. `/usr/local/bin`
is the archetype's plugin directory with `PATH` composed from the same constant,
65532:65532 is its pinned non-root user, `App.WithFile` and `App.WithDirectory`
contribute the IR schema with a document each, and `App.Publish` derives the tag
family, pushes one index under every tag of it, signs it recursively and attaches
provenance and SBOMs.

What this module still owns is the contract checks, whether a commit is a release,
the release notes, and the fact that this repository holds four Go modules.
[`.dagger/main.go`](.dagger/main.go)'s package comment is the full list and the
argument for each.

Both dependencies in `dagger.json` are pinned to one `devex` commit, so a bump
has to move them together.

## Linting

`.golangci.yml` is written in the golangci-lint **v2** dialect, because v2 is the
major the standard pipeline runs. The majors are not interchangeable: a v2 binary
refuses a v1 file outright, before any linter runs, and v1 refuses a v2 one. The
`version: "2"` at the top of the file is the config schema's version, not a tool
pin — the tool pin lives in the shared Go module, which is where a bump belongs.

The linter selection is v2's own default set, spelled out rather than inherited:
errcheck, govet, ineffassign, staticcheck and unused. Note that `staticcheck` in
v2 subsumes what v1 called `gosimple` and `stylecheck`, so the same five names
select more checks than they did under v1.

If you run `golangci-lint` directly rather than through `dagger call lint`, use a
v2 release or it will reject the config for the mirror-image reason.

### Linting the IR schema

The resolved IR's protobuf schema lives at
[`proto/cpybkc/ir/v1/ir.proto`](proto/cpybkc/ir/v1/ir.proto), and `buf.yaml` at
the repository root configures the lint over it: buf's `STANDARD` category, with
no exceptions and nothing ignored. `dagger call proto-lint` is what runs it, and
`ci` runs it too, so the gate covers the schema.

That stage is this repository's own rather than the standard's, because the
Z5Labs Go pipeline has no protobuf stage to wrap. It is still one definition
rather than two: the buf release is pinned by tag *and* by digest in
[`.dagger/main.go`](.dagger/main.go), and the configuration it reads is the
`buf.yaml` committed here, for the same reason the Go lint stage is handed this
repository's `.golangci.yml`.

`buf` is not needed locally — the stage runs it in a container, like every other.
If you have the CLI, `buf lint` from the repository root reads the same file and
gives the same answer.

Changing the schema is not only a lint question. `ir.proto`'s own comments carry
the compatibility policy — which edits require `Descriptor.version` to advance,
and why that rule is stricter than what protobuf calls wire-compatible — and
[`docs/ir/SPEC.md`](docs/ir/SPEC.md) is normative for what every node means. A
new node kind, or a new member of any closed set in the file, advances the
version even though `buf` will not say so.

## The IR module

The Go form of the resolved IR is [`irpb/`](irpb/), and it is a **separate Go
module** — `github.com/Zaba505/cpybkc/irpb` — not a package of this one. Its
package comment carries the argument; the short version is that a generator
plugin importing the IR should take on the protobuf runtime and nothing the CLI
happens to need, and only a module boundary delivers that.

Three consequences a contributor meets:

- **`irpb/go.mod` requires exactly one module.** `irpb/module_test.go` fails the
  build if a second appears, including a test-only one — a test dependency lands
  in a plugin author's module graph like any other, which is also why that test
  parses `go.mod` by hand instead of using `golang.org/x/mod`.
- **`go test ./...` from the repository root does not reach it.** That is Go's
  rule about nested modules, not a configuration, and it is why the pipeline
  invokes the standard a second time as `dagger call ir-ci`. A change to `irpb/`
  is checked by `dagger call ci` like everything else, but through that call.
- **The root `go.mod` requires it with a `replace`.** A module cannot be required
  at a version nobody has published yet, and the first `irpb/v0.1.0` can only tag
  a commit that already contains `irpb/`. The line comes out with that tag; the
  comment beside it says what breaks if it does not.

### Regenerating the stubs

`irpb/ir.pb.go` is generated from `proto/cpybkc/ir/v1/ir.proto` and **committed**,
for the reasons `.dagger/`'s codegen is: the module has to build from a checkout
alone, and a published module whose sources appear only after somebody runs a
generator is not a module anyone can `go get`.

```sh
dagger call proto-gen export --path=irpb
```

[`buf.gen.yaml`](buf.gen.yaml) decides what that writes — it pins the
`protoc-gen-go` release, and `irpb/go.mod` requires the matching
`google.golang.org/protobuf`, so the two move in one change. `dagger call
proto-gen` and a contributor's own `buf generate` read that same file and produce
the same tree.

A pull request whose `ir.proto` moved without `irpb/ir.pb.go` moving with it is
the protobuf half of the rule [After changing the
module](#after-changing-the-module) already states about `.dagger/`. Nothing in
`ci` regenerates to check it: a stage that rewrites the tree it is checking is a
pipeline with an opinion about your working directory, and this is what review is
for.

### Releasing it

The IR module's tags are `irpb/vX.Y.Z` — Go's own rule for a module in a
subdirectory, not a convention chosen here. They move independently of the CLI's
`vX.Y.Z`, which is the point: a documentation fix in `irpb/` is a release of the
IR module and of nothing else, and a CLI release is not a new IR for anyone to
re-vendor. `.github/workflows/ci.yaml` matches both patterns.

Neither tag is the IR's own version. That is `Descriptor.version`, specified by
[`docs/ir/SPEC.md`](docs/ir/SPEC.md), and one IR version outlives many module
tags. If the schema ever breaks its wire format and becomes package
`cpybkc.ir.v2`, the Go module becomes `github.com/Zaba505/cpybkc/irpb/v2` under
Go's major-version rule, and the two version suffixes agree without either being
kept in step by hand.

## The release artifacts

Every published release carries three files. Two describe the IR, for the plugin
authors who are not importing `irpb`; the third is the layout schema, for the
shop generating layout files rather than writing them:

```sh
dagger call ir-descriptor-set export --path=ir.binpb
dagger call ir-protos export --path=ir-protos.tar.gz
dagger call layout-schema export --path=layout-schema.sexpr
```

`ir.binpb` is the protobuf `FileDescriptorSet` describing
`cpybkc.ir.v1.Descriptor`, which is what lets a runtime with no code generation
in the build decode a descriptor dynamically. `ir-protos.tar.gz` is the schema
sources at the paths their protobuf packages require, for a build that would
rather compile them. [Reading a descriptor without generated
code](docs/ir/SPEC.md#reading-a-descriptor-without-generated-code) is the
contract for both, and it is the document to change if what they contain changes.

`layout-schema.sexpr` is the machine-readable contract for the layout format:
the set of form declarations a layout generator targets, written in the notation
it describes. It is `schema/layout.sexpr` in the tree, published byte for byte,
because an adopter's diagnostic and this repository's have to be about the same
text. [The published schema](docs/layout/SPEC.md#the-published-schema) is the
document to change if what it declares changes — including the version it
carries, which moves with the format and not with the document.

Three properties are worth knowing before you touch any of them:

- **Neither IR artifact is committed.** `ir.binpb` is computed from the
  descriptors `protoc-gen-go` compiled into `irpb`, so it cannot drift from the
  schema; a checked-in copy could. Changing `ir.proto` and regenerating
  `irpb/ir.pb.go` changes what is published, in the same commit. The layout
  schema is the other way round — it is a source file a person writes, so it is
  committed, and `layout-schema` publishes it unchanged after checking that it
  loads.
- **All three are reproducible.** Two builds of one commit produce byte-identical
  files — the encoding is deterministic, every tar field the filesystem could
  have supplied is a constant, and the schema is copied rather than reformatted —
  because an artifact that moved on a rebuild would make a rebuild
  indistinguishable from a change to the contract.
- **`dagger call ci` builds them.** `ir-artifacts` and `layout-artifact` are in
  the pipeline so that the recipe a release runs is exercised on every pull
  request, rather than for the first time on a tag. What is in them is asserted
  by Go tests, in `irpb` and beside each tool under `internal/tools/`; the
  pipeline stages only check that the commands run to completion and leave a file
  behind.
- **Both IR artifacts also ship inside the image**, at
  `/usr/local/share/cpybkc/ir.binpb` and `/usr/local/share/cpybkc/proto/`, so
  that a plugin author building `FROM` the image needs no download at all. They
  are the same nodes — `image` copies in the file `ir-descriptor-set` produces,
  rather than building its own — and `image-contract` compares the image's copy
  against a fresh build byte for byte. That is what makes [the base-image
  contract](docs/container/SPEC.md#these-are-the-release-assets-not-copies-of-them)'s
  "two ways of getting one artifact, not two artifacts" a property of the
  pipeline rather than a promise somebody has to keep.

A `.proto` added under `proto/` that nothing reachable from
`cpybkc.ir.v1.Descriptor` imports fails `TestPublishedFileDescriptorSetCoversTheSchema`.
That is deliberate: the IR is one message and everything in `proto/` is reachable
from it, so a file that is not is a mistake in the schema rather than something
to exclude.

`.github/workflows/release.yaml` attaches all three to a release when one is
published, building them with the same three calls above at the release's tag.

## Signing a release

A published image carries a signature and two kinds of attestation, and **none
of them is produced here any more.** `.dagger/sign.go` is gone, and so are
`sbom`, `provenance`, `attest` and `attestations`: `App.Publish` in the shared
pipeline signs every published digest recursively, attaches a signed SLSA
provenance statement, and attaches an SPDX and a CycloneDX document per platform.
It refuses a publish it cannot produce provenance for, so there is no arrangement
in which an image goes out unsigned.

What a consumer does with any of it is [the base-image
contract](docs/container/SPEC.md#verifying-a-signature)'s, including [how to
discover the attestations](docs/container/SPEC.md#discovering-the-attestations)
and [what changes after mirroring into an internal
registry](docs/container/SPEC.md#after-mirroring-to-an-internal-registry).

Three things are worth knowing:

- **The signature is recursive; the attestations are not.** A tag resolves to a
  multi-platform index, so the signature covers the index *and* each per-platform
  manifest under it — otherwise the manifest a consumer's runtime pulls is
  unsigned while `cosign verify` against their tag still passes. The provenance
  and the SBOMs go on the index digest alone, because they are statements about
  the release and the release is the index.
- **Signing is keyless, and the provenance says only what the token says.** There
  is no cpybkc key to hold or rotate; the identity is the release workflow,
  certified for the length of one run from the OIDC token GitHub mints. Every
  identifying field in the provenance comes out of that token's claims, which is
  why `release` no longer takes a `--builder` or an `--invocation`: anything a
  caller could have supplied attests to nothing.
- **Nothing here checks them, and that is deliberate.** There is no predicate of
  this repository's left to check the shape of, and a table here would be a
  second copy of somebody else's rule — the copy that stays green after the
  original moves. The documents are checked upstream, against the code that
  writes them.

## Making a release

Publish a GitHub release whose tag is a canonical `vX.Y.Z`.
`.github/workflows/release.yaml` does the rest: it attaches the three artifacts
above, pushes the image, signs the digest, and writes into the release's notes
which IR version that image speaks.

Nothing else is a step, after the first one. There is no version to bump by hand
and no tag to push beyond the one the release object carries.

### The version a release publishes is the version it was cut from

Both binaries learn their own version at link time, from the release. The shared
archetype cross-compiles the CLI with `-X main.version=<version>`, with the flag
and the variable's name fixed by that module so every z5labs Go application
answers "which build am I running" the same way;
[`.dagger/image.go`](.dagger/image.go)'s `generatorBinary` passes the same stamp
by hand to `cpybkc-gen-go`, which is built through the generic constructor and
so has nobody upstream to stamp it. A build nobody stamped keeps the value in
the tree, `v0.0.0-dev`, which is the same string
[`.dagger/image.go`](.dagger/image.go)'s `devVersion` builds everything that is
not a release under — so a `go build` from a checkout and an unreleased pipeline
build are indistinguishable, as they should be.

The version reaches the two programs as the image tag, `v0.2.0`, and each drops
the leading `v` before printing it: `cpybkc --version` writes a SemVer string
because [the CLI contract](docs/cli/SPEC.md#--version) requires one, and
`cpybkc-gen-go` names its version in a refusal in the same scheme. That rule is
written three times — once in each command and once in the pipeline — because
the two commands deliberately share nothing (`cmd/cpybkc-gen-go` imports `irpb`
and the standard library and nothing else of this repository, so that it
exercises the surface an outside plugin author has) and the pipeline is a Go
module that cannot import either. What holds the three together is the check
below rather than an import, exactly as `generatorRepository` and the companion
module's spelling of it are held together.

**This is the opposite of what this file said before #181, and the reasoning is
worth keeping.** The version used to be a constant in the source, moved by hand
in the release that published it, on the argument that *"a stamped version is a
build whose output depends on how it was invoked, and this repository builds its
binaries from the tree alone"*. Two things retired it:

- The premise stopped holding. `planRelease` takes the version from the
  canonical version tag pointing at HEAD and refuses one pointing anywhere else,
  so the version is a ref of the tree being built rather than something a caller
  chooses — the same standing the commit that link step also stamps has. Two
  builds of one commit under one release are still byte-identical, which is what
  makes [re-running a release](#re-running-a-release-is-safe) safe and is the
  whole of what that argument was protecting.
- There stopped being a choice. Since #185 the archetype passes the stamp with
  no seam to turn it off, and a linker silently ignores a stamp naming a
  constant — so the constant did not decline the stamp, it hid it. The pipeline
  appeared to be stamping a version while `--version` kept printing what the tree
  said, which is how `v0.0.0` shipped an image identifying itself as an
  unreleased development build.

`dagger call image-contract` is what refuses a version that disagrees with the
release it was cut from. It runs the published image's `--version` through its
entrypoint, and runs the generator out of the image on a descriptor stating no
IR version so that the refusal names the generator's own, and holds both to the
version the image was built for. `release` runs that same check as its gate,
over the very containers it is about to push and under the release's tag, so
nothing is published under a version its binaries disagree with.

On a pull request the version being checked against is `devVersion`, and that is
the same string both commands carry in their tree — so a stamp that landed and a
stamp the linker silently dropped write the same line, and neither of those
assertions can tell them apart. What they do catch there is a version that
stopped dropping the tag's leading `v`. Proving the stamp lands at all takes a
version nothing publishes, which is why the contract builds one image under
`v0.0.0-contract` and requires the two executables to report it: under a value
that cannot collide with the tree's default, a `const` that `-X` cannot reach
fails on an ordinary pull request instead of at a release, where the tag it
disagrees with has already been pushed.

`internal/assemble.Version` is a different number and is not stamped by
anything: it is the IR version, and `--version` reports it beside the build's
own.

**The first release needs one manual step, once.** A package that
`secrets.GITHUB_TOKEN` creates on ghcr.io is private, and nothing the workflow
can do with the default token makes it public. Until somebody sets the package's
visibility to public in its settings on GitHub, every `FROM
ghcr.io/zaba505/cpybkc:v0` in [the base-image contract](docs/container/SPEC.md)
gets a 401 — which is the whole point of the image, so it is worth doing before
announcing the release rather than after the first bug report. The same page is
where a user-namespace package is given the repository write access the workflow
pushes with. Every release after that one is the paragraph above.

Releases are also expected to be cut in ascending version order. The moving tags
follow the release being published rather than the highest version ever
published, so a backport cut *after* the release that supersedes it would land
`v0` and `latest` back on the older image.

### Whether a commit is a release is a function, not a step

[`.dagger/release.go`](.dagger/release.go) is handed the release's tag and checks
it against the refs at HEAD: a canonical `vX.Y.Z` pointing at HEAD is a release
of the image, and anything else is not. The **tag family** that version implies —
the table in [the base-image
contract](docs/container/SPEC.md#tags-and-what-pinning-one-buys) — is the shared
pipeline's, derived from the version and checked there against its own literals.
That split is stated in the document itself, so a reader concludes neither that
the table is unenforced nor that this repository enforces it.

The major tag is the one a derived Dockerfile should usually pin — it picks up
every fix inside the compatibility guarantees — and it is what the [companion
Dagger module's default](#the-default-image-tag-is-the-moving-major-tag)
resolves through.

```sh
dagger call tag-scheme             # the release decision, over the cases that matter
dagger call release-notes-contract # the block a release's notes carry
```

Both are in `ci`, so the decision is checked on every pull request rather than
discovered at a release. Three edge cases are settled there rather than in
production:

- A **prerelease** publishes its own full version tag and moves none of the other
  three. A release candidate is not a fix anybody consented to be given.
- A version carrying **`+build` metadata** is refused rather than mangled. An OCI
  tag has no `+` in it, and dropping the metadata would publish two releases
  under one name.
- **Two version tags on one commit** is an error, because which of them `latest`
  should follow has no defensible answer.

A commit with no version tag publishes nothing and succeeds — which is what lets
the workflow fire on the IR module's `irpb/vX.Y.Z` releases without a filter
naming tag shapes in YAML.

### Re-running a release is safe

The job can be re-run, and this project's contract still holds that a published
full version tag is never repointed. Both are true because the image is a
function of the source: the binary is built `-trimpath` and CGO-free, the IR
artifacts are byte-deterministic, and the image is assembled from those alone. A
second run pushes the same bytes, so every tag lands back on the digest it
already named — one index pushed once, with every tag of the family pointing at
it.

The asset uploads use `--clobber`, the signature and the attestations are
additive rather than replacing, and the notes block is delimited and regenerated
rather than appended — so a second run leaves one block, not two.

Correcting a *broken* release is the case none of that covers, and the answer is
a new version number. Repointing `v0.2.0` at different bytes is the one thing
[the contract](docs/container/SPEC.md#tags-and-what-pinning-one-buys) forbids
outright.

### Publishing somewhere else

Where the image goes is an argument, not a constant: `release` takes the
registry and the repository as two arguments, so a mirror or an internal registry
serves the same release by changing one of them and nothing else.

There is no `publish` verb beside it any more, and nothing replaces the
capability. The archetype's publish refuses a run it cannot produce provenance
for, so the only caller that can reach it is the release workflow holding an OIDC
token — pushing to a test registry by hand is not something this pipeline offers.
What a contributor does instead is [export the image and load
it](#building-the-image).

Holding the registry out is deliberate — [the base-image
contract](docs/container/SPEC.md#also-out-of-scope) holds it out of what it
promises, because a mirror serving the same digest satisfies it identically. What
is *not* an argument is which tags exist, for the reason above.

## The companion Dagger module

A second Dagger module ships from this repository: the consumer-facing one that
runs the CLI for a caller, published as
`github.com/Zaba505/cpybkc/daggerverse/cpybkc` and unrelated to the `.dagger/`
module that runs this repository's own pipeline. It lives in
[`daggerverse/cpybkc/`](daggerverse/cpybkc/) (#61), and one thing about it had
to be settled before it could be written, because the answer is a default value
in a constructor, and a default in a module directory that is never renamed is
public API from the first release.

The module gets no `SPEC.md`. It is a convenience over the [base-image
contract](docs/container/SPEC.md) rather than an interface of its own, and what
it needs to say it says in its module comment and in `dagger call --help`
([`docs/CONVENTIONS.md`](docs/CONVENTIONS.md), *What belongs here*). What
follows is the argument behind one of those comments, kept here because the
argument is longer than the comment and outlives the reader who needs it.

### The directory name is the public ref, so it is chosen once

A Dagger module ref is a directory path inside a tag of this repository, which
makes `daggerverse/cpybkc` as much public API as any default in it. Renaming
the directory would not deprecate the old ref, it would delete it: a caller
pinning `github.com/Zaba505/cpybkc/daggerverse/cpybkc@v0.3.1` resolves that path
inside the tag they named, so a rename breaks every unpinned caller at once and
strands every pinned one on a path that will never move again. There is no
redirect to leave behind and no deprecation period to serve, so **the directory
is not renamed** — a module that has to be called something else is a new
directory published beside this one.

`daggerverse/<module>` is the house layout, and it is where
[`github.com/z5labs/devex`](https://github.com/z5labs/devex) publishes the two
modules this repository's pipeline already depends on. The module is named for
what it drives rather than for what it does, so a second module shipping from
here later sits beside it without either being renamed.

### One `engineVersion`, held by a dependency edge and one check

The root [`dagger.json`](dagger.json) declares the companion as a **local**
dependency (`"source": "daggerverse/cpybkc"`), and `dagger call engine-lock`
asserts that the two `dagger.json` files pin the same engine. It takes both,
and it is worth being exact about why, because the edge alone does only half
the job.

The edge's half is one-directional, and it was measured rather than assumed.
Setting the companion's `engineVersion` **above** the engine in use fails every
root call outright:

```
! failed to resolve dep to source: module requires dagger v0.99.0,
  but you have v0.21.8
```

Setting it **below** the root's does not fail anything: `dagger functions`,
`dagger call ci` and the rest load and run exactly as before. So the edge
catches a companion that has run ahead of the engine, and it misses one that has
been left behind — which is the direction drift actually goes, since lagging is
what a file nobody edited does. `dagger develop` rewrites each module's own
`dagger.json` independently, so the two can diverge through the ordinary
workflow rather than through neglect.

[`EngineLock`](.dagger/companion.go) covers that direction, and it is in `ci`,
so it runs on every pull request:

```sh
dagger call engine-lock
```

This is **not** the shape of check
[#65 was closed for being](#65-is-closed-rather-than-left-open). That one
compared a generated constant against the release it had just followed and could
not fail; this one fails on a bump somebody performed in one module and not the
other, which is a thing that happens and which nothing else reports. A check
that can fail is the difference.

What the edge buys beyond that is cheapness: both files are in one tree, a bump
is one commit that edits both, and the same commit has to carry regenerated code
for both modules anyway (below). The daggerverse modules this project depends on
pin theirs independently, in another repository, and have already drifted — one
tree, one commit and one version is the difference being bought.

The same edge is how the pipeline gets to drive the module over the image built
in the pull request rather than the last published release (#64). Both uses are
served by one line.

### The companion module is checked like any other Go module here

`dagger call companion-ci` runs the standard pipeline — fmt, vet, lint and `go
test -race` — over [`daggerverse/cpybkc/`](daggerverse/cpybkc/), and it is in
`ci`. It is a third call rather than a wider source directory for the reason
[`IrCi`](.dagger/main.go) is a second one: the companion is a separate Go
module, so `go test ./...` from the repository root stops at its `go.mod` and
never descends. Without it, the module strangers actually call would be the one
part of the tree nothing checks.

What it runs is [`internal/imageref`](daggerverse/cpybkc/internal/imageref/),
which is where the image-reference assembly lives **precisely so that it can be
tested at all**. The module's own `package main` imports the generated Dagger
client, whose `init` panics when no session is present, so a test beside `main`
cannot run under plain `go test`. Keeping the pure part in a package that
imports no Dagger is what turns the published default
`ghcr.io/zaba505/cpybkc:v0` into a string a test pins rather than a string a
comment asserts.

This is not [`companion-module`](#the-modules-own-calls-are-checked-against-the-image-this-pull-request-built).
That one drives the module's *functions* over the image built in the same pull
request, which needs an engine and a built image; this is the ordinary Go
pipeline over the module's source, and the two catch different things.

Both modules' generated code is committed, so a change to either is a
`dagger develop` in that module and the generated files in the same commit —
[*After changing the module*](#after-changing-the-module) applies to the
companion exactly as it does to the root.

### The curated surface, and the check that keeps it honest

`Generate` takes a source directory and a manifest and hands back a
`Directory`; `Run` takes an argument vector verbatim and hands back a
`Container`. Those two are the whole surface, and the split between them is a
decision rather than a stopping point (#62).

The module **does not** grow an argument per entry in
[`docs/cli/SPEC.md`](docs/cli/SPEC.md)'s flag table. A module argument is public
API for as long as the published ref exists — the [directory name is the public
ref](#the-directory-name-is-the-public-ref-so-it-is-chosen-once) — so a table
mapped one-to-one would make every change to the CLI's surface a change to this
module's, and it would have to express in Dagger arguments two things a command
line says better: a flag that *replaces* the action rather than configuring it
(`--emit-ir` is terminal — no generator runs, and nothing is merged or pruned),
and a flag that is only legal beside another (`--emit-ir-format` without
`--emit-ir` is a usage error). `Run` is what makes the small surface safe: an
uncurated flag is reachable without the module having an opinion about it, and
it returns the container rather than a directory because the uncurated
invocations are exactly the ones whose answer is not a tree — `--emit-ir` may
write to standard output, and `--version` and `--help` write nothing else at
all.

What a curated surface risks is the CLI quietly growing a flag the module can
no longer express, which reads from out here exactly like a module that chose
not to express it. `dagger call cli-surface` is the difference, and it is in
`ci`:

```sh
dagger call cli-surface
```

It reads the CLI's flag constants out of the [`cmd/cpybkc/`](cmd/cpybkc/) tree
and fails when one of them is not recorded in `companionCoverage` in
[`.dagger/companion.go`](.dagger/companion.go), when a recorded entry names a
function `daggerverse/cpybkc` does not declare, or when an entry names a flag
the CLI has stopped accepting. **So adding a flag to cpybkc fails CI until
somebody says which side of the curation it falls on** — a curated argument on
`Generate`, or `Run`.

Be exact about what an entry in that table claims, because it is less than it
looks. It is a person's assertion that they thought about the flag and decided
where it belongs; nothing verifies that the named function can *reach* it, and
since `Run` forwards an arbitrary vector, every flag is reachable through `Run`
by construction. What the check buys is that the assertion has to be re-made
whenever the CLI's surface moves, which is exactly when it stops being true by
accident.

Three things about how it reads are worth knowing before changing either side.
It reads the parser's **constants**, not `cpybkc --help` and not the spec's
table: the help text is deliberately written out by hand, because what a flag is
called is a covered guarantee and what usage says about it is explicitly not
one, so a check reading it would fail on a rewording and pass on a flag the
document forgot. It reads them from the whole tree and in every shape a constant
can be written — package scope or a function body, one hyphen or two, a literal
or one flag's spelling built from another's — because each of those was a hole
found in review, and each is now a row in
[`.dagger/internal/surface/`](.dagger/internal/surface/)'s tests rather than a
sentence in a comment. And it is a **flag**-level check because
`docs/cli/SPEC.md` fixes cpybkc as one command with no subcommands — there is no
verb list for a module function to correspond to, so the surface that can drift
is the flag table.

Two things stop it degrading quietly, which matters more here than for an
ordinary check: a drift guard's failure mode is *staying green*. A read that
finds no flags at all fails rather than passes, and a constant whose value it
cannot evaluate is reported rather than dropped, because "I could not read this"
and "this is not a flag" are different things to have learned.

This is the shape of check [#65 was closed for not
being](#65-is-closed-rather-than-left-open). It fails on something a person does
— adding a flag in one module and not thinking about the other — rather than on
a constant compared against the thing it was generated from.

### Composing a generator is `COPY --from`, split across two methods

The base image carries the CLI and no generator, so an image that generates
anything is a composition. `WithGenerator` performs it, and the thing to keep in
view is that it is not a second extension mechanism (#63):

```sh
dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
  with-generator --name hello --image ghcr.io/example/cpybkc-gen-hello:v1 \
  generate --source . export --path .
```

```dockerfile
FROM ghcr.io/zaba505/cpybkc:v0
COPY --from=ghcr.io/example/cpybkc-gen-hello:v1 --chown=65532:65532 --chmod=0755 \
     /usr/local/bin/cpybkc-gen-hello /usr/local/bin/cpybkc-gen-hello
```

Those are the same two instructions — the second is the final stage of
[the container contract's worked
example](docs/container/SPEC.md#worked-example-adding-a-generator), which is what
an adopter writes by hand and what the module is measured against. What the
module saves is the build context, the registry to push a derived image to and a
Dockerfile to keep in step with a cpybkc release; a caller who prefers the
Dockerfile is not on a lesser path, and the [module gets no
`SPEC.md`](#the-companion-dagger-module) precisely so that nothing here reads as
the contract having moved.

**Two methods rather than one.** `WithGenerator` takes a generator *image* and is
the adopter's path: it works for a generator image this project has never heard
of, and with `--image` omitted it pulls `<repository>-gen-<name>:<version>` —
resolved against the coordinates the CLI image itself came from, so a generator
from one release never lands beside a CLI from another without somebody having
said so. `WithGeneratorExecutable` takes a *File* and is the generator author's
path, for the plugin that has been built and not yet published, which is exactly
when checking it against a real cpybkc run matters most. The split follows
[`z5labs/avroc`'s module](https://github.com/z5labs/avroc/blob/v0.2.0/daggerverse/avroc/main.go),
where one method for both was tried first and the author's case turned out to be
the one nobody would notice breaking.

**Permissions and no owner.** The file lands `0755` and its ownership is left
alone, which is the one place the module deliberately does less than the
`COPY` line above it. The mode is what makes the file runnable, by the image's
own UID and by any UID a caller overrides it with; the owner is a property of the
image the module was handed, and a caller who passed `--image` may be composing
onto a base with a user of their own that this module has no business overwriting
with the contract's. The Dockerfile is entitled to write `--chown=65532:65532`
because its `FROM` line names the image it derives from; a module handed a
container knows no such thing.

**Static linking stays the caller's obligation.** Nothing in the module can check
it, and a dynamically linked generator fails at exec time with the kernel's
message rather than cpybkc's. That is the same requirement the worked example
states as `CGO_ENABLED=0`, and it is said in both methods' documentation rather
than enforced in either.

### Multi-platform is one composition per platform

A composed image is one platform, because a `dagger.Container` is. The module's
guarantee is that it is *one* platform: `WithGenerator` reads the platform off
the container it is composing into and pulls the generator image for that
platform, and refuses an explicitly supplied generator image built for another
one. An arm64 base quietly acquiring an amd64 generator is a build that succeeds,
pushes, and then fails at the first generation with a kernel message naming
neither cpybkc nor the call that caused it.

That refusal has an escape hatch, named in its own error text: a platform string
is a comparison the module can be wrong about — a variant spelling that runs
perfectly well is still not the same string — so a caller who knows better takes
the file out of the image themselves and passes it to
`WithGeneratorExecutable`, which states the match as their obligation anyway.

`New`'s `--platform` is what makes the other architecture reachable at all;
without it an amd64 engine could only ever compose an amd64 image. It is refused
beside `--image`, because a container arrives already built for a platform and
the argument would then describe a pull that is not happening — ignoring it
silently is the worse half of that choice, since the caller would believe they
had asked for an architecture and find out at exec time that they had not. The
check is a comparison of arguments rather than of the container's actual
platform, so that a constructor still performs no network round trip, for the
same reason [`internal/imageref`](daggerverse/cpybkc/internal/imageref/)
validates the shape of a reference and not its existence.

A derived **multi-platform index** is therefore one composition per platform,
published as variants of one index, and the loop belongs to the caller because
the index does: which platforms a derived image serves is a property of who will
run it, and a module that decided it would be publishing this project's platform
table as somebody else's. The module comment carries the loop.

### The module's own calls are checked against the image this pull request built

Everything above reads the module. `companion-ci` runs `go test` over its
source, `engine-lock` reads its `dagger.json` and `cli-surface` reads its
function *names* — and none of the three makes a call, so a module whose calls
no longer compose into a working image would fail none of them. `dagger call
companion-module` is the one place `dagger call -m daggerverse/cpybkc …`
actually happens, and it is in `ci` (#64):

```sh
dagger call companion-module
```

That matters more than it would for a contract, because the module is a
*convenience* over the [base-image contract](docs/container/SPEC.md) rather than
an interface of its own. A caller reaches for it precisely because they did not
want to learn the contract underneath, so a convenience that has quietly stopped
working lands the failure on somebody with no reason to know where to look.

**It drives the image this run built, never a published one.** The module's
defaults pull `ghcr.io/zaba505/cpybkc:v0`, which is a *released* image: a check
that used them would be checking the last release and would keep passing through
a pull request that broke both the module and the image it drives. So the base
image [`image.go`](.dagger/image.go) just built is passed through the same
`--image` argument a caller uses to try an unreleased cpybkc, and the generator
is `cmd/cpybkc-gen-go` built from the tree under test. That is also the only
reason the module takes `--image` at all, which is worth knowing before anybody
removes it as unused — it is used here, on every pull request, and the [local
dependency edge](#one-engineversion-held-by-a-dependency-edge-and-one-check) is
what makes it reachable.

*Never* is enforced rather than intended. The module is constructed with
`--repository` pointing at `cpybkc.invalid/never-pulled` — `.invalid` is
reserved by RFC 2606 and resolves nowhere — so a `with-generator` call added
later that forgot to pass an image fails naming that host, instead of pulling
the *released* generator, generating with it and passing. That is the failure
worth designing against: it would look exactly like a green run while checking
the last release instead of the change.

**The assertion is the committed example, byte for byte.** The module composes a
generator, runs `generate` over [`example/`](example/) and has to reproduce that
tree exactly — the same golden tree
[`example/regenerate_test.go`](example/regenerate_test.go) holds the CLI to. A
smoke test saying the calls *ran* would pass on a module that composed an image
which generated the wrong thing.

**Both compositions, against that same tree.** `with-generator` from a generator
image and `with-generator-executable` from a file are required to produce it,
which is what makes the two
[interchangeable](#composing-a-generator-is-copy---from-split-across-two-methods)
rather than merely both present (#63). Both start from the base image, which
carries the CLI and **no** generator — so a generation that succeeded did so with
the generators these calls installed and not with something lying around in the
image. `image` and `run` are checked too, since a check that exercised only what
it needed would leave the rest of the module's surface covered by nothing.

**Two generators, because the example runs two.** Since #191 the committed
example names `go` *and* `graph`, so each composition installs both and the
plugin directory is required to hold exactly those two. `cpybkc-gen-graph` ships
no image — a release publishes the CLI image and one generator image, and that is
not one of them — so it goes in through `with-generator-executable` in both
compositions, built from this tree. What the pair is still being compared on is
the `go` half, which is the one added two different ways.

Every call is checked and every failure reported rather than stopping at the
first, and each message names the module function that broke: *it works from the
image and not from the file* is the finding, not a detail.

It runs on the **engine's own platform only**. What varies per platform is the
executable, and `image-contract` already builds and checks the image on each
platform this project publishes for; what this adds is that the module's calls
compose into a working image, which is not a property a second architecture can
disagree about.

### The pipeline module is checked like any other Go module here

`dagger call pipeline-ci` runs the standard pipeline over
[`.dagger/`](.dagger/), and it is in `ci`. It is a fourth call for the reason
[`IrCi`](.dagger/main.go) is a second and
[`CompanionCi`](.dagger/companion.go) a third: a nested `go.mod` is where `go
test ./...` stops. Until it existed, the module that checks everything else in
this repository was the one Go module nothing checked.

What made that worth fixing rather than noting is `cli-surface`. Its reading of
the CLI's constants lives in [`.dagger/internal/surface/`](.dagger/internal/surface/)
**precisely so that it can be tested** — the pipeline's own `package main`
imports the generated Dagger client, whose `init` panics without a session, so a
test beside it cannot run under plain `go test`, exactly as in the companion.
Without this stage those tests would run on a contributor's machine and nowhere
else, and a drift guard nothing exercises is a drift guard nobody finds out has
stopped working.

### The default image tag is the moving major tag

`New`'s `version` argument defaults to **`v0`**, the moving major tag, and not
to a full version constant generated at release time (#104).

A major tag follows every release within its major by the [tag
scheme](docs/container/SPEC.md#tags-and-what-pinning-one-buys) (#59), so the
default cannot go stale. Nothing is generated at release time, no CI check
asserts that a constant equals the git tag, and the release runbook grows no
step that could be forgotten. A caller who pins nothing picks up every fix in
the major version without editing anything — which is what a moving tag is for,
and what the container contract already recommends to a derived Dockerfile.

### What pinning the module ref buys, and what it does not

Two version numbers are in play, and neither pins the other.
`github.com/Zaba505/cpybkc/<dir>@v0.3.1` pins the module's *source* — the
constructor's defaults, and the composition its methods perform — at the commit
that tag names. Which image that source pulls is decided when it runs, by the
`version` argument, whose default resolves through a tag that moves. A pinned
module ref therefore buys a module that behaves the same way every time, and
says nothing about the bytes of the CLI it drives.

What the two do agree on is the major version, and that is not an accident: a
module published at a `v0.x.y` ref defaults to the image's `v0`. Pinning the ref
pins which major of the image you get, and that is exactly the range over which
the [compatibility
guarantees](docs/container/SPEC.md#compatibility-guarantees) hold. Anything
narrower than a major is the caller's explicit act — `v0.3.1` names one release,
and a digest names the bytes.

### Skew between the module and the image is supported within a major

A module from one release driving an image from another is a **supported
combination** for as long as both are inside one major version of the image, and
what bounds it is the container contract's compatibility guarantees rather than
anything the module promises on its own. Across a major version it is not
supported, and the default cannot produce it: `v0` does not cross into `v1`.

The bound is narrower than the whole contract, which is what makes it worth
leaning on. The module drives the image through four things, and all four are in
the covered table: the plugin directory it copies a generator into, the
entrypoint it passes arguments to, the user that has to be able to execute what
was copied, and the platform set. Everything else it reads off the container
rather than assuming — the working directory in particular, which the contract
explicitly does **not** cover. A module that had copied more of the contract
into itself would have that much more to hold across the skew this default
permits, so the discipline that keeps the module small (#61) is the same
discipline that makes the moving default safe.

One level down, this project refuses the analogous skew, and the difference is
worth stating because the prior art this module follows does not state it. A
later `WithGenerator` pulls the generator matching the CLI already composed in,
because a generator from one release beside a CLI from another is a combination
nobody tested and defaulting to it silently is how somebody ends up in it (#63).
That is a rule about two arguments that can carry different strings, and the fix
is to carry one string forward. The module ref and the image tag are not two
spellings of one version, so there is no string to carry; and the tag that *is*
carried forward to the generator is the same `v0` the CLI came from. The pair
#63 refuses is one this default cannot construct.

### Why the generated constant lost

It was a real option, and it buys something this one does not: a caller who pins
nothing gets a reproducible build, and one version number then names one tested
combination of module and image.

It costs release-time code generation, a CI check that the constant equals the
git tag, a runbook step — and it introduces a failure the moving tag does not
have. A constant nobody regenerated is stale, and stale is silent: generation
succeeds, against an old CLI, and nothing reports it. The check would have been
the only thing standing between that failure and a user, which is the argument
against the option that needs it. A moving default fails the other way round:
the only way it can fail is a change inside a major version that the module did
not expect, and that breaks the next run loudly rather than the next quarter's
quietly.

Reproducibility is not lost by this, it is made explicit — and the escalation on
the argument is where a caller learns how to ask for it. A digest is the only
reference that pins bytes, which is the position
[`docs/container/SPEC.md`](docs/container/SPEC.md#tags-and-what-pinning-one-buys)
takes about tags and
[`docs/plugin/SPEC.md`](docs/plugin/SPEC.md#also-out-of-scope) takes about
plugin distribution. Nothing else claims to.

### What the constructor says

The escalation belongs on the argument and not only here, because a caller reads
`dagger call --help` and never opens this file. `New`'s `version` argument
carries it (#61), and a change to the default is a change to both:

```go
// Version is the tag of the published cpybkc image to run.
//
// It defaults to the moving major tag "v0", which follows every release in
// that major version: a caller who passes nothing keeps up with fixes and
// stays inside the base-image contract's compatibility guarantees. Escalate
// deliberately — "v0" follows releases, "v0.3.1" pins one release, and only a
// digest pins the bytes. A digest is not a tag, so it goes to the container
// override argument rather than here.
```

The default is spelled as a `+default="v0"` pragma rather than as a zero-value
check, so that `dagger call --help` prints it: a default a caller cannot see is
one they have to read source to learn. The container override the last sentence
points at is named **`image`**, because it names the thing rather than a
Dockerfile verb, and it reads as the noun it is at the call site —
`--image=$(…)`. Both were #61's to settle; what was settled before it is the
value and what the comment says about it.

### #65 is closed rather than left open

#65 asks for the default tag to be generated from a version constant written at
release time, and for CI to fail when it does not equal the git tag. With `v0`
as the default there is no constant to generate and no invariant left to
enforce: the check would compare a tag that follows releases against the release
it has just followed, and would pass by construction. A check that cannot fail
is not a weaker check — it is a claim that something is being verified. So the
story is closed as unnecessary rather than carried against a premise this
decision removed.

What survives of it is the one thing the default depends on: that every release
publishes the moving major tag. Since #185 the tag family is [derived and
published by the shared
pipeline](docs/container/SPEC.md#tags-and-what-pinning-one-buys), so
`dagger call tag-scheme` no longer says it — that check covers only [whether a
commit is a release at
all](#whether-a-commit-is-a-release-is-a-function-not-a-step). What says it now is
`release` itself: after the push it reads back the references the publish
returned and requires a stable release to have moved more than the version tag
and a prerelease to have moved nothing, so a family that stopped moving the major
tag is a release that goes red. If that ever stopped being published, this
decision is what would have to be reopened, rather than the default quietly
patched at the call site.

## The conformance corpus

[`testdata/conformance/`](testdata/conformance/) is a set of small files with the
right answer written down: a layout and its copybooks, the IR they resolve to,
the bytes of a file laid out that way, and the values those bytes decode to. The
format is [`docs/conformance/SPEC.md`](docs/conformance/SPEC.md) — a generator
author in another language implements it, which is the test
[`docs/CONVENTIONS.md`](docs/CONVENTIONS.md), *What belongs here*, applies — and
the corpus's README is what stays with the corpus: which entries there are, what
each was derived from, and how to add one.

```sh
go test ./internal/conformance/...
```

That generates Go for every entry with `cpybkc-gen-go` built from the tree,
compiles it, reads each entry's bytes with it, writes those records back out with
it, reads that file too, and holds both answers against what the entry says.
`dagger call ci` runs it too, like every other test; the call above is for
narrowing down what it reported.

Being an ordinary test is what makes it a gate on every platform in the CI
matrix: the matrix is a matrix of `dagger call ci`, and a conformance job of its
own would be a second gate that a platform added to the matrix would silently not
carry. What it is not is a check of the bytes the writer produced —
[`docs/ir/SPEC.md`](docs/ir/SPEC.md)'s *Writing a file* makes byte identity a
claim about a record and not about a file, and
[`docs/conformance/SPEC.md`](docs/conformance/SPEC.md)'s *Why the writing
direction is checked by reading* is the rest of the argument.

It exists because cpybkc is strictly a generator: nothing here reads a data file
except the code a generator emitted, so nothing else holds two generators in two
languages to one reading of one descriptor. Adding an entry is four files and a
citation, and the README says how.

## Specs

Six of this project's interfaces are built against from outside it — the command
line, the file layout format, the resolved IR, the plugin CLI contract, the
container base-image contract and the conformance corpus format — and each is
far harder to change than the code behind it. Each has a `SPEC.md` under
[`docs/`](docs/), linked from [README.md](README.md), which is the only list of
them.

[`docs/CONVENTIONS.md`](docs/CONVENTIONS.md) is what those six agree on: the
conformance language, the section set every spec carries, how sources are cited
and how requirements are traced back to stories. It is defined there once and
referenced, never restated — the same argument this file already makes about the
pipeline and the lint configuration, applied to the word **MUST**.

### Why `docs/` and not beside the code

[`cobol-go`](https://github.com/Zaba505/cobol-go) puts `codec/SPEC.md` next to
package `codec`, and it is the model these specs follow in every other respect.
It is not followed here because five of the six specify things that are not Go
packages: a command line, a text file format, a CLI contract for executables
that may be shell scripts, an OCI image, and a directory of test fixtures.
Package-adjacency would have conjured an empty `container/` package into
existence to hold one markdown file, and would have
scattered six documents that are peers — a reader comparing what the plugin
contract promises against what the image provides should not have to know the
package tree to find both.

Each spec gets a directory rather than a bare file because they grow supporting
material: the layout format's published schema belongs beside the layout spec,
not in a second place that has to be kept in step with it.

### Changing a spec

Read `docs/CONVENTIONS.md` first; it is short, and it is what a reviewer will
check against. Then:

- Keep the section set. A spec missing `Out of Scope` is a spec that will have
  the same exclusion proposed to it every six months.
- Fill a stub heading and its `<!-- -->` brief goes in the same change, along
  with the *Mapping to Stories* row. A section with prose and its scaffolding
  comment still above it is a change somebody stopped halfway through.
- Cite, do not restate. Anything true of COBOL source or of byte-level field
  layout is `cobol-go`'s to say, and a second copy here is a second thing to be
  wrong.
- A new spec is a new row in `README.md`. There is one index; keep it complete.

`dagger call ci` does not read `docs/` — it is fmt, vet, lint and
`go test -race`, and it will pass on a documentation change without having
looked at it. Run it anyway, because a change that claims to be docs-only and is
not should fail in the same place as everything else, but do not mistake it for
review. What checks a spec is a reader asking whether every link resolves from
the file it is written in, whether every **MUST** names who it binds, and
whether anything is restated that could have been cited.

Mechanising the first of those — link resolution and the 80-column wrap — would
be welcome. It arrives as another function on the root Dagger module, called by
another `dagger call`, for the reason
[`.github/workflows/ci.yaml`](.github/workflows/ci.yaml) already gives: raw
steps beside it would be a second definition of what this repository checks.
