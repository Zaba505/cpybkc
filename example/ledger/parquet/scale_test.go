// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The run at scale, and the one wall this conversion can be driven into.
//
// # What this is, and what it is not
//
// [memory_test.go] beside the sibling conversion is the *instrument*: it measures
// `a` and `W` for a row type by probing the writer at two controlled phases, it
// is generic over that row type, and it stays one file for the reason its README
// gives — a second copy of a measurement is a second measurement to keep in step.
// Nothing here measures either constant that way.
//
// This is the **run**: a sweep of the real conversion over its own row-group
// bound, at a record count where the retained term is not a rounding error. It
// is written twice, once beside each conversion, because a sweep of a conversion
// has to be in the module that conversion is in — `convert` is an unexported
// function of `package main` and there is no third place either module can see.
// The two files are the same shape and are meant to be read side by side.
//
// [memory_test.go]: ../../policy/parquet/memory_test.go
//
// # Where it runs — the decision
//
// **Not in CI.** Every test here is skipped unless `-scale.records` is passed, so
// `dagger call ci` compiles this file and runs none of it, and the runtime of an
// ordinary pull request does not move. What CI keeps is what it already had: the
// shape assertions in the sibling's memory harness, which take a couple of
// seconds at their default N, and the cheap constant checks in convert_test.go.
//
// It is skipped rather than put behind a build tag because a tagged file is one
// the linter and the compiler in CI do not see either, and a scale run that has
// stopped compiling is discovered by the person who least wants to discover it.
// The flag defaults to zero and zero means "not asked for".
//
// Run it before a release, and whenever the parquet-go pin, the schema or the row
// group moves:
//
//	go test -run TestPeak -v -scale.records=2000000 -timeout=30m
//	go test -run TestTheRowGroupCeiling -v -scale.records=1 -timeout=30m
//
// The second is not a function of `-scale.records` — it is 32,768 rows at one row
// a group whatever N says — but it is part of the same run and is gated behind
// the same flag, so `-run` is how one of them is taken on its own.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"runtime"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/example/ledger/ledger"
	"github.com/parquet-go/parquet-go"
)

// scaleRecords is N: how many postings each point of the sweep converts.
//
// Zero means the scale run was not asked for, which is what makes every test in
// this file a skip under an ordinary `go test ./...`. See this file's package
// comment for why that is the decision rather than a build tag.
//
// The `scale.` prefix is the same defence [memoryRecords] documents next door: a
// test file cannot register on a FlagSet of its own, so this shares the global
// one with anything the command under test declares at package scope. convert.go's
// flags live on a [flag.FlagSet] of its own and cannot collide.
var scaleRecords = flag.Int("scale.records", 0,
	"postings converted at each point of the peak sweep; unset means the scale run was not asked for and every test here skips")

const (
	// scalePoints is how many rows-per-row-group the sweep is drawn at, and
	// scaleSpread is the factor between neighbouring points.
	//
	// Seven points a factor of two apart span a factor of 64, which is wide
	// enough that both ends are well up the sides of the curve: being off by k
	// costs (k + 1/k)/2, so the ends of this sweep sit around 4x the bottom.
	scalePoints = 7
	scaleSpread = 2

	// scaleShoulderRatio is how far above the minimum each end of the sweep has
	// to sit for the curve to have been *observed* to have a bottom.
	//
	// It is also the whole defence against the probe having read a collected
	// heap. [liveHeap] cannot hold this conversion's writer across the
	// collection — it is a local of [convert] and this file is on the other side
	// of a call from it — so the argument that it stays live is an argument
	// about Go's stack liveness rather than a [runtime.KeepAlive]. If that
	// argument is ever wrong the readings go flat, because what would be left is
	// the same fixed overhead at every point, and a flat sweep fails here and at
	// the interior-minimum check above it. A probe reading a program that has
	// finished cannot draw a U.
	//
	// The two ends do not get the same number, and the asymmetry is the knee
	// below rather than a fudge. The retained side is linear all the way down, so
	// the left-hand end climbs as steeply as the model says; the buffered side
	// flattens above [kneeRows], so the right-hand end climbs more slowly than
	// W*R and a sweep centred well above the knee cannot reach 1.5x there. It was
	// measured: at 4.5 M records the wide sparse conversion's right-hand end read
	// 1.47x while its left read 3.3x.
	scaleShoulderRatio         = 1.5
	scaleBufferedShoulderRatio = 1.25

	// scalePenalty is what the rule's own R* may cost against the best reading of
	// the sweep, as a factor of peak.
	//
	// **This is the assertion that the recommended row group is the measured
	// best**, and it is the right one because the distance between two points is
	// not what an adopter pays. The curve is flat at the bottom: a sweep whose
	// argmin is one step from R* may have the two within a fraction of a percent
	// of each other, and reporting that as "a factor of two out" is asserting
	// which of two indistinguishable points won. What is checked is the height.
	//
	// 1.25 is (k + 1/k)/2 at k = 2, which is what the model itself says one step
	// of this sweep costs. A rule landing further off the bottom than its own
	// arithmetic allows for is a rule that has stopped picking the neighbourhood.
	scalePenalty = 1.25

	// pageBufferBytes is parquet-go's DefaultPageBufferSize (config.go:29), and
	// it is the reason the model's second term is linear only up to a point.
	//
	// A column's buffer is flushed into a page as soon as it reaches 98% of this
	// (writer.go:262, writer.go:795), so a column never holds more than a quarter
	// of a megabyte of *raw* buffer: past that, an open row group carries encoded
	// pages instead, which are smaller. W*R therefore over-states what a large row
	// group costs, and it over-states it in the safe direction — a bound derived
	// from it holds less memory than the arithmetic promised, not more.
	//
	// This is the one thing the harness at its default N cannot see. Its W probe
	// runs to 16,384 rows, which is well inside the linear regime for both of
	// these schemas; the knee is at [kneeRows], and finding it is what running at
	// millions is for.
	pageBufferBytes = 256 << 10

	// scaleTolerance is how far the observed argmin may sit from the R* the
	// committed constants predict, as a factor.
	//
	// Four, which is two steps of this sweep, and the right order because the
	// curve is flat where it matters: being off by k costs (k + 1/k)/2, so two
	// steps is 2.13x and the bottom two or three points of a real sweep sit
	// within noise of each other. The first run of this test read 2.5 MB at both
	// 2,744 and 5,488 rows a group; tightening this would be asserting which of
	// two indistinguishable points won.
	scaleTolerance = 4.0

	// maxScaleRecords is the largest N this fixture can describe, and the limit
	// is TRL-NET rather than anything about memory.
	//
	// [netOf] sums what the postings store, and the two constants convert_test.go
	// carries net 1,777,777,778,878 per debit-and-credit pair — so an int64
	// overflows at about 10.4 million postings and the conversion would fail its
	// own TRL-NET reconciliation for a reason that has nothing to do with the
	// run. Ten million is under it with room to spare.
	//
	// It is worth saying that this is *lower* than the record count README.md
	// names as the memory ceiling at the derived bound. A fixture is not the
	// thing it stands in for, and this one runs out of trailer before it runs
	// out of budget.
	maxScaleRecords = 10_000_000

	// ceilingRowGroups is parquet-go's MaxRowGroups, math.MaxInt16 (limits.go:29):
	// the most row groups a Parquet *file* can hold, which is the format's number
	// and not the library's.
	ceilingRowGroups = 32767
)

// liveHeap is the probe: the live heap, taken with the world stopped.
//
// Two collections rather than one, because the first can leave memory reachable
// only from a finalizer queue and the second clears it. It takes no argument to
// keep alive, and that is the difference between this probe and [memory_test.go]'s
// — see [scaleShoulderRatio] for what stands in for the [runtime.KeepAlive] that
// cannot be written from here, and why a failure of that argument cannot pass
// silently.
//
// [memory_test.go]: ../../policy/parquet/memory_test.go
func liveHeap() uint64 {
	runtime.GC()
	runtime.GC()

	var ms runtime.MemStats

	runtime.ReadMemStats(&ms)

	return ms.HeapAlloc
}

// sink counts the bytes the conversion hands over and keeps none of them.
//
// Discarding them is what makes the reading the *retained* heap rather than the
// file: pages leave the heap when a row group closes and what stays behind is
// footer metadata that cannot be written until Close. Counting them is what tells
// "m row groups have closed and their footers are retained" from "nothing has
// flushed and one row group is still growing" — the two draw the same line and
// only the first is the thing being measured.
type sink struct {
	n int64
}

func (s *sink) Write(p []byte) (int, error) {
	s.n += int64(len(p))

	return len(p), nil
}

// scaleSource is a well-formed ledger extract of n postings, generated a record
// at a time, and it is where the reading is taken from.
//
// **No dataset is committed and none is written to disk.** convert_test.go builds
// its fixtures through [ledger.Writer]; this is the same mechanism three orders
// of magnitude further along, minus the encoding — the conversion reads through
// [recordSource], so a source that hands over records directly is the conversion's
// own input and not a shortcut around it.
//
// Not encoding them is also what lets N exceed what this layout can describe.
// HDR-COUNT is PIC 9(3) and TRL-COUNT is PIC 9(6), so a real ledger extract
// carries at most 999 postings and could not carry a million however it was
// written. What is being measured here is what the *schema* costs the writer at a
// record count a wider count field would admit, which is the question every
// adopter of this example has and this example's own dataset cannot answer.
// [ledger.LedgerHeader.HdrCount] is left at what the layout allows and is ignored
// by the conversion, which is HDR-COUNT's documented status here; TRL-COUNT is
// carried at n because [convert] reconciles against it.
//
// The probe fires from [scaleSource.Next], because Next is the only place in a
// conversion's loop a caller has. Entering it having already handed over k
// postings is exactly the moment k rows have been written and none is in flight,
// which is what makes the phase below a phase and not an average.
type scaleSource struct {
	// postings is n, and net is the TRL-NET they sum to.
	postings int32
	net      int64

	// sent is how many postings have been handed over, and done says the trailer
	// has gone too.
	sent int32
	done bool

	// phase is the number of rows written at which the reading is taken, and
	// peak is that reading. See [peakPhase].
	phase int32
	peak  uint64
}

func (s *scaleSource) Next() (ledger.Record, error) {
	if s.sent == 0 {
		s.sent++

		return &ledger.LedgerHeader{
			HdrType:     "01",
			HdrLedgerId: "GL-MAIN",
			HdrPeriod:   202601,
			HdrCurrency: "USD",
			// Not n. PIC 9(3) tops out at 999 and this conversion does not
			// read the field; see the type comment.
			HdrCount: 999,
		}, nil
	}

	// One posting has been handed over for every sent above the header, so
	// sent-1 rows have been written when this is entered.
	written := s.sent - 1

	if written == s.phase {
		s.peak = liveHeap()
	}

	if written < s.postings {
		s.sent++

		return posting(written + 1), nil
	}

	if s.done {
		return nil, io.EOF
	}

	s.done = true

	return &ledger.LedgerTrailer{TrlType: "99", TrlCount: s.postings, TrlNet: s.net}, nil
}

// peakPhase is the row count at which the open row group is fullest: m whole row
// groups closed and the open one holding r-1 rows, one row short of the write
// that closes it.
//
// A sample taken at an arbitrary fill reads the buffered term at an arbitrary
// fraction of itself, and on a sweep whose R changes at every point that is a
// different fraction every time — the result still looks like a curve, and is
// the curve of where the samples happened to fall. m is one short of n/r so the
// phase lands inside the run whatever r is.
//
// It is written as m*r + r - 1 rather than as m*r for the reason
// [memory_test.go] gives at length: parquet-go closes a full row group on the
// *next* write rather than on the one that fills it, and at this phase both
// conventions agree about what is closed and what is held.
//
// [memory_test.go]: ../../policy/parquet/memory_test.go
func peakPhase(n, r int32) int32 {
	m := max(n/r-1, 0)

	return m*r + r - 1
}

// peakAt converts n postings at r rows a row group and returns the live heap at
// [peakPhase], along with the bytes the writer handed over.
func peakAt(t *testing.T, n, r int32, net int64) (uint64, int64) {
	t.Helper()

	src := &scaleSource{postings: n, net: net, phase: peakPhase(n, r)}
	out := &sink{}

	if err := convert(src, out, int(r)); err != nil {
		t.Fatalf("converting %d postings at %d rows a row group: %v", n, r, err)
	}

	if src.peak == 0 {
		t.Fatalf("the probe never fired at %d rows a row group: the phase is %d rows and the run wrote %d", r, src.phase, n)
	}

	return src.peak, out.n
}

// TestPeakAgainstTheRowGroupAtScale is the run.
//
// It draws peak against rows per row group at a record count where the retained
// term is not a rounding error, reads `a` and `W` back off the two ends of the
// curve, and holds the bottom against the R* the constants convert.go commits
// predict. Everything it finds is logged; what it *asserts* is the shape, for
// the reason the sibling's harness gives — the byte counts are a property of
// this machine, this Go version and this parquet-go pin, and a pipeline that
// gated on one would go red on a dependency bump that changed nothing.
//
// The sweep is centred on the predicted R* rather than halving down from N, and
// that is the one place this differs from the harness next door. Halving from N
// starts at one row group for the whole file, which at sixteen thousand records
// is a few megabytes and at two million is the very thing this run exists to
// avoid holding. Centring bounds the top of the sweep at 8*R*, whose buffered
// term is eight times the optimum's half-budget and no more.
func TestPeakAgainstTheRowGroupAtScale(t *testing.T) {
	n := scaleN(t)
	net := netOf(n)

	star := rowsPerRowGroupAt(int64(n))
	sweep := sweepAround(star)

	// Checked here rather than in the flag reader because [minScaleRecords] is a
	// fact about this schema's own constants, and reading it beside the R* they
	// produce is what makes the refusal legible.
	if int(n) < minScaleRecords() {
		t.Fatalf("-scale.records is %d and the smallest N this curve can be read at is about %d: below it the sweep's largest row group approaches the whole extract and the writer's fixed overhead is most of every reading, so the U flattens and what fails is the flag rather than the model",
			n, minScaleRecords())
	}

	t.Logf("N = %d postings, C = %d columns; the committed constants put R* at %d rows a row group",
		n, columns, star)

	peaks := make([]uint64, len(sweep))

	for i, r := range sweep {
		peak, bytes := peakAt(t, n, r, net)
		peaks[i] = peak

		if bytes == 0 {
			t.Fatalf("R = %d wrote %d rows and the writer handed over no bytes: no row group closed, so what was measured is one open row group and not the retained footer this curve is half made of",
				r, n)
		}
	}

	at := argmin(peaks)

	t.Logf("peak against rows per row group, over the real conversion:")

	for i, r := range sweep {
		mark := ""
		if i == at {
			mark = "  <- observed minimum"
		}

		t.Logf("  R = %8d   peak = %8.1f MB%s", r, float64(peaks[i])/(1<<20), mark)
	}

	if at == 0 || at == len(sweep)-1 {
		t.Fatalf("the minimum is at R = %d, an end of the sweep: the two terms pull opposite ways and the bottom is meant to be interior, so an end is a sweep that never crossed it",
			sweep[at])
	}

	low := float64(peaks[at])

	for _, end := range []struct {
		at   int
		want float64
		side string
	}{
		{0, scaleShoulderRatio, "retained"},
		{len(sweep) - 1, scaleBufferedShoulderRatio, "buffered"},
	} {
		if got := float64(peaks[end.at]) / low; got < end.want {
			t.Errorf("R = %d peaks at %.2fx the minimum on the %s side, want at least %.2fx: a curve whose ends are level with its bottom has no bottom to size a row group at — and a probe that read a collected heap would draw exactly this",
				sweep[end.at], got, end.side, end.want)
		}
	}

	// The bottom is where the committed constants say it is. This is the claim
	// the whole file exists to test: R* is derived from `a` and `W`, both of
	// which are measurements taken on a different row type, and this is where
	// they are confirmed against a curve drawn by the conversion itself.
	off := math.Max(float64(sweep[at])/float64(star), float64(star)/float64(sweep[at]))

	t.Logf("R* from the committed constants is %d rows; the observed minimum is at %d, a factor of %.2f away",
		star, sweep[at], off)

	if off > scaleTolerance {
		t.Errorf("the observed minimum is at R = %d and the committed constants predict R* = %d, a factor of %.2f: a = %d B and W = %d B are what size this conversion's row group, and this far out they are sizing a different curve",
			sweep[at], star, off, retainedPerColumnPerRowGroup, bufferedPerRow)
	}

	// And what the rule's own point costs against the best of the sweep, which is
	// the number an adopter actually pays and the one this run is here to settle.
	// The sweep is centred on R*, so the middle point *is* the rule's answer.
	mid := scalePoints / 2
	penalty := float64(peaks[mid]) / float64(peaks[at])

	t.Logf("the rule's R* = %d rows peaks at %.1f MB against the sweep's best %.1f MB at R = %d, a penalty of %.3fx",
		sweep[mid], float64(peaks[mid])/(1<<20), float64(peaks[at])/(1<<20), sweep[at], penalty)

	if penalty > scalePenalty {
		t.Errorf("the rule puts R* at %d and that costs %.3fx the best reading of the sweep, which is at R = %d: the rule is meant to pick the neighbourhood of the bottom, and %.2fx is further off it than the curve's own (k + 1/k)/2 allows for one step",
			sweep[mid], penalty, sweep[at], penalty)
	}

	reportConstants(t, n, sweep, peaks)
}

// reportConstants fits the model to the curve the sweep drew and logs what it
// reads beside the numbers convert.go commits.
//
// It is a log and never a failure, which is the same line the sibling's harness
// draws and for the same reason: the byte counts are a property of this machine,
// this Go version and this parquet-go pin.
//
// **It is a weaker measurement than that harness's, and it is taken somewhere
// the harness cannot go.** The harness isolates each term by choosing the phase
// and the row group it probes at, and gets a slope of one term with the other
// held to a single row; this has seven readings of their sum and separates them
// by fitting. What it is for is that it is taken over the *conversion*, at N,
// with nothing arranged — so a reading here that has walked away from the
// committed constants is the first sign that the arrangement next door has
// stopped standing in for the real thing.
//
// Reading the two slopes off the two ends of the curve was the obvious way to do
// this and it is wrong in a direction worth recording. Each end is contaminated
// by the term that is not being measured, and the contamination is signed:
// towards small R the buffered term is *falling* as the fitted axis rises, so it
// subtracts from the slope, and the same at the other end. The first run of this
// test read a = 818 B and W = 87 B that way against 1,024 and 95 committed —
// both low, both low for the same reason, and neither a reading of anything.
func reportConstants(t *testing.T, n int32, sweep []int32, peaks []uint64) {
	t.Helper()

	knee := kneeRows()

	groups := make([]float64, 0, len(sweep))
	rows := make([]float64, 0, len(sweep))
	heap := make([]float64, 0, len(sweep))

	// Only the points the model is a model of. Above [kneeRows] the row group
	// holds encoded pages rather than raw column buffers, so peak rises far more
	// slowly than W*R — fitting a straight line through those points drags W down
	// and the fixed term up, and the result is a fit of nothing. The first run of
	// this test at 1.8 M records read W = 475 B a row that way, over a sweep whose
	// linear points read 1,436.
	for i, r := range sweep {
		if r > knee {
			continue
		}

		groups = append(groups, float64(n)/float64(r))
		rows = append(rows, float64(r))
		heap = append(heap, float64(peaks[i]))
	}

	t.Logf("the buffered term is linear below about %d rows a group, where one column's share of a row fills parquet-go's %d KB page buffer; %d of the sweep's %d points are under it",
		knee, pageBufferBytes>>10, len(heap), len(sweep))

	// Three parameters need four points to be a fit rather than a solution.
	if len(heap) < 4 {
		t.Logf("the model is not fitted: %d of the sweep's points sit below the knee and the fit has three parameters, so what came back would be an exact solution of an under-determined system rather than a reading",
			len(heap))

		return
	}

	// The buffered coefficient is fitted and deliberately not reported; see below.
	retained, _, fixed := fitModel(groups, rows, heap)

	a := retained / float64(columns)

	t.Logf("fitted below the knee: a = %.0f B per column per row group (convert.go commits %d), fixed overhead = %.1f MB",
		a, retainedPerColumnPerRowGroup, fixed/(1<<20))

	// **Only `a` comes back from this.** The two axes of the fit are not equally
	// well behaved, and the asymmetry is a property of what is being measured
	// rather than of the arithmetic — so W is fitted, because the terms cannot be
	// separated without it, and then thrown away. A number this file distrusts is
	// worse published than absent: somebody would use it.
	//
	// The retained axis is clean: closed row groups accumulate footer that nothing
	// releases, so the readings sit on a line, and this fit agrees with the
	// harness's direct probe to within a percent or two.
	//
	// The buffered axis is a **staircase**, for the reason [harness.buffered]
	// gives next door: a column buffer grows by doubling, so live heap steps
	// rather than sloping and a fit over a handful of points a factor of two apart
	// measures whichever doubling it straddled. The page-buffer knee above sits on
	// top of that. Both are why W is measured over thirty-two closely spaced
	// samples by an instrument that holds the row group open, and not here.
	//
	// So the note below is on `a` alone, and it is a note. A constant *below* the
	// reading is the unsafe way to be wrong — [maxRecords] divides by a*C, so
	// under-stating it claims room that is not there — but this stays a log for
	// the same reason every other byte count here does: it is a property of the
	// machine, the Go version and the parquet-go pin, and gating on it would go
	// red on a dependency bump that changed nothing an adopter cares about.
	//
	// It is also a reading that needs a run to be worth anything. Near
	// [minScaleRecords] the writer's fixed overhead is most of every point and the
	// fit is mostly measuring that: at the smallest N the sweep can be drawn at,
	// `a` comes back a quarter high. What gates this file is the shape — the
	// interior minimum, the two shoulders, and the penalty at R* — and none of
	// those is a byte count.
	if a > float64(retainedPerColumnPerRowGroup) {
		t.Logf("a fits at %.0f B against the %d committed, which is the unsafe direction: the retained term is what makes a conversion stop fitting its budget, and a constant below the reading puts maxRecords past where this really fits. At a small -scale.records this is the fixed overhead and not the term; take it again at millions before acting on it",
			a, retainedPerColumnPerRowGroup)
	}
}

// kneeRows is the row group above which the buffered term stops being linear:
// where one column's share of a row, W/C, has filled [pageBufferBytes].
//
// It is an estimate and a rough one — W/C is an average over columns of very
// different widths, and the widest column reaches the page size first — so what
// it is used for is choosing which points of the sweep the model may be fitted
// over, and never for an assertion.
func kneeRows() int32 {
	return pageBufferBytes * columns / bufferedPerRow
}

// fitModel is the least-squares fit of peak = retained*groups + buffered*rows +
// fixed, over every point of the sweep.
//
// Three basis functions and three normal equations, solved by Cramer's rule. The
// two axes are two and five orders of magnitude apart, so each is normalised by
// its own mean before the solve and the coefficients are scaled back afterwards
// — a 3x3 determinant over raw row-group counts and raw row counts is where the
// conditioning goes, not the arithmetic.
func fitModel(groups, rows, heap []float64) (retained, buffered, fixed float64) {
	gm, rm := meanOf(groups), meanOf(rows)

	basis := [3][]float64{make([]float64, len(heap)), make([]float64, len(heap)), make([]float64, len(heap))}

	for i := range heap {
		basis[0][i] = groups[i] / gm
		basis[1][i] = rows[i] / rm
		basis[2][i] = 1
	}

	var m [3][3]float64
	var v [3]float64

	for i := range 3 {
		for j := range 3 {
			m[i][j] = dotOf(basis[i], basis[j])
		}

		v[i] = dotOf(basis[i], heap)
	}

	det := determinantOf(m)

	// A singular system is a sweep whose points do not span the model — every
	// reading at one R, or a sweep of fewer points than parameters. Cramer's
	// rule would hand back +Inf or NaN, and **NaN passes every comparison**, so
	// a fit that does not exist would be logged as one that agreed.
	if det == 0 {
		panic("fitModel: the normal equations are singular, so the sweep does not determine the model — a fit through fewer points than parameters is not a fit")
	}

	c := [3]float64{}

	for i := range 3 {
		swapped := m
		for row := range 3 {
			swapped[row][i] = v[row]
		}

		c[i] = determinantOf(swapped) / det
	}

	return c[0] / gm, c[1] / rm, c[2]
}

func meanOf(xs []float64) float64 {
	var sum float64

	for _, x := range xs {
		sum += x
	}

	return sum / float64(len(xs))
}

func dotOf(xs, ys []float64) float64 {
	var sum float64

	for i, x := range xs {
		sum += x * ys[i]
	}

	return sum
}

func determinantOf(m [3][3]float64) float64 {
	return m[0][0]*(m[1][1]*m[2][2]-m[1][2]*m[2][1]) -
		m[0][1]*(m[1][0]*m[2][2]-m[1][2]*m[2][0]) +
		m[0][2]*(m[1][0]*m[2][1]-m[1][1]*m[2][0])
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

// rowsPerRowGroupAt is R* = sqrt(a*C*N/W) for n records: where the accumulated
// footer and the held row group are the same size, from the constants convert.go
// commits.
//
// It is the *rule*, evaluated at the N in hand, and it is not
// [derivedRowsPerRowGroup] — that is this same expression at
// [derivedMaxRecords], which is the one N a single default can be optimal at. The
// gap between them is the cost a default pays on a smaller extract, and it is
// what the sibling conversion's `-rows-per-row-group` flag exists to let an
// adopter close.
func rowsPerRowGroupAt(n int64) int32 {
	return int32(math.Sqrt(float64(retainedPerColumnPerRowGroup) * float64(columns) * float64(n) / float64(bufferedPerRow)))
}

// minScaleRecords is the smallest N this curve can be *read* at, which is four
// times the smallest one it can be drawn at.
//
// Drawn: the sweep's top point is 8*R*, and every point has to fit inside the run,
// so 8*sqrt(a*C*N/W) <= N — which rearranges to N >= 64*a*C/W. Below that the
// largest row group is bigger than the whole extract, the peak phase lands past
// the last record, and the probe never fires.
//
// **Read is the larger floor, and it is the writer's fixed overhead that makes it
// larger.** Every reading of the sweep carries a couple of megabytes of column
// buffer capacity that is neither of the model's two terms, and that offset is the
// same at every point — so at a small N it is most of the minimum and it flattens
// the U the sweep exists to show. Right at the drawing floor both schemas read
// their retained shoulder at 1.46x against the 1.50x this file asks for, which is
// a failure about N and not about the model.
//
// Four, because peak* is 2*sqrt(a*C*W*N): quadrupling N doubles the signal while
// leaving the offset where it is. It was measured rather than argued — both
// conversions fail at N = 10,000 and pass from 12,000, and four times the drawing
// floor is about 39,000 for this schema, which is margin rather than a knife edge.
// [memoryRecords] next door defaults the way it does for the same reason.
//
// It is a coincidence, and worth naming so nobody reads one for the other, that
// this comes out equal to [kneeRows] on both schemas: 4*64*a is 262,144 exactly
// when `a` is a kilobyte, which is [pageBufferBytes]. The two have nothing to do
// with each other — one is where the sweep becomes legible, the other is where
// parquet-go stops holding raw buffers — and an `a` measured at 969 would part
// them.
//
// The refusal happens before anything is converted, and it names the flag. Without
// it the run starts, spends its time, and reports "the probe never fired" or a
// shoulder a hair under its ratio — true sentences about the instrument in place
// of the one sentence the caller needs, which is that they asked for a sweep too
// small to read.
func minScaleRecords() int {
	return 4 * 64 * retainedPerColumnPerRowGroup * columns / bufferedPerRow
}

// sweepAround is the rows-per-row-group the curve is drawn at: scalePoints of
// them, a factor of scaleSpread apart, centred on star.
func sweepAround(star int32) []int32 {
	sweep := make([]int32, 0, scalePoints)
	r := star

	for range scalePoints / 2 {
		r /= scaleSpread
	}

	for range scalePoints {
		sweep = append(sweep, max(r, 1))
		r *= scaleSpread
	}

	return sweep
}

// scaleN reads -scale.records, skipping the test when it was not passed and
// refusing a value the fixture cannot describe.
func scaleN(t *testing.T) int32 {
	t.Helper()

	n := *scaleRecords

	if n <= 0 {
		t.Skip("-scale.records was not passed: this is the run a person takes before a release, and CI does not take it — see this file's package comment")
	}

	if n > maxScaleRecords {
		t.Fatalf("-scale.records is %d and the fixture tops out at %d: %d postings sum to a TRL-NET this layout's own trailer field can hold and an int64 cannot go far past, so a larger run would fail its own reconciliation for a reason that is about the fixture",
			n, maxScaleRecords, maxScaleRecords)
	}

	return int32(n)
}

// TestTheRowGroupCeilingIsReachedAndSaysWhichLimitItWas drives the conversion
// into parquet-go's row-group cap for real.
//
// **This is the one place in either example where the cap is reached rather than
// simulated.** The sibling conversion tests its [tooManyRowGroups] as a function,
// handing it an error to wrap, and says plainly that no fixture there can provoke
// one: 32,767 row groups over 197 columns retain about 6.6 GB. This schema is
// fourteen columns wide, so the same 32,767 row groups retain around 470 MB —
// enough that this does not belong in CI, and little enough that a person can run
// it. That difference is the section it belongs to: the ceiling is a function of
// how small the row group is, and what makes it reachable here is one row a
// group.
//
// What it holds the diagnostic to is what #304 needed and did not get. parquet-go
// says "the limit of 32767 row groups has been reached" and the conversion used
// to wrap that as "writing the posting row" — a true sentence naming neither the
// cap nor the thing that moves it. The report has to carry the cap, the bound in
// force, and the record ceiling the two of them imply.
func TestTheRowGroupCeilingIsReachedAndSaysWhichLimitItWas(t *testing.T) {
	if *scaleRecords <= 0 {
		t.Skip("-scale.records was not passed: reaching the cap writes 32,768 row groups and retains a few hundred megabytes, which is not a thing to do on every pull request")
	}

	// One row a group, which is the smallest bound there is and the fastest way
	// to the cap: the 32,768th row is the row that asks for a 32,768th row
	// group. The postings themselves are the fixture's, so the row is the row
	// this conversion really writes.
	const rows = int32(1)

	n := int32(ceilingRowGroups) + 1

	src := &scaleSource{postings: n, net: netOf(n), phase: -1}

	err := convert(src, &sink{}, int(rows))
	if err == nil {
		t.Fatalf("%d postings at %d row a row group converted without complaint: a Parquet file holds at most %d row groups, so this run asked for one more than the format has",
			n, rows, ceilingRowGroups)
	}

	if !errors.Is(err, parquet.ErrTooManyRowGroups) {
		t.Fatalf("the conversion failed with %v, and the cap is what it was driven into: an error that is not parquet.ErrTooManyRowGroups means this test is measuring something else",
			err)
	}

	for _, want := range []string{
		// The cap, which is the format's number.
		fmt.Sprint(ceilingRowGroups),
		// The bound in force, which is the thing that moves it.
		fmt.Sprintf("bounded at %d rows", rows),
		// And the ceiling the two imply, which is the number an adopter
		// compares against the extract in front of them.
		fmt.Sprint(int64(rows) * ceilingRowGroups),
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the report is %q, and it does not carry %q: #304 got the cap and nothing that moves it, which is the half of this that was missing", err, want)
		}
	}

	t.Logf("the cap was reached at %d rows of one a group, and reported as: %v", n, err)
}
