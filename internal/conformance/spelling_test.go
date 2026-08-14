// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package conformance

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Zaba505/cpybkc/irpb"
)

// txnDescriptor is a descriptor carrying one item of every form the value
// language has, in every position a value can be in: at the top of a record, in
// a table, behind a variant's two arms, and repeating on its own.
//
// It carries the encoding of nothing and the width of almost nothing, because
// neither decides a form — docs/conformance/SPEC.md makes the form a function of
// usage and category alone — and a descriptor written out with every attribute
// set would bury the two that this file is about. It is a fixture for the walk
// and not an entry: nothing here loads it, and it is deliberately not run
// through [github.com/Zaba505/cpybkc/internal/assemble.Validate], which the
// corpus's own entries are held to instead.
const txnDescriptor = `{
  "version": "IR_VERSION_1",
  "nodes": [
    {"file": {"unframed": {}, "start_state_id": "20"}},
    {"id": "1", "record": {"root_id": "2", "names": {"original": "TXN"}}},
    {"id": "2", "group": {
      "member_ids": ["3", "4", "8", "10", "11", "14", "5"],
      "names": {"original": "TXN"}
    }},
    {"id": "3", "field": {
      "width": 5, "usage": "USAGE_DISPLAY",
      "picture": {"category": "CATEGORY_NUMERIC", "digits": 5, "signed": true},
      "names": {"original": "AMT"}
    }},
    {"id": "4", "field": {
      "width": 6, "usage": "USAGE_DISPLAY",
      "picture": {"category": "CATEGORY_ALPHANUMERIC"},
      "names": {"original": "NAME"}
    }},
    {"id": "5", "group": {
      "member_ids": ["9", "6"],
      "names": {"original": "LINE"},
      "repetition": {"constant": 2}
    }},
    {"id": "6", "variant": {"arms": [
      {"predicate_id": "30", "field_id": "7"},
      {"predicate_id": "30", "group_id": "12"}
    ]}},
    {"id": "7", "field": {
      "width": 3, "usage": "USAGE_DISPLAY",
      "picture": {"category": "CATEGORY_NUMERIC", "digits": 3},
      "names": {"original": "QTY"}
    }},
    {"id": "8", "slack": {"width": 1}},
    {"id": "9", "field": {
      "width": 3, "usage": "USAGE_DISPLAY",
      "picture": {"category": "CATEGORY_ALPHANUMERIC"},
      "names": {"original": "SKU"}
    }},
    {"id": "10", "field": {"width": 4, "usage": "USAGE_INDEX", "names": {"original": "PTR"}}},
    {"id": "11", "field": {"width": 4, "usage": "USAGE_COMP_1", "names": {"original": "RATE"}}},
    {"id": "12", "group": {"member_ids": ["13"], "names": {"original": "DETAIL"}}},
    {"id": "13", "field": {
      "width": 2, "usage": "USAGE_DISPLAY",
      "picture": {"category": "CATEGORY_ALPHANUMERIC"},
      "names": {"original": "CODE"}
    }},
    {"id": "14", "field": {
      "width": 1, "usage": "USAGE_DISPLAY",
      "picture": {"category": "CATEGORY_ALPHANUMERIC"},
      "names": {"original": "TAGS"},
      "repetition": {"constant": 2}
    }}
  ]
}`

// txnValues is a document [txnDescriptor] admits, which every case below breaks
// in exactly one place.
const txnValues = `{
  "records": [
    {
      "name": "TXN",
      "value": {
        "AMT": "-12345",
        "NAME": "WIDGET",
        "PTR": "AAECAw==",
        "RATE": "0x1.2p+3",
        "TAGS": ["A", "B"],
        "LINE": [
          {"SKU": "SK1", "QTY": "7"},
          {"SKU": "SK2", "DETAIL": {"CODE": "XX"}}
        ]
      }
    }
  ]
}`

// TestTheLoaderRefusesAValueThatIsNotCanonical walks the spellings a values
// document may not carry, one per rule of the value language and one per place
// in a record a value can be written.
//
// Every case is the document above with one scalar rewritten, so what is
// asserted is the rule rather than a fixture. The path is asserted along with
// the rule, because a corpus of many-record entries is read by somebody who has
// to find the value they mistyped.
func TestTheLoaderRefusesAValueThatIsNotCanonical(t *testing.T) {
	tests := map[string]struct {
		was, is string
		says    []string
	}{
		"a number carrying a leading zero": {
			was: `"-12345"`, is: `"-012345"`,
			says: []string{"record 1 TXN.AMT", numberHasNoZero},
		},
		"a number carrying a leading +": {
			was: `"-12345"`, is: `"+12345"`,
			says: []string{"record 1 TXN.AMT", numberHasNoPlus},
		},
		"a negative zero": {
			was: `"-12345"`, is: `"-0"`,
			says: []string{"record 1 TXN.AMT", numberHasNoMinus},
		},
		"a number carrying a decimal point": {
			was: `"-12345"`, is: `"123.45"`,
			says: []string{"record 1 TXN.AMT", numberIsDecimal},
		},
		"a number written as a JSON number": {
			was: `"-12345"`, is: `-12345`,
			says: []string{"record 1 TXN.AMT", scalarIsAString},
		},
		"a run of bytes that is not padded": {
			was: `"AAECAw=="`, is: `"AAECAw"`,
			says: []string{"record 1 TXN.PTR", bytesAreBase64},
		},
		"a run of bytes in the URL-safe alphabet": {
			was: `"AAECAw=="`, is: `"-_8="`,
			says: []string{"record 1 TXN.PTR", bytesAreBase64},
		},
		"a run of bytes carrying a line break": {
			was: `"AAECAw=="`, is: `"AAEC\nAw=="`,
			says: []string{"record 1 TXN.PTR", bytesAreBase64},
		},
		"a run of bytes whose last quantum is not canonical": {
			was: `"AAECAw=="`, is: `"AB=="`,
			says: []string{"record 1 TXN.PTR", bytesAreCanon},
		},
		"a float written in decimal": {
			was: `"0x1.2p+3"`, is: `"9"`,
			says: []string{"record 1 TXN.RATE", floatIsWritten},
		},
		"a float written as a JSON number": {
			was: `"0x1.2p+3"`, is: `9`,
			says: []string{"record 1 TXN.RATE", scalarIsAString},
		},
		"a float whose exponent Go padded": {
			was: `"0x1.2p+3"`, is: `"0x1.2p+03"`,
			says: []string{"record 1 TXN.RATE", floatExponentSign},
		},
		"a character item padded to its width": {
			was: `"WIDGET"`, is: `"WIDGET   "`,
			says: []string{"record 1 TXN.NAME", textIsTrimmed},
		},
		"an occurrence of a repeating item": {
			was: `["A", "B"]`, is: `["A", "B "]`,
			says: []string{"record 1 TXN.TAGS[1]", textIsTrimmed},
		},
		"an item inside a table": {
			was: `"SK1"`, is: `"SK1 "`,
			says: []string{"record 1 TXN.LINE[0].SKU", textIsTrimmed},
		},
		"an item behind the first arm of a variant": {
			was: `"QTY": "7"`, is: `"QTY": "07"`,
			says: []string{"record 1 TXN.LINE[0].QTY", numberHasNoZero},
		},
		"an item behind the second arm of a variant": {
			was: `"CODE": "XX"`, is: `"CODE": "X "`,
			says: []string{"record 1 TXN.LINE[1].DETAIL.CODE", textIsTrimmed},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			document := strings.Replace(txnValues, test.was, test.is, 1)
			if document == txnValues {
				t.Fatalf("the case rewrites %s, and the document does not carry it", test.was)
			}

			err := parsed(t, document).check(descriptorOf(t, txnDescriptor))
			if err == nil {
				t.Fatal("the document was accepted, and the value language does not spell a value that way")
			}

			for _, says := range test.says {
				if !strings.Contains(err.Error(), says) {
					t.Errorf("the refusal is %q, and it does not say %q", err, says)
				}
			}
		})
	}
}

// TestTheLoaderAdmitsEveryCanonicalSpelling is the other half: nothing above
// fires on a document that is written the way the format writes one.
//
// A loader that refused a correct entry would be worse than one that checked
// nothing, because the corpus is what a third party's runner is judged against
// and an entry it cannot load is a claim it cannot be held to.
func TestTheLoaderAdmitsEveryCanonicalSpelling(t *testing.T) {
	if err := parsed(t, txnValues).check(descriptorOf(t, txnDescriptor)); err != nil {
		t.Fatalf("%v", err)
	}
}

// TestTheSpellingWalkIsSilentAboutShape holds the loader to the line it draws:
// a spelling is its business and a shape is not.
//
// Each of these documents disagrees with the descriptor about the shape of a
// value, and every one of those disagreements is something two answers can have
// — a generator that lost a table, an arm one implementation selected and
// another did not — which [Compare] reports against a runner that decoded the
// bytes. A loader that refused them would be refusing an entry over the very
// thing the corpus exists to compare, and it would do it to the entry rather
// than to the generator.
func TestTheSpellingWalkIsSilentAboutShape(t *testing.T) {
	tests := map[string]struct{ was, is string }{
		"a group where an item is described":     {was: `"NAME": "WIDGET"`, is: `"NAME": {"INNER": "WIDGET"}`},
		"a table written as one value":           {was: `["A", "B"]`, is: `"A"`},
		"an item written as a table":             {was: `"NAME": "WIDGET"`, is: `"NAME": ["WIDGET"]`},
		"a table's occurrence written as a name": {was: `{"SKU": "SK1", "QTY": "7"}`, is: `"SK1"`},
		"a key the descriptor does not carry":    {was: `"NAME": "WIDGET"`, is: `"NAME": "WIDGET", "EXTRA": "  "`},
		"an item the document does not carry":    {was: `"NAME": "WIDGET",`, is: ``},
		"neither arm of a variant":               {was: `, "QTY": "7"`, is: ``},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			document := strings.Replace(txnValues, test.was, test.is, 1)
			if document == txnValues {
				t.Fatalf("the case rewrites %s, and the document does not carry it", test.was)
			}

			if err := parsed(t, document).check(descriptorOf(t, txnDescriptor)); err != nil {
				t.Fatalf("the document was refused — %v — and a shape is not the loader's to have an opinion about", err)
			}
		})
	}
}

// TestTheFormOfAnItemIsUsageThenCategory pins the order the two attributes are
// read in.
//
// The order is the whole of the rule: the two floating-point usages carry
// CATEGORY_NUMERIC, so a reader that looked at the category first would hold
// every COMP-1 in the corpus to the decimal grammar and refuse the entries that
// carry one. An item the format decides nothing about is decided nothing about
// here either, rather than being defaulted into a form it might not have.
func TestTheFormOfAnItemIsUsageThenCategory(t *testing.T) {
	tests := map[string]struct {
		usage    irpb.Usage
		category irpb.Category
		want     form
	}{
		"a COMP-1, which is numeric and not a number": {usage: irpb.Usage_USAGE_COMP_1, category: irpb.Category_CATEGORY_NUMERIC, want: formFloat},
		"a COMP-2, which is numeric and not a number": {usage: irpb.Usage_USAGE_COMP_2, category: irpb.Category_CATEGORY_NUMERIC, want: formFloat},
		"an INDEX":  {usage: irpb.Usage_USAGE_INDEX, want: formBytes},
		"a POINTER": {usage: irpb.Usage_USAGE_POINTER, want: formBytes},
		"a NATIONAL, which is characters this format does not decode": {usage: irpb.Usage_USAGE_NATIONAL, category: irpb.Category_CATEGORY_ALPHANUMERIC, want: formBytes},
		"a zoned number":                           {usage: irpb.Usage_USAGE_DISPLAY, category: irpb.Category_CATEGORY_NUMERIC, want: formNumber},
		"a packed number":                          {usage: irpb.Usage_USAGE_PACKED_DECIMAL, category: irpb.Category_CATEGORY_NUMERIC, want: formNumber},
		"a COMP-6 number":                          {usage: irpb.Usage_USAGE_COMP_6, category: irpb.Category_CATEGORY_NUMERIC, want: formNumber},
		"a binary number":                          {usage: irpb.Usage_USAGE_BINARY, category: irpb.Category_CATEGORY_NUMERIC, want: formNumber},
		"a COMP-5 number":                          {usage: irpb.Usage_USAGE_COMP_5, category: irpb.Category_CATEGORY_NUMERIC, want: formNumber},
		"an alphabetic item":                       {usage: irpb.Usage_USAGE_DISPLAY, category: irpb.Category_CATEGORY_ALPHABETIC, want: formText},
		"an alphanumeric item":                     {usage: irpb.Usage_USAGE_DISPLAY, category: irpb.Category_CATEGORY_ALPHANUMERIC, want: formText},
		"a numeric-edited item":                    {usage: irpb.Usage_USAGE_DISPLAY, category: irpb.Category_CATEGORY_NUMERIC_EDITED, want: formText},
		"an alphanumeric-edited item":              {usage: irpb.Usage_USAGE_DISPLAY, category: irpb.Category_CATEGORY_ALPHANUMERIC_EDITED, want: formText},
		"an item whose descriptor decides neither": {usage: irpb.Usage_USAGE_DISPLAY, want: formUnstated},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			field := &irpb.Field{Usage: test.usage}
			if test.category != irpb.Category_CATEGORY_UNSPECIFIED {
				field.Picture = &irpb.Picture{Category: test.category}
			}

			if got := formOf(field); got != test.want {
				t.Errorf("the form is %d and the format gives it %d", got, test.want)
			}
		})
	}
}

// TestTheScalarFormsReadTheirGrammars exercises the three grammars a spelling is
// held to, away from the walk that reaches them.
//
// The float form has a table of its own beside [FormatFloat], which is the
// function it has to agree with; these three have nobody to agree with, so what
// they are checked against is the section of the spec each one states.
func TestTheScalarFormsReadTheirGrammars(t *testing.T) {
	t.Run("a number", func(t *testing.T) {
		admitted := []string{"0", "7", "42", "-1", "-12345", "123456789012345678901234567890"}
		refused := map[string]string{
			"":       numberIsDecimal,
			"-":      numberIsDecimal,
			"07":     numberHasNoZero,
			"012":    numberHasNoZero,
			"-07":    numberHasNoZero,
			"00":     numberHasNoZero,
			"+7":     numberHasNoPlus,
			"-0":     numberHasNoMinus,
			"0.5":    numberIsDecimal,
			"1e3":    numberIsDecimal,
			"1 000":  numberIsDecimal,
			"1,000":  numberIsDecimal,
			"7-":     numberIsDecimal,
			"١٢":     numberIsDecimal,
			"0x1p+0": numberIsDecimal,
		}

		walk(t, numberFault, admitted, refused)
	})

	t.Run("a run of bytes", func(t *testing.T) {
		admitted := []string{"", "AA==", "AAE=", "AAEC", "AAECAw==", "/+8=",
			"QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVphYmNkZWZnaGlqaw=="}
		refused := map[string]string{
			"AAEC=":      bytesAreBase64,
			"AAECA":      bytesAreBase64,
			"AAEC ":      bytesAreBase64,
			"AAEC\n":     bytesAreBase64,
			"AAEC\nAw==": bytesAreBase64,
			"-_8=":       bytesAreBase64,
			"AB==":       bytesAreCanon,
			"AAF=":       bytesAreCanon,
		}

		walk(t, bytesFault, admitted, refused)
	})

	t.Run("characters", func(t *testing.T) {
		// A tab, a NUL, a leading space and a character some charset renders
		// as blank are data and survive. Only a trailing U+0020 is padding,
		// and an item holding nothing but spaces is written "".
		admitted := []string{"", "A", " A", "A B", "A\t", "A\x00", "A\u00a0"}
		refused := map[string]string{
			"A ":   textIsTrimmed,
			" ":    textIsTrimmed,
			"    ": textIsTrimmed,
		}

		walk(t, textFault, admitted, refused)
	})
}

// walk holds one form's grammar to the values it admits and the values it
// refuses, each with the rule it is refused by.
func walk(t *testing.T, fault func(string) string, admitted []string, refused map[string]string) {
	t.Helper()

	for _, text := range admitted {
		if says := fault(text); says != "" {
			t.Errorf("%q is refused — %s — and the format admits it", text, says)
		}
	}

	for text, says := range refused {
		got := fault(text)

		if got == "" {
			t.Errorf("%q is admitted, and the format does not spell a value that way", text)

			continue
		}

		if got != says {
			t.Errorf("%q is refused as %q, and the rule it breaks is %q", text, got, says)
		}
	}
}

// descriptorOf reads a descriptor written inline, in the same JSON rendering an
// entry's ir.json is written in.
func descriptorOf(t *testing.T, document string) *irpb.Descriptor {
	t.Helper()

	var descriptor irpb.Descriptor
	if err := protojson.Unmarshal([]byte(document), &descriptor); err != nil {
		t.Fatalf("%v", err)
	}

	return &descriptor
}
