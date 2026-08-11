// Package imageref assembles the image reference the module pulls, and refuses
// the inputs that would otherwise assemble into something a registry cannot
// answer for.
//
// It is a package of its own, rather than two functions in package main, so that
// it can be tested by `go test` alone. The generated `internal/dagger` package
// panics in its `init` when no Dagger session is present, and package main
// imports it — so a test beside main cannot run outside `dagger run`, which is
// exactly the friction that leaves a default nobody checks. Nothing here imports
// Dagger, so `go test ./internal/...` runs anywhere.
package imageref

import (
	"errors"
	"fmt"
	"strings"
)

// Assemble returns the reference to pull for a repository and a version.
//
// The refusals exist because dagger.Container.From is lazy: a malformed
// reference built at construction does not fail there, it fails much later at
// whatever terminal finally evaluates the container, as a registry error quoting
// a string the caller never typed. Turning that into a constructor error is the
// whole reason the module's New returns one.
//
// Note that a Dagger `+default` fires only when an argument is *omitted*, so an
// explicitly empty --version arrives as "" and reaches these checks rather than
// the default. That is the case the emptiness rules are really for.
//
// This validates the shape of a reference and not its contents: whether the
// repository exists, whether the tag has been published and whether the caller
// may pull it are all questions only the registry can answer, and asking them
// here would mean a network call in a constructor.
func Assemble(repository, version string) (string, error) {
	switch {
	case repository == "":
		return "", errors.New(
			"a repository is required: it is where the image is pulled from, as <host>/<path> with no tag — for example ghcr.io/zaba505/cpybkc")
	case strings.Contains(repository, "@"):
		return "", fmt.Errorf(
			"repository %q carries a digest: pass <host>/<path> with no tag or digest, and pin bytes by building the container yourself and passing it to --image", repository)
	case strings.Contains(LastPathSegment(repository), ":"):
		return "", fmt.Errorf(
			"repository %q carries a tag: pass <host>/<path> with no tag — the tag is --version, and giving it here would assemble a reference naming two", repository)
	case version == "":
		return "", errors.New(
			`a version is required: it is the tag on the published image — "v0" follows every release in that major, "v0.3.1" pins one`)
	case strings.ContainsAny(version, ":@/"):
		return "", fmt.Errorf(
			"version %q is not a tag: a tag is one path-free component, so a digest or a full reference does not go here — pin bytes by building the container yourself and passing it to --image", version)
	}
	return repository + ":" + version, nil
}

// LastPathSegment is what a tag would have to appear in for a reference to name
// two of them. It exists because a colon in any earlier segment is a port —
// `localhost:5000/cpybkc` is an ordinary repository on a registry served over
// one — so a check for a colon anywhere would refuse every private registry
// reachable by host and port, which is a large share of the air-gapped setups
// the repository argument is there to serve.
func LastPathSegment(repository string) string {
	if i := strings.LastIndex(repository, "/"); i >= 0 {
		return repository[i+1:]
	}
	return repository
}
