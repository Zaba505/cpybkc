// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The assertions over the worked conversion.
//
// What they are for is stated in #272 and in README.md: the nine-line sample
// that story proposed was wrong in ten ways on this very layout — no flush, no
// terminal flush so the last partial batch went out with every well-formed file,
// an ordinal never incremented, and a mapping error discarded into a zero row.
// Prose samples rot and nothing catches them, so this one is compiled and each
// of those properties is a test.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/example/ledger"
	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/format"
)

// postingsPerType is how many of the six posting types the fixtures cycle
// through, which is all of them.
const postingsPerType = 6

// ledgerBytes is a well-formed ledger extract of n postings, cycling through all
// six of the record types this layout resolves out of one 01-level.
//
// trlCount is stated apart from n so that a file whose trailer disagrees with
// its own rows can be built: nothing in the layout ties TRL-COUNT to the
// postings, and a reconciliation nobody can make fail is not a reconciliation.
func ledgerBytes(t *testing.T, n int32, trlCount int32) []byte {
	t.Helper()

	var b bytes.Buffer

	w, err := ledger.NewWriter(&b, ledger.Encoding())
	if err != nil {
		t.Fatalf("ledger.NewWriter: %v", err)
	}

	if err := w.Write(&ledger.LedgerHeader{
		HdrType:     "01",
		HdrLedgerId: "GL-MAIN",
		HdrPeriod:   202601,
		HdrCurrency: "USD",
		HdrCount:    n,
	}); err != nil {
		t.Fatalf("writing the header: %v", err)
	}

	for i := int32(1); i <= n; i++ {
		if err := w.Write(posting(i)); err != nil {
			t.Fatalf("writing posting %d: %v", i, err)
		}
	}

	if err := w.Write(&ledger.LedgerTrailer{
		TrlType:  "99",
		TrlCount: trlCount,
		TrlNet:   trailerNet,
	}); err != nil {
		t.Fatalf("writing the trailer: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("closing the ledger: %v", err)
	}

	return b.Bytes()
}

// trailerNet is TRL-NET's unscaled value: fifteen digits with two after the
// point, so -1234567890123.45. It is carried through the conversion unchanged
// and nothing here divides by a hundred, which is the point of asserting it.
const trailerNet = int64(-123456789012345)

// debitAmount and creditAmount are the unscaled values of the two posting
// amounts, chosen to fill their own precisions rather than to be small:
// PDB-AMOUNT is thirteen digits and PCR-AMOUNT eleven, and a value that fits in
// both would not tell a mistaken precision from a correct one.
const (
	debitAmount  = int64(9876543210987)
	creditAmount = int64(-98765432109)
)

// posting is the i'th posting of a fixture, of the i'th of the six record types.
func posting(i int32) ledger.Record {
	account := fmt.Sprintf("ACCT%06d", i)
	sequence := i % 100

	switch i % postingsPerType {
	case 0:
		p := &ledger.DebitPosting{PstAccount: account, PstSequence: sequence, PstType: "DA", PstTail: "TAIL0001"}
		p.PstDebit.PdbCostCentre = "CC0001"
		p.PstDebit.PdbAmount = debitAmount
		p.PstDebit.PdbMemo = "DEBIT MEMO ABCD"

		return p
	case 1:
		p := &ledger.DebitPostingRef{PstAccount: account, PstSequence: sequence, PstType: "DB"}
		p.PstDebit.PdbCostCentre = "CC0002"
		p.PstDebit.PdbAmount = debitAmount
		p.PstDebit.PdbMemo = "DEBIT MEMO EFGH"
		p.PstTailRef.PtrBatch = 4321
		p.PstTailRef.PtrLine = 8765

		return p
	case 2:
		p := &ledger.CreditPosting{PstAccount: account, PstSequence: sequence, PstType: "CA", PstTail: "TAIL0002"}
		p.PstCredit.PcrSource = "SRC1"
		p.PstCredit.PcrAmount = creditAmount
		p.PstCredit.PcrReference = "CREDIT REF ABC"

		return p
	case 3:
		p := &ledger.CreditPostingRef{PstAccount: account, PstSequence: sequence, PstType: "CB"}
		p.PstCredit.PcrSource = "SRC2"
		p.PstCredit.PcrAmount = creditAmount
		p.PstCredit.PcrReference = "CREDIT REF DEF"
		p.PstTailRef.PtrBatch = 1234
		p.PstTailRef.PtrLine = 5678

		return p
	case 4:
		return &ledger.MemoPosting{
			PstAccount:  account,
			PstSequence: sequence,
			PstType:     "MA",
			PstBody:     "MEMO BODY TWENTY EIGHT BYTES",
			PstTail:     "TAIL0003",
		}
	default:
		p := &ledger.MemoPostingRef{
			PstAccount:  account,
			PstSequence: sequence,
			PstType:     "MB",
			PstBody:     "MEMO BODY TWENTY EIGHT BYTES",
		}
		p.PstTailRef.PtrBatch = 2468
		p.PstTailRef.PtrLine = 1357

		return p
	}
}

// converted runs the conversion over a ledger extract of n postings whose
// trailer counts trlCount of them, and returns the two files it wrote.
func converted(t *testing.T, n int32, trlCount int32) (extract, posting []byte, err error) {
	t.Helper()

	raw := ledgerBytes(t, n, trlCount)

	r, rerr := ledger.NewReader(bytes.NewReader(raw), ledger.Encoding())
	if rerr != nil {
		t.Fatalf("ledger.NewReader: %v", rerr)
	}

	var extractBuf, postingBuf bytes.Buffer

	err = convert(r, &extractBuf, &postingBuf)

	return extractBuf.Bytes(), postingBuf.Bytes(), err
}

// rowsOf reads a whole Parquet file back as T.
//
// What comes back is the rows actually decoded, not the row count the footer
// claims. The two are the same on a well-formed file and they are exactly what
// the caller is holding against each other on a broken one — a slice sized from
// the footer and returned at that length hands back zero values for rows nobody
// read, so an assertion about how many rows came back would be an assertion
// about the footer.
func rowsOf[T any](t *testing.T, b []byte) []T {
	t.Helper()

	r := parquet.NewGenericReader[T](bytes.NewReader(b))
	defer func() { _ = r.Close() }()

	rows := make([]T, r.NumRows())
	read := 0

	for read < len(rows) {
		n, err := r.Read(rows[read:])
		read += n

		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatalf("reading rows back: %v", err)
		}

		if n == 0 {
			t.Fatalf("reading rows back: no progress after %d of %d rows", read, len(rows))
		}
	}

	return rows[:read]
}

// TestTheTwoGrainsBecomeTwoFiles is the first thing an adopter meets: a Parquet
// file carries exactly one schema, and this extract has two grains.
func TestTheTwoGrainsBecomeTwoFiles(t *testing.T) {
	extract, posting, err := converted(t, postingsPerType, postingsPerType)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	extracts := rowsOf[extractRow](t, extract)
	if len(extracts) != 1 {
		t.Fatalf("the extract table holds %d rows, want 1: its grain is the file", len(extracts))
	}

	want := extractRow{
		HdrLedgerID: "GL-MAIN",
		HdrPeriod:   202601,
		HdrCurrency: "USD",
		HdrCount:    postingsPerType,
		TrlCount:    postingsPerType,
		TrlNet:      trailerNet,
	}
	if extracts[0] != want {
		t.Errorf("the extract row is %+v, want %+v", extracts[0], want)
	}

	postings := rowsOf[postingRow](t, posting)
	if len(postings) != postingsPerType {
		t.Fatalf("the posting table holds %d rows, want %d: its grain is the posting", len(postings), postingsPerType)
	}
}

// TestHeaderContextIsDenormalizedOntoEveryPosting is the decision that makes the
// posting table usable on its own.
func TestHeaderContextIsDenormalizedOntoEveryPosting(t *testing.T) {
	_, posting, err := converted(t, postingsPerType, postingsPerType)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	for i, row := range rowsOf[postingRow](t, posting) {
		if row.HdrLedgerID != "GL-MAIN" || row.HdrPeriod != 202601 || row.HdrCurrency != "USD" {
			t.Errorf("posting row %d carries (%q, %d, %q), want the header's (%q, %d, %q): a query filtering on the period has to be able to do it without a join",
				i, row.HdrLedgerID, row.HdrPeriod, row.HdrCurrency, "GL-MAIN", 202601, "USD")
		}
	}
}

// TestTrailerFieldsDoNotPromote is the other half of that decision, and the
// reason it is a test rather than a sentence: TRL-COUNT and TRL-NET are
// summaries *of* the posting rows, so denormalizing them would make
// SUM(trl_net) return the total times the row count.
func TestTrailerFieldsDoNotPromote(t *testing.T) {
	_, posting, err := converted(t, postingsPerType, postingsPerType)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	f, err := parquet.OpenFile(bytes.NewReader(posting), int64(len(posting)))
	if err != nil {
		t.Fatalf("opening the posting table: %v", err)
	}

	for _, field := range f.Schema().Fields() {
		if strings.HasPrefix(field.Name(), "trl_") {
			t.Errorf("the posting table carries a %q column: a trailer field is a summary of these rows, and one on every row is a summary multiplied by the row count", field.Name())
		}
	}
}

// TestTheMergedTableKeepsTheRecordTypeRecoverable is the merged-versus-narrow
// decision, asserted rather than described: six record types share one table, so
// PST-TYPE and the group that is present have to name which of the six a row was.
func TestTheMergedTableKeepsTheRecordTypeRecoverable(t *testing.T) {
	_, posting, err := converted(t, postingsPerType, postingsPerType)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	byType := make(map[string]postingRow)
	for _, row := range rowsOf[postingRow](t, posting) {
		byType[row.PstType] = row
	}

	cases := []struct {
		pstType string
		body    string
		tail    string
	}{
		{"DA", "pst_debit", "pst_tail"},
		{"DB", "pst_debit", "pst_tail_ref"},
		{"CA", "pst_credit", "pst_tail"},
		{"CB", "pst_credit", "pst_tail_ref"},
		{"MA", "pst_body", "pst_tail"},
		{"MB", "pst_body", "pst_tail_ref"},
	}

	for _, c := range cases {
		row, ok := byType[c.pstType]
		if !ok {
			t.Errorf("no row carries PST-TYPE %q, and the six record types are what the layout's (alt …) lists", c.pstType)

			continue
		}

		if got := present(row); got != c.body+"+"+c.tail {
			t.Errorf("PST-TYPE %q arrived as %s, want %s+%s: the two redefined runs are independent, so the alternatives multiply", c.pstType, got, c.body, c.tail)
		}
	}
}

// present names the alternative of each of the two redefined runs that a row
// carries, which is exactly what recovers its record type.
func present(row postingRow) string {
	body := "none"

	switch {
	case row.PstBody != nil:
		body = "pst_body"
	case row.PstDebit != nil:
		body = "pst_debit"
	case row.PstCredit != nil:
		body = "pst_credit"
	}

	tail := "none"

	switch {
	case row.PstTail != nil:
		tail = "pst_tail"
	case row.PstTailRef != nil:
		tail = "pst_tail_ref"
	}

	return body + "+" + tail
}

// TestEveryPostingReadIsAPostingWritten is the terminal flush, and it is run
// over a file whose posting count is not a multiple of the batch size because
// that is the only kind of file a missing one loses rows from.
//
// It is also where the memory bound stops being a claim. The row group is closed
// with every batch, so a file of 999 postings comes back as sixteen row groups
// of at most sixty-four rows — and a conversion that buffered the file would
// come back as one.
func TestEveryPostingReadIsAPostingWritten(t *testing.T) {
	// The most this layout can describe: HDR-COUNT is PIC 9(3).
	const postings = 999

	if postings%batchSize == 0 {
		t.Fatalf("this fixture has %d postings and the batch is %d: a whole number of batches is the one file a missing terminal flush does not lose rows from", postings, batchSize)
	}

	_, posting, err := converted(t, postings, postings)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	rows := rowsOf[postingRow](t, posting)
	if len(rows) != postings {
		t.Errorf("%d posting rows were written and %d were read: the last partial batch is what a conversion without a terminal flush drops, on every well-formed file", len(rows), postings)
	}

	f, err := parquet.OpenFile(bytes.NewReader(posting), int64(len(posting)))
	if err != nil {
		t.Fatalf("opening the posting table: %v", err)
	}

	for i, group := range f.RowGroups() {
		if group.NumRows() > batchSize {
			t.Errorf("row group %d holds %d rows and the batch is %d: peak memory is the batch plus the open row group, and a row group that outgrows the batch is a bound nothing enforces", i, group.NumRows(), batchSize)
		}
	}

	if want := (postings + batchSize - 1) / batchSize; len(f.RowGroups()) != want {
		t.Errorf("the posting table holds %d row groups, want %d: one per batch is what an explicit flush per batch produces", len(f.RowGroups()), want)
	}
}

// TestTheThreeAmountsRoundTripThroughDecimal is the claim that looks like it
// needs a conversion and does not.
//
// cpybkc-gen-go writes a COMP-3 item as an unscaled int64 with the scale in its
// doc comment, which is what DECIMAL(p,s) is, so the mapping is an annotation.
// Reading the written file back and comparing against what the ledger reader
// produced is what makes that a fact rather than a plausible sentence.
func TestTheThreeAmountsRoundTripThroughDecimal(t *testing.T) {
	extract, posting, err := converted(t, postingsPerType, postingsPerType)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	if got := rowsOf[extractRow](t, extract)[0].TrlNet; got != trailerNet {
		t.Errorf("TRL-NET came back as %d, want %d", got, trailerNet)
	}

	debits, credits := 0, 0

	for _, row := range rowsOf[postingRow](t, posting) {
		if row.PstDebit != nil {
			debits++

			if row.PstDebit.PdbAmount != debitAmount {
				t.Errorf("PDB-AMOUNT came back as %d, want %d", row.PstDebit.PdbAmount, debitAmount)
			}
		}

		if row.PstCredit != nil {
			credits++

			if row.PstCredit.PcrAmount != creditAmount {
				t.Errorf("PCR-AMOUNT came back as %d, want %d", row.PstCredit.PcrAmount, creditAmount)
			}
		}
	}

	// Without this the two comparisons above are guarded by the very thing that
	// would be broken — optional groups coming back nil — and the test named for
	// the round trip would pass having compared nothing.
	if debits == 0 || credits == 0 {
		t.Fatalf("%d debit and %d credit rows carried an amount to compare, want some of each", debits, credits)
	}

	assertDecimal(t, extract, "trl_net", 15, 2)
	assertDecimal(t, posting, "pst_debit.pdb_amount", 13, 2)
	assertDecimal(t, posting, "pst_credit.pcr_amount", 11, 2)
}

// assertDecimal requires the column at path to be annotated DECIMAL(precision,
// scale) in the file's own schema, which is what a query engine reads rather
// than the Go struct tag.
func assertDecimal(t *testing.T, file []byte, path string, precision, scale int32) {
	t.Helper()

	f, err := parquet.OpenFile(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}

	node := parquet.Node(f.Schema())

	for name := range strings.SplitSeq(path, ".") {
		next := (parquet.Node)(nil)

		for _, field := range node.Fields() {
			if field.Name() == name {
				next = field
			}
		}

		if next == nil {
			t.Fatalf("the schema carries no %q on the way to %s", name, path)
		}

		node = next
	}

	decimal := (*format.DecimalType)(nil)
	if logical := node.Type().LogicalType(); logical != nil {
		decimal, _ = logical.Value.(*format.DecimalType)
	}

	if decimal == nil {
		t.Fatalf("%s is not annotated DECIMAL: an unscaled integer written without one is a number whose scale only the copybook knows", path)
	}

	if decimal.Precision != precision || decimal.Scale != scale {
		t.Errorf("%s is DECIMAL(%d,%d), want DECIMAL(%d,%d)", path, decimal.Precision, decimal.Scale, precision, scale)
	}
}

// TestTheTrailerCountIsReconciled is the check a conversion that streams can
// still make, and it runs before either footer is written.
func TestTheTrailerCountIsReconciled(t *testing.T) {
	extract, posting, err := converted(t, postingsPerType, postingsPerType+1)
	if err == nil {
		t.Fatalf("a file whose TRL-COUNT is %d and whose body holds %d postings converted without complaint", postingsPerType+1, postingsPerType)
	}

	if !strings.Contains(err.Error(), "TRL-COUNT") {
		t.Errorf("the reconciliation failure is %q, and it does not name TRL-COUNT", err)
	}

	assertUnopenable(t, extract, "extract")
	assertUnopenable(t, posting, "posting")
}

// unmappable is a record no transition of this layout admits, which is what a
// record type added to the layout and not to this conversion would look like
// from here.
type unmappable struct {
	ledger.Record
}

// oneOff is a record source that hands out a fixed list and then reports the end
// of the file.
type oneOff struct {
	records []ledger.Record
}

func (s *oneOff) Next() (ledger.Record, error) {
	if len(s.records) == 0 {
		return nil, io.EOF
	}

	rec := s.records[0]
	s.records = s.records[1:]

	return rec, nil
}

// TestAMappingErrorFailsTheConversion is #272's tenth defect: its sample
// discarded the error and appended the zero value, which writes a posting whose
// account is empty and whose amount is zero.
//
// The ordinal in the diagnostic is the caller's own count. ledger.Reader keeps
// one and does not export it, which is why a conversion that wants to say where
// it failed counts its own.
func TestAMappingErrorFailsTheConversion(t *testing.T) {
	src := &oneOff{records: []ledger.Record{
		&ledger.LedgerHeader{HdrType: "01", HdrLedgerId: "GL-MAIN", HdrPeriod: 202601, HdrCurrency: "USD", HdrCount: 1},
		unmappable{},
	}}

	var extract, posting bytes.Buffer

	err := convert(src, &extract, &posting)
	if err == nil {
		t.Fatal("a record none of the six posting types describes converted without complaint")
	}

	if !strings.Contains(err.Error(), "record 2") {
		t.Errorf("the mapping failure is %q, and it does not say which record it was", err)
	}

	assertUnopenable(t, posting.Bytes(), "posting")
}

// TestAPostingBeforeItsHeaderIsReported is the other end of the denormalization:
// there is nothing to denormalize until the header has been read.
func TestAPostingBeforeItsHeaderIsReported(t *testing.T) {
	src := &oneOff{records: []ledger.Record{posting(1)}}

	var extract, posting bytes.Buffer

	err := convert(src, &extract, &posting)
	if err == nil {
		t.Fatal("a posting ahead of its header converted without complaint")
	}

	if !strings.Contains(err.Error(), "LEDGER-HEADER") {
		t.Errorf("the failure is %q, and it does not say what was missing", err)
	}
}

// assertUnopenable requires that a failed conversion left nothing a reader will
// accept. A Parquet file is its footer, and a conversion that returns an error
// before writing one leaves bytes no engine will read as a table.
func assertUnopenable(t *testing.T, file []byte, name string) {
	t.Helper()

	if _, err := parquet.OpenFile(bytes.NewReader(file), int64(len(file))); err == nil {
		t.Errorf("the %s table opened after a failed conversion: a half-converted file that reads as a table is one somebody queries", name)
	}
}
