// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package assemble

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	cobol "github.com/Zaba505/cobol-go"
	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/emit"
	"github.com/Zaba505/cpybkc/internal/layout"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
	"github.com/Zaba505/cpybkc/internal/resolve"
	"github.com/Zaba505/cpybkc/irpb"
)

// Every test here drives the real layout parser, the real copybook reader and
// the real resolver, so that no test asserts a descriptor assembled out of a
// model those readers would never have produced. What a test writes is a layout
// and one copybook per record, and what it asserts is the whole descriptor
// rendered — one line per node — because what has to be right is not one member
// but which node carries it and which identifier it was given.

// The three copybooks of docs/ir/SPEC.md's "A counted run, as nodes": a header
// carrying a detail count and a flag, the details the count governs, and the
// summary the flag governs.
const (
	header = `01 HDR-REC.
   05 HDR-TYPE PIC X(1).
   05 DTL-COUNT PIC 9(2).
   05 SUM-FLAG PIC X(1).
`

	detail = `01 DTL-REC.
   05 DTL-TYPE PIC X(1).
   05 DTL-BODY PIC X(20).
`

	summary = `01 SUM-REC.
   05 SUM-TYPE PIC X(1).
   05 SUM-TOTAL PIC S9(7).
`
)

// countedRun is that appendix's layout: the records, what tells each apart, and
// the expression saying any number of counted groups make a file.
const countedRun = `(framing (recfm FB) (lrecl 24))
(encoding (charset cp037) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))
(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(record SUMMARY (copybook "sum.cpy" SUM-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(discriminate SUMMARY (equals (item SUMMARY SUM-TYPE) "S"))
(sequence
  (+ (seq HEADER
          (times DETAIL (item HEADER DTL-COUNT))
          (when (item HEADER SUM-FLAG) "Y" SUMMARY))))`

// countedRunCopybooks binds those three record names to those three copybooks.
func countedRunCopybooks() map[string]string {
	return map[string]string{"HEADER": header, "DETAIL": detail, "SUMMARY": summary}
}

// recordOf builds the first record of a copybook source, driving `cobol-go`'s
// real parser so that no test hand-assembles an AST the reader would never
// produce.
func recordOf(t *testing.T, src string) *copybook.Field {
	t.Helper()

	file, err := cobol.Parse(strings.NewReader(src), cobol.WithFragment())
	if err != nil {
		t.Fatalf("parsing the copybook: %v", err)
	}
	if file.Fragment == nil {
		t.Fatal("parsing the copybook: no fragment")
	}

	records, err := copybook.Build(file.Fragment.Entries)
	if err != nil {
		t.Fatalf("building the copybook: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("building the copybook: no records")
	}

	return records[0]
}

// fieldNamed finds a field of a record by name, for a test naming what a layout
// would name.
func fieldNamed(t *testing.T, record *copybook.Field, name string) *copybook.Field {
	t.Helper()

	var found *copybook.Field

	var walk func(*copybook.Field)
	walk = func(field *copybook.Field) {
		if found == nil && !field.Filler && field.Name == name {
			found = field
		}

		for _, child := range field.Children {
			walk(child)
		}
	}
	walk(record)

	if found == nil {
		t.Fatalf("the copybook declares no %s", name)
	}

	return found
}

// options is the whole pipeline in front of this package: parse the layout, read
// every layer, resolve each record's copybook against them, compile the
// sequencing expression, and hand the lot over.
//
// It is one function rather than a fixture per test because what this package
// assembles is the *output* of that pipeline, and a fixture would be a second
// answer to what the pipeline produces.
func options(t *testing.T, source string, copybooks map[string]string, redefines ...redefiner) Options {
	t.Helper()

	file, err := layout.Parse("layout.sexpr", strings.NewReader(source))
	if err != nil {
		t.Fatalf("parsing the layout: %v", err)
	}

	framing, err := layoutmodel.ReadFraming(file)
	if err != nil {
		t.Fatalf("reading the framing layer: %v", err)
	}

	profile, err := layoutmodel.ReadProfile(file)
	if err != nil {
		t.Fatalf("reading the encoding layer: %v", err)
	}

	sequence, err := layoutmodel.ReadSequence(file)
	if err != nil {
		t.Fatalf("reading the sequencing layer: %v", err)
	}

	discrimination, err := layoutmodel.ReadDiscrimination(file)
	if err != nil {
		t.Fatalf("reading the discrimination layer: %v", err)
	}

	opts := Options{Framing: framing}

	sequencing := resolve.Sequencing{
		Sequence: sequence,
		Dialect:  copybook.IBMEnterprise(),
		Reading:  layoutmodel.ODOSlide,
		Encoding: profile.Axes,
	}

	for _, discriminator := range discrimination.Records {
		src, bound := copybooks[discriminator.Record]
		if !bound {
			t.Fatalf("the test binds no copybook to record %s", discriminator.Record)
		}

		path := strings.ToLower(discriminator.Record) + ".cpy"
		record := recordOf(t, src)

		sequencing.Records = append(sequencing.Records, resolve.SequencedRecord{
			Name:          discriminator.Record,
			Copybook:      path,
			Item:          record,
			Discriminator: discriminator.Strategy,
		})

		resolved, err := resolve.Resolve(record, resolve.Options{
			Copybook:          path,
			Dialect:           copybook.IBMEnterprise(),
			Framing:           framing,
			Reading:           layoutmodel.ODOSlide,
			Encoding:          profile.Axes,
			EncodingOverrides: overriding(t, profile, discriminator.Record, record),
			Redefines:         stated(t, redefines, discriminator.Record, record),
		})
		if err != nil {
			t.Fatalf("resolving %s: %v", discriminator.Record, err)
		}
		if len(resolved) != 1 {
			t.Fatalf("%s resolved to %d record types, want 1", discriminator.Record, len(resolved))
		}

		opts.Records = append(opts.Records, Record{
			Name:     discriminator.Record,
			Copybook: path,
			Resolved: resolved[0],
		})
	}

	automaton, err := resolve.CompileSequence(sequencing)
	if err != nil {
		t.Fatalf("compiling the sequence: %v", err)
	}

	opts.Automaton = automaton

	return opts
}

// overriding is the layout's `encoding-override` forms that name items of one
// record, resolved against the copybook the pipeline built.
//
// Resolving an item reference is `project`'s in the shipped pipeline, and this
// is the smallest thing that stands in for it: each name of the path is
// resolved under the item the name before it found, outermost first. A
// reference naming another record is left alone rather than failing, because an
// override may name any record in the layout and only one is being resolved
// here.
func overriding(t *testing.T, profile *layoutmodel.Profile, name string, record *copybook.Field) []resolve.EncodingOverride {
	t.Helper()

	var overrides []resolve.EncodingOverride

	for _, override := range profile.Overrides {
		if override.Item.Record != name {
			continue
		}

		item := record

		for _, step := range override.Item.Path {
			item = fieldNamed(t, item, step)
		}

		overrides = append(overrides, resolve.EncodingOverride{
			Pos:  override.Pos,
			Item: item,
			Axes: override.Axes,
		})
	}

	return overrides
}

// redefiner states a test's redefines against the copybook the pipeline built,
// rather than against one the test parsed for itself.
//
// A [github.com/Zaba505/cpybkc/internal/resolve.Redefine] names a
// [github.com/Zaba505/cobol-go/copybook.Field], and two reads of one copybook
// produce two of them, so a test stating one against its own read would state it
// against a record nobody resolved.
type redefiner func(t *testing.T, name string, record *copybook.Field) []resolve.Redefine

// stated is what the redefiners a test handed over say about one record.
func stated(t *testing.T, redefiners []redefiner, name string, record *copybook.Field) []resolve.Redefine {
	t.Helper()

	var redefines []resolve.Redefine

	for _, state := range redefiners {
		redefines = append(redefines, state(t, name, record)...)
	}

	return redefines
}

// assembled is a layout the caller expects to assemble.
func assembled(t *testing.T, source string, copybooks map[string]string, redefines ...redefiner) *irpb.Descriptor {
	t.Helper()

	descriptor, err := Assemble(options(t, source, copybooks, redefines...))
	if err != nil {
		t.Fatalf("assembling the descriptor: %v", err)
	}

	return descriptor
}

// render draws the whole descriptor: its version, then one line per node.
//
// A rendering rather than a struct literal, for the reason the automaton's tests
// give: what has to be right is every member *and* the identifier carrying it,
// and a rendering fails with the whole descriptor in the message.
func render(d *irpb.Descriptor) string {
	var b strings.Builder

	fmt.Fprintf(&b, "version %d\n", d.GetVersion())

	for _, node := range d.GetNodes() {
		fmt.Fprintf(&b, "%d %s\n", node.GetId(), renderNode(node))
	}

	return b.String()
}

// renderNode draws one node: its kind and every member it carries.
func renderNode(node *irpb.Node) string {
	switch body := node.GetKind().(type) {
	case *irpb.Node_File:
		return "file " + renderFraming(body.File) + " start=" + id(body.File.GetStartStateId())
	case *irpb.Node_Record:
		return "record " + renderNames(body.Record.GetNames()) + " root=" + id(body.Record.GetRootId())
	case *irpb.Node_Group:
		return "group " + renderNames(body.Group.GetNames()) +
			" members=[" + ids(body.Group.GetMemberIds()) + "]" + renderRepetition(body.Group.GetRepetition())
	case *irpb.Node_Variant:
		return "variant " + renderArms(body.Variant.GetArms())
	case *irpb.Node_Field:
		return "field " + renderNames(body.Field.GetNames()) +
			" width=" + strconv.Itoa(int(body.Field.GetWidth())) +
			" " + spell(body.Field.GetUsage(), "USAGE_") +
			" " + renderEncoding(body.Field.GetEncoding()) +
			renderPicture(body.Field.GetPicture()) +
			renderRepetition(body.Field.GetRepetition())
	case *irpb.Node_Slack:
		return "slack width=" + strconv.Itoa(int(body.Slack.GetWidth()))
	case *irpb.Node_Predicate:
		return "predicate field=" + id(body.Predicate.GetFieldId()) + " " + renderTest(body.Predicate)
	case *irpb.Node_State:
		state := "state"
		if body.State.GetAccepts() {
			state += " accepts"
		}
		if guards := body.State.GetAcceptanceGuardIds(); len(guards) > 0 {
			state += " when=[" + ids(guards) + "]"
		}

		return state + " transitions=[" + ids(body.State.GetTransitionIds()) + "]"
	case *irpb.Node_Transition:
		line := "transition record=" + id(body.Transition.GetRecordId()) +
			" to=" + id(body.Transition.GetNextStateId())
		if body.Transition.PredicateId != nil {
			line += " predicate=" + id(body.Transition.GetPredicateId())
		}
		if guards := body.Transition.GetGuardIds(); len(guards) > 0 {
			line += " guards=[" + ids(guards) + "]"
		}
		if bindings := body.Transition.GetBindingIds(); len(bindings) > 0 {
			line += " bindings=[" + ids(bindings) + "]"
		}

		return line
	case *irpb.Node_Register:
		return "register " + spell(body.Register.GetKind(), "REGISTER_KIND_")
	case *irpb.Node_Binding:
		line := "binding register=" + id(body.Binding.GetRegisterId())
		if field, reads := body.Binding.GetValue().(*irpb.Binding_FieldId); reads {
			return line + " field=" + id(field.FieldId)
		}

		return line + " less-one"
	case *irpb.Node_Guard:
		return "guard register=" + id(body.Guard.GetRegisterId()) + " " + renderGuardTest(body.Guard)
	}

	return "kindless"
}

func renderFraming(file *irpb.File) string {
	switch framing := file.GetFraming().(type) {
	case *irpb.File_Unframed:
		return "unframed"
	case *irpb.File_DescriptorWord:
		return "descriptor-word"
	case *irpb.File_Segmented:
		return "segmented max-segment=" + strconv.Itoa(int(framing.Segmented.GetMaxSegmentSize()))
	case *irpb.File_Delimited:
		return "delimited delimiter=" + quoteBytes(framing.Delimited.GetDelimiter()) +
			" " + spell(framing.Delimited.GetPlacement(), "DELIMITER_PLACEMENT_")
	}

	return "unframed?"
}

func renderNames(names *irpb.Names) string {
	if names == nil {
		return "(unnamed)"
	}

	if names.OverrideName != nil {
		return names.GetOriginal() + "->" + names.GetOverrideName()
	}

	return names.GetOriginal()
}

func renderEncoding(encoding *irpb.Encoding) string {
	return spell(encoding.GetCharset(), "CHARSET_") + "/" +
		spell(encoding.GetSignConvention(), "SIGN_CONVENTION_") + "/" +
		spell(encoding.GetByteOrder(), "BYTE_ORDER_") + "/" +
		spell(encoding.GetFloatFormat(), "FLOAT_FORMAT_")
}

func renderPicture(picture *irpb.Picture) string {
	if picture == nil {
		return ""
	}

	rendered := fmt.Sprintf(" %s(%d,%d)",
		spell(picture.GetCategory(), "CATEGORY_"), picture.GetDigits(), picture.GetScale())

	if picture.GetSigned() {
		rendered += " signed " + spell(picture.GetSignPosition(), "SIGN_POSITION_")
	}

	return rendered
}

func renderRepetition(repetition *irpb.Repetition) string {
	switch count := repetition.GetCount().(type) {
	case *irpb.Repetition_Constant:
		return " occurs=" + strconv.Itoa(int(count.Constant))
	case *irpb.Repetition_Variable:
		where := "?"
		switch read := count.Variable.GetCount().(type) {
		case *irpb.VariableCount_FieldId:
			where = "field=" + id(read.FieldId)
		case *irpb.VariableCount_RegisterId:
			where = "register=" + id(read.RegisterId)
		}

		return fmt.Sprintf(" occurs=%d..%d depending-on %s",
			count.Variable.GetMinOccurrences(), count.Variable.GetMaxOccurrences(), where)
	}

	return ""
}

func renderArms(arms []*irpb.Arm) string {
	rendered := make([]string, 0, len(arms))

	for _, arm := range arms {
		body := "?"
		switch held := arm.GetBody().(type) {
		case *irpb.Arm_GroupId:
			body = "group=" + id(held.GroupId)
		case *irpb.Arm_FieldId:
			body = "field=" + id(held.FieldId)
		}

		rendered = append(rendered, "arm(predicate="+id(arm.GetPredicateId())+" "+body+")")
	}

	return strings.Join(rendered, " ")
}

func renderTest(predicate *irpb.Predicate) string {
	switch test := predicate.GetTest().(type) {
	case *irpb.Predicate_BytesEqual:
		return "equals " + quoteBytes(test.BytesEqual.GetValue())
	case *irpb.Predicate_BytesOneOf:
		values := make([]string, 0, len(test.BytesOneOf.GetValues()))
		for _, value := range test.BytesOneOf.GetValues() {
			values = append(values, quoteBytes(value))
		}

		return "one-of " + strings.Join(values, " ")
	}

	return "no test"
}

func renderGuardTest(guard *irpb.Guard) string {
	switch test := guard.GetTest().(type) {
	case *irpb.Guard_Equals:
		return "equals " + renderLiteral(test.Equals)
	case *irpb.Guard_OneOf:
		values := make([]string, 0, len(test.OneOf.GetValues()))
		for _, value := range test.OneOf.GetValues() {
			values = append(values, renderLiteral(value))
		}

		return "one-of " + strings.Join(values, " ")
	case *irpb.Guard_GreaterThanZero:
		return "greater-than-zero"
	}

	return "no test"
}

func renderLiteral(literal *irpb.Literal) string {
	switch value := literal.GetValue().(type) {
	case *irpb.Literal_BytesValue:
		return quoteBytes(value.BytesValue)
	case *irpb.Literal_Integer:
		return strconv.FormatInt(value.Integer, 10)
	}

	return "?"
}

// spell renders one member of a closed set the way the layout format spells it:
// the schema's prefix dropped, lower case, and hyphens where protobuf's naming
// convention puts underscores.
func spell(value fmt.Stringer, prefix string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(value.String(), prefix)), "_", "-")
}

func id(of uint64) string { return strconv.FormatUint(of, 10) }

func ids(of []uint64) string {
	rendered := make([]string, 0, len(of))
	for _, one := range of {
		rendered = append(rendered, id(one))
	}

	return strings.Join(rendered, " ")
}

// equal fails with both renderings where they differ, which is what makes a
// golden over a whole descriptor debuggable.
func equal(t *testing.T, got, want string) {
	t.Helper()

	if strings.TrimSpace(got) == strings.TrimSpace(want) {
		return
	}

	t.Errorf("the descriptor is not what was expected.\ngot:\n%s\nwant:\n%s", got, want)
}

// TestADescriptorCarriesEveryLayerOfTheLayout is the whole of this package in
// one assertion: three copybooks, a framing, an encoding profile and a
// sequencing expression, in one message with identifiers on everything.
//
// The golden is the whole descriptor because the traversal is what this test is
// about. Every identifier in it is a position in the node list, and a change to
// the order nodes are met in moves every number after it — which is exactly the
// change docs/ir/SPEC.md's "Identity, ordering and determinism" makes a
// producer state once and keep.
func TestADescriptorCarriesEveryLayerOfTheLayout(t *testing.T) {
	descriptor := assembled(t, countedRun, countedRunCopybooks())

	equal(t, render(descriptor), `version 1
0 file unframed start=19
1 record HDR-REC root=2
2 group HDR-REC members=[3 4 5 6]
3 field HDR-TYPE width=1 display cp037/ebcdic/big-endian/ibm-hfp alphanumeric(0,0)
4 field DTL-COUNT width=2 display cp037/ebcdic/big-endian/ibm-hfp numeric(2,0)
5 field SUM-FLAG width=1 display cp037/ebcdic/big-endian/ibm-hfp alphanumeric(0,0)
6 slack width=20
7 record DTL-REC root=8
8 group DTL-REC members=[9 10 11]
9 field DTL-TYPE width=1 display cp037/ebcdic/big-endian/ibm-hfp alphanumeric(0,0)
10 field DTL-BODY width=20 display cp037/ebcdic/big-endian/ibm-hfp alphanumeric(0,0)
11 slack width=3
12 record SUM-REC root=13
13 group SUM-REC members=[14 15 16]
14 field SUM-TYPE width=1 display cp037/ebcdic/big-endian/ibm-hfp alphanumeric(0,0)
15 field SUM-TOTAL width=7 display cp037/ebcdic/big-endian/ibm-hfp numeric(7,0) signed trailing
16 slack width=16
17 register integer
18 register bytes
19 state transitions=[23]
20 state accepts when=[27] transitions=[28 32 36]
21 state accepts when=[40] transitions=[41 44 47]
22 state accepts transitions=[51]
23 transition record=1 to=20 predicate=24 bindings=[25 26]
24 predicate field=3 equals 0xc8
25 binding register=17 field=4
26 binding register=18 field=5
27 guard register=17 equals 0
28 transition record=7 to=21 predicate=29 guards=[30] bindings=[31]
29 predicate field=9 equals 0xc4
30 guard register=17 greater-than-zero
31 binding register=17 less-one
32 transition record=12 to=22 predicate=33 guards=[34 35]
33 predicate field=14 equals 0xe2
34 guard register=17 equals 0
35 guard register=18 equals 0xe8
36 transition record=1 to=20 predicate=24 guards=[37] bindings=[38 39]
37 guard register=17 equals 0
38 binding register=17 field=4
39 binding register=18 field=5
40 guard register=17 equals 0
41 transition record=7 to=21 predicate=29 guards=[42] bindings=[43]
42 guard register=17 greater-than-zero
43 binding register=17 less-one
44 transition record=12 to=22 predicate=33 guards=[45 46]
45 guard register=17 equals 0
46 guard register=18 equals 0xe8
47 transition record=1 to=20 predicate=24 guards=[48] bindings=[49 50]
48 guard register=17 equals 0
49 binding register=17 field=4
50 binding register=18 field=5
51 transition record=1 to=20 predicate=24 bindings=[52 53]
52 binding register=17 field=4
53 binding register=18 field=5
`)
}

// TestIdenticalInputsProduceByteIdenticalIR is the promise the whole traversal
// exists for, run end to end: two independent passes over the same layout, each
// building its own copybooks, its own resolved records and its own automaton,
// come out as the same bytes.
//
// The two halves are in two places and this is where they meet. The identifiers
// and the order of the node list are this package's (docs/ir/SPEC.md, "Identity,
// ordering and determinism"); the encoding is
// [github.com/Zaba505/cpybkc/internal/emit.Marshal]'s. Asserting over the bytes
// rather than over the message is what makes it the promise a consumer diffing
// two emissions actually holds.
func TestIdenticalInputsProduceByteIdenticalIR(t *testing.T) {
	first, err := emit.Marshal(assembled(t, countedRun, countedRunCopybooks()))
	if err != nil {
		t.Fatalf("encoding the first descriptor: %v", err)
	}

	second, err := emit.Marshal(assembled(t, countedRun, countedRunCopybooks()))
	if err != nil {
		t.Fatalf("encoding the second descriptor: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Errorf("two assemblies of one layout encoded differently:\n%x\n%x", first, second)
	}
}

// TestNoDefaultSurvivesIntoTheDescriptor walks every node for a member left at
// the unspecified zero of its closed set.
//
// [Validate] already refuses one, and [Assemble] runs it — so this asserts the
// property directly rather than through the pass that enforces it, because a
// pass and the thing it enforces failing together is the one way a green suite
// says nothing.
func TestNoDefaultSurvivesIntoTheDescriptor(t *testing.T) {
	descriptor := assembled(t, countedRun, countedRunCopybooks())

	for _, node := range descriptor.GetNodes() {
		field := node.GetField()
		if field == nil {
			continue
		}

		if missing := unresolved(field.GetEncoding()); len(missing) > 0 {
			t.Errorf("field node %d states no %s", node.GetId(), list(missing))
		}

		if field.GetUsage() == irpb.Usage_USAGE_UNSPECIFIED {
			t.Errorf("field node %d states no USAGE", node.GetId())
		}

		if picture := field.GetPicture(); picture != nil &&
			picture.GetCategory() == irpb.Category_CATEGORY_UNSPECIFIED {
			t.Errorf("field node %d carries a PICTURE of no category", node.GetId())
		}
	}

	for _, node := range descriptor.GetNodes() {
		if register := node.GetRegister(); register != nil &&
			register.GetKind() == irpb.RegisterKind_REGISTER_KIND_UNSPECIFIED {
			t.Errorf("register node %d does not say what it holds", node.GetId())
		}
	}
}

// TestAnOverrideStandsBesideTheOriginal holds a rename to substituting a name
// and keeping the copybook's, so that generated code can still point back at the
// copybook it came from.
func TestAnOverrideStandsBesideTheOriginal(t *testing.T) {
	opts := options(t, countedRun, countedRunCopybooks())

	item := fieldNamed(t, opts.Records[0].Resolved.Item, "DTL-COUNT")
	opts.Renames = []Rename{{Record: opts.Records[0].Name, Item: item, Substitute: "detail_count"}}

	descriptor, err := Assemble(opts)
	if err != nil {
		t.Fatalf("assembling the descriptor: %v", err)
	}

	names := namesOf(t, descriptor, "DTL-COUNT")
	if names.GetOriginal() != "DTL-COUNT" {
		t.Errorf("the original name is %q, want DTL-COUNT", names.GetOriginal())
	}

	if names.GetOverrideName() != "detail_count" {
		t.Errorf("the override is %q, want detail_count", names.GetOverrideName())
	}
}

// TestARenameReachesOneItemAndNoOther holds a rename to being about the item it
// names and no other, under the one record type it was written for.
func TestARenameReachesOneItemAndNoOther(t *testing.T) {
	opts := options(t, countedRun, countedRunCopybooks())
	opts.Renames = []Rename{{
		Record:     opts.Records[0].Name,
		Item:       fieldNamed(t, opts.Records[0].Resolved.Item, "HDR-TYPE"),
		Substitute: "kind",
	}}

	descriptor, err := Assemble(opts)
	if err != nil {
		t.Fatalf("assembling the descriptor: %v", err)
	}

	for _, name := range []string{"DTL-TYPE", "SUM-TYPE", "DTL-COUNT"} {
		if names := namesOf(t, descriptor, name); names.OverrideName != nil {
			t.Errorf("%s carries the override %q, and the rename named HDR-TYPE", name, names.GetOverrideName())
		}
	}
}

// namesOf finds the names of the one field node standing for a copybook item.
func namesOf(t *testing.T, d *irpb.Descriptor, original string) *irpb.Names {
	t.Helper()

	var found *irpb.Names

	for _, node := range d.GetNodes() {
		names := node.GetField().GetNames()
		if names.GetOriginal() != original {
			continue
		}

		if found != nil {
			t.Fatalf("two field nodes are named %s", original)
		}

		found = names
	}

	if found == nil {
		t.Fatalf("no field node is named %s", original)
	}

	return found
}

// TestAFramingReachesTheFileNode holds each of the layout's record formats to
// the physical shape docs/ir/SPEC.md's "Four framings, and none of them is a
// RECFM" maps it to, and holds what a framing carries beside the shape to
// reaching the node with it.
func TestAFramingReachesTheFileNode(t *testing.T) {
	tests := []struct {
		name    string
		framing string
		want    string
	}{
		{
			name:    "F is unframed",
			framing: `(framing (recfm F) (lrecl 24))`,
			want:    "unframed",
		},
		{
			name:    "V is a descriptor word",
			framing: `(framing (recfm V) (lrecl 24))`,
			want:    "descriptor-word",
		},
		{
			name:    "VBS is segmented, and carries the one size a framing carries",
			framing: `(framing (recfm VBS) (lrecl 24) (blksize 512) (max-segment 200))`,
			want:    "segmented max-segment=200",
		},
		{
			name:    "line sequential is delimited, and carries the delimiter as bytes",
			framing: `(framing (recfm line-sequential) (delimiter (bytes "0A")) (placement separator))`,
			want:    "delimited delimiter=0x0a separator",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := strings.Replace(countedRun, `(framing (recfm FB) (lrecl 24))`, test.framing, 1)

			descriptor := assembled(t, source, countedRunCopybooks())

			if got := renderFraming(descriptor.GetNodes()[0].GetFile()); got != test.want {
				t.Errorf("the file node states %q, want %q", got, test.want)
			}
		})
	}
}

// TestATableCarriesItsCountAsAReference holds an OCCURS DEPENDING ON to
// resolving into a repetition whose count is the identifier of the field the
// clause names, with the copybook's own bounds beside it.
func TestATableCarriesItsCountAsAReference(t *testing.T) {
	const orders = `01 ORD-REC.
   05 ORD-TYPE PIC X(1).
   05 ORD-COUNT PIC 9(2).
   05 ORD-LINE OCCURS 1 TO 5 TIMES DEPENDING ON ORD-COUNT.
      10 ORD-SKU PIC X(4).
`

	const source = `(framing (recfm V) (lrecl 100))
(encoding (charset cp037) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))
(copybook-reading (occurs-depending-on odoslide))
(record ORDER (copybook "ord.cpy" ORD-REC))
(discriminate ORDER (equals (item ORDER ORD-TYPE) "O"))
(sequence (+ ORDER))`

	descriptor := assembled(t, source, map[string]string{"ORDER": orders})

	equal(t, render(descriptor), `version 1
0 file descriptor-word start=7
1 record ORD-REC root=2
2 group ORD-REC members=[3 4 5]
3 field ORD-TYPE width=1 display cp037/ebcdic/big-endian/ibm-hfp alphanumeric(0,0)
4 field ORD-COUNT width=2 display cp037/ebcdic/big-endian/ibm-hfp numeric(2,0)
5 group ORD-LINE members=[6] occurs=1..5 depending-on field=4
6 field ORD-SKU width=4 display cp037/ebcdic/big-endian/ibm-hfp alphanumeric(0,0)
7 state transitions=[9]
8 state accepts transitions=[11]
9 transition record=1 to=8 predicate=10
10 predicate field=3 equals 0xd6
11 transition record=1 to=8 predicate=10
`)
}

// TestAVariantBecomesArmsAndPredicates holds a REDEFINES inside a repeating
// group to reaching the descriptor as the one alternation resolution does not
// split into record types: an ordered list of arms, each naming the predicate
// that selects it and the item that is its body.
func TestAVariantBecomesArmsAndPredicates(t *testing.T) {
	const entries = `01 ENT-REC.
   05 ENT-TYPE PIC X(1).
   05 ENT-LINE OCCURS 3 TIMES.
      10 ENT-KIND PIC X(1).
      10 ENT-CASH PIC 9(4).
      10 ENT-NOTE REDEFINES ENT-CASH PIC X(4).
`

	const source = `(framing (recfm V) (lrecl 100))
(encoding (charset cp037) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))
(record ENTRY (copybook "ent.cpy" ENT-REC))
(discriminate ENTRY (equals (item ENTRY ENT-TYPE) "E"))
(sequence (+ ENTRY))`

	descriptor := assembled(t, source, map[string]string{"ENTRY": entries},
		func(t *testing.T, name string, record *copybook.Field) []resolve.Redefine {
			t.Helper()

			return []resolve.Redefine{{
				Item: fieldNamed(t, record, "ENT-CASH"),
				Alternatives: []resolve.Alternative{
					{Name: "ENT-CASH", Predicate: selects("C", "ENT-LINE", "ENT-KIND")},
					{Name: "ENT-NOTE", Predicate: selects("N", "ENT-LINE", "ENT-KIND")},
				},
			}}
		})

	equal(t, render(descriptor), `version 1
0 file descriptor-word start=11
1 record ENT-REC root=2
2 group ENT-REC members=[3 4]
3 field ENT-TYPE width=1 display cp037/ebcdic/big-endian/ibm-hfp alphanumeric(0,0)
4 group ENT-LINE members=[5 6] occurs=3
5 field ENT-KIND width=1 display cp037/ebcdic/big-endian/ibm-hfp alphanumeric(0,0)
6 variant arm(predicate=7 field=8) arm(predicate=9 field=10)
7 predicate field=5 equals 0xc3
8 field ENT-CASH width=4 display cp037/ebcdic/big-endian/ibm-hfp numeric(4,0)
9 predicate field=5 equals 0xd5
10 field ENT-NOTE width=4 display cp037/ebcdic/big-endian/ibm-hfp alphanumeric(0,0)
11 state transitions=[13]
12 state accepts transitions=[15]
13 transition record=1 to=12 predicate=14
14 predicate field=3 equals 0xc5
15 transition record=1 to=12 predicate=14
`)
}

// selects is an `equals` strategy over an item of a record, for a test that
// needs an arm selected by something rather than by a particular thing.
func selects(value string, path ...string) layoutmodel.Strategy {
	return layoutmodel.Strategy{
		Kind:     layoutmodel.Equals,
		Item:     layoutmodel.ItemRef{Path: path},
		Literals: []layoutmodel.Literal{{Kind: layoutmodel.TextLiteral, Text: value}},
	}
}

// TestARecordNothingAdmitsIsRefused holds the descriptor to every record type
// hanging off the file node.
func TestARecordNothingAdmitsIsRefused(t *testing.T) {
	opts := options(t, countedRun, countedRunCopybooks())
	opts.Records = append(opts.Records, Record{
		Name:     "ORPHAN",
		Copybook: "orphan.cpy",
		Resolved: opts.Records[0].Resolved,
	})

	_, err := Assemble(opts)
	if err == nil {
		t.Fatal("the descriptor assembled, want a fault")
	}

	var fault *NodeError
	if !errors.As(err, &fault) {
		t.Fatalf("the fault is %v, want a NodeError about an unreachable node", err)
	}

	if !strings.Contains(err.Error(), "not reachable from the file node") {
		t.Errorf("the fault reads %q, and does not say what is wrong", err)
	}
}

// TestTwoRecordTypesUnderOneNameAreRefused holds a name to being what a
// transition says which record it admits by.
func TestTwoRecordTypesUnderOneNameAreRefused(t *testing.T) {
	opts := options(t, countedRun, countedRunCopybooks())
	opts.Records = append(opts.Records, Record{
		Name:     "HEADER",
		Copybook: "twice.cpy",
		Resolved: opts.Records[1].Resolved,
	})

	_, err := Assemble(opts)

	var fault *DuplicateRecordError
	if !errors.As(err, &fault) {
		t.Fatalf("the fault is %v, want a DuplicateRecordError", err)
	}

	if fault.Record != "HEADER" || fault.Copybook != "twice.cpy" {
		t.Errorf("the fault names %s in %s, want HEADER in twice.cpy", fault.Record, fault.Copybook)
	}
}

// TestATransitionAdmittingAnUnknownRecordIsRefused holds every transition to
// admitting a record type the descriptor carries.
func TestATransitionAdmittingAnUnknownRecordIsRefused(t *testing.T) {
	opts := options(t, countedRun, countedRunCopybooks())
	opts.Records = slices.Delete(opts.Records, 2, 3)

	_, err := Assemble(opts)

	var fault *UnknownRecordError
	if !errors.As(err, &fault) {
		t.Fatalf("the fault is %v, want an UnknownRecordError", err)
	}

	if fault.Record != "SUMMARY" {
		t.Errorf("the fault names %s, want SUMMARY", fault.Record)
	}

	if !slices.Equal(fault.Defined, []string{"HEADER", "DETAIL"}) {
		t.Errorf("the fault lists %v, want the two record types that were handed over", fault.Defined)
	}
}

// TestAnIncompleteLayoutIsRefusedBeforeAnythingIsAssembled holds the three
// members a descriptor cannot be built without to being required rather than
// defaulted.
func TestAnIncompleteLayoutIsRefusedBeforeAnythingIsAssembled(t *testing.T) {
	tests := []struct {
		name  string
		spoil func(Options) Options
		want  error
	}{
		{
			name:  "no framing",
			spoil: func(opts Options) Options { opts.Framing = nil; return opts },
			want:  ErrNoFraming,
		},
		{
			name:  "no automaton",
			spoil: func(opts Options) Options { opts.Automaton = nil; return opts },
			want:  ErrNoAutomaton,
		},
		{
			name:  "no record types",
			spoil: func(opts Options) Options { opts.Records = nil; return opts },
			want:  ErrNoRecords,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Assemble(test.spoil(options(t, countedRun, countedRunCopybooks())))
			if !errors.Is(err, test.want) {
				t.Errorf("the fault is %v, want %v", err, test.want)
			}
		})
	}
}

// TestASignedDisplayItemCarriesWhereItsSignIs holds the SIGN clause to reaching
// the descriptor, inherited the way USAGE is, and to being unspecified where the
// question does not arise.
func TestASignedDisplayItemCarriesWhereItsSignIs(t *testing.T) {
	const signs = `01 SGN-REC.
   05 SGN-TYPE PIC X(1).
   05 SGN-GROUP SIGN IS LEADING SEPARATE CHARACTER.
      10 SGN-ONE PIC S9(3).
      10 SGN-TWO PIC S9(3) SIGN IS TRAILING.
   05 SGN-PLAIN PIC S9(3).
   05 SGN-COMP PIC S9(4) COMP-3.
   05 SGN-CHAR PIC X(2).
`

	const source = `(framing (recfm V) (lrecl 100))
(encoding (charset cp037) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))
(record SIGNS (copybook "sgn.cpy" SGN-REC))
(discriminate SIGNS (equals (item SIGNS SGN-TYPE) "S"))
(sequence (+ SIGNS))`

	descriptor := assembled(t, source, map[string]string{"SIGNS": signs})

	want := map[string]string{
		"SGN-ONE":   "leading-separate",
		"SGN-TWO":   "trailing",
		"SGN-PLAIN": "trailing",
		"SGN-COMP":  "unspecified",
		"SGN-CHAR":  "unspecified",
	}

	for name, position := range want {
		picture := fieldNodeNamed(t, descriptor, name).GetPicture()

		if got := spell(picture.GetSignPosition(), "SIGN_POSITION_"); got != position {
			t.Errorf("%s holds its sign %s, want %s", name, got, position)
		}
	}
}

// TestAnAliasedUsageResolvesToTheRepresentationItsBytesAreIn holds COMP, COMP-4
// and BINARY to reaching a generator as one representation, and COMP-3 and
// PACKED-DECIMAL likewise.
func TestAnAliasedUsageResolvesToTheRepresentationItsBytesAreIn(t *testing.T) {
	const usages = `01 USE-REC.
   05 USE-TYPE PIC X(1).
   05 USE-COMP PIC S9(4) COMP.
   05 USE-COMP4 PIC S9(4) COMP-4.
   05 USE-BINARY PIC S9(4) BINARY.
   05 USE-COMP3 PIC S9(4) COMP-3.
   05 USE-PACKED PIC S9(4) PACKED-DECIMAL.
   05 USE-COMP5 PIC S9(4) COMP-5.
   05 USE-FLOAT COMP-1.
`

	const source = `(framing (recfm V) (lrecl 100))
(encoding (charset cp037) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))
(record USAGES (copybook "use.cpy" USE-REC))
(discriminate USAGES (equals (item USAGES USE-TYPE) "U"))
(sequence (+ USAGES))`

	descriptor := assembled(t, source, map[string]string{"USAGES": usages})

	want := map[string]irpb.Usage{
		"USE-COMP":   irpb.Usage_USAGE_BINARY,
		"USE-COMP4":  irpb.Usage_USAGE_BINARY,
		"USE-BINARY": irpb.Usage_USAGE_BINARY,
		"USE-COMP3":  irpb.Usage_USAGE_PACKED_DECIMAL,
		"USE-PACKED": irpb.Usage_USAGE_PACKED_DECIMAL,
		"USE-COMP5":  irpb.Usage_USAGE_COMP_5,
		"USE-FLOAT":  irpb.Usage_USAGE_COMP_1,
	}

	for name, usage := range want {
		if got := fieldNodeNamed(t, descriptor, name).GetUsage(); got != usage {
			t.Errorf("%s is %s, want %s", name, got, usage)
		}
	}

	// COMP-1 permits no PICTURE, and what a field with none carries is no
	// picture message rather than an empty one.
	if picture := fieldNodeNamed(t, descriptor, "USE-FLOAT").GetPicture(); picture != nil {
		t.Errorf("USE-FLOAT carries the picture %v, and COMP-1 permits none", picture)
	}
}

// fieldNodeNamed finds the one field node standing for a copybook item.
func fieldNodeNamed(t *testing.T, d *irpb.Descriptor, original string) *irpb.Field {
	t.Helper()

	for _, node := range d.GetNodes() {
		if field := node.GetField(); field.GetNames().GetOriginal() == original {
			return field
		}
	}

	t.Fatalf("no field node is named %s", original)

	return nil
}

// twoOverOne is a layout binding two record names to one copybook item, which is
// the shape docs/layout/SPEC.md's "Many records may name one copybook, and two
// may name one item" admits and the one every rename rule below turns on.
const twoOverOne = `(framing (recfm FB) (lrecl 8))
(encoding (charset cp037) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))
(record ORDER-OPEN (copybook "ord.cpy" ORD-REC))
(record ORDER-CLOSE (copybook "ord.cpy" ORD-REC))
(discriminate ORDER-OPEN (equals (item ORDER-OPEN ORD-TYPE) "O"))
(discriminate ORDER-CLOSE (equals (item ORDER-CLOSE ORD-TYPE) "C"))
(sequence (seq ORDER-OPEN ORDER-CLOSE))`

// order is the copybook both of them name.
const order = `01 ORD-REC.
   05 ORD-TYPE PIC X(1).
   05 ORD-NO PIC X(7).
`

// twoOrders is that layout resolved: two record types over one copybook item,
// each with an item tree of its own, exactly as the pipeline in front of this
// package builds them.
func twoOrders(t *testing.T) Options {
	t.Helper()

	opts := options(t, twoOverOne, map[string]string{"ORDER-OPEN": order, "ORDER-CLOSE": order})
	if len(opts.Records) != 2 {
		t.Fatalf("the layout resolved to %d record types, want 2", len(opts.Records))
	}

	return opts
}

// TestARenameIsPerRecordAndNotPerCopybookItem is #164's half of the rename rule:
// a rename belongs to the record type it was written under, and reaches no node
// of any other.
//
// The rename below names ORDER-OPEN's own ORD-NO item and is written under
// ORDER-CLOSE, which is a rename an adopter cannot write and this package can
// nevertheless be handed. It has to reach nothing. Keyed on the copybook item
// alone it would reach ORDER-OPEN — the record it was not written for — and the
// descriptor would come out well-formed, carrying a name only the other record
// was given.
func TestARenameIsPerRecordAndNotPerCopybookItem(t *testing.T) {
	opts := twoOrders(t)

	opts.Renames = []Rename{{
		Record:     "ORDER-CLOSE",
		Item:       fieldNamed(t, opts.Records[0].Resolved.Item, "ORD-NO"),
		Substitute: "OpeningOrderNumber",
	}}

	descriptor, err := Assemble(opts)
	if err != nil {
		t.Fatalf("assembling the descriptor: %v", err)
	}

	for _, node := range descriptor.GetNodes() {
		names := node.GetField().GetNames()
		if names.GetOriginal() == "ORD-NO" && names.OverrideName != nil {
			t.Errorf("an ORD-NO node carries %q, and the rename was written under the other record",
				names.GetOverrideName())
		}
	}

	// The same rename under the record it names does reach it, so the check
	// above is about the record and not about the item having been missed.
	opts.Renames[0].Record = "ORDER-OPEN"

	descriptor, err = Assemble(opts)
	if err != nil {
		t.Fatalf("assembling the descriptor: %v", err)
	}

	substitutes := make([]string, 0, 2)

	for _, node := range descriptor.GetNodes() {
		names := node.GetField().GetNames()
		if names.GetOriginal() == "ORD-NO" {
			substitutes = append(substitutes, names.GetOverrideName())
		}
	}

	if want := []string{"OpeningOrderNumber", ""}; !slices.Equal(substitutes, want) {
		t.Errorf("the two ORD-NO nodes carry %q, want %q", substitutes, want)
	}
}

// TestARenameOnARecordNamesTheRecordNode is the other half: a rename carrying no
// item substitutes a name for the record type's own, which is the one name an
// item reference cannot reach.
//
// The original stays the `01`-level's, and both record nodes carry it: every
// alternative of a redefined level is a description of that level, and the
// override is what tells two of them apart (docs/ir/SPEC.md, "Names").
func TestARenameOnARecordNamesTheRecordNode(t *testing.T) {
	opts := twoOrders(t)

	opts.Renames = []Rename{{Record: "ORDER-CLOSE", Substitute: "ORD-CLOSE-REC"}}

	descriptor, err := Assemble(opts)
	if err != nil {
		t.Fatalf("assembling the descriptor: %v", err)
	}

	originals := make([]string, 0, 2)
	overrides := make([]string, 0, 2)

	for _, node := range descriptor.GetNodes() {
		record := node.GetRecord()
		if record == nil {
			continue
		}

		originals = append(originals, record.GetNames().GetOriginal())
		overrides = append(overrides, record.GetNames().GetOverrideName())
	}

	if want := []string{"ORD-REC", "ORD-REC"}; !slices.Equal(originals, want) {
		t.Errorf("the record nodes are named %q, want %q", originals, want)
	}

	if want := []string{"", "ORD-CLOSE-REC"}; !slices.Equal(overrides, want) {
		t.Errorf("the record nodes carry the overrides %q, want %q", overrides, want)
	}
}

// TestARenameNamingNoRecordIsReported holds a rename with no record to being a
// fault rather than a no-op.
//
// A rename is per record, so one naming none reaches nothing — and the
// descriptor that comes out of ignoring it is well formed and carries no
// override at all, which is indistinguishable from a layout that wrote no
// renames. It is also the shape a caller written against the older [Rename]
// produces, and that caller still compiles.
func TestARenameNamingNoRecordIsReported(t *testing.T) {
	opts := twoOrders(t)

	opts.Renames = []Rename{{
		Item:       fieldNamed(t, opts.Records[0].Resolved.Item, "ORD-NO"),
		Substitute: "OpeningOrderNumber",
	}}

	_, err := Assemble(opts)
	if err == nil {
		t.Fatal("a rename naming no record assembles")
	}

	var unnamed *UnnamedRenameError
	if !errors.As(err, &unnamed) {
		t.Fatalf("a rename naming no record reads as %v, want an UnnamedRenameError", err)
	}

	if !strings.Contains(err.Error(), "ORD-NO") || !strings.Contains(err.Error(), "OpeningOrderNumber") {
		t.Errorf("the diagnostic names neither the item nor the substitute: %v", err)
	}
}

// TestCharsetNoneReachesTheFieldTheOverrideNames is #275 through the whole
// pipeline: a `PIC X` item whose bytes are a payload carries CHARSET_NONE into
// the descriptor, and every field beside it carries the profile's code page.
//
// It is asserted on the assembled descriptor rather than on the mapping
// function because what has to be right is which field the value lands on. A
// mapping test would pass just as well against a producer that put the value on
// every field of the record.
func TestCharsetNoneReachesTheFieldTheOverrideNames(t *testing.T) {
	const regions = `01 REG-REC.
   05 REG-TYPE PIC X(1).
   05 REG-NAME PIC X(4).
   05 REG-CODE PIC X(2).
   05 REG-COUNT PIC 9(2).
`

	const source = `(framing (recfm FB) (lrecl 9))
(encoding (charset cp037) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))
(encoding-override (item REGION REG-CODE) (charset none))
(record REGION (copybook "reg.cpy" REG-REC))
(discriminate REGION (equals (item REGION REG-TYPE) "R"))
(sequence (+ REGION))`

	descriptor := assembled(t, source, map[string]string{"REGION": regions})

	equal(t, render(descriptor), `version 1
0 file unframed start=7
1 record REG-REC root=2
2 group REG-REC members=[3 4 5 6]
3 field REG-TYPE width=1 display cp037/ebcdic/big-endian/ibm-hfp alphanumeric(0,0)
4 field REG-NAME width=4 display cp037/ebcdic/big-endian/ibm-hfp alphanumeric(0,0)
5 field REG-CODE width=2 display none/ebcdic/big-endian/ibm-hfp alphanumeric(0,0)
6 field REG-COUNT width=2 display cp037/ebcdic/big-endian/ibm-hfp numeric(2,0)
7 state transitions=[9]
8 state accepts transitions=[11]
9 transition record=1 to=8 predicate=10
10 predicate field=3 equals 0xd9
11 transition record=1 to=8 predicate=10
`)
}

// The batched file docs/ir/SPEC.md's "Transitions are ordered by what they read"
// is written for: a header keyed on the record's first byte, a detail keyed ten
// bytes in, and both admissible at the state after a detail. The ten digits
// ahead of the detail's type code are what make the pair provably exclusive, so
// what a descriptor carries here is an order rather than a refusal.
const (
	batchHeader = `01 BAT-HDR.
   05 BAT-TYPE PIC X(1).
   05 BAT-ID   PIC 9(9).
   05 BAT-SEQ  PIC 9(5).
   05 BAT-BODY PIC X(6).
`

	batchDetail = `01 BDT-REC.
   05 BDT-KEY  PIC 9(10).
   05 BDT-TYPE PIC X(1).
   05 BDT-BODY PIC X(10).
`
)

const batched = `(framing (recfm FB) (lrecl 21))
(encoding (charset cp037) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))
(record HEADER (copybook "header.cpy" BAT-HDR))
(record DETAIL (copybook "detail.cpy" BDT-REC))
(discriminate HEADER (equals (item HEADER BAT-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL BDT-TYPE) "D"))
(sequence (+ (seq HEADER (+ DETAIL))))`

// TestTheOrderAStateCarriesItsTransitionsInSurvivesAssembly is docs/ir/SPEC.md's
// "Transitions are ordered by what they read" asserted one layer on: the order
// `resolve` decided is the order the state node's transition list is in, and
// nothing between here and the wire re-derives it (#331).
//
// It is asserted over the record each transition admits rather than over the
// identifiers, because identifiers are assigned by this traversal and would
// come out ascending whatever order the transitions were in — which is the one
// way this assertion could pass while saying nothing.
func TestTheOrderAStateCarriesItsTransitionsInSurvivesAssembly(t *testing.T) {
	t.Parallel()

	descriptor := assembled(t, batched, map[string]string{"HEADER": batchHeader, "DETAIL": batchDetail})

	var admitted [][]string
	for _, node := range descriptor.GetNodes() {
		state, isState := node.GetKind().(*irpb.Node_State)
		if !isState || len(state.State.GetTransitionIds()) < 2 {
			continue
		}

		var records []string
		for _, at := range state.State.GetTransitionIds() {
			records = append(records, recordAdmittedBy(t, descriptor, at))
		}

		admitted = append(admitted, records)
	}

	// The names are the copybooks' `01`-levels, which is what a record node
	// carries (docs/ir/SPEC.md, "Names").
	want := [][]string{{"BAT-HDR", "BDT-REC"}}
	if !slices.EqualFunc(admitted, want, slices.Equal) {
		t.Errorf("the states offering a choice admit %v, want %v — the header's code is byte zero and the detail's is byte ten",
			admitted, want)
	}
}

// recordAdmittedBy is the name of the record one transition node admits.
func recordAdmittedBy(t *testing.T, d *irpb.Descriptor, transition uint64) string {
	t.Helper()

	byID := make(map[uint64]*irpb.Node, len(d.GetNodes()))
	for _, node := range d.GetNodes() {
		byID[node.GetId()] = node
	}

	edge, isTransition := byID[transition].GetKind().(*irpb.Node_Transition)
	if !isTransition {
		t.Fatalf("node %d is not a transition", transition)
	}

	record, isRecord := byID[edge.Transition.GetRecordId()].GetKind().(*irpb.Node_Record)
	if !isRecord {
		t.Fatalf("transition %d admits node %d, which is not a record", transition, edge.Transition.GetRecordId())
	}

	return record.Record.GetNames().GetOriginal()
}
