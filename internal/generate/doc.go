// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package generate runs a project's generators and puts what they produce into
// its output tree.
//
// A generator never writes into that tree. It is handed a directory of this
// package's making — empty, writable, and nobody else's — and what it leaves
// there reaches the project only once every generator in the run has exited
// zero (docs/plugin/SPEC.md, "The output directory"). Running the generators is
// [github.com/Zaba505/cpybkc/internal/plugin]'s; deciding what a run produced
// and writing it where a person will find it is this package's, and it is the
// only place anything is written there.
//
// # Why a scratch directory
//
// A generator writing straight into the project's tree gives up the only
// guarantee worth having about a failed run. A plugin that dies halfway leaves
// a tree that is neither the old one nor a new one — and, because generated
// code usually still compiles with half of it missing, CI goes green on output
// nobody produced. Interposing a directory costs a copy and buys back the rule
// docs/plugin/SPEC.md states: a failed run leaves the project tree exactly as
// it found it.
//
// The directory being **empty** is the second half of it, and is what makes
// "the files this invocation produced" mechanically equal to "the files in the
// directory afterwards". No marker inside a generated file, no manifest a
// plugin has to maintain, and no bookkeeping asked of a plugin author at all —
// which is what lets the contract stay small enough for a generator to be a
// shell script. Cross-generator collision detection rests on that equality, and
// so will stale-file pruning (#45), which is not here yet.
//
// # Why a collision is the run's fault and not a generator's
//
// Two generators producing one path in the project's tree fails the run with
// nothing merged (docs/plugin/SPEC.md, "What a plugin does not own"), and
// neither of them is the one that was wrong. A plugin is told nothing about the
// others, cannot coordinate with them, and is entitled to produce the files its
// options asked for; what is wrong is the pair, and the fix is a manifest that
// stops asking two executables for one file.
//
// So the fault names both, and it is found in the pass that plans the merge
// rather than as each generator finishes. A check made per generator would let
// the first one's files land before the second was known about, and would then
// name whichever of them lost a race — so identical inputs would fail
// differently on different runs, which is the one thing generated output cannot
// do.
//
// # Why the merge waits for every generator
//
// Not for the one that produced the files. docs/plugin/SPEC.md: nothing reaches
// the project's tree until every generator in the run has succeeded, so one
// generator's failure discards another's output too. That is the point rather
// than a side effect — a half-generated tree is worse than an ungenerated one,
// because a person then has to work out which half is which — and it is what
// lets a plugin fail late, on an option key only it could have recognised,
// without costing anything but the run.
//
// It is also what keeps the run's answer the same twice. A merge made as each
// generator finished would let the first one's files land before the second was
// known about, so the same unchanged inputs would fail differently depending on
// which generator lost a race.
//
// # What "enforced rather than trusted" can mean
//
// docs/plugin/SPEC.md forbids a plugin to write outside the directory it was
// handed — not through `..`, not through an absolute path, and not through a
// symlink it created for the purpose — and says cpybkc enforces that rather
// than trusting it. What can be enforced from here is exact, and worth stating
// precisely, because a plugin is a separate process and nothing in this package
// can stop one writing wherever its user could:
//
//   - Only what is *beneath* a generator's directory is ever read, so a file
//     the plugin wrote elsewhere is not output. That is the whole of the `..`
//     and absolute-path cases: the escape is not blocked, it is simply not
//     collected, and the run produces the files the contract says it produces.
//   - A symlink is refused rather than followed, wherever it points, and so is
//     anything else that is neither a regular file nor a directory. A merge
//     that followed one would read or write through a link a plugin chose,
//     which is the escape rather than a variant of it.
//   - A destination the merge is about to write is removed first rather than
//     opened, so a symlink already in the project tree — left by a person, or
//     by a run of something else — is replaced rather than written through.
//   - A directory the merge has to descend *beneath* the output directory is
//     examined without following a link, so a symlink standing where a plugin's
//     path needs a directory fails the merge instead of becoming the way out of
//     the tree. The output directory itself and everything above it is followed
//     rather than refused: that path is the one a person wrote in the manifest,
//     and it routinely runs through a link nobody meant anything by.
//
// # Modes and ownership, in one place
//
// Every file and directory the merge creates gets its mode from this run and
// not from the plugin: 0666 for a file, 0777 for a directory, 0777 for a file
// the plugin made executable, each with the run's umask taken out of it. A
// plugin writes into a scratch directory with whatever umask the process — or
// the container — it ran in happened to have, so carrying its modes through
// would make the permissions in a checked-out project depend on where the
// generator ran.
//
// [Runner.Owner] is the same decision for ownership, and exists for one case:
// cpybkc running as root in a container over a bind mount, where without it a
// project's generated files come out owned by root and the person who ran the
// container cannot edit them. Both are applied at one call in merge.go, which
// is what makes "output ownership is decided in one place" a property of the
// code rather than a convention.
//
// # What a caller supplies
//
// [Generator] rather than [github.com/Zaba505/cpybkc/internal/plugin.Invocation],
// because the two disagree about what an output directory is: an Invocation's
// is the directory the generator is *handed*, which here is a scratch directory
// no caller names, while a Generator's is where that directory's contents end
// up. One field spelling both would let a caller hand a plugin the project's
// tree by writing the obvious thing, which is exactly the arrangement this
// package exists to replace.
package generate
