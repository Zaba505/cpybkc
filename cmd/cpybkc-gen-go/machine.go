// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Zaba505/cpybkc/irpb"
)

// The unexported identifiers the generated file declares for itself.
//
// Lowercase, and that is what keeps them out of the way of everything the
// copybook names: every identifier munged from a name is exported, so a
// copybook item called MATCHES or DELIMITER becomes Matches and Delimiter and
// collides with neither. It is the argument codec.go's helpers already make.
const (
	delimiterVar    = "delimiter"
	lookaheadConst  = "lookahead"
	readAheadConst  = "readAhead"
	recordingType   = "recording"
	occurrencesFunc = "occurrences"
)

// emit is the whole of the generated file below its import block: the record
// interface, the reader, the predicates both directions share, and the writer.
func (f *filer) emit() (string, []string, error) {
	walks := make([][]transition, len(f.states))

	for i, state := range f.states {
		out, err := f.transitionsOf(state.GetState())
		if err != nil {
			return "", nil, err
		}

		walks[i] = out
	}

	if err := f.survey(walks); err != nil {
		return "", nil, err
	}

	f.gather(walks)

	var b strings.Builder

	f.emitRecord(&b, walks)

	if err := f.emitReader(&b, walks); err != nil {
		return "", nil, err
	}

	if err := f.emitPredicates(&b, walks); err != nil {
		return "", nil, err
	}

	if err := f.emitWriter(&b, walks); err != nil {
		return "", nil, err
	}

	imports := append([]string{}, fileImports...)
	if f.comparesBytes {
		imports = append(imports, "bytes")
	}

	if f.reports(walks) {
		imports = append(imports, "strings")
	}

	sort.Strings(imports)

	return b.String(), imports, nil
}

// survey settles the three facts the shape of the generated file turns on: what
// the reader has to hold, whether the file compares byte strings anywhere, and
// whether any record type collides with the names this file occupies at package
// scope.
func (f *filer) survey(walks [][]transition) error {
	// A delimiter is compared against the bytes standing where the framing says
	// one stands, which is the only comparison a framing itself makes.
	if f.how == delimited {
		f.comparesBytes = true
	}

	// Only the accepting states, because only their acceptance guards are
	// emitted: [filer.acceptance] skips a state that does not accept, so a
	// guard on one is not a comparison this file makes.
	for _, node := range f.states {
		if state := node.GetState(); state.GetAccepts() {
			f.surveyGuards(state.GetAcceptanceGuardIds())
		}
	}

	for _, walk := range walks {
		for _, t := range walk {
			// A transition's predicate is an equality over a window of the
			// bytes in front of the walk, and both directions evaluate it.
			if t.match != "" {
				f.comparesBytes = true
			}

			f.surveyGuards(t.node.GetGuardIds())

			switch t.typ {
			case recordInterface, readerType, writerType, newReaderFunc, newWriterFunc:
				return &collisionError{
					Go:    t.typ,
					Cobol: []colliding{{Original: t.record.GetNames().GetOriginal(), Override: t.record.GetNames().GetOverrideName()}, {Original: t.typ}},
					Where: "the package this generator writes, where " + t.typ + " is the file-level reader and writer's",
				}
			}

			for _, id := range t.node.GetBindingIds() {
				node, ok := f.nodes[id]
				if !ok {
					return unresolved(id)
				}

				binding := node.GetBinding()
				if binding == nil {
					return malformed(fmt.Sprintf("node %d is a transition's binding and is not a binding node", id),
						"a transition's binding list names binding nodes; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
				}

				kind, err := f.registerKind(binding.GetRegisterId())
				if err != nil {
					return err
				}

				// A bytes register holds its source field's bytes as they
				// appear in the record, so a reader that binds one has to hold
				// the record's own bytes and not only the values decoded out of
				// them. Nothing else obliges it to, which is why this is asked
				// rather than assumed.
				if kind == "[]byte" {
					if _, ok := binding.GetValue().(*irpb.Binding_FieldId); ok {
						f.keepsBytes = true
					}
				}
			}
		}
	}

	return nil
}

// surveyGuards records whether any of these guards reads a bytes register,
// which is a guard [compare] writes as an equality over byte strings.
//
// The kind is the whole of the question: the greater-than-zero test is refused
// over a bytes register and an equality over one is a comparison of bytes
// whatever it is compared against, so no guard reading such a register is
// written any other way.
//
// # It refuses nothing, deliberately
//
// A guard whose node is missing, is not a guard node, or names a register this
// descriptor does not carry is a malformed descriptor, and the refusal is
// [filer.guardTests]'s and [filer.acceptance]'s to make — later, over the same
// nodes, and with the phrasing that belongs to them. Refusing here would take
// those messages out of reach by arriving first, and it would answer for guards
// nothing emits, so what cannot be resolved is passed over as "not a bytes
// register". Nothing is lost by that: a descriptor whose guard does not resolve
// produces no file at all, so an import decision made about it never reaches
// anybody.
func (f *filer) surveyGuards(ids []uint64) {
	for _, id := range ids {
		node, ok := f.nodes[id]
		if !ok {
			continue
		}

		guard := node.GetGuard()
		if guard == nil {
			continue
		}

		if kind, err := f.registerKind(guard.GetRegisterId()); err == nil && kind == "[]byte" {
			f.comparesBytes = true
		}
	}
}

// emitRecord declares what the reader produces and the writer takes.
func (f *filer) emitRecord(b *strings.Builder, walks [][]transition) {
	line(b, "")
	line(b, "// %s is one record of the file this package describes: what a [%s]", recordInterface, readerType)
	line(b, "// produces and what a [%s] takes.", writerType)
	line(b, "//")
	line(b, "// It is codec's two interfaces together rather than a method this generator")
	line(b, "// invented. Every record type here already implements both, and a marker method")
	line(b, "// would be an identifier neither the copybook nor the layout wrote — which is")
	line(b, "// the thing this generator refuses to produce everywhere else. A value that is")
	line(b, "// not one of this package's record types satisfies it too, and the writer")
	line(b, "// reports one rather than emitting it.")
	line(b, "type %s interface {", recordInterface)
	line(b, "codec.Unmarshaler")
	line(b, "codec.Marshaler")
	line(b, "}")

	f.emitFramingDeclarations(b, walks)
}

// emitFramingDeclarations writes the constants the framing and the predicates
// need.
func (f *filer) emitFramingDeclarations(b *strings.Builder, _ [][]transition) {
	if f.how == delimited {
		line(b, "")
		line(b, "// %s is what stands between this file's records, as literal bytes.", delimiterVar)
		line(b, "//")
		line(b, "// Bytes rather than a named character, a code point or a line-ending style,")
		line(b, "// and compared to the input as bytes: nothing names the byte that ends a")
		line(b, "// line-delimited record, and cp037 and cp1047 disagree about which of 0x15 and")
		line(b, "// 0x25 is LF. It is never searched for, either — a record's end comes from its")
		line(b, "// extent, and 0x15 sits inside any COMP-3 field holding a value like +152.50.")
		line(b, "// See docs/ir/SPEC.md, \"A delimiter is bytes, not a character\".")
		line(b, "var %s = []byte(%s)", delimiterVar, strconv.Quote(string(f.delimiter)))
	}

	line(b, "")

	// The lookahead is the reader's only where the framing states nothing
	// about a record's length. Under the other two the record's bytes are in
	// hand before a predicate runs, so there is nothing to see in front of.
	if !f.needsShort() {
		b.WriteString(commentLines(fmt.Sprintf(`%s is the reader's buffer. This file's framing puts a record's bytes
in hand before a predicate is evaluated against them, so nothing has to be
seen in front of a record.

%s`, readAheadConst, readAheadNote(f.readAhead()))))
		line(b, "const %s = %d", readAheadConst, f.readAhead())

		return
	}

	b.WriteString(commentLines(fmt.Sprintf(`%[1]s is how far in front of a record the reader has to see to evaluate
the predicates of the state it is in, and %[2]s is the buffer that
guarantees it.

A predicate's target has a constant position within the record it belongs
to, so this is a number the descriptor fixes rather than one the data
moves. %[2]s is never below it — a window the buffer cannot hold is a
Peek that can never be satisfied — which is the whole of the correctness
this size carries.

%[3]s`, lookaheadConst, readAheadConst, readAheadNote(f.readAhead()))))
	line(b, "const (")
	line(b, "%s = %d", lookaheadConst, f.lookahead)
	line(b, "%s = %d", readAheadConst, f.readAhead())
	line(b, ")")
}

// bufioDefault is bufio.NewReader's own buffer size, which is where the
// generated reader's read-ahead starts.
//
// Named rather than written twice, because the emitted comment argues about
// this number and [filer.readAhead] computes with it, and the argument is only
// true while they are the same number.
const bufioDefault = 4096

// readAheadNote is the paragraph both forms of the read-ahead declaration
// carry: where the size came from, and what an adopter does when it is not
// enough.
//
// It is here rather than in either branch above because it is the same answer
// in both — the size is not a throughput decision, and the buffer is not where
// a throughput decision would go. See README.md, "Decided: the read-ahead
// buffer is a constant".
//
// It takes the size that is actually being declared rather than naming
// [bufioDefault]: a descriptor whose deepest predicate target reaches past that
// default declares that target instead, and a paragraph quoting 4096 above a
// constant reading 5002 contradicts both itself and the declaration it sits on.
func readAheadNote(size int) string {
	chosen := fmt.Sprintf(`%d is bufio's own default, and nothing here derives a size from these
records.`, bufioDefault)

	if size > bufioDefault {
		chosen = fmt.Sprintf(`%d is the deepest window a predicate of this file reads, which is the floor
above bufio's own default of %d, and nothing here derives a size above that
floor.`, size, bufioDefault)
	}

	return fmt.Sprintf(`%[1]s

What a larger buffer would be worth is a property of what a read of the file
costs — the filesystem it sits on, and what the host does on the way to it —
which no descriptor carries. Where a read is expensive, hand %[2]s a reader
that is already buffered to at least this size: bufio hands that reader back
rather than wrapping it a second time, so the file is read in its bites and
not in these.

	r, err := %[2]s(bufio.NewReaderSize(f, 1<<20), %[3]s())`,
		chosen, newReaderFunc, encodingFunc)
}

// readAhead is how big the reader's buffer is: bufio's own default, and the
// deepest predicate target of any state where that does not hold it.
//
// A constant rather than a value derived from the record's extent or an option
// on the plugin contract; README.md's "Decided: the read-ahead buffer is a
// constant" is the argument, and [readAheadNote] is what the generated file
// tells an adopter about it. What is not a decision is the floor: a lookahead
// the buffer cannot hold is a Peek that can never be satisfied, so the maximum
// below is the whole of this function.
func (f *filer) readAhead() int {
	if f.lookahead > bufioDefault {
		return f.lookahead
	}

	return bufioDefault
}

// emitPredicates writes one function per distinct predicate the automaton tests.
//
// A function rather than an expression inlined at both use sites, because both
// directions evaluate the same predicate against the same offsets — the reader
// against a window of the bytes in front of it, the writer against the bytes it
// is about to emit — and two spellings of one test are two things to get wrong.
//
// One per predicate rather than one per transition testing it: see [filer.gather].
func (f *filer) emitPredicates(b *strings.Builder, _ [][]transition) error {
	for _, p := range f.predicates {
		line(b, "")
		line(b, "// %s is the predicate over bytes %d:%d of a record: the transitions it", p.name, p.at, p.reads)
		line(b, "// selects admit %s.", strings.Join(p.admits, ", "))
		line(b, "//")
		line(b, "// One function per distinct predicate rather than one per transition that")
		line(b, "// tests it. A predicate is a function of where it reads, how wide that window")
		line(b, "// is and what it is compared against, and not of the state whose transition")
		line(b, "// happens to reach it — so every state testing this one names this function.")
		line(b, "//")
		line(b, "// A target that is not wholly inside the bytes it is handed does not match. A")
		line(b, "// reader hands it the record the framing bounds, or as much of the input as it")
		line(b, "// can see where the framing bounds nothing; a writer hands it the whole of the")
		line(b, "// record it is about to emit.")
		line(b, "func %s(b []byte) bool {", p.name)
		line(b, "if len(b) < %d {", p.reads)
		line(b, "return false")
		line(b, "}")
		line(b, "")
		line(b, "return %s", p.match)
		line(b, "}")
	}

	return nil
}

// predicateFunc is one distinct predicate of the automaton and the single
// function both directions test it with.
type predicateFunc struct {
	// name is that function's identifier, and match is the expression it
	// returns.
	name  string
	match string

	// at is where its window starts in the record and reads is how far into
	// the record it reaches, which are the bounds of the slice match compares.
	at    int
	reads int

	// admits is the records the transitions this predicate selects admit, in
	// the order the automaton first reaches them and without repeats. It is
	// what the emitted doc comment says the predicate is for.
	admits []string
}

// gather is the one function per distinct predicate this file emits, and the
// name each transition testing one calls it by.
//
// A predicate is a function of the offset it reads at, the width it reads and
// the literals it compares against, and of nothing else. Emitting one function
// per transition that tests it states a dependence on the state reaching it
// which does not exist, and it is the one part of this output whose size grows
// with the automaton's fan-out rather than with the number of things being
// tested: example/policy carried 93 functions with 11 distinct bodies, nine and
// ten copies of each, for around 2,000 lines of a 12,394-line tree.
//
// The expression is the key because it already carries the whole triple and
// carries nothing else: the window is written into it as b[offset:offset+width]
// and the literals are its operands, so two transitions testing the same thing
// produce the same string by construction and two testing different things
// cannot. Two one-of predicates carrying one set in two orders are two
// predicates here, which is the conservative answer — they are two expressions,
// and nothing reorders a set the descriptor stated.
//
// The order is the order the automaton reaches them, so the names are a function
// of the descriptor and the numbering does not move when an unrelated state is
// added behind them.
func (f *filer) gather(walks [][]transition) {
	f.predicateOf = make(map[string]int)

	for _, walk := range walks {
		for _, t := range walk {
			if t.match == "" {
				continue
			}

			i, ok := f.predicateOf[t.match]
			if !ok {
				i = len(f.predicates)
				f.predicateOf[t.match] = i
				f.predicates = append(f.predicates, predicateFunc{
					name:  matcher(i+1, t.at),
					match: t.match,
					at:    t.at,
					reads: t.reads,
				})
			}

			original := t.record.GetNames().GetOriginal()
			if !slices.Contains(f.predicates[i].admits, original) {
				f.predicates[i].admits = append(f.predicates[i].admits, original)
			}
		}
	}
}

// matcherOf is what the predicate of one transition is called, which is the
// name of the one function every transition testing that predicate calls.
func (f *filer) matcherOf(t transition) string {
	return f.predicates[f.predicateOf[t.match]].name
}

// matcher is what the nth distinct predicate of a file, reading at byte offset
// at, is called.
//
// Keyed on the predicate rather than on the state that tests it. The offset is
// in the name because it is a property of the predicate and it is what a reader
// holding this file against the graph is looking for; the ordinal is what makes
// the name unique, since a discriminator at one offset is exactly the case where
// several predicates read the same window.
func matcher(nth, at int) string {
	return fmt.Sprintf("matches%dAt%d", nth, at)
}

// guardOf is the Go expression a guard tests, and the phrase a diagnostic
// reports it with when it excludes a transition.
//
// The phrase names the register by the identifier the descriptor carries it
// under, because a register has no name and there is nothing else to call one:
// what a user needs from the diagnostic is which of the automaton's values said
// this record does not belong here.
func (f *filer) guardOf(id uint64, holder string) (test, phrase string, err error) {
	node, ok := f.nodes[id]
	if !ok {
		return "", "", unresolved(id)
	}

	guard := node.GetGuard()
	if guard == nil {
		return "", "", malformed(fmt.Sprintf("node %d guards a transition or a state and is not a guard node", id),
			"a guard reads a register and decides whether the transition carrying it is eligible; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
	}

	kind, err := f.registerKind(guard.GetRegisterId())
	if err != nil {
		return "", "", err
	}

	at := holder + "." + register(guard.GetRegisterId())
	named := fmt.Sprintf("the register the descriptor carries as node %d", guard.GetRegisterId())

	switch test := guard.GetTest().(type) {
	case *irpb.Guard_Equals:
		value, err := literal(test.Equals, kind)
		if err != nil {
			return "", "", err
		}

		return compare(at, value, kind), fmt.Sprintf("%s is %s", named, value), nil
	case *irpb.Guard_OneOf:
		values := test.OneOf.GetValues()
		if len(values) == 0 {
			return "", "", malformed("a one-of guard carries no literals",
				"a guard tests the register against a set, and a set of nothing excludes every transition carrying it")
		}

		tests := make([]string, 0, len(values))
		names := make([]string, 0, len(values))

		for _, v := range values {
			value, err := literal(v, kind)
			if err != nil {
				return "", "", err
			}

			tests = append(tests, compare(at, value, kind))
			names = append(names, value)
		}

		return "(" + strings.Join(tests, " || ") + ")", fmt.Sprintf("%s is one of %s", named, strings.Join(names, ", ")), nil
	case *irpb.Guard_GreaterThanZero:
		if kind != "int64" {
			return "", "", malformed(fmt.Sprintf("a guard asks whether node %d is greater than zero and it holds bytes", guard.GetRegisterId()),
				"the greater-than-zero test is over an integer register; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
		}

		return at + " > 0", named + " is greater than zero", nil
	default:
		return "", "", malformed("a guard carries no test",
			"the set is closed and a guard carries one member of it; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
	}
}

// literal is a guard's carried value as Go source, and it is refused where its
// member does not match the kind of the register it will be compared against.
func literal(l *irpb.Literal, kind string) (string, error) {
	switch value := l.GetValue().(type) {
	case *irpb.Literal_BytesValue:
		if kind != "[]byte" {
			return "", malformed("a guard compares an integer register against a byte string",
				"which member of a literal is set MUST match the kind of the register tested; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
		}

		return strconv.Quote(string(value.BytesValue)), nil
	case *irpb.Literal_Integer:
		if kind != "int64" {
			return "", malformed("a guard compares a bytes register against a number",
				"which member of a literal is set MUST match the kind of the register tested; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
		}

		return strconv.FormatInt(value.Integer, 10), nil
	default:
		return "", malformed("a guard carries a literal holding no value",
			"a literal carries the bytes a bytes register is compared against or the number an integer one is")
	}
}

// compare is the equality test between a register and one literal.
func compare(at, value, kind string) string {
	if kind == "[]byte" {
		return fmt.Sprintf("bytes.Equal(%s, []byte(%s))", at, value)
	}

	return fmt.Sprintf("%s == %s", at, value)
}

// registerFields declares the register file, which the reader and the writer
// hold identically at the same point in a file.
func (f *filer) registerFields(b *strings.Builder, holder string) error {
	if len(f.registers) == 0 {
		return nil
	}

	line(b, "")
	line(b, "// The register file. There is one for the whole %s, a register holds what the", holder)
	line(b, "// most recent binding put in it, and nothing saves or restores one — so the")
	line(b, "// value in force is the one from the nearest preceding record that bound it,")
	line(b, "// along the path actually taken.")
	line(b, "//")
	line(b, "// Each carries whether anything has bound it, because reading a register")
	line(b, "// nothing has written is a malformed descriptor rather than a zero: a consumer")
	line(b, "// MUST NOT supply a zero, an empty byte string or the value of any other")
	line(b, "// register. See docs/ir/SPEC.md, \"The automaton remembers, in registers\".")

	for _, node := range f.registers {
		kind, err := f.registerKind(node.GetId())
		if err != nil {
			return err
		}

		line(b, "%s %s", register(node.GetId()), kind)
		line(b, "%s bool", held(node.GetId()))
	}

	return nil
}

// acceptance emits the body of the check both directions owe the end of a file:
// the state stopped in accepts, and its acceptance guards all hold.
func (f *filer) acceptance(b *strings.Builder, holder, ending string) error {
	line(b, "switch %s.state {", holder)

	for i, node := range f.states {
		state := node.GetState()
		if !state.GetAccepts() {
			continue
		}

		line(b, "case %d:", i)

		for _, id := range state.GetAcceptanceGuardIds() {
			test, phrase, err := f.guardOf(id, holder)
			if err != nil {
				return err
			}

			guard := f.nodes[id].GetGuard()

			line(b, "if !%s.%s {", holder, held(guard.GetRegisterId()))
			line(b, "return %s.unbound(%d)", holder, guard.GetRegisterId())
			line(b, "}")
			line(b, "")
			line(b, "if !(%s) {", test)
			line(b, "return fmt.Errorf(%q, %s.ordinal)",
				fmt.Sprintf("%s after %%d records and it is not complete: the state it ends in accepts only where %s",
					escaped(ending), escaped(phrase)), holder)
			line(b, "}")
			line(b, "")
		}

		line(b, "return nil")
	}

	line(b, "default:")
	line(b, "return fmt.Errorf(%q, %s.ordinal)", fmt.Sprintf("%s after %%d records and the layout describes a record to come", escaped(ending)), holder)
	line(b, "}")

	return nil
}

// guardTests is the eligibility test of one transition and the phrase reporting
// what excluded it, or the empty string where it carries no guards.
func (f *filer) guardTests(t transition, holder string) (test, phrase string, guards []uint64, err error) {
	tests := make([]string, 0, len(t.node.GetGuardIds()))
	phrases := make([]string, 0, len(t.node.GetGuardIds()))

	for _, id := range t.node.GetGuardIds() {
		one, said, err := f.guardOf(id, holder)
		if err != nil {
			return "", "", nil, err
		}

		guard := f.nodes[id].GetGuard()

		tests = append(tests, one)
		phrases = append(phrases, said)
		guards = append(guards, guard.GetRegisterId())
	}

	if len(tests) == 0 {
		return "", "", nil, nil
	}

	return strings.Join(tests, " && "), strings.Join(phrases, " and "), guards, nil
}
