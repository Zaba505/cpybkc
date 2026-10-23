// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package schema carries the published layout schema into every binary that
// checks a layout against it.
//
// The schema is layout.sexpr, beside this file. It is the file a release
// publishes as layout-schema.sexpr (internal/tools/layout-schema writes its bytes
// unchanged), and docs/layout/SPEC.md's "Validation and diagnostics" says the
// reader performs the checks a declaration states "by reading the schema". So
// the run reads these bytes, embedded at build time, rather than a copy of what
// they say written out in Go — a schema an adopter targets and a schema cpybkc
// checks against are then one file, and cannot drift apart.
//
// It is a package of its own, rather than an embed in the package that reads it,
// because a //go:embed reaches only files under the directory of the package
// declaring it, and the schema lives at the path the release, the tests and the
// Dagger function all name it by.
package schema

import _ "embed"

// Layout is the text of the published layout schema, schema/layout.sexpr.
//
// It is the source text and nothing parsed from it, for the reason the release
// tool transforms nothing on the way out: a diagnostic an adopter's validator
// reports and one cpybkc reports have to be about the same text.
// [github.com/Zaba505/cpybkc/internal/layoutschema.Published] is what loads it.
//
// It is a function over a string rather than an exported variable so that
// nothing linked into a binary can change the schema that binary checks
// against.
func Layout() string { return layout }

//go:embed layout.sexpr
var layout string
