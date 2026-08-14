// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/conformance"
	"github.com/Zaba505/cpybkc/irpb"
)

// TestThePositionSumIsRunOverTheDescriptor pins the arithmetic a mismatch is
// explained with.
//
// docs/ir/SPEC.md carries no offset on any node — position is stated once, as
// ordering and width — so an offset is a sum a consumer runs, and this is the
// engine's. The record here holds the three things that make the sum more than
// an addition: bytes that belong to no item, an item that repeats, and an item
// behind both of them.
func TestThePositionSumIsRunOverTheDescriptor(t *testing.T) {
	descriptor := &irpb.Descriptor{
		Version: irpb.IrVersion_IR_VERSION_1,
		Nodes: []*irpb.Node{
			{Id: 1, Kind: &irpb.Node_File{File: &irpb.File{
				Framing: &irpb.File_Unframed{Unframed: &irpb.Unframed{}}, StartStateId: 9,
			}}},
			{Id: 2, Kind: &irpb.Node_Record{Record: &irpb.Record{
				RootId: 3, Names: &irpb.Names{Original: "REC"},
			}}},
			{Id: 3, Kind: &irpb.Node_Group{Group: &irpb.Group{
				MemberIds: []uint64{4, 5, 6, 7}, Names: &irpb.Names{Original: "REC"},
			}}},
			{Id: 4, Kind: &irpb.Node_Field{Field: &irpb.Field{
				Width: 4, Names: &irpb.Names{Original: "HEAD"},
				Usage:    irpb.Usage_USAGE_DISPLAY,
				Encoding: &irpb.Encoding{Charset: irpb.Charset_CHARSET_CP037},
			}}},
			{Id: 5, Kind: &irpb.Node_Slack{Slack: &irpb.Slack{Width: 2}}},
			{Id: 6, Kind: &irpb.Node_Field{Field: &irpb.Field{
				Width: 5, Names: &irpb.Names{Original: "TABLE"},
				Usage:      irpb.Usage_USAGE_PACKED_DECIMAL,
				Encoding:   &irpb.Encoding{Charset: irpb.Charset_CHARSET_ASCII},
				Repetition: &irpb.Repetition{Count: &irpb.Repetition_Constant{Constant: 3}},
			}}},
			{Id: 7, Kind: &irpb.Node_Field{Field: &irpb.Field{
				Width: 1, Names: &irpb.Names{Original: "TAIL"},
				Usage:    irpb.Usage_USAGE_BINARY,
				Encoding: &irpb.Encoding{Charset: irpb.Charset_CHARSET_ASCII},
			}}},
		},
	}

	values, err := conformance.ParseValues([]byte(
		`{"records": [{"name": "REC", "value": {"HEAD": "abcd", "TABLE": ["1", "2", "3"], "TAIL": "7"}}]}`))
	if err != nil {
		t.Fatalf("the values the test is written against: %v", err)
	}

	found := positions(&conformance.Entry{Name: "made-up", Descriptor: descriptor, Values: values})

	tests := map[string]struct {
		offset int
		width  int
	}{
		"record 1 REC.HEAD":     {offset: 0, width: 4},
		"record 1 REC.TABLE[0]": {offset: 6, width: 5},
		"record 1 REC.TABLE[1]": {offset: 11, width: 5},
		"record 1 REC.TABLE[2]": {offset: 16, width: 5},
		"record 1 REC.TAIL":     {offset: 21, width: 1},
	}

	for path, want := range tests {
		got, ok := found[path]
		if !ok {
			t.Errorf("%s was not placed at all", path)

			continue
		}

		if got.Offset != want.offset || got.Width != want.width {
			t.Errorf("%s is %d bytes at offset %d, and the descriptor puts it %d bytes at offset %d",
				path, got.Width, got.Offset, want.width, want.offset)
		}

		if got.FileOffset != want.offset || !got.Located {
			t.Errorf("%s is at 0x%x of a file whose records abut, and it begins at %d",
				path, got.FileOffset, want.offset)
		}
	}

	if said := found["record 1 REC.TABLE[1]"].String(); !strings.Contains(said, "usage PACKED_DECIMAL") ||
		!strings.Contains(said, "charset ASCII") {
		t.Errorf("the item reads %q, and a disagreement about a number is nearly always about one of those axes", said)
	}
}

// TestARecordAfterAnotherIsPlacedBehindIt asserts that a second record's bytes
// are found where the first one's end, which is what makes the quoted bytes
// beneath a mismatch the right bytes.
//
// It is asserted against a real entry rather than an invented one, because what
// is being checked is that the sum agrees with a file somebody wrote down.
func TestARecordAfterAnotherIsPlacedBehindIt(t *testing.T) {
	entry := loadEntry(t, "zoned-ebcdic")

	if len(entry.Values.Records) < 2 {
		t.Fatalf("%s carries %d records, and this is about the second", entry.Name, len(entry.Values.Records))
	}

	found := positions(entry)

	starts := map[int]int{}

	for _, held := range found {
		if !held.Located {
			t.Fatalf("%s is not placed in a file whose records abut", held.Path)
		}

		// Every item of one record shares the record's own start, which is
		// what the file offset is: the record's beginning plus the offset the
		// position sum gives inside it.
		start := held.FileOffset - held.Offset

		if held, ok := starts[held.Record]; ok && held != start {
			t.Fatalf("record %d begins at two different bytes", held)
		}

		starts[held.Record] = start
	}

	if starts[1] != 0 {
		t.Errorf("the first record begins at byte %d of the file", starts[1])
	}

	if starts[2] <= starts[1] || starts[2] >= len(entry.Input) {
		t.Errorf("the second record begins at byte %d of a file of %d bytes", starts[2], len(entry.Input))
	}

	// The two records are the same record type, so the second begins exactly
	// one record's width behind the first.
	if starts[2]*2 != len(entry.Input) {
		t.Errorf("two records of one type do not divide a file of %d bytes at %d", len(entry.Input), starts[2])
	}
}

// TestTheBytesQuotedAreTheItemsOwn asserts that what a report quotes beneath a
// mismatch is the run of bytes the item was read from, and that an item nothing
// can be said about is passed over rather than guessed at.
func TestTheBytesQuotedAreTheItemsOwn(t *testing.T) {
	entry := loadEntry(t, "packed-ebcdic")

	held, ok := positions(entry)["record 1 PACKED-RECORD.P-SIGNED-POS"]
	if !ok {
		t.Fatal("the first item of the first record was not placed")
	}

	said, ok := held.bytes(entry.Input)
	if !ok {
		t.Fatal("the item's bytes were not quoted, and the file's records abut")
	}

	if !strings.Contains(said, "12 34 5c") {
		t.Errorf("the quote is %q, and the file begins 12 34 5c", said)
	}

	// An item the framing could not place quotes nothing at all. An offset into
	// input.bin that was wrong would be worse than none: it would send whoever
	// reads it to the wrong bytes with no way to tell.
	unplaced := held
	unplaced.Located = false

	if _, ok := unplaced.bytes(entry.Input); ok {
		t.Error("an item that could not be placed quoted bytes anyway")
	}

	past := held
	past.FileOffset = len(entry.Input)

	if _, ok := past.bytes(entry.Input); ok {
		t.Error("an item running past the end of the file quoted bytes anyway")
	}
}

// TestPositionsSurvivesADescriptorItCannotWalk asserts that the explanation is
// an addition to a report and never a condition of one.
//
// A report that refused to be written because the disagreement was structural
// would withhold the explanation exactly when it is most wanted.
func TestPositionsSurvivesADescriptorItCannotWalk(t *testing.T) {
	values, err := conformance.ParseValues([]byte(`{"records": [{"name": "NOBODY", "value": {"X": "1"}}]}`))
	if err != nil {
		t.Fatalf("the values the test is written against: %v", err)
	}

	tests := map[string]*conformance.Entry{
		"an entry with nothing in it":           {Name: "empty"},
		"a record the descriptor does not hold": {Name: "unknown", Descriptor: &irpb.Descriptor{}, Values: values},
	}

	for name, entry := range tests {
		t.Run(name, func(t *testing.T) {
			if found := positions(entry); len(found) != 0 {
				t.Errorf("%d items were placed against a descriptor that carries none", len(found))
			}
		})
	}
}

// loadEntry reads one entry of the corpus by name.
func loadEntry(t *testing.T, name string) *conformance.Entry {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found above %q", dir)
		}

		dir = parent
	}

	entry, err := conformance.LoadEntry(filepath.Join(conformance.CorpusPath(dir), name))
	if err != nil {
		t.Fatalf("the conformance corpus: %v", err)
	}

	return entry
}
