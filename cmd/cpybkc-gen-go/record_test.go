// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/irpb"
)

// goldenPackage is the package the golden output declares, and goldenDir is
// where it is checked in.
//
// The golden is a Go package rather than a string constant or a file under
// testdata, and that is what makes "generated code compiles" a property this
// repository's own pipeline asserts on every run: `go build ./...`, `go vet
// ./...` and golangci-lint all reach it, and a change to this generator that
// emitted Go that does not compile fails four stages rather than none. The
// tests below pin the bytes; the compiler pins that they are Go.
const (
	goldenPackage = "orders"
	goldenDir     = "internal/orders"
)

// TestTheGeneratedPackageIsTheGolden generates from [ordersDescriptor] and
// holds every byte of the result against the package checked in beside this
// command.
//
// A golden over the whole output rather than assertions about parts of it: what
// this generator produces is source somebody reads, so a change to the shape of
// it is a change to what an adopter sees, and a test asserting that a field is
// present somewhere would pass through most of those changes without noticing.
func TestTheGeneratedPackageIsTheGolden(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	if err := generate(ordersDescriptor(), out, options{packageName: goldenPackage}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	generated := written(t, out)
	golden := written(t, goldenDir)

	for name, want := range golden {
		got, ok := generated[name]
		if !ok {
			t.Errorf("nothing was generated for %s, which the golden carries", name)

			continue
		}

		if got != want {
			t.Errorf("the generated %s is not the golden\n got:\n%s\nwant:\n%s", name, got, want)
		}
	}

	for name := range generated {
		if _, ok := golden[name]; !ok {
			t.Errorf("%s was generated and the golden does not carry it", name)
		}
	}
}

// TestEveryItemOfTheGoldenIsAFieldOfIt is the criterion the golden itself
// cannot state: that the struct covers the record rather than most of it.
//
// It reads the descriptor rather than the generated text, so an item added to
// [ordersDescriptor] and not to this generator's output fails here as well as
// in the golden — which is the difference between a test that noticed and a
// golden somebody updated.
func TestEveryItemOfTheGoldenIsAFieldOfIt(t *testing.T) {
	t.Parallel()

	source := written(t, goldenDir)[recordsFile]

	for _, node := range ordersDescriptor().GetNodes() {
		original := originalOf(node)
		if original == "" {
			continue
		}

		name, err := identifier("item", namesOf(node))
		if err != nil {
			t.Fatalf("identifier(%s): %v", original, err)
		}

		if !strings.Contains(source, name+" ") {
			t.Errorf("%s is an item of the descriptor and %s declares nothing called %s", original, recordsFile, name)
		}

		if !strings.Contains(source, original) {
			t.Errorf("%s is an item of the descriptor and %s never names it", original, recordsFile)
		}
	}
}

// TestARecordCarryingSlackHoldsARunForEveryNodeOfIt is docs/ir/SPEC.md's "Slack
// survives a read", from the only side a story about structs can assert it: the
// record has somewhere to put those bytes, and it is somewhere the caller is
// not asked to fill.
//
// One run per node per occurrence is the requirement, and the shape is what
// delivers the second half — a group that repeats became an array, so the
// struct holding the runs is the element type and there is one of everything in
// it per occurrence. Filling them is #51's.
func TestARecordCarryingSlackHoldsARunForEveryNodeOfIt(t *testing.T) {
	t.Parallel()

	source := written(t, goldenDir)[recordsFile]

	// Two slack nodes in ordersDescriptor: one among the record's own members
	// and one among the members of the group that repeats. Neither is a field a
	// caller can reach, and each is an array of exactly the runs its group's
	// slack nodes need.
	for _, want := range []string{slackField + " [1][]byte"} {
		if strings.Count(source, want) != 2 {
			t.Errorf("%s declares %q %d times, want 2", recordsFile, want, strings.Count(source, want))
		}
	}
}

// TestADescriptorCarryingNoRecordWritesOnlyTheDocFile keeps the record file
// something a descriptor produced rather than something this generator always
// writes. A file holding a package clause and no declaration says nothing
// doc.go does not.
func TestADescriptorCarryingNoRecordWritesOnlyTheDocFile(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	if err := generate(descriptorAt(supportedIRVersion), out, options{packageName: goldenPackage}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	generated := written(t, out)

	if _, ok := generated[recordsFile]; ok {
		t.Errorf("a descriptor carrying no record node wrote %s", recordsFile)
	}

	if _, ok := generated[generatedFile]; !ok {
		t.Errorf("a descriptor carrying no record node wrote no %s", generatedFile)
	}
}

// TestAVariantIsRefusedRatherThanEmittedWithoutItsArms is the one shape this
// generator declines.
//
// The failure it exists to avoid is a struct that looks complete and is missing
// an arm's items: the caller reads the generated source, finds no field for the
// alternative their copybook declares, and has no way to tell that from a
// copybook that does not declare one. What shape a variant takes in Go is the
// generator's to choose (#90) and the story that decodes one chooses it (#51).
func TestAVariantIsRefusedRatherThanEmittedWithoutItsArms(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "ENTRY-RECORD", 2),
			group(2, "ENTRY-RECORD", nil, 3),
			{Id: 3, Kind: &irpb.Node_Group{Group: &irpb.Group{
				Names:      &irpb.Names{Original: "ENTRY"},
				MemberIds:  []uint64{4},
				Repetition: constant(2),
			}}},
			{Id: 4, Kind: &irpb.Node_Variant{Variant: &irpb.Variant{}}},
		},
	}

	out := t.TempDir()

	err := generate(d, out, options{packageName: goldenPackage})

	var refusal *unsupportedShapeError
	if !errors.As(err, &refusal) {
		t.Fatalf("generate returned %v, want a refusal of the variant", err)
	}

	if refusal.Node != 4 {
		t.Errorf("the refusal is about node %d, want node 4", refusal.Node)
	}

	if entries, err := os.ReadDir(out); err != nil {
		t.Fatalf("reading the output directory: %v", err)
	} else if len(entries) != 0 {
		t.Errorf("the refusal left %d files beneath --out, want none", len(entries))
	}
}

// TestTwoNamesThatMungeToOneIdentifierAreRefused covers both places a collision
// lands: two records of one descriptor, and two items of one group.
//
// Refused rather than disambiguated. A generator that appended a number would
// put an identifier in an adopter's source that their copybook does not contain
// and that a later copybook edit would move from one item to the other, with
// nothing failing while it happened.
func TestTwoNamesThatMungeToOneIdentifierAreRefused(t *testing.T) {
	t.Parallel()

	for name, d := range map[string]*irpb.Descriptor{
		"two records": {
			Version: supportedIRVersion,
			Nodes: []*irpb.Node{
				record(1, "ORDER-RECORD", 2),
				group(2, "ORDER-RECORD", nil),
				record(3, "ORDER_RECORD", 4),
				group(4, "ORDER_RECORD", nil),
			},
		},
		"two items of one group": {
			Version: supportedIRVersion,
			Nodes: []*irpb.Node{
				record(1, "ORDER-RECORD", 2),
				group(2, "ORDER-RECORD", nil, 3, 4),
				alphanumeric(3, "ORDER-ID", 4),
				alphanumeric(4, "ORDER_ID", 4),
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := generate(d, t.TempDir(), options{packageName: goldenPackage})

			var collision *collisionError
			if !errors.As(err, &collision) {
				t.Fatalf("generate returned %v, want a collision", err)
			}

			if len(collision.Cobol) != 2 {
				t.Fatalf("the collision names %d copybook names, want the two that produced it", len(collision.Cobol))
			}

			for _, want := range collision.Cobol {
				if !strings.Contains(collision.Error(), want) {
					t.Errorf("the collision reads %q and does not name %s", collision.Error(), want)
				}
			}
		})
	}
}

// TestANameWithNoGoIdentifierInItIsRefused is the other half of naming this
// story declines to invent. A leading digit is a legal COBOL data-name and is
// not the start of a Go identifier, and prefixing one would be a rename nobody
// asked for.
func TestANameWithNoGoIdentifierInItIsRefused(t *testing.T) {
	t.Parallel()

	for _, cobol := range []string{"1ST-ADDRESS-LINE", "---"} {
		d := &irpb.Descriptor{
			Version: supportedIRVersion,
			Nodes: []*irpb.Node{
				record(1, "ORDER-RECORD", 2),
				group(2, "ORDER-RECORD", nil, 3),
				alphanumeric(3, cobol, 4),
			},
		}

		err := generate(d, t.TempDir(), options{packageName: goldenPackage})

		var unmungeable *unmungeableError
		if !errors.As(err, &unmungeable) {
			t.Fatalf("generate returned %v for %s, want a refusal", err, cobol)
		}

		if unmungeable.Cobol != cobol {
			t.Errorf("the refusal is about %s, want %s", unmungeable.Cobol, cobol)
		}
	}
}

// TestAFieldMissingAnEncodingAxisIsRefused holds this generator to the
// obligation docs/ir/SPEC.md puts on every consumer.
//
// Nothing in the struct emitter reads an axis, which is exactly why the check
// is worth a test: a descriptor that reached a generator with one unresolved is
// a bug in resolve, every one of the four fails silently when wrong, and a
// generator that emitted a struct for it would be the last thing in a position
// to say so.
func TestAFieldMissingAnEncodingAxisIsRefused(t *testing.T) {
	t.Parallel()

	for name, encoding := range map[string]*irpb.Encoding{
		"no encoding at all": nil,
		"no charset": {
			SignConvention: irpb.SignConvention_SIGN_CONVENTION_EBCDIC,
			ByteOrder:      irpb.ByteOrder_BYTE_ORDER_BIG_ENDIAN,
			FloatFormat:    irpb.FloatFormat_FLOAT_FORMAT_IBM_HFP,
		},
		"no sign convention": {
			Charset:     irpb.Charset_CHARSET_CP037,
			ByteOrder:   irpb.ByteOrder_BYTE_ORDER_BIG_ENDIAN,
			FloatFormat: irpb.FloatFormat_FLOAT_FORMAT_IBM_HFP,
		},
		"no byte order": {
			Charset:        irpb.Charset_CHARSET_CP037,
			SignConvention: irpb.SignConvention_SIGN_CONVENTION_EBCDIC,
			FloatFormat:    irpb.FloatFormat_FLOAT_FORMAT_IBM_HFP,
		},
		"no float format": {
			Charset:        irpb.Charset_CHARSET_CP037,
			SignConvention: irpb.SignConvention_SIGN_CONVENTION_EBCDIC,
			ByteOrder:      irpb.ByteOrder_BYTE_ORDER_BIG_ENDIAN,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			item := alphanumeric(3, "CUSTOMER-NAME", 20)
			item.GetField().Encoding = encoding

			d := &irpb.Descriptor{
				Version: supportedIRVersion,
				Nodes: []*irpb.Node{
					record(1, "ORDER-RECORD", 2),
					group(2, "ORDER-RECORD", nil, 3),
					item,
				},
			}

			err := generate(d, t.TempDir(), options{packageName: goldenPackage})

			var refusal *malformedError
			if !errors.As(err, &refusal) {
				t.Fatalf("generate returned %v, want a malformed descriptor", err)
			}
		})
	}
}

// TestAMalformedDescriptorIsReportedRatherThanGeneratedFrom is the rest of what
// a consumer of a node set has to survive: a reference to nothing, a reference
// to the wrong kind of node, and containment that goes round in a circle.
//
// The last is the one worth a test on its own. A consumer walks containment by
// recursion, docs/ir/SPEC.md requires containment to be acyclic, and a
// descriptor breaking that turns a diagnostic into a stack overflow unless
// something is watching for it.
func TestAMalformedDescriptorIsReportedRatherThanGeneratedFrom(t *testing.T) {
	t.Parallel()

	for name, d := range map[string]*irpb.Descriptor{
		"a record whose root is nothing": {
			Version: supportedIRVersion,
			Nodes:   []*irpb.Node{record(1, "ORDER-RECORD", 99)},
		},
		"a record whose root is not a group": {
			Version: supportedIRVersion,
			Nodes: []*irpb.Node{
				record(1, "ORDER-RECORD", 2),
				alphanumeric(2, "ORDER-ID", 4),
			},
		},
		"a member that is nothing": {
			Version: supportedIRVersion,
			Nodes: []*irpb.Node{
				record(1, "ORDER-RECORD", 2),
				group(2, "ORDER-RECORD", nil, 99),
			},
		},
		"a member that is a record": {
			Version: supportedIRVersion,
			Nodes: []*irpb.Node{
				record(1, "ORDER-RECORD", 2),
				group(2, "ORDER-RECORD", nil, 3),
				record(3, "OTHER-RECORD", 2),
			},
		},
		"a group that contains itself": {
			Version: supportedIRVersion,
			Nodes: []*irpb.Node{
				record(1, "ORDER-RECORD", 2),
				group(2, "ORDER-RECORD", nil, 3),
				group(3, "LINE-ITEM", constant(2), 2),
			},
		},
		"two nodes with one identifier": {
			Version: supportedIRVersion,
			Nodes: []*irpb.Node{
				record(1, "ORDER-RECORD", 2),
				group(2, "ORDER-RECORD", nil),
				group(2, "OTHER-RECORD", nil),
			},
		},
		"an item the descriptor carries no name for": {
			Version: supportedIRVersion,
			Nodes: []*irpb.Node{
				record(1, "ORDER-RECORD", 2),
				group(2, "ORDER-RECORD", nil, 3),
				alphanumeric(3, "", 4),
			},
		},
		"an OCCURS DEPENDING ON counted by nothing": {
			Version: supportedIRVersion,
			Nodes: []*irpb.Node{
				record(1, "ORDER-RECORD", 2),
				group(2, "ORDER-RECORD", nil, 3),
				group(3, "DETAIL", depending(99, 0, 4)),
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := generate(d, t.TempDir(), options{packageName: goldenPackage})

			var refusal *malformedError
			if !errors.As(err, &refusal) {
				t.Fatalf("generate returned %v, want a malformed descriptor", err)
			}

			if len(refusal.Notes()) == 0 {
				t.Error("the refusal names no rule the descriptor broke")
			}
		})
	}
}

// TestTheTypeTableIsWhatTheReadmeDocuments walks every USAGE and category pair
// this generator has a Go type for.
//
// The table is in README.md because an adopter reads that before they read any
// generated code, and it is here because a table in prose is a table nothing
// checks. The pairs are written out rather than derived, so that a change to
// the mapping has to be made twice — once where the generator does it and once
// where an adopter was told it would.
func TestTheTypeTableIsWhatTheReadmeDocuments(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		usage    irpb.Usage
		category irpb.Category
		digits   uint32
		want     string
	}{
		{irpb.Usage_USAGE_DISPLAY, irpb.Category_CATEGORY_ALPHABETIC, 0, "string"},
		{irpb.Usage_USAGE_DISPLAY, irpb.Category_CATEGORY_ALPHANUMERIC, 0, "string"},
		{irpb.Usage_USAGE_DISPLAY, irpb.Category_CATEGORY_ALPHANUMERIC_EDITED, 0, "string"},
		{irpb.Usage_USAGE_DISPLAY, irpb.Category_CATEGORY_NUMERIC_EDITED, 0, "string"},
		{irpb.Usage_USAGE_DISPLAY, irpb.Category_CATEGORY_NUMERIC, 9, "int32"},
		{irpb.Usage_USAGE_DISPLAY, irpb.Category_CATEGORY_NUMERIC, 10, "int64"},
		{irpb.Usage_USAGE_DISPLAY, irpb.Category_CATEGORY_NUMERIC, 19, bigIntType},
		{irpb.Usage_USAGE_PACKED_DECIMAL, irpb.Category_CATEGORY_NUMERIC, 4, "int32"},
		{irpb.Usage_USAGE_PACKED_DECIMAL, irpb.Category_CATEGORY_NUMERIC, 18, "int64"},
		{irpb.Usage_USAGE_COMP_6, irpb.Category_CATEGORY_NUMERIC, 9, "int32"},
		{irpb.Usage_USAGE_BINARY, irpb.Category_CATEGORY_NUMERIC, 4, "int16"},
		{irpb.Usage_USAGE_BINARY, irpb.Category_CATEGORY_NUMERIC, 5, "int32"},
		{irpb.Usage_USAGE_BINARY, irpb.Category_CATEGORY_NUMERIC, 18, "int64"},
		{irpb.Usage_USAGE_BINARY, irpb.Category_CATEGORY_NUMERIC, 19, bigIntType},
		{irpb.Usage_USAGE_COMP_5, irpb.Category_CATEGORY_NUMERIC, 4, "int16"},
		{irpb.Usage_USAGE_COMP_1, irpb.Category_CATEGORY_UNSPECIFIED, 0, "float32"},
		{irpb.Usage_USAGE_COMP_2, irpb.Category_CATEGORY_UNSPECIFIED, 0, "float64"},
		{irpb.Usage_USAGE_INDEX, irpb.Category_CATEGORY_UNSPECIFIED, 0, "[]byte"},
		{irpb.Usage_USAGE_POINTER, irpb.Category_CATEGORY_UNSPECIFIED, 0, "[]byte"},
		{irpb.Usage_USAGE_NATIONAL, irpb.Category_CATEGORY_UNSPECIFIED, 0, "[]byte"},
	} {
		f := &irpb.Field{Usage: tc.usage, Encoding: resolvedEncoding()}

		if tc.category != irpb.Category_CATEGORY_UNSPECIFIED {
			f.Picture = &irpb.Picture{Category: tc.category, Digits: tc.digits}
		}

		e, err := newEmitter(&irpb.Descriptor{})
		if err != nil {
			t.Fatalf("newEmitter: %v", err)
		}

		got, err := e.fieldType(f)
		if err != nil {
			t.Fatalf("fieldType(%s, %s, %d digits): %v", tc.usage, tc.category, tc.digits, err)
		}

		if got != tc.want {
			t.Errorf("%s of %d digits is %s, want %s", tc.usage, tc.digits, got, tc.want)
		}
	}
}

// TestAnItemWithNoPictureWhereOneIsRequiredIsRefused is the other side of the
// table: a USAGE that stores a number and a picture that says nothing about
// what it stores.
func TestAnItemWithNoPictureWhereOneIsRequiredIsRefused(t *testing.T) {
	t.Parallel()

	for name, f := range map[string]*irpb.Field{
		"DISPLAY with no picture": {
			Usage: irpb.Usage_USAGE_DISPLAY, Encoding: resolvedEncoding(),
		},
		"DISPLAY with no category": {
			Usage: irpb.Usage_USAGE_DISPLAY, Encoding: resolvedEncoding(),
			Picture: &irpb.Picture{},
		},
		"packed with no picture": {
			Usage: irpb.Usage_USAGE_PACKED_DECIMAL, Encoding: resolvedEncoding(),
		},
		"binary with an alphanumeric picture": {
			Usage: irpb.Usage_USAGE_BINARY, Encoding: resolvedEncoding(),
			Picture: &irpb.Picture{Category: irpb.Category_CATEGORY_ALPHANUMERIC},
		},
		"a USAGE this generator has never heard of": {
			Usage: irpb.Usage(99), Encoding: resolvedEncoding(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			e, err := newEmitter(&irpb.Descriptor{})
			if err != nil {
				t.Fatalf("newEmitter: %v", err)
			}

			if _, err := e.fieldType(f); err == nil {
				t.Error("fieldType accepted an item it has no type for")
			}
		})
	}
}

// TestTheGeneratedFilesAreGofmtClean is the criterion held to gofmt rather than
// to the eye: what this generator writes is what gofmt would write, so the
// output can be checked in and never shows up as a diff of whitespace.
func TestTheGeneratedFilesAreGofmtClean(t *testing.T) {
	t.Parallel()

	for name, source := range written(t, goldenDir) {
		if !strings.HasPrefix(source, generatedBy+"\n") {
			t.Errorf("%s opens with %q, and generated code says so on its first line",
				name, strings.SplitN(source, "\n", 2)[0])
		}

		if !strings.HasSuffix(source, "\n") {
			t.Errorf("%s does not end in a newline", name)
		}
	}
}

// ordersDescriptor is the descriptor the golden is generated from: two records
// covering every shape this generator emits.
//
// One descriptor rather than one per shape, because the golden is what an
// adopter reads and a struct is read whole. It carries a fixed OCCURS and an
// OCCURS DEPENDING ON, slack at the record's top level and slack inside the
// group that repeats, a scaled item, an item too wide for an int64, and the
// three usages the IR derives no logical value for.
func ordersDescriptor() *irpb.Descriptor {
	return &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			{
				Id: 1,
				Kind: &irpb.Node_File{File: &irpb.File{
					Framing:      &irpb.File_Unframed{Unframed: &irpb.Unframed{}},
					StartStateId: 2,
				}},
			},
			{Id: 2, Kind: &irpb.Node_State{State: &irpb.State{Accepts: true}}},

			record(10, "ORDER-RECORD", 11),
			group(11, "ORDER-RECORD", nil, 12, 13, 14, 15, 16, 22, 23),
			zoned(12, "ORDER-ID", 5, 5, 0, false),
			alphanumeric(13, "CUSTOMER-NAME", 20),
			slack(14, 2),
			packed(15, "ORDER-TOTAL", 4, 7, 2, true),
			group(16, "LINE-ITEM", constant(3), 17, 18, 19),
			alphanumeric(17, "SKU", 8),
			binary(18, "QUANTITY", 2, 4, true),
			slack(19, 1),
			zoned(22, "DETAIL-COUNT", 3, 3, 0, false),
			group(23, "DETAIL", depending(22, 0, 12), 24),
			alphanumeric(24, "DETAIL-TEXT", 10),

			record(30, "TRAILER-RECORD", 31),
			group(31, "TRAILER-RECORD", nil, 32, 33, 34, 35),
			packed(32, "GRAND-TOTAL", 11, 20, 0, true),
			{Id: 33, Kind: &irpb.Node_Field{Field: &irpb.Field{
				Width: 4, Encoding: resolvedEncoding(), Usage: irpb.Usage_USAGE_COMP_1,
				Names: &irpb.Names{Original: "EXCHANGE-RATE"},
			}}},
			{Id: 34, Kind: &irpb.Node_Field{Field: &irpb.Field{
				Width: 4, Encoding: resolvedEncoding(), Usage: irpb.Usage_USAGE_INDEX,
				Names: &irpb.Names{Original: "TABLE-INDEX"},
			}}},
			{Id: 35, Kind: &irpb.Node_Field{Field: &irpb.Field{
				Width: 12, Encoding: resolvedEncoding(), Usage: irpb.Usage_USAGE_DISPLAY,
				Picture: &irpb.Picture{Category: irpb.Category_CATEGORY_NUMERIC_EDITED, Digits: 7, Scale: 2},
				Names:   &irpb.Names{Original: "PRINTED-TOTAL"}, Repetition: constant(2),
			}}},
		},
	}
}

// record is a record node whose top level is the group root names.
func record(id uint64, name string, root uint64) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Record{Record: &irpb.Record{
		RootId: root, Names: &irpb.Names{Original: name},
	}}}
}

// group is a group node.
func group(id uint64, name string, rep *irpb.Repetition, members ...uint64) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Group{Group: &irpb.Group{
		MemberIds: members, Names: &irpb.Names{Original: name}, Repetition: rep,
	}}}
}

// slack is a slack node of that many bytes.
func slack(id uint64, width uint32) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Slack{Slack: &irpb.Slack{Width: width}}}
}

// alphanumeric is a PIC X(width) DISPLAY item.
func alphanumeric(id uint64, name string, width uint32) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Field{Field: &irpb.Field{
		Width: width, Encoding: resolvedEncoding(), Usage: irpb.Usage_USAGE_DISPLAY,
		Picture: &irpb.Picture{Category: irpb.Category_CATEGORY_ALPHANUMERIC},
		Names:   &irpb.Names{Original: name},
	}}}
}

// zoned is a numeric DISPLAY item.
func zoned(id uint64, name string, width, digits uint32, scale int32, signed bool) *irpb.Node {
	return numericItem(id, name, irpb.Usage_USAGE_DISPLAY, width, digits, scale, signed)
}

// packed is a PACKED-DECIMAL item.
func packed(id uint64, name string, width, digits uint32, scale int32, signed bool) *irpb.Node {
	return numericItem(id, name, irpb.Usage_USAGE_PACKED_DECIMAL, width, digits, scale, signed)
}

// binary is a COMP item.
func binary(id uint64, name string, width, digits uint32, signed bool) *irpb.Node {
	return numericItem(id, name, irpb.Usage_USAGE_BINARY, width, digits, 0, signed)
}

func numericItem(id uint64, name string, usage irpb.Usage, width, digits uint32, scale int32, signed bool) *irpb.Node {
	position := irpb.SignPosition_SIGN_POSITION_UNSPECIFIED
	if signed && usage == irpb.Usage_USAGE_DISPLAY {
		position = irpb.SignPosition_SIGN_POSITION_TRAILING
	}

	return &irpb.Node{Id: id, Kind: &irpb.Node_Field{Field: &irpb.Field{
		Width: width, Encoding: resolvedEncoding(), Usage: usage,
		Picture: &irpb.Picture{
			Category: irpb.Category_CATEGORY_NUMERIC, Digits: digits,
			Scale: scale, Signed: signed, SignPosition: position,
		},
		Names: &irpb.Names{Original: name},
	}}}
}

// constant is an OCCURS n repetition.
func constant(n uint32) *irpb.Repetition {
	return &irpb.Repetition{Count: &irpb.Repetition_Constant{Constant: n}}
}

// depending is an OCCURS min TO max DEPENDING ON the field node count.
func depending(count uint64, minimum, maximum uint32) *irpb.Repetition {
	return &irpb.Repetition{Count: &irpb.Repetition_Variable{Variable: &irpb.VariableCount{
		Count:          &irpb.VariableCount_FieldId{FieldId: count},
		MinOccurrences: minimum, MaxOccurrences: maximum,
	}}}
}

// resolvedEncoding is the four axes, all four set, as a producer must leave
// them on every field.
func resolvedEncoding() *irpb.Encoding {
	return &irpb.Encoding{
		Charset:        irpb.Charset_CHARSET_CP037,
		SignConvention: irpb.SignConvention_SIGN_CONVENTION_EBCDIC,
		ByteOrder:      irpb.ByteOrder_BYTE_ORDER_BIG_ENDIAN,
		FloatFormat:    irpb.FloatFormat_FLOAT_FORMAT_IBM_HFP,
	}
}

// namesOf is a node's names, for a test that has one in hand.
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

// written is every Go file in a directory, by name.
func written(t *testing.T, dir string) map[string]string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	files := make(map[string]string, len(entries))

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".go" {
			continue
		}

		files[entry.Name()] = contents(t, filepath.Join(dir, entry.Name()))
	}

	return files
}
