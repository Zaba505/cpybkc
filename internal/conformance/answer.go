// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package conformance

import "fmt"

// Answer is what a runner reports about one entry: what the generated reader
// made of the entry's bytes, and what the file the generated writer laid those
// records back out into reads as.
//
// Two documents rather than one because an entry states one set of values and a
// generator is asked about it twice, once in each direction (#68). A generator
// that reads a file correctly and writes one nobody can read back is a
// generator half the ecosystem cannot use, and a corpus that only ever read
// would call it conformant.
//
// # Why the writing direction is checked by reading, and not by comparing bytes
//
// The obvious check is that the bytes the writer produced are the entry's
// bytes, and it is the wrong one twice over.
//
// [docs/ir/SPEC.md], "Writing a file", makes byte identity a claim about a
// *record* and refuses to make it about a file: under an optional terminator a
// writer emits a final delimiter the input need not have carried, and under
// segmented framing it lays a record into as few segments as the largest
// allows, whatever the input did. Both are deliberate, so a corpus demanding
// the input's bytes back would fail two of the four framings by design.
//
// It is wrong at the field level too, and the corpus already holds the case:
// packed-ascii carries the lenient sign nibble A, which a reader admits as
// positive and a writer has no reason to emit — it writes the C the convention
// prescribes. The same holds of every encoding that admits more than one
// spelling of one value. Demanding the bytes back would make those entries
// unpassable by a correct generator, and dropping them would cost the corpus
// exactly the vectors it was seeded for.
//
// What the specification does make normative of a file is that "a file a writer
// produces MUST be one that a reader built from the same descriptor reads back
// as the records the writer was given", so that is what is compared: the
// records read out of the written file, against the same values.json the
// reading direction is held to. It holds for all four framings and for every
// encoding, and it needs nothing of a runner that the reading direction did not
// already need.
//
// [docs/ir/SPEC.md]: https://github.com/Zaba505/cpybkc/blob/main/docs/ir/SPEC.md
type Answer struct {
	// Decoded is what the generated reader made of the entry's bytes.
	Decoded *Values `json:"decoded"`

	// Written is what a reader makes of the file the generated writer produced
	// from the records Decoded holds.
	//
	// It is absent where the reading direction did not reach the end of the
	// file: a run that stopped at a failure holds no complete set of records to
	// write back, and an entry expecting a failure is an entry about reading.
	// It is absent, too, from a runner whose generator emits no writer at all —
	// emitting one is the generator's decision (docs/ir/SPEC.md, "Writing a
	// file"), and a runner is not asked to invent the direction. What this
	// repository's own runner does with that latitude is not to take it: the Go
	// generator emits a writer, so the Go runner reports this member, and
	// [CompareAnswer] holds it to it.
	Written *Values `json:"written,omitempty"`
}

// ParseAnswer reads a runner's answer, holding it to the shape the corpus
// format states.
//
// Unknown fields are refused at every level, for the reason entry.json refuses
// one: a key an author wrote in the expectation that it means something is a
// typo, and a document that silently ignores it is a document that passes for
// the wrong reason.
func ParseAnswer(b []byte) (*Answer, error) {
	var answer Answer
	if err := decodeOne(b, &answer); err != nil {
		return nil, err
	}

	var faults []error

	if answer.Decoded == nil {
		faults = append(faults, fmt.Errorf("decoded is what the generated reader made of the entry's bytes, and is required"))
	} else {
		faults = append(faults, prefixed("decoded", answer.Decoded.records())...)
	}

	if answer.Written != nil {
		faults = append(faults, prefixed("written", answer.Written.records())...)

		if answer.Decoded != nil && answer.Decoded.Failure != "" {
			faults = append(faults, fmt.Errorf(
				"written stands beside a decoded failure, and a file the reader refused holds no complete set of records to write back"))
		}
	}

	return &answer, joined(faults)
}

// CompareAnswer holds a runner's answer against what an entry expects, in both
// directions.
//
// One entry, one set of values, and both directions are held to it: the records
// the generated reader made of the entry's bytes, and the records read back out
// of the file the generated writer made of those records. The second comparison
// is against the entry rather than against the first answer on purpose — a
// reader and a writer that are wrong the same way agree with each other, and
// only the entry knows what the file holds.
func CompareAnswer(want *Values, got *Answer) error {
	if got == nil || got.Decoded == nil {
		return fmt.Errorf("the runner reported nothing it read")
	}

	var faults []error

	if err := Compare(want, got.Decoded); err != nil {
		faults = append(faults, fmt.Errorf("reading the entry's bytes: %w", err))
	}

	switch {
	case want.Failure != "":
		// Nothing is written back from a read that stopped, so there is nothing
		// to compare. The entry is about the reading direction and says so by
		// expecting a failure.
	case got.Written == nil:
		faults = append(faults, fmt.Errorf(
			"the records were not written back, and a file read to its end is one the corpus asks to be written again"))
	default:
		if err := Compare(want, got.Written); err != nil {
			faults = append(faults, fmt.Errorf("reading back the file written from those records: %w", err))
		}
	}

	return joined(faults)
}

// prefixed says which document of an answer a fault is in.
//
// The two are the same shape, so a fault reported as itself would send an
// author to whichever of them they looked at first.
func prefixed(document string, faults []error) []error {
	said := make([]error, 0, len(faults))

	for _, fault := range faults {
		said = append(said, fmt.Errorf("%s: %w", document, fault))
	}

	return said
}
