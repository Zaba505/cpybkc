// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package conformance

import (
	"strings"
	"testing"
)

// TestCompareReportsEveryDisagreement walks the ways a runner's answer can
// differ from an entry.
//
// The wording is asserted along with the verdict, because a conformance failure
// is read by somebody deciding whether their generator or the entry is wrong,
// and "not equal" sends them to neither. Each case names the path through the
// record, which is what makes a report over a file of many records usable.
func TestCompareReportsEveryDisagreement(t *testing.T) {
	const want = `{
		"records": [
			{
				"name": "ORDER-RECORD",
				"value": {
					"ORDER-ID": "A001",
					"ORDER-QTY": "42",
					"ORDER-LINE": [{"LINE-SKU": "SK1"}, {"LINE-SKU": "SK2"}]
				}
			}
		]
	}`

	tests := map[string]struct {
		got  string
		says []string
	}{
		"the same values": {got: want},
		"an item holding something else": {
			got:  strings.Replace(want, `"A001"`, `"B002"`, 1),
			says: []string{"ORDER-RECORD.ORDER-ID", `"B002"`, `"A001"`},
		},
		"a number written as a number": {
			got:  strings.Replace(want, `"42"`, `42`, 1),
			says: []string{"ORDER-RECORD.ORDER-QTY", "42"},
		},
		"an item the entry expects and the runner did not surface": {
			got:  strings.Replace(want, `"ORDER-QTY": "42",`, ``, 1),
			says: []string{"ORDER-QTY is not there"},
		},
		"an item the entry does not describe": {
			got:  strings.Replace(want, `"ORDER-ID": "A001",`, `"ORDER-ID": "A001", "ORDER-FILLER": "  ",`, 1),
			says: []string{"ORDER-FILLER", "does not expect it at all"},
		},
		"a table of the wrong length": {
			got:  strings.Replace(want, `, {"LINE-SKU": "SK2"}`, ``, 1),
			says: []string{"ORDER-LINE holds 1 occurrences and the entry expects 2"},
		},
		"an occurrence holding something else": {
			got:  strings.Replace(want, `"SK2"`, `"SK9"`, 1),
			says: []string{"ORDER-LINE[1].LINE-SKU"},
		},
		"a group where the entry expects one item": {
			got:  strings.Replace(want, `"ORDER-ID": "A001"`, `"ORDER-ID": {"NESTED": "A001"}`, 1),
			says: []string{"ORDER-ID", "it is a group"},
		},
		"a record the file does not hold": {
			got:  `{"records": []}`,
			says: []string{"the file holds 0 records and the entry expects 1"},
		},
		"a record of another type": {
			got:  strings.Replace(want, `"ORDER-RECORD"`, `"HEADER-RECORD"`, 1),
			says: []string{"record 1 is a HEADER-RECORD and the entry expects a ORDER-RECORD"},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := Compare(parsed(t, want), parsed(t, test.got))

			if len(test.says) == 0 {
				if err != nil {
					t.Fatalf("the comparison reported %v, and the two agree", err)
				}

				return
			}

			if err == nil {
				t.Fatal("the comparison passed, and the two disagree")
			}

			for _, says := range test.says {
				if !strings.Contains(err.Error(), says) {
					t.Errorf("the report is %q, and it does not say %q", err, says)
				}
			}
		})
	}
}

// TestCompareReadsAFailureAsAnAnswer asserts the one part of a values document
// that is compared for its presence rather than its content.
//
// A diagnostic is a generator's own wording in its own language, so an entry
// demanding particular words would be an entry only one generator could pass.
// What an entry does state is that reading failed, and where.
func TestCompareReadsAFailureAsAnAnswer(t *testing.T) {
	const (
		read   = `{"records": [{"name": "ORDER-RECORD", "value": {"ORDER-ID": "A001"}}]}`
		failed = `{"records": [{"name": "ORDER-RECORD", "value": {"ORDER-ID": "A001"}}], "failure": "%s"}`
	)

	t.Run("a failure the entry expects", func(t *testing.T) {
		want := parsed(t, strings.ReplaceAll(failed, "%s", "the sign nibble is not one of the four"))
		got := parsed(t, strings.ReplaceAll(failed, "%s", "record 2: invalid sign nibble 0xE at offset 7"))

		if err := Compare(want, got); err != nil {
			t.Fatalf("the comparison reported %v, and both sides failed", err)
		}
	})

	t.Run("a failure the entry does not expect", func(t *testing.T) {
		err := Compare(parsed(t, read), parsed(t, strings.ReplaceAll(failed, "%s", "short record")))
		if err == nil {
			t.Fatal("the comparison passed, and the file was expected to be read to its end")
		}

		if !strings.Contains(err.Error(), "short record") {
			t.Errorf("the report is %q, and it does not carry what the runner said", err)
		}
	})

	t.Run("a failure the entry expects and did not get", func(t *testing.T) {
		err := Compare(parsed(t, strings.ReplaceAll(failed, "%s", "a truncated record")), parsed(t, read))
		if err == nil {
			t.Fatal("the comparison passed, and the file was expected to fail")
		}

		if !strings.Contains(err.Error(), "a truncated record") {
			t.Errorf("the report is %q, and it does not carry what the entry expects", err)
		}
	})
}

// TestParseValuesRefuses walks the values documents the format does not admit.
func TestParseValuesRefuses(t *testing.T) {
	tests := map[string]string{
		"a field nobody reads":             `{"records": [], "note": "why"}`,
		"a record with no name":            `{"records": [{"value": {}}]}`,
		"a record with no value":           `{"records": [{"name": "ORDER-RECORD"}]}`,
		"something that is not a document": `[]`,
		"more than one document":           `{"records": []}{"records": []}`,
		"something behind the document":    `{"records": []} and then some`,
	}

	for name, document := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseValues([]byte(document)); err == nil {
				t.Fatal("the document was read, and the format does not admit it")
			}
		})
	}
}

// parsed is a values document written inline, for a test to compare.
func parsed(t *testing.T, document string) *Values {
	t.Helper()

	values, err := ParseValues([]byte(document))
	if err != nil {
		t.Fatalf("%v", err)
	}

	return values
}
