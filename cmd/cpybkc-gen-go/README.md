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
  "options": {
    "package_name": "orders",
    "import_path": "example.com/warehouse/gen"
  }
}
```

## Options

| Key | Required | What it is |
|---|---|---|
| `package_name` | yes | The Go package the generated files declare. It must be a Go identifier that is neither a keyword, the blank identifier, nor `init`. |
| `import_path` | yes, where the descriptor carries a record | The Go import path the generated package will answer to once it has been merged. |
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

`import_path` is required wherever [the generated tests](#the-generated-tests)
are written, which is wherever the descriptor carries a record. They are an
**external** test package — `package <name>_test` — so they reach the package
beside them by importing it, by path, and that path is the one thing about the
output this generator cannot see. `--out` is a private scratch directory cpybkc
creates for one invocation and discards (`docs/plugin/SPEC.md`, *The scratch
directory*), so it names neither the module nor the directory the files end up
in, and walking up from it for a `go.mod` would find whatever happens to sit
above a temporary directory. A missing one is an **error** rather than a run
that quietly skips the tests: a silent skip is exactly the switch [the test
files are written
unconditionally](#decided-the-test-files-are-written-unconditionally) says this
generator does not have.

The last element of the path need not be the package's own name — an adopter is
free to generate `package ledger` into `.../accounting/v2` — and where they
differ the generated import carries the alias that makes it read as the package
does.

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

`warning` has one use, and it is the middle severity for the reason the middle
severity exists: the generation succeeded and something is missing from it. A
construct this generator emits a reader and a writer for and cannot synthesize a
checkable file for costs its generated test and nothing else, and the warning is
what says which construct and why — see [When a descriptor admits no checkable
file](#when-a-descriptor-admits-no-checkable-file).

## Output

Every file is written beneath `--out`, which cpybkc creates empty for this
invocation and merges into the project's tree only once every generator in the
run has succeeded. Two runs given the same descriptor and the same options
produce byte-identical files: nothing in the output comes from the clock, the
environment, the host, the user or the paths in the argument vector.

So far that is six files: four that are the package, and two that are its tests.
`doc.go` carries the package clause and nothing else, `records.go` carries
[the record structs](#the-record-structs) — one per record the descriptor
describes — `codec.go` carries
[the decode and encode methods](#decoding-and-encoding), and `file.go` carries
[the file-level reader and writer](#reading-and-writing-a-file).

Beside them, [the generated tests](#the-generated-tests) come in two tiers.
`records_test.go` is the record tier, and it covers `records.go` and `codec.go`
together — a struct and the two methods that fill it are one thing to a case:
one case per record type and per variant arm, each decoding a synthesized
literal, asserting every exported field against a value written out beside it,
and encoding it back byte for byte. `file_test.go` is the file tier and covers
`file.go`: the framing around a record and the order records come in, with a
case for every transition predicate and every literal of a set-membership
predicate the automaton carries.

Both tiers are written today, which is why the hand-written `file_test.go` in
each of [the golden packages](internal/) has moved out of the way; see [the
names, and which side moves](#the-names-and-which-side-moves).

Each test file is written exactly when the files it covers are, and nothing else
turns it off ([written
unconditionally](#decided-the-test-files-are-written-unconditionally)). A
descriptor carrying no record node produces only `doc.go` — a file holding a
package clause and no declaration says nothing `doc.go` does not, and a tier with
no record to make a case out of has nothing to write. One whose automaton admits
no record produces neither `file.go` nor `file_test.go`, for the same reason.

There is one more way a test file is absent, and it is a property of the
descriptor rather than of the invocation: a tier every one of whose cases this
generator cannot synthesize bytes for has nothing left to write either. That is
never silent — see [When a descriptor admits no checkable
file](#when-a-descriptor-admits-no-checkable-file) — and it never changes the
four files, which are composed before either tier is.

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
| A `PIC X` item a layout gave the charset `none` | `[]byte` |

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

### An item that carries bytes rather than characters

A `PIC X` item is routinely used to carry something that is not text: a status
flag whose documented values are `0x01` through `0x03`, a region identifier
holding a hex value, a run of packed bits. The copybook cannot say so — `PIC X`
is the only picture there is for it — so the layout says it, on the charset
axis, with the value `none`:

```
(encoding-override (item ORDER-RECORD STATUS-FLAG)
  (charset none))
```

`none` is not a code page and not an identity translation. It is the charset
axis answering that these bytes do not become characters at all, so nothing
translates them, nothing trims them and nothing pads them. The field is a
`[]byte` and not a `string`, and both halves of that matter:

- `encoding/json` writes a `[]byte` as base64, so all 256 byte values survive a
  round trip through your own serialisation. A `string` holding a byte above
  `0x7F` marshals to a replacement character no viewer can show.
- `ReadAlphanumeric` trims trailing spaces, which is right for text and deletes
  a payload byte that happens to be the charset's space. `ReadBytes` trims
  nothing.

Neither loss is recoverable from what you are left holding, and neither says
anything when it happens. That is the whole argument.

On the **writing** side there is one thing to know, and it is the price of the
padding not happening. `WriteAlphanumeric` pads a short value out to the item's
declared width; `WriteBytes` takes no width and pads nothing, so the generated
writer enforces the width itself, on three terms:

| What the field holds | What the writer does |
|---|---|
| a run of the item's width | writes those bytes, as they stand |
| `nil` | writes that many **zero** bytes |
| a run of any other length | **reports** — no truncation, no padding |

Zero for `nil` because a record you built rather than read has to be writable
and there is no pad byte to reach for instead: the space that pads a text item
is a value this item may legitimately hold, which is the reason it is not going
through the text writer in the first place. A wrong-length run is reported
rather than fixed because truncating it would drop bytes you supplied and
padding it would add bytes you did not, and either one moves every item behind
it in the record.

Where the charset is `none`, the item is `USAGE DISPLAY` and its picture is
alphanumeric. A descriptor putting `none` on a `DISPLAY` item of any other
category is **refused** as malformed: an edited item's storage really is
characters and a zoned item's digits are read through the charset's own digit
zone, so there is no reading that honours both. On a usage the charset does not
govern at all — packed, binary, `COMP-5`, `COMP-6`, `COMP-1`, `COMP-2`, `INDEX`,
`POINTER`, `NATIONAL` — it is inert and ignored, exactly as `cp037` is.

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

### The five axes

Neither method chooses an `Encoding`. The character set, the zoned sign
convention, the byte order, the floating-point format and the binary width
staircase are properties of the *file in hand* rather than of an item, so
`codec` carries them on the `Reader` and the `Writer` and the caller states them
once:

```go
r, err := codec.NewReader(f, orders.Encoding())
```

`Encoding()` is generated from the descriptor, so what it returns is what your
layout declared and what `resolve` resolved. It is a value you pass rather than
one anything applies on its own — the same records converted to another
character set are read by passing a different `Encoding`, not by regenerating. A
charset `codec` ships no table for is an **error** rather than a substitution:
generating `cp037` for a descriptor naming `cp500` would read most of a file
correctly and the bracket, currency and accent characters wrongly.

`Binary` is the axis your layout does *not* declare, and it is the one worth
knowing about. It is the width staircase your `COMP` items were compiled under —
`PIC S9(2) COMP` is two bytes under IBM Enterprise COBOL and one under
GnuCOBOL's default — so it is a property of the compiler that wrote the file
rather than of the bytes, and `resolve` puts the staircase it laid your record
out under into the descriptor for this function to read back
([`ir/SPEC.md`](../../docs/ir/SPEC.md#a-binary-items-width-is-the-staircase-not-the-digits)).
Passing a different one does not reinterpret the file: it describes a different
one, because every offset behind the first `COMP` item moves with it. A
staircase `codec` has no member for is an error rather than a substitution, for
the same reason a charset is and with a worse failure — a wrong staircase leaves
every field behind that item at the wrong offset, and nothing in the record
disagrees.

Items whose descriptor gives them **all five** axes are what `Encoding()` is
read off, and they have to agree: `codec` carries one `Encoding` per `Reader`,
so a descriptor whose items disagree on any axis describes a file there is no
single `Encoding` for, and that is reported.

The agreement is per **axis**, not per item. An item whose charset is
[`none`](#an-item-that-carries-bytes-rather-than-characters) is not a party to
the **charset** half of it — it states that its bytes become characters under no
code page, so it makes no claim there to disagree with, and a byte item sitting
beside a `cp037` text item leaves `Encoding()` naming `cp037`. Its sign
convention, byte order, float format and binary width staircase are claims like
any other item's and are held to the agreement, because an `encoding-override`
may name a **group**, and a group holds packed and binary items whose sign and
byte order are read whatever the charset says. A descriptor **no** item of which states a charset
states nothing about the file at all, and no `Encoding()` is generated for it;
you pass your own, exactly as you would for a descriptor holding no item.

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

## The generated tests

`records_test.go` and `file_test.go` are the two files this generator writes
that are not the package. Each holds one case per thing the descriptor
distinguishes, and each case carries the bytes it reads as a **literal** —
which is the artifact, and the reason these files exist.

Everything else covering this generator's output covers it *in this repository*:
the golden packages under [`internal/`](internal/), the hand-written round-trip
assertions inside them, [`example/regenerate_test.go`](../../example/regenerate_test.go), and the
adapter driving the generator through the plugin contract. None of it reaches
the directory you generate into, and the question it leaves unanswered is not
*is the generated code correct*. It is **is the layout I wrote the file I
actually have**. You write a layout, run cpybkc, get a package — and have
nothing showing what a file matching that descriptor looks like. If `PST-TYPE`
is at offset 12 and you wrote 14, the first time you find out is when a real
dataset fails to read, or worse, when it reads and the numbers are wrong.

A case carrying synthesized bytes closes that. You read the literal, hold it
against the file on your desk, and find a wrong offset, a wrong width or a wrong
discriminator literal before opening a real dataset at all. It is the job
[`cpybkc-gen-graph`](../cpybkc-gen-graph/) already does for the automaton, done
for the bytes: that document answers *the right records, in the right order,
told apart on the right bytes, at the right offsets* about the descriptor, and
until these files nothing answered it about the bytes.

### What they catch, and what they cannot

Read this before deciding how much a green run buys you, because the honest
answer is smaller than it looks and the part that is left over is the point.

The bytes in a case are laid out, at generation time, with `cobol-go/codec`'s
own `Writer`, and the generated decoder reads them back with `codec`'s `Reader`.
The same library is on both ends, so **a `codec` bug cancels** — and so does a
bug the generated decoder and the generated encoder share.

What they do catch is worth having anyway:

- the two generated directions **disagreeing** — a decoder and an encoder that do
  not walk the same items in the same order,
- an emitted offset or width that does not match the descriptor's,
- framing emitted wrongly, or checked wrongly,
- an automaton that refuses a file its own descriptor describes.

What is left is **yours**, and no assertion in these files can make it: *the
descriptor being wrong about the real file* is caught by a person reading the
literal. That is not a shortcoming of the feature, it is the feature — the
assertions keep the generated code honest with itself, and the literal is what
you check against the world.

### Decided: the test files are written unconditionally

There is no `tests=` option. Both files come out of every generation that
produces the files they cover, exactly as those files themselves do — there is
no switch, and the emission rule in [Output](#output) is the whole of it.

An adopter who has to discover a flag is an adopter who never gets the
spot-check — and the spot-check is worth most on the first generation of a
layout nobody has read yet, which is exactly the run that has not been
configured. This generator also refuses to carry configuration that does
nothing: an unrecognised option is an error here rather than something ignored
(see [Options](#options)), and a switch whose off position is *a worse first
run* is not a choice a checked-in manifest should have to state.

The cost is two files in a directory that was never yours to edit (see [Your own
tests do not go here](#your-own-tests-do-not-go-here)), and `go test` running
them is the whole of what they ask of you.

*Unconditionally* is about **options**, not about descriptors. A construct this
generator cannot synthesize bytes for still costs its case, and what happens then
is the next section.

### When a descriptor admits no checkable file

Some descriptors admit no file this generator can synthesize bytes for. The
answer is **not** a refusal, and it is not a generated test that fails the first
time you run it either — an adopter whose first `go test` in a brand new package
is red has been told nothing about whether their layout is wrong, this generator
is wrong, or the test is describing something it could not describe.

**A construct with no checkable file costs its case and nothing else.** The four
files that are the package are written exactly as they would have been, the case
is not, and the omission is said out loud twice: as a `warning:` on standard
error, and as a line in the generated test file itself.

```
warning: no test was generated for the record type ORD-DETAIL (OrdDetail): …
note: …
note: its struct, its decoder and its encoder are generated as usual; see
  cmd/cpybkc-gen-go/README.md, "When a descriptor admits no checkable file"
```

The argument is one of proportion. A package that reads and writes your file is
worth more than a spot-check of it, and refusing to generate one over a test
would make the spot-check a **cost** of adopting rather than a benefit — the
exact inversion the generated tests exist to avoid. An adopter with one item this
generator cannot lay out bytes for should still get their reader.

What that costs, and it is worth stating rather than discovering: the output is
no longer a function of the descriptor in the sense a reader might assume. Two
descriptors differing in one field can produce different **sets of files**. That
is already true of `file.go`, which is not written when the automaton admits no
record; this only widens it.

Two shapes are **not** softened, and they are the deny-list `advisory` reads: a
charset `codec` ships no table for (case 2 below), and a reference to a node the
descriptor does not carry. The second has no layout to be unable to synthesize —
the descriptor does not describe one — so it stays the refusal it always was
rather than becoming a warning about a construct nobody can go and look at. The
classification of every error type this generator raises is pinned by a test, so
one added later cannot become a warning by default.

#### The four cases

1. **No finite accepting path.** The automaton offers no walk from its start
   state to a state that accepts, so there is no file of any kind to show. The
   file tier is not written and the warning names the automaton.

   This is a shape `resolve` **does not produce**, and that was established
   rather than assumed: the automaton is compiled from a sequencing expression,
   every state of it is a position in an expression that terminates, and
   acceptance is therefore reachable from all of them. It is checked all the same
   because nothing screens a descriptor in front of a plugin — there is no
   handshake ([The IR version check](#the-ir-version-check)) and `--descriptor`
   is a path a person can hand this program. The finding is recorded beside the
   check, in `filetest.unreachable`, so the next reader does not delete it as
   dead. It is also not a check that could hang: the walk is a bounded
   breadth-first search over the states, and a state is visited once.

2. **A charset `cobol-go/codec` ships no table for.** **This one still fails the
   generation**, and it is the only one that does. A charset `codec` has no table
   for is a charset the *generated code* cannot read the file in either, so there
   is nothing to keep — the package would be a reader that cannot read. It is
   refused where the accessors are emitted, before any synthesizer runs, so it
   reaches you as the one `unsupportedCharsetError` diagnostic and never as a
   second one saying the same thing differently.

3. **An item `codec` cannot lay out.** Every usage the IR carries is one the
   synthesizer lays out today: `COMP-1` and `COMP-2` through `codec`'s float
   accessors, `INDEX`, `POINTER` and `NATIONAL` as the bytes [the IR says they
   are](#cobol-to-go), and the rest through the same table the emitted accessor
   is chosen from. A usage this generator does not know is refused by the *struct*
   emitter, which runs first, so it fails the generation and never reaches a
   synthesizer at all.

   What is left for this case is an item whose **value** cannot be synthesized —
   a discriminator literal that is not the width of the field it tests is the
   reachable one — and the record type it belongs to loses its case. Numeric
   editing is not in this category: an edited item is [a `string` of the edited
   text](#cobol-to-go), which lays out like any other.

4. **A count that cannot be satisfied.** An `OCCURS DEPENDING ON` whose declared
   bounds cannot be reconciled with a guard on the register the count feeds, or
   one count sizing two tables that want different numbers — `docs/ir/SPEC.md`'s
   *"One count may size two tables, and a writer refuses to choose"*, met at
   generation time instead of at run time. The tier that refuses is the one the
   contradiction is in: a *file* the automaton cannot walk costs a file-tier
   case, and a count contradicting itself inside one record costs that record's
   case.

#### Every refusal names the layout construct

A diagnostic here names the **record**, the **item**, the **usage** or the
**count**, and never only the fact that generation was incomplete. *Could not
generate tests* sends you to read this generator; *`TYPE-CODE` is 1 byte wide,
against a literal of 2* sends you to the line of the copybook it is about.

A record type is named by the copybook **and** by the Go type it becomes —
`ORD-DETAIL (OrdDetail)` — because the copybook name is the half you can act on
and the Go type is what you are looking at in the generated directory. A
predicate is named by its node, the item it tests and the bytes it tests for,
which identifies it to whoever is reading the descriptor as well as to whoever is
reading the copybook.

#### Decided: partial coverage is a warning per construct

A record type that cannot be synthesized while its five siblings can leaves a
test file that **looks complete and is not**, and that is the case worth being
noisy about. So it is one line per skipped construct rather than one line naming
a count, because the adopter reading it is deciding whether their file is
covered, and a count does not answer that.

One line per **cause**, though, and not one per goal. An automaton offering no
accepting path fails every goal the file tier carries, and saying that once per
predicate would be a page of warnings about one fact.

The list is **capped** on standard error — those lines go to a terminal beside
every other generator `cpybkc` ran, and a copybook whose every record type
carries the same unsynthesizable item would otherwise bury all of them. The cap
is announced when it is reached.

The cap applies **only to a construct the generated file also names**, and that
is what makes truncating safe rather than lossy: the terminal is scrollback and
the directory is checked in, so a construct in both loses nothing by being
dropped from the first. A tier that skipped every construct it had writes no
file, and those names exist nowhere else — so they are written whatever the
count. A number where a name should be, for the only constructs a reader cannot
go and look up, would be the worst of the three options.

#### Decided: the generated file records what is missing

The header comment of each generated test file names every construct that file
could not cover.

The terminal a generation ran in is scrollback; this directory is checked in. Put
the fact in the file and a copybook that grows an item this generator cannot
synthesize turns into a **line a reviewer sees in a diff** — which is an argument
for it rather than against, and the same one that puts the per-item comment
column beside a case's bytes. A tier left with no case at all writes no file, so
that fact lives only in the warning; there is nothing for a package clause and an
import block over no case to be.

#### Determinism holds through a refusal

The same unsynthesizable descriptor produces the same files **and the same
diagnostics** on every run, in the same order: record tier first, then file tier,
each walking its constructs in the order `docs/ir/SPEC.md` fixes. `cpybkc` reads
those lines, so an order that came from a map walk would be a diff in something.

The warnings are written **after** the files are on disk. Every one of them says
the package was generated anyway, and that is a sentence this program is only
entitled to say once it is true — a generation that fails after a case was
skipped writes no warning, because it wrote no package for the warning to be
about.

#### Not settled: a fixed `OCCURS` too large to be readable

A table of ten thousand fixed occurrences is a descriptor whose honest answer is
*no useful test* — the literal would be a megabyte of source nobody reads — and
it is the same shape as the four above without being a correctness question. No
bound is imposed today. If one is wanted, it belongs here, as a fifth case with
a warning naming the table.

### Decided: bytes are spelled charset-aware

A case's bytes are a readable Go **string literal** where the charset is an
ASCII family, and an annotated **`[]byte{…}`** — one comment per item, naming
the copybook item, its offset and its picture — anywhere else.

This is not a new rule. It is
[`cpybkc-gen-graph`](../cpybkc-gen-graph/README.md#why-an-edge-is-taken)'s,
applied to a whole record instead of to one predicate literal:

> **A literal is bytes unless the field it is tested against is ASCII.** `0x40`
> is `@` in ASCII and a space in cp037, so a document that read a printable byte
> as text because it happened to be printable would print `"@"` for a literal
> that is a space in your file.

The rule keys on the **descriptor's charset**, not on each item's kind, because
a case's bytes are one literal and a literal has one spelling. A record whose
items are not text under any charset — `COMP`, `COMP-3` — therefore reads as
escapes inside the string literal an ASCII descriptor gets, and what to do about
a mixed record is
[#264](https://github.com/Zaba505/cpybkc/issues/264)'s to settle when the first
bytes are actually emitted, and it settled it the same way the keying was
settled: the record is one literal, so a run of `COMP` bytes inside an ASCII
record reads as `\xNN` escapes inside the string rather than moving the whole
record to hex. A record whose *charset* is ASCII is a record whose readable
items read, and the two or three items that are not characters under any charset
are the ones a reader was going to check against a hex dump either way.

What is added here is only which spelling helps at which end. For an EBCDIC file
the slice is the honest spelling and also the useful one — it pastes into
`hexdump` beside the dataset. For an ASCII file the string literal is readable
the way the file is readable, and hex would put a transcription step between you
and the check you came to make. The per-item comments are what make the slice
checkable at all: without them a wrong offset is invisible in a run of hex, and
with them the picture the copybook wrote sits beside the bytes it produced.

### Where a case's values come from

The bytes in a case are synthesized, and *which* value each item holds is a
**rule** rather than a set of numbers somebody picked. That is deliberate and it
is here rather than in the code because it is what a reviewer needs when a
regenerated golden lands: an arbitrary value is not reviewable, and a value that
moves when an unrelated item is inserted makes every regeneration a diff nobody
can read.

The rule, in the order it is applied:

- **A discriminated field holds the literal its predicate requires.** Both kinds
  count — the transition predicate that tells this record from the others in the
  file, and the arm predicate that selects the arm the case is *for* — and where
  one field is both, the arm wins, because the arm is the thing being covered. A
  set-membership predicate contributes its first literal; every literal of one
  is the file tier's to cover. The case asserts the *value* those bytes mean,
  read back through `codec`, rather than the bytes themselves.
- **A count field holds the number of occurrences its tables were laid out
  with**, because the generated writer derives the count it emits from `len()`
  of those tables. Anything else is a case whose bytes cannot come back. Where a
  predicate also names that field, the predicate wins and the *tables* follow
  the literal's own number: the emitted decoder reads its occurrences out of
  those bytes, so a number chosen against the literal is a literal the case
  cannot read back.
- **A variable table takes its declared minimum**, or one occurrence where that
  minimum is zero — so every shape in the record appears at least once, and the
  literal stays short enough to read. Where one count sizes two tables the
  number chosen is the largest any of them asks for, and a count whose tables
  cannot agree on one is refused rather than emitted as a case that cannot pass.
  A table inside an **arm** counts towards that number whichever arm the case
  selects, because an occurrence holding a variant is read whole before any of it
  is decoded and the width of that read is summed from the *first* arm.
  A table counted by a **register** takes none: [the decode method has no
  register file](#a-table-counted-by-a-register), so a record decoded from
  nothing but bytes carries no occurrences of one.
- **Everything else takes a value derived from the item's own picture and its
  position in the record.** An alphanumeric item holds one letter repeated
  across the whole of it, chosen by its offset — a run, because the two things a
  reader is holding the literal against are the item's width and its offset, and
  both are visible at a glance in one: a field a byte too narrow is a run a
  character short, and a field at the wrong offset is a run starting on the
  wrong letter. A numeric item holds its own offset plus one, reduced to what
  the picture's digit count admits and **negated where the picture carries an
  S**, because the sign is the half of a zoned or packed field a positive value
  never exercises. A float holds `offset + 1.5`, which is exact in both formats.
  An item the IR derives no value for at all — `INDEX`, `POINTER`, `NATIONAL` —
  holds bytes numbered from its offset.
- **A slack node, and an item the copybook gives no data-name, hold bytes that
  are neither zero nor either charset's space.** That is the whole of what makes
  `ir/SPEC.md`'s *Slack survives a read* an assertion rather than a coincidence:
  a writer that filled the run instead of emitting what it read would emit
  zeros, so a case whose slack were zeros would pass whether the bytes survived
  or not.

Nothing in a chosen value comes from the clock, the environment, the host or a
path, which is `docs/plugin/SPEC.md`'s *Determinism* over the one file of this
generator's output whose values are chosen rather than copied.

#### The one predicate this generator inverts, and why that is not the rule it stands next to

`ir/SPEC.md`'s *A writer evaluates a predicate, it never inverts one* is a rule
about **writers**: the value a predicate tests is the caller's, over a record
the caller built, so a writer checks it and reports a record satisfying none
rather than quietly storing the literal the predicate wanted. Nothing emitted
here changes that — the generated writer still refuses, and these cases are what
show it *accepting* the record it should.

What happens at generation time is a different act. There is no caller. The
descriptor states the literal outright, and laying it into a record this
generator is inventing decides nothing on anybody's behalf; a case that ignored
it would instead show an adopter a run of bytes their own reader would never
admit. The site is commented to say so, because a reader meeting it beside that
rule deserves the distinction rather than a suspicion.

### Decided: a case asserts round-trip and field values, over every discriminator path

One case does three things, in this order. It decodes its bytes; it asserts
every **exported** field against a value written out beside it; it re-encodes
the record and asserts the bytes came back byte for byte. Unexported state — the
[slack a record retains](#where-the-slack-goes) — is named by no case and is
covered by the round-trip half alone, which is the point of [the package
clause](#the-package-clause).

The expected values are the ones the synthesizer wrote into the bytes in the
first place. That makes the field-value half the generated decoder checked
against `codec`'s own `Writer` — a second implementation of the same layout,
which is what makes it a check rather than an echo, and it is why an offset or a
width the generated walk got wrong shows up as a field that reads as something
else. What it does not survive is a bug the synthesizer and the generated
decoder share, which is the cancellation [What they catch, and what they
cannot](#what-they-catch-and-what-they-cannot) describes and this half does not
escape.

So the field-value half is not redundant with the round-trip half. Round-trip
alone is satisfied by a decoder and an encoder that agree with each other — it
says the walk is *consistent*, not that it lands where the bytes were put. It is
also the half that makes the literal legible: *these five bytes are the order
id, and they read as 12345* is the sentence that turns a wall of hex into a
spot-check, and a byte literal on its own cannot say it.

Coverage is not one happy path. **Every record type, every variant arm, every
transition predicate and every literal of a set-membership predicate** is
exercised by some case. A discriminator that no case covers is one whose
spelling you find out about from a production file, and a set-membership
predicate is exactly where a literal goes wrong one value at a time — the arm
nobody generates a case for is the arm nobody reads.

### Decided: the file tier covers by predicate, not by edge

A file-tier case is a whole file — the framing, and the records in an order the
automaton admits — and between them the cases satisfy **every transition
predicate the descriptor carries and every literal of a set-membership one**.
Transitions that share a predicate are covered **once, by that predicate**, not
once per edge.

This has to be decided rather than assumed, because it is the difference between
a readable file and an unreadable one.
[`example/graph/graph.md`](../../example/graph/graph.md) draws nine states and
fifty transitions for one ordinary ledger — every posting state offers all six
posting types — so a full edge cover would be fifty cases for the worked example
alone, over seven. What an adopter is checking is *this record type is selected
on these bytes*, which is a property of the **predicate**; the guards that make
two edges carrying one predicate distinct are the automaton's business, and
[`cpybkc-gen-graph`](../cpybkc-gen-graph/) has already drawn them.

So it is a bound on what these cases prove, and it is stated here for that
reason: a green file tier says every way a record can be *told apart* has been
read and written back. It does not say every way the automaton can *get there*
has been.

A literal of a set-membership predicate is not covered by another literal of the
same set. `is one of "DA" or "DB"` is two ways a file can be spelled, and an
adopter who has checked one has not checked the other, so each gets a case.

A predicate **no file reaches** is refused rather than skipped: a transition no
reader will ever take selects a record the automaton describes and no file can
hold, which is a bug in whatever produced the descriptor. Refusing is the answer
this generator gives everywhere else a descriptor cannot be walked, and a test in
this package holds it to the rule so that a predicate quietly falling out of
reach cannot regenerate into a smaller golden nobody reads.

### Decided: shortest first, and a counted run walked twice

The path a case walks is the **shortest** the automaton offers that reaches the
predicate being covered: the shortest walk from the start state to a state
offering that transition, the transition, and the shortest walk from where it
lands to a state that accepts. Where more than one state offers the transition,
the candidates are ordered by the length of the file each produces, so the case
an adopter gets is decided by how long it is rather than by which state the
descriptor happens to carry first. Every predicate the path passes through on the
way is struck off with it, so two predicates on one path cost one case rather
than two. Shortest first is what keeps a case readable — the literal is as long
as the path — and it is why the ledger gets seven cases rather than fifty.

Where the state a record lands in offers the same predicate again, the record is
taken **twice**. One is enough to reach the predicate and is shortest to read,
and it is still not enough to see: a counted run walked once cannot tell a
register that was decremented from one that was tested, and a file of one record
under a separator placement carries no separator at all — which is half of the
framing [the golden packages](internal/) exist to exercise. Two costs one more
record in the literal and shows both. It is a constant in the generator rather
than an option, because the file a case carries is the artifact and a number a
run could set is a literal an adopter cannot predict from their own layout.

### Decided: the count is chosen before it is stored

A transition may bind an integer register out of a field of the record it just
admitted, and a later transition may guard on that register and take one off it.
`example/ledger.sexpr` is exactly that shape: the header states how many postings
follow, the header's edge binds `HDR-COUNT` into a register, each posting edge is
guarded *greater than zero* and decrements it, and the trailer's edge is guarded
on zero.

Nothing downstream fixes such a field up — `ir/SPEC.md`'s *"A writer evaluates a
guard, it never back-fills a count"* — so the number has to be **chosen first and
then written into the field**. The choice is made over the path rather than by a
search over values: every guard the path evaluates and every table of the path's
records whose count is a register becomes one requirement on the value a binding
put in a register, shifted by the decrements standing between the two. The set to
solve over is closed and small — the predicates are `BytesEqual` and
`BytesOneOf`, the guards are `equals`, `one_of` and `greater_than_zero`, and the
bindings are from a field or `Decrement`, and there is no fourth of anything — so
each binding is settled on its own: the requirements naming a set intersect, the
requirements naming a bound narrow, and the smallest value satisfying both wins.

A binding **nothing along the path constrains** is left alone, and its field
takes the value [the record tier's own rule](#where-a-cases-values-come-from)
gives it. That is not a gap: a register no guard reads and no table is counted by
is a register whose value nothing along the path can tell apart.

A field the admitting predicate already pins is **read** rather than chosen — the
predicate decides whether the record is admitted at all — and a path whose guards
that value cannot satisfy is refused in favour of the next candidate.

Two consequences worth stating. A table counted by a register carries that many
occurrences at the file tier, where the automaton has run, and **none** at the
record tier, where it has not — see [a table counted by a
register](#a-table-counted-by-a-register); the two tiers show the same record
with different extents on purpose. And every candidate path is checked against
the walk the generated reader would actually make: both directions evaluate a
state's transitions in the order the state carries them and take the first that
is eligible, so a path is only a path if every earlier transition of every state
along it is excluded — by a guard that does not hold, or by a predicate the
record's own bytes do not satisfy.

### Decided: the file tier asserts the record types, their order and the discriminator

One file-tier case reads its bytes with the generated `Reader`, checks that the
records came back **in the order the automaton admits them and as the types the
descriptor names**, checks the field each transition's predicate names, writes
them all back with the generated `Writer` and checks the bytes are the bytes it
started from.

Not every field of every record. That is a deliberate narrowing of [the record
tier's rule](#decided-a-case-asserts-round-trip-and-field-values-over-every-discriminator-path)
and it is taken rather than fallen into: each record already has a case of its own
one file over, where every exported field of it is written out beside its value,
and the ledger's six posting types asserted field by field inside seven files
would be a literal nobody reads to the end. What is left is what this tier is
*for* — the framing, the order, and the bytes a record is told apart on — and
byte equality over the whole file is what keeps the round-trip half as strong as
the record tier's.

The framing is **in the literal**, never stripped or elided: a record descriptor
word is four bytes an adopter will see in a hexdump and want to recognise, and a
segmented record's own runs are cut at the segment boundaries with the piece
behind each marked as a continuation.

A case is named after the file it holds — `TestALedgerHeaderThenTwoDebitPostingsThenALedgerTrailer`
— with runs collapsed. Long, and that is the trade: the name is the first thing a
reader sees in a failure and the only summary of the path the case walks, and a
numbered one would be short and say nothing.

### The style, and why it inverts this repository's own

A generated case is written to four rules:

- **No package-level variables, and no global state.**
- **No helpers.** Not `assertBytes`, not `roundTrip`, not `laidOut`.
- **No separate test data.** No `testdata/`, no shared fixture, no builder — the
  bytes are inline in the case that reads them, as a literal.
- **One top-level `func Test…(t *testing.T)` per case**, carrying its own bytes,
  its own expected values and its own assertions.

Every one of those is broken, deliberately and correctly, by the hand-written
tests in this repository:
[`internal/orders/roundtrip_test.go`](internal/orders/roundtrip_test.go) has
`ascii`, `laidOut`, `roundTrip`, `assertBytes`, `orderBytes`, `tableBytes` and
`entryBytes`, and it states the opposite policy on literals outright, with a
good reason:

> The original bytes are laid out here with codec's own accessors rather than
> written out as hex. That is independent of what is under test in the way that
> matters: what these tests exercise is the generated *walk* […] and a
> hand-written EBCDIC literal would be a second thing to get wrong without
> making the walk any more checked.

Both inversions come from one place. A helper amortises over the cases a person
would otherwise write out by hand; a generated file amortises nothing, because a
machine writes every case and nobody edits any of them. What is left of a helper
is its cost, and that cost is paid at the worst possible moment — a failing
generated test is read by somebody who did not write it, under time pressure,
and every helper is one jump away from the line that failed and the bytes that
caused it. The literal rule inverts for the same reason: nobody hand-writes this
one, so there is no second thing to get wrong, and the literal here is not a
means to an assertion — it *is* the artifact.

So the trade this repository makes everywhere else reverses inside a generated
file, and that file is verbose on purpose. The six hand-written tests keep their
approach: they are testing the walk, not showing the file.

### The names, and which side moves

`records_test.go` and `file_test.go`, mirroring the source files whose output
each covers. A name is part of this generator's output, so it is fixed here
rather than left to a run: an adopter grepping for the case that covers a record
should land in the file named after the file that declares it.

`file_test.go` is a name already in use in this repository — all six golden
packages under [`internal/`](internal/) carry a hand-written one — and **the
hand-written side moves.** The generated name is seen by every adopter of this
generator and cannot be renamed by any of them; the hand-written files are this
repository's own, read by contributors who can be told once. Trading the
adopter-facing name away to keep six internal ones would buy nothing and cost
the only one that is a contract. Exactly seven files move, and here is the whole
of it: the `file_test.go` in each of the six packages becomes
`file_roundtrip_test.go`, and `orders` — the one package that also carries a
record-level `roundtrip_test.go` — has that become `record_roundtrip_test.go`.
The second rename is not forced by a collision; it goes with the first so that
which layer each file asserts stays legible once `file_test.go` means something
else in the same directory.

The rename landed with the first change to what is emitted
([#264](https://github.com/Zaba505/cpybkc/issues/264)) rather than ahead of it,
so that a rename and the file it makes room for are one diff rather than two —
and so that the seven moved files are read beside the reason they moved. The
tier that collides with them landed one pull request later
([#265](https://github.com/Zaba505/cpybkc/issues/265)), which is what the rename
bought: a rename and the file that displaces it are two diffs a reviewer can
read rather than one nobody can.

The golden packages hold both kinds of `_test.go` for that reason, and the two
tests that pin them tell one from the other by the `// Code generated … DO NOT
EDIT.` header rather than by name. It is the honest signal — every generated
file in this repository already carries it, no hand-written one may, and a rule
keyed on a name would have to be changed again the next time a tier is added.

### The package clause

`package <name>_test` — an external test package, beside the generated package
rather than inside it.

Everything a case needs is exported: `Encoding()`, `NewReader`,
`(*Reader).Next`, `NewWriter`, `(*Writer).Write` and `Close`, and each record's
`UnmarshalCOBOL` and `MarshalCOBOL`. The one thing that looks as though it might
need more is the [slack a record retains](#where-the-slack-goes), and it is the
thing an external package demonstrates best: slack survives a round trip through
the public surface without a case ever naming the field it lives in.

So the clause is what makes *the public API is sufficient to read and write this
file* something this generator asserts rather than assumes. It is the opposite
choice from the assertions inside the golden packages, and both are right: those
are `package orders` because two of their criteria cannot be stated from outside
at all, and they are testing the walk from within. These are showing you the
file from where you sit, which is outside.

### Your own tests do not go here

The generated directory is cpybkc's. It is merged wholesale from a scratch
directory this generator is handed empty on every run
([#43](https://github.com/Zaba505/cpybkc/issues/43)), and a file a previous run
produced that this one did not is pruned against `cpybkc.gen.json`
([#45](https://github.com/Zaba505/cpybkc/issues/45)). **A test file you add
there is a file you lose**, quietly, at the next generation — and an edit you
make to a generated case is lost the same way.

Put your own tests in a package of your own that imports the generated one.
Everything the generated cases touch is exported, so anything they can do yours
can too; what they cover is that the descriptor's own records read and write,
and what is yours to cover is what your program does with them.

The golden packages under [`internal/`](internal/) are not a counter-example,
though they look like one: they hold hand-written `_test.go` files in a
directory that is otherwise generated output. They are checked into *this*
repository and regenerated by a test that compares bytes — they never travel the
scratch-directory merge an adopter's output does, and the golden comparison
tells the hand-written files from the generated ones by the `// Code generated …
DO NOT EDIT.` header rather than by the `_test.go` suffix, so a generated case
is still pinned byte for byte while a hand-written one beside it is left alone.
Nothing in a project cpybkc generates into works that way.

### A separate `cpybkc-gen-gotest`, considered and rejected

Recorded so that it is not proposed again. The contract permits it:
[`docs/plugin/SPEC.md`](../../docs/plugin/SPEC.md) lets two generators share an
`out` directory as long as no output *path* collides
([#44](https://github.com/Zaba505/cpybkc/issues/44)).

It is rejected because a test package is a function of everything the package
beside it is. A second generator would have to re-derive `package_name`, the
identifier munging, the record struct shapes and the extent arithmetic — from
the same descriptor, in a second implementation, with nothing holding the two in
step. Its failure mode is the worst one available: a test package that disagreed
with the package beside it compiles, fails, and blames the wrong side, so the
adopter debugs a layout that was right. A generator writing tests for its own
output has nothing to disagree with. The manifest cost is real too — a second
entry, a second binary on `PATH`, a second thing to version — bought for a
capability nobody asked for, since these files are not optional and there is no
one who wants the package without them.

### `docs/plugin/SPEC.md` needs no word

Nothing here changes the plugin contract, and nothing in it needs adding. The
contract fixes *where* a generator writes and what it may not do — not how many
files it writes, nor what any of them is for — so two more files from this
generator are already inside it. A clause about test output would be a rule
about one generator written into the document that governs all of them.
