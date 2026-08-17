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

// The golden packages the file-level reader and writer are pinned by, beside
// the one records.go and codec.go are pinned by.
//
// One per framing, because a descriptor carries one file node and a file node
// carries one framing: what the reader and the writer do with the bytes around
// a record cannot be exercised from a package whose file node names a different
// one. They are Go packages rather than fixtures under testdata/ for the reason
// internal/orders is — `go build ./...`, `go vet ./...` and golangci-lint reach
// a package and none of them reaches testdata/, so *generated code compiles* is
// asserted by the compiler this repository already runs.
var machineGoldens = map[string]func() *irpb.Descriptor{
	"internal/counted": countedDescriptor,
	"internal/fixed":   fixedDescriptor,
	"internal/chunks":  chunksDescriptor,
	"internal/sep":     separatedDescriptor,
	"internal/opt":     optionalDescriptor,
}

// TestTheGeneratedFileMachinesAreTheGoldens holds every byte of each of them
// against what is checked in, the way [TestTheGeneratedPackageIsTheGolden] does
// for internal/orders.
func TestTheGeneratedFileMachinesAreTheGoldens(t *testing.T) {
	t.Parallel()

	for dir, descriptor := range machineGoldens {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()

			out := t.TempDir()

			name := dir[strings.LastIndex(dir, "/")+1:]

			if err := generate(descriptor(), out, options{packageName: name, importPath: goldenModule + dir}); err != nil {
				t.Fatalf("generate: %v", err)
			}

			generated := written(t, out)
			golden := written(t, dir)

			for file, want := range golden {
				got, ok := generated[file]
				if !ok {
					t.Errorf("nothing was generated for %s, which the golden carries", file)

					continue
				}

				if got != want {
					t.Errorf("the generated %s is not the golden\n got:\n%s\nwant:\n%s", file, got, want)
				}
			}

			for file := range generated {
				if _, ok := golden[file]; !ok {
					t.Errorf("%s was generated and the golden does not carry it", file)
				}
			}
		})
	}
}

// countedDescriptor is docs/ir/SPEC.md's "Appendix: A counted run, as nodes",
// as a descriptor: a header carrying a detail count and a flag, a run of
// details the count governs, a summary record the flag governs, and any number
// of such groups in one file.
//
// It is delimited by 0x15 and its detail record carries a COMP-3 field, which
// is the pair that makes scanning for a delimiter wrong: PIC S9(3)V99 COMP-3
// holding +152.50 is the bytes 15 25 0C, so a reader looking for 0x15 cuts the
// record in half and fails three records later.
//
// Its summary record carries two tables counted by one register, which is the
// half of "One count may size two tables" that lands on the automaton: neither
// of them sets the register, so a writer checks both against it rather than
// picking between them.
func countedDescriptor() *irpb.Descriptor {
	return &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			{Id: 1, Kind: &irpb.Node_File{File: &irpb.File{
				Framing: &irpb.File_Delimited{Delimited: &irpb.Delimited{
					Delimiter: []byte{0x15},
					Placement: irpb.DelimiterPlacement_DELIMITER_PLACEMENT_TERMINATOR,
				}},
				StartStateId: 2,
			}}},

			// start — does not accept.
			{Id: 2, Kind: &irpb.Node_State{State: &irpb.State{TransitionIds: []uint64{10}}}},

			// group — accepts, guarded by the count being zero and the flag
			// being one of N or a space.
			{Id: 3, Kind: &irpb.Node_State{State: &irpb.State{
				Accepts: true, AcceptanceGuardIds: []uint64{30, 31}, TransitionIds: []uint64{11, 12, 13},
			}}},

			// summarised — accepts.
			{Id: 4, Kind: &irpb.Node_State{State: &irpb.State{
				Accepts: true, TransitionIds: []uint64{14, 15},
			}}},

			edge(10, 100, 3, predicateOn(50), nil, []uint64{40, 41, 42}),
			edge(11, 110, 3, predicateOn(51), []uint64{32}, []uint64{43}),
			edge(12, 120, 4, predicateOn(52), []uint64{30, 33}, nil),
			edge(13, 100, 3, predicateOn(50), []uint64{30, 31}, []uint64{40, 41, 42}),

			// A transition carrying no predicate, standing in front of one that
			// carries one, and excluded by a guard that never holds. It would
			// have matched whatever was in front of the reader, so it says
			// nothing about the bytes in hand and never displaces the
			// undescribed-record diagnostic.
			edge(14, 110, 3, nil, []uint64{34}, nil),
			edge(15, 100, 3, predicateOn(50), nil, []uint64{40, 41, 42}),

			counter(20),
			flag(21),
			counter(22),

			equalsInteger(30, 20, 0),
			oneOfBytes(31, 21, "\xd5", "\x40"),
			positive(32, 20),
			equalsBytes(33, 21, "\xe8"),

			// A guard nothing ever satisfies, which is what makes transition 14
			// a transition the reader always finds ineligible.
			equalsBytes(34, 21, "\xe9"),

			binds(40, 20, 102),
			binds(41, 21, 103),
			binds(42, 22, 104),
			decrements(43, 20),

			equals(50, 101, "\xc8"),
			equals(51, 111, "\xc4"),
			equals(52, 121, "\xe2"),

			record(100, "HEADER-RECORD", 105),
			group(105, "HEADER-RECORD", nil, 101, 102, 103, 104),
			alphanumeric(101, "TYPE-CODE", 1),
			zoned(102, "DTL-COUNT", 2, 2, 0, false),
			alphanumeric(103, "SUM-FLAG", 1),
			zoned(104, "TOTAL-COUNT", 2, 2, 0, false),

			record(110, "DETAIL-RECORD", 115),
			group(115, "DETAIL-RECORD", nil, 111, 112),
			alphanumeric(111, "TYPE-CODE", 1),
			packed(112, "AMOUNT", 3, 5, 2, true),

			record(120, "SUMMARY-RECORD", 125),
			group(125, "SUMMARY-RECORD", nil, 121, 122, 124),
			alphanumeric(121, "TYPE-CODE", 1),
			group(122, "LINE", counted(22, 0, 4), 123),
			alphanumeric(123, "LINE-TEXT", 3),
			group(124, "NOTE", counted(22, 0, 4), 126),
			alphanumeric(126, "NOTE-TEXT", 2),
		},
	}
}

// fixedDescriptor is a fixed-length dataset: one record type, no predicate, and
// an automaton of one state that admits it and accepts.
//
// It carries the two shapes a read-modify-write job destroys where slack does
// not survive a read — a record whose items stop short of LRECL, and a
// REDEFINES alternative shorter than its sibling, whose tail is the other
// alternative's payload.
func fixedDescriptor() *irpb.Descriptor {
	return &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			{Id: 1, Kind: &irpb.Node_File{File: &irpb.File{
				Framing:      &irpb.File_Unframed{Unframed: &irpb.Unframed{}},
				StartStateId: 2,
			}}},

			{Id: 2, Kind: &irpb.Node_State{State: &irpb.State{
				Accepts: true, TransitionIds: []uint64{10},
			}}},

			edge(10, 100, 2, nil, nil, nil),

			record(100, "LEDGER-RECORD", 105),
			group(105, "LEDGER-RECORD", nil, 101, 102, 109),
			alphanumeric(101, "LEDGER-ID", 4),
			group(102, "ENTRY", constant(2), 103, 104),
			alphanumeric(103, "ENTRY-TYPE", 1),
			variant(104, armOf(50, 106), armOf(51, 107)),
			equals(50, 103, "\xc4"),
			equals(51, 103, "\xe2"),
			group(106, "ENTRY-DETAIL", nil, 110, 111),
			alphanumeric(110, "DETAIL-SKU", 4),
			binary(111, "DETAIL-QTY", 2, 4, true),
			group(107, "ENTRY-SUMMARY", nil, 112, 113),
			alphanumeric(112, "SUMMARY-TEXT", 4),
			slack(113, 2),

			// The tail of a record whose items stop short of LRECL, which on
			// these files holds whatever the program that wrote it left there.
			slack(109, 6),
		},
	}
}

// chunksDescriptor is a segmented dataset whose largest segment is smaller than
// its one record type, so that every record is split and reassembled.
func chunksDescriptor() *irpb.Descriptor {
	return &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			{Id: 1, Kind: &irpb.Node_File{File: &irpb.File{
				Framing:      &irpb.File_Segmented{Segmented: &irpb.Segmented{MaxSegmentSize: 12}},
				StartStateId: 2,
			}}},

			{Id: 2, Kind: &irpb.Node_State{State: &irpb.State{
				Accepts: true, TransitionIds: []uint64{10},
			}}},

			edge(10, 100, 2, nil, nil, nil),

			record(100, "CHUNK-RECORD", 105),
			group(105, "CHUNK-RECORD", nil, 101, 102),
			alphanumeric(101, "CHUNK-ID", 4),
			alphanumeric(102, "CHUNK-BODY", 20),
		},
	}
}

// separatedDescriptor is a delimited dataset whose delimiter stands between two
// records: a file of n records carries n-1 of them, and a trailing one
// announces a record that is not there.
func separatedDescriptor() *irpb.Descriptor {
	return lineDescriptor(irpb.DelimiterPlacement_DELIMITER_PLACEMENT_SEPARATOR)
}

// optionalDescriptor is the same dataset under optional terminator: a delimiter
// follows every record, except that the file MAY end after the last one
// without.
func optionalDescriptor() *irpb.Descriptor {
	return lineDescriptor(irpb.DelimiterPlacement_DELIMITER_PLACEMENT_OPTIONAL_TERMINATOR)
}

// lineDescriptor is one line-delimited record type under the placement given.
func lineDescriptor(placement irpb.DelimiterPlacement) *irpb.Descriptor {
	return &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			{Id: 1, Kind: &irpb.Node_File{File: &irpb.File{
				Framing: &irpb.File_Delimited{Delimited: &irpb.Delimited{
					Delimiter: []byte{0x15},
					Placement: placement,
				}},
				StartStateId: 2,
			}}},

			{Id: 2, Kind: &irpb.Node_State{State: &irpb.State{
				Accepts: true, TransitionIds: []uint64{10},
			}}},

			edge(10, 100, 2, nil, nil, nil),

			record(100, "LINE-RECORD", 105),
			group(105, "LINE-RECORD", nil, 101, 102),
			alphanumeric(101, "LINE-TEXT", 5),
			packed(102, "LINE-AMOUNT", 3, 5, 2, true),
		},
	}
}

// edge is a transition node.
func edge(id, record, next uint64, predicate *uint64, guards, bindings []uint64) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Transition{Transition: &irpb.Transition{
		RecordId: record, NextStateId: next, PredicateId: predicate,
		GuardIds: guards, BindingIds: bindings,
	}}}
}

// predicateOn is a transition's predicate reference, which is the one reference
// in the schema absence is a meaning for.
//
// Kept rather than inlined to `new(expr)`, on the rule this repository applies
// to every `newexpr` finding: a one-line shim earns its name when it is called
// repeatedly *and* says something the call site does not. Both hold here.
// [edge] takes four identifier-shaped arguments in a row, so at the call site
// `predicateOn(50)` is the only thing marking which of them is the reference
// absence is a meaning for — and there are five of them.
//
// Where either half fails, the shim goes: `grammarString("XX")` in
// internal/conformance/grammar_test.go had one call site and sat beside a field
// already named `Text`, and was inlined to `new("XX")` for exactly that reason.
// The conversion an untyped constant costs — `new(uint64(50))` — is the price
// of the rule and not the reason for it.
func predicateOn(id uint64) *uint64 { return &id } //nolint:modernize // see above

// counter is an integer register and flag is a bytes one.
func counter(id uint64) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Register{Register: &irpb.Register{
		Kind: irpb.RegisterKind_REGISTER_KIND_INTEGER,
	}}}
}

func flag(id uint64) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Register{Register: &irpb.Register{
		Kind: irpb.RegisterKind_REGISTER_KIND_BYTES,
	}}}
}

// equalsInteger, equalsBytes, oneOfBytes and positive are the three guard tests
// and no fourth.
func equalsInteger(id, register uint64, value int64) *irpb.Node {
	return guardNode(id, register, func(g *irpb.Guard) {
		g.Test = &irpb.Guard_Equals{Equals: &irpb.Literal{
			Value: &irpb.Literal_Integer{Integer: value},
		}}
	})
}

func equalsBytes(id, register uint64, value string) *irpb.Node {
	return guardNode(id, register, func(g *irpb.Guard) {
		g.Test = &irpb.Guard_Equals{Equals: &irpb.Literal{
			Value: &irpb.Literal_BytesValue{BytesValue: []byte(value)},
		}}
	})
}

func oneOfBytes(id, register uint64, values ...string) *irpb.Node {
	set := &irpb.LiteralSet{}

	for _, value := range values {
		set.Values = append(set.Values, &irpb.Literal{
			Value: &irpb.Literal_BytesValue{BytesValue: []byte(value)},
		})
	}

	return guardNode(id, register, func(g *irpb.Guard) { g.Test = &irpb.Guard_OneOf{OneOf: set} })
}

func positive(id, register uint64) *irpb.Node {
	return guardNode(id, register, func(g *irpb.Guard) {
		g.Test = &irpb.Guard_GreaterThanZero{GreaterThanZero: &irpb.GreaterThanZero{}}
	})
}

// guardNode is a guard node carrying one of the three tests.
//
// The test is applied to a built node rather than passed in, because the oneof
// wrapper interface protoc-gen-go declares for it is unexported and a test file
// outside irpb has no name for it.
func guardNode(id, register uint64, apply func(*irpb.Guard)) *irpb.Node {
	guard := &irpb.Guard{RegisterId: register}

	apply(guard)

	return &irpb.Node{Id: id, Kind: &irpb.Node_Guard{Guard: guard}}
}

// binds writes a register from a field of the record the transition admits, and
// decrements takes one off it.
func binds(id, register, field uint64) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Binding{Binding: &irpb.Binding{
		RegisterId: register, Value: &irpb.Binding_FieldId{FieldId: field},
	}}}
}

func decrements(id, register uint64) *irpb.Node {
	return &irpb.Node{Id: id, Kind: &irpb.Node_Binding{Binding: &irpb.Binding{
		RegisterId: register, Value: &irpb.Binding_Decrement{Decrement: &irpb.Decrement{}},
	}}}
}

// counted is an OCCURS min TO max whose count is a register an earlier
// transition bound rather than a field of the record being read.
func counted(register uint64, minimum, maximum uint32) *irpb.Repetition {
	return &irpb.Repetition{Count: &irpb.Repetition_Variable{Variable: &irpb.VariableCount{
		Count:          &irpb.VariableCount_RegisterId{RegisterId: register},
		MinOccurrences: minimum, MaxOccurrences: maximum,
	}}}
}

// TestADescriptorWhoseAutomatonAdmitsNothingWritesNoFileMachine keeps file.go
// something a descriptor produced rather than something this generator always
// writes.
//
// A descriptor with no file node, and one whose every state offers no
// transition, both describe no record stream: a reader and a writer over either
// would be a pair of types with nothing to read or write. It is the test
// records.go already applies to a descriptor carrying no record node.
func TestADescriptorWhoseAutomatonAdmitsNothingWritesNoFileMachine(t *testing.T) {
	t.Parallel()

	for name, d := range map[string]*irpb.Descriptor{
		"no file node at all":           {Version: supportedIRVersion},
		"a file node and no transition": descriptorAt(supportedIRVersion),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			out := t.TempDir()

			if err := generate(d, out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
				t.Fatalf("generate: %v", err)
			}

			if _, ok := written(t, out)[fileMachineFile]; ok {
				t.Errorf("a descriptor whose automaton admits nothing wrote %s", fileMachineFile)
			}
		})
	}
}

// TestTheFileMachineRefusesADescriptorItCannotWalk is every shape this emitter
// reports rather than emitting code for.
//
// Each is a producer bug rather than something an adopter can fix in their
// copybook, and each one of them would otherwise become generated code that
// compiles and reads a file wrongly — which is the failure the whole of
// docs/ir/SPEC.md is arranged around.
func TestTheFileMachineRefusesADescriptorItCannotWalk(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		nodes []*irpb.Node
		says  string
	}{
		"a delimited file whose delimiter is no bytes at all": {
			nodes: replaceFile(func(f *irpb.File) {
				f.Framing = &irpb.File_Delimited{Delimited: &irpb.Delimited{
					Placement: irpb.DelimiterPlacement_DELIMITER_PLACEMENT_TERMINATOR,
				}}
			}),
			says: "no bytes at all",
		},
		"a delimited file that says nothing about where its delimiter stands": {
			nodes: replaceFile(func(f *irpb.File) {
				f.Framing = &irpb.File_Delimited{Delimited: &irpb.Delimited{Delimiter: []byte{0x15}}}
			}),
			says: "says nothing about where its delimiter stands",
		},
		"a segmented file whose largest segment carries no data": {
			nodes: replaceFile(func(f *irpb.File) {
				f.Framing = &irpb.File_Segmented{Segmented: &irpb.Segmented{MaxSegmentSize: 4}}
			}),
			says: "largest segment is 4 bytes",
		},
		"a transition carrying neither a predicate nor a guard beside another": {
			nodes: withNodes(func(nodes []*irpb.Node) []*irpb.Node {
				for _, node := range nodes {
					if node.GetId() == 2 {
						node.GetState().TransitionIds = []uint64{10, 11}
					}

					if node.GetId() == 10 {
						node.GetTransition().PredicateId = nil
						node.GetTransition().GuardIds = nil
					}
				}

				return nodes
			}),
			says: "neither a predicate nor a guard",
		},
		"a guard comparing an integer register against a byte string": {
			nodes: withNodes(func(nodes []*irpb.Node) []*irpb.Node {
				return append(nodes, equalsBytes(60, 20, "\xc1"))
			}, guarding(11, 60)),
			says: "compares an integer register against a byte string",
		},
		"a guard asking whether a bytes register is greater than zero": {
			nodes: withNodes(func(nodes []*irpb.Node) []*irpb.Node {
				return append(nodes, positive(60, 21))
			}, guarding(11, 60)),
			says: "greater than zero and it holds bytes",
		},
		"a binding of an alphanumeric field into an integer register": {
			nodes: withNodes(func(nodes []*irpb.Node) []*irpb.Node {
				return append(nodes, binds(60, 20, 111))
			}, binding(11, 60)),
			says: "into an integer register",
		},
		"a transition admitting a node that is not a record": {
			nodes: withNodes(func(nodes []*irpb.Node) []*irpb.Node {
				for _, node := range nodes {
					if node.GetId() == 10 {
						node.GetTransition().RecordId = 2
					}
				}

				return nodes
			}),
			says: "is not a record node",
		},
		"a file node beginning in a node that is not a state": {
			nodes: withNodes(func(nodes []*irpb.Node) []*irpb.Node {
				nodes[0].GetFile().StartStateId = 100

				return nodes
			}),
			says: "is not a state node",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := fileMachine(&irpb.Descriptor{Version: supportedIRVersion, Nodes: tc.nodes}, options{packageName: "counted", importPath: goldenModule + "internal/counted"})
			if err == nil {
				t.Fatal("the emitter accepted a descriptor it cannot walk")
			}

			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("the refusal reads %q and does not say %q", err, tc.says)
			}
		})
	}
}

// TestARecordMungingToTheReadersOwnNameIsRefused is the collision rule
// records.go already applies to two items, applied to the five identifiers this
// file occupies at package scope.
//
// A generator that disambiguated silently would put a name in an adopter's
// source that a later copybook edit would move, with nothing failing while it
// happened.
func TestARecordMungingToTheReadersOwnNameIsRefused(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"READER", "WRITER", "RECORD"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			nodes := withNodes(func(nodes []*irpb.Node) []*irpb.Node {
				for _, node := range nodes {
					if node.GetId() == 100 {
						node.GetRecord().GetNames().Original = name
					}
				}

				return nodes
			})

			_, err := fileMachine(&irpb.Descriptor{Version: supportedIRVersion, Nodes: nodes}, options{packageName: "counted", importPath: goldenModule + "internal/counted"})
			if err == nil {
				t.Fatalf("a record called %s was emitted beside the reader of the same name", name)
			}

			if !strings.Contains(err.Error(), "file-level reader and writer") {
				t.Errorf("the refusal reads %q and does not say what it collides with", err)
			}
		})
	}
}

// withNodes is countedDescriptor's nodes with each change applied in turn.
func withNodes(changes ...func([]*irpb.Node) []*irpb.Node) []*irpb.Node {
	nodes := countedDescriptor().GetNodes()

	for _, change := range changes {
		nodes = change(nodes)
	}

	return nodes
}

// replaceFile is countedDescriptor's nodes with another framing on its file
// node.
//
// The framing is applied to the node rather than passed in, because the oneof
// wrapper interface protoc-gen-go declares for it is unexported and a test file
// outside irpb has no name for it.
func replaceFile(apply func(*irpb.File)) []*irpb.Node {
	return withNodes(func(nodes []*irpb.Node) []*irpb.Node {
		apply(nodes[0].GetFile())

		return nodes
	})
}

// guarding puts one more guard on a transition, and binding one more binding.
func guarding(transition, guard uint64) func([]*irpb.Node) []*irpb.Node {
	return func(nodes []*irpb.Node) []*irpb.Node {
		for _, node := range nodes {
			if node.GetId() == transition {
				node.GetTransition().GuardIds = append(node.GetTransition().GetGuardIds(), guard)
			}
		}

		return nodes
	}
}

func binding(transition, bind uint64) func([]*irpb.Node) []*irpb.Node {
	return func(nodes []*irpb.Node) []*irpb.Node {
		for _, node := range nodes {
			if node.GetId() == transition {
				node.GetTransition().BindingIds = append(node.GetTransition().GetBindingIds(), bind)
			}
		}

		return nodes
	}
}

// TestNoRegisterIsReadThatNoBindingHasWritten is the rule docs/ir/SPEC.md
// states for every one of the three places a register is read: a guard on a
// transition, the acceptance guards of a state, and the count of a repetition.
//
// A consumer MUST treat a read of a register nothing has bound as a malformed
// descriptor, and MUST NOT supply a zero, an empty byte string or the value of
// any other register. That is a property of the emitted code rather than of any
// file, so it is asserted over the source: every one of the three reads stands
// behind the flag that says a binding wrote it, in both directions.
func TestNoRegisterIsReadThatNoBindingHasWritten(t *testing.T) {
	t.Parallel()

	source, err := fileMachine(countedDescriptor(), options{packageName: "counted", importPath: goldenModule + "internal/counted"})
	if err != nil {
		t.Fatalf("fileMachine: %v", err)
	}

	for _, id := range []uint64{20, 21, 22} {
		for _, holder := range []string{"r", "w"} {
			if !strings.Contains(source, fmt.Sprintf("if !%s.%s {", holder, held(id))) {
				t.Errorf("the %s reads the register the descriptor carries as node %d and never tests whether a binding wrote it", holder, id)
			}
		}

		if !strings.Contains(source, fmt.Sprintf("unbound(%d)", id)) {
			t.Errorf("nothing reports node %d as a register no binding has written", id)
		}
	}

	// And the report itself says so rather than substituting a value.
	if !strings.Contains(source, "no transition taken before it bound one") {
		t.Error("the report for an unbound register does not say that nothing bound it")
	}
}
