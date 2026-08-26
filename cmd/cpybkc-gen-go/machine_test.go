// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"maps"
	"slices"
	"strconv"
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

			if err := generate(io.Discard, descriptor(), out, options{packageName: name, importPath: goldenModule + dir}); err != nil {
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

			if err := generate(io.Discard, d, out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
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

// TestAFileMachineImportsBytesOnlyWhereItComparesThem holds the conditional
// "bytes" import against what the generated file actually uses.
//
// It is stated as an equivalence rather than as four expectations, and that is
// the point of it: the import block and the body are read out of the same
// parsed file, so a construct that reaches for bytes and a condition in
// [filer.survey] that does not know about it fail here rather than at an
// adopter's compiler. The four cases are the four ways the body reaches for it
// today, each isolated so that no case passes through a condition another case
// covers — every golden package with a register file also carries a predicate,
// so the guard case in particular is reachable nowhere else.
//
// The `wants` column is there so that a change making every case reach for
// bytes — or none — still fails: an equivalence alone is satisfied by a
// generator that imports nothing and uses nothing.
func TestAFileMachineImportsBytesOnlyWhereItComparesThem(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		descriptor *irpb.Descriptor
		wants      bool
	}{
		"nothing compares bytes": {descriptor: plainDescriptor(nil), wants: false},

		"a transition carries a predicate": {descriptor: plainDescriptor(predicateOn(50)), wants: true},

		"a guard reads a bytes register": {descriptor: guardedDescriptor(), wants: true},

		"the framing carries a delimiter": {descriptor: terminatedDescriptor(), wants: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			out := t.TempDir()

			if err := generate(io.Discard, tc.descriptor, out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
				t.Fatalf("generate: %v", err)
			}

			source := written(t, out)[fileMachineFile]

			file, err := parser.ParseFile(token.NewFileSet(), fileMachineFile, source, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parsing the generated %s: %v\n%s", fileMachineFile, err, source)
			}

			imported := false

			for _, spec := range file.Imports {
				if spec.Path.Value == `"bytes"` {
					imported = true
				}
			}

			used := false

			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}

				if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "bytes" {
					used = true
				}

				return true
			})

			if used != tc.wants {
				t.Errorf("the generated %s reaches for bytes: %t, and this descriptor was written so that it would be %t\n%s",
					fileMachineFile, used, tc.wants, source)
			}

			if imported != used {
				t.Errorf("the generated %s imports bytes: %t, and reaches for it: %t\n%s",
					fileMachineFile, imported, used, source)
			}
		})
	}
}

// plainDescriptor is one unframed record type read by one state, with the
// transition carrying the predicate given and nothing else: no delimiter, no
// register file and no guard.
//
// Pass nil and the generated file has no reason to compare bytes at all, which
// is what makes it the negative case; pass a reference to node 50 and the one
// reason it has is the predicate.
func plainDescriptor(predicate *uint64) *irpb.Descriptor {
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

			edge(10, 100, 2, predicate, nil, nil),
			equals(50, 101, "AB"),

			record(100, "LINE-RECORD", 105),
			group(105, "LINE-RECORD", nil, 101),
			alphanumeric(101, "LINE-TAG", 2),
		},
	}
}

// guardedDescriptor is a header binding a bytes register and a detail record a
// guard over that register admits, under an unframed file and with no predicate
// anywhere.
//
// Each state offers one transition, so nothing has to be told apart by reading
// the bytes in front of the walk — which is what leaves the guard as the only
// comparison the generated file makes.
func guardedDescriptor() *irpb.Descriptor {
	return &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			{Id: 1, Kind: &irpb.Node_File{File: &irpb.File{
				Framing:      &irpb.File_Unframed{Unframed: &irpb.Unframed{}},
				StartStateId: 2,
			}}},

			// start — the header is what it reads, and a file of nothing at all
			// is not one this layout describes.
			{Id: 2, Kind: &irpb.Node_State{State: &irpb.State{TransitionIds: []uint64{10}}}},

			// bodied — accepts, and the detail it reads is guarded by the flag
			// the header bound.
			{Id: 3, Kind: &irpb.Node_State{State: &irpb.State{
				Accepts: true, TransitionIds: []uint64{11},
			}}},

			edge(10, 100, 3, nil, nil, []uint64{40}),
			edge(11, 200, 3, nil, []uint64{41}, nil),

			flag(60),
			binds(40, 60, 101),
			equalsBytes(41, 60, "A"),

			record(100, "HEADER-RECORD", 105),
			group(105, "HEADER-RECORD", nil, 101),
			alphanumeric(101, "HEADER-FLAG", 1),

			record(200, "DETAIL-RECORD", 205),
			group(205, "DETAIL-RECORD", nil, 201),
			alphanumeric(201, "DETAIL-TEXT", 5),
		},
	}
}

// terminatedDescriptor is [plainDescriptor] with a delimiter behind every
// record, which is the one comparison a framing itself makes.
func terminatedDescriptor() *irpb.Descriptor {
	d := plainDescriptor(nil)

	d.Nodes[0] = &irpb.Node{Id: 1, Kind: &irpb.Node_File{File: &irpb.File{
		Framing: &irpb.File_Delimited{Delimited: &irpb.Delimited{
			Delimiter: []byte{0x15},
			Placement: irpb.DelimiterPlacement_DELIMITER_PLACEMENT_TERMINATOR,
		}},
		StartStateId: 2,
	}}}

	return d
}

// deepDescriptor is [terminatedDescriptor] with a predicate, and with the item
// that predicate reads pushed pad bytes into the record.
//
// A predicate under a framing that states nothing about a record's length is
// what makes the lookahead the reader's, and the pad is what carries its target
// past [bufioDefault] — which is the only way the read-ahead is anything other
// than that number, and so the only way the correctness floor is observable
// from the outside.
func deepDescriptor(pad uint32) *irpb.Descriptor {
	d := plainDescriptor(predicateOn(50))

	d.Nodes[0] = &irpb.Node{Id: 1, Kind: &irpb.Node_File{File: &irpb.File{
		Framing: &irpb.File_Delimited{Delimited: &irpb.Delimited{
			Delimiter: []byte{0x15},
			Placement: irpb.DelimiterPlacement_DELIMITER_PLACEMENT_TERMINATOR,
		}},
		StartStateId: 2,
	}}}

	for i, node := range d.GetNodes() {
		if node.GetId() == 105 {
			d.Nodes[i] = group(105, "LINE-RECORD", nil, 102, 101)
		}
	}

	d.Nodes = append(d.Nodes, alphanumeric(102, "LINE-PAD", pad))

	return d
}

// readAheadDeclaration is the const declaration the generated file.go carries
// readAhead in, with the value every constant of it is declared with.
//
// Read out of the parsed file rather than matched in the text, so that a
// declaration that moved into or out of a const group is still found and the
// doc comment above it is the one the compiler would attach.
func readAheadDeclaration(t *testing.T, source string) (*ast.GenDecl, map[string]string) {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), fileMachineFile, source,
		parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing the generated %s: %v\n%s", fileMachineFile, err, source)
	}

	for _, node := range file.Decls {
		decl, ok := node.(*ast.GenDecl)
		if !ok || decl.Tok != token.CONST {
			continue
		}

		values := make(map[string]string)

		for _, spec := range decl.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}

			for i, name := range value.Names {
				if i >= len(value.Values) {
					continue
				}

				if lit, ok := value.Values[i].(*ast.BasicLit); ok {
					values[name.Name] = lit.Value
				}
			}
		}

		if _, ok := values[readAheadConst]; ok {
			return decl, values
		}
	}

	t.Fatalf("the generated %s declares no %s\n%s", fileMachineFile, readAheadConst, source)

	return nil, nil
}

// TestTheReadAheadIsNeverBelowTheDeepestPredicateTarget is the correctness floor
// this generator's read-ahead carries, and the one thing about the size that is
// not a decision.
//
// A predicate is evaluated against a window in front of a record the reader has
// not identified yet, so a buffer that cannot hold the deepest such window is a
// Peek that can never be satisfied. Asserted as both floors at once — the
// lookahead and bufio's own default — because the answer is their maximum and a
// bug in either direction is a buffer somebody has to debug at an adopter.
func TestTheReadAheadIsNeverBelowTheDeepestPredicateTarget(t *testing.T) {
	t.Parallel()

	for _, lookahead := range []int{0, 1, 2, bufioDefault - 1, bufioDefault, bufioDefault + 1, 1 << 16} {
		f := &filer{lookahead: lookahead}

		want := max(lookahead, bufioDefault)

		if got := f.readAhead(); got != want {
			t.Errorf("a lookahead of %d read ahead %d, and the floors it sits above are %d and %d",
				lookahead, got, lookahead, bufioDefault)
		}
	}
}

// TestAPredicateBeyondBufiosDefaultRaisesTheReadAhead is the same floor from the
// other side: through a descriptor, into the constant the generated file
// declares.
//
// None of the golden descriptors reaches past bufio's default — a predicate
// target that deep is not a shape a copybook writes often — so the floor would
// otherwise be asserted only by the arithmetic that implements it.
func TestAPredicateBeyondBufiosDefaultRaisesTheReadAhead(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	if err := generate(io.Discard, deepDescriptor(bufioDefault), out,
		options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	source := written(t, out)[fileMachineFile]

	_, values := readAheadDeclaration(t, source)

	if values[lookaheadConst] != values[readAheadConst] {
		t.Errorf("%s is %s and %s is %s, and a buffer smaller than the window a predicate reads is a Peek that cannot be satisfied",
			lookaheadConst, values[lookaheadConst], readAheadConst, values[readAheadConst])
	}

	if values[readAheadConst] == strconv.Itoa(bufioDefault) {
		t.Errorf("this descriptor reads past %d and %s is still %s", bufioDefault, readAheadConst, values[readAheadConst])
	}
}

// TestTheReadAheadSaysWhereItsSizeCameFrom is the emitted comment answering the
// question the size actually raises.
//
// The comment used to say only why the buffer need not be *larger* for
// correctness, which is a different question from why it is this number and
// leaves the reader of a slow run with nowhere to go. Both are now on it, and
// the second one is the wrapping idiom — the fix that works today and needs
// nothing from this generator, which is worth nothing to an adopter who has to
// already know it exists. See README.md, "Decided: the read-ahead buffer is a
// constant".
//
// The descriptor whose predicate reaches past bufio's default is in the table
// for the reason the whole table is: every golden sits at 4096, so a comment
// hardcoding that number would satisfy the five of them and misstate the size
// on the only shape where the size is anything else.
func TestTheReadAheadSaysWhereItsSizeCameFrom(t *testing.T) {
	t.Parallel()

	cases := map[string]func() *irpb.Descriptor{
		"a predicate past bufio's default": func() *irpb.Descriptor { return deepDescriptor(bufioDefault) },
	}

	maps.Copy(cases, machineGoldens)

	for name, descriptor := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			out := t.TempDir()

			if err := generate(io.Discard, descriptor(), out,
				options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
				t.Fatalf("generate: %v", err)
			}

			source := written(t, out)[fileMachineFile]

			decl, values := readAheadDeclaration(t, source)

			doc := decl.Doc.Text()

			// The size it names is the size it is declared with, whichever of
			// the two floors produced it.
			for _, want := range []string{
				values[readAheadConst],
				"bufio's own default",
				newReaderFunc + "(bufio.NewReaderSize(",
			} {
				if !strings.Contains(doc, want) {
					t.Errorf("the comment above %s = %s never says %q\n%s",
						readAheadConst, values[readAheadConst], want, doc)
				}
			}

			if values[readAheadConst] != strconv.Itoa(bufioDefault) &&
				strings.Contains(doc, "%d is bufio's own default") {
				t.Errorf("%s is %s and its comment calls %d the size\n%s",
					readAheadConst, values[readAheadConst], bufioDefault, doc)
			}

			// And the adopter reads NewReader, not an unexported constant, so
			// the idiom is on the constructor's own doc as well.
			if !strings.Contains(source, "//\t"+newReaderFunc+"(bufio.NewReaderSize(") {
				t.Errorf("no doc comment in the generated %s carries the wrapping idiom\n%s", fileMachineFile, source)
			}
		})
	}
}

// TestTheGeneratedFileMachineIsTheSameForTheSameDescriptor is determinism, held
// over the whole file rather than over the read-ahead alone.
//
// The size is a constant and a constant is a function of the descriptor: there
// is no option that sets it, and nothing reads the host it is generated on. Over
// the bytes because the arithmetic producing that constant is a maximum of two
// integers and could not disagree with itself — where a generator realistically
// becomes nondeterministic is a map walked in hash order, and that shows up in
// the order of what it emitted rather than in this number.
func TestTheGeneratedFileMachineIsTheSameForTheSameDescriptor(t *testing.T) {
	t.Parallel()

	cases := map[string]func() *irpb.Descriptor{
		"a predicate past bufio's default": func() *irpb.Descriptor { return deepDescriptor(bufioDefault) },
	}

	maps.Copy(cases, machineGoldens)

	for name, descriptor := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var first string

			for range 2 {
				out := t.TempDir()

				if err := generate(io.Discard, descriptor(), out,
					options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
					t.Fatalf("generate: %v", err)
				}

				source := written(t, out)[fileMachineFile]

				if first == "" {
					first = source

					continue
				}

				if source != first {
					t.Errorf("two generations of one descriptor wrote different %s\n got:\n%s\nwant:\n%s",
						fileMachineFile, source, first)
				}
			}
		})
	}
}

// TestNoTwoPredicateFunctionsAreTheSameTest is #318, over every golden: a
// predicate is a function of the offset it reads at, the width it reads and the
// literals it compares against, and of nothing else — so a file that emits two
// functions with one body has stated a dependence that does not exist.
//
// It is asserted here as well as by the goldens because the goldens agree with
// the bug. `example/policy/` carried 93 predicate functions with eleven distinct
// bodies, nine and ten copies of each, and every byte of that was checked in and
// reviewed: the duplication is invisible in a diff of the file that carries it,
// and only a count over the whole file finds it.
func TestNoTwoPredicateFunctionsAreTheSameTest(t *testing.T) {
	t.Parallel()

	for dir, descriptor := range machineGoldens {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()

			name := dir[strings.LastIndex(dir, "/")+1:]

			by := make(map[string]string)

			for fn, body := range predicateFunctions(t, generatedMachine(t, descriptor(), name, dir)) {
				if other, ok := by[body]; ok {
					t.Errorf("%s and %s are both %s, and one predicate is one function", fn, other, body)

					continue
				}

				by[body] = fn
			}
		})
	}
}

// TestStatesTestingOnePredicateNameOneFunction is the same rule from the
// descriptor's side: that the count follows the predicates rather than the
// transitions testing them.
//
// [countedDescriptor] is the one golden whose automaton tests a predicate from
// more than one state — five of its transitions carry one, three predicate nodes
// between them, and node 50 is reached from three different states. A generator
// emitting per transition produces five functions here, which is the growth
// #318 is about: it is quadratic in the automaton's fan-out while the number of
// things being discriminated is not.
func TestStatesTestingOnePredicateNameOneFunction(t *testing.T) {
	t.Parallel()

	d := countedDescriptor()

	tested := 0

	for _, node := range d.GetNodes() {
		if edge := node.GetTransition(); edge != nil && edge.PredicateId != nil {
			tested++
		}
	}

	functions := predicateFunctions(t, generatedMachine(t, d, "counted", "internal/counted"))

	// The number, not merely fewer than the transitions. "Fewer" passes on a
	// partial fold — four functions where three are right — and it also passes
	// on the dangerous direction, all five folded onto one: that file compiles,
	// and it admits the wrong record. The transition count stays because it is
	// what the answer must *not* follow.
	if len(functions) != 3 {
		t.Errorf("%d transitions test a predicate, three predicate nodes between them, and %d functions were emitted: %v",
			tested, len(functions), slices.Sorted(maps.Keys(functions)))
	}
}

// generatedMachine is the file-level reader and writer this descriptor
// generates, as source.
func generatedMachine(t *testing.T, d *irpb.Descriptor, name, dir string) string {
	t.Helper()

	out := t.TempDir()

	if err := generate(io.Discard, d, out, options{packageName: name, importPath: goldenModule + dir}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	return written(t, out)[fileMachineFile]
}

// predicateFunctions is every predicate function the generated file declares,
// by name, against the expression it returns.
//
// The expression is read out of the parsed file and printed back, so that two
// tests are the same test exactly when they are the same Go expression —
// whatever the emitter spelled around them and whatever gofmt did to it
// afterwards.
func predicateFunctions(t *testing.T, source string) map[string]string {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, fileMachineFile, source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing the generated %s: %v\n%s", fileMachineFile, err, source)
	}

	out := make(map[string]string)

	declared := 0

	for _, node := range file.Decls {
		decl, ok := node.(*ast.FuncDecl)
		if !ok || decl.Recv != nil || !strings.HasPrefix(decl.Name.Name, "matches") {
			continue
		}

		declared++

		var last ast.Expr

		for _, stmt := range decl.Body.List {
			ret, ok := stmt.(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				continue
			}

			last = ret.Results[0]
		}

		if last == nil {
			t.Fatalf("%s returns nothing, and a predicate is an expression over the bytes it is handed\n%s", decl.Name.Name, source)
		}

		var b strings.Builder

		if err := printer.Fprint(&b, fset, last); err != nil {
			t.Fatalf("printing what %s returns: %v", decl.Name.Name, err)
		}

		out[decl.Name.Name] = b.String()
	}

	// Keyed by name, so two declarations sharing one name would collapse into
	// a single entry and every assertion over this map would go green on a
	// file that does not compile. The parser type-checks nothing, so counting
	// is the only place that collision can be caught here.
	if len(out) != declared {
		t.Fatalf("%d predicate functions are declared and %d names are distinct, and a name emitted twice is a file that does not build\n%s",
			declared, len(out), source)
	}

	return out
}

// TestEveryTransitionNamesTheFunctionThatIsItsOwnTest is the other direction of
// #318, and the dangerous one.
//
// [TestNoTwoPredicateFunctionsAreTheSameTest] catches a fold that did not
// happen. It cannot catch one that went too far: a key coarser than the
// predicate — the offset alone, say, which is exactly where a discriminator puts
// several predicates — folds two different tests onto one function, and the file
// that comes out has no duplicate bodies, compiles, and admits the wrong record
// on the right bytes. Nothing about it looks wrong in a diff.
//
// So this walks it back. Every transition carrying a predicate is asked what
// function it names, and that function's body has to be that transition's own
// expression rather than some other transition's.
func TestEveryTransitionNamesTheFunctionThatIsItsOwnTest(t *testing.T) {
	t.Parallel()

	for dir, descriptor := range machineGoldens {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()

			f, walks := gathered(t, descriptor())

			by := make(map[string]predicateFunc, len(f.predicates))
			for _, p := range f.predicates {
				by[p.name] = p
			}

			tested := 0

			for _, walk := range walks {
				for _, edge := range walk {
					if edge.match == "" {
						continue
					}

					tested++

					name, err := f.matcherOf(edge)
					if err != nil {
						t.Errorf("a transition admitting %s tests %s and names no function: %v",
							edge.record.GetNames().GetOriginal(), edge.match, err)

						continue
					}

					if got := by[name].match; got != edge.match {
						t.Errorf("a transition admitting %s tests %s and names %s, which tests %s",
							edge.record.GetNames().GetOriginal(), edge.match, name, got)
					}
				}
			}

			if tested == 0 {
				t.Skip("this descriptor's automaton tests no predicate")
			}
		})
	}
}

// gathered is a filer that has resolved a descriptor's walks and gathered the
// predicates of them, which is the state [filer.matcherOf] answers from.
//
// It is the first half of [filer.emit] rather than the whole of it: what is
// being asserted is the mapping from a transition to the function it names, and
// that mapping is settled before a byte is emitted.
func gathered(t *testing.T, d *irpb.Descriptor) (*filer, [][]transition) {
	t.Helper()

	e, err := newEmitter(d)
	if err != nil {
		t.Fatalf("newEmitter: %v", err)
	}

	f := &filer{emitter: e, opts: options{packageName: goldenPackage}, index: make(map[uint64]int)}

	if err := f.collect(d); err != nil {
		t.Fatalf("collect: %v", err)
	}

	walks := make([][]transition, len(f.states))

	for i, state := range f.states {
		out, err := f.transitionsOf(state.GetState())
		if err != nil {
			t.Fatalf("resolving the transitions of state %d: %v", state.GetId(), err)
		}

		walks[i] = out
	}

	f.gather(walks)

	return f, walks
}
