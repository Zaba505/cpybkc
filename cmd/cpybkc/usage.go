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
// The synopsis is the document's, line for line, because the three forms are
// the command set — one command, no subcommands, and no operand in any of them.
// -h is the one single-hyphen spelling that appears here; the document requires
// any other to go undocumented.
const usage = `cpybkc generates code from the copybooks a project's layout names.

Usage:
  cpybkc [--manifest <path>] [--emit-ir <dest> [--emit-ir-format <format>]]
  cpybkc --version
  cpybkc --help

Flags:
  --manifest <path>          the project manifest to read (default: cpybkc.json
                             in the working directory)
  --emit-ir <dest>           write the run's resolved IR descriptor to <dest>,
                             or to standard output for -, instead of generating
  --emit-ir-format <format>  the encoding --emit-ir writes: binary or json
                             (default: binary)
  --version                  print the version and exit
  -h, --help                 print this help and exit

Every input is named by a flag: cpybkc takes no operand, and each flag appears
at most once. docs/cli/SPEC.md is the contract this summarises.
`

// writeUsage writes usage to w.
//
// Which w is the whole of what makes `cpybkc --help | less` work while keeping
// a failing run's output off the data channel: usage goes to standard output
// when it was asked for and to standard error when it accompanies a usage
// error, and nothing else about it changes between the two.
func writeUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, usage)
}
