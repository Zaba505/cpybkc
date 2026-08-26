// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Command parquet converts the ledger extract in the directory above this one
// into the Parquet table a data platform would query it as.
//
// It is a worked conversion and a reference, not a recommendation and not a
// library. Every decision it makes is one an adopter has to make for themselves
// on their own layout; README.md beside this file states each of them, says why
// this one was taken, and marks each place where another adopter would reasonably
// differ. An adopter who reads it and does something else has used it correctly.
//
// It is package main rather than a package with a Convert function so that
// nothing here can be imported. A conversion is a schema design, and a schema
// design is not a function of the source — so there is nothing to reuse, and a
// package that offered something would be inviting exactly the reuse this whole
// example argues against.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Zaba505/cpybkc/example/ledger/ledger"
	"github.com/parquet-go/parquet-go"
)

// rowsPerRowGroup is how many posting rows a row group holds before parquet-go
// closes it and opens the next.
//
// It is the whole of what bounds this conversion's memory, and it is small
// deliberately: sixty-four is chosen so that the ledger this example carries —
// which HDR-COUNT's PIC 9(3) bounds at 999 postings — fills fifteen row groups
// and leaves a sixteenth partial one, so that a bound nothing enforced, or a
// last group nothing wrote, is visible in a test rather than theoretical.
//
// **It is a test number and not a size**, and the number to copy is
// [derivedRowsPerRowGroup] below, which is arithmetic over the same model the
// sibling conversion is sized by. What sixty-four costs is in the constants
// under it: at this bound the conversion stops fitting [memoryBudget] at
// [maxRecords] postings, which is five orders of magnitude below where the
// derived bound stops fitting. It is affordable here for exactly one reason —
// this layout cannot describe an extract long enough to care.
//
// It has to be said out loud, because parquet-go's default is not a bound at
// all: DefaultMaxRowsPerRowGroup is math.MaxInt64 (config.go:37), so a writer
// given no option grows one row group for the whole file. That — and not a
// caller holding no slice of its own — is what makes a conversion buffer the
// file it claims to stream.
const rowsPerRowGroup = 64

// The memory model this schema is measured against, and the numbers README.md's
// "Memory" quotes. Every one of them is either a property of the layout or a
// measurement, and scale_test.go is the run that takes the measurements over
// this conversion at a record count where the retained term is not a rounding
// error.
//
// The model is the sibling conversion's, unchanged, and its README is the
// derivation:
//
//	peak(N, R) ~= a*C*(N/R) + W*R
//
// The first term is footer already accumulated — a ColumnChunk, a ColumnIndex
// and an OffsetIndex per column per closed row group, none of it writable until
// Close. The second is the row group still being held. They pull opposite ways,
// so there is a bottom, and R* = sqrt(a*C*N/W) is where it is.
const (
	// columns is C: the leaves of [postingRow]'s schema. The two optional groups
	// and the required one are structure and get no column of their own.
	//
	// It is written down rather than counted because it is an input to constant
	// arithmetic. TestTheColumnCountIsTheSchemaAndNotANumberWrittenDown opens a
	// converted file and holds this against the leaves it finds, so a column
	// added to the row and not to this constant fails there rather than leaving
	// the arithmetic sized for a table that no longer exists.
	columns = 14

	// optionalColumns is how many of those fourteen sit under an optional group:
	// the three leaves of PST-DEBIT and the three of PST-CREDIT.
	//
	// It is a property of the *layout* and not of the copybook. `posting.cpy`
	// admits six combinations of its two redefined runs; `ledger.sexpr` names
	// two, and what makes PST-DEBIT and PST-CREDIT optional is that a posting is
	// described by one of them and not the other. PST-TAIL-REF is the only
	// description the layout names of the second run, so every posting carries
	// it and its two leaves are required.
	//
	// **This is where a wide sparse table differs from this one.** The sibling
	// schema's 197 leaves are optional to a leaf, so a row there pays a
	// definition level for every column it has; a posting pays for six of
	// fourteen. That single ratio is most of why W below is 95 bytes and the
	// sibling's is 1,256.
	optionalColumns = 6

	// definitionLevelBytes is the five bytes an open row group holds per row per
	// *optional* column, whether or not there is a value under it: parquet-go's
	// optionalColumnBuffer keeps `rows []int32` and `definitionLevels []byte`
	// (column_buffer_optional.go:22).
	definitionLevelBytes = 5

	// postingBytes is what a posting record is: PST-ACCOUNT through PST-TAIL,
	// fifty bytes.
	//
	// It is the record and not LRECL, which is the one place this differs from
	// the sibling's arithmetic. `ledger.sexpr` frames this dataset RECFM=VB at
	// LRECL 512, and 512 is the longest record the dataset admits rather than
	// the length of any record in it — a fixed-block dataset's LRECL is every
	// record's length and a variable-block one's is a ceiling. Taking the
	// ceiling here would over-estimate W by a factor of ten, which sizes the row
	// group ten times too small and puts the default a factor of ten off the
	// bottom of the curve.
	postingBytes = 50

	// bufferedOverheadPerRow is what a row costs an open row group beyond its
	// values and its definition levels — #304's "about 15 bytes a record for the
	// buffered form", measured as the intercept of a padding sweep whose slope
	// was 1.02.
	bufferedOverheadPerRow = 15

	// bufferedPerRow is W: what one row costs the open row group.
	//
	// scale_test.go reads it back off the large-R end of the peak curve and logs
	// it beside this constant, so a reading that has drifted is visible in a run
	// rather than in an incident.
	bufferedPerRow = definitionLevelBytes*optionalColumns + postingBytes + bufferedOverheadPerRow

	// retainedPerColumnPerRowGroup is a: what one column of one closed row group
	// holds until Close, in bytes.
	//
	// #304 measured 975, 988 and 943 across schemas of 4, 16 and 32 columns —
	// flat to 5% over an eight-fold range — and the sibling conversion's harness
	// reads 969 on a 197-column one. It is not a function of the schema's width,
	// which is what lets a kilobyte stand for it here without a harness of this
	// module's own; it *is* a function of how wide the values are, and no item
	// in `posting.cpy` is wider than fifteen bytes.
	retainedPerColumnPerRowGroup = 1024

	// memoryBudget is what the arithmetic below is against, and it is the only
	// input here that is a choice rather than a reading. 256 MiB is an ordinary
	// container limit for a batch job, and it is the same number the sibling
	// conversion uses so the two are comparable.
	//
	// It is a budget and not a bound: nothing here enforces it, and the Go
	// runtime will not infer it from a cgroup. See the sibling README's
	// "GOMEMLIMIT".
	memoryBudget = 256 << 20

	// derivedRowsPerRowGroup is what this conversion's bound would be if it were
	// derived rather than chosen: the equal-terms row group at the largest
	// extract the budget admits, which is memoryBudget/2 bytes of open row group.
	//
	// **It is the number to copy, and it is not what this command runs at.**
	// Nothing reads it but README.md and the tests, and that is deliberate: a
	// worked example whose bound was 1.4 million rows could not show a partial
	// last row group over a 999-posting dataset, which is the property #272 got
	// wrong and the property [rowsPerRowGroup] exists to keep visible.
	derivedRowsPerRowGroup = memoryBudget / 2 / bufferedPerRow

	// maxRecords is the record count at which this conversion stops fitting
	// memoryBudget **at the bound it actually runs**: the budget less the open
	// row group, divided by what each closed row group retains.
	//
	// The retained term is linear in N, so this number exists for every budget
	// and every schema, and a conversion that fits today has a record count at
	// which it stops. Ours is about 1.2 million postings — and HDR-COUNT's
	// PIC 9(3) stops a real ledger extract at 999, so this conversion cannot be
	// handed a dataset that reaches it. That is a fact about this layout and not
	// a reason not to know the number; the copybook whose count field is six
	// digits wide, or nine, is the next one an adopter meets.
	maxRecords = (memoryBudget - bufferedPerRow*rowsPerRowGroup) * rowsPerRowGroup /
		(retainedPerColumnPerRowGroup * columns)

	// derivedMaxRecords is the same ceiling at [derivedRowsPerRowGroup], which
	// is where the two terms are equal and each is half the budget.
	//
	// About 13 billion postings, against 1.2 million at sixty-four. Peak grows
	// as sqrt(N) and the bound is the whole of the difference, which is the
	// clearest statement this file has of what a row group sized by arithmetic
	// buys over one sized to make a test legible.
	derivedMaxRecords = (memoryBudget - bufferedPerRow*derivedRowsPerRowGroup) * derivedRowsPerRowGroup /
		(retainedPerColumnPerRowGroup * columns)
)

// postingRow is the `posting` table, and it is the only table: one row per
// posting.
//
// A Parquet file carries exactly one schema and this extract has two grains, so
// the file-level one had to go somewhere. Its identifying fields are
// denormalized onto every row here; its summaries — TRL-COUNT and TRL-NET — are
// reconciled by [convert] and never stored; and HDR-COUNT is the reader's
// business rather than this conversion's. README.md says why a second table is
// the wrong home for them.
//
// The record types the layout names are one table rather than one each, and a
// run the layout names more than one description of becomes one optional column
// per description — which is what keeps the record type recoverable from a row.
//
// The shape of this struct is a function of the **layout** and not of the
// copybook. `posting.cpy` admits six combinations; `ledger.sexpr` names the two
// the extract carries, and the four it leaves out are the ones described by
// PST-BODY or PST-TAIL — the base descriptions of the two redefined runs, which
// a file a mainframe produced does not hold. So there is no `pst_body` and no
// `pst_tail` column: a column for a description no record of this file is ever
// described by would be null on every row of every extract.
type postingRow struct {
	HdrLedgerID string `parquet:"hdr_ledger_id"`
	HdrPeriod   int32  `parquet:"hdr_period"`
	HdrCurrency string `parquet:"hdr_currency"`

	PstAccount  string `parquet:"pst_account"`
	PstSequence int32  `parquet:"pst_sequence"`
	PstType     string `parquet:"pst_type"`

	// The first redefined run, PST-BODY. The layout names two of its three
	// descriptions, so exactly one of these two is present on any row and
	// which one is a function of pst_type.
	PstDebit  *debitBody  `parquet:"pst_debit"`
	PstCredit *creditBody `parquet:"pst_credit"`

	// The second run, PST-TAIL. The layout names one description of it —
	// PST-TAIL-REF — so every posting carries it and the column is required
	// rather than optional. An optional column here would be one a query has
	// to null-check on a file where it is never null, and its record type
	// would look like something a row was still choosing between.
	PstTailRef tailRef `parquet:"pst_tail_ref"`
}

// debitBody is PST-DEBIT, nested rather than flattened.
type debitBody struct {
	PdbCostCentre string `parquet:"pdb_cost_centre"`

	// PdbAmount is PDB-AMOUNT, PIC S9(11)V99 COMP-3: thirteen digits, two after
	// the point, already unscaled in the generated struct.
	PdbAmount int64  `parquet:"pdb_amount,decimal(2:13)"`
	PdbMemo   string `parquet:"pdb_memo"`
}

// creditBody is PST-CREDIT, nested rather than flattened.
type creditBody struct {
	PcrSource string `parquet:"pcr_source"`

	// PcrAmount is PCR-AMOUNT, PIC S9(9)V99 COMP-3: eleven digits, two after
	// the point, already unscaled in the generated struct.
	PcrAmount    int64  `parquet:"pcr_amount,decimal(2:11)"`
	PcrReference string `parquet:"pcr_reference"`
}

// tailRef is PST-TAIL-REF, nested rather than flattened.
type tailRef struct {
	PtrBatch int32 `parquet:"ptr_batch"`
	PtrLine  int32 `parquet:"ptr_line"`
}

// recordSource is the one record at a time a ledger extract is read as.
//
// [ledger.Reader] is what satisfies it, and the interface exists so that a test
// can hand this conversion a record the automaton would never admit — which is
// how "a mapping error fails the conversion" is asserted rather than asserted
// about a function nothing calls.
//
// io.EOF means the end of the file and carries **no** record, which is
// [ledger.Reader.Next]'s own contract. A source that returned its last record
// alongside io.EOF would have that row dropped here, so an implementation of
// this interface owes the same promise.
type recordSource interface {
	Next() (ledger.Record, error)
}

func main() {
	err := run(os.Args[1:], os.Stderr)
	if err == nil {
		return
	}

	if msg := err.Error(); msg != "" {
		fmt.Fprintln(os.Stderr, msg)
	}

	os.Exit(1)
}

// run converts the dataset named by -in into the Parquet file named by -out,
// creating the directory that file sits in if it is not there.
//
// What a failed run leaves behind is write's business; see there.
func run(args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("parquet", flag.ContinueOnError)
	flags.SetOutput(stderr)

	in := flags.String("in", "", "the ledger extract to convert")
	// -out names the file and not a directory. With two tables it had to name a
	// directory, because the conversion was choosing both filenames; with one
	// there is no name for it to choose, and a flag that still named a
	// directory would be inventing `posting.parquet` inside it for the caller
	// to then go and find.
	out := flags.String("out", "posting.parquet", "the Parquet file to write")

	if err := flags.Parse(args); err != nil {
		// The flag set has already written its message and the usage to stderr,
		// and -h is a request that succeeded rather than a failure. Returning
		// either would have main print it a second time and exit non-zero for
		// having answered the question it was asked.
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return errAlreadyReported
	}

	if *in == "" {
		return errors.New("-in names the ledger extract to convert, and is required")
	}

	src, err := os.Open(*in)
	if err != nil {
		return err
	}

	// The input is read to the end and never written, so there is nothing its
	// Close can report that a caller could act on.
	defer func() { _ = src.Close() }()

	r, err := ledger.NewReader(src, ledger.Encoding())
	if err != nil {
		return err
	}

	// -out named a directory while this conversion wrote two files, so the
	// invocation an adopter already has hands over one that exists. That
	// reaches os.Create as "is a directory", which is a true sentence about the
	// wrong thing: the mistake is in the flag, and the fix is a filename rather
	// than a permission or a disk. So the flag checks its own argument, and
	// this is the only output-side failure worth telling apart by hand —
	// the rest are ordinary I/O and read as such.
	//
	// A -out that is not a directory is taken at its word, extension or no
	// extension. There is nothing to check there: a path that names no file yet
	// is exactly what this flag is for, and refusing one that does not end in
	// .parquet would be this conversion having an opinion about filenames.
	if info, err := os.Stat(*out); err == nil && info.IsDir() {
		return fmt.Errorf("-out is %s, which is a directory: it named the directory to write two files into while this conversion wrote two, and it names the file now that it writes one — try -out %s", *out, filepath.Join(*out, "posting.parquet"))
	}

	// The directory -out sits in is created rather than assumed, so that the
	// invocation README.md documents runs as it is written on a machine that
	// has never run it. The file itself is os.Create's business below.
	if err := os.MkdirAll(filepath.Dir(*out), 0o750); err != nil {
		return err
	}

	// Both paths, because what write reports can be a fact about either of
	// them, and a wrapper naming only the input reads as a bad input file
	// whatever actually went wrong.
	if err := write(r, *out, rowsPerRowGroup); err != nil {
		return fmt.Errorf("converting %s into %s: %w", *in, *out, err)
	}

	return nil
}

// errAlreadyReported stands for a flag error the flag set has already printed.
// main exits non-zero on it and says nothing further.
var errAlreadyReported = errors.New("")

// write creates the table and converts into it.
//
// **path is clobbered whether or not this succeeds.** os.Create truncates, and a
// failed conversion then removes what it truncated — so a run that fails against
// the path an earlier successful run wrote destroys that output, and leaves no
// file rather than a short one. That is the deliberate half: a conversion that
// fails returns before the footer is written, and a Parquet file with no footer
// is bytes that read as corruption rather than as a run somebody has to repeat,
// so the path goes. The destructive half is the ordinary cost of an output path
// that is opened for writing, and it is said out loud here because the default
// -out is a bare posting.parquet in the working directory, which makes a second
// run over a bad extract an easy way to meet it.
//
// The file is closed whatever happens, and a failure to close is joined to
// whatever the conversion reported rather than replacing it: a full disk shows
// up here and nowhere else.
func write(src recordSource, path string, rows int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}

	err = errors.Join(convert(src, f, rows), f.Close())
	if err == nil {
		return nil
	}

	return errors.Join(err, remove(path))
}

// remove deletes a path this run created, treating an absent one as done rather
// than as a failure.
//
// The caller is undoing its own writes, so a file that is not there is the state
// it wanted — and an ENOENT joined onto the real diagnostic would bury it under
// a line about a file nobody was looking for.
func remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	return nil
}

// convert reads every record of src once and writes the posting table, bounding
// the row group at rows records.
//
// One pass, and the file is never held: what this carries at any moment is the
// header, the one row it has just mapped, the row group parquet-go is buffering
// behind it, and two running totals. That is the whole of its footprint, and
// README.md states it that way rather than as "constant".
//
// **rows is a parameter and not the constant, and there is no flag behind it.**
// [rowsPerRowGroup] is what [run] passes and is the only value this command ever
// converts at; the sibling conversion's `-rows-per-row-group` is a flag because
// its default is derived and an adopter re-derives it, and this one's is a test
// number nobody should be encouraged to tune. What the parameter buys is that
// peak memory can be *measured* against it — the curve in README.md's "Memory"
// is a sweep of this argument over the real conversion, taken by scale_test.go
// — and the precedent is [recordSource] two types up: a parameter exists here so
// that a test can drive the conversion, rather than so that a caller can.
func convert(src recordSource, w io.Writer, rows int) error {
	// The bound is the writer's rather than this loop's, and stating it is the
	// whole of what keeps the row group finite. parquet.MaxRowsPerRowGroup is
	// enforced inside GenericWriter.Write: the row-group writer returns
	// ErrTooManyRowGroups once it is at the cap (writer.go:1036), Write catches
	// exactly that error, closes the group and carries on with the rows it has
	// left (writer.go:273-277), and Close writes the last partial one. So a row
	// goes over as it is produced and there is nothing here to flush.
	postings := parquet.NewGenericWriter[postingRow](w, parquet.MaxRowsPerRowGroup(int64(rows)))

	var hdr *ledger.LedgerHeader
	var trl *ledger.LedgerTrailer

	// ordinal is which record of the file has been read, counting from one.
	// ledger.Reader keeps one for its own diagnostics and does not export it, so
	// a caller that wants to say where it failed counts its own — and this one
	// is a diagnostic and never a column. See README.md, "No key is minted".
	ordinal := 0

	// row is the one-row slice a mapped posting is handed over in, reused
	// across records. It is the argument to a call and not a batch: what is in
	// it has been written before the next record is read, and Write copies what
	// it takes before it returns — see postingRowOf.
	row := make([]postingRow, 1)

	// written is how many rows the writer took, and it is reconciled against
	// TRL-COUNT below.
	written := int64(0)

	// net accumulates the posting amounts, to be reconciled against TRL-NET
	// below. It is plain int64 addition with no rescaling anywhere, and that is
	// a property of this layout rather than of decimals in general: TRL-NET is
	// PIC S9(13)V99, PDB-AMOUNT is PIC S9(11)V99 and PCR-AMOUNT is
	// PIC S9(9)V99, so all three carry scale 2 and their unscaled integers are
	// already in the same units. A layout whose trailer kept whole currency
	// units while its postings kept cents needs one side multiplied by a
	// hundred here — and a conversion that forgot to would fail this
	// reconciliation on every file, or pass it on the one file whose net is
	// zero.
	//
	// The sign each amount contributes is an assumption and not a reading; see
	// postingRowOf, and README.md, "The sign the net assumes".
	//
	// It cannot overflow on a file this layout describes. HDR-COUNT is
	// PIC 9(3) and ledger.Reader holds the body to it, so at most 999 amounts
	// of at most thirteen digits are summed — about 1e16, against int64's
	// 9.2e18. A layout with a wider count, or wider amounts, is one where that
	// has to be checked rather than argued.
	net := int64(0)

	for {
		rec, err := src.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("reading record %d: %w", ordinal+1, err)
		}

		ordinal++

		switch v := rec.(type) {
		case *ledger.LedgerHeader:
			hdr = v

			continue
		case *ledger.LedgerTrailer:
			trl = v

			continue
		}

		if hdr == nil {
			return fmt.Errorf("record %d is a posting and no LEDGER-HEADER has been read: the header's fields are denormalized onto every posting row and there is nothing to denormalize", ordinal)
		}

		mapped, amount, err := postingRowOf(hdr, rec)
		if err != nil {
			// Nothing is written. #272's sample discarded this error and
			// wrote the zero value, which writes a posting whose account is
			// the empty string and whose amount is zero — a row that reads as
			// data rather than as the failure it is.
			return fmt.Errorf("record %d: %w", ordinal, err)
		}

		row[0] = mapped

		n, err := postings.Write(row)
		if err != nil {
			return fmt.Errorf("record %d: writing the posting row: %w", ordinal, tooManyRowGroups(err, rows))
		}

		// written counts rows the writer took and never rows handed over. A
		// Write that returned 0 without an error would leave it short, and the
		// TRL-COUNT reconciliation below is what fails on that — reported as a
		// trailer disagreement rather than as a short write, and not reported
		// at all on a file carrying no trailer, which fails just below for that
		// reason instead.
		//
		// The per-row check that used to say it plainly is gone because
		// parquet-go cannot produce the case: the write function
		// GenericWriter.Write builds returns len(rows) or an error
		// (writer.go:227), so n is 1 here whenever err is nil, and a check
		// against a literal 1 is a check against a constant.
		written += int64(n)
		net += amount
	}

	if hdr == nil {
		return errors.New("the file carried no LEDGER-HEADER")
	}

	if trl == nil {
		return errors.New("the file carried no LEDGER-TRAILER")
	}

	// Both reconciliations before the footer is written, so that a conversion
	// which does not reconcile leaves a file no Parquet reader will open rather
	// than one a query would happily return wrong answers from. This is what
	// becomes of the trailer: it is checked and discarded, not stored.
	if int64(trl.TrlCount) != written {
		return fmt.Errorf("TRL-COUNT is %d and %d posting rows were written: the trailer counts the rows of this extract, so the two disagreeing means the file, the layout or this conversion is wrong and none of the three is a thing to write out anyway", trl.TrlCount, written)
	}

	if trl.TrlNet != net {
		return fmt.Errorf("TRL-NET is %d and the posting amounts sum to %d, both unscaled at scale 2: the trailer totals the rows of this extract, so the two disagreeing means the file is wrong or this conversion's sign convention is — it adds every amount with the sign the record stores it under, and a layout whose credits are stored positive and are meant to subtract disagrees here first", trl.TrlNet, net)
	}

	// Close is what writes the last row group, which on any file whose posting
	// count is not a multiple of rowsPerRowGroup is a partial one — so it is
	// the other half of the bound above rather than only the footer.
	if err := postings.Close(); err != nil {
		return fmt.Errorf("closing the posting table: %w", tooManyRowGroups(err, rows))
	}

	return nil
}

// tooManyRowGroups re-reports parquet-go's row-group cap as the thing an adopter
// can act on.
//
// A Parquet file holds at most MaxRowGroups = math.MaxInt16 = 32767 of them
// (limits.go:29), so a bound of R records puts a ceiling of 32767*R records on
// the file. #304 met it at 2,097,088 rows with a 64-row bound — this
// conversion's bound — and got "the limit of 32767 row groups has been reached"
// wrapped as "flushing 64 posting rows": a true sentence naming neither the cap
// nor the thing that moves it.
//
// It is not a wall this conversion's own dataset can reach. HDR-COUNT is
// PIC 9(3), so a ledger extract carries at most 999 postings and fills sixteen
// row groups; the ceiling is 2,097,088 records away and the memory budget
// README.md sizes against gives out first, at about 1.2 million. Both are
// arithmetic until somebody runs them, which is what
// TestTheRowGroupCeilingIsReachedAndSaysWhichLimitItWas does — it is the one
// test here that reaches a real cap in a real writer rather than handing this
// function an error to wrap.
//
// **The ceiling and the linear heap are one mistake with one fix, not two walls
// with two**: tiny row groups produce both, and a row group sized anywhere near
// R* produces neither.
func tooManyRowGroups(err error, rows int) error {
	if !errors.Is(err, parquet.ErrTooManyRowGroups) {
		return err
	}

	return fmt.Errorf("%w: a Parquet file holds at most 32767 row groups, so a row group bounded at %d rows puts a ceiling of %d records on this file — raise the bound, which lowers peak memory as well as the row-group count, and see README.md for the arithmetic", err, rows, int64(rows)*32767)
}

// postingRowOf is the row one posting contributes, under the header in force,
// and the amount it contributes to the net.
//
// An item present in one alternative and absent in the other becomes an
// optional column, and a pointer is how a row spells the absence: nil is the
// null, and there is no other value that says "this posting has no debit body".
//
// The composite literal is **not** a defence against aliasing, and this says so
// because the comment that claimed it was has now been read rather than carried
// over. Neither end of that claim holds. [ledger.Reader.Next] allocates a fresh
// record per call and its strings own their bytes, which is what convert is
// relying on when it keeps one *ledger.LedgerHeader and reads it on every
// posting for the rest of the file; and the writer retains nothing of a row past
// Write, because the column buffers copy out of the slice before it returns.
//
// So the copy is here for the ordinary reason: debitBody is a different type
// from the group ledger generated, a conversion's schema is not its source's,
// and a copy is what crossing that boundary is.
//
// The amount comes back beside the row rather than being read off it, so that
// there is exactly one place that decides what a record of each type contributes
// — the arm that builds the row. Reading it back off the row would mean a second
// switch on which optional group is present, which is a second place for the
// same decision to be made and a place for the two to drift apart.
//
// **That amount is the copybook's value with the sign the record stores it
// under, and taking it that way is an assumption the copybook does not make.**
// `posting.cpy` gives both amounts a signed PICTURE and says nothing about what
// a debit or a credit does to a total; this conversion reads the file as one
// that already carries the sign, so the net is a plain sum. A layout that stores
// both magnitudes positive and means the credit to subtract needs the credit arm
// negated here, and README.md's "The sign the net assumes" is where that is
// argued rather than merely noted.
//
// A record that is not one of this file's two posting types is an error and
// never a row. The two are what the layout's `(alt …)` lists, and a third
// arriving here means the layout and this conversion have gone out of step —
// which is a thing to report, not to write a zero row for.
func postingRowOf(hdr *ledger.LedgerHeader, rec ledger.Record) (postingRow, int64, error) {
	row := postingRow{
		HdrLedgerID: hdr.HdrLedgerId,
		HdrPeriod:   hdr.HdrPeriod,
		HdrCurrency: hdr.HdrCurrency,
	}

	amount := int64(0)

	switch v := rec.(type) {
	case *ledger.DebitPosting:
		row.PstAccount, row.PstSequence, row.PstType = v.PstAccount, v.PstSequence, v.PstType
		row.PstDebit = &debitBody{v.PstDebit.PdbCostCentre, v.PstDebit.PdbAmount, v.PstDebit.PdbMemo}
		row.PstTailRef = tailRef{v.PstTailRef.PtrBatch, v.PstTailRef.PtrLine}
		amount = v.PstDebit.PdbAmount
	case *ledger.CreditPosting:
		row.PstAccount, row.PstSequence, row.PstType = v.PstAccount, v.PstSequence, v.PstType
		row.PstCredit = &creditBody{v.PstCredit.PcrSource, v.PstCredit.PcrAmount, v.PstCredit.PcrReference}
		row.PstTailRef = tailRef{v.PstTailRef.PtrBatch, v.PstTailRef.PtrLine}
		amount = v.PstCredit.PcrAmount
	default:
		return postingRow{}, 0, fmt.Errorf("this file's postings are DEBIT-POSTING and CREDIT-POSTING, and a %T is neither of them", rec)
	}

	return row, amount, nil
}
