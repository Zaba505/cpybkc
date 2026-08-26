// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Zaba505/cpybkc/irpb"
)

// emitWriter writes the file-level writer.
//
// It walks the same automaton in the other direction, and the difference is
// that a predicate is a test rather than a recipe: a writer evaluates one
// against the record it is about to emit and never derives a value satisfying
// it. See docs/ir/SPEC.md, "A writer evaluates a predicate, it never inverts
// one".
func (f *filer) emitWriter(b *strings.Builder, walks [][]transition) error {
	admitted := f.admitted(walks)

	line(b, "")
	line(b, "// %s writes the records of one file, walking the automaton this descriptor", writerType)
	line(b, "// carries in the other direction.")
	line(b, "//")
	line(b, "// Its caller names a record and supplies that record's values. It narrows to")
	line(b, "// the transitions leaving the state it is in that admit such a record and whose")
	line(b, "// guards all hold, evaluates their predicates in the order the state carries")
	line(b, "// them, and takes the first that carries no predicate or whose predicate matches")
	line(b, "// the bytes it is about to emit. A record satisfying none is reported rather")
	line(b, "// than emitted: refusing where the mistake was made costs one diagnostic, and")
	line(b, "// emitting costs a file somebody has to read before anyone finds out.")
	line(b, "//")
	line(b, "// Nothing is buffered beyond the record in hand. A count in a header is never")
	line(b, "// back-filled from the records behind it, because holding a group to count it")
	line(b, "// gives up exactly the streaming this type exists to have — and because a")
	line(b, "// writer that has emitted a record cannot reach back into a stream it does not")
	line(b, "// own.")
	line(b, "type %s struct {", writerType)
	line(b, "dst io.Writer")
	line(b, "")
	line(b, "// cw lays out the record in hand, and is the only encoder this writer")
	line(b, "// builds: a predicate is evaluated against the bytes that are about to go")
	line(b, "// out, so a record exists as bytes before the transition carrying it is")
	line(b, "// chosen, and the method emitting a record rewinds this onto it rather")
	line(b, "// than constructing one over it. The buffer those bytes accumulate in is")
	line(b, "// this encoder's own — [codec.Writer.Bytes] is where they are read back —")
	line(b, "// and it is kept at its capacity across every rewind, along with everything")
	line(b, "// the encoding derives. That is why the encoding is not kept beside it:")
	line(b, "// codec.Writer carries it, and one that could be swapped under a half-laid")
	line(b, "// record is what codec refuses to allow.")
	line(b, "cw *codec.Writer")
	line(b, "")
	line(b, "// state is where in the automaton the write is, numbered as [%s.state] is.", readerType)
	line(b, "state int")
	line(b, "")
	line(b, "// ordinal is how many records have been written, so that a diagnostic can")
	line(b, "// say where.")
	line(b, "ordinal int")

	if f.how == delimited && f.placement == irpb.DelimiterPlacement_DELIMITER_PLACEMENT_SEPARATOR {
		line(b, "")
		line(b, "// first is whether the next record is the first, which is the one a")
		line(b, "// separator does not stand in front of. A separator is emitted in front of")
		line(b, "// each record other than the first rather than behind each but the last,")
		line(b, "// because a writer does not learn which record is last until its caller")
		line(b, "// stops, and one that waited to find out would be holding a record it had")
		line(b, "// already been given.")
		line(b, "first bool")
	}

	if err := f.registerFields(b, "write"); err != nil {
		return err
	}

	line(b, "}")

	f.emitNewWriter(b)

	if err := f.emitWrite(b, admitted); err != nil {
		return err
	}

	if err := f.emitClose(b); err != nil {
		return err
	}

	if err := f.emitWriteRecords(b, walks, admitted); err != nil {
		return err
	}

	f.emitWriterDiagnostics(b, walks)
	f.emitEmit(b)

	return nil
}

// admitted is every record type some transition admits, in ascending record
// node order, with the Go type each one became.
func (f *filer) admitted(walks [][]transition) []transition {
	seen := make(map[string]transition)

	for _, walk := range walks {
		for _, t := range walk {
			seen[t.typ] = t
		}
	}

	out := make([]transition, 0, len(seen))
	for _, t := range seen {
		out = append(out, t)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].typ < out[j].typ })

	return out
}

// emitNewWriter writes the constructor.
func (f *filer) emitNewWriter(b *strings.Builder) {
	line(b, "")
	line(b, "// %s writes records into w under enc.", newWriterFunc)
	line(b, "//")
	line(b, "// The five axes are the caller's for the reason they are on [%s]: they are", newReaderFunc)
	line(b, "// properties of the file being written rather than of this descriptor's items.")
	line(b, "func %s(w io.Writer, enc codec.Encoding) (*%s, error) {", newWriterFunc, writerType)
	line(b, "if w == nil {")
	line(b, "return nil, codec.ErrNilWriter")
	line(b, "}")
	line(b, "")
	line(b, "// The one encoder this writer builds, over a buffer of no bytes until the")
	line(b, "// first record is laid into it. Construction is what validates the")
	line(b, "// encoding, and it reports the same error for the same axis that")
	line(b, "// enc.Validate does, so nothing is checked twice here.")
	line(b, "cw, err := codec.NewBytesWriter(nil, enc)")
	line(b, "if err != nil {")
	line(b, "return nil, err")
	line(b, "}")
	line(b, "")
	line(b, "return &%s{", writerType)
	line(b, "dst: w,")
	line(b, "cw: cw,")
	line(b, "state: %d,", f.index[f.file.GetStartStateId()])

	if f.how == delimited && f.placement == irpb.DelimiterPlacement_DELIMITER_PLACEMENT_SEPARATOR {
		line(b, "first: true,")
	}

	line(b, "}, nil")
	line(b, "}")
}

// emitWrite writes the entry point, which is a type switch over the record
// types this file's transitions admit.
func (f *filer) emitWrite(b *strings.Builder, admitted []transition) error {
	names := make([]string, 0, len(admitted))
	for _, t := range admitted {
		names = append(names, t.record.GetNames().GetOriginal())
	}

	line(b, "")
	line(b, "// Write emits one record.")
	line(b, "//")
	line(b, "// A value that is not one of this file's record types is reported rather than")
	line(b, "// emitted: no transition admits a record the descriptor does not carry, and")
	line(b, "// [%s] is satisfied by anything codec can encode.", recordInterface)
	line(b, "func (w *%s) Write(rec %s) error {", writerType, recordInterface)

	if len(admitted) == 0 {
		line(b, "switch rec.(type) {")
	} else {
		line(b, "switch v := rec.(type) {")
	}

	for _, t := range admitted {
		line(b, "case *%s:", t.typ)
		line(b, "return w.write%s(v)", t.typ)
	}

	line(b, "default:")
	line(b, "return fmt.Errorf(%q, w.ordinal+1, rec)",
		fmt.Sprintf("record %%d: this file's records are %s, and a %%T is none of them", escaped(joinNames(names))))
	line(b, "}")
	line(b, "}")

	return nil
}

// joinNames is a list of record names as a sentence names them.
func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return "none at all"
	case 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}

// emitClose writes the writer's end-of-file check.
func (f *filer) emitClose(b *strings.Builder) error {
	line(b, "")
	line(b, "// Close ends the file, and reports a state that does not accept — or whose")
	line(b, "// acceptance guards do not all hold — rather than closing it.")
	line(b, "//")
	line(b, "// A group that promised four details and was given three is caught here. It is")
	line(b, "// the reader's truncation rule from the other side, and a writer skipping the")
	line(b, "// check emits the truncated file its reader complains about one build later.")
	line(b, "//")
	line(b, "// It does not close the io.Writer it was handed, which belongs to the caller.")
	line(b, "func (w *%s) Close() error {", writerType)

	if err := f.acceptance(b, "w", "the file is closed"); err != nil {
		return err
	}

	line(b, "}")

	return nil
}

// emitWriteRecords writes one method per record type a transition admits.
func (f *filer) emitWriteRecords(b *strings.Builder, walks [][]transition, admitted []transition) error {
	for _, record := range admitted {
		original := record.record.GetNames().GetOriginal()

		line(b, "")
		line(b, "// write%s emits one %s.", record.typ, original)
		line(b, "//")
		line(b, "// The record is laid out first and the transition is chosen against those")
		line(b, "// bytes, because what a predicate tests is what is about to be emitted.")
		line(b, "func (w *%s) write%s(rec *%s) error {", writerType, record.typ, record.typ)

		counts, err := f.registerCounts(record.record)
		if err != nil {
			return err
		}

		for _, count := range counts {
			f.emitCountCheck(b, count)
		}

		line(b, "// The encoder is rewound onto the buffer it filled for the record before")
		line(b, "// this one rather than built over a fresh one. A rewind keeps everything")
		line(b, "// the encoding derives and the capacity that buffer reached, and it puts")
		line(b, "// the offset back to zero, so every offset codec reports is counted from")
		line(b, "// the start of this record rather than from the start of the file.")
		line(b, "w.cw.Reset(w.cw.Bytes())")
		line(b, "")
		line(b, "if err := rec.MarshalCOBOL(w.cw); err != nil {")
		line(b, "return fmt.Errorf(\"writing record %%d: %%w\", w.ordinal+1, err)")
		line(b, "}")
		line(b, "")
		line(b, "// The record's bytes, which are the encoder's own buffer and are valid")
		line(b, "// until the rewind above happens again. Nothing below holds them past")
		line(b, "// that: a predicate reads them, the framing writes them out, and a")
		line(b, "// binding taking a register's bytes out of them copies.")
		line(b, "raw := w.cw.Bytes()")
		line(b, "")

		guarded := false

		for _, walk := range walks {
			for _, t := range walk {
				if t.typ == record.typ && len(t.node.GetGuardIds()) != 0 {
					guarded = true
				}
			}
		}

		if guarded {
			line(b, "var excluded string")
			line(b, "")
		}

		line(b, "switch w.state {")

		for i, walk := range walks {
			narrowed := make([]int, 0, len(walk))

			for j, t := range walk {
				if t.typ == record.typ {
					narrowed = append(narrowed, j)
				}
			}

			if len(narrowed) == 0 {
				continue
			}

			line(b, "case %d: // the state the descriptor carries as node %d", i, f.states[i].GetId())

			for _, j := range narrowed {
				if err := f.emitWriterTransition(b, walk[j], j); err != nil {
					return err
				}
			}
		}

		line(b, "}")
		line(b, "")

		if guarded {
			line(b, "return w.refuse(%q, excluded)", escaped(original))
		} else {
			line(b, "return w.refuse(%q, \"\")", escaped(original))
		}

		line(b, "}")
	}

	return nil
}

// emitWriterTransition writes one candidate transition of the narrowed walk.
func (f *filer) emitWriterTransition(b *strings.Builder, t transition, at int) error {
	line(b, "// Transition %d of that state.", at+1)

	test, phrase, registers, err := f.guardTests(t, "w")
	if err != nil {
		return err
	}

	if test != "" {
		for _, id := range registers {
			line(b, "if !w.%s {", held(id))
			line(b, "return w.unbound(%d)", id)
			line(b, "}")
			line(b, "")
		}

		line(b, "if %s {", test)
	}

	closing := ""

	if t.match != "" {
		line(b, "if %s(raw) {", f.matcherOf(t))

		closing = "}"
	}

	line(b, "if err := w.emit(raw); err != nil {")
	line(b, "return err")
	line(b, "}")
	line(b, "")

	if err := f.emitBindings(b, t, "w", "return "); err != nil {
		return err
	}

	line(b, "w.state = %d", t.next)
	line(b, "w.ordinal++")
	line(b, "")
	line(b, "return nil")

	if closing != "" {
		line(b, "%s", closing)
	}

	if test == "" {
		return nil
	}

	if t.match != "" {
		line(b, "} else if excluded == \"\" && %s(raw) {", f.matcherOf(t))
		line(b, "excluded = fmt.Sprintf(%q%s)",
			fmt.Sprintf("a guard excluded the transition that would have taken it, which is taken only where %s%s",
				escaped(phrase), f.holding(registers)),
			f.holdingArgs(registers, "w"))
		line(b, "}")
	} else {
		line(b, "} else if excluded == \"\" {")
		line(b, "excluded = fmt.Sprintf(%q%s)",
			fmt.Sprintf("a guard excluded the transition that would have taken it, which is taken only where %s%s",
				escaped(phrase), f.holding(registers)),
			f.holdingArgs(registers, "w"))
		line(b, "}")
	}

	return nil
}

// emitCountCheck writes the writer's half of a table a register counts.
//
// The register was filled two records ago and what has to agree with it is the
// data, so the caller's occurrences are checked against it rather than the
// other way round. Where two repetitions of one record name that one register,
// each is checked against it: neither of them sets it, so there is nothing to
// pick between. See docs/ir/SPEC.md, "A writer evaluates a guard, it never
// back-fills a count".
func (f *filer) emitCountCheck(b *strings.Builder, count registerCount) {
	for _, loop := range count.loops {
		line(b, "for %s := range %s {", loop.variable, loop.over)
	}

	line(b, "if !w.%s {", held(count.register))
	line(b, "return w.unbound(%d)", count.register)
	line(b, "}")
	line(b, "")
	line(b, "if int64(len(%s)) != w.%s {", count.expr, register(count.register))
	line(b, "return fmt.Errorf(%q, w.ordinal+1, w.%s, len(%s))",
		fmt.Sprintf("record %%d: %s occurs as many times as the register the descriptor carries as node %d holds, which is %%d, and the record holds %%d occurrences of it",
			escaped(count.item), count.register),
		register(count.register), count.expr)
	line(b, "}")
	line(b, "")

	// The bounds the copybook declared, checked whatever the register holds,
	// because those are the copybook's. A minimum of zero is left out rather
	// than emitted as a comparison no slice length can fail.
	bound := fmt.Sprintf("len(%s) > %d", count.expr, count.maximum)
	if count.minimum > 0 {
		bound = fmt.Sprintf("len(%s) < %d || %s", count.expr, count.minimum, bound)
	}

	line(b, "if %s {", bound)
	line(b, "return fmt.Errorf(%q, w.ordinal+1, len(%s))",
		fmt.Sprintf("record %%d: %s occurs %d to %d times and the record holds %%d occurrences of it",
			escaped(count.item), count.minimum, count.maximum),
		count.expr)
	line(b, "}")

	for range count.loops {
		line(b, "}")
	}

	line(b, "")
}

// emitWriterDiagnostics writes the two reports the writing walk has to be able
// to make.
func (f *filer) emitWriterDiagnostics(b *strings.Builder, walks [][]transition) {
	line(b, "")
	line(b, "// refuse is what a writer says when no transition leaving the state it is in")
	line(b, "// took the record it was asked to write.")
	line(b, "//")
	line(b, "// Two things bring a caller here and the message covers both, because the")
	line(b, "// writer's walk is narrowed before any predicate runs: the state may admit no")
	line(b, "// record of this type at all, which is a record written out of order, or it may")
	line(b, "// admit one and be selected by a predicate this record's values do not satisfy.")
	line(b, "//")
	line(b, "// Either way the record does not belong at this point in the file with the")
	line(b, "// values it has, and the value a predicate tests is the caller's: a writer")
	line(b, "// checks it and reports it when it is wrong rather than deriving one that would")
	line(b, "// satisfy the test and storing it into the field.")
	line(b, "func (w *%s) refuse(record string, excluded string) error {", writerType)
	line(b, "if excluded != \"\" {")
	line(b, "return fmt.Errorf(\"record %%d is a %%s and does not belong here: %%s\", w.ordinal+1, record, excluded)")
	line(b, "}")
	line(b, "")
	line(b, "return fmt.Errorf(\"record %%d is a %%s and no transition leaving the state the automaton is in takes it: that state admits no %%s here, or one it admits is selected by a predicate this record's values do not satisfy. It is reported rather than emitted\", w.ordinal+1, record, record)")
	line(b, "}")

	if !f.readsRegisters(walks) {
		return
	}

	line(b, "")
	line(b, "// unbound is a register read before anything wrote it, which is a malformed")
	line(b, "// descriptor rather than a zero.")
	line(b, "func (w *%s) unbound(node int) error {", writerType)
	line(b, "return fmt.Errorf(\"record %%d reads the register the descriptor carries as node %%d, and no transition taken before it bound one\", w.ordinal+1, node)")
	line(b, "}")
}

// emitEmit writes the framing bytes around a record, at the same points of the
// same walk the reader consumes them at.
func (f *filer) emitEmit(b *strings.Builder) {
	line(b, "")
	line(b, "// emit writes one record's bytes with this file's framing around them.")

	switch f.how {
	case unframed:
		line(b, "//")
		line(b, "// An unframed file has none: a record is its extent and the next one begins at")
		line(b, "// the byte after it.")
	case descriptorWord:
		line(b, "//")
		line(b, "// The record descriptor word DFSMS defines stands in front of each record and")
		line(b, "// states the record's length including itself.")
	case segmented:
		line(b, "//")
		line(b, "// Each record is laid into as few segments as the largest segment this file")
		line(b, "// node carries allows, whatever the file it was read from did. That is the one")
		line(b, "// framing fact a writer needs and cannot compute, which is why the file node")
		line(b, "// carries it.")
	case delimited:
		line(b, "//")

		switch f.placement {
		case irpb.DelimiterPlacement_DELIMITER_PLACEMENT_SEPARATOR:
			line(b, "// The delimiter stands in front of every record other than the first, because")
			line(b, "// a writer does not learn which record is the last until its caller stops.")
		case irpb.DelimiterPlacement_DELIMITER_PLACEMENT_OPTIONAL_TERMINATOR:
			line(b, "// The final delimiter is emitted rather than chosen about. Two writers left to")
			line(b, "// decide produce two different files from one descriptor and one set of")
			line(b, "// records, which is the divergence this whole arrangement exists to abolish.")
		default:
			line(b, "// The delimiter follows every record, the last included.")
		}
	}

	line(b, "func (w *%s) emit(raw []byte) error {", writerType)

	switch f.how {
	case unframed:
		line(b, "_, err := w.dst.Write(raw)")
		line(b, "")
		line(b, "return err")
	case descriptorWord:
		line(b, "stated := len(raw) + %d", segmentDescriptorWidth)
		line(b, "if stated > 0xFFFF {")
		line(b, "return fmt.Errorf(\"record %%d is %%d bytes and a record descriptor word states a length of two bytes\", w.ordinal+1, len(raw))")
		line(b, "}")
		line(b, "")
		line(b, "if _, err := w.dst.Write([]byte{byte(stated >> 8), byte(stated), 0, 0}); err != nil {")
		line(b, "return err")
		line(b, "}")
		line(b, "")
		line(b, "_, err := w.dst.Write(raw)")
		line(b, "")
		line(b, "return err")
	case segmented:
		line(b, "const most = %d - %d", f.maxSegment, segmentDescriptorWidth)
		line(b, "")
		line(b, "for at := 0; ; {")
		line(b, "size := len(raw) - at")
		line(b, "if size > most {")
		line(b, "size = most")
		line(b, "}")
		line(b, "")
		line(b, "code := byte(0x03)")
		line(b, "")
		line(b, "switch {")
		line(b, "case at == 0 && size == len(raw):")
		line(b, "code = 0x00")
		line(b, "case at == 0:")
		line(b, "code = 0x01")
		line(b, "case at+size == len(raw):")
		line(b, "code = 0x02")
		line(b, "}")
		line(b, "")
		line(b, "stated := size + %d", segmentDescriptorWidth)
		line(b, "")
		line(b, "if _, err := w.dst.Write([]byte{byte(stated >> 8), byte(stated), code, 0}); err != nil {")
		line(b, "return err")
		line(b, "}")
		line(b, "")
		line(b, "if _, err := w.dst.Write(raw[at : at+size]); err != nil {")
		line(b, "return err")
		line(b, "}")
		line(b, "")
		line(b, "at += size")
		line(b, "")
		line(b, "if at >= len(raw) {")
		line(b, "return nil")
		line(b, "}")
		line(b, "}")
	case delimited:
		if f.placement == irpb.DelimiterPlacement_DELIMITER_PLACEMENT_SEPARATOR {
			line(b, "if !w.first {")
			line(b, "if _, err := w.dst.Write(%s); err != nil {", delimiterVar)
			line(b, "return err")
			line(b, "}")
			line(b, "}")
			line(b, "")
			line(b, "w.first = false")
			line(b, "")
			line(b, "_, err := w.dst.Write(raw)")
			line(b, "")
			line(b, "return err")
		} else {
			line(b, "if _, err := w.dst.Write(raw); err != nil {")
			line(b, "return err")
			line(b, "}")
			line(b, "")
			line(b, "_, err := w.dst.Write(%s)", delimiterVar)
			line(b, "")
			line(b, "return err")
		}
	}

	line(b, "}")
}
