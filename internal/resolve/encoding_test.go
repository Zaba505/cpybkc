// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"errors"
	"strings"
	"testing"

	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/layout"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// converted is the other profile every test here reads: a file a mainframe
// wrote and a conversion turned into ASCII.
//
// It is not "the ASCII settings" — it is deliberately the combination
// docs/layout/SPEC.md says no compiler produces and real files hit most often,
// with ASCII characters, translated-EBCDIC signs and big-endian binary. A test
// pairing a mainframe profile with an all-native one would pass just as well
// against an implementation that treated the four axes as a single dialect flag,
// which is the thing they exist not to be.
func converted() layoutmodel.Axes {
	return layoutmodel.Axes{
		Charset:        layoutmodel.ASCII,
		SignConvention: layoutmodel.SignTranslatedEBCDIC,
		ByteOrder:      layoutmodel.BigEndian,
		FloatFormat:    layoutmodel.IEEE754,
	}
}

// resolveUnder resolves a copybook source under a profile and the overrides the
// caller builds over its own copybook fields, which is the two-step a layout
// takes: each item reference is resolved against the copybook, then the copybook
// is resolved.
func resolveUnder(
	t *testing.T,
	src string,
	profile layoutmodel.Axes,
	build func(*copybook.Field) []EncodingOverride,
) *Record {
	t.Helper()

	field := recordOf(t, src)

	var overrides []EncodingOverride
	if build != nil {
		overrides = build(field)
	}

	records, err := Resolve(field, Options{
		Copybook:          "test.cpy",
		Dialect:           copybook.IBMEnterprise(),
		Encoding:          profile,
		EncodingOverrides: overrides,
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("resolved to %d records, want 1", len(records))
	}
	return records[0]
}

// encodingOf is the resolved encoding of the field named name.
func encodingOf(t *testing.T, record *Record, name string) layoutmodel.Axes {
	t.Helper()

	node := record.Find(name)
	if node == nil {
		t.Fatalf("the resolved record holds no %s", name)
	}
	if node.Kind != KindField {
		t.Fatalf("%s resolved to a %s, want a field", name, node.Kind)
	}
	return node.Encoding
}

// mixedSource is one record whose fields do not agree about their encoding: the
// ordinary shape of a converted file, where the text was translated and the
// packed and binary fields were copied through untouched.
const mixedSource = `01 CUSTOMER.
   05 CUST-ID PIC X(6).
   05 CUST-NAME PIC X(20).
   05 CUST-BALANCE PIC S9(7)V99 COMP-3.
   05 CUST-COUNT PIC S9(4) COMP.
`

// TestEveryFieldCarriesAllFourAxes is docs/ir/SPEC.md's "The encoding profile,
// applied" as this package meets it: all four, on every field, with none left
// for a generator to default.
func TestEveryFieldCarriesAllFourAxes(t *testing.T) {
	t.Parallel()

	record := resolveUnder(t, mixedSource, mainframe(), nil)

	seen := 0
	record.Walk(func(node *Node) {
		if node.Kind != KindField {
			return
		}
		seen++

		if missing := node.Encoding.Missing(); len(missing) > 0 {
			t.Errorf("%s states no %v", itemName(node.Field), missing)
		}
		if node.Encoding != mainframe() {
			t.Errorf("%s carries %+v, want the profile %+v", itemName(node.Field), node.Encoding, mainframe())
		}
	})

	if seen != 4 {
		t.Fatalf("walked %d fields, want the copybook's 4", seen)
	}
}

// TestAFieldCarriesTheUsageTheCopybookGaveIt is the other half of what a
// generator needs to read a field's bytes. It is not restated on the node: the
// copybook has already inherited it down the entry tree, and a second copy here
// would be a second answer to it.
func TestAFieldCarriesTheUsageTheCopybookGaveIt(t *testing.T) {
	t.Parallel()

	record := resolveUnder(t, `01 R.
   05 TEXT PIC X(4).
   05 PACKED PIC S9(5) COMP-3.
   05 G COMP.
      10 INHERITED PIC S9(4).
`, mainframe(), nil)

	for _, tc := range []struct {
		name string
		want copybook.Usage
	}{
		{name: "TEXT", want: copybook.UsageDisplay},
		{name: "PACKED", want: copybook.UsageComp3},
		// The item states no USAGE of its own; the group above it does.
		{name: "INHERITED", want: copybook.UsageComp},
	} {
		if got := record.Find(tc.name).Field.Usage; got != tc.want {
			t.Errorf("%s is USAGE %s, want %s", tc.name, got, tc.want)
		}
	}
}

// TestNoNodeButAFieldCarriesAnEncoding holds the IR's refusal to carry a profile
// for anything to inherit from: a group carrying a copy of the axes would be
// that profile under another name.
func TestNoNodeButAFieldCarriesAnEncoding(t *testing.T) {
	t.Parallel()

	// A dialect whose binary widths are not powers of two is what puts a
	// repeating elementary item inside a group this package introduced, which
	// is the one wrapper a field node sits in.
	dialect := copybook.IBMEnterprise()
	dialect.Binary = copybook.BinarySizeSmallest

	field := recordOf(t, `01 R.
   05 T PIC S9(5) COMP SYNCHRONIZED OCCURS 3 TIMES.
   05 Z PIC X(2).
`)
	records, err := Resolve(field, Options{
		Copybook: "test.cpy",
		Dialect:  dialect,
		Encoding: mainframe(),
		EncodingOverrides: []EncodingOverride{
			{Item: fieldNamed(t, field, "T"), Axes: layoutmodel.Axes{Charset: layoutmodel.ASCII}},
		},
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	record := records[0]

	kinds := 0
	record.Walk(func(node *Node) {
		if node.Kind == KindField {
			return
		}
		kinds++

		if node.Encoding != (layoutmodel.Axes{}) {
			t.Errorf("a %s node carries the encoding %+v", node.Kind, node.Encoding)
		}
	})

	// The record's top level, the group holding the padded item, and the
	// padding itself.
	if kinds != 3 {
		t.Fatalf("walked %d nodes that are not fields, want 3", kinds)
	}

	if got := record.Find("T").Encoding.Charset; got != layoutmodel.ASCII {
		t.Errorf("the padded item's charset is %q, want the override's ascii", got)
	}
}

// TestAnOverrideRestatesTheAxesItNamesAndLeavesTheRest is
// docs/layout/SPEC.md's "An override naming one axis leaves the other three as
// the profile states them; one naming all four replaces the profile entirely for
// that item".
func TestAnOverrideRestatesTheAxesItNamesAndLeavesTheRest(t *testing.T) {
	t.Parallel()

	record := resolveUnder(t, `01 R.
   05 ONE PIC X(4).
   05 SOME PIC X(4).
   05 ALL PIC X(4).
`, mainframe(), func(field *copybook.Field) []EncodingOverride {
		return []EncodingOverride{
			{
				Item: fieldNamed(t, field, "ONE"),
				Axes: layoutmodel.Axes{Charset: layoutmodel.ASCII},
			},
			{
				Item: fieldNamed(t, field, "SOME"),
				Axes: layoutmodel.Axes{
					SignConvention: layoutmodel.SignRealia,
					ByteOrder:      layoutmodel.LittleEndian,
				},
			},
			{
				Item: fieldNamed(t, field, "ALL"),
				Axes: converted(),
			},
		}
	})

	for _, tc := range []struct {
		name string
		want layoutmodel.Axes
	}{
		{
			name: "ONE",
			want: layoutmodel.Axes{
				Charset:        layoutmodel.ASCII,
				SignConvention: layoutmodel.SignEBCDIC,
				ByteOrder:      layoutmodel.BigEndian,
				FloatFormat:    layoutmodel.HFP,
			},
		},
		{
			name: "SOME",
			want: layoutmodel.Axes{
				Charset:        layoutmodel.CP037,
				SignConvention: layoutmodel.SignRealia,
				ByteOrder:      layoutmodel.LittleEndian,
				FloatFormat:    layoutmodel.HFP,
			},
		},
		{name: "ALL", want: converted()},
	} {
		if got := encodingOf(t, record, tc.name); got != tc.want {
			t.Errorf("%s carries %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// TestAnOverrideOnAGroupReachesEveryElementaryItemUnderIt, at every depth, and
// leaves the items beside it alone.
func TestAnOverrideOnAGroupReachesEveryElementaryItemUnderIt(t *testing.T) {
	t.Parallel()

	record := resolveUnder(t, `01 R.
   05 OUTSIDE PIC X(4).
   05 G.
      10 SHALLOW PIC X(4).
      10 INNER.
         15 DEEP PIC X(4).
`, mainframe(), func(field *copybook.Field) []EncodingOverride {
		return []EncodingOverride{{
			Item: fieldNamed(t, field, "G"),
			Axes: layoutmodel.Axes{Charset: layoutmodel.ASCII},
		}}
	})

	for _, name := range []string{"SHALLOW", "DEEP"} {
		if got := encodingOf(t, record, name).Charset; got != layoutmodel.ASCII {
			t.Errorf("%s under the overridden group carries the charset %q, want ascii", name, got)
		}
	}
	if got := encodingOf(t, record, "OUTSIDE").Charset; got != layoutmodel.CP037 {
		t.Errorf("the item beside the overridden group carries the charset %q, want the profile's cp037", got)
	}
}

// TestAnOverrideOnARepeatingItemReachesEveryOccurrence — which it does by
// reaching the item, because an encoding is a property of an item's bytes
// wherever they sit and a node states it once for every occurrence.
func TestAnOverrideOnARepeatingItemReachesEveryOccurrence(t *testing.T) {
	t.Parallel()

	record := resolveUnder(t, `01 R.
   05 T OCCURS 4 TIMES.
      10 K PIC X(2).
      10 V PIC X(3).
`, mainframe(), func(field *copybook.Field) []EncodingOverride {
		return []EncodingOverride{{
			Item: fieldNamed(t, field, "T"),
			Axes: layoutmodel.Axes{Charset: layoutmodel.ASCII},
		}}
	})

	table := record.Find("T")
	if table.Repetition == nil || table.Repetition.Count != 4 {
		t.Fatal("the overridden item does not repeat four times")
	}

	for _, name := range []string{"K", "V"} {
		if got := encodingOf(t, record, name).Charset; got != layoutmodel.ASCII {
			t.Errorf("%s carries the charset %q, want the table's override of ascii", name, got)
		}
	}
}

// TestAnOverrideAppliesOverTheOneAboveItRatherThanOverTheProfile is the reading
// this package settles, which docs/layout/SPEC.md leaves to it: an override
// reaches every elementary item under the group it names, so an inner override
// restates axes over what already governs the item. Under the other reading an
// override naming one axis would silently undo an enclosing override of another.
func TestAnOverrideAppliesOverTheOneAboveItRatherThanOverTheProfile(t *testing.T) {
	t.Parallel()

	record := resolveUnder(t, `01 R.
   05 G.
      10 PLAIN PIC X(4).
      10 NESTED PIC S9(4) COMP.
`, mainframe(), func(field *copybook.Field) []EncodingOverride {
		return []EncodingOverride{
			{
				Item: fieldNamed(t, field, "G"),
				Axes: layoutmodel.Axes{Charset: layoutmodel.ASCII},
			},
			{
				Item: fieldNamed(t, field, "NESTED"),
				Axes: layoutmodel.Axes{ByteOrder: layoutmodel.LittleEndian},
			},
		}
	})

	want := layoutmodel.Axes{
		Charset:        layoutmodel.ASCII,
		SignConvention: layoutmodel.SignEBCDIC,
		ByteOrder:      layoutmodel.LittleEndian,
		FloatFormat:    layoutmodel.HFP,
	}
	if got := encodingOf(t, record, "NESTED"); got != want {
		t.Errorf("the inner override resolved to %+v, want %+v", got, want)
	}
	if got := encodingOf(t, record, "PLAIN").Charset; got != layoutmodel.ASCII {
		t.Errorf("the sibling carries the charset %q, want the group's ascii", got)
	}
}

// TestAMixedCharsetRecordNeedsNoSpecialTreatment is docs/ir/SPEC.md's "Two
// things then follow without being separately specified": the axes are per
// field, so a record whose fields disagree is the ordinary case rather than an
// exception.
func TestAMixedCharsetRecordNeedsNoSpecialTreatment(t *testing.T) {
	t.Parallel()

	record := resolveUnder(t, mixedSource, mainframe(), func(field *copybook.Field) []EncodingOverride {
		// The text was translated on the way off the mainframe and the
		// packed and binary fields were copied through byte for byte.
		return []EncodingOverride{
			{Item: fieldNamed(t, field, "CUST-ID"), Axes: layoutmodel.Axes{Charset: layoutmodel.ASCII}},
			{Item: fieldNamed(t, field, "CUST-NAME"), Axes: layoutmodel.Axes{Charset: layoutmodel.ASCII}},
		}
	})

	for _, tc := range []struct {
		name string
		want layoutmodel.Charset
	}{
		{name: "CUST-ID", want: layoutmodel.ASCII},
		{name: "CUST-NAME", want: layoutmodel.ASCII},
		{name: "CUST-BALANCE", want: layoutmodel.CP037},
		{name: "CUST-COUNT", want: layoutmodel.CP037},
	} {
		if got := encodingOf(t, record, tc.name).Charset; got != tc.want {
			t.Errorf("%s carries the charset %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestTheSameLayoutResolvesUnderASCIIAndUnderEBCDIC: the profile decides what
// every field says about its bytes and decides nothing about where those bytes
// are. Storage widths are the copybook's and the dialect's, so the two
// resolutions differ in the axes and in nothing else.
func TestTheSameLayoutResolvesUnderASCIIAndUnderEBCDIC(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		profile layoutmodel.Axes
	}{
		{name: "ebcdic", profile: mainframe()},
		{name: "ascii", profile: converted()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			record := resolveUnder(t, mixedSource, tc.profile, nil)

			for _, name := range []string{"CUST-ID", "CUST-NAME", "CUST-BALANCE", "CUST-COUNT"} {
				if got := encodingOf(t, record, name); got != tc.profile {
					t.Errorf("%s carries %+v, want the profile %+v", name, got, tc.profile)
				}
			}

			// PIC X(6), PIC X(20), a nine-digit COMP-3, and a four-digit
			// COMP under IBM Enterprise COBOL. None of them moves with the
			// charset.
			for _, want := range []struct {
				name  string
				width int
				at    int
			}{
				{name: "CUST-ID", width: 6, at: 0},
				{name: "CUST-NAME", width: 20, at: 6},
				{name: "CUST-BALANCE", width: 5, at: 26},
				{name: "CUST-COUNT", width: 2, at: 31},
			} {
				if got := widthOf(t, record, want.name); got != want.width {
					t.Errorf("%s is %d bytes, want %d", want.name, got, want.width)
				}
				if got := positionOf(t, record, want.name); got != want.at {
					t.Errorf("%s is at %d, want %d", want.name, got, want.at)
				}
			}
		})
	}
}

// TestAnOverrideReachesInsideAVariantsArms, because an arm's body is resolved
// like anything else and an alternative is an item the layout can name.
func TestAnOverrideReachesInsideAVariantsArms(t *testing.T) {
	t.Parallel()

	field := recordOf(t, tableSource)
	records, err := Resolve(field, Options{
		Copybook: "test.cpy",
		Dialect:  copybook.IBMEnterprise(),
		Encoding: mainframe(),
		EncodingOverrides: []EncodingOverride{{
			Item: fieldNamed(t, field, "SPLIT"),
			Axes: layoutmodel.Axes{Charset: layoutmodel.ASCII},
		}},
		Redefines: []Redefine{{
			Item: fieldNamed(t, field, "BODY"),
			Alternatives: []Alternative{
				{Name: "BODY", Predicate: selects("B")},
				{Name: "SPLIT", Predicate: selects("S")},
			},
		}},
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	record := records[0]
	for _, name := range []string{"LEFT", "RIGHT"} {
		if got := encodingOf(t, record, name).Charset; got != layoutmodel.ASCII {
			t.Errorf("%s under the overridden arm carries the charset %q, want ascii", name, got)
		}
	}
	if got := encodingOf(t, record, "BODY").Charset; got != layoutmodel.CP037 {
		t.Errorf("the arm beside it carries the charset %q, want the profile's cp037", got)
	}
}

// TestAnOverrideNamingAnItemOfAnotherRecordReachesNothing, so that a caller may
// hand over the layout's whole list rather than filtering it per record.
func TestAnOverrideNamingAnItemOfAnotherRecordReachesNothing(t *testing.T) {
	t.Parallel()

	elsewhere := recordOf(t, "01 OTHER.\n   05 A PIC X(4).\n")

	record := resolveUnder(t, "01 R.\n   05 A PIC X(4).\n", mainframe(), func(*copybook.Field) []EncodingOverride {
		return []EncodingOverride{{
			Item: fieldNamed(t, elsewhere, "A"),
			Axes: layoutmodel.Axes{Charset: layoutmodel.ASCII},
		}}
	})

	// The two records name their items the same, which is what makes the match
	// an identity rather than a name.
	if got := encodingOf(t, record, "A").Charset; got != layoutmodel.CP037 {
		t.Errorf("A carries the charset %q, want the profile's cp037", got)
	}
}

// TestResolveRejectsAProfileWithAHoleInIt, naming every axis nobody stated:
// there is no default for one, and completing it here would be the guess made
// once and applied to every field of the record.
func TestResolveRejectsAProfileWithAHoleInIt(t *testing.T) {
	t.Parallel()

	profile := mainframe()
	profile.SignConvention = ""
	profile.FloatFormat = ""

	_, err := Resolve(recordOf(t, "01 R.\n   05 A PIC X(4).\n"), Options{
		Copybook: "test.cpy",
		Dialect:  copybook.IBMEnterprise(),
		Encoding: profile,
	})

	var incomplete *IncompleteProfileError
	if !errors.As(err, &incomplete) {
		t.Fatalf("resolving reported %v, want an IncompleteProfileError", err)
	}
	if len(incomplete.Axes) != 2 {
		t.Fatalf("the fault names %v, want both unstated axes", incomplete.Axes)
	}

	// The axes are named as a layout writes them, so an adopter can search for
	// the word they would have typed.
	for _, want := range []string{"sign-convention", "float-format"} {
		if !strings.Contains(incomplete.Error(), want) {
			t.Errorf("the message does not name %s: %s", want, incomplete.Error())
		}
	}
}

// TestResolveRejectsAnEmptyProfile is the same fault where nobody stated
// anything, which is what a caller who forgot the field entirely produces.
func TestResolveRejectsAnEmptyProfile(t *testing.T) {
	t.Parallel()

	_, err := Resolve(recordOf(t, "01 R.\n   05 A PIC X(4).\n"), Options{
		Copybook: "test.cpy",
		Dialect:  copybook.IBMEnterprise(),
	})

	var incomplete *IncompleteProfileError
	if !errors.As(err, &incomplete) {
		t.Fatalf("resolving reported %v, want an IncompleteProfileError", err)
	}
	if len(incomplete.Axes) != 4 {
		t.Fatalf("the fault names %v, want all four axes", incomplete.Axes)
	}
}

// TestTheCompletenessAssertionCatchesAFieldWithAnUnsetAxis drives the assertion
// against a record no resolution produces, because none can: a complete profile
// with overrides over it is complete.
//
// That is what it is for. docs/ir/SPEC.md puts the requirement on what leaves
// resolution and forbids a consumer the only repair — a field missing an axis is
// a malformed descriptor and **MUST NOT** be filled in — so the check has to be
// over the result rather than over the argument, and a check that cannot be
// reached through the front door is checked through this one.
func TestTheCompletenessAssertionCatchesAFieldWithAnUnsetAxis(t *testing.T) {
	t.Parallel()

	record := recordOf(t, "01 R.\n   05 A PIC X(4).\n")
	r := &resolver{opts: Options{Copybook: "test.cpy"}, record: record}

	held := &Node{Kind: KindField, Field: fieldNamed(t, record, "A"), width: 4}
	held.Encoding = mainframe()
	held.Encoding.ByteOrder = ""

	r.assertResolved([]*Record{{
		Root: &Node{Kind: KindGroup, Field: record, Members: []*Node{held, slackNode(2)}},
		Item: record,
	}})

	var unresolved *UnresolvedEncodingError
	if !errors.As(r.faults.Err(), &unresolved) {
		t.Fatalf("the assertion reported %v, want an UnresolvedEncodingError", r.faults.Err())
	}
	if unresolved.Item != "A" || len(unresolved.Axes) != 1 {
		t.Fatalf("the fault is %+v, want A missing one axis", unresolved)
	}
	if got := unresolved.Diagnostic().Spans[0].File; got != "test.cpy" {
		t.Errorf("the fault points at %q, want the copybook", got)
	}
	if !strings.Contains(unresolved.Error(), "byte-order") {
		t.Errorf("the message does not name byte-order: %s", unresolved.Error())
	}
}

// TestTheCompletenessAssertionPassesEveryResolutionThisPackageProduces is the
// other side of it, run over the shapes that carry field nodes in the awkward
// places: inside a variant's arms, inside a group this package introduced, and
// beside slack.
func TestTheCompletenessAssertionPassesEveryResolutionThisPackageProduces(t *testing.T) {
	t.Parallel()

	field := recordOf(t, tableSource)
	records, err := Resolve(field, Options{
		Copybook: "test.cpy",
		Dialect:  copybook.IBMEnterprise(),
		Encoding: mainframe(),
		Redefines: []Redefine{{
			Item: fieldNamed(t, field, "BODY"),
			Alternatives: []Alternative{
				{Name: "BODY", Predicate: selects("B")},
				{Name: "SPLIT", Predicate: selects("S")},
			},
		}},
	})
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	r := &resolver{opts: Options{Copybook: "test.cpy"}, record: field}
	r.assertResolved(records)
	if r.faults.Failed() {
		t.Fatalf("the assertion faulted on a resolution this package produced: %v", r.faults.Err())
	}
}

// payloadSource is the shape #275 is about, and the shape a group override has
// to be able to reach across: a status flag and a region identifier whose bytes
// are a payload, beside items of every kind whose bytes are not.
const payloadSource = `01 REGION-REC.
   05 R-NAME PIC X(20).
   05 R-STATUS PIC X.
   05 R-CODE PIC X(2).
   05 R-COUNT PIC 9(4).
   05 R-AMOUNT PIC S9(7)V99 COMP-3.
   05 R-INDEX PIC S9(4) COMP.
`

// noneOn is the override a layout writes to say an item carries a payload, with
// the position the layout wrote it at, so that a diagnostic about it names the
// line an adopter can change.
func noneOn(t *testing.T, record *copybook.Field, name string) EncodingOverride {
	t.Helper()

	return EncodingOverride{
		Pos:  layout.Pos{File: "orders.sexpr", Line: 7, Column: 1},
		Item: fieldNamed(t, record, name),
		Axes: layoutmodel.Axes{Charset: layoutmodel.None},
	}
}

// TestAnOverrideSaysAnItemCarriesNoCharacters is docs/layout/SPEC.md's "A byte
// is not a character, and such an item has no charset" as this package meets it:
// the item the override names carries `none`, every other item carries the
// profile's code page, and the other three axes are left where they were.
func TestAnOverrideSaysAnItemCarriesNoCharacters(t *testing.T) {
	t.Parallel()

	record := resolveUnder(t, payloadSource, mainframe(), func(field *copybook.Field) []EncodingOverride {
		return []EncodingOverride{noneOn(t, field, "R-STATUS")}
	})

	if got := encodingOf(t, record, "R-STATUS").Charset; got != layoutmodel.None {
		t.Errorf("the overridden item carries the charset %q, want none", got)
	}

	// The rest of the axes are the profile's on the overridden item too: an
	// override restates the axes it names and no others, and `none` is one
	// value of one axis rather than a statement about the item's encoding.
	axes := encodingOf(t, record, "R-STATUS")
	if axes.SignConvention != mainframe().SignConvention ||
		axes.ByteOrder != mainframe().ByteOrder ||
		axes.FloatFormat != mainframe().FloatFormat {
		t.Errorf("the overridden item carries %+v, want the profile's other three axes", axes)
	}

	for _, name := range []string{"R-NAME", "R-CODE", "R-COUNT", "R-AMOUNT", "R-INDEX"} {
		if got := encodingOf(t, record, name).Charset; got != layoutmodel.CP037 {
			t.Errorf("%s carries the charset %q, want the profile's cp037", name, got)
		}
	}
}

// TestCharsetNoneOnAGroupReachesTheItemsUnderItAndFaultsOnNone is the
// inertness the group case depends on: `none` reaches every elementary item
// under the group it names, governs the alphanumeric ones, and is as
// unremarkable on the packed and binary ones as a code page already is.
//
// The zoned item is left out of the group deliberately — it is the one item
// under a mixed group that `none` cannot mean anything for, and it has a test of
// its own below.
func TestCharsetNoneOnAGroupReachesTheItemsUnderItAndFaultsOnNone(t *testing.T) {
	t.Parallel()

	record := resolveUnder(t, `01 R.
   05 PAYLOAD.
      10 P-FLAG PIC X.
      10 P-BODY.
         15 P-DEEP PIC X(4).
      10 P-AMOUNT PIC S9(7)V99 COMP-3.
      10 P-INDEX PIC S9(4) COMP.
   05 OUTSIDE PIC X(4).
`, mainframe(), func(field *copybook.Field) []EncodingOverride {
		return []EncodingOverride{noneOn(t, field, "PAYLOAD")}
	})

	for _, name := range []string{"P-FLAG", "P-DEEP", "P-AMOUNT", "P-INDEX"} {
		if got := encodingOf(t, record, name).Charset; got != layoutmodel.None {
			t.Errorf("%s under the overridden group carries the charset %q, want none", name, got)
		}
	}

	if got := encodingOf(t, record, "OUTSIDE").Charset; got != layoutmodel.CP037 {
		t.Errorf("the item beside the overridden group carries the charset %q, want the profile's cp037", got)
	}
}

// TestCharsetNoneIsHeldToItemsItCanMeanSomethingFor is the diagnostic half.
//
// Three answers, one per case: legal on an alphanumeric DISPLAY item, a fault on
// any other DISPLAY item, and inert on every usage the charset does not govern.
// The third is what lets an override name a group holding numbers, so a change
// that made `none` a fault everywhere it is not read would pass the first two
// cases and break the shape the story is about.
func TestCharsetNoneIsHeldToItemsItCanMeanSomethingFor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		item   string
		fault  string
	}{
		{
			name:   "an alphanumeric DISPLAY item is what the value is for",
			source: "01 R.\n   05 F PIC X(4).\n",
			item:   "F",
		},
		{
			name:   "a zoned item's digit zone is read through the charset",
			source: "01 R.\n   05 F PIC 9(4).\n",
			item:   "F",
			fault: "in record R, charset none reaches F, and a zoned decimal item's digit zone and separate" +
				" sign are read through the charset, so it would be left with no reading at all",
		},
		{
			name:   "a signed zoned item, whose separate sign is read through it too",
			source: "01 R.\n   05 F PIC S9(4) SIGN LEADING SEPARATE.\n",
			item:   "F",
			fault: "in record R, charset none reaches F, and a zoned decimal item's digit zone and separate" +
				" sign are read through the charset, so it would be left with no reading at all",
		},
		{
			name:   "an alphabetic item is characters by declaration",
			source: "01 R.\n   05 F PIC A(4).\n",
			item:   "F",
			fault:  "in record R, charset none reaches F, and an item of category alphabetic is characters by declaration",
		},
		{
			name:   "a numeric-edited item is characters by declaration",
			source: "01 R.\n   05 F PIC ZZ9.99.\n",
			item:   "F",
			fault:  "in record R, charset none reaches F, and an item of category numeric-edited is characters by declaration",
		},
		{
			name:   "an alphanumeric-edited item is characters by declaration",
			source: "01 R.\n   05 F PIC XXBXX.\n",
			item:   "F",
			fault: "in record R, charset none reaches F, and an item of category alphanumeric-edited is" +
				" characters by declaration",
		},
		{
			name:   "a packed item is one the charset does not govern",
			source: "01 R.\n   05 F PIC S9(7)V99 COMP-3.\n",
			item:   "F",
		},
		{
			name:   "a binary item is one the charset does not govern",
			source: "01 R.\n   05 F PIC S9(4) COMP.\n",
			item:   "F",
		},
		{
			name:   "a COMP-6 item is one the charset does not govern",
			source: "01 R.\n   05 F PIC 9(4) COMP-6.\n",
			item:   "F",
		},
		{
			name:   "a COMP-5 item is one the charset does not govern",
			source: "01 R.\n   05 F PIC S9(4) COMP-5.\n",
			item:   "F",
		},
		{
			name:   "a floating point item is one the charset does not govern",
			source: "01 R.\n   05 F COMP-1.\n",
			item:   "F",
		},
		{
			name:   "an INDEX item is one the charset does not govern",
			source: "01 R.\n   05 F USAGE INDEX.\n",
			item:   "F",
		},
		{
			name:   "a POINTER item is one the charset does not govern",
			source: "01 R.\n   05 F USAGE POINTER.\n",
			item:   "F",
		},
		{
			// A FILLER is reached the same way and reported against never: no
			// accessor is generated for it and no item reference can name it,
			// so the diagnostic would name an item the adopter cannot address.
			name:   "a FILLER a group override reaches, whatever its PICTURE says",
			source: "01 R.\n   05 G.\n      10 FILLER PIC 9(4).\n      10 F PIC X(2).\n",
			item:   "G",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			field := recordOf(t, testCase.source)
			_, err := Resolve(field, Options{
				Copybook:          "test.cpy",
				Dialect:           copybook.IBMEnterprise(),
				Encoding:          mainframe(),
				EncodingOverrides: []EncodingOverride{noneOn(t, field, testCase.item)},
			})

			var fault *CharsetNoneError
			if testCase.fault == "" {
				if errors.As(err, &fault) {
					t.Fatalf("charset none reported %v, and the charset does not govern this item", err)
				}
				if err != nil {
					t.Fatalf("resolving: %v", err)
				}

				return
			}

			if !errors.As(err, &fault) {
				t.Fatalf("resolving reported %v, want a CharsetNoneError", err)
			}

			want := "orders.sexpr:7:1: " + testCase.fault + "\n  test.cpy:2:4: " + testCase.item + " is declared here"
			if got := fault.Error(); got != want {
				t.Errorf("reported\n%s\nwant\n%s", got, want)
			}
		})
	}
}

// TestCharsetNoneNamesTheLayoutAndTheCopybook is docs/layout/SPEC.md's "Every
// diagnostic carries a span, and some carry two" for this fault: the override is
// the line the adopter wrote and the copybook may not be theirs to change, so a
// diagnostic naming only one of them names the half they cannot fix.
func TestCharsetNoneNamesTheLayoutAndTheCopybook(t *testing.T) {
	t.Parallel()

	field := recordOf(t, "01 R.\n   05 G.\n      10 QTY PIC 9(4).\n")
	_, err := Resolve(field, Options{
		Copybook:          "cpy/region.cpy",
		Dialect:           copybook.IBMEnterprise(),
		Encoding:          mainframe(),
		EncodingOverrides: []EncodingOverride{noneOn(t, field, "G")},
	})

	var fault *CharsetNoneError
	if !errors.As(err, &fault) {
		t.Fatalf("resolving reported %v, want a CharsetNoneError", err)
	}

	spans := fault.Diagnostic().Spans
	if len(spans) != 2 {
		t.Fatalf("the fault carries %d spans, want the layout's and the copybook's", len(spans))
	}

	// The layout span is the override the adopter wrote, and not the group it
	// named: the group is where the value arrived, and the form is where it
	// was written.
	if spans[0].File != "orders.sexpr" || spans[0].Line != 7 {
		t.Errorf("the first span is %s, want the encoding-override in the layout", spans[0])
	}

	if spans[1].File != "cpy/region.cpy" || spans[1].Note == "" {
		t.Errorf("the second span is %s %q, want the item's entry and a note saying so", spans[1], spans[1].Note)
	}
}

// TestAnInnerOverrideRestoringACodePageIsNotAFault is why the check reads the
// encoding in effect at the item rather than the overrides as written: an
// adopter who says a group is payload and then says one item in it is text has
// said something consistent, and reporting the outer statement against the inner
// item would be a diagnostic about a layout that is right.
func TestAnInnerOverrideRestoringACodePageIsNotAFault(t *testing.T) {
	t.Parallel()

	record := resolveUnder(t, `01 R.
   05 G.
      10 FLAG PIC X.
      10 QTY PIC 9(4).
`, mainframe(), func(field *copybook.Field) []EncodingOverride {
		return []EncodingOverride{
			noneOn(t, field, "G"),
			{
				Item: fieldNamed(t, field, "QTY"),
				Axes: layoutmodel.Axes{Charset: layoutmodel.ASCII},
			},
		}
	})

	if got := encodingOf(t, record, "FLAG").Charset; got != layoutmodel.None {
		t.Errorf("FLAG carries the charset %q, want the group's none", got)
	}

	if got := encodingOf(t, record, "QTY").Charset; got != layoutmodel.ASCII {
		t.Errorf("QTY carries the charset %q, want the inner override's ascii", got)
	}
}
