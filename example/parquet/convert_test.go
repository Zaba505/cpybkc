// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The assertions over the worked conversion.
//
// What they are for is stated in #272 and in README.md: the nine-line sample
// that story proposed was wrong in ten ways on this very layout — an unbounded
// row group, a writer never closed so the last partial group went out with every
// well-formed file, an ordinal never incremented, and a mapping error discarded
// into a zero row.
// Prose samples rot and nothing catches them, so this one is compiled and each
// of those properties is a test.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/example/ledger"
	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/format"
)

// postingTypes is how many posting record types the fixtures cycle through,
// which is all of them: `ledger.sexpr` names two of the six combinations
// `posting.cpy` admits, and the four it leaves out are described by PST-BODY or
// PST-TAIL — the base descriptions a mainframe-produced extract does not carry.
const postingTypes = 2

// ledgerBytes is a ledger extract of n postings, cycling through both of the
// record types this layout names.
//
// trlCount and trlNet are stated apart from the postings so that a file whose
// trailer disagrees with its own rows can be built: nothing in the layout ties
// either of them to the body, and a reconciliation nobody can make fail is not a
// reconciliation. [netOf] is what makes a well-formed one.
func ledgerBytes(t *testing.T, n int32, trlCount int32, trlNet int64) []byte {
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
		TrlNet:   trlNet,
	}); err != nil {
		t.Fatalf("writing the trailer: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("closing the ledger: %v", err)
	}

	return b.Bytes()
}

// debitAmount and creditAmount are the unscaled values of the two posting
// amounts, chosen to fill their own precisions rather than to be small:
// PDB-AMOUNT is thirteen digits and PCR-AMOUNT eleven, and a value that fits in
// both would not tell a mistaken precision from a correct one.
//
// The debit shrank from a full 9,999,999,999,999 when TRL-NET started being
// reconciled, because the largest fixture here now has to sum inside the
// trailer's own fifteen digits. What fits is the **alternating** 999-posting
// fixture — 499 debits and 500 credits, netting 887,012,346,228,013 — and not
// 999 debits, which come to 1,874,666,667,776,013 and do not. The headroom is
// in the alternation and not in the constant, so a change to which record type
// [posting] hands out, or a fixture longer than 999, overflows TRL-NET and
// arrives as a reconciliation failure that has nothing to do with the
// conversion.
//
// That the margin is this thin is a property of the layout rather than of the
// fixture: TRL-NET is two digits wider than PDB-AMOUNT, so a file of more than
// about a hundred like-signed postings has a total its own trailer cannot
// describe. A real layout's trailer has to be wide enough for the rows it
// totals, and this one only just is.
//
// The credit is negative because this file stores the sign of a posting on the
// posting; see [TestTheNetTakesEachAmountWithTheSignTheRecordStoresIt].
const (
	debitAmount  = int64(1876543210987)
	creditAmount = int64(-98765432109)
)

// netOf is the TRL-NET a well-formed fixture of n postings carries: the amounts
// its records store, summed.
//
// It walks [posting] rather than counting the record types itself. Closed-form
// arithmetic over the two constants is shorter, and it was wrong to write:
// n/2 debits is only right because [ledgerBytes] numbers its postings from one,
// and a loop that started from zero would leave every fixture failing its own
// TRL-NET reconciliation for a reason that has nothing to do with the
// conversion. Reading the fixture is not a second copy of the conversion's
// accumulator — it never touches convert.go — and it survives a change to
// either the alternation or the loop base.
func netOf(n int32) int64 {
	net := int64(0)

	for i := int32(1); i <= n; i++ {
		switch rec := posting(i).(type) {
		case *ledger.DebitPosting:
			net += rec.PstDebit.PdbAmount
		case *ledger.CreditPosting:
			net += rec.PstCredit.PcrAmount
		default:
			// posting hands out one of the two, so this is unreachable —
			// and a third record type added to the fixture without an arm
			// here would otherwise silently net to zero for it, which is a
			// TRL-NET the conversion would then be blamed for.
			panic(fmt.Sprintf("the fixture handed out a %T, and netOf knows the two record types this layout names", rec))
		}
	}

	return net
}

// posting is the i'th posting of a fixture, of the i'th of the two record types.
func posting(i int32) ledger.Record {
	account := fmt.Sprintf("ACCT%06d", i)
	sequence := i % 100

	if i%postingTypes == 0 {
		p := &ledger.DebitPosting{PstAccount: account, PstSequence: sequence, PstType: "DR"}
		p.PstDebit.PdbCostCentre = "CC0001"
		p.PstDebit.PdbAmount = debitAmount
		p.PstDebit.PdbMemo = "DEBIT MEMO ABCD"
		p.PstTailRef.PtrBatch = 4321
		p.PstTailRef.PtrLine = 8765

		return p
	}

	p := &ledger.CreditPosting{PstAccount: account, PstSequence: sequence, PstType: "CR"}
	p.PstCredit.PcrSource = "SRC2"
	p.PstCredit.PcrAmount = creditAmount
	p.PstCredit.PcrReference = "CREDIT REF DEF"
	p.PstTailRef.PtrBatch = 1234
	p.PstTailRef.PtrLine = 5678

	return p
}

// converted runs the conversion over a ledger extract of n postings whose
// trailer counts trlCount of them and totals trlNet, and returns the one file it
// wrote.
//
// The bytes come back whether or not the conversion succeeded, because what a
// failed conversion left behind is half of what the reconciliation tests assert.
func converted(t *testing.T, n int32, trlCount int32, trlNet int64) ([]byte, error) {
	t.Helper()

	raw := ledgerBytes(t, n, trlCount, trlNet)

	r, rerr := ledger.NewReader(bytes.NewReader(raw), ledger.Encoding())
	if rerr != nil {
		t.Fatalf("ledger.NewReader: %v", rerr)
	}

	var posting bytes.Buffer

	err := convert(r, &posting)

	return posting.Bytes(), err
}

// postingTable is the table a well-formed extract of n postings converts to.
func postingTable(t *testing.T, n int32) []byte {
	t.Helper()

	posting, err := converted(t, n, n, netOf(n))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	return posting
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

// TestTheConversionWritesOneFile is the first thing an adopter meets, and it is
// the decision this example changed its mind about: a Parquet file carries
// exactly one schema and this extract has two grains, and the answer is still
// one file. The file-level grain is denormalized and reconciled away rather than
// given a table of its own.
//
// It goes through [run] rather than through [convert], because "one file" is a
// claim about what is on the disk afterwards and not about how many writers the
// conversion opened. That is also what asserts -out: it names the file, so a
// directory that reads back with exactly one entry in it is both halves of the
// claim at once.
func TestTheConversionWritesOneFile(t *testing.T) {
	in := extractOnDisk(t, postingTypes, netOf(postingTypes))
	dir, out := outputPath(t)

	if err := run([]string{"-in", in, "-out", out}, io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}

	names := entryNames(t, dir)

	if len(names) != 1 || names[0] != filepath.Base(out) {
		t.Fatalf("the conversion left %v, want exactly [%q]: one grain that is not the posting is not a second file", names, filepath.Base(out))
	}

	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading the posting table back: %v", err)
	}

	if rows := rowsOf[postingRow](t, written); len(rows) != postingTypes {
		t.Errorf("the posting table holds %d rows, want %d: its grain is the posting", len(rows), postingTypes)
	}
}

// extractOnDisk writes a fixture of n postings whose trailer totals trlNet to a
// file, and returns the path, for the tests that go through [run].
func extractOnDisk(t *testing.T, n int32, trlNet int64) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "ledger.dat")
	if err := os.WriteFile(path, ledgerBytes(t, n, n, trlNet), 0o600); err != nil {
		t.Fatalf("writing the fixture extract: %v", err)
	}

	return path
}

// outputPath is a -out under a directory that is not there yet, because the
// invocation README.md documents has to run on a machine that has never run it.
func outputPath(t *testing.T) (dir, out string) {
	t.Helper()

	dir = filepath.Join(t.TempDir(), "out")

	return dir, filepath.Join(dir, "posting.parquet")
}

// entryNames is what a directory holds, which is how a claim about how many
// files a run wrote is asserted.
func entryNames(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s back: %v", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

// TestAFailedRunLeavesNoFileBehind is the on-disk half of both reconciliations,
// and it is the half [TestTheTrailerCountIsReconciled] and
// [TestTheTrailerNetIsReconciled] cannot assert: they convert into a buffer, so
// what they show is that the bytes are unopenable, not that write cleaned up
// after itself.
//
// What is on the disk after a failed run is not an unopenable file. It is no
// file: the footer was never written, and write removes the path it created
// rather than leaving bytes somebody has to know to distrust.
func TestAFailedRunLeavesNoFileBehind(t *testing.T) {
	// One cent out, which is the reconciliation failing rather than the reader.
	in := extractOnDisk(t, postingTypes, netOf(postingTypes)+1)
	dir, out := outputPath(t)

	err := run([]string{"-in", in, "-out", out}, io.Discard)
	if err == nil {
		t.Fatal("a file whose TRL-NET disagrees with its own postings converted without complaint")
	}

	if !strings.Contains(err.Error(), "TRL-NET") {
		t.Errorf("the failure is %q, and it does not name TRL-NET", err)
	}

	if names := entryNames(t, dir); len(names) != 0 {
		t.Errorf("the failed run left %v behind, want nothing: a half-converted file that survives is one somebody queries", names)
	}
}

// TestOutNamingADirectoryIsReportedAgainstTheRightFlag is the upgrade path.
//
// -out named a directory while this conversion wrote two files, so the
// invocation an adopter already has hands over one that exists. Left to
// os.Create that is "is a directory" under a wrapper naming the input, which
// sends somebody to look at their extract over a mistake in a flag.
func TestOutNamingADirectoryIsReportedAgainstTheRightFlag(t *testing.T) {
	in := extractOnDisk(t, postingTypes, netOf(postingTypes))
	dir := t.TempDir()

	err := run([]string{"-in", in, "-out", dir}, io.Discard)
	if err == nil {
		t.Fatal("-out naming a directory was accepted, and the conversion writes a file")
	}

	if !strings.Contains(err.Error(), "-out") {
		t.Errorf("the failure is %q, and it does not name the flag that is wrong", err)
	}

	if strings.Contains(err.Error(), in) {
		t.Errorf("the failure is %q, and it names the input file: the mistake is in -out, and an error that mentions the extract sends somebody to read the wrong thing", err)
	}

	if names := entryNames(t, dir); len(names) != 0 {
		t.Errorf("the rejected run left %v in the directory it refused, want nothing", names)
	}
}

// TestHeaderContextIsDenormalizedOntoEveryPosting is the decision that makes the
// posting table usable on its own.
func TestHeaderContextIsDenormalizedOntoEveryPosting(t *testing.T) {
	for i, row := range rowsOf[postingRow](t, postingTable(t, postingTypes)) {
		if row.HdrLedgerID != "GL-MAIN" || row.HdrPeriod != 202601 || row.HdrCurrency != "USD" {
			t.Errorf("posting row %d carries (%q, %d, %q), want the header's (%q, %d, %q): a query filtering on the period has to be able to do it without a join",
				i, row.HdrLedgerID, row.HdrPeriod, row.HdrCurrency, "GL-MAIN", 202601, "USD")
		}
	}
}

// TestTrailerFieldsDoNotPromote is the other half of that decision, and the
// reason it is a test rather than a sentence: TRL-COUNT and TRL-NET are
// summaries *of* the posting rows, so denormalizing them would make
// SUM(trl_net) return the total times the row count. With no second table to
// keep them in, "they do not promote" now means there is no column for them
// anywhere.
//
// HDR-COUNT is here for a different reason. It is a summary too, but it is one
// the generated reader has already enforced — `ledger.sexpr` reads it into a
// register and holds the body to it — so a column for it would be a number that
// can only ever agree with a row count a query can take for itself.
func TestTrailerFieldsDoNotPromote(t *testing.T) {
	posting := postingTable(t, postingTypes)

	f, err := parquet.OpenFile(bytes.NewReader(posting), int64(len(posting)))
	if err != nil {
		t.Fatalf("opening the posting table: %v", err)
	}

	for _, field := range f.Schema().Fields() {
		if strings.HasPrefix(field.Name(), "trl_") {
			t.Errorf("the posting table carries a %q column: a trailer field is a summary of these rows, and one on every row is a summary multiplied by the row count", field.Name())
		}

		if field.Name() == "hdr_count" {
			t.Error("the posting table carries an hdr_count column: ledger.Reader already holds the body to HDR-COUNT, so the column is a count that cannot disagree with COUNT(*)")
		}
	}
}

// TestTheMergedTableKeepsTheRecordTypeRecoverable is the merged-versus-narrow
// decision, asserted rather than described: both record types share one table,
// so PST-TYPE and the group that is present have to name which of the two a row
// was.
func TestTheMergedTableKeepsTheRecordTypeRecoverable(t *testing.T) {
	byType := make(map[string]postingRow)
	for _, row := range rowsOf[postingRow](t, postingTable(t, postingTypes)) {
		byType[row.PstType] = row
	}

	cases := []struct {
		pstType string
		body    string
	}{
		{"DR", "pst_debit"},
		{"CR", "pst_credit"},
	}

	for _, c := range cases {
		row, ok := byType[c.pstType]
		if !ok {
			t.Errorf("no row carries PST-TYPE %q, and the two record types are what the layout's (alt …) lists", c.pstType)

			continue
		}

		if got := present(row); got != c.body {
			t.Errorf("PST-TYPE %q arrived as %s, want %s: the description of PST-BODY a row carries is what says which record type it was", c.pstType, got, c.body)
		}
	}
}

// present names the description of PST-BODY that a row carries, which is
// exactly what recovers its record type.
//
// There is no second term. PST-TAIL is described one way in this layout, so
// every row carries pst_tail_ref and it distinguishes nothing —
// [TestTheOnlyDescriptionOfATailIsARequiredColumn] is where that is asserted.
func present(row postingRow) string {
	switch {
	case row.PstDebit != nil:
		return "pst_debit"
	case row.PstCredit != nil:
		return "pst_credit"
	default:
		return "none"
	}
}

// TestTheOnlyDescriptionOfATailIsARequiredColumn is the half of the merged table
// that the layout's narrowing changes.
//
// PST-TAIL is a redefined run of two descriptions in `posting.cpy` and of *one*
// in `ledger.sexpr`: every posting this extract carries is described by
// PST-TAIL-REF. A run with one description in play is not a choice a row is
// making, so the column is required — an optional one would be null on no row
// ever, and a reader would have to null-check it to find that out.
//
// And the descriptions the layout does not name carry no column at all. A
// column that is null on every row of every extract is one a query has to know
// to ignore.
func TestTheOnlyDescriptionOfATailIsARequiredColumn(t *testing.T) {
	posting := postingTable(t, postingTypes)

	f, err := parquet.OpenFile(bytes.NewReader(posting), int64(len(posting)))
	if err != nil {
		t.Fatalf("opening the posting table: %v", err)
	}

	tail := parquet.Node(nil)

	for _, field := range f.Schema().Fields() {
		switch field.Name() {
		case "pst_body", "pst_tail":
			t.Errorf("the posting table carries a %q column, and no record this layout names is described by it: it would be null on every row of every extract", field.Name())
		case "pst_tail_ref":
			tail = field
		}
	}

	if tail == nil {
		t.Fatal("the posting table carries no pst_tail_ref column, and every posting this layout names is described by PST-TAIL-REF")
	}

	if tail.Optional() {
		t.Error("pst_tail_ref is optional, and the layout names one description of PST-TAIL: a column that is null on no row is one every query null-checks for nothing")
	}
}

// TestEveryPostingReadIsAPostingWritten is the last partial row group, and it is
// run over a file whose posting count is not a multiple of the bound because
// that is the only kind of file an unwritten one loses rows from.
//
// It is also where the memory bound stops being a claim. The bound is the
// writer's — parquet.MaxRowsPerRowGroup, closed inside GenericWriter.Write and
// finished by Close — so a file of 999 postings comes back as sixteen row groups
// of at most sixty-four rows. A conversion that passed no option would come back
// as one, because parquet-go's default is math.MaxInt64 rather than a bound, and
// that is what this reads the written file to rule out rather than trust.
func TestEveryPostingReadIsAPostingWritten(t *testing.T) {
	// The most this layout can describe: HDR-COUNT is PIC 9(3).
	const postings = 999

	if postings%rowsPerRowGroup == 0 {
		t.Fatalf("this fixture has %d postings and the row group holds %d: a whole number of row groups is the one file an unwritten last one does not lose rows from", postings, rowsPerRowGroup)
	}

	posting := postingTable(t, postings)

	rows := rowsOf[postingRow](t, posting)
	if len(rows) != postings {
		t.Errorf("%d posting rows were written and %d were read: the last partial row group is what a conversion that never closed the writer drops, on every well-formed file", len(rows), postings)
	}

	f, err := parquet.OpenFile(bytes.NewReader(posting), int64(len(posting)))
	if err != nil {
		t.Fatalf("opening the posting table: %v", err)
	}

	for i, group := range f.RowGroups() {
		if group.NumRows() > rowsPerRowGroup {
			t.Errorf("row group %d holds %d rows and the bound is %d: peak memory is the open row group, and a row group that outgrows the bound is a parquet.MaxRowsPerRowGroup nothing passed", i, group.NumRows(), rowsPerRowGroup)
		}
	}

	if want := (postings + rowsPerRowGroup - 1) / rowsPerRowGroup; len(f.RowGroups()) != want {
		t.Errorf("the posting table holds %d row groups, want %d: one per %d rows is what parquet.MaxRowsPerRowGroup produces, and one row group is what the default bound of math.MaxInt64 produces", len(f.RowGroups()), want, rowsPerRowGroup)
	}
}

// TestTheTwoAmountColumnsRoundTripThroughDecimal is the claim that looks like it
// needs a conversion and does not.
//
// cpybkc-gen-go writes a COMP-3 item as an unscaled int64 with the scale in its
// doc comment, which is what DECIMAL(p,s) is, so the mapping is an annotation.
// Reading the written file back and comparing against what the ledger reader
// produced is what makes that a fact rather than a plausible sentence.
//
// TRL-NET is the third amount of the same shape and it is not here, because it
// is no longer a column: it is read, reconciled and discarded, which is
// [TestTheTrailerNetIsReconciled]. That all three carry scale 2 is what lets the
// reconciliation be plain addition, and it is the reason the mapping is worth
// stating for an item nothing writes.
func TestTheTwoAmountColumnsRoundTripThroughDecimal(t *testing.T) {
	posting := postingTable(t, postingTypes)

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

// TestTheTrailerCountIsReconciled is one of the two checks a conversion that
// streams can still make, and it runs before the footer is written.
func TestTheTrailerCountIsReconciled(t *testing.T) {
	posting, err := converted(t, postingTypes, postingTypes+1, netOf(postingTypes))
	if err == nil {
		t.Fatalf("a file whose TRL-COUNT is %d and whose body holds %d postings converted without complaint", postingTypes+1, postingTypes)
	}

	if !strings.Contains(err.Error(), "TRL-COUNT") {
		t.Errorf("the reconciliation failure is %q, and it does not name TRL-COUNT", err)
	}

	assertUnopenable(t, posting, "posting")
}

// TestTheTrailerNetIsReconciled is the other, and it is the one that costs an
// accumulator: TRL-COUNT falls out of the row count, and TRL-NET does not.
//
// TRL-NET, PDB-AMOUNT and PCR-AMOUNT all carry scale 2, so what is summed here
// is the unscaled integers as they stand and nothing multiplies or divides by a
// hundred. A conversion that rescaled one side would fail this on every fixture,
// which is the point of running it over a net that is not zero.
func TestTheTrailerNetIsReconciled(t *testing.T) {
	// One cent out. A reconciliation that compared anything coarser than the
	// unscaled integer — the count, the magnitude, the sign — would pass this.
	const off = 1

	posting, err := converted(t, postingTypes, postingTypes, netOf(postingTypes)+off)
	if err == nil {
		t.Fatalf("a file whose TRL-NET is %d and whose postings sum to %d converted without complaint", netOf(postingTypes)+off, netOf(postingTypes))
	}

	if !strings.Contains(err.Error(), "TRL-NET") {
		t.Errorf("the reconciliation failure is %q, and it does not name TRL-NET", err)
	}

	assertUnopenable(t, posting, "posting")
}

// TestTheNetTakesEachAmountWithTheSignTheRecordStoresIt is the assumption this
// conversion makes that the copybook does not.
//
// `posting.cpy` gives both amounts a signed PICTURE and says nothing about what
// a debit or a credit does to a total, so a converter that reconciles TRL-NET at
// all has to pick. This one adds every amount with the sign the record stores
// it under: the file is read as one that already carries the sign, which is what
// the fixtures do — [posting] stores its credits negative.
//
// The fixture here is the other kind of layout, the one where both amounts are
// stored as magnitudes and the credit is meant to subtract. Its trailer is
// right for that layout and this conversion reports it as wrong, which is
// exactly what an adopter whose file is shaped that way must see happen rather
// than have quietly accepted. README.md, "The sign the net assumes", is where
// the choice is argued.
func TestTheNetTakesEachAmountWithTheSignTheRecordStoresIt(t *testing.T) {
	const magnitude = int64(50000)

	debit := &ledger.DebitPosting{PstAccount: "ACCT000001", PstSequence: 1, PstType: "DR"}
	debit.PstDebit.PdbAmount = magnitude

	// Stored positive, and meant to subtract. That is the layout this
	// conversion is not.
	credit := &ledger.CreditPosting{PstAccount: "ACCT000002", PstSequence: 2, PstType: "CR"}
	credit.PstCredit.PcrAmount = magnitude

	src := &oneOff{records: []ledger.Record{
		&ledger.LedgerHeader{HdrType: "01", HdrLedgerId: "GL-MAIN", HdrPeriod: 202601, HdrCurrency: "USD", HdrCount: 2},
		debit,
		credit,
		// Debits less credits, which is zero — and this conversion sums them
		// to twice the magnitude instead.
		&ledger.LedgerTrailer{TrlType: "99", TrlCount: 2, TrlNet: 0},
	}}

	var posting bytes.Buffer

	err := convert(src, &posting)
	if err == nil {
		t.Fatal("an extract whose credits are stored positive and meant to subtract reconciled against a net this conversion did not compute")
	}

	if !strings.Contains(err.Error(), fmt.Sprint(2*magnitude)) {
		t.Errorf("the reconciliation failure is %q, and it does not say what this conversion summed the amounts to: an adopter whose layout is the other one finds out from this line", err)
	}
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

	var posting bytes.Buffer

	err := convert(src, &posting)
	if err == nil {
		t.Fatal("a record neither of the two posting types describes converted without complaint")
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

	var written bytes.Buffer

	err := convert(src, &written)
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
