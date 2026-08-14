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
func scaffold(ctx context.Context, inv invocation) error {
	// A run cancelled before it starts is a cancelled run. The check is here for
	// the reason it is on the emitting path: there is no output tree to leave
	// alone, but there is a file about to appear at a path somebody named, and a
	// signal that arrived first is the answer they are owed rather than whatever
	// the stage would otherwise have said.
	if err := ctx.Err(); err != nil {
		return cancelled(ctx, err)
	}

	return fmt.Errorf(
		"cpybkc %s read its vector — %d copybook(s), writing to %q — and this build cannot derive a scaffold "+
			"from them; the derivation is the story that follows this one (#215), and until it lands %s is a "+
			"line cpybkc understands and cannot perform",
		initSubcommand, len(inv.copybooks), inv.out, initSubcommand)
}
