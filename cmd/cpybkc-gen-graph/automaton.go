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

// The rest of the automaton: what chooses a transition, what makes it eligible,
// what it remembers, and what remembers it.
//
// # Why the phrases live on the model
//
// Everything here composes a phrase and none of it writes a notation. That is
// [framing.String]'s rule applied to the half of the automaton this file draws:
// the wording is prose about the descriptor, both notations have the same thing
// to say, and a second emitter free to word it its own way is a second emitter
// free to word it less carefully.
//
// What the emitter still owns is *escaping*, because that is a property of the
// notation and not of the descriptor. So the composers below take an escaping
// function and apply it to every name that came out of somebody's copybook —
// [edge.label] and [binding.phrase] take `esc`, and [mermaid] passes
// [mermaidLabel]. Nothing else in a phrase is a name: a register is an
// identifier this generator spells, a literal is bytes rendered by
// [literalBytes] into a set no notation reacts to, and the connecting words are
// this file's own.

// registerName is what a document calls a register: `r` and the register node's
// own identifier.
//
// The identifier for the reason [stateName] uses one — registers carry
// identifiers and no names, and the identifier is what takes a reader who wants
// more than the diagram says to the same node of `cpybkc --emit-ir`. It is also
// why the register table exists: an `r20` in an edge label means nothing on its
// own, and the table is what turns it into a kind and a list of the transitions
// that write it.
func registerName(id uint64) string { return fmt.Sprintf("r%d", id) }

// noPredicate is what an edge says where its transition carries none.
//
// Said rather than left blank, because docs/ir/SPEC.md's "A transition may
// carry no predicate" makes the absence a meaning: such a transition matches
// every record, is selected by its guards alone, and gives up the
// undescribed-record diagnostic at the state offering it. A blank where a
// predicate would go reads as a generator that had nothing to print, which is
// the one thing it does not mean.
const noPredicate = "no predicate"

// predicate is a transition's discriminator as the diagram draws it: the field
// it names, by its path within the record, and the test it makes.
//
// The path rather than the identifier, because a predicate is the one thing on
// this diagram a person checks against their own copybook — `TYPE-CODE` is a
// name they wrote and `node 101` is not. docs/ir/SPEC.md's "Names" carries no
// materialised qualified path on any node, so this generator walks the member
// lists for it, which is what that section says a consumer needing one does.
type predicate struct {
	// carried is whether the transition carries a predicate at all.
	carried bool

	// field is the target's path within the record the transition admits,
	// dotted, and without the record's own top level: a reader already knows
	// which record the edge is about, and repeating its name on every field
	// would say it twice.
	field string

	// oneOf is whether the test is BytesOneOf rather than BytesEqual.
	oneOf bool

	// values are the literals, each already padded by the producer to the
	// target's width. One for BytesEqual, at least two for BytesOneOf.
	values [][]byte

	// text is whether those literals may be read as text, which is true only
	// where the target field's charset is the one whose bytes this generator
	// can name: ASCII. See [literalBytes].
	text bool
}

// phrase is the predicate as an edge label states it.
func (p predicate) phrase(esc func(string) string) string {
	if !p.carried {
		return noPredicate
	}

	printed := make([]string, 0, len(p.values))
	for _, value := range p.values {
		printed = append(printed, literalBytes(value, p.text))
	}

	return "when " + comparison(esc(p.field), p.oneOf, printed)
}

// guard is one guard: the register it reads and the test it makes of it.
type guard struct {
	// register is the register node's identifier.
	register uint64

	// positive is whether the test is GreaterThanZero, which carries no
	// operand.
	positive bool

	// oneOf is whether the test is a set rather than a single literal.
	oneOf bool

	// values are the literals the register is compared against, empty where
	// [guard.positive].
	values []literal
}

// phrase is the guard as a label states it.
func (g guard) phrase() string {
	if g.positive {
		// The words rather than `> 0`. `>` is the one character Mermaid's
		// transition arrow is made of, and a phrase this generator writes
		// literally is a phrase no escaping pass looks at — so the safe reading
		// is to not write the character at all. It also matches the schema's
		// own name for the test.
		return registerName(g.register) + " greater than zero"
	}

	printed := make([]string, 0, len(g.values))
	for _, value := range g.values {
		printed = append(printed, value.String())
	}

	return comparison(registerName(g.register), g.oneOf, printed)
}

// literal is a value a guard compares a register against: bytes, or an integer.
//
// Which member is set mirrors the kind of the register tested, as
// docs/ir/SPEC.md requires of a producer, and this carries the choice rather
// than an untyped byte string for the same reason the schema does — a `0`
// compared against a counter and a `0x00` compared against a flag are different
// facts and a document that printed them alike would blur them.
type literal struct {
	// bytes is the value where it is a bytes literal.
	bytes []byte

	// integer is the value where it is an integer literal.
	integer int64

	// isBytes says which of the two it is.
	isBytes bool
}

// String is the literal as a label prints it.
//
// A bytes literal is always bytes here, and never text. A guard reads a
// register, a bytes register holds "its source field's bytes as they appear in
// the record", and a register node declares its kind and nothing more — so
// there is no charset in reach of this literal at all, not even the one a
// predicate's target carries. Bytes is the only honest rendering left.
func (l literal) String() string {
	if !l.isBytes {
		return strconv.FormatInt(l.integer, 10)
	}

	return literalBytes(l.bytes, false)
}

// binding is one binding: the register it writes, and what it writes there.
type binding struct {
	// register is the register node's identifier.
	register uint64

	// field is the path within the record of the field read, where the binding
	// reads one.
	field string

	// decrement is whether the binding is the register's own value less one.
	decrement bool
}

// phrase is the binding as an edge label states it.
func (b binding) phrase(esc func(string) string) string {
	name := registerName(b.register)

	if b.decrement {
		// The subtraction rather than the word "decrement". A Decrement node
		// carries nothing at all — not even which register, which is the
		// binding's — so a reader who saw only the node kind would have to know
		// what it does; `r20 = r20 - 1` says it.
		return name + " = " + name + " - 1"
	}

	return name + " = " + esc(b.field)
}

// register is one register node as the table draws it.
type register struct {
	// id is the register node's own identifier, which is what an edge label
	// names it by.
	id uint64

	// holds is its kind, as a phrase.
	holds string

	// boundBy is every transition that writes it, in the order the diagram
	// draws those transitions.
	boundBy []binder
}

// binder is one transition that writes a register, named the way the table can
// point at the diagram: the edge it is, and the record it admits.
//
// The edge rather than the transition node's identifier, because the diagram
// above draws edges and not identifiers — a table naming a node a reader cannot
// find in the picture beside it would send them to `cpybkc --emit-ir` for
// something the picture already shows.
type binder struct {
	// from and to are the states the edge runs between.
	from, to uint64

	// record is the record the transition admits.
	record string
}

// comparison is the shape a predicate's test and a guard's equality tests
// share: a subject, and either one literal or a set of them.
//
// One composer for both because they are one sentence — "these bytes are that
// value" — asked of a field in one case and of a register in the other, and two
// composers would be two chances to word it differently.
func comparison(subject string, oneOf bool, values []string) string {
	if !oneOf {
		if len(values) == 0 {
			// Unreachable: every resolver below refuses a test carrying no
			// value before it builds one of these. Written as a phrase rather
			// than as a panic, for the reason [framing.String]'s default arm is
			// a sentence.
			return subject + " compared against nothing, which is a bug in " + pluginName
		}

		return subject + " = " + values[0]
	}

	// `or` rather than a comma, because the sections of an edge label are
	// separated by commas and a set written with them would run into the
	// section behind it. It is also what the test means: any one of these.
	return subject + " is one of " + strings.Join(values, " or ")
}

// conjunction is a transition's guards, or a state's acceptance guards, as one
// phrase.
//
// docs/ir/SPEC.md's "The automaton remembers, in registers" has all of a
// transition's guards hold for it to be eligible, so the list is a conjunction
// and `and` is what it is. Their order is not significant and this keeps the
// descriptor's, which is the only order there is to keep.
func conjunction(guards []guard) string {
	printed := make([]string, 0, len(guards))
	for _, g := range guards {
		printed = append(printed, g.phrase())
	}

	return strings.Join(printed, " and ")
}

// literalBytes is a literal as a document prints it: the text in quotes where
// the caller has established that these bytes are text, and the bytes
// themselves everywhere else.
//
// # text is a fact about the descriptor, not a guess about the bytes
//
// A literal is bytes, and whether a byte is a character is a question only a
// charset answers. This generator never guesses one: `0x40` is `@` in ASCII and
// a space in cp037, so a document that read the byte as printable ASCII because
// it happens to be printable ASCII would print `"@"` for a literal that is a
// space in somebody's file — the mangling this rule exists to prevent, arrived
// at without ever naming a charset. So text is passed in by a caller that has
// read the target field's own encoding, and everything with no field in reach —
// every guard literal — gets bytes. Same rule as [hex] for a delimiter, and the
// same reason.
//
// # Why the quoted form exists at all
//
// Where the bytes are known to be text, printing them as bytes would hide the
// thing a person is reading the label for: `0x59 0x20` and `0x59` are the same
// fact as `"Y "` and `"Y"`, and only the second pair is one anybody checks by
// eye. The producer has already padded the literal to the target's width
// (BytesEqual's `value`), so the trailing pad is part of the literal — and the
// quotes are what make it visible, since a trailing space with nothing behind
// it is not something a reader can see.
//
// # Why some text still prints as bytes
//
// The quoted form is written into the document literally, without an escaping
// pass, so it may only carry characters that end no line of any notation this
// generator writes and open no construct of one. `"` would close the quotes,
// `:` and `;` end a Mermaid label, `|` ends a cell of the register table, `#`
// opens Mermaid's own numeric escape, and `<`, `>` and `&` are what a Mermaid
// label's text is markup in — it is how `<br/>` works in one — so a literal
// carrying them would render as markup rather than as the value somebody is
// checking, and `>` is besides the character a transition arrow is made of. A
// literal carrying one of those, or any byte that is not printable ASCII at
// all, is printed as bytes instead — which says the same thing and needs no
// convention for saying it.
//
// The set is the same judgment [guard.phrase] makes when it writes "greater
// than zero" rather than `> 0`: a phrase written past every escaping pass is
// one to keep the metacharacters out of, and the cost of being conservative is
// a literal rendered as bytes rather than a document that renders wrongly.
func literalBytes(b []byte, text bool) string {
	// Before either rendering, because [hex] prints no bytes as nothing at all
	// — a blank where a value goes, which is the one thing this document may
	// not leave. A literal of no bytes is `""` under any charset.
	if len(b) == 0 {
		return `""`
	}

	if !text {
		return hex(b)
	}

	for _, one := range b {
		switch {
		case one < 0x20 || one > 0x7E:
			return hex(b)
		case one == '"', one == '#', one == ':', one == ';', one == '|', one == '`', one == '\\',
			one == '<', one == '>', one == '&':
			return hex(b)
		}
	}

	return `"` + string(b) + `"`
}

// predicateOf is the predicate selecting a transition, resolved.
func predicateOf(nodes nodeSet, t *irpb.Transition, record *irpb.Record) (predicate, error) {
	// The one reference in the schema absence is a meaning for, and it is read
	// by presence rather than by a sentinel: zero is an ordinary identifier.
	if t.PredicateId == nil {
		return predicate{}, nil
	}

	return predicateResolved(nodes, t.GetPredicateId(), record, "the predicate selecting a transition")
}

// predicateResolved is one predicate node, read against the record whose bytes
// it tests. position is what a diagnostic calls the place the reference came
// from.
//
// The two positions that carry one are a transition, where it chooses which
// record is in front of the reader, and an arm of a variant, where it chooses
// which alternative one occurrence holds. docs/ir/SPEC.md points both at the
// same message deliberately — "one closed set of tests, not a second set for
// arms" — so this generator reads them with one function rather than wording the
// same sentence twice.
func predicateResolved(nodes nodeSet, id uint64, record *irpb.Record, position string) (predicate, error) {
	node, ok := nodes.predicate(id)
	if !ok {
		return predicate{}, unresolved(id, position)
	}

	p := predicate{carried: true}

	switch test := node.GetTest().(type) {
	case *irpb.Predicate_BytesEqual:
		p.values = [][]byte{test.BytesEqual.GetValue()}
	case *irpb.Predicate_BytesOneOf:
		if len(test.BytesOneOf.GetValues()) < 2 {
			return predicate{}, malformed(
				fmt.Sprintf("predicate %d tests membership of a set of %d literals", id, len(test.BytesOneOf.GetValues())),
				"a producer MUST carry at least two literals in a one-of predicate; see docs/ir/SPEC.md, \"Discriminator predicates\"")
		}

		// The other half of the same rule, and drawn rather than a rule this
		// generator only quotes: a set carrying one literal twice draws as
		// `is one of 0xC8 or 0xC8`, which is a test no reader can act on and a
		// producer that did not check its own overlap.
		seen := make(map[string]bool, len(test.BytesOneOf.GetValues()))

		for _, value := range test.BytesOneOf.GetValues() {
			if seen[string(value)] {
				return predicate{}, malformed(
					fmt.Sprintf("predicate %d tests membership of a set carrying the same literal twice", id),
					"a producer MUST NOT carry the same literal twice: it is a value tested twice and a producer that did not check its own overlap; see docs/ir/SPEC.md, \"Discriminator predicates\"")
			}

			seen[string(value)] = true
		}

		p.oneOf, p.values = true, test.BytesOneOf.GetValues()
	default:
		return predicate{}, malformed(fmt.Sprintf("predicate %d carries no test", id),
			"the set is closed and a predicate carries one member of it; see docs/ir/SPEC.md, \"Discriminator predicates\"")
	}

	path, err := fieldPath(nodes, record, node.GetFieldId(), fmt.Sprintf("the field predicate %d tests", id))
	if err != nil {
		return predicate{}, err
	}

	p.field = path

	// The target's own charset decides whether its literals are text. A charset
	// of none is the axis saying the field holds bytes rather than characters,
	// so it takes the same side of this test as an EBCDIC one and for a
	// stronger reason: there is no character for a literal over it to be spelled
	// as. The resolution above has already established that the field is there,
	// so this lookup cannot fail; it is written as a lookup rather than threaded
	// out of [fieldPath] because the path and the encoding are two different
	// questions about the same node.
	if target, ok := nodes.field(node.GetFieldId()); ok {
		p.text = target.GetEncoding().GetCharset() == irpb.Charset_CHARSET_ASCII
	}

	return p, nil
}

// guardAt is one guard node, resolved. position is what a diagnostic calls the
// place the reference came from.
func guardAt(nodes nodeSet, id uint64, position string) (guard, error) {
	node, ok := nodes.guard(id)
	if !ok {
		return guard{}, unresolved(id, position)
	}

	read, ok := nodes.register(node.GetRegisterId())
	if !ok {
		return guard{}, unresolved(node.GetRegisterId(), fmt.Sprintf("the register guard %d reads", id))
	}

	// The register is kept rather than dropped after resolution, because its
	// kind is what says whether the literals beside it are the right kind of
	// literal. See [literalOf].
	kind := read.GetKind()

	g := guard{register: node.GetRegisterId()}

	switch test := node.GetTest().(type) {
	case *irpb.Guard_GreaterThanZero:
		// The one test with no literal beside it, and the one this generator
		// does not check the register's kind for. docs/ir/SPEC.md words it as
		// "the register holds an integer greater than zero", so a bytes
		// register under it is a producer bug — and it is the same producer bug
		// as a bytes register holding an integer, which is a fact about the
		// binding that wrote it rather than about this guard.
		g.positive = true
	case *irpb.Guard_Equals:
		one, err := literalOf(test.Equals, id, kind)
		if err != nil {
			return guard{}, err
		}

		g.values = []literal{one}
	case *irpb.Guard_OneOf:
		if len(test.OneOf.GetValues()) == 0 {
			// A guard testing membership of nothing can never hold, so the
			// transition carrying it is one no reader ever takes. That is a bug
			// in whatever compiled the automaton and not a set this document
			// has an honest way to draw — the alternative is `is one of` with
			// nothing behind it.
			//
			// Empty and not "fewer than two", which is where this differs from
			// the predicate set above. A producer MUST carry two literals in a
			// BytesOneOf and the schema says so; it states no such minimum for
			// a guard's LiteralSet, and refusing a one-literal set here would
			// be this generator inventing a rule and then reporting a
			// conforming descriptor as malformed.
			return guard{}, malformed(fmt.Sprintf("guard %d tests membership of an empty set of literals", id),
				"a guard tests the register against a literal or against a set of them; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
		}

		g.oneOf = true

		for _, value := range test.OneOf.GetValues() {
			one, err := literalOf(value, id, kind)
			if err != nil {
				return guard{}, err
			}

			g.values = append(g.values, one)
		}
	default:
		return guard{}, malformed(fmt.Sprintf("guard %d carries no test", id),
			"the set is three members and a guard carries one of them; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
	}

	return g, nil
}

// literalOf is one literal a guard carries, checked against the kind of the
// register it will be compared against.
//
// The check is here rather than left to a producer because the two kinds print
// differently and neither printing is wrong on its face: `r20 = 0` and
// `r21 = 0x00` are both sentences this document knows how to write, so a
// literal of the wrong kind draws confidently and says something the descriptor
// does not. That is the failure the schema splits Literal into two members to
// prevent — "a `0` compared against a counter and a `0x00` compared against a
// flag are different facts" — and a consumer that does not check is a consumer
// the split bought nothing.
func literalOf(l *irpb.Literal, guardID uint64, kind irpb.RegisterKind) (literal, error) {
	// The rule below is about a literal disagreeing with a register whose kind
	// the descriptor states. A register that states no kind at all is a
	// different failure, and [holdsOf] is where it is reported; reporting it
	// here as well would name this guard for a bug in the register node.
	mismatched := func(carried string) error {
		return malformed(
			fmt.Sprintf("guard %d compares a register that holds %s against %s", guardID, holds(kind), carried),
			"which member of a literal is set MUST match the kind of the register tested; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
	}

	switch value := l.GetValue().(type) {
	case *irpb.Literal_BytesValue:
		if kind == irpb.RegisterKind_REGISTER_KIND_INTEGER {
			return literal{}, mismatched("bytes")
		}

		return literal{isBytes: true, bytes: value.BytesValue}, nil
	case *irpb.Literal_Integer:
		if kind == irpb.RegisterKind_REGISTER_KIND_BYTES {
			return literal{}, mismatched("an integer")
		}

		return literal{integer: value.Integer}, nil
	default:
		return literal{}, malformed(fmt.Sprintf("guard %d compares a register against a literal that carries no value", guardID),
			"a literal is bytes or an integer, and which member is set MUST match the kind of the register tested; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
	}
}

// holds is a register kind as a diagnostic names it, including the kind a
// register node that states nothing has.
func holds(kind irpb.RegisterKind) string {
	switch kind {
	case irpb.RegisterKind_REGISTER_KIND_BYTES:
		return "bytes"
	case irpb.RegisterKind_REGISTER_KIND_INTEGER:
		return "an integer"
	default:
		return "nothing this generator has a name for"
	}
}

// bindingAt is one binding node, resolved against the record its transition
// admits.
func bindingAt(nodes nodeSet, id uint64, record *irpb.Record) (binding, error) {
	node, ok := nodes.binding(id)
	if !ok {
		return binding{}, unresolved(id, "a transition's binding")
	}

	if _, ok := nodes.register(node.GetRegisterId()); !ok {
		return binding{}, unresolved(node.GetRegisterId(), fmt.Sprintf("the register binding %d writes", id))
	}

	b := binding{register: node.GetRegisterId()}

	switch value := node.GetValue().(type) {
	case *irpb.Binding_Decrement:
		b.decrement = true
	case *irpb.Binding_FieldId:
		path, err := fieldPath(nodes, record, value.FieldId, fmt.Sprintf("the field binding %d reads", id))
		if err != nil {
			return binding{}, err
		}

		b.field = path
	default:
		return binding{}, malformed(fmt.Sprintf("binding %d writes a register and says nothing about what it writes", id),
			"a binding names the register it writes and the value written; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
	}

	return b, nil
}

// holdsOf is a register's kind as the table states it, refused where the
// register node states none.
func holdsOf(r *irpb.Register, id uint64) (string, error) {
	switch r.GetKind() {
	case irpb.RegisterKind_REGISTER_KIND_BYTES, irpb.RegisterKind_REGISTER_KIND_INTEGER:
		return holds(r.GetKind()), nil
	default:
		return "", malformed(fmt.Sprintf("register node %d says nothing about what it holds", id),
			"a register's kind is bytes or an integer; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
	}
}

// fieldPath is a field's path within the record a transition admits, dotted.
//
// # Why the record is walked rather than the field read
//
// No node carries a materialised qualified path, for the reason docs/ir/SPEC.md
// gives beside Names: a name is local, and "a consumer needing one walks the
// member lists it has already inverted". So this walks, and the walk is also
// the check — docs/ir/SPEC.md requires a predicate's target and a binding's
// source to be contained in the record the transition admits, and a field that
// is not turns up here as a walk that did not find it.
//
// # Why it is resolved only where something names a field
//
// A record's top level is resolved by this function and by nothing else, so a
// descriptor whose transitions carry no predicate and no field binding is drawn
// without it ever being read. That is deliberate: refusing such a descriptor
// for a dangling reference in the part of it this document does not draw would
// be refusing a diagram over something nobody looking at the diagram could see.
func fieldPath(nodes nodeSet, record *irpb.Record, id uint64, position string) (string, error) {
	if _, ok := nodes.field(id); !ok {
		return "", unresolved(id, position)
	}

	root, ok := nodes.group(record.GetRootId())
	if !ok {
		return "", unresolved(record.GetRootId(), "the top level of a record a transition admits")
	}

	// The root group's own name is seeded as seen rather than as a path
	// element. A reader already knows which record the edge is about — the
	// label says so two words earlier — and repeating it in front of every
	// field would say it twice.
	path, found, err := walkTo(nodes, root, id, nil, map[uint64]bool{record.GetRootId(): true})
	if err != nil {
		return "", err
	}

	if !found {
		return "", malformed(
			fmt.Sprintf("%s names node %d, and that field is not in the record the transition admits", position, id),
			"a producer MUST ensure the target is contained in the record the referring transition admits, at any depth; see docs/ir/SPEC.md, \"A predicate always names a field\"")
	}

	return strings.Join(path, "."), nil
}

// walkTo is the path from a group down to the field with this identifier, and
// whether it is beneath that group at all.
//
// seen carries the nodes this walk has already entered, so a member list that
// contains an ancestor of itself is a walk that stops rather than one that runs
// until the stack is gone. A descriptor cannot legally hold one — a member list
// states containment downward — and a diagram generator is not the place to
// find out that a producer emitted one the hard way. It is shared across the
// whole walk rather than per path, which is safe here because the identity
// check below happens before the prune: a node reached twice is a node already
// tested against the target.
//
// A member naming no node at all is refused rather than skipped. Skipping it
// would make the walk exhaust the record and report "that field is not in the
// record" — which names the predicate's target and blames the containment rule,
// when what is broken is the member list and the identifier it names. Two
// different bugs, and only one of them is in the place that diagnostic sends a
// reader to look.
func walkTo(nodes nodeSet, g *irpb.Group, id uint64, prefix []string, seen map[uint64]bool) ([]string, bool, error) {
	for _, memberID := range g.GetMemberIds() {
		if memberID == id {
			if f, ok := nodes.field(memberID); ok {
				return extend(prefix, nameOf(f.GetNames())), true, nil
			}
		}

		if seen[memberID] {
			continue
		}

		seen[memberID] = true

		member, ok := nodes.by[memberID]
		if !ok {
			return nil, false, unresolved(memberID, "a member of a record a transition admits")
		}

		switch kind := member.GetKind().(type) {
		case *irpb.Node_Group:
			path, found, err := walkTo(nodes, kind.Group, id, extend(prefix, nameOf(kind.Group.GetNames())), seen)
			if err != nil || found {
				return path, found, err
			}
		case *irpb.Node_Variant:
			// A variant contributes no element: docs/ir/SPEC.md gives it no
			// names at all, because the copybook gives the alternation none.
			// Neither a predicate's target nor a binding's source may sit
			// inside one — both forbid a field beneath a group that repeats,
			// and a variant may only exist beneath one — so this descent
			// normally finds nothing. It is here so that a descriptor breaking
			// that rule is refused as a field in the wrong place rather than as
			// a field the record does not carry.
			for _, arm := range kind.Variant.GetArms() {
				// The body's reference says which kind it is, rather than
				// leaving this to dereference an untyped identifier and find
				// out — so it is read as the member it is and never as
				// `GetGroupId()`, which answers zero for an arm whose body is a
				// field and zero is an ordinary identifier.
				switch body := arm.GetBody().(type) {
				case *irpb.Arm_FieldId:
					if body.FieldId != id {
						continue
					}

					if f, ok := nodes.field(body.FieldId); ok {
						return extend(prefix, nameOf(f.GetNames())), true, nil
					}
				case *irpb.Arm_GroupId:
					if seen[body.GroupId] {
						continue
					}

					seen[body.GroupId] = true

					held, ok := nodes.group(body.GroupId)
					if !ok {
						return nil, false, unresolved(body.GroupId, "the body of an arm of a variant in a record a transition admits")
					}

					path, found, err := walkTo(nodes, held, id, extend(prefix, nameOf(held.GetNames())), seen)
					if err != nil || found {
						return path, found, err
					}
				}
			}
		}
	}

	return nil, false, nil
}

// extend is a path with one more element, in storage of its own.
//
// A plain append would let two siblings of one member list write into the same
// backing array, so the second one's name would land in the first one's path —
// a wrong path in a document, produced by a walk that visited the right nodes.
func extend(prefix []string, name string) []string {
	out := make([]string, len(prefix)+1)

	copy(out, prefix)
	out[len(prefix)] = name

	return out
}

// The node kinds this half of the automaton resolves, each by identifier and by
// the kind its position admits. They are the same shape as [nodeSet.state] and
// its neighbours, and they are here rather than beside them because everything
// that calls one is in this file.
func (n nodeSet) predicate(id uint64) (*irpb.Predicate, bool) {
	kind, ok := n.by[id].GetKind().(*irpb.Node_Predicate)
	if !ok {
		return nil, false
	}

	return kind.Predicate, true
}

func (n nodeSet) guard(id uint64) (*irpb.Guard, bool) {
	kind, ok := n.by[id].GetKind().(*irpb.Node_Guard)
	if !ok {
		return nil, false
	}

	return kind.Guard, true
}

func (n nodeSet) binding(id uint64) (*irpb.Binding, bool) {
	kind, ok := n.by[id].GetKind().(*irpb.Node_Binding)
	if !ok {
		return nil, false
	}

	return kind.Binding, true
}

func (n nodeSet) register(id uint64) (*irpb.Register, bool) {
	kind, ok := n.by[id].GetKind().(*irpb.Node_Register)
	if !ok {
		return nil, false
	}

	return kind.Register, true
}

func (n nodeSet) field(id uint64) (*irpb.Field, bool) {
	kind, ok := n.by[id].GetKind().(*irpb.Node_Field)
	if !ok {
		return nil, false
	}

	return kind.Field, true
}

func (n nodeSet) group(id uint64) (*irpb.Group, bool) {
	kind, ok := n.by[id].GetKind().(*irpb.Node_Group)
	if !ok {
		return nil, false
	}

	return kind.Group, true
}
