// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Zaba505/cpybkc/irpb"
)

// span is a number of bytes as this document states it: the constant part the
// descriptor states, and one term per table ahead of it whose number of
// occurrences is read at run time.
//
// docs/ir/SPEC.md's "Ordering and width, and no offset" is why an offset is a
// sum rather than a value read off a node. No node carries one, deliberately —
// "an offset field is a fact stated twice, and a producer that gets it wrong is
// wrong in a way no consumer can detect" — and the cost that section names is
// that "every consumer is free to get it wrong on its own". This generator is
// now one of those consumers, and this type is where its arithmetic lives.
//
// It is cmd/cpybkc-gen-go/extent.go's `extent` with the Go expressions replaced
// by item names, because the two are computing the same sum for two different
// readers. Ported rather than shared: that generator emits a Go expression a
// compiler will read, this one emits a phrase a person will read, and a package
// they both imported would be a convenience no third-party generator has.
type span struct {
	// fixed is the constant part, in bytes.
	fixed uint64

	// terms are the parts that are not, in the order the containment order
	// produced them.
	terms []term
}

// term is one data-dependent part of a span: a constant number of bytes, taken
// once per occurrence of each table whose count is read at run time.
//
// A product rather than a string, because the factors are copybook names and a
// name is escaped by whichever notation is about to print it — the same split
// [edge.label] makes. A list of factors rather than one, because a table whose
// count is data may enclose another: docs/ir/SPEC.md forbids a count reference
// that names a field inside a repeating group, which keeps each count constant
// across the table it sizes, and forbids nothing about nesting the tables
// themselves.
type term struct {
	// bytes is the constant factor: one occurrence's width, in bytes.
	bytes uint64

	// by are the counts it is multiplied by, outermost first. Each is a field's
	// path within the record or a register's name.
	by []string
}

// fixedSpan is a span of a constant number of bytes.
func fixedSpan(n uint64) span { return span{fixed: n} }

// add is the two spans summed.
func (s span) add(t span) span {
	return span{fixed: s.fixed + t.fixed, terms: append(append([]term{}, s.terms...), t.terms...)}
}

// times is the span taken once per occurrence of a table whose count is data.
//
// The constant part becomes a term of its own, because a constant multiplied by
// something that is not one is not a constant — which is the whole of what
// docs/ir/SPEC.md's "A variable record is a sum with a variable term" says
// happens to every offset behind such a table.
func (s span) times(count string) span {
	var out span

	if s.fixed != 0 {
		out.terms = append(out.terms, term{bytes: s.fixed, by: []string{count}})
	}

	for _, one := range s.terms {
		// The factor list is copied rather than appended to in place: two terms
		// of one span share no storage, and a plain append would let the second
		// one's count land in the first one's product.
		out.terms = append(out.terms, term{bytes: one.bytes, by: append(append([]string{}, one.by...), count)})
	}

	return out
}

// repeated is the span taken a constant number of times.
func (s span) repeated(n uint64) span {
	out := span{fixed: s.fixed * n}

	for _, one := range s.terms {
		// A term multiplied out to nothing is dropped rather than printed as
		// `0 × DTL-COUNT`, which is a product a reader would stop to work out
		// and which contributes nothing to the sum either way.
		if one.bytes*n == 0 {
			continue
		}

		out.terms = append(out.terms, term{bytes: one.bytes * n, by: one.by})
	}

	return out
}

// phrase is the span as a cell of the table states it.
//
// A bare number where the sum is constant, which is every offset in a record
// holding no variable table — and a sum where it is not, so that a reader can
// see at a glance which of the two they are looking at.
func (s span) phrase(esc func(string) string) string {
	if len(s.terms) == 0 {
		return strconv.FormatUint(s.fixed, 10)
	}

	printed := make([]string, 0, len(s.terms)+1)

	// The constant part first and dropped where it is zero, so that an item at
	// the very start of a variable table reads `4 × DTL-COUNT` rather than
	// `0 + 4 × DTL-COUNT`.
	if s.fixed != 0 {
		printed = append(printed, strconv.FormatUint(s.fixed, 10))
	}

	for _, one := range s.terms {
		printed = append(printed, one.phrase(esc))
	}

	return strings.Join(printed, " + ")
}

// phrase is one term as a cell states it.
//
// `×` rather than `*`, because a cell's contents are inline Markdown and `*`
// opens emphasis there — the same reason [markdownCell] escapes one in a name.
// A multiplication sign needs no escape in any notation this generator writes
// and cannot be mistaken for anything else.
func (t term) phrase(esc func(string) string) string {
	printed := make([]string, 0, len(t.by)+1)

	// A width of one byte is left out of the product: `DTL-COUNT` says what
	// `1 × DTL-COUNT` says, in a cell a reader is scanning rather than reading.
	if t.bytes != 1 {
		printed = append(printed, strconv.FormatUint(t.bytes, 10))
	}

	for _, one := range t.by {
		printed = append(printed, esc(one))
	}

	return strings.Join(printed, " × ")
}

// recordTable is one record's items, as the document tables them.
type recordTable struct {
	// name is the record, named as the diagram's edges name it.
	name string

	// items are the rows, in containment order.
	items []item
}

// item is one row: where it begins, how wide it is, what it is called, and what
// the descriptor says about its contents.
type item struct {
	// path is the item's path within the record, dotted by an emitter and
	// without the record's own top level — the same convention a predicate's
	// field takes in an edge label, and for the same reason: a reader already
	// knows which record the table is about.
	//
	// The last element is the item itself and the ones ahead of it are what
	// contains it, so an unnamed *group* nests its members beneath its own word
	// rather than beside them. Getting that wrong is invisible in a leaf and
	// wrong in a way a reader would act on in a group: a member of a FILLER
	// group would read as a member of the record.
	path []element

	// at is where the item begins, measured from the first byte of the record's
	// data.
	at span

	// extent is its whole width, every occurrence included.
	extent span

	// usage and picture are what the descriptor says about an elementary item's
	// contents, and [notAnItem] on a row that is not one.
	usage, picture string

	// present is what makes the item present, and how many times.
	present presence
}

// element is one step of an item's path: a name the copybook spells, or a word
// this generator supplies for a node the copybook did not name.
//
// Which of the two it is has to travel with it, because the escaping does. A
// name is somebody's and is escaped by whichever notation is about to print it;
// a word is this generator's and is written past the escaping, emphasised, so a
// reader can tell the descriptor's vocabulary from this program's.
//
// Three nodes take a word. A slack node and a variant node carry no names at all
// in the schema; a group or a field carries none where COBOL gave the item no
// data-name, which is a FILLER.
type element struct {
	// name is the copybook's spelling, or this generator's word.
	name string

	// supplied is whether the name is this generator's rather than the
	// copybook's.
	supplied bool
}

// named and supplied are the two kinds of path element.
func named(name string) element { return element{name: name} }

func supplied(word string) element { return element{name: word, supplied: true} }

// The words standing where an unnamed node's name would be.
const (
	anonymousSlack   = "slack"
	anonymousVariant = "variant"
	anonymousFiller  = "filler"
)

// notAnItem is what the usage and picture cells hold on a row that is not an
// elementary item.
//
// A dash rather than an empty cell, for the reason [mermaidBinders] writes
// "nothing" rather than leaving one blank: a group, a slack run and a variant
// each carry neither a USAGE nor a picture, and a blank reads as a table this
// generator failed to fill in.
const notAnItem = "—"

// presence is a row's last column: what makes the item present, and how many
// times it is.
//
// One column for two facts because they are one question asked of two kinds of
// row. An item that repeats is present as many times as its count says; an arm
// of a variant is present when its predicate holds and not otherwise. A row that
// is neither is present once, always, and says so rather than leaving a cell
// blank.
type presence struct {
	// chosen is the predicate selecting this row where it is an arm of a
	// variant, and carries nothing where it is not.
	chosen predicate

	// repeats is whether the item repeats at all.
	repeats bool

	// constant is the number of occurrences where the count is one.
	constant uint32

	// by is where the count is read from where it is not: a field's path within
	// the record, or a register's name.
	by string

	// min and max are the bounds a variable count carries, which
	// docs/ir/SPEC.md carries "for that check and for nothing else" — a count
	// outside them is malformed data. They are printed because a reader
	// checking a layout against a copybook is checking the OCCURS clause it was
	// written from.
	min, max uint32
}

// always is what the presence column says of a row that is present once and
// unconditionally, which is most of them.
const always = "always"

// times is the noun after a constant count, in the number that count is.
func times(n uint32) string {
	if n == 1 {
		return "time"
	}

	return "times"
}

// phrase is the presence as a cell states it.
func (p presence) phrase(esc func(string) string) string {
	var said []string

	if p.chosen.carried {
		said = append(said, p.chosen.phrase(esc))
	}

	switch {
	case !p.repeats:
	case p.by == "":
		// The singular where the count is one. `OCCURS 1` is legal and rare, and
		// "occurs 1 times" in a column a reader is scanning reads as a generator
		// that did not consider the case — which invites them to distrust the
		// numbers beside it, which are the whole point of the table.
		said = append(said, fmt.Sprintf("occurs %d %s", p.constant, times(p.constant)))
	default:
		// The bounds beside the count, because they are the copybook's own
		// `OCCURS integer-1 TO integer-2` and a reader holding that copybook is
		// checking exactly those two numbers.
		said = append(said, fmt.Sprintf("occurs %s times (%d to %d)", esc(p.by), p.min, p.max))
	}

	if len(said) == 0 {
		return always
	}

	return strings.Join(said, ", ")
}

// readRecords is one table per record the automaton admits, in the order the
// diagram above first admits it.
//
// Per record rather than per transition. A record admitted from three states is
// the same bytes each time — docs/ir/SPEC.md states position once, as ordering
// and width, and nothing about a transition changes it — so three tables would
// be three copies of one fact, and a reader comparing two of them would be
// looking for a difference that cannot exist.
//
// The walk order is the diagram's, so the tables read in the order a file does:
// the record the first edge admits, then what may follow it. Any total order
// would be deterministic, which is what docs/plugin/SPEC.md's "Determinism"
// asks; this one is also the order somebody traces.
func (g *graph) readRecords(nodes nodeSet) error {
	tabled := map[uint64]bool{}

	for _, s := range g.states {
		for _, e := range s.edges {
			if tabled[e.recordID] {
				continue
			}

			tabled[e.recordID] = true

			table, err := recordItems(nodes, e.recordID, e.record)
			if err != nil {
				return err
			}

			g.records = append(g.records, table)
		}
	}

	return nil
}

// recordItems is one record's rows, in containment order.
func recordItems(nodes nodeSet, id uint64, name string) (recordTable, error) {
	record, ok := nodes.record(id)
	if !ok {
		return recordTable{}, unresolved(id, "a record a transition admits")
	}

	if _, ok := nodes.group(record.GetRootId()); !ok {
		return recordTable{}, unresolved(record.GetRootId(), "the top level of a record a transition admits")
	}

	w := &itemWalk{nodes: nodes, record: record, open: map[uint64]bool{record.GetRootId(): true}}

	// The top level's own row is not drawn, and its name is not a path element.
	// The heading above the table is the record, so a row for it would say the
	// record's name twice and put every item one level deeper than the copybook
	// writes it.
	if _, err := w.members(record.GetRootId(), nil, span{}); err != nil {
		return recordTable{}, err
	}

	return recordTable{name: name, items: w.rows}, nil
}

// itemWalk is one record's containment order being read into rows.
type itemWalk struct {
	nodes  nodeSet
	record *irpb.Record

	// rows are the rows so far, in the order the table prints them.
	rows []item

	// open are the group and variant nodes this walk is currently inside.
	//
	// A member list states containment downward, so a node that contains itself
	// at any depth is a descriptor no producer may emit — and a walk that met
	// one would recurse until the stack was gone, which is not a failure any
	// diagnostic of this program composes. Entries are cleared on the way out,
	// so a node legitimately reached twice along two different paths is walked
	// twice and only a genuine cycle is refused.
	open map[uint64]bool
}

// members appends the rows for every member of the group id and answers the sum
// of their widths, which is the group's own width for one occurrence.
func (w *itemWalk) members(id uint64, path []element, at span) (span, error) {
	group, ok := w.nodes.group(id)
	if !ok {
		return span{}, unresolved(id, "a group in the containment order of a record a transition admits")
	}

	var total span

	for _, memberID := range group.GetMemberIds() {
		// Each member begins where everything ahead of it ends, which is
		// docs/ir/SPEC.md's "Ordering and width, and no offset" performed rather
		// than quoted.
		width, err := w.member(memberID, path, at.add(total), presence{})
		if err != nil {
			return span{}, err
		}

		total = total.add(width)
	}

	return total, nil
}

// member appends the rows for one member of a containment order and answers its
// whole width, occurrences and all.
//
// chosen is the presence the row starts from: empty for an ordinary member, and
// carrying the selecting predicate where the member is the body of an arm.
func (w *itemWalk) member(id uint64, path []element, at span, chosen presence) (span, error) {
	node, ok := w.nodes.by[id]
	if !ok {
		return span{}, unresolved(id, "a member of the containment order of a record a transition admits")
	}

	switch kind := node.GetKind().(type) {
	case *irpb.Node_Slack:
		return w.slack(kind.Slack, path, at, chosen), nil
	case *irpb.Node_Field:
		return w.field(id, kind.Field, path, at, chosen)
	case *irpb.Node_Group:
		return w.group(id, kind.Group, path, at, chosen)
	case *irpb.Node_Variant:
		return w.variant(id, kind.Variant, path, at, chosen)
	default:
		return span{}, malformed(fmt.Sprintf("node %d is not something a group may contain", id),
			"a member list names a group, variant, field or slack node; see docs/ir/SPEC.md, \"The node kinds\"")
	}
}

// slack is one run of bytes that belongs to no item.
//
// Drawn as a row rather than left out. docs/ir/SPEC.md's "Slack is a node, not
// a rule" makes every byte of a record that no item occupies a node of its own,
// and a table that omitted them would show a gap between two offsets that a
// reader would take for an error in this generator — which is the opposite of
// what the run is: bytes the producer has already accounted for.
func (w *itemWalk) slack(s *irpb.Slack, path []element, at span, chosen presence) span {
	width := fixedSpan(uint64(s.GetWidth()))

	w.rows = append(w.rows, item{
		path:    descend(path, supplied(anonymousSlack)),
		at:      at,
		extent:  width,
		usage:   notAnItem,
		picture: notAnItem,
		present: chosen,
	})

	return width
}

// field is one elementary item.
func (w *itemWalk) field(id uint64, f *irpb.Field, path []element, at span, chosen presence) (span, error) {
	name, anonymous, err := itemName(f.GetNames(), id)
	if err != nil {
		return span{}, err
	}

	usage, picture, err := described(f)
	if err != nil {
		return span{}, err
	}

	present, whole, err := w.occurrences(fixedSpan(uint64(f.GetWidth())), f.GetRepetition(), chosen)
	if err != nil {
		return span{}, err
	}

	w.rows = append(w.rows, item{
		path:    descend(path, itemElement(name, anonymous)),
		at:      at,
		extent:  whole,
		usage:   usage,
		picture: picture,
		present: present,
	})

	return whole, nil
}

// group is one item holding other items.
func (w *itemWalk) group(id uint64, g *irpb.Group, path []element, at span, chosen presence) (span, error) {
	// The cycle check comes first, ahead of anything that can fail for a reason
	// of its own. A group that contains itself and is also unnamed would
	// otherwise be reported as carrying no name — which is true, and is the less
	// useful of the two facts, and sends a reader to look at the wrong thing.
	if w.open[id] {
		return span{}, cyclic(id)
	}

	w.open[id] = true
	defer delete(w.open, id)

	name, anonymous, err := itemName(g.GetNames(), id)
	if err != nil {
		return span{}, err
	}

	// A FILLER group is walked into like any other, and its own element is this
	// generator's word rather than a name. The members' prefix is `here` either
	// way: they are inside it, and a member of a FILLER group that read as a
	// member of the record would put an item at a level of the copybook it is
	// not at.
	here := descend(path, itemElement(name, anonymous))

	// The group's own row goes in ahead of its members', so the table reads in
	// containment order. Its width is not known until they have been walked, so
	// the row is filled in below rather than composed here — the alternative is
	// summing the members twice.
	row := len(w.rows)
	w.rows = append(w.rows, item{path: here, at: at, usage: notAnItem, picture: notAnItem})

	// The members are laid out from the group's own offset, which is the first
	// occurrence's. docs/ir/SPEC.md's "Ordering and width, and no offset" fixes
	// that reading: a sum counts "exactly one occurrence of every group that
	// encloses it", and it is the first.
	one, err := w.members(id, here, at)
	if err != nil {
		return span{}, err
	}

	present, whole, err := w.occurrences(one, g.GetRepetition(), chosen)
	if err != nil {
		return span{}, err
	}

	w.rows[row].extent, w.rows[row].present = whole, present

	return whole, nil
}

// variant is one alternation over a run of bytes inside a table.
//
// The variant is a row of its own and each arm is the row of its body, at the
// variant's own offset. That repetition of one offset down a column is the
// point: docs/ir/SPEC.md's "A variant is chosen once per occurrence" has every
// arm begin at the variant's first byte, and a table drawing them at increasing
// offsets would describe a record where they follow one another.
func (w *itemWalk) variant(id uint64, v *irpb.Variant, path []element, at span, chosen presence) (span, error) {
	arms := v.GetArms()
	if len(arms) < 2 {
		return span{}, malformed(fmt.Sprintf("variant %d carries %d arms", id, len(arms)),
			"a producer MUST NOT emit a variant carrying fewer than two arms; see docs/ir/SPEC.md, \"A variant is chosen once per occurrence\"")
	}

	if w.open[id] {
		return span{}, cyclic(id)
	}

	w.open[id] = true
	defer delete(w.open, id)

	// Taken before the append, the way [itemWalk.group] takes it: the row is
	// filled in once the arms have been walked, and two places in one file
	// reaching for the same index two different ways is a difference a reader
	// stops to check.
	row := len(w.rows)

	w.rows = append(w.rows, item{
		path:    descend(path, supplied(anonymousVariant)),
		at:      at,
		usage:   notAnItem,
		picture: notAnItem,
		present: chosen,
	})

	var extent span

	for which, arm := range arms {
		width, err := w.arm(id, arm, path, at)
		if err != nil {
			return span{}, err
		}

		// Every arm's extent MUST equal every other arm's, and no item of one
		// may repeat a data-dependent number of times — which together are what
		// keep a variant's contribution to the sum a constant, so that nothing
		// behind it moves for it. Both halves are checked here because this is
		// where getting it wrong is invisible: a longer arm would draw every
		// offset behind the variant at the position the first arm implies, in a
		// table that looks perfectly well formed.
		if len(width.terms) != 0 {
			return span{}, malformed(
				fmt.Sprintf("an arm of variant %d holds an item repeating a data-dependent number of times", id),
				"no item of an arm may carry a repetition whose count is a reference at any depth; see docs/ir/SPEC.md, \"A variant is chosen once per occurrence\"")
		}

		if which == 0 {
			extent = width

			continue
		}

		if width.fixed != extent.fixed {
			return span{}, malformed(
				fmt.Sprintf("arm %d of variant %d is %d bytes and its first arm is %d", which, id, width.fixed, extent.fixed),
				"every arm's extent MUST equal every other arm's; see docs/ir/SPEC.md, \"A variant is chosen once per occurrence\"")
		}
	}

	w.rows[row].extent = extent

	return extent, nil
}

// arm is one alternative: the predicate that selects it, and the rows of its
// body.
func (w *itemWalk) arm(id uint64, a *irpb.Arm, path []element, at span) (span, error) {
	chosen, err := predicateResolved(w.nodes, a.GetPredicateId(), w.record,
		fmt.Sprintf("the predicate selecting an arm of variant %d", id))
	if err != nil {
		return span{}, err
	}

	// The reference says which kind the body is, rather than leaving this to
	// dereference an untyped identifier and find out — and each is checked
	// against the kind its position admits, so an arm naming a slack node is a
	// refusal rather than a row drawn as slack.
	switch body := a.GetBody().(type) {
	case *irpb.Arm_GroupId:
		if _, ok := w.nodes.group(body.GroupId); !ok {
			return span{}, unresolved(body.GroupId, fmt.Sprintf("the group an arm of variant %d is", id))
		}

		return w.member(body.GroupId, path, at, presence{chosen: chosen})
	case *irpb.Arm_FieldId:
		if _, ok := w.nodes.field(body.FieldId); !ok {
			return span{}, unresolved(body.FieldId, fmt.Sprintf("the field an arm of variant %d is", id))
		}

		return w.member(body.FieldId, path, at, presence{chosen: chosen})
	default:
		return span{}, malformed(fmt.Sprintf("an arm of variant %d has no body", id),
			"an arm names the item that is its body, and the reference says which kind it is; see docs/ir/SPEC.md, \"A variant is chosen once per occurrence\"")
	}
}

// occurrences is one occurrence's width taken as many times as the repetition
// says, with the presence column that states how many that is.
func (w *itemWalk) occurrences(one span, rep *irpb.Repetition, chosen presence) (presence, span, error) {
	if rep == nil {
		return chosen, one, nil
	}

	switch count := rep.GetCount().(type) {
	case *irpb.Repetition_Constant:
		chosen.repeats, chosen.constant = true, count.Constant

		return chosen, one.repeated(uint64(count.Constant)), nil
	case *irpb.Repetition_Variable:
		by, err := w.countName(count.Variable)
		if err != nil {
			return presence{}, span{}, err
		}

		chosen.repeats, chosen.by = true, by
		chosen.min, chosen.max = count.Variable.GetMinOccurrences(), count.Variable.GetMaxOccurrences()

		return chosen, one.times(by), nil
	default:
		return presence{}, span{}, malformed("an item repeats and says nothing about how many times",
			"a repetition carries a constant count or an OCCURS DEPENDING ON one; an item that does not repeat carries no repetition at all")
	}
}

// countName is where an OCCURS DEPENDING ON count is read from, as the document
// names it.
//
// The two kinds are named the way the rest of the document already names them —
// a field by its path within the record, a register by `r` and its identifier —
// so that a symbolic offset points at something a reader can find: the field in
// the table above it, or the register in the table beside the diagram.
func (w *itemWalk) countName(v *irpb.VariableCount) (string, error) {
	switch count := v.GetCount().(type) {
	case *irpb.VariableCount_FieldId:
		return fieldPath(w.nodes, w.record, count.FieldId, "the field an OCCURS DEPENDING ON count is read from")
	case *irpb.VariableCount_RegisterId:
		if _, ok := w.nodes.register(count.RegisterId); !ok {
			return "", unresolved(count.RegisterId, "the register an OCCURS DEPENDING ON count is read from")
		}

		return registerName(count.RegisterId), nil
	default:
		return "", malformed("an OCCURS DEPENDING ON repetition says nothing about where its count is read from",
			"a variable count names a field node or a register node; see docs/ir/SPEC.md, \"A variable record is a sum with a variable term\"")
	}
}

// itemName is what a table calls a group or a field: the name where the item
// has one, and [anonymousFiller] where COBOL gave it none.
//
// # A node with no names at all is a FILLER, not a fault
//
// docs/ir/SPEC.md's "Names" says what a *named* node carries, and not that
// every node is named. An item COBOL gives no data-name has no original for a
// substitute to stand beside, so a producer emits no names message for it —
// which is what a FILLER is, and a FILLER may be a group as much as an
// elementary item. It is drawn as a row like any other, with this generator's
// own word where the name would be, exactly as slack and a variant are.
//
// Refusing it was this generator's first reading and it was wrong in the
// expensive direction: FILLER is in most real copybooks, `records=all` is the
// default, and a refusal here refuses the whole document — so a layout with one
// unnamed item would have got no diagram either, over an item nobody was
// looking at.
//
// # A names message that states a blank name still is
//
// The refusal survives for the case it was actually about: a producer that
// emitted a names message and put nothing in it. That is not an unnamed item,
// it is a named one whose name is missing, and whitespace passes an emptiness
// test and draws as a cell holding a space. Same refusal [edgeAt] makes of a
// record name, and for the same reason.
func itemName(n *irpb.Names, id uint64) (string, string, error) {
	if n == nil {
		return "", anonymousFiller, nil
	}

	name := nameOf(n)
	if strings.TrimSpace(name) == "" {
		return "", "", malformed(
			fmt.Sprintf("node %d is an item of a record, carries a names message, and states no name in it", id),
			"a named node carries the original COBOL name, spelled as the copybook spells it, and an item COBOL names nothing carries no names message at all; see docs/ir/SPEC.md, \"Names\"")
	}

	return name, "", nil
}

// descend is a path with one more element, in storage of its own.
//
// The same shape as [extend] beside [walkTo], and separate from it for the
// same reason two escapers are: a plain append would let two siblings of one
// member list write into the same backing array, so the second one's element
// would land in the first one's path — a wrong path in a document, produced by a
// walk that visited the right nodes.
func descend(prefix []element, one element) []element {
	out := make([]element, len(prefix)+1)

	copy(out, prefix)
	out[len(prefix)] = one

	return out
}

// itemElement is the path element an item contributes: its name where it has
// one, and this generator's word where it has not.
func itemElement(name, anonymous string) element {
	if anonymous != "" {
		return supplied(anonymous)
	}

	return named(name)
}

// cyclic is a member list that contains one of its own ancestors.
func cyclic(id uint64) error {
	return malformed(fmt.Sprintf("node %d contains itself, at some depth", id),
		"containment is stated once, downward, so no item may contain itself; see docs/ir/SPEC.md, \"A node set, not a tree\"")
}

// described is an elementary item's usage and picture, as the table states
// them.
//
// The two are resolved together because which of them may be absent is decided
// by the other: docs/ir/SPEC.md carries a picture on every item whose PICTURE
// resolves to something, and on no item whose USAGE has none to resolve —
// COMP-1 and COMP-2 do not permit one, and INDEX, POINTER and NATIONAL have no
// logical value to describe.
//
// Both halves of that are refused rather than drawn around. A picture missing
// where one belongs would leave the cell blank, which reads as a generator that
// failed to fill it in; a picture present where none may be is a producer
// stating something about an item the IR says it derives nothing about, and
// printing it would put a fact in this table that its own descriptor disowns.
func described(f *irpb.Field) (string, string, error) {
	switch f.GetUsage() {
	case irpb.Usage_USAGE_COMP_1, irpb.Usage_USAGE_COMP_2,
		irpb.Usage_USAGE_INDEX, irpb.Usage_USAGE_POINTER, irpb.Usage_USAGE_NATIONAL:
		if f.GetPicture() != nil {
			return "", "", malformed(
				fmt.Sprintf("an item of USAGE %s carries a picture", usageName(f.GetUsage())),
				"COMP-1 and COMP-2 do not permit a PICTURE, and INDEX, POINTER and NATIONAL have no logical value to describe; see docs/ir/SPEC.md and the Field message")
		}

		return usageName(f.GetUsage()), notAnItem, nil
	case irpb.Usage_USAGE_DISPLAY, irpb.Usage_USAGE_PACKED_DECIMAL, irpb.Usage_USAGE_COMP_6,
		irpb.Usage_USAGE_BINARY, irpb.Usage_USAGE_COMP_5:
		picture := f.GetPicture()
		if picture == nil {
			return "", "", malformed(
				fmt.Sprintf("an item of USAGE %s carries no picture", usageName(f.GetUsage())),
				"only COMP-1, COMP-2, INDEX, POINTER and NATIONAL items have no PICTURE to resolve; see docs/ir/SPEC.md and the Field message")
		}

		printed, err := pictureOf(picture, f.GetWidth(), f.GetUsage())
		if err != nil {
			return "", "", err
		}

		return usageName(f.GetUsage()), printed, nil
	default:
		return "", "", malformed(
			fmt.Sprintf("an item carries USAGE %d, which this generator has no name for", int32(f.GetUsage())),
			"the set is closed and a consumer MUST refuse a member it does not recognise rather than fall back to one it does; see docs/ir/SPEC.md, \"Dereferencing is not recomputation\"")
	}
}

// usageName is a USAGE as the table spells it: the enum's own name for the
// usage, and never one of the aliases a copybook may have written.
//
// COMP-3 is also COMPUTATIONAL-3 and COMP is also COMP-4; the IR resolves an
// alias away and carries the usage the bytes are in, so a document choosing
// among the spellings would be putting back a distinction the descriptor
// deliberately dropped.
func usageName(u irpb.Usage) string {
	switch u {
	case irpb.Usage_USAGE_DISPLAY:
		return "DISPLAY"
	case irpb.Usage_USAGE_PACKED_DECIMAL:
		return "PACKED-DECIMAL"
	case irpb.Usage_USAGE_COMP_6:
		return "COMP-6"
	case irpb.Usage_USAGE_BINARY:
		return "BINARY"
	case irpb.Usage_USAGE_COMP_5:
		return "COMP-5"
	case irpb.Usage_USAGE_COMP_1:
		return "COMP-1"
	case irpb.Usage_USAGE_COMP_2:
		return "COMP-2"
	case irpb.Usage_USAGE_INDEX:
		return "INDEX"
	case irpb.Usage_USAGE_POINTER:
		return "POINTER"
	case irpb.Usage_USAGE_NATIONAL:
		return "NATIONAL"
	default:
		// Unreachable: [described] refuses a usage outside the set before it
		// asks for a name. Written as a phrase rather than as a panic, for the
		// reason [framing.String]'s default arm is a sentence.
		return "a USAGE this generator has no name for, which is a bug in " + pluginName
	}
}

// The two categories whose editing characters the IR does not carry, as the
// table names them.
const (
	numericEdited      = "numeric-edited"
	alphanumericEdited = "alphanumeric-edited"
)

// pictureOf is the structured picture as a PICTURE character-string.
//
// # It is a spelling, not a quotation
//
// The IR carries no PICTURE string anywhere. It carries five facts — the
// category, the number of stored digit positions, the scale, whether the item
// is signed and where its sign sits — because those are what a consumer acts
// on, and deriving them is cobol-go's work done before the IR exists. So
// `S9(5)V99` here is this generator's spelling of those five facts and may not
// be the text somebody's copybook wrote: `S9(5)V9(2)`, `S99999V99` and
// `S9(5)V99` are one item, and this prints one of them. The document says so
// beside the table, because a reader comparing a column against a copybook
// needs to know which side is authoritative.
//
// The repeat-count form is used at every length, `9(1)` included, so that a
// column of them lines up and a reader counting digits is reading a number
// rather than counting characters.
//
// # The two edited categories are named, and nothing of them is spelled
//
// An edited item's picture is its editing characters — `ZZ,ZZ9.99` — and the IR
// carries none of them, deliberately: an edited item has no logical value a
// generator can use, and it carries a width so that the sum stays correct
// across it. So the category is named and that is all.
//
// Not even the digit count, which the descriptor does carry. `digits` is the
// count of `9` symbols, and on a numeric picture those are the whole of the
// value — but an edited picture's `Z`, `*` and `9` are all digit positions, so
// `ZZ,ZZ9.99` carries three `9` symbols and holds seven digits. Printing three
// beside the word "stored" would state a fact about storage that is wrong, in
// the one category where a reader has nothing in the row to check it against.
// Naming the category is the same judgment as not inventing the mask.
func pictureOf(p *irpb.Picture, width uint32, usage irpb.Usage) (string, error) {
	sign, err := signClause(p, usage)
	if err != nil {
		return "", err
	}

	switch p.GetCategory() {
	case irpb.Category_CATEGORY_NUMERIC:
		digits, err := digitPositions(p)
		if err != nil {
			return "", err
		}

		return sign.leading + digits + sign.clause, nil
	case irpb.Category_CATEGORY_NUMERIC_EDITED:
		return numericEdited, nil
	case irpb.Category_CATEGORY_ALPHABETIC, irpb.Category_CATEGORY_ALPHANUMERIC,
		irpb.Category_CATEGORY_ALPHANUMERIC_EDITED:
		if p.GetSigned() {
			return "", malformed(
				fmt.Sprintf("an item's picture is %s and carries an operational sign", categoryName(p.GetCategory())),
				"a sign is S in the picture and only a numeric picture admits one; see docs/ir/SPEC.md and the Picture message")
		}

		if p.GetCategory() == irpb.Category_CATEGORY_ALPHANUMERIC_EDITED {
			return alphanumericEdited, nil
		}

		// The character count is the item's width and not its digit count:
		// `digits` is the count of 9 symbols and an alphabetic picture has
		// none, while a DISPLAY item is one character per character position.
		// docs/ir/SPEC.md gives only numeric items a USAGE other than DISPLAY in
		// any meaningful sense, so the two are the same number here.
		//
		// It is the one cell in the row derived from something other than the
		// picture, and it rests on every charset in the closed set being one
		// byte per character — which the five members are, cp037 through ASCII.
		// The one multi-byte thing the schema has is NATIONAL, and that carries
		// no picture at all, so it never reaches here. A double-byte charset
		// added to that set later would make this overstate by its own factor,
		// and this is the comment that says so.
		if p.GetCategory() == irpb.Category_CATEGORY_ALPHABETIC {
			return symbolRun("A", uint64(width)), nil
		}

		return symbolRun("X", uint64(width)), nil
	default:
		return "", malformed("an item's picture states no category",
			"a consumer may not supply one; see docs/ir/SPEC.md and the Category enum")
	}
}

// signed is the two places an operational sign shows in a picture column.
type signed struct {
	// leading is the `S` the picture opens with, and the empty string where the
	// item is unsigned.
	leading string

	// clause is the SIGN clause that says where the sign sits, and the empty
	// string where the question does not arise.
	clause string
}

// signClause is where an item's operational sign sits, as the picture column
// states it.
//
// Every position is spelled, the default included. SIGN TRAILING is what a
// signed DISPLAY item has where the copybook wrote no SIGN clause at all, and a
// column that left it out would make a reader work out which of "no sign
// position" and "the default one" a blank meant — on the one axis where the
// answer changes which byte the sign is in.
//
// # The whole axis is described, in both directions
//
// The schema carries SIGN_POSITION_UNSPECIFIED "where the question does not
// arise: an unsigned item, or a USAGE the SIGN clause has no effect on, which is
// every usage other than DISPLAY". That is an exact statement of when the field
// is set, so it is checked in both directions rather than one — and it takes the
// usage in order to do it, which is why this is not a function of the picture
// alone.
//
// Each refusal is a descriptor stating two facts that contradict, and each has
// two drawings that are equally defensible and equally wrong: print the clause
// and describe a sign the item does not hold, or drop it and discard something
// the descriptor states.
//
// Checking only the unsigned direction was this generator's first reading. It
// left the mirror case — a signed DISPLAY item stating no position — drawing as
// a bare `S9(3)`, which is exactly the blank this function's whole argument says
// must not happen, on exactly the axis it says decides which byte the sign is
// in.
func signClause(p *irpb.Picture, usage irpb.Usage) (signed, error) {
	// Whether the question arises at all: the SIGN clause has an effect on a
	// signed DISPLAY item whose picture is numeric, and on nothing else.
	//
	// The category is in the test as well as the usage because an edited item's
	// sign is one of its editing characters — a CR, a DB, a `+` — and not a
	// clause about a zone. It is also exactly where this requirement earns its
	// keep: the position is *drawn* only on a numeric picture, so a numeric
	// picture is the only place a missing one leaves a blank a reader has to
	// interpret.
	asked := usage == irpb.Usage_USAGE_DISPLAY &&
		p.GetSigned() &&
		p.GetCategory() == irpb.Category_CATEGORY_NUMERIC

	if !asked {
		if p.GetSignPosition() != irpb.SignPosition_SIGN_POSITION_UNSPECIFIED {
			return signed{}, malformed(
				fmt.Sprintf("an item of USAGE %s that is %s states where its operational sign sits",
					usageName(usage), signedness(p.GetSigned())),
				"the sign position is unspecified where the question does not arise: an unsigned item, or a USAGE the SIGN clause has no effect on, which is every usage other than DISPLAY; see docs/ir/SPEC.md and the SignPosition enum")
		}

		if !p.GetSigned() {
			return signed{}, nil
		}

		// Signed, and of a usage the SIGN clause says nothing about. The `S` is
		// the whole of what there is to say.
		return signed{leading: "S"}, nil
	}

	s := signed{leading: "S"}

	switch p.GetSignPosition() {
	case irpb.SignPosition_SIGN_POSITION_UNSPECIFIED:
		return signed{}, malformed(
			"a signed numeric DISPLAY item states nothing about where its operational sign sits",
			"the sign position is unspecified only where the question does not arise, and on a signed numeric DISPLAY item it arises — SIGN TRAILING is the default and is a position, not an absence; see docs/ir/SPEC.md and the SignPosition enum")
	case irpb.SignPosition_SIGN_POSITION_LEADING:
		s.clause = " SIGN LEADING"
	case irpb.SignPosition_SIGN_POSITION_TRAILING:
		s.clause = " SIGN TRAILING"
	case irpb.SignPosition_SIGN_POSITION_LEADING_SEPARATE:
		s.clause = " SIGN LEADING SEPARATE"
	case irpb.SignPosition_SIGN_POSITION_TRAILING_SEPARATE:
		s.clause = " SIGN TRAILING SEPARATE"
	default:
		return signed{}, malformed(
			fmt.Sprintf("an item states sign position %d, which this generator has no name for", int32(p.GetSignPosition())),
			"the set is closed and a consumer MUST refuse a member it does not recognise rather than fall back to one it does; see docs/ir/SPEC.md and the SignPosition enum")
	}

	return s, nil
}

// signedness is how a diagnostic names the half of the contradiction the
// picture states.
func signedness(is bool) string {
	if is {
		return "signed"
	}

	return "unsigned"
}

// digitPositions is the `9` and `P` positions of a numeric picture, with the
// implied decimal point where the scale puts one.
//
// The five cases are the five things a scale can be. A `P` position is a digit
// position of the value that occupies no storage, which is what a scale larger
// than the digit count or smaller than zero means, and printing it is what
// keeps `9(3)P(2)` — a value in hundreds — from reading as `9(3)`.
func digitPositions(p *irpb.Picture) (string, error) {
	digits, scale := p.GetDigits(), p.GetScale()

	if digits == 0 {
		return "", malformed("a numeric item states no stored digit positions",
			"the digit count is the number of 9 symbols with every repeat count expanded, and a numeric picture carries at least one; see docs/ir/SPEC.md and the Picture message")
	}

	switch {
	case scale < 0:
		// A picture ending in a run of P: the stored digits, then the positions
		// that scale them up.
		return symbolRun("9", uint64(digits)) + symbolRun("P", uint64(-int64(scale))), nil
	case scale == 0:
		return symbolRun("9", uint64(digits)), nil
	case uint32(scale) < digits:
		return symbolRun("9", uint64(digits)-uint64(scale)) + "V" + symbolRun("9", uint64(scale)), nil
	case uint32(scale) == digits:
		return "V" + symbolRun("9", uint64(digits)), nil
	default:
		// A picture opening with a run of P, which implies the decimal point at
		// their left: the positions that scale the value down, then the stored
		// digits.
		return symbolRun("P", uint64(scale)-uint64(digits)) + symbolRun("9", uint64(digits)), nil
	}
}

// symbolRun is one picture symbol in the repeat-count form.
//
// Named for what it is rather than `repeated`, which is already [span.repeated]
// in this file and means something else entirely — one of them takes a picture
// symbol to a string and the other takes a number of bytes to a number of bytes.
func symbolRun(symbol string, n uint64) string {
	return symbol + "(" + strconv.FormatUint(n, 10) + ")"
}

// categoryName is a category as a diagnostic names it.
func categoryName(c irpb.Category) string {
	switch c {
	case irpb.Category_CATEGORY_NUMERIC:
		return "numeric"
	case irpb.Category_CATEGORY_ALPHABETIC:
		return "alphabetic"
	case irpb.Category_CATEGORY_ALPHANUMERIC:
		return "alphanumeric"
	case irpb.Category_CATEGORY_NUMERIC_EDITED:
		return numericEdited
	case irpb.Category_CATEGORY_ALPHANUMERIC_EDITED:
		return alphanumericEdited
	default:
		return "of no category this generator has a name for"
	}
}
