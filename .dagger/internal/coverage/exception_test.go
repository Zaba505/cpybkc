// These tests are the whole reason this package exists apart from the pipeline
// that uses it, for the reason internal/surface's are: what is built on it is a
// drift guard, and a drift guard is the one kind of check whose failure mode is
// staying green.
//
// The rows below are the ways an exception can be written badly, and each one is
// a way the mirroring stance would otherwise go unenforced — a flag on the escape
// hatch with no argument for being there, a gap nobody is closing recorded as
// though it were a decision, and a decision recorded as though somebody were
// still working on it.

package coverage

import (
	"strings"
	"testing"
)

func TestCheckAcceptsTheTwoShapesOfException(t *testing.T) {
	for _, tc := range []struct {
		name      string
		exception Exception
	}{
		{
			// The escape hatch is this flag's answer, and the argument for that is
			// written down. --version, --help and -h are this shape.
			name:      "settled, with a reason",
			exception: Exception{Reason: "the question is answered Dagger-natively", Settled: true},
		},
		{
			// A curated function is coming and the story writing it is named, so a
			// reader can tell this from a flag nobody thought about. --emit-ir is
			// this shape.
			name:      "tracked, with a reason and an issue",
			exception: Exception{Reason: "a Directory return cannot express a stream", Tracking: "#251"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.exception.Check("--flag"); err != nil {
				t.Fatalf("Check() = %v, want nil", err)
			}
		})
	}
}

func TestCheckRefusesAnExceptionThatClaimsNothing(t *testing.T) {
	for _, tc := range []struct {
		name      string
		exception Exception
		want      string
	}{
		{
			// The state this type exists to make unwritable: a flag on the escape
			// hatch and nothing said about why.
			name:      "the zero value",
			exception: Exception{},
			want:      "no reason is written beside it",
		},
		{
			// Settled without an argument is the old one-word entry wearing a
			// longer spelling.
			name:      "settled with no reason",
			exception: Exception{Settled: true},
			want:      "no reason is written beside it",
		},
		{
			name:      "tracked with no reason",
			exception: Exception{Tracking: "#251"},
			want:      "no reason is written beside it",
		},
		{
			// Neither claim made. This is the one that would otherwise pass by
			// being derived: an empty Tracking read as "settled" lets the claim be
			// made by forgetting.
			name:      "a reason, and neither settled nor tracked",
			exception: Exception{Reason: "because"},
			want:      "neither settled nor tracked",
		},
		{
			// Both claims made, which does not say which happens next.
			name:      "settled and tracked at once",
			exception: Exception{Reason: "because", Settled: true, Tracking: "#251"},
			want:      "both settled and tracked",
		},
		{
			name:      "a tracking value that is not an issue reference",
			exception: Exception{Reason: "because", Tracking: "251"},
			want:      "not an issue reference",
		},
		{
			// A typo in the one field that says this gap has an owner.
			name:      "issue zero",
			exception: Exception{Reason: "because", Tracking: "#0"},
			want:      "not an issue reference",
		},
		{
			name:      "a leading zero",
			exception: Exception{Reason: "because", Tracking: "#012"},
			want:      "not an issue reference",
		},
		{
			name:      "a URL rather than a reference",
			exception: Exception{Reason: "because", Tracking: "https://github.com/Zaba505/cpybkc/issues/251"},
			want:      "not an issue reference",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.exception.Check("--flag")
			if err == nil {
				t.Fatalf("Check() = nil, want an error mentioning %q", tc.want)
			}

			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Check() = %q, want it to mention %q", err, tc.want)
			}

			// Every message names the flag, because the caller reports these
			// alongside one another and a diagnostic that does not say which entry
			// it is about sends a contributor to read the whole table.
			if !strings.Contains(err.Error(), "--flag") {
				t.Errorf("Check() = %q, want it to name the flag it is about", err)
			}
		})
	}
}

func TestCheckReportsEveryFaultAtOnce(t *testing.T) {
	// An exception written in a hurry is usually wrong in more than one way, and
	// learning about the second one on the next run is a second run.
	err := Exception{Tracking: "nope"}.Check("--flag")
	if err == nil {
		t.Fatal("Check() = nil, want an error")
	}

	for _, want := range []string{"no reason is written beside it", "not an issue reference"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Check() = %q, want it to mention %q", err, want)
		}
	}
}
