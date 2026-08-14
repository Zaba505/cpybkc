// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/Zaba505/cpybkc/irpb"
)

const (
	// pluginName is what this executable is called, and the whole of how cpybkc
	// identifies it: docs/plugin/SPEC.md resolves the manifest's `go` to
	// `cpybkc-gen-go` on PATH, and nothing inside the executable is consulted.
	//
	// It is stated rather than taken from os.Args[0], because a diagnostic
	// naming whatever path a caller happened to invoke this program by is one
	// the reader cannot compare against a release.
	pluginName = "cpybkc-gen-go"

	// supportedIRVersion is the highest IR version this generator implements.
	//
	// docs/ir/SPEC.md makes the version a single monotonic integer with no
	// newer-but-compatible case to detect: an addition a generator can ignore
	// and still be correct about leaves it alone, and every addition it cannot
	// advances it. So "the highest it implements" and "the only one it accepts"
	// are the same number today, and this constant is what moves when that
	// stops being true.
	supportedIRVersion = irpb.IrVersion_IR_VERSION_1

	// versionField is [irpb.Descriptor]'s version field number.
	//
	// ir.proto gives the version field number 1 so that it precedes the node
	// list on the wire for a producer serializing in field-number order, which
	// is what makes reading it first cheap as well as required.
	versionField = protowire.Number(1)
)

// version is the version this build was stamped with, in this author's scheme,
// and the third fact a refusal names. It is not the string the refusal carries;
// see [reportedVersion] for the one leading `v` between the two.
//
// It is neither the IR's version nor the Go module's: one IR version outlives
// many releases of this program, and the user reading a refusal needs to know
// which of the two ends is behind.
//
// docs/plugin/SPEC.md leaves a plugin's versioning scheme to its author, so
// what this is pinned to is a convention of this repository rather than a
// contract: the generator a release publishes reports that release's version,
// and a build made outside one reports 0.0.0-dev. A generator reporting
// 0.0.0-dev from a released image would give the "descriptor IR version N;
// cpybkc-gen-go 0.0.0-dev implements …" refusal a version number nobody can map
// to a release, which is the whole use the third fact is put to.
//
// # How it gets here
//
// .dagger/image.go's generatorBinary passes `-X main.version=<version>` when it
// cross-compiles this program for the generator image, and a build nobody
// stamped keeps the value written here. It is a var and not a const because the
// linker silently ignores a stamp naming a constant — the failure #181 was
// filed for, where the pipeline appears to be stamping a version and the
// program keeps reporting the tree's.
//
// The name is `version` rather than the `pluginVersion` this was called because
// the stamp names it: the base image's CLI is stamped `main.version` by the
// shared archetype, with the name fixed by that module, and one convention for
// "which build am I running" across both of this repository's commands is worth
// more than a name that repeated what the package it lives in already says.
//
// `dagger call image-contract` runs this generator out of the image on a
// descriptor it must refuse and holds the version in that refusal to the version
// the image was built for. One of those images is built under a version nothing
// publishes, and that one is what proves the stamp lands: the version everything
// unreleased is built under is the same string this file defaults to, so under
// it a dropped stamp and a landed one give the identical refusal.
var version = "v0.0.0-dev"

// reportedVersion is the version a refusal carries.
//
// The pipeline states a version as an OCI image tag — `v0.2.0`, because that is
// what the image family is published under — and this generator's scheme, like
// the CLI's, states it as a SemVer 2.0.0 string, `0.2.0`. One leading `v` is
// the whole of the difference, and this is where it goes. A version arriving
// without the `v` is already the reported form and passes through unchanged.
//
// cmd/cpybkc carries the same three lines. They are deliberately not shared:
// this command imports irpb and the standard library and nothing else from this
// repository, so that the surface it exercises is the one an outside plugin
// author has, and a rule this small is cheaper written twice than that is worth
// breaking.
func reportedVersion() string {
	return strings.TrimPrefix(version, "v")
}

// versionOf is the IR version the descriptor's bytes state, read without
// decoding the rest of the message.
//
// docs/plugin/SPEC.md requires a plugin to read the version before anything
// else in the message and to refuse one it does not implement. Reading it off
// the wire, rather than decoding into [irpb.Descriptor] and asking the message,
// is what makes "before anything else" a property of the program instead of a
// promise about the order two statements are written in: a descriptor this
// generator does not implement is refused with no part of it interpreted, and
// with no node it could not have understood ever built.
//
// A descriptor stating no version at all reads as
// [irpb.IrVersion_IR_VERSION_UNSPECIFIED], which is a version this generator
// does not implement and is refused like any other. The last value wins where
// the field appears more than once, which is protobuf's own rule for a scalar
// field.
//
// A version field carrying some other wire type is skipped like any field this
// reader is not looking for, so a readable varint occurrence elsewhere in the
// message still decides the version — again the decoder's own rule, which is
// what keeps this read and a decode of the same bytes from disagreeing. With no
// readable occurrence at all, the descriptor reads as stating no version.
func versionOf(descriptor []byte) (irpb.IrVersion, error) {
	stated := irpb.IrVersion_IR_VERSION_UNSPECIFIED

	for rest := descriptor; len(rest) > 0; {
		number, kind, read := protowire.ConsumeTag(rest)
		if read < 0 {
			return stated, fmt.Errorf("the descriptor is not a protobuf message: %w", protowire.ParseError(read))
		}

		rest = rest[read:]

		if number == versionField && kind == protowire.VarintType {
			value, read := protowire.ConsumeVarint(rest)
			if read < 0 {
				return stated, fmt.Errorf("the descriptor's version is not readable: %w", protowire.ParseError(read))
			}

			rest, stated = rest[read:], irpb.IrVersion(int32(value))

			continue
		}

		read = protowire.ConsumeFieldValue(number, kind, rest)
		if read < 0 {
			return stated, fmt.Errorf("the descriptor is not a protobuf message: %w", protowire.ParseError(read))
		}

		rest = rest[read:]
	}

	return stated, nil
}

// unsupportedVersionError is a descriptor written against an IR version this
// generator does not implement.
//
// Refusing means writing no file beneath --out and exiting non-zero, which is
// what returning this error does: [run] returns before anything is generated,
// and main turns it into an `error:` diagnostic and a non-zero status.
type unsupportedVersionError struct {
	// Descriptor is the IR version the descriptor stated.
	Descriptor irpb.IrVersion
}

// Error implements the error interface.
//
// It names all three facts docs/plugin/SPEC.md's "What the refusal must say"
// requires, because cpybkc never learns there was a mismatch and so cannot
// compose this on the plugin's behalf. With one number the user cannot tell an
// out-of-date generator from an out-of-date CLI; with all three the next step
// is not a guess.
//
// The versions are rendered as the integers docs/ir/SPEC.md makes them rather
// than as the enum's Go spelling, so that a descriptor from a release this
// generator has never heard of prints as a number rather than as the
// placeholder an unknown enum value takes.
func (e *unsupportedVersionError) Error() string {
	if e.Descriptor == irpb.IrVersion_IR_VERSION_UNSPECIFIED {
		return fmt.Sprintf("the descriptor states no IR version; %s %s implements IR version %d",
			pluginName, reportedVersion(), int32(supportedIRVersion))
	}

	return fmt.Sprintf("descriptor IR version %d; %s %s implements IR version %d",
		int32(e.Descriptor), pluginName, reportedVersion(), int32(supportedIRVersion))
}

// Notes is what follows the refusal as `note:` diagnostics.
//
// The three numbers say what the mismatch is; this says what to do about it,
// and it names both directions because the refusal cannot tell which end is
// behind — a descriptor is written by cpybkc and read by this generator, and
// either of the two can be the older.
func (e *unsupportedVersionError) Notes() []string {
	return []string{
		fmt.Sprintf("upgrade %s if the descriptor is newer than it, or generate with a cpybkc that writes IR version %d if it is not",
			pluginName, int32(supportedIRVersion)),
	}
}
