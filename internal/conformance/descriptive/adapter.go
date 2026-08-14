// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package descriptive

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Adapter is one descriptive generator, as the conformance engine meets it.
//
// Both members are for the report and neither is compared or parsed by
// anything: what a reader of a result wants to know is which generator, at
// which version, declined to be a conformance subject, and nothing else in the
// conversation carries it.
type Adapter struct {
	// Name is the generator's name, as a report should spell it — the `<name>`
	// of a cpybkc-gen-<name> executable.
	Name string

	// Version is which version of it. It is optional, which the contract makes
	// it exactly for the run against a working tree that has no version string
	// to give.
	Version string
}

// Serve holds one conversation: it reads request frames from in, writes response
// frames to out, and returns when the engine says goodbye or closes its end.
//
// A nil error is the one exit an adapter may make zero from, which is bye
// answered or end of input seen. Every other return is this adapter reporting
// that it broke, which is what a non-zero exit means and the only thing it
// means.
//
// Nothing is written to out but frames. That is the caller's to arrange as well
// — see the recipe in cmd/adapter — because a library that greets the world on
// standard output turns every subsequent frame into a parse error, and the
// failure surfaces as an adapter that appears to have answered gibberish.
//
// There is no context to cancel it with, unlike
// [github.com/Zaba505/cpybkc/internal/conformance/goadapter.Adapter.Serve]:
// this adapter starts no process, builds nothing and reads no file, so the only
// thing it ever blocks on is the engine's own input. An engine that gives up
// closes that input, which is end of input and an exit of zero, and one that
// gives up harder kills the process — which an adapter is under no obligation
// to survive gracefully.
func (a *Adapter) Serve(in io.Reader, out io.Writer) error {
	// ReadString rather than a Scanner: a receiver MUST accept a frame of any
	// length. Nothing a descriptive conversation is sent is long, but a frame
	// this adapter refuses is a frame it was sent anyway, and refusing a
	// too-long generate as a broken stream rather than as an operation would
	// report the wrong thing about the engine.
	reader := bufio.NewReader(in)

	for {
		line, err := reader.ReadString('\n')
		if errors.Is(err, io.EOF) && line == "" {
			// End of input is a bye already answered: the adapter exits zero
			// without writing anything further. A run that ended in a fault has
			// no useful frame left to send, and an adapter that waited for a
			// polite goodbye that was never coming would hang at the end of
			// every such run.
			return nil
		}

		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("failed to read a request frame: %w", err)
		}

		req, err := parse(line)
		if err != nil {
			return err
		}

		answer, done := a.serve(req)

		if err := write(out, answer); err != nil {
			return err
		}

		if done {
			return nil
		}
	}
}

// serve answers one request, and says whether the conversation is over.
func (a *Adapter) serve(req *request) (*response, bool) {
	id := *req.ID

	switch req.Op {
	case opHello:
		return a.hello(id, req), false
	case opGenerate, opDecode, opRoundtrip, opRebuild:
		// An engine MUST NOT send one of these to an adapter that declared
		// itself descriptive, so a correct one never arrives. It is refused
		// rather than attempted because there is nothing to attempt: the
		// generator behind this adapter emits a description and never opens a
		// data file, so an answer would be an answer about a file nothing read.
		return refuse(id, "this adapter's generator is %s: it emits something other than code that reads a file, "+
			"so there is nothing to serve %s with", kindDescriptive, req.Op), false
	case opBye:
		return &response{ID: id, OK: true}, true
	default:
		// An unrecognised member is an extension a receiver can safely do
		// nothing about; an unrecognised operation is work it cannot do, and an
		// adapter that reported success for work it did not do is an adapter
		// whose whole report is worthless.
		return refuse(id, "this adapter does not know the operation %q", req.Op), false
	}
}

// hello is the handshake, and the only frame of this conversation that says
// anything.
//
// kind is declared here, before the adapter has been asked anything, because
// the alternative is discovering it from a failure that looks like a wrong
// answer: a descriptive generator scored zero out of the whole corpus, or one
// declining every entry in turn. Neither is true, both read as failures, and the
// truthful answer is reachable in one member of this frame.
func (a *Adapter) hello(id int, req *request) *response {
	spoken := Protocol

	if req.Protocol == nil || *req.Protocol != Protocol {
		// An adapter that does not speak the requested version states its own
		// anyway, so that a report can say which two versions failed to meet
		// instead of only that the handshake failed. A refused handshake
		// carries nothing else: an adapter that could not agree the version has
		// no basis for declaring a kind or a capability set.
		asked := 0
		if req.Protocol != nil {
			asked = *req.Protocol
		}

		answer := refuse(id, "this adapter speaks protocol %d and the engine asked for %d", Protocol, asked)
		answer.Protocol = &spoken

		return answer
	}

	// Empty, and present. Reading is not a capability and this adapter does not
	// read; writing and rebuilding are, and it does neither. An author who has
	// to write `{}` has been asked which optional operations they serve, where
	// one who may omit it has not.
	capabilities := map[string]bool{}

	return &response{
		ID:           id,
		OK:           true,
		Protocol:     &spoken,
		Name:         a.name(),
		Version:      a.Version,
		Kind:         kindDescriptive,
		Capabilities: &capabilities,
	}
}

// name is what a report calls this adapter.
func (a *Adapter) name() string {
	name := a.Name
	if name == "" {
		return "an unnamed descriptive generator's adapter"
	}

	return fmt.Sprintf("cpybkc-gen-%s adapter", name)
}

// write puts one response frame on the stream, terminated by the line feed that
// delimits it.
//
// One call, because the conversation is strictly alternating and a peer waiting
// on a frame this side has half-written is a deadlock neither side can leave.
// The frame is marshalled first for the same reason: a marshal that failed
// halfway through a write would leave the stream unresynchronisable.
func write(out io.Writer, answer *response) error {
	b, err := json.Marshal(answer)
	if err != nil {
		// Not reachable: every member of a response is a string, an integer, a
		// boolean or a map of them. What is left is this adapter breaking, and a
		// frame that cannot be written is not something there is a frame to say.
		return fmt.Errorf("failed to write a response frame: %w", err)
	}

	if _, err := out.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("failed to write a response frame: %w", err)
	}

	return nil
}
