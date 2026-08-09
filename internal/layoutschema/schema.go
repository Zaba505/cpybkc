// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutschema

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	sexpr "github.com/z5labs/sexpr-go"
)

// contextTopLevel is the context a form declared (in top-level) is reachable
// from: the layout itself, rather than a sort or a parent form.
const contextTopLevel = "top-level"

// SchemaFile is where the published schema lives, relative to the repository
// root, in the slash-separated spelling a repository path is written in.
//
// It is a constant here rather than a default in the tool that publishes it,
// because the tests, the tool and the Dagger function that attaches the artifact
// to a release all have to mean the same file, and a path repeated in three
// places is a path two of them get wrong.
const SchemaFile = "schema/layout.sexpr"

// SchemaPath is [SchemaFile] under root, in the host's own path spelling.
func SchemaPath(root string) string {
	return filepath.Join(root, filepath.FromSlash(SchemaFile))
}

// The primitive sorts. A position may name one of these without anything having
// declared it, which is what stops the schema needing a declaration for "a
// symbol".
const (
	sortSymbol         = "symbol"
	sortText           = "text"
	sortNumber         = "number"
	sortPositiveNumber = "positive-number"
)

// primitives is the set above, for the membership tests the loader makes.
var primitives = []string{sortSymbol, sortText, sortNumber, sortPositiveNumber}

// Schema is a loaded layout schema: the form declarations, the sorts their
// positions are written against, and the version the file states.
//
// It is immutable once [Load] has returned. [Schema.Check] reads it and writes
// nothing, so one loaded schema serves every layout a process validates.
type Schema struct {
	subject string
	version int64

	// sorts, by name. Populated by both (sort ...) and (reference ...), because
	// a position names either the same way and neither the loader nor a
	// declaration has a reason to tell them apart at the point of use.
	sorts map[string]*sortDecl

	// contextual holds every form declared (in ...), keyed by the context and
	// the tag. Two forms may share a tag in different contexts — `one-of` is a
	// discrimination strategy and a `when` match, with different arguments — so
	// the tag alone is not the key.
	contextual map[contextKey]*formDecl

	// children holds every form declared without (in ...), by tag. A child is
	// reachable only where a parent's (child ...) names it, and the arity is the
	// parent's, which is why `charset` is declared once and is required under
	// `encoding` and optional under `encoding-override`.
	children map[string]*formDecl

	// topLevel is the (in top-level) forms in declaration order, so that a
	// report over them reads in the order the schema states them rather than in
	// whichever order a map produced.
	topLevel []*formDecl
}

// contextKey identifies a form by where it may appear as well as by its tag.
type contextKey struct {
	context string
	tag     string
}

// formDecl is one (form ...) declaration.
type formDecl struct {
	tag   string
	pos   sexpr.Pos
	in    string // "" for a child form
	arity arity  // the arity within `in`; only top-level forms carry one
	layer string
	args  []argDecl
	// children, in declaration order, and childArity keyed by tag. The order is
	// what a "missing required child" report is emitted in.
	children   []string
	childArity map[string]arity
}

// argDecl is one (argument <name> <sort> <arity>) declaration.
type argDecl struct {
	name  string
	sort  sortRef
	arity arity
}

// sortRef is what an argument's position admits: either a sort by name — a
// primitive or one declared below — or a closed set written inline.
//
// Inline rather than named, for every closed set in the schema, because each is
// used in exactly one position. A set named and referred to by name would be a
// second place to look for what a `recfm` may say, and the indirection buys
// nothing while that stays true.
type sortRef struct {
	name   string
	values []string
}

// sortDecl is one (sort ...) or (reference ...) declaration.
type sortDecl struct {
	name string
	pos  sexpr.Pos

	// forms names the tags this sort admits as forms. It is kept in step with
	// the (in ...) on each of those forms by a check in the loader rather than
	// by hand: a sort listing a form that does not declare itself into it, or a
	// form declaring itself into a sort that does not list it, is a schema this
	// package refuses to load.
	forms []string

	// symbols names the bare symbols this sort admits — `single-record-type` is
	// the only one today.
	symbols []string

	// include names other sorts whose members this one also admits.
	include []string

	// refForm and refArg are set only by (reference ...): the sort admits a
	// symbol equal to that argument of some form with that tag in the same
	// layout.
	refForm string
	refArg  string
}

// isReference reports whether this sort was declared by (reference ...).
func (s *sortDecl) isReference() bool { return s.refForm != "" }

// arity is how many times something may appear at a position.
//
// The spellings are symbols rather than SPEC.md's 1, 0..1, 1..n and 0..n
// because `0..1` is not a lexeme the grammar admits — it begins like a number
// and is a malformed one — so a schema written in SPEC.md's notation would not
// parse.
type arity struct {
	spelling string
	min      int
	max      int // -1 for unbounded
}

// arities maps a spelling to what it means. It is the whole set; a spelling not
// in it is a schema error rather than a default.
var arities = map[string]arity{
	"exactly-one":  {spelling: "exactly-one", min: 1, max: 1},
	"at-most-one":  {spelling: "at-most-one", min: 0, max: 1},
	"one-or-more":  {spelling: "one-or-more", min: 1, max: -1},
	"zero-or-more": {spelling: "zero-or-more", min: 0, max: -1},
	"two-or-more":  {spelling: "two-or-more", min: 2, max: -1},
}

// label renders an arity the way a diagnostic says it: "at most one" rather
// than "at-most-one", so the message reads as a sentence.
func (a arity) label() string { return strings.ReplaceAll(a.spelling, "-", " ") }

// admits reports whether n occurrences satisfy this arity.
func (a arity) admits(n int) bool {
	if n < a.min {
		return false
	}

	return a.max < 0 || n <= a.max
}

// repeatable reports whether this arity admits more than one.
func (a arity) repeatable() bool { return a.max < 0 || a.max > 1 }

// Subject is what the schema says it describes — `layout` for this one. It is
// read rather than assumed so that a consumer handed the wrong file is told
// which file it has.
func (s *Schema) Subject() string { return s.subject }

// Version is the schema version the file states.
//
// One monotonic integer, and the policy for advancing it is
// docs/layout/SPEC.md's "The published schema". A consumer that does not know
// the version in front of it refuses the schema rather than proceeding on the
// declarations it recognises.
func (s *Schema) Version() int64 { return s.version }

// TopLevelForms returns the tags a layout may write at the top level, in the
// order the schema declares them.
func (s *Schema) TopLevelForms() []string {
	tags := make([]string, 0, len(s.topLevel))
	for _, form := range s.topLevel {
		tags = append(tags, form.tag)
	}

	return tags
}

// Layers returns the layers the top-level forms are declared into, sorted.
//
// SPEC.md claims five separable layers, and a form belongs to exactly one of
// them. This is what lets a test assert that the published schema covers every
// layer rather than most of them.
func (s *Schema) Layers() []string {
	var layers []string

	for _, form := range s.topLevel {
		if form.layer != "" && !slices.Contains(layers, form.layer) {
			layers = append(layers, form.layer)
		}
	}

	slices.Sort(layers)

	return layers
}

// Load reads a schema from r.
//
// It fails on anything it cannot make sense of rather than skipping it. The
// schema is the contract, and a declaration silently ignored is a form a
// generator targets and a validator never checks — the one failure mode this
// package exists to remove.
func Load(r io.Reader) (*Schema, error) {
	file, err := sexpr.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse the schema: %w", err)
	}

	schema := &Schema{
		sorts:      make(map[string]*sortDecl),
		contextual: make(map[contextKey]*formDecl),
		children:   make(map[string]*formDecl),
	}

	if err := schema.declare(file); err != nil {
		return nil, err
	}

	return schema, schema.link()
}

// declare reads every top-level declaration in the file into the schema,
// without resolving anything one declaration says about another. Resolution is
// link's, because a schema is not required to declare a sort before the form
// that names it.
func (s *Schema) declare(file *sexpr.File) error {
	seenHeader := false

	for _, node := range file.Nodes {
		list, ok := node.(sexpr.List)
		if !ok {
			return fmt.Errorf("%s: a schema is a list of declarations, and this is not one", at(posOf(node)))
		}

		tag, err := tagOf(list)
		if err != nil {
			return err
		}

		switch tag {
		case "schema":
			if seenHeader {
				return fmt.Errorf("%s: a second (schema ...) declaration", at(list.Pos))
			}

			seenHeader = true

			if err := s.declareHeader(list); err != nil {
				return err
			}
		case "sort":
			if err := s.declareSort(list); err != nil {
				return err
			}
		case "reference":
			if err := s.declareReference(list); err != nil {
				return err
			}
		case "form":
			if err := s.declareForm(list); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s: unknown declaration %q", at(list.Pos), tag)
		}
	}

	if !seenHeader {
		return fmt.Errorf("the schema states no (schema <name> <version>): nothing says which version this is")
	}

	return nil
}

// declareHeader reads (schema <name> <version>).
func (s *Schema) declareHeader(list sexpr.List) error {
	if len(list.Elements) != 3 {
		return fmt.Errorf("%s: (schema ...) takes a name and a version", at(list.Pos))
	}

	name, ok := list.Elements[1].(sexpr.Symbol)
	if !ok {
		return fmt.Errorf("%s: a schema's name is a symbol", at(posOf(list.Elements[1])))
	}

	version, ok := list.Elements[2].(sexpr.Int)
	if !ok {
		return fmt.Errorf("%s: a schema's version is an integer", at(posOf(list.Elements[2])))
	}

	if version.Value < 1 {
		return fmt.Errorf("%s: a schema's version starts at 1", at(version.Pos))
	}

	s.subject = name.Value
	s.version = version.Value

	return nil
}

// declareSort reads (sort <name> <member> ...).
func (s *Schema) declareSort(list sexpr.List) error {
	if len(list.Elements) < 2 {
		return fmt.Errorf("%s: (sort ...) takes a name", at(list.Pos))
	}

	decl, err := s.newSort(list, 1)
	if err != nil {
		return err
	}

	for _, member := range list.Elements[2:] {
		switch member := member.(type) {
		case sexpr.Symbol:
			decl.include = append(decl.include, member.Value)
		case sexpr.List:
			kind, err := tagOf(member)
			if err != nil {
				return err
			}

			name, err := symbolArg(member, 1, kind)
			if err != nil {
				return err
			}

			switch kind {
			case "form":
				decl.forms = append(decl.forms, name)
			case "symbol":
				decl.symbols = append(decl.symbols, name)
			default:
				return fmt.Errorf("%s: a sort's members are (form ...), (symbol ...) or the name of another sort, not %q", at(member.Pos), kind)
			}
		default:
			return fmt.Errorf("%s: a sort's members are (form ...), (symbol ...) or the name of another sort", at(posOf(member)))
		}
	}

	return nil
}

// declareReference reads (reference <sort> <form> <argument>).
func (s *Schema) declareReference(list sexpr.List) error {
	if len(list.Elements) != 4 {
		return fmt.Errorf("%s: (reference ...) takes a sort, a form and the argument of that form it names", at(list.Pos))
	}

	decl, err := s.newSort(list, 1)
	if err != nil {
		return err
	}

	if decl.refForm, err = symbolArg(list, 2, "reference"); err != nil {
		return err
	}

	decl.refArg, err = symbolArg(list, 3, "reference")

	return err
}

// newSort registers a sort declared by list, naming it from the element at
// index. Both (sort ...) and (reference ...) arrive here, so a name cannot be
// declared once as each.
func (s *Schema) newSort(list sexpr.List, index int) (*sortDecl, error) {
	name, err := symbolArg(list, index, "sort")
	if err != nil {
		return nil, err
	}

	if slices.Contains(primitives, name) {
		return nil, fmt.Errorf("%s: %q is a primitive sort and cannot be declared", at(list.Pos), name)
	}

	if _, exists := s.sorts[name]; exists {
		return nil, fmt.Errorf("%s: sort %q is declared twice", at(list.Pos), name)
	}

	decl := &sortDecl{name: name, pos: list.Pos}
	s.sorts[name] = decl

	return decl, nil
}

// declareForm reads (form <tag> ...).
func (s *Schema) declareForm(list sexpr.List) error {
	tag, err := symbolArg(list, 1, "form")
	if err != nil {
		return err
	}

	decl := &formDecl{tag: tag, pos: list.Pos, childArity: make(map[string]arity)}

	for _, clause := range list.Elements[2:] {
		if err := s.declareFormClause(decl, clause); err != nil {
			return err
		}
	}

	if err := checkArgumentOrder(decl); err != nil {
		return err
	}

	if decl.in == "" {
		if _, exists := s.children[tag]; exists {
			return fmt.Errorf("%s: child form %q is declared twice", at(list.Pos), tag)
		}

		s.children[tag] = decl

		return nil
	}

	key := contextKey{context: decl.in, tag: tag}
	if _, exists := s.contextual[key]; exists {
		return fmt.Errorf("%s: form %q is declared twice in %s", at(list.Pos), tag, decl.in)
	}

	s.contextual[key] = decl

	if decl.in == contextTopLevel {
		s.topLevel = append(s.topLevel, decl)
	}

	return nil
}

// declareFormClause reads one clause of a (form ...) declaration.
func (s *Schema) declareFormClause(decl *formDecl, node sexpr.Node) error {
	clause, ok := node.(sexpr.List)
	if !ok {
		return fmt.Errorf("%s: a form's clauses are lists", at(posOf(node)))
	}

	kind, err := tagOf(clause)
	if err != nil {
		return err
	}

	switch kind {
	case "in":
		decl.in, err = symbolArg(clause, 1, kind)
	case "layer":
		decl.layer, err = symbolArg(clause, 1, kind)
	case "arity":
		decl.arity, err = arityArg(clause, 1)
	case "argument":
		err = s.declareArgument(decl, clause)
	case "child":
		err = declareChild(decl, clause)
	default:
		err = fmt.Errorf("%s: unknown clause %q in form %q", at(clause.Pos), kind, decl.tag)
	}

	return err
}

// declareArgument reads (argument <name> <sort> <arity>).
func (s *Schema) declareArgument(decl *formDecl, clause sexpr.List) error {
	if len(clause.Elements) != 4 {
		return fmt.Errorf("%s: (argument ...) takes a name, a sort and an arity", at(clause.Pos))
	}

	name, err := symbolArg(clause, 1, "argument")
	if err != nil {
		return err
	}

	ref, err := sortRefOf(clause.Elements[2])
	if err != nil {
		return err
	}

	a, err := arityArg(clause, 3)
	if err != nil {
		return err
	}

	decl.args = append(decl.args, argDecl{name: name, sort: ref, arity: a})

	return nil
}

// declareChild reads (child <tag> <arity>).
func declareChild(decl *formDecl, clause sexpr.List) error {
	if len(clause.Elements) != 3 {
		return fmt.Errorf("%s: (child ...) takes a tag and an arity", at(clause.Pos))
	}

	tag, err := symbolArg(clause, 1, "child")
	if err != nil {
		return err
	}

	a, err := arityArg(clause, 2)
	if err != nil {
		return err
	}

	if _, exists := decl.childArity[tag]; exists {
		return fmt.Errorf("%s: form %q admits child %q twice", at(clause.Pos), decl.tag, tag)
	}

	decl.children = append(decl.children, tag)
	decl.childArity[tag] = a

	return nil
}

// sortRefOf reads what an argument's position admits: a sort name, or an inline
// (values ...) set.
func sortRefOf(node sexpr.Node) (sortRef, error) {
	switch node := node.(type) {
	case sexpr.Symbol:
		return sortRef{name: node.Value}, nil
	case sexpr.List:
		kind, err := tagOf(node)
		if err != nil {
			return sortRef{}, err
		}

		if kind != "values" {
			return sortRef{}, fmt.Errorf("%s: an argument's sort is a name or an inline (values ...), not %q", at(node.Pos), kind)
		}

		if len(node.Elements) < 2 {
			return sortRef{}, fmt.Errorf("%s: (values ...) with no members admits nothing", at(node.Pos))
		}

		ref := sortRef{}

		for i := range node.Elements[1:] {
			value, err := symbolArg(node, i+1, "values")
			if err != nil {
				return sortRef{}, err
			}

			ref.values = append(ref.values, value)
		}

		return ref, nil
	default:
		return sortRef{}, fmt.Errorf("%s: an argument's sort is a name or an inline (values ...)", at(posOf(node)))
	}
}

// checkArgumentOrder refuses a form whose arguments cannot be matched
// positionally without guessing.
//
// Arguments are consumed in order by count, so only the last may repeat and
// only the last may be optional — anything else leaves two readings of the same
// list. A form that both repeats its last argument and admits children is
// refused for the same reason: there would be nothing to say where the
// arguments stop and the children start.
func checkArgumentOrder(decl *formDecl) error {
	for i, arg := range decl.args {
		if i == len(decl.args)-1 {
			continue
		}

		if arg.arity.repeatable() || arg.arity.min == 0 {
			return fmt.Errorf("%s: argument %q of form %q is %s, which only the last argument may be", at(decl.pos), arg.name, decl.tag, arg.arity.label())
		}
	}

	if len(decl.args) == 0 || len(decl.children) == 0 {
		return nil
	}

	if decl.args[len(decl.args)-1].arity.repeatable() {
		return fmt.Errorf("%s: form %q repeats its last argument and admits children, so nothing says where the arguments stop", at(decl.pos), decl.tag)
	}

	return nil
}

// link resolves what one declaration says about another, once every declaration
// has been read.
//
// Everything it checks is a way for the schema to be self-consistent and wrong:
// a position naming a sort nobody declared, a sort and a form sharing a name so
// that (in ...) is ambiguous, a form declaring itself into a sort that does not
// list it, a child nothing admits. Each of those publishes a contract with a
// hole in it, and the hole is invisible until a generator falls into it.
func (s *Schema) link() error {
	if err := s.linkNames(); err != nil {
		return err
	}

	if err := s.linkSorts(); err != nil {
		return err
	}

	if err := s.linkIncludes(); err != nil {
		return err
	}

	return s.linkForms()
}

// linkNames refuses a schema in which a sort and a child form share a name.
//
// Nothing in this loader is ambiguous about such a name: (in ...) resolves
// against the sorts alone, and a child is reached through its parent's
// (child ...) rather than through a context. The collision costs the reader
// instead, and the reader is a generator author holding the published file as
// the contract. `(in charset)` would name the sort while `(child charset ...)`
// named the form, and a diagnostic saying `charset` would say nothing about
// which of the two it meant. One name for one thing is worth keeping while it
// costs a check.
func (s *Schema) linkNames() error {
	for _, name := range s.sortNames() {
		if _, exists := s.children[name]; exists {
			return fmt.Errorf("%s: %q is both a sort and a child form, and one name in this schema names one thing", at(s.sorts[name].pos), name)
		}
	}

	return nil
}

// linkIncludes refuses a sort that reaches itself through the sorts it
// includes.
//
// A cycle is the one schema fault that would survive loading and then not
// terminate: checking a value against a sort walks that sort's includes, so two
// sorts including each other is an infinite descent at validation time rather
// than a diagnostic. Catching it here is what lets the walk in validate.go stay
// a plain recursion, and it is caught at the same point as every other way a
// schema can parse and still be unusable.
//
// [Schema.linkSorts] has already rejected an include naming nothing, so
// anything not in the sort table here is a primitive and has no includes of its
// own.
func (s *Schema) linkIncludes() error {
	const (
		unvisited = iota
		onPath
		settled
	)

	state := make(map[string]int, len(s.sorts))

	var walk func(name string) error

	walk = func(name string) error {
		switch state[name] {
		case onPath:
			return fmt.Errorf("%s: sort %q includes itself, by way of the sorts it includes", at(s.sorts[name].pos), name)
		case settled:
			return nil
		case unvisited:
			// Walked below. Named rather than left as the default so that the
			// three states a sort can be in are all in one place.
		}

		state[name] = onPath

		for _, included := range s.sorts[name].include {
			if _, exists := s.sorts[included]; !exists {
				continue
			}

			if err := walk(included); err != nil {
				return err
			}
		}

		state[name] = settled

		return nil
	}

	for _, name := range s.sortNames() {
		if err := walk(name); err != nil {
			return err
		}
	}

	return nil
}

// sortNames returns every declared sort's name, sorted, so that a schema with
// two problems fails on the same one every time it is loaded.
func (s *Schema) sortNames() []string {
	names := make([]string, 0, len(s.sorts))
	for name := range s.sorts {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}

// linkSorts resolves each sort's members.
func (s *Schema) linkSorts() error {
	for _, name := range s.sortNames() {
		decl := s.sorts[name]

		for _, included := range decl.include {
			if !s.knownSort(included) {
				return fmt.Errorf("%s: sort %q includes %q, which nothing declares", at(decl.pos), decl.name, included)
			}
		}

		if decl.isReference() {
			form, exists := s.contextual[contextKey{context: contextTopLevel, tag: decl.refForm}]
			if !exists {
				return fmt.Errorf("%s: sort %q names %q, which is not a top-level form", at(decl.pos), decl.name, decl.refForm)
			}

			if !slices.ContainsFunc(form.args, func(arg argDecl) bool { return arg.name == decl.refArg }) {
				return fmt.Errorf("%s: sort %q names argument %q of form %q, which has no such argument", at(decl.pos), decl.name, decl.refArg, decl.refForm)
			}
		}

		for _, tag := range decl.forms {
			if _, exists := s.contextual[contextKey{context: decl.name, tag: tag}]; !exists {
				return fmt.Errorf("%s: sort %q lists form %q, which does not declare (in %s)", at(decl.pos), decl.name, tag, decl.name)
			}
		}
	}

	return nil
}

// linkForms resolves what each form's clauses name.
func (s *Schema) linkForms() error {
	admitted := make(map[string]bool)

	for _, decl := range s.allForms() {
		if err := s.linkForm(decl); err != nil {
			return err
		}

		for _, tag := range decl.children {
			admitted[tag] = true
		}
	}

	tags := make([]string, 0, len(s.children))
	for tag := range s.children {
		tags = append(tags, tag)
	}

	slices.Sort(tags)

	for _, tag := range tags {
		if !admitted[tag] {
			return fmt.Errorf("%s: child form %q is declared and no form admits it", at(s.children[tag].pos), tag)
		}
	}

	return nil
}

// linkForm resolves one form's context, arity, layer, arguments and children.
func (s *Schema) linkForm(decl *formDecl) error {
	switch {
	case decl.in == contextTopLevel:
		if decl.arity.spelling == "" {
			return fmt.Errorf("%s: top-level form %q states no arity", at(decl.pos), decl.tag)
		}

		if decl.layer == "" {
			return fmt.Errorf("%s: top-level form %q states no layer", at(decl.pos), decl.tag)
		}
	case decl.in != "":
		sort, exists := s.sorts[decl.in]
		if !exists {
			return fmt.Errorf("%s: form %q declares (in %s), which is not a sort", at(decl.pos), decl.tag, decl.in)
		}

		if !slices.Contains(sort.forms, decl.tag) {
			return fmt.Errorf("%s: form %q declares (in %s), and sort %q does not list it", at(decl.pos), decl.tag, decl.in, decl.in)
		}
	default:
		if decl.arity.spelling != "" {
			return fmt.Errorf("%s: child form %q states an arity, which is the arity of the parent admitting it", at(decl.pos), decl.tag)
		}
	}

	for _, arg := range decl.args {
		if arg.sort.name != "" && !s.knownSort(arg.sort.name) {
			return fmt.Errorf("%s: argument %q of form %q is of sort %q, which nothing declares", at(decl.pos), arg.name, decl.tag, arg.sort.name)
		}
	}

	for _, tag := range decl.children {
		if _, exists := s.children[tag]; !exists {
			return fmt.Errorf("%s: form %q admits child %q, which is not declared as a child form", at(decl.pos), decl.tag, tag)
		}
	}

	return nil
}

// allForms returns every declared form, contextual and child alike, in a stable
// order so that two loads of one schema fail on the same declaration.
func (s *Schema) allForms() []*formDecl {
	forms := make([]*formDecl, 0, len(s.contextual)+len(s.children))

	keys := make([]contextKey, 0, len(s.contextual))
	for key := range s.contextual {
		keys = append(keys, key)
	}

	slices.SortFunc(keys, func(a, b contextKey) int {
		if a.context != b.context {
			return strings.Compare(a.context, b.context)
		}

		return strings.Compare(a.tag, b.tag)
	})

	for _, key := range keys {
		forms = append(forms, s.contextual[key])
	}

	tags := make([]string, 0, len(s.children))
	for tag := range s.children {
		tags = append(tags, tag)
	}

	slices.Sort(tags)

	for _, tag := range tags {
		forms = append(forms, s.children[tag])
	}

	return forms
}

// knownSort reports whether name is a primitive or a declared sort.
func (s *Schema) knownSort(name string) bool {
	if slices.Contains(primitives, name) {
		return true
	}

	_, exists := s.sorts[name]

	return exists
}

// tagOf returns the symbol a list opens with, which is what makes it a form.
func tagOf(list sexpr.List) (string, error) {
	if len(list.Elements) == 0 {
		return "", fmt.Errorf("%s: the empty list has no tag", at(list.Pos))
	}

	symbol, ok := list.Elements[0].(sexpr.Symbol)
	if !ok {
		return "", fmt.Errorf("%s: a form opens with a symbol naming it", at(posOf(list.Elements[0])))
	}

	return symbol.Value, nil
}

// symbolArg reads the symbol at index, naming the clause it belongs to when it
// is not one.
func symbolArg(list sexpr.List, index int, clause string) (string, error) {
	if index >= len(list.Elements) {
		return "", fmt.Errorf("%s: (%s ...) is missing an argument", at(list.Pos), clause)
	}

	symbol, ok := list.Elements[index].(sexpr.Symbol)
	if !ok {
		return "", fmt.Errorf("%s: argument %d of (%s ...) is a symbol", at(posOf(list.Elements[index])), index, clause)
	}

	return symbol.Value, nil
}

// arityArg reads the arity spelling at index.
func arityArg(list sexpr.List, index int) (arity, error) {
	spelling, err := symbolArg(list, index, "arity")
	if err != nil {
		return arity{}, err
	}

	known, exists := arities[spelling]
	if !exists {
		return arity{}, fmt.Errorf("%s: unknown arity %q", at(list.Pos), spelling)
	}

	return known, nil
}

// posOf returns the position a node carries. Every node kind carries one, and
// the switch is exhaustive because the interface is sealed by the package that
// declares it.
func posOf(node sexpr.Node) sexpr.Pos {
	switch node := node.(type) {
	case sexpr.Symbol:
		return node.Pos
	case sexpr.String:
		return node.Pos
	case sexpr.Int:
		return node.Pos
	case sexpr.Float:
		return node.Pos
	case sexpr.Bool:
		return node.Pos
	case sexpr.Nil:
		return node.Pos
	case sexpr.List:
		return node.Pos
	case sexpr.Quote:
		return node.Pos
	default:
		return sexpr.Pos{}
	}
}

// at renders a position the way every message in this package opens.
func at(pos sexpr.Pos) string {
	return fmt.Sprintf("line %d, column %d", pos.Line, pos.Column)
}
