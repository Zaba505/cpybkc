// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"errors"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
	"github.com/Zaba505/cpybkc/internal/layoutdoc"
)

// parsed is a layout's source, parsed under the name layout.sexpr.
func parsed(t *testing.T, source string) *layout.File {
	t.Helper()

	file, err := layout.Parse("layout.sexpr", strings.NewReader(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	return file
}

// TestARecordReadsEachOfItsThreeSources is #402's record reader: a source is a
// copybook path, a definitions-file path or a block name, told apart by the
// form's tag and by the first argument's kind, and each part carries its
// position.
func TestARecordReadsEachOfItsThreeSources(t *testing.T) {
	t.Parallel()

	records, err := recordsOf(t, `(record HEADER (copybook "cpy/header.cpy" HEADER-REC))
(record POSTING
  (definitions "posting.cpy.sexpr" POSTING-RECORD))
(definitions-block TRAILER (entry 01 LEDGER-TRAILER))
(record LEDGER-TRAILER (definitions TRAILER LEDGER-TRAILER))
`)
	if err != nil {
		t.Fatalf("records: %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("the layout defines %d records, want 3", len(records))
	}

	for i, want := range []struct {
		kind  SourceKind
		path  string
		block string
		item  string
		line  int
	}{
		{kind: CopybookSource, path: "cpy/header.cpy", item: "HEADER-REC", line: 1},
		{kind: DefinitionsFileSource, path: "posting.cpy.sexpr", item: "POSTING-RECORD", line: 3},
		{kind: DefinitionsBlockSource, block: "TRAILER", item: "LEDGER-TRAILER", line: 5},
	} {
		record := records[i]

		if record.Kind != want.kind || record.Path != want.path || record.Block != want.block || record.Item != want.item {
			t.Errorf("record %s reads as %s %q %q item %q, want %s %q %q item %q",
				record.Name, record.Kind, record.Path, record.Block, record.Item, want.kind, want.path, want.block, want.item)
		}

		if record.Source.Line != want.line {
			t.Errorf("record %s's source is at %s, want line %d", record.Name, record.Source, want.line)
		}

		if record.ItemPos.Line != want.line {
			t.Errorf("record %s's item is at %s, want line %d", record.Name, record.ItemPos, want.line)
		}
	}

	if pos := records[1].PathPos; pos.Line != 3 || pos.Column != 16 {
		t.Errorf("the definitions file's path is at %s, want 3:16", pos)
	}

	if pos := records[2].BlockPos; pos.Line != 5 || pos.Column != 37 {
		t.Errorf("the block name is at %s, want 5:37", pos)
	}

	if records[2].Path != "" || records[0].Block != "" {
		t.Error("a source carries a path and a block name both; it carries the one it names")
	}
}

// TestAMalformedDefinitionsSourceIsReportedAgainstThePartThatIsWrong is
// [TestAMalformedRecordIsReportedAgainstThePartThatIsWrong] for the second
// source form.
func TestAMalformedDefinitionsSourceIsReportedAgainstThePartThatIsWrong(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		source string
		want   any
	}{
		"no top-level name":           {"(record P (definitions \"p.cpy.sexpr\"))\n", new(*DefinitionsFormError)},
		"a top-level name as text":    {"(record P (definitions \"p.cpy.sexpr\" \"P-REC\"))\n", new(*DefinitionsFormError)},
		"an origin that is a number":  {"(record P (definitions 3 P-REC))\n", new(*DefinitionsFormError)},
		"an origin that is a form":    {"(record P (definitions (item P A) P-REC))\n", new(*DefinitionsFormError)},
		"a path of no characters":     {"(record P (definitions \"\" P-REC))\n", new(*EmptyDefinitionsPathError)},
		"a block nothing defines":     {"(record P (definitions TRAILER P-REC))\n", new(*UndefinedBlockError)},
		"a source of another tag":     {"(record P (definitions-block T (entry 01 A)))\n", new(*ChildError)},
		"a name and no source at all": {"(record P)\n", new(*RecordFormError)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			records, err := recordsOf(t, test.source)
			if err == nil {
				t.Fatalf("%s reads as %d sound records, want a fault", name, len(records))
			}

			if !errors.As(err, test.want) {
				t.Fatalf("%s reads as %v, want a %T", name, err, test.want)
			}
		})
	}
}

// TestTheRecordFaultsNameBothSources holds the two messages about a record's
// shape to the two sources it admits, so an adopter told their record is
// wrong is shown both spellings that are right.
func TestTheRecordFaultsNameBothSources(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"a record with no source":       "(record P)\n",
		"a source that is neither form": "(record P (rename \"a\"))\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := recordsOf(t, source)
			if err == nil {
				t.Fatal("the record read without a fault")
			}

			message := err.Error()
			if !strings.Contains(message, "copybook") || !strings.Contains(message, "definitions") {
				t.Errorf("the fault does not name both sources: %s", message)
			}
		})
	}
}

// TestAnUndefinedBlockListsTheBlocksDefined is the reader's half of the
// schema's reference fault, for a caller reading without the schema.
func TestAnUndefinedBlockListsTheBlocksDefined(t *testing.T) {
	t.Parallel()

	_, err := recordsOf(t, `(definitions-block HEADER (entry 01 H))
(definitions-block FOOTER (entry 01 F))
(record H (definitions HEADER H))
(record F (definitions FOOTER F))
(record T (definitions TRAILR T))
`)

	var undefined *UndefinedBlockError
	if !errors.As(err, &undefined) {
		t.Fatalf("a block nothing defines reads as %v, want an UndefinedBlockError", err)
	}

	if undefined.Pos.Line != 5 || undefined.Pos.Column != 24 {
		t.Errorf("the fault is at %s, want the block name, at 5:24", undefined.Pos)
	}

	if !strings.Contains(err.Error(), "it defines HEADER, FOOTER") {
		t.Errorf("the fault does not list the blocks the layout defines: %v", err)
	}
}

// TestABlockNoRecordNamesIsAnErrorAtItsName is docs/layout/SPEC.md's "A block's
// name, and a block no record names": an error, at the block's name, and only
// for the block nothing names.
func TestABlockNoRecordNamesIsAnErrorAtItsName(t *testing.T) {
	t.Parallel()

	_, err := recordsOf(t, `(record T (definitions TRAILER T-REC))
(definitions-block TRAILER (entry 01 T-REC))
(definitions-block HEADER
  (entry 01 H-REC))
`)

	var unnamed *UnnamedBlockError
	if !errors.As(err, &unnamed) {
		t.Fatalf("a block no record names reads as %v, want an UnnamedBlockError", err)
	}

	if unnamed.Block != "HEADER" || unnamed.Pos.Line != 3 || unnamed.Pos.Column != 20 {
		t.Errorf("the fault names %q at %s, want HEADER at its name, 3:20", unnamed.Block, unnamed.Pos)
	}

	if got := len(diag.Diagnostics(err)); got != 1 {
		t.Errorf("want one fault, for the block nothing names; got %d:\n%s", got, diag.Render(err))
	}
}

// TestARecordNamingABlockCountsWhateverElseIsWrongWithIt keeps the unnamed
// block fault to the block nothing names: a record that names a block and is
// wrong in another way still names it.
func TestARecordNamingABlockCountsWhateverElseIsWrongWithIt(t *testing.T) {
	t.Parallel()

	_, err := recordsOf(t, `(definitions-block TRAILER (entry 01 T-REC))
(record T (definitions TRAILER "T-REC"))
`)

	var unnamed *UnnamedBlockError
	if errors.As(err, &unnamed) {
		t.Errorf("the block a malformed record names is reported unnamed:\n%s", diag.Render(err))
	}

	var form *DefinitionsFormError
	if !errors.As(err, &form) {
		t.Errorf("want the record's own fault; got:\n%s", diag.Render(err))
	}
}

// TestEveryBlockIsReadWithItsNameItsPositionsAndItsBody is [ReadBlocks]: every
// block, in source order, carrying its body as the nodes the layout wrote.
func TestEveryBlockIsReadWithItsNameItsPositionsAndItsBody(t *testing.T) {
	t.Parallel()

	blocks, err := ReadBlocks(parsed(t, `(record T (definitions TRAILER T-REC))
(definitions-block TRAILER
  (entry 01 T-REC)
    (entry 05 T-TYPE (pic "X")))
(definitions-block HEADER (entry 01 H-REC))
`))
	if err != nil {
		t.Fatalf("blocks: %v", err)
	}

	if len(blocks) != 2 || blocks[0].Name != "TRAILER" || blocks[1].Name != "HEADER" {
		t.Fatalf("the blocks read as %+v, want TRAILER then HEADER", blocks)
	}

	trailer := blocks[0]

	for name, want := range map[string]struct {
		pos          layout.Pos
		line, column int
	}{
		"the form": {trailer.Pos, 2, 1},
		"the tag":  {trailer.TagPos, 2, 2},
		"the name": {trailer.NamePos, 2, 20},
	} {
		if want.pos.File != "layout.sexpr" || want.pos.Line != want.line || want.pos.Column != want.column {
			t.Errorf("%s is at %s, want layout.sexpr:%d:%d", name, want.pos, want.line, want.column)
		}
	}

	if len(trailer.Entries) != 2 {
		t.Fatalf("TRAILER's body is %d nodes, want 2", len(trailer.Entries))
	}

	second, ok := trailer.Entries[1].(layout.Form)
	if !ok || second.Tag != "entry" || second.Pos.Line != 4 {
		t.Errorf("TRAILER's second entry reads as %#v, want the entry on line 4", trailer.Entries[1])
	}
}

// TestABlockDefinedTwiceCarriesBothSpans is the layout reader's fault with two
// spans: the second definition, where the fault is, and the first.
func TestABlockDefinedTwiceCarriesBothSpans(t *testing.T) {
	t.Parallel()

	_, err := ReadBlocks(parsed(t, `(definitions-block TRAILER (entry 01 A))
(record T (definitions TRAILER A))
(definitions-block TRAILER (entry 01 B))
`))

	var duplicate *DuplicateBlockError
	if !errors.As(err, &duplicate) {
		t.Fatalf("a block defined twice reads as %v, want a DuplicateBlockError", err)
	}

	diagnostics := diag.Diagnostics(err)
	if len(diagnostics) != 1 {
		t.Fatalf("want one diagnostic; got:\n%s", diag.Render(err))
	}

	want := []diag.Span{
		{File: "layout.sexpr", Line: 3, Column: 20},
		{File: "layout.sexpr", Line: 1, Column: 20, Note: "it is defined first here"},
	}

	if spans := diagnostics[0].Spans; len(spans) != 2 || spans[0] != want[0] || spans[1] != want[1] {
		t.Errorf("the fault's spans are %+v, want %+v", diagnostics[0].Spans, want)
	}
}

// TestAMalformedBlockIsAFault is the reader's half of a block's shape, for a
// caller reading without the schema.
func TestAMalformedBlockIsAFault(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"no entries":            "(definitions-block TRAILER)\n",
		"no name":               "(definitions-block)\n",
		"a name that is text":   "(definitions-block \"TRAILER\" (entry 01 A))\n",
		"a name that is a form": "(definitions-block (entry 01 A) (entry 05 B))\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := ReadBlocks(parsed(t, source))

			var form *BlockFormError
			if !errors.As(err, &form) {
				t.Errorf("%s reads as %v, want a BlockFormError", name, err)
			}
		})
	}
}

// TestTheTopLevelNameIsReadFromEitherSource is recordNames, which the
// discrimination and rename readers share: a record's top-level name is read
// from whichever source it names, so a rename colliding with it collides
// whatever the source.
func TestTheTopLevelNameIsReadFromEitherSource(t *testing.T) {
	t.Parallel()

	records := recordNames(parsed(t, `(record A (copybook "a.cpy" A-REC))
(record B (definitions "b.cpy.sexpr" B-REC))
(record C (definitions C-BLOCK C-REC))
`))

	var items []string
	for _, record := range records {
		items = append(items, record.item)
	}

	if got := strings.Join(items, ","); got != "A-REC,B-REC,C-REC" {
		t.Errorf("the top-level names read as %s, want A-REC,B-REC,C-REC", got)
	}
}

// TestTheSpecsDefinitionsLayoutIsReadByEveryLayer is the staleness gate over
// #395's inline example, docs/layout/SPEC.md's "Appendix: Definitions held
// three ways": every layer reader reads it, and its records come back bound to
// a copybook, a file and a block.
func TestTheSpecsDefinitionsLayoutIsReadByEveryLayer(t *testing.T) {
	t.Parallel()

	file := parsed(t, specExample(t, layoutdoc.DefinitionsExample))

	records, err := ReadRecords(file)
	if err != nil {
		t.Fatalf("records: %v", err)
	}

	kinds := make(map[string]SourceKind, len(records))
	for _, record := range records {
		kinds[record.Name] = record.Kind
	}

	for name, want := range map[string]SourceKind{
		"LEDGER-HEADER":  CopybookSource,
		"DEBIT-POSTING":  DefinitionsFileSource,
		"CREDIT-POSTING": DefinitionsFileSource,
		"LEDGER-TRAILER": DefinitionsBlockSource,
	} {
		if kinds[name] != want {
			t.Errorf("record %s is bound to %s, want %s", name, kinds[name], want)
		}
	}

	blocks, err := ReadBlocks(file)
	if err != nil {
		t.Fatalf("blocks: %v", err)
	}

	if len(blocks) != 1 || blocks[0].Name != "TRAILER" || len(blocks[0].Entries) != 4 {
		t.Errorf("the blocks read as %+v, want TRAILER holding four entries", blocks)
	}

	for name, read := range map[string]func(*layout.File) error{
		"profile":          func(f *layout.File) error { _, err := ReadProfile(f); return err },
		"framing":          func(f *layout.File) error { _, err := ReadFraming(f); return err },
		"discrimination":   func(f *layout.File) error { _, err := ReadDiscrimination(f); return err },
		"sequence":         func(f *layout.File) error { _, err := ReadSequence(f); return err },
		"renames":          func(f *layout.File) error { _, err := ReadRenames(f); return err },
		"copybook reading": func(f *layout.File) error { _, err := ReadCopybookReading(f); return err },
	} {
		if err := read(file); err != nil {
			t.Errorf("the %s reader rejects the SPEC's example:\n%s", name, diag.Render(err))
		}
	}
}
