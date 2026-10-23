// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutschema

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/schema"
)

// TestARunChecksAgainstTheSchemaAReleasePublishes holds the embedded schema to
// the file it was embedded from.
//
// The release publishes schema/layout.sexpr byte for byte as
// layout-schema.sexpr, and a run checks a layout against [Published]. Those are
// one contract only while the two are the same bytes, which is what this pins:
// a run checking against anything else would refuse a layout an adopter's
// validator accepts, or the reverse.
func TestARunChecksAgainstTheSchemaAReleasePublishes(t *testing.T) {
	want, err := os.ReadFile(SchemaPath(repoRoot(t)))
	if err != nil {
		t.Fatalf("read the published schema: %v", err)
	}

	if got := schema.Layout(); got != string(want) {
		t.Errorf("the embedded schema is not the bytes of %s", SchemaFile)
	}

	published, err := Published()
	if err != nil {
		t.Fatalf("load the embedded schema: %v", err)
	}

	again, err := Published()
	if err != nil {
		t.Fatalf("load the embedded schema a second time: %v", err)
	}

	if published != again {
		t.Error("the embedded schema is loaded once per call rather than once per process")
	}

	if got, want := published.Version(), publishedSchema(t).Version(); got != want {
		t.Errorf("the embedded schema states version %d, and the file states %d", got, want)
	}
}

// TestASchemaDiagnosticIsAnError is what lets a run report the schema's faults
// through the joined error every layer reader reports through: each one is
// assertable, every one is reached by diag's walk, and each renders in the
// schema's own words.
func TestASchemaDiagnosticIsAnError(t *testing.T) {
	diagnostics, err := publishedSchema(t).Validate("orders.sexpr", strings.NewReader(`(encoding
  (charset cp037) (sign-convention ebcdic)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 8) (lrec 8))
(record ORDER (copybook "orders.cpy" ORDER-REC))
(discriminate ORDER single-record-type)
(renme ORDER "Order")
(sequence (* ORDER))
`))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	if len(diagnostics) != 2 {
		t.Fatalf("got %d diagnostics, want the two the layout carries: %v", len(diagnostics), diagnostics)
	}

	joined := Err(diagnostics)

	var first Diagnostic
	if !errors.As(joined, &first) {
		t.Fatalf("no Diagnostic in %v", joined)
	}

	found := diag.Diagnostics(joined)
	if len(found) != len(diagnostics) {
		t.Fatalf("diag walks %d faults out of the joined error, want %d", len(found), len(diagnostics))
	}

	for i, d := range diagnostics {
		if got, want := found[i].String(), d.String(); got != want {
			t.Errorf("fault %d renders as %q through diag, want the schema's %q", i, got, want)
		}

		if got, want := d.Error(), d.String(); got != want {
			t.Errorf("fault %d's error text is %q, want %q", i, got, want)
		}
	}

	if Err(nil) != nil {
		t.Error("no diagnostics joined into a non-nil error")
	}
}
