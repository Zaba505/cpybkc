// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Zaba505/cpybkc/irpb"
)

// TestRunRefusesADescriptorAVersionNewerThanThisGeneratorImplements is the
// check docs/plugin/SPEC.md leaves entirely to the plugin: cpybkc performs no
// handshake and no version negotiation, so nothing has screened the descriptor
// in front of this program.
//
// The descriptor is one version above the highest this generator implements,
// which is the case that does not look like a failure — protobuf hands an
// unknown field to an old reader and tells it to ignore one, so a generator
// that skipped the check would decode it cleanly and draw a picture of an
// automaton it understood only in part.
func TestRunRefusesADescriptorAVersionNewerThanThisGeneratorImplements(t *testing.T) {
	t.Parallel()

	ahead := irpb.IrVersion(int32(supportedIRVersion) + 1)

	out := t.TempDir()
	err := run(vector(t, marshal(t, descriptorAt(ahead)), out), nothing())

	var refusal *unsupportedVersionError
	if !errors.As(err, &refusal) {
		t.Fatalf("run returned %v, want a refusal of IR version %d", err, int32(ahead))
	}

	if refusal.Descriptor != ahead {
		t.Errorf("the refusal is about IR version %d, want %d", int32(refusal.Descriptor), int32(ahead))
	}

	// docs/plugin/SPEC.md: refusing means writing no file beneath --out. A
	// plugin that refused after writing would leave cpybkc a directory it is
	// entitled to merge, holding a diagram of a layout it could not read.
	if entries, err := os.ReadDir(out); err != nil {
		t.Fatalf("reading the output directory: %v", err)
	} else if len(entries) != 0 {
		t.Errorf("the refusal left %d files beneath --out, want none", len(entries))
	}
}

// TestTheRefusalNamesBothVersionsAndThisGeneratorsOwn holds the diagnostic to
// docs/plugin/SPEC.md's "What the refusal must say".
//
// cpybkc never learns there was a mismatch, so no other party can compose this
// message; a refusal naming one number leaves the user unable to tell an
// out-of-date generator from an out-of-date CLI.
func TestTheRefusalNamesBothVersionsAndThisGeneratorsOwn(t *testing.T) {
	t.Parallel()

	ahead := irpb.IrVersion(int32(supportedIRVersion) + 1)

	var stderr bytes.Buffer
	report(&stderr, &unsupportedVersionError{Descriptor: ahead})

	written := stderr.String()

	// Derived from the two versions rather than written out, so that this
	// asserts the refusal names the numbers it was built from whatever
	// supportedIRVersion becomes. A literal here would pass on the wrong
	// numbers the day that constant moves.
	for _, want := range []string{
		fmt.Sprintf("descriptor IR version %d", int32(ahead)),
		fmt.Sprintf("implements IR version %d", int32(supportedIRVersion)),
		pluginName,
		reportedVersion(),
	} {
		if !strings.Contains(written, want) {
			t.Errorf("the refusal does not name %q:\n%s", want, written)
		}
	}

	first, _, _ := strings.Cut(written, "\n")
	if !strings.HasPrefix(first, severityError+severitySeparator) {
		t.Errorf("the refusal opens with %q, want an %s%s diagnostic", first, severityError, severitySeparator)
	}
}

// TestRunRefusesADescriptorStatingNoVersionAtAll covers the other side of the
// same rule. An unspecified version is not a descriptor to proceed on the parts
// of: docs/ir/SPEC.md has a consumer refuse a version it does not know, and
// nothing says which contract a descriptor stating none was written against.
func TestRunRefusesADescriptorStatingNoVersionAtAll(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	err := run(vector(t, marshal(t, descriptorAt(irpb.IrVersion_IR_VERSION_UNSPECIFIED)), out), nothing())

	var refusal *unsupportedVersionError
	if !errors.As(err, &refusal) {
		t.Fatalf("run returned %v, want a refusal", err)
	}

	if got := refusal.Error(); !strings.Contains(got, "states no IR version") {
		t.Errorf("the refusal reads %q, and a descriptor stating no version has none to name", got)
	}
}

// TestEveryDiagnosticThisProgramWritesOpensWithASeverity is the format
// docs/plugin/SPEC.md fixes, over every failure [run] can return rather than
// over the one the version check produces.
//
// cpybkc classifies a line by its severity and files anything else at warning,
// so a failure reported as a bare sentence is one a user meets a level away
// from where they were looking.
func TestEveryDiagnosticThisProgramWritesOpensWithASeverity(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		args []string
	}{
		{name: "an unrecognised option", args: []string{descriptorFlag, "/tmp/one", outFlag, "/tmp/two", optFlag, "colour=blue"}},
		{name: "a format outside the closed set", args: []string{descriptorFlag, "/tmp/one", outFlag, "/tmp/two", optFlag, formatOption + "=svg"}},
		{name: "no descriptor", args: []string{outFlag, "/tmp/two"}},
		{name: "a descriptor that is not there", args: []string{descriptorFlag, filepath.Join(t.TempDir(), "nowhere.binpb"), outFlag, t.TempDir()}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := run(testCase.args, nothing())
			if err == nil {
				t.Fatalf("run accepted %v", testCase.args)
			}

			var stderr bytes.Buffer
			report(&stderr, err)

			for _, line := range strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n") {
				severity, message, separated := strings.Cut(line, severitySeparator)
				if !separated || message == "" {
					t.Errorf("the diagnostic %q is not `<severity>%s<message>`", line, severitySeparator)

					continue
				}

				if severity != severityError && severity != severityNote {
					t.Errorf("the diagnostic %q opens with %q, which is not a severity", line, severity)
				}
			}

			if !strings.HasPrefix(stderr.String(), severityError+severitySeparator) {
				t.Errorf("a failure wrote %q, and a non-zero exit is accompanied by an %s%s line",
					stderr.String(), severityError, severitySeparator)
			}
		})
	}
}

// TestRunWritesTheDocumentEachFormatIsNamedBy is this story's whole output: a
// document beneath --out, named for the notation the invocation asked for, and
// carrying nothing but the generated-by line and an empty diagram.
//
// The contents are asserted as far as the story goes and no further. What is
// inside the diagram is the next story's, and a test written against an empty
// one would have to be deleted rather than extended.
func TestRunWritesTheDocumentEachFormatIsNamedBy(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		format string
		file   string
		opens  string
	}{
		{format: formatMermaid, file: mermaidFile, opens: mermaidGeneratedBy},
		{format: formatDot, file: dotFile, opens: dotGeneratedBy},
	}

	for _, testCase := range testCases {
		t.Run(testCase.format, func(t *testing.T) {
			t.Parallel()

			out := t.TempDir()

			args := append(vector(t, marshal(t, descriptorAt(supportedIRVersion)), out),
				optFlag, formatOption+"="+testCase.format)

			if err := run(args, nothing()); err != nil {
				t.Fatalf("run: %v", err)
			}

			entries, err := os.ReadDir(out)
			if err != nil {
				t.Fatalf("reading the output directory: %v", err)
			}

			if len(entries) != 1 || entries[0].Name() != testCase.file {
				t.Fatalf("a run in %s wrote %v, want %s alone", testCase.format, names(entries), testCase.file)
			}

			written := contents(t, filepath.Join(out, testCase.file))

			if !strings.HasPrefix(written, testCase.opens+"\n") {
				t.Errorf("%s opens with %q, and generated output says so on its first line", testCase.file, written)
			}
		})
	}
}

// TestTheDefaultFormatIsTheOneAManifestGetsWithoutAskingForIt is the option
// table read from the side an adopter who states nothing sees.
func TestTheDefaultFormatIsTheOneAManifestGetsWithoutAskingForIt(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	if err := run(vector(t, marshal(t, descriptorAt(supportedIRVersion)), out), nothing()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(out, mermaidFile)); err != nil {
		t.Errorf("a run stating no %s did not write %s: %v", formatOption, mermaidFile, err)
	}
}

// TestAFormatWithNoDocumentIsAFailureRatherThanTheDefaultOne is the arm no
// argument vector reaches, and the reason it is written as a failure.
//
// A format added to the closed set [parse] admits, with no arm added to
// [document] beside it, would otherwise write the Mermaid document under the
// Mermaid filename and report success — the "silently rounded to a default"
// outcome the option vocabulary refuses on the way in.
func TestAFormatWithNoDocumentIsAFailureRatherThanTheDefaultOne(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	err := write(descriptorAt(supportedIRVersion), out, options{format: "svg", records: defaultRecords})
	if err == nil {
		t.Fatal("a format with no document wrote one anyway")
	}

	if !strings.Contains(err.Error(), "svg") {
		t.Errorf("the failure reads %q and does not name the format it is about", err)
	}

	if entries, err := os.ReadDir(out); err != nil {
		t.Fatalf("reading the output directory: %v", err)
	} else if len(entries) != 0 {
		t.Errorf("a format with no document left %d files beneath --out", len(entries))
	}
}

// TestRunReadsTheDescriptorFromStandardInput covers the `-` form, which cpybkc
// never emits and a plugin MUST accept: it is what makes this generator
// drivable from a pipeline holding a descriptor and nowhere to put it.
func TestRunReadsTheDescriptorFromStandardInput(t *testing.T) {
	t.Parallel()

	out := t.TempDir()
	stdin := bytes.NewReader(marshal(t, descriptorAt(supportedIRVersion)))

	if err := run([]string{descriptorFlag, standardInput, outFlag, out}, stdin); err != nil {
		t.Fatalf("run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(out, mermaidFile)); err != nil {
		t.Errorf("nothing was written from a descriptor on standard input: %v", err)
	}
}

// TestTheSameDescriptorAndOptionsWriteTheSameBytes is docs/plugin/SPEC.md's
// "Determinism", from the side a repetition can see: two invocations differing
// in the paths in their argument vector — which the requirement names as
// something output may not vary with — produce the same file with the same
// contents.
func TestTheSameDescriptorAndOptionsWriteTheSameBytes(t *testing.T) {
	t.Parallel()

	bytesOf := func() map[string]string {
		out := t.TempDir()

		if err := run(vector(t, marshal(t, descriptorAt(supportedIRVersion)), out), nothing()); err != nil {
			t.Fatalf("run: %v", err)
		}

		entries, err := os.ReadDir(out)
		if err != nil {
			t.Fatalf("reading the output directory: %v", err)
		}

		written := map[string]string{}
		for _, entry := range entries {
			written[entry.Name()] = contents(t, filepath.Join(out, entry.Name()))
		}

		return written
	}

	first, second := bytesOf(), bytesOf()

	if len(first) != len(second) {
		t.Fatalf("two runs wrote %d and %d files", len(first), len(second))
	}

	for name, content := range first {
		if second[name] != content {
			t.Errorf("%s differs between two runs of the same descriptor and options", name)
		}
	}
}

// TestRunRefusesADescriptorThatIsNotAnIRMessage keeps a read failure a failure.
// Bytes that are not a protobuf message are reported rather than drawn from,
// and nothing is written for them.
func TestRunRefusesADescriptorThatIsNotAnIRMessage(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	if err := run(vector(t, []byte("this is not a descriptor"), out), nothing()); err == nil {
		t.Fatal("run wrote a document from bytes that are not a descriptor")
	}

	if entries, err := os.ReadDir(out); err != nil {
		t.Fatalf("reading the output directory: %v", err)
	} else if len(entries) != 0 {
		t.Errorf("a descriptor that could not be read left %d files beneath --out", len(entries))
	}
}

// TestRunReportsADescriptorItCannotRead is the path a generator run by hand
// takes most often: a path that is not there, or one whose bytes went away with
// the invocation that wrote them.
func TestRunReportsADescriptorItCannotRead(t *testing.T) {
	t.Parallel()

	args := []string{
		descriptorFlag, filepath.Join(t.TempDir(), "nowhere.binpb"),
		outFlag, t.TempDir(),
	}

	if err := run(args, nothing()); err == nil {
		t.Error("run accepted a descriptor path that names no file")
	}
}

// descriptorAt is a descriptor at the stated IR version.
//
// It carries a node beyond the version field so that what the version check is
// read out of is a whole message rather than one field, which is what makes
// "the version is read before anything else in the message" a claim with
// anything else to be read before.
func descriptorAt(version irpb.IrVersion) *irpb.Descriptor {
	return &irpb.Descriptor{
		Version: version,
		Nodes: []*irpb.Node{
			{
				Id: 1,
				Kind: &irpb.Node_File{File: &irpb.File{
					Framing:      &irpb.File_Unframed{Unframed: &irpb.Unframed{}},
					StartStateId: 2,
				}},
			},
			{
				Id:   2,
				Kind: &irpb.Node_State{State: &irpb.State{Accepts: true}},
			},
		},
	}
}

// marshal is the descriptor's bytes in the encoding a plugin is handed.
func marshal(t *testing.T, d *irpb.Descriptor) []byte {
	t.Helper()

	b, err := proto.Marshal(d)
	if err != nil {
		t.Fatalf("marshalling the descriptor: %v", err)
	}

	return b
}

// vector writes the descriptor to a file and is the argument vector cpybkc
// would run this generator with for it.
func vector(t *testing.T, descriptor []byte, out string) []string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "descriptor.binpb")

	if err := os.WriteFile(path, descriptor, 0o644); err != nil {
		t.Fatalf("writing the descriptor: %v", err)
	}

	return []string{descriptorFlag, path, outFlag, out}
}

// nothing is the standard input of a generator cpybkc ran, which never passes
// `--descriptor -`.
func nothing() *bytes.Reader { return bytes.NewReader(nil) }

// contents is a written file as text.
func contents(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	return string(b)
}

// names is what a directory listing holds, for a failure that has to say what
// was written instead.
func names(entries []os.DirEntry) []string {
	written := make([]string, 0, len(entries))
	for _, entry := range entries {
		written = append(written, entry.Name())
	}

	return written
}
