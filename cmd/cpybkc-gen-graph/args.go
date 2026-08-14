// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"strings"
)

// The argument vector docs/plugin/SPEC.md fixes, and nothing else:
//
//	cpybkc-gen-graph --descriptor <path> --out <dir> [--opt k=v ...]
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

	// options are the `--opt k=v` pairs, in this generator's own vocabulary.
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
				"unrecognised argument %q; the vector is `%s %s <path> %s <dir> [%s k=v ...]`",
				flag, pluginName, descriptorFlag, outFlag, optFlag)
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

	// Applied here rather than where an option is read, so that the invocation
	// this function returns is the whole of what the run is, and no reader of it
	// has to know which fields mean "unstated" and which mean what they say.
	inv.options = inv.options.defaulted()

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

// formatOption is the option naming the notation the document is written in.
//
// It is an option rather than two executables because what this generator draws
// is one diagram of one automaton, and the notation it is spelled in is a
// property of where the reader intends to paste it — a Markdown file a forge
// renders, or a `dot` a build turns into an image. Splitting it in two would
// make `cpybkc-gen-mermaid` and `cpybkc-gen-dot` two entries in a manifest that
// mean the same thing, and two names in [internal/plugin.Prefix]'s namespace for
// one generator.
const formatOption = "format"

// The closed set [formatOption] takes, and the default.
//
// Mermaid by default because the document it produces is one a forge renders in
// place: an adopter checking a layout in is looking at the diagram in a pull
// request, and a `dot` there is a file they would have to run something over.
const (
	formatMermaid = "mermaid"
	formatDot     = "dot"

	defaultFormat = formatMermaid
)

// recordsOption is the option saying whether each record's items are drawn
// beneath the state that reads it.
//
// It exists because the two questions this diagram answers are asked at
// different sizes. *Are these the right records, in the right order, told apart
// on the right bytes* is a diagram of the automaton alone, and it stays legible
// for a layout with dozens of records; *and are they the right fields at the
// right offsets* needs every item, and on a real copybook that is a page per
// record. One document cannot be both, and which one a reader wants is not
// something the descriptor states.
const recordsOption = "records"

// The closed set [recordsOption] takes, and the default.
//
// `all` by default because a layout small enough to be checked by eye is the
// case this generator is for, and a reader who wants the automaton on its own
// asks for it — where the reverse default would hide the item offsets from
// somebody who did not know to ask.
const (
	recordsAll  = "all"
	recordsNone = "none"

	defaultRecords = recordsAll
)

// options are the `--opt k=v` pairs this generator understands.
//
// The vocabulary is this generator's own: docs/plugin/SPEC.md has cpybkc pass
// options through without interpreting one or checking one against a declared
// list, because no plugin declares one. What follows from that is the rule
// below — an option this generator does not recognise is a failure rather than
// something ignored, since an ignored option is a line in a checked-in manifest
// that reads as configuration and does nothing.
//
// Both of this generator's options have a default and neither is required, so
// each field is empty until an option states it and carries its default from
// [options.defaulted] onwards — which [parse] applies once the whole vector has
// been read. Emptiness is therefore only a fact about a half-parsed invocation,
// and it is what makes "passed twice" answerable without a second bool per
// option: a value that is already there is one this invocation has already
// stated, and neither closed set contains the empty string.
//
// Defaulting in the struct rather than behind an accessor is what keeps the
// rest of this program from having two ways to ask what the format is, one of
// which is wrong.
type options struct {
	// format is the notation the document is written in.
	format string

	// records is whether each record's items are drawn beneath the state that
	// reads it.
	records string
}

// defaulted is opts with every option the invocation did not state carrying its
// default.
func (o options) defaulted() options {
	if o.format == "" {
		o.format = defaultFormat
	}

	if o.records == "" {
		o.records = defaultRecords
	}

	return o
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
	case formatOption:
		return assign(&o.format, key, value, formatMermaid, formatDot)
	case recordsOption:
		return assign(&o.records, key, value, recordsAll, recordsNone)
	default:
		return fmt.Errorf("this generator has no option %q; it takes %s and %s", key, formatOption, recordsOption)
	}
}

// assign is one option out of a closed set of values, refused when it was
// already stated or when the value is not one of them.
//
// Both of this generator's options are that shape, and the refusals are the two
// docs/plugin/SPEC.md's Options section makes a plugin's own: a key passed twice
// is a manifest stating two things where one is read, and a value outside the
// set is configuration that would otherwise be silently rounded to a default.
func assign(field *string, key, value string, admitted ...string) error {
	if *field != "" {
		return fmt.Errorf("%s %s was passed more than once", optFlag, key)
	}

	for _, one := range admitted {
		if value == one {
			*field = value

			return nil
		}
	}

	return fmt.Errorf("%s=%q is not a value this generator takes; it takes %s",
		key, value, strings.Join(admitted, " and "))
}
