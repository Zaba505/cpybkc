// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package engine

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Protocol is the version of docs/adapter/SPEC.md this engine speaks, which the
// handshake states and an adapter either matches or does not. There is no range
// and no fallback: an adapter speaks one version, and either it is this one or
// the run does not happen.
const Protocol = 1

// The operations, spelled once. They are constants because each is written by
// the code that builds a request and read by the tests that pin the
// conversation, and an operation the tests agree with and an adapter does not is
// the one thing this contract cannot afford.
const (
	opHello     = "hello"
	opGenerate  = "generate"
	opDecode    = "decode"
	opRoundtrip = "roundtrip"
	opBye       = "bye"
)

// The capabilities an adapter may declare. `rebuild` is deliberately absent
// from this engine's vocabulary rather than declared and unused: a corpus run
// generates once and asks each entry once, so it has nothing to rebuild, and an
// engine that read a capability it never exercised would be claiming a
// conversation it does not have.
const capabilityWrite = "write"

// The two kinds an adapter may declare. An engine MUST refuse a kind it does
// not recognise rather than falling back to one it does: a value added later
// means something, and treating it as the nearest thing that already existed is
// how a run reports a result about a thing it did not understand.
const (
	kindCodec       = "codec"
	kindDescriptive = "descriptive"
)

// request is a frame the engine writes.
//
// Every optional member is omitted when empty, so that each operation's frame
// carries the members that operation defines and nothing else. A receiver is
// required to ignore a member it does not recognise, so an engine that sent
// them all would be conforming and unreadable.
type request struct {
	ID int    `json:"id"`
	Op string `json:"op"`

	// Protocol is the handshake's, and is the version this engine speaks.
	Protocol int `json:"protocol,omitempty"`

	// Entries is generate's: every entry at once, which is what lets an adapter
	// for a compiled language build the corpus in one invocation of its
	// toolchain rather than once per entry.
	Entries []requestEntry `json:"entries,omitempty"`

	// Entry is decode's and roundtrip's: which entry the request is about.
	Entry string `json:"entry,omitempty"`

	// Input is decode's: the entry's bytes, which encoding/json writes as
	// base64 in RFC 4648 section 4's alphabet — the alphabet the contract names
	// and the one a values document already spells a run of bytes in.
	//
	// The bytes travel in the frame rather than as a path because the door that
	// produces a believable result runs the adapter with no access to the corpus
	// directory at all, and because an adapter that could open input.bin could
	// open values.json beside it.
	//
	// A pointer, so that an entry whose file holds no bytes is asked about with
	// an empty input rather than with no input member at all. An empty file is a
	// file — a layout whose sequencing expression accepts nothing is exactly
	// what one entry ought to be about — and the two are different questions.
	Input *[]byte `json:"input,omitempty"`
}

// requestEntry is one entry of a generate request: its name, and its descriptor
// in the binary encoding docs/plugin/SPEC.md hands a generator.
type requestEntry struct {
	Entry      string `json:"entry"`
	Descriptor []byte `json:"descriptor"`
}

// response is a frame the adapter writes.
//
// Every member an adapter may omit is a pointer or a nil-able type, because
// absent and zero are different answers here: an `ok` nobody wrote is a
// malformed frame, where an `ok` written false is a fault, and a protocol
// version of 0 is a version an adapter stated rather than one it left out.
//
// Unknown members are not refused. That is the opposite of the rule the corpus
// format applies to an entry, and the difference is who writes the document: an
// entry is written by a person, where an unrecognised member is a typo, and a
// frame is written by a program against a version it announced at the
// handshake, where an unrecognised member is one a later version of the
// contract added. A receiver that refused it would make every future addition a
// breaking change.
type response struct {
	ID    *int   `json:"id"`
	OK    *bool  `json:"ok"`
	Error string `json:"error"`

	// Protocol is the version the adapter speaks — stated, not echoed, so that
	// a refused handshake can say which two versions failed to meet.
	Protocol *int `json:"protocol"`

	Name    string `json:"name"`
	Version string `json:"version"`
	Kind    string `json:"kind"`

	// Capabilities is a pointer because the member is required of a successful
	// handshake even when it is empty: an author who has to write `{}` has been
	// asked the question, where an author who may omit it has not.
	Capabilities *map[string]bool `json:"capabilities"`

	// Entries is a generate response's per-entry results.
	Entries []entryResult `json:"entries"`

	// Entry is echoed by decode and roundtrip.
	Entry string `json:"entry"`

	// Decoded and Written are values documents, held back as raw JSON so that
	// they are read by the corpus format's own reader rather than by this one.
	// The relaxation above is the envelope's and stops at the envelope: an
	// unknown member inside one of these is refused exactly as the corpus format
	// requires.
	Decoded json.RawMessage `json:"decoded"`
	Written json.RawMessage `json:"written"`
}

// entryResult is what a generate or rebuild response says about one entry.
type entryResult struct {
	Entry string `json:"entry"`
	OK    *bool  `json:"ok"`
	Error string `json:"error"`
}

// served is whether the adapter served the request at all, which is `ok` and
// never the presence of an answer beside it.
func (r *response) served() bool { return r.OK != nil && *r.OK }

// capabilities is what the adapter declared, with an absent object read as no
// capabilities rather than as an error — the handshake is where a missing one
// is refused, so that every caller of this does not have to.
func (r *response) capabilities() map[string]bool {
	if r.Capabilities == nil {
		return nil
	}

	return *r.Capabilities
}

// marshal writes a request as the one line it goes out as.
//
// encoding/json escapes every control character inside a string, so the line
// feed appended here is the only one in the frame — which is what makes a line
// feed a frame delimiter at all.
func marshal(req *request) ([]byte, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to write the %s request: %w", req.Op, err)
	}

	return append(b, '\n'), nil
}

// unmarshal reads one line of the adapter's standard output as a response
// frame, and refuses everything the framing does not admit.
//
// The line arrives with its terminator, or without one at end of input. What is
// refused here is what docs/adapter/SPEC.md, "A frame is one line of UTF-8
// JSON", requires be refused rather than tolerated:
//
//   - a blank line, because a skipped one is a corrupted stream reported one
//     frame later, at whichever frame first fails to parse;
//   - a carriage return before the line feed, because a stream that tolerates
//     both is one where a text-mode file handle silently changes what was sent;
//   - anything that is not one JSON object, which is the greeting a library
//     printed the first time it was used, arriving where a frame was expected.
//
// A line that is not a well-formed frame is an adapter fault, and it is quoted
// back, because that line is usually the diagnostic that explains it.
func unmarshal(line string) (*response, error) {
	body, ok := strings.CutSuffix(line, "\n")
	if !ok {
		return nil, fmt.Errorf("the adapter's output ended in the middle of a frame: %s", quoted(line))
	}

	if strings.HasSuffix(body, "\r") {
		return nil, fmt.Errorf("a frame is terminated by a line feed and this one carries a carriage return before it: %s",
			quoted(body))
	}

	if body == "" {
		return nil, fmt.Errorf("a blank line is not a frame")
	}

	var frame response
	if err := json.Unmarshal([]byte(body), &frame); err != nil {
		return nil, fmt.Errorf("a frame is one JSON object on one line, and this is not: %s", quoted(body))
	}

	if frame.ID == nil {
		return nil, fmt.Errorf("the frame carries no id, and an id is what pairs it with a request: %s", quoted(body))
	}

	if frame.OK == nil {
		return nil, fmt.Errorf("the frame carries no ok, so it says nothing about whether the request was served: %s",
			quoted(body))
	}

	if !frame.served() && frame.Error == "" {
		return nil, fmt.Errorf("the frame refused the request and says nothing about why: %s", quoted(body))
	}

	return &frame, nil
}

// quotedLimit is how much of an unreadable line is quoted back.
//
// A line that is not a frame is usually a diagnostic and occasionally a
// megabyte of a runtime's stack trace, and a report is read in a terminal.
const quotedLimit = 240

// quoted is a line as a diagnostic should carry it: as a Go quoted string, so
// that a stray carriage return or a control character is visible rather than
// something that moved the cursor, and shortened to something a report can hold.
func quoted(line string) string {
	if len(line) > quotedLimit {
		return fmt.Sprintf("%q (and %d more bytes)", line[:quotedLimit], len(line)-quotedLimit)
	}

	return fmt.Sprintf("%q", line)
}
