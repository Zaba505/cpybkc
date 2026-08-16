// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"strings"

	"github.com/Zaba505/cpybkc/irpb"
)

// needsLook reports whether any state evaluates a predicate, which is the only
// thing that obliges the reader to see in front of a record it has not
// identified.
func (f *filer) needsLook() bool { return f.lookahead > 0 }

// needsShort reports whether the reader can find itself with less input than a
// predicate wanted.
//
// Only where the framing bounds nothing: under a framing that states a record's
// length, the record's bytes are in hand before a predicate runs, and a target
// past the end of them does not match rather than cutting the file short.
func (f *filer) needsShort() bool { return f.needsLook() && !f.how.bounded() }

// emitReader writes the file-level reader.
func (f *filer) emitReader(b *strings.Builder, walks [][]transition) error {
	line(b, "")
	line(b, "// %s reads the records of one file, walking the automaton this descriptor", readerType)
	line(b, "// carries.")
	line(b, "//")
	line(b, "// One record at a time and in this order, which is docs/ir/SPEC.md's \"Where")
	line(b, "// framing is consumed, and where it is emitted\": test for end of input at a")
	line(b, "// record boundary, consume the framing in front of the record, evaluate the")
	line(b, "// state's transitions — guards first, then predicates, in the order the state")
	line(b, "// carries them — admit the record the first one that matches names, check the")
	line(b, "// framing against that record's extent, consume the framing behind it, and apply")
	line(b, "// that transition's bindings.")
	line(b, "//")
	line(b, "// The file is never held. What this carries is one record's bytes and the")
	line(b, "// register file the automaton remembers in, whatever the file's size.")
	line(b, "type %s struct {", readerType)
	line(b, "src *bufio.Reader")
	line(b, "enc codec.Encoding")
	line(b, "")
	line(b, "// state is where in the automaton the read is, as a position in the switch")
	line(b, "// [%s.Next] walks. The descriptor's state nodes are numbered by ascending", readerType)
	line(b, "// identifier, so a state is a small integer rather than a lookup.")
	line(b, "state int")
	line(b, "")
	line(b, "// ordinal is which record of the file has been read, counting from one, so")
	line(b, "// that a diagnostic can say where.")
	line(b, "ordinal int")
	line(b, "")
	line(b, "// done is whether the end of the file has been reached and reported.")
	line(b, "done bool")

	if f.needsLook() {
		line(b, "")
		line(b, "// look is the bytes a predicate of the current state is evaluated against.")
		line(b, "look []byte")
	}

	if f.needsShort() {
		line(b, "")
		line(b, "// short is whether the input ran out before that window was full, which is")
		line(b, "// a file cut short rather than a record the layout does not describe.")
		line(b, "short bool")
	}

	if f.how.bounded() {
		line(b, "")
		line(b, "// data is the record the framing states the length of, reassembled where")
		line(b, "// the file split it into segments. It is reused between records.")
		line(b, "data []byte")
	}

	if f.keepsBytes && !f.how.bounded() {
		line(b, "")
		line(b, "// raw is the bytes of the record in hand, kept because a bytes register")
		line(b, "// holds its source field's bytes as they appear in the record. It is")
		line(b, "// reused between records, and a binding copies out of it.")
		line(b, "raw []byte")
	}

	if f.how == delimited && f.placement == irpb.DelimiterPlacement_DELIMITER_PLACEMENT_SEPARATOR {
		line(b, "")
		line(b, "// first is whether the next record is the first, which is the one a")
		line(b, "// separator does not stand in front of.")
		line(b, "first bool")
	}

	if err := f.registerFields(b, "read"); err != nil {
		return err
	}

	line(b, "}")

	f.emitNewReader(b)

	if err := f.emitNext(b, walks); err != nil {
		return err
	}

	if err := f.emitAccepts(b); err != nil {
		return err
	}

	f.emitReaderDiagnostics(b, walks)
	f.emitLeading(b)
	f.emitAdmit(b)
	f.emitTrailing(b)

	return f.emitHelpers(b, walks)
}

// emitNewReader writes the constructor.
func (f *filer) emitNewReader(b *strings.Builder) {
	line(b, "")
	line(b, "// %s reads the records of r under enc.", newReaderFunc)
	line(b, "//")
	line(b, "// Neither the charset, the zoned sign convention, the byte order nor the")
	line(b, "// floating-point format is chosen here. They are properties of the file in hand")
	line(b, "// rather than of this descriptor's items, so the caller states all four at once")
	line(b, "// — [Encoding] is what this descriptor resolved, and a file of these records")
	line(b, "// converted to another character set is read by passing a different one.")
	line(b, "func %s(r io.Reader, enc codec.Encoding) (*%s, error) {", newReaderFunc, readerType)
	line(b, "if r == nil {")
	line(b, "return nil, codec.ErrNilReader")
	line(b, "}")
	line(b, "")
	line(b, "if err := enc.Validate(); err != nil {")
	line(b, "return nil, err")
	line(b, "}")
	line(b, "")
	line(b, "return &%s{", readerType)
	line(b, "src: bufio.NewReaderSize(r, %s),", readAheadConst)
	line(b, "enc: enc,")
	line(b, "state: %d,", f.index[f.file.GetStartStateId()])

	if f.how == delimited && f.placement == irpb.DelimiterPlacement_DELIMITER_PLACEMENT_SEPARATOR {
		line(b, "first: true,")
	}

	line(b, "}, nil")
	line(b, "}")
}

// emitNext writes the read loop.
func (f *filer) emitNext(b *strings.Builder, walks [][]transition) error {
	line(b, "")
	line(b, "// Next is the next record of the file, or io.EOF where the file is complete.")
	line(b, "//")
	line(b, "// io.EOF means that and only that. A file cut short is reported as an error of")
	line(b, "// its own and never wraps io.EOF, so a caller stepping through records tells")
	line(b, "// the two apart with errors.Is and does not have to read the message.")
	line(b, "//")
	line(b, "// A file that ends in a state that does not accept, or whose acceptance guards")
	line(b, "// do not all hold, is reported as truncated rather than returning the records")
	line(b, "// that were read: an automaton whose accepting states nobody checks detects")
	line(b, "// nothing.")
	line(b, "func (r *%s) Next() (%s, error) {", readerType, recordInterface)
	line(b, "if r.done {")
	line(b, "return nil, io.EOF")
	line(b, "}")
	line(b, "")
	line(b, "// End of input is tested here, at a record boundary, and nowhere else. By")
	line(b, "// the time it runs, framing behind the record before this one has been")
	line(b, "// consumed as the framing it is, so a file whose last record carries a")
	line(b, "// well-formed trailing delimiter is complete rather than holding a record")
	line(b, "// the layout does not describe.")
	line(b, "if _, err := r.src.Peek(1); err != nil {")
	line(b, "if !errors.Is(err, io.EOF) {")
	line(b, "return nil, fmt.Errorf(\"reading record %%d: %%w\", r.ordinal+1, err)")
	line(b, "}")
	line(b, "")
	line(b, "r.done = true")
	line(b, "")
	line(b, "if err := r.accepts(); err != nil {")
	line(b, "return nil, err")
	line(b, "}")
	line(b, "")
	line(b, "return nil, io.EOF")
	line(b, "}")
	line(b, "")
	line(b, "r.ordinal++")
	line(b, "")
	line(b, "if err := r.leading(); err != nil {")
	line(b, "return nil, err")
	line(b, "}")
	line(b, "")
	line(b, "switch r.state {")

	for i, walk := range walks {
		line(b, "case %d: // the state the descriptor carries as node %d", i, f.states[i].GetId())

		if err := f.emitState(b, i, walk); err != nil {
			return err
		}
	}

	line(b, "default:")
	line(b, "return nil, fmt.Errorf(\"record %%d: the automaton is in a state this file does not carry\", r.ordinal)")
	line(b, "}")
	line(b, "}")

	return nil
}

// emitState writes one state's transitions, in the order the state carries
// them.
func (f *filer) emitState(b *strings.Builder, at int, walk []transition) error {
	if len(walk) == 0 {
		line(b, "return nil, r.undescribed(nil, \"\")")

		return nil
	}

	if err := f.exhaustive(walk); err != nil {
		return err
	}

	// A state offering one transition that carries neither a predicate nor a
	// guard admits whatever is in front of the reader, so there is nothing for
	// it to report and no set of record types it expected.
	if len(walk) == 1 && walk[0].match == "" && len(walk[0].node.GetGuardIds()) == 0 {
		line(b, "// Its one transition, which admits %s and carries no predicate: it", walk[0].record.GetNames().GetOriginal())
		line(b, "// matches every record, and this state offers nowhere else for one to go.")

		return f.emitAdmission(b, walk[0])
	}

	guarded := false

	for _, t := range walk {
		if len(t.node.GetGuardIds()) != 0 {
			guarded = true
		}
	}

	line(b, "expected := make([]string, 0, %d)", len(walk))

	if guarded {
		line(b, "")
		line(b, "var excluded string")
	}

	for j, t := range walk {
		line(b, "")
		line(b, "// Transition %d, which admits %s.", j+1, t.record.GetNames().GetOriginal())

		test, phrase, registers, err := f.guardTests(t, "r")
		if err != nil {
			return err
		}

		if test != "" {
			for _, id := range registers {
				line(b, "if !r.%s {", held(id))
				line(b, "return nil, r.unbound(%d)", id)
				line(b, "}")
				line(b, "")
			}

			line(b, "if %s {", test)
		}

		line(b, "expected = append(expected, %q)", t.record.GetNames().GetOriginal())

		closing := ""

		if t.match != "" {
			line(b, "if %s(r.look) {", matcher(f.states[at].GetId(), j))

			closing = "}"
		}

		if err := f.emitAdmission(b, t); err != nil {
			return err
		}

		if closing != "" {
			line(b, "%s", closing)
		}

		if test != "" {
			// A transition a guard excluded is reported as that rather than as
			// a record the layout does not describe — but only where it
			// carries a predicate that would have matched. One carrying none
			// would have matched whatever was in front of the reader, so it
			// says nothing about the bytes in hand and never displaces the
			// diagnostic.
			if t.match != "" {
				line(b, "} else if excluded == \"\" && %s(r.look) {", matcher(f.states[at].GetId(), j))
				line(b, "excluded = fmt.Sprintf(%q%s)",
					fmt.Sprintf("a guard excluded the transition that would have admitted %s, which is taken only where %s%s",
						escaped(t.record.GetNames().GetOriginal()), escaped(phrase), f.holding(registers)),
					f.holdingArgs(registers, "r"))
				line(b, "}")
			} else {
				line(b, "}")
			}
		}
	}

	line(b, "")

	if guarded {
		line(b, "return nil, r.undescribed(expected, excluded)")
	} else {
		line(b, "return nil, r.undescribed(expected, \"\")")
	}

	return nil
}

// exhaustive refuses a state offering a transition that carries neither a
// predicate nor a guard beside another transition.
//
// Such a transition is always eligible and matches every record, so anything
// the state carries behind it could never be selected — which is the ambiguity
// docs/ir/SPEC.md's "When two match, and when none does" forbids, seen from the
// generator. Refusing it is also what keeps the emitted walk free of code no
// path reaches.
func (f *filer) exhaustive(walk []transition) error {
	for i, t := range walk {
		if t.match != "" || len(t.node.GetGuardIds()) != 0 {
			continue
		}

		if len(walk) == 1 {
			continue
		}

		return malformed(fmt.Sprintf("transition %d of a state carries neither a predicate nor a guard, and the state carries %d transitions", i+1, len(walk)),
			"a transition carrying no predicate matches every record and one carrying no guard is always eligible, so no other transition of that state could ever be selected; see docs/ir/SPEC.md, \"When two match, and when none does\"")
	}

	return nil
}

// holding is the tail of a guard-exclusion diagnostic: what the registers it
// tested actually hold, so that the report names the value and not only the
// rule.
func (f *filer) holding(registers []uint64) string {
	if len(registers) == 0 {
		return ""
	}

	parts := make([]string, 0, len(registers))

	for _, id := range registers {
		kind, err := f.registerKind(id)
		if err != nil {
			continue
		}

		verb := "%d"
		if kind == "[]byte" {
			verb = "%q"
		}

		parts = append(parts, fmt.Sprintf("node %d holds %s", id, verb))
	}

	return "; " + strings.Join(parts, " and ")
}

// holdingArgs is the argument list [filer.holding]'s verbs take.
func (f *filer) holdingArgs(registers []uint64, holder string) string {
	if len(registers) == 0 {
		return ""
	}

	args := make([]string, 0, len(registers))
	for _, id := range registers {
		args = append(args, holder+"."+register(id))
	}

	return ", " + strings.Join(args, ", ")
}

// emitAdmission writes steps 4 to 7 for one transition: size what a register
// counts, admit the record, consume the framing behind it, apply the bindings
// and move.
func (f *filer) emitAdmission(b *strings.Builder, t transition) error {
	line(b, "rec := new(%s)", t.typ)
	line(b, "")

	counts, err := f.registerCounts(t.record)
	if err != nil {
		return err
	}

	for _, count := range counts {
		f.emitSizing(b, count)
	}

	line(b, "if err := r.admit(rec); err != nil {")
	line(b, "return nil, err")
	line(b, "}")
	line(b, "")

	if f.trailingFraming() {
		line(b, "if err := r.trailing(); err != nil {")
		line(b, "return nil, err")
		line(b, "}")
		line(b, "")
	}

	if err := f.emitBindings(b, t, "r", "return nil, "); err != nil {
		return err
	}

	line(b, "r.state = %d", t.next)
	line(b, "")
	line(b, "return rec, nil")

	return nil
}

// emitSizing writes the reader's half of a table a register counts: the
// register already holds the number of occurrences, so the table is sized from
// it before the record is decoded and the decode reads exactly that many.
func (f *filer) emitSizing(b *strings.Builder, count registerCount) {
	for _, loop := range count.loops {
		line(b, "for %s := range %s {", loop.variable, loop.over)
	}

	line(b, "if !r.%s {", held(count.register))
	line(b, "return nil, r.unbound(%d)", count.register)
	line(b, "}")
	line(b, "")
	line(b, "if r.%s < %d || r.%s > %d {", register(count.register), count.minimum, register(count.register), count.maximum)
	line(b, "return nil, fmt.Errorf(%q, r.ordinal, r.%s)",
		fmt.Sprintf("record %%d: %s occurs %d to %d times and the register the descriptor carries as node %d holds %%d",
			count.item, count.minimum, count.maximum, count.register),
		register(count.register))
	line(b, "}")
	line(b, "")
	line(b, "%s = %s(%s, int(r.%s))", count.expr, occurrencesFunc, count.expr, register(count.register))

	for range count.loops {
		line(b, "}")
	}

	line(b, "")
}

// emitBindings writes a transition's bindings, which apply after the record is
// admitted and read the register file as it stood on entry to the state.
func (f *filer) emitBindings(b *strings.Builder, t transition, holder, fail string) error {
	for _, id := range t.node.GetBindingIds() {
		binding := f.nodes[id].GetBinding()

		kind, err := f.registerKind(binding.GetRegisterId())
		if err != nil {
			return err
		}

		switch value := binding.GetValue().(type) {
		case *irpb.Binding_FieldId:
			if err := f.emitFieldBinding(b, t, binding.GetRegisterId(), value.FieldId, kind, holder, fail); err != nil {
				return err
			}
		case *irpb.Binding_Decrement:
			if kind != "int64" {
				return malformed(fmt.Sprintf("a binding takes one off node %d, which holds bytes", binding.GetRegisterId()),
					"the decrement member writes an integer register; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
			}

			line(b, "if !%s.%s {", holder, held(binding.GetRegisterId()))
			line(b, "%s%s.unbound(%d)", fail, holder, binding.GetRegisterId())
			line(b, "}")
			line(b, "")
			line(b, "if %s.%s <= 0 {", holder, register(binding.GetRegisterId()))
			line(b, "%sfmt.Errorf(%q, %s.ordinal)", fail,
				fmt.Sprintf("record %%d takes one off the register the descriptor carries as node %d, which is already at zero: a counter that would run below zero is a bug in whatever produced this descriptor",
					binding.GetRegisterId()), holder)
			line(b, "}")
			line(b, "")
			line(b, "%s.%s--", holder, register(binding.GetRegisterId()))
			line(b, "")
		default:
			return malformed("a binding writes a register and says nothing about what with",
				"a binding names the value written: a field of the record the transition admits, or the register's own value less one")
		}
	}

	return nil
}

// emitFieldBinding writes the register from a field of the record just
// admitted.
//
// An integer register takes the field's value, decoded by that field's own four
// axes, which the decode method has already done. A bytes register takes the
// field's bytes as they stand in the record, so it is cut out of the record's
// own bytes and copied — a register holding a view of a buffer the next record
// overwrites is a guard that tests the wrong file.
func (f *filer) emitFieldBinding(b *strings.Builder, t transition, id, field uint64, kind, holder, fail string) error {
	node, ok := f.nodes[field]
	if !ok {
		return unresolved(field)
	}

	source := node.GetField()
	if source == nil {
		return malformed(fmt.Sprintf("node %d is bound into a register and is not a field node", field),
			"a binding names a field node contained in the record the transition admits; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
	}

	if source.GetRepetition() != nil {
		return malformed(fmt.Sprintf("%s is bound into a register and repeats", originalOf(node)),
			"a binding names a field, not an occurrence of one; see docs/ir/SPEC.md, \"A reference names a field, not an occurrence of one\"")
	}

	c := &coder{emitter: f.emitter}

	at, found, err := c.offsetOf(t.record.GetRootId(), field, "rec", encoding)
	if err != nil {
		return err
	}

	if !found {
		return malformed(fmt.Sprintf("%s is bound into a register and is not in the record the transition admits", originalOf(node)),
			"a producer MUST NOT name a field of any other record; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
	}

	if kind == "[]byte" {
		line(b, "%s.%s = append(%s.%s[:0], %s[%s:%s]...)",
			holder, register(id), holder, register(id), f.rawExpr(holder), at.String(), at.plus(int(source.GetWidth())).String())
		line(b, "%s.%s = true", holder, held(id))
		line(b, "")

		return nil
	}

	bound, err := f.integerValue(t, node, source)
	if err != nil {
		return err
	}

	if bound.guard != "" {
		line(b, "if %s {", bound.guard)
		line(b, "%sfmt.Errorf(%q, %s.ordinal, %s)", fail,
			fmt.Sprintf("record %%d: %s is bound into the register the descriptor carries as node %d and holds %%d, which is above the %s an integer register holds",
				escaped(originalOf(node)), id, maxInt64),
			holder, bound.path)
		line(b, "}")
		line(b, "")
	}

	line(b, "%s.%s = %s", holder, register(id), bound.value)
	line(b, "%s.%s = true", holder, held(id))
	line(b, "")

	return nil
}

// maxInt64 is the largest value an integer register holds, written out because
// the generated file reaches it as an untyped constant rather than through a
// math import it would otherwise not need.
const maxInt64 = "9223372036854775807"

// integerBinding is how an integer register is bound from a field: the Go
// expression it takes, and the condition under which taking it would lose the
// value.
type integerBinding struct {
	// value is the expression assigned to the register.
	value string
	// path is the field the value is read from, before any conversion.
	path string
	// guard is the condition the reader refuses on, empty for a field whose
	// type cannot hold a value an int64 register cannot.
	guard string
}

// integerValue is the Go expression an integer register is bound from.
func (f *filer) integerValue(t transition, node *irpb.Node, source *irpb.Field) (integerBinding, error) {
	typ, err := f.fieldType(source)
	if err != nil {
		return integerBinding{}, err
	}

	path, found, err := f.pathTo(t.record.GetRootId(), node.GetId(), "rec")
	if err != nil {
		return integerBinding{}, err
	}

	if !found {
		return integerBinding{}, malformed(fmt.Sprintf("%s is bound into a register and is not in the record the transition admits", originalOf(node)),
			"a producer MUST NOT name a field of any other record; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
	}

	switch typ {
	// A register holds an int64, so every signed integer type but the big one
	// widens into it and cannot lose anything on the way.
	case "int16", "int32", "int64":
		return integerBinding{value: "int64(" + path + ")", path: path}, nil
	// An unsigned binary item takes a uint64 whatever its digit count, and that
	// one does not widen for free. A COMP-5 item is bounded by its storage
	// rather than by the decimal range its PICTURE declares — which is the
	// whole reason it is read unsigned — so an unsigned PIC 9(18) COMP-5 is
	// eight bytes and reads back anything up to 2^64 - 1. Converting that to
	// int64 unchecked would bind a negative number from a positive one and
	// every guard downstream would then test the wrong value, silently. The
	// reader refuses it instead; see [integerBinding].
	case "uint64":
		return integerBinding{value: "int64(" + path + ")", path: path, guard: path + " > " + maxInt64}, nil
	case bigIntType:
		return integerBinding{value: path + ".Int64()", path: path}, nil
	default:
		return integerBinding{}, malformed(fmt.Sprintf("%s is bound into an integer register and its value is a %s", originalOf(node), typ),
			"a producer MUST NOT bind a field whose value does not decode to the register's kind; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
	}
}

// rawExpr is where the bytes of the record in hand live, which is not the same
// place in both directions: a reader holds the record the framing bounds, or
// the bytes it kept while decoding one the framing does not; a writer holds the
// record it laid out before choosing a transition for it.
func (f *filer) rawExpr(holder string) string {
	if holder != "r" {
		return "raw"
	}

	if f.how.bounded() {
		return "r.data"
	}

	return "r.raw"
}

// emitAccepts writes the reader's end-of-file check.
func (f *filer) emitAccepts(b *strings.Builder) error {
	line(b, "")
	line(b, "// accepts is the truncation rule: a file is complete where the state the read")
	line(b, "// ends in accepts and its acceptance guards all hold, and truncated otherwise.")
	line(b, "//")
	line(b, "// Guarded acceptance is what makes the last iteration of a count detectable. A")
	line(b, "// state that reads details while a counter is positive would otherwise have to")
	line(b, "// accept unconditionally, and would accept a file stopping three details short")
	line(b, "// of what its own header promised.")
	line(b, "func (r *%s) accepts() error {", readerType)

	if err := f.acceptance(b, "r", "the file ends"); err != nil {
		return err
	}

	line(b, "}")

	return nil
}

// reports whether any state has a report to make: a state offering one
// transition that carries neither a predicate nor a guard admits whatever is in
// front of the reader and never gets there.
func (f *filer) reports(walks [][]transition) bool {
	for _, walk := range walks {
		if len(walk) == 1 && walk[0].match == "" && len(walk[0].node.GetGuardIds()) == 0 {
			continue
		}

		return true
	}

	return false
}

// emitReaderDiagnostics writes the two reports a walk has to be able to make.
func (f *filer) emitReaderDiagnostics(b *strings.Builder, walks [][]transition) {
	if f.reports(walks) {
		f.emitUndescribed(b)
	}

	if !f.readsRegisters(walks) {
		return
	}

	line(b, "")
	line(b, "// unbound is a register read before anything wrote it, which is a malformed")
	line(b, "// descriptor rather than a zero. A consumer supplying one would read a file")
	line(b, "// against a count nobody stated.")
	line(b, "func (r *%s) unbound(node int) error {", readerType)
	line(b, "return fmt.Errorf(\"record %%d reads the register the descriptor carries as node %%d, and no transition taken before it bound one\", r.ordinal+1, node)")
	line(b, "}")
}

// emitUndescribed writes the report a state makes when nothing it offers took
// the record in front of the reader.
func (f *filer) emitUndescribed(b *strings.Builder) {
	line(b, "")
	line(b, "// undescribed is what a state says when none of its transitions took the record")
	line(b, "// in front of the reader.")
	line(b, "//")
	line(b, "// A record excluded by a guard is reported as that rather than as a record the")
	line(b, "// layout does not describe, because the two send a reader of the diagnostic to")
	line(b, "// different places: the first is a record that does not belong at this point in")
	line(b, "// the file, the second a record type the layout is missing.")
	line(b, "func (r *%s) undescribed(expected []string, excluded string) error {", readerType)
	line(b, "if excluded != \"\" {")
	line(b, "return fmt.Errorf(\"record %%d does not belong here: %%s\", r.ordinal, excluded)")
	line(b, "}")
	line(b, "")

	if f.needsShort() {
		line(b, "if r.short {")
		line(b, "return fmt.Errorf(\"the file ends part-way through record %%d, which is not all there\", r.ordinal)")
		line(b, "}")
		line(b, "")
	}

	line(b, "if len(expected) == 0 {")
	line(b, "return fmt.Errorf(\"record %%d is a record the layout does not describe, and the automaton is in a state admitting none\", r.ordinal)")
	line(b, "}")
	line(b, "")
	line(b, "return fmt.Errorf(\"record %%d is a record the layout does not describe, and the automaton expected one of %%s\", r.ordinal, strings.Join(expected, \", \"))")
	line(b, "}")
}

// readsRegisters reports whether anything in the automaton reads a register:
// a guard on a transition, the acceptance guards of a state, or the count of a
// repetition.
func (f *filer) readsRegisters(walks [][]transition) bool {
	for _, node := range f.states {
		if len(node.GetState().GetAcceptanceGuardIds()) != 0 {
			return true
		}
	}

	for _, walk := range walks {
		for _, t := range walk {
			if len(t.node.GetGuardIds()) != 0 || len(t.node.GetBindingIds()) != 0 {
				return true
			}

			counts, err := f.registerCounts(t.record)
			if err == nil && len(counts) != 0 {
				return true
			}
		}
	}

	return false
}

// emitLeading writes step 2 of the read loop, and takes the window a predicate
// is evaluated against.
func (f *filer) emitLeading(b *strings.Builder) {
	line(b, "")
	line(b, "// leading consumes the framing in front of a record%s.", f.leadingSays())
	line(b, "//")
	line(b, "// Framing bytes belong to the dataset and not to any record: no item covers")
	line(b, "// them, they are not slack, and no predicate ever sees one.")
	line(b, "func (r *%s) leading() error {", readerType)

	switch f.how {
	case unframed:
		f.emitPeek(b)
	case descriptorWord:
		line(b, "var word [%d]byte", segmentDescriptorWidth)
		line(b, "")
		line(b, "if _, err := io.ReadFull(r.src, word[:]); err != nil {")
		line(b, "return fmt.Errorf(\"the file ends inside the record descriptor word in front of record %%d: %%v\", r.ordinal, err)")
		line(b, "}")
		line(b, "")
		line(b, "stated := int(word[0])<<8 | int(word[1])")
		line(b, "if stated < %d {", segmentDescriptorWidth)
		line(b, "return fmt.Errorf(\"the record descriptor word in front of record %%d states %%d bytes, and %d of them are the word itself\", r.ordinal, stated)", segmentDescriptorWidth)
		line(b, "}")
		line(b, "")
		line(b, "r.data = r.data[:0]")
		line(b, "")
		line(b, "if err := r.fill(stated - %d); err != nil {", segmentDescriptorWidth)
		line(b, "return fmt.Errorf(\"the file ends part-way through record %%d, which its record descriptor word states is %%d bytes: %%v\", r.ordinal, stated-%d, err)", segmentDescriptorWidth)
		line(b, "}")
		line(b, "")

		if f.needsLook() {
			line(b, "r.look = r.data")
			line(b, "")
		}

		line(b, "return nil")
	case segmented:
		f.emitSegments(b)
	case delimited:
		if f.placement == irpb.DelimiterPlacement_DELIMITER_PLACEMENT_SEPARATOR {
			line(b, "if !r.first {")
			line(b, "if err := r.expect(\"in front of\"); err != nil {")
			line(b, "return err")
			line(b, "}")
			line(b, "")
			line(b, "// A delimiter with nothing behind it announces a record that is not")
			line(b, "// there, and a separator stands between two records.")
			line(b, "if _, err := r.src.Peek(1); errors.Is(err, io.EOF) {")
			line(b, "return fmt.Errorf(\"the file ends with a delimiter behind record %%d, and a separator stands between two records: it announces a record that is not there\", r.ordinal-1)")
			line(b, "}")
			line(b, "}")
			line(b, "")
			line(b, "r.first = false")
			line(b, "")
		}

		f.emitPeek(b)
	}

	line(b, "}")
}

// leadingSays is the tail of leading's doc comment, which says what that
// framing puts in front of a record.
func (f *filer) leadingSays() string {
	switch f.how {
	case unframed:
		return ", of which an unframed file has none: a record begins at the byte\n// after the record before it"
	case descriptorWord:
		return ": the record descriptor word DFSMS defines,\n// whose length this reader checks against the extent of the record it admits\n// rather than taking a record's end from"
	case segmented:
		return ": every segment descriptor word of the record,\n// whose segments' data are reassembled into one contiguous run before any\n// predicate runs, so that every other rule reads a record the file may have\n// split"
	case delimited:
		if f.placement == irpb.DelimiterPlacement_DELIMITER_PLACEMENT_SEPARATOR {
			return ": the delimiter in front of every record other\n// than the first"
		}

		return ", of which this file has none: its delimiter\n// follows a record rather than standing in front of one"
	}

	return ""
}

// emitPeek writes the window an unbounded framing gives a predicate.
func (f *filer) emitPeek(b *strings.Builder) {
	if !f.needsLook() {
		line(b, "return nil")

		return
	}

	line(b, "// The framing states nothing about this record's length, so what bounds a")
	line(b, "// predicate is the input: every target a state can evaluate lies inside the")
	line(b, "// shortest record that state can admit, so the bytes are there whenever the")
	line(b, "// input holds a whole record, and a window that came up short is a file cut")
	line(b, "// short rather than a record the layout does not describe.")
	line(b, "window, err := r.src.Peek(%s)", lookaheadConst)
	line(b, "if err != nil && !errors.Is(err, io.EOF) {")
	line(b, "return fmt.Errorf(\"reading record %%d: %%w\", r.ordinal, err)")
	line(b, "}")
	line(b, "")
	line(b, "r.look, r.short = window, len(window) < %s", lookaheadConst)
	line(b, "")
	line(b, "return nil")
}

// emitSegments writes the reassembly a segmented file needs.
func (f *filer) emitSegments(b *strings.Builder) {
	line(b, "r.data = r.data[:0]")
	line(b, "")
	line(b, "for {")
	line(b, "var word [%d]byte", segmentDescriptorWidth)
	line(b, "")
	line(b, "if _, err := io.ReadFull(r.src, word[:]); err != nil {")
	line(b, "return fmt.Errorf(\"the file ends inside a segment descriptor word of record %%d: %%v\", r.ordinal, err)")
	line(b, "}")
	line(b, "")
	line(b, "stated := int(word[0])<<8 | int(word[1])")
	line(b, "if stated < %d {", segmentDescriptorWidth)
	line(b, "return fmt.Errorf(\"a segment descriptor word of record %%d states %%d bytes, and %d of them are the word itself\", r.ordinal, stated)", segmentDescriptorWidth)
	line(b, "}")
	line(b, "")
	line(b, "began := len(r.data) != 0")
	line(b, "")
	line(b, "if err := r.fill(stated - %d); err != nil {", segmentDescriptorWidth)
	line(b, "return fmt.Errorf(\"the file ends part-way through a segment of record %%d: %%v\", r.ordinal, err)")
	line(b, "}")
	line(b, "")
	line(b, "switch word[2] {")
	line(b, "case 0x00, 0x02: // a complete record, and the last segment of one")
	line(b, "if began != (word[2] == 0x02) {")
	line(b, "return fmt.Errorf(\"the segments of record %%d do not make one record: a segment control code of %%#02x stands where it cannot\", r.ordinal, word[2])")
	line(b, "}")
	line(b, "")

	if f.needsLook() {
		line(b, "r.look = r.data")
		line(b, "")
	}

	line(b, "return nil")
	line(b, "case 0x01, 0x03: // the first segment of a record, and a middle one")
	line(b, "if began != (word[2] == 0x03) {")
	line(b, "return fmt.Errorf(\"the segments of record %%d do not make one record: a segment control code of %%#02x stands where it cannot\", r.ordinal, word[2])")
	line(b, "}")
	line(b, "default:")
	line(b, "return fmt.Errorf(\"a segment of record %%d carries the segment control code %%#02x, which names no segment\", r.ordinal, word[2])")
	line(b, "}")
	line(b, "}")
}

// emitAdmit writes steps 4 and 5: the record is decoded, its end comes from its
// extent, and the framing is checked against that extent.
func (f *filer) emitAdmit(b *strings.Builder) {
	line(b, "")
	line(b, "// admit decodes one record and takes its end from its extent.")
	line(b, "//")
	line(b, "// The input is never searched for a delimiter and a descriptor word's length is")
	line(b, "// never preferred to the extent. Scanning is the obvious way to write a")
	line(b, "// line-oriented reader and is wrong on exactly these files: a PIC S9(3)V99")
	line(b, "// COMP-3 field holding +152.50 is the bytes 15 25 0C, and both 0x15 and 0x25")
	line(b, "// are line delimiters on some mainframe code page. A reader counting the extent")
	line(b, "// never reads those bytes as anything but the number they are.")
	line(b, "func (r *%s) admit(rec %s) error {", readerType, recordInterface)

	if f.how.bounded() {
		line(b, "src := bytes.NewReader(r.data)")
		line(b, "")
		line(b, "cr, err := codec.NewReader(src, r.enc)")
		line(b, "if err != nil {")
		line(b, "return fmt.Errorf(\"reading record %%d: %%w\", r.ordinal, err)")
		line(b, "}")
		line(b, "")
		line(b, "if err := rec.UnmarshalCOBOL(cr); err != nil {")
		line(b, "if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {")
		line(b, "return fmt.Errorf(\"the framing states record %%d is %%d bytes and the extent of the record it admits is longer than that: %%v\", r.ordinal, len(r.data), err)")
		line(b, "}")
		line(b, "")
		line(b, "return fmt.Errorf(\"reading record %%d: %%w\", r.ordinal, err)")
		line(b, "}")
		line(b, "")
		line(b, "// The framing checked against the extent, which is what a framed file buys")
		line(b, "// over an unframed one: a wrong width or a mis-selected transition is an")
		line(b, "// error at the record it happened on rather than silence.")
		line(b, "if src.Len() != 0 {")
		line(b, "return fmt.Errorf(\"the framing states record %%d is %%d bytes and the extent of the record it admits is %%d\", r.ordinal, len(r.data), len(r.data)-src.Len())")
		line(b, "}")
		line(b, "")
		line(b, "return nil")
		line(b, "}")

		f.emitFill(b)

		return
	}

	if f.keepsBytes {
		line(b, "r.raw = r.raw[:0]")
		line(b, "")
		line(b, "cr, err := codec.NewReader(&%s{src: r.src, into: &r.raw}, r.enc)", recordingType)
	} else {
		line(b, "cr, err := codec.NewReader(r.src, r.enc)")
	}

	line(b, "if err != nil {")
	line(b, "return fmt.Errorf(\"reading record %%d: %%w\", r.ordinal, err)")
	line(b, "}")
	line(b, "")
	line(b, "if err := rec.UnmarshalCOBOL(cr); err != nil {")
	line(b, "if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {")
	line(b, "return fmt.Errorf(\"the file ends part-way through record %%d: %%v\", r.ordinal, err)")
	line(b, "}")
	line(b, "")
	line(b, "return fmt.Errorf(\"reading record %%d: %%w\", r.ordinal, err)")
	line(b, "}")
	line(b, "")
	line(b, "return nil")
	line(b, "}")
}

// emitFill writes the append the two bounded framings read a record with.
func (f *filer) emitFill(b *strings.Builder) {
	line(b, "")
	line(b, "// fill appends the next n bytes of the input to the record in hand.")
	line(b, "func (r *%s) fill(n int) error {", readerType)
	line(b, "if n < 0 {")
	line(b, "return io.ErrUnexpectedEOF")
	line(b, "}")
	line(b, "")
	line(b, "at := len(r.data)")
	line(b, "")
	line(b, "if cap(r.data)-at < n {")
	line(b, "grown := make([]byte, at+n)")
	line(b, "copy(grown, r.data)")
	line(b, "r.data = grown")
	line(b, "}")
	line(b, "")
	line(b, "r.data = r.data[:at+n]")
	line(b, "")
	line(b, "_, err := io.ReadFull(r.src, r.data[at:])")
	line(b, "")
	line(b, "return err")
	line(b, "}")
}

// trailingFraming reports whether anything stands behind a record.
func (f *filer) trailingFraming() bool {
	return f.how == delimited && f.placement != irpb.DelimiterPlacement_DELIMITER_PLACEMENT_SEPARATOR
}

// emitTrailing writes step 6 of the read loop, where the framing has something
// behind a record.
func (f *filer) emitTrailing(b *strings.Builder) {
	if f.how != delimited {
		return
	}

	line(b, "")
	line(b, "// expect consumes one delimiter and reports where it is not the bytes this")
	line(b, "// file's delimiter is — which is a delimiter that is not where the extent of a")
	line(b, "// record ends.")
	line(b, "func (r *%s) expect(where string) error {", readerType)
	line(b, "found := make([]byte, len(%s))", delimiterVar)
	line(b, "")
	line(b, "if _, err := io.ReadFull(r.src, found); err != nil {")
	line(b, "return fmt.Errorf(\"the file ends where the delimiter %%s record %%d stands, so it was cut short: %%v\", where, r.ordinal, err)")
	line(b, "}")
	line(b, "")
	line(b, "if !bytes.Equal(found, %s) {", delimiterVar)
	line(b, "return fmt.Errorf(\"the bytes where the delimiter %%s record %%d stand are not the delimiter, so that record's extent and the framing disagree\", where, r.ordinal)")
	line(b, "}")
	line(b, "")
	line(b, "return nil")
	line(b, "}")

	if !f.trailingFraming() {
		return
	}

	line(b, "")
	line(b, "// trailing consumes the delimiter behind a record.")

	if f.placement == irpb.DelimiterPlacement_DELIMITER_PLACEMENT_OPTIONAL_TERMINATOR {
		line(b, "//")
		line(b, "// The input MAY be at its end instead, and that is the file ending rather than")
		line(b, "// a record cut short. What the placement gives up is stated where it is")
		line(b, "// carried: a final record whose bytes were cut off is indistinguishable from")
		line(b, "// one whose delimiter was never written.")
	} else {
		line(b, "//")
		line(b, "// A final record with nothing behind it is a file that was cut short, because")
		line(b, "// a terminator follows every record, the last included.")
	}

	line(b, "func (r *%s) trailing() error {", readerType)

	if f.placement == irpb.DelimiterPlacement_DELIMITER_PLACEMENT_OPTIONAL_TERMINATOR {
		line(b, "if _, err := r.src.Peek(1); errors.Is(err, io.EOF) {")
		line(b, "return nil")
		line(b, "}")
		line(b, "")
	}

	line(b, "return r.expect(\"behind\")")
	line(b, "}")
}

// emitHelpers writes the two package-level helpers the walk needs, each only
// where something in this descriptor needs it.
func (f *filer) emitHelpers(b *strings.Builder, walks [][]transition) error {
	if f.keepsBytes && !f.how.bounded() {
		line(b, "")
		line(b, "// %s is the reader a record is decoded through where a binding writes a", recordingType)
		line(b, "// bytes register: it keeps what it hands on, so that the record's own bytes")
		line(b, "// are in hand once its values are.")
		line(b, "type %s struct {", recordingType)
		line(b, "src io.Reader")
		line(b, "into *[]byte")
		line(b, "}")
		line(b, "")
		line(b, "// Read implements io.Reader.")
		line(b, "func (t *%s) Read(p []byte) (int, error) {", recordingType)
		line(b, "n, err := t.src.Read(p)")
		line(b, "*t.into = append(*t.into, p[:n]...)")
		line(b, "")
		line(b, "return n, err")
		line(b, "}")
	}

	sized := false

	for _, walk := range walks {
		for _, t := range walk {
			counts, err := f.registerCounts(t.record)
			if err != nil {
				return err
			}

			if len(counts) != 0 {
				sized = true
			}
		}
	}

	if !sized {
		return nil
	}

	line(b, "")
	line(b, "// %s is s with n occurrences in it, reusing what it already has room for.", occurrencesFunc)
	line(b, "//")
	line(b, "// A function rather than a make, because a group inside a record is an anonymous")
	line(b, "// struct type and there is no name to write inside one. Type inference supplies")
	line(b, "// what there is no identifier for, and nothing here reflects.")
	line(b, "func %s[T any](s []T, n int) []T {", occurrencesFunc)
	line(b, "if n <= cap(s) {")
	line(b, "s = s[:n]")
	line(b, "clear(s)")
	line(b, "")
	line(b, "return s")
	line(b, "}")
	line(b, "")
	line(b, "return make([]T, n)")
	line(b, "}")

	return nil
}

// registerCount is one repeating item of a record whose number of occurrences a
// register decides.
type registerCount struct {
	// expr is where its occurrences live in Go, relative to the record.
	expr string

	// loops are the constant tables it sits inside, outermost first.
	loops []countLoopOver

	// item is what the copybook calls it, for a diagnostic.
	item string

	// register is the node holding the count, and the bounds are the ones the
	// copybook declared.
	register uint64
	minimum  uint32
	maximum  uint32
}

// countLoopOver is one `for` a register-counted table sits inside.
type countLoopOver struct{ variable, over string }

// registerCounts is every repeating item of a record whose count is a register.
//
// A register holds what a transition bound out of a record already read, so the
// number of occurrences is the automaton's rather than the record's: the reader
// sizes the table from it before the record is decoded, and the writer checks
// what its caller supplied against it. Neither derives the other.
func (f *filer) registerCounts(record *irpb.Record) ([]registerCount, error) {
	var out []registerCount

	if err := f.walkCounts(record.GetRootId(), "rec", nil, false, &out); err != nil {
		return nil, err
	}

	return out, nil
}

// walkCounts is [filer.registerCounts] over one group.
func (f *filer) walkCounts(id uint64, expr string, loops []countLoopOver, sliding bool, out *[]registerCount) error {
	members, err := f.flattened(id)
	if err != nil {
		return err
	}

	for _, memberID := range members {
		member, ok := f.nodes[memberID]
		if !ok {
			return unresolved(memberID)
		}

		var (
			rep   *irpb.Repetition
			names *irpb.Names
			kind  string
		)

		switch k := member.GetKind().(type) {
		case *irpb.Node_Field:
			rep, names, kind = k.Field.GetRepetition(), k.Field.GetNames(), "field"
		case *irpb.Node_Group:
			rep, names, kind = k.Group.GetRepetition(), k.Group.GetNames(), "group"
		default:
			continue
		}

		// An item the copybook gives no data-name is a run of retained bytes
		// and holds no table a register could size; a group carrying no name
		// has already been replaced here by its own members.
		if anonymous(names) {
			continue
		}

		name, err := identifier(kind, names)
		if err != nil {
			return err
		}

		at := expr + "." + name

		variable, isVariable := rep.GetCount().(*irpb.Repetition_Variable)

		if isVariable {
			if from, ok := variable.Variable.GetCount().(*irpb.VariableCount_RegisterId); ok {
				if sliding {
					return malformed(fmt.Sprintf("%s is counted by a register and sits inside a table whose own length is data", names.GetOriginal()),
						"a table a register counts is sized before the record is decoded, and a table inside one whose length is data has no such number in hand")
				}

				*out = append(*out, registerCount{
					expr:     at,
					loops:    append([]countLoopOver{}, loops...),
					item:     names.GetOriginal(),
					register: from.RegisterId,
					minimum:  variable.Variable.GetMinOccurrences(),
					maximum:  variable.Variable.GetMaxOccurrences(),
				})
			}
		}

		if kind != "group" {
			continue
		}

		inner := loops
		element := at
		below := sliding

		switch {
		case rep == nil:
		case isVariable:
			below = true

			element = at + "[0]"
		default:
			index := fmt.Sprintf("k%d", len(loops))
			inner = append(append([]countLoopOver{}, loops...), countLoopOver{variable: index, over: at})
			element = at + "[" + index + "]"
		}

		if err := f.walkCounts(memberID, element, inner, below, out); err != nil {
			return err
		}
	}

	return nil
}

// pathTo is where the field target lives in Go relative to the record rooted at
// the group root, and whether it is in that record at all.
//
// Only a group that does not repeat is descended into. A binding and a
// predicate both name a field, not an occurrence of one, so a target inside a
// table is refused where it is read rather than found here.
func (f *filer) pathTo(root, target uint64, expr string) (string, bool, error) {
	members, err := f.flattened(root)
	if err != nil {
		return "", false, err
	}

	for _, memberID := range members {
		member, ok := f.nodes[memberID]
		if !ok {
			return "", false, unresolved(memberID)
		}

		var (
			rep   *irpb.Repetition
			names *irpb.Names
			kind  string
		)

		switch k := member.GetKind().(type) {
		case *irpb.Node_Field:
			rep, names, kind = k.Field.GetRepetition(), k.Field.GetNames(), "field"
		case *irpb.Node_Group:
			rep, names, kind = k.Group.GetRepetition(), k.Group.GetNames(), "group"
		default:
			continue
		}

		// An item the copybook gives no data-name is not a path to anything: a
		// binding and a predicate each name a field, and a FILLER is a field
		// nothing can name.
		if anonymous(names) {
			continue
		}

		name, err := identifier(kind, names)
		if err != nil {
			return "", false, err
		}

		if memberID == target {
			return expr + "." + name, true, nil
		}

		if kind != "group" || rep != nil {
			continue
		}

		below, found, err := f.pathTo(memberID, target, expr+"."+name)
		if err != nil {
			return "", false, err
		}

		if found {
			return below, true, nil
		}
	}

	return "", false, nil
}

// escaped is s with its percent signs doubled, for embedding in a format
// string. A copybook name carries none, and a guard's literal may.
func escaped(s string) string { return strings.ReplaceAll(s, "%", "%%") }
