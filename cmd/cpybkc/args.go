// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"strings"

	"github.com/Zaba505/cpybkc/internal/emit"
)

// The whole argument vector docs/cli/SPEC.md fixes, and nothing else:
//
//	cpybkc [--manifest <path>] [--emit-ir <dest> [--emit-ir-format <format>]]
//	cpybkc --version
//	cpybkc --help
//
// A flag this list does not carry is a usage error naming it. There is no
// --out, no --include, no --jobs, no --verbose and no --config; that document's
// "Out of Scope" refuses each one with its reason, and the refusal is only real
// if the parser has no case for it.
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
	versionFlag      = "--version"
	helpFlag         = "--help"

	// shortHelpFlag is the one single-hyphen spelling docs/cli/SPEC.md states.
	// A single leading hyphen MAY be accepted as a synonym for the others and
	// MUST NOT be documented; none is, because a spelling nothing documents is
	// one nobody can be told about and one this parser would have to keep
	// forever. -h is here because it is the one every user already has in their
	// fingers.
	shortHelpFlag = "-h"

	// endOfOptions is POSIX's delimiter. cpybkc defines no operand, so it can
	// only ever be followed by arguments that are usage errors; it is honoured
	// anyway, as the end of options, so that this program behaves the way its
	// neighbours on the system do.
	endOfOptions = "--"
)

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
	if asked, ok := answerAsked(args); ok {
		return invocation{answer: asked}, nil
	}

	var (
		inv  invocation
		seen = map[string]bool{}
	)

	for at := 0; at < len(args); at++ {
		argument := args[at]

		if argument == endOfOptions {
			// Everything after it is an operand by definition, and cpybkc
			// takes none. The delimiter itself is honoured; what follows it
			// cannot be.
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

		if !takesAValue(name) {
			// A value-less flag written with one is a vector this parser
			// refuses rather than one it strips a value off: --version=2 asks
			// for something --version does not do.
			if joined && (name == versionFlag || name == helpFlag || name == shortHelpFlag) {
				return invocation{}, usagef("%s takes no value, and %q gives it one", name, argument)
			}

			return invocation{}, unrecognisedError(name)
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
		if seen[name] {
			return invocation{}, usagef("%s appears more than once, and each flag appears at most once", name)
		}

		seen[name] = true

		switch name {
		case manifestFlag:
			if err := setManifest(&inv, value); err != nil {
				return invocation{}, err
			}
		case emitIRFlag:
			if value == "" {
				return invocation{}, usagef("%s names nowhere to write the descriptor", emitIRFlag)
			}

			inv.emitIR = value
		case emitIRFormatFlag:
			if value != binaryFormat && value != jsonFormat {
				return invocation{}, usagef("%s is %s or %s, and %q is neither",
					emitIRFormatFlag, binaryFormat, jsonFormat, value)
			}

			inv.emitIRFormat = value
		}
	}

	// docs/cli/SPEC.md: --emit-ir-format given without --emit-ir is a usage
	// error. It selects the encoding of an emission that is not happening, and
	// a flag on a command line that reads as configuration and does nothing is
	// the same fault an unknown field in a manifest is.
	if seen[emitIRFormatFlag] && !inv.emitting() {
		return invocation{}, usagef("%s selects the encoding %s writes, and this line asks for no emission",
			emitIRFormatFlag, emitIRFlag)
	}

	if inv.emitting() && inv.emitIRFormat == "" {
		inv.emitIRFormat = binaryFormat
	}

	return inv, nil
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

// takesAValue reports whether a flag is one of the three that carry one.
func takesAValue(name string) bool {
	switch name {
	case manifestFlag, emitIRFlag, emitIRFormatFlag:
		return true
	default:
		return false
	}
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

// operandError is a non-flag argument, wherever it appeared.
//
// The operand position is reserved and empty on purpose: it is where a
// subcommand would go, and a CLI that has already spent it on a path can never
// add one without deciding whether an argument is a file or a verb.
func operandError(argument string) error {
	return usagef("cpybkc takes no operand, and %q is one; every input is named by a flag, and the manifest "+
		"is named by %s", argument, manifestFlag)
}

// unrecognisedError is a flag this vector does not carry, named as it was
// typed.
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
}

// Error implements the error interface.
func (e *usageError) Error() string { return e.message }
