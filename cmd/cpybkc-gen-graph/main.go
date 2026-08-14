// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Command cpybkc-gen-graph reads a resolved cpybkc IR descriptor and writes a
// state-machine diagram of the sequencing automaton it describes.
//
//	cpybkc-gen-graph --descriptor <path> --out <dir> [--opt k=v ...]
//
// docs/plugin/SPEC.md is the contract, and this command implements it rather
// than restating it. The argument vector, the descriptor's encoding and
// lifetime, what may be written beneath --out, the stderr diagnostic format and
// the determinism required of the output are all that document's; README.md
// beside this file is the plugin's own half — the options it takes and what it
// does with them.
//
// # What it is for
//
// A resolved descriptor is checkable in two ways today and neither shows a
// person whether their file is described right. `cpybkc --emit-ir` renders the
// IR as JSON, which is a flat node set referring to itself by identifier —
// deliberately, and nobody can read a record order out of it. What
// cmd/cpybkc-gen-go writes is correct Go and, for this purpose, unreadable: the
// automaton arrives as a switch over integer state indices.
//
// So the question an adopter most wants answered before they trust a layout —
// the right records, in the right order, told apart on the right bytes, at the
// right offsets — has no artifact behind it. This is that artifact. The
// automaton is already first-class in the IR, as State, Transition, Predicate,
// Guard, Binding and Register nodes, so drawing it is a read of nodes that
// exist rather than resolution work of its own.
//
// # Why a second generator
//
// cmd/cpybkc-gen-go's package comment argues that a generator of this project's
// own reaching into internal/ would exercise a surface no outside author has.
// A second generator built the same way is what turns that argument into a
// demonstration: this command imports [github.com/Zaba505/cpybkc/irpb] and the
// standard library and nothing else from this repository, and it is discovered
// by name on PATH and run with the same argument vector a stranger's generator
// is.
//
// Written twice rather than shared, therefore. The version check, the
// diagnostic writer and the argument parser here are cmd/cpybkc-gen-go's read
// of the same contract, and moving either into a package both import would make
// each a consumer of a convenience no third-party generator has — which is the
// one thing this command exists to avoid being.
//
// # What this command does not do yet
//
// Draw. It writes an empty document in the notation `--opt format=` asks for,
// so that the executable is on the contract from the commit that creates it
// rather than from the one that gives it something to say; see [document].
//
// # The version check
//
// cpybkc performs no handshake and no version negotiation of any kind
// (docs/plugin/SPEC.md, "The version check, and why there is no handshake"), so
// nothing has screened the descriptor in front of this program. The IR version
// is therefore read before anything else in the message, and a version this
// generator does not implement is refused with nothing written.
//
// A descriptor a version too new decodes cleanly — protobuf hands an unknown
// field to an old reader and tells it to ignore one — so skipping the check
// does not fail, it draws a diagram of an automaton it understood only in part
// and hands somebody a picture they are about to trust. That is the failure
// this program refuses to have, and it is why the refusal names three facts:
// the descriptor's version, the highest this generator implements, and this
// generator's own version. With no handshake in front of it, no other party is
// in a position to compose that diagnostic.
package main

import (
	"fmt"
	"io"
	"os"

	"google.golang.org/protobuf/proto"

	"github.com/Zaba505/cpybkc/irpb"
)

func main() {
	if err := run(os.Args[1:], os.Stdin); err != nil {
		report(os.Stderr, err)

		// docs/plugin/SPEC.md attaches no meaning to a particular non-zero
		// value, and a plugin may not expect one to be attached: the small
		// integers already belong to the shell. One is the failure.
		os.Exit(1)
	}
}

// run is the whole program with the exit path taken out, so that main owns the
// exit status and nothing else, and so that a test can drive a generation
// without ending the test binary.
//
// stdin is a parameter for the same reason: `--descriptor -` is a form a plugin
// MUST accept even though cpybkc never emits it, and a test that had to move
// this process's own standard input could not assert it.
func run(args []string, stdin io.Reader) error {
	inv, err := parse(args)
	if err != nil {
		return err
	}

	descriptor, err := readDescriptor(inv.descriptor, stdin)
	if err != nil {
		return err
	}

	// Before anything else in the message, and before the message is decoded at
	// all: the version is read off the wire so that a descriptor this generator
	// does not implement is refused without any part of it having been
	// interpreted. See versionOf.
	stated, err := versionOf(descriptor)
	if err != nil {
		return err
	}

	if stated != supportedIRVersion {
		return &unsupportedVersionError{Descriptor: stated}
	}

	// Decoded once the version has been read off the wire, and not before: a
	// descriptor whose bytes are not a cpybkc IR message is a failure to report
	// rather than one to discover after a file has been written.
	var d irpb.Descriptor
	if err := proto.Unmarshal(descriptor, &d); err != nil {
		return fmt.Errorf("the descriptor is not a cpybkc IR message: %w", err)
	}

	return write(&d, inv.out, inv.options)
}

// readDescriptor is the descriptor's bytes, from the path the invocation named
// or from stdin where it named `-`.
//
// The bytes are read whole rather than streamed, because the version has to be
// read before the message is decoded and both are answers about one buffer. A
// descriptor is a resolved layout rather than a data file, so its size is a
// property of the copybooks a project checked in.
func readDescriptor(path string, stdin io.Reader) ([]byte, error) {
	if path == standardInput {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("failed to read the descriptor from standard input: %w", err)
		}

		return b, nil
	}

	// Read, and nothing else: docs/plugin/SPEC.md forbids a plugin to write to
	// the descriptor, rename it or delete it, and forbids it to derive anything
	// from the file's name or the directory holding it. Both are a temporary
	// location cpybkc chose and neither survives the invocation.
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read the descriptor: %w", err)
	}

	return b, nil
}
