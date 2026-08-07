# Contributing to cpybkc

## The pipeline

fmt, vet, golangci-lint and `go test -race` are defined once, in the root Dagger
module under [`.dagger/`](.dagger/). CI calls that module and so do you, which is
the point: there is no arrangement of local commands that passes while CI fails,
because they are the same functions.

Run the whole thing before pushing:

```sh
dagger call ci
```

That is the same call CI makes, with no arguments on either side. It runs the
four stages in parallel and reports every failure, not just the first.

### Running one stage

`dagger call ci` is the gate; the individual stages are for narrowing down what
it reported.

```sh
dagger call fmt    # gofmt, reported as a diff of what it would rewrite
dagger call vet    # go vet ./...
dagger call lint   # golangci-lint over ./... against .golangci.yml
dagger call test   # go test -race ./...
```

These are not a second definition of the pipeline. Each drives the same builder
`ci` drives with one stage enabled instead of four, against the same lint
configuration, so a stage that passes on its own passes inside `ci`.

`dagger check` runs all five as a checklist, if you would rather see them
together than pick one.

### Getting the tools

The pipeline needs the Dagger CLI and a container runtime (Docker or Podman).
Nothing else — not even a Go toolchain, since every stage runs in a container
built from the version in `go.mod`.

```sh
curl -fsSL https://dl.dagger.io/dagger/install.sh \
  | DAGGER_VERSION="$(jq -r .engineVersion dagger.json | tr -d v)" \
    BIN_DIR="$HOME/.local/bin" sh
```

The version comes out of `dagger.json` rather than being typed in, because the
`engineVersion` the module declares is what CI installs too. A CLI that
provisions a different engine is a difference between your machine and CI, which
is the one thing this module exists to prevent.

### After changing the module

`.dagger/dagger.gen.go` and `.dagger/internal/` are generated **and committed**.
Regenerate them after editing `.dagger/main.go` or `dagger.json`, and commit the
result alongside the change:

```sh
dagger develop
```

They are committed rather than ignored because Dagger is moving to requiring
generated code in the tree by v1.0, and because it means the module builds from
a checkout alone instead of only after somebody has run `dagger develop`.
`.dagger/.gitattributes` marks them `linguist-generated`, so they stay collapsed
in diffs and out of the repository's language statistics.

A pull request whose `.dagger/main.go` or `dagger.json` moved without the
generated files moving with it is a tree that does not build. Bumping a
dependency pin counts: `dagger develop` rewrites `.dagger/internal/dagger/` from
the dependency's schema, so the pin and the generated bindings have to land in
the same commit.

## Why the module wraps the Z5Labs standard pipeline

`ci` does not implement the four stages. It hands the source to
[`github.com/z5labs/devex/daggerverse/z5labs`](https://github.com/z5labs/devex/tree/main/daggerverse/z5labs)
and lets that module's `GoLib` archetype run them — the same standard
`z5labs/dfcad` runs.

Reimplementing them here would be a second definition of what "checked" means in
a Z5Labs repository, and two definitions drift. A stage added to the standard
would silently not apply to cpybkc; a difference in how a stage is invoked would
show up as this repository disagreeing with every other one, for reasons nobody
wrote down. Wrapping costs one dependency and makes that impossible. The full
reasoning, including why the stage functions are not a fork of it, is in
[`.dagger/main.go`](.dagger/main.go)'s package comment.

`GoLib` rather than `GoApp` because cpybkc is a library: there is no main package
to compile, so the multi-platform image build and the publish half of the
standard have nothing to act on. When a command lands under `cmd/`, the archetype
moves to `GoApp` and `ci` gains those stages with it — a change to which factory
the module calls, not a change to what the pipeline is.

Both dependencies in `dagger.json` are pinned to one `devex` commit, so a bump
has to move them together.

## Linting

`.golangci.yml` is written in golangci-lint **v1** syntax, not v2, and that is
deliberate. The standard pipeline pins golangci-lint v1.64.8 and offers no way to
override it, and v1 refuses a v2 configuration file outright rather than ignoring
what it does not understand. Writing it in v1 is what lets the module pass this
repository's own configuration to the pipeline, so the file you read here is the
one CI enforces.

The linter selection is unchanged by that: errcheck, govet, ineffassign,
staticcheck and unused, which is golangci-lint v2's default set. It goes back to
v2 syntax once the standard pipeline runs v2 — tracked upstream as
[z5labs/devex#374](https://github.com/z5labs/devex/issues/374).

If you run `golangci-lint` directly rather than through `dagger call lint`, use
v1.64.8 or it will reject the config for the mirror-image reason.
