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

## Contributing

`dagger call ci` runs fmt, vet, lint and `go test -race` — the same call CI
makes. See [CONTRIBUTING.md](CONTRIBUTING.md).
