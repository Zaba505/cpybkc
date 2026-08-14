// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package descriptive_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/conformance/descriptive"
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
	Version      string           `json:"version"`
	Kind         string           `json:"kind"`
	Capabilities *map[string]bool `json:"capabilities"`
}

// converse holds one conversation against a named descriptive adapter and reads
// back every frame it wrote.
func converse(t *testing.T, frames ...string) ([]answer, string, error) {
	t.Helper()

	return hold(t, &descriptive.Adapter{Name: "graph", Version: "0.0.0-test"}, frames...)
}

// hold is one conversation against the adapter it is given, and hands back the
// frames it wrote, the raw stream it wrote them on, and how it ended.
//
// The raw stream is kept because two of the things the contract requires are
// properties of the bytes rather than of the values they decode to: that a frame
// is one line, and that an empty capabilities object is written as `{}` rather
// than left out or written as null.
func hold(t *testing.T, adapter *descriptive.Adapter, frames ...string) ([]answer, string, error) {
	t.Helper()

	var out strings.Builder

	err := adapter.Serve(strings.NewReader(strings.Join(frames, "")), &out)

	var read []answer

	for line := range strings.Lines(out.String()) {
		var frame answer
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("the adapter wrote a line that is not a frame: %q: %v", line, err)
		}

		read = append(read, frame)
	}

	return read, out.String(), err
}

// frame is one request, as the engine writes one: one JSON object on one line.
func frame(text string) string { return text + "\n" }

// TestTheHandshakeDeclaresADescriptiveGenerator is the whole of this story: the
// kind is declared in the first frame, before the adapter has been asked
// anything, which is what lets an engine report a generator it cannot test as
// not applicable instead of discovering it from a failure.
func TestTheHandshakeDeclaresADescriptiveGenerator(t *testing.T) {
	read, raw, err := converse(t, frame(`{"id":1,"op":"hello","protocol":1}`))
	if err != nil {
		t.Fatalf("the adapter broke: %v", err)
	}

	if len(read) != 1 {
		t.Fatalf("one request was made and the adapter wrote %d frames", len(read))
	}

	got := read[0]

	// Every member is asserted rather than the first one that disagrees, because
	// these are independent claims about one frame: an adapter that answered the
	// wrong id and declared kind codec has two things wrong with it, and a run
	// that reported only the id would send somebody to fix the smaller one.
	if got.ID != 1 {
		t.Errorf("the adapter answered id %d and it was asked id 1", got.ID)
	}

	if !got.OK {
		t.Errorf("the adapter refused the handshake: %s", got.Error)
	}

	if got.Protocol == nil || *got.Protocol != descriptive.Protocol {
		t.Errorf("the adapter stated protocol %v and it speaks %d", got.Protocol, descriptive.Protocol)
	}

	if got.Kind != "descriptive" {
		t.Errorf("the adapter declared kind %q, and it drives a generator that never opens a data file", got.Kind)
	}

	if got.Name != "cpybkc-gen-graph adapter" {
		t.Errorf("the adapter calls itself %q, and a report should be able to say which generator declined", got.Name)
	}

	if got.Version != "0.0.0-test" {
		t.Errorf("the adapter states version %q, and it was given 0.0.0-test", got.Version)
	}

	if got.Capabilities == nil {
		t.Fatalf("the adapter declared no capabilities, and the member is required even when it is empty")
	}

	if len(*got.Capabilities) != 0 {
		t.Errorf("the adapter declared the capabilities %v, and it serves no optional operation", *got.Capabilities)
	}

	// The member is required of a successful handshake even when it is empty, so
	// what the engine reads has to be an empty object and not a null and not an
	// absence. Asserted against the bytes because that distinction is one
	// encoding/json will happily erase on the way back in.
	if !strings.Contains(raw, `"capabilities":{}`) {
		t.Errorf("the handshake frame is %s, and an adapter with no capabilities says so with an empty object", raw)
	}
}

// TestAHandshakeWithoutANameIsStillReadable is the adapter that was given no
// name: a report has to have something to call it, and the engine refuses a
// handshake that declared nothing.
func TestAHandshakeWithoutANameIsStillReadable(t *testing.T) {
	read, _, err := hold(t, &descriptive.Adapter{}, frame(`{"id":1,"op":"hello","protocol":1}`))
	if err != nil {
		t.Fatalf("the adapter broke: %v", err)
	}

	if len(read) != 1 {
		t.Fatalf("one request was made and the adapter wrote %d frames", len(read))
	}

	if got := read[0]; got.Name == "" {
		t.Errorf("the adapter declared no name, and a report has nothing to call it")
	}
}

// TestARefusedHandshakeStatesItsOwnProtocol is the version that did not meet.
//
// A refused handshake carries the four members the contract allows it and
// nothing else: an adapter that could not agree the version has no basis for
// declaring a kind or a capability set, and a frame that declared them anyway
// would be one a report could quote.
func TestARefusedHandshakeStatesItsOwnProtocol(t *testing.T) {
	for _, asked := range []string{`{"id":1,"op":"hello","protocol":2}`, `{"id":1,"op":"hello"}`} {
		t.Run(asked, func(t *testing.T) {
			read, _, err := converse(t, frame(asked))
			if err != nil {
				t.Fatalf("the adapter broke: %v", err)
			}

			if len(read) != 1 {
				t.Fatalf("one request was made and the adapter wrote %d frames", len(read))
			}

			got := read[0]

			if got.OK {
				t.Errorf("the adapter agreed a handshake for a protocol it does not speak")
			}

			if got.Error == "" {
				t.Errorf("the adapter refused the handshake and said nothing about why")
			}

			if got.Protocol == nil || *got.Protocol != descriptive.Protocol {
				t.Errorf("the adapter stated protocol %v, and a report should say which two versions failed to meet",
					got.Protocol)
			}

			if got.Kind != "" {
				t.Errorf("the refused handshake declared kind %q, and it agreed no version to declare one under", got.Kind)
			}

			if got.Capabilities != nil {
				t.Errorf("the refused handshake declared capabilities, and it agreed no version to declare them under")
			}
		})
	}
}

// TestEveryCodecOperationIsRefused is what happens when an engine asks a
// question this generator cannot be asked.
//
// A correct engine never sends one — it MUST NOT, once the handshake said
// descriptive — so this is about the two shapes that must not happen when one
// does. Attempting the work would mean answering about a file nothing read, and
// exiting would report a broken adapter where nothing is broken. The refusal
// costs the entry and says why.
func TestEveryCodecOperationIsRefused(t *testing.T) {
	asked := []string{
		`{"id":2,"op":"generate","entries":[{"entry":"packed-ebcdic","descriptor":"AAAA"}]}`,
		`{"id":3,"op":"decode","entry":"packed-ebcdic","input":"AAAA"}`,
		`{"id":4,"op":"roundtrip","entry":"packed-ebcdic"}`,
		`{"id":5,"op":"rebuild","entry":"packed-ebcdic","descriptor":"AAAA"}`,
		`{"id":6,"op":"transmogrify"}`,
	}

	var conversation []string
	for _, one := range asked {
		conversation = append(conversation, frame(one))
	}

	read, _, err := converse(t, conversation...)
	if err != nil {
		t.Fatalf("the adapter broke, and a request it had no business being sent is not a broken adapter: %v", err)
	}

	if len(read) != len(asked) {
		t.Fatalf("%d requests were made and the adapter wrote %d frames", len(asked), len(read))
	}

	for i, got := range read {
		if got.ID != i+2 {
			t.Errorf("the adapter answered id %d and it was asked id %d", got.ID, i+2)
		}

		if got.OK {
			t.Errorf("the adapter served %s, and it drives a generator that never opens a data file", asked[i])
		}

		if got.Error == "" {
			t.Errorf("the adapter refused %s and said nothing about why", asked[i])
		}
	}
}

// TestByeEndsTheConversation is the last frame, and the one exit an adapter may
// make zero from.
func TestByeEndsTheConversation(t *testing.T) {
	read, _, err := converse(t,
		frame(`{"id":1,"op":"hello","protocol":1}`),
		frame(`{"id":2,"op":"bye"}`),
		frame(`{"id":3,"op":"hello","protocol":1}`))
	if err != nil {
		t.Fatalf("the adapter broke: %v", err)
	}

	if len(read) != 2 {
		t.Fatalf("the adapter wrote %d frames, and it was asked two questions before it was told goodbye", len(read))
	}

	if got := read[1]; got.ID != 2 || !got.OK {
		t.Errorf("the adapter answered bye with %+v: bye takes no argument it could object to", got)
	}
}

// TestEndOfInputIsAByeAlreadyAnswered is the run that ended in a fault: there is
// no useful frame left to send, and an adapter that waited for a polite goodbye
// that was never coming would hang at the end of every such run.
func TestEndOfInputIsAByeAlreadyAnswered(t *testing.T) {
	read, raw, err := converse(t)
	if err != nil {
		t.Fatalf("the adapter broke on an input that simply ended: %v", err)
	}

	if len(read) != 0 {
		t.Errorf("the adapter wrote %q after end of input, and it exits zero without writing anything further", raw)
	}
}

// TestAFrameOfAnyLengthIsRead is the receiver's MUST that reading into a fixed
// buffer breaks.
//
// A bufio.Scanner stops at a line longer than the buffer it reads into, which is
// why this adapter reads with ReadString instead, and a stated MUST resting on a
// comment is one nothing would notice the loss of. The length is made of an
// unknown member because that is what a later version of the contract adds: it
// is ignored rather than refused, so what comes back is an ordinary handshake.
func TestAFrameOfAnyLengthIsRead(t *testing.T) {
	long := fmt.Sprintf(`{"id":1,"op":"hello","protocol":1,"a-member-a-later-version-added":%q}`,
		strings.Repeat("x", 256<<10))

	read, _, err := converse(t, frame(long))
	if err != nil {
		t.Fatalf("the adapter broke on a frame of %d bytes: %v", len(long), err)
	}

	if len(read) != 1 {
		t.Fatalf("one request was made and the adapter wrote %d frames", len(read))
	}

	if got := read[0]; !got.OK || got.Kind != "descriptive" {
		t.Errorf("the adapter answered a long frame with %+v", got)
	}
}

// TestAStreamThatIsNotFramedStopsTheConversation is every line the framing does
// not admit.
//
// Each one is a broken engine rather than a request, and the adapter stops
// rather than answering: a stream whose framing is in doubt cannot be
// resynchronised by anything the receiver can see, so an adapter that skipped a
// bad line and read on would be answering questions it cannot know it was asked.
func TestAStreamThatIsNotFramedStopsTheConversation(t *testing.T) {
	tests := map[string]struct {
		line string
		says string
	}{
		"a blank line":          {line: "\n", says: "blank line"},
		"a carriage return":     {line: "{\"id\":1,\"op\":\"hello\"}\r\n", says: "carriage return"},
		"not one object":        {line: frame(`not json at all`), says: "one JSON object"},
		"no id":                 {line: frame(`{"op":"hello"}`), says: "no id"},
		"no op":                 {line: frame(`{"id":1}`), says: "no op"},
		"an unterminated frame": {line: `{"id":1,"op":"hello","protocol":1}`, says: "in the middle of a frame"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			read, _, err := converse(t, test.line)
			if err == nil {
				t.Fatalf("the adapter read %q as a frame and answered %+v", test.line, read)
			}

			if !strings.Contains(err.Error(), test.says) {
				t.Errorf("the adapter broke with %q, and it does not say %q", err, test.says)
			}

			if len(read) != 0 {
				t.Errorf("the adapter answered %+v a line that is not a frame", read)
			}
		})
	}
}
