// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Command cpybkc-gen-env is a generator that reports one environment variable it
// was started with, and fails when it was not started with it.
//
//	cpybkc-gen-env --descriptor <path> --out <dir> --opt variable=SOURCE_DATE_EPOCH
//
// It generates nothing anybody would want. It exists so that *the environment
// reached the generator process* is a thing a check can assert rather than
// infer, which is what #252 turned out to need: the companion Dagger module's
// with-env-variable is a pass-through with three hops in it — the module sets a
// variable on a container, the container starts cpybkc with it, and cpybkc hands
// its whole environment to each generator (docs/plugin/SPEC.md, "The
// environment") — and the only one of those a check can see from outside is the
// last. A composed image whose environment carries the variable and whose
// generator does not see it is exactly the failure that would otherwise pass.
//
// # Why this is a program rather than a fixture
//
// The published image is a scratch image: it holds cpybkc, a generator, and no
// shell, no coreutils and no interpreter (docs/container/SPEC.md). So the
// two-line shell generator that answers this question in internal/plugin's tests
// cannot be composed into it, and the thing that can is a statically linked
// executable. That is this, built by the pipeline from this repository's own
// module and installed through with-generator-executable, which is the path a
// generator author takes before there is anything of theirs to pull.
//
// # Why it is not in cmd/ and is not published
//
// It is not a generator anybody should run over a real project, and the
// directories a release publishes an image from are cmd/'s. Test infrastructure
// is documented where it lives (docs/CONVENTIONS.md, "What belongs here"), and
// this is test infrastructure with a `main` function: internal/tools/ is where
// this repository already keeps the programs its pipeline runs and its releases
// do not.
//
// # What it implements of the plugin contract, and what it does not
//
// The whole of the invocation: --descriptor and --out each exactly once, --opt
// any number of times in the separated form, `--` ending the options, and a
// refusal with an `error:` diagnostic and a non-zero exit for an option key it
// does not recognise — which docs/plugin/SPEC.md requires of every plugin rather
// than permitting, because an ignored option is a line in a checked-in manifest
// that reads as configuration and does nothing.
//
// It reads no descriptor, which is a liberty this contract allows and this
// program takes: it is required to be handed one and required to say so if it is
// not, and nothing about the variable it reports depends on what is in it. A
// generator with an opinion about the IR is cmd/cpybkc-gen-go's job.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Flags of the invocation, spelled as docs/plugin/SPEC.md fixes them. They are
// constants here for the reason they are constants in the CLI: a spelling
// written twice is a spelling that can disagree with itself.
const (
	descriptorFlag = "--descriptor"
	outFlag        = "--out"
	optFlag        = "--opt"
	endOfOptions   = "--"
)

// variableOption is the one option key this generator recognises, and it names
// the environment variable to report.
//
// A key rather than a fixed SOURCE_DATE_EPOCH, so that the check driving this
// can state which variable it set in the same place it states the value. An
// option is the generator's own vocabulary and cpybkc neither interprets nor
// validates one, so nothing outside this file has to agree about the spelling
// beyond the manifest that names it.
const variableOption = "variable"

// valueFile is what this generator writes under --out, holding the value of the
// variable it was asked about and nothing else — no trailing newline, so that a
// check comparing bytes compares the value.
const valueFile = "value"

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		// The diagnostic format: one line, `<severity>: <message>`, on standard
		// error (docs/plugin/SPEC.md, "The diagnostic format"). The exit status
		// is 1 because the contract attaches meaning to zero and non-zero and to
		// nothing else.
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

// run is main with its inputs and its diagnostic stream passed in, so that a
// test can drive it.
func run(args []string, stderr io.Writer) error {
	invocation, err := parse(args)
	if err != nil {
		return err
	}

	value, ok := os.LookupEnv(invocation.variable)
	if !ok {
		// A refusal rather than an empty file, and it is the assertion this
		// program is for: a variable that did not arrive is the failure #252's
		// three hops exist to detect, and a run that quietly wrote nothing would
		// leave the check comparing an empty file with an empty expectation.
		return fmt.Errorf(
			"%s is not set in the environment this generator was started with: cpybkc passes its own environment "+
				"through unchanged, so a variable missing here is one that never reached cpybkc", invocation.variable)
	}

	// Standard error rather than standard output, where the contract says a
	// generator's explanation goes and where cpybkc attributes it to the
	// generator that wrote it. It makes a failing check readable without going
	// looking for the file.
	//
	// The write is deliberately unchecked: a diagnostic that could not be
	// written leaves nothing better to do than produce the file this generator
	// was asked for, and what cpybkc reads is the exit status.
	_, _ = fmt.Fprintf(stderr, "info: %s=%s\n", invocation.variable, value)

	return os.WriteFile(filepath.Join(invocation.out, valueFile), []byte(value), 0o644)
}

// invocation is one run's arguments, after parsing.
type invocation struct {
	// out is the private scratch directory this run writes into.
	out string
	// variable is the environment variable to report, from the one option this
	// generator takes.
	variable string
}

// parse reads the argument vector docs/plugin/SPEC.md fixes, and refuses
// everything it does not.
//
// The separated form only, which is the one form a plugin **MUST** accept and
// the only one cpybkc emits. The joined `--out=<dir>` spelling is one a plugin
// **MAY** additionally accept, and accepting it here would be this fixture
// covering a spelling the thing it is checking never produces.
func parse(args []string) (invocation, error) {
	var (
		parsed     invocation
		descriptor string
		options    []string
		// seen counts each flag, so that the arity below is enforced rather
		// than claimed. A message saying a flag appears exactly once beside a
		// loop that keeps the last of several is the fixture being wrong about
		// the contract it exists to be right about.
		seen = map[string]int{}
	)

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == endOfOptions {
			// This contract defines no operands and cpybkc passes none, so
			// anything after the delimiter is a caller doing something this
			// generator has no meaning for.
			if rest := args[i+1:]; len(rest) > 0 {
				return invocation{}, fmt.Errorf(
					"%d operands were passed after %s (%s): this contract defines no operands",
					len(rest), endOfOptions, strings.Join(rest, " "))
			}

			break
		}

		// Recognised before its value is counted, because the two failures have
		// different messages and only one of them is true of an argument this
		// program does not take: `--bogus` in final position is an unrecognised
		// argument, not a flag missing its value, and the second message sends
		// its reader looking for the value rather than for the typo.
		switch arg {
		case descriptorFlag, outFlag, optFlag:
			seen[arg]++
		default:
			return invocation{}, fmt.Errorf("unrecognised argument %q: the invocation is `%s <path> %s <dir> "+
				"[%s k=v ...]` and nothing else", arg, descriptorFlag, outFlag, optFlag)
		}

		if arg != optFlag && seen[arg] > 1 {
			return invocation{}, fmt.Errorf("%s was passed %d times, and it appears exactly once in every "+
				"invocation cpybkc emits", arg, seen[arg])
		}

		if i+1 >= len(args) {
			return invocation{}, fmt.Errorf("%s was passed with no value after it", arg)
		}

		value := args[i+1]
		i++

		switch arg {
		case descriptorFlag:
			descriptor = value
		case outFlag:
			parsed.out = value
		case optFlag:
			options = append(options, value)
		}
	}

	switch {
	case descriptor == "":
		return invocation{}, fmt.Errorf("%s is required and appears exactly once in every invocation", descriptorFlag)
	case parsed.out == "":
		return invocation{}, fmt.Errorf("%s is required and appears exactly once in every invocation", outFlag)
	}

	variable, err := readOptions(options)
	if err != nil {
		return invocation{}, err
	}

	parsed.variable = variable

	return parsed, nil
}

// readOptions takes the one option this generator recognises out of the `k=v`
// values it was given, and refuses every other key.
//
// Refusing rather than ignoring is the contract's requirement on a plugin and
// not this fixture's fastidiousness: an option cpybkc passed through and a
// generator dropped is configuration a reviewer can read in the manifest and
// nobody can observe in the output.
func readOptions(options []string) (string, error) {
	var (
		variable string
		// named separately from the value, because an option that was not
		// passed and one passed with an empty value are different mistakes and
		// telling a caller their required option is missing when they wrote it
		// is the diagnostic sending them to the wrong line of their manifest.
		// A value MAY be empty as far as the contract is concerned; it is this
		// generator that has no variable to read when it is.
		named bool
	)

	for _, option := range options {
		key, value, ok := strings.Cut(option, "=")
		if !ok {
			return "", fmt.Errorf("option %q is not `k=v`: everything before the first = is the key", option)
		}

		if key != variableOption {
			return "", fmt.Errorf("unrecognised option %q: this generator takes %s and nothing else",
				key, variableOption)
		}

		variable, named = value, true
	}

	switch {
	case !named:
		return "", errors.New("the `" + variableOption +
			"` option is required, and names the environment variable this generator reports")
	case variable == "":
		return "", errors.New("the `" + variableOption +
			"` option names no variable: it was passed with an empty value, and there is no variable to report")
	}

	return variable, nil
}
