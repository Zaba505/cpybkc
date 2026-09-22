// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/irpb"
)

// A variant inside a table whose declared maximum is *one* is a shape no
// producer in this repository could emit until #373: `resolve` asked whether the
// redefine was inside a table by reading the copybook's declared maximum, so at
// one it answered no and built record types instead. This generator has never
// carried a special case on the maximum, which is a reason to expect it to be
// right there and not a reason to leave it untested — the path had simply never
// been walked.
//
// `declaredMax` is where the maximum is not inert: it is the *M* a schedule's
// coverage is checked against, and at *M* = 1 a variant's two arms cannot cover
// 1..1 exactly once. So the two kinds of selector part company at this maximum,
// and that is what these two tests pin.

// optionalNote is a record of the shape `testdata/conformance/variant-optional`
// carries, over a table of whatever declared maximum the case hands it: a count,
// a group sized by it, a kind byte inside each occurrence, and a variant over
// the two alternatives the rest of the occurrence could hold.
//
// The arms are the same two widths whichever way they are selected, so a case
// that changes only the selector changes only the thing under test.
func optionalNote(maximum uint32, arms ...*irpb.Arm) *irpb.Descriptor {
	return &irpb.Descriptor{
		Version: supportedIRVersion,
		Nodes: []*irpb.Node{
			record(1, "OPT-RECORD", 2),
			group(2, "OPT-RECORD", nil, 10, 3),
			group(3, "OPT-NOTE", depending(10, 0, maximum), 11, 4),
			variant(4, arms...),
			equals(5, 11, "\xe3"),
			group(6, "OPT-NOTE-BODY", nil, 12),
			group(7, "OPT-NOTE-CODE", nil, 13, 14),
			equals(8, 11, "\xc3"),
			numericItem(10, "OPT-COUNT", irpb.Usage_USAGE_DISPLAY, 1, 1, 0, false),
			alphanumeric(11, "OPT-KIND", 1),
			alphanumeric(12, "OPT-TEXT", 6),
			alphanumeric(13, "OPT-REF", 4),
			slack(14, 2),
		},
	}
}

// byteSelectedNote is that record with its arms chosen by the kind byte, which
// is the shape #373 made reachable.
func byteSelectedNote(maximum uint32) *irpb.Descriptor {
	return optionalNote(maximum, armOf(5, 6), armOf(8, 7))
}

// codecOf generates a descriptor and returns the codec file it wrote.
func codecOf(t *testing.T, d *irpb.Descriptor) string {
	t.Helper()

	out := t.TempDir()
	if err := generate(io.Discard, d, out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
		t.Fatalf("generating: %v", err)
	}

	source, err := os.ReadFile(filepath.Join(out, codecFile))
	if err != nil {
		t.Fatalf("reading the generated codec: %v", err)
	}

	return string(source)
}

// TestAByteSelectedVariantInATableOfOneGeneratesWhatOneInAWiderTableDoes is the
// claim the story would otherwise be making without evidence.
//
// The two descriptors differ in one field of one repetition — the declared
// maximum — and a count read at run time is what says how many occurrences a
// record carries under either. The maximum is not inert even so: it is the bound
// a decoded count is held to, and both methods carry that check. So what is
// asserted is that the difference is *confined* to it — every line of the two
// sources that differs is one of those checks — which says it more exactly than
// asserting the presence of a switch would, because a special case on the
// maximum anywhere else in the walk shows up here whatever shape it took.
func TestAByteSelectedVariantInATableOfOneGeneratesWhatOneInAWiderTableDoes(t *testing.T) {
	t.Parallel()

	one := codecOf(t, byteSelectedNote(1))
	several := codecOf(t, byteSelectedNote(4))

	changed := differingLines(t, one, several)
	if len(changed) == 0 {
		t.Fatal("the two codecs are identical, so the declared maximum reached no count check at all")
	}

	for _, line := range changed {
		if strings.Contains(line, "occurs 0 to 1 times") || strings.Contains(line, "> 1 {") {
			continue
		}

		t.Errorf("the declared maximum reached the generated code outside the count check: %s", strings.TrimSpace(line))
	}

	// And it is the ordinary byte-selected shape rather than two identically
	// empty methods: the arm is chosen by comparing the occurrence's own bytes,
	// and an occurrence matching neither arm is reported.
	decode := method(one, "(x *OptRecord) UnmarshalCOBOL")
	if decode == "" {
		t.Fatalf("%s carries no decode method for OPT-RECORD", codecFile)
	}

	for _, want := range []string{
		"bytes.Equal",
		"no arm of the alternation over OPT-NOTE-BODY matches the entry",
	} {
		if !strings.Contains(decode, want) {
			t.Errorf("the decode method does not contain %q:\n%s", want, decode)
		}
	}
}

// differingLines is the lines of first that its counterpart in second does not
// match, and it fails the test where the two are not the same shape at all.
//
// Line for line rather than a diff, because the claim is that the two emissions
// are the same emission: a source that gained or lost a line is already the
// failure, and pairing what is left by position is what makes the lines that do
// differ readable as "this line and no other".
func differingLines(t *testing.T, first, second string) []string {
	t.Helper()

	a, b := strings.Split(first, "\n"), strings.Split(second, "\n")
	if len(a) != len(b) {
		t.Fatalf("the two codecs are %d and %d lines: the emission itself moved", len(a), len(b))
	}

	var changed []string

	for at := range a {
		if a[at] != b[at] {
			changed = append(changed, a[at])
		}
	}

	return changed
}

// TestAScheduledVariantInATableOfOneIsRefused is the other half, and it is the
// question the newly reachable shape leaves open answered rather than left to a
// coverage check to decide case by case.
//
// A variant needs two arms and a schedule has to cover 1..*M* exactly once. At
// *M* = 1 there is one occurrence, so every assignment of two arms over it
// either leaves an arm scheduling an occurrence the table has not got or has
// both arms scheduling the one it has. There is no third arrangement, so the
// form is refused for every layout that writes it — which is a statement about
// the shape and not about a particular schedule.
//
// `resolve` refuses it first, against the layout that wrote it. This generator
// refuses it again for the reason it re-checks every other schedule rule: the
// absence of a read-time failure is a claim about the emitted code, and a claim
// about emitted code is this generator's to establish rather than to assume.
func TestAScheduledVariantInATableOfOneIsRefused(t *testing.T) {
	t.Parallel()

	for name, arms := range map[string][]*irpb.Arm{
		"the second arm names an occurrence the table has not got": {
			scheduledArm(6, 1), scheduledArm(7, 2),
		},
		"both arms name the one occurrence there is": {
			scheduledArm(6, 1), scheduledArm(7, 1),
		},
		"the second arm is scheduled for nothing at all": {
			scheduledArm(6, 1), scheduledArm(7),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			out := t.TempDir()

			err := generate(io.Discard, optionalNote(1, arms...), out,
				options{packageName: goldenPackage, importPath: goldenImport})

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
