// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// This file is in package emit rather than emit_test, which is the exception in
// this repository and is here for one reason: the property it asserts is the
// whole of what makes [MarshalJSON]'s output worth committing, and it cannot be
// observed from outside.
//
// protojson chooses its extra whitespace from a hash of the running binary, so
// it is fixed for the life of a test process and different in the next build. A
// test that rendered twice and compared would therefore pass in every build,
// including the ones where the output had changed. The only way to fail on that
// is to hand the normalizer both spacings by hand, which means calling it
// directly.
package emit

import (
	"bytes"
	"testing"
)

// TestIndentJSONIgnoresTheWhitespaceItIsHanded is the assertion the paragraph
// above exists for.
//
// The two inputs are the two shapes protojson emits — a space after a comma,
// and a second space after a "key": — written out rather than produced, so that
// a regression fails every time instead of in half of all builds.
func TestIndentJSONIgnoresTheWhitespaceItIsHanded(t *testing.T) {
	compact := []byte(`{"version":"IR_VERSION_1","nodes":[{"id":"1","slack":{"width":2}}]}`)
	spaced := []byte(`{"version":  "IR_VERSION_1", "nodes": [{"id":  "1", "slack": {"width":  2}}]}`)

	fromCompact, err := indentJSON(compact)
	if err != nil {
		t.Fatalf("normalize the compact form: %v", err)
	}

	fromSpaced, err := indentJSON(spaced)
	if err != nil {
		t.Fatalf("normalize the spaced form: %v", err)
	}

	if !bytes.Equal(fromCompact, fromSpaced) {
		t.Errorf("the output depends on the whitespace it was handed\n from compact:\n%s\n from spaced:\n%s", fromCompact, fromSpaced)
	}
}

// TestIndentJSONLeavesContentAlone is the other half. Whitespace inside a
// string is a COBOL name or a value, not spacing between tokens, and a
// normalizer that reformatted it would be changing the descriptor rather than
// the rendering of one.
func TestIndentJSONLeavesContentAlone(t *testing.T) {
	got, err := indentJSON([]byte(`{"names":{"original":"DTL  AMOUNT, LOW"}}`))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}

	if !bytes.Contains(got, []byte(`"DTL  AMOUNT, LOW"`)) {
		t.Errorf("the string's own spacing was rewritten:\n%s", got)
	}
}

// TestIndentJSONReportsWhatItCannotParse keeps a failure a failure. Every input
// this package hands it came from protojson and is valid, so reaching the error
// means something upstream is broken and returning the bytes unchanged would
// hide it.
func TestIndentJSONReportsWhatItCannotParse(t *testing.T) {
	if _, err := indentJSON([]byte(`{"unterminated":`)); err == nil {
		t.Error("normalizing invalid JSON succeeded")
	}
}
