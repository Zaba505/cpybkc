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
	"slices"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/conformance"
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/emit"
	"github.com/Zaba505/cpybkc/internal/manifest"
	"github.com/Zaba505/cpybkc/internal/plugin"
	"github.com/Zaba505/cpybkc/internal/project"
	"github.com/Zaba505/cpybkc/internal/resolve"
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
// as one record type per alternative, and the `alternative` children are where a
// layout says which of them a `record` form is. A form carrying none over such a
// copybook has chosen nothing, and pairing it to an alternative by position
// would be a rule an adopter could not read anywhere — so it is refused, with
// every alternative there was to choose from named in the message.
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

	// The message names what there was to choose from, because the fix is
	// writing one of them down and an adopter should not have to open the
	// copybook to find out what the choices were.
	rendered := diag.Render(err)

	for _, want := range []string{"TXN-PURCHASE", "TXN-REFUND", "alternative"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the diagnostic does not name %s:\n%s", want, rendered)
		}
	}
}

// TestARecordFormChoosesItsAlternativeByName is #164 end to end on the layout
// side: one `01`-level redefined three ways, discriminated behind a shared key,
// resolves to three record types, each of them the alternative its `record` form
// named and each carrying the name its `rename` gave it.
//
// Every half of the decision is asserted rather than described. The pairing is
// the layout's statement — swap the two lines and the two record types swap with
// them — and the naming is a rename on the record, because the `01`-level's name
// is what all three of them carry as an original (docs/ir/SPEC.md, "Names").
func TestARecordFormChoosesItsAlternativeByName(t *testing.T) {
	t.Parallel()

	run, err := project.Load(filepath.Join(redefinedTransactions(t), manifest.Name))
	if err != nil {
		t.Fatalf("the project does not resolve:\n%s", diag.Render(err))
	}

	if got := records(run.Descriptor); got != 3 {
		t.Fatalf("the descriptor carries %d record types, want one per alternative", got)
	}

	// The original is the `01`-level's on all three: every alternative is a
	// description of that level, and taking an alternative's name would leave a
	// copybook with two redefines with nothing to take.
	overrides := make([]string, 0, 3)

	for _, node := range run.Descriptor.GetNodes() {
		record := node.GetRecord()
		if record == nil {
			continue
		}

		if got := record.GetNames().GetOriginal(); got != "TXN-REC" {
			t.Errorf("a record node is named %q, want the 01-level's TXN-REC", got)
		}

		overrides = append(overrides, record.GetNames().GetOverrideName())
	}

	want := []string{"TXN-PURCHASE-REC", "TXN-REFUND-REC", "TXN-ADJUST-REC"}
	if !slices.Equal(overrides, want) {
		t.Errorf("the record nodes carry %v, want %v", overrides, want)
	}

	// The pairing reached the fields: PURCHASE holds the purchase alternative's
	// items and neither of the others', which is what says the choice selected a
	// record type rather than being carried along beside one.
	fields := fieldNames(run.Descriptor)

	for _, name := range []string{"PUR-AMT", "REF-ORIG", "ADJ-REASON"} {
		if got := fields[name]; got != 1 {
			t.Errorf("%s stands in %d record types, want exactly the one that chose it", name, got)
		}
	}
}

// TestAnAlternativeIsRootedAtTheRecordChoosingIt is the half of the choice the
// layout reader decides on its own: a reference is rooted at a record, and an
// `alternative` rooted at another record would be looked up in another record's
// item tree.
func TestAnAlternativeIsRootedAtTheRecordChoosingIt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	write(t, filepath.Join(dir, "txn.cpy"), fixed(
		"01  TXN-REC.",
		"    05  TXN-PURCHASE  PIC X(4).",
		"    05  TXN-REFUND REDEFINES TXN-PURCHASE PIC X(4).",
	))

	write(t, filepath.Join(dir, "txn.sexpr"), `(encoding
  (charset ascii) (sign-convention ascii-zone-37)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 4))
(record PURCHASE (copybook "txn.cpy" TXN-REC) (alternative (item REFUND TXN-PURCHASE)))
(record REFUND   (copybook "txn.cpy" TXN-REC) (alternative (item REFUND TXN-REFUND)))
(discriminate PURCHASE single-record-type)
(discriminate REFUND   single-record-type)
(sequence (seq PURCHASE REFUND))
`)
	write(t, filepath.Join(dir, manifest.Name), `{"layout": "txn.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	_, err := project.Load(filepath.Join(dir, manifest.Name))
	if err == nil {
		t.Fatal("the reader accepts an alternative rooted at another record")
	}

	if !strings.Contains(err.Error(), "rooted at record") {
		t.Errorf("the diagnostic does not say what is wrong with it:\n%s", diag.Render(err))
	}
}

// redefinedTransactions writes the worked shape #164 decides — one `01`-level
// redefined three ways, discriminated on a code behind a shared account key —
// and hands back the directory holding it.
//
// The same shape is written again in `cpybkc-gen-go`'s tests, which assert that
// the descriptor generates without a collision — the other half of #164, and the
// half that cannot be asserted from here, because a generator is a program this
// package runs rather than a package it imports.
func redefinedTransactions(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	write(t, filepath.Join(dir, "txn.cpy"), fixed(
		"01  TXN-REC.",
		"    05  TXN-ACCT        PIC X(4).",
		"    05  TXN-PURCHASE.",
		"        10  PUR-CODE    PIC X(2).",
		"        10  PUR-AMT     PIC X(6).",
		"    05  TXN-REFUND REDEFINES TXN-PURCHASE.",
		"        10  REF-CODE    PIC X(2).",
		"        10  REF-ORIG    PIC X(6).",
		"    05  TXN-ADJUST REDEFINES TXN-PURCHASE.",
		"        10  ADJ-CODE    PIC X(2).",
		"        10  ADJ-REASON  PIC X(6).",
	))

	write(t, filepath.Join(dir, "txn.sexpr"), `(encoding
  (charset ascii) (sign-convention ascii-zone-37)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 12))

(record PURCHASE (copybook "txn.cpy" TXN-REC) (alternative (item PURCHASE TXN-PURCHASE)))
(record REFUND   (copybook "txn.cpy" TXN-REC) (alternative (item REFUND   TXN-REFUND)))
(record ADJUST   (copybook "txn.cpy" TXN-REC) (alternative (item ADJUST   TXN-ADJUST)))

(rename PURCHASE "TXN-PURCHASE-REC")
(rename REFUND   "TXN-REFUND-REC")
(rename ADJUST   "TXN-ADJUST-REC")

(discriminate PURCHASE (equals (item PURCHASE TXN-PURCHASE PUR-CODE) "PU"))
(discriminate REFUND   (equals (item REFUND   TXN-REFUND   REF-CODE) "RF"))
(discriminate ADJUST   (equals (item ADJUST   TXN-ADJUST   ADJ-CODE) "AJ"))

(sequence (* (alt PURCHASE REFUND ADJUST)))
`)

	write(t, filepath.Join(dir, manifest.Name),
		`{"layout": "txn.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	return dir
}

// fieldNames is how many field nodes of a descriptor carry each copybook name.
func fieldNames(d *irpb.Descriptor) map[string]int {
	found := make(map[string]int)

	for _, node := range d.GetNodes() {
		if field := node.GetField(); field != nil {
			found[field.GetNames().GetOriginal()]++
		}
	}

	return found
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
// discriminated and encoding-overridden independently. `assemble` keys a rename
// on the record it was written under as well as on the copybook item (#164), so
// a shared tree would no longer make a rename reach the wrong record — but every
// item reference here would still resolve into one tree, and an
// encoding-override or a discriminator written for one record would be looked up
// against the other's items.
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

// TestTwoRedefinesAreChosenAsASetAndNotInOrder is the case the whole naming
// decision rests on, and the only one that exercises the choice as a set.
//
// Two independent redefines in one `01`-level resolve to one record type per
// *combination*, which is why a record node cannot take an alternative's name —
// there are two of them and neither names the record. So the two `alternative`
// children are a set: written in either order they choose the same record type,
// and a layout that swapped its two lines would otherwise mean something else.
func TestTwoRedefinesAreChosenAsASetAndNotInOrder(t *testing.T) {
	t.Parallel()

	// A-or-B beside C-or-D: four record types, each carrying the whole record.
	copybook := fixed(
		"01  PAIR-REC.",
		"    05  PAIR-A      PIC X(4).",
		"    05  PAIR-B REDEFINES PAIR-A PIC X(4).",
		"    05  PAIR-C      PIC X(6).",
		"    05  PAIR-D REDEFINES PAIR-C PIC X(6).",
	)

	layoutFor := func(first, second string) string {
		return `(encoding
  (charset ascii) (sign-convention ascii-zone-37)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 10))
(record PAIR (copybook "pair.cpy" PAIR-REC)
  (alternative (item PAIR ` + first + `))
  (alternative (item PAIR ` + second + `)))
(discriminate PAIR single-record-type)
(sequence (* PAIR))
`
	}

	// Both orders name one record type, and it is the one holding B and D.
	for _, order := range [][2]string{{"PAIR-B", "PAIR-D"}, {"PAIR-D", "PAIR-B"}} {
		dir := t.TempDir()

		write(t, filepath.Join(dir, "pair.cpy"), copybook)
		write(t, filepath.Join(dir, "pair.sexpr"), layoutFor(order[0], order[1]))
		write(t, filepath.Join(dir, manifest.Name),
			`{"layout": "pair.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

		run, err := project.Load(filepath.Join(dir, manifest.Name))
		if err != nil {
			t.Fatalf("(alternative %s) then (alternative %s) does not resolve:\n%s",
				order[0], order[1], diag.Render(err))
		}

		if got := records(run.Descriptor); got != 1 {
			t.Fatalf("choosing %v resolves to %d record types, want 1", order, got)
		}

		fields := fieldNames(run.Descriptor)
		for name, want := range map[string]int{"PAIR-A": 0, "PAIR-B": 1, "PAIR-C": 0, "PAIR-D": 1} {
			if fields[name] != want {
				t.Errorf("choosing %v, %s stands %d times, want %d", order, name, fields[name], want)
			}
		}
	}
}

// TestAChoiceThatIsNoCombinationNamesEveryCombination is the diagnostic over the
// same copybook: a record choosing one alternative where two redefines want two
// is refused, and every combination there was to choose from is in the message.
//
// One redefine chosen twice is refused by the reader before this, so the shapes
// that reach here are a choice that is short, long, or names something that is
// no alternative at all — and one message answers all of them.
func TestAChoiceThatIsNoCombinationNamesEveryCombination(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	write(t, filepath.Join(dir, "pair.cpy"), fixed(
		"01  PAIR-REC.",
		"    05  PAIR-A      PIC X(4).",
		"    05  PAIR-B REDEFINES PAIR-A PIC X(4).",
		"    05  PAIR-C      PIC X(6).",
		"    05  PAIR-D REDEFINES PAIR-C PIC X(6).",
	))

	write(t, filepath.Join(dir, "pair.sexpr"), `(encoding
  (charset ascii) (sign-convention ascii-zone-37)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 10))
(record PAIR (copybook "pair.cpy" PAIR-REC)
  (alternative (item PAIR PAIR-B)))
(discriminate PAIR single-record-type)
(sequence (* PAIR))
`)
	write(t, filepath.Join(dir, manifest.Name),
		`{"layout": "pair.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	_, err := project.Load(filepath.Join(dir, manifest.Name))

	var alternatives *project.AlternativesError
	if !errors.As(err, &alternatives) {
		t.Fatalf("a short choice reads as %v, want an AlternativesError", err)
	}

	rendered := diag.Render(err)

	// Four combinations of two, rendered one entry apiece rather than flattened
	// into eight names — a copybook with two redefines offers four choices.
	for _, want := range []string{
		`"PAIR-A" with "PAIR-C"`,
		`"PAIR-A" with "PAIR-D"`,
		`"PAIR-B" with "PAIR-C"`,
		`"PAIR-B" with "PAIR-D"`,
		"which is none of the 4 it describes",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the diagnostic does not carry %s:\n%s", want, rendered)
		}
	}
}

// TestAnAlternativeOverACopybookWithNoRedefineIsRefused is the other end of the
// same rule: a copybook with nothing to choose among resolves to one record type
// choosing nothing, and a form choosing something has named an item that is no
// alternative.
func TestAnAlternativeOverACopybookWithNoRedefineIsRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	write(t, filepath.Join(dir, "flat.cpy"), fixed(
		"01  FLAT-REC.",
		"    05  FLAT-KEY   PIC X(4).",
	))

	write(t, filepath.Join(dir, "flat.sexpr"), `(encoding
  (charset ascii) (sign-convention ascii-zone-37)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 4))
(record FLAT (copybook "flat.cpy" FLAT-REC)
  (alternative (item FLAT FLAT-KEY)))
(discriminate FLAT single-record-type)
(sequence (* FLAT))
`)
	write(t, filepath.Join(dir, manifest.Name),
		`{"layout": "flat.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	_, err := project.Load(filepath.Join(dir, manifest.Name))

	var alternatives *project.AlternativesError
	if !errors.As(err, &alternatives) {
		t.Fatalf("an alternative over a copybook with no redefine reads as %v, want an AlternativesError", err)
	}

	if !strings.Contains(diag.Render(err), "no alternative") {
		t.Errorf("the diagnostic does not say there was nothing to choose:\n%s", diag.Render(err))
	}
}

// TestAnAlternativeNamingNoItemIsReported is the fault the resolving order
// exists for: an `alternative` naming an item the copybook does not declare is
// reported against the layout, rather than dropping its record type out of the
// run in silence.
func TestAnAlternativeNamingNoItemIsReported(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	write(t, filepath.Join(dir, "txn.cpy"), fixed(
		"01  TXN-REC.",
		"    05  TXN-PURCHASE  PIC X(4).",
		"    05  TXN-REFUND REDEFINES TXN-PURCHASE PIC X(4).",
	))

	write(t, filepath.Join(dir, "txn.sexpr"), `(encoding
  (charset ascii) (sign-convention ascii-zone-37)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 4))
(record PURCHASE (copybook "txn.cpy" TXN-REC) (alternative (item PURCHASE TXN-CHARGE)))
(discriminate PURCHASE single-record-type)
(sequence (* PURCHASE))
`)
	write(t, filepath.Join(dir, manifest.Name),
		`{"layout": "txn.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	_, err := project.Load(filepath.Join(dir, manifest.Name))

	var unknown *project.UnknownItemError
	if !errors.As(err, &unknown) {
		t.Fatalf("an alternative naming no item reads as %v, want an UnknownItemError", err)
	}
}

// TestCharsetNoneCarriesThroughTheWholePipeline is #275 end to end: an
// `encoding-override` saying an item's bytes are a payload reaches the
// descriptor as CHARSET_NONE on that field and leaves every other field alone.
//
// It is here as well as in `resolve` because this package is where the layout's
// item reference becomes the copybook field the override applies to, and where
// the form's position is carried across for the diagnostic below to point at.
func TestCharsetNoneCarriesThroughTheWholePipeline(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	write(t, filepath.Join(dir, "region.cpy"), fixed(
		"01  REG-REC.",
		"    05  REG-TYPE       PIC X(1).",
		"    05  REG-CODE       PIC X(2).",
		"    05  REG-NAME       PIC X(4).",
	))

	write(t, filepath.Join(dir, "region.sexpr"), `(encoding
  (charset cp037) (sign-convention ebcdic)
  (byte-order big-endian) (float-format hfp))
(encoding-override (item REGION REG-CODE) (charset none))
(framing (recfm F) (lrecl 7))
(record REGION (copybook "region.cpy" REG-REC))
(discriminate REGION single-record-type)
(sequence (* REGION))
`)
	write(t, filepath.Join(dir, manifest.Name), `{"layout": "region.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	run, err := project.Load(filepath.Join(dir, manifest.Name))
	if err != nil {
		t.Fatalf("the layout does not resolve:\n%s", diag.Render(err))
	}

	want := map[string]irpb.Charset{
		"REG-TYPE": irpb.Charset_CHARSET_CP037,
		"REG-CODE": irpb.Charset_CHARSET_NONE,
		"REG-NAME": irpb.Charset_CHARSET_CP037,
	}

	seen := 0

	for _, node := range run.Descriptor.GetNodes() {
		field := node.GetField()
		if field == nil {
			continue
		}

		charset, named := want[field.GetNames().GetOriginal()]
		if !named {
			t.Errorf("the descriptor carries an unexpected field %s", field.GetNames().GetOriginal())

			continue
		}

		seen++

		if got := field.GetEncoding().GetCharset(); got != charset {
			t.Errorf("%s carries %s, want %s", field.GetNames().GetOriginal(), got, charset)
		}
	}

	if seen != len(want) {
		t.Errorf("the descriptor carries %d fields, want the copybook's %d", seen, len(want))
	}
}

// TestCharsetNoneOverAnItemThatIsCharactersNamesBothFiles is the cross-file
// diagnostic docs/layout/SPEC.md's "Every diagnostic carries a span, and some
// carry two" asks for: the override is the line the adopter wrote and the
// copybook may not be theirs to change, so a fault naming only one of them
// names the half they cannot fix.
func TestCharsetNoneOverAnItemThatIsCharactersNamesBothFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	write(t, filepath.Join(dir, "region.cpy"), fixed(
		"01  REG-REC.",
		"    05  REG-TYPE       PIC X(1).",
		"    05  REG-BODY.",
		"        10  REG-CODE   PIC X(2).",
		"        10  REG-COUNT  PIC 9(4).",
	))

	write(t, filepath.Join(dir, "region.sexpr"), `(encoding
  (charset cp037) (sign-convention ebcdic)
  (byte-order big-endian) (float-format hfp))
(encoding-override (item REGION REG-BODY) (charset none))
(framing (recfm F) (lrecl 7))
(record REGION (copybook "region.cpy" REG-REC))
(discriminate REGION single-record-type)
(sequence (* REGION))
`)
	write(t, filepath.Join(dir, manifest.Name), `{"layout": "region.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	_, err := project.Load(filepath.Join(dir, manifest.Name))

	var fault *resolve.CharsetNoneError
	if !errors.As(err, &fault) {
		t.Fatalf("charset none over a zoned item reads as %v, want a CharsetNoneError", err)
	}

	rendered := diag.Render(err)

	// The zoned item under the group is named, and the alphanumeric item beside
	// it is not: the override reaches both and means something for one.
	for _, want := range []string{"REG-COUNT", "region.sexpr:4:", "region.cpy:"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the diagnostic does not name %s:\n%s", want, rendered)
		}
	}

	if strings.Contains(rendered, "REG-CODE") {
		t.Errorf("the diagnostic names REG-CODE, whose bytes charset none can mean something for:\n%s", rendered)
	}
}

// TestCharsetNoneOverAFillerIsBuiltRatherThanRefusedTwiceOver is the two stages
// agreeing, pinned by running one descriptor past both of them.
//
// `resolve` blesses a FILLER an override reaches whatever its PICTURE says, for
// a reason `assemble` shares: an item COBOL gives no data-name is read as the
// bytes it is, no accessor is generated for it, and no item reference can name
// it, so its charset is never read. Two passes reading the same rule differently
// is a layout one stage admits and the next refuses, with the second diagnostic
// naming a node id and no file, line or name — so the agreement is asserted
// here, where a descriptor goes through both, rather than separately in each.
func TestCharsetNoneOverAFillerIsBuiltRatherThanRefusedTwiceOver(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	write(t, filepath.Join(dir, "region.cpy"), fixed(
		"01  REG-REC.",
		"    05  REG-TYPE       PIC X(1).",
		"    05  REG-BODY.",
		"        10  REG-CODE   PIC X(2).",
		"        10  FILLER     PIC 9(4).",
	))

	write(t, filepath.Join(dir, "region.sexpr"), `(encoding
  (charset cp037) (sign-convention ebcdic)
  (byte-order big-endian) (float-format hfp))
(encoding-override (item REGION REG-BODY) (charset none))
(framing (recfm F) (lrecl 7))
(record REGION (copybook "region.cpy" REG-REC))
(discriminate REGION single-record-type)
(sequence (* REGION))
`)
	write(t, filepath.Join(dir, manifest.Name), `{"layout": "region.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	run, err := project.Load(filepath.Join(dir, manifest.Name))
	if err != nil {
		t.Fatalf("a group override reaching a FILLER does not build:\n%s", diag.Render(err))
	}

	// And the override did reach it. A build that passed because the FILLER
	// never carried the charset would prove nothing about the rule, so the
	// unnamed field is found by the shape a FILLER has — no names message at
	// all — and held to carrying the value the override wrote.
	found := false

	for _, node := range run.Descriptor.GetNodes() {
		field := node.GetField()
		if field == nil || field.GetNames() != nil {
			continue
		}

		found = true

		if got := field.GetEncoding().GetCharset(); got != irpb.Charset_CHARSET_NONE {
			t.Errorf("the FILLER under the override carries %s, want %s", got, irpb.Charset_CHARSET_NONE)
		}
	}

	if !found {
		t.Error("the descriptor carries no unnamed field, so the copybook's FILLER never reached it")
	}
}
