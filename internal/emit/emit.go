// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package emit writes a resolved IR descriptor where the CLI's --emit-ir flag
// names, in the protobuf binary wire encoding a generator plugin is handed.
//
// # Why the flag exists
//
// The layout parser and the resolver live in the CLI, so a third party who
// wants what they produce has exactly two ways to get it: write a generator
// plugin, or ask the CLI for the descriptor and read it directly. --emit-ir is
// the second, and it is the supported one — the reuse path for a consumer that
// is not a code generator at all, and the tool's primary debugging surface when
// a generator's output is wrong and the question is whether the IR or the
// generator is at fault.
//
// It is stable in the sense that matters to somebody building on it: the flag
// writes a [github.com/Zaba505/cpybkc/irpb.Descriptor] and nothing else, at the
// version that message carries, encoded the one way this package encodes it.
// What is *in* a descriptor is docs/ir/SPEC.md's to say and its own version
// field's to move; a consumer whose build has no protobuf code generation in it
// decodes the bytes with the published FileDescriptorSet
// ([github.com/Zaba505/cpybkc/irpb.FileDescriptorSet]) and needs nothing from
// this repository but the two artifacts a release attaches.
//
// # A path, and - for standard output
//
// The operand is a filesystem path, or [Stdout] — "-", POSIX's spelling for a
// standard stream as an operand. Both are needed and neither is a convenience
// over the other.
//
// A path is what lets a plugin author run a plugin by hand against the exact
// bytes the CLI would have handed it. The file outlives the invocation that
// wrote it, so a failing generation can be replayed against a descriptor
// captured months earlier, and the answer is about those bytes rather than
// about a pipeline nobody can reproduce.
//
// Standard output is what gets a descriptor out of a container without a bind
// mount. `--emit-ir -` redirected on the host needs no writable volume, no
// matching UID on an output directory and no second command to copy the file
// back out — the difference between one `docker run` and a mount whose
// ownership is the caller's problem to get right, on an image that deliberately
// does not run as root.
//
// What standard output asks of its caller is that nothing else writes to that
// stream while a descriptor is going to it. Diagnostics belong on stderr
// whatever the destination is; a progress line interleaved into the wire
// encoding produces a descriptor that fails to decode at whatever field the
// line happened to land in, rather than anywhere near the mistake.
//
// # Determinism
//
// [Marshal] is the only place this repository encodes a descriptor, and it
// encodes deterministically: equal descriptors produce equal bytes, so a
// consumer that diffs two emissions is reading a change to the resolved layout
// and never a change of encoder mood. Ordering *inside* the message — the node
// identifiers and the order of the node list — is the producer's obligation and
// is stated as one by docs/ir/SPEC.md, "Identity, ordering and determinism";
// this package cannot supply it and does not pretend to. What it supplies is
// the other half, so that identical descriptors cannot come out as different
// files.
//
// The IR schema has no map field today, which is the one construct whose
// encoding would otherwise follow Go's randomised map iteration. Setting the
// option anyway is what keeps that a property of the encoder rather than a
// silent requirement on the schema, discovered by whoever adds the first map
// and diffs two runs.
//
// # It runs no generator
//
// Emitting is a terminal action, not a stage of generating. Nothing here loads
// a plugin, resolves a name on PATH or starts a process, and a caller that was
// asked for a descriptor and nothing else is finished once [Write] returns. The
// obligation that survives into the CLI is the same one stated the other way
// round: when --emit-ir is the only action requested, the command exits after
// it rather than going on to invoke a generator whose output nobody asked for.
package emit

import (
	"fmt"
	"io"
	"os"

	"github.com/Zaba505/cpybkc/irpb"
	"google.golang.org/protobuf/proto"
)

const (
	// Flag is the name of the CLI flag this package is behind, without its
	// leading dashes.
	//
	// It is spelled once, here, because the flag's name appears in the command
	// that defines it, in the error a bad operand produces and in the tests
	// that drive both. Three literals would be three places for it to be
	// renamed in two of.
	Flag = "emit-ir"

	// Stdout is the --emit-ir operand asking for the descriptor on standard
	// output rather than in a file.
	//
	// POSIX's Utility Conventions give "-" this meaning as an operand, which is
	// the same reading the plugin contract's --descriptor takes (docs/plugin/
	// SPEC.md). A dash is therefore never a relative path here, and a caller
	// wanting a file of that name spells it "./-".
	Stdout = "-"
)

// Marshal encodes d in the protobuf binary wire encoding: the form a generator
// plugin is handed, the form --emit-ir writes, and the only form this
// repository produces a descriptor in.
//
// The bytes are deterministic; see the package documentation for what that
// covers and what it leaves to the producer of the descriptor.
//
// A nil descriptor is an error. protobuf encodes one to zero bytes and reports
// no failure, which would put an empty file where a descriptor was asked for
// and leave the mistake to be found by whoever tries to decode it — a producer
// that failed to build a descriptor and did not say so is the one thing this
// package can still turn back into a failure.
func Marshal(d *irpb.Descriptor) ([]byte, error) {
	if d == nil {
		return nil, fmt.Errorf("there is no descriptor to encode")
	}

	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("failed to encode the descriptor: %w", err)
	}

	return b, nil
}

// Write encodes d and writes it to dest, which is a filesystem path or [Stdout]
// — in which case the bytes go to out and out alone.
//
// An existing file is replaced. A descriptor is derived entirely from its
// inputs, so re-emitting after a layout changed is the whole point of the flag,
// and refusing the second run would make the ordinary case the one that needs a
// flag to get past.
//
// A path whose parent directory does not exist is an error rather than a
// directory this creates. The operand is a path somebody typed, where a missing
// parent is far more often a typo than an intention, and the caller that does
// want a tree made — a pipeline naming an output path in a container filesystem
// it owns — is the one place that knows it.
//
// Nothing is created or truncated until d has encoded, so a call that fails
// leaves whatever was at dest alone rather than replacing a good descriptor
// with a short one.
func Write(dest string, out io.Writer, d *irpb.Descriptor) error {
	if dest == "" {
		return fmt.Errorf("--%s: name a file to write the descriptor to, or %q for standard output", Flag, Stdout)
	}

	b, err := Marshal(d)
	if err != nil {
		return err
	}

	if dest == Stdout {
		if _, err := out.Write(b); err != nil {
			return fmt.Errorf("failed to write the descriptor to standard output: %w", err)
		}

		return nil
	}

	if err := os.WriteFile(dest, b, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", dest, err)
	}

	return nil
}
