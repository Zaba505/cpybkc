// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutschema

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestThePublishedSchemaLoads is the floor under everything else here: the file
// this repository publishes is one this package can read. It is separate from
// the tests below so that a schema which fails to load reports that rather than
// reporting whichever coverage check happened to run first.
func TestThePublishedSchemaLoads(t *testing.T) {
	schema := publishedSchema(t)

	if got := schema.Subject(); got != "layout" {
		t.Errorf("the schema describes %q, want %q", got, "layout")
	}

	if got := schema.Version(); got < 1 {
		t.Errorf("the schema states version %d, want at least 1", got)
	}
}

// TestThePublishedSchemaDeclaresTheTopLevelFormsTheSpecStates is the staleness
// gate over the contract.
//
// docs/layout/SPEC.md's "The top-level forms" table is the normative list of
// what a layout may write at its top level, and the schema is the machine-
// readable statement of the same thing. Two statements of one fact drift, so
// this reads the table out of the document and requires the published schema to
// agree with it — on the forms, on which layer each belongs to, and on how many
// of each a layout carries.
//
// A form added to SPEC.md and not to the schema fails here, which is the failure
// worth catching: a generator targeting the schema would emit layouts missing a
// form the document requires, and nothing else in this repository would notice.
func TestThePublishedSchemaDeclaresTheTopLevelFormsTheSpecStates(t *testing.T) {
	schema := publishedSchema(t)
	stated := specTopLevelForms(t)

	if got := schema.TopLevelForms(); !slices.Equal(sorted(got), sorted(keys(stated))) {
		t.Fatalf("the schema and SPEC.md disagree about the top-level forms:\n schema: %v\n SPEC.md: %v", sorted(got), sorted(keys(stated)))
	}

	for _, form := range schema.topLevel {
		row := stated[form.tag]

		if want := strings.ReplaceAll(form.layer, "-", " "); want != row.layer {
			t.Errorf("the schema puts %s in layer %q and SPEC.md puts it in %q", form.tag, want, row.layer)
		}

		// SPEC.md spells `discriminate`'s arity "one per `record`", which
		// relates two forms and is the layout reader's to enforce. The schema
		// states the half it can — that a layout carries at least one — so the
		// notations do not line up and the row is checked for its layer alone.
		want, ok := specArities[row.arity]
		if !ok {
			continue
		}

		if form.arity.spelling != want {
			t.Errorf("the schema gives %s arity %q and SPEC.md gives it %q", form.tag, form.arity.spelling, row.arity)
		}
	}
}

// specArities maps SPEC.md's arity notation to the schema's spelling. The
// schema cannot use SPEC.md's, because `0..1` begins like a number and is a
// malformed one, so the grammar rejects it.
var specArities = map[string]string{
	"1":    "exactly-one",
	"0..1": "at-most-one",
	"1..n": "one-or-more",
	"0..n": "zero-or-more",
}

// TestThePublishedSchemaCoversEveryLayer is the acceptance criterion the schema
// exists for: it is the contract for the whole format, not for the parts that
// were easy. SPEC.md's Scope claims five separable layers; a schema covering
// four of them is one a generator can satisfy while emitting a layout nothing
// can read.
func TestThePublishedSchemaCoversEveryLayer(t *testing.T) {
	want := []string{
		"discrimination",
		"encoding-profile",
		"physical-framing",
		"record-definitions",
		"sequencing",
	}

	if got := publishedSchema(t).Layers(); !slices.Equal(got, want) {
		t.Errorf("the schema covers %v, want %v", got, want)
	}
}

// TestLoadRefusesASchemaItCannotHold covers the ways a schema can parse and
// still be a contract with a hole in it. Every one of them publishes something
// a generator can target and this package would never check, so each is a load
// failure rather than a warning.
func TestLoadRefusesASchemaItCannotHold(t *testing.T) {
	testCases := []struct {
		name   string
		schema string
	}{
		{
			name:   "no version",
			schema: `(form encoding (in top-level) (arity exactly-one) (layer encoding-profile))`,
		},
		{
			name:   "two headers",
			schema: "(schema layout 1)\n(schema layout 2)",
		},
		{
			name:   "unknown declaration",
			schema: "(schema layout 1)\n(kind encoding)",
		},
		{
			name:   "unknown clause",
			schema: "(schema layout 1)\n(form encoding (in top-level) (arity exactly-one) (layer encoding-profile) (colour red))",
		},
		{
			name:   "unknown arity",
			schema: "(schema layout 1)\n(form encoding (in top-level) (arity three) (layer encoding-profile))",
		},
		{
			name:   "top-level form with no layer",
			schema: "(schema layout 1)\n(form encoding (in top-level) (arity exactly-one))",
		},
		{
			name:   "top-level form with no arity",
			schema: "(schema layout 1)\n(form encoding (in top-level) (layer encoding-profile))",
		},
		{
			name:   "argument of an undeclared sort",
			schema: "(schema layout 1)\n(form encoding (in top-level) (arity exactly-one) (layer encoding-profile) (argument item item-ref exactly-one))",
		},
		{
			name:   "child nothing declares",
			schema: "(schema layout 1)\n(form encoding (in top-level) (arity exactly-one) (layer encoding-profile) (child charset exactly-one))",
		},
		{
			name:   "child no form admits",
			schema: "(schema layout 1)\n(form encoding (in top-level) (arity exactly-one) (layer encoding-profile))\n(form charset (argument value symbol exactly-one))",
		},
		{
			name:   "sort listing a form that does not declare itself into it",
			schema: "(schema layout 1)\n(sort strategy (form equals))",
		},
		{
			name:   "form declaring itself into a sort that does not list it",
			schema: "(schema layout 1)\n(sort strategy (symbol single-record-type))\n(form equals (in strategy))",
		},
		{
			name:   "a sort and a child form sharing a name",
			schema: "(schema layout 1)\n(sort charset (symbol ascii))\n(form encoding (in top-level) (arity exactly-one) (layer encoding-profile) (child charset exactly-one))\n(form charset (argument value symbol exactly-one))",
		},
		{
			name:   "a repeatable argument that is not the last",
			schema: "(schema layout 1)\n(form record (in top-level) (arity exactly-one) (layer record-definitions) (argument name symbol one-or-more) (argument path text exactly-one))",
		},
		{
			name:   "a repeatable argument beside children",
			schema: "(schema layout 1)\n(form record (in top-level) (arity exactly-one) (layer record-definitions) (argument name symbol one-or-more) (child copybook exactly-one))\n(form copybook (argument path text exactly-one))",
		},
		{
			name:   "a child form stating an arity",
			schema: "(schema layout 1)\n(form encoding (in top-level) (arity exactly-one) (layer encoding-profile) (child charset exactly-one))\n(form charset (arity exactly-one) (argument value symbol exactly-one))",
		},
		{
			name:   "a reference naming an argument the form does not have",
			schema: "(schema layout 1)\n(reference record-name record label)\n(form record (in top-level) (arity exactly-one) (layer record-definitions) (argument name symbol exactly-one))",
		},
		{
			name:   "a sort declared twice",
			schema: "(schema layout 1)\n(sort literal text)\n(sort literal number)",
		},
		{
			// The one fault that would otherwise survive loading and then not
			// terminate: checking a value against a sort walks its includes.
			name:   "two sorts including each other",
			schema: "(schema layout 1)\n(sort literal match)\n(sort match literal)",
		},
		{
			name:   "a sort including itself",
			schema: "(schema layout 1)\n(sort literal literal)",
		},
		{
			name:   "a primitive redeclared",
			schema: "(schema layout 1)\n(sort symbol text)",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := Load(strings.NewReader(testCase.schema)); err == nil {
				t.Error("Load accepted a schema it should have refused")
			}
		})
	}
}

// specTopLevelForm is one row of SPEC.md's "The top-level forms" table.
type specTopLevelForm struct {
	arity string
	layer string
}

// specTopLevelForms reads that table out of the document.
//
// It parses the markdown rather than holding a copy of the list, for the reason
// the test above exists at all: a copy here would be a third statement of the
// same fact, and would go stale in exactly the way this is meant to catch.
func specTopLevelForms(t *testing.T) map[string]specTopLevelForm {
	t.Helper()

	rows := make(map[string]specTopLevelForm)

	for _, line := range strings.Split(section(t, "### The top-level forms"), "\n") {
		cells := tableRow(line)
		if len(cells) != 3 || !strings.HasPrefix(cells[0], "`") {
			continue
		}

		rows[strings.Trim(cells[0], "`")] = specTopLevelForm{arity: cells[1], layer: cells[2]}
	}

	if len(rows) == 0 {
		t.Fatal("no table rows found under SPEC.md's \"The top-level forms\": the check would pass vacuously")
	}

	return rows
}

// tableRow splits a markdown table row into its cells, and returns nothing for
// a line that is not one.
func tableRow(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
		return nil
	}

	var cells []string
	for _, cell := range strings.Split(strings.Trim(line, "|"), "|") {
		cells = append(cells, strings.TrimSpace(cell))
	}

	return cells
}

// section returns the text of SPEC.md under heading, up to the next heading at
// the same level or above.
func section(t *testing.T, heading string) string {
	t.Helper()

	level := strings.IndexFunc(heading, func(r rune) bool { return r != '#' })

	var (
		body  []string
		found bool
	)

	for _, line := range strings.Split(specText(t), "\n") {
		if line == heading {
			found = true

			continue
		}

		if !found {
			continue
		}

		if strings.HasPrefix(line, "#") && strings.IndexFunc(line, func(r rune) bool { return r != '#' }) <= level {
			break
		}

		body = append(body, line)
	}

	if !found {
		t.Fatalf("SPEC.md carries no heading %q", heading)
	}

	return strings.Join(body, "\n")
}

// specText reads docs/layout/SPEC.md.
func specText(t *testing.T) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "layout", "SPEC.md"))
	if err != nil {
		t.Fatalf("read the layout SPEC: %v", err)
	}

	return string(b)
}

// publishedSchema loads schema/layout.sexpr — the file a release publishes and
// this repository's own tests are held to, rather than a schema built here.
func publishedSchema(t *testing.T) *Schema {
	t.Helper()

	f, err := os.Open(SchemaPath(repoRoot(t)))
	if err != nil {
		t.Fatalf("open the published schema: %v", err)
	}

	defer func() {
		if err := f.Close(); err != nil {
			t.Errorf("close the published schema: %v", err)
		}
	}()

	schema, err := Load(f)
	if err != nil {
		t.Fatalf("load the published schema: %v", err)
	}

	return schema
}

// repoRoot walks up from the test's working directory to the directory holding
// go.mod, which for this module is the repository root and so the directory
// schema/ and docs/ sit in.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found above %q", dir)
		}

		dir = parent
	}
}

// keys returns a map's keys.
func keys[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}

	return names
}

// sorted returns a sorted copy, so that a comparison of two lists is about
// their members rather than about the order two sources happened to state them
// in.
func sorted(items []string) []string {
	out := slices.Clone(items)
	slices.Sort(out)

	return out
}
