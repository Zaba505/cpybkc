// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/conformance"
)

// TestCheckRunsTheCorpusThroughAnAdapter is the whole program, over the corpus
// this repository publishes, against a real process speaking the real contract.
//
// The descriptive adapter is the one to drive it with here. A conversation with
// it is four frames long and invokes no generator and no compiler, so this test
// exercises the command — the flags, the corpus, the door and the exit path —
// rather than re-running the conformance suite, which
// internal/conformance/goadapter already owns.
func TestCheckRunsTheCorpusThroughAnAdapter(t *testing.T) {
	root := repoRoot(t)
	adapter := build(t, root, "./internal/conformance/descriptive/cmd/adapter")

	stdout, stderr, err := drive(t, "check",
		"--corpus", conformance.CorpusPath(root),
		"--exec", adapter,
		"--", "--name", "graph")
	if err != nil {
		t.Fatalf("check: %v\n%s", err, stderr)
	}

	// The door's own sentence, quoted rather than summarised: a report that did
	// not carry it would be a result presented as though it had guarantees this
	// door does not provide.
	if !strings.Contains(stdout, "no network isolation") {
		t.Errorf("the report does not say what the door provides:\n%s", stdout)
	}

	if !strings.Contains(stdout, "not applicable") {
		t.Errorf("a descriptive generator's run is not reported as not applicable:\n%s", stdout)
	}

	if !strings.Contains(stderr, "sha256 ") {
		t.Errorf("nothing said which corpus the run was against:\n%s", stderr)
	}
}

// TestCheckFailsWhenTheRunDoes pins the exit path apart from the report. A run
// that went badly has to come back as errFailed and not as an ordinary error,
// because main turns the first into exit 1 — a fact about the generator — and
// the second into exit 2, which says the run never happened.
func TestCheckFailsWhenTheRunDoes(t *testing.T) {
	root := repoRoot(t)
	adapter := build(t, root, "./internal/conformance/descriptive/cmd/adapter")

	// An argument the adapter refuses, so it exits before it says hello. That is
	// a run that happened and failed rather than one that could not be started:
	// the door opened.
	_, _, err := drive(t, "check",
		"--corpus", conformance.CorpusPath(root),
		"--exec", adapter,
		"--", "--not-a-flag-this-adapter-has")

	if !errors.Is(err, errFailed) {
		t.Fatalf("a failed run came back as %v, and not as errFailed", err)
	}
}

// TestCheckRefusesACorpusThatIsNotThePublishedOne is the digest doing its job,
// and the diagnostic matters as much as the refusal: somebody holding a corpus
// that does not match wants both numbers, so they can tell a half-finished
// download from an edit they made themselves.
func TestCheckRefusesACorpusThatIsNotThePublishedOne(t *testing.T) {
	dir := writeCorpus(t)

	digest, err := conformance.Digest(dir)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}

	const wrong = "0000000000000000000000000000000000000000000000000000000000000000"
	write(t, conformance.DigestPath(dir), conformance.FormatDigest(wrong))

	_, _, err = drive(t, "check", "--corpus", dir, "--exec", os.Args[0])
	if err == nil {
		t.Fatal("check ran against a corpus that does not match its published digest")
	}

	if errors.Is(err, errFailed) {
		t.Error("a corpus that does not match is reported as a failed run, and no run happened")
	}

	for _, want := range []string{digest, wrong} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %s:\n%v", want, err)
		}
	}
}

// TestCheckSaysWhenNothingCheckedTheCorpus covers the other half. A corpus with
// no digest beside it is the ordinary case in a checkout and an unusual one in a
// download, and a run that passed over it in silence would report a conformance
// result about a corpus nobody vouched for.
func TestCheckSaysWhenNothingCheckedTheCorpus(t *testing.T) {
	root := repoRoot(t)
	adapter := build(t, root, "./internal/conformance/descriptive/cmd/adapter")

	_, stderr, err := drive(t, "check",
		"--corpus", conformance.CorpusPath(root),
		"--exec", adapter,
		"--", "--name", "graph")
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	if !strings.Contains(stderr, "nothing checked") {
		t.Errorf("a run against a corpus with no digest beside it said nothing about it:\n%s", stderr)
	}
}

// TestDigestWritesOnlyTheDigest is the command's contract with a shell. Anything
// else on standard output turns a comparison into a parse, and the comparison is
// the whole reason this is a command rather than a line of the report.
func TestDigestWritesOnlyTheDigest(t *testing.T) {
	dir := writeCorpus(t)

	want, err := conformance.Digest(dir)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}

	stdout, stderr, err := drive(t, "digest", "--corpus", dir)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}

	if stdout != want+"\n" {
		t.Errorf("digest wrote %q, want %q", stdout, want+"\n")
	}

	if !strings.Contains(stderr, "compared against nothing") {
		t.Errorf("digest did not say there was nothing to compare against:\n%s", stderr)
	}
}

// TestDigestReportsTheNumberItComputedEvenWhenItDisagrees is why the write comes
// before the error. Somebody whose download does not match wants to see what
// they have as well as what they should have had, and a program that failed
// without printing either has told them only that something is wrong.
func TestDigestReportsTheNumberItComputedEvenWhenItDisagrees(t *testing.T) {
	dir := writeCorpus(t)

	computed, err := conformance.Digest(dir)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}

	write(t, conformance.DigestPath(dir),
		conformance.FormatDigest("0000000000000000000000000000000000000000000000000000000000000000"))

	stdout, _, err := drive(t, "digest", "--corpus", dir)
	if err == nil {
		t.Fatal("digest passed a corpus that does not match its published digest")
	}

	if stdout != computed+"\n" {
		t.Errorf("digest wrote %q and not the number it computed, %q", stdout, computed+"\n")
	}
}

// TestHelpIsAnAnswer keeps the synopsis on standard output and the exit status
// at zero. A person who asked for it got what they asked for, and a program that
// answered on standard error would be one whose help cannot be piped.
func TestHelpIsAnAnswer(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			stdout, _, err := drive(t, arg)
			if err != nil {
				t.Fatalf("%s: %v", arg, err)
			}

			if !strings.Contains(stdout, "cpybkc-conform check") {
				t.Errorf("%s did not write the synopsis:\n%s", arg, stdout)
			}
		})
	}
}

// TestRunRejectsBadUsage keeps every way of asking for nothing a failure. The
// two that matter most are a missing --exec, which would otherwise start no
// process and report an empty run, and a name where a path belongs — which the
// door refuses to look up on PATH, and so must this.
func TestRunRejectsBadUsage(t *testing.T) {
	dir := t.TempDir()

	testCases := []struct {
		name string
		args []string
	}{
		{name: "no command", args: nil},
		{name: "a command this program does not have", args: []string{"conform"}},
		{name: "no adapter", args: []string{"check", "--corpus", dir}},
		{name: "an adapter that is not there", args: []string{"check", "--exec", filepath.Join(dir, "nowhere")}},
		{name: "an adapter that is a directory", args: []string{"check", "--exec", dir}},
		{name: "a flag this program does not have", args: []string{"check", "--image", "example.com/x"}},
		{name: "a deadline of zero", args: []string{"check", "--exec", os.Args[0], "--deadline", "0"}},
		{name: "a negative build deadline", args: []string{"check", "--exec", os.Args[0], "--build-deadline", "-1s"}},
		{name: "an operand digest does not take", args: []string{"digest", "extra"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, _, err := drive(t, testCase.args...); err == nil {
				t.Error("run accepted arguments it should have refused")
			}
		})
	}
}

// drive runs the program with args and returns what it wrote to each stream.
func drive(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	var stdout, stderr bytes.Buffer

	err := run(t.Context(), args, &stdout, &stderr)

	return stdout.String(), stderr.String(), err
}

// build compiles pkg from root into a fresh directory and returns the path to
// it, which is what --exec is given.
//
// Built rather than found, for the reason [engine.Command.Path] refuses a name:
// a run is against the tree under test, and resolving a name would find
// whichever adapter happened to be installed on the machine running the tests.
func build(t *testing.T, root, pkg string) string {
	t.Helper()

	name := filepath.Base(pkg)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	path := filepath.Join(t.TempDir(), name)

	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", path, pkg)
	cmd.Dir = root

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", pkg, err, out)
	}

	return path
}

// writeCorpus writes a corpus to a fresh directory and returns its path.
//
// It is deliberately not a loadable one. Every case that uses it is refused
// before the corpus is loaded — the digest is checked first, which is what makes
// a half-unpacked download a refusal rather than a generator that appears to
// disagree with entries nobody published.
func writeCorpus(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), conformance.PublishedCorpusDir)

	write(t, filepath.Join(dir, "orders-fixed", "entry.json"), []byte(`{"description":"x"}`))
	write(t, filepath.Join(dir, "orders-fixed", "input.bin"), []byte{0x00, 0xff})

	return dir
}

// write writes b to path, creating the directories above it.
func write(t *testing.T, path string, b []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// repoRoot walks up from the test's working directory to the directory holding
// go.mod, which for this module is the repository root and so the directory
// testdata/ sits in.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found above %q", dir)
		}

		dir = parent
	}
}
