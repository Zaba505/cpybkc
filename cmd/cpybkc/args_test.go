// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"slices"
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
			args: []string{"--nope", "gen"},
			says: "--nope",
		},
		{
			name: "a flag this document refuses by name",
			args: []string{"--verbose"},
			says: "--verbose",
		},
		{
			name: "a subcommand's flag with no subcommand",
			args: []string{outFlag, "layout.sexpr"},
			says: initSubcommand,
		},
		{
			name: "a subcommand's repeatable flag with no subcommand",
			args: []string{copybookFlag, "posting.cpy"},
			says: initSubcommand,
		},
		{
			name: "a first argument that is a verb nobody has",
			args: []string{"generate"},
			says: "not a cpybkc subcommand",
		},
		{
			name: "the subcommand name behind the end of options",
			args: []string{endOfOptions, initSubcommand},
			says: "no operand",
		},
		{
			name: "the subcommand name behind another action's flag",
			args: []string{manifestFlag, defaultManifest, initSubcommand},
			says: "no operand",
		},
		{
			name: "a single-hyphen synonym nothing documents",
			args: []string{"-manifest", "cpybkc.json"},
			says: "-manifest",
		},
		{
			// The head is the subcommand's position now, so a first argument
			// that is a non-flag is refused as the verb it is not, rather than
			// as the operand it would have been.
			name: "a first argument that is neither a flag nor a subcommand name",
			args: []string{"cpybkc.json"},
			says: "not a cpybkc subcommand",
		},
		{
			name: "an operand after the end of options",
			args: []string{endOfOptions, "cpybkc.json"},
			says: "no operand",
		},
		{
			name: "a lone dash, which POSIX makes an operand and not a flag",
			args: []string{standardOutput},
			says: "not a cpybkc subcommand",
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

// TestParseReadsTheInitVector is the second synopsis line docs/cli/SPEC.md
// fixes: the subcommand at the head, one --copybook per file, and --out.
//
// The copybooks arrive in the order they were given and as they were typed. A
// layout's own paths are relative to the layout, so a path cpybkc rewrote on the
// adopter's behalf would be one they cannot find in what they typed.
func TestParseReadsTheInitVector(t *testing.T) {
	t.Parallel()

	inv, err := parse([]string{
		initSubcommand,
		copybookFlag, "copybooks/posting.cpy",
		copybookFlag + "=copybooks/account.cpy",
		outFlag, "layout.sexpr",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !inv.scaffolding() {
		t.Errorf("the line names %s and the invocation says the subcommand is %q", initSubcommand, inv.subcommand)
	}

	if inv.answer != answerRun {
		t.Errorf("the line asks for answer %d, want a run", inv.answer)
	}

	want := []string{"copybooks/posting.cpy", "copybooks/account.cpy"}
	if !slices.Equal(inv.copybooks, want) {
		t.Errorf("the copybooks are %q, want %q", inv.copybooks, want)
	}

	if inv.out != "layout.sexpr" {
		t.Errorf("%s is %q", outFlag, inv.out)
	}

	// init reads no manifest, so the line naming none is not the line defaulting
	// to one: nothing on this path ever asks [invocation.manifestPath].
	if inv.manifest != "" {
		t.Errorf("an %s line named the manifest %q", initSubcommand, inv.manifest)
	}
}

// TestInitWritesToStandardOutput covers the destination that is not a path.
// `--out -` is the one spelling that puts the scaffold on standard output, and
// it is the same reading of a dash the rest of this program uses.
func TestInitWritesToStandardOutput(t *testing.T) {
	t.Parallel()

	inv, err := parse([]string{initSubcommand, copybookFlag, "posting.cpy", outFlag, standardOutput})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if inv.out != standardOutput {
		t.Errorf("%s is %q, want %q", outFlag, inv.out, standardOutput)
	}
}

// TestTwoSpellingsAreTwoCopybooks is what docs/cli/SPEC.md deliberately does not
// decide: whether two --copybook values name one file on disk.
//
// Byte-equal values are a duplicate and refused. Two different spellings that
// resolve to one file are two copybooks, and the scaffold holds each 01-level
// twice under two record names — because behind a symlink, a bind mount or a
// case-insensitive filesystem there is no comparison that is right every time.
func TestTwoSpellingsAreTwoCopybooks(t *testing.T) {
	t.Parallel()

	inv, err := parse([]string{
		initSubcommand,
		copybookFlag, "posting.cpy",
		copybookFlag, "./posting.cpy",
		outFlag, standardOutput,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(inv.copybooks) != 2 {
		t.Errorf("two spellings of one path parsed as %q, want two copybooks", inv.copybooks)
	}
}

// TestParseRefusesTheInitVector is every way docs/cli/SPEC.md says an `init`
// line can fail to be understood. Each is status 2, and each is decided with
// nothing opened and nothing created.
func TestParseRefusesTheInitVector(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		args []string
		says string
	}{
		{
			name: "no copybook at all",
			args: []string{initSubcommand, outFlag, "layout.sexpr"},
			says: copybookFlag,
		},
		{
			name: "no destination",
			args: []string{initSubcommand, copybookFlag, "posting.cpy"},
			says: outFlag,
		},
		{
			name: "no flags at all",
			args: []string{initSubcommand},
			says: copybookFlag,
		},
		{
			name: "a copybook on a stream",
			args: []string{initSubcommand, copybookFlag, standardOutput, outFlag, "layout.sexpr"},
			says: "has none to state",
		},
		{
			name: "one copybook named twice",
			args: []string{
				initSubcommand,
				copybookFlag, "posting.cpy",
				copybookFlag + "=posting.cpy",
				outFlag, "layout.sexpr",
			},
			says: "more than once",
		},
		{
			name: "a destination named twice",
			args: []string{
				initSubcommand,
				copybookFlag, "posting.cpy",
				outFlag, "one.sexpr",
				outFlag, "two.sexpr",
			},
			says: "more than once",
		},
		{
			name: "an empty copybook",
			args: []string{initSubcommand, copybookFlag + "=", outFlag, "layout.sexpr"},
			says: "names no copybook",
		},
		{
			name: "an empty destination",
			args: []string{initSubcommand, copybookFlag, "posting.cpy", outFlag + "="},
			says: "names nowhere",
		},
		{
			name: "a missing value",
			args: []string{initSubcommand, copybookFlag},
			says: "takes a value",
		},
		{
			name: "an operand after the subcommand",
			args: []string{initSubcommand, "posting.cpy"},
			says: "no operand",
		},
		{
			name: "a second operand after the subcommand's flags",
			args: []string{initSubcommand, copybookFlag, "posting.cpy", outFlag, "layout.sexpr", "extra"},
			says: "no operand",
		},
		{
			name: "an operand after the end of options",
			args: []string{initSubcommand, endOfOptions, "posting.cpy"},
			says: "no operand",
		},
		{
			name: "a flag no action carries",
			args: []string{initSubcommand, "--nope"},
			says: "not a cpybkc flag",
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

// TestAFlagUnderTheWrongActionIsNotUnrecognised is the distinction
// docs/cli/SPEC.md asks for in both directions: a real flag of this program,
// written under the action it does not belong to, names the flag and the
// subcommand rather than reporting it as unrecognised.
//
// The difference is what a reader does next. "--manifest is not a cpybkc flag"
// sends them to check a spelling that is already right, and the search ends
// nowhere.
func TestAFlagUnderTheWrongActionIsNotUnrecognised(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		args []string
		flag string
	}{
		{name: "the manifest under init", args: []string{initSubcommand, manifestFlag, defaultManifest},
			flag: manifestFlag},
		{name: "an emission under init", args: []string{initSubcommand, emitIRFlag, standardOutput},
			flag: emitIRFlag},
		{name: "an emission format under init", args: []string{initSubcommand, emitIRFormatFlag, jsonFormat},
			flag: emitIRFormatFlag},
		{name: "a copybook with no subcommand", args: []string{copybookFlag, "posting.cpy"}, flag: copybookFlag},
		{name: "a destination with no subcommand", args: []string{outFlag, "layout.sexpr"}, flag: outFlag},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := parse(test.args)
			if err == nil {
				t.Fatalf("parse(%q) returned no error, want a usage error", test.args)
			}

			for _, want := range []string{test.flag, initSubcommand} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("parse(%q) reads %q, and does not name %q", test.args, err, want)
				}
			}

			if strings.Contains(err.Error(), unrecognisedError(test.flag).Error()) {
				t.Errorf("parse(%q) reports a flag of this program as unrecognised: %q", test.args, err)
			}
		})
	}
}

// TestAUsageErrorCarriesTheActionItWasWrittenUnder is what decides which usage
// accompanies it on standard error. The parser is the only place that knows, and
// [run] is where usage is written; between the two there is nothing left of the
// line but the error.
func TestAUsageErrorCarriesTheActionItWasWrittenUnder(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{initSubcommand, "--nope"}, want: initSubcommand},
		{args: []string{initSubcommand, copybookFlag, "posting.cpy"}, want: initSubcommand},
		{args: []string{"--nope"}, want: defaultAction},
		{args: []string{endOfOptions, "posting.cpy"}, want: defaultAction},

		// The head itself was never classified, so the error belongs to no
		// action and the whole command's usage is what answers it.
		{args: []string{"bogus"}, want: defaultAction},
	} {
		_, err := parse(test.args)

		var usage *usageError
		if !errors.As(err, &usage) {
			t.Errorf("parse(%q) returned %v, want a usage error", test.args, err)

			continue
		}

		if usage.subcommand != test.want {
			t.Errorf("parse(%q) is reported under %q, want %q", test.args, usage.subcommand, test.want)
		}
	}
}

// TestHelpUnderASubcommandIsTheSubcommands is docs/cli/SPEC.md's rule that
// --help and --version are answered under every subcommand, and that only
// --help varies with the head.
func TestHelpUnderASubcommandIsTheSubcommands(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		args   []string
		answer answer
		under  string
	}{
		{args: []string{initSubcommand, helpFlag}, answer: answerHelp, under: initSubcommand},
		{args: []string{initSubcommand, shortHelpFlag}, answer: answerHelp, under: initSubcommand},
		{args: []string{initSubcommand, copybookFlag, helpFlag}, answer: answerHelp, under: initSubcommand},
		{args: []string{helpFlag}, answer: answerHelp, under: defaultAction},

		// A verb nobody has is not complained about: the question is answered,
		// with the top-level usage.
		{args: []string{"bogus", helpFlag}, answer: answerHelp, under: defaultAction},

		// A build has one version whichever action was going to run.
		{args: []string{initSubcommand, versionFlag}, answer: answerVersion, under: defaultAction},
	} {
		inv, err := parse(test.args)
		if err != nil {
			t.Errorf("parse(%q): %v", test.args, err)

			continue
		}

		if inv.answer != test.answer {
			t.Errorf("parse(%q) asks for answer %d, want %d", test.args, inv.answer, test.answer)
		}

		if inv.subcommand != test.under {
			t.Errorf("parse(%q) is answered under %q, want %q", test.args, inv.subcommand, test.under)
		}
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
