// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package layout reads a layout file into a positioned AST.
//
// A layout is a set of tagged forms written as S-expressions, specified by
// docs/layout/SPEC.md's "The surface syntax". This package is the step between
// the bytes an adopter wrote and everything that reasons about them: it hands
// back [File], a set of [Form]s in the order the source states them, with a
// [Pos] on every node naming the file, the line and the column it was written
// at.
//
// # Why the AST is not sexpr-go's
//
// [github.com/z5labs/sexpr-go] already parses the source, and this package does
// not re-parse it — SPEC.md's "What this document delegates" hands the lexis and
// the grammar to that package, and there is one reading of it in this
// repository. What is added here is the two things a layout needs and a generic
// S-expression tree cannot have.
//
// A layout node carries the file it came from. sexpr-go parses an [io.Reader]
// and so has no name to attach; its [github.com/z5labs/sexpr-go.Pos] is a line
// and a column. A diagnostic an adopter can act on names the file too, and #31's
// cross-file spans put a layout position and a copybook position side by side —
// so a position that cannot say which file it is in is a position that stops
// being usable exactly when there are two.
//
// And a layout node is a form. Every list in a layout opens with a symbol naming
// the relation it states, so the AST has [Form] where the grammar has a list,
// with the tag lifted out of the elements and carrying a position of its own.
// That is what lets a diagnostic point at the tag that is misspelled rather than
// at the form containing it, which SPEC.md's "Every diagnostic carries a span"
// requires of every diagnostic this project emits.
//
// Uniform positions are the other half of it. Every [Node] answers
// [Node.Position], so a walker asking where something is never needs a type
// switch to find out.
//
// # What it rejects
//
// Several constructs are legal S-expressions and have no meaning in a layout:
// improper lists, quote shorthands, nil, the empty list and booleans. SPEC.md
// excludes each by name, for the reason it gives — "a construct with no meaning
// that nevertheless parses is a construct two generators will emit
// differently, and there is nothing for a diagnostic to say about it".
//
// This package rejects them rather than dropping them, and rejects them as
// [ConstructError], naming the construct and where it was written. There is no
// node in this AST for any of them, so accepting one would mean either
// inventing a node nothing downstream can read or silently discarding something
// an adopter wrote. Two further shapes are rejected the same way: a top-level
// node that is not a form ([NotAFormError]) and a form that does not open with
// a symbol ([UntaggedFormError]).
//
// [Parse] reports every fault it finds rather than the first, joined with
// [errors.Join], for the reason
// [github.com/Zaba505/cpybkc/internal/layoutschema] returns a slice of
// diagnostics: a generated layout is generated wrong in the same way in many
// places at once, and a reader that reports one fault per run is a reader run
// once per fault. Each is still assertable with [errors.As], because that is
// what errors.Join is traversed by.
//
// # What it does not check
//
// Everything that needs to know what a tag means. Whether `encoding` is a form
// a layout may carry, whether `recfm` admits `VBS`, whether a `record` name is
// defined, how many `framing` forms a layout takes: those are the published
// schema's, and [github.com/Zaba505/cpybkc/internal/layoutschema] is what holds
// a layout to it. The rules relating two forms, the conditional arities and
// everything needing a copybook are the layers above this one — SPEC.md's
// "Validation and diagnostics" is the split and the reasoning.
//
// So an AST this package returns is well-formed S-expression-wise and says
// nothing about whether it is a layout anything can resolve. Keeping the two
// apart is what makes a misspelled tag reportable as a misspelled tag: the
// reader has already built the form carrying it, with a position on it, by the
// time anybody asks what the tag means.
package layout
