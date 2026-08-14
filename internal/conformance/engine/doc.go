// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package engine drives an adapter through the contract docs/adapter/SPEC.md
// specifies: it starts a process, speaks JSON frames to it over standard input
// and standard output, holds what came back against what each corpus entry
// states, and reports (#198, #199).
//
// [github.com/Zaba505/cpybkc/internal/conformance] is the language-neutral half
// — the entry, the value language and the comparison — and this package is the
// process boundary and the front door onto it. Nothing here knows what a value
// means or how one is spelled; what it knows is how the question is put, and
// that the answers are compared here rather than by whoever answered them.
//
// # What the engine owns
//
// Everything except compiling and running generated code. Loading and
// validating entries is the corpus package's; spawning the adapter, the
// per-operation deadline, the fault isolation, the comparison and the report
// are this one's. The split is not organisational: the comparison stays on this
// side of the process boundary because an adapter holding the expected answers
// is self-grading, which is not a weaker check than an independent one but a
// different one — it measures whether the adapter's author could reproduce a
// document they were handed (docs/adapter/SPEC.md, "The adapter is never given
// the expected values"). The engine therefore sends an entry's descriptor and
// its bytes and nothing else, and no frame this package writes carries any part
// of values.json or anything derived from it.
//
// # Three outcomes, kept apart
//
// docs/adapter/SPEC.md's "Refusal is an answer, a fault is not, and an exit
// code is neither" is the whole of what [Engine.Run] is careful about, because
// an engine that conflates any two of them reports a wrong thing about a
// working generator or a working thing about a broken one:
//
//   - An answer is `ok: true` and a values document, which may carry a failure.
//     A file the generated reader refused is one of these, and a large part of
//     the corpus expects exactly that, so it is compared like any other answer
//     and never reported as a fault.
//   - A fault is `ok: false`: the adapter could not serve that request. The
//     entry is lost and the run is not, and it is reported as a
//     [github.com/Zaba505/cpybkc/internal/conformance.RunError] — never as a
//     mismatch, which would send a generator author to a specification section
//     about a claim that was never tested.
//   - A broken adapter is a non-zero exit, a stream that stopped parsing, or a
//     deadline that expired. That conversation is over; this package kills the
//     process and starts a fresh one on the entries that were left, so that one
//     entry's crash costs that entry rather than the run.
//
// # Deadlines, and why they are here rather than in the adapter
//
// docs/adapter/SPEC.md, "Deadlines and lifetime belong to the engine": an
// adapter that gave up on a slow entry would turn one slow entry into a broken
// adapter and cost the run everything after it, where the engine's own deadline
// costs that entry alone. So every request this package sends is bounded, and
// an expiry kills the process rather than reading on — an adapter killed
// mid-frame may have written half a line, and a stream whose next byte is the
// middle of an abandoned frame cannot be resynchronised by anything the
// receiver can see.
//
// # A door is how a process gets started, and the contract begins after that
//
// The argument vector, the working directory, the environment and whether the
// process is local at all are not the contract's, which is why they are not
// this package's either: [Door] is the seam, and the two doors this package
// ships are siblings behind one implementation of the conversation (#203).
//
//   - [Command] runs a command. It provides nothing beyond a process: no
//     network namespace, no read-only root, no cap of any kind. A result
//     produced through it is the author's own working result.
//   - [Image] runs a container image, with no network, a read-only root, a
//     writable tmpfs at /tmp, a memory cap, a process cap and a wall-clock
//     bound. A result produced through it is one the author can hand to
//     somebody else.
//
// What a door adds is the door's own to describe and is never the contract's,
// and [Report] quotes that description rather than claiming a guarantee of its
// own — an engine MUST NOT report a result as though it carried a guarantee its
// door did not provide. Neither door makes a run a claim a third party should
// be asked to trust without qualification; both are self-reports, and the
// difference between them is how much of one.
//
// # Failure reporting is the point
//
// An engine that says `packed-comp6: mismatch` has done the minimum. This one
// holds the descriptor, so a disagreement is followed by where the descriptor
// puts the field that disagreed — its offset within the record, its width, its
// usage and its charset — and by the bytes of input.bin it was read from, where
// the framing lets those bytes be found. That is available to the engine and to
// nothing else in the system: the adapter was never told what was expected, and
// the corpus package compares documents rather than bytes.
package engine
