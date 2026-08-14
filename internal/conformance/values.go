// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/Zaba505/cpybkc/irpb"
)

// Values is what a file's bytes decode to: the records in the order they come
// out of the file, and whether reading stopped at a failure.
//
// It is both halves of the comparison — what an entry's values.json states and
// what a runner writes on standard output — because a runner and an entry that
// spoke different dialects would need a translation nobody could test. See
// docs/conformance/SPEC.md, "What a runner does", for the whole of the
// contract this type is the Go reading of.
type Values struct {
	// Records are the records read, in file order.
	Records []Record `json:"records"`

	// Failure is present where reading stopped at a record the file does not
	// carry correctly, and absent where the file was read to its end.
	//
	// The text is a note for whoever reads the report, and it is deliberately
	// not compared: a diagnostic is a generator's own wording in its own
	// language, so an entry demanding particular words would be an entry only
	// one generator could pass. What is compared is that a failure happened,
	// and that it happened after the records the entry lists.
	Failure string `json:"failure,omitempty"`
}

// Record is one record of a file: which record type it is, and what it holds.
type Record struct {
	// Name is the record's name as the copybook spells it — the `original` of
	// the record node's names, never an identifier munged from it.
	Name string `json:"name"`

	// Value is what the record's top-level node holds, in the shape
	// docs/conformance/SPEC.md's "The value language" describes: an object for
	// a group, an array for an item that repeats, and a scalar for an
	// elementary item.
	Value any `json:"value"`
}

// ParseValues reads a values document, holding it to the shape the corpus
// format states.
//
// Unknown fields are refused at both levels, for the reason entry.json refuses
// one: a key an author wrote in the expectation that it means something is a
// typo, and a document that silently ignores it is a document that passes for
// the wrong reason.
func ParseValues(b []byte) (*Values, error) {
	var values Values
	if err := decodeOne(b, &values); err != nil {
		return nil, err
	}

	if faults := values.records(); len(faults) > 0 {
		return nil, joined(faults)
	}

	return &values, nil
}

// records is what is wrong with the records of a values document, which is
// everything the format asks of one beyond it being JSON.
//
// It is separate from [ParseValues] because a values document is read in two
// places: on its own, as an entry's values.json, and as a member of the answer
// document a runner writes (see [Answer]). Both hold it to this.
func (v *Values) records() []error {
	var faults []error

	// Counted from one, as [Compare] counts and as the driver counts: a record
	// is named to whoever is reading a values document beside the file it came
	// from, and two numbering conventions in one report is one of them being
	// off by one.
	for i, record := range v.Records {
		if record.Name == "" {
			faults = append(faults, fmt.Errorf("record %d carries no name", i+1))
		}

		if record.Value == nil {
			faults = append(faults, fmt.Errorf("record %d (%s) carries no value", i+1, record.Name))
		}
	}

	return faults
}

// decodeOne reads exactly one JSON document out of b, and is how every JSON
// member of an entry and every runner's answer is read.
//
// Two things beyond decoding. An unknown field is refused, for the reason the
// project manifest refuses one: a key an author wrote in the expectation that it
// means something is a typo, and a document that ignores it passes for the wrong
// reason. And anything behind the first document is refused, because
// encoding/json stops at the end of a value and reports nothing about what
// follows — so a file with a second document appended, or with a paste that went
// wrong at the end of it, would be read as its first half and pass on values
// nobody meant.
func decodeOne(b []byte, document any) error {
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(document); err != nil {
		return err
	}

	if decoder.More() {
		return fmt.Errorf("there is more than one document in the file, and only the first was read")
	}

	return nil
}

// check holds a values document to the descriptor of the entry it belongs to.
//
// What it checks is the record names and the spelling of every scalar it can
// reach, and neither the shape of a value nor what a value says. A pass holding
// the document's *shape* to the node tree would be a second reading of what a
// descriptor means — the reading the generated code already performs, and the
// one thing the corpus exists to compare answers about — so a value of the
// wrong shape is left to be reported as the disagreement it is, by [Compare],
// against a runner that actually decoded the bytes.
//
// A name is worth checking here because it is the one part of a values document
// that no runner can disagree with: the names come out of the descriptor, so an
// entry naming a record the descriptor does not carry is a typo that would
// otherwise be reported as every record in the file being unexpected. A
// spelling is worth checking for the same reason and is [Values.spellings],
// which says at length why it walks the tree where this does not (#196).
func (v *Values) check(descriptor *irpb.Descriptor) error {
	names := recordNames(descriptor)

	var faults []error

	for _, record := range v.Records {
		if !slices.Contains(names, record.Name) {
			faults = append(faults, fmt.Errorf("%s: %s is not a record the descriptor carries; it carries %s",
				ValuesName, record.Name, strings.Join(names, ", ")))
		}
	}

	faults = append(faults, v.spellings(descriptor)...)

	return joined(faults)
}

// recordNames is the copybook name of every record type the descriptor carries,
// in node order.
func recordNames(descriptor *irpb.Descriptor) []string {
	var names []string

	for _, node := range descriptor.GetNodes() {
		if record := node.GetRecord(); record != nil {
			names = append(names, record.GetNames().GetOriginal())
		}
	}

	return names
}

// Compare reports every way in which got differs from want.
//
// The comparison is structural and knows no COBOL: both sides have already been
// written in the corpus's own value language, where a number is a decimal
// string and a run of bytes is base64, precisely so that comparing them needs
// neither the descriptor nor a decoder. What that buys is that a runner for a
// language this repository has never seen is compared by exactly this function.
//
// Every difference is reported rather than the first, each naming the path
// through the record it is at, because a generator that gets one axis of the
// encoding wrong gets every field along that axis wrong at once and a report
// naming one of them sends its author looking for a single-field bug.
func Compare(want, got *Values) error {
	var faults []error

	switch {
	case want.Failure != "" && got.Failure == "":
		faults = append(faults, fmt.Errorf("the file was read to its end, and the entry expects it to fail: %s", want.Failure))
	case want.Failure == "" && got.Failure != "":
		faults = append(faults, fmt.Errorf("reading the file failed, and the entry expects it not to: %s", got.Failure))
	}

	if len(want.Records) != len(got.Records) {
		faults = append(faults, fmt.Errorf("the file holds %d records and the entry expects %d",
			len(got.Records), len(want.Records)))
	}

	for i := range min(len(want.Records), len(got.Records)) {
		wantRecord, gotRecord := want.Records[i], got.Records[i]

		path := fmt.Sprintf("record %d", i+1)

		if wantRecord.Name != gotRecord.Name {
			faults = append(faults, at(path, fmt.Errorf("%s is a %s and the entry expects a %s",
				path, gotRecord.Name, wantRecord.Name)))

			continue
		}

		faults = append(faults, difference(path+" "+wantRecord.Name, wantRecord.Value, gotRecord.Value)...)
	}

	return joined(faults)
}

// difference is the recursive half of [Compare]: everything at or beneath path
// that got says differently from want.
func difference(path string, want, got any) []error {
	switch want := want.(type) {
	case map[string]any:
		gotObject, ok := got.(map[string]any)
		if !ok {
			return []error{at(path, fmt.Errorf("%s: %s, and the entry expects a group", path, described(got)))}
		}

		return objectDifference(path, want, gotObject)
	case []any:
		gotArray, ok := got.([]any)
		if !ok {
			return []error{at(path, fmt.Errorf("%s: %s, and the entry expects %d occurrences", path, described(got), len(want)))}
		}

		return arrayDifference(path, want, gotArray)
	default:
		// A group or a table where the entry expects one value is reported as
		// the shape it is rather than as a value that differs: the two send a
		// reader to different places, one to the bytes and the other to whether
		// the item repeats at all.
		switch got.(type) {
		case map[string]any, []any:
			return []error{at(path, fmt.Errorf("%s: %s, and the entry expects %s", path, described(got), rendered(want)))}
		}

		// String equality over the written form, and deliberately not Go's !=
		// on the two decoded values: != on an any holding a float64 is IEEE
		// equality, under which a negative zero equals a positive one, so a
		// generator that lost the sign of a zero would pass. Every scalar of
		// the value language is a JSON string — a float included, in the
		// hexadecimal form [FormatFloat] writes — precisely so that this
		// comparison can be the one docs/conformance/SPEC.md's "Comparison is
		// over the written form" requires, and rendering both sides keeps that
		// true of anything a document carries that is not one (#194, #195).
		if rendered(want) != rendered(got) {
			return []error{at(path, fmt.Errorf("%s is %s and the entry expects %s", path, rendered(got), rendered(want)))}
		}

		return nil
	}
}

// at says where in the values document a disagreement is, in a form a caller
// holding the descriptor can act on. See [PathError], which is the whole of why
// it exists; the message is untouched.
func at(path string, err error) error {
	return &PathError{Path: path, Err: err}
}

// objectDifference compares two groups, member by member.
//
// A key present on one side and absent on the other is reported as that rather
// than as a value being wrong, because the two mean different things: an absent
// key is the arm of a variant an occurrence does not hold, and a key nobody
// expects is an item a generator surfaced that the entry does not describe.
func objectDifference(path string, want, got map[string]any) []error {
	var faults []error

	for _, name := range slices.Sorted(maps.Keys(want)) {
		gotValue, ok := got[name]
		if !ok {
			faults = append(faults, at(path+"."+name, fmt.Errorf("%s: %s is not there, and the entry expects %s",
				path, name, rendered(want[name]))))

			continue
		}

		faults = append(faults, difference(path+"."+name, want[name], gotValue)...)
	}

	for _, name := range slices.Sorted(maps.Keys(got)) {
		if _, ok := want[name]; !ok {
			faults = append(faults, at(path+"."+name, fmt.Errorf("%s: %s is %s, and the entry does not expect it at all",
				path, name, rendered(got[name]))))
		}
	}

	return faults
}

// arrayDifference compares two runs of occurrences, occurrence by occurrence.
func arrayDifference(path string, want, got []any) []error {
	var faults []error

	if len(want) != len(got) {
		faults = append(faults, at(path, fmt.Errorf("%s holds %d occurrences and the entry expects %d",
			path, len(got), len(want))))
	}

	for i := range min(len(want), len(got)) {
		faults = append(faults, difference(fmt.Sprintf("%s[%d]", path, i), want[i], got[i])...)
	}

	return faults
}

// rendered is a value as a report should quote it: the JSON it was written as,
// so that a string and the decimal string of a number are told apart by the
// quotes around them.
//
// It is also the comparison, since [difference] compares two scalars by what
// each side wrote rather than by Go equality, so it carries an obligation a
// helper that only composed prose would not: **distinct values must render
// distinctly.** json.Marshal is what gives that — it writes the JSON kind along
// with the content, so a string never renders as a number, a number never as a
// bool, and nothing is elided or truncated — and
// TestRenderedTellsTheScalarKindsApart is what holds it there, so that a change
// made to this function for readability cannot quietly turn half the corpus
// off. A change here that loses the quotes, lowercases, or shortens a long
// value is a change that makes two different answers agree.
//
// The %v fallback is not injective and does not need to be: json.Marshal
// refuses only values encoding/json cannot have produced in the first place —
// a NaN or an infinity as a Go float64 — and every value reaching this function
// came out of [ParseValues].
func rendered(value any) string {
	b, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}

	return string(b)
}

// described says what kind of thing a value is, for the report that one side is
// not the shape the other is.
func described(value any) string {
	switch value := value.(type) {
	case map[string]any:
		return "it is a group"
	case []any:
		return fmt.Sprintf("it holds %d occurrences", len(value))
	case nil:
		return "it is not there"
	default:
		return "it is " + rendered(value)
	}
}
