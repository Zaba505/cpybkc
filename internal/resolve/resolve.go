// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"errors"

	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// ErrNilRecord is returned by [Resolve] when it is handed no record.
var ErrNilRecord = errors.New("resolve: nil record")

// Options are what resolving a record needs beyond the copybook item itself.
type Options struct {
	// Copybook is the copybook's path as the layout spells it, carried so
	// that a diagnostic can name the file the fault is in. It is not resolved
	// against anything here: where a copybook was looked for is the CLI's,
	// and a diagnostic naming a path the adopter never wrote sends them
	// looking for a file they have not got.
	Copybook string

	// Dialect is the compiler-side half of the layout: the binary width
	// staircase, whether SYNCHRONIZED inserts slack, what an oversized
	// REDEFINES is, and the widths of INDEX and POINTER. It has no default
	// here for the reason `cobol-go` gives it none: a wrong layout setting is
	// not visible in the result.
	Dialect copybook.Dialect

	// Redefines says how each REDEFINES inside a repeating group is told
	// apart. A redefine outside one needs no entry: its alternatives become
	// records, and [Resolve] returns one per combination.
	Redefines []Redefine
}

// Redefine is what a layout says about one REDEFINES inside a repeating group.
//
// It is input rather than something inferred from the copybook because the one
// thing a layout says about a redefine is which alternative to read — never what
// a redefine is, where its bytes are, or which items overlay which. Two or more
// alternatives make a variant. Exactly one says every occurrence takes that
// alternative, and resolves to that alternative's items with no variant emitted
// at all (docs/ir/SPEC.md, "A variant is chosen once per occurrence").
type Redefine struct {
	// Item is the redefined item — the first alternative, the one every
	// REDEFINES of it names. It is a field of the record being resolved.
	Item *copybook.Field

	// Alternatives are the alternatives, in the order they are to be
	// evaluated.
	Alternatives []Alternative
}

// Alternative is one alternative of a [Redefine].
type Alternative struct {
	// Name is the name the copybook gives the item: the redefined item's own
	// name for the first alternative, and a redefining item's for the rest.
	Name string

	// Predicate is the strategy that selects it. It is required where the
	// redefine has two or more alternatives — an arm chosen by nothing is not
	// a thing an alternation can mean — and ignored where it has one, because
	// nothing is being chosen.
	Predicate layoutmodel.Strategy
}

// Resolve resolves record into one [Record] per combination of the alternatives
// its REDEFINES clauses admit outside a repeating group.
//
// record is a field [github.com/Zaba505/cobol-go/copybook.Build] returned: a
// level-01 record or a level-77 standalone item. Widths come from each
// elementary item's PICTURE, USAGE and the dialect, per codec/SPEC.md, "Storage
// Widths", and an item carrying no logical value a generator can use —
// numeric-edited, national, INDEX, POINTER — still carries one, so that the
// position sum stays correct across it.
//
// A record whose copybook holds no REDEFINES outside a repeating group resolves
// to exactly one Record. The order is the order the alternatives are met walking
// the record, outermost and earliest first.
//
// Every fault it finds is reported, joined with [errors.Join] and assertable
// with [errors.As], rather than the first: a copybook and the layout over it go
// wrong in the same way in several places at once. A record it found a fault in
// is not returned — a half-resolved record is one a caller can read, and there
// is no reading of a copybook that was rejected.
func Resolve(record *copybook.Field, opts Options) ([]*Record, error) {
	if record == nil {
		return nil, ErrNilRecord
	}

	layout, err := copybook.NewLayout(record, opts.Dialect)
	if err != nil {
		return nil, err
	}

	r := &resolver{opts: opts, record: record}
	options := r.item(layout.Record)
	if r.faults.Failed() {
		return nil, r.faults.Err()
	}

	records := make([]*Record, 0, len(options))
	for _, option := range options {
		root := option.node
		if root.Kind != KindGroup {
			// A level-77 standalone item is a record whose top level is
			// elementary, and the IR's top level is a group whatever the
			// copybook wrote: it is where the slack goes.
			root = &Node{Kind: KindGroup, Field: record, Members: []*Node{root}}
		}
		records = append(records, &Record{
			Root:         root,
			Item:         record,
			Alternatives: option.alternatives,
		})
	}
	return records, nil
}

// resolver carries what the walk needs: the options it was given, the record it
// is resolving — which every diagnostic names — and the faults found so far.
type resolver struct {
	opts   Options
	record *copybook.Field
	faults diag.List
}

// option is one resolution of one item: the node it became, and the alternatives
// chosen to get there.
//
// An item resolves to more than one option exactly where a REDEFINES outside a
// repeating group stands under it, which is what makes a record's alternatives
// multiply rather than a record carrying a choice.
type option struct {
	node         *Node
	alternatives []*copybook.Field
}

// item resolves one laid-out item into every option it admits.
func (r *resolver) item(item *copybook.Item) []option {
	if item.Field.Kind != copybook.KindGroup {
		return []option{{node: elementary(item)}}
	}
	return r.group(item)
}

// elementary builds the node for an item that holds no others.
//
// A repeating item whose stride exceeds its width has padding between one
// occurrence and the next, and an elementary node has nowhere to put a slack
// node. So it becomes a group that repeats, holding the item and the padding —
// which is the same shape the IR would need for it, since alignment reaches a
// generator as bytes it already has rather than as a rule it applies.
func elementary(item *copybook.Item) *Node {
	field := &Node{
		Kind:       KindField,
		Field:      item.Field,
		width:      item.Length,
		Repetition: repetitionOf(item),
	}
	if item.Stride == item.Length {
		return field
	}

	repetition := field.Repetition
	field.Repetition = nil
	return &Node{
		Kind:       KindGroup,
		Members:    []*Node{field, slackNode(item.Stride - item.Length)},
		Repetition: repetition,
	}
}

// run is what one redefine cluster contributes to its group's member list: the
// nodes themselves, and the alternatives chosen to arrive at them.
//
// A cluster contributes more than one node where the alternative chosen does not
// fill the storage the cluster reserved, which is where the slack behind it goes.
type run struct {
	nodes        []*Node
	alternatives []*copybook.Field
}

// group resolves a group item into every option its member list admits.
//
// The member list is built cluster by cluster — an item and the items redefining
// it are one cluster — and every byte of the group's extent no cluster covers
// becomes slack, so that the members sum to the extent exactly and the position
// of everything behind them is the sum and nothing else.
func (r *resolver) group(item *copybook.Item) []option {
	lists := []run{{}}
	cursor := item.Offset

	for _, c := range clustersOf(item) {
		gap := c.start() - cursor
		choices := r.cluster(c)
		cursor = c.start() + c.extent()

		next := make([]run, 0, len(lists)*len(choices))
		for _, list := range lists {
			for _, choice := range choices {
				members := make([]*Node, 0, len(list.nodes)+len(choice.nodes)+1)
				members = append(members, list.nodes...)
				if gap > 0 {
					members = append(members, slackNode(gap))
				}
				members = append(members, choice.nodes...)

				chosen := make([]*copybook.Field, 0, len(list.alternatives)+len(choice.alternatives))
				chosen = append(chosen, list.alternatives...)
				chosen = append(chosen, choice.alternatives...)

				next = append(next, run{nodes: members, alternatives: chosen})
			}
		}
		lists = next
	}

	// One occurrence of the group is its stride, not its length: a repeating
	// group padded out so that the next occurrence starts on its boundary
	// carries that padding inside itself, where the sum can see it.
	trailing := item.Offset + item.Stride - cursor

	options := make([]option, 0, len(lists))
	for _, list := range lists {
		members := list.nodes
		if trailing > 0 {
			members = append(members[:len(members):len(members)], slackNode(trailing))
		}
		options = append(options, option{
			node: &Node{
				Kind:       KindGroup,
				Field:      item.Field,
				Members:    mergeSlack(members),
				Repetition: repetitionOf(item),
			},
			alternatives: list.alternatives,
		})
	}
	return options
}

// cluster resolves one redefine cluster into every run its group's member list
// admits.
func (r *resolver) cluster(c cluster) []run {
	if len(c.members) == 1 {
		options := r.item(c.members[0])
		runs := make([]run, 0, len(options))
		for _, chosen := range options {
			runs = append(runs, run{nodes: []*Node{chosen.node}, alternatives: chosen.alternatives})
		}
		return runs
	}

	if inTable(c.members[0]) {
		return []run{r.variant(c)}
	}

	// Outside a repeating group every alternative becomes its own record, so
	// the cluster multiplies the options rather than becoming a node.
	var runs []run
	for _, member := range c.members {
		for _, chosen := range r.item(member) {
			alternatives := make([]*copybook.Field, 0, len(chosen.alternatives)+1)
			alternatives = append(alternatives, chosen.alternatives...)
			alternatives = append(alternatives, member.Field)
			runs = append(runs, run{
				nodes:        pad(chosen.node, c.extent()),
				alternatives: alternatives,
			})
		}
	}
	return runs
}

// variant resolves a redefine cluster inside a repeating group.
//
// The layout says which alternatives there are and what selects each one. Two or
// more make a variant; one says every occurrence takes it, and resolves to that
// alternative's items with no variant at all.
func (r *resolver) variant(c cluster) run {
	redefined := c.members[0]
	table := enclosingTable(redefined)

	spec := r.redefine(redefined.Field)
	if spec == nil {
		r.faults.Fail(&UndiscriminatedRedefineError{
			Pos:       r.span(redefined.Field),
			Record:    r.record.Name,
			Group:     groupName(table),
			Redefined: itemName(redefined.Field),
			Names:     names(c.members[1:]),
		})
		return r.base(c)
	}

	// The redefined item's storage is what an occurrence reserved, and it is
	// what every arm has to fit in. A dialect lenient enough to let a
	// redefinition grow its group cannot make that true one entry at a time,
	// so an arm needing more bytes is the layout this package rejects rather
	// than one it pads.
	extent := redefined.Total()

	arms := make([]Arm, 0, len(spec.Alternatives))
	for _, alternative := range spec.Alternatives {
		member := c.find(alternative.Name)
		if member == nil {
			r.faults.Fail(&UnknownAlternativeError{
				Pos:         r.span(redefined.Field),
				Record:      r.record.Name,
				Redefined:   itemName(redefined.Field),
				Alternative: alternative.Name,
				Names:       names(c.members),
			})
			continue
		}

		switch counted := referenceCount(member); {
		case member.Total() > extent:
			r.faults.Fail(&ArmExtentError{
				Pos:       r.span(member.Field),
				Record:    r.record.Name,
				Group:     groupName(table),
				Redefined: itemName(redefined.Field),
				Arm:       itemName(member.Field),
				Extent:    member.Total(),
				Want:      extent,
			})
			continue

		case counted != nil:
			r.faults.Fail(&ArmVariableCountError{
				Pos:       r.span(counted.Field),
				Record:    r.record.Name,
				Group:     groupName(table),
				Redefined: itemName(redefined.Field),
				Arm:       itemName(member.Field),
				Item:      itemName(counted.Field),
				Count:     itemName(counted.DependingOn.Field),
			})
			continue

		case len(spec.Alternatives) > 1 && !alternative.Predicate.Predicate():
			r.faults.Fail(&ArmPredicateError{
				Pos:       r.span(member.Field),
				Record:    r.record.Name,
				Group:     groupName(table),
				Redefined: itemName(redefined.Field),
				Arm:       itemName(member.Field),
			})
			continue
		}

		arms = append(arms, Arm{
			Alternative: alternative.Name,
			Predicate:   alternative.Predicate,
			Body:        armBody(r.first(member), extent),
		})
	}

	switch {
	case len(spec.Alternatives) == 0:
		r.faults.Fail(&ArmCountError{
			Pos:       r.span(redefined.Field),
			Record:    r.record.Name,
			Group:     groupName(table),
			Redefined: itemName(redefined.Field),
		})
		return r.base(c)

	case len(spec.Alternatives) == 1:
		// Nothing is chosen, so there is no alternation to carry and no
		// predicate to carry it under: the one alternative's items stand
		// where the cluster stood, padded out like a record's.
		if len(arms) == 0 {
			return r.base(c)
		}
		member := c.find(spec.Alternatives[0].Name)
		return run{
			nodes:        pad(r.first(member), c.extent()),
			alternatives: []*copybook.Field{member.Field},
		}
	}

	if len(arms) < len(spec.Alternatives) {
		// A fault was already reported for the arms that are missing, and a
		// variant short of them would be a second, less useful one.
		return r.base(c)
	}

	return run{nodes: pad(&Node{Kind: KindVariant, Arms: arms}, c.extent())}
}

// base is the run a cluster contributes when a fault stopped it from resolving:
// the redefined item, which is the copybook's own first description of those
// bytes. It keeps the walk going so that a second fault is reported beside the
// first, and nothing is returned to a caller while a fault stands.
func (r *resolver) base(c cluster) run {
	return run{nodes: pad(r.first(c.members[0]), c.extent())}
}

// first resolves an item to its one option.
//
// Inside a repeating group there is only ever one: a redefine down there is
// itself in a repeating group, so it becomes a variant rather than multiplying
// the record's alternatives.
func (r *resolver) first(item *copybook.Item) *Node {
	options := r.item(item)
	if len(options) == 0 {
		return &Node{Kind: KindGroup, Field: item.Field}
	}
	return options[0].node
}

// redefine finds the layout's word on a redefined item.
func (r *resolver) redefine(field *copybook.Field) *Redefine {
	for i := range r.opts.Redefines {
		if r.opts.Redefines[i].Item == field {
			return &r.opts.Redefines[i]
		}
	}
	return nil
}

// span is where in the copybook a field is.
func (r *resolver) span(field *copybook.Field) diag.Span {
	return diag.Span{File: r.opts.Copybook, Line: field.Pos.Line, Column: field.Pos.Column}
}

// pad returns the nodes a member list gets for node, followed by the slack
// covering the bytes of extent it does not occupy.
//
// This is the record-level half of resolving a redefine away: the alternative's
// items stand where the cluster stood, and the storage the copybook gave the
// alternatives jointly that this one does not use is slack. Those bytes are not
// padding — in a file written by a program that used another alternative they
// hold that alternative's data.
func pad(node *Node, extent int) []*Node {
	if gap := extent - node.Extent(); gap > 0 {
		return []*Node{node, slackNode(gap)}
	}
	return []*Node{node}
}

// armBody builds an arm's body, padded to the variant's extent.
//
// The slack has to sit inside the arm rather than behind it: a run of uncovered
// bytes stops at the edge of an arm, and one node covering both would belong to
// neither list and would make the arm wider than its siblings. A group that does
// not repeat can hold it among its own members; anything else — an elementary
// alternative above all — is wrapped in a group that carries no names, because
// the alternative's name is on the node inside it.
func armBody(node *Node, extent int) *Node {
	gap := extent - node.Extent()
	if gap <= 0 {
		return node
	}
	if node.Kind == KindGroup && node.Repetition == nil {
		node.Members = mergeSlack(append(node.Members, slackNode(gap)))
		return node
	}
	return &Node{Kind: KindGroup, Members: []*Node{node, slackNode(gap)}}
}

// mergeSlack collapses each run of abutting slack nodes into one, so that two
// runs of different origin that abut are one node and not two.
//
// It costs nothing today and stops costing something later: a record carries one
// run of bytes per slack node, so how a producer divides a run of eight into
// nodes would otherwise decide the shape of every record a generator emits.
func mergeSlack(nodes []*Node) []*Node {
	merged := make([]*Node, 0, len(nodes))
	for _, node := range nodes {
		last := len(merged) - 1
		if node.Kind == KindSlack && last >= 0 && merged[last].Kind == KindSlack {
			merged[last] = slackNode(merged[last].width + node.width)
			continue
		}
		merged = append(merged, node)
	}
	return merged
}

// repetitionOf reads an item's repetition, nil where it does not repeat.
func repetitionOf(item *copybook.Item) *Repetition {
	if item.MaxOccurs <= 1 {
		return nil
	}

	repetition := &Repetition{Count: item.Occurs, Min: item.MinOccurs, Max: item.MaxOccurs}
	if item.DependingOn != nil {
		repetition.DependingOn = item.DependingOn.Field
	}
	return repetition
}

// referenceCount returns the first item at or under item whose repetition count
// is read out of the record, or nil where none is.
func referenceCount(item *copybook.Item) *copybook.Item {
	if item.DependingOn != nil {
		return item
	}
	for _, child := range item.Children {
		if found := referenceCount(child); found != nil {
			return found
		}
	}
	return nil
}

// inTable reports whether item is contained, at any depth, in a group that
// repeats. It is the whole of what decides which resolution a REDEFINES takes.
func inTable(item *copybook.Item) bool { return enclosingTable(item) != nil }

// enclosingTable returns the innermost group above item that repeats, or nil
// where none does.
func enclosingTable(item *copybook.Item) *copybook.Item {
	for parent := item.Parent; parent != nil; parent = parent.Parent {
		if parent.MaxOccurs > 1 {
			return parent
		}
	}
	return nil
}

// cluster is an item and the items redefining it: one run of storage with more
// than one description of it.
type cluster struct {
	members []*copybook.Item
}

// start is the byte the cluster's storage begins at. Every member of a cluster
// starts there — that is what REDEFINES means.
func (c cluster) start() int { return c.members[0].Offset }

// extent is the bytes the cluster's storage covers: the longest description of
// it, which under a strict dialect is always the redefined item's own.
func (c cluster) extent() int {
	widest := 0
	for _, member := range c.members {
		if total := member.Total(); total > widest {
			widest = total
		}
	}
	return widest
}

// find returns the member named name, or nil.
func (c cluster) find(name string) *copybook.Item {
	for _, member := range c.members {
		if !member.Field.Filler && member.Field.Name == name {
			return member
		}
	}
	return nil
}

// clustersOf partitions a group's children into clusters, in the order the
// redefined items appear.
//
// A REDEFINES names a preceding sibling, and a chain of them — B redefining A
// and C redefining B — describes one run of storage three ways, so the chain is
// walked to its root rather than each link being its own cluster.
func clustersOf(item *copybook.Item) []cluster {
	var clusters []cluster
	index := make(map[*copybook.Item]int, len(item.Children))

	for _, child := range item.Children {
		base := child
		for base.Redefines != nil {
			base = base.Redefines
		}
		if at, ok := index[base]; ok {
			clusters[at].members = append(clusters[at].members, child)
			continue
		}
		index[base] = len(clusters)
		clusters = append(clusters, cluster{members: []*copybook.Item{child}})
	}
	return clusters
}

// itemName is what a diagnostic calls a field: its name, or FILLER where it has
// none.
func itemName(field *copybook.Field) string {
	if field == nil {
		return ""
	}
	if field.Filler || field.Name == "" {
		return "FILLER"
	}
	return field.Name
}

// groupName is what a diagnostic calls the repeating group a variant sits in.
func groupName(item *copybook.Item) string {
	if item == nil {
		return ""
	}
	return itemName(item.Field)
}

// names is what the items are called, for a diagnostic listing the alternatives
// an adopter could have meant.
func names(items []*copybook.Item) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, itemName(item.Field))
	}
	return out
}
