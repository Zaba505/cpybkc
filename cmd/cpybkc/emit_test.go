// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Zaba505/cpybkc/internal/emit"
	"github.com/Zaba505/cpybkc/internal/generate"
	"github.com/Zaba505/cpybkc/irpb"
)

// TestTheEmissionFlagsAreSpelledTheWayTheEncoderNamesThem is the drift guard the
// parser's literals need.
//
// internal/emit names the flag it is behind, the format spellings it accepts and
// the dash that means standard output, and this command writes all five out
// again — because the companion module's CLI-surface check reads these constants
// to decide which flags cpybkc accepts, and it reports a value assembled from
// another package as one it could not evaluate rather than as a flag. Two
// spellings of one covered guarantee need something that fails when they part,
// and a compiler cannot: both sides are strings, and the run that would notice
// is one where a caller's --emit-ir-format binary reaches [emit.Write] as a
// format it does not know, after the manifest has been read.
func TestTheEmissionFlagsAreSpelledTheWayTheEncoderNamesThem(t *testing.T) {
	t.Parallel()

	for _, spelling := range []struct {
		constant string
		got      string
		want     string
	}{
		{constant: "emitIRFlag", got: emitIRFlag, want: "--" + emit.Flag},
		{constant: "emitIRFormatFlag", got: emitIRFormatFlag, want: "--" + emit.FormatFlag},
		{constant: "binaryFormat", got: binaryFormat, want: string(emit.FormatBinary)},
		{constant: "jsonFormat", got: jsonFormat, want: string(emit.FormatJSON)},
		{constant: "standardOutput", got: standardOutput, want: emit.Stdout},
	} {
		if spelling.got != spelling.want {
			t.Errorf("%s is %q, and internal/emit spells it %q", spelling.constant, spelling.got, spelling.want)
		}
	}
}

// handedToAGenerator runs the project in dir the ordinary way and returns the
// descriptor the generator was handed, byte for byte.
//
// It is the other end of the equality every test below is about: the generator
// script copies the file it was invoked with into its output directory, so these
// are the bytes a real process received over a real invocation rather than a
// second encoding this test performed.
func handedToAGenerator(t *testing.T, dir, bin string) string {
	t.Helper()

	if _, stderr, code := generateIn(t, dir, bin); code != statusOK {
		t.Fatalf("the generating run exited %d, want %d:\n%s", code, statusOK, stderr)
	}

	return read(t, filepath.Join(dir, "gen", "one.ir"))
}

// TestEmittingWritesTheBytesEveryGeneratorWouldBeHanded is the criterion the
// flag exists for, in its strong form: the binary emission is the same bytes a
// generator of the same run received, not a descriptor assembled the same way.
//
// Both destinations are driven, because docs/cli/SPEC.md makes them the same
// bytes by the same rule — a caller redirecting `--emit-ir -` must be reading
// what a caller naming a path would have got.
func TestEmittingWritesTheBytesEveryGeneratorWouldBeHanded(t *testing.T) {
	bin := t.TempDir()
	install(t, bin, "one", generatorScript("one"))

	dir := projectIn(t, `[{"name": "one", "out": "gen"}]`)

	handed := handedToAGenerator(t, dir, bin)

	if len(handed) == 0 {
		t.Fatal("the generator was handed an empty descriptor, so nothing below is comparing anything")
	}

	dest := filepath.Join(t.TempDir(), "orders.binpb")

	stdout, stderr, code := generateIn(t, dir, bin, emitIRFlag, dest)
	if code != statusOK {
		t.Fatalf("an emitting run exited %d, want %d:\n%s", code, statusOK, stderr)
	}

	// docs/cli/SPEC.md: a path destination puts nothing on standard output.
	if stdout != "" {
		t.Errorf("emitting to a path wrote %q to standard output", stdout)
	}

	if got := read(t, dest); got != handed {
		t.Errorf("the emitted descriptor is not the one a generator was handed: %d bytes against %d",
			len(got), len(handed))
	}

	// The same run, asked for the descriptor on standard output.
	stdout, stderr, code = generateIn(t, dir, bin, emitIRFlag, standardOutput)
	if code != statusOK {
		t.Fatalf("emitting to %q exited %d, want %d:\n%s", standardOutput, code, statusOK, stderr)
	}

	if stdout != handed {
		t.Errorf("the descriptor on standard output is not the one a generator was handed: %d bytes against %d",
			len(stdout), len(handed))
	}

	// And it shares that stream with nothing, which is the whole of what keeps
	// `--emit-ir -` pipeable: a line landing in the middle of the wire encoding
	// produces a file that fails to decode nowhere near the mistake.
	if stderr != "" {
		t.Errorf("an emitting run said %q, and it succeeded", stderr)
	}
}

// TestEmittingJSONRendersTheSameDescriptor is the JSON form's half of the same
// criterion. The rendering is one-way — nothing reads it back — so what can be
// held is that it renders the descriptor the binary form carries, through the
// one renderer #21 specified, rather than some other descriptor or some other
// spacing.
func TestEmittingJSONRendersTheSameDescriptor(t *testing.T) {
	bin := t.TempDir()
	install(t, bin, "one", generatorScript("one"))

	dir := projectIn(t, `[{"name": "one", "out": "gen"}]`)

	handed := handedToAGenerator(t, dir, bin)

	var d irpb.Descriptor
	if err := proto.Unmarshal([]byte(handed), &d); err != nil {
		t.Fatalf("the descriptor a generator was handed does not decode: %v", err)
	}

	want, err := emit.MarshalJSON(&d)
	if err != nil {
		t.Fatalf("render the descriptor: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "orders.json")

	stdout, stderr, code := generateIn(t, dir, bin, emitIRFlag, dest, emitIRFormatFlag, jsonFormat)
	if code != statusOK {
		t.Fatalf("an emitting run exited %d, want %d:\n%s", code, statusOK, stderr)
	}

	if stdout != "" {
		t.Errorf("emitting JSON to a path wrote %q to standard output", stdout)
	}

	if got := read(t, dest); got != string(want) {
		t.Errorf("the rendered descriptor is not the rendering of the one a generator was handed\n got:\n%s\nwant:\n%s",
			got, want)
	}

	// The same rendering, on standard output.
	stdout, stderr, code = generateIn(t, dir, bin, emitIRFlag, standardOutput, emitIRFormatFlag, jsonFormat)
	if code != statusOK {
		t.Fatalf("emitting JSON to %q exited %d, want %d:\n%s", standardOutput, code, statusOK, stderr)
	}

	if stdout != string(want) {
		t.Errorf("the rendering on standard output differs from the one written to a path\n got:\n%s\nwant:\n%s",
			stdout, want)
	}
}

// TestTheDefaultEmissionIsTheCanonicalForm covers the default docs/cli/SPEC.md
// fixes: a line that names no format gets the bytes the rest of the system uses,
// and the debug form is the one you ask for by name.
//
// The two emissions are compared rather than the default one being decoded,
// because decoding is satisfied by too much: protobuf reads empty input as an
// empty message and reports no failure, so a default that emitted nothing at all
// would pass. Comparing says the stronger thing — the default is *this* format —
// and the emptiness check is what stops two silent emissions agreeing.
func TestTheDefaultEmissionIsTheCanonicalForm(t *testing.T) {
	bin := t.TempDir()
	install(t, bin, "one", generatorScript("one"))

	dir := projectIn(t, `[{"name": "one", "out": "gen"}]`)

	byDefault, stderr, code := generateIn(t, dir, bin, emitIRFlag, standardOutput)
	if code != statusOK {
		t.Fatalf("an emitting run exited %d, want %d:\n%s", code, statusOK, stderr)
	}

	if byDefault == "" {
		t.Fatal("an emitting run that named no format wrote nothing")
	}

	byName, stderr, code := generateIn(t, dir, bin, emitIRFlag, standardOutput, emitIRFormatFlag, binaryFormat)
	if code != statusOK {
		t.Fatalf("emitting %s by name exited %d, want %d:\n%s", binaryFormat, code, statusOK, stderr)
	}

	if byDefault != byName {
		t.Errorf("the default emission is not the %s form: %d bytes against %d",
			binaryFormat, len(byDefault), len(byName))
	}

	// And it is the encoding a consumer decodes, rather than two runs agreeing
	// on something that is neither format.
	if err := proto.Unmarshal([]byte(byDefault), &irpb.Descriptor{}); err != nil {
		t.Fatalf("the default emission does not decode as a descriptor: %v", err)
	}
}

// TestEmittingStartsNoGenerator is docs/cli/SPEC.md's "Emitting replaces
// generation", asserted in the two ways it can fail.
//
// The first half is the one that catches a run which merely emits *as well*: the
// generator would leave a mark, and an emitting run leaves none, alongside no
// output directory and no record of a run. The second is the one that catches a
// run which resolves generators before deciding — the manifest names a generator
// that is on no PATH, so a run that searched would fail, and this one succeeds.
func TestEmittingStartsNoGenerator(t *testing.T) {
	t.Run("an installed generator is not started", func(t *testing.T) {
		bin := t.TempDir()
		marker := filepath.Join(t.TempDir(), "ran")

		install(t, bin, "one", "#!/bin/sh\ntouch "+marker+"\n")

		dir := projectIn(t, `[{"name": "one", "out": "gen"}]`)
		dest := filepath.Join(dir, "orders.binpb")

		_, stderr, code := generateIn(t, dir, bin, emitIRFlag, dest)
		if code != statusOK {
			t.Fatalf("an emitting run exited %d, want %d:\n%s", code, statusOK, stderr)
		}

		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Errorf("the generator ran: %v", err)
		}

		if _, err := os.Stat(filepath.Join(dir, "gen")); !os.IsNotExist(err) {
			t.Errorf("an emitting run left an output directory behind: %v", err)
		}

		if _, err := os.Stat(filepath.Join(dir, generate.RecordName)); !os.IsNotExist(err) {
			t.Errorf("an emitting run left a record of a generation behind: %v", err)
		}

		if _, err := os.Stat(dest); err != nil {
			t.Errorf("an emitting run wrote no descriptor: %v", err)
		}
	})

	t.Run("no generator is resolved on PATH", func(t *testing.T) {
		bin := t.TempDir()
		dir := projectIn(t, `[{"name": "nowhere", "out": "gen"}]`)

		// The control: generating this project fails, because the generator it
		// names is on no PATH. Without it, the run below could be succeeding for
		// a reason that has nothing to do with the flag.
		if _, _, code := generateIn(t, dir, bin); code != statusFailed {
			t.Fatalf("generating a project whose generator is on no PATH exited %d, want %d", code, statusFailed)
		}

		dest := filepath.Join(dir, "orders.binpb")

		_, stderr, code := generateIn(t, dir, bin, emitIRFlag, dest)
		if code != statusOK {
			t.Fatalf("an emitting run exited %d, want %d; it resolved a generator it had no reason to:\n%s",
				code, statusOK, stderr)
		}

		if _, err := os.Stat(dest); err != nil {
			t.Errorf("an emitting run wrote no descriptor: %v", err)
		}
	})
}

// TestAnEmissionThatCannotBeWrittenFails is the failure this flag adds, held to
// the statuses and the stream every other failure of the work is held to:
// exit 1, an `error:` diagnostic naming the path, and no usage, because the
// vector was understood.
func TestAnEmissionThatCannotBeWrittenFails(t *testing.T) {
	bin := t.TempDir()
	install(t, bin, "one", generatorScript("one"))

	dir := projectIn(t, `[{"name": "one", "out": "gen"}]`)

	// A parent directory that is not there. docs/cli/SPEC.md's rule is that a
	// path typed on the command line is resolved against the working directory
	// and opened, not that a tree is made for it.
	dest := filepath.Join(dir, "nowhere", "orders.binpb")

	stdout, stderr, code := generateIn(t, dir, bin, emitIRFlag, dest)
	if code != statusFailed {
		t.Fatalf("an emission that cannot be written exited %d, want %d:\n%s", code, statusFailed, stderr)
	}

	if stdout != "" {
		t.Errorf("a failed emission wrote %q to standard output", stdout)
	}

	if !strings.HasPrefix(stderr, severityError+severitySeparator) {
		t.Errorf("a failed emission reported %q, want an %s%s line", stderr, severityError, severitySeparator)
	}

	if !strings.Contains(stderr, "orders.binpb") {
		t.Errorf("the diagnostic does not name the path that could not be written: %q", stderr)
	}

	// A fault in the work is not a fault in the vector.
	if strings.Contains(stderr, "Usage:") {
		t.Errorf("a failed emission was answered with usage: %q", stderr)
	}
}

// TestAFormatThatIsNeitherEncodingIsAUsageError is the last of the vector's
// refusals reached through a whole invocation rather than through the parser:
// status 2 means cpybkc did nothing at all, so the file the line named is not
// there afterwards.
func TestAFormatThatIsNeitherEncodingIsAUsageError(t *testing.T) {
	bin := t.TempDir()
	install(t, bin, "one", generatorScript("one"))

	dir := projectIn(t, `[{"name": "one", "out": "gen"}]`)
	dest := filepath.Join(dir, "orders.out")

	stdout, stderr, code := generateIn(t, dir, bin, emitIRFlag, dest, emitIRFormatFlag, "xml")
	if code != statusUsage {
		t.Fatalf("a format that is neither encoding exited %d, want %d:\n%s", code, statusUsage, stderr)
	}

	if stdout != "" {
		t.Errorf("a usage error wrote %q to standard output", stdout)
	}

	if !strings.Contains(stderr, emitIRFormatFlag) {
		t.Errorf("the diagnostic does not name the flag that was refused: %q", stderr)
	}

	// The vector is the one failure a caller can fix without knowing anything
	// about the project, so the command set accompanies it.
	if !strings.Contains(stderr, "Usage:") {
		t.Errorf("a usage error was reported without usage: %q", stderr)
	}

	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("a vector cpybkc could not understand wrote %s anyway: %v", dest, err)
	}
}

// TestAnEmissionIsAFunctionOfItsInputs is #47 through this flag: two emissions
// of one project are byte-identical files, which is what makes a descriptor
// committed beside a layout a diff of the layout rather than of the run.
func TestAnEmissionIsAFunctionOfItsInputs(t *testing.T) {
	bin := t.TempDir()
	install(t, bin, "one", generatorScript("one"))

	dir := projectIn(t, `[{"name": "one", "out": "gen"}]`)
	out := t.TempDir()

	for _, format := range []string{binaryFormat, jsonFormat} {
		first := filepath.Join(out, "first."+format)
		if _, stderr, code := generateIn(t, dir, bin, emitIRFlag, first, emitIRFormatFlag, format); code != statusOK {
			t.Fatalf("the first %s emission exited %d:\n%s", format, code, stderr)
		}

		second := filepath.Join(out, "second."+format)
		if _, stderr, code := generateIn(t, dir, bin, emitIRFlag, second, emitIRFormatFlag, format); code != statusOK {
			t.Fatalf("the second %s emission exited %d:\n%s", format, code, stderr)
		}

		if read(t, first) != read(t, second) {
			t.Errorf("two %s emissions of one project differ", format)
		}
	}
}
