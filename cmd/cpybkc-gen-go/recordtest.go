// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/Zaba505/cpybkc/irpb"
)

// recordsTestFile is the record tier of the generated tests, beside the two
// files it covers.
//
// Named after them rather than after itself: records.go declares the structs
// and codec.go the two methods that fill one, and to a case those are one thing
// — an adopter grepping for the case that covers a record lands in the file
// named after the file that declares it. README.md's "The generated tests" is
// where the whole of that decision is, and nothing here re-opens it.
const recordsTestFile = "records_test.go"

// caseIdentifiers are every identifier a generated case's file already spends,
// which is what the generated package may not be imported under.
//
// Two kinds, and both of them matter. The **locals** a case declares — `t`,
// `in`, `r`, `record`, `out`, `w`, `want` — shadow the import from the point
// they are declared, so `var record ledger.LedgerHeader` inside `package record`
// would make the second `record.Encoding()` name the struct. The **packages**
// the file imports — `bytes`, `testing`, `codec`, `big` — collide with it
// outright: `package bytes` generated into `.../x/bytes` puts `"bytes"` in the
// import block twice, and generated into `.../x/pkg` puts an alias `bytes` beside
// the standard library's.
//
// A generated package's name is the adopter's, so `package codec` is a package
// this generator has to be able to write tests for rather than one it may
// refuse. Where the name is spent, [shadowAlias] is imported under instead.
var caseIdentifiers = map[string]struct{}{
	"t": {}, "in": {}, "r": {}, "record": {}, "out": {}, "w": {}, "want": {},
	"bytes": {}, "testing": {}, lastElement(codecImport): {}, lastElement(bigIntImport): {},
}

// shadowAlias is what the generated package is imported under where its own name
// is one this file has already spent.
//
// It is not itself in [caseIdentifiers] and cannot become one: the identifiers
// there are this generator's own, so a name it never writes is a name nothing
// can collide with. It is unexported and short for the same reason every other
// identifier in a generated case is — the file is read by somebody debugging a
// failure, not written by anybody.
const shadowAlias = "pkg"

// recordTests is the source of [recordsTestFile] for this descriptor, or the
// empty string where there is nothing for a case to be made out of.
//
// One case per record type and one per variant arm, in the order the node list
// carries the records — which docs/ir/SPEC.md's "Identity, ordering and
// determinism" fixes as ascending identifier order, so the cases come out in a
// producer's deterministic order rather than in one this generator invents.
//
// Absent for a descriptor carrying no record node, for the reason records.go is
// absent for one: a tier with no record to make a case out of has nothing to
// write. Absent too where no field of the descriptor gives an encoding to read
// — a case reads its bytes under the generated Encoding, and a descriptor with
// no field declares none.
func recordTests(d *irpb.Descriptor, opts options) (string, error) {
	e, err := newEmitter(d)
	if err != nil {
		return "", err
	}

	c := &coder{emitter: e, receiver: opts.receiverName()}

	var records []*irpb.Node

	for _, node := range d.GetNodes() {
		if node.GetRecord() != nil {
			records = append(records, node)
		}
	}

	if len(records) == 0 {
		return "", nil
	}

	enc, err := descriptorEncoding(d)
	if err != nil {
		return "", err
	}

	if enc == nil {
		return "", nil
	}

	if opts.importPath == "" {
		return "", fmt.Errorf(
			"%s %s=<path> is required for a descriptor carrying a record: the generated tests are an external test package, so they import the package beside them by path, and %s names a scratch directory rather than where the files end up",
			optFlag, importPathOption, outFlag)
	}

	s, err := newSynth(c, d, enc)
	if err != nil {
		return "", err
	}

	alias := opts.packageName
	if _, shadowed := caseIdentifiers[alias]; shadowed {
		alias = shadowAlias
	}

	var (
		funcs []string
		used  = make(map[string]struct{})
	)

	for _, node := range records {
		typ, err := recordName(node.GetRecord().GetNames())
		if err != nil {
			return "", err
		}

		cases, err := c.cases(node.GetRecord(), typ, used)
		if err != nil {
			return "", err
		}

		for _, one := range cases {
			source, err := s.testCase(node, typ, one, alias)
			if err != nil {
				return "", err
			}

			funcs = append(funcs, source)
		}
	}

	return testSource(funcs, s.needs, opts, alias), nil
}

// testSource is the file the cases were written into: the generated-file
// header, the external test package's clause, the imports, and the cases.
func testSource(funcs []string, needs map[string]struct{}, opts options, alias string) string {
	paths := []string{"bytes", "testing", codecImport, opts.importPath}
	for path := range needs {
		paths = append(paths, path)
	}

	slices.Sort(paths)

	var b strings.Builder

	b.WriteString(generatedBy)
	b.WriteString("\n\n")
	b.WriteString(commentLines(fmt.Sprintf(`The record tier of this package's generated tests: one case per record the
layout describes, and one per arm of an alternation inside one.

Each case carries the bytes it reads as a literal, decodes them, checks every
field against the value the literal was laid out with, and writes the record
back out byte for byte. The bytes are synthesized from the layout rather than
read from a file, so they are what to hold against the file on your desk: the
comment column names the item, the offset summed from the widths ahead of it,
and the picture.

Nothing here is yours to edit — this directory is regenerated whole. Put your
own tests in a package of your own that imports %s.`, opts.packageName)))
	b.WriteString("package ")
	b.WriteString(opts.packageName)
	b.WriteString("_test\n\nimport (\n")

	for _, path := range paths {
		if path == opts.importPath && alias != lastElement(path) {
			fmt.Fprintf(&b, "%s %q\n", alias, path)

			continue
		}

		fmt.Fprintf(&b, "%q\n", path)
	}

	b.WriteString(")\n")

	for _, source := range funcs {
		b.WriteString("\n")
		b.WriteString(source)
		b.WriteString("\n")
	}

	return b.String()
}

// lastElement is the name Go takes an unaliased import by, absent the package
// declaring another.
func lastElement(path string) string {
	if at := strings.LastIndex(path, "/"); at >= 0 {
		return path[at+1:]
	}

	return path
}

// generatedCase is one test function to be written: its name, the arms it
// selects, and what to say about that in its doc comment.
type generatedCase struct {
	// name is the Go identifier of the test function.
	name string

	// selects is the arm this case is for, as the copybook names it, and is
	// empty for the case that takes every alternation's first arm.
	selects string

	// arms is the arm index each variant takes, by the variant node's
	// identifier. A variant this says nothing about takes its first arm.
	arms map[uint64]int
}

// cases is the cases one record produces: one for the record itself, and one
// more for every arm of every alternation it carries beyond the first.
//
// Coverage is per arm rather than per happy path because an arm is a
// discriminator path, and a discriminator no case covers is one whose spelling
// an adopter finds out about from a production file. The first arm of each
// alternation is covered by the record's own case — it is what a case takes
// where it says nothing — so the count is one plus one per further arm rather
// than one per arm plus one.
//
// A record type the automaton cannot reach still gets a case. The tier is per
// record *type*, not per path: a type no transition admits is a layout bug an
// adopter would want shown rather than silently skipped, and the case is where
// they would see it.
func (c *coder) cases(record *irpb.Record, typ string, used map[string]struct{}) ([]generatedCase, error) {
	variants, err := c.variantsOf(record.GetRootId(), nil, nil)
	if err != nil {
		return nil, err
	}

	cases := []generatedCase{{
		name: unique("Test"+typ+"ReadsBackTheBytesItWasReadFrom", used),
		arms: map[uint64]int{},
	}}

	for _, at := range variants {
		node, ok := c.nodes[at.id]
		if !ok {
			return nil, unresolved(at.id)
		}

		arms := node.GetVariant().GetArms()

		for i := 1; i < len(arms); i++ {
			body, err := c.armBody(arms[i])
			if err != nil {
				return nil, err
			}

			name, err := identifier("arm", namesOf(body))
			if err != nil {
				return nil, err
			}

			selected := map[uint64]int{at.id: i}
			maps.Copy(selected, at.path)

			cases = append(cases, generatedCase{
				name:    unique("Test"+typ+"Holding"+name+"ReadsBackTheBytesItWasReadFrom", used),
				selects: originalOf(body),
				arms:    selected,
			})
		}
	}

	return cases, nil
}

// unique is name, or name with the lowest number after it that nothing has
// taken.
//
// Two alternations in one record may name arms alike — a copybook is free to
// spell one REDEFINES the same way twice at different levels — and two test
// functions of one name is a file that does not compile. Numbered rather than
// qualified by the containing group, because the group's name is already in the
// case's own comment column and a name assembled from two copybook words is
// longer than the thing it disambiguates.
func unique(name string, used map[string]struct{}) string {
	candidate := name

	for n := 2; ; n++ {
		if _, taken := used[candidate]; !taken {
			used[candidate] = struct{}{}

			return candidate
		}

		candidate = name + strconv.Itoa(n)
	}
}

// variantAt is one alternation of a record and the arms that have to be
// selected to reach it.
type variantAt struct {
	// id is the variant node.
	id uint64

	// path is the arm each alternation containing this one takes, by that
	// alternation's node identifier. Empty for one no alternation contains.
	path map[uint64]int
}

// variantsOf is every alternation the record contains, at any depth, in record
// order.
//
// An alternation inside an arm of another is reached only when that arm is
// selected, so it is recorded with the selections that reach it. Without that,
// a case for an inner arm would be laid out over an outer arm the record does
// not hold, which is bytes for a shape that is not there.
func (c *coder) variantsOf(id uint64, path map[uint64]int, found []variantAt) ([]variantAt, error) {
	members, err := c.flattened(id)
	if err != nil {
		return nil, err
	}

	for _, memberID := range members {
		member, ok := c.nodes[memberID]
		if !ok {
			return nil, unresolved(memberID)
		}

		switch kind := member.GetKind().(type) {
		case *irpb.Node_Group:
			found, err = c.variantsOf(memberID, path, found)
			if err != nil {
				return nil, err
			}
		case *irpb.Node_Variant:
			found = append(found, variantAt{id: memberID, path: path})

			for i, a := range kind.Variant.GetArms() {
				body, err := c.armBody(a)
				if err != nil {
					return nil, err
				}

				if body.GetGroup() == nil {
					continue
				}

				inner := map[uint64]int{memberID: i}
				maps.Copy(inner, path)

				found, err = c.variantsOf(body.GetId(), inner, found)
				if err != nil {
					return nil, err
				}
			}
		}
	}

	return found, nil
}

// testCase is one case, laid out and written.
func (s *synth) testCase(node *irpb.Node, typ string, one generatedCase, alias string) (string, error) {
	if err := s.layOut(node, "record", one.arms); err != nil {
		return "", err
	}

	item := node.GetRecord().GetNames().GetOriginal()

	doc := wrapped(fmt.Sprintf(
		"%s is %s: the bytes below, every field they decode into, and the same bytes written back.",
		one.name, item))

	if one.selects != "" {
		doc += "\n\n" + wrapped(fmt.Sprintf("The alternation this case is for holds %s.", one.selects))
	}

	var b strings.Builder

	b.WriteString(commentLines(doc))
	fmt.Fprintf(&b, "func %s(t *testing.T) {\nt.Parallel()\n\n", one.name)
	b.WriteString(s.byteLiteral())
	fmt.Fprintf(&b, "\nr, err := codec.NewReader(bytes.NewReader(in), %s.%s())\n", alias, encodingFunc)
	b.WriteString("if err != nil {\nt.Fatalf(\"codec.NewReader: %v\", err)\n}\n\n")
	fmt.Fprintf(&b, "var record %s.%s\n\n", alias, typ)
	b.WriteString("if err := record.UnmarshalCOBOL(r); err != nil {\nt.Fatalf(\"UnmarshalCOBOL: %v\", err)\n}\n")

	for _, check := range s.checks {
		b.WriteString("\n")
		b.WriteString(check)
		b.WriteString("\n")
	}

	b.WriteString("\nvar out bytes.Buffer\n\n")
	fmt.Fprintf(&b, "w, err := codec.NewWriter(&out, %s.%s())\n", alias, encodingFunc)
	b.WriteString("if err != nil {\nt.Fatalf(\"codec.NewWriter: %v\", err)\n}\n\n")
	b.WriteString("if err := record.MarshalCOBOL(w); err != nil {\nt.Fatalf(\"MarshalCOBOL: %v\", err)\n}\n\n")
	b.WriteString("if !bytes.Equal(out.Bytes(), in) {\n")
	b.WriteString("t.Errorf(\"the record does not write back the bytes it was read from\\n got: % x\\nwant: % x\", out.Bytes(), in)\n")
	b.WriteString("}\n}")

	return b.String(), nil
}

// byteLiteral is the case's bytes as source: a readable string where the charset is
// an ASCII family, and an annotated slice of hex anywhere else.
//
// The rule keys on the descriptor's charset rather than on each item's kind,
// because a case's bytes are one literal and a literal has one spelling. For an
// EBCDIC file the slice is both the honest spelling and the useful one — it
// pastes into `hexdump` beside the dataset — and for an ASCII file the string
// is readable the way the file is, with hex only where an item's bytes are not
// characters under any charset. See README.md, "Decided: bytes are spelled
// charset-aware".
func (s *synth) byteLiteral() string {
	if s.charset == irpb.Charset_CHARSET_ASCII {
		return s.stringLiteral()
	}

	return s.hexLiteral()
}

// stringLiteral is the ASCII spelling: one quoted run per item, concatenated,
// each with the item that wrote it in the comment column.
func (s *synth) stringLiteral() string {
	var runs []chunk

	for _, one := range s.runs {
		if len(one.body) > 0 {
			runs = append(runs, one)
		}
	}

	if len(runs) == 0 {
		return "in := []byte(\"\")\n"
	}

	var b strings.Builder

	b.WriteString("in := []byte(")

	for i, one := range runs {
		b.WriteString(strconv.Quote(string(one.body)))

		if i < len(runs)-1 {
			b.WriteString(" +")
		} else {
			b.WriteString(")")
		}

		fmt.Fprintf(&b, " // %s\n", one.note)
	}

	return b.String()
}

// bytesPerLine is how many bytes of one item's run stand on a line of the hex
// spelling.
//
// Eight, because that is the width a hex dump is read in and the width a line
// stays inside once the comment column is beside it.
const bytesPerLine = 8

// hexLiteral is the EBCDIC spelling: one line of hex per eight bytes, with the
// item that wrote them in the comment column of the first and the column held
// open on the rest.
func (s *synth) hexLiteral() string {
	var b strings.Builder

	b.WriteString("in := []byte{\n")

	for _, one := range s.runs {
		if len(one.body) == 0 {
			continue
		}

		for at := 0; at < len(one.body); at += bytesPerLine {
			end := min(at+bytesPerLine, len(one.body))

			for _, value := range one.body[at:end] {
				fmt.Fprintf(&b, "0x%02x, ", value)
			}

			if at == 0 {
				fmt.Fprintf(&b, "// %s\n", one.note)
			} else {
				b.WriteString("//\n")
			}
		}
	}

	b.WriteString("}\n")

	return b.String()
}

// commentWidth is where a generated doc comment wraps.
//
// Seventy-six, so that the two slashes and the space ahead of it leave the line
// inside the eighty this repository's own source keeps to. A test function's
// name is as long as the record the copybook spells, so the first line of a
// case's comment is the one that would otherwise run away.
const commentWidth = 76

// wrapped is text broken at [commentWidth], on spaces.
func wrapped(text string) string {
	var (
		b    strings.Builder
		line int
	)

	for i, word := range strings.Fields(text) {
		switch {
		case i == 0:
			line = len(word)
		case line+1+len(word) > commentWidth:
			b.WriteString("\n")

			line = len(word)
		default:
			b.WriteString(" ")

			line += 1 + len(word)
		}

		b.WriteString(word)
	}

	return b.String()
}
