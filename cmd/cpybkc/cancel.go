// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
)

// errCancelled is a run stopped by a signal.
//
// docs/cli/SPEC.md: on SIGINT or SIGTERM cpybkc stops the run, leaves the
// project's output tree exactly as it found it, removes the scratch directories
// and the descriptor files it created, and exits 1 with an `error:` diagnostic
// saying the run was cancelled. The first three are what cancelling the context
// buys — a generator is killed with it, nothing is merged unless every generator
// exited zero, and the scratch space is removed however the run ended — and this
// is the fourth.
//
// Exiting 1 rather than dying by the signal is a departure from the convention
// that a program killed by SIGINT re-raises it, and it is deliberate: the
// cleanup above is the reason the signal is caught at all, and a cancelled run
// is a failed run by the definition already in the table.
var errCancelled = errors.New("the run was cancelled")

// signalled is the set a run is stopped by. It is the two docs/cli/SPEC.md
// names and no others: cpybkc catches a signal in order to clean up after
// itself, and a signal it has nothing to do about is one whose default
// disposition is the right answer.
var signalled = []os.Signal{syscall.SIGINT, syscall.SIGTERM}

// cancellable is ctx, cancelled by the first SIGINT or SIGTERM to arrive, with
// [errCancelled] as the cause.
//
// The second signal is deliberately not caught. docs/cli/SPEC.md leaves it to
// its default disposition, so that a run holding a generator that will not exit
// can still be killed by repeating the gesture that did not work — which is why
// the notification is stopped as the first one is handled rather than when the
// process ends.
//
// The returned function releases the notification and cancels the context. It
// is safe to call more than once and is what a caller defers.
func cancellable(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(ctx)

	// Buffered, because a signal is delivered without blocking and a
	// notification nobody was ready for is dropped: an unbuffered channel here
	// would lose the interrupt that arrives while the run is between stages.
	arrived := make(chan os.Signal, 1)
	signal.Notify(arrived, signalled...)

	go func() {
		select {
		case <-arrived:
			signal.Stop(arrived)
			cancel(errCancelled)
		case <-ctx.Done():
			signal.Stop(arrived)
		}
	}()

	return ctx, func() { cancel(context.Canceled) }
}

// cancelled reports err as the cancellation it was, or leaves it alone.
//
// A cancelled run surfaces from the stage that was running as whatever that
// stage makes of a context that is done — a generator killed, a read that never
// started — and none of those messages says what happened. The cause carried on
// the context does, and this is where it is put back: docs/cli/SPEC.md requires
// the diagnostic to say the run was cancelled, and every other member of status
// 1 already explains itself.
func cancelled(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}

	if cause := context.Cause(ctx); errors.Is(cause, errCancelled) {
		return cause
	}

	return err
}
