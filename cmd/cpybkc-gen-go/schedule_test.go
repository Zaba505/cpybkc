// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/irpb"
)

// scheduledRecord is a record of discussion #340's shape with whatever
// selectors the case hands it: a table of entries, and a variant over the two
// alternatives their bytes could hold.
//
// The two arms are the same two widths whichever way they are selected, so a
// case that changes only the selector changes only the thing under test.
func scheduledRecord(arms ...*irpb.Arm) *irpb.Descriptor {
	return &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "ADDR-RECORD", 2),
			group(2, "ADDR-RECORD", nil, 3),
			group(3, "ADR-ENTRY", constant(2), 4),
			variant(4, arms...),
			equals(5, 11, "\xc4"),
			group(6, "ADR-HOME", nil, 11),
			group(7, "ADR-WORK", nil, 12, 13),
			alphanumeric(11, "HOME-STREET", 6),
			alphanumeric(12, "WORK-COMPANY", 4),
			slack(13, 2),
		},
	}
}

// method is one generated method's source, from its `func` line to the next
// declaration, or the empty string where the file carries no such method.
func method(source, signature string) string {
	_, body, found := strings.Cut(source, "\nfunc "+signature)
	if !found {
		return ""
	}

	head, _, more := strings.Cut(body, "\nfunc ")
	if !more {
		return body
	}

	return head
}

// TestAScheduledVariantReadsAnOccurrenceNumberAndAByteSelectedOneReadsBytes is
// docs/ir/SPEC.md's "An arm may be selected by its position in the table" as it
// reaches the page: the two kinds of selector are two shapes of switch, and an
// adopter reading the generated method can tell which they have.
//
// The second half is the one that is easy to lose. A scheduled variant cannot
// produce the "occurrence no arm matched" failure at all, so its switch carries
// no default — and that has to be *because* it is scheduled rather than because
// a default went missing, so the byte-selected form in the same package is held
// to still carrying one.
func TestAScheduledVariantReadsAnOccurrenceNumberAndAByteSelectedOneReadsBytes(t *testing.T) {
	t.Parallel()

	source := written(t, goldenDir)[codecFile]

	addr := method(source, "(x *AddrRecord) UnmarshalCOBOL")
	entry := method(source, "(x *EntryRecord) UnmarshalCOBOL")

	if addr == "" || entry == "" {
		t.Fatalf("%s carries no decode method for one of the two records", codecFile)
	}

	// The scheduled one switches on which occurrence this is, evaluates
	// nothing, and ends without the diagnostic.
	for _, unwanted := range []string{"bytes.Equal", "no arm of the alternation"} {
		if strings.Contains(addr, unwanted) {
			t.Errorf("the decode method for a scheduled variant contains %q", unwanted)
		}
	}

	if !strings.Contains(addr, "switch i0 + 1 {") {
		t.Errorf("the decode method for a scheduled variant does not switch on the occurrence number:\n%s", addr)
	}

	// And the byte-selected one is unchanged by any of it.
	for _, want := range []string{"switch {", "bytes.Equal", "no arm of the alternation over ENTRY-DETAIL matches the entry"} {
		if !strings.Contains(entry, want) {
			t.Errorf("the decode method for a byte-selected variant no longer contains %q", want)
		}
	}
}

// TestAWriterSelectsAScheduledArmOnNothingTheCallerSupplied is the same
// distinction on the encode side.
//
// A byte-selected variant is written by switching on which arm the caller left
// non-nil and then holding the occurrence's own bytes against that arm's
// predicate. A scheduled one does neither: it switches on the occurrence
// number, and what it does with the pointers is report a caller who filled in
// an arm the schedule did not assign.
func TestAWriterSelectsAScheduledArmOnNothingTheCallerSupplied(t *testing.T) {
	t.Parallel()

	source := written(t, goldenDir)[codecFile]

	addr := method(source, "(x *AddrRecord) MarshalCOBOL")
	if addr == "" {
		t.Fatalf("%s carries no encode method for ADDR-RECORD", codecFile)
	}

	if !strings.Contains(addr, "switch i0 + 1 {") {
		t.Errorf("the encode method for a scheduled variant does not switch on the occurrence number:\n%s", addr)
	}

	for _, unwanted := range []string{"satisfying no arm's predicate", "never derives a value satisfying one", "bytes.Equal"} {
		if strings.Contains(addr, unwanted) {
			t.Errorf("the encode method for a scheduled variant evaluates a predicate: it contains %q", unwanted)
		}
	}

	if !strings.Contains(addr, "a writer emits the arm the schedule assigns and never the one its caller named") {
		t.Errorf("the encode method for a scheduled variant does not report a caller naming another arm:\n%s", addr)
	}
}

// TestAScheduleThatDoesNotCoverTheTableIsRefused is what lets the switch above
// carry no default.
//
// resolve checks all of this and this generator checks it again, for the reason
// it re-checks the two-arm minimum: the absence of a read-time failure is a
// claim about the emitted code, and a claim about emitted code is this
// generator's to establish rather than to assume. Each of these would otherwise
// emit a total-looking switch that is not one.
func TestAScheduleThatDoesNotCoverTheTableIsRefused(t *testing.T) {
	t.Parallel()

	for name, arms := range map[string][]*irpb.Arm{
		"an occurrence no arm is scheduled for": {
			scheduledArm(6, 1), scheduledArm(7, 3),
		},
		"an occurrence two arms are scheduled for": {
			scheduledArm(6, 1, 2), scheduledArm(7, 2),
		},
		"an occurrence number past the declared maximum": {
			scheduledArm(6, 1), scheduledArm(7, 2, 9),
		},
		"an occurrence number of zero": {
			scheduledArm(6, 0, 1), scheduledArm(7, 2),
		},
		"an arm scheduled for nothing at all": {
			scheduledArm(6), scheduledArm(7, 1, 2),
		},
		"a schedule that does not ascend": {
			scheduledArm(6, 2, 1), scheduledArm(7, 3),
		},
		"arms of two kinds in one variant": {
			armOf(5, 6), scheduledArm(7, 1, 2),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			out := t.TempDir()

			err := generate(io.Discard, scheduledRecord(arms...), out, options{packageName: goldenPackage, importPath: goldenImport})

			var refusal *malformedError
			if !errors.As(err, &refusal) {
				t.Fatalf("generate returned %v, want a malformed descriptor", err)
			}

			if entries, err := os.ReadDir(out); err != nil {
				t.Fatalf("reading the output directory: %v", err)
			} else if len(entries) != 0 {
				t.Errorf("the refusal left %d files beneath --out, want none", len(entries))
			}
		})
	}
}

// TestAScheduledVariantOutsideATableIsRefused is the containment rule, which a
// schedule needs more sharply than a predicate does: there is no occurrence for
// an occurrence number to name.
func TestAScheduledVariantOutsideATableIsRefused(t *testing.T) {
	t.Parallel()

	d := scheduledRecord(scheduledArm(6, 1), scheduledArm(7, 2))

	// ADR-ENTRY stops repeating, and the variant inside it is then chosen once
	// for a group there is only one of.
	for _, node := range d.GetNodes() {
		if node.GetId() == 3 {
			node.GetGroup().Repetition = nil
		}
	}

	err := generate(io.Discard, d, t.TempDir(), options{packageName: goldenPackage, importPath: goldenImport})

	var refusal *malformedError
	if !errors.As(err, &refusal) {
		t.Fatalf("generate returned %v, want a malformed descriptor", err)
	}

	if !strings.Contains(refusal.What, "sits outside a group that repeats") {
		t.Errorf("the refusal reads %q and does not say the variant is outside a table", refusal.What)
	}
}

// TestAScheduleCoveringTheTableGenerates is the other side of the refusals
// above: the shapes a producer may emit are emitted from, including the one
// discussion #340 asked about, where a single alternative covers more than one
// entry.
//
// That last one is what the structural lowering could not have expressed —
// two unrolled members would carry one copybook name between them — so it is
// the case that says a schedule is a selector rather than an unrolling.
func TestAScheduleCoveringTheTableGenerates(t *testing.T) {
	t.Parallel()

	// A table of three, so that one alternative covering more than one entry is
	// sayable: an arm nothing selects is refused, so two arms over two
	// occurrences leave no room for it.
	table := func(arms ...*irpb.Arm) *irpb.Descriptor {
		d := scheduledRecord(arms...)

		for _, node := range d.GetNodes() {
			if node.GetId() == 3 {
				node.GetGroup().Repetition = constant(3)
			}
		}

		return d
	}

	for name, tc := range map[string]struct {
		nodes *irpb.Descriptor
		cases []string
	}{
		"one occurrence apiece": {
			nodes: table(scheduledArm(6, 1), scheduledArm(7, 2, 3)),
			cases: []string{"case 1:", "case 2, 3:"},
		},
		"the arms in the other order": {
			nodes: table(scheduledArm(7, 1), scheduledArm(6, 2, 3)),
			cases: []string{"case 1:", "case 2, 3:"},
		},
		"one alternative for entries a schedule does not run together": {
			nodes: table(scheduledArm(6, 1, 3), scheduledArm(7, 2)),
			cases: []string{"case 1, 3:", "case 2:"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			files, _ := generated(t, tc.nodes, options{packageName: goldenPackage, importPath: goldenImport})

			for _, want := range tc.cases {
				if !strings.Contains(files[codecFile], want) {
					t.Errorf("the generated switch carries no %q:\n%s", want, files[codecFile])
				}
			}
		})
	}
}
