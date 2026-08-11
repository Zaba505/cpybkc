// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"

	"github.com/Zaba505/cpybkc/internal/assemble"
)

const (
	// programName is what this executable is called, and what --version names
	// first.
	//
	// It is stated rather than taken from os.Args[0], because a line naming
	// whatever path a caller happened to invoke this program by is one the
	// reader cannot compare against a release — and the published image's
	// entrypoint is this program under a path of the image's choosing.
	programName = "cpybkc"

	// version is this build's own version, and it is the fact --version exists
	// to report.
	//
	// docs/cli/SPEC.md requires the released version for a build made from a
	// release tag and 0.0.0-dev for one made outside a release. Nothing has
	// been released yet, which 0.0.0-dev says out loud; it moves with the
	// repository's first release tag, the way cmd/cpybkc-gen-go's own version
	// does.
	//
	// A constant rather than a variable a linker stamps: a stamped version is a
	// build whose output depends on how it was invoked, and this repository
	// builds its binaries from the tree alone.
	version = "0.0.0-dev"
)

// producedIRVersion is the IR version this build produces, which is the third
// fact on the --version line.
//
// It is [github.com/Zaba505/cpybkc/internal/assemble.Version] rather than a
// second statement of the same number, because the line is a promise about what
// this build writes into a descriptor and the assembler is what writes it. Two
// constants would be two facts able to disagree, and the day they did, the
// --version line would be the one that lied.
const producedIRVersion = assemble.Version

// versionLine is the one line --version writes.
//
// The IR version is on it because of what a plugin's refusal says: a plugin
// that will not read a descriptor names the descriptor's IR version, the
// highest it implements and its own version, and the user reading that refusal
// has to decide whether to upgrade the generator or pin the CLI. Two of the
// three are in the message; the third is what the CLI in front of them
// produces, and without a way to ask for it the next step is a guess.
//
// One line, so that it can be read by an eye and by a script without either
// needing a parser. What is deliberately not on it is the rest of a build's
// provenance — no commit, no build date, no Go version — because a version
// number is what identifies a release and the rest is recoverable from it.
//
// The IR version is rendered as the integer docs/ir/SPEC.md makes it rather
// than as the enum's Go spelling, which is a name for a number and not the
// number a plugin's refusal quotes.
func versionLine() string {
	return fmt.Sprintf("%s %s (IR version %d)", programName, version, int32(producedIRVersion))
}
