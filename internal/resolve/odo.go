// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// This file is the whole of what an `OCCURS DEPENDING ON` clause costs this
// package: which reading the layout stated, what a count reference is held to
// under the one reading that has any, and reading a resolved record back at the
// counts a consumer decoded.
//
// # The reading decides whether there is a reference at all
//
// docs/ir/SPEC.md's "An item after a table slides, and the other reading is a
// fixed table" forks here and nowhere else. Under `odoslide` a table becomes a
// repetition whose count is a reference and the record's extent moves with it;
// under `noodoslide` the same copybook describes a *fixed* table of the declared
// maximum beside a field saying how many entries the writing program filled,
// which is an ordinary shape this package already resolved before ODO reached
// it. So the second reading adds no arithmetic: it drops the reference, and
// every rule below has nothing left to bind.
//
// There is no default, because the two readings put every item behind the table
// somewhere different and nothing in the file disagrees with the wrong one. A
// layout that states neither is rejected against the table that needed the
// answer (#27, #35, #87).
//
// # Why the checks run on the copybook's items rather than on the nodes
//
// Every rule here is about the copybook's own shape — where the count sits, what
// repeats, what two OCCURS clauses declare — and none of it is about which
// REDEFINES alternative a caller is reading. A record whose copybook holds four
// alternatives resolves to four [Record]s, and running these over the nodes
// would report one copybook fault four times.

// tables returns every item of the record whose OCCURS clause names a count, in
// source order.
//
// It is what decides whether the layout owed a reading: a copybook holding no
// such clause needs no answer, and requiring one of every layout would make a
// setting about tables into a setting about layouts that have none.
func tables(items []*copybook.Item) []*copybook.Item {
	var found []*copybook.Item

	for _, item := range items {
		if item.DependingOn != nil {
			found = append(found, item)
		}
	}

	return found
}

// requireReading holds a layout binding a copybook with an `OCCURS DEPENDING ON`
// in it to having said which reading its file was written under.
//
// One fault per table, and each names the table, because that is where the
// adopter looks the setting up: the answer is a property of the compiler that
// wrote the file, and finding out which compiler that was starts at the clause.
func (r *resolver) requireReading(odo []*copybook.Item) {
	for _, table := range odo {
		r.faults.Fail(&UnstatedReadingError{
			Pos:    r.span(table.Field),
			Record: r.record.Name,
			Table:  itemName(table.Field),
			Count:  itemName(table.DependingOn.Field),
		})
	}
}

// checkCounts holds every count reference of the record to what docs/ir/SPEC.md
// admits in one.
//
// It runs under the sliding reading alone. A non-sliding record carries no count
// reference for any of this to bind — "None of this reaches a record of a
// non-sliding file", as "A count is in hand before the extent it decides" puts
// it — and running the checks anyway would reject a layout over bytes that are a
// fixed table and a field.
func (r *resolver) checkCounts(items, odo []*copybook.Item) {
	for _, table := range odo {
		count := table.DependingOn

		switch group := enclosingTable(count); {
		case count.MaxOccurs > 1 || group != nil:
			// docs/ir/SPEC.md, "A reference names a field, not an
			// occurrence of one": a count with a value per occurrence of
			// its enclosing group is a group whose occurrences are not all
			// the same width, and the extent sum stops being arithmetic.
			r.faults.Fail(&CountOccurrenceError{
				Pos:    r.span(count.Field),
				Record: r.record.Name,
				Count:  itemName(count.Field),
				Table:  itemName(table.Field),
				Group:  groupName(group),
			})

		default:
			// docs/ir/SPEC.md, "A count is in hand before the extent it
			// decides", second half: a count whose own position is a sum
			// with a variable term in it.
			//
			// That section's first half — a count lying behind the very
			// table it sizes — is not checked here, and not because it is
			// admitted. `cobol-go` refuses it while it lays the copybook
			// out, naming the count and the table ("item N is defined
			// after the table it controls"), and [Resolve] returns that
			// unchanged. Restating it would mean resolving the DEPENDING
			// ON data-name out of the raw OCCURS clause a second time, to
			// answer a question the layout has already answered.
			if ahead := variableAhead(items, count); ahead != nil {
				r.faults.Fail(&CountPositionError{
					Pos:    r.span(count.Field),
					Record: r.record.Name,
					Count:  itemName(count.Field),
					Table:  itemName(table.Field),
					Behind: itemName(ahead.Field),
				})
			}
		}
	}

	r.checkSharedBounds(odo)
}

// variableAhead returns the first item lying wholly ahead of count whose own
// repetition count is a reference, or nil where none does.
//
// "Ahead of" is the record's byte order, which is the order a consumer walks it
// in. An item that ends at or before the count's first byte is one whose extent
// enters the sum that locates the count, so a variable one leaves that sum
// without a value at the moment the count is needed.
func variableAhead(items []*copybook.Item, count *copybook.Item) *copybook.Item {
	for _, item := range items {
		if item.DependingOn != nil && item.End() <= count.Offset {
			return item
		}
	}

	return nil
}

// checkSharedBounds refuses a record whose repeating items name one count and
// declare ranges with no value in common.
//
// Sharing itself is admitted: a consumer decodes the count once and sizes every
// table naming it from that one value, and nothing about locating an item
// changes (docs/ir/SPEC.md, "One count may size two tables, and a writer refuses
// to choose", #89). What cannot hold is two declared ranges that do not overlap,
// because then no count sizes both tables and every record of the descriptor is
// malformed data — so the layout is rejected once here rather than diagnosed
// once per record for the life of the file.
//
// One fault per count field, naming the first pair that cannot hold. A third
// table disjoint from the same two is the same mistake, and three diagnostics
// about it would send an adopter to the same clause three times.
func (r *resolver) checkSharedBounds(odo []*copybook.Item) {
	sharing := make(map[*copybook.Item][]*copybook.Item, len(odo))

	var counts []*copybook.Item

	for _, table := range odo {
		count := table.DependingOn
		if _, seen := sharing[count]; !seen {
			counts = append(counts, count)
		}

		sharing[count] = append(sharing[count], table)
	}

	for _, count := range counts {
		if pair := disjoint(sharing[count]); pair != nil {
			r.faults.Fail(&SharedCountBoundsError{
				Pos:    r.span(count.Field),
				Record: r.record.Name,
				Count:  itemName(count.Field),
				Tables: []string{itemName(pair[0].Field), itemName(pair[1].Field)},
				Bounds: [][2]int{
					{pair[0].MinOccurs, pair[0].MaxOccurs},
					{pair[1].MinOccurs, pair[1].MaxOccurs},
				},
			})
		}
	}
}

// disjoint returns the first pair of the tables whose declared ranges have no
// value in common, or nil where every pair overlaps.
//
// Two alternatives of one REDEFINES are skipped, because they never both stand
// in one record: each becomes a record of its own, and a count sizing the one
// being read is never asked to size the other.
func disjoint(sharing []*copybook.Item) []*copybook.Item {
	for i, a := range sharing {
		for _, b := range sharing[i+1:] {
			if exclusive(a, b) {
				continue
			}

			if max(a.MinOccurs, b.MinOccurs) > min(a.MaxOccurs, b.MaxOccurs) {
				return []*copybook.Item{a, b}
			}
		}
	}

	return nil
}

// exclusive reports whether two items are alternatives of one another: two
// descriptions of one run of storage, at whatever depth the REDEFINES was
// written, which never both stand in one resolved record.
func exclusive(a, b *copybook.Item) bool {
	for x := a; x != nil; x = x.Parent {
		for y := b; y != nil; y = y.Parent {
			if x == y || x.Parent != y.Parent {
				continue
			}

			if redefineBase(x) == redefineBase(y) {
				return true
			}
		}
	}

	return false
}

// redefineBase is the item a chain of REDEFINES is rooted at, which is the
// identity of the run of storage the chain describes.
func redefineBase(item *copybook.Item) *copybook.Item {
	for item.Redefines != nil {
		item = item.Redefines
	}

	return item
}

// repetitionOf reads an item's repetition under the layout's reading, nil where
// the item does not repeat.
//
// The fork is here and in one line of [resolver.referenceCount], and nothing
// else in this package asks which reading it is running under. Under `odoslide`
// the count is the reference the copybook wrote and the bounds are the
// copybook's own `OCCURS integer-1 TO integer-2`, carried for the one check
// docs/ir/SPEC.md makes with them — a count outside them is malformed data — and
// neither narrowed nor widened to what a layout would prefer. Under `noodoslide`
// the same clause describes a fixed table at its declared maximum, so the
// repetition is a constant, the bounds equal it as they do for any fixed table,
// and the count field is left an ordinary field of the record with nothing
// pointing at it.
func (r *resolver) repetitionOf(item *copybook.Item) *Repetition {
	if item.MaxOccurs <= 1 {
		return nil
	}

	if item.DependingOn == nil {
		return &Repetition{Count: item.Occurs, Min: item.MinOccurs, Max: item.MaxOccurs}
	}

	if !r.opts.Reading.Slides() {
		return &Repetition{Count: item.MaxOccurs, Min: item.MaxOccurs, Max: item.MaxOccurs}
	}

	return &Repetition{
		Count:       item.Occurs,
		Min:         item.MinOccurs,
		Max:         item.MaxOccurs,
		DependingOn: item.DependingOn.Field,
	}
}

// referenceCount returns the first item at or under item whose repetition count
// is read out of the record, or nil where none is.
//
// It answers under the reading for [resolver.repetitionOf]'s reason: on a
// non-sliding file the same clause is a fixed table, so an arm holding one has
// the constant extent an arm needs and there is nothing to report.
func (r *resolver) referenceCount(item *copybook.Item) *copybook.Item {
	if !r.opts.Reading.Slides() {
		return nil
	}

	if item.DependingOn != nil {
		return item
	}

	for _, child := range item.Children {
		if found := r.referenceCount(child); found != nil {
			return found
		}
	}

	return nil
}

// Counts are the occurrence counts a consumer decoded out of one record's bytes,
// keyed by the field each one was read from.
//
// Keyed by the count rather than by the table because that is how a consumer
// holds them: docs/ir/SPEC.md admits two repeating items naming one count, and a
// consumer decodes that field once and sizes both tables from the one value. A
// map keyed by the table would carry that value twice, for the two copies to
// disagree.
type Counts map[*copybook.Field]int

// At returns the record as it stands at counts: the same containment order, with
// every repetition whose count is a reference standing at the value decoded for
// it, so that [Record.Extent] and [Record.Position] answer for one record's
// bytes rather than at the declared maximum.
//
// This is docs/ir/SPEC.md's "A variable record is a sum with a variable term"
// run here, for the reason [Record.Position] is: the sum is the arithmetic every
// consumer performs, and this package's tests run it against the offsets
// `cobol-go` computed independently. It is not a decoder — nothing here reads
// bytes — and a count that would not decode as a number at all is reported by
// whoever decoded it.
//
// What it will not do is default. A count the map does not carry is a
// [MissingCountError] rather than an absent table, because reading a count field
// holding spaces as zero produces a record that parses and is wrong, and that is
// a real mainframe occurrence. A count outside the bounds the copybook declared
// — negative among them — is a [CountBoundsError] against *every* repetition
// naming it, not against the first, because each carries its own declared
// minimum and maximum and both bind the one value.
//
// The nodes of the record it returns are copies, so a caller naming one of them
// names it through [Record.Find] or [Record.Walk] rather than by holding a
// pointer into the record it started from.
//
// A record carrying no count reference — every record of a non-sliding file, and
// every record with no table in it — comes back unchanged, and an empty map is
// the right argument for one.
func (r *Record) At(counts Counts) (*Record, error) {
	var faults diag.List

	r.Walk(func(node *Node) {
		if !node.Repetition.Reference() {
			return
		}

		count, ok := counts[node.Repetition.DependingOn]
		if !ok {
			faults.Fail(&MissingCountError{
				Record: itemName(r.Item),
				Count:  itemName(node.Repetition.DependingOn),
				Table:  tableName(node),
			})

			return
		}

		if count < node.Repetition.Min || count > node.Repetition.Max {
			faults.Fail(&CountBoundsError{
				Record: itemName(r.Item),
				Count:  itemName(node.Repetition.DependingOn),
				Table:  tableName(node),
				Value:  count,
				Min:    node.Repetition.Min,
				Max:    node.Repetition.Max,
			})
		}
	})

	if faults.Failed() {
		return nil, faults.Err()
	}

	return &Record{
		Root:         r.Root.at(counts),
		Item:         r.Item,
		Alternatives: r.Alternatives,
	}, nil
}

// at copies the node with every reference repetition under it standing at the
// count decoded for it.
func (n *Node) at(counts Counts) *Node {
	copied := *n

	if n.Repetition != nil {
		repetition := *n.Repetition
		if repetition.Reference() {
			repetition.Count = counts[repetition.DependingOn]
		}

		copied.Repetition = &repetition
	}

	if len(n.Members) > 0 {
		copied.Members = make([]*Node, 0, len(n.Members))
		for _, member := range n.Members {
			copied.Members = append(copied.Members, member.at(counts))
		}
	}

	if len(n.Arms) > 0 {
		copied.Arms = make([]Arm, 0, len(n.Arms))
		for _, arm := range n.Arms {
			arm.Body = arm.Body.at(counts)
			copied.Arms = append(copied.Arms, arm)
		}
	}

	return &copied
}

// tableName is what a diagnostic calls the repeating item a count sizes.
//
// A node this package introduced to hold an elementary item beside the slack
// padding it out to its stride carries the repetition while the field inside it
// carries the name, so the name is taken from the first member where the node
// has none of its own — the same reading [variableTerm] makes.
func tableName(node *Node) string {
	if node.Field != nil {
		return itemName(node.Field)
	}

	if len(node.Members) > 0 {
		return itemName(node.Members[0].Field)
	}

	return itemName(nil)
}
