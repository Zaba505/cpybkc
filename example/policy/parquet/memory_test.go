// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The instrument behind the memory model.
//
// Every constant at the top of convert.go, and every number the two Parquet
// READMEs quote from them, is a *measurement*. A measurement nobody can rerun is
// prose, and prose about heap is the kind that stays plausible for a year and is
// wrong the whole time. This is the harness that takes the readings, held against
// the same row type the conversion writes.
//
// # Where it runs, and what it asserts
//
// It is an ordinary `go test` in the conversion's own module, so `dagger call ci`
// runs it — ExampleParquetCi walks every nested module under example/ and runs
// the standard Go pipeline over each. That is the decision, and the two things it
// rules out are worth saying because both were defensible:
//
//   - **Not a benchmark.** `testing.B` measures work per operation against `b.N`,
//     and nothing here is per-operation: `a` and `W` are slopes of *live heap*
//     against row groups and against rows, taken at two fixed phases of the write.
//     A benchmark that ignored `b.N` would be a test wearing a benchmark's name,
//     and `go test ./...` does not run benchmarks — so the shape below would be
//     compiled by CI and checked by nobody.
//   - **Not a Dagger function.** A stage of its own would be a second definition
//     of a test this module already runs, and it would put a machine-dependent
//     number on the critical path of every CI run in a container whose heap
//     behaviour is not the reader's.
//
// Because it runs in CI, **it asserts a shape and never a byte count.** The
// numbers are a property of the machine, the Go version and the parquet-go pin;
// gating on `a == 1024` would turn a green pipeline red on a dependency bump that
// changed nothing an adopter cares about. What is gated is the model itself: that
// what a closed row group retains is linear in the number of closed row groups,
// that what an open one holds is linear in the rows in it, that a row of this
// schema costs the writer more than the record it came from, and that the curve
// of peak against rows per row group has a bottom — an *interior* minimum, near
// where the rule says it is. Every measured value is logged beside the constant
// convert.go commits, so a reading that has drifted is visible in `go test -v`
// without being a failure.
//
// Turn it up with `-memory.records`; the run at millions of records is #315.
//
//	go test -run TestTheWriterMemoryModelHoldsItsShape -v -memory.records=2000000
//
// # The two traps
//
// Both were hit while producing #304's numbers, and both report a plausible
// number rather than an error — which is what makes them worth a paragraph each
// rather than a line. They are [liveHeap] and [peakOf].
package main

import (
	"flag"
	"fmt"
	"math"
	"runtime"
	"testing"

	"github.com/parquet-go/parquet-go"
)

// memoryRecords is N: how many records each point of the peak curve writes.
//
// The default is about the smallest N at which the curve's bottom is clear of
// the writer's fixed overhead. A 197-column writer holds a few megabytes of
// column-buffer capacity once it has been handed a row or two — 4.8 MB on the
// machine this was written on, against a bottom of 4.1 MB at this N — and that
// offset is added to every point of the sweep, so too small an N flattens the U
// the sweep is drawn to show. It is a flag because the reading an adopter wants
// is at their N and not at ours, and because #315 is this same harness with a
// bigger number.
//
// The `memory.` prefix is deliberate and is the only defence available. A test
// file cannot register on a FlagSet of its own — `go test` parses the global one —
// so this shares a namespace with anything the command under test declares at
// package scope, and a collision is a panic during the test binary's init naming
// `flag.Var` rather than either definition. convert.go's flags live on a
// [flag.FlagSet] of its own and cannot collide; a future package-scope flag here
// would have to avoid this prefix.
var memoryRecords = flag.Int("memory.records", 16384,
	"records written at each point of the peak curve; raise it to take a reading rather than to check the shape")

const (
	// retainedRowGroup is the rows per row group the `a` probe writes at, and it
	// is small on purpose: the model is a·C·(N/R) + W·R, so shrinking R shrinks
	// the buffered term to a single row and leaves the slope all footer.
	//
	// It is not *arbitrarily* small. What a closed row group retains is a
	// ColumnChunk, a ColumnIndex and an OffsetIndex per column, and the last two
	// carry one entry per data page — so a row group large enough to spill more
	// than one page per column would fold a page count into `a` and make it a
	// function of R. Sixteen rows of this schema is one page per column, which is
	// the regime the constant is quoted in.
	retainedRowGroup = 16

	// retainedGroupStep and retainedGroups are the closed-row-group counts the
	// `a` fit is taken over: eight samples, every eighth row group up to
	// sixty-four. Sixty-four groups of this schema retain around 12 MB, which is
	// far enough above the writer's fixed overhead that the slope is the signal
	// and not the rounding.
	retainedGroupStep = 8
	retainedGroups    = 8

	// bufferedRowStep and bufferedSamples are the open-row-group fills the `W`
	// fit is taken over: thirty-two samples, every 512 rows up to 16,384.
	//
	// Wide and dense, both for the reason [harness.buffered] gives. The live heap
	// of an open row group is a staircase, so a fit over a handful of closely
	// spaced samples measures whichever doubling it happened to straddle — a
	// four-sample fit over 512..2,048 rows read 709 B a row where thirty-two over
	// 512..16,384 read 1,365, and only the second is a reading of anything.
	bufferedRowStep = 512
	bufferedSamples = 32

	// writeChunk is how many rows are handed to Write at a time.
	//
	// It matches parquet-go's own internal chunking (writer.go, maxRowsPerWrite)
	// so that batching here changes nothing about when a row group closes. The
	// phases below are defined on the *total* rows written and are indifferent to
	// it either way; this only keeps the harness from being slower than the thing
	// it measures.
	writeChunk = 64

	// sweepPoints is how many rows-per-row-group the peak curve is drawn at,
	// halving from N down. Seven spans a factor of 64, which is wide enough that
	// both ends are clearly up the sides of the curve.
	sweepPoints = 7

	// poolPolicies is how many policy terms [poolOf] builds its ring from, each
	// with one detail record of every type behind it. See there for why this
	// number is what it is.
	poolPolicies = 64

	// minRecords is the smallest -memory.records the sweep can be drawn at: N
	// halved sweepPoints times has to stay above one row.
	//
	// Refusing below it is a diagnostic and not a limit anyone will meet. A sweep
	// with fewer points than it was asked for has no interior for a minimum to be
	// in, so every assertion below would fail for a reason that is about the flag
	// rather than about the model — and at N below two it would fail by indexing
	// an empty sweep, which is a panic in place of a sentence.
	minRecords = 1 << sweepPoints

	// linearityTolerance is how far apart the steepest and shallowest segments of
	// a fit may be before it stops being a straight line. It is loose because it
	// is a shape check on a machine nobody here owns: a genuinely linear term
	// measured over four samples lands well inside it, and a term that had become
	// quadratic in the row-group count would not.
	linearityTolerance = 3.0

	// curveTolerance is how far the observed argmin may sit from the R* the
	// measured a and W predict, as a factor.
	//
	// Four is two steps of the sweep, and it is the right order because the curve
	// is flat where it matters: being off by a factor of k costs (k + 1/k)/2, so
	// two steps is 2.13x and the bottom two or three points of any real sweep sit
	// within noise of each other. Tightening this would be asserting which of two
	// indistinguishable points won.
	curveTolerance = 4.0

	// shoulderRatio is how far above the minimum both ends of the sweep must sit
	// for the curve to have been observed to have a bottom rather than to have
	// been assumed to.
	shoulderRatio = 1.5
)

// harness measures the writer's memory model for one row type.
//
// It is generic because the model is: `a` is per column per row group and `W` is
// per row, and neither has anything to do with what a row *means*. Pointing it at
// another row type is one instantiation and a pool of rows — which is what makes
// this an instrument rather than a fact about [record].
type harness[T any] struct {
	// columns is the number of leaves in T's schema, which is the C the model
	// divides the retained term by. It is taken from the caller rather than
	// counted here because it is the same number convert.go sizes the row group
	// with, and holding the two against each other is
	// TestTheColumnCountIsTheSchemaAndNotANumberWrittenDown's job.
	columns int

	// rowAt is the i'th row of an arbitrarily long extract. It must not allocate:
	// a generator that built a row per call would put its garbage on the same
	// heap the probe is reading, and the slope would be measuring the generator.
	rowAt func(i int) T
}

// liveHeap is the probe, and the first trap.
//
// **The thing being measured has to stay reachable across the GC.** The heap this
// reads is the live one, so a `runtime.GC()` with the writer already unreachable
// collects the entire retained footer and reports the memory of a program that
// has finished rather than one that is halfway through. #304 hit exactly that and
// got 72 KB where 247 MB was live — a number small enough to be believable and
// wrong by four orders of magnitude. It is not an error and nothing warns about
// it; the only defence is the [runtime.KeepAlive] below, which is why it is here
// and not at the call sites.
//
// Two collections rather than one: the first can leave memory reachable only from
// a finalizer queue, and the second clears it. ReadMemStats stops the world, so
// the reading is of a quiet heap either way.
func liveHeap(keep any) uint64 {
	runtime.GC()
	runtime.GC()

	var ms runtime.MemStats

	runtime.ReadMemStats(&ms)

	runtime.KeepAlive(keep)

	return ms.HeapAlloc
}

// troughOf is the total row count at which m row groups of r rows have closed and
// the open one holds exactly one row.
//
// See [peakOf] for why the phase is written this way rather than as m*r.
func troughOf(m, r int) int {
	return m*r + 1
}

// peakOf is the total row count at which m row groups of r rows have closed and
// the open one holds r-1 of them — one row short of the write that closes it,
// which is the fullest an open row group ever gets.
//
// This is the second trap. **A sample that lands at an arbitrary fill of the row
// group reads the buffered term at an arbitrary fraction of itself**, and the
// curve it draws is not the curve of the peak: it is the curve of wherever the
// sample happened to fall, which on a sweep that halves R every point is a
// different fraction at every point. The result still looks like a curve.
//
// The phase is expressed as m*r+1 and m*r+r-1 rather than as m*r for a reason
// that outlives the pin. parquet-go closes a full row group on the *next* write
// rather than on the one that fills it — writeRows returns ErrTooManyRowGroups
// when `remain <= 0` (writer.go), so after exactly m*r rows the m'th group is
// full and has not been flushed. A harness that assumed the other convention
// would read a full buffer where it wanted an empty one and vice versa, and would
// report `a` off by W*r a row group. At m*r+1 and m*r+r-1 both conventions agree:
// m groups closed, and one row or r-1 rows in the open one. The harness does not
// have to know which convention is in force, and does not break when it changes.
func peakOf(m, r int) int {
	return m*r + r - 1
}

// sink is where the written file goes, and it keeps only how many bytes it was
// given.
//
// Discarding the bytes is what makes the reading the *retained* heap and not the
// file: pages leave the heap when a row group closes, and what stays is the footer
// metadata that cannot be written until Close. That is precisely the term the
// model's first half describes.
//
// Counting them is what makes the two probes checkable, and that is worth the type
// over an io.Discard. Both probes rest on a claim about row groups closing — the
// `a` probe that m of them have, the W probe that none has — and with the bytes
// merely discarded there is nothing to tell "m row groups closed and their footers
// are retained" from "nothing ever flushed and the open group is still growing".
// The second fits a straight line too, and would report a plausible `a` that was
// really a fraction of W·R. A row group's chunks reach the sink when it closes and
// at no other time, so a byte count is exactly the observation that separates
// them.
type sink struct {
	n int64
}

func (s *sink) Write(p []byte) (int, error) {
	s.n += int64(len(p))

	return len(p), nil
}

// writer is a writer of T bounded at rowsPerRowGroup rows, and the sink counting
// what it has handed over.
func (h harness[T]) writer(rowsPerRowGroup int64) (*parquet.GenericWriter[T], *sink) {
	out := &sink{}

	return parquet.NewGenericWriter[T](out, parquet.MaxRowsPerRowGroup(rowsPerRowGroup)), out
}

// writeThrough hands w rows until it has been given exactly total of them, and
// returns the new count so the caller can carry on from a phase it has just
// sampled.
func (h harness[T]) writeThrough(w *parquet.GenericWriter[T], written, total int, buf []T) (int, error) {
	for written < total {
		n := min(len(buf), total-written)

		for i := range n {
			buf[i] = h.rowAt(written + i)
		}

		if _, err := w.Write(buf[:n]); err != nil {
			return written, fmt.Errorf("writing rows %d..%d: %w", written, written+n, err)
		}

		written += n
	}

	return written, nil
}

// reading is one sample of the live heap against whatever is being varied.
type reading struct {
	x, y float64
}

// retained samples the live heap at the trough phase for each of the given
// closed-row-group counts, over **one** writer.
//
// One writer and not one per sample, which is the whole reason the result is a
// slope. Everything that is not the footer — the column buffers' capacity, the
// schema, the test binary's own retained heap — is the same at every sample and
// cancels in the difference. So the fit does not have to know what the writer's
// fixed overhead is, and does not go wrong when it changes.
func (h harness[T]) retained(t *testing.T, groups []int) []reading {
	t.Helper()

	w, out := h.writer(retainedRowGroup)
	buf := make([]T, writeChunk)
	readings := make([]reading, 0, len(groups))
	written, flushed := 0, int64(0)

	for _, m := range groups {
		var err error

		written, err = h.writeThrough(w, written, troughOf(m, retainedRowGroup), buf)
		if err != nil {
			t.Fatalf("filling %d row groups of %d: %v", m, retainedRowGroup, err)
		}

		// The claim this whole probe rests on, and the sink is what makes it an
		// observation rather than an assumption: row groups have been closing. A
		// bound that stopped being enforced would leave one row group growing for
		// the whole probe, the fit would still be straight, and `a` would come back
		// a plausible fraction of W·R with nothing to have said so.
		if out.n <= flushed {
			t.Fatalf("at %d row groups of %d rows the writer has handed the sink %d bytes and it had %d before: a closed row group writes its column chunks, so a count that has not moved is a bound that is not being enforced and a slope that is not the footer",
				m, retainedRowGroup, out.n, flushed)
		}

		flushed = out.n

		readings = append(readings, reading{x: float64(m), y: float64(liveHeap(w))})
	}

	return readings
}

// buffered samples the live heap against the rows held in a row group that is
// never allowed to close.
//
// The bound is math.MaxInt64 — parquet-go's own default, and the one that makes a
// writer handed no option grow a single row group for the whole file. Here that
// is the point rather than the bug it is in a conversion: with nothing ever
// flushed there are no closed row groups, the retained term is zero, and the
// slope is all W.
//
// **W is a trend and not a step, and the samples are spread wide because of it.**
// What the open row group actually holds is allocated capacity: a column buffer
// grows by doubling, so it overshoots what is in it by up to a factor of two and
// stands still until the next doubling, and a page that reaches PageBufferSize
// spills and gives its buffer back — so the live heap here is a staircase that
// climbs, plateaus, and now and then falls. It was reproducible to the byte
// across repeated runs on one machine, which is what makes it a property of the
// writer rather than noise, and it is why this fit is thirty-two samples over a
// thirty-two-fold range rather than four over a fourfold one. It is also why the
// test below asserts only that this term grows and dominates the record, and
// leaves the neighbourhood of the number to be read off the log.
func (h harness[T]) buffered(t *testing.T, rows []int) []reading {
	t.Helper()

	w, out := h.writer(math.MaxInt64)
	buf := make([]T, writeChunk)
	readings := make([]reading, 0, len(rows))
	written := 0

	for _, r := range rows {
		var err error

		written, err = h.writeThrough(w, written, r, buf)
		if err != nil {
			t.Fatalf("buffering %d rows: %v", r, err)
		}

		// The mirror of the check in [harness.retained], and the claim this probe
		// rests on: *nothing* has been released. A row group's chunks reach the sink
		// when it closes and at no other time — a page that spills inside one goes
		// into the chunk buffer and stays on the heap — so a sink that has taken a
		// byte is a row group that closed, and the slope would be missing whatever
		// it took with it.
		if out.n != 0 {
			t.Fatalf("at %d buffered rows the writer has handed the sink %d bytes: nothing is meant to have been flushed under a bound of math.MaxInt64, so this slope is not W",
				r, out.n)
		}

		readings = append(readings, reading{x: float64(r), y: float64(liveHeap(w))})
	}

	return readings
}

// peak is the live heap at the peak phase of a run of records rows bounded at
// rowsPerRowGroup — the observed peak(N, R), one point of the curve.
//
// A fresh writer per point, because these are not a slope: each one is a whole
// run at its own R, and they are compared with each other rather than differenced.
//
// m is one short of records/rowsPerRowGroup so that the phase lands inside the
// run: peakOf(m, r) is (records/r)*r - 1, which is at most records-1 whatever r
// is. That is what makes the points of a sweep comparable — they are all runs of
// about the same N — and "about" is exact: the shortfall is records mod r + 1,
// which is one row when r divides records and at most r rows when it does not.
// The default sweep halves from N, so every r divides N and the shortfall is one
// row everywhere; an odd -memory.records gives up a little of that at the
// large-R end, where the retained term is smallest and least sensitive to it.
func (h harness[T]) peak(t *testing.T, rowsPerRowGroup, records int) uint64 {
	t.Helper()

	w, _ := h.writer(int64(rowsPerRowGroup))
	buf := make([]T, writeChunk)

	m := max(records/rowsPerRowGroup-1, 0)

	if _, err := h.writeThrough(w, 0, peakOf(m, rowsPerRowGroup), buf); err != nil {
		t.Fatalf("writing %d records at %d rows per row group: %v", records, rowsPerRowGroup, err)
	}

	return liveHeap(w)
}

// slopeOf is the least-squares slope of y against x.
func slopeOf(rs []reading) float64 {
	var sx, sy float64

	for _, r := range rs {
		sx += r.x
		sy += r.y
	}

	n := float64(len(rs))
	mx, my := sx/n, sy/n

	var num, den float64

	for _, r := range rs {
		num += (r.x - mx) * (r.y - my)
		den += (r.x - mx) * (r.x - mx)
	}

	// A single sample, or several taken at the same x, has no slope. Returning
	// num/den here would be NaN, and **NaN passes every gate below**: every
	// comparison against it is false, so a fit that does not exist would read as
	// linear, as positive, and as agreeing with the curve. That is the same shape
	// of failure as the two traps this file is written around — a plausible answer
	// rather than an error — so it is refused here, at the one place it can be.
	if den == 0 {
		panic("slopeOf: every sample is at the same x, so there is no slope to fit — a fit through one point is a probe that was never given a range")
	}

	return num / den
}

// chordOf is the slope of the straight line through the first and last reading.
//
// It is reported beside [slopeOf] rather than instead of it because the two
// disagree on a staircase, and the size of the disagreement is the honest width
// of the reading. See [harness.buffered].
func chordOf(rs []reading) float64 {
	first, last := rs[0], rs[len(rs)-1]

	return (last.y - first.y) / (last.x - first.x)
}

// halvesOf fits the lower and upper half of the samples separately, sharing the
// middle one so that the two halves cover the whole range between them with no
// gap at the join. On an even count the lower half carries the extra point.
func halvesOf(rs []reading) (lower, upper float64) {
	mid := len(rs) / 2

	return slopeOf(rs[:mid+1]), slopeOf(rs[mid:])
}

// assertGrows fails unless the term is larger at the end of the range than at the
// start, and reports whether it held.
//
// This is the whole of what is asserted about W, and the ceiling is deliberate.
// The staircase [harness.buffered] describes is reproducible and steep enough
// that the upper half of a fit over 512..16,384 rows slopes *negative* — a page
// spill giving back more than the rows in that window added — so a linearity gate
// on this term would fail on a writer that is behaving exactly as the model says.
// What the model needs from W and what a staircase can carry is the same
// statement: an open row group costs more the more rows are in it. The size of it
// is read off the log, between the least-squares slope and the chord.
func assertGrows(t *testing.T, what string, rs []reading) bool {
	t.Helper()

	if len(rs) < 2 {
		t.Errorf("%s: %d samples, and a term's shape is not visible in fewer than two", what, len(rs))

		return false
	}

	first, last := rs[0], rs[len(rs)-1]
	if last.y <= first.y {
		t.Errorf("%s: %.0f bytes at %.0f against %.0f at %.0f, and the term is meant to grow — a flat or falling reading over the whole range is either a probe that collected what it was measuring or a term that is not there at all",
			what, last.y, last.x, first.y, first.x)

		return false
	}

	return true
}

// assertLinear fails unless the term grows and the slope fitted over the lower
// half of the samples agrees with the slope over the upper half.
//
// Halves rather than consecutive pairs, and that is a finding rather than a
// preference. Live heap is not a smooth function of anything: a column buffer
// grows by doubling, so it stands still for a while and then jumps, and a page
// that spills is released, so it sometimes falls. A check on consecutive samples
// reads either of those as a broken model. What survives is the trend over a span
// wide enough to hold several doublings, which is what a half is chosen to be.
func assertLinear(t *testing.T, what string, rs []reading) {
	t.Helper()

	if !assertGrows(t, what, rs) {
		return
	}

	lower, upper := halvesOf(rs)
	if math.IsNaN(lower) || math.IsNaN(upper) {
		t.Errorf("%s: a half of the fit came back NaN, which every check below would read as agreement", what)

		return
	}

	if lower <= 0 || upper <= 0 {
		t.Errorf("%s: the halves of the fit slope %.0f and %.0f bytes, and a linear term does not change sign", what, lower, upper)

		return
	}

	if spread := max(lower/upper, upper/lower); spread > linearityTolerance {
		t.Errorf("%s: the fit slopes %.0f bytes over its lower half and %.0f over its upper, a factor of %.1f, and the model says this term is linear — a term that steepens like that has a shape the arithmetic in convert.go does not have",
			what, lower, upper, spread)
	}
}

// stepsOf is n multiples of step, ascending: the sample points of a fit written
// as the two numbers that describe them rather than as a literal list nobody can
// change one end of.
func stepsOf(step, n int) []int {
	out := make([]int, 0, n)

	for i := 1; i <= n; i++ {
		out = append(out, i*step)
	}

	return out
}

// poolOf is a ring of rows covering all eleven record types, which the probes
// draw from by index.
//
// By index rather than built per call, for the reason [harness.rowAt] gives: a
// generator allocating a row per call would leave its garbage on the heap the
// probe is about to read, and the slope would be measuring the generator rather
// than the writer.
//
// **A ring repeats, and a repeated value is a way of measuring `a` too small** —
// a column holding ten distinct values dictionary-encodes better than a real
// extract's and retains a narrower ColumnIndex, since the minimum and maximum are
// what the index keeps. That is the obvious objection to a harness built this way,
// so it was measured rather than argued: over rings of 74, 578, 2,306 and 9,218
// rows, `a` reads 970, 966, 964 and 964 bytes and W does not move at all. Sixty-four
// policy terms is chosen from the flat part of that, and the answer to "does the
// ring bias the reading" is a number rather than a hope.
func poolOf(t *testing.T) []record {
	t.Helper()

	recs := extract(poolPolicies, detailTypes)
	rows := make([]record, 0, len(recs))

	for i, rec := range recs {
		row, _, err := rowOf(rec)
		if err != nil {
			t.Fatalf("mapping pool record %d: %v", i, err)
		}

		rows = append(rows, row)
	}

	if len(rows) == 0 {
		t.Fatal("the row pool is empty: every probe below indexes it modulo its length, so an empty one is a divide by zero from inside the writer rather than a failure here")
	}

	return rows
}

// TestTheWriterMemoryModelHoldsItsShape measures a and W for [record] and draws
// the curve of peak against rows per row group, then asserts the shape of the
// model convert.go is sized by — never a byte count. See this file's package
// comment for why that line is drawn there.
func TestTheWriterMemoryModelHoldsItsShape(t *testing.T) {
	// Read first, so a flag the sweep cannot be drawn at is a sentence before any
	// measuring rather than a failure a second and a half into it.
	records := *memoryRecords
	if records < minRecords {
		t.Fatalf("-memory.records is %d and the curve needs at least %d: the sweep halves R %d times from N, and a sweep that runs out of points before then has no interior for a minimum to be found in",
			records, minRecords, sweepPoints)
	}

	pool := poolOf(t)
	h := harness[record]{
		columns: columns,
		rowAt:   func(i int) record { return pool[i%len(pool)] },
	}

	retained := h.retained(t, stepsOf(retainedGroupStep, retainedGroups))
	assertLinear(t, "what closed row groups retain", retained)

	a := slopeOf(retained) / float64(h.columns)

	buffered := h.buffered(t, stepsOf(bufferedRowStep, bufferedSamples))
	if !assertGrows(t, "what an open row group holds", buffered) {
		return
	}

	w := slopeOf(buffered)

	t.Logf("a = %.0f B per column per row group over %d..%d closed row groups of %d rows (convert.go commits %d)",
		a, retainedGroupStep, retainedGroupStep*retainedGroups, retainedRowGroup, retainedPerColumnPerRowGroup)
	t.Logf("W = %.0f B per row over %d columns, %.0f taken as the chord — %.1fx the %d-byte record (convert.go commits %d = 5*C + %d + %d)",
		w, h.columns, chordOf(buffered), w/float64(recordBytes), recordBytes, bufferedPerRow, recordBytes, bufferedOverheadPerRow)

	// The wide sparse claim, and the only one of these that puts a measurement
	// beside a number. That number is LRECL — a property of the copybook, not of
	// this machine — and the claim is an ordering rather than a size: every leaf of
	// this schema is an optional column, so a row pays five bytes for each of the
	// 197 whether or not there is a value under it, and a row therefore costs the
	// writer more than the record it was read from. It would fail on the day
	// definition levels stopped being held per row per column, which is the day the
	// arithmetic in convert.go stops being right.
	//
	// The gate is the bare ordering and not a margin, though the margin is wide —
	// 5.3x on the machine this was written on, and the ratio is logged above so a
	// reading creeping toward the record is visible long before it crosses. A
	// margin here would be picking a number to hold a machine to, which is the
	// thing this file does not do.
	if w <= float64(recordBytes) {
		t.Errorf("W measured %.0f B a row against a %d-byte record: this table is wide and sparse, so a row is meant to cost the writer more than the record it came from — %d optional columns of definition levels do not vanish because a row has no value under them",
			w, recordBytes, h.columns)
	}

	sweep := sweepOf(records)
	peaks := make([]uint64, len(sweep))

	for i, r := range sweep {
		peaks[i] = h.peak(t, r, records)
	}

	at := argmin(peaks)

	t.Logf("peak against rows per row group, at N = %d records:", records)

	for i, r := range sweep {
		mark := ""
		if i == at {
			mark = "  <- observed minimum"
		}

		t.Logf("  R = %7d   peak = %8.1f MB%s", r, float64(peaks[i])/(1<<20), mark)
	}

	// The bottom is *observed*: it is the smallest reading of the sweep, and the
	// two assertions below are that there is one. An argmin at either end is a
	// sweep that has only seen one side of the curve, and a bottom the ends are
	// level with is a curve that is flat — either way the minimum would be an
	// artefact of where the sweep stopped rather than a property of the model.
	if at == 0 || at == len(sweep)-1 {
		t.Fatalf("the peak curve's minimum is at R = %d, an end of the sweep: the two terms pull opposite ways and the minimum is meant to be interior, so an end is a sweep that never crossed the bottom",
			sweep[at])
	}

	low := float64(peaks[at])

	for _, end := range []int{0, len(sweep) - 1} {
		if got := float64(peaks[end]) / low; got < shoulderRatio {
			t.Errorf("R = %d peaks at %.1fx the minimum, want at least %.1fx: a curve whose ends are level with its bottom has no bottom to size a row group at",
				sweep[end], got, shoulderRatio)
		}
	}

	// And the observed bottom is where the rule says it is. This is what ties the
	// two measurements above to the curve: R* is computed from the a and W this
	// run measured, not from the constants convert.go commits, so the check is
	// that the model predicts its own measurements rather than that this machine
	// agrees with the one #304 ran on.
	// Refused rather than computed, because math.Sqrt of a negative is NaN and NaN
	// compares false against everything — so the one check that ties the two
	// measurements to the curve would report nothing exactly when the measurements
	// were broken.
	if a <= 0 || w <= 0 {
		t.Fatalf("a is %.0f and W is %.0f, and R* = sqrt(a*C*N/W) is not a number for either: the check that the rule predicts its own measurements cannot be made, and a NaN would pass it silently",
			a, w)
	}

	star := math.Sqrt(a * float64(h.columns) * float64(records) / w)
	off := math.Max(float64(sweep[at])/star, star/float64(sweep[at]))

	t.Logf("R* = sqrt(a*C*N/W) = %.0f rows from the measured a and W; the sweep's minimum is at %d, a factor of %.2f away",
		star, sweep[at], off)

	if off > curveTolerance {
		t.Errorf("the observed minimum is at R = %d and the rule predicts R* = %.0f, a factor of %.2f: the sizing rule is meant to pick the neighbourhood of the bottom, and this far out it is picking a different curve",
			sweep[at], star, off)
	}
}

// sweepOf is the rows-per-row-group the curve is drawn at: N, halving.
//
// It starts at N because that is the degenerate right-hand end — one row group
// for the whole file, which is what parquet-go does when it is handed no bound —
// and every point below it closes at least one row group. Halving rather than
// stepping keeps the sweep even in log R, which is the axis the curve is
// symmetric in.
func sweepOf(records int) []int {
	sweep := make([]int, 0, sweepPoints)

	for r := records; len(sweep) < sweepPoints && r > 1; r /= 2 {
		sweep = append(sweep, r)
	}

	return sweep
}

// argmin is the index of the smallest reading.
func argmin(peaks []uint64) int {
	at := 0

	for i, p := range peaks {
		if p < peaks[at] {
			at = i
		}
	}

	return at
}
