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
// produced. The decode and encode methods land in [codecFile] and the
// file-level reader and writer (#52) land in a file of their own for the same
// reason.
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

	declared := make(map[string]colliding)

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
				Cobol: []colliding{first, namedBy(record.GetNames())},
				Where: "the generated package",
			}
		}

		declared[name] = namedBy(record.GetNames())

		body, err := e.structType(record.GetRootId())
		if err != nil {
			return "", err
		}

		doc := fmt.Sprintf("%s is the %s record, as docs/ir/SPEC.md resolved it.%s",
			name, record.GetNames().GetOriginal(), renameNote(record.GetNames()))

		decls = append(decls, commentLines(doc)+fmt.Sprintf("type %s %s", name, body))
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
		named  = make(map[string]colliding)
		slack  int
	)

	for _, memberID := range group.GetMemberIds() {
		member, ok := e.nodes[memberID]
		if !ok {
			return "", unresolved(memberID)
		}

		switch kind := member.GetKind().(type) {
		case *irpb.Node_Slack:
			// A width and nothing else, so it contributes no member field: the
			// bytes it stands for live in the one unexported field below,
			// paired to it by the position it holds among this group's slack
			// members.
			slack++

			continue
		case *irpb.Node_Variant:
			// One field per arm rather than one field for the alternation, so
			// that this generator still names nothing the copybook did not.
			arms, err := e.armFields(kind.Variant)
			if err != nil {
				return "", err
			}

			for _, arm := range arms {
				if first, dup := named[arm.name]; dup {
					return "", &collisionError{
						Go:    arm.name,
						Cobol: []colliding{first, arm.cobol},
						Where: "the " + group.GetNames().GetOriginal() + " group",
					}
				}

				named[arm.name] = arm.cobol

				fields = append(fields, arm.decl)
			}

			continue
		}

		field, name, err := e.member(member, false)
		if err != nil {
			return "", err
		}

		if first, dup := named[name]; dup {
			return "", &collisionError{
				Go:    name,
				Cobol: []colliding{first, namedBy(namesOf(member))},
				Where: "the " + group.GetNames().GetOriginal() + " group",
			}
		}

		named[name] = namedBy(namesOf(member))

		fields = append(fields, field)
	}

	if slack > 0 {
		fields = append(fields, slackDeclaration(slack))
	}

	return "struct {\n" + strings.Join(fields, "\n\n") + "\n}", nil
}

// member is one field of a struct: the declaration with its doc comment, and
// the Go name it declares.
//
// pointer is what an arm of a variant takes and nothing else: an arm is one of
// several alternatives over one run of bytes, and a pointer is what says which
// of them an occurrence holds without this generator inventing a discriminant
// identifier the copybook never wrote.
func (e *emitter) member(node *irpb.Node, pointer bool) (string, string, error) {
	star := ""
	if pointer {
		star = "*"
	}

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

		return comment(name, kind.Group.GetNames(), summary) + name + " " + star + typ, name, nil
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

		return comment(name, kind.Field.GetNames(), summary) + name + " " + star + typ, name, nil
	default:
		return "", "", malformed(fmt.Sprintf("node %d is not something a group may contain", node.GetId()),
			"a member list names a group, variant, field or slack node; see docs/ir/SPEC.md, \"The node kinds\"")
	}
}

// armField is one arm of a variant as a field of the struct the variant's group
// became.
type armField struct {
	// decl is the declaration with its doc comment.
	decl string

	// name is the Go identifier it declares.
	name string

	// cobol is what the copybook calls the arm's body, for a collision
	// diagnostic.
	cobol colliding
}

// armFields is the fields a variant contributes to the struct of the group
// containing it: one per arm, each a pointer to the arm's body.
//
// A pointer per arm rather than a discriminant beside them. Go has no sum type,
// so an occurrence of such a table is a choice among alternatives and something
// has to say which one it holds (docs/ir/SPEC.md, "A variant is chosen once per
// occurrence") — and every other shape wants an identifier neither the copybook
// nor the layout wrote. A variant carries no name of its own, so a discriminant
// field, a discriminant type or a constant per arm would each be a name this
// generator invented, which is exactly what it refuses to do everywhere else.
// Nil and non-nil say the same thing and say it in names the copybook already
// spells: exactly one arm of an occurrence is non-nil.
func (e *emitter) armFields(v *irpb.Variant) ([]armField, error) {
	if len(v.GetArms()) < 2 {
		return nil, malformed(fmt.Sprintf("a variant carries %d arms", len(v.GetArms())),
			"a producer must not emit a variant carrying fewer than two arms; see docs/ir/SPEC.md, \"A variant is chosen once per occurrence\"")
	}

	bodies := make([]*irpb.Node, 0, len(v.GetArms()))

	for _, arm := range v.GetArms() {
		body, err := e.armBody(arm)
		if err != nil {
			return nil, err
		}

		bodies = append(bodies, body)
	}

	names := make([]string, 0, len(bodies))

	for _, body := range bodies {
		name, err := identifier("arm", namesOf(body))
		if err != nil {
			return nil, err
		}

		names = append(names, name)
	}

	fields := make([]armField, 0, len(bodies))

	for i, body := range bodies {
		decl, name, err := e.member(body, true)
		if err != nil {
			return nil, err
		}

		fields = append(fields, armField{
			decl:  armNote(decl, names, i),
			name:  name,
			cobol: namedBy(namesOf(body)),
		})
	}

	return fields, nil
}

// armBody is the group or field node an arm's body names.
func (e *emitter) armBody(arm *irpb.Arm) (*irpb.Node, error) {
	var id uint64

	switch body := arm.GetBody().(type) {
	case *irpb.Arm_GroupId:
		id = body.GroupId
	case *irpb.Arm_FieldId:
		id = body.FieldId
	default:
		return nil, malformed("an arm of a variant names no body",
			"each arm names the predicate that selects it and the group or field that is its body; see docs/ir/SPEC.md, \"The node kinds\"")
	}

	node, ok := e.nodes[id]
	if !ok {
		return nil, unresolved(id)
	}

	switch node.GetKind().(type) {
	case *irpb.Node_Group, *irpb.Node_Field:
		return node, nil
	default:
		return nil, malformed(fmt.Sprintf("node %d is the body of an arm and is neither a group nor a field", id),
			"an arm names the group or field that is its body; see docs/ir/SPEC.md, \"The node kinds\"")
	}
}

// armNote is an arm's declaration with the sentence that says it is one, added
// beneath the doc comment the member itself produced.
func armNote(decl string, names []string, i int) string {
	others := make([]string, 0, len(names)-1)

	for j, name := range names {
		if j != i {
			others = append(others, name)
		}
	}

	note := commentLines(fmt.Sprintf(
		"It is one alternative over one run of bytes, beside %s: exactly one of\nthem is non-nil in an occurrence, and it is the one the record holds.\nSee docs/ir/SPEC.md, \"A variant is chosen once per occurrence\".",
		english(others)))

	// The doc comment is the run of lines opening the declaration that are
	// comments; the declaration itself begins at the first line that is not,
	// and the type may carry lines of its own behind that.
	end := 0

	for _, line := range strings.SplitAfter(decl, "\n") {
		if !strings.HasPrefix(line, "//") {
			break
		}

		end += len(line)
	}

	return decl[:end] + "//\n" + note + decl[end:]
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
	case irpb.Usage_USAGE_PACKED_DECIMAL:
		if err := numeric(f); err != nil {
			return "", err
		}

		return e.decimal(f.GetPicture().GetDigits()), nil
	// The Go type is the packed one at the same digit counts, because codec's
	// COMP-6 family returns the packed family's types: an int32, an int64 and a
	// big.Int. Everything else about the two usages differs, which is why this
	// is an arm of its own rather than a second label on the one above — and in
	// particular the item is refused here on the same terms it is refused in
	// [coder.readCall], so that a signed COMP-6 item is malformed wherever this
	// generator meets it rather than only where it emits an accessor for it.
	case irpb.Usage_USAGE_COMP_6:
		if err := unsignedPacked(f); err != nil {
			return "", err
		}

		return e.decimal(f.GetPicture().GetDigits()), nil
	case irpb.Usage_USAGE_BINARY, irpb.Usage_USAGE_COMP_5:
		if err := numeric(f); err != nil {
			return "", err
		}

		return e.binary(f.GetPicture()), nil
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

// binary is the same for a COMP, COMP-4 or COMP-5 item. It has two more steps
// than [emitter.decimal]: a binary item of four digits or fewer occupies two
// bytes, which codec reads into an int16, and an unsigned one is read into a
// uint64 whatever its digit count.
//
// A uint64 for a two-byte item is wider than the PICTURE needs, and it is still
// the rule the rest of this table follows — the type is the one the accessor
// returns — because codec offers no narrower unsigned reader and the narrowing
// would have to happen somewhere. Doing it in the generated decode would put a
// conversion in code nobody reads, where a value can change without anything
// saying so; doing it in the field's type would oblige the encode to widen it
// back. Which accessor an unsigned item takes at all is [unsignedBinary].
func (e *emitter) binary(p *irpb.Picture) string {
	if unsignedBinary(p) {
		return "uint64"
	}

	if p.GetDigits() <= 4 {
		return "int16"
	}

	return e.decimal(p.GetDigits())
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
//
// The copybook's name is in it whether or not the layout renamed the item, and
// that is what makes the identifier traceable: a reader holding the generated
// source can get back to the item in the copybook without holding the layout as
// well. Where a rename is what produced the identifier, [renameNote] says so
// beneath, because otherwise the name in the comment is one this generator's
// munging visibly did not produce and there is nothing on the page to say why.
func comment(name string, names *irpb.Names, summary string) string {
	return commentLines(fmt.Sprintf("%s is %s — %s%s", name, names.GetOriginal(), summary, renameNote(names)))
}

// commentLines is a doc comment's text as the lines of one, each line of it
// commented in turn so that a sentence added beneath is a sentence rather than
// source.
func commentLines(text string) string {
	var b strings.Builder

	for _, line := range strings.Split(text, "\n") {
		b.WriteString("// ")
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

// renameNote is the sentence a renamed item's doc comment carries, or the empty
// string where the layout renamed nothing.
//
// It names the override as the layout spells it rather than as this generator
// munged it, so that the sentence points at a line the adopter can go and edit.
func renameNote(names *irpb.Names) string {
	if names.GetOverrideName() == "" {
		return ""
	}

	return fmt.Sprintf("\nThe layout renames it to %s.", names.GetOverrideName())
}

// identifier is the exported Go identifier for a node's names: the override
// where the layout gave one and the copybook's own name otherwise, munged.
//
// The override is munged rather than taken as written, so that one rule decides
// what an identifier looks like whatever it was spelled from — an adopter who
// writes `customer_id` in a layout gets the identifier they would have got from
// a copybook item spelled that way. Being munged is also what keeps a rename
// from being a hole in the rest of this: a name that cannot become an exported
// identifier is refused wherever it came from, and two names that arrive at one
// identifier collide whether or not a rename put them there.
func identifier(kind string, names *irpb.Names) (string, error) {
	original := names.GetOriginal()

	// A node the copybook named and the descriptor did not is a bug in the
	// producer rather than a name an adopter can do anything about, so it is
	// reported as the malformed descriptor it is. Telling them to rename an
	// item in their layout would send them looking for a name that is not
	// missing from anything they wrote.
	if original == "" {
		return "", malformed(fmt.Sprintf("a %s node carries no name", kind),
			"record, group and field nodes each carry the name the copybook spells; see docs/ir/SPEC.md, \"The node kinds\"")
	}

	// An override that is present and empty is the same kind of bug, one step
	// further along: a rename substitutes a name, and the empty string is not
	// one. Falling back to the original would generate from a layout line that
	// silently did nothing.
	if names.OverrideName != nil && names.GetOverrideName() == "" {
		return "", malformed(fmt.Sprintf("the %s named %s carries an empty rename override", kind, original),
			"a rename substitutes a name for the copybook's, and there is no name in the empty string; see docs/ir/SPEC.md, \"Names\"")
	}

	spelled := original
	if names.GetOverrideName() != "" {
		spelled = names.GetOverrideName()
	}

	munged := munge(spelled)

	if munged == "" {
		return "", &unmungeableError{Kind: kind, Cobol: original, Override: names.GetOverrideName()}
	}

	if unicode.IsDigit(rune(munged[0])) {
		return "", &unmungeableError{Kind: kind, Cobol: original, Override: names.GetOverrideName()}
	}

	return munged, nil
}

// munge turns a name into an exported Go identifier: each run of letters and
// digits becomes one word, the separators between them go, and every word opens
// with a capital.
//
// What happens to the rest of each word turns on whether the name was written
// in one case throughout. A name that is — `ORDER-ID` as a copybook spells one,
// or `order_id` as an adopter might spell a rename — carries no casing of its
// own, so this supplies it and lowercases the tail: `OrderId`. A name written in
// more than one case carries the casing somebody chose, so this keeps it and
// only ensures the first letter is a capital: `CustomerID` stays `CustomerID`,
// and `custId` becomes `CustId`.
//
// That second half is what makes the rename override a control over the
// identifier rather than another string to be flattened. There is no table of
// initialisms here and there is deliberately not going to be one: a table is a
// list of words this generator has heard of, `OrderId` and `OrderID` would then
// differ by whether `ID` made the list, and a name an adopter cannot predict is
// the thing this generator refuses to produce everywhere else. An adopter who
// wants `OrderID` renames the item to `OrderID` and gets exactly that.
//
// Reserved words need no handling and get none: every identifier this produces
// is exported, so its first letter is a capital, and every Go keyword is
// lowercase. There is no COBOL name that munges to one.
func munge(name string) string {
	uniform := uniformCase(name)

	var b strings.Builder

	boundary := true

	for _, r := range name {
		switch {
		case unicode.IsLetter(r):
			switch {
			case boundary:
				b.WriteRune(unicode.ToUpper(r))
			case uniform:
				b.WriteRune(unicode.ToLower(r))
			default:
				b.WriteRune(r)
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

// uniformCase is whether a name is written in one case throughout, which is
// what says its casing carries no information for [munge] to keep.
//
// A name with no letter in it at all counts as uniform; it has no tail for the
// answer to change, and it is refused a line later for having no identifier in
// it.
func uniformCase(name string) bool {
	var upper, lower bool

	for _, r := range name {
		switch {
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsLower(r):
			lower = true
		}
	}

	return !(upper && lower)
}

// namesOf is what a node is called: the copybook's name for it and the rename
// the layout asked for beside it. A node the copybook gives no name — a slack
// node, a state, a register — carries none, and nil is what those are called.
func namesOf(node *irpb.Node) *irpb.Names {
	switch kind := node.GetKind().(type) {
	case *irpb.Node_Record:
		return kind.Record.GetNames()
	case *irpb.Node_Group:
		return kind.Group.GetNames()
	case *irpb.Node_Field:
		return kind.Field.GetNames()
	default:
		return nil
	}
}

// originalOf is a node's COBOL name, for a diagnostic about it.
func originalOf(node *irpb.Node) string { return namesOf(node).GetOriginal() }

// namedBy is what a set of names collides as: both of them, so that a collision
// a rename caused names the rename.
func namedBy(names *irpb.Names) colliding {
	return colliding{Original: names.GetOriginal(), Override: names.GetOverrideName()}
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
