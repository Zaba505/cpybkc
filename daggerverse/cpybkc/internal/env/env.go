// Package env refuses the environment variable names that could not be
// environment variables at all, and refuses nothing else.
//
// It is a package of its own, rather than one function in package main, for the
// reason internal/generator and internal/imageref are: the module's own package
// main imports the generated Dagger client, whose init panics when no Dagger
// session is present, so a test beside main cannot run under plain `go test`
// and the rule below would be asserted by a comment instead of pinned by a
// test. Nothing here imports Dagger, so `go test ./internal/...` runs anywhere.
//
// What is deliberately not here is any opinion about *which* variables a caller
// may set. cpybkc passes its whole environment through to a generator unchanged
// and names no variable of its own (docs/plugin/SPEC.md, "The environment"), so
// a module mirroring the CLI (#253) has nothing to say about the difference
// between SOURCE_DATE_EPOCH and anything else — and a module that did would be
// making the contract smaller from the outside, which is the one thing the
// mirroring stance rules out. The whole of this package is the two shapes that
// are not a name in any environment.
package env

import (
	"errors"
	"fmt"
	"strings"
)

// CheckName refuses a name that cannot be an environment variable: the empty
// one, and one carrying `=` or a NUL byte.
//
// Both refusals are about the representation rather than about taste. An
// environment is a list of `NAME=VALUE` strings terminated by NUL, so a name
// holding either character does not encode: `A=B` set to `c` is indistinguishable
// from `A` set to `B=c`, and everything from a NUL onwards is not carried at
// all. The failure from letting one through is silent rather than loud — the
// generator sees a variable it was not told about, or does not see the one it
// was — and the value would have travelled a long way from the argument that
// named it.
//
// Those two and no more, which is less than a reader might expect. POSIX
// reserves the names made of upper-case letters, digits and underscore for
// utilities and says a portable *application*'s own variables should keep to
// that shape, and this refuses neither a lowercase name nor one starting with a
// digit. Both of those are variables the shell can export, cpybkc can be started
// with and a generator can read, so a module refusing them would refuse a run
// the CLI performs — and what a plugin may be handed is docs/plugin/SPEC.md's to
// narrow, not this module's. It is the same boundary
// [github.com/Zaba505/cpybkc/daggerverse/cpybkc/internal/generator.CheckName]
// draws between that document's **MUST**s and its **SHOULD**s.
func CheckName(name string) error {
	switch {
	case name == "":
		return errors.New(
			"an environment variable name is required: it is the NAME in the NAME=VALUE an environment is made of, " +
				"and there is no run in which the empty one reaches a generator")
	case strings.Contains(name, "="):
		return fmt.Errorf(
			"environment variable name %q contains a =: an environment is a list of NAME=VALUE strings split at the "+
				"first =, so this name and this value cannot be told apart from a shorter name and a longer value",
			name)
	case strings.ContainsRune(name, 0):
		return fmt.Errorf(
			"environment variable name %q contains a NUL byte: an environment string ends at the first one, so "+
				"everything from it onwards would not reach the generator at all",
			name)
	}

	return nil
}
