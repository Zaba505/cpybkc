// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import "errors"

// The three statuses docs/cli/SPEC.md enumerates, and cpybkc exits with no
// other:
//
//	0  what was asked for was done
//	1  the run failed
//	2  the argument vector could not be understood
//
// A status per failing stage would look more informative and is not: it would
// make every new stage a compatibility question, it would have to answer which
// status a run with a manifest fault and a layout fault carries, and a script
// branches on zero against non-zero anyway. The one distinction worth encoding
// is whether cpybkc understood the request at all, because that is the failure
// a caller can fix without knowing anything about the project.
//
// The small integers are spoken for by parties this project does not control —
// a shell exits 126 and 127 for a file it cannot execute or find, and reports a
// signalled process as 128 plus the signal number — so a caller seeing one of
// those knows it came from their shell rather than from this program.
type status int

const (
	statusOK status = 0

	statusFailed status = 1

	statusUsage status = 2
)

// statusOf is the whole of how a failure becomes an exit status, and it is the
// only place in this program that decides one.
//
// docs/cli/SPEC.md requires every enumerated status to be reachable and this is
// where each is reached from: a run that did what was asked exits 0, a vector
// cpybkc could not understand exits 2, and every other fault — a manifest that
// is not there, a layout that does not resolve, a generator that failed, a
// cancelled run — is the run failing and exits 1. Scattering that decision
// across the stages that raise the faults is how a stage acquires a status of
// its own, which is the table growing a fourth row nobody wrote down.
//
// A generator's own exit status never reaches here. It is a verdict on one
// invocation, delivered to cpybkc; this is a verdict on the whole run, and a
// run has several generators.
func statusOf(err error) status {
	switch {
	case err == nil:
		return statusOK
	case errors.As(err, new(*usageError)):
		return statusUsage
	default:
		return statusFailed
	}
}
