// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

// usage is what the program says when it is asked, and when it is given
// something it cannot act on.
//
// It names where the corpus and the contract are written down rather than
// summarising either. This command implements two documents and specifies
// neither, and a synopsis that explained what an entry is would be a third
// description for the other two to drift from.
const usage = `cpybkc-conform runs the cpybkc conformance corpus against a generator's adapter.

  cpybkc-conform check --exec <path>  [flags] [-- args for the adapter...]
  cpybkc-conform check --image <ref>  [flags] [-- args for the adapter...]
  cpybkc-conform digest [--corpus <dir>]
  cpybkc-conform help

check drives the adapter through the contract and reports every entry. It exits
0 when nothing failed, 1 when a normative entry disagreed or could not be asked,
and 2 when the run could not be attempted at all. A provisional entry is
reported and is in neither: it counts in no total and fails nothing.

  --exec <path>              the adapter executable, as a path and not a name to
                             find on PATH
  --image <ref>              the adapter as a container image, run through the
                             image door below
  --corpus <dir>             the unpacked corpus (default "corpus")
  --deadline <duration>      bounds one operation (default 1m0s)
  --build-deadline <duration>
                             bounds generate, which may run a compiler over the
                             whole corpus (default 10m0s)
  --grace <duration>         how long the adapter is given to exit once the
                             conversation is over (default 10s)

--exec or --image, and never both: they are two doors and a run goes through one.

  --dir <dir>                --exec only: the adapter's working directory
                             (default: this program's own)
  --runtime <name>           --image only: the container runtime, found on PATH
                             (default "docker")
  --image-deadline <duration>
                             --image only: bounds the whole container, by the
                             wall clock, and must outlive --deadline and
                             --build-deadline (default 30m0s)
  --image-memory <size>      --image only: the memory cap (default "2g")
  --image-processes <n>      --image only: the process cap (default 256)
  --image-scratch <size>     --image only: the size of the writable /tmp the
                             door mounts (default "1g")

Everything left over is the adapter's own argument vector — the container's,
under --image. Put a bare -- in front of it: an adapter that takes flags of its
own takes them at the same spelling this program takes its, and without the
terminator they are read as this program's and refused.

digest writes the corpus's SHA-256 to standard output, and fails if a digest is
published beside the corpus and disagrees with it.

The two doors, and what each one is worth

--exec runs the adapter here, as a process, and provides no isolation of any
kind: no network namespace, no read-only root and no resource cap. A result
produced through it is your own working result.

--image runs it in a container with no network, a read-only root, a writable
/tmp, a memory cap, a process cap and a wall-clock bound. Those are the door's
properties and never the contract's, which is why the report quotes the door
rather than assuming them of every run — and why a result produced through this
door is one you can hand to somebody else.

/tmp is the only writable path in that container, and no host directory is
mounted into it. An adapter image whose toolchain writes under a home directory
has to point it there itself — TMPDIR, HOME, GOCACHE, CARGO_HOME, whatever it
reads — because a build that fails on a read-only root fails for a reason that
has nothing to do with the corpus.

Neither is a conformance claim a third party should be asked to trust without
qualification: a run computed on your machine, against a corpus you downloaded,
by a program you are holding, is a self-report whichever door produced it. That
is a fine thing to publish, labelled as one. cpybkc awards no level, profile,
score or badge.

What an entry holds and what an answer is written in:
    https://github.com/Zaba505/cpybkc/blob/main/docs/conformance/SPEC.md
How an adapter is asked, so that you can write one:
    https://github.com/Zaba505/cpybkc/blob/main/docs/adapter/SPEC.md
`
