// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"strings"
	"testing"
)

// TestParseReadsTheWholeVector is the argument vector docs/cli/SPEC.md fixes,
// in the separated form, with every flag a run may carry.
func TestParseReadsTheWholeVector(t *testing.T) {
	t.Parallel()

	inv, err := parse([]string{
		manifestFlag, "projects/orders/cpybkc.json",
		emitIRFlag, "orders.json",
		emitIRFormatFlag, jsonFormat,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if inv.answer != answerRun {
		t.Errorf("the line asks for answer %d, want a run", inv.answer)
	}

	if inv.manifestPath() != "projects/orders/cpybkc.json" {
		t.Errorf("the manifest is %q", inv.manifestPath())
	}

	if inv.emitIR != "orders.json" {
		t.Errorf("%s is %q", emitIRFlag, inv.emitIR)
	}

	if inv.emitIRFormat != jsonFormat {
		t.Errorf("%s is %q, want %s", emitIRFormatFlag, inv.emitIRFormat, jsonFormat)
	}
}

// TestParseAcceptsTheJoinedForm covers --manifest=cpybkc.json.
//
// Both spellings are required here where the plugin contract requires exactly
// one, and the difference is who is typing: that vector is built by a program,
// this one by a person who has already learned both spellings from every other
// tool on their machine.
func TestParseAcceptsTheJoinedForm(t *testing.T) {
	t.Parallel()

	inv, err := parse([]string{
		manifestFlag + "=projects/orders/cpybkc.json",
		emitIRFlag + "=" + standardOutput,
		emitIRFormatFlag + "=" + binaryFormat,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if inv.manifestPath() != "projects/orders/cpybkc.json" {
		t.Errorf("the manifest is %q", inv.manifestPath())
	}

	if inv.emitIR != standardOutput {
		t.Errorf("%s is %q, want %q", emitIRFlag, inv.emitIR, standardOutput)
	}
}

// TestParseAcceptsAValueCarryingAnEquals holds the joined form to splitting on
// the first = only, so that a path or a format containing one arrives whole.
func TestParseAcceptsAValueCarryingAnEquals(t *testing.T) {
	t.Parallel()

	inv, err := parse([]string{manifestFlag + "=odd=name/cpybkc.json"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if inv.manifestPath() != "odd=name/cpybkc.json" {
		t.Errorf("the manifest is %q", inv.manifestPath())
	}
}

// TestParseDefaultsTheManifestToTheWorkingDirectory is docs/cli/SPEC.md's
// discovery rule: with no --manifest, cpybkc reads cpybkc.json in the working
// directory and looks nowhere else. There is no upward search, so the default
// is a file name rather than the start of one.
func TestParseDefaultsTheManifestToTheWorkingDirectory(t *testing.T) {
	t.Parallel()

	inv, err := parse(nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if inv.manifestPath() != defaultManifest {
		t.Errorf("the manifest is %q, want %s", inv.manifestPath(), defaultManifest)
	}

	if inv.manifest != "" {
		t.Errorf("the line named the manifest %q, and it named none", inv.manifest)
	}
}

// TestParseDefaultsTheEmissionToBinary covers the other default: binary is the
// form the rest of the system uses, and the debug form is the one you ask for
// by name.
func TestParseDefaultsTheEmissionToBinary(t *testing.T) {
	t.Parallel()

	inv, err := parse([]string{emitIRFlag, "orders.binpb"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if inv.emitIRFormat != binaryFormat {
		t.Errorf("%s is %q, want %s", emitIRFormatFlag, inv.emitIRFormat, binaryFormat)
	}

	if !inv.emitting() {
		t.Error("the line asks for an emission and the invocation says it does not")
	}
}

// TestParseHonoursTheEndOfOptions covers `--` with nothing after it. cpybkc
// defines no operand, so the delimiter can only ever be followed by a usage
// error; it is honoured anyway, as a courtesy to muscle memory.
func TestParseHonoursTheEndOfOptions(t *testing.T) {
	t.Parallel()

	inv, err := parse([]string{manifestFlag, "cpybkc.json", endOfOptions})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if inv.manifestPath() != "cpybkc.json" {
		t.Errorf("the manifest is %q", inv.manifestPath())
	}
}

// TestParseRefusesTheVector is every way docs/cli/SPEC.md says a vector can
// fail to be understood. Each is status 2, and each is a fault decided with
// nothing opened.
func TestParseRefusesTheVector(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		args []string
		says string
	}{
		{
			name: "an unrecognised flag",
			args: []string{"--out", "gen"},
			says: "--out",
		},
		{
			name: "a flag this document refuses by name",
			args: []string{"--verbose"},
			says: "--verbose",
		},
		{
			name: "a single-hyphen synonym nothing documents",
			args: []string{"-manifest", "cpybkc.json"},
			says: "-manifest",
		},
		{
			name: "an operand",
			args: []string{"cpybkc.json"},
			says: "no operand",
		},
		{
			name: "an operand after the end of options",
			args: []string{endOfOptions, "cpybkc.json"},
			says: "no operand",
		},
		{
			name: "a lone dash, which POSIX makes an operand",
			args: []string{standardOutput},
			says: "no operand",
		},
		{
			name: "an operand between two flags",
			args: []string{manifestFlag, "cpybkc.json", "generate"},
			says: "no operand",
		},
		{
			name: "a missing value",
			args: []string{manifestFlag},
			says: "takes a value",
		},
		{
			name: "an empty value",
			args: []string{manifestFlag + "="},
			says: "names no manifest",
		},
		{
			name: "a repeated flag",
			args: []string{manifestFlag, "one.json", manifestFlag, "two.json"},
			says: "more than once",
		},
		{
			name: "a repeated flag carrying the same value twice",
			args: []string{manifestFlag, "one.json", manifestFlag + "=one.json"},
			says: "more than once",
		},
		{
			name: "a manifest on a stream",
			args: []string{manifestFlag, standardOutput},
			says: "relative to the directory holding it",
		},
		{
			name: "an emission with nowhere to go",
			args: []string{emitIRFlag + "="},
			says: "names nowhere",
		},
		{
			name: "a format that is neither encoding",
			args: []string{emitIRFlag, "orders.out", emitIRFormatFlag, "xml"},
			says: "is neither",
		},
		{
			name: "a format selecting the encoding of an emission that is not happening",
			args: []string{emitIRFormatFlag, jsonFormat},
			says: "asks for no emission",
		},
		{
			name: "a value on a flag that takes none",
			args: []string{versionFlag + "=2"},
			says: "takes no value",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			inv, err := parse(test.args)
			if err == nil {
				t.Fatalf("parse(%q) returned %+v, want a usage error", test.args, inv)
			}

			if !errors.As(err, new(*usageError)) {
				t.Errorf("parse(%q) returned %T, want a usage error", test.args, err)
			}

			if got := statusOf(err); got != statusUsage {
				t.Errorf("parse(%q) failed with status %d, want %d", test.args, got, statusUsage)
			}

			if !strings.Contains(err.Error(), test.says) {
				t.Errorf("parse(%q) reads %q, and does not name %q", test.args, err, test.says)
			}
		})
	}
}

// TestHelpIsAnsweredBeforeEverythingElse is docs/cli/SPEC.md's deliberate
// exception to the refusal of an unrecognised flag and to the at-most-once
// rule: a person asking a program what it is has usually typed the rest of the
// line already, and answering with a complaint about a flag they were in the
// middle of getting wrong teaches them nothing they asked to learn.
func TestHelpIsAnsweredBeforeEverythingElse(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{helpFlag},
		{shortHelpFlag},
		{"--nonsense", helpFlag},
		{manifestFlag, manifestFlag, helpFlag},
		{helpFlag, "an-operand"},
		{versionFlag, helpFlag},
		{helpFlag, versionFlag},
	} {
		inv, err := parse(args)
		if err != nil {
			t.Errorf("parse(%q): %v", args, err)

			continue
		}

		if inv.answer != answerHelp {
			t.Errorf("parse(%q) asks for answer %d, want help", args, inv.answer)
		}
	}
}

// TestVersionIsAnsweredBeforeEverythingButHelp is the other half of the same
// rule, and the precedence between the two.
func TestVersionIsAnsweredBeforeEverythingButHelp(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{versionFlag},
		{"--nonsense", versionFlag},
		{versionFlag, versionFlag},
		{manifestFlag, versionFlag},
	} {
		inv, err := parse(args)
		if err != nil {
			t.Errorf("parse(%q): %v", args, err)

			continue
		}

		if inv.answer != answerVersion {
			t.Errorf("parse(%q) asks for answer %d, want the version", args, inv.answer)
		}
	}
}

// TestAskingIsAWholeArgument holds the question to whole arguments only.
//
// A joined value is a value: `--manifest=--help` names a manifest called
// --help, however odd, and is not a request for usage. And an argument after
// `--` is an operand rather than a flag, so `-- --help` is the usage error
// every operand is.
func TestAskingIsAWholeArgument(t *testing.T) {
	t.Parallel()

	inv, err := parse([]string{manifestFlag + "=" + helpFlag})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if inv.answer != answerRun {
		t.Errorf("a manifest named %s asks for answer %d, want a run", helpFlag, inv.answer)
	}

	if inv.manifestPath() != helpFlag {
		t.Errorf("the manifest is %q, want %q", inv.manifestPath(), helpFlag)
	}

	if _, err := parse([]string{endOfOptions, helpFlag}); !errors.As(err, new(*usageError)) {
		t.Errorf("%s after %s returned %v, want the usage error every operand is", helpFlag, endOfOptions, err)
	}
}
