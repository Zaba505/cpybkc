// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"io"
)

// usage is what --help prints, and what accompanies a usage error.
//
// It is written out rather than assembled from the flag constants: what a flag
// is called is a covered guarantee and what usage says about it is explicitly
// not one, so a generated block would tie the wording docs/cli/SPEC.md leaves
// free to the spellings it fixes, and every wording change would read as a
// change to the surface.
//
// The synopsis is the document's, line for line, because the forms are the
// command set. The fourth line arrived with the subcommand (#183, #214): the
// bare form still leads, because generating is what this command does when
// nothing else is asked of it and that is what every existing command line and
// every published image already means by `cpybkc`. -h is the one single-hyphen
// spelling that appears here; the document requires any other to go
// undocumented.
const usage = `cpybkc generates code from the copybooks a project's layout names.

Usage:
  cpybkc [--manifest <path>] [--emit-ir <dest> [--emit-ir-format <format>]]
  cpybkc init --copybook <path> … --out <dest>
  cpybkc --version
  cpybkc --help

Commands:
  init                       write a layout scaffold from the copybooks it
                             names; cpybkc init --help is its own usage

Flags:
  --manifest <path>          the project manifest to read (default: cpybkc.json
                             in the working directory)
  --emit-ir <dest>           write the run's resolved IR descriptor to <dest>,
                             or to standard output for -, instead of generating
  --emit-ir-format <format>  the encoding --emit-ir writes: binary or json
                             (default: binary)
  --version                  print the version and exit
  -h, --help                 print this help and exit

Every input is named by a flag: cpybkc takes no operand beyond the subcommand
name, and each flag appears at most once — except init's --copybook, which is
given once per file. docs/cli/SPEC.md is the contract this summarises.
`

// initUsage is what `cpybkc init --help` prints, and what accompanies a usage
// error on a line written under `init`.
//
// A usage of its own rather than the whole command's, because docs/cli/SPEC.md
// says which usage --help writes: the named subcommand's where the first
// argument is one. A reader who has typed a verb has already narrowed what they
// are asking about, and answering with every flag of an action they are not
// running is answering a question they did not ask.
//
// What it says about the scaffold's incompleteness is the honest description of
// the split and not a caveat: the part an adopter is qualified to write is
// exactly the part left blank, which is the reason the command can be trusted
// with the half it does write.
//
// The last line is the other half of that honesty, and it is the reason this
// text can exist before the derivation does. #214 lands the vector and #215 the
// scaffold, so `cpybkc init` currently reads its line and fails; a help text
// promising a written file would be documenting a command nobody can run, which
// is what the comment above [usage] refused for the verb itself while it did not
// parse. Saying so here is what lets --help be answered — docs/cli/SPEC.md
// requires it under every subcommand — without it being a promise. The line goes
// when the promise becomes true.
const initUsage = `cpybkc init writes a layout scaffold from the copybooks it is given.

Usage:
  cpybkc init --copybook <path> … --out <dest>

Flags:
  --copybook <path>          a copybook to read; given once per file, at least
                             once, and each path is written into the scaffold
                             as it was typed
  --out <dest>               where the scaffold is written, or - for standard
                             output

The scaffold holds what a copybook decides — a record per 01-level, an
alternative per REDEFINES — and leaves the rest for you: the encoding, the
framing, the discrimination and the sequence are yours to write, and it is not
a valid layout until you have. init reads no manifest and starts no generator.

--help and --version are answered under every subcommand and are not init's own
flags. docs/cli/SPEC.md is the contract this summarises.

Not implemented in this build: init reads its arguments and reports that it
cannot derive a scaffold from them yet. It exits 1 without writing anything.
`

// writeUsage writes the usage of the action named to w.
//
// Which w is the whole of what makes `cpybkc --help | less` work while keeping
// a failing run's output off the data channel: usage goes to standard output
// when it was asked for and to standard error when it accompanies a usage
// error, and nothing else about it changes between the two.
//
// Which usage is decided here rather than at the two call sites, so that a line
// written under a subcommand gets the same answer whether it asked for usage or
// earned it. An action this does not know is the whole command's usage, which is
// what `cpybkc bogus --help` is owed: docs/cli/SPEC.md answers the question
// rather than complaining about the verb, and an unrecognised verb is the
// commonest way to arrive at it.
func writeUsage(w io.Writer, subcommand string) {
	text := usage
	if subcommand == initSubcommand {
		text = initUsage
	}

	_, _ = fmt.Fprint(w, text)
}
