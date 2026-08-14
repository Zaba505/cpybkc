// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package goadapter_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/conformance/goadapter"
)

// answer is a response frame as a test reads one. It is written out here rather
// than shared with the adapter's own type on purpose: what these tests assert is
// what goes on the wire, and a test that unmarshalled into the same struct the
// adapter marshalled from would agree with it about a member neither of them
// spells the way the contract does.
type answer struct {
	ID           int              `json:"id"`
	OK           bool             `json:"ok"`
	Error        string           `json:"error"`
	Protocol     *int             `json:"protocol"`
	Name         string           `json:"name"`
	Kind         string           `json:"kind"`
	Capabilities *map[string]bool `json:"capabilities"`
	Entries      []entryAnswer    `json:"entries"`
	Entry        string           `json:"entry"`
	Decoded      json.RawMessage  `json:"decoded"`
	Written      json.RawMessage  `json:"written"`
}

// entryAnswer is what a generate response says about one entry.
type entryAnswer struct {
	Entry string `json:"entry"`
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

// converse holds one conversation against the adapter and reads back every frame
// it wrote.
//
// The generator is a path that is never run: every case below is answered before
// a generator would be invoked, which is what makes them cheap enough to be
// ordinary unit tests rather than a build per assertion.
func converse(t *testing.T, frames ...string) ([]answer, error) {
	t.Helper()

	return hold(t, &goadapter.Adapter{
		Root:      t.TempDir(),
		Name:      "go",
		Generator: "a generator this conversation never reaches",
		Version:   "0.0.0-test",
	}, frames...)
}

// hold is one conversation against the adapter it is given.
func hold(t *testing.T, adapter *goadapter.Adapter, frames ...string) ([]answer, error) {
	t.Helper()

	var out strings.Builder

	err := adapter.Serve(t.Context(), strings.NewReader(strings.Join(frames, "")), &out)

	var read []answer

	for line := range strings.Lines(out.String()) {
		var frame answer
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("the adapter wrote a line that is not a frame: %q: %v", line, err)
		}

		read = append(read, frame)
	}

	return read, err
}

// frame is one request, as the engine writes one: one JSON object on one line.
func frame(text string) string { return text + "\n" }

// TestTheHandshakeDeclaresACodecThatWrites is the first frame of every
// conversation, and the one that decides what the engine may ask afterwards.
func TestTheHandshakeDeclaresACodecThatWrites(t *testing.T) {
	read, err := converse(t, frame(`{"id":1,"op":"hello","protocol":1}`))
	if err != nil {
		t.Fatalf("the adapter broke: %v", err)
	}

	if len(read) != 1 {
		t.Fatalf("one request was made and the adapter wrote %d frames", len(read))
	}

	got := read[0]

	switch {
	case got.ID != 1:
		t.Errorf("the adapter answered id %d and it was asked id 1", got.ID)
	case !got.OK:
		t.Errorf("the adapter refused the handshake: %s", got.Error)
	case got.Protocol == nil || *got.Protocol != goadapter.Protocol:
		t.Errorf("the adapter stated protocol %v and it speaks %d", got.Protocol, goadapter.Protocol)
	case got.Kind != "codec":
		t.Errorf("the adapter declared kind %q, and it drives a generator that emits a reader and a writer", got.Kind)
	case got.Name == "":
		t.Errorf("the adapter declared no name, and a report has nothing to call it")
	case got.Capabilities == nil:
		t.Fatalf("the adapter declared no capabilities, and the member is required even when it is empty")
	case !(*got.Capabilities)["write"]:
		t.Errorf("the adapter declared no writer, and cpybkc-gen-go emits one")
	}
}

// TestAHandshakeAtAnotherVersionIsRefusedAndStatesThisOne is what lets a report
// say which two versions failed to meet instead of only that the handshake did.
func TestAHandshakeAtAnotherVersionIsRefusedAndStatesThisOne(t *testing.T) {
	read, err := converse(t, frame(`{"id":1,"op":"hello","protocol":2}`))
	if err != nil {
		t.Fatalf("the adapter broke: %v", err)
	}

	got := read[0]

	switch {
	case got.OK:
		t.Fatalf("the adapter agreed a handshake at a version it does not speak")
	case got.Protocol == nil || *got.Protocol != goadapter.Protocol:
		t.Errorf("the refusal stated protocol %v, and an adapter states its own anyway", got.Protocol)
	case got.Error == "":
		t.Errorf("the refusal says nothing about why")
	case got.Kind != "" || got.Capabilities != nil:
		t.Errorf("the refusal declared a kind or a capability set, and an adapter that could not agree "+
			"the version has no basis for either: %+v", got)
	}
}

// TestAnOperationThisAdapterDoesNotKnowIsRefused is the rule an unknown *member*
// is the opposite of: a receiver can safely do nothing about an extension, and an
// adapter that reported success for work it did not do is one whose whole report
// is worthless.
func TestAnOperationThisAdapterDoesNotKnowIsRefused(t *testing.T) {
	read, err := converse(t,
		frame(`{"id":1,"op":"hello","protocol":1}`),
		frame(`{"id":2,"op":"explain","entry":"packed-ebcdic"}`),
	)
	if err != nil {
		t.Fatalf("the adapter broke: %v", err)
	}

	if got := read[1]; got.OK || got.Error == "" {
		t.Errorf("the adapter answered an operation it does not know: %+v", got)
	}
}

// TestAnUnknownMemberIsIgnored is the other half of that rule: a frame is written
// by a program against a version announced at the handshake, so an unrecognised
// member is one a later version of the contract added, and a receiver that
// refused it would make every future addition a breaking change.
func TestAnUnknownMemberIsIgnored(t *testing.T) {
	read, err := converse(t, frame(`{"id":1,"op":"hello","protocol":1,"nickname":"a member from a later version"}`))
	if err != nil {
		t.Fatalf("the adapter broke: %v", err)
	}

	if got := read[0]; !got.OK {
		t.Errorf("the adapter refused a frame for carrying a member it does not know: %s", got.Error)
	}
}

// TestRebuildIsRefusedBecauseItWasNotDeclared is one of the two requests an
// adapter MUST refuse: one that asks for a capability it did not declare.
func TestRebuildIsRefusedBecauseItWasNotDeclared(t *testing.T) {
	read, err := converse(t,
		frame(`{"id":1,"op":"hello","protocol":1}`),
		frame(`{"id":2,"op":"rebuild","entry":"packed-ebcdic","descriptor":""}`),
	)
	if err != nil {
		t.Fatalf("the adapter broke: %v", err)
	}

	if got := read[1]; got.OK {
		t.Errorf("the adapter served rebuild, which it declared no capability for")
	}
}

// TestARoundtripWithNothingHeldIsRefused is the other one, and it is the
// precondition an adapter can check against itself: what it is holding, rather
// than what was most recently decoded.
func TestARoundtripWithNothingHeldIsRefused(t *testing.T) {
	read, err := converse(t,
		frame(`{"id":1,"op":"hello","protocol":1}`),
		frame(`{"id":2,"op":"roundtrip","entry":"packed-ebcdic"}`),
	)
	if err != nil {
		t.Fatalf("the adapter broke: %v", err)
	}

	if got := read[1]; got.OK {
		t.Errorf("the adapter answered a roundtrip while holding no records to write back")
	}
}

// TestADecodeOfAnEntryThereIsNoCodeForIsRefused is a fault against that entry and
// never an answer about it: nothing has been learned about the generator.
func TestADecodeOfAnEntryThereIsNoCodeForIsRefused(t *testing.T) {
	read, err := converse(t,
		frame(`{"id":1,"op":"hello","protocol":1}`),
		frame(`{"id":2,"op":"decode","entry":"packed-ebcdic","input":""}`),
	)
	if err != nil {
		t.Fatalf("the adapter broke: %v", err)
	}

	if got := read[1]; got.OK {
		t.Errorf("the adapter answered a decode for an entry generate never produced code for")
	}
}

// TestByeIsAnsweredAndEndsTheConversation, after which the adapter exits zero —
// which here is Serve returning no error and writing nothing further.
func TestByeIsAnsweredAndEndsTheConversation(t *testing.T) {
	read, err := converse(t,
		frame(`{"id":1,"op":"hello","protocol":1}`),
		frame(`{"id":2,"op":"bye"}`),
		frame(`{"id":3,"op":"hello","protocol":1}`),
	)
	if err != nil {
		t.Fatalf("the adapter broke: %v", err)
	}

	if len(read) != 2 {
		t.Fatalf("the adapter wrote %d frames, and it was asked two things before it said goodbye", len(read))
	}

	if got := read[1]; !got.OK || got.ID != 2 {
		t.Errorf("the adapter refused bye: %+v", got)
	}
}

// TestEndOfInputIsAByeAlreadyAnswered, because a run that ended in a fault has no
// useful frame left to send, and an adapter that waited for a polite goodbye that
// was never coming would hang at the end of every such run.
func TestEndOfInputIsAByeAlreadyAnswered(t *testing.T) {
	read, err := converse(t, frame(`{"id":1,"op":"hello","protocol":1}`))
	if err != nil {
		t.Fatalf("the adapter broke: %v", err)
	}

	if len(read) != 1 {
		t.Errorf("the adapter wrote %d frames at end of input, and it was asked one thing", len(read))
	}
}

// TestAFrameTheFramingDoesNotAdmitStopsTheConversation is the receiver's half of
// the framing rules. Each of these is a stream whose framing is in doubt, and a
// stream whose framing is in doubt cannot be resynchronised by anything the
// receiver can see — so the adapter stops rather than skipping the line and
// reading on.
func TestAFrameTheFramingDoesNotAdmitStopsTheConversation(t *testing.T) {
	for name, line := range map[string]string{
		"a blank line":          "\n",
		"a carriage return":     "{\"id\":2,\"op\":\"bye\"}\r\n",
		"not JSON at all":       "a library greeting the world\n",
		"a frame with no id":    "{\"op\":\"bye\"}\n",
		"a frame with no op":    "{\"id\":2}\n",
		"an unterminated frame": `{"id":2,"op":"bye"}`,
	} {
		t.Run(name, func(t *testing.T) {
			read, err := converse(t, frame(`{"id":1,"op":"hello","protocol":1}`), line)
			if err == nil {
				t.Fatalf("the adapter read on past %s", name)
			}

			if len(read) != 1 {
				t.Errorf("the adapter answered %d frames, and only the handshake was well formed", len(read))
			}
		})
	}
}

// TestARunIsRefusedBeforeAnythingIsCreated holds a conversation to what it needs
// before a scratch tree is made, so that a missing field is reported as itself
// rather than as a build that failed for no visible reason.
func TestARunIsRefusedBeforeAnythingIsCreated(t *testing.T) {
	for name, adapter := range map[string]*goadapter.Adapter{
		"no root":      {Generator: "a generator"},
		"no generator": {Root: t.TempDir()},
	} {
		t.Run(name, func(t *testing.T) {
			var out strings.Builder

			if err := adapter.Serve(t.Context(), strings.NewReader(""), &out); err == nil {
				t.Fatalf("a conversation with %s was served", name)
			}

			if out.String() != "" {
				t.Errorf("the adapter wrote %q before refusing to run at all", out.String())
			}
		})
	}
}
