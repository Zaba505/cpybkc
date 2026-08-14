// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Zaba505/cpybkc/internal/conformance"
	"github.com/Zaba505/cpybkc/internal/conformance/engine"
)

// check runs the corpus against one adapter.
//
// The order is deliberate and is what makes a failure attributable: the corpus
// is checked against its digest first, then loaded, and only then is a process
// started. A run against a corpus that was half unpacked would otherwise report
// a generator disagreeing with entries nobody published.
func check(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)

	// Discarded, because flag's own error output writes a synopsis of its own to
	// standard error the moment a flag is wrong, and this program has one
	// synopsis. What a caller gets is the parse error and the same usage every
	// other mistake produces.
	flags.SetOutput(io.Discard)

	var (
		corpus        = flags.String("corpus", defaultCorpus, "the unpacked corpus")
		exec          = flags.String("exec", "", "the adapter executable, as a path")
		dir           = flags.String("dir", "", "the adapter's working directory")
		deadline      = flags.Duration("deadline", engine.DefaultDeadline, "bounds one operation")
		buildDeadline = flags.Duration("build-deadline", engine.DefaultBuildDeadline, "bounds generate")
		grace         = flags.Duration("grace", engine.DefaultGrace, "how long the adapter is given to exit")
	)

	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w\n\n%s", err, usage)
	}

	path, err := adapterPath(*exec)
	if err != nil {
		return err
	}

	if err := positive(*deadline, *buildDeadline, *grace); err != nil {
		return err
	}

	entries, err := readCorpus(*corpus, stderr)
	if err != nil {
		return err
	}

	report, err := (&engine.Engine{
		Door: &engine.Command{
			Path: path,
			// Everything the flags did not consume, which is the adapter's own
			// argument vector: docs/adapter/SPEC.md leaves it to the door
			// precisely so that an adapter can be a script taking arguments of
			// its own without the contract growing a place to put them.
			Args: flags.Args(),
			Dir:  *dir,
		},
		Deadline:      *deadline,
		BuildDeadline: *buildDeadline,
		Grace:         *grace,
	}).Run(ctx, entries)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprint(stdout, report)

	if report.Failed() {
		return errFailed
	}

	return nil
}

// readCorpus checks the corpus at dir against the digest published beside it,
// loads it, and says on stderr which corpus the run is about to be against.
//
// The digest line is on standard error rather than in the report because the
// report is the engine's and describes a conversation. Which corpus was asked
// is a fact about this program's inputs, and it belongs beside the report
// rather than inside a structure that will one day be serialised as one.
func readCorpus(dir string, stderr io.Writer) ([]*conformance.Entry, error) {
	digest, checked, err := conformance.CheckDigest(dir)
	if err != nil {
		return nil, err
	}

	entries, err := conformance.Load(dir)
	if err != nil {
		return nil, err
	}

	_, _ = fmt.Fprintf(stderr, "corpus: %s, %d entries, sha256 %s\n", dir, len(entries), digest)

	if !checked {
		// Said out loud rather than passed over. A corpus with no digest beside
		// it is the ordinary case in a checkout and an unusual one in a
		// download, and a run that silently accepted an edited corpus would
		// report a conformance result about a question nobody asked.
		_, _ = fmt.Fprintf(stderr, "there is no %s beside it, so nothing checked that this is the published corpus\n",
			conformance.DigestPath(dir))
	}

	return entries, nil
}

// adapterPath is the executable --exec named, checked before a run starts.
//
// A name is refused rather than looked up on PATH, which is the rule
// [engine.Command.Path] states and the reason it gives: a run is usually
// against something just built from the tree under test, and resolving a name
// would find whichever adapter the author happened to have installed. Refusing
// is the honest half of that — silently reading it as a relative path would
// make `--exec adapter` mean something different from what a shell means by it.
func adapterPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("--exec is required: name the adapter executable to run\n\n%s", usage)
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("failed to find the adapter %s: %w", path, err)
	}

	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory: --exec names the adapter executable itself", path)
	}

	return path, nil
}

// positive refuses a bound that is not one. A zero duration is the engine's
// "use the default", so a caller who wrote --deadline 0 meaning "no limit" would
// silently get a minute; a negative one is a deadline already past, which turns
// every operation into a timeout and reads as an adapter that answers nothing.
func positive(bounds ...time.Duration) error {
	for _, bound := range bounds {
		if bound <= 0 {
			return fmt.Errorf("a bound of %s is not one: every deadline must be positive, and the defaults are "+
				"what a run with no flags uses", bound)
		}
	}

	return nil
}
