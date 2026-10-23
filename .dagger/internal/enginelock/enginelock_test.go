// These tests pin what EngineLock reads out of a dagger-module.toml and what it
// says when the two modules disagree. The check is a guard, and a guard's failure
// mode is staying green, so every layout it must read and every shape it must
// refuse is a row here.
package enginelock_test

import (
	"strings"
	"testing"

	"dagger/cpybkc/internal/enginelock"
)

const rootPath = "dagger-module.toml"
const companionPath = "daggerverse/cpybkc/dagger-module.toml"

func TestVersion(t *testing.T) {
	cases := []struct {
		name     string
		contents string
		want     string
	}{
		{
			name:     "as dagger generate writes it",
			contents: "name = \"cpybkc\"\nengineVersion = \"v1.0.0-beta.15\"\nsource = \".dagger\"\n\n[runtime]\n  source = \"go\"\n\n[codegen]\n  automaticGitignore = false\n",
			want:     "v1.0.0-beta.15",
		},
		{
			name:     "a literal string",
			contents: "name = 'cpybkc'\nengineVersion = 'v1.0.0-beta.15'\n",
			want:     "v1.0.0-beta.15",
		},
		{
			name:     "a quoted key",
			contents: "\"engineVersion\" = \"v1.0.0-beta.15\"\n",
			want:     "v1.0.0-beta.15",
		},
		{
			name:     "a trailing comment",
			contents: "engineVersion = \"v1.0.0-beta.15\" # moves with z5labs/devex\n",
			want:     "v1.0.0-beta.15",
		},
		{
			name:     "beside dependency tables",
			contents: "engineVersion = \"v1.0.0-beta.15\"\n\n[[dependencies]]\n  name = \"companion\"\n  source = \"daggerverse/cpybkc\"\n",
			want:     "v1.0.0-beta.15",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := enginelock.Version(rootPath, tc.contents)
			if err != nil {
				t.Fatalf("Version: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Version = %q, want %q", got, tc.want)
			}
		})
	}
}

// A key of the same name inside a table is not the module's engine version, and
// reading it as one is exactly the mistake a pattern match makes.
func TestVersionIgnoresNestedKeys(t *testing.T) {
	contents := "name = \"cpybkc\"\n\n[runtime]\n  engineVersion = \"v1.0.0-beta.15\"\n"

	if got, err := enginelock.Version(rootPath, contents); err == nil {
		t.Fatalf("Version = %q, want an error: a nested engineVersion is not the module's", got)
	}
}

func TestVersionErrorsNameTheManifest(t *testing.T) {
	cases := []struct {
		name     string
		contents string
		want     string
	}{
		{name: "absent", contents: "name = \"cpybkc\"\n", want: "declares no engineVersion"},
		{name: "empty", contents: "engineVersion = \"\"\n", want: "declares no engineVersion"},
		{name: "not TOML", contents: "{\"engineVersion\": \"v0.21.8\"}\n", want: "parsing"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := enginelock.Version(companionPath, tc.contents)
			if err == nil {
				t.Fatal("Version: want an error")
			}
			if !strings.Contains(err.Error(), companionPath) {
				t.Errorf("error %q does not name %s", err, companionPath)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not say %q", err, tc.want)
			}
		})
	}
}

func TestAgree(t *testing.T) {
	if err := enginelock.Agree(rootPath, "v1.0.0-beta.15", companionPath, "v1.0.0-beta.15"); err != nil {
		t.Fatalf("Agree on equal versions: %v", err)
	}
}

// A bump made in one module and not the other is the failure the check exists
// for, in both directions: the dependency edge catches only a companion that has
// run ahead of the engine.
func TestAgreeRefusesABumpInOneModule(t *testing.T) {
	cases := []struct {
		name      string
		root      string
		companion string
	}{
		{name: "root bumped alone", root: "v1.0.0-beta.16", companion: "v1.0.0-beta.15"},
		{name: "companion bumped alone", root: "v1.0.0-beta.15", companion: "v1.0.0-beta.16"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := enginelock.Agree(rootPath, tc.root, companionPath, tc.companion)
			if err == nil {
				t.Fatal("Agree: want an error")
			}
			for _, want := range []string{rootPath, companionPath, tc.root, tc.companion, enginelock.Manifest} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err, want)
				}
			}
		})
	}
}
