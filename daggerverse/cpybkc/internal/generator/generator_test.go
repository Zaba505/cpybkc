// These tests cover the pure half of composing a generator: the two strings a
// name is turned into, and the two names that are refused before either string
// is built. That is the half worth a test, because both strings are public
// behaviour from the first release — the filename is what cpybkc resolves on
// PATH, so getting it wrong produces an image that builds, pushes and then
// reports that a generator the manifest plainly names cannot be found.
//
// What is not tested here is anything that needs an engine: that /usr/local/bin
// is on the image's PATH, that the file lands executable, that a composed image
// actually runs the generator. Those are properties of the image rather than of
// this module — the root pipeline's ImageContract checks the first two against
// docs/container/SPEC.md, and #64 is the story that drives these compositions
// over an image the pipeline just built and proves the last one end to end.

package generator

import (
	"strings"
	"testing"
)

func TestExecutable(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		// Spelled out rather than assembled from executablePrefix. A test that
		// rebuilt the name the way Executable does would agree with it however
		// wrong both were.
		{name: "go", want: "cpybkc-gen-go"},
		{name: "hello", want: "cpybkc-gen-hello"},
		// docs/plugin/SPEC.md's SHOULD is lowercase ASCII, digits and hyphens; a
		// name using all three has to survive unchanged, prefix and nothing else.
		{name: "acme-cobol2", want: "cpybkc-gen-acme-cobol2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Executable(tc.name); got != tc.want {
				t.Errorf("Executable(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestRepository(t *testing.T) {
	for _, tc := range []struct {
		testName   string
		repository string
		name       string
		want       string
	}{
		{
			// The published family, spelled out: this is the reference a caller who
			// passed neither --repository nor --image reaches on their first
			// WithGenerator call.
			testName:   "the published default",
			repository: "ghcr.io/zaba505/cpybkc",
			name:       "go",
			want:       "ghcr.io/zaba505/cpybkc-gen-go",
		},
		{
			// The air-gap case, and the whole reason the derivation reads the
			// caller's repository rather than a constant: redirecting the CLI image
			// has to redirect its generators too.
			testName:   "an internal mirror redirects the family",
			repository: "registry.internal/mirrors/cpybkc",
			name:       "go",
			want:       "registry.internal/mirrors/cpybkc-gen-go",
		},
		{
			testName:   "a registry reached by host and port",
			repository: "localhost:5000/cpybkc",
			name:       "hello",
			want:       "localhost:5000/cpybkc-gen-hello",
		},
	} {
		t.Run(tc.testName, func(t *testing.T) {
			if got := Repository(tc.repository, tc.name); got != tc.want {
				t.Errorf("Repository(%q, %q) = %q, want %q", tc.repository, tc.name, got, tc.want)
			}
		})
	}
}

func TestCheckNameAccepts(t *testing.T) {
	for _, name := range []string{
		"go",
		"acme-cobol2",
		// Refused by docs/plugin/SPEC.md's SHOULD and not by either MUST. cpybkc
		// resolves it, so this module composes it: a convenience over a contract
		// does not get to make the contract smaller.
		"Weird_Name.v2",
		// Without a separator it cannot leave the plugin directory, so it needs no
		// rule of its own and must not acquire one by accident.
		"..",
	} {
		t.Run(name, func(t *testing.T) {
			if err := CheckName(name); err != nil {
				t.Errorf("CheckName(%q): unexpected error: %v", name, err)
			}
		})
	}
}

func TestCheckNameRefuses(t *testing.T) {
	for _, tc := range []struct {
		testName string
		name     string
		// mentions is a substring the error has to carry, so a refusal is checked
		// for saying what was wrong rather than merely for being non-nil.
		mentions string
	}{
		{
			// A +default fires on omission and name has none, so this arrives from
			// the command line as an empty string rather than being unreachable.
			testName: "an empty name",
			name:     "",
			mentions: "name is required",
		},
		{
			testName: "a name carrying a path separator",
			name:     "acme/go",
			mentions: "contains a /",
		},
		{
			// The shape that would otherwise write outside the plugin directory
			// entirely, which is the failure the separator rule is really for.
			testName: "a name escaping the plugin directory",
			name:     "../../etc/go",
			mentions: "contains a /",
		},
	} {
		t.Run(tc.testName, func(t *testing.T) {
			err := CheckName(tc.name)
			if err == nil {
				t.Fatalf("CheckName(%q) = nil, want an error", tc.name)
			}
			if !strings.Contains(err.Error(), tc.mentions) {
				t.Errorf("CheckName(%q) error = %q, want it to mention %q", tc.name, err, tc.mentions)
			}
		})
	}
}
