// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The assertions over the wide sparse conversion.
//
// What they are for is the same thing convert.go is for: not the schema design,
// which the ledger conversion argues, but the **cost** of one wide sparse table
// and the arithmetic that pays it. So the tests that matter most here are the
// ones that hold the memory model's inputs against the file the conversion
// actually writes — the column count, and every leaf being an optional column —
// because those are what the row-group bound is derived from, and a derivation
// whose inputs nobody checks is a number somebody chose.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/example/policy/policy"
	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/format"
)

// recordTypes is how many record types `pxtract.cpy` declares, which is how many
// groups the merged schema carries.
const recordTypes = 11

// detailTypes is how many of them hang off a policy: the eleven less the header,
// the trailer and PX-POLICY itself.
const detailTypes = 8

// widestRecord is the most columns any record type of this file populates.
//
// It is the number the sibling README's central claim is made of — no row fills
// more than 20 of 197, which is 10.2% — and
// [TestNoRowFillsMoreThanATenthOfTheTable] is where it stops being a sentence.
const widestRecord = 20

// The file-level values every fixture carries. The cycle date and the run number
// are on both the header and the trailer, which is what [reconcile] holds them
// against each other for.
const (
	cycleDate = 20260825
	// The zoned item is PIC 9(6) and holds 013000; an int32 does not carry the
	// leading zero, and nothing here puts it back. See README.md, "The semantics
	// stop at the copybook".
	cycleTime = 13000
	runNumber = 4207
	carrier   = "ACME"
)

// The scaled values the fixtures carry, one distinct constant per scaled item so
// that a column receiving another column's value is visible.
//
// Each one fills its item's precision rather than being small: a value that fits
// in every precision would not tell a mistaken annotation from a correct one.
// Negative where the item's PICTURE is signed, because an unscaled integer that
// is never negative does not exercise the sign at all.
const (
	plcWrittenPremium = int64(12345678901)     // S9(9)V99  — 11 digits
	locDistanceToFire = int32(1234)            // 9(3)V9    — 4 digits, unsigned, and not COMP-3
	covLimit1         = int64(1234567890123)   // S9(11)V99 — 13
	covLimit2         = int64(-9876543210987)  // S9(11)V99 — 13
	covDeductible     = int32(123456789)       // S9(7)V99  — 9
	covRate           = int64(1234567890)      // S9(5)V9(5) — 10, four places past the point of the rest
	prmWritten        = int64(11111111111)     // S9(9)V99  — 11
	prmEarned         = int64(22222222222)     // S9(9)V99  — 11
	prmUnearned       = int64(-33333333333)    // S9(9)V99  — 11
	prmCommission     = int32(444444444)       // S9(7)V99  — 9
	prmCommissionRate = int32(1234567)         // S9(3)V9(4) — 7
	prmTax            = int32(555555555)       // S9(7)V99  — 9
	prmFee            = int32(666666666)       // S9(7)V99  — 9
	prmSurcharge      = int32(-777777777)      // S9(7)V99  — 9
	clmPaidIndemnity  = int64(1111111111111)   // S9(11)V99 — 13
	clmPaidExpense    = int64(22222222222)     // S9(9)V99  — 11
	clmReserveIndem   = int64(-3333333333333)  // S9(11)V99 — 13
	clmReserveExpense = int64(44444444444)     // S9(9)V99  — 11
	enrPremiumDelta   = int64(-55555555555)    // S9(9)V99  — 11
	ftrWrittenPremium = int64(123456789012345) // S9(13)V99 — 15
	ftrPaidLoss       = int64(-987654321098765)
)

// extract is a well-formed run of records: a header, `policies` policy terms
// each followed by `details` detail records cycling through all eight types, and
// a trailer whose two counts agree with what is in front of it.
//
// It is a slice rather than bytes so that a test can break one field of one
// record — which is how a reconciliation nobody can make fail is told from one
// that works.
func extract(policies, details int32) []policy.Record {
	recs := []policy.Record{&policy.PxFileHeader{
		FhdRecordType:    "000",
		FhdExtractName:   "PXTRACT DAILY",
		FhdCycleDate:     cycleDate,
		FhdCycleTime:     cycleTime,
		FhdCarrier:       carrier,
		FhdRegion:        "SE",
		FhdSourceSystem:  "POLADMIN",
		FhdRunNumber:     runNumber,
		FhdFormatVersion: 3,
	}}

	for p := int32(1); p <= policies; p++ {
		recs = append(recs, policyRecord(p))

		for d := range details {
			recs = append(recs, detail(p, d))
		}
	}

	return append(recs, &policy.PxFileTrailer{
		FtrRecordType:       "999",
		FtrCycleDate:        cycleDate,
		FtrPolicyCount:      policies,
		FtrDetailCount:      policies * details,
		FtrWrittenPremium:   ftrWrittenPremium,
		FtrPaidLoss:         ftrPaidLoss,
		FtrHashPolicyNumber: 987654321098765,
		FtrRunNumber:        runNumber,
	})
}

// policyNumber is the key a policy term and its details share, under a prefix of
// its own on each of them — which is the duplication this example is about.
func policyNumber(p int32) string {
	return fmt.Sprintf("POL%09d", p)
}

func policyRecord(p int32) *policy.PxPolicy {
	return &policy.PxPolicy{
		PlcRecordType:     "PLC",
		PlcPolicyNumber:   policyNumber(p),
		PlcCarrier:        carrier,
		PlcTermEffDate:    20260101,
		PlcSeq:            p,
		PlcProductCode:    "HO0003",
		PlcLob:            "PROP",
		PlcState:          "GA",
		PlcTermExpDate:    20270101,
		PlcIssueDate:      20251215,
		PlcStatus:         "IF",
		PlcCancelDate:     0,
		PlcCancelReason:   "",
		PlcAgencyCode:     "AG000123",
		PlcProducerCode:   "PR000456",
		PlcBillMethod:     "DB",
		PlcPayPlan:        "M12",
		PlcTermMonths:     12,
		PlcWrittenPremium: plcWrittenPremium,
		PlcRenewalCount:   3,
	}
}

// detail is the d'th detail record behind policy p, of the d'th of the eight
// types.
func detail(p, d int32) policy.Record {
	number := policyNumber(p)
	seq := p

	switch d % detailTypes {
	case 0:
		return &policy.PxInsured{
			InsRecordType: "INS", InsPolicyNumber: number, InsCarrier: carrier,
			InsTermEffDate: 20260101, InsSeq: seq, InsRole: "NI",
			InsLastName: "MCALLISTER", InsFirstName: "ROSALIND", InsMiddleInitial: "Q",
			InsAddressLine1: "1200 PEACHTREE STREET NE", InsAddressLine2: "SUITE 400",
			InsCity: "ATLANTA", InsState: "GA", InsPostalCode: "303091234",
			InsCountry: "USA", InsBirthDate: 19790214, InsGender: "F",
			InsMaritalStatus: "M", InsTaxIdLast4: "8821", InsEmailOnFile: "Y",
		}
	case 1:
		return &policy.PxLocation{
			LocRecordType: "LOC", LocPolicyNumber: number, LocCarrier: carrier,
			LocTermEffDate: 20260101, LocSeq: seq, LocLocationNumber: 1,
			LocAddressLine1: "88 RIVERBEND DRIVE", LocCity: "MARIETTA",
			LocState: "GA", LocPostalCode: "300608812", LocCountyCode: "13067",
			LocTerritory: "0412", LocProtectionClass: "04",
			LocConstructionCode: "F1", LocOccupancyCode: "OWN",
			LocYearBuilt: 1998, LocSquareFeet: 2840, LocNumberOfStories: 2,
			LocDistanceToFire: locDistanceToFire, LocFloodZone: "X500",
		}
	case 2:
		return &policy.PxVehicle{
			VehRecordType: "VEH", VehPolicyNumber: number, VehCarrier: carrier,
			VehTermEffDate: 20260101, VehSeq: seq, VehUnitNumber: 1,
			VehVin: "1HGCM82633A004352", VehModelYear: 2023, VehMake: "HONDA",
			VehModel: "ACCORD EX-L", VehBodyStyle: "4DSD", VehVehicleUse: "PL",
			VehGarageState: "GA", VehGaragePostal: "300608812",
			VehAnnualMileage: 12000, VehSymbol: "18", VehAntiTheft: "P",
			VehCostNew: 3421000, VehLienholderName: "SOUTHEAST CREDIT UNION",
			VehLeasedFlag: "N",
		}
	case 3:
		return &policy.PxDriver{
			DrvRecordType: "DRV", DrvPolicyNumber: number, DrvCarrier: carrier,
			DrvTermEffDate: 20260101, DrvSeq: seq, DrvDriverNumber: 1,
			DrvLastName: "MCALLISTER", DrvFirstName: "ROSALIND",
			DrvBirthDate: 19790214, DrvGender: "F", DrvMaritalStatus: "M",
			DrvLicenseState: "GA", DrvLicenseNumber: "GA0088211934",
			DrvLicenseDate: 19960301, DrvRelationToIns: "IN", DrvRatedUnit: 1,
			DrvGoodStudent: "N", DrvTrainingFlag: "Y", DrvPoints: 0,
			DrvExcludedFlag: "N",
		}
	case 4:
		return &policy.PxCoverage{
			CovRecordType: "COV", CovPolicyNumber: number, CovCarrier: carrier,
			CovTermEffDate: 20260101, CovSeq: seq,
			// Both present, at most one meaningful. See [coverage].
			CovLocationNumber: 1, CovUnitNumber: 0,
			CovCoverageCode: "DWELL", CovCoverageDesc: "DWELLING",
			CovLimit1: covLimit1, CovLimit2: covLimit2,
			CovDeductible: covDeductible, CovDeductibleType: "FL",
			CovEffDate: 20260101, CovExpDate: 20270101,
			CovFormCode: "HO000300", CovClassCode: "C00412",
			CovRate: covRate, CovExposureBasis: "AM", CovWaiverFlag: "N",
		}
	case 5:
		return &policy.PxPremium{
			PrmRecordType: "PRM", PrmPolicyNumber: number, PrmCarrier: carrier,
			PrmTermEffDate: 20260101, PrmSeq: seq, PrmCoverageCode: "DWELL",
			PrmTransactionCode: "NBIZ", PrmTransactionDate: 20260101,
			PrmAccountingPeriod: 202601, PrmWrittenAmount: prmWritten,
			PrmEarnedAmount: prmEarned, PrmUnearnedAmount: prmUnearned,
			PrmCommissionAmount: prmCommission, PrmCommissionRate: prmCommissionRate,
			PrmTaxAmount: prmTax, PrmFeeAmount: prmFee,
			PrmSurchargeAmount: prmSurcharge, PrmCurrency: "USD",
			PrmGlAccount: "4100001200", PrmStatutoryLine: "0401",
		}
	case 6:
		return &policy.PxClaim{
			ClmRecordType: "CLM", ClmPolicyNumber: number, ClmCarrier: carrier,
			ClmTermEffDate: 20260101, ClmSeq: seq, ClmClaimNumber: "CLM2026000917",
			ClmLossDate: 20260318, ClmReportedDate: 20260319, ClmClosedDate: 0,
			ClmStatus: "OP", ClmCauseOfLoss: "WIND", ClmCoverageCode: "DWELL",
			ClmLocationNumber: 1, ClmUnitNumber: 0, ClmAdjusterCode: "ADJ00551",
			ClmPaidIndemnity: clmPaidIndemnity, ClmPaidExpense: clmPaidExpense,
			ClmReserveIndemnity: clmReserveIndem, ClmReserveExpense: clmReserveExpense,
			ClmSubrogationFlag: "N",
		}
	default:
		return &policy.PxEndorsement{
			EnrRecordType: "ENR", EnrPolicyNumber: number, EnrCarrier: carrier,
			EnrTermEffDate: 20260101, EnrSeq: seq, EnrEndorsementNumber: 1,
			EnrFormNumber: "HO0430", EnrFormEdition: "052011",
			EnrFormTitle: "LIMITED WATER BACK-UP COVERAGE",
			EnrEffDate:   20260401, EnrExpDate: 20270101,
			EnrTransactionCode: "ENDO", EnrTransactionDate: 20260325,
			EnrPremiumDelta: enrPremiumDelta, EnrLocationNumber: 1,
			EnrUnitNumber: 0, EnrCoverageCode: "WBACKU", EnrMandatoryFlag: "N",
			EnrPrintFlag: "Y", EnrStateFiled: "GA",
		}
	}
}

// encoded is a run of records as the bytes of an FB dataset in cp037.
//
// This repository commits no `.dat` fixture for the same reason the ledger
// example does not: an extract is EBCDIC on a fixed-block dataset, which is not
// a thing to read in a diff. The generated writer is the shortest way to make
// yourself one to run the conversion against.
func encoded(t *testing.T, recs []policy.Record) []byte {
	t.Helper()

	var b bytes.Buffer

	w, err := policy.NewWriter(&b, policy.Encoding())
	if err != nil {
		t.Fatalf("policy.NewWriter: %v", err)
	}

	for i, rec := range recs {
		if err := w.Write(rec); err != nil {
			t.Fatalf("writing record %d (%T): %v", i+1, rec, err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("closing the extract: %v", err)
	}

	return b.Bytes()
}

// converted runs the conversion over a run of records at the given row-group
// bound, and returns the file it wrote.
//
// The bytes come back whether or not the conversion succeeded, because what a
// failed conversion left behind is half of what the reconciliation tests assert.
func converted(t *testing.T, recs []policy.Record, rows int) ([]byte, error) {
	t.Helper()

	r, err := policy.NewReader(bytes.NewReader(encoded(t, recs)), policy.Encoding())
	if err != nil {
		t.Fatalf("policy.NewReader: %v", err)
	}

	var out bytes.Buffer

	// convert first and read the buffer afterwards. `return out.Bytes(),
	// convert(...)` evaluates left to right, so it hands back the bytes as they
	// stood before the conversion ran — which is nothing, and which reads in a
	// failing test as "the conversion wrote no file" whatever it wrote.
	err = convert(r, &out, rows)

	return out.Bytes(), err
}

// table is the file a well-formed extract converts to, at a bound small enough
// that a test can watch the row group rotate.
func table(t *testing.T, policies, details int32, rows int) []byte {
	t.Helper()

	// The buffer has to be read after convert returns rather than captured
	// before it, which converted cannot do for a caller that wants only the
	// bytes — so this repeats the three lines instead of returning a slice
	// taken too early.
	r, err := policy.NewReader(bytes.NewReader(encoded(t, extract(policies, details))), policy.Encoding())
	if err != nil {
		t.Fatalf("policy.NewReader: %v", err)
	}

	var out bytes.Buffer

	if err := convert(r, &out, rows); err != nil {
		t.Fatalf("convert: %v", err)
	}

	return out.Bytes()
}

// rowsOf reads a whole Parquet file back.
//
// What comes back is the rows actually decoded, not the row count the footer
// claims. The two are the same on a well-formed file and they are exactly what
// the caller is holding against each other on a broken one.
func rowsOf(t *testing.T, b []byte) []record {
	t.Helper()

	r := parquet.NewGenericReader[record](bytes.NewReader(b))
	defer func() { _ = r.Close() }()

	rows := make([]record, r.NumRows())
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

// opened is the written file, as a Parquet reader sees it.
func opened(t *testing.T, b []byte) *parquet.File {
	t.Helper()

	f, err := parquet.OpenFile(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("opening the table: %v", err)
	}

	return f
}

// leavesOf is how many columns a node contributes to the file. A group is
// structure and contributes its children; only a leaf is a column.
func leavesOf(node parquet.Node) int {
	if node.Leaf() {
		return 1
	}

	n := 0
	for _, field := range node.Fields() {
		n += leavesOf(field)
	}

	return n
}

// TestEveryRecordTypeIsOneSchema is the decision this example exists to carry:
// eleven record types, one Parquet file, one schema.
//
// A Parquet file carries exactly one schema, and merging is what makes the table
// wide and sparse. That is not a compromise — it is the primary thing the format
// is for. A query touching five of the 197 columns pays for five, and a column
// null for every row of a row group compresses to nothing. The whole cost lands
// on the writer, once, and convert.go is sized to pay it.
//
// The schema's *top level* is exactly the eleven groups and nothing else. That
// is what says no key was minted, nothing was denormalized onto anything, and no
// column was invented: every column of this file is a field of `pxtract.cpy`.
func TestEveryRecordTypeIsOneSchema(t *testing.T) {
	f := opened(t, table(t, 3, detailTypes, 64))

	want := []string{
		"px_file_header", "px_policy", "px_insured", "px_location", "px_vehicle",
		"px_driver", "px_coverage", "px_premium", "px_claim", "px_endorsement",
		"px_file_trailer",
	}

	fields := f.Schema().Fields()
	if len(fields) != recordTypes {
		t.Fatalf("the schema carries %d top-level fields, want %d: one group per record type and nothing else, because a minted key or a denormalized parent would be a %d'th", len(fields), recordTypes, recordTypes+1)
	}

	for i, field := range fields {
		if field.Name() != want[i] {
			t.Errorf("top-level field %d is %q, want %q", i, field.Name(), want[i])
		}

		if !field.Optional() {
			t.Errorf("%s is required: a row is one record, so ten of the eleven groups are null on it and a required group would make every row carry all eleven", field.Name())
		}
	}
}

// TestTheColumnCountIsTheSchemaAndNotANumberWrittenDown is the load-bearing
// test of this file, and it is the one that makes the row-group bound a
// derivation rather than a preference.
//
// [columns] is an *input* to the memory model — peak is a·C·(N/R) + W·R, and W
// is itself 5·C plus the record — so a C that has drifted from the schema sizes
// the row group for a table that does not exist. It cannot be computed from the
// schema in the constant expression that needs it, so it is written down and
// held here against the file the conversion actually produced.
//
// Add a record type to `pxtract.cpy` and this fails. That is the point: the
// number in convert.go and the number in README.md both have to move, and the
// row group has to be re-derived from the new one.
func TestTheColumnCountIsTheSchemaAndNotANumberWrittenDown(t *testing.T) {
	f := opened(t, table(t, 3, detailTypes, 64))

	if got := leavesOf(f.Schema()); got != columns {
		t.Errorf("the written schema has %d columns and the memory model is derived from %d: peak is a·C·(N/R) + W·R and C is this number, so a row group sized from the wrong one is sized for a table that is not there", got, columns)
	}

	// Every leaf sits under an optional group, so every leaf is an optional
	// column and pays parquet-go's five bytes a row whether or not there is a
	// value under it — which is where 985 of this file's 1,256 bytes a row go.
	// A leaf that were *not* optional would break the W in the same derivation,
	// silently and in the cheap direction.
	for _, group := range f.Schema().Fields() {
		for _, leaf := range group.Fields() {
			if !leaf.Leaf() {
				t.Errorf("%s.%s is a group: this schema is one level of nesting deep, and a second level changes neither C nor W but does change what a reader has to walk", group.Name(), leaf.Name())
			}
		}
	}
}

// TestTheRowGroupBoundIsDerivedAndNotChosen holds [rowsPerRowGroup] to the rule
// it claims to come from.
//
// R* is where the two terms of peak(N, R) = a·C·(N/R) + W·R are equal: size the
// row group so that the footer you have accumulated is the same size as the row
// group you are holding. At [maxRecords] that is what this asserts, to within
// the integer division that produced the constant.
//
// It is not a tautology dressed as a test. The constants are stated
// independently — a, C, W and the budget — and this is the only place that says
// the number derived from them is at the bottom of the curve rather than
// somewhere on it. Substituting the ledger conversion's 64 gives a footer of
// 268,298,240 bytes against an open row group of 80,384 — the two terms out by a
// factor of 3,338, which is what "not a production number" means when it is
// checked rather than said.
func TestTheRowGroupBoundIsDerivedAndNotChosen(t *testing.T) {
	retained := int64(retainedPerColumnPerRowGroup) * columns * (maxRecords / rowsPerRowGroup)
	buffered := int64(bufferedPerRow) * rowsPerRowGroup

	if diff := retained - buffered; diff > buffered/100 || -diff > buffered/100 {
		t.Errorf("at %d records the footer retains %d bytes and the open row group holds %d: R* is where those two are equal, so a bound that is not is one somebody chose", maxRecords, retained, buffered)
	}

	if peak := retained + buffered; peak > memoryBudget {
		t.Errorf("peak at %d records is %d bytes against a budget of %d: maxRecords is defined as the count at which the budget is reached, so exceeding it means the arithmetic that produced one of them is wrong", maxRecords, peak, memoryBudget)
	}

	// The other wall, and the one #304 hit first: a Parquet file holds at most
	// 32767 row groups, so the bound has to clear N/32767 as well as sit at R*.
	// It does, by a wide margin — the ceiling and the linear heap are one
	// mistake with one fix rather than two walls with two.
	if floor := int64(maxRecords)/32767 + 1; rowsPerRowGroup < floor {
		t.Errorf("the bound is %d and %d records need at least %d rows a group to stay inside 32767 of them", rowsPerRowGroup, maxRecords, floor)
	}
}

// TestARowIsOneRecordAndCarriesItsTypeWithIt is the merged table's other half:
// with eleven record types in one schema, a row has to say which one it was
// without consulting anything else.
//
// Exactly one group is non-nil on every row, and it is the one for the record
// that was read. Two would mean the mapping wrote into a row it had already
// filled; none would mean a discarded error had appended a row of nulls, which
// on a table this sparse is not even conspicuous.
func TestARowIsOneRecordAndCarriesItsTypeWithIt(t *testing.T) {
	const policies, details = 3, detailTypes

	recs := extract(policies, details)
	rows := rowsOf(t, table(t, policies, details, 64))

	if len(rows) != len(recs) {
		t.Fatalf("%d records converted to %d rows: the grain of this table is one record", len(recs), len(rows))
	}

	for i, row := range rows {
		got := groupsOf(row)
		if len(got) != 1 {
			t.Errorf("row %d carries %v, want exactly one group: a row is one record", i, got)

			continue
		}

		if want := groupNameOf(recs[i]); got[0] != want {
			t.Errorf("row %d is %s and the record was a %T, want %s", i, got[0], recs[i], want)
		}
	}
}

// groupsOf names the groups a row carries, which is what recovers its record
// type.
func groupsOf(row record) []string {
	present := make([]string, 0, 1)

	for name, ok := range map[string]bool{
		"px_file_header": row.FileHeader != nil, "px_policy": row.Policy != nil,
		"px_insured": row.Insured != nil, "px_location": row.Location != nil,
		"px_vehicle": row.Vehicle != nil, "px_driver": row.Driver != nil,
		"px_coverage": row.Coverage != nil, "px_premium": row.Premium != nil,
		"px_claim": row.Claim != nil, "px_endorsement": row.Endorsement != nil,
		"px_file_trailer": row.FileTrailer != nil,
	} {
		if ok {
			present = append(present, name)
		}
	}

	return present
}

// groupNameOf is the group a record of this type belongs in, worked out from the
// record rather than from the conversion — a second reading of the same mapping,
// which is what makes the comparison above an assertion instead of an identity.
func groupNameOf(rec policy.Record) string {
	switch rec.(type) {
	case *policy.PxFileHeader:
		return "px_file_header"
	case *policy.PxPolicy:
		return "px_policy"
	case *policy.PxInsured:
		return "px_insured"
	case *policy.PxLocation:
		return "px_location"
	case *policy.PxVehicle:
		return "px_vehicle"
	case *policy.PxDriver:
		return "px_driver"
	case *policy.PxCoverage:
		return "px_coverage"
	case *policy.PxPremium:
		return "px_premium"
	case *policy.PxClaim:
		return "px_claim"
	case *policy.PxEndorsement:
		return "px_endorsement"
	case *policy.PxFileTrailer:
		return "px_file_trailer"
	default:
		return fmt.Sprintf("no group holds a %T", rec)
	}
}

// TestNoRowFillsMoreThanATenthOfTheTable is the sparsity this example is for,
// asserted against the written file rather than argued in a README.
//
// The widest record type populates 20 of 197 columns, so no row of any extract
// fills more than 10.2% of the table it merges into. That is what makes the
// writer's peak a function of the schema rather than of the data, and it is the
// whole reason the row-group bound has to be derived rather than guessed.
//
// It counts **the file's own nulls** — the `NullCount` every `ColumnIndex`
// carries per page, which is the same retained metadata the memory model's `a`
// is mostly made of — rather than counting non-nil pointers on the Go rows. The
// distinction matters: the claim is about what Parquet materialised, and a row
// that arrived as one non-nil group would satisfy a Go-side count even if the
// writer had emitted values for columns the record does not have.
func TestNoRowFillsMoreThanATenthOfTheTable(t *testing.T) {
	written := table(t, 3, detailTypes, 64)
	f := opened(t, written)

	// No record type declares more than the widest one, so no row can fill more
	// than that many columns however the values fall.
	for _, group := range f.Schema().Fields() {
		if n := leavesOf(group); n > widestRecord {
			t.Errorf("%s carries %d columns and the widest record type of this file declares %d", group.Name(), n, widestRecord)
		}
	}

	rows, values := int64(0), int64(0)

	for _, group := range f.RowGroups() {
		rows += group.NumRows()

		for _, chunk := range group.ColumnChunks() {
			index, err := chunk.ColumnIndex()
			if err != nil {
				t.Fatalf("reading the column index of column %d: %v", chunk.Column(), err)
			}

			nulls := int64(0)
			for page := range index.NumPages() {
				nulls += index.NullCount(page)
			}

			values += group.NumRows() - nulls
		}
	}

	// What the extract actually carries: one header at 9 columns, one trailer at
	// 8, and three policies each with one of every detail type at 20 apiece.
	want := int64(9 + 8 + 3*(1+detailTypes)*widestRecord)

	if values != want {
		t.Errorf("the file materialised %d non-null values over %d rows, want %d: a row carries the columns of its own record type and nothing else", values, rows, want)
	}

	// The claim, stated as the ratio the sibling README makes it: 10.2%.
	if cells := rows * columns; values*1000 > cells*102 {
		t.Errorf("%d of %d cells are non-null, which is %.1f%% of the table: this example's claim is that a merged wide table is nine tenths empty on every row", values, cells, 100*float64(values)/float64(cells))
	}
}

// TestEveryRecordReadIsARowWritten is the last partial row group, and it is run
// over a file whose record count is not a multiple of the bound because that is
// the only kind of file an unwritten one loses rows from.
//
// It is also where the bound stops being a claim. The bound is the writer's —
// parquet.MaxRowsPerRowGroup, closed inside GenericWriter.Write and finished by
// Close — so a file of 999 records at 64 comes back as sixteen row groups of at
// most sixty-four rows. A conversion that passed no option would come back as
// one, because parquet-go's default is math.MaxInt64 rather than a bound, and
// that is what this reads the written file to rule out rather than trust.
//
// Sixty-four is a test's bound and not this conversion's: the default is
// [rowsPerRowGroup], which is 1,670 times larger and which a fixture cannot
// afford to fill. That the two are different is exactly why the bound is an
// argument rather than a constant read from inside [convert].
//
// What it does **not** rule out is the trap README.md is about: a caller-side
// batch flushed every 64 rows produces this same file, byte for byte, because
// the flush and the option close the row group at the same point. The absence of
// a batch is structural — there is one row in the slice [convert] hands Write —
// and no assertion over the output can see it. Said here so that it is not
// mistaken for something this test covers.
func TestEveryRecordReadIsARowWritten(t *testing.T) {
	// One policy carrying 996 details, plus the header and the trailer.
	const policies, details, rows = 1, 996, 64

	const records = 2 + policies*(1+details)

	if records%rows == 0 {
		t.Fatalf("this fixture has %d records and the row group holds %d: a whole number of row groups is the one file an unwritten last one does not lose rows from", records, rows)
	}

	written := table(t, policies, details, rows)

	if got := len(rowsOf(t, written)); got != records {
		t.Errorf("%d records were read and %d rows came back: the last partial row group is what a conversion that never closed the writer drops, on every well-formed file", records, got)
	}

	f := opened(t, written)

	for i, group := range f.RowGroups() {
		if group.NumRows() > rows {
			t.Errorf("row group %d holds %d rows and the bound is %d: peak memory is the open row group, and a row group that outgrows the bound is a parquet.MaxRowsPerRowGroup nothing passed", i, group.NumRows(), rows)
		}
	}

	if want := (records + rows - 1) / rows; len(f.RowGroups()) != want {
		t.Errorf("the table holds %d row groups, want %d: one per %d rows is what parquet.MaxRowsPerRowGroup produces, and one row group is what the default bound of math.MaxInt64 produces", len(f.RowGroups()), want, rows)
	}
}

// TestTheDefaultRowGroupIsTheDerivedOne is the wiring between the derivation and
// the command.
//
// [rowsPerRowGroup] being right is worth nothing if -rows-per-row-group defaults
// to something else, and no test that converts a fixture can notice: at any size
// a fixture can afford, every bound above it produces one row group and they all
// look alike.
//
// So it reads the default out of [run]'s **own** flag set rather than out of a
// copy. That is the whole difficulty here, and the first version of this test
// got it wrong: it rebuilt the flag set locally with rowsPerRowGroup as the
// default and then asserted that default was rowsPerRowGroup, which is an
// identity that holds however run is written. `flag.PrintDefaults` is the way
// in — it renders `(default N)` for a non-zero int, and the usage run writes is
// run's flag set describing itself.
func TestTheDefaultRowGroupIsTheDerivedOne(t *testing.T) {
	usage := usageOf(t)

	if want := fmt.Sprintf("(default %d)", rowsPerRowGroup); !strings.Contains(usage, want) {
		t.Errorf("run's usage does not carry %q, so -rows-per-row-group defaults to something other than the derived number and the memory model is one nothing runs under:\n%s", want, usage)
	}
}

// TestTheCommandDeclaresTheThreeFlagsDocumented is the other half: a default
// asserted on a flag nobody declares would pass against an empty usage.
func TestTheCommandDeclaresTheThreeFlagsDocumented(t *testing.T) {
	usage := usageOf(t)

	for _, name := range []string{"-in", "-out", "-rows-per-row-group"} {
		if !strings.Contains(usage, name) {
			t.Errorf("run's usage does not mention %s, which README.md documents:\n%s", name, usage)
		}
	}
}

// usageOf is what [run] writes when it is asked to describe itself.
//
// -h is a request that succeeded, so run returns nil and the flag set has
// written the usage to the writer it was handed — which is run's own flag set
// rendering its own defaults, and the only thing here that is not a copy.
func usageOf(t *testing.T) string {
	t.Helper()

	var usage bytes.Buffer

	if err := run([]string{"-h"}, &usage); err != nil {
		t.Fatalf("run -h: %v", err)
	}

	if usage.Len() == 0 {
		t.Fatal("run -h wrote no usage, so there is nothing here to assert against")
	}

	return usage.String()
}

// TestAMissingInputIsRefusedByName is the other flag path a first run meets.
func TestAMissingInputIsRefusedByName(t *testing.T) {
	err := run(nil, io.Discard)
	if err == nil {
		t.Fatal("run with no -in converted something")
	}

	if !strings.Contains(err.Error(), "-in") {
		t.Errorf("the failure is %q, and it does not name the flag that is missing", err)
	}
}

// TestAFlagTheFlagSetHasAlreadyReportedIsNotReportedTwice is what
// [errAlreadyReported] is for: the flag set writes its own message and the
// usage, so an error carrying a second copy of it would have main print the
// same thing twice.
func TestAFlagTheFlagSetHasAlreadyReportedIsNotReportedTwice(t *testing.T) {
	var reported bytes.Buffer

	err := run([]string{"-not-a-flag"}, &reported)
	if err == nil {
		t.Fatal("an unknown flag was accepted")
	}

	if !strings.Contains(reported.String(), "-not-a-flag") {
		t.Errorf("the flag set did not report the unknown flag itself:\n%s", reported.String())
	}

	if err.Error() != "" {
		t.Errorf("the error carries %q, and main prints whatever it carries: the flag set has already written that message", err)
	}
}

// TestANonPositiveRowGroupIsRefused is the one way the flag can be set to
// something that reads as a smaller bound and is really no bound at all:
// parquet-go takes a non-positive cap as unlimited, so `-rows-per-row-group 0`
// would grow one row group for the whole file.
func TestANonPositiveRowGroupIsRefused(t *testing.T) {
	in := extractOnDisk(t, extract(1, detailTypes))
	dir, out := outputPath(t)

	err := run([]string{"-in", in, "-out", out, "-rows-per-row-group", "0"}, io.Discard)
	if err == nil {
		t.Fatal("a row group of zero rows was accepted, and parquet-go reads it as no bound at all")
	}

	if !strings.Contains(err.Error(), "-rows-per-row-group") {
		t.Errorf("the failure is %q, and it does not name the flag that is wrong", err)
	}

	// The refusal is ahead of every side effect, including the mkdir that would
	// have made -out's directory, so there is nothing to read back — which is
	// itself the assertion.
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the rejected run created %s: a flag refused before it is used should leave the filesystem alone", dir)
	}
}

// TestTheConversionWritesOneFile is the decision the story this file lands is
// named for, asserted on the disk rather than in a buffer: eleven record types,
// three grains, **one file**.
//
// One table and one file are separate axes, and this is only the second of them.
// A lake table is normally one schema across many files, and a converter that
// partitioned this output by cycle date would still be writing one schema. That
// is a decision this example deliberately does not make; see README.md.
func TestTheConversionWritesOneFile(t *testing.T) {
	in := extractOnDisk(t, extract(2, detailTypes))
	dir, out := outputPath(t)

	if err := run([]string{"-in", in, "-out", out}, io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}

	names := entryNames(t, dir)

	if len(names) != 1 || names[0] != filepath.Base(out) {
		t.Fatalf("the conversion left %v, want exactly [%q]: eleven record types are one schema, and one schema here is one file", names, filepath.Base(out))
	}

	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading the table back: %v", err)
	}

	if want := 2 + 2*(1+detailTypes); len(rowsOf(t, written)) != want {
		t.Errorf("the table holds %d rows, want %d: every record of the extract is a row, including the header and the trailer", len(rowsOf(t, written)), want)
	}
}

// extractOnDisk writes a run of records to a file, for the tests that go through
// [run].
func extractOnDisk(t *testing.T, recs []policy.Record) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "pxtract.dat")
	if err := os.WriteFile(path, encoded(t, recs), 0o600); err != nil {
		t.Fatalf("writing the fixture extract: %v", err)
	}

	return path
}

// outputPath is a -out under a directory that is not there yet, because the
// invocation README.md documents has to run on a machine that has never run it.
func outputPath(t *testing.T) (dir, out string) {
	t.Helper()

	dir = filepath.Join(t.TempDir(), "out")

	return dir, filepath.Join(dir, "pxtract.parquet")
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

// TestAFailedRunLeavesNoFileBehind is the on-disk half of every reconciliation:
// what is there after a failed run is not an unopenable file but no file.
func TestAFailedRunLeavesNoFileBehind(t *testing.T) {
	recs := extract(2, detailTypes)
	trailerOf(t, recs).FtrPolicyCount = 99

	in := extractOnDisk(t, recs)
	dir, out := outputPath(t)

	err := run([]string{"-in", in, "-out", out}, io.Discard)
	if err == nil {
		t.Fatal("a file whose FTR-POLICY-COUNT disagrees with its own body converted without complaint")
	}

	if !strings.Contains(err.Error(), "FTR-POLICY-COUNT") {
		t.Errorf("the failure is %q, and it does not name FTR-POLICY-COUNT", err)
	}

	if names := entryNames(t, dir); len(names) != 0 {
		t.Errorf("the failed run left %v behind, want nothing: a half-converted file that survives is one somebody queries", names)
	}
}

// trailerOf is the PX-FILE-TRAILER of a run of records, so that a test can break
// one of its control totals.
func trailerOf(t *testing.T, recs []policy.Record) *policy.PxFileTrailer {
	t.Helper()

	trl, ok := recs[len(recs)-1].(*policy.PxFileTrailer)
	if !ok {
		t.Fatalf("the last record of the fixture is a %T, and this layout ends in a PX-FILE-TRAILER", recs[len(recs)-1])
	}

	return trl
}

// headerOf is the PX-FILE-HEADER, for the same reason.
func headerOf(t *testing.T, recs []policy.Record) *policy.PxFileHeader {
	t.Helper()

	hdr, ok := recs[0].(*policy.PxFileHeader)
	if !ok {
		t.Fatalf("the first record of the fixture is a %T, and this layout begins with a PX-FILE-HEADER", recs[0])
	}

	return hdr
}

// TestTheTrailerCountsAreReconciled is the pair of checks the copybook's own
// names decide, and they run before the footer is written — so a conversion that
// does not reconcile leaves a file no reader will open rather than one a query
// would happily return wrong answers from.
//
// That ordering is the whole trick: a Parquet file is its footer, so "fail
// before the footer" and "leave nothing queryable" are the same sentence.
//
// The fixture is [failingFixture] rather than the small one the other tests use,
// and that is what makes [assertUnopenable] mean something here: a conversion
// that fails before any row group has been flushed leaves an empty buffer, and
// "an empty buffer is not a Parquet file" is true of nothing in particular.
func TestTheTrailerCountsAreReconciled(t *testing.T) {
	cases := []struct {
		name   string
		damage func(*policy.PxFileTrailer)
		names  string
	}{
		{"policy count", func(trl *policy.PxFileTrailer) { trl.FtrPolicyCount++ }, "FTR-POLICY-COUNT"},
		{"detail count", func(trl *policy.PxFileTrailer) { trl.FtrDetailCount-- }, "FTR-DETAIL-COUNT"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			recs, rows := failingFixture()
			c.damage(trailerOf(t, recs))

			written, err := converted(t, recs, rows)
			if err == nil {
				t.Fatalf("a file whose %s disagrees with its own body converted without complaint", c.names)
			}

			if !strings.Contains(err.Error(), c.names) {
				t.Errorf("the reconciliation failure is %q, and it does not name %s", err, c.names)
			}

			assertUnopenable(t, written)
		})
	}
}

// TestTheHeaderAndTrailerMustAgreeOnTheRun is the third and fourth checks, and
// they are the cheapest real ones this file admits: the cycle date and the run
// number are the same two items on both ends of the extract, so a file whose two
// disagree is two extracts that arrived as one.
func TestTheHeaderAndTrailerMustAgreeOnTheRun(t *testing.T) {
	cases := []struct {
		name   string
		damage func(*policy.PxFileHeader)
		names  string
	}{
		{"cycle date", func(hdr *policy.PxFileHeader) { hdr.FhdCycleDate = 20260826 }, "FHD-CYCLE-DATE"},
		{"run number", func(hdr *policy.PxFileHeader) { hdr.FhdRunNumber = 4208 }, "FHD-RUN-NUMBER"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			recs, rows := failingFixture()
			c.damage(headerOf(t, recs))

			written, err := converted(t, recs, rows)
			if err == nil {
				t.Fatalf("a file whose header and trailer disagree about the %s converted without complaint", c.name)
			}

			if !strings.Contains(err.Error(), c.names) {
				t.Errorf("the failure is %q, and it does not name %s", err, c.names)
			}

			assertUnopenable(t, written)
		})
	}
}

// TestTheMoneyTotalsAreNotReconciled is a decision asserted the only way a
// declined check can be: a file whose money totals are nonsense converts, and
// the totals land on the trailer row exactly as they were read.
//
// **Nothing in `pxtract.cpy` says which grain FTR-WRITTEN-PREMIUM totals.**
// PLC-WRITTEN-PREMIUM is `S9(9)V99` on a policy term and PRM-WRITTEN-AMOUNT is
// `S9(9)V99` on a premium transaction, and the trailer's own item is `S9(13)V99`
// — a name shared by three items of two grains and three widths. A converter
// that picked one would fail on every file that meant the other, which is the
// opposite of what the ledger conversion's TRL-NET check does: there the
// copybook admitted exactly one reading and the check was falsifiable.
//
// FTR-HASH-POLICY-NUMBER is declined for a sharper reason: it is a hash total
// over `PIC X(12)`, so reproducing it means knowing how the producing system
// turns an alphanumeric key into a number, and no copybook carries that.
//
// A check that has to invent a semantic is a check that fails on files that are
// fine, and this repository's line is that a semantic which only *checks* what is
// stored can be assumed when it is stated and falsifiable. These three are
// neither.
func TestTheMoneyTotalsAreNotReconciled(t *testing.T) {
	recs := extract(2, detailTypes)

	trl := trailerOf(t, recs)
	trl.FtrWrittenPremium = 1
	trl.FtrPaidLoss = 2
	trl.FtrHashPolicyNumber = 3

	written, err := converted(t, recs, 64)
	if err != nil {
		t.Fatalf("a file whose money totals total nothing in it was refused: %v", err)
	}

	rows := rowsOf(t, written)

	last := rows[len(rows)-1].FileTrailer
	if last == nil {
		t.Fatal("the last row is not the trailer, and every record of this file is a row")
	}

	if last.FtrWrittenPremium != 1 || last.FtrPaidLoss != 2 || last.FtrHashPolicyNumber != 3 {
		t.Errorf("the trailer row carries (%d, %d, %d), want (1, 2, 3): a total this conversion does not check is one it stores as it was read", last.FtrWrittenPremium, last.FtrPaidLoss, last.FtrHashPolicyNumber)
	}
}

// TestTheControlTotalsDoNotPromote is the other half of storing them: they sit
// on the trailer's own row and nowhere else.
//
// This is what a merged table buys that the ledger conversion could not have. A
// summary denormalized onto every row makes SUM(ftr_written_premium) return the
// total times the row count — a wrong answer that looks like a right one — so
// the ledger conversion had to drop its trailer's fields entirely. Here the
// trailer is a row, the summary is on it once, and an aggregate over the column
// returns the file's own number exactly once.
func TestTheControlTotalsDoNotPromote(t *testing.T) {
	trailers := 0

	for i, row := range rowsOf(t, table(t, 3, detailTypes, 64)) {
		if row.FileTrailer == nil {
			continue
		}

		trailers++

		if i != 2+3*(1+detailTypes)-1 {
			t.Errorf("a trailer row is at %d, and this extract carries one at the end", i)
		}
	}

	if trailers != 1 {
		t.Errorf("%d rows carry a px_file_trailer, want 1: a control total on more than one row is a total multiplied by the rows it is on", trailers)
	}
}

// TestAFileWithNoTrailerIsReported is what a reconciliation that has nothing to
// reconcile against has to do.
//
// The generated reader will not produce such a file — `policy.sexpr` ends in
// PX-FILE-TRAILER and a stream that stops short is reported as truncated — which
// is exactly why [recordSource] exists: the case is unreachable through the
// reader and reachable through the interface.
func TestAFileWithNoTrailerIsReported(t *testing.T) {
	src := &oneOff{records: extract(1, detailTypes)[:2]}

	var written bytes.Buffer

	err := convert(src, &written, 64)
	if err == nil {
		t.Fatal("an extract with no trailer converted without complaint, and there was nothing to balance it on")
	}

	if !strings.Contains(err.Error(), "PX-FILE-TRAILER") {
		t.Errorf("the failure is %q, and it does not say what was missing", err)
	}
}

// TestAMappingErrorFailsTheConversion is the defect a converter written by eye
// makes: it discards the error and appends the zero value, which on this schema
// is a row whose every group is null.
//
// On a narrow table that row is conspicuous. On a table that is nine tenths
// empty on every row it is not, which is why this one is an error rather than a
// row.
func TestAMappingErrorFailsTheConversion(t *testing.T) {
	// The bad record goes near the end, so that several row groups have been
	// flushed before the conversion gives up — which is what makes the
	// unopenability below a statement about bytes that were written.
	good, rows := failingFixture()
	ordinal := len(good) - 1

	src := &oneOff{records: append(append([]policy.Record{}, good[:ordinal]...), unmappable{})}

	var written bytes.Buffer

	err := convert(src, &written, rows)
	if err == nil {
		t.Fatal("a record none of the eleven types describes converted without complaint")
	}

	if want := fmt.Sprintf("record %d", ordinal+1); !strings.Contains(err.Error(), want) {
		t.Errorf("the mapping failure is %q, and it does not say which record it was — %q", err, want)
	}

	assertUnopenable(t, written.Bytes())
}

// unmappable is a record no transition of this layout admits, which is what a
// twelfth record type added to the layout and not to this conversion would look
// like from here.
type unmappable struct {
	policy.Record
}

// oneOff is a record source that hands out a fixed list and then reports the end
// of the file.
type oneOff struct {
	records []policy.Record
}

func (s *oneOff) Next() (policy.Record, error) {
	if len(s.records) == 0 {
		return nil, io.EOF
	}

	rec := s.records[0]
	s.records = s.records[1:]

	return rec, nil
}

// failingFixture is a run of records and a row-group bound chosen so that
// several row groups have been written by the time a reconciliation at the end
// of the file fails.
//
// 103 records at 32 rows a group is three full groups and a partial one. Every
// test that ends in [assertUnopenable] uses it, because the assertion is about
// *bytes that were written* being unreadable without a footer — and a fixture
// that never reaches the bound leaves no bytes at all, which passes for the
// wrong reason.
func failingFixture() (recs []policy.Record, rows int) {
	const details, bound = 100, 32

	recs = extract(1, details)

	if len(recs) <= bound {
		panic(fmt.Sprintf("the fixture is %d records at a bound of %d: nothing is flushed before the failure and assertUnopenable asserts nothing", len(recs), bound))
	}

	return recs, bound
}

// assertUnopenable requires that a failed conversion left nothing a reader will
// accept. A Parquet file is its footer, and a conversion that returns an error
// before writing one leaves bytes no engine will read as a table.
//
// It refuses an empty buffer rather than passing on one. Nothing written is a
// true answer to a different question — see [failingFixture] — and letting it
// through is how this assertion goes quiet.
func assertUnopenable(t *testing.T, file []byte) {
	t.Helper()

	if len(file) == 0 {
		t.Error("the failed conversion wrote no bytes at all, so this asserts nothing: use failingFixture, whose row groups are flushed before the failure")

		return
	}

	if _, err := parquet.OpenFile(bytes.NewReader(file), int64(len(file))); err == nil {
		t.Error("the table opened after a failed conversion: a half-converted file that reads as a table is one somebody queries")
	}
}

// TestEveryScaledItemIsAnnotatedDecimal is the mapping that looks like it needs
// a conversion and does not.
//
// cpybkc-gen-go writes a scaled item as the unscaled integer with the scale in
// its doc comment, which is precisely what DECIMAL(p,s) is, so the mapping is an
// annotation and nothing here multiplies or divides by a hundred. Reading the
// annotation out of the file's own schema is what makes that a fact rather than
// a plausible sentence: the schema is what a query engine reads, and the Go
// struct tag is not.
//
// All twenty-one scaled items are here rather than a representative two. They do
// not share a scale — COV-RATE is five places and PRM-COMMISSION-RATE is four,
// against two everywhere else — and LOC-DISTANCE-TO-FIRE is `PIC 9(3)V9`, which
// is scaled, unsigned and *not* COMP-3. A conversion that read the scale off the
// storage rather than off the item would get that last one wrong.
func TestEveryScaledItemIsAnnotatedDecimal(t *testing.T) {
	written := table(t, 1, detailTypes, 64)

	cases := []struct {
		path             string
		precision, scale int32
	}{
		{"px_policy.plc_written_premium", 11, 2},
		{"px_location.loc_distance_to_fire", 4, 1},
		{"px_coverage.cov_limit_1", 13, 2},
		{"px_coverage.cov_limit_2", 13, 2},
		{"px_coverage.cov_deductible", 9, 2},
		{"px_coverage.cov_rate", 10, 5},
		{"px_premium.prm_written_amount", 11, 2},
		{"px_premium.prm_earned_amount", 11, 2},
		{"px_premium.prm_unearned_amount", 11, 2},
		{"px_premium.prm_commission_amount", 9, 2},
		{"px_premium.prm_commission_rate", 7, 4},
		{"px_premium.prm_tax_amount", 9, 2},
		{"px_premium.prm_fee_amount", 9, 2},
		{"px_premium.prm_surcharge_amount", 9, 2},
		{"px_claim.clm_paid_indemnity", 13, 2},
		{"px_claim.clm_paid_expense", 11, 2},
		{"px_claim.clm_reserve_indemnity", 13, 2},
		{"px_claim.clm_reserve_expense", 11, 2},
		{"px_endorsement.enr_premium_delta", 11, 2},
		{"px_file_trailer.ftr_written_premium", 15, 2},
		{"px_file_trailer.ftr_paid_loss", 15, 2},
	}

	for _, c := range cases {
		assertDecimal(t, written, c.path, c.precision, c.scale)
	}
}

// assertDecimal requires the column at path to be annotated DECIMAL(precision,
// scale) in the file's own schema.
func assertDecimal(t *testing.T, file []byte, path string, precision, scale int32) {
	t.Helper()

	node := parquet.Node(opened(t, file).Schema())

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

// TestTheScaledValuesRoundTripUnscaled is the other half: the annotation is
// right and the integer under it is the one the generated reader produced,
// unchanged.
func TestTheScaledValuesRoundTripUnscaled(t *testing.T) {
	seen := 0

	for _, row := range rowsOf(t, table(t, 1, detailTypes, 64)) {
		switch {
		case row.Policy != nil:
			seen++

			if row.Policy.PlcWrittenPremium != plcWrittenPremium {
				t.Errorf("PLC-WRITTEN-PREMIUM came back as %d, want %d", row.Policy.PlcWrittenPremium, plcWrittenPremium)
			}
		case row.Location != nil:
			seen++

			if row.Location.LocDistanceToFire != locDistanceToFire {
				t.Errorf("LOC-DISTANCE-TO-FIRE came back as %d, want %d", row.Location.LocDistanceToFire, locDistanceToFire)
			}
		case row.Coverage != nil:
			seen++

			if row.Coverage.CovLimit2 != covLimit2 || row.Coverage.CovRate != covRate {
				t.Errorf("the coverage row came back with (%d, %d), want (%d, %d)", row.Coverage.CovLimit2, row.Coverage.CovRate, covLimit2, covRate)
			}
		case row.Premium != nil:
			seen++

			if row.Premium.PrmSurchargeAmount != prmSurcharge || row.Premium.PrmCommissionRate != prmCommissionRate {
				t.Errorf("the premium row came back with (%d, %d), want (%d, %d)", row.Premium.PrmSurchargeAmount, row.Premium.PrmCommissionRate, prmSurcharge, prmCommissionRate)
			}
		case row.Claim != nil:
			seen++

			if row.Claim.ClmReserveIndemnity != clmReserveIndem {
				t.Errorf("CLM-RESERVE-INDEMNITY came back as %d, want %d", row.Claim.ClmReserveIndemnity, clmReserveIndem)
			}
		case row.FileTrailer != nil:
			seen++

			if row.FileTrailer.FtrPaidLoss != ftrPaidLoss {
				t.Errorf("FTR-PAID-LOSS came back as %d, want %d", row.FileTrailer.FtrPaidLoss, ftrPaidLoss)
			}
		}
	}

	// Without this the comparisons above are guarded by the very thing that
	// would be broken — optional groups coming back nil — and a test named for a
	// round trip would pass having compared nothing.
	if want := 6; seen != want {
		t.Fatalf("%d rows carried a value to compare, want %d", seen, want)
	}
}

// TestAlphanumericItemsArriveTrimmed is a fact about the generated reader that a
// downstream join has to know, and it is asserted here because it is the kind of
// thing a converter is blamed for.
//
// `codec` trims an alphanumeric item's DISPLAY padding, so PLC-STATE is "GA" and
// not "GA" behind however many EBCDIC spaces the record held. A system on the
// other side that kept the padding will not match.
func TestAlphanumericItemsArriveTrimmed(t *testing.T) {
	for _, row := range rowsOf(t, table(t, 1, detailTypes, 64)) {
		if row.Policy == nil {
			continue
		}

		if got := row.Policy.PlcCancelReason; got != "" {
			t.Errorf("PLC-CANCEL-REASON came back as %q, want the empty string: an item this fixture leaves unset is spaces in the record and nothing after trimming", got)
		}

		if got := row.Policy.PlcState; got != "GA" {
			t.Errorf("PLC-STATE came back as %q, want %q", got, "GA")
		}
	}
}

// TestTheRowGroupCapIsReportedWithTheFlagThatMovesIt is [tooManyRowGroups],
// which is the one diagnostic here no fixture can provoke.
//
// Reaching the cap needs 32767 row groups, and 32767 of them over 197 columns
// retains about 6.6 GB — a is a kilobyte per column per row group, and that is
// the whole point of the section this error belongs to. So the wrapping is
// tested as a function rather than through a run, and what it is held to is what
// #304 says a converter needs and did not get: the cap named, and the flag that
// moves it named beside it.
func TestTheRowGroupCapIsReportedWithTheFlagThatMovesIt(t *testing.T) {
	const rows = 64

	err := tooManyRowGroups(fmt.Errorf("writing rows: %w", parquet.ErrTooManyRowGroups), rows)

	if !errors.Is(err, parquet.ErrTooManyRowGroups) {
		t.Fatalf("the wrapped error no longer is parquet.ErrTooManyRowGroups: %v", err)
	}

	for _, want := range []string{
		"32767",
		"-rows-per-row-group 64",
		// 32767 × 64, which is the ceiling this bound puts on the file and the
		// number #304 hit — reported there only as "the limit of 32767 row
		// groups has been reached", wrapped as "flushing 64 posting rows".
		"2097088",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the report is %q, and it does not carry %q", err, want)
		}
	}
}

// TestAnErrorThatIsNotTheCapIsPassedThrough is the other half, and it is what
// stops the wrapping above from being advice glued onto every failure: a full
// disk is not a row-group cap and must not be reported as one.
func TestAnErrorThatIsNotTheCapIsPassedThrough(t *testing.T) {
	want := errors.New("no space left on device")

	if got := tooManyRowGroups(want, 64); got != want {
		t.Errorf("an unrelated failure came back as %q, want it untouched: advice about a row-group cap on a disk that is full sends somebody to the wrong place", got)
	}
}

// TestAFileWithNoHeaderIsReported is the trailer check's twin. Nothing in the
// conversion needs the header to *map* a record — this table denormalizes
// nothing — so it is needed only to hold FHD-CYCLE-DATE and FHD-RUN-NUMBER
// against the trailer's, and a file without one has a reconciliation that cannot
// run rather than one that passes.
func TestAFileWithNoHeaderIsReported(t *testing.T) {
	recs := extract(1, detailTypes)

	var written bytes.Buffer

	err := convert(&oneOff{records: recs[1:]}, &written, 64)
	if err == nil {
		t.Fatal("an extract with no header converted without complaint, and half of the run it is balanced against was missing")
	}

	if !strings.Contains(err.Error(), "PX-FILE-HEADER") {
		t.Errorf("the failure is %q, and it does not say what was missing", err)
	}
}

// TestAFailedRunDestroysAnEarlierGoodOutput is the documented behaviour that is
// least likely to be believed until it happens, so it is asserted rather than
// only written down.
//
// `os.Create` truncates and a failed conversion removes what it truncated. That
// is deliberate — a Parquet file with no footer reads as corruption rather than
// as a run somebody has to repeat — but it means a second run over a bad extract
// destroys the output of a first run over a good one. With the default -out
// being a bare pxtract.parquet in the working directory, that is easy to meet.
func TestAFailedRunDestroysAnEarlierGoodOutput(t *testing.T) {
	dir, out := outputPath(t)

	good := extractOnDisk(t, extract(2, detailTypes))
	if err := run([]string{"-in", good, "-out", out}, io.Discard); err != nil {
		t.Fatalf("the first run: %v", err)
	}

	if names := entryNames(t, dir); len(names) != 1 {
		t.Fatalf("the first run left %v, want one file to destroy", names)
	}

	broken := extract(2, detailTypes)
	trailerOf(t, broken).FtrDetailCount = 0

	if err := run([]string{"-in", extractOnDisk(t, broken), "-out", out}, io.Discard); err == nil {
		t.Fatal("the second run converted a file whose FTR-DETAIL-COUNT is zero")
	}

	if names := entryNames(t, dir); len(names) != 0 {
		t.Errorf("the failed run left %v at a path an earlier good run had written, want nothing: -out is clobbered whether or not the run succeeds, and README.md says so because this is how you find out", names)
	}
}
