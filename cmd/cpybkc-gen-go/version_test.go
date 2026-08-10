// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/Zaba505/cpybkc/irpb"
)

// TestTheVersionIsReadOffTheWire is what "read the version before anything else
// in the message" is made of: the version comes out of the bytes, without the
// message being decoded and without anything else in it being interpreted.
func TestTheVersionIsReadOffTheWire(t *testing.T) {
	t.Parallel()

	got, err := versionOf(marshal(t, descriptorAt(supportedIRVersion)))
	if err != nil {
		t.Fatalf("versionOf: %v", err)
	}

	if got != supportedIRVersion {
		t.Errorf("the descriptor reads as IR version %d, want %d", int32(got), int32(supportedIRVersion))
	}
}

// TestTheVersionIsFoundWhereverItIsOnTheWire is why the read is a scan rather
// than a look at the first field.
//
// ir.proto numbers the version 1 so that it precedes the node list for a
// producer serializing in field-number order, which is what makes reading it
// first cheap. It is not what makes it correct: the field order in a protobuf
// message is not guaranteed, and a descriptor written by something other than
// this project's encoder may carry the version anywhere.
func TestTheVersionIsFoundWhereverItIsOnTheWire(t *testing.T) {
	t.Parallel()

	// A node, and then the version behind it.
	message := append([]byte{}, nodesFirst(t)...)
	message = protowire.AppendTag(message, versionField, protowire.VarintType)
	message = protowire.AppendVarint(message, uint64(supportedIRVersion))

	got, err := versionOf(message)
	if err != nil {
		t.Fatalf("versionOf: %v", err)
	}

	if got != supportedIRVersion {
		t.Errorf("a descriptor carrying its version behind its nodes reads as IR version %d, want %d",
			int32(got), int32(supportedIRVersion))
	}
}

// TestAVersionStatedTwiceIsTheLastOne is protobuf's own rule for a scalar
// field, and it is the rule this reader follows rather than a rule of its own:
// a descriptor a decoder would read as version 1 must not be refused as version
// 2 by the check in front of the decoder.
func TestAVersionStatedTwiceIsTheLastOne(t *testing.T) {
	t.Parallel()

	var message []byte

	// The first value is derived rather than written out, so that the two
	// stated versions differ whatever supportedIRVersion becomes — a literal
	// here would one day state the same version twice and assert nothing.
	for _, version := range []uint64{uint64(int32(supportedIRVersion) + 1), uint64(supportedIRVersion)} {
		message = protowire.AppendTag(message, versionField, protowire.VarintType)
		message = protowire.AppendVarint(message, version)
	}

	got, err := versionOf(message)
	if err != nil {
		t.Fatalf("versionOf: %v", err)
	}

	if got != supportedIRVersion {
		t.Errorf("a version stated twice reads as %d, want the last one, %d", int32(got), int32(supportedIRVersion))
	}
}

// TestADescriptorStatingNoVersionReadsAsUnspecified is the value the refusal is
// composed from when the field is absent. It is not an error to read: it is a
// version this generator does not implement, which [run] refuses like any
// other.
func TestADescriptorStatingNoVersionReadsAsUnspecified(t *testing.T) {
	t.Parallel()

	got, err := versionOf(nodesFirst(t))
	if err != nil {
		t.Fatalf("versionOf: %v", err)
	}

	if got != irpb.IrVersion_IR_VERSION_UNSPECIFIED {
		t.Errorf("a descriptor with no version field reads as IR version %d, want unspecified", int32(got))
	}
}

// TestAVersionAheadOfThisGeneratorIsReadRatherThanRejectedAsUnknown is the case
// the whole check exists for. The enum has no member for it, and the number has
// to survive being read anyway — a refusal that could not name the descriptor's
// version would leave the user unable to tell which end is behind.
func TestAVersionAheadOfThisGeneratorIsReadRatherThanRejectedAsUnknown(t *testing.T) {
	t.Parallel()

	ahead := irpb.IrVersion(int32(supportedIRVersion) + 1)

	got, err := versionOf(marshal(t, descriptorAt(ahead)))
	if err != nil {
		t.Fatalf("versionOf: %v", err)
	}

	if got != ahead {
		t.Errorf("a descriptor a version ahead reads as IR version %d, want %d", int32(got), int32(ahead))
	}
}

// TestBytesThatAreNotAProtobufMessageAreReported keeps a malformed descriptor a
// failure rather than a version of zero, which would be refused with a
// diagnostic about a version mismatch that is not what happened.
func TestBytesThatAreNotAProtobufMessageAreReported(t *testing.T) {
	t.Parallel()

	// A tag naming a field, and then nothing where its value belongs.
	truncated := protowire.AppendTag(nil, versionField, protowire.VarintType)

	if _, err := versionOf(truncated); err == nil {
		t.Error("versionOf read a version out of a truncated message")
	}

	if _, err := versionOf([]byte{0xff, 0xff, 0xff, 0xff}); err == nil {
		t.Error("versionOf read a version out of bytes that are not a message")
	}
}

// nodesFirst is a descriptor's node list with no version field in front of it,
// which is what a version read has to see past.
func nodesFirst(t *testing.T) []byte {
	t.Helper()

	stated := descriptorAt(irpb.IrVersion_IR_VERSION_UNSPECIFIED)

	return marshal(t, stated)
}
