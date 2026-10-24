// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package project_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/copymember"
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
	"github.com/Zaba505/cpybkc/internal/manifest"
	"github.com/Zaba505/cpybkc/internal/project"
)

// composedForms are the `copy-member` forms of the composed orders layout, one
// per line from line 5, so that a test can drop or change one.
var composedForms = []string{
	`(copy-member STDHDR "cpy/stdhdr.cpy")`,
	`(copy-member ORDDTL "cpy/orddtl.cpy")`,
	`(copy-member (of STDTRL GLLIB) "cpy/stdtrl.cpy")`,
}

// composedLayout is a layout whose one record is bound to a copybook joining a
// shared header to a per-type body with COPY, the body copying a trailer from a
// named library, with forms in place of composedForms and extra after them.
func composedLayout(forms []string, extra string) string {
	return `(encoding
  (charset ascii) (sign-convention ascii-zone-37)
  (byte-order big-endian) (float-format ieee-754))
(framing (recfm F) (lrecl 9))
` + strings.Join(forms, "\n") + `
(record ORDER-DETAIL (copybook "cpy/order-detail.cpy" ORDER-DETAIL-REC))
` + extra + `(discriminate ORDER-DETAIL single-record-type)
(sequence (* ORDER-DETAIL))
`
}

// composedFiles are the copybook and its members.
func composedFiles() map[string]string {
	return map[string]string{
		"cpy/order-detail.cpy": fixed(
			"01  ORDER-DETAIL-REC.",
			"    COPY STDHDR.",
			"    COPY ORDDTL.",
		),
		"cpy/stdhdr.cpy": fixed(
			"    05  STD-HEADER.",
			"        10  STD-REC-TYPE  PIC X.",
			"        10  STD-REC-LEN   PIC 9(4).",
		),
		"cpy/orddtl.cpy": fixed(
			"    05  OD-QTY            PIC 9(3).",
			"    COPY STDTRL OF GLLIB.",
		),
		"cpy/stdtrl.cpy": fixed(
			"    05  STD-TRAILER       PIC X.",
		),
	}
}

// composedProject writes a composed project and returns its manifest's path
// and its directory.
func composedProject(t *testing.T, layout string, files map[string]string) (string, string) {
	t.Helper()

	dir := t.TempDir()

	write(t, filepath.Join(dir, "orders.sexpr"), layout)
	write(t, filepath.Join(dir, manifest.Name), `{"layout": "orders.sexpr", "generators": [{"name": "go", "out": "gen"}]}`)

	for name, body := range files {
		write(t, filepath.Join(dir, name), body)
	}

	return filepath.Join(dir, manifest.Name), dir
}

// loadFault loads a project that must fail, and returns its rendered
// diagnostics with the directory written as <dir>.
func loadFault(t *testing.T, path, dir string) (error, string) {
	t.Helper()

	_, err := project.Load(path)
	if err == nil {
		t.Fatal("the project resolves, want a fault")
	}

	return err, strings.ReplaceAll(diag.Render(err), dir, "<dir>")
}

// TestACopybookComposedWithCopyResolvesThroughTheLayoutsForms is a run reading
// a composed copybook: every member is found at the path its `copy-member`
// form states, relative to the layout, a nested COPY and a COPY … OF
// included, and the record holds every entry the expansion puts in it.
func TestACopybookComposedWithCopyResolvesThroughTheLayoutsForms(t *testing.T) {
	t.Parallel()

	path, _ := composedProject(t, composedLayout(composedForms, ""), composedFiles())

	run, err := project.Load(path)
	if err != nil {
		t.Fatalf("the project does not resolve:\n%s", diag.Render(err))
	}

	names := fieldNames(run.Descriptor)

	for _, name := range []string{"STD-REC-TYPE", "STD-REC-LEN", "OD-QTY", "STD-TRAILER"} {
		if names[name] != 1 {
			t.Errorf("the descriptor carries %d fields named %s, want 1", names[name], name)
		}
	}
}

// TestACopyNamingNoFormIsAFaultAtTheCopy is the first of docs/cli/SPEC.md's
// `COPY` faults: at the statement, in the file that writes it, with the members
// the layout does name.
func TestACopyNamingNoFormIsAFaultAtTheCopy(t *testing.T) {
	t.Parallel()

	path, dir := composedProject(t, composedLayout([]string{composedForms[0], composedForms[2]}, ""), composedFiles())

	err, rendered := loadFault(t, path, dir)

	var unnamed *copymember.UnnamedMemberError
	if !errors.As(err, &unnamed) {
		t.Fatalf("the fault is not an UnnamedMemberError:\n%s", rendered)
	}

	const want = "cpy/order-detail.cpy:3:11: COPY ORDDTL names no copy-member in the layout\n" +
		"  the layout names STDHDR and STDTRL OF GLLIB"
	if rendered != want {
		t.Errorf("the fault reads\n%s\nwant\n%s", rendered, want)
	}
}

// TestACopyInsideAMemberNamingNoFormCarriesItsChain: the file that writes the
// COPY is a member, so the span is into the member, with the COPY that reached
// it.
func TestACopyInsideAMemberNamingNoFormCarriesItsChain(t *testing.T) {
	t.Parallel()

	path, dir := composedProject(t, composedLayout(composedForms[:2], ""), composedFiles())

	_, rendered := loadFault(t, path, dir)

	const want = "cpy/orddtl.cpy:2:11: COPY STDTRL OF GLLIB names no copy-member in the layout\n" +
		"  cpy/order-detail.cpy:3:11: copied by COPY ORDDTL\n" +
		"  the layout names STDHDR and ORDDTL"
	if rendered != want {
		t.Errorf("the fault reads\n%s\nwant\n%s", rendered, want)
	}
}

// TestAMemberFileThatIsNotThereNamesBothPaths is the second `COPY` fault, in the
// shape every missing file takes: the path as the form spells it, the span at
// that path, and where it was looked for.
func TestAMemberFileThatIsNotThereNamesBothPaths(t *testing.T) {
	t.Parallel()

	files := composedFiles()
	delete(files, "cpy/orddtl.cpy")

	path, dir := composedProject(t, composedLayout(composedForms, ""), files)

	err, rendered := loadFault(t, path, dir)

	var missing *project.MissingCopyMemberError
	if !errors.As(err, &missing) {
		t.Fatalf("the fault is not a MissingCopyMemberError:\n%s", rendered)
	}

	want := "<dir>/orders.sexpr:6:21: no copy member at cpy/orddtl.cpy\n" +
		"  looked for <dir>/cpy/orddtl.cpy"
	if rendered != want {
		t.Errorf("the fault reads\n%s\nwant\n%s", rendered, want)
	}
}

// TestACopyCycleIsAFaultAtTheCopyClosingIt is the third: the COPY that closes
// the cycle, and the members open there, outermost first.
func TestACopyCycleIsAFaultAtTheCopyClosingIt(t *testing.T) {
	t.Parallel()

	files := composedFiles()
	files["cpy/stdtrl.cpy"] = fixed(
		"    05  STD-TRAILER       PIC X.",
		"    COPY ORDDTL.",
	)

	path, dir := composedProject(t, composedLayout(composedForms, ""), files)

	err, rendered := loadFault(t, path, dir)

	var cycle *copymember.CycleError
	if !errors.As(err, &cycle) {
		t.Fatalf("the fault is not a CycleError:\n%s", rendered)
	}

	const want = "cpy/stdtrl.cpy:2:11: COPY ORDDTL closes a cycle: ORDDTL is already being copied\n" +
		"  cpy/order-detail.cpy:3:11: COPY ORDDTL is open here\n" +
		"  cpy/orddtl.cpy:2:11: COPY STDTRL OF GLLIB is open here"
	if rendered != want {
		t.Errorf("the fault reads\n%s\nwant\n%s", rendered, want)
	}
}

// TestAFormNoCopyReachesIsAnErrorAndItsFileIsNotOpened is docs/layout/SPEC.md's
// unreached form: an error at its member, and nothing opened for it — its file
// is not there, and no missing-file fault is reported.
func TestAFormNoCopyReachesIsAnErrorAndItsFileIsNotOpened(t *testing.T) {
	t.Parallel()

	forms := append(append([]string{}, composedForms...), `(copy-member UNUSED "cpy/unused.cpy")`)

	path, dir := composedProject(t, composedLayout(forms, ""), composedFiles())

	err, rendered := loadFault(t, path, dir)

	var unreached *project.UnreachedCopyMemberError
	if !errors.As(err, &unreached) {
		t.Fatalf("the fault is not an UnreachedCopyMemberError:\n%s", rendered)
	}

	const want = "<dir>/orders.sexpr:8:14: no COPY in a copybook a record names reaches the copy member UNUSED"
	if rendered != want {
		t.Errorf("the fault reads\n%s\nwant\n%s", rendered, want)
	}
}

// TestALayoutWhoseRecordsNameNoCopybookHasOnlyUnreachedForms: a definitions
// file holds no COPY, so a form beside records naming only files is unreached.
func TestALayoutWhoseRecordsNameNoCopybookHasOnlyUnreachedForms(t *testing.T) {
	t.Parallel()

	layout := strings.Replace(postings, "(record DEBIT", `(copy-member STDHDR "cpy/stdhdr.cpy")
(record DEBIT`, 1)

	path, _ := ledgerProject(t, layout, map[string]string{"posting.cpy.sexpr": posting})

	_, err := project.Load(path)

	var unreached *project.UnreachedCopyMemberError
	if !errors.As(err, &unreached) {
		t.Fatalf("the fault is %v, want an UnreachedCopyMemberError", err)
	}
}

// TestTwoFormsNamingOneMemberAreTheLayoutReadersFault, reported before any
// copybook is opened.
func TestTwoFormsNamingOneMemberAreTheLayoutReadersFault(t *testing.T) {
	t.Parallel()

	forms := append(append([]string{}, composedForms...), `(copy-member stdhdr "cpy/other.cpy")`)

	path, _ := composedProject(t, composedLayout(forms, ""), map[string]string{})

	_, err := project.Load(path)

	var duplicate *layoutmodel.DuplicateCopyMemberError
	if !errors.As(err, &duplicate) {
		t.Fatalf("the fault is %v, want a DuplicateCopyMemberError", err)
	}

	if strings.Contains(diag.Render(err), "no copybook") {
		t.Errorf("a copybook was opened after the layout reader's fault:\n%s", diag.Render(err))
	}
}

// TestAnItemReferenceIntoAMemberPointsIntoTheMember is docs/cli/SPEC.md's rule
// for a fault inside a member: the second span is in the member, at the line
// and column the group has there, named as its form spells it, with one line
// per COPY that reached it, innermost first.
func TestAnItemReferenceIntoAMemberPointsIntoTheMember(t *testing.T) {
	t.Parallel()

	layout := composedLayout(composedForms, `(rename (item ORDER-DETAIL STD-HEADER STD-TYPE) "RecordType")
`)

	path, dir := composedProject(t, layout, composedFiles())

	err, rendered := loadFault(t, path, dir)

	var unknown *project.UnknownItemError
	if !errors.As(err, &unknown) {
		t.Fatalf("the fault is not an UnknownItemError:\n%s", rendered)
	}

	want := `<dir>/orders.sexpr:9:9: (item ORDER-DETAIL STD-HEADER STD-TYPE) names no item: "STD-HEADER" declares no "STD-TYPE"` + "\n" +
		"  cpy/stdhdr.cpy:1:11: it declares STD-REC-TYPE and STD-REC-LEN\n" +
		"  cpy/order-detail.cpy:2:11: copied by COPY STDHDR"
	if rendered != want {
		t.Errorf("the fault reads\n%s\nwant\n%s", rendered, want)
	}
}

// TestAFaultCobolGoFindsInsideANestedMemberPointsIntoIt: an entry `copybook`
// refuses, two COPY statements deep, is reported in the member with both.
func TestAFaultCobolGoFindsInsideANestedMemberPointsIntoIt(t *testing.T) {
	t.Parallel()

	files := composedFiles()
	files["cpy/stdtrl.cpy"] = fixed(
		"    05  STD-TRAILER       PIC Q.",
	)

	path, dir := composedProject(t, composedLayout(composedForms, ""), files)

	_, rendered := loadFault(t, path, dir)

	if !strings.HasPrefix(rendered, `<dir>/orders.sexpr:8:22: the copybook "cpy/order-detail.cpy" could not be read`+"\n  cpy/stdtrl.cpy:1:33: ") {
		t.Errorf("the fault does not point into the member:\n%s", rendered)
	}

	if want := "\n  cpy/orddtl.cpy:2:11: copied by COPY STDTRL OF GLLIB\n  cpy/order-detail.cpy:3:11: copied by COPY ORDDTL"; !strings.HasSuffix(rendered, want) {
		t.Errorf("the fault does not carry the chain innermost first:\n%s", rendered)
	}
}
