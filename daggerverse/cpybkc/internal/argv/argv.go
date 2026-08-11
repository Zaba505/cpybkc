// Package argv assembles the argument vectors this module hands the cpybkc
// CLI.
//
// It is a package of its own for the reason internal/imageref is one: the
// module's own package main imports the generated Dagger client, whose init
// panics without a session, so a test beside main cannot run under plain
// `go test` at all. Keeping the part that is only string handling in a package
// that imports no Dagger is what turns the vector a curated function builds
// into something a test pins rather than something a comment asserts.
//
// What is deliberately not here is a builder covering the whole of
// docs/cli/SPEC.md's flag table. The module curates: it maps the run a caller
// almost always wants, and hands everything else to Run, whose vector is the
// caller's own words and is not this package's to assemble.
package argv

import "fmt"

// manifestFlag names the project manifest a run reads. It is spelled here
// exactly as docs/cli/SPEC.md fixes it, and the separated form is the one the
// document's synopsis writes.
const manifestFlag = "--manifest"

// standardOutput is the dash cpybkc reads as "a stream" wherever it accepts
// one. --manifest is the one flag that refuses it.
const standardOutput = "-"

// Generate is the vector for a generation run over a project mounted at the
// container's working directory.
//
// An empty manifest is a vector of no arguments, which is the whole point of
// the CLI having no subcommand: generating is what cpybkc does when nothing
// else is asked of it, and the mounted project's own cpybkc.json is what it
// reads. A manifest names one somewhere else, relative to the mounted project
// root because that is the working directory the CLI resolves a path typed on
// the command line against.
//
// The dash is refused here rather than passed through for the CLI to refuse,
// because the two refusals do not cost the same. A vector that reaches the
// container has already paid for a pull and an exec to say what is knowable
// from the argument alone, and the reason — a manifest's paths are relative to
// the directory holding it, and a manifest on a stream is in no directory — is
// the same reason either way. Every other value is passed through unexamined:
// what is a readable manifest is a question about a filesystem this package
// cannot see, and guessing at it here would refuse runs cpybkc would have
// performed.
func Generate(manifest string) ([]string, error) {
	if manifest == "" {
		return nil, nil
	}

	if manifest == standardOutput {
		return nil, fmt.Errorf(
			"manifest cannot be %q: a manifest's paths are relative to the directory holding it, and a manifest "+
				"on a stream is in no directory", standardOutput)
	}

	return []string{manifestFlag, manifest}, nil
}
