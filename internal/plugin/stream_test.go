// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package plugin

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// record is one line a generator wrote, as it reached the log.
type record struct {
	Level     slog.Level
	Message   string
	Generator string
	Stream    string
}

// recording is what a run said, in the order it said it.
//
// A handler of its own rather than a text handler and a parse of its output:
// the claims under test are that a line arrives at a level and attributed to a
// generator, and both are structure that a rendering flattens back into a
// string for the test to guess at again.
type recording struct {
	mu      sync.Mutex
	records []record
}

// recorder is a logger that keeps everything, at every level.
func recorder() (*slog.Logger, *recording) {
	kept := &recording{}

	return slog.New(&handler{recording: kept}), kept
}

// all is what has been recorded so far.
func (r *recording) all() []record {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.records)
}

// on is everything recorded on stream, in order.
func (r *recording) on(stream string) []record {
	var found []record

	for _, rec := range r.all() {
		if rec.Stream == stream {
			found = append(found, rec)
		}
	}

	return found
}

// messages is every message recorded on stream, in order.
func (r *recording) messages(stream string) []string {
	var found []string

	for _, rec := range r.on(stream) {
		found = append(found, rec.Message)
	}

	return found
}

// handler collects records into a [recording].
type handler struct {
	recording *recording
	attrs     []slog.Attr
}

// Enabled keeps everything: a test that filtered would be testing its own
// threshold rather than the level a line was recorded at.
func (h *handler) Enabled(context.Context, slog.Level) bool { return true }

// Handle records one line.
func (h *handler) Handle(_ context.Context, r slog.Record) error {
	kept := record{Level: r.Level, Message: r.Message}

	collect := func(a slog.Attr) {
		switch a.Key {
		case "generator":
			kept.Generator = a.Value.String()
		case "stream":
			kept.Stream = a.Value.String()
		}
	}

	for _, a := range h.attrs {
		collect(a)
	}

	r.Attrs(func(a slog.Attr) bool {
		collect(a)

		return true
	})

	h.recording.mu.Lock()
	defer h.recording.mu.Unlock()

	h.recording.records = append(h.recording.records, kept)

	return nil
}

// WithAttrs is how the generator's name reaches a record: the run attaches it
// once and every line the generator writes carries it.
func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handler{recording: h.recording, attrs: append(slices.Clone(h.attrs), attrs...)}
}

// WithGroup is unused, and is the identity rather than a panic so that a caller
// grouping this repository's logs does not fall over on a test double.
func (h *handler) WithGroup(string) slog.Handler { return h }

func TestADiagnosticIsRecordedAtItsSeverity(t *testing.T) {
	t.Parallel()

	// docs/plugin/SPEC.md, "The diagnostic format": `error:` to error,
	// `warning:` to warning, `note:` to info, with the severity stripped
	// because the level now carries it.
	for name, test := range map[string]struct {
		line    string
		level   slog.Level
		message string
	}{
		"error": {
			line:    "error: ORDER-DETAIL.OD-QTY: USAGE COMP-3 is not supported by this generator",
			level:   slog.LevelError,
			message: "ORDER-DETAIL.OD-QTY: USAGE COMP-3 is not supported by this generator",
		},
		"warning": {
			line:    "warning: ORDER-DETAIL: two fields render to the same name",
			level:   slog.LevelWarn,
			message: "ORDER-DETAIL: two fields render to the same name",
		},
		"note": {
			line:    "note: ORDER-DETAIL.OD-QTY: declared as PIC S9(5)V99",
			level:   slog.LevelInfo,
			message: "ORDER-DETAIL.OD-QTY: declared as PIC S9(5)V99",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			level, message := classify(stderrStream, test.line)

			if level != test.level {
				t.Errorf("%q was recorded at %v, want %v", test.line, level, test.level)
			}

			if message != test.message {
				t.Errorf("%q was recorded as %q, want %q", test.line, message, test.message)
			}
		})
	}
}

func TestALineThatIsNotADiagnosticIsRecordedVerbatimAtWarning(t *testing.T) {
	t.Parallel()

	// docs/plugin/SPEC.md: an unrecognised line is surfaced verbatim, and at
	// **warning** rather than at the mildest level available — info is where a
	// log is ordinarily thresholded, and a line cpybkc could not classify is
	// usually a panic.
	for name, line := range map[string]string{
		"no separator":         "panic: runtime error: index out of range [3]",
		"no space":             "error:ORDER-DETAIL: something",
		"indented":             "  error: under a stack trace",
		"an unknown severity":  "fatal: the generator gave up",
		"a bare severity":      "error: ",
		"an empty message":     "error:  ",
		"a severity by itself": "warning:",
		"nothing at all":       "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			level, message := classify(stderrStream, line)

			if level != slog.LevelWarn {
				t.Errorf("%q was recorded at %v, want %v", line, level, slog.LevelWarn)
			}

			if message != line {
				t.Errorf("%q was recorded as %q, want it verbatim", line, message)
			}
		})
	}
}

func TestStandardOutputIsRecordedVerbatim(t *testing.T) {
	t.Parallel()

	// Standard output carries nothing the contract defines, so nothing on it is
	// a diagnostic — a line that looks like one is still just a line, recorded
	// as it was written.
	for _, line := range []string{"generated 4 files", "error: this is not a diagnostic"} {
		level, message := classify(stdoutStream, line)

		if level != slog.LevelInfo {
			t.Errorf("%q was recorded at %v, want %v", line, level, slog.LevelInfo)
		}

		if message != line {
			t.Errorf("%q was recorded as %q, want it verbatim", line, message)
		}
	}
}

func TestBothStreamsAreSurfacedAndAttributed(t *testing.T) {
	t.Parallel()

	body := `
		echo "generated 2 files"
		echo "note: ORDER-DETAIL: one record" >&2
		echo "panic: nothing is fine" >&2
	`

	invocation := generator(t, "go", body, t.TempDir())

	log, kept := recorder()
	r := &Runner{Log: log, TempDir: t.TempDir(), Env: env()}

	if err := r.Run(t.Context(), descriptor(), []Invocation{invocation}); err != nil {
		t.Fatalf("running the generator: %v", err)
	}

	// The whole line on standard output, at info; on standard error, the note
	// stripped of its severity and at info, and the panic verbatim at warning.
	expected := map[string][]record{
		stdoutStream: {
			{Level: slog.LevelInfo, Message: "generated 2 files", Generator: "go", Stream: stdoutStream},
		},
		stderrStream: {
			{Level: slog.LevelInfo, Message: "ORDER-DETAIL: one record", Generator: "go", Stream: stderrStream},
			{Level: slog.LevelWarn, Message: "panic: nothing is fine", Generator: "go", Stream: stderrStream},
		},
	}

	for stream, want := range expected {
		if got := kept.on(stream); !slices.Equal(got, want) {
			t.Errorf("%s was surfaced as %+v, want %+v", stream, got, want)
		}
	}
}

func TestEachGeneratorsOutputIsAttributedToIt(t *testing.T) {
	t.Parallel()

	first := generator(t, "go", `echo "error: from go" >&2`, t.TempDir())
	second := generator(t, "docs", `echo "error: from docs" >&2`, t.TempDir())

	log, kept := recorder()
	r := &Runner{Log: log, TempDir: t.TempDir(), Env: env()}

	if err := r.Run(t.Context(), descriptor(), []Invocation{first, second}); err != nil {
		t.Fatalf("running the generators: %v", err)
	}

	// The two run at the same time, so the order the lines arrive in is not
	// this test's to assert; which generator each is attributed to is.
	said := map[string]string{}

	for _, rec := range kept.all() {
		said[rec.Generator] = rec.Message
	}

	for name, want := range map[string]string{"go": "from go", "docs": "from docs"} {
		if got := said[name]; got != want {
			t.Errorf("the generator %q is recorded as saying %q, want %q", name, got, want)
		}
	}
}

func TestALineIsSurfacedBeforeTheGeneratorExits(t *testing.T) {
	t.Parallel()

	// docs/plugin/SPEC.md: a line is never held back until the process exits.
	// The generator writes, waits for this test to have seen it, and only then
	// exits — so a run that buffered would deadlock rather than pass slowly.
	out := t.TempDir()

	invocation := generator(t, "go", `
		echo "error: written before the wait" >&2
		i=0
		while [ $i -lt 20 ]; do
			if [ -e "$OUT/seen" ]; then exit 0; fi
			sleep 1
			i=$((i + 1))
		done
		exit 1
	`, out)

	log, kept := recorder()
	r := &Runner{Log: log, TempDir: t.TempDir(), Env: env("OUT=" + out)}

	done := make(chan error, 1)

	go func() { done <- r.Run(t.Context(), descriptor(), []Invocation{invocation}) }()

	// Nothing here waits on the process: the line has to arrive while it is
	// still running, and the process only ends because it did.
	waitForLine(t, kept, "written before the wait")

	if err := os.WriteFile(filepath.Join(out, "seen"), nil, 0o644); err != nil {
		t.Fatalf("writing the acknowledgement: %v", err)
	}

	if err := <-done; err != nil {
		t.Fatalf("the generator did not see the acknowledgement: %v", err)
	}
}

// waitForLine blocks until something containing want has been recorded.
func waitForLine(t *testing.T, kept *recording, want string) {
	t.Helper()

	deadline := time.Now().Add(time.Minute)

	for time.Now().Before(deadline) {
		for _, rec := range kept.all() {
			if strings.Contains(rec.Message, want) {
				return
			}
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("%q was never surfaced", want)
}

func TestALongLineIsSurfacedRatherThanEndingTheStream(t *testing.T) {
	t.Parallel()

	// A line longer than the buffer arrives as pieces of itself and the lines
	// after it still arrive, which is what [splitLines] is for: the alternative
	// ends the scan and discards everything the generator wrote afterwards.
	long := strings.Repeat("x", 3*maxLine/2)

	log, kept := recorder()

	surface(t.Context(), log, stderrStream, strings.NewReader(long+"\nafter\n"))

	pieces := kept.messages(stderrStream)

	if len(pieces) < 2 {
		t.Fatalf("a long line was surfaced as %d records, want it split", len(pieces))
	}

	if got, want := pieces[len(pieces)-1], "after"; got != want {
		t.Errorf("the line after a long one was surfaced as %q, want %q", got, want)
	}

	if got, want := len(strings.Join(pieces[:len(pieces)-1], "")), len(long); got != want {
		t.Errorf("the long line was surfaced as %d bytes, want all %d of them", got, want)
	}
}

func TestALastLineWithoutANewlineIsStillSurfaced(t *testing.T) {
	t.Parallel()

	log, kept := recorder()

	surface(t.Context(), log, stderrStream, strings.NewReader("error: it stopped here"))

	if got, want := kept.messages(stderrStream), []string{"it stopped here"}; !slices.Equal(got, want) {
		t.Errorf("a final line without a newline was surfaced as %q, want %q", got, want)
	}
}

func TestACarriageReturnIsNotPartOfTheLine(t *testing.T) {
	t.Parallel()

	log, kept := recorder()

	surface(t.Context(), log, stderrStream, strings.NewReader("error: written on a CRLF host\r\n"))

	if got, want := kept.messages(stderrStream), []string{"written on a CRLF host"}; !slices.Equal(got, want) {
		t.Errorf("a CRLF line was surfaced as %q, want %q", got, want)
	}
}
