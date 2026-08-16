// Every rule the mirroring stance rests on is a row here, and one of them is a
// row because it was missing. The first draft of this change asserted in three
// comments that the flag table does not accept the escape hatch as a value, and
// enforced it nowhere: a flag recorded as `"--new-flag": "Run"` passed the check
// green, which is precisely the drift the stance exists to catch. It was found in
// review rather than by a test, because the rule lived where no test could reach
// it.
//
// That is the argument for this file existing at all, so the rules stay here even
// though the tables they are applied to live in the pipeline.

package coverage

import (
	"strings"
	"testing"
)

// theModule is a stand-in for daggerverse/cpybkc: the rules are the same
// whichever module a record is about, and a fixture that named the real one
// would read as though they were not.
const theModule = "the/module"

// sound is a record with an answer of each kind and nothing wrong with it. The
// tests below break one thing about it at a time, which is what keeps a row's
// name honest — a fixture broken in two ways passes a test looking for either.
func sound() Record {
	return Record{
		Module:   theModule,
		Fallback: "Run",
		Mapped: map[string]string{
			"--manifest": "Generate",
			"--copybook": "Init",
		},
		Exceptions: map[string]Exception{
			"--help":    {Reason: "the question has a Dagger-native form", Settled: true},
			"--emit-ir": {Reason: "a stream destination", Tracking: "#251"},
		},
	}
}

func flags() []string     { return []string{"--copybook", "--emit-ir", "--help", "--manifest"} }
func functions() []string { return []string{"Generate", "Image", "Init", "Run"} }

func TestCheckAcceptsARecordThatAnswersEveryFlag(t *testing.T) {
	if err := sound().Check(flags(), functions()); err != nil {
		t.Fatalf("Check() = %v, want nil", err)
	}
}

func TestCheckRefusesTheWaysTheModuleAndTheCliDrift(t *testing.T) {
	for _, tc := range []struct {
		name      string
		record    func(Record) Record
		flags     []string
		functions []string
		want      string
	}{
		{
			// The CLI grew a flag and nobody said what the module does about it.
			name:  "a flag neither table answers",
			flags: append(flags(), "--jobs"),
			want:  "records nothing that answers it",
		},
		{
			// The rule review found missing. A mapping onto the escape hatch reads
			// in a diff exactly like a mapping onto a real function, and it is the
			// stance being abandoned one flag at a time.
			name: "a flag mapped onto the escape hatch",
			record: func(r Record) Record {
				r.Mapped["--jobs"] = "Run"
				return r
			},
			flags: append(flags(), "--jobs"),
			want:  "is the escape hatch rather than a function that carries a flag",
		},
		{
			name: "a flag in both tables",
			record: func(r Record) Record {
				r.Exceptions["--manifest"] = Exception{Reason: "because", Settled: true}
				return r
			},
			want: "does not at once",
		},
		{
			name: "a mapping onto a function the module does not declare",
			record: func(r Record) Record {
				r.Mapped["--manifest"] = "Regenerate"
				return r
			},
			want: "declares no such function",
		},
		{
			// Exception.Check's rules reach the record through here, so a
			// malformed entry fails the whole check rather than only the type.
			name: "an exception that claims nothing",
			record: func(r Record) Record {
				r.Exceptions["--help"] = Exception{Reason: "because"}
				return r
			},
			want: "neither settled nor tracked",
		},
		{
			name:  "a flag the CLI no longer accepts, mapped",
			flags: []string{"--copybook", "--emit-ir", "--help"},
			want:  "no longer accepts that flag",
		},
		{
			name:  "a flag the CLI no longer accepts, excepted",
			flags: []string{"--copybook", "--help", "--manifest"},
			want:  "no longer accepts that flag",
		},
		{
			// The route every exception is an exception to.
			name:      "an escape hatch the module does not declare",
			functions: []string{"Generate", "Image", "Init"},
			want:      "the escape hatch is what every exception is an exception to",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := sound()
			if tc.record != nil {
				record = tc.record(record)
			}

			have, declares := flags(), functions()
			if tc.flags != nil {
				have = tc.flags
			}

			if tc.functions != nil {
				declares = tc.functions
			}

			err := record.Check(have, declares)
			if err == nil {
				t.Fatalf("Check() = nil, want an error mentioning %q", tc.want)
			}

			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Check() = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestCheckReportsBothAnswersForAFlagInBothTables(t *testing.T) {
	// The flag is already a failure, but the contributor is about to delete one
	// of the two entries and should learn now whether the one they keep is sound.
	record := sound()
	record.Mapped["--help"] = "Regenerate"

	err := record.Check(flags(), functions())
	if err == nil {
		t.Fatal("Check() = nil, want an error")
	}

	for _, want := range []string{"does not at once", "declares no such function"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Check() = %q, want it to mention %q", err, want)
		}
	}
}

func TestCheckReportsEveryFlagsFaultAtOnce(t *testing.T) {
	// A record is usually wrong in more than one place at a time — the CLI grew a
	// flag and a function was renamed in the same pull request — and reporting one
	// fault per run is one run per fault.
	record := sound()
	record.Mapped["--manifest"] = "Regenerate"

	err := record.Check(append(flags(), "--jobs"), functions())
	if err == nil {
		t.Fatal("Check() = nil, want an error")
	}

	for _, want := range []string{"records nothing that answers it", "declares no such function"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Check() = %q, want it to mention %q", err, want)
		}
	}
}
