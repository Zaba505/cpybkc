// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layout

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sexpr "github.com/z5labs/sexpr-go"
)

// TestParseBuildsAPositionedAST is the criterion this package exists for, and
// it is asserted as a rendering rather than as a struct literal on purpose:
// what has to be right is the shape *and* the position of every node, and a
// rendering carrying both fails with the whole tree in the message rather than
// with a diff of nested values.
func TestParseBuildsAPositionedAST(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "a form that is nothing but a tag",
			source: "(framing)",
			want:   "1:1 form framing",
		},
		{
			name:   "arguments and a child",
			source: "(record ORDER-HEADER\n  (copybook \"cpy/orders.cpy\" ORDER-HEADER-REC))",
			want: strings.Join([]string{
				"1:1 form record",
				"  1:9 symbol ORDER-HEADER",
				"  2:3 form copybook",
				"    2:13 text \"cpy/orders.cpy\"",
				"    2:30 symbol ORDER-HEADER-REC",
			}, "\n"),
		},
		{
			name:   "numbers keep the spelling they were written in",
			source: "(framing (lrecl 512) (drift -1.5))",
			want: strings.Join([]string{
				"1:1 form framing",
				"  1:10 form lrecl",
				"    1:17 int 512",
				"  1:22 form drift",
				"    1:29 float -1.5",
			}, "\n"),
		},
		{
			name:   "a form nested three deep",
			source: "(discriminate ORDER-HEADER (equals (item ORDER-HEADER OH-REC-TYPE) \"10\"))",
			want: strings.Join([]string{
				"1:1 form discriminate",
				"  1:15 symbol ORDER-HEADER",
				"  1:28 form equals",
				"    1:36 form item",
				"      1:42 symbol ORDER-HEADER",
				"      1:55 symbol OH-REC-TYPE",
				"    1:68 text \"10\"",
			}, "\n"),
		},
		{
			name:   "the layers are separate top-level forms",
			source: "(encoding (charset cp037))\n(framing (recfm VB))",
			want: strings.Join([]string{
				"1:1 form encoding",
				"  1:11 form charset",
				"    1:20 symbol cp037",
				"2:1 form framing",
				"  2:10 form recfm",
				"    2:17 symbol VB",
			}, "\n"),
		},
		{
			name:   "comments and blank lines are not nodes, and do not move the ones that are",
			source: ";; the record types.\n\n(record HEADER) ;; and its trailing note\n",
			want:   "3:1 form record\n  3:9 symbol HEADER",
		},
		{
			name:   "an empty layout has no forms",
			source: ";; nothing here yet.\n",
			want:   "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			file, err := Parse("layout.sexpr", strings.NewReader(testCase.source))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			if got := render(file); got != testCase.want {
				t.Errorf("parsed as\n%s\nwant\n%s", got, testCase.want)
			}
		})
	}
}

// TestEveryNodeCarriesTheFileItWasReadFrom is the half of the position that
// sexpr-go cannot supply, and the half #31's cross-file spans are built on: a
// position that cannot say which file it is in stops being usable exactly when
// there are two.
func TestEveryNodeCarriesTheFileItWasReadFrom(t *testing.T) {
	t.Parallel()

	file, err := Parse("orders.sexpr", strings.NewReader(specExample(t)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var nodes int

	for _, form := range file.Forms {
		Walk(form, func(node Node) bool {
			nodes++

			pos := node.Position()
			if pos.File != "orders.sexpr" || pos.Line < 1 || pos.Column < 1 {
				t.Errorf("%T at %+v carries no complete position", node, pos)
			}

			return true
		})
	}

	if nodes == 0 {
		t.Fatal("the walk visited no nodes, so the check above passed vacuously")
	}
}

// TestTheSpecsWorkedExampleParses is the staleness gate over the notation.
//
// docs/layout/SPEC.md's "A layout, end to end" appendix is the layout the
// document shows an adopter, and it is the one layout in this repository nobody
// wrote against this reader. Reading it out of the document rather than copying
// it here is the point: a change to the notation the appendix followed and the
// reader did not would otherwise be invisible until an adopter pasted the
// example into a file.
func TestTheSpecsWorkedExampleParses(t *testing.T) {
	t.Parallel()

	file, err := Parse("orders.sexpr", strings.NewReader(specExample(t)))
	if err != nil {
		t.Fatalf("the reader rejects SPEC.md's own worked example: %v", err)
	}

	// Every statement in the appendix is a top-level form, and the count is
	// stated here so that an example gaining or losing one is a failure rather
	// than something the walk above absorbs.
	if len(file.Forms) != 14 {
		t.Errorf("read %d top-level forms out of the appendix, want 14: %s", len(file.Forms), strings.Join(tags(file), ", "))
	}
}

// TestAFormsTagCarriesItsOwnPosition is what lets a diagnostic about a tag
// point at the tag. A form's own position is its opening parenthesis, and a
// reader sent there to find a misspelled word one column over has been sent to
// the wrong place.
func TestAFormsTagCarriesItsOwnPosition(t *testing.T) {
	t.Parallel()

	file, err := Parse("layout.sexpr", strings.NewReader("(framing\n  (recfm VB))"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	framing := file.Forms[0]
	if framing.Pos.Column != 1 || framing.TagPos.Column != 2 {
		t.Errorf("framing opens at column %d with its tag at column %d, want 1 and 2", framing.Pos.Column, framing.TagPos.Column)
	}

	recfm, ok := framing.Elements[0].(Form)
	if !ok {
		t.Fatalf("the child of framing is %T, want a Form", framing.Elements[0])
	}

	if recfm.Pos.Line != 2 || recfm.Pos.Column != 3 || recfm.TagPos.Column != 4 {
		t.Errorf("recfm opens at %s with its tag at %s, want 2:3 and 2:4", recfm.Pos, recfm.TagPos)
	}
}

// TestParseRejectsConstructsWithNoMeaningInALayout covers docs/layout/SPEC.md's
// "What this document delegates" — the legal S-expressions the format excludes
// by name. Each is rejected rather than dropped, and each names the construct:
// a construct with no meaning that nevertheless parses is one two generators
// will emit differently, and there is nothing for a diagnostic to say about it.
func TestParseRejectsConstructsWithNoMeaningInALayout(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		source    string
		construct Construct
		pos       Pos
	}{
		{
			name:      "an improper list at the top level",
			source:    "(record . ORDER-HEADER)",
			construct: ConstructImproperList,
			pos:       Pos{File: "layout.sexpr", Line: 1, Column: 1},
		},
		{
			name:      "an improper list inside a form",
			source:    "(record ORDER-HEADER\n  (copybook . \"cpy/orders.cpy\"))",
			construct: ConstructImproperList,
			pos:       Pos{File: "layout.sexpr", Line: 2, Column: 3},
		},
		{
			name:      "a quote shorthand where a value belongs",
			source:    "(record 'ORDER-HEADER)",
			construct: ConstructQuoteShorthand,
			pos:       Pos{File: "layout.sexpr", Line: 1, Column: 9},
		},
		{
			name:      "a quasiquote",
			source:    "(record `ORDER-HEADER)",
			construct: ConstructQuoteShorthand,
			pos:       Pos{File: "layout.sexpr", Line: 1, Column: 9},
		},
		{
			// The tag position is held to the same exclusions as every other,
			// so an excluded construct written there is reported as the
			// construct it is rather than as a form that opens with something
			// odd.
			name:      "a quote shorthand where the tag belongs",
			source:    "('record ORDER-HEADER)",
			construct: ConstructQuoteShorthand,
			pos:       Pos{File: "layout.sexpr", Line: 1, Column: 2},
		},
		{
			name:      "a boolean where the tag belongs",
			source:    "(#t ORDER-HEADER)",
			construct: ConstructBoolean,
			pos:       Pos{File: "layout.sexpr", Line: 1, Column: 2},
		},
		{
			name:      "the empty list where the tag belongs",
			source:    "(() ORDER-HEADER)",
			construct: ConstructEmptyList,
			pos:       Pos{File: "layout.sexpr", Line: 1, Column: 2},
		},
		{
			name:      "an improper list where the tag belongs",
			source:    "((record . ORDER-HEADER) HEADER)",
			construct: ConstructImproperList,
			pos:       Pos{File: "layout.sexpr", Line: 1, Column: 2},
		},
		{
			name:      "nil where the tag belongs",
			source:    "(nil ORDER-HEADER)",
			construct: ConstructNil,
			pos:       Pos{File: "layout.sexpr", Line: 1, Column: 2},
		},
		{
			name:      "nil where a child was omitted",
			source:    "(framing (recfm VB) nil)",
			construct: ConstructNil,
			pos:       Pos{File: "layout.sexpr", Line: 1, Column: 21},
		},
		{
			name:      "the empty list",
			source:    "(framing ())",
			construct: ConstructEmptyList,
			pos:       Pos{File: "layout.sexpr", Line: 1, Column: 10},
		},
		{
			name:      "a boolean where a closed value set belongs",
			source:    "(framing (blocks #t))",
			construct: ConstructBoolean,
			pos:       Pos{File: "layout.sexpr", Line: 1, Column: 18},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			file, err := Parse("layout.sexpr", strings.NewReader(testCase.source))
			if err == nil {
				t.Fatalf("the reader accepted %q, which carries %s", testCase.source, testCase.construct)
			}

			if file != nil {
				t.Errorf("a rejected layout came back with an AST anyway")
			}

			var construct *ConstructError
			if !errors.As(err, &construct) {
				t.Fatalf("the error is %T (%v), want a *ConstructError", err, err)
			}

			if construct.Construct != testCase.construct {
				t.Errorf("reported %s, want %s", construct.Construct, testCase.construct)
			}

			if construct.Pos != testCase.pos {
				t.Errorf("reported at %s, want %s", construct.Pos, testCase.pos)
			}
		})
	}
}

// TestParseRejectsWhatCannotBeAForm covers the two shapes that parse as
// S-expressions and are not statements: a top-level node that is not a form,
// and a form with nothing naming it. Neither has a node in this AST, so
// accepting one would mean inventing a node nothing downstream can read.
func TestParseRejectsWhatCannotBeAForm(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		assert func(*testing.T, error)
	}{
		{
			name:   "a bare symbol at the top level",
			source: "(framing)\nORDER-HEADER",
			assert: func(t *testing.T, err error) {
				t.Helper()
				assertNotAForm(t, err, "a symbol", Pos{File: "layout.sexpr", Line: 2, Column: 1})
			},
		},
		{
			name:   "text at the top level",
			source: "\"cpy/orders.cpy\"",
			assert: func(t *testing.T, err error) {
				t.Helper()
				assertNotAForm(t, err, "text", Pos{File: "layout.sexpr", Line: 1, Column: 1})
			},
		},
		{
			name:   "a number at the top level",
			source: "512",
			assert: func(t *testing.T, err error) {
				t.Helper()
				assertNotAForm(t, err, "a number", Pos{File: "layout.sexpr", Line: 1, Column: 1})
			},
		},
		{
			name:   "a form opening with a number",
			source: "(1 2)",
			assert: func(t *testing.T, err error) {
				t.Helper()
				assertUntagged(t, err, "a number", Pos{File: "layout.sexpr", Line: 1, Column: 2})
			},
		},
		{
			name:   "a form opening with text",
			source: "(\"record\" ORDER-HEADER)",
			assert: func(t *testing.T, err error) {
				t.Helper()
				assertUntagged(t, err, "text", Pos{File: "layout.sexpr", Line: 1, Column: 2})
			},
		},
		{
			name:   "a child form opening with a form",
			source: "(framing ((recfm) VB))",
			assert: func(t *testing.T, err error) {
				t.Helper()
				assertUntagged(t, err, "a form", Pos{File: "layout.sexpr", Line: 1, Column: 11})
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			file, err := Parse("layout.sexpr", strings.NewReader(testCase.source))
			if err == nil {
				t.Fatalf("the reader accepted %q", testCase.source)
			}

			if file != nil {
				t.Errorf("a rejected layout came back with an AST anyway")
			}

			testCase.assert(t, err)
		})
	}
}

// TestAnErrorSaysWhatItFoundAndWhere holds the messages themselves, because
// they are what an adopter reads. docs/layout/SPEC.md requires that a
// diagnostic name what it found and where and not merely report that a layout
// is invalid, so the wording is under test rather than left to whatever the
// fields happen to render as.
func TestAnErrorSaysWhatItFoundAndWhere(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "an improper list",
			source: "(record . ORDER-HEADER)",
			want:   "layout.sexpr:1:1: an improper list has no meaning in a layout",
		},
		{
			name:   "a quote shorthand",
			source: "(record 'ORDER-HEADER)",
			want:   "layout.sexpr:1:9: a quote shorthand has no meaning in a layout",
		},
		{
			name:   "nil",
			source: "(framing (recfm VB) nil)",
			want:   "layout.sexpr:1:21: nil has no meaning in a layout; absence is expressed by omitting a child",
		},
		{
			name:   "the empty list",
			source: "(framing ())",
			want:   "layout.sexpr:1:10: the empty list has no meaning in a layout; every form has a tag",
		},
		{
			name:   "a boolean",
			source: "(framing (blocks #t))",
			want:   "layout.sexpr:1:18: a boolean has no meaning in a layout",
		},
		{
			name:   "a number at the top level",
			source: "512",
			want:   "layout.sexpr:1:1: a layout is a set of forms, and this is a number",
		},
		{
			name:   "a form with nothing naming it",
			source: "(\"record\" ORDER-HEADER)",
			want:   "layout.sexpr:1:2: a form opens with a symbol naming it, and this one opens with text",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse("layout.sexpr", strings.NewReader(testCase.source))
			if err == nil {
				t.Fatalf("the reader accepted %q", testCase.source)
			}

			if got := err.Error(); got != testCase.want {
				t.Errorf("said\n%s\nwant\n%s", got, testCase.want)
			}
		})
	}
}

// TestParseReportsEveryFaultRatherThanTheFirst is why the errors are joined. A
// generated layout is generated wrong in the same way in many places at once,
// and a reader reporting one fault per run is a reader run once per fault.
func TestParseReportsEveryFaultRatherThanTheFirst(t *testing.T) {
	t.Parallel()

	const source = "(framing (blocks #t) ())\nORDER-HEADER\n(record 'HEADER)"

	_, err := Parse("layout.sexpr", strings.NewReader(source))
	if err == nil {
		t.Fatal("the reader accepted a layout with four faults in it")
	}

	faults := faults(err)
	if len(faults) != 4 {
		t.Fatalf("reported %d faults, want 4:\n%v", len(faults), err)
	}

	// Every one of them is still assertable on its own, which is what
	// errors.Join buys and what a single concatenated message would not.
	for _, fault := range faults {
		var (
			construct *ConstructError
			notAForm  *NotAFormError
		)

		if !errors.As(fault, &construct) && !errors.As(fault, &notAForm) {
			t.Errorf("fault %v is neither a *ConstructError nor a *NotAFormError", fault)
		}
	}
}

// TestParseFailsOnSourceTheGrammarRejects keeps a grammatical failure the
// grammar's. It is reported as one error rather than beside a list of forms,
// because it leaves no forms — and the grammar's own error stays reachable,
// since docs/layout/SPEC.md delegates what a symbol is and where one ends and
// this repository does not restate it.
func TestParseFailsOnSourceTheGrammarRejects(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		pos    Pos
	}{
		{
			// The position is the last token the grammar read, which is where
			// it ran out rather than where the parenthesis is missing. It is
			// the grammar's to say: docs/layout/SPEC.md delegates the parse and
			// the positions on it, and this package carries them rather than
			// improving on them.
			name:   "a form nobody closed",
			source: "(record ORDER-HEADER",
			pos:    Pos{File: "layout.sexpr", Line: 1, Column: 9},
		},
		{
			name:   "a string nobody closed",
			source: "(copybook \"cpy/orders.cpy)",
			pos:    Pos{File: "layout.sexpr", Line: 1, Column: 11},
		},
		{
			name:   "a closing parenthesis with nothing open",
			source: "(framing))",
			pos:    Pos{File: "layout.sexpr", Line: 1, Column: 10},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			file, err := Parse("layout.sexpr", strings.NewReader(testCase.source))
			if err == nil {
				t.Fatalf("the reader accepted %q", testCase.source)
			}

			if file != nil {
				t.Errorf("a rejected layout came back with an AST anyway")
			}

			var syntax *SyntaxError
			if !errors.As(err, &syntax) {
				t.Fatalf("the error is %T (%v), want a *SyntaxError", err, err)
			}

			if syntax.Pos != testCase.pos {
				t.Errorf("reported at %s, want %s", syntax.Pos, testCase.pos)
			}

			if syntax.Unwrap() == nil {
				t.Error("the grammar's own error was dropped")
			}

			if !strings.Contains(syntax.Error(), testCase.pos.String()) {
				t.Errorf("the message %q does not open with the position", syntax.Error())
			}
		})
	}
}

// TestASyntaxErrorKeepsTheGrammarsError asserts what the position on a
// SyntaxError is a summary of: the error sexpr-go returned survives, so a
// caller that wants to know which token was unexpected can ask.
func TestASyntaxErrorKeepsTheGrammarsError(t *testing.T) {
	t.Parallel()

	_, err := Parse("layout.sexpr", strings.NewReader("(record ORDER-HEADER"))

	var unexpected sexpr.UnexpectedEndOfTokensError
	if !errors.As(err, &unexpected) {
		t.Fatalf("the grammar's error did not survive: %v", err)
	}

	if unexpected.Pos.Line != 1 {
		t.Errorf("the grammar reported line %d, want 1", unexpected.Pos.Line)
	}
}

// TestParseFileNamesTheFileItRead is the one thing an in-memory reader cannot
// cover, and the reason ParseFile exists rather than being left to every
// caller: the name a position carries is the path the layout was opened at.
func TestParseFileNamesTheFileItRead(t *testing.T) {
	t.Parallel()

	path := filepath.Join("testdata", "line-sequential.sexpr")

	file, err := ParseFile(path)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	if file.Name != path {
		t.Errorf("the file is named %q, want %q", file.Name, path)
	}

	if file.Start() != (Pos{File: path, Line: 1, Column: 1}) {
		t.Errorf("the start of the file is %s, want %s:1:1", file.Start(), path)
	}

	for _, form := range file.Forms {
		Walk(form, func(node Node) bool {
			if node.Position().File != path {
				t.Errorf("%T at %s does not name the file it was read from", node, node.Position())
			}

			return true
		})
	}
}

// TestParseFileFailsOnAFileThatIsNotThere reports the open failure rather than
// an empty layout, which is the difference between a layout that says nothing
// and a layout nobody found.
func TestParseFileFailsOnAFileThatIsNotThere(t *testing.T) {
	t.Parallel()

	_, err := ParseFile(filepath.Join("testdata", "no-such-layout.sexpr"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("opening a layout that is not there failed with %v, want it to be an os.ErrNotExist", err)
	}
}

// TestPosRenders covers the spelling a diagnostic is read in. A position with
// no file renders as line and column alone rather than as a leading colon,
// because a caller that named nothing is not naming a file called "".
func TestPosRenders(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		pos  Pos
		want string
	}{
		{name: "a named file", pos: Pos{File: "orders.sexpr", Line: 12, Column: 3}, want: "orders.sexpr:12:3"},
		{name: "no file", pos: Pos{Line: 12, Column: 3}, want: "12:3"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.pos.String(); got != testCase.want {
				t.Errorf("rendered as %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestWalkStopsWhereItIsTold keeps the walk usable for a search rather than
// only for a census: a caller that has found what it came for should not pay
// for the subtree under it.
func TestWalkStopsWhereItIsTold(t *testing.T) {
	t.Parallel()

	file, err := Parse("layout.sexpr", strings.NewReader("(discriminate HEADER (equals (item HEADER H-TYPE) \"H\"))"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var visited int

	Walk(file.Forms[0], func(node Node) bool {
		visited++

		form, ok := node.(Form)

		return !ok || form.Tag != "equals"
	})

	// discriminate, HEADER and equals — and nothing under equals.
	if visited != 3 {
		t.Errorf("visited %d nodes, want 3", visited)
	}
}

// TestConstructsAreNamed keeps the enumeration's spelling under test, because
// the names are what a message says it found rather than an internal label.
func TestConstructsAreNamed(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		construct Construct
		want      string
	}{
		{construct: ConstructImproperList, want: "an improper list"},
		{construct: ConstructQuoteShorthand, want: "a quote shorthand"},
		{construct: ConstructNil, want: "nil"},
		{construct: ConstructEmptyList, want: "the empty list"},
		{construct: ConstructBoolean, want: "a boolean"},
		{construct: Construct(42), want: "Construct(42)"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.want, func(t *testing.T) {
			t.Parallel()

			if got := testCase.construct.String(); got != testCase.want {
				t.Errorf("named %q, want %q", got, testCase.want)
			}
		})
	}
}

// assertNotAForm asserts that err reports a node written where only a form
// belongs.
func assertNotAForm(t *testing.T, err error, found string, pos Pos) {
	t.Helper()

	var notAForm *NotAFormError
	if !errors.As(err, &notAForm) {
		t.Fatalf("the error is %T (%v), want a *NotAFormError", err, err)
	}

	if notAForm.Found != found {
		t.Errorf("reported %q, want %q", notAForm.Found, found)
	}

	if notAForm.Pos != pos {
		t.Errorf("reported at %s, want %s", notAForm.Pos, pos)
	}
}

// assertUntagged asserts that err reports a form with nothing naming it.
func assertUntagged(t *testing.T, err error, found string, pos Pos) {
	t.Helper()

	var untagged *UntaggedFormError
	if !errors.As(err, &untagged) {
		t.Fatalf("the error is %T (%v), want an *UntaggedFormError", err, err)
	}

	if untagged.Found != found {
		t.Errorf("reported %q, want %q", untagged.Found, found)
	}

	if untagged.Pos != pos {
		t.Errorf("reported at %s, want %s", untagged.Pos, pos)
	}
}

// faults splits a joined error back into the faults it was built from.
func faults(err error) []error {
	var joined interface{ Unwrap() []error }
	if errors.As(err, &joined) {
		return joined.Unwrap()
	}

	return []error{err}
}

// render draws an AST as one line per node, indented by depth, carrying the
// position of every one.
func render(file *File) string {
	var lines []string

	for _, form := range file.Forms {
		lines = append(lines, renderNode(form, 0)...)
	}

	return strings.Join(lines, "\n")
}

// renderNode draws one node and everything under it.
func renderNode(node Node, depth int) []string {
	indent := strings.Repeat("  ", depth)
	pos := fmt.Sprintf("%d:%d", node.Position().Line, node.Position().Column)

	switch node := node.(type) {
	case Form:
		lines := []string{fmt.Sprintf("%s%s form %s", indent, pos, node.Tag)}
		for _, element := range node.Elements {
			lines = append(lines, renderNode(element, depth+1)...)
		}

		return lines
	case Symbol:
		return []string{fmt.Sprintf("%s%s symbol %s", indent, pos, node.Value)}
	case Text:
		return []string{fmt.Sprintf("%s%s text %q", indent, pos, node.Value)}
	case Int:
		return []string{fmt.Sprintf("%s%s int %d", indent, pos, node.Value)}
	case Float:
		return []string{fmt.Sprintf("%s%s float %v", indent, pos, node.Value)}
	default:
		return []string{fmt.Sprintf("%s%s %T", indent, pos, node)}
	}
}

// tags names a layout's top-level forms, for a failure message about how many
// of them there are.
func tags(file *File) []string {
	names := make([]string, 0, len(file.Forms))
	for _, form := range file.Forms {
		names = append(names, form.Tag)
	}

	return names
}

// specExample returns docs/layout/SPEC.md's worked example.
func specExample(t *testing.T) string {
	t.Helper()

	return fencedBlock(t, section(t, "## Appendix: A layout, end to end"))
}

// fencedBlock returns the first fenced code block in body.
func fencedBlock(t *testing.T, body string) string {
	t.Helper()

	var (
		block []string
		open  bool
	)

	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if open {
				return strings.Join(block, "\n")
			}

			open = true

			continue
		}

		if open {
			block = append(block, line)
		}
	}

	t.Fatal("no fenced code block found")

	return ""
}

// section returns the text of SPEC.md under heading, up to the next heading at
// the same level or above.
func section(t *testing.T, heading string) string {
	t.Helper()

	level := strings.IndexFunc(heading, func(r rune) bool { return r != '#' })

	var (
		body  []string
		found bool
	)

	for _, line := range strings.Split(specText(t), "\n") {
		if line == heading {
			found = true

			continue
		}

		if !found {
			continue
		}

		if strings.HasPrefix(line, "#") && strings.IndexFunc(line, func(r rune) bool { return r != '#' }) <= level {
			break
		}

		body = append(body, line)
	}

	if !found {
		t.Fatalf("the layout SPEC has no %q section", heading)
	}

	return strings.Join(body, "\n")
}

// specText reads docs/layout/SPEC.md.
func specText(t *testing.T) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "layout", "SPEC.md"))
	if err != nil {
		t.Fatalf("read the layout SPEC: %v", err)
	}

	return string(b)
}

// repoRoot walks up from the test's working directory to the directory holding
// go.mod, which for this module is the repository root and so the directory
// docs/ sits in.
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
			t.Fatal("no go.mod above the test's working directory")
		}

		dir = parent
	}
}
