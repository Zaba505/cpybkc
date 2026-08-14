// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Zaba505/cpybkc/internal/conformance"
	"github.com/Zaba505/cpybkc/internal/emit"
)

// The bounds a run is held to when the caller states none.
//
// They are deliberately generous. A deadline is not a performance budget — it is
// what stops one hung entry costing the run — so the number that matters is the
// one an adapter doing honest work never reaches, and every entry of the corpus
// is a handful of bytes.
const (
	// DefaultDeadline bounds an ordinary operation: a handshake, one entry read,
	// one file written and read back, a goodbye.
	DefaultDeadline = time.Minute

	// DefaultBuildDeadline bounds generate, which is the one operation that may
	// run a compiler over the whole corpus. It has a bound of its own because
	// the alternative is one number that has to be generous enough for a
	// toolchain and tight enough for a read, which is a number that is wrong for
	// both.
	DefaultBuildDeadline = 10 * time.Minute

	// DefaultGrace is how long an adapter is given to exit after its input has
	// been closed, before it is killed. An adapter that has answered bye has
	// nothing left to do, so this bounds a process that ignored end of input
	// rather than one that is still working.
	DefaultGrace = 10 * time.Second
)

// Engine drives an adapter through the contract and reports what came back.
//
// The zero value is not runnable: a [Door] is how a process gets started, and
// there is no default one — a door decides what a result is worth, and an engine
// that picked one would be an engine deciding that on a caller's behalf.
type Engine struct {
	// Door starts adapter processes, and is required.
	Door Door

	// Deadline bounds every operation but generate. Zero is [DefaultDeadline].
	Deadline time.Duration

	// BuildDeadline bounds generate. Zero is [DefaultBuildDeadline].
	BuildDeadline time.Duration

	// Grace is how long an adapter is given to exit once the conversation is
	// over. Zero is [DefaultGrace].
	Grace time.Duration
}

// Run asks one adapter about every entry, in order, and reports.
//
// What comes back is a [Report] whether the run went well or badly: an adapter
// that refused the handshake, one that broke halfway and one that disagreed
// about every entry are all runs that happened, and each is reported as itself.
// The error is for a run that could not be attempted at all — no door, no
// entries — because those are the caller's mistake rather than the adapter's
// behaviour.
//
// The conversation is the contract's: hello, then generate carrying every
// entry's descriptor at once, then decode for each entry and roundtrip after it
// where the adapter declared a writer, then bye. Nothing sent carries any part
// of an entry's values.json, and the comparison happens here.
//
// # Fault isolation
//
// An adapter that crashes, hangs or stops making sense costs the entry it was
// asked about and nothing else. That conversation is over — a stream whose
// framing is in doubt cannot be resynchronised — so the process is killed and a
// fresh one is started on the entries that were left, which is the latitude
// docs/adapter/SPEC.md, "Deadlines and lifetime belong to the engine", grants
// in as many words. The one exception is an adapter that breaks during the
// handshake or during generate: neither is about an entry, both would break the
// same way again, and a run that restarted on them would restart forever.
func (e *Engine) Run(ctx context.Context, entries []*conformance.Entry) (*Report, error) {
	if e.Door == nil {
		return nil, fmt.Errorf("the engine has no door to start an adapter through")
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("there is no entry to ask an adapter about")
	}

	report := &Report{Door: e.Door.Describe()}

	for remaining := entries; len(remaining) > 0; {
		session, err := e.hello(ctx, report)
		if err != nil {
			// A fault at the handshake is the one fault that costs the whole
			// run rather than one entry: there is no conversation left to have.
			report.faultAll(remaining, err)

			return report, nil
		}

		if session.adapter.Kind == kindDescriptive {
			// A descriptive generator emits a diagram, a schema or a copybook.
			// It never opens a data file, so the engine MUST NOT send it
			// generate, decode or roundtrip: it says goodbye and reports the run
			// as not applicable. What such a generator should be held to instead
			// is an open question (#193, #201) and is deliberately not answered
			// by asking it the wrong one.
			report.NotApplicable = true
			report.Results = nil

			report.note(session.bye(ctx, e.grace()))

			return report, nil
		}

		remaining = e.pass(ctx, session, remaining, report)

		if len(remaining) > 0 {
			report.Restarts++
		}
	}

	return report, nil
}

// pass asks one adapter process about entries, and hands back the ones a broken
// conversation never reached.
func (e *Engine) pass(ctx context.Context, session *session, entries []*conformance.Entry, report *Report) []*conformance.Entry {
	faults, err := session.generate(ctx, entries, e.buildDeadline())
	if err != nil {
		// Every entry is lost, whether the adapter refused the operation or
		// stopped being an adapter during it. Neither is attributable to an
		// entry, so neither is retried: an adapter that could not generate the
		// corpus would fail to generate it again.
		if broke(err) {
			session.abandon(err)
		} else {
			report.note(session.bye(ctx, e.grace()))
		}

		report.faultAll(entries, err)

		return nil
	}

	for i, entry := range entries {
		if fault := faults[entry.Name]; fault != nil {
			// An entry whose descriptor the generator would not accept, or
			// whose generated code would not compile. It is an adapter fault
			// against that entry, never a mismatch and never a refusal, and the
			// adapter stays alive and serves the rest.
			report.fault(entry, fault)

			continue
		}

		answer, err := session.ask(ctx, entry, e.deadline())
		if err == nil {
			e.compare(entry, answer, session.adapter.Writes(), report)

			continue
		}

		if !broke(err) {
			report.fault(entry, err)

			continue
		}

		// The conversation is over. The entries behind this one are carried to
		// a fresh process, which is what keeps one crash from costing the run.
		session.abandon(err)
		report.fault(entry, err)

		return entries[i+1:]
	}

	report.note(session.bye(ctx, e.grace()))

	return nil
}

// compare holds one answer against what the entry states, and says where the
// descriptor puts whatever disagreed.
func (e *Engine) compare(entry *conformance.Entry, answer *conformance.Answer, writes bool, report *Report) {
	var err error

	if writes {
		err = conformance.CompareAnswer(entry.Values, answer)
	} else {
		// A read-only adapter is asked only the reading direction, and an engine
		// MUST NOT report an entry as failing because written is absent from an
		// adapter that declared write: false. The claim the run makes is smaller
		// and the report says so; it is not a lesser result.
		err = conformance.Compare(entry.Values, answer.Decoded)
	}

	if err == nil {
		report.pass(entry)

		return
	}

	report.mismatch(entry, explain(err, positions(entry), entry.Input))
}

// hello starts an adapter and shakes hands with it.
func (e *Engine) hello(ctx context.Context, report *Report) (*session, error) {
	process, err := e.Door.Open(ctx)
	if err != nil {
		return nil, err
	}

	session := &session{conn: newConn(process)}

	if err := session.handshake(ctx, e.deadline()); err != nil {
		session.abandon(err)

		return nil, err
	}

	// The first adapter's declaration is the run's. A restarted process that
	// declared something different is a different adapter answering the same
	// corpus, which is worth saying and is not worth silently averaging.
	if report.Adapter == nil {
		adapter := session.adapter
		report.Adapter = &adapter
	} else if session.adapter.String() != report.Adapter.String() {
		report.note(fmt.Errorf("a restarted adapter declared itself %s, and the first declared itself %s",
			&session.adapter, report.Adapter))
	}

	return session, nil
}

func (e *Engine) deadline() time.Duration {
	if e.Deadline > 0 {
		return e.Deadline
	}

	return DefaultDeadline
}

func (e *Engine) buildDeadline() time.Duration {
	if e.BuildDeadline > 0 {
		return e.BuildDeadline
	}

	return DefaultBuildDeadline
}

func (e *Engine) grace() time.Duration {
	if e.Grace > 0 {
		return e.Grace
	}

	return DefaultGrace
}

// session is one adapter process, from its handshake to its goodbye.
type session struct {
	conn    *conn
	adapter Adapter
}

// handshake is the first frame of every conversation, and the only one whose
// failure costs the run.
func (s *session) handshake(ctx context.Context, deadline time.Duration) error {
	got, err := s.conn.exchange(ctx, deadline, &request{Op: opHello, Protocol: Protocol})
	if err != nil {
		return err
	}

	spoken := 0
	if got.Protocol != nil {
		spoken = *got.Protocol
	}

	if !got.served() {
		// An adapter that does not speak this version answers ok: false and
		// states its own anyway, so that a report can say which two versions
		// failed to meet instead of only that the handshake failed.
		return fmt.Errorf("the adapter refused the handshake: %s (this engine speaks protocol %d and it speaks %d)",
			got.Error, Protocol, spoken)
	}

	if spoken != Protocol {
		return fmt.Errorf("the adapter agreed the handshake while stating protocol %d, and this engine speaks %d",
			spoken, Protocol)
	}

	if got.Kind != kindCodec && got.Kind != kindDescriptive {
		// An engine MUST refuse a kind it does not recognise rather than
		// falling back to one it does: a value added later means something, and
		// treating it as the nearest thing that already existed is how a run
		// reports a result about a thing it did not understand.
		return fmt.Errorf("the adapter declared kind %q, and this engine knows %q and %q",
			got.Kind, kindCodec, kindDescriptive)
	}

	if got.Name == "" {
		return fmt.Errorf("the adapter declared no name, and a report has nothing to call it")
	}

	if got.Capabilities == nil {
		// Required even when it is empty: an author who has to write `{}` has
		// been asked which optional operations they serve, where an author who
		// may omit it has not.
		return fmt.Errorf("the adapter declared no capabilities, and an adapter with none says so with an empty object")
	}

	s.adapter = Adapter{
		Name:         got.Name,
		Version:      got.Version,
		Kind:         got.Kind,
		Protocol:     spoken,
		Capabilities: got.capabilities(),
	}

	return nil
}

// generate hands the adapter every entry's descriptor at once, and reports
// which entries it has code for.
//
// All of them at once because an adapter for a compiled language compiles what
// its generator produced, and compiling is usually the most expensive thing in
// the run by a wide margin: handed the corpus one entry at a time it pays that
// cost once per entry, and handed all of them at once it builds the lot in one
// invocation of its toolchain.
//
// What comes back is one error per entry the adapter could not generate, keyed
// by name — a fault against that entry, which the run then skips while carrying
// on with the rest.
func (s *session) generate(ctx context.Context, entries []*conformance.Entry, deadline time.Duration) (map[string]error, error) {
	req := &request{Op: opGenerate}

	for _, entry := range entries {
		// The binary encoding docs/plugin/SPEC.md calls canonical and the form a
		// generator is handed, so the bytes an adapter passes to its own
		// generator are the bytes cpybkc would have passed it — base64 because a
		// frame is JSON.
		descriptor, err := emit.Marshal(entry.Descriptor)
		if err != nil {
			return nil, fmt.Errorf("conformance entry %s: %w", entry.Name, err)
		}

		req.Entries = append(req.Entries, requestEntry{Entry: entry.Name, Descriptor: descriptor})
	}

	got, err := s.conn.exchange(ctx, deadline, req)
	if err != nil {
		return nil, err
	}

	if !got.served() {
		// A generate the adapter did not serve at all — a malformed request, an
		// argument it could not read. Every entry is lost and the engine MUST
		// NOT send decode or roundtrip for any of them.
		return nil, fmt.Errorf("the adapter could not generate for the corpus at all: %s", got.Error)
	}

	asked := make(map[string]bool, len(entries))
	for _, entry := range entries {
		asked[entry.Name] = true
	}

	faults := make(map[string]error, len(entries))
	answered := make(map[string]bool, len(entries))

	for _, result := range got.Entries {
		if !asked[result.Entry] {
			return nil, fmt.Errorf("the adapter answered generate about %q, which it was not asked about", result.Entry)
		}

		if answered[result.Entry] {
			return nil, fmt.Errorf("the adapter answered generate about %q twice", result.Entry)
		}

		answered[result.Entry] = true

		if result.OK == nil {
			return nil, fmt.Errorf("the adapter's generate result for %q says nothing about whether it succeeded",
				result.Entry)
		}

		if !*result.OK {
			faults[result.Entry] = fmt.Errorf("the adapter has no code to read this entry with: %s", result.Error)
		}
	}

	// Exactly one result per entry the request named. A missing one is not an
	// entry to skip quietly: the adapter has said nothing about whether it can
	// read it, and reading on would be the engine deciding for it.
	for _, entry := range entries {
		if !answered[entry.Name] {
			return nil, fmt.Errorf("the adapter's generate response says nothing about %q, which it was asked about",
				entry.Name)
		}
	}

	return faults, nil
}

// ask puts one entry to the adapter: read these bytes, and — where it declared a
// writer and the read reached the end of the file — write those records back out
// and read them again.
func (s *session) ask(ctx context.Context, entry *conformance.Entry, deadline time.Duration) (*conformance.Answer, error) {
	// An empty file is a file: the pointer is what keeps the member present
	// rather than omitted, so that an entry of no bytes is asked about as an
	// entry of no bytes rather than as a request carrying no input at all.
	input := entry.Input
	if input == nil {
		input = []byte{}
	}

	decoded, err := s.values(ctx, deadline, &request{Op: opDecode, Entry: entry.Name, Input: &input},
		entry.Name, func(got *response) json.RawMessage { return got.Decoded }, "decoded")
	if err != nil {
		return nil, err
	}

	answer := &conformance.Answer{Decoded: decoded}

	// The preconditions on roundtrip, both of them checked here because an
	// engine MUST NOT send a request whose precondition is not met: the adapter
	// declared the write capability, and the read did not stop at a failure — a
	// read that stopped holds no complete set of records to write back.
	if !s.adapter.Writes() || decoded.Failure != "" {
		return answer, nil
	}

	written, err := s.values(ctx, deadline, &request{Op: opRoundtrip, Entry: entry.Name},
		entry.Name, func(got *response) json.RawMessage { return got.Written }, "written")
	if err != nil {
		return nil, err
	}

	answer.Written = written

	return answer, nil
}

// values sends one request that answers with a values document, and reads that
// document with the corpus format's own reader.
//
// A refusal here is a fault against the entry and never a mismatch, and the
// document itself is not: a file the generated reader refused comes back
// ok: true carrying a failure, which is an answer an entry is allowed to expect
// and is compared like any other.
func (s *session) values(ctx context.Context, deadline time.Duration, req *request, entry string,
	held func(*response) json.RawMessage, member string,
) (*conformance.Values, error) {
	got, err := s.conn.exchange(ctx, deadline, req)
	if err != nil {
		return nil, err
	}

	if !got.served() {
		return nil, fmt.Errorf("the adapter could not serve %s for this entry: %s", req.Op, got.Error)
	}

	// The adapter MUST echo the entry. It is checked rather than trusted
	// because the alternative is comparing one entry's answer against another
	// entry's values, which is a mismatch nobody could explain.
	if got.Entry != entry {
		return nil, fmt.Errorf("the adapter answered %s about %q, and it was asked about %q", req.Op, got.Entry, entry)
	}

	document := held(got)
	if len(document) == 0 {
		return nil, fmt.Errorf("the adapter served %s and its answer carries no %s document", req.Op, member)
	}

	values, err := conformance.ParseValues(document)
	if err != nil {
		return nil, fmt.Errorf("the adapter's %s document cannot be read: %w", member, err)
	}

	return values, nil
}

// bye ends a conversation that is still working, and reports an adapter that
// ended badly.
//
// An adapter MUST exit zero when it has answered bye or seen end of input, and
// non-zero when it stopped for any other reason — so a non-zero exit here is
// the adapter saying it broke on its way out. It is a note on the run rather
// than a fault against an entry: every entry has already been answered, and
// reporting it against the last one would blame an entry for something that
// happened after it.
func (s *session) bye(ctx context.Context, grace time.Duration) error {
	got, err := s.conn.exchange(ctx, grace, &request{Op: opBye})
	if err != nil {
		_ = s.conn.abandon()

		return fmt.Errorf("the adapter did not say goodbye: %w", err)
	}

	exited := s.conn.end(grace)

	switch {
	case !got.served():
		// Nothing is lost by it, and it is still a thing the contract does not
		// admit: bye takes no argument an adapter could object to.
		return fmt.Errorf("the adapter refused bye: %s", got.Error)
	case exited != nil:
		return fmt.Errorf("the adapter answered bye and then exited badly: %w", exited)
	}

	return nil
}

// abandon ends a conversation that is already over, and folds what the adapter
// wrote to standard error into the fault that ended it — which an engine SHOULD
// capture and quote beside a fault, and which is usually the only explanation
// there is for a stream that stopped parsing.
//
// It is called before the fault is reported, so that the report carries the
// diagnostics rather than acquiring them afterwards.
func (s *session) abandon(err error) {
	diagnostics := s.conn.abandon()

	var broken *brokenError
	if diagnostics != "" && errors.As(err, &broken) {
		broken.Diagnostics = diagnostics
	}
}
