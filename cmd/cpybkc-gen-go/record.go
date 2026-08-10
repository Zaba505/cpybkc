// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/Zaba505/cpybkc/irpb"
)

// recordsFile is the file the record structs are written to, beside the
// package's doc file.
//
// A file of its own rather than more of doc.go, because the files this
// generator writes divide by what produced them: doc.go is the package clause
// and nothing else, and this is everything the descriptor's record nodes
// produced. The decode and encode methods (#51) and the file-level reader and
// writer (#52) land in files of their own for the same reason.
const recordsFile = "records.go"

// slackField is the name of the unexported field a struct carries for the
// slack nodes among its members.
//
// It cannot collide with a member's field name: a member's name is munged to
// an exported identifier and this one is not, so the two live in disjoint sets
// however a copybook spells its items — including a copybook with an item
// literally called SLACK.
const slackField = "slack"

// bigIntType is the Go type a numeric item wider than an int64 takes, and
// bigIntImport is what the generated file imports for it.
const (
	bigIntType   = "*big.Int"
	bigIntImport = "math/big"
)

// records is the source of [recordsFile] for this descriptor, or the empty
// string where the descriptor carries no record node at all.
//
// One exported struct type per record node, in the order the node list carries
// them — which docs/ir/SPEC.md's "Identity, ordering and determinism" fixes as
// ascending identifier order, so the file's declarations are in a producer's
// deterministic order rather than in one this generator invents.
//
// Nested groups are anonymous struct types inside the record's, which is what
// makes "one Go struct per record definition" literally true and keeps this
// story from inventing a naming scheme for types the copybook never named.
// Naming — the rename override, the munging and what a collision after it is —
// is #50's, and it is the one place this generator refuses rather than guesses.
func records(d *irpb.Descriptor, opts options) (string, error) {
	e, err := newEmitter(d)
	if err != nil {
		return "", err
	}

	var decls []string

	declared := make(map[string]string)

	for _, node := range d.GetNodes() {
		record := node.GetRecord()
		if record == nil {
			continue
		}

		name, err := identifier("record", record.GetNames())
		if err != nil {
			return "", err
		}

		if first, dup := declared[name]; dup {
			return "", &collisionError{
				Go:    name,
				Cobol: []string{first, record.GetNames().GetOriginal()},
				Where: "the generated package",
			}
		}

		declared[name] = record.GetNames().GetOriginal()

		body, err := e.structType(record.GetRootId())
		if err != nil {
			return "", err
		}

		decl := fmt.Sprintf("// %s is the %s record, as docs/ir/SPEC.md resolved it.\ntype %s %s",
			name, record.GetNames().GetOriginal(), name, body)

		decls = append(decls, decl)
	}

	if len(decls) == 0 {
		return "", nil
	}

	var b strings.Builder

	b.WriteString(generatedBy)
	b.WriteString("\n\npackage ")
	b.WriteString(opts.packageName)
	b.WriteString("\n")

	for _, path := range e.sortedImports() {
		fmt.Fprintf(&b, "\nimport %q\n", path)
	}

	for _, decl := range decls {
		b.WriteString("\n")
		b.WriteString(decl)
		b.WriteString("\n")
	}

	return b.String(), nil
}

// emitter carries what turning one descriptor's records into Go needs: the
// node list indexed by identifier, the imports the declarations have asked
// for, and the containment path currently being walked.
type emitter struct {
	// nodes is the node list by identifier. docs/ir/SPEC.md's "A node set, not
	// a tree" obliges a consumer to build this before it can walk anything.
	nodes map[uint64]*irpb.Node

	// imports is the set of import paths the declarations so far need. Only a
	// numeric item too wide for an int64 puts anything in it today.
	imports map[string]struct{}

	// visiting is the containment path from the record's root to where the
	// walk is now. Containment MUST be acyclic; this is what turns a
	// descriptor breaking that into a diagnostic rather than a stack overflow.
	visiting map[uint64]struct{}
}

func newEmitter(d *irpb.Descriptor) (*emitter, error) {
	e := &emitter{
		nodes:    make(map[uint64]*irpb.Node, len(d.GetNodes())),
		imports:  make(map[string]struct{}),
		visiting: make(map[uint64]struct{}),
	}

	for _, node := range d.GetNodes() {
		if _, dup := e.nodes[node.GetId()]; dup {
			return nil, malformed(fmt.Sprintf("two nodes carry identifier %d", node.GetId()),
				"an identifier means identity, so a descriptor stating one twice describes two things as one")
		}

		e.nodes[node.GetId()] = node
	}

	return e, nil
}

// sortedImports is the import paths in a fixed order, so that two runs over one
// descriptor write the same bytes.
func (e *emitter) sortedImports() []string {
	paths := make([]string, 0, len(e.imports))
	for path := range e.imports {
		paths = append(paths, path)
	}

	sort.Strings(paths)

	return paths
}

// structType is the Go struct type of the group node id names, as source.
//
// The members become fields in the order the member list carries them, which
// docs/ir/SPEC.md makes record order, and the slack nodes among them become the
// one unexported field at the end.
func (e *emitter) structType(id uint64) (string, error) {
	node, ok := e.nodes[id]
	if !ok {
		return "", unresolved(id)
	}

	group := node.GetGroup()
	if group == nil {
		return "", malformed(fmt.Sprintf("node %d is not a group node, and a group is what stands here", id),
			"a record's top level and a group's group members are group nodes; see docs/ir/SPEC.md, \"The node kinds\"")
	}

	if _, cycle := e.visiting[id]; cycle {
		return "", malformed(fmt.Sprintf("node %d contains itself", id),
			"docs/ir/SPEC.md requires containment to be acyclic, and a record whose group contains itself has no width")
	}

	e.visiting[id] = struct{}{}
	defer delete(e.visiting, id)

	var (
		fields []string
		named  = make(map[string]string)
		slack  int
	)

	for _, memberID := range group.GetMemberIds() {
		member, ok := e.nodes[memberID]
		if !ok {
			return "", unresolved(memberID)
		}

		switch member.GetKind().(type) {
		case *irpb.Node_Slack:
			// A width and nothing else, so it contributes no member field: the
			// bytes it stands for live in the one unexported field below,
			// paired to it by the position it holds among this group's slack
			// members.
			slack++

			continue
		case *irpb.Node_Variant:
			return "", &unsupportedShapeError{Node: memberID}
		}

		field, name, err := e.member(member)
		if err != nil {
			return "", err
		}

		if first, dup := named[name]; dup {
			return "", &collisionError{
				Go:    name,
				Cobol: []string{first, originalOf(member)},
				Where: "the " + group.GetNames().GetOriginal() + " group",
			}
		}

		named[name] = originalOf(member)

		fields = append(fields, field)
	}

	if slack > 0 {
		fields = append(fields, slackDeclaration(slack))
	}

	return "struct {\n" + strings.Join(fields, "\n\n") + "\n}", nil
}

// member is one field of a struct: the declaration with its doc comment, and
// the Go name it declares.
func (e *emitter) member(node *irpb.Node) (string, string, error) {
	switch kind := node.GetKind().(type) {
	case *irpb.Node_Group:
		name, err := identifier("group", kind.Group.GetNames())
		if err != nil {
			return "", "", err
		}

		body, err := e.structType(node.GetId())
		if err != nil {
			return "", "", err
		}

		typ, err := e.repeated(kind.Group.GetRepetition(), body)
		if err != nil {
			return "", "", err
		}

		summary, err := e.groupSummary(kind.Group)
		if err != nil {
			return "", "", err
		}

		return comment(name, kind.Group.GetNames().GetOriginal(), summary) + name + " " + typ, name, nil
	case *irpb.Node_Field:
		name, err := identifier("field", kind.Field.GetNames())
		if err != nil {
			return "", "", err
		}

		base, err := e.fieldType(kind.Field)
		if err != nil {
			return "", "", err
		}

		typ, err := e.repeated(kind.Field.GetRepetition(), base)
		if err != nil {
			return "", "", err
		}

		summary, err := e.fieldSummary(kind.Field)
		if err != nil {
			return "", "", err
		}

		return comment(name, kind.Field.GetNames().GetOriginal(), summary) + name + " " + typ, name, nil
	default:
		return "", "", malformed(fmt.Sprintf("node %d is not something a group may contain", node.GetId()),
			"a member list names a group, variant, field or slack node; see docs/ir/SPEC.md, \"The node kinds\"")
	}
}

// repeated wraps a member's type in what its repetition makes of it.
//
// A constant count is an array and a variable one is a slice, and the
// difference is where the number of occurrences comes from: OCCURS n is a fact
// of the copybook, so it belongs in the type where nothing can disagree with
// the descriptor, and an OCCURS DEPENDING ON count is data read at run time,
// so it cannot.
func (e *emitter) repeated(rep *irpb.Repetition, base string) (string, error) {
	if rep == nil {
		return base, nil
	}

	switch count := rep.GetCount().(type) {
	case *irpb.Repetition_Constant:
		return fmt.Sprintf("[%d]%s", count.Constant, base), nil
	case *irpb.Repetition_Variable:
		return "[]" + base, nil
	default:
		return "", malformed("an item repeats and says nothing about how many times",
			"a repetition carries a constant count or an OCCURS DEPENDING ON one; an item that does not repeat carries no repetition at all")
	}
}

// fieldType is the Go type one occurrence of an elementary item takes.
//
// The table this implements is documented in README.md, and the shape of it is
// that USAGE decides the family and the picture's digit count decides how wide
// a Go integer has to be to hold every value the picture admits. Nothing here
// is derived from the item's width in bytes: a width is what the item occupies
// in the file, and the two agree only for DISPLAY.
//
// A scaled item takes an integer all the same, and it holds the unscaled one.
// The Go standard library has no decimal type, and the accessors this generated
// code will call in #51 — cobol-go's codec — return integers, so a scale
// applied here would be applied twice by the time a value reached a caller.
// Where the scale is, and what it is, is the field's doc comment.
func (e *emitter) fieldType(f *irpb.Field) (string, error) {
	if err := resolved(f.GetEncoding()); err != nil {
		return "", err
	}

	switch f.GetUsage() {
	case irpb.Usage_USAGE_COMP_1:
		return "float32", nil
	case irpb.Usage_USAGE_COMP_2:
		return "float64", nil
	case irpb.Usage_USAGE_INDEX, irpb.Usage_USAGE_POINTER, irpb.Usage_USAGE_NATIONAL:
		// The IR derives no logical value for these and says so: each carries a
		// width so that the sum stays correct across it and nothing more. The
		// field is the bytes, so that an item the copybook declares is visible
		// in the record rather than silently absent from it.
		return "[]byte", nil
	case irpb.Usage_USAGE_DISPLAY:
		picture := f.GetPicture()
		if picture == nil {
			return "", malformed("a DISPLAY item carries no picture",
				"only COMP-1, COMP-2, INDEX, POINTER and NATIONAL items have no PICTURE to resolve")
		}

		if picture.GetCategory() == irpb.Category_CATEGORY_NUMERIC {
			return e.decimal(picture.GetDigits()), nil
		}

		if picture.GetCategory() == irpb.Category_CATEGORY_UNSPECIFIED {
			return "", malformed("an item's picture states no category",
				"a consumer may not supply one; see docs/ir/SPEC.md and the Category enum")
		}

		// Alphabetic, alphanumeric, and the two edited categories. An edited
		// item has no logical value the IR describes either, and it is a string
		// here rather than bytes because its storage is characters in the
		// field's charset — the edited text, as it stands in the record.
		return "string", nil
	case irpb.Usage_USAGE_PACKED_DECIMAL, irpb.Usage_USAGE_COMP_6:
		if err := numeric(f); err != nil {
			return "", err
		}

		return e.decimal(f.GetPicture().GetDigits()), nil
	case irpb.Usage_USAGE_BINARY, irpb.Usage_USAGE_COMP_5:
		if err := numeric(f); err != nil {
			return "", err
		}

		return e.binary(f.GetPicture().GetDigits()), nil
	default:
		return "", malformed(fmt.Sprintf("an item carries USAGE %d, which this generator does not know", int32(f.GetUsage())),
			"docs/ir/SPEC.md requires a consumer to refuse a member of a closed set it does not recognise rather than fall back to one it does")
	}
}

// decimal is the Go type a zoned or packed numeric item of that many digits
// takes: the narrowest of the integer types cobol-go's codec reads one into
// that holds every value the picture admits.
func (e *emitter) decimal(digits uint32) string {
	switch {
	case digits <= 9:
		return "int32"
	case digits <= 18:
		return "int64"
	default:
		e.imports[bigIntImport] = struct{}{}

		return bigIntType
	}
}

// binary is the same for a COMP, COMP-4 or COMP-5 item. It has one more step
// than [emitter.decimal] because a binary item of four digits or fewer occupies
// two bytes, and codec reads that one into an int16.
func (e *emitter) binary(digits uint32) string {
	if digits <= 4 {
		return "int16"
	}

	return e.decimal(digits)
}

// numeric refuses an item whose USAGE says it holds a number and whose picture
// does not.
func numeric(f *irpb.Field) error {
	picture := f.GetPicture()
	if picture == nil {
		return malformed("an item with a computational USAGE carries no picture",
			"only COMP-1, COMP-2, INDEX, POINTER and NATIONAL items have no PICTURE to resolve")
	}

	if picture.GetCategory() != irpb.Category_CATEGORY_NUMERIC {
		return malformed("an item with a computational USAGE is not of the numeric category",
			"a picture that is not numeric has no digits for a computational usage to store")
	}

	return nil
}

// resolved refuses a field whose encoding leaves one of its four axes unset.
//
// Nothing in this file reads an axis — a Go type does not turn on a charset —
// and the check is here all the same, because docs/ir/SPEC.md puts it on every
// consumer and every one of the four fails silently when wrong. An IR that
// reached a generator with an axis unresolved is a bug upstream of it, and a
// generator that emitted a struct for it would be the last thing in a position
// to say so.
func resolved(enc *irpb.Encoding) error {
	if enc == nil {
		return malformed("an item carries no encoding",
			"a producer must set all four axes on every field, and a consumer may not supply a default for one; see docs/ir/SPEC.md, \"The encoding profile, applied\"")
	}

	unset := make([]string, 0, 4)

	if enc.GetCharset() == irpb.Charset_CHARSET_UNSPECIFIED {
		unset = append(unset, "charset")
	}

	if enc.GetSignConvention() == irpb.SignConvention_SIGN_CONVENTION_UNSPECIFIED {
		unset = append(unset, "sign convention")
	}

	if enc.GetByteOrder() == irpb.ByteOrder_BYTE_ORDER_UNSPECIFIED {
		unset = append(unset, "byte order")
	}

	if enc.GetFloatFormat() == irpb.FloatFormat_FLOAT_FORMAT_UNSPECIFIED {
		unset = append(unset, "float format")
	}

	if len(unset) == 0 {
		return nil
	}

	return malformed(fmt.Sprintf("an item's encoding leaves %s unresolved", english(unset)),
		"a producer must set all four axes on every field, and a consumer may not supply a default for one; see docs/ir/SPEC.md, \"The encoding profile, applied\"")
}

// fieldSummary is what a field's doc comment says about the item behind it.
func (e *emitter) fieldSummary(f *irpb.Field) (string, error) {
	parts := []string{usageName(f.GetUsage())}

	if picture := f.GetPicture(); picture != nil {
		parts = append([]string{categoryName(picture.GetCategory())}, parts...)

		if picture.GetCategory() == irpb.Category_CATEGORY_NUMERIC {
			parts = append(parts, plural(int(picture.GetDigits()), "digit"))

			if picture.GetSigned() {
				parts = append(parts, "signed")
			} else {
				parts = append(parts, "unsigned")
			}
		}
	}

	parts = append(parts, plural(int(f.GetWidth()), "byte"))

	occurs, err := e.occurs(f.GetRepetition())
	if err != nil {
		return "", err
	}

	if occurs != "" {
		parts = append(parts, occurs)
	}

	summary := strings.Join(parts, ", ") + "."

	// The scale is the one attribute the Go type does not carry, so it is the
	// one the comment has to say out loud: the field holds the unscaled
	// integer, and a reader of the generated code has nowhere else to learn by
	// how much. Said only where the field is a number — an edited item's scale
	// is already spelled in the characters it stores.
	if scale := f.GetPicture().GetScale(); scale != 0 && f.GetPicture().GetCategory() == irpb.Category_CATEGORY_NUMERIC {
		summary += fmt.Sprintf("\nThe value is unscaled: the item's value is this field times 10^%d.", -scale)
	}

	return summary, nil
}

// groupSummary is the same for a group.
func (e *emitter) groupSummary(g *irpb.Group) (string, error) {
	parts := []string{plural(len(g.GetMemberIds()), "member")}

	occurs, err := e.occurs(g.GetRepetition())
	if err != nil {
		return "", err
	}

	if occurs != "" {
		parts = append(parts, occurs)
	}

	return "a group of " + strings.Join(parts, ", ") + ".", nil
}

// occurs is how a repetition reads in a comment, or the empty string where the
// item does not repeat.
func (e *emitter) occurs(rep *irpb.Repetition) (string, error) {
	if rep == nil {
		return "", nil
	}

	switch count := rep.GetCount().(type) {
	case *irpb.Repetition_Constant:
		return fmt.Sprintf("OCCURS %d", count.Constant), nil
	case *irpb.Repetition_Variable:
		on, err := e.countedBy(count.Variable)
		if err != nil {
			return "", err
		}

		return fmt.Sprintf("OCCURS %d TO %d DEPENDING ON %s",
			count.Variable.GetMinOccurrences(), count.Variable.GetMaxOccurrences(), on), nil
	default:
		return "", malformed("an item repeats and says nothing about how many times",
			"a repetition carries a constant count or an OCCURS DEPENDING ON one; an item that does not repeat carries no repetition at all")
	}
}

// countedBy names where an OCCURS DEPENDING ON count is read from.
func (e *emitter) countedBy(v *irpb.VariableCount) (string, error) {
	switch count := v.GetCount().(type) {
	case *irpb.VariableCount_FieldId:
		node, ok := e.nodes[count.FieldId]
		if !ok {
			return "", unresolved(count.FieldId)
		}

		field := node.GetField()
		if field == nil {
			return "", malformed(fmt.Sprintf("node %d counts an OCCURS DEPENDING ON and is not a field node", count.FieldId),
				"a count is a field of the record being read or a register an earlier transition bound")
		}

		return field.GetNames().GetOriginal(), nil
	case *irpb.VariableCount_RegisterId:
		// Named as what it is rather than by an identifier: a register has no
		// name, and the useful fact about one is that its value came from a
		// record read before this one.
		return "a register an earlier record bound", nil
	default:
		return "", malformed("an OCCURS DEPENDING ON says nothing about where its count comes from",
			"a variable count names a field of the record being read or a register an earlier transition bound")
	}
}

// slackDeclaration is the one unexported field a struct carries for the slack
// nodes among its members.
func slackDeclaration(runs int) string {
	return fmt.Sprintf(`// %[1]s is the bytes retained for the slack nodes among this item's
// members, in the order those nodes occupy the record: one run each, as
// they stood when the record was read, and one set of them per occurrence
// of this struct. A nil run is one the record does not carry; an empty run
// is a run of no bytes, and the two are not the same.
//
// They travel with the record and there is nothing here for a caller to do.
// See docs/ir/SPEC.md, "Slack survives a read".
%[1]s [%[2]d][]byte`, slackField, runs)
}

// comment is a struct field's doc comment, opening with the Go name Go's own
// convention wants there and naming the copybook's word for it.
func comment(name, original, summary string) string {
	var b strings.Builder

	for _, line := range strings.Split(fmt.Sprintf("%s is %s — %s", name, original, summary), "\n") {
		b.WriteString("// ")
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

// identifier is the exported Go identifier for a node's names.
//
// The original COBOL name and nothing else: the rename override is #50's, along
// with the rest of what munging owes an adopter — the traceability tag and what
// a collision after munging is. What is here is the least that turns a copybook
// name into an identifier, and it refuses rather than invents where that is not
// enough.
func identifier(kind string, names *irpb.Names) (string, error) {
	original := names.GetOriginal()

	munged := munge(original)

	if munged == "" {
		return "", &unmungeableError{Kind: kind, Cobol: original}
	}

	if unicode.IsDigit(rune(munged[0])) {
		return "", &unmungeableError{Kind: kind, Cobol: original}
	}

	return munged, nil
}

// munge turns a COBOL name into an exported Go identifier: each run of letters
// and digits becomes one word, capitalised, and the separators between them go.
func munge(name string) string {
	var b strings.Builder

	boundary := true

	for _, r := range name {
		switch {
		case unicode.IsLetter(r):
			if boundary {
				b.WriteRune(unicode.ToUpper(r))
			} else {
				b.WriteRune(unicode.ToLower(r))
			}

			boundary = false
		case unicode.IsDigit(r):
			b.WriteRune(r)

			boundary = false
		default:
			boundary = true
		}
	}

	return b.String()
}

// originalOf is a node's COBOL name, for a diagnostic about it. A node the
// copybook gives no name — a slack node, a state, a register — has none.
func originalOf(node *irpb.Node) string {
	switch kind := node.GetKind().(type) {
	case *irpb.Node_Record:
		return kind.Record.GetNames().GetOriginal()
	case *irpb.Node_Group:
		return kind.Group.GetNames().GetOriginal()
	case *irpb.Node_Field:
		return kind.Field.GetNames().GetOriginal()
	default:
		return ""
	}
}

// plural is a count and its unit, with the unit pluralised.
func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}

	return fmt.Sprintf("%d %ss", n, unit)
}

// english joins a list the way a sentence does.
func english(items []string) string {
	switch len(items) {
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
	}
}

// usageName is a USAGE as a copybook spells it.
func usageName(u irpb.Usage) string {
	switch u {
	case irpb.Usage_USAGE_DISPLAY:
		return "DISPLAY"
	case irpb.Usage_USAGE_PACKED_DECIMAL:
		return "PACKED-DECIMAL"
	case irpb.Usage_USAGE_COMP_6:
		return "COMP-6"
	case irpb.Usage_USAGE_BINARY:
		return "BINARY"
	case irpb.Usage_USAGE_COMP_5:
		return "COMP-5"
	case irpb.Usage_USAGE_COMP_1:
		return "COMP-1"
	case irpb.Usage_USAGE_COMP_2:
		return "COMP-2"
	case irpb.Usage_USAGE_INDEX:
		return "INDEX"
	case irpb.Usage_USAGE_POINTER:
		return "POINTER"
	case irpb.Usage_USAGE_NATIONAL:
		return "NATIONAL"
	default:
		return "an unnamed USAGE"
	}
}

// categoryName is a picture's category in the standard's words.
func categoryName(c irpb.Category) string {
	switch c {
	case irpb.Category_CATEGORY_NUMERIC:
		return "numeric"
	case irpb.Category_CATEGORY_ALPHABETIC:
		return "alphabetic"
	case irpb.Category_CATEGORY_ALPHANUMERIC:
		return "alphanumeric"
	case irpb.Category_CATEGORY_NUMERIC_EDITED:
		return "numeric-edited"
	case irpb.Category_CATEGORY_ALPHANUMERIC_EDITED:
		return "alphanumeric-edited"
	default:
		return "an unnamed category"
	}
}
