// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package assemble turns everything a resolved layout consists of — the
// framing, the record automaton, one resolved record type per `record` form,
// and the renames over them — into the single
// [github.com/Zaba505/cpybkc/irpb.Descriptor] a generator plugin consumes.
//
// [Assemble] is the whole of the interface. What reaches it is already
// resolved: [github.com/Zaba505/cpybkc/internal/resolve] has spent the COBOL
// and the algebra, and this package spends neither. It assigns the identifiers,
// spells each resolved value into the member the schema gives it, and holds the
// result to [Validate] before handing it back.
//
// # Why the identifiers are assigned here and nowhere else
//
// A [github.com/Zaba505/cpybkc/internal/resolve.Node] names a
// [github.com/Zaba505/cobol-go/copybook.Field] and never an identifier, and so
// does a [github.com/Zaba505/cpybkc/internal/resolve.Predicate], a
// [github.com/Zaba505/cpybkc/internal/resolve.Repetition] and a
// [github.com/Zaba505/cpybkc/internal/resolve.Binding]. That is deliberate:
// resolving a copybook is one question and numbering the result is another, and
// a package doing both would have to know, while it was laying out a record,
// which record node the automaton would eventually point at.
//
// So the numbering is one traversal, here, and it is the traversal
// docs/ir/SPEC.md's "Identity, ordering and determinism" asks a producer for.
// Identifiers are positions in the node list: a node's identifier is its index,
// the list is therefore in ascending identifier order by construction, and
// neither property is a sort this package could forget to run.
//
// # The traversal, and why identical inputs produce identical bytes
//
// The order is fixed and reads down the descriptor:
//
//  1. the file node, which is therefore identifier zero;
//  2. each record type in the order [Options.Records] gives them, and inside
//     one, its record node, then its top level, then its members in record
//     order, an arm's predicate ahead of the arm's body;
//  3. the registers, in the order the automaton allocated them;
//  4. every state, in the automaton's own order;
//  5. then, state by state in that same order, the guards qualifying its
//     acceptance, and each of its transitions with its predicate, its guards
//     and its bindings.
//
// The states are numbered before anything hanging off them because a transition
// names the state it moves to and a cycle there is ordinary — a header followed
// by any number of details is one — so there is no order in which every state's
// identifier is already known when its transitions are built.
//
// Nothing in that walk reads a Go map, which is what makes the whole of it a
// function of the input rather than of the run. Two assemblies of one layout
// produce identical descriptors, and
// [github.com/Zaba505/cpybkc/internal/emit.Marshal] turns identical descriptors
// into identical bytes — the two halves docs/ir/SPEC.md asks for, met in the
// two places that can meet them (#38).
//
// A predicate is shared by every transition admitting one record, because
// `resolve` compiles a record's discriminator once and hands the same node to
// each appearance. It is shared *within* a record type and never across one:
// the field a predicate names is looked up in the record whose bytes the
// predicate reads, and two record types that resolved from one copybook have
// two field nodes for one copybook item. Guards and bindings are not shared at
// all — two transitions carrying the same test carry two guard nodes, because
// the identity of a guard is not something any consumer reads.
//
// # No default survives, and the validation pass says so
//
// [Validate] is the completeness pass, and [Assemble] runs it over its own
// output before returning: nothing leaves this package unvalidated, so the
// obligation is met wherever a descriptor comes from rather than wherever a
// generator is invoked.
//
// What it checks is the shape of the message rather than the COBOL behind it —
// every reference resolves to a node of a kind its position admits, every node
// is reachable from the file node, every closed set carries a member rather
// than its unspecified zero, and every field states all five encoding axes.
// The last is the one docs/ir/SPEC.md phrases as a requirement on the producer
// twice over, because each of the five fails silently when it is wrong: a
// charset yields a plausible string and a byte order a plausible number, with
// nothing in the file to disagree.
//
// The pass exists because the failures it catches are otherwise invisible until
// a generator reads the descriptor, and a generator reading one is entitled to
// assume it conforms. A descriptor that reached a plugin with an axis unset is
// a bug in this repository being reported by somebody else's code.
//
// # Faults are diagnostics, not a first error
//
// Every fault is a [github.com/Zaba505/cpybkc/internal/diag.Error] carrying the
// span of whatever the fault is about — the layout's framing form, the
// copybook entry behind a node — joined with [errors.Join] and assertable with
// [errors.As], exactly as the readers and `resolve` report theirs. One
// descriptor is wrong in the same way in many places at once, and a pass
// reporting one fault per run is a pass run once per fault.
package assemble
