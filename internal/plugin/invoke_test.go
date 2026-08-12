// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package plugin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Zaba505/cpybkc/internal/emit"
	"github.com/Zaba505/cpybkc/irpb"
)

// The generators below are shell scripts, because docs/plugin/SPEC.md makes one
// a first-class plugin and because a test whose fixture was a compiled binary
// would be testing a toolchain rather than a contract. They are real processes
// against a real filesystem for the reason resolve_test.go gives: what is under
// test is a question about processes, and an abstraction would let this package
// agree with a fake about what one is.

// generator writes an executable plugin named for name, running body, and hands
// back the [Invocation] that runs it into out.
func generator(t *testing.T, name, body, out string) Invocation {
	t.Helper()

	path := filepath.Join(t.TempDir(), Filename(name))

	if err := writeGenerator(path, body); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	return Invocation{Name: name, Path: path, Out: out}
}

// writeGenerator writes an executable plugin, under [syscall.ForkLock] held for
// reading.
//
// The lock is what stops the exec that follows failing ETXTBSY: a fork
// concurrent with the write inherits the descriptor the script is open for
// writing on, and a file open for writing anywhere cannot be executed
// (golang/go#22315). This package's tests run several generators at once by
// design, so the window is not theoretical — it has been seen as `text file
// busy` under load, and the two other packages here that write a script and
// then run it hold the lock for the same reason.
func writeGenerator(path, body string) error {
	syscall.ForkLock.RLock()
	defer syscall.ForkLock.RUnlock()

	return os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755)
}

// env is an environment for a generator: PATH, so that a shell script can reach
// the commands it calls, and whatever else the test is about.
//
// PATH is stated rather than inherited because [Runner.Env] is either the whole
// environment or nothing; a test that left it nil would be asserting against the
// environment the test binary happened to be started with.
func env(kv ...string) []string {
	return append([]string{"PATH=" + os.Getenv("PATH")}, kv...)
}

// descriptor is the message every run below hands over: a whole descriptor
// rather than a version field, so that the bytes being compared are bytes a
// resolved layout would produce.
func descriptor() *irpb.Descriptor {
	return &irpb.Descriptor{
		Version: irpb.IrVersion_IR_VERSION_1,
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

// runner is a runner whose output goes somewhere a test can read but need not.
func runner(t *testing.T, kv ...string) *Runner {
	t.Helper()

	log, _ := recorder()

	return &Runner{Log: log, TempDir: t.TempDir(), Env: env(kv...)}
}

// run runs invocations against [descriptor] and hands back the run's verdict.
func run(t *testing.T, invocations ...Invocation) error {
	t.Helper()

	return runner(t).Run(t.Context(), descriptor(), invocations)
}

// lines is what a plugin recorded, one per line, with the trailing newline
// dropped.
func lines(t *testing.T, path string) []string {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// echoArgv is a generator that records the vector it was handed, one argument
// per line, so that an assertion is against the arguments and not against a
// rendering in which a space and a separator look the same.
const echoArgv = `for arg in "$@"; do echo "$arg" >> "$OUT/argv"; done`

func TestAGeneratorIsRunWithTheVectorTheContractDefines(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	invocation := generator(t, "go", echoArgv, out)
	invocation.Options = []Option{
		{Key: "package", Value: "orders"},
		{Key: "module", Value: "example.com/x"},
	}

	if err := runner(t, "OUT="+out).Run(t.Context(), descriptor(), []Invocation{invocation}); err != nil {
		t.Fatalf("running the generator: %v", err)
	}

	argv := lines(t, filepath.Join(out, "argv"))

	// Nothing beyond the four arguments and the two options:
	// docs/plugin/SPEC.md, "Invocation", forbids cpybkc adding any.
	if len(argv) != 8 {
		t.Fatalf("the generator was passed %d arguments, want 8: %q", len(argv), argv)
	}

	if got, want := argv[0], descriptorFlag; got != want {
		t.Errorf("argv[0] = %q, want %q", got, want)
	}

	if got, want := argv[2], outFlag; got != want {
		t.Errorf("argv[2] = %q, want %q", got, want)
	}

	if got, want := argv[3], out; got != want {
		t.Errorf("--out was passed %q, want %q", got, want)
	}

	// The options are in the order they were declared, which is the order the
	// manifest declared them; docs/plugin/SPEC.md makes the vector a function
	// of the manifest rather than of a map iteration.
	if got, want := argv[4:], []string{optFlag, "package=orders", optFlag, "module=example.com/x"}; !slices.Equal(got, want) {
		t.Errorf("the options were passed as %q, want %q", got, want)
	}
}

func TestAnOptionValueMayBeEmptyOrCarryFurtherEquals(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	invocation := generator(t, "go", echoArgv, out)
	invocation.Options = []Option{
		{Key: "bare", Value: ""},
		{Key: "expr", Value: "a=b=c"},
	}

	if err := runner(t, "OUT="+out).Run(t.Context(), descriptor(), []Invocation{invocation}); err != nil {
		t.Fatalf("running the generator: %v", err)
	}

	argv := lines(t, filepath.Join(out, "argv"))

	if got, want := argv[4:], []string{optFlag, "bare=", optFlag, "expr=a=b=c"}; !slices.Equal(got, want) {
		t.Errorf("the options were passed as %q, want %q", got, want)
	}
}

func TestBothPathsArePassedAbsolute(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	invocation := generator(t, "go", echoArgv, out)

	// A relative --out is what a manifest can perfectly well write, and
	// docs/plugin/SPEC.md says the generator is handed an absolute one all the
	// same: two runs of the same generator from different directories have to
	// be the same invocation.
	working, err := os.Getwd()
	if err != nil {
		t.Fatalf("reading the working directory: %v", err)
	}

	relative, err := filepath.Rel(working, out)
	if err != nil {
		t.Fatalf("relativising %s: %v", out, err)
	}

	invocation.Out = relative

	if err := runner(t, "OUT="+out).Run(t.Context(), descriptor(), []Invocation{invocation}); err != nil {
		t.Fatalf("running the generator: %v", err)
	}

	argv := lines(t, filepath.Join(out, "argv"))

	for i, flag := range map[int]string{1: descriptorFlag, 3: outFlag} {
		if !filepath.IsAbs(argv[i]) {
			t.Errorf("%s was passed %q, which is not absolute", flag, argv[i])
		}
	}

	if got, want := argv[3], out; got != want {
		t.Errorf("--out was passed %q, want the directory it names, %q", got, want)
	}
}

func TestTheDescriptorIsTheBytesEmitIRWrites(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	invocation := generator(t, "go", `cat "$2" > "$4/descriptor"`, out)

	if err := run(t, invocation); err != nil {
		t.Fatalf("running the generator: %v", err)
	}

	handed, err := os.ReadFile(filepath.Join(out, "descriptor"))
	if err != nil {
		t.Fatalf("reading what the generator was handed: %v", err)
	}

	// The comparison is against the function --emit-ir writes with, not against
	// a second encoding assembled here: the claim is that a plugin and an
	// author holding a captured descriptor have the same bytes.
	emitted, err := emit.Marshal(descriptor())
	if err != nil {
		t.Fatalf("emitting the descriptor: %v", err)
	}

	if !bytes.Equal(handed, emitted) {
		t.Errorf("the generator was handed %d bytes, --emit-ir writes %d, and they differ", len(handed), len(emitted))
	}
}

// TestEveryGeneratorOfARunIsHandedTheBytesEmitIRWrites is the equality #157
// settled, asserted where it can be broken.
//
// A run has one descriptor: the manifest names one layout, the layout names the
// copybooks, and nothing a generator entry carries can change either — which is
// what removing the manifest's per-generator `inputs` made true (docs/cli/SPEC.md,
// "Which descriptor is emitted"). The observable consequence is this one, and it
// is the one docs/plugin/SPEC.md rests reproducibility on: two generators of one
// run, declaring different options, are handed the *same bytes*, and those bytes
// are what `--emit-ir <path>` leaves on disk.
//
// The comparison is against [github.com/Zaba505/cpybkc/internal/emit.Write] and
// a second generator rather than against a constant, because a constant would go
// on agreeing with itself if a future run started tailoring a descriptor to the
// generator receiving it — which is the failure this test exists to catch and
// the one an author reproducing a generation by hand would meet as a descriptor
// their generator never saw.
func TestEveryGeneratorOfARunIsHandedTheBytesEmitIRWrites(t *testing.T) {
	t.Parallel()

	first, second := t.TempDir(), t.TempDir()

	// The bodies are identical and the options are not: what differs between
	// the two invocations is everything a generator entry can differ by.
	capture := `cat "$2" > "$4/descriptor"`

	go1 := generator(t, "go", capture, first)
	go1.Options = []Option{{Key: "package_name", Value: "orders"}}

	go2 := generator(t, "json-schema", capture, second)
	go2.Options = []Option{{Key: "draft", Value: "2020-12"}, {Key: "flatten", Value: ""}}

	if err := run(t, go1, go2); err != nil {
		t.Fatalf("running two generators: %v", err)
	}

	// What --emit-ir <path> leaves on disk, written by the function the flag
	// writes with, in the format it defaults to.
	emitted := filepath.Join(t.TempDir(), "ir.binpb")
	if err := emit.Write(emitted, nil, descriptor(), emit.FormatBinary); err != nil {
		t.Fatalf("emitting the descriptor: %v", err)
	}

	want, err := os.ReadFile(emitted)
	if err != nil {
		t.Fatalf("reading the emitted descriptor: %v", err)
	}

	for _, out := range []string{first, second} {
		handed, err := os.ReadFile(filepath.Join(out, "descriptor"))
		if err != nil {
			t.Fatalf("reading what the generator in %s was handed: %v", out, err)
		}

		if !bytes.Equal(handed, want) {
			t.Errorf("the generator in %s was handed %d bytes and --emit-ir writes %d, and they differ",
				out, len(handed), len(want))
		}
	}
}

func TestEachGeneratorGetsItsOwnDescriptorFile(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	// docs/plugin/SPEC.md: cpybkc MUST NOT share a descriptor file between two
	// generators, and MUST write it into a directory it creates for that one
	// invocation and nothing else. One file per invocation is what makes the
	// bytes attributable.
	body := `echo "$2" >> "$OUT/paths"`

	first := generator(t, "go", body, out)
	second := generator(t, "docs", body, out)

	if err := runner(t, "OUT="+out).Run(t.Context(), descriptor(), []Invocation{first, second}); err != nil {
		t.Fatalf("running the generators: %v", err)
	}

	paths := lines(t, filepath.Join(out, "paths"))

	if len(paths) != 2 {
		t.Fatalf("two generators ran and recorded %d descriptors: %q", len(paths), paths)
	}

	if paths[0] == paths[1] {
		t.Errorf("both generators were handed %s", paths[0])
	}

	if filepath.Dir(paths[0]) == filepath.Dir(paths[1]) {
		t.Errorf("both descriptors were written into %s", filepath.Dir(paths[0]))
	}
}

func TestTheDescriptorDoesNotOutliveTheInvocation(t *testing.T) {
	t.Parallel()

	// docs/plugin/SPEC.md: cpybkc MUST remove the file, and the directory
	// holding it, once the generator has exited, whether it exited zero or not.
	for _, status := range []int{0, 1} {
		t.Run(fmt.Sprintf("exit %d", status), func(t *testing.T) {
			t.Parallel()

			out := t.TempDir()

			invocation := generator(t, "go", fmt.Sprintf(`echo "$2" > "$4/path"; exit %d`, status), out)

			err := run(t, invocation)

			if (err != nil) != (status != 0) {
				t.Fatalf("a generator that exited %d reported %v", status, err)
			}

			path := lines(t, filepath.Join(out, "path"))[0]

			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the descriptor at %s is still there: %v", path, err)
			}

			if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the directory %s is still there: %v", filepath.Dir(path), err)
			}
		})
	}
}

func TestTheDescriptorIsWrittenReadOnly(t *testing.T) {
	t.Parallel()

	// The mode is read here rather than through a plugin because a test that
	// asked a shell script whether it could write the file would be asking
	// about the user the tests run as — and as root the answer is yes whatever
	// the mode says.
	path, err := writeDescriptor(t.TempDir(), []byte("descriptor"))
	if err != nil {
		t.Fatalf("writing the descriptor: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	if got := info.Mode().Perm(); got&0o222 != 0 {
		t.Errorf("the descriptor was written %#o, which is writable", got)
	}
}

// rendezvous is a generator that announces itself and then waits for the other
// one to announce itself, giving up after a bounded wait.
//
// Run at the same time the two meet at once. Run one after the other, the first
// waits for a file the second cannot write yet, gives up, and exits non-zero —
// so concurrency is decided by the run's verdict rather than by a duration this
// test would otherwise have to guess at. The sleep is a whole second because
// that is the only argument POSIX requires sleep to accept.
func rendezvous(mine, theirs string) string {
	return fmt.Sprintf(`
		touch "$OUT/%s"
		i=0
		while [ $i -lt 20 ]; do
			if [ -e "$OUT/%s" ]; then exit 0; fi
			sleep 1
			i=$((i + 1))
		done
		exit 1
	`, mine, theirs)
}

func TestGeneratorsRunConcurrently(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	first := generator(t, "go", rendezvous("go", "docs"), out)
	second := generator(t, "docs", rendezvous("docs", "go"), out)

	if err := runner(t, "OUT="+out).Run(t.Context(), descriptor(), []Invocation{first, second}); err != nil {
		t.Fatalf("the two generators did not run at the same time: %v", err)
	}
}

func TestANonZeroExitFailsTheRunAndNamesTheGenerator(t *testing.T) {
	t.Parallel()

	invocation := generator(t, "go", "exit 3", t.TempDir())

	err := run(t, invocation)

	var exit *ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("a generator that exited 3 reported %v, want an *ExitError", err)
	}

	if got, want := exit.Name, "go"; got != want {
		t.Errorf("the failure names %q, want %q", got, want)
	}

	if got, want := exit.Code, 3; got != want {
		t.Errorf("the failure carries exit status %d, want %d", got, want)
	}

	if got := exit.Error(); !strings.Contains(got, `"go"`) {
		t.Errorf("the message does not name the generator: %s", got)
	}
}

func TestEveryFailingGeneratorIsReported(t *testing.T) {
	t.Parallel()

	// Nothing is merged until every generator has succeeded (#43, #44), so a
	// run that stopped at the first failure would buy nothing back and would
	// report whichever generator lost a race.
	first := generator(t, "go", "exit 1", t.TempDir())
	second := generator(t, "docs", "exit 0", t.TempDir())
	third := generator(t, "sql", "exit 2", t.TempDir())

	err := run(t, first, second, third)
	if err == nil {
		t.Fatal("two generators failed and the run reported nothing")
	}

	rendered := err.Error()

	for _, name := range []string{`"go"`, `"sql"`} {
		if !strings.Contains(rendered, name) {
			t.Errorf("the run does not report %s failing: %s", name, rendered)
		}
	}

	if strings.Contains(rendered, `"docs"`) {
		t.Errorf("the run reports the generator that succeeded: %s", rendered)
	}

	// In the order the invocations were declared, so that the same failing run
	// reads the same way twice.
	if strings.Index(rendered, `"go"`) > strings.Index(rendered, `"sql"`) {
		t.Errorf("the failures are not in the order the generators were declared: %s", rendered)
	}
}

func TestAGeneratorKilledBySignalIsReportedAsKilled(t *testing.T) {
	t.Parallel()

	// docs/plugin/SPEC.md requires this to be distinguishable from a non-zero
	// exit: the two need different responses, and a shell reports a killed
	// process as 128 plus the signal, which is a number this must not be read
	// back out of.
	invocation := generator(t, "go", `kill -TERM $$; sleep 30`, t.TempDir())

	err := run(t, invocation)

	var signalled *SignalError
	if !errors.As(err, &signalled) {
		t.Fatalf("a generator killed by SIGTERM reported %v, want a *SignalError", err)
	}

	if got, want := signalled.Signal, syscall.SIGTERM; got != want {
		t.Errorf("the failure names signal %v, want %v", got, want)
	}

	var exit *ExitError
	if errors.As(err, &exit) {
		t.Errorf("a killed generator is also reported as having exited %d", exit.Code)
	}

	if got := signalled.Error(); !strings.Contains(got, `"go"`) {
		t.Errorf("the message does not name the generator: %s", got)
	}
}

func TestCancellingTheRunStopsTheGenerators(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())

	out := t.TempDir()
	invocation := generator(t, "go", `touch "$4/started"; sleep 120`, out)

	done := make(chan error, 1)

	go func() { done <- runner(t, "OUT="+out).Run(ctx, descriptor(), []Invocation{invocation}) }()

	waitFor(t, filepath.Join(out, "started"))
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled run reported no failure")
		}
	case <-time.After(time.Minute):
		t.Fatal("the run outlived the context that was cancelled")
	}
}

// waitFor blocks until path exists, which is how a test waits for a generator
// to have started without guessing at how long that takes.
func waitFor(t *testing.T, path string) {
	t.Helper()

	deadline := time.Now().Add(time.Minute)

	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("%s was never written", path)
}

func TestAGeneratorThatExitsZeroAfterAnErrorDiagnosticSucceeded(t *testing.T) {
	t.Parallel()

	// docs/plugin/SPEC.md: cpybkc MUST NOT fail a run whose generator exited
	// zero after printing `error:`, because the exit status is the verdict and
	// the diagnostics are the explanation.
	invocation := generator(t, "go", `echo "error: ORDER-DETAIL: something" >&2; exit 0`, t.TempDir())

	if err := run(t, invocation); err != nil {
		t.Errorf("a generator that exited zero failed the run: %v", err)
	}
}

func TestAGeneratorThatLeavesAChildHoldingItsOutputStillFinishes(t *testing.T) {
	t.Parallel()

	// The generator exits zero, having started something that inherits its
	// streams and outlives it. The exit status is the verdict, so the run
	// succeeds — and it succeeds after [outputGrace] rather than after the
	// child gets round to exiting, which is the whole reason the delay is set.
	invocation := generator(t, "go", `sleep 120 & echo "note: finished" >&2; exit 0`, t.TempDir())

	log, kept := recorder()
	r := &Runner{Log: log, TempDir: t.TempDir(), Env: env()}

	done := make(chan error, 1)

	go func() { done <- r.Run(t.Context(), descriptor(), []Invocation{invocation}) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a generator that exited zero failed the run: %v", err)
		}
	case <-time.After(time.Minute):
		t.Fatal("the run waited for a child the generator left behind")
	}

	// What the generator itself wrote before exiting is still surfaced.
	if got := kept.messages(stderrStream); !slices.Contains(got, "finished") {
		t.Errorf("the generator's own output was not surfaced: %q", got)
	}
}

func TestAGeneratorThatCannotBeStartedIsNotAnExit(t *testing.T) {
	t.Parallel()

	invocation := Invocation{Name: "go", Path: filepath.Join(t.TempDir(), Filename("go")), Out: t.TempDir()}

	err := run(t, invocation)

	var start *StartError
	if !errors.As(err, &start) {
		t.Fatalf("a generator that is not there reported %v, want a *StartError", err)
	}

	if got := start.Error(); !strings.Contains(got, `"go"`) {
		t.Errorf("the message does not name the generator: %s", got)
	}
}

func TestTheEnvironmentReachesTheGeneratorUnchanged(t *testing.T) {
	t.Parallel()

	out := t.TempDir()
	invocation := generator(t, "go", `echo "$SOURCE_DATE_EPOCH" > "$4/epoch"`, out)

	if err := runner(t, "SOURCE_DATE_EPOCH=1700000000").Run(t.Context(), descriptor(), []Invocation{invocation}); err != nil {
		t.Fatalf("running the generator: %v", err)
	}

	if got, want := lines(t, filepath.Join(out, "epoch"))[0], "1700000000"; got != want {
		t.Errorf("the generator saw SOURCE_DATE_EPOCH=%q, want %q", got, want)
	}
}

func TestAnInvocationThatCannotBeWrittenAsAVectorRunsNothing(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	good := generator(t, "go", `touch "$4/ran"`, out)

	bad := generator(t, "docs", "exit 0", t.TempDir())
	bad.Options = []Option{{Key: "package=orders", Value: "x"}}

	err := run(t, good, bad)

	var invalid *InvalidOptionError
	if !errors.As(err, &invalid) {
		t.Fatalf("an option key carrying an = reported %v, want an *InvalidOptionError", err)
	}

	if got, want := invalid.Key, "package=orders"; got != want {
		t.Errorf("the failure names the key %q, want %q", got, want)
	}

	// Checked before anything starts: an invocation the caller assembled wrong
	// is knowable here, unlike an option key only the plugin knows about.
	if _, err := os.Stat(filepath.Join(out, "ran")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a generator ran beside an invocation that could not be written as a vector: %v", err)
	}
}

func TestAnEmptyOptionKeyIsRefused(t *testing.T) {
	t.Parallel()

	invocation := generator(t, "go", "exit 0", t.TempDir())
	invocation.Options = []Option{{Key: "", Value: "orders"}}

	err := run(t, invocation)

	var invalid *InvalidOptionError
	if !errors.As(err, &invalid) {
		t.Fatalf("an empty option key reported %v, want an *InvalidOptionError", err)
	}

	if got := invalid.Error(); !strings.Contains(got, "empty") {
		t.Errorf("the message does not say the key is empty: %s", got)
	}
}

func TestAnInvocationMissingWhatARunNeedsIsRefused(t *testing.T) {
	t.Parallel()

	for name, invocation := range map[string]Invocation{
		"no executable": {Name: "go", Out: t.TempDir()},
		"no directory":  {Name: "go", Path: "/usr/bin/cpybkc-gen-go"},
		"no name":       {Path: "/usr/bin/cpybkc-gen-go", Out: t.TempDir()},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := run(t, invocation); err == nil {
				t.Errorf("an invocation with %s was accepted", name)
			}
		})
	}
}

func TestARunWithNoGeneratorsDoesNothing(t *testing.T) {
	t.Parallel()

	if err := run(t); err != nil {
		t.Errorf("a run with no generators reported %v", err)
	}
}
