// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package scaffold

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/copymember"
	"github.com/Zaba505/cpybkc/internal/diag"
)

// orderDetail joins a shared header to a per-type body with COPY.
const orderDetail = `       01  ORDER-DETAIL-REC.
           COPY STDHDR.
           COPY ORDDTL.
`

// members writes each member file into a directory of its own and returns
// the --copy-member forms giving them, the path as typed being the file's.
func members(t *testing.T, files map[string]string, forms ...copymember.Form) []copymember.Form {
	t.Helper()

	dir := t.TempDir()

	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	given := make([]copymember.Form, 0, len(forms))

	for _, form := range forms {
		form.Path = filepath.Join(dir, form.Path)
		form.File = form.Path
		given = append(given, form)
	}

	return given
}

// member is a --copy-member value for a test's members.
func member(name, library, path string) copymember.Form {
	return copymember.Form{Member: copymember.Member{Name: name, Library: library}, Path: path}
}

// composedMembers are the members orderDetail copies, the body copying a
// trailer from a library.
var composedMembers = map[string]string{
	"stdhdr.cpy": `           05  STD-HEADER.
               10  STD-REC-TYPE    PIC X.
               10  STD-REC-LEN     PIC 9(4).
`,
	"orddtl.cpy": `           05  OD-QTY              PIC 9(3).
           COPY STDTRL OF GLLIB.
`,
	"stdtrl.cpy": `           05  STD-TRAILER         PIC X.
`,
}

// TestACopybookThatWritesCopyConvertsAsItsExpansion is docs/cli/SPEC.md's
// "Where the members come from": the definitions file holds the expanded
// entries, each member's where its COPY stood, a member gets no definitions
// file of its own, and the scaffold's record is the copybook's 01-level.
func TestACopybookThatWritesCopyConvertsAsItsExpansion(t *testing.T) {
	t.Parallel()

	given := members(t, composedMembers,
		member("STDHDR", "", "stdhdr.cpy"),
		member("ORDDTL", "", "orddtl.cpy"),
		member("STDTRL", "GLLIB", "stdtrl.cpy"),
	)

	derived, err := Derive([]Copybook{book("cpy/order-detail.cpy", orderDetail)}, given...)
	if err != nil {
		t.Fatalf("derive:\n%s", diag.Render(err))
	}

	files := derived.Definitions()
	if len(files) != 1 || files[0].Name != "order-detail.cpy.sexpr" {
		t.Fatalf("the definitions files are %v, want order-detail.cpy.sexpr alone", files)
	}

	const want = `; The entries of the copybook "cpy/order-detail.cpy", transcribed by cpybkc init.
(entry 01 ORDER-DETAIL-REC)
  (entry 05 STD-HEADER)
    (entry 10 STD-REC-TYPE (pic "X"))
    (entry 10 STD-REC-LEN (pic "9(4)"))
  (entry 05 OD-QTY (pic "9(3)"))
  (entry 05 STD-TRAILER (pic "X"))
`
	if got := string(files[0].Bytes); got != want {
		t.Errorf("the definitions file reads\n%s\nwant\n%s", got, want)
	}

	if got := recordNames(derived); !slices.Equal(got, []string{"ORDER-DETAIL-REC"}) {
		t.Errorf("the records are %v, want ORDER-DETAIL-REC", got)
	}

	if notes := derived.Notes(); len(notes) != 0 {
		t.Errorf("a run naming every member it uses owes no note, and got %q", notes)
	}
}

// TestAMemberNoCopyNamesIsNoted, naming the value as typed, and the run carries
// on: it is a note and not a refusal, and the member's file is not read.
func TestAMemberNoCopyNamesIsNoted(t *testing.T) {
	t.Parallel()

	given := members(t, composedMembers,
		member("STDHDR", "", "stdhdr.cpy"),
		member("ORDDTL", "", "orddtl.cpy"),
		member("STDTRL", "GLLIB", "stdtrl.cpy"),
		member("UNUSED", "", "unused.cpy"),
	)

	derived, err := Derive([]Copybook{book("cpy/order-detail.cpy", orderDetail)}, given...)
	if err != nil {
		t.Fatalf("derive:\n%s", diag.Render(err))
	}

	notes := derived.Notes()
	if len(notes) != 1 || !strings.Contains(notes[0], "--copy-member UNUSED=") || !strings.Contains(notes[0], "no COPY in any copybook names UNUSED") {
		t.Errorf("the notes are %q, want one naming --copy-member UNUSED", notes)
	}
}

// TestACopyNamingNoMemberIsARefusalWithTheNoteBesideIt: the misspelled member
// is noted on the refusal its COPY produces, so the two read together.
func TestACopyNamingNoMemberIsARefusalWithTheNoteBesideIt(t *testing.T) {
	t.Parallel()

	given := members(t, composedMembers,
		member("STDHDR", "", "stdhdr.cpy"),
		member("ORDTL", "", "orddtl.cpy"),
	)

	_, err := Derive([]Copybook{book("cpy/order-detail.cpy", orderDetail)}, given...)

	var unnamed *copymember.UnnamedMemberError
	if !errors.As(err, &unnamed) {
		t.Fatalf("the fault is %v, want a COPY naming no member", err)
	}

	const want = "cpy/order-detail.cpy:3:12: COPY ORDDTL names no --copy-member on the command line\n" +
		"  the command line names STDHDR and ORDTL"
	if got := diag.Render(err); got != want {
		t.Errorf("the fault reads\n%s\nwant\n%s", got, want)
	}

	var noted Noted
	if !errors.As(err, &noted) || len(noted.Notes()) != 1 || !strings.Contains(noted.Notes()[0], "--copy-member ORDTL=") {
		t.Errorf("the refusal does not carry the note about ORDTL: %v", err)
	}
}

// TestAMemberFileThatCannotBeOpenedIsARefusal at the COPY needing it, naming the
// path as typed and where it was looked for.
func TestAMemberFileThatCannotBeOpenedIsARefusal(t *testing.T) {
	t.Parallel()

	given := members(t, nil, member("STDHDR", "", "stdhdr.cpy"), member("ORDDTL", "", "orddtl.cpy"))

	_, err := Derive([]Copybook{book("cpy/order-detail.cpy", orderDetail)}, given...)

	var missing *MissingMemberError
	if !errors.As(err, &missing) {
		t.Fatalf("the fault is %v, want a member file that cannot be opened", err)
	}

	rendered := diag.Render(err)

	if !strings.HasPrefix(rendered, "cpy/order-detail.cpy:2:12: no copy member at "+given[0].Path+", which --copy-member STDHDR="+given[0].Path+" gives\n  looked for ") {
		t.Errorf("the fault reads\n%s", rendered)
	}
}

// TestACopyCycleIsARefusal at the COPY closing it.
func TestACopyCycleIsARefusal(t *testing.T) {
	t.Parallel()

	given := members(t, map[string]string{
		"stdhdr.cpy": "           05  STD-REC-TYPE        PIC X.\n           COPY STDHDR.\n",
	}, member("STDHDR", "", "stdhdr.cpy"))

	_, err := Derive([]Copybook{book("cpy/header.cpy", "       01  HEADER-REC.\n           COPY STDHDR.\n")}, given...)

	var cycle *copymember.CycleError
	if !errors.As(err, &cycle) {
		t.Fatalf("the fault is %v, want a cycle", err)
	}

	want := given[0].Path + ":2:12: COPY STDHDR closes a cycle: STDHDR is already being copied\n" +
		"  cpy/header.cpy:2:12: COPY STDHDR is open here"
	if got := diag.Render(err); got != want {
		t.Errorf("the fault reads\n%s\nwant\n%s", got, want)
	}
}

// TestAFaultInsideAMemberPointsIntoTheMember, named as --copy-member gives it,
// with the chain that reached it.
func TestAFaultInsideAMemberPointsIntoTheMember(t *testing.T) {
	t.Parallel()

	files := maps.Clone(composedMembers)

	files["stdtrl.cpy"] = "           05  STD-TRAILER         PIC Q.\n"

	given := members(t, files,
		member("STDHDR", "", "stdhdr.cpy"),
		member("ORDDTL", "", "orddtl.cpy"),
		member("STDTRL", "GLLIB", "stdtrl.cpy"),
	)

	_, err := Derive([]Copybook{book("cpy/order-detail.cpy", orderDetail)}, given...)

	var source *SourceError
	if !errors.As(err, &source) {
		t.Fatalf("the fault is %v, want a copybook that cannot be read", err)
	}

	rendered := diag.Render(err)

	if !strings.HasPrefix(rendered, "cpy/order-detail.cpy: this is not a copybook this build can read\n  "+given[2].Path+":1:") {
		t.Errorf("the fault does not point into the member:\n%s", rendered)
	}

	if want := "\n  " + given[1].Path + ":2:12: copied by COPY STDTRL OF GLLIB\n  cpy/order-detail.cpy:3:12: copied by COPY ORDDTL"; !strings.HasSuffix(rendered, want) {
		t.Errorf("the fault does not carry the chain:\n%s", rendered)
	}
}

// TestTwoMembersACopyCannotTellApartAreRefused, by Derive as by the command
// line.
func TestTwoMembersACopyCannotTellApartAreRefused(t *testing.T) {
	t.Parallel()

	_, err := Derive([]Copybook{book("cpy/order-detail.cpy", orderDetail)},
		member("STDHDR", "", "a.cpy"), member("stdhdr", "", "b.cpy"))

	var twice *MemberTwiceError
	if !errors.As(err, &twice) {
		t.Fatalf("the fault is %v, want two members naming one", err)
	}
}

// TestAMemberFileThatCannotBeReadIsNotCalledMissing: a path that is there and
// is no readable file is said to be that, with what reading it said.
func TestAMemberFileThatCannotBeReadIsNotCalledMissing(t *testing.T) {
	t.Parallel()

	given := members(t, nil, member("STDHDR", "", "."))

	_, err := Derive([]Copybook{book("cpy/header.cpy", "       01  HEADER-REC.\n           COPY STDHDR.\n")}, given...)

	rendered := diag.Render(err)

	if !strings.Contains(rendered, "could not be read") || strings.Contains(rendered, "no copy member at") {
		t.Errorf("an unreadable member reads as\n%s\nwant it said to be unreadable rather than missing", rendered)
	}
}

// TestANoteAfterARefusalSaysTheReadingStopped: a COPY after the one refused was
// never read, so the member it names is noted as named by no COPY that was
// read, not by none at all.
func TestANoteAfterARefusalSaysTheReadingStopped(t *testing.T) {
	t.Parallel()

	given := members(t, composedMembers, member("STDHDR", "", "stdhdr.cpy"), member("ORDDTL", "", "orddtl.cpy"))

	const misspelled = `       01  ORDER-DETAIL-REC.
           COPY STDHRD.
           COPY ORDDTL.
`

	_, err := Derive([]Copybook{book("cpy/order-detail.cpy", misspelled)}, given...)

	var noted Noted
	if !errors.As(err, &noted) {
		t.Fatalf("the refusal carries no notes: %v", err)
	}

	for _, note := range noted.Notes() {
		if strings.Contains(note, "no COPY in any copybook names") {
			t.Errorf("a note claims more than the refused reading can know: %s", note)
		}
	}

	if len(noted.Notes()) != 2 || !strings.Contains(noted.Notes()[1], "a COPY after the refusal may still name it") {
		t.Errorf("the notes are %q, want both members noted as unread before the refusal", noted.Notes())
	}
}

// TestANoteQuotesTheValueAsTyped: a member written as a literal, or with IN,
// is noted as it was typed and not rebuilt from what it means.
func TestANoteQuotesTheValueAsTyped(t *testing.T) {
	t.Parallel()

	form := member("UNUSED", "PAYLIB", "unused.cpy")
	form.File = form.Path
	form.Given = "'UNUSED' IN PAYLIB=unused.cpy"

	derived, err := Derive([]Copybook{book("cpy/header.cpy", "       01  HEADER-REC.\n           05  H PIC X.\n")}, form)
	if err != nil {
		t.Fatalf("derive:\n%s", diag.Render(err))
	}

	if notes := derived.Notes(); len(notes) != 1 || !strings.HasPrefix(notes[0], "--copy-member 'UNUSED' IN PAYLIB=unused.cpy: ") {
		t.Errorf("the notes are %q, want the value quoted as typed", notes)
	}
}
