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

The vocabulary grows with what this generator emits: the method receiver the
manifest example in the root `README.md` shows arrives with the decode and
encode methods in [#51](https://github.com/Zaba505/cpybkc/issues/51).

Renaming an item is **not** an option here. A rename is a property of one item
of one copybook, it names its target fully qualified, and it is written in the
layout beside the item it is about — where cpybkc resolves it and carries it
into the IR for every generator to see. See [Names](#names).

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

So far that is two files. `doc.go` carries the package clause and nothing else,
and `records.go` carries [the record structs](#the-record-structs) — one per
record the descriptor describes. A descriptor carrying no record node produces
only the first, because a file holding a package clause and no declaration says
nothing `doc.go` does not.

The decode and encode methods
([#51](https://github.com/Zaba505/cpybkc/issues/51)), the file-level reader and
writer ([#52](https://github.com/Zaba505/cpybkc/issues/52)) and the codec
version assertions ([#53](https://github.com/Zaba505/cpybkc/issues/53)) land
beside them, each in a file of its own.

## The record structs

One exported struct per record, in the order the descriptor's node list carries
them. A group *inside* a record is an anonymous struct inside its record's, so
that the whole of a record is one declaration and this generator names nothing
the copybook or the layout did not: a nested type would need a name, and a name
neither of them wrote is a name an adopter cannot predict and a later copybook
edit can move.

```go
type OrderRecord struct {
	// OrderID is ORDER-ID — numeric, DISPLAY, 5 digits, unsigned, 5 bytes.
	// The layout renames it to OrderID.
	OrderID int32

	// LineItem is LINE-ITEM — a group of 3 members, OCCURS 3.
	LineItem [3]struct {
		// Sku is SKU — alphanumeric, DISPLAY, 8 bytes.
		Sku string
	}
}
```

### Names

An identifier is munged from the item's **rename override** where the layout
gave it one, and from the copybook's own name otherwise. The override is munged
rather than taken as written, so one rule decides what an identifier looks like
whatever it was spelled from — and so a rename is not a hole in the two refusals
below.

Munging drops the separators and opens every word with a capital. What happens
to the rest of each word turns on whether the name was written in one case
throughout:

| The name | The identifier | Why |
|---|---|---|
| `ORDER-ID` | `OrderId` | one case throughout, so the tail is lowered |
| `order_id` | `OrderId` | the same, from the other case |
| `ADDRESS-2ND-LINE` | `Address2ndLine` | a digit opens a word and stands as it is |
| `CustomerID` | `CustomerID` | more than one case, so the tail is kept |
| `custId` | `CustId` | only the first letter is made a capital |

A name written in one case throughout — every copybook name, and most renames —
carries no casing of its own, so this supplies it. A name written in more than
one carries the casing somebody chose, so this keeps it.

That second row is what makes the rename override a control over the identifier
rather than another string to be flattened, and it is the whole of this
generator's answer to Go's initialisms. There is **no** table of them here and
there is deliberately not going to be one: a table is a list of words this
generator has heard of, `OrderId` and `OrderID` would then differ by whether
`ID` made the list, and a name an adopter cannot predict is the thing this
generator refuses to produce everywhere else. If you want `OrderID`, rename the
item to `OrderID` and you get exactly that.

Reserved words need no handling and get none. Every identifier this generator
produces is exported, so it opens with a capital, and every Go keyword is
lowercase; there is no name that munges to one.

Two things are an **error** rather than something quietly worked around:

- **A collision.** Two names that munge to one identifier — two records of a
  descriptor, or two items of one group — are refused, and the refusal names
  both, and the rename where a rename is what was munged. A generator that
  appended a number would put an identifier in your source that your copybook
  does not contain and that a later copybook edit would move from one item to
  the other, with nothing failing while it happened.
- **A name with no Go identifier in it**, one beginning with a digit say. The
  refusal is about the rename where you have already written one, because
  telling you to rename an item you have just renamed sends you to a line you
  have already written.

Whichever name the identifier came from, the **copybook's** name is in the
field's doc comment, and a renamed item's comment also says what the layout
renamed it to. That is what keeps the generated source traceable back to the
copybook: an identifier your copybook does not contain is one you would
otherwise have no way to connect to the item it stands for.

### COBOL to Go

The Go type is a function of the item's `USAGE` and, where it stores a number,
of how many digits its PICTURE declares. It is **not** a function of the item's
width in bytes: a width is what the item occupies in the file, and the two agree
only for `DISPLAY`.

| The item | Go type |
|---|---|
| Alphabetic, alphanumeric and both edited categories — `PIC A`, `PIC X`, `PIC ZZ9.99`, `USAGE DISPLAY` | `string` |
| Numeric `DISPLAY` (zoned), `PACKED-DECIMAL` or `COMP-6`, up to 9 digits | `int32` |
| … 10 to 18 digits | `int64` |
| … 19 digits or more | `*big.Int` |
| `BINARY`, `COMP`, `COMP-4` or `COMP-5`, up to 4 digits | `int16` |
| … 5 to 9 digits | `int32` |
| … 10 to 18 digits | `int64` |
| … 19 digits or more | `*big.Int` |
| `COMP-1` | `float32` |
| `COMP-2` | `float64` |
| `INDEX`, `POINTER`, `NATIONAL` | `[]byte` |

The integer widths are the ones cobol-go's `codec` reads an item into, so the
type an item takes here is the type the accessor #51 will call already returns.
The narrowest that holds every value the PICTURE admits, and no narrower: an
item's declared digits are what a COBOL program may store in it, whatever the
data happens to contain.

A **scaled** item takes an integer all the same, and it holds the *unscaled*
one — the digits as they are stored, with the implied decimal point not applied.
Go has no decimal type and `codec`'s accessors return integers, so a scale
applied here would be applied twice by the time a value reached a caller. Where
the point falls is in the field's doc comment, on every field that has one.

`INDEX`, `POINTER` and `NATIONAL` items are bytes because the IR derives no
logical value for them: each carries a width so that the record's sum stays
correct across it and nothing more. They get a field rather than no field, so
that an item the copybook declares is visible in the record instead of silently
missing from it. An **edited** item is a `string` for the same reason and a
different one: its storage really is characters, and what the field holds is the
edited text as it stands in the record.

### `OCCURS`

A constant `OCCURS n` is an array, `[n]T`, and an `OCCURS DEPENDING ON` is a
slice, `[]T`. The difference is where the number of occurrences comes from: a
constant is a fact of the copybook, so it belongs in the type where nothing can
disagree with the descriptor, and a depending count is data read at run time, so
it cannot.

### Where the slack goes

`ir/SPEC.md`'s *Slack survives a read* requires the bytes of every slack node to
travel with the record, and forbids a generator to oblige its caller to supply
them or to carry them across from the record a reader handed it. So a struct
whose members include slack nodes carries one unexported field for them:

```go
	slack [1][]byte
```

One run per node, in the order the nodes occupy the record. Hiding them
conforms, and hiding them is what this generator does: the bytes are held where
the decode and encode methods can reach them and a caller cannot, so there is
nothing to remember and nothing to corrupt. Retention is per *occurrence*, and
it is per occurrence here because the struct is what an occurrence is — a slack
node inside a group that repeats gets one run in every element of the array or
slice that group became.

An adopter who wants to see those bytes is asking for something this generator
does not offer yet; they are not lost, and surfacing them is an addition rather
than a change to how they survive.

### What is not emitted yet

A **variant** — what a `REDEFINES` inside a repeating group resolves to — is
refused rather than emitted, and the refusal names the node. Go has no sum type,
so an occurrence of such a table is a discriminant beside its arms rather than a
flat set of fields; which shape a caller sees is this generator's to choose
([#90](https://github.com/Zaba505/cpybkc/issues/90)), and it is chosen by the
story that decodes and encodes one
([#51](https://github.com/Zaba505/cpybkc/issues/51)) rather than guessed at by
the one that lays the struct out. A struct that quietly left an arm's items out
would look complete, and an adopter reading it has no way to tell a missing
field from a copybook that never declared one.

Every other record in a descriptor carrying one is generatable, so this bites
only a layout with such a `REDEFINES` in it.
