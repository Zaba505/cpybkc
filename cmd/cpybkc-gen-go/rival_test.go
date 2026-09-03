// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/irpb"
)

// writerBody is one method of the generated writer, printed back out of the
// parsed file so that an assertion is about what the emitter wrote rather than
// about where gofmt put it.
func writerBody(t *testing.T, d *irpb.Descriptor, method string) string {
	t.Helper()

	source, err := fileMachine(d, options{packageName: "batched", importPath: goldenModule + "internal/batched"})
	if err != nil {
		t.Fatalf("fileMachine: %v", err)
	}

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, fileMachineFile, source, parser.SkipObjectResolution|parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing the generated %s: %v\n%s", fileMachineFile, err, source)
	}

	for _, node := range file.Decls {
		decl, ok := node.(*ast.FuncDecl)
		if !ok || decl.Recv == nil || decl.Name.Name != method {
			continue
		}

		var b strings.Builder

		if err := printer.Fprint(&b, fset, decl); err != nil {
			t.Fatalf("printing %s: %v", method, err)
		}

		return b.String()
	}

	t.Fatalf("the generated %s declares no %s\n%s", fileMachineFile, method, source)

	return ""
}

// batchedWith is [batchedDescriptor] with the header's discriminator replaced,
// which is the one node these tests move.
func batchedWith(apply func(*irpb.Predicate)) *irpb.Descriptor {
	d := batchedDescriptor()

	for _, node := range d.GetNodes() {
		if node.GetId() == 50 {
			apply(node.GetPredicate())
		}
	}

	return d
}

// TestTheWriterEvaluatesTheTransitionsOrderedAheadOfTheOneItTook is
// docs/ir/SPEC.md's "A writer walks the same automaton", at the state its "A
// batch boundary is told by the order" admits.
//
// The narrowed walk is no longer proved to land where the reader lands there, so
// the writer spends the order too: the predicate of every transition ordered
// ahead of the one it took, against the bytes it is about to emit.
func TestTheWriterEvaluatesTheTransitionsOrderedAheadOfTheOneItTook(t *testing.T) {
	t.Parallel()

	writing := writerBody(t, batchedDescriptor(), "writeBatchDetail")

	// The rival's own matcher, called on the bytes about to go out rather than
	// on the window in front of a reader.
	if !strings.Contains(writing, "matches1At0(raw)") {
		t.Errorf("the writer does not evaluate the transition ordered ahead of the one it took:\n%s", writing)
	}

	// Four things, because a caller holding this message has no descriptor in
	// front of them: the record refused, the item whose value those bytes would
	// be read as, the run that did it, and the record type whose boundary it
	// would forge.
	for _, says := range []string{"BATCH-DETAIL", "HDR-TYPE", "bytes 0:2", "BATCH-HEADER"} {
		if !strings.Contains(writing, says) {
			t.Errorf("the refusal does not name %s:\n%s", says, writing)
		}
	}

	// And through the refusal every other refusal of this writer goes through,
	// rather than an error shape of its own.
	if !strings.Contains(writing, "return w.refuse(") {
		t.Errorf("the refusal does not go through the writer's own refusal path:\n%s", writing)
	}
}

// TestAStateWhoseTransitionsAreToldApartByTheirLiteralsEvaluatesNoRival is the
// other half of that rule, and is what keeps every golden in this repository
// byte for byte.
//
// Two transitions whose runs share a byte are told apart by the literals over
// the bytes they share, and a pair agreeing there is an ambiguity `resolve`
// refuses — so bytes satisfying one cannot satisfy the other and there is
// nothing for the writer to evaluate. Moving the header's discriminator onto the
// detail's own run is the whole of the difference.
func TestAStateWhoseTransitionsAreToldApartByTheirLiteralsEvaluatesNoRival(t *testing.T) {
	t.Parallel()

	// HDR-NAME covers bytes 2:20 of a header, and 10:12 of it is the run the
	// detail's discriminator reads, so the two now ask about one run.
	writing := writerBody(t, batchedWith(func(p *irpb.Predicate) {
		p.FieldId = 102
		p.Test = &irpb.Predicate_BytesEqual{BytesEqual: &irpb.BytesEqual{
			Value: []byte(strings.Repeat("\xc8", 18)),
		}}
	}), "writeBatchDetail")

	// The header is named nowhere in the method that writes a detail, which is
	// what "no rival evaluation is emitted" comes to in the source: neither its
	// matcher is called nor its refusal is carried.
	if strings.Contains(writing, "BATCH-HEADER") {
		t.Errorf("a pair the literals tell apart still evaluates a rival:\n%s", writing)
	}
}

// TestAStateWhoseGuardsCannotBothHoldEvaluatesNoRival is the third of the three
// transitions that are not rivals.
//
// Two transitions whose guards contradict each other are never both eligible, so
// the reader never evaluates the earlier one's predicate at all and the writer
// has nothing to check against. That is the counted run — the transition reading
// another detail and the one moving past them may be selected by the very same
// test on the very same bytes, and only the register separates them — and it is
// why example/ledger, whose header and detail discriminators sit twelve bytes
// apart, carries no rival evaluation either.
func TestAStateWhoseGuardsCannotBothHoldEvaluatesNoRival(t *testing.T) {
	t.Parallel()

	d := batchedDescriptor()

	for _, node := range d.GetNodes() {
		switch node.GetId() {
		case 10:
			node.GetTransition().BindingIds = []uint64{40}
		case 11:
			node.GetTransition().GuardIds = []uint64{41}
		case 12:
			node.GetTransition().GuardIds = []uint64{42}
		}
	}

	d.Nodes = append(d.GetNodes(), flag(60), binds(40, 60, 101), equalsBytes(41, 60, "\xc8"), equalsBytes(42, 60, "\xc4"))

	writing := writerBody(t, d, "writeBatchDetail")

	if strings.Contains(writing, "BATCH-HEADER") {
		t.Errorf("a pair no register makes co-eligible still evaluates a rival:\n%s", writing)
	}
}

// TestAFillerLandingOnAnEarlierTransitionsLiteralIsPickedAgain is the
// synthesizer's half of the same hole.
//
// The values a case invents are keyed to where each item sits and to nothing
// else, so a filler can hold the literal some other record type's discriminator
// tests — here the header's, whose literal is exactly the two bytes the rule in
// [synth.value] gives the detail's account key. The file that came out would be
// one the generated reader routes elsewhere, and the answer is to choose again
// rather than to lose the case.
func TestAFillerLandingOnAnEarlierTransitionsLiteralIsPickedAgain(t *testing.T) {
	t.Parallel()

	source, skips, err := fileTests(batchedWith(func(p *irpb.Predicate) {
		p.Test = &irpb.Predicate_BytesEqual{BytesEqual: &irpb.BytesEqual{Value: []byte("\xc1\xc1")}}
	}), options{packageName: "batched", importPath: goldenModule + "internal/batched"})
	if err != nil {
		t.Fatalf("fileTests: %v", err)
	}

	if len(skips) != 0 {
		t.Fatalf("a collision the synthesizer can pick its way out of cost %d goals: %v", len(skips), skips)
	}

	// The account key the rule gives at the record's own offsets is a run of the
	// first letter, which is the literal the header now tests. The case that
	// came out holds the second.
	if strings.Contains(source, "0xc1, 0xc1, 0xc1, 0xc1, 0xc1, 0xc1, 0xc1, 0xc1, // record 2: DTL-ACCOUNT") {
		t.Errorf("the case still lays down the filler the header's discriminator matches:\n%s", source)
	}

	if !strings.Contains(source, "0xc2, 0xc2, 0xc2, 0xc2, 0xc2, 0xc2, 0xc2, 0xc2, // record 2: DTL-ACCOUNT") {
		t.Errorf("the case was not laid out again with the values moved along:\n%s", source)
	}
}

// TestAFillerNoRePickEscapesIsSkippedWithTheReason is what happens when there is
// no filler to move to.
//
// The header's discriminator is a set holding every literal the rule reaches
// inside the re-pick budget, so no number of attempts produces a detail the
// header's transition does not take first. The goal is skipped rather than
// covered, and the skip says which record, which bytes and which record type
// took it — which is what an adopter would act on.
func TestAFillerNoRePickEscapesIsSkippedWithTheReason(t *testing.T) {
	t.Parallel()

	set := &irpb.BytesOneOf{}
	for at := range repicks + 1 {
		set.Values = append(set.Values, []byte{byte(0xc1 + at), byte(0xc1 + at)})
	}

	_, skips, err := fileTests(batchedWith(func(p *irpb.Predicate) {
		p.Test = &irpb.Predicate_BytesOneOf{BytesOneOf: set}
	}), options{packageName: "batched", importPath: goldenModule + "internal/batched"})
	if err != nil {
		t.Fatalf("fileTests: %v", err)
	}

	if len(skips) != 1 {
		t.Fatalf("the tier skipped %d goals, want the one predicate no re-pick reaches: %v", len(skips), skips)
	}

	var uncoverable *uncoverableError
	if !errors.As(skips[0].why, &uncoverable) {
		t.Fatalf("the skip is a %T, want the refusal a goal no path reaches carries", skips[0].why)
	}

	for _, says := range []string{"BATCH-DETAIL", "bytes 0:2", "BATCH-HEADER"} {
		if !strings.Contains(uncoverable.Rule, says) {
			t.Errorf("the skip reads %q and does not say %s", uncoverable.Rule, says)
		}
	}
}
