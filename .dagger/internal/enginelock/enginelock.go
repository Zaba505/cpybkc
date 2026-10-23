// Package enginelock answers the question EngineLock asks of the two Dagger
// modules this repository ships: what engineVersion does each dagger-module.toml
// declare, and do they agree?
//
// It is a package of its own for the reason [gomod] and [surface] are: the
// pipeline's own package main imports the generated Dagger client, whose init
// panics without a session, so a test beside it cannot run under plain
// `go test`. This package imports no Dagger and takes file contents rather than
// a *dagger.Directory, so every rule below is pinned by a test.
//
// The manifest is decoded with a TOML parser rather than matched with a pattern.
// `dagger generate` is entitled to rewrite a dagger-module.toml — on the
// migration to Dagger v1 it dropped two `pin` keys from the root one unasked —
// and a check that read the version with a regular expression would pass or fail
// on how the file happens to be laid out: a key quoted as `"engineVersion"`, a
// literal string in single quotes, a trailing comment, or the same key inside a
// table, where it means something else entirely.
//
// [gomod]: dagger/cpybkc/internal/gomod
// [surface]: dagger/cpybkc/internal/surface
package enginelock

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// Manifest is the file each module declares its engine version in. Dagger v1
// calls it the module config; before #429 it was dagger.json.
const Manifest = "dagger-module.toml"

// Version decodes the engineVersion a dagger-module.toml declares at its top
// level. path names the file in every error, so a failure says which of the two
// modules is wrong.
//
// An absent or empty engineVersion is an error rather than a value to compare,
// because two modules that both say nothing are not two modules that agree.
func Version(path, contents string) (string, error) {
	var manifest struct {
		EngineVersion string `toml:"engineVersion"`
	}
	if _, err := toml.Decode(contents, &manifest); err != nil {
		return "", fmt.Errorf("parsing %s: %w", path, err)
	}

	if manifest.EngineVersion == "" {
		return "", fmt.Errorf("%s declares no engineVersion", path)
	}

	return manifest.EngineVersion, nil
}

// Agree reports an error when the root module and the companion declare
// different engine versions. rootPath and companionPath name the two manifests
// in the message, which also says what a bump has to do and where the argument
// for that is written down.
func Agree(rootPath, root, companionPath, companion string) error {
	if root == companion {
		return nil
	}

	return fmt.Errorf(
		"%s declares engineVersion %s but %s declares %s: the two modules ship from one repository and are driven by one engine, so a bump is one commit that edits both (CONTRIBUTING.md, \"One engineVersion, held by a dependency edge and one check\")",
		rootPath, root, companionPath, companion)
}
