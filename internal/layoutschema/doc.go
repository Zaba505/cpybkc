// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package layoutschema reads the published layout schema and checks a layout
// against it.
//
// The schema is schema/layout.sexpr, specified by docs/layout/SPEC.md's "The
// published schema". It is a set of form declarations written in the notation
// it describes — tagged forms over [github.com/z5labs/sexpr-go]'s grammar — and
// this package is the validator that reads it. An adopter generating layouts
// from metadata they already hold targets the schema and runs cpybkc to check
// what they generated, which is the trade SPEC.md's "Tagged forms over
// S-expressions" argues for: a notation that states an edge as an edge, at the
// cost of no off-the-shelf validator.
//
// # What it checks, and what it leaves alone
//
// Everything here is driven by the schema. Nothing in this package knows that a
// layout has an `encoding` form or that a `recfm` may say `VBS`; it knows how to
// read a declaration and how to hold a layout to one. A form added to the schema
// is checked without a line changing here, and that is the property that makes
// the published file the contract rather than this code.
//
// So it reports exactly what a declaration can be wrong about: an unknown form,
// a form in a context that does not admit it, a missing or repeated child, a
// wrong number of arguments, a value outside a closed set, a reference naming no
// record the layout defines, and the S-expression constructs SPEC.md's "What
// this document delegates" excludes — improper lists, quote shorthands, `nil`,
// `()` and booleans.
//
// It reports none of what a copybook decides, none of the conditional arities
// framing carries, and none of the rules relating two forms. Those are the
// layout reader's (#24) and `resolve`'s (#32–#38); SPEC.md's "What the schema
// does not say" is the list and the reasoning. A layout this package accepts is
// well-formed, not valid, and the distinction is deliberate: a schema that
// carried the conditional rules would be a second statement of them beside the
// reader's, for the two to disagree about.
//
// # Why diagnostics rather than an error
//
// [Schema.Check] returns every diagnostic it finds rather than the first,
// because a generated layout is generated wrong in the same way in many places
// at once, and a validator that reports one of them per run is a validator run
// once per fault. Each carries the position of the sub-form that is wrong rather
// than of the top-level form containing it, which is what SPEC.md's "Positions
// survive" buys and what the layout reader's spans are built out of.
//
// A diagnostic's position is a
// [github.com/Zaba505/cpybkc/internal/diag.Span] and names the file the layout
// was checked under, which is why [Schema.Validate] takes a name: an adopter
// checking a layout against a copybook is holding two files, and a position
// that cannot say which one it is in stops being usable exactly then.
package layoutschema
