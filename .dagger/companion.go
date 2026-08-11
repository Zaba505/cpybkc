// This file checks the companion Dagger module — the consumer-facing one in
// daggerverse/cpybkc that runs the published image for somebody else's pipeline
// (#61). It is not that module; it is this pipeline's opinion about it.
//
// Three things are checked here, and they are separate because they fail for
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
// CliSurface asserts the module has an answer for every flag the CLI accepts.
// The module curates deliberately rather than mapping the flag table one-to-one
// (#62), and a curated surface is only safe if adding a flag to the CLI forces
// somebody to say which side of the curation it falls on.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"

	"dagger/cpybkc/internal/dagger"
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
	return dag.Z5Labs().
		GoLib(m.Source.Directory(companionModuleDir), dagger.Z5LabsGoLibOpts{LintConfig: m.LintConfig}).
		Ci(ctx)
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
// CliSurface holds the companion module to, and it is a directory rather than a
// file so that a flag introduced in a new file beside args.go is still seen.
const cliPackageDir = "cmd/cpybkc"

// companionCoverage is what the companion module answers each of the CLI's
// flags with. It is the record the curation of #62 is worth having: the module
// maps the run a caller almost always wants and hands the rest to one escape
// hatch, and without a written record of which flag went where, "curated" and
// "forgotten" are the same thing to read.
//
// Every value names a function on daggerverse/cpybkc's own type, checked to
// exist rather than taken on trust. Run appears five times over because it is
// the escape hatch and that is what an escape hatch looks like when it is
// working; a flag moving from Run to a curated argument is an edit here in the
// same commit that adds the argument.
//
// This is a table in the pipeline rather than a list in the module because it is
// this repository's opinion about the module, in the file that already holds the
// other two (#61). The module states the same split in prose, in its package
// comment, where a caller reading `dagger call --help` will meet it.
var companionCoverage = map[string]string{
	// The one flag Generate maps by name: it says which project is being
	// generated, which is the question a Dagger caller is already answering with
	// --source.
	"--manifest": "Generate",

	// Terminal, and mutually constrained: --emit-ir replaces generation outright
	// and --emit-ir-format is a usage error without it. A Directory-returning
	// function cannot express either — the emission may go to standard output —
	// and two Dagger arguments cannot express "one is only legal beside the
	// other" at all.
	"--emit-ir":        "Run",
	"--emit-ir-format": "Run",

	// Questions about the program rather than about a project, whose answer is a
	// line on standard output. Image() reaches them too; Run is what reaches them
	// with no project and no with-exec spelled out.
	"--version": "Run",
	"--help":    "Run",
}

// CliSurface checks that every flag the CLI accepts is one the companion module
// has an answer for.
//
// docs/cli/SPEC.md fixes cpybkc as one command with no subcommands, so the
// surface that can drift away from the module is the flag table rather than a
// verb list: a flag added to the CLI is the event that would otherwise leave the
// module quietly unable to express a run somebody can perform by hand. That is
// what this fails on, in both directions — a flag no entry covers, and an entry
// naming a flag the CLI no longer accepts.
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
// It follows that a flag introduced *without* a constant — matched inline in a
// string literal — would escape this check. That is a shape the parser does not
// use and one the lint stage would have opinions about, and the alternative,
// reading every string literal in the package, would fail on the diagnostics
// that quote flags at the user. The compromise is stated here rather than left
// as a surprise, and the empty case below is what stops the check degrading into
// one that compares nothing.
//
// +check
// +cache="session"
func (m *Cpybkc) CliSurface(ctx context.Context) error {
	flags, err := cliFlags(ctx, m.Source)
	if err != nil {
		return err
	}

	// A check that found no flags has not passed, it has stopped working: a
	// renamed package, a parse that silently produced nothing, or a constant
	// block written some other way would all read as "every flag is covered".
	if len(flags) == 0 {
		return fmt.Errorf(
			"%s declares no flag constants, so this check compared the companion module against nothing; the CLI's "+
				"flags are the constants cmd/cpybkc/args.go matches on, and a check that cannot find them is not a "+
				"check that passed", cliPackageDir)
	}

	functions, err := companionFunctions(ctx, m.Source)
	if err != nil {
		return err
	}

	var errs []error

	for _, flag := range flags {
		function, covered := companionCoverage[flag]
		if !covered {
			errs = append(errs, fmt.Errorf(
				"the cpybkc CLI accepts %s and %s records nothing that covers it: add it to companionCoverage in "+
					"this file, against the curated function that maps it or against Run, which is the escape hatch "+
					"an uncurated flag reaches through (#62)",
				flag, companionModuleDir))

			continue
		}

		if !slices.Contains(functions, function) {
			errs = append(errs, fmt.Errorf(
				"%s is recorded as covered by %s's %s, and that module declares no such function; either the "+
					"function was renamed and this table was not, or the flag now reaches the module some other way",
				flag, companionModuleDir, function))
		}
	}

	for _, flag := range slices.Sorted(maps.Keys(companionCoverage)) {
		if !slices.Contains(flags, flag) {
			errs = append(errs, fmt.Errorf(
				"%s records %s as covered by %s and the cpybkc CLI no longer accepts that flag; a module argument "+
					"is public API for as long as the published module ref exists, so a flag leaving the CLI is a "+
					"decision about the module rather than a line to delete from this table without one",
				companionModuleDir, flag, companionCoverage[flag]))
		}
	}

	return errors.Join(errs...)
}

// cliFlags is every flag the CLI's parser matches on, read out of its own
// constants.
//
// Two spellings are deliberately not flags here. The bare "--" is POSIX's end of
// options rather than something a caller passes a value to, and "-h" is a
// single-hyphen synonym docs/cli/SPEC.md requires to go undocumented — a module
// covering "--help" covers it, and listing it would ask the coverage table to
// record a spelling the CLI's own help text will not print.
func cliFlags(ctx context.Context, source *dagger.Directory) ([]string, error) {
	files, err := goFiles(ctx, source, cliPackageDir)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	for path, contents := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), path, contents, 0)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}

		for _, value := range constantStrings(parsed) {
			if strings.HasPrefix(value, "--") && value != "--" {
				seen[value] = true
			}
		}
	}

	return slices.Sorted(maps.Keys(seen)), nil
}

// companionFunctions is every exported method the companion module declares on
// its own type — which is exactly what Dagger publishes as the module's
// functions, and so exactly what a coverage entry may name.
func companionFunctions(ctx context.Context, source *dagger.Directory) ([]string, error) {
	files, err := goFiles(ctx, source, companionModuleDir)
	if err != nil {
		return nil, err
	}

	var functions []string
	for path, contents := range files {
		if strings.HasSuffix(path, generatedFile) {
			continue
		}

		parsed, err := parser.ParseFile(token.NewFileSet(), path, contents, 0)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}

		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || !fn.Name.IsExported() {
				continue
			}

			if receiverType(fn.Recv.List[0].Type) == companionType {
				functions = append(functions, fn.Name.Name)
			}
		}
	}

	slices.Sort(functions)

	return functions, nil
}

// companionType is the companion module's own type, whose exported methods are
// the module's functions. It is the directory's name in Go's spelling, and the
// two move together or the module does not build.
const companionType = "Cpybkc"

// receiverType names the type a method is declared on, with the pointer taken
// off. A Dagger module's functions are conventionally declared on the pointer,
// but a value receiver publishes the same function, so both are read.
func receiverType(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}

	ident, ok := expr.(*ast.Ident)
	if !ok {
		return ""
	}

	return ident.Name
}

// constantStrings is every string a file declares as a constant.
//
// Constants only, and not every string literal in the file: a diagnostic that
// quotes a flag back at the user is prose about the surface rather than part of
// it, and reading those too would make the check fail on a reworded error
// message.
func constantStrings(file *ast.File) []string {
	var values []string

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}

		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}

			for _, expr := range value.Values {
				literal, ok := expr.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}

				unquoted, err := strconv.Unquote(literal.Value)
				if err != nil {
					continue
				}

				values = append(values, unquoted)
			}
		}
	}

	return values
}

// goFiles reads a directory's Go source, keyed by path.
//
// Tests are skipped because a test's constants are its own fixtures: a table
// driving the parser over "--jobs" to assert it is refused would otherwise read
// as the CLI having grown a flag.
func goFiles(ctx context.Context, source *dagger.Directory, dir string) (map[string]string, error) {
	directory := source.Directory(dir)

	entries, err := directory.Entries(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}

	files := map[string]string{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry, ".go") || strings.HasSuffix(entry, "_test.go") {
			continue
		}

		contents, err := directory.File(entry).Contents(ctx)
		if err != nil {
			return nil, fmt.Errorf("reading %s/%s: %w", dir, entry, err)
		}

		files[dir+"/"+entry] = contents
	}

	return files, nil
}
