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

// The identifiers the constructors' options occupy at package scope, beside
// the five the file-level reader and writer do.
//
// One option type, taken by both [newReaderFunc] and [newWriterFunc] because
// the axes are the same four in both directions, and one function per axis a
// layout states. There is deliberately none for the binary width staircase and
// none replacing the whole encoding: the staircase is the one axis every offset
// in the package was computed under, so it is not an axis a consumer may swap
// (docs/ir/SPEC.md, "A binary item's width is the staircase, not the digits"),
// and a whole encoding would carry one. A constructor that cannot be handed a
// staircase needs no refusal for one.
//
// Exported, so every one of them is a name a record type can munge to, and
// [filer.survey] refuses that exactly as it refuses a record called READER.
const (
	optionType        = "Option"
	withCharsetFunc   = "WithCharset"
	withSignFunc      = "WithSignConvention"
	withByteOrderFunc = "WithByteOrder"
	withFloatFunc     = "WithFloatFormat"
)

// encodingWithFunc is the unexported helper both constructors build their
// encoding with: the layout's, with each option's axis replaced. Lowercase for
// the reason [refuseStaircaseFunc] is.
const encodingWithFunc = "encodingWith"

// fileIdentifiers is every exported identifier the file-level reader and
// writer, and their options, occupy at package scope — the set a record type
// munging to one of is a collision.
var fileIdentifiers = []string{
	recordInterface, readerType, writerType, newReaderFunc, newWriterFunc,
	optionType, withCharsetFunc, withSignFunc, withByteOrderFunc, withFloatFunc,
}

// layoutEncoding is what the file-level constructors build under when handed
// no option, as a Go expression, beside the encoding the descriptor states
// where it declares [encodingFunc] and nil where it does not.
//
// Where the package declares [encodingFunc] it is that, which is the layout's
// own encoding. Where it declares none — no item of the descriptor states a
// charset — it is the axes the items do state, as a literal: every field
// carries a sign convention, a byte order, a float format and a staircase
// whatever its charset, and those are still the layout's. The charset is left
// unset, because the descriptor states none and a charset this generator chose
// would be a translation for bytes the layout said were not text; codec refuses
// an encoding without one, so the caller names it with [withCharsetFunc], and
// codec's own error says so where they have not. A descriptor carrying no field
// at all states no axis, and the literal is codec's zero Encoding.
func layoutEncoding(d *irpb.Descriptor) (string, *irpb.Encoding, error) {
	stated, err := descriptorEncoding(d)
	if err != nil {
		return "", nil, err
	}

	if stated != nil {
		return encodingFunc + "()", stated, nil
	}

	held := heldEncoding(d)
	if held == nil {
		return "codec.Encoding{}", nil, nil
	}

	binary, err := binarySize(held.GetBinarySize())
	if err != nil {
		return "", nil, err
	}

	return fmt.Sprintf("codec.Encoding{\nSign: %s,\nByteOrder: %s,\nFloat: %s,\nBinary: %s,\n}",
		signConvention(held.GetSignConvention()), byteOrder(held.GetByteOrder()), floatFormat(held.GetFloatFormat()), binary), nil, nil
}

// heldEncoding is the encoding of the descriptor's first field, or nil where it
// carries none. It is read after [descriptorEncoding] has held every field to
// the same four non-charset axes, so the first field's are every field's.
func heldEncoding(d *irpb.Descriptor) *irpb.Encoding {
	for _, node := range d.GetNodes() {
		if field := node.GetField(); field != nil {
			return field.GetEncoding()
		}
	}

	return nil
}

// convertedExample is the options a doc comment shows reading a converted copy
// of the file with: the charset and sign convention a transfer between EBCDIC
// and ASCII rewrites, in whichever direction leaves the layout's own.
func convertedExample(charset irpb.Charset) string {
	if charset == irpb.Charset_CHARSET_ASCII {
		return fmt.Sprintf("%s(codec.CP037()), %s(codec.SignEBCDIC)", withCharsetFunc, withSignFunc)
	}

	return fmt.Sprintf("%s(codec.ASCII()), %s(codec.SignTranslatedEBCDIC)", withCharsetFunc, withSignFunc)
}

// docBlock is a doc comment of paragraphs, each [wrapped] but a code line —
// one beginning with a tab — which is kept as it is.
func docBlock(paragraphs ...string) string {
	out := make([]string, len(paragraphs))

	for i, paragraph := range paragraphs {
		if strings.HasPrefix(paragraph, "\t") {
			out[i] = paragraph

			continue
		}

		out[i] = wrapped(paragraph)
	}

	return commentLines(strings.Join(out, "\n\n"))
}

// emitOptions writes the option type, one option per axis a layout states, and
// the helper applying them.
func (f *filer) emitOptions(b *strings.Builder) {
	base := "[" + encodingFunc + "], the layout's own encoding"
	if !f.declared {
		base = "the axes the layout states for this package's items, which name no charset, so one is given with [" + withCharsetFunc + "]"
	}

	line(b, "")
	b.WriteString(docBlock(
		fmt.Sprintf(`%s replaces one axis of the encoding a [%s] or a [%s] is built under. Handed none, [%s] and [%s] build under %s. Each option replaces the axis it names and no other, and where two name the same axis the later one holds.`,
			optionType, readerType, writerType, newReaderFunc, newWriterFunc, base),
		fmt.Sprintf(`There is one per axis a layout states — [%s], [%s], [%s] and [%s] — and nothing else is one. The binary width staircase has none: every offset this package slices at was computed under the descriptor's, so another does not read this file another way, it describes a different one. Nor is there one replacing the whole encoding, which would carry a staircase with it.`,
			withCharsetFunc, withSignFunc, withByteOrderFunc, withFloatFunc),
		`Its zero value replaces nothing.`,
	))
	line(b, "type %s struct {", optionType)
	line(b, "apply func(*codec.Encoding)")
	line(b, "}")

	const validated = `It is validated when the reader or the writer is built, and a value codec has no member for is refused there with the error codec reports for the axis.`

	for _, axis := range []struct {
		fn, param, typ, field, doc string
	}{
		{withCharsetFunc, "charset", "codec.Charset", "Charset", `%s reads or writes the file under charset rather than the charset the layout states: the same records, converted to another character set. Every literal this package compares a text item against is re-expressed under it when the reader or the writer is built.`},
		{withSignFunc, "sign", "codec.SignConvention", "Sign", `%s reads or writes the file's zoned items under sign rather than the sign convention the layout states — what a transfer that rewrote the characters of a sign byte leaves behind.`},
		{withByteOrderFunc, "order", "binary.ByteOrder", "ByteOrder", `%s reads or writes the file's binary items in order rather than the byte order the layout states.`},
		{withFloatFunc, "format", "codec.FloatFormat", "Float", `%s reads or writes the file's floating-point items in format rather than the floating-point format the layout states.`},
	} {
		line(b, "")
		b.WriteString(docBlock(fmt.Sprintf(axis.doc, axis.fn), validated))
		line(b, "func %s(%s %s) %s {", axis.fn, axis.param, axis.typ, optionType)
		line(b, "return %s{apply: func(enc *codec.Encoding) { enc.%s = %s }}", optionType, axis.field, axis.param)
		line(b, "}")
	}

	line(b, "")

	if f.declared {
		b.WriteString(docBlock(fmt.Sprintf(`%s is the encoding a reader or a writer handed opts is built under: [%s], with each option's axis replaced in the order they were handed.`, encodingWithFunc, encodingFunc)))
	} else {
		b.WriteString(docBlock(fmt.Sprintf(`%s is the encoding a reader or a writer handed opts is built under: the axes the layout states for this package's items, with each option's axis replaced in the order they were handed. No item states a charset, so none is set until an option sets one, and codec refuses the encoding until then.`, encodingWithFunc)))
	}

	line(b, "func %s(opts []%s) codec.Encoding {", encodingWithFunc, optionType)
	line(b, "enc := %s", f.base)
	line(b, "")
	line(b, "for _, opt := range opts {")
	line(b, "if opt.apply != nil {")
	line(b, "opt.apply(&enc)")
	line(b, "}")
	line(b, "}")
	line(b, "")
	line(b, "return enc")
	line(b, "}")
}

// emitConstructorDoc writes the doc comment [filer.emitNewReader] and
// [filer.emitNewWriter] open with: what the constructor builds under, the
// no-option call and an override, and what it refuses before any record is
// done — "read" or "written". verb is "reads" or "writes", and call is the
// constructor's call up to its first argument, which the two examples finish.
// tail is a paragraph the constructor adds where the file compares a literal.
func (f *filer) emitConstructorDoc(b *strings.Builder, fn, what, verb, done, call, tail string) {
	var paragraphs []string

	if f.declared {
		paragraphs = append(paragraphs,
			fmt.Sprintf(`%s %s under [%s], the layout's own encoding, with each of opts replacing the axis it names. Handed no option, it %s the file the layout describes:`, fn, what, encodingFunc, verb),
			"\t"+call+")",
			fmt.Sprintf(`A copy of that file converted to another character set holds the same records, and is %s by naming the axes the conversion rewrote:`, done),
			"\t"+call+", "+convertedExample(f.charset)+")",
		)
	} else {
		paragraphs = append(paragraphs,
			fmt.Sprintf(`%s %s under the axes the layout states for this package's items, with each of opts replacing the axis it names. No item states a charset, so there is none to default to, and one is named here:`, fn, what),
			"\t"+call+", "+withCharsetFunc+"(codec.CP037()))",
		)
	}

	paragraphs = append(paragraphs, fmt.Sprintf(`Each option is validated here, before any record is %s: a value codec has no member for is refused with the error codec reports for its axis. The binary width staircase is not among them — every offset this package slices at was computed under the descriptor's, and no [%s] replaces it.`, done, optionType))

	if f.compares || f.literals.arms {
		paragraphs = append(paragraphs,
			fmt.Sprintf(`Every literal this package compares a field against is re-expressed here, once, as a file under the encoding the options leave spells it; an item whose charset is none carries bytes, and its literals never move. A literal no file under that encoding can hold is refused here rather than at the record that would first have needed it, and the refusal names the literal, the item, the record and the axis: it is about the layout and the options, and not about the file. See %s.`, literalsFile))

		if tail != "" {
			paragraphs = append(paragraphs, tail)
		}
	}

	b.WriteString(docBlock(paragraphs...))
}

// readsFiles is whether the package generated for d declares the file-level
// reader and writer — a file node, and a state offering a transition — which
// is the test [fileMachineWith] makes, asked of the descriptor rather than of a
// filer so that codec.go's doc comments can say whether there are constructors
// to name.
func readsFiles(d *irpb.Descriptor) bool {
	var file, admits bool

	for _, node := range d.GetNodes() {
		switch {
		case node.GetFile() != nil:
			file = true
		case len(node.GetState().GetTransitionIds()) != 0:
			admits = true
		}
	}

	return file && admits
}
