// These tests cover the vector Generate hands the CLI, which is public API in
// the same way imageref's default reference is: a caller who passes no manifest
// gets an invocation of no arguments, and what that means — the mounted
// project's own cpybkc.json, read from the working directory — is a promise
// docs/cli/SPEC.md makes and this module relies on rather than restates. Pinning
// the vector here turns a later accidental flag into a red build.
//
// What is not tested here is anything needing an engine: whether the exec
// succeeds, whether the project mounted at the working directory has a manifest
// in it, or what the CLI does with one. Those are the CLI's own tests and, for
// this module driven over a real image, #64's.

package argv

import (
	"slices"
	"strings"
	"testing"
)

func TestGenerateWithNoManifest(t *testing.T) {
	got, err := Generate("")
	if err != nil {
		t.Fatalf("Generate(\"\"): unexpected error: %v", err)
	}
	// No arguments at all, and not a --manifest naming the default. Spelling the
	// default here would be this module's second statement of where cpybkc looks
	// for a manifest, and the two would drift the moment that document changed.
	if len(got) != 0 {
		t.Errorf("Generate(\"\") = %q, want no arguments", got)
	}
}

func TestGenerateWithAManifest(t *testing.T) {
	for _, tc := range []struct {
		name     string
		manifest string
		want     []string
	}{
		{
			// The ordinary case: a project keeping its manifest below its root.
			name:     "a path below the project root",
			manifest: "build/cpybkc.json",
			want:     []string{"--manifest", "build/cpybkc.json"},
		},
		{
			name:     "a manifest named something else",
			manifest: "orders.json",
			want:     []string{"--manifest", "orders.json"},
		},
		{
			// Separated rather than joined, so a value carrying an equals sign
			// needs no thought about which one cpybkc cuts on.
			name:     "a path carrying an equals sign",
			manifest: "odd=name/cpybkc.json",
			want:     []string{"--manifest", "odd=name/cpybkc.json"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Generate(tc.manifest)
			if err != nil {
				t.Fatalf("Generate(%q): unexpected error: %v", tc.manifest, err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("Generate(%q) = %q, want %q", tc.manifest, got, tc.want)
			}
		})
	}
}

func TestGenerateRefusesTheDash(t *testing.T) {
	got, err := Generate("-")
	if err == nil {
		t.Fatalf("Generate(\"-\") = %q, want an error", got)
	}
	if got != nil {
		t.Errorf("Generate(\"-\") returned %q beside its error, want no vector", got)
	}
	// Checked for saying why rather than merely for being non-nil: a caller who
	// reached for the dash was thinking of a stream, and the reason they cannot
	// have one is the whole of what the message has to teach.
	if !strings.Contains(err.Error(), "in no directory") {
		t.Errorf("Generate(\"-\") error = %q, want it to say why a manifest cannot arrive on a stream", err)
	}
}
