// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package conformance

import (
	"encoding/json"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Zaba505/cpybkc/irpb"
)

// grammarDescriptor is one node per row of docs/conformance/GRAMMAR.md: every
// usage the IR has, every category, and every way a node can be arranged —
// nested, repeating, behind a variant's arms, and slack.
//
// It is a fixture and not an entry. Nothing loads it, it carries the width and
// the encoding of nothing, and it is deliberately not run through
// [github.com/Zaba505/cpybkc/internal/assemble.Validate], for the reason
// txnDescriptor in spelling_test.go is not: what is under test is what a value
// is written as, which docs/conformance/SPEC.md makes a function of usage,
// category and the shape of the node tree alone.
//
// The identifiers are grouped by the table each row belongs to, which is what
// makes a row and its node findable from each other.
const grammarDescriptor = `{
  "version": "IR_VERSION_1",
  "nodes": [
    {"id": "100", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_ALPHANUMERIC"}, "names": {"original": "X-KEY"}}},
    {"id": "101", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_ALPHANUMERIC"}, "names": {"original": "HDR-NAME"}}},
    {"id": "102", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_ALPHANUMERIC"}, "names": {"original": "TRL-FILLER"}}},
    {"id": "103", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_ALPHANUMERIC"}, "names": {"original": "X-LEAD"}}},
    {"id": "104", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_ALPHANUMERIC"}, "names": {"original": "X-MIDDLE"}}},
    {"id": "105", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_ALPHANUMERIC"}, "names": {"original": "X-TAB"}}},
    {"id": "106", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_ALPHABETIC"}, "names": {"original": "A-WORD"}}},
    {"id": "107", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_NUMERIC_EDITED"}, "names": {"original": "E-AMOUNT"}}},
    {"id": "108", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_NUMERIC_EDITED"}, "names": {"original": "E-SIGNED"}}},
    {"id": "109", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_ALPHANUMERIC_EDITED"}, "names": {"original": "E-CODE"}}},

    {"id": "110", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_NUMERIC", "digits": 3}, "names": {"original": "N-COUNT"}}},
    {"id": "111", "field": {"usage": "USAGE_PACKED_DECIMAL", "picture": {"category": "CATEGORY_NUMERIC", "digits": 5, "signed": true}, "names": {"original": "P-AMOUNT"}}},
    {"id": "112", "field": {"usage": "USAGE_PACKED_DECIMAL", "picture": {"category": "CATEGORY_NUMERIC", "digits": 3, "signed": true}, "names": {"original": "P-ZERO"}}},
    {"id": "113", "field": {"usage": "USAGE_PACKED_DECIMAL", "picture": {"category": "CATEGORY_NUMERIC", "digits": 3, "signed": true}, "names": {"original": "P-MINUS-ZERO"}}},
    {"id": "114", "field": {"usage": "USAGE_PACKED_DECIMAL", "picture": {"category": "CATEGORY_NUMERIC", "digits": 5, "scale": -2, "signed": true}, "names": {"original": "P-SCALED"}}},
    {"id": "115", "field": {"usage": "USAGE_COMP_6", "picture": {"category": "CATEGORY_NUMERIC", "digits": 4}, "names": {"original": "C6-COUNT"}}},
    {"id": "116", "field": {"usage": "USAGE_BINARY", "picture": {"category": "CATEGORY_NUMERIC", "digits": 4, "signed": true}, "names": {"original": "B-DELTA"}}},
    {"id": "117", "field": {"usage": "USAGE_COMP_5", "picture": {"category": "CATEGORY_NUMERIC", "digits": 4}, "names": {"original": "B-MASK"}}},
    {"id": "118", "field": {"usage": "USAGE_PACKED_DECIMAL", "picture": {"category": "CATEGORY_NUMERIC", "digits": 18, "signed": true}, "names": {"original": "P-WIDE"}}},
    {"id": "119", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_NUMERIC", "digits": 19, "signed": true}, "names": {"original": "N-WIDER"}}},

    {"id": "120", "field": {"usage": "USAGE_COMP_2", "names": {"original": "F-ONE"}}},
    {"id": "121", "field": {"usage": "USAGE_COMP_2", "names": {"original": "F-NINE"}}},
    {"id": "122", "field": {"usage": "USAGE_COMP_2", "names": {"original": "F-SMALL"}}},
    {"id": "123", "field": {"usage": "USAGE_COMP_1", "names": {"original": "F-TENTH"}}},
    {"id": "124", "field": {"usage": "USAGE_COMP_2", "names": {"original": "F-TENTH-LONG"}}},
    {"id": "125", "field": {"usage": "USAGE_COMP_1", "names": {"original": "F-SUBNORMAL"}}},
    {"id": "126", "field": {"usage": "USAGE_COMP_2", "names": {"original": "F-ZERO"}}},
    {"id": "127", "field": {"usage": "USAGE_COMP_2", "names": {"original": "F-MINUS-ZERO"}}},
    {"id": "128", "field": {"usage": "USAGE_COMP_1", "names": {"original": "F-NAN"}}},
    {"id": "129", "field": {"usage": "USAGE_COMP_1", "names": {"original": "F-INF"}}},
    {"id": "130", "field": {"usage": "USAGE_COMP_1", "names": {"original": "F-MINUS-INF"}}},

    {"id": "131", "field": {"usage": "USAGE_INDEX", "names": {"original": "I-CURSOR"}}},
    {"id": "132", "field": {"usage": "USAGE_POINTER", "names": {"original": "P-ADDRESS"}}},
    {"id": "133", "field": {"usage": "USAGE_NATIONAL", "names": {"original": "N-TEXT"}}},
    {"id": "134", "field": {"usage": "USAGE_INDEX", "names": {"original": "I-NONE"}}},

    {"id": "140", "group": {"member_ids": ["141", "142"], "names": {"original": "PAIR"}}},
    {"id": "141", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_NUMERIC", "digits": 1}, "names": {"original": "A"}}},
    {"id": "142", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_NUMERIC", "digits": 1}, "names": {"original": "B"}}},

    {"id": "143", "group": {"member_ids": ["144", "145"], "names": {"original": "HDR"}}},
    {"id": "144", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_NUMERIC", "digits": 1}, "names": {"original": "HDR-ID"}}},
    {"id": "145", "group": {"member_ids": ["146"], "names": {"original": "HDR-WHEN"}}},
    {"id": "146", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_NUMERIC", "digits": 1}, "names": {"original": "WHEN-DAY"}}},

    {"id": "147", "group": {"member_ids": ["148"], "names": {"original": "PAD"}}},
    {"id": "148", "slack": {"width": 2}},

    {"id": "149", "group": {"member_ids": ["150", "151", "152"], "names": {"original": "SLK"}}},
    {"id": "150", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_NUMERIC", "digits": 1}, "names": {"original": "A"}}},
    {"id": "151", "slack": {"width": 1}},
    {"id": "152", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_NUMERIC", "digits": 1}, "names": {"original": "B"}}},

    {"id": "153", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_ALPHANUMERIC"}, "names": {"original": "TAG"}, "repetition": {"constant": 3}}},
    {"id": "154", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_ALPHANUMERIC"}, "names": {"original": "TAG"}, "repetition": {"constant": 1}}},
    {"id": "155", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_ALPHANUMERIC"}, "names": {"original": "TAG"}, "repetition": {"variable": {"field_id": "156", "min_occurrences": 0, "max_occurrences": 3}}}},
    {"id": "156", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_NUMERIC", "digits": 1}, "names": {"original": "TAG-COUNT"}}},

    {"id": "157", "group": {"member_ids": ["158"], "names": {"original": "ORDER-LINE"}, "repetition": {"constant": 2}}},
    {"id": "158", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_ALPHANUMERIC"}, "names": {"original": "LINE-SKU"}}},

    {"id": "159", "group": {"member_ids": ["160", "161", "164"], "names": {"original": "REC"}}},
    {"id": "160", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_NUMERIC", "digits": 1}, "names": {"original": "BEFORE"}}},
    {"id": "161", "variant": {"arms": [
      {"predicate_id": "999", "field_id": "162"},
      {"predicate_id": "999", "field_id": "163"}
    ]}},
    {"id": "162", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_NUMERIC", "digits": 1}, "names": {"original": "NUM"}}},
    {"id": "163", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_ALPHANUMERIC"}, "names": {"original": "TEXT"}}},
    {"id": "164", "field": {"usage": "USAGE_DISPLAY", "picture": {"category": "CATEGORY_NUMERIC", "digits": 1}, "names": {"original": "AFTER"}}}
  ]
}`

// The record types the rows about groups, tables and variants are written
// against. Each is the shape cmd/cpybkc-gen-go emits for its node: one exported
// field per member in the descriptor's order, an unexported one for the bytes a
// slack node retains, one pointer per arm of a variant, and a slice where a node
// repeats. Only the shape matters — [WriteValue] takes every key it writes from
// the descriptor, and never from a Go field name.
type (
	grammarPair struct{ A, B int32 }

	grammarWhen struct{ WhenDay int32 }

	grammarHeader struct {
		HdrID   int32
		HdrWhen grammarWhen
	}

	grammarPad struct{ slack []byte }

	grammarSlack struct {
		A     int32
		slack []byte
		B     int32
	}

	grammarLine struct{ LineSKU string }

	grammarRecord struct {
		Before int32
		Num    *int32
		Text   *string
		After  int32
	}
)

// grammarValue is one row of a value table: which node of [grammarDescriptor]
// the item is, and what the generated code decoded it into.
type grammarValue struct {
	node  uint64
	value any
}

// grammarValues is what every row of every value table in
// docs/conformance/GRAMMAR.md holds, keyed by the row's identifier.
//
// The document states what each is written as and this states what each is;
// neither says the other, which is the whole point — a table whose answers were
// taken from this map would be a recording of what the writer does rather than a
// statement of what it owes.
var grammarValues = map[string]grammarValue{
	"text-alphanumeric":              {node: 100, value: "A001"},
	"text-trailing-spaces":           {node: 101, value: "BATCH-0001     "},
	"text-all-spaces":                {node: 102, value: "          "},
	"text-leading-space":             {node: 103, value: " AB   "},
	"text-interior-spaces":           {node: 104, value: "A  B    "},
	"text-tab":                       {node: 105, value: "A\t  "},
	"text-alphabetic":                {node: 106, value: "ABC  "},
	"text-numeric-edited":            {node: 107, value: " 1,234.50"},
	"text-numeric-edited-blank-sign": {node: 108, value: "042 "},
	"text-alphanumeric-edited":       {node: 109, value: "AB CD"},

	"number-positive":        {node: 110, value: int32(42)},
	"number-negative":        {node: 111, value: int32(-12345)},
	"number-zero":            {node: 112, value: int32(0)},
	"number-negative-zero":   {node: 113, value: int32(0)},
	"number-scaled":          {node: 114, value: int32(-12345)},
	"number-comp-6":          {node: 115, value: int32(1234)},
	"number-binary":          {node: 116, value: int16(-1)},
	"number-binary-unsigned": {node: 117, value: uint64(65535)},
	"number-eighteen-digits": {node: 118, value: int64(999999999999999999)},
	"number-beyond-a-double": {node: 119, value: big.NewInt(-1234567890123456789)},

	"float-one":               {node: 120, value: float64(1)},
	"float-nine":              {node: 121, value: float64(9)},
	"float-thirty-second":     {node: 122, value: float64(0.03125)},
	"float-tenth-comp-1":      {node: 123, value: float32(0.1)},
	"float-tenth-comp-2":      {node: 124, value: float64(0.1)},
	"float-subnormal":         {node: 125, value: math.Float32frombits(1)},
	"float-zero":              {node: 126, value: float64(0)},
	"float-negative-zero":     {node: 127, value: math.Copysign(0, -1)},
	"float-nan":               {node: 128, value: float32(math.NaN())},
	"float-infinity":          {node: 129, value: float32(math.Inf(1))},
	"float-negative-infinity": {node: 130, value: float32(math.Inf(-1))},

	"bytes-index":    {node: 131, value: []byte{0x00, 0x00, 0x00, 0x07}},
	"bytes-pointer":  {node: 132, value: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00}},
	"bytes-national": {node: 133, value: []byte{0x00, 0x41, 0x00, 0x42}},
	"bytes-empty":    {node: 134, value: []byte{}},

	"group-two-members": {node: 140, value: grammarPair{A: 1, B: 2}},
	"group-nested":      {node: 143, value: grammarHeader{HdrID: 1, HdrWhen: grammarWhen{WhenDay: 2}}},
	"group-only-slack":  {node: 147, value: grammarPad{slack: []byte{0x40, 0x40}}},
	"slack-omitted":     {node: 149, value: grammarSlack{A: 1, slack: []byte{0x40}, B: 2}},

	"table-three":     {node: 153, value: []string{"x", "y", "z"}},
	"table-one":       {node: 154, value: []string{"x"}},
	"table-none":      {node: 155, value: []string{}},
	"table-of-groups": {node: 157, value: []grammarLine{{LineSKU: "SK1"}, {LineSKU: "SK2"}}},

	"variant-arm-held":   {node: 159, value: grammarRecord{Before: 1, Num: grammarNum(2), After: 3}},
	"variant-arm-absent": {node: 159, value: grammarRecord{Before: 1, Text: grammarString("XX"), After: 3}},
}

// grammarDocuments is what every row of GRAMMAR.md's "A record and a document"
// table holds. Those rows are whole documents rather than values, so what writes
// them is [Values]'s own JSON rendering and not [WriteValue].
var grammarDocuments = map[string]*Values{
	"document": {Records: []Record{{
		Name: "ORDER-RECORD",
		Value: map[string]any{
			"ORDER-ID": "A001",
			"ORDER-LINE": []any{
				map[string]any{"LINE-SKU": "SK1"},
				map[string]any{"LINE-SKU": "SK2"},
			},
		},
	}}},
	"document-no-records": {Records: []Record{}},
	"document-failure": {
		Records: []Record{{Name: "TXN", Value: map[string]any{"AMT": "1"}}},
		Failure: "the sign nibble is not one of the four the convention admits",
	},
	"record-empty-value": {Records: []Record{{Name: "TXN", Value: map[string]any{}}}},
}

// grammarForms pairs the Form column of GRAMMAR.md's "Not admissible" table with
// a descriptor that takes that form, so that a row is held to the loader through
// [formOf] rather than against a form named directly. A row whose form the
// descriptor no longer selects is a row about nothing.
var grammarForms = map[string]*irpb.Field{
	"number": {Usage: irpb.Usage_USAGE_DISPLAY, Picture: &irpb.Picture{Category: irpb.Category_CATEGORY_NUMERIC}},
	"float":  {Usage: irpb.Usage_USAGE_COMP_1},
	"bytes":  {Usage: irpb.Usage_USAGE_INDEX},
	"text":   {Usage: irpb.Usage_USAGE_DISPLAY, Picture: &irpb.Picture{Category: irpb.Category_CATEGORY_ALPHANUMERIC}},
}

func grammarNum(n int32) *int32 { return &n }

func grammarString(s string) *string { return &s }

// TestTheWriterWritesEveryGrammarRow holds this repository's writer to
// docs/conformance/GRAMMAR.md: for every row of every value table, the value
// behind it is written as the text beside it.
//
// This is the check the document exists for. A generator author in another
// language reads those tables and writes their own writer against them, so a
// table this repository's own writer does not satisfy is a table that sends them
// somewhere wrong — and the failure they would see instead is a corpus entry
// disagreeing about a record, which reads as a decoding bug and is not one
// (#197).
func TestTheWriterWritesEveryGrammarRow(t *testing.T) {
	nodes := grammarNodes(t)
	written := grammarWritten(t)

	for row, fixture := range grammarValues {
		t.Run(row, func(t *testing.T) {
			text, ok := written[row]
			if !ok {
				t.Fatalf("no row %q in GRAMMAR.md, and a value here with no row there is a value nobody was told about", row)
			}

			node, ok := nodes[fixture.node]
			if !ok {
				t.Fatalf("node %d is not in grammarDescriptor", fixture.node)
			}

			got, err := WriteValue(nodes, node, reflect.ValueOf(fixture.value))
			if err != nil {
				t.Fatalf("WriteValue: %v", err)
			}

			var want any
			if err := json.Unmarshal([]byte(text), &want); err != nil {
				t.Fatalf("the row's text %s is not JSON: %v", text, err)
			}

			// Compare is the corpus's own comparison, which is over the written
			// form: two scalars are equal when their text is, and never after
			// being decoded into a number. It is what a harness runs against a
			// runner, so it is what a row is worth checking with.
			if err := Compare(grammarOne(want), grammarOne(got)); err != nil {
				t.Errorf("the writer disagrees with GRAMMAR.md:\n%v", err)
			}

			// An object's member order is not significant and Compare says so;
			// a scalar's text is significant to the character, which is what
			// makes the comparison of two answers string equality. So a row
			// whose answer is a scalar is held to the document byte for byte.
			if _, ok := got.(string); !ok {
				return
			}

			marshalled, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshal what the writer wrote: %v", err)
			}

			if string(marshalled) != text {
				t.Errorf("the writer wrote %s and GRAMMAR.md states %s", marshalled, text)
			}
		})
	}
}

// TestTheDocumentRowsAreWhatValuesRenders holds the whole-document rows to
// [Values]'s own JSON, which is what an entry's values.json and a runner's
// answer are both written with.
func TestTheDocumentRowsAreWhatValuesRenders(t *testing.T) {
	written := grammarWritten(t)

	for row, values := range grammarDocuments {
		t.Run(row, func(t *testing.T) {
			text, ok := written[row]
			if !ok {
				t.Fatalf("no row %q in GRAMMAR.md", row)
			}

			marshalled, err := json.Marshal(values)
			if err != nil {
				t.Fatalf("marshal the document: %v", err)
			}

			var got, want any
			if err := json.Unmarshal(marshalled, &got); err != nil {
				t.Fatalf("read back what Values rendered: %v", err)
			}

			if err := json.Unmarshal([]byte(text), &want); err != nil {
				t.Fatalf("the row's text %s is not JSON: %v", text, err)
			}

			if !reflect.DeepEqual(got, want) {
				t.Errorf("Values rendered\n%s\nand GRAMMAR.md states\n%s", marshalled, text)
			}
		})
	}
}

// TestEveryGrammarRowHasAValueBehindIt is the other direction of the two tests
// above: a row somebody added to the document with nothing behind it here would
// otherwise pass forever, which is the failure a grammar corpus is least able to
// afford.
func TestEveryGrammarRowHasAValueBehindIt(t *testing.T) {
	written := grammarWritten(t)

	for row := range written {
		_, isValue := grammarValues[row]
		_, isDocument := grammarDocuments[row]

		switch {
		case isValue && isDocument:
			t.Errorf("row %q is both a value and a document, and only one of them is checked", row)
		case !isValue && !isDocument:
			t.Errorf("row %q of GRAMMAR.md has nothing behind it, so nothing holds the writer to it", row)
		}
	}
}

// TestTheLoaderAgreesWithEveryNotAdmissibleRow holds the loader to GRAMMAR.md's
// catalogue of mistakes: it refuses the spelling the row calls wrong and admits
// the one it offers instead.
//
// Both halves matter. A rule that refuses everything would pass the first half
// alone, and a document telling an author to write something the loader then
// refuses is worse than no document.
func TestTheLoaderAgreesWithEveryNotAdmissibleRow(t *testing.T) {
	rows := grammarNotAdmissible(t)

	if len(rows) == 0 {
		t.Fatal("no rows found under GRAMMAR.md's \"Not admissible\": the check would pass vacuously")
	}

	for row, cells := range rows {
		t.Run(row, func(t *testing.T) {
			field, ok := grammarForms[cells.form]
			if !ok {
				t.Fatalf("GRAMMAR.md names the form %q, which is not one of the four", cells.form)
			}

			refused := scalarSpelling(field, grammarScalar(t, cells.refused), row)
			if len(refused) == 0 {
				t.Errorf("the loader admits %s, which GRAMMAR.md says it may not", cells.refused)
			}

			admitted := scalarSpelling(field, grammarScalar(t, cells.admitted), row)
			if len(admitted) != 0 {
				t.Errorf("the loader refuses %s, which GRAMMAR.md offers instead: %v", cells.admitted, admitted)
			}
		})
	}
}

// TestTheGrammarCoversEveryUsageAndCategory is the completeness the document
// claims, checked rather than asserted.
//
// docs/conformance/SPEC.md makes the form of a value a function of usage and
// category alone, so a usage or a category no row covers is a form of value the
// table cannot be used to check. The corpus itself covers neither the edited
// categories nor INDEX, POINTER and NATIONAL — no entry has needed one — which
// is exactly why this is checked here and not there (#197).
func TestTheGrammarCoversEveryUsageAndCategory(t *testing.T) {
	nodes := grammarNodes(t)

	usages := map[irpb.Usage]bool{}
	categories := map[irpb.Category]bool{}

	for _, node := range nodes {
		field, ok := node.GetKind().(*irpb.Node_Field)
		if !ok {
			continue
		}

		usages[field.Field.GetUsage()] = true

		if picture := field.Field.GetPicture(); picture != nil {
			categories[picture.GetCategory()] = true
		}
	}

	for value, name := range irpb.Usage_name {
		if value == int32(irpb.Usage_USAGE_UNSPECIFIED) {
			continue
		}

		if !usages[irpb.Usage(value)] {
			t.Errorf("no row of GRAMMAR.md covers %s", name)
		}
	}

	for value, name := range irpb.Category_name {
		if value == int32(irpb.Category_CATEGORY_UNSPECIFIED) {
			continue
		}

		if !categories[irpb.Category(value)] {
			t.Errorf("no row of GRAMMAR.md covers %s", name)
		}
	}
}

// TestTheGrammarCoversEveryArrangementOfNodes is the same completeness for the
// shape of a value, which usage and category say nothing about.
//
// The five are what docs/conformance/SPEC.md's "A group, a table and a variant"
// and "Slack is not a value" name, and the corpus covers only the first two.
func TestTheGrammarCoversEveryArrangementOfNodes(t *testing.T) {
	nodes := grammarNodes(t)

	arrangements := map[string]bool{
		"a group":                    false,
		"a repeating node":           false,
		"a variant":                  false,
		"an arm not held":            false,
		"a slack node":               false,
		"a group holding only slack": false,
	}

	for _, node := range nodes {
		if node.GetSlack() != nil {
			arrangements["a slack node"] = true
		}

		if node.GetVariant() != nil {
			arrangements["a variant"] = true
		}

		switch kind := node.GetKind().(type) {
		case *irpb.Node_Group:
			arrangements["a group"] = true

			if kind.Group.GetRepetition() != nil {
				arrangements["a repeating node"] = true
			}
		case *irpb.Node_Field:
			if kind.Field.GetRepetition() != nil {
				arrangements["a repeating node"] = true
			}
		}
	}

	for _, fixture := range grammarValues {
		if record, ok := fixture.value.(grammarRecord); ok && (record.Num == nil || record.Text == nil) {
			arrangements["an arm not held"] = true
		}

		if _, ok := fixture.value.(grammarPad); ok {
			arrangements["a group holding only slack"] = true
		}
	}

	for arrangement, covered := range arrangements {
		if !covered {
			t.Errorf("no row of GRAMMAR.md covers %s", arrangement)
		}
	}
}

// grammarNodes is [grammarDescriptor] by node identifier.
func grammarNodes(t *testing.T) map[uint64]*irpb.Node {
	t.Helper()

	var descriptor irpb.Descriptor
	if err := protojson.Unmarshal([]byte(grammarDescriptor), &descriptor); err != nil {
		t.Fatalf("read grammarDescriptor: %v", err)
	}

	return nodesByID(&descriptor)
}

// grammarOne wraps a value as the one record of a values document, which is what
// [Compare] takes.
func grammarOne(value any) *Values {
	return &Values{Records: []Record{{Name: "ROW", Value: value}}}
}

// grammarScalar reads one cell of GRAMMAR.md's "Not admissible" table as the
// JSON it stands for, which is a string for every admissible spelling and
// deliberately not one for the two rows about writing a scalar as a number.
func grammarScalar(t *testing.T, cell string) any {
	t.Helper()

	var value any
	if err := json.Unmarshal([]byte(cell), &value); err != nil {
		t.Fatalf("the cell %s is not JSON: %v", cell, err)
	}

	return value
}

// grammarRowID is the first cell of a row of one of GRAMMAR.md's tables. A table
// row whose first cell is not one is a table this file is not about — the
// heading row, its underline, and any table of prose the document grows.
var grammarRowID = regexp.MustCompile("^`[a-z0-9-]+`$")

// grammarWritten is every row of GRAMMAR.md's three-column tables: the row's
// identifier against the exact document text beside it.
//
// The document is parsed rather than copied, for the reason it exists at all. A
// copy of the answers here would be a second statement of them, it would be the
// one the test actually checked, and the published table would be free to drift
// away from the code while every test passed.
func grammarWritten(t *testing.T) map[string]string {
	t.Helper()

	rows := map[string]string{}

	for _, line := range strings.Split(grammarText(t), "\n") {
		cells := grammarCells(line)
		if len(cells) != 3 || !grammarRowID.MatchString(cells[0]) {
			continue
		}

		rows[strings.Trim(cells[0], "`")] = strings.Trim(cells[2], "`")
	}

	if len(rows) == 0 {
		t.Fatal("no value rows found in GRAMMAR.md: every check over them would pass vacuously")
	}

	return rows
}

// grammarNotAdmissibleRow is one row of GRAMMAR.md's four-column table.
type grammarNotAdmissibleRow struct {
	form     string
	refused  string
	admitted string
}

// grammarNotAdmissible is that table, by row identifier.
func grammarNotAdmissible(t *testing.T) map[string]grammarNotAdmissibleRow {
	t.Helper()

	rows := map[string]grammarNotAdmissibleRow{}

	for _, line := range strings.Split(grammarText(t), "\n") {
		cells := grammarCells(line)
		if len(cells) != 4 || !grammarRowID.MatchString(cells[0]) {
			continue
		}

		rows[strings.Trim(cells[0], "`")] = grammarNotAdmissibleRow{
			form:     cells[1],
			refused:  strings.Trim(cells[2], "`"),
			admitted: strings.Trim(cells[3], "`"),
		}
	}

	return rows
}

// grammarCells splits a markdown table row into its cells, and returns nothing
// for a line that is not one.
func grammarCells(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
		return nil
	}

	var cells []string
	for _, cell := range strings.Split(strings.Trim(line, "|"), "|") {
		cells = append(cells, strings.TrimSpace(cell))
	}

	return cells
}

// grammarText is docs/conformance/GRAMMAR.md.
func grammarText(t *testing.T) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "conformance", "GRAMMAR.md"))
	if err != nil {
		t.Fatalf("read the grammar corpus: %v", err)
	}

	return string(b)
}
