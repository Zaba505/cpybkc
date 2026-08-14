// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestAFrameIsOneLineOfJSON walks what the framing does not admit.
//
// Every row is a hazard docs/adapter/SPEC.md names rather than an invented
// malformation: a receiver that skipped a blank line would report a corrupted
// stream one frame later, at whichever frame first failed to parse; one that
// trimmed a carriage return would let a text-mode file handle silently change
// what was sent; and one that read a frame with no ok would call an answer out
// of a line that says nothing about whether the request was served.
func TestAFrameIsOneLineOfJSON(t *testing.T) {
	tests := map[string]struct {
		line string
		says string
	}{
		"a blank line": {
			line: "\n",
			says: "blank line",
		},
		"a carriage return before the terminator": {
			line: "{\"id\":1,\"ok\":true}\r\n",
			says: "carriage return",
		},
		"a line the stream cut off": {
			line: `{"id":1,"ok":true}`,
			says: "ended in the middle of a frame",
		},
		"a greeting a library printed": {
			line: "loading tables, please wait\n",
			says: "one JSON object on one line",
		},
		"two documents on one line": {
			line: "{\"id\":1,\"ok\":true} {\"id\":2,\"ok\":true}\n",
			says: "one JSON object on one line",
		},
		"a frame with no id": {
			line: "{\"ok\":true}\n",
			says: "carries no id",
		},
		"a frame with no ok": {
			line: "{\"id\":1}\n",
			says: "carries no ok",
		},
		"a refusal that says nothing": {
			line: "{\"id\":1,\"ok\":false}\n",
			says: "says nothing about why",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := unmarshal(test.line)
			if err == nil {
				t.Fatal("the line was read as a frame, and the framing does not admit it")
			}

			if !strings.Contains(err.Error(), test.says) {
				t.Errorf("the fault is %q, and it does not say %q", err, test.says)
			}
		})
	}
}

// TestAnUnknownMemberIsIgnored is the opposite of the rule the corpus format
// applies to an entry, and the difference is who writes the document.
//
// An entry is written by a person, where an unrecognised member is a typo and
// refusing it is a kindness. A frame is written by a program against a version
// it announced at the handshake, where an unrecognised member is one a later
// version of the contract added — and a receiver that refused it would make
// every future addition a breaking change, which is how a protocol acquires a
// version number it never gets to increment.
func TestAnUnknownMemberIsIgnored(t *testing.T) {
	got, err := unmarshal("{\"id\":1,\"ok\":true,\"protocol\":1,\"name\":\"x\",\"kind\":\"codec\"," +
		"\"capabilities\":{},\"telemetry\":{\"entries\":7}}\n")
	if err != nil {
		t.Fatalf("a frame carrying a member this version does not know was refused: %v", err)
	}

	if !got.served() || got.Name != "x" {
		t.Errorf("the frame was read as %+v", got)
	}
}

// TestAnEmptyInputIsAskedAboutAsAnEmptyInput asserts that an entry whose file
// holds no bytes reaches the adapter as one.
//
// An empty file is a file — a layout whose sequencing expression accepts
// nothing is exactly what one entry ought to be about — and a request that
// dropped the member would be asking a different question, which the adapter
// would have to guess the answer to.
func TestAnEmptyInputIsAskedAboutAsAnEmptyInput(t *testing.T) {
	empty := []byte{}

	frame, err := marshal(&request{ID: 3, Op: opDecode, Entry: "empty", Input: &empty})
	if err != nil {
		t.Fatalf("the request could not be written: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(frame, &got); err != nil {
		t.Fatalf("the request is not a JSON object: %v", err)
	}

	if input, ok := got["input"]; !ok || input != "" {
		t.Errorf("the request carries input %v, and an entry of no bytes is asked about with an empty one", input)
	}

	if _, ok := got["entries"]; ok {
		t.Error("a decode request carries generate's entries")
	}
}

// TestAFrameIsOneLine asserts that nothing this engine writes carries a line
// feed of its own, which is the whole of what makes a line feed a delimiter.
func TestAFrameIsOneLine(t *testing.T) {
	descriptor := []byte{0x00, 0x0a, 0xff}

	frame, err := marshal(&request{
		ID: 2, Op: opGenerate,
		Entries: []requestEntry{{Entry: "an entry\nwith a line feed in its name", Descriptor: descriptor}},
	})
	if err != nil {
		t.Fatalf("the request could not be written: %v", err)
	}

	if got := strings.Count(string(frame), "\n"); got != 1 {
		t.Errorf("the frame carries %d line feeds, and a frame is one line", got)
	}

	if !strings.HasSuffix(string(frame), "\n") {
		t.Error("the frame is not terminated by a line feed")
	}
}

// TestQuotedShortensWhatItQuotes asserts that a report quoting a line which is
// not a frame stays something a person can read.
//
// A line that is not a frame is usually a diagnostic and occasionally a
// megabyte of a runtime's stack trace, and the quote is the thing somebody has
// to read to find out what the adapter did.
func TestQuotedShortensWhatItQuotes(t *testing.T) {
	said := quoted(strings.Repeat("x", quotedLimit*2))

	if len(said) > quotedLimit+64 {
		t.Errorf("a quoted line is %d bytes long", len(said))
	}

	if !strings.Contains(said, "more bytes") {
		t.Errorf("the quote is %q, and it does not say it is short of the whole line", said)
	}
}
