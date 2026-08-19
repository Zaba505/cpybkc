// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Command parquet converts the ledger extract in the directory above this one
// into the two Parquet files a data platform would query it as.
//
// It is a worked conversion and a reference, not a recommendation and not a
// library. Every decision it makes is one an adopter has to make for themselves
// on their own layout; README.md beside this file states each of them, says why
// this one was taken, and marks the four where another adopter would reasonably
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
	"os"
	"path/filepath"

	"github.com/Zaba505/cpybkc/example/ledger"
	"github.com/parquet-go/parquet-go"
)

// batchSize is how many posting rows are held before they are written and the
// row group behind them is closed.
//
// It is the whole of what bounds this conversion's memory, and it is small
// deliberately: a real converter would size a row group in the tens or hundreds
// of thousands of rows, because a row group is the unit a query engine skips and
// a file of tiny ones costs footer metadata and dictionary resets on every one.
// Sixty-four is chosen so that the ledger this example carries — which
// HDR-COUNT's PIC 9(3) bounds at 999 postings — crosses the boundary sixteen
// times, so that a missing flush is visible in a test rather than theoretical.
const batchSize = 64

// extractRow is the `extract` table: one row per file.
//
// A Parquet file carries exactly one schema and this example has two grains, so
// there are two tables. The header and the trailer describe the extract as a
// whole, and this is where they live — both of them, together, so that
// TRL-COUNT and TRL-NET sit beside the HDR-COUNT they are a summary of.
type extractRow struct {
	HdrLedgerID string `parquet:"hdr_ledger_id"`
	HdrPeriod   int32  `parquet:"hdr_period"`
	HdrCurrency string `parquet:"hdr_currency"`
	HdrCount    int32  `parquet:"hdr_count"`

	TrlCount int32 `parquet:"trl_count"`

	// TrlNet is TRL-NET, PIC S9(13)V99 COMP-3: fifteen digits with two after
	// the point. cpybkc-gen-go produced it as an unscaled int64 with the scale
	// in its doc comment, which is exactly what DECIMAL(15,2) is, so this is a
	// re-annotation and not a conversion. Nothing here divides by a hundred.
	TrlNet int64 `parquet:"trl_net,decimal(2:15)"`
}

// postingRow is the `posting` table: one row per posting.
//
// The header's identifying fields are denormalized onto every row and the
// trailer's are not; README.md says why in terms of what SUM does to each. The
// six record types the copybook's REDEFINES resolve to are one table rather than
// six, and the two independent runs each become one optional column per
// alternative — which is what keeps the record type recoverable from a row.
type postingRow struct {
	HdrLedgerID string `parquet:"hdr_ledger_id"`
	HdrPeriod   int32  `parquet:"hdr_period"`
	HdrCurrency string `parquet:"hdr_currency"`

	PstAccount  string `parquet:"pst_account"`
	PstSequence int32  `parquet:"pst_sequence"`
	PstType     string `parquet:"pst_type"`

	// The first redefined run, PST-BODY, described three ways. Exactly one of
	// these three is present on any row, and which one is a function of
	// pst_type.
	PstBody   *string     `parquet:"pst_body"`
	PstDebit  *debitBody  `parquet:"pst_debit"`
	PstCredit *creditBody `parquet:"pst_credit"`

	// The second run, PST-TAIL, described two ways. Independent of the first,
	// which is why six record types and not three come out of one 01-level.
	PstTail    *string  `parquet:"pst_tail"`
	PstTailRef *tailRef `parquet:"pst_tail_ref"`
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
type recordSource interface {
	Next() (ledger.Record, error)
}

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run converts the dataset named by -in into extract.parquet and
// posting.parquet under -out.
//
// Both outputs are removed if the conversion fails. A half-written Parquet file
// has no footer and no reader will open it, so leaving one behind would be
// leaving something that reads as corruption rather than as a failed run.
func run(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("parquet", flag.ContinueOnError)
	fs.SetOutput(stderr)

	in := fs.String("in", "", "the ledger extract to convert")
	out := fs.String("out", ".", "the directory the two Parquet files are written to")

	if err := fs.Parse(args); err != nil {
		return err
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

	extractPath := filepath.Join(*out, "extract.parquet")
	postingPath := filepath.Join(*out, "posting.parquet")

	if err := write(r, extractPath, postingPath); err != nil {
		// Both outputs go. A conversion that fails returns before either footer
		// is written, and a Parquet file with no footer is bytes that read as
		// corruption rather than as a run somebody has to repeat.
		return errors.Join(fmt.Errorf("converting %s: %w", *in, err), os.Remove(extractPath), os.Remove(postingPath))
	}

	return nil
}

// write creates the two tables and converts into them.
//
// Both files are closed whatever happens, and a failure to close is joined to
// whatever the conversion reported rather than replacing it: a full disk shows
// up here and nowhere else.
func write(src recordSource, extractPath, postingPath string) (err error) {
	extract, err := os.Create(extractPath)
	if err != nil {
		return err
	}

	defer func() { err = errors.Join(err, extract.Close()) }()

	posting, err := os.Create(postingPath)
	if err != nil {
		return err
	}

	defer func() { err = errors.Join(err, posting.Close()) }()

	return convert(src, extract, posting)
}

// convert reads every record of src once and writes the two tables.
//
// One pass, and the file is never held: what this carries at any moment is the
// header, the batch of posting rows in hand, and the row group parquet-go is
// buffering behind it. That is the whole of its footprint, and README.md states
// it that way rather than as "constant".
func convert(src recordSource, extract, posting io.Writer) error {
	postings := parquet.NewGenericWriter[postingRow](posting)
	extracts := parquet.NewGenericWriter[extractRow](extract)

	var hdr *ledger.LedgerHeader
	var trl *ledger.LedgerTrailer

	// ordinal is which record of the file has been read, counting from one.
	// ledger.Reader keeps one for its own diagnostics and does not export it, so
	// a caller that wants to say where it failed counts its own — and this one
	// is a diagnostic and never a column. See README.md, "No key is minted".
	ordinal := 0

	// batch is reused between flushes. Its capacity is the bound.
	batch := make([]postingRow, 0, batchSize)
	written := int64(0)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}

		if _, err := postings.Write(batch); err != nil {
			return fmt.Errorf("writing %d posting rows: %w", len(batch), err)
		}

		// Explicit, because Write only buffers. Without this the row group
		// grows for the whole file and the bound above is a comment rather
		// than a fact.
		if err := postings.Flush(); err != nil {
			return fmt.Errorf("flushing %d posting rows: %w", len(batch), err)
		}

		written += int64(len(batch))
		batch = batch[:0]

		return nil
	}

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

		row, err := postingRowOf(hdr, rec)
		if err != nil {
			// The row is not appended. #272's sample discarded this error and
			// appended the zero value, which writes a posting whose account is
			// the empty string and whose amount is zero — a row that reads as
			// data rather than as the failure it is.
			return fmt.Errorf("record %d: %w", ordinal, err)
		}

		batch = append(batch, row)

		if len(batch) == batchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}

	// The terminal flush. Without it every file whose posting count is not a
	// multiple of batchSize silently loses its last partial batch, which is a
	// defect that never shows up on a test file of a round number of rows.
	if err := flush(); err != nil {
		return err
	}

	if hdr == nil {
		return errors.New("the file carried no LEDGER-HEADER")
	}

	if trl == nil {
		return errors.New("the file carried no LEDGER-TRAILER")
	}

	// Reconciliation before either footer is written, so that a conversion which
	// does not reconcile leaves two files no Parquet reader will open rather
	// than two a query would happily return wrong answers from.
	if int64(trl.TrlCount) != written {
		return fmt.Errorf("TRL-COUNT is %d and %d posting rows were written: the trailer counts the rows of this extract, so the two disagreeing means the file, the layout or this conversion is wrong and none of the three is a thing to write out anyway", trl.TrlCount, written)
	}

	if err := postings.Close(); err != nil {
		return fmt.Errorf("closing the posting table: %w", err)
	}

	if _, err := extracts.Write([]extractRow{extractRowOf(hdr, trl)}); err != nil {
		return fmt.Errorf("writing the extract row: %w", err)
	}

	if err := extracts.Close(); err != nil {
		return fmt.Errorf("closing the extract table: %w", err)
	}

	return nil
}

// extractRowOf is the one row of the `extract` table.
func extractRowOf(hdr *ledger.LedgerHeader, trl *ledger.LedgerTrailer) extractRow {
	return extractRow{
		HdrLedgerID: hdr.HdrLedgerId,
		HdrPeriod:   hdr.HdrPeriod,
		HdrCurrency: hdr.HdrCurrency,
		HdrCount:    hdr.HdrCount,
		TrlCount:    trl.TrlCount,
		TrlNet:      trl.TrlNet,
	}
}

// postingRowOf is the row one posting contributes, under the header in force.
//
// An item present in one alternative and absent in the others becomes an
// optional column, and `new(expr)` is what points at one: it allocates and
// copies, so the row does not alias a record the reader is free to reuse behind
// it — a batch is held until it is flushed, which is up to sixty-four records
// later.
//
// A record that is not one of this file's six posting types is an error and
// never a row. The six are what the layout's `(alt …)` lists, and a seventh
// arriving here means the layout and this conversion have gone out of step —
// which is a thing to report, not to write a zero row for.
func postingRowOf(hdr *ledger.LedgerHeader, rec ledger.Record) (postingRow, error) {
	row := postingRow{
		HdrLedgerID: hdr.HdrLedgerId,
		HdrPeriod:   hdr.HdrPeriod,
		HdrCurrency: hdr.HdrCurrency,
	}

	switch v := rec.(type) {
	case *ledger.DebitPosting:
		row.PstAccount, row.PstSequence, row.PstType = v.PstAccount, v.PstSequence, v.PstType
		row.PstDebit = &debitBody{v.PstDebit.PdbCostCentre, v.PstDebit.PdbAmount, v.PstDebit.PdbMemo}
		row.PstTail = new(v.PstTail)
	case *ledger.DebitPostingRef:
		row.PstAccount, row.PstSequence, row.PstType = v.PstAccount, v.PstSequence, v.PstType
		row.PstDebit = &debitBody{v.PstDebit.PdbCostCentre, v.PstDebit.PdbAmount, v.PstDebit.PdbMemo}
		row.PstTailRef = &tailRef{v.PstTailRef.PtrBatch, v.PstTailRef.PtrLine}
	case *ledger.CreditPosting:
		row.PstAccount, row.PstSequence, row.PstType = v.PstAccount, v.PstSequence, v.PstType
		row.PstCredit = &creditBody{v.PstCredit.PcrSource, v.PstCredit.PcrAmount, v.PstCredit.PcrReference}
		row.PstTail = new(v.PstTail)
	case *ledger.CreditPostingRef:
		row.PstAccount, row.PstSequence, row.PstType = v.PstAccount, v.PstSequence, v.PstType
		row.PstCredit = &creditBody{v.PstCredit.PcrSource, v.PstCredit.PcrAmount, v.PstCredit.PcrReference}
		row.PstTailRef = &tailRef{v.PstTailRef.PtrBatch, v.PstTailRef.PtrLine}
	case *ledger.MemoPosting:
		row.PstAccount, row.PstSequence, row.PstType = v.PstAccount, v.PstSequence, v.PstType
		row.PstBody = new(v.PstBody)
		row.PstTail = new(v.PstTail)
	case *ledger.MemoPostingRef:
		row.PstAccount, row.PstSequence, row.PstType = v.PstAccount, v.PstSequence, v.PstType
		row.PstBody = new(v.PstBody)
		row.PstTailRef = &tailRef{v.PstTailRef.PtrBatch, v.PstTailRef.PtrLine}
	default:
		return postingRow{}, fmt.Errorf("this file's postings are DEBIT-POSTING, DEBIT-POSTING-REF, CREDIT-POSTING, CREDIT-POSTING-REF, MEMO-POSTING and MEMO-POSTING-REF, and a %T is none of them", rec)
	}

	return row, nil
}
