// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/Zaba505/cpybkc/internal/conformance"
)

// digest writes the corpus's own SHA-256, and fails if a digest published
// beside the corpus disagrees with it.
//
// Standard output carries the digest and a newline and nothing else, so that
// the check somebody would write by hand is a comparison rather than a parse:
//
//	test "$(cpybkc-conform digest)" = "$(cat corpus.sha256)"
//
// Everything a reader wants beside it goes to standard error, for that reason.
//
// It exists as a command of its own so that the digest can be recomputed
// without trusting this program's silence. `check` verifies it and says nothing
// when it agrees, which is the right shape for a run and the wrong shape for
// somebody deciding whether to trust a download at all.
func digest(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("digest", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	corpus := flags.String("corpus", defaultCorpus, "the unpacked corpus")

	if err := flags.Parse(args); err != nil {
		// An answer rather than a failed run, for the reason check gives.
		if errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprint(stdout, usage)

			return nil
		}

		return fmt.Errorf("%w\n\n%s", err, usage)
	}

	if rest := flags.Args(); len(rest) > 0 {
		return fmt.Errorf("unexpected arguments %v\n\n%s", rest, usage)
	}

	computed, checked, err := conformance.CheckDigest(*corpus)

	// The digest is written before the error is reported, and that is the whole
	// point of the command: somebody holding a corpus that does not match its
	// published digest wants to see both numbers, and a program that failed
	// without printing what it computed would have told them only that
	// something is wrong.
	if computed != "" {
		_, _ = fmt.Fprintln(stdout, computed)
	}

	if err != nil {
		return err
	}

	if checked {
		_, _ = fmt.Fprintf(stderr, "%s agrees\n", conformance.DigestPath(*corpus))
	} else {
		_, _ = fmt.Fprintf(stderr, "there is no %s beside the corpus, so this digest was compared against nothing\n",
			conformance.DigestPath(*corpus))
	}

	return nil
}
