// These tests cover the whole of what this module decides about an environment
// variable name, which is deliberately two refusals. The accepted cases are here
// in the same number as the refused ones, because the interesting claim is the
// one a stricter rule would break: a module that grew a POSIX-shaped name check
// would pass every refusal below and start refusing runs cpybkc performs.
//
// What is not tested here is that the variable reaches anything. That is three
// hops — the module sets it on a container, the container starts cpybkc with it,
// and cpybkc passes its whole environment through to the generator — and the
// last one is the point, so it is asserted end to end where an engine is
// available: the root pipeline's CompanionModule check runs a generator that
// reports what it was started with (#252).

package env

import (
	"strings"
	"testing"
)

func TestCheckNameAcceptsWhatCanBeAName(t *testing.T) {
	for _, tc := range []struct {
		testName string
		name     string
	}{
		{
			// The one variable docs/plugin/SPEC.md says legitimately reaches a
			// generator and changes its output, and the reason this builder exists
			// at all (#47).
			testName: "SOURCE_DATE_EPOCH",
			name:     "SOURCE_DATE_EPOCH",
		},
		{
			// A variable of the caller's own. cpybkc names none of its own and
			// removes none, so what else a build has already exported is not this
			// module's business.
			testName: "a caller's own variable",
			name:     "ACME_BUILD_ID",
		},
		{
			// Lowercase, which POSIX reserves *for applications* rather than
			// forbids. The shell exports it, cpybkc is started with it and a
			// generator reads it, so refusing it here would refuse a run the CLI
			// performs.
			testName: "lowercase",
			name:     "http_proxy",
		},
		{
			// Leading digit, which no shell's assignment syntax can write and
			// `env 1PASSWORD=x` sets anyway. It is not a name this module has a
			// reason to have an opinion about: what a plugin may be handed is the
			// plugin contract's to narrow.
			testName: "leading digit",
			name:     "1PASSWORD_TOKEN",
		},
	} {
		t.Run(tc.testName, func(t *testing.T) {
			if err := CheckName(tc.name); err != nil {
				t.Errorf("CheckName(%q) = %v, want no error", tc.name, err)
			}
		})
	}
}

func TestCheckValue(t *testing.T) {
	for _, tc := range []struct {
		testName string
		value    string
		// want is a substring the refusal has to carry, and an empty one is a
		// value that has to be accepted.
		want string
	}{
		{
			testName: "an epoch",
			value:    "1700000000",
		},
		{
			// A value may be empty, and an exported variable with no value is
			// set: cpybkc would have carried it, so this does too.
			testName: "empty",
			value:    "",
		},
		{
			// The one character that is a name's problem and not a value's. An
			// environment string is split at the first =, so everything after
			// it is value by definition.
			testName: "carries equals signs",
			value:    "-X main.version=v1 -X main.commit=abc",
		},
		{
			testName: "carries a NUL byte",
			value:    "1700000000\x00trailing",
			want:     "NUL",
		},
	} {
		t.Run(tc.testName, func(t *testing.T) {
			err := CheckValue("SOURCE_DATE_EPOCH", tc.value)

			if tc.want == "" {
				if err != nil {
					t.Errorf("CheckValue(%q) = %v, want no error", tc.value, err)
				}

				return
			}

			if err == nil {
				t.Fatalf("CheckValue(%q) = nil, want an error", tc.value)
			}

			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("CheckValue(%q) = %q, which does not mention %q", tc.value, err, tc.want)
			}

			// The name, because a refusal that does not say which variable was
			// being set is one a caller with several has to bisect.
			if !strings.Contains(err.Error(), "SOURCE_DATE_EPOCH") {
				t.Errorf("CheckValue(%q) = %q, which does not name the variable it refused", tc.value, err)
			}
		})
	}
}

func TestCheckNameRefusesWhatCannotBeAName(t *testing.T) {
	for _, tc := range []struct {
		testName string
		name     string
		// wants are substrings the message has to carry. They are the fact the
		// reader needs and not the wording: what was refused, and why it could
		// never have arrived.
		wants []string
	}{
		{
			testName: "empty",
			name:     "",
			wants:    []string{"required", "NAME=VALUE"},
		},
		{
			testName: "carries an equals sign",
			name:     "SOURCE_DATE_EPOCH=1700000000",
			wants:    []string{"SOURCE_DATE_EPOCH=1700000000", "first ="},
		},
		{
			// The one that is easy to leave out and impossible to see: a name
			// truncated at a NUL arrives as a shorter name that looks right in
			// every diagnostic printed afterwards.
			testName: "carries a NUL byte",
			name:     "SOURCE_DATE\x00_EPOCH",
			wants:    []string{"NUL"},
		},
	} {
		t.Run(tc.testName, func(t *testing.T) {
			err := CheckName(tc.name)
			if err == nil {
				t.Fatalf("CheckName(%q) = nil, want an error", tc.name)
			}

			for _, want := range tc.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("CheckName(%q) = %q, which does not mention %q", tc.name, err, want)
				}
			}
		})
	}
}
