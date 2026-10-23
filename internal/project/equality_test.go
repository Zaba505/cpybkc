// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package project_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	cobol "github.com/Zaba505/cobol-go"

	"github.com/Zaba505/cpybkc/internal/conformance"
	"github.com/Zaba505/cpybkc/internal/definitions"
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/emit"
	"github.com/Zaba505/cpybkc/internal/manifest"
	"github.com/Zaba505/cpybkc/internal/project"
)

// trackedCorpusEntries is how many entries the conformance corpus held when
// #405 was filed. It is a floor, so that an entry added later is simply one
// more converted, and it is there so that a corpus that failed to load reads as
// a failure rather than as a pass over nothing.
const trackedCorpusEntries = 59

// copybookSource is a record's `copybook` source, with its path, as a layout
// writes one. A `copybook-reading` form is not one: its tag is followed by a
// form, never by text.
var copybookSource = regexp.MustCompile(`\(copybook "([^"]*)"`)

// TestADefinitionsFileResolvesToTheCopybooksDescriptor is docs/layout/SPEC.md's
// "Three sources, one descriptor" for the first two of them: a record bound to
// a copybook and the same record bound to the definitions file #401's writer
// converts from it resolve to byte-identical descriptors, everything else in
// the layout held equal.
//
// It is every layout this repository holds — every conformance corpus entry and
// every example — and every record in each of them. Each copybook a layout
// names is converted beside it, each record's `copybook` source is rewritten to
// `definitions`, and both projects go through [project.Load], which is the
// whole of what a run does before a generator is started.
func TestADefinitionsFileResolvesToTheCopybooksDescriptor(t *testing.T) {
	t.Parallel()

	entries, err := conformance.Load(conformance.CorpusPath(root))
	if err != nil {
		t.Fatalf("loading the corpus: %v", err)
	}

	if len(entries) < trackedCorpusEntries {
		t.Fatalf("the corpus holds %d entries, fewer than the %d it held when #405 was filed", len(entries), trackedCorpusEntries)
	}

	for _, entry := range entries {
		t.Run("corpus/"+entry.Name, func(t *testing.T) {
			t.Parallel()

			dir := copyProject(t, filepath.Dir(entry.Layout))
			layout := filepath.Base(entry.Layout)
			write(t, filepath.Join(dir, manifest.Name), `{"layout": "`+layout+`", "generators": [{"name": "go", "out": "gen"}]}`)

			sameDescriptor(t, dir, layout, convert)
		})
	}

	everyExample(t, convert)
}

// TestADefinitionsBlockResolvesToTheCopybooksDescriptor is "Three sources, one
// descriptor" for the third of them: a record bound to a copybook and the same
// record bound to a `definitions-block` holding the entries #401's writer
// converts from it, at block depth, resolve to byte-identical descriptors.
//
// It is every layout this repository holds and every record in each, as the
// definitions file's equality is. Each copybook a layout names is converted
// into a block inlined in the layout, each record's `copybook` source is
// rewritten to name the block, and both projects go through [project.Load].
func TestADefinitionsBlockResolvesToTheCopybooksDescriptor(t *testing.T) {
	t.Parallel()

	entries, err := conformance.Load(conformance.CorpusPath(root))
	if err != nil {
		t.Fatalf("loading the corpus: %v", err)
	}

	if len(entries) < trackedCorpusEntries {
		t.Fatalf("the corpus holds %d entries, fewer than the %d it held when #405 was filed", len(entries), trackedCorpusEntries)
	}

	for _, entry := range entries {
		t.Run("corpus/"+entry.Name, func(t *testing.T) {
			t.Parallel()

			dir := copyProject(t, filepath.Dir(entry.Layout))
			layout := filepath.Base(entry.Layout)
			write(t, filepath.Join(dir, manifest.Name), `{"layout": "`+layout+`", "generators": [{"name": "go", "out": "gen"}]}`)

			sameDescriptor(t, dir, layout, inline)
		})
	}

	everyExample(t, inline)
}

// everyExample holds every example to the equality, converted by conversion.
func everyExample(t *testing.T, conversion func(*testing.T, string, string)) {
	t.Helper()

	examples, err := filepath.Glob(filepath.Join(root, "example", "*", manifest.Name))
	if err != nil || len(examples) == 0 {
		t.Fatalf("no example found: %v", err)
	}

	for _, example := range examples {
		t.Run("example/"+filepath.Base(filepath.Dir(example)), func(t *testing.T) {
			t.Parallel()

			m, err := manifest.ReadFile(example)
			if err != nil {
				t.Fatalf("reading the example's manifest: %v", err)
			}

			dir := copyProject(t, filepath.Dir(example))

			sameDescriptor(t, dir, m.Layout, conversion)
		})
	}
}

// TestTheKitchenSinkResolvesTheSameEitherWay holds the kitchen sink — every
// production of the pinned `cobol-go`'s data-description grammar — to the same
// equality, or, where `cobol-go` refuses to build or lay it out, to the same
// refusal: the same type, saying the same thing about the same item, with the
// one difference that the definitions file's says it at its line in that file.
//
// The layout over it chooses one of the three alternatives of the REDEFINES it
// carries outside a table, since a record has to say which it is; which one is
// immaterial to the equality, and both sides make the same choice.
func TestTheKitchenSinkResolvesTheSameEitherWay(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		conversion func(*testing.T, string, string)
		in         func(dir string) string
	}{
		"a definitions file": {
			conversion: convert,
			in:         func(dir string) string { return filepath.Join(dir, "kitchen-sink.cpy.sexpr") },
		},
		"a definitions block": {
			conversion: inline,
			in:         func(dir string) string { return filepath.Join(dir, "sink.sexpr") },
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			kitchenSink(t, test.conversion, test.in)
		})
	}
}

// kitchenSink resolves the kitchen sink from its copybook, converts it by
// conversion and resolves it again, and requires the same descriptor or the
// same refusal, the converted one in the file in names.
func kitchenSink(t *testing.T, conversion func(*testing.T, string, string), in func(dir string) string) {
	t.Helper()

	src, err := os.ReadFile(filepath.Join(root, "internal", "definitions", "testdata", "kitchen-sink.cpy"))
	if err != nil {
		t.Fatalf("reading the kitchen sink: %v", err)
	}

	dir := t.TempDir()

	write(t, filepath.Join(dir, "kitchen-sink.cpy"), string(src))
	write(t, filepath.Join(dir, "sink.sexpr"), `(encoding
  (charset cp037) (sign-convention ebcdic)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm V))
(copybook-reading (occurs-depending-on odoslide))
(record SINK (copybook "kitchen-sink.cpy" KITCHEN-SINK)
  (alternative (item SINK LEVEL-03 ORIGINAL)))
(discriminate SINK single-record-type)
(sequence (* SINK))
`)
	write(t, filepath.Join(dir, manifest.Name), `{"layout": "sink.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	original, originalErr := project.Load(filepath.Join(dir, manifest.Name))

	conversion(t, dir, "sink.sexpr")

	converted, convertedErr := project.Load(filepath.Join(dir, manifest.Name))

	switch {
	case originalErr == nil && convertedErr == nil:
		if !bytes.Equal(marshal(t, original), marshal(t, converted)) {
			t.Error("the kitchen sink resolves to different descriptors from its copybook and converted")
		}
	case originalErr != nil && convertedErr != nil:
		sameRefusal(t, originalErr, convertedErr, in(dir))
	default:
		t.Errorf("the kitchen sink resolves one way and not the other:\nfrom the copybook: %s\nconverted: %s",
			diag.Render(originalErr), diag.Render(convertedErr))
	}
}

// sameRefusal requires two refusals to be the same `cobol-go` refusal: one
// fault each, the same type, the same words once its line and column are set
// aside, and the converted one's pointing into file — the definitions file, or
// the layout holding the block.
func sameRefusal(t *testing.T, original, converted error, file string) {
	t.Helper()

	was, is := diag.Diagnostics(original), diag.Diagnostics(converted)
	if len(was) != 1 || len(is) != 1 {
		t.Fatalf("want one refusal either way:\nfrom the copybook:\n%s\nfrom the definitions file:\n%s",
			diag.Render(original), diag.Render(converted))
	}

	var fault *project.DefinitionsFaultError
	if !errors.As(converted, &fault) {
		t.Fatalf("the converted refusal is not a fault in the entries:\n%s", diag.Render(converted))
	}

	position := regexp.MustCompile(`at line \d+, column \d+`)

	if got, want := position.ReplaceAllString(errors.Unwrap(fault.Err).Error(), ""), position.ReplaceAllString(original.Error(), ""); got != want {
		t.Errorf("the converted entries are refused with\n%s\nwant cobol-go's refusal of the copybook\n%s", got, want)
	}

	if at := is[0].Spans[0]; at.File != file || at.Line == 0 {
		t.Errorf("the refusal is at %s, want a line of %s", at, file)
	}
}

// sameDescriptor resolves the project in dir, converts every copybook its
// layout names by conversion — into a definitions file, or a block — rewriting
// every record onto it, resolves it again and requires the two descriptors to
// be the same bytes.
func sameDescriptor(t *testing.T, dir, layout string, conversion func(*testing.T, string, string)) {
	t.Helper()

	path := filepath.Join(dir, manifest.Name)

	original, err := project.Load(path)
	if err != nil {
		t.Fatalf("the project does not resolve from its copybooks:\n%s", diag.Render(err))
	}

	conversion(t, dir, layout)

	converted, err := project.Load(path)
	if err != nil {
		t.Fatalf("the project does not resolve converted:\n%s", diag.Render(err))
	}

	if !bytes.Equal(marshal(t, original), marshal(t, converted)) {
		t.Error("the project resolves to different descriptors from its copybooks and converted")
	}
}

// convert writes a definitions file beside every copybook the layout at
// dir/layout names, with #401's writer, and rewrites each record's `copybook`
// source onto it. Each copybook is converted once, however many records name
// it.
func convert(t *testing.T, dir, layout string) {
	t.Helper()

	path := filepath.Join(dir, filepath.FromSlash(layout))

	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the layout: %v", err)
	}

	converted := make(map[string]bool)

	for _, match := range copybookSource.FindAllStringSubmatch(string(src), -1) {
		named := match[1]
		if converted[named] {
			continue
		}

		converted[named] = true

		copybookPath := filepath.Join(filepath.Dir(path), filepath.FromSlash(named))

		entries := parseCopybook(t, copybookPath)

		var body bytes.Buffer
		if err := definitions.Write(&body, entries, definitions.FileDepth); err != nil {
			t.Fatalf("converting %s: %v", named, err)
		}

		write(t, copybookPath+".sexpr", body.String())
	}

	if len(converted) == 0 {
		t.Fatalf("the layout names no copybook to convert:\n%s", src)
	}

	rewritten := copybookSource.ReplaceAllString(string(src), `(definitions "$1.sexpr"`)
	if strings.Contains(rewritten, `(copybook "`) {
		t.Fatalf("a record is still bound to a copybook:\n%s", rewritten)
	}

	write(t, path, rewritten)
}

// inline converts every copybook the layout at dir/layout names into a
// `definitions-block` holding its entries, written by #401's writer at block
// depth and added to the layout, and rewrites each record's `copybook` source
// to name the block. Each copybook is converted once, into one block, however
// many records name it — which is the "several records naming one block"
// docs/layout/SPEC.md's "What a record may reach" allows.
func inline(t *testing.T, dir, layout string) {
	t.Helper()

	path := filepath.Join(dir, filepath.FromSlash(layout))

	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the layout: %v", err)
	}

	blocks := make(map[string]string)

	var added strings.Builder

	for _, match := range copybookSource.FindAllStringSubmatch(string(src), -1) {
		named := match[1]
		if _, already := blocks[named]; already {
			continue
		}

		name := fmt.Sprintf("CONVERTED-%d", len(blocks)+1)
		blocks[named] = name

		entries := parseCopybook(t, filepath.Join(filepath.Dir(path), filepath.FromSlash(named)))

		var body bytes.Buffer
		if err := definitions.Write(&body, entries, definitions.BlockDepth); err != nil {
			t.Fatalf("converting %s: %v", named, err)
		}

		fmt.Fprintf(&added, "\n(definitions-block %s\n%s)\n", name, strings.TrimSuffix(body.String(), "\n"))
	}

	if len(blocks) == 0 {
		t.Fatalf("the layout names no copybook to convert:\n%s", src)
	}

	rewritten := copybookSource.ReplaceAllStringFunc(string(src), func(source string) string {
		return "(definitions " + blocks[copybookSource.FindStringSubmatch(source)[1]]
	})
	if strings.Contains(rewritten, `(copybook "`) {
		t.Fatalf("a record is still bound to a copybook:\n%s", rewritten)
	}

	write(t, path, rewritten+added.String())
}

// parseCopybook parses a copybook as a run reads one: a fragment, in fixed
// format.
func parseCopybook(t *testing.T, path string) []*cobol.DataDescriptionEntry {
	t.Helper()

	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	file, err := cobol.Parse(bytes.NewReader(src), cobol.WithFragment(), cobol.WithSourceFormat(cobol.FixedFormat))
	if err != nil || file.Fragment == nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	return file.Fragment.Entries
}

// copyProject copies the layouts, copybooks and manifest under dir into a
// directory of the test's own, keeping their relative paths, so that the
// conversion writes nothing into the repository.
func copyProject(t *testing.T, dir string) string {
	t.Helper()

	into := t.TempDir()

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		switch strings.ToLower(filepath.Ext(path)) {
		case ".sexpr", ".cpy", ".json":
		default:
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		target := filepath.Join(into, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		return os.WriteFile(target, src, 0o600)
	})
	if err != nil {
		t.Fatalf("copying %s: %v", dir, err)
	}

	return into
}

// marshal is a run's descriptor as `--emit-ir` writes it.
func marshal(t *testing.T, run *project.Run) []byte {
	t.Helper()

	encoded, err := emit.Marshal(run.Descriptor)
	if err != nil {
		t.Fatalf("encoding the descriptor: %v", err)
	}

	return encoded
}
