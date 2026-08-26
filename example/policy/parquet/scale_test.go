// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The run at scale.
//
// # What this is, and what it is not
//
// [memory_test.go] beside this file is the *instrument*: it measures `a` and `W`
// for a row type by probing the writer at two controlled phases, it is generic
// over that row type, and it is one file for the reason its own comment gives.
// Nothing here measures either constant that way.
//
// This is the **run**: a sweep of the real conversion over `-rows-per-row-group`,
// at a record count where the retained term is not a rounding error. It is
// written twice, once beside each conversion, because a sweep of a conversion has
// to be in the module that conversion is in — [convert] is an unexported function
// of `package main` and there is no third place either module can see. The other
// copy is [scale_test.go] beside the ledger conversion, and the two are the same
// shape and are meant to be read side by side.
//
// [memory_test.go]: memory_test.go
// [scale_test.go]: ../../ledger/parquet/scale_test.go
//
// # Where it runs — the decision
//
// **Not in CI.** Every test here is skipped unless `-scale.records` is passed, so
// `dagger call ci` compiles this file and runs none of it, and the runtime of an
// ordinary pull request does not move. What CI keeps is what it already had:
// [TestTheWriterMemoryModelHoldsItsShape] at its default N, which is a couple of
// seconds, and the two constant checks in convert_test.go.
//
// It is skipped rather than put behind a build tag because a tagged file is one
// the linter and the compiler in CI do not see either, and a scale run that has
// stopped compiling is discovered by the person who least wants to discover it.
// The flag defaults to zero and zero means "not asked for".
//
// Run it before a release, and whenever the parquet-go pin, the schema or
// [rowsPerRowGroup] moves:
//
//	go test -run TestPeak -v -scale.records=1000000 -timeout=60m
//
// **Budget an hour and watch the first point.** This schema is 197 columns wide
// and each point of the sweep converts N records, so the run is seven whole
// conversions; the production run #304 came from took 23 minutes for one. That
// asymmetry with the ledger's copy — which is fourteen columns and finishes in
// seconds — is not incidental. It is the same factor that makes this the example
// that has to be sized rather than assumed.
package main

import (
	"flag"
	"io"
	"math"
	"testing"

	"github.com/Zaba505/cpybkc/example/policy/policy"
)

// scaleRecords is N: how many records each point of the sweep converts.
//
// Zero means the scale run was not asked for, which is what makes every test in
// this file a skip under an ordinary `go test ./...`. See this file's package
// comment for why that is the decision rather than a build tag.
//
// The `scale.` prefix is the same defence [memoryRecords] documents: a test file
// cannot register on a FlagSet of its own, so this shares the global one with
// anything the command under test declares at package scope. convert.go's flags
// live on a [flag.FlagSet] of its own and cannot collide.
var scaleRecords = flag.Int("scale.records", 0,
	"records converted at each point of the peak sweep; unset means the scale run was not asked for and every test here skips")

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
	// heap. [liveHeap] is handed nothing to keep alive here, because the writer
	// this sweep measures is a local of [convert] and this file is on the other
	// side of a call from it. If Go's stack liveness ever stopped keeping it
	// across the collection the readings would go flat — the same fixed overhead
	// at every point — and a flat sweep fails here and at the interior-minimum
	// check above it. A probe reading a program that has finished cannot draw a
	// U.
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
	// committed constants predict, **counted in steps of the sweep**.
	//
	// Two steps, which is the same allowance [curveTolerance] next door makes as
	// a factor of four: the curve is flat where it matters, two steps is a 2.13x
	// penalty, and the bottom two or three points of a real sweep sit within
	// noise of each other.
	//
	// **Counted in steps and not as a factor**, and that is a correction rather
	// than a style. The sweep is centred on R* and the two ends already Fatalf,
	// so the argmin can only be one, two or three steps out and the factor can
	// only be about 2, 4 or 8 — but sweepAround divides an integer three times
	// before doubling, so the low side comes out a hair over its factor and the
	// high side a hair under. A float comparison at exactly 4.0 therefore fired
	// on one side and never on the other, which is a check that looked like it
	// held and did not.
	scaleTolerance = 2

	// recordsPerPolicy is how many records one policy term contributes: itself
	// and one detail of each of the eight types, which is the shape [extract]
	// builds and [reconcile] counts.
	recordsPerPolicy = 1 + detailTypes
)

// scaleSource is a well-formed policy extract of the given size, generated a
// record at a time, and it is where the reading is taken from.
//
// **No dataset is committed and none is written to disk.** convert_test.go builds
// its fixtures as a `[]policy.Record` through [extract]; this is the same records
// three orders of magnitude further along and produced one at a time rather than
// as a slice, because a slice of nine million records is a second copy of the
// file on the heap the probe is about to read.
//
// The conversion reads through [recordSource], so a source that hands over
// records directly is the conversion's own input and not a shortcut around it.
// Unlike the ledger extract, nothing in this layout bounds how many records it
// can describe: what [reconcile] holds the trailer to is two *counts* and two
// identifiers, and FTR-POLICY-COUNT is PIC S9(9) — so the fixture runs out of
// patience long before it runs out of trailer.
//
// The probe fires from [scaleSource.Next], because Next is the only place in a
// conversion's loop a caller has. Entering it having already handed over k
// records is exactly the moment k rows have been written and none is in flight —
// every record of this extract is a row, including the header and the trailer,
// which is what one merged table means.
type scaleSource struct {
	// policies is how many policy terms the extract carries, and sent is how
	// many records have been handed over.
	policies int32
	sent     int32

	// done says the trailer has gone too.
	done bool

	// out is the sink the conversion is writing into, and flushed is how many
	// bytes it had taken when the probe fired.
	//
	// **The snapshot is the whole point, and reading the sink afterwards was the
	// bug.** Both halves of the model rest on row groups having closed by the
	// peak phase, and a byte count taken after `convert` returns is the whole
	// file — non-zero for every successful run, including one where nothing
	// flushed until Close. That check could not fail. A row group's column chunks
	// reach the sink when it closes and at no other time, so the count *at the
	// probe* is exactly the observation that separates "m row groups closed and
	// their footers are retained" from "one row group is still growing".
	out     *sink
	flushed int64

	// phase is the number of rows written at which the reading is taken, and
	// peak is that reading. See [scalePhase].
	phase int32
	peak  uint64
}

// records is how many records this extract is, which is the N of the run: the
// header, every policy term with its details, and the trailer.
func (s *scaleSource) records() int32 {
	return 2 + s.policies*recordsPerPolicy
}

func (s *scaleSource) Next() (policy.Record, error) {
	if s.sent == s.phase {
		s.peak = liveHeap(nil)
		s.flushed = s.out.n
	}

	body := s.records() - 2

	switch {
	case s.sent == 0:
		s.sent++

		return &policy.PxFileHeader{
			FhdRecordType:    "000",
			FhdExtractName:   "PXTRACT DAILY",
			FhdCycleDate:     cycleDate,
			FhdCycleTime:     cycleTime,
			FhdCarrier:       carrier,
			FhdRegion:        "SE",
			FhdSourceSystem:  "POLADMIN",
			FhdRunNumber:     runNumber,
			FhdFormatVersion: 3,
		}, nil
	case s.sent <= body:
		// The i'th record of the body, of policy p, which is the p'th term
		// followed by its eight details — the order [extract] writes and the
		// order a real extract carries.
		i := s.sent - 1
		p := i/recordsPerPolicy + 1
		d := i % recordsPerPolicy

		s.sent++

		if d == 0 {
			return policyRecord(p), nil
		}

		return detail(p, d-1), nil
	case !s.done:
		s.done = true
		s.sent++

		return &policy.PxFileTrailer{
			FtrRecordType:       "999",
			FtrCycleDate:        cycleDate,
			FtrPolicyCount:      s.policies,
			FtrDetailCount:      s.policies * detailTypes,
			FtrWrittenPremium:   ftrWrittenPremium,
			FtrPaidLoss:         ftrPaidLoss,
			FtrHashPolicyNumber: 987654321098765,
			FtrRunNumber:        runNumber,
		}, nil
	}

	return nil, io.EOF
}

// scalePhase is the row count at which the open row group is fullest: m whole
// row groups closed and the open one holding r-1 rows, one row short of the
// write that closes it.
//
// A sample taken at an arbitrary fill reads the buffered term at an arbitrary
// fraction of itself, and on a sweep whose R changes at every point that is a
// different fraction every time — the result still looks like a curve, and is
// the curve of where the samples happened to fall. m is one short of n/r so the
// phase lands inside the run whatever r is.
//
// It is written as m*r + r - 1 rather than as m*r for the reason [peakOf] gives
// at length: parquet-go closes a full row group on the *next* write rather than
// on the one that fills it, and at this phase both conventions agree about what
// is closed and what is held.
func scalePhase(n, r int32) int32 {
	m := max(n/r-1, 0)

	return m*r + r - 1
}

// peakAt converts an extract of policies terms at r rows a row group and returns
// the live heap at [scalePhase], along with the bytes the writer handed over.
func peakAt(t *testing.T, policies, r int32) (uint64, int64) {
	t.Helper()

	src := &scaleSource{policies: policies, out: &sink{}}
	src.phase = scalePhase(src.records(), r)

	if err := convert(src, src.out, int(r)); err != nil {
		t.Fatalf("converting %d records at %d rows a row group: %v", src.records(), r, err)
	}

	if src.peak == 0 {
		t.Fatalf("the probe never fired at %d rows a row group: the phase is %d rows and the run wrote %d", r, src.phase, src.records())
	}

	return src.peak, src.flushed
}

// TestPeakAgainstTheRowGroupAtScale is the run.
//
// It draws peak against rows per row group at a record count where the retained
// term is not a rounding error, fits the model to the curve, and holds the bottom
// against the R* the constants convert.go commits predict. Everything it finds is
// logged; what it *asserts* is the shape, for the reason
// [TestTheWriterMemoryModelHoldsItsShape] gives — the byte counts are a property
// of this machine, this Go version and this parquet-go pin, and a pipeline that
// gated on one would go red on a dependency bump that changed nothing.
//
// The sweep is centred on the predicted R* rather than halving down from N, and
// that is the one place this differs from the harness beside it. Halving from N
// starts at one row group for the whole file, which at sixteen thousand records
// is 22 MB and at a million is 1.3 GB of open row group — the very thing this run
// exists to avoid holding. Centring bounds the top of the sweep at 8*R*, whose
// buffered term is eight times the optimum's and no more.
func TestPeakAgainstTheRowGroupAtScale(t *testing.T) {
	policies := scalePolicies(t)

	n := (&scaleSource{policies: policies}).records()
	star := rowsPerRowGroupAt(int64(n))
	sweep := sweepAround(star)

	// Checked here rather than in the flag reader because [minScaleRecords] is a
	// fact about this schema's own constants, and reading it beside the R* they
	// produce is what makes the refusal legible.
	if int(n) < minScaleRecords() {
		t.Fatalf("-scale.records is %d and the smallest N this curve can be read at is about %d: below it the sweep's largest row group approaches the whole extract and the writer's fixed overhead is most of every reading, so the U flattens and what fails is the flag rather than the model",
			n, minScaleRecords())
	}

	t.Logf("N = %d records over %d policy terms, C = %d columns; the committed constants put R* at %d rows a row group",
		n, policies, columns, star)
	t.Logf("the default -rows-per-row-group is %d, which is R* at maxRecords = %d and not at this N",
		rowsPerRowGroup, maxRecords)

	peaks := make([]uint64, len(sweep))

	for i, r := range sweep {
		peak, flushed := peakAt(t, policies, r)
		peaks[i] = peak

		// The bytes the sink had taken **when the probe fired**, which is the
		// observation that says row groups were closing by then. Zero means
		// nothing had flushed and the reading is one growing row group rather
		// than the sum of the two terms this curve is drawn from.
		if flushed == 0 {
			t.Fatalf("R = %d had flushed no bytes at the peak phase: a row group's column chunks reach the sink when it closes and at no other time, so nothing having closed by then means the reading is one open row group and not the retained footer this curve is half made of",
				r)
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
	// the whole file exists to test at a record count the harness's default N
	// cannot reach: a and W are what size this conversion's row group, and this
	// is the curve they are held against.
	off := math.Max(float64(sweep[at])/float64(star), float64(star)/float64(sweep[at]))
	steps := at - scalePoints/2

	if steps < 0 {
		steps = -steps
	}

	t.Logf("R* from the committed constants is %d rows; the observed minimum is at %d, %d steps of the sweep and a factor of %.2f away",
		star, sweep[at], steps, off)

	if steps > scaleTolerance {
		t.Errorf("the observed minimum is at R = %d and the committed constants predict R* = %d, %d steps of the sweep away (a factor of %.2f): a = %d B and W = %d B are what size this conversion's row group, and this far out they are sizing a different curve",
			sweep[at], star, steps, off, retainedPerColumnPerRowGroup, bufferedPerRow)
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
// It is a log and never a failure, which is the same line
// [TestTheWriterMemoryModelHoldsItsShape] draws and for the same reason.
//
// **It is a weaker measurement than that harness's, and it is taken somewhere the
// harness cannot go.** The harness isolates each term by choosing the phase and
// the row group it probes at, and gets a slope of one term with the other held to
// a single row; this has seven readings of their sum and separates them by
// fitting. What it is for is that it is taken over the *conversion*, at N, with
// nothing arranged — so a reading here that has walked away from the committed
// constants is the first sign that the arrangement next door has stopped standing
// in for the real thing.
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

	// Three parameters need four points to be a fit rather than a solution — and
	// four is one residual degree of freedom, which is a fit with nothing left
	// over to disagree with. Read a reading taken over four points as weaker
	// than the same reading over six, and the count is logged above so it can be.
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
// two axes are orders of magnitude apart, so each is normalised by its own mean
// before the solve and the coefficients are scaled back afterwards — a 3x3
// determinant over raw row-group counts and raw row counts is where the
// conditioning goes, not the arithmetic.
//
// Fitting the whole curve rather than reading a slope off each end is not
// fussiness. Each end is contaminated by the term that is not being measured, and
// the contamination is signed: towards small R the buffered term is *falling* as
// the fitted axis rises, so it subtracts from the slope, and the same at the
// other end. Both constants come back low, and they come back low together, which
// is the shape of a wrong answer that looks like a consistent one.
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
	// reading at one R, or a sweep of fewer points than parameters. Cramer's rule
	// would hand back +Inf or NaN, and **NaN passes every comparison**, so a fit
	// that does not exist would be logged as one that agreed.
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

// rowsPerRowGroupAt is R* = sqrt(a*C*N/W) for n records: where the accumulated
// footer and the held row group are the same size, from the constants convert.go
// commits.
//
// It is the *rule*, evaluated at the N in hand, and it is not [rowsPerRowGroup] —
// that is this same expression at [maxRecords], which is the one N a single
// default can be optimal at. The gap between them is the cost the default pays on
// a smaller extract, README.md works it at a million records, and
// `-rows-per-row-group` is what an adopter closes it with.
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

// scalePolicies reads -scale.records and turns it into a whole number of policy
// terms, skipping the test when the flag was not passed.
//
// A whole number of them, because a policy term and its eight details are one
// unit of this extract and [reconcile] counts the two grains separately: an
// extract stopping halfway through a policy's details is one no reader of this
// layout would produce and one the trailer could still describe, which is a
// fixture pretending to be a file.
func scalePolicies(t *testing.T) int32 {
	t.Helper()

	n := *scaleRecords

	if n <= 0 {
		t.Skip("-scale.records was not passed: this is the run a person takes before a release, and CI does not take it — see this file's package comment")
	}

	return int32((n - 2) / recordsPerPolicy)
}
