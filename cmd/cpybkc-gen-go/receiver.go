// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// receiverPlaceholder is the identifier every method's receiver is composed
// under, before [clearReceiver] gives it the spelling the manifest chose.
//
// The receiver is not the only name in scope inside a generated method. The
// generator declares its own there — the method's codec.Reader or codec.Writer
// parameter, the packages the file imports, the locals the body declares, the
// package-level helpers it calls — and which of them a method reaches depends on
// the layout. So the receiver is composed under a spelling none of those can
// have, and only once the whole package exists, and it can be asked what each
// method actually reaches, is it renamed.
//
// Two underscores is what keeps it apart: every identifier munged from a
// copybook name is exported, and nothing this generator names for itself
// carries one. [clearReceiver] checks that rather than trusting it, because a
// placeholder that is also a name the method uses would be renamed along with
// it and silently change what the method refers to.
const receiverPlaceholder = "receiver__"

// clearReceiver renames the receiver of every generated method from
// [receiverPlaceholder] to receiver, after first renaming every name a method
// reaches that receiver would otherwise shadow.
//
// sources are the generated package's files by name. Only the package's own
// files are read and rewritten: a `_test.go` file here is the external test
// package, which reaches nothing unexported and declares no method with the
// receiver.
//
// # Why the generator's names move rather than the receiver being refused
//
// Go's convention for a receiver is one letter, and the letters a shop picks
// for a record decoder are exactly the ones this generator already spends: `r`
// is the decode method's codec.Reader and `w` the encode method's codec.Writer.
// Refusing them would refuse the receiver the option most often exists to set.
// The generator's own names are its to choose, and choosing them clear of
// whatever receiver it was given means every unexported identifier works on
// every layout. The one kind it cannot move is a predeclared identifier — `len`
// cannot be spelled any other way — and [isReceiverName] refuses every one of
// those for every layout, so that no receiver works for one layout and fails for
// another.
//
// # How a collision is found
//
// By resolving names rather than by matching text, so that a field read
// through a selector — `r.Encoding`, or `x.slack` — is never mistaken for the
// thing it shares a spelling with. The package is type-checked with every
// import stubbed out: the generator cannot rely on the source of the packages
// it imports being on the machine it runs on, and resolving which declaration
// each identifier refers to needs only the generated files themselves. The type
// errors the stubs cause are discarded; what is kept is which object every
// identifier names.
//
// Every object named receiver that an identifier inside a receiver method
// refers to — a parameter, a local, an import, a package-level helper — is
// renamed, at its declaration and at every use, to one fresh spelling no
// identifier in the package already carries. Renaming every such object to the
// same spelling is safe because they already coexisted under one spelling, so
// none of them shadowed another that the rename could expose. An import has no
// declaration to rename, so it gains an alias.
//
// The method's own prose names its parameter — "reads one record out of r" —
// so where the parameter moves, the comments inside that method move with it.
// Nothing else in a method's prose names a generated identifier, and a word in
// a comment is not renamed on the strength of merely being spelled like one.
func clearReceiver(sources map[string]string, pkg, receiver string) error {
	var (
		names    []string
		composed bool
	)

	for name, source := range sources {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		names = append(names, name)
		composed = composed || strings.Contains(source, receiverPlaceholder)
	}

	// A package with no record declares no method, and so has no receiver to
	// give a spelling to.
	if !composed || receiver == receiverPlaceholder {
		return nil
	}

	slices.Sort(names)

	fset := token.NewFileSet()

	files := make([]*ast.File, 0, len(names))

	for _, name := range names {
		file, err := parser.ParseFile(fset, name, sources[name], parser.ParseComments)
		if err != nil {
			return fmt.Errorf("failed to read back the generated %s: %w", name, err)
		}

		files = append(files, file)
	}

	info := &types.Info{
		Defs:      make(map[*ast.Ident]types.Object),
		Uses:      make(map[*ast.Ident]types.Object),
		Implicits: make(map[ast.Node]types.Object),
	}

	conf := types.Config{Importer: stubImporter{}, Error: func(error) {}}

	// The error is every type error the stubs cause, and it is not what this
	// asks: Defs and Uses are filled in for every identifier whatever the
	// checker made of the expression around it.
	_, _ = conf.Check(pkg, fset, files, info)

	methods, receivers := receiverMethods(files, info)

	referred := func(yield func(*ast.Ident, types.Object) bool) {
		for _, m := range []map[*ast.Ident]types.Object{info.Defs, info.Uses} {
			for ident, obj := range m {
				if obj != nil && !yield(ident, obj) {
					return
				}
			}
		}
	}

	collides := make(map[types.Object]bool)

	for ident, obj := range referred {
		if obj.Name() == receiverPlaceholder && !receivers[obj] {
			return fmt.Errorf("the generated %s names something other than a receiver %s, which is the spelling a receiver is composed under",
				fset.Position(ident.Pos()).Filename, receiverPlaceholder)
		}

		if obj.Name() != receiver || !within(methods, ident.Pos()) || selected(obj) {
			continue
		}

		if obj.Parent() == types.Universe {
			return fmt.Errorf("%s=%q is a predeclared identifier, and the generated %s uses it", receiverOption, receiver,
				fset.Position(ident.Pos()).Filename)
		}

		collides[obj] = true
	}

	edits := make(map[string][]edit)

	at := func(pos token.Pos, width int, text string) {
		p := fset.Position(pos)
		edits[p.Filename] = append(edits[p.Filename], edit{offset: p.Offset, width: width, text: text})
	}

	if len(collides) > 0 {
		fresh := freshName(receiver, files)

		for ident, obj := range referred {
			if collides[obj] {
				at(ident.Pos(), len(ident.Name), fresh)
			}
		}

		for node, obj := range info.Implicits {
			if spec, ok := node.(*ast.ImportSpec); ok && collides[obj] {
				at(spec.Path.Pos(), 0, fresh+" ")
			}
		}

		for _, m := range methods {
			if !collides[parameterOf(m, info)] {
				continue
			}

			for _, group := range m.file.Comments {
				if group.Pos() < m.start() || group.End() > m.decl.End() {
					continue
				}

				for _, c := range group.List {
					for _, offset := range wordsIn(c.Text, receiver) {
						at(c.Pos()+token.Pos(offset), len(receiver), fresh)
					}
				}
			}
		}
	}

	for name, source := range sources {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		sources[name] = strings.ReplaceAll(apply(source, edits[name]), receiverPlaceholder, receiver)
	}

	return nil
}

// receiverMethod is one method declared with the placeholder receiver, and the
// file it is in.
type receiverMethod struct {
	decl *ast.FuncDecl
	file *ast.File
}

// start is where the method's text begins, which is its doc comment where it
// carries one.
func (m receiverMethod) start() token.Pos {
	if m.decl.Doc != nil {
		return m.decl.Doc.Pos()
	}

	return m.decl.Pos()
}

// receiverMethods is every method declared with the placeholder receiver, and
// the objects those receivers declare.
func receiverMethods(files []*ast.File, info *types.Info) ([]receiverMethod, map[types.Object]bool) {
	var methods []receiverMethod

	receivers := make(map[types.Object]bool)

	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil {
				continue
			}

			for _, field := range fn.Recv.List {
				for _, name := range field.Names {
					if name.Name != receiverPlaceholder {
						continue
					}

					methods = append(methods, receiverMethod{decl: fn, file: file})
					receivers[info.Defs[name]] = true
				}
			}
		}
	}

	return methods, receivers
}

// parameterOf is the object the method's one parameter declares — its
// codec.Reader or its codec.Writer — or nil where it declares none.
func parameterOf(m receiverMethod, info *types.Info) types.Object {
	params := m.decl.Type.Params
	if params == nil || len(params.List) != 1 || len(params.List[0].Names) != 1 {
		return nil
	}

	return info.Defs[params.List[0].Names[0]]
}

// within reports whether pos is inside one of methods, signature or body.
func within(methods []receiverMethod, pos token.Pos) bool {
	return slices.ContainsFunc(methods, func(m receiverMethod) bool {
		return m.decl.Pos() <= pos && pos < m.decl.End()
	})
}

// selected reports whether obj is reached only through a selector — a struct
// field or a method — which no receiver can shadow.
func selected(obj types.Object) bool {
	switch obj := obj.(type) {
	case *types.Var:
		return obj.IsField()
	case *types.Func:
		sig, ok := obj.Type().(*types.Signature)

		return ok && sig.Recv() != nil
	}

	return false
}

// freshName is base followed by the lowest number that makes it a spelling no
// identifier in files carries and no predeclared identifier has.
//
// Every identifier, including the ones after a dot, so that nothing has to be
// argued about which of them could be in scope where the fresh name lands.
func freshName(base string, files []*ast.File) string {
	spent := make(map[string]bool)

	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok {
				spent[ident.Name] = true
			}

			return true
		})
	}

	for n := 0; ; n++ {
		name := base + strconv.Itoa(n)
		if !spent[name] && types.Universe.Lookup(name) == nil {
			return name
		}
	}
}

// wordsIn is the byte offset of every occurrence of word in text that stands as
// a word of its own: neither neighbour is a letter, a digit or an underscore.
func wordsIn(text, word string) []int {
	var offsets []int

	for from := 0; ; {
		i := strings.Index(text[from:], word)
		if i < 0 {
			return offsets
		}

		start, end := from+i, from+i+len(word)

		before, _ := utf8.DecodeLastRuneInString(text[:start])
		after, _ := utf8.DecodeRuneInString(text[end:])

		if !identRune(before) && !identRune(after) {
			offsets = append(offsets, start)
		}

		from = end
	}
}

// identRune reports whether r can continue an identifier.
func identRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// edit replaces width bytes at offset with text.
type edit struct {
	offset, width int
	text          string
}

// apply makes edits to source, which may arrive in any order and do not
// overlap.
func apply(source string, edits []edit) string {
	slices.SortFunc(edits, func(a, b edit) int { return cmp.Compare(a.offset, b.offset) })

	var b strings.Builder

	last := 0

	for _, e := range edits {
		b.WriteString(source[last:e.offset])
		b.WriteString(e.text)
		last = e.offset + e.width
	}

	b.WriteString(source[last:])

	return b.String()
}

// stubImporter answers every import with an empty package under the name its
// path ends in, which is the name every import the generator writes is
// referred to by.
//
// Empty because [clearReceiver] asks which declaration each identifier refers
// to, and an imported package's own declarations are only ever reached through
// a selector, which no receiver can shadow.
type stubImporter struct{}

// Import implements [types.Importer].
func (stubImporter) Import(importPath string) (*types.Package, error) {
	pkg := types.NewPackage(importPath, path.Base(importPath))
	pkg.MarkComplete()

	return pkg, nil
}
