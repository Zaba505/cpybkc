// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Command layout-schema writes the published layout schema — the released
// layout-schema.sexpr — to a file, after checking that it loads.
//
// It is the third artifact a release carries, beside the IR's ir.binpb and
// ir-protos.tar.gz, and it is for a different consumer than either: not a plugin
// author decoding a resolved descriptor, but the adopter generating layout files
// from metadata they already hold. docs/layout/SPEC.md's "The published schema"
// is what this file is; the schema is the contract a generator targets and
// cpybkc's own validator reads.
//
// # Why a command rather than an upload of the file
//
// The bytes it writes are the bytes of schema/layout.sexpr, unchanged. What this
// adds is the check: it loads the schema through
// [github.com/Zaba505/cpybkc/internal/layoutschema] first, and refuses to write
// anything if it will not load.
//
// That is worth a command because of what the failure looks like otherwise. A
// schema with a form declaring itself into a sort nothing lists, or a child no
// form admits, parses as S-expressions and publishes as happily as a good one —
// and the hole is invisible until a generator falls into it, at which point the
// artifact naming the version it fell out of has already shipped. The Go tests
// hold the published schema to docs/layout/SPEC.md on every pull request; this
// holds it once more at the point the bytes leave the repository, on the path
// that runs when nobody is watching.
//
// It transforms nothing on the way out for the reason the format exists: an
// adopter's diagnostic and this repository's diagnostic have to be about the
// same text, and a published artifact rewritten from a parse would be a second
// spelling of one schema.
//
// It lives under internal/ for the reason ir-protos does: nothing outside this
// repository has a reason to run it, and cmd/ is where a shipped command goes.
//
//	go run ./internal/tools/layout-schema -o layout-schema.sexpr
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Zaba505/cpybkc/internal/layoutschema"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "layout-schema: %v\n", err)
		os.Exit(1)
	}
}

// run is separated from main so that the exit path is the only thing main owns,
// and so that a test can drive the whole program without ending the test binary.
func run(args []string) error {
	flags := flag.NewFlagSet("layout-schema", flag.ContinueOnError)
	out := flags.String("o", "", "path to write the schema to (required)")
	source := flags.String("schema", layoutschema.SchemaFile, "the schema to publish")

	if err := flags.Parse(args); err != nil {
		return err
	}

	if *out == "" {
		return fmt.Errorf("-o is required: name the file to write")
	}

	if rest := flags.Args(); len(rest) > 0 {
		return fmt.Errorf("unexpected arguments %v", rest)
	}

	b, err := os.ReadFile(*source)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", *source, err)
	}

	// Loaded and thrown away. Nothing here needs the parsed schema; what is
	// needed is the assurance that it parses and hangs together, which is the
	// whole reason this artifact goes through a program rather than a copy.
	if _, err := layoutschema.Load(bytes.NewReader(b)); err != nil {
		return fmt.Errorf("%s is not a schema this repository can load: %w", *source, err)
	}

	// The parent directory is created rather than required, for the reason
	// ir-protos gives: the caller is a pipeline naming a path in a fresh
	// container filesystem.
	if dir := filepath.Dir(*out); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create %s: %w", dir, err)
		}
	}

	if err := os.WriteFile(*out, b, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", *out, err)
	}

	return nil
}
