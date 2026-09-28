// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/irpb"
)

// optionGoldens is every golden package that declares a file-level reader and
// writer, by directory.
func optionGoldens() map[string]func() *irpb.Descriptor {
	all := map[string]func() *irpb.Descriptor{goldenDir: ordersDescriptor}
	maps.Copy(all, machineGoldens)

	return all
}

// generatedFileMachine is the parsed [fileMachineFile] generate writes for d.
func generatedFileMachine(t *testing.T, dir string, d *irpb.Descriptor) (*ast.File, string) {
	t.Helper()

	out := t.TempDir()
	name := dir[strings.LastIndex(dir, "/")+1:]

	if err := generate(io.Discard, d, out, options{packageName: name, importPath: goldenModule + dir}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	source, ok := written(t, out)[fileMachineFile]
	if !ok {
		t.Fatalf("no %s was generated", fileMachineFile)
	}

	file, err := parser.ParseFile(token.NewFileSet(), fileMachineFile, source, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing the generated %s: %v", fileMachineFile, err)
	}

	return file, source
}

// TestTheOptionsAreOnePerAxisALayoutStates holds the shape #381 settled, over
// every golden: an option type whose only field is unexported, so that no
// caller builds one but through the functions declared for it; exactly four of
// those, one per axis a layout states, each setting that axis of the encoding
// and nothing else; and none for the binary width staircase or replacing the
// whole encoding. A fifth function returning an option — one for the
// staircase, or one taking a codec.Encoding — fails here by being there.
func TestTheOptionsAreOnePerAxisALayoutStates(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		withCharsetFunc:   "Charset",
		withSignFunc:      "Sign",
		withByteOrderFunc: "ByteOrder",
		withFloatFunc:     "Float",
	}

	for dir, descriptor := range optionGoldens() {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()

			file, _ := generatedFileMachine(t, dir, descriptor())

			got := make(map[string]string)

			for _, decl := range file.Decls {
				switch decl := decl.(type) {
				case *ast.GenDecl:
					for _, spec := range decl.Specs {
						ts, ok := spec.(*ast.TypeSpec)
						if !ok || ts.Name.Name != optionType {
							continue
						}

						st, ok := ts.Type.(*ast.StructType)
						if !ok {
							t.Fatalf("%s is not a struct, so a caller can build one of their own", optionType)
						}

						for _, field := range st.Fields.List {
							for _, name := range field.Names {
								if name.IsExported() {
									t.Errorf("%s carries the exported field %s, which a caller can set to anything", optionType, name.Name)
								}
							}
						}
					}
				case *ast.FuncDecl:
					if decl.Recv != nil || decl.Type.Results == nil || len(decl.Type.Results.List) != 1 {
						continue
					}

					if ident, ok := decl.Type.Results.List[0].Type.(*ast.Ident); !ok || ident.Name != optionType {
						continue
					}

					var fields []string

					ast.Inspect(decl.Body, func(n ast.Node) bool {
						assign, ok := n.(*ast.AssignStmt)
						if !ok {
							return true
						}

						for _, lhs := range assign.Lhs {
							switch lhs := lhs.(type) {
							case *ast.SelectorExpr:
								fields = append(fields, lhs.Sel.Name)
							default:
								fields = append(fields, "the whole encoding")
							}
						}

						return true
					})

					if len(fields) != 1 {
						t.Errorf("%s sets %v, and an option sets one axis", decl.Name.Name, fields)

						continue
					}

					got[decl.Name.Name] = fields[0]
				}
			}

			if !maps.Equal(got, want) {
				t.Errorf("the options set %v, want %v", got, want)
			}

			for fn, field := range got {
				if field == "Binary" {
					t.Errorf("%s sets the binary width staircase", fn)
				}
			}
		})
	}
}

// TestTheConstructorsTakeOptionsAndNoEncoding holds both constructors to the
// signature #381 settled, over every golden: the stream, then a variadic list
// of options — so that the no-option call is the layout's encoding and no
// codec.Encoding, which would carry a staircase, can be handed to either.
func TestTheConstructorsTakeOptionsAndNoEncoding(t *testing.T) {
	t.Parallel()

	for dir, descriptor := range optionGoldens() {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()

			file, _ := generatedFileMachine(t, dir, descriptor())

			seen := map[string]bool{}

			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || (fn.Name.Name != newReaderFunc && fn.Name.Name != newWriterFunc) {
					continue
				}

				seen[fn.Name.Name] = true

				params := fn.Type.Params.List
				if len(params) != 2 {
					t.Fatalf("%s takes %d parameters, want the stream and the options", fn.Name.Name, len(params))
				}

				ellipsis, ok := params[1].Type.(*ast.Ellipsis)
				if !ok {
					t.Fatalf("%s's second parameter is not variadic", fn.Name.Name)
				}

				if ident, ok := ellipsis.Elt.(*ast.Ident); !ok || ident.Name != optionType {
					t.Errorf("%s's options are not ...%s", fn.Name.Name, optionType)
				}
			}

			for _, name := range []string{newReaderFunc, newWriterFunc} {
				if !seen[name] {
					t.Errorf("the generated %s declares no %s", fileMachineFile, name)
				}
			}
		})
	}
}

// TestTheConstructorsDocumentTheNoOptionCallAndAnOverride holds both doc
// comments to showing the two calls an adopter makes: the file as the layout
// describes it, and a converted copy of it.
func TestTheConstructorsDocumentTheNoOptionCallAndAnOverride(t *testing.T) {
	t.Parallel()

	for dir, descriptor := range optionGoldens() {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()

			file, _ := generatedFileMachine(t, dir, descriptor())

			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || (fn.Name.Name != newReaderFunc && fn.Name.Name != newWriterFunc) {
					continue
				}

				doc := fn.Doc.Text()

				bare := map[string]string{newReaderFunc: newReaderFunc + "(f)", newWriterFunc: newWriterFunc + "(out)"}[fn.Name.Name]
				if !strings.Contains(doc, bare) {
					t.Errorf("%s's doc comment never shows the no-option call %s\n%s", fn.Name.Name, bare, doc)
				}

				if !strings.Contains(doc, withCharsetFunc+"(") {
					t.Errorf("%s's doc comment never shows an override\n%s", fn.Name.Name, doc)
				}
			}
		})
	}
}

// TestEncodingIsDocumentedAsTheLayoutsEncodingAndTheDefault holds the rewritten
// doc comment on Encoding to the three things #381 made it: the layout's
// encoding, the default the constructors build under, and what a caller builds
// a codec.Reader from for the record methods — and to no longer carrying the
// argument that none of the five has a default.
func TestEncodingIsDocumentedAsTheLayoutsEncodingAndTheDefault(t *testing.T) {
	t.Parallel()

	out := t.TempDir()

	if err := generate(io.Discard, ordersDescriptor(), out, options{packageName: goldenPackage, importPath: goldenImport}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	source := written(t, out)[codecFile]

	start := strings.Index(source, "// "+encodingFunc+" is ")
	end := strings.Index(source, "func "+encodingFunc+"()")

	if start < 0 || end < start {
		t.Fatalf("%s carries no doc comment on %s\n%s", codecFile, encodingFunc, source)
	}

	doc := source[start:end]

	for _, want := range []string{
		"the layout this package was generated from",
		"[" + newReaderFunc + "] and [" + newWriterFunc + "] build under when handed no [" + optionType + "]",
		"codec.NewReader(f, " + encodingFunc + "())",
		"not a guess about",
	} {
		if !strings.Contains(strings.Join(strings.Fields(strings.ReplaceAll(doc, "//", "")), " "), want) {
			t.Errorf("the doc comment on %s never says %q\n%s", encodingFunc, want, doc)
		}
	}

	if strings.Contains(doc, "None of the five has a default") {
		t.Errorf("the doc comment on %s still argues that none of the five has a default\n%s", encodingFunc, doc)
	}
}

// TestAPackageWhoseLayoutStatesNoCharsetDefaultsTheAxesItDoesState is the one
// shape with no [encodingFunc] to default to: a file whose every item carries
// charset none. The layout still states the other four axes on every item, so
// those are the default, and the charset — which the layout left to nothing — is
// left unset for codec to report missing until the caller names one. Both
// constructors take the same options as everywhere else.
func TestAPackageWhoseLayoutStatesNoCharsetDefaultsTheAxesItDoesState(t *testing.T) {
	t.Parallel()

	d := chunksDescriptor()
	for _, node := range d.GetNodes() {
		if field := node.GetField(); field != nil {
			noCharset(node)
		}
	}

	file, source := generatedFileMachine(t, "internal/chunks", d)

	if strings.Contains(source, encodingFunc+"()") {
		t.Errorf("the generated %s calls %s, which a package whose layout states no charset does not declare\n%s", fileMachineFile, encodingFunc, source)
	}

	var body string

	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == encodingWithFunc {
			body = source[fn.Body.Pos()-file.FileStart : fn.Body.End()-file.FileStart]
		}
	}

	if body == "" {
		t.Fatalf("the generated %s declares no %s\n%s", fileMachineFile, encodingWithFunc, source)
	}

	for _, want := range []string{"Sign:", "ByteOrder:", "Float:", "Binary:"} {
		if !strings.Contains(body, want) {
			t.Errorf("%s does not default %s\n%s", encodingWithFunc, strings.TrimSuffix(want, ":"), body)
		}
	}

	if strings.Contains(body, "Charset:") {
		t.Errorf("%s defaults a charset the layout never stated\n%s", encodingWithFunc, body)
	}

	if !slices.ContainsFunc(strings.Split(source, "\n"), func(line string) bool {
		return strings.Contains(line, newReaderFunc+"(f, "+withCharsetFunc+"(")
	}) {
		t.Errorf("%s's doc comment never shows naming the charset\n%s", newReaderFunc, source)
	}
}
