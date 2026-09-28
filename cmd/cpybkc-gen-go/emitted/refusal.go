// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package emitted

import (
	"fmt"
)

// literalError is why one literal has no spelling under the axes asked for:
// the axis it could not cross, and what about the literal stopped it.
//
// It is not the refusal a caller sees. It knows the literal's bytes and the
// axes and nothing about where the literal is compared, which is what
// [cannotReexpress] adds.
type literalError struct {
	// axis is the axis the literal could not cross, spelled with the value
	// asked for — "the charset ASCII", "the sign convention realia".
	axis string

	// reason is what about the literal stopped it, as a clause following the
	// axis: "which has no byte for '€'".
	reason string
}

// Error implements the error interface.
func (e literalError) Error() string {
	return "under " + e.axis + ", " + e.reason
}

// cannotReexpress is the refusal of a reader or a writer over one literal that
// has no spelling under the axes it was built with.
//
// It names the literal, the field it is compared against, the record holding
// that field and the axis, and says what docs/ir/SPEC.md requires it to: that
// no file under those axes holds that value in that field. It says as plainly
// that it is not about a file's data, because it is raised before any record is
// read and the adopter meeting it has made no mistake in their data or their
// layout — they have asked for axes under which the layout's own distinction
// cannot be written down, and this is where they learn that rather than where
// they start looking for a corrupt record.
func cannotReexpress(literal, field, record string, err error) error {
	return fmt.Errorf("%s is compared against %s, which a file cannot spell %v: no file under those axes holds that value in %s, "+
		"so no %s in one could be told apart by it. That is a property of the layout and the axes asked for, and not a fault in any file's data",
		field+" of "+record, literal, err, field, record)
}
