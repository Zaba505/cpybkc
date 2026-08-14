// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package goadapter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Zaba505/cpybkc/internal/plugin"
)

// Adapter drives one Go generator through the conformance adapter contract.
//
// The generator is a path rather than a name on PATH, because a run is nearly
// always against a generator just built from the tree under test, and resolving
// a name would find whichever one an author happened to have installed.
type Adapter struct {
	// Root is the repository root: where the scratch tree is made and where the
	// go tool is run. It is required, and a run refuses an empty one before
	// anything is created, so that nothing here can reach a directory the
	// machine happened to name (#184).
	Root string

	// Name is the generator's name, as a diagnostic about the invocation should
	// spell it — the `<name>` of a cpybkc-gen-<name> executable.
	Name string

	// Generator is the executable to run. It is required.
	Generator string

	// Version is what to tell the engine this adapter is, beside [Adapter.Name].
	// It is quoted in a report and compared by nothing.
	Version string

	// Options are the options to pass the generator beyond the package name, in
	// the order they are to be passed.
	Options []plugin.Option
}

// Serve holds one conversation: it reads request frames from in, writes response
// frames to out, and returns when the engine says goodbye or closes its end.
//
// A nil error is the one exit an adapter may make zero from, which is bye
// answered or end of input seen. Every other return is this adapter reporting
// that it broke, which is what a non-zero exit means and the only thing it
// means: bytes that decoded wrongly, a reader that refused a file and a writer
// that refused a record are answers, and each comes back inside a frame.
//
// Nothing is written to out but frames. That is the caller's to arrange as well
// — see the recipe in cmd/adapter — because a library that greets the world on
// standard output turns every subsequent frame into a parse error, and the
// failure surfaces as an adapter that appears to have answered gibberish.
func (a *Adapter) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	switch {
	case a.Root == "":
		return fmt.Errorf("this adapter has no Root, and it is where the scratch tree is made and the go tool is run")
	case a.Generator == "":
		return fmt.Errorf("this adapter has no Generator, and it is the executable to drive")
	}

	// Absolute once, here: every path this conversation writes, builds and runs
	// is under it, and a relative one would mean something different to the go
	// tool, to a codec program and to this process the moment any of them was
	// given a directory of its own.
	root, err := filepath.Abs(a.Root)
	if err != nil {
		return fmt.Errorf("failed to resolve the repository root %s: %w", a.Root, err)
	}

	held := &conversation{adapter: a, root: root}
	defer held.close()

	// ReadString rather than a Scanner: a receiver MUST accept a frame of any
	// length, and a generate frame carrying every descriptor of the corpus is
	// already past the size a Scanner reads into by default.
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

		answer, done := held.serve(ctx, req)

		if err := write(out, answer); err != nil {
			return err
		}

		if done {
			return nil
		}
	}
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
		// Not reachable through a values document, which is held to being JSON
		// where that is a fault against one entry ([child.read]). What is left
		// is this adapter breaking, and a frame that cannot be written is not
		// something there is a frame to say.
		return fmt.Errorf("failed to write a response frame: %w", err)
	}

	if _, err := out.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("failed to write a response frame: %w", err)
	}

	return nil
}

// conversation is the state one adapter process keeps: where it built, what it
// built, and the codec program still holding the records of the last decode.
type conversation struct {
	adapter *Adapter

	// root is [Adapter.Root], made absolute.
	root string

	// scratch is the tree this conversation generates, writes and builds in. It
	// is made when generate arrives and removed when the conversation ends.
	scratch string

	// built is every entry generate produced a codec program for, by name.
	built map[string]*built

	// held is the codec program of the most recent decode, still running and
	// still holding the records its reader produced. It is what roundtrip is
	// served out of.
	held *child
}

// serve answers one request, and says whether the conversation is over.
func (c *conversation) serve(ctx context.Context, req *request) (*response, bool) {
	id := *req.ID

	switch req.Op {
	case opHello:
		return c.hello(id, req), false
	case opGenerate:
		return c.generate(ctx, id, req), false
	case opDecode:
		return c.decode(ctx, id, req), false
	case opRoundtrip:
		return c.roundtrip(id, req), false
	case opRebuild:
		// A request for a capability this adapter did not declare. It is
		// refused rather than attempted, which is one of the two things an
		// adapter MUST refuse and is a fact about itself it knows without
		// keeping any record.
		return refuse(id, "this adapter did not declare the %s capability, so it has nothing warm to regenerate one entry in",
			capabilityRebuild), false
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

// hello is the handshake: what this adapter is, which version of the contract it
// speaks, and which optional operations it serves.
func (c *conversation) hello(id int, req *request) *response {
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

	// Required even when it is empty, so it is written out in full rather than
	// assembled from whatever happens to be true.
	capabilities := map[string]bool{capabilityWrite: true}

	return &response{
		ID:           id,
		OK:           true,
		Protocol:     &spoken,
		Name:         c.adapter.name(),
		Version:      c.adapter.Version,
		Kind:         kindCodec,
		Capabilities: &capabilities,
	}
}

// decode reads one entry's bytes with the code the generator produced for it.
//
// The bytes come out of the frame and are written into this adapter's own
// scratch tree, never read from the corpus: an adapter that could open input.bin
// could open values.json beside it, and the door that produces a believable
// result gives it no access to that directory at all.
func (c *conversation) decode(ctx context.Context, id int, req *request) *response {
	entry, ok := c.built[req.Entry]
	if !ok {
		return refuse(id, "this adapter has no code for %q: generate did not produce any", req.Entry)
	}

	// The previous decode's records are discarded here and nowhere else, which
	// is the retention the contract asks for: one entry's records, until the
	// next decode or the end of the conversation.
	c.stop()

	// An empty file is a file, so a nil input is written as a file of no bytes
	// rather than refused.
	input := req.Input
	if input == nil {
		input = []byte{}
	}

	if err := os.WriteFile(entry.input, input, 0o600); err != nil {
		return refuse(id, "failed to write the bytes to read: %v", err)
	}

	started, err := start(ctx, entry)
	if err != nil {
		return refuse(id, "the generated code did not run: %v", err)
	}

	document, err := started.read()
	if err != nil {
		return refuse(id, "the generated code did not read this entry: %v", started.stop(err))
	}

	// One member of the document is looked at, and it is not a value: whether
	// the read stopped. It is what says this entry has no complete set of
	// records to write back, which is roundtrip's precondition and a fact about
	// this adapter rather than about the answer.
	var stopped struct {
		Failure string `json:"failure"`
	}

	if err := json.Unmarshal(document, &stopped); err != nil {
		return refuse(id, "the generated code wrote a document this adapter cannot read: %v", started.stop(err))
	}

	started.refused = stopped.Failure != ""
	c.held = started

	return &response{ID: id, OK: true, Entry: req.Entry, Decoded: document}
}

// roundtrip lays the records the reader just produced back out with the
// generated writer, reads that file with the generated reader, and answers with
// what came back.
//
// The records never leave the codec program. What travels here is the word that
// tells it to write them, which is why this operation carries no records and why
// the program that answered the decode is still running.
func (c *conversation) roundtrip(id int, req *request) *response {
	switch {
	case c.held == nil || c.held.entry != req.Entry:
		// The precondition an adapter MUST check, stated as what it is holding
		// rather than as what was most recently decoded.
		return refuse(id, "this adapter is holding no records of %q to write back", req.Entry)
	case c.held.refused:
		return refuse(id, "the read of %q stopped at a failure, so there is no complete set of records to write back",
			req.Entry)
	}

	document, err := c.held.roundtrip()
	if err != nil {
		err = c.held.stop(err)
		c.held = nil

		return refuse(id, "the generated code did not write this entry back out: %v", err)
	}

	return &response{ID: id, OK: true, Entry: req.Entry, Written: document}
}

// stop ends the codec program holding the last decode's records, if there is
// one. It is what discards those records.
func (c *conversation) stop() {
	if c.held == nil {
		return
	}

	_ = c.held.stop(nil)
	c.held = nil
}

// close ends the conversation: the codec program stops and the scratch tree
// goes.
//
// Neither is reported. Both happen after the last frame this adapter will write,
// so there is nothing left to report them in, and a conversation that answered
// every request and could not tidy up afterwards has not failed at what it was
// asked.
func (c *conversation) close() {
	c.stop()

	if c.scratch != "" {
		_ = os.RemoveAll(c.scratch)
	}
}

// name is what this adapter calls itself in a report, which names the generator
// it drives because that is what a reader of a result wants to know.
func (a *Adapter) name() string {
	name := a.Name
	if name == "" {
		name = "an unnamed Go generator"
	}

	return fmt.Sprintf("cpybkc-gen-%s adapter", name)
}

// child is one codec program: the process that read an entry's bytes and is
// holding the records it produced.
type child struct {
	entry string

	cmd   *exec.Cmd
	stdin io.WriteCloser
	out   *bufio.Reader

	// errs is whatever the program wrote to standard error, and is read only
	// after [child.stop] has waited for it: os/exec copies the stream on a
	// goroutine of its own, and reading it before the wait is a race.
	errs *bytes.Buffer

	// refused is whether the decode this program answered stopped at a failure,
	// which is what makes a roundtrip of it a request this adapter refuses.
	refused bool
}

// The one word a codec program takes on its standard input, and the only thing
// this adapter ever writes to one.
const roundtripCommand = "roundtrip"

// start runs one entry's codec program on the bytes written for it.
func start(ctx context.Context, entry *built) (*child, error) {
	// CommandContext so that a run this adapter's caller gave up on does not
	// leave a codec program behind it.
	cmd := exec.CommandContext(ctx, entry.program, entry.descriptor, entry.input)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	errs := &bytes.Buffer{}
	cmd.Stderr = errs

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &child{
		entry: entry.name,
		cmd:   cmd,
		stdin: stdin,
		out:   bufio.NewReader(stdout),
		errs:  errs,
	}, nil
}

// read takes the one document the codec program writes per question: a values
// document on a line of its own.
func (c *child) read() (json.RawMessage, error) {
	line, err := c.out.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	if line == "" {
		return nil, fmt.Errorf("it wrote no document at all")
	}

	// Held to being JSON here, where it is a fault against one entry, rather
	// than at the marshal of the frame it goes into — which is this adapter
	// breaking, and would cost the run an entry's worth of failure.
	if !json.Valid([]byte(line)) {
		return nil, fmt.Errorf("it wrote a line that is not a document: %s", quoted(line))
	}

	return json.RawMessage(line), nil
}

// roundtrip asks the codec program for the writing direction and takes the
// document it answers with.
func (c *child) roundtrip() (json.RawMessage, error) {
	if _, err := io.WriteString(c.stdin, roundtripCommand+"\n"); err != nil {
		return nil, err
	}

	return c.read()
}

// stop ends the codec program and folds what it wrote to standard error into the
// fault that ended it, which is usually the only explanation there is.
//
// Closing the standard input first is what a codec program is written to end on,
// so an ordinary stop is a process exiting zero rather than one being killed.
// How it ended is kept beside what went wrong rather than instead of it. A codec
// program killed by the engine's deadline, or by the machine running out of
// memory, writes nothing at all — so "it wrote no document" is the whole of the
// fault unless the exit status is carried with it.
func (c *child) stop(err error) error {
	_ = c.stdin.Close()

	ended := errors.Join(err, c.cmd.Wait())
	if ended == nil {
		return nil
	}

	said := c.errs.String()
	if said == "" {
		return ended
	}

	return fmt.Errorf("%w\nwhat it said:\n%s", ended, said)
}
