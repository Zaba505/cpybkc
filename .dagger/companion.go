// This file checks the companion Dagger module — the consumer-facing one in
// daggerverse/cpybkc that runs the published image for somebody else's pipeline
// (#61). It is not that module; it is this pipeline's opinion about it.
//
// Two things are checked here, and they are separate because they fail for
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
package main

import (
	"context"
	"encoding/json"
	"fmt"

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
