// Package argv assembles the argument vectors this module hands the cpybkc
// CLI.
//
// It is a package of its own for the reason internal/imageref is one: the
// module's own package main imports the generated Dagger client, whose init
// panics without a session, so a test beside main cannot run under plain
// `go test` at all. Keeping the part that is only string handling in a package
// that imports no Dagger is what turns the vector a named function builds into
// something a test pins rather than something a comment asserts.
//
// One vector is deliberately not assembled here, and it is Run's. The module
// mirrors the cpybkc CLI (#253), so a function is added here as each command's
// flags are mapped onto it; but the fallback's vector is the caller's own words,
// passed through as written, and a vector this package built for them would be a
// second, unversioned reading of the document the CLI already implements.
package argv

import "fmt"

// manifestFlag names the project manifest a run reads. It is spelled here
// exactly as docs/cli/SPEC.md fixes it, and the separated form is the one the
// document's synopsis writes.
const manifestFlag = "--manifest"

// initSubcommand is the verb a scaffolding run is asked for by, and it is the
// first argument of the vector rather than a flag: docs/cli/SPEC.md reads a
// subcommand name at the first argument and nowhere else, so it cannot follow
// anything.
const initSubcommand = "init"

// copybookFlag names one copybook a scaffolding run reads. It is the one flag
// that repeats — once per file, in the order the scaffold then holds them in —
// and the repetition is what a list argument on the module becomes here.
const copybookFlag = "--copybook"

// outFlag names where the scaffold goes. It has no default in the CLI and none
// here: the caller of [Init] supplies the destination, because where the file
// lands inside the container is a property of how this module mounts things and
// not of the vector.
const outFlag = "--out"

// standardOutput is the dash cpybkc reads as "a stream" wherever it accepts
// one. --manifest is the one flag that refuses it.
const standardOutput = "-"

// Generate is the vector for a generation run over a project mounted at the
// container's working directory.
//
// An empty manifest is a vector of no arguments, which is the whole point of
// the CLI leaving its default action unnamed: generating is what cpybkc does
// when nothing else is asked of it — a promise that survives the one subcommand
// docs/cli/SPEC.md specifies (#183) — and the mounted project's own cpybkc.json
// is what it reads. A manifest names one somewhere else, relative to the mounted
// project root because that is the working directory the CLI resolves a path
// typed on the command line against.
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

// Init is the vector for a scaffolding run over copybooks in a project mounted
// at the container's working directory, with the scaffold written to out.
//
// The subcommand leads and every copybook is a flag of its own, which is the
// whole shape of `cpybkc init --copybook <path> … --out <dest>`. The order the
// copybooks were given is kept, because it is not decoration: the scaffold holds
// one record per 01-level in the order the copybooks were read, so reordering
// the vector would reorder somebody's layout.
//
// A copybook is a path the CLI resolves against its working directory — the
// mounted project's root — and writes into the scaffold as it was typed. That is
// why they arrive here as strings rather than as files: the paths are what the
// adopter has to find again in their own tree.
//
// out is the caller's rather than a constant here, and it is the only argument
// of the two that this package does not get from a user. Where a scaffold lands
// inside the container is a property of how the module mounts things, so it is
// stated there and passed in; what belongs here is that the destination is one
// flag and that the flag is not optional.
//
// Two things are refused before the vector leaves, on the grounds
// [Generate] already states for a manifest on a stream: a vector with no
// copybook in it names nothing to read, and a copybook named "-" names an input
// whose path the scaffold has to record and a stream has none. Neither describes
// a run the CLI could perform for any state of any filesystem, so paying for a
// pull and an exec to be told so buys nothing.
//
// Everything else stays the CLI's, including the rules that are decidable from a
// line. A byte-equal duplicate is a usage error whose diagnostic names the value
// (docs/cli/SPEC.md, "A flag appears at most once"), and leaving it there is what
// keeps this package from holding a second copy of a rule that document is
// entitled to revise. A value naming a directory, or one that cannot be opened,
// is a status 1 that needs a look at the filesystem — a better diagnostic than
// anything guessable from out here.
func Init(copybooks []string, out string) ([]string, error) {
	if len(copybooks) == 0 {
		return nil, fmt.Errorf(
			"%s reads the copybooks it is given and this run names none: a scaffold is derived from the 01-levels "+
				"in them, so there is nothing to derive one from", initSubcommand)
	}

	// The position is named because a list argument has no flag of its own to
	// point at: the caller wrote one --copybook holding several values, and
	// which of them the message is about is otherwise theirs to guess at.
	for i, copybook := range copybooks {
		if copybook == standardOutput {
			return nil, fmt.Errorf(
				"copybook %d cannot be %q: the scaffold states a path for each record's copybook, and a copybook "+
					"on a stream has none to state", i+1, standardOutput)
		}
	}

	args := make([]string, 0, 2*len(copybooks)+3)
	args = append(args, initSubcommand)

	for _, copybook := range copybooks {
		args = append(args, copybookFlag, copybook)
	}

	return append(args, outFlag, out), nil
}
