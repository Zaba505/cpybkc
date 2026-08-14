// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Zaba505/cpybkc/internal/emit"
)

// The whole argument vector docs/cli/SPEC.md fixes, and nothing else:
//
//	cpybkc [--manifest <path>] [--emit-ir <dest> [--emit-ir-format <format>]]
//	cpybkc init --copybook <path> … --out <dest>
//	cpybkc --version
//	cpybkc --help
//
// A flag this list does not carry is a usage error naming it. There is no
// --include, no --jobs, no --verbose and no --config; that document's "Out of
// Scope" refuses each one with its reason, and the refusal is only real if the
// parser has no case for it.
//
// The two sets are disjoint in the flags that name an **input**, and that is
// what makes a flag written under the wrong action a fault this parser can
// report as such rather than as an unrecognised one: --manifest, --emit-ir and
// --emit-ir-format are the default action's, --copybook and --out are `init`'s,
// and each is a real flag of this program in the wrong place.
//
// Every spelling here is a literal, including the two
// [github.com/Zaba505/cpybkc/internal/emit] also names. These constants are what
// the companion module's CLI-surface check reads to decide which flags this
// command accepts, and it evaluates a literal or a spelling built from another
// flag's and reports anything else as a value it could not read — so a name
// imported from another package would turn a drift guard into a failure. They
// are held to that package's names by a test instead
// (TestTheEmissionFlagsAreSpelledTheWayTheEncoderNamesThem).
const (
	manifestFlag     = "--manifest"
	emitIRFlag       = "--emit-ir"
	emitIRFormatFlag = "--emit-ir-format"
	copybookFlag     = "--copybook"
	outFlag          = "--out"
	versionFlag      = "--version"
	helpFlag         = "--help"

	// shortHelpFlag is the one single-hyphen spelling docs/cli/SPEC.md states.
	// A single leading hyphen MAY be accepted as a synonym for the others and
	// MUST NOT be documented; none is, because a spelling nothing documents is
	// one nobody can be told about and one this parser would have to keep
	// forever. -h is here because it is the one every user already has in their
	// fingers.
	shortHelpFlag = "-h"

	// endOfOptions is POSIX's delimiter. Neither action takes an operand, so it
	// can only ever be followed by arguments that are usage errors; it is
	// honoured anyway, as the end of options, so that this program behaves the
	// way its neighbours on the system do.
	//
	// It is not a way to reach a subcommand. docs/cli/SPEC.md reads the
	// subcommand name at the first argument and nowhere else, so `cpybkc --
	// init` is the default action with an operand in it — the usage error
	// `cpybkc -- foo` always was.
	endOfOptions = "--"
)

// initSubcommand is the one subcommand name cpybkc has, and the set is closed at
// it (docs/cli/SPEC.md, "One command, and one subcommand"). A second member is a
// change to that document rather than a case somebody adds on the way past.
//
// It is spelled here as a constant with no hyphens, so the companion module's
// CLI-surface check reads it as what it is: that check evaluates this package's
// string constants and keeps the ones shaped like a flag, and a verb is not one.
// The set of subcommands is not the flag table, and the check says so in its own
// words rather than by accident (.dagger/companion.go).
const initSubcommand = "init"

// defaultAction is the subcommand a line names when it names none: generating,
// which docs/cli/SPEC.md deliberately gives no name of its own so that the bare
// form keeps meaning what every existing command line and every published image
// already means by it.
const defaultAction = ""

// The encodings --emit-ir-format selects between.
//
// binaryFormat is the default because it is the form the rest of the system
// uses — the canonical protobuf wire encoding a plugin is handed — and the
// debug form is the one you ask for by name.
//
// They are the two encodings [github.com/Zaba505/cpybkc/internal/emit] produces,
// spelled here and held to that package's by the same test the flag names are: a
// format this parser accepted and [emit.Write] did not know would be a run that
// fails after the manifest has been read, over a vector that was the thing that
// was wrong.
const (
	binaryFormat = "binary"
	jsonFormat   = "json"
)

// defaultManifest is the manifest a run reads when the line names none: the one
// in the working directory, and nowhere else. docs/cli/SPEC.md forbids an
// upward search, so this is a file name rather than the start of one.
const defaultManifest = "cpybkc.json"

// standardOutput is what `--emit-ir -` names, and it is deliberately the
// plugin contract's reading of a dash, so that the character means one thing
// across this project. It is handed to [emit.Write] unchanged, and a dash this
// parser recognised and that function did not would be a file called "-", which
// is the last of the spellings that test holds.
const standardOutput = "-"

// answer is what a command line asks cpybkc for.
//
// It is a member of the invocation rather than a pair of booleans because the
// three are exclusive by docs/cli/SPEC.md's own rule — --help beats --version
// and both beat everything else — and a pair of booleans is a shape in which
// "both were asked for" is representable and has to be resolved again at every
// use site.
type answer int

const (
	// answerRun is a line that asked for a run: the default, and the only
	// answer that reads a manifest.
	answerRun answer = iota

	// answerHelp is --help or -h, anywhere on the line.
	answerHelp

	// answerVersion is --version, with no --help anywhere on the line.
	answerVersion
)

// invocation is one command line, resolved.
type invocation struct {
	// answer is what the line asked for.
	answer answer

	// subcommand is the action the line named: [initSubcommand] where the first
	// argument was that word, and [defaultAction] otherwise.
	//
	// It is set on a help answer too, because docs/cli/SPEC.md makes --help
	// write the named subcommand's usage where the first argument is one. It is
	// deliberately not set on a version answer: a build has one version
	// whichever action was going to run, so a subcommand on a --version line is
	// ignored along with everything else.
	subcommand string

	// copybooks are the paths --copybook named, in the order they were given
	// and as they were typed. A layout's own paths are relative to the layout,
	// so a path cpybkc rewrote on the adopter's behalf would be one they cannot
	// find in what they typed.
	copybooks []string

	// out is where --out says the scaffold goes: a path, or [standardOutput].
	out string

	// manifest is the path --manifest named, empty where it named none.
	// [invocation.manifestPath] is what a run reads.
	manifest string

	// emitIR is the destination --emit-ir named — a path, or
	// [standardOutput] — and is empty where the line asked for no emission.
	emitIR string

	// emitIRFormat is the encoding the emission is written in, and is
	// [binaryFormat] where an emission was asked for and no format was.
	emitIRFormat string
}

// manifestPath is the manifest this run reads: the one --manifest named, or
// [defaultManifest] in the working directory.
//
// A method rather than a default filled in during the parse, so that "the line
// named no manifest" survives parsing. #148 resolves this path and reports what
// it could not open, and a diagnostic has to be able to say whether the path it
// names was typed or defaulted.
func (inv invocation) manifestPath() string {
	if inv.manifest == "" {
		return defaultManifest
	}

	return inv.manifest
}

// emitting reports whether this run writes a descriptor instead of generating.
func (inv invocation) emitting() bool { return inv.emitIR != "" }

// scaffolding reports whether this line is `init`'s rather than the default
// action's.
func (inv invocation) scaffolding() bool { return inv.subcommand == initSubcommand }

// format is the encoding [emit.Write] is asked for.
//
// The conversion is safe by [parse]'s own rule rather than by assertion: a value
// that is neither spelling is a usage error before anything is opened, so what
// reaches here is one of the two constants above or the default they fall back
// to. It is a method so that the conversion happens once, where the parser's
// string meets the encoder's type, rather than at whatever use site got there
// first.
func (inv invocation) format() emit.Format { return emit.Format(inv.emitIRFormat) }

// parse reads the argument vector.
//
// Every fault it reports is a usage error, which is the whole of status 2: the
// vector could not be understood, and cpybkc did nothing at all — no file was
// opened, no generator was started, and the project's tree was not touched.
// Nothing here opens anything, and that is what makes the promise true rather
// than intended.
func parse(args []string) (invocation, error) {
	// docs/cli/SPEC.md: --help and --version are answered before anything
	// else, and every other flag on the line is ignored, including one that
	// would otherwise be a usage error. So the question is asked before the
	// vector is read rather than during it — a person asking a program what it
	// is has usually typed the rest of the line already, and answering with a
	// complaint about a flag they were in the middle of getting wrong teaches
	// them nothing they asked to learn.
	//
	// It is asked before the head is classified, too, which is what makes
	// `cpybkc bogus --help` write the top-level usage and exit 0 rather than
	// complaining about a verb nobody has: an unrecognised verb is the commonest
	// way to arrive at that question.
	if asked, ok := answerAsked(args); ok {
		inv := invocation{answer: asked}

		// Which usage --help writes is the one thing the subcommand changes
		// here. --version is unchanged by it on purpose, so it is not read from
		// the head at all.
		if asked == answerHelp {
			inv.subcommand, _ = head(args)
		}

		return inv, nil
	}

	// docs/cli/SPEC.md: the set of subcommand names is closed at `init`, so a
	// first argument that is a non-flag and is not that word is a usage error
	// naming it, decided before anything is opened. It is a sentence of its own
	// rather than the operand refusal below, because the position it is in is
	// the one a verb goes in and a reader who typed a verb needs to be told
	// which verbs there are.
	if len(args) > 0 && !isFlag(args[0]) && args[0] != initSubcommand {
		return invocation{}, subcommandError(args[0])
	}

	subcommand, rest := head(args)

	inv, err := vector(subcommand, rest)
	if err != nil {
		// The usage a reader needs is the one whose flags they were using, and
		// the only place that knows which action the line was written under is
		// here, above the walk. [run] reads it back off the error.
		return invocation{}, under(subcommand, err)
	}

	return inv, nil
}

// head reads the subcommand name off the front of the vector, and hands back
// what is left for the action's own flags.
//
// docs/cli/SPEC.md reads a subcommand name at exactly one position, the first
// argument, and the rule is one rule at one position rather than "the first
// argument that is not a flag". The two disagree on lines somebody will type:
// `cpybkc -- init` and `cpybkc --manifest x.json init` both have `init` as their
// first non-flag argument, and under this rule both are the default action with
// an operand in them — the usage error [operandError] reports. A subcommand that
// could hide behind another action's flags would make "which flags belong to
// what" a question with no answer on the line itself.
//
// Everything else is the default action's, including a first argument that is
// neither a flag nor a subcommand name: [parse] refuses that one above, so that
// this reads as the two-way split docs/cli/SPEC.md states.
func head(args []string) (subcommand string, rest []string) {
	if len(args) > 0 && args[0] == initSubcommand {
		return initSubcommand, args[1:]
	}

	return defaultAction, args
}

// vector walks one action's flags: everything after the head, read against the
// set the head admits.
//
// One walk for both actions rather than one each. The shape of the vector —
// the two value spellings, the end of options, a missing value, a flag that
// repeats when it may not — is docs/cli/SPEC.md's "The argument vector", which
// says it applies to `init` unchanged; two walks would be two places for that to
// stop being true, and the second would be the one nobody re-read. What differs
// between the actions is which flags they carry, what a flag they do not carry
// is reported as, and what is required at the end, and those are the three
// functions below.
func vector(subcommand string, args []string) (invocation, error) {
	var (
		inv  = invocation{subcommand: subcommand}
		seen = map[string]bool{}
	)

	for at := 0; at < len(args); at++ {
		argument := args[at]

		if argument == endOfOptions {
			// Everything after it is an operand by definition, and neither
			// action takes one. The delimiter itself is honoured; what follows
			// it cannot be.
			if operands := args[at+1:]; len(operands) > 0 {
				return invocation{}, operandError(operands[0])
			}

			break
		}

		if !isFlag(argument) {
			return invocation{}, operandError(argument)
		}

		// The joined form, --manifest=cpybkc.json. Only the first = separates a
		// flag from its value, so a value may itself contain one.
		name, value, joined := strings.Cut(argument, "=")

		if !takesAValue(subcommand, name) {
			// A value-less flag written with one is a vector this parser
			// refuses rather than one it strips a value off: --version=2 asks
			// for something --version does not do. It is asked under both
			// actions, because both answer those three flags.
			if joined && (name == versionFlag || name == helpFlag || name == shortHelpFlag) {
				return invocation{}, usagef("%s takes no value, and %q gives it one", name, argument)
			}

			return invocation{}, refuse(subcommand, name)
		}

		if !joined {
			at++
			if at >= len(args) {
				return invocation{}, usagef("%s takes a value and was passed as the last argument", name)
			}

			value = args[at]
		}

		// docs/cli/SPEC.md: a flag given twice is a usage error, even where
		// both occurrences carry the same value. Taking the last is the rule
		// most parsers default to, and it makes a line that names one file
		// twice a line that reads the file it also names as something else.
		//
		// --copybook is the one flag that repeats, and it is not an exception to
		// that reasoning: its occurrences are a list rather than one value
		// stated several times, so nothing the person wrote is discarded. What
		// the rule was for survives there too, in [setCopybook], which refuses
		// two occurrences carrying equal values.
		if seen[name] && name != copybookFlag {
			return invocation{}, usagef("%s appears more than once, and each flag appears at most once", name)
		}

		seen[name] = true

		if err := set(&inv, name, value); err != nil {
			return invocation{}, err
		}
	}

	if err := required(&inv, seen); err != nil {
		return invocation{}, err
	}

	return inv, nil
}

// set applies one flag occurrence to the invocation being built.
//
// Every name reaching here is one the action carries: [takesAValue] has already
// said so, and a flag the action does not carry was refused above with a
// sentence of its own. So this switch is about values and not about membership.
func set(inv *invocation, name, value string) error {
	switch name {
	case manifestFlag:
		return setManifest(inv, value)
	case copybookFlag:
		return setCopybook(inv, value)
	case outFlag:
		if value == "" {
			return usagef("%s names nowhere to write the scaffold", outFlag)
		}

		inv.out = value
	case emitIRFlag:
		if value == "" {
			return usagef("%s names nowhere to write the descriptor", emitIRFlag)
		}

		inv.emitIR = value
	case emitIRFormatFlag:
		if value != binaryFormat && value != jsonFormat {
			return usagef("%s is %s or %s, and %q is neither",
				emitIRFormatFlag, binaryFormat, jsonFormat, value)
		}

		inv.emitIRFormat = value
	}

	return nil
}

// required is what each action needs before it can be performed, asked once the
// whole vector has been read.
//
// It is here rather than at each flag's own case because none of these faults is
// a property of one occurrence: a missing flag is a property of the line, and so
// is a flag whose meaning depends on another that never arrived.
func required(inv *invocation, seen map[string]bool) error {
	if inv.scaffolding() {
		// docs/cli/SPEC.md: at least one --copybook MUST be given, and --out
		// MUST be. There is no default for either. A copybook cannot be
		// defaulted because the layout that would name one is the file this
		// command is writing; a destination cannot, because a name of cpybkc's
		// own is one the adopter then has to correct, and a scaffold written
		// somewhere they did not name is one they may not find.
		switch {
		case len(inv.copybooks) == 0:
			return usagef("%s reads the copybooks %s names, and this line names none",
				initSubcommand, copybookFlag)
		case inv.out == "":
			return usagef("%s writes the scaffold where %s says, and this line says nowhere",
				initSubcommand, outFlag)
		}

		return nil
	}

	// docs/cli/SPEC.md: --emit-ir-format given without --emit-ir is a usage
	// error. It selects the encoding of an emission that is not happening, and
	// a flag on a command line that reads as configuration and does nothing is
	// the same fault an unknown field in a manifest is.
	if seen[emitIRFormatFlag] && !inv.emitting() {
		return usagef("%s selects the encoding %s writes, and this line asks for no emission",
			emitIRFormatFlag, emitIRFlag)
	}

	if inv.emitting() && inv.emitIRFormat == "" {
		inv.emitIRFormat = binaryFormat
	}

	return nil
}

// setManifest applies --manifest.
//
// The dash is refused here rather than left to the open: every path inside a
// manifest is relative to the manifest's own directory, and a manifest arriving
// on a stream has no directory for them to be relative to. Refusing it is a
// statement about the vector, decided with nothing opened, which is why it
// carries status 2 and not the status a manifest that cannot be read carries.
func setManifest(inv *invocation, value string) error {
	switch value {
	case "":
		return usagef("%s names no manifest to read", manifestFlag)
	case standardOutput:
		return usagef("%s cannot be %q: a manifest's paths are relative to the directory holding it, "+
			"and a manifest on a stream is in no directory", manifestFlag, standardOutput)
	}

	inv.manifest = value

	return nil
}

// setCopybook applies one --copybook occurrence.
//
// It appends rather than replaces, because docs/cli/SPEC.md makes this the one
// flag that repeats: its occurrences are a list, one per file, and a layout is
// what would otherwise name a project's copybooks — there is no layout yet, so
// this is the one input `init` cannot resolve the ordinary way.
//
// The value is kept exactly as it was typed. A layout's own paths are relative
// to the layout, so a path that reached cpybkc from a different directory than
// the scaffold's is the adopter's to correct, and one cpybkc rewrote on their
// behalf would be one they cannot find in what they typed.
//
// Two occurrences carrying byte-equal values are the duplicate the at-most-once
// rule is really about: naming one copybook twice states nothing the line did
// not already state. Two different spellings are two copybooks, and nothing here
// asks what they resolve to — behind a symlink, a bind mount or a
// case-insensitive filesystem there is no comparison that is right every time,
// and the scaffold simply holds each 01-level twice under two record names.
func setCopybook(inv *invocation, value string) error {
	switch value {
	case "":
		return usagef("%s names no copybook to read", copybookFlag)
	case standardOutput:
		return usagef("%s cannot be %q: the scaffold states a path for each record's copybook, "+
			"and a copybook on a stream has none to state", copybookFlag, standardOutput)
	}

	if slices.Contains(inv.copybooks, value) {
		return usagef("%s names %q more than once, and naming one copybook twice states nothing "+
			"the line did not already state", copybookFlag, value)
	}

	inv.copybooks = append(inv.copybooks, value)

	return nil
}

// takesAValue reports whether a flag is one the given action carries and one
// that carries a value.
//
// The two questions are one question, because every flag either action carries
// takes a value: --version, --help and -h are answered before the vector is
// interpreted at all, so they never reach this. A flag that is false here is
// refused, and [refuse] is what decides which of the two refusals it gets.
func takesAValue(subcommand, name string) bool {
	switch name {
	case copybookFlag, outFlag:
		return subcommand == initSubcommand
	case manifestFlag, emitIRFlag, emitIRFormatFlag:
		return subcommand == defaultAction
	default:
		return false
	}
}

// refuse is a flag the action does not carry.
//
// docs/cli/SPEC.md asks for two different sentences here, and the difference is
// what a reader has to do next. A flag of this program written under the action
// it does not belong to is a real flag in the wrong place, and reporting it as
// unrecognised sends the reader to check their spelling — a search that ends
// nowhere, because the spelling is right. So each direction names the flag and
// the action it belongs to, and neither reads like [unrecognisedError].
func refuse(subcommand, name string) error {
	switch {
	case subcommand == initSubcommand && (name == manifestFlag || name == emitIRFlag || name == emitIRFormatFlag):
		return usagef("%s is not one of %s's flags: it is the default action's, and this line is %s's",
			name, initSubcommand, initSubcommand)
	case subcommand == defaultAction && (name == copybookFlag || name == outFlag):
		return usagef("%s is one of %s's flags, and this line names no subcommand; "+
			"`cpybkc %s %s …` is where it belongs", name, initSubcommand, initSubcommand, name)
	default:
		return unrecognisedError(name)
	}
}

// under stamps a usage error with the action the line was written under, so that
// the usage accompanying it on standard error is the one whose flags the reader
// was using.
//
// It is applied once, where the head was read, rather than at each refusal: a
// message that has to remember to say which action it came from is one that
// eventually does not, and every fault below the head belongs to the same action
// by construction.
func under(subcommand string, err error) error {
	var usage *usageError
	if errors.As(err, &usage) {
		usage.subcommand = subcommand
	}

	return err
}

// answerAsked reports whether the line asks what cpybkc is rather than for a
// run, and which of the two questions it asks.
//
// Only a whole argument counts, so `--manifest=--help` is a manifest named
// --help and not a request for usage; and the scan stops at [endOfOptions],
// because an argument after it is an operand rather than a flag, and cpybkc's
// answer to an operand is that it takes none.
//
// --help beats --version wherever the two appear, which is the precedence
// docs/cli/SPEC.md states rather than one this function chooses.
func answerAsked(args []string) (answer, bool) {
	asked, found := answerRun, false

	for _, argument := range args {
		switch argument {
		case endOfOptions:
			return asked, found
		case helpFlag, shortHelpFlag:
			return answerHelp, true
		case versionFlag:
			asked, found = answerVersion, true
		}
	}

	return asked, found
}

// isFlag reports whether an argument is written as one.
//
// A lone dash is an operand rather than a flag: POSIX gives it its meaning as
// an operand standing for a standard stream, and cpybkc has no operand for it
// to stand in.
func isFlag(argument string) bool {
	return strings.HasPrefix(argument, "-") && argument != standardOutput
}

// operandError is a non-flag argument in a position no subcommand name is read
// at: anywhere but the head, under either action, and anywhere at all after
// [endOfOptions].
//
// The head is spent — docs/cli/SPEC.md's "The first operand is a subcommand
// name" gives it to `init`, and [subcommandError] is what a first argument that
// is not that word gets. Everywhere else the answer is unchanged: neither action
// takes an operand, and every input is named by a flag. That includes the
// arguments after `init`, which are its flags and not a list of copybooks;
// operands there would spend the position a second subcommand's own subject
// would need, one release after this document spent the first.
func operandError(argument string) error {
	return usagef("cpybkc takes no operand, and %q is one; every input is named by a flag — the manifest "+
		"by %s, and a copybook by %s under %s", argument, manifestFlag, copybookFlag, initSubcommand)
}

// subcommandError is a first argument that is a non-flag and is not a subcommand
// name.
//
// It names what was typed and the one verb there is. docs/cli/SPEC.md closes the
// set at `init`, and a reader who typed a verb has already decided they are
// looking at a program with verbs; telling them cpybkc takes no operand answers
// a question they did not ask.
func subcommandError(argument string) error {
	return usagef("%q is not a cpybkc subcommand, and %s is the only one; cpybkc with no subcommand generates, "+
		"and takes no operand", argument, initSubcommand)
}

// unrecognisedError is a flag neither action carries, named as it was typed.
//
// A flag this program does carry, written under the action it does not belong
// to, is [refuse]'s other case and reads differently on purpose.
func unrecognisedError(name string) error {
	return usagef("%s is not a cpybkc flag", name)
}

// usagef is a [usageError], spelled at its use sites the way a sentence is
// rather than as a struct literal.
func usagef(format string, a ...any) error {
	return &usageError{message: fmt.Sprintf(format, a...)}
}

// usageError is a vector cpybkc could not understand.
//
// It is a type rather than a sentinel because it is what [statusOf] reads: the
// one distinction docs/cli/SPEC.md thinks worth encoding in an exit status is
// whether cpybkc understood the request at all, so "this is that failure" has
// to survive the trip from the parser to the exit path.
type usageError struct {
	message string

	// subcommand is the action the line was written under, and it decides which
	// usage accompanies this error on standard error. It travels on the error
	// because the vector is what knows it and [run] is where usage is written,
	// and between the two there is nothing left of the line but this.
	subcommand string
}

// Error implements the error interface.
func (e *usageError) Error() string { return e.message }
