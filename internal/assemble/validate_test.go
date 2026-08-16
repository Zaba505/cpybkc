// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package assemble

import (
	"errors"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/irpb"
)

// [Assemble] cannot produce most of what these test, which is the point: the
// pass exists for the descriptor a later change to this package, or a caller
// building one some other way, gets wrong. So each test spoils one member of a
// descriptor that passes and asserts what the pass then says, rather than
// looking for a layout that provokes it.
//
// The descriptor spoiled is the smallest one that conforms — one record type of
// one field, one state, one transition — because a fault reported against six
// nodes is a fault a reader can check by hand.

// valid is the smallest conforming descriptor: a fixed-length dataset of one
// record type, read by a state that admits it and accepts.
func valid() *irpb.Descriptor {
	return &irpb.Descriptor{
		Version: irpb.IrVersion_IR_VERSION_1,
		Nodes: []*irpb.Node{
			{Id: 0, Kind: &irpb.Node_File{File: &irpb.File{
				Framing:      &irpb.File_Unframed{Unframed: &irpb.Unframed{}},
				StartStateId: 4,
			}}},
			{Id: 1, Kind: &irpb.Node_Record{Record: &irpb.Record{
				RootId: 2,
				Names:  &irpb.Names{Original: "REC"},
			}}},
			{Id: 2, Kind: &irpb.Node_Group{Group: &irpb.Group{
				MemberIds: []uint64{3},
				Names:     &irpb.Names{Original: "REC"},
			}}},
			{Id: 3, Kind: &irpb.Node_Field{Field: &irpb.Field{
				Width: 1,
				Encoding: &irpb.Encoding{
					Charset:        irpb.Charset_CHARSET_CP037,
					SignConvention: irpb.SignConvention_SIGN_CONVENTION_EBCDIC,
					ByteOrder:      irpb.ByteOrder_BYTE_ORDER_BIG_ENDIAN,
					FloatFormat:    irpb.FloatFormat_FLOAT_FORMAT_IBM_HFP,
				},
				Usage: irpb.Usage_USAGE_DISPLAY,
				Names: &irpb.Names{Original: "REC-TYPE"},
			}}},
			{Id: 4, Kind: &irpb.Node_State{State: &irpb.State{
				Accepts:       true,
				TransitionIds: []uint64{5},
			}}},
			{Id: 5, Kind: &irpb.Node_Transition{Transition: &irpb.Transition{
				RecordId:    1,
				NextStateId: 4,
			}}},
		},
	}
}

// node is one node of a descriptor, by identifier, for a test about to spoil it.
func node(t *testing.T, d *irpb.Descriptor, id uint64) *irpb.Node {
	t.Helper()

	for _, found := range d.GetNodes() {
		if found.GetId() == id {
			return found
		}
	}

	t.Fatalf("the descriptor carries no node %d", id)

	return nil
}

// refused holds a spoiled descriptor to being refused, and to saying what is
// wrong in words a reader can act on.
func refused(t *testing.T, d *irpb.Descriptor, says string) {
	t.Helper()

	err := Validate(d)
	if err == nil {
		t.Fatalf("the descriptor validated, want a fault saying it %q", says)
	}

	if !strings.Contains(diag.Render(err), says) {
		t.Errorf("the fault reads:\n%s\nand does not say %q", diag.Render(err), says)
	}
}

// TestTheSmallestConformingDescriptorPasses keeps every test below honest: a
// pass that refuses everything proves nothing about the descriptor it refuses.
func TestTheSmallestConformingDescriptorPasses(t *testing.T) {
	if err := Validate(valid()); err != nil {
		t.Fatalf("the smallest conforming descriptor was refused: %v", diag.Render(err))
	}
}

// TestAnAssembledDescriptorPasses holds the whole pipeline's output to the same
// pass, which is what [Assemble] running it means.
func TestAnAssembledDescriptorPasses(t *testing.T) {
	if err := Validate(assembled(t, countedRun, countedRunCopybooks())); err != nil {
		t.Fatalf("an assembled descriptor was refused: %v", diag.Render(err))
	}
}

// TestADescriptorMustSayWhichContractItWasWrittenAgainst holds the version to
// being set, because a consumer's first obligation is to read it.
func TestADescriptorMustSayWhichContractItWasWrittenAgainst(t *testing.T) {
	t.Run("nothing at all", func(t *testing.T) {
		var fault *DescriptorError
		if err := Validate(nil); !errors.As(err, &fault) {
			t.Fatalf("the fault is %v, want a DescriptorError", err)
		}
	})

	t.Run("no version", func(t *testing.T) {
		d := valid()
		d.Version = irpb.IrVersion_IR_VERSION_UNSPECIFIED

		refused(t, d, "states no version")

		var fault *DescriptorError
		if err := Validate(d); !errors.As(err, &fault) {
			t.Errorf("the fault is %v, want a DescriptorError", err)
		}
	})

	t.Run("a version no release names", func(t *testing.T) {
		d := valid()
		d.Version = irpb.IrVersion(97)

		refused(t, d, "which no release of the IR names")
	})
}

// TestExactlyOneFileNodeIsTheRoot holds the descriptor to having one root, since
// nothing points at it and a second would leave two graphs in one message.
func TestExactlyOneFileNodeIsTheRoot(t *testing.T) {
	t.Run("none", func(t *testing.T) {
		d := valid()
		d.Nodes = d.GetNodes()[1:]

		refused(t, d, "carries no file node")
	})

	t.Run("two", func(t *testing.T) {
		d := valid()
		d.Nodes = append(d.GetNodes(), &irpb.Node{Id: 6, Kind: &irpb.Node_File{File: &irpb.File{
			Framing:      &irpb.File_Unframed{Unframed: &irpb.Unframed{}},
			StartStateId: 4,
		}}})

		refused(t, d, "carries 2 file nodes")
	})
}

// TestTheNodeListIsAscendingAndItsIdentifiersUnique holds the ordering the
// determinism promise is built on: a producer assigns identifiers by a
// deterministic traversal and emits the list in ascending order.
func TestTheNodeListIsAscendingAndItsIdentifiersUnique(t *testing.T) {
	t.Run("out of order", func(t *testing.T) {
		d := valid()
		d.Nodes[4], d.Nodes[5] = d.GetNodes()[5], d.GetNodes()[4]

		refused(t, d, "ascending identifier order")
	})

	t.Run("shared", func(t *testing.T) {
		d := valid()
		node(t, d, 5).Id = 4

		refused(t, d, "shares its identifier with another node")
	})
}

// TestEveryReferenceResolvesToAKindItsPositionAdmits is the rule a node set
// stands on: a consumer indexes by identifier, and a reference that resolves to
// nothing or to the wrong kind is a walk that fails halfway.
func TestEveryReferenceResolvesToAKindItsPositionAdmits(t *testing.T) {
	t.Run("nothing", func(t *testing.T) {
		d := valid()
		node(t, d, 5).GetTransition().RecordId = 97

		refused(t, d, "names node 97, which is not in this descriptor")
	})

	t.Run("the wrong kind", func(t *testing.T) {
		d := valid()
		node(t, d, 5).GetTransition().RecordId = 3

		refused(t, d, "a field node, and admits only record")
	})

	t.Run("a node with no body at all", func(t *testing.T) {
		d := valid()
		node(t, d, 3).Kind = nil

		refused(t, d, "carries no body")
	})
}

// TestEveryFieldStatesAllFourAxes is the requirement docs/ir/SPEC.md makes of a
// producer and a consumer both, because every one of the four fails silently
// when it is wrong.
func TestEveryFieldStatesAllFourAxes(t *testing.T) {
	tests := []struct {
		name  string
		spoil func(*irpb.Encoding)
		says  string
	}{
		{
			name:  "charset",
			spoil: func(e *irpb.Encoding) { e.Charset = irpb.Charset_CHARSET_UNSPECIFIED },
			says:  "states no charset",
		},
		{
			name:  "sign convention",
			spoil: func(e *irpb.Encoding) { e.SignConvention = irpb.SignConvention_SIGN_CONVENTION_UNSPECIFIED },
			says:  "states no sign convention",
		},
		{
			name:  "byte order",
			spoil: func(e *irpb.Encoding) { e.ByteOrder = irpb.ByteOrder_BYTE_ORDER_UNSPECIFIED },
			says:  "states no byte order",
		},
		{
			name:  "float format",
			spoil: func(e *irpb.Encoding) { e.FloatFormat = irpb.FloatFormat_FLOAT_FORMAT_UNSPECIFIED },
			says:  "states no float format",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := valid()
			test.spoil(node(t, d, 3).GetField().GetEncoding())

			refused(t, d, test.says)
		})
	}

	t.Run("all four", func(t *testing.T) {
		d := valid()
		node(t, d, 3).GetField().Encoding = nil

		refused(t, d, "states no charset, sign convention, byte order and float format")
	})
}

// TestAFieldSaysWhatItsBytesAreAndHowManyOfThem holds the two members a field
// cannot be read without.
func TestAFieldSaysWhatItsBytesAreAndHowManyOfThem(t *testing.T) {
	t.Run("no usage", func(t *testing.T) {
		d := valid()
		node(t, d, 3).GetField().Usage = irpb.Usage_USAGE_UNSPECIFIED

		refused(t, d, "states no USAGE")
	})

	t.Run("no width", func(t *testing.T) {
		d := valid()
		node(t, d, 3).GetField().Width = 0

		refused(t, d, "occupies no bytes")
	})

	t.Run("a picture of no category", func(t *testing.T) {
		d := valid()
		node(t, d, 3).GetField().Picture = &irpb.Picture{Digits: 2}

		refused(t, d, "carries a PICTURE of no category")
	})
}

// TestASignedComp6FieldIsRefused holds the one contradiction between a USAGE
// and a PICTURE this pass reads.
//
// A COMP-6 item is packed with no sign nibble, so there is nowhere in the field
// for an S to be recorded and docs/ir/SPEC.md forbids a producer to emit one.
// It is refused here rather than left to a generator because it is a fault a
// consumer cannot recover from — reading the item as unsigned is silently right
// about every value it is ever shown but the negative ones — and because this
// repository is itself a producer: `cobol-go` admits `COMP-6` as a spelling and
// assigns no meaning to pairing it with an `S`, so a copybook may write one.
func TestASignedComp6FieldIsRefused(t *testing.T) {
	t.Run("signed", func(t *testing.T) {
		d := valid()
		field := node(t, d, 3).GetField()
		field.Usage = irpb.Usage_USAGE_COMP_6
		field.Picture = &irpb.Picture{
			Category: irpb.Category_CATEGORY_NUMERIC, Digits: 4, Signed: true,
		}

		refused(t, d, "is a COMP-6 item whose PICTURE carries an S")
	})

	t.Run("unsigned", func(t *testing.T) {
		d := valid()
		field := node(t, d, 3).GetField()
		field.Usage = irpb.Usage_USAGE_COMP_6
		field.Picture = &irpb.Picture{Category: irpb.Category_CATEGORY_NUMERIC, Digits: 4}

		if err := Validate(d); err != nil {
			t.Fatalf("an unsigned COMP-6 field was refused: %v", err)
		}
	})
}

// TestAnOverrideWithNoOriginalIsRefused holds the original to being present even
// where a substitute is, which is what lets generated code point back at the
// copybook it came from.
func TestAnOverrideWithNoOriginalIsRefused(t *testing.T) {
	d := valid()
	node(t, d, 3).GetField().Names = &irpb.Names{OverrideName: new("kind")}

	refused(t, d, "carries an override and no original name")
}

// TestEveryNodeHangsOffTheFileNode holds a descriptor to describing everything
// it carries. A node nothing reaches is one no consumer will index its way to.
func TestEveryNodeHangsOffTheFileNode(t *testing.T) {
	d := valid()
	d.Nodes = append(d.GetNodes(), &irpb.Node{Id: 6, Kind: &irpb.Node_Register{
		Register: &irpb.Register{Kind: irpb.RegisterKind_REGISTER_KIND_INTEGER},
	}})

	refused(t, d, "is not reachable from the file node")
}

// TestEveryItemIsContainedExactlyOnce holds the member lists to being the one
// statement of where anything is: twice puts one run of bytes in two places, and
// never leaves it belonging to no record.
func TestEveryItemIsContainedExactlyOnce(t *testing.T) {
	t.Run("twice", func(t *testing.T) {
		d := valid()
		node(t, d, 2).GetGroup().MemberIds = []uint64{3, 3}

		refused(t, d, "is contained 2 times")
	})

	t.Run("never", func(t *testing.T) {
		d := valid()
		d.Nodes = append(d.GetNodes(), &irpb.Node{Id: 6, Kind: &irpb.Node_Slack{
			Slack: &irpb.Slack{Width: 4},
		}})

		refused(t, d, "is contained by nothing")
	})
}

// TestARecordWithNoItemsIsRefused holds the rule that no transition may admit a
// record whose extent is zero, at the one shape a producer can reach it by.
func TestARecordWithNoItemsIsRefused(t *testing.T) {
	d := valid()
	node(t, d, 2).GetGroup().MemberIds = nil
	node(t, d, 3).Kind = &irpb.Node_Slack{Slack: &irpb.Slack{Width: 1}}

	refused(t, d, "holds no items")
}

// TestARecordRootedAtTheWrongKindIsOneFault holds the emptiness check to being
// about a record whose top level really is a group.
//
// A root naming some other kind reads as holding no members whatever it is, so
// reporting both would report the reference fault twice — once as itself and
// once as a consequence a reader would go looking for separately.
func TestARecordRootedAtTheWrongKindIsOneFault(t *testing.T) {
	d := valid()
	node(t, d, 1).GetRecord().RootId = 3

	refused(t, d, "a field node, and admits only group")

	// The reference, the top level nothing now contains, and the field the
	// record and its group both name. Every one of those is what the root
	// being wrong did; a fourth about the record holding no items would be a
	// consequence a reader would go looking for separately.
	if rendered := diag.Render(Validate(d)); strings.Contains(rendered, "holds no items") {
		t.Errorf("the pass also reported the record as holding no items:\n%s", rendered)
	}
}

// TestAVariantCarriesArmsAndEachArmCarriesBoth holds the one place an
// alternation survives resolution to being a choice a consumer can make.
func TestAVariantCarriesArmsAndEachArmCarriesBoth(t *testing.T) {
	t.Run("no arms", func(t *testing.T) {
		d := valid()
		node(t, d, 3).Kind = &irpb.Node_Variant{Variant: &irpb.Variant{}}

		refused(t, d, "carries no arms")
	})

	t.Run("an arm with no body", func(t *testing.T) {
		d := valid()
		node(t, d, 3).Kind = &irpb.Node_Variant{Variant: &irpb.Variant{
			Arms: []*irpb.Arm{{PredicateId: 6}},
		}}
		d.Nodes = append(d.GetNodes(), &irpb.Node{Id: 6, Kind: &irpb.Node_Predicate{
			Predicate: &irpb.Predicate{
				FieldId: 3,
				Test:    &irpb.Predicate_BytesEqual{BytesEqual: &irpb.BytesEqual{Value: []byte{0xc8}}},
			},
		}})

		refused(t, d, "names no body")
	})
}

// TestAPredicateCarriesATestOverBytes holds the closed set of two to being
// stated, and a one-of to carrying a set worth testing.
func TestAPredicateCarriesATestOverBytes(t *testing.T) {
	predicated := func(t *testing.T, test *irpb.Predicate) *irpb.Descriptor {
		t.Helper()

		d := valid()
		node(t, d, 5).GetTransition().PredicateId = new(uint64(6))
		d.Nodes = append(d.GetNodes(), &irpb.Node{Id: 6, Kind: &irpb.Node_Predicate{Predicate: test}})

		return d
	}

	t.Run("no test", func(t *testing.T) {
		refused(t, predicated(t, &irpb.Predicate{FieldId: 3}), "carries no test")
	})

	t.Run("no bytes to compare", func(t *testing.T) {
		refused(t, predicated(t, &irpb.Predicate{
			FieldId: 3,
			Test:    &irpb.Predicate_BytesEqual{BytesEqual: &irpb.BytesEqual{}},
		}), "compares against no bytes at all")
	})

	t.Run("one literal in a one-of", func(t *testing.T) {
		refused(t, predicated(t, &irpb.Predicate{
			FieldId: 3,
			Test: &irpb.Predicate_BytesOneOf{BytesOneOf: &irpb.BytesOneOf{
				Values: [][]byte{{0xc8}},
			}},
		}), "offers 1 literals, and a one-of test carries at least two")
	})

	t.Run("one literal twice", func(t *testing.T) {
		refused(t, predicated(t, &irpb.Predicate{
			FieldId: 3,
			Test: &irpb.Predicate_BytesOneOf{BytesOneOf: &irpb.BytesOneOf{
				Values: [][]byte{{0xc8}, {0xc4}, {0xc8}},
			}},
		}), "carries the literal 0xc8 twice")
	})
}

// TestTheAutomatonsMemoryIsWellFormed holds the register file's three node kinds
// to what each cannot be read without.
func TestTheAutomatonsMemoryIsWellFormed(t *testing.T) {
	// A register, a guard reading it and a binding writing it, hung off the
	// one transition the smallest descriptor has.
	memory := func() *irpb.Descriptor {
		d := valid()
		d.Nodes = append(d.GetNodes(),
			&irpb.Node{Id: 6, Kind: &irpb.Node_Register{Register: &irpb.Register{
				Kind: irpb.RegisterKind_REGISTER_KIND_INTEGER,
			}}},
			&irpb.Node{Id: 7, Kind: &irpb.Node_Guard{Guard: &irpb.Guard{
				RegisterId: 6,
				Test:       &irpb.Guard_GreaterThanZero{GreaterThanZero: &irpb.GreaterThanZero{}},
			}}},
			&irpb.Node{Id: 8, Kind: &irpb.Node_Binding{Binding: &irpb.Binding{
				RegisterId: 6,
				Value:      &irpb.Binding_Decrement{Decrement: &irpb.Decrement{}},
			}}},
		)

		transition := node(t, d, 5).GetTransition()
		transition.GuardIds = []uint64{7}
		transition.BindingIds = []uint64{8}

		return d
	}

	t.Run("it passes as it stands", func(t *testing.T) {
		if err := Validate(memory()); err != nil {
			t.Fatalf("a descriptor with a register file was refused: %v", diag.Render(err))
		}
	})

	t.Run("a register that does not say what it holds", func(t *testing.T) {
		d := memory()
		node(t, d, 6).GetRegister().Kind = irpb.RegisterKind_REGISTER_KIND_UNSPECIFIED

		refused(t, d, "does not say what it holds")
	})

	t.Run("a guard with no test", func(t *testing.T) {
		d := memory()
		node(t, d, 7).GetGuard().Test = nil

		refused(t, d, "carries no test")
	})

	t.Run("a guard whose literal carries no value", func(t *testing.T) {
		d := memory()
		node(t, d, 7).GetGuard().Test = &irpb.Guard_Equals{Equals: &irpb.Literal{}}

		refused(t, d, "carries no value")
	})

	t.Run("a literal of the kind the register does not hold", func(t *testing.T) {
		d := memory()
		node(t, d, 7).GetGuard().Test = &irpb.Guard_Equals{Equals: &irpb.Literal{
			Value: &irpb.Literal_BytesValue{BytesValue: []byte{0xe8}},
		}}

		refused(t, d, "carries bytes, and the register it is compared against holds an integer")
	})

	t.Run("a number compared against a register holding bytes", func(t *testing.T) {
		d := memory()
		node(t, d, 6).GetRegister().Kind = irpb.RegisterKind_REGISTER_KIND_BYTES
		node(t, d, 7).GetGuard().Test = &irpb.Guard_OneOf{OneOf: &irpb.LiteralSet{
			Values: []*irpb.Literal{{Value: &irpb.Literal_Integer{Integer: 3}}},
		}}

		refused(t, d, "carries a number, and the register it is compared against holds bytes")
	})

	t.Run("a bytes register tested for being greater than zero", func(t *testing.T) {
		d := memory()
		node(t, d, 6).GetRegister().Kind = irpb.RegisterKind_REGISTER_KIND_BYTES

		refused(t, d, "for being greater than zero, and only an integer is a number")
	})

	t.Run("a binding that writes nothing", func(t *testing.T) {
		d := memory()
		node(t, d, 8).GetBinding().Value = nil

		refused(t, d, "writes no value")
	})

	t.Run("two bindings writing one register", func(t *testing.T) {
		d := memory()
		d.Nodes = append(d.GetNodes(), &irpb.Node{Id: 9, Kind: &irpb.Node_Binding{Binding: &irpb.Binding{
			RegisterId: 6,
			Value:      &irpb.Binding_FieldId{FieldId: 3},
		}}})
		node(t, d, 5).GetTransition().BindingIds = []uint64{8, 9}

		refused(t, d, "applies two bindings writing register node 6")
	})

	t.Run("a state that does not accept and guards its acceptance anyway", func(t *testing.T) {
		d := memory()
		state := node(t, d, 4).GetState()
		state.Accepts = false
		state.AcceptanceGuardIds = []uint64{7}

		refused(t, d, "does not accept and still qualifies its acceptance with guards")
	})
}

// TestARepetitionSaysHowManyTimes holds the count to being stated, and a
// variable one to carrying bounds a decoded count can be held to.
func TestARepetitionSaysHowManyTimes(t *testing.T) {
	t.Run("no count", func(t *testing.T) {
		d := valid()
		node(t, d, 3).GetField().Repetition = &irpb.Repetition{}

		refused(t, d, "carries a repetition that states no count")
	})

	t.Run("a constant of zero", func(t *testing.T) {
		d := valid()
		node(t, d, 3).GetField().Repetition = &irpb.Repetition{
			Count: &irpb.Repetition_Constant{Constant: 0},
		}

		refused(t, d, "repeats zero times")
	})

	t.Run("a variable count read from nowhere", func(t *testing.T) {
		d := valid()
		node(t, d, 3).GetField().Repetition = &irpb.Repetition{
			Count: &irpb.Repetition_Variable{Variable: &irpb.VariableCount{
				MinOccurrences: 1,
				MaxOccurrences: 5,
			}},
		}

		refused(t, d, "does not say where the count is read from")
	})

	t.Run("bounds that are no range", func(t *testing.T) {
		d := valid()
		node(t, d, 3).GetField().Repetition = &irpb.Repetition{
			Count: &irpb.Repetition_Variable{Variable: &irpb.VariableCount{
				Count:          &irpb.VariableCount_FieldId{FieldId: 3},
				MinOccurrences: 5,
				MaxOccurrences: 1,
			}},
		}

		refused(t, d, "which is no range at all")
	})
}

// TestADelimitedDatasetSaysWhatItsDelimiterIsAndWhereItStands holds the one
// framing that carries more than its own name.
func TestADelimitedDatasetSaysWhatItsDelimiterIsAndWhereItStands(t *testing.T) {
	t.Run("no framing at all", func(t *testing.T) {
		d := valid()
		node(t, d, 0).GetFile().Framing = nil

		refused(t, d, "states no framing")
	})

	t.Run("no delimiter", func(t *testing.T) {
		d := valid()
		node(t, d, 0).GetFile().Framing = &irpb.File_Delimited{Delimited: &irpb.Delimited{
			Placement: irpb.DelimiterPlacement_DELIMITER_PLACEMENT_TERMINATOR,
		}}

		refused(t, d, "whose delimiter is no bytes at all")
	})

	t.Run("no placement", func(t *testing.T) {
		d := valid()
		node(t, d, 0).GetFile().Framing = &irpb.File_Delimited{Delimited: &irpb.Delimited{
			Delimiter: []byte{0x0a},
		}}

		refused(t, d, "does not say where the delimiter stands")
	})
}

// TestEveryFaultIsReportedRatherThanTheFirst holds the pass to collecting, since
// a descriptor assembled wrong is wrong in the same way in many places at once.
func TestEveryFaultIsReportedRatherThanTheFirst(t *testing.T) {
	d := valid()
	node(t, d, 3).GetField().Usage = irpb.Usage_USAGE_UNSPECIFIED
	node(t, d, 3).GetField().Width = 0
	node(t, d, 5).GetTransition().RecordId = 97

	err := Validate(d)

	if found := len(diag.Diagnostics(err)); found != 3 {
		t.Errorf("the pass reported %d faults, want 3:\n%s", found, diag.Render(err))
	}
}
