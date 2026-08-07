# The Base-Image Contract

> **Stub.** #54 writes this document. `Scope`, `Governing sources` and `Out of
> Scope` are settled (#15); the headings between them are its outline and are
> empty on purpose. See [CONVENTIONS.md](../CONVENTIONS.md).

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
`--user $(id -u):$(id -g)`, whether a shell is present and what follows if it is
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

<!-- #54: the one stable directory on PATH that a derived image copies a
     generator into. It is a path in strangers' Dockerfiles, so state the
     stability guarantee alongside it, not separately. -->

## The entrypoint

<!-- #54: the cpybkc CLI, and what a derived image must not do to it. -->

## The user

<!-- #54: a stable non-root UID, why it is pinned rather than allocated, the
     ownership of the plugin directory and the output directory that follows
     from it, and guidance on --user $(id -u):$(id -g) for writing files a
     caller can then read. -->

## Shell or no shell

<!-- #54: whether the base image has one. If it does not, extension is
     COPY-only — no RUN in a stage built FROM it — and that has to be said
     plainly, because it is the difference between a Dockerfile that works and
     one that fails on its second line. -->

## Tags and what pinning one buys

<!-- #54: which tags exist and what each promises across a rebuild. A derived
     Dockerfile pins one, so the tag is as much a part of this contract as the
     paths are. Published by #59. -->

## Worked example: adding a generator

<!-- #54: a complete multi-stage Dockerfile that builds a custom plugin and
     copies it in, runnable as written. -->

## Compatibility guarantees

<!-- #54: what is covered, what is explicitly not, and how a covered thing
     would change if it had to. -->

## Out of Scope

### How the image is built and published

The build, the platform matrix, emulated testing, signing, provenance and SBOM
generation are **not specified here**.

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
  comment and `dagger call --help`.
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
| [The entrypoint](#the-entrypoint) | #55 `container` |
| [The user](#the-user) | #55 `container` |
| [Shell or no shell](#shell-or-no-shell) | #55 `container` |
| [Tags and what pinning one buys](#tags-and-what-pinning-one-buys) | #59 `container` |
| [Worked example: adding a generator](#worked-example-adding-a-generator) | #54 `container` |
| [Compatibility guarantees](#compatibility-guarantees) | #54, #58 `container` |
| The IR proto shipped in the image — a consumer-visible file, sized here | #57 `container` |
| Multi-platform build and s390x testing — out of scope, see above | #55, #56 `container` |
| Signing, provenance and SBOM — verifiable, so the tags section cites them | #58 `container` |
| Conventions this document follows | #15 `setup` |
