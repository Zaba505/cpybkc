// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutschema

import (
	"fmt"
	"slices"
	"strings"
)

// A form written in a position the schema declares it for somewhere else is
// reported as what it is rather than as unknown: an `entry` at the top level of
// a layout, a `definitions-block` inside a record. docs/layout/SPEC.md's "How a
// definitions file and a layout are told apart" asks for those diagnostics to
// name what was found, and the helpers here are how they do it without this
// package naming a tag. Each reads the declarations, as collectReferents does,
// so a form the schema gains is placed by the same code.

// writtenIn says where a tag the top level does not admit is written instead —
// "inside a definitions-block, or in a definitions file a record names" — or
// returns "" when the schema declares the tag nowhere at all.
//
// A contextual form is written inside each form that takes its sort, and a
// child form inside each form that admits it. The one phrase not read off a
// position is the definitions file: a file is one block's body, so a form a
// block's body holds is also written there, and [BlockForm] is the tag this
// package already names for that reason.
func (s *Schema) writtenIn(tag string) string {
	var holders []string

	for _, decl := range s.allForms() {
		if decl.tag != tag || decl.in == contextTopLevel {
			continue
		}

		if decl.in == "" {
			holders = appendNew(holders, s.parentsOf(tag)...)

			continue
		}

		holders = appendNew(holders, s.takersOf(decl.in)...)
	}

	if len(holders) == 0 {
		return ""
	}

	where := make([]string, 0, len(holders)+1)
	for _, holder := range holders {
		where = append(where, article(holder))
	}

	phrase := "inside " + and(where)

	if body, ok := s.blockBody(); ok && slices.Contains(s.sorts[body.sort.name].forms, tag) {
		phrase += ", or in a definitions file a record names"
	}

	return phrase
}

// topLevelHint is what a diagnostic about a top-level form met in another
// position adds: that the form is written at the top level, and, where exactly
// one top-level form names it through a reference, which one that is.
//
// So a `definitions-block` written inside a record reads "definitions-block is
// a top-level form, and a record names one with (definitions ...)" — the block
// moves out, and the record keeps a source naming it. A `record` written inside
// another form gains the first half alone: records are named from too many
// places for one of them to be the hint.
func (s *Schema) topLevelHint(tag string) string {
	if _, top := s.contextual[contextKey{context: contextTopLevel, tag: tag}]; !top {
		return ""
	}

	var hint strings.Builder

	fmt.Fprintf(&hint, "; %s is a top-level form", tag)

	for _, name := range s.sortNames() {
		decl := s.sorts[name]
		if !decl.isReference() || decl.refForm != tag {
			continue
		}

		via := s.takersOf(name)
		roots := s.rootsOf(name)

		if len(via) != 1 || len(roots) != 1 {
			continue
		}

		if via[0] == roots[0] {
			fmt.Fprintf(&hint, ", and %s names one", article(roots[0]))
		} else {
			fmt.Fprintf(&hint, ", and %s names one with (%s ...)", article(roots[0]), via[0])
		}
	}

	return hint.String()
}

// takersOf is every form with an argument whose sort admits the members of the
// named sort, by tag, in the stable order [Schema.allForms] gives.
func (s *Schema) takersOf(sort string) []string {
	var takers []string

	for _, decl := range s.allForms() {
		for _, arg := range decl.args {
			if arg.sort.name != "" && s.includes(arg.sort.name, sort) {
				takers = appendNew(takers, decl.tag)

				break
			}
		}
	}

	return takers
}

// rootsOf is every top-level form a member of the named sort is written
// beneath, however many contextual forms stand between: `block-name` is taken
// by `definitions`, which is in `record-source`, which `record` takes — so its
// root is `record`.
func (s *Schema) rootsOf(sort string) []string {
	var roots []string

	seen := make(map[string]bool)

	var walk func(sort string)

	walk = func(sort string) {
		if seen[sort] {
			return
		}

		seen[sort] = true

		for _, decl := range s.allForms() {
			if !slices.ContainsFunc(decl.args, func(arg argDecl) bool {
				return arg.sort.name != "" && s.includes(arg.sort.name, sort)
			}) {
				continue
			}

			switch decl.in {
			case contextTopLevel:
				roots = appendNew(roots, decl.tag)
			case "":
				for _, parent := range s.parentsOf(decl.tag) {
					if _, top := s.contextual[contextKey{context: contextTopLevel, tag: parent}]; top {
						roots = appendNew(roots, parent)
					}
				}
			default:
				walk(decl.in)
			}
		}
	}

	walk(sort)

	return roots
}

// parentsOf is every form admitting the child form tag.
func (s *Schema) parentsOf(tag string) []string {
	var parents []string

	for _, decl := range s.allForms() {
		if _, admitted := decl.childArity[tag]; admitted {
			parents = appendNew(parents, decl.tag)
		}
	}

	return parents
}

// includes reports whether the sort outer admits every member of inner: it is
// inner, or includes it by way of the sorts it includes. The loader has refused
// a cycle among includes, so the walk terminates.
func (s *Schema) includes(outer, inner string) bool {
	if outer == inner {
		return true
	}

	decl, exists := s.sorts[outer]
	if !exists {
		return false
	}

	for _, included := range decl.include {
		if s.includes(included, inner) {
			return true
		}
	}

	return false
}

// article puts the indefinite article before a tag, so that a diagnostic
// naming a form reads as a sentence: "a record", "an encoding-override".
func article(tag string) string {
	if tag != "" && strings.ContainsRune("aeiou", rune(tag[0])) {
		return "an " + tag
	}

	return "a " + tag
}

// appendNew appends each of items not already in list.
func appendNew(list []string, items ...string) []string {
	for _, item := range items {
		if !slices.Contains(list, item) {
			list = append(list, item)
		}
	}

	return list
}
