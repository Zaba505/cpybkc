// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package conformance

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Zaba505/cpybkc/irpb"
)

// AxesName is the member of an entry stating the axes its file is read and
// written under, where they are not the ones its descriptor resolved.
//
// It is optional, and an entry without one is read under the descriptor's own
// axes exactly as every entry was before it existed. It is a file of its own
// rather than a member of entry.json because it is part of the *question*: the
// adapter is handed it, where entry.json holds what the corpus says about the
// entry and never reaches an adapter at all (docs/conformance/SPEC.md, "A
// provisional entry").
const AxesName = "axes.json"

// Axes are the axes an entry's file is read and written under, each replacing
// the one every field of the descriptor resolved: docs/ir/SPEC.md's *read
// axes*, which a consumer MAY offer and MUST re-express every literal it
// compares under (#379, #383).
//
// Each member is optional and one left empty replaces nothing. The four a
// layout states are spelled as a layout spells them, so that an entry's author
// writes the same word in axes.json they would write in layout.sexpr. The
// fifth, BinarySize, is admitted only so that an entry can expect it to be
// refused: docs/ir/SPEC.md, "The staircase is not an axis a consumer may
// replace", makes reading under another staircase something no consumer does,
// so an entry stating one states a question whose one right answer is
// [Values.AxesRefused].
type Axes struct {
	// Charset is a code page as a layout spells one — "cp037", "cp500",
	// "cp1047", "cp1140" — or "ascii". Never "none": that is an item's
	// statement and not a file's, and an item carrying it keeps it under any
	// read axes.
	Charset string `json:"charset,omitempty"`

	// SignConvention is "ebcdic", "ascii-zone-37", "translated-ebcdic" or
	// "realia".
	SignConvention string `json:"sign_convention,omitempty"`

	// ByteOrder is "big-endian" or "little-endian".
	ByteOrder string `json:"byte_order,omitempty"`

	// FloatFormat is "ieee-754" or "hfp".
	FloatFormat string `json:"float_format,omitempty"`

	// BinarySize is a binary width staircase as codec/SPEC.md spells one —
	// "2-4-8", "1-2-4-8", "1--8" or "full". See the type's documentation for
	// why stating one is a question with one answer.
	BinarySize string `json:"binary_size,omitempty"`
}

// The spellings each axis admits.
//
// The four a layout states are the layout's own words (docs/layout/SPEC.md,
// "The encoding profile"), and the staircase is codec's, since no layout states
// one. A spelling outside these is refused rather than passed through: an
// adapter handed a word it does not know would have to guess at it, and a guess
// at an axis is the silent failure every one of them is.
var (
	charsets        = []string{"ascii", "cp037", "cp500", "cp1047", "cp1140"}
	signConventions = []string{"ebcdic", "ascii-zone-37", "translated-ebcdic", "realia"}
	byteOrders      = []string{"big-endian", "little-endian"}
	floatFormats    = []string{"ieee-754", "hfp"}
	binarySizes     = []string{"2-4-8", "1-2-4-8", "1--8", "full"}
)

// check is what is wrong with a set of axes on its own, before the descriptor
// they replace is consulted.
func (a *Axes) check() []error {
	var faults []error

	stated := false

	for _, axis := range []struct {
		name, value string
		admitted    []string
	}{
		{"charset", a.Charset, charsets},
		{"sign_convention", a.SignConvention, signConventions},
		{"byte_order", a.ByteOrder, byteOrders},
		{"float_format", a.FloatFormat, floatFormats},
		{"binary_size", a.BinarySize, binarySizes},
	} {
		if axis.value == "" {
			continue
		}

		stated = true

		if !slices.Contains(axis.admitted, axis.value) {
			faults = append(faults, fmt.Errorf("%s: %s is %q, and it is one of %s",
				AxesName, axis.name, axis.value, strings.Join(quotedAll(axis.admitted), ", ")))
		}
	}

	if !stated {
		faults = append(faults, fmt.Errorf("%s states no axis; an entry read under its descriptor's own axes carries no %s",
			AxesName, AxesName))
	}

	return faults
}

// against is what is wrong with a set of axes as the replacement for the ones
// the descriptor resolved, and with the values document expected under them.
//
// Two rules, both about the staircase. An entry stating one expects it refused,
// since that is the only answer a consumer may give; and it may not state the
// descriptor's own, which replaces nothing and would expect a refusal no
// consumer owes.
func (a *Axes) against(descriptor *irpb.Descriptor, values *Values) []error {
	if a.BinarySize == "" {
		return nil
	}

	var faults []error

	if values != nil && values.AxesRefused == "" {
		faults = append(faults, fmt.Errorf("%s states the binary width staircase %q, and a consumer refuses every staircase "+
			"but the descriptor's, so %s expects axes_refused", AxesName, a.BinarySize, ValuesName))
	}

	for _, node := range descriptor.GetNodes() {
		field := node.GetField()
		if field == nil {
			continue
		}

		if staircase(field.GetEncoding().GetBinarySize()) == a.BinarySize {
			faults = append(faults, fmt.Errorf("%s states the binary width staircase %q, which is the descriptor's own and "+
				"replaces nothing", AxesName, a.BinarySize))

			break
		}
	}

	return faults
}

// staircase is a descriptor's staircase as axes.json spells it, or the empty
// string for one it does not name.
func staircase(size irpb.BinarySize) string {
	switch size {
	case irpb.BinarySize_BINARY_SIZE_248:
		return "2-4-8"
	case irpb.BinarySize_BINARY_SIZE_1248:
		return "1-2-4-8"
	case irpb.BinarySize_BINARY_SIZE_SMALLEST:
		return "1--8"
	case irpb.BinarySize_BINARY_SIZE_FULL:
		return "full"
	default:
		return ""
	}
}

// readAxes reads axes.json, where the entry carries one.
func (e *Entry) readAxes() error {
	b, err := os.ReadFile(filepath.Join(e.Dir, AxesName))
	if os.IsNotExist(err) {
		return nil
	}

	if err != nil {
		return err
	}

	var axes Axes
	if err := decodeOne(b, &axes); err != nil {
		return fmt.Errorf("%s: %w", AxesName, err)
	}

	if faults := axes.check(); len(faults) > 0 {
		return joined(faults)
	}

	e.Axes = &axes

	return nil
}

// quotedAll is every spelling as a diagnostic quotes it.
func quotedAll(spellings []string) []string {
	quoted := make([]string, 0, len(spellings))

	for _, spelling := range spellings {
		quoted = append(quoted, fmt.Sprintf("%q", spelling))
	}

	return quoted
}
