// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// The refusal of a binary width staircase that is not the descriptor's, in a
// package holding no binary item.
//
// No offset in this package depends on the staircase, and it is refused all
// the same: the staircase is the descriptor's whether or not an item reaches
// it, and a rule that depended on what the copybook holds would read a file
// today and refuse it once somebody added a COMP item to an unrelated record.
package counted

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Zaba505/cobol-go/codec"
)

// TestAPackageWithNoBinaryItemRefusesAnotherStaircase holds every entry point
// of a package holding no binary item that can be handed a staircase — the
// record methods, through a caller's own codec.Reader and codec.Writer — to the
// refusal the orders golden holds its own to. NewReader and NewWriter take no
// option carrying one, so there is nothing for them to refuse.
func TestAPackageWithNoBinaryItemRefusesAnotherStaircase(t *testing.T) {
	t.Parallel()

	enc := Encoding()
	enc.Binary = codec.BinarySize1248

	assertRefused := func(where string, err error) {
		t.Helper()

		if err == nil {
			t.Errorf("%s accepted the staircase 1-2-4-8, and the descriptor resolved 2-4-8", where)

			return
		}

		for _, want := range []string{
			"binary width staircase is 1-2-4-8",
			"offsets were computed under the descriptor's, 2-4-8",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s's refusal reads %q and does not say %q", where, err, want)
			}
		}
	}

	for name, rec := range map[string]interface {
		codec.Unmarshaler
		codec.Marshaler
	}{
		"HEADER-RECORD":  &HeaderRecord{},
		"DETAIL-RECORD":  &DetailRecord{},
		"SUMMARY-RECORD": &SummaryRecord{},
	} {
		r, err := codec.NewReader(bytes.NewReader(convertibleRun(t, Encoding())), enc)
		if err != nil {
			t.Fatalf("codec.NewReader: %v", err)
		}

		assertRefused(name+"'s UnmarshalCOBOL", rec.UnmarshalCOBOL(r))

		w, err := codec.NewWriter(&bytes.Buffer{}, enc)
		if err != nil {
			t.Fatalf("codec.NewWriter: %v", err)
		}

		assertRefused(name+"'s MarshalCOBOL", rec.MarshalCOBOL(w))
	}
}
