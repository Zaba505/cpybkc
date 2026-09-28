// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"go/parser"
	"go/token"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/irpb"
)

// signsDescriptor is the golden for the literals a re-expression moves by
// something other than a character: four record types told apart by the byte
// at offset zero — one by a character, two by the sign byte of a signed zoned
// digit and one by the first byte of a binary count.
//
// Under cp037 and the EBCDIC convention the first three are F5, C5 and D5: the
// character `5`, and the digit 5 in the positive and the negative column. A
// transfer to ASCII with translated-EBCDIC signs spells them 35, 45 and 4E —
// three bytes still, because it moves each by the axis that reaches it
// (docs/ir/SPEC.md, "Each axis carries a literal the way a file crosses it").
// The native ASCII convention ascii-zone-3-7 spells +5 as the character `5`,
// and a reader built under it cannot tell a TEXT-RECORD from a POSITIVE-RECORD.
// That is the pair the overlap re-check exists for, and this is the golden
// whose literals.go carries it — and carries every file of package emitted but
// float.go, so that each of them is compiled and linted in a generated package
// and not only in its own.
func signsDescriptor() *irpb.Descriptor {
	kind := func(id uint64) *irpb.Node { return zoned(id, "KIND", 1, 1, 0, true) }

	return &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			{Id: 1, Kind: &irpb.Node_File{File: &irpb.File{
				Framing:      &irpb.File_DescriptorWord{DescriptorWord: &irpb.DescriptorWord{}},
				StartStateId: 2,
			}}},

			{Id: 2, Kind: &irpb.Node_State{State: &irpb.State{Accepts: true, TransitionIds: []uint64{10, 11, 12, 13}}}},

			edge(10, 100, 2, predicateOn(50), nil, nil),
			edge(11, 110, 2, predicateOn(51), nil, nil),
			edge(12, 120, 2, predicateOn(52), nil, nil),
			edge(13, 130, 2, predicateOn(53), nil, nil),

			equals(50, 101, "\xf5"),
			equals(51, 111, "\xc5"),
			equals(52, 121, "\xd5"),
			equals(53, 131, "\x00\x07"),

			record(100, "TEXT-RECORD", 105),
			group(105, "TEXT-RECORD", nil, 101, 102),
			alphanumeric(101, "CODE", 1),
			alphanumeric(102, "TEXT-NOTE", 4),

			record(110, "POSITIVE-RECORD", 115),
			group(115, "POSITIVE-RECORD", nil, 111, 112),
			kind(111),
			alphanumeric(112, "POSITIVE-NOTE", 4),

			record(120, "NEGATIVE-RECORD", 125),
			group(125, "NEGATIVE-RECORD", nil, 121, 122),
			kind(121),
			alphanumeric(122, "NEGATIVE-NOTE", 4),

			record(130, "COUNT-RECORD", 135),
			group(135, "COUNT-RECORD", nil, 131, 132),
			binary(131, "COUNT", 2, 4, true),
			alphanumeric(132, "COUNT-NOTE", 4),
		},
	}
}

// TestAnItemDecidesHowItsLiteralMoves holds the classification literals.go
// re-expresses by to the table docs/ir/SPEC.md, "Each axis carries a literal
// the way a file crosses it", draws: which axis reaches which item, and the one
// byte of a signed zoned item the sign convention reaches.
func TestAnItemDecidesHowItsLiteralMoves(t *testing.T) {
	t.Parallel()

	leading := zoned(1, "LEADING", 3, 3, 0, true)
	leading.GetField().GetPicture().SignPosition = irpb.SignPosition_SIGN_POSITION_LEADING

	separate := zoned(1, "SEPARATE", 4, 3, 0, true)
	separate.GetField().GetPicture().SignPosition = irpb.SignPosition_SIGN_POSITION_TRAILING_SEPARATE

	float := &irpb.Field{Width: 4, Usage: irpb.Usage_USAGE_COMP_1, Encoding: resolvedEncoding()}

	for name, tc := range map[string]struct {
		field  *irpb.Field
		moves  movement
		signAt int
	}{
		"text":                      {field: alphanumeric(1, "TEXT", 2).GetField(), moves: characters, signAt: -1},
		"edited text":               {field: categorized(1, "EDITED", 6, irpb.Category_CATEGORY_NUMERIC_EDITED).GetField(), moves: characters, signAt: -1},
		"an unsigned zoned item":    {field: zoned(1, "UNSIGNED", 3, 3, 0, false).GetField(), moves: characters, signAt: -1},
		"a trailing overpunch":      {field: zoned(1, "TRAILING", 3, 3, 0, true).GetField(), moves: overpunched, signAt: 2},
		"a leading overpunch":       {field: leading.GetField(), moves: overpunched, signAt: 0},
		"a separate sign":           {field: separate.GetField(), moves: characters, signAt: -1},
		"a binary item":             {field: binary(1, "COMP", 2, 4, true).GetField(), moves: reversed, signAt: -1},
		"a COMP-5 item":             {field: comp5(1, "COMP5", 4, 9, false).GetField(), moves: reversed, signAt: -1},
		"a float":                   {field: float, moves: valued, signAt: -1},
		"a packed item":             {field: packed(1, "PACKED", 3, 5, 0, true).GetField(), moves: stays, signAt: -1},
		"a COMP-6 item":             {field: comp6(1, "COMP6", 2, 4, 0).GetField(), moves: stays, signAt: -1},
		"an item with no charset":   {field: opaque(1, "BYTES", 2).GetField(), moves: stays, signAt: -1},
		"a sign on a packed item":   {field: packed(1, "SIGNED", 2, 3, 0, true).GetField(), moves: stays, signAt: -1},
		"a charset on a binary one": {field: noCharset(binary(1, "BIN", 2, 4, false)).GetField(), moves: reversed, signAt: -1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := movementOf(tc.field); got != tc.moves {
				t.Errorf("moves %d, want %d", got, tc.moves)
			}

			if got := signAtOf(tc.field); got != tc.signAt {
				t.Errorf("the sign byte is %d, want %d", got, tc.signAt)
			}
		})
	}
}

// TestOnlyAPairAReExpressionCouldBringTogetherIsChecked holds the table to
// checking the pairs whose proof `resolve` gave does not carry across a
// re-expression, and only those: two text literals are moved through one code
// page and stay apart by construction, and the goldens whose every literal is
// text carry no check at all.
func TestOnlyAPairAReExpressionCouldBringTogetherIsChecked(t *testing.T) {
	t.Parallel()

	lits, err := gatherLiterals(signsDescriptor())
	if err != nil {
		t.Fatalf("gatherLiterals: %v", err)
	}

	if _, err := fileMachineWith(signsDescriptor(), options{packageName: "signs"}, lits); err != nil {
		t.Fatalf("fileMachineWith: %v", err)
	}

	var pairs []string
	for _, check := range lits.checks {
		pairs = append(pairs, check.one.record+"/"+check.other.record)
	}

	slices.Sort(pairs)

	// Every pair of the four, because no two of them are moved by one code
	// page alone: a character beside a sign byte, two sign bytes a convention
	// may spell alike, and a binary count beside each of the others.
	want := []string{
		"POSITIVE-RECORD/COUNT-RECORD",
		"POSITIVE-RECORD/NEGATIVE-RECORD",
		"NEGATIVE-RECORD/COUNT-RECORD",
		"TEXT-RECORD/COUNT-RECORD",
		"TEXT-RECORD/NEGATIVE-RECORD",
		"TEXT-RECORD/POSITIVE-RECORD",
	}
	slices.Sort(want)

	if !slices.Equal(pairs, want) {
		t.Errorf("the pairs checked are %v, want %v", pairs, want)
	}

	for dir, descriptor := range map[string]func() *irpb.Descriptor{
		"internal/counted": countedDescriptor, "internal/batched": batchedDescriptor,
	} {
		lits, err := gatherLiterals(descriptor())
		if err != nil {
			t.Fatalf("%s: gatherLiterals: %v", dir, err)
		}

		if _, err := fileMachineWith(descriptor(), options{packageName: "x"}, lits); err != nil {
			t.Fatalf("%s: fileMachineWith: %v", dir, err)
		}

		if len(lits.checks) != 0 {
			t.Errorf("%s compares text and nothing else, and checks %d pairs", dir, len(lits.checks))
		}
	}
}

// TestOnlyALiteralSomethingComparesIsDeclared holds literals.go to what the
// other two files compare. A predicate no transition or arm names is compared
// by nothing, and re-expressing it would refuse an encoding over a literal no
// record is ever held to.
func TestOnlyALiteralSomethingComparesIsDeclared(t *testing.T) {
	t.Parallel()

	d := batchedDescriptor()
	d.Nodes = append(d.Nodes, equals(59, 102, "\xe9\xe9\xe9\xe9\xe9\xe9\xe9\xe9\xe9\xe9\xe9\xe9\xe9\xe9\xe9\xe9\xe9\xe9"))

	lits, err := gatherLiterals(d)
	if err != nil {
		t.Fatalf("gatherLiterals: %v", err)
	}

	if len(lits.all) != 2 {
		t.Errorf("%d literals were gathered, and two predicates are named by a transition", len(lits.all))
	}
}

// TestAPackageComparingNothingWritesNoLiterals is the file's absence: the
// sequenced goldens carry no predicate and no bytes register, and a literals.go
// for them would declare a struct of nothing and the machinery to re-express
// it.
func TestAPackageComparingNothingWritesNoLiterals(t *testing.T) {
	t.Parallel()

	for _, dir := range []string{"internal/chunks", "internal/sep", "internal/opt"} {
		if _, err := os.Stat(dir + "/" + literalsFile); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s carries %s, and compares no literal", dir, literalsFile)
		}
	}

	out := t.TempDir()

	if err := generate(io.Discard, separatedDescriptor(), out, options{packageName: "sep", importPath: goldenModule + "internal/sep"}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	if _, ok := written(t, out)[literalsFile]; ok {
		t.Errorf("a package comparing no literal was given %s", literalsFile)
	}
}

// TestEveryEmittedFileIsOneTheGeneratorCanTake holds package emitted to what
// literalsSource needs of it: every file but the package's own doc parses, and
// has declarations below its import block to copy.
func TestEveryEmittedFileIsOneTheGeneratorCanTake(t *testing.T) {
	t.Parallel()

	entries, err := emittedSources.ReadDir("emitted")
	if err != nil {
		t.Fatalf("reading the embedded sources: %v", err)
	}

	taken := 0

	for _, entry := range entries {
		name := entry.Name()
		if name == "doc.go" || strings.HasSuffix(name, "_test.go") {
			continue
		}

		_, body, err := emittedFile(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)

			continue
		}

		if _, err := parser.ParseFile(token.NewFileSet(), name, "package x\n"+body, 0); err != nil {
			t.Errorf("%s does not parse once its import block is gone: %v", name, err)
		}

		if !strings.Contains(body, "\nfunc ") {
			t.Errorf("%s has nothing below its import block to copy", name)
		}

		taken++
	}

	if taken != 7 {
		t.Errorf("%d emitted files were taken, and literals.go names seven", taken)
	}
}

// TestARegisterBoundFromTwoItemsThatMoveDifferentlyIsRefused is the one shape
// literals.go does not emit, refused rather than read under one item's
// re-expression and compared against the other's.
func TestARegisterBoundFromTwoItemsThatMoveDifferentlyIsRefused(t *testing.T) {
	t.Parallel()

	d := countedDescriptor()
	d.Nodes = append(d.Nodes,
		binds(49, 21, 199),
		binary(199, "OTHER-FLAG", 2, 1, false),
	)

	_, err := gatherLiterals(d)

	var shared *sharedRegisterError
	if !errors.As(err, &shared) {
		t.Fatalf("got %v, want a sharedRegisterError", err)
	}

	if shared.Register != 21 {
		t.Errorf("the refusal names node %d, want 21", shared.Register)
	}
}

// steadyDescriptor is [batchedDescriptor] with both of its discriminators
// declared to carry bytes rather than characters, so that no literal it
// compares is one an axis a caller may replace reaches.
func steadyDescriptor() *irpb.Descriptor {
	d := batchedDescriptor()

	for _, node := range d.GetNodes() {
		if id := node.GetId(); id == 101 || id == 112 {
			noCharset(node)
		}
	}

	return d
}

// TestALiteralNoAxisReachesIsNeverReExpressed is "where nothing moves, nothing
// is re-expressed" over a whole package: a literal compared against an item
// whose charset is none is carried as it is under every encoding, so the
// package declares its literals and nothing that could move or refuse them.
func TestALiteralNoAxisReachesIsNeverReExpressed(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	if err := generate(io.Discard, steadyDescriptor(), out, options{packageName: "steady", importPath: goldenModule + "internal/steady"}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	source, ok := written(t, out)[literalsFile]
	if !ok {
		t.Fatalf("a package comparing two literals was given no %s", literalsFile)
	}

	if _, err := parser.ParseFile(token.NewFileSet(), literalsFile, source, 0); err != nil {
		t.Fatalf("the generated %s does not parse: %v\n%s", literalsFile, err, source)
	}

	for _, absent := range []string{"reexpress", "cannotReexpress", "literalsUnder", "resolvedAxes", "atomic"} {
		if strings.Contains(source, absent) {
			t.Errorf("a package whose literals no axis reaches carries %s:\n%s", absent, source)
		}
	}

	if !strings.Contains(source, "func literalsFor(codec.Encoding) (*literals, error) {\n\treturn &resolvedLiterals, nil\n}") {
		t.Errorf("literalsFor does not hand back the literals as resolved under every encoding:\n%s", source)
	}
}

// TestAGuardOverARegisterNoItemFillsIsGatheredRatherThanPanicking is the
// register no binding fills from an item — a descriptor reading it is one the
// generated reader reports as unbound at run time, and not one this gathering
// gets to crash on. Its literals are gathered against no item, which is the
// one reading that moves nothing: with no item there is no axis that reaches
// one.
func TestAGuardOverARegisterNoItemFillsIsGatheredRatherThanPanicking(t *testing.T) {
	t.Parallel()

	d := countedDescriptor()
	d.Nodes = slices.DeleteFunc(d.Nodes, func(node *irpb.Node) bool { return node.GetId() == 41 })

	lits, err := gatherLiterals(d)
	if err != nil {
		t.Fatalf("gatherLiterals: %v", err)
	}

	one, err := lits.ofGuard([]byte("\xe8"), 21)
	if err != nil {
		t.Fatalf("the guard's literal was not gathered: %v", err)
	}

	if one.field != nil || one.moves != stays {
		t.Errorf("a literal over a register no item fills is compared against %v and moves %d", one.field, one.moves)
	}

	if got := itemOf(one); got != "a register no binding fills from an item" {
		t.Errorf("a refusal would name the item %q", got)
	}
}
