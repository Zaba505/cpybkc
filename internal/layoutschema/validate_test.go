// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutschema

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/layoutdoc"
)

// TestValidLayoutsValidate is half of the acceptance criterion the corpus
// exists for. Every layout under testdata/valid is one the schema admits, and a
// diagnostic on any of them is the schema being wrong about the format rather
// than the layout being wrong about the schema.
//
// Between them the three framings cover every top-level form, every closed
// value set, every discrimination strategy, every sequencing operator and all
// three literal spellings — which is what makes "the schema covers every layer"
// a claim this suite tests rather than one it asserts.
//
// The fourth file is not a fourth framing. It carries the spellings SPEC.md's
// "Many records may name one copybook, and two may name one item" admits: two
// records over one `01`-level, and a record bound to a copybook that composes a
// shared header with a body. It fails if the schema ever grows a uniqueness
// rule over `copybook` that the document does not state.
//
// Nor is the fifth. It carries the second scope a discriminator is written in —
// SPEC.md's "A discriminator for a redefine inside a table" — which is the one
// top-level form the three framings do not reach: two variants in one record,
// arms selected by both predicates, and a target inside the repeating group the
// arm is being chosen for, which is a position a record-level discriminator may
// not name.
func TestValidLayoutsValidate(t *testing.T) {
	schema := publishedSchema(t)

	for _, name := range layouts(t, "valid") {
		t.Run(name, func(t *testing.T) {
			diagnostics, err := schema.Validate(name, bytes.NewReader(layout(t, "valid", name)))
			if err != nil {
				t.Fatalf("validate: %v", err)
			}

			for _, diagnostic := range diagnostics {
				t.Errorf("unexpected diagnostic: %s", diagnostic)
			}
		})
	}
}

// TestInvalidLayoutsAreRejected is the other half, and the more load-bearing
// one: a validator that accepts everything passes the test above.
//
// Each file breaks exactly one rule the schema states, and carries the
// diagnostic it expects on its first line as `;; want: <text>`. The expectation
// travels with the example rather than sitting in a table here, so a rule and
// the message it produces are read together.
func TestInvalidLayoutsAreRejected(t *testing.T) {
	schema := publishedSchema(t)

	for _, name := range layouts(t, "invalid") {
		t.Run(name, func(t *testing.T) {
			source := layout(t, "invalid", name)

			want, ok := strings.CutPrefix(strings.SplitN(string(source), "\n", 2)[0], ";; want: ")
			if !ok {
				t.Fatalf("%s carries no `;; want:` line saying which diagnostic it expects", name)
			}

			diagnostics, err := schema.Validate(name, bytes.NewReader(source))
			if err != nil {
				t.Fatalf("validate: %v", err)
			}

			if len(diagnostics) == 0 {
				t.Fatalf("the schema accepted %s, which should have failed on %q", name, want)
			}

			for _, diagnostic := range diagnostics {
				if strings.Contains(diagnostic.Message, want) {
					return
				}
			}

			t.Errorf("no diagnostic on %s contains %q; got:\n%s", name, want, strings.Join(messages(diagnostics), "\n"))
		})
	}
}

// TestTheSpecsWorkedExampleValidates is the staleness gate over the format
// itself.
//
// docs/layout/SPEC.md's "A layout, end to end" appendix is the layout the
// document shows an adopter, and it is one of the two layouts in this
// repository nobody wrote to satisfy the schema. Reading it out of the document rather than
// copying it here is the point: a change to the notation that the appendix
// followed and the schema did not would otherwise be invisible until an adopter
// pasted the example into a file.
func TestTheSpecsWorkedExampleValidates(t *testing.T) {
	example := specExample(t, layoutdoc.NativeExample)

	diagnostics, err := publishedSchema(t).Validate("SPEC.md", strings.NewReader(example))
	if err != nil {
		t.Fatalf("validate the SPEC's example: %v", err)
	}

	for _, diagnostic := range diagnostics {
		t.Errorf("the schema rejects SPEC.md's own worked example: %s", diagnostic)
	}
}

// TestTheSpecsConvertedExampleValidates is the same gate over the second layout
// the document shows an adopter.
//
// A second appendix and so a second heading, for the reason
// [github.com/Zaba505/cpybkc/internal/layoutdoc] gives. What the schema earns
// over the native example is the forms that example has no reason to write: two
// overrides on one layout, `none` on the charset axis of one of them, and a
// `?` where the native example writes a `when`.
func TestTheSpecsConvertedExampleValidates(t *testing.T) {
	example := specExample(t, layoutdoc.ConvertedExample)

	diagnostics, err := publishedSchema(t).Validate("SPEC.md", strings.NewReader(example))
	if err != nil {
		t.Fatalf("validate the SPEC's converted example: %v", err)
	}

	for _, diagnostic := range diagnostics {
		t.Errorf("the schema rejects SPEC.md's own converted example: %s", diagnostic)
	}
}

// TestADiagnosticPointsAtTheSubFormThatIsWrong asserts the property SPEC.md
// requires of every diagnostic this project emits: the span is the sub-form
// that is wrong, not the top-level form containing it. A validator reporting
// the enclosing form is one that sends a reader to the top of a page to find a
// fault three lines down.
func TestADiagnosticPointsAtTheSubFormThatIsWrong(t *testing.T) {
	const source = `(encoding
  (charset cp037)
  (sign-convention ebcdic)
  (byte-order sideways)
  (float-format hfp))
`

	diagnostics, err := publishedSchema(t).Validate("orders.sexpr", strings.NewReader(source))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	found := false

	for _, diagnostic := range diagnostics {
		if !strings.Contains(diagnostic.Message, "byte-order") {
			continue
		}

		found = true

		if diagnostic.Span.Line != 4 {
			t.Errorf("the diagnostic points at line %d, want line 4 — the sub-form that is wrong", diagnostic.Span.Line)
		}
	}

	if !found {
		t.Errorf("no diagnostic about the byte-order axis; got:\n%s", strings.Join(messages(diagnostics), "\n"))
	}
}

// TestADiagnosticNamesTheFileItIsIn is the other half of the span SPEC.md
// requires. A line and a column say where in a file, and an adopter checking a
// layout against a copybook is holding two of them — so a diagnostic that
// cannot say which file it is in stops being usable exactly when it matters.
//
// The name is the caller's, which is why it is a parameter rather than
// something this package invents: a layout validated out of a stream has
// whatever name the caller knows it by.
func TestADiagnosticNamesTheFileItIsIn(t *testing.T) {
	const source = `(encoding
  (charset cp037)
  (sign-convention ebcdic)
  (byte-order sideways)
  (float-format hfp))
`

	diagnostics, err := publishedSchema(t).Validate("orders.sexpr", strings.NewReader(source))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	if len(diagnostics) == 0 {
		t.Fatal("the schema accepted an axis value outside the closed set")
	}

	for _, diagnostic := range diagnostics {
		if diagnostic.Span.File != "orders.sexpr" {
			t.Errorf("a diagnostic is in file %q, want the name the layout was checked under", diagnostic.Span.File)
		}

		if !strings.HasPrefix(diagnostic.String(), "orders.sexpr:") {
			t.Errorf("a diagnostic renders as %q, which does not open with the file", diagnostic)
		}
	}
}

// TestValidateReportsEveryFaultRatherThanTheFirst is why Check returns a slice.
// A generated layout is generated wrong in the same way in many places at once,
// and a validator reporting one fault per run is a validator run once per fault.
func TestValidateReportsEveryFaultRatherThanTheFirst(t *testing.T) {
	const source = `(encoding
  (charset cp037)
  (sign-convention sideways)
  (byte-order upwards)
  (float-format hfp))
`

	diagnostics, err := publishedSchema(t).Validate("orders.sexpr", strings.NewReader(source))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	var axes int

	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, "sign-convention") || strings.Contains(diagnostic.Message, "byte-order") {
			axes++
		}
	}

	if axes != 2 {
		t.Errorf("reported %d of the two bad axes; got:\n%s", axes, strings.Join(messages(diagnostics), "\n"))
	}
}

// TestValidateFailsOnSourceThatIsNotSExpressions keeps a parse failure an error
// rather than a diagnostic. It comes from the grammar this format delegates to
// and leaves no layout to check, so reporting it as one finding among a list
// would claim a coverage the call does not have.
func TestValidateFailsOnSourceThatIsNotSExpressions(t *testing.T) {
	if _, err := publishedSchema(t).Validate("orders.sexpr", strings.NewReader("(encoding")); err == nil {
		t.Error("Validate accepted source the grammar cannot parse")
	}
}

// specExample returns the worked example under heading in docs/layout/SPEC.md.
func specExample(t *testing.T, heading string) string {
	t.Helper()

	example, err := layoutdoc.Example(heading)
	if err != nil {
		t.Fatalf("read the layout SPEC's worked example: %v", err)
	}

	return example
}

// layouts returns the names of every layout in one of the corpus directories.
func layouts(t *testing.T, kind string) []string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join("testdata", kind))
	if err != nil {
		t.Fatalf("read testdata/%s: %v", kind, err)
	}

	var names []string

	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}

	if len(names) == 0 {
		t.Fatalf("testdata/%s is empty: the check would pass vacuously", kind)
	}

	return names
}

// layout reads one layout out of the corpus.
func layout(t *testing.T, kind, name string) []byte {
	t.Helper()

	b, err := os.ReadFile(filepath.Join("testdata", kind, name))
	if err != nil {
		t.Fatalf("read testdata/%s/%s: %v", kind, name, err)
	}

	return b
}

// messages renders a run of diagnostics for a failure message.
func messages(diagnostics []Diagnostic) []string {
	out := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		out = append(out, "  "+diagnostic.String())
	}

	return out
}
