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

// codecFile is the file the decode and encode methods are written to, beside
// the record structs they fill and read.
//
// A file of its own for the reason every other one is: the files this generator
// writes divide by what produced them. records.go is the descriptor's record
// nodes as Go types, and this is those types moving bytes.
const codecFile = "codec.go"

// codecImport is the runtime every byte-level decision is delegated to.
//
// docs/ir/SPEC.md carries no byte tables and this generator carries none
// either: what a zoned sign byte is, how wide a binary item of nine digits is,
// and what an IBM hexadecimal float looks like are all codec/SPEC.md's, and the
// generated code's whole job is to call the right accessor with the values the
// IR resolved. That is the same arrangement avroc-gen-go has with avro-go.
const codecImport = "github.com/Zaba505/cobol-go/codec"

// The unexported helpers the generated file declares for itself.
//
// Each is lowercase, and that is what keeps it out of the way of everything the
// copybook names: every identifier munged from a name is exported, so a
// copybook item called RESIZED or ZERO-FILL becomes Resized and ZeroFill and
// collides with neither. It is the argument [slackField] already makes.
const (
	zeroFillHelper = "zeroFill"
	resizedHelper  = "resized"
	freshHelper    = "fresh"
)

// coder writes one descriptor's decode and encode methods.
//
// It carries the emitter's node map and identifier munging so that the method
// bodies name exactly the fields records.go declared, and four things of its
// own: what the record being walked determines, what the file being written
// needs declared, and which of the two directions is being emitted.
type coder struct {
	*emitter

	// countOf is, for the record being walked, every repetition naming a count
	// field, by that field's identifier. It is what makes a writer able to
	// report a caller who supplied two numbers of occurrences for one count
	// (docs/ir/SPEC.md, "One count may size two tables, and a writer refuses to
	// choose").
	countOf map[uint64][]countUse

	// valueOf is, for the record being decoded, the Go expression each field
	// already read is held in. An OCCURS DEPENDING ON count is read before the
	// extent it decides, so the table's own emission finds it here.
	valueOf map[uint64]string

	// receiver is the identifier every method's receiver is declared under.
	receiver string

	// zeroFill is the width of the widest slack node in the descriptor, or zero
	// where it carries none. It sizes the one shared run of zero bytes a writer
	// emits for slack a record does not carry.
	zeroFill uint32

	// resized and fresh are whether the helper of that name is reached.
	resized, fresh bool

	// counter is what makes a generated temporary unique inside one method.
	counter int

	// subs is, for the record being decoded, every sub-reader the walk rewinds
	// onto an occurrence, in the order the walk reaches them. Each is declared
	// and built once at the top of the method rather than once per occurrence;
	// see [coder.subReaders].
	subs []subReader

	// record is the COBOL name of the record being walked, for a diagnostic
	// composed away from the walk.
	record string
}

// subReader is one decoder a record's decode method builds over the bytes of
// one occurrence: the identifier it is declared under, and the table whose
// occurrences it reads.
type subReader struct {
	// name is the Go identifier, unique inside the method that declares it.
	name string

	// item is the repeating group one occurrence of which it reads, as the
	// copybook names it, for the diagnostic its construction makes.
	item string
}

// countUse is one repeating item naming a count field: where its occurrences
// live in Go, what the copybook calls it, and the bounds its own repetition
// declares.
type countUse struct {
	// item is the repeating item, as the copybook names it.
	item string

	// expr is the Go expression holding its occurrences, which is a slice
	// because an OCCURS DEPENDING ON is one.
	expr string

	// loops is what has to be walked to reach expr, outermost first: a table
	// inside a group repeating on the same count contributes one number per
	// occurrence of that group, and every one of them binds the count.
	loops []countLoop

	// minimum and maximum are the repetition's own declared bounds. Each
	// repetition naming a count carries its own, and every one of them binds
	// the one value.
	minimum, maximum uint32
}

// countLoop is one `for` a count's occurrences sit inside.
type countLoop struct {
	// variable is the loop's own index.
	variable string

	// over is the Go expression it ranges, and item is what the copybook calls
	// that.
	over, item string
}

// scope is where in a record the walk currently is: what it reads and writes
// through, what it is inside, and how to say so in a diagnostic.
type scope struct {
	// record is the record node's COBOL name, which opens every message.
	record string

	// group is the innermost named group the walk is inside, as the copybook
	// names it. It is what a refusal about an item with no name of its own has
	// to point at, and it is carried here so that every part of this generator
	// points at the same one.
	group string

	// rw is the *codec.Reader or *codec.Writer in scope. It is the method's own
	// argument outside a buffered occurrence and a sub-reader or sub-writer
	// over that occurrence's bytes inside one.
	rw string

	// buf is the occurrence's bytes, where the walk is inside one: the slice a
	// decoder evaluates an arm's predicate against, or the bytes.Buffer an
	// encoder is filling. It is empty outside one.
	buf string

	// occurrence is the group one occurrence of which buf holds, and occExpr is
	// the Go expression of that occurrence.
	occurrence uint64
	occExpr    string

	// depth is the nesting of loops and buffers, which is what makes a
	// generated temporary unique.
	depth int

	// suffix is what a diagnostic adds to say which occurrence of what it is
	// about, and args are the loop variables it formats.
	suffix string
	args   []string

	// dir is which of the record's two methods is being emitted, which is what
	// says where a table's number of occurrences comes from.
	dir direction
}

// in is s one occurrence of item deep, indexed by variable.
func (s scope) in(item, variable string) scope {
	// Innermost first, because that is the order the sentence reads in: an
	// occurrence of BLOCK-ITEM in an occurrence of BLOCK, rather than the walk's
	// own order down the tree.
	s.suffix = fmt.Sprintf(" in occurrence %%d of %s", item) + s.suffix
	s.args = append([]string{variable}, s.args...)
	s.depth++

	return s
}

// codecMethods is the source of [codecFile] for this descriptor, or the empty
// string where the descriptor carries no record node.
//
// One pair of methods per record: UnmarshalCOBOL and MarshalCOBOL, which are
// codec's own Unmarshaler and Marshaler. The interfaces are the reason those
// are the names — a generated record satisfies them, so codec.Unmarshal and
// codec.Marshal take one, and #52's file-level reader and writer have a shape
// to call rather than one to invent.
//
// Neither method chooses an Encoding. The five axes are properties of the file
// in hand and codec carries them on the Reader and the Writer, so the caller
// states them once and every record read through that Reader is read under
// them. What this descriptor resolved is [Encoding], emitted beside the methods
// so that the statement a caller makes is the descriptor's rather than one they
// retyped.
func codecMethods(d *irpb.Descriptor, opts options) (string, error) {
	e, err := newEmitter(d)
	if err != nil {
		return "", err
	}

	c := &coder{emitter: e, receiver: opts.receiverName()}

	var (
		decls []string
		names []string
	)

	for _, node := range d.GetNodes() {
		record := node.GetRecord()
		if record == nil {
			continue
		}

		name, err := recordName(record.GetNames())
		if err != nil {
			return "", err
		}

		// The one identifier this generator declares at package scope that a
		// copybook could also produce. Reported like any other collision rather
		// than worked around, so that an adopter is told to rename the record
		// instead of handed a package that does not compile.
		if name == encodingFunc {
			return "", &collisionError{
				Go:    name,
				Cobol: []colliding{namedBy(record.GetNames()), {Original: "the file's resolved encoding profile"}},
				Where: "the generated package",
			}
		}

		decode, err := c.unmarshal(name, record)
		if err != nil {
			return "", err
		}

		encode, err := c.marshal(name, record)
		if err != nil {
			return "", err
		}

		names = append(names, name)
		decls = append(decls, decode, encode)
	}

	if len(names) == 0 {
		return "", nil
	}

	profile, err := c.profile(d)
	if err != nil {
		return "", err
	}

	var head []string

	if profile != "" {
		head = append(head, profile)
	}

	head = append(head, assertions(names))

	if c.zeroFill > 0 {
		head = append(head, zeroFillDeclaration(c.zeroFill))
	}

	if c.resized {
		head = append(head, resizedDeclaration())
	}

	if c.fresh {
		head = append(head, freshDeclaration())
	}

	c.imports[codecImport] = struct{}{}
	c.imports["fmt"] = struct{}{}

	var b strings.Builder

	b.WriteString(generatedBy)
	b.WriteString("\n\npackage ")
	b.WriteString(opts.packageName)
	b.WriteString("\n\nimport (\n")

	for _, path := range c.sortedImports() {
		fmt.Fprintf(&b, "%q\n", path)
	}

	b.WriteString(")\n")

	for _, decl := range append(head, decls...) {
		b.WriteString("\n")
		b.WriteString(decl)
		b.WriteString("\n")
	}

	return b.String(), nil
}

// unmarshal is one record's decode method.
func (c *coder) unmarshal(name string, record *irpb.Record) (string, error) {
	c.valueOf = make(map[uint64]string)
	c.counter = 0
	c.subs = nil
	c.record = record.GetNames().GetOriginal()

	s := scope{record: c.record, rw: "r", dir: decoding}

	var body strings.Builder

	if err := c.decodeGroup(&body, record.GetRootId(), c.receiver, s); err != nil {
		return "", err
	}

	doc := commentLines(fmt.Sprintf(`UnmarshalCOBOL reads one %s out of r, in the order docs/ir/SPEC.md
resolved its items, and retains the bytes of every slack node it carries and
of every item the copybook gives no data-name.

It is codec's Unmarshaler. The Encoding is r's: the five axes are properties
of the file in hand, and %s is what this descriptor resolved.`,
		record.GetNames().GetOriginal(), encodingFunc))

	return doc + fmt.Sprintf("func (%s *%s) UnmarshalCOBOL(r *codec.Reader) error {\n%s\nreturn nil\n}",
		c.receiver, name, c.prologue(c.subReaders()+body.String())), nil
}

// subReaders declares and builds every sub-reader the record being decoded
// rewinds onto an occurrence, ahead of the walk that rewinds them.
//
// One per buffered table rather than one per occurrence of one, which is the
// whole of what this saves: codec.Reader.Reset keeps everything the Encoding
// derives — the zoned decimal byte tables, the alphanumeric translation table
// and both scratch buffers — and swaps only the bytes, so a table of a hundred
// occurrences carrying a variant costs one construction rather than a hundred,
// and none of the allocations that stood beside them.
//
// It is the decode direction only. An encoder's sub-writer fills a buffer of
// its own per occurrence and hands those bytes back to the writer above it, so
// there is nothing there for one built once to be rewound onto.
//
// # Built for the record rather than at the first occurrence that needs one
//
// A record therefore pays one construction for a table whose loop runs no
// times — an OCCURS DEPENDING ON that decoded to zero — and one for a
// variant-carrying table inside an arm the record did not take. The lazy
// alternative, a nil check at each Reset site, removes both and was declined:
// what it charges instead is five lines of generated Go at every buffered
// table, in a package whose output is what an adopter reads, and what it saves
// is bounded by the record's *shape* — the handful of tables it declares —
// rather than by its *data*, which is the unbounded per-occurrence tax this
// whole method exists to remove. If a descriptor ever makes the shape half of
// that cost matter, the nil check is the change and this paragraph is the
// reason it was not made first.
func (c *coder) subReaders() string {
	var b strings.Builder

	for _, sub := range c.subs {
		line(&b, "// %s reads one occurrence of %s, which carries a variant and so is read", sub.name, sub.item)
		line(&b, "// whole before it is walked. It is built here rather than inside the loop")
		line(&b, "// over those occurrences, and rewound onto each occurrence's bytes there.")
		line(&b, "var %s *codec.Reader", sub.name)
		line(&b, "if %s, err = codec.NewBytesReader(nil, r.Encoding()); err != nil {", sub.name)
		line(&b, "return fmt.Errorf(%s, err)", strconv.Quote(c.record+": building the decoder the occurrences of "+sub.item+" are read through: %w"))
		line(&b, "}")
		line(&b, "")
	}

	return b.String()
}

// marshal is one record's encode method.
func (c *coder) marshal(name string, record *irpb.Record) (string, error) {
	c.countOf = make(map[uint64][]countUse)
	c.counter = 0
	c.record = record.GetNames().GetOriginal()

	if err := c.collectCounts(record.GetRootId(), c.receiver, nil); err != nil {
		return "", err
	}

	s := scope{record: c.record, rw: "w", dir: encoding}

	var body strings.Builder

	if err := c.encodeGroup(&body, record.GetRootId(), c.receiver, s); err != nil {
		return "", err
	}

	doc := commentLines(fmt.Sprintf(`MarshalCOBOL writes this %s into w, in the order docs/ir/SPEC.md
resolved its items, emitting the bytes retained for every slack node and
every unnamed item it carries, and zero bytes for one it does not.

It is codec's Marshaler. Two values are the descriptor's rather than the
caller's and are supplied rather than taken from the record: an OCCURS
DEPENDING ON count is emitted as the number of occurrences written, and
slack — and the bytes of an item the copybook gives no data-name — is
emitted as what was retained for it. Everything else is the caller's,
including the value a discriminator tests — a writer evaluates a predicate
and never inverts one.`, record.GetNames().GetOriginal()))

	return doc + fmt.Sprintf("func (%s *%s) MarshalCOBOL(w *codec.Writer) error {\n%s\nreturn nil\n}",
		c.receiver, name, c.prologue(body.String())), nil
}

// prologue is a method body with the one declaration it needs, where it needs
// one. A record of nothing but a group of no members reads and writes no bytes,
// and an `err` nothing assigns does not compile.
func (c *coder) prologue(body string) string {
	if !strings.Contains(body, "err =") {
		return body
	}

	return "var err error\n\n" + body
}

// decodeGroup emits the statements that read one occurrence of the group id
// into the struct expr.
func (c *coder) decodeGroup(b *strings.Builder, id uint64, expr string, s scope) error {
	group, err := c.group(id)
	if err != nil {
		return err
	}

	s.group = group.GetNames().GetOriginal()

	members, err := c.flattened(id)
	if err != nil {
		return err
	}

	runs, fillers := 0, 0

	for at, memberID := range members {
		member, ok := c.nodes[memberID]
		if !ok {
			return unresolved(memberID)
		}

		// One item to a paragraph, which is what the record itself reads like.
		if at > 0 {
			b.WriteString("\n")
		}

		// An item the copybook gives no data-name is read into the run
		// retained for it, the way a slack node is: it has no field, because it
		// has no name for one. See [emitter.flattened].
		if field := member.GetField(); field != nil && anonymous(field.GetNames()) {
			width, err := fillerRun(field, s.group)
			if err != nil {
				return err
			}

			c.retain(b, width)

			line(b, "if %s.%s[%d], err = %s.ReadBytes(%d); err != nil {", expr, fillerField, fillers, s.rw, width)
			line(b, "%s", wrapf(s, "reading the "+plural(int(width), "byte")+" of an item the copybook gives no data-name"))
			line(b, "}")

			fillers++

			continue
		}

		switch kind := member.GetKind().(type) {
		case *irpb.Node_Slack:
			c.retain(b, kind.Slack.GetWidth())

			line(b, "if %s.%s[%d], err = %s.ReadBytes(%d); err != nil {", expr, slackField, runs, s.rw, kind.Slack.GetWidth())
			line(b, "%s", wrapf(s, "reading the "+plural(int(kind.Slack.GetWidth()), "byte")+" no item of it covers"))
			line(b, "}")

			runs++
		case *irpb.Node_Field:
			name, err := identifier("field", kind.Field.GetNames())
			if err != nil {
				return err
			}

			target := expr + "." + name

			if kind.Field.GetRepetition() == nil {
				c.valueOf[memberID] = target
			}

			if err := c.decodeRepeated(b, kind.Field.GetRepetition(), target, kind.Field.GetNames().GetOriginal(), s,
				func(elem string, s scope) error {
					return c.decodeField(b, kind.Field, elem, s)
				}); err != nil {
				return err
			}
		case *irpb.Node_Group:
			name, err := identifier("group", kind.Group.GetNames())
			if err != nil {
				return err
			}

			if err := c.decodeRepeated(b, kind.Group.GetRepetition(), expr+"."+name, kind.Group.GetNames().GetOriginal(), s,
				func(elem string, s scope) error {
					return c.decodeMembers(b, memberID, elem, s)
				}); err != nil {
				return err
			}
		case *irpb.Node_Variant:
			if err := c.decodeVariant(b, kind.Variant, expr, s); err != nil {
				return err
			}
		default:
			return malformed(fmt.Sprintf("node %d is not something a group may contain", memberID),
				"a member list names a group, variant, field or slack node; see docs/ir/SPEC.md, \"The node kinds\"")
		}
	}

	return nil
}

// decodeMembers is [coder.decodeGroup] with the buffering one occurrence of a
// table holding a variant needs.
//
// An arm's predicate reads a field of the occurrence being chosen for, and that
// field may sit behind the variant (docs/ir/SPEC.md, "A predicate on an arm
// reads one occurrence"). So the occurrence's bytes are read whole before any
// of it is decoded, the members are decoded out of those bytes, and an arm is
// selected by comparing the target's bytes where they stand in them. Every arm
// of a variant covers the same bytes, so the occurrence has one width whichever
// arms it holds and that read is the read a table without a variant would have
// made in pieces.
func (c *coder) decodeMembers(b *strings.Builder, id uint64, expr string, s scope) error {
	if !c.buffers(id) {
		return c.decodeGroup(b, id, expr, s)
	}

	width, err := c.sumWidth(id, expr, s.dir)
	if err != nil {
		return err
	}

	group, err := c.group(id)
	if err != nil {
		return err
	}

	buf := fmt.Sprintf("occurrence%d", s.depth)

	// The sub-reader is declared and built once for the record by
	// [coder.subReaders], and rewound onto each occurrence here. That
	// declaration stands at the top of the method rather than inside this
	// loop's block, which is what its name now has to be unique against: a
	// suffix telling this table apart from the ones it is nested inside was
	// enough while each declaration had a block of its own, and two tables
	// standing side by side are at one depth and would now be one identifier
	// declared twice.
	c.counter++

	sub := fmt.Sprintf("entry%d", c.counter)

	c.subs = append(c.subs, subReader{name: sub, item: group.GetNames().GetOriginal()})

	// Assigned rather than declared, so that the one `err` the method declares
	// is the one every statement of it uses: a `:=` here would shadow it, and a
	// record whose every read sits inside such a loop would then declare an
	// `err` nothing assigns.
	line(b, "var %s []byte", buf)
	line(b, "if %s, err = %s.ReadBytes(%s); err != nil {", buf, s.rw, width)
	line(b, "%s", wrapf(s, "reading its bytes"))
	line(b, "}")
	line(b, "%s.Reset(%s)", sub, buf)

	inner := s
	inner.rw = sub
	inner.buf = buf
	inner.occurrence = id
	inner.occExpr = expr

	return c.decodeGroup(b, id, expr, inner)
}

// decodeRepeated emits body once for a member that does not repeat, and inside
// the loop over its occurrences for one that does.
func (c *coder) decodeRepeated(b *strings.Builder, rep *irpb.Repetition, expr, item string, s scope, body func(string, scope) error) error {
	if rep == nil {
		return body(expr, s)
	}

	index := fmt.Sprintf("i%d", s.depth)

	switch count := rep.GetCount().(type) {
	case *irpb.Repetition_Constant:
		line(b, "for %s := range %s {", index, expr)
	case *irpb.Repetition_Variable:
		switch count.Variable.GetCount().(type) {
		case *irpb.VariableCount_FieldId:
			held, err := c.countValue(count.Variable, item)
			if err != nil {
				return err
			}

			c.resized = true

			c.counter++

			n := fmt.Sprintf("n%d", c.counter)

			line(b, "%s := int(%s)", n, held)
			line(b, "if %s < %d || %s > %d {", n, count.Variable.GetMinOccurrences(), n, count.Variable.GetMaxOccurrences())
			line(b, "%s", failf(s, fmt.Sprintf("%s occurs %d to %d times depending on %s, and the record's count is %%d",
				item, count.Variable.GetMinOccurrences(), count.Variable.GetMaxOccurrences(), c.countName(count.Variable)), n))
			line(b, "}")
			line(b, "%s = %s(%s, %s)", expr, resizedHelper, expr, n)
			line(b, "for %s := range %s {", index, expr)
		case *irpb.VariableCount_RegisterId:
			// A register holds what a transition bound out of a record already
			// read, and this method has no register file — the file-level
			// reader is where the automaton lives (#52). So the occurrences it
			// reads are the ones the record it was handed already carries,
			// which is what lets that reader size the table from the register
			// and then hand the record over.
			line(b, "for %s := range %s {", index, expr)
		default:
			return malformed("an OCCURS DEPENDING ON says nothing about where its count comes from",
				"a variable count names a field of the record being read or a register an earlier transition bound")
		}
	default:
		return malformed("an item repeats and says nothing about how many times",
			"a repetition carries a constant count or an OCCURS DEPENDING ON one; an item that does not repeat carries no repetition at all")
	}

	if err := body(expr+"["+index+"]", s.in(item, index)); err != nil {
		return err
	}

	line(b, "}")

	return nil
}

// decodeField emits the one accessor call an elementary item takes.
func (c *coder) decodeField(b *strings.Builder, f *irpb.Field, target string, s scope) error {
	call, err := c.readCall(f, s.rw)
	if err != nil {
		return err
	}

	line(b, "if %s, err = %s; err != nil {", target, call)
	line(b, "%s", wrapf(s, "reading "+f.GetNames().GetOriginal()))
	line(b, "}")

	return nil
}

// decodeVariant selects one arm of a variant and decodes it, leaving every
// other arm of the occurrence nil.
func (c *coder) decodeVariant(b *strings.Builder, v *irpb.Variant, expr string, s scope) error {
	if s.buf == "" {
		return malformed("a variant sits outside a group that repeats",
			"a variant MUST be contained, at any depth, in a group that repeats; see docs/ir/SPEC.md, \"A variant is chosen once per occurrence\"")
	}

	arms, err := c.arms(v, expr, s)
	if err != nil {
		return err
	}

	c.fresh = true

	line(b, "switch {")

	for i, arm := range arms {
		line(b, "case %s:", arm.match)
		line(b, "%s = %s(%s)", arm.expr, freshHelper, arm.expr)

		for j, other := range arms {
			if j != i {
				line(b, "%s = nil", other.expr)
			}
		}

		if arm.group != 0 {
			if err := c.decodeGroup(b, arm.group, arm.expr, s); err != nil {
				return err
			}
		} else if err := c.decodeField(b, arm.field, "*"+arm.expr, s); err != nil {
			return err
		}
	}

	line(b, "default:")
	line(b, "%s", failf(s, fmt.Sprintf(
		"the record type is one the layout describes and no arm of the alternation over %s matches the entry", arms[0].item)))
	line(b, "}")

	return nil
}

// encodeGroup emits the statements that write one occurrence of the group id
// out of the struct expr.
func (c *coder) encodeGroup(b *strings.Builder, id uint64, expr string, s scope) error {
	group, err := c.group(id)
	if err != nil {
		return err
	}

	s.group = group.GetNames().GetOriginal()

	members, err := c.flattened(id)
	if err != nil {
		return err
	}

	runs, fillers := 0, 0

	for at, memberID := range members {
		member, ok := c.nodes[memberID]
		if !ok {
			return unresolved(memberID)
		}

		// One item to a paragraph, which is what the record itself reads like.
		if at > 0 {
			b.WriteString("\n")
		}

		// An item the copybook gives no data-name is written back out of the
		// run retained for it, on the same three terms a slack node is: what
		// was read, zero bytes where the record was never read, and a report
		// rather than a truncation where the run is the wrong length.
		if field := member.GetField(); field != nil && anonymous(field.GetNames()) {
			width, err := fillerRun(field, s.group)
			if err != nil {
				return err
			}

			c.retain(b, width)

			run := fmt.Sprintf("%s.%s[%d]", expr, fillerField, fillers)

			line(b, "switch {")
			line(b, "case %s == nil:", run)
			line(b, "if err = %s.WriteBytes(%s[:%d]); err != nil {", s.rw, zeroFillHelper, width)
			line(b, "%s", wrapf(s, "writing "+plural(int(width), "zero byte")+" for an item the copybook gives no data-name and this record carries none for"))
			line(b, "}")
			line(b, "case len(%s) != %d:", run, width)
			line(b, "%s", failf(s, fmt.Sprintf(
				"a writer reports a retained run rather than truncating or padding it, and the run for an item of %s the copybook gives no data-name is %%d", plural(int(width), "byte")),
				"len("+run+")"))
			line(b, "default:")
			line(b, "if err = %s.WriteBytes(%s); err != nil {", s.rw, run)
			line(b, "%s", wrapf(s, "writing the "+plural(int(width), "byte")+" of an item the copybook gives no data-name"))
			line(b, "}")
			line(b, "}")

			fillers++

			continue
		}

		switch kind := member.GetKind().(type) {
		case *irpb.Node_Slack:
			c.retain(b, kind.Slack.GetWidth())

			run := fmt.Sprintf("%s.%s[%d]", expr, slackField, runs)
			width := kind.Slack.GetWidth()

			line(b, "switch {")
			line(b, "case %s == nil:", run)
			line(b, "if err = %s.WriteBytes(%s[:%d]); err != nil {", s.rw, zeroFillHelper, width)
			line(b, "%s", wrapf(s, "writing "+plural(int(width), "zero byte")+" for slack this record carries none for"))
			line(b, "}")
			line(b, "case len(%s) != %d:", run, width)
			line(b, "%s", failf(s, fmt.Sprintf(
				"a writer reports a retained run rather than truncating or padding it, and the run for a slack node of %s is %%d", plural(int(width), "byte")),
				"len("+run+")"))
			line(b, "default:")
			line(b, "if err = %s.WriteBytes(%s); err != nil {", s.rw, run)
			line(b, "%s", wrapf(s, "writing the "+plural(int(width), "byte")+" no item of it covers"))
			line(b, "}")
			line(b, "}")

			runs++
		case *irpb.Node_Field:
			name, err := identifier("field", kind.Field.GetNames())
			if err != nil {
				return err
			}

			target := expr + "." + name

			if uses, determined := c.countOf[memberID]; determined {
				if err := c.encodeCount(b, kind.Field, target, uses, s); err != nil {
					return err
				}

				continue
			}

			if err := c.encodeRepeated(b, kind.Field.GetRepetition(), target, kind.Field.GetNames().GetOriginal(), s,
				func(elem string, s scope) error {
					return c.encodeField(b, kind.Field, elem, s)
				}); err != nil {
				return err
			}
		case *irpb.Node_Group:
			name, err := identifier("group", kind.Group.GetNames())
			if err != nil {
				return err
			}

			if err := c.encodeRepeated(b, kind.Group.GetRepetition(), expr+"."+name, kind.Group.GetNames().GetOriginal(), s,
				func(elem string, s scope) error {
					return c.encodeMembers(b, memberID, elem, s)
				}); err != nil {
				return err
			}
		case *irpb.Node_Variant:
			if err := c.encodeVariant(b, kind.Variant, expr, s); err != nil {
				return err
			}
		default:
			return malformed(fmt.Sprintf("node %d is not something a group may contain", memberID),
				"a member list names a group, variant, field or slack node; see docs/ir/SPEC.md, \"The node kinds\"")
		}
	}

	return nil
}

// encodeMembers is [coder.encodeGroup] with the buffering an occurrence holding
// a variant needs, for the reason [coder.decodeMembers] needs it: an arm's
// predicate is evaluated against the occurrence's bytes, and the target may sit
// behind the variant.
func (c *coder) encodeMembers(b *strings.Builder, id uint64, expr string, s scope) error {
	if !c.buffers(id) {
		return c.encodeGroup(b, id, expr, s)
	}

	c.imports["bytes"] = struct{}{}

	var (
		buf = fmt.Sprintf("occurrence%d", s.depth)
		sub = fmt.Sprintf("entry%d", s.depth)
	)

	line(b, "var %s bytes.Buffer", buf)
	line(b, "var %s *codec.Writer", sub)
	line(b, "if %s, err = codec.NewWriter(&%s, %s.Encoding()); err != nil {", sub, buf, s.rw)
	line(b, "%s", wrapf(s, "writing over its bytes"))
	line(b, "}")

	inner := s
	inner.rw = sub
	inner.buf = buf
	inner.occurrence = id
	inner.occExpr = expr

	if err := c.encodeGroup(b, id, expr, inner); err != nil {
		return err
	}

	if err := c.checkArms(b, id, expr, inner); err != nil {
		return err
	}

	line(b, "if err = %s.WriteBytes(%s.Bytes()); err != nil {", s.rw, buf)
	line(b, "%s", wrapf(s, "writing its bytes"))
	line(b, "}")

	return nil
}

// encodeRepeated emits body once for a member that does not repeat, and inside
// the loop over its occurrences for one that does.
//
// A variable count is not read here and no slice is sized: the number of
// occurrences is the caller's, and what the descriptor determines is the count
// field, which [coder.encodeCount] writes where the record carries it.
func (c *coder) encodeRepeated(b *strings.Builder, rep *irpb.Repetition, expr, item string, s scope, body func(string, scope) error) error {
	if rep == nil {
		return body(expr, s)
	}

	index := fmt.Sprintf("i%d", s.depth)

	switch count := rep.GetCount().(type) {
	case *irpb.Repetition_Constant:
	case *irpb.Repetition_Variable:
		if _, register := count.Variable.GetCount().(*irpb.VariableCount_RegisterId); register {
			// Nothing determines a register from occurrences: it holds what a
			// transition bound out of a record already emitted, so the
			// occurrences are what has to agree with it, and the comparison is
			// the automaton's (#52). The bounds are this method's all the same.
			line(b, "if n := len(%s); n < %d || n > %d {", expr, count.Variable.GetMinOccurrences(), count.Variable.GetMaxOccurrences())
			line(b, "%s", failf(s, fmt.Sprintf("%s occurs %d to %d times and the record holds %%d occurrences of it",
				item, count.Variable.GetMinOccurrences(), count.Variable.GetMaxOccurrences()), "n"))
			line(b, "}")
		}
	default:
		return malformed("an item repeats and says nothing about how many times",
			"a repetition carries a constant count or an OCCURS DEPENDING ON one; an item that does not repeat carries no repetition at all")
	}

	line(b, "for %s := range %s {", index, expr)

	if err := body(expr+"["+index+"]", s.in(item, index)); err != nil {
		return err
	}

	line(b, "}")

	return nil
}

// encodeField emits the one accessor call an elementary item takes.
func (c *coder) encodeField(b *strings.Builder, f *irpb.Field, value string, s scope) error {
	opaque, err := opaqueDisplay(f)
	if err != nil {
		return err
	}

	if opaque {
		return c.encodeOpaque(b, f, value, s)
	}

	if raw, err := c.rawWidth(f); err != nil {
		return err
	} else if raw {
		line(b, "if len(%s) != %d {", value, f.GetWidth())
		line(b, "%s", failf(s, fmt.Sprintf("%s is %d bytes and the record holds %%d for it",
			f.GetNames().GetOriginal(), f.GetWidth()), "len("+value+")"))
		line(b, "}")
	}

	call, err := c.writeCall(f, value, s.rw)
	if err != nil {
		return err
	}

	line(b, "if err = %s; err != nil {", call)
	line(b, "%s", wrapf(s, "writing "+f.GetNames().GetOriginal()))
	line(b, "}")

	return nil
}

// encodeOpaque emits the statements that write an item no charset governs.
//
// Three terms, and they are the three the run retained for an item the copybook
// gives no data-name already takes, for the same reason: the field is a []byte,
// and a []byte is whatever the caller left in it. What made that unnecessary
// for every other DISPLAY item was codec's WriteAlphanumeric, which takes the
// declared width and pads a short value out to it. WriteBytes takes no width
// and pads nothing, so the width this item occupies in the record is enforced
// by the statements below or by nothing at all.
//
//   - nil writes zero bytes, out of the run [zeroFillDeclaration] holds. A
//     record a caller built rather than read has to be writable, and there is
//     no pad byte to reach for instead: the space that pads a text item is a
//     value this item may legitimately hold, which is the whole reason it does
//     not go through the alphanumeric writer. Zero is the choice, and it is
//     written here rather than left to be read out of the emitted code.
//   - a run of the wrong length is reported. Truncating it would drop bytes
//     the caller supplied and padding it would add bytes they did not, and
//     either one moves every item behind it in the record.
//   - anything else is written as it stands, which is [coder.writeCall]'s
//     WriteBytes.
func (c *coder) encodeOpaque(b *strings.Builder, f *irpb.Field, value string, s scope) error {
	width := f.GetWidth()
	item := f.GetNames().GetOriginal()

	// The same call [zeroFillDeclaration] is sized by, so that the run this
	// emits a slice of is declared wide enough for it. Reused rather than
	// counted a second way: a second mechanism for the same width is a second
	// thing to keep in step with the declaration, and it would go out of step
	// silently — the generated package would compile and slice out of range.
	c.retain(b, width)

	call, err := c.writeCall(f, value, s.rw)
	if err != nil {
		return err
	}

	line(b, "switch {")
	line(b, "case %s == nil:", value)
	line(b, "if err = %s.WriteBytes(%s[:%d]); err != nil {", s.rw, zeroFillHelper, width)
	line(b, "%s", wrapf(s, "writing "+plural(int(width), "zero byte")+" for "+item+", which this record carries no bytes for"))
	line(b, "}")
	line(b, "case len(%s) != %d:", value, width)
	line(b, "%s", failf(s, fmt.Sprintf(
		"a writer reports the bytes of %s rather than truncating or padding them, and %s is %s and the record holds %%d for it",
		item, item, plural(int(width), "byte")),
		"len("+value+")"))
	line(b, "default:")
	line(b, "if err = %s; err != nil {", call)
	line(b, "%s", wrapf(s, "writing "+item))
	line(b, "}")
	line(b, "}")

	return nil
}

// encodeCount writes an OCCURS DEPENDING ON count field.
//
// docs/ir/SPEC.md's "What the descriptor determines, a writer supplies" makes
// this one of the two values a writer supplies rather than takes: the field's
// value *is* the number of occurrences, so what the caller left in the field is
// not emitted. More than one repetition may name it, and then it is determined
// more than once — where the numbers agree there is one value, and where they
// disagree there is none and a writer reports rather than picking, because
// either number sizes the other table wrong and slides every item behind it.
func (c *coder) encodeCount(b *strings.Builder, f *irpb.Field, target string, uses []countUse, s scope) error {
	if f.GetRepetition() != nil {
		return malformed(fmt.Sprintf("%s is the count of an OCCURS DEPENDING ON and repeats", f.GetNames().GetOriginal()),
			"a count names a field, not an occurrence of one; see docs/ir/SPEC.md, \"A reference names a field, not an occurrence of one\"")
	}

	c.counter++

	held := fmt.Sprintf("count%d", c.counter)
	from := fmt.Sprintf("from%d", c.counter)
	name := f.GetNames().GetOriginal()

	shared := len(uses) > 1 || len(uses[0].loops) > 0

	if !shared {
		line(b, "%s := len(%s)", held, uses[0].expr)
		c.bounds(b, held, name, uses[0], s)
	} else {
		line(b, "%s, %s := -1, \"\"", held, from)

		for _, use := range uses {
			for _, loop := range use.loops {
				line(b, "for %s := range %s {", loop.variable, loop.over)
			}

			at := s
			for _, loop := range use.loops {
				at = at.in(loop.item, loop.variable)
			}

			line(b, "{")
			line(b, "n := len(%s)", use.expr)

			c.bounds(b, "n", name, use, at)

			line(b, "if %s < 0 {", held)
			line(b, "%s, %s = n, %q", held, from, use.item)
			line(b, "} else if n != %s {", held)
			line(b, "%s", failf(at, fmt.Sprintf(
				"a writer reports rather than choosing between two numbers of occurrences, and %s is the count of more than one item of this record: the caller supplied %%d occurrences of %%s and %%d of %s",
				name, use.item), held, from, "n"))
			line(b, "}")
			line(b, "}")

			for range use.loops {
				line(b, "}")
			}
		}

		line(b, "if %s < 0 {", held)
		line(b, "%s = 0", held)
		line(b, "}")
	}

	value, err := c.countLiteral(f, held)
	if err != nil {
		return err
	}

	call, err := c.writeCall(f, value, s.rw)
	if err != nil {
		return err
	}

	line(b, "if err = %s; err != nil {", call)
	line(b, "%s", wrapf(s, "writing "+name))
	line(b, "}")

	return nil
}

// bounds emits the check every repetition naming a count makes against it.
//
// Per repetition rather than once, because each carries its own minimum and
// maximum and both bind the one value: the range a record can actually carry is
// the overlap of the declared ones rather than either.
func (c *coder) bounds(b *strings.Builder, held, count string, use countUse, s scope) {
	if use.minimum == 0 {
		line(b, "if %s > %d {", held, use.maximum)
	} else {
		line(b, "if %s < %d || %s > %d {", held, use.minimum, held, use.maximum)
	}

	line(b, "%s", failf(s, fmt.Sprintf("%s occurs %d to %d times depending on %s, and the record holds %%d occurrences of it",
		use.item, use.minimum, use.maximum, count), held))
	line(b, "}")
}

// encodeVariant writes the arm the caller supplied, and refuses an occurrence
// holding any number of arms other than one.
func (c *coder) encodeVariant(b *strings.Builder, v *irpb.Variant, expr string, s scope) error {
	if s.buf == "" {
		return malformed("a variant sits outside a group that repeats",
			"a variant MUST be contained, at any depth, in a group that repeats; see docs/ir/SPEC.md, \"A variant is chosen once per occurrence\"")
	}

	arms, err := c.arms(v, expr, s)
	if err != nil {
		return err
	}

	line(b, "switch {")

	for _, arm := range arms {
		line(b, "case %s != nil:", arm.expr)

		if arm.group != 0 {
			if err := c.encodeGroup(b, arm.group, arm.expr, s); err != nil {
				return err
			}
		} else if err := c.encodeField(b, arm.field, "*"+arm.expr, s); err != nil {
			return err
		}
	}

	line(b, "default:")
	line(b, "%s", failf(s, fmt.Sprintf(
		"an occurrence holds exactly one arm of the alternation over %s and this one holds none", arms[0].item)))
	line(b, "}")

	return nil
}

// checkArms is the predicate evaluation a writer owes an occurrence holding a
// variant, run once the occurrence's bytes exist.
//
// docs/ir/SPEC.md's "A writer evaluates a predicate, it never inverts one"
// requires exactly this and forbids the shape most people expect: the value an
// arm's predicate tests is the caller's, so it is checked and reported rather
// than derived and stored. It runs behind the whole occurrence because a
// target may sit behind the variant.
func (c *coder) checkArms(b *strings.Builder, id uint64, expr string, s scope) error {
	in, err := c.groupName(id)
	if err != nil {
		return err
	}

	s.group = in

	members, err := c.flattened(id)
	if err != nil {
		return err
	}

	for _, memberID := range members {
		member, ok := c.nodes[memberID]
		if !ok {
			return unresolved(memberID)
		}

		variant := member.GetVariant()
		if variant == nil {
			continue
		}

		bytesOf := s
		bytesOf.buf = s.buf + ".Bytes()"

		arms, err := c.arms(variant, expr, bytesOf)
		if err != nil {
			return err
		}

		c.counter++

		var (
			matched = fmt.Sprintf("matched%d", c.counter)
			holds   = fmt.Sprintf("holds%d", c.counter)
		)

		line(b, "%s, %s := -1, -1", matched, holds)
		line(b, "switch {")

		for i, arm := range arms {
			line(b, "case %s:", arm.match)
			line(b, "%s = %d", matched, i)
		}

		line(b, "}")
		line(b, "switch {")

		for i, arm := range arms {
			line(b, "case %s != nil:", arm.expr)
			line(b, "%s = %d", holds, i)
		}

		line(b, "}")
		line(b, "if %s < 0 {", matched)
		line(b, "%s", failf(s, fmt.Sprintf(
			"a writer reports an occurrence satisfying no arm's predicate rather than emitting it, and none of the alternation over %s is satisfied", arms[0].item)))
		line(b, "}")
		line(b, "if %s != %s {", matched, holds)
		line(b, "%s", failf(s, fmt.Sprintf(
			"a writer evaluates the predicate of the arm its caller supplied and never derives a value satisfying one, and this occurrence holds arm %%d of the alternation over %s while its values satisfy the predicate of arm %%d",
			arms[0].item), holds, matched))
		line(b, "}")
	}

	return nil
}

// arm is one alternative of a variant, as the generated code reaches it.
type arm struct {
	// expr is the Go expression of the pointer field records.go declared.
	expr string

	// item is what the copybook calls the arm's body.
	item string

	// match is the Go expression that is true when the occurrence's bytes
	// satisfy the arm's predicate.
	match string

	// group is the identifier of the arm's body where it is a group, and field
	// is the body where it is an elementary item. Exactly one is set.
	group uint64
	field *irpb.Field
}

// arms is a variant's arms, resolved against the occurrence in scope.
func (c *coder) arms(v *irpb.Variant, expr string, s scope) ([]arm, error) {
	if len(v.GetArms()) < 2 {
		return nil, malformed(fmt.Sprintf("a variant carries %d arms", len(v.GetArms())),
			"a producer must not emit a variant carrying fewer than two arms; see docs/ir/SPEC.md, \"A variant is chosen once per occurrence\"")
	}

	out := make([]arm, 0, len(v.GetArms()))

	for _, a := range v.GetArms() {
		body, err := c.armBody(a)
		if err != nil {
			return nil, err
		}

		if err := namedArm(body, s.group); err != nil {
			return nil, err
		}

		name, err := identifier("arm", namesOf(body))
		if err != nil {
			return nil, err
		}

		match, err := c.armMatch(a, s)
		if err != nil {
			return nil, err
		}

		one := arm{expr: expr + "." + name, item: originalOf(body), match: match}

		switch kind := body.GetKind().(type) {
		case *irpb.Node_Group:
			one.group = body.GetId()
		case *irpb.Node_Field:
			one.field = kind.Field
		}

		out = append(out, one)
	}

	return out, nil
}

// armMatch is the Go expression testing an arm's predicate against the bytes of
// the occurrence in scope.
func (c *coder) armMatch(a *irpb.Arm, s scope) (string, error) {
	node, ok := c.nodes[a.GetPredicateId()]
	if !ok {
		return "", unresolved(a.GetPredicateId())
	}

	predicate := node.GetPredicate()
	if predicate == nil {
		return "", malformed(fmt.Sprintf("node %d selects an arm and is not a predicate node", a.GetPredicateId()),
			"each arm names the predicate that selects it; see docs/ir/SPEC.md, \"A predicate on an arm reads one occurrence\"")
	}

	target, ok := c.nodes[predicate.GetFieldId()]
	if !ok {
		return "", unresolved(predicate.GetFieldId())
	}

	field := target.GetField()
	if field == nil {
		return "", malformed(fmt.Sprintf("node %d is a predicate's target and is not a field node", predicate.GetFieldId()),
			"a predicate always names a field; see docs/ir/SPEC.md, \"A predicate always names a field\"")
	}

	if field.GetRepetition() != nil {
		return "", malformed(fmt.Sprintf("%s is the target of an arm's predicate and repeats", originalOf(target)),
			"a predicate names a field, not an occurrence of one; see docs/ir/SPEC.md, \"A reference names a field, not an occurrence of one\"")
	}

	at, found, err := c.offsetOf(s.occurrence, predicate.GetFieldId(), s.occExpr, s.dir)
	if err != nil {
		return "", err
	}

	if !found {
		return "", malformed(fmt.Sprintf("%s is the target of an arm's predicate and is not in the occurrence the arm is chosen for", originalOf(target)),
			"an arm's predicate target MUST be contained, at any depth, in the innermost group that repeats and contains the variant; see docs/ir/SPEC.md, \"A predicate on an arm reads one occurrence\"")
	}

	slice := fmt.Sprintf("%s[%s:%s]", s.buf, at.String(), at.plus(int(field.GetWidth())).String())

	c.imports["bytes"] = struct{}{}

	switch test := predicate.GetTest().(type) {
	case *irpb.Predicate_BytesEqual:
		return fmt.Sprintf("bytes.Equal(%s, []byte(%s))", slice, strconv.Quote(string(test.BytesEqual.GetValue()))), nil
	case *irpb.Predicate_BytesOneOf:
		if len(test.BytesOneOf.GetValues()) < 2 {
			return "", malformed("a one-of predicate carries fewer than two literals",
				"a producer MUST carry at least two and MUST NOT carry the same literal twice; see docs/ir/SPEC.md, \"Discriminator predicates\"")
		}

		tests := make([]string, 0, len(test.BytesOneOf.GetValues()))

		for _, value := range test.BytesOneOf.GetValues() {
			tests = append(tests, fmt.Sprintf("bytes.Equal(%s, []byte(%s))", slice, strconv.Quote(string(value))))
		}

		return "(" + strings.Join(tests, " || ") + ")", nil
	default:
		return "", malformed("a predicate carries no test",
			"the set is closed and a predicate carries one member of it; see docs/ir/SPEC.md, \"Discriminator predicates\"")
	}
}

// collectCounts records every repetition of a record naming a count field.
func (c *coder) collectCounts(id uint64, expr string, loops []countLoop) error {
	members, err := c.flattened(id)
	if err != nil {
		return err
	}

	for _, memberID := range members {
		member, ok := c.nodes[memberID]
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

		// An item the copybook gives no data-name has no occurrences a writer
		// counts: an elementary one is a run of retained bytes, and a group
		// carrying no name is not here at all — [emitter.flattened] has already
		// put its members in this list, and each of them is walked on its own
		// terms.
		if anonymous(names) {
			continue
		}

		name, err := identifier(kind, names)
		if err != nil {
			return err
		}

		at := expr + "." + name

		if variable, ok := rep.GetCount().(*irpb.Repetition_Variable); ok {
			if count, ok := variable.Variable.GetCount().(*irpb.VariableCount_FieldId); ok {
				c.countOf[count.FieldId] = append(c.countOf[count.FieldId], countUse{
					item:    names.GetOriginal(),
					expr:    at,
					loops:   loops,
					minimum: variable.Variable.GetMinOccurrences(),
					maximum: variable.Variable.GetMaxOccurrences(),
				})
			}
		}

		if kind != "group" {
			continue
		}

		inner := loops
		element := at

		if rep != nil {
			variable := fmt.Sprintf("k%d", len(loops))
			inner = append(append([]countLoop{}, loops...), countLoop{variable: variable, over: at, item: names.GetOriginal()})
			element = at + "[" + variable + "]"
		}

		if err := c.collectCounts(memberID, element, inner); err != nil {
			return err
		}
	}

	return nil
}

// countValue is the Go expression an OCCURS DEPENDING ON count is read from
// while decoding.
func (c *coder) countValue(v *irpb.VariableCount, item string) (string, error) {
	count, ok := v.GetCount().(*irpb.VariableCount_FieldId)
	if !ok {
		return "", malformed("an OCCURS DEPENDING ON says nothing about where its count comes from",
			"a variable count names a field of the record being read or a register an earlier transition bound")
	}

	held, ok := c.valueOf[count.FieldId]
	if !ok {
		return "", malformed(fmt.Sprintf("%s of %s depends on a count this record has not read when the table begins", item, c.record),
			"a count is in hand before the extent it decides, and it names a field that does not repeat; see docs/ir/SPEC.md, \"A count is in hand before the extent it decides\"")
	}

	node, ok := c.nodes[count.FieldId]
	if !ok {
		return "", unresolved(count.FieldId)
	}

	if node.GetField().GetPicture().GetDigits() > 18 {
		return held + ".Int64()", nil
	}

	return held, nil
}

// countName is what the copybook calls a count, for a diagnostic.
func (c *coder) countName(v *irpb.VariableCount) string {
	count, ok := v.GetCount().(*irpb.VariableCount_FieldId)
	if !ok {
		return "a register an earlier record bound"
	}

	return originalOf(c.nodes[count.FieldId])
}

// countLiteral is the number of occurrences as the count field's own Go type.
func (c *coder) countLiteral(f *irpb.Field, held string) (string, error) {
	typ, err := c.fieldType(f)
	if err != nil {
		return "", err
	}

	if typ == bigIntType {
		c.imports[bigIntImport] = struct{}{}

		return fmt.Sprintf("big.NewInt(int64(%s))", held), nil
	}

	return fmt.Sprintf("%s(%s)", typ, held), nil
}

// retain notes that the descriptor carries a slack node this wide, which is
// what sizes the run of zero bytes a writer emits for one a record does not
// carry.
func (c *coder) retain(_ *strings.Builder, width uint32) {
	if width > c.zeroFill {
		c.zeroFill = width
	}
}

// armBody is the group or field node an arm's body names.
func (c *coder) armBody(a *irpb.Arm) (*irpb.Node, error) {
	return c.emitter.armBody(a)
}

// buffers reports whether one occurrence of the group id has to be read whole
// before it is walked: it holds a variant that is not inside a table of its own.
func (c *coder) buffers(id uint64) bool {
	group, err := c.group(id)
	if err != nil {
		return false
	}

	for _, memberID := range group.GetMemberIds() {
		member, ok := c.nodes[memberID]
		if !ok {
			continue
		}

		switch kind := member.GetKind().(type) {
		case *irpb.Node_Variant:
			return true
		case *irpb.Node_Group:
			if kind.Group.GetRepetition() == nil && c.buffers(memberID) {
				return true
			}
		}
	}

	return false
}

// line writes one statement of the generated body. Indentation is gofmt's:
// every file is formatted before it is written, so nothing here counts tabs.
func line(b *strings.Builder, format string, args ...any) {
	fmt.Fprintf(b, format, args...)
	b.WriteString("\n")
}

// failf is a `return fmt.Errorf(...)` naming the record and the occurrence the
// walk is in.
func failf(s scope, what string, args ...string) string {
	return "return " + errorf(s, what, args, "")
}

// wrapf is the same around an error codec reported.
func wrapf(s scope, what string, args ...string) string {
	return "return " + errorf(s, what, args, "err")
}

func errorf(s scope, what string, args []string, wrapped string) string {
	format := s.record + ": " + what + s.suffix

	all := append(append([]string{}, args...), s.args...)

	if wrapped != "" {
		format += ": %w"

		all = append(all, wrapped)
	}

	if len(all) == 0 {
		return fmt.Sprintf("fmt.Errorf(%s)", strconv.Quote(format))
	}

	return fmt.Sprintf("fmt.Errorf(%s, %s)", strconv.Quote(format), strings.Join(all, ", "))
}
