// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/manifest"
	"github.com/Zaba505/cpybkc/internal/project"
	"github.com/Zaba505/cpybkc/irpb"
)

// TestARecordLevelRedefineGeneratesOneTypePerAlternative is #164 driven the
// whole way: a copybook whose `01`-level is redefined three ways, discriminated
// on a code behind a shared account key, resolved and assembled into a
// descriptor, and generated from here without a collision.
//
// It runs the real chain rather than a hand-built descriptor because the failure
// it is about lived between the stages. `resolve` had always produced one record
// type per alternative and this generator had always refused two of them called
// one thing (#50); what was missing was any way for a layout to say which
// alternative a record form meant and what to call it, so the shape resolved,
// assembled, and then died here with a message pointing at a remedy that did not
// exist. Asserting it from a descriptor written by hand would assert the half
// that was never broken.
//
// The three types are named by the three `rename` forms on the records. Without
// them the descriptor is still well formed — all three record nodes carry
// `TXN-REC`, which is the honest original for every alternative of that level
// (docs/ir/SPEC.md, "Names") — and this generator refuses it, which is the
// second half of the test below.
func TestARecordLevelRedefineGeneratesOneTypePerAlternative(t *testing.T) {
	t.Parallel()

	descriptor := transactions(t, renamedRecords)

	out := t.TempDir()
	if err := generate(descriptor, out, options{packageName: "txn"}); err != nil {
		t.Fatalf("generating from three alternatives of one 01-level: %v", err)
	}

	source, err := os.ReadFile(filepath.Join(out, recordsFile))
	if err != nil {
		t.Fatalf("reading the generated records: %v", err)
	}

	for _, want := range []string{"type TxnPurchaseRec", "type TxnRefundRec", "type TxnAdjustRec"} {
		if !strings.Contains(string(source), want) {
			t.Errorf("the generated package has no %s:\n%s", want, source)
		}
	}

	// Each type holds its own alternative's items and neither of the others',
	// which is what says the layout's choice selected a record type rather than
	// being carried along beside one.
	bodies := declarations(string(source))

	for typ, own := range map[string]string{
		"TxnPurchaseRec": "PurAmt",
		"TxnRefundRec":   "RefOrig",
		"TxnAdjustRec":   "AdjReason",
	} {
		for _, field := range []string{"PurAmt", "RefOrig", "AdjReason"} {
			held := strings.Contains(bodies[typ], field)
			if held != (field == own) {
				t.Errorf("%s holds %s: %v, want %v", typ, field, held, field == own)
			}
		}
	}
}

// declarations is each generated type's source, by the name it was declared
// under, so that a test can say what one type holds without saying anything
// about the others.
func declarations(source string) map[string]string {
	bodies := make(map[string]string)

	for _, decl := range strings.Split(source, "\ntype ")[1:] {
		name, body, found := strings.Cut(decl, " ")
		if !found {
			continue
		}

		bodies[name] = body
	}

	return bodies
}

// TestThreeAlternativesUnrenamedStillCollide holds the other half of the
// decision: the layout is what tells two record types over one `01`-level apart,
// and a layout that does not is refused here exactly as it was before #164.
//
// The refusal is what makes the rename load-bearing rather than decorative. It
// is also why nothing in the layout format *requires* one: whether two record
// nodes carrying one name are a problem is a property of the target language,
// and this generator is the thing that has the answer for Go.
func TestThreeAlternativesUnrenamedStillCollide(t *testing.T) {
	t.Parallel()

	descriptor := transactions(t, plainRecords)

	err := generate(descriptor, t.TempDir(), options{packageName: "txn"})
	if err == nil {
		t.Fatal("three record types called TXN-REC generate without a collision")
	}

	if !strings.Contains(err.Error(), "TXN-REC") {
		t.Errorf("the collision does not name the record it is about: %v", err)
	}
}

// The two layouts the tests above resolve: the same records and the same
// discriminators, differing only in whether the record types are renamed.
const (
	renamedRecords = `(rename PURCHASE "TXN-PURCHASE-REC")
(rename REFUND   "TXN-REFUND-REC")
(rename ADJUST   "TXN-ADJUST-REC")
`

	plainRecords = ""
)

// transactions writes the redefined transaction record, resolves it, and hands
// back the descriptor.
func transactions(t *testing.T, renames string) *irpb.Descriptor {
	t.Helper()

	dir := t.TempDir()

	write(t, filepath.Join(dir, "txn.cpy"), fixedFormat(
		"01  TXN-REC.",
		"    05  TXN-ACCT        PIC X(4).",
		"    05  TXN-PURCHASE.",
		"        10  PUR-CODE    PIC X(2).",
		"        10  PUR-AMT     PIC X(6).",
		"    05  TXN-REFUND REDEFINES TXN-PURCHASE.",
		"        10  REF-CODE    PIC X(2).",
		"        10  REF-ORIG    PIC X(6).",
		"    05  TXN-ADJUST REDEFINES TXN-PURCHASE.",
		"        10  ADJ-CODE    PIC X(2).",
		"        10  ADJ-REASON  PIC X(6).",
	))

	write(t, filepath.Join(dir, "txn.sexpr"), `(encoding
  (charset ascii) (sign-convention ascii-zone-37)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 12))

(record PURCHASE (copybook "txn.cpy" TXN-REC) (alternative (item PURCHASE TXN-PURCHASE)))
(record REFUND   (copybook "txn.cpy" TXN-REC) (alternative (item REFUND   TXN-REFUND)))
(record ADJUST   (copybook "txn.cpy" TXN-REC) (alternative (item ADJUST   TXN-ADJUST)))

`+renames+`
(discriminate PURCHASE (equals (item PURCHASE TXN-PURCHASE PUR-CODE) "PU"))
(discriminate REFUND   (equals (item REFUND   TXN-REFUND   REF-CODE) "RF"))
(discriminate ADJUST   (equals (item ADJUST   TXN-ADJUST   ADJ-CODE) "AJ"))

(sequence (* (alt PURCHASE REFUND ADJUST)))
`)

	write(t, filepath.Join(dir, manifest.Name),
		`{"layout": "txn.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	run, err := project.Load(filepath.Join(dir, manifest.Name))
	if err != nil {
		t.Fatalf("the project does not resolve:\n%s", diag.Render(err))
	}

	return run.Descriptor
}

// write puts a file where a test needs one.
func write(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// fixedFormat renders copybook lines the way a copybook is written: fixed-format
// COBOL, with the six-column sequence area in front of every line.
func fixedFormat(lines ...string) string {
	var b strings.Builder

	for _, line := range lines {
		b.WriteString("      " + line + "\n")
	}

	return b.String()
}
