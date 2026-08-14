# The grammar corpus

A table of *a value, and the exact document text that value is written as*, for
every construct the conformance corpus's value language has. It is here so that
somebody writing a values-document writer — in Rust, in Java, in Python, in
whatever language their generator emits — can check that writer against
something before they run a single corpus entry (#197).

[SPEC.md](SPEC.md)'s *The value language* is what decides these answers, and
nothing here changes or adds to it. Every row is a worked example of a rule
stated there, and each table below names the section it comes from. Where this
document and that one appear to disagree, that one is right and this one has a
bug.

## Why a table, and why it is not a library

A generator author has to write a values-document writer before the corpus can
tell them anything about their COBOL. Today the only way to find out whether
that writer is right is to run the whole corpus and read the failures — so a
JSON formatting mistake presents as a COBOL decoding mistake, which is the most
expensive possible way to learn it.

The alternative to a table was a helper library per language, and it was
rejected. Two hundred lines of value-language helper in three languages this
repository cannot all review is three release cadences; the predictable failure
is that the language gains a rule, one of the three lags, and a generator's
conformance result moves for a formatting reason with no COBOL content in it —
the worst thing a conformance result can measure.

A table has no release cadence. Anyone's twenty-line writer can be checked
against it in an afternoon, in any language, and a formatting error becomes
reportable as distinct from a decoding error: the row is the report.

## How to use it

Every row has an identifier in its first column, and those identifiers are the
point of them being there. `float-negative-zero` and `bytes-unpadded` are worth
naming because "your writer fails `bytes-unpadded`" is a bug report anybody can
act on, and "the base64 looks wrong" is not.

Hold your writer to the *Written as* column literally. Every scalar of this
language is a JSON string, so a scalar's text is exact character for character —
that is what makes a comparison of two answers string equality, which
[SPEC.md](SPEC.md)'s *Comparison is over the written form* says at length. The
order of an object's members is the one exception, because a JSON object is
unordered and the comparison is by key; the tables write them in the order the
copybook declares them because that is what reads best.

The identifiers of the rows are stable. A row whose answer changes is a change
to the value language, which is a change to [SPEC.md](SPEC.md) first.

## Characters

[SPEC.md](SPEC.md)'s [*Characters, and why trailing spaces do not
survive*](SPEC.md#characters-and-why-trailing-spaces-do-not-survive). All four
character categories — alphabetic, alphanumeric, numeric-edited and
alphanumeric-edited — are written the same way, and the trailing-space rule
covers the two edited categories as well.

| Row | Value | Written as |
|---|---|---|
| `text-alphanumeric` | A `PIC X(4)` item holding `A001`. | `"A001"` |
| `text-trailing-spaces` | A `PIC X(15)` item holding `BATCH-0001` and five spaces. | `"BATCH-0001"` |
| `text-all-spaces` | A `PIC X(10)` item holding ten spaces. | `""` |
| `text-leading-space` | A `PIC X(6)` item holding a space, `AB`, and three spaces. | `" AB"` |
| `text-interior-spaces` | A `PIC X(8)` item holding `A`, two spaces, `B`, and four spaces. | `"A  B"` |
| `text-tab` | A `PIC X(4)` item holding `A`, a tab, and two spaces. Only U+0020 is padding. | `"A\t"` |
| `text-alphabetic` | A `PIC A(5)` item holding `ABC` and two spaces. | `"ABC"` |
| `text-numeric-edited` | A `PIC ZZ,ZZ9.99` item holding ` 1,234.50`. The leading space is not trailing, so it stays. | `" 1,234.50"` |
| `text-numeric-edited-blank-sign` | A `PIC 999-` item holding `042` and the blank its sign position takes for a positive value. | `"042"` |
| `text-alphanumeric-edited` | A `PIC XXBXX` item holding `AB CD`. | `"AB CD"` |

## Numbers

[SPEC.md](SPEC.md)'s [*A number is a decimal
string*](SPEC.md#a-number-is-a-decimal-string). Zoned, packed, `COMP-6`, binary
and `COMP-5` all reach the same place: the unscaled digits, no leading zero, no
leading `+`, no `-0`, and never a JSON number.

| Row | Value | Written as |
|---|---|---|
| `number-positive` | A `PIC 9(3)` `DISPLAY` item holding 42. The item is three digits wide and the value is two. | `"42"` |
| `number-negative` | A `PIC S9(5) COMP-3` item holding −12345. | `"-12345"` |
| `number-zero` | A `PIC S9(3) COMP-3` item holding zero. | `"0"` |
| `number-negative-zero` | A `PIC S9(3) COMP-3` item whose bytes are `00 0D`, which is a packed negative zero. | `"0"` |
| `number-scaled` | A `PIC S9(3)V99 COMP-3` item holding −123.45. The digits are the unscaled ones; the point is in the descriptor. | `"-12345"` |
| `number-comp-6` | A `PIC 9(4) COMP-6` item holding 1234. | `"1234"` |
| `number-binary` | A `PIC S9(4) COMP` item holding −1. | `"-1"` |
| `number-binary-unsigned` | A `PIC 9(4) COMP-5` item holding 65535. | `"65535"` |
| `number-eighteen-digits` | A `PIC S9(18) COMP-3` item holding 999999999999999999. | `"999999999999999999"` |
| `number-beyond-a-double` | A `PIC S9(19)` `DISPLAY` item holding −1234567890123456789. | `"-1234567890123456789"` |

## Floats

[SPEC.md](SPEC.md)'s [*A float is written exactly, and never as a JSON
number*](SPEC.md#a-float-is-written-exactly-and-never-as-a-json-number).
`COMP-1` and `COMP-2` are the only two usages that land here, and the form is
C's hexadecimal floating constant, normalized: lowercase throughout, the
exponent's sign always written, no trailing zero in the fraction, and a zero
whose exponent is always `+0`.

| Row | Value | Written as |
|---|---|---|
| `float-one` | A `COMP-2` item holding 1. | `"0x1p+0"` |
| `float-nine` | A `COMP-2` item holding 9, which is 1.125 × 2³. | `"0x1.2p+3"` |
| `float-thirty-second` | A `COMP-2` item holding 0.03125. | `"0x1p-5"` |
| `float-tenth-comp-1` | A `COMP-1` item holding the nearest single to 0.1. There is no `COMP-1` value that 0.1 names, which is why the form is not decimal. | `"0x1.99999ap-4"` |
| `float-tenth-comp-2` | A `COMP-2` item holding the nearest double to 0.1. | `"0x1.999999999999ap-4"` |
| `float-subnormal` | A `COMP-1` item holding 2⁻¹⁴⁹, the smallest positive value a single can hold. A subnormal is normalized like everything else. | `"0x1p-149"` |
| `float-zero` | A `COMP-2` item holding zero. | `"0x0p+0"` |
| `float-negative-zero` | A `COMP-2` item holding negative zero, which is a different answer from zero. | `"-0x0p+0"` |
| `float-nan` | A `COMP-1` item holding any NaN. Neither the sign nor the payload is distinguished. | `"NaN"` |
| `float-infinity` | A `COMP-1` item holding positive infinity. | `"Infinity"` |
| `float-negative-infinity` | A `COMP-1` item holding negative infinity. | `"-Infinity"` |

## Runs of bytes

[SPEC.md](SPEC.md)'s [*`INDEX`, `POINTER` and `NATIONAL` are
base64*](SPEC.md#index-pointer-and-national-are-base64). RFC 4648 section 4's
alphabet, padded with `=`, canonical in the final quantum.

| Row | Value | Written as |
|---|---|---|
| `bytes-index` | An `INDEX` item holding the four bytes `00 00 00 07`. | `"AAAABw=="` |
| `bytes-pointer` | A `POINTER` item holding the eight bytes `00 00 00 00 00 01 00 00`. | `"AAAAAAABAAA="` |
| `bytes-national` | A `PIC N(2) USAGE NATIONAL` item holding the four bytes `00 41 00 42`, which is `AB` in UTF-16BE. The encoding is the descriptor's to record and not this format's to interpret. | `"AEEAQg=="` |
| `bytes-empty` | An `INDEX` item of no bytes. | `""` |

## Groups, tables and variants

[SPEC.md](SPEC.md)'s [*A group, a table and a
variant*](SPEC.md#a-group-a-table-and-a-variant) and [*Slack is not a
value*](SPEC.md#slack-is-not-a-value). A group is an object keyed by the
copybook's names, a repeating node is an array whatever it holds and however
many occurrences it has, a variant contributes its one held arm to the
*enclosing* object and no key of its own, and a slack node contributes nothing
at all.

| Row | Value | Written as |
|---|---|---|
| `group-two-members` | A group of two numeric items, `A` holding 1 and `B` holding 2. | `{"A": "1", "B": "2"}` |
| `group-nested` | A group holding an item `HDR-ID` and a group `HDR-WHEN` of one item `WHEN-DAY`. | `{"HDR-ID": "1", "HDR-WHEN": {"WHEN-DAY": "2"}}` |
| `group-only-slack` | A group holding nothing but a slack node. | `{}` |
| `slack-omitted` | A group of `A`, a slack node, and `B`. The slack takes no key and shifts nothing. | `{"A": "1", "B": "2"}` |
| `table-three` | An alphanumeric item occurring three times, holding `x`, `y` and `z`. | `["x", "y", "z"]` |
| `table-one` | The same item occurring once. A table of one is still an array, or a generator that lost a table would pass. | `["x"]` |
| `table-none` | The same item under an `OCCURS DEPENDING ON` whose count is zero. | `[]` |
| `table-of-groups` | A group of one item `LINE-SKU`, occurring twice. | `[{"LINE-SKU": "SK1"}, {"LINE-SKU": "SK2"}]` |
| `variant-arm-held` | A group of `BEFORE`, a variant of arms `NUM` and `TEXT`, and `AFTER`, where the occurrence holds `NUM`. The variant's own name appears nowhere. | `{"BEFORE": "1", "NUM": "2", "AFTER": "3"}` |
| `variant-arm-absent` | The same group where the occurrence holds `TEXT`. `NUM` has no key at all, rather than a key whose value is `null`. | `{"BEFORE": "1", "TEXT": "XX", "AFTER": "3"}` |

## A record and a document

[SPEC.md](SPEC.md)'s [`values.json`](SPEC.md#valuesjson) and [*A file the reader
refused*](SPEC.md#a-file-the-reader-refused). These are whole documents rather
than values, so the row is the entire text.

| Row | Value | Written as |
|---|---|---|
| `document` | A file of one `ORDER-RECORD` holding an `ORDER-ID` and a table of two `ORDER-LINE`. | `{"records": [{"name": "ORDER-RECORD", "value": {"ORDER-ID": "A001", "ORDER-LINE": [{"LINE-SKU": "SK1"}, {"LINE-SKU": "SK2"}]}}]}` |
| `document-no-records` | A file holding no record. The member is an empty array and never absent, and never `null`. | `{"records": []}` |
| `document-failure` | A read that stopped after the first record. The text is a note and is never compared. | `{"records": [{"name": "TXN", "value": {"AMT": "1"}}], "failure": "the sign nibble is not one of the four the convention admits"}` |
| `record-empty-value` | A record whose group holds nothing but slack. An empty value is an answer, not a missing one. | `{"records": [{"name": "TXN", "value": {}}]}` |

## Not admissible

The mistakes a writer makes, with what each should have been. A loader is
entitled to refuse every one of them, and this repository's does — so an author
who writes one is told by the thing that read their file rather than by a
generator appearing to disagree with them.

The *Form* column is which of the four forms the item takes, which
[SPEC.md](SPEC.md)'s [*Which form a value takes is decided by the
descriptor*](SPEC.md#which-form-a-value-takes-is-decided-by-the-descriptor)
makes a function of the item's usage and category.

| Row | Form | Not admissible | Admissible |
|---|---|---|---|
| `number-not-a-string` | number | `42` | `"42"` |
| `number-leading-zero` | number | `"012"` | `"12"` |
| `number-leading-plus` | number | `"+5"` | `"5"` |
| `number-minus-zero` | number | `"-0"` | `"0"` |
| `number-decimal-point` | number | `"123.45"` | `"12345"` |
| `number-exponent` | number | `"1e3"` | `"1000"` |
| `float-not-a-string` | float | `1` | `"0x1p+0"` |
| `float-in-decimal` | float | `"9"` | `"0x1.2p+3"` |
| `float-uppercase` | float | `"0X1P+3"` | `"0x1p+3"` |
| `float-no-exponent-sign` | float | `"0x1p3"` | `"0x1p+3"` |
| `float-trailing-zero` | float | `"0x1.20p+3"` | `"0x1.2p+3"` |
| `float-unnormalized` | float | `"0x2p+2"` | `"0x1p+3"` |
| `float-zero-exponent` | float | `"0x0p+1"` | `"0x0p+0"` |
| `float-lowercase-nan` | float | `"nan"` | `"NaN"` |
| `bytes-url-safe` | bytes | `"-_8="` | `"+/8="` |
| `bytes-unpadded` | bytes | `"AAEC/w"` | `"AAEC/w=="` |
| `bytes-not-canonical` | bytes | `"AR=="` | `"AQ=="` |
| `text-trailing-space-kept` | text | `"BATCH-0001 "` | `"BATCH-0001"` |

## What holds this document to the code

`TestTheWriterWritesEveryGrammarRow` and its neighbours, in
[`grammar_test.go`](../../internal/conformance/grammar_test.go), read the tables
above out of this file and hold this repository's own writer and loader to every
row: the writer produces the *Written as* text for the value,
the loader admits the *Admissible* spelling and refuses the *Not admissible*
one, and no row exists without a value behind it or a value behind it without a
row.

That is what makes this document a corpus rather than a description. It also
means the completeness this file claims is checked rather than asserted: a usage
or a category the IR gains, and this table does not cover, fails the test that
every one of them is named by some row.

The corpus's own entries do not cover a variant arm, a slack node, `INDEX`,
`POINTER`, `NATIONAL` or either edited category — every entry in
[`testdata/conformance/`](../../testdata/conformance) is derived from a real
file layout and none of those has needed one yet. This table is where those
constructs are written down, which is the other half of why it exists.
