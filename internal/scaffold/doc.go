// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package scaffold derives a layout scaffold from copybooks and writes it where
// `cpybkc init --out` names.
//
// # The split it implements
//
// A layout is two kinds of statement in one file and only one of them is
// knowledge the adopter has (docs/cli/SPEC.md, "What a copybook decides, and
// what only the adopter can"). This package writes the first kind complete —
// one `record` per 01-level, one `alternative` child per REDEFINES outside a
// repeating group — and writes the second kind as commented forms with their
// subjects filled in and their values left as placeholders.
//
// It invents nothing. Not an encoding axis, not a record format, not a
// discrimination strategy and not a sequencing operator: a guessed value is the
// one outcome worse than a blank, because a blank is reported and a guess is
// read. Every placeholder is spelled so that uncommenting the form it is in
// without filling it in is a fault the layout reader reports naming it — an
// adopter who works down the checklist by deleting `;;` and loses their place
// is the ordinary case, and a placeholder that happened to be a legal value
// would let them finish a layout they never wrote.
//
// # The file it writes is deliberately not a layout
//
// The scaffold parses as S-expressions and fails the layout reader's
// validation, and both halves are requirements. It parses because a file that
// failed the grammar would be reported as one lexical fault and nothing else,
// which turns the remainder back into something discovered a form at a time. It
// fails validation because the reader that will have to accept the finished
// layout is a better checklist than a second description of what a layout needs:
// running cpybkc over a fresh scaffold reports the entire remainder at once,
// each fault at the place the missing statement belongs.
//
// # One reading of a copybook, not two
//
// Which REDEFINES clauses become record types and which become variants is
// [github.com/Zaba505/cpybkc/internal/resolve.Describe]'s answer, over the same
// clusters and the same enclosing-table question the resolver itself uses. This
// package computes nothing about a copybook on its own. That is the whole
// reason the command is in the tool rather than a script over the published
// schema: a second reading that drifted would produce a scaffold cpybkc then
// rejected, and the adopter would be holding two readings of their copybook with
// nothing to say which is wrong.
//
// # What is here, and what is not
//
// [Derive] takes copybooks that have already been read, as bytes beside the path
// they were named by, and is a function of them alone — so two runs over one set
// of copybooks produce byte-identical files, which docs/cli/SPEC.md requires
// rather than hopes for. Opening the files, refusing a directory and reporting
// what could not be read are the command's, beside every other diagnostic it
// writes.
//
// [Write] is the destination rule, and it is not
// [github.com/Zaba505/cpybkc/internal/emit]'s: that package replaces what it
// finds, because a descriptor is derived entirely from its inputs and
// re-emitting is the point of the flag. A layout is not. Overwriting one an
// adopter has edited is the one unrecoverable thing this command could do — the
// derived half is recomputable and the `discriminate` forms and the `sequence`
// are not — so nothing at the destination is ever replaced, written through or
// appended to.
package scaffold
