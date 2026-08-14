// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package goadapter

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
const (
	opHello     = "hello"
	opGenerate  = "generate"
	opDecode    = "decode"
	opRoundtrip = "roundtrip"
	opRebuild   = "rebuild"
	opBye       = "bye"
)

// The kinds and capabilities this adapter declares.
//
// kindCodec because cmd/cpybkc-gen-go emits code that reads a file into records
// and writes them back, capabilityWrite because it emits the writer, and
// capabilityRebuild deliberately absent: regenerating one entry inside a warm
// process is an operation this adapter does not serve, and a capability
// declared and unserved is worse than one not declared at all.
const (
	kindCodec         = "codec"
	capabilityWrite   = "write"
	capabilityRebuild = "rebuild"
)

// request is a frame the engine writes.
//
// A member this adapter does not recognise is ignored rather than refused: a
// frame is written by a program against a version announced at the handshake,
// where an unrecognised member is one a later version of the contract added, and
// a receiver that refused it would make every future addition a breaking change.
// encoding/json ignores one by default, which is what implements that here.
type request struct {
	ID *int   `json:"id"`
	Op string `json:"op"`

	// Protocol is the handshake's: the version the engine speaks.
	Protocol *int `json:"protocol"`

	// Entries is generate's, and rebuild carries the same two members flat.
	Entries []requestEntry `json:"entries"`

	// Entry is decode's, roundtrip's and rebuild's.
	Entry string `json:"entry"`

	// Descriptor is rebuild's.
	Descriptor []byte `json:"descriptor"`

	// Input is decode's: the entry's bytes, which encoding/json reads out of
	// base64 in RFC 4648 section 4's alphabet.
	Input []byte `json:"input"`
}

// requestEntry is one entry of a generate request: its name, and its descriptor
// in the binary encoding docs/plugin/SPEC.md hands a generator.
type requestEntry struct {
	Entry      string `json:"entry"`
	Descriptor []byte `json:"descriptor"`
}

// response is a frame this adapter writes.
//
// Every member but the three the contract requires of every response is omitted
// when empty, so that each operation's frame carries what that operation defines
// and nothing else.
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
	// handshake even when it is empty: an author who has to write `{}` has been
	// asked which optional operations they serve.
	Capabilities *map[string]bool `json:"capabilities,omitempty"`

	// Entries is generate's per-entry results.
	Entries []entryResult `json:"entries,omitempty"`

	// Entry is echoed by decode and roundtrip, because an engine checks it
	// rather than trusting it.
	Entry string `json:"entry,omitempty"`

	// Decoded and Written are values documents, carried as raw JSON: they are
	// the corpus format's documents and this envelope changes nothing about
	// them. encoding/json refuses to marshal a RawMessage that is not JSON, so a
	// codec program that wrote something else is a fault this adapter reports
	// rather than a corrupt frame it emits.
	Decoded json.RawMessage `json:"decoded,omitempty"`
	Written json.RawMessage `json:"written,omitempty"`
}

// entryResult is what a generate response says about one entry.
type entryResult struct {
	Entry string `json:"entry"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
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
		return nil, fmt.Errorf("a frame is one JSON object on one line, and this is not: %s", quoted(body))
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
