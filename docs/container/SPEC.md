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
	"os"
	"path/filepath"
	"strings"

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

	b, err := os.ReadFile(descriptor)
	if err != nil {
		fail("%v", err)
	}
	var d irpb.Descriptor
	if err := proto.Unmarshal(b, &d); err != nil {
		fail("%v", err)
	}

	// The version, before anything else in the message.
	if d.GetVersion() != irVersion {
		fail("descriptor IR version %d; cpybkc-gen-hello %s implements IR version %d",
			d.GetVersion(), pluginVersion, irVersion)
	}

	name := filepath.Join(out, "hello.txt")
	if err := os.WriteFile(name, []byte(greeting+"\n"), 0o644); err != nil {
		fail("%v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
EOF

RUN go get github.com/Zaba505/cpybkc/irpb \
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
**final stage is interpreted rather than built**: `FROM ghcr.io/zaba505/cpybkc`
names a published image, and its `FROM` line, its absence of a `RUN` and its
`COPY` flags and paths are read and checked against the requirements above.
Building that stage against the base image the pipeline itself produced is what
#55 adds, when there is one to build it against.

The check refuses what it cannot replay: an instruction in the final stage other
than the ones this document permits is an error naming it, rather than a line
that quietly goes unchecked. A worked example that grows an `ENV` teaches the
pipeline what an `ENV` means before CI accepts it.

## Compatibility guarantees

**Covered.** Within a major version of the image each of these holds, and a
change to any of them is a breaking change:

| Guarantee | Value |
| --- | --- |
| [The plugin directory](#the-plugin-directory) | `/usr/local/bin`, on `PATH`, existing and owned by the image's user |
| [The entrypoint](#the-entrypoint) | Is the cpybkc CLI, and takes its arguments |
| [The user](#the-user) | UID 65532, GID 65532, non-root, overridable |
| [Shell or no shell](#shell-or-no-shell) | Absent; extension is `COPY`-only |
| [Platforms](#why-linuxs390x-is-inside-the-guarantee) | `linux/amd64`, `linux/arm64` and `linux/s390x` |
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

### Why `linux/s390x` is inside the guarantee

The covered platform set is the deliberate part of that table, because it is the
one place this contract does not follow the prior art it is otherwise modelled
on: `avroc` publishes `linux/amd64` and `linux/arm64` and puts every further
platform explicitly *outside* its guarantee.

cpybkc brings `linux/s390x` inside instead (#55, #56). The audience is the
reason. Files composed of COBOL copybook records come from mainframes, and Linux
on Z and z/OS Container Extensions are where a shop with those files already
runs containers; a derived image that cannot be built for the machine holding
the data is an extension mechanism that mostly works. It is also the only
big-endian platform in the matrix, which makes it the leg that tests the
[byte-order fork](https://github.com/Zaba505/cobol-go/blob/main/codec/SPEC.md)
the generated code is written against rather than merely packaging for it — the
class of bug that passes everywhere else (#56).

The cost is honest and small: CGO-free Go cross-compiles to it for nothing, and
the emulated test leg (#56) is CI time rather than a promise. What putting it in
the table buys is that a Dockerfile in a mainframe shop's repository may say
`FROM` and mean it, and that dropping the platform later would be the breaking
change it ought to be rather than a quiet change of mind.

The *set* is covered; the machinery that fills it is not. Which platforms a
published index carries is something a consumer reads out of the index and
builds against, so it is a guarantee. How each one is cross-compiled, tested
under emulation or assembled is [out of
scope](#how-the-image-is-built-and-published) like the rest of the build.

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
| [Why cpybkc's own generator is not in the base image](#why-cpybkcs-own-generator-is-not-in-the-base-image) | #55 `container` for the base holding none, #48–#53 `gen-go` for the generator that goes through the front door |
| [The CLI's own path is not part of the contract](#the-clis-own-path-is-not-part-of-the-contract) | #55 `container` |
| [The entrypoint](#the-entrypoint) | #55 `container` |
| [The user](#the-user) | #55 `container` |
| [Shell or no shell](#shell-or-no-shell) | #55 `container` |
| [Tags and what pinning one buys](#tags-and-what-pinning-one-buys) | #59 `container` |
| [What a tag carries besides the image](#what-a-tag-carries-besides-the-image) | #58 `container` |
| [Worked example: adding a generator](#worked-example-adding-a-generator) | #54 `container` |
| [This example is built by the pipeline](#this-example-is-built-by-the-pipeline) | #54 `container` for the build stage and the reading of the final one, #55 `container` for building that stage against a base image of this pipeline's own |
| [Compatibility guarantees](#compatibility-guarantees) | #54, #58 `container` |
| [Why `linux/s390x` is inside the guarantee](#why-linuxs390x-is-inside-the-guarantee) | #54 `container` decides it, #55, #56 `container` build and test it |
| The IR proto shipped in the image — a consumer-visible file, sized here | #57 `container` |
| Multi-platform build and s390x testing — out of scope, see above | #55, #56 `container` |
| Signing, provenance and SBOM — verifiable, so the tags section cites them | #58 `container` |
| This document | #54 `container`; its shape and the settled `Scope`, `Governing sources` and `Out of Scope`, #15 `setup` |
| Conventions this document follows | #15 `setup` |
