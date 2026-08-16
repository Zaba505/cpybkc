package coverage

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

// Record is this repository's account of how the companion Dagger module answers
// every flag the cpybkc CLI accepts: the flags mapped onto a function by name,
// and the flags that reach the module through the fallback instead.
//
// It is here rather than beside the pipeline's package main for the reason the
// package comment gives, and the reason is not a formality. The rules below are
// the whole of what holds the module to its stance, and the first version of
// them lived where no test could reach — which is exactly where a rule that is
// asserted in three comments and enforced in none goes unnoticed. That is not
// hypothetical: it is what review found in the first draft of this change, and
// [Record.Check]'s refusal of a mapping onto Fallback is the rule that was
// missing.
type Record struct {
	// Module names the module being held to this record, for diagnostics. A
	// message that says which module's surface is wrong is one a contributor can
	// act on without opening the pipeline.
	Module string

	// Fallback is the module's escape-hatch function — the one that takes an
	// argument vector verbatim. It is the route every entry in Exceptions is an
	// exception *to*, and it is not a legal value in Mapped: mirroring the CLI
	// means a flag's answer is a function named for its command, and forwarding
	// it is what an exception says instead.
	Fallback string

	// Mapped is each flag against the function that carries it by name.
	Mapped map[string]string

	// Exceptions is each flag that reaches the module through Fallback, against
	// the argument for it being there.
	Exceptions map[string]Exception
}

// Check holds the record against the flags the CLI actually accepts and the
// functions the module actually declares.
//
// It fails in six directions, and every one of them was a way the module and the
// CLI could drift apart while this looked healthy:
//
//   - a flag neither table answers, which is the CLI having grown one;
//   - a flag mapped onto [Record.Fallback], which is the stance being abandoned
//     one flag at a time, in a diff that reads like any other mapping;
//   - a flag in both tables, which does not say which answer is the module's;
//   - a mapping onto a function the module does not declare;
//   - an exception that claims neither settled nor tracked, or gives no reason;
//   - either table naming a flag the CLI no longer accepts.
//
// Every fault is reported rather than the first, and the flags are walked in a
// fixed order, because two runs over one tree should say the same thing in the
// same order — a map's iteration order is whatever the runtime felt like, and a
// diagnostic that reshuffles is one a contributor cannot diff against the last.
//
// What none of this establishes is that an answer is *right*. Nothing here proves
// the named function can reach the flag, and since the fallback forwards an
// arbitrary vector, every flag is reachable through it by construction. What the
// rules buy is that somebody had to say which answer they meant, in a form that
// distinguishes a decision from a gap — and had to say it again the next time the
// CLI's surface moved.
func (r Record) Check(flags, functions []string) error {
	var errs []error

	// Where each flag is recorded, and as what: the reverse direction's message
	// as well as this loop's bookkeeping.
	recorded := map[string]string{}
	maps.Copy(recorded, r.Mapped)

	for _, flag := range slices.Sorted(maps.Keys(r.Exceptions)) {
		if function, both := r.Mapped[flag]; both {
			errs = append(errs, fmt.Errorf(
				"%s is recorded both as mapped onto %s's %s and as an exception reaching it through %s: a flag has "+
					"one answer, and the two tables are how a mapped flag and an excepted one are told apart, so a "+
					"flag in both says the module mirrors it and does not at once",
				flag, r.Module, function, r.Fallback))

			continue
		}

		recorded[flag] = r.Fallback
	}

	for _, flag := range flags {
		function, mapped := r.Mapped[flag]
		exception, excepted := r.Exceptions[flag]

		// Both are checked when a flag is in both tables, rather than one of them
		// standing in for the pair. The flag is already a failure by then, but the
		// contributor about to delete one of the two entries should learn now
		// whether the one they are keeping is sound.
		if mapped {
			switch {
			case function == r.Fallback:
				errs = append(errs, fmt.Errorf(
					"%s is mapped onto %s in the flag table, and %s is the escape hatch rather than a function that "+
						"carries a flag: %s mirrors the cpybkc CLI, so a flag's answer is an argument on the function "+
						"named for its command — record it there, or, if a Dagger argument cannot express it, as an "+
						"exception carrying the argument for that and the issue curating it if one is",
					flag, r.Fallback, r.Fallback, r.Module))

			case !slices.Contains(functions, function):
				errs = append(errs, fmt.Errorf(
					"%s is recorded as covered by %s's %s, and that module declares no such function; either the "+
						"function was renamed and this table was not, or the flag now reaches the module some other way",
					flag, r.Module, function))
			}
		}

		if excepted {
			if err := exception.Check(flag); err != nil {
				errs = append(errs, err)
			}
		}

		if !mapped && !excepted {
			errs = append(errs, fmt.Errorf(
				"the cpybkc CLI accepts %s and %s records nothing that answers it: %s is the cpybkc CLI daggerized, "+
					"so the answer is ordinarily an argument on the function named for the command this flag belongs "+
					"to; a flag a Dagger argument cannot express is an exception instead, carrying the argument for "+
					"that and the issue curating it if one is",
				flag, r.Module, r.Module))
		}
	}

	// The route every exception is an exception *to*. An entry saying a flag
	// reaches the module through a function the module no longer declares covers
	// nothing, and it would otherwise be the one claim in either table checked
	// against nothing.
	if len(r.Exceptions) > 0 && !slices.Contains(functions, r.Fallback) {
		errs = append(errs, fmt.Errorf(
			"flags are recorded as reaching %s through %s, and that module declares no such function: the escape "+
				"hatch is what every exception is an exception to, so a rename of it is a rewrite of the exceptions "+
				"rather than a rename",
			r.Module, r.Fallback))
	}

	for _, flag := range slices.Sorted(maps.Keys(recorded)) {
		if !slices.Contains(flags, flag) {
			errs = append(errs, fmt.Errorf(
				"%s records %s as answered by %s and the cpybkc CLI no longer accepts that flag; a module argument "+
					"is public API for as long as the published module ref exists, so a flag leaving the CLI is a "+
					"decision about the module rather than a line to delete from this table without one",
				r.Module, flag, recorded[flag]))
		}
	}

	return errors.Join(errs...)
}
