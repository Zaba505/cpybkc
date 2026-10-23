# The receivables example

An accounts-receivable transaction extract carried from a layout to bytes, with
**no copybook anywhere in the project**. The copybook belongs to a vendor, is
replaced on every release, and is the kind of file a licence keeps out of a
repository. What is checked in instead is what `cpybkc init` wrote from it.

This is one of the examples under [`example/`](..), and a cpybkc project of its
own: [`cpybkc.json`](cpybkc.json) here names one layout and the generators run
over it. [Why the manifest is per example](../README.md#one-manifest-per-example)
is the index's to say.

| What a caller writes | What cpybkc writes |
|---|---|
| [`cpybkc.json`](cpybkc.json) — the project manifest | [`receivables/`](receivables) — the generated Go package |
| [`receivables.sexpr`](receivables.sexpr) — the layout | [`graph/graph.md`](graph/graph.md) — the generated diagram of the automaton |
| [`ARTRANS.sexpr`](ARTRANS.sexpr) — the definitions file the layout's records name | [`cpybkc.gen.json`](cpybkc.gen.json) — the record of what was generated |

The left-hand column is "what a caller writes" in the sense every example uses
it, and here the caller wrote less of it than anywhere else. `ARTRANS.sexpr` is
`init`'s transcription of the copybook, byte for byte, and nobody edits it. The
layout is `init`'s scaffold with its questions answered. The rest is generated.

## Where the files came from

The vendor's copybook is
[`testdata/vendor/ARTRANS`](../../testdata/vendor/ARTRANS). It lives outside
`example/` because it stands for a file the project does not have: an
adopter's copybook is on a mainframe, or in a vendor's drop, and never in the
tree they commit. It is written the way a vendor ships one — a member name and no
extension, sequence numbers in columns 1–6, a release stamp in columns 73–80, a
banner comment saying not to modify it — and it carries the clauses a vendor
copybook carries: condition-names (`88`), `VALUE`, `SIGN LEADING SEPARATE` and
`SIGN TRAILING SEPARATE`, `SYNC` on two binary items, a `77`-level release
constant, and a `REDEFINES` with three descriptions over one run.

Standing in this directory, one command wrote the first draft of everything a
caller writes here except the manifest:

```sh
cpybkc init --copybook ../../testdata/vendor/ARTRANS --out receivables.sexpr
```

```
note: ../../testdata/vendor/ARTRANS: ART-DETAIL-REC carries 3 REDEFINES outside a repeating group, which resolve to 4 record types
```

It wrote two files. [`ARTRANS.sexpr`](ARTRANS.sexpr), named after the copybook's
final path element, holds every entry and clause of the copybook in the layout
format's [entry notation](../../docs/layout/SPEC.md#definitions): the `77`, all
three `01`-levels, every `88` and every `VALUE`, the two `SIGN … SEPARATE`
clauses and the two `(synchronized)` ones. It holds none of the vendor's
comments, and one of its own, naming the copybook as it was typed. The layout is
named `receivables.sexpr` rather than `ARTRANS.sexpr` because `init` refuses a
layout whose name is one of the definitions files it is about to write.

The other file, [`receivables.sexpr`](receivables.sexpr), was a scaffold. Its six
`record` forms were complete, and the five kept here are unchanged. Everything
else in it was a commented question:

- **the encoding** — `cp037`, `ebcdic`, `big-endian` and `ieee-754`, because an
  IBM mainframe wrote the file;
- **the framing** — `FB` at eighty bytes, which is what every `01`-level in the
  copybook works out to;
- **three renames** — the three transaction records share `ART-DETAIL-REC`'s name
  until something says otherwise, and `cpybkc-gen-go` refuses the collision
  rather than inventing names;
- **a discriminator per record** — byte one, `H`, `I`, `P`, `A` or `T`;
- **the sequence** — a header, then invoices, payments and adjustments in any
  order, then a trailer.

One of the six records is gone. `ART-DETAIL-REC` has four descriptions of its
detail area: `ARD-BODY`, which is forty bytes of text, and the three `REDEFINES`
of it. `init` wrote a record for each, because a copybook cannot say which ones
a file holds. `ARD-BODY` is how the vendor declares the storage the three
share, and no record in the extract is one. So the layout leaves that record
out. Nothing in `ARTRANS.sexpr` changes to do that: the definitions are the
copybook's, and narrowing to what the data holds is the layout's job.

## One definitions file, five records

Every record in the layout names `ARTRANS.sexpr`. The header and the trailer each
name their own `01`-level. The three transaction records all name
`ART-DETAIL-REC`, and each picks one alternative:

```
(record ART-DETAIL-REC-ARD-INVOICE
  (definitions "ARTRANS.sexpr" ART-DETAIL-REC)
  (alternative (item ART-DETAIL-REC-ARD-INVOICE ARD-INVOICE)))
```

A run reads the file once, however many records name it. The record names are
`init`'s: long, mechanical, and kept because each one says which alternative it
selects. The renames are what the generated code calls them —
`ArtInvoiceRec`, `ArtPaymentRec` and `ArtAdjustmentRec` in
[`receivables/records.go`](receivables/records.go).

The payment's description is twenty-four bytes over a forty-byte run. The other
sixteen bytes are slack: [`graph/graph.md`](graph/graph.md) shows them at offset
51, and a payment read and written back carries them out unchanged. The two `SYNC`
items leave slack of their own: one byte ahead of `ARH-BATCH-NO` and two ahead of
`ATR-HASH`, at offsets 13 and 22. The diagram is the place to check both before
trusting a real dataset.

## Moving a project off its copybook

A project that already names the copybook moves to the definitions file without
anything a generator is handed changing. This is docs/cli/SPEC.md's [copybook to
file](../../docs/cli/SPEC.md#when-a-copybook-changes-and-moving-a-record-between-sources)
move. Had this example started out naming the copybook, each record would have
read

```
(record ART-HEADER-REC (copybook "../../testdata/vendor/ARTRANS" ART-HEADER-REC))
```

and the move would be:

```sh
cpybkc --manifest cpybkc.json --emit-ir /tmp/before.binpb
cpybkc init --copybook ../../testdata/vendor/ARTRANS --out /tmp/next/receivables.sexpr
mv /tmp/next/ARTRANS.sexpr .
# in each record naming the copybook:
#   (copybook "../../testdata/vendor/ARTRANS" N)  ->  (definitions "ARTRANS.sexpr" N)
cpybkc --manifest cpybkc.json --emit-ir /tmp/after.binpb
cmp /tmp/before.binpb /tmp/after.binpb && echo identical
```

```
identical
```

`cmp` prints nothing and exits zero, because a record bound to a copybook and the
same record bound to the file `init` converts it into [resolve to byte-identical
descriptors](../../docs/layout/SPEC.md#three-sources-one-descriptor). A difference
would be a mistake in the move, not in the copybook. Nothing else in the layout
changes: discriminators, renames and the sequence are rooted at record names,
never at the file a record's entries are held in.

## When the vendor ships a new copybook

The definitions file is checked in so that a run never needs the copybook. When a
new release arrives, run the same `init` into a scratch directory and diff the
definitions file:

```sh
cpybkc init --copybook ../../testdata/vendor/ARTRANS --out /tmp/next/receivables.sexpr
diff -u ARTRANS.sexpr /tmp/next/ARTRANS.sexpr
```

Against the copybook in this repository the diff is empty. `init` writes the same
bytes on every run of one build, and a test in
[`../regenerate_test.go`](../regenerate_test.go) runs exactly this command and
holds its output to `ARTRANS.sexpr`.

Type the copybook's path the way you typed it the first time. The header comment
names it as it was typed, so a different spelling of the same file shows up as a
one-line change to the header and nothing else.

Here is what a release 3.2 that splits the adjustment note in two looks like:

```diff
--- ARTRANS.sexpr
+++ /tmp/next/ARTRANS.sexpr
@@ -1,5 +1,5 @@
 ; The entries of the copybook "../../testdata/vendor/ARTRANS", transcribed by cpybkc init.
-(entry 77 ART-COPYBOOK-RELEASE (pic "X(4)") (value "'R031'"))
+(entry 77 ART-COPYBOOK-RELEASE (pic "X(4)") (value "'R032'"))
 (entry 01 ART-HEADER-REC)
   (entry 05 ARH-REC-TYPE (pic "X(1)"))
     (entry 88 ARH-IS-HEADER (value "'H'"))
@@ -37,7 +37,8 @@
     (entry 10 ARA-REASON (pic "X(3)"))
     (entry 10 ARA-AMOUNT (pic "S9(9)V99") (sign TRAILING SEPARATE))
     (entry 10 ARA-APPROVER (pic "X(8)"))
-    (entry 10 ARA-NOTE (pic "X(17)"))
+    (entry 10 ARA-CASE-NO (pic "X(8)"))
+    (entry 10 ARA-NOTE (pic "X(9)"))
   (entry 05 ARD-OPERATOR (pic "X(6)"))
   (entry 05 FILLER (pic "X(7)") (value SPACES))
 (entry 01 ART-TRAILER-REC)
```

That is the vendor's change, entry by entry, in the notation the project already
reads. Whatever the vendor did to its banner comments, sequence numbers and
release stamps does not show here, because a definitions file carries none of
them. Copying the new
file over the old one is your act, not cpybkc's. After that, the next run reports
at its span every item reference in the layout that the new entries no longer
satisfy. `/tmp/next/receivables.sexpr` is there to be read, not moved: if the
release added a record, its commented questions say what that record needs.

## Moving the definitions into the layout, and back

A definitions file can also be held in the layout itself, as a
[`definitions-block`](../../docs/layout/SPEC.md#the-definitions-block-form). Then
the layout is the only file beside the manifest. Each move is checked the same
way as above, by `--emit-ir` before and after and a `cmp`.

**File to block.** Take a reading first:

```sh
cpybkc --manifest cpybkc.json --emit-ir /tmp/before.binpb
```

Then add the file's entries to the layout as a block: each line two spaces
further in, the block's closing parenthesis on its last entry's line, and the
header comment left behind, since a comment is nothing a run reads:

```
(definitions-block ARTRANS
  (entry 77 ART-COPYBOOK-RELEASE (pic "X(4)") (value "'R031'"))
  (entry 01 ART-HEADER-REC)
    (entry 05 ARH-REC-TYPE (pic "X(1)"))
      (entry 88 ARH-IS-HEADER (value "'H'"))
  …
    (entry 05 ATR-HASH (pic "S9(9)") (usage COMP) (synchronized))
    (entry 05 FILLER (pic "X(52)") (value SPACES)))
```

In each of the five records, change `"ARTRANS.sexpr"` to `ARTRANS`:

```
(record ART-HEADER-REC (definitions ARTRANS ART-HEADER-REC))
```

Delete `ARTRANS.sexpr`, since no other layout names it, and take a second
reading:

```sh
rm ARTRANS.sexpr
cpybkc --manifest cpybkc.json --emit-ir /tmp/after.binpb
cmp /tmp/before.binpb /tmp/after.binpb && echo identical
```

```
identical
```

The block's name, `ARTRANS`, belongs to the layout and is not a COBOL name, so
it can be anything that does not collide with another block. A record names it
unquoted, where it named a file with a quoted path.

**Block to file.** The same edit, run backwards. Write the block's body, two
spaces dedented, to `ARTRANS.sexpr`. Remove the block. In each record, change
`ARTRANS` back to `"ARTRANS.sexpr"`. The reading after it is identical to the
reading before it. The layout is the one checked in here, byte for byte, and the
file is this one less its header comment, which `init` writes and no move needs.

No inline variant of this example is committed. `init` never writes a block, so
a committed one would be maintained by hand and would test nothing that is not
already tested. `internal/project`'s equality tests move every example's
descriptions into blocks, this one's file included, and require the same
descriptor. [`../regenerate_test.go`](../regenerate_test.go) carries out the
moves above as they are written here.

## Regenerating

From the repository root, as [the index](../README.md#regenerating) gives it for
every example:

```sh
go build -o "$(go env GOPATH)/bin/cpybkc-gen-go" ./cmd/cpybkc-gen-go
go build -o "$(go env GOPATH)/bin/cpybkc-gen-graph" ./cmd/cpybkc-gen-graph
go run ./cmd/cpybkc --manifest example/receivables/cpybkc.json
```

No copybook is read. The run opens the manifest, the layout and `ARTRANS.sexpr`,
and nothing else. `go test ./example/...` is what fails if the generated tree is
not what those three files produce, or if `ARTRANS.sexpr` is no longer what
`init` writes from the vendor's copybook.
