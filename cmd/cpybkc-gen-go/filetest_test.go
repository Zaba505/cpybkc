// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
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

// TestTheFileTierCoversEveryTransitionPredicate is the coverage rule, held
// against the bytes that were emitted rather than against the generator's own
// bookkeeping.
//
// Every transition predicate the descriptor carries, and every literal of a
// set-membership one, is spelled by some case's file at the offset that
// predicate reads. Over the bytes because the alternative is tautological:
// [filetest.cover] already refuses a goal it cannot reach, so asking it which
// goals it reached asks it to mark its own work. What this cannot be satisfied by
// is a path that was chosen and then not written.
//
// It is asserted here rather than left to the golden comparison because a golden
// agrees with whatever was generated last: a predicate that quietly stopped being
// reached would regenerate into a smaller file and the goldens would be updated
// with it, and the gap would be in an adopter's output before anybody read the
// diff.
func TestTheFileTierCoversEveryTransitionPredicate(t *testing.T) {
	t.Parallel()

	for dir, descriptor := range fileTierDescriptors {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()

			name := dir[strings.LastIndex(dir, "/")+1:]
			opts := options{packageName: name, importPath: goldenModule + dir}

			one, err := newFiletest(descriptor(), opts)
			if err != nil {
				t.Fatalf("newFiletest: %v", err)
			}

			want, err := one.goals()
			if err != nil {
				t.Fatalf("goals: %v", err)
			}

			source, _, err := fileTests(descriptor(), opts)
			if err != nil {
				t.Fatalf("fileTests: %v", err)
			}

			files, _, err := one.cover()
			if err != nil {
				t.Fatalf("cover: %v", err)
			}

			// The paths were chosen, and every one of them is a case in the
			// file that was written. Counted rather than assumed, because a
			// chosen path that never reached the source is exactly the gap the
			// rest of this test would not see.
			if got := strings.Count(source, "\nfunc Test"); got != len(files) {
				t.Errorf("%s cases were chosen and %d were written", plural(len(files), "case"), got)
			}

			for _, missing := range want {
				if !spelled(t, one, files, missing) {
					t.Errorf("no case spells %s in the bytes it reads", spell(t, one, missing))
				}
			}
		})
	}
}

// spelled is whether some case's file holds the goal's literal at the offset the
// predicate reads it from, in a record the transition carrying that predicate
// admits.
func spelled(t *testing.T, one *filetest, files []*laid, want goal) bool {
	t.Helper()

	if want.automaton {
		return len(files) != 0
	}

	values, err := one.literalsOf(want.predicate)
	if err != nil {
		t.Fatalf("literalsOf: %v", err)
	}

	value := values[want.pick]

	for _, walk := range one.walks {
		for _, edge := range walk {
			if edge.node.PredicateId == nil || edge.node.GetPredicateId() != want.predicate {
				continue
			}

			at := edge.reads - len(value)

			for _, file := range files {
				for _, record := range file.records {
					if record.typ != edge.typ || len(record.raw) < edge.reads {
						continue
					}

					if bytes.Equal(record.raw[at:edge.reads], value) {
						return true
					}
				}
			}
		}
	}

	return false
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

	one, err := newFiletest(oneOfDescriptor(), options{packageName: "counted", importPath: goldenModule + "internal/counted"})
	if err != nil {
		t.Fatalf("newFiletest: %v", err)
	}

	files, _, err := one.cover()
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

// TestAPredicateNoFileReachesIsSkippedAndNamed keeps a gap from being generated
// quietly, which is what this generator has always owed a descriptor it cannot
// walk. What it no longer does is refuse the generation over one.
//
// A transition no file can take is still a bug in whatever produced the
// descriptor — it selects a record the automaton describes and no reader will
// ever admit — and the note under the warning still says so. #266 is why the
// price changed: refusing cost the adopter the file-level reader and writer as
// well as the case, and a package that reads their file is worth more than a
// spot-check of one. So the cases that *can* be reached are written, the one
// that cannot is named on standard error and again in the generated file, and
// nothing is silent.
func TestAPredicateNoFileReachesIsSkippedAndNamed(t *testing.T) {
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

	source, skips, err := fileTests(&irpb.Descriptor{Version: supportedIRVersion, Nodes: nodes},
		options{packageName: "counted", importPath: goldenModule + "internal/counted"})
	if err != nil {
		t.Fatalf("fileTests: %v", err)
	}

	if source == "" {
		t.Fatal("no file tier at all was written for a descriptor carrying one predicate no file reaches")
	}

	if len(skips) != 1 {
		t.Fatalf("the tier skipped %d goals, want the one predicate no file reaches: %v", len(skips), skips)
	}

	// The construct, not the fact that something is missing: what sends a reader
	// to the descriptor is the predicate node, the item it tests and the bytes it
	// tests for.
	for _, want := range []string{"predicate", "TYPE-CODE"} {
		if !strings.Contains(skips[0].construct, want) {
			t.Errorf("the skip names %q, which does not say %q", skips[0].construct, want)
		}
	}

	// And again in the file, because the terminal the generation ran in is
	// scrollback and this directory is checked in. By the pieces rather than by
	// the whole phrase: the comment is wrapped, so a line break falls somewhere
	// inside it and where is not this test's business.
	for _, want := range []string{"node 52", "TYPE-CODE"} {
		if !strings.Contains(source, want) {
			t.Errorf("the generated file does not name %q, and it could not cover it:\n%s", want, source)
		}
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

			source, _, err := fileTests(&irpb.Descriptor{Version: supportedIRVersion, Nodes: nodes},
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

			first, _, err := fileTests(descriptor(), opts)
			if err != nil {
				t.Fatalf("fileTests: %v", err)
			}

			for range 10 {
				again, _, err := fileTests(descriptor(), opts)
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

// TestADecrementCountsAgainstTheLatestBindingOfItsRegister is the arithmetic a
// register bound twice along one path turns on.
//
// A binding replaces what a register holds, so a decrement after it takes one off
// the *new* value. Counting decrements against the register's first binding
// instead is off by however many stood between the two, and the number is what
// sizes a table counted by that register and what decides whether an earlier
// transition of a state would have been eligible — so getting it wrong writes a
// wrong file rather than refusing.
//
// None of the golden descriptors walks a path that rebinds a register, which is
// exactly why this is asserted directly: the goldens would agree with the bug.
func TestADecrementCountsAgainstTheLatestBindingOfItsRegister(t *testing.T) {
	t.Parallel()

	one, err := newFiletest(countedDescriptor(), options{packageName: "counted", importPath: goldenModule + "internal/counted"})
	if err != nil {
		t.Fatalf("newFiletest: %v", err)
	}

	// header, detail, header, detail: the second header rebinds the counter the
	// first one bound, and a detail stands on each side of it.
	path := []step{{from: 0, at: 0}, {from: 1, at: 0}, {from: 1, at: 2}, {from: 1, at: 0}}

	sites, needs, why, err := one.demands(path)
	if err != nil {
		t.Fatalf("demands: %v", err)
	}

	if why != "" {
		t.Fatalf("the path was refused before anything was solved: %s", why)
	}

	if why, err := one.solve(sites, needs); err != nil || why != "" {
		t.Fatalf("solve: %v %s", err, why)
	}

	reached, err := one.reachedWith(path, sites)
	if err != nil {
		t.Fatalf("reachedWith: %v", err)
	}

	// Node 20 is the detail counter. The second header bound it to one and the
	// detail behind that header took that one off, so the run is complete and
	// the register is at zero — not at minus one, which is what counting both
	// decrements against the first header would give.
	held, bound := 0, false

	for _, register := range reached {
		if register.register == 20 {
			held, bound = int(register.value), true
		}
	}

	if !bound {
		t.Fatal("the counter the descriptor carries as node 20 is bound twice along this path and came back unbound")
	}

	if held != 0 {
		t.Errorf("the counter reads %d after the second header bound it to one and one detail took one off, want 0", held)
	}
}
