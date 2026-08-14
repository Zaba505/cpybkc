// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Command adapter is the conformance adapter for cpybkc-gen-go: a process the
// conformance engine starts and speaks docs/adapter/SPEC.md to, exactly as it
// speaks to an adapter somebody else wrote for a generator in another language.
//
//	adapter --root <repository> --generator <path to cpybkc-gen-go>
//
// Both are the door's business rather than the contract's — the argument vector,
// the working directory and the environment are deliberately out of the
// specification's scope — which is why they are flags here and why nothing in
// the conversation carries them.
//
// The generator is a path rather than a name on PATH because a run is nearly
// always against a generator just built from the tree under test, and resolving
// a name would find whichever one an author happened to have installed.
//
// `--version` is the other half of what a report calls this adapter, and it is
// the door's to supply for the same reason: a published image knows which
// release of the generator it carries (#203), and a run against a working tree
// has no version string to give — so the corpus's own runs leave it empty, which
// the contract makes optional exactly for this case.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/Zaba505/cpybkc/internal/conformance/goadapter"
)

func main() {
	root := flag.String("root", "", "the repository root: where the scratch tree is made and the go tool is run")
	generator := flag.String("generator", "", "the cpybkc-gen-go executable to drive")
	name := flag.String("name", "go", "the generator's name, as a diagnostic about the invocation should spell it")
	version := flag.String("version", "", "what to tell the engine this adapter's generator is, for the report")

	flag.Parse()

	// The frames go to the standard output this process started with, and
	// nothing else does.
	//
	// docs/adapter/SPEC.md gives the recipe in POSIX's terms — dup the
	// descriptor, then point standard output at standard error — and says
	// plainly that it is a recipe rather than the requirement: what is required
	// is the property that nothing but frames reaches the stream, which a
	// platform without those calls meets by whatever means its runtime offers.
	// Go's is os.Stdout, which every print in this process and in everything it
	// imports resolves at the moment it writes. Moving it here, before anything
	// else runs, is what makes a greeting from some dependency a diagnostic
	// instead of a corrupted frame — and an adapter that instead resolved to be
	// careful is one that is one dependency away from being wrong.
	frames := os.Stdout
	os.Stdout = os.Stderr

	// A killed adapter is not in violation of the contract and is under no
	// obligation to write anything on its way out. What this buys is the codec
	// program it started: cancelling the context is what stops one being left
	// behind when the engine gives up on this process.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	adapter := &goadapter.Adapter{
		Root:      *root,
		Name:      *name,
		Generator: *generator,
		Version:   *version,
	}

	if err := adapter.Serve(ctx, os.Stdin, frames); err != nil {
		// A non-zero exit means the adapter broke, and it means nothing else:
		// bytes that decoded wrongly, a reader that refused a file and a writer
		// that refused a record are answers, and every one of them has already
		// gone back inside a frame.
		fmt.Fprintln(os.Stderr, err)

		os.Exit(1)
	}
}
