// These tests cover the one piece of this module that is pure: assembling the
// reference New pulls. That is deliberately the piece worth a test, because the
// default it assembles is public API from the first release — a caller who
// passes nothing gets `ghcr.io/zaba505/cpybkc:v0`, and CONTRIBUTING.md's *The
// default image tag is the moving major tag* is the argument for that exact
// string. Pinning it here is what turns a later accidental change into a red
// build rather than a silent one; a comment cannot do that.
//
// What is not tested here is anything that needs an engine: whether the pull
// succeeds, what the image contains, whether the entrypoint is the CLI. Those
// are properties of the published image, the root module's ImageContract checks
// them against docs/container/SPEC.md, and #64 is the story that drives this
// module over an image the pipeline just built.

package imageref

import (
	"strings"
	"testing"
)

func TestAssembleDefaults(t *testing.T) {
	// The published default, spelled out rather than assembled from the same
	// constants the code uses. A test that rebuilt the string the way Assemble
	// does would agree with Assemble however wrong both were.
	const want = "ghcr.io/zaba505/cpybkc:v0"

	got, err := Assemble("ghcr.io/zaba505/cpybkc", "v0")
	if err != nil {
		t.Fatalf("Assemble with the documented defaults: unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("Assemble with the documented defaults = %q, want %q", got, want)
	}
}

func TestAssembleAccepts(t *testing.T) {
	for _, tc := range []struct {
		name       string
		repository string
		version    string
		want       string
	}{
		{
			name:       "a full version pins one release",
			repository: "ghcr.io/zaba505/cpybkc",
			version:    "v0.3.1",
			want:       "ghcr.io/zaba505/cpybkc:v0.3.1",
		},
		{
			// The air-gap case the repository argument exists for.
			name:       "an internal mirror",
			repository: "registry.internal/mirrors/cpybkc",
			version:    "v0",
			want:       "registry.internal/mirrors/cpybkc:v0",
		},
		{
			// A colon that is a port and not a tag. This is the case the
			// last-segment rule exists for, and refusing it would rule out a large
			// share of the private registries the argument is meant to serve.
			name:       "a registry reached by host and port",
			repository: "localhost:5000/cpybkc",
			version:    "v0",
			want:       "localhost:5000/cpybkc:v0",
		},
		{
			// docs/container/SPEC.md publishes a prerelease under its own full
			// version tag, so this has to be nameable.
			name:       "a prerelease tag",
			repository: "ghcr.io/zaba505/cpybkc",
			version:    "v0.3.0-rc.1",
			want:       "ghcr.io/zaba505/cpybkc:v0.3.0-rc.1",
		},
		{
			// The generator images of #63 derive from the repository by suffix, so
			// a name carrying hyphens has to survive.
			name:       "a companion image repository",
			repository: "ghcr.io/zaba505/cpybkc-gen-go",
			version:    "v0",
			want:       "ghcr.io/zaba505/cpybkc-gen-go:v0",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Assemble(tc.repository, tc.version)
			if err != nil {
				t.Fatalf("Assemble(%q, %q): unexpected error: %v", tc.repository, tc.version, err)
			}
			if got != tc.want {
				t.Errorf("Assemble(%q, %q) = %q, want %q", tc.repository, tc.version, got, tc.want)
			}
		})
	}
}

func TestAssembleRefuses(t *testing.T) {
	const digest = "sha256:abababababababababababababababababababababababababababababababab"

	for _, tc := range []struct {
		name       string
		repository string
		version    string
		// mentions is a substring the error has to carry, so that a refusal is
		// checked for saying which argument was wrong rather than merely for being
		// non-nil. An error naming the wrong argument sends the caller to edit
		// something that was already right.
		mentions string
	}{
		{
			// A +default fires on omission, not on an explicit empty value, so this
			// is reachable from the command line.
			name:       "an explicitly empty version",
			repository: "ghcr.io/zaba505/cpybkc",
			version:    "",
			mentions:   "version is required",
		},
		{
			name:       "an explicitly empty repository",
			repository: "",
			version:    "v0",
			mentions:   "repository is required",
		},
		{
			// The easy mistake: reading "repository" as "image reference". Left
			// unchecked this assembles a reference naming two tags.
			name:       "a repository that already carries a tag",
			repository: "registry.internal/mirrors/cpybkc:v0",
			version:    "v0",
			mentions:   "carries a tag",
		},
		{
			name:       "a repository that already carries a digest",
			repository: "ghcr.io/zaba505/cpybkc@" + digest,
			version:    "v0",
			mentions:   "carries a digest",
		},
		{
			// The caller who missed the sentence sending digests to --image. The
			// error has to point them at it.
			name:       "a digest passed as the version",
			repository: "ghcr.io/zaba505/cpybkc",
			version:    digest,
			mentions:   "--image",
		},
		{
			name:       "a whole reference passed as the version",
			repository: "ghcr.io/zaba505/cpybkc",
			version:    "ghcr.io/zaba505/cpybkc:v0",
			mentions:   "is not a tag",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Assemble(tc.repository, tc.version)
			if err == nil {
				t.Fatalf("Assemble(%q, %q) = %q, want an error", tc.repository, tc.version, got)
			}
			if got != "" {
				t.Errorf("Assemble(%q, %q) returned %q beside its error, want the empty string",
					tc.repository, tc.version, got)
			}
			if !strings.Contains(err.Error(), tc.mentions) {
				t.Errorf("Assemble(%q, %q) error = %q, want it to mention %q",
					tc.repository, tc.version, err, tc.mentions)
			}
		})
	}
}

func TestLastPathSegment(t *testing.T) {
	for _, tc := range []struct {
		repository string
		want       string
	}{
		{repository: "ghcr.io/zaba505/cpybkc", want: "cpybkc"},
		{repository: "localhost:5000/cpybkc", want: "cpybkc"},
		// No separator at all: the whole string is the last segment, which is what
		// makes a bare `cpybkc:v0` refusable.
		{repository: "cpybkc", want: "cpybkc"},
		{repository: "cpybkc:v0", want: "cpybkc:v0"},
	} {
		t.Run(tc.repository, func(t *testing.T) {
			if got := LastPathSegment(tc.repository); got != tc.want {
				t.Errorf("LastPathSegment(%q) = %q, want %q", tc.repository, got, tc.want)
			}
		})
	}
}
