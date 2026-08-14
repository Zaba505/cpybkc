// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// brokenError is the adapter having stopped being an adapter: it exited, it
// wrote something that is not a frame, it answered a request nobody sent, or it
// did not answer inside the deadline.
//
// It is a type rather than a sentinel because what the engine does about it is
// not what it does about a fault. A fault costs one entry and the conversation
// goes on; this ends the conversation, and the remaining entries are carried to
// a fresh process — so the two have to be told apart at the point they are
// caught rather than by reading the message.
type brokenError struct {
	// Err is what stopped the conversation.
	Err error

	// Diagnostics is what the adapter had written to standard error, quoted
	// beside the fault because a broken adapter's own words are usually the
	// only explanation there is. It is filled in when the process is stopped,
	// so that a stream that stopped parsing and the panic trace that explains it
	// arrive together.
	Diagnostics string
}

func (e *brokenError) Error() string {
	if e.Diagnostics == "" {
		return e.Err.Error()
	}

	return fmt.Sprintf("%v\nthe adapter's standard error:\n%s", e.Err, e.Diagnostics)
}

func (e *brokenError) Unwrap() error { return e.Err }

// broke reports whether an error ended the conversation.
func broke(err error) bool {
	var broken *brokenError

	return errors.As(err, &broken)
}

// conn is one conversation with one adapter process.
//
// It holds the identifier the next request carries, which is strictly
// increasing over the conversation and starts at 1. Under a strictly
// alternating conversation that identifier is redundant, which is exactly why
// it is cheap and worth carrying: it is the only thing that turns a stream that
// has silently desynchronised — one extra frame, one frame swallowed — into an
// error at the frame where it happened rather than a wrong answer several
// entries later.
type conn struct {
	process Process
	out     *bufio.Reader
	next    int
}

// newConn begins a conversation over a started process.
func newConn(process Process) *conn {
	return &conn{
		process: process,

		// A bufio.Reader rather than a fixed buffer, because a receiver MUST
		// accept a frame of any length: a values document for an entry with a
		// large table is long, and ReadString grows for it rather than
		// truncating at whatever the buffer happened to be.
		out:  bufio.NewReader(process.Stdout()),
		next: 1,
	}
}

// exchange sends one request and waits for the response that answers it,
// bounded by the deadline.
//
// The conversation is strictly alternating — the engine MUST NOT send a request
// before the previous one's response has arrived — so this is the only way a
// frame is written, and there is at most one read outstanding at a time.
func (c *conn) exchange(ctx context.Context, deadline time.Duration, req *request) (*response, error) {
	req.ID = c.next
	c.next++

	frame, err := marshal(req)
	if err != nil {
		// A request this engine could not write is this engine's fault and not
		// the adapter's, but the conversation is over either way: nothing was
		// sent, and the adapter is waiting for something.
		return nil, &brokenError{Err: err}
	}

	if err := c.write(ctx, deadline, req, frame); err != nil {
		return nil, err
	}

	got, err := c.read(ctx, deadline, req)
	if err != nil {
		return nil, err
	}

	// An engine MUST refuse a response whose id is not the one it is waiting
	// for, and refusing means treating the peer as broken rather than
	// resynchronising by skipping the frame and reading on. A stream whose
	// framing is in doubt cannot be resynchronised by anything the receiver can
	// see, so an engine that skipped and an engine that killed would report
	// different things about the same adapter.
	if *got.ID != req.ID {
		return nil, &brokenError{Err: fmt.Errorf("the adapter answered request %d while %s was request %d",
			*got.ID, req.Op, req.ID)}
	}

	return got, nil
}

// write puts one frame on the adapter's standard input, bounded by the same
// deadline as the answer to it.
//
// One Write of the whole line, which is the flush the contract requires after
// every frame: the conversation is strictly alternating, so a side that
// buffered a frame would deadlock against a peer waiting for it, and the peer
// has no way out — an adapter blocked on a request the engine never flushed is
// forbidden to time out, to exit, and to answer.
//
// The bound is the half that is easy to leave out, and leaving it out is a hang
// with no way out either. An adapter that stops *reading* — one that deadlocked
// in its own startup, or that answered the handshake and then stopped attending
// to its input — parks this write forever once the pipe's buffer is full, and
// the generate frame carries every entry's descriptor at once, so it is
// comfortably the frame that fills one. A deadline on the read alone would
// never be reached, because the request it was waiting to have answered was
// never sent.
func (c *conn) write(ctx context.Context, deadline time.Duration, req *request, frame []byte) error {
	// Written in a goroutine for the reason the read is: there is no way to
	// interrupt a blocking write to a pipe, and what ends it is the process
	// being killed. The channel is buffered so that a write which completes
	// after the deadline was given up on is discarded rather than leaking the
	// goroutine holding it.
	written := make(chan error, 1)

	go func() {
		_, err := c.process.Stdin().Write(frame)
		written <- err
	}()

	timer := time.NewTimer(deadline)
	defer timer.Stop()

	select {
	case err := <-written:
		if err != nil {
			return &brokenError{Err: fmt.Errorf("the adapter stopped reading its input during %s: %w", req.Op, err)}
		}

		return nil
	case <-timer.C:
		return &brokenError{Err: fmt.Errorf("the adapter did not read the %s request within %s", req.Op, deadline)}
	case <-ctx.Done():
		return &brokenError{Err: fmt.Errorf("the run was cancelled while %s was being sent: %w", req.Op, ctx.Err())}
	}
}

// read waits for one frame, or for the deadline.
//
// The read runs in a goroutine of its own because there is no way to interrupt
// a blocking read of a pipe: what ends it is the process being killed, which
// closes the pipe under it. The channel is buffered so that a read which lands
// after the deadline has already been given up on is discarded rather than
// leaking the goroutine holding it.
func (c *conn) read(ctx context.Context, deadline time.Duration, req *request) (*response, error) {
	type frame struct {
		line string
		err  error
	}

	lines := make(chan frame, 1)

	go func() {
		line, err := c.out.ReadString('\n')
		lines <- frame{line: line, err: err}
	}()

	timer := time.NewTimer(deadline)
	defer timer.Stop()

	select {
	case got := <-lines:
		if got.err != nil && got.line == "" {
			if errors.Is(got.err, io.EOF) {
				return nil, &brokenError{Err: fmt.Errorf("the adapter closed its output without answering %s", req.Op)}
			}

			return nil, &brokenError{Err: fmt.Errorf("failed to read the adapter's answer to %s: %w", req.Op, got.err)}
		}

		// A line that arrived without its terminator because the stream ended
		// is still a line to report, and [unmarshal] is what says so: a frame
		// cut off halfway is the most useful thing there is to quote.
		frame, err := unmarshal(got.line)
		if err != nil {
			return nil, &brokenError{Err: fmt.Errorf("the adapter's answer to %s is not a frame: %w", req.Op, err)}
		}

		return frame, nil
	case <-timer.C:
		return nil, &brokenError{Err: fmt.Errorf("the adapter did not answer %s within %s", req.Op, deadline)}
	case <-ctx.Done():
		return nil, &brokenError{Err: fmt.Errorf("the run was cancelled while %s was outstanding: %w", req.Op, ctx.Err())}
	}
}

// end closes the conversation and reports how the adapter exited.
//
// An engine that stops early — a refused handshake, a broken adapter, a run it
// abandons — MUST close the adapter's standard input, and MAY then kill the
// process. Without the close the adapter is left waiting on a conversation that
// is over and forbidden to leave it, which is a hang neither side is at fault
// for; that obligation is the other half of the prohibition on an adapter
// exiting quietly mid-conversation.
//
// The kill after it is what bounds this: an adapter that ignores end of input
// would otherwise hold the run open for as long as it liked, and the contract
// gives it no right to. An adapter killed here is not in violation of anything.
func (c *conn) end(grace time.Duration) error {
	// The close is the message. Its error is not reported, because the only one
	// it has to report is a pipe the adapter has already closed by exiting,
	// which is the state being asked for.
	_ = c.process.Stdin().Close()

	exited := make(chan error, 1)

	go func() {
		exited <- c.process.Wait()
	}()

	timer := time.NewTimer(grace)
	defer timer.Stop()

	select {
	case err := <-exited:
		return err
	case <-timer.C:
		c.process.Kill()

		// Waited for after the kill so that the process is reaped and its
		// standard error has been copied in full. What comes back is the signal
		// that killed it, which is not the adapter's verdict on anything and is
		// dropped for that reason: the fault being reported is that it did not
		// leave when the conversation ended.
		<-exited

		return fmt.Errorf("the adapter did not exit within %s of its input being closed, and was killed", grace)
	}
}

// abandon ends a conversation that is already over, and hands back what the
// adapter said as it went.
//
// The process is killed rather than waited for. Its input is closed first all
// the same, because the contract obliges an engine that stops early to close
// it, and an adapter that is merely slow rather than broken deserves the chance
// to exit of its own accord — but nothing here waits to find out whether it
// took it.
func (c *conn) abandon() string {
	_ = c.process.Stdin().Close()
	c.process.Kill()

	// Reaped so that the pipe copying finishes and the diagnostics are whole.
	// How it exited says nothing that is not already known: it was killed.
	_ = c.process.Wait()

	return c.process.Diagnostics()
}
