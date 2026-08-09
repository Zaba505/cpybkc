// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package diag is the vocabulary every layer of cpybkc reports a fault in.
//
// docs/layout/SPEC.md's "Every diagnostic carries a span, and some carry two"
// is what this package holds the rest of the repository to. A diagnostic names
// what it found and where, the where is a file with a line and a column in it,
// and a diagnostic about something in a copybook names the copybook too. Three
// things follow from that and all three are here: a [Span], a [Diagnostic] that
// carries as many of them as it implicates, and one rendering of the pair so
// that a fault reads the same however it was reached.
//
// # Why a span is not a layout position
//
// [github.com/Zaba505/cpybkc/internal/layout.Pos] already names a file, a line
// and a column, and it is the right type for a place in a layout. It is the
// wrong type for a place in a copybook: a copybook is not a layout, nothing
// parses one into that AST, and a position out of the copybook reader (#32)
// would have to be laundered through a package it has nothing to do with to be
// reportable.
//
// So a [Span] is a place in a file and belongs to no reader. This package
// imports nothing outside the standard library, which is what lets the layout
// reader, the layer readers and `resolve` all report through it without one of
// them becoming a dependency of the others.
//
// # What a second span is for
//
// A diagnostic about something in a copybook carries a span into the layout and
// a span into the copybook, because the adopter is usually holding a file they
// did not author and a copybook they did not write, and a message naming one of
// the two leaves them to find the other by hand. SPEC.md's "A copybook that is
// not there, and an item that is not in it" requires both by name, and
// [MissingCopybookError] and [UndeclaredItemError] are those two.
//
// Neither is raised here. Both checks need a copybook read, so both are
// `resolve`'s (#32–#38) and SPEC.md says so; what is here is the shape of the
// diagnostic, which is a requirement on the message rather than on the stage
// that finds it. A [Diagnostic] takes as many spans as a fault implicates
// rather than exactly two, because an item reference that resolves to more than
// one item names every match, and "two" is the smallest case of that rather
// than the rule.
//
// # Why accumulation is here
//
// A generated layout is generated wrong in the same way in many places at once,
// so every reader in this repository reports every fault it found rather than
// the first, joined with [errors.Join]. [List] is that accumulation, in one
// place: "keep reading after a fault" is decided once, and a third reader does
// not arrive with a third copy of it.
//
// [Diagnostics] is the other end. It walks what [errors.Join] built and hands
// back one [Diagnostic] per fault, in the order they were reported, so that a
// caller printing them never has to know how they were joined.
//
// # Why rendering is here
//
// A fault that names two files cannot render itself on one line, and a
// [Diagnostic] that rendered differently in the CLI and in a test would be
// pinned by the test and read by nobody. [Diagnostic.String] is the rendering,
// [Render] applies it to a whole joined error, and the golden tests beside this
// file are what the output is held to.
//
// An error that does not carry a diagnostic is rendered as it renders itself.
// Every typed error in this repository already leads with the position it
// carries, so a reader's fault reads the same through [Render] as it does
// through [error.Error]; what [Render] adds is the span such an error cannot
// state, which is one in a file the layout is not.
//
// # What it does not do
//
// There are no severities: everything reported here is something an adopter has
// to fix, and a warning nobody has to act on is a line of output that teaches a
// reader to skip the others. There is no source excerpt, no caret line and no
// colour — a span an editor and a terminal both understand is what a diagnostic
// owes its reader, and quoting the source back is a decision about a terminal
// that the CLI is closer to than this package.
package diag
