# The Generator Plugin CLI Contract

> **Stub.** #39 writes this document. `Scope`, `Governing sources` and `Out of
> Scope` are settled (#15); the headings between them are its outline and are
> empty on purpose. See [CONVENTIONS.md](../CONVENTIONS.md).

## Overview

A cpybkc generator is an executable, not a server. cpybkc finds it on `PATH`,
runs it with a resolved descriptor and an output directory, and waits for it to
exit; the generator writes files to disk and says what went wrong on stderr.
That is the entire contract, and it is deliberately small enough that a
generator can be a shell script.

The alternative — a plugin that speaks gRPC, as `protoc`'s do — costs a socket,
a port, a lifecycle and a protobuf runtime in every implementation language, to
buy a structured response that a process writing files to a directory does not
need. Inside a container it is strictly worse: two processes and a rendezvous
where there was one process and a path.

This document is the plugin author's half. What a plugin *receives* is the
[resolved IR](../ir/SPEC.md), specified there and never restated here; where on
`PATH` the published image keeps plugins is the [base-image
contract](../container/SPEC.md)'s. What is in the descriptor belongs to the IR
spec, which directory exists in the image belongs to the container spec, and how
an executable is found and invoked belongs here.

**protobuf is the IR's schema language and nothing more: there is no gRPC
service, no server and no port.** The argument is in the IR spec, under [Why
protobuf, and why no gRPC](../ir/SPEC.md#why-protobuf-and-why-no-grpc); it is
repeated here because a plugin author arrives looking for a service definition
and must not have to open another document to find out there is not one.

### Scope

In scope: how cpybkc discovers a generator and hands it work. The
`cpybkc-gen-<name>` naming convention and its resolution against `PATH`; the
argument vector — `--descriptor <path>` with `-` for stdin, `--out <dir>`, and
repeated `--opt k=v`; the encoding and lifetime of the descriptor file; what a
plugin may and may not do to the output directory; exit codes and the stderr
diagnostic format; the determinism a plugin must exhibit; and the
`--plugin-info` handshake by which a plugin declares what it supports.

Out of scope, with reasons, in [Out of Scope](#out-of-scope).

### Governing sources

- **`plugin.proto`**, *the `protoc` compiler plugin contract* — cited as the
  design this one deliberately does not follow. It is the reference point every
  prospective plugin author already has, so the differences are stated against
  it rather than described from nothing.
  <https://github.com/protocolbuffers/protobuf/blob/main/src/google/protobuf/compiler/plugin.proto>
- **POSIX.1-2024 Base Definitions, chapter 12**, *Utility Conventions* —
  normative for the shape of the argument vector: option syntax, `--` handling,
  and the meaning of `-` as an operand standing for standard input.
  <https://pubs.opengroup.org/onlinepubs/9799919799/basedefs/V1_chap12.html>
- **`SOURCE_DATE_EPOCH`**, *Reproducible Builds* — the reference for the
  determinism requirements, and for why an embedded timestamp is the failure it
  is: it turns every regeneration into a diff and makes generated output
  useless as a thing to commit.
  <https://reproducible-builds.org/docs/source-date-epoch/>
- **Protocol Buffers Language Guide (proto3)** — normative for the wire encoding
  of the descriptor file a plugin reads.
  <https://protobuf.dev/programming-guides/proto3/>

> **Ambiguity:** two names collide and neither is this document's to rename.
> A **descriptor**, in `--descriptor`, is a resolved cpybkc IR message; a
> **`FileDescriptorSet`** is protobuf's own reflection type, which cpybkc also
> ships (#19, #57) so that plugin authors without protobuf codegen can read the
> first one. Where this document says "the descriptor" it always means the IR.
> POSIX is cited as the convention the argument vector follows, not as a
> standard cpybkc claims conformance to.

### Conformance language

**MUST**, **MUST NOT**, **SHOULD** and **MAY** are normative requirements on a
generator plugin and on the cpybkc CLI that invokes one, interpreted as
described in [CONVENTIONS.md](../CONVENTIONS.md). Everything else is
descriptive.

## Discovery

<!-- #39: the cpybkc-gen-<name> convention and how a name in the manifest
     resolves to an executable on PATH. Implemented by #41. Open question for
     #39: whether a non-POSIX host is supported at all — nothing in the backlog
     commits either way, and the container contract is Linux-only. Decide it;
     do not let it be decided by omission. -->

## Invocation

<!-- #39: --descriptor <path> with `-` for stdin, --out <dir>, repeated
     --opt k=v. Implemented by #42. Record why a path is the default rather
     than stdin: it makes the bytes a plugin receives reproducible and lets an
     author re-run the failing invocation by hand. -->

## The output directory

<!-- #39: a plugin writes into a scratch directory it owns, and cpybkc merges
     it atomically (#43); collisions between plugins are cpybkc's to detect
     (#44) and stale files cpybkc's to prune (#45). State what that means for
     the plugin: what it may assume about the directory it is given. -->

## Exit codes and diagnostics

<!-- #39: what a zero and a non-zero exit mean, and the stderr format cpybkc
     expects a diagnostic in. -->

## Determinism

<!-- #39: identical descriptor and options produce byte-identical output. No
     embedded timestamps, no hostnames, no map-iteration order. Enforced by
     #47. -->

## Capability negotiation

<!-- #39: --plugin-info, by which a plugin declares what it supports before it
     is handed work. Implemented by #46. -->

## Out of Scope

### What is in the descriptor

The structure and meaning of the IR a plugin reads are **not specified here**.

Reason: they are [`ir/SPEC.md`](../ir/SPEC.md)'s (#16), and the split is what
lets the two evolve apart. The IR gains a field without this document changing;
this document changes a flag without the IR moving. Restating any of the IR's
shape here would produce a second description for a plugin author to find and
for the two to disagree about.

### Where plugins live in the published image

The directory a derived image copies a generator into, the entrypoint, and the
UID it runs as are **not specified here**.

Reason: they are the [base-image contract](../container/SPEC.md)'s (#54). This
document says a generator is found by name on `PATH`, which is true wherever
cpybkc runs, including a developer's laptop with no container involved. Which
directories are *on* that `PATH` inside the published image is a property of the
image, and binding the two together would make a contract about executables
depend on a contract about layers.

### The `cpybkc.json` project manifest

Which generators a project runs, over which layouts, with which options, is
**not part of this contract**.

Reason: different audience. The manifest (#40) is what a *user* writes to drive
the CLI; this document is what a *plugin author* implements. A plugin never
reads the manifest — it receives the options the manifest selected, already
resolved, on its command line — so specifying the file here would put a
build-configuration format in front of the one reader with no use for it.

### Also out of scope

- **A transport.** There is no gRPC service, no server, no port, no lifecycle.
  Argued in the IR spec, restated in the overview above.
- **What a generator emits.** `cpybkc-gen-go` (#48–#53) is one plugin among
  many; its Go output is its own concern, and a contract that described it
  would not be a contract a third-party generator could meet.
- **Plugin distribution.** There is no plugin registry, no lockfile, no OCI
  fetch and no resolution protocol. A plugin arrives on `PATH`, by whatever
  means put it there; the container contract describes the one this project
  supports.

## Appendix: Mapping to Stories

| Section | Implemented by |
|---|---|
| [Discovery](#discovery) | #41 `plugin` |
| [Invocation](#invocation) | #42 `plugin` |
| [The output directory](#the-output-directory) | #43, #44, #45 `plugin` |
| [Exit codes and diagnostics](#exit-codes-and-diagnostics) | #39 `plugin` |
| [Determinism](#determinism) | #47 `plugin` |
| [Capability negotiation](#capability-negotiation) | #46 `plugin` |
| The project manifest — out of scope, see above | #40 `plugin` |
| A worked implementation of this contract | #48–#53 `gen-go` |
| Conventions this document follows | #15 `setup` |
