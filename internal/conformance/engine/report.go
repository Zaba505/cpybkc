// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package engine

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Zaba505/cpybkc/internal/conformance"
)

// Outcome is what became of one entry.
//
// Three, and keeping them apart is most of the value of the contract: an engine
// that conflated any two of them would report a wrong thing about a working
// generator or a working thing about a broken one.
type Outcome int

const (
	// Passed is an entry the adapter answered and whose answer is what the
	// entry states — in both directions, where the adapter declared a writer.
	Passed Outcome = iota

	// Mismatched is an entry the adapter answered and whose answer is not what
	// the entry states. Something is wrong with the generator or with the entry,
	// and whoever reads the report decides which.
	Mismatched

	// Faulted is an entry the adapter could not answer about at all: it refused
	// the request, it broke, or it did not answer in time. Nothing has been
	// learned about the generator, which is why it is not a mismatch.
	Faulted
)

func (o Outcome) String() string {
	switch o {
	case Passed:
		return "PASS"
	case Mismatched:
		return "FAIL"
	case Faulted:
		return "FAULT"
	default:
		return fmt.Sprintf("Outcome(%d)", int(o))
	}
}

// Result is what became of one entry, and why.
type Result struct {
	// Entry is the entry's name, and Source is the section of a specification
	// it cites as the origin of its expected answer — carried because whoever
	// reads a failure has to decide whether the generator is wrong or the entry
	// is, and that decision starts there.
	Entry  string
	Source string

	Outcome Outcome

	// Err is the disagreement or the fault, and is nil where the entry passed.
	// It is a
	// [github.com/Zaba505/cpybkc/internal/conformance.MismatchError] or a
	// [github.com/Zaba505/cpybkc/internal/conformance.RunError], so that a
	// result printed on its own says which of the two it is.
	Err error
}

// Adapter is what the adapter said about itself at the handshake.
//
// Every member of it is for the report and none of it is compared or parsed: an
// adapter that drives a particular generator at a particular version is the
// useful thing to say, because that is what a reader of a result wants to know
// and nothing else in the conversation carries it.
type Adapter struct {
	Name     string
	Version  string
	Kind     string
	Protocol int

	// Capabilities are the optional operations it declared. A capability absent
	// is one it does not have.
	Capabilities map[string]bool
}

// Writes is whether the adapter's generator emits a writer.
//
// A generator that emits a reader and no writer is conformant to
// docs/ir/SPEC.md's "Writing a file", so an engine that demanded the writing
// direction of every adapter would fail every positive entry for such a
// generator — reporting, once per entry, a missing answer to a question the
// specification never obliged it to answer.
func (a *Adapter) Writes() bool { return a != nil && a.Capabilities[capabilityWrite] }

// String is the adapter as a report names it.
func (a *Adapter) String() string {
	said := a.Name
	if said == "" {
		said = "an adapter that gave no name"
	}

	if a.Version != "" {
		said += " " + a.Version
	}

	return fmt.Sprintf("%s (%s, protocol %d)", said, a.Kind, a.Protocol)
}

// Report is one run: which adapter, through which door, and what became of
// every entry it was asked about.
type Report struct {
	// Door is what the door said it provides, quoted rather than summarised. An
	// engine MUST NOT report a result as though it carried a guarantee its door
	// did not provide, and quoting is how this one keeps that promise: the
	// sentence in a report is the door's own.
	Door string

	// Adapter is what the adapter declared, and is nil where the handshake
	// never completed.
	Adapter *Adapter

	// NotApplicable is a run by an adapter that declared itself descriptive: a
	// generator that emits a diagram, a schema or a copybook never opens a data
	// file, so there is nothing to hand input.bin to. It is not a failure and
	// not a score of zero — the two shapes that must not happen are a
	// descriptive generator scored zero out of the whole corpus, and one
	// declining every entry one at a time.
	NotApplicable bool

	// Restarts is how many fresh adapter processes this run needed after one
	// broke. It is reported because an adapter that had to be restarted is one
	// whose results were produced by more than one process, and because a run
	// that restarted repeatedly is a fact about the adapter that no single entry
	// carries.
	Restarts int

	// Results are the entries, in the order they were asked about.
	Results []*Result

	// Notes are what happened to the run rather than to an entry: an adapter
	// that exited badly, a door that could not start another process.
	Notes []string
}

// Failed is whether anything went wrong: an entry that disagreed, or one that
// could not be asked at all.
//
// A run that could not ask is a failed run and not an empty one. An entry lost
// to a broken adapter has told nobody anything about the generator, and a
// caller that read "nothing mismatched" as "everything passed" would report a
// conformant generator on the strength of never having asked it.
func (r *Report) Failed() bool {
	for _, result := range r.Results {
		if result.Outcome != Passed {
			return true
		}
	}

	return false
}

// Counts is how many entries passed, disagreed and could not be asked.
func (r *Report) Counts() (passed, mismatched, faulted int) {
	for _, result := range r.Results {
		switch result.Outcome {
		case Passed:
			passed++
		case Mismatched:
			mismatched++
		case Faulted:
			faulted++
		}
	}

	return passed, mismatched, faulted
}

// String is the whole report, as something a person reads.
//
// The summary is first because the first question is whether it passed, and
// every entry is listed rather than only the failures, because a corpus run
// whose output shrinks as it goes wrong is one where a missing line and a
// passing entry look the same.
func (r *Report) String() string {
	var said strings.Builder

	if r.Adapter != nil {
		fmt.Fprintf(&said, "adapter: %s\n", r.Adapter)
	}

	fmt.Fprintf(&said, "door: %s\n", r.Door)

	if r.NotApplicable {
		fmt.Fprintf(&said, "\nthis generator is descriptive: it emits something other than code that reads a file,\n"+
			"so the corpus has nothing to ask it and this run is not applicable rather than failed.\n")

		return said.String()
	}

	if r.Adapter != nil && !r.Adapter.Writes() {
		fmt.Fprintf(&said, "this adapter declares no writer, so only the reading direction was asked:\n"+
			"a run by a read-only adapter is a smaller claim than a run by a full one, and not a lesser result.\n")
	}

	passed, mismatched, faulted := r.Counts()

	fmt.Fprintf(&said, "%d entries: %d passed, %d disagreed, %d could not be asked\n",
		len(r.Results), passed, mismatched, faulted)

	if r.Restarts == 1 {
		said.WriteString("a fresh adapter was started after one broke\n")
	} else if r.Restarts > 1 {
		fmt.Fprintf(&said, "%d fresh adapters were started after one broke\n", r.Restarts)
	}

	for _, note := range r.Notes {
		fmt.Fprintf(&said, "%s\n", note)
	}

	said.WriteString("\n")

	for _, result := range r.Results {
		if result.Err == nil {
			fmt.Fprintf(&said, "%s %s\n", result.Outcome, result.Entry)

			continue
		}

		fmt.Fprintf(&said, "%s %v\n", result.Outcome, result.Err)
	}

	return said.String()
}

// pass records an entry whose answer is what the entry states.
func (r *Report) pass(entry *conformance.Entry) {
	r.Results = append(r.Results, &Result{Entry: entry.Name, Source: entry.Source, Outcome: Passed})
}

// mismatch records an entry the adapter answered and disagreed about.
func (r *Report) mismatch(entry *conformance.Entry, err error) {
	r.Results = append(r.Results, &Result{
		Entry:   entry.Name,
		Source:  entry.Source,
		Outcome: Mismatched,
		Err:     &conformance.MismatchError{Entry: entry.Name, Source: entry.Source, Err: err},
	})
}

// fault records an entry nothing was learned about.
func (r *Report) fault(entry *conformance.Entry, err error) {
	r.Results = append(r.Results, &Result{
		Entry:   entry.Name,
		Source:  entry.Source,
		Outcome: Faulted,
		Err:     &conformance.RunError{Entry: entry.Name, Source: entry.Source, Err: err},
	})
}

// faultAll records every entry of a run that ended before they could be asked.
func (r *Report) faultAll(entries []*conformance.Entry, err error) {
	for _, entry := range entries {
		r.fault(entry, err)
	}
}

// note records something that happened to the run rather than to an entry.
func (r *Report) note(err error) {
	if err == nil {
		return
	}

	r.Notes = append(r.Notes, err.Error())
}

// explained is a disagreement with where the descriptor puts the items it
// named.
//
// The disagreement is unchanged and the explanation follows it, because the two
// answer different questions: the first is what the adapter said, which is the
// corpus's own comparison, and the second is where in the file that item lives,
// which only the engine can say.
type explained struct {
	Err error

	// Where is one line per item named, and the bytes it was read from where
	// the framing let them be found.
	Where []string
}

func (e *explained) Error() string {
	return fmt.Sprintf("%v\nwhere the descriptor puts what disagreed:\n\t%s",
		e.Err, strings.Join(e.Where, "\n\t"))
}

func (e *explained) Unwrap() error { return e.Err }

// explain says where the descriptor puts every item a comparison named.
//
// A path the walk could not place is left out rather than guessed at, and a
// comparison naming nothing placeable comes back untouched: the explanation is
// an addition to a report and never a condition of one.
func explain(err error, found map[string]position, input []byte) error {
	if err == nil {
		return nil
	}

	var (
		where []string
		seen  = map[string]bool{}
	)

	for _, path := range paths(err) {
		held, ok := found[path]
		if !ok || seen[path] {
			continue
		}

		seen[path] = true

		where = append(where, held.String())

		if quoted, ok := held.bytes(input); ok {
			where = append(where, quoted)
		}
	}

	if len(where) == 0 {
		return err
	}

	return &explained{Err: err, Where: where}
}

// paths is every place in the values document a comparison disagreed about, in
// the order it reported them.
//
// The tree is walked rather than [errors.As]'d over, because a comparison
// reports every disagreement rather than the first and errors.As stops at the
// first match — which would explain one item of a record whose whole encoding is
// wrong and leave the rest unexplained.
func paths(err error) []string {
	switch held := err.(type) {
	case interface{ Unwrap() []error }:
		var found []string

		for _, one := range held.Unwrap() {
			found = append(found, paths(one)...)
		}

		return found
	case *conformance.PathError:
		return []string{held.Path}
	}

	if unwrapped := errors.Unwrap(err); unwrapped != nil {
		return paths(unwrapped)
	}

	return nil
}
