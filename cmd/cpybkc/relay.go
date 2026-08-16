// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
)

// generatorKey is the attribute
// [github.com/Zaba505/cpybkc/internal/plugin.Runner] names a generator with,
// and it is the name as the manifest spells it.
//
// It is a constant here and there rather than a shared one because the log is
// the seam between the two: the runner decides what it records and this decides
// what a record looks like on a terminal, and a package neither of them owns
// holding the key would put a third party between them.
const generatorKey = "generator"

// relay is the [slog.Handler] a generator's output reaches standard error
// through.
//
// docs/plugin/SPEC.md requires cpybkc to parse a generator's diagnostics into
// its structured log at the corresponding level, to surface every other line
// verbatim at warning, and to discard nothing. That parsing is
// [github.com/Zaba505/cpybkc/internal/plugin]'s, and what arrives here is its
// result: a record per line, at the level the severity named, attributed to the
// generator that wrote it. docs/cli/SPEC.md fixes what that log looks like when
// it comes out — the same `<severity>: <message>` form as cpybkc's own
// diagnostics, with the generator's name standing between the severity and the
// message.
//
// A handler rather than a wrapper around the runner because the level is where
// the severity went. Reconstituting a severity from a rendered string would be
// parsing back out what the contract just had a plugin write down, and the two
// spellings would then be free to disagree.
type relay struct {
	// mu serialises the writes, so that a line arrives whole.
	//
	// Generators run concurrently (#42) and each has two streams being read at
	// once, so without this a line could be interleaved with another mid-word.
	// docs/cli/SPEC.md leaves the order of two generators' lines unspecified
	// and says nothing that would permit half a line.
	mu *sync.Mutex

	// w is standard error.
	w io.Writer

	// name is the generator every record through this handler came from, taken
	// from the attribute the runner attaches with [slog.Logger.With]. It is
	// empty for a line cpybkc wrote itself, and the absence of a name is what
	// docs/cli/SPEC.md makes that mean.
	name string

	// grouped records that a group is open, which stops [generatorKey] being
	// read out of a record's attributes.
	//
	// Inside a group that key is a different key — `run.generator` is not
	// `generator` — and attributing a line to whatever happened to be called
	// `generator` in some nested group would put a name on a line no generator
	// wrote.
	grouped bool
}

// newRelay is the handler writing to w.
func newRelay(w io.Writer) *relay {
	return &relay{mu: new(sync.Mutex), w: w}
}

// Enabled implements [slog.Handler].
//
// Every level is enabled, because every level here is a line a generator wrote
// and docs/plugin/SPEC.md says none of them is discarded. A threshold would be
// a filter on somebody else's output; the one place that decides what reaches
// this stream is the plugin contract, and it has already decided.
func (r *relay) Enabled(context.Context, slog.Level) bool { return true }

// Handle implements [slog.Handler].
func (r *relay) Handle(_ context.Context, record slog.Record) error {
	name := r.name

	if !r.grouped {
		record.Attrs(func(attr slog.Attr) bool {
			if attr.Key == generatorKey {
				name = attr.Value.String()
			}

			return true
		})
	}

	severity := severityOf(record.Level)

	r.mu.Lock()
	defer r.mu.Unlock()

	// A record carrying a newline is a line long enough that the reader handed
	// it over in pieces, or a plugin that wrote one where the contract says a
	// diagnostic has none. Either way every piece of it is written at the
	// severity the whole arrived at, because the alternative is a line on this
	// stream that no severity opens.
	for line := range strings.SplitSeq(record.Message, "\n") {
		relayed(r.w, severity, name, line)
	}

	return nil
}

// WithAttrs implements [slog.Handler].
//
// Only [generatorKey] is kept. The runner's other attribute says which of the
// generator's two streams a line came from, and docs/cli/SPEC.md has already
// spent that: the stream is what decided the severity, so repeating it on the
// line would be saying the same thing twice in a form a reader has to translate.
func (r *relay) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return r
	}

	next := *r

	if !next.grouped {
		for _, attr := range attrs {
			if attr.Key == generatorKey {
				next.name = attr.Value.String()
			}
		}
	}

	return &next
}

// WithGroup implements [slog.Handler].
func (r *relay) WithGroup(name string) slog.Handler {
	if name == "" {
		return r
	}

	next := *r
	next.grouped = true

	return &next
}

// severityOf is the level a line arrived at, back in the words a reader sees.
//
// docs/plugin/SPEC.md maps `error:` to error, `warning:` to warning and `note:`
// to info, and puts a line that is not a diagnostic at warning. This is that
// mapping read the other way, so a generator's own severity reaches the user
// rather than a summary of it — docs/cli/SPEC.md requires the severity never to
// change on the way through.
//
// The comparisons are ordered rather than exact so that a level between two of
// slog's named ones is reported at the milder of the two it sits between, which
// is the only way to answer without inventing a fourth severity.
func severityOf(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return severityError
	case level >= slog.LevelWarn:
		return severityWarning
	default:
		return severityNote
	}
}

// relayed writes one line that came from a generator.
//
// The message is written exactly as it arrived, which is not what [diagnostic]
// does with a line of cpybkc's own: docs/plugin/SPEC.md requires a line that is
// not a diagnostic to be surfaced verbatim, and trailing whitespace is part of
// a line a generator wrote — a plugin emitting a padded column or a shell
// tracing itself is saying something about how it writes, and cpybkc tidying
// that away would be reporting output nobody produced.
//
// Unlike [diagnostic] it also writes a line with nothing in it rather than
// dropping one, because a generator's output is never discarded and a blank
// line it wrote is output. What that becomes is the severity and the name with
// nothing after them: the separator's own trailing space goes, since it belongs
// to this rendering rather than to the line, and there is no message left for
// it to separate.
func relayed(w io.Writer, severity, name, message string) {
	line := severity + severitySeparator
	if name != "" {
		line += name + severitySeparator
	}

	if message == "" {
		line = strings.TrimSuffix(line, " ")
	} else {
		line += message
	}

	_, _ = io.WriteString(w, line+"\n")
}

// logger is the log to hand to
// [github.com/Zaba505/cpybkc/internal/plugin.Runner].
//
// It exists so that the one place that knows where diagnostics go is the one
// place that builds the log they arrive in, rather than a stage further down
// deciding for itself.
func logger(w io.Writer) *slog.Logger { return slog.New(newRelay(w)) }
