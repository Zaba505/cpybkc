// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"go/token"
	"slices"
	"strings"
	"unicode"
)

// The argument vector docs/plugin/SPEC.md fixes, and nothing else:
//
//	cpybkc-gen-go --descriptor <path> --out <dir> [--opt k=v ...]
//
// Only the separated form is accepted — each flag followed by its value as the
// next argument. The joined `--descriptor=<path>` spelling is one a plugin MAY
// accept and cpybkc MUST NOT emit, so accepting it here would be a second
// spelling of an invocation nothing produces, and the one required form is what
// keeps a shell-script plugin able to meet this contract in three lines of
// `case`.
const (
	descriptorFlag = "--descriptor"
	outFlag        = "--out"
	optFlag        = "--opt"

	// endOfOptions is POSIX's delimiter. This contract defines no operands and
	// cpybkc passes none; it is honoured anyway so that this program behaves
	// like its neighbours when it is run by hand.
	endOfOptions = "--"

	// standardInput is what `--descriptor -` names. cpybkc always passes a
	// path, and the `-` form exists so that a plugin is drivable from a
	// pipeline that has a descriptor and nowhere convenient to put it.
	standardInput = "-"
)

// invocation is one run's arguments, resolved.
type invocation struct {
	// descriptor is the path the descriptor is read from, or [standardInput].
	descriptor string

	// out is the directory every generated file is written beneath.
	out string

	// options are the `--opt k=v` pairs, in the generator's own vocabulary.
	options options
}

// parse reads the argument vector.
//
// Every fault it can report is the caller's mistake rather than a user's typo —
// cpybkc builds this vector from a manifest it has already checked — but a
// plugin is entitled to be run by hand and by something that is not this
// version of cpybkc, so each one is reported as what it is rather than
// tolerated.
func parse(args []string) (invocation, error) {
	var (
		inv            invocation
		seenDescriptor bool
		seenOut        bool
	)

parsing:
	for at := 0; at < len(args); at++ {
		flag := args[at]

		if flag == endOfOptions {
			if operands := args[at+1:]; len(operands) > 0 {
				return invocation{}, fmt.Errorf(
					"this generator takes no operands, and %d followed %s", len(operands), endOfOptions)
			}

			break parsing
		}

		var value string

		switch flag {
		case descriptorFlag, outFlag, optFlag:
			at++
			if at >= len(args) {
				return invocation{}, fmt.Errorf("%s takes a value and was passed as the last argument", flag)
			}

			value = args[at]
		default:
			return invocation{}, fmt.Errorf(
				"unrecognised argument %q; the vector is `cpybkc-gen-go %s <path> %s <dir> [%s k=v ...]`",
				flag, descriptorFlag, outFlag, optFlag)
		}

		switch flag {
		case descriptorFlag:
			if seenDescriptor {
				return invocation{}, twice(descriptorFlag)
			}

			if value == "" {
				return invocation{}, fmt.Errorf("%s names no file to read the descriptor from", descriptorFlag)
			}

			inv.descriptor, seenDescriptor = value, true
		case outFlag:
			if seenOut {
				return invocation{}, twice(outFlag)
			}

			if value == "" {
				return invocation{}, fmt.Errorf("%s names no directory to write into", outFlag)
			}

			inv.out, seenOut = value, true
		case optFlag:
			if err := inv.options.set(value); err != nil {
				return invocation{}, err
			}
		}
	}

	if !seenDescriptor {
		return invocation{}, fmt.Errorf("%s is required: it names the resolved IR this generator reads", descriptorFlag)
	}

	if !seenOut {
		return invocation{}, fmt.Errorf("%s is required: it names the directory this generator writes into", outFlag)
	}

	if err := inv.options.check(); err != nil {
		return invocation{}, err
	}

	return inv, nil
}

// twice is the fault of a flag docs/plugin/SPEC.md says appears exactly once
// appearing more than once.
//
// Refused rather than resolved by taking the last one, because there is no
// reading of two descriptors or two output directories that is more obviously
// right than the other, and a vector cpybkc cannot have produced is one whose
// author wants telling.
func twice(flag string) error {
	return fmt.Errorf("%s appears more than once, and it appears exactly once", flag)
}

// packageNameOption is the option naming the Go package the generated files
// declare.
//
// It is required, and it is an option rather than something inferred, because
// the only names this program can see are the descriptor's path and the output
// directory's — and docs/plugin/SPEC.md forbids deriving anything from the
// first, while the second is a scratch directory whose name is cpybkc's to
// choose and varies between runs. A package name taken from either would make
// the output a function of a path, which is exactly what "Determinism" forbids.
const packageNameOption = "package_name"

// receiverOption is the identifier the decode and encode methods declare their
// receiver under.
//
// Optional, and it is an option rather than something derived from the record's
// own name because Go's convention for a receiver is a shop's rather than a
// generator's — one letter, the same letter on every method of a type, and
// which letter is the sort of thing a house style states once. Deriving it from
// each record's identifier would give one package a different receiver per
// type, which is the one shape the convention rules out.
const receiverOption = "receiver"

// importPathOption is the Go import path the generated package will answer to
// once cpybkc has merged it into the project tree.
//
// It is the one fact about the output this generator cannot derive and cannot
// do without. The generated tests are an external test package — `package
// <name>_test`, which docs/plugin/SPEC.md's neighbour decision in README.md
// settled — and an external test package reaches the package beside it by
// importing it, by path. The path is not knowable from anything else in the
// invocation: `--out` is a private scratch directory cpybkc creates and
// discards (docs/plugin/SPEC.md, "The scratch directory"), so it names neither
// the module nor the directory the files end up in, and walking up from it for
// a go.mod would find whatever happens to be above a temporary directory.
//
// So it is stated in the manifest, once, beside the package name it goes with.
// Required only where the descriptor carries a record — a package with no
// record has no record-tier test file to write and therefore nothing to import.
const importPathOption = "import_path"

// defaultReceiver is what the methods take where the manifest says nothing.
//
// A name rather than a letter, and deliberately not the initial of anything:
// the receiver is the same identifier in every method of the generated package,
// so it cannot be a mnemonic for a particular record, and a letter that happens
// to be one reads as though it were.
const defaultReceiver = "x"

// options are the `--opt k=v` pairs this generator understands.
//
// The vocabulary is this generator's own: docs/plugin/SPEC.md has cpybkc pass
// options through without interpreting one or checking one against a declared
// list, because no plugin declares one. What follows from that is the rule
// below — an option this generator does not recognise is a failure rather than
// something ignored, since an ignored option is a line in a checked-in manifest
// that reads as configuration and does nothing.
type options struct {
	// packageName is the package clause every generated file carries.
	packageName string

	// receiver is the identifier the decode and encode methods declare their
	// receiver under, empty where the invocation named none.
	receiver string

	// importPath is the Go import path the generated package will have once it
	// is merged, empty where the invocation named none. See
	// [importPathOption].
	importPath string
}

// receiverName is the receiver the methods take, which is [defaultReceiver]
// where the invocation named none.
func (o options) receiverName() string {
	if o.receiver == "" {
		return defaultReceiver
	}

	return o.receiver
}

// set applies one `--opt` argument.
func (o *options) set(pair string) error {
	key, value, separated := strings.Cut(pair, "=")
	if !separated {
		return fmt.Errorf("the option %q is not k=v: a key is separated from its value by =", pair)
	}

	if key == "" {
		return fmt.Errorf("the option %q has no key", pair)
	}

	switch key {
	case packageNameOption:
		if o.packageName != "" {
			return fmt.Errorf("%s %s was passed more than once", optFlag, key)
		}

		if !isPackageName(value) {
			return fmt.Errorf(
				"%s=%q is not a Go package name: it must be an identifier that is neither a keyword nor a blank",
				packageNameOption, value)
		}

		o.packageName = value
	case receiverOption:
		if o.receiver != "" {
			return fmt.Errorf("%s %s was passed more than once", optFlag, key)
		}

		if !isReceiverName(value) {
			return fmt.Errorf(
				"%s=%q is not a Go method receiver: it must be an unexported identifier that is not a blank",
				receiverOption, value)
		}

		o.receiver = value
	case importPathOption:
		if o.importPath != "" {
			return fmt.Errorf("%s %s was passed more than once", optFlag, key)
		}

		if !isImportPath(value) {
			return fmt.Errorf(
				"%s=%q is not a Go import path: it is a non-empty slash-separated path carrying no space, quote, control character or empty element",
				importPathOption, value)
		}

		o.importPath = value
	default:
		return fmt.Errorf("this generator has no option %q; it takes %s, %s and %s",
			key, packageNameOption, receiverOption, importPathOption)
	}

	return nil
}

// check refuses an option set that cannot be generated from.
func (o *options) check() error {
	if o.packageName == "" {
		return fmt.Errorf(
			"%s %s=<name> is required: it names the Go package the generated files declare, and nothing else in the invocation says what it is",
			optFlag, packageNameOption)
	}

	return nil
}

// isPackageName reports whether s can stand after `package` in a Go file.
//
// The compiler's rule, not a style: an identifier, not the blank identifier,
// and not `init` — a package may not take the name of the function every
// package may declare. Keywords need no test of their own, because
// [token.IsIdentifier] already refuses one.
//
// Casing is left alone deliberately; an adopter generating into a package whose
// name they chose is not this program's to overrule, and #50 is where the
// identifiers this generator invents are held to a convention.
func isPackageName(s string) bool {
	return token.IsIdentifier(s) && s != "_" && s != "init"
}

// isReceiverName reports whether s can stand as a method receiver.
//
// An identifier, not the blank identifier, and unexported — because every
// identifier this generator munges from a name is exported, so an unexported
// receiver is one that cannot collide with a field of the record it is a
// receiver for, whatever the copybook spells.
func isReceiverName(s string) bool {
	return token.IsIdentifier(s) && s != "_" && !token.IsExported(s)
}

// isImportPath reports whether s can stand inside the quotes of an import
// declaration.
//
// The compiler's rule and no more: the specification leaves an import path
// implementation-defined beyond "a non-empty string using only characters
// belonging to Unicode's L, M, N, P and S categories", and the module system
// layers conventions on top of it that are not this generator's to enforce. A
// path is checked for the faults that would produce a file which does not
// compile — an empty path, an empty element, whitespace, a quote, a control
// character — and everything else is taken as written, because refusing a path
// `go build` would have accepted is a refusal an adopter cannot work around.
func isImportPath(s string) bool {
	if s == "" || strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") {
		return false
	}

	if slices.Contains(strings.Split(s, "/"), "") {
		return false
	}

	for _, r := range s {
		if r == '"' || r == '`' || r == '\\' || unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}

	return true
}
