// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package goadapter

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// decodeMethod is the method every record type a Go generator emits carries:
// codec's own Unmarshaler, which is what a caller holding a codec.Reader of its
// own reads one record through.
const decodeMethod = "UnmarshalCOBOL"

// recordTypes is every type of the generated package in dir that a record is
// read into, named as the package declares it — in the order their decoders are
// declared, file by file in the order of the files' names. Nothing depends on
// that order beyond two runs over one package asking in the same one: every
// type is asked, and every one of them has to refuse.
//
// A codec program needs them for one question: whether the generated code
// refuses a binary width staircase that is not its descriptor's. The package's
// own reader cannot be handed one — it takes an option per axis a layout
// states, and none for the staircase — so the one road a staircase reaches it
// by is a caller's own codec.Reader handed to a record's decoder, and asking
// that needs a record to hand it to before any file is read
// (docs/ir/SPEC.md, "The staircase is not an axis a consumer may replace").
//
// The names are read out of the generated source rather than munged out of the
// descriptor, for the reason the codec program pairs a type with a record node
// by folding both names rather than by munging one: a second copy of the
// generator's munging rule would agree with it only until the rule changed.
func recordTypes(dir string) ([]string, error) {
	listing, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read the generated package: %w", err)
	}

	var (
		found []string
		files = token.NewFileSet()
	)

	for _, item := range listing {
		name := item.Name()
		if item.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(files, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("failed to read the generated package: %w", err)
		}

		for _, decl := range file.Decls {
			if named := decoder(decl); named != "" && !slices.Contains(found, named) {
				found = append(found, named)
			}
		}
	}

	return found, nil
}

// decoder is the exported type a declaration is the record decoder of, or the
// empty string where it is not one: a method named [decodeMethod] on a pointer
// to a type of the package.
func decoder(decl ast.Decl) string {
	fn, ok := decl.(*ast.FuncDecl)
	if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || fn.Name.Name != decodeMethod {
		return ""
	}

	pointer, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return ""
	}

	named, ok := pointer.X.(*ast.Ident)
	if !ok || !named.IsExported() {
		return ""
	}

	return named.Name
}
