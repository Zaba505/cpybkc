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

import (
	"fmt"
	"strings"
)

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

// copyMemberFlag names one member a copybook's `COPY` names and the file
// holding it, written `<member>=<path>` (docs/cli/SPEC.md, "Where the members
// come from"). It is the other flag that repeats — once per member — and the
// repetition is what a list argument on the module becomes here, as it is for
// [copybookFlag].
const copyMemberFlag = "--copy-member"

// outFlag names the layout a scaffolding run writes, with a definitions file per
// copybook beside it. It has no default in the CLI and none here: the caller of
// [Init] supplies the directory, because where the files land inside the
// container is a property of how this module mounts things and not of the
// vector, and the user supplies the layout's name.
const outFlag = "--out"

// emitIRFlag names where the run's resolved descriptor goes, and it is what
// makes a run an emitting one: docs/cli/SPEC.md has it replace generation
// outright, so this flag being in the vector is the whole of what [EmitIR]
// asks for.
const emitIRFlag = "--emit-ir"

// emitIRFormatFlag selects the encoding of that descriptor. It is a usage error
// without [emitIRFlag], which is a rule this package cannot break rather than
// one it enforces: it is only ever written beside it, three lines from here.
const emitIRFormatFlag = "--emit-ir-format"

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
// at the container's working directory, with the layout written to dir under
// the name layout and a definitions file per copybook beside it, and with each
// member a copybook's `COPY` names read from where a copy-member value says.
//
// The subcommand leads and every copybook and every member is a flag of its
// own, which is the whole shape of `cpybkc init --copybook <path> …
// [--copy-member <member>=<path> …] --out <layout>`. The order the copybooks
// were given is kept, because it is not decoration: the scaffold holds one
// record per 01-level in the order the copybooks were read, so reordering the
// vector would reorder somebody's layout. The members keep theirs too, which
// decides nothing — a member map is not ordered — but is what the caller wrote.
//
// A copy-member value is passed through exactly as given. Splitting it at the
// first `=` outside a literal, reading the member as a `COPY` writes it, and
// refusing a value with no `=`, an empty or malformed member, an empty path, the
// path "-" and two values naming one member are all the CLI's usage errors,
// naming the value as typed (docs/cli/SPEC.md, "Where the members come from");
// a second reading of that grammar here is one that could disagree with it. The
// path in it resolves against the mounted project's root, as a copybook's does.
//
// A copybook is a path the CLI resolves against its working directory — the
// mounted project's root — and writes, as it was typed, into the header of the
// definitions file converted from it. That is why they arrive here as strings
// rather than as files: the paths are what the adopter has to find again in
// their own tree.
//
// dir is the caller's rather than a constant here, and it is the one argument
// this package does not get from a user. Where the files land inside the
// container is a property of how the module mounts things, so it is stated there
// and passed in. layout is the user's: it is the name the layout has in the
// directory the module hands back, and it is the name a manifest's `layout` field
// then has to match, which is why this module does not choose it any more than
// the CLI does (docs/cli/SPEC.md, "Where the scaffold is written").
//
// Four things are refused before the vector leaves, on the grounds [Generate]
// already states for a manifest on a stream: a vector with no copybook in it
// names nothing to read; a copybook named "-" names an input a definitions file
// has to be named after, and a stream has no name; a layout named "-" asks for a
// stream where the CLI writes a set of files; and a layout name that is not a
// bare file name — empty, ".", "..", or holding a slash — names somewhere other
// than the one directory this module hands back. None describes a run the CLI
// could perform into that directory for any state of any filesystem, so paying
// for a pull and an exec to be told so buys nothing.
//
// Everything else stays the CLI's, including the rules that are decidable from a
// line. A byte-equal duplicate is a usage error whose diagnostic names the value
// (docs/cli/SPEC.md, "A flag appears at most once"), and so are two copybooks
// whose definitions files would share a name or one whose definitions file would
// be named as the layout is ("The vector `init` takes"); leaving them there is
// what keeps this package from holding a second copy of rules that document is
// entitled to revise. A value naming a directory, or one that cannot be opened,
// is a status 1 that needs a look at the filesystem — a better diagnostic than
// anything guessable from out here.
func Init(copybooks, copyMembers []string, dir, layout string) ([]string, error) {
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
				"copybook %d cannot be %q: a definitions file is named after its copybook, and a copybook on a "+
					"stream has no name to name one after", i+1, standardOutput)
		}
	}

	switch {
	case layout == standardOutput:
		return nil, fmt.Errorf(
			"out cannot be %q: %s writes a layout and a definitions file per copybook beside it, and a stream is "+
				"no directory for them to be in", standardOutput, initSubcommand)
	case layout == "" || layout == "." || layout == ".." || strings.Contains(layout, "/"):
		return nil, fmt.Errorf(
			"out is the layout's file name in the directory this function hands back, with its definitions files "+
				"beside it, and %q is not a file name; name the file here and choose where it goes at export",
			layout)
	}

	args := make([]string, 0, 2*len(copybooks)+2*len(copyMembers)+3)
	args = append(args, initSubcommand)

	for _, copybook := range copybooks {
		args = append(args, copybookFlag, copybook)
	}

	for _, member := range copyMembers {
		args = append(args, copyMemberFlag, member)
	}

	return append(args, outFlag, strings.TrimSuffix(dir, "/")+"/"+layout), nil
}

// EmitIR is the vector for an emitting run over a project mounted at the
// container's working directory, with the resolved descriptor written to dest.
//
// It is a generating run's vector plus the flag that replaces the generating,
// which is why [Generate] builds the front of it rather than this function
// restating the manifest rule. Which descriptor is emitted follows from which
// manifest was read (docs/cli/SPEC.md, "Which descriptor is emitted"), so a
// project keeping its manifest somewhere else says so here exactly as it does
// for a generating run — and the dash is refused here for the same reason and
// with the same message, because it is the same argument.
//
// dest is the caller's rather than a constant here, for the reason [Init]'s dir
// is: where the descriptor lands inside the container is a property of how the
// module mounts things. It is not examined. The one value that would be worth
// refusing is the dash, and no caller of this package can pass it — a
// File-returning function has no stream to hand back, so `--emit-ir -` stays
// the escape hatch's spelling and never reaches here.
//
// An empty format is no --emit-ir-format at all, which leaves the CLI applying
// its own default. That is the [Generate] treatment of an empty manifest and it
// is chosen for the same reason: the default encoding is docs/cli/SPEC.md's to
// name, and a vector spelling it out here would be this module's second reading
// of it, drifting the moment that document changed.
//
// A format that is not one of the spellings that document fixes is passed
// through unexamined, and the refusal is the CLI's. It has one to give —
// `"xml" is not a format; write "binary" or "json"` — and it names the
// alternatives from the parser that decides them, which is a better answer than
// this package could assemble from out here. The two refusals above are
// different in the way [Generate] gives: a stream is wrong for every state of
// every filesystem, and paying for a pull and an exec to be told so buys
// nothing, whereas the set of formats is a thing the CLI is entitled to grow.
func EmitIR(manifest, dest, format string) ([]string, error) {
	args, err := Generate(manifest)
	if err != nil {
		return nil, err
	}

	args = append(args, emitIRFlag, dest)

	if format == "" {
		return args, nil
	}

	return append(args, emitIRFormatFlag, format), nil
}
