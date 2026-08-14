// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Command cpybkc-conform runs the conformance corpus against a generator's
// adapter and reports what came back.
//
//	cpybkc-conform check --exec <path> [--corpus <dir>] [-- args for the adapter...]
//	cpybkc-conform check --image <ref> [--corpus <dir>] [-- args for the adapter...]
//	cpybkc-conform digest [--corpus <dir>]
//
// It is [github.com/Zaba505/cpybkc/internal/conformance/engine] with a command
// line on it, and it holds no rule of its own: what an entry is and what its
// answer is written in are docs/conformance/SPEC.md's, how the adapter is asked
// is docs/adapter/SPEC.md's, and the comparison is the engine's.
//
// # Why this is a command at all
//
// Until this existed the only way to run the corpus was to clone this
// repository and run `go test ./internal/conformance/...`, which asks
// cpybkc-gen-go and nothing else. A generator author in another language had a
// corpus they could read and no program that would ask their generator about
// it.
//
// The natural next thought is a container image, and it fails a specific and
// common adopter: an engineer inside an organisation whose builders have no
// egress and whose images come from an internal mirror with an allowlist, where
// adding an external image is a ticket with a security review and a named
// internal owner. Conformance is the thing an outsider runs once, on a whim,
// before they are invested, which makes it the worst possible place to spend a
// procurement ticket (#202). So this command is built for several platforms and
// attached to every release beside the corpus, as cpybkc-conformance.tar.gz —
// a download and an `--exec`, with no registry, no daemon and no image.
//
// # Two doors, and what each one is worth
//
// So `--exec` is the door that needs nothing: a download and a path. It also
// provides nothing — no network namespace, no read-only root, no resource cap —
// and [engine.Command.Describe] says so in as many words, which the report
// quotes. A run through it is the author's own working result.
//
// `--image` is the other door, and is where the properties that make a result
// believable to somebody else live: no network, a read-only root, a memory and
// process cap, and a wall-clock bound on the container
// ([engine.Image.Describe], again quoted). Both doors drive one implementation
// of the conversation, because the contract begins after the process exists
// (docs/adapter/SPEC.md, "A process is the unit, and a container is a door onto
// it") — the image door is an argument vector and a container to take away
// afterwards, and nothing about the conversation changes (#203).
//
// Which door a run went through is therefore a property of the result, and the
// report records it. Neither door makes a run a conformance claim a third party
// should be asked to trust without qualification: a run computed on the
// claimant's machine, against a corpus they downloaded, by a program they are
// holding, is a self-report either way. That is a fine thing to publish, as
// long as it is labelled as one.
//
// # The exit status
//
// docs/conformance/SPEC.md leaves what exit code a failing comparison produces
// out of scope, deliberately, so this is the command's own contract rather than
// the format's:
//
//   - 0 — the run happened and nothing failed. A descriptive generator's run,
//     which the corpus has nothing to ask, is one of these.
//   - 1 — the run happened and something failed: an entry disagreed, or one
//     could not be asked at all.
//   - 2 — the run could not be attempted: the arguments were wrong, the corpus
//     could not be read, or it did not match the digest published beside it.
//
// One and two are separate because a script that treats them alike cannot tell
// a generator that failed the corpus from a corpus that never ran, and only the
// first is a fact about the generator.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/Zaba505/cpybkc/internal/conformance"
)

// defaultCorpus is where the corpus is looked for when nothing says otherwise.
//
// It is the directory cpybkc-conformance.tar.gz unpacks the corpus into, so
// that the invocation in the archive's own README is `--exec` and nothing else.
// Running against this repository's tree needs `--corpus testdata/conformance`,
// which is the right way round: the default serves the artifact this command is
// shipped in, and a checkout is the case where somebody already knows where
// they are.
const defaultCorpus = conformance.PublishedCorpusDir

// errFailed is what a run that happened and went badly comes back as.
//
// It is a sentinel rather than a message because the report has already said
// everything there is to say, on standard output, entry by entry. What is left
// for main is the exit status, and a second summary on standard error would be
// the same news in a place a reader is not looking.
var errFailed = errors.New("the run reported a failure")

func main() {
	// Interrupt cancels the run, which kills the adapter: the conversation's own
	// deadlines bound each operation, and this is what bounds the whole thing
	// against somebody who has seen enough. An adapter killed by the engine is
	// not in violation of anything (docs/adapter/SPEC.md, "Deadlines and
	// lifetime belong to the engine").
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	err := run(ctx, os.Args[1:], os.Stdout, os.Stderr)

	switch {
	case err == nil:
		return
	case errors.Is(err, errFailed):
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "cpybkc-conform: %v\n", err)
		os.Exit(2)
	}
}

// run is the whole program with the exit path taken out, so that main owns the
// exit status and nothing else, and so that a test can drive a whole run
// without ending the test binary.
//
// Both streams are parameters for the same reason. stdout carries the answer —
// the report, or the digest — and stderr carries what a reader wants beside it
// but a script piping the answer does not, and a test that had to move this
// process's own streams could not assert either.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("no command given\n\n%s", usage)
	}

	switch command := args[0]; command {
	case "check":
		return check(ctx, args[1:], stdout, stderr)
	case "digest":
		return digest(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(stdout, usage)

		return nil
	default:
		return fmt.Errorf("%q is not a command of this program\n\n%s", command, usage)
	}
}
