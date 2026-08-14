// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package conformance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/layoutschema"
)

// TestTheShippedCorpusLoads holds the corpus this repository ships to its own
// format.
//
// It is the check that the corpus is a corpus rather than a directory of files:
// every entry parses, every descriptor validates, every values document names
// records that exist, and no entry carries a file the format has no place for.
// An author adding an entry finds out here rather than from a generator failing
// to be invoked.
func TestTheShippedCorpusLoads(t *testing.T) {
	entries, err := Load(CorpusPath(repoRoot(t)))
	if err != nil {
		t.Fatalf("%v", err)
	}

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			switch {
			case entry.Description == "":
				t.Error("the entry says nothing about what it is for")
			case entry.Source == "":
				t.Error("the entry cites no source for the answer it expects")
			case entry.Descriptor == nil:
				t.Error("the entry carries no descriptor")
			case entry.Values == nil:
				t.Error("the entry carries no values")
			case len(entry.Copybooks) == 0:
				t.Error("the entry carries no copybook")
			}
		})
	}
}

// TestEveryEntryLayoutIsWellFormed holds each entry's layout to the published
// schema.
//
// The layout is the one member of the tuple nothing else in this repository
// reads yet — resolving it against its copybooks is the pipeline the CLI carries
// (#148) — so without this check an entry could ship a layout that is not a
// layout at all and nothing would say so until that pipeline arrived.
//
// What it asserts is what the schema can decide: an unknown form, a missing
// child, a value outside a closed set. Whether the layout means what the entry's
// descriptor says it means is the claim the corpus makes and cannot check from
// here.
func TestEveryEntryLayoutIsWellFormed(t *testing.T) {
	root := repoRoot(t)

	published, err := os.Open(layoutschema.SchemaPath(root))
	if err != nil {
		t.Fatalf("the published schema: %v", err)
	}

	defer func() { _ = published.Close() }()

	schema, err := layoutschema.Load(published)
	if err != nil {
		t.Fatalf("the published schema: %v", err)
	}

	entries, err := Load(CorpusPath(root))
	if err != nil {
		t.Fatalf("%v", err)
	}

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			layout, err := os.Open(entry.Layout)
			if err != nil {
				t.Fatalf("%v", err)
			}

			defer func() { _ = layout.Close() }()

			diagnostics, err := schema.Validate(entry.Layout, layout)
			if err != nil {
				t.Fatalf("%v", err)
			}

			for _, diagnostic := range diagnostics {
				t.Error(diagnostic.String())
			}
		})
	}
}

// TestAnEntryTheFormatRefuses walks every way an entry can be wrong that the
// loader is in a position to see.
//
// Each case is the shipped entry with one thing done to it, so that what is
// being asserted is the rule rather than a fixture: an entry that stopped being
// refused because the mutation stopped applying would fail here on the mutation
// rather than pass quietly.
func TestAnEntryTheFormatRefuses(t *testing.T) {
	tests := map[string]struct {
		breaks func(t *testing.T, dir string)
		says   string
	}{
		"a file the format has no place for": {
			breaks: func(t *testing.T, dir string) {
				write(t, filepath.Join(dir, "notes.txt"), "read me")
			},
			says: "notes.txt is not a member of an entry",
		},
		"a member that is not there": {
			breaks: func(t *testing.T, dir string) {
				remove(t, filepath.Join(dir, ValuesName))
			},
			says: ValuesName + " is missing",
		},
		"no copybook at all": {
			breaks: func(t *testing.T, dir string) {
				remove(t, filepath.Join(dir, "orders.cpy"))
			},
			says: "carries no .cpy copybook",
		},
		"a directory inside the entry": {
			breaks: func(t *testing.T, dir string) {
				if err := os.Mkdir(filepath.Join(dir, "vectors"), 0o755); err != nil {
					t.Fatalf("%v", err)
				}
			},
			says: "vectors is a directory",
		},
		"metadata carrying a field nobody reads": {
			breaks: func(t *testing.T, dir string) {
				write(t, filepath.Join(dir, MetadataName), `{"description": "a", "source": "b", "notes": "c"}`)
			},
			says: "unknown field",
		},
		"metadata with a second document behind it": {
			breaks: func(t *testing.T, dir string) {
				write(t, filepath.Join(dir, MetadataName),
					`{"description": "a", "source": "b"}{"description": "c", "source": "d"}`)
			},
			says: "more than one document",
		},
		"metadata citing no source": {
			breaks: func(t *testing.T, dir string) {
				write(t, filepath.Join(dir, MetadataName), `{"description": "a", "source": ""}`)
			},
			says: "source cites the section",
		},
		"a descriptor that is not the canonical rendering": {
			breaks: func(t *testing.T, dir string) {
				write(t, filepath.Join(dir, DescriptorName), `{"version":"IR_VERSION_1","nodes":[`+
					`{"file":{"unframed":{},"start_state_id":"1"}},`+
					`{"id":"1","state":{"accepts":true}}]}`)
			},
			says: "is not the canonical rendering",
		},
		"a descriptor that does not validate": {
			breaks: func(t *testing.T, dir string) {
				descriptor := read(t, filepath.Join(dir, DescriptorName))
				write(t, filepath.Join(dir, DescriptorName),
					strings.Replace(descriptor, `"start_state_id": "7"`, `"start_state_id": "70"`, 1))
			},
			says: DescriptorName,
		},
		"values naming a record the descriptor does not carry": {
			breaks: func(t *testing.T, dir string) {
				values := read(t, filepath.Join(dir, ValuesName))
				write(t, filepath.Join(dir, ValuesName),
					strings.Replace(values, `"ORDER-RECORD"`, `"ORDERS-RECORD"`, 1))
			},
			says: "is not a record the descriptor carries",
		},
		"values carrying a field nobody reads": {
			breaks: func(t *testing.T, dir string) {
				write(t, filepath.Join(dir, ValuesName), `{"records": [], "note": "why"}`)
			},
			says: "unknown field",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			dir := entryCopy(t)

			test.breaks(t, dir)

			_, err := LoadEntry(dir)
			if err == nil {
				t.Fatal("the entry was loaded, and the format does not admit it")
			}

			if !strings.Contains(err.Error(), test.says) {
				t.Errorf("the refusal is %q, and it does not say %q", err, test.says)
			}
		})
	}
}

// TestTheReservedMemberIsAdmittedAndReadByNothing holds [OffsetsName] to what
// reserving a name means.
//
// Two halves, and the second is the one that decays quietly. An entry carrying
// the member loads, which is what makes the name reserved rather than merely
// documented — the loader refuses every other file it has no place for, so
// without this the reservation would be a sentence nobody could act on. And no
// entry in the shipped corpus carries one, because the member has no content
// specified and no consumer (#194): an entry that started carrying one would be
// carrying a file whose meaning nothing had agreed, which is the thing the
// reservation exists to postpone.
func TestTheReservedMemberIsAdmittedAndReadByNothing(t *testing.T) {
	dir := entryCopy(t)

	write(t, filepath.Join(dir, OffsetsName), `{"reserved": true}`)

	entry, err := LoadEntry(dir)
	if err != nil {
		t.Fatalf("an entry carrying the reserved %s: %v", OffsetsName, err)
	}

	for _, copybook := range entry.Copybooks {
		if filepath.Base(copybook) == OffsetsName {
			t.Errorf("%s was read as a copybook, and nothing reads it", OffsetsName)
		}
	}

	entries, err := Load(CorpusPath(repoRoot(t)))
	if err != nil {
		t.Fatalf("%v", err)
	}

	for _, shipped := range entries {
		if _, err := os.Stat(filepath.Join(shipped.Dir, OffsetsName)); err == nil {
			t.Errorf("%s carries %s, and the member is reserved with nothing specified to be in it",
				shipped.Name, OffsetsName)
		}
	}
}

// entryCopy is a copy of the shipped entry, in a directory of the test's own, so
// that a case may break it without breaking the corpus.
func entryCopy(t *testing.T) string {
	t.Helper()

	source := filepath.Join(CorpusPath(repoRoot(t)), "orders-fixed")

	dir := filepath.Join(t.TempDir(), "orders-fixed")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("%v", err)
	}

	listing, err := os.ReadDir(source)
	if err != nil {
		t.Fatalf("%v", err)
	}

	for _, item := range listing {
		b, err := os.ReadFile(filepath.Join(source, item.Name()))
		if err != nil {
			t.Fatalf("%v", err)
		}

		if err := os.WriteFile(filepath.Join(dir, item.Name()), b, 0o600); err != nil {
			t.Fatalf("%v", err)
		}
	}

	return dir
}

func write(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("%v", err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v", err)
	}

	return string(b)
}

func remove(t *testing.T, path string) {
	t.Helper()

	if err := os.Remove(path); err != nil {
		t.Fatalf("%v", err)
	}
}

// repoRoot walks up from the test's working directory to the directory holding
// go.mod, which for this module is the repository root and so the directory the
// corpus sits in.
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
