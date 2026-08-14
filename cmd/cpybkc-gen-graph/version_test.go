// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

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
	for _, stated := range []uint64{uint64(int32(supportedIRVersion) + 1), uint64(supportedIRVersion)} {
		message = protowire.AppendTag(message, versionField, protowire.VarintType)
		message = protowire.AppendVarint(message, stated)
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

// TestAVersionFieldOfAnotherWireTypeReadsAsTheDecoderReadsIt pins the claim
// [versionOf]'s comment makes: skipping a version field that carries some other
// wire type is the decoder's own rule rather than a rule of this reader's, so
// the two cannot disagree about the same bytes.
//
// protobuf hands a known field arriving under the wrong wire type to the
// unknown-field set rather than failing, which is why the descriptor below
// decodes cleanly and states no version — the same answer this reader gives, and
// a refusal rather than a generation either way.
func TestAVersionFieldOfAnotherWireTypeReadsAsTheDecoderReadsIt(t *testing.T) {
	t.Parallel()

	// The version field, carrying a string where a varint belongs.
	message := protowire.AppendTag(nil, versionField, protowire.BytesType)
	message = protowire.AppendString(message, "one")

	got, err := versionOf(message)
	if err != nil {
		t.Fatalf("versionOf: %v", err)
	}

	var decoded irpb.Descriptor
	if err := proto.Unmarshal(message, &decoded); err != nil {
		t.Fatalf("proto.Unmarshal: %v", err)
	}

	if got != decoded.GetVersion() {
		t.Errorf("this reader reads IR version %d out of the bytes and the decoder reads %d",
			int32(got), int32(decoded.GetVersion()))
	}

	if got != irpb.IrVersion_IR_VERSION_UNSPECIFIED {
		t.Errorf("a version field of another wire type reads as IR version %d, want unspecified", int32(got))
	}
}

// nodesFirst is a descriptor's node list with no version field in front of it,
// which is what a version read has to see past.
func nodesFirst(t *testing.T) []byte {
	t.Helper()

	return marshal(t, descriptorAt(irpb.IrVersion_IR_VERSION_UNSPECIFIED))
}

// TestAnUnstampedBuildReportsTheDevelopmentVersion is the convention this
// repository's commands keep: a build made outside a release says so.
//
// The literal is written out because it is the fact. Derived from the variable
// it is checking, this test would pass on whatever that variable became — which
// is how a release ships a generator reporting a number nobody can map to it
// (#181).
func TestAnUnstampedBuildReportsTheDevelopmentVersion(t *testing.T) {
	if got := reportedVersion(); got != "0.0.0-dev" {
		t.Errorf("an unstamped build reports %q, want %q: a build made outside a release reports the "+
			"development version, and this build was not stamped", got, "0.0.0-dev")
	}
}

// TestTheReportedVersionDropsTheTagsLeadingV is the one difference between the
// version a pipeline stamps into this generator and the version a refusal
// names.
//
// A version is stated to a build as an OCI image tag, `v0.2.0`, because that is
// what the image family is published under. This generator's scheme, like the
// CLI's, is a SemVer 2.0.0 string, and SemVer has no `v` — so a released
// generator quoting the tag verbatim would give the refusal a version in a
// vocabulary the CLI's own --version line does not use, which is the one line a
// reader would be comparing it against.
//
// Not parallel: the stamp is a package variable, and Go resumes a paused
// parallel test only once every serial test has returned, which is what makes a
// serial test that restores what it moved safe beside them.
func TestTheReportedVersionDropsTheTagsLeadingV(t *testing.T) {
	for _, test := range []struct {
		name    string
		stamped string
		want    string
	}{
		{"a release", "v0.2.0", "0.2.0"},
		{"a release candidate", "v0.3.0-rc.1", "0.3.0-rc.1"},
		{"the development version a build made outside a release carries", "v0.0.0-dev", "0.0.0-dev"},
		{"a version already in the reported form", "0.2.0", "0.2.0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			restore := version
			t.Cleanup(func() { version = restore })

			version = test.stamped

			if got := reportedVersion(); got != test.want {
				t.Errorf("a build stamped %q reports %q, want %q", test.stamped, got, test.want)
			}
		})
	}
}

// TestTheRefusalCarriesTheReportedVersionAndNotTheStamp is the same rule seen
// from the refusal, which is the only surface this version reaches a user
// through: docs/plugin/SPEC.md has a plugin name its own version there and
// nowhere else, so a refusal composed from the variable beside [reportedVersion]
// rather than from it is a version nobody would see was wrong until a release.
func TestTheRefusalCarriesTheReportedVersionAndNotTheStamp(t *testing.T) {
	restore := version
	t.Cleanup(func() { version = restore })

	version = "v9.8.7"

	refused := (&unsupportedVersionError{Descriptor: irpb.IrVersion_IR_VERSION_UNSPECIFIED}).Error()

	if !strings.Contains(refused, " 9.8.7 ") {
		t.Errorf("a build stamped %q refused with %q, and the refusal names the version without the tag's `v`",
			version, refused)
	}

	if strings.Contains(refused, "v9.8.7") {
		t.Errorf("a build stamped %q refused with %q, which quotes the image tag rather than this generator's "+
			"version", version, refused)
	}
}
