// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"context"
	"fmt"
)

// scaffold is `cpybkc init`: the copybooks the line named, read, and the layout
// scaffold they decide, written where --out says.
//
// # What is here, and what is not
//
// The vector is here — which copybooks were named, in which order and as they
// were typed, and where the scaffold goes — because that is the half of
// docs/cli/SPEC.md's `init` this story lands (#214). The derivation is not: a
// record per 01-level, an alternative per REDEFINES outside a repeating group,
// the commented forms whose subject is computable and whose value is not, the
// destination that is never overwritten and the combination count reported on
// standard error are all #215's, and they are a reading of a copybook rather
// than of a command line.
//
// So this build understands the line and cannot perform it, which is exactly the
// distinction the exit statuses encode: status 1, the run failed, and not status
// 2, which promises the vector was not understood and that cpybkc did nothing at
// all. Reporting the vector's own faults correctly while exiting 0 over a
// scaffold nobody wrote would be the one answer worse than either — silence is
// success for a run by docs/cli/SPEC.md's rule, and a person would go looking
// for a file that is not there.
//
// It takes neither stream. Nothing is written to standard output until there is
// a scaffold to write, and the one line this stage has to say is a failure,
// which [run] reports on standard error where every other fault is reported.
// #215 is what gives it the writer, beside the file it writes.
//
// It does take the invocation, which it does not yet read. That is this stage's
// whole input — which copybooks, in which order, and where the scaffold goes —
// and the parse that produces it is what this story lands; a signature that
// omitted it would have to be changed by #215 in the same commit that reads it,
// and the branch in [execute] would read as though the vector were beside the
// point.
func scaffold(ctx context.Context, inv invocation) error {
	// A run cancelled before it starts is a cancelled run. The check is here for
	// the reason it is on the emitting path: there is no output tree to leave
	// alone, but there is a file about to appear at a path somebody named, and a
	// signal that arrived first is the answer they are owed rather than whatever
	// the stage would otherwise have said.
	if err := ctx.Err(); err != nil {
		return cancelled(ctx, err)
	}

	// The message says what the reader can act on and nothing else: the line was
	// understood, the build cannot carry it out, and no file was written. It
	// names no issue number — a tracker is not something the person who ran the
	// command can reach, and the reference belongs in the comment above, where
	// the reader who can act on it is already looking.
	return fmt.Errorf("cpybkc %s is not implemented in this build: the arguments were read and understood, "+
		"and the derivation that writes a scaffold from copybooks is not in it yet; nothing was written",
		initSubcommand)
}
