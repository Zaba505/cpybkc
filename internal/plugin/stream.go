// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package plugin

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
)

// The names the two streams are attributed by, which are the words a plugin
// author reads in docs/plugin/SPEC.md, "Standard streams".
const (
	stdoutStream = "stdout"
	stderrStream = "stderr"
)

// The severities of a diagnostic: a closed set of three, matched
// case-sensitively, and the separator that follows one.
//
// docs/plugin/SPEC.md, "The diagnostic format". The separator is a colon and a
// single space and nothing else, which is what tells a diagnostic from the
// ordinary output of a program that also writes to standard error.
const (
	severityError   = "error"
	severityWarning = "warning"
	severityNote    = "note"

	severitySeparator = ": "
)

// maxLine is how much of one line is read before it is surfaced as a piece of
// itself rather than held for its newline.
//
// A line is not required to be short, and the one that is not is usually the
// one that matters — a panic, a stack trace, a library that meant to write
// several lines and wrote no newlines. So the limit is a chunk size and not a
// ceiling: at it, what has been read is surfaced and reading continues, because
// docs/plugin/SPEC.md says a line is never discarded and a reader that stopped
// at the first long one would discard everything after it too.
const maxLine = 1 << 20

// surface reads everything the generator wrote to one stream and puts it in the
// log, attributed to the generator, as it arrives.
//
// docs/plugin/SPEC.md: cpybkc MUST parse a diagnostic into its structured log
// at the corresponding level, MUST surface any other line verbatim, and MUST do
// neither of those things later than the line arrives. Reading concurrently
// with the process is what makes the last of those true; a generator that
// writes an explanation and then hangs has still explained itself.
func surface(ctx context.Context, log *slog.Logger, stream string, r io.Reader) {
	attr := slog.String("stream", stream)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(nil, maxLine)
	scanner.Split(splitLines(maxLine))

	for scanner.Scan() {
		level, message := classify(stream, scanner.Text())

		log.LogAttrs(ctx, level, message, attr)
	}

	if err := scanner.Err(); err != nil {
		// The read failed partway. That is not the generator's fault and not a
		// failed run — the exit status is the verdict — but the output is
		// incomplete and saying so is cheaper than leaving a user to wonder
		// where the rest of a message went.
		log.LogAttrs(ctx, slog.LevelWarn, "the rest of what this generator wrote could not be read: "+err.Error(), attr)
	}
}

// classify says at what level a line is recorded, and with what message.
//
// docs/plugin/SPEC.md fixes three of the four answers. `error:` records at
// error, `warning:` at warning and `note:` at info, with the severity stripped
// because the level now carries it; anything on standard error that is not a
// diagnostic is recorded **verbatim at warning**, one level above the `note:` a
// plugin writes deliberately, because info is where a log is ordinarily
// thresholded and a handler configured a notch above it would drop exactly the
// panic that rule exists to surface.
//
// The fourth answer is this repository's, because the contract says only that
// standard output MUST be surfaced verbatim and attributed. It is recorded at
// info: standard output is not the diagnostic channel, so a line on it is not a
// severity anybody stated, and the argument that puts an unclassified *stderr*
// line at warning does not reach it — a plugin that leaves `set -x` on is
// untidy rather than broken, which docs/plugin/SPEC.md says in as many words
// when it makes writing there a SHOULD NOT rather than a MUST NOT.
func classify(stream, line string) (slog.Level, string) {
	if stream != stderrStream {
		return slog.LevelInfo, line
	}

	severity, message, found := strings.Cut(line, severitySeparator)

	// A message that is empty, or that is nothing but space, says nothing a
	// level could be attached to; docs/plugin/SPEC.md puts a bare `error: ` on
	// the text side of the line along with an `error:something` carrying no
	// space and a line indented under a stack trace. The first is this test,
	// the second is Cut finding no separator, and the third is a severity with
	// leading space, which does not match any of the three.
	if !found || strings.TrimSpace(message) == "" {
		return slog.LevelWarn, line
	}

	switch severity {
	case severityError:
		return slog.LevelError, message
	case severityWarning:
		return slog.LevelWarn, message
	case severityNote:
		return slog.LevelInfo, message
	default:
		return slog.LevelWarn, line
	}
}

// splitLines is [bufio.ScanLines] with the one difference [maxLine] describes:
// where ScanLines asks for a bigger buffer and gives up when it cannot have
// one, this hands back what it has and carries on reading.
//
// The only observable difference is on a line longer than max, which arrives as
// several records instead of one. That is what surfacing everything costs; the
// alternative — [bufio.ErrTooLong] — ends the scan, and with it every line the
// generator wrote afterwards.
func splitLines(max int) bufio.SplitFunc {
	return func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			return i + 1, dropCR(data[:i]), nil
		}

		if atEOF {
			if len(data) == 0 {
				return 0, nil, nil
			}

			// The last line of a generator that exited without a final newline
			// is still a line it wrote.
			return len(data), dropCR(data), nil
		}

		if len(data) >= max {
			return len(data), data, nil
		}

		return 0, nil, nil
	}
}

// dropCR removes a terminating carriage return, exactly as [bufio.ScanLines]
// does: a plugin written on, or piped through, something that ends its lines
// with CRLF is writing the same diagnostic as one that does not.
func dropCR(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\r' {
		return data[:len(data)-1]
	}

	return data
}
