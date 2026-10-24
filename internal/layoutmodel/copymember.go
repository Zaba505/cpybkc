// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"github.com/Zaba505/cpybkc/internal/copymember"
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
)

// The tags a `copy-member` form is written with: the form, and the `of` form a
// library-qualified member is.
const (
	tagCopyMember = "copy-member"
	tagOf         = "of"
)

// CopyMember is one `copy-member` form: a member a copybook's `COPY` names, and
// the path of the file holding it (docs/layout/SPEC.md, "A `COPY` member is
// found where the layout says").
//
// The path is carried as the layout spells it and nothing has been resolved
// against anything, for the reason a record's is: where a file is looked for is
// the CLI's, and a diagnostic naming a path the adopter never wrote sends them
// looking for a file they have not got.
type CopyMember struct {
	// Pos is the `copy-member` form.
	Pos layout.Pos

	// Member is the member, as a `COPY` names it.
	Member copymember.Member

	// MemberPos is the member: its symbol, its `word` form, or its `of` form.
	MemberPos layout.Pos

	// Path is the member file's path as the layout spells it.
	Path string

	// PathPos is the path itself.
	PathPos layout.Pos
}

// ReadCopyMembers reads the `copy-member` forms out of a parsed layout, in the
// order the layout writes them.
//
// It reports every fault it finds, joined, and returns nothing when it found
// one. What it enforces is the half of the forms the layout decides on its
// own: that no two forms name one member, under the matching a `COPY` is
// matched with. Two forms naming one member are reported at the second, with a
// second span at the first, as a block defined twice is: either is a good
// statement on its own, and which file the member is meant to be is the
// adopter's to decide. Two forms naming one path under two members are not a
// fault — a shop may know one member by two names.
//
// A form no `COPY` reaches needs the copybooks, and is the run's to report once
// it has read them.
func ReadCopyMembers(file *layout.File) ([]CopyMember, error) {
	var (
		faults  diag.List
		members []CopyMember
	)

	for _, form := range file.Forms {
		if form.Tag != tagCopyMember {
			continue
		}

		member, ok := readCopyMember(&faults, form)
		if !ok {
			continue
		}

		if first, already := namedBefore(members, member.Member); already {
			faults.Fail(&DuplicateCopyMemberError{Pos: member.MemberPos, First: first.MemberPos, Member: member.Member})

			continue
		}

		members = append(members, member)
	}

	if faults.Failed() {
		return nil, faults.Err()
	}

	return members, nil
}

// namedBefore is the form among members naming member, if one does.
func namedBefore(members []CopyMember, member copymember.Member) (CopyMember, bool) {
	for _, earlier := range members {
		if earlier.Member.Same(member) {
			return earlier, true
		}
	}

	return CopyMember{}, false
}

// readCopyMember reads one `(copy-member <member> "<path>")`.
//
// The schema check has already held the form to its shape, so a form of
// another shape is reported in this package's words only as the backstop for a
// caller that never ran the check.
func readCopyMember(faults *diag.List, form layout.Form) (CopyMember, bool) {
	if len(form.Elements) != 2 {
		faults.Fail(&CopyMemberFormError{Pos: form.Pos, Found: shortfall(form.Elements)})

		return CopyMember{}, false
	}

	path, ok := form.Elements[1].(layout.Text)
	if !ok {
		faults.Fail(&CopyMemberFormError{Pos: form.Elements[1].Position(), Found: describe(form.Elements[1])})

		return CopyMember{}, false
	}

	member, at, ok := readMember(faults, form.Elements[0])
	if !ok {
		return CopyMember{}, false
	}

	return CopyMember{
		Pos:       form.Pos,
		Member:    member,
		MemberPos: at,
		Path:      path.Value,
		PathPos:   path.Pos,
	}, true
}

// readMember reads a member: a data-name, or `(of <text-name> <library-name>)`.
func readMember(faults *diag.List, node layout.Node) (copymember.Member, layout.Pos, bool) {
	if of, isForm := node.(layout.Form); isForm && of.Tag == tagOf {
		if len(of.Elements) != 2 {
			faults.Fail(&CopyMemberFormError{Pos: of.Pos, Found: shortfall(of.Elements)})

			return copymember.Member{}, layout.Pos{}, false
		}

		name, nameOK := memberName(faults, of.Elements[0])
		library, libraryOK := memberName(faults, of.Elements[1])

		return copymember.Member{Name: name, Library: library}, of.Pos, nameOK && libraryOK
	}

	name, ok := memberName(faults, node)

	return copymember.Member{Name: name}, node.Position(), ok
}

// memberName reads one name of a member, spelled as a data-name is.
func memberName(faults *diag.List, node layout.Node) (string, bool) {
	name, _, ok, err := dataName(node)
	if err != nil {
		faults.Fail(err)

		return "", false
	}

	if !ok {
		faults.Fail(&CopyMemberFormError{Pos: node.Position(), Found: describe(node)})

		return "", false
	}

	return name, true
}
