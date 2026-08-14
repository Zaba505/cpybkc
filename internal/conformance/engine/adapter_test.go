// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package engine_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zaba505/cpybkc/internal/conformance"
	"github.com/Zaba505/cpybkc/internal/conformance/engine"
)

// adapterEnv is the variable that turns this test binary into an adapter.
//
// The tests need a process on the other end of two pipes, because that is what
// the contract is: a conversation this package holds over a real standard input
// and a real standard output, with a real exit status and a process that can
// really be killed. Re-executing the test binary is how that is had without a
// build step — the alternative is compiling a helper program per run, which
// costs a go build on every `go test` of this package and gives nothing back.
const adapterEnv = "CPYBKC_CONFORMANCE_FAKE_ADAPTER"

// TestMain is the fork in the road: with the variable set this process is an
// adapter, and without it, it is the test binary.
func TestMain(m *testing.M) {
	if path := os.Getenv(adapterEnv); path != "" {
		os.Exit(adapt(path))
	}

	os.Exit(m.Run())
}

// script is what a fake adapter has been told to do, which the test writes to a
// file and the adapter reads.
//
// It reaches the adapter through a file of the test's own and never through the
// engine, which is the point: the engine's frames are what the transcript
// records, and an assertion that they carry no expected value is an assertion
// about the engine rather than about how the fake was arranged.
type script struct {
	// Transcript is where every request frame the adapter received is written,
	// one per line, as it arrives.
	Transcript string

	// The handshake. Protocol, Kind, Name and Capabilities each default to
	// something conformant, so that a test states only what it is about.
	Protocol         *int
	Kind             string
	Name             string
	Capabilities     map[string]bool
	OmitCapabilities bool
	RefuseHello      string

	// GenerateRefuse is a generate the adapter does not serve at all, and
	// GenerateFail is a generate that failed for particular entries.
	GenerateRefuse string
	GenerateFail   map[string]string

	// Entries is what the adapter answers about each entry.
	Entries map[string]entryScript

	// Break is how the adapter stops being an adapter, keyed by the entry it
	// does so on. Its values are the constants below.
	Break map[string]string

	// DeafAfterHello is an adapter that answers the handshake and then stops
	// attending to its standard input altogether — a generator that deadlocked
	// in its own startup, seen from the engine's side. It never reads and never
	// answers, so what it costs the engine is a write that cannot complete
	// rather than an answer that does not come.
	DeafAfterHello bool
}

// entryScript is one entry's answers.
type entryScript struct {
	Decoded       json.RawMessage
	Written       json.RawMessage
	DecodeRefuse  string
	OmitRoundtrip bool
}

// The ways a fake adapter stops being one. Each is a hazard the contract names:
// a process that exits mid-conversation, one that never answers, a library that
// greeted the world on standard output, a blank line, a carriage return the
// engine must not trim, and a frame paired with a request nobody sent.
const (
	breakCrash   = "crash"
	breakHang    = "hang"
	breakGarbage = "garbage"
	breakBlank   = "blank"
	breakCR      = "carriage-return"
	breakWrongID = "wrong-id"
)

// adapt is the fake adapter: it reads frames, writes frames, and misbehaves in
// whichever one way its script names.
func adapt(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "the fake adapter could not read its script: %v\n", err)

		return 2
	}

	var told script
	if err := json.Unmarshal(b, &told); err != nil {
		fmt.Fprintf(os.Stderr, "the fake adapter could not read its script: %v\n", err)

		return 2
	}

	fake := &fakeAdapter{script: told}

	return fake.run()
}

type fakeAdapter struct {
	script script
}

// run reads one frame at a time and answers it, which is the whole of the
// conversation from the other side.
func (f *fakeAdapter) run() int {
	in := bufio.NewReader(os.Stdin)

	for {
		line, err := in.ReadString('\n')
		if err != nil {
			// End of input is a bye already answered: exit zero without
			// writing anything further.
			return 0
		}

		f.record(line)

		var got struct {
			ID      int    `json:"id"`
			Op      string `json:"op"`
			Entry   string `json:"entry"`
			Entries []struct {
				Entry string `json:"entry"`
			} `json:"entries"`
		}

		if err := json.Unmarshal([]byte(line), &got); err != nil {
			fmt.Fprintf(os.Stderr, "the fake adapter was sent something that is not a frame: %q\n", line)

			return 3
		}

		if broke := f.script.Break[got.Entry]; broke != "" && (got.Op == "decode" || got.Op == "roundtrip") {
			if code, done := f.misbehave(broke, got.ID); done {
				return code
			}

			continue
		}

		switch got.Op {
		case "hello":
			f.hello(got.ID)

			if f.script.DeafAfterHello {
				time.Sleep(time.Hour)
			}
		case "generate":
			names := make([]string, 0, len(got.Entries))
			for _, one := range got.Entries {
				names = append(names, one.Entry)
			}

			f.generate(got.ID, names)
		case "decode":
			f.decode(got.ID, got.Entry)
		case "roundtrip":
			f.roundtrip(got.ID, got.Entry)
		case "bye":
			f.write(map[string]any{"id": got.ID, "ok": true})

			return 0
		default:
			f.write(map[string]any{"id": got.ID, "ok": false, "error": "this adapter does not know that operation"})
		}
	}
}

// misbehave is the one way this adapter stops being one, and whether that ends
// the process.
func (f *fakeAdapter) misbehave(how string, id int) (int, bool) {
	switch how {
	case breakCrash:
		fmt.Fprintln(os.Stderr, "the fake adapter is about to crash")

		return 3, true
	case breakHang:
		// Never answers. What ends this is the engine's deadline and the kill
		// behind it — a sleep rather than a blocked channel, because the Go
		// runtime turns a process whose every goroutine is parked into a
		// deadlock panic, which is a crash and not the hang being tested.
		time.Sleep(time.Hour)
	case breakGarbage:
		_, _ = fmt.Fprintln(os.Stdout, "a library in this adapter printed a greeting on standard output")
	case breakBlank:
		_, _ = fmt.Fprintln(os.Stdout, "")
	case breakCR:
		_, _ = fmt.Fprintf(os.Stdout, "{\"id\":%d,\"ok\":true}\r\n", id)
	case breakWrongID:
		f.write(map[string]any{"id": id + 1000, "ok": true})
	}

	return 0, false
}

func (f *fakeAdapter) hello(id int) {
	protocol := engine.Protocol
	if f.script.Protocol != nil {
		protocol = *f.script.Protocol
	}

	if f.script.RefuseHello != "" {
		f.write(map[string]any{"id": id, "ok": false, "error": f.script.RefuseHello, "protocol": protocol})

		return
	}

	name := f.script.Name
	if name == "" {
		name = "the fake adapter"
	}

	kind := f.script.Kind
	if kind == "" {
		kind = "codec"
	}

	said := map[string]any{
		"id": id, "ok": true, "protocol": protocol,
		"name": name, "version": "0.0.1", "kind": kind,
	}

	if !f.script.OmitCapabilities {
		capabilities := f.script.Capabilities
		if capabilities == nil {
			capabilities = map[string]bool{"write": true}
		}

		said["capabilities"] = capabilities
	}

	f.write(said)
}

func (f *fakeAdapter) generate(id int, entries []string) {
	if f.script.GenerateRefuse != "" {
		f.write(map[string]any{"id": id, "ok": false, "error": f.script.GenerateRefuse})

		return
	}

	results := make([]map[string]any, 0, len(entries))

	for _, entry := range entries {
		result := map[string]any{"entry": entry, "ok": true}

		if failed, ok := f.script.GenerateFail[entry]; ok {
			result["ok"] = false
			result["error"] = failed
		}

		results = append(results, result)
	}

	f.write(map[string]any{"id": id, "ok": true, "entries": results})
}

func (f *fakeAdapter) decode(id int, entry string) {
	told, ok := f.script.Entries[entry]

	switch {
	case !ok:
		f.write(map[string]any{"id": id, "ok": false, "error": "this adapter was told nothing about " + entry})
	case told.DecodeRefuse != "":
		f.write(map[string]any{"id": id, "ok": false, "error": told.DecodeRefuse})
	default:
		f.write(map[string]any{"id": id, "ok": true, "entry": entry, "decoded": told.Decoded})
	}
}

func (f *fakeAdapter) roundtrip(id int, entry string) {
	told := f.script.Entries[entry]

	if told.OmitRoundtrip {
		f.write(map[string]any{"id": id, "ok": true, "entry": entry})

		return
	}

	f.write(map[string]any{"id": id, "ok": true, "entry": entry, "written": told.Written})
}

// write puts one frame on standard output, on one line and flushed, which is
// what the framing requires of every writer.
func (f *fakeAdapter) write(frame map[string]any) {
	b, err := json.Marshal(frame)
	if err != nil {
		fmt.Fprintf(os.Stderr, "the fake adapter could not write a frame: %v\n", err)
		os.Exit(4)
	}

	_, _ = fmt.Fprintf(os.Stdout, "%s\n", b)
}

// record appends a request frame to the transcript, so that a test can assert
// what the engine sent as well as what it made of the answers.
func (f *fakeAdapter) record(line string) {
	if f.script.Transcript == "" {
		return
	}

	file, err := os.OpenFile(f.script.Transcript, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}

	defer func() { _ = file.Close() }()

	_, _ = file.WriteString(line)
}

// door is a door onto the fake adapter running the given script.
//
// The transcript is written beside the script, and both are the test's
// temporary directory's, so that a run leaves nothing behind.
func door(t *testing.T, told script) (*engine.Command, string) {
	t.Helper()

	dir := t.TempDir()

	transcript := filepath.Join(dir, "transcript.jsonl")
	told.Transcript = transcript

	b, err := json.Marshal(told)
	if err != nil {
		t.Fatalf("failed to write the fake adapter's script: %v", err)
	}

	path := filepath.Join(dir, "script.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("failed to write the fake adapter's script: %v", err)
	}

	return &engine.Command{
		Path: os.Args[0],
		Env:  append(os.Environ(), adapterEnv+"="+path),
	}, transcript
}

// transcript is every request frame the adapter received, as text.
func transcript(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}

		t.Fatalf("failed to read what the engine sent: %v", err)
	}

	return string(b)
}

// corpus is the entries the tests are written against, by name, or the whole
// corpus where a test names none.
//
// Real entries and not invented ones: what makes a mismatch explainable is the
// descriptor, and a hand-written descriptor in a test is a second author's
// reading of the IR rather than the corpus's own.
func corpus(t *testing.T, names ...string) []*conformance.Entry {
	t.Helper()

	entries, err := conformance.Load(conformance.CorpusPath(repoRoot(t)))
	if err != nil {
		t.Fatalf("the conformance corpus: %v", err)
	}

	if len(names) == 0 {
		return entries
	}

	held := make([]*conformance.Entry, 0, len(names))

	for _, name := range names {
		found := false

		for _, entry := range entries {
			if entry.Name == name {
				held = append(held, entry)
				found = true

				break
			}
		}

		if !found {
			t.Fatalf("the corpus holds no entry called %s", name)
		}
	}

	return held
}

// answers is a script that answers every entry with exactly what the entry
// states, in both directions: the run that passes.
func answers(t *testing.T, entries []*conformance.Entry) map[string]entryScript {
	t.Helper()

	told := make(map[string]entryScript, len(entries))

	for _, entry := range entries {
		values := marshalled(t, entry.Values)

		written := values
		if entry.Values.Failure != "" {
			// Nothing is written back from a read that stopped, and the engine
			// does not ask.
			written = nil
		}

		told[entry.Name] = entryScript{Decoded: values, Written: written}
	}

	return told
}

// marshalled is a values document as a frame carries it.
func marshalled(t *testing.T, values *conformance.Values) json.RawMessage {
	t.Helper()

	b, err := json.Marshal(values)
	if err != nil {
		t.Fatalf("failed to write a values document: %v", err)
	}

	return b
}

// repoRoot walks up from the test's working directory to the directory holding
// go.mod, which for this module is the repository root and so the directory the
// corpus sits in.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found above %q", dir)
		}

		dir = parent
	}
}
