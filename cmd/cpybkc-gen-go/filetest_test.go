// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/irpb"
)

// fileTierDescriptors is every descriptor whose file tier this package pins,
// which is the six golden packages: one per framing, plus the delimiter's three
// placements, plus internal/orders for the record shapes.
//
// The coverage rule below is asserted over all of them rather than over one,
// because what a descriptor's cases have to reach is a property of its
// automaton and the six carry six different ones — a chain of five states with
// one predicate between them, a loop guarded on a counted register, and four
// that discriminate on nothing at all.
var fileTierDescriptors = map[string]func() *irpb.Descriptor{
	"internal/orders":  ordersDescriptor,
	"internal/counted": countedDescriptor,
	"internal/fixed":   fixedDescriptor,
	"internal/chunks":  chunksDescriptor,
	"internal/sep":     separatedDescriptor,
	"internal/opt":     optionalDescriptor,
}

// TestTheFileTierCoversEveryTransitionPredicate is the coverage rule, held over
// the generator rather than over the files it happened to write.
//
// Every transition predicate the descriptor carries, and every literal of a
// set-membership one, is exercised by some case. It is asserted here rather
// than left to the golden comparison because a golden agrees with whatever was
// generated last: a predicate that quietly stopped being reached would
// regenerate into a smaller file and the goldens would be updated with it, and
// the gap would be in an adopter's output before anybody read the diff.
func TestTheFileTierCoversEveryTransitionPredicate(t *testing.T) {
	t.Parallel()

	for dir, descriptor := range fileTierDescriptors {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()

			name := dir[strings.LastIndex(dir, "/")+1:]

			one, err := newFileTest(descriptor(), options{packageName: name, importPath: goldenModule + dir})
			if err != nil {
				t.Fatalf("fileTests: %v", err)
			}

			want, err := one.goals()
			if err != nil {
				t.Fatalf("goals: %v", err)
			}

			files, err := one.cover()
			if err != nil {
				t.Fatalf("cover: %v", err)
			}

			reached := make(map[goal]struct{})

			for _, file := range files {
				for _, met := range file.meets {
					reached[met] = struct{}{}
				}
			}

			for _, missing := range want {
				if _, ok := reached[missing]; !ok {
					t.Errorf("no case reaches %s", spell(t, one, missing))
				}
			}
		})
	}
}

// TestEveryLiteralOfASetMembershipPredicateGetsACase is the half of the
// coverage rule a descriptor with one literal per transition cannot state.
//
// A predicate written as `is one of "DA" or "DB"` is two ways a file can be
// spelled, and an adopter who has checked one has not checked the other. None
// of the six goldens carries such a transition predicate, so the rule is stated
// against a descriptor written for it.
func TestEveryLiteralOfASetMembershipPredicateGetsACase(t *testing.T) {
	t.Parallel()

	one, err := newFileTest(oneOfDescriptor(), options{packageName: "counted", importPath: goldenModule + "internal/counted"})
	if err != nil {
		t.Fatalf("fileTests: %v", err)
	}

	files, err := one.cover()
	if err != nil {
		t.Fatalf("cover: %v", err)
	}

	spelled := make(map[string]struct{})

	for _, file := range files {
		for at, record := range file.records {
			if record.typ != "DetailRecord" {
				continue
			}

			spelled[string(file.records[at].raw[:1])] = struct{}{}
		}
	}

	for _, want := range []string{"\xc4", "\xc5"} {
		if _, ok := spelled[want]; !ok {
			t.Errorf("no case spells DETAIL-RECORD's type code %q, and its predicate admits it", want)
		}
	}
}

// TestAPredicateNoFileReachesIsRefused keeps a gap from being generated
// quietly.
//
// A transition no file can take is a bug in whatever produced the descriptor:
// it selects a record the automaton describes and no reader will ever admit.
// This generator refuses it rather than writing the cases it could reach and
// saying nothing about the one it could not, which is the same answer it gives
// everywhere else a descriptor cannot be walked.
func TestAPredicateNoFileReachesIsRefused(t *testing.T) {
	t.Parallel()

	// The transition admitting SUMMARY-RECORD is given a second guard over the
	// flag register that contradicts the one it already carries: the flag has to
	// be Y and one of N or a space at the same time, so no file ever reaches the
	// predicate selecting that record.
	nodes := withNodes(func(nodes []*irpb.Node) []*irpb.Node {
		for _, node := range nodes {
			if node.GetId() == 12 {
				node.GetTransition().GuardIds = append(node.GetTransition().GetGuardIds(), 31)
			}
		}

		return nodes
	})

	_, err := fileTests(&irpb.Descriptor{Version: supportedIRVersion, Nodes: nodes},
		options{packageName: "counted", importPath: goldenModule + "internal/counted"})
	if err == nil {
		t.Fatal("the file tier was written for a descriptor carrying a predicate no file reaches")
	}

	if !strings.Contains(err.Error(), "no file this layout describes reaches the predicate") {
		t.Errorf("the refusal reads %q and does not name the predicate no file reaches", err)
	}
}

// TestADescriptorWhoseAutomatonAdmitsNoRecordWritesNoFileTier is the emission
// rule file.go already has, applied to the tier that covers it: a tier over a
// reader and a writer that were not emitted has nothing to cover.
func TestADescriptorWhoseAutomatonAdmitsNoRecordWritesNoFileTier(t *testing.T) {
	t.Parallel()

	for name, nodes := range map[string][]*irpb.Node{
		"no file node": withNodes(func(nodes []*irpb.Node) []*irpb.Node { return nodes[1:] }),
		"no state offering a transition": withNodes(func(nodes []*irpb.Node) []*irpb.Node {
			for _, node := range nodes {
				if node.GetState() != nil {
					node.GetState().TransitionIds = nil
				}
			}

			return nodes
		}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source, err := fileTests(&irpb.Descriptor{Version: supportedIRVersion, Nodes: nodes},
				options{packageName: "counted", importPath: goldenModule + "internal/counted"})
			if err != nil {
				t.Fatalf("fileTests: %v", err)
			}

			if source != "" {
				t.Errorf("a file tier was written for a descriptor whose automaton admits no record:\n%s", source)
			}
		})
	}
}

// TestTheFileTierIsTheSameBytesTwice is docs/plugin/SPEC.md's determinism over
// the one file whose content is chosen by a search rather than copied.
//
// The path a descriptor gets is a function of the descriptor and nothing else,
// so two runs produce the same file. Ten runs rather than two, because what
// would break this is a walk over a map, and one iteration of a two-element map
// agrees with the next about half the time.
func TestTheFileTierIsTheSameBytesTwice(t *testing.T) {
	t.Parallel()

	for dir, descriptor := range fileTierDescriptors {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()

			name := dir[strings.LastIndex(dir, "/")+1:]
			opts := options{packageName: name, importPath: goldenModule + dir}

			first, err := fileTests(descriptor(), opts)
			if err != nil {
				t.Fatalf("fileTests: %v", err)
			}

			for range 10 {
				again, err := fileTests(descriptor(), opts)
				if err != nil {
					t.Fatalf("fileTests: %v", err)
				}

				if again != first {
					t.Fatalf("two runs over one descriptor wrote different files\n got:\n%s\nwant:\n%s", again, first)
				}
			}
		})
	}
}

// newFileTest is the emitter the two coverage assertions reach past
// [fileTests] for, so that they can ask what the goals were as well as what was
// written.
func newFileTest(d *irpb.Descriptor, opts options) (*filetest, error) {
	e, err := newEmitter(d)
	if err != nil {
		return nil, err
	}

	f := &filer{emitter: e, opts: opts, index: make(map[uint64]int)}
	if err := f.collect(d); err != nil {
		return nil, err
	}

	enc, err := descriptorEncoding(d)
	if err != nil {
		return nil, err
	}

	s, err := newSynth(&coder{emitter: e, receiver: opts.receiverName()}, d, enc)
	if err != nil {
		return nil, err
	}

	one := &filetest{filer: f, synth: s}

	for _, state := range f.states {
		walk, err := f.transitionsOf(state.GetState())
		if err != nil {
			return nil, err
		}

		one.walks = append(one.walks, walk)
	}

	return one, nil
}

// spell is a goal as a failure names it.
func spell(t *testing.T, one *filetest, want goal) string {
	t.Helper()

	if want.automaton {
		return "the automaton at all"
	}

	values, err := one.literalsOf(want.predicate)
	if err != nil {
		t.Fatalf("literalsOf: %v", err)
	}

	return fmt.Sprintf("the predicate the descriptor carries as node %d holding %q", want.predicate, string(values[want.pick]))
}

// oneOfDescriptor is [countedDescriptor] with its detail record selected on a
// set of two type codes rather than on one.
func oneOfDescriptor() *irpb.Descriptor {
	nodes := withNodes(func(nodes []*irpb.Node) []*irpb.Node {
		for _, node := range nodes {
			if node.GetId() == 51 {
				node.GetPredicate().Test = &irpb.Predicate_BytesOneOf{
					BytesOneOf: &irpb.BytesOneOf{Values: [][]byte{[]byte("\xc4"), []byte("\xc5")}},
				}
			}
		}

		return nodes
	})

	return &irpb.Descriptor{Version: supportedIRVersion, Nodes: nodes}
}
