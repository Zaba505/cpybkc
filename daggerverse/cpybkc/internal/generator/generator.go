// Package generator names a generator the way cpybkc finds one, and refuses the
// names that would assemble into something cpybkc never searches for.
//
// It is a package of its own, rather than three functions in package main, for
// the reason internal/imageref is one: the module's own package main imports the
// generated Dagger client, whose init panics when no Dagger session is present,
// so a test beside main cannot run under plain `go test` and the strings composed
// here would be asserted by a comment instead of pinned by a test. Nothing here
// imports Dagger, so `go test ./internal/...` runs anywhere.
//
// Two strings are composed, and they are the module's whole knowledge of how a
// generator is named: the filename inside the image, which docs/plugin/SPEC.md
// fixes, and the repository a published generator image is pulled from, which is
// this project's publishing rule rather than anybody's contract. Everything else
// about a generator — the argument vector, the descriptor, the exit codes,
// determinism — is docs/plugin/SPEC.md's, holds with no container anywhere in the
// picture, and is not this module's business.
package generator

import (
	"errors"
	"fmt"
	"strings"
)

// executablePrefix and repositorySuffix are the two spellings of "the generator
// called <name>", and they are deliberately not one constant: the first is a
// filename cpybkc resolves on PATH (docs/plugin/SPEC.md, Discovery) and the
// second is a registry repository this project happens to publish under. They
// look alike today and answer to different owners, so a change to either is not
// a change to the other.
const (
	executablePrefix = "cpybkc-gen-"
	repositorySuffix = "-gen-"
)

// Executable is the filename cpybkc searches PATH for when a manifest asks for
// name.
//
// This is docs/plugin/SPEC.md's discovery rule and the only part of the plugin
// contract this module implements. An installed executable takes this name
// whatever it was called before it arrived, because the suffix of the filename
// *is* the generator's name and nothing inside the file is consulted to discover
// it — so renaming on the way in is not a liberty, it is the mechanism.
func Executable(name string) string {
	return executablePrefix + name
}

// Repository is where this project publishes the generator image for name,
// derived from the repository the CLI image itself was pulled from.
//
// The derivation is a fact about how cpybkc publishes rather than a promise the
// base-image contract makes, which is why it lives here and not in
// docs/container/SPEC.md. Deriving it from the caller's --repository rather than
// from a constant is what makes a mirror, an internal registry or an air-gapped
// copy redirect the whole family at once: a caller who moved the CLI image and
// not its generators would otherwise reach back to ghcr.io from inside a network
// that cannot see it, on the second call rather than the first.
func Repository(repository, name string) string {
	return repository + repositorySuffix + name
}

// CheckName enforces docs/plugin/SPEC.md's two **MUST**s on a generator name:
// non-empty, and no `/`.
//
// It is checked here because name is interpolated into a path inside the image
// and into a registry repository, and neither failure says what went wrong. A
// name carrying a separator writes the executable to a directory that is not on
// PATH, and the run then fails much later complaining that a generator the caller
// plainly asked for cannot be found; an empty one produces `cpybkc-gen-`, which
// is a filename cpybkc will never search for. A name of `..` needs no rule of its
// own — without a separator it cannot leave the plugin directory.
//
// Those two and no more. The same section's preference for lowercase ASCII,
// digits and hyphens is a **SHOULD** — a convention about names that are easy to
// type, not a constraint on what cpybkc will run — and a module refusing a name
// cpybkc would have resolved would be making the contract smaller from the
// outside, which is the one thing a convenience over a contract must not do.
func CheckName(name string) error {
	switch {
	case name == "":
		return errors.New(
			"a generator name is required: it is the <name> in cpybkc-gen-<name>, which is the filename cpybkc resolves on PATH")
	case strings.Contains(name, "/"):
		return fmt.Errorf(
			"generator name %q contains a /: a name is a single filename component (docs/plugin/SPEC.md, Discovery), and one carrying a path separator would put the executable somewhere cpybkc never searches",
			name)
	}
	return nil
}
