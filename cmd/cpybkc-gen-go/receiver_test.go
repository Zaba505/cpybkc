// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/irpb"
)

// receiverScratch is where [TestTheLettersAShopPicksPassTheirGeneratedTests]
// writes the packages it hands the go tool, relative to this package.
//
// Inside the module, because a generated package imports cobol-go's codec and
// this module already requires it, so a run needs no network and no module of
// its own. The leading underscore keeps it out of ./..., and so out of `go
// build`, `go vet` and the linter, while an explicit path still builds it —
// internal/conformance/goadapter's scratch tree is the same arrangement, for
// the same reason.
const receiverScratch = "_receivers"

// TestEveryNameInScopeInsideAMethodIsRefusedOrCompiles is the rule
// [isReceiverName] and [clearReceiver] keep between them: every receiver the
// option accepts generates a package that compiles, for every layout, and every
// one it refuses is refused for a reason that holds for every layout.
//
// The names tried are not a list kept here. They are read out of each golden
// layout's generated package, by type-checking it and collecting every name
// in scope anywhere inside a generated method — its parameter, every local of
// every block, every import of the file, every declaration of the package, and
// every predeclared identifier. So a local, an import or a helper this generator
// starts emitting is a receiver tried here without anyone remembering to add
// it.
func TestEveryNameInScopeInsideAMethodIsRefusedOrCompiles(t *testing.T) {
	t.Parallel()

	// One importer for every check, because it type-checks what it imports from
	// source and keeps what it checked: codec and the standard library are read
	// once rather than once per receiver. It is not safe for concurrent use,
	// which is why nothing below runs in parallel.
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "source", nil)

	layouts := receiverLayouts()

	for _, dir := range slices.Sorted(maps.Keys(layouts)) {
		opts := options{packageName: path.Base(dir), importPath: goldenModule + dir}

		checked := typeCheck(t, fset, imp, sourcesOf(t, layouts[dir](), opts), opts)
		if len(checked.errs) > 0 {
			t.Fatalf("%s under the default receiver does not compile: %v", dir, checked.errs)
		}

		names := checked.inScope()
		if len(names) == 0 {
			t.Fatalf("%s declares no method with the receiver %s, so nothing was tried", dir, defaultReceiver)
		}

		for _, name := range names {
			if name == defaultReceiver {
				continue
			}

			if !isReceiverName(name) {
				if !token.IsExported(name) && !isPredeclared(name) && name != "_" {
					t.Errorf("%s: %q is refused as a receiver, and it is neither exported, predeclared nor the blank", dir, name)
				}

				continue
			}

			given := opts
			given.receiver = name

			got := typeCheck(t, fset, imp, sourcesOf(t, layouts[dir](), given), given)

			if len(got.errs) > 0 {
				t.Errorf("%s with %s=%s does not compile: %v", dir, receiverOption, name, got.errs)

				continue
			}

			for _, m := range got.methods {
				if spelled := m.decl.Recv.List[0].Names[0].Name; spelled != name {
					t.Errorf("%s with %s=%s declares %s with the receiver %s", dir, receiverOption, name, m.decl.Name.Name, spelled)
				}
			}
		}
	}
}

// TestTheLettersAShopPicksPassTheirGeneratedTests runs the generated tests of a
// package whose methods reach every kind of name a receiver could shadow, under
// the four receivers that shadowed one before the generator moved its own names
// out of the way.
//
// `r` and `w` are the methods' own parameters, `codec` is the import every
// method body calls through, and `err` is the local every one of them declares.
// The other test above holds that each compiles; this one holds that the rename
// changed no behaviour, by running the cases the generator wrote for that
// package.
func TestTheLettersAShopPicksPassTheirGeneratedTests(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("the go tool is not on PATH")
	}

	receivers := []string{"r", "w", "codec", "err"}

	fset := token.NewFileSet()

	base := options{packageName: goldenPackage, importPath: goldenImport}

	checked := typeCheck(t, fset, importer.ForCompiler(fset, "source", nil), sourcesOf(t, ordersDescriptor(), base), base)

	// The layout earns its place by reaching every kind of name, which is
	// asserted rather than assumed: a layout that stopped reaching one would
	// make this test pass over the case it exists for.
	reached := checked.reached()

	for _, kind := range []string{"parameter", "import", "local", "package", "predeclared"} {
		if len(reached[kind]) == 0 {
			t.Errorf("no method of %s reaches a %s name", goldenDir, kind)
		}
	}

	for _, name := range receivers {
		if !slices.ContainsFunc(slices.Collect(maps.Values(reached)), func(names []string) bool {
			return slices.Contains(names, name)
		}) {
			t.Errorf("no method of %s reaches anything spelled %s, so %s=%s collides with nothing", goldenDir, name, receiverOption, name)
		}
	}

	parent, err := filepath.Abs(receiverScratch)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}

	scratch, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = os.RemoveAll(scratch)
		_ = os.Remove(parent)
	})

	var packages []string

	for _, name := range receivers {
		out := filepath.Join(scratch, name)
		if err := os.Mkdir(out, 0o755); err != nil {
			t.Fatal(err)
		}

		rel := receiverScratch + "/" + filepath.Base(scratch) + "/" + name

		given := options{packageName: goldenPackage, importPath: goldenModule + rel, receiver: name}
		if err := generate(io.Discard, ordersDescriptor(), out, given); err != nil {
			t.Fatalf("generate with %s=%s: %v", receiverOption, name, err)
		}

		packages = append(packages, "./"+rel)
	}

	var said bytes.Buffer

	cmd := exec.Command("go", append([]string{"test", "-count=1"}, packages...)...)
	cmd.Stdout = &said
	cmd.Stderr = &said

	if err := cmd.Run(); err != nil {
		t.Fatalf("the generated tests failed: %v\n%s", err, said.String())
	}
}

// TestTheRenameIsWhatAReaderOfTheMethodSees pins what moving a name out of the
// receiver's way looks like in the source an adopter reads.
//
// The receiver keeps the spelling the manifest gave it and the generator's own
// name takes the next free number. A parameter is named in its method's prose,
// so the prose follows it; an import has no declaration to rename, so it gains
// an alias in the file whose methods it would have been shadowed in, and in no
// other.
func TestTheRenameIsWhatAReaderOfTheMethodSees(t *testing.T) {
	t.Parallel()

	reader := sourcesOf(t, ordersDescriptor(), options{packageName: goldenPackage, importPath: goldenImport, receiver: "r"})[codecFile]

	for _, want := range []string{
		"func (r *OrderRecord) UnmarshalCOBOL(r0 *codec.Reader) error {",
		"// UnmarshalCOBOL reads one ORDER-RECORD out of r0, in the order",
		"if err = refuseStaircase(r0.Encoding().Binary); err != nil {",
		"if r.OrderID, err = r0.ReadZonedInt32(5, codec.SignUnsigned); err != nil {",
		"func (r *OrderRecord) MarshalCOBOL(w *codec.Writer) error {",
	} {
		if !strings.Contains(reader, want) {
			t.Errorf("with %s=r, %s does not contain %q", receiverOption, codecFile, want)
		}
	}

	if strings.Contains(reader, "out of r,") {
		t.Errorf("with %s=r, %s still says the method reads out of r, which is now the receiver", receiverOption, codecFile)
	}

	sources := sourcesOf(t, ordersDescriptor(), options{packageName: goldenPackage, importPath: goldenImport, receiver: "codec"})

	if want := `codec0 "github.com/Zaba505/cobol-go/codec"`; !strings.Contains(sources[codecFile], want) {
		t.Errorf("with %s=codec, %s does not import codec as codec0", receiverOption, codecFile)
	}

	if want := "func (codec *OrderRecord) UnmarshalCOBOL(r *codec0.Reader) error {"; !strings.Contains(sources[codecFile], want) {
		t.Errorf("with %s=codec, %s does not contain %q", receiverOption, codecFile, want)
	}

	if strings.Contains(sources[fileMachineFile], "codec0") {
		t.Errorf("with %s=codec, %s aliases an import its methods were never shadowed in", receiverOption, fileMachineFile)
	}
}

// receiverLayouts is every golden layout, by the directory its package is
// checked in under.
func receiverLayouts() map[string]func() *irpb.Descriptor {
	all := map[string]func() *irpb.Descriptor{goldenDir: ordersDescriptor}
	maps.Copy(all, machineGoldens)

	return all
}

// sourcesOf is every file generate writes for d under opts, by name.
func sourcesOf(t *testing.T, d *irpb.Descriptor, opts options) map[string]string {
	t.Helper()

	out := t.TempDir()

	if err := generate(io.Discard, d, out, opts); err != nil {
		t.Fatalf("generate with %s=%q: %v", receiverOption, opts.receiver, err)
	}

	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}

	sources := make(map[string]string, len(entries))

	for _, entry := range entries {
		sources[entry.Name()] = contents(t, filepath.Join(out, entry.Name()))
	}

	return sources
}

// checkedPackage is one generated package after the type checker has read it.
type checkedPackage struct {
	info    *types.Info
	methods []receiverMethod

	// errs is every error the package or its generated tests carry.
	errs []error
}

// typeCheck type-checks the package sources make up, and then its generated
// tests against it, the way the compiler would: with every import resolved.
func typeCheck(t *testing.T, fset *token.FileSet, imp types.Importer, sources map[string]string, opts options) checkedPackage {
	t.Helper()

	parse := func(test bool) []*ast.File {
		var files []*ast.File

		for _, name := range slices.Sorted(maps.Keys(sources)) {
			if strings.HasSuffix(name, "_test.go") != test {
				continue
			}

			file, err := parser.ParseFile(fset, name, sources[name], parser.ParseComments)
			if err != nil {
				t.Fatalf("parsing the generated %s: %v", name, err)
			}

			files = append(files, file)
		}

		return files
	}

	c := checkedPackage{info: &types.Info{
		Defs:   make(map[*ast.Ident]types.Object),
		Uses:   make(map[*ast.Ident]types.Object),
		Scopes: make(map[ast.Node]*types.Scope),
	}}

	files := parse(false)

	conf := types.Config{Importer: imp, Error: func(err error) { c.errs = append(c.errs, err) }}

	pkg, _ := conf.Check(opts.packageName, fset, files, c.info)

	if tests := parse(true); len(tests) > 0 {
		conf.Importer = beside{Importer: imp, path: opts.importPath, pkg: pkg}
		_, _ = conf.Check(opts.packageName+"_test", fset, tests, nil)
	}

	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List[0].Names) == 0 {
				continue
			}

			if fn.Name.Name == "UnmarshalCOBOL" || fn.Name.Name == "MarshalCOBOL" {
				c.methods = append(c.methods, receiverMethod{decl: fn, file: file})
			}
		}
	}

	return c
}

// inScope is every name in scope anywhere inside the package's generated
// methods, sorted: everything their own scopes and the blocks inside them
// declare, and everything the file, the package and the universe declare
// around them.
func (c checkedPackage) inScope() []string {
	names := make(map[string]bool)

	var inner func(*types.Scope)
	inner = func(s *types.Scope) {
		for _, name := range s.Names() {
			names[name] = true
		}

		for child := range s.Children() {
			inner(child)
		}
	}

	for _, m := range c.methods {
		scope := c.info.Scopes[m.decl.Type]
		inner(scope)

		for outer := scope.Parent(); outer != nil; outer = outer.Parent() {
			for _, name := range outer.Names() {
				names[name] = true
			}
		}
	}

	return slices.Sorted(maps.Keys(names))
}

// reached is every name an identifier inside a generated method refers to, by
// the kind of thing it names: the method's parameter, an import, a local, a
// declaration of the package, or a predeclared identifier.
func (c checkedPackage) reached() map[string][]string {
	kinds := make(map[string][]string)

	for _, m := range c.methods {
		scope := c.info.Scopes[m.decl.Type]
		param := c.info.Defs[m.decl.Type.Params.List[0].Names[0]]

		ast.Inspect(m.decl.Body, func(n ast.Node) bool {
			ident, ok := n.(*ast.Ident)
			if !ok {
				return true
			}

			obj := c.info.Uses[ident]
			if obj == nil || selected(obj) {
				return true
			}

			var kind string

			switch {
			case obj == param:
				kind = "parameter"
			case obj.Parent() == types.Universe:
				kind = "predeclared"
			case obj.Pkg() != nil && obj.Parent() == obj.Pkg().Scope():
				kind = "package"
			default:
				if _, ok := obj.(*types.PkgName); ok {
					kind = "import"
				} else if scope.Contains(obj.Pos()) {
					kind = "local"
				} else {
					return true
				}
			}

			if !slices.Contains(kinds[kind], obj.Name()) {
				kinds[kind] = append(kinds[kind], obj.Name())
			}

			return true
		})
	}

	return kinds
}

// beside resolves the generated package's own import path to the package
// already checked, which is how its external tests reach it, and everything
// else as the importer it wraps does.
type beside struct {
	types.Importer

	path string
	pkg  *types.Package
}

// Import implements [types.Importer].
func (b beside) Import(importPath string) (*types.Package, error) {
	if importPath == b.path {
		return b.pkg, nil
	}

	return b.Importer.Import(importPath)
}
