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
		optFlag, packageNameOption + "=orders",
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

	if inv.options.packageName != "orders" {
		t.Errorf("%s is %q", packageNameOption, inv.options.packageName)
	}

	// The receiver is optional, so a vector that names none is a vector that
	// takes the default rather than one that fails.
	if inv.options.receiverName() != defaultReceiver {
		t.Errorf("%s is %q, want %q", receiverOption, inv.options.receiverName(), defaultReceiver)
	}
}

// TestParseReadsTheReceiver is the one option that changes the generated
// source without changing what it does.
//
// Go's convention for a receiver is a shop's rather than a generator's — one
// letter, the same letter on every method of a type — so it is stated in the
// manifest rather than derived from each record's own name, which would give
// one package a different receiver per type.
func TestParseReadsTheReceiver(t *testing.T) {
	t.Parallel()

	inv, err := parse([]string{
		descriptorFlag, "/tmp/one",
		outFlag, "/tmp/two",
		optFlag, packageNameOption + "=orders",
		optFlag, receiverOption + "=o",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if inv.options.receiverName() != "o" {
		t.Errorf("%s is %q, want o", receiverOption, inv.options.receiverName())
	}
}

// TestParseAcceptsTheStandardInputForm covers `--descriptor -`, which cpybkc
// never emits and a plugin MUST accept.
func TestParseAcceptsTheStandardInputForm(t *testing.T) {
	t.Parallel()

	inv, err := parse([]string{descriptorFlag, standardInput, outFlag, "/tmp/two", optFlag, packageNameOption + "=orders"})
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
		optFlag, packageNameOption + "=orders",
		endOfOptions,
	}); err != nil {
		t.Errorf("parse refused a vector ending in %s: %v", endOfOptions, err)
	}
}

// TestParseKeepsAnOptionValueWhole covers the two shapes of value the contract
// admits and a naive split would lose: a value carrying further `=` characters,
// and an empty one.
func TestParseKeepsAnOptionValueWhole(t *testing.T) {
	t.Parallel()

	var o options

	if err := o.set("nowhere=a=b=c"); err == nil {
		t.Error("an unrecognised key was accepted")
	} else if !strings.Contains(err.Error(), "nowhere") {
		t.Errorf("the fault names %q and not the key it is about", err)
	}

	// An empty value reaches the option that owns it and is refused there,
	// because a package name is not a thing that may be empty — not because the
	// vector could not carry one.
	if err := o.set(packageNameOption + "="); err == nil {
		t.Error("an empty package name was accepted")
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
	pkg := []string{optFlag, packageNameOption + "=orders"}
	receiver := []string{optFlag, receiverOption + "=o"}

	testCases := []struct {
		name string
		args []string
	}{
		{name: "nothing at all", args: nil},
		{name: "no descriptor", args: join(out, pkg)},
		{name: "no out", args: join(descriptor, pkg)},
		{name: "no package name", args: join(descriptor, out)},
		{name: "the descriptor twice", args: join(descriptor, descriptor, out, pkg)},
		{name: "out twice", args: join(descriptor, out, out, pkg)},
		{name: "the package name twice", args: join(descriptor, out, pkg, pkg)},
		{name: "an empty descriptor", args: join([]string{descriptorFlag, ""}, out, pkg)},
		{name: "an empty out", args: join(descriptor, []string{outFlag, ""}, pkg)},
		{name: "a flag with no value", args: join(out, pkg, []string{descriptorFlag})},
		{name: "an unrecognised flag", args: join(descriptor, out, pkg, []string{"--verbose", "yes"})},
		{name: "the joined form cpybkc never emits", args: join([]string{descriptorFlag + "=/tmp/one"}, out, pkg)},
		{name: "an operand", args: join(descriptor, out, pkg, []string{"orders.cpy"})},
		{name: "an operand after the delimiter", args: join(descriptor, out, pkg, []string{endOfOptions, "orders.cpy"})},
		{name: "an option that is not k=v", args: join(descriptor, out, []string{optFlag, packageNameOption})},
		{name: "an option with no key", args: join(descriptor, out, []string{optFlag, "=orders"})},
		{name: "an unrecognised option", args: join(descriptor, out, pkg, []string{optFlag, "method_prefix=Read"})},
		{name: "the receiver twice", args: join(descriptor, out, pkg, receiver, receiver)},
		{name: "a receiver that is exported", args: join(descriptor, out, pkg, []string{optFlag, receiverOption + "=X"})},
		{name: "a receiver that is blank", args: join(descriptor, out, pkg, []string{optFlag, receiverOption + "=_"})},
		{name: "a receiver that is not an identifier", args: join(descriptor, out, pkg, []string{optFlag, receiverOption + "=the record"})},
		{name: "a package name that is a keyword", args: join(descriptor, out, []string{optFlag, packageNameOption + "=range"})},
		{name: "a package name that is blank", args: join(descriptor, out, []string{optFlag, packageNameOption + "=_"})},
		{name: "a package name that is init", args: join(descriptor, out, []string{optFlag, packageNameOption + "=init"})},
		{name: "a package name that is not an identifier", args: join(descriptor, out, []string{optFlag, packageNameOption + "=order records"})},
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
