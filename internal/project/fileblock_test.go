// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package project

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	cobol "github.com/Zaba505/cobol-go"
	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/definitions"
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// repositoryRoot is the module root, from this package's directory.
const repositoryRoot = "../.."

// copybookRoots are the directories every copybook this repository holds is
// under — the ones #401's round trip walks.
var copybookRoots = []string{
	"testdata/conformance",
	"example",
	"internal/conformance/goadapter/testdata",
}

// trackedCopybooks is how many copybooks the walk found when #409 was filed. It
// is a floor, so that a copybook added later is simply one more compared, and
// it is there so that a walk that broke and found nothing fails rather than
// passing over an empty set.
const trackedCopybooks = 78

// TestAFileAndABlockHoldingItsEntriesReadAlike holds the two halves of a run's
// reading of an entry body to each other, over every copybook the repository
// holds that cpybkc can read: the copybook's entries, written as a definitions
// file and read the way a record naming the file reads it, and the same entries
// written as a block's body and read the way a record naming the block reads
// it, are the same entries once their positions are set aside — which are the
// only thing that may differ, since one is in the file and the other in the
// layout.
//
// #401's round trip holds each to the copybook through the package's own
// reader. This is the run's path: [readDefinitions] for the file, and
// [layoutmodel.ReadBlocks] and [judgeBlock] for the block.
func TestAFileAndABlockHoldingItsEntriesReadAlike(t *testing.T) {
	t.Parallel()

	found := 0

	for _, root := range copybookRoots {
		err := filepath.WalkDir(filepath.Join(repositoryRoot, root), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".cpy") {
				return err
			}

			found++

			name, _ := filepath.Rel(repositoryRoot, path)

			entries, ok := readableCopybook(path)
			if !ok {
				t.Logf("%s is not a copybook cpybkc can read today, so it is not compared", name)

				return nil
			}

			t.Run(filepath.ToSlash(name), func(t *testing.T) {
				t.Parallel()

				fromFile := fileEntries(t, entries)
				fromBlock := blockEntries(t, entries)

				if len(fromFile) != len(fromBlock) {
					t.Fatalf("the file reads as %d entries and the block as %d", len(fromFile), len(fromBlock))
				}

				for i := range fromFile {
					if file, block := definitions.Cleared(fromFile[i]), definitions.Cleared(fromBlock[i]); !reflect.DeepEqual(file, block) {
						t.Errorf("entry %d reads as\n%#v\nfrom the file and\n%#v\nfrom the block", i+1, file, block)
					}
				}
			})

			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}

	if found < trackedCopybooks {
		t.Fatalf("the walk found %d copybooks, fewer than the %d tracked when #409 was filed", found, trackedCopybooks)
	}
}

// fileEntries writes entries as a definitions file and reads it as a run does.
func fileEntries(t *testing.T, entries []*cobol.DataDescriptionEntry) []*cobol.DataDescriptionEntry {
	t.Helper()

	var body bytes.Buffer
	if err := definitions.Write(&body, entries, definitions.FileDepth); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	path := filepath.Join(t.TempDir(), "converted.cpy.sexpr")
	if err := os.WriteFile(path, body.Bytes(), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	read := readDefinitions(path, "converted.cpy.sexpr")
	if read.missing != nil || read.empty || read.entries == nil {
		t.Fatalf("the file does not read:\n%s\n%s", body.String(), diag.Render(joined(read.faults)))
	}

	return read.entries
}

// blockEntries writes entries as a block's body, in a layout holding nothing
// else, and reads the block as a run does.
func blockEntries(t *testing.T, entries []*cobol.DataDescriptionEntry) []*cobol.DataDescriptionEntry {
	t.Helper()

	var body bytes.Buffer
	if err := definitions.Write(&body, entries, definitions.BlockDepth); err != nil {
		t.Fatalf("writing the block: %v", err)
	}

	src := "(definitions-block CONVERTED\n" + strings.TrimSuffix(body.String(), "\n") + ")\n"

	file, err := layout.Parse("layout.sexpr", strings.NewReader(src))
	if err != nil {
		t.Fatalf("parsing the block:\n%s", diag.Render(err))
	}

	blocks, err := layoutmodel.ReadBlocks(file)
	if err != nil || len(blocks) != 1 {
		t.Fatalf("reading the block: %v", err)
	}

	built, faults := judgeBlock(blocks[0])
	if built == nil {
		t.Fatalf("the block does not read:\n%s\n%s", src, diag.Render(joined(faults)))
	}

	return built
}

// readableCopybook parses a copybook as a run reads one, and reports whether it
// is one cpybkc can read: it parses as a fragment in fixed format, declares an
// entry, and builds.
func readableCopybook(path string) ([]*cobol.DataDescriptionEntry, bool) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	file, err := cobol.Parse(bytes.NewReader(src), cobol.WithFragment(), cobol.WithSourceFormat(cobol.FixedFormat))
	if err != nil || file.Fragment == nil || len(file.Fragment.Entries) == 0 {
		return nil, false
	}

	if _, err := copybook.Build(file.Fragment.Entries); err != nil {
		return nil, false
	}

	return file.Fragment.Entries, true
}

// joined is faults as one error, for rendering.
func joined(faults []error) error {
	var list diag.List
	for _, fault := range faults {
		list.Fail(fault)
	}

	return list.Err()
}
