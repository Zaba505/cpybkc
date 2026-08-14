// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import "fmt"

// malformedError is a descriptor that does not say what docs/ir/SPEC.md says a
// descriptor says.
//
// Every one of these is a bug upstream of this generator rather than something
// an adopter can fix in their copybook, and each carries a note naming the rule
// it broke — because the user reading it is holding a cpybkc that produced it
// and has no other way to tell a bug in the producer from one in their layout.
//
// Written here rather than imported from cmd/cpybkc-gen-go, for the reason this
// command's package comment gives about the version check and the argument
// parser: a package both generators shared would be a convenience no
// third-party generator has, and being a generator with no such convenience is
// the thing this one exists to demonstrate.
type malformedError struct {
	// What is the failure, as the `error:` line.
	What string

	// Rule is the requirement the descriptor broke, as the `note:` line.
	Rule string
}

// malformed is a [malformedError], spelled at its use sites the way a sentence
// is rather than as a struct literal.
func malformed(what, rule string) error {
	return &malformedError{What: what, Rule: rule}
}

// unresolved is the commonest of them: a reference to an identifier no node
// carries, or one carried by a node of a kind the referring position does not
// admit.
func unresolved(id uint64, position string) error {
	return malformed(fmt.Sprintf("%s names node %d, and the descriptor carries no such node of that kind", position, id),
		"every reference must resolve to a node in the same message, of a kind the referring position admits; see docs/ir/SPEC.md, \"Identity, ordering and determinism\"")
}

// Error implements the error interface.
func (e *malformedError) Error() string {
	return "the descriptor is malformed: " + e.What
}

// Notes is what follows it as a `note:` diagnostic.
func (e *malformedError) Notes() []string { return []string{e.Rule} }
