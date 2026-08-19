// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"io"
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

// goldenModule is where the golden packages sit in this module, and
// goldenImport is the import path the one above answers to.
//
// The generated tests are an external test package, so they reach the package
// beside them by importing it, and the path is not something this generator can
// derive — `--out` is a scratch directory. Here it is a constant because these
// packages have a settled home; in a manifest it is `--opt import_path=<path>`.
// See [importPathOption].
const (
	goldenModule = "github.com/Zaba505/cpybkc/cmd/cpybkc-gen-go/"
	goldenImport = goldenModule + goldenDir
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

	if err := generate(io.Discard, ordersDescriptor(), out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
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

	// Neither is a field a caller can reach, and each is an array of exactly
	// the runs its group's slack nodes need.
	for want, times := range map[string]int{
		// One among ORDER-RECORD's own members, one among the members of the
		// group that repeats, and one inside the arm of the variant that is
		// shorter than its sibling.
		slackField + " [1][]byte": 3,

		// SYNC-RECORD carries two: the alignment gap ahead of its binary item
		// and the tail its items stop short of.
		slackField + " [2][]byte": 1,
	} {
		if got := strings.Count(source, want); got != times {
			t.Errorf("%s declares %q %d times, want %d", recordsFile, want, got, times)
		}
	}
}

// TestAFillerIsGeneratedRatherThanRefused is the case that makes the difference
// between this generator working on real copybooks and not.
//
// COBOL's FILLER has no data-name, so a producer emits no names message for it
// (internal/assemble/assemble.go's `names`), and FILLER is in most copybooks
// anybody actually has. Refusing it refused the whole package over an item
// nobody was looking at — and refused it as a *malformed descriptor*, which is
// what docs/ir/SPEC.md's "Names" says it is not.
//
// The two halves are different decisions and are asserted separately. An
// elementary FILLER has no value anybody named, so it gets no exported field
// and its bytes are retained beside the slack. A FILLER *group* holds members
// the copybook does name, which is the case a run of retained bytes could not
// stand in for, so its members are reached at the enclosing level — which is
// where COBOL's own qualification reaches them.
func TestAFillerIsGeneratedRatherThanRefused(t *testing.T) {
	t.Parallel()

	source := written(t, goldenDir)[recordsFile]

	// The FILLER item's bytes, retained beside ORDER-RECORD's slack rather than
	// in it: a slack node and a FILLER are two different facts about a record.
	if !strings.Contains(source, fillerField+" [1][]byte") {
		t.Errorf("%s declares no run for the FILLER of ORDER-RECORD", recordsFile)
	}

	// The FILLER group's member, at the level COBOL qualifies it at.
	for _, want := range []string{"NoteCode string", "// NoteCode is NOTE-CODE"} {
		if !strings.Contains(source, want) {
			t.Errorf("%s does not contain %q, and NOTE-CODE is a member of a FILLER group", recordsFile, want)
		}
	}

	// Nothing is *named* after an item the copybook did not name. A positional
	// name would be an identifier no copybook spells and one that moves the
	// moment an unrelated item is inserted ahead of it. Munging produces
	// exported identifiers, so `Filler` in any form is one — the word appears
	// in the retained run's doc comment as COBOL spells it and nowhere else.
	if strings.Contains(source, "Filler") {
		t.Errorf("%s declares an identifier munged from FILLER, which no copybook and no layout named", recordsFile)
	}
}

// TestAnItemCarryingNoNameIsRefused is the other half of the FILLER rule, and
// the descriptor the refusal was written for.
//
// A names message that is *present* and states no original is not an unnamed
// item: it is a named one whose name went missing, which is a bug in the
// producer and reads correctly as one. The two used to be the same test —
// `names.GetOriginal() == ""` is true of both — and collapsing them is what
// refused every copybook with a FILLER in it.
func TestAnItemCarryingNoNameIsRefused(t *testing.T) {
	t.Parallel()

	for name, node := range map[string]*irpb.Node{
		"a field": {Id: 3, Kind: &irpb.Node_Field{Field: &irpb.Field{
			Width: 4, Encoding: resolvedEncoding(), Usage: irpb.Usage_USAGE_DISPLAY,
			Picture: &irpb.Picture{Category: irpb.Category_CATEGORY_ALPHANUMERIC},
			Names:   &irpb.Names{},
		}}},
		"a group": {Id: 3, Kind: &irpb.Node_Group{Group: &irpb.Group{
			MemberIds: []uint64{4}, Names: &irpb.Names{},
		}}},
	} {
		d := &irpb.Descriptor{
			Version: supportedIRVersion,
			Nodes: []*irpb.Node{
				record(1, "ORDER-RECORD", 2),
				group(2, "ORDER-RECORD", nil, 3),
				node,
				alphanumeric(4, "NOTE-CODE", 2),
			},
		}

		err := generate(io.Discard, d, t.TempDir(), options{packageName: goldenPackage, importPath: goldenImport})

		var refusal *malformedError
		if !errors.As(err, &refusal) {
			t.Errorf("%s carrying a names message that states no name generated %v, want a malformed descriptor", name, err)
		}
	}
}

// TestAFillerThatCannotBePlacedIsRefusedAndIsNotCalledMalformed is the third
// shape a FILLER takes, where there is an honest refusal rather than a
// generated answer.
//
// A FILLER that repeats has no name for its occurrences to differ by, and one
// standing as an alternative of an alternation has no name to say it is the one
// an occurrence holds. Each is refused — and refused as the copybook item it is,
// naming what contains it, rather than as a malformed descriptor. The
// descriptor is exactly what docs/ir/SPEC.md admits, and telling an adopter to
// go and find a producer bug over an item they wrote correctly is the
// misdirection this whole story is about.
func TestAFillerThatCannotBePlacedIsRefusedAndIsNotCalledMalformed(t *testing.T) {
	t.Parallel()

	repeating := fillerItem(3, 4)
	repeating.GetField().Repetition = depending(5, 0, 4)

	// The group each refusal has to name is the innermost one the copybook
	// *does* name, and it is asserted exactly. A test satisfied by any of the
	// names in the descriptor would pass whichever part of this generator
	// reached the item first, which is the same as not checking: the whole
	// point of the message is that an adopter can go and search for the word in
	// it.
	for name, tc := range map[string]struct {
		descriptor *irpb.Descriptor
		in         string
	}{
		"a FILLER group that repeats": {
			in: "ORDER-RECORD",
			descriptor: &irpb.Descriptor{
				Version: supportedIRVersion,
				Nodes: []*irpb.Node{
					record(1, "ORDER-RECORD", 2),
					group(2, "ORDER-RECORD", nil, 3),
					repeated(fillerGroup(3, 4), constant(2)),
					alphanumeric(4, "NOTE-CODE", 2),
				},
			},
		},
		"a FILLER item counted by another": {
			in: "ORDER-RECORD",
			descriptor: &irpb.Descriptor{
				Version: supportedIRVersion,
				Nodes: []*irpb.Node{
					record(1, "ORDER-RECORD", 2),
					group(2, "ORDER-RECORD", nil, 5, 3),
					repeating,
					zoned(5, "NOTE-COUNT", 2, 2, 0, false),
				},
			},
		},
		// The item is inside ENTRY and ENTRY is inside ORDER-RECORD, so this is
		// the case that says which of the two the message names: the group the
		// adopter would find the item under, not the record it is somewhere in.
		"a FILLER standing as one alternative of an alternation": {
			in: "ENTRY",
			descriptor: &irpb.Descriptor{
				Version: supportedIRVersion,
				Nodes: []*irpb.Node{
					record(1, "ORDER-RECORD", 2),
					group(2, "ORDER-RECORD", nil, 3),
					group(3, "ENTRY", constant(2), 4, 5),
					alphanumeric(4, "ENTRY-TYPE", 1),
					variant(5, armOf(6, 7), armOf(8, 9)),
					equals(6, 4, "\xc4"),
					equals(8, 4, "\xe2"),
					alphanumeric(7, "ENTRY-DETAIL", 4),
					fillerItem(9, 4),
				},
			},
		},
		// A FILLER inside a FILLER: the members of both are lifted to the one
		// named level, so the group the refusal names is that level rather than
		// either of the two anonymous ones between.
		"a FILLER group that repeats inside another FILLER group": {
			in: "ORDER-RECORD",
			descriptor: &irpb.Descriptor{
				Version: supportedIRVersion,
				Nodes: []*irpb.Node{
					record(1, "ORDER-RECORD", 2),
					group(2, "ORDER-RECORD", nil, 3),
					fillerGroup(3, 4),
					repeated(fillerGroup(4, 5), constant(2)),
					alphanumeric(5, "NOTE-CODE", 2),
				},
			},
		},
	} {
		out := t.TempDir()

		err := generate(io.Discard, tc.descriptor, out, options{packageName: goldenPackage, importPath: goldenImport})

		var refusal *fillerError
		if !errors.As(err, &refusal) {
			t.Errorf("%s generated %v, want a refusal naming the item", name, err)

			continue
		}

		var wrong *malformedError
		if errors.As(err, &wrong) {
			t.Errorf("%s is reported as a malformed descriptor, and the descriptor says what docs/ir/SPEC.md admits", name)
		}

		if refusal.In != tc.in {
			t.Errorf("%s is refused in %q, want %q — the innermost group the copybook names", name, refusal.In, tc.in)
		}

		// "a item of ..." is what a hard-coded article produces, and this
		// message is the one the whole story is about.
		if !strings.HasPrefix(refusal.Error(), "a group ") && !strings.HasPrefix(refusal.Error(), "an item ") {
			t.Errorf("%s reads %q, which opens with neither article", name, refusal.Error())
		}

		if entries, err := os.ReadDir(out); err != nil {
			t.Fatalf("reading the output directory: %v", err)
		} else if len(entries) != 0 {
			t.Errorf("%s left %d files beneath --out, want none", name, len(entries))
		}
	}
}

// TestAFillerGroupInsideAFillerGroupIsFlattenedToTheNamedLevel is the recursive
// half of the flattening rule, and the one that says what "the enclosing level"
// means when there is more than one anonymous level to skip.
//
// COBOL qualification skips *every* unnamed group between an item and the name
// it is qualified by, so two levels of FILLER lift a member exactly as far as
// one does. The struct is the assertion: NOTE-CODE is a field of the record and
// there is no intermediate type between them.
func TestAFillerGroupInsideAFillerGroupIsFlattenedToTheNamedLevel(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "ORDER-RECORD", 2),
			group(2, "ORDER-RECORD", nil, 3, 7),
			fillerGroup(3, 4),
			fillerGroup(4, 5, 6),
			alphanumeric(5, "NOTE-CODE", 2),
			fillerItem(6, 3),
			alphanumeric(7, "ORDER-ID", 4),
		},
	}

	out := t.TempDir()

	if err := generate(io.Discard, d, out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	source := written(t, out)[recordsFile]

	// Both members reach the record's own struct, in record order, and the
	// FILLER two levels down contributes to the record's own run.
	for _, want := range []string{"NoteCode string", "OrderId string", fillerField + " [1][]byte"} {
		if !strings.Contains(source, want) {
			t.Errorf("%s does not contain %q", recordsFile, want)
		}
	}

	// One struct: the record's. An anonymous group that produced a type of its
	// own would be a type named after an item the copybook did not name.
	if got := strings.Count(source, "struct {"); got != 1 {
		t.Errorf("%s declares %d struct types, want 1", recordsFile, got)
	}
}

// TestAnUnnamedGroupThatContainsItselfIsRefused is the cycle the flattening
// walk can meet before any other guard does.
//
// An unnamed group is expanded in place, so one containing itself would be
// expanded for ever — and it would do so before the containment guard over
// struct types is reached, because no struct is emitted for it.
func TestAnUnnamedGroupThatContainsItselfIsRefused(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "ORDER-RECORD", 2),
			group(2, "ORDER-RECORD", nil, 3),
			fillerGroup(3, 4),
			fillerGroup(4, 3),
		},
	}

	var refusal *malformedError
	if err := generate(io.Discard, d, t.TempDir(), options{packageName: goldenPackage, importPath: goldenImport}); !errors.As(err, &refusal) {
		t.Fatalf("a FILLER group containing itself generated %v, want a malformed descriptor", err)
	}
}

// TestAMemberLiftedOutOfAFillerGroupCollidesLikeAnyOther is the cost of
// flattening, and the thing that has to be reported rather than resolved.
//
// Lifting a FILLER group's members into the enclosing struct can bring two
// names to one Go identifier that the copybook keeps at two levels. COBOL is
// content — the two are qualified differently there — and Go is not, so it is
// refused with the same diagnostic every other collision gets, naming both
// items.
func TestAMemberLiftedOutOfAFillerGroupCollidesLikeAnyOther(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "ORDER-RECORD", 2),
			group(2, "ORDER-RECORD", nil, 3, 4),
			alphanumeric(3, "NOTE-CODE", 2),
			fillerGroup(4, 5),
			alphanumeric(5, "NOTE_CODE", 2),
		},
	}

	err := generate(io.Discard, d, t.TempDir(), options{packageName: goldenPackage, importPath: goldenImport})

	var collision *collisionError
	if !errors.As(err, &collision) {
		t.Fatalf("two names lifted to one level generated %v, want a collision", err)
	}

	for _, want := range []string{"NOTE-CODE", "NOTE_CODE", "NoteCode"} {
		if !strings.Contains(collision.Error(), want) {
			t.Errorf("the collision reads %q and never names %s", collision.Error(), want)
		}
	}
}

// TestAFillerAloneDeclaresTheZeroFillItsWriterNeeds is the copybook this story
// is actually about: one with FILLER in it and no slack anywhere.
//
// Every golden package that carries a FILLER also carries a slack node, so the
// helper a writer emits zero bytes from is already declared in each of them for
// another reason. A record whose only retained run is a FILLER has to declare
// it on its own account, and it has to be wide enough for that run.
func TestAFillerAloneDeclaresTheZeroFillItsWriterNeeds(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "ORDER-RECORD", 2),
			group(2, "ORDER-RECORD", nil, 3, 4),
			alphanumeric(3, "ORDER-ID", 4),
			fillerItem(4, 6),
		},
	}

	out := t.TempDir()

	if err := generate(io.Discard, d, out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	if source := written(t, out)[codecFile]; !strings.Contains(source, "var zeroFill = make([]byte, 6)") {
		t.Errorf("a record whose only retained run is a FILLER of 6 bytes declares no zeroFill wide enough for it:\n%s", source)
	}
}

// TestAFillerInsideAnArmOfAnAlternationIsRetainedLikeAnyOther is the last place
// a FILLER can stand that the golden does not reach: inside the body of one arm
// of a variant.
//
// An arm's body is a group like any other, so its struct is emitted by the same
// walk and its FILLER is retained in that struct's own run — per arm, and per
// occurrence of the table the variant is in.
func TestAFillerInsideAnArmOfAnAlternationIsRetainedLikeAnyOther(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "ENTRY-RECORD", 2),
			group(2, "ENTRY-RECORD", nil, 3),
			group(3, "ENTRY", constant(2), 4, 5),
			alphanumeric(4, "ENTRY-TYPE", 1),
			variant(5, armOf(6, 8), armOf(7, 10)),
			equals(6, 4, "\xc4"),
			equals(7, 4, "\xe2"),
			group(8, "ENTRY-DETAIL", nil, 9, 12),
			alphanumeric(9, "DETAIL-SKU", 4),
			group(10, "ENTRY-SUMMARY", nil, 11),
			alphanumeric(11, "SUMMARY-TEXT", 6),
			fillerItem(12, 2),
		},
	}

	out := t.TempDir()

	if err := generate(io.Discard, d, out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	// The run belongs to the arm's own struct, so it is declared inside the
	// arm and the record itself carries none.
	source := written(t, out)[recordsFile]

	if got := strings.Count(source, fillerField+" [1][]byte"); got != 1 {
		t.Errorf("%s declares %d FILLER runs, want 1 — the one in the arm that carries it", recordsFile, got)
	}

	// And the two arms still cover the same bytes, which is what says the
	// FILLER was counted into the width of the arm holding it.
	codec := written(t, out)[codecFile]
	if !strings.Contains(codec, "ReadBytes(7)") {
		t.Errorf("%s reads an occurrence of ENTRY that is not 7 bytes wide:\n%s", codecFile, codec)
	}
}

// TestADescriptorCarryingNoRecordWritesOnlyTheDocFile keeps the record file
// something a descriptor produced rather than something this generator always
// writes. A file holding a package clause and no declaration says nothing
// doc.go does not.
func TestADescriptorCarryingNoRecordWritesOnlyTheDocFile(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	if err := generate(io.Discard, descriptorAt(supportedIRVersion), out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
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

// TestAVariantIsOneFieldPerArmAndNoInventedName is the shape this generator
// chose for a REDEFINES inside a repeating group, which docs/ir/SPEC.md leaves
// to it.
//
// Go has no sum type, so something has to say which alternative an occurrence
// holds. Every other spelling of that — a discriminant field, a type for it, a
// constant per arm — wants an identifier neither the copybook nor the layout
// wrote, and a name an adopter cannot predict is what this generator refuses to
// produce everywhere else. A pointer per arm says the same thing in names the
// copybook already spells.
func TestAVariantIsOneFieldPerArmAndNoInventedName(t *testing.T) {
	t.Parallel()

	source := written(t, goldenDir)[recordsFile]

	for _, want := range []string{
		"EntryDetail *struct",
		"EntrySummary *struct",
		"// EntryDetail is ENTRY-DETAIL",
		"// EntrySummary is ENTRY-SUMMARY",
		"exactly one of",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("%s does not contain %q", recordsFile, want)
		}
	}

	// The alternation itself has no name in the copybook, so it has none here:
	// nothing in the generated source is called after the choice rather than
	// after one of the things chosen between.
	for _, unwanted := range []string{"Which", "Arm ", "Discriminant"} {
		if strings.Contains(source, unwanted) {
			t.Errorf("%s declares %s, which no copybook and no layout named", recordsFile, unwanted)
		}
	}
}

// TestAVariantCarryingOneArmIsRefused is the producer bug the shape above
// cannot express: a choice between one thing.
func TestAVariantCarryingOneArmIsRefused(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "ENTRY-RECORD", 2),
			group(2, "ENTRY-RECORD", nil, 3),
			group(3, "ENTRY", constant(2), 4),
			variant(4),
		},
	}

	out := t.TempDir()

	err := generate(io.Discard, d, out, options{packageName: goldenPackage, importPath: goldenImport})

	var refusal *malformedError
	if !errors.As(err, &refusal) {
		t.Fatalf("generate returned %v, want a malformed descriptor", err)
	}

	if entries, err := os.ReadDir(out); err != nil {
		t.Fatalf("reading the output directory: %v", err)
	} else if len(entries) != 0 {
		t.Errorf("the refusal left %d files beneath --out, want none", len(entries))
	}
}

// TestTwoNamesThatMungeToOneIdentifierAreRefused covers every place a collision
// lands: two records of one descriptor, two items of one group, and a rename
// that lands on a name something else already munges to.
//
// Refused rather than disambiguated. A generator that appended a number would
// put an identifier in an adopter's source that their copybook does not contain
// and that a later copybook edit would move from one item to the other, with
// nothing failing while it happened. A rename is no exception: it is munged like
// any other name, so it collides like any other name.
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
		"a rename onto the name of the item beside it": {
			Version: supportedIRVersion,
			Nodes: []*irpb.Node{
				record(1, "ORDER-RECORD", 2),
				group(2, "ORDER-RECORD", nil, 3, 4),
				alphanumeric(3, "ORDER-ID", 4),
				renamed(alphanumeric(4, "CUSTOMER-NAME", 20), "order_id"),
			},
		},
		"a rename onto the name of the record beside it": {
			Version: supportedIRVersion,
			Nodes: []*irpb.Node{
				record(1, "ORDER-RECORD", 2),
				group(2, "ORDER-RECORD", nil),
				renamed(record(3, "TRAILER-RECORD", 4), "order-record"),
				group(4, "TRAILER-RECORD", nil),
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := generate(io.Discard, d, t.TempDir(), options{packageName: goldenPackage, importPath: goldenImport})

			var collision *collisionError
			if !errors.As(err, &collision) {
				t.Fatalf("generate returned %v, want a collision", err)
			}

			if len(collision.Cobol) != 2 {
				t.Fatalf("the collision names %d items, want the two that produced it", len(collision.Cobol))
			}

			// Both halves of what an adopter needs: the copybook's name, which
			// says which item it is, and the rename where a rename is what was
			// munged, which says which line of the layout to go and edit.
			for _, want := range collision.Cobol {
				if !strings.Contains(collision.Error(), want.Original) {
					t.Errorf("the collision reads %q and does not name %s", collision.Error(), want.Original)
				}

				if want.Override != "" && !strings.Contains(collision.Error(), want.Override) {
					t.Errorf("the collision reads %q and does not name the rename %s", collision.Error(), want.Override)
				}
			}
		})
	}
}

// TestARenameIsTheIdentifierAndTheCopybookNameIsStillOnThePage is what a rename
// override buys an adopter, on all three of the named node kinds.
//
// Two properties, and the second is the one a story about names could lose
// without noticing: the identifier is the rename's, and the copybook's own name
// is still in the generated source. An adopter reading the struct has to be able
// to get back to the item in the copybook, and a generator that substituted the
// name would leave them holding an identifier that appears nowhere in the
// copybook and nothing to connect the two.
func TestARenameIsTheIdentifierAndTheCopybookNameIsStillOnThePage(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			renamed(record(1, "ORDER-RECORD", 2), "purchase_order"),
			group(2, "ORDER-RECORD", nil, 3, 4, 5),
			renamed(alphanumeric(3, "CUST-NM", 20), "CustomerName"),
			alphanumeric(4, "SKU", 8),
			renamed(group(5, "LINE-ITEM", constant(2), 6), "Lines"),
			alphanumeric(6, "ITEM-CODE", 8),
		},
	}

	out := t.TempDir()

	if err := generate(io.Discard, d, out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	source := written(t, out)[recordsFile]

	for _, want := range []string{
		// The record, the field and the group each take the rename.
		"type PurchaseOrder struct",
		"CustomerName string",
		"Lines [2]struct",

		// And each says what the copybook calls it, and that the name it is
		// declared under came from the layout rather than from munging.
		"// PurchaseOrder is the ORDER-RECORD record",
		"// The layout renames it to purchase_order.",
		"// CustomerName is CUST-NM —",
		"// The layout renames it to CustomerName.",
		"// Lines is LINE-ITEM —",
		"// The layout renames it to Lines.",

		// The item the layout said nothing about keeps the copybook's name and
		// carries no note about a rename that did not happen.
		"// Sku is SKU — alphanumeric, DISPLAY, 8 bytes.\n\tSku string",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("%s does not contain %q\n%s", recordsFile, want, source)
		}
	}

	for _, unwanted := range []string{"OrderRecord", "CustNm", "LineItem "} {
		if strings.Contains(source, unwanted) {
			t.Errorf("%s declares %s, and the layout renamed it", recordsFile, unwanted)
		}
	}
}

// TestMungingIsWhatTheReadmeDocuments walks the rule an adopter is told, from
// both sides of it.
//
// A name written in one case throughout carries no casing of its own, so munging
// supplies it. A name written in more than one carries somebody's choice, so
// munging keeps it — which is what makes the rename override a control over the
// identifier rather than another string to be flattened, and what stands in for
// the table of initialisms this generator deliberately does not carry.
func TestMungingIsWhatTheReadmeDocuments(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]string{
		// One case throughout: each word capitalised, the rest lowered.
		"ORDER-ID":         "OrderId",
		"order_id":         "OrderId",
		"CUSTOMER NAME":    "CustomerName",
		"ADDRESS-2ND-LINE": "Address2ndLine",

		// More than one: the tail is left as it was written, and only the first
		// letter of each word is made a capital.
		"CustomerID": "CustomerID",
		"custId":     "CustId",
		"XMLDoc":     "XMLDoc",
		"order-ID":   "OrderID",
	} {
		if got := munge(name); got != want {
			t.Errorf("%s munges to %s, want %s", name, got, want)
		}
	}
}

// TestARenameWithNoGoIdentifierInItIsRefusedAndSaysSo is the unmungeable name
// again, one step along: the adopter has already renamed the item, and the
// rename is the name that will not munge.
//
// The diagnostic has to be about the rename. Telling somebody to rename an item
// they have just renamed sends them to a line they have already written and
// leaves them looking for a second one.
func TestARenameWithNoGoIdentifierInItIsRefusedAndSaysSo(t *testing.T) {
	t.Parallel()

	for _, override := range []string{"1st_address_line", "___"} {
		d := &irpb.Descriptor{
			Version: supportedIRVersion,
			Nodes: []*irpb.Node{
				record(1, "ORDER-RECORD", 2),
				group(2, "ORDER-RECORD", nil, 3),
				renamed(alphanumeric(3, "ADDRESS-LINE", 30), override),
			},
		}

		err := generate(io.Discard, d, t.TempDir(), options{packageName: goldenPackage, importPath: goldenImport})

		var unmungeable *unmungeableError
		if !errors.As(err, &unmungeable) {
			t.Fatalf("generate returned %v for the rename %s, want a refusal", err, override)
		}

		if unmungeable.Override != override {
			t.Errorf("the refusal is about the rename %q, want %s", unmungeable.Override, override)
		}

		if !strings.Contains(unmungeable.Error(), override) || !strings.Contains(unmungeable.Error(), "ADDRESS-LINE") {
			t.Errorf("the refusal reads %q, and names neither the rename nor the item", unmungeable.Error())
		}

		if len(unmungeable.Notes()) == 0 {
			t.Error("the refusal says nothing about what to change")
		}
	}
}

// TestARenameToNothingIsMalformedRatherThanIgnored is a descriptor carrying an
// override that is present and empty.
//
// A rename substitutes a name and the empty string is not one, so this is a bug
// in the producer rather than a layout an adopter can fix. Falling back to the
// copybook's name would generate from a rename that silently did nothing.
func TestARenameToNothingIsMalformedRatherThanIgnored(t *testing.T) {
	t.Parallel()

	d := &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "ORDER-RECORD", 2),
			group(2, "ORDER-RECORD", nil, 3),
			renamed(alphanumeric(3, "ADDRESS-LINE", 30), ""),
		},
	}

	err := generate(io.Discard, d, t.TempDir(), options{packageName: goldenPackage, importPath: goldenImport})

	var refusal *malformedError
	if !errors.As(err, &refusal) {
		t.Fatalf("generate returned %v, want a malformed descriptor", err)
	}

	if !strings.Contains(refusal.Error(), "ADDRESS-LINE") {
		t.Errorf("the refusal reads %q and does not name the item carrying the override", refusal.Error())
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

		err := generate(io.Discard, d, t.TempDir(), options{packageName: goldenPackage, importPath: goldenImport})

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

			err := generate(io.Discard, d, t.TempDir(), options{packageName: goldenPackage, importPath: goldenImport})

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

			err := generate(io.Discard, d, t.TempDir(), options{packageName: goldenPackage, importPath: goldenImport})

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
		signed   bool

		// charset is the item's, where it is not the one every other row is
		// read under. It is in the table because it is in the mapping: the
		// charset axis decides one row of it and nothing else here does.
		charset irpb.Charset

		want string
	}{
		{irpb.Usage_USAGE_DISPLAY, irpb.Category_CATEGORY_ALPHABETIC, 0, false, 0, "string"},
		{irpb.Usage_USAGE_DISPLAY, irpb.Category_CATEGORY_ALPHANUMERIC, 0, false, 0, "string"},
		{irpb.Usage_USAGE_DISPLAY, irpb.Category_CATEGORY_ALPHANUMERIC_EDITED, 0, false, 0, "string"},
		{irpb.Usage_USAGE_DISPLAY, irpb.Category_CATEGORY_NUMERIC_EDITED, 0, false, 0, "string"},
		{irpb.Usage_USAGE_DISPLAY, irpb.Category_CATEGORY_NUMERIC, 9, true, 0, "int32"},
		{irpb.Usage_USAGE_DISPLAY, irpb.Category_CATEGORY_NUMERIC, 10, true, 0, "int64"},
		{irpb.Usage_USAGE_DISPLAY, irpb.Category_CATEGORY_NUMERIC, 19, true, 0, bigIntType},

		// The one row a charset decides. A PIC X item a layout gave no charset
		// carries bytes rather than characters, so it is a []byte: a string
		// would lose every byte above 0x7F to encoding/json and every trailing
		// 0x20 to the trim ReadAlphanumeric applies.
		{irpb.Usage_USAGE_DISPLAY, irpb.Category_CATEGORY_ALPHANUMERIC, 0, false, irpb.Charset_CHARSET_NONE, "[]byte"},

		// A zoned or packed item takes a signed type whichever it is: the sign
		// lives in a zone or a nibble, so an unsigned one is a reading that
		// never comes back negative rather than a different accessor.
		{irpb.Usage_USAGE_DISPLAY, irpb.Category_CATEGORY_NUMERIC, 9, false, 0, "int32"},
		{irpb.Usage_USAGE_PACKED_DECIMAL, irpb.Category_CATEGORY_NUMERIC, 4, true, 0, "int32"},
		{irpb.Usage_USAGE_PACKED_DECIMAL, irpb.Category_CATEGORY_NUMERIC, 4, false, 0, "int32"},
		{irpb.Usage_USAGE_PACKED_DECIMAL, irpb.Category_CATEGORY_NUMERIC, 18, true, 0, "int64"},
		// A COMP-6 item is always unsigned — there is no sign nibble in the
		// field to hold an S — and it takes the packed family's Go types all
		// the same, because codec's COMP-6 accessors return them.
		{irpb.Usage_USAGE_COMP_6, irpb.Category_CATEGORY_NUMERIC, 9, false, 0, "int32"},
		{irpb.Usage_USAGE_COMP_6, irpb.Category_CATEGORY_NUMERIC, 18, false, 0, "int64"},
		{irpb.Usage_USAGE_COMP_6, irpb.Category_CATEGORY_NUMERIC, 19, false, 0, bigIntType},

		// A binary item stores two's complement, where the top bit is a digit
		// in an unsigned item and the sign in a signed one, so the S is what
		// picks the family and the Go type with it.
		{irpb.Usage_USAGE_BINARY, irpb.Category_CATEGORY_NUMERIC, 4, true, 0, "int16"},
		{irpb.Usage_USAGE_BINARY, irpb.Category_CATEGORY_NUMERIC, 5, true, 0, "int32"},
		{irpb.Usage_USAGE_BINARY, irpb.Category_CATEGORY_NUMERIC, 18, true, 0, "int64"},
		{irpb.Usage_USAGE_BINARY, irpb.Category_CATEGORY_NUMERIC, 19, true, 0, bigIntType},
		{irpb.Usage_USAGE_BINARY, irpb.Category_CATEGORY_NUMERIC, 1, false, 0, "uint64"},
		{irpb.Usage_USAGE_BINARY, irpb.Category_CATEGORY_NUMERIC, 4, false, 0, "uint64"},
		{irpb.Usage_USAGE_BINARY, irpb.Category_CATEGORY_NUMERIC, 9, false, 0, "uint64"},
		{irpb.Usage_USAGE_BINARY, irpb.Category_CATEGORY_NUMERIC, 18, false, 0, "uint64"},

		// Nineteen digits and up is sixteen bytes, where codec has only the
		// Big family: a *big.Int whether the PICTURE carries an S or not.
		{irpb.Usage_USAGE_BINARY, irpb.Category_CATEGORY_NUMERIC, 19, false, 0, bigIntType},
		{irpb.Usage_USAGE_COMP_5, irpb.Category_CATEGORY_NUMERIC, 4, true, 0, "int16"},
		{irpb.Usage_USAGE_COMP_5, irpb.Category_CATEGORY_NUMERIC, 4, false, 0, "uint64"},
		{irpb.Usage_USAGE_COMP_5, irpb.Category_CATEGORY_NUMERIC, 19, false, 0, bigIntType},

		{irpb.Usage_USAGE_COMP_1, irpb.Category_CATEGORY_UNSPECIFIED, 0, false, 0, "float32"},
		{irpb.Usage_USAGE_COMP_2, irpb.Category_CATEGORY_UNSPECIFIED, 0, false, 0, "float64"},
		{irpb.Usage_USAGE_INDEX, irpb.Category_CATEGORY_UNSPECIFIED, 0, false, 0, "[]byte"},
		{irpb.Usage_USAGE_POINTER, irpb.Category_CATEGORY_UNSPECIFIED, 0, false, 0, "[]byte"},
		{irpb.Usage_USAGE_NATIONAL, irpb.Category_CATEGORY_UNSPECIFIED, 0, false, 0, "[]byte"},
	} {
		f := &irpb.Field{Usage: tc.usage, Encoding: resolvedEncoding()}

		if tc.charset != irpb.Charset_CHARSET_UNSPECIFIED {
			f.Encoding.Charset = tc.charset
		}

		if tc.category != irpb.Category_CATEGORY_UNSPECIFIED {
			f.Picture = &irpb.Picture{Category: tc.category, Digits: tc.digits, Signed: tc.signed}

			if tc.signed && tc.usage == irpb.Usage_USAGE_DISPLAY {
				f.Picture.SignPosition = irpb.SignPosition_SIGN_POSITION_TRAILING
			}
		}

		e, err := newEmitter(&irpb.Descriptor{})
		if err != nil {
			t.Fatalf("newEmitter: %v", err)
		}

		got, err := e.fieldType(f)
		if err != nil {
			t.Fatalf("fieldType(%s, %s, %d digits, signed=%t): %v", tc.usage, tc.category, tc.digits, tc.signed, err)
		}

		if got != tc.want {
			t.Errorf("%s of %d digits, signed=%t, is %s, want %s", tc.usage, tc.digits, tc.signed, got, tc.want)
		}
	}
}

// TestASignedComp6ItemHasNoGoTypeEither is the type table's own share of the
// rule that a COMP-6 item is always unsigned.
//
// The refusal belongs to the usage rather than to one code path, and this is
// the half of that claim [TestASignedComp6ItemIsRefused] cannot make: that one
// generates a whole package, so it reaches the accessors, and a `fieldType`
// still handing back an `int32` for the item would not show up in it.
func TestASignedComp6ItemHasNoGoTypeEither(t *testing.T) {
	t.Parallel()

	e, err := newEmitter(&irpb.Descriptor{})
	if err != nil {
		t.Fatalf("newEmitter: %v", err)
	}

	got, err := e.fieldType(&irpb.Field{
		Usage: irpb.Usage_USAGE_COMP_6, Encoding: resolvedEncoding(),
		Picture: &irpb.Picture{Category: irpb.Category_CATEGORY_NUMERIC, Digits: 4, Signed: true},
		Names:   &irpb.Names{Original: "ON-HAND"},
	})
	if err == nil {
		t.Fatalf("fieldType gave a signed COMP-6 item the type %s, want a refusal", got)
	}

	var refusal *malformedError
	if !errors.As(err, &refusal) {
		t.Fatalf("fieldType returned %v, want a malformed descriptor", err)
	}

	if !strings.Contains(refusal.Error(), "ON-HAND") {
		t.Errorf("the refusal reads %q and does not name the item", refusal)
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
			// Descriptor-word rather than unframed, because two of these record
			// types have an extent that moves with a count and a fixed-length
			// dataset has no single number of bytes to pad one out to. See
			// docs/ir/SPEC.md, "A variable record does not fit a fixed-length
			// dataset".
			{
				Id: 1,
				Kind: &irpb.Node_File{File: &irpb.File{
					Framing:      &irpb.File_DescriptorWord{DescriptorWord: &irpb.DescriptorWord{}},
					StartStateId: 2,
				}},
			},

			// A record apiece, in a fixed order, each state offering the one
			// transition that carries no predicate. Nothing in these records
			// says which they are — the copybooks carry no type code — so the
			// automaton is what tells them apart, which is the shape
			// docs/ir/SPEC.md's "A transition may carry no predicate" admits.
			{Id: 2, Kind: &irpb.Node_State{State: &irpb.State{TransitionIds: []uint64{90}}}},
			{Id: 3, Kind: &irpb.Node_State{State: &irpb.State{TransitionIds: []uint64{91}}}},
			{Id: 4, Kind: &irpb.Node_State{State: &irpb.State{TransitionIds: []uint64{92}}}},
			{Id: 5, Kind: &irpb.Node_State{State: &irpb.State{TransitionIds: []uint64{93}}}},
			{Id: 6, Kind: &irpb.Node_State{State: &irpb.State{TransitionIds: []uint64{94}}}},
			{Id: 7, Kind: &irpb.Node_State{State: &irpb.State{Accepts: true}}},

			admits(90, 10, 3),
			admits(91, 50, 4),

			// The one transition of the five that carries a predicate, so that
			// a framing stating a record's length has a predicate to bound:
			// a target outside the record the framing bounds does not match.
			{Id: 92, Kind: &irpb.Node_Transition{Transition: &irpb.Transition{
				RecordId: 40, NextStateId: 5, PredicateId: new(uint64(95)),
			}}},
			equals(95, 42, "\xe8"),

			admits(93, 60, 6),
			admits(94, 30, 7),

			record(10, "ORDER-RECORD", 11),
			group(11, "ORDER-RECORD", nil, 12, 13, 14, 15, 16, 20, 21, 22, 23),
			renamed(zoned(12, "ORDER-ID", 5, 5, 0, false), "OrderID"),
			alphanumeric(13, "CUSTOMER-NAME", 20),
			slack(14, 2),
			packed(15, "ORDER-TOTAL", 4, 7, 2, true),
			group(16, "LINE-ITEM", constant(3), 17, 18, 19, 26),
			alphanumeric(17, "SKU", 8),
			binary(18, "QUANTITY", 2, 4, true),
			slack(19, 1),

			// The two shapes an item COBOL gives no data-name takes. The first
			// is a FILLER item, which gets no field and whose bytes are
			// retained; the second is a FILLER group, which cannot be retained
			// as bytes because it holds a member the copybook does name — and
			// that member is reached at this level, exactly as COBOL's own
			// qualification reaches it.
			fillerItem(20, 4),
			fillerGroup(21, 25),

			zoned(22, "DETAIL-COUNT", 3, 3, 0, false),
			group(23, "DETAIL", depending(22, 0, 12), 24),
			alphanumeric(24, "DETAIL-TEXT", 10),
			alphanumeric(25, "NOTE-CODE", 2),

			// A FILLER inside a group that repeats, which is what says the
			// retained run is per occurrence: LINE-ITEM's struct is the
			// element type, so each of the three elements holds its own.
			fillerItem(26, 1),

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

			// A record that is nothing but slack and the two items around it:
			// an alignment gap ahead of a SYNCHRONIZED binary item, and the
			// tail of a fixed-length record whose items stop short of LRECL.
			// The IR tells the two apart nowhere and deliberately — one slack
			// node per maximal run of uncovered bytes, whatever produced it.
			record(40, "SYNC-RECORD", 41),
			group(41, "SYNC-RECORD", nil, 42, 43, 44, 45),
			alphanumeric(42, "SYNC-FLAG", 1),
			slack(43, 3),
			binary(44, "SYNC-COUNTER", 4, 9, true),
			slack(45, 8),

			// One count sizing three tables, one of which is inside a group
			// repeating on that same count. The last is why a writer's
			// comparison is over every repetition naming a count rather than
			// over a pair: it contributes one number per occurrence of BLOCK.
			record(50, "TABLE-RECORD", 51),
			group(51, "TABLE-RECORD", nil, 52, 53, 55, 57),
			zoned(52, "PAIR-COUNT", 2, 2, 0, false),
			group(53, "LEFT-ITEM", depending(52, 0, 4), 54),
			alphanumeric(54, "LEFT-TEXT", 3),
			group(55, "RIGHT-ITEM", depending(52, 0, 4), 56),
			alphanumeric(56, "RIGHT-TEXT", 2),
			group(57, "BLOCK", depending(52, 0, 4), 58),
			group(58, "BLOCK-ITEM", depending(52, 0, 4), 59),
			alphanumeric(59, "BLOCK-TEXT", 1),

			// A REDEFINES inside a repeating group: a table of entries whose
			// body is one of two alternatives, chosen per occurrence by the
			// type code beside it. The second alternative is shorter than the
			// first, so it carries the slack that makes the two arms cover the
			// same bytes.
			record(60, "ENTRY-RECORD", 61),
			group(61, "ENTRY-RECORD", nil, 62),
			group(62, "ENTRY", constant(3), 63, 64),
			alphanumeric(63, "ENTRY-TYPE", 1),
			variant(64, armOf(65, 67), armOf(66, 70)),
			equals(65, 63, "\xc4"),
			equals(66, 63, "\xe2"),
			group(67, "ENTRY-DETAIL", nil, 68, 69),
			alphanumeric(68, "DETAIL-SKU", 4),
			binary(69, "DETAIL-QTY", 2, 4, true),
			group(70, "ENTRY-SUMMARY", nil, 71, 72),
			alphanumeric(71, "SUMMARY-TEXT", 4),
			slack(72, 2),

			// The item shapes the five records above do not reach, so that
			// every Go type this generator's table gives an item is one the
			// compiler and `go test -race` see: COMP-6, COMP-2, a COMP-5 item
			// wide enough for an int64, an unsigned COMP-5 one read through
			// codec's uint64 family, and the two USAGEs beside INDEX that the
			// IR derives no logical value for.
			//
			// No transition admits it, and that is the second thing it is here
			// for: a record type the automaton cannot reach is a layout bug an
			// adopter would want shown rather than silently skipped, so the
			// record tier makes a case for it like any other.
			record(80, "SHAPE-RECORD", 81),
			group(81, "SHAPE-RECORD", nil, 82, 83, 84, 85, 86, 87),
			comp6(82, "TALLY", 3, 5, 0),
			{Id: 83, Kind: &irpb.Node_Field{Field: &irpb.Field{
				Width: 8, Encoding: resolvedEncoding(), Usage: irpb.Usage_USAGE_COMP_2,
				Names: &irpb.Names{Original: "RATE"},
			}}},
			comp5(84, "WIDE-COUNT", 8, 18, true),
			comp5(85, "UNSIGNED-COUNT", 2, 4, false),
			{Id: 86, Kind: &irpb.Node_Field{Field: &irpb.Field{
				Width: 4, Encoding: resolvedEncoding(), Usage: irpb.Usage_USAGE_POINTER,
				Names: &irpb.Names{Original: "ANCHOR"},
			}}},
			{Id: 87, Kind: &irpb.Node_Field{Field: &irpb.Field{
				Width: 6, Encoding: resolvedEncoding(), Usage: irpb.Usage_USAGE_NATIONAL,
				Names: &irpb.Names{Original: "WIDE-TEXT"},
			}}},
		},
	}
}

// renamed is a node the layout gave a substitute name for, which is the name
// this generator munges into an identifier.
func renamed(node *irpb.Node, override string) *irpb.Node {
	namesOf(node).OverrideName = new(override)

	return node
}

// admits is a transition node carrying no predicate and no guards: it admits
// the record and moves to the state.
func admits(id, record, next uint64) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Transition{Transition: &irpb.Transition{
		RecordId: record, NextStateId: next,
	}}}
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

// fillerItem and fillerGroup are an elementary item and a group COBOL gave no
// data-name: a FILLER, and a FILLER group.
//
// Neither carries a names message at all, which is what a producer emits for
// one — internal/assemble/assemble.go's `names` returns nil for a FILLER, and
// docs/ir/SPEC.md's "Names" says what a *named* node carries rather than that
// every node is named. That is the descriptor this generator has to make a
// decision about, and it is a legal one.
func fillerItem(id uint64, width uint32) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Field{Field: &irpb.Field{
		Width: width, Encoding: resolvedEncoding(), Usage: irpb.Usage_USAGE_DISPLAY,
		Picture: &irpb.Picture{Category: irpb.Category_CATEGORY_ALPHANUMERIC},
	}}}
}

func fillerGroup(id uint64, members ...uint64) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Group{Group: &irpb.Group{MemberIds: members}}}
}

// repeated is a group node that occurs that many times.
func repeated(node *irpb.Node, rep *irpb.Repetition) *irpb.Node {
	node.GetGroup().Repetition = rep

	return node
}

// slack is a slack node of that many bytes.
func slack(id uint64, width uint32) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Slack{Slack: &irpb.Slack{Width: width}}}
}

// variant is a variant node: an ordered list of arms over one run of bytes.
func variant(id uint64, arms ...*irpb.Arm) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Variant{Variant: &irpb.Variant{Arms: arms}}}
}

// armOf is one alternative of a variant: the predicate that selects it and the
// group that is its body.
func armOf(predicate, body uint64) *irpb.Arm {
	return &irpb.Arm{PredicateId: predicate, Body: &irpb.Arm_GroupId{GroupId: body}}
}

// equals is a predicate node satisfied when a field's bytes are the literal,
// which the producer has already padded to the field's width.
func equals(id, field uint64, value string) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Predicate{Predicate: &irpb.Predicate{
		FieldId: field,
		Test:    &irpb.Predicate_BytesEqual{BytesEqual: &irpb.BytesEqual{Value: []byte(value)}},
	}}}
}

// alphanumeric is a PIC X(width) DISPLAY item.
func alphanumeric(id uint64, name string, width uint32) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Field{Field: &irpb.Field{
		Width: width, Encoding: resolvedEncoding(), Usage: irpb.Usage_USAGE_DISPLAY,
		Picture: &irpb.Picture{Category: irpb.Category_CATEGORY_ALPHANUMERIC},
		Names:   &irpb.Names{Original: name},
	}}}
}

// categorized is a DISPLAY item of a category [alphanumeric] does not cover:
// alphabetic, or one of the two edited ones.
//
// The digit count is what a numeric-edited picture carries and an alphabetic
// one does not, so it is left at zero and the width is what stands for the
// item's size, which is what every consumer here reads for a picture that is
// not numeric.
func categorized(id uint64, name string, width uint32, category irpb.Category) *irpb.Node {
	node := alphanumeric(id, name, width)
	node.GetField().GetPicture().Category = category

	return node
}

// opaque is a PIC X(width) DISPLAY item a layout gave no charset: the item
// whose bytes are a payload rather than characters.
//
// Every axis is still set — [opaqueDisplay] is a reading of the charset axis
// and not a licence to leave one unresolved — and the charset is the fifth
// value rather than the unset one.
func opaque(id uint64, name string, width uint32) *irpb.Node {
	return noCharset(alphanumeric(id, name, width))
}

// noCharset is the charset axis answering none on an item of any usage.
//
// [opaque] is the one shape that reading is admitted on. This is the same axis
// on the items beside it, which is what an `encoding-override` naming a *group*
// leaves behind: the override reaches every item under the group, and a group
// holds packed, binary and floating-point items the charset governs nothing of.
func noCharset(node *irpb.Node) *irpb.Node {
	node.GetField().GetEncoding().Charset = irpb.Charset_CHARSET_NONE

	return node
}

// zoned is a numeric DISPLAY item.
func zoned(id uint64, name string, width, digits uint32, scale int32, signed bool) *irpb.Node {
	return numericItem(id, name, irpb.Usage_USAGE_DISPLAY, width, digits, scale, signed)
}

// packed is a PACKED-DECIMAL item.
func packed(id uint64, name string, width, digits uint32, scale int32, signed bool) *irpb.Node {
	return numericItem(id, name, irpb.Usage_USAGE_PACKED_DECIMAL, width, digits, scale, signed)
}

// comp6 is a COMP-6 item: packed with no sign nibble, so its width is
// ceil(digits/2) rather than the packed ceil((digits+1)/2), and it carries no S.
func comp6(id uint64, name string, width, digits uint32, scale int32) *irpb.Node {
	return numericItem(id, name, irpb.Usage_USAGE_COMP_6, width, digits, scale, false)
}

// signedComp6 is the item docs/ir/SPEC.md says no producer may emit: a COMP-6
// picture carrying an operational sign there is nowhere in the field to store.
func signedComp6(id uint64, name string, width, digits uint32) *irpb.Node {
	return numericItem(id, name, irpb.Usage_USAGE_COMP_6, width, digits, 0, true)
}

// binary is a COMP item.
func binary(id uint64, name string, width, digits uint32, signed bool) *irpb.Node {
	return numericItem(id, name, irpb.Usage_USAGE_BINARY, width, digits, 0, signed)
}

// comp5 is a COMP-5 item, which is a binary one with TRUNC(BIN) range
// semantics: the same bytes, and the full range of the storage rather than the
// decimal range the PICTURE declares.
func comp5(id uint64, name string, width, digits uint32, signed bool) *irpb.Node {
	return numericItem(id, name, irpb.Usage_USAGE_COMP_5, width, digits, 0, signed)
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

// written is every generated Go file in a directory, by name.
//
// A `_test.go` file may or may not be one, and the header is what tells them
// apart: a file opening with the `// Code generated … DO NOT EDIT.` line
// [generatedBy] emits is output and is pinned byte for byte like everything
// else, and one that does not is this repository's own.
//
// Both kinds sit in the same directories. The generated cases are output —
// records_test.go is written by every generation that writes the files it
// covers — and the hand-written round-trip assertions have to run *inside* the
// golden package, because the bytes retained for a slack node are unexported
// and a wrong-length run is a thing only code in the package can hand a writer.
// So the golden pins one set and ignores the other, and the header is the
// signal because it is the one every generated file in this repository already
// carries and the one no hand-written file may.
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

		body := contents(t, filepath.Join(dir, entry.Name()))

		if strings.HasSuffix(entry.Name(), "_test.go") && !strings.HasPrefix(body, generatedBy) {
			continue
		}

		files[entry.Name()] = body
	}

	return files
}
