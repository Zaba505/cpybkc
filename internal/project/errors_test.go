// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package project

import "testing"

// TestAndJoinsAListTheWayASentenceDoes pins every arm of [and], which is the one
// place in this package where a diagnostic's wording is assembled rather than
// formatted.
//
// It is here because [and] was rewritten when `modernize` was enforced (#257):
// the `default` arm was a `+=` loop and is now a [strings.Join], and the two
// spellings agree on every length the arm can see but disagree at one it cannot
// — a single item, where the loop contributed a trailing `", "` and the join
// contributes none. That length is unreachable, because `case 1` is above the
// `default`. This test is what makes the unreachability a fact the package
// states rather than one a reader has to re-derive from the switch.
func TestAndJoinsAListTheWayASentenceDoes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		about string
		items []string
		want  string
	}{
		{
			about: "nothing at all is a word and not an empty string, because it is read in a sentence",
			items: nil,
			want:  "nothing",
		},
		{
			about: "one item is the item, with nothing joining it to anything",
			items: []string{"A"},
			want:  "A",
		},
		{
			about: "two items take no comma, which is what English does",
			items: []string{"A", "B"},
			want:  "A and B",
		},
		{
			about: "three items take the serial comma",
			items: []string{"A", "B", "C"},
			want:  "A, B, and C",
		},
		{
			about: "four items put a comma between every pair and before the last",
			items: []string{"A", "B", "C", "D"},
			want:  "A, B, C, and D",
		},
		{
			about: "an item that is two words is still one item, which is what the serial comma is for",
			items: []string{"A", "B", "C D"},
			want:  "A, B, and C D",
		},
	}

	for _, test := range tests {
		t.Run(test.about, func(t *testing.T) {
			t.Parallel()

			if got := and(test.items); got != test.want {
				t.Errorf("and(%q) is %q, want %q", test.items, got, test.want)
			}
		})
	}
}
