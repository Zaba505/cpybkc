// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// read is the whole of what a caller does: hand [Read] a manifest and a name for
// it. It fails the test on a manifest that will not read, because every test
// using it is about what a good one becomes.
func read(t *testing.T, source string) *Manifest {
	t.Helper()

	m, err := Read(Name, strings.NewReader(source))
	if err != nil {
		t.Fatalf("read the manifest:\n%s", diag.Render(err))
	}

	return m
}

// worked is a manifest with one of everything in it: shared inputs, a layout,
// two generators, one of them with inputs and options of its own.
const worked = `{
  "inputs": ["cpy/orders.cpy", "cpy/common.cpy"],
  "layout": "orders.sexpr",
  "generators": [
    {
      "name": "go",
      "out": "gen",
      "inputs": ["cpy/orders-go.cpy"],
      "options": {"package_name": "orders", "receiver": ""}
    },
    {
      "name": "json-schema",
      "out": "schema"
    }
  ]
}`

func TestReadTakesAManifestApart(t *testing.T) {
	m := read(t, worked)

	if m.File != Name {
		t.Errorf("the manifest was read under %q, want %q", m.File, Name)
	}

	if m.Layout != "orders.sexpr" {
		t.Errorf("the layout is %q, want %q", m.Layout, "orders.sexpr")
	}

	wantInputs := []string{"cpy/orders.cpy", "cpy/common.cpy"}
	if !slices.Equal(m.Inputs, wantInputs) {
		t.Errorf("the inputs are %q, want %q", m.Inputs, wantInputs)
	}

	if len(m.Generators) != 2 {
		t.Fatalf("the manifest declares %d generators, want 2", len(m.Generators))
	}

	first, second := m.Generators[0], m.Generators[1]

	if first.Name != "go" || first.Out != "gen" {
		t.Errorf("the first generator is %q into %q, want %q into %q", first.Name, first.Out, "go", "gen")
	}

	if !slices.Equal(first.Inputs, []string{"cpy/orders-go.cpy"}) {
		t.Errorf("the first generator reads %q, want %q", first.Inputs, []string{"cpy/orders-go.cpy"})
	}

	// An option value may be empty; docs/plugin/SPEC.md says so, and a manifest
	// is where one is written.
	wantOptions := []Option{{Key: "package_name", Value: "orders"}, {Key: "receiver", Value: ""}}
	if !slices.Equal(first.Options, wantOptions) {
		t.Errorf("the first generator's options are %v, want %v", first.Options, wantOptions)
	}

	// The merge, end to end: what the entry declared, behind what the manifest
	// shares, in the order both were written.
	wantMerged := []string{"cpy/orders.cpy", "cpy/common.cpy", "cpy/orders-go.cpy"}
	if got := m.InputsFor(first); !slices.Equal(got, wantMerged) {
		t.Errorf("the first generator reads %q in all, want %q", got, wantMerged)
	}

	if got := m.InputsFor(second); !slices.Equal(got, wantInputs) {
		t.Errorf("the second generator reads %q in all, want %q", got, wantInputs)
	}

	if second.Name != "json-schema" || second.Out != "schema" {
		t.Errorf("the second generator is %q into %q, want %q into %q",
			second.Name, second.Out, "json-schema", "schema")
	}

	if second.Inputs != nil || second.Options != nil {
		t.Errorf("the second generator declares inputs %q and options %v, want neither",
			second.Inputs, second.Options)
	}
}

// TestAGeneratorKnowsWhereItWasWritten is what a fault found after the read —
// a name that resolves to no executable (#41), two generators writing one file
// (#44) — points at.
func TestAGeneratorKnowsWhereItWasWritten(t *testing.T) {
	m := read(t, worked)

	want := []diag.Span{
		{File: Name, Line: 5, Column: 5},
		{File: Name, Line: 11, Column: 5},
	}

	for i, generator := range m.Generators {
		if generator.Span != want[i] {
			t.Errorf("generators[%d] is at %v, want %v", i, generator.Span, want[i])
		}
	}
}

// TestOptionsKeepTheOrderTheManifestDeclaresThem is docs/plugin/SPEC.md's
// requirement that cpybkc pass the options "in the order the manifest declares
// them, so that the vector is a function of the manifest rather than of a map
// iteration". The keys below are in no order a map would reproduce, and a
// reader that unmarshalled them into one would fail this test most of the time
// rather than every time — which is why the order is a property of the type and
// not of a sort applied afterwards.
func TestOptionsKeepTheOrderTheManifestDeclaresThem(t *testing.T) {
	m := read(t, `{
  "layout": "orders.sexpr",
  "generators": [
    {
      "name": "go",
      "out": "gen",
      "options": {"z": "1", "a": "2", "m": "3", "b": "4", "y": "5", "c": "6", "n": "7", "d": "8"}
    }
  ]
}`)

	want := []string{"z", "a", "m", "b", "y", "c", "n", "d"}

	var got []string
	for _, option := range m.Generators[0].Options {
		got = append(got, option.Key)
	}

	if !slices.Equal(got, want) {
		t.Errorf("the options are declared %q, want %q", got, want)
	}
}

func TestInputsForMergesTheManifestsInputsWithAGenerators(t *testing.T) {
	tests := []struct {
		name      string
		manifest  []string
		generator []string
		want      []string
	}{
		{
			name: "neither states one",
		},
		{
			name:     "the manifest alone",
			manifest: []string{"cpy/orders.cpy"},
			want:     []string{"cpy/orders.cpy"},
		},
		{
			name:      "the generator alone",
			generator: []string{"cpy/orders.cpy"},
			want:      []string{"cpy/orders.cpy"},
		},
		{
			name:      "the manifest's first, then the generator's",
			manifest:  []string{"cpy/orders.cpy", "cpy/common.cpy"},
			generator: []string{"cpy/orders-go.cpy"},
			want:      []string{"cpy/orders.cpy", "cpy/common.cpy", "cpy/orders-go.cpy"},
		},
		{
			name:      "a copybook both name is read once",
			manifest:  []string{"cpy/orders.cpy", "cpy/common.cpy"},
			generator: []string{"cpy/common.cpy", "cpy/orders-go.cpy"},
			want:      []string{"cpy/orders.cpy", "cpy/common.cpy", "cpy/orders-go.cpy"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := &Manifest{Inputs: test.manifest}

			got := m.InputsFor(Generator{Inputs: test.generator})
			if !slices.Equal(got, test.want) {
				t.Errorf("the generator reads %q, want %q", got, test.want)
			}
		})
	}
}

func TestReadFileReadsAManifestFromDisk(t *testing.T) {
	path := filepath.Join("testdata", Name)

	m, err := ReadFile(path)
	if err != nil {
		t.Fatalf("read %s:\n%s", path, diag.Render(err))
	}

	if m.File != path {
		t.Errorf("the manifest was read under %q, want %q", m.File, path)
	}

	if m.Layout != "orders.sexpr" {
		t.Errorf("the layout is %q, want %q", m.Layout, "orders.sexpr")
	}

	if len(m.Generators) != 1 {
		t.Fatalf("the manifest declares %d generators, want 1", len(m.Generators))
	}
}

func TestReadFileReportsAManifestThatIsNotThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), Name)

	_, err := ReadFile(path)

	var missing *NotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("reading a manifest that is not there gave %v, want a *NotFoundError", err)
	}

	if missing.Path != path {
		t.Errorf("the fault names %q, want %q", missing.Path, path)
	}

	// The message has to say what the file is for, because an adopter meets it
	// by running cpybkc somewhere a project is not.
	if got := diag.Render(err); !strings.Contains(got, path) || !strings.Contains(got, "generators to run") {
		t.Errorf("the fault reads:\n%s\nwant it to name %s and say what a manifest is for", got, path)
	}
}

// TestReadFileKeepsAnUnreadableManifestDistinctFromAMissingOne is the other half
// of the file being unopenable: a directory where the manifest should be is not
// a project without one, and reporting it as absent would send an adopter to
// write a file that is in their way.
func TestReadFileKeepsAnUnreadableManifestDistinctFromAMissingOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), Name)
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("make a directory where the manifest goes: %v", err)
	}

	_, err := ReadFile(path)
	if err == nil {
		t.Fatal("reading a directory as a manifest succeeded, want a fault")
	}

	var missing *NotFoundError
	if errors.As(err, &missing) {
		t.Errorf("reading a directory gave %v, want something other than a *NotFoundError", err)
	}

	if !strings.Contains(err.Error(), path) {
		t.Errorf("the fault reads %q, want it to name %s", err, path)
	}
}
