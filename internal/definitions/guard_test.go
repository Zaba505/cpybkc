// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package definitions_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// cobolModule is the module whose grammar an entry body transcribes.
const cobolModule = "github.com/Zaba505/cobol-go"

// procedureOnly are the value kinds `cobol-go` gives a `cobol()` method that
// are procedure-division operands. No data-description entry its parser builds
// holds one, so the writer has no form for them and refuses one if it is ever
// handed it.
var procedureOnly = []string{"AddressOf", "FunctionReference", "Identifier"}

// fingerprinted are the types a transcription is made of besides the clauses:
// a change to the shape of any of them is a change to what the notation has to
// say.
var fingerprinted = []string{
	"AllLiteral",
	"DataDescriptionEntry",
	"Fragment",
	"NumericLiteral",
	"OccursKey",
	"StringLiteral",
	"ValueSpec",
	"Word",
}

// fingerprint is the field shape of every clause struct and every type in
// fingerprinted, at the pinned `cobol-go`. It is written out rather than
// derived so that a pin bump reshaping any of them fails here until someone has
// reviewed the notation against it, which CONTRIBUTING's "Bumping the cobol-go
// pin" requires in the same change.
const fingerprint = `AllLiteral{Pos Pos; Literal Type}
BlankWhenZeroClause{Pos Pos}
DataDescriptionEntry{Pos Pos; Comments []*Comment; Level int; Name *Word; Filler bool; Clauses []DataClause}
ExternalClause{Pos Pos}
Fragment{Entries []*DataDescriptionEntry; Trailing []*Comment}
GlobalClause{Pos Pos}
JustifiedClause{Pos Pos}
NumericLiteral{Pos Pos; Value string}
OccursClause{Pos Pos; Min *NumericLiteral; Max *NumericLiteral; DependingOn *Word; Keys []OccursKey; IndexedBy *Word}
OccursKey{Pos Pos; Ascending bool; Name *Word}
PictureClause{Pos Pos; Picture string}
RedefinesClause{Pos Pos; Name *Word}
RenamesClause{Pos Pos; From *Word; Through *Word}
SignClause{Pos Pos; Position string; Separate bool}
StringLiteral{Pos Pos; Kind LiteralKind; Value string}
SynchronizedClause{Pos Pos; Direction string}
UsageClause{Pos Pos; Usage string}
ValueClause{Pos Pos; Values []ValueSpec}
ValueSpec{From Type; Through Type}
Word{Pos Pos; Value string}`

// TestTheWriterKnowsEveryClause reads `cobol-go`'s source at the pinned
// version and the writer's own, and holds the one to the other: every type
// with a `dataClause()` method is a case of the writer's clause switch, and
// every type with a `cobol()` method is a case of its operand switch or a
// procedure-division operand. A pin bump that adds a clause kind or a value
// kind fails here, before any copybook using it is converted without it.
func TestTheWriterKnowsEveryClause(t *testing.T) {
	t.Parallel()

	source := cobolSource(t)
	handled := writerCases(t)

	clauses := methodHolders(source, "dataClause")
	if len(clauses) == 0 {
		t.Fatal("cobol-go declares no type with a dataClause() method; the guard is reading the wrong source")
	}

	for _, clause := range clauses {
		if !slices.Contains(handled, clause) {
			t.Errorf("cobol-go's %s is a data clause the writer has no case for", clause)
		}
	}

	for _, value := range methodHolders(source, "cobol") {
		if !slices.Contains(handled, value) && !slices.Contains(procedureOnly, value) {
			t.Errorf("cobol-go's %s is a value the writer has no case for and is not on the procedure-division list", value)
		}
	}
}

// TestTheNotationsTypesHaveNotChanged holds every clause struct, and the types
// a transcription is made of, to the fingerprint taken when the notation was
// last reviewed.
func TestTheNotationsTypesHaveNotChanged(t *testing.T) {
	t.Parallel()

	source := cobolSource(t)

	names := append(methodHolders(source, "dataClause"), fingerprinted...)
	sort.Strings(names)

	lines := make([]string, 0, len(names))

	for _, name := range names {
		shape, ok := structShape(source, name)
		if !ok {
			t.Errorf("cobol-go declares no struct %s", name)

			continue
		}

		lines = append(lines, name+shape)
	}

	if got := strings.Join(lines, "\n"); got != fingerprint {
		t.Errorf("the pinned cobol-go's entry types are not the ones the notation was reviewed against; "+
			"extend the notation, schema/layout.sexpr and the kitchen sink as CONTRIBUTING's \"Bumping the cobol-go pin\" says, then update the fingerprint.\ngot:\n%s\nwant:\n%s", got, fingerprint)
	}
}

// cobolSource parses package cobol, without its tests, from the module
// directory the build resolves the pinned version to.
func cobolSource(t *testing.T) []*ast.File {
	t.Helper()

	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", cobolModule).Output()
	if err != nil {
		t.Fatalf("locate %s: %v", cobolModule, err)
	}

	dir := strings.TrimSpace(string(out))
	if dir == "" {
		t.Fatalf("go list names no directory for %s", cobolModule)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("list %s: %v", dir, err)
	}

	var files []*ast.File

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		if file.Name.Name == "cobol" {
			files = append(files, file)
		}
	}

	if len(files) == 0 {
		t.Fatalf("%s holds no package cobol", dir)
	}

	return files
}

// methodHolders is every type with a niladic method of that name, sorted.
func methodHolders(files []*ast.File, method string) []string {
	var names []string

	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != method || len(fn.Recv.List) != 1 {
				continue
			}

			receiver := fn.Recv.List[0].Type
			if star, ok := receiver.(*ast.StarExpr); ok {
				receiver = star.X
			}

			if ident, ok := receiver.(*ast.Ident); ok && !slices.Contains(names, ident.Name) {
				names = append(names, ident.Name)
			}
		}
	}

	sort.Strings(names)

	return names
}

// structShape is a struct's fields, `{Name Type; …}`, or false when no struct
// of that name is declared.
func structShape(files []*ast.File, name string) (string, bool) {
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}

			for _, spec := range gen.Specs {
				typeSpec := spec.(*ast.TypeSpec)
				if typeSpec.Name.Name != name {
					continue
				}

				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					return "", false
				}

				var fields []string

				for _, field := range structType.Fields.List {
					kind := types.ExprString(field.Type)

					if len(field.Names) == 0 {
						fields = append(fields, kind)
					}

					for _, fieldName := range field.Names {
						fields = append(fields, fieldName.Name+" "+kind)
					}
				}

				return "{" + strings.Join(fields, "; ") + "}", true
			}
		}
	}

	return "", false
}

// writerCases is every `*cobol.<Type>` the writer's type switches have a case
// for, read from write.go.
func writerCases(t *testing.T) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "write.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse write.go: %v", err)
	}

	var cases []string

	ast.Inspect(file, func(node ast.Node) bool {
		clause, ok := node.(*ast.CaseClause)
		if !ok {
			return true
		}

		for _, expr := range clause.List {
			star, ok := expr.(*ast.StarExpr)
			if !ok {
				continue
			}

			selector, ok := star.X.(*ast.SelectorExpr)
			if !ok {
				continue
			}

			if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "cobol" {
				cases = append(cases, selector.Sel.Name)
			}
		}

		return true
	})

	if len(cases) == 0 {
		t.Fatal("write.go has no case for any cobol type; the guard is reading the wrong file")
	}

	return cases
}
