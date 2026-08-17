# cpybkc
A modular code generator for files composed COBOL copybook records

## Specs

Seven interfaces are built against from outside this repository, so each is
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
- [The conformance corpus format](docs/conformance/SPEC.md) — what a generator
  in any language is held to, and the language the answer is written in. Its
  [grammar corpus](docs/conformance/GRAMMAR.md) is that language as a table of
  values against the exact text each is written as, which is where a writer for
  a new language is checked first.
- [The conformance adapter contract](docs/adapter/SPEC.md) — the process a
  generator in any language is driven through to be held to that corpus: a
  handshake, JSON frames over standard input and output, and what an exit code
  may and may not mean.

[docs/CONVENTIONS.md](docs/CONVENTIONS.md) defines the conformance language all
seven use, and what else they have in common.

A generator plugin written in Go imports the resolved IR from
[`irpb`](irpb/) — `github.com/Zaba505/cpybkc/irpb`, a module of its own, so that
building against the contract costs the protobuf runtime and nothing else.

[`cpybkc-gen-go`](cmd/cpybkc-gen-go/) is the worked implementation of that
contract and this project's own generator. It is found on `PATH` and run with
the same argument vector a third-party generator is, and it imports `irpb` and
the standard library and nothing else from this repository, so the contract has
a consumer rather than only readers. Its README is the options it takes.

[`cpybkc-gen-graph`](cmd/cpybkc-gen-graph/) is the second, and it is built the
same way for the same reason — the argument that a generator of this project's
own must not reach into `internal/` is only a demonstration once two generators
live under it. It draws the sequencing automaton a descriptor describes as a
Mermaid or Graphviz diagram, with each record's items and their offsets tabled
beneath it — which is how you check that a layout describes the records you
meant, in the order you meant, told apart on the bytes you meant, at the offsets
you meant.

Both are published as images of their own from every release, beside the CLI
image and under the same tags:

```console
$ docker pull ghcr.io/zaba505/cpybkc-gen-go:v0
$ docker pull ghcr.io/zaba505/cpybkc-gen-graph:v0
```

So neither needs a Go toolchain to run: a generator is copied out of its image
into one built `FROM` the CLI's, which is the same route a stranger's generator
takes. [Where this project's own generators are
published](docs/container/SPEC.md#where-this-projects-own-generators-are-published)
is the rule those addresses follow — derived from the CLI image's repository, so
a mirror moves the whole family by moving one.

**Until the first release under that rule is cut, those two references resolve
to nothing.** They are written here ahead of it deliberately, because the rule
they follow is what the release is built against; a reader arriving before it
should build from source, as [the worked example](example/README.md#regenerating)
does.

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

Whoever wrote that plugin takes the fourth, `cpybkc-conformance.tar.gz`: the
conformance corpus — small files with the right answer written down — a digest
of it, and `cpybkc-conform` built for five platforms. Unpack it, write an
[adapter](docs/adapter/SPEC.md) for your generator, and run

```sh
./bin/cpybkc-conform-linux-amd64 check --exec ../my-adapter
```

with no clone, no registry and no container runtime in front of you. [The
published corpus](docs/conformance/SPEC.md#the-published-corpus) is the
archive's layout and the digest rule; [the corpus
format](docs/conformance/SPEC.md) is what an entry holds and the language a
decoded record is written in.

There are two doors onto that one contract, and which one a run went through is
part of the result. `--exec` provides no isolation at all, so a run through it
is your own working result. `--image ghcr.io/you/gen-rust-adapter:v1` runs the
adapter in a container with no network, a read-only root, a memory and process
cap and a wall-clock bound, which is a result you can hand to somebody else.
The report quotes whichever door produced it, because those guarantees are the
door's and never the contract's. Neither makes a run a claim a third party
should be asked to trust without qualification: cpybkc awards no level, profile,
score or badge, and a result is a self-report either way.

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
      "options": {
        "package_name": "orders",
        "import_path": "example.com/warehouse/gen",
        "receiver": "o"
      }
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
copybooks it names, the manifest, and the Go package and the diagram cpybkc
writes for them, all checked in. A test regenerates both from those inputs and
requires them byte for byte, so the path an adopter takes — write a layout,
generate, look at the graph, read a file — is one this repository runs on every
pull request rather than one it describes.

It is also the one project here that runs **two** generators, `go` and `graph`,
which is what makes it the place the plugin contract's central equality can be
tested rather than only stated: every generator in a run — and `--emit-ir` — is
handed the same descriptor bytes. With one generator there is no second set of
bytes for that to hold between.

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

It is the cpybkc CLI daggerized: what `cpybkc` can do, this module aims to do by
name, with the CLI's commands as functions and their flags as arguments.
`generate` and `init` are the two commands there are, and `emit-ir` is the flag
that replaces generating outright, so it gets a function under the flag's own
name; `run` is the fallback for what has not been mapped yet and for the
spellings a Dagger type cannot express, such as a destination that is a stream.
Where the two disagree, that is a gap to file rather than a decision to work
around.

**Filing a bug against cpybkc or against a generator? Attach the descriptor.**
It is exactly the bytes cpybkc handed every generator in the run, so reading it
settles in one step whether a fault is the producer's or the consumer's — and an
emitting run is terminal, so a project whose `generate` fails inside a generator
still hands one back:

```sh
dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
  emit-ir --source . export --path descriptor.binpb
```

That is the canonical protobuf encoding, and it is the artifact to attach: it is
the bytes a generator received, byte for byte. `--format json` renders the same
descriptor as the normalized JSON a person reads — useful for quoting a field in
the issue text, but a rendering rather than the thing itself, so send it *beside*
the binary file rather than instead of it. `cpybkc --emit-ir descriptor.binpb` is
the same run without Dagger.

**Building reproducibly? State the environment.** cpybkc passes its whole
environment through to every generator it runs, which is how [the plugin
contract](docs/plugin/SPEC.md#the-environment) propagates `SOURCE_DATE_EPOCH`, so
a build that sets it for its other tools sets it for generated output too —
through this module, that is `with-env-variable`:

```sh
dagger call -m github.com/Zaba505/cpybkc/daggerverse/cpybkc \
  with-env-variable --name SOURCE_DATE_EPOCH --value "$SOURCE_DATE_EPOCH" \
  with-generator --name hello --image ghcr.io/example/cpybkc-gen-hello:v1 \
  generate --source . export --path .
```

It takes any variable, not that one: what the CLI hands a generator is an
environment rather than a timestamp, and this module mirrors the CLI. What it is
*for* is still that one — a generator's output is configured by the manifest, and
[the plugin contract](docs/plugin/SPEC.md#the-environment) is explicit that a
generator must not need a variable set to do its normal work, precisely so that
two developers comparing generated output can see the difference between their
machines.

It still has no `SPEC.md`, and not because there is nothing to specify — it is
public API, and a function or an argument here lasts as long as the published
ref does. It is that everything there would be to specify is specified already:
what a flag means is [the CLI's](docs/cli/SPEC.md), what the image promises is
[the base-image contract's](docs/container/SPEC.md), and what a generator is
handed is [the plugin contract's](docs/plugin/SPEC.md). A document here would be
a second reading of those three. What it needs to say beyond them it says in
`dagger call --help`.

Everything it does can still be written by hand as a `docker run` or a
`COPY --from` instead, and a caller reaching for one is not on a lesser path —
`with-generator` stands for exactly the two lines [the worked
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
