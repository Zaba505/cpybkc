// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package emitted

import (
	"fmt"
	"strings"

	"github.com/Zaba505/cobol-go/codec"
)

// literalsApart reports whether two literals still disagree somewhere over the
// bytes both of their predicates read, oneAt and otherAt being where in the
// record or the occurrence each literal starts.
//
// `resolve` proved the pair apart under the descriptor's axes, and
// re-expression carries that proof across two axes and not the other two
// (docs/ir/SPEC.md, "What cannot be re-expressed is refused before any record
// is read"). A code page is a bijection over its bytes and so is reversing
// them, but a sign convention may spell two columns with one byte, as
// ascii-zone-3-7 does positive and unsigned, and HFP spells one value more than
// one way. So the pairs whose proof could have been lost are held to it again,
// and this is the test: agreeing over every shared byte is two predicates one
// record satisfies at once.
func literalsApart(one []byte, oneAt int, other []byte, otherAt int) bool {
	start, end := max(oneAt, otherAt), min(oneAt+len(one), otherAt+len(other))

	for at := start; at < end; at++ {
		if one[at-oneAt] != other[at-otherAt] {
			return true
		}
	}

	return start >= end
}

// literalsNotApart is the refusal of a reader or a writer under whose axes two
// predicates `resolve` proved apart can both match one record.
//
// It names both literals, both fields and both records, and the axes that
// moved, and says that no file under those axes tells the two apart — which is
// a statement about the descriptor and the axes, raised before any record is
// read, and not about any file's data.
func literalsNotApart(from, to codec.Encoding, oneLiteral, oneField, oneRecord, otherLiteral, otherField, otherRecord string) error {
	return fmt.Errorf("%s of %s is compared against %s and %s of %s against %s, and under %s the two are the same bytes over every byte both read: "+
		"no file under those axes tells a %s from a %s there, so a record of one could be read as the other. "+
		"That is a property of the layout and the axes asked for, and not a fault in any file's data",
		oneField, oneRecord, oneLiteral, otherField, otherRecord, otherLiteral, movedAxes(from, to), oneRecord, otherRecord)
}

// movedAxes names the axes on which to differs from from, with the values to
// gives them.
func movedAxes(from, to codec.Encoding) string {
	var axes []string

	if !sameCharset(from.Charset, to.Charset) {
		axes = append(axes, "the charset "+to.Charset.Name())
	}

	if from.Sign != to.Sign {
		axes = append(axes, "the sign convention "+to.Sign.String())
	}

	if !sameByteOrder(from.ByteOrder, to.ByteOrder) {
		axes = append(axes, fmt.Sprintf("the byte order %v", to.ByteOrder))
	}

	if from.Float != to.Float {
		axes = append(axes, "the floating-point format "+to.Float.String())
	}

	if len(axes) == 0 {
		return "the axes asked for"
	}

	if len(axes) == 1 {
		return axes[0]
	}

	return strings.Join(axes[:len(axes)-1], ", ") + " and " + axes[len(axes)-1]
}
