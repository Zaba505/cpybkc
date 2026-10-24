// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package scaffold

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	cobol "github.com/Zaba505/cobol-go"

	"github.com/Zaba505/cpybkc/internal/definitions"
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/emit"
	"github.com/Zaba505/cpybkc/internal/manifest"
	"github.com/Zaba505/cpybkc/internal/project"
)

// ledgerDir is the worked example, whose three copybooks are what
// docs/cli/SPEC.md's `init` arithmetic is measured against.
var ledgerDir = filepath.Join("..", "..", "example", "ledger")

// ledgerCopybooks are the worked example's copybooks, as `init` would be handed
// them from inside its directory.
func ledgerCopybooks(t *testing.T) []Copybook {
	t.Helper()

	books := make([]Copybook, 0, 3)

	for _, name := range []string{"header.cpy", "posting.cpy", "trailer.cpy"} {
		source, err := os.ReadFile(filepath.Join(ledgerDir, name))
		if err != nil {
			t.Fatalf("reading the worked example's copybook: %v", err)
		}

		books = append(books, Copybook{Path: name, Source: source})
	}

	return books
}

func TestADefinitionsFileIsNamedAfterTheCopybooksFinalPathElement(t *testing.T) {
	t.Parallel()

	for typed, want := range map[string]string{
		"cpy/posting.cpy":       "posting.cpy.sexpr",
		"vendor/PXTRACT":        "PXTRACT.sexpr",
		"posting.cpy":           "posting.cpy.sexpr",
		"../shared/header.cbl":  "header.cbl.sexpr",
		"./cpy/../trailer.cpy":  "trailer.cpy.sexpr",
		"/abs/path/to/LEDGER.c": "LEDGER.c.sexpr",
	} {
		if got := DefinitionsName(typed); got != want {
			t.Errorf("DefinitionsName(%q) = %q, want %q", typed, got, want)
		}
	}
}

// docs/cli/SPEC.md, "What the scaffold states, form by form": every entry and
// every clause of the copybook, in source order, including every entry no record
// derives from, under exactly one comment of its own naming the copybook as it
// was typed — and none of the copybook's comments.
func TestADefinitionsFileHoldsEveryEntryAndOneCommentNamingTheCopybook(t *testing.T) {
	t.Parallel()

	const source = `      * A comment the copybook carries, which the file does not.
       01  FILLER.
           05  FLR-ONE              PIC X.
       01  KEPT-RECORD.
           05  KPT-CODE             PIC X.
               88  KPT-IS-A         VALUE 'A'.
           05  KPT-AMOUNT           PIC S9(5)V99 COMP-3.
       77  WORK-COUNT               PIC 9(4).
`

	derived := deriveOf(t, book("vendor/kept.cpy", source))

	files := derived.Definitions()
	if len(files) != 1 {
		t.Fatalf("wrote %d definitions files over one copybook, want one", len(files))
	}

	want := `; The entries of the copybook "vendor/kept.cpy", transcribed by cpybkc init.
(entry 01 FILLER)
  (entry 05 FLR-ONE (pic "X"))
(entry 01 KEPT-RECORD)
  (entry 05 KPT-CODE (pic "X"))
    (entry 88 KPT-IS-A (value "'A'"))
  (entry 05 KPT-AMOUNT (pic "S9(5)V99") (usage COMP-3))
(entry 77 WORK-COUNT (pic "9(4)"))
`

	if files[0].Name != "kept.cpy.sexpr" {
		t.Errorf("the definitions file is named %s, want kept.cpy.sexpr", files[0].Name)
	}

	if got := string(files[0].Bytes); got != want {
		t.Errorf("the definitions file reads\n%s\nwant\n%s", got, want)
	}

	// Lines that open a comment, rather than semicolons: a POSIX path may
	// hold one, and the header quotes the path.
	comments := 0

	for line := range strings.SplitSeq(string(files[0].Bytes), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), ";") {
			comments++
		}
	}

	if comments != 1 {
		t.Errorf("the definitions file carries %d comments, want its one header", comments)
	}
}

// Two runs of one build produce byte-identical files throughout, the
// definitions files as well as the layout: a definitions file is what an adopter
// diffs when the vendor ships a new copybook.
func TestTwoRunsProduceByteIdenticalDefinitionsFiles(t *testing.T) {
	t.Parallel()

	first, second := deriveOf(t, ledgerCopybooks(t)...), deriveOf(t, ledgerCopybooks(t)...)

	one, two := first.Definitions(), second.Definitions()
	if len(one) != len(two) {
		t.Fatalf("two runs wrote %d and %d definitions files", len(one), len(two))
	}

	for i := range one {
		if one[i].Name != two[i].Name || !bytes.Equal(one[i].Bytes, two[i].Bytes) {
			t.Errorf("two runs wrote %s differently", one[i].Name)
		}
	}

	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Error("two runs wrote the layout differently")
	}
}

// The read-back is what catches a transcription that does not say what the
// copybook says, before anything is written. Nothing cpybkc writes today
// mismatches — every tracked copybook round-trips (#401) — so the mismatch is
// forced, by reading back text that differs from the copybook in one clause.
func TestADefinitionsFileThatDoesNotReadBackAsItsCopybookIsRefused(t *testing.T) {
	t.Parallel()

	b := book("cpy/header.cpy", header)

	file, err := cobol.Parse(bytes.NewReader(b.Source), cobol.WithFragment(), cobol.WithSourceFormat(cobol.FixedFormat))
	if err != nil {
		t.Fatalf("parsing the copybook: %v", err)
	}

	entries := file.Fragment.Entries

	var text bytes.Buffer
	if err := definitions.Write(&text, entries, definitions.FileDepth); err != nil {
		t.Fatalf("writing the definitions file: %v", err)
	}

	if err := readBack(b, entries, text.Bytes(), nil); err != nil {
		t.Fatalf("the faithful transcription did not read back:\n%s", diag.Render(err))
	}

	// HDR-COUNT's picture changed in the bytes about to be written, which is
	// the shape a writer defect would take.
	tampered := bytes.Replace(text.Bytes(), []byte(`"9(6)"`), []byte(`"9(7)"`), 1)
	if bytes.Equal(tampered, text.Bytes()) {
		t.Fatalf("the test's tampering found nothing to change in\n%s", text.Bytes())
	}

	err = readBack(b, entries, tampered, nil)

	var mismatch *ReadBackError
	if !errors.As(err, &mismatch) {
		t.Fatalf("the fault is %v, want a definitions file that did not read back", err)
	}

	rendered := diag.Render(err)
	for _, want := range []string{"cpy/header.cpy:3:", "05 HDR-COUNT", "defect in cpybkc"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the diagnostic does not carry %q:\n%s", want, rendered)
		}
	}

	// A file that loses an entry is refused too, naming the counts.
	short := bytes.Join(bytes.Split(text.Bytes(), []byte("\n"))[:2], []byte("\n"))

	if err := readBack(b, entries, short, nil); !errors.As(err, &mismatch) {
		t.Fatalf("a file short an entry gave %v, want a definitions file that did not read back", err)
	} else if rendered := diag.Render(err); !strings.Contains(rendered, "holds 2 entries and the copybook declares 3") {
		t.Errorf("the diagnostic does not say how the file is short:\n%s", rendered)
	}
}

// A copybook the writer refuses is one that cannot be transcribed, which fails
// the run naming the copybook as typed and the place in it. Since
// Zaba505/cobol-go#145, #150 and #152 no copybook `cobol-go` parses holds an
// entry the writer has no form for, so the refusal is reached here with an
// entry built by hand: a VALUE holding a procedure-division operand, which no
// data-description entry the parser builds holds.
func TestACopybookTheWriterRefusesFailsTheRunAtItsPlaceInTheCopybook(t *testing.T) {
	t.Parallel()

	entries := []*cobol.DataDescriptionEntry{
		{Pos: cobol.Pos{Line: 1, Column: 8}, Level: 1, Name: &cobol.Word{Value: "CODE-RECORD"}},
		{
			Pos:   cobol.Pos{Line: 3, Column: 12},
			Level: 5,
			Name:  &cobol.Word{Value: "CDE-TYPE"},
			Clauses: []cobol.DataClause{
				&cobol.PictureClause{Picture: "X"},
				&cobol.ValueClause{Values: []cobol.ValueSpec{{From: &cobol.Identifier{Name: &cobol.Word{Value: "OTHER"}}}}},
			},
		},
	}

	_, err := transcribe(book("cpy/code.cpy", ""), entries, nil)

	var refused *TranscriptionError
	if !errors.As(err, &refused) {
		t.Fatalf("the fault is %v, want a copybook that cannot be transcribed", err)
	}

	var cause *definitions.UnknownNodeError
	if !errors.As(err, &cause) {
		t.Errorf("the fault does not carry the writer's refusal: %v", err)
	}

	if rendered := diag.Render(err); !strings.HasPrefix(rendered, "cpy/code.cpy:3:") {
		t.Errorf("the diagnostic does not lead with the copybook as typed and the line:\n%s", rendered)
	}
}

// docs/layout/SPEC.md, "A prefixed literal, an ALL literal and VALUES ARE are
// transcribed as cobol-go reads them": a condition-name the pin before
// Zaba505/cobol-go#145, #150 and #152 misread, which conversion refused and
// failed the run over, converts as the values it holds.
func TestACopybookCobolGoOnceMisreadConvertsAsItReads(t *testing.T) {
	t.Parallel()

	const once = `       01  CODE-RECORD.
           05  CDE-TYPE             PIC X.
               88  CDE-KNOWN        VALUES ARE 'A' 'B'.
               88  CDE-NUL          VALUE X'00'.
               88  CDE-STARS        VALUE ALL '*'.
`

	s, err := Derive([]Copybook{book("cpy/code.cpy", once)})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}

	got := string(s.Definitions()[0].Bytes)

	for _, want := range []string{
		`(entry 88 CDE-KNOWN (value "'A'" "'B'"))`,
		`(entry 88 CDE-NUL (value "X'00'"))`,
		`(entry 88 CDE-STARS (value (all "'*'")))`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the definitions file does not hold %s:\n%s", want, got)
		}
	}
}

// Two copybooks whose definitions files would share a name are refused by the
// command line before anything is opened; Derive refuses them as well, because
// it is reachable without one and a set holding two files of one name cannot
// be put in a directory.
func TestTwoCopybooksDerivingOneDefinitionsNameAreRefused(t *testing.T) {
	t.Parallel()

	_, err := Derive([]Copybook{book("a/posting.cpy", header), book("b/POSTING.cpy", header)})

	var clash *DefinitionsNameError
	if !errors.As(err, &clash) {
		t.Fatalf("the fault is %v, want two copybooks deriving one definitions name", err)
	}

	if clash.First != "a/posting.cpy" || clash.Second != "b/POSTING.cpy" {
		t.Errorf("the fault names %s and %s, want both copybooks as typed", clash.First, clash.Second)
	}
}

// copybookSource is a record's `copybook` source, with its path, as a layout
// writes one.
var copybookSource = regexp.MustCompile(`\(copybook "([^"]*)"`)

// The worked example, end to end: `init` over example/ledger's three copybooks
// writes definitions files that resolve to the descriptor the committed project
// resolves to from its copybooks, byte for byte. The committed layout is the
// adopter's finished version of the scaffold, and moving it onto `init`'s
// definitions files — `(copybook "x.cpy" N)` to `(definitions "x.cpy.sexpr" N)`,
// which is docs/cli/SPEC.md's "copybook to file" move — changes nothing a
// generator is handed.
func TestInitOverTheLedgerCopybooksResolvesToTheLedgersDescriptor(t *testing.T) {
	t.Parallel()

	committed, err := project.Load(filepath.Join(ledgerDir, manifest.Name))
	if err != nil {
		t.Fatalf("the worked example does not resolve:\n%s", diag.Render(err))
	}

	derived := deriveOf(t, ledgerCopybooks(t)...)
	dir := t.TempDir()

	definitionsFiles := make([]Output, 0, 3)
	for _, file := range derived.Definitions() {
		definitionsFiles = append(definitionsFiles, Output{Path: filepath.Join(dir, file.Name), Bytes: file.Bytes})
	}

	// The scaffold goes to a name of its own; it is not the layout this run
	// resolves, which is the committed one moved onto the files beside it.
	scaffold := Output{Path: filepath.Join(dir, "scaffold.sexpr"), Bytes: derived.Bytes()}
	if err := Write(t.Context(), scaffold, definitionsFiles); err != nil {
		t.Fatalf("writing the set:\n%s", diag.Render(err))
	}

	layout, err := os.ReadFile(filepath.Join(ledgerDir, "ledger.sexpr"))
	if err != nil {
		t.Fatalf("reading the worked example's layout: %v", err)
	}

	moved := copybookSource.ReplaceAllString(string(layout), `(definitions "$1.sexpr"`)
	if strings.Contains(moved, `(copybook "`) || moved == string(layout) {
		t.Fatalf("the layout was not moved onto the definitions files:\n%s", moved)
	}

	writeFile(t, filepath.Join(dir, "ledger.sexpr"), moved)

	m, err := os.ReadFile(filepath.Join(ledgerDir, manifest.Name))
	if err != nil {
		t.Fatalf("reading the worked example's manifest: %v", err)
	}

	writeFile(t, filepath.Join(dir, manifest.Name), string(m))

	resolved, err := project.Load(filepath.Join(dir, manifest.Name))
	if err != nil {
		t.Fatalf("the layout does not resolve over init's definitions files:\n%s", diag.Render(err))
	}

	want, err := emit.Marshal(committed.Descriptor)
	if err != nil {
		t.Fatalf("encoding the committed descriptor: %v", err)
	}

	got, err := emit.Marshal(resolved.Descriptor)
	if err != nil {
		t.Fatalf("encoding the descriptor: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Error("init's definitions files resolve to a different descriptor than the worked example's copybooks")
	}
}

// writeFile puts text at path, or fails the test.
func writeFile(t *testing.T, path, text string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
