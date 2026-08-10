# `cpybkc-gen-go`

The Go generator: it reads a resolved cpybkc IR descriptor and writes Go source
into the directory it is handed.

It is also this project's worked implementation of
[the plugin CLI contract](../../docs/plugin/SPEC.md) — discovered by name on
`PATH` and run with the same argument vector a third-party generator is, so that
the contract has a consumer from the start rather than only readers.

## Invocation

```
cpybkc-gen-go --descriptor <path> --out <dir> [--opt k=v ...]
```

`--descriptor` and `--out` are required and each appears exactly once.
`--descriptor -` reads the descriptor from standard input; cpybkc never emits
that form, and it is accepted so that this generator is drivable from a pipeline
with nowhere convenient to put the bytes. `--` ends the options, and there are
no operands after it — this generator takes none.

Only the separated form is accepted, each flag followed by its value as the next
argument. That is the form cpybkc emits and the one the contract requires a
plugin to accept; `--descriptor=<path>` is not accepted.

You do not normally type any of this. cpybkc builds the vector from
[`cpybkc.json`](../../README.md#the-project-manifest), passing each entry of a
generator's `options` object as one `--opt k=v` in the order the object writes
them:

```json
{
  "name": "go",
  "out": "gen",
  "options": {"package_name": "orders"}
}
```

## Options

| Key | Required | What it is |
|---|---|---|
| `package_name` | yes | The Go package the generated files declare. It must be a Go identifier that is neither a keyword, the blank identifier, nor `init`. |

An option this generator does not recognise is an **error**, not something
ignored — as is a `package_name` given twice, or one that is not a package name.
An ignored option is a line in a checked-in manifest that reads as configuration
and does nothing, and the user finds out by noticing that the output never
changed.

`package_name` is required, and it is an option rather than something inferred,
because the two paths this generator can see are the descriptor's — which the
contract forbids it to derive anything from — and a scratch directory whose name
cpybkc chooses and varies between runs. A package name taken from either would
make the output a function of a path, which is what the contract's *Determinism*
forbids.

The vocabulary grows with what this generator emits: the rename overrides and
identifier munging are [#50](https://github.com/Zaba505/cpybkc/issues/50), and
the method receiver the manifest example in the root `README.md` shows arrives
with the decode and encode methods in
[#51](https://github.com/Zaba505/cpybkc/issues/51).

## The IR version check

cpybkc runs a generator without a handshake of any kind: it does not ask what
this program supports, and it does not compare the descriptor's version against
anything. So this generator reads the descriptor's IR version **before anything
else in the message** — off the wire, before the message is decoded — and
refuses a version it does not implement, writing no file and exiting non-zero.

```
error: descriptor IR version 2; cpybkc-gen-go 0.0.0-dev implements IR version 1
note: upgrade cpybkc-gen-go if the descriptor is newer than it, or generate with a cpybkc that writes IR version 1 if it is not
```

The refusal names three facts — the descriptor's version, the highest this
generator implements, and this generator's own version — because nothing else
is in a position to compose that diagnostic, and a refusal naming one number
leaves you unable to tell an out-of-date generator from an out-of-date CLI.

A descriptor one version too new decodes cleanly, since protobuf hands an
unknown field to an old reader and tells it to ignore one. That is why the check
is not optional: without it this generator would emit code that compiles, links,
and silently misreads the file it was written for.

## Diagnostics and exit status

Everything this program has to say goes to standard error as
`<severity>: <message>`, one line each, with `error`, `warning` and `note` the
only severities. A non-zero exit means the invocation failed and cpybkc discards
the output directory; no particular non-zero value means anything beyond that.

## Output

Every file is written beneath `--out`, which cpybkc creates empty for this
invocation and merges into the project's tree only once every generator in the
run has succeeded. Two runs given the same descriptor and the same options
produce byte-identical files: nothing in the output comes from the clock, the
environment, the host, the user or the paths in the argument vector.

So far that is one file, `doc.go`, carrying the package clause. The structs
([#49](https://github.com/Zaba505/cpybkc/issues/49)), the decode and encode
methods ([#51](https://github.com/Zaba505/cpybkc/issues/51)), the file-level
reader and writer ([#52](https://github.com/Zaba505/cpybkc/issues/52)) and the
codec version assertions
([#53](https://github.com/Zaba505/cpybkc/issues/53)) land beside it, and the
table mapping COBOL categories to Go types arrives with the first of them.
