// Package surface reads a Go package's command-line surface out of its source:
// the flags it declares as constants, and the exported methods it declares on a
// type.
//
// It is a package of its own for the reason daggerverse/cpybkc's internal
// packages are: the pipeline's own package main imports the generated Dagger
// client, whose init panics without a session, so a test beside it cannot run
// under plain `go test`. This package imports no Dagger and takes file contents
// rather than a *dagger.Directory, so every rule below is pinned by a test
// rather than asserted by a comment — which matters more here than anywhere
// else in the pipeline, because what is built on it is a drift guard, and a
// drift guard with a hole fails by staying green.
package surface

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"regexp"
	"slices"
	"strconv"
)

// flagToken is what a flag looks like when it is written on a command line: one
// or two hyphens, then a letter, then letters, digits and hyphens.
//
// It is a shape test rather than a prefix test, and it does three jobs at once.
// It admits a single-hyphen flag, so a short flag that is not a synonym of a
// long one cannot slip past a guard built on this. It rejects "-" and "--",
// which are POSIX's standard-stream operand and its end-of-options delimiter
// rather than flags. And it rejects anything carrying a space, which is what
// keeps a diagnostic or a wrapped usage line hoisted into a constant —
// "--emit-ir-format may only be given beside --emit-ir" — from being read as a
// flag nobody has covered, a red build whose suggested remedy would make things
// worse.
var flagToken = regexp.MustCompile(`^--?[A-Za-z][A-Za-z0-9-]*$`)

// Flags is every flag the given Go sources declare as a string constant, sorted,
// with the names of the constants whose value could not be evaluated.
//
// The keys of files are paths used in parse errors; the values are the sources.
//
// Constants only, and not every string literal: a diagnostic that quotes a flag
// back at the user is prose about the surface rather than part of it, and
// reading those too would make a guard built on this fail on a reworded error
// message. Within that, every constant counts — one declared inside a function
// body as much as one at package scope, because which of the two a contributor
// reaches for is a style choice and not a statement about whether the flag is
// real.
//
// The second return is what stops "I could not read this" being reported as
// "this is not a flag". A constant whose value this package cannot evaluate but
// which has a string literal somewhere inside it is named rather than dropped,
// so a caller can refuse to draw a conclusion from a reading it knows is
// incomplete. A value with no string literal in it is neither: it is arithmetic,
// or an untyped number, and it was never a candidate.
func Flags(files map[string]string) ([]string, []string, error) {
	constants, err := stringConstants(files)
	if err != nil {
		return nil, nil, err
	}

	values, unresolved := resolve(constants)

	flags := map[string]bool{}
	for _, value := range values {
		if flagToken.MatchString(value) {
			flags[value] = true
		}
	}

	return slices.Sorted(maps.Keys(flags)), unresolved, nil
}

// Functions is every exported method the given Go sources declare on typeName,
// sorted — which for a Dagger module's own type is exactly the set of functions
// the module publishes.
//
// A pointer receiver and a value receiver both count: the pointer is the
// convention, and a value receiver publishes the same function.
func Functions(files map[string]string, typeName string) ([]string, error) {
	names := map[string]bool{}

	for _, path := range slices.Sorted(maps.Keys(files)) {
		parsed, err := parser.ParseFile(token.NewFileSet(), path, files[path], 0)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}

		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || !fn.Name.IsExported() {
				continue
			}

			if receiverType(fn.Recv.List[0].Type) == typeName {
				names[fn.Name.Name] = true
			}
		}
	}

	return slices.Sorted(maps.Keys(names)), nil
}

// constant is one declared constant, kept unevaluated because a constant may be
// written in terms of another declared later or in another file.
type constant struct {
	name string
	expr ast.Expr
}

// stringConstants is every constant in the given sources whose value could be a
// string — that is, every one with a string literal somewhere inside its value
// expression.
//
// ast.Inspect rather than a walk of file.Decls, because a constant declared
// inside a function body is a *ast.DeclStmt in that body and a walk of the
// file's top-level declarations never reaches it.
func stringConstants(files map[string]string) ([]constant, error) {
	var constants []constant

	for _, path := range slices.Sorted(maps.Keys(files)) {
		parsed, err := parser.ParseFile(token.NewFileSet(), path, files[path], 0)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}

		ast.Inspect(parsed, func(node ast.Node) bool {
			decl, ok := node.(*ast.GenDecl)
			if !ok || decl.Tok != token.CONST {
				return true
			}

			for _, spec := range decl.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok || len(value.Values) != len(value.Names) {
					// A spec with no values of its own is an iota repetition,
					// which carries the previous spec's expression and never a
					// string of its own.
					continue
				}

				for at, expr := range value.Values {
					if holdsAStringLiteral(expr) {
						constants = append(constants, constant{name: value.Names[at].Name, expr: expr})
					}
				}
			}

			return true
		})
	}

	return constants, nil
}

// resolve evaluates what it can, to a fixed point, and names what it cannot.
//
// A fixed point rather than one pass because a constant may be written in terms
// of one declared further down the file or in another file of the package, and
// declaration order is not evaluation order in Go. Each round resolves at least
// one constant or the loop is over, so the number of rounds is bounded by the
// number of constants.
func resolve(constants []constant) (map[string]string, []string) {
	values := map[string]string{}

	for {
		progressed := false

		for _, c := range constants {
			if _, done := values[c.name]; done {
				continue
			}

			if value, ok := evaluate(c.expr, values); ok {
				values[c.name] = value
				progressed = true
			}
		}

		if !progressed {
			break
		}
	}

	var unresolved []string
	for _, c := range constants {
		if _, done := values[c.name]; !done {
			unresolved = append(unresolved, c.name)
		}
	}

	slices.Sort(unresolved)

	return values, slices.Compact(unresolved)
}

// evaluate reads a constant expression as a string.
//
// Literals, concatenation and references to constants already resolved, which is
// the whole of what a flag spelling is ever written as: a literal, or one flag's
// spelling built from another's — `emitIRFlag + "-format"`. Anything else — a
// conversion, a call, a reference to something outside the package — is not
// guessed at; it comes back false and its constant is named to the caller.
func evaluate(expr ast.Expr, values map[string]string) (string, bool) {
	switch expr := expr.(type) {
	case *ast.BasicLit:
		if expr.Kind != token.STRING {
			return "", false
		}

		unquoted, err := strconv.Unquote(expr.Value)
		if err != nil {
			return "", false
		}

		return unquoted, true

	case *ast.Ident:
		value, ok := values[expr.Name]

		return value, ok

	case *ast.ParenExpr:
		return evaluate(expr.X, values)

	case *ast.BinaryExpr:
		if expr.Op != token.ADD {
			return "", false
		}

		left, ok := evaluate(expr.X, values)
		if !ok {
			return "", false
		}

		right, ok := evaluate(expr.Y, values)
		if !ok {
			return "", false
		}

		return left + right, true

	default:
		return "", false
	}
}

// holdsAStringLiteral reports whether an expression has a string literal
// anywhere inside it, which is what makes a constant a candidate for being a
// flag at all. It is what keeps integer arithmetic out of the unresolved list.
func holdsAStringLiteral(expr ast.Expr) bool {
	found := false

	ast.Inspect(expr, func(node ast.Node) bool {
		if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING {
			found = true
		}

		return !found
	})

	return found
}

// receiverType names the type a method is declared on, with the pointer taken
// off.
func receiverType(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}

	ident, ok := expr.(*ast.Ident)
	if !ok {
		return ""
	}

	return ident.Name
}
