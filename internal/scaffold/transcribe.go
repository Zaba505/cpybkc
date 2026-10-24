// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package scaffold

import (
	"bytes"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	cobol "github.com/Zaba505/cobol-go"

	"github.com/Zaba505/cpybkc/internal/copymember"
	"github.com/Zaba505/cpybkc/internal/definitions"
	"github.com/Zaba505/cpybkc/internal/layout"
)

// definitionsSuffix is what a definitions file's name adds to its copybook's.
const definitionsSuffix = ".sexpr"

// DefinitionsName is the name of the definitions file converted from the
// copybook a --copybook value names: the value's final path element, as typed,
// with `.sexpr` appended (docs/cli/SPEC.md, "The vector `init` takes").
//
// `cpy/posting.cpy` gives `posting.cpy.sexpr` and `vendor/PXTRACT` gives
// `PXTRACT.sexpr`. Nothing beyond the suffix is cpybkc's choice — the extension
// the copybook carried, or the absence of one, is kept — so the adopter can tell
// which copybook a definitions file came from by reading its name, and the name
// is the same on every run over the same command line.
func DefinitionsName(copybook string) string {
	return filepath.Base(copybook) + definitionsSuffix
}

// File is one definitions file a scaffold carries: the name it is written under,
// beside the layout, and its bytes.
type File struct {
	// Name is [DefinitionsName] of the copybook it was converted from.
	Name string

	// Bytes are the whole file.
	Bytes []byte
}

// transcribe writes a copybook's entries as a definitions file and reads the
// result back, so that what is returned is a file already known to say what the
// copybook says.
//
// The file is every entry and every clause of the copybook, in source order and
// in the canonical printed form, under one comment naming the copybook as it was
// typed (docs/cli/SPEC.md, "What the scaffold states, form by form"). That
// includes every entry no record derives from — a 77-level, an unnamed 01-level,
// an 01-level every combination of which was noted — because the file is the
// copybook's transcription and not the layout's selection from it.
//
// A composed copybook's entries are its expansion, each member's entries where
// its `COPY` stood, so the file holds that expansion and is read back against
// it; members is what a mismatch in a member's entry is pointed into it with.
func transcribe(book Copybook, entries []*cobol.DataDescriptionEntry, members *copymember.Resolver) ([]byte, error) {
	var out bytes.Buffer

	out.WriteString(headerComment(book.Path))

	if err := definitions.Write(&out, entries, definitions.FileDepth); err != nil {
		return nil, &TranscriptionError{Path: book.Path, Err: err}
	}

	if err := readBack(book, entries, out.Bytes(), members); err != nil {
		return nil, err
	}

	return out.Bytes(), nil
}

// headerComment is the one comment a definitions file carries: the copybook it was
// converted from, as it was typed.
//
// A `;` comment, which nothing reads (docs/layout/SPEC.md, "What a transcription
// leaves out"), and not the header *form* a definitions file never has — so
// moving the file's entries into a block leaves it behind with nothing lost. The
// path is quoted with the grammar's own escapes, which keeps a path holding a
// newline on the one line a comment has.
func headerComment(path string) string {
	return "; The entries of the copybook " + quote(path) + ", transcribed by cpybkc init.\n"
}

// readBack requires text to read back, through the reader a run uses, as the
// entries the copybook was parsed into: positions and comments cleared, and an
// omitted name counted as FILLER — the one difference docs/layout/SPEC.md's
// "What a transcription leaves out" declares unobservable.
//
// It is the judge's comparison (docs/layout/SPEC.md, "cobol-go judges an entry,
// and nothing here says what one means"), applied to the bytes about to be
// written rather than to an earlier rendering of them, so that nothing written
// was not read.
func readBack(book Copybook, entries []*cobol.DataDescriptionEntry, text []byte, members *copymember.Resolver) error {
	file, err := layout.Parse(DefinitionsName(book.Path), bytes.NewReader(text))
	if err != nil {
		return &ReadBackError{Path: book.Path, Err: err}
	}

	read, err := definitions.Read(definitions.FileBody(file))
	if err != nil {
		return &ReadBackError{Path: book.Path, Err: err}
	}

	built, err := definitions.Judge(read)
	if err != nil {
		return &ReadBackError{Path: book.Path, Err: err}
	}

	if len(built) != len(entries) {
		return &ReadBackError{
			Path: book.Path,
			Err: fmt.Errorf("it holds %d entries and the copybook declares %d",
				len(built), len(entries)),
		}
	}

	for i, entry := range entries {
		if reflect.DeepEqual(definitions.Cleared(built[i]), transcribable(entry)) {
			continue
		}

		fault := &ReadBackError{
			Path:   book.Path,
			Line:   entry.Pos.Line,
			Column: entry.Pos.Column,
			Item:   label(entry),
		}

		if entry.Pos.Text != nil && members != nil {
			at := members.Span(book.Path, entry.Pos)
			fault.In = &at
		}

		return fault
	}

	return nil
}

// transcribable is what a transcription of entry reads back as: entry with
// positions and comments cleared, and an omitted name at a level other than 66
// and 88 counted as FILLER.
func transcribable(entry *cobol.DataDescriptionEntry) *cobol.DataDescriptionEntry {
	cleared := definitions.Cleared(entry)

	if cleared.Name == nil && cleared.Level != 66 && cleared.Level != 88 {
		cleared.Filler = true
	}

	return cleared
}

// label names an entry in a diagnostic as the copybook writes it: its level,
// and its name or FILLER.
func label(entry *cobol.DataDescriptionEntry) string {
	name := "FILLER"
	if entry.Name != nil && !entry.Filler {
		name = entry.Name.Value
	}

	return strings.TrimSpace(fmt.Sprintf("%02d %s", entry.Level, name))
}
