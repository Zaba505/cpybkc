// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package conformance

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/Zaba505/cpybkc/irpb"
)

// The rules a scalar that is not written canonically is reported against. They
// are sentences rather than codes because the reader is the author of an entry,
// and what they need is the rule they broke rather than a name for it. Each is
// docs/conformance/SPEC.md's "The value language" in one line (#196).
const (
	scalarIsAString  = "every scalar of the value language is a JSON string"
	numberIsDecimal  = "a number is 0, or an optional - and a digit 1 to 9 with any digits behind it"
	numberHasNoZero  = "a number carries no leading zero, so that one value has one spelling"
	numberHasNoPlus  = "a number carries no leading +, and the negative case is the only one with a sign"
	numberHasNoMinus = "zero is written 0 whatever sign the bytes carry, and a COBOL numeric item has no negative zero"
	bytesAreBase64   = "a run of bytes is written in RFC 4648 section 4's alphabet, padded with = to a multiple of four, and carries no line break or space"
	bytesAreCanon    = "the unused bits of a base64 value's final quantum are zero, which is RFC 4648 section 3.5's canonical encoding"
	textIsTrimmed    = `a writer removes every trailing space, and an item holding nothing but spaces is written ""`
)

// form is how one elementary item's value is written, which
// docs/conformance/SPEC.md's "Which form a value takes is decided by the
// descriptor" makes a function of the item's usage and category alone.
type form int

const (
	// formUnstated is an item whose descriptor says neither of the two things
	// that decide a form: a usage that is not one of the five that decide by
	// itself, beside a category that is not one of the five the format names. A
	// spelling is not checked there, because there is no form to check it
	// against and inventing one would refuse an entry over a rule this format
	// has not stated.
	formUnstated form = iota

	formNumber
	formFloat
	formBytes
	formText
)

// formOf is the form an item's value takes, read exactly in the order the
// format states: usage first, and category only where usage did not decide.
//
// The order is not an implementation detail. The two floating-point usages
// carry CATEGORY_NUMERIC and are not written as numbers, so a reader that keyed
// on category alone would hold a COMP-1 to the decimal grammar and refuse every
// entry that carries one.
func formOf(field *irpb.Field) form {
	switch field.GetUsage() {
	case irpb.Usage_USAGE_COMP_1, irpb.Usage_USAGE_COMP_2:
		return formFloat
	case irpb.Usage_USAGE_INDEX, irpb.Usage_USAGE_POINTER, irpb.Usage_USAGE_NATIONAL:
		return formBytes
	}

	switch field.GetPicture().GetCategory() {
	case irpb.Category_CATEGORY_NUMERIC:
		return formNumber
	case irpb.Category_CATEGORY_ALPHABETIC, irpb.Category_CATEGORY_ALPHANUMERIC,
		irpb.Category_CATEGORY_NUMERIC_EDITED, irpb.Category_CATEGORY_ALPHANUMERIC_EDITED:
		return formText
	}

	return formUnstated
}

// fault is why text is not written in this form, and "" where it is.
func (f form) fault(text string) string {
	switch f {
	case formNumber:
		return numberFault(text)
	case formFloat:
		return floatFault(text)
	case formBytes:
		return bytesFault(text)
	case formText:
		return textFault(text)
	default:
		return ""
	}
}

// spellings is every value of a document that is not written the way the value
// language writes a value of its form.
//
// This walks the descriptor beside the document, which [Values.check] says at
// length it does not do for a value's *shape*. The difference is what a fault
// here would otherwise be reported as. A shape that differs is a disagreement
// two answers can have — a generator that lost a table against an entry that
// states one — and [Compare] reports it against a runner that actually decoded
// the bytes. A spelling is not a disagreement anybody can have: "012" and "12"
// are the same value, one entry, and one author who wrote it down twice as they
// meant it once. Left to the comparison it surfaces as a generator appearing to
// disagree about a number, which sends its author to their decoder over a typo
// in the file they were editing (#196).
//
// So the walk is deliberately silent about everything except a spelling. Where
// the document's shape does not line up with the descriptor's — a group where a
// scalar is described, a key the descriptor does not carry, an arm that is not
// there — it checks what it can reach and reports nothing about the shape, so
// that the loader keeps having no opinion about one.
func (v *Values) spellings(descriptor *irpb.Descriptor) []error {
	nodes := nodesByID(descriptor)
	roots := recordRoots(descriptor, nodes)

	var faults []error

	for i, record := range v.Records {
		root, ok := roots[record.Name]
		if !ok || root == nil {
			// A name the descriptor does not carry is already reported by
			// [Values.check], and one it carries twice is a record this walk
			// cannot tell the shape of. Neither is a spelling.
			continue
		}

		// Counted from one and named, as [Compare] counts and names, because
		// the two reports are read side by side over the same document.
		faults = append(faults, spelling(nodes, root, record.Value,
			fmt.Sprintf("record %d %s", i+1, record.Name))...)
	}

	return faults
}

// spelling is every fault at or beneath path, where node is the descriptor's
// account of what value holds.
func spelling(nodes map[uint64]*irpb.Node, node *irpb.Node, value any, path string) []error {
	switch kind := node.GetKind().(type) {
	case *irpb.Node_Group:
		if kind.Group.GetRepetition() != nil {
			return occurrenceSpellings(value, path, func(one any, at string) []error {
				return groupSpellings(nodes, kind.Group, one, at)
			})
		}

		return groupSpellings(nodes, kind.Group, value, path)
	case *irpb.Node_Field:
		if kind.Field.GetRepetition() != nil {
			return occurrenceSpellings(value, path, func(one any, at string) []error {
				return scalarSpelling(kind.Field, one, at)
			})
		}

		return scalarSpelling(kind.Field, value, path)
	default:
		// A record whose root is neither is a descriptor
		// [github.com/Zaba505/cpybkc/internal/assemble.Validate] has already
		// refused, and nothing here is the place to say so a second time.
		return nil
	}
}

// groupSpellings walks a group's members beside the object the document wrote
// for it.
//
// A member the object does not carry is passed over rather than reported: an
// arm the occurrence does not hold has no key by design, and a key that is
// genuinely missing is a difference [Compare] states against an answer.
//
// A name two members share is passed over as well, for the reason [recordRoots]
// passes over a record name two records share, and with more force: one key
// would be two forms, and holding the value to both would refuse a document
// that is correct about the one the occurrence actually holds. The format says
// such a group cannot be written down at all — two arms of one name would be
// one key — so this is the walk declining to be the thing that reports it, not
// a case it supports.
func groupSpellings(nodes map[uint64]*irpb.Node, group *irpb.Group, value any, path string) []error {
	held, ok := value.(map[string]any)
	if !ok {
		return nil
	}

	items := groupItems(nodes, group)

	claimed := make(map[string]int, len(items))
	for _, item := range items {
		claimed[original(item)]++
	}

	var faults []error

	for _, item := range items {
		name := original(item)

		if claimed[name] != 1 {
			continue
		}

		one, ok := held[name]
		if !ok {
			continue
		}

		faults = append(faults, spelling(nodes, item, one, path+"."+name)...)
	}

	return faults
}

// groupItems is everything a group contributes to the object written for it, in
// member order — which is the order a report reads in, and the reason this is a
// slice and the count above is taken from it rather than the other way round.
func groupItems(nodes map[uint64]*irpb.Node, group *irpb.Group) []*irpb.Node {
	var items []*irpb.Node

	for _, id := range group.GetMemberIds() {
		member, ok := nodes[id]
		if !ok {
			continue
		}

		if member.GetSlack() != nil {
			// Slack is not a value, so a values document carries no key for it
			// and this walk has nothing to check.
			continue
		}

		items = append(items, members(nodes, member)...)
	}

	return items
}

// members is what one member of a group contributes to the enclosing object: a
// variant contributes each of its arms, whose bodies are members of that object
// under their own names, and everything else contributes itself.
func members(nodes map[uint64]*irpb.Node, member *irpb.Node) []*irpb.Node {
	variant := member.GetVariant()
	if variant == nil {
		return []*irpb.Node{member}
	}

	var bodies []*irpb.Node

	for _, arm := range variant.GetArms() {
		var id uint64

		switch body := arm.GetBody().(type) {
		case *irpb.Arm_GroupId:
			id = body.GroupId
		case *irpb.Arm_FieldId:
			id = body.FieldId
		default:
			continue
		}

		if body, ok := nodes[id]; ok {
			bodies = append(bodies, body)
		}
	}

	return bodies
}

// occurrenceSpellings walks the occurrences of a repeating node.
//
// A value that is not an array is passed over for the reason a group that is
// not an object is: a table written as a scalar is a difference between two
// answers, and [Compare] is where a difference is reported.
func occurrenceSpellings(value any, path string, one func(any, string) []error) []error {
	held, ok := value.([]any)
	if !ok {
		return nil
	}

	var faults []error

	for i, occurrence := range held {
		faults = append(faults, one(occurrence, fmt.Sprintf("%s[%d]", path, i))...)
	}

	return faults
}

// scalarSpelling holds one elementary item's value to the form its descriptor
// gives it.
//
// A group or a table where the descriptor describes an elementary item is
// passed over, because that is a shape and shapes are [Compare]'s. Anything
// else that is not a string is reported here rather than left, because the
// value language writes every scalar as a string and a JSON number is the one
// way of writing a value that this format names and forbids in the same
// sentence: a number written as one is read as a double, and a PIC S9(18) item
// holds values a double does not.
//
// That rule is ahead of the form and not behind it. "Every scalar is a JSON
// string" is stated of the language rather than of a form, and its reason —
// what a JSON number does to a value on the way through a reader — holds for an
// item whose descriptor decides no form just as it does for one that decides
// the decimal grammar. Only the grammar itself is skipped there.
func scalarSpelling(field *irpb.Field, value any, path string) []error {
	switch value.(type) {
	case map[string]any, []any:
		return nil
	}

	text, ok := value.(string)
	if !ok {
		return []error{fmt.Errorf("%s: %s is %s, and %s", ValuesName, path, rendered(value), scalarIsAString)}
	}

	form := formOf(field)
	if form == formUnstated {
		return nil
	}

	if fault := form.fault(text); fault != "" {
		return []error{fmt.Errorf("%s: %s is %s, and %s", ValuesName, path, rendered(text), fault)}
	}

	return nil
}

// numberFault holds a value to the decimal grammar
// docs/conformance/SPEC.md's "A number is a decimal string" states:
//
//	number  = "0" / [ "-" ] NONZERO *DIGIT
//	NONZERO = %x31-39
//
// The three spellings the section calls out by name are reported by name — a
// leading zero, a leading +, and a negative zero — because each of them is
// something an author wrote on purpose, and "not a decimal string" would send
// them looking for a typo they did not make.
func numberFault(text string) string {
	if text == "0" {
		return ""
	}

	digits := strings.TrimPrefix(text, "-")

	switch {
	case strings.HasPrefix(digits, "+"):
		return numberHasNoPlus
	case !onlyDigits(digits):
		// Ahead of the two rules below, so that "0.5" is reported as not being
		// a decimal string rather than as a decimal string with a leading
		// zero, which would send its author to the zero and not to the point.
		return numberIsDecimal
	case digits == "0":
		return numberHasNoMinus
	case strings.HasPrefix(digits, "0"):
		return numberHasNoZero
	}

	return ""
}

// bytesFault holds a value to the base64 encoding
// docs/conformance/SPEC.md's "INDEX, POINTER and NATIONAL are base64" states.
//
// The check is a decode and an encode rather than a grammar, and it takes all
// three passes because encoding/base64 is lenient in two different ways and the
// two are different faults to an author. An ordinary decode refuses the
// alphabet and the padding. A strict decode is the only thing that refuses a
// final quantum whose unused bits are not zero, which an ordinary one accepts
// and rounds away. And re-encoding is what refuses the line breaks a decoder
// skips over silently on the way through — the wrapping every language's
// encoder offers, which would otherwise decode to the right bytes and compare
// unequal to every other implementation's spelling of them.
func bytesFault(text string) string {
	decoded, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return bytesAreBase64
	}

	if _, err := base64.StdEncoding.Strict().DecodeString(text); err != nil {
		return bytesAreCanon
	}

	if base64.StdEncoding.EncodeToString(decoded) != text {
		return bytesAreBase64
	}

	return ""
}

// textFault holds a character item to the one rule the format states about how
// its characters are written: a trailing space is padding rather than data, and
// a writer removes every one of them.
//
// Nothing else about a character item is checkable from here. Its content is
// whatever the file's charset made of the bytes, so this is the whole of the
// canonical form of one.
func textFault(text string) string {
	if strings.HasSuffix(text, " ") {
		return textIsTrimmed
	}

	return ""
}

// onlyDigits is whether text is one or more ASCII digits and nothing else.
//
// Written out rather than taken from a package because "is a digit" has two
// readings in Go and only one of them is this one: unicode.IsDigit admits every
// decimal digit of every script, and a values document written in Devanagari
// digits is not a document any other implementation reads as a number.
func onlyDigits(text string) bool {
	if text == "" {
		return false
	}

	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

// nodesByID indexes a descriptor, which is how every reference in one is
// resolved: the nodes are a flat list and a reference is an identifier.
func nodesByID(descriptor *irpb.Descriptor) map[uint64]*irpb.Node {
	nodes := make(map[uint64]*irpb.Node, len(descriptor.GetNodes()))

	for _, node := range descriptor.GetNodes() {
		nodes[node.GetId()] = node
	}

	return nodes
}

// recordRoots is the node each record type's values live under, keyed by the
// name a values document names that record by.
//
// A name two record nodes share maps to nothing, so that the walk passes over
// it. Two records of one name are two shapes a document's record could have,
// and checking against whichever came first would refuse a document that is
// correct about the other.
func recordRoots(descriptor *irpb.Descriptor, nodes map[uint64]*irpb.Node) map[string]*irpb.Node {
	roots := map[string]*irpb.Node{}

	for _, node := range descriptor.GetNodes() {
		record := node.GetRecord()
		if record == nil {
			continue
		}

		name := record.GetNames().GetOriginal()

		if _, ok := roots[name]; ok {
			roots[name] = nil

			continue
		}

		roots[name] = nodes[record.GetRootId()]
	}

	return roots
}

// original is what the copybook calls a node, which is the key a values
// document writes it under.
func original(node *irpb.Node) string {
	switch kind := node.GetKind().(type) {
	case *irpb.Node_Group:
		return kind.Group.GetNames().GetOriginal()
	case *irpb.Node_Field:
		return kind.Field.GetNames().GetOriginal()
	default:
		return ""
	}
}
