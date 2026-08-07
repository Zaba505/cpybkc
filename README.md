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

## Contributing

`dagger call ci` runs fmt, vet, lint and `go test -race` — the same call CI
makes. See [CONTRIBUTING.md](CONTRIBUTING.md).
