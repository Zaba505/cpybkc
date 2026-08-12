# cpybkc
A modular code generator for files composed COBOL copybook records

## Specs

Five interfaces are built against from outside this repository, so each is
specified rather than merely implemented:

- [The command-line interface](docs/cli/SPEC.md) — what a person types, where
  the manifest is looked for, and what comes back on each stream.
- [The file layout format](docs/layout/SPEC.md) — what an adopter writes to
  describe a data file that a copybook alone cannot.
- [The resolved IR](docs/ir/SPEC.md) — what every generator plugin consumes, in
  any language.
- [The generator plugin CLI contract](docs/plugin/SPEC.md) — what
  `cpybkc-gen-<name>` has to implement.
- [The container base-image contract](docs/container/SPEC.md) — what a
  Dockerfile building `FROM` the published image may rely on.

[docs/CONVENTIONS.md](docs/CONVENTIONS.md) defines the conformance language all
five use, and what else they have in common.

A generator plugin written in Go imports the resolved IR from
[`irpb`](irpb/) — `github.com/Zaba505/cpybkc/irpb`, a module of its own, so that
building against the contract costs the protobuf runtime and nothing else.

[`cpybkc-gen-go`](cmd/cpybkc-gen-go/) is the worked implementation of that
contract and this project's own generator. It is found on `PATH` and run with
the same argument vector a third-party generator is, and it imports `irpb` and
the standard library and nothing else from this repository, so the contract has
a consumer rather than only readers. Its README is the options it takes.

A plugin written in anything else takes one of the two IR artifacts attached to
every release: `ir.binpb`, the protobuf `FileDescriptorSet` that lets any
runtime decode a descriptor with no code generation in the build, or
`ir-protos.tar.gz`, the schema sources for a build that would rather compile
them. [Reading a descriptor without generated
code](docs/ir/SPEC.md#reading-a-descriptor-without-generated-code) is the
contract for both.

A shop generating layout files from metadata it already holds takes the third
asset, `layout-schema.sexpr` — the machine-readable contract for the layout
format, written in the notation it describes and carrying the version of that
format inside it. [The published
schema](docs/layout/SPEC.md#the-published-schema) is what it is and what it
deliberately leaves to the reader; `schema/layout.sexpr` is the same file in
this repository.

## The project manifest

A project is driven by a `cpybkc.json` checked in beside the files it names, so
that generator selection and options are diffable, reviewable and the same on a
laptop as in CI:

```json
{
  "layout": "orders.sexpr",
  "generators": [
    {
      "name": "go",
      "out": "gen",
      "options": {"package_name": "orders", "receiver": "o"}
    },
    {
      "name": "json-schema",
      "out": "schema"
    }
  ]
}
```

| Field | Scope | Required | What it is |
|---|---|---|---|
| `layout` | top-level | yes | The [layout file](docs/layout/SPEC.md) the run resolves against. There is one of it: a project resolving against two layouts is two runs. |
| `generators` | top-level | yes | The generators to run, in order, at least one of them. |
| `name` | generator | yes | Resolves to the `cpybkc-gen-<name>` executable on `PATH`. It is the whole of how a generator is identified — there is no `source` and no `version` beside it. |
| `out` | generator | yes | The directory this generator's output lands in. |
| `options` | generator | no | The generator's own options, each handed to it as one `--opt k=v`, **in the order this object writes them**. A key carries no `=`; a value is text, and may be empty. |

**A manifest does not list copybooks.** Which copybooks a run reads is the
layout's to say — a `record` form names the file its record is in — and cpybkc
opens those and no others. A generator never sees a copybook at all: it is
handed the resolved descriptor, an output directory and its options, and the IR
carries no path back to the file an item came from. So there is no top-level
`inputs` and no per-generator `inputs`; a manifest carrying either is reported
as the unknown field it is, with the line and column it is at. [Which descriptor
is emitted](docs/cli/SPEC.md#which-descriptor-is-emitted) is where that is
settled, and [Finding the
inputs](docs/cli/SPEC.md#finding-the-inputs) is why the manifest does not get a
say. The point of both is one sentence: a run has **one** descriptor, and every
generator in it — and `--emit-ir` — is handed the same bytes.

Four rules are worth knowing before a manifest is written, because each of them
is a fault rather than something cpybkc works around:

- **An unknown field is reported, never ignored.** A manifest is a file a person
  wrote, so a misspelled field is a typo they want told about rather than a line
  that reads as configuration and silently does nothing. The same goes for a
  field written twice, an option key written twice, and an empty string where a
  path or a name belongs.
- **A path is used as written.** cpybkc resolves nothing against anything on
  your behalf; a relative path is relative to the manifest.
- **Every fault is reported at once**, each with the line and column in
  `cpybkc.json` it is at — a manifest with three things wrong with it is one run,
  not three.
- **Malformed JSON stops the read**, since there is no way to know where the
  value that failed to parse was meant to end.

The manifest is deliberately **not** one of the four specs above: a plugin never
reads one, and receives the options it selected already resolved on its command
line. [The `cpybkc.json` project
manifest](docs/plugin/SPEC.md#the-cpybkcjson-project-manifest) is where the
plugin contract says so, and why.

## A worked example

[`example/`](example/) carries one artifact from a layout to bytes: a layout, the
copybooks it names, the manifest, and the Go package cpybkc writes for them, all
checked in. A test regenerates that package from those inputs and requires it
byte for byte, so the path an adopter takes — write a layout, generate, read a
file — is one this repository runs on every pull request rather than one it
describes.

Its layout is deliberately a hard one, because a worked example is what an
adopter reads to find out whether their own file is describable: six record
types resolved out of one `01`-level by three redefines over two independent
runs, a type code
that sits at two different offsets depending on the record, and a redefine
shorter than the run it describes. [`example/README.md`](example/README.md) is
what to read first.

## The companion Dagger module

[`daggerverse/cpybkc/`](daggerverse/cpybkc/) runs the published image as a step
in somebody else's pipeline, for a caller who would rather not write a
Dockerfile:

```sh
dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
  with-generator --name hello --image ghcr.io/example/cpybkc-gen-hello:v1 \
  generate --source . export --path .
```

It is a convenience over [the container base-image
contract](docs/container/SPEC.md) and not an interface of its own, so it has no
`SPEC.md`: what it needs to say it says in `dagger call --help`. Everything it
does can be written by hand as a `docker run` or a `COPY --from` instead, and a
caller reaching for one is not on a lesser path — `with-generator` stands for
exactly the two lines [the worked
example](docs/container/SPEC.md#worked-example-adding-a-generator) gives somebody
writing a Dockerfile, and `with-generator-executable` does the same for a
generator that has not been published yet.

Its module ref is that directory path, which makes the path itself public API —
the directory is never renamed. `--version` selects the release, defaulting to
the moving major tag `v0`; `--repository` points it at a mirror or an internal
registry, and the generator images derive from it; `--platform` composes for an
architecture other than the engine's, which is how a derived multi-platform
image is built one variant at a time; `--image` replaces the container outright,
which is how a build pins a digest.
[CONTRIBUTING.md](CONTRIBUTING.md#the-companion-dagger-module) is the argument
behind those defaults.

The `.dagger/` module at the repository root is a different thing: it runs this
repository's own pipeline and is published for nobody.

## Contributing

`dagger call ci` runs fmt, vet, lint and `go test -race` — the same call CI
makes. See [CONTRIBUTING.md](CONTRIBUTING.md).
