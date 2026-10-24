// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/copymember"
)

// TestParseReadsEveryCopyMember: --copy-member repeats, in both value forms,
// and each occurrence is the member and the path as typed.
func TestParseReadsEveryCopyMember(t *testing.T) {
	t.Parallel()

	inv, err := parse([]string{
		initSubcommand,
		copybookFlag, "cpy/order-detail.cpy",
		copyMemberFlag, "STDHDR=cpy/stdhdr.cpy",
		copyMemberFlag + "=STDTRL OF GLLIB=cpy/stdtrl.cpy",
		outFlag, "orders.sexpr",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	want := []copyMember{
		{value: "STDHDR=cpy/stdhdr.cpy", member: copymember.Member{Name: "STDHDR"}, path: "cpy/stdhdr.cpy"},
		{value: "STDTRL OF GLLIB=cpy/stdtrl.cpy", member: copymember.Member{Name: "STDTRL", Library: "GLLIB"}, path: "cpy/stdtrl.cpy"},
	}
	if !slices.Equal(inv.copyMembers, want) {
		t.Errorf("the members are %+v, want %+v", inv.copyMembers, want)
	}
}

// TestParseRefusesACopyMemberTheSpecRefuses: each a usage error naming the value
// as typed, decided before anything is opened.
func TestParseRefusesACopyMemberTheSpecRefuses(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		values []string
		says   []string
	}{
		{"no equals", []string{"STDHDR"}, []string{`"STDHDR"`, "no ="}},
		{"an equals only inside a literal", []string{"'A=B'"}, []string{`"'A=B'"`, "no ="}},
		{"an empty member", []string{"=cpy/stdhdr.cpy"}, []string{`"=cpy/stdhdr.cpy"`, "member"}},
		{"a malformed member", []string{"STD HDR=x.cpy"}, []string{`"STD HDR=x.cpy"`, "not a COBOL word or literal"}},
		{"an empty path", []string{"STDHDR="}, []string{`"STDHDR="`, "path"}},
		{"the path -", []string{"STDHDR=-"}, []string{`"STDHDR=-"`, "stream"}},
		{"two equal members", []string{"STDHDR=a.cpy", "'stdhdr'=b.cpy"}, []string{`"STDHDR=a.cpy"`, `"'stdhdr'=b.cpy"`, "one member"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			args := []string{initSubcommand, copybookFlag, "order.cpy"}
			for _, value := range test.values {
				args = append(args, copyMemberFlag, value)
			}

			args = append(args, outFlag, "orders.sexpr")

			_, err := parse(args)
			if err == nil {
				t.Fatal("parse accepted the line")
			}

			for _, want := range test.says {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the usage error %q does not say %s", err, want)
				}
			}

			if code := statusOf(err); code != statusUsage {
				t.Errorf("the line exits %d, want %d", code, statusUsage)
			}
		})
	}
}

// TestACopyMemberUnderTheDefaultActionIsInits: a real flag in the wrong place.
func TestACopyMemberUnderTheDefaultActionIsInits(t *testing.T) {
	t.Parallel()

	_, err := parse([]string{copyMemberFlag, "STDHDR=cpy/stdhdr.cpy"})
	if err == nil || !strings.Contains(err.Error(), initSubcommand) {
		t.Errorf("--copy-member under the default action reads as %v, want it named as init's", err)
	}
}

// TestInitConvertsAComposedCopybookAsItsExpansion end to end: the definitions
// file holds the expanded entries, a member gets no file of its own, and a
// member no COPY names is a note on standard error.
func TestInitConvertsAComposedCopybookAsItsExpansion(t *testing.T) {
	t.Parallel()

	in := t.TempDir()

	writeFile(t, filepath.Join(in, "order-detail.cpy"), "       01  ORDER-DETAIL-REC.\n           COPY STDHDR.\n           05  OD-QTY PIC 9(3).\n")
	writeFile(t, filepath.Join(in, "stdhdr.cpy"), "           05  STD-REC-TYPE PIC X.\n")

	out := t.TempDir()

	_, stderr, code := invoke(initSubcommand,
		copybookFlag, filepath.Join(in, "order-detail.cpy"),
		copyMemberFlag, "STDHDR="+filepath.Join(in, "stdhdr.cpy"),
		copyMemberFlag, "UNUSED="+filepath.Join(in, "unused.cpy"),
		outFlag, filepath.Join(out, "orders.sexpr"))

	if code != statusOK {
		t.Fatalf("`cpybkc %s` exited %d, want %d:\n%s", initSubcommand, code, statusOK, stderr)
	}

	if !strings.Contains(stderr, severityNote+severitySeparator+"--copy-member UNUSED=") {
		t.Errorf("standard error does not note the member no COPY names:\n%s", stderr)
	}

	transcribed, err := os.ReadFile(filepath.Join(out, "order-detail.cpy.sexpr"))
	if err != nil {
		t.Fatalf("reading back the definitions file: %v", err)
	}

	const want = "(entry 01 ORDER-DETAIL-REC)\n  (entry 05 STD-REC-TYPE (pic \"X\"))\n  (entry 05 OD-QTY (pic \"9(3)\"))\n"
	if !strings.HasSuffix(string(transcribed), want) {
		t.Errorf("the definitions file reads\n%s\nwant it to end\n%s", transcribed, want)
	}

	if left := entriesIn(t, out); !slices.Equal(left, []string{"order-detail.cpy.sexpr", "orders.sexpr"}) {
		t.Errorf("the run left %v, want the layout and the copybook's definitions file only", left)
	}
}

// TestInitRefusesACopyNamingNoMemberAndWritesNothing, with the note about the
// misspelled member beside the refusal.
func TestInitRefusesACopyNamingNoMemberAndWritesNothing(t *testing.T) {
	t.Parallel()

	in := t.TempDir()

	writeFile(t, filepath.Join(in, "order-detail.cpy"), "       01  ORDER-DETAIL-REC.\n           COPY STDHDR.\n")
	writeFile(t, filepath.Join(in, "stdhdr.cpy"), "           05  STD-REC-TYPE PIC X.\n")

	out := t.TempDir()
	copybook := filepath.Join(in, "order-detail.cpy")

	_, stderr, code := invoke(initSubcommand,
		copybookFlag, copybook,
		copyMemberFlag, "STDHRD="+filepath.Join(in, "stdhdr.cpy"),
		outFlag, filepath.Join(out, "orders.sexpr"))

	if code != statusFailed {
		t.Fatalf("`cpybkc %s` exited %d, want %d:\n%s", initSubcommand, code, statusFailed, stderr)
	}

	for _, want := range []string{
		copybook + ":2:12: COPY STDHDR names no --copy-member on the command line",
		"the command line names STDHRD",
		severityNote + severitySeparator + "--copy-member STDHRD=",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("standard error does not carry %q:\n%s", want, stderr)
		}
	}

	if left := entriesIn(t, out); len(left) != 0 {
		t.Errorf("a refused run left %v", left)
	}
}
