// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package project_test

import (
	"path/filepath"
	"testing"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/manifest"
	"github.com/Zaba505/cpybkc/internal/project"
)

// TestAnItemNamedNilResolvesThroughAWord is #404 end to end. `cobol-go` reads an
// item named `nil` and `sexpr-go` reads `nil` as its empty value, so no layout
// could name one until every position naming an item took `(word "nil")`. Both
// halves of that resolve here through the whole chain: a record bound to an
// 01-level named `nil`, and a subordinate item named `nil` referenced by a
// discriminator — the schema check, the layout reader and `resolve` each have
// to agree that the word is the name.
func TestAnItemNamedNilResolvesThroughAWord(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		copybook string
		layout   string
		record   string
		field    string
	}{
		{
			name:     "an 01-level named nil, bound by its top-level name",
			copybook: fixed("01  nil.", "    05  N-ID    PIC X(4)."),
			layout: `(encoding
  (charset cp037) (sign-convention ebcdic)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 4))
(record R (copybook "x.cpy" (word "nil")))
(discriminate R single-record-type)
(sequence (* R))
`,
			record: "nil",
			field:  "N-ID",
		},
		{
			name:     "a subordinate item named nil, referenced by its path",
			copybook: fixed("01  R-REC.", "    05  nil     PIC X.", "    05  R-ID    PIC X(3)."),
			layout: `(encoding
  (charset cp037) (sign-convention ebcdic)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 4))
(record R (copybook "x.cpy" R-REC))
(discriminate R (equals (item R (word "nil")) "A"))
(sequence (* R))
`,
			record: "R-REC",
			field:  "nil",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()

			write(t, filepath.Join(dir, "x.cpy"), testCase.copybook)
			write(t, filepath.Join(dir, "x.sexpr"), testCase.layout)
			write(t, filepath.Join(dir, manifest.Name), `{"layout": "x.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

			run, err := project.Load(filepath.Join(dir, manifest.Name))
			if err != nil {
				t.Fatalf("the project does not resolve:\n%s", diag.Render(err))
			}

			var named []string

			for _, node := range run.Descriptor.GetNodes() {
				if record := node.GetRecord(); record != nil {
					named = append(named, record.GetNames().GetOriginal())
				}
			}

			if len(named) != 1 || named[0] != testCase.record {
				t.Errorf("the descriptor's records are %q, want the one record %q", named, testCase.record)
			}

			if got := fieldNames(run.Descriptor)[testCase.field]; got != 1 {
				t.Errorf("the descriptor carries %d fields named %q, want 1", got, testCase.field)
			}
		})
	}
}
