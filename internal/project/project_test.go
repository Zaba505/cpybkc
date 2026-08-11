// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package project_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/conformance"
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/emit"
	"github.com/Zaba505/cpybkc/internal/manifest"
	"github.com/Zaba505/cpybkc/internal/plugin"
	"github.com/Zaba505/cpybkc/internal/project"
	"github.com/Zaba505/cpybkc/irpb"
)

// root is this repository's root, from the directory a test in this package
// runs in.
const root = "../.."

// manifestFor writes a manifest naming layout, in a directory of its own, and
// hands back the path to it.
//
// The manifest is written outside the tree it names on purpose: a manifest's
// paths are relative to the manifest and a layout's are relative to the layout,
// so a manifest in one directory naming a layout in another is the arrangement
// that would break if either half were resolved against the wrong base.
func manifestFor(t *testing.T, layout string, generators string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, manifest.Name)

	layout, err := filepath.Abs(layout)
	if err != nil {
		t.Fatalf("resolving %s: %v", layout, err)
	}

	body, err := json.Marshal(map[string]any{"layout": layout})
	if err != nil {
		t.Fatalf("building the manifest: %v", err)
	}

	// A manifest carries at least one generator, because one carrying none asks
	// for nothing to happen. Which generator it names does not matter to
	// resolving a descriptor — nothing is looked for on PATH until
	// [project.Run.Generators] is called — so a test about the layout states the
	// one it will not run.
	if generators == "" {
		generators = `[{"name": "go", "out": "gen"}]`
	}

	source := strings.TrimSuffix(string(body), "}") + `,"generators":` + generators + "}"

	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	return path
}

// TestEveryCorpusEntryResolves runs the whole chain over every layout and
// copybook this repository has committed.
//
// The conformance corpus is twenty-eight real layouts — every framing, every
// encoding axis, packed and zoned and binary and floating-point items, tables,
// variants and multi-record files — each with the copybooks it binds. Nothing
// had ever resolved one before this package existed, because resolving a
// copybook needs the pipeline the CLI carries; running the corpus through it is
// the widest input this composition has, and every entry has to come out with a
// descriptor rather than a diagnostic.
//
// What is *not* asserted here is equality with the `ir.json` each entry
// commits, and the reason is a discrepancy this story found rather than one it
// caused. Those files were written by hand from a specification section, and
// their sequencing automata are minimal — `(* ORDER)` is one accepting state
// with a transition to itself — while `resolve.CompileSequence` builds a
// position automaton, which has a start state besides. Both accept the same
// files and docs/ir/SPEC.md requires no particular one, but the extra state
// shifts every identifier after it, so the two descriptors differ in the
// automaton and in node numbering and nowhere else: with the state, transition
// and file nodes set aside, all twenty-eight entries resolve to exactly the
// records, groups, fields and predicates they commit. Settling which shape the
// corpus should carry belongs to whoever owns the corpus (#68) or the compiler
// (#36), not to the story that composes them.
func TestEveryCorpusEntryResolves(t *testing.T) {
	t.Parallel()

	entries, err := conformance.Load(conformance.CorpusPath(root))
	if err != nil {
		t.Fatalf("loading the corpus: %v", err)
	}

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			t.Parallel()

			run, err := project.Load(manifestFor(t, entry.Layout, ""))
			if err != nil {
				t.Fatalf("%s does not resolve:\n%s", entry.Name, diag.Render(err))
			}

			if run.Descriptor.GetVersion() != entry.Descriptor.GetVersion() {
				t.Errorf("%s resolves at IR version %s, want %s",
					entry.Name, run.Descriptor.GetVersion(), entry.Descriptor.GetVersion())
			}

			if got, want := records(run.Descriptor), records(entry.Descriptor); got != want {
				t.Errorf("%s resolves to %d record types, want the %d it commits", entry.Name, got, want)
			}
		})
	}
}

// TestARunResolvesOneDescriptorAndItIsTheSameBytesTwice is
// docs/cli/SPEC.md's "Which descriptor is emitted", asserted rather than
// claimed: a run resolves one descriptor, and the bytes every generator of that
// run is handed are that one descriptor's.
//
// Resolving the same project twice has to produce the same bytes for the run to
// be a function of its inputs at all (#47), and it is the same equality
// `--emit-ir` rests on: there is one encoder, so a descriptor that encodes the
// same twice is one that reaches two generators the same.
func TestARunResolvesOneDescriptorAndItIsTheSameBytesTwice(t *testing.T) {
	t.Parallel()

	path := manifestFor(t, filepath.Join(root, "testdata/conformance/orders-fixed/layout.sexpr"), "")

	first, err := project.Load(path)
	if err != nil {
		t.Fatalf("the project does not resolve:\n%s", diag.Render(err))
	}

	second, err := project.Load(path)
	if err != nil {
		t.Fatalf("the project does not resolve the second time:\n%s", diag.Render(err))
	}

	one, err := emit.Marshal(first.Descriptor)
	if err != nil {
		t.Fatalf("encoding the descriptor: %v", err)
	}

	two, err := emit.Marshal(second.Descriptor)
	if err != nil {
		t.Fatalf("encoding the descriptor: %v", err)
	}

	if string(one) != string(two) {
		t.Errorf("two runs over one project encode to %d and %d bytes, and a run is a function of its inputs",
			len(one), len(two))
	}
}

// TestAPathIsRelativeToTheFileThatStatesIt is docs/cli/SPEC.md's rule, tested
// where it can actually go wrong: the manifest, the layout and the copybooks
// are each in a different directory, so a copybook resolved against the
// manifest instead of the layout finds nothing.
func TestAPathIsRelativeToTheFileThatStatesIt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(dir, "layouts", "cpy"), 0o755); err != nil {
		t.Fatalf("building the project: %v", err)
	}

	write(t, filepath.Join(dir, "layouts", "cpy", "orders.cpy"), fixed(
		"01  ORDER-REC.",
		"    05  ORDER-ID    PIC X(4).",
	))

	write(t, filepath.Join(dir, "layouts", "orders.sexpr"), `(encoding
  (charset ascii) (sign-convention ascii-zone-37)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 4))
(record ORDER (copybook "cpy/orders.cpy" ORDER-REC))
(discriminate ORDER single-record-type)
(sequence (* ORDER))
`)

	write(t, filepath.Join(dir, manifest.Name), `{"layout": "layouts/orders.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	run, err := project.Load(filepath.Join(dir, manifest.Name))
	if err != nil {
		t.Fatalf("the project does not resolve:\n%s", diag.Render(err))
	}

	if got := records(run.Descriptor); got != 1 {
		t.Errorf("the descriptor carries %d record nodes, want 1", got)
	}
}

// TestACopybookThatIsNotThereNamesBothPaths is the diagnostic
// docs/cli/SPEC.md spells out: the path as the layout writes it, so the adopter
// can find it in their file, and the absolute path cpybkc opened, so they know
// where it was looked for.
func TestACopybookThatIsNotThereNamesBothPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	write(t, filepath.Join(dir, "orders.sexpr"), `(encoding
  (charset ascii) (sign-convention ascii-zone-37)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 4))
(record ORDER (copybook "cpy/orders.cpy" ORDER-REC))
(discriminate ORDER single-record-type)
(sequence (* ORDER))
`)
	write(t, filepath.Join(dir, manifest.Name), `{"layout": "orders.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	_, err := project.Load(filepath.Join(dir, manifest.Name))

	var missing *project.CopybookError
	if !errors.As(err, &missing) {
		t.Fatalf("a missing copybook reads as %v, want a CopybookError", err)
	}

	rendered := diag.Render(err)

	if !strings.Contains(rendered, `"cpy/orders.cpy"`) {
		t.Errorf("the diagnostic does not name the path the layout spells:\n%s", rendered)
	}

	if !strings.Contains(rendered, filepath.Join(dir, "cpy", "orders.cpy")) {
		t.Errorf("the diagnostic does not say where the copybook was looked for:\n%s", rendered)
	}

	// The second line names no place, because there is no file to point into:
	// the fault is that there is not one.
	if !strings.Contains(rendered, "\n  looked for ") {
		t.Errorf("the absolute path is not on a continuation line:\n%s", rendered)
	}
}

// TestALayoutNamingACopybookIsNeverAFaultForNotHavingBeenDeclared is #157: the
// manifest lists no copybooks and constrains none, so the ones a run reads are
// exactly the ones the layout's `record` forms name.
func TestALayoutNamingACopybookIsNeverAFaultForNotHavingBeenDeclared(t *testing.T) {
	t.Parallel()

	path := manifestFor(t, filepath.Join(root, "testdata/conformance/batch-fixed/layout.sexpr"), "")

	run, err := project.Load(path)
	if err != nil {
		t.Fatalf("the project does not resolve:\n%s", diag.Render(err))
	}

	// The entry's layout binds three records over three copybooks, none of
	// which the manifest above mentions at all.
	if got := records(run.Descriptor); got < 3 {
		t.Errorf("the descriptor carries %d record nodes, want the three the layout names", got)
	}

	// And the manifest has nowhere to name one: `inputs` is gone from the file,
	// so a manifest carrying one is an unknown field.
	dir := t.TempDir()
	write(t, filepath.Join(dir, manifest.Name), `{"layout": "a.sexpr", "inputs": ["a.cpy"], "generators": [{"name": "go", "out": "gen"}]}`)

	if _, err := project.Load(filepath.Join(dir, manifest.Name)); err == nil {
		t.Error("a manifest listing inputs is read without complaint, and there is no such field")
	}
}

// TestAGeneratorIsResolvedToAnExecutableOnPATH is #41 seen from the composition:
// the name in the manifest becomes the `cpybkc-gen-<name>` the search found,
// and the generator's `out` is resolved against the manifest.
func TestAGeneratorIsResolvedToAnExecutableOnPATH(t *testing.T) {
	t.Parallel()

	bin := t.TempDir()
	executable := filepath.Join(bin, plugin.Filename("go"))

	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing %s: %v", executable, err)
	}

	path := manifestFor(t,
		filepath.Join(root, "testdata/conformance/orders-fixed/layout.sexpr"),
		`[{"name": "go", "out": "gen"}]`)

	run, err := project.Load(path)
	if err != nil {
		t.Fatalf("the project does not resolve:\n%s", diag.Render(err))
	}

	generators, err := run.Generators(bin)
	if err != nil {
		t.Fatalf("resolving the generators:\n%s", diag.Render(err))
	}

	if len(generators) != 1 {
		t.Fatalf("the run has %d generators, want 1", len(generators))
	}

	if generators[0].Path != executable {
		t.Errorf("the generator resolved to %s, want %s", generators[0].Path, executable)
	}

	if want := filepath.Join(filepath.Dir(path), "gen"); generators[0].Out != want {
		t.Errorf("the generator writes into %s, want %s — `out` is relative to the manifest",
			generators[0].Out, want)
	}
}

// TestAGeneratorThatIsNotOnPATHNamesTheManifestEntry is the other half: what
// the search could not find, reported against the line the adopter has to edit.
func TestAGeneratorThatIsNotOnPATHNamesTheManifestEntry(t *testing.T) {
	t.Parallel()

	path := manifestFor(t,
		filepath.Join(root, "testdata/conformance/orders-fixed/layout.sexpr"),
		`[{"name": "nowhere", "out": "gen"}]`)

	run, err := project.Load(path)
	if err != nil {
		t.Fatalf("the project does not resolve:\n%s", diag.Render(err))
	}

	_, err = run.Generators(t.TempDir())

	var fault *project.GeneratorError
	if !errors.As(err, &fault) {
		t.Fatalf("an unresolvable generator reads as %v, want a GeneratorError", err)
	}

	var notFound *plugin.NotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("the search's own fault is not reachable through it: %v", err)
	}

	rendered := diag.Render(err)

	if !strings.Contains(rendered, plugin.Filename("nowhere")) {
		t.Errorf("the diagnostic does not name the executable that was looked for:\n%s", rendered)
	}

	if !strings.Contains(rendered, manifest.Name) {
		t.Errorf("the diagnostic does not point at the manifest entry:\n%s", rendered)
	}
}

// TestAManifestThatIsNotThereIsAFault covers the first thing a run does, and
// the failure a caller hits by running cpybkc in the wrong directory.
func TestAManifestThatIsNotThereIsAFault(t *testing.T) {
	t.Parallel()

	_, err := project.Load(filepath.Join(t.TempDir(), manifest.Name))

	var missing *manifest.NotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("a missing manifest reads as %v, want a NotFoundError", err)
	}
}

// TestALayoutThatIsNotThereNamesBothPaths is the manifest's half of the rule
// the copybook diagnostic keeps: the path as the manifest spells it, and where
// cpybkc looked for it.
func TestALayoutThatIsNotThereNamesBothPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write(t, filepath.Join(dir, manifest.Name), `{"layout": "layouts/orders.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	_, err := project.Load(filepath.Join(dir, manifest.Name))

	var missing *project.MissingLayoutError
	if !errors.As(err, &missing) {
		t.Fatalf("a missing layout reads as %v, want a MissingLayoutError", err)
	}

	rendered := diag.Render(err)

	if !strings.Contains(rendered, `"layouts/orders.sexpr"`) {
		t.Errorf("the diagnostic does not name the path the manifest spells:\n%s", rendered)
	}

	if !strings.Contains(rendered, filepath.Join(dir, "layouts", "orders.sexpr")) {
		t.Errorf("the diagnostic does not say where the layout was looked for:\n%s", rendered)
	}
}

// TestEveryFaultInOnePassIsReported is docs/cli/SPEC.md's rule about more than
// one fault: a generated layout is generated wrong in the same way in many
// places at once, and a reader that reports the first is run once per fault.
func TestEveryFaultInOnePassIsReported(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	write(t, filepath.Join(dir, "orders.sexpr"), `(encoding
  (charset nonsense) (sign-convention ascii-zone-37)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 4))
(record ORDER (copybook "cpy/orders.cpy" ORDER-REC))
(record ORDER (copybook "cpy/orders.cpy" ORDER-REC))
(discriminate ORDER single-record-type)
(sequence (* ORDER))
`)
	write(t, filepath.Join(dir, manifest.Name), `{"layout": "orders.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	_, err := project.Load(filepath.Join(dir, manifest.Name))
	if err == nil {
		t.Fatal("a layout wrong in two layers resolves without complaint")
	}

	if got := len(diag.Diagnostics(err)); got < 2 {
		t.Errorf("a layout wrong in two layers reports %d faults, want both:\n%s", got, diag.Render(err))
	}
}

// TestARecordLevelRedefineIsRefusedRatherThanGuessedAt is the discrepancy this
// composition found and did not paper over.
//
// `resolve` reads one `01`-level holding a REDEFINES outside a repeating group
// as one record type per alternative, and nothing in the layout format names
// them apart or says which alternative a `record` form meant. Pairing them by
// position would be a rule an adopter could not read anywhere, so the layout is
// refused with a diagnostic naming #164 — the story that decides it.
func TestARecordLevelRedefineIsRefusedRatherThanGuessedAt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	write(t, filepath.Join(dir, "txn.cpy"), fixed(
		"01  TXN-REC.",
		"    05  TXN-ACCT       PIC X(2).",
		"    05  TXN-PURCHASE.",
		"        10  PUR-CODE   PIC X(2).",
		"    05  TXN-REFUND REDEFINES TXN-PURCHASE.",
		"        10  REF-CODE   PIC X(2).",
	))

	write(t, filepath.Join(dir, "txn.sexpr"), `(encoding
  (charset ascii) (sign-convention ascii-zone-37)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 4))
(record PURCHASE (copybook "txn.cpy" TXN-REC))
(discriminate PURCHASE (equals (item PURCHASE TXN-PURCHASE PUR-CODE) "PU"))
(sequence (* PURCHASE))
`)
	write(t, filepath.Join(dir, manifest.Name), `{"layout": "txn.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	_, err := project.Load(filepath.Join(dir, manifest.Name))

	var alternatives *project.AlternativesError
	if !errors.As(err, &alternatives) {
		t.Fatalf("a record-level redefine reads as %v, want an AlternativesError", err)
	}

	if !strings.Contains(diag.Render(err), "#164") {
		t.Errorf("the diagnostic does not name the story that decides it:\n%s", diag.Render(err))
	}
}

// records is how many record nodes a descriptor carries, which is one per
// record type the layout defines.
func records(d *irpb.Descriptor) int {
	found := 0

	for _, node := range d.GetNodes() {
		if _, ok := node.GetKind().(*irpb.Node_Record); ok {
			found++
		}
	}

	return found
}

// write puts a file where a test needs one, making the directory above it.
func write(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("making %s: %v", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// fixed renders copybook lines the way a copybook is written: fixed-format
// COBOL, with the six-column sequence area in front of every line.
//
// It is here rather than in the sources themselves so that a test's copybook
// reads as the record it describes, and so that the format this build assumes is
// stated in one place a reader can find.
func fixed(lines ...string) string {
	var b strings.Builder

	for _, line := range lines {
		b.WriteString("      " + line + "\n")
	}

	return b.String()
}

// TestAnItemReferenceThatNamesNothingCarriesTwoSpans is the cross-file
// diagnostic docs/layout/SPEC.md requires: one span into the layout at the
// reference, and one into the copybook at the group that was supposed to hold
// the item, carrying what that group does declare.
func TestAnItemReferenceThatNamesNothingCarriesTwoSpans(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	write(t, filepath.Join(dir, "orders.cpy"), fixed(
		"01  ORDER-REC.",
		"    05  ORDER-KEY.",
		"        10  O-NUMBER   PIC X(4).",
	))

	write(t, filepath.Join(dir, "orders.sexpr"), `(encoding
  (charset ascii) (sign-convention ascii-zone-37)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 4))
(record ORDER (copybook "orders.cpy" ORDER-REC))
(rename (item ORDER ORDER-KEY O-NO) "OrderNumber")
(discriminate ORDER single-record-type)
(sequence (* ORDER))
`)
	write(t, filepath.Join(dir, manifest.Name),
		`{"layout": "orders.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	_, err := project.Load(filepath.Join(dir, manifest.Name))

	var unknown *project.UnknownItemError
	if !errors.As(err, &unknown) {
		t.Fatalf("a reference naming nothing reads as %v, want an UnknownItemError", err)
	}

	rendered := diag.Render(err)

	if !strings.Contains(rendered, "orders.sexpr:") {
		t.Errorf("the diagnostic does not point into the layout:\n%s", rendered)
	}

	// The second span points at what is there, because an absent item has no
	// position and the list an adopter needs is the one they could have meant.
	if !strings.Contains(rendered, "O-NUMBER") {
		t.Errorf("the diagnostic does not say what the group declares:\n%s", rendered)
	}
}

// TestTwoRecordsOverOneCopybookAreRenamedIndependently is the reason a copybook
// is assembled into one item tree per `record` form.
//
// docs/layout/SPEC.md requires two records over one `01`-level to be renamed,
// discriminated and encoding-overridden independently, and `assemble` keys its
// renames on the copybook field. So a shared tree would make a rename written
// for one record reach the other, and the two record nodes would come out with
// the same substituted names.
func TestTwoRecordsOverOneCopybookAreRenamedIndependently(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	write(t, filepath.Join(dir, "orders.cpy"), fixed(
		"01  ORDER-REC.",
		"    05  O-NO       PIC X(4).",
	))

	write(t, filepath.Join(dir, "orders.sexpr"), `(encoding
  (charset ascii) (sign-convention ascii-zone-37)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 4))
(record ORDER-OPEN  (copybook "orders.cpy" ORDER-REC))
(record ORDER-CLOSE (copybook "orders.cpy" ORDER-REC))
(rename (item ORDER-OPEN  O-NO) "OpeningOrderNumber")
(rename (item ORDER-CLOSE O-NO) "ClosingOrderNumber")
(discriminate ORDER-OPEN  single-record-type)
(discriminate ORDER-CLOSE single-record-type)
(sequence (seq ORDER-OPEN ORDER-CLOSE))
`)
	write(t, filepath.Join(dir, manifest.Name),
		`{"layout": "orders.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	run, err := project.Load(filepath.Join(dir, manifest.Name))
	if err != nil {
		t.Fatalf("the project does not resolve:\n%s", diag.Render(err))
	}

	substitutes := map[string]bool{}

	for _, node := range run.Descriptor.GetNodes() {
		field, ok := node.GetKind().(*irpb.Node_Field)
		if !ok {
			continue
		}

		if substitute := field.Field.GetNames().GetOverrideName(); substitute != "" {
			substitutes[substitute] = true
		}
	}

	for _, want := range []string{"OpeningOrderNumber", "ClosingOrderNumber"} {
		if !substitutes[want] {
			t.Errorf("the descriptor carries no field named %s; it carries %v", want, substitutes)
		}
	}

	if len(substitutes) != 2 {
		t.Errorf("the descriptor carries %d substituted names, want one per record: %v",
			len(substitutes), substitutes)
	}
}

// TestOneMissingCopybookIsReportedAgainstEveryRecordThatNamesIt is the rule
// this repository collects faults for, applied to a file rather than to a form.
//
// The file is read once — the answer is the same whichever record asked — but
// two records bound to it are two lines of the layout an adopter has to look at,
// so both are reported. Reporting only the first would have them fix one line,
// run again, and be told about the next.
func TestOneMissingCopybookIsReportedAgainstEveryRecordThatNamesIt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	write(t, filepath.Join(dir, "orders.sexpr"), `(encoding
  (charset ascii) (sign-convention ascii-zone-37)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 4))
(record ORDER-OPEN  (copybook "cpy/orders.cpy" ORDER-REC))
(record ORDER-CLOSE (copybook "cpy/orders.cpy" ORDER-REC))
(discriminate ORDER-OPEN  single-record-type)
(discriminate ORDER-CLOSE single-record-type)
(sequence (seq ORDER-OPEN ORDER-CLOSE))
`)
	write(t, filepath.Join(dir, manifest.Name),
		`{"layout": "orders.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	_, err := project.Load(filepath.Join(dir, manifest.Name))

	found := diag.Diagnostics(err)
	if len(found) != 2 {
		t.Fatalf("two records bound to one missing copybook report %d faults, want 2:\n%s",
			len(found), diag.Render(err))
	}

	// Each points at its own `copybook` child, which is the line that record's
	// reader has to edit.
	if found[0].Spans[0] == found[1].Spans[0] {
		t.Errorf("both faults point at %s, want one per record", found[0].Spans[0])
	}
}
