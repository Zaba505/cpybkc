// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Command adapter is the conformance adapter for a descriptive generator: a
// process the conformance engine starts and speaks docs/adapter/SPEC.md to,
// which declares at the handshake that the generator behind it is not a
// conformance subject and is asked nothing else (#201).
//
//	adapter --name graph
//
// It is one command for the whole category rather than one per generator.
// cmd/cpybkc-gen-graph is the generator that motivated it, and gen-docs,
// gen-sql, gen-avro, gen-json-schema and gen-copybook are all plausibly in it —
// and every one of them holds the same four-frame conversation, because a
// descriptive adapter is asked nothing that could differ between them.
//
// `--name` and `--version` are the two halves of what a report calls this
// adapter, and they are the door's to supply: the argument vector, the working
// directory and the environment are deliberately out of the specification's
// scope, which is why they are flags here and why nothing in the conversation
// carries them. A published image knows which release of the generator it
// carries (#203), and a run against a working tree has no version string to
// give — so the corpus's own runs leave it empty, which the contract makes
// optional exactly for this case.
//
// No generator is invoked. A descriptive adapter is never asked anything a
// generator could answer, so starting one would be starting it to throw its
// output away — and the point is that the framework can decline a subject it
// cannot test without pretending to have tested it.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Zaba505/cpybkc/internal/conformance/descriptive"
)

func main() {
	name := flag.String("name", "", "the generator's name, as a report should spell it: the <name> of a cpybkc-gen-<name>")
	version := flag.String("version", "", "what to tell the engine this adapter's generator is, for the report")

	flag.Parse()

	// The frames go to the standard output this process started with, and this
	// is the first statement of the program so that nothing has had the chance
	// to print before it runs.
	//
	// docs/adapter/SPEC.md gives the recipe in POSIX's terms — dup the
	// descriptor, then point standard output at standard error — and says
	// plainly that it is a recipe rather than the requirement: what is required
	// is the property that nothing but frames reaches the stream.
	//
	// What this buys, exactly: every write that resolves os.Stdout at the moment
	// it writes — fmt.Print and everything built on it — lands on standard
	// error from here on, where it is a diagnostic instead of a corruption. What
	// it does not buy is a writer captured during package initialisation, which
	// runs before main (`var log = log.New(os.Stdout, …)`), or a write to file
	// descriptor 1 that never goes through the variable at all. Those need the
	// dup, which needs a syscall this repository does not otherwise depend on
	// and a fallback for every platform without one.
	//
	// It is enough here because this program's whole import graph is the
	// standard library and one package of this repository's, and neither does
	// either of those things. An adapter that grew a dependency would be one
	// this reasoning no longer covers, which is why it is written down rather
	// than assumed.
	frames := os.Stdout
	os.Stdout = os.Stderr

	adapter := &descriptive.Adapter{Name: *name, Version: *version}

	if err := adapter.Serve(os.Stdin, frames); err != nil {
		// A non-zero exit means the adapter broke, and it means nothing else. A
		// request this adapter had no business being sent is not that: it goes
		// back refused, inside a frame, and the conversation carries on.
		fmt.Fprintln(os.Stderr, err)

		os.Exit(1)
	}
}
