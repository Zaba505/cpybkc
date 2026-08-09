// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"strings"
	"testing"
)

// TestItemRefRendersAsALayoutWritesIt is what every diagnostic naming an item
// quotes, so the rendering is the reference an adopter can search their layout
// for rather than a description of one.
func TestItemRefRendersAsALayoutWritesIt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		ref  ItemRef
		want string
	}{
		{
			name: "a field directly under a record's top-level item",
			ref:  ItemRef{Record: "ORDER-HEADER", Path: []string{"OH-PARTNER-REF"}},
			want: "(item ORDER-HEADER OH-PARTNER-REF)",
		},
		{
			name: "a field two levels down",
			ref:  ItemRef{Record: "ORDER-HEADER", Path: []string{"OH-KEY", "OH-CUST-NO"}},
			want: "(item ORDER-HEADER OH-KEY OH-CUST-NO)",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.ref.String(); got != testCase.want {
				t.Errorf("rendered as %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestOverridesOnDifferentItemsAreBothKept is the other side of the duplicate
// rule: what makes two overrides the same is the item, and a path passing
// through the same names is not the same path.
func TestOverridesOnDifferentItemsAreBothKept(t *testing.T) {
	t.Parallel()

	source := strings.Join([]string{
		"(encoding (charset cp037) (sign-convention ebcdic) (byte-order big-endian) (float-format hfp))",
		"(encoding-override (item ORDER-HEADER OH-KEY) (charset ascii))",
		"(encoding-override (item ORDER-HEADER OH-KEY OH-CUST-NO) (charset cp500))",
		"(encoding-override (item ORDER-DETAIL OH-KEY) (charset cp1047))",
	}, "\n")

	read, err := profile(t, source)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}

	want := []string{
		"(item ORDER-HEADER OH-KEY)",
		"(item ORDER-HEADER OH-KEY OH-CUST-NO)",
		"(item ORDER-DETAIL OH-KEY)",
	}

	if len(read.Overrides) != len(want) {
		t.Fatalf("read %d overrides, want %d", len(read.Overrides), len(want))
	}

	for i, override := range read.Overrides {
		if got := override.Item.String(); got != want[i] {
			t.Errorf("override %d is on %s, want %s", i, got, want[i])
		}
	}
}
