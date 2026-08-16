// Package coverage is the shape of an exception to the companion module's
// stance, and the rules an exception has to satisfy to be one.
//
// daggerverse/cpybkc mirrors the cpybkc CLI: a flag's answer is an argument on a
// function named for the command it belongs to (#253). A flag that reaches the
// module through the escape hatch instead is an exception, and an exception is
// worth nothing unless it can be told apart from a flag nobody got to. That is
// the whole of what [Exception] is for — it makes the two different things to
// write, so that the difference survives into a diff a reviewer reads.
//
// It is a package of its own for the reason internal/surface is: the pipeline's
// own package main imports the generated Dagger client, whose init panics
// without a session, so a test beside it cannot run under plain `go test`. This
// package imports no Dagger, so the rules below are pinned by tests rather than
// asserted by a comment — which matters here for the same reason it matters
// there, because what is built on them is a drift guard and a drift guard with a
// hole fails by staying green.
//
// The files here are named for the type rather than for the package because
// .gitignore's `coverage.*` — Go's test-coverage profiles — would match a
// coverage.go and quietly leave it out of a commit. That is worth a sentence
// rather than a rediscovery: `git add -A` says nothing, and what lands is a
// package that does not compile.
package coverage

import (
	"errors"
	"fmt"
	"regexp"
)

// issueRef is how an exception names the story that will curate its flag: a
// GitHub issue reference in this repository, `#` and a number.
//
// The shape is checked rather than the issue's existence, for the reason
// internal/imageref validates the shape of an image reference and not what is
// behind it: a check that resolved references would need a network and a token
// to say whether somebody's exception was well formed, and would fail on a
// private fork of this repository rather than on anything a contributor did.
//
// A leading zero is refused because GitHub has no issue #0 and none whose number
// is written with one, so "#0" and "#012" are a typo rather than a reference —
// and a typo in the one field that says *this gap is tracked* is the field
// failing silently.
var issueRef = regexp.MustCompile(`^#[1-9][0-9]*$`)

// Exception is what the record says about one CLI flag the companion module
// answers through Run rather than through a function named for a command.
//
// Every field is load bearing and none of them is decoration. The zero value is
// not a valid exception: it claims nothing, which is exactly the state this type
// exists to make unwritable.
type Exception struct {
	// Reason is the argument for this flag not being curated, in prose, and it is
	// required.
	//
	// What it has to answer is why an argument on a named function is the wrong
	// shape for this flag — not that nobody has written one yet, which is what
	// Tracking says.
	Reason string

	// Settled says the module is not going to grow a function for this flag, and
	// that the answer it has is the answer.
	//
	// It is an explicit claim rather than something derived from an empty
	// Tracking, because the two are the whole distinction this type carries and
	// deriving one of them from the absence of the other would let it be made by
	// forgetting. Somebody writing an exception says which of the two they mean,
	// in a word, in the diff.
	Settled bool

	// Tracking is the issue where this flag's curated function is being written,
	// as `#<number>`, and it is what makes an uncurated flag a gap with an owner
	// rather than an omission.
	//
	// It is set when Settled is not, and the two are mutually exclusive: a gap
	// that is being closed is not settled, and a decision that is settled has no
	// story left to name.
	Tracking string
}

// Check reports whether e is a well-formed exception for flag.
//
// It says nothing about whether the exception is *right* — no rule here can, and
// pretending otherwise is what the flag table's own comment warns about
// everywhere else. What it enforces is that a claim was made: that somebody
// wrote down why this flag is not curated, and said whether that is a decision
// or a gap somebody is closing. A flag arriving on the escape hatch with neither
// is the drift the mirroring stance exists to end, and before this it was one
// word in a map.
//
// Every failure is reported rather than the first, because an exception written
// in a hurry is usually wrong in more than one way at once and a contributor
// should learn both in one run.
func (e Exception) Check(flag string) error {
	var errs []error

	if e.Reason == "" {
		errs = append(errs, fmt.Errorf(
			"%s is recorded as reaching the companion module through its escape hatch and no reason is written "+
				"beside it: the module mirrors the cpybkc CLI, so a flag with no function of its own is an exception "+
				"that carries the argument for itself (#253)", flag))
	}

	switch {
	case e.Settled && e.Tracking != "":
		errs = append(errs, fmt.Errorf(
			"%s is recorded as both settled and tracked by %s: settled says the module will not grow a function for "+
				"this flag, and a tracking issue says one is being written, so the two together do not say which "+
				"happens next", flag, e.Tracking))

	case !e.Settled && e.Tracking == "":
		errs = append(errs, fmt.Errorf(
			"%s is recorded as reaching the companion module through its escape hatch and is neither settled nor "+
				"tracked: say Settled when the escape hatch is this flag's answer and the argument for that is "+
				"written beside it, or name the issue curating it in Tracking, so that a decision and a gap nobody "+
				"got to stop looking the same (#253)", flag))

	case e.Tracking != "" && !issueRef.MatchString(e.Tracking):
		errs = append(errs, fmt.Errorf(
			"%s names %q as the issue curating it, which is not an issue reference: it is written as # and the "+
				"number, so that the story closing this gap can be found from here", flag, e.Tracking))
	}

	return errors.Join(errs...)
}
