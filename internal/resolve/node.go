// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// Kind is what a resolved node is, one member of the closed set docs/ir/SPEC.md,
// "The node kinds", admits inside a record.
//
// The kinds a record cannot contain — file, record, predicate, state,
// transition, register, binding, guard — are not here. This is what is inside a
// record; the automaton's own nodes are [State], [Transition] and [Register],
// and assembling every kind into one descriptor is #38's.
type Kind int

const (
	// KindGroup is an item holding other items. It carries no width of its
	// own: [Node.Width] answers from the member list.
	KindGroup Kind = iota + 1

	// KindField is an elementary item, carrying the width one occurrence of
	// it occupies.
	KindField

	// KindVariant is an alternation over one run of bytes inside a table:
	// what a REDEFINES inside a repeating group resolves to.
	KindVariant

	// KindSlack is bytes that are part of the record and belong to no item.
	KindSlack
)

// String implements the [fmt.Stringer] interface.
func (k Kind) String() string {
	switch k {
	case KindGroup:
		return "group"
	case KindField:
		return "field"
	case KindVariant:
		return "variant"
	case KindSlack:
		return "slack"
	}
	return "unknown"
}

// Node is one item of a resolved record.
//
// It is one type over four kinds rather than four types behind an interface,
// because the IR is a flat list of nodes over a closed set of bodies and a
// consumer switches over that set. What a kind does not carry is nil or zero
// here for the same reason it is absent there: a slack node has no copybook
// item behind it, a variant has no names and no repetition, and a field has no
// members.
//
// No node carries a byte offset. Position is stated once, as ordering and
// width, and [Record.Position] is the sum that reads it back
// (docs/ir/SPEC.md, "Ordering and width, and no offset").
type Node struct {
	// Kind is which of the four this is.
	Kind Kind

	// Field is the copybook item the node stands for, and is nil for a slack
	// node, for a variant, and for a group this package introduced to hold a
	// node beside the slack that pads it out. A nil Field is a node with no
	// names, which is what the IR carries for one.
	Field *copybook.Field

	// Members are a group's members, in record order — the order in which
	// they occupy bytes. It is empty for every other kind.
	Members []*Node

	// Arms are a variant's alternatives, in evaluation order. Every arm
	// begins at the variant's first byte, so the order says nothing about
	// position. It is empty for every other kind.
	Arms []Arm

	// Repetition is what an item that repeats carries, and is nil for one
	// that does not. A variant never carries one: it repeats by sitting
	// inside the group that does, which is the only place it may sit.
	Repetition *Repetition

	// Encoding is the four encoding axes governing this field's bytes: the
	// layout's profile with every override reaching this item applied over
	// it. All four are stated on a field node this package hands back.
	//
	// The fifth thing a generator needs to read a field's bytes is its
	// USAGE, and that is not here: it is a clause of the copybook, already
	// inherited down the entry tree by
	// [github.com/Zaba505/cobol-go/copybook.Field.Usage], and a copy on the
	// node would be a second answer to a question the copybook has already
	// answered. The axes are here because no copybook states them.
	//
	// It is carried by a field node and by no other kind. docs/ir/SPEC.md's
	// "The encoding profile, applied" puts all four on every field node and
	// carries no profile node for one to inherit from, so a group holding a
	// copy would be that profile under another name — a second statement of
	// an axis, for a member to disagree with. A slack node has no encoding
	// for the reason it has no names: nothing reads those bytes as a value.
	Encoding layoutmodel.Axes

	// width is the bytes one occurrence of a field or a slack node occupies.
	// It is unexported because a group and a variant must not be able to
	// carry one: their widths follow from what they hold, and a member
	// stating a second answer is the failure the IR is shaped to make
	// unrepresentable.
	width int
}

// Width reports the bytes one occurrence of the node occupies.
//
// A field's and a slack node's is carried. A group's is the sum of its members'
// extents and a variant's is its arms' common extent, and neither is stored:
// docs/ir/SPEC.md declines to carry either, and a method is how that refusal
// survives contact with a caller.
func (n *Node) Width() int {
	switch n.Kind {
	case KindGroup:
		total := 0
		for _, member := range n.Members {
			total += member.Extent()
		}
		return total
	case KindVariant:
		if len(n.Arms) == 0 {
			return 0
		}
		return n.Arms[0].Body.Extent()
	}
	return n.width
}

// Occurrences reports how many times the node repeats: one where it carries no
// repetition, and the count the layout was computed at where it does.
func (n *Node) Occurrences() int {
	if n.Repetition == nil {
		return 1
	}
	return n.Repetition.Count
}

// Extent reports the bytes the node occupies in the member list holding it:
// [Node.Width] times [Node.Occurrences].
//
// This is what enters the position sum, so a node ahead of another contributes
// its whole extent and not one occurrence of it.
func (n *Node) Extent() int { return n.Width() * n.Occurrences() }

// Repetition is what an item that repeats carries: a constant number of
// occurrences, or a reference to the field a count is read from together with
// the bounds the copybook declared (docs/ir/SPEC.md, "A variable record is a sum
// with a variable term").
//
// A repetition whose count is a reference is what the *sliding* reading of
// `OCCURS DEPENDING ON` resolves to. Under the other reading the same clause is
// a fixed table at its declared maximum, and what stands here is a constant with
// no reference in it at all — see [Options.Reading] (#87).
type Repetition struct {
	// Count is the occurrence count this repetition stands at: the constant
	// the copybook wrote for a fixed table, the declared maximum for a
	// reference as [Resolve] hands it back — the storage a compiler reserves
	// — and the count a consumer decoded after [Record.At].
	Count int

	// Min and Max are the bounds the OCCURS clause allows, and they both
	// equal Count for a fixed table.
	//
	// They are the copybook's own `OCCURS integer-1 TO integer-2`, neither
	// narrowed nor widened to what a layout would prefer, and they are
	// carried for one check and nothing else: a count outside them is
	// malformed data, which is [Record.At]'s to report. Without them there is
	// nothing in a record to bound a number decoded out of a file.
	Min int
	Max int

	// DependingOn is the field whose value gives the count, nil for a fixed
	// table.
	//
	// A field of the record being read, at any depth, and never a field of
	// another record: a DEPENDING ON phrase names an item of the copybook
	// record carrying it, and a count that comes from an earlier record
	// arrives as a [Register] the automaton bound rather than as a
	// reference reaching across records (docs/ir/SPEC.md, #77). Which
	// transitions bind that register, and the proof that one is bound
	// before it is read, are [CompileSequence]'s.
	DependingOn *copybook.Field
}

// Reference reports whether the count is read out of the record rather than
// written in the copybook.
func (r *Repetition) Reference() bool { return r != nil && r.DependingOn != nil }

// Arm is one alternative of a variant: the predicate that selects it and the
// node that is its body.
//
// A pair and not a node of its own, because nothing points at an arm and a kind
// for it would be one more member a consumer switches over in order to reach two
// references (docs/ir/SPEC.md, "A variant is chosen once per occurrence").
type Arm struct {
	// Alternative is the name the copybook gives the item this arm selects,
	// which is what a layout writes in an `arm` form.
	Alternative string

	// Predicate is the predicate that selects this arm, and is never nil.
	//
	// The same node kind and the same closed set of tests a transition's
	// predicate is, and bound by different rules: its target sits inside the
	// occurrence being walked rather than at a constant position in the
	// record, because it is evaluated once the record has been admitted
	// (docs/ir/SPEC.md, "A predicate on an arm reads one occurrence", #90).
	//
	// An arm carries exactly one, and there is no default arm: an alternative
	// selected by nothing is a choice a consumer cannot make.
	Predicate *Predicate

	// Body is the arm's body, a group or a field node. Where the alternative
	// occupies fewer bytes than the variant's extent, it is a group holding
	// the alternative and the slack that pads it out, because the slack has
	// to sit inside the arm and an elementary item has nowhere to put it.
	Body *Node
}

// Record is one resolved alternative of a copybook record.
//
// A copybook record holding no REDEFINES outside a repeating group resolves to
// exactly one of these. One holding some resolves to one per combination of
// alternatives, each carrying the whole record with that combination's items in
// place and slack over the bytes they do not occupy.
type Record struct {
	// Root is the record's top level, always a group node.
	//
	// A group rather than either kind of item, because the top level is where
	// the slack that resolving REDEFINES away leaves behind has to go, and
	// holding members is what a group is.
	Root *Node

	// Item is the copybook record this is an alternative of.
	Item *copybook.Field

	// Alternatives are the items chosen at each redefine outside a repeating
	// group, in the order the choices are met walking the record. It is empty
	// for a record whose copybook holds none, and it is what tells two
	// alternatives of one copybook record apart — the IR carries the
	// copybook's name, which is the same on both.
	Alternatives []*copybook.Field

	// Binary is the width staircase every COMP, COMP-4 and COMP-5 item below
	// [Record.Root] was laid out under: [Options.Dialect]'s, copied here by
	// [Resolve].
	//
	// It travels with the record because it is not a fact about the record's
	// *shape* that a reader could recover from the shape. Every width and
	// every offset in the tree was computed under it, and a consumer laying
	// bytes out under a different staircase gets a record that is the right
	// length and wrong from the first binary item onwards — which is not
	// something the bytes can be asked about. Carrying it beside the widths it
	// produced is what lets `assemble` state it in the IR without asking a
	// second time where the dialect came from, and what makes the two
	// answers impossible to give differently.
	//
	// It is `copybook`'s enum rather than this package's: the staircase is
	// `copybook`'s decision here — [copybook.NewLayout] is what applied it —
	// and a second spelling of it in this package would be a second place for
	// the mapping onto `codec`'s to go wrong.
	Binary copybook.BinarySize
}

// Extent reports the record's length in bytes: the width of its top level.
//
// A record node carries no length in the IR, for the reason a group carries no
// width; this is the sum, run here.
func (r *Record) Extent() int { return r.Root.Width() }

// Position reports the byte position of target within the record, measured from
// the first byte of the record's data: the sum of the extents of everything
// ahead of it in containment order, counting exactly one occurrence of every
// group that encloses it and repeats.
//
// The occurrence it lands on is the first. Saying so costs nothing today —
// docs/ir/SPEC.md's "A reference names a field, not an occurrence of one"
// forbids every reference that could reach a later one — and it is what makes
// the sum the same arithmetic before and after that prohibition is relaxed.
//
// A variant contributes its arms' common extent like any other constant term,
// so an item behind one sits where it would sit without it, and a position
// inside an arm is measured through the arm the item is in.
//
// It reports false for a node that is not in the record.
//
// A record holding a table whose count is a reference has positions behind that
// table which move with the count, and this is the sum at the count every
// repetition currently stands at — the declared maximum, as [Resolve] hands a
// record back, and one record's own counts after [Record.At]. That is the whole
// of how a data-dependent offset is modelled: ordering and width, with a term in
// the sum that is not a constant. Nothing carries a second, different mechanism
// for a field behind a variable table, and no node carries an offset for one to
// be wrong in (docs/ir/SPEC.md, "A variable record is a sum with a variable
// term", #35).
func (r *Record) Position(target *Node) (int, bool) {
	return position(r.Root, target, 0)
}

// position walks node looking for target, carrying the bytes ahead of node in
// at. It is a walk rather than a parent chain because containment is stated once
// and downward, exactly as a consumer of the IR has it.
func position(node, target *Node, at int) (int, bool) {
	if node == target {
		return at, true
	}

	switch node.Kind {
	case KindGroup:
		for _, member := range node.Members {
			if found, ok := position(member, target, at); ok {
				return found, true
			}
			at += member.Extent()
		}
	case KindVariant:
		// Every arm begins at the variant's first byte, so an arm
		// contributes nothing to the sum and the arms are searched at the
		// variant's own position rather than one behind another.
		for _, arm := range node.Arms {
			if found, ok := position(arm.Body, target, at); ok {
				return found, true
			}
		}
	}
	return 0, false
}

// Walk calls fn for every node of the record in containment order, outermost
// first, the top level included. A variant's arms are walked in evaluation
// order.
func (r *Record) Walk(fn func(*Node)) { walk(r.Root, fn) }

func walk(node *Node, fn func(*Node)) {
	fn(node)
	for _, member := range node.Members {
		walk(member, fn)
	}
	for _, arm := range node.Arms {
		walk(arm.Body, fn)
	}
}

// Find returns the first node standing for the copybook item named name, in
// [Record.Walk] order, or nil where the record holds none.
//
// A name is not identity — duplicate data names are legal COBOL — so this is for
// a caller that already knows the record it is looking in holds one, which in
// practice means a test.
func (r *Record) Find(name string) *Node {
	var found *Node
	r.Walk(func(node *Node) {
		if found != nil || node.Field == nil || node.Field.Filler {
			return
		}
		if node.Field.Name == name {
			found = node
		}
	})
	return found
}

// slackNode builds a slack node of width bytes.
func slackNode(width int) *Node { return &Node{Kind: KindSlack, width: width} }
