// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"strings"

	"github.com/Zaba505/cpybkc/internal/assemble"
)

// programName is what this executable is called, and what --version names
// first.
//
// It is stated rather than taken from os.Args[0], because a line naming
// whatever path a caller happened to invoke this program by is one the reader
// cannot compare against a release — and the published image's entrypoint is
// this program under a path of the image's choosing.
const programName = "cpybkc"

// version is the version this build was stamped with, and the fact --version
// exists to report. It is not the string the line carries; see
// [reportedVersion] for the one leading `v` between the two.
//
// docs/cli/SPEC.md requires the released version for a build made from a
// release tag and 0.0.0-dev for one made outside a release. The pipeline
// supplies it: .dagger/image.go builds the published binary through the shared
// archetype, which cross-compiles it stamped with `-X main.version=<version>`,
// and a build nobody stamped keeps the value written here — which is
// .dagger/image.go's devVersion, so a plain `go build` and an unreleased
// pipeline build report the same thing. `dagger call image-contract` runs the
// published image's own --version and holds the answer to the version the image
// was built for, so a stamp that stopped landing fails on an ordinary pull
// request rather than in a release.
//
// # Stamped rather than moved by hand, which is the opposite of what this said
//
// It used to be a constant, on the argument that "a stamped version is a build
// whose output depends on how it was invoked, and this repository builds its
// binaries from the tree alone". Two things retired that.
//
// The first is that the argument's premise no longer holds. The version is not
// something a caller chooses: .dagger/release.go's planRelease takes it from the
// canonical version tag pointing at HEAD and refuses a version tag that points
// anywhere else, so it is a ref of the tree being built, exactly as the commit
// the same link step stamps is. Two builds of one commit under one release are
// still byte-identical, which is what "the image is a function of the source"
// was protecting and what makes re-running a release safe.
//
// The second is that there is no longer an option. Since #185 the shared
// archetype compiles this binary and passes that stamp with no seam to turn it
// off, and the linker silently ignores a stamp naming a constant — so keeping
// the constant did not decline the stamp, it hid it. The pipeline would have
// appeared to be stamping a version while --version kept printing whatever the
// tree said, which is what shipped v0.0.0 reporting 0.0.0-dev (#181).
//
// # main.commit is stamped too, and is deliberately not declared here
//
// The same link step passes `-X main.commit=<short sha>`. No such variable
// exists in this package and none should: docs/cli/SPEC.md puts the rest of a
// build's provenance off the version line in as many words, so a commit
// variable here would be one nothing reads. The linker ignoring it is the
// intended outcome rather than an oversight — which is worth saying, because it
// is indistinguishable from the oversight that made this file's version a
// constant.
var version = "v0.0.0-dev"

// reportedVersion is the version the --version line carries.
//
// The pipeline states a version as an OCI image tag — `v0.2.0`, because that is
// what docs/container/SPEC.md's tag table publishes and what the archetype is
// handed — and docs/cli/SPEC.md states it as a SemVer 2.0.0 string, `0.2.0`.
// One leading `v` is the whole of the difference, and this is where it goes.
//
// Trimmed here rather than at the seam, because the seam is somebody else's:
// the archetype fixes the stamp's value to the version it publishes under, so
// the program printing the line is the only place left that can make the line
// the shape its own contract requires. A version arriving without the `v` — a
// plain `go build`, or a stamp somebody passed by hand — is already the reported
// form and passes through unchanged.
//
// cmd/cpybkc-gen-go carries the same three lines for the same reason and they
// are deliberately not shared: that command imports irpb and the standard
// library and nothing else from this repository, so that it exercises the
// surface an outside plugin author has (see its package comment). A rule this
// small is cheaper written twice than it is worth breaking that for.
func reportedVersion() string {
	return strings.TrimPrefix(version, "v")
}

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
	return fmt.Sprintf("%s %s (IR version %d)", programName, reportedVersion(), int32(producedIRVersion))
}
