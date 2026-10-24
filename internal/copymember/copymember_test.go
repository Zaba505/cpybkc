// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package copymember_test

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"

	cobol "github.com/Zaba505/cobol-go"

	"github.com/Zaba505/cpybkc/internal/copymember"
	"github.com/Zaba505/cpybkc/internal/diag"
)

// files is a filesystem a test can count the reads of.
type files struct {
	text  map[string]string
	reads map[string]int
}

func newFiles(text map[string]string) *files {
	return &files{text: text, reads: make(map[string]int)}
}

func (f *files) open(path string) ([]byte, error) {
	f.reads[path]++

	text, ok := f.text[path]
	if !ok {
		return nil, fmt.Errorf("open %s: %w", path, fs.ErrNotExist)
	}

	return []byte(text), nil
}

// form is a form whose path is spelled as it is opened.
func form(name, library, path string) copymember.Form {
	return copymember.Form{Member: copymember.Member{Name: name, Library: library}, Path: path, File: "/srv/" + path}
}

// parse parses a free-format fragment through r.
func parse(t *testing.T, r *copymember.Resolver, source string) ([]*cobol.DataDescriptionEntry, error) {
	t.Helper()

	file, err := cobol.Parse(strings.NewReader(source), cobol.WithFragment(), cobol.WithCopyBooks(r))
	if err != nil {
		return nil, err
	}

	return file.Fragment.Entries, nil
}

// names is the name of every entry, in order.
func names(entries []*cobol.DataDescriptionEntry) []string {
	var got []string
	for _, entry := range entries {
		if entry.Name != nil {
			got = append(got, entry.Name.Value)
		}
	}

	return got
}

// TestAMemberIsMatchedAsTheSpecSays holds [copymember.Member.Names] to
// docs/layout/SPEC.md's "A `COPY` member is found where the layout says": the
// text-names and the library-names equal without regard to case, or neither
// with a library-name, and no fallback between the two shapes.
func TestAMemberIsMatchedAsTheSpecSays(t *testing.T) {
	t.Parallel()

	tests := []struct {
		member        copymember.Member
		name, library string
		want          bool
	}{
		{copymember.Member{Name: "STDHDR"}, "STDHDR", "", true},
		{copymember.Member{Name: "STDHDR"}, "stdhdr", "", true},
		{copymember.Member{Name: "stdhdr.cpy"}, "STDHDR.CPY", "", true},
		{copymember.Member{Name: "STDHDR", Library: "PAYLIB"}, "stdhdr", "paylib", true},
		{copymember.Member{Name: "STDHDR"}, "STDHDR", "PAYLIB", false},
		{copymember.Member{Name: "STDHDR", Library: "PAYLIB"}, "STDHDR", "", false},
		{copymember.Member{Name: "STDHDR", Library: "PAYLIB"}, "STDHDR", "GLLIB", false},
		{copymember.Member{Name: "STDHDR"}, "ORDDTL", "", false},
	}

	for _, test := range tests {
		if got := test.member.Names(test.name, test.library); got != test.want {
			t.Errorf("%s names COPY %s OF %q: %v, want %v", test.member, test.name, test.library, got, test.want)
		}
	}
}

// TestACopyIsExpandedThroughTheMap is a copybook joining a shared header to a
// body, with a nested `COPY` and a `COPY … OF`, expanded through the map and
// nothing else, each member file read once however often it is named.
func TestACopyIsExpandedThroughTheMap(t *testing.T) {
	t.Parallel()

	fsys := newFiles(map[string]string{
		"/srv/cpy/stdhdr.cpy": "05 STD-REC-TYPE PIC X.\n05 STD-REC-LEN PIC 9(4).\n",
		"/srv/cpy/orddtl.cpy": "05 OD-QTY PIC 9(3).\nCOPY STDTRL OF GLLIB.\n",
		"/srv/cpy/stdtrl.cpy": "05 STD-TRAILER PIC X.\n",
	})

	r := copymember.NewWith([]copymember.Form{
		form("STDHDR", "", "cpy/stdhdr.cpy"),
		form("ORDDTL", "", "cpy/orddtl.cpy"),
		form("STDTRL", "GLLIB", "cpy/stdtrl.cpy"),
		form("UNUSED", "", "cpy/unused.cpy"),
	}, fsys.open)

	entries, err := parse(t, r, "01 ORDER-DETAIL-REC.\n   COPY stdhdr.\n   COPY ORDDTL.\n01 OTHER-REC.\n   COPY STDHDR.\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	want := []string{"ORDER-DETAIL-REC", "STD-REC-TYPE", "STD-REC-LEN", "OD-QTY", "STD-TRAILER", "OTHER-REC", "STD-REC-TYPE", "STD-REC-LEN"}
	if got := names(entries); !slices.Equal(got, want) {
		t.Errorf("expanded as %v, want %v", got, want)
	}

	for path, n := range fsys.reads {
		if n != 1 {
			t.Errorf("%s was read %d times, want once", path, n)
		}
	}

	if _, read := fsys.reads["/srv/cpy/unused.cpy"]; read {
		t.Error("a member no COPY names was read")
	}

	for i, want := range []bool{true, true, true, false} {
		if got := r.Reached(i); got != want {
			t.Errorf("form %d reached: %v, want %v", i, got, want)
		}
	}
}

// TestTwoMembersNamingOneFileReadItOnce: a shop may know one member by two
// names, and the file is still read once.
func TestTwoMembersNamingOneFileReadItOnce(t *testing.T) {
	t.Parallel()

	fsys := newFiles(map[string]string{"/srv/cpy/stdhdr.cpy": "05 STD-REC-TYPE PIC X.\n"})

	r := copymember.NewWith([]copymember.Form{
		form("STDHDR", "", "cpy/stdhdr.cpy"),
		form("HEADER", "", "cpy/stdhdr.cpy"),
	}, fsys.open)

	if _, err := parse(t, r, "01 A.\n   COPY STDHDR.\n01 B.\n   COPY HEADER.\n"); err != nil {
		t.Fatalf("parse: %v", err)
	}

	if n := fsys.reads["/srv/cpy/stdhdr.cpy"]; n != 1 {
		t.Errorf("the file was read %d times, want once", n)
	}
}

// TestACopyTheMapDoesNotNameIsAFault, with no fallback from a qualified COPY to
// an unqualified member, and the members the map does name carried for the
// diagnostic.
func TestACopyTheMapDoesNotNameIsAFault(t *testing.T) {
	t.Parallel()

	r := copymember.NewWith([]copymember.Form{form("STDHDR", "", "cpy/stdhdr.cpy")}, newFiles(nil).open)

	_, err := parse(t, r, "01 A.\n   COPY STDHDR OF PAYLIB.\n")

	fault, ok := copymember.FaultOf(err)
	if !ok || fault.Unnamed == nil {
		t.Fatalf("the fault is %v, want a COPY naming no member", err)
	}

	if got := fault.Unnamed.Member.String(); got != "STDHDR OF PAYLIB" {
		t.Errorf("the COPY names %s, want STDHDR OF PAYLIB", got)
	}

	if len(fault.Unnamed.Named) != 1 || fault.Unnamed.Named[0].Name != "STDHDR" {
		t.Errorf("the map names %v, want STDHDR", fault.Unnamed.Named)
	}

	if fault.At.Line != 2 || fault.At.Column != 4 {
		t.Errorf("the fault is at %d:%d, want the COPY at 2:4", fault.At.Line, fault.At.Column)
	}

	if !errors.Is(err, fs.ErrNotExist) {
		t.Error("the fault does not say the resolver has no such copybook")
	}
}

// TestAMemberFileThatIsNotThereIsAFault names the form, and is reported once
// however many COPY statements name it, since the read is not repeated.
func TestAMemberFileThatIsNotThereIsAFault(t *testing.T) {
	t.Parallel()

	fsys := newFiles(nil)
	r := copymember.NewWith([]copymember.Form{form("STDHDR", "", "cpy/stdhdr.cpy")}, fsys.open)

	_, err := parse(t, r, "01 A.\n   COPY STDHDR.\n")

	fault, ok := copymember.FaultOf(err)
	if !ok || fault.Unreadable == nil {
		t.Fatalf("the fault is %v, want a member file that cannot be read", err)
	}

	if fault.Unreadable.Form.Path != "cpy/stdhdr.cpy" {
		t.Errorf("the fault names %s, want the path as the form spells it", fault.Unreadable.Form.Path)
	}

	if !errors.Is(err, fs.ErrNotExist) {
		t.Error("the fault does not wrap fs.ErrNotExist")
	}

	if !r.Reached(0) {
		t.Error("a member whose file is missing is still reached by the COPY naming it")
	}
}

// TestACycleIsAFault is a member that copies the member copying it, reported
// at the COPY that closes it.
func TestACycleIsAFault(t *testing.T) {
	t.Parallel()

	r := copymember.NewWith([]copymember.Form{
		form("A", "", "cpy/a.cpy"),
		form("B", "", "cpy/b.cpy"),
	}, newFiles(map[string]string{
		"/srv/cpy/a.cpy": "05 A-ONE PIC X.\nCOPY B.\n",
		"/srv/cpy/b.cpy": "05 B-ONE PIC X.\nCOPY A.\n",
	}).open)

	_, err := parse(t, r, "01 R.\n   COPY A.\n")

	fault, ok := copymember.FaultOf(err)
	if !ok || fault.Cycle == nil {
		t.Fatalf("the fault is %v, want a cycle", err)
	}

	if got := r.File("order.cpy", fault.At); got != "cpy/b.cpy" {
		t.Errorf("the cycle is closed in %s, want cpy/b.cpy", got)
	}

	if fault.At.Line != 2 {
		t.Errorf("the cycle is closed at line %d, want the COPY at 2", fault.At.Line)
	}
}

// TestASpanIntoAMemberCarriesItsChain is the provenance rule: a position in a
// member nested two deep is a span into the innermost member, at its line and
// column there, with one span per COPY that reached it, innermost first.
func TestASpanIntoAMemberCarriesItsChain(t *testing.T) {
	t.Parallel()

	r := copymember.NewWith([]copymember.Form{
		form("ORDDTL", "", "cpy/orddtl.cpy"),
		form("STDTRL", "GLLIB", "cpy/stdtrl.cpy"),
	}, newFiles(map[string]string{
		"/srv/cpy/orddtl.cpy": "05 OD-QTY PIC 9(3).\n    COPY STDTRL OF GLLIB.\n",
		"/srv/cpy/stdtrl.cpy": "\n   05 STD-TRAILER PIC X.\n",
	}).open)

	entries, err := parse(t, r, "01 ORDER-DETAIL-REC.\n   COPY ORDDTL.\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	trailer := entries[len(entries)-1]

	got := diag.Diagnostic{Message: "here", Spans: []diag.Span{r.Span("cpy/order-detail.cpy", trailer.Pos)}}.String()

	const want = "cpy/stdtrl.cpy:2:4: here\n" +
		"  cpy/orddtl.cpy:2:5: copied by COPY STDTRL OF GLLIB\n" +
		"  cpy/order-detail.cpy:2:4: copied by COPY ORDDTL"
	if got != want {
		t.Errorf("rendered\n%s\nwant\n%s", got, want)
	}

	if got := r.Span("cpy/order-detail.cpy", entries[0].Pos); got.Via != nil || got.File != "cpy/order-detail.cpy" {
		t.Errorf("an entry of the copybook itself is %#v, want a span into it with no chain", got)
	}
}

// TestReplacingIsCobolGos: a member copied with REPLACING is expanded as
// `cobol-go` applies the phrase, and a position after a substitution that
// changed a word's length is the one the member has as written.
func TestReplacingIsCobolGos(t *testing.T) {
	t.Parallel()

	r := copymember.NewWith([]copymember.Form{form("TAGGED", "", "cpy/tagged.cpy")}, newFiles(map[string]string{
		"/srv/cpy/tagged.cpy": "05 :TAG:-ID PIC X. 05 :TAG:-NAME PIC X(4).\n",
	}).open)

	entries, err := parse(t, r, "01 R.\n   COPY TAGGED REPLACING ==:TAG:== BY ==CUSTOMER==.\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got, want := names(entries), []string{"R", "CUSTOMER-ID", "CUSTOMER-NAME"}; !slices.Equal(got, want) {
		t.Errorf("expanded as %v, want %v", got, want)
	}

	if at := r.Span("r.cpy", entries[2].Pos); at.File != "cpy/tagged.cpy" || at.Line != 1 || at.Column != 20 {
		t.Errorf("the second entry is at %s, want cpy/tagged.cpy:1:20, its place as written", at)
	}
}
