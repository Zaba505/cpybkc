// This file checks the companion Dagger module — the consumer-facing one in
// daggerverse/cpybkc that runs the published image for somebody else's pipeline
// (#61). It is not that module; it is this pipeline's opinion about it.
//
// Four things are checked here, and they are separate because they fail for
// unrelated reasons and a run should say which.
//
// CompanionCi runs the standard Go pipeline over the module, exactly as IrCi
// does for irpb/ and for the same reason: it is a separate Go module, so
// `go test ./...` from the repository root stops at its go.mod and never
// descends. Without this stage the module published to strangers is the one part
// of the tree nothing checks.
//
// EngineLock asserts the two modules agree about the Dagger engine. That one
// needs a check rather than a convention because the tool only catches half of
// it — see the function's comment, which records what was measured rather than
// what was assumed.
//
// CliSurface asserts the module has an answer for every flag the CLI accepts,
// and that the answer is the one the module's stance says it should be. The
// module mirrors the CLI (#253): a flag's answer is an argument on a function
// named for the command it belongs to, and a flag that reaches the module
// through the escape hatch instead is an exception carrying the argument for
// itself. Adding a flag to the CLI therefore fails CI until somebody writes one
// or the other down.
//
// CompanionModule runs the module's functions — over the image this pull request
// built rather than over the last release (#64). The three above read the
// module's source, its dagger.json and its function names; this one is the only
// place the calls are made, so it is the only place a composition that no longer
// composes can fail.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"dagger/cpybkc/internal/coverage"
	"dagger/cpybkc/internal/dagger"
	"dagger/cpybkc/internal/surface"
)

// companionModuleDir is the companion module's directory, which is also its
// published module ref — `github.com/Zaba505/cpybkc/daggerverse/cpybkc`. A
// Dagger module ref is a directory path inside a tag, so renaming this directory
// deletes the old ref rather than deprecating it; CONTRIBUTING.md's *The
// directory name is the public ref, so it is chosen once* is the argument, and
// this constant is one of the two places a rename would have to be made
// deliberately.
const companionModuleDir = "daggerverse/cpybkc"

// CompanionCi runs the same standard pipeline over the companion Dagger module.
//
// It is a third call rather than a wider source directory for the reason IrCi is
// a second one: daggerverse/cpybkc is a separate Go module, and `go test ./...`
// from the repository root stops at a nested go.mod. The module a stranger
// actually calls would otherwise be checked by nothing.
//
// What it really runs is the module's internal/imageref package, which is where
// the reference assembly lives precisely so that it can be tested by `go test`
// alone. The module's own package main imports the generated Dagger client,
// whose init panics without a session, so a test beside main could not run here
// at all — that constraint is why the pure part is a package of its own.
//
// It is handed the same .golangci.yml, so all three Go modules are linted
// against one configuration rather than one each.
//
// +check
// +cache="session"
func (m *Cpybkc) CompanionCi(ctx context.Context) error {
	return m.goChain(m.Source.Directory(companionModuleDir)).Ci(ctx)
}

// EngineLock checks that the companion module pins the same Dagger
// engineVersion as this one.
//
// This is a check and not a convention because the dependency edge only enforces
// half of it, which was measured rather than assumed. With the companion pinned
// *above* the engine in use, every root call fails outright:
//
//	failed to resolve dep to source: module requires dagger v0.99.0,
//	but you have v0.21.8
//
// With it pinned *below*, nothing fails: `dagger functions`, `dagger call ci`
// and the rest load and run exactly as before. So the edge catches a companion
// that has run ahead of the engine and misses one that has been left behind —
// and behind is the direction drift actually goes, because lagging is what a
// file nobody edited does. `dagger develop` rewrites each module's own
// dagger.json independently, so the two can diverge through the ordinary
// workflow rather than through neglect.
//
// That is what makes this different from the check #65 was closed for being.
// That one compared a constant against the release it had just followed and
// could not fail; this one fails on a bump somebody did in one module and not
// the other, which is a thing that happens.
//
// The two versions matter together because a caller reaching the companion
// through this repository's pipeline (#64) drives both in one engine, and
// because the daggerverse modules this project depends on pin theirs
// independently, in another repository, and have already drifted. One tree, one
// commit, and one version is what is being bought.
//
// +check
// +cache="session"
func (m *Cpybkc) EngineLock(ctx context.Context) error {
	root, err := engineVersion(ctx, m.Source, "dagger.json")
	if err != nil {
		return err
	}

	companion, err := engineVersion(ctx, m.Source, companionModuleDir+"/dagger.json")
	if err != nil {
		return err
	}

	if root != companion {
		return fmt.Errorf(
			"dagger.json pins engineVersion %s but %s/dagger.json pins %s: the two modules ship from one repository and are driven by one engine, so a bump is one commit that edits both (CONTRIBUTING.md, \"One engineVersion, held by a dependency edge and one check\")",
			root, companionModuleDir, companion)
	}

	return nil
}

// engineVersion reads one module's pinned engine version out of its dagger.json.
//
// It decodes the field rather than grepping for it so that a reformatted
// dagger.json — which `dagger develop` is entitled to write — cannot make the
// check pass or fail on whitespace. An absent or empty engineVersion is an error
// rather than a match against another absent one, because two modules that both
// say nothing are not two modules that agree.
func engineVersion(ctx context.Context, source *dagger.Directory, path string) (string, error) {
	contents, err := source.File(path).Contents(ctx)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}

	var config struct {
		EngineVersion string `json:"engineVersion"`
	}
	if err := json.Unmarshal([]byte(contents), &config); err != nil {
		return "", fmt.Errorf("parsing %s: %w", path, err)
	}

	if config.EngineVersion == "" {
		return "", fmt.Errorf("%s declares no engineVersion", path)
	}

	return config.EngineVersion, nil
}

// generatedFile is the file `dagger develop` writes into a module, and the one
// file in either module that is nobody's opinion about anything. CliSurface
// skips it: it declares methods on the module's own type that the module's
// author did not write, and counting those as functions covering a flag would
// let the check pass on a name nobody chose.
const generatedFile = "dagger.gen.go"

// cliPackageDir is the CLI's own package. Its flag constants are the surface
// CliSurface holds the companion module to.
//
// It is read as a tree rather than as one file or one directory's entries: a
// flag introduced in a new file beside args.go counts, and so does one in a
// subpackage a later refactor moves parsing into. Reading only the immediate
// entries would lose the whole of cmd/cpybkc/internal/ while args.go still
// declared enough flags for the check to look healthy, which is the partial
// failure the empty-result guard below cannot catch.
const cliPackageDir = "cmd/cpybkc"

// companionCoverage is the function each of the CLI's flags is mapped onto by
// name. daggerverse/cpybkc is the CLI daggerized (#253), so this is the ordinary
// answer for a flag and the two tables below are read in that order: a flag
// belongs here unless there is an argument for it not being here.
//
// It was not always. #62 built the module as a deliberately small surface with
// one escape hatch behind it, and under that stance a flag recorded against Run
// was the design working rather than a gap — which is why this map used to hold
// "Run" as a value five times, beside the reasons. #253 settled the other way:
// the module should expose every capability the CLI has as a named function, Run
// is the fallback rather than the plan, and a flag on the escape hatch is an
// exception. So Run is no longer a value this map accepts. An exception is an
// entry in [companionRunExceptions], which is a different thing to write and
// therefore a different thing to read.
//
// Be exact about what an entry claims, because it is less than it looks. It is a
// person's assertion that they thought about this flag and decided where it
// belongs — nothing here proves the named function can *reach* the flag, and
// since Run forwards an arbitrary vector, every flag is reachable through Run by
// construction. Deleting Generate's manifest argument would leave "--manifest"
// pointing at a Generate that no longer maps it, and CliSurface would pass. What
// the check buys is that the assertion has to be re-made by somebody whenever the
// CLI's surface moves, which is exactly the moment it stops being true by
// accident.
//
// Every value names a function on daggerverse/cpybkc's own type, checked to
// exist rather than taken on trust. No count of the entries is written down
// anywhere in this file, deliberately: the prose here used to say how many flags
// reached Run, it read "six" while the map held seven, and a number nobody
// re-derives is exactly what this comment warns about everywhere else.
//
// This is a table in the pipeline rather than a list in the module because it is
// this repository's opinion about the module, in the file that already holds the
// other two (#61). The module states the same stance in prose, in its package
// comment, where a caller reading `dagger call --help` will meet it.
var companionCoverage = map[string]string{
	// The one flag Generate maps by name: it says which project is being
	// generated, which is the question a Dagger caller is already answering with
	// --source.
	"--manifest": "Generate",

	// `init`'s two flags (#183, #214), curated onto one function (#228). This is
	// the move this table was kept for: they were recorded against Run while the
	// module expressed a scaffolding run through the escape hatch alone, and
	// they came off it in the commit that added the function.
	//
	// The name is the CLI's verb rather than the `Scaffold` this comment used to
	// name in prose. There are two commands, and mapping the second one's name
	// straight through is what lets somebody who read docs/cli/SPEC.md find it
	// here — a second name for one command would be this module's own vocabulary,
	// which is what a module mirroring the CLI has the least business growing.
	//
	// Init takes copybook *paths* into --source rather than files, because
	// docs/cli/SPEC.md has the scaffold record each path as it was typed and a
	// layout's paths are relative to the layout; and it supplies --out itself, at
	// a path outside the mounted project, so *nothing at <dest> is ever replaced*
	// holds without the module reasoning about the caller's tree.
	//
	// The one spelling Init does not offer is `--out -`, and that is not an
	// exception in the sense below, because a spelling is not a flag and this
	// table is keyed by flags. --out is curated; a stream destination is what a
	// File-returning function cannot express, and it stays Run's:
	//
	//	dagger call run --source . --args=init,--copybook,posting.cpy,--out,-
	"--copybook": "Init",
	"--out":      "Init",
}

// companionRunExceptions are the CLI's flags that reach the module through Run
// rather than through a function named for a command, each with the argument for
// why.
//
// This table is the whole of what the mirroring stance costs. Under #62's
// curation an uncurated flag needed no defence — the small surface was the design
// — and the record could be one word. Under #253's it needs one, because "this
// flag's answer is the escape hatch" and "nobody has got to this flag" are now
// different claims that used to be written identically. [coverage.Exception] is
// what makes them different to write: a reason is required, and an entry says
// either that the escape hatch is the answer or which issue is writing the
// function.
//
// Neither claim is one this pipeline can verify, and it does not pretend to. It
// enforces that the claim was made, which is the same thing companionCoverage
// enforces about a curated flag and for the same reason: an assertion that has to
// be re-made whenever the CLI's surface moves is one that stops being true on
// purpose rather than by accident.
var companionRunExceptions = map[string]coverage.Exception{
	// Terminal, and mutually constrained: --emit-ir replaces generation outright
	// and --emit-ir-format is a usage error without it. That was the argument for
	// leaving both on Run, and #251 answers it: a curated function need not return
	// a Directory — Init already returns a File — and one function taking the
	// format as an argument makes the illegal pairing unstateable rather than
	// enforced, because there is no call that names a format without asking for an
	// emission.
	//
	// So these are a gap rather than a decision, and the stance is why: the
	// resolved descriptor is the single most useful thing to attach to a bug
	// report, and it is a capability the CLI has.
	"--emit-ir": {
		Reason: "the emission may go to standard output, and --emit-ir-format is only legal beside it; #251 curates " +
			"both onto one File-returning function, which makes the illegal pairing unstateable",
		Tracking: "#251",
	},
	"--emit-ir-format": {
		Reason:   "it is only legal beside --emit-ir, so it is that function's argument rather than a function",
		Tracking: "#251",
	},

	// Questions about the program rather than about a project, and the one place
	// the mirroring stance stops on purpose (#253). Both have a Dagger-native
	// form that is not a function on this module: `dagger call --help` and the
	// per-function documentation are what a caller reaches for instead of
	// --help, and which release runs is something they state at `New --version`
	// rather than ask the CLI afterwards.
	//
	// The reason does not cover everything, which is why Run remains the answer
	// for the rest of it: --version defaults to the moving "v0" tag, so *which
	// tag was asked for* and *which build is running* are different questions,
	// and `run --args=--version` is how the second one is asked.
	"--version": {
		Reason: "a caller states the release at `New --version` rather than asking the CLI afterwards; " +
			"`run --args=--version` is how the moving v0 tag's actual build is read back",
		Settled: true,
	},
	"--help": {
		Reason:  "`dagger call --help` and the per-function documentation are the Dagger-native form of the question",
		Settled: true,
	},

	// The one single-hyphen spelling docs/cli/SPEC.md states. It is recorded
	// rather than filtered out as "a synonym of a covered flag", because that
	// reasoning is true of -h and of nothing else: filtering the whole class would
	// let a future short flag that is nobody's synonym land with this check green,
	// and an entry costs a few lines.
	"-h": {
		Reason:  "the short spelling of --help, and settled with it",
		Settled: true,
	},
}

// runFunction is the escape hatch every entry in [companionRunExceptions]
// reaches its flag through. It is checked to exist for the reason a curated
// function is: an exception naming a route the module no longer has is an
// exception covering nothing.
const runFunction = "Run"

// CliSurface checks that every flag the CLI accepts is one the companion module
// has an answer for, and that the answer is one the module's stance allows.
//
// The surface that can drift away from the module is the flag table: a flag
// added to the CLI is the event that would otherwise leave the module quietly
// unable to express a run somebody can perform by hand. That is what this fails
// on, in five directions — a flag neither table covers, a flag both cover, an
// entry naming a function the module does not declare, an exception that claims
// nothing, and an entry naming a flag the CLI no longer accepts.
//
// The second and fourth are #253's, and they are what the stance cost this
// check. While the module curated deliberately (#62), a flag recorded against
// Run was the design working, so one map with "Run" as an ordinary value said
// everything there was to say. Under a mirroring stance it is the opposite: a new
// flag quietly recorded against Run, passing CI, *is* the drift this check exists
// to catch, and one map cannot tell that from the two flags nobody has curated
// on purpose. Two tables can, because writing an exception is writing a reason
// and saying whether it is settled or tracked — see [coverage.Exception], whose
// rules are pinned by tests rather than by this comment.
//
// What none of that buys is that an exception is *right*. No check can read an
// argument; what this one does is make sure there is one, and make adding a flag
// to the escape hatch a visibly different act from mapping it onto a function.
//
// The CLI has a verb now — `init` (#183, #214) — and the flag table is still the
// whole of what this checks. That is a decision rather than an omission, and it
// is worth stating because the obvious reading of "the surface grew" is that
// this check should have grown with it.
//
// The subcommand *name* is not read, for two reasons. It cannot drift the way a
// flag can: the set is closed at one member by docs/cli/SPEC.md, and a second is
// a change to that document, reviewed, rather than a constant somebody adds on
// the way past. And the reading itself would be unsound — internal/surface keeps
// the string constants shaped like a flag, and a verb is not one, so the only
// way to pick `init` out of this package's constants is to know its spelling in
// advance, which is a check that cannot discover anything it was not told.
//
// What the verb does add is two flags, and those *are* read, exactly as every
// other flag is: --copybook and --out reach this table through the same
// constants the parser matches on, and the companion module's answer for them is
// recorded above beside everything else. So the event this guard exists for —
// the CLI growing a flag the module cannot express — is covered for `init`'s
// flags on the day they landed, without this check learning anything about
// subcommands.
//
// The CLI's side is read from the flag constants the parser matches on, not from
// what `--help` prints and not from docs/cli/SPEC.md's table. The help text is
// deliberately written out rather than assembled from those constants, because
// what a flag is called is a covered guarantee and what usage says about it is
// explicitly not one (cmd/cpybkc/usage.go); a check reading it would fail on a
// rewording and pass on a flag the document forgot. The constants are what
// decides whether an argument is accepted, which is the only reading that cannot
// be true of the document and false of the program.
//
// # What the flag table is not
//
// It is not the CLI's surface, and the mirroring stance is what makes the
// difference matter. This check reads flag *constants*, so a capability the CLI
// has that is not spelled as a flag is invisible to it by construction — and
// there is one today: cpybkc passes its whole environment through to a generator,
// which is how docs/plugin/SPEC.md propagates SOURCE_DATE_EPOCH, and the
// companion module has no way to state an environment at all (#252). No check in
// this repository noticed, and none could have.
//
// So the honest answer to how a non-flag capability is caught is that it is
// caught by a person, at the document that promises it: a change to
// docs/cli/SPEC.md or docs/plugin/SPEC.md that adds a capability which is not a
// flag is answered in the companion module in the same review, and CONTRIBUTING.md
// says so where the stance is argued. That is a weaker guarantee than this check
// and it is stated as one rather than dressed up.
//
// The mechanical version was considered and is not worth having. Anything that
// could notice "docs/cli/SPEC.md changed" is a check comparing a document against
// a copy of itself, which is the shape #65 was closed for being: it fails on
// every edit to that document and on nothing else, so it is answered by updating
// the copy, and a check whose remedy is *tell it what happened* has taught
// nobody anything. What is available instead is that the gap this one missed is
// written down where the stance is, as the standing reminder that the flag table
// is a lower bound on the module's obligations.
//
// # What is outside the reading itself
//
// A flag matched inline on a string literal rather than through a constant — a
// shape the parser does not use, and the one this check cannot see. Everything
// else a constant can be written as is read: package scope or a function body,
// one hyphen or two, a literal or one flag's spelling built from another's.
// The rules are internal/surface's and each of
// them is a test rather than a sentence here, because a drift guard's failure
// mode is staying green and a sentence cannot fail.
//
// Two guards stop this degrading quietly. A read that finds no flags at all is a
// failure rather than a pass, since a renamed package would otherwise read as
// "every flag is covered". And a constant this check could not evaluate is
// reported rather than dropped, because "I could not read this" and "this is not
// a flag" are different things to have learned.
//
// +check
// +cache="session"
func (m *Cpybkc) CliSurface(ctx context.Context) error {
	cli, err := goFiles(ctx, m.Source, cliPackageDir)
	if err != nil {
		return err
	}

	flags, unreadable, err := surface.Flags(cli)
	if err != nil {
		return err
	}

	if len(unreadable) > 0 {
		return fmt.Errorf(
			"%s declares the constants %s with values this check cannot evaluate, so it cannot say whether they "+
				"are flags; a flag's spelling is written as a literal or built from another flag's, and a value "+
				"assembled some other way has to be read by a person instead",
			cliPackageDir, strings.Join(unreadable, ", "))
	}

	if len(flags) == 0 {
		return fmt.Errorf(
			"%s declares no flag constants, so this check compared the companion module against nothing; the CLI's "+
				"flags are the constants cmd/cpybkc/args.go matches on, and a check that cannot find them is not a "+
				"check that passed", cliPackageDir)
	}

	module, err := goFiles(ctx, m.Source, companionModuleDir)
	if err != nil {
		return err
	}

	functions, err := surface.Functions(module, companionType)
	if err != nil {
		return err
	}

	var errs []error

	// Where each flag is recorded, and what it is recorded as reaching, which is
	// the reverse direction's message as well as this one's bookkeeping.
	recorded := map[string]string{}
	for flag, function := range companionCoverage {
		recorded[flag] = function
	}

	// Sorted, because these are diagnostics: two runs over one tree should report
	// the same faults in the same order, and a map's is whatever the runtime felt
	// like.
	for _, flag := range slices.Sorted(maps.Keys(companionRunExceptions)) {
		if function, both := recorded[flag]; both {
			errs = append(errs, fmt.Errorf(
				"%s is recorded both as mapped onto %s's %s and as an exception reaching it through %s: a flag has "+
					"one answer, and the two tables are how a curated flag and an uncurated one are told apart, so a "+
					"flag in both says the module mirrors it and does not at once",
				flag, companionModuleDir, function, runFunction))

			continue
		}

		recorded[flag] = runFunction
	}

	for _, flag := range flags {
		if exception, excepted := companionRunExceptions[flag]; excepted {
			if err := exception.Check(flag); err != nil {
				errs = append(errs, err)
			}

			continue
		}

		function, covered := companionCoverage[flag]
		if !covered {
			errs = append(errs, fmt.Errorf(
				"the cpybkc CLI accepts %s and %s records nothing that covers it: %s is the cpybkc CLI daggerized, "+
					"so the answer is ordinarily an argument on the function named for the command this flag belongs "+
					"to, recorded in companionCoverage in this file; a flag a Dagger argument cannot express is an "+
					"entry in companionRunExceptions instead, carrying the argument for that and the issue curating "+
					"it if one is (#253)",
				flag, companionModuleDir, companionModuleDir))

			continue
		}

		if !slices.Contains(functions, function) {
			errs = append(errs, fmt.Errorf(
				"%s is recorded as covered by %s's %s, and that module declares no such function; either the "+
					"function was renamed and this table was not, or the flag now reaches the module some other way",
				flag, companionModuleDir, function))
		}
	}

	// The route every exception is an exception *to*. An entry saying a flag
	// reaches the module through a function the module no longer declares covers
	// nothing, and it would otherwise be the one claim in either table that is
	// checked against nothing.
	if len(companionRunExceptions) > 0 && !slices.Contains(functions, runFunction) {
		errs = append(errs, fmt.Errorf(
			"companionRunExceptions records flags as reaching %s through %s, and that module declares no such "+
				"function: the escape hatch is what every exception in that table is an exception to, so a rename "+
				"of it is a rewrite of the table rather than a rename",
			companionModuleDir, runFunction))
	}

	for _, flag := range slices.Sorted(maps.Keys(recorded)) {
		if !slices.Contains(flags, flag) {
			errs = append(errs, fmt.Errorf(
				"%s records %s as covered by %s and the cpybkc CLI no longer accepts that flag; a module argument "+
					"is public API for as long as the published module ref exists, so a flag leaving the CLI is a "+
					"decision about the module rather than a line to delete from this table without one",
				companionModuleDir, flag, recorded[flag]))
		}
	}

	return errors.Join(errs...)
}

// companionType is the companion module's own type, whose exported methods are
// the module's functions. It is the directory's name in Go's spelling, and the
// two move together or the module does not build.
const companionType = "Cpybkc"

// goFiles reads a directory tree's Go source, keyed by path.
//
// The whole tree, because a package's flags do not all have to live in its top
// directory and a check that read only the top one would go quiet about a
// subpackage rather than fail about it.
//
// Tests are skipped because a test's constants are its own fixtures: a table
// driving the parser over "--jobs" to assert it is refused would otherwise read
// as the CLI having grown a flag. The generated client is skipped for the reason
// generatedFile gives.
func goFiles(ctx context.Context, source *dagger.Directory, dir string) (map[string]string, error) {
	directory := source.Directory(dir)

	paths, err := directory.Glob(ctx, "**/*.go")
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}

	files := map[string]string{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, generatedFile) {
			continue
		}

		contents, err := directory.File(path).Contents(ctx)
		if err != nil {
			return nil, fmt.Errorf("reading %s/%s: %w", dir, path, err)
		}

		files[dir+"/"+path] = contents
	}

	return files, nil
}

const (
	// companionExampleDir is the committed worked example (#177): the inputs a
	// caller writes, and the tree cpybkc writes for them, checked in whole.
	//
	// It is the golden tree example/regenerate_test.go already holds the CLI to,
	// and reusing it rather than generating something smaller here is the whole
	// assertion. A smoke test saying that the module's calls *ran* would pass on
	// a module that composed an image which generated the wrong thing; what is
	// worth asserting is that the module reproduced the committed example byte
	// for byte, and this repository already has that tree.
	companionExampleDir = "example"

	// neverPulledRepository is the registry repository this check hands the
	// module, and it is deliberately one that cannot exist: `.invalid` is
	// reserved by RFC 2606 and resolves nowhere, ever.
	//
	// It is what turns *this pull request's image, never a published one* from a
	// convention into a failure. The module pulls `<repository>-gen-<name>` only
	// when with-generator is called without an image, and every call below passes
	// one — but a call added later that forgot would pull the *released*
	// generator, generate with it, and pass, quietly checking the last release
	// instead of the change. Pointing the coordinates at a host with no registry
	// behind it makes that call fail instead, naming this constant.
	//
	// #180 made that guard load bearing rather than precautionary, which is worth
	// saying because the change looks like the opposite. Until a release published
	// a generator image there was nothing at `<repository>-gen-go` for a forgotten
	// --image to find, so the call would have failed at the registry anyway; from
	// the first release onwards it resolves, and what it resolves to is the last
	// release rather than the change. This constant is the whole of what stands
	// between those two outcomes.
	//
	// The version is left at the module's own default. Nothing resolves it here
	// for the same reason, and overriding it would only add a second unused
	// value to read.
	neverPulledRepository = "cpybkc.invalid/never-pulled"

	// scaffoldName is what the two scaffolds are called while they are being
	// compared. It is a name for the diff and nothing else: the curated call
	// hands back a File the caller names at export, and the hand-typed one hands
	// back bytes on standard output, so neither side has a name of its own here.
	scaffoldName = "scaffold.sexpr"
)

// exampleCopybooks are the copybooks in [companionExampleDir], as the paths a
// person would type them, and they are all three of them.
//
// All three because the check that would pass on one is not the check worth
// having: `init` derives a record per 01-level across every copybook it was
// given, in the order it was given them, so a single input says nothing about
// whether the module preserved that order or dropped a repetition of the flag.
//
// They are written down rather than globbed out of the example for the reason
// the composed generators are: a fourth copybook added to that directory is a
// decision about what this check covers, and it should fail here loudly rather
// than widen silently.
var exampleCopybooks = []string{"header.cpy", "posting.cpy", "trailer.cpy"}

// exampleInitVector is the hand-typed escape-hatch spelling of the same
// scaffolding run, `--out -` and all.
//
// It is written out here rather than assembled from the module's internal/argv,
// which is the whole point of the comparison below: two statements of one run
// that were arrived at independently, so that a vector this module got wrong
// disagrees with itself instead of agreeing with its own mistake.
var exampleInitVector = []string{
	"init",
	"--copybook", "header.cpy",
	"--copybook", "posting.cpy",
	"--copybook", "trailer.cpy",
	"--out", "-",
}

// CompanionModule drives the companion module's functions over the image this
// pipeline just built, and requires what comes out to be the committed worked
// example byte for byte (#64).
//
// # Why the module's own functions need a check at all
//
// CompanionCi, EngineLock and CliSurface read the module — its Go source, its
// dagger.json and its function names. None of them makes a call, so a module
// whose calls no longer compose into a working image would fail none of them.
// This is the only place `dagger call -m daggerverse/cpybkc …` actually happens,
// and the module implements contracts written elsewhere rather than one of its
// own — docs/cli/SPEC.md's and docs/container/SPEC.md's — which is exactly why a
// broken one is worse than none: a caller reaches for it because they did not
// want to learn the contract underneath, so the failure lands on somebody with
// no reason to know where to look.
//
// # Why it drives the image built here
//
// The module's defaults pull `ghcr.io/zaba505/cpybkc:v0`, which is a *released*
// image. A check that used them would be checking the last release, and would
// keep passing through a pull request that broke both the module and the image
// it drives. So the base image this pipeline just built is passed through the
// same --image argument a caller uses to try an unreleased cpybkc, the generator
// is the one built from this tree, and the coordinates that would resolve
// anything else point at [neverPulledRepository]. Nothing here reaches a
// registry.
//
// That is also the only reason the module takes --image at all, which is worth
// knowing before anybody removes it as unused: it is used here, on every pull
// request.
//
// # What is checked
//
// Both ways of adding a generator (#63), against the same expected tree:
//
//   - with-generator, taking the executable out of a generator image — this is
//     `COPY --from`, and it is the documented adopter path.
//   - with-generator-executable, taking the same executable as a file straight
//     from the build. This is the generator author's path, before anything of
//     theirs is published, and it is the one nobody would notice breaking.
//
// Requiring both to produce the committed example is what makes them
// interchangeable rather than merely both present.
//
// The example runs two generators since #191, so each composition installs two —
// and since #230 both of them go in the same way on each side, because both are
// published as images. `graph` used to be a with-generator-executable in both
// compositions for want of an image to take it out of, which left the documented
// adopter path uncovered for one of this project's two generators; the check for
// it is now the same check `go` gets.
//
// Then the functions that are not part of that pair, because a check that
// exercised only what it needed would leave the rest of the module's surface
// covered by nothing: image, whose plugin directory has to hold the generator
// the CLI resolves on PATH; run, the escape hatch, asked the one question
// docs/cli/SPEC.md requires to succeed against nothing at all; and init, the
// curated scaffolding run (#228), required to hand back the same scaffold the
// escape hatch writes over the same copybooks — see [Cpybkc.checkInit].
//
// Both compositions start from the base image this pipeline built, which carries
// the CLI and no generator — so a generation that succeeded did so with the
// generators these calls installed and not with something that was lying around
// in the image.
//
// Every call is checked and every failure reported rather than stopping at the
// first, because *it works from the image and not from the file* is the finding
// rather than a detail, and each message names the module function that broke.
//
// # One platform
//
// The engine's own, rather than every published one. What varies per platform is
// the executable, and ImageContract already builds and checks the image on each
// of them; what this adds is that the module's calls compose into a working
// image, which is not a property a second architecture can disagree about.
//
// +check
// +cache="session"
func (m *Cpybkc) CompanionModule(ctx context.Context) error {
	platform, err := dag.DefaultPlatform(ctx)
	if err != nil {
		return fmt.Errorf("resolving the engine's platform, which is the one this check runs on: %w", err)
	}

	committed := m.Source.Directory(companionExampleDir)

	// Two generators in each composition, because the committed example runs two,
	// and each composition installs both of them its own way: every generator
	// through with-generator on one side and through with-generator-executable on
	// the other. That is what makes the two routes interchangeable *for each
	// generator* rather than for whichever one happened to have an image.
	//
	// Each is written out rather than looped over [publishedGenerators], because
	// what this check covers is the committed example's manifest and not the set
	// of images a release publishes. The two agree today; a generator published
	// without being added to the example, or the reverse, is a decision that
	// should be made here rather than followed silently.
	fromImage := m.companion(platform).
		WithGenerator(ownGenerator, dagger.CompanionWithGeneratorOpts{
			Image: m.generatorImage(platform, ownGeneratorSpec()),
		}).
		WithGenerator(graphGenerator, dagger.CompanionWithGeneratorOpts{
			Image: m.generatorImage(platform, graphGeneratorSpec()),
		})
	fromExecutable := m.companion(platform).
		WithGeneratorExecutable(ownGenerator, m.generatorBinary(devVersion, platform, ownGeneratorSpec())).
		WithGeneratorExecutable(graphGenerator, m.generatorBinary(devVersion, platform, graphGeneratorSpec()))

	var errs []error

	// The wrapper names the calls the tree came through rather than asserting
	// which of them broke, because it cannot know: a composition that failed
	// before generate ran arrives here too, and the one this check is built to
	// produce — a with-generator that forgot its image, reaching for
	// [neverPulledRepository] — is exactly that shape. The wrapped error is what
	// says which, and it names the registry host when that is the answer.
	if err := m.diffTrees(ctx, committed, fromImage.Generate(committed), "/companion-module/image"); err != nil {
		errs = append(errs, fmt.Errorf(
			"with-generator taking the generator out of an image, then generate, did not hand back the committed "+
				"%s/ (or did not get that far — the wrapped error says which): %w",
			companionExampleDir, err))
	}

	if err := m.diffTrees(ctx, committed, fromExecutable.Generate(committed), "/companion-module/executable"); err != nil {
		errs = append(errs, fmt.Errorf(
			"with-generator-executable taking the generator out of a file, then generate, did not hand back the "+
				"committed %s/ (or did not get that far — the wrapped error says which): %w",
			companionExampleDir, err))
	}

	// Both compositions, not just the first. Generate succeeding says only that
	// *a* working generator was resolvable on PATH; it does not say the plugin
	// directory holds what it should, so a with-generator-executable that
	// installed a second copy under another name would pass every assertion
	// above.
	//
	// What the pair being written down buys is the **set**, and not the spelling.
	// The two entries are the same constants the compositions above were built
	// from, so a constant changed to a wrong spelling moves both together and this
	// would not notice; what it does notice is a generator arriving or leaving. A
	// third entry in example/cpybkc.json, or a third generator in
	// [publishedGenerators], has to be added here too, and fails this check loudly
	// rather than going uncovered if it is not.
	//
	// The spelling is pinned where it is a promise instead:
	// daggerverse/cpybkc/internal/generator's TestExecutable, against the same
	// discovery rule and in a module that cannot import this one.
	composed := []string{generatorExecutable, graphGeneratorExecutable}

	if err := m.checkComposedImage(ctx, fromImage.Image(), composed); err != nil {
		errs = append(errs, fmt.Errorf(
			"with-generator, then image, handed back a container the CLI could not resolve the generators in: %w", err))
	}

	if err := m.checkComposedImage(ctx, fromExecutable.Image(), composed); err != nil {
		errs = append(errs, fmt.Errorf(
			"with-generator-executable, then image, handed back a container the CLI could not resolve the "+
				"generators in: %w", err))
	}

	if err := m.checkInit(ctx, platform); err != nil {
		errs = append(errs, err)
	}

	// --version through the escape hatch, with no project: it is the one
	// invocation docs/cli/SPEC.md requires to succeed touching nothing, so a
	// failure here is run failing to reach the CLI rather than anything about a
	// manifest. The line's shape is checked by the same function Build and the
	// image contract use, because what "this is cpybkc answering" means is a
	// property of the line and not of which container it came out of.
	line, err := fromImage.Run([]string{"--version"}).Stdout(ctx)
	if err != nil {
		errs = append(errs, fmt.Errorf("run did not reach the CLI in the composed image: %w", err))
	} else if err := checkVersionLine(line, devVersion); err != nil {
		errs = append(errs, fmt.Errorf("run reached the CLI in the composed image and %w", err))
	}

	return errors.Join(errs...)
}

// checkInit requires the curated scaffolding function and the hand-typed escape
// hatch to be the same run, over [companionExampleDir]'s copybooks.
//
// # What is asserted, and why it is not what a scaffold should contain
//
// The scaffold `init --source . --copybook …` hands back has to be byte-identical
// to what `run --args=init,--copybook,…,--out,-` writes on standard output over
// the same copybooks. That is cheap, it needs no second reading of
// docs/cli/SPEC.md's `init` section — what a record is derived from, which forms
// are commented, what a note says — and it is exactly the property `init`'s
// escape-hatch entry used to stand in for, back when its flags were recorded
// against Run: a caller who reached for `run` before this function existed
// should get the same file from the function that replaced it.
//
// That is worth keeping in view now that every command is meant to have a
// function (#253), because it generalises. What makes a curated function safe to
// add is that the run it replaces is still spellable through Run and still
// produces the same bytes, which is a check the next curated function can be
// written against as easily as this one was.
//
// What the scaffold *contains* is checked where it is decided:
// internal/scaffold's tests and cmd/cpybkc's. A second expectation here would be
// this pipeline's own reading of that document, and the two would drift.
//
// # No generator, deliberately
//
// This drives the base image with nothing composed into it, where the
// compositions above install two. `init` resolves no layout and runs no
// generator (docs/cli/SPEC.md, "`init` reads no manifest"), and the state it
// runs in is the one an adopter is actually in: before there is a manifest, let
// alone a generator to name in one. A check that reached for a composed image
// would be asserting less while costing more.
//
// The two sides are compared as trees rather than as strings so that a
// disagreement arrives as a diff naming the line, through the same helper the
// tree comparisons above use. [Cpybkc.diffTrees] runs diff(1), which compares
// contents and nothing else, so the two sides may be built differently — one
// from the file cpybkc wrote and one from bytes — without a mode or an owner
// deciding the outcome.
func (m *Cpybkc) checkInit(ctx context.Context, platform dagger.Platform) error {
	bare := m.companion(platform)
	example := m.Source.Directory(companionExampleDir)

	// "did not succeed" rather than "did not reach the CLI", because both are
	// reachable from here: run may fail to reach the CLI at all, and the CLI may
	// be reached and exit non-zero — which is what a typo in the hand-maintained
	// [exampleInitVector] looks like. The wrapped error says which.
	handTyped, err := bare.Run(exampleInitVector, dagger.CompanionRunOpts{Source: example}).Stdout(ctx)
	if err != nil {
		return fmt.Errorf("run --args=init,… over %s/'s copybooks did not succeed: %w",
			companionExampleDir, err)
	}

	curated := dag.Directory().WithFile(scaffoldName, bare.Init(example, exampleCopybooks))
	typed := dag.Directory().WithNewFile(scaffoldName, handTyped)

	if err := m.diffTrees(ctx, typed, curated, "/companion-module/init"); err != nil {
		return fmt.Errorf(
			"init over %s/'s copybooks did not hand back the scaffold `run --args=init,…` writes over the same "+
				"copybooks (or did not get that far — the wrapped error says which): the curated function and the "+
				"escape hatch are the same run spelled two ways, so a difference is one of them being wrong: %w",
			companionExampleDir, err)
	}

	return nil
}

// companion is the module bound to the image this pipeline built, and it is the
// one construction of it: both compositions come through here, so there is no
// arrangement in which one of them is driving a released image and the other the
// change.
//
// The container is passed rather than the coordinates because that is what pins
// bytes — --version and --repository name a tag, and a tag is not a build. See
// [neverPulledRepository] for what the coordinates are set to instead and why
// they are set at all.
func (m *Cpybkc) companion(platform dagger.Platform) *dagger.Companion {
	return dag.Companion(dagger.CompanionOpts{
		Image:      m.baseImage(platform),
		Repository: neverPulledRepository,
	})
}

// checkComposedImage requires an image's plugin directory to hold the generators
// the compositions added, and nothing else.
//
// The directory is listed rather than the image run, because the image is a
// scratch one: there is no shell and no `ls` in it, and the only thing that can
// be executed is cpybkc itself. Reading the directory out of the container
// answers the question anyway, and it answers it about the exact path the CLI
// resolves a generator on — PATH in this image is the plugin directory and
// nothing more.
//
// Exhaustive rather than a containment check, and run over both compositions
// rather than one. A composition that installed the generator twice under two
// names, or left something else behind, is a composition this pipeline should
// report rather than a detail it tolerates — and generate succeeding says
// nothing about it, because a run only needs *a* generator resolvable on PATH.
//
// want is passed rather than written down here because the two callers are
// asking about different images: the published generator image carries the one
// generator a release publishes, and a composition for the committed example
// carries the two that example runs. A single expectation would make one of them
// assert something that is not true of it, and the one it would be false about is
// the published image — which is the one an adopter pulls.
func (m *Cpybkc) checkComposedImage(ctx context.Context, composed *dagger.Container, want []string) error {
	entries, err := composed.Directory(pluginDir).Entries(ctx)
	if err != nil {
		return fmt.Errorf("listing %s in the composed image: %w", pluginDir, err)
	}

	// Those generators and nothing else. The CLI is not in the plugin directory
	// since #185 — the archetype puts an application's own binary in /app and
	// names it absolutely in the entrypoint — so what a composition leaves here is
	// exactly what the composition added.
	want = slices.Clone(want)
	slices.Sort(want)
	slices.Sort(entries)

	if !slices.Equal(entries, want) {
		return fmt.Errorf("%s holds %v where the composition should have left %v: the CLI resolves a generator on "+
			"PATH, and PATH in this image is that directory alone", pluginDir, entries, want)
	}

	return nil
}

// diffTrees requires two directory trees to be identical and reports how they
// differ when they are not.
//
// A real diff rather than a walk comparing file contents, because the failure
// this reports is one somebody has to act on: which file, which line, and what
// changed is the whole of the diagnostic, and a boolean would send them to
// regenerate the example locally to find out. --text because every file cpybkc
// writes is text, and a diff that said only "binary files differ" would report
// the failure without reporting what it was.
//
// The trees are mounted under one directory named by the caller, so a run with
// more than one comparison in it says which comparison the paths in the output
// belong to.
func (m *Cpybkc) diffTrees(ctx context.Context, want, got *dagger.Directory, at string) error {
	wantAt, gotAt := at+"/want", at+"/got"

	_, err := dag.Go().
		Container(m.Source).
		WithMountedDirectory(wantAt, want).
		WithMountedDirectory(gotAt, got).
		WithExec([]string{"diff", "--recursive", "--unified", "--text", wantAt, gotAt}).
		Sync(ctx)

	return err
}
