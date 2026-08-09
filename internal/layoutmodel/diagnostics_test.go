// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
)

// brokenLayout is one layout with three independent faults in it, in three
// different places.
//
// Nothing here depends on any two of them: a charset nobody has a table for, an
// axis the profile never states, and an override naming no axis at all are
// three things an adopter has to fix, and a reader that stopped at the first
// would be a reader they ran three times.
const brokenLayout = `(encoding
  (charset cp999)
  (sign-convention ebcdic)
  (byte-order big-endian))
(encoding-override (item ORDER-HEADER OH-AMOUNT))`

// goldenDiagnostics is what a caller holding the rejected read and a terminal
// sees, written out in full.
//
// It is the golden docs/layout/SPEC.md's "Every diagnostic carries a span" is
// checked against on the way a reader's faults actually reach a reader: every
// line names the file, the line and the column of the sub-form that is wrong,
// and all three faults are there rather than the first alone.
const goldenDiagnostics = `orders.sexpr:2:12: charset is one of cp037, cp500, cp1047, cp1140 or ascii, and this one says "cp999"
orders.sexpr:1:1: the encoding profile states no float-format; all four axes are required and none of them has a default
orders.sexpr:5:1: the override on (item ORDER-HEADER OH-AMOUNT) states no axis; an override states at least one of charset, sign-convention, byte-order or float-format`

// TestEveryFaultRendersWithItsFileLineAndColumn is the acceptance criterion
// [github.com/Zaba505/cpybkc/internal/diag] exists to hold this package to.
//
// A reader here returns its faults joined, and [diag.Render] is what turns that
// into something an adopter reads. The two halves this pins are that the faults
// are accumulated rather than cut off at the first, and that each names where
// it is — which is what makes a rejected layout a list of places to open rather
// than a report that something is wrong with it.
func TestEveryFaultRendersWithItsFileLineAndColumn(t *testing.T) {
	t.Parallel()

	file, err := layout.Parse("orders.sexpr", strings.NewReader(brokenLayout))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	profile, err := ReadProfile(file)
	if err == nil {
		t.Fatalf("the reader accepted a layout with three faults in it: %+v", profile)
	}

	if got := diag.Render(err); got != goldenDiagnostics {
		t.Errorf("the rendering is not the golden\n got:\n%s\nwant:\n%s", got, goldenDiagnostics)
	}

	found := diag.Diagnostics(err)
	if len(found) != 3 {
		t.Fatalf("reported %d faults, want 3:\n%s", len(found), diag.Render(err))
	}

	for _, diagnostic := range found {
		if !strings.HasPrefix(diagnostic.Message, "orders.sexpr:") {
			t.Errorf("a fault does not name the file it is in: %s", diagnostic.Message)
		}
	}
}
