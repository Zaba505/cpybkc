# The Base-Image Contract

## Overview

Adding a generator to cpybkc means building an image: a multi-stage Dockerfile
that compiles the generator, copies it onto a directory already on `PATH`, and
inherits the cpybkc CLI as its entrypoint. That is the whole extension
mechanism, and it is why this project has no plugin registry, no lockfile, no
OCI fetching and no resolution protocol — `COPY` and `FROM` already are one.

The consequence is that the published image is a **public contract**, not just a
convenient way to ship a binary. A Dockerfile in somebody else's repository
names a path in it, and that path cannot move without breaking their build. This
document is what those Dockerfiles are entitled to rely on, and — just as
importantly — what they are not.

It is the deployment half of the [plugin contract](../plugin/SPEC.md). That
document says a generator is discovered as `cpybkc-gen-<name>` somewhere on
`PATH`, which is true on a laptop with no container in sight. This one says
which directory is on that `PATH` inside the image, who owns it, and as which
user the process reading it runs. How a plugin is invoked belongs there; where
it goes belongs here.

### Scope

In scope: what the published image guarantees to an image built `FROM` it — the
directory a generator is copied into, the entrypoint, the user and UID the
process runs as and the guidance for overriding it with
`--user $(id -u):$(id -g)`, the paths the IR schema ships at and what a
consumer may do with them, whether a shell is present and what follows if it is
not, the tags a derived image may pin to, and which of these are covered by a
compatibility guarantee and which are implementation detail that may change
without notice.

Out of scope, with reasons, in [Out of Scope](#out-of-scope).

### Governing sources

- **OCI Image Format Specification** — normative for what an image is, and so
  for what a guarantee about one can even be made about. It fixes the terms this
  document uses for layers, manifests and platforms.
  <https://github.com/opencontainers/image-spec/blob/main/spec.md>
- **OCI Image Configuration** — normative for `Entrypoint`, `Cmd`, `User`, `Env`
  and `WorkingDir`, which are precisely the fields a derived image inherits and
  that this contract makes promises about.
  <https://github.com/opencontainers/image-spec/blob/main/config.md>
- **Dockerfile reference** — the reference for the `FROM`/`COPY --from` form the
  worked example uses, and for how a derived image's `USER` and `ENTRYPOINT`
  interact with the base image's.
  <https://docs.docker.com/reference/dockerfile/>

> **Ambiguity:** the OCI specification defines the artifact; the Dockerfile
> reference describes one widely-used builder for it, and is not a standard.
> Where they differ this document states the guarantee in OCI terms — those are
> what a Podman or Buildah user gets too — and treats Dockerfile syntax as the
> example rather than the promise.

### Conformance language

**MUST**, **MUST NOT**, **SHOULD** and **MAY** are normative requirements on the
published cpybkc image and on any image built `FROM` it, interpreted as
described in [CONVENTIONS.md](../CONVENTIONS.md). Everything else is
descriptive.

## The plugin directory

The plugin directory is **`/usr/local/bin`**, and it is on `PATH` (#55).

That is the whole of the promise a derived Dockerfile depends on, and it is
worth saying what each half of it buys. The path is what a `COPY` line writes
to; the `PATH` membership is what makes the copied file reachable by the name a
manifest asks for. Either half alone is useless, which is why this is one
requirement rather than two facts in different sections.

The image **MUST** set `Env` so that `PATH` contains `/usr/local/bin`, and the
directory **MUST** exist in the base image even when it is empty, so that a
`COPY` into it never depends on the builder creating it. A derived image **MUST
NOT** remove it from `PATH`, and **MUST NOT** shadow it with an earlier `PATH`
entry holding an executable of the same name: the plugin contract's rule that
[the earliest `PATH` match wins](../plugin/SPEC.md#discovery) is exactly what
makes that a way to substitute a generator silently.

An executable copied there **MUST** be executable by the [image's
user](#the-user), and **MUST** be a statically linked native executable for the
image's platform. It **MUST NOT** be a script: there is [no
shell](#shell-or-no-shell) and no interpreter, so a `#!` line names a file that
does not exist and the generator fails to start with an error naming the
interpreter rather than the plugin. The plugin contract deliberately keeps [a
shell script with `chmod +x`](../plugin/SPEC.md#discovery) a first-class plugin;
that freedom is real on a host and is spent inside this image, and what buys it
back is an image with nothing in it to run.

**Stability.** `/usr/local/bin` is covered by the [compatibility
guarantees](#compatibility-guarantees) below: it does not move within a major
version, and the release that moved it would ship both directories for a full
minor release first. It is stated here as well as there because a path in a
stranger's Dockerfile is not something they should have to go looking for a
guarantee about.

`/usr/local/bin` is chosen over a `/opt/cpybkc/plugins` precisely because it is
not cpybkc-specific. It is the conventional destination for a locally installed
executable on a Unix-alike, it is what a `COPY --from=build` line reads
naturally against, and a reader who has never seen this document guesses it
correctly. A project-specific directory would have been equally stable and would
have taught every adopter one more thing.

### Why cpybkc's own generator is not in the base image

The base image holds **one** executable in the plugin directory — the cpybkc CLI
— and **no generator** (#55). `cpybkc-gen-go` (#48–#53) is this project's own
generator and reaches a user the way a stranger's does: as an image built `FROM`
the base, adding one executable at the path this section names and changing
nothing else.

A base image carrying its own generator would publish the same bytes and would
quietly stop testing anything. A generator that is on `PATH` because the build
put it there is not a consumer of this contract; it is a private arrangement
that resembles one. If it needed a path this document does not promise, an
entrypoint edit, or a shell in the final stage, nobody here would find out — the
first person to find out would be a stranger, at their own `docker build`,
against a mechanism that had never been used.

So the mechanism has a user before it has any, and the cost is one extra image
reference in a consumer's Dockerfile.

### The CLI's own path is not part of the contract

Where the `cpybkc` executable itself lives inside the image is **implementation
detail**. A derived image reaches it through the [entrypoint](#the-entrypoint),
never by path.

Keeping it out is deliberate: the image is built by a shared pipeline archetype
with its own opinion about where a binary goes (#55), and pinning the CLI's path
here would make a promise to strangers out of a detail of this repository's
build. Nothing a derived image legitimately does requires knowing it.

## The entrypoint

The image's `Entrypoint` is the cpybkc CLI, and its `Cmd` is empty (#55). The
arguments a caller passes to `docker run` are therefore cpybkc's arguments:

```console
$ docker run --rm -v "$PWD:/src" -w /src ghcr.io/zaba505/cpybkc:v0 <arguments>
```

Which arguments those are is the CLI's own documentation and not this
document's. What this document promises is that they arrive at the CLI
unaltered, and that nothing sits between them and it.

A derived image **MUST NOT** replace or clear `Entrypoint`. That is the one edit
which turns a derived image into a different program wearing cpybkc's
filesystem: the plugin it added would no longer be run by cpybkc, and the `FROM`
would be doing nothing but supplying a base. A derived image **MAY** set `Cmd`,
which is how an image supplies default arguments while leaving a caller free to
override them.

A derived image **MUST NOT** rely on `Entrypoint` having any particular *value*,
only on its behaviour: it accepts cpybkc's arguments. The array itself is
[implementation
detail](#the-clis-own-path-is-not-part-of-the-contract), which is what allows
the CLI to move without a major version.

## The user

The image runs as UID **65532**, GID **65532** — `User` is the literal string
`65532:65532` (#55). It is not root, and there is no `/etc/passwd` entry for it.

### Why the UID is pinned rather than allocated

The number is part of the contract, not an artifact of whichever `useradd` ran
last. Three things need it and none of them can ask the image what it is:

- A derived Dockerfile writes `COPY --chown=65532:65532` to hand ownership of a
  copied plugin to the runtime user. `--chown=cpybkc` would need a name the
  image has no passwd file to resolve.
- A Kubernetes `securityContext` writing `runAsUser: 65532`, or a policy
  admitting only known non-root UIDs, is written against a number in a manifest
  a long way from this repository.
- A caller reasoning about the ownership of files that appear in a bind mount is
  reasoning about a number, because a number is all their host kernel ever sees.

An allocated UID would be a value that could differ between two rebuilds of the
same tag, which is precisely the kind of change [a published version
tag](#tags-and-what-pinning-one-buys) promises does not happen. 65532 is the
conventional non-root UID for a distroless-style image; it is chosen for being
the number other people's tooling already expects.

### What it means for ownership

The [plugin directory](#the-plugin-directory) is owned by 65532:65532 in the
base image. A derived image copying a plugin in **SHOULD** use
`--chown=65532:65532` and **MUST** ensure the result is readable and executable
by that user; a world-readable, world-executable file satisfies this without the
`--chown`, which is what makes the omission a latent bug rather than an
immediate one.

Files cpybkc writes are created by the process, so they are owned by whichever
UID the container is actually running as — 65532 unless a caller says otherwise.

### Writing files a caller can read

Generated output that lands in a bind mount is owned by the UID that wrote it,
and a host user who is not 65532 then owns none of it. Depending on the mount,
they may be unable to edit or delete it either.

A caller **SHOULD** therefore run as themselves:

```console
$ docker run --rm --user "$(id -u):$(id -g)" -v "$PWD:/src" -w /src \
    ghcr.io/zaba505/cpybkc:v0 <arguments>
```

This is supported, and it is the recommended invocation whenever output is
written into a mount. The image **MUST NOT** require its own UID: nothing in it
is readable only by 65532, no directory a run needs to write is owned only by
it, and no code path looks the running user up in a passwd file it would not
find. An overridden UID is an ordinary configuration and not a workaround.

The default exists for the case where no host user is involved — a CI runner, a
Kubernetes job, a Dagger pipeline — where running as a pinned non-root UID is
what a policy wants to see.

## The IR schema in the image

A generator is handed a `cpybkc.ir.v1.Descriptor` and has to decode it. The
schema that says how is **in the image**, at two paths, so that a plugin author
building `FROM` it needs nothing else from this project — no download, no
network at build time, and no arrangement with this repository at all (#57):

| Path | What it is | Who it is for |
| --- | --- | --- |
| `/usr/local/share/cpybkc/ir.binpb` | the protobuf `FileDescriptorSet` describing `cpybkc.ir.v1.Descriptor` and the transitive closure of its imports | a generator whose build has no protobuf code generation in it, decoding dynamically |
| `/usr/local/share/cpybkc/proto/` | the `.proto` sources, each file at the path its protobuf package requires | a build that would rather run a compiler and generate bindings |

Both **MUST** exist, and they are not the same shape:
`/usr/local/share/cpybkc/ir.binpb` **MUST** be a **regular file**, and
`/usr/local/share/cpybkc/proto/` **MUST** be a **directory** whose every entry
is a directory or a regular file.

**Nothing under either path may be a symbolic link.** A `COPY --from` naming a
link copies the link, into an image where its target does not exist, and a
runtime resolving one inside a filesystem that holds nothing else resolves a
dangling name. So what a consumer copies out is bytes, whichever of the two
paths they name.

Both **MUST** be readable by the [image's user](#the-user) *and* by any UID a
caller overrides it with — for the directory, that means traversable as well, so
that reaching a file inside it is not a privilege the image's own user has and
a caller's does not. A generator run under `--user $(id -u):$(id -g)` reads the
same schema as one run under 65532, and a copy readable only by the image's own
user would make the recommended invocation fail on the one thing the image
exists to hand it.

`/usr/local/share/cpybkc/proto` is an **include root**. Every file under it sits
at the path its protobuf package requires, so a compiler is pointed straight at
the directory:

```console
$ protoc -I /usr/local/share/cpybkc/proto --python_out=. cpybkc/ir/v1/ir.proto
```

That layout is the reason it is a directory and not one flattened `ir.proto`. A
`.proto`'s path is part of its identity — it is the name a `FileDescriptorProto`
carries and the string another schema's `import` resolves — so a file moved out
of its package's directory compiles into descriptors naming a path this project
does not publish.

### These are the release assets, not copies of them

The bytes at `/usr/local/share/cpybkc/ir.binpb` **MUST** be identical to the
`ir.binpb` asset attached to the matching release, and the files under
`/usr/local/share/cpybkc/proto/` **MUST** be identical to the contents of that
release's `ir-protos.tar.gz`. They are **two ways of getting one artifact, not
two artifacts.**

This is the guarantee that lets a build stop after the cheaper one. A pipeline
that already pulls the image reads the file out of it and never touches the
release page; a pipeline that does not want a container image downloads the
asset. Neither has any reason to fetch both, and neither has to diff them to
find out whether it should have. Two files that merely agreed today would make
that a gamble on nobody having changed one of the two recipes, so there is one
recipe: `dagger call ir-descriptor-set` produces the file the release attaches
*and* the file the image carries, and the pipeline compares them on every pull
request.

For the same reason the bytes are identical across the platforms of one release.
A `FileDescriptorSet` is a function of the schema and knows nothing about an
architecture, so `linux/amd64` and `linux/arm64` carry the same file and a build
that reads it out of either gets the same answer.

### What a derived image may do with them

A derived image **MAY** copy either path out — into a builder stage that runs
`protoc`, or into a distroless image of its own:

```dockerfile
FROM ghcr.io/zaba505/cpybkc:v0 AS cpybkc

FROM python:3.13
COPY --from=cpybkc /usr/local/share/cpybkc/ir.binpb /opt/ir.binpb
```

A derived image **MUST NOT** modify either in place. An image that rewrote the
descriptor set would be publishing a cpybkc whose shipped schema no longer
describes the descriptors its own CLI emits, and every generator in it would
decode against a contract the entrypoint does not implement. The files are
root-owned and mode `0644`, so the running user cannot do it by accident; the
requirement is on the build that could.

### Their size, which is not a guarantee

Descriptive, and stated because *"ships a protobuf descriptor set"* otherwise
means anything from two kilobytes to a hundred megabytes, which is the
difference between `COPY --from`-ing it into a distroless image without thinking
and not doing it at all: `ir.binpb` is roughly **five kilobytes** and the
sources are roughly **forty-five**, so the whole of
`/usr/local/share/cpybkc` is tens of kilobytes.

Those numbers are **not covered**. They move with the schema, and a release that
added a message would change both without changing anything a consumer depends
on. What is covered is that the paths are there, are regular files, are readable
and are the release's own bytes.

### Why both, when the descriptor set alone would do

The descriptor set is the one that serves the consumer with the least: a runtime
that can load a `FileDescriptorSet` — which is every protobuf runtime — needs no
compiler, no build step and no generated code. If only one form could ship, it
would be that one.

The sources ship as well because the consumer who *can* run a compiler is not
served by it. Generating bindings from a `.proto` gives them named types, their
language's own field accessors and their own build's error messages, and asking
them to reconstruct a `.proto` from a descriptor set to get there is asking them
to write a decompiler. The two are alternatives rather than a fallback pair, and
[`ir/SPEC.md`](../ir/SPEC.md#reading-a-descriptor-without-generated-code) is
normative for what each contains.

There is a third way to read a descriptor and it ships nothing, because it needs
nothing: cpybkc renders one as JSON on request, and the entrypoint is already in
the image. That is the [IR specification's](../ir/SPEC.md#a-descriptor-is-readable-by-a-person),
and it is a CLI flag rather than a file.

## Shell or no shell

**There is no shell in the image**, and no package manager, no `cp`, no `chmod`
and no libc. The base is `scratch` plus the files this document names (#55).

The consequence is a rule rather than a nuance, because it is the difference
between a Dockerfile that builds and one that fails on its second line:
**extension is `COPY`-only.** A stage built `FROM` the cpybkc image **MUST NOT**
contain a `RUN` instruction, and **MUST** do everything it needs with `COPY`,
`COPY --from`, `ENV`, `CMD`, `LABEL` and the other instructions that only edit
metadata.

The shell form of `RUN` is the one that bites first: it is implemented as
`/bin/sh -c`, and there is no `/bin/sh`. The exec form fails for a different
reason — the image is not empty, so an exec-form `RUN` naming an executable on
`PATH` would in fact execute, but every executable in the image is either a
generator or the CLI, whose [path is not part of this
contract](#the-clis-own-path-is-not-part-of-the-contract). There is no `cp`, no
`chmod`, no `mkdir` and no package manager, so nothing a build step would
plausibly want to `RUN` is there to be run.

Everything that needs a shell belongs in an earlier stage, which is a full
distribution image and may do as it likes. That is not a restriction on what a
generator may be built with; it is a restriction on where the building happens,
and the multi-stage form the [worked
example](#worked-example-adding-a-generator) uses is how an image in this model
is written anyway.

Two consequences are worth stating outright, because each has been discovered
the hard way by somebody:

- A plugin **MUST** be statically linked, as [the plugin
  directory](#the-plugin-directory) requires. For a Go generator that means
  `CGO_ENABLED=0`; a dynamically linked executable finds no loader and no libc,
  and fails with `no such file or directory` naming a file that is plainly
  there.
- `docker run --entrypoint sh` does not work, and neither does `docker exec`
  into a running container. Debugging is done by running the CLI with different
  arguments, or against an image built `FROM` this one with a busybox copied in
  for the purpose.

One directory in the image holds no file and is worth naming anyway: there is a
writable temporary directory, because cpybkc writes [each invocation's
descriptor](../plugin/SPEC.md#the-descriptors-location-and-lifetime) into a
directory it creates under one, and hands each generator [an empty output
directory](../plugin/SPEC.md#the-output-directory) it created the same way. It
is **not covered** — its path, its mode and its existence are implementation
detail like everything else in the filesystem that is not named above, and a
derived image writing into it by path would be depending on something that may
change in a patch release. It is mentioned only so that "`scratch` plus the
files named above" is not read as a promise that the image cannot write
anywhere at all.

Keeping the shell out is the same decision as having no plugin registry: the
image's contents are exactly the executables somebody deliberately put there,
and there is nothing else in it to run, exploit or depend on by accident.

## Tags and what pinning one buys

A derived Dockerfile names a tag in its `FROM` line, so the tag is as much a
part of this contract as the paths are. Four tags are published (#59), and a
digest is the fifth way to name the image:

| Reference | Example | Moves? |
| --- | --- | --- |
| Full version tag | `v0.2.0` | Never |
| Minor tag | `v0.2` | On each patch release of that minor |
| Major tag | `v0` | On each release within that major |
| Rolling tag | `latest` | On each release |
| Digest | `@sha256:…` | Never, by construction |

A published full-version tag **MUST NOT** be repointed at a different manifest
after it is published — not for a rebuild, not for a base-image refresh, not to
correct a broken release. A release that has to be corrected gets a new version
number. That is the promise which makes `v0.2.0` mean something across a
rebuild: two builds naming it resolve to the same digest, forever, or the tag
was a lie.

The other three **MUST** move, and moving is what they are for. `v0` picks up
every fix in the major version and, by the [guarantee
below](#compatibility-guarantees), keeps every path this document names; it is
the right pin for a derived image that wants security updates without a
Dockerfile change, and it is what the [worked
example](#worked-example-adding-a-generator) uses. A **prerelease** —
`v0.3.0-rc.1` — publishes its own full version tag and **MUST NOT** move any of
the other three, because a release candidate is not a fix anybody consented to
be given (#59).

The three that move follow **the release being published**, not the highest
version ever published. For releases cut in ascending order — which is how this
project cuts them — the two are the same thing. They come apart only if a
backport is released after the version that supersedes it, which would land `v0`
and `latest` back on the older image; that is a constraint on the releaser
rather than a licence for a consumer to see the moving tags go backwards, and
the full version tag and the digest are unaffected either way.

A digest is the only reference that pins the bytes rather than a promise about
them, and it is what to use when reproducibility is the requirement — the
position the plugin contract takes under [Plugin
distribution](../plugin/SPEC.md#also-out-of-scope). Pinning a digest fixes the
generators in the image along with cpybkc itself, which is the whole reason this
project has no lockfile.

### What a tag carries besides the image

Every published tag resolves to a **multi-platform index**, and two things are
attached to what it resolves to (#58), both of which a consumer can check
without any prior arrangement with this project:

- a **signature**, over the published index and over each per-platform manifest
  beneath it, so the manifest a runtime actually pulls is signed and not only
  the one the tag named;
- **attestations** — a provenance statement and an SBOM — attached to the
  published index digest and to that alone. They are statements about the
  release, and the release is the index; a per-platform manifest carries none of
  its own, and looking for one there finds nothing.

Signing is **keyless**: there is no cpybkc public key to obtain or trust. The
signing identity is [this repository's release
workflow](../../.github/workflows/release.yaml), certified for the length of one
run from the OIDC token the CI provider mints and recorded in a public
transparency log, so verification asks *what built this image* and gets an
answer anybody can check. Verification therefore names that workflow as the
certificate identity and the CI provider as the OIDC issuer; a verification
passing neither asks only whether *somebody* signed the image, which is not a
question worth the command.

Nothing is ever attached to a **tag**, only to a digest. An attestation about a
name that moves would say nothing, which is why verifying a moving tag is
verifying whatever it resolves to at that moment.

The SBOMs are **one SPDX 2.3 document per executable per platform** (#58), not
one per image. Each document is tied to the SHA-256 of the binary it describes,
so a single document for an index spanning two platforms would name one of those
binaries and be wrong about the other — and a consumer matching an advisory
against it would be matching against a build they are not running.

### Verifying a signature

Verification needs [cosign](https://github.com/sigstore/cosign) and nothing else
— no key to fetch, no account, and no prior arrangement with this project (#58).

Verify the signature on a full version tag:

```console
$ cosign verify \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    --certificate-identity 'https://github.com/Zaba505/cpybkc/.github/workflows/release.yaml@refs/tags/v0.2.0' \
    ghcr.io/zaba505/cpybkc:v0.2.0
```

Both flags carry the whole of what is being asked. A `cosign verify` without
them checks that *somebody* signed the image and says nothing about who, which
is not a question worth the command. The identity is the release workflow **at
the tag being verified**, so it names a different ref for each release — which
is why a moving tag is verified against a pattern rather than a literal:

```console
$ cosign verify \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    --certificate-identity-regexp '^https://github\.com/Zaba505/cpybkc/\.github/workflows/release\.yaml@refs/tags/v' \
    ghcr.io/zaba505/cpybkc:v0
```

Because the signature covers each manifest under the index, the same command
verifies the per-platform manifest a runtime on that platform actually pulls —
substitute its digest for the reference. That is the check a shop pinning a
digest in a `FROM` line wants, and it is the reason the signature is recursive.

The same two flags verify the attestations, with `--type` naming which of the
two is wanted:

```console
$ cosign verify-attestation --type slsaprovenance1 \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    --certificate-identity 'https://github.com/Zaba505/cpybkc/.github/workflows/release.yaml@refs/tags/v0.2.0' \
    ghcr.io/zaba505/cpybkc:v0.2.0

$ cosign verify-attestation --type spdxjson \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    --certificate-identity 'https://github.com/Zaba505/cpybkc/.github/workflows/release.yaml@refs/tags/v0.2.0' \
    ghcr.io/zaba505/cpybkc:v0.2.0
```

`--type spdxjson` returns every SBOM on the digest, one per executable per
platform. Which build a document describes is inside the document rather than in
how it was attached: its name and the SHA-256 on its subject package.

Both attestation commands take the **index** — a tag, or the digest that tag
resolves to. Asking a per-platform manifest for one finds nothing, and that is
the arrangement rather than a fault.

#### After mirroring to an internal registry

A shop that mirrors this image into a registry of its own can verify it there,
and the command does not change in the way people expect it to.

**The identity does not move with the image.** `--certificate-identity` and
`--certificate-oidc-issuer` name what *built* the release, not where it is
stored, so they hold the same values against a mirror as against `ghcr.io`.
Rewriting them to name the mirror would be asserting that the mirror built the
release, which is exactly the claim the signature exists to distinguish.

**Copy the signatures, not just the image.** A signature and an attestation are
ordinary objects in the same repository as the image, under tags derived from
its digest — `sha256-<digest>.sig` and `sha256-<digest>.att`. A mirror made by
pulling one tag and pushing it elsewhere copies the image alone and leaves both
behind, and the copy then fails to verify with nothing found. `cosign copy`
moves all of it, and a mirroring tool that copies *every* tag in the repository
does the same job, because those objects are tags:

```console
$ cosign copy ghcr.io/zaba505/cpybkc:v0.2.0 registry.internal/mirror/cpybkc:v0.2.0
$ cosign verify \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    --certificate-identity 'https://github.com/Zaba505/cpybkc/.github/workflows/release.yaml@refs/tags/v0.2.0' \
    registry.internal/mirror/cpybkc:v0.2.0
```

**The digest is what carries across.** A mirror that copies bytes preserves the
index digest, which is why everything above still applies: the signature and the
attestations are statements about that digest and about nothing else. Something
that *rebuilt* or repacked the image would produce a different digest, and
nothing this project signed would apply to it — that is a rebuild rather than a
mirror, however it is spelled in the pipeline that produced it.

**Signatures kept somewhere else.** Where a mirror cannot hold those sibling
tags — a registry that rejects the tag form, or a policy that keeps signatures
apart from images — `COSIGN_REPOSITORY` names the repository to read them from,
and the verification is otherwise identical:

```console
$ COSIGN_REPOSITORY=registry.internal/mirror/cpybkc-signatures cosign verify \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    --certificate-identity 'https://github.com/Zaba505/cpybkc/.github/workflows/release.yaml@refs/tags/v0.2.0' \
    registry.internal/mirror/cpybkc:v0.2.0
```

**Verifying without reaching the internet.** Keyless verification checks the
signing certificate against sigstore's public trust root and the signature
against a public transparency log. A keyless signature carries its own log entry
beside it, so `--offline` verifies against that instead of asking the log; the
trust root still has to be present, and `cosign initialize` is what fetches it
on a machine that has a network, before verification happens on one that does
not.

That this verification is *possible* is in scope here, because it is something a
consumer performs. How the signature comes to exist is not, and is under [Out of
Scope](#how-the-image-is-built-and-published).

## Worked example: adding a generator

A complete multi-stage Dockerfile that builds a generator cpybkc has never heard
of and copies it into the plugin directory. It is runnable as written, against
an empty build context:

```dockerfile
# syntax=docker/dockerfile:1

FROM golang:1.25 AS build
WORKDIR /src

COPY <<'EOF' go.mod
module example.com/cpybkc-gen-hello

go 1.24
EOF

COPY <<'EOF' main.go
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/Zaba505/cpybkc/irpb"
)

// The highest IR version this generator implements, and its own version. Both
// are named in the refusal below, because cpybkc never learns there was a
// mismatch and cannot compose that diagnostic on the plugin's behalf.
const (
	irVersion     = irpb.IrVersion_IR_VERSION_1
	pluginVersion = "0.1.0"
)

func main() {
	descriptor, out, greeting := "", "", "hello"

	args := os.Args[1:]
	for len(args) > 0 && args[0] != "--" {
		if len(args) < 2 {
			fail("%s: missing value", args[0])
		}
		flag, value := args[0], args[1]
		args = args[2:]
		switch flag {
		case "--descriptor":
			descriptor = value
		case "--out":
			out = value
		case "--opt":
			key, v, ok := strings.Cut(value, "=")
			if !ok || key != "greeting" {
				fail("--opt %s: unrecognised option", value)
			}
			greeting = v
		default:
			fail("%s: unrecognised flag", flag)
		}
	}
	if descriptor == "" || out == "" {
		fail("--descriptor and --out are both required")
	}

	b, err := read(descriptor)
	if err != nil {
		fail("%v", err)
	}

	// The version, off the wire, before the message is decoded at all: a
	// descriptor this generator does not implement is refused with no part of
	// it interpreted.
	if version := versionOf(b); version != irVersion {
		fail("descriptor IR version %d; cpybkc-gen-hello %s implements IR version %d",
			version, pluginVersion, irVersion)
	}

	var d irpb.Descriptor
	if err := proto.Unmarshal(b, &d); err != nil {
		fail("the descriptor is not a cpybkc IR message: %v", err)
	}

	name := filepath.Join(out, "hello.txt")
	if err := os.WriteFile(name, []byte(greeting+"\n"), 0o644); err != nil {
		fail("%v", err)
	}
}

// read is the descriptor's bytes, from the path or from standard input where
// the path is `-`. cpybkc always passes a real path, and a plugin accepts `-`
// anyway.
func read(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

// versionOf is the IR version the descriptor's bytes state, read without
// decoding the rest of the message. Field 1 is the version; anything else is
// skipped, and bytes that are not a message read as no version stated, which is
// a version this generator does not implement.
func versionOf(descriptor []byte) irpb.IrVersion {
	stated := irpb.IrVersion_IR_VERSION_UNSPECIFIED
	for rest := descriptor; len(rest) > 0; {
		number, kind, n := protowire.ConsumeTag(rest)
		if n < 0 {
			return stated
		}
		rest = rest[n:]
		if number == 1 && kind == protowire.VarintType {
			v, n := protowire.ConsumeVarint(rest)
			if n < 0 {
				return stated
			}
			stated, rest = irpb.IrVersion(v), rest[n:]
			continue
		}
		if n = protowire.ConsumeFieldValue(number, kind, rest); n < 0 {
			return stated
		}
		rest = rest[n:]
	}
	return stated
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
EOF

RUN go mod tidy \
 && CGO_ENABLED=0 go build -trimpath -o /out/cpybkc-gen-hello .

FROM ghcr.io/zaba505/cpybkc:v0
COPY --from=build --chown=65532:65532 --chmod=0755 \
     /out/cpybkc-gen-hello /usr/local/bin/cpybkc-gen-hello
```

Build it, and run it against a project whose `cpybkc.json` names the `hello`
generator and nothing else:

```console
$ docker build -t cpybkc-hello .
$ docker run --rm --user "$(id -u):$(id -g)" -v "$PWD:/src" -w /src \
    cpybkc-hello <arguments>
$ cat out/hello.txt
hello
```

The `--opt` half of the plugin contract works from here unchanged: an
`"options": {"greeting": "hei"}` in that generator's manifest entry is what
cpybkc turns into `--opt greeting=hei` on its command line, and `hello.txt` says
`hei` instead.

Five things in it are the contract rather than the example, and each one is a
line that would be wrong in a Dockerfile written from habit:

- The final stage contains **no `RUN`**. Every command ran in the `golang`
  stage, which has a shell; the stage built `FROM` the cpybkc image only copies.
  See [Shell or no shell](#shell-or-no-shell).
- `CGO_ENABLED=0` produces a static executable. Without it the build succeeds
  and the generator fails to start inside the image.
- The destination is `/usr/local/bin`, on `PATH`, and the filename is
  `cpybkc-gen-hello` — the name cpybkc searches for when a manifest asks for
  `hello`, per the [plugin contract](../plugin/SPEC.md#discovery).
- `--chown=65532:65532 --chmod=0755` hands the file to the [image's
  user](#the-user) as an executable. `COPY` otherwise preserves the source's
  ownership, which is root's in the build stage.
- `ENTRYPOINT` is untouched, so the image is still cpybkc — now with one more
  generator on its `PATH`.

The generator itself is deliberately small: it parses the argument vector it is
promised, refuses an option it does not know, reads the descriptor's IR version
before anything else in the message and refuses one it does not implement, and
writes one deterministic file beneath `--out`. Every one of those is required of
it by the [plugin contract](../plugin/SPEC.md), and none of them is required of
it by this one — which is the point. This document's whole involvement in the
generator is the two lines that put it on `PATH`.

### This example is built by the pipeline

`dagger call worked-example` extracts the Dockerfile above out of this file and
checks it, and CI runs that on every pull request (#54). So the example is
executable documentation rather than a transcription, and an edit that breaks it
fails on the pull request that made it.

It is checked rather than trusted because nothing else in this repository reads
it: this project's build, its tests and its own images would all pass on an
example that stopped building releases ago, and the first person to find out
would be an adopter, at the first thing they tried.

Two details of how it is checked bound what passing means. The **build stage is
handed to the builder exactly as committed**, with an empty build context, which
is what makes "runnable as written" something somebody measured rather than
remembered — a heredoc that no longer parses, a Go program that no longer
compiles, a module path that no longer resolves or a toolchain that has moved on
all fail there, and so does an executable that comes out dynamically linked. The
**final stage is replayed onto the image the pipeline just built** (#55): a
`FROM` line cannot name a container that exists only inside a pipeline, so its
`FROM` is required to name the published base and its `COPY` — the path, the
`--chown` and the `--chmod` this document wrote — is applied to that base
instead. What comes out is a real derived image, and it is checked against this
document the same way the base is: the filesystem is the base's plus exactly the
one file that was copied, and the entrypoint still answers as cpybkc.

Checking against the base the pipeline built rather than against the published
tag is the point. The tag is last release's image, so an edit that moved the
plugin directory would pass against it and break the first adopter to pull the
next release.

The check refuses what it cannot replay: an instruction in the final stage other
than the ones this document permits is an error naming it, rather than a line
that quietly goes unchecked. A worked example that grows an `ENV` teaches the
pipeline what an `ENV` means before CI accepts it.

## Worked example: reading the IR without generated code

The example above put a generator on `PATH` and said nothing about how it reads
what cpybkc hands it. This one is the other half, and it is the reason [the IR
schema](#the-ir-schema-in-the-image) is in the image at all: a generator that
decodes a descriptor using **only the `FileDescriptorSet` at
`/usr/local/share/cpybkc/ir.binpb`**. Nothing in its build context is generated
from cpybkc's schema, nothing in its `go.mod` comes from this repository, and it
never downloads anything — the schema arrives with the base image.

It is written in Go because the final stage has [no
shell](#shell-or-no-shell) and every plugin is a static executable, but nothing
below is Go-specific: the four calls are `decode a FileDescriptorSet`, `build a
registry`, `look up a message by name`, `decode into a dynamic message of that
type`, and every protobuf runtime has them. [`ir/SPEC.md`, *Reading a descriptor
without generated code*](../ir/SPEC.md#reading-a-descriptor-without-generated-code)
is normative for them.

```dockerfile
# syntax=docker/dockerfile:1

FROM golang:1.25 AS build
WORKDIR /src

COPY <<'EOF' go.mod
module example.com/cpybkc-gen-irdump

go 1.24
EOF

COPY <<'EOF' main.go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// Everything this generator knows about the IR comes out of the image it is
// running in. There is no import of cpybkc's Go module here and no ir.proto in
// the build context; the schema is the file below.
const (
	shippedDescriptorSet = "/usr/local/share/cpybkc/ir.binpb"
	descriptorMessage    = "cpybkc.ir.v1.Descriptor"

	// The one IR version this generator implements, by name. A generator built
	// against generated code compares an enum constant; one that decodes
	// dynamically has the value's name and nothing else, and the name is in the
	// descriptor set above.
	irVersion     = "IR_VERSION_1"
	pluginVersion = "0.1.0"
)

func main() {
	descriptor, out := "", ""

	args := os.Args[1:]
	for len(args) > 0 && args[0] != "--" {
		if len(args) < 2 {
			fail("%s: missing value", args[0])
		}
		flag, value := args[0], args[1]
		args = args[2:]
		switch flag {
		case "--descriptor":
			descriptor = value
		case "--out":
			out = value
		default:
			fail("%s: unrecognised flag", flag)
		}
	}
	if descriptor == "" || out == "" {
		fail("--descriptor and --out are both required")
	}

	// The schema, read out of the image.
	schema, err := os.ReadFile(shippedDescriptorSet)
	if err != nil {
		fail("%v", err)
	}

	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(schema, &set); err != nil {
		fail("%s is not a FileDescriptorSet: %v", shippedDescriptorSet, err)
	}

	// A registry of every file in it, and the one message a generator is
	// handed. Both come from the shipped bytes; neither needs a compiler.
	files, err := protodesc.NewFiles(&set)
	if err != nil {
		fail("%s does not build a registry: %v", shippedDescriptorSet, err)
	}

	found, err := files.FindDescriptorByName(descriptorMessage)
	if err != nil {
		fail("%s describes no %s: %v", shippedDescriptorSet, descriptorMessage, err)
	}

	message, ok := found.(protoreflect.MessageDescriptor)
	if !ok {
		fail("%s is not a message", descriptorMessage)
	}

	encoded, err := os.ReadFile(descriptor)
	if err != nil {
		fail("%v", err)
	}

	root := dynamicpb.NewMessage(message)
	if err := proto.Unmarshal(encoded, root); err != nil {
		fail("the descriptor is not a %s: %v", descriptorMessage, err)
	}

	// The version before anything else in the message, and a refusal rather
	// than a walk. That obligation binds a consumer that decoded dynamically
	// exactly as it binds one with generated code — more visibly, if anything,
	// since nothing here would have failed to compile against a newer IR.
	field := message.Fields().ByName("version")
	stated := field.Enum().Values().ByNumber(root.Get(field).Enum())
	if stated == nil || string(stated.Name()) != irVersion {
		fail("descriptor IR version %v; cpybkc-gen-irdump %s implements %s",
			root.Get(field), pluginVersion, irVersion)
	}

	// A descriptor is a flat list of nodes, and a record names itself through a
	// Names message. Reading either by field name is the whole of what the
	// descriptor set bought.
	report := &strings.Builder{}
	fmt.Fprintf(report, "ir version: %s\n", stated.Name())

	nodes := root.Get(message.Fields().ByName("nodes")).List()
	fmt.Fprintf(report, "nodes: %d\n", nodes.Len())

	for i := range nodes.Len() {
		node := nodes.Get(i).Message()

		kind := node.WhichOneof(node.Descriptor().Oneofs().ByName("kind"))
		if kind == nil || kind.Name() != "record" {
			continue
		}

		record := node.Get(kind).Message()
		names := record.Get(record.Descriptor().Fields().ByName("names")).Message()

		fmt.Fprintf(report, "record: %s\n",
			names.Get(names.Descriptor().Fields().ByName("original")).String())
	}

	name := filepath.Join(out, "ir.txt")
	if err := os.WriteFile(name, []byte(report.String()), 0o644); err != nil {
		fail("%v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
EOF

RUN go mod tidy \
 && CGO_ENABLED=0 go build -trimpath -o /out/cpybkc-gen-irdump .

FROM ghcr.io/zaba505/cpybkc:v0
COPY --from=build --chown=65532:65532 --chmod=0755 \
     /out/cpybkc-gen-irdump /usr/local/bin/cpybkc-gen-irdump
```

Build it and run cpybkc against a project whose `cpybkc.json` names the
`irdump` generator, and `ir.txt` names the IR version and every record in the
descriptor:

```console
$ docker build -t cpybkc-irdump .
$ docker run --rm --user "$(id -u):$(id -g)" -v "$PWD:/src" -w /src \
    cpybkc-irdump <arguments>
$ cat out/ir.txt
ir version: IR_VERSION_1
nodes: 42
record: CUSTOMER-RECORD
```

Three things in it are the contract rather than the example:

- `/usr/local/share/cpybkc/ir.binpb` is a path, written into the program, with
  no fetch behind it. That is what [the IR schema in the
  image](#the-ir-schema-in-the-image) guarantees, and it is why this Dockerfile
  has no `ADD`, no `curl` and no build argument naming a release.
- The file is opened by a process running as whatever UID the caller passed.
  The `--user "$(id -u):$(id -g)"` above is the recommended invocation and it is
  an ordinary one here too, because the shipped file is world-readable.
- Nothing past the argument parsing knows the schema at compile time. Swap
  `--python_out` for `go build` and the same four calls are the same four calls;
  the only thing that would change is which of the two shipped forms the build
  reaches for.

### This example is built and run by the pipeline

`dagger call worked-example` checks this Dockerfile exactly as it checks the
[first one](#this-example-is-built-by-the-pipeline) — the build stage handed to
the builder as committed, the final stage replayed onto the base image the
pipeline just built — and then does one thing more: it **runs the generator**
inside that derived image, as an overridden UID, against a descriptor stating an
IR version and nothing else, and requires the output to name the version.

That last step is the only thing that can tell this example from a program which
merely mentions the path. Everything else about it — two stages, one static
executable, one `COPY` into the plugin directory — would pass on a generator
that never opened the file. The claim being checked is that the shipped
descriptor set is *sufficient*: the version's name, `IR_VERSION_1`, appears in
`ir.binpb` and nowhere in the bytes the generator was handed, so output carrying
it is output that read the image's own copy of the schema.

## Compatibility guarantees

**Covered.** Within a major version of the image each of these holds, and a
change to any of them is a breaking change:

| Guarantee | Value |
| --- | --- |
| [The plugin directory](#the-plugin-directory) | `/usr/local/bin`, on `PATH`, existing and owned by the image's user |
| [The entrypoint](#the-entrypoint) | Is the cpybkc CLI, and takes its arguments |
| [The user](#the-user) | UID 65532, GID 65532, non-root, overridable |
| [The IR `FileDescriptorSet`](#the-ir-schema-in-the-image) | `/usr/local/share/cpybkc/ir.binpb`, a world-readable regular file, byte-identical to the release asset |
| [The IR `.proto` sources](#the-ir-schema-in-the-image) | `/usr/local/share/cpybkc/proto/`, a world-traversable include root of world-readable regular files, byte-identical to the release archive's contents |
| [Shell or no shell](#shell-or-no-shell) | Absent; extension is `COPY`-only |
| [Platforms](#why-the-platform-set-is-the-two-it-is) | `linux/amd64` and `linux/arm64` |
| [Tags](#tags-and-what-pinning-one-buys) | A published full-version tag never moves |
| [Signatures](#what-a-tag-carries-besides-the-image) | The published index and each manifest beneath it are signed; the index digest carries provenance and an SBOM |

**Not covered**, and explicitly implementation detail. Depending on any of it is
depending on something that may change in a patch release, with no notice:

- The base image, and everything in the filesystem other than what is named
  above — its existence, its contents and its paths, including the writable
  temporary directory.
- The path of the `cpybkc` executable itself, and the literal value of
  `Entrypoint`.
- The value of `PATH` beyond its containing `/usr/local/bin`, and the value of
  any other environment variable.
- `WorkingDir`, and the existence of any directory a caller mounts over. A
  caller passes `--workdir`, or paths, or both; cpybkc resolves a manifest's
  relative paths against the manifest rather than against a working directory,
  so where a project is mounted is the caller's arrangement and not a promise
  this image makes.
- Layer count, layer ordering, image size, build timestamps, and every other
  label or annotation on the manifest.
- Which UID owns a file that is not in the plugin directory.
- The **size** of either IR artifact, and the number of files under
  `/usr/local/share/cpybkc/proto/`. Both move with the schema; [the figures
  given](#their-size-which-is-not-a-guarantee) are a description of one release
  and not a promise about the next.
- Anything else appearing under `/usr/local/share/cpybkc/`. The two paths named
  above are the contract; a third file arriving beside them is not one to depend
  on until this document names it.

### Why the platform set is the two it is

The covered platform set is the deliberate part of that table. An earlier draft
of this document had a third entry, `linux/s390x`, on the reasoning that files
composed of COBOL copybook records come from mainframes and that a shop with
those files runs containers on Linux on Z. That reasoning was about the *files*
and this table is about the *machine cpybkc executes on*, and the two are not
the same place (#55, #56).

cpybkc runs on developer machines. It is a code generator for a modern codebase
that integrates with an existing mainframe ecosystem through a file format a
copybook defines in part; it is not deployed to the mainframe, and nothing about
using it requires it to run on one. So `linux/amd64` and `linux/arm64` are the
platforms developers and their CI actually have, and a third leg would have been
a published promise with no consumer behind it — which #56 said outright when it
was closed as not planned.

What this retires is a build target, and nothing else. The [byte-order
fork](https://github.com/Zaba505/cobol-go/blob/main/codec/SPEC.md) the generated
code is written against is a property of the **data being read**, not of the
host CPU: big-endian files stay fully in scope, must decode correctly on both
platforms above, and are tested there. A big-endian *host* is what is out of
scope.

Any platform beyond those two is explicitly outside this guarantee, which is the
same position `avroc` takes for the same set. Adding one later is not a breaking
change and needs no new major version; dropping one of these two is, and would
go through [How a covered thing would change](#how-a-covered-thing-would-change)
like any other covered value.

The *set* is covered; the machinery that fills it is not. Which platforms a
published index carries is something a consumer reads out of the index and
builds against, so it is a guarantee. How each one is cross-compiled or
assembled is [out of scope](#how-the-image-is-built-and-published) like the rest
of the build.

### How a covered thing would change

Not by moving it. A covered path or value changes in a new major version, and
the transition **MUST** hold both forms simultaneously for at least one full
minor release of that new major: a moved plugin directory means both directories
exist and both are on `PATH`, with the old one deprecated in the release notes
and removed no earlier than the following minor release.

The overlap is the entire point. A derived Dockerfile is in a repository this
project cannot see, is built by a pipeline this project cannot warn, and fails
at `COPY` time with an error naming a path rather than a version. Giving it a
release in which both the old and the new form work is what turns that into a
deprecation notice somebody reads instead of a broken build somebody bisects.

## Out of Scope

### How the image is built and published

The build, the platform matrix, how each leg of it is cross-compiled or tested,
signing, provenance and SBOM generation are **not specified here**.

Reason: none of it is visible to a Dockerfile that says `FROM`. This document
describes what an image consumer may depend on; the machinery producing it
(#55–#59) can be replaced wholesale without any of those promises changing, and
a contract that described it would freeze an implementation by accident. What
*is* in scope is anything a consumer can verify — the tags, and the fact that a
signature exists for them.

### The plugin CLI contract

The `cpybkc-gen-<name>` naming convention, the argument vector, exit codes and
determinism are **not specified here**.

Reason: they are [`plugin/SPEC.md`](../plugin/SPEC.md)'s (#39), and they hold
with no container involved. Restating them here would imply the contract is a
property of the image, which would leave a plugin author running cpybkc from a
Go install with no document that applies to them.

### Also out of scope

- **The Dagger module** (#61–#65) that runs cpybkc for a caller. It is a
  convenience over this contract; what it needs to say, it says in its module
  comment and `dagger call --help`. Which tag it pulls when a caller names none
  — the moving major tag — and what pinning its module ref does and does not pin
  about the image are settled in
  [CONTRIBUTING.md](../../CONTRIBUTING.md#the-companion-dagger-module) (#104),
  which is where an argument about a convenience belongs.
- **The registry.** That the image is published to `ghcr.io` (#59) is a fact
  about where to find it, not a guarantee about what is inside it. A mirror
  serving the same digest satisfies this contract identically.
- **Base-image choice.** Which distroless or scratch base is used, and the
  contents of the filesystem beyond what is promised above, are implementation
  detail. Depending on them is what `Compatibility guarantees` exists to warn
  against.

## Appendix: Mapping to Stories

| Section | Implemented by |
|---|---|
| [The plugin directory](#the-plugin-directory) | #54, #55 `container` |
| [Why cpybkc's own generator is not in the base image](#why-cpybkcs-own-generator-is-not-in-the-base-image) | #55 `container` for the base holding none, #48–#53 `gen-go` for the generator that goes through the front door |
| [The CLI's own path is not part of the contract](#the-clis-own-path-is-not-part-of-the-contract) | #55 `container` |
| [The entrypoint](#the-entrypoint) | #55 `container` |
| [The user](#the-user) | #55 `container` |
| [Shell or no shell](#shell-or-no-shell) | #55 `container` |
| [Tags and what pinning one buys](#tags-and-what-pinning-one-buys) | #59 `container` |
| [What a tag carries besides the image](#what-a-tag-carries-besides-the-image) | #58 `container` |
| [Verifying a signature](#verifying-a-signature) | #58 `container` |
| [After mirroring to an internal registry](#after-mirroring-to-an-internal-registry) | #58 `container` |
| [Worked example: adding a generator](#worked-example-adding-a-generator) | #54 `container` |
| [This example is built by the pipeline](#this-example-is-built-by-the-pipeline) | #54 `container` for the build stage and the reading of the final one, #55 `container` for replaying that stage onto a base image of this pipeline's own |
| [Compatibility guarantees](#compatibility-guarantees) | #54, #58 `container` |
| [Why the platform set is the two it is](#why-the-platform-set-is-the-two-it-is) | #54 `container` decides it, #55 `container` builds it, #56 `container` retires the third leg it once had |
| [The IR schema in the image](#the-ir-schema-in-the-image) | #57 `container` |
| [Worked example: reading the IR without generated code](#worked-example-reading-the-ir-without-generated-code) | #57 `container`; #19 `ir` for what the descriptor set contains |
| The multi-platform build itself — out of scope, see above | #55 `container` |
| The Dagger module's default image tag — out of scope, see above | #104 `dagger` settles it in [CONTRIBUTING.md](../../CONTRIBUTING.md#the-companion-dagger-module); #61 `dagger` carries it onto the constructor |
| Signing, provenance and SBOM — verifiable, so the tags section cites them | #58 `container` |
| This document | #54 `container`; its shape and the settled `Scope`, `Governing sources` and `Out of Scope`, #15 `setup` |
| Conventions this document follows | #15 `setup` |
