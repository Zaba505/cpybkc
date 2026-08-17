// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The assertions over docs/ir/SPEC.md's "Appendix: A counted run, as nodes",
// generated.
//
// Four things that appendix says the automaton detects and a memoryless graph
// could not, one it says the same file with no type codes gives up, and the two
// halves of a register-counted table. The file is delimited by 0x15 and its
// detail record carries a COMP-3 field holding that byte, which is the pair
// that makes searching the input for a delimiter wrong.
package counted

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Zaba505/cobol-go/codec"
)

// laidOut is a record's bytes, written item by item, with the delimiter behind
// it — which is where a terminator stands.
func laidOut(t *testing.T, items func(*codec.Writer) error) []byte {
	t.Helper()

	var b bytes.Buffer

	w, err := codec.NewWriter(&b, Encoding())
	if err != nil {
		t.Fatalf("codec.NewWriter: %v", err)
	}

	if err := items(w); err != nil {
		t.Fatalf("laying the record out: %v", err)
	}

	b.Write([]byte{0x15})

	return b.Bytes()
}

// headerBytes is one HEADER-RECORD: a detail count, a flag and the count the
// summary's two tables are sized by.
func headerBytes(t *testing.T, details int, flag string, total int) []byte {
	t.Helper()

	return laidOut(t, func(w *codec.Writer) error {
		if err := w.WriteAlphanumeric("H", 1); err != nil {
			return err
		}

		if err := w.WriteZonedInt32(int32(details), 2, codec.SignUnsigned); err != nil {
			return err
		}

		if err := w.WriteAlphanumeric(flag, 1); err != nil {
			return err
		}

		return w.WriteZonedInt32(int32(total), 2, codec.SignUnsigned)
	})
}

// detailBytes is one DETAIL-RECORD whose amount is +152.50, which is the three
// bytes 15 25 0C — the first of them this file's delimiter.
func detailBytes(t *testing.T) []byte {
	t.Helper()

	return laidOut(t, func(w *codec.Writer) error {
		if err := w.WriteAlphanumeric("D", 1); err != nil {
			return err
		}

		return w.WritePackedInt32(15250, 5, codec.Signed)
	})
}

// summaryBytes is one SUMMARY-RECORD whose two tables the register sizes.
func summaryBytes(t *testing.T, lines int) []byte {
	t.Helper()

	return laidOut(t, func(w *codec.Writer) error {
		if err := w.WriteAlphanumeric("S", 1); err != nil {
			return err
		}

		for range lines {
			if err := w.WriteAlphanumeric("LIN", 3); err != nil {
				return err
			}
		}

		for range lines {
			if err := w.WriteAlphanumeric("NO", 2); err != nil {
				return err
			}
		}

		return nil
	})
}

// read is every record of in, through the generated reader.
func read(t *testing.T, in []byte) ([]Record, error) {
	t.Helper()

	r, err := NewReader(bytes.NewReader(in), Encoding())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	var out []Record

	for {
		rec, err := r.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}

		if err != nil {
			return out, err
		}

		out = append(out, rec)
	}
}

// joined is a file's records, delimiter and all.
func joined(records ...[]byte) []byte { return bytes.Join(records, nil) }

// TestACountedRunReadsAndWritesBackTheFileItWas is the whole appendix in one
// file: a header, the details its count governs, the summary its flag governs,
// and a second group behind them.
//
// Bytes in both directions. Under this placement a delimiter follows every
// record, the last included, so the writer emits exactly what the reader
// consumed.
func TestACountedRunReadsAndWritesBackTheFileItWas(t *testing.T) {
	t.Parallel()

	want := joined(
		headerBytes(t, 2, "Y", 2),
		detailBytes(t),
		detailBytes(t),
		summaryBytes(t, 2),
		headerBytes(t, 0, "N", 0),
	)

	records, err := read(t, want)
	if err != nil {
		t.Fatalf("reading the file: %v", err)
	}

	if len(records) != 5 {
		t.Fatalf("the file holds five records and the reader produced %d", len(records))
	}

	// The COMP-3 field of each detail carries this file's delimiter inside it,
	// so a reader that scanned for one would have cut the record in half.
	for _, at := range []int{1, 2} {
		detail, ok := records[at].(*DetailRecord)
		if !ok {
			t.Fatalf("record %d is a %T", at+1, records[at])
		}

		if detail.Amount != 15250 {
			t.Errorf("the detail's amount came back as %d, want 15250", detail.Amount)
		}
	}

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

	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("the file does not write back the bytes it was read from\n got: % x\nwant: % x", b.Bytes(), want)
	}
}

// TestTheFourThingsAMemorylessGraphWouldNotDetect is the list the appendix
// makes, each with the diagnostic it says the consumer owes.
func TestTheFourThingsAMemorylessGraphWouldNotDetect(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		file []byte
		says []string
	}{
		// End of input in the group state with the count at two: the
		// acceptance guards do not hold, so the file is truncated.
		"a file ending two details short": {
			file: joined(headerBytes(t, 2, "N", 0)),
			says: []string{"not complete", "node 20"},
		},

		// End of input with the flag guard failing — also truncated, and
		// distinguishable from the file simply running out mid-run.
		"a missing summary where the flag says Y": {
			file: joined(headerBytes(t, 0, "Y", 0)),
			says: []string{"not complete", "node 21"},
		},

		// In the group state with the count at zero, the detail transition is
		// ineligible and no other predicate matches. It was a guard that
		// excluded the transition that would have matched, and that is what the
		// consumer says rather than calling the record undescribed.
		"a sixth detail where the header said five": {
			file: joined(headerBytes(t, 0, "N", 0), detailBytes(t)),
			says: []string{"a guard excluded", "DETAIL-RECORD", "node 20"},
		},

		// The same failure, on the flag.
		"a summary where the flag says N": {
			file: joined(headerBytes(t, 0, "N", 0), summaryBytes(t, 0)),
			says: []string{"a guard excluded", "SUMMARY-RECORD", "node 21"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := read(t, tc.file)
			if err == nil {
				t.Fatal("the reader read the file as complete")
			}

			for _, want := range tc.says {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the report reads %q and does not name %s", err, want)
				}
			}
		})
	}
}

// TestAGuardExcludedTransitionCarryingNoPredicateDoesNotDisplaceTheDiagnostic
// is the other half of docs/ir/SPEC.md's "A transition may carry no predicate".
//
// The summarised state offers one transition carrying no predicate and a guard
// nothing satisfies, in front of one carrying a predicate. The first would have
// matched whatever was in front of the reader, so it says nothing about the
// bytes in hand, and a record neither takes is a record the layout does not
// describe rather than one a guard excluded.
func TestAGuardExcludedTransitionCarryingNoPredicateDoesNotDisplaceTheDiagnostic(t *testing.T) {
	t.Parallel()

	_, err := read(t, joined(
		headerBytes(t, 0, "Y", 0),
		summaryBytes(t, 0),
		detailBytes(t),
	))
	if err == nil {
		t.Fatal("a detail behind a summary was read as a record the layout describes")
	}

	if strings.Contains(err.Error(), "a guard excluded") {
		t.Errorf("the report reads %q, and a transition carrying no predicate says nothing about the bytes in hand", err)
	}

	for _, want := range []string{"does not describe", "HEADER-RECORD"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the report reads %q and does not name %s", err, want)
		}
	}
}

// TestATableCountedByARegisterIsSizedByItAndCheckedAgainstIt is
// docs/ir/SPEC.md's "A writer evaluates a guard, it never back-fills a count":
// the register was filled two records ago, so the occurrences are what has to
// agree with it.
//
// Both of the summary's tables name that one register, and each is checked
// against it rather than only the first — neither of them sets it, so there is
// nothing for a writer to pick between.
func TestATableCountedByARegisterIsSizedByItAndCheckedAgainstIt(t *testing.T) {
	t.Parallel()

	for name, wrong := range map[string]func(*SummaryRecord){
		"the first table":  func(x *SummaryRecord) { x.Line = x.Line[:1] },
		"the second table": func(x *SummaryRecord) { x.Note = x.Note[:1] },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			records, err := read(t, joined(headerBytes(t, 0, "Y", 2), summaryBytes(t, 2)))
			if err != nil {
				t.Fatalf("reading the file: %v", err)
			}

			summary, ok := records[1].(*SummaryRecord)
			if !ok {
				t.Fatalf("the second record is a %T", records[1])
			}

			if len(summary.Line) != 2 || len(summary.Note) != 2 {
				t.Fatalf("the register sized the tables to %d and %d, want 2 and 2", len(summary.Line), len(summary.Note))
			}

			wrong(summary)

			var b bytes.Buffer

			w, err := NewWriter(&b, Encoding())
			if err != nil {
				t.Fatalf("NewWriter: %v", err)
			}

			if err := w.Write(records[0]); err != nil {
				t.Fatalf("Write: %v", err)
			}

			err = w.Write(summary)
			if err == nil {
				t.Fatal("a writer emitted a table whose occurrences disagree with the register that counts it")
			}

			if !strings.Contains(err.Error(), "node 22") {
				t.Errorf("the refusal reads %q and does not name the register", err)
			}
		})
	}
}

// TestAWriterRefusesARecordAGuardExcludes is the writing side of the sixth
// detail: a caller writing one after a header saying none is told so at the
// record that made the mistake, and told which register said otherwise.
func TestAWriterRefusesARecordAGuardExcludes(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer

	w, err := NewWriter(&b, Encoding())
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	if err := w.Write(&HeaderRecord{TypeCode: "H", DtlCount: 0, SumFlag: "N", TotalCount: 0}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	err = w.Write(&DetailRecord{TypeCode: "D", Amount: 15250})
	if err == nil {
		t.Fatal("a writer emitted a detail after a header saying there are none")
	}

	for _, want := range []string{"a guard excluded", "node 20"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal reads %q and does not name %s", err, want)
		}
	}
}

// TestAWriterEvaluatesAPredicateAndNeverInvertsOne is docs/ir/SPEC.md's rule of
// that name at the file level: the discriminating value is the caller's, and a
// writer checks it and reports it when it is wrong rather than storing a value
// that would satisfy the test.
func TestAWriterEvaluatesAPredicateAndNeverInvertsOne(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer

	w, err := NewWriter(&b, Encoding())
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	// Every transition leaving the start state is selected by a type code of
	// H, and this record's is not.
	err = w.Write(&HeaderRecord{TypeCode: "X", DtlCount: 0, SumFlag: "N", TotalCount: 0})
	if err == nil {
		t.Fatal("a writer emitted a record no transition's predicate matches")
	}

	if !strings.Contains(err.Error(), "HEADER-RECORD") {
		t.Errorf("the refusal reads %q and does not name the record", err)
	}

	if b.Len() != 0 {
		t.Errorf("a refused record put %d bytes in the file", b.Len())
	}
}

// TestAFileWithNoDelimiterBehindItsLastRecordIsTruncated is the detection this
// placement buys: a terminator follows every record, the last included, so a
// final record with nothing behind it is a file that was cut short.
func TestAFileWithNoDelimiterBehindItsLastRecordIsTruncated(t *testing.T) {
	t.Parallel()

	whole := joined(headerBytes(t, 0, "N", 0))

	_, err := read(t, whole[:len(whole)-1])
	if err == nil {
		t.Fatal("a file whose last record carries no delimiter was read as complete")
	}

	if !strings.Contains(err.Error(), "cut short") {
		t.Errorf("the report reads %q and does not say the file was cut short", err)
	}
}

// TestADelimiterThatIsNotWhereTheExtentEndsIsReported is the other half of "The
// extent governs, and framing is checked against it".
func TestADelimiterThatIsNotWhereTheExtentEndsIsReported(t *testing.T) {
	t.Parallel()

	whole := joined(headerBytes(t, 0, "N", 0))

	// The delimiter one byte early, so the record's extent runs past it.
	moved := append(append([]byte{}, whole[:len(whole)-2]...), 0x15, 0x40)

	if _, err := read(t, moved); err == nil {
		t.Fatal("a delimiter that is not where the extent ends was read as a well-formed file")
	}
}
