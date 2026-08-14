// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/conformance"
)

// TestCheckRunsTheCorpusThroughAnImage is the image door end to end, from the
// flag to the report, with a stub in the place of the container runtime.
//
// A stub because a test that needed a working container runtime would be a test
// that does not run: the pipeline this repository's verify command drives is
// itself a container. What it covers is the wiring — that --image reaches the
// image door, that the adapter's own arguments reach the container, and that
// the report quotes the door that produced it rather than the one this program
// happens to have run more often.
func TestCheckRunsTheCorpusThroughAnImage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stub runtime is a shell script")
	}

	root := repoRoot(t)
	adapter := build(t, root, "./internal/conformance/descriptive/cmd/adapter")

	stdout, stderr, err := drive(t, "check",
		"--corpus", conformance.CorpusPath(root),
		"--image", adapter,
		"--runtime", stubRuntime(t),
		"--", "--name", "graph")
	if err != nil {
		t.Fatalf("check: %v\n%s", err, stderr)
	}

	// The door's own sentence, quoted rather than summarised, and it is a
	// different sentence from --exec's: which door a result came through is the
	// whole of what it is worth to somebody who did not produce it.
	for _, want := range []string{"no network", "read-only root", adapter} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the report does not say %q about the door it came through:\n%s", want, stdout)
		}
	}

	if strings.Contains(stdout, "no network isolation") {
		t.Errorf("a containerised run is reported as though it had no isolation:\n%s", stdout)
	}

	if !strings.Contains(stdout, "not applicable") {
		t.Errorf("a descriptive generator's run is not reported as not applicable:\n%s", stdout)
	}
}

// TestARunGoesThroughOneDoor is every way of naming none, both, or one door
// with the other's flags.
//
// The two that matter most are opposite failures. Naming both doors would have
// to pick one silently, and the two provide very different things. Naming an
// image flag beside --exec is a caller who has said what they wanted the run to
// guarantee, on the door that guarantees nothing: accepted quietly, it would
// hand them a report saying the opposite of what they asked for, in a sentence
// they had no reason to re-read.
func TestARunGoesThroughOneDoor(t *testing.T) {
	dir := t.TempDir()

	testCases := []struct {
		name string
		args []string
		says string
	}{
		{
			name: "neither door",
			args: []string{"check", "--corpus", dir},
			says: "--exec or --image is required",
		},
		{
			name: "both doors",
			args: []string{"check", "--corpus", dir, "--exec", os.Args[0], "--image", "example.com/adapter"},
			says: "two doors",
		},
		{
			name: "the command door's working directory on the image door",
			args: []string{"check", "--corpus", dir, "--image", "example.com/adapter", "--dir", dir},
			says: "--dir is the command door's",
		},
		{
			name: "the image door's runtime on the command door",
			args: []string{"check", "--corpus", dir, "--exec", os.Args[0], "--runtime", "podman"},
			says: "--runtime is the image door's",
		},
		{
			name: "the image door's wall clock on the command door",
			args: []string{"check", "--corpus", dir, "--exec", os.Args[0], "--image-deadline", "1h"},
			says: "--image-deadline is the image door's",
		},
		{
			name: "the image door's memory cap on the command door",
			args: []string{"check", "--corpus", dir, "--exec", os.Args[0], "--image-memory", "1g"},
			says: "--image-memory is the image door's",
		},
		{
			name: "the image door's process cap on the command door",
			args: []string{"check", "--corpus", dir, "--exec", os.Args[0], "--image-processes", "8"},
			says: "--image-processes is the image door's",
		},
		{
			name: "the image door's scratch on the command door",
			args: []string{"check", "--corpus", dir, "--exec", os.Args[0], "--image-scratch", "1g"},
			says: "--image-scratch is the image door's",
		},
		{
			name: "a process cap that is not one",
			args: []string{"check", "--corpus", dir, "--image", "example.com/adapter", "--image-processes", "0"},
			says: "is not a cap",
		},
		{
			name: "a size that is not one",
			args: []string{"check", "--corpus", dir, "--image", "example.com/adapter", "--image-memory", ""},
			says: "not a size",
		},
		{
			name: "a wall clock that is not positive",
			args: []string{"check", "--corpus", dir, "--image", "example.com/adapter", "--image-deadline", "0"},
			says: "is not one",
		},
		{
			name: "a runtime that is not named",
			args: []string{"check", "--corpus", dir, "--image", "example.com/adapter", "--runtime", ""},
			says: "--runtime names the container runtime",
		},
		{
			name: "a size in a notation no runtime has",
			args: []string{"check", "--corpus", dir, "--image", "example.com/adapter", "--image-memory", "bananas"},
			says: "is not a size",
		},
		{
			name: "a scratch size in a notation no runtime has",
			args: []string{"check", "--corpus", dir, "--image", "example.com/adapter", "--image-scratch", "1 gig"},
			says: "is not a size",
		},
		{
			// The reference goes where the runtime reads its own options, so a
			// reference that is a flag starts something else — with whatever
			// followed it as the image, and none of the isolation the report
			// would then describe. Refused here as a usage error and again in
			// the door, which is the half that covers a caller who is not this
			// program.
			name: "an image reference the runtime would read as a flag",
			args: []string{"check", "--corpus", dir, "--image", "--network=host"},
			says: "begins with a dash",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, _, err := drive(t, testCase.args...)
			if err == nil {
				t.Fatal("check accepted arguments that name no one door")
			}

			if !strings.Contains(err.Error(), testCase.says) {
				t.Errorf("the refusal does not say %q:\n%v", testCase.says, err)
			}
		})
	}
}

// TestTheWallClockMustOutliveTheEnginesBounds is the pair of bounds whose wrong
// value is silent. The wall clock takes the whole container away, while the
// engine's deadlines bound one operation each: a container ended in the middle
// of a build the run allowed, or of an entry it allowed, faults for a reason
// that is the door's and reads as one that is the generator's.
func TestTheWallClockMustOutliveTheEnginesBounds(t *testing.T) {
	dir := t.TempDir()

	testCases := []struct {
		name string
		args []string
		says string
	}{
		{
			name: "shorter than the build deadline",
			args: []string{"--build-deadline", "10m", "--image-deadline", "5m"},
			says: "--build-deadline",
		},
		{
			name: "shorter than the per-operation deadline",
			args: []string{"--build-deadline", "1m", "--deadline", "30m", "--image-deadline", "5m"},
			says: "--deadline",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			args := append([]string{"check", "--corpus", dir, "--image", "example.com/adapter"}, testCase.args...)

			_, _, err := drive(t, args...)
			if err == nil {
				t.Fatal("check accepted a wall clock that ends work the same run allows")
			}

			if !strings.Contains(err.Error(), testCase.says) {
				t.Errorf("the refusal does not name the bound it conflicts with:\n%v", err)
			}
		})
	}
}

// stubRuntime writes a container runtime that is not one, onto a PATH of this
// test's own, and returns the name --runtime is given.
//
// It does what the door's argument vector says it should: skips `run` and every
// flag joined to its value, takes the next argument as the image, and runs it
// with what follows. That the vector can be read that way is the door's
// property and is asserted on its own in the engine's tests; here it is what
// lets a real adapter answer through the flag.
func stubRuntime(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "stub-runtime")

	write(t, path, []byte(`#!/bin/sh
[ "$1" = "run" ] || { echo "the stub runtime was asked to $1" >&2; exit 2; }
shift
while [ $# -gt 0 ]; do
	case "$1" in
	-*) shift ;;
	*) break ;;
	esac
done
[ $# -gt 0 ] || { echo "the stub runtime was given no image" >&2; exit 2; }
image=$1
shift
exec "$image" "$@"
`))

	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	return filepath.Base(path)
}
