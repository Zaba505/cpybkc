// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package descriptive

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Protocol is the version of docs/adapter/SPEC.md this adapter speaks. There is
// no range and no fallback: an adapter speaks one version, and either it is the
// engine's or the run does not happen.
const Protocol = 1

// The operations this adapter recognises. An op that is not one of these is
// refused with ok: false rather than ignored — an unrecognised member is an
// extension a receiver can safely do nothing about, and an unrecognised
// operation is work it cannot do.
//
// The four between hello and bye are recognised in order to be refused. A
// correct engine never sends one to an adapter that declared itself
// descriptive, and an adapter that answered one anyway would be answering about
// a file nothing read.
const (
	opHello     = "hello"
	opGenerate  = "generate"
	opDecode    = "decode"
	opRoundtrip = "roundtrip"
	opRebuild   = "rebuild"
	opBye       = "bye"
)

// kindDescriptive is what this adapter declares, and the whole reason it
// exists: the generator behind it emits something other than code that reads a
// file, so the corpus has nothing to ask it and the run is not applicable
// rather than failed.
const kindDescriptive = "descriptive"

// request is a frame the engine writes.
//
// It carries the three members a descriptive conversation turns on and no
// others, because a member this adapter does not recognise is ignored rather
// than refused: a frame is written by a program against a version announced at
// the handshake, where an unrecognised member is one a later version of the
// contract added, and a receiver that refused it would make every future
// addition a breaking change. encoding/json ignores one by default, which is
// what implements that here — and is why generate's entries and decode's input
// need no field to be safely discarded.
type request struct {
	ID *int   `json:"id"`
	Op string `json:"op"`

	// Protocol is the handshake's: the version the engine speaks.
	Protocol *int `json:"protocol"`
}

// response is a frame this adapter writes.
//
// `id` and `ok` are written unconditionally, because the contract requires them
// of every response and a zero one is an answer rather than an omission. Every
// other member is omitted when empty, `error` included: it is present exactly
// when `ok` is false, which is what omitting it from a successful frame
// implements. So each frame carries what its operation defines and nothing else.
type response struct {
	ID    int    `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`

	// Protocol is the handshake's, and is stated rather than echoed: an adapter
	// that does not speak the engine's version answers ok: false and states its
	// own anyway, so that a report can say which two versions failed to meet.
	Protocol *int `json:"protocol,omitempty"`

	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	Kind    string `json:"kind,omitempty"`

	// Capabilities is a pointer because the member is required of a successful
	// handshake even when it is empty — and here it is always empty, which is
	// the case the pointer is for: an adapter that serves no optional operation
	// says so with `{}`, and a member left out would be read as an adapter that
	// was never asked which it serves.
	Capabilities *map[string]bool `json:"capabilities,omitempty"`
}

// refuse is a response that did not serve the request. It is a fault and never a
// wrong answer: the entry is lost, and the run is not.
func refuse(id int, format string, args ...any) *response {
	return &response{ID: id, OK: false, Error: fmt.Sprintf(format, args...)}
}

// parse reads one line of the engine's standard output as a request frame, and
// refuses everything the framing does not admit.
//
// Refusing here means the conversation stops and the peer is treated as broken,
// rather than as having asked something: a stream whose framing is in doubt
// cannot be resynchronised by anything the receiver can see, so an adapter that
// skipped a bad line and read on would be answering questions it cannot know it
// was asked.
//
// The line arrives with its terminator. What is refused is what
// docs/adapter/SPEC.md, "A frame is one line of UTF-8 JSON", requires be refused
// rather than tolerated: a blank line, a carriage return before the line feed,
// and anything that is not one JSON object carrying an id and an op.
func parse(line string) (*request, error) {
	body, ok := strings.CutSuffix(line, "\n")
	if !ok {
		return nil, fmt.Errorf("the engine's input ended in the middle of a frame: %s", quoted(line))
	}

	if strings.HasSuffix(body, "\r") {
		return nil, fmt.Errorf("a frame is terminated by a line feed and this one carries a carriage return before it: %s",
			quoted(body))
	}

	if body == "" {
		return nil, fmt.Errorf("a blank line is not a frame")
	}

	var frame request
	if err := json.Unmarshal([]byte(body), &frame); err != nil {
		// Two different failures reach this branch: a line that is not one JSON
		// object, and a line that is one but spells a member as something the
		// contract does not give it — a protocol that is a float, an id that is
		// a string. Both stop the conversation, because a frame whose members
		// are not the types the contract fixes is a peer this adapter cannot
		// read rather than a request it could refuse. Which of the two it was is
		// the decoder's to say, so its words are carried rather than dropped.
		return nil, fmt.Errorf("a frame is one JSON object on one line, written in the types the contract gives its "+
			"members, and this is not: %w: %s", err, quoted(body))
	}

	if frame.ID == nil {
		return nil, fmt.Errorf("the frame carries no id, and an id is what pairs it with a response: %s", quoted(body))
	}

	if frame.Op == "" {
		return nil, fmt.Errorf("the frame carries no op, so it asks for nothing: %s", quoted(body))
	}

	return &frame, nil
}

// quotedLimit is how much of an unreadable line is quoted back. A line that is
// not a frame is usually a diagnostic and occasionally a megabyte of a runtime's
// stack trace, and a report is read in a terminal.
const quotedLimit = 240

// quoted is a line as a diagnostic should carry it: as a Go quoted string, so
// that a stray carriage return is visible rather than something that moved the
// cursor, and shortened to something a report can hold.
func quoted(line string) string {
	if len(line) > quotedLimit {
		return fmt.Sprintf("%q (and %d more bytes)", line[:quotedLimit], len(line)-quotedLimit)
	}

	return fmt.Sprintf("%q", line)
}
