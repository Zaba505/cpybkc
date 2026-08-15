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
	// Unknown is the zero value, and is never a verdict a run reaches. It is
	// first so that nothing defaults to having passed: a [Result] nobody filled
	// in, or an entry looked up in a map of outcomes it was never added to,
	// reads as an entry nothing is known about rather than as one that
	// succeeded. That is the one direction a conformance report must not
	// default in — an entry lost to a broken adapter has told nobody anything.
	Unknown Outcome = iota

	// Passed is an entry the adapter answered and whose answer is what the
	// entry states — in both directions, where the adapter declared a writer.
	Passed

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
	case Unknown:
		return "UNKNOWN"
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

	// Provisional is whether the entry it is about declared itself provisional
	// — an entry whose expected answer nothing has corroborated yet
	// ([github.com/Zaba505/cpybkc/internal/conformance.Provisional]).
	//
	// It sits beside the outcome rather than replacing one of its values,
	// because what happened to the entry and how much the corpus stands behind
	// the entry are two facts and a reader of a report wants both: an
	// implementation that disagrees with a provisional entry has learned
	// something worth reading even though it has not failed.
	Provisional bool

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
	//
	// Normative and provisional entries are both in here, so a reading of this
	// slice has to consult [Result.Provisional]: len(Results) is not the sum of
	// [Report.Counts], and the obvious loop testing Outcome != Passed counts a
	// provisional entry as a failure — which is exactly what [Report.Failed]
	// stopped doing. Ask [Report.Failed] and the two count methods rather than
	// summing this.
	Results []*Result

	// Notes are what happened to the run rather than to an entry: an adapter
	// that exited badly, a door that could not start another process.
	Notes []string
}

// Failed is whether anything went wrong: a normative entry that disagreed, or
// one that could not be asked at all.
//
// A run that could not ask is a failed run and not an empty one. An entry lost
// to a broken adapter has told nobody anything about the generator, and a
// caller that read "nothing mismatched" as "everything passed" would report a
// conformant generator on the strength of never having asked it.
//
// A provisional entry produces no failure verdict, whatever became of it. The
// corpus does not yet stand behind its expected answer, so a run that failed on
// one would be telling an implementation it is wrong on the authority of a byte
// string one person computed by hand — and the failure that guards against is
// not a red mark, it is a generator author who trusts the corpus, changes
// correct code to match a wrong oracle and ships it (#207). What became of it
// is in [Report.Results] and in the report's text either way; it is reported
// and it is not a verdict.
func (r *Report) Failed() bool {
	for _, result := range r.Results {
		if result.Provisional {
			continue
		}

		if result.Outcome != Passed {
			return true
		}
	}

	return false
}

// Counts is how many normative entries passed, disagreed and could not be
// asked.
//
// Provisional entries are in none of the three and are counted by
// [Report.ProvisionalCounts] instead, which is what "counts in no total" means:
// a corpus that grew an uncorroborated entry must not move the number an
// implementation reports, in either direction, or the entry has scored a
// generator nobody agreed it could score.
func (r *Report) Counts() (passed, mismatched, faulted int) {
	return r.count(false)
}

// ProvisionalCounts is how many provisional entries agreed with what they
// state, disagreed with it, and could not be asked.
//
// It is a second set of three rather than an extra value on the first, so that
// a caller adding up a total cannot accidentally include them: every existing
// reading of [Report.Counts] means normative entries, and a fourth return value
// would have been dropped into those sums by whoever updated the call.
func (r *Report) ProvisionalCounts() (agreed, disagreed, unanswered int) {
	return r.count(true)
}

// StatesNothing is whether the run asked about entries and none of them was
// one the corpus stands behind.
//
// It is the one shape a reader must not take for a pass: [Report.Failed] is
// false, [Report.Counts] is three zeros and the process exits zero, and every
// line of the report agrees, because nothing disagreed and nothing faulted. A
// caller that has to act on it — a job that fails a build on a result that
// learned nothing — should ask this rather than match the report's text, which
// is prose and may be reworded.
//
// A run of no entries at all is not this. A descriptive generator is asked
// about nothing by design ([Report.NotApplicable]), and calling that a run that
// states nothing would fail the one case the engine goes out of its way not to
// score.
func (r *Report) StatesNothing() bool {
	passed, mismatched, faulted := r.Counts()
	agreed, disagreed, unanswered := r.ProvisionalCounts()

	return passed+mismatched+faulted == 0 && agreed+disagreed+unanswered > 0
}

// count is either half of the corpus, by whether the entry was provisional.
func (r *Report) count(provisional bool) (passed, mismatched, faulted int) {
	for _, result := range r.Results {
		if result.Provisional != provisional {
			continue
		}

		switch result.Outcome {
		case Passed:
			passed++
		case Mismatched:
			mismatched++
		case Faulted, Unknown:
			// An outcome nobody set is counted with the faults rather than
			// dropped, so that the three numbers always sum to the entries of
			// their half and a result that was never filled in cannot go
			// missing from a total.
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
	normative := passed + mismatched + faulted

	fmt.Fprintf(&said, "%d entries: %d passed, %d disagreed, %d could not be asked\n",
		normative, passed, mismatched, faulted)

	if agreed, disagreed, unanswered := r.ProvisionalCounts(); agreed+disagreed+unanswered > 0 {
		// A line of its own, and outside the total above rather than added to
		// it. The number a run reports is the number of entries the corpus
		// stands behind, and a provisional entry that moved it would have
		// scored a generator on an answer nothing has corroborated.
		fmt.Fprintf(&said, "%d provisional entries, in no total above and in no verdict: "+
			"%d agreed, %d disagreed, %d could not be asked\n",
			agreed+disagreed+unanswered, agreed, disagreed, unanswered)

		if r.StatesNothing() {
			// Said out loud, because every other line of this report would
			// otherwise read as a clean run: nothing disagreed, nothing
			// faulted, and nothing was asked that anybody stands behind. The
			// condition is [Report.StatesNothing] rather than a second reading
			// of the counts, so that the sentence and the accessor cannot come
			// to disagree.
			said.WriteString("every entry asked about was provisional, so this run states nothing about conformance\n")
		}
	}

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
			fmt.Fprintf(&said, "%s %s\n", result.mark(), result.Entry)

			continue
		}

		fmt.Fprintf(&said, "%s %v\n", result.mark(), result.Err)
	}

	return said.String()
}

// mark is how a result's line opens: the outcome, and for a provisional entry
// the word that says the line is a report rather than a verdict.
//
// The outcome is kept and prefixed rather than replaced, because a reader
// scanning for FAIL wants to find a provisional disagreement too — it is the
// one that most needs looking at, since either the implementation or the entry
// is wrong and nothing yet says which.
func (r *Result) mark() string {
	if r.Provisional {
		return "PROVISIONAL " + r.Outcome.String()
	}

	return r.Outcome.String()
}

// pass records an entry whose answer is what the entry states.
func (r *Report) pass(entry *conformance.Entry) {
	r.Results = append(r.Results, &Result{
		Entry:       entry.Name,
		Source:      entry.Source,
		Outcome:     Passed,
		Provisional: entry.IsProvisional(),
	})
}

// mismatch records an entry the adapter answered and disagreed about.
func (r *Report) mismatch(entry *conformance.Entry, err error) {
	r.Results = append(r.Results, &Result{
		Entry:       entry.Name,
		Source:      entry.Source,
		Outcome:     Mismatched,
		Provisional: entry.IsProvisional(),
		Err:         &conformance.MismatchError{Entry: entry.Name, Source: entry.Source, Err: err},
	})
}

// fault records an entry nothing was learned about.
func (r *Report) fault(entry *conformance.Entry, err error) {
	r.Results = append(r.Results, &Result{
		Entry:       entry.Name,
		Source:      entry.Source,
		Outcome:     Faulted,
		Provisional: entry.IsProvisional(),
		Err:         &conformance.RunError{Entry: entry.Name, Source: entry.Source, Err: err},
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
