# The Command-Line Interface

## Overview

cpybkc is one executable a person runs. It finds the project's manifest, reads
the layout that manifest names, resolves it against the copybooks it names, and
hands the resolved descriptor to every generator the manifest asks for. This
document is the contract between that person and the program: what may be
typed, where the manifest is looked for, what arrives on each stream, and what
an exit status means.

The neighbouring documents own everything on the far side of a process
boundary. How a generator is found, what it is handed and what its exit status
means is the [plugin contract](../plugin/SPEC.md)'s; what is *in* a descriptor
is the [IR](../ir/SPEC.md)'s; what a derived image may rely on is the
[base-image contract](../container/SPEC.md)'s. Questions about what cpybkc
gives a *program* belong there; questions about what cpybkc takes from a
*person*, and hands back to one, belong here.

A command line gets a document here for the reason
[CONVENTIONS.md](../CONVENTIONS.md) admits anything: it is an interface a third
party builds against. A pipeline that runs cpybkc is a file in a repository
this project cannot see, built by something it cannot warn, and written against
flag names and exit codes rather than against a Go API. The published image
sharpens it — `Entrypoint` **is** this CLI and `Cmd` is empty (#55), so the
arguments in somebody's `docker run` line are the arguments specified below,
and a flag renamed here breaks a Dockerfile there. That is harder to change
than the code behind it, which is the whole test.

### Scope

In scope: the command set; how the project manifest is located; the complete
argument vector, with every flag's form, default and repeatability; what
`--emit-ir` does to a run; what standard output and standard error carry and in
what form; the exit statuses and what each one means; the environment cpybkc
reads; what `--version` prints; and how much of that a caller may depend on
across releases.

Out of scope, with reasons, in [Out of Scope](#out-of-scope).

### Governing sources

- **POSIX.1-2024 Base Definitions, chapter 12**, *Utility Conventions* —
  normative for the shape of the argument vector: option syntax, the `--`
  delimiter, and the meaning of `-` where a stream stands in for a file. It is
  cited as the convention this vector follows rather than as a standard cpybkc
  claims conformance to; the deliberate departures are named where they are
  made.
  <https://pubs.opengroup.org/onlinepubs/9799919799/basedefs/V1_chap12.html>
- **POSIX.1-2024 Base Definitions, chapter 8**, *Environment Variables* — the
  normative definition of `PATH`, the one variable a run reads.
  [The environment](#the-environment) is a statement about that list being
  closed, and it is stated in the terms this chapter already defines.
  <https://pubs.opengroup.org/onlinepubs/9799919799/basedefs/V1_chap08.html>
- **[`plugin/SPEC.md`](../plugin/SPEC.md)** — normative for the diagnostic
  format cpybkc *parses*, for the meaning of a generator's exit status, and for
  the environment a generator inherits. This document specifies the other end
  of each: what that parsed diagnostic looks like coming out on cpybkc's own
  stderr, and what cpybkc's exit status says about a generator's.
- **[`ir/SPEC.md`](../ir/SPEC.md)** — normative for the bytes `--emit-ir`
  writes in either form (#20, #21), and for the version field
  [`--version`](#--version) reports. What a descriptor contains is that
  document's; which command writes one is this document's.
- **[`layout/SPEC.md`](../layout/SPEC.md)** — normative for what a layout
  states, and the source of two questions it hands to the CLI by name: where a
  copybook path is looked for, and where a layout's forms are read from. Both
  are answered in [Finding the inputs](#finding-the-inputs).
- **[`container/SPEC.md`](../container/SPEC.md)** — normative for the promise
  that makes this vector a published interface: the image's entrypoint is this
  CLI, its arguments arrive unaltered, and nothing sits between them and it
  (#55).
- **[`README.md`](../../README.md)**, *The project manifest* — the description
  of the `cpybkc.json` this CLI locates. It is deliberately not a spec (#40),
  and this document fixes only how the file is found, never what is in it.
- **Semantic Versioning 2.0.0** — normative for the form of the version string
  `--version` prints and for the release in which a change to this surface may
  appear. <https://semver.org/spec/v2.0.0.html>

> **Ambiguity:** the sources do not conflict, and where this document appears
> to contradict [`plugin/SPEC.md`](../plugin/SPEC.md),
> [`ir/SPEC.md`](../ir/SPEC.md) or [`container/SPEC.md`](../container/SPEC.md),
> those documents win and this one has a bug. One overlap is worth naming
> anyway: POSIX gives `-` its meaning as an *operand* standing for a standard
> stream, while `--emit-ir -` spells it as a flag's *value*. The reading is the
> plugin contract's `--descriptor -` reading, and the two are deliberately the
> same reading, so that a dash means one thing across this project.

### Conformance language

**MUST**, **MUST NOT**, **SHOULD** and **MAY** are normative requirements on
the cpybkc CLI and on a caller invoking it, interpreted as described in
[CONVENTIONS.md](../CONVENTIONS.md). Everything else is descriptive.

## One command, and no subcommands

cpybkc **MUST** be a single command. There are no subcommands: no `generate`,
no `emit`, no `init`, no `plugin install`. Generating is what the command does
when nothing else is asked of it (#147, #148).

The product does one thing. A subcommand set exists to name several, and naming
one of several costs more than it looks: the default action needs a name it
keeps forever, every document and every CI script gains a word, and the
container's `Entrypoint` promise turns `docker run image generate` into the
short form of a command that used to be `docker run image`. What it would buy —
room for actions nobody has proposed — is bought below for nothing.

### The operand position is reserved and empty

cpybkc **MUST NOT** accept an operand. Every input is named by a flag, and a
non-flag argument, wherever it appears and including after `--`, **MUST** be a
usage error ([Exit codes](#exit-codes), status 2).

That is what keeps the door open. The first operand is where a subcommand would
go, and a CLI that already spends it on a path — `cpybkc ./cpybkc.json` — can
never add one without deciding whether an argument is a file or a verb. Leaving
it empty means a subcommand may be added later as an addition rather than as a
break, and it costs one flag ([`--manifest`](#finding-the-manifest)) that has
to be typed in the uncommon case and not at all in the common one.

`--` is honoured anyway, as the end of options, so that cpybkc behaves the way
its neighbours on the system do. Since it can only ever be followed by
arguments that are usage errors, honouring it is a courtesy to muscle memory
rather than a way to pass anything.

## The argument vector

```
cpybkc [--manifest <path>] [--emit-ir <dest> [--emit-ir-format <format>]]
cpybkc --version
cpybkc --help
```

That is the whole surface (#147):

| Flag | Value | Default | May repeat |
| --- | --- | --- | --- |
| `--manifest` | a path to the project manifest | `cpybkc.json` in the working directory | no |
| `--emit-ir` | a path, or `-` for standard output | absent — nothing is emitted | no |
| `--emit-ir-format` | `binary` or `json` | `binary` | no |
| `--version` | none | — | no |
| `--help`, `-h` | none | — | no |

cpybkc **MUST NOT** accept a flag this table does not list, and an unrecognised
flag **MUST** be a usage error naming it. There is no `--out`, no `--include`,
no `--jobs`, no `--verbose` and no `--config`; each is refused with its reason
in [Out of Scope](#out-of-scope).

### Flag form

A flag **MUST** be accepted in the two-hyphen form this document writes it in,
and a flag taking a value **MUST** be accepted both separated —
`--manifest cpybkc.json` — and joined — `--manifest=cpybkc.json`. A single
leading hyphen **MAY** be accepted as a synonym and **MUST NOT** be documented;
`-h` is the one single-hyphen spelling this document states, because it is the
one every user already has in their fingers.

A flag taking no value — `--version`, `--help` — **SHOULD** be written without
one.

Both value forms are required here where the [plugin
contract](../plugin/SPEC.md#option-form-and-why-the-separated-one) requires
exactly one, and the difference is who is typing. That vector is built by a
program, so one form keeps a shell-script plugin's `case` statement to three
lines; this one is typed by a person who has already learned both spellings
from every other tool on their machine, and refusing the one they used would be
pedantry a shell script never has to suffer.

### A flag appears at most once

A flag given twice **MUST** be a usage error, even where both occurrences carry
the same value. Silently taking the last is the rule most argument parsers
default to, and it makes `cpybkc --manifest a.json --manifest b.json` a command
that reads a file the line also names as something else.

It is the rule the manifest reader already applies to a field written twice
(#40), for the same reason: the file, and the command line, are things a person
wrote, and a duplicate is something they did not mean rather than a precedence
question for the program to answer on their behalf.

### `--help` and `--version` are answered before anything else

When `--help` is present, cpybkc **MUST** write usage to standard output and
exit 0. When `--version` is present and `--help` is not, it **MUST** write the
version line to standard output and exit 0. In both cases every other flag on
the line **MUST** be ignored, including one that would otherwise be a usage
error, and no manifest is read.

This is a deliberate exception to [A flag appears at most
once](#a-flag-appears-at-most-once) and to the refusal of an unrecognised flag.
A person asking a program what it is has usually typed the rest of the line
already, and answering with a complaint about a flag they were in the middle of
getting wrong teaches them nothing they asked to learn.

Usage **MUST** be written to standard output when it was requested, and to
standard error when it accompanies a usage error. The distinction is the whole
of what makes `cpybkc --help | less` work while keeping a failing run's output
off the data channel ([Standard output](#standard-output)).

## Finding the manifest

`--manifest <path>` names the project manifest. With no `--manifest`, cpybkc
**MUST** read `cpybkc.json` in the working directory, and **MUST NOT** look
anywhere else (#148).

The value names a regular file. It **MUST NOT** be `-`: every path inside a
manifest is relative to the manifest's own directory, and a manifest arriving
on a stream has no directory for them to be relative to. A value naming a
directory, or naming nothing that can be opened, **MUST** fail the run with a
diagnostic naming the path as it was typed.

The working directory is used for exactly two things: locating that default
`cpybkc.json`, and resolving a relative path *typed on the command line*, as
[below](#a-path-is-relative-to-the-file-that-states-it) states. Nothing else
about a run is a function of where the user was standing, and that is what
makes the published image's arrangement work — `docker run -v "$PWD:/src"
-w /src …` mounts the project and stands in it (#55) — while leaving a run
started from anywhere else identical as soon as `--manifest` names the file.

### Why there is no upward search

cpybkc **MUST NOT** search parent directories for a manifest, and **MUST NOT**
consult more than one manifest in a run.

An upward search is the convention several build tools follow, and it is wrong
here for a reason particular to this one: a manifest's paths are relative to
the manifest, so the directory a search *found* the file in decides where every
input is read from and where every generator's output lands. A user standing
two directories deeper would get a run that reads the same copybooks and writes
its output somewhere else entirely — silently, because the search succeeded.

It also disposes of the case a search has to answer and cannot answer well.
Two manifests found on one walk is an ambiguity whose resolutions are all bad:
nearest wins is a rule nobody can see in a diff, and failing is a failure the
user cannot fix without moving files. With no search there is no ambiguity to
resolve — the manifest is the one the command line names, or the one in the
directory the command was run from, and there is never a third answer.

A project that keeps its manifest somewhere else says so, once, in whatever
script or CI step runs cpybkc. That statement is in a file a reviewer can read,
which is the same standard the manifest itself is held to (#40).

### A path is relative to the file that states it

One rule covers every path in a run, and it has two halves:

- A relative path **stated in a file** is resolved against the directory of
  that file. A manifest's `layout` and `out` are relative to the manifest
  (#40); a layout's `copybook` path is relative to the layout (#148).
- A relative path **typed on the command line** — `--manifest`, `--emit-ir` —
  is resolved against the working directory, because that is the directory the
  person typing it was standing in.

cpybkc **MUST** resolve paths this way and **MUST NOT** resolve one against
anything else — not against a search path, not against a variable, not against
the project root.

The first half is what makes a layout portable. A layout and the copybooks it
names travel together, so a layout naming `cpy/orders.cpy` means the copybook
beside it wherever the pair is checked out, and a project that vendors somebody
else's layout does not have to rewrite the paths inside it. Resolving a
copybook against the *manifest* instead would make one layout mean different
files in two projects, which is the one thing a description of a data file
cannot afford.

### Finding the inputs

[`layout/SPEC.md`](../layout/SPEC.md) hands two questions to the CLI by name,
and this section answers both — and then the third, which nothing had asked
until a manifest and a layout each looked like a place a copybook could be
named from.

**Where a copybook is looked for.** At the path its layout states, resolved as
above, and nowhere else. cpybkc **MUST NOT** search a list of directories:
there is no `--include`, no `-I`, no `COPYPATH`, and no implicit directory.
A search path makes the same layout resolve to different copybooks on two
machines with no difference between them that any file records, which is the
failure a generated file library cannot detect and cannot recover from — the
code compiles, and the offsets are wrong.

**Where a layout's forms are read from.** From the one file the manifest's
`layout` names. A layout has no include form and needs none (`layout/SPEC.md`),
and cpybkc composes nothing: two files are two layouts and two runs.

**Which copybooks a run reads**, and the answer #157 owed this section: exactly
the ones its layout's `record` forms name, and no others. The manifest does not
list them. It names the layout, the generators and their options — the things a
layout has no opinion about — and it carries no `inputs` (#40), so there is no
set a layout may not name a copybook outside of, and no diagnostic for naming
one. The only fault about a copybook is the one above, a path cpybkc cannot
open.

That is a decision against the alternative and not an omission. A manifest that
listed the copybooks a run may read would be a second statement of what the
layout already says, in a file that is checked in beside it and diffable in the
same review — right up until the two disagreed, at which point the layout would
still be the one that named the item each record is. Worse, enforcing the list
would mean deciding whether two spellings name one file: a manifest's paths
resolve against the manifest and a layout's against the layout ([A path is
relative to the file that states it](#a-path-is-relative-to-the-file-that-states-it)),
so the same copybook is written two ways, and `layout/SPEC.md` hands "whether
two paths name one copybook" to the CLI as a question about the files rather
than the spellings. Behind a symlink, a bind mount or a case-insensitive
filesystem there is no comparison that is right every time — and a check that
fails a correct run is worse than no check, because the run it rejects is one
the adopter cannot fix by changing anything that is wrong.

A copybook cpybkc cannot open **MUST** be reported with a diagnostic that names
the path **as the layout spells it** and, additionally, the absolute path
cpybkc opened. The first is what the adopter can find in their layout; the
second is the "where it was looked for" that `layout/SPEC.md` requires the CLI
to explain, and without it a relative path in a shared layout sends a reader
looking in the wrong directory.

```
error: orders.sexpr:14:5: no copybook at cpy/orders.cpy
  looked for /srv/orders/cpy/orders.cpy
```

The absolute path is on a continuation line rather than in a second span,
because `layout/SPEC.md` requires this diagnostic to carry no second span:
there is no file to point into, and a span invented for one would point at
nothing. A continuation line that names no place is what [the diagnostic
format](#standard-error-diagnostics) allows for exactly this case.

## Emitting the IR

`--emit-ir <dest>` writes the run's resolved descriptor (#20, #21, #149).
`<dest>` is a path, or `-` for standard output; a path is written in full or
not at all, so that nothing partial is ever left where another tool would read
it. `--emit-ir-format` selects the encoding: `binary`, the canonical protobuf
wire encoding a plugin is handed, or `json`, the normalized rendering a person
reads. `binary` is the default, because the default is the form the rest of the
system uses and the debug form is the one you ask for by name.

`--emit-ir-format` given without `--emit-ir` **MUST** be a usage error. It
selects the encoding of an emission that is not happening, and a flag on a
command line that reads as configuration and does nothing is the same fault an
unknown field in a manifest is (#40).

The bytes **MUST** be byte-identical to the bytes a generator would have been
handed for the same inputs, which is the equality the plugin contract rests
reproducibility on ([The descriptor](../plugin/SPEC.md#the-descriptor)). It
holds by there being one encoder rather than by two agreeing.

### Emitting replaces generation

`--emit-ir` **MUST** be terminal. cpybkc reads the manifest, resolves the
descriptor, writes it, and exits: no generator is resolved on `PATH`, no
generator is started, nothing is merged into the project's tree, and nothing is
pruned from it (#43, #45, #149).

The alternatives are worse in opposite directions. Emitting *alongside* a
generation run makes the debugging gesture — "show me the IR" — start every
generator in the project, which is minutes of work and a mutated output tree in
exchange for a file the user wanted on its own. Making it a subcommand costs
the subcommand set [One command](#one-command-and-no-subcommands) declines to
open. A flag that replaces the action is the reading that leaves nothing
surprising: the user asked for the descriptor, and cpybkc's answer is the
descriptor.

An emitting run therefore has no generators to fail, and its exit status is
about the emission alone.

### Which descriptor is emitted

A run resolves **one** descriptor, from the layout the manifest names and the
copybooks that layout names, and `--emit-ir` writes that one. No flag selects
among descriptors, because there is one — and nothing a project can write makes
there be two. A manifest names exactly one layout, a layout names the copybooks
its records are in, and those two facts are the whole of what the descriptor is
a function of.

That was an open question when this document was written, and #157 settled it by
changing the manifest rather than by giving the flag a selector. A generator
entry used to carry its own `inputs` — "copybooks this generator reads in
addition to the top-level `inputs`" — which read as though two generators could
be resolved against two different sets of copybooks and therefore handed two
different descriptors. They never could. A generator is invoked with
`--descriptor`, `--out` and its options and nothing else ([Invocation](../plugin/SPEC.md#invocation)),
no copybook path is among them, and the IR carries none either, so a list of
copybooks attached to a generator named files that generator never opened. Both
lists are gone from `cpybkc.json` (#40), and which copybooks a run reads is
settled where it was always decided — in the layout.

So the equality the plugin contract rests reproducibility on holds in its strong
form, and cpybkc **MUST** meet it that way: every generator of one run is handed
**the same bytes**, and they are the bytes `--emit-ir` writes for that run. Not
"a descriptor equivalent to" and not "a descriptor assembled the same way" — the
same bytes, because there is one descriptor and one encoder (#20). A plugin
contract statement of the same rule is at [The
descriptor](../plugin/SPEC.md#the-descriptor), and it is asserted by a test over
a real generator process rather than claimed by either document (#157).

## Standard output

Standard output is the data channel. cpybkc **MUST** write to it only what was
asked for by name:

- the descriptor, when `--emit-ir -` asked for it there;
- the version line, for `--version`;
- usage, for `--help`.

A run that generates writes **nothing** to standard output. No progress, no
timing, no summary, no count of files written, and no line saying a run
succeeded. Silence is success, and the exit status is the verdict.

cpybkc **MUST NOT** write a diagnostic to standard output under any
circumstance, and **MUST NOT** relay a generator's output there. The plugin
contract requires whatever a generator writes to its own standard output to be
surfaced verbatim and attributed ([Standard
streams](../plugin/SPEC.md#standard-streams)); cpybkc **MUST** surface it on
its own standard **error**, as [a relayed line](#a-relayed-generator-line),
because attribution means it is no longer verbatim on a stream and because
cpybkc's standard output belongs to whoever is reading it.

That rule is what keeps `--emit-ir -` pipeable. A descriptor going to standard
output shares that stream with nothing, so a progress line cannot land in the
middle of the wire encoding and produce a file that fails to decode at whatever
field the line happened to fall in — a failure that surfaces nowhere near the
mistake. The same rule, applied unconditionally rather than only while emitting,
is what makes it safe to redirect standard output from a container without
knowing what a run will do.

## Standard error: diagnostics

Everything cpybkc says about a run goes to standard error, one diagnostic per
line, encoded in UTF-8 (#150):

```
<severity>: <message>
```

with a second form for a line that came from a generator:

```
<severity>: <generator>: <message>
```

`<severity>` **MUST** be one of `error`, `warning` or `note` — the same closed
set of three, matched case-sensitively, with the same colon-and-single-space
separator as [the plugin diagnostic
format](../plugin/SPEC.md#the-diagnostic-format). That is deliberate and it is
the point of the section: a person watching one terminal sees one stream, in
one shape, whether a line was written by cpybkc or relayed from a generator, and
a script that greps for `^error: ` catches both.

Where a fault has a place, `<message>` **MUST** open with it, in
`file:line:column` form, followed by a colon and a space — or `file` alone
where a line number would point at nothing, as for a copybook declaring no
top-level item at all (#31). A fault implicating somewhere else **MUST** state
it on a continuation line beginning with exactly two spaces, carrying that
place and what is at it — or, where there is no place to name, carrying what it
has to say and nothing else:

```
error: orders.sexpr:22:9: ORDER-DETAIL declares no item OD-QTY
  cpy/orders.cpy:41:8: it declares OD-QUANTITY and OD-PRICE
```

A continuation line carries no severity and **MUST NOT** be read as a
diagnostic of its own; the two-space indent is what tells them apart, and it is
the same shape a compiler's notes take. cpybkc **MUST NOT** colour its output,
**MUST NOT** vary it by whether a terminal is attached, and **MUST NOT** quote
source lines back: a span an editor and a terminal both understand is what a
diagnostic owes its reader.

More than one fault found in one pass **MUST** be reported together rather than
one per run (#40, #150). A generated layout is generated wrong in the same way
in many places at once, and a reader that reports the first fault is a reader
run once per fault.

### A relayed generator line

The plugin contract requires cpybkc to parse a generator's diagnostics into its
structured log at the corresponding level, to surface any other line verbatim,
and to discard nothing ([The diagnostic
format](../plugin/SPEC.md#the-diagnostic-format)). What comes out on standard
error is that log, rendered in the form above, with the generator's name — as
the manifest spells it (#40) — standing between the severity and the message:

| The generator wrote | on | comes out as |
| --- | --- | --- |
| `error: <message>` | stderr | `error: <name>: <message>` |
| `warning: <message>` | stderr | `warning: <name>: <message>` |
| `note: <message>` | stderr | `note: <name>: <message>` |
| anything else | stderr | `warning: <name>: <line, verbatim>` |
| anything at all | stdout | `note: <name>: <line, verbatim>` |

A line cpybkc wrote itself carries no name, so the absence of one is what says
the line is cpybkc's. The severity is never changed on the way through: a
generator's `error:` is relayed as an `error:`, which is what makes the
generator's own wording reach the user rather than a summary of it.

The last two rows are the two the plugin contract argues rather than assumes.
An unrecognised line on standard error is usually a panic or a stack trace, and
it is relayed at **warning** — one level above the `note:` a plugin writes
deliberately, so that a reader skimming for the mildest severity does not find
it filed there. A line on standard output is untidiness rather than breakage —
the contract makes writing there a **SHOULD NOT** — so it is relayed at
`note:`.

Lines from one generator **MUST** appear in the order that generator wrote
them, and **MUST** be written as they arrive rather than held until the process
exits, so that a generator which explains itself and then hangs has still
explained itself. The relative order of lines from *different* generators is
unspecified: generators run concurrently (#42), and a rule fixing their
interleaving would be a rule making them queue.

### Severity does not decide the exit status

An `error:` diagnostic cpybkc wrote itself **MUST** fail the run. A `warning:`
or a `note:` **MUST NOT** change the exit status.

A relayed `error:` is different, and the difference is the plugin contract's:
the exit status is the verdict and the diagnostics are the explanation, so
cpybkc **MUST NOT** fail a run whose generator printed `error:` and then exited
zero, and **MUST** fail one whose generator exited non-zero having printed
nothing at all ([Exit codes and
diagnostics](../plugin/SPEC.md#exit-codes-and-diagnostics)).

### Determinism

The same inputs failing the same way **MUST** produce byte-identical standard
error, except for the interleaving of two generators' lines, which is
unspecified above. Diagnostics from one stage come out in the order that stage
reported them, and the stages run in the order the pipeline runs them.

The *wording* of a message is not part of this contract
([Compatibility guarantees](#compatibility-guarantees)); the format around it
is. A run may be reported in better words in a later release without that being
a breaking change, and a script depending on the words rather than on the
severity or the exit status has depended on something no document promised.

## Exit codes

Three statuses, and cpybkc **MUST NOT** exit with any other (#147):

| Status | Meaning |
| --- | --- |
| `0` | What was asked for was done. |
| `1` | The run failed. |
| `2` | The argument vector could not be understood. |

`0` covers a completed generation, a written descriptor, `--version` and
`--help`.

`1` covers every fault in the work itself: no manifest where one was named or
defaulted, a manifest that does not parse or does not validate, a layout that
does not parse or does not resolve, a copybook that is missing or does not
declare what a record names, a generator that does not resolve on `PATH`, a
generator that exits non-zero or is killed by a signal, two generators
colliding on one output path, a merge or a prune that fails, a descriptor that
cannot be written, and a run that is cancelled (#148, #149, #150).

`2` covers only the vector: an unrecognised flag, a flag repeated, a missing
value, a non-flag argument, and a flag combination this document refuses. A
status of 2 means cpybkc did nothing at all — no file was opened, no generator
was started, and the project's tree was not touched.

### Why three, and not one per stage

A status per failing stage looks more informative and is not. It would make
every new stage a compatibility question, it would have to answer which status
a run with a manifest fault *and* a layout fault carries, and it would be read
by almost nobody: a script branches on zero against non-zero, and a person
reads the diagnostic. The one distinction worth encoding is the one above —
whether cpybkc understood the request at all — because that is the failure a
caller can fix without knowing anything about the project.

Whatever a caller needs beyond that is on standard error, named, with a place
in a file attached to it.

### A plugin's exit status is not cpybkc's

cpybkc **MUST NOT** exit with a generator's exit status, and **MUST NOT**
attach meaning to a particular non-zero value a generator returned beyond
failure. A generator that exits 3, 42 or 127 fails the run, and the run's
status is `1`.

The two are different events. A generator's status is a verdict on one
invocation, delivered to cpybkc; cpybkc's is a verdict on the whole run,
delivered to whatever started it — and a run has several generators, so there
is no single status to propagate even when one might be wanted. The small
integers are also spoken for by parties neither document controls: a shell
exits 126 for a file it cannot execute and 127 for one it cannot find, and
reports a signalled process as 128 plus the signal number. cpybkc **MUST NOT**
exit with any of those values, so that a caller seeing one knows it came from
their shell and not from this program.

A generator terminated by a signal is reported distinguishably from one that
exited non-zero (#42) — in the diagnostic, where the difference is actionable,
because the first is usually a cancelled run or an out-of-memory kill and the
second is a bug in the generator.

### Cancellation

On `SIGINT` or `SIGTERM`, cpybkc **MUST** stop the run, **MUST** leave the
project's output tree exactly as it found it, **MUST** remove the scratch
directories and descriptor files it created ([The descriptor's location and
lifetime](../plugin/SPEC.md#the-descriptors-location-and-lifetime)), and
**MUST** exit `1` with an `error:` diagnostic saying the run was cancelled.

A second signal **SHOULD** be left to its default disposition, so that a run
holding a generator that will not exit can still be killed by repeating the
gesture that did not work.

Exiting `1` rather than dying by the signal is a departure from the convention
that a program killed by `SIGINT` should re-raise it, and it is deliberate: the
cleanup above is the reason the signal is caught at all, and a cancelled run is
a failed run by the definition already in the table. A caller that needs to
know *why* a run failed reads the diagnostic, exactly as for every other member
of status `1`.

## The environment

cpybkc reads one environment variable and no others:

- **`PATH`**, to resolve `cpybkc-gen-<name>` for each generator the manifest
  names (#41). Its rules are the plugin contract's, including that an empty
  element is not the working directory.

`TMPDIR` is **not** read, and neither is any other name for a system temporary
directory (#184). A run's scratch space — and the per-invocation directory the
descriptor is written into — is created inside the project cpybkc was pointed
at, so a run needs nothing writable outside the tree it is already writing.
Both are removed as the work that made them finishes, whatever the exit status;
a run killed outright leaves them, named so that what left them is plain. Where
that space is has no effect on what a run produces.

cpybkc **MUST NOT** define an environment variable of its own — there is no
`CPYBKC_*` — and **MUST NOT** take any part of a run's configuration from the
environment. Everything that decides what a run does is in the manifest or on
the command line, both of which a reviewer can read; a setting that lives in
the environment is absent from both, and two developers comparing generated
output cannot see the difference between their machines. It is the same rule
the plugin contract puts on a generator's own options, pointed at cpybkc
([The environment](../plugin/SPEC.md#the-environment)).

`SOURCE_DATE_EPOCH` is not an exception. cpybkc does not read it; it reaches a
generator because cpybkc passes its whole environment through unchanged (#47),
which is a promise about generators and not a setting of cpybkc's own.

## `--version`

`--version` **MUST** write exactly one line to standard output and exit `0`:

```
cpybkc 0.2.0 (IR version 1)
```

The line **MUST** name the program, its own version, and the IR version this
build produces. `<tool-version>` **MUST** be the released version for a build
made from a release tag and `0.0.0-dev` for one made outside a release, and
**MAY** carry SemVer build metadata after a `+`. cpybkc **MUST NOT** contact
anything to answer `--version`.

The IR version is on the line because of what a plugin's refusal says. A plugin
that will not read a descriptor **MUST** name three facts — the descriptor's IR
version, the highest it implements, and its own version ([What the refusal must
say](../plugin/SPEC.md#what-the-refusal-must-say)) — and the user reading that
refusal has to decide whether to upgrade the generator or pin the CLI. Two of
the three are in the message; the third is what the CLI in front of them
produces, and without a way to ask for it the next step is a guess. One line
answers it.

It is one line rather than several so that it can be read by an eye and by a
script without either needing a parser. What is *not* promised is the rest of a
build's provenance: there is no commit, no build date and no Go version on it,
because a version number is what identifies a release and the rest is
recoverable from it.

## Compatibility guarantees

**Covered.** Within a major version of cpybkc each of these holds, and a change
to any of them is a breaking change:

| Guarantee | Value |
| --- | --- |
| [The command set](#one-command-and-no-subcommands) | One command, no subcommands, no operands |
| [The flags](#the-argument-vector) | The five above: each keeps its name, its value, its default and its meaning |
| [Manifest discovery](#finding-the-manifest) | `--manifest`, else `cpybkc.json` in the working directory, and no search |
| [Path resolution](#a-path-is-relative-to-the-file-that-states-it) | A path in a file is relative to that file; a path on the command line is relative to the working directory |
| [`--emit-ir`](#emitting-the-ir) | Replaces generation; writes the run's one descriptor, which is the same bytes every generator of that run is handed |
| [Standard output](#standard-output) | Carries only what was asked for by name, and nothing during a generation run |
| [The diagnostic format](#standard-error-diagnostics) | The severity set, the separator, the generator's name, the two-space continuation indent, and the stream all of it goes to |
| [Exit codes](#exit-codes) | `0`, `1`, `2`, and no other value |
| [`--version`](#--version) | One line naming the program, its version and the IR version |

**Not covered**, and explicitly implementation detail. Depending on any of it
is depending on something that may change in a patch release, with no notice:

- The wording of any message, usage text included. The format is covered; the
  sentences inside it are not.
- The order in which independent diagnostics appear, which is deterministic
  within a version and may change between versions, and the interleaving of two
  generators' lines, which is not deterministic at all.
- Which stage of the pipeline reports a given fault, and how many diagnostics
  one fault produces.
- The location and naming of scratch directories and of the descriptor file a
  generator is handed, which the plugin contract already makes implementation.
- Whether generators run concurrently, in what order they are started, and how
  many run at once.
- The record a run keeps of what it generated — its name, its location and its
  contents (#45).
- Anything about the published image, which the [base-image
  contract](../container/SPEC.md) covers on its own terms and separately.

### How a covered thing would change

Not by moving it. A covered flag, value or status changes in a new major
version, and the transition **MUST** hold both forms simultaneously for at
least one full minor release of that new major: a renamed flag means both
spellings work, with the old one accepted and reported at `warning:` on use,
and removed no earlier than the following minor release.

Adding a flag whose absence leaves every existing command line behaving exactly
as it did is **not** a breaking change and **MAY** appear in a minor release.
Adding a *required* flag, adding an operand, changing a default, or narrowing
what an existing flag accepts all are, whatever they do to the flag table's
size.

The overlap is the point, and it is the same argument the container contract
makes for a moved path. A CI pipeline that runs cpybkc is in a repository this
project cannot see and cannot warn, and it fails with a message naming a flag
rather than a version. A release in which both spellings work turns that into a
deprecation somebody reads instead of a broken build somebody bisects.

## Out of Scope

### What is in the project manifest

The fields `cpybkc.json` carries, their types, their defaults and what is a
fault in one are **not specified here**. This document fixes only how the file
is located.

Reason: it is not a spec at all, deliberately (#40). The manifest is a
build-configuration file a *user* writes, documented in the
[README](../../README.md) and free to change with the CLI; the plugin contract
excludes it for the same reason from the other side ([The `cpybkc.json` project
manifest](../plugin/SPEC.md#the-cpybkcjson-project-manifest)). Specifying it
here would make a fifth published interface out of a file whose whole advantage
is that it is not one.

### What a generator receives

The `cpybkc-gen-<name>` naming convention, the `PATH` search, the argument
vector a generator is invoked with, the descriptor's lifetime, and what a
generator's exit status means are **not specified here**.

Reason: they are the [plugin contract](../plugin/SPEC.md)'s (#39), and the two
documents have different readers. This one is read by a person deciding what to
type; that one is read by an author implementing an executable. The seam
between them is stated in both directions where it matters — the diagnostic
format above, and the exit status under [A plugin's exit status is not
cpybkc's](#a-plugins-exit-status-is-not-cpybkcs) — and restating any more of it
would produce a second description for the two to disagree about.

### Configuration outside the argument vector

There is no configuration file of cpybkc's own, no user-level or system-level
settings file, no `CPYBKC_*` environment variable, and no flag that overrides
something the manifest states — no `--out`, no `--generator`, no
`--set opt=value`.

Reason: the manifest exists to be the reviewable record of how a project's code
was generated, and every one of those would make a run a function of something
the record cannot show. Two developers whose output differs would have no
diffable file to compare, which is the failure the manifest was introduced to
prevent (#40). The flags this document does define are the ones that name
*which* project to run and *what to do instead of running it* — neither of
which is a property of the project.

### Also out of scope

- **A verbosity flag.** No `--verbose`, `--quiet` or `--log-level`. Everything
  cpybkc writes is something a reader has to act on — a fault, a file it
  removed from their tree, or a line a generator wrote — so there is nothing
  to threshold. The repository's diagnostic vocabulary carries no severity for
  the same reason (`internal/diag`): a line nobody has to act on teaches a
  reader to skip the others.
- **A machine-readable log.** No `--log-format json`. A consumer that wants
  structured data out of cpybkc wants the descriptor, and `--emit-ir` is that
  (#20, #21); a JSON rendering of progress lines would be a second interface
  serving nobody who has the first.
- **A concurrency flag.** No `--jobs`. A manifest declares a handful of
  generators, they are independent processes with disjoint directories, and
  they all run (#42).
- **Watch mode, incremental builds and caching.** A run is a function of its
  inputs and produces the same tree every time (#47); deciding when to make one
  belongs to whatever already decides when to run a build.
- **Shell completion and man pages.** Generated surface with its own
  compatibility questions, over a flag set small enough to be read in one
  screen.
- **Colour and terminal detection.** Stated as a **MUST NOT** above rather than
  left open, so that output is the same in a terminal, in a pipe and in a CI
  log.
- **Anything about the published image** — its entrypoint's value, its user,
  its `PATH`, its tags — which is the [base-image
  contract](../container/SPEC.md)'s (#54, #55).

## Appendix: Mapping to Stories

| Section | Implemented by |
| --- | --- |
| [One command, and no subcommands](#one-command-and-no-subcommands) | #147 `cli`; the entrypoint promise it answers to, #55 `container` |
| [The argument vector](#the-argument-vector) | #147 `cli` |
| [Finding the manifest](#finding-the-manifest) | #148 `cli`, over the manifest reader #40 `plugin` |
| [A path is relative to the file that states it](#a-path-is-relative-to-the-file-that-states-it) | #148 `cli` |
| [Finding the inputs](#finding-the-inputs) | #148 `cli`; the diagnostics it owes, #31 `resolve` and #150 `cli`; which copybooks a run reads, and that the manifest does not list them, #157 `cli` |
| [Emitting the IR](#emitting-the-ir) | #149 `cli`, over the encoders #20 and #21 `ir` |
| [Which descriptor is emitted](#which-descriptor-is-emitted) | #157 `cli` decided it — one descriptor per run, and the manifest's `inputs` removed to keep it true; #148 `cli` assembles it, #149 `cli` writes it |
| [Standard output](#standard-output) | #147 `cli` for the version and usage, #149 `cli` for `--emit-ir -` |
| [Standard error: diagnostics](#standard-error-diagnostics) | #150 `cli`, over `internal/diag` and the spans of #31 `resolve` |
| [A relayed generator line](#a-relayed-generator-line) | #150 `cli`, over the classification #42 `plugin` already performs, against the format #39 `plugin` fixes |
| [Exit codes](#exit-codes) | #147 `cli` for the vector's own status and the single place they are emitted from, #148 and #149 `cli` for the failures that reach them |
| [Cancellation](#cancellation) | #148 `cli` |
| [The environment](#the-environment) | #41 and #43 `plugin` for the two variables read, #147 `cli` for reading no others |
| [`--version`](#--version) | #147 `cli`; the IR version it reports, #17 `ir` |
| [Compatibility guarantees](#compatibility-guarantees) | #146 `cli` — decided here, in the shape #54 `container` uses for the image |
| The project manifest — out of scope, see above | #40 `plugin` |
| This document | #146 `cli` |
| Conventions this document follows | #15 `setup` |
