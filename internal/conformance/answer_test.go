// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package conformance

import (
	"strings"
	"testing"
)

// the values every case below is written against: one record, read from a file
// the reader reads to its end.
const oneRecord = `{
	"records": [
		{"name": "ORDER-RECORD", "value": {"ORDER-ID": "A001", "ORDER-QTY": "42"}}
	]
}`

// TestCompareAnswerHoldsBothDirectionsToTheEntry walks what a runner can say
// about an entry once it is asked in both directions.
//
// The two directions are compared against the *entry* rather than against each
// other, which is the case a reader and a writer that are wrong the same way
// would otherwise pass: what the file holds is written down once, in
// values.json, and both answers are held to it (#68).
func TestCompareAnswerHoldsBothDirectionsToTheEntry(t *testing.T) {
	tests := map[string]struct {
		want   string
		answer string
		says   []string
	}{
		"both directions agree with the entry": {
			want:   oneRecord,
			answer: `{"decoded": ` + oneRecord + `, "written": ` + oneRecord + `}`,
		},
		"the reader disagrees": {
			want: oneRecord,
			answer: `{"decoded": ` + strings.Replace(oneRecord, `"A001"`, `"B002"`, 1) +
				`, "written": ` + oneRecord + `}`,
			says: []string{"reading the entry's bytes", "ORDER-RECORD.ORDER-ID", `"B002"`},
		},
		"the writer laid out a file that reads back as something else": {
			want: oneRecord,
			answer: `{"decoded": ` + oneRecord +
				`, "written": ` + strings.Replace(oneRecord, `"42"`, `"41"`, 1) + `}`,
			says: []string{"reading back the file written from those records", "ORDER-RECORD.ORDER-QTY"},
		},
		"the writer refused the records it was given": {
			want: oneRecord,
			answer: `{"decoded": ` + oneRecord +
				`, "written": {"records": [], "failure": "writing record 1: a run of 3 bytes for a slack node 4 bytes wide"}}`,
			says: []string{"reading back the file written from those records", "a run of 3 bytes"},
		},
		"the records were never written back": {
			want:   oneRecord,
			answer: `{"decoded": ` + oneRecord + `}`,
			says:   []string{"were not written back"},
		},
		"an entry about a file the reader refuses": {
			want:   `{"records": [], "failure": "the sign nibble is not one of the four"}`,
			answer: `{"decoded": {"records": [], "failure": "invalid sign nibble 0xE at offset 7"}}`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := CompareAnswer(parsed(t, test.want), answered(t, test.answer))

			if len(test.says) == 0 {
				if err != nil {
					t.Fatalf("the comparison reported %v, and the answer is what the entry expects", err)
				}

				return
			}

			if err == nil {
				t.Fatal("the comparison passed, and the answer is not what the entry expects")
			}

			for _, says := range test.says {
				if !strings.Contains(err.Error(), says) {
					t.Errorf("the report is %q, and it does not say %q", err, says)
				}
			}
		})
	}
}

// TestParseAnswerRefuses walks the answers the format does not admit.
func TestParseAnswerRefuses(t *testing.T) {
	tests := map[string]string{
		"nothing that was read":            `{"written": ` + oneRecord + `}`,
		"a field nobody reads":             `{"decoded": ` + oneRecord + `, "bytes": "AAA="}`,
		"a values document on its own":     oneRecord,
		"a record with no name":            `{"decoded": {"records": [{"value": {}}]}}`,
		"a record with no value":           `{"decoded": {"records": [{"name": "ORDER-RECORD"}]}}`,
		"a written record with no name":    `{"decoded": ` + oneRecord + `, "written": {"records": [{"value": {}}]}}`,
		"something behind the document":    `{"decoded": ` + oneRecord + `} and then some`,
		"a file written back from nothing": `{"decoded": {"records": [], "failure": "short record"}, "written": {"records": []}}`,
	}

	for name, document := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseAnswer([]byte(document)); err == nil {
				t.Fatal("the answer was read, and the format does not admit it")
			}
		})
	}
}

// TestParseAnswerNamesWhichDocumentIsWrong asserts that a fault in one of the
// two values documents says which one it is in.
//
// They are the same shape, so a report naming neither sends an author to
// whichever they looked at first — and the two are written by different halves
// of a runner.
func TestParseAnswerNamesWhichDocumentIsWrong(t *testing.T) {
	_, err := ParseAnswer([]byte(`{"decoded": ` + oneRecord + `, "written": {"records": [{"value": {}}]}}`))
	if err == nil {
		t.Fatal("the answer was read, and one of its records carries no name")
	}

	if !strings.Contains(err.Error(), "written") {
		t.Errorf("the report is %q, and it does not say which document the record is in", err)
	}
}

// answered is an answer document written inline, for a test to compare.
func answered(t *testing.T, document string) *Answer {
	t.Helper()

	answer, err := ParseAnswer([]byte(document))
	if err != nil {
		t.Fatalf("%v", err)
	}

	return answer
}
