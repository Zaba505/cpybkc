# The Generator Plugin CLI Contract

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
diagnostic format; the determinism a plugin must exhibit; and the checks a
plugin makes for itself, because cpybkc makes none on its behalf.

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
- **POSIX.1-2024 Base Definitions, chapter 8**, *Environment Variables* — the
  normative definition of `PATH`: a colon-separated list of directory prefixes
  searched in order. [Discovery](#discovery) is a `PATH` search and inherits
  those rules, including which entry wins when a filename appears twice.
  <https://pubs.opengroup.org/onlinepubs/9799919799/basedefs/V1_chap08.html>
- **`SOURCE_DATE_EPOCH`**, *Reproducible Builds* — the reference for the
  determinism requirements, and for why an embedded timestamp is the failure it
  is: it turns every regeneration into a diff and makes generated output
  useless as a thing to commit.
  <https://reproducible-builds.org/docs/source-date-epoch/>
- **Protocol Buffers Language Guide (proto3)** — normative for the wire encoding
  of the descriptor file a plugin reads.
  <https://protobuf.dev/programming-guides/proto3/>
- **[`ir/SPEC.md`](../ir/SPEC.md)** — normative for the value this contract
  moves, and in particular for the version field a plugin checks before
  anything else. With no handshake in front of it (#46), that field is the only
  thing standing between a plugin and a descriptor it half-understands, so the
  rule governing it is cited rather than paraphrased.

> **Ambiguity:** two names collide and neither is this document's to rename.
> A **descriptor**, in `--descriptor`, is a resolved cpybkc IR message; a
> **`FileDescriptorSet`** is protobuf's own reflection type, which cpybkc also
> ships (#19, #57) so that plugin authors without protobuf codegen can read the
> first one. Where this document says "the descriptor" it always means the IR.
> POSIX is cited as the convention the argument vector follows, not as a
> standard cpybkc claims conformance to.
>
> Otherwise the sources do not overlap: POSIX governs the argument vector and
> the `PATH` search, Reproducible Builds governs timestamps, and the IR spec
> governs the bytes. Where this document appears to contradict
> [`ir/SPEC.md`](../ir/SPEC.md) about the descriptor — about the version field
> above all — that document wins and this one has a bug.

### Conformance language

**MUST**, **MUST NOT**, **SHOULD** and **MAY** are normative requirements on a
generator plugin and on the cpybkc CLI that invokes one, interpreted as
described in [CONVENTIONS.md](../CONVENTIONS.md). Everything else is
descriptive.

## Host platform

cpybkc targets POSIX hosts. A plugin author writes against a POSIX filesystem
and a POSIX process model: an executable is a file carrying an execute bit, its
name is the whole of its name, `PATH` is colon-separated, and paths are rooted
at `/`.

Windows is not a target, and that is a decision rather than an omission. The
extension mechanism this project ships is an image a user builds `FROM` and
copies a generator into ([`container/SPEC.md`](../container/SPEC.md), #54), or
the companion Dagger module, whose stages run in Linux containers whatever the
host they were invoked from. Neither path puts a Windows host in the position of
running a generator executable, so the machinery that would have to sit behind
discovery there — a second executable test, an `.exe` suffix to strip before a
generator's name could be read — would be surface area with nobody behind it.
The cost of the decision is that the `PATH` search below is written once, in
the terms POSIX already defines, instead of twice.

macOS and the other Unixes are targets in the sense that matters here: nothing
in this document distinguishes them from Linux. What is excluded is the
non-POSIX case, not everything that is not Linux.

## Discovery

A generator has a **name**: the `<name>` a project's manifest asks for (#40).
Its executable **MUST** be named `cpybkc-gen-<name>`, and cpybkc **MUST**
resolve a name to an executable by searching `PATH` for exactly that filename
(#41). The name is the only thing that connects the two, in either direction:
the suffix of the filename is the generator's name, and nothing inside the
executable is consulted to discover it.

A `<name>` **MUST** be non-empty and **MUST NOT** contain `/`, and **SHOULD** be
lowercase ASCII letters, digits and hyphens. The first two follow from the name
being a filename component; the third is a convention rather than a rule,
because a name appears in a manifest, in a filename and in a log line, and a
name that needs quoting in one of those is a name that will eventually be
mistyped in another.

A candidate **MUST** be a regular file carrying an execute bit. There is no
second test — no extension, no magic number, no metadata file beside it — which
is what keeps a shell script with `chmod +x` a first-class plugin.

`PATH` is searched **in order, and the earliest match wins**, exactly as it does
for a shell resolving a command name (#41). That rule is worth stating because
it is what makes a plugin under development shadow an installed one by
prepending a directory, which is how an author tests a change; the opposite rule
would make that gesture silently do nothing, and the difference is invisible in
the output.

An empty `PATH` element **MUST NOT** be treated as the current working
directory, though POSIX permits it to be. cpybkc runs generators as a side
effect of a generation command, and a generator picked up from whatever
directory a user happened to be standing in is an execution surface nobody
chose. The working directory is searched only when it appears in `PATH` as a
written-out path.

cpybkc **MUST NOT** consult anything beyond `PATH` to find a generator — no
registry, no cache directory of its own, no lockfile, no download. That is not a
detail of discovery but the whole distribution position, and it is stated as
such under [Also out of scope](#also-out-of-scope).

## Invocation

cpybkc runs a discovered executable once per generation, with this argument
vector and no other (#42):

```
cpybkc-gen-<name> --descriptor <path> --out <dir> [--opt k=v ...]
```

`--descriptor` and `--out` **MUST** each appear exactly once. `--opt` **MAY**
appear any number of times, including none, and cpybkc **MUST** pass the options
in the order the manifest declares them (#40), so that the vector is a function
of the manifest rather than of a map iteration.

cpybkc **MUST** pass both paths as absolute paths, and a plugin **MUST NOT**
assume any particular working directory. Two runs of the same generator from
different directories are the same invocation, and a plugin that resolves a
relative path against `.` makes them differ.

cpybkc **MUST NOT** add arguments beyond those above, and in particular **MUST
NOT** take arguments from the environment. A variable that prepended words to
every generator's vector would make the vector a function of the ambient
environment, which is the one input a manifest does not record and a reviewer
cannot see — the same hole [Determinism](#determinism) closes on the other side.

### Options

An option is a single `--opt` argument whose value is `k=v`: everything before
the first `=` is the key, everything after it is the value, and a value **MAY**
be empty or contain further `=` characters. A key **MUST NOT** be empty and
**MUST NOT** contain `=`.

Option keys are the generator's own vocabulary. cpybkc **MUST NOT** interpret
one, and **MUST NOT** validate one against a declared vocabulary, because no
plugin declares one — there is no handshake in which it could (#46). A plugin
**MUST** fail on a key it does not recognise rather than ignoring it, writing an
`error:` diagnostic and exiting non-zero (#48). An ignored option is a line in a
checked-in manifest that reads as configuration and does nothing, and the user
finds out by noticing that the output never changed.

That makes this rule the whole mechanism rather than a backstop behind an
earlier screen. The consequence is that an unrecognised key is caught after the
generator has started instead of before, which costs wasted work and not
correctness: nothing reaches the project's output tree until every generator has
succeeded (#43, #44), so the run fails with the tree exactly as it found it. The
same trade is argued in full under [What dropping the handshake gave
up](#what-dropping-the-handshake-gave-up).

### Option form, and why the separated one

A plugin **MUST** accept the separated form — `--descriptor`, `--out` and
`--opt` each followed by their value as the next argument — and cpybkc **MUST**
emit that form. A plugin **MAY** additionally accept the joined
`--descriptor=<path>` form, and cpybkc **MUST NOT** rely on it.

One required form is what keeps the shell-script plugin honest: a `case`
statement over `"$1"` handles the separated form in three lines and needs no
option-parsing library. Requiring both would mean every plugin author writing
the split on `=` themselves, or reaching for a parser, in order to accept a
spelling cpybkc never emits.

A plugin **MUST** treat `--` as the end of options. This contract defines no
operands, and cpybkc **MUST NOT** pass any; the delimiter is required anyway so
that a plugin invoked by hand behaves the way its neighbours on the system do.

### The descriptor

The value at `--descriptor` is a resolved IR message in the protobuf binary wire
encoding, as [`ir/SPEC.md`](../ir/SPEC.md) defines it (#17). It is not JSON: the
JSON rendering (#21) is a thing a person reads, and a plugin that accepted both
would have to sniff which one it had been handed.

The bytes are the bytes `--emit-ir` writes for the same inputs (#20, #42). That
equality is what makes a failing invocation reproducible: an author emits the
descriptor, saves it, and re-runs the generator against it by hand, and the
second run is the same invocation rather than an approximation of it.

A plugin **MUST** read the descriptor and **MUST NOT** write to it, rename it,
or delete it. It **MUST NOT** derive anything from the file's name or its
directory — the path is a temporary location cpybkc chose, not a place a plugin
may hang meaning on.

### The descriptor's location and lifetime

cpybkc **MUST** write the descriptor into a directory it creates for that one
invocation and nothing else, and **MUST NOT** share a descriptor file between
two generators, or between two runs. One file per invocation is what makes the
bytes attributable: a descriptor on disk belongs to exactly one generator and
one run, and cannot have been overwritten by whichever invocation happened to
finish last.

The file **MUST** be written in full and closed before the generator is started.
A plugin therefore never observes a partial descriptor, and needs no protocol —
no lock, no sentinel, no retry — to find out whether the bytes it can see are
all of them.

cpybkc **MUST** remove the file, and the directory holding it, once the
generator has exited, whether it exited zero or not. That is the whole of the
file's lifetime. A plugin that wants the descriptor to outlive the invocation
**MUST** copy the bytes, and **MUST NOT** expect the path to resolve to anything
afterwards; what it would find there instead is unspecified, because cpybkc
creates the next invocation's directory by the same means.

cpybkc **SHOULD** create the file read-only. The prohibition above is on the
plugin and a mode bit does not enforce it — a plugin running as the user that
created the file can change the mode back — but it turns the accidental case, a
plugin opening the path for writing, into an error where the mistake is rather
than into a descriptor quietly different from the one cpybkc wrote.

Nothing here is a promise about the *path*. The directory cpybkc picks, the name
it gives the file and the suffix on that name are all implementation, and a
plugin reading any of them has broken the rule above rather than found a
feature.

### Why a path rather than standard input

cpybkc **MUST** always pass a path, and that is the point of the flag rather
than a default it happens to have. A file is bytes an author can save, check,
diff, attach to an issue and feed back to the plugin by hand a year later.
Standard input is bytes that exist while a pipeline is running and nowhere
afterwards: a plugin that failed on a descriptor delivered that way leaves the
author holding an invocation they can describe but not repeat, and the second
attempt at reproducing it re-derives the descriptor from the copybook and the
layout, which is a different act with a different possible answer.

`--descriptor -` means the descriptor arrives on standard input instead, and a
plugin **MUST** accept it even though cpybkc never emits it. The `-` form exists
so that a plugin is drivable from a pipeline that has a descriptor and no
convenient place to put it, and it costs a plugin one branch.

### The environment

cpybkc passes its own environment through to a generator unchanged, and that
pass-through **is** the propagation of `SOURCE_DATE_EPOCH` that
[Determinism](#determinism) requires (#47): cpybkc adds no variable of its own,
removes none, and names none, so that variable reaches a generator for the same
reason every other one does. A plugin therefore sees exactly what cpybkc was
started with, and a build that sets `SOURCE_DATE_EPOCH` for its other tools has
already set it for every generator cpybkc runs.

A plugin **MUST NOT** require an environment variable to be set in order to do
its normal work; everything that configures its output arrives as `--opt`. The
manifest is the reviewable record of how a project's code was generated (#40),
and a setting that lives in the environment is absent from it — the two
developers comparing generated output cannot see the difference between their
machines.

### Standard streams

Standard error is the diagnostic channel, specified in [Exit codes and
diagnostics](#exit-codes-and-diagnostics). Standard input is unused unless
`--descriptor -` was passed.

Standard output carries nothing this contract defines. cpybkc **MUST** surface
whatever a plugin writes there, verbatim and attributed to the plugin that wrote
it (#42), and a plugin **SHOULD NOT** write to it: a generator's product is
files beneath `--out` and its explanation is on stderr, so a line on stdout is
either misdirected or debris.

A **SHOULD NOT** rather than a **MUST NOT** is a departure from the design this
contract otherwise follows, and #46 is the reason. A contract carrying a
`--plugin-info` handshake has to keep stdout empty during generation, because
that is the channel the declaration arrives on and telling the two apart would
otherwise need a mode flag. With no handshake there is no second thing on stdout
to protect, so the prohibition would buy nothing and would make a `set -x` left
in a shell-script plugin a conformance failure rather than untidiness.

## The output directory

The directory at `--out` is a private scratch directory that cpybkc creates for
this invocation and this generator alone (#43). A plugin **MAY** assume it
exists, is a directory, is writable, and is **empty**.

A plugin **MUST** write every file it produces beneath that directory. It
**MUST NOT** write outside it — not through `..`, not through an absolute path,
and not through a symlink it created for the purpose. cpybkc enforces this
rather than trusting it (#43), so an escape is a failed run; the requirement is
stated here because a plugin that needs to escape has misunderstood the contract
rather than hit a limitation. A plugin **MAY** create subdirectories beneath
`--out` to whatever depth it needs.

Everything the plugin leaves in that directory on a zero exit is the output of
the invocation, and cpybkc merges it into the project's output tree (#43).
Nothing else is: a file written elsewhere is not output, and a file the plugin
deletes before exiting was never output.

The project's output tree is not something a plugin sees or names. Which
directory the manifest asked for, and how a scratch directory's contents reach
it, are cpybkc's (#40, #43); the plugin's whole obligation is to leave its files
under the directory it was handed.

### What a plugin does not own

Three responsibilities that look like a generator's are cpybkc's, because cpybkc
owns the project's output tree and a generator can see only its own scratch
directory:

- **Merging.** Files reach the project tree only after a zero exit, in one step
  (#43). A plugin **MUST NOT** attempt to write into the project tree itself,
  and does not learn where it is.
- **Collision detection.** Two generators producing the same output path is an
  error cpybkc raises before anything is merged, naming both (#44). A plugin
  **MUST NOT** try to coordinate with another generator, and cannot: their
  directories are disjoint and their order is unspecified.
- **Stale-file pruning.** Removing a file that a previous run produced and this
  one did not is cpybkc's (#45). A plugin **MUST NOT** delete or rename anything
  it did not itself create in this invocation, and **MUST NOT** write or
  maintain a record of what it generated. The record cpybkc keeps spans every
  generator's output and outlives the manifest entry that produced any of it,
  which is what lets a generator *removed* from the manifest have its output
  pruned; its form and where it lives are #45's and not this contract's.

The empty scratch directory is what makes the first two mechanical: the set of
files an invocation produced is exactly the set found in the directory
afterwards, with no marker inside a file and no bookkeeping asked of the plugin.

Collision detection is what fixes *when* a merge happens, so it is stated as a
requirement on cpybkc rather than left to an implementation: cpybkc **MUST**
have resolved every generator's output before it writes any of it into the
project tree, and a collision **MUST** fail the run with nothing merged (#44). A
check made as each generator finished would let the first one's files land
before the second was known about, and the report would then name whichever
generator lost a race — so the same unchanged inputs would fail differently on
different runs, which is the one thing generated output cannot do.

Generators run concurrently (#42), and the consequence for a plugin is only
this: its files appear when the whole run's do, not when it exits, and it
**MUST NOT** depend on being run or merged before, after or alongside another.

### A plugin does not read its own past output

A plugin **MUST NOT** read the project's existing output, and **MUST NOT**
expect files it wrote on a previous run to be present: the directory it is
handed is empty every time (#43). Generation is a function of the descriptor and
the options, and a generator that consulted its previous output would make it a
function of the repository's history too — so a regeneration from a clean
checkout would differ from a regeneration in place, and only one of them would
be reviewed. [Determinism](#determinism) is the same requirement stated
positively.

## Exit codes and diagnostics

A **zero** exit status means every file the plugin intended to produce is
written and closed beneath `--out`, and the invocation succeeded. cpybkc merges
the directory (#43), once every other generator in the run has succeeded too
(#44).

A **non-zero** exit status means the invocation failed. cpybkc **MUST** fail the
run, naming the generator, and **MUST** discard the scratch directory rather
than merging it (#42, #43). Because it is discarded, a plugin **MAY** exit
non-zero with partial output on disk, and **SHOULD NOT** spend effort cleaning
up after itself before failing.

One generator's failure fails the whole run, and since nothing is merged until
every generator has produced its output (#44), a failed run leaves the project
tree as it found it. So a plugin **MUST NOT** read its own zero exit as a
promise that its files are in the tree: another generator failing, or colliding
with it, discards them too. That is the point rather than a side effect — a
half-generated tree is worse than an ungenerated one, because a person then has
to work out which half is which.

cpybkc **MUST NOT** attach meaning to a particular non-zero value beyond
failure, and a plugin **MUST NOT** expect it to. The small integers are already
spoken for by parties this contract does not control: a shell exits 126 for a
file it cannot execute and 127 for one it cannot find, and a process killed by a
signal is reported as 128 plus the signal number by most shells. A scheme
assigning meanings on top of those would be a scheme that misreads the cases it
most needs to get right.

A generator terminated by a **signal** **MUST** be reported by cpybkc as
terminated by that signal, naming it, and distinguishably from one that exited
non-zero (#42). The two need different responses — the second is a bug in the
generator, the first is usually the run being cancelled or the machine running
out of memory — and a report that flattens them sends the user looking in the
wrong place.

### The diagnostic format

A diagnostic is a single line on standard error, encoded in UTF-8:

```
<severity>: <message>
```

`<severity>` **MUST** be one of `error`, `warning` or `note` — a closed set of
three, matched case-sensitively. `<message>` **MUST NOT** contain a newline; a
diagnostic that needs more than one line **MUST** be written as one `error:` or
`warning:` line followed by `note:` lines. A message **SHOULD** open with the
qualified name of the record or field it is about, because that is the only
location a plugin has: it never sees the copybook or the layout file a user
wrote, only the names the IR carries (#38).

The severity **MUST** open the line, the separator **MUST** be a colon and a
single space, and the message **MUST NOT** be empty. Each of those is what tells
a diagnostic from the ordinary output of a program that also writes to standard
error: a line indented under a stack trace, an `error:something` with no space,
and a bare `error: ` say nothing a level could be attached to, and cpybkc treats
all three as text rather than guessing.

```
error: ORDER-DETAIL.OD-QTY: USAGE COMP-3 is not supported by this generator
note: ORDER-DETAIL.OD-QTY: declared as PIC S9(5)V99
```

cpybkc **MUST** parse lines of that form into its structured log at the
corresponding level, attributed to the generator (#42). Any line that does not
match **MUST** be surfaced verbatim and attributed the same way. It is never
discarded and never held back until the process exits: an unrecognised line is
usually a panic, a stack trace or a library writing to stderr on its own
account, and those are exactly what a user needs to see when a generator fails
in a way its author did not anticipate.

The levels are `error:` to error, `warning:` to warning and `note:` to info. A
line that is not a diagnostic is recorded at **warning**, one level above the
`note:` a plugin writes deliberately — info is where a log is ordinarily
thresholded, so a handler configured a notch above it would drop exactly the
panic this rule exists to surface, and a line cpybkc could not classify is not
one to file under the mildest severity it has.

A plugin that writes an `error:` diagnostic **MUST** exit non-zero, and a plugin
that exits non-zero **SHOULD** write at least one `error:` diagnostic saying
why. cpybkc **MUST** fail the run on a non-zero exit even when nothing was
written to stderr — a silent failure is still a failure — and **MUST NOT** fail
a run whose generator exited zero after printing `error:`, because the exit
status is the verdict and the diagnostics are the explanation.

## Determinism

Two invocations of the same plugin executable, given a byte-identical descriptor
and an identical option list, **MUST** produce the same set of relative paths
beneath `--out`, with byte-identical contents (#47).

That holds regardless of the wall-clock time, the hostname, the user, the
working directory, the locale, the environment beyond `SOURCE_DATE_EPOCH`, the
absolute paths passed in `--descriptor` and `--out`, the order in which the
filesystem returns entries, and any concurrency inside the plugin. In
particular, a plugin **MUST NOT** embed a timestamp, a hostname, a username, an
absolute path or a random value in its output, and **MUST** emit anything
derived from an unordered collection in a fixed order — the descriptor's own
order where there is one, or a byte-wise sort of a stable key where there is
not. Map iteration order is the usual way this requirement is broken, and it
breaks intermittently, which is worse than breaking every time.

A plugin **MAY** embed its own name and version, which are properties of the
plugin rather than of the invocation and do not vary between two runs of the
same executable. Output that changes when the generator is upgraded is expected;
output that changes when nothing changed is the failure.

Where a timestamp is genuinely unavoidable, a plugin **MUST** use the value of
`SOURCE_DATE_EPOCH` when it is set, interpreted as a count of seconds since the
Unix epoch in UTC, and **MUST NOT** read the clock. cpybkc **MUST** propagate
that variable to a generator when it is set in cpybkc's own environment (#47);
it is the single permitted channel for a time, rather than one convention among
several. When it is not set, a plugin **SHOULD** omit the timestamp rather than
fall back to the clock: a missing generation date costs a reader nothing, and a
present one costs every regeneration a diff.

The requirement is checked rather than asserted — the pipeline generates
repeatedly, from runs that deliberately disagree about every surrounding named
above and agree only about `SOURCE_DATE_EPOCH`, and byte-compares the trees,
failing on any difference (#47) — because determinism is the kind of property
that holds until nobody is looking. Repetition catches output ordered by map
iteration, because Go randomises that on every range. It cannot catch a clock
read, since two runs a moment apart agree on the date, so the generators in this
repository are additionally held to the rule by a check on their source, which
parses every package that decides what a run writes and refuses a call that
reads the clock, the environment, the working directory, the host, the user or a
random value. A third-party plugin is bound by the requirement above however it
chooses to meet it.

## The version check, and why there is no handshake

cpybkc performs **no** version check and **no** capability negotiation of any
kind before invoking a generator (#46). It does not ask a plugin what it
supports, it does not compare the descriptor's version against anything a plugin
declared, and it does not screen the options in the manifest against a declared
vocabulary. A plugin **MUST NOT** assume it was pre-screened in any respect, and
**MUST NOT** assume the descriptor in front of it is one it can read.

That is a decision rather than an omission. A `--plugin-info` handshake was
specified and then dropped (#46): it cost a process call per generator per run,
and the capability model behind it rested on a feature vocabulary nobody had
designed — which, published as a third-party interface, would have been far
harder to change than the code behind it. What survives is the check the plugin
was going to have to make anyway, since a plugin is entitled to be run by
something other than this version of cpybkc.

### Reading the version first

A plugin **MUST** read the descriptor's IR version field before anything else in
the message, and **MUST** refuse a descriptor whose version it does not
implement (#17, #48). Refusing means writing no file beneath `--out` and exiting
non-zero. A plugin **MUST NOT** generate from a descriptor it understood only in
part.

*Does not implement* is the IR spec's rule and not a looser one of this
document's. The version is a single monotonic integer, and a consumer **MUST**
refuse one it does not know rather than proceeding on the parts it recognises
([The version field](../ir/SPEC.md#the-version-field)). There is no
newer-but-compatible case to detect and no minor number in which to express one:
an addition a generator can ignore and still be correct about leaves the version
alone, and every addition it cannot leaves the version behind (#17).

The failure this prevents is the one that does not look like a failure. A
descriptor a version too new decodes cleanly — a new member of a closed set
reaches an old reader as an unknown field, which protobuf tells it to ignore —
so a plugin that skips the check emits code that compiles, links, and silently
misreads the file it was written for. Reading the version *first*, before any
other field, is what keeps that from happening in the window between decoding
the message and noticing what is in it.

### What the refusal must say

cpybkc never learns there was a mismatch, so it cannot compose the diagnostic on
the plugin's behalf, and a refusal naming one number leaves the user unable to
tell an out-of-date generator from an out-of-date CLI. A refusal therefore
**MUST** name all three of:

- the IR version of the descriptor the plugin was handed;
- the highest IR version the plugin implements;
- the plugin's own version, in whatever scheme its author uses.

It **MUST** be written as an `error:` diagnostic in the form [The diagnostic
format](#the-diagnostic-format) defines, so that it reaches the user by the same
path as every other failure, and the plugin **MUST** then exit non-zero.

```
error: descriptor IR version 2; cpybkc-gen-go 0.1.0 implements IR version 1
```

The wording is the plugin author's. The three facts are not: with them the user
knows whether to upgrade the generator or pin the CLI, and without any one of
them the next step is a guess.

### What dropping the handshake gave up

Three things, stated so that they are a decision and not a discovery:

- **Failure now happens after generators start rather than before.** That costs
  wasted work and not correctness: nothing reaches the project's output tree
  until every generator has succeeded (#43, #44), so a late refusal leaves the
  tree exactly as it found it. The unrecognised-`--opt`-key rule under
  [Options](#options) is the same trade in the same direction.
- **cpybkc can no longer name a version mismatch itself.** Diagnostic quality
  now depends on each plugin, which is why the content of the refusal is
  specified above rather than left to taste.
- **Feature-level negotiation is gone entirely.** A plugin that decodes a new
  discriminator kind cleanly and generates wrong code from it is covered only by
  the IR version advancing for it — so that version **MUST** advance on every
  addition a generator could misread, not only on the wire-breaking ones, which
  is the obligation [What breaks it](../ir/SPEC.md#what-breaks-it) carries
  (#17). A rule keyed to wire compatibility instead would never advance and
  would guard nothing.

Retrofitting a handshake later is the expensive direction, and that is
understood. If it is ever reconsidered, #46 is where the case for it is
recorded.

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
| [Host platform](#host-platform) | #39 `plugin` — decided here; nothing in the backlog had committed either way, and the container contract is Linux-only |
| [Discovery](#discovery) | #41 `plugin` |
| [Invocation](#invocation) | #42 `plugin`; the option order, and the manifest it comes from, #40 `plugin` |
| [The descriptor](#the-descriptor) | #17 `ir` for the message, #20 `ir` for the bytes `--emit-ir` writes, #42 `plugin` for the file handed over |
| [The output directory](#the-output-directory) | #43, #44, #45 `plugin` |
| [Exit codes and diagnostics](#exit-codes-and-diagnostics) | #42 `plugin` |
| [Determinism](#determinism) | #47 `plugin` |
| [The version check, and why there is no handshake](#the-version-check-and-why-there-is-no-handshake) | #17 `ir` for the version field's rule, #48 `gen-go` for the reference plugin that implements and tests the refusal |
| No handshake — `--plugin-info` specified and dropped | #46 `plugin`, closed not planned; it produced no section, and [the version check](#the-version-check-and-why-there-is-no-handshake) is what carries the weight it would have shared |
| The project manifest — out of scope, see above | #40 `plugin` |
| A worked implementation of this contract | #48–#53 `gen-go` |
| This document | #39 `plugin` |
| Conventions this document follows | #15 `setup` |
