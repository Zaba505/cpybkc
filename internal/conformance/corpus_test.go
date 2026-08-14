// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package conformance

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
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
		// entry is the shipped entry the case breaks, and "" is the one entry
		// most of them break. A case names another where the rule it is about
		// needs an item no other entry carries — a float, whose form no entry
		// of ordinary items can be wrong about.
		entry  string
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

		// One case per rule of the value language, each of them a spelling of
		// the right value (#196). They are here rather than only beside the
		// grammars because what this asserts is that the loader reaches them:
		// a rule the walk never applies to an entry's values is a rule that
		// passes its own tests and refuses nothing.
		"a number carrying a leading zero": {
			breaks: func(t *testing.T, dir string) {
				rewriteValues(t, dir, `"42"`, `"042"`)
			},
			says: numberHasNoZero,
		},
		"a number written as a JSON number": {
			breaks: func(t *testing.T, dir string) {
				rewriteValues(t, dir, `"42"`, `42`)
			},
			says: scalarIsAString,
		},
		"a character item padded to its width": {
			breaks: func(t *testing.T, dir string) {
				rewriteValues(t, dir, `"A001"`, `"A001 "`)
			},
			says: textIsTrimmed,
		},
		"a float in a form the corpus does not write": {
			entry: "float-ieee754",
			breaks: func(t *testing.T, dir string) {
				rewriteValues(t, dir, `"0x1p+0"`, `"0x1P+0"`)
			},
			says: floatIsWritten,
		},
		"a run of bytes that is not base64": {
			breaks: func(t *testing.T, dir string) {
				// The corpus carries no INDEX, POINTER or NATIONAL item, so
				// the mutation is on the side that decides the form rather
				// than on the value: LINE-SKU becomes an INDEX item, and the
				// three characters the entry states for it stop being a
				// padded base64 quantum. An entry that carries one of the
				// three usages is the day this case states the value instead.
				asIndexItem(t, dir, "LINE-SKU")
			},
			says: bytesAreBase64,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			entry := test.entry
			if entry == "" {
				entry = "orders-fixed"
			}

			dir := entryCopy(t, entry)

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
// reserving a name means: the entry loads with it there, and loads to exactly
// what it loads to without it.
//
// The content is deliberately not a document. Nothing specifies what is in this
// member (#194), so an entry carrying JSON would pass whether the loader
// ignores the file, parses it, or holds it to some shape somebody added later —
// and only the third of those is a change to what was reserved. Bytes no reader
// could accept fail the day the member acquires a consumer without acquiring a
// specification first, which is the direction this reservation has to be
// defended in.
func TestTheReservedMemberIsAdmittedAndReadByNothing(t *testing.T) {
	dir := entryCopy(t, "orders-fixed")

	without, err := LoadEntry(dir)
	if err != nil {
		t.Fatalf("%v", err)
	}

	write(t, filepath.Join(dir, OffsetsName), "not a document {")

	with, err := LoadEntry(dir)
	if err != nil {
		t.Fatalf("an entry carrying the reserved %s: %v", OffsetsName, err)
	}

	if !slices.Equal(with.Copybooks, without.Copybooks) {
		t.Errorf("the entry's copybooks are %v with %s beside them and %v without it",
			with.Copybooks, OffsetsName, without.Copybooks)
	}

	if with.Layout != without.Layout || with.Description != without.Description ||
		with.Source != without.Source || !bytes.Equal(with.Input, without.Input) {
		t.Errorf("the entry loaded differently with %s beside it, and nothing reads it", OffsetsName)
	}
}

// TestNoShippedEntryCarriesTheReservedMember is the half of the reservation
// that decays quietly.
//
// The member has no content specified and no consumer, so an entry that started
// carrying one would carry a file whose meaning nothing had agreed — which is
// exactly what reserving the name postpones. It is a test of its own because it
// is a claim about the corpus rather than about the loader, and a failure here
// sends its reader somewhere else entirely.
func TestNoShippedEntryCarriesTheReservedMember(t *testing.T) {
	entries, err := Load(CorpusPath(repoRoot(t)))
	if err != nil {
		t.Fatalf("%v", err)
	}

	for _, entry := range entries {
		switch _, err := os.Stat(filepath.Join(entry.Dir, OffsetsName)); {
		case err == nil:
			t.Errorf("%s carries %s, and the member is reserved with nothing specified to be in it",
				entry.Name, OffsetsName)
		case !os.IsNotExist(err):
			t.Errorf("%s: %v", entry.Name, err)
		}
	}
}

// rewriteValues is one thing done to an entry's values document: the first
// occurrence of was, written as is.
//
// It fails where the replacement did not apply, which is what keeps a case
// about the rule rather than about the fixture — an entry edited until the
// mutation no longer matches would otherwise load, and the case would pass by
// asserting nothing.
func rewriteValues(t *testing.T, dir, was, is string) {
	t.Helper()

	path := filepath.Join(dir, ValuesName)

	values := read(t, path)

	rewritten := strings.Replace(values, was, is, 1)
	if rewritten == values {
		t.Fatalf("%s does not carry %s, and the case is about rewriting it", ValuesName, was)
	}

	write(t, path, rewritten)
}

// asIndexItem rewrites the named item's usage to USAGE_INDEX, which is one of
// the three usages the value language writes as base64.
//
// The usage is written ahead of the names inside a field, so the one to rewrite
// is the last one before the name — which is what makes this a change to the
// named item and not to whichever item came first. The rendering stays
// canonical, because only an enum's spelling changed and nothing about the
// shape of the document did.
func asIndexItem(t *testing.T, dir, name string) {
	t.Helper()

	const was = `"usage": "USAGE_DISPLAY"`

	path := filepath.Join(dir, DescriptorName)

	descriptor := read(t, path)

	named := strings.Index(descriptor, `"original": "`+name+`"`)
	if named < 0 {
		t.Fatalf("%s carries no item named %s", DescriptorName, name)
	}

	at := strings.LastIndex(descriptor[:named], was)
	if at < 0 {
		t.Fatalf("%s states no %s ahead of %s", DescriptorName, was, name)
	}

	write(t, path, descriptor[:at]+`"usage": "USAGE_INDEX"`+descriptor[at+len(was):])
}

// entryCopy is a copy of a shipped entry, in a directory of the test's own, so
// that a case may break it without breaking the corpus.
func entryCopy(t *testing.T, entry string) string {
	t.Helper()

	source := filepath.Join(CorpusPath(repoRoot(t)), entry)

	dir := filepath.Join(t.TempDir(), entry)
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
