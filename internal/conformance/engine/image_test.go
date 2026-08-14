// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Zaba505/cpybkc/internal/conformance/engine"
)

// runtimeEnv is the variable that turns this test binary into a container
// runtime, and names the file it records its argument vectors in.
//
// A stub rather than the real thing, because a test that needed a container
// runtime would be a test that does not run: the pipeline this repository's
// verify command drives is itself a container, and a machine with no daemon on
// it is the ordinary case for a contributor. What the stub covers is everything
// the door is: the argument vector it builds, the boundary between the door's
// flags and the container's, the removal, and that the same conversation comes
// back through it. What it cannot cover is whether Linux honoured the flags,
// which is the runtime's promise and not this door's.
const runtimeEnv = "CPYBKC_CONFORMANCE_FAKE_RUNTIME"

// fakeImage is the reference the stub answers to, hangImage is the one it
// answers by never answering, and unknownImage is the one it refuses the way a
// runtime refuses an image it cannot get: exit 125, before any container
// exists.
const (
	fakeImage     = "ghcr.io/example/fake-adapter:v1"
	hangImage     = "ghcr.io/example/never-ends:v1"
	unknownImage  = "ghcr.io/example/not-published:v1"
	unknownStatus = 125
)

// TestBothDoorsDriveOneContract is the story's first line and the reason [Door]
// is an interface at all: the contract begins after the process exists, so a
// run through a container has to be the same conversation with the same
// outcomes, and the door has to be the only thing that differs.
func TestBothDoorsDriveOneContract(t *testing.T) {
	entries := corpus(t, "packed-ebcdic", "packed-invalid-sign", "zoned-ebcdic")
	told := script{Entries: answers(t, entries)}

	command, commandTranscript := door(t, told)
	image, imageTranscript, _ := imageDoor(t, told)

	direct, err := (&engine.Engine{Door: command}).Run(t.Context(), entries)
	if err != nil {
		t.Fatalf("the run through the command door could not be made: %v", err)
	}

	contained, err := (&engine.Engine{Door: image}).Run(t.Context(), entries)
	if err != nil {
		t.Fatalf("the run through the image door could not be made: %v", err)
	}

	if contained.Failed() {
		t.Fatalf("the adapter answered what every entry states and the containerised run failed:\n%v", contained)
	}

	// The frames, which are the contract itself. An image door that sent a
	// different conversation would be a second dialect of one specification.
	if got, want := transcript(t, imageTranscript), transcript(t, commandTranscript); got != want {
		t.Errorf("the two doors held different conversations:\nthrough the image:\n%s\ndirectly:\n%s", got, want)
	}

	if len(contained.Results) != len(direct.Results) {
		t.Fatalf("the image door reported %d entries and the command door %d",
			len(contained.Results), len(direct.Results))
	}

	for i, result := range contained.Results {
		if result.Entry != direct.Results[i].Entry || result.Outcome != direct.Results[i].Outcome {
			t.Errorf("entry %d came back as %s %s through the image and %s %s directly",
				i, result.Outcome, result.Entry, direct.Results[i].Outcome, direct.Results[i].Entry)
		}
	}

	// And the one thing that must differ, because it is the whole of what the
	// two doors are worth to a reader of the result.
	if contained.Door == direct.Door {
		t.Errorf("both reports describe the same door: %s", contained.Door)
	}
}

// TestTheImageDoorIsolatesTheAdapter is the guarantee the door exists to
// provide, asserted where it is actually made — in the argument vector handed
// to the runtime, since nothing else in this process can observe a namespace.
func TestTheImageDoorIsolatesTheAdapter(t *testing.T) {
	entries := corpus(t, "packed-ebcdic")

	image, _, record := imageDoor(t, script{Entries: answers(t, entries)})
	image.Memory = "512m"
	image.Processes = 32
	image.Scratch = "64m"

	if _, err := (&engine.Engine{Door: image}).Run(t.Context(), entries); err != nil {
		t.Fatalf("the run could not be made: %v", err)
	}

	run := invocation(t, record, "run")

	for _, want := range []string{
		"--network=none",
		"--read-only",
		"--tmpfs=/tmp:rw,nosuid,nodev,size=64m",
		"--memory=512m",
		"--memory-swap=512m",
		"--pids-limit=32",
		"--cap-drop=ALL",
		"--security-opt=no-new-privileges",
		"--interactive",
		"--rm",
	} {
		if !slices.Contains(run, want) {
			t.Errorf("the container was not started with %s: %v", want, run)
		}
	}

	// A TTY would echo what the engine writes and rewrite its line endings, and
	// the conversation is newline-delimited JSON in both directions: the
	// engine's own request frames would come back to it as answers.
	if slices.Contains(run, "--tty") || slices.Contains(run, "-t") {
		t.Errorf("the container was given a terminal, and the conversation is framed: %v", run)
	}
}

// TestTheImageReferenceEndsTheDoorsArgumentsAndBeginsTheContainers pins the one
// boundary in the vector that a mistake would make invisible: an adapter's own
// argument landing among the runtime's flags is either refused by the runtime or,
// worse, accepted by it.
func TestTheImageReferenceEndsTheDoorsArgumentsAndBeginsTheContainers(t *testing.T) {
	entries := corpus(t, "packed-ebcdic")

	image, _, record := imageDoor(t, script{Entries: answers(t, entries)})
	image.Args = append(image.Args, "--generator", "./gen-rust")

	if _, err := (&engine.Engine{Door: image}).Run(t.Context(), entries); err != nil {
		t.Fatalf("the run could not be made: %v", err)
	}

	run := invocation(t, record, "run")

	at := -1

	for i, arg := range run {
		if arg == fakeImage {
			at = i

			break
		}
	}

	if at < 0 {
		t.Fatalf("the image was never named to the runtime: %v", run)
	}

	for _, arg := range run[1:at] {
		if !strings.HasPrefix(arg, "-") {
			t.Errorf("%q sits between `run` and the image and is not a flag, so the image reference "+
				"cannot be read off the vector: %v", arg, run)
		}
	}

	if got := strings.Join(run[at+1:], " "); !strings.HasSuffix(got, "--generator ./gen-rust") {
		t.Errorf("the container's own arguments are not what followed the image: %q", got)
	}
}

// TestTheReportSaysWhichDoorProducedIt is the fourth acceptance criterion, and
// the rule underneath it is docs/adapter/SPEC.md's: an engine MUST NOT report a
// result as though it carried a guarantee its door did not provide. The
// description is the door's own words, so what is asserted is that the numbers
// in it are the ones the run actually used.
func TestTheReportSaysWhichDoorProducedIt(t *testing.T) {
	entries := corpus(t, "packed-ebcdic")

	image, _, _ := imageDoor(t, script{Entries: answers(t, entries)})
	image.Memory = "512m"
	image.Processes = 32
	image.Timeout = 20 * time.Minute

	report, err := (&engine.Engine{Door: image}).Run(t.Context(), entries)
	if err != nil {
		t.Fatalf("the run could not be made: %v", err)
	}

	for _, want := range []string{fakeImage, "no network", "read-only root", "512m", "32 processes", "20m0s"} {
		if !strings.Contains(report.Door, want) {
			t.Errorf("the report does not say %q about the door it came through: %s", want, report.Door)
		}
	}

	if !strings.Contains(report.String(), report.Door) {
		t.Errorf("the report as a person reads it does not name the door:\n%s", report)
	}
}

// TestTheWallClockTakesTheContainerAway is the third property of the door, and
// the removal is the part a plain command does not need: killing a client that
// is attached to a container does not stop the container, so a door that only
// killed the client would leave an adapter running with every cap it was given
// and nobody talking to it.
func TestTheWallClockTakesTheContainerAway(t *testing.T) {
	image, _, record := imageDoor(t, script{})
	image.Reference = hangImage
	image.Timeout = 200 * time.Millisecond

	process, err := image.Open(t.Context())
	if err != nil {
		t.Fatalf("the door would not open: %v", err)
	}

	err = process.Wait()
	if err == nil {
		t.Fatal("a container that never ended came back as one that ended well")
	}

	if !strings.Contains(err.Error(), "wall-clock") {
		t.Errorf("the failure does not say the door's bound ended the run: %v", err)
	}

	assertRemoved(t, record)
}

// TestKillingTheAdapterTakesItsContainerAway is the same removal by the route
// the engine actually takes: it kills an adapter that broke, and then reaps it.
func TestKillingTheAdapterTakesItsContainerAway(t *testing.T) {
	image, _, record := imageDoor(t, script{})
	image.Reference = hangImage

	process, err := image.Open(t.Context())
	if err != nil {
		t.Fatalf("the door would not open: %v", err)
	}

	// The container is waited for before it is killed. Open returns when the
	// runtime client has started, which is before the client has done anything,
	// and a test that killed it in that window would be asserting the removal of
	// a container the stub had not recorded starting.
	invocation(t, record, "run")

	process.Kill()

	_ = process.Wait()

	assertRemoved(t, record)
}

// TestARuntimeThatCouldNotRunTheImageIsNotAnAdapterThatSaidNothing is the
// failure this door has that a command door does not: the process the engine
// gets is the runtime's client, so an image that does not exist, a flag the
// daemon will not take or an entrypoint that cannot be run all arrive as a
// client that exited after the door had already opened — which, unexplained,
// reads as an adapter whose stream stopped and sends a generator author to look
// at their generator.
func TestARuntimeThatCouldNotRunTheImageIsNotAnAdapterThatSaidNothing(t *testing.T) {
	entries := corpus(t, "packed-ebcdic")

	image, _, _ := imageDoor(t, script{Entries: answers(t, entries)})
	image.Reference = unknownImage

	report, err := (&engine.Engine{Door: image}).Run(t.Context(), entries)
	if err != nil {
		t.Fatalf("the run could not be made: %v", err)
	}

	if !report.Failed() {
		t.Fatalf("a run against an image that could not be run came back as one that passed:\n%v", report)
	}

	// Both halves: the door's reading of the status, and the runtime's own
	// words, which are what settle whether the reading was right.
	for _, want := range []string{"could not run the image", unknownImage, "Unable to find image"} {
		if !strings.Contains(report.String(), want) {
			t.Errorf("the report does not say %q, so the failure reads as the adapter's:\n%v", want, report)
		}
	}
}

// TestACallersOwnDeadlineIsNotTheDoorsBound keeps two ways of running out of
// time apart. The door's wall clock and a deadline on the context it was handed
// both end the same container, and a door that reported the second as the first
// would name a bound that never elapsed — sending whoever reads it to raise a
// flag that was not the one holding the run.
func TestACallersOwnDeadlineIsNotTheDoorsBound(t *testing.T) {
	image, _, record := imageDoor(t, script{})
	image.Reference = hangImage
	image.Timeout = time.Hour

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	process, err := image.Open(ctx)
	if err != nil {
		t.Fatalf("the door would not open: %v", err)
	}

	err = process.Wait()
	if err == nil {
		t.Fatal("a container the caller gave up on came back as one that ended well")
	}

	if strings.Contains(err.Error(), "wall-clock") {
		t.Errorf("the caller's own deadline is reported as the door's bound: %v", err)
	}

	// The container is still taken away: whose clock ran out changes what is
	// said about it and not what becomes of it.
	assertRemoved(t, record)
}

// TestAnImageDoorThatCannotOpen keeps the ways it cannot apart, and all of them
// away from a run that reports nothing: a door that cannot start a process at
// all costs the run, and says which of them it was.
//
// The reference beginning with a dash is the one that is not merely a
// diagnostic. The vector is `run <the door's flags> <reference> <the
// container's args>`, so a reference the runtime reads as another option to
// itself starts something else — with whatever came next as its image, and
// without the isolation the report would then describe.
func TestAnImageDoorThatCannotOpen(t *testing.T) {
	testCases := []struct {
		name string
		door *engine.Image
		says string
	}{
		{
			name: "no image",
			door: &engine.Image{Runtime: os.Args[0]},
			says: "no image",
		},
		{
			name: "a runtime that is not installed",
			door: &engine.Image{Reference: fakeImage, Runtime: "not-a-container-runtime-anybody-has"},
			says: "not-a-container-runtime-anybody-has",
		},
		{
			name: "an image reference that is a flag",
			door: &engine.Image{Reference: "--network=host", Runtime: os.Args[0]},
			says: "begins with a dash",
		},
		{
			name: "an image reference that is a mount",
			door: &engine.Image{Reference: "-v/:/host:rw", Runtime: os.Args[0]},
			says: "begins with a dash",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := testCase.door.Open(t.Context())
			if err == nil {
				t.Fatal("the door opened onto nothing")
			}

			if !strings.Contains(err.Error(), testCase.says) {
				t.Errorf("the failure does not say what was wrong with the door: %v", err)
			}
		})
	}
}

// imageDoor is the image door pointed at the stub runtime, with a fake adapter
// scripted as the container's own argument vector.
func imageDoor(t *testing.T, told script) (*engine.Image, string, string) {
	t.Helper()

	path, transcript := scripted(t, told)
	record := filepath.Join(t.TempDir(), "runtime.jsonl")

	return &engine.Image{
		Reference: fakeImage,
		Args:      []string{path},
		Runtime:   os.Args[0],
		Env:       append(os.Environ(), runtimeEnv+"="+record),
	}, transcript, record
}

// assertRemoved is the container being taken away, named as the one that was
// started: a removal that named something else would leave the container it was
// supposed to remove running.
func assertRemoved(t *testing.T, record string) {
	t.Helper()

	run := invocation(t, record, "run")

	var name string

	for _, arg := range run {
		if strings.HasPrefix(arg, "--name=") {
			name = strings.TrimPrefix(arg, "--name=")
		}
	}

	if name == "" {
		t.Fatalf("the container was started without a name, so nothing can take it away: %v", run)
	}

	removed := invocation(t, record, "rm")

	if !slices.Contains(removed, "--force") || !slices.Contains(removed, name) {
		t.Errorf("the container %s was not forced away: %v", name, removed)
	}
}

// invocation is the first recorded call to the stub runtime whose first
// argument is want, waited for rather than read once.
//
// Waited for because the record is written by another process: a test that
// killed a container the moment the door opened would otherwise race the stub's
// own first line, and fail as though the door had never started anything.
func invocation(t *testing.T, record, want string) []string {
	t.Helper()

	var last []byte

	for range 1000 {
		b, err := os.ReadFile(record)
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("the stub runtime's record could not be read: %v", err)
		}

		last = b

		for line := range strings.Lines(string(b)) {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			var args []string
			if err := json.Unmarshal([]byte(line), &args); err != nil {
				t.Fatalf("the stub runtime recorded something that is not an argument vector: %q", line)
			}

			if len(args) > 0 && args[0] == want {
				return args
			}
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("the runtime was never asked to %s:\n%s", want, last)

	return nil
}

// runAsRuntime is the stub container runtime: it records what it was asked to
// do, and then does the smallest thing that keeps the conversation real.
//
// `run` starts the adapter in this process, over the standard input and
// standard output it was handed, which is what a runtime does from the engine's
// side. `rm` records and succeeds, because the container it would remove is a
// process this stub never had.
func runAsRuntime(record string) int {
	args := os.Args[1:]

	if err := recordInvocation(record, args); err != nil {
		fmt.Fprintf(os.Stderr, "the stub runtime could not record what it was asked: %v\n", err)

		return 2
	}

	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "the stub runtime was asked for nothing")

		return 2
	}

	switch args[0] {
	case "rm":
		return 0
	case "run":
	default:
		fmt.Fprintf(os.Stderr, "the stub runtime was asked to %q\n", args[0])

		return 2
	}

	rest := args[1:]
	for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
		rest = rest[1:]
	}

	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "the stub runtime was given no image to run")

		return 2
	}

	if rest[0] == unknownImage {
		// What a runtime does when it cannot get the image: it says so on its
		// own standard error and exits 125, having created nothing.
		fmt.Fprintf(os.Stderr, "Unable to find image %q locally: no such image\n", rest[0])

		return unknownStatus
	}

	if rest[0] == hangImage {
		// A container that never ends, which is what the wall clock is for.
		// Slept rather than blocked forever on a channel, which the runtime
		// would report as a deadlock and turn into a panic rather than a hang.
		time.Sleep(time.Hour)

		return 0
	}

	if len(rest) < 2 {
		fmt.Fprintln(os.Stderr, "the stub runtime was given no script to run as the adapter")

		return 2
	}

	return adapt(rest[1])
}

// recordInvocation appends one argument vector to the record, as JSON.
//
// Appended rather than written, because the door starts a fresh process to take
// a container away and a run may start more than one container: what a test
// reads is every call the door made, in the order it made them.
func recordInvocation(record string, args []string) error {
	b, err := json.Marshal(args)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(record, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}

	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()

		return err
	}

	// Closed rather than deferred and dropped: the test on the other side of
	// this file reads it from another process, and a line still sitting in a
	// buffer is a door that appears not to have started anything.
	return f.Close()
}
