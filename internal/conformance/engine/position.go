// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package engine

import (
	"fmt"
	"strings"

	"github.com/Zaba505/cpybkc/internal/conformance"
	"github.com/Zaba505/cpybkc/irpb"
)

// position is where the descriptor puts one value of a values document.
//
// An engine that says `packed-comp6: mismatch` has done the minimum. This is
// the rest of it: the engine holds the descriptor and the bytes, so it can say
// which item disagreed, where it sits, how wide it is and what encoding it was
// read under — which is available to the engine and to nothing else in the
// system, since the adapter was never told what was expected and the corpus
// package compares documents rather than bytes.
type position struct {
	// Path is the values-document path [conformance.Compare] names, and is what
	// a fault is looked up by.
	Path string

	// Name is what the copybook calls the item.
	Name string

	// Record is which record of the file it is in, counted from one as every
	// other report of a record counts.
	Record int

	// Offset is where the item begins inside its record, and Width is how many
	// bytes it takes. Both come from the position sum over the record's member
	// lists — the sum docs/ir/SPEC.md names as the thing every consumer runs and
	// every consumer may get wrong on its own, which is why the corpus exists.
	Offset int
	Width  int

	// Group is whether the item holds other items, in which case the encoding
	// axes below are its members' and not its own.
	Group bool

	// Usage, Charset and Sign are the axes an elementary item was read under.
	// They are named because a disagreement about a number is nearly always a
	// disagreement about one of them, and the entry that reports it is the one
	// that knows which of the four the item carried.
	Usage   string
	Charset string
	Sign    string

	// FileOffset is where the item's bytes are in input.bin, and Located says
	// whether that could be worked out at all: a record's start in the file is
	// determinate only where the framing lets records abut, and an item behind a
	// table whose count is read at run time slides with it.
	FileOffset int
	Located    bool
}

// String is the sentence a report carries beneath the disagreement.
func (p position) String() string {
	held := "bytes"
	if p.Width == 1 {
		held = "byte"
	}

	said := fmt.Sprintf("%s is %d %s at offset %d of record %d", p.Name, p.Width, held, p.Offset, p.Record)

	if p.Group {
		return said + ", and is a group"
	}

	return fmt.Sprintf("%s, usage %s, charset %s, sign convention %s", said, p.Usage, p.Charset, p.Sign)
}

// bytesLimit is how many of an item's bytes a report quotes.
//
// An item is usually a handful of bytes and occasionally a table thousands of
// bytes wide, and what makes the quote useful is that it fits on the line under
// the disagreement.
const bytesLimit = 16

// bytes is the item's own bytes, quoted from the entry's input where the
// framing let them be found.
//
// It is the item's run and not a window around it on purpose: a window has to
// choose a width and mark a position inside it, and every reader then has to
// count characters to check the marking. The offsets are written out instead, so
// what is quoted is unambiguous by construction.
func (p position) bytes(input []byte) (string, bool) {
	if !p.Located || p.Width == 0 {
		return "", false
	}

	end := p.FileOffset + p.Width
	if p.FileOffset < 0 || end > len(input) {
		// The item runs past the end of the file. That is a fact about the
		// entry rather than about the answer — and quoting whatever bytes there
		// happen to be would be quoting an item the file does not carry.
		return "", false
	}

	run := input[p.FileOffset:end]

	var said strings.Builder

	fmt.Fprintf(&said, "%s 0x%04x..0x%04x:", conformance.InputName, p.FileOffset, end-1)

	for _, b := range run[:min(len(run), bytesLimit)] {
		fmt.Fprintf(&said, " %02x", b)
	}

	if len(run) > bytesLimit {
		fmt.Fprintf(&said, " (and %d more)", len(run)-bytesLimit)
	}

	return said.String(), true
}

// positions is where the descriptor puts every value the entry states, keyed by
// the path [conformance.Compare] names it by.
//
// The walk is guided by the entry's own values document rather than by the
// descriptor alone, because the descriptor does not say how many occurrences a
// table holds when its count is read at run time, nor which arm of a variant an
// occurrence took. The entry states both, and it is the oracle here as
// everywhere else in the corpus.
//
// Nothing in it can fail. A descriptor and a values document that disagree about
// shape produce fewer positions rather than an error: this is what a report
// adds beneath a disagreement, and a report that refused to be written because
// the disagreement was structural would withhold the explanation exactly when it
// is most wanted.
func positions(entry *conformance.Entry) map[string]position {
	found := map[string]position{}

	if entry.Descriptor == nil || entry.Values == nil {
		return found
	}

	nodes := index(entry.Descriptor)

	walk := &walker{nodes: nodes, found: found}

	// Where one record's bytes end and the next record's begin is the file
	// node's, and only one of the four framings lets a reader place a record
	// without reading it: under unframed, a record begins at the byte after the
	// record before it. Under the other three there are framing bytes that
	// belong to the dataset and to no record, and this package deliberately does
	// not model them — an offset into input.bin that was wrong would be worse
	// than none at all, so the offsets within the record are still reported and
	// the bytes are not quoted.
	located := unframed(entry.Descriptor)
	file := 0

	for i, record := range entry.Values.Records {
		root := recordRoot(entry.Descriptor, nodes, record.Name)
		if root == nil {
			// A record the descriptor does not carry, which the loader already
			// refuses in an entry: nothing after it can be placed either, since
			// its width is unknown.
			return found
		}

		walk.record = i + 1
		walk.file = file
		walk.located = located

		width, ok := walk.node(root, record.Value, fmt.Sprintf("record %d %s", i+1, record.Name), 0)
		if !ok {
			// The shape of this record could not be followed, so where the next
			// one starts is not known either. What was placed before it stands.
			return found
		}

		file += width
	}

	return found
}

// index is every node of the descriptor by identifier, which is how every
// reference in it is resolved: a consumer MUST resolve a reference by
// identifier and MUST NOT look a node up by name, because duplicate data names
// are legal COBOL.
func index(descriptor *irpb.Descriptor) map[uint64]*irpb.Node {
	nodes := make(map[uint64]*irpb.Node, len(descriptor.GetNodes()))

	for _, node := range descriptor.GetNodes() {
		nodes[node.GetId()] = node
	}

	return nodes
}

// unframed is whether the descriptor's file node says records abut.
func unframed(descriptor *irpb.Descriptor) bool {
	for _, node := range descriptor.GetNodes() {
		if file := node.GetFile(); file != nil {
			return file.GetUnframed() != nil
		}
	}

	return false
}

// recordRoot is the top-level group of the record type the copybook calls name.
//
// The first record node of that name wins. Two record types cannot be told
// apart by a values document either — a record is named there and nowhere
// identified — so a descriptor carrying two of one name is a descriptor this
// explanation is approximate about, and the comparison it explains is not.
func recordRoot(descriptor *irpb.Descriptor, nodes map[uint64]*irpb.Node, name string) *irpb.Node {
	for _, node := range descriptor.GetNodes() {
		record := node.GetRecord()
		if record == nil || record.GetNames().GetOriginal() != name {
			continue
		}

		return nodes[record.GetRootId()]
	}

	return nil
}

// walker is one record's walk: the descriptor beside the values the entry
// states, accumulating positions as it goes.
type walker struct {
	nodes  map[uint64]*irpb.Node
	found  map[string]position
	record int

	// file is where this record begins in input.bin, and located is whether
	// that number means anything.
	file    int
	located bool
}

// node places one node of a record, and reports how many bytes it takes.
//
// The boolean is whether the width is known. A width that is not is not a
// failure — it is the point past which nothing else in the record can be
// placed, because every offset behind it would be a guess.
func (w *walker) node(node *irpb.Node, value any, path string, offset int) (int, bool) {
	switch kind := node.GetKind().(type) {
	case *irpb.Node_Slack:
		// Bytes that are part of the record and belong to no item: they take
		// their width and contribute no value and no path.
		return int(kind.Slack.GetWidth()), true
	case *irpb.Node_Group:
		return w.occurrences(kind.Group.GetRepetition(), value, path, offset,
			func(one any, path string, offset int) (int, bool) {
				width, ok := w.group(kind.Group, one, path, offset)

				w.place(path, kind.Group.GetNames().GetOriginal(), offset, width, ok, position{Group: true})

				return width, ok
			})
	case *irpb.Node_Field:
		return w.occurrences(kind.Field.GetRepetition(), value, path, offset,
			func(_ any, path string, offset int) (int, bool) {
				width := int(kind.Field.GetWidth())

				w.place(path, kind.Field.GetNames().GetOriginal(), offset, width, true, position{
					Usage:   trimmed("USAGE_", kind.Field.GetUsage().String()),
					Charset: trimmed("CHARSET_", kind.Field.GetEncoding().GetCharset().String()),
					Sign:    trimmed("SIGN_CONVENTION_", kind.Field.GetEncoding().GetSignConvention().String()),
				})

				return width, true
			})
	default:
		// A predicate, a state, a transition or anything else that is not an
		// item of a record. Nothing points at one from a member list, so
		// reaching here is a descriptor this walk does not understand rather
		// than a shape it can carry on through.
		return 0, false
	}
}

// occurrences places a node that may repeat, once per occurrence.
//
// How many there are comes from the descriptor where the count is a constant,
// and from the entry's own document where it is read at run time — the entry
// states what the file holds, which is exactly the question a variable count
// asks. A repetition whose count is neither is the end of the walk.
func (w *walker) occurrences(repetition *irpb.Repetition, value any, path string, offset int,
	one func(value any, path string, offset int) (int, bool),
) (int, bool) {
	if repetition == nil {
		return one(value, path, offset)
	}

	held, _ := value.([]any)

	count, ok := 0, false

	switch counted := repetition.GetCount().(type) {
	case *irpb.Repetition_Constant:
		count, ok = int(counted.Constant), true
	case *irpb.Repetition_Variable:
		// The count is read at run time, so the descriptor does not carry it and
		// the entry does: what the file holds is what values.json states.
		if held != nil {
			count, ok = len(held), true
		}
	}

	if !ok {
		return 0, false
	}

	total := 0

	for i := range count {
		var occurrence any
		if i < len(held) {
			occurrence = held[i]
		}

		width, ok := one(occurrence, fmt.Sprintf("%s[%d]", path, i), offset+total)
		if !ok {
			return 0, false
		}

		total += width
	}

	return total, true
}

// group places every member of a group in record order.
func (w *walker) group(group *irpb.Group, value any, path string, offset int) (int, bool) {
	// A value that is not an object is a shape the answer disagrees about, and
	// the members are still placed: their widths come from the descriptor, and
	// what the walk loses is only the arm a variant took and the length of a
	// table whose count is read at run time.
	held, _ := value.(map[string]any)

	total := 0

	for _, id := range group.GetMemberIds() {
		member, ok := w.nodes[id]
		if !ok {
			return 0, false
		}

		if variant := member.GetVariant(); variant != nil {
			width, ok := w.variant(variant, held, path, offset+total)
			if !ok {
				return 0, false
			}

			total += width

			continue
		}

		name := member.GetGroup().GetNames().GetOriginal()
		if field := member.GetField(); field != nil {
			name = field.GetNames().GetOriginal()
		}

		var value any
		if held != nil && name != "" {
			value = held[name]
		}

		memberPath := path
		if name != "" {
			memberPath = path + "." + name
		}

		width, ok := w.node(member, value, memberPath, offset+total)
		if !ok {
			return 0, false
		}

		total += width
	}

	return total, true
}

// variant places the arm an occurrence took.
//
// Every arm's extent equals every other arm's, which is what keeps a variant's
// contribution to the position sum constant — so the arm that was taken is
// needed for the paths beneath it and never for the width. Which arm that was
// is not in the descriptor: an arm the occurrence did not take contributes no
// key at all to the document, so the key that is there is the answer.
func (w *walker) variant(variant *irpb.Variant, held map[string]any, path string, offset int) (int, bool) {
	for _, arm := range variant.GetArms() {
		body, name, ok := w.arm(arm)
		if !ok {
			return 0, false
		}

		if value, taken := held[name]; taken {
			return w.node(body, value, path+"."+name, offset)
		}
	}

	// No arm's name is a key of the document. Either the entry states a shape
	// this walk did not follow, or the answer is about to be reported as
	// holding an arm the entry does not expect — and the first arm's extent is
	// the alternation's whichever it was, so the walk goes on. Placing that
	// arm's items is deliberate: a mismatch naming one of them is exactly the
	// answer that took the wrong arm, and it is the one worth explaining.
	arms := variant.GetArms()
	if len(arms) == 0 {
		return 0, false
	}

	body, name, ok := w.arm(arms[0])
	if !ok {
		return 0, false
	}

	return w.node(body, nil, path+"."+name, offset)
}

// arm resolves an arm to the node that is its body and the name that body
// carries, which is the key an occurrence writes when it took that arm.
func (w *walker) arm(arm *irpb.Arm) (*irpb.Node, string, bool) {
	var id uint64

	switch body := arm.GetBody().(type) {
	case *irpb.Arm_GroupId:
		id = body.GroupId
	case *irpb.Arm_FieldId:
		id = body.FieldId
	default:
		return nil, "", false
	}

	body, ok := w.nodes[id]
	if !ok {
		return nil, "", false
	}

	name := body.GetGroup().GetNames().GetOriginal()
	if field := body.GetField(); field != nil {
		name = field.GetNames().GetOriginal()
	}

	return body, name, true
}

// place records where an item sits, unless its width could not be worked out.
func (w *walker) place(path, name string, offset, width int, known bool, held position) {
	if !known || name == "" {
		return
	}

	held.Path = path
	held.Name = name
	held.Record = w.record
	held.Offset = offset
	held.Width = width
	held.FileOffset = w.file + offset
	held.Located = w.located

	w.found[path] = held
}

// trimmed is an enumerator's name without the prefix every one of its values
// carries, because a report reads better saying PACKED_DECIMAL than
// USAGE_PACKED_DECIMAL and the prefix is already in the word before it.
func trimmed(prefix, value string) string {
	return strings.TrimPrefix(value, prefix)
}
