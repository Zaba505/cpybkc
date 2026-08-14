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

  cpybkc-conform check --exec <path> [flags] [-- args for the adapter...]
  cpybkc-conform digest [--corpus <dir>]
  cpybkc-conform help

check drives the adapter through the contract and reports every entry. It exits
0 when nothing failed, 1 when an entry disagreed or could not be asked, and 2
when the run could not be attempted at all.

  --exec <path>              the adapter executable, as a path and not a name to
                             find on PATH (required)
  --corpus <dir>             the unpacked corpus (default "corpus")
  --dir <dir>                the adapter's working directory (default: this
                             program's own)
  --deadline <duration>      bounds one operation (default 1m0s)
  --build-deadline <duration>
                             bounds generate, which may run a compiler over the
                             whole corpus (default 10m0s)
  --grace <duration>         how long the adapter is given to exit once the
                             conversation is over (default 10s)

Everything left over is the adapter's own argument vector. Put a bare -- in
front of it: an adapter that takes flags of its own takes them at the same
spelling this program takes its, and without the terminator they are read as
this program's and refused.

digest writes the corpus's SHA-256 to standard output, and fails if a digest is
published beside the corpus and disagrees with it.

This door runs the adapter directly and provides no isolation of any kind: no
network namespace, no read-only root and no resource cap. A result produced
through it is your own working result rather than one to hand to somebody else.

What an entry holds and what an answer is written in:
    https://github.com/Zaba505/cpybkc/blob/main/docs/conformance/SPEC.md
How an adapter is asked, so that you can write one:
    https://github.com/Zaba505/cpybkc/blob/main/docs/adapter/SPEC.md
`
