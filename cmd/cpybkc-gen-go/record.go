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
// slack nodes among its members, and fillerField is the same for the items
// among them COBOL gave no data-name.
//
// Neither can collide with a member's field name: a member's name is munged to
// an exported identifier and these are not, so the two live in disjoint sets
// however a copybook spells its items — including a copybook with an item
// literally called SLACK, and including the FILLER this one is for.
//
// Two fields rather than one, because a slack node and a FILLER are two
// different facts about a record and docs/ir/SPEC.md keeps them apart: slack is
// bytes that belong to no item, and a FILLER *is* an item — it carries a
// PICTURE and a USAGE, and cpybkc-gen-graph draws it as a row. Retaining both
// in one run of storage would be that collapse in this generator instead of in
// the producer.
const (
	slackField  = "slack"
	fillerField = "filler"
)

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

		name, err := recordName(record.GetNames())
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
// docs/ir/SPEC.md makes record order, and the two kinds of member that get no
// field of their own — the slack nodes and the items COBOL named nothing —
// become the unexported fields at the end.
func (e *emitter) structType(id uint64) (string, error) {
	group, err := e.group(id)
	if err != nil {
		return "", err
	}

	if _, cycle := e.visiting[id]; cycle {
		return "", malformed(fmt.Sprintf("node %d contains itself", id),
			"docs/ir/SPEC.md requires containment to be acyclic, and a record whose group contains itself has no width")
	}

	e.visiting[id] = struct{}{}
	defer delete(e.visiting, id)

	members, err := e.flattened(id)
	if err != nil {
		return "", err
	}

	var (
		fields []string
		named  = make(map[string]colliding)
		slack  int
		filler int
	)

	for _, memberID := range members {
		member, ok := e.nodes[memberID]
		if !ok {
			return "", unresolved(memberID)
		}

		// An item COBOL gave no data-name contributes bytes and no field, the
		// way a slack node does. Which of the two shapes a FILLER takes, and
		// why a group's is the other one, is [emitter.flattened].
		if field := member.GetField(); field != nil && anonymous(field.GetNames()) {
			if _, err := fillerRun(field, group.GetNames().GetOriginal()); err != nil {
				return "", err
			}

			filler++

			continue
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
			arms, err := e.armFields(kind.Variant, group.GetNames().GetOriginal())
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

	if filler > 0 {
		fields = append(fields, fillerDeclaration(filler))
	}

	return "struct {\n" + strings.Join(fields, "\n\n") + "\n}", nil
}

// flattened is the members of the group id as the struct standing for it sees
// them: its own member list, with each member that is a group COBOL gave no
// data-name replaced, in place, by that group's own members.
//
// # A FILLER group is flattened, and a FILLER item is retained as bytes
//
// docs/ir/SPEC.md's "Names" says what a *named* node carries and never that
// every node is named, so an item COBOL gives no data-name — a FILLER — carries
// no names message at all, and a FILLER may be a group as much as an elementary
// item. Neither is a malformed descriptor and neither is refused; what each one
// generates is this generator's decision, and the two decisions are different
// because the two items are.
//
// An elementary FILLER holds no value anybody named, so it gets no exported
// field and its bytes are retained the way a slack node's are — see
// [fillerDeclaration]. That answer is not available to a group: a FILLER group
// holds members, and those members are named. Retaining the group as one run of
// bytes would hide items the copybook does name, which is the whole reason
// there is a second decision to make here at all.
//
// Flattening is what COBOL already says about them. Qualification skips an
// unnamed group — a NOTE-CODE inside a FILLER group is reached as NOTE-CODE OF
// THE-RECORD, because there is no intermediate name to qualify by — so its
// members already belong to the enclosing named level, and putting them there
// is a reading of the copybook rather than a shape invented for Go. A collision
// after it goes through [collisionError] like any other, which is what says so
// when two levels turn out to spell one member the same way.
//
// # What is refused, and why it is not "malformed"
//
// A FILLER group that *repeats* cannot be flattened: its members would have to
// move up a level once per occurrence, and there is no name for the occurrences
// to be an array of. That is a [fillerError] — the item is refused, the
// diagnostic names the item containing it, and it does not call the descriptor
// malformed over something the adopter wrote correctly.
//
// A names message that is *present* and states no original is a different thing
// and keeps the old refusal: that is a named item whose name went missing,
// which is a bug in the producer. [identifier] is where the two part company.
func (e *emitter) flattened(id uint64) ([]uint64, error) {
	group, err := e.group(id)
	if err != nil {
		return nil, err
	}

	return e.flatten(id, group.GetNames().GetOriginal(), map[uint64]struct{}{id: {}})
}

// flatten is [emitter.flattened] over one group, in the name of the named one
// containing it.
func (e *emitter) flatten(id uint64, in string, seen map[uint64]struct{}) ([]uint64, error) {
	group, err := e.group(id)
	if err != nil {
		return nil, err
	}

	var out []uint64

	for _, memberID := range group.GetMemberIds() {
		member, ok := e.nodes[memberID]
		if !ok {
			return nil, unresolved(memberID)
		}

		inner := member.GetGroup()
		if inner == nil || !anonymous(inner.GetNames()) {
			out = append(out, memberID)

			continue
		}

		if inner.GetRepetition() != nil {
			return nil, &fillerError{Kind: "a group", In: in, Because: "repeats"}
		}

		// A group with no name that contains itself would otherwise be expanded
		// for ever, and the expansion happens before [emitter.structType]'s own
		// guard is reached.
		if _, cycle := seen[memberID]; cycle {
			return nil, malformed(fmt.Sprintf("node %d contains itself", memberID),
				"docs/ir/SPEC.md requires containment to be acyclic, and a record whose group contains itself has no width")
		}

		seen[memberID] = struct{}{}

		below, err := e.flatten(memberID, in, seen)
		if err != nil {
			return nil, err
		}

		delete(seen, memberID)

		out = append(out, below...)
	}

	return out, nil
}

// group is the group node id names.
func (e *emitter) group(id uint64) (*irpb.Group, error) {
	node, ok := e.nodes[id]
	if !ok {
		return nil, unresolved(id)
	}

	group := node.GetGroup()
	if group == nil {
		return nil, malformed(fmt.Sprintf("node %d is not a group node, and a group is what stands here", id),
			"a record's top level and a group's group members are group nodes; see docs/ir/SPEC.md, \"The node kinds\"")
	}

	return group, nil
}

// recordName is [identifier] for a record node.
//
// A record is the one node kind docs/ir/SPEC.md requires to be named —
// internal/assemble/validate.go's `names` demands an original on a record and
// on nothing else — so a record carrying no names message is a malformed
// descriptor rather than a FILLER, and this is where that is said. Everywhere
// else, absence of a names message is an item COBOL named nothing and is
// handled; see [identifier].
func recordName(names *irpb.Names) (string, error) {
	if anonymous(names) {
		return "", malformed("a record node carries no name",
			"a record node carries the name the copybook spells, and it is the one node kind that must; see docs/ir/SPEC.md, \"Names\"")
	}

	return identifier("record", names)
}

// groupName is what the copybook calls the group node id.
//
// It is the one thing a diagnostic about a FILLER has to go on — the item has
// no name of its own — so it is read from the node rather than from wherever a
// walk happened to have a name to hand, which is what keeps the same item
// refused with the same words whichever file this generator was emitting.
func (e *emitter) groupName(id uint64) (string, error) {
	group, err := e.group(id)
	if err != nil {
		return "", err
	}

	return group.GetNames().GetOriginal(), nil
}

// anonymous is whether a node's names are those of an item COBOL gave no
// data-name: no names message at all.
//
// The whole of the FILLER decision turns on this being *absence* rather than
// emptiness. A names message that is present and states no original is a named
// item whose name went missing — a producer bug — and it is refused as one; see
// [identifier].
func anonymous(names *irpb.Names) bool { return names == nil }

// fillerRun is the number of bytes retained for an item COBOL gave no name: its
// width, taken as many times as it occurs.
//
// A constant OCCURS is one run of all of it rather than one run per occurrence.
// The occurrences of a FILLER are indistinguishable — nothing names them and
// nothing reads them — so a run each would be storage divided along a line no
// caller can see.
//
// An OCCURS DEPENDING ON is refused. The run would have to be as long as the
// count says, so a writer would have to agree with a count field it does not
// derive, over bytes no caller can supply; that is a promise this generator
// cannot keep, and it says so rather than emitting a record that fails at run
// time.
func fillerRun(f *irpb.Field, in string) (uint32, error) {
	rep := f.GetRepetition()
	if rep == nil {
		return f.GetWidth(), nil
	}

	switch count := rep.GetCount().(type) {
	case *irpb.Repetition_Constant:
		return f.GetWidth() * count.Constant, nil
	case *irpb.Repetition_Variable:
		return 0, &fillerError{Kind: "an item", In: in, Because: "occurs a number of times the record states"}
	default:
		return 0, malformed("an item repeats and says nothing about how many times",
			"a repetition carries a constant count or an OCCURS DEPENDING ON one; an item that does not repeat carries no repetition at all")
	}
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
func (e *emitter) armFields(v *irpb.Variant, in string) ([]armField, error) {
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
		if err := namedArm(body, in); err != nil {
			return nil, err
		}

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

// namedArm refuses an arm whose body is an item COBOL gave no data-name.
//
// This is the third shape a FILLER takes and the one there is no answer for. An
// alternation is spelled as a field per alternative, exactly one of which is
// non-nil in an occurrence, so an alternative with no name has no way to say it
// is the one the record holds — and it cannot be retained as bytes either,
// because its siblings cover the same bytes and one of them is a field a caller
// fills. Refused rather than called malformed: a `FILLER REDEFINES` is
// something a copybook may say, and the answer is in the copybook.
func namedArm(body *irpb.Node, in string) error {
	if !anonymous(namesOf(body)) {
		return nil
	}

	return &fillerError{
		Kind:    "an item",
		In:      in,
		Because: "is one alternative of an alternation over a run of bytes, which this generator spells as a field per alternative",
	}
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

		// An item whose charset axis says no charset governs its bytes is
		// those bytes, and it is the one place in this table where an
		// encoding decides a Go type. A string was the alternative and it
		// loses data twice over: encoding/json writes a []byte as base64, so
		// all 256 byte values survive a caller's own serialisation, while a
		// string holding a byte above 0x7F marshals to a replacement
		// character no viewer can show and a string holding the charset's
		// space loses that byte to the trim ReadAlphanumeric applies. Neither
		// loss is recoverable and neither says anything when it happens. See
		// [opaqueDisplay].
		opaque, err := opaqueDisplay(f)
		if err != nil {
			return "", err
		}

		if opaque {
			return "[]byte", nil
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

// opaqueDisplay reports whether a DISPLAY item's bytes are a payload rather
// than characters, and refuses a descriptor that says they are and says they
// are something else in the same breath.
//
// CHARSET_NONE is the charset axis answering that these bytes do not become
// characters at all. docs/ir/SPEC.md admits that answer on one shape of item —
// a DISPLAY item of the alphanumeric category, which is the PIC X a copybook
// routinely uses to carry a status flag or a hex identifier — and on any other
// DISPLAY item it is a producer bug. It is refused rather than read past
// because the two facts cannot both be honoured: an edited item's storage is
// the edited characters, and a zoned item's digits are read through the
// charset's own digit zone, so a generator that ignored the charset would hand
// back text nothing in the file disagrees with, and one that ignored the
// category would hand back bytes nothing in the file disagrees with either.
//
// A usage other than DISPLAY reaches here and reports false. On those the
// charset is inert — a packed or binary item's bytes are not characters under
// any charset — so a charset of none is ignored there exactly as cp037 is,
// rather than being a second thing to refuse.
func opaqueDisplay(f *irpb.Field) (bool, error) {
	if f.GetUsage() != irpb.Usage_USAGE_DISPLAY {
		return false, nil
	}

	if f.GetEncoding().GetCharset() != irpb.Charset_CHARSET_NONE {
		return false, nil
	}

	if category := f.GetPicture().GetCategory(); category != irpb.Category_CATEGORY_ALPHANUMERIC {
		return false, malformed(
			fmt.Sprintf("%s is a DISPLAY item of the %s category and its charset is none", f.GetNames().GetOriginal(), categoryName(category)),
			"a field carrying no charset MUST be USAGE DISPLAY with an alphanumeric category; see docs/ir/SPEC.md, \"An item with no charset carries bytes, not characters\"")
	}

	return true, nil
}

// resolved refuses a field whose encoding leaves one of its four axes unset.
//
// One of the four decides a Go type and three do not: a charset of none is what
// makes an item's field a []byte rather than a string, and nothing here turns
// on the sign convention, the byte order or the float format. All four are
// checked all the same, because docs/ir/SPEC.md puts the check on every
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

	// The charset is named in the comment only where it is none, and that is
	// not an omission on the other five. Every other charset leaves the field
	// a string holding the characters the item stores, which is what a reader
	// already assumes; none leaves it the bytes, which is the one case where a
	// reader who assumed the usual thing would be wrong about what they may do
	// with the value.
	opaque, err := opaqueDisplay(f)
	if err != nil {
		return "", err
	}

	if opaque {
		summary += "\nThe bytes are the value: no charset governs them, so nothing translates,\ntrims or pads them."
	}

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

// fillerDeclaration is the one unexported field a struct carries for the items
// among its members COBOL gave no data-name.
//
// The same shape as [slackDeclaration] and for a related reason: the bytes are
// part of the record, and nothing in the copybook names them, so they travel
// with the record where the decode and encode methods can reach them and a
// caller cannot. Exporting them would need a name — Filler1, Filler2 — that no
// copybook spells and that moves the moment an unrelated item is inserted ahead
// of it, which is the one thing this generator refuses to produce everywhere
// else.
func fillerDeclaration(runs int) string {
	return fmt.Sprintf(`// %[1]s is the bytes of the items among this item's members that the
// copybook gives no data-name — its FILLER — in the order they occupy the
// record: one run each, as they stood when the record was read, and one set
// of them per occurrence of this struct. A nil run is one the record does
// not carry; an empty run is a run of no bytes, and the two are not the
// same.
//
// A FILLER is an item, and it is one no program names: it holds no value a
// caller of this package could set and none it could read. So it travels
// with the record and there is nothing here for a caller to do. An item you
// do want to read or write is one to give a data-name in the copybook,
// which makes it a field like any other.
%[1]s [%[2]d][]byte`, fillerField, runs)
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

	for line := range strings.SplitSeq(text, "\n") {
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
// # The two descriptors this used to collapse
//
// `names.GetOriginal() == ""` is true of two different descriptors, and telling
// them apart is this function's own job rather than a convention its callers are
// trusted to observe:
//
//   - **No names message at all.** An item COBOL gave no data-name, which is a
//     FILLER: legal, common, and generated rather than refused. A caller that
//     may meet one asks [anonymous] first and never arrives here, so reaching
//     this function with one is a bug in *this generator* — and it says so, in
//     those words, rather than sending an adopter after a producer bug in an
//     item they wrote correctly. That misdirection is the whole of what this
//     story was opened about, and leaving it to an unenforced convention is how
//     it would come back.
//   - **A names message stating no original.** A node the copybook named and
//     the descriptor did not, which is a bug in the producer and is reported as
//     the malformed descriptor it is.
//
// A record node is required to be named (internal/assemble/validate.go) and so
// is never asked for through here — [recordName] is its way in, and it turns
// the first case back into the descriptor fault it is for that node kind.
func identifier(kind string, names *irpb.Names) (string, error) {
	if anonymous(names) {
		return "", fmt.Errorf(
			"a %s the copybook gives no data-name reached the part of %s that names things, which is a bug in %s rather than in the descriptor or in your copybook",
			kind, pluginName, pluginName)
	}

	original := names.GetOriginal()

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

	return !upper || !lower
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
