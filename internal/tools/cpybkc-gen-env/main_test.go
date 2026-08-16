// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// These tests hold this generator to the half of the plugin contract it
// implements, because a fixture that is wrong about the contract fails the check
// it exists for and blames the thing under test. The companion module's
// with-env-variable is what that check is about (#252), and a run that failed
// here would read as *the variable did not reach the generator* whichever of the
// two was at fault.

package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// vector is the invocation cpybkc emits, with the two paths filled in.
func vector(descriptor, out string, options ...string) []string {
	args := []string{descriptorFlag, descriptor, outFlag, out}
	for _, option := range options {
		args = append(args, optFlag, option)
	}

	return args
}

func TestRunWritesTheVariableItWasStartedWith(t *testing.T) {
	const (
		variable = "SOURCE_DATE_EPOCH"
		want     = "1700000000"
	)

	t.Setenv(variable, want)

	out := t.TempDir()

	var diagnostics strings.Builder
	if err := run(vector("/descriptor", out, variableOption+"="+variable), &diagnostics); err != nil {
		t.Fatalf("run over a set %s = %v, want no error", variable, err)
	}

	got, err := os.ReadFile(filepath.Join(out, valueFile))
	if err != nil {
		t.Fatalf("reading what the generator wrote: %v", err)
	}

	// Byte-exact, with no trailing newline: the check driving this compares the
	// file against the value it set, and a newline nobody asked for would make
	// that comparison a rule about this fixture rather than about the variable.
	if string(got) != want {
		t.Errorf("the generator wrote %q, want %q", got, want)
	}

	// The value on stderr as well, since that is where the contract puts a
	// generator's explanation and where cpybkc surfaces it attributed. A failing
	// end-to-end check should say what arrived without anybody exporting a
	// directory.
	if diagnostic := diagnostics.String(); !strings.Contains(diagnostic, variable+"="+want) {
		t.Errorf("the generator's diagnostics were %q, which do not report %s=%s", diagnostic, variable, want)
	}
}

func TestRunRefusesAVariableThatDidNotArrive(t *testing.T) {
	const variable = "CPYBKC_GEN_ENV_UNSET_IN_THIS_TEST"

	// Not merely unset: an empty value is a value, and this generator has to
	// tell "set to nothing" from "not set at all" — the second is the failure
	// the module's three hops are checked for.
	if _, ok := os.LookupEnv(variable); ok {
		t.Fatalf("%s is set in this test's environment, which is the one thing this case needs it not to be", variable)
	}

	err := run(vector("/descriptor", t.TempDir(), variableOption+"="+variable), io.Discard)
	if err == nil {
		t.Fatalf("run over an unset %s = nil, want an error", variable)
	}

	if !strings.Contains(err.Error(), variable) {
		t.Errorf("the refusal is %q, which does not name the variable that was missing", err)
	}
}

func TestRunAcceptsAVariableSetToNothing(t *testing.T) {
	const variable = "CPYBKC_GEN_ENV_EMPTY"

	t.Setenv(variable, "")

	out := t.TempDir()
	if err := run(vector("/descriptor", out, variableOption+"="+variable), io.Discard); err != nil {
		t.Fatalf("run over an empty %s = %v, want no error: an exported variable with no value is set", variable, err)
	}

	got, err := os.ReadFile(filepath.Join(out, valueFile))
	if err != nil {
		t.Fatalf("reading what the generator wrote: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("the generator wrote %q for a variable set to nothing, want an empty file", got)
	}
}

func TestParseRefusesWhatTheContractDoesNotAllow(t *testing.T) {
	for _, tc := range []struct {
		testName string
		args     []string
		// want is a substring of the refusal, and it is the fact rather than the
		// wording.
		want string
	}{
		{
			testName: "no descriptor",
			args:     []string{outFlag, "/out", optFlag, variableOption + "=X"},
			want:     descriptorFlag,
		},
		{
			testName: "no out",
			args:     []string{descriptorFlag, "/descriptor", optFlag, variableOption + "=X"},
			want:     outFlag,
		},
		{
			testName: "no variable option",
			args:     vector("/descriptor", "/out"),
			want:     variableOption,
		},
		{
			// The contract's own requirement, and the one a fixture is most
			// tempted to skip: a plugin MUST fail on a key it does not
			// recognise rather than ignoring it.
			testName: "an option key it does not recognise",
			args:     vector("/descriptor", "/out", variableOption+"=X", "package_name=ledger"),
			want:     "package_name",
		},
		{
			testName: "an option that is not k=v",
			args:     vector("/descriptor", "/out", "variable"),
			want:     "k=v",
		},
		{
			// Distinct from the option being absent: it was written, and what
			// it names is nothing. A message saying it is required would send
			// its reader looking for a line that is in front of them.
			testName: "the variable option with an empty value",
			args:     vector("/descriptor", "/out", variableOption+"="),
			want:     "names no variable",
		},
		{
			// The arity the messages claim, now enforced. cpybkc emits each of
			// these exactly once, and a fixture that quietly kept the last of
			// several would be wrong about the contract it exists to be right
			// about.
			testName: "a repeated out",
			args:     []string{descriptorFlag, "/descriptor", outFlag, "/a", outFlag, "/b"},
			want:     "exactly once",
		},
		{
			testName: "a repeated descriptor",
			args:     []string{descriptorFlag, "/a", descriptorFlag, "/b", outFlag, "/out"},
			want:     "exactly once",
		},
		{
			// The one the arity check used to answer wrongly: an argument this
			// program does not take, in final position, read as a flag missing
			// its value. The message has to name the typo rather than send its
			// reader looking for a value.
			testName: "an unrecognised argument in final position",
			args:     append(vector("/descriptor", "/out", variableOption+"=X"), "--bogus"),
			want:     "unrecognised argument",
		},
		{
			// The joined form, which a plugin MAY accept and cpybkc never
			// emits. Refusing it keeps this fixture covering exactly the
			// spelling the thing it checks produces.
			testName: "the joined spelling",
			args:     []string{outFlag + "=/out", descriptorFlag, "/descriptor"},
			want:     "unrecognised argument",
		},
		{
			testName: "a flag with no value after it",
			args:     []string{descriptorFlag},
			want:     descriptorFlag,
		},
		{
			testName: "operands after the delimiter",
			args:     append(vector("/descriptor", "/out", variableOption+"=X"), endOfOptions, "extra"),
			want:     "operands",
		},
	} {
		t.Run(tc.testName, func(t *testing.T) {
			_, err := parse(tc.args)
			if err == nil {
				t.Fatalf("parse(%q) = nil, want an error", tc.args)
			}

			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("parse(%q) = %q, which does not mention %q", tc.args, err, tc.want)
			}
		})
	}
}

func TestParseAcceptsTheDelimiterWithNothingAfterIt(t *testing.T) {
	// `--` ends the options and cpybkc passes no operands; a plugin has to treat
	// it as the end rather than as an argument, so that one invoked by hand
	// behaves the way its neighbours do.
	args := append(vector("/descriptor", "/out", variableOption+"=SOURCE_DATE_EPOCH"), endOfOptions)

	parsed, err := parse(args)
	if err != nil {
		t.Fatalf("parse(%q) = %v, want no error", args, err)
	}

	if parsed.out != "/out" || parsed.variable != "SOURCE_DATE_EPOCH" {
		t.Errorf("parse(%q) = %+v, want the out directory and the variable it was given", args, parsed)
	}
}
