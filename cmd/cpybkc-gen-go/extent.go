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

// direction is which of a record's two methods a width is being summed for.
//
// It exists because one of the two terms of a sum has a different source in
// each: the number of occurrences of a variable table is the count field the
// record carries when the record is being read, and is the number of
// occurrences the caller supplied when it is being written. Everything else in
// a sum is a constant the descriptor states.
type direction int

const (
	decoding direction = iota
	encoding
)

// extent is a number of bytes as the generated code computes it: the constant
// part, which the descriptor states, and one term per table whose number of
// occurrences is data.
//
// docs/ir/SPEC.md's "Ordering and width, and no offset" is why this is summed
// rather than read: the IR carries a width on every item and no offset
// anywhere, because an offset is the sum of the widths ahead of it and a fact
// stated twice is a fact two producers can disagree about. Summing it here, at
// generation time, is what "Dereferencing is not recomputation" asks of a
// consumer — the widths are taken as they stand and nothing is re-derived from
// a PICTURE.
type extent struct {
	// fixed is the constant part, in bytes.
	fixed int

	// terms are the parts that are not, each a Go expression in bytes.
	terms []string
}

// fixed is a constant extent.
func fixed(n int) extent { return extent{fixed: n} }

// add is the two extents summed.
func (x extent) add(y extent) extent {
	return extent{fixed: x.fixed + y.fixed, terms: append(append([]string{}, x.terms...), y.terms...)}
}

// plus is the extent n bytes further along.
func (x extent) plus(n int) extent { return x.add(fixed(n)) }

// times is the extent repeated count times, where count is a Go expression.
func (x extent) times(count string) extent {
	return extent{terms: []string{"(" + x.String() + ")*(" + count + ")"}}
}

// repeated is the extent repeated a constant number of times.
func (x extent) repeated(n int) extent {
	if len(x.terms) == 0 {
		return fixed(x.fixed * n)
	}

	return extent{terms: []string{"(" + x.String() + ")*" + strconv.Itoa(n)}}
}

// String is the extent as one Go expression.
func (x extent) String() string {
	if len(x.terms) == 0 {
		return strconv.Itoa(x.fixed)
	}

	if x.fixed == 0 {
		return strings.Join(x.terms, " + ")
	}

	return strconv.Itoa(x.fixed) + " + " + strings.Join(x.terms, " + ")
}

// sumWidth is the width of one occurrence of the group id, as a Go expression.
func (c *coder) sumWidth(id uint64, expr string, dir direction) (string, error) {
	at, err := c.members(id, expr, dir)
	if err != nil {
		return "", err
	}

	return at.String(), nil
}

// members is the sum of the widths of the group id's members.
func (c *coder) members(id uint64, expr string, dir direction) (extent, error) {
	in, err := c.groupName(id)
	if err != nil {
		return extent{}, err
	}

	members, err := c.flattened(id)
	if err != nil {
		return extent{}, err
	}

	var total extent

	for _, memberID := range members {
		width, err := c.width(memberID, in, expr, dir)
		if err != nil {
			return extent{}, err
		}

		total = total.add(width)
	}

	return total, nil
}

// width is the width of one member, occurrences and all.
//
// in is the group containing it, as the copybook names it, and it is there for
// the one member that may have no name of its own: a refusal about a FILLER has
// to name the same group wherever this generator met the item, so every caller
// passes the innermost named group rather than whatever it happens to hold.
func (c *coder) width(id uint64, in, expr string, dir direction) (extent, error) {
	node, ok := c.nodes[id]
	if !ok {
		return extent{}, unresolved(id)
	}

	switch kind := node.GetKind().(type) {
	case *irpb.Node_Slack:
		return fixed(int(kind.Slack.GetWidth())), nil
	case *irpb.Node_Field:
		// An item the copybook gives no data-name occupies the bytes it
		// occupies, and no expression is needed to say how many: its
		// occurrences are a constant or it is refused. See [fillerRun].
		if anonymous(kind.Field.GetNames()) {
			run, err := fillerRun(kind.Field, in)
			if err != nil {
				return extent{}, err
			}

			return fixed(int(run)), nil
		}

		name, err := identifier("field", kind.Field.GetNames())
		if err != nil {
			return extent{}, err
		}

		return c.occurrences(fixed(int(kind.Field.GetWidth())), kind.Field.GetRepetition(), expr+"."+name, dir)
	case *irpb.Node_Group:
		name, err := identifier("group", kind.Group.GetNames())
		if err != nil {
			return extent{}, err
		}

		at := expr + "." + name

		element := at
		if kind.Group.GetRepetition() != nil {
			// One occurrence, whichever it is: every occurrence of a group is
			// the same width, which is the property the whole treatment of a
			// table is built on.
			element = at + "[0]"
		}

		one, err := c.members(id, element, dir)
		if err != nil {
			return extent{}, err
		}

		return c.occurrences(one, kind.Group.GetRepetition(), at, dir)
	case *irpb.Node_Variant:
		// Checked here as well as where the arms become fields, because a
		// width is summed for a record whose length is all a consumer wants
		// and this is the one place that would answer with a panic rather than
		// a diagnostic.
		if len(kind.Variant.GetArms()) < 2 {
			return extent{}, malformed(fmt.Sprintf("a variant carries %d arms", len(kind.Variant.GetArms())),
				"a producer must not emit a variant carrying fewer than two arms; see docs/ir/SPEC.md, \"A variant is chosen once per occurrence\"")
		}

		// Every arm covers the same bytes, so the variant's width is any arm's
		// and a consumer that wants a record's length evaluates no predicate at
		// all.
		arm, err := c.armBody(kind.Variant.GetArms()[0])
		if err != nil {
			return extent{}, err
		}

		return c.width(arm.GetId(), in, expr, dir)
	default:
		return extent{}, malformed(fmt.Sprintf("node %d is not something a group may contain", id),
			"a member list names a group, variant, field or slack node; see docs/ir/SPEC.md, \"The node kinds\"")
	}
}

// occurrences is one occurrence's extent taken as many times as the repetition
// says.
func (c *coder) occurrences(one extent, rep *irpb.Repetition, expr string, dir direction) (extent, error) {
	if rep == nil {
		return one, nil
	}

	switch count := rep.GetCount().(type) {
	case *irpb.Repetition_Constant:
		return one.repeated(int(count.Constant)), nil
	case *irpb.Repetition_Variable:
		if dir == encoding {
			// What the caller supplied, because that is what a writer is about
			// to emit and what it determines the count field from.
			return one.times("len(" + expr + ")"), nil
		}

		held, err := c.countValue(count.Variable, expr)
		if err != nil {
			return extent{}, err
		}

		return one.times("int(" + held + ")"), nil
	default:
		return extent{}, malformed("an item repeats and says nothing about how many times",
			"a repetition carries a constant count or an OCCURS DEPENDING ON one; an item that does not repeat carries no repetition at all")
	}
}

// offsetOf is where the field target begins inside one occurrence of the group
// root, as a Go expression, and whether it is in there at all.
func (c *coder) offsetOf(root, target uint64, expr string, dir direction) (extent, bool, error) {
	in, err := c.groupName(root)
	if err != nil {
		return extent{}, false, err
	}

	members, err := c.flattened(root)
	if err != nil {
		return extent{}, false, err
	}

	var at extent

	for _, memberID := range members {
		if memberID == target {
			return at, true, nil
		}

		member, ok := c.nodes[memberID]
		if !ok {
			return extent{}, false, unresolved(memberID)
		}

		switch kind := member.GetKind().(type) {
		case *irpb.Node_Group:
			if kind.Group.GetRepetition() == nil {
				name, err := identifier("group", kind.Group.GetNames())
				if err != nil {
					return extent{}, false, err
				}

				inner, found, err := c.offsetOf(memberID, target, expr+"."+name, dir)
				if err != nil {
					return extent{}, false, err
				}

				if found {
					return at.add(inner), true, nil
				}
			}
		case *irpb.Node_Variant:
			// A target inside an arm that also contains the variant is
			// admissible, which is a copybook redefining a redefinition. One
			// inside a sibling arm is not, and resolve has already refused it.
			for _, a := range kind.Variant.GetArms() {
				body, err := c.armBody(a)
				if err != nil {
					return extent{}, false, err
				}

				if body.GetId() == target {
					return at, true, nil
				}

				if body.GetGroup() == nil {
					continue
				}

				if err := namedArm(body, in); err != nil {
					return extent{}, false, err
				}

				name, err := identifier("arm", namesOf(body))
				if err != nil {
					return extent{}, false, err
				}

				inner, found, err := c.offsetOf(body.GetId(), target, expr+"."+name, dir)
				if err != nil {
					return extent{}, false, err
				}

				if found {
					return at.add(inner), true, nil
				}
			}
		}

		width, err := c.width(memberID, in, expr, dir)
		if err != nil {
			return extent{}, false, err
		}

		at = at.add(width)
	}

	return extent{}, false, nil
}
