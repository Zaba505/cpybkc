// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"errors"
	"testing"

	"github.com/Zaba505/cpybkc/internal/copymember"
	"github.com/Zaba505/cpybkc/internal/definitions"
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
)

// TestEveryCopyMemberIsReadWithItsPositions is [ReadCopyMembers]: a member is
// a data-name or an `of` form, each name spelled as a data-name is, and the
// path is carried as the layout spells it.
func TestEveryCopyMemberIsReadWithItsPositions(t *testing.T) {
	t.Parallel()

	members, err := ReadCopyMembers(parsed(t, `(copy-member STDHDR "cpy/stdhdr.cpy")
(copy-member (of STDTRL GLLIB) "cpy/stdtrl.cpy")
(copy-member (word "1ST-HDR") "cpy/first.cpy")
(copy-member HEADER "cpy/stdhdr.cpy")
`))
	if err != nil {
		t.Fatalf("read:\n%s", diag.Render(err))
	}

	want := []CopyMember{
		{
			Pos:       layout.Pos{File: "layout.sexpr", Line: 1, Column: 1},
			Member:    copymember.Member{Name: "STDHDR"},
			MemberPos: layout.Pos{File: "layout.sexpr", Line: 1, Column: 14},
			Path:      "cpy/stdhdr.cpy",
			PathPos:   layout.Pos{File: "layout.sexpr", Line: 1, Column: 21},
		},
		{
			Pos:       layout.Pos{File: "layout.sexpr", Line: 2, Column: 1},
			Member:    copymember.Member{Name: "STDTRL", Library: "GLLIB"},
			MemberPos: layout.Pos{File: "layout.sexpr", Line: 2, Column: 14},
			Path:      "cpy/stdtrl.cpy",
			PathPos:   layout.Pos{File: "layout.sexpr", Line: 2, Column: 32},
		},
		{
			Pos:       layout.Pos{File: "layout.sexpr", Line: 3, Column: 1},
			Member:    copymember.Member{Name: "1ST-HDR"},
			MemberPos: layout.Pos{File: "layout.sexpr", Line: 3, Column: 14},
			Path:      "cpy/first.cpy",
			PathPos:   layout.Pos{File: "layout.sexpr", Line: 3, Column: 31},
		},
		{
			Pos:       layout.Pos{File: "layout.sexpr", Line: 4, Column: 1},
			Member:    copymember.Member{Name: "HEADER"},
			MemberPos: layout.Pos{File: "layout.sexpr", Line: 4, Column: 14},
			Path:      "cpy/stdhdr.cpy",
			PathPos:   layout.Pos{File: "layout.sexpr", Line: 4, Column: 21},
		},
	}

	if len(members) != len(want) {
		t.Fatalf("read %d members, want %d: %+v", len(members), len(want), members)
	}

	for i := range want {
		if members[i] != want[i] {
			t.Errorf("member %d is %+v, want %+v", i, members[i], want[i])
		}
	}
}

// TestTwoFormsNamingOneMemberCarryBothSpans is the layout reader's fault with
// two spans, the second form's member and the first's, under the matching a
// COPY is matched with: case does not tell two members apart, and a library
// does.
func TestTwoFormsNamingOneMemberCarryBothSpans(t *testing.T) {
	t.Parallel()

	_, err := ReadCopyMembers(parsed(t, `(copy-member STDHDR "cpy/stdhdr.cpy")
(copy-member (of STDHDR PAYLIB) "cpy/payhdr.cpy")
(copy-member stdhdr "cpy/other.cpy")
`))

	var duplicate *DuplicateCopyMemberError
	if !errors.As(err, &duplicate) {
		t.Fatalf("two forms naming one member read as %v, want a DuplicateCopyMemberError", err)
	}

	diagnostics := diag.Diagnostics(err)
	if len(diagnostics) != 1 {
		t.Fatalf("want one diagnostic; got:\n%s", diag.Render(err))
	}

	const want = "layout.sexpr:3:14: the copy member stdhdr is named twice; a COPY of it would have two files to read\n" +
		"  layout.sexpr:1:14: it is named first here"
	if got := diagnostics[0].String(); got != want {
		t.Errorf("the fault reads\n%s\nwant\n%s", got, want)
	}
}

// TestANeedlessWordInAMemberIsTheEntryReadersFault: a member's names are
// data-names, with one spelling each.
func TestANeedlessWordInAMemberIsTheEntryReadersFault(t *testing.T) {
	t.Parallel()

	_, err := ReadCopyMembers(parsed(t, `(copy-member (of (word "STDHDR") PAYLIB) "cpy/stdhdr.cpy")
`))

	var needless *definitions.NeedlessWordError
	if !errors.As(err, &needless) {
		t.Fatalf("a needless word reads as %v, want a NeedlessWordError", err)
	}
}
