// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package definitions_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cobol "github.com/Zaba505/cobol-go"
	"github.com/Zaba505/cobol-go/copybook"

	"github.com/Zaba505/cpybkc/internal/copymember"
	"github.com/Zaba505/cpybkc/internal/layout"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// repositoryRoot is the module root, from this package's directory.
const repositoryRoot = "../.."

// copybookRoots are the directories the round trip walks for copybooks: every
// one this repository holds is under one of them.
var copybookRoots = []string{
	"testdata/conformance",
	"example",
	"internal/conformance/goadapter/testdata",
}

// trackedCopybooks is how many copybooks the walk found when #401 was filed. It
// is a floor rather than an equality, so that a fixture added later is simply
// one more converted; it is there so that a walk that broke and found nothing
// fails instead of passing over an empty set.
const trackedCopybooks = 78

// kitchenSink is the fixed-format copybook covering every production of
// `cobol-go`'s data-description grammar at the pinned commit.
const kitchenSink = "testdata/kitchen-sink.cpy"

// fixture is one copybook the converter is held to, parsed.
type fixture struct {
	// Name is its path from the repository root, or the kitchen sink's from
	// this package.
	Name string

	// Entries are what `cobol-go` parsed, in fixed format.
	Entries []*cobol.DataDescriptionEntry
}

// fixtures is every copybook cpybkc can read today, by docs/layout/SPEC.md's
// definition — one `cobol.Parse(WithFragment(), WithSourceFormat(FixedFormat))`
// and `copybook.Build` both accept and that declares at least one entry — found
// by walking the tree, and the kitchen sink.
//
// A walked copybook outside that definition is not a failure: a conformance
// case may hold a copybook that is refused on purpose, and SPEC.md promises
// conversion only for the ones a run reads. The kitchen sink is exempt from
// that check, and from `copybook.Build` in particular, since it exists to cover
// the grammar rather than to describe a record; it must still parse, and it
// goes through every test a walked copybook does.
func fixtures(t *testing.T) []fixture {
	t.Helper()

	var (
		all   []fixture
		found int
	)

	for _, root := range copybookRoots {
		inRoot := 0

		err := filepath.WalkDir(filepath.Join(repositoryRoot, root), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".cpy") {
				return err
			}

			found++
			inRoot++

			name, _ := filepath.Rel(repositoryRoot, path)

			entries, ok := readable(t, path)
			if !ok {
				t.Logf("%s is not a copybook cpybkc can read today, so it is not converted", name)

				return nil
			}

			all = append(all, fixture{Name: filepath.ToSlash(name), Entries: entries})

			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}

		if inRoot == 0 {
			t.Fatalf("the walk found no copybook under %s", root)
		}
	}

	if found < trackedCopybooks {
		t.Fatalf("the walk found %d copybooks, fewer than the %d tracked when #401 was filed", found, trackedCopybooks)
	}

	sink, err := parseFixed(kitchenSink)
	if err != nil {
		t.Fatalf("parse the kitchen sink: %v", err)
	}

	all = append(all, fixture{Name: kitchenSink, Entries: sink})

	return append(all, composedFixtures(t)...)
}

// composedProject is the project whose copybooks compose with COPY: a shared
// header member, a per-type body member, a COPY nested in it and a COPY … OF a
// library, every member found where the layout's copy-member forms say.
const composedProject = "testdata/composed"

// composedFixtures are the composed project's copybooks, each parsed as a run
// reads it: expanded through the one resolver over its layout's copy-member
// forms, so that the entries are the expansion a definitions file holds.
func composedFixtures(t *testing.T) []fixture {
	t.Helper()

	dir := filepath.Join(repositoryRoot, composedProject)
	path := filepath.Join(dir, "orders.sexpr")

	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the composed layout: %v", err)
	}

	file, err := layout.Parse(path, bytes.NewReader(src))
	if err != nil {
		t.Fatalf("parsing the composed layout: %v", err)
	}

	members, err := layoutmodel.ReadCopyMembers(file)
	if err != nil {
		t.Fatalf("reading the composed layout's copy members: %v", err)
	}

	forms := make([]copymember.Form, 0, len(members))
	for _, member := range members {
		forms = append(forms, copymember.Form{Member: member.Member, Path: member.Path, File: filepath.Join(dir, filepath.FromSlash(member.Path))})
	}

	resolver := copymember.New(forms)

	var composed []fixture

	for _, copybook := range []string{"cpy/order-header.cpy", "cpy/order-detail.cpy"} {
		src, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(copybook)))
		if err != nil {
			t.Fatalf("reading %s: %v", copybook, err)
		}

		parsed, err := cobol.Parse(bytes.NewReader(src), cobol.WithFragment(), cobol.WithSourceFormat(cobol.FixedFormat), cobol.WithCopyBooks(resolver))
		if err != nil {
			t.Fatalf("parsing %s through its layout's members: %v", copybook, err)
		}

		composed = append(composed, fixture{Name: composedProject + "/" + copybook, Entries: parsed.Fragment.Entries})
	}

	for i, member := range members {
		if !resolver.Reached(i) {
			t.Fatalf("no COPY reaches %s; the composed fixture no longer covers it", member.Member)
		}
	}

	return composed
}

// readable parses a copybook and reports whether it is one cpybkc can read.
func readable(t *testing.T, path string) ([]*cobol.DataDescriptionEntry, bool) {
	t.Helper()

	entries, err := parseFixed(path)
	if err != nil || len(entries) == 0 {
		return nil, false
	}

	if _, err := copybook.Build(entries); err != nil {
		return nil, false
	}

	return entries, true
}

// parseFixed parses a copybook as a run reads one: a fragment, in fixed
// format.
func parseFixed(path string) ([]*cobol.DataDescriptionEntry, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	file, err := cobol.Parse(bytes.NewReader(src), cobol.WithFragment(), cobol.WithSourceFormat(cobol.FixedFormat))
	if err != nil {
		return nil, err
	}

	if file.Fragment == nil {
		return nil, nil
	}

	return file.Fragment.Entries, nil
}

// parseFree parses a copybook fragment held in a string, in free format.
func parseFree(t *testing.T, source string) []*cobol.DataDescriptionEntry {
	t.Helper()

	file, err := cobol.Parse(strings.NewReader(source), cobol.WithFragment())
	if err != nil {
		t.Fatalf("parse the copybook: %v", err)
	}

	return file.Fragment.Entries
}
