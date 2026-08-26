// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Command parquet converts the policy administration extract in the directory
// above this one into the one Parquet table a data platform would query it as.
//
// It is a worked conversion and a reference, not a recommendation and not a
// library. README.md beside this file states every decision it makes, says why
// this one was taken, and marks each place where another adopter would
// reasonably differ.
//
// What it is here for is not the schema design — [the ledger conversion] is
// where that argument lives — but the **cost**. The extract is wide and sparse:
// eleven record types merge into one schema of 197 columns, no record fills
// more than twenty of them, and Parquet materialises a value or a null for every
// column of every row. Sparsity is nearly free on read and on disk and expensive
// only on the writer, so the subject here is what one wide sparse file costs to
// write and how to pay it. The row-group bound below is derived rather than
// chosen, and README.md shows the derivation.
//
// It is package main rather than a package with a Convert function so that
// nothing here can be imported. A conversion is a schema design, and a schema
// design is not a function of the source.
//
// [the ledger conversion]: https://github.com/Zaba505/cpybkc/tree/main/example/ledger/parquet
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Zaba505/cpybkc/example/policy/policy"
	"github.com/parquet-go/parquet-go"
)

// The memory model this conversion is sized by, and every constant in it is a
// property of the copybook or of a number #304 measured. Nothing here is a
// preference.
//
// The rule, for N records at R records per row group over a schema of C columns
// at W bytes a row:
//
//	peak(N, R) ≈ a·C·(N/R) + W·R
//
// The first term is the footer already accumulated — Parquet carries a
// ColumnChunk, a ColumnIndex and an OffsetIndex per column per row group, and
// none of it can be written until Close. The second is the row group still open.
// The two pull opposite ways, and R* is where they are equal: **size the row
// group so that the footer you have accumulated is the same size as the row
// group you are holding.**
//
// Substituting W ≈ 5·C + the record's own bytes, and noting that 5·C dominates
// on a table this wide, gives peak* ≈ 2·C·√(5·a·N) — **linear in the column
// count, not its square root.** For a wide sparse table the column count is the
// whole story, and halving it halves peak memory. No writer option does that.
const (
	// columns is the width of the merged table: the leaves of the schema below,
	// which is the 197 fields the eleven record types declare between them. The
	// eleven groups are structure and get no column of their own.
	//
	// It is written down rather than computed because it is an *input* to the
	// arithmetic below, which has to be a constant expression.
	// TestTheColumnCountIsTheSchemaAndNotANumberWrittenDown opens a converted
	// file and holds this against the leaves it actually finds, so a record type
	// added to the layout and not to this constant fails there rather than
	// leaving the row group sized for a table that no longer exists.
	columns = 197

	// recordBytes is LRECL. `policy.sexpr` frames this dataset RECFM=FB at 256,
	// so every record type accounts for all of it and the record's own bytes are
	// bounded by it whatever record type a row was.
	recordBytes = 256

	// retainedPerColumnPerRowGroup is `a`: what one column of one closed row
	// group holds until Close, in bytes.
	//
	// #304 measured 975, 988 and 943 across schemas of 4, 16 and 32 columns —
	// flat to 5% over an eight-fold range — and 1,878 to 2,960 on schemas whose
	// values are wide, because the minimum and maximum are retained twice, once
	// in Statistics and once in the ColumnIndex. "Read a as 1–3 KB, and measure
	// it if the answer is close."
	//
	// A kilobyte is the bottom of that range and it is the right end for this
	// file: no item in `pxtract.cpy` is wider than 40 bytes and most are under
	// ten. Being wrong by the whole range moves R* by √3 and peak by √3, which
	// the curve absorbs — see [rowsPerRowGroup].
	retainedPerColumnPerRowGroup = 1024

	// definitionLevelBytes is the five bytes an open row group holds per row per
	// optional column, **whether or not there is a value under it**:
	// parquet-go's optionalColumnBuffer keeps `rows []int32` and
	// `definitionLevels []byte` (column_buffer_optional.go:22).
	//
	// Every one of this schema's 197 leaves sits under an optional group, so
	// every one of them is an optional column and every row pays for all 197.
	// That is 985 bytes a row against a 256-byte record, and it is what makes
	// this the wide sparse case rather than an ordinary one.
	definitionLevelBytes = 5

	// bufferedOverheadPerRow is what a row costs an open row group beyond its
	// values and its definition levels — #304's "about 15 bytes a record for the
	// buffered form", measured as the intercept of a padding sweep whose slope
	// was 1.02.
	bufferedOverheadPerRow = 15

	// bufferedPerRow is `W`: what one row costs the open row group.
	//
	// The record's own bytes are taken at LRECL, which is an over-estimate on
	// every row — alphanumeric items arrive from the generated reader already
	// trimmed of their DISPLAY padding, and a record's FILLER is not a column at
	// all. Over-estimating W sizes the row group *smaller*, which is the safe
	// direction: it moves the peak off the bottom of a curve that is flat there.
	bufferedPerRow = definitionLevelBytes*columns + recordBytes + bufferedOverheadPerRow

	// memoryBudget is what this conversion is sized to fit in, and it is the
	// number to put in GOMEMLIMIT when running it. 256 MiB is an ordinary
	// container limit for a batch job.
	//
	// It is a budget and not a bound: nothing here enforces it, and the Go
	// runtime will not infer it. See README.md, "GOMEMLIMIT".
	memoryBudget = 256 << 20

	// rowsPerRowGroup is the default for -rows-per-row-group, and it is the
	// equal-terms row group **at [maxRecords]** rather than at whatever N you
	// have. That distinction is load bearing and the flag is why it can be:
	// R* = √(a·C·N/W) has N in it, so no constant is the optimum for every
	// extract, and the one to pin as a default is the one that keeps the budget
	// at the largest extract the budget admits.
	//
	// At that N the two terms are equal and each is half the budget, so the open
	// row group is memoryBudget/2 bytes and this is that many rows of W. **It is
	// arithmetic and not a choice**, which is the whole difference between this
	// number and the ledger conversion's 64.
	//
	// What it costs on a smaller extract is real and README.md works it: at a
	// million records the true R* is 12,672 and this default holds 134 MB of
	// open row group to amortize a footer that never reaches 2 MB. It stays
	// inside the budget, so the default is safe everywhere and optimal only at
	// the top — which is the trade a single default has to make, and the flag is
	// how you take the other side of it.
	//
	// It is not rounded, and that is deliberate: a round number reads as one
	// somebody liked. The curve does not care either way — it is symmetric in
	// log R, so being off by a factor of k multiplies peak by (k + 1/k)/2, which
	// is 1.25× at a factor of two and 2.13× at four. The rule picks a
	// neighbourhood rather than a number.
	rowsPerRowGroup = memoryBudget / 2 / bufferedPerRow

	// maxRecords is the record count at which this conversion stops fitting
	// memoryBudget at rowsPerRowGroup: the budget less the open row group,
	// divided by what each closed row group retains.
	//
	// **The retained term is linear in N, so this number exists for every budget
	// and every schema.** A conversion that fits today has a record count at
	// which it stops fitting, and an adopter who does not know theirs finds it
	// on a Tuesday. Ours is about 71 million records, which is roughly 18 GB of
	// this extract.
	//
	// Past it, raise the budget and re-derive: peak grows as √N, so quadrupling
	// the budget multiplies this by sixteen.
	maxRecords = (memoryBudget - bufferedPerRow*rowsPerRowGroup) * rowsPerRowGroup /
		(retainedPerColumnPerRowGroup * columns)
)

// record is the one table, and it is the whole file: every record type of
// `pxtract.cpy` is one optional group of the one schema, so a row is a record
// and the table is as wide as the eleven types together.
//
// **Exactly one of these eleven is non-nil on any row**, which is what keeps the
// record type recoverable from a row without consulting anything else. Nothing
// is denormalized onto anything and no key is minted; the file's three grains
// are three kinds of row rather than three tables.
//
// The groups are nested rather than flattened, for the reason the ledger
// conversion gives: flattening collapses a group `A-B` holding `C` onto a group
// `A` holding `B-C`, so it needs a collision rule, and a collision rule is a
// decision you will be making about somebody's field names at three in the
// morning. Here it buys one thing more: a nil `px_insured` says all twenty of
// that record type's columns are absent at once, so "which record type was this"
// is one nil check rather than twenty.
//
// It buys **no memory**, and that is worth saying because it looks like it
// should. Every leaf under an optional group is an optional column and pays its
// five bytes a row whether or not there is a value under it, so a flattened
// schema of 197 pointers would cost the writer the same. Nesting is about
// reading the schema, not about what it costs to write.
type record struct {
	FileHeader  *fileHeader  `parquet:"px_file_header"`
	Policy      *policyTerm  `parquet:"px_policy"`
	Insured     *insured     `parquet:"px_insured"`
	Location    *location    `parquet:"px_location"`
	Vehicle     *vehicle     `parquet:"px_vehicle"`
	Driver      *driver      `parquet:"px_driver"`
	Coverage    *coverage    `parquet:"px_coverage"`
	Premium     *premium     `parquet:"px_premium"`
	Claim       *claim       `parquet:"px_claim"`
	Endorsement *endorsement `parquet:"px_endorsement"`
	FileTrailer *fileTrailer `parquet:"px_file_trailer"`
}

// fileHeader is PX-FILE-HEADER, the `000` record: one per extract, and the
// only record type in this file with no policy key on it — it describes the run
// and not a policy.
type fileHeader struct {
	FhdRecordType    string `parquet:"fhd_record_type"`
	FhdExtractName   string `parquet:"fhd_extract_name"`
	FhdCycleDate     int32  `parquet:"fhd_cycle_date"`
	FhdCycleTime     int32  `parquet:"fhd_cycle_time"`
	FhdCarrier       string `parquet:"fhd_carrier"`
	FhdRegion        string `parquet:"fhd_region"`
	FhdSourceSystem  string `parquet:"fhd_source_system"`
	FhdRunNumber     int32  `parquet:"fhd_run_number"`
	FhdFormatVersion int32  `parquet:"fhd_format_version"`
}

// policyTerm is PX-POLICY, the `PLC` record: one per policy term, and the grain
// the eight detail types hang off.
//
// It is not named `policy` because that is the generated package this reads its
// records through.
type policyTerm struct {
	PlcRecordType     string `parquet:"plc_record_type"`
	PlcPolicyNumber   string `parquet:"plc_policy_number"`
	PlcCarrier        string `parquet:"plc_carrier"`
	PlcTermEffDate    int32  `parquet:"plc_term_eff_date"`
	PlcSeq            int32  `parquet:"plc_seq"`
	PlcProductCode    string `parquet:"plc_product_code"`
	PlcLob            string `parquet:"plc_lob"`
	PlcState          string `parquet:"plc_state"`
	PlcTermExpDate    int32  `parquet:"plc_term_exp_date"`
	PlcIssueDate      int32  `parquet:"plc_issue_date"`
	PlcStatus         string `parquet:"plc_status"`
	PlcCancelDate     int32  `parquet:"plc_cancel_date"`
	PlcCancelReason   string `parquet:"plc_cancel_reason"`
	PlcAgencyCode     string `parquet:"plc_agency_code"`
	PlcProducerCode   string `parquet:"plc_producer_code"`
	PlcBillMethod     string `parquet:"plc_bill_method"`
	PlcPayPlan        string `parquet:"plc_pay_plan"`
	PlcTermMonths     int32  `parquet:"plc_term_months"`
	PlcWrittenPremium int64  `parquet:"plc_written_premium,decimal(2:11)"`
	PlcRenewalCount   int32  `parquet:"plc_renewal_count"`
}

// insured is PX-INSURED, the `INS` record: a named insured on a policy term.
type insured struct {
	InsRecordType    string `parquet:"ins_record_type"`
	InsPolicyNumber  string `parquet:"ins_policy_number"`
	InsCarrier       string `parquet:"ins_carrier"`
	InsTermEffDate   int32  `parquet:"ins_term_eff_date"`
	InsSeq           int32  `parquet:"ins_seq"`
	InsRole          string `parquet:"ins_role"`
	InsLastName      string `parquet:"ins_last_name"`
	InsFirstName     string `parquet:"ins_first_name"`
	InsMiddleInitial string `parquet:"ins_middle_initial"`
	InsAddressLine1  string `parquet:"ins_address_line_1"`
	InsAddressLine2  string `parquet:"ins_address_line_2"`
	InsCity          string `parquet:"ins_city"`
	InsState         string `parquet:"ins_state"`
	InsPostalCode    string `parquet:"ins_postal_code"`
	InsCountry       string `parquet:"ins_country"`
	InsBirthDate     int32  `parquet:"ins_birth_date"`
	InsGender        string `parquet:"ins_gender"`
	InsMaritalStatus string `parquet:"ins_marital_status"`
	InsTaxIdLast4    string `parquet:"ins_tax_id_last_4"`
	InsEmailOnFile   string `parquet:"ins_email_on_file"`
}

// location is PX-LOCATION, the `LOC` record: an insured location.
//
// LOC-DISTANCE-TO-FIRE is the item worth looking at. It is `PIC 9(3)V9` —
// DISPLAY, unsigned, with an implied point one place in — so it is a scaled
// item that is not COMP-3, and it is annotated DECIMAL(4,1) like any other.
type location struct {
	LocRecordType       string `parquet:"loc_record_type"`
	LocPolicyNumber     string `parquet:"loc_policy_number"`
	LocCarrier          string `parquet:"loc_carrier"`
	LocTermEffDate      int32  `parquet:"loc_term_eff_date"`
	LocSeq              int32  `parquet:"loc_seq"`
	LocLocationNumber   int32  `parquet:"loc_location_number"`
	LocAddressLine1     string `parquet:"loc_address_line_1"`
	LocCity             string `parquet:"loc_city"`
	LocState            string `parquet:"loc_state"`
	LocPostalCode       string `parquet:"loc_postal_code"`
	LocCountyCode       string `parquet:"loc_county_code"`
	LocTerritory        string `parquet:"loc_territory"`
	LocProtectionClass  string `parquet:"loc_protection_class"`
	LocConstructionCode string `parquet:"loc_construction_code"`
	LocOccupancyCode    string `parquet:"loc_occupancy_code"`
	LocYearBuilt        int32  `parquet:"loc_year_built"`
	LocSquareFeet       int32  `parquet:"loc_square_feet"`
	LocNumberOfStories  int32  `parquet:"loc_number_of_stories"`
	LocDistanceToFire   int32  `parquet:"loc_distance_to_fire,decimal(1:4)"`
	LocFloodZone        string `parquet:"loc_flood_zone"`
}

// vehicle is PX-VEHICLE, the `VEH` record: an insured unit.
type vehicle struct {
	VehRecordType     string `parquet:"veh_record_type"`
	VehPolicyNumber   string `parquet:"veh_policy_number"`
	VehCarrier        string `parquet:"veh_carrier"`
	VehTermEffDate    int32  `parquet:"veh_term_eff_date"`
	VehSeq            int32  `parquet:"veh_seq"`
	VehUnitNumber     int32  `parquet:"veh_unit_number"`
	VehVin            string `parquet:"veh_vin"`
	VehModelYear      int32  `parquet:"veh_model_year"`
	VehMake           string `parquet:"veh_make"`
	VehModel          string `parquet:"veh_model"`
	VehBodyStyle      string `parquet:"veh_body_style"`
	VehVehicleUse     string `parquet:"veh_vehicle_use"`
	VehGarageState    string `parquet:"veh_garage_state"`
	VehGaragePostal   string `parquet:"veh_garage_postal"`
	VehAnnualMileage  int32  `parquet:"veh_annual_mileage"`
	VehSymbol         string `parquet:"veh_symbol"`
	VehAntiTheft      string `parquet:"veh_anti_theft"`
	VehCostNew        int32  `parquet:"veh_cost_new"`
	VehLienholderName string `parquet:"veh_lienholder_name"`
	VehLeasedFlag     string `parquet:"veh_leased_flag"`
}

// driver is PX-DRIVER, the `DRV` record: a rated or excluded driver.
type driver struct {
	DrvRecordType    string `parquet:"drv_record_type"`
	DrvPolicyNumber  string `parquet:"drv_policy_number"`
	DrvCarrier       string `parquet:"drv_carrier"`
	DrvTermEffDate   int32  `parquet:"drv_term_eff_date"`
	DrvSeq           int32  `parquet:"drv_seq"`
	DrvDriverNumber  int32  `parquet:"drv_driver_number"`
	DrvLastName      string `parquet:"drv_last_name"`
	DrvFirstName     string `parquet:"drv_first_name"`
	DrvBirthDate     int32  `parquet:"drv_birth_date"`
	DrvGender        string `parquet:"drv_gender"`
	DrvMaritalStatus string `parquet:"drv_marital_status"`
	DrvLicenseState  string `parquet:"drv_license_state"`
	DrvLicenseNumber string `parquet:"drv_license_number"`
	DrvLicenseDate   int32  `parquet:"drv_license_date"`
	DrvRelationToIns string `parquet:"drv_relation_to_ins"`
	DrvRatedUnit     int32  `parquet:"drv_rated_unit"`
	DrvGoodStudent   string `parquet:"drv_good_student"`
	DrvTrainingFlag  string `parquet:"drv_training_flag"`
	DrvPoints        int32  `parquet:"drv_points"`
	DrvExcludedFlag  string `parquet:"drv_excluded_flag"`
}

// coverage is PX-COVERAGE, the `COV` record: one coverage on a location, on a
// unit, or on the policy itself.
//
// COV-LOCATION-NUMBER and COV-UNIT-NUMBER are both present on every coverage
// record and at most one of them is ever meaningful. That is the copybook's
// shape rather than this conversion's to tidy, and both get a column: a
// converter that dropped the inapplicable one would be deciding, per row, which
// of them the producing system meant.
type coverage struct {
	CovRecordType     string `parquet:"cov_record_type"`
	CovPolicyNumber   string `parquet:"cov_policy_number"`
	CovCarrier        string `parquet:"cov_carrier"`
	CovTermEffDate    int32  `parquet:"cov_term_eff_date"`
	CovSeq            int32  `parquet:"cov_seq"`
	CovLocationNumber int32  `parquet:"cov_location_number"`
	CovUnitNumber     int32  `parquet:"cov_unit_number"`
	CovCoverageCode   string `parquet:"cov_coverage_code"`
	CovCoverageDesc   string `parquet:"cov_coverage_desc"`
	CovLimit1         int64  `parquet:"cov_limit_1,decimal(2:13)"`
	CovLimit2         int64  `parquet:"cov_limit_2,decimal(2:13)"`
	CovDeductible     int32  `parquet:"cov_deductible,decimal(2:9)"`
	CovDeductibleType string `parquet:"cov_deductible_type"`
	CovEffDate        int32  `parquet:"cov_eff_date"`
	CovExpDate        int32  `parquet:"cov_exp_date"`
	CovFormCode       string `parquet:"cov_form_code"`
	CovClassCode      string `parquet:"cov_class_code"`
	CovRate           int64  `parquet:"cov_rate,decimal(5:10)"`
	CovExposureBasis  string `parquet:"cov_exposure_basis"`
	CovWaiverFlag     string `parquet:"cov_waiver_flag"`
}

// premium is PX-PREMIUM, the `PRM` record: one premium transaction.
//
// Eight of its twenty items are scaled and they do not share a scale:
// PRM-COMMISSION-RATE is `S9(3)V9(4)`, four places, beside seven items at two.
// Nothing here rescales any of them — see [convert] on what that would cost.
type premium struct {
	PrmRecordType       string `parquet:"prm_record_type"`
	PrmPolicyNumber     string `parquet:"prm_policy_number"`
	PrmCarrier          string `parquet:"prm_carrier"`
	PrmTermEffDate      int32  `parquet:"prm_term_eff_date"`
	PrmSeq              int32  `parquet:"prm_seq"`
	PrmCoverageCode     string `parquet:"prm_coverage_code"`
	PrmTransactionCode  string `parquet:"prm_transaction_code"`
	PrmTransactionDate  int32  `parquet:"prm_transaction_date"`
	PrmAccountingPeriod int32  `parquet:"prm_accounting_period"`
	PrmWrittenAmount    int64  `parquet:"prm_written_amount,decimal(2:11)"`
	PrmEarnedAmount     int64  `parquet:"prm_earned_amount,decimal(2:11)"`
	PrmUnearnedAmount   int64  `parquet:"prm_unearned_amount,decimal(2:11)"`
	PrmCommissionAmount int32  `parquet:"prm_commission_amount,decimal(2:9)"`
	PrmCommissionRate   int32  `parquet:"prm_commission_rate,decimal(4:7)"`
	PrmTaxAmount        int32  `parquet:"prm_tax_amount,decimal(2:9)"`
	PrmFeeAmount        int32  `parquet:"prm_fee_amount,decimal(2:9)"`
	PrmSurchargeAmount  int32  `parquet:"prm_surcharge_amount,decimal(2:9)"`
	PrmCurrency         string `parquet:"prm_currency"`
	PrmGlAccount        string `parquet:"prm_gl_account"`
	PrmStatutoryLine    string `parquet:"prm_statutory_line"`
}

// claim is PX-CLAIM, the `CLM` record: a claim against a policy term.
type claim struct {
	ClmRecordType       string `parquet:"clm_record_type"`
	ClmPolicyNumber     string `parquet:"clm_policy_number"`
	ClmCarrier          string `parquet:"clm_carrier"`
	ClmTermEffDate      int32  `parquet:"clm_term_eff_date"`
	ClmSeq              int32  `parquet:"clm_seq"`
	ClmClaimNumber      string `parquet:"clm_claim_number"`
	ClmLossDate         int32  `parquet:"clm_loss_date"`
	ClmReportedDate     int32  `parquet:"clm_reported_date"`
	ClmClosedDate       int32  `parquet:"clm_closed_date"`
	ClmStatus           string `parquet:"clm_status"`
	ClmCauseOfLoss      string `parquet:"clm_cause_of_loss"`
	ClmCoverageCode     string `parquet:"clm_coverage_code"`
	ClmLocationNumber   int32  `parquet:"clm_location_number"`
	ClmUnitNumber       int32  `parquet:"clm_unit_number"`
	ClmAdjusterCode     string `parquet:"clm_adjuster_code"`
	ClmPaidIndemnity    int64  `parquet:"clm_paid_indemnity,decimal(2:13)"`
	ClmPaidExpense      int64  `parquet:"clm_paid_expense,decimal(2:11)"`
	ClmReserveIndemnity int64  `parquet:"clm_reserve_indemnity,decimal(2:13)"`
	ClmReserveExpense   int64  `parquet:"clm_reserve_expense,decimal(2:11)"`
	ClmSubrogationFlag  string `parquet:"clm_subrogation_flag"`
}

// endorsement is PX-ENDORSEMENT, the `ENR` record: a form endorsed onto a
// policy term.
type endorsement struct {
	EnrRecordType        string `parquet:"enr_record_type"`
	EnrPolicyNumber      string `parquet:"enr_policy_number"`
	EnrCarrier           string `parquet:"enr_carrier"`
	EnrTermEffDate       int32  `parquet:"enr_term_eff_date"`
	EnrSeq               int32  `parquet:"enr_seq"`
	EnrEndorsementNumber int32  `parquet:"enr_endorsement_number"`
	EnrFormNumber        string `parquet:"enr_form_number"`
	EnrFormEdition       string `parquet:"enr_form_edition"`
	EnrFormTitle         string `parquet:"enr_form_title"`
	EnrEffDate           int32  `parquet:"enr_eff_date"`
	EnrExpDate           int32  `parquet:"enr_exp_date"`
	EnrTransactionCode   string `parquet:"enr_transaction_code"`
	EnrTransactionDate   int32  `parquet:"enr_transaction_date"`
	EnrPremiumDelta      int64  `parquet:"enr_premium_delta,decimal(2:11)"`
	EnrLocationNumber    int32  `parquet:"enr_location_number"`
	EnrUnitNumber        int32  `parquet:"enr_unit_number"`
	EnrCoverageCode      string `parquet:"enr_coverage_code"`
	EnrMandatoryFlag     string `parquet:"enr_mandatory_flag"`
	EnrPrintFlag         string `parquet:"enr_print_flag"`
	EnrStateFiled        string `parquet:"enr_state_filed"`
}

// fileTrailer is PX-FILE-TRAILER, the `999` record: the control totals the
// receiving system balances the extract on.
//
// Four of its eight items are checked by [convert] and three are not. README.md,
// "What is reconciled, and what deliberately is not", is where that is argued.
type fileTrailer struct {
	FtrRecordType       string `parquet:"ftr_record_type"`
	FtrCycleDate        int32  `parquet:"ftr_cycle_date"`
	FtrPolicyCount      int32  `parquet:"ftr_policy_count"`
	FtrDetailCount      int32  `parquet:"ftr_detail_count"`
	FtrWrittenPremium   int64  `parquet:"ftr_written_premium,decimal(2:15)"`
	FtrPaidLoss         int64  `parquet:"ftr_paid_loss,decimal(2:15)"`
	FtrHashPolicyNumber int64  `parquet:"ftr_hash_policy_number"`
	FtrRunNumber        int32  `parquet:"ftr_run_number"`
}

// grain is which of this file's three grains a record belongs to.
//
// The file has three and the table has one row per record, so the grain is not
// a table and not a column — it is what the two record counts in the trailer are
// counts of. `policy.sexpr` is where it is written down: a header, then any
// number of policies, then a trailer, with any number of detail records behind
// each policy.
type grain int

const (
	// fileGrain is PX-FILE-HEADER and PX-FILE-TRAILER: one row each per extract.
	fileGrain grain = iota

	// policyGrain is PX-POLICY, counted by FTR-POLICY-COUNT.
	policyGrain

	// detailGrain is the eight types that hang off a policy, counted by
	// FTR-DETAIL-COUNT.
	detailGrain
)

// recordSource is the one record at a time a policy extract is read as.
//
// [policy.Reader] is what satisfies it, and the interface exists so that a test
// can hand this conversion a record the automaton would never admit — which is
// how "a mapping error fails the conversion" is asserted rather than asserted
// about a function nothing calls.
//
// io.EOF means the end of the file and carries **no** record, which is
// [policy.Reader.Next]'s own contract. A source that returned its last record
// alongside io.EOF would have that row dropped here, so an implementation of
// this interface owes the same promise.
type recordSource interface {
	Next() (policy.Record, error)
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

	in := flags.String("in", "", "the policy extract to convert")
	out := flags.String("out", "pxtract.parquet", "the Parquet file to write")

	// The row group is a flag rather than only a constant because R* is a
	// function of the extract's size and not of the schema alone: the retained
	// term is a·C·(N/R), so the record count is one of the rule's three inputs
	// and it is a property of the file in hand. The default is the design point
	// README.md derives — memoryBudget at maxRecords — and an adopter with a
	// different budget or a different extract re-derives it there and passes it
	// here.
	//
	// It is the *only* bound. There is deliberately no caller-side batch beside
	// it and no Flush: see [convert].
	rows := flags.Int("rows-per-row-group", rowsPerRowGroup,
		"how many rows a row group holds before it is closed")

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
		return errors.New("-in names the policy extract to convert, and is required")
	}

	// Checked here rather than left to parquet-go, which takes a non-positive
	// cap as "no bound at all" — DefaultMaxRowsPerRowGroup is math.MaxInt64
	// (config.go:37) — so `-rows-per-row-group 0` would grow one row group for
	// the whole file and read as this conversion having no memory model.
	if *rows < 1 {
		return fmt.Errorf("-rows-per-row-group is %d: a row group holds at least one row, and a non-positive bound is read as no bound at all rather than as a small one", *rows)
	}

	src, err := os.Open(*in)
	if err != nil {
		return err
	}

	// The input is read to the end and never written, so there is nothing its
	// Close can report that a caller could act on.
	defer func() { _ = src.Close() }()

	r, err := policy.NewReader(src, policy.Encoding())
	if err != nil {
		return err
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
	if err := write(r, *out, *rows); err != nil {
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
// so the path goes.
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

// convert reads every record of src once and writes the one table.
//
// One pass, and the file is never held: what this carries at any moment is the
// header, the trailer once it arrives, the one row it has just mapped, two
// counters, and whatever parquet-go is buffering for the open row group and the
// footer behind it. Those last two are the whole memory model, and they are what
// the constants at the top of this file size.
//
// **There is no caller-side batch, and that is not an omission.** rowsPerRowGroup
// is passed to parquet-go as parquet.MaxRowsPerRowGroup and enforced inside
// GenericWriter.Write: the row-group writer returns ErrTooManyRowGroups once it
// is at the cap (writer.go:1036), Write catches exactly that error, closes the
// group and carries on with the rows it has left (writer.go:273-277), and Close
// writes the last partial one.
//
// Holding a slice of our own *and* setting the option is the trap #304 names:
// a manual Flush closes the row group unconditionally, so rg.numRows never
// reaches the cap and the option never fires. The row group is then the batch
// whatever the option says, and raising the batch to raise the row group pays
// for it twice — once in parquet-go's buffers and once in a slice of Go structs.
// The two are not complementary. Use one.
func convert(src recordSource, w io.Writer, rows int) error {
	table := parquet.NewGenericWriter[record](w, parquet.MaxRowsPerRowGroup(int64(rows)))

	// row is the one-row slice a mapped record is handed over in, reused across
	// records. It is the argument to a call and not a batch: what is in it has
	// been written before the next record is read, and the column buffers copy
	// out of it before Write returns.
	//
	// A converter writing millions would hand Write a longer slice while still
	// leaving the bound to parquet.MaxRowsPerRowGroup — the slice amortizes
	// Write's per-call work over the rows in it, and on a 197-column schema that
	// work is 197 column visits a row. It was never the bound.
	row := make([]record, 1)

	// ordinal is which record of the file has been read, counting from one.
	// policy.Reader keeps one for its own diagnostics and does not export it, so
	// a caller that wants to say where it failed counts its own — and this one
	// is a diagnostic and never a column.
	ordinal := 0

	// The two grains the trailer counts, counted as records are read.
	//
	// There is no separate count of rows *written*, and no per-row short-write
	// check, because parquet-go cannot produce one: the write function
	// GenericWriter.Write builds returns len(rows) or an error (writer.go:227),
	// and rows is one row here — so n is 1 whenever err is nil, and a check
	// against it is a check against a literal. The ledger conversion deleted the
	// same check for the same reason. What would catch it if that ever changed
	// is TestEveryRecordReadIsARowWritten, which counts the rows a written file
	// actually holds.
	policies, details := int64(0), int64(0)

	var hdr *fileHeader
	var trl *fileTrailer

	for {
		rec, err := src.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("reading record %d: %w", ordinal+1, err)
		}

		ordinal++

		mapped, at, err := rowOf(rec)
		if err != nil {
			// Nothing is written. A discarded error here would write a row
			// whose every group is null — a row that reads as data rather than
			// as the failure it is, and on a table this sparse an entirely null
			// row is not even conspicuous.
			return fmt.Errorf("record %d: %w", ordinal, err)
		}

		switch at {
		case policyGrain:
			policies++
		case detailGrain:
			details++
		case fileGrain:
			// Kept for the reconciliation below rather than counted. The
			// pointers are this conversion's own allocations and the writer
			// retains nothing of a row past Write, so holding them across the
			// loop is safe.
			if mapped.FileHeader != nil {
				hdr = mapped.FileHeader
			}

			if mapped.FileTrailer != nil {
				trl = mapped.FileTrailer
			}
		}

		row[0] = mapped

		if _, err := table.Write(row); err != nil {
			return fmt.Errorf("record %d: writing the row: %w", ordinal, tooManyRowGroups(err, rows))
		}
	}

	if hdr == nil {
		return errors.New("the file carried no PX-FILE-HEADER")
	}

	if trl == nil {
		return errors.New("the file carried no PX-FILE-TRAILER")
	}

	// Every reconciliation before the footer is written, so that a conversion
	// which does not reconcile leaves a file no Parquet reader will open rather
	// than one a query would happily return wrong answers from.
	if err := reconcile(hdr, trl, policies, details); err != nil {
		return err
	}

	// Close is what writes the last row group, which on any file whose record
	// count is not a multiple of the bound is a partial one — so it is the other
	// half of the bound above rather than only the footer.
	if err := table.Close(); err != nil {
		return fmt.Errorf("closing the table: %w", tooManyRowGroups(err, rows))
	}

	return nil
}

// tooManyRowGroups re-reports parquet-go's row-group cap as the thing an adopter
// can act on.
//
// A Parquet file holds at most MaxRowGroups = math.MaxInt16 = 32767 of them
// (limits.go:29), so a bound of R records puts a ceiling of 32767·R records on
// the file. #304 met it at 2,097,088 rows with a 64-row bound and got "the limit
// of 32767 row groups has been reached", wrapped as "flushing 64 posting rows" —
// a true sentence that names neither the cap nor the flag that moves it.
//
// The ceiling and the linear heap are one mistake with one fix rather than two
// walls with two: tiny row groups produce both, and a row group sized anywhere
// near R* produces neither. At the default this conversion clears the cap by a
// factor of 49 at maxRecords: 32767 row groups of 106,861 rows is 3.5 billion
// records, against a memory ceiling of 71 million.
func tooManyRowGroups(err error, rows int) error {
	if !errors.Is(err, parquet.ErrTooManyRowGroups) {
		return err
	}

	return fmt.Errorf("%w: a Parquet file holds at most 32767 row groups, so -rows-per-row-group %d puts a ceiling of %d records on this file — raise it, which lowers peak memory as well as the row-group count, and see README.md for the arithmetic", err, rows, int64(rows)*32767)
}

// reconcile checks the trailer's control totals against what was read, and is
// deliberately not a check of all of them.
//
// **What is checked is what the copybook's own names decide.** FTR-POLICY-COUNT
// and FTR-DETAIL-COUNT are counts of record types the layout names, and the
// cycle date and run number are the same two items on the header and on the
// trailer — a file whose two disagree is two extracts concatenated.
//
// FTR-WRITTEN-PREMIUM, FTR-PAID-LOSS and FTR-HASH-POLICY-NUMBER are **not**
// checked, and README.md is where that is argued: nothing in `pxtract.cpy` says
// which grain they total, and a check that has to invent one is a check that
// fails on files that are fine.
func reconcile(hdr *fileHeader, trl *fileTrailer, policies, details int64) error {
	if got := int64(trl.FtrPolicyCount); got != policies {
		return fmt.Errorf("FTR-POLICY-COUNT is %d and %d PX-POLICY records were read: the trailer counts the policies of this extract, so the two disagreeing means the file, the layout or this conversion is wrong and none of the three is a thing to write out anyway", got, policies)
	}

	if got := int64(trl.FtrDetailCount); got != details {
		return fmt.Errorf("FTR-DETAIL-COUNT is %d and %d detail records were read: the eight types that hang off a policy are what this counts, and PX-POLICY itself is FTR-POLICY-COUNT's", got, details)
	}

	if hdr.FhdCycleDate != trl.FtrCycleDate {
		return fmt.Errorf("FHD-CYCLE-DATE is %d and FTR-CYCLE-DATE is %d: one extract has one cycle, and a header and a trailer that disagree about it are two files that arrived as one", hdr.FhdCycleDate, trl.FtrCycleDate)
	}

	if hdr.FhdRunNumber != trl.FtrRunNumber {
		return fmt.Errorf("FHD-RUN-NUMBER is %d and FTR-RUN-NUMBER is %d: same file, same run", hdr.FhdRunNumber, trl.FtrRunNumber)
	}

	return nil
}

// rowOf is the row one record contributes, and the grain it was read at.
//
// Exactly one group of the returned row is non-nil. The switch is the only place
// that decides which, and the grain comes back beside the row rather than being
// read off it so that there is not a second switch somewhere else for the two to
// drift apart in.
//
// The composite literals each group builder returns are copies, and they are
// here for the ordinary reason rather than as a defence against aliasing:
// [insured] is a different type from the struct `policy` generated, a
// conversion's schema is not its source's, and a copy is what crossing that
// boundary is.
//
// A record that is none of this file's eleven types is an error and never a row.
// The eleven are what `policy.sexpr` names, and a twelfth arriving here means the
// layout and this conversion have gone out of step — which is a thing to report,
// not to write a null row for.
func rowOf(rec policy.Record) (record, grain, error) {
	switch v := rec.(type) {
	case *policy.PxFileHeader:
		return record{FileHeader: fileHeaderOf(v)}, fileGrain, nil
	case *policy.PxFileTrailer:
		return record{FileTrailer: fileTrailerOf(v)}, fileGrain, nil
	case *policy.PxPolicy:
		return record{Policy: policyTermOf(v)}, policyGrain, nil
	case *policy.PxInsured:
		return record{Insured: insuredOf(v)}, detailGrain, nil
	case *policy.PxLocation:
		return record{Location: locationOf(v)}, detailGrain, nil
	case *policy.PxVehicle:
		return record{Vehicle: vehicleOf(v)}, detailGrain, nil
	case *policy.PxDriver:
		return record{Driver: driverOf(v)}, detailGrain, nil
	case *policy.PxCoverage:
		return record{Coverage: coverageOf(v)}, detailGrain, nil
	case *policy.PxPremium:
		return record{Premium: premiumOf(v)}, detailGrain, nil
	case *policy.PxClaim:
		return record{Claim: claimOf(v)}, detailGrain, nil
	case *policy.PxEndorsement:
		return record{Endorsement: endorsementOf(v)}, detailGrain, nil
	default:
		return record{}, fileGrain, fmt.Errorf("this file's records are the eleven types `pxtract.cpy` declares, and a %T is none of them", rec)
	}
}

// fileHeaderOf is the group one PxFileHeader contributes.
func fileHeaderOf(rec *policy.PxFileHeader) *fileHeader {
	return &fileHeader{
		FhdRecordType:    rec.FhdRecordType,
		FhdExtractName:   rec.FhdExtractName,
		FhdCycleDate:     rec.FhdCycleDate,
		FhdCycleTime:     rec.FhdCycleTime,
		FhdCarrier:       rec.FhdCarrier,
		FhdRegion:        rec.FhdRegion,
		FhdSourceSystem:  rec.FhdSourceSystem,
		FhdRunNumber:     rec.FhdRunNumber,
		FhdFormatVersion: rec.FhdFormatVersion,
	}
}

// policyTermOf is the group one PxPolicy contributes.
func policyTermOf(rec *policy.PxPolicy) *policyTerm {
	return &policyTerm{
		PlcRecordType:     rec.PlcRecordType,
		PlcPolicyNumber:   rec.PlcPolicyNumber,
		PlcCarrier:        rec.PlcCarrier,
		PlcTermEffDate:    rec.PlcTermEffDate,
		PlcSeq:            rec.PlcSeq,
		PlcProductCode:    rec.PlcProductCode,
		PlcLob:            rec.PlcLob,
		PlcState:          rec.PlcState,
		PlcTermExpDate:    rec.PlcTermExpDate,
		PlcIssueDate:      rec.PlcIssueDate,
		PlcStatus:         rec.PlcStatus,
		PlcCancelDate:     rec.PlcCancelDate,
		PlcCancelReason:   rec.PlcCancelReason,
		PlcAgencyCode:     rec.PlcAgencyCode,
		PlcProducerCode:   rec.PlcProducerCode,
		PlcBillMethod:     rec.PlcBillMethod,
		PlcPayPlan:        rec.PlcPayPlan,
		PlcTermMonths:     rec.PlcTermMonths,
		PlcWrittenPremium: rec.PlcWrittenPremium,
		PlcRenewalCount:   rec.PlcRenewalCount,
	}
}

// insuredOf is the group one PxInsured contributes.
func insuredOf(rec *policy.PxInsured) *insured {
	return &insured{
		InsRecordType:    rec.InsRecordType,
		InsPolicyNumber:  rec.InsPolicyNumber,
		InsCarrier:       rec.InsCarrier,
		InsTermEffDate:   rec.InsTermEffDate,
		InsSeq:           rec.InsSeq,
		InsRole:          rec.InsRole,
		InsLastName:      rec.InsLastName,
		InsFirstName:     rec.InsFirstName,
		InsMiddleInitial: rec.InsMiddleInitial,
		InsAddressLine1:  rec.InsAddressLine1,
		InsAddressLine2:  rec.InsAddressLine2,
		InsCity:          rec.InsCity,
		InsState:         rec.InsState,
		InsPostalCode:    rec.InsPostalCode,
		InsCountry:       rec.InsCountry,
		InsBirthDate:     rec.InsBirthDate,
		InsGender:        rec.InsGender,
		InsMaritalStatus: rec.InsMaritalStatus,
		InsTaxIdLast4:    rec.InsTaxIdLast4,
		InsEmailOnFile:   rec.InsEmailOnFile,
	}
}

// locationOf is the group one PxLocation contributes.
func locationOf(rec *policy.PxLocation) *location {
	return &location{
		LocRecordType:       rec.LocRecordType,
		LocPolicyNumber:     rec.LocPolicyNumber,
		LocCarrier:          rec.LocCarrier,
		LocTermEffDate:      rec.LocTermEffDate,
		LocSeq:              rec.LocSeq,
		LocLocationNumber:   rec.LocLocationNumber,
		LocAddressLine1:     rec.LocAddressLine1,
		LocCity:             rec.LocCity,
		LocState:            rec.LocState,
		LocPostalCode:       rec.LocPostalCode,
		LocCountyCode:       rec.LocCountyCode,
		LocTerritory:        rec.LocTerritory,
		LocProtectionClass:  rec.LocProtectionClass,
		LocConstructionCode: rec.LocConstructionCode,
		LocOccupancyCode:    rec.LocOccupancyCode,
		LocYearBuilt:        rec.LocYearBuilt,
		LocSquareFeet:       rec.LocSquareFeet,
		LocNumberOfStories:  rec.LocNumberOfStories,
		LocDistanceToFire:   rec.LocDistanceToFire,
		LocFloodZone:        rec.LocFloodZone,
	}
}

// vehicleOf is the group one PxVehicle contributes.
func vehicleOf(rec *policy.PxVehicle) *vehicle {
	return &vehicle{
		VehRecordType:     rec.VehRecordType,
		VehPolicyNumber:   rec.VehPolicyNumber,
		VehCarrier:        rec.VehCarrier,
		VehTermEffDate:    rec.VehTermEffDate,
		VehSeq:            rec.VehSeq,
		VehUnitNumber:     rec.VehUnitNumber,
		VehVin:            rec.VehVin,
		VehModelYear:      rec.VehModelYear,
		VehMake:           rec.VehMake,
		VehModel:          rec.VehModel,
		VehBodyStyle:      rec.VehBodyStyle,
		VehVehicleUse:     rec.VehVehicleUse,
		VehGarageState:    rec.VehGarageState,
		VehGaragePostal:   rec.VehGaragePostal,
		VehAnnualMileage:  rec.VehAnnualMileage,
		VehSymbol:         rec.VehSymbol,
		VehAntiTheft:      rec.VehAntiTheft,
		VehCostNew:        rec.VehCostNew,
		VehLienholderName: rec.VehLienholderName,
		VehLeasedFlag:     rec.VehLeasedFlag,
	}
}

// driverOf is the group one PxDriver contributes.
func driverOf(rec *policy.PxDriver) *driver {
	return &driver{
		DrvRecordType:    rec.DrvRecordType,
		DrvPolicyNumber:  rec.DrvPolicyNumber,
		DrvCarrier:       rec.DrvCarrier,
		DrvTermEffDate:   rec.DrvTermEffDate,
		DrvSeq:           rec.DrvSeq,
		DrvDriverNumber:  rec.DrvDriverNumber,
		DrvLastName:      rec.DrvLastName,
		DrvFirstName:     rec.DrvFirstName,
		DrvBirthDate:     rec.DrvBirthDate,
		DrvGender:        rec.DrvGender,
		DrvMaritalStatus: rec.DrvMaritalStatus,
		DrvLicenseState:  rec.DrvLicenseState,
		DrvLicenseNumber: rec.DrvLicenseNumber,
		DrvLicenseDate:   rec.DrvLicenseDate,
		DrvRelationToIns: rec.DrvRelationToIns,
		DrvRatedUnit:     rec.DrvRatedUnit,
		DrvGoodStudent:   rec.DrvGoodStudent,
		DrvTrainingFlag:  rec.DrvTrainingFlag,
		DrvPoints:        rec.DrvPoints,
		DrvExcludedFlag:  rec.DrvExcludedFlag,
	}
}

// coverageOf is the group one PxCoverage contributes.
func coverageOf(rec *policy.PxCoverage) *coverage {
	return &coverage{
		CovRecordType:     rec.CovRecordType,
		CovPolicyNumber:   rec.CovPolicyNumber,
		CovCarrier:        rec.CovCarrier,
		CovTermEffDate:    rec.CovTermEffDate,
		CovSeq:            rec.CovSeq,
		CovLocationNumber: rec.CovLocationNumber,
		CovUnitNumber:     rec.CovUnitNumber,
		CovCoverageCode:   rec.CovCoverageCode,
		CovCoverageDesc:   rec.CovCoverageDesc,
		CovLimit1:         rec.CovLimit1,
		CovLimit2:         rec.CovLimit2,
		CovDeductible:     rec.CovDeductible,
		CovDeductibleType: rec.CovDeductibleType,
		CovEffDate:        rec.CovEffDate,
		CovExpDate:        rec.CovExpDate,
		CovFormCode:       rec.CovFormCode,
		CovClassCode:      rec.CovClassCode,
		CovRate:           rec.CovRate,
		CovExposureBasis:  rec.CovExposureBasis,
		CovWaiverFlag:     rec.CovWaiverFlag,
	}
}

// premiumOf is the group one PxPremium contributes.
func premiumOf(rec *policy.PxPremium) *premium {
	return &premium{
		PrmRecordType:       rec.PrmRecordType,
		PrmPolicyNumber:     rec.PrmPolicyNumber,
		PrmCarrier:          rec.PrmCarrier,
		PrmTermEffDate:      rec.PrmTermEffDate,
		PrmSeq:              rec.PrmSeq,
		PrmCoverageCode:     rec.PrmCoverageCode,
		PrmTransactionCode:  rec.PrmTransactionCode,
		PrmTransactionDate:  rec.PrmTransactionDate,
		PrmAccountingPeriod: rec.PrmAccountingPeriod,
		PrmWrittenAmount:    rec.PrmWrittenAmount,
		PrmEarnedAmount:     rec.PrmEarnedAmount,
		PrmUnearnedAmount:   rec.PrmUnearnedAmount,
		PrmCommissionAmount: rec.PrmCommissionAmount,
		PrmCommissionRate:   rec.PrmCommissionRate,
		PrmTaxAmount:        rec.PrmTaxAmount,
		PrmFeeAmount:        rec.PrmFeeAmount,
		PrmSurchargeAmount:  rec.PrmSurchargeAmount,
		PrmCurrency:         rec.PrmCurrency,
		PrmGlAccount:        rec.PrmGlAccount,
		PrmStatutoryLine:    rec.PrmStatutoryLine,
	}
}

// claimOf is the group one PxClaim contributes.
func claimOf(rec *policy.PxClaim) *claim {
	return &claim{
		ClmRecordType:       rec.ClmRecordType,
		ClmPolicyNumber:     rec.ClmPolicyNumber,
		ClmCarrier:          rec.ClmCarrier,
		ClmTermEffDate:      rec.ClmTermEffDate,
		ClmSeq:              rec.ClmSeq,
		ClmClaimNumber:      rec.ClmClaimNumber,
		ClmLossDate:         rec.ClmLossDate,
		ClmReportedDate:     rec.ClmReportedDate,
		ClmClosedDate:       rec.ClmClosedDate,
		ClmStatus:           rec.ClmStatus,
		ClmCauseOfLoss:      rec.ClmCauseOfLoss,
		ClmCoverageCode:     rec.ClmCoverageCode,
		ClmLocationNumber:   rec.ClmLocationNumber,
		ClmUnitNumber:       rec.ClmUnitNumber,
		ClmAdjusterCode:     rec.ClmAdjusterCode,
		ClmPaidIndemnity:    rec.ClmPaidIndemnity,
		ClmPaidExpense:      rec.ClmPaidExpense,
		ClmReserveIndemnity: rec.ClmReserveIndemnity,
		ClmReserveExpense:   rec.ClmReserveExpense,
		ClmSubrogationFlag:  rec.ClmSubrogationFlag,
	}
}

// endorsementOf is the group one PxEndorsement contributes.
func endorsementOf(rec *policy.PxEndorsement) *endorsement {
	return &endorsement{
		EnrRecordType:        rec.EnrRecordType,
		EnrPolicyNumber:      rec.EnrPolicyNumber,
		EnrCarrier:           rec.EnrCarrier,
		EnrTermEffDate:       rec.EnrTermEffDate,
		EnrSeq:               rec.EnrSeq,
		EnrEndorsementNumber: rec.EnrEndorsementNumber,
		EnrFormNumber:        rec.EnrFormNumber,
		EnrFormEdition:       rec.EnrFormEdition,
		EnrFormTitle:         rec.EnrFormTitle,
		EnrEffDate:           rec.EnrEffDate,
		EnrExpDate:           rec.EnrExpDate,
		EnrTransactionCode:   rec.EnrTransactionCode,
		EnrTransactionDate:   rec.EnrTransactionDate,
		EnrPremiumDelta:      rec.EnrPremiumDelta,
		EnrLocationNumber:    rec.EnrLocationNumber,
		EnrUnitNumber:        rec.EnrUnitNumber,
		EnrCoverageCode:      rec.EnrCoverageCode,
		EnrMandatoryFlag:     rec.EnrMandatoryFlag,
		EnrPrintFlag:         rec.EnrPrintFlag,
		EnrStateFiled:        rec.EnrStateFiled,
	}
}

// fileTrailerOf is the group one PxFileTrailer contributes.
func fileTrailerOf(rec *policy.PxFileTrailer) *fileTrailer {
	return &fileTrailer{
		FtrRecordType:       rec.FtrRecordType,
		FtrCycleDate:        rec.FtrCycleDate,
		FtrPolicyCount:      rec.FtrPolicyCount,
		FtrDetailCount:      rec.FtrDetailCount,
		FtrWrittenPremium:   rec.FtrWrittenPremium,
		FtrPaidLoss:         rec.FtrPaidLoss,
		FtrHashPolicyNumber: rec.FtrHashPolicyNumber,
		FtrRunNumber:        rec.FtrRunNumber,
	}
}
