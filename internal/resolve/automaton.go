// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"strconv"
	"strings"

	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// This file is the record automaton: the nodes docs/ir/SPEC.md's "The
// sequencing automaton" describes, and the compilation of a layout's sequencing
// expression into them.
//
// # Why a layout's expression does not reach a generator
//
// The IR carries states and transitions and **MUST NOT** carry the expression
// they were compiled from. A regex-like algebra implemented independently in
// four languages is four slightly different algebras, and the disagreements
// surface as files one generator reads and another rejects. So the algebra is
// spent here, exactly once, and what leaves is a graph a plugin author walks
// knowing no COBOL and no regular expressions.
//
// # The construction, and why it has no epsilon transitions
//
// The graph is a position automaton: one state per appearance of a record name
// in the expression, plus a start state that is nothing's target. A transition
// leaves the state for one appearance and enters the state for another exactly
// where the second may follow the first, and it admits the second's record.
// Every transition therefore consumes exactly one record, which is
// docs/ir/SPEC.md's "No epsilon transitions, and what the graph pays instead"
// falling out of the construction rather than being enforced over it.
//
// It is also what makes the start state expressible. docs/layout/SPEC.md's "The
// first record of a file is the first thing the expression admits, and nothing
// is written to say so" needs a state no transition re-enters, and a position
// automaton has one for free: the start state stands for no appearance, so no
// position can follow it (#80).
//
// The graph pays for the missing epsilons in transitions, as that section says
// it does: a segment that may be skipped is compiled by letting every record
// that can follow the skip be reachable directly, each under the guards saying
// the skipped segment is done. What a state carries follows the number of
// segments that can be skipped past it and never the values in the file.
//
// # Registers, and what compiles into one
//
// `times` and `when` are the only operators that read a value out of the file,
// and each becomes a register: an integer one for `times`, holding what is left
// of a count, and a bytes one for `when`, holding the value a guard compares.
// Every transition admitting the record that holds the named item binds the
// register from that item, and the transitions the operator governs carry the
// guards that read it (docs/ir/SPEC.md, "The automaton remembers, in
// registers", #76, #77).
//
// Nothing else becomes a register, and that is the SHOULD in "When a value
// becomes a state, and when it becomes a register" kept rather than restated: a
// value tested only on the transition admitting the record that holds it is a
// record's own discriminator, and a discriminator compiles into the predicate on
// that transition. It is a branch — two transitions admitting two records and
// moving to different states — and no register is emitted for it, because a
// register the graph does not need is memory every consumer in every language
// carries for nothing.
//
// A `times` register is one per operator, because a pass of the run takes one
// off it: two counted runs naming one item are two counts, and sharing the
// register would leave the second run at whatever the first left behind. A
// `when` register is one per item, because nothing ever writes it but the
// transitions admitting the record it is read from, and two `when`s over one
// item are two guards over one value.
//
// # A counted run is one register and one guard
//
// The transition entering a pass carries the guard that the counter is above
// zero and the binding that takes one off it, and the run is left where the
// counter has reached zero. So a `PIC 9(4)` count is one register and one guard
// whatever the number in the file is, and never ten thousand states — which is
// what docs/ir/SPEC.md promises in exchange for the register file it asks a
// consumer to carry.
//
// The guard is also the rule that a counter cannot run below zero: the only
// transition that takes one off is the one the guard makes ineligible at zero.
//
// # What `when` cannot say, and what that costs
//
// A guard is one of three tests and none of them is a negation
// (docs/ir/SPEC.md, "Three tests, and no fourth"). So `(when <item> "Y" <e>)`
// compiles into a guard on the transitions entering `<e>` and into nothing at
// all on the path that skips it: there is no test for *the flag is not `Y`* to
// put there, and inventing one is a fourth member of a closed set.
//
// What that costs is stated here because it is a real loss and an adopter can
// buy it back. docs/ir/SPEC.md's "A counted run, as nodes" carries the guard
// `flag one-of N or space` on its accepting state, and detects a file whose
// summary is missing where the flag said `Y`. A layout writing `(when flag "Y"
// SUMMARY)` gets the other three detections that appendix lists and not that
// one, because the complement of `Y` is not a set this compiler can know. An
// adopter who needs it states the count instead of the flag — `(times SUMMARY
// (item HEADER SUM-COUNT))` compiles into an acceptance guard, because zero is
// a test the set has.
//
// docs/layout/SPEC.md, "A `when` permits a record, and never requires one" is
// that loss written where an adopter meets it, and it names the other way out:
// where the governing value is the record's own discriminating item, two record
// names over one copybook put the flagged record in a state of its own and the
// records that must follow it are the only ones that state offers. No register
// and no guard, because the value became a state (#144).
//
// # A guard lands on a position, and a repetition has two ways into one
//
// A `when` guards every transition entering a position inside the expression it
// wraps, and the construction draws no distinction between the ways in. So
// `(when <item> "Y" (+ <e>))` guards the transitions entering the repetition and
// not its back edge, while `(+ (when <item> "Y" <e>))` guards both, the second
// being the same transitions seen from inside the body.
//
// Nothing spells the back edge alone. A record name is one position however many
// transitions reach it, which is what the position automaton buys above, and
// telling two entries apart is making two positions — the `alt` of two record
// names again. One consequence surfaces as a diagnostic about the read loop
// rather than about the repetition: a guard on the back edge is a guard on the
// way in, so `(+ (when (item DETAIL NEXT-FLAG) "X" DETAIL))` is refused by the
// strictly-earlier proof below, because on the first pass nothing has bound the
// register (docs/layout/SPEC.md, "A guard on a repetition guards every way into
// its body", #144).
//
// # What is proved before a byte is read
//
// Two proofs run over the assembled graph, and each is docs/ir/SPEC.md asking
// for a layout to be rejected rather than a file to be misread.
//
// Every read of a register is preceded, on every path from the start state, by
// a binding of that register on a transition taken strictly earlier than the
// reading one (#88). Strictly earlier is the whole of it: a transition's
// bindings apply at step 7 of the read loop and its guards are evaluated at step
// 3, so a transition that binds a register it also reads reads the value the
// binding was about to replace, or nothing at all. That is what rejects `(times
// R (item R N))` — a run counted by a field of the record being counted — and
// what admits `(times D (item H N))`, where the transition admitting a detail
// reads the counter an earlier transition bound and takes one off it in the same
// step.
//
// And no two transitions leaving one state are eligible together and selected by
// predicates that can both match one record ("When two match, and when none
// does"). Guards narrow which pairs that is about: two transitions whose guards
// cannot hold at the same time may be selected by the very same test on the very
// same bytes, which is what makes a counted run expressible at all. A transition
// carrying no predicate is inside that rule rather than beside it — it matches
// every record, so it overlaps every sibling whose guards can hold at the same
// time as its own (#80).

// RegisterKind is what a register holds, one member of the closed set
// docs/ir/SPEC.md's "The automaton remembers, in registers" admits.
//
// The zero value is not a kind. A register handed back by [CompileSequence]
// always names one.
type RegisterKind int

const (
	// RegisterBytes holds its source field's bytes as they appear in the
	// record, so a guard over one is a byte comparison and needs no charset
	// knowledge — the same reason a predicate tests bytes.
	RegisterBytes RegisterKind = iota + 1

	// RegisterInteger holds a number, decoded from the source field by that
	// field's own four encoding axes, because a count is arithmetic and the
	// field holding one may be zoned, packed or binary.
	RegisterInteger
)

// String implements the [fmt.Stringer] interface.
func (k RegisterKind) String() string {
	switch k {
	case RegisterBytes:
		return "bytes"
	case RegisterInteger:
		return "integer"
	}
	return "unknown"
}

// Register is a value the automaton carries forward between records.
//
// The IR's register node declares its kind and nothing more. What is carried
// beside it here is what a binding and a diagnostic need: the item reference the
// layout wrote, and the copybook field it resolved to. Both are read by this
// package and neither is a member of the IR node.
type Register struct {
	// ID is the register's identifier, assigned in the order the operators
	// that need one are met walking the expression.
	ID int

	// Kind is what it holds.
	Kind RegisterKind

	// Source is the item reference the operator wrote, which is what a
	// diagnostic about this register quotes.
	Source layoutmodel.ItemRef

	// Field is the copybook item that reference resolved to: the field every
	// binding of this register reads.
	Field *copybook.Field
}

// String renders the register the way a diagnostic names one: the reference the
// layout wrote.
func (r *Register) String() string {
	if r == nil {
		return "nothing"
	}
	return r.Source.String()
}

// BindingValue is what a binding writes, one member of the closed set
// docs/ir/SPEC.md admits.
type BindingValue int

const (
	// BindField writes the value of a field contained in the record the
	// transition admits.
	BindField BindingValue = iota + 1

	// BindLessOne writes the register's own value less one. It is what
	// counts: a transition that admits one detail and takes one off the
	// counter is how a run of n records is read without n states.
	BindLessOne
)

// String implements the [fmt.Stringer] interface.
func (v BindingValue) String() string {
	switch v {
	case BindField:
		return "field"
	case BindLessOne:
		return "less one"
	}
	return "unknown"
}

// Binding is a write of a register, applied when the transition carrying it is
// taken.
//
// Bindings are the last thing a transition does, so nothing that transition
// reads sees what one writes: not its guards, and not the extent of the record
// it admits. Each reads the register file as it stood on entry to the state, so
// the order of one transition's bindings is not significant.
type Binding struct {
	// Register is the register written.
	Register *Register

	// Value is what is written.
	Value BindingValue

	// Field is the field the value comes from under [BindField], and nil
	// under [BindLessOne]. It is a field of the record the transition
	// admits, at any depth.
	Field *copybook.Field
}

// GuardTest is what a guard tests, one member of the closed set of three
// docs/ir/SPEC.md admits. There is no fourth, and no negation: conjunction is
// the guard list, disjunction is a second transition leaving the same state, and
// a state already is a disjunction.
type GuardTest int

const (
	// GuardEquals is the register equal to one carried literal.
	GuardEquals GuardTest = iota + 1

	// GuardOneOf is the register equal to one of a carried set of literals.
	GuardOneOf

	// GuardPositive is the register holding an integer greater than zero.
	GuardPositive
)

// String implements the [fmt.Stringer] interface.
func (t GuardTest) String() string {
	switch t {
	case GuardEquals:
		return "equals"
	case GuardOneOf:
		return "one-of"
	case GuardPositive:
		return "positive"
	}
	return "unknown"
}

// Guard is a test of a register that decides whether a transition is eligible,
// or whether a state accepts end of input.
//
// A guard is evaluated before the record in front of the consumer is examined at
// all, and against the register file as it stands on entry to the state, so a
// guard never reads what its own transition binds. All of a transition's guards
// must hold for it to be eligible, and their order is therefore not significant.
type Guard struct {
	// Register is the register tested.
	Register *Register

	// Test is the test.
	Test GuardTest

	// Values are what the register is compared against, in the order the
	// layout writes them: exactly one under [GuardEquals], at least one under
	// [GuardOneOf], and none under [GuardPositive].
	//
	// They are resolved rather than the layout's own spellings. A guard over a
	// bytes register compares bytes, so its values are the source field's
	// width and already padded to it — no consumer decides whether `Y` matches
	// `Y `, which is the same comparison [Predicate] refuses to leave open. A
	// guard over an integer register compares a number, and carries one.
	Values []Value
}

// key is what makes two guards the same guard, so that composing an expression
// out of its parts cannot put one test on a transition twice.
func (g Guard) key() string {
	parts := make([]string, 0, len(g.Values)+2)
	parts = append(parts, strconv.Itoa(g.Register.ID), g.Test.String())
	for _, value := range g.Values {
		parts = append(parts, value.Identity())
	}
	return strings.Join(parts, " ")
}

// Holds reports whether the guard holds of the value the register carries.
//
// The register file is the consumer's, so what is handed over is the one value
// the guard reads rather than the whole of it: a guard is a test of one
// register, and a consumer evaluating one already has it (docs/ir/SPEC.md, "The
// automaton remembers, in registers").
func (g Guard) Holds(held Value) bool {
	switch g.Test {
	case GuardPositive:
		return held.Bytes == nil && held.Number > 0
	case GuardEquals, GuardOneOf:
		for _, value := range g.Values {
			if sameValue(value, held) {
				return true
			}
		}
	}

	return false
}

// Transition is one edge of the automaton: it consumes exactly one record, and
// there is no transition that moves without reading.
type Transition struct {
	// Record is the name of the `record` form whose record this transition
	// admits.
	//
	// It is what the transition *produces* and never what selects it: a
	// transition is not labelled with a record name, because which record the
	// consumer is looking at is precisely the thing it does not yet know
	// (docs/ir/SPEC.md, "The sequencing automaton").
	Record string

	// Predicate is the discriminator that selects this transition, and is nil
	// where it carries none.
	//
	// A transition carrying no predicate matches every record and is selected
	// by its guards alone — or, where it carries none of those either, by
	// being the only thing its state offers. That is the *absence* of a
	// predicate and not a member of the set testing nothing
	// (docs/ir/SPEC.md, "A transition may carry no predicate", #80).
	//
	// It is carried on every transition admitting a record whose discriminator
	// names a field, even at a state where the guards alone would select it.
	// That is docs/ir/SPEC.md's **SHOULD** kept rather than optimised away: a
	// predicate the automaton does not need in order to choose is the only
	// detection such a state has, and a group two records short would
	// otherwise be absorbed by reading the next group's header as a detail.
	Predicate *Predicate

	// Guards are the tests that make this transition eligible, all of which
	// must hold. A transition carrying none is always eligible.
	Guards []Guard

	// Bindings are the register writes applied when it is taken.
	Bindings []Binding

	// To is the state the automaton moves to.
	To *State
}

// State is a state of the record automaton.
//
// States carry identifiers and no names. A consumer reads a file by evaluating
// the current state's transitions in the order given, skipping any whose guards
// do not all hold, and taking the first of the rest that carries no predicate or
// whose predicate matches.
type State struct {
	// ID is the state's identifier. The start state's is zero.
	ID int

	// Accepts reports whether end of input in this state is a complete file.
	//
	// A consumer reaching end of input in a state that does not accept, or
	// whose acceptance guards do not all hold, reports the file as truncated
	// rather than returning the records it managed to read.
	Accepts bool

	// Acceptance are the guards qualifying acceptance: the state accepts end
	// of input only when all of them hold. A state that does not accept
	// carries none.
	//
	// Guarded acceptance is what makes the last iteration of a count
	// detectable. A state that reads details while a counter is positive
	// would otherwise accept a file stopping three details short of what its
	// own header promised.
	Acceptance []Guard

	// Transitions are the edges leaving the state, in evaluation order.
	Transitions []*Transition
}

// Automaton is a layout's sequencing expression, compiled.
type Automaton struct {
	// Start is the state the read begins in. No transition enters it, which
	// is what makes *the first record of the file* expressible: a state is
	// the only thing the automaton knows about position, and there is no
	// predicate for *first* to lower into (#80).
	Start *State

	// States are every state, the start state first, in a deterministic
	// order.
	States []*State

	// Registers are every register the automaton carries, in the order they
	// were allocated. It is empty for a layout whose sequencing needs no
	// memory, which is every layout naming neither `times` nor `when`.
	Registers []*Register
}

// SequencedRecord is one record type the sequencing expression may name.
//
// It arrives resolved rather than as the layout's forms, for [Redefine]'s
// reason: reading a layout is one step and resolving a copybook against it is
// another, and a package doing both would read a layout's spelling of a record
// in two places.
type SequencedRecord struct {
	// Name is the name the layout's `record` form defines, which is what the
	// sequencing expression writes.
	Name string

	// Copybook is the copybook's path as the layout spells it, carried so
	// that a diagnostic about an item can name the file the fault is in.
	Copybook string

	// Item is the copybook's top-level item this record is bound to.
	Item *copybook.Field

	// Discriminator is the strategy the layout's `discriminate` form chose
	// for this record, and becomes the predicate on every transition
	// admitting it — or, under [layoutmodel.SingleRecordType], the absence of
	// one.
	Discriminator layoutmodel.Strategy
}

// Sequencing is what compiling a sequencing expression needs beyond the
// expression itself.
type Sequencing struct {
	// Sequence is the layout's sequencing layer, as
	// [layoutmodel.ReadSequence] hands it back.
	Sequence *layoutmodel.Sequence

	// Dialect is the compiler-side half of the layout, for [Options.Dialect]'s
	// reason. It is read to lay each record's copybook out, which is what
	// answers whether an item a `times` or a `when` names repeats or sits
	// inside a group that repeats.
	Dialect copybook.Dialect

	// Reading is which of the two vendor readings of `OCCURS DEPENDING ON` the
	// layout says its file was written under, for [Options.Reading]'s reason.
	//
	// It is read for two rules. A discriminator's target must sit at a constant
	// position within its record, and whether an item ahead of it has an extent
	// that moves is a question only the reading answers; and the shortest
	// record a state can admit is measured with every table at the occurrence
	// count the reading gives it. Under `noodoslide` the same clause is a fixed
	// table and nothing moves, so a layout stating no reading is one this half
	// never has to ask.
	Reading layoutmodel.Reading

	// Framing is the layout's physical framing, or nil where the caller has
	// none to state.
	//
	// It is read for one rule, docs/ir/SPEC.md's "A predicate never reads past
	// the record in front of it": which of the two mechanisms bounds a
	// predicate is decided by the framing, and only the one that bounds the
	// *layout* is a thing to reject a layout over (#94). What the rule needs of
	// it is [layoutmodel.Framing.Kind] and, under a fixed-length dataset, the
	// `lrecl` every record type is padded out to.
	//
	// The whole value is taken for [Options.Framing]'s reason: the bound
	// follows from the record format and the `lrecl` read together, and a
	// caller decomposing it here would be a second reading of
	// docs/layout/SPEC.md's table.
	//
	// A nil framing states neither mechanism, and the rule is simply not run —
	// as it is not for the two framings that state each record's length.
	// [layoutmodel.ReadFraming] refuses a layout that does not carry exactly
	// one `framing` form, so nil is a caller that assembled a [Sequencing] by
	// hand rather than a layout an adopter wrote.
	Framing *layoutmodel.Framing

	// Encoding is the layout's encoding profile, and EncodingOverrides the
	// per-item overrides over it: the four axes a literal is resolved to bytes
	// through.
	//
	// They reach the automaton for the reason they reach [Resolve]: a
	// discriminator's literal and a `when`'s are compared against a field's
	// bytes, and what those bytes are is the item's charset and width. A
	// layout stating fewer than four axes is refused while it is being read
	// and again as a record is resolved, so what arrives here is complete or
	// the caller never got this far.
	Encoding          layoutmodel.Axes
	EncodingOverrides []EncodingOverride

	// Records are the record types the expression may name, in the order the
	// layout defines them.
	Records []SequencedRecord
}

// CompileSequence compiles a layout's sequencing expression into the record
// automaton docs/ir/SPEC.md's "The sequencing automaton" describes.
//
// What comes back is states and transitions, the registers the value-reading
// operators needed, and nothing of the expression they were compiled from. Every
// transition consumes exactly one record, no transition enters the start state,
// and a counted run is one register and one guard however wide the count field
// is.
//
// Every fault it finds is reported, joined with [errors.Join] and assertable
// with [errors.As], rather than the first — with one staging: a `times` or a
// `when` whose item reference does not resolve stops the compilation there,
// because the register that reference decides is what every guard, binding and
// proof after it is about, and a graph built over a register nobody could
// resolve would report a second fault per transition that reads it.
//
// The faults it reports are docs/ir/SPEC.md's and docs/layout/SPEC.md's, and
// each names what the section asking for it asks to be named rather than
// reporting a generic ambiguity:
//
//   - an item reference naming no item of the record it is rooted at, or more
//     than one ([SequenceItemError]);
//   - an item that repeats or sits at any depth inside a group that repeats,
//     naming the record, the item and the enclosing group
//     ([SequenceOccurrenceError], #84);
//   - a `times` whose item does not decode to an integer
//     ([SequenceCountKindError]);
//   - a register read where some path reaches the read without an earlier
//     binding, including the run counted by a field of the record being counted
//     ([UnboundRegisterError], #88);
//   - two transitions leaving one state whose guards can hold at the same time
//     and whose predicates can both match one record, a transition carrying no
//     predicate included ([SequenceAmbiguityError], #37, #80);
//   - a state whose acceptance would have to be a disjunction of guard lists,
//     which is not a shape a state can carry ([SequenceAcceptanceError]).
//
// What it does not report is the shapes the algebra cannot spell.
// docs/ir/SPEC.md's "The automaton counts; it does not compute" asks `resolve`
// to reject arithmetic on a count, a register compared against another register
// and a guard comparing a register to a field of the record in front of the
// consumer. None of the three is writable: docs/layout/SPEC.md closes the
// operator set at eight and every one of them compares a value against literals
// the layout wrote. The one member of that list a layout *can* ask for is a
// dependence on a value in a record the consumer has not read, and that is
// [UnboundRegisterError] — which names the record and the item, as that section
// requires, and not an ambiguity.
func CompileSequence(opts Sequencing) (*Automaton, error) {
	if opts.Sequence == nil {
		return nil, ErrNilSequence
	}

	c := &compiler{
		opts:       opts,
		follow:     make(map[int][]entry),
		byItem:     make(map[string]*Register),
		predicates: make(map[string]*Predicate),
		layouts:    make(map[string]*copybook.Layout),
	}

	top := c.expression(opts.Sequence.Expression)
	if c.faults.Failed() {
		return nil, c.faults.Err()
	}

	automaton := c.assemble(top)

	c.prove(automaton)
	c.checkDiscrimination(automaton)
	c.checkAmbiguity(automaton)
	if c.faults.Failed() {
		return nil, c.faults.Err()
	}

	return automaton, nil
}

// appearance is one appearance of a record name in the expression, and the state
// the automaton is in once that appearance has been admitted.
type appearance struct {
	// record is the name of the `record` form the appearance names.
	record string

	// pos is where the name was written, which is what a diagnostic about
	// this appearance points at.
	pos layout.Pos

	// predicate is the discriminator of that record, compiled, or nil where
	// its strategy lowers into no predicate at all.
	predicate *Predicate
}

// entry is one way into a position: the guards a transition into it carries and
// the bindings it applies.
type entry struct {
	at      int
	guards  []Guard
	binding []Binding
}

// exit is one way an expression is complete at a position: the guards under
// which nothing more of it is outstanding.
//
// Those guards become the acceptance guards of the state where the expression is
// the whole sequence, and the leading half of a transition's guards where
// something may follow it.
type exit struct {
	at     int
	guards []Guard
}

// facts are what compiling one node of the expression yields: where it can
// start, where it can end, and under what conditions it admits no record at all.
//
// The three are the position automaton's `first`, `last` and `nullable`, with a
// guard list on each — which is the whole of what the value-reading operators
// add to the construction. `null` is nil for an expression that always admits at
// least one record, and holds one entry per way of admitting none: an
// unconditional way carries no guards.
type facts struct {
	first []entry
	last  []exit
	null  [][]Guard
}

// compiler holds the state one [CompileSequence] accumulates.
type compiler struct {
	opts   Sequencing
	faults diag.List

	// positions are the appearances of record names, in the order the walk
	// meets them. A state's identifier is its index here plus one, the start
	// state being zero.
	positions []appearance

	// follow is the ways into each position from another, keyed by the
	// position followed.
	follow map[int][]entry

	// registers are every register allocated, in allocation order.
	registers []*Register

	// byItem is the bytes register standing for each item a `when` reads,
	// keyed by the reference's spelling. Nothing writes such a register but
	// the transitions admitting the record it is read from, so two `when`s
	// over one item are two guards over one value.
	byItem map[string]*Register

	// predicates are the compiled discriminator of each record, keyed by the
	// record's name and holding nil for a record whose strategy lowers into
	// none. A record named twice in the expression is compiled once, so a
	// discriminator that cannot be resolved is one fault and not one per
	// appearance.
	predicates map[string]*Predicate

	// layouts are the copybook layouts, one per record, built on demand: a
	// layout is what answers whether an item repeats or sits in a group that
	// does, and building one per reference would build the same one twice.
	layouts map[string]*copybook.Layout
}

// expression compiles one node of the expression.
func (c *compiler) expression(e layoutmodel.Expression) facts {
	switch e.Kind {
	case layoutmodel.RecordName:
		return c.name(e)
	case layoutmodel.Seq:
		return c.sequence(c.subexpressions(e))
	case layoutmodel.Alt:
		return c.alternation(c.subexpressions(e))
	case layoutmodel.ZeroOrMore:
		return c.repeat(c.subexpressions(e), true)
	case layoutmodel.OneOrMore:
		return c.repeat(c.subexpressions(e), false)
	case layoutmodel.Optional:
		return c.optional(c.subexpressions(e))
	case layoutmodel.Times:
		return c.times(e)
	case layoutmodel.When:
		return c.when(e)
	}

	// A value handed back by [layoutmodel.ReadSequence] always names a member
	// of the closed set, so this is a caller that assembled one by hand. It
	// admits nothing, which is the reading that cannot invent a record.
	return facts{}
}

// subexpressions compiles the subexpressions of an operator, in order.
func (c *compiler) subexpressions(e layoutmodel.Expression) []facts {
	parts := make([]facts, 0, len(e.Sub))
	for _, sub := range e.Sub {
		parts = append(parts, c.expression(sub))
	}
	return parts
}

// name compiles a bare record name: one position, admitting one record of that
// type, selected by that record's own discriminator.
func (c *compiler) name(e layoutmodel.Expression) facts {
	at := len(c.positions)

	record, known := c.record(e.Record)
	if !known {
		// [layoutmodel.ReadSequence] has already refused a name no `record`
		// form defines, so this is a caller that assembled a sequence by
		// hand. The appearance is kept with no predicate rather than
		// dropped, so that the shape of the graph is still the shape the
		// expression describes.
		c.positions = append(c.positions, appearance{record: e.Record, pos: e.Pos})

		return facts{first: []entry{{at: at}}, last: []exit{{at: at}}}
	}

	// One appearance of a record name is one position, and every appearance of
	// one record carries the same predicate: the discriminator is a property
	// of the record and not of where it stands in the expression. It is
	// compiled once per record for that reason, and the compiled node shared.
	predicate, already := c.predicates[e.Record]
	if !already {
		predicate = c.discriminator(record)
		c.predicates[e.Record] = predicate
	}

	c.positions = append(c.positions, appearance{record: e.Record, pos: e.Pos, predicate: predicate})

	return facts{first: []entry{{at: at}}, last: []exit{{at: at}}}
}

// sequence compiles `(seq <e> …)`: each in turn, left to right.
//
// It is where the guards a skippable segment leaves behind are composed. A
// subexpression can be started wherever every subexpression ahead of it admits
// nothing, under the guards saying so; it can be ended wherever every one behind
// it does; and one position can be followed by another wherever everything
// between them does.
func (c *compiler) sequence(parts []facts) facts {
	var out facts

	for i := range parts {
		for _, skipped := range emptyWays(parts[:i]) {
			for _, in := range parts[i].first {
				out.first = append(out.first, entry{
					at:      in.at,
					guards:  mergeGuards(skipped, in.guards),
					binding: in.binding,
				})
			}
		}

		for _, skipped := range emptyWays(parts[i+1:]) {
			for _, done := range parts[i].last {
				out.last = append(out.last, exit{at: done.at, guards: mergeGuards(done.guards, skipped)})
			}
		}
	}

	for i := range parts {
		for j := i + 1; j < len(parts); j++ {
			for _, skipped := range emptyWays(parts[i+1 : j]) {
				c.link(parts[i].last, parts[j].first, skipped, nil)
			}
		}
	}

	out.null = emptyWays(parts)

	return out
}

// alternation compiles `(alt <e> …)`: exactly one of them. A state already is a
// disjunction, so nothing is composed and the ways in, the ways out and the ways
// of admitting nothing are the union of the branches'.
func (c *compiler) alternation(parts []facts) facts {
	var out facts

	for _, part := range parts {
		out.first = append(out.first, part.first...)
		out.last = append(out.last, part.last...)
		out.null = append(out.null, part.null...)
	}

	return out
}

// repeat compiles `(* <e>)` and `(+ <e>)`: the body, and a way back from every
// position it can end at to every position it can start at.
//
// The two differ in one thing and it is the one docs/layout/SPEC.md names: an
// empty file is a `*` accepting and a `+` not.
func (c *compiler) repeat(parts []facts, empty bool) facts {
	body := c.sequence(parts)

	c.link(body.last, body.first, nil, nil)

	if empty {
		body.null = [][]Guard{nil}
	}

	return body
}

// optional compiles `(? <e>)`: the body, admitting nothing unconditionally
// besides.
func (c *compiler) optional(parts []facts) facts {
	body := c.sequence(parts)
	body.null = [][]Guard{nil}

	return body
}

// times compiles `(times <e> <item-ref>)`: exactly as many of the body as the
// named item holds.
//
// One register and one guard, whatever the count field's width. The transition
// entering a pass is guarded on the counter being above zero and takes one off
// it, which is both how the run is bounded and how the counter is kept from
// running below zero — the only transition that takes one off is the one that
// guard makes ineligible at zero. The run is left where the counter has reached
// zero, and a count of zero is the whole expression admitting nothing.
func (c *compiler) times(e layoutmodel.Expression) facts {
	body := c.sequence(c.subexpressions(e))
	register := c.register(e, RegisterInteger)

	more := Guard{Register: register, Test: GuardPositive}
	done := Guard{
		Register: register,
		Test:     GuardEquals,
		Values: []Value{number(layoutmodel.Literal{
			Pos:    e.Item.Pos,
			Kind:   layoutmodel.NumberLiteral,
			Number: "0",
		})},
	}
	less := []Binding{{Register: register, Value: BindLessOne}}

	c.link(body.last, body.first, []Guard{more}, less)

	var out facts
	for _, in := range body.first {
		out.first = append(out.first, entry{
			at:      in.at,
			guards:  mergeGuards([]Guard{more}, in.guards),
			binding: append(append([]Binding{}, less...), in.binding...),
		})
	}
	for _, end := range body.last {
		out.last = append(out.last, exit{at: end.at, guards: mergeGuards(end.guards, []Guard{done})})
	}
	out.null = [][]Guard{{done}}

	return out
}

// when compiles `(when <item-ref> <match> <e>)`: the body only where the item
// holds that value.
//
// The guard lands on every way into the body and on nothing else. The path that
// skips the body carries no guard, because the guard set has no negation and
// there is no test for *the item does not hold that value* to put there; the
// package comment above says what that costs and what an adopter writes instead.
func (c *compiler) when(e layoutmodel.Expression) facts {
	body := c.sequence(c.subexpressions(e))
	register := c.register(e, RegisterBytes)

	holds := Guard{Register: register, Test: GuardEquals, Values: c.guardValues(e, register)}
	if e.Match.Kind == layoutmodel.OneOf {
		holds.Test = GuardOneOf
	}

	var out facts
	for _, in := range body.first {
		out.first = append(out.first, entry{
			at:      in.at,
			guards:  mergeGuards([]Guard{holds}, in.guards),
			binding: in.binding,
		})
	}
	out.last = body.last
	out.null = [][]Guard{nil}

	return out
}

// link records that every position from may be followed by every position to,
// under the guards each side carries and the ones between them, applying
// bindings.
func (c *compiler) link(from []exit, to []entry, between []Guard, bindings []Binding) {
	for _, done := range from {
		for _, in := range to {
			c.follow[done.at] = append(c.follow[done.at], entry{
				at:      in.at,
				guards:  mergeGuards(done.guards, between, in.guards),
				binding: append(append([]Binding{}, bindings...), in.binding...),
			})
		}
	}
}

// emptyWays is every way the parts together admit no record at all: the product
// of each part's own ways, and nothing where any part always admits one.
//
// The empty product is one way carrying no guards, which is what makes "nothing
// stands between these two positions" the same expression as "everything between
// them can be skipped".
func emptyWays(parts []facts) [][]Guard {
	ways := [][]Guard{nil}

	for _, part := range parts {
		if len(part.null) == 0 {
			return nil
		}

		var next [][]Guard
		for _, way := range ways {
			for _, empty := range part.null {
				next = append(next, mergeGuards(way, empty))
			}
		}
		ways = next
	}

	return ways
}

// mergeGuards is the conjunction of several guard lists, in order and without
// repeating a test.
func mergeGuards(lists ...[]Guard) []Guard {
	var merged []Guard
	seen := make(map[string]bool)

	for _, list := range lists {
		for _, guard := range list {
			key := guard.key()
			if seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, guard)
		}
	}

	return merged
}

// register is the register one value-reading operator needs, allocating it where
// this is the first operator to need it.
//
// A `times` register is one per operator and a `when` register one per item; the
// package comment above says why the two are keyed differently.
func (c *compiler) register(e layoutmodel.Expression, kind RegisterKind) *Register {
	if kind == RegisterBytes {
		if already, ok := c.byItem[e.Item.String()]; ok {
			return already
		}
	}

	register := &Register{ID: len(c.registers) + 1, Kind: kind, Source: e.Item, Field: c.field(e)}
	c.registers = append(c.registers, register)

	if kind == RegisterBytes {
		c.byItem[e.Item.String()] = register
	}

	return register
}
