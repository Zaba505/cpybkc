// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"testing"
)

// TestJoinAndVerbAgreesWithTheListItFollows covers the rendering every message
// that names a list and then says something about it goes through, at each of
// the lengths the list can be. A verb written into the format string instead is
// correct at exactly one of them.
func TestJoinAndVerbAgreesWithTheListItFollows(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		names []string
		want  string
	}{
		{names: nil, want: "nothing redefines"},
		{names: []string{"SPLIT"}, want: "SPLIT redefines"},
		{names: []string{"SPLIT", "HALVES"}, want: "SPLIT and HALVES redefine"},
		{
			names: []string{"SPLIT", "HALVES", "QUARTERS"},
			want:  "SPLIT, HALVES and QUARTERS redefine",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.want, func(t *testing.T) {
			t.Parallel()

			if got := joinAndVerb(testCase.names, "redefines", "redefine"); got != testCase.want {
				t.Errorf("rendered %q, want %q", got, testCase.want)
			}
		})
	}
}
