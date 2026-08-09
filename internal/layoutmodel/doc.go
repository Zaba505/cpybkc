// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package layoutmodel turns a positioned layout AST into the typed model of the
// format's layers, and enforces the rules a schema declaration cannot state.
//
// [github.com/Zaba505/cpybkc/internal/layout] hands back what an adopter wrote,
// with a position on every node.
// [github.com/Zaba505/cpybkc/internal/layoutschema] holds that to the published
// schema: an unknown form, an unknown child, a missing or repeated one, a value
// outside a closed set. Between them a layout is well-formed, and that is all it
// is — docs/layout/SPEC.md's "What the schema does not say" lists what a
// declaration cannot reach, and this package is where those land.
//
// Each layer of the format gets a reader here, and a type for what the layer
// says. [ReadProfile] is the encoding profile (#25), [ReadFraming] is the
// physical framing (#26), [ReadDiscrimination] is discrimination (#28),
// [ReadSequence] is sequencing (#29) and [ReadRenames] is the record
// definitions layer's renames (#27, #30). What they have in common is that a
// value handed back is one the format admits: a [Profile] carries four stated
// axes because a profile stating three is an error and not a value with a hole
// in it, a [Framing] resolves to one of the IR's four framings because the one
// record format that resolves to none is rejected while the layout is read,
// every `record` in a [Discrimination] carries exactly one strategy out of a
// closed set, every node of a [Sequence]'s expression is one of the eight terms
// the algebra is made of, carrying what that term takes, and every [Rename]
// names its item in full and carries the substitute beside the original rather
// than in place of it.
//
// # The one layer that prints as well as reads
//
// Sequencing is the only layer whose value is a term rather than a record of
// settings, and [Expression.String] renders one back into the notation a layout
// writes it in. It exists because a term is the one thing here that a diagnostic
// has to quote a *part* of — the subexpression a rule was broken under, not the
// form it was written in — and because printing then reading yields the same
// expression, which is a property the reader can be tested against and the other
// layers have no shape to state.
//
// # Why the model is not the AST
//
// A layer's rules are about what a form means, and a walker over [layout.Form]
// has to rediscover the meaning at every use. `(charset cp037)` is a form with a
// tag and a symbol until something decides that the tag names an axis, that the
// symbol names a code page, and that a code page is one of a bounded set — and a
// second walker deciding that a second time is a second reading of SPEC.md for
// the two to disagree about.
//
// So the reading happens once, here, and what comes out is data: [Charset] is a
// code page this project supports and cannot hold anything else, and
// [Axes.Complete] is a question about a value rather than about a check somebody
// remembered to run.
//
// # What it assumes about the schema
//
// Nothing. A reader here reports every fault it finds in the layer it reads,
// including the ones the published schema would also have caught, because a
// caller that skipped the schema check would otherwise get a model built out of
// a layout nothing held to anything.
//
// That is not a second statement of the schema. The schema says a well-formed
// layout carries four axes; this package says an [Axes] value has four axes, and
// refuses to build one from a layout that states three. The overlap is the point
// at which a declaration and a Go type happen to agree, and the rule
// docs/layout/SPEC.md states — "An implementation MUST NOT supply a default for
// a missing axis" — is a rule about the value, which only the value can keep.
//
// # Diagnostics
//
// Every fault is a typed error carrying a [layout.Pos], assertable with
// [errors.As], and a reader returns every one it found rather than the first,
// joined with [errors.Join]. The reasoning is
// [github.com/Zaba505/cpybkc/internal/layout]'s: a generated layout is generated
// wrong in the same way in many places at once, and a reader that reports one
// fault per run is a reader run once per fault.
//
// A reader that found a fault returns no model. A half-built value is one a
// caller can read, and there is no reading of a layout that was rejected.
package layoutmodel
