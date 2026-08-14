// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package conformance

import (
	"fmt"
	"math"
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

// TestCompareIsOverTheWrittenForm asserts that the comparison is string
// equality over what each side wrote, and never a comparison of two decoded
// numbers.
//
// The case that matters is the negative zero. The comparison used to be Go's !=
// on an any, which for a JSON number holding a float64 is IEEE equality — and
// under IEEE equality -0.0 equals 0.0, so a generator that lost the sign of a
// zero passed silently over an entry that stated one. Both spellings of zero
// are here as the strings the value language now writes them as, and as the
// JSON numbers it no longer does, because the defect was in the comparison and
// would come back if only the writing side had been changed.
func TestCompareIsOverTheWrittenForm(t *testing.T) {
	const document = `{"records": [{"name": "FLOAT-RECORD", "value": {"F-SHORT": %s}}]}`

	tests := map[string]struct {
		want, got string
		agree     bool
	}{
		"the same float":                     {want: `"0x1p+0"`, got: `"0x1p+0"`, agree: true},
		"a float that differs":               {want: `"0x1p+0"`, got: `"0x1.2p+3"`},
		"a negative zero read as a zero":     {want: `"-0x0p+0"`, got: `"0x0p+0"`},
		"a zero read as a negative zero":     {want: `"0x0p+0"`, got: `"-0x0p+0"`},
		"the same NaN":                       {want: `"NaN"`, got: `"NaN"`, agree: true},
		"an infinity read as a NaN":          {want: `"Infinity"`, got: `"NaN"`},
		"both infinities":                    {want: `"Infinity"`, got: `"-Infinity"`},
		"two zeros written as JSON numbers":  {want: `-0.0`, got: `0.0`},
		"a float written as a JSON number":   {want: `"0x1p+0"`, got: `1.0`},
		"an entry still carrying the number": {want: `1.0`, got: `"0x1p+0"`},

		// The comparison changed for every scalar and not only for a float, so
		// the kinds that changed as a side effect are here too: rendering both
		// sides has to keep telling a JSON kind from the string that spells it.
		"a number and the decimal string of it": {want: `"1"`, got: `1`},
		"a bool and the string of it":           {want: `true`, got: `"true"`},
		"the string of a bool and the bool":     {want: `"true"`, got: `true`},
		"a null and the string of it":           {want: `null`, got: `"null"`},
		"two nulls":                             {want: `null`, got: `null`, agree: true},
		"two bools that differ":                 {want: `true`, got: `false`},
		"two long base64 values that differ in the last character": {
			want: `"QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVphYmNkZWZnaGlqaw=="`,
			got:  `"QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVphYmNkZWZnaGlqbA=="`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := Compare(
				parsed(t, fmt.Sprintf(document, test.want)),
				parsed(t, fmt.Sprintf(document, test.got)),
			)

			if test.agree && err != nil {
				t.Fatalf("the comparison reported %v, and the two wrote the same thing", err)
			}

			if !test.agree && err == nil {
				t.Fatalf("the comparison passed, and one side wrote %s where the other wrote %s",
					test.got, test.want)
			}
		})
	}
}

// TestRenderedTellsTheScalarKindsApart pins the property [Compare] now rests
// on: two values that are not the same value do not render as the same string.
//
// It is a test about a helper because the helper became load bearing. rendered
// was written to quote a value in a report, and a report is judged by how it
// reads — so without this, a later change made for readability could lose the
// quotes around a string or shorten a long one, and the corpus would go on
// passing while comparing two different answers as equal. That failure reports
// nothing at all, which is the worst kind a conformance harness has.
func TestRenderedTellsTheScalarKindsApart(t *testing.T) {
	// One value per JSON kind a decoded scalar can be, beside the string that
	// spells it — which is the collision that matters, because the value
	// language writes a number and a run of bytes as strings.
	values := []any{
		nil, "null",
		true, "true",
		false, "false",
		float64(1), "1",
		float64(0), "0",
		math.Copysign(0, -1), "-0",
		"", "0x1p+0", "NaN", "Infinity", "-Infinity",
		"QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVphYmNkZWZnaGlqaw==",
		"QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVphYmNkZWZnaGlqbA==",
	}

	seen := make(map[string]any, len(values))

	for _, value := range values {
		form := rendered(value)

		if first, ok := seen[form]; ok {
			t.Errorf("%#v and %#v both render as %s, so the comparison cannot tell them apart", first, value, form)

			continue
		}

		seen[form] = value
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
