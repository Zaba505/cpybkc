# cpybkc
A modular code generator for files composed COBOL copybook records

## Specs

Four interfaces are built against from outside this repository, so each is
specified rather than merely implemented:

- [The file layout format](docs/layout/SPEC.md) — what an adopter writes to
  describe a data file that a copybook alone cannot.
- [The resolved IR](docs/ir/SPEC.md) — what every generator plugin consumes, in
  any language.
- [The generator plugin CLI contract](docs/plugin/SPEC.md) — what
  `cpybkc-gen-<name>` has to implement.
- [The container base-image contract](docs/container/SPEC.md) — what a
  Dockerfile building `FROM` the published image may rely on.

[docs/CONVENTIONS.md](docs/CONVENTIONS.md) defines the conformance language all
four use, and what else they have in common.

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
  "inputs": ["cpy/orders.cpy"],
  "layout": "orders.sexpr",
  "generators": [
    {
      "name": "go",
      "out": "gen",
      "inputs": ["cpy/orders-go.cpy"],
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
| `inputs` | top-level | no | The copybooks every generator reads. |
| `layout` | top-level | yes | The [layout file](docs/layout/SPEC.md) the run resolves against. There is one of it: a project resolving against two layouts is two runs. |
| `generators` | top-level | yes | The generators to run, in order, at least one of them. |
| `name` | generator | yes | Resolves to the `cpybkc-gen-<name>` executable on `PATH`. It is the whole of how a generator is identified — there is no `source` and no `version` beside it. |
| `out` | generator | yes | The directory this generator's output lands in. |
| `inputs` | generator | no | Copybooks this generator reads **in addition to** the top-level `inputs`; a path named in both is read once. |
| `options` | generator | no | The generator's own options, each handed to it as one `--opt k=v`, **in the order this object writes them**. A key carries no `=`; a value is text, and may be empty. |

Four rules are worth knowing before a manifest is written, because each of them
is a fault rather than something cpybkc works around:

- **An unknown field is reported, never ignored.** A manifest is a file a person
  wrote, so a misspelled field is a typo they want told about rather than a line
  that reads as configuration and silently does nothing. The same goes for a
  field written twice, a copybook named twice in one list, and an empty string
  where a path or a name belongs.
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

## Contributing

`dagger call ci` runs fmt, vet, lint and `go test -race` — the same call CI
makes. See [CONTRIBUTING.md](CONTRIBUTING.md).
