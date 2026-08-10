// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package plugin finds the generator executables cpybkc runs.
//
// A generator has a name — the `<name>` a manifest asks for — and that name is
// the whole of how it is identified. docs/plugin/SPEC.md's "Discovery" is the
// contract: the executable is named `cpybkc-gen-<name>`, and cpybkc resolves a
// name to one by searching PATH for exactly that filename. [Resolve] is that
// search and nothing else; what a discovered executable is then handed, and
// what it may do with the directory it writes into, is #42's and #43's.
//
// This is not the standard library's plugin package and has nothing to do with
// it. A cpybkc generator is a process with an argument vector, not a shared
// object loaded into this one, which is what lets a generator be a shell script
// and be written in a language that is not Go.
//
// # Why PATH is the whole of it
//
// docs/plugin/SPEC.md: cpybkc MUST NOT consult anything beyond PATH to find a
// generator — no registry, no cache directory of its own, no lockfile, no
// download. That is not a detail of discovery but the project's distribution
// position: a generator is added by building an image FROM the published one
// and copying an executable onto a directory already on PATH
// (docs/container/SPEC.md, #54), and `FROM` plus `COPY` already are a
// resolution protocol. So this package opens no network connection, writes no
// state, and reads no file but the ones it stats.
//
// It also reads no environment. The PATH to search is an argument, because a
// package that read one would be a second place the search could come from,
// and a test of it would have to move the process's own environment to say
// anything.
//
// # Why not exec.LookPath
//
// [os/exec.LookPath] is the same search with three differences, and the first
// of them is a requirement of the contract rather than a preference:
//
//   - It treats an empty PATH element as the working directory, which POSIX
//     permits and docs/plugin/SPEC.md forbids. cpybkc runs generators as a side
//     effect of a generation command, and a generator picked up from whatever
//     directory a user happened to be standing in is an execution surface
//     nobody chose. An empty element is skipped here.
//   - It asks the kernel whether this process could execute the file, through
//     eaccess(2). The contract says a candidate is a regular file carrying an
//     execute bit, which is a question about the file rather than about who is
//     asking — so [Resolve] reads the mode. The two differ for a file whose
//     execute bit belongs to another user, and for root, and where they differ
//     the contract is what a plugin author read.
//   - It reports a relative resolution as [os/exec.ErrDot], which is a decision
//     about running a command from an interactive shell's PATH rather than
//     about this one.
//
// # What a candidate is
//
// A regular file with an execute bit, and there is no second test — no
// extension, no magic number, no metadata file beside it. That is what keeps a
// shell script with `chmod +x` a first-class plugin, and it is why a generator
// is not asked to describe itself before it is run.
//
// A symlink is followed, because [os.Stat] follows one and because a plugin
// installed by a package manager usually is one. What the mode test sees is the
// file at the end of the chain, which is the file that would be executed.
//
// A file with the right name that fails either test is not a candidate and the
// search continues past it, exactly as a shell's would. It is still named in
// the diagnostic when nothing resolves ([NotFoundError]): a missing execute bit
// is the likeliest single explanation for a generator that is installed and
// not found, and a message that stayed silent about the file it had just
// looked at would leave the adopter to find it by hand.
//
// # Why the earliest match wins
//
// PATH is searched in order and the first match is the answer, exactly as it is
// for a shell resolving a command name. That rule is what makes a plugin under
// development shadow an installed one by prepending a directory, which is how
// an author tests a change; the opposite rule would make that gesture silently
// do nothing, and the difference is invisible in the output.
//
// The search is therefore deterministic in the only sense that matters to a
// caller: one PATH and one name give one answer, and it changes when the
// filesystem does rather than when a map is iterated.
//
// # What the answer is spelled as
//
// The path handed back is the PATH element with the filename joined onto it, as
// PATH spells it. A relative element resolves to a relative path, against the
// working directory the search was made from — which is what a shell does with
// the same PATH, and the reason docs/plugin/SPEC.md says the working directory
// is searched when it appears in PATH as a written-out path. Making the answer
// absolute would be this package deciding that a relative element meant
// somewhere else than it said.
//
// # Where the container comes in
//
// Nothing here knows about the published image, and that is the point. The
// image's contribution is a directory on PATH that a derived image copies a
// generator into (docs/container/SPEC.md, #54); a plugin in it is found by the
// search below because it is on PATH, under exactly the rules a plugin on a
// laptop's PATH is found by. There is no container path in this package to keep
// in step with that document, and a generator behaves the same in both places
// because only one search exists.
//
// # Why the name is checked here as well
//
// docs/plugin/SPEC.md requires a name to be non-empty and to contain no `/`,
// because the name is a filename component. [github.com/Zaba505/cpybkc/internal/manifest]
// already reports both, with the line in cpybkc.json the name is written at —
// a name carrying a `/` as a fault of its own, and an empty one as the fault
// every field that has to carry something raises. This package reports them
// again, as one fault and with no position at all.
//
// That is two enforcements of one MUST for two audiences rather than a
// duplicated check. A name reaches [Resolve] from wherever a caller had one — a
// manifest, a flag, a test — and a name with a `/` in it that arrived by some
// other route would otherwise turn into a path that leaves the directory being
// searched, which is a search this package is not entitled to make. The
// manifest reports it earlier and better; this reports it wherever it comes
// from.
package plugin
