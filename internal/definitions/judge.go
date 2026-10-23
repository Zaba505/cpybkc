// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package definitions

import (
	"fmt"
	"reflect"
	"strings"

	cobol "github.com/Zaba505/cobol-go"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// Judge builds entries with [Construct] and holds each to docs/layout/SPEC.md's
// validity rule, "cobol-go judges an entry": printed with [cobol.Print] and read
// back in free format with [cobol.Parse] and [cobol.WithFragment], it gives back
// the entry it was built from, with positions and comments cleared.
//
// The rule is applied entry by entry, so that a refusal is a diagnostic at the
// entry that drew it — an [InadmissibleEntryError] quoting the printed COBOL and
// what `cobol-go` said — and every refused entry is reported, not the first.
// It holds in a definitions file and in a block alike, since both are read into
// the one model. Only when every entry is admitted does it return the built
// entries.
//
// Nothing here says what an entry means. Which levels exist, which clauses an
// entry at a level may carry, which usages there are: each is whatever the
// pinned `cobol-go` reads back, so a usage it gains is admitted the day the pin
// moves, with no list here to extend.
func Judge(entries []Entry) ([]*cobol.DataDescriptionEntry, error) {
	built, err := Construct(entries)
	if err != nil {
		return nil, err
	}

	var faults diag.List

	for i, entry := range built {
		if refusal := judge(entry); refusal != nil {
			refusal.Pos = entries[i].Pos
			faults.Fail(refusal)
		}
	}

	if faults.Failed() {
		return nil, faults.Err()
	}

	return built, nil
}

// judge applies the validity rule to one entry, and returns the refusal, with
// no position yet, or nil when it is admitted.
func judge(entry *cobol.DataDescriptionEntry) *InadmissibleEntryError {
	printed, err := printEntries(entry)
	if err != nil {
		return &InadmissibleEntryError{Reason: fmt.Sprintf("cobol.Print refuses it: %v", err)}
	}

	quoted := strings.TrimSpace(printed)

	file, err := cobol.Parse(strings.NewReader(printed), cobol.WithFragment())
	if err != nil {
		return &InadmissibleEntryError{Printed: quoted, Reason: fmt.Sprintf("cobol.Parse refuses it: %v", err)}
	}

	var read []*cobol.DataDescriptionEntry
	if file.Fragment != nil {
		read = file.Fragment.Entries
	}

	want := Cleared(entry)

	if len(read) == 1 && reflect.DeepEqual(Cleared(read[0]), want) {
		return nil
	}

	return &InadmissibleEntryError{Printed: quoted, Reason: readBack(read)}
}

// readBack says what `cobol-go` read in place of the entry it was shown.
func readBack(read []*cobol.DataDescriptionEntry) string {
	if len(read) != 1 {
		reprinted, err := printEntries(read...)
		if err != nil || len(read) == 0 {
			return fmt.Sprintf("which cobol-go reads back as %d entries", len(read))
		}

		return fmt.Sprintf("which cobol-go reads back as %d entries, %q", len(read), strings.TrimSpace(reprinted))
	}

	reprinted, err := printEntries(read[0])
	if err != nil {
		return "which cobol-go reads back as a different entry"
	}

	return fmt.Sprintf("which cobol-go reads back as %q", strings.TrimSpace(reprinted))
}

// printEntries is entries as `cobol.Print` writes a fragment holding them.
func printEntries(entries ...*cobol.DataDescriptionEntry) (string, error) {
	var out strings.Builder

	err := cobol.Print(&out, &cobol.File{Fragment: &cobol.Fragment{Entries: entries}})

	return out.String(), err
}

// Cleared is a deep copy of entry with every [cobol.Pos] zeroed and every
// comment dropped, which is what the validity rule compares and what the round
// trip holds a transcription to. A reflection walk does it, rather than a field
// list, so that a field the pinned `cobol-go` gains is compared the day it
// appears instead of being skipped by a list nobody extended.
func Cleared(entry *cobol.DataDescriptionEntry) *cobol.DataDescriptionEntry {
	if entry == nil {
		return nil
	}

	cleared := clearValue(reflect.ValueOf(entry))

	return cleared.Interface().(*cobol.DataDescriptionEntry)
}

var (
	posType      = reflect.TypeFor[cobol.Pos]()
	commentsType = reflect.TypeFor[[]*cobol.Comment]()
)

// clearValue is a copy of v with positions zeroed and comments dropped.
func clearValue(v reflect.Value) reflect.Value {
	switch {
	case v.Type() == posType:
		return reflect.Zero(posType)
	case v.Type() == commentsType:
		return reflect.Zero(commentsType)
	}

	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}

		copied := reflect.New(v.Type().Elem())
		copied.Elem().Set(clearValue(v.Elem()))

		return copied
	case reflect.Interface:
		if v.IsNil() {
			return v
		}

		copied := reflect.New(v.Type()).Elem()
		copied.Set(clearValue(v.Elem()))

		return copied
	case reflect.Struct:
		copied := reflect.New(v.Type()).Elem()

		for i := range v.NumField() {
			if !copied.Field(i).CanSet() {
				// An unexported field cannot be copied by reflection, and the
				// pinned `cobol-go` has none in an entry; the fingerprint test
				// fails before one could be skipped here unseen.
				continue
			}

			copied.Field(i).Set(clearValue(v.Field(i)))
		}

		return copied
	case reflect.Slice:
		if v.IsNil() {
			return v
		}

		copied := reflect.MakeSlice(v.Type(), v.Len(), v.Len())

		for i := range v.Len() {
			copied.Index(i).Set(clearValue(v.Index(i)))
		}

		return copied
	default:
		return v
	}
}
