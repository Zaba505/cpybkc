# Contributing to cpybkc

## The pipeline

fmt, vet, golangci-lint, `go test -race` and `buf lint` are defined once, in the
root Dagger module under [`.dagger/`](.dagger/). CI calls that module and so do
you, which is the point: there is no arrangement of local commands that passes
while CI fails, because they are the same functions.

Run the whole thing before pushing:

```sh
dagger call ci
```

That is the same call CI makes, with no arguments on either side. It runs the
four Go stages in parallel and reports every failure, not just the first, and it
runs the schema lint alongside them.

### Running one stage

`dagger call ci` is the gate; the individual stages are for narrowing down what
it reported.

```sh
dagger call fmt         # gofmt, reported as a diff of what it would rewrite
dagger call vet         # go vet ./...
dagger call lint        # golangci-lint over ./... against .golangci.yml
dagger call test        # go test -race ./...
dagger call proto-lint  # buf lint over proto/ against buf.yaml
```

The first four are not a second definition of the pipeline. Each drives the same
builder `ci` drives with one stage enabled instead of four, against the same lint
configuration, so a stage that passes on its own passes inside `ci`. `proto-lint`
is the one `ci` calls directly rather than through the standard, because the
standard has no protobuf stage to wrap; see [Linting the IR
schema](#linting-the-ir-schema).

`dagger check` runs all six as a checklist, if you would rather see them together
than pick one.

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

### Linting the IR schema

The resolved IR's protobuf schema lives at
[`proto/cpybkc/ir/v1/ir.proto`](proto/cpybkc/ir/v1/ir.proto), and `buf.yaml` at
the repository root configures the lint over it: buf's `STANDARD` category, with
no exceptions and nothing ignored. `dagger call proto-lint` is what runs it, and
`ci` runs it too, so the gate covers the schema.

That stage is this repository's own rather than the standard's, because the
Z5Labs Go pipeline has no protobuf stage to wrap. It is still one definition
rather than two: the buf release is pinned by tag *and* by digest in
[`.dagger/main.go`](.dagger/main.go), and the configuration it reads is the
`buf.yaml` committed here, for the same reason the Go lint stage is handed this
repository's `.golangci.yml`.

`buf` is not needed locally — the stage runs it in a container, like every other.
If you have the CLI, `buf lint` from the repository root reads the same file and
gives the same answer.

Changing the schema is not only a lint question. `ir.proto`'s own comments carry
the compatibility policy — which edits require `Descriptor.version` to advance,
and why that rule is stricter than what protobuf calls wire-compatible — and
[`docs/ir/SPEC.md`](docs/ir/SPEC.md) is normative for what every node means. A
new node kind, or a new member of any closed set in the file, advances the
version even though `buf` will not say so.

## Specs

Four of this project's interfaces are built against from outside it — the file
layout format, the resolved IR, the plugin CLI contract and the container
base-image contract — and each is far harder to change than the code behind it.
Each has a `SPEC.md` under [`docs/`](docs/), linked from
[README.md](README.md), which is the only list of them.

[`docs/CONVENTIONS.md`](docs/CONVENTIONS.md) is what those four agree on: the
conformance language, the section set every spec carries, how sources are cited
and how requirements are traced back to stories. It is defined there once and
referenced, never restated — the same argument this file already makes about the
pipeline and the lint configuration, applied to the word **MUST**.

### Why `docs/` and not beside the code

[`cobol-go`](https://github.com/Zaba505/cobol-go) puts `codec/SPEC.md` next to
package `codec`, and it is the model these specs follow in every other respect.
It is not followed here because three of the four specify things that are not Go
packages: a text file format, a CLI contract for executables that may be shell
scripts, and an OCI image. Package-adjacency would have conjured an empty
`container/` package into existence to hold one markdown file, and would have
scattered four documents that are peers — a reader comparing what the plugin
contract promises against what the image provides should not have to know the
package tree to find both.

Each spec gets a directory rather than a bare file because they grow supporting
material: the layout format's published schema belongs beside the layout spec,
not in a second place that has to be kept in step with it.

### Changing a spec

Read `docs/CONVENTIONS.md` first; it is short, and it is what a reviewer will
check against. Then:

- Keep the section set. A spec missing `Out of Scope` is a spec that will have
  the same exclusion proposed to it every six months.
- Fill a stub heading and its `<!-- -->` brief goes in the same change, along
  with the *Mapping to Stories* row. A section with prose and its scaffolding
  comment still above it is a change somebody stopped halfway through.
- Cite, do not restate. Anything true of COBOL source or of byte-level field
  layout is `cobol-go`'s to say, and a second copy here is a second thing to be
  wrong.
- A new spec is a new row in `README.md`. There is one index; keep it complete.

`dagger call ci` does not read `docs/` — it is fmt, vet, lint and
`go test -race`, and it will pass on a documentation change without having
looked at it. Run it anyway, because a change that claims to be docs-only and is
not should fail in the same place as everything else, but do not mistake it for
review. What checks a spec is a reader asking whether every link resolves from
the file it is written in, whether every **MUST** names who it binds, and
whether anything is restated that could have been cited.

Mechanising the first of those — link resolution and the 80-column wrap — would
be welcome. It arrives as another function on the root Dagger module, called by
another `dagger call`, for the reason
[`.github/workflows/ci.yaml`](.github/workflows/ci.yaml) already gives: raw
steps beside it would be a second definition of what this repository checks.
