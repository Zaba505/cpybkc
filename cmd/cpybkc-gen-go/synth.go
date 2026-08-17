// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	byteorder "encoding/binary"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/Zaba505/cobol-go/codec"
	"github.com/Zaba505/cpybkc/irpb"
)

// synth lays out one record's bytes at generation time and says, run by run,
// which item put each of them there.
//
// The bytes are written with cobol-go's own [codec.Writer] rather than with a
// second implementation of zoned, packed, binary and floating-point encoding.
// codec is already this module's runtime half and this generator already
// reasons about it — [coder.readCall] and [coder.writeCall] are the accessor
// table, [coder.profile] is the encoding — so re-deriving any of it here would
// be a second implementation of the thing the generated tests are about to
// exercise. What is left over for this file is the part codec cannot supply: a
// *rule* for which value each item takes.
//
// The walk is [coder.decodeGroup]'s, member for member and in the same order,
// because the bytes it lays down have to be the bytes that walk expects to
// read. Anywhere the two could drift — the occurrences of a table, the arm of a
// variant, the run retained for a slack node — this file says how it stays in
// step.
type synth struct {
	*coder

	// enc is the four axes as codec takes them, and charset is the one of them
	// the spelling of the literal turns on.
	enc     codec.Encoding
	charset irpb.Charset

	// out is the record's bytes and w is the writer filling it.
	out bytes.Buffer
	w   *codec.Writer

	// arms is the arm each variant takes in the case being laid out, by the
	// variant node's identifier. A variant the map says nothing about takes its
	// first arm.
	arms map[uint64]int

	// literal is the bytes a predicate requires of a field, by that field's
	// identifier. See [synth.discriminators].
	literal map[uint64][]byte

	// counts is the number of occurrences chosen for each OCCURS DEPENDING ON
	// count field, by that field's identifier. See [synth.chooseCounts].
	counts map[uint64]int

	// The file tier's, and nil at the record tier — where a record is laid out
	// on its own, no automaton has run in front of it and there is nothing for
	// any of these to say.
	//
	// admitting is the transition whose predicate this record's bytes have to
	// satisfy, which at the file tier is one edge rather than every edge
	// admitting the type. pick is which literal of a set-membership predicate
	// that edge is being covered for, by predicate node. pinned is the bytes a
	// register's guard requires of the field bound into it, and forced the
	// number one requires of a numeric one. registers is what each register
	// holds where the record is read, which is what sizes a table counted by
	// one. only is the fields whose values the case asserts.
	admitting *irpb.Transition
	pick      map[uint64]int
	pinned    map[uint64][]byte
	forced    map[uint64]int
	registers map[uint64]int
	only      map[uint64]struct{}

	// runs is the record's bytes divided by the item that wrote them, in
	// record order, each with the comment column that names it.
	runs []chunk

	// checks are the assertions the case makes about the decoded record, in the
	// order they are made. Each is a statement, or a small block of them.
	checks []string

	// needs are the import paths the assertions have asked for beyond the ones
	// every case takes.
	needs map[string]struct{}

	// order is the descriptor's node list as it stands, which is the order
	// docs/ir/SPEC.md's "Identity, ordering and determinism" fixes. The node
	// map cannot answer a question whose answer depends on order, and the
	// transition a record's discriminator comes from is one.
	order []*irpb.Node
}

// chunk is one item's bytes: what it wrote, and the comment column that says
// which item it was, where it starts and what its picture is.
type chunk struct {
	body []byte
	note string
}

// newSynth is a synthesizer over one descriptor's resolved encoding.
func newSynth(c *coder, d *irpb.Descriptor, enc *irpb.Encoding) (*synth, error) {
	profile, err := encodingValue(enc)
	if err != nil {
		return nil, err
	}

	return &synth{
		coder:   c,
		enc:     profile,
		charset: enc.GetCharset(),
		needs:   make(map[string]struct{}),
		order:   d.GetNodes(),
	}, nil
}

// encodingValue is the descriptor's four axes as codec's own value.
//
// The same four resolutions [coder.profile] writes into the generated package,
// made here as values rather than as source. They are read off one descriptor
// and cannot part company for that reason: a charset spelled one way in the
// generated Encoding and another way in the bytes the generated tests read
// would make every case fail for a reason that has nothing to do with the
// walk.
func encodingValue(enc *irpb.Encoding) (codec.Encoding, error) {
	if err := resolved(enc); err != nil {
		return codec.Encoding{}, err
	}

	var charset codec.Charset

	switch enc.GetCharset() {
	case irpb.Charset_CHARSET_CP037:
		charset = codec.CP037()
	case irpb.Charset_CHARSET_ASCII:
		charset = codec.ASCII()
	default:
		return codec.Encoding{}, &unsupportedCharsetError{Charset: enc.GetCharset()}
	}

	sign := codec.SignRealia

	switch enc.GetSignConvention() {
	case irpb.SignConvention_SIGN_CONVENTION_EBCDIC:
		sign = codec.SignEBCDIC
	case irpb.SignConvention_SIGN_CONVENTION_ASCII_ZONE37:
		sign = codec.SignASCIIZone37
	case irpb.SignConvention_SIGN_CONVENTION_TRANSLATED_EBCDIC:
		sign = codec.SignTranslatedEBCDIC
	}

	order := byteorder.ByteOrder(byteorder.BigEndian)
	if enc.GetByteOrder() == irpb.ByteOrder_BYTE_ORDER_LITTLE_ENDIAN {
		order = byteorder.LittleEndian
	}

	float := codec.FloatHFP
	if enc.GetFloatFormat() == irpb.FloatFormat_FLOAT_FORMAT_IEEE754 {
		float = codec.FloatIEEE
	}

	return codec.Encoding{Charset: charset, Sign: sign, ByteOrder: order, Float: float}, nil
}

// layOut is one case: the record's bytes, the assertions over the record they
// decode into, and nothing carried over from the case before.
func (s *synth) layOut(node *irpb.Node, expr string, arms map[uint64]int) error {
	record := node.GetRecord()

	s.out.Reset()
	s.runs, s.checks, s.arms = nil, nil, arms

	w, err := codec.NewWriter(&s.out, s.enc)
	if err != nil {
		return fmt.Errorf("laying out %s: %w", record.GetNames().GetOriginal(), err)
	}

	s.w = w

	// The discriminators first, because one of them may name a count field:
	// a predicate states the bytes that field holds outright, and a number of
	// occurrences chosen without reading it would be a number the literal
	// disagrees with. See [synth.chooseCounts].
	if err := s.discriminators(node.GetId(), record); err != nil {
		return err
	}

	if err := s.chooseCounts(record.GetRootId()); err != nil {
		return err
	}

	return s.walkGroup(record.GetRootId(), expr, "")
}

// chooseCounts fixes the number of occurrences of every table whose length is
// data, before a byte is written.
//
// The rule is the story's, and it is a rule rather than a number so that a
// regenerated case is a diff somebody can read: **a variable table takes its
// declared minimum, or one occurrence where that minimum is zero**. One
// occurrence rather than none, so that every shape the record carries appears
// in the literal at least once — a table nobody generates an occurrence of is a
// table whose item widths nobody checks — and the minimum rather than the
// maximum so that the literal stays short enough to read.
//
// One count may size two tables (docs/ir/SPEC.md, "One count may size two
// tables, and a writer refuses to choose"), and the generated writer reports a
// caller who supplies two different numbers for one count rather than choosing.
// So the number chosen here is one number per count field — the largest any of
// its tables asks for — and a count whose tables cannot agree on one is refused
// here rather than emitted as a case that cannot pass.
//
// A **discriminated** count field overrides all of that. A predicate states the
// bytes that field holds outright, and the emitted decoder reads its number of
// occurrences out of those bytes — so a number chosen here against the literal
// would be a literal the case cannot read back, and the failure would name
// neither the predicate nor the table. The literal's own number wins, and a
// literal outside a table's declared bounds is refused here rather than emitted.
//
// The walk follows the arms of a variant. [coder.collectCounts] does not, and
// the two are asking different questions: that one is the set of tables the
// generated *writer* derives a count from, and this one is the set of tables a
// case's bytes have to be laid out consistently with — which includes the ones
// inside an arm, because [coder.width] sizes an occurrence from the first arm
// whichever arm the occurrence holds.
func (s *synth) chooseCounts(rootID uint64) error {
	s.counts = make(map[uint64]int)

	uses := make(map[uint64][]*irpb.VariableCount)

	order, err := s.countUses(rootID, uses, nil)
	if err != nil {
		return err
	}

	for _, id := range order {
		node, ok := s.nodes[id]
		if !ok {
			return unresolved(id)
		}

		chosen, err := s.countFor(id, node, uses[id])
		if err != nil {
			return err
		}

		for _, use := range uses[id] {
			if chosen < int(use.GetMinOccurrences()) || chosen > int(use.GetMaxOccurrences()) {
				return malformed(fmt.Sprintf(
					"%s counts a table of a record and no one number of occurrences is inside every bound declared for it",
					originalOf(node)),
					"one count sizes every table naming it, so the range a record can carry is the overlap of the declared ones; see docs/ir/SPEC.md, \"One count may size two tables, and a writer refuses to choose\"")
			}
		}

		s.counts[id] = chosen
	}

	// A field an earlier transition binds into an integer register holds the
	// number the file tier chose for it, whether or not it also counts a table
	// of its own. Where it does, [synth.countFor] has already sized that table
	// from the same number, so the two cannot disagree.
	for _, id := range sortedKeys(s.forced) {
		s.counts[id] = s.forced[id]
	}

	return nil
}

// countFor is the number of occurrences one count field's tables are laid out
// with: what a predicate pins it to where one does, and the story's rule
// otherwise.
func (s *synth) countFor(id uint64, node *irpb.Node, uses []*irpb.VariableCount) (int, error) {
	// A number the file tier chose because an earlier transition binds this
	// field into a register a guard reads. It wins over the story's rule for
	// the reason a predicate's literal does: the value is fixed elsewhere, and
	// a table sized against it is a table the case cannot read back.
	if want, ok := s.forced[id]; ok {
		return want, nil
	}

	if want, ok := s.literal[id]; ok {
		field := node.GetField()
		if field == nil {
			return 0, malformed(fmt.Sprintf("node %d counts an OCCURS DEPENDING ON and is not a field node", id),
				"a count is a field of the record being read or a register an earlier transition bound")
		}

		value, err := s.readBack(field, want)
		if err != nil {
			return 0, err
		}

		number, numeric := integral(value)
		if !numeric {
			return 0, malformed(fmt.Sprintf(
				"a predicate pins %s, which counts an OCCURS DEPENDING ON, to bytes that are not a number",
				originalOf(node)),
				"a count is a numeric field; see docs/ir/SPEC.md, \"A count is in hand before the extent it decides\"")
		}

		return number, nil
	}

	chosen := 1

	for _, use := range uses {
		if n := int(use.GetMinOccurrences()); n > chosen {
			chosen = n
		}
	}

	return chosen, nil
}

// countUses is every repetition naming a count field that this case lays bytes
// down for, by that field's identifier, and the order the counts were first met
// in.
func (s *synth) countUses(id uint64, into map[uint64][]*irpb.VariableCount, order []uint64) ([]uint64, error) {
	members, err := s.flattened(id)
	if err != nil {
		return nil, err
	}

	for _, memberID := range members {
		member, ok := s.nodes[memberID]
		if !ok {
			return nil, unresolved(memberID)
		}

		var rep *irpb.Repetition

		switch kind := member.GetKind().(type) {
		case *irpb.Node_Field:
			rep = kind.Field.GetRepetition()
		case *irpb.Node_Group:
			rep = kind.Group.GetRepetition()
		case *irpb.Node_Variant:
			// Every arm, not only the one this case selects. An occurrence
			// holding a variant is read whole before any of it is decoded, and
			// [coder.width] sizes that read from the **first** arm whichever
			// arm the occurrence turns out to hold — so a count feeding a table
			// inside any arm governs the layout of every case, and choosing it
			// per case would make one case's bytes the wrong length for the
			// read the generated decoder makes.
			for _, a := range kind.Variant.GetArms() {
				body, err := s.armBody(a)
				if err != nil {
					return nil, err
				}

				if body.GetGroup() == nil {
					continue
				}

				order, err = s.countUses(body.GetId(), into, order)
				if err != nil {
					return nil, err
				}
			}

			continue
		default:
			continue
		}

		if variable, ok := rep.GetCount().(*irpb.Repetition_Variable); ok {
			if count, ok := variable.Variable.GetCount().(*irpb.VariableCount_FieldId); ok {
				if _, seen := into[count.FieldId]; !seen {
					order = append(order, count.FieldId)
				}

				into[count.FieldId] = append(into[count.FieldId], variable.Variable)
			}
		}

		if member.GetGroup() == nil {
			continue
		}

		order, err = s.countUses(memberID, into, order)
		if err != nil {
			return nil, err
		}
	}

	return order, nil
}

// discriminators is the bytes every predicate the case has to satisfy requires
// of the field it names.
//
// This is the one place this generator does at generation time what
// docs/ir/SPEC.md's *"A writer evaluates a predicate, it never inverts one"*
// forbids at run time, and the two are different acts. That rule binds a
// **writer**: the value a predicate tests is the caller's, over a record the
// caller built, so a writer checks it and reports a record satisfying none
// rather than quietly storing the literal the predicate wanted. There is no
// caller here. The descriptor states the literal outright, and laying it into a
// record this generator is inventing — so that the record is one the layout
// actually describes — decides nothing on anybody's behalf. Nothing emitted
// inverts a predicate; the emitted writer still refuses, and these tests are
// what show it accepting the record it should.
//
// Two sources, applied in that order. A transition's predicate is what tells
// this record from the others in the file, so a case that ignored it would show
// an adopter a run of bytes their reader would never admit. An arm's predicate
// is what selects the arm the case is *for*, so it is applied second and wins:
// where one field is both, the arm is the thing being covered.
func (s *synth) discriminators(id uint64, record *irpb.Record) error {
	s.literal = make(map[uint64][]byte)

	switch {
	case s.admitting != nil:
		// The file tier admits a record along one edge, and it is that edge's
		// predicate the bytes have to satisfy — not every edge admitting the
		// type. Two transitions may admit one record on two literals of one
		// field, and a case laid out for the second would be a record the
		// reader takes the first edge for.
		if s.admitting.PredicateId != nil {
			if err := s.require(s.admitting.GetPredicateId(), false); err != nil {
				return err
			}
		}
	default:
		for _, node := range s.order {
			transition := node.GetTransition()
			if transition == nil || transition.PredicateId == nil || transition.GetRecordId() != id {
				continue
			}

			if err := s.require(transition.GetPredicateId(), false); err != nil {
				return err
			}
		}
	}

	// What a guard requires of the field an earlier binding read it out of,
	// where the predicate has not already claimed that field. Second, and never
	// over a predicate: a predicate decides whether the record is admitted at
	// all, and a guard tested against a register the record cannot produce is a
	// path this file does not walk.
	for _, at := range sortedKeys(s.pinned) {
		if _, claimed := s.literal[at]; !claimed {
			s.literal[at] = s.pinned[at]
		}
	}

	return s.armLiterals(record.GetRootId())
}

// sortedKeys is a map's keys in ascending order, so that a walk over one is a
// walk two runs make alike.
func sortedKeys[V any](m map[uint64]V) []uint64 {
	out := make([]uint64, 0, len(m))
	for key := range m {
		out = append(out, key)
	}

	slices.Sort(out)

	return out
}

// armLiterals is the predicate literal of every arm the case selects.
func (s *synth) armLiterals(id uint64) error {
	members, err := s.flattened(id)
	if err != nil {
		return err
	}

	for _, memberID := range members {
		member, ok := s.nodes[memberID]
		if !ok {
			return unresolved(memberID)
		}

		switch kind := member.GetKind().(type) {
		case *irpb.Node_Group:
			if err := s.armLiterals(memberID); err != nil {
				return err
			}
		case *irpb.Node_Variant:
			chosen, err := s.chosenArm(memberID, kind.Variant)
			if err != nil {
				return err
			}

			if err := s.require(kind.Variant.GetArms()[chosen].GetPredicateId(), true); err != nil {
				return err
			}

			body, err := s.armBody(kind.Variant.GetArms()[chosen])
			if err != nil {
				return err
			}

			if body.GetGroup() != nil {
				if err := s.armLiterals(body.GetId()); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// require records the bytes a predicate demands of the field it names.
//
// A one-of predicate contributes its first literal. Every literal of one is
// covered by the file tier, where a transition is what a case is about; here
// the record is, and the first literal is the one a descriptor's own order
// puts first.
//
// over says whether this predicate may take a field another has already
// claimed. An arm's may — it is what the case is for — and a transition's may
// not, so that two transitions admitting one record leave the first one's
// literal standing rather than the last one's.
func (s *synth) require(id uint64, over bool) error {
	node, ok := s.nodes[id]
	if !ok {
		return unresolved(id)
	}

	predicate := node.GetPredicate()
	if predicate == nil {
		return malformed(fmt.Sprintf("node %d selects a record or an arm and is not a predicate node", id),
			"a transition and an arm each name the predicate that selects it; see docs/ir/SPEC.md, \"Discriminator predicates\"")
	}

	if _, claimed := s.literal[predicate.GetFieldId()]; claimed && !over {
		return nil
	}

	switch test := predicate.GetTest().(type) {
	case *irpb.Predicate_BytesEqual:
		s.literal[predicate.GetFieldId()] = test.BytesEqual.GetValue()
	case *irpb.Predicate_BytesOneOf:
		if len(test.BytesOneOf.GetValues()) < 2 {
			return malformed("a one-of predicate carries fewer than two literals",
				"a producer MUST carry at least two and MUST NOT carry the same literal twice; see docs/ir/SPEC.md, \"Discriminator predicates\"")
		}

		// The first literal at the record tier, where the record is what a case
		// is about; the one the file tier is covering where a transition is.
		// Every literal of a set is the file tier's to reach, one case each.
		at := s.pick[id]
		if at < 0 || at >= len(test.BytesOneOf.GetValues()) {
			at = 0
		}

		s.literal[predicate.GetFieldId()] = test.BytesOneOf.GetValues()[at]
	default:
		return malformed("a predicate carries no test",
			"the set is closed and a predicate carries one member of it; see docs/ir/SPEC.md, \"Discriminator predicates\"")
	}

	return nil
}

// chosenArm is the arm of a variant this case selects, which is the first one
// unless the case says otherwise.
func (s *synth) chosenArm(id uint64, v *irpb.Variant) (int, error) {
	if len(v.GetArms()) < 2 {
		return 0, malformed(fmt.Sprintf("a variant carries %d arms", len(v.GetArms())),
			"a producer must not emit a variant carrying fewer than two arms; see docs/ir/SPEC.md, \"A variant is chosen once per occurrence\"")
	}

	chosen := s.arms[id]
	if chosen < 0 || chosen >= len(v.GetArms()) {
		chosen = 0
	}

	return chosen, nil
}

// walkGroup lays out one occurrence of the group id, member by member, in the
// order [coder.decodeGroup] reads them.
//
// expr is the Go expression of the occurrence in the decoded record and item is
// the copybook's path to it, which is what the comment column and every failure
// message name.
func (s *synth) walkGroup(id uint64, expr, item string) error {
	in, err := s.groupName(id)
	if err != nil {
		return err
	}

	members, err := s.flattened(id)
	if err != nil {
		return err
	}

	for _, memberID := range members {
		member, ok := s.nodes[memberID]
		if !ok {
			return unresolved(memberID)
		}

		// An item the copybook gives no data-name is a run of retained bytes
		// rather than a field, exactly as a slack node is, and it is met here
		// before the switch for the same reason the decoder meets it here.
		if field := member.GetField(); field != nil && anonymous(field.GetNames()) {
			width, err := fillerRun(field, in)
			if err != nil {
				return err
			}

			note, err := fillerNote(s.emitter, field)
			if err != nil {
				return err
			}

			if err := s.opaque(int(width), qualify(item, "FILLER"), note); err != nil {
				return err
			}

			continue
		}

		switch kind := member.GetKind().(type) {
		case *irpb.Node_Slack:
			if err := s.opaque(int(kind.Slack.GetWidth()), qualify(item, "(slack)"),
				plural(int(kind.Slack.GetWidth()), "byte")+" no item covers"); err != nil {
				return err
			}
		case *irpb.Node_Field:
			if err := s.walkField(memberID, kind.Field, expr, item); err != nil {
				return err
			}
		case *irpb.Node_Group:
			if err := s.walkNested(memberID, kind.Group, expr, item); err != nil {
				return err
			}
		case *irpb.Node_Variant:
			if err := s.walkVariant(memberID, kind.Variant, expr, item); err != nil {
				return err
			}
		default:
			return malformed(fmt.Sprintf("node %d is not something a group may contain", memberID),
				"a member list names a group, variant, field or slack node; see docs/ir/SPEC.md, \"The node kinds\"")
		}
	}

	return nil
}

// walkField lays out an elementary item, once or once per occurrence.
func (s *synth) walkField(id uint64, f *irpb.Field, expr, item string) error {
	name, err := identifier("field", f.GetNames())
	if err != nil {
		return err
	}

	cobol := f.GetNames().GetOriginal()
	target := expr + "." + name

	n, table, err := s.occurrences(f.GetRepetition(), qualify(item, cobol), target)
	if err != nil {
		return err
	}

	if !table {
		return s.field(id, f, target, qualify(item, cobol))
	}

	for i := range n {
		if err := s.field(id, f, fmt.Sprintf("%s[%d]", target, i),
			fmt.Sprintf("%s(%d)", qualify(item, cobol), i+1)); err != nil {
			return err
		}
	}

	return nil
}

// walkNested lays out a group member, once or once per occurrence.
func (s *synth) walkNested(id uint64, g *irpb.Group, expr, item string) error {
	name, err := identifier("group", g.GetNames())
	if err != nil {
		return err
	}

	cobol := g.GetNames().GetOriginal()
	target := expr + "." + name

	n, table, err := s.occurrences(g.GetRepetition(), qualify(item, cobol), target)
	if err != nil {
		return err
	}

	if !table {
		return s.walkGroup(id, target, qualify(item, cobol))
	}

	for i := range n {
		if err := s.walkGroup(id, fmt.Sprintf("%s[%d]", target, i),
			fmt.Sprintf("%s(%d)", qualify(item, cobol), i+1)); err != nil {
			return err
		}
	}

	return nil
}

// walkVariant lays out the arm this case selects and asserts that the record
// holds that arm and no other.
func (s *synth) walkVariant(id uint64, v *irpb.Variant, expr, item string) error {
	chosen, err := s.chosenArm(id, v)
	if err != nil {
		return err
	}

	var (
		body *irpb.Node
		at   string
	)

	for i, a := range v.GetArms() {
		arm, err := s.armBody(a)
		if err != nil {
			return err
		}

		if err := namedArm(arm, item); err != nil {
			return err
		}

		name, err := identifier("arm", namesOf(arm))
		if err != nil {
			return err
		}

		if i == chosen {
			body, at = arm, expr+"."+name

			if s.only == nil {
				s.checks = append(s.checks, fmt.Sprintf(
					"if %s == nil {\nt.Fatalf(%q)\n}", at,
					qualify(item, originalOf(arm))+": the record holds no arm, and its bytes select this one"))
			}

			continue
		}

		if s.only == nil {
			s.checks = append(s.checks, fmt.Sprintf(
				"if %s != nil {\nt.Errorf(%q)\n}", expr+"."+name,
				qualify(item, originalOf(arm))+": the record holds this arm, and its bytes select another"))
		}
	}

	if body.GetGroup() != nil {
		return s.walkGroup(body.GetId(), at, qualify(item, originalOf(body)))
	}

	// The one arm shape that is not a group. The decoder reads it through the
	// pointer, so this asserts through the pointer too; the nil check above is
	// a Fatalf for exactly this dereference.
	return s.field(body.GetId(), body.GetField(), "*"+at, qualify(item, originalOf(body)))
}

// occurrences is how many times a member of a record is laid out, and whether
// it is indexed at all.
//
// A table whose count is a **register** takes none, and that is not a choice
// this file makes. A register holds what a transition bound out of a record
// already read, and the decode method has no register file — the automaton
// lives in the file-level reader (#52) — so the occurrences it reads are the
// ones the record it was handed already carries. A record decoded from nothing
// but bytes carries none, so a case laying down more than zero would be a case
// whose bytes the generated decoder does not read.
func (s *synth) occurrences(rep *irpb.Repetition, item, target string) (int, bool, error) {
	if rep == nil {
		return 1, false, nil
	}

	switch count := rep.GetCount().(type) {
	case *irpb.Repetition_Constant:
		return int(count.Constant), true, nil
	case *irpb.Repetition_Variable:
		n := 0

		switch count := count.Variable.GetCount().(type) {
		case *irpb.VariableCount_FieldId:
			n = s.counts[count.FieldId]
		case *irpb.VariableCount_RegisterId:
			// At the file tier the automaton has run, so the register holds a
			// number and the generated reader sizes the table from it. At the
			// record tier [synth.registers] is nil and this is none, which is
			// the paragraph above.
			n = s.registers[count.RegisterId]
		}

		if s.only == nil {
			// A slice, so its length is something the case states before it
			// indexes into it — and a Fatalf, because everything after it does
			// index.
			s.checks = append(s.checks, fmt.Sprintf(
				"if len(%s) != %d {\nt.Fatalf(%q, len(%s), %d)\n}",
				target, n, item+": got %d occurrences, want %d", target, n))
		}

		return n, true, nil
	default:
		return 0, false, malformed("an item repeats and says nothing about how many times",
			"a repetition carries a constant count or an OCCURS DEPENDING ON one; an item that does not repeat carries no repetition at all")
	}
}

// opaque lays down a run of bytes no field of the record holds: a slack node,
// or an item the copybook gives no data-name.
//
// None of the bytes is a zero and none is either charset's space, and that is
// the whole of what makes docs/ir/SPEC.md's *"Slack survives a read"* an
// assertion rather than a coincidence. A writer that filled the run instead of
// emitting what was read would emit zeros — [zeroFillDeclaration] is exactly
// that run — so a case whose slack were zeros would pass whether the bytes
// survived or not.
func (s *synth) opaque(width int, item, picture string) error {
	at := int(s.w.Offset())

	body := make([]byte, width)
	for i := range body {
		body[i] = opaqueByte(at + i)
	}

	// Reported rather than discarded, and by the same route [synth.field]
	// reports its own: the io.Writer under this one is a bytes.Buffer, so an
	// I/O failure is not the thing being guarded against — a run codec itself
	// refuses is, and a refusal swallowed here would come out as a literal
	// silently short with no diagnostic anywhere.
	if err := s.w.WriteBytes(body); err != nil {
		return fmt.Errorf("laying out %s: %w", item, err)
	}

	s.runs = append(s.runs, chunk{body: body, note: note(item, at, picture)})

	return nil
}

// opaqueByte is one byte of a run nothing names, chosen by where it sits.
//
// The range is 0xA0 to 0xEF: never zero, which is what a writer filling the run
// would emit, and never 0x20 or 0x40, which are the space in the two charsets
// codec ships a table for. Derived from the offset so that a run written back
// in the wrong place is visible in the failure rather than merely unequal.
func opaqueByte(at int) byte { return byte(0xA0 + at%0x50) }

// field lays down one elementary item and records the assertion its value
// makes.
func (s *synth) field(id uint64, f *irpb.Field, target, item string) error {
	if err := resolved(f.GetEncoding()); err != nil {
		return err
	}

	at := int(s.w.Offset())

	value, err := s.value(id, f, at)
	if err != nil {
		return err
	}

	if err := s.write(f, value); err != nil {
		return fmt.Errorf("laying out %s: %w", item, err)
	}

	body := append([]byte(nil), s.out.Bytes()[at:]...)

	if want, ok := s.literal[id]; ok && !bytes.Equal(body, want) {
		return malformed(fmt.Sprintf(
			"a predicate requires %s to hold %q, and those are not the bytes the item writes for the value they read back as",
			f.GetNames().GetOriginal(), string(want)),
			"a discriminator literal is a value the field it names can hold and write back unchanged; see docs/ir/SPEC.md, \"Discriminator predicates\"")
	}

	s.runs = append(s.runs, chunk{body: body, note: note(item, at, pictureNote(f))})

	if !s.asserts(id) {
		return nil
	}

	check, err := s.assertion(f, target, item, value)
	if err != nil {
		return err
	}

	s.checks = append(s.checks, check)

	return nil
}

// asserts is whether a case states the value one item holds.
//
// Every one of them at the record tier, where a record is what the case is
// about. At the file tier only the field the transition's predicate names: the
// case is about the framing and the order records come in, each record already
// has a case of its own one file over, and a whole ledger's fields written out
// record by record is a literal nobody reads to the end. See README.md,
// "Decided: the file tier asserts the record types, their order and the
// discriminator".
func (s *synth) asserts(id uint64) bool {
	if s.only == nil {
		return true
	}

	_, want := s.only[id]

	return want
}

// value is the value one item holds in a case: the one a predicate requires of
// it, or the one this generator's rule gives it.
//
// The rule, which is written down in README.md because a reviewer reading a
// regenerated golden needs to know what should have changed:
//
//   - a **discriminated** field holds the literal its predicate requires, read
//     back through codec so that the case asserts the value those bytes mean
//     rather than the bytes themselves;
//   - everything else holds a value derived from the item's own picture and its
//     position in the record, so that nothing comes from the clock, the
//     environment or a path, and so that a reader can tell at a glance which run
//     of bytes is which item.
func (s *synth) value(id uint64, f *irpb.Field, at int) (any, error) {
	if want, ok := s.literal[id]; ok {
		if len(want) != int(f.GetWidth()) {
			return nil, malformed(fmt.Sprintf(
				"a predicate tests %s, which is %s wide, against a literal of %d",
				f.GetNames().GetOriginal(), plural(int(f.GetWidth()), "byte"), len(want)),
				"a predicate compares the whole of the field it names; see docs/ir/SPEC.md, \"Discriminator predicates\"")
		}

		return s.readBack(f, want)
	}

	if count, ok := s.counts[id]; ok {
		// A count field holds the number of occurrences its tables were laid
		// out with, and nothing else will do: the generated writer derives the
		// count it emits from len() of those tables, so a count field holding
		// anything else is a case whose bytes cannot come back.
		return s.number(f, big.NewInt(int64(count)))
	}

	switch f.GetUsage() {
	case irpb.Usage_USAGE_COMP_1:
		return float32(at+1) + 0.5, nil
	case irpb.Usage_USAGE_COMP_2:
		return float64(at+1) + 0.5, nil
	case irpb.Usage_USAGE_INDEX, irpb.Usage_USAGE_POINTER, irpb.Usage_USAGE_NATIONAL:
		body := make([]byte, f.GetWidth())
		for i := range body {
			body[i] = opaqueByte(at + i)
		}

		return body, nil
	case irpb.Usage_USAGE_DISPLAY:
		if f.GetPicture().GetCategory() != irpb.Category_CATEGORY_NUMERIC {
			if _, err := s.fieldType(f); err != nil {
				return nil, err
			}

			return textValue(at, int(f.GetWidth())), nil
		}
	}

	return s.number(f, magnitude(at, f.GetPicture()))
}

// textValue is what an item with no numeric value of its own holds: one letter,
// repeated across the whole of the item, chosen by where the item sits.
//
// A run of one letter rather than a word, because the two things a reader is
// holding the literal against are the item's **width** and its **offset**, and
// both are visible at a glance in a run: a field one byte too narrow is a run
// one character short, and a field at the wrong offset is a run that starts on
// the wrong letter. A word would spell neither and would have to be truncated
// to a width no copybook chose.
func textValue(at, width int) string {
	return strings.Repeat(string(rune('A'+at%26)), width)
}

// magnitude is the number an item with no predicate over it holds.
//
// The item's own position, plus one so that no item holds a zero — a zero is
// the value a field nothing wrote also holds, and a case asserting one asserts
// nothing — reduced to what the picture's digit count admits, and negative
// where the picture carries an S. Negative rather than positive on a signed
// item because the sign is the half of a zoned or packed field a positive value
// never exercises: an overpunched sign is a byte a reader of the literal wants
// to see.
func magnitude(at int, p *irpb.Picture) *big.Int {
	digits := int(p.GetDigits())
	if digits == 0 {
		return big.NewInt(0)
	}

	n := big.NewInt(int64(at) + 1)

	if digits < 19 {
		n.Mod(n, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil))

		if n.Sign() == 0 {
			n.SetInt64(1)
		}
	}

	if p.GetSigned() {
		n.Neg(n)
	}

	return n
}

// number is a magnitude in the Go type the item's accessor takes, which is the
// type [emitter.fieldType] gave the field.
func (s *synth) number(f *irpb.Field, n *big.Int) (any, error) {
	typ, err := s.fieldType(f)
	if err != nil {
		return nil, err
	}

	switch typ {
	case "int16":
		return int16(n.Int64()), nil
	case "int32":
		return int32(n.Int64()), nil
	case "int64":
		return n.Int64(), nil
	case "uint64":
		return n.Uint64(), nil
	case bigIntType:
		return n, nil
	default:
		return nil, malformed(fmt.Sprintf("%s is numeric and its Go type is %s", f.GetNames().GetOriginal(), typ),
			"a numeric item takes one of the integer types codec's accessors return; see README.md")
	}
}

// write is the one codec accessor the item is written with, chosen by the same
// table [coder.writeCall] emits a call to.
//
// The two are the same table read twice and that is the point: what the
// generated encoder will call to write this item is what lays the bytes down
// here, so a case's bytes are the bytes that item produces rather than bytes
// this file believes it produces.
func (s *synth) write(f *irpb.Field, value any) error {
	picture := f.GetPicture()

	switch f.GetUsage() {
	case irpb.Usage_USAGE_COMP_1:
		v, ok := value.(float32)
		if !ok {
			return mistyped(f, value, "float32")
		}

		return s.w.WriteFloat32(v)
	case irpb.Usage_USAGE_COMP_2:
		v, ok := value.(float64)
		if !ok {
			return mistyped(f, value, "float64")
		}

		return s.w.WriteFloat64(v)
	case irpb.Usage_USAGE_INDEX, irpb.Usage_USAGE_POINTER, irpb.Usage_USAGE_NATIONAL:
		v, ok := value.([]byte)
		if !ok {
			return mistyped(f, value, "[]byte")
		}

		return s.w.WriteBytes(v)
	case irpb.Usage_USAGE_DISPLAY:
		if picture.GetCategory() != irpb.Category_CATEGORY_NUMERIC {
			v, ok := value.(string)
			if !ok {
				return mistyped(f, value, "string")
			}

			return s.w.WriteAlphanumeric(v, int(f.GetWidth()))
		}

		position, err := signPositionValue(f)
		if err != nil {
			return err
		}

		switch v := value.(type) {
		case int32:
			return s.w.WriteZonedInt32(v, int(picture.GetDigits()), position)
		case int64:
			return s.w.WriteZonedInt64(v, int(picture.GetDigits()), position)
		case *big.Int:
			return s.w.WriteZonedBig(v, int(picture.GetDigits()), position)
		default:
			return mistyped(f, value, "int32, int64 or *big.Int")
		}
	case irpb.Usage_USAGE_PACKED_DECIMAL:
		if err := numeric(f); err != nil {
			return err
		}

		switch v := value.(type) {
		case int32:
			return s.w.WritePackedInt32(v, int(picture.GetDigits()), signednessValue(f))
		case int64:
			return s.w.WritePackedInt64(v, int(picture.GetDigits()), signednessValue(f))
		case *big.Int:
			return s.w.WritePackedBig(v, int(picture.GetDigits()), signednessValue(f))
		default:
			return mistyped(f, value, "int32, int64 or *big.Int")
		}
	case irpb.Usage_USAGE_COMP_6:
		if err := unsignedPacked(f); err != nil {
			return err
		}

		switch v := value.(type) {
		case int32:
			return s.w.WriteComp6Int32(v, int(picture.GetDigits()))
		case int64:
			return s.w.WriteComp6Int64(v, int(picture.GetDigits()))
		case *big.Int:
			return s.w.WriteComp6Big(v, int(picture.GetDigits()))
		default:
			return mistyped(f, value, "int32, int64 or *big.Int")
		}
	case irpb.Usage_USAGE_BINARY, irpb.Usage_USAGE_COMP_5:
		if err := numeric(f); err != nil {
			return err
		}

		return s.writeBinary(f, value)
	default:
		return malformed(fmt.Sprintf("an item carries USAGE %d, which this generator does not know", int32(f.GetUsage())),
			"docs/ir/SPEC.md requires a consumer to refuse a member of a closed set it does not recognise rather than fall back to one it does")
	}
}

// mistyped is the refusal a value of the wrong Go type gets.
//
// Two tables decide that type — [emitter.fieldType], which the struct emitter
// and [synth.number] read, and this one, which the accessor is chosen from —
// and they agreeing is an invariant nothing enforces on its own. So a
// disagreement is a diagnostic rather than a panic: a plugin that panics reports
// a broken pipe to cpybkc, and what an adopter would then be looking at is a
// crash where a sentence about their copybook belongs.
func mistyped(f *irpb.Field, value any, want string) error {
	return malformed(fmt.Sprintf("%s is laid out from a %T and its accessor takes %s",
		f.GetNames().GetOriginal(), value, want),
		"the Go type an item takes is README.md's table, and one table decides it for the struct field and for the accessor alike")
}

// writeBinary is [synth.write]'s binary half, which has one more family than
// the others: an unsigned item is written through codec's uint64 accessor.
func (s *synth) writeBinary(f *irpb.Field, value any) error {
	var (
		digits = int(f.GetPicture().GetDigits())
		sign   = signednessValue(f)
		comp5  = f.GetUsage() == irpb.Usage_USAGE_COMP_5
	)

	switch v := value.(type) {
	case int16:
		if comp5 {
			return s.w.WriteComp5Int16(v, digits, sign)
		}

		return s.w.WriteBinaryInt16(v, digits, sign)
	case int32:
		if comp5 {
			return s.w.WriteComp5Int32(v, digits, sign)
		}

		return s.w.WriteBinaryInt32(v, digits, sign)
	case int64:
		if comp5 {
			return s.w.WriteComp5Int64(v, digits, sign)
		}

		return s.w.WriteBinaryInt64(v, digits, sign)
	case uint64:
		if comp5 {
			return s.w.WriteComp5Uint64(v, digits, sign)
		}

		return s.w.WriteBinaryUint64(v, digits, sign)
	case *big.Int:
		if comp5 {
			return s.w.WriteComp5Big(v, digits, sign)
		}

		return s.w.WriteBinaryBig(v, digits, sign)
	default:
		return mistyped(f, value, "int16, int32, int64, uint64 or *big.Int")
	}
}

// readBack is what a predicate's literal means as a value, read through the
// accessor the generated decoder will read that field with.
//
// A predicate names bytes and a case asserts values, and this is the one place
// the two meet. Read through codec rather than interpreted here for the reason
// the layout is written through codec: an interpretation of its own would be a
// second implementation of the encoding, and a literal it disagreed with codec
// about would produce a case that cannot pass.
func (s *synth) readBack(f *irpb.Field, want []byte) (any, error) {
	r, err := codec.NewReader(bytes.NewReader(want), s.enc)
	if err != nil {
		return nil, err
	}

	value, err := s.read(r, f)
	if err != nil {
		return nil, malformed(fmt.Sprintf("a predicate tests %s against %q, which the item cannot hold: %v",
			f.GetNames().GetOriginal(), string(want), err),
			"a discriminator literal is a value the field it names can hold; see docs/ir/SPEC.md, \"Discriminator predicates\"")
	}

	return value, nil
}

// read is [coder.readCall]'s table, called rather than emitted.
func (s *synth) read(r *codec.Reader, f *irpb.Field) (any, error) {
	picture := f.GetPicture()

	switch f.GetUsage() {
	case irpb.Usage_USAGE_COMP_1:
		return r.ReadFloat32()
	case irpb.Usage_USAGE_COMP_2:
		return r.ReadFloat64()
	case irpb.Usage_USAGE_INDEX, irpb.Usage_USAGE_POINTER, irpb.Usage_USAGE_NATIONAL:
		return r.ReadBytes(int(f.GetWidth()))
	case irpb.Usage_USAGE_DISPLAY:
		if picture.GetCategory() != irpb.Category_CATEGORY_NUMERIC {
			if _, err := s.fieldType(f); err != nil {
				return nil, err
			}

			return r.ReadAlphanumeric(int(f.GetWidth()))
		}

		position, err := signPositionValue(f)
		if err != nil {
			return nil, err
		}

		switch decimalFamily(picture.GetDigits()) {
		case "Int32":
			return r.ReadZonedInt32(int(picture.GetDigits()), position)
		case "Int64":
			return r.ReadZonedInt64(int(picture.GetDigits()), position)
		default:
			return r.ReadZonedBig(int(picture.GetDigits()), position)
		}
	case irpb.Usage_USAGE_PACKED_DECIMAL:
		if err := numeric(f); err != nil {
			return nil, err
		}

		switch decimalFamily(picture.GetDigits()) {
		case "Int32":
			return r.ReadPackedInt32(int(picture.GetDigits()))
		case "Int64":
			return r.ReadPackedInt64(int(picture.GetDigits()))
		default:
			return r.ReadPackedBig(int(picture.GetDigits()))
		}
	case irpb.Usage_USAGE_COMP_6:
		if err := unsignedPacked(f); err != nil {
			return nil, err
		}

		switch decimalFamily(picture.GetDigits()) {
		case "Int32":
			return r.ReadComp6Int32(int(picture.GetDigits()))
		case "Int64":
			return r.ReadComp6Int64(int(picture.GetDigits()))
		default:
			return r.ReadComp6Big(int(picture.GetDigits()))
		}
	case irpb.Usage_USAGE_BINARY, irpb.Usage_USAGE_COMP_5:
		if err := numeric(f); err != nil {
			return nil, err
		}

		return s.readBinary(r, f)
	default:
		return nil, malformed(fmt.Sprintf("an item carries USAGE %d, which this generator does not know", int32(f.GetUsage())),
			"docs/ir/SPEC.md requires a consumer to refuse a member of a closed set it does not recognise rather than fall back to one it does")
	}
}

// readBinary is [synth.read]'s binary half.
func (s *synth) readBinary(r *codec.Reader, f *irpb.Field) (any, error) {
	var (
		digits = int(f.GetPicture().GetDigits())
		comp5  = f.GetUsage() == irpb.Usage_USAGE_COMP_5
	)

	switch binaryFamily(f.GetPicture()) {
	case "Int16":
		if comp5 {
			return r.ReadComp5Int16(digits)
		}

		return r.ReadBinaryInt16(digits)
	case "Int32":
		if comp5 {
			return r.ReadComp5Int32(digits)
		}

		return r.ReadBinaryInt32(digits)
	case "Int64":
		if comp5 {
			return r.ReadComp5Int64(digits)
		}

		return r.ReadBinaryInt64(digits)
	case "Uint64":
		if comp5 {
			return r.ReadComp5Uint64(digits)
		}

		return r.ReadBinaryUint64(digits)
	default:
		if comp5 {
			return r.ReadComp5Big(digits)
		}

		return r.ReadBinaryBig(digits)
	}
}

// signPositionValue is [signPosition] as codec's value rather than as the
// source naming it.
func signPositionValue(f *irpb.Field) (codec.SignPosition, error) {
	switch f.GetPicture().GetSignPosition() {
	case irpb.SignPosition_SIGN_POSITION_LEADING:
		return codec.SignLeading, nil
	case irpb.SignPosition_SIGN_POSITION_TRAILING:
		return codec.SignTrailing, nil
	case irpb.SignPosition_SIGN_POSITION_LEADING_SEPARATE:
		return codec.SignLeadingSeparate, nil
	case irpb.SignPosition_SIGN_POSITION_TRAILING_SEPARATE:
		return codec.SignTrailingSeparate, nil
	default:
		if _, err := signPosition(f); err != nil {
			return codec.SignUnsigned, err
		}

		return codec.SignUnsigned, nil
	}
}

// signednessValue is [signedness] as codec's value.
func signednessValue(f *irpb.Field) codec.Signedness {
	if f.GetPicture().GetSigned() {
		return codec.Signed
	}

	return codec.Unsigned
}

// assertion is the statement a case makes about one decoded field.
//
// One statement per field, naming the copybook's word for the item, the value
// read and the value the bytes were laid out with. Verbose, and deliberately so
// — a failing generated case is read by somebody who did not write it, and
// everything they need is on the line that failed.
func (s *synth) assertion(f *irpb.Field, target, item string, value any) (string, error) {
	switch v := value.(type) {
	case string:
		return fmt.Sprintf("if %s != %s {\nt.Errorf(%q, %s, %s)\n}",
			target, strconv.Quote(v), item+": got %q, want %q", target, strconv.Quote(v)), nil
	case int16:
		return integerAssertion(target, item, strconv.FormatInt(int64(v), 10)), nil
	case int32:
		return integerAssertion(target, item, strconv.FormatInt(int64(v), 10)), nil
	case int64:
		return integerAssertion(target, item, strconv.FormatInt(v, 10)), nil
	case uint64:
		return integerAssertion(target, item, strconv.FormatUint(v, 10)), nil
	case float32:
		return fmt.Sprintf("if %s != %s {\nt.Errorf(%q, %s, %s)\n}",
			target, strconv.FormatFloat(float64(v), 'g', -1, 32), item+": got %v, want %v",
			target, strconv.FormatFloat(float64(v), 'g', -1, 32)), nil
	case float64:
		return fmt.Sprintf("if %s != %s {\nt.Errorf(%q, %s, %s)\n}",
			target, strconv.FormatFloat(v, 'g', -1, 64), item+": got %v, want %v",
			target, strconv.FormatFloat(v, 'g', -1, 64)), nil
	case []byte:
		return fmt.Sprintf("if want := %s; !bytes.Equal(%s, want) {\nt.Errorf(%q, %s, want)\n}",
			byteSlice(v), target, item+": got % x, want % x", target), nil
	case *big.Int:
		s.needs[bigIntImport] = struct{}{}

		return fmt.Sprintf(
			"if want, _ := new(big.Int).SetString(%s, 10); %s == nil || %s.Cmp(want) != 0 {\nt.Errorf(%q, %s, want)\n}",
			strconv.Quote(v.String()), target, target, item+": got %v, want %v", target), nil
	default:
		return "", mistyped(f, value, "one of the Go types README.md's table gives an item")
	}
}

// integerAssertion is [synth.assertion] for the four integer types codec's
// accessors return, which differ only in how the literal is spelled.
func integerAssertion(target, item, want string) string {
	return fmt.Sprintf("if %s != %s {\nt.Errorf(%q, %s, %s)\n}",
		target, want, item+": got %d, want %d", target, want)
}

// byteSlice is a run of bytes as a Go composite literal.
func byteSlice(body []byte) string {
	parts := make([]string, 0, len(body))
	for _, b := range body {
		parts = append(parts, fmt.Sprintf("0x%02x", b))
	}

	return "[]byte{" + strings.Join(parts, ", ") + "}"
}

// note is the comment column of one run: the copybook's path to the item, the
// offset this generator summed, and the item's picture.
//
// The offset is the point of it. docs/ir/SPEC.md's *"Ordering and width, and no
// offset"* means the IR carries no offset anywhere, so every one of these is a
// sum of the widths ahead of it — which is exactly the arithmetic an adopter is
// holding the literal against their own file to check.
func note(item string, at int, picture string) string {
	return fmt.Sprintf("%s @%d %s", item, at, picture)
}

// qualify is the copybook's path to an item, one qualifier at a time.
func qualify(item, name string) string {
	if item == "" {
		return name
	}

	return item + "." + name
}

// pictureNote is how an item's picture reads in a comment column.
//
// A rendering for a person and nothing else: it is composed from the attributes
// resolve already resolved — the category, the digit count, the scale, the S —
// rather than read from a PICTURE this generator has never seen, because the IR
// carries no PICTURE string. Nothing decodes from it and nothing derives a
// width from it; docs/ir/SPEC.md's "Dereferencing is not recomputation" is
// about the second, and this is the first.
func pictureNote(f *irpb.Field) string {
	picture := f.GetPicture()
	if picture == nil {
		return usageName(f.GetUsage())
	}

	var base string

	switch picture.GetCategory() {
	case irpb.Category_CATEGORY_NUMERIC:
		if picture.GetSigned() {
			base = "S"
		}

		// The scale is spelled the way a copybook spells it, which is the
		// opposite sign from the one the IR carries: the item's value is the
		// stored one times 10 to the minus scale, so a positive scale is that
		// many digits behind an implied decimal point and a negative one is
		// that many places the point sits beyond the digits.
		switch scale := int(picture.GetScale()); {
		case scale > 0 && scale < int(picture.GetDigits()):
			base += fmt.Sprintf("9(%d)V9(%d)", int(picture.GetDigits())-scale, scale)
		case scale > 0:
			base += fmt.Sprintf("V9(%d)", picture.GetDigits())
		case scale < 0:
			base += fmt.Sprintf("9(%d)P(%d)", picture.GetDigits(), -scale)
		default:
			base += fmt.Sprintf("9(%d)", picture.GetDigits())
		}
	case irpb.Category_CATEGORY_ALPHABETIC:
		base = fmt.Sprintf("A(%d)", f.GetWidth())
	case irpb.Category_CATEGORY_NUMERIC_EDITED, irpb.Category_CATEGORY_ALPHANUMERIC_EDITED:
		base = fmt.Sprintf("%s, %s", categoryName(picture.GetCategory()), plural(int(f.GetWidth()), "byte"))
	default:
		base = fmt.Sprintf("X(%d)", f.GetWidth())
	}

	if f.GetUsage() != irpb.Usage_USAGE_DISPLAY {
		base += " " + usageName(f.GetUsage())
	}

	return base
}

// fillerNote is the comment column of an item the copybook gives no data-name.
//
// Its picture, and the repetition where it carries one: the bytes retained for a
// FILLER are one run of all its occurrences rather than one run each (see
// [fillerRun]), so a note carrying only the picture would name a width the run
// is a multiple of rather than the width of the run.
func fillerNote(e *emitter, f *irpb.Field) (string, error) {
	occurs, err := e.occurs(f.GetRepetition())
	if err != nil {
		return "", err
	}

	if occurs == "" {
		return pictureNote(f), nil
	}

	return pictureNote(f) + " " + occurs, nil
}

// integral is a value one of codec's numeric accessors returned, as an int, and
// whether it was one of them at all.
//
// The set is [emitter.fieldType]'s numeric half — the four integer types and
// the big one — because that is what [synth.number] and [synth.readBack] can
// hand back for a numeric item. Anything else is an item that is not numeric,
// and the caller says so rather than this saying zero.
func integral(value any) (int, bool) {
	switch number := value.(type) {
	case int16:
		return int(number), true
	case int32:
		return int(number), true
	case int64:
		return int(number), true
	case uint64:
		return int(number), true
	case *big.Int:
		return int(number.Int64()), true
	default:
		return 0, false
	}
}
