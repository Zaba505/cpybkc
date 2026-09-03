// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/layout"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// Sequencing reaches a generator compiled, and these hold the compilation to
// docs/ir/SPEC.md's "The sequencing automaton": states and transitions and no
// expression, every transition consuming exactly one record, a start state no
// transition re-enters, and a counted run that is one register and one guard
// however wide the count is.
//
// Every test here drives the real layout parser and the real copybook reader, so
// that no test asserts a graph compiled out of a model the readers would never
// have produced. What a test writes is a layout and one copybook per record, and
// what it asserts is the whole graph rendered — one line per state and one per
// transition — because what has to be right is not one guard but which state
// carries it.

// header, detail and summary are the three copybooks of docs/ir/SPEC.md's
// "A counted run, as nodes": a header carrying a detail count and a flag, the
// details the count governs, and the summary the flag governs. Each carries its
// type code at its own first byte, which is what a consumer tells them apart by.
const (
	header = `01 HDR-REC.
   05 HDR-TYPE PIC X(1).
   05 DTL-COUNT PIC 9(2).
   05 SUM-FLAG PIC X(1).
`

	detail = `01 DTL-REC.
   05 DTL-TYPE PIC X(1).
   05 DTL-BODY PIC X(20).
`

	summary = `01 SUM-REC.
   05 SUM-TYPE PIC X(1).
   05 SUM-TOTAL PIC 9(7).
`
)

// countedRun is the layout of that appendix: the records, what tells each apart,
// and the expression saying any number of counted groups make a file.
const countedRun = `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(record SUMMARY (copybook "sum.cpy" SUM-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(discriminate SUMMARY (equals (item SUMMARY SUM-TYPE) "S"))
(sequence
  (+ (seq HEADER
          (times DETAIL (item HEADER DTL-COUNT))
          (when (item HEADER SUM-FLAG) "Y" SUMMARY))))`

// countedRunCopybooks binds those three record names to those three copybooks.
func countedRunCopybooks() map[string]string {
	return map[string]string{"HEADER": header, "DETAIL": detail, "SUMMARY": summary}
}

// compileLayout is the whole pipeline a caller runs: parse the layout, read the
// layers the automaton is compiled out of, resolve each record name to the
// copybook the test gave it, and compile.
//
// It reports what compilation said rather than failing on it, because half of
// these tests are about the fault.
func compileLayout(t *testing.T, source string, copybooks map[string]string) (*Automaton, error) {
	t.Helper()

	return CompileSequence(sequencingOf(t, source, copybooks))
}

// sequencingOf is what [compileLayout] compiles, so that a test needing one
// setting of its own can take the pipeline's answer and change that one thing.
func sequencingOf(t *testing.T, source string, copybooks map[string]string) Sequencing {
	t.Helper()

	file, err := layout.Parse("layout.sexpr", strings.NewReader(source))
	if err != nil {
		t.Fatalf("parsing the layout: %v", err)
	}

	sequence, err := layoutmodel.ReadSequence(file)
	if err != nil {
		t.Fatalf("reading the sequencing layer: %v", err)
	}

	discrimination, err := layoutmodel.ReadDiscrimination(file)
	if err != nil {
		t.Fatalf("reading the discrimination layer: %v", err)
	}

	opts := Sequencing{
		Sequence: sequence,
		Dialect:  copybook.IBMEnterprise(),
		Reading:  layoutmodel.ODOSlide,
		Encoding: mainframe(),
		Framing:  framingIn(t, file),
	}
	for _, discriminator := range discrimination.Records {
		src, bound := copybooks[discriminator.Record]
		if !bound {
			t.Fatalf("the test binds no copybook to record %s", discriminator.Record)
		}

		opts.Records = append(opts.Records, SequencedRecord{
			Name:          discriminator.Record,
			Copybook:      strings.ToLower(discriminator.Record) + ".cpy",
			Item:          recordOf(t, src),
			Discriminator: discriminator.Strategy,
		})
	}

	return opts
}

// framingIn is the framing a test's layout states, or nil where it states none.
//
// Most of these layouts state none, because most of the automaton's rules are
// about the sequence and not about the dataset the records came out of. The one
// that is about both — how far into the file a predicate may read — is keyed on
// the framing, so the tests for it write a `framing` form and this reads it, by
// the same reader a layout goes through.
//
// The form's presence is what is tested and not the read's success:
// [layoutmodel.ReadFraming] refuses a layout carrying no `framing` form at all,
// and taking that as "states none" would make a malformed framing in a test
// silently become the nil one, which is the value the rule is not run on.
func framingIn(t *testing.T, file *layout.File) *layoutmodel.Framing {
	t.Helper()

	if !slices.ContainsFunc(file.Forms, func(form layout.Form) bool { return form.Tag == "framing" }) {
		return nil
	}

	framing, err := layoutmodel.ReadFraming(file)
	if err != nil {
		t.Fatalf("reading the framing layer: %v", err)
	}

	return framing
}

// compiled is a layout the caller expects to compile.
func compiled(t *testing.T, source string, copybooks map[string]string) *Automaton {
	t.Helper()

	automaton, err := compileLayout(t, source, copybooks)
	if err != nil {
		t.Fatalf("compiling the sequence: %v", err)
	}

	return automaton
}

// refused is a layout the caller expects to be rejected.
func refused(t *testing.T, source string, copybooks map[string]string) error {
	t.Helper()

	automaton, err := compileLayout(t, source, copybooks)
	if err == nil {
		t.Fatalf("the sequence compiled, want a fault:\n%s", renderAutomaton(automaton))
	}

	return err
}

// renderAutomaton draws the whole graph: the registers, then one line per state
// and one indented line per transition leaving it.
//
// A rendering rather than a struct literal for [renderSequence]'s reason: what
// has to be right is every guard *and* the state carrying it, and a rendering
// carrying both fails with the whole graph in the message.
func renderAutomaton(a *Automaton) string {
	lines := make([]string, 0, len(a.Registers)+len(a.States))

	for _, register := range a.Registers {
		lines = append(lines, fmt.Sprintf("register %d %s from %s", register.ID, register.Kind, register.Source))
	}

	for _, state := range a.States {
		head := fmt.Sprintf("state %d", state.ID)
		switch {
		case state.Accepts && len(state.Acceptance) > 0:
			head += " accepts when " + renderGuards(state.Acceptance)
		case state.Accepts:
			head += " accepts"
		}

		lines = append(lines, head)
		for _, transition := range state.Transitions {
			lines = append(lines, "  "+renderTransition(transition))
		}
	}

	return strings.Join(lines, "\n")
}

// renderTransition draws one edge in the order a consumer evaluates it: the
// guards that make it eligible, the predicate that selects it, the record it
// admits, the bindings it applies and the state it moves to.
func renderTransition(t *Transition) string {
	parts := make([]string, 0, 5)

	if len(t.Guards) > 0 {
		parts = append(parts, "when "+renderGuards(t.Guards))
	}

	parts = append(parts, "on "+t.Predicate.String())

	parts = append(parts, "admit "+t.Record)

	for _, binding := range t.Bindings {
		if binding.Value == BindLessOne {
			parts = append(parts, "take one off "+binding.Register.String())

			continue
		}

		parts = append(parts, "bind "+binding.Register.String())
	}

	return strings.Join(parts, ", ") + fmt.Sprintf(", go to %d", t.To.ID)
}

// assertRendering fails with the whole graph where it is not the one wanted.
func assertRendering(t *testing.T, a *Automaton, want string) {
	t.Helper()

	if got := renderAutomaton(a); got != strings.TrimSpace(want) {
		t.Errorf("the automaton is\n%s\n\nwant\n%s", got, strings.TrimSpace(want))
	}
}

// TestTheCountedRunOfTheAppendix compiles docs/ir/SPEC.md's "A counted run, as
// nodes" and asserts the graph against it.
//
// The appendix names three states and this graph has four, because a position
// automaton gives every appearance of a record name its own state and the
// appendix merges the two that behave alike. State 1 and state 2 are the
// appendix's single `group`: one is where a header has just been admitted and
// the other where a detail has, and every transition leaving them agrees. States
// carry identifiers and no names, so which of the two a consumer is in is not a
// thing anything downstream can ask.
//
// The one difference in substance is the flag guard the appendix carries on
// `group`'s acceptance and on its third transition. This compiles `(when flag
// "Y" SUMMARY)` into a guard on the transition admitting the summary and into
// nothing on the paths that skip it, because the guard set has no negation and
// the complement of `Y` is not a set a compiler can know. What that costs is one
// of the appendix's four detections, and [TestTheFailuresACountedRunDetects] is
// where each of them is asserted.
func TestTheCountedRunOfTheAppendix(t *testing.T) {
	t.Parallel()

	assertRendering(t, compiled(t, countedRun, countedRunCopybooks()), `
register 1 integer from (item HEADER DTL-COUNT)
register 2 bytes from (item HEADER SUM-FLAG)
state 0
  on bytes-equal HDR-TYPE "H", admit HEADER, bind (item HEADER DTL-COUNT), bind (item HEADER SUM-FLAG), go to 1
state 1 accepts when (item HEADER DTL-COUNT) equal to 0
  when (item HEADER DTL-COUNT) above zero, on bytes-equal DTL-TYPE "D", admit DETAIL, take one off (item HEADER DTL-COUNT), go to 2
  when (item HEADER DTL-COUNT) equal to 0 and (item HEADER SUM-FLAG) equal to "Y", on bytes-equal SUM-TYPE "S", admit SUMMARY, go to 3
  when (item HEADER DTL-COUNT) equal to 0, on bytes-equal HDR-TYPE "H", admit HEADER, bind (item HEADER DTL-COUNT), bind (item HEADER SUM-FLAG), go to 1
state 2 accepts when (item HEADER DTL-COUNT) equal to 0
  when (item HEADER DTL-COUNT) above zero, on bytes-equal DTL-TYPE "D", admit DETAIL, take one off (item HEADER DTL-COUNT), go to 2
  when (item HEADER DTL-COUNT) equal to 0 and (item HEADER SUM-FLAG) equal to "Y", on bytes-equal SUM-TYPE "S", admit SUMMARY, go to 3
  when (item HEADER DTL-COUNT) equal to 0, on bytes-equal HDR-TYPE "H", admit HEADER, bind (item HEADER DTL-COUNT), bind (item HEADER SUM-FLAG), go to 1
state 3 accepts
  on bytes-equal HDR-TYPE "H", admit HEADER, bind (item HEADER DTL-COUNT), bind (item HEADER SUM-FLAG), go to 1
`)
}

// TestTheFailuresACountedRunDetects walks the four things docs/ir/SPEC.md's
// appendix says its automaton catches that a memoryless graph could not, and
// asserts of each that the compiled graph carries what catches it.
//
// The second is the one a compiled `when` cannot carry, and it is asserted as
// what it is rather than skipped: the acceptance guard is the counter and not
// the flag, so a file whose summary is missing where the flag said `Y` is
// accepted. The sub-test after it is the layout an adopter writes instead, where
// the summary is counted rather than flagged and the detection comes back —
// because zero is a test the guard set has and *not `Y`* is not.
func TestTheFailuresACountedRunDetects(t *testing.T) {
	t.Parallel()

	automaton := compiled(t, countedRun, countedRunCopybooks())

	t.Run("a file ending two details short", func(t *testing.T) {
		// End of input in the group with the counter above zero: the
		// acceptance guard does not hold, so the file is truncated rather
		// than complete.
		for _, state := range automaton.States[1:3] {
			if !state.Accepts || len(state.Acceptance) != 1 {
				t.Fatalf("state %d accepts %v under %d guards, want one", state.ID, state.Accepts, len(state.Acceptance))
			}

			if guard := state.Acceptance[0]; guard.Test != GuardEquals || guard.Register.Kind != RegisterInteger {
				t.Errorf("state %d accepts under %s, want the counter equal to zero", state.ID, renderGuard(guard))
			}
		}
	})

	t.Run("a missing summary where the flag says Y", func(t *testing.T) {
		// The appendix's second detection, and the one a compiled `when`
		// gives up. Its accepting states are guarded on the counter alone,
		// so end of input with the flag at `Y` and no summary read is a
		// complete file here and a truncated one there.
		for _, state := range automaton.States[1:3] {
			for _, guard := range state.Acceptance {
				if guard.Register.Kind == RegisterBytes {
					t.Errorf("state %d accepts under %s: a compiled `when` has no negation to put there",
						state.ID, renderGuard(guard))
				}
			}
		}
	})

	t.Run("a missing summary where a count says one", func(t *testing.T) {
		// What an adopter writes to buy that detection back: the summary is
		// counted rather than flagged, and acceptance is guarded on the
		// count reaching zero like every other counted run.
		counted := strings.Replace(countedRun,
			`(when (item HEADER SUM-FLAG) "Y" SUMMARY)`,
			`(times SUMMARY (item HEADER SUM-COUNT))`, 1)
		copybooks := countedRunCopybooks()
		copybooks["HEADER"] = strings.Replace(header, "SUM-FLAG PIC X(1)", "SUM-COUNT PIC 9(1)", 1)

		guarded := compiled(t, counted, copybooks)
		for _, state := range guarded.States[1:] {
			if !state.Accepts {
				continue
			}

			if len(state.Acceptance) == 0 {
				t.Errorf("state %d accepts unguarded, want the summary count to qualify it", state.ID)
			}
		}
	})

	t.Run("a sixth detail where the header said five", func(t *testing.T) {
		// In the group with the counter at zero the detail transition is
		// ineligible, and the two transitions that are eligible are selected
		// by predicates a detail record does not match. So the record is
		// reported rather than admitted.
		detail := transitionAdmitting(t, automaton.States[1], "DETAIL")
		if len(detail.Guards) != 1 || detail.Guards[0].Test != GuardPositive {
			t.Fatalf("the detail transition is guarded by %s, want the counter above zero", renderGuards(detail.Guards))
		}

		for _, other := range automaton.States[1].Transitions {
			if other == detail {
				continue
			}

			if satisfiable(mergeGuards(detail.Guards, other.Guards)) {
				t.Errorf("admitting %s is eligible at the same time as the detail, so a sixth detail has "+
					"somewhere to go", other.Record)
			}
		}
	})

	t.Run("a summary where the flag says N", func(t *testing.T) {
		// The transition admitting the summary carries the flag guard, so
		// with the flag anything but `Y` it is ineligible and a summary
		// record matches nothing the state offers.
		summary := transitionAdmitting(t, automaton.States[1], "SUMMARY")

		var flag *Guard
		for i, guard := range summary.Guards {
			if guard.Register.Kind == RegisterBytes {
				flag = &summary.Guards[i]
			}
		}

		if flag == nil {
			t.Fatalf("the summary transition is guarded by %s, want the flag among them", renderGuards(summary.Guards))
		}

		if flag.Test != GuardEquals || len(flag.Values) != 1 || flag.Values[0].Literal.Text != "Y" {
			t.Errorf("the flag guard is %s, want it equal to \"Y\"", renderGuard(*flag))
		}
	})
}

// transitionAdmitting is the one transition of a state that admits a record.
func transitionAdmitting(t *testing.T, state *State, record string) *Transition {
	t.Helper()

	for _, transition := range state.Transitions {
		if transition.Record == record {
			return transition
		}
	}

	t.Fatalf("state %d offers no transition admitting %s", state.ID, record)

	return nil
}

// TestTheStartStateIsNothingsTarget is docs/layout/SPEC.md's "The first record
// of a file is the first thing the expression admits, and nothing is written to
// say so".
//
// A state is the only thing the automaton knows about position and there is no
// predicate for *first* to lower into, so what makes the first record
// expressible is a state no transition re-enters — even where the file is any
// number of repeats of the same group and every other state is re-entered (#80).
func TestTheStartStateIsNothingsTarget(t *testing.T) {
	t.Parallel()

	automaton := compiled(t, countedRun, countedRunCopybooks())

	if automaton.Start != automaton.States[0] || automaton.Start.ID != 0 {
		t.Fatalf("the start state is %d, want the first state", automaton.Start.ID)
	}

	for _, state := range automaton.States {
		for _, transition := range state.Transitions {
			if transition.To == automaton.Start {
				t.Errorf("state %d re-enters the start state admitting %s", state.ID, transition.Record)
			}
		}
	}

	// And the first record of the file is the only thing it offers.
	if len(automaton.Start.Transitions) != 1 || automaton.Start.Transitions[0].Record != "HEADER" {
		t.Errorf("the start state offers %d transitions, want one admitting HEADER", len(automaton.Start.Transitions))
	}
}

// TestEveryTransitionConsumesExactlyOneRecord is docs/ir/SPEC.md's "No epsilon
// transitions, and what the graph pays instead": there is no transition that
// moves without reading, which is what keeps a generated reader a loop over one
// record at a time.
func TestEveryTransitionConsumesExactlyOneRecord(t *testing.T) {
	t.Parallel()

	automaton := compiled(t, countedRun, countedRunCopybooks())

	for _, state := range automaton.States {
		for _, transition := range state.Transitions {
			if transition.Record == "" {
				t.Errorf("state %d carries a transition admitting no record", state.ID)
			}
		}
	}
}

// TestACountedRunIsOneRegisterAndOneGuard is what the register file is bought
// with: a graph whose size follows the layout instead of the data.
//
// A `PIC 9(4)` count admits ten thousand different files and the graph is the
// same size for all of them — one register, one guard and no state per possible
// count, which is what docs/ir/SPEC.md means by a counted run compiled without
// unrolling.
func TestACountedRunIsOneRegisterAndOneGuard(t *testing.T) {
	t.Parallel()

	copybooks := countedRunCopybooks()
	copybooks["HEADER"] = strings.Replace(header, "DTL-COUNT PIC 9(2)", "DTL-COUNT PIC 9(4)", 1)

	source := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(sequence (seq HEADER (times DETAIL (item HEADER DTL-COUNT))))`

	assertRendering(t, compiled(t, source, copybooks), `
register 1 integer from (item HEADER DTL-COUNT)
state 0
  on bytes-equal HDR-TYPE "H", admit HEADER, bind (item HEADER DTL-COUNT), go to 1
state 1 accepts when (item HEADER DTL-COUNT) equal to 0
  when (item HEADER DTL-COUNT) above zero, on bytes-equal DTL-TYPE "D", admit DETAIL, take one off (item HEADER DTL-COUNT), go to 2
state 2 accepts when (item HEADER DTL-COUNT) equal to 0
  when (item HEADER DTL-COUNT) above zero, on bytes-equal DTL-TYPE "D", admit DETAIL, take one off (item HEADER DTL-COUNT), go to 2
`)
}

// TestTheCounterCannotRunBelowZero is docs/ir/SPEC.md's requirement on a
// producer: a transition taking one off a register is guarded so that the
// register cannot run below zero.
//
// One guard does both jobs here, which is why there is no second rule to keep.
// The transition that takes one off is the transition that enters a pass of the
// run, and the guard that bounds the run is the guard that keeps the counter
// non-negative.
func TestTheCounterCannotRunBelowZero(t *testing.T) {
	t.Parallel()

	automaton := compiled(t, countedRun, countedRunCopybooks())

	took := 0
	for _, state := range automaton.States {
		for _, transition := range state.Transitions {
			for _, binding := range transition.Bindings {
				if binding.Value != BindLessOne {
					continue
				}

				took++

				guarded := false
				for _, guard := range transition.Guards {
					if guard.Register == binding.Register && guard.Test == GuardPositive {
						guarded = true
					}
				}

				if !guarded {
					t.Errorf("state %d takes one off %s under %s, want it guarded above zero",
						state.ID, binding.Register, renderGuards(transition.Guards))
				}
			}
		}
	}

	if took == 0 {
		t.Error("nothing takes one off a counter, so the graph is not the one this asserts")
	}
}

// TestARegisterBoundEarlierAndDecrementedByTheAdmittingTransitionCompiles is
// what docs/ir/SPEC.md's "A count is in hand before the extent it decides"
// leaves admissible after #88's rewording: a register bound on an earlier
// transition and rebound, or decremented, by the transition admitting the record
// that reads it.
//
// Both shapes stand on one transition of the counted run. The transition
// admitting a detail reads the counter at step 3 and takes one off it at step 7,
// and the transition admitting the next header reads the counter *and* rebinds
// it from the record it admits — each read taking the value the register held on
// entry to the state, which is what makes the two orders different things.
func TestARegisterBoundEarlierAndDecrementedByTheAdmittingTransitionCompiles(t *testing.T) {
	t.Parallel()

	automaton := compiled(t, countedRun, countedRunCopybooks())
	group := automaton.States[1]

	detail := transitionAdmitting(t, group, "DETAIL")
	if len(detail.Guards) != 1 || len(detail.Bindings) != 1 || detail.Bindings[0].Value != BindLessOne {
		t.Errorf("the detail transition is %s, want it reading the counter and taking one off it",
			renderTransition(detail))
	}

	next := transitionAdmitting(t, group, "HEADER")
	rebinds := false
	for _, binding := range next.Bindings {
		if binding.Value == BindField && binding.Register == next.Guards[0].Register {
			rebinds = true
		}
	}

	if !rebinds {
		t.Errorf("the transition admitting the next header is %s, want it reading the counter and rebinding it",
			renderTransition(next))
	}
}

// TestARunCountedByTheRecordBeingCountedIsRejected is the shape #88 reworded
// docs/ir/SPEC.md to refuse: a repetition naming a register that only the
// transition admitting its own record binds.
//
// A binding applies at step 7 of the read loop and the extent it decides is
// wanted at step 4, so on the first admission the register holds nothing and on
// every later one it holds the previous record's value. What is asserted here is
// as much the message as the refusal: the diagnostic names the record and the
// register rather than reporting a reference that does not resolve, because the
// reference resolves perfectly and it is the *order* that does not work.
func TestARunCountedByTheRecordBeingCountedIsRejected(t *testing.T) {
	t.Parallel()

	source := `(record DETAIL (copybook "dtl.cpy" DTL-REC))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(sequence (times DETAIL (item DETAIL DTL-COUNT)))`

	counted := `01 DTL-REC.
   05 DTL-TYPE PIC X(1).
   05 DTL-COUNT PIC 9(2).
`

	err := refused(t, source, map[string]string{"DETAIL": counted})

	var unbound *UnboundRegisterError
	if !errors.As(err, &unbound) {
		t.Fatalf("compiling reported %v, want an unbound register", err)
	}

	if !unbound.OnAdmitting {
		t.Error("the fault does not say the only binding is on the admitting transition")
	}

	for _, want := range []string{"DETAIL", "(item DETAIL DTL-COUNT)", "after the record is admitted"} {
		if !strings.Contains(unbound.Error(), want) {
			t.Errorf("the diagnostic is %q, want it to name %q", unbound.Error(), want)
		}
	}
}

// TestAValueInARecordNotYetReadIsRejected is docs/ir/SPEC.md's "A value the
// automaton has not read yet", and the other half of what
// [UnboundRegisterError] reports.
//
// The header is optional here, so there is a path to the counted run on which no
// header was admitted and the counter holds nothing. A consumer reading a stream
// forward has no way to go back for it, and `resolve` proves that before a byte
// is read rather than leaving it to surprise a reader halfway through a
// hundred-million-record file.
func TestAValueInARecordNotYetReadIsRejected(t *testing.T) {
	t.Parallel()

	source := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(sequence (seq (? HEADER) (times DETAIL (item HEADER DTL-COUNT))))`

	err := refused(t, source, countedRunCopybooks())

	var unbound *UnboundRegisterError
	if !errors.As(err, &unbound) {
		t.Fatalf("compiling reported %v, want an unbound register", err)
	}

	if unbound.OnAdmitting {
		t.Error("the fault says the admitting transition binds it, and no transition here does")
	}

	for _, want := range []string{"HEADER", "(item HEADER DTL-COUNT)", "not read yet"} {
		if !strings.Contains(unbound.Error(), want) {
			t.Errorf("the diagnostic is %q, want it to name %q", unbound.Error(), want)
		}
	}
}

// TestABindingNeverNamesAnItemThatRepeats is docs/ir/SPEC.md's "A reference
// names a field, not an occurrence of one", applied to the one position this
// story owns.
//
// Nothing carries an occurrence number, so an item with a value per occurrence
// is a value the automaton has no spelling for. The diagnostic names the record,
// the item and the enclosing group that repeats, because the generic version
// sends a reader to look for a misspelling in a reference that is spelled
// correctly.
func TestABindingNeverNamesAnItemThatRepeats(t *testing.T) {
	t.Parallel()

	source := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(sequence (seq HEADER (times DETAIL %s)))`

	for _, tc := range []struct {
		name      string
		reference string
		copybook  string
		item      string
		group     string
	}{
		{
			name:      "the item itself repeats",
			reference: "(item HEADER COUNTS)",
			copybook: `01 HDR-REC.
   05 HDR-TYPE PIC X(1).
   05 COUNTS PIC 9(2) OCCURS 3 TIMES.
`,
			item: "COUNTS",
		},
		{
			name:      "the item sits in a group that repeats",
			reference: "(item HEADER TOTALS COUNTS)",
			copybook: `01 HDR-REC.
   05 HDR-TYPE PIC X(1).
   05 TOTALS OCCURS 3 TIMES.
      10 COUNTS PIC 9(2).
`,
			item:  "COUNTS",
			group: "TOTALS",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			copybooks := countedRunCopybooks()
			copybooks["HEADER"] = tc.copybook

			err := refused(t, fmt.Sprintf(source, tc.reference), copybooks)

			var repeats *SequenceOccurrenceError
			if !errors.As(err, &repeats) {
				t.Fatalf("compiling reported %v, want a repeating reference", err)
			}

			if repeats.Record != "HEADER" || repeats.Item != tc.item || repeats.Group != tc.group {
				t.Errorf("the fault names record %q, item %q and group %q, want %q, %q and %q",
					repeats.Record, repeats.Item, repeats.Group, "HEADER", tc.item, tc.group)
			}
		})
	}
}

// flagged, special and trailer are the copybooks of a shape the OCCURS rule
// above is routinely misread as barring: a data record carrying a flag that
// says something about the record after it, a record that flag governs, and a
// trailer.
//
// The flag is a field of its own here, beside the type code, which is what
// keeps the branch encoding out of reach — see the sub-tests of
// [TestARegisterIsBoundFromARecordInsideARepetition].
const (
	flagged = `01 DTL-REC.
   05 DTL-TYPE PIC X(1).
   05 NEXT-FLAG PIC X(1).
`

	special = `01 SPC-REC.
   05 SPC-TYPE PIC X(1).
   05 SPC-BODY PIC X(9).
`

	trailer = `01 TRL-REC.
   05 TRL-TYPE PIC X(1).
   05 TRL-BODY PIC X(4).
`
)

// flaggedRunCopybooks binds the four record names of that shape.
func flaggedRunCopybooks() map[string]string {
	return map[string]string{
		"HEADER":  header,
		"DETAIL":  flagged,
		"SPECIAL": special,
		"TRAILER": trailer,
	}
}

// TestARegisterIsBoundFromARecordInsideARepetition is docs/layout/SPEC.md's
// restatement of the `OCCURS` rule, and the graph the restatement is about.
//
// The rule bars an item the copybook gives an `OCCURS`. It says nothing about
// how often the record holding the item is admitted, and this is the case that
// makes the difference visible: `DETAIL` sits inside a `+`, the flag it carries
// binds a register, and the register is rebound on every pass. The counted run
// at the top of this file admits its `HEADER` inside a `+` too, but the record
// the flag governs is admitted in the same pass, so nothing there turns on the
// rebinding and nothing there asserts it.
//
// What the graph then says is docs/layout/SPEC.md's "A `when` permits a record,
// and never requires one", asserted rather than described. State 2 is where a
// detail has just been admitted, and the guard on `SPECIAL` is the only guard in
// the whole graph: the loop back to `DETAIL` and the edge to `TRAILER` carry
// none, so a file whose flag says `X` and whose next record is another detail is
// admitted. That is the half of the adopter's rule this format keeps and the
// half it throws away, and the sub-tests are what to write instead.
func TestARegisterIsBoundFromARecordInsideARepetition(t *testing.T) {
	t.Parallel()

	source := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(record SPECIAL (copybook "spc.cpy" SPC-REC))
(record TRAILER (copybook "trl.cpy" TRL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(discriminate SPECIAL (equals (item SPECIAL SPC-TYPE) "S"))
(discriminate TRAILER (equals (item TRAILER TRL-TYPE) "T"))
(sequence (seq HEADER
               (+ (seq DETAIL (when (item DETAIL NEXT-FLAG) "X" SPECIAL)))
               TRAILER))`

	assertRendering(t, compiled(t, source, flaggedRunCopybooks()), `
register 1 bytes from (item DETAIL NEXT-FLAG)
state 0
  on bytes-equal HDR-TYPE "H", admit HEADER, go to 1
state 1
  on bytes-equal DTL-TYPE "D", admit DETAIL, bind (item DETAIL NEXT-FLAG), go to 2
state 2
  when (item DETAIL NEXT-FLAG) equal to "X", on bytes-equal SPC-TYPE "S", admit SPECIAL, go to 3
  on bytes-equal DTL-TYPE "D", admit DETAIL, bind (item DETAIL NEXT-FLAG), go to 2
  on bytes-equal TRL-TYPE "T", admit TRAILER, go to 4
state 3
  on bytes-equal DTL-TYPE "D", admit DETAIL, bind (item DETAIL NEXT-FLAG), go to 2
  on bytes-equal TRL-TYPE "T", admit TRAILER, go to 4
state 4 accepts
`)

	t.Run("enumerating the complement does not close it", func(t *testing.T) {
		t.Parallel()

		// The rewrite an adopter reaches for next: guard the trailer on the
		// values the flag holds when no special follows. The loop back to
		// `DETAIL` still carries no guard, because that edge is generated by
		// the repetition and the `when` wraps `TRAILER`, so the file whose
		// flag says `X` and whose next record is a detail is admitted exactly
		// as before. And gating the trailer makes the states a detail leads
		// to accepting, so the file may now legally end after any detail —
		// which is a second thing the adopter did not ask for.
		gated := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(record SPECIAL (copybook "spc.cpy" SPC-REC))
(record TRAILER (copybook "trl.cpy" TRL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(discriminate SPECIAL (equals (item SPECIAL SPC-TYPE) "S"))
(discriminate TRAILER (equals (item TRAILER TRL-TYPE) "T"))
(sequence (seq HEADER
               (+ (seq DETAIL (when (item DETAIL NEXT-FLAG) "X" SPECIAL)))
               (when (item DETAIL NEXT-FLAG) (one-of " " "N") TRAILER)))`

		assertRendering(t, compiled(t, gated, flaggedRunCopybooks()), `
register 1 bytes from (item DETAIL NEXT-FLAG)
state 0
  on bytes-equal HDR-TYPE "H", admit HEADER, go to 1
state 1
  on bytes-equal DTL-TYPE "D", admit DETAIL, bind (item DETAIL NEXT-FLAG), go to 2
state 2 accepts
  when (item DETAIL NEXT-FLAG) equal to "X", on bytes-equal SPC-TYPE "S", admit SPECIAL, go to 3
  on bytes-equal DTL-TYPE "D", admit DETAIL, bind (item DETAIL NEXT-FLAG), go to 2
  when (item DETAIL NEXT-FLAG) one of " ", "N", on bytes-equal TRL-TYPE "T", admit TRAILER, go to 4
state 3 accepts
  on bytes-equal DTL-TYPE "D", admit DETAIL, bind (item DETAIL NEXT-FLAG), go to 2
  when (item DETAIL NEXT-FLAG) one of " ", "N", on bytes-equal TRL-TYPE "T", admit TRAILER, go to 4
state 4 accepts
`)
	})

	t.Run("the flag as the discriminating item says must, and needs no register", func(t *testing.T) {
		t.Parallel()

		// docs/layout/SPEC.md's answer where the file allows it: two record
		// names over one copybook, told apart by the item that carries the
		// flag. The graph has no registers and no guards at all, and state 3
		// — where a flagged detail has just been admitted — offers `SPECIAL`
		// and nothing else. That is the requirement `when` cannot state,
		// stated as a state.
		branched := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(record DETAIL-FLAGGED (copybook "dtl.cpy" DTL-REC))
(record SPECIAL (copybook "spc.cpy" SPC-REC))
(record TRAILER (copybook "trl.cpy" TRL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(discriminate DETAIL-FLAGGED (equals (item DETAIL-FLAGGED DTL-TYPE) "F"))
(discriminate SPECIAL (equals (item SPECIAL SPC-TYPE) "S"))
(discriminate TRAILER (equals (item TRAILER TRL-TYPE) "T"))
(sequence (seq HEADER (+ (alt DETAIL (seq DETAIL-FLAGGED SPECIAL))) TRAILER))`

		copybooks := flaggedRunCopybooks()
		copybooks["DETAIL-FLAGGED"] = flagged

		assertRendering(t, compiled(t, branched, copybooks), `
state 0
  on bytes-equal HDR-TYPE "H", admit HEADER, go to 1
state 1
  on bytes-equal DTL-TYPE "D", admit DETAIL, go to 2
  on bytes-equal DTL-TYPE "F", admit DETAIL-FLAGGED, go to 3
state 2
  on bytes-equal DTL-TYPE "D", admit DETAIL, go to 2
  on bytes-equal DTL-TYPE "F", admit DETAIL-FLAGGED, go to 3
  on bytes-equal TRL-TYPE "T", admit TRAILER, go to 5
state 3
  on bytes-equal SPC-TYPE "S", admit SPECIAL, go to 4
state 4
  on bytes-equal DTL-TYPE "D", admit DETAIL, go to 2
  on bytes-equal DTL-TYPE "F", admit DETAIL-FLAGGED, go to 3
  on bytes-equal TRL-TYPE "T", admit TRAILER, go to 5
state 5 accepts
`)
	})

	t.Run("a flag beside the type code is an overlap, not a missing feature", func(t *testing.T) {
		t.Parallel()

		// The precondition on the branch above, stated as the fault an
		// adopter meets where it does not hold. Telling the two details apart
		// needs the flag and telling either of them from the trailer needs
		// the type code, and a discriminator is one test on one item — so
		// `resolve` reports the pair that can both match one record rather
		// than a shape the format is missing.
		overlapping := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(record DETAIL-FLAGGED (copybook "dtl.cpy" DTL-REC))
(record SPECIAL (copybook "spc.cpy" SPC-REC))
(record TRAILER (copybook "trl.cpy" TRL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (one-of (item DETAIL NEXT-FLAG) " " "N"))
(discriminate DETAIL-FLAGGED (equals (item DETAIL-FLAGGED NEXT-FLAG) "X"))
(discriminate SPECIAL (equals (item SPECIAL SPC-TYPE) "S"))
(discriminate TRAILER (equals (item TRAILER TRL-TYPE) "T"))
(sequence (seq HEADER (+ (alt DETAIL (seq DETAIL-FLAGGED SPECIAL))) TRAILER))`

		copybooks := flaggedRunCopybooks()
		copybooks["DETAIL-FLAGGED"] = flagged

		err := refused(t, overlapping, copybooks)

		var ambiguous *SequenceAmbiguityError
		if !errors.As(err, &ambiguous) {
			t.Fatalf("compiling reported %v, want an ambiguity", err)
		}

		// The pair is named in the state's evaluation order, and that order
		// is now the copybooks' rather than the walk's: the trailer's type
		// code is the record's first byte and the detail's flag is its
		// second, so the trailer's test is tried first and is named first
		// (#331).
		if ambiguous.Records != [2]string{"TRAILER", "DETAIL"} {
			t.Errorf("the fault names %v, want the pair a discriminator on the flag cannot separate",
				ambiguous.Records)
		}
	})
}

// TestAGuardOnARepetitionLandsOnEveryWayIntoItsBody is docs/layout/SPEC.md's
// section of that name, and the difference between the two places a `when` may
// be written around a repetition.
//
// A guard lands on every transition entering a position inside the expression
// the `when` wraps, and the construction draws no distinction between the way in
// from outside and the way round from the end of a pass. So where the `when` is
// written decides which of the two carries it, and the two spellings compile to
// different graphs rather than to the same one.
func TestAGuardOnARepetitionLandsOnEveryWayIntoItsBody(t *testing.T) {
	t.Parallel()

	source := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(record TRAILER (copybook "trl.cpy" TRL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(discriminate TRAILER (equals (item TRAILER TRL-TYPE) "T"))
(sequence (seq HEADER %s TRAILER))`

	copybooks := map[string]string{"HEADER": header, "DETAIL": detail, "TRAILER": trailer}

	t.Run("a when around the repetition guards the way in", func(t *testing.T) {
		t.Parallel()

		// The flag decides whether the run happens, and nothing about how
		// long it is: the back edge at state 2 carries no guard.
		expression := `(when (item HEADER SUM-FLAG) "Y" (+ DETAIL))`

		assertRendering(t, compiled(t, fmt.Sprintf(source, expression), copybooks), `
register 1 bytes from (item HEADER SUM-FLAG)
state 0
  on bytes-equal HDR-TYPE "H", admit HEADER, bind (item HEADER SUM-FLAG), go to 1
state 1
  when (item HEADER SUM-FLAG) equal to "Y", on bytes-equal DTL-TYPE "D", admit DETAIL, go to 2
  on bytes-equal TRL-TYPE "T", admit TRAILER, go to 3
state 2
  on bytes-equal DTL-TYPE "D", admit DETAIL, go to 2
  on bytes-equal TRL-TYPE "T", admit TRAILER, go to 3
state 3 accepts
`)
	})

	t.Run("a when around the whole body guards the back edge too", func(t *testing.T) {
		t.Parallel()

		// The same guard on both ways in, because every transition admitting
		// the body's first record is a transition entering the expression the
		// `when` wraps. This is the one spelling that puts a guard on a
		// repetition's back edge, and it never puts one there alone.
		expression := `(+ (when (item HEADER SUM-FLAG) "Y" DETAIL))`

		assertRendering(t, compiled(t, fmt.Sprintf(source, expression), copybooks), `
register 1 bytes from (item HEADER SUM-FLAG)
state 0
  on bytes-equal HDR-TYPE "H", admit HEADER, bind (item HEADER SUM-FLAG), go to 1
state 1
  when (item HEADER SUM-FLAG) equal to "Y", on bytes-equal DTL-TYPE "D", admit DETAIL, go to 2
  on bytes-equal TRL-TYPE "T", admit TRAILER, go to 3
state 2
  when (item HEADER SUM-FLAG) equal to "Y", on bytes-equal DTL-TYPE "D", admit DETAIL, go to 2
  on bytes-equal TRL-TYPE "T", admit TRAILER, go to 3
state 3 accepts
`)
	})

	t.Run("a back edge guarded on the record it admits is refused", func(t *testing.T) {
		t.Parallel()

		// The consequence worth having in front of an adopter: a guard on the
		// back edge is a guard on the way in as well, and on the first pass
		// nothing has bound the register. The fault is the strictly-earlier
		// proof and names the read loop, not the repetition — which is why
		// docs/layout/SPEC.md says so where the operator is, rather than
		// leaving it to be found by compiling.
		guarded := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(record TRAILER (copybook "trl.cpy" TRL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(discriminate TRAILER (equals (item TRAILER TRL-TYPE) "T"))
(sequence (seq HEADER
               (+ (when (item DETAIL NEXT-FLAG) (one-of " " "N") DETAIL))
               TRAILER))`

		books := map[string]string{"HEADER": header, "DETAIL": flagged, "TRAILER": trailer}

		err := refused(t, guarded, books)

		var unbound *UnboundRegisterError
		if !errors.As(err, &unbound) {
			t.Fatalf("compiling reported %v, want an unbound register", err)
		}

		if !unbound.OnAdmitting {
			t.Error("the fault does not say the only binding is on the admitting transition")
		}
	})
}

// TestACountThatDoesNotDecodeToAnIntegerIsRejected is docs/layout/SPEC.md's
// third restriction on `times`, and docs/ir/SPEC.md's rule that a producer must
// not bind a field whose value does not decode to the register's kind.
//
// A `when` carries no such rule and the sub-test says so: a bytes register holds
// its source field's bytes as they appear, so a guard over one is a byte
// comparison whatever the item's PICTURE turns out to be.
func TestACountThatDoesNotDecodeToAnIntegerIsRejected(t *testing.T) {
	t.Parallel()

	alphanumeric := `01 HDR-REC.
   05 HDR-TYPE PIC X(1).
   05 DTL-COUNT PIC X(2).
   05 SUM-FLAG PIC X(1).
`

	copybooks := countedRunCopybooks()
	copybooks["HEADER"] = alphanumeric

	source := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(sequence (seq HEADER (times DETAIL (item HEADER DTL-COUNT))))`

	err := refused(t, source, copybooks)

	var kind *SequenceCountKindError
	if !errors.As(err, &kind) {
		t.Fatalf("compiling reported %v, want a count of the wrong kind", err)
	}

	if kind.Record != "HEADER" || kind.Item != "DTL-COUNT" {
		t.Errorf("the fault names record %q and item %q, want HEADER and DTL-COUNT", kind.Record, kind.Item)
	}

	t.Run("a when reads the same item without complaint", func(t *testing.T) {
		flagged := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(sequence (seq HEADER (when (item HEADER DTL-COUNT) "01" DETAIL)))`

		automaton := compiled(t, flagged, copybooks)
		if len(automaton.Registers) != 1 || automaton.Registers[0].Kind != RegisterBytes {
			t.Errorf("the automaton carries %d registers, want one holding bytes", len(automaton.Registers))
		}
	})
}

// TestAnItemReferenceNamingNoItemIsRejected is the half of the reference rules
// that needs a copybook, which is why it is here and not in `layoutmodel`.
func TestAnItemReferenceNamingNoItemIsRejected(t *testing.T) {
	t.Parallel()

	source := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(sequence (seq HEADER (times DETAIL (item HEADER NO-SUCH-COUNT))))`

	err := refused(t, source, countedRunCopybooks())

	var missing *SequenceItemError
	if !errors.As(err, &missing) {
		t.Fatalf("compiling reported %v, want an unresolved reference", err)
	}

	if missing.Operator != "times" || missing.Record != "HEADER" {
		t.Errorf("the fault names operator %q of record %q, want times of HEADER", missing.Operator, missing.Record)
	}
}

// TestAFlagGoverningTheRecordHoldingItBecomesABranch is docs/ir/SPEC.md's "When
// a value becomes a state, and when it becomes a register", and the SHOULD in
// it kept rather than restated.
//
// Two records told apart by a flag are an `alt` of two names with a
// discriminator each: the dependence on the value has become the state the
// automaton is in, and no register is emitted for it. A register the graph does
// not need is memory every consumer in every language carries for nothing.
func TestAFlagGoverningTheRecordHoldingItBecomesABranch(t *testing.T) {
	t.Parallel()

	source := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(sequence (alt HEADER DETAIL))`

	automaton := compiled(t, source, countedRunCopybooks())

	if len(automaton.Registers) != 0 {
		t.Errorf("the automaton carries %d registers, want none", len(automaton.Registers))
	}

	assertRendering(t, automaton, `
state 0
  on bytes-equal HDR-TYPE "H", admit HEADER, go to 1
  on bytes-equal DTL-TYPE "D", admit DETAIL, go to 2
state 1 accepts
state 2 accepts
`)
}

// TestASingleRecordTypeCompilesToNoPredicate is docs/ir/SPEC.md's "A transition
// may carry no predicate" and the strategy #28 named for it.
//
// A file with one record type has nothing for a predicate to test, and the
// transition admitting it carries the *absence* of a predicate rather than a
// member of the set testing nothing.
func TestASingleRecordTypeCompilesToNoPredicate(t *testing.T) {
	t.Parallel()

	source := `(record DETAIL (copybook "dtl.cpy" DTL-REC))
(discriminate DETAIL single-record-type)
(sequence (* DETAIL))`

	assertRendering(t, compiled(t, source, countedRunCopybooks()), `
state 0 accepts
  on no predicate, admit DETAIL, go to 1
state 1 accepts
  on no predicate, admit DETAIL, go to 1
`)
}

// TestATransitionCarryingNoPredicateBesideAnEligibleSiblingIsRejected is #80
// folded into the overlap rule rather than beside it.
//
// A transition carrying no predicate matches every record, so it overlaps every
// transition leaving its state whose guards can hold at the same time as its
// own. Guards are the only thing that can separate two of them, and where
// nothing does the layout is rejected rather than the ambiguity being settled by
// evaluation order — a transition carrying no predicate is not a default arm and
// is not tried last.
func TestATransitionCarryingNoPredicateBesideAnEligibleSiblingIsRejected(t *testing.T) {
	t.Parallel()

	source := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL single-record-type)
(sequence (alt HEADER DETAIL))`

	err := refused(t, source, countedRunCopybooks())

	var ambiguous *SequenceAmbiguityError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("compiling reported %v, want an ambiguity", err)
	}

	if !ambiguous.Unguarded {
		t.Error("the fault does not say one of the two carries no discriminator")
	}

	if ambiguous.State != 0 {
		t.Errorf("the fault is at state %d, want the start state", ambiguous.State)
	}

	// This shape is not about a run of bytes and keeps the wording it had
	// (#326). A record type offering nothing to tell it apart by has no
	// second target to compare with, and naming a byte offset here would
	// point an adopter at the discriminator that is fine.
	message := ambiguous.Error()
	if !strings.Contains(message, "one of them carries no discriminator, so it matches every record") {
		t.Errorf("the unguarded shape has been reworded: %s", message)
	}

	if strings.Contains(message, "byte") {
		t.Errorf("the unguarded shape names a run of bytes, and there is no pair of runs to name: %s", message)
	}

	// The runs the fault carries are the zero pair here, and a run of no
	// bytes says so rather than being drawn as one: the last byte of a run
	// that has none is the byte before its first, so drawing it gives
	// `bytes 0--1` to anyone who renders the fault's runs directly.
	for _, run := range ambiguous.Runs {
		if run.String() != "no bytes" {
			t.Errorf("a target that is not there renders as %s, want no bytes", run)
		}
	}
}

// TestTwoRecordsAtOnePointTestingOneRunOfBytesForOneValueAreRejected is
// docs/ir/SPEC.md's "When two match, and when none does": `resolve` rejects a
// layout whose discriminators overlap, so that the question of what a consumer
// does when two match does not arise.
func TestTwoRecordsAtOnePointTestingOneRunOfBytesForOneValueAreRejected(t *testing.T) {
	t.Parallel()

	source := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "H"))
(sequence (alt HEADER DETAIL))`

	err := refused(t, source, countedRunCopybooks())

	var ambiguous *SequenceAmbiguityError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("compiling reported %v, want an ambiguity", err)
	}

	if ambiguous.Unguarded {
		t.Error("the fault says one carries no discriminator, and both carry one")
	}

	if ambiguous.Records != [2]string{"HEADER", "DETAIL"} {
		t.Errorf("the fault names %v, want both records", ambiguous.Records)
	}
}

// TestTwoDiscriminatorsOverDifferentRunsOfBytesOverlap is the reading
// docs/ir/SPEC.md insists on: the test is whether one input can satisfy both,
// not whether the two read the same bytes.
//
// A record keyed on its first byte beside one keyed on its second is where the
// narrower reading lets a real ambiguity through, because a record can carry
// both values at once.
func TestTwoDiscriminatorsOverDifferentRunsOfBytesOverlap(t *testing.T) {
	t.Parallel()

	elsewhere := `01 DTL-REC.
   05 DTL-LEAD PIC X(1).
   05 DTL-TYPE PIC X(1).
   05 DTL-BODY PIC X(20).
`

	copybooks := countedRunCopybooks()
	copybooks["DETAIL"] = elsewhere

	source := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(sequence (alt HEADER DETAIL))`

	var ambiguous *SequenceAmbiguityError
	if err := refused(t, source, copybooks); !errors.As(err, &ambiguous) {
		t.Fatalf("compiling reported %v, want an ambiguity", err)
	}
}

// The copybooks the shared-window tests below are written over: a type code one
// byte wide at byte zero, one two bytes wide at byte zero, and one two bytes
// wide at byte one. Every record is the same width and carries its type ahead of
// everything else, so the run each discriminator reads is the only thing that
// differs between a pair and nothing else in the compiler has an opinion about
// them.
const (
	narrowType = `01 NAR-REC.
   05 NAR-TYPE PIC X(1).
   05 NAR-BODY PIC X(20).
`

	wideType = `01 WID-REC.
   05 WID-TYPE PIC X(2).
   05 WID-BODY PIC X(19).
`

	shiftedType = `01 SHF-REC.
   05 SHF-LEAD PIC X(1).
   05 SHF-TYPE PIC X(2).
   05 SHF-BODY PIC X(18).
`
)

// sharedWindowCopybooks binds NARROW, WIDE and SHIFTED to those three.
func sharedWindowCopybooks() map[string]string {
	return map[string]string{"NARROW": narrowType, "WIDE": wideType, "SHIFTED": shiftedType}
}

// TestDiscriminatorsAtOneOffsetOverDifferentWidthsDisagreeOnTheirSharedByte is
// #325: two runs that intersect without being identical are decided on the bytes
// they share, and a disagreement anywhere in that window is proof no record
// satisfies both.
//
// A NARROW keyed on byte zero equal to `H` cannot be a WIDE keyed on bytes zero
// and one equal to `DD`, because byte zero would have to be `H` and `D` at once.
// Requiring the two runs to be identical called this pair ambiguous, which is
// the implementation being coarser than docs/ir/SPEC.md's "When two match, and
// when none does" rather than the rule saying so.
func TestDiscriminatorsAtOneOffsetOverDifferentWidthsDisagreeOnTheirSharedByte(t *testing.T) {
	t.Parallel()

	source := `(record NARROW (copybook "narrow.cpy" NAR-REC))
(record WIDE (copybook "wide.cpy" WID-REC))
(discriminate NARROW (equals (item NARROW NAR-TYPE) "H"))
(discriminate WIDE (equals (item WIDE WID-TYPE) "DD"))
(sequence (+ (alt NARROW WIDE)))`

	automaton := compiled(t, source, sharedWindowCopybooks())

	if admitted := admits(automaton.States[0]); len(admitted) != 2 {
		t.Errorf("the start state admits %v, want both records", admitted)
	}
}

// TestDiscriminatorsAtOneOffsetOverDifferentWidthsAgreeOnTheirSharedByte is the
// other side of the same narrowing: intersecting runs whose literals agree
// across the window they share still overlap, because a record carrying `DD` at
// bytes zero and one carries `D` at byte zero too.
func TestDiscriminatorsAtOneOffsetOverDifferentWidthsAgreeOnTheirSharedByte(t *testing.T) {
	t.Parallel()

	source := `(record NARROW (copybook "narrow.cpy" NAR-REC))
(record WIDE (copybook "wide.cpy" WID-REC))
(discriminate NARROW (equals (item NARROW NAR-TYPE) "D"))
(discriminate WIDE (equals (item WIDE WID-TYPE) "DD"))
(sequence (+ (alt NARROW WIDE)))`

	var ambiguous *SequenceAmbiguityError
	if err := refused(t, source, sharedWindowCopybooks()); !errors.As(err, &ambiguous) {
		t.Fatalf("compiling reported %v, want an ambiguity", err)
	}

	if ambiguous.Records != [2]string{"NARROW", "WIDE"} {
		t.Errorf("the fault names %v, want both records", ambiguous.Records)
	}
}

// TestDiscriminatorsOverPartiallyOverlappingRunsAreDecidedOnTheBytesTheyShare
// takes the same rule to runs that start at different offsets: WIDE reads bytes
// zero and one, SHIFTED reads bytes one and two, and byte one is the whole of
// what decides the pair.
//
// Both orders are written out because the pair is walked in the order the
// transitions leave the state, and an intersection taken as "the second run
// inside the first" would answer one of the two and not the other.
func TestDiscriminatorsOverPartiallyOverlappingRunsAreDecidedOnTheBytesTheyShare(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		shifted   string
		ambiguous bool
	}{
		// WIDE asks for `B` at byte one and SHIFTED asks for `B` there too,
		// so one record satisfies both.
		"the shared byte agrees": {shifted: `"BC"`, ambiguous: true},

		// SHIFTED asks for `X` at byte one, which WIDE says is `B`.
		"the shared byte disagrees": {shifted: `"XC"`, ambiguous: false},
	}

	for name, test := range tests {
		for _, order := range [][2]string{{"WIDE", "SHIFTED"}, {"SHIFTED", "WIDE"}} {
			t.Run(name+", "+order[0]+" first", func(t *testing.T) {
				t.Parallel()

				source := `(record WIDE (copybook "wide.cpy" WID-REC))
(record SHIFTED (copybook "shifted.cpy" SHF-REC))
(discriminate WIDE (equals (item WIDE WID-TYPE) "AB"))
(discriminate SHIFTED (equals (item SHIFTED SHF-TYPE) ` + test.shifted + `))
(sequence (+ (alt ` + order[0] + ` ` + order[1] + `)))`

				if !test.ambiguous {
					compiled(t, source, sharedWindowCopybooks())

					return
				}

				var ambiguous *SequenceAmbiguityError
				if err := refused(t, source, sharedWindowCopybooks()); !errors.As(err, &ambiguous) {
					t.Fatalf("compiling reported %v, want an ambiguity", err)
				}
			})
		}
	}
}

// TestAOneOfOverlapsWhereAnyOfItsValuesAgreesOnTheSharedWindow is what a set of
// values means under the narrowed rule: the window belongs to the two runs and
// not to the values, so a `one-of` is inside the rule with nothing added. The
// pair overlaps where any value of the one agrees with any value of the other
// across the bytes the runs share.
func TestAOneOfOverlapsWhereAnyOfItsValuesAgreesOnTheSharedWindow(t *testing.T) {
	t.Parallel()

	const (
		narrowItem = `(item NARROW NAR-TYPE)`
		wideItem   = `(item WIDE WID-TYPE)`
	)

	tests := map[string]struct {
		narrow    string
		wide      string
		ambiguous bool
	}{
		// Neither `H` nor `S` is what WIDE says byte zero is.
		"a one-of on the narrower run agrees on neither value": {
			narrow:    `(one-of ` + narrowItem + ` "H" "S")`,
			wide:      `(equals ` + wideItem + ` "DD")`,
			ambiguous: false,
		},

		// `D` is WIDE's byte zero, so a record carrying `DD` there is a
		// NARROW too.
		"a one-of on the narrower run agrees on one value": {
			narrow:    `(one-of ` + narrowItem + ` "H" "D")`,
			wide:      `(equals ` + wideItem + ` "DD")`,
			ambiguous: true,
		},

		"a one-of on the wider run agrees on neither value": {
			narrow:    `(equals ` + narrowItem + ` "H")`,
			wide:      `(one-of ` + wideItem + ` "DD" "SS")`,
			ambiguous: false,
		},

		"a one-of on the wider run agrees on one value": {
			narrow:    `(equals ` + narrowItem + ` "S")`,
			wide:      `(one-of ` + wideItem + ` "DD" "SS")`,
			ambiguous: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := `(record NARROW (copybook "narrow.cpy" NAR-REC))
(record WIDE (copybook "wide.cpy" WID-REC))
(discriminate NARROW ` + test.narrow + `)
(discriminate WIDE ` + test.wide + `)
(sequence (+ (alt NARROW WIDE)))`

			if !test.ambiguous {
				compiled(t, source, sharedWindowCopybooks())

				return
			}

			var ambiguous *SequenceAmbiguityError
			if err := refused(t, source, sharedWindowCopybooks()); !errors.As(err, &ambiguous) {
				t.Fatalf("compiling reported %v, want an ambiguity", err)
			}
		})
	}
}

// TestTheAmbiguityDiagnosticNamesTheRunsTheDiscriminatorsRead is #326: the
// offset and width of each discriminator's target is the one fact that decides
// what an adopter does next, and it is the one fact the message used to
// withhold.
//
// Discussion #323 is what withholding it costs. It took four rounds to establish
// where the adopter's two targets sat, and the report was filed against the
// sequencing operators — which turned out to be irrelevant, because the fault
// was two type codes that did not line up.
//
// The two shapes are asserted together because the whole point is that they read
// differently. Runs sharing no byte are told apart by nothing a literal can
// express and the layout needs a count, a trailer or a different target; runs
// agreeing over the bytes they share are a clash in the literals and are fixed
// there. Once #325 narrowed the check to the shared window, those are the only
// two shapes left, so the message can say which it is without hedging.
func TestTheAmbiguityDiagnosticNamesTheRunsTheDiscriminatorsRead(t *testing.T) {
	t.Parallel()

	// A detail whose type code sits behind a lead byte, so that the two
	// discriminators read runs that share nothing at all.
	shifted := `01 DTL-REC.
   05 DTL-LEAD PIC X(1).
   05 DTL-TYPE PIC X(1).
   05 DTL-BODY PIC X(20).
`

	disjoint := countedRunCopybooks()
	disjoint["DETAIL"] = shifted

	tests := map[string]struct {
		source    string
		copybooks map[string]string
		runs      [2]ByteRun
		wants     []string
	}{
		// HEADER's type code is byte zero of a header and DETAIL's is byte
		// one of a detail, so no record can be told apart by either literal:
		// a record carries an `H` at byte zero and a `D` at byte one at the
		// same time.
		"runs sharing no bytes": {
			source: `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(sequence (alt HEADER DETAIL))`,
			copybooks: disjoint,
			runs:      [2]ByteRun{{At: 0, Width: 1}, {At: 1, Width: 1}},
			wants: []string{
				"HEADER byte 0",
				"DETAIL byte 1",
				"share no byte",
				"no literal either could carry would tell the two apart",
			},
		},

		// The genuine clash: one run, one literal, and two records asking
		// for it.
		"identical runs agreeing on a literal": {
			source: `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "H"))
(sequence (alt HEADER DETAIL))`,
			copybooks: countedRunCopybooks(),
			runs:      [2]ByteRun{{At: 0, Width: 1}, {At: 0, Width: 1}},
			wants: []string{
				"HEADER byte 0",
				"DETAIL byte 0",
				"agree on a literal over byte 0",
			},
		},

		// Runs of different widths at one offset, which is where naming both
		// ends earns its keep: `bytes 0-1` beside `byte 0` says which of the
		// two reads further and over what the two were compared.
		"intersecting runs of different widths": {
			source: `(record NARROW (copybook "narrow.cpy" NAR-REC))
(record WIDE (copybook "wide.cpy" WID-REC))
(discriminate NARROW (equals (item NARROW NAR-TYPE) "D"))
(discriminate WIDE (equals (item WIDE WID-TYPE) "DD"))
(sequence (alt NARROW WIDE))`,
			copybooks: sharedWindowCopybooks(),
			runs:      [2]ByteRun{{At: 0, Width: 1}, {At: 0, Width: 2}},
			wants: []string{
				"NARROW byte 0",
				"WIDE bytes 0-1",
				"agree on a literal over byte 0",
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var ambiguous *SequenceAmbiguityError
			if err := refused(t, test.source, test.copybooks); !errors.As(err, &ambiguous) {
				t.Fatalf("compiling reported %v, want an ambiguity", err)
			}

			if ambiguous.Runs != test.runs {
				t.Errorf("the fault carries %v, want %v", ambiguous.Runs, test.runs)
			}

			for _, want := range test.wants {
				if !strings.Contains(ambiguous.Error(), want) {
					t.Errorf("the diagnostic does not say %q: %s", want, ambiguous.Error())
				}
			}

			// The fault is about a pair the layout named, and the spans are
			// still the two places it named them. Naming the bytes is an
			// addition to what the message says and not a move of where it
			// points.
			if spans := ambiguous.Diagnostic().Spans; len(spans) != 2 {
				t.Errorf("the fault carries %d spans, want the two appearances", len(spans))
			}
		})
	}
}

// TestTheAmbiguityDiagnosticStillSaysGuardsCanHoldTogether pins the other shape
// #326 leaves alone.
//
// Guards are not about a byte run either: two transitions whose guards can hold
// at the same time are both eligible, and saying so is what stops an adopter
// wondering whether the guards were read at all. The clause is kept beside the
// runs rather than replaced by them.
func TestTheAmbiguityDiagnosticStillSaysGuardsCanHoldTogether(t *testing.T) {
	t.Parallel()

	// After the header the state offers an unguarded detail beside a summary
	// the flag governs, and the two type codes are the same byte asked for
	// the same value.
	source := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(record SUMMARY (copybook "sum.cpy" SUM-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(discriminate SUMMARY (equals (item SUMMARY SUM-TYPE) "D"))
(sequence (seq HEADER (alt DETAIL (when (item HEADER SUM-FLAG) "Y" SUMMARY))))`

	var ambiguous *SequenceAmbiguityError
	if err := refused(t, source, countedRunCopybooks()); !errors.As(err, &ambiguous) {
		t.Fatalf("compiling reported %v, want an ambiguity", err)
	}

	if !ambiguous.Guards {
		t.Fatalf("the fault does not say guards are carried, and the summary's is: %v", ambiguous)
	}

	for _, want := range []string{
		"DETAIL byte 0",
		"SUMMARY byte 0",
		"and their guards can hold at the same time",
	} {
		if !strings.Contains(ambiguous.Error(), want) {
			t.Errorf("the diagnostic does not say %q: %s", want, ambiguous.Error())
		}
	}
}

// TestGuardsSeparateTransitionsWhosePredicatesOverlap is the narrowing
// docs/ir/SPEC.md puts on the overlap rule, and the thing that makes a counted
// run expressible at all.
//
// Two transitions whose guards cannot hold at the same time are never both
// eligible, so their predicates may overlap freely — here they are the very same
// test on the very same run of bytes, and only the counter separates them.
func TestGuardsSeparateTransitionsWhosePredicatesOverlap(t *testing.T) {
	t.Parallel()

	// TAIL is built to the detail's copybook and told apart by the same value
	// in the same place, so nothing but the counter says which of the two a
	// record at that point is.
	source := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(record TAIL (copybook "dtl.cpy" DTL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(discriminate TAIL (equals (item TAIL DTL-TYPE) "D"))
(sequence (seq HEADER (times DETAIL (item HEADER DTL-COUNT)) TAIL))`

	copybooks := countedRunCopybooks()
	copybooks["TAIL"] = detail

	automaton := compiled(t, source, copybooks)

	group := automaton.States[1]
	if len(group.Transitions) != 2 {
		t.Fatalf("the state after the header offers %d transitions, want two", len(group.Transitions))
	}

	if satisfiable(mergeGuards(group.Transitions[0].Guards, group.Transitions[1].Guards)) {
		t.Errorf("the two transitions can be eligible together:\n  %s\n  %s",
			renderTransition(group.Transitions[0]), renderTransition(group.Transitions[1]))
	}
}

// TestANestedWhenIsTheConjunctionTheAlgebraDoesNotCarry is
// docs/layout/SPEC.md's "What the algebra deliberately cannot say", from the
// compiler's side: there is no conjunction on one operator, and a nested `when`
// is the shape that already works. Both guards land on the transition the pair
// governs.
func TestANestedWhenIsTheConjunctionTheAlgebraDoesNotCarry(t *testing.T) {
	t.Parallel()

	source := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record SUMMARY (copybook "sum.cpy" SUM-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate SUMMARY (equals (item SUMMARY SUM-TYPE) "S"))
(sequence
  (seq HEADER
       (when (item HEADER SUM-FLAG) "Y"
             (when (item HEADER HDR-TYPE) (one-of "H" "h") SUMMARY))))`

	automaton := compiled(t, source, countedRunCopybooks())

	summary := transitionAdmitting(t, automaton.States[1], "SUMMARY")
	if len(summary.Guards) != 2 {
		t.Fatalf("the summary transition carries %s, want both tests", renderGuards(summary.Guards))
	}

	if summary.Guards[0].Test != GuardEquals || summary.Guards[1].Test != GuardOneOf {
		t.Errorf("the summary transition carries %s, want an equals and a one-of", renderGuards(summary.Guards))
	}
}

// TestAPointAcceptingUnderTwoSetsOfGuardsIsRejected is the one shape of the
// algebra that has nowhere to go in the IR.
//
// A state carries one list of acceptance guards and a list is a conjunction.
// docs/ir/SPEC.md puts disjunction in the state — a second transition leaving it
// — and acceptance is not a transition, so an `alt` of two counted runs leaves
// the state ahead of it complete under either counter reaching zero and there is
// nowhere to write the second condition. It is reported rather than narrowed to
// one, because either narrowing calls some file the layout admits truncated or
// some file it forbids complete.
func TestAPointAcceptingUnderTwoSetsOfGuardsIsRejected(t *testing.T) {
	t.Parallel()

	source := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(record SUMMARY (copybook "sum.cpy" SUM-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(discriminate SUMMARY (equals (item SUMMARY SUM-TYPE) "S"))
(sequence
  (seq HEADER
       (alt (times DETAIL (item HEADER DTL-COUNT))
            (times SUMMARY (item HEADER DTL-COUNT)))))`

	err := refused(t, source, countedRunCopybooks())

	var acceptance *SequenceAcceptanceError
	if !errors.As(err, &acceptance) {
		t.Fatalf("compiling reported %v, want a disjunctive acceptance", err)
	}

	if acceptance.What != "HEADER" || acceptance.Ways != 2 {
		t.Errorf("the fault is about %q under %d sets of guards, want HEADER under two",
			acceptance.What, acceptance.Ways)
	}
}

// TestATransitionNoRegisterFileAdmitsIsNotEmitted is the other half of what
// makes overlap decidable: a guard list is a flat conjunction of tests against
// literals and zero, so whether one contradicts itself is a question this
// compiler can answer.
//
// A counted run inside a `*` is the shape that produces one. Restarting the run
// needs the counter above zero, leaving it needs the counter at zero, and
// nothing between the two rebinds it — so the edge back into the run is an edge
// no register file admits. It is dropped rather than emitted, because a consumer
// would evaluate it against every record forever and a producer would have to
// explain it.
func TestATransitionNoRegisterFileAdmitsIsNotEmitted(t *testing.T) {
	t.Parallel()

	source := `(record HEADER (copybook "hdr.cpy" HDR-REC))
(record DETAIL (copybook "dtl.cpy" DTL-REC))
(discriminate HEADER (equals (item HEADER HDR-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL DTL-TYPE) "D"))
(sequence (seq HEADER (* (times DETAIL (item HEADER DTL-COUNT)))))`

	automaton := compiled(t, source, countedRunCopybooks())

	for _, state := range automaton.States {
		for _, transition := range state.Transitions {
			if !satisfiable(transition.Guards) {
				t.Errorf("state %d carries %s, which no register file admits",
					state.ID, renderTransition(transition))
			}
		}
	}

	// The run itself is still there, entered from the header and continued
	// from within: what is dropped is only the way back in from its own end.
	if len(automaton.States) != 3 {
		t.Errorf("the automaton has %d states, want the header, the detail and the start", len(automaton.States))
	}
}

// TestCompileSequenceRefusesNoSequence is the one fault that is not about a
// layout: a caller handing over nothing at all.
func TestCompileSequenceRefusesNoSequence(t *testing.T) {
	t.Parallel()

	if _, err := CompileSequence(Sequencing{}); !errors.Is(err, ErrNilSequence) {
		t.Errorf("compiling nothing reported %v, want %v", err, ErrNilSequence)
	}
}

// The copybooks the byte-domain tests below are written over. Every record is
// twenty-one bytes wide and carries a one-byte type code — the header's at byte
// zero and the detail's at byte ten — which is discussion #323's shape: two
// discriminators whose runs never meet, at a state where both records are
// eligible.
//
// What differs between the details is the one item covering byte zero, which is
// what the header's literal is asked of. The header is fixed, and byte ten falls
// inside its five-digit sequence number.
const (
	batchHeader = `01 BAT-HDR.
   05 BAT-TYPE PIC X(1).
   05 BAT-ID   PIC 9(9).
   05 BAT-SEQ  PIC 9(5).
   05 BAT-BODY PIC X(6).
`

	batchDetail = `01 BDT-REC.
   05 BDT-KEY  %s.
   05 BDT-TYPE PIC X(1).
   05 BDT-BODY PIC X(10).
`
)

// batchCopybooks binds HEADER to that header and DETAIL to that detail, with the
// ten bytes ahead of its type code described by key.
func batchCopybooks(key string) map[string]string {
	return map[string]string{
		"HEADER": batchHeader,
		"DETAIL": fmt.Sprintf(batchDetail, key),
	}
}

// batchSource is the layout those two are read under: a file of batches, each a
// header followed by its details, which puts both records on the state after a
// detail.
const batchSource = `(record HEADER (copybook "header.cpy" BAT-HDR))
(record DETAIL (copybook "detail.cpy" BDT-REC))
(discriminate HEADER (equals (item HEADER BAT-TYPE) "H"))
(discriminate DETAIL (equals (item DETAIL BDT-TYPE) "D"))
(sequence (+ (seq HEADER (+ DETAIL))))`

// TestDiscriminatorsOverDifferentRunsAreDisjointWhereTheCopybooksForbidTheOpposingLiteral
// is #330: two runs that share no byte are decided on what the copybooks say
// those bytes may hold, and not on the general truth that a record can carry one
// value at byte zero and another at byte ten.
//
// The header is keyed on byte zero equal to `H` and the detail on byte ten equal
// to `D`, both EBCDIC letters. Byte ten of a header is inside a five-digit
// DISPLAY item, so a header can never carry `D` there; where byte zero of a
// detail is a ten-digit DISPLAY item it can never carry `H` either, and the two
// tests are then provably exclusive with nothing added to the layout. Where byte
// zero of a detail is anything this cannot state a domain for, the pair is left
// overlapping and refused exactly as before.
func TestDiscriminatorsOverDifferentRunsAreDisjointWhereTheCopybooksForbidTheOpposingLiteral(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		key       string
		ambiguous bool
	}{
		// The whole point: neither record can hold the other's letter.
		"an unsigned zoned item holds digits and no letter": {
			key: "PIC 9(10)", ambiguous: false,
		},

		// "Any byte value may appear in an alphanumeric field", so this
		// proves nothing and the refusal stands. One direction is not
		// enough: a detail carrying `H` at byte zero and `D` at byte ten
		// satisfies both tests, whatever the header says about byte ten.
		"an alphanumeric item holds any byte": {
			key: "PIC X(10)", ambiguous: true,
		},

		// The sign is an overpunch on one of the ten digits, and where
		// codec puts it is codec's to say rather than this package's.
		"a signed zoned item is declined": {
			key: "PIC S9(10)", ambiguous: true,
		},

		// A packed item's digits are nibbles and its last nibble is the
		// sign; same reason.
		"a packed item is declined": {
			key: "PIC 9(18) COMP-3", ambiguous: true,
		},

		// A binary item is a bit pattern with no bytes ruled out.
		"a binary item is declined": {
			key: "PIC 9(9) COMP.\n   05 BDT-PAD  PIC X(6)", ambiguous: true,
		},

		// BLANK WHEN ZERO puts the charset's spaces in a numeric item, so
		// the digits are not the whole of what it holds.
		"a numeric item blank when zero is declined": {
			key: "PIC 9(10) BLANK WHEN ZERO", ambiguous: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if !test.ambiguous {
				compiled(t, batchSource, batchCopybooks(test.key))

				return
			}

			var ambiguous *SequenceAmbiguityError
			if err := refused(t, batchSource, batchCopybooks(test.key)); !errors.As(err, &ambiguous) {
				t.Fatalf("compiling reported %v, want an ambiguity", err)
			}

			// The refusal is the one #326 wrote: the two runs are what an
			// adopter reads the message for, and narrowing when the pair is
			// refused does not change what is said when it is.
			want := [2]ByteRun{{At: 0, Width: 1}, {At: 10, Width: 1}}
			if ambiguous.Runs != want && ambiguous.Runs != [2]ByteRun{want[1], want[0]} {
				t.Errorf("the fault names the runs %v, want %v", ambiguous.Runs, want)
			}
		})
	}
}

// TestTheByteDomainIsDeclinedWhereTheLayoutDoesNotSettleTheItem is the soundness
// half: the refinement reads a domain only where the record's own layout says
// which item covers the opposing run, and every other shape is left overlapping.
//
// Each detail below keys on a run the header describes with something other than
// one item at a fixed offset, and each is refused for that reason alone — byte
// zero of every one of them is a zoned item that cannot hold `H`, so the pair
// would be disjoint if the header's side of the question had an answer.
func TestTheByteDomainIsDeclinedWhereTheLayoutDoesNotSettleTheItem(t *testing.T) {
	t.Parallel()

	// A two-byte type code at byte nine spans the header's nine-digit
	// identifier and its sequence number, and one at byte ten sits wholly
	// inside the sequence number.
	spanning := `01 BSP-REC.
   05 BSP-KEY  PIC 9(9).
   05 BSP-TYPE PIC X(2).
   05 BSP-BODY PIC X(10).
`

	inside := `01 BSP-REC.
   05 BSP-KEY  PIC 9(10).
   05 BSP-TYPE PIC X(2).
   05 BSP-BODY PIC X(9).
`

	// Slack: a four-byte binary item SYNCHRONIZED onto a four-byte boundary
	// leaves bytes nine, ten and eleven belonging to no item at all.
	slack := `01 BSL-HDR.
   05 BSL-TYPE PIC X(1).
   05 BSL-FILL PIC X(8).
   05 BSL-BIN  PIC 9(9) COMP SYNC.
`

	// An OCCURS DEPENDING ON ahead of byte ten moves every byte at or after
	// it, so the static layout does not say which item is there.
	variable := `01 BOD-HDR.
   05 BOD-TYPE  PIC X(1).
   05 BOD-COUNT PIC 9(2).
   05 BOD-TAB   PIC X(4) OCCURS 1 TO 5 TIMES DEPENDING ON BOD-COUNT.
   05 BOD-SEQ   PIC 9(5).
`

	tests := map[string]struct {
		header    string
		record    string
		detail    string
		item      string
		literal   string
		ambiguous bool
	}{
		"the opposing run spans two items": {
			header: batchHeader, record: "BAT-HDR", detail: spanning,
			item: "BSP-TYPE", literal: `"DD"`, ambiguous: true,
		},

		"the opposing run sits inside one": {
			header: batchHeader, record: "BAT-HDR", detail: inside,
			item: "BSP-TYPE", literal: `"DD"`, ambiguous: false,
		},

		"the opposing run lands on slack": {
			header: slack, record: "BSL-HDR", detail: fmt.Sprintf(batchDetail, "PIC 9(10)"),
			item: "BDT-TYPE", literal: `"D"`, ambiguous: true,
		},

		"the opposing run sits in an ODO-variable region": {
			header: variable, record: "BOD-HDR", detail: fmt.Sprintf(batchDetail, "PIC 9(10)"),
			item: "BDT-TYPE", literal: `"D"`, ambiguous: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			headerType := recordOf(t, test.header).Children[0].Name
			source := `(record HEADER (copybook "header.cpy" ` + test.record + `))
(record DETAIL (copybook "detail.cpy" BDT-REC))
(discriminate HEADER (equals (item HEADER ` + headerType + `) "H"))
(discriminate DETAIL (equals (item DETAIL ` + test.item + `) ` + test.literal + `))
(sequence (+ (seq HEADER (+ DETAIL))))`

			copybooks := map[string]string{"HEADER": test.header, "DETAIL": test.detail}

			if !test.ambiguous {
				compiled(t, source, copybooks)

				return
			}

			var ambiguous *SequenceAmbiguityError
			if err := refused(t, source, copybooks); !errors.As(err, &ambiguous) {
				t.Fatalf("compiling reported %v, want an ambiguity", err)
			}
		})
	}
}
