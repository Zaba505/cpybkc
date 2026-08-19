// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package layoutdoc reads docs/layout/SPEC.md's worked examples out of the
// document itself.
//
// Three packages check the layouts that document shows an adopter —
// [github.com/Zaba505/cpybkc/internal/layout] parses them,
// [github.com/Zaba505/cpybkc/internal/layoutschema] validates them against the
// published schema, and [github.com/Zaba505/cpybkc/internal/layoutmodel] reads
// every layer out of them — and each of them reads the example rather than
// carrying a copy of it. A copy is a second thing to keep right, and the one
// that is wrong is always the one nobody runs: an adopter runs the document.
//
// # A heading per example
//
// A worked example is located by the heading of the section it sits under, and
// is the first fenced block there. The alternative was an anchor of some kind
// inside one section holding both, and the reason against it is what happens
// when a third example is added: with one section per example the new heading
// terminates the previous section, so what an existing heading extracts cannot
// move, while under a shared section the identity of every block is its
// position among the others and an example inserted above the first silently
// retargets every package that reads it.
//
// It is also the answer this repository already gave. .dagger/worked_example.go
// reads docs/container/SPEC.md's two worked examples by heading, one constant
// each, for the same reason — so a second convention here would be a second
// convention and not a decision.
//
// # Why this is a package and not a helper per test
//
// It was a helper per test for as long as two packages wanted one, with
// [github.com/Zaba505/cpybkc/internal/layoutmodel] carrying a note saying that
// two copies were worth leaving until a third package wanted one. Two examples
// read by three packages is that arrival: what they share is one reading of the
// document, and three readings of it are three chances to disagree about which
// block is which.
//
// The functions here return errors rather than taking a [testing.TB], so that
// nothing outside a test's own file decides what a missing section means, and
// so that a package whose only consumers are tests does not put the testing
// flags into every binary that links it.
package layoutdoc
