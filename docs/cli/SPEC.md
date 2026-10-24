# The Command-Line Interface

## Overview

cpybkc is one executable a person runs. It finds the project's manifest, reads
the layout that manifest names, resolves it against the record descriptions
that layout holds or names — copybooks, definitions files and blocks — and
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
`--emit-ir` does to a run; what `init` derives from a copybook, what it leaves
to the adopter and where it writes it; what standard output and standard error
carry and in what form; the exit statuses and what each one means; the
environment cpybkc reads; what `--version` prints; and how much of that a caller
may depend on across releases.

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
  states, and the source of three questions it hands to the CLI by name: where a
  copybook path is looked for, where a layout's forms are read from, and where
  a member a copybook's `COPY` names is found. All three are answered in
  [Finding the inputs](#finding-the-inputs). It is normative a
  second time for [`init`](#init-scaffolds-a-layout-from-copybooks): which forms
  a copybook decides, which it cannot, and what a file has to carry before a
  reader accepts it are all that document's, and the scaffold is held to them
  rather than to a second description here (#183).
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

## One command, and one subcommand

cpybkc **MUST** be a single command whose default action is generating. Running
it with no subcommand generates, and generating **MUST NOT** be given a name of
its own: there is no `generate`, no `emit`, no `plugin install` (#147, #148).

There is exactly one named subcommand,
[`init`](#init-scaffolds-a-layout-from-copybooks), which scaffolds a layout from
copybooks (#183). The set is closed at that one member, and a second is a change
to this section rather than an addition somebody makes on the way past.

**The default action keeps the bare form**, and that is the whole of what the
subcommand costs a caller: nothing. Requiring `cpybkc generate` would break
every command line already written and every derived image already published,
because the image's `Entrypoint` **is** this CLI and its `Cmd` is empty ([the
entrypoint
promise](../container/SPEC.md#the-entrypoint), #55) — `docker run image` would
become a usage message for everyone who had followed the instruction to leave
`Cmd` alone. It would also charge the whole existing user base for a subcommand
one of them asked for. The bare form is what those callers already type, so
it is what it keeps meaning.

What that costs *this document* is a breaking change to a covered row. Below
1.0.0 [any release may make one](../container/SPEC.md#below-100-any-release-may-break-a-covered-thing)
(#398), and what the same change would cost from 1.0.0 is written down under
[How a covered thing would change](#how-a-covered-thing-would-change) rather
than left for whoever implements the next subcommand to discover.

### The first operand is a subcommand name

A subcommand name is read at exactly one position, and it is the **first
argument**. One rule covers the whole vector:

- Where the first argument is `init`, the line is that subcommand's, and every
  argument after it is read against [the vector `init`
  takes](#the-vector-init-takes).
- Where it is anything else — a flag, `--`, or nothing at all — the line is the
  default action's, and a non-flag argument anywhere in it **MUST** be a usage
  error naming it ([Exit codes](#exit-codes), status 2).

The set of subcommand names is closed at `init`, so a first argument that is a
non-flag and is not `init` **MUST** be a usage error naming it, and cpybkc
**MUST NOT** open anything before reporting one. Under either branch cpybkc
**MUST NOT** accept a second operand, wherever it appears.

It is one rule at one position rather than "the first argument that is not a
flag" because the two are not the same rule and they disagree on lines somebody
will type. `cpybkc -- init` and `cpybkc --manifest x.json init` both have `init`
as their first *non-flag* argument, and under this rule both are the default
action with an operand in them — a usage error naming `init`. That is the
answer this document wants: `--` is the end of options and has never introduced
anything, and a subcommand that could hide behind another action's flags would
make "which flags belong to what" a question with no answer on the line itself.

Reading it at one fixed position is also what keeps the vector readable without
such a rule: everything after the head belongs to the subcommand the head names,
or to the default action where the head is not a subcommand name. A line that
could put the verb anywhere would need this document to answer whether
`--copybook` before `init` is `init`'s flag or a flag the default action does
not have, and either answer is one somebody has to look up.

That the door was open at all is the reserved operand position's doing, and it
is the reason this is an addition rather than a redesign. A CLI that had spent
its first operand on a path — `cpybkc ./cpybkc.json` — could never have added a
subcommand without deciding whether an argument is a file or a verb. It cost one
flag ([`--manifest`](#finding-the-manifest)), typed in the uncommon case and not
at all in the common one, and the door it kept open is now spent on `init`.

`--` is still honoured as the end of options, so that cpybkc behaves the way its
neighbours on the system do. It is not a way to reach a subcommand — after it
every argument is an operand, and the default action has none — so `cpybkc --
init` is the usage error the rule above makes it, exactly as `cpybkc -- foo`
always was.

## The argument vector

```
cpybkc [--manifest <path>] [--emit-ir <dest> [--emit-ir-format <format>]]
cpybkc init --copybook <path> … [--copy-member <member>=<path> …] --out <path>
cpybkc --version
cpybkc --help
```

That is the whole surface (#147, #183). The default action's flags are these:

| Flag | Value | Default | May repeat |
| --- | --- | --- | --- |
| `--manifest` | a path to the project manifest | `cpybkc.json` in the working directory | no |
| `--emit-ir` | a path, or `-` for standard output | absent — nothing is emitted | no |
| `--emit-ir-format` | `binary` or `json` | `binary` | no |
| `--version` | none | — | no |
| `--help`, `-h` | none | — | no |

With no subcommand named, cpybkc **MUST NOT** accept a flag this table does not
list, and an unrecognised flag **MUST** be a usage error naming it. There is no
`--include`, no `--jobs`, no `--verbose` and no `--config`; each is refused with
its reason in [Out of Scope](#out-of-scope). `--copybook`, `--copy-member` and
`--out` are [`init`'s](#the-vector-init-takes) and are usage errors here, named
the same way.

`init`'s own flags are in [The vector `init` takes](#the-vector-init-takes). No
flag that names an **input** belongs to both sets, so a flag on a line is either
one the head admits or a usage error naming it and the subcommand it was written
under. The two sets share exactly one spelling — `--help` and its `-h` — which
belongs to neither action in particular for the reason the next subsection
gives: it, and `--version`, are answered before the vector is interpreted at
all.

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

A flag whose table says it may not repeat and which is given twice **MUST** be a
usage error, even where both occurrences carry the same value. Silently taking
the last is the rule most argument parsers default to, and it makes
`cpybkc --manifest a.json --manifest b.json` a command that reads a file the
line also names as something else.

It is the rule the manifest reader already applies to a field written twice
(#40), for the same reason: the file, and the command line, are things a person
wrote, and a duplicate is something they did not mean rather than a precedence
question for the program to answer on their behalf.

[`--copybook`](#where-the-copybooks-come-from) is the one flag that repeats, and
it is not an exception to the reasoning above but an application of it. Its
occurrences are a list rather than one value stated several times, so a second
occurrence adds a copybook instead of replacing one, and nothing the person
wrote is discarded. What the rule was for survives there too: two occurrences
carrying **equal** values are a duplicate and **MUST** be a usage error,
because naming one copybook twice states nothing the line did not already
state.

### `--help` and `--version` are answered before anything else

When `--help` is present, cpybkc **MUST** write usage to standard output and
exit 0. When `--version` is present and `--help` is not, it **MUST** write the
version line to standard output and exit 0. In both cases every other flag on
the line **MUST** be ignored, including one that would otherwise be a usage
error, and no manifest is read.

**`--help` and `--version` are answered under every subcommand**, and neither is
a flag the tables below list. They are read before the first argument is
classified at all, so no rule about which flags an action carries reaches them:
`cpybkc init --help` and `cpybkc init --version` are answered, not refused, and
so is `cpybkc bogus --help`, which writes the top-level usage and exits 0 rather
than complaining about `bogus`. That last case is the same courtesy this
subsection already extends to a flag the user was in the middle of getting
wrong, and an unrecognised verb is the commonest way to arrive here.

What the subcommand does change is *which* usage `--help` writes: cpybkc
**MUST** write the named subcommand's where the first argument is one, and the
top-level usage otherwise. `--version` **MUST NOT** vary that way — a build has
one version whichever action was going to run — so a subcommand on a `--version`
line is ignored along with everything else.

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
  (#40); a layout's `copybook` path is relative to the layout (#148), and so are
  the path a text `definitions` argument states (#399) and the path a
  `copy-member` form states (#407).
- A relative path **typed on the command line** — `--manifest`, `--emit-ir`,
  and `init`'s `--copybook` and `--copy-member` —
  is resolved against the working directory, because that is the directory the
  person typing it was standing in.

cpybkc **MUST** resolve paths this way and **MUST NOT** resolve one against
anything else — not against a search path, not against a variable, not against
the project root.

The first half is what makes a layout portable. A layout and the copybooks and
definitions files it names travel together, so a layout naming `cpy/orders.cpy` means the copybook
beside it wherever the pair is checked out, and a project that vendors somebody
else's layout does not have to rewrite the paths inside it. Resolving a
copybook against the *manifest* instead would make one layout mean different
files in two projects, which is the one thing a description of a data file
cannot afford.

### Finding the inputs

[`layout/SPEC.md`](../layout/SPEC.md) hands three questions to the CLI by
name, and this section answers them — and then a fourth, which nothing had
asked until a manifest and a layout each looked like a place a copybook could
be named from.

**Where a copybook is looked for.** At the path its layout states, resolved as
above, and nowhere else. cpybkc **MUST NOT** search a list of directories:
there is no `--include`, no `-I`, no `COPYPATH`, and no implicit directory.
A search path makes the same layout resolve to different copybooks on two
machines with no difference between them that any file records, which is the
failure a generated file library cannot detect and cannot recover from — the
code compiles, and the offsets are wrong.

**Where a definitions file is looked for.** Exactly where a copybook is: at the
path its layout states, resolved as above, and nowhere else. Every argument
against a search path above holds for a file of entries as it does for a
copybook, and a definitions file exists precisely so that what a run reads is
checked in.

**Where a member named by `COPY` is found.** At the path the layout's
[`copy-member`](../layout/SPEC.md#a-copy-member-is-found-where-the-layout-says)
form for that member states, resolved as above, and nowhere else. A
`COPY STDHDR.` in a copybook is a *name*, not a path, and the layout is what
says which file the name is: there is no directory a member is looked for in
and no suffix tried after its name, so no difference between two machines can
make one `COPY` read two files. [Where a `COPY` member is
found](#where-a-copy-member-is-found) states the rest and argues it.

**Where a layout's forms are read from.** From the one file the manifest's
`layout` names. Composing a layout's own forms from several files is out of
scope, with its reason, in `layout/SPEC.md`'s [Out of
Scope](../layout/SPEC.md#also-out-of-scope).

**Which files a run reads**, and the answer #157 owed this section: the layout,
plus exactly the copybooks and definitions files its layout's `record` forms
name, plus the member file of each `copy-member` form a `COPY` in one of those
copybooks reaches, and no others. A `definitions-block` opens nothing — it is a
form of the layout, already read — and only the text form of `definitions` is a
path; a symbol there names a block. A definitions file names no further file
([What a record may reach](../layout/SPEC.md#what-a-record-may-reach)), so that
list stops one step from the layout. A copybook's `COPY` names a member, and a
member may itself write `COPY`; each of those names is looked up among the
layout's `copy-member` forms, so every file the chain reaches is still a file
the layout names. Several records naming one path read it once, and a member
copied from several places is read once.

The manifest does not list them. It names the layout, the generators and their
options — the things a layout has no opinion about — and it carries no `inputs`
(#40), so there is no set a layout may not name a copybook or a definitions
file outside of, and no diagnostic for naming one. The only fault about either
here is a path cpybkc cannot open.

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

A definitions file cpybkc cannot open is reported in the same shape, naming
the path as the layout spells it and the absolute path cpybkc looked for:

```
error: ledger.sexpr:22:5: no definitions file at posting.cpy.sexpr
  looked for /srv/ledger/posting.cpy.sexpr
```

A member file cpybkc cannot open is reported in the same shape again, naming
the path as the `copy-member` form spells it, with the span at that path in
the layout:

```
error: orders.sexpr:3:22: no copy member at cpy/stdhdr.cpy
  looked for /srv/orders/cpy/stdhdr.cpy
```

None of these lines ever applies to a block, which is in the layout and is
never opened.

The absolute path is on a continuation line rather than in a second span,
because `layout/SPEC.md` requires this diagnostic to carry no second span:
there is no file to point into, and a span invented for one would point at
nothing. A continuation line that names no place is what [the diagnostic
format](#standard-error-diagnostics) allows for exactly this case.

### Where a `COPY` member is found

A copybook that writes `COPY STDHDR.` is read with `STDHDR`'s text in place of
the statement — that is what `COPY` means, and `cobol-go` does the expanding
(`cobol.WithCopyBooks`). What `cobol-go` cannot know is which file `STDHDR` is.
It asks, through a resolver it is handed, and cpybkc's answer **MUST** be the
file a [`copy-member`](../layout/SPEC.md#a-copy-member-is-found-where-the-layout-says)
form names for that member, and no other:

```
(copy-member STDHDR "cpy/stdhdr.cpy")
(copy-member ORDDTL "cpy/orddtl.cpy")
(record ORDER-DETAIL (copybook "cpy/order-detail.cpy" ORDER-DETAIL-REC))
```

The path is resolved [relative to the
layout](#a-path-is-relative-to-the-file-that-states-it), as every path a layout
states is. A `COPY` naming a member no `copy-member` form names is a fault,
not a cue to look somewhere else.

**There is no search, and no convention for the name.** The resolvers
`cobol-go` ships are searches: `FSCopyBooks` tries the text-name as written,
upper-cased and lower-cased, each bare, with `.cpy` and with `.CPY`, in a
directory it is given. Handing it the copybook's own directory, or the
layout's, is the implicit directory [Finding the
inputs](#finding-the-inputs) refuses, and its up to nine candidates are a
search path compressed into one directory: which of `STDHDR`, `stdhdr.cpy` and
`STDHDR.CPY` a run reads would turn on which of them happens to exist, on a
filesystem whose case-sensitivity no file records. The failure is the one that
section names — the same layout resolving to different copybooks on two
machines, compiling, and producing the wrong offsets. A member is named in the
layout instead, by its path, once.

**A copybook's `COPY` line is not edited to make it findable.** `COPY
'cpy/stdhdr.cpy'.`, with a literal path, would let the copybook say where its
member is. It is not the answer, because the copybooks that compose with `COPY`
are mostly not the adopter's to edit — a vendor's member that copies another is
read as the vendor wrote it — and because the composing lines are usually
copied from a program's `FD`, where they say `COPY STDHDR.`
([Composition](../layout/SPEC.md#composition-is-the-copybooks-and-copy-is-where-it-is-written)).
A `COPY` written with a literal is still a name to cpybkc, matched against
`copy-member` forms like any other.

**One member map per run, stated in the layout.** Every copybook the run reads,
and every member they copy, is expanded against the same `copy-member` forms —
the layout's. That is the shape of a compile, where one library serves every
`COPY` in the program, and it is what lets a member name mean one file
throughout a run: a member copied by six record types' copybooks is one
statement in the layout, not six that could disagree. Why the forms are
layout-wide rather than written inside each `copybook` source is the layout
format's to argue, and [it
does](../layout/SPEC.md#a-copy-member-is-found-where-the-layout-says).

**Each member file is read once per run**, however many `COPY` statements name
it, and the bytes read are the bytes every expansion of it uses. A member is
read in the reference format of the copybook copying it, which for every
copybook cpybkc reads is fixed format; a member nested inside another is
expanded through the same forms, and a chain that returns to a member already
open is `cobol-go`'s `CopyBookCycleError`, reported at the `COPY` that closes
it with the chain on continuation lines. `REPLACING` is `cobol-go`'s too: it
is applied as `cobol-go` applies it, and nothing here restates it.

**The run and `init` read members through one resolver.** A run builds the
member map from the layout's `copy-member` forms; [`init`](#where-the-members-come-from),
which has no layout, builds it from its `--copy-member` flags. Either way the
map is handed to one resolver — the only code in cpybkc that turns a `COPY` into
a file — so that a copybook converted by `init` and the same copybook read by a
run are expanded by the same rules into the same entries, and [Three sources,
one descriptor](../layout/SPEC.md#three-sources-one-descriptor) holds for a
composed record as it does for any other. The resolver, the `copy-member` form
and `--copy-member` are introduced together, in one change (#423), and that
change adds a composed copybook to the round trip every copybook cpybkc reads
goes through (#401) and to the descriptor-equality suites for a definitions
file (#405) and a block (#409).

#### What a `COPY` fault looks like

Each of these is a diagnostic in [the ordinary
format](#standard-error-diagnostics), and each names the member the way the
copybook spells it:

- **A `COPY` naming a member no `copy-member` form names.** The span is at the
  `COPY` statement, in the file that writes it, and a continuation line lists
  the members the layout does name:

  ```
  error: cpy/order-detail.cpy:3:12: COPY ORDDTL names no copy-member in the layout
    the layout names STDHDR
  ```

- **A member file that cannot be opened.** In the shape [Finding the
  inputs](#finding-the-inputs) gives every missing file: the path as the
  `copy-member` form spells it, the span at that path, and where it was looked
  for.
- **A cycle.** At the `COPY` that closes it, with the chain of members open at
  that point, outermost first.

A fault **inside** a member — an entry `cobol-go` refuses, an item a reference
names that does not satisfy it, any diagnostic whose second span
[points into a record's
description](../layout/SPEC.md#every-diagnostic-carries-a-span-and-some-carry-two)
— **MUST** point into the member, at the line and column the entry has there,
and name the member as its `copy-member` form spells it. It carries one more
continuation line per `COPY` statement that reached it, innermost first, each
at that statement, so the reader can tell which expansion it was:

```
error: orders.sexpr:22:9: ORDER-DETAIL declares no item STD-TYPE
  cpy/stdhdr.cpy:3:12: it declares STD-REC-TYPE and STD-REC-LEN
  cpy/order-detail.cpy:2:12: copied by COPY STDHDR
```

A span into the copybook that wrote the `COPY`, at the position the entry has
in the member, would point at a line that says something else.

**This rests on member provenance from `cobol-go`** (Zaba505/cobol-go#153). A
`cobol.Pos` from expanded text names the library text it is in — by the
text-name and library the resolver was asked for — and the chain of `COPY`
statements that reached it, and it is a position in that text as written, not
as a `REPLACING` phrase left it; every positioned error, `copybook.Build`'s
and `NewLayout`'s included, carries the same. That is what lets a span name
the member's file as its `copy-member` form spells it, at the entry's own line
and column there, with one continuation line per `COPY`. #423 moved the pin to
it.

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
exchange for a file the user wanted on its own. Making it a subcommand would
spend a second member of the set [One command, and one
subcommand](#one-command-and-one-subcommand) keeps at one, and buy nothing the
flag does not: an emitting run is a run over the project the manifest names,
with the same inputs and the same resolution, differing only in what comes out
at the end. A flag that replaces the action is the reading that leaves nothing
surprising: the user asked for the descriptor, and cpybkc's answer is the
descriptor.

`init` is the other side of that test, and it is why one of them is a flag and
the other a verb. It reads no manifest, resolves nothing, and takes inputs this
vector has no other flag for; there is no run for it to replace.

An emitting run therefore has no generators to fail, and its exit status is
about the emission alone.

### Which descriptor is emitted

A run resolves **one** descriptor, from the layout the manifest names and the
record descriptions that layout holds or names — the copybooks and definitions
files its records name, and the `definitions-block` forms it holds — and
`--emit-ir` writes that one. No flag selects among descriptors, because there is
one — and nothing a project can write makes there be two. A manifest names
exactly one layout, a layout holds or names the descriptions its records are
in, and those two facts are the whole of what the descriptor is a function of.

Which source holds a description is not among them. A record bound to a
copybook, to the definitions file `init` converts from it, or to a block
holding that file's entries **MUST** resolve to byte-identical descriptors,
which is `layout/SPEC.md`'s [Three sources, one
descriptor](../layout/SPEC.md#three-sources-one-descriptor); so moving a
record's entries from one to another is checked with two `--emit-ir` runs and a
byte comparison, and changes nothing a generator is handed.

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

## `init` scaffolds a layout from copybooks

```
cpybkc init --copybook <path> … [--copy-member <member>=<path> …] --out <path>
```

`init` reads the copybooks it is given, writes one [definitions
file](../layout/SPEC.md#definitions) per copybook and a layout scaffold whose
records name them, and exits. It resolves no layout, starts no generator and
reads no manifest (#183). Paths it is handed are still resolved in the ordinary
way; what it does not do is the resolution the rest of this document means by
the word — record descriptions against a layout, into a descriptor.

An adopter runs it once over their copybooks and commits what it writes in
place of the copybooks. When the vendor ships a new copybook, they run it again
into a scratch directory and diff ([When a copybook changes, and moving a
record between sources](#when-a-copybook-changes-and-moving-a-record-between-sources)).
That is what makes a copybook that cannot be committed — a vendor's, under a
licence that keeps it out of the repository — no obstacle to regenerating from
checked-in files (discussion #394).

What it is for is the split in the next subsection. A layout and its record
descriptions are two kinds of statement, and only one of them is knowledge the
adopter has. The descriptions are the copybook's, and `init` writes them whole,
into definitions files beside the layout; the layout states what a copybook
cannot, and `init` writes the questions it raises and none of the answers.
Without `init` all of it is typed by hand, and the half that is not the
adopter's is the half they cannot check by reading.

### What a copybook decides, and what only the adopter can

**Derived, and written into the scaffold complete:**

| Form | Where it comes from |
| --- | --- |
| a definitions file | one per copybook named, holding every entry and clause of it ([What the scaffold states, form by form](#what-the-scaffold-states-form-by-form)) |
| `record` | one per record type each named `01`-level resolves to, in each copybook named, less what is [noted](#how-a-combinations-record-name-is-chosen) |
| `alternative` | one per `REDEFINES` outside a repeating group, on each record ([Which alternative a record is](../layout/SPEC.md#which-alternative-a-record-is)) |

**Derived as far as a copybook goes, and left commented** — the form and its
subject are computable, the value is not:

| Form | What is computable | What is not |
| --- | --- | --- |
| `rename` | which records need one, and which record each names | the substitute |
| `discriminate-variant` | the variant, and one `arm` per alternative | what each arm tests |
| `copybook-reading` | whether a copybook carries an `OCCURS DEPENDING ON` at all | which reading wrote the data |

**Not derivable at all**, and the whole of what an adopter knows that a copybook
does not: [`encoding`](../layout/SPEC.md#the-encoding-profile), whose four axes
have no default for any; [`framing`](../layout/SPEC.md#physical-framing), with
`lrecl`, `max-segment`, `delimiter` and `placement` behind the spelling;
[`discriminate`](../layout/SPEC.md#discrimination); and
[`sequence`](../layout/SPEC.md#sequencing).

`example/ledger/posting.cpy` is the measure, and the arithmetic is `init`'s
output for it. That one copybook scaffolds six `record` forms and twelve
`alternative` children — thirty-odd lines, every one of them recoverable from
the copybook and none of them a thing the adopter has to know — against the
`discriminate` form
each of those six record types needs and the one `sequence`, which are the file's
actual description and are recoverable from nothing. The six commented `rename`s
sit between the two: which records need one is recoverable, and the substitutes
are a reading of what the file *means*, so `init` writes the question and not the
answer. Beside the layout it writes `posting.cpy.sexpr`, the copybook's
seventeen entries transcribed, and all six records name it. What `init` claims
is that first column and nothing beyond it.

The measure is stated against the copybook rather than against
`example/ledger/ledger.sexpr`, because the layout beside it names **two** of
those six combinations — the two whose `alternative`s name a `REDEFINES` rather than a base
description, which are the two a mainframe-produced extract carries. That is not
`init` having derived too much. A copybook cannot say which combinations occur in
a file, so `init` **MUST** emit all six and is right to; which of them the data
holds is the adopter's knowledge, and narrowing to it is the layout's job. See
[the ledger example](../../example/ledger/README.md) for the same point from the
layout end.

### The vector `init` takes

| Flag | Value | Default | May repeat |
| --- | --- | --- | --- |
| `--copybook` | a path to a copybook | none; at least one **MUST** be given | yes |
| `--copy-member` | a member a `COPY` names, `=`, and the path of its file | none | yes |
| `--out` | a path, naming the layout | none; **MUST** be given | no |

`--copybook` is unchanged: repeatable, and given at least once. `--copy-member`
says where a member a copybook's `COPY` names is found, once per member, and
is needed only for a copybook that writes `COPY` ([Where the members come
from](#where-the-members-come-from)). `--out` names
the layout and nothing else; it is never `-`
([Where the scaffold is written](#where-the-scaffold-is-written-and-why-nothing-is-ever-overwritten)).

**`init` MUST write one definitions file per `--copybook`, beside the layout.**
Beside means in the directory `--out` names the layout in. A definitions file's
name **MUST** be the final element of the `--copybook` value as it was typed,
with `.sexpr` appended: `--copybook cpy/posting.cpy` gives `posting.cpy.sexpr`,
and `--copybook vendor/PXTRACT` gives `PXTRACT.sexpr`. Nothing about the name
is chosen by cpybkc beyond the suffix — the extension the copybook carried, or
the absence of one, is kept — so the adopter can always tell which copybook a
definitions file came from by reading its name, and the name is the same on
every run over the same command line.

Each name is written into the layout's records bare, as `(definitions
"posting.cpy.sexpr" …)`. A path in a layout is [relative to the
layout](#a-path-is-relative-to-the-file-that-states-it), and the file is beside
it, so the path the scaffold states is right wherever the two are moved
together.

**No flag names the definitions files.** Where they go and what they are called
both follow from `--copybook` and `--out`, and a flag that named them
separately would be a second place to say what the layout's records already
say, for the two to disagree about. (`--copy-member`, added by #407, names an
input and not an output: where a member is read from is knowledge a command
line has to carry, because `init` has no layout to read it from.) `init` writes no `definitions-block`: the
owner decided on discussion #394 that `init` writes definitions files, and
#399's answer to it says so. Moving a file's entries into the layout is an
edit the adopter makes, and [When a copybook changes, and moving a record
between sources](#when-a-copybook-changes-and-moving-a-record-between-sources)
writes it down.

**Two derivations are a usage error.** Both are decidable from the vector
before anything is opened, and both **MUST** be reported naming both of the
values involved, with status 2:

- two `--copybook` values whose derived names are equal under Unicode simple
  case folding — `a/posting.cpy` and `b/posting.cpy`, or `cpy/POSTING` and
  `vendor/posting`;
- a derived name equal, under the same folding, to the layout's own name, the
  final element of `--out` — `--copybook vendor/layout --out layout.sexpr`
  derives `layout.sexpr` for the definitions file.

Case folding is the comparison because the files are written into one
directory, and on a case-insensitive filesystem two names differing only in
case are one file: the second link would find the first's name taken, so one
command line would fail on one machine and succeed on another. Refusing the
pair on every filesystem keeps the outcome a function of the command line.

The workaround for two copybooks that share a final path element is two `init`
runs, each into a scratch directory of its own, then renaming one of the two
definitions files and changing the path in every `definitions` source that
names it. A rename by cpybkc — a numeric suffix, a directory prefix folded into
the name — would be a name derived from the order of the flags or from a
directory the layout never mentions, and it is the adopter, not cpybkc, who
knows which of the two copybooks should keep the name.

`init` **MUST NOT** accept a flag this table does not list. `--manifest`,
`--emit-ir` and `--emit-ir-format` are the default action's and **MUST** be
usage errors under `init`, naming the flag and the subcommand it was written
under rather than reporting it as unrecognised — it is a real flag of this
program, in the wrong place, and a message saying otherwise sends the reader to
check their spelling.

`--help`, `-h` and `--version` are not flags this table lists and are not
refused by that rule: they are [answered before the vector is
interpreted](#--help-and---version-are-answered-before-anything-else) under
every subcommand, and `cpybkc init --version` prints the version line.

[Flag form](#flag-form) and the rest of [The argument
vector](#the-argument-vector) apply unchanged: both value spellings are
accepted, `--copybook` and `--copy-member` are the two flags that repeat, and a
missing value is a usage error.

### Where the copybooks come from

Every copybook is named by a `--copybook` flag, repeated once per file. A layout
is what names a project's copybooks, and there is no layout yet, so this is the
one input `init` cannot resolve the ordinary way.

A repeatable flag rather than operands after the subcommand, for two reasons.
Operands would spend the position a *second* subcommand's own subject would
need, one release after this document spent the first; and it would make `init`
the only action in this vector taking an input that is not named by a flag, so
that the rule a reader has just learned holds everywhere but here.

A value is a path, resolved against the working directory as [every path typed
on the command line is](#a-path-is-relative-to-the-file-that-states-it). It is
written **as it was typed** into the header comment of the definitions file
converted from it, and into every diagnostic and every note about it — a path
cpybkc rewrote on the adopter's behalf would be one they cannot find in what
they typed. No record names the copybook itself: each names the definitions
file beside the layout, so no path in the scaffold depends on the directory the
command was typed in.

A value **MUST NOT** be `-`. A definitions file is named after its copybook, and
a copybook arriving on a stream has no name to name one after; the refusal is
decidable from the vector alone, so it is a usage error and nothing is opened.

A value naming a **directory MUST** fail the run ([Exit codes](#exit-codes),
status 1), with a diagnostic naming the path as it was typed. It is a status 1
rather than a 2 because it is not decidable from the vector — cpybkc has to look
at the path to learn what is there — and it is stated separately from a copybook
that cannot be opened because a directory opens perfectly well. Reading one
would make the scaffold a function of a directory's contents, so a copybook
dropped in
beside the others later changes what `init` produces with no file recording the
difference — which is the failure [Finding the inputs](#finding-the-inputs)
refuses a search path for, arriving by a different door. There is no extension
convention to fall back on either: `.cpy`, `.cbl`, `.cob` and no extension at
all are all in use, so any rule for which files in a directory are copybooks is
one cpybkc invented and no adopter can predict. A shell already expands a glob
into the flags, in a line a reviewer can read.

Whether two `--copybook` values name one file on disk is **not** decided.
Byte-equal values are a duplicate and a usage error ([A flag appears at most
once](#a-flag-appears-at-most-once)); two different spellings that resolve to
one file are two copybooks as far as `init` is concerned. Where the two share a
final path element — `cpy/posting.cpy` and `./cpy/posting.cpy` — their derived
names are equal and the vector is refused [before anything is
opened](#the-vector-init-takes). Where they do not — a symlink by another name
— each gets its own definitions file, and every `01`-level the two declare
derives one record name twice, which is a [derived record-name
collision](#how-a-combinations-record-name-is-chosen): each colliding record is
noted, none of them is written, and the run exits 0 with both definitions files
written. That is the same refusal to
compare spellings that [Finding the inputs](#finding-the-inputs) argues at
length, and for the same reason: behind a symlink, a bind mount or a
case-insensitive filesystem there is no comparison that is right every time.

### Where the members come from

A copybook that writes `COPY` is converted as its **expanded** entries, as
[Composition](../layout/SPEC.md#composition-is-the-copybooks-and-copy-is-where-it-is-written)
requires of a definitions file, so `init` has to read every member its `COPY`
statements name. A run learns where a member is from the layout's
`copy-member` forms ([Where a `COPY` member is
found](#where-a-copy-member-is-found)); `init` has no layout, and learns it
from `--copy-member`, one per member:

```
cpybkc init --copybook cpy/order-detail.cpy \
  --copy-member STDHDR=cpy/stdhdr.cpy --copy-member ORDDTL=cpy/orddtl.cpy \
  --out orders.sexpr
```

The value is the member, `=`, and the path of the file holding it. The member
is written as the `COPY` statement writes it: a text-name — a COBOL word, or
an alphanumeric literal in its delimiters — optionally followed by `OF` or `IN`
and a library-name written the same way, so `STDHDR`, `'stdhdr.cpy'` and
`STDHDR OF PAYLIB` — quoted for the shell, as a value with a space always is —
are all members. The `=` ending the member is the first
one outside a literal; a COBOL word holds no `=`, and a literal ends at its
closing delimiter, so the split is unambiguous for every member a `COPY` can
name, and everything after it is the path, whatever it holds. The path is
resolved against the working directory, as [every path typed on the command
line is](#a-path-is-relative-to-the-file-that-states-it), and written as typed
into every diagnostic about it.

A literal's delimiters are there for the split and nothing else. The member
is the literal's **content**, exactly as a `copy-member` form writes it, so
`'STDHDR'` and `STDHDR` are one member — `cobol-go` hands a resolver the name
with its delimiters removed and does not say which way it was written — and
two `--copy-member` values giving them are two values naming one member.

A member is matched to a `COPY` exactly as a `copy-member` form is
([A `COPY` member is found where the layout
says](../layout/SPEC.md#a-copy-member-is-found-where-the-layout-says)) —
by content, without regard to case, the library part included, and with no
fallback from a qualified `COPY` to an unqualified member — and the map is
handed to the [one
resolver](#where-a-copy-member-is-found) a run uses. So a `COPY` is expanded by
`init` exactly as it is by a run whose layout names the same files.

Four values are usage errors, each decidable from the vector before anything
is opened, and each reported naming the value as typed: one with no `=` outside
a literal; one whose member or path is empty, or whose member is not a COBOL
word or literal, optionally `OF`/`IN` another; one whose path is `-`, because a
fault inside a member is reported at the member's path and a stream has none;
and two whose members are equal under that matching, naming both, because one
`COPY` would have two files to read.

A `--copy-member` that no `COPY` in any copybook names is **noted**, and the
run carries on. A layout's `copy-member` form nothing reaches is an error,
because a layout is a checked-in statement and an unreached one is more often a
misspelling ([the same rule as a
block](../layout/SPEC.md#a-blocks-name-and-a-block-no-record-names)); a command
line is typed once, often from a script that serves several copybooks, and
nothing it says outlives the run. The note is still there so that a misspelled
member reads as one beside the refusal its `COPY` then produces.

A member is **not** a copybook `init` converts. It gets no definitions file of
its own and derives no record: its entries reach the definitions file of every
copybook that copies it, in the place the `COPY` stood. An adopter who wants
a member transcribed on its own names it with `--copybook` too, and the two
flags do not interact.

### Where the scaffold is written, and why nothing is ever overwritten

`--out <path>` names the layout, and the definitions files go beside it. It
**MUST** be given, and `init` without it **MUST** be a usage error.

There is no default because no default is defensible. A name of cpybkc's own —
`layout.sexpr` in the working directory — is a name the adopter then has to
match in the manifest's `layout` field, so the tool would have chosen an
identifier for a file it does not own. Where the files go is the one thing in
this command the adopter knows and cpybkc does not.

**`--out -` MUST be a usage error**, with status 2, and no release keeps the
old behaviour of writing the scaffold to standard output alongside. The reason
is the owner's decision on discussion #394: `init`'s output is a set of files,
one per copybook beside the layout, and a stream is one unnamed sequence of
bytes. Putting the layout on standard output would leave the definitions files
its records name without a directory to be beside, and putting the files there
too would need a framing of several files on one stream that no reader of a
layout or of a definitions file understands. Below 1.0.0 [any release may
withdraw it](../container/SPEC.md#below-100-any-release-may-break-a-covered-thing)
(#398). A run that wants the output somewhere it can read without committing to
it names a scratch directory, which is what [When a copybook
changes](#when-a-copybook-changes-and-moving-a-record-between-sources) does.

**Nothing at any destination is ever replaced, and the set is written whole or
not at all.** A run's destinations are the layout and each definitions file it
derived a name for, and:

- **Every destination MUST be checked before anything is written.** Where
  anything exists at one — a regular file, a directory, a symbolic link,
  including one that dangles — cpybkc **MUST** fail the run, write nothing, and
  report a diagnostic per occupied destination, naming the path as it was
  derived from what was typed and the absolute path it looked at. It **MUST
  NOT** truncate, append to, or write through what it found.
- **Each file MUST be written to a temporary file beside its destination and
  then linked to it**, the definitions files first and the layout last. A link,
  unlike a rename, refuses a name that is taken, so a file that appeared at a
  destination after the check is still not replaced: the link fails, and so
  does the run. Each temporary file is removed once it is linked or the run
  ends.
- **Any failure or cancellation MUST remove every file this run linked.** A run
  that fails or is [cancelled](#cancellation) leaves the directory as it found
  it — no definitions file without its layout, and no layout naming a
  definitions file that is not there.

The layout comes last because it is the file that names the others. A
directory holding definitions files and no layout is one an adopter reads as a
run that did not finish, and the removal above makes that a moment rather than
a state; a layout without its definitions files would be one a reader reports
as a missing file, at a span in the layout, as though the layout were wrong.

Overwriting a layout an adopter has edited is the one unrecoverable thing this
command could do. The derived half is recomputable — the copybooks are still
there and the scaffold is one command away — but the `discriminate` forms and
the `sequence` are the part no copybook holds, and nothing recovers them. A
definitions file is held to the same rule even though it is recomputable,
because it is the one an adopter whose copybook cannot be committed has no
copybook to recompute from, and because a run that replaced some of its
destinations and refused others would leave a set no single run wrote. Every
other rule in this project already implies the refusal: a manifest's unknown
field is reported rather than ignored (#40), a duplicate flag is refused rather
than resolved by precedence, and no path is searched for. It is stated here
rather than assumed because "assumed" is how the opposite gets implemented.

There is **no `--force`**. A flag whose only purpose is to permit the
unrecoverable act is a flag that gets written into a script once and is never
reconsidered, and what it saves is one `rm` — a gesture the adopter performs
deliberately and their shell records.

All this section has to say about a layout that already exists is the rule
above: a destination that is occupied fails the run, whatever is in it and
however mature it is. Whether `init` has anything else to offer such a project —
the derived forms for a copybook it has taken on — is [`init` does not extend a
layout that already
exists](#init-does-not-extend-a-layout-that-already-exists)'s question, and it
is answered there, with what the adopter does instead (#212). Nothing in that
answer loosens anything here: there is still no `--force`, still no destination
cpybkc writes through, and the derived forms reach an existing layout by way of
the adopter's editor.

### The scaffold is deliberately incomplete

The layout `init` writes — the **scaffold**, as the rest of this document calls
it — is **not a valid layout**, and cpybkc **MUST NOT** write one its own reader
accepts as complete. It **MUST** parse as S-expressions, and it **MUST** fail the
layout reader's validation until the adopter finishes it: `encoding` and
`framing` are absent, every `record` is missing its `discriminate`, and there is
no `sequence`.

**The definitions files are complete, and are no part of that
incompleteness.** Each is the whole of its copybook's entries, a file the
layout reader [reads and checks](../layout/SPEC.md#definitions) as it reads any
definitions file, and nothing in one is left for the adopter to finish. What
is missing is missing from the layout, because what is missing is exactly what
no copybook holds; a definitions file holds only what a copybook does, so
there is nothing it could leave out that the adopter would be the one to
supply. An adopter who finishes the layout and changes no definitions file has
a project that resolves.

This is the feature rather than a wart, and the reason is a rule this document
already carries. [More than one fault found in one pass **MUST** be reported
together](#standard-error-diagnostics), and [every diagnostic carries a
span](../layout/SPEC.md#every-diagnostic-carries-a-span-and-some-carry-two) — so
running `cpybkc` against a fresh scaffold reports the entire remainder at once,
each fault pointing into the file at the place the missing statement belongs.
That is exactly the checklist the adopter wants next, in the order the file is
written, in a form their editor can step through, and produced by the reader
that will have to accept the finished layout rather than by a second description
of what a layout needs.

Two things follow, and both are requirements rather than consequences.

**cpybkc MUST NOT invent a value for anything an adopter decides.** Not an
encoding axis, for the reason [All four, always, with no default for
any](../layout/SPEC.md#all-four-always-with-no-default-for-any) gives; not a
`recfm`; not a strategy on a `discriminate`; not a shape for the `sequence`. A
guessed value is the one outcome worse than a blank, because a blank is
reported and a guess is read.

**The scaffold MUST parse.** A file that failed `sexpr-go` would be reported as
one lexical fault and nothing else, which turns the remainder back into
something discovered a form at a time — the opposite of what the incompleteness
is for. Everything cpybkc cannot state goes in a comment, which is [the only
shape a layout has](../layout/SPEC.md#tagged-forms-over-s-expressions) for text
a reader ignores.

A placeholder inside a comment **MUST** be written so that uncommenting it
without filling it in is a fault the layout reader reports, naming the
placeholder. The adopter who works down the checklist by deleting `;;` and then
loses their place is the ordinary case, and a placeholder that happened to be a
legal value would let them succeed with a layout they never finished writing.
The spelling this document's examples use is `<charset>` — the placeholder form
[`layout/SPEC.md`](../layout/SPEC.md#notation-used-below) already writes its
skeletons in — and the spelling itself is [not
covered](#compatibility-guarantees).

### What the scaffold states, form by form

A scaffold **MUST** carry every item of the list below that its copybooks reach,
**MUST NOT** carry anything else, and **MUST** write them in the order given —
which is the order [the top-level
forms](../layout/SPEC.md#the-top-level-forms) are tabled, so that a form the
adopter uncomments is already where the format's own examples put it. Two runs
of one build over one set of copybooks **MUST** produce byte-identical files;
determinism is a requirement here rather than a hoped-for property, because a
scaffold that reordered itself between runs would be undiffable in exactly the
review where an adopter is deciding what to keep.

The list is normative in its membership and its order, and descriptive in its
wording: what each comment *says* is [not
covered](#compatibility-guarantees), and the examples below are illustrations of
the shape rather than text an implementation has to reproduce.

1. A header comment saying what the file is, what was derived, and what is left.
2. `encoding`, commented: the four axis names, each with a placeholder, and no
   value. The values are not listed even for the three closed axes — a listed
   value reads as a recommendation, and the charset axis is deliberately
   open-ended, so any list here goes stale the release a code page is added
   ([The encoding profile](../layout/SPEC.md#the-encoding-profile)).
3. `framing`, commented: `recfm` with a placeholder, and a note that which other
   children are admitted follows from the value chosen.
4. `record` forms, **uncommented and complete**: one per record type each named
   `01`-level resolves to, in the order the copybooks were named and, within
   one, in declaration order; each carrying `(definitions "<name>.sexpr"
   <01-name>)` as its source — `<name>.sexpr` the definitions file converted
   from that `01`-level's copybook, and `<01-name>` the `01`-level — and one
   `alternative` child per `REDEFINES` outside a repeating group. A record
   [noted](#how-a-combinations-record-name-is-chosen) rather than written is
   absent here, and absent from items 6–9 too: no `rename`, `discriminate` or
   `sequence` names a record the layout does not carry, so uncommenting one
   never produces a reference to nothing.
5. `copybook-reading`, commented, when and only when some named copybook carries
   an `OCCURS DEPENDING ON` — with both readings named and neither chosen. Which
   compiler wrote the data is not in the copybook.
6. `rename` forms, commented, one per record over an `01`-level that resolved to
   more than one record type, with the record name filled in and the substitute
   a placeholder. See [How a combination's record name is
   chosen](#how-a-combinations-record-name-is-chosen) for why these are never
   uncommented.
7. `discriminate` forms, commented, one per record, with the record name filled
   in and the strategy a placeholder.
8. `discriminate-variant` forms, commented, one per `REDEFINES` inside a
   repeating group, with the variant reference and one `arm` per alternative
   filled in and each predicate a placeholder.
9. `sequence`, commented, naming every record once in emission order. It is a
   list of the record names in a shape that parses and **MUST NOT** be described
   as an order the file has; the operators are the format's
   ([Sequencing](../layout/SPEC.md#sequencing)) and choosing among them is the
   adopter's.

The commented forms are what makes this a scaffold rather than a list of
records. A commented
`(discriminate DEBIT-POSTING (equals (item DEBIT-POSTING <item>) <literal>))`
carries the record name, the reference's root and the shape of the strategy —
everything about that line except the one thing only the adopter knows — and the
alternative is an adopter reading two specifications to find out what the form
is called.

**Each definitions file MUST hold every entry and every clause of its
copybook** — for a copybook that writes `COPY`, every entry and clause of its
expansion, each member's entries where its `COPY` stood ([Where the members
come from](#where-the-members-come-from)) — in the [notation](../layout/SPEC.md#definitions) #395 settles and in
[the canonical printed form](../layout/SPEC.md#the-printed-form-init-writes):
one entry per line, in source order, every clause the copybook carries on its
entry. That includes every entry no record derives from — a `77`-level, an
unnamed `01`-level, an `01`-level every combination of which was noted — because
the file is the copybook's transcription and not the layout's selection from
it. A scaffold carries no `copy-member` form: no record in it names a
copybook, so no `COPY` is read when the layout is, and a `copy-member` form
nothing reaches is an error. It **MUST** carry none of the copybook's comments, and **MUST** carry
exactly one comment of its own, first: a header naming the copybook as it was
typed on the command line. That is a `;` comment, which [nothing
reads](../layout/SPEC.md#what-a-transcription-leaves-out); it is not the
header *form* a definitions file never has, and moving the file's entries into
a block leaves it behind with nothing lost.

Two runs of one build over one set of copybooks **MUST** produce byte-identical
files throughout — the layout and every definitions file. A definitions file is
what an adopter diffs when the vendor ships a new copybook, and a file whose
bytes moved with nothing in the copybook having moved would bury the one change
the diff is run to find.

### How a combination's record name is chosen

An `01`-level with two independent two-way `REDEFINES` outside a repeating group
is four record types, and `init` has to invent four symbols. It **MUST** derive
them, mechanically:

- An `01`-level carrying no such `REDEFINES` is one record type, and its record
  name **MUST** be the `01`-level's own name.
- One carrying any is one record type per combination, and each record name
  **MUST** be the `01`-level's name followed by the name of each chosen
  alternative, in the order the redefines appear in the copybook, joined by a
  single `-` — `POSTING-RECORD-PST-DEBIT-PST-TAIL`.
- Where that produces one symbol for two combinations, or a symbol another
  record already carries, cpybkc **MUST** note every colliding combination and
  **MUST NOT** write any of them. Duplicate data names are legal COBOL, so this
  is reachable; disambiguating it would mean inventing a name whose only
  property is that it differs from another invented one, and writing one of the
  colliding records and not the others would be cpybkc choosing which of them
  the adopter meant.

**What `init` cannot write a record for, it notes, and the run goes on.** Four
things stop a record from being written, and each **MUST** be a `note:`
diagnostic rather than a refusal:

- **an unnamed `REDEFINES` alternative** — a `FILLER` that redefines. An
  `alternative` child names its alternative by an [item
  reference](../layout/SPEC.md#an-item-reference), and an item with no name
  cannot be reached by one, so no record selecting it can be written;
- **a derived record-name collision**, as above, where every colliding
  combination is noted and none is written;
- **a per-`01` `resolve.Describe` failure** — an `01`-level the copybook
  parses and builds, but that cpybkc's own resolution cannot describe as a
  record;
- **a derived record name the S-expression grammar cannot read as a symbol**,
  so that the `record` form carrying it would not parse as the name it was
  derived as.

Each note **MUST** name the copybook as it was typed, the `01`-level, and what
was not written — the record names, or the combinations where there is no name
to give — and the run exits `0`. A copybook no record derives from at all —
one holding only `77`-levels, or no named `01`-level, or one every combination
of which was noted — still gets its definitions file, and **MUST** get a note
saying no record was derived from it.

Reason: everything in that list is a statement about the layout, never about
the copybook. The definitions file is complete whatever was noted, and it is
the part of the output nobody can type reliably by hand; a record `init` could
not name is one line the adopter writes against that file, with the note
telling them which. Failing the run would withhold every definitions file to
report a gap in one record, and an adopter whose copybook cannot be committed
would have no way past it but to transcribe the copybook themselves. The full
failure rule, notes and refusals together, is under [`init`'s streams and exit
codes](#inits-streams-and-exit-codes); it is written so that the notes can be
implemented before definitions files exist (#406), and the rest with them
(#408).

The names are long and nobody would choose them, and that is affordable for a
reason the format states: a record name is the layout's own identifier, and
[nothing outside the layout ever sees it as an
identity](../layout/SPEC.md#record-definitions). So it does not have to be a
good name. It has to be unique, deterministic, and traceable back to the
alternatives it selects — because the adopter's next act is writing a
`discriminate` for it, and they cannot do that without knowing which bytes it
is.

Numbering fails exactly there. `POSTING-RECORD-1` through `-4` is unique and
deterministic and says nothing: the adopter has to enumerate the combinations by
hand to learn which one `-3` is, which is the work this command exists to
remove. It is also unstable in the way that matters — a copybook gaining an
alternative renumbers the combinations, so a layout written against one run and
a scaffold produced by a later one disagree about which record `-3` is, silently
and in a file no layout is stored beside. A marked placeholder the adopter must
replace has the same defect and adds a second reason the file does not read.

And cpybkc **MUST NOT** name a record after the data. `example/ledger/ledger.sexpr`
calls the two combinations it names `DEBIT-POSTING` and `CREDIT-POSTING`; that is
a reading of what the file *means*, and no copybook holds it — neither the names
nor the fact that those two of the six `posting.cpy` admits are the ones its
extracts carry.

**No `rename` is emitted**, only a commented one. A rename on a record
substitutes the name the IR carries — the name a generator turns into an
identifier in somebody's public API — and a machine-invented substitute would
land there permanently. It would also silence the one thing that tells an
adopter they have a problem: several record types over one `01`-level carry one
name until a rename says otherwise ([A rename may name a
record](../layout/SPEC.md#a-rename-may-name-a-record)), and `cpybkc-gen-go`
refuses the collision naming the two that collided rather than munging them
(#50). That refusal is a checklist item; a substitute cpybkc chose is a name
nobody agreed to.

### The combination count is reported, not bounded

Three independent three-way redefines is twenty-seven record types out of one
`01`-level. `init` **MUST** emit all of them, and cpybkc **MUST NOT** refuse a
copybook for the number of record types it resolves to.

A bound would be a number cpybkc invented, and the first adopter to exceed it
would have a correct copybook, a layout the format requires to be written that
way, and nothing to do but type by hand exactly what the tool declined to type.
The twenty-seven `record` forms are not the cost anyway — the twenty-seven
`discriminate` forms are, and that cost is the adopter's whether this command
exists or not.

What it **MUST NOT** be is discovered in the file. Where an `01`-level resolves
to more than one record type, cpybkc **MUST** write a `note:` diagnostic naming
the copybook, the `01`-level, how many redefines outside a repeating group it
carries, and how many record types they produce. It is something the reader has
to act on — it is the size of the work in front of them — which is the standard
[Also out of scope](#also-out-of-scope) holds every line cpybkc writes to.

### `init` reads no manifest

cpybkc **MUST NOT** read a manifest under `init`, **MUST NOT** look for
`cpybkc.json`, and **MUST** treat `--manifest` under `init` as a usage error.

It needs none: `init` resolves no layout and runs no generator, and the manifest
names the layout, the generators and their options. Reading one would also drag
a whole validation surface into a command with no use for it — a manifest is
validated as a unit and an unknown field is a fault (#40) — so `cpybkc init`
would fail in a project whose manifest is half-written, which is the state a
project is in at exactly the moment `init` is worth running.

The decisive reason is the destination. `--manifest`'s absence defaults to
`cpybkc.json` in the working directory, so a run that read one and wrote to the
path its `layout` names would take its destination from a file the command line
never mentioned. The one unrecoverable act available to this command is writing
over an edited layout, and a destination that was discovered rather than typed
is how that happens with nobody having typed the path — a fresh-looking
`cpybkc init` in the wrong directory, and a `--out` that is not there to read.

Refusing is also the reversible direction. Admitting `--manifest` later — say,
defaulting `--out` to the path the manifest's `layout` names — turns a command
line that was a usage error into one that works, which [How a covered thing
would change](#how-a-covered-thing-would-change) admits in a minor release.
Reading a manifest now and withdrawing it later would not be.

### `init` does not extend a layout that already exists

cpybkc **MUST NOT** accept a flag naming a layout under `init`, **MUST NOT**
read a layout there, and **MUST NOT** vary what a scaffold carries or the order
it carries it in ([What the scaffold states, form by
form](#what-the-scaffold-states-form-by-form)) because a layout exists (#212). A
project with six copybooks and a seventh to take on runs the same command over
the seventh that it ran over the first six.

That project is not left with nothing, which is the part worth saying plainly.
The derivation is the whole of what this command performs and it is available to
them unchanged: `cpybkc init --copybook vendor/new.cpy --out /tmp/new/layout.sexpr`
writes that copybook's definitions file, `/tmp/new/new.cpy.sexpr`, and a
scaffold holding its `record` forms and the commented questions they raise,
into a scratch directory the rule above has nothing to refuse. From there the
adopter moves the definitions file beside the layout they already have, and
their editor picks up the `record` forms and puts them where they belong in
it. The records name `"new.cpy.sexpr"` bare, relative to the layout, so once
the file sits beside the layout the path they state is already right. What
`init` declines is not the derivation; it is the *merge*.

**What is emitted is the whole scaffold, and the adopter prunes it.** Not a
fragment: the header comment, the commented `encoding` and the commented
`framing` are written for the seventh copybook exactly as they were for the
first, and the adopter deletes the ones their layout has already answered.
Emitting less would make `init` write a third kind of file — neither the
scaffold this document specifies nor a layout the reader accepts — whose
membership is conditional on a file cpybkc was told about. `sequence` is where
that stops being a small change: it names *every* record once, so a `sequence`
over the new records alone is not a shorter sequence but a list the adopter has
to splice into theirs, and a scaffold that omitted it would drop the one form
that has to be edited rather than pasted.

So the prune is not uniform, and what the adopter does with each part is worth
writing down. The header comment, the commented `encoding` and the commented
`framing` are deleted, because their layout has already answered them — as is a
commented `copybook-reading`, where it has. The definitions file moves across
whole, as a file, into the directory beside the layout; if a file of that name
is already there, the adopter meets it in their own `mv` rather than in a write
cpybkc made, and renames the new one and the paths its records state as
[the vector's workaround](#the-vector-init-takes) does. The `record` forms and
the commented `rename`, `discriminate` and `discriminate-variant` questions
raised over them move across as they stand. The commented `sequence` is the one
part that is read
rather than moved: the new record names come out of it and into the sequencing
expression the layout already carries, at whatever position in that expression
the adopter's file puts them. That last act is the only one this command could
not have performed for them under any of the shapes weighed here — where a
record belongs in a file's order is not in a copybook — which is what makes a
whole scaffold the smaller imposition, and it needs no second description of
what a scaffold is.

**Record names are checked by the layout reader, not by `init`.** [How a
combination's record name is chosen](#how-a-combinations-record-name-is-chosen)
holds within a run, and a run that read no layout cannot see the names one
carries — so a derived name colliding with a name the adopter's layout already
holds is caught after the paste rather than before it, by the reader: a
duplicate record name is one of the faults [the layout reader
reports](../layout/SPEC.md#validation-and-diagnostics) (#24), over a file it has
in front of it and [with a span for each
occurrence](../layout/SPEC.md#every-diagnostic-carries-a-span-and-some-carry-two).
That is the division [The scaffold is deliberately
incomplete](#the-scaffold-is-deliberately-incomplete) already draws: what the
finished layout is missing is reported by the reader that will have to accept
it, rather than by a second implementation inside `init` that would have to be
kept in agreement with it.

The flag that would buy the merge is priced like every other. `--layout` would
be [covered](#compatibility-guarantees) for a major version — its name, its
value and its meaning — in exchange for the pruning above. It costs more than
[`init` reads no manifest](#init-reads-no-manifest) refuses for: a layout is
validated as a unit by a reader a mature layout passes and a half-edited one
does not, and a project adopting a copybook is routinely mid-edit, so the flag
would fail the run at exactly the moment the command is worth running. That is
the manifest's argument arriving over a second file. `--layout` is therefore not
a flag [the vector `init` takes](#the-vector-init-takes) lists, and is a usage
error like any other flag that table does not carry.

Rewriting the layout in place is refused further still, and not only for the
write. cpybkc would have to *print* the layout format — preserving an adopter's
comments, their spacing and their ordering, or silently losing them — which is a
printer and a compatibility surface of its own over a file this document does
not own. And it puts a write over an edited layout back on the table, which is
the [one unrecoverable act this command
has](#where-the-scaffold-is-written-and-why-nothing-is-ever-overwritten).

The direction stays open in the sense the rest of this document uses. A release
that adds `--layout` leaves every command line written against this one behaving
exactly as it did, so it would be [a minor release's
change](#how-a-covered-thing-would-change) — made when an adopter has asked for
it rather than before. What this section claims is that the paste is small
enough that nobody will.

### When a copybook changes, and moving a record between sources

A definitions file is checked in so that a run never needs the copybook it came
from, and that leaves one question `init` has to answer: what the adopter does
when the vendor ships a new copybook. The answer is the same command into a
scratch directory, and a diff:

```
cpybkc init --copybook vendor/posting.cpy --out /tmp/next/layout.sexpr
diff -u posting.cpy.sexpr /tmp/next/posting.cpy.sexpr
```

The diff is the vendor's change, entry by entry, in the notation the adopter
already reads. It is that and nothing else because the definitions file is
[byte-identical](#what-the-scaffold-states-form-by-form) across runs of one
build and carries none of the copybook's comments, so a comment the vendor
reworded is not a line in it. The adopter then copies the new file over the
old one — their act, in their shell, and not cpybkc's — and the layout reader
reports, at its spans, every item reference the new entries no longer
satisfy. `/tmp/next/layout.sexpr` is there to be read, not moved: its commented
questions say what a record the new copybook added would need.

**A record can move between its three possible sources**, and each move is
checked the same way: run `cpybkc --emit-ir` before the edit and after it, and
compare the two files byte for byte. [Three sources, one
descriptor](../layout/SPEC.md#three-sources-one-descriptor) requires them to
be equal, so any difference is a mistake in the move.

- **Copybook to file.** Convert the copybook with `init` into a scratch
  directory, move the definitions file from there to beside the layout, and
  swap `(copybook "x.cpy" N)` for
  `(definitions "x.cpy.sexpr" N)` in each record that names the copybook.
  Where the copybook writes `COPY`, give `init` one `--copy-member` per
  `copy-member` form the layout holds for its members, with the same paths;
  then remove each `copy-member` form no remaining copybook source reaches,
  which the run reports by name.
- **File to block.** Add `(definitions-block NAME …)` to the layout, holding
  the file's entries; in every record of that layout that names the file,
  change `"x.cpy.sexpr"` to `NAME`; and delete the file once no other layout
  names it.
- **Block to file.** Write the block's body, dedented, to a file; remove the
  block; and in each record that names it, change `NAME` to the file's path.

Nothing else in the layout changes, and nothing has to. Alternatives,
discriminators, renames, overrides and the sequencing expression are all rooted
at **record names**, never at a copybook, a file or a block ([Record
definitions](../layout/SPEC.md#record-definitions), [an item
reference](../layout/SPEC.md#an-item-reference)), so the source is the one
argument of the one form that says where a record's entries are held, and the
only thing a move edits.

### No flag states what only an adopter knows

cpybkc **MUST NOT** accept a flag that states an `encoding` axis, a `framing`
child, a `discriminate` strategy or any part of a `sequence`, and **MUST NOT**
write a value for any of them into a scaffold.

The **MUST NOT** default half is [All four, always, with no default for
any](../layout/SPEC.md#all-four-always-with-no-default-for-any)'s, restated here
because a scaffold is the obvious place to break it. Whether cpybkc may be
*told* them — an `--encoding`/`--framing` pair bringing a scaffold closer to
something a reader accepts — is the question this section answers, and the
answer is no.

It buys no completeness. Even with both supplied, the scaffold is exactly as
unreadable as before: `discriminate` is one per `record` and `sequence` is
required with every record appearing in it, and no argument derives either from
a copybook. So the pair moves four lines out of a comment and leaves the file
still failing, which is the whole of what it achieves.

It costs a second notation for something the layout format already spells.
`framing`'s children are conditional on its `recfm` — `lrecl` under `F` and
`FB`, `max-segment` under `VBS`, `delimiter` and `placement` under
`line-sequential` ([Physical framing](../layout/SPEC.md#physical-framing)) — so
a `--framing` value either re-spells those conditional arities on a command line
or accepts an incomplete form, and a second spelling of one thing is a second
thing to keep in agreement.

And a flag is [covered](#compatibility-guarantees): its name, value, default and
meaning hold for a major version, bought here in exchange for four lines an
adopter uncomments once per project. The direction is left open — adding the
flags later leaves every existing command line behaving as it did, so it is a
minor release's change, made when an adopter has asked for it rather than
before.

### `init`'s streams and exit codes

Nothing about the diagnostic format changes. `init`'s diagnostics are [the same
severities, the same separator and the same two-space
continuation](#standard-error-diagnostics), on standard error. It starts no
generator, so no line it writes carries a generator's name — and the absence of
one already means the line is cpybkc's.

`init` writes **nothing** to standard output. Its output is a set of files, and
`--out -` is [refused](#where-the-scaffold-is-written-and-why-nothing-is-ever-overwritten);
there is no spelling under which the scaffold or a definitions file reaches the
data channel ([Standard output](#standard-output)).

**The failure rule.** `init` writes the layout and every definitions file, or
it writes none of them. What decides which is whether every copybook could be
transcribed — never whether every record could be named.

- **Notes, and the run exits `0`.** An unnamed `REDEFINES` alternative; a
  derived record-name collision, where every colliding combination is noted
  and none is written; a per-`01` `resolve.Describe` failure; and a derived
  record name the S-expression grammar cannot read as a symbol. Each note names
  the copybook as typed, the `01`-level, and what was not written ([How a
  combination's record name is chosen](#how-a-combinations-record-name-is-chosen)).
  A copybook no record derives from — only `77`-levels, no named `01`-level, or
  every combination noted — still gets its definitions file, and a note. The
  [combination count](#the-combination-count-is-reported-not-bounded) is a note
  too, and so is a `--copy-member` no `COPY` names ([Where the members come
  from](#where-the-members-come-from)).
- **Refusals, and the run exits `1` having written nothing.** A copybook that
  cannot be opened, parsed or built, and a copybook that declares no entry at
  all (#31). A copybook whose `COPY` names a member no `--copy-member` gives, a
  member file that cannot be opened, and a `COPY` cycle are all a copybook that
  cannot be parsed, reported in the shapes [What a `COPY` fault looks
  like](#what-a-copy-fault-looks-like) gives, at the copybook as typed. A
  copybook that writes `COPY` and whose members are all given converts as its
  expanded entries, and is refused or not on the same terms as any other. A
  copybook holding a prefixed literal, an `ALL` literal or `VALUES ARE` is not
  refused: each is [transcribed as `cobol-go` reads
  it](../layout/SPEC.md#a-prefixed-literal-an-all-literal-and-values-are-are-transcribed-as-cobol-go-reads-them)
  (#423). Every refused copybook is reported, not only the first, and each
  diagnostic names it as typed.
- **The read-back.** Before writing, `init` **MUST** read each definitions file
  back, from the bytes it is about to write, and require its entries to equal
  the copybook's — the [judge's
  comparison](../layout/SPEC.md#cobol-go-judges-an-entry-and-nothing-here-says-what-one-means),
  positions and comments cleared. A mismatch fails the run, status `1`, with
  nothing written, and the diagnostic names the copybook as typed and the item
  whose entry differed. It is cpybkc's own defect when it fires, and it is
  caught here rather than in the adopter's next run because a definitions file
  is never re-read from its copybook: a transcription that was wrong when it
  was written stays wrong in every run after.

The rule is drawn so that it can be implemented in two halves: the notes over
the `copybook` sources `init` writes today, before definitions files exist
(#406), and the definitions files, their read-back and the refusals that
withhold them with the rest (#408).

The three statuses are unchanged, and `init`'s faults distribute across them:

- `0` — the scaffold and every definitions file were written, whatever was
  noted. It is `0` even though the scaffold is not a valid layout, because
  [what was asked for was done](#exit-codes): the incompleteness is the answer
  this command gives, not a failure to give one.
- `1` — a `--copybook` value naming a **directory**; a copybook refused as
  above; a definitions file that does not read back as its copybook; a
  destination that already exists; a destination that cannot be written; and
  a run that is [cancelled](#cancellation). Each leaves no file this run wrote
  behind it.
- `2` — a first argument that is a non-flag and is not `init`; an operand under
  either action; `init` with no `--copybook` or no `--out`; `--copybook -`;
  `--out -`; `--copybook` twice with equal values; two `--copybook` values whose
  derived names are equal under case folding, or one whose derived name is the
  layout's own ([The vector `init` takes](#the-vector-init-takes)); a
  `--copy-member` value with no `=`, an empty or malformed member, an empty
  path or the path `-`, and two whose members are equal ([Where the members
  come from](#where-the-members-come-from)); and any flag
  `init` does not carry, including `--manifest`, `--emit-ir` and
  `--emit-ir-format`. As everywhere else, a `2` means cpybkc did nothing at all
  — no copybook or member was opened and no file was created.

### Why this is a subcommand and not a script over the published schema

[`README.md`](../../README.md) already points a shop that generates layouts from
metadata it holds at the third release asset, `layout-schema.sexpr`. This is the
in-house case of that, and it is not the same case.

What it derives from is a **copybook**, not metadata a shop already holds. A
script written against the published schema would have to parse COBOL, resolve
`REDEFINES`, and apply "exactly one `alternative` per redefine outside a
repeating group, and none where the copybook writes none" — which is
`cobol-go`'s parser and `resolve`'s rules (#31, #35), re-implemented. Any drift
between that re-implementation and cpybkc's own produces a scaffold cpybkc then
rejects, and the adopter is holding two readings of their copybook with nothing
to tell them which is wrong.

The schema cannot close that gap, because it is not the kind of thing it says. A
schema declares the shape of a layout file; the derivation of a layout from a
copybook is a relation between two files, and no declaration of the first states
it ([What the schema does not
say](../layout/SPEC.md#what-the-schema-does-not-say)).

Being in the tool is also what keeps it current. A form added to the layout
format gains its scaffold line in the same release and the same review; a script
outside learns about it when a reader rejects its output.

Half of it is a converter, and the other half is not. The definitions files are
a conversion, whole and checked: every entry and clause of a copybook, in the
notation a reader of the layout format already reads, and read back before it is
written. That half has to be in the tool for the reason above made sharper — a
transcription by a second implementation that drifted from `cobol-go`'s parse
would be checked-in text stating, as the copybook's own, a reading cpybkc does
not make. The scaffold is not a conversion. Every statement in it came from a
copybook, and the part an adopter is qualified to write is exactly the part left
blank — which is the honest description of the split, and the reason the command
can be trusted with the half it does write.

## Standard output

Standard output is the data channel. cpybkc **MUST** write to it only what was
asked for by name:

- the descriptor, when `--emit-ir -` asked for it there;
- the version line, for `--version`;
- usage, for `--help`.

`init` puts nothing there: its output is a layout and the definitions files
beside it, a set of files no single stream can carry, and [`--out -` is a usage
error](#where-the-scaffold-is-written-and-why-nothing-is-ever-overwritten).

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

`0` covers a completed generation, a written descriptor, a written layout
scaffold and its definitions files — with whatever `init` [noted](#inits-streams-and-exit-codes)
about records it could not write — `--version` and `--help`.

`1` covers every fault in the work itself: no manifest where one was named or
defaulted, a manifest that does not parse or does not validate, a layout that
does not parse or does not resolve, a copybook that is missing or does not
declare what a record names, a generator that does not resolve on `PATH`, a
generator that exits non-zero or is killed by a signal, two generators
colliding on one output path, a merge or a prune that fails, a descriptor that
cannot be written, a copybook `init` cannot transcribe, a definitions file that
does not read back as its copybook, a scaffold or definitions file that cannot
be written or whose destination is occupied, and a run that is cancelled (#148,
#149, #150, #183, #403).

`2` covers only the vector: an unrecognised flag, a flag repeated where its
table forbids it, a missing value, an operand that is not a subcommand name or
that is not the first argument, and a flag combination this document refuses —
including a flag belonging to the other action, which
[`init`](#the-vector-init-takes) refuses in both directions, `init`'s `--out -`,
and two names `init` would derive into one directory. A status of 2 means
cpybkc did nothing at all — no file was opened, no file was created, no
generator was started, and the project's tree was not touched.

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

Under `init` the tree it found is the directory `--out` names, and leaving it as
found means removing every temporary file and every file this run linked —
the definitions files as well as the scaffold ([Where the scaffold is
written](#where-the-scaffold-is-written-and-why-nothing-is-ever-overwritten)).

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

`<tool-version>` is a SemVer string and carries no leading `v`, which is worth
saying because the image publishing that same release wears one: [the image tag
table](../container/SPEC.md#tags-and-what-pinning-one-buys) names `v0.2.0` and
this line names `0.2.0`, and they are the same release (#181). A reader
comparing the two is comparing the tag they pulled against the version the
program in it reports, so the difference between them is one character and it is
this one.

How a build comes to know which of the two versions above it is remains an
implementation matter; this one is written down in
[`CONTRIBUTING.md`](../../CONTRIBUTING.md).

## Compatibility guarantees

**This table states what 1.0.0 will promise.** Below 1.0.0 [any release may
break any row of
it](../container/SPEC.md#below-100-any-release-may-break-a-covered-thing), with
no particular number, no overlap and no deprecation (#398); from 1.0.0 it binds
as written. The same holds for every other place this document calls something
covered for a major version, or a change one a minor release may make: each
states what 1.0.0 will promise, and none of them needs saying again where it
appears.

**Covered.** From 1.0.0, within a major version of cpybkc each of these holds,
and a change to any of them is a breaking change:

| Guarantee | Value |
| --- | --- |
| [The command set](#one-command-and-one-subcommand) | One command whose default action is generating, plus `init`; the first operand is a subcommand name from that closed set, and there is no second operand |
| [The flags](#the-argument-vector) | The five above: each keeps its name, its value, its default and its meaning |
| [`init`'s flags](#the-vector-init-takes) | `--copybook`, `--copy-member` and `--out`: each keeps its name, its value, its repeatability and its meaning — `--copybook` names one copybook, never a stream; `--copy-member` names one member, as a `COPY` writes it, and the path of its file, never a stream; and `--out` names the layout, never a stream — and neither action admits the other's input-naming flags |
| [The scaffold](#the-scaffold-is-deliberately-incomplete) | A layout and one definitions file per `--copybook`, beside the layout and named after the copybook's final path element with `.sexpr` appended, each record naming its definitions file; the layout parses, states nothing an adopter has to decide, and is not a layout a reader accepts as complete; the set is written whole or not at all; and no destination is ever replaced |
| [The definitions text `init` writes](#what-the-scaffold-states-form-by-form) | Its forms — the [entry notation](../layout/SPEC.md#definitions), every entry and clause of the copybook, expanded where it writes `COPY`; their order, which is source order; and its line layout, [the canonical printed form](../layout/SPEC.md#the-printed-form-init-writes). Comment wording is not covered |
| [Manifest discovery](#finding-the-manifest) | `--manifest`, else `cpybkc.json` in the working directory, and no search |
| [Path resolution](#a-path-is-relative-to-the-file-that-states-it) | A path in a file is relative to that file; a path on the command line is relative to the working directory |
| [`--emit-ir`](#emitting-the-ir) | Replaces generation; writes the run's one descriptor, which is the same bytes every generator of that run is handed |
| [Standard output](#standard-output) | Carries only what was asked for by name, and nothing during a generation run |
| [The diagnostic format](#standard-error-diagnostics) | The severity set, the separator, the generator's name, the two-space continuation indent, and the stream all of it goes to |
| [Exit codes](#exit-codes) | `0`, `1`, `2`, and no other value |
| [`--version`](#--version) | One line naming the program, its version and the IR version |

The `init` rows changed with discussion #394 (#403): `--out -` was withdrawn,
the scaffold became a set of files, and the definitions text joined the table.
Each is read under #398's policy like every other row — it states what 1.0.0
will promise — and this section reserves no release number, no overlap and no
deprecation for the change.

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
- The scaffold's wording: every comment in it, the placeholder spelling, and its
  whitespace — the scaffold's, the layout's, and not a definitions file's, whose
  line layout is covered above. The wording of a definitions file's header
  comment is not covered either. What is covered is above — that it parses, that it decides
  nothing, and that it is incomplete — and the order of its forms, which follows
  [the layout format's own table](../layout/SPEC.md#the-top-level-forms) and
  moves if that moves.
- The record names `init` derives for the combinations of one `01`-level. Two
  runs of *one build* agree, which [What the scaffold states, form by
  form](#what-the-scaffold-states-form-by-form) requires of the whole file; two
  builds need not, because a record name is the layout's own identifier, nothing
  outside the file sees it, and the file is scaffolded once and then edited.
- Anything about the published image, which the [base-image
  contract](../container/SPEC.md) covers on its own terms and separately. The
  release *number* a break in this document takes is the one thing that is not
  separable — it is a tag on that image and a `--version` line here — and it is
  settled there: [below 1.0.0 it is any
  number](../container/SPEC.md#below-100-any-release-may-break-a-covered-thing)
  (#398), and [from 1.0.0 a new major
  version](../container/SPEC.md#a-breaking-change-to-either-contract-takes-a-new-major-version)
  (#213), which is a statement about numbering rather than about any guarantee
  in the table above.

### How a covered thing would change

Not by moving it. A covered flag, value or status changes in a new major
version, and the transition **MUST** hold both forms simultaneously for at
least one full minor release of that new major: a renamed flag means both
spellings work, with the old one accepted and reported at `warning:` on use,
and removed no earlier than the following minor release.

This section is what 1.0.0 will promise, and binds as written from then. Below
1.0.0 [any release may break a covered
thing](../container/SPEC.md#below-100-any-release-may-break-a-covered-thing), and
a `0.y.z` owes the break no particular number, no overlap and no deprecation
(#398).

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

**Which release is a new major version** is settled in the [base-image
contract](../container/SPEC.md#a-breaking-change-to-either-contract-takes-a-new-major-version),
because that number reaches a consumer as an image tag as well as as a
`--version` line and one release publishes both: from 1.0.0, a break in either
contract takes a new major (#213). Below 1.0.0 the same document [leaves the
number to the releaser](../container/SPEC.md#below-100-any-release-may-break-a-covered-thing),
and a reader who needs a command line held pins a full version tag or a digest.

#### The subcommand is the first change to break a covered thing

[`init`](#init-scaffolds-a-layout-from-copybooks) is a **breaking** change to
the command set, and it is worth arguing rather than asserting, because it is
not obvious: no command line this document admitted before behaves differently
now. Every existing vector is flags only, an operand was always a usage error,
and adding `init` leaves all of them doing exactly what they did — which is the
test the paragraph above uses for an additive change.

It is breaking anyway, on the guarantee itself. The covered value said *no
subcommands, no operands*, and a caller was entitled to depend on it: a wrapper
that passes a word of its own user's input through as cpybkc's first argument
used to receive status 2 for every value of that word, and now receives, for one
value, a command that writes a file. Turning a refusal into a filesystem write
is a change of behaviour on a line nobody edited, and it is the kind that is
found afterwards rather than at the failure.

That argument now states what 1.0.0's table will promise. From 1.0.0, a change
like this one — a refusal on the command set withdrawn — is a breaking change
and takes a new major version, however additive it looks from inside the flag
table.

The transition rule above has nothing to hold in parallel for it. What is being
withdrawn is a rejection, and a rejection has no second spelling to keep alive
for a minor release — there is no old form to accept at `warning:`, because the
old form *was* the warning. So the one thing a release can do for its callers is
the announcement: the release notes say the command set gained a member. That is
the only shape a deprecation takes when the behaviour being replaced was an
error message, and from 1.0.0 it is what [How a covered thing would
change](#how-a-covered-thing-would-change) asks of a change like this.

`init` itself arrived below 1.0.0, and the policy permits it there. Every tag
from `v0.0.0` to `v0.0.7` carries it — `git tag --contains c2ed0f7`, the #224
commit, lists all eight — so no released `0.y.z` was without it, and [any
release below 1.0.0 may break a covered
thing](../container/SPEC.md#below-100-any-release-may-break-a-covered-thing)
(#398). #213 had required the release first carrying `init` to be `1.0.0`; that
requirement is withdrawn with the rule it came from, and
[`CONTRIBUTING.md`](../../CONTRIBUTING.md#below-100-a-release-may-take-any-number)
says so in the place a release is actually cut from.

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

### Scaffolding anything but a layout

`init` writes a layout and the definitions files its records name — one per
copybook — and nothing else. There is no scaffolding of `cpybkc.json`, of a
copybook, of a generator's options, or of a project directory, and no
`definitions-block` written into the layout in place of a file (the owner's
decision on discussion #394).

Reason: the manifest is deliberately not a spec (#40) — a handful of fields a
person writes once, documented in the [README](../../README.md) and free to
change with the CLI — so a command that wrote one would be committing this
document to a file whose whole advantage is that it is not committed to. The
derivation argument does not carry over either: nothing in a copybook says which
generators a project wants or where their output goes, so a scaffolded manifest
would be a template rather than a derivation, and a template is a thing an
adopter copies out of the README in less time than it takes to read a flag. It
would also add to [never
overwriting](#where-the-scaffold-is-written-and-why-nothing-is-ever-overwritten)
a file of a different kind with a different answer, at the moment the rule
already covers the only unrecoverable act this command can perform. The
definitions files are not that exception: they are a derivation, not a
template — every line in one is a line of its copybook — and the layout
cannot name its records' descriptions without them.

### Extending a layout that already exists

`init` neither merges forms into a layout nor shapes what it emits around one.
There is no `--layout` flag and no other way to name a layout to it, no partial
scaffold shaped by one, and no in-place rewrite of a layout the adopter has
edited. The occupied-destination rule is unchanged and is stated where it
belongs, under [Where the scaffold is written, and why nothing is ever
overwritten](#where-the-scaffold-is-written-and-why-nothing-is-ever-overwritten).

Reason: argued in full under [`init` does not extend a layout that already
exists](#init-does-not-extend-a-layout-that-already-exists) — the derivation is
already theirs and only the paste is being declined, so the exclusion costs a
project with a mature layout nothing it cannot do today. What the adopter does
instead is run `cpybkc init --copybook <new.cpy> --out <scratch>/layout.sexpr`:
what comes back is the ordinary whole scaffold for that copybook and its
definitions file. They move the definitions file beside the layout they have,
move the `record` forms and the commented questions raised over them into that
layout, delete the header, `encoding` and `framing` their layout has
already answered, and take the new record names out of the commented `sequence`
and into the sequencing expression their layout already carries — the one part
of the scaffold that is read rather than moved. Whether a derived record name
collides with one that layout carries is answered by the reader the next time
the layout is resolved, not by `init`, which read no layout to compare against
(#212).

### Also out of scope

- **Flags that state what only an adopter knows.** No `--encoding`,
  `--framing`, `--discriminate` or `--sequence`, argued at length in [No flag
  states what only an adopter
  knows](#no-flag-states-what-only-an-adopter-knows): they would buy no
  completeness and cost a second spelling of a notation the layout format
  already has.
- **`--force`, and any other way to replace an existing scaffold
  destination.** Refused where the destination rule is stated, and left there
  because the flag and the rule are one decision.
- **Reading copybooks from a directory or from a stream.** Both refused in
  [Where the copybooks come from](#where-the-copybooks-come-from) — a directory
  because its contents would decide the scaffold with nothing recording it, a
  stream because a definitions file is named after its copybook and a stream
  has no name.
- **A directory of members, or a member found by its name.** No `--copy-lib`,
  no `--include`, and no lookup of `COPY STDHDR` as `STDHDR.cpy` anywhere:
  each member is named by `--copy-member`, with its path, for the reason [Where
  a `COPY` member is found](#where-a-copy-member-is-found) refuses a search for
  a run.
- **Writing the scaffold to a stream.** `--out -` is refused in [Where the
  scaffold is written](#where-the-scaffold-is-written-and-why-nothing-is-ever-overwritten):
  `init`'s output is a set of files, and a stream carries one.
- **A flag naming the definitions files, their directory, or the
  `definitions-block` form instead.** Each follows from what `init` already
  takes ([The vector `init` takes](#the-vector-init-takes)), and a flag stating
  it again would be a second place for the records' paths to be decided.
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
| [One command, and one subcommand](#one-command-and-one-subcommand) | #147 `cli` for the one command; #183 `cli` decides the subcommand and that the default action keeps the bare form, #214 `cli` implements the dispatch; the entrypoint promise it answers to, #55 and #183 `container` |
| [The first operand is a subcommand name](#the-first-operand-is-a-subcommand-name) | #147 `cli` reserved the position, #183 `cli` spends it, #214 `cli` parses it |
| [The argument vector](#the-argument-vector) | #147 `cli`; the second synopsis line, #183 and #214 `cli` |
| [`init` scaffolds a layout from copybooks](#init-scaffolds-a-layout-from-copybooks) | #183 `cli` specifies it; #214 `cli` the vector, #215 `cli` the derivation and the file; a definitions file per copybook beside the layout, and the adopter committing them in place of the copybooks, #403 `cli` against discussion #394, implemented by #406 and #408 `cli`; a composed copybook converted as its expansion, its members named by `--copy-member`, #407 `cli`, implemented by #423 `cli` |
| [What a copybook decides, and what only the adopter can](#what-a-copybook-decides-and-what-only-the-adopter-can) | #183 `cli` draws the split, over the forms #25–#29 `layout` fix; the derivation itself, #215 `cli`; the definitions-file row, and a record per named `01`-level less what is noted, #403 `cli` against discussion #394, the notes implemented by #406 `cli` and the definitions files by #408 `cli` |
| [The vector `init` takes](#the-vector-init-takes) | #183 `cli`, implemented by #214 `cli`; `--out` naming the layout, one definitions file per `--copybook` and its name, no flag added, no `definitions-block` written, and the two derivations refused as usage errors, #403 `cli` against discussion #394, implemented by #408 `cli`; `--copy-member`, #407 `cli`, implemented by #423 `cli` |
| [Where the copybooks come from](#where-the-copybooks-come-from) | #183 `cli`, over the refusal to search #148 `cli` already states; `--copybook -` refused because a definitions file is named after its copybook, and two spellings of one file, #403 `cli` against discussion #394; two spellings noted as a collision, #406 `cli`; refused where they share a final path element, and a definitions file each where they do not, #408 `cli` |
| [Where the members come from](#where-the-members-come-from) | #407 `cli` — decided here: `--copy-member`, its value, its usage errors, an unreached member noted, and a member converted only where it is copied; implemented by #423 `cli` |
| [Where the scaffold is written, and why nothing is ever overwritten](#where-the-scaffold-is-written-and-why-nothing-is-ever-overwritten) | #183 `cli`, implemented by #215 `cli`; that the rule holds whatever is at the destination, and that a layout which already exists is not extended, #212 `cli`; `--out -` refused, and the whole set checked, linked definitions files first and removed on failure, #403 `cli` against discussion #394, implemented by #408 `cli` |
| [The scaffold is deliberately incomplete](#the-scaffold-is-deliberately-incomplete) | #183 `cli`, over the layout reader #24 `layout` and the batched diagnostics #150 `cli`; the definitions files complete and outside the incompleteness, #403 `cli` against discussion #394, implemented by #408 `cli` |
| [What the scaffold states, form by form](#what-the-scaffold-states-form-by-form) | #183 `cli` decides it, #215 `cli` emits it, against the forms #25–#29 `layout` fix and the alternatives #164 `layout` settled; each record's `definitions` source, noted records absent throughout, and what a definitions file holds, in the notation #395 `layout` settles, #403 `cli` against discussion #394; noted records absent, #406 `cli`; each record's `definitions` source and the definitions files, #408 `cli`; a definitions file holding a composed copybook's expansion, and no `copy-member` form in the scaffold, #407 `cli`, implemented by #423 `cli` |
| [How a combination's record name is chosen](#how-a-combinations-record-name-is-chosen) | #183 `cli`, over the alternatives #164 `layout` settled and the collision #50 `gen-go` refuses; what is noted rather than written, #403 `cli` against discussion #394, the notes implemented by #406 `cli`, and a copybook no record derives from still transcribed, by #408 `cli` |
| [The combination count is reported, not bounded](#the-combination-count-is-reported-not-bounded) | #183 `cli`, implemented by #215 `cli` |
| [`init` reads no manifest](#init-reads-no-manifest) | #183 `cli`, against the manifest reader #40 `plugin` |
| [`init` does not extend a layout that already exists](#init-does-not-extend-a-layout-that-already-exists) | #212 `cli` — decided here, over the destination rule #183 `cli` states and the collision the layout reader #24 `layout` reports on the paste; no implementation story follows, because the derivation #215 `cli` already emits is the whole of what is offered; the procedure restated for definitions files and a scratch directory, #403 `cli` against discussion #394 |
| [When a copybook changes, and moving a record between sources](#when-a-copybook-changes-and-moving-a-record-between-sources) | #403 `cli` against discussion #394, over the byte-identical property #399 `layout` states; the definitions files the drift procedure diffs, #408 `cli`; moving a composed record, #407 `cli`, implemented by #423 `cli` |
| [No flag states what only an adopter knows](#no-flag-states-what-only-an-adopter-knows) | #183 `cli`, holding the line #25 `layout` draws for the four axes |
| [`init`'s streams and exit codes](#inits-streams-and-exit-codes) | #183 `cli`, over the format #150 `cli` and the statuses #147 `cli` fix; implemented by #214 `cli`; the whole failure rule — notes, refusals and the read-back — #403 `cli` against discussion #394, the notes implemented by #406 `cli` and the rest by #408 `cli`; the `COPY` refusals and the unreached-member note, replacing the refusal of every copybook writing `COPY`, #407 `cli`, implemented by #423 `cli` |
| [Why this is a subcommand and not a script over the published schema](#why-this-is-a-subcommand-and-not-a-script-over-the-published-schema) | #183 `cli`, against the published schema #23 `layout` and the reader #24 `layout`; the definitions files as the half that is a conversion, #403 `cli` against discussion #394 |
| [Finding the manifest](#finding-the-manifest) | #148 `cli`, over the manifest reader #40 `plugin` |
| [A path is relative to the file that states it](#a-path-is-relative-to-the-file-that-states-it) | #148 `cli`; a text `definitions` path, #399 `layout` against discussion #394; a `copy-member` path and a `--copy-member` path, #407 `cli`, implemented by #423 `cli` |
| [Finding the inputs](#finding-the-inputs) | #148 `cli`; the diagnostics it owes, #31 `resolve` and #150 `cli`; which copybooks a run reads, and that the manifest does not list them, #157 `cli`; which definitions files a run reads, that a block opens nothing, the missing-definitions diagnostic, and that a layout's own forms are read from one file, #399 `layout` against discussion #394; where a member named by `COPY` is found, and the member files among those a run reads, #407 `cli`, implemented by #423 `cli` |
| [Where a `COPY` member is found](#where-a-copy-member-is-found) | #407 `cli` — decided here: the layout's `copy-member` forms and no search, one member map per run, each member read once, one resolver for the run and `init`, the `COPY` faults, and a fault inside a member pointing into it, which needs member provenance from `cobol-go` for the owner to file in Zaba505/cobol-go; implemented by #423 `cli` |
| [Emitting the IR](#emitting-the-ir) | #149 `cli`, over the encoders #20 and #21 `ir` |
| [Which descriptor is emitted](#which-descriptor-is-emitted) | #157 `cli` decided it — one descriptor per run, and the manifest's `inputs` removed to keep it true; #148 `cli` assembles it, #149 `cli` writes it; definitions files and blocks among what it is a function of, and the three sources resolving to byte-identical descriptors, #399 `layout` against discussion #394 |
| [Standard output](#standard-output) | #147 `cli` for the version and usage, #149 `cli` for `--emit-ir -`; `init` writing nothing there, #403 `cli` against discussion #394, implemented by #408 `cli` |
| [Standard error: diagnostics](#standard-error-diagnostics) | #150 `cli`, over `internal/diag` and the spans of #31 `resolve` |
| [A relayed generator line](#a-relayed-generator-line) | #150 `cli`, over the classification #42 `plugin` already performs, against the format #39 `plugin` fixes |
| [Exit codes](#exit-codes) | #147 `cli` for the vector's own status and the single place they are emitted from, #148 and #149 `cli` for the failures that reach them; `init`'s, #403 `cli` against discussion #394, its notes exiting `0` implemented by #406 `cli` and the rest by #408 `cli`; `--copy-member`'s, #407 `cli`, implemented by #423 `cli` |
| [Cancellation](#cancellation) | #148 `cli`; what `init` removes, #403 `cli` against discussion #394, implemented by #408 `cli` |
| [The environment](#the-environment) | #41 and #43 `plugin` for the two variables read, #147 `cli` for reading no others |
| [`--version`](#--version) | #147 `cli`; the IR version it reports, #17 `ir`; the version it reports being the release's, #181 `cli` |
| [Compatibility guarantees](#compatibility-guarantees) | #146 `cli` — decided here, in the shape #54 `container` uses for the image; #183 `cli` for the command set's new value and the first breaking change under it; #213 `container` for the release number a break here takes, which is the one thing about the published image this table does not hand over whole; #398 `container` says the table states what 1.0.0 will promise, and that below 1.0.0 any release may break it; the `init` rows for definitions files and the withdrawn `--out -`, and the definitions-text row, #403 `cli` against discussion #394, implemented by #408 `cli`; `--copy-member` in the `init` flags row, and expansion in the definitions-text row, #407 `cli`, implemented by #423 `cli` |
| [How a covered thing would change](#how-a-covered-thing-would-change) | #146 `cli`; the subcommand priced against it, #183 `cli`; which release number pays it and what it does to the image's major tag, #213 `container`, which settles both halves in [the base-image contract](../container/SPEC.md#a-breaking-change-to-either-contract-takes-a-new-major-version); #398 `container` makes it bind from 1.0.0, withdraws #213's requirement that `init` ship as `1.0.0`, and records that every tag from `v0.0.0` carries `init` |
| The project manifest — out of scope, see above | #40 `plugin` |
| Flags that would state an adopter's knowledge, and scaffolding anything but a layout — out of scope, see above | #183 `cli`; the definitions files as the layout's, a stream, and a flag naming them, #403 `cli` against discussion #394; a directory of members and a member found by its name, #407 `cli` |
| Extending a layout that already exists — out of scope, see above | #212 `cli`; the procedure through a scratch directory, #403 `cli` against discussion #394 |
| This document | #146 `cli` |
| Conventions this document follows | #15 `setup` |
