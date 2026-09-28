// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package goadapter

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestRecordTypesAreTheTypesARecordIsDecodedInto holds the one read this
// adapter makes of generated source: the types a caller's own codec.Reader can
// be handed to, and nothing else the package declares.
//
// A method of that name on a value receiver, on an unexported type, or in a
// test file is not a record decoder a codec program can reach, and a type
// listed twice would be asked twice.
func TestRecordTypesAreTheTypesARecordIsDecodedInto(t *testing.T) {
	dir := t.TempDir()

	files := map[string]string{
		"records.go": `package corpus

type Header struct{}
type Detail struct{}
type trailer struct{}
type Value struct{}
`,
		"codec.go": `package corpus

func (p *Header) UnmarshalCOBOL(r any) error { return nil }
func (p *Detail) UnmarshalCOBOL(r any) error { return nil }
func (p *Header) MarshalCOBOL(w any) error   { return nil }
func (p *trailer) UnmarshalCOBOL(r any) error { return nil }
func (v Value) UnmarshalCOBOL(r any) error    { return nil }
func UnmarshalCOBOL(r any) error              { return nil }
`,
		"codec_test.go": `package corpus

type Fixture struct{}

func (p *Fixture) UnmarshalCOBOL(r any) error { return nil }
`,
	}

	for name, source := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o600); err != nil {
			t.Fatalf("%v", err)
		}
	}

	got, err := recordTypes(dir)
	if err != nil {
		t.Fatalf("%v", err)
	}

	if want := []string{"Header", "Detail"}; !slices.Equal(got, want) {
		t.Errorf("the record types are %v, and the package decodes records into %v", got, want)
	}
}
