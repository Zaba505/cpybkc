// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"strings"
	"testing"
)

// TestParseReadsTheVectorCpybkcEmits is the argument vector docs/plugin/SPEC.md
// fixes, in the separated form cpybkc MUST emit and a plugin MUST accept.
func TestParseReadsTheVectorCpybkcEmits(t *testing.T) {
	t.Parallel()

	inv, err := parse([]string{
		descriptorFlag, "/tmp/one/descriptor.binpb",
		outFlag, "/tmp/two",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if inv.descriptor != "/tmp/one/descriptor.binpb" {
		t.Errorf("the descriptor is %q", inv.descriptor)
	}

	if inv.out != "/tmp/two" {
		t.Errorf("--out is %q", inv.out)
	}
}

// TestParseDefaultsEveryOptionTheVectorDoesNotState is the vocabulary from the
// side a manifest that names none sees. Neither option is required, so a vector
// carrying no `--opt` at all is a run rather than a refusal, and what it runs is
// the two defaults.
func TestParseDefaultsEveryOptionTheVectorDoesNotState(t *testing.T) {
	t.Parallel()

	inv, err := parse([]string{descriptorFlag, "/tmp/one", outFlag, "/tmp/two"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if inv.options.format != defaultFormat {
		t.Errorf("%s defaulted to %q, want %q", formatOption, inv.options.format, defaultFormat)
	}

	if inv.options.records != defaultRecords {
		t.Errorf("%s defaulted to %q, want %q", recordsOption, inv.options.records, defaultRecords)
	}
}

// TestParseReadsEveryValueEachOptionAdmits walks both closed sets, because an
// option whose default is read correctly and whose other value is not is an
// option that only appears to work.
func TestParseReadsEveryValueEachOptionAdmits(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		key    string
		value  string
		stated func(options) string
	}{
		{key: formatOption, value: formatMermaid, stated: func(o options) string { return o.format }},
		{key: formatOption, value: formatDot, stated: func(o options) string { return o.format }},
		{key: recordsOption, value: recordsAll, stated: func(o options) string { return o.records }},
		{key: recordsOption, value: recordsNone, stated: func(o options) string { return o.records }},
	}

	for _, testCase := range testCases {
		t.Run(testCase.key+"="+testCase.value, func(t *testing.T) {
			t.Parallel()

			inv, err := parse([]string{
				descriptorFlag, "/tmp/one",
				outFlag, "/tmp/two",
				optFlag, testCase.key + "=" + testCase.value,
			})
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			if got := testCase.stated(inv.options); got != testCase.value {
				t.Errorf("%s is %q, want %q", testCase.key, got, testCase.value)
			}
		})
	}
}

// TestParseAcceptsTheStandardInputForm covers `--descriptor -`, which cpybkc
// never emits and a plugin MUST accept.
func TestParseAcceptsTheStandardInputForm(t *testing.T) {
	t.Parallel()

	inv, err := parse([]string{descriptorFlag, standardInput, outFlag, "/tmp/two"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if inv.descriptor != standardInput {
		t.Errorf("the descriptor is %q, want %q", inv.descriptor, standardInput)
	}
}

// TestParseAcceptsTheEndOfOptionsDelimiter holds the POSIX delimiter this
// contract defines no operands for. cpybkc passes none; a person invoking this
// generator by hand should still find it behaving like its neighbours.
func TestParseAcceptsTheEndOfOptionsDelimiter(t *testing.T) {
	t.Parallel()

	if _, err := parse([]string{
		descriptorFlag, "/tmp/one",
		outFlag, "/tmp/two",
		optFlag, formatOption + "=" + formatDot,
		endOfOptions,
	}); err != nil {
		t.Errorf("parse refused a vector ending in %s: %v", endOfOptions, err)
	}
}

// TestParseKeepsAnOptionValueWhole covers the two shapes of value the contract
// admits and a naive split would lose: a value carrying further `=` characters,
// and an empty one. Neither is a value this generator's vocabulary takes, so
// both are refused — by the option that owns them, and by their whole text
// rather than by the part before the second `=`.
func TestParseKeepsAnOptionValueWhole(t *testing.T) {
	t.Parallel()

	var o options

	if err := o.set(formatOption + "=a=b=c"); err == nil {
		t.Error("a format outside the closed set was accepted")
	} else if !strings.Contains(err.Error(), "a=b=c") {
		t.Errorf("the fault reads %q and not the whole value it is about", err)
	}

	if err := o.set(formatOption + "="); err == nil {
		t.Error("an empty format was accepted")
	}
}

// TestAnUnrecognisedOptionNamesTheVocabulary is the rule docs/plugin/SPEC.md
// makes the whole mechanism rather than a backstop: cpybkc validates no key,
// because no plugin declares a vocabulary, so a key this generator does not
// recognise is a failure here or nowhere.
func TestAnUnrecognisedOptionNamesTheVocabulary(t *testing.T) {
	t.Parallel()

	var o options

	err := o.set("layout_name=orders")
	if err == nil {
		t.Fatal("an unrecognised key was accepted")
	}

	for _, want := range []string{"layout_name", formatOption, recordsOption} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the fault reads %q and does not name %q", err, want)
		}
	}
}

// TestAValueOutsideAClosedSetIsNamedWithTheSetItIsNotIn keeps a rejected value
// actionable. The sets are two words each, so a diagnostic that named neither
// would leave the reader guessing at a spelling.
func TestAValueOutsideAClosedSetIsNamedWithTheSetItIsNotIn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		key      string
		value    string
		admitted []string
	}{
		{key: formatOption, value: "graphviz", admitted: []string{formatMermaid, formatDot}},
		{key: recordsOption, value: "some", admitted: []string{recordsAll, recordsNone}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.key, func(t *testing.T) {
			t.Parallel()

			var o options

			err := o.set(testCase.key + "=" + testCase.value)
			if err == nil {
				t.Fatalf("%s=%s was accepted", testCase.key, testCase.value)
			}

			for _, want := range append([]string{testCase.key, testCase.value}, testCase.admitted...) {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the fault reads %q and does not name %q", err, want)
				}
			}
		})
	}
}

// TestParseRefusesAVectorThisContractCannotProduce keeps every fault a fault.
//
// docs/plugin/SPEC.md builds this vector from a manifest cpybkc has already
// checked, so none of these is an adopter's typo — but a plugin is entitled to
// be run by hand and by something that is not this version of cpybkc, and an
// option silently ignored is a line in a checked-in manifest that reads as
// configuration and does nothing.
func TestParseRefusesAVectorThisContractCannotProduce(t *testing.T) {
	t.Parallel()

	descriptor := []string{descriptorFlag, "/tmp/one"}
	out := []string{outFlag, "/tmp/two"}
	format := []string{optFlag, formatOption + "=" + formatDot}
	records := []string{optFlag, recordsOption + "=" + recordsNone}

	testCases := []struct {
		name string
		args []string
	}{
		{name: "nothing at all", args: nil},
		{name: "no descriptor", args: join(out)},
		{name: "no out", args: join(descriptor)},
		{name: "the descriptor twice", args: join(descriptor, descriptor, out)},
		{name: "out twice", args: join(descriptor, out, out)},
		{name: "an empty descriptor", args: join([]string{descriptorFlag, ""}, out)},
		{name: "an empty out", args: join(descriptor, []string{outFlag, ""})},
		{name: "a flag with no value", args: join(out, []string{descriptorFlag})},
		{name: "an unrecognised flag", args: join(descriptor, out, []string{"--verbose", "yes"})},
		{name: "the joined form cpybkc never emits", args: join([]string{descriptorFlag + "=/tmp/one"}, out)},
		{name: "an operand", args: join(descriptor, out, []string{"orders.cpy"})},
		{name: "an operand after the delimiter", args: join(descriptor, out, []string{endOfOptions, "orders.cpy"})},
		{name: "an option that is not k=v", args: join(descriptor, out, []string{optFlag, formatOption})},
		{name: "an option with no key", args: join(descriptor, out, []string{optFlag, "=" + formatDot})},
		{name: "an unrecognised option", args: join(descriptor, out, []string{optFlag, "direction=LR"})},
		{name: "the format twice", args: join(descriptor, out, format, format)},
		{name: "the records twice", args: join(descriptor, out, records, records)},
		{name: "a format outside the closed set", args: join(descriptor, out, []string{optFlag, formatOption + "=svg"})},
		{name: "a records outside the closed set", args: join(descriptor, out, []string{optFlag, recordsOption + "=first"})},
		{name: "a format that is the default spelled wrong", args: join(descriptor, out, []string{optFlag, formatOption + "=Mermaid"})},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if inv, err := parse(testCase.args); err == nil {
				t.Errorf("parse accepted %v as %+v", testCase.args, inv)
			}
		})
	}
}

// join is one argument vector out of several fragments.
func join(fragments ...[]string) []string {
	var args []string
	for _, fragment := range fragments {
		args = append(args, fragment...)
	}

	return args
}
