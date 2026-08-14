# The cpybkc conformance archive

Everything you need to run the cpybkc conformance corpus against your own
generator, on a machine with no container runtime, no registry access and no Go
toolchain.

```
cpybkc-conformance/
  README.md            this file
  corpus.sha256        the digest of corpus/
  corpus/              the conformance corpus, one directory per entry
  bin/                 cpybkc-conform, built per platform
```

## Running it

Write an **adapter**: a program that reads request frames on standard input,
writes response frames on standard output, and drives your generator in
between. The contract it implements is
[`docs/adapter/SPEC.md`](https://github.com/Zaba505/cpybkc/blob/main/docs/adapter/SPEC.md),
and the smallest conforming one is a shell script.

Then, from the directory this archive unpacked into:

```sh
./bin/cpybkc-conform-linux-amd64 check --exec ../my-adapter
```

`--corpus` defaults to `corpus`, which is why the line above names no corpus.
Anything after a bare `--` is your adapter's own argument vector:

```sh
./bin/cpybkc-conform-linux-amd64 check --exec ../my-adapter -- --generator ./gen-rust
```

It exits **0** when nothing failed, **1** when an entry disagreed or could not
be asked, and **2** when the run could not be attempted at all. `--help` is the
rest of the flags.

If your adapter ships as a container image, name it instead and it runs behind
one:

```sh
./bin/cpybkc-conform-linux-amd64 check --image ghcr.io/you/gen-rust-adapter:v1
```

That is the other door, and what it adds is in the last section. It needs a
container runtime (`--runtime`, `docker` by default) and `--exec` does not,
which is the whole reason both exist.

## Checking what you downloaded

`corpus.sha256` holds a SHA-256 over `corpus/`. `check` verifies it before it
starts a process and refuses to run against a corpus that does not match, so
there is nothing you have to remember to do about the corpus.

It covers the corpus and only the corpus. Nothing here checks `bin/` or this
file, so a download truncated inside one of the executables shows up as an
executable that will not start rather than as a digest that disagrees. Treat the
number below as "this is the corpus that was published", not as "this download
arrived intact".

To see the number yourself:

```sh
./bin/cpybkc-conform-linux-amd64 digest
cat corpus.sha256
```

The rule the digest follows is written down in
[`docs/conformance/SPEC.md`](https://github.com/Zaba505/cpybkc/blob/main/docs/conformance/SPEC.md#the-published-corpus),
so you can recompute it without this program.

## What a result from this archive is, and is not

`--exec` runs your adapter directly. There is no network isolation, no
read-only root and no resource cap, and `cpybkc-conform` says so in every
report it writes. A run through this door is **your own working result** — a
fine thing to have and to publish, as long as it is labelled as one.

`--image` runs it in a container with no network, a read-only root, a writable
`/tmp`, a memory cap, a process cap and a wall-clock bound. Those are the
door's properties and not the contract's, which is why the report quotes the
door rather than assuming them of every run, and why a result produced through
this one is **a result you can hand to somebody else**: nothing in it came from
the network, and nothing it did outlived the container.

Both doors drive the same contract with one implementation behind them, so the
conversation, the entries and the comparison are identical and only the
guarantees differ. Two things about the container are worth knowing before you
build the image: `/tmp` is the only writable path in it, so point `TMPDIR`,
`HOME`, `GOCACHE` or `CARGO_HOME` there if your toolchain needs a cache; and the
memory and process caps are asked of your runtime, which warns on its own
standard error when a kernel will not honour one. `cpybkc-conform` quotes that
warning beside the report rather than claiming a cap it may not have had.

Either way it is a self-report in a second sense: a run computed on your machine,
against a corpus you downloaded, by a program you are holding. Nothing here is
a conformance claim a third party should be asked to trust without
qualification, and cpybkc awards no level, profile, score or badge.

## Where the rest of it is written down

- **What an entry holds, and the language a decoded record is written in** —
  [`docs/conformance/SPEC.md`](https://github.com/Zaba505/cpybkc/blob/main/docs/conformance/SPEC.md)
- **Every rule of that language as a worked table** —
  [`docs/conformance/GRAMMAR.md`](https://github.com/Zaba505/cpybkc/blob/main/docs/conformance/GRAMMAR.md),
  which is where to check a values-document writer before running a single
  entry
- **How your adapter is asked** —
  [`docs/adapter/SPEC.md`](https://github.com/Zaba505/cpybkc/blob/main/docs/adapter/SPEC.md)
- **What a descriptor means** —
  [`docs/ir/SPEC.md`](https://github.com/Zaba505/cpybkc/blob/main/docs/ir/SPEC.md)
- **Which entries the corpus holds and what each covers** — `corpus/README.md`,
  beside the entries

Licensed under the MIT License, like the rest of cpybkc.
