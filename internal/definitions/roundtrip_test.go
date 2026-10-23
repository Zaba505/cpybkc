// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package definitions_test

import (
	"reflect"
	"strings"
	"testing"

	cobol "github.com/Zaba505/cobol-go"

	"github.com/Zaba505/cpybkc/internal/definitions"
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
)

// TestEveryCopybookRoundTrips is #394's promise that every copybook cpybkc can
// read converts, not a subset. Every copybook the repository holds, and the
// kitchen sink, is parsed in fixed format, written, checked against the
// published schema, read, and built and judged, once as a definitions file and
// once as a block's body. What comes back is what `cobol-go` parsed, with
// positions and comments cleared and an omitted name counted as FILLER — the
// one difference docs/layout/SPEC.md's "What a transcription leaves out"
// declares unobservable.
func TestEveryCopybookRoundTrips(t *testing.T) {
	t.Parallel()

	for _, fixture := range fixtures(t) {
		t.Run(fixture.Name, func(t *testing.T) {
			t.Parallel()

			want := make([]*cobol.DataDescriptionEntry, 0, len(fixture.Entries))
			for _, entry := range fixture.Entries {
				want = append(want, transcribable(entry))
			}

			t.Run("file", func(t *testing.T) {
				t.Parallel()

				body := write(t, fixture.Entries, definitions.FileDepth)
				same(t, judged(t, fileBody(t, body)), want, body)
			})

			t.Run("block", func(t *testing.T) {
				t.Parallel()

				body := write(t, fixture.Entries, definitions.BlockDepth)
				same(t, judged(t, blockBody(t, body)), want, body)
			})
		})
	}
}

// transcribable is what a transcription of entry reads back as: entry with
// positions and comments cleared, and an omitted name at a level other than 66
// and 88 counted as FILLER.
func transcribable(entry *cobol.DataDescriptionEntry) *cobol.DataDescriptionEntry {
	cleared := definitions.Cleared(entry)

	if cleared.Name == nil && cleared.Level != 66 && cleared.Level != 88 {
		cleared.Filler = true
	}

	return cleared
}

// judged reads a body, and builds and judges its entries.
func judged(t *testing.T, body []layout.Node) []*cobol.DataDescriptionEntry {
	t.Helper()

	entries, err := definitions.Read(body)
	if err != nil {
		t.Fatalf("read:\n%s", diag.Render(err))
	}

	built, err := definitions.Judge(entries)
	if err != nil {
		t.Fatalf("judge:\n%s", diag.Render(err))
	}

	return built
}

// same requires the built entries to be the parsed ones.
func same(t *testing.T, built, want []*cobol.DataDescriptionEntry, body string) {
	t.Helper()

	if len(built) != len(want) {
		t.Fatalf("built %d entries from\n%s\nwant %d", len(built), body, len(want))
	}

	lines := strings.Split(body, "\n")

	for i := range want {
		if got := definitions.Cleared(built[i]); !reflect.DeepEqual(got, want[i]) {
			t.Errorf("entry %d, %s, comes back as\n%#v\nwant\n%#v", i+1, strings.TrimSpace(lines[i]), got, want[i])
		}
	}
}
