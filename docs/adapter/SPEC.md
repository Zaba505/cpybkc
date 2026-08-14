# The Conformance Adapter Contract

## Overview

The [conformance corpus](../conformance/SPEC.md) is a set of small files with
the right answer written down, so that a generator written by somebody who has
never seen this repository can be asked the same question `cpybkc-gen-go` is
asked. That document specifies the files. It does not specify how the question
is *put* — how a program in some other language is started, told which
descriptors to generate code from, handed a file's bytes, and asked what its
generated code made of them.

This document specifies that. It is a **process contract**: the unit of
interoperability is an operating-system process, which the engine starts and
then speaks to over standard input and standard output in JSON, one object per
line. The program on the other end of those two streams is an **adapter**. A
container image is one way to produce such a process, and the way to produce a
result somebody else should believe (#203) — but the image is a door onto this
contract rather than the contract itself. Making the image the unit would put a
registry pull and a build in front of the first thing an outsider runs, before
they are invested enough to pay for either.

What an adapter is asked about is not here. Which entries exist and what each
covers are the corpus's own, in
[`testdata/conformance/README.md`](../../testdata/conformance/README.md). What
an entry is made of, the value language a decoded record is written in, and what
it means for a document to carry a failure are the [conformance corpus
format](../conformance/SPEC.md)'s; every values document a frame below carries
is one of that document's, unchanged. What the bytes of a record mean is the
[resolved IR](../ir/SPEC.md)'s. How an adapter's own generator is found and
invoked is the [plugin contract](../plugin/SPEC.md)'s — the engine never invokes
a generator, and an adapter driving a `cpybkc-gen-<name>` executable drives it
through that contract like any other caller. What an image built `FROM` the
published base may rely on is the [container
contract](../container/SPEC.md)'s.

What the answer is written in belongs there; how the question is put, and how
the answer comes back, belongs here.

### Scope

In scope: the framing of the conversation and the hazard that framing exists to
survive; the operations an adapter serves and the order they arrive in; what an
adapter declares about itself before it is asked anything; how a refusal, a
fault and a broken adapter are told apart; and the rule that the adapter is
never handed the answers it is being checked against.

Out of scope, with reasons, in [Out of Scope](#out-of-scope).

### Governing sources

- **[`conformance/SPEC.md`](../conformance/SPEC.md)**, *The conformance corpus
  format* — normative for every values document a frame carries and for what a
  `failure` in one means. This document places those documents in an envelope
  and changes nothing about them; a member of one, and the spelling of every
  scalar in it, is that document's and is never restated here.
- **[`ir/SPEC.md`](../ir/SPEC.md)**, *The resolved IR* — normative for the
  descriptor a `generate` frame carries, and for *[Writing a
  file](../ir/SPEC.md#writing-a-file)*, which leaves emitting a writer to the
  generator. That latitude is the whole reason an adapter has to declare a
  capability rather than simply be asked.
- **[`plugin/SPEC.md`](../plugin/SPEC.md)**, *The generator plugin CLI contract*
  — normative for the encoding a generator is handed its descriptor in, which is
  why the descriptor travels through this contract in that same encoding rather
  than in one an adapter would have to convert.
- **RFC 8259**, *The JavaScript Object Notation (JSON) Data Interchange Format*
  — normative for every frame. It is what fixes that a frame is one JSON text,
  that its member names are strings, and — in section 7 — that a string holds no
  unescaped control character, which is what makes a line terminator a safe
  frame delimiter. <https://www.rfc-editor.org/rfc/rfc8259>
- **RFC 4648**, *The Base16, Base32, and Base64 Data Encodings* — normative for
  the two members that carry bytes, and specifically for section 4's alphabet as
  against section 5's. It is the same choice
  [`conformance/SPEC.md`](../conformance/SPEC.md) makes for a byte-valued
  scalar, made the same way for the same reason.
  <https://www.rfc-editor.org/rfc/rfc4648>
- **IEEE Std 1003.1-2017**, *POSIX.1-2017 Base Specifications* — cited for two
  things this document names rather than invents: the `dup` and `dup2` calls a
  process redirects its own standard output with, and the exit status a parent
  reads from a child. It is cited as a recipe and not as a requirement — the
  requirement below is a property of the stream, which a platform without those
  calls meets by whatever means its runtime offers.
  <https://pubs.opengroup.org/onlinepubs/9699919799/>

> **Ambiguity:** the sources do not overlap and this document resolves nothing
> between them. Where it appears to contradict
> [`conformance/SPEC.md`](../conformance/SPEC.md) about what a values document
> holds, that document wins and this one has a bug: this one specifies the
> envelope and never the answer inside it, so the only way the two can collide
> is an envelope that made a legal answer uncarriable, which is a defect here by
> definition.

### Conformance language

**MUST**, **MUST NOT**, **SHOULD** and **MAY** are normative requirements on an
adapter and on an engine that drives one, interpreted as described in
[CONVENTIONS.md](../CONVENTIONS.md). Everything else is descriptive.

## A process is the unit, and a container is a door onto it

An adapter is a process. The engine starts it, holds its standard input and
standard output open for the whole run, and speaks the conversation below over
them. Nothing else is required of it: not a port, not a filesystem layout, not
an image, not a runtime, not a language.

That is a deliberately low bar, and it is low so that the first adapter somebody
writes can be a shell script that pipes lines through a program they already
have. A contract whose smallest conforming implementation needs a Dockerfile and
a registry is a contract most people evaluate by reading about it.

An engine will usually offer a door that simply runs a command, and may offer
others — a container image being the one this project blesses (#203). Which
doors an engine offers is the engine's and not this contract's, which is why
that sentence carries no keyword; what each door adds, though, is reportable and
does. What each door adds is the door's: the
image door is where a run with no network, a read-only root and a wall-clock cap
lives, and those properties are what make a result worth handing to somebody
else. None of them is a property of this contract, and an engine **MUST NOT**
report a result as though it carried a guarantee its door did not provide.

## Framing

### stdout carries frames and nothing else

An adapter's standard output carries frames and nothing else. Not a log line,
not a progress bar, not a compiler's warnings, not a runtime's panic trace, not
the notice a library prints the first time it is used, not a stray newline.

This is the practical hazard of the whole contract, and it is a hazard because
most of what threatens it is code the adapter's author did not write and cannot
easily audit. A single library that greets the world on standard output turns
every subsequent frame into a parse error, and the failure surfaces as an
adapter that appears to have answered gibberish rather than as one that printed
a greeting.

An adapter **MUST** ensure that nothing but frames reaches its standard output.
The recipe, and the reason this is stated as a property rather than as a
discipline:

1. At startup, before any other code in the process has had the chance to
   print, duplicate the standard output descriptor to a private one. `dup` is
   POSIX's name for that call.
2. Write every frame to the private descriptor, and to nothing else.
3. Point the standard output descriptor at the standard error descriptor.
   `dup2(2, 1)` is POSIX's name for that call.

After step 3, anything in the process that prints to standard output — however
deep in a dependency, however far from the adapter's own code — lands on
standard error, where it is a diagnostic instead of a corruption. An adapter
that instead resolves to be careful is an adapter that is one dependency away
from being wrong.

Standard error is free-form. An engine **MUST NOT** parse it and **SHOULD**
capture it, so that it can be quoted beside a fault; an adapter **MAY** write
whatever it likes there, and writing nothing is fine too.

An engine **MUST** treat a line on standard output that is not a well-formed
frame as an [adapter
fault](#refusal-is-an-answer-a-fault-is-not-and-an-exit-code-is-neither), and
**SHOULD** quote the offending line, because that line is usually the diagnostic
that explains it.

### A frame is one line of UTF-8 JSON

A frame is one JSON object, encoded in UTF-8, written on a single line and
terminated by a line feed (`%x0A`).

- A frame **MUST NOT** contain an unescaped line feed. RFC 8259 already forbids
  a raw control character inside a string, so this amounts to the requirement
  that a frame is not pretty-printed.
- A carriage return is not a terminator. Neither side terminates a frame with
  `%x0D %x0A`, and a receiver **MUST** refuse one that is rather than trimming
  it, because a stream that tolerates both is one where a text-mode file handle
  silently changes what was sent. The direction that matters most is the
  engine's: a text-mode handle writing to the adapter's standard input is how a
  carriage return gets into this conversation in the first place.
- A blank line is not a frame. Neither side sends one, and a receiver **MUST**
  treat one as a malformed frame rather than skipping it — a skipped blank line
  is a corrupted stream reported one frame later, at whichever frame first fails
  to parse.
- A receiver **MUST** accept a frame of any length. A values document for an
  entry with a large table is long, and a receiver that reads into a fixed
  buffer will meet it.
- Each side **MUST** flush after every frame it writes. The conversation is
  strictly alternating, so a side that buffers a frame is a side that deadlocks
  against a peer waiting for it, and the peer has no way out — an adapter
  blocked on a request the engine never flushed is forbidden to time out, to
  exit, and to answer. It is a **MUST** rather than a **SHOULD** because
  line-buffered output satisfies it in every language, so the stronger keyword
  costs nothing and the weaker one buys a hang.

### Request, response, and the identifier that pairs them

The engine sends **request** frames; the adapter answers each with exactly one
**response** frame. The conversation is strictly alternating: the engine
**MUST NOT** send a request before the previous one's response has arrived, and
an adapter **MUST NOT** write a frame it was not asked for.

Every request carries:

| Member | Type | Meaning |
|---|---|---|
| `id` | integer | Strictly increasing over the conversation, starting at 1. |
| `op` | string | The operation, one of the [set below](#the-operations). |

Every response carries:

| Member | Type | Meaning |
|---|---|---|
| `id` | integer | The `id` of the request it answers. |
| `ok` | boolean | Whether the adapter served the request at all. |
| `error` | string | Why not. Present exactly when `ok` is `false`. |

An adapter **MUST** echo the request's `id`, and an engine **MUST** refuse a
response whose `id` is not the one it is waiting for. Under a strictly
alternating conversation the identifier is redundant, which is exactly why it is
cheap and worth carrying: it is the only thing that turns a stream that has
silently desynchronised — one extra frame, one frame swallowed — into an error
at the frame where it happened rather than a wrong answer several entries later.

**Refusing a frame** means one thing throughout this document, whether the frame
was refused for a mismatched `id`, a trailing carriage return, a blank line or
anything else that is not well formed: the receiver stops the conversation and
treats the peer as [broken](#refusal-is-an-answer-a-fault-is-not-and-an-exit-code-is-neither),
rather than as having answered or faulted. It **MUST NOT** resynchronise by
skipping the frame and reading on. The reason is [the deadline
section](#deadlines-and-lifetime-belong-to-the-engine)'s: a stream whose framing
is in doubt cannot be resynchronised by anything the receiver can see, so an
engine that skipped and an engine that killed would report different things
about the same adapter.

`error` is a diagnostic for whoever reads the report. An engine **MUST NOT**
compare it or match against it, for the reason
[`conformance/SPEC.md`](../conformance/SPEC.md#a-file-the-reader-refused) gives
for not comparing a `failure`: a diagnostic is written in its author's own
language, and a contract that expected particular words is one only its first
implementation satisfies.

### An unknown member is ignored, and an unknown operation is refused

A receiver **MUST** ignore a member of a request or response frame that it does
not recognise, and **MUST NOT** refuse the frame for carrying one.

This is the opposite of the rule
[`conformance/SPEC.md`](../conformance/SPEC.md#an-entry) applies to an entry and
to an answer document, and the difference is who writes the document. An entry
is written by a person, where an unrecognised member is a typo and refusing it
is a kindness. A frame is written by a program against a version it announced at
the handshake, where an unrecognised member is a member added by a later version
of this contract — and a receiver that refused it would make every future
addition a breaking change, which is how a protocol acquires a version number it
never gets to increment.

The values documents *inside* a frame keep their own rule. `decoded` and
`written` are documents of the corpus format, and an unknown member in one is
refused exactly as that document requires: the relaxation here is the envelope's
and stops at the envelope.

An unknown `op`, by contrast, **MUST** be refused with `ok: false`, and an
adapter **MUST NOT** answer one with `ok: true`. An unrecognised member is an
extension a receiver can safely do nothing about; an unrecognised operation is
work it cannot do, and an adapter that reported success for work it did not do
is an adapter whose whole report is worthless.

## The operations

Six, and no others.

| `op` | Sent | Requires |
|---|---|---|
| [`hello`](#hello) | Exactly once, first. | — |
| [`generate`](#generate) | Exactly once, after `hello`; never when `kind` is `descriptive`. | `kind` is `codec` |
| [`decode`](#decode) | Zero or more times, after `generate`. | a `generate` or `rebuild` that succeeded for the entry |
| [`roundtrip`](#roundtrip) | After a `decode` of the same entry. | the `write` capability, and records held from that `decode` |
| [`rebuild`](#rebuild) | Zero or more times, after `generate`. | the `rebuild` capability |
| [`bye`](#bye-and-end-of-input) | At most once, last. | — |

An engine **MUST NOT** send a request whose precondition above is not met.

The adapter's half of that is deliberately smaller. An adapter **MUST** refuse
with `ok: false` a request that asks for a capability it did not declare, or a
[`roundtrip`](#roundtrip) for which it is holding no records; both are facts
about itself that it knows without keeping any other record. It **MAY** refuse
the ordering preconditions too, and **MAY** simply attempt the work instead.

Requiring more would oblige every adapter to keep a second copy of the engine's
state machine — which entry generated, which was decoded last — in branches
unreachable against a correct engine and therefore never exercised by anything.
That is code a third party writes, cannot test, and gets subtly wrong, and it is
a poor trade against the promise that the first adapter somebody writes can be a
shell script.

### `hello`

The handshake, and the first frame of every conversation.

```json
{"id": 1, "op": "hello", "protocol": 1}
```

`protocol` in the request is the version of this contract the engine speaks. It
is `1`.

`protocol` in the response is the version the **adapter** speaks — stated, not
echoed. An adapter that speaks the requested version answers `ok: true` with the
same number; one that does not answers `ok: false` and states its own anyway, so
that a report can say which two versions failed to meet instead of only that the
handshake failed. An adapter **MUST NOT** guess at a version it does not know.

There is no range and no fallback: an adapter speaks one version, and either it
is the engine's or the run does not happen. That is deliberate while one version
exists, and it is the first thing to revisit if a second is ever wanted.

A fault at the handshake is the one fault that costs the whole run rather than
one entry: there is no conversation left to have, so the engine reports it and
stops, closing the adapter's standard input as [below](#bye-and-end-of-input).

```json
{"id": 1, "ok": true, "protocol": 1, "name": "cpybkc-gen-go adapter",
 "version": "0.4.0", "kind": "codec",
 "capabilities": {"write": true, "rebuild": true}}
```

| Member | Type | Required | Meaning |
|---|---|---|---|
| `protocol` | integer | yes | The version the adapter speaks. **MUST** equal the request's when `ok` is `true`. |
| `name` | string | when `ok` is `true` | What the adapter is, for the report. |
| `version` | string | no | Which version of it, for the report. |
| `kind` | string | when `ok` is `true` | `codec` or `descriptive`. See [below](#the-adapter-declares-its-kind-and-its-capabilities). |
| `capabilities` | object | when `ok` is `true` | Which optional operations it serves. See [below](#the-adapter-declares-its-kind-and-its-capabilities). |

A refused handshake carries `id`, `ok`, `error` and `protocol`, and nothing
else. An adapter that could not agree the version has no basis for declaring a
`kind` or a capability set, and a frame that declared them anyway would be one a
report could quote.

`name` and `version` are quoted in reports and **MUST NOT** be compared or
parsed. An adapter that drives a particular generator at a particular version is
the useful thing to say in them, because that is what a reader of a result wants
to know and nothing else in the conversation carries it.

### `generate`

Every descriptor at once, exactly once, before any entry is read.

```json
{"id": 2, "op": "generate", "entries": [
  {"entry": "packed-ebcdic", "descriptor": "…"},
  {"entry": "zoned-ebcdic", "descriptor": "…"}]}
```

`entry` is the entry's name, which
[`conformance/SPEC.md`](../conformance/SPEC.md#an-entry) makes its identity.
`descriptor` is the entry's `ir.json` in the binary encoding — the encoding
[`plugin/SPEC.md`](../plugin/SPEC.md) says a plugin is handed — base64 encoded
because a frame is JSON.

```json
{"id": 2, "ok": true, "entries": [
  {"entry": "packed-ebcdic", "ok": true},
  {"entry": "zoned-ebcdic", "ok": false,
   "error": "OCCURS DEPENDING ON is not supported"}]}
```

Each element of `entries` is a per-entry result:

| Member | Type | Required | Meaning |
|---|---|---|---|
| `entry` | string | yes | Which entry the result is about. |
| `ok` | boolean | yes | Whether the adapter has code to read that entry with. |
| `error` | string | when `ok` is `false` | Why not. |

A response whose top-level `ok` is `true` **MUST** carry exactly one result per
entry the request named, and **MUST NOT** carry a result for an entry it did not
name. Order does not matter; the pairing is by name.

A response whose top-level `ok` is `false` is a `generate` the adapter did not
serve at all — a malformed request, an argument it could not read. It carries
`error` and no `entries`, every entry is lost, and the engine **MUST NOT** send
`decode`, `roundtrip` or `rebuild` for any of them. The per-entry table binds
only the `ok: true` case; an adapter that could not serve the operation is not
asked to invent a result for each entry in it.

**Why all of them at once.** An adapter for a compiled language compiles what
its generator produced, and compiling is usually the most expensive thing in the
run by a wide margin. Handed the corpus one entry at a time it pays that cost
once per entry; handed all of them at once it can emit every entry's code and
build the lot in one invocation of its toolchain. There is nothing to be gained
by splitting the operation up, since the engine has every descriptor in hand
before it starts, and a contract that offered both would be a contract whose
fast path nobody found.

**A failure here is per entry, and the run continues.** An entry whose
descriptor the generator would not accept, or whose generated code would not
compile, comes back `ok: false` with a diagnostic, and the adapter stays alive
and serves the remaining entries. That entry is reported as an adapter fault
(#199), never as a mismatch and never as a refusal; the engine **MUST NOT** send
`decode` or `roundtrip` for it unless a later [`rebuild`](#rebuild) has
succeeded for it. That exception is most of what `rebuild` is for: the entry
somebody most wants to rebuild is the one that just failed to compile.

An adapter that cannot go on at all — its toolchain is absent, its own working
directory is unwritable — exits non-zero instead. The distinction is the whole
of the [exit-code rule](#exit-codes) applied here: one entry's worth of failure
costs one entry, and only a broken adapter costs the run.

### `decode`

One entry, read top to bottom.

```json
{"id": 3, "op": "decode", "entry": "packed-ebcdic", "input": "…"}
```

`input` is the entry's `input.bin`, base64 encoded.

```json
{"id": 3, "ok": true, "entry": "packed-ebcdic",
 "decoded": {"records": [{"REC": {"AMOUNT": "-123.45"}}]}}
```

`decoded` is a values document, in exactly the form
[`conformance/SPEC.md`](../conformance/SPEC.md#valuesjson) specifies, holding
what the generated reader made of those bytes. The adapter **MUST** echo
`entry`.

A file the generated reader refused is an answer and not a fault: `ok` stays
`true` and `decoded` carries a `failure` beside the records read before the read
stopped, exactly as [*A file the reader
refused*](../conformance/SPEC.md#a-file-the-reader-refused) requires. A large
fraction of the corpus's entries expect precisely this, and an adapter that
reported one of them as a fault would fail those entries by being right about
them. (How many
there are is the corpus's to say, in
[`testdata/conformance/README.md`](../../testdata/conformance/README.md), where
it can be kept true; a count written here would go stale the first time an entry
was added, in a document that says [entries may be
added](#which-entries-exist) without touching it.)

**The bytes travel in the frame, and not as a path.** An adapter is not given
the corpus directory to open, because the door that produces a believable result
runs it with no filesystem access to that directory at all (#203) — and a
contract whose adapter read files would be a different contract behind each
door. It is also half of [*The adapter is never given the expected
values*](#the-adapter-is-never-given-the-expected-values): an adapter that could
open `input.bin` could open `values.json` beside it.

### `roundtrip`

The writing direction, over the records the reader just produced.

```json
{"id": 4, "op": "roundtrip", "entry": "packed-ebcdic"}
```

The request carries no records, and that is the point. The adapter writes the
records its own generated reader produced in the immediately preceding `decode`
of this entry, lays them out with the generated writer, reads that file back
with the generated reader, and answers with what came back:

```json
{"id": 4, "ok": true, "entry": "packed-ebcdic",
 "written": {"records": [{"REC": {"AMOUNT": "-123.45"}}]}}
```

`written` is a values document, and both it and `decoded` are compared against
the entry rather than against each other, for the reason
[`conformance/SPEC.md`](../conformance/SPEC.md#the-answer-document) gives: a
reader and a writer that are wrong the same way agree with each other, and only
the entry knows what the file holds. Why the direction is checked by reading
rather than by comparing bytes is that document's too, in [*Why the writing
direction is checked by
reading*](../conformance/SPEC.md#why-the-writing-direction-is-checked-by-reading-and-not-by-comparing-bytes),
and this contract adds nothing to it.

An adapter **MUST** retain the records from the most recent `decode` it answered
`ok: true` — whether or not that document carried a `failure` — until the next
`decode`, a `rebuild` of that entry, or the end of the conversation. *Answered
`ok: true`* is the wire sense of success this document uses everywhere else, and
it is the one meant: a refused read still produced the records it managed, and
they are still the last thing the reader made.

An adapter **MUST NOT** be asked to retain more than that. The engine sends
`roundtrip` immediately after the `decode` it belongs to, so one entry's records
is the whole memory this contract requires of an adapter.

**Why the engine cannot supply the records.** A values document is not a record.
It is a rendering of what a record decoded to, and
[`conformance/SPEC.md`](../conformance/SPEC.md#slack-is-not-a-value) has slack
bytes travelling with a record while never appearing as a value — so records
rebuilt from a values document would be missing exactly the bytes the writing
direction is being asked about. The records have to be the reader's own, which
means they have to stay inside the adapter, which is why this operation names an
entry and carries nothing else.

Preconditions: the adapter declared the `write` capability; the adapter is
holding records from a `decode` of the named entry; and that decode did not
carry a `failure` — a read that stopped holds no complete set of records to
write back.

The middle one is stated as what the adapter is *holding* rather than as what
was most recently *decoded*, and the difference is not pedantry. A
[`rebuild`](#rebuild) discards the records without changing the decode history,
so under the other phrasing `decode X`, `rebuild X`, `roundtrip X` would satisfy
the precondition while leaving the adapter nothing to serve it with — a request
the contract obliged an engine to be allowed to send and an adapter to have no
answer for. Phrased this way the adapter can check it against itself, and a
`rebuild` falsifies it automatically.

A writer that refused a record it was given is an answer too: `ok` stays `true`
and `written` carries a `failure` with an empty `records`, as [*A file the
reader refused*](../conformance/SPEC.md#a-file-the-reader-refused) specifies for
that half of an answer.

### `rebuild`

One entry, regenerated inside a process that is already warm.

```json
{"id": 5, "op": "rebuild", "entry": "packed-ebcdic",
 "descriptor": "…"}
```

```json
{"id": 5, "ok": true, "entry": "packed-ebcdic"}
```

The members mean what they mean in [`generate`](#generate), and the result is
the same per-entry result. What differs is the cost: the adapter's toolchain,
its caches and anything else expensive it set up at `generate` are still there,
so regenerating one entry is the cost of one entry rather than of a corpus.

This exists for the loop somebody actually works in — change the generator,
re-run one entry, look — and for an engine that would otherwise pay a whole
`generate` to ask one entry again. It is optional because an adapter with
nothing to keep warm gains nothing by implementing it; an adapter that does not
declare the `rebuild` capability is asked for a fresh process instead, which is
slower and never wrong.

A `rebuild` **MAY** name an entry the preceding `generate` did not: an entry
that failed to generate and an entry the engine has only now decided to ask
about are the same request, and refusing the second would buy nothing.

A successful `rebuild` **MUST** replace whatever the previous `generate` or
`rebuild` produced for that entry, and **MUST** discard any records retained for
it. It is what makes an entry askable whose `generate` failed — the engine may
`decode` that entry once a `rebuild` for it has succeeded. A `roundtrip` for it
needs a fresh `decode` first, because the discarded records are exactly what
[`roundtrip`](#roundtrip)'s middle precondition asks for.

### `bye`, and end of input

```json
{"id": 6, "op": "bye"}
```

```json
{"id": 6, "ok": true}
```

After answering, the adapter closes its standard output and exits zero.

An engine **MAY** instead simply close the adapter's standard input, and an
adapter **MUST** treat end of input as a `bye` it has already answered: it exits
zero without writing anything further. Both exist because a run that ended in a
fault has no useful frame left to send, and an adapter that waited for a polite
goodbye that was never coming would hang at the end of every such run.

An adapter **MUST NOT** exit zero having neither answered `bye` nor seen end of
input. Exiting quietly in the middle of a conversation is the one failure that
looks, from the engine's side, exactly like a successful run that stopped early.

That prohibition is what obliges the other side: an engine that stops early —
a refused handshake, a broken adapter, a run it abandons — **MUST** close the
adapter's standard input, and **MAY** then kill the process. Without it the
adapter is left waiting on a conversation that is over and forbidden to leave
it, which is a hang neither side is at fault for.

## The adapter declares its kind and its capabilities

Both are declared at [`hello`](#hello), before the adapter has been asked
anything, and both exist because the alternative is discovering them from a
failure that looks like a wrong answer.

### `kind`, because not every generator is a conformance subject

`kind` is a closed set of two:

- **`codec`** — the generator emits code that reads a file into records, and
  usually writes records back into a file. The corpus is about these, and the
  operations above are for these.
- **`descriptive`** — the generator emits something else: a diagram, a schema, a
  document, a copybook. It never opens a data file, so there is nothing to hand
  `input.bin` to.

An engine **MUST** refuse a `kind` it does not recognise rather than falling
back to one it does, which is the rule [`ir/SPEC.md`](../ir/SPEC.md) applies to
every closed set it defines, for the same reason: a value added later means
something, and treating it as the nearest thing that already existed is how a
run reports a result about a thing it did not understand.

An adapter declaring `descriptive` is **not applicable**, not failing. The
engine **MUST NOT** send it `generate`, `decode`, `roundtrip` or `rebuild`; it
sends `bye`, reports the run as not applicable, and exits clean (#201). The two
shapes that must not happen are a descriptive generator scored zero out of the
whole corpus, and a descriptive generator declining every entry one at a time.
Neither is true, both read as failures, and the truthful answer is reachable in
one member of one frame.

What a descriptive generator *should* be held to instead is an open question and
is deliberately not answered here; see [Out of
Scope](#what-a-descriptive-adapter-is-measured-against).

### `capabilities`, because a read-only generator is a legal generator

`capabilities` is an object whose members are the optional operations the
adapter serves. A capability absent is a capability the adapter does not have,
and the member itself is required even when it is empty — an author who has to
write `{}` has been asked the question, where an author who may omit it has not.

| Capability | Meaning |
|---|---|
| `write` | The adapter's generator emits a writer, so [`roundtrip`](#roundtrip) can be served. |
| `rebuild` | [`rebuild`](#rebuild) can be served. |

Reading is not a capability. An adapter of kind `codec` that cannot read a file
is not a codec adapter, and giving it a member to say so would be giving it a
way to declare itself a subject it cannot be.

Writing is a capability because
[*Writing a file*](../ir/SPEC.md#writing-a-file) leaves emitting a writer to the
generator. A generator that emits a reader and no writer is conformant to that
document, and an engine that demanded the writing direction of every adapter
would fail every positive entry for such a generator — reporting, once per
entry, a missing answer to a question the specification never obliged it to
answer. So:
an engine **MUST NOT** report an entry as failing because `written` is absent
from an adapter that declared `write: false` (#199), and **MUST NOT** send it
`roundtrip` at all.

A run by a read-only adapter is a smaller claim than a run by a full one, and an
engine **SHOULD** say which it was in the report. It is not a lesser result; it
is a result about a smaller thing.

## Refusal is an answer, a fault is not, and an exit code is neither

Three outcomes, and most of the value of this contract is in keeping them apart.
An engine that conflates any two of them reports a wrong thing about a working
generator, or a working thing about a broken one.

| Outcome | On the wire | What it says |
|---|---|---|
| **An answer** | `ok: true`, and whatever the operation returns — for [`decode`](#decode) and [`roundtrip`](#roundtrip) a values document, which **MAY** carry a `failure` | The adapter served the request. Where the request put a question to the generated code, a refusal is one of the answers, and an entry is allowed to expect it. |
| **A fault** | `ok: false`, with `error` | The adapter could not serve this request. The entry is lost; the run is not. |
| **A broken adapter** | a non-zero exit, or a stream that stopped parsing | The adapter cannot go on. The run is over until a fresh process is started. |

A file the generated reader rejected is the first row and never the second or
the third. This is the row an implementation gets wrong: rejecting a malformed
file is what a correct reader *does*, a large fraction of the corpus's entries
are about exactly that, and an adapter that treats a rejection as an error
condition — exiting, or answering `ok: false` — turns the corpus's most
interesting entries into noise.

### Exit codes

An adapter **MUST** exit zero when it has answered
[`bye`](#bye-and-end-of-input) or seen end of input, and **MUST** exit non-zero
when it stopped for any other reason.

A non-zero exit means the adapter broke: the generator crashed, generated code
would not compile in a way that left nothing to go on, a frame could not be
written, a toolchain was missing. It **MUST NOT** mean that bytes decoded
wrongly, that a reader refused a file, that a writer refused a record, or that
an answer differed from anything. The adapter has nothing to differ from — it
is [never given the expected
values](#the-adapter-is-never-given-the-expected-values) — so an exit code that
carried a verdict would be an exit code reporting a comparison nobody made.

There is no table of exit codes and there will not be one. Beyond zero and
non-zero an exit status is not portable — a platform may choose the low bits, a
signal may set them, and a shell wrapper will happily overwrite them — so a
contract that assigned meanings to particular numbers would be a contract whose
meanings survive one platform. The diagnostic goes on standard error, where its
length is not eight bits.

An engine **MUST** report the operation in flight when an adapter exited as a
fault against that entry, never as a mismatch, and **MUST** quote the adapter's
standard error where it captured any.

## The adapter is never given the expected values

The engine **MUST NOT** send an entry's `values.json`, or any part of it, or any
statement derived from it, in any frame. An adapter **MUST NOT** read the corpus
directory, and is given the two things it needs — the descriptor and the input
bytes — inline for exactly that reason.

The comparison is the engine's, and it stays the engine's. An adapter holding
the expected answers is self-grading, and self-grading is not a weaker check
than an independent one, it is a different check: it measures whether the
adapter's author could reproduce a document they were handed. It is also
unfalsifiable from outside, since nothing in a passing result distinguishes an
adapter that decoded correctly from one that echoed what it was told.

This is why [`decode`](#decode) carries `input` rather than a path, why an
adapter is never told an entry's description or the specification section it was
derived from, and why the engine reports a mismatch by naming the field and its
position out of the descriptor it holds (#199) rather than by asking the adapter
to explain itself. An adapter that argued its own case would be an adapter whose
report had to be trusted.

## Deadlines and lifetime belong to the engine

An engine **MUST** enforce a deadline on each operation it sends. An adapter
**MUST NOT** impose one of its own — it **MUST NOT** exit, and **MUST NOT**
answer `ok: false`, because it decided an operation was taking too long. An
adapter that gave up on a slow entry would turn one slow entry into a broken
adapter and cost the run everything after it, where the engine's own deadline
costs that entry and nothing else (#199).

When a deadline expires the engine **MUST** stop the conversation rather than
continue it: it kills the process, reports that operation as a fault against its
entry, and **MAY** start a fresh adapter to carry on with the remaining entries.
Continuing is not an option even though the response could be paired by `id`,
because an adapter killed mid-frame may have written half a line, and a stream
whose next byte is the middle of an abandoned frame cannot be resynchronised by
anything the receiver can see.

An adapter killed by the engine is not in violation of this contract, and it is
under no obligation to catch a signal, unwind, or write anything on its way out.

## Out of Scope

### How the adapter process is started

The argument vector, the working directory, the environment, the image
reference, and whether the process is local at all are **not specified here**.

Reason: they are the door's, not the contract's (#203). One engine offers a
command to run and another spawns a container, and both drive this contract with
one implementation behind them precisely because the contract begins after the
process exists. Specifying the launch would make each door a dialect.

### What makes a result believable

Network isolation, a read-only root filesystem, memory and process caps, and a
wall-clock bound on the whole run are **not specified here**.

Reason: those are properties of the door too, and stating them here would imply
that every conforming run carried them (#203). It does not: a result produced by
running an adapter directly is the author's own working result, and a result
produced through the blessed image door is one they can hand to somebody else.
The difference is worth reporting and is the engine's to report.

### What a value means, and how it is spelled

The value language, the admissible spelling of every scalar, what a `failure`
means, and how two values documents are compared are **not specified here**.

Reason: they are
[`conformance/SPEC.md`](../conformance/SPEC.md)'s, and this document carries its
documents unmodified. A second description of a spelling rule is a second thing
for an adapter author to satisfy, and the day the two disagree is the day every
adapter is wrong against one of them.

### Which entries exist

The corpus's entries, what each covers and how one is added are **not specified
here**.

Reason: they are the corpus's own, in
[`testdata/conformance/README.md`](../../testdata/conformance/README.md). This
contract names an entry by its name and knows nothing else about it, which is
what lets the corpus grow an entry without touching a published interface.

### How a generator is invoked

The argument vector a `cpybkc-gen-<name>` executable is run with, the `PATH`
search, the option syntax and the plugin's own exit codes are **not specified
here**.

Reason: they are the [plugin contract](../plugin/SPEC.md)'s. An adapter invokes
its generator through that contract like any other caller — or does not invoke
one at all, if it generates in process — and this contract deliberately cannot
tell the difference.

### What a descriptive adapter is measured against

An adapter that declares [`kind:
"descriptive"`](#kind-because-not-every-generator-is-a-conformance-subject) is
reported as not applicable, and what it *should* be held to instead is **not
specified here**.

Reason: whether a descriptive track is worth building, and what its oracle would
be, are open questions in discussion #193. What this document settles is the
cheap half — that such a generator can say what it is in one member, in the
first frame, and be told the truth about itself. Specifying an oracle before
deciding whether to have one would specify it twice.

### Also out of scope

- **Any transport but the process's own standard streams.** No socket, no HTTP,
  no shared file. A process with two pipes is the smallest thing every language
  already has.
- **Concurrency within one conversation.** One request at a time, in order. An
  engine that wants parallelism runs more adapters, which needs nothing from
  this document.
- **What the engine prints.** The report, its format and its exit code are the
  engine's (#199), and this contract is what a report is made from rather than
  what it looks like.
- **Resuming a broken adapter.** There is no reconnect, no replay and no
  checkpoint. A fresh process starts at [`hello`](#hello).
- **A conformance level, a score or a badge.** Restated from
  [`conformance/SPEC.md`](../conformance/SPEC.md): an entry either compares
  equal or it does not, and a run computed by a claimant on their own machine is
  a self-report however it is printed (#203).

## Appendix: A worked conversation

One codec adapter, two entries, one of which the corpus expects the reader to
refuse. `→` is the engine writing to the adapter's standard input; `←` is the
adapter writing to its standard output. Base64 payloads are elided; nothing else
is.

```
→ {"id":1,"op":"hello","protocol":1}
← {"id":1,"ok":true,"protocol":1,"name":"gen-rust adapter","version":"0.2.1",
   "kind":"codec","capabilities":{"write":true}}

→ {"id":2,"op":"generate","entries":[
   {"entry":"packed-ebcdic","descriptor":"…"},
   {"entry":"packed-invalid-sign","descriptor":"…"}]}
← {"id":2,"ok":true,"entries":[
   {"entry":"packed-ebcdic","ok":true},
   {"entry":"packed-invalid-sign","ok":true}]}

→ {"id":3,"op":"decode","entry":"packed-ebcdic","input":"…"}
← {"id":3,"ok":true,"entry":"packed-ebcdic",
   "decoded":{"records":[{"REC":{"AMOUNT":"-123.45"}}]}}

→ {"id":4,"op":"roundtrip","entry":"packed-ebcdic"}
← {"id":4,"ok":true,"entry":"packed-ebcdic",
   "written":{"records":[{"REC":{"AMOUNT":"-123.45"}}]}}

→ {"id":5,"op":"decode","entry":"packed-invalid-sign","input":"…"}
← {"id":5,"ok":true,"entry":"packed-invalid-sign",
   "decoded":{"records":[],"failure":"sign nibble 0x7 is not one of the four"}}

→ {"id":6,"op":"bye"}
← {"id":6,"ok":true}
```

Three things that conversation shows and prose says less clearly. The refused
file at `id: 5` came back `ok: true`, and it is the entry's business whether
that was the expected outcome — the adapter does not know and was not asked. No
`roundtrip` followed it, because a read that stopped holds no complete set of
records to write back. And the frames are wrapped here for the page: on the wire
each is one line.

The same conversation with a descriptive adapter is four frames long:

```
→ {"id":1,"op":"hello","protocol":1}
← {"id":1,"ok":true,"protocol":1,"name":"gen-graph adapter",
   "kind":"descriptive","capabilities":{}}

→ {"id":2,"op":"bye"}
← {"id":2,"ok":true}
```

## Appendix: Mapping to Stories

| Section | Implemented by |
|---|---|
| [A process is the unit, and a container is a door onto it](#a-process-is-the-unit-and-a-container-is-a-door-onto-it) | #198 decides it; #203 builds the image door and the direct one behind one implementation |
| [stdout carries frames and nothing else](#stdout-carries-frames-and-nothing-else) | #198; #200 for the adapter that has to do it |
| [A frame is one line of UTF-8 JSON](#a-frame-is-one-line-of-utf-8-json) | #198; #199 for the engine that reads and writes them |
| [Request, response, and the identifier that pairs them](#request-response-and-the-identifier-that-pairs-them) | #199 `conformance` |
| [An unknown member is ignored, and an unknown operation is refused](#an-unknown-member-is-ignored-and-an-unknown-operation-is-refused) | #199 `conformance`; the opposite rule it contrasts with, #66 and #196 |
| [`hello`](#hello) | #199 `conformance` for the engine's half; #200 for the Go adapter's; #201 for `kind` |
| [`generate`](#generate) | #199 and #200 `conformance` |
| [`decode`](#decode) | #199 and #200 `conformance`; the values document it carries, #66 and #194 |
| [`roundtrip`](#roundtrip) | #199 and #200 `conformance`; the argument it rests on, #68, and *Writing a file*, #17 `ir` |
| [`rebuild`](#rebuild) | #199 and #200 `conformance` |
| [`bye`, and end of input](#bye-and-end-of-input) | #199 and #200 `conformance` |
| [`kind`, because not every generator is a conformance subject](#kind-because-not-every-generator-is-a-conformance-subject) | #201 `conformance`; the discussion it comes from is #193, and no story specifies a descriptive oracle |
| [`capabilities`, because a read-only generator is a legal generator](#capabilities-because-a-read-only-generator-is-a-legal-generator) | #199 `conformance` for the engine that must not fail a read-only adapter; the latitude it serves, #17 `ir` |
| [Refusal is an answer, a fault is not, and an exit code is neither](#refusal-is-an-answer-a-fault-is-not-and-an-exit-code-is-neither) | #199 `conformance`; the reading it inherits, #66 and #68 |
| [Exit codes](#exit-codes) | #199 and #200 `conformance` |
| [The adapter is never given the expected values](#the-adapter-is-never-given-the-expected-values) | #199 `conformance`, which keeps the comparison |
| [Deadlines and lifetime belong to the engine](#deadlines-and-lifetime-belong-to-the-engine) | #199 `conformance` |
| The corpus format this contract carries | #66, #68, #194-#197 `conformance` |
| This document | #198 `conformance` |
| Conventions this document follows | #15 `setup` |
