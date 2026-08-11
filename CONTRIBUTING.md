# Contributing to cpybkc

## The pipeline

fmt, vet, golangci-lint, `go test -race`, `buf lint`, the three artifacts a
release attaches and the worked example in the base-image contract are defined
once, in the root Dagger module under [`.dagger/`](.dagger/). CI calls that
module and so do you, which is the point: there is no arrangement of local
commands that passes while CI fails, because they are the same functions.

Run the whole thing before pushing:

```sh
dagger call ci
```

That is the same call CI makes, with no arguments on either side. It runs the
four Go stages in parallel and reports every failure, not just the first, and it
runs the schema lint, the IR module's own stages, a build of the [release
artifacts](#the-release-artifacts) and a build of the [base-image
contract](docs/container/SPEC.md)'s worked example alongside them.

### Running one stage

`dagger call ci` is the gate; the individual stages are for narrowing down what
it reported.

```sh
dagger call fmt           # gofmt, reported as a diff of what it would rewrite
dagger call vet           # go vet ./...
dagger call lint          # golangci-lint over ./... against .golangci.yml
dagger call test          # go test -race ./...
dagger call proto-lint    # buf lint over proto/ against buf.yaml
dagger call ir-ci         # the whole standard again, over the irpb/ module
dagger call ir-artifacts  # builds the two IR artifacts a release attaches
dagger call layout-artifact  # builds the layout schema a release attaches
dagger call worked-example   # builds docs/container/SPEC.md's worked example
```

The first four are not a second definition of the pipeline. Each drives the same
builder `ci` drives with one stage enabled instead of four, against the same lint
configuration, so a stage that passes on its own passes inside `ci`. `proto-lint`
is the one `ci` calls directly rather than through the standard, because the
standard has no protobuf stage to wrap; see [Linting the IR
schema](#linting-the-ir-schema).

`ir-ci` is not a stage but the whole standard a second time, over the second Go
module; see [The IR module](#the-ir-module) for why there is one. `ir-artifacts`
and `layout-artifact` are not stages either; see [The release
artifacts](#the-release-artifacts).

`worked-example` is the odd one out: it builds a document. The multi-stage
Dockerfile in [the base-image contract](docs/container/SPEC.md#worked-example-adding-a-generator)
is the first thing an adopter runs and the last thing anybody here would notice
had broken, since no other stage reads it, so `ci` extracts it from that file and
builds it. Edit that example and this is the stage that will tell you about it.

`dagger check` runs all ten as a checklist, if you would rather see them
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

`GoLib` rather than `GoApp`, even now that `cmd/cpybkc-gen-go` is a main package.
What `GoApp` adds is the multi-platform image build and the publish half of the
standard, and both belong to the container stories: what is in the image, which
platforms it carries and where it goes are the [base-image
contract](docs/container/SPEC.md)'s, and switching factories ahead of them would
build an image nothing had described. The four check stages gate a pull request
under either archetype and they run over `./...`, so `cmd/` is checked today. The
move lands with the image — a change to which factory the module calls, not a
change to what the pipeline is.

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

## The IR module

The Go form of the resolved IR is [`irpb/`](irpb/), and it is a **separate Go
module** — `github.com/Zaba505/cpybkc/irpb` — not a package of this one. Its
package comment carries the argument; the short version is that a generator
plugin importing the IR should take on the protobuf runtime and nothing the CLI
happens to need, and only a module boundary delivers that.

Three consequences a contributor meets:

- **`irpb/go.mod` requires exactly one module.** `irpb/module_test.go` fails the
  build if a second appears, including a test-only one — a test dependency lands
  in a plugin author's module graph like any other, which is also why that test
  parses `go.mod` by hand instead of using `golang.org/x/mod`.
- **`go test ./...` from the repository root does not reach it.** That is Go's
  rule about nested modules, not a configuration, and it is why the pipeline
  invokes the standard a second time as `dagger call ir-ci`. A change to `irpb/`
  is checked by `dagger call ci` like everything else, but through that call.
- **The root `go.mod` requires it with a `replace`.** A module cannot be required
  at a version nobody has published yet, and the first `irpb/v0.1.0` can only tag
  a commit that already contains `irpb/`. The line comes out with that tag; the
  comment beside it says what breaks if it does not.

### Regenerating the stubs

`irpb/ir.pb.go` is generated from `proto/cpybkc/ir/v1/ir.proto` and **committed**,
for the reasons `.dagger/`'s codegen is: the module has to build from a checkout
alone, and a published module whose sources appear only after somebody runs a
generator is not a module anyone can `go get`.

```sh
dagger call proto-gen export --path=irpb
```

[`buf.gen.yaml`](buf.gen.yaml) decides what that writes — it pins the
`protoc-gen-go` release, and `irpb/go.mod` requires the matching
`google.golang.org/protobuf`, so the two move in one change. `dagger call
proto-gen` and a contributor's own `buf generate` read that same file and produce
the same tree.

A pull request whose `ir.proto` moved without `irpb/ir.pb.go` moving with it is
the protobuf half of the rule [After changing the
module](#after-changing-the-module) already states about `.dagger/`. Nothing in
`ci` regenerates to check it: a stage that rewrites the tree it is checking is a
pipeline with an opinion about your working directory, and this is what review is
for.

### Releasing it

The IR module's tags are `irpb/vX.Y.Z` — Go's own rule for a module in a
subdirectory, not a convention chosen here. They move independently of the CLI's
`vX.Y.Z`, which is the point: a documentation fix in `irpb/` is a release of the
IR module and of nothing else, and a CLI release is not a new IR for anyone to
re-vendor. `.github/workflows/ci.yaml` matches both patterns.

Neither tag is the IR's own version. That is `Descriptor.version`, specified by
[`docs/ir/SPEC.md`](docs/ir/SPEC.md), and one IR version outlives many module
tags. If the schema ever breaks its wire format and becomes package
`cpybkc.ir.v2`, the Go module becomes `github.com/Zaba505/cpybkc/irpb/v2` under
Go's major-version rule, and the two version suffixes agree without either being
kept in step by hand.

## The release artifacts

Every published release carries three files. Two describe the IR, for the plugin
authors who are not importing `irpb`; the third is the layout schema, for the
shop generating layout files rather than writing them:

```sh
dagger call ir-descriptor-set export --path=ir.binpb
dagger call ir-protos export --path=ir-protos.tar.gz
dagger call layout-schema export --path=layout-schema.sexpr
```

`ir.binpb` is the protobuf `FileDescriptorSet` describing
`cpybkc.ir.v1.Descriptor`, which is what lets a runtime with no code generation
in the build decode a descriptor dynamically. `ir-protos.tar.gz` is the schema
sources at the paths their protobuf packages require, for a build that would
rather compile them. [Reading a descriptor without generated
code](docs/ir/SPEC.md#reading-a-descriptor-without-generated-code) is the
contract for both, and it is the document to change if what they contain changes.

`layout-schema.sexpr` is the machine-readable contract for the layout format:
the set of form declarations a layout generator targets, written in the notation
it describes. It is `schema/layout.sexpr` in the tree, published byte for byte,
because an adopter's diagnostic and this repository's have to be about the same
text. [The published schema](docs/layout/SPEC.md#the-published-schema) is the
document to change if what it declares changes — including the version it
carries, which moves with the format and not with the document.

Three properties are worth knowing before you touch any of them:

- **Neither IR artifact is committed.** `ir.binpb` is computed from the
  descriptors `protoc-gen-go` compiled into `irpb`, so it cannot drift from the
  schema; a checked-in copy could. Changing `ir.proto` and regenerating
  `irpb/ir.pb.go` changes what is published, in the same commit. The layout
  schema is the other way round — it is a source file a person writes, so it is
  committed, and `layout-schema` publishes it unchanged after checking that it
  loads.
- **All three are reproducible.** Two builds of one commit produce byte-identical
  files — the encoding is deterministic, every tar field the filesystem could
  have supplied is a constant, and the schema is copied rather than reformatted —
  because an artifact that moved on a rebuild would make a rebuild
  indistinguishable from a change to the contract.
- **`dagger call ci` builds them.** `ir-artifacts` and `layout-artifact` are in
  the pipeline so that the recipe a release runs is exercised on every pull
  request, rather than for the first time on a tag. What is in them is asserted
  by Go tests, in `irpb` and beside each tool under `internal/tools/`; the
  pipeline stages only check that the commands run to completion and leave a file
  behind.

A `.proto` added under `proto/` that nothing reachable from
`cpybkc.ir.v1.Descriptor` imports fails `TestPublishedFileDescriptorSetCoversTheSchema`.
That is deliberate: the IR is one message and everything in `proto/` is reachable
from it, so a file that is not is a mistake in the schema rather than something
to exclude.

`.github/workflows/release.yaml` attaches all three to a release when one is
published, building them with the same three calls above at the release's tag.

## The companion Dagger module

A second Dagger module ships from this repository: the consumer-facing one that
runs the CLI for a caller, published as `github.com/Zaba505/cpybkc/<dir>` and
unrelated to the `.dagger/` module that runs this repository's own pipeline. It
does not exist yet — #61 writes it — and one thing about it had to be settled
before it could be written, because the answer is a default value in a
constructor, and a default in a module directory that is never renamed is public
API from the first release.

The module gets no `SPEC.md`. It is a convenience over the [base-image
contract](docs/container/SPEC.md) rather than an interface of its own, and what
it needs to say it says in its module comment and in `dagger call --help`
([`docs/CONVENTIONS.md`](docs/CONVENTIONS.md), *What belongs here*). What
follows is the argument behind one of those comments, kept here because the
argument is longer than the comment and outlives the reader who needs it.

### The default image tag is the moving major tag

`New`'s `version` argument defaults to **`v0`**, the moving major tag, and not
to a full version constant generated at release time (#104).

A major tag follows every release within its major by the [tag
scheme](docs/container/SPEC.md#tags-and-what-pinning-one-buys) (#59), so the
default cannot go stale. Nothing is generated at release time, no CI check
asserts that a constant equals the git tag, and the release runbook grows no
step that could be forgotten. A caller who pins nothing picks up every fix in
the major version without editing anything — which is what a moving tag is for,
and what the container contract already recommends to a derived Dockerfile.

### What pinning the module ref buys, and what it does not

Two version numbers are in play, and neither pins the other.
`github.com/Zaba505/cpybkc/<dir>@v0.3.1` pins the module's *source* — the
constructor's defaults, and the composition its methods perform — at the commit
that tag names. Which image that source pulls is decided when it runs, by the
`version` argument, whose default resolves through a tag that moves. A pinned
module ref therefore buys a module that behaves the same way every time, and
says nothing about the bytes of the CLI it drives.

What the two do agree on is the major version, and that is not an accident: a
module published at a `v0.x.y` ref defaults to the image's `v0`. Pinning the ref
pins which major of the image you get, and that is exactly the range over which
the [compatibility
guarantees](docs/container/SPEC.md#compatibility-guarantees) hold. Anything
narrower than a major is the caller's explicit act — `v0.3.1` names one release,
and a digest names the bytes.

### Skew between the module and the image is supported within a major

A module from one release driving an image from another is a **supported
combination** for as long as both are inside one major version of the image, and
what bounds it is the container contract's compatibility guarantees rather than
anything the module promises on its own. Across a major version it is not
supported, and the default cannot produce it: `v0` does not cross into `v1`.

The bound is narrower than the whole contract, which is what makes it worth
leaning on. The module drives the image through four things, and all four are in
the covered table: the plugin directory it copies a generator into, the
entrypoint it passes arguments to, the user that has to be able to execute what
was copied, and the platform set. Everything else it reads off the container
rather than assuming — the working directory in particular, which the contract
explicitly does **not** cover. A module that had copied more of the contract
into itself would have that much more to hold across the skew this default
permits, so the discipline that keeps the module small (#61) is the same
discipline that makes the moving default safe.

One level down, this project refuses the analogous skew, and the difference is
worth stating because the prior art this module follows does not state it. A
later `WithGenerator` pulls the generator matching the CLI already composed in,
because a generator from one release beside a CLI from another is a combination
nobody tested and defaulting to it silently is how somebody ends up in it (#63).
That is a rule about two arguments that can carry different strings, and the fix
is to carry one string forward. The module ref and the image tag are not two
spellings of one version, so there is no string to carry; and the tag that *is*
carried forward to the generator is the same `v0` the CLI came from. The pair
#63 refuses is one this default cannot construct.

### Why the generated constant lost

It was a real option, and it buys something this one does not: a caller who pins
nothing gets a reproducible build, and one version number then names one tested
combination of module and image.

It costs release-time code generation, a CI check that the constant equals the
git tag, a runbook step — and it introduces a failure the moving tag does not
have. A constant nobody regenerated is stale, and stale is silent: generation
succeeds, against an old CLI, and nothing reports it. The check would have been
the only thing standing between that failure and a user, which is the argument
against the option that needs it. A moving default fails the other way round:
the only way it can fail is a change inside a major version that the module did
not expect, and that breaks the next run loudly rather than the next quarter's
quietly.

Reproducibility is not lost by this, it is made explicit — and the escalation on
the argument is where a caller learns how to ask for it. A digest is the only
reference that pins bytes, which is the position
[`docs/container/SPEC.md`](docs/container/SPEC.md#tags-and-what-pinning-one-buys)
takes about tags and
[`docs/plugin/SPEC.md`](docs/plugin/SPEC.md#also-out-of-scope) takes about
plugin distribution. Nothing else claims to.

### What the constructor says

The escalation belongs on the argument and not only here, because a caller reads
`dagger call --help` and never opens this file. #61 carries this onto `version`,
and a change to the default is a change to both:

```go
// Version is the tag of the published cpybkc image to run.
//
// It defaults to the moving major tag "v0", which follows every release in
// that major version: a caller who passes nothing keeps up with fixes and
// stays inside the base-image contract's compatibility guarantees. Escalate
// deliberately — "v0" follows releases, "v0.3.1" pins one release, and only a
// digest pins the bytes. A digest is not a tag, so it goes to the container
// override argument rather than here.
```

How the default is spelled — a `+default` pragma or a zero-value check — is
#61's, and so is the name of the container override the last sentence points at.
What is settled here is the value and what the comment says about it.

### #65 is closed rather than left open

#65 asks for the default tag to be generated from a version constant written at
release time, and for CI to fail when it does not equal the git tag. With `v0`
as the default there is no constant to generate and no invariant left to
enforce: the check would compare a tag that follows releases against the release
it has just followed, and would pass by construction. A check that cannot fail
is not a weaker check — it is a claim that something is being verified. So the
story is closed as unnecessary rather than carried against a premise this
decision removed.

What survives of it is the one thing the default depends on. #59 publishes the
moving major tag; if that ever stopped being published, this decision is what
would have to be reopened, rather than the default quietly patched at the call
site.

## The conformance corpus

[`testdata/conformance/`](testdata/conformance/) is a set of small files with the
right answer written down: a layout and its copybooks, the IR they resolve to,
the bytes of a file laid out that way, and the values those bytes decode to. Its
README is the format, and it is the whole of the documentation — the corpus is
test infrastructure, so it documents itself where it lives rather than under
`docs/` ([`docs/CONVENTIONS.md`](docs/CONVENTIONS.md), *What belongs here*).

```sh
go test ./internal/conformance/...
```

That generates Go for every entry with `cpybkc-gen-go` built from the tree,
compiles it, reads each entry's bytes with it, writes those records back out with
it, reads that file too, and holds both answers against what the entry says.
`dagger call ci` runs it too, like every other test; the call above is for
narrowing down what it reported.

Being an ordinary test is what makes it a gate on every platform in the CI
matrix: the matrix is a matrix of `dagger call ci`, and a conformance job of its
own would be a second gate that a platform added to the matrix would silently not
carry. What it is not is a check of the bytes the writer produced —
[`docs/ir/SPEC.md`](docs/ir/SPEC.md)'s *Writing a file* makes byte identity a
claim about a record and not about a file, and the corpus README's *Why the
writing direction is checked by reading* is the rest of the argument.

It exists because cpybkc is strictly a generator: nothing here reads a data file
except the code a generator emitted, so nothing else holds two generators in two
languages to one reading of one descriptor. Adding an entry is four files and a
citation, and the README says how.

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
