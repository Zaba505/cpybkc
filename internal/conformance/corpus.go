// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Zaba505/cpybkc/internal/assemble"
	"github.com/Zaba505/cpybkc/internal/emit"
	"github.com/Zaba505/cpybkc/irpb"
)

// CorpusFile is where the corpus lives, relative to the repository root, in the
// slash-separated spelling a repository path is written in.
//
// It is a constant here for the reason
// [github.com/Zaba505/cpybkc/internal/layoutschema.SchemaFile] is one: the
// tests, the runners and anything else that means "the corpus" have to mean one
// directory, and a path written out in three places is a path two of them get
// wrong.
const CorpusFile = "testdata/conformance"

// The names an entry's members are written under. They are fixed rather than
// declared in entry.json because a tuple whose members could be anywhere is a
// tuple every reader has to be told about, and a third-party runner reading the
// corpus should be able to open a file without parsing something first.
const (
	// MetadataName carries what an entry is and where it came from.
	MetadataName = "entry.json"

	// LayoutName is the layout the entry's file is laid out by.
	LayoutName = "layout.sexpr"

	// DescriptorName is the IR the layout and its copybooks resolve to, in the
	// canonical JSON rendering --emit-ir-format json writes.
	DescriptorName = "ir.json"

	// InputName is the bytes of one file laid out that way.
	InputName = "input.bin"

	// ValuesName is what those bytes decode to.
	ValuesName = "values.json"

	// CopybookExt is the extension every copybook of an entry carries. The
	// layout names them; what this decides is only which files in the directory
	// are allowed to be there.
	CopybookExt = ".cpy"

	// readmeName is the corpus's own documentation, which sits beside the
	// entries rather than under docs/ (docs/CONVENTIONS.md, "What belongs
	// here"). It is the one file at the top of the corpus that is not an entry.
	readmeName = "README.md"
)

// CorpusPath is [CorpusFile] under root, in the host's own path spelling.
func CorpusPath(root string) string {
	return filepath.Join(root, filepath.FromSlash(CorpusFile))
}

// Entry is one corpus entry: the tuple, loaded, and the metadata saying what it
// is for.
//
// The layout and the copybooks are paths rather than parsed values. Nothing
// here resolves a copybook — that needs the pipeline the CLI carries (#148) —
// and a member this package parsed but could not check would claim a coverage
// the loader does not have.
type Entry struct {
	// Name is the entry's directory name, which is what every diagnostic about
	// it names and what a failing run reports.
	Name string

	// Dir is the directory the entry was read from.
	Dir string

	// Description is the one line entry.json carries: what shape of file this
	// entry is about.
	Description string

	// Source is the section of a specification the entry was derived from,
	// exactly as entry.json spells it. It is carried so that a failure names
	// where the expected answer came from as well as which entry disagreed
	// with it (#68).
	Source string

	// Layout is the path to the entry's layout file.
	Layout string

	// Copybooks are the paths to the copybooks in the entry's directory, sorted
	// by name. Which of them the layout names is the layout's business; what is
	// asserted here is only that an entry carries at least one.
	Copybooks []string

	// Descriptor is the IR the entry expects the layout and its copybooks to
	// resolve to, and the IR a runner's generator is handed.
	Descriptor *irpb.Descriptor

	// Input is the bytes of the file the entry describes.
	Input []byte

	// Values is what those bytes decode to.
	Values *Values
}

// metadata is entry.json as it is written.
type metadata struct {
	Description string `json:"description"`
	Source      string `json:"source"`
}

// Load reads every entry of the corpus rooted at dir, in ascending name order.
//
// Every entry is loaded, and every entry that could not be is reported, rather
// than the first: a corpus with three bad entries is one editing session, not
// three.
//
// A corpus holding no entry is an error. An empty directory and a corpus that
// failed to be found are the same thing to a caller about to report that
// everything passed, and only one of them is honest.
func Load(dir string) ([]*Entry, error) {
	listing, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read the conformance corpus: %w", err)
	}

	var (
		entries []*Entry
		faults  []error
	)

	for _, item := range listing {
		if !item.IsDir() {
			// The README is the format's documentation and lives beside the
			// entries; anything else at this level is a file somebody put in
			// the corpus rather than in an entry.
			if item.Name() == readmeName {
				continue
			}

			faults = append(faults, fmt.Errorf("%s is not an entry directory, and the corpus holds nothing else but %s",
				item.Name(), readmeName))

			continue
		}

		entry, err := LoadEntry(filepath.Join(dir, item.Name()))
		if err != nil {
			faults = append(faults, err)

			continue
		}

		entries = append(entries, entry)
	}

	if len(faults) > 0 {
		return nil, joined(faults)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("the conformance corpus at %s holds no entry", dir)
	}

	return entries, nil
}

// LoadEntry reads one entry from its directory.
//
// Everything wrong with the entry is reported at once. An entry is written by
// hand, so it goes wrong the way a layout does — in several places in one
// sitting — and a loader stopping at the first fault is a loader run once per
// fault.
func LoadEntry(dir string) (*Entry, error) {
	entry := &Entry{Name: filepath.Base(dir), Dir: dir}

	listing, err := os.ReadDir(dir)
	if err != nil {
		return nil, &EntryError{Entry: entry.Name, Err: err}
	}

	var faults []error

	fault := func(err error) {
		if err != nil {
			faults = append(faults, err)
		}
	}

	fault(entry.readListing(listing))
	fault(entry.readMetadata())
	fault(entry.readDescriptor())
	fault(entry.readInput())
	fault(entry.readValues())

	if entry.Descriptor != nil && entry.Values != nil {
		fault(entry.Values.check(entry.Descriptor))
	}

	if len(faults) > 0 {
		return nil, &EntryError{Entry: entry.Name, Err: joined(faults)}
	}

	return entry, nil
}

// readListing holds the directory to the set of files an entry is made of: the
// five members, and one or more copybooks.
//
// A file the format has no place for is a fault rather than something ignored,
// which is the rule README.md's "The project manifest" already states about an
// unknown field: a file added to an entry in the expectation that something
// reads it is worse than one whose author is told nothing reads it.
func (e *Entry) readListing(listing []os.DirEntry) error {
	var faults []error

	for _, item := range listing {
		name := item.Name()

		if item.IsDir() {
			faults = append(faults, fmt.Errorf("%s is a directory, and an entry is a flat set of files", name))

			continue
		}

		switch {
		case name == MetadataName, name == LayoutName, name == DescriptorName,
			name == InputName, name == ValuesName:
		case strings.HasSuffix(name, CopybookExt):
			e.Copybooks = append(e.Copybooks, filepath.Join(e.Dir, name))
		default:
			faults = append(faults, fmt.Errorf("%s is not a member of an entry and not a %s copybook", name, CopybookExt))
		}
	}

	slices.Sort(e.Copybooks)

	for _, name := range []string{MetadataName, LayoutName, DescriptorName, InputName, ValuesName} {
		if !slices.ContainsFunc(listing, func(item os.DirEntry) bool {
			return !item.IsDir() && item.Name() == name
		}) {
			faults = append(faults, fmt.Errorf("%s is missing", name))
		}
	}

	if len(e.Copybooks) == 0 {
		faults = append(faults, fmt.Errorf("the entry carries no %s copybook", CopybookExt))
	}

	e.Layout = filepath.Join(e.Dir, LayoutName)

	return joined(faults)
}

// readMetadata reads entry.json.
//
// An unknown field is refused, for the reason the project manifest refuses one:
// a key somebody wrote expecting it to mean something is a typo they want told
// about rather than a line that reads as metadata and does nothing.
func (e *Entry) readMetadata() error {
	b, err := os.ReadFile(filepath.Join(e.Dir, MetadataName))
	if err != nil {
		return skipMissing(err)
	}

	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()

	var read metadata
	if err := decoder.Decode(&read); err != nil {
		return fmt.Errorf("%s: %w", MetadataName, err)
	}

	var faults []error

	if read.Description == "" {
		faults = append(faults, fmt.Errorf("%s: description says what the entry is about, and is required", MetadataName))
	}

	if read.Source == "" {
		faults = append(faults, fmt.Errorf("%s: source cites the section the expected answer came from, and is required", MetadataName))
	}

	e.Description = read.Description
	e.Source = read.Source

	return joined(faults)
}

// readDescriptor reads ir.json.
//
// Two things are asserted beyond it parsing. The descriptor passes
// [github.com/Zaba505/cpybkc/internal/assemble.Validate], because an entry
// carrying a descriptor no generator would accept tests nothing; and the file
// is byte for byte the canonical rendering of what it decodes to, because a
// hand-authored entry is reviewed as a diff and a rendering that varied would
// make every review of one a review of its whitespace.
func (e *Entry) readDescriptor() error {
	b, err := os.ReadFile(filepath.Join(e.Dir, DescriptorName))
	if err != nil {
		return skipMissing(err)
	}

	var descriptor irpb.Descriptor
	if err := protojson.Unmarshal(b, &descriptor); err != nil {
		return fmt.Errorf("%s: failed to read the descriptor: %w", DescriptorName, err)
	}

	if err := assemble.Validate(&descriptor); err != nil {
		return fmt.Errorf("%s: %w", DescriptorName, err)
	}

	canonical, err := emit.MarshalJSON(&descriptor)
	if err != nil {
		return fmt.Errorf("%s: %w", DescriptorName, err)
	}

	if !bytes.Equal(b, canonical) {
		return fmt.Errorf("%s is not the canonical rendering of the descriptor it carries; write what --emit-ir --emit-ir-format json writes", DescriptorName)
	}

	e.Descriptor = &descriptor

	return nil
}

// readInput reads input.bin.
//
// A file of no bytes is admitted: an empty file is a file, and a layout whose
// sequencing expression accepts nothing is exactly what one entry ought to be
// about.
func (e *Entry) readInput() error {
	b, err := os.ReadFile(filepath.Join(e.Dir, InputName))
	if err != nil {
		return skipMissing(err)
	}

	e.Input = b

	return nil
}

// readValues reads values.json.
func (e *Entry) readValues() error {
	b, err := os.ReadFile(filepath.Join(e.Dir, ValuesName))
	if err != nil {
		return skipMissing(err)
	}

	values, err := ParseValues(b)
	if err != nil {
		return fmt.Errorf("%s: %w", ValuesName, err)
	}

	e.Values = values

	return nil
}

// skipMissing drops the error of a member that is not there, because
// [Entry.readListing] has already reported it by name. Reporting it twice would
// tell an author that one missing file is two faults.
func skipMissing(err error) error {
	if os.IsNotExist(err) {
		return nil
	}

	return err
}
