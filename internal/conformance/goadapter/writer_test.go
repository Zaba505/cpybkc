// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package goadapter_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/conformance"
	"github.com/Zaba505/cpybkc/internal/conformance/engine"
)

// TestAWriterRefusesARecordItsOwnReaderWouldRouteElsewhere is docs/ir/SPEC.md's
// "A writer walks the same automaton", at the state docs/ir/SPEC.md's "A batch
// boundary is told by the order" admits a pair at: a writer that has narrowed to
// this record's own transition MUST evaluate the predicates of the transitions
// ordered before it against the bytes it is about to emit, and MUST report a
// record any of them matches rather than emitting it (#333).
//
// The fixture is a batched extract whose header is keyed on the three bytes a
// detail's packed amount occupies, so the two runs share no byte, the copybooks
// decline a domain for a packed item, and the pair rests on the order with the
// header's test first. What turns that into a record the two directions disagree
// about is that a packed item is read leniently and written canonically: the
// detail in the file carries the sign nibble A, which is positive, so the header's
// test fails against those bytes and the reader routes the record to the detail —
// and a writer handed that record back emits the canonical nibble C, which is the
// header's literal. Without the check the writer would produce a file its own
// reader splits in the wrong place, which is the whole of what #333 pairs with the
// permission.
//
// # Why the fixture is not a corpus entry
//
// The corpus states one set of values and holds both directions to it
// (docs/conformance/SPEC.md, "The answer document"), and a values document's
// failure is a read that stopped — a writer refusal beside a complete read has
// nowhere to be written down, and `written` beside a `decoded` failure is refused
// outright. So an entry expecting this outcome could only be spelled as an entry
// that fails, which is why the fixture lives here and is loaded by name rather
// than shipped in testdata/conformance. What it asserts is the same thing an
// entry would: the run reports a disagreement, and the disagreement is the
// writing direction's.
func TestAWriterRefusesARecordItsOwnReaderWouldRouteElsewhere(t *testing.T) {
	root := repoRoot(t)

	entry, err := conformance.LoadEntry(filepath.Join(root,
		"internal", "conformance", "goadapter", "testdata", "writer-refusal"))
	if err != nil {
		t.Fatalf("the writer-refusal fixture: %v", err)
	}

	report := ask(t, root, []*conformance.Entry{entry})

	if len(report.Results) != 1 {
		t.Fatalf("one entry was asked about and the report carries %d results:\n%s", len(report.Results), report)
	}

	result := report.Results[0]

	if result.Outcome != engine.Mismatched {
		t.Fatalf("the run reported %s, and a writer that emitted this record disagreed with the fixture:\n%s",
			result.Outcome, report)
	}

	said := result.Err.Error()

	// The reading direction is the one the fixture is right about: the record
	// carries the lenient sign nibble, the header's test fails against it, and
	// the reader routes it to the detail. A disagreement there would mean the
	// fixture is wrong about the file rather than that the writer refused.
	if strings.Contains(said, "reading the entry's bytes") {
		t.Errorf("the reading direction disagreed with the fixture, and it is the writing direction that refuses: %s", said)
	}

	if !strings.Contains(said, "reading back the file written from those records") {
		t.Errorf("the disagreement is not the writing direction's: %s", said)
	}

	// The second record is the detail, and it is the one whose canonical bytes
	// the header's predicate matches. A refusal naming the first would be a
	// writer refusing the record the file opens with, which no rule asks for.
	if !strings.Contains(said, "writing record 2") {
		t.Errorf("the refusal does not name the detail the reader routed here: %s", said)
	}
}
