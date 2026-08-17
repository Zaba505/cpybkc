// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The assertions over a segmented dataset, whose largest segment is smaller
// than its one record type.
//
// Two things turn on that. A reader reassembles a record's segments into one
// contiguous run before any predicate runs, so every rule in docs/ir/SPEC.md
// reads a record the file may have split. And a writer lays a record into as
// few segments as the largest allows whatever the input did — which is why a
// file of this framing is not byte-identical and a record is.
package chunks

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/Zaba505/cobol-go/codec"
)

// chunkBytes is one CHUNK-RECORD, with no framing around it.
func chunkBytes(t *testing.T, id, body string) []byte {
	t.Helper()

	var b bytes.Buffer

	w, err := codec.NewWriter(&b, Encoding())
	if err != nil {
		t.Fatalf("codec.NewWriter: %v", err)
	}

	if err := w.WriteAlphanumeric(id, 4); err != nil {
		t.Fatalf("CHUNK-ID: %v", err)
	}

	if err := w.WriteAlphanumeric(body, 20); err != nil {
		t.Fatalf("CHUNK-BODY: %v", err)
	}

	return b.Bytes()
}

// segmented is a record split into segments of the sizes given, each with the
// segment descriptor word DFSMS defines in front of it.
func segmented(raw []byte, sizes ...int) []byte {
	var out bytes.Buffer

	at := 0

	for i, size := range sizes {
		code := byte(0x03)

		switch {
		case i == 0 && len(sizes) == 1:
			code = 0x00
		case i == 0:
			code = 0x01
		case i == len(sizes)-1:
			code = 0x02
		}

		stated := size + 4

		out.Write([]byte{byte(stated >> 8), byte(stated), code, 0})
		out.Write(raw[at : at+size])

		at += size
	}

	return out.Bytes()
}

// read is every record of in, through the generated reader.
func read(t *testing.T, in []byte) []Record {
	t.Helper()

	r, err := NewReader(bytes.NewReader(in), Encoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	var out []Record

	for {
		rec, err := r.Next()
		if errors.Is(err, io.EOF) {
			return out
		}

		if err != nil {
			t.Fatalf("Next: %v", err)
		}

		out = append(out, rec)
	}
}

// TestSegmentsAreReassembledHoweverTheFileSplitThem is the reader's half: a
// record split three ways and a record split six ways are the same record.
func TestSegmentsAreReassembledHoweverTheFileSplitThem(t *testing.T) {
	t.Parallel()

	raw := chunkBytes(t, "ID01", "A BODY OF TWENTY CHS")

	for name, in := range map[string][]byte{
		"as few segments as the largest allows": segmented(raw, 8, 8, 8),
		"more segments than it needed":          segmented(raw, 4, 4, 4, 4, 4, 4),
		"segments of uneven size":               segmented(raw, 1, 20, 3),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			records := read(t, in)
			if len(records) != 1 {
				t.Fatalf("the file holds one record and the reader produced %d", len(records))
			}

			chunk, ok := records[0].(*ChunkRecord)
			if !ok {
				t.Fatalf("the record is a %T", records[0])
			}

			if chunk.ChunkId != "ID01" || chunk.ChunkBody != "A BODY OF TWENTY CHS" {
				t.Errorf("the record came back as %q and %q", chunk.ChunkId, chunk.ChunkBody)
			}
		})
	}
}

// TestAWriterLaysARecordIntoAsFewSegmentsAsTheLargestAllows is the writer's
// half, and the reason a file of this framing is not byte-identical.
//
// The largest segment is the one framing fact a writer needs and cannot
// compute, which is why the file node carries it; a reader has no use for it,
// since every segment states its own length.
func TestAWriterLaysARecordIntoAsFewSegmentsAsTheLargestAllows(t *testing.T) {
	t.Parallel()

	raw := chunkBytes(t, "ID01", "A BODY OF TWENTY CHS")

	// Read from a file that split it six ways and written back into the fewest
	// the largest segment allows: 24 bytes of data at 8 apiece is three.
	records := read(t, segmented(raw, 4, 4, 4, 4, 4, 4))

	var b bytes.Buffer

	w, err := NewWriter(&b, Encoding())
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	for _, rec := range records {
		if err := w.Write(rec); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if want := segmented(raw, 8, 8, 8); !bytes.Equal(b.Bytes(), want) {
		t.Errorf("the record was laid into\n got: % x\nwant: % x", b.Bytes(), want)
	}

	// The record is byte-identical even though the file is not, which is the
	// claim docs/ir/SPEC.md's "Writing a file" makes of each.
	if got := read(t, b.Bytes()); len(got) != 1 {
		t.Fatalf("the file written back holds %d records", len(got))
	}
}

// TestASegmentDescriptorWordThatDisagreesWithTheExtentIsReported is "The extent
// governs, and framing is checked against it" under this framing: the segments
// reassemble into a run of bytes, and a run that is not the record's extent is
// an error at the record it happened on.
func TestASegmentDescriptorWordThatDisagreesWithTheExtentIsReported(t *testing.T) {
	t.Parallel()

	raw := chunkBytes(t, "ID01", "A BODY OF TWENTY CHS")

	for name, in := range map[string][]byte{
		"a record one byte short": segmented(raw[:23], 8, 8, 7),
		"a record one byte long":  segmented(append(append([]byte{}, raw...), 0x40), 8, 8, 9),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r, err := NewReader(bytes.NewReader(in), Encoding())
			if err != nil {
				t.Fatalf("NewReader: %v", err)
			}

			if _, err := r.Next(); err == nil {
				t.Fatal("segments that do not come to the record's extent were read as a record")
			}
		})
	}
}

// TestSegmentsThatDoNotMakeOneRecordAreReported is the other thing a segment
// control code says: which of a record's segments a segment is.
func TestSegmentsThatDoNotMakeOneRecordAreReported(t *testing.T) {
	t.Parallel()

	raw := chunkBytes(t, "ID01", "A BODY OF TWENTY CHS")

	// A middle segment with nothing in front of it.
	in := append([]byte{0, 12, 0x03, 0}, raw[:8]...)

	r, err := NewReader(bytes.NewReader(in), Encoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	if _, err := r.Next(); err == nil {
		t.Fatal("a record beginning with a middle segment was read as a record")
	}
}
