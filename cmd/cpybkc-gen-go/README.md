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
| `receiver` | no | The identifier the decode and encode methods declare their receiver under. It must be an unexported Go identifier that is not a blank. Defaults to `x`. |

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

`receiver` is an option rather than something derived from each record's name
because Go's convention for a receiver is a shop's rather than a generator's —
one letter, the same letter on every method of a type. Deriving it would give
one package a different receiver per type, which is the one shape the convention
rules out. It is required to be unexported, and that is what keeps it clear of
every identifier munged from a copybook name, all of which are exported.

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

So far that is four files. `doc.go` carries the package clause and nothing
else, `records.go` carries [the record structs](#the-record-structs) — one per
record the descriptor describes — `codec.go` carries
[the decode and encode methods](#decoding-and-encoding), and `file.go` carries
[the file-level reader and writer](#reading-and-writing-a-file). A descriptor
carrying no record node produces only the first, because a file holding a
package clause and no declaration says nothing `doc.go` does not, and one whose
automaton admits no record produces no `file.go` for the same reason.

The codec version assertions
([#53](https://github.com/Zaba505/cpybkc/issues/53)) land beside them, in a
file of their own.

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

The Go type is a function of the item's `USAGE`, of how many digits its PICTURE
declares where it stores a number, and — for a binary item — of whether that
PICTURE carries an `S`. It is **not** a function of the item's width in bytes: a
width is what the item occupies in the file, and the two agree only for
`DISPLAY`.

| The item | Go type |
|---|---|
| Alphabetic, alphanumeric and both edited categories — `PIC A`, `PIC X`, `PIC ZZ9.99`, `USAGE DISPLAY` | `string` |
| Numeric `DISPLAY` (zoned) or `PACKED-DECIMAL`, up to 9 digits | `int32` |
| … 10 to 18 digits | `int64` |
| … 19 digits or more | `*big.Int` |
| `COMP-6` — the three rows above, by digit count | `int32`, `int64`, `*big.Int` |
| **Signed** `BINARY`, `COMP`, `COMP-4` or `COMP-5` — `PIC S9(n)` — up to 4 digits | `int16` |
| … 5 to 9 digits | `int32` |
| … 10 to 18 digits | `int64` |
| **Unsigned** `BINARY`, `COMP`, `COMP-4` or `COMP-5` — `PIC 9(n)` — up to 18 digits | `uint64` |
| Either, 19 digits or more | `*big.Int` |
| `COMP-1` | `float32` |
| `COMP-2` | `float64` |
| `INDEX`, `POINTER`, `NATIONAL` | `[]byte` |

The integer widths are the ones cobol-go's `codec` reads an item into, so the
type an item takes here is the type the accessor `codec.go` calls already
returns.
The narrowest that holds every value the PICTURE admits, and no narrower: an
item's declared digits are what a COBOL program may store in it, whatever the
data happens to contain.

A **zoned or packed** item takes a signed type whether or not its PICTURE
carries an `S`, and a **binary** one does not. That is not an inconsistency
here; it is `codec`'s surface. A zoned or packed item stores its sign in a
nibble or a zone, so an unsigned one is a signed reading that never comes back
negative and `int32` holds it. A binary item stores two's complement, where the
top bit is a digit in an unsigned item and the sign in a signed one — `FF FF` is
65535 unsigned and -1 signed, and `codec` documents that the difference is not
recoverable from the bytes. Which accessor is called is the only thing that says
which the copybook declared, so `PIC 9(4) COMP-5` reads through
`ReadComp5Uint64` and `PIC S9(4) COMP-5` through `ReadComp5Int16`.

`uint64` for an item of four digits is wider than the PICTURE needs, and it is
what `codec` returns: there is no narrower unsigned reader, so a narrower field
would put a conversion either in the generated decode — where a value could
change with nothing saying so — or in the encode that widened it back. The one
place the rule stops is 19 digits and up, which is sixteen bytes: `codec` has
only the `Big` family there, so an item that wide takes a `*big.Int` whether it
is signed or not.

The unsigned rows are **new**, and regenerating an existing package moves every
unsigned binary field off the `int16`, `int32` or `int64` it used to take. That
is a source-breaking change for code already written against a generated
package: an assignment into a narrower variable, or a helper taking an `int64`,
stops compiling until the call site is updated. It is deliberate — the old types
were the defect, and `PIC 9(4) COMP-5` holding 65535 read back as -1 through them
— and a compiler error at every such call site is the loudest way to say so.

A **register** an unsigned binary field is bound into is the one place a `uint64`
does not simply widen. A register holds an `int64`, and a `COMP-5` item is
bounded by its storage rather than by the decimal range its PICTURE declares, so
an unsigned `PIC 9(18) COMP-5` can hold a value no register can. The generated
reader range-checks it and reports, rather than converting it to a negative
number every guard downstream would then test.

A **`COMP-6`** item shares a row's worth of Go types with `PACKED-DECIMAL` and
shares nothing else. It is packed decimal with **no sign nibble at all**, so it
is `ceil(digits / 2)` bytes where `PACKED-DECIMAL` of the same digit count is
`ceil((digits + 1) / 2)` — `PIC 9(4) COMP-6` is two bytes where `PIC 9(4) COMP-3`
is three — and it is always unsigned, because there is nowhere in the field for a
sign to be recorded. It is therefore read and written through `codec`'s own
`ReadComp6*` and `WriteComp6*` accessors and never the packed ones: a packed
reader over a `COMP-6` field takes the item's last digit for a sign and consumes
a byte past the end of it, which moves every field behind it in the record. An
item whose PICTURE carries an `S` under `COMP-6` is refused rather than read as
unsigned, and the refusal names the item.

Those accessors are `codec`'s and not this generator's, which was a decision
rather than a given: `codec` shipped no `COMP-6` family at all when the defect
was found, and emitting the nibble arithmetic into every generated package was
the alternative. Upstream won for the reason every other usage is served from
there — the byte-level meaning of a `USAGE` is `codec/SPEC.md`'s, a generator
that carried its own would be a second reading of it, and a generator in another
language would then need a third. What this generator decides is which accessor
an item takes, and that is the whole of its share of the question.

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

### An item your copybook gives no name

`FILLER` is generated, not refused. It has no data-name, so there is nothing to
munge into an identifier and nothing in the table above to look it up in — and
it is in most copybooks anybody actually has, so a refusal here is a refusal of
the whole package over an item nobody was looking at.

There are two shapes, and they are two decisions rather than one:

| The item | What is generated |
|---|---|
| An elementary `FILLER` | no field; its bytes are retained beside the slack, in an unexported `filler` run |
| A `FILLER` **group** | no field; its members become fields of the enclosing struct |

```cobol
       01  ORDER-RECORD.
           05  ORDER-TYPE              PIC X(2).
           05  FILLER.
               10  NOTE-CODE           PIC X(2).
           05  FILLER                  PIC X(8).
```

```go
type OrderRecord struct {
	// OrderType is ORDER-TYPE — alphanumeric, DISPLAY, 2 bytes.
	OrderType string

	// NoteCode is NOTE-CODE — alphanumeric, DISPLAY, 2 bytes.
	NoteCode string

	filler [1][]byte
}
```

An elementary `FILLER` holds no value anybody named, so it is retained the way
slack is — see *Where the slack goes*: one run per item, in the order they
occupy the record, one set per occurrence of the struct, so a `FILLER` inside a
group that `OCCURS` gets a run in every element of the array or slice that group
became. A record that was read writes those bytes back exactly; a record you
built rather than read carries no run for them and the writer emits zero bytes,
for the reason it does for slack.
If a `FILLER` is somewhere you need a particular value, that item is not filler:
give it a data-name in the copybook and it becomes a field like any other.

A `FILLER` **group** is the half that had to be argued rather than derived. A
run of retained bytes cannot stand in for it, because it holds members your
copybook *does* name and hiding them would lose items you can see in your own
source. So its members are lifted into the enclosing struct — which is what
COBOL already says about them: qualification skips an unnamed group, so
`NOTE-CODE` above is `NOTE-CODE OF LEDGER-TRAILER` in a program and there is no
intermediate name it could be qualified by. If lifting them makes two names
collide, that is reported like any other collision.

Nothing is named `Filler1`, `Filler2`, or anything else positional. Such a name
is one your copybook does not contain and one that moves the moment an unrelated
item is inserted ahead of it, which is what this generator refuses to produce
everywhere else.

Three cases are **refused**, and the refusal names the record or group holding
the item rather than calling your descriptor malformed:

- A `FILLER` **group that repeats**. Its members would have to move up a level
  once per occurrence, and there is no name for the occurrences to be an array
  of.
- A `FILLER` whose `OCCURS DEPENDING ON` count is data. The retained run would
  have to be as long as a count field says, over bytes you cannot supply.
- A `FILLER` standing as one alternative of a `REDEFINES` inside a table. An
  alternation is a field per alternative and exactly one of them is non-nil;
  an alternative with no name has no way to say it is the one the record holds.

In all three the answer is in the copybook: give the item a data-name. And in
none of them is the descriptor at fault — `ir/SPEC.md`'s *Names* says what a
**named** node carries and never that every node is named, so an item with no
names message is exactly what a producer emits for a `FILLER`. A names message
that is *present* and states no name is the opposite: a named item whose name
went missing, which is a bug in whatever produced the IR, and that one is still
reported as a malformed descriptor.

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

### A `REDEFINES` inside a table

A **variant** — what a `REDEFINES` inside a repeating group resolves to — is one
field per alternative, each a pointer, and exactly one of them is non-nil in an
occurrence:

```go
	Entry [3]struct {
		// EntryType is ENTRY-TYPE — alphanumeric, DISPLAY, 1 byte.
		EntryType string

		// EntryDetail is ENTRY-DETAIL — a group of 2 members.
		EntryDetail *struct{ ... }

		// EntrySummary is ENTRY-SUMMARY — a group of 2 members.
		EntrySummary *struct{ ... }
	}
```

Go has no sum type, so something has to say which alternative an occurrence
holds, and `ir/SPEC.md` deliberately leaves the spelling to the generator. Every
other spelling wants an identifier neither your copybook nor your layout wrote:
a discriminant field needs a name, a type for it needs a name, and a constant
per arm needs one at package scope. The alternation itself has no name in the
copybook — the redefined item is the first arm and carries its own — so nil and
non-nil say the same thing in names you already have.

Decoding fills exactly one of them, per occurrence, and leaves the others nil.
Encoding writes the one that is non-nil; an occurrence holding none, or more
than one, is an error naming the record, the table and which occurrence it was.

## Decoding and encoding

Every record type gets two methods, and they are `codec`'s own interfaces:

```go
func (x *OrderRecord) UnmarshalCOBOL(r *codec.Reader) error
func (x *OrderRecord) MarshalCOBOL(w *codec.Writer) error
```

So a record is a `codec.Unmarshaler` and a `codec.Marshaler`, which is what
`codec.Unmarshal` and `codec.Marshal` take. Nothing in them is byte-level: the
widths, the digit counts, the sign position and the signedness come out of the
resolved IR and every byte is `codec`'s, which is the same arrangement
`avroc-gen-go` has with `avro-go`. There is no reflection.

### The four axes

Neither method chooses an `Encoding`. The character set, the zoned sign
convention, the byte order and the floating-point format are properties of the
*file in hand* rather than of an item, so `codec` carries them on the `Reader`
and the `Writer` and the caller states them once:

```go
r, err := codec.NewReader(f, orders.Encoding())
```

`Encoding()` is generated from the descriptor, so what it returns is what your
layout declared. It is a value you pass rather than one anything applies on its
own — the same records converted to another character set are read by passing a
different `Encoding`, not by regenerating. A charset `codec` ships no table for
is an **error** rather than a substitution: generating `cp037` for a descriptor
naming `cp500` would read most of a file correctly and the bracket, currency and
accent characters wrongly.

### What the writer supplies, and what it refuses to

`ir/SPEC.md`'s *What the descriptor determines, a writer supplies* makes exactly
two values the descriptor's rather than yours.

An **`OCCURS DEPENDING ON` count** is emitted as the number of occurrences
written, whatever you left in the count field: the field's value *is* that
number, and a count disagreeing with what follows it is a record this reader
cannot walk. Where more than one table names one count — which is ordinary, and
IBM documents it — the numbers have to agree, and where they do not the writer
**reports** rather than picking. Picking would size one of the tables by a
number that was never about it and slide every item behind it, out of a
descriptor saying nothing is wrong.

**Slack** is emitted as [what was retained for it](#where-the-slack-goes), and
as zero bytes only where the record carries none, because its caller built it
rather than read it. A retained run whose length is not the slack node's is
reported rather than truncated or padded.

Everything else is yours, including the value a discriminator tests. A writer
*evaluates* a predicate against the record it is about to emit and never derives
a satisfying value to store into the predicate's target — see `ir/SPEC.md`'s
*A writer evaluates a predicate, it never inverts one*, which is why that field
is still a field you fill.

### A table counted by a register

Where a table's count is a register rather than a field of the record, decoding
reads as many occurrences as the record it was handed already carries, and
encoding writes exactly that many. A register holds what a transition bound out
of a record already read, so the value is the automaton's — the file-level
reader ([#52](https://github.com/Zaba505/cpybkc/issues/52)) sizes the table and
then hands the record over. The bounds the copybook declared are checked here
either way, because those are the copybook's.

## Reading and writing a file

`file.go` is the file node and the automaton: the framing around a record, and
the order records come in. Nothing in it is a table this generator interprets at
run time — the states, their transitions and the predicates selecting them are
emitted as Go, so what you read is the walk your layout describes rather than an
engine with your descriptor inside it.

```go
r, err := orders.NewReader(f, orders.Encoding())

for {
	rec, err := r.Next()
	if errors.Is(err, io.EOF) {
		break
	}
	if err != nil {
		return err
	}

	switch rec := rec.(type) {
	case *orders.OrderRecord:
		...
	}
}
```

`Next` returns `io.EOF` and only `io.EOF` when the file is complete. A file that
was cut short is an error of its own and never wraps `io.EOF`, so the two are
told apart with `errors.Is` rather than by reading the message.

`Record` is the interface both directions take, and it is `codec.Unmarshaler`
and `codec.Marshaler` together rather than a method this generator invented —
every record type here already implements both, and a marker method would be an
identifier neither your copybook nor your layout wrote. `Reader`, `Writer`,
`Record`, `NewReader` and `NewWriter` are the five identifiers this file
occupies at package scope, and a record whose name munges to one of them is a
**collision** and an error, exactly as two items that munge alike are.

Writing is the same walk in the other direction, and it ends with a `Close` that
is not the file's:

```go
w, err := orders.NewWriter(f, orders.Encoding())

for _, rec := range records {
	if err := w.Write(rec); err != nil {
		return err
	}
}

return w.Close()
```

`Close` reports a current state that does not accept, or whose acceptance guards
do not all hold, rather than closing the file — a group that promised four
details and was given three is caught there. It does not close the `io.Writer`
you handed it, which is yours.

Neither direction holds more than one record. A count in a header is never
back-filled from the records behind it: holding a group to count it gives up the
streaming property, and a writer that has emitted a record cannot reach back
into a stream it does not own.

### What the framing does

`ir/SPEC.md`'s [Physical framing](../../docs/ir/SPEC.md) settles all of this and
this generator implements it rather than restating it. Three consequences are
worth knowing before you read the generated code:

- **A record's end comes from its extent**, never from a search of the input.
  `0x15` sits inside any `COMP-3` field holding a value like `+152.50`, so a
  reader scanning for a delimiter cuts the record there and fails three records
  later, somewhere the corruption did not happen.
- **The framing is checked against that extent**, and a disagreement is reported
  at the record it happened on: a descriptor word whose length is not the
  record's extent, or a delimiter that is not where the extent ends. **unframed**
  buys none of that, which is a property of the dataset rather than a choice
  made here.
- **End of input is tested at a record boundary and nowhere else**, so a file
  whose last record carries a well-formed trailing delimiter is complete rather
  than holding a record the layout does not describe.

A file is not byte-identical in two cases, and both are deliberate. Under
**optional terminator** the writer emits the final delimiter rather than
choosing whether to, because two writers left to decide produce two different
files from one descriptor and one set of records. And under **segmented** it
lays each record into as few segments as the file node's largest allows,
whatever the input did. A *record* is byte-identical either way.

### What the automaton remembers

Where the descriptor carries registers, both directions carry a register file:
guards are checked before a transition's predicate, and its bindings are applied
after the record is admitted or emitted. Three things follow that a caller
meets:

- A record a **guard** excluded is reported as that, naming the register the
  descriptor carries it under, rather than as a record the layout does not
  describe. The two send you to different places — one is a record that does not
  belong at this point in the file, the other a record type your layout is
  missing.
- A transition carrying **no predicate** matches every record. It is not a
  fall-through: it is evaluated in the order the state carries it like every
  other transition, and a guard-excluded one never displaces the
  undescribed-record diagnostic, because it would have matched whatever was
  there and so says nothing about the bytes in hand.
- A table whose count is a **register** is sized from that register by the
  reader and checked against it by the writer, and a caller supplying a
  different number is reported. Where two tables of one record name that one
  register, each is checked: neither of them sets it, so there is nothing to
  pick between.

A writer never derives a discriminating value. It evaluates the predicate of the
transition it would take against the bytes it is about to emit and reports a
record satisfying none, naming the record — see `ir/SPEC.md`'s *A writer
evaluates a predicate, it never inverts one*.
