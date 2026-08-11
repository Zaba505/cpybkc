// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package conformance

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestAFailureNamesTheEntryAndItsSource asserts the two things every way a
// corpus run can fail has to say (#68).
//
// Whoever reads a conformance failure has to decide whether the generator is
// wrong or the entry is, and that decision starts at which entry disagreed and
// at the section its expected answer was derived from. A report carrying only
// the disagreement leaves them grepping a directory of thirty entries for a
// field name.
func TestAFailureNamesTheEntryAndItsSource(t *testing.T) {
	const (
		entry  = "packed-ascii"
		source = `cobol-go codec/SPEC.md, "A.4 Packed decimal"`
	)

	tests := map[string]error{
		"a disagreement about what the bytes hold": &MismatchError{
			Entry:  entry,
			Source: source,
			Err:    fmt.Errorf("record 1 PACKED-RECORD.PACKED-POS is \"1234\" and the entry expects \"12345\""),
		},
		"a run that could not be made at all": &RunError{
			Entry:  entry,
			Source: source,
			Err:    fmt.Errorf("the generated code did not compile"),
		},
	}

	for name, err := range tests {
		t.Run(name, func(t *testing.T) {
			said := err.Error()

			for _, says := range []string{entry, source} {
				if !strings.Contains(said, says) {
					t.Errorf("the report is %q, and it does not say %q", said, says)
				}
			}

			if errors.Unwrap(err) == nil {
				t.Error("the report carries nothing to unwrap, and what went wrong is behind it")
			}
		})
	}
}
