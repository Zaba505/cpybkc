// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Command ir-protos writes the IR schema sources — the published
// ir-protos.tar.gz — to a file.
//
// It is the other half of what a release carries for a consumer who is not
// importing [github.com/Zaba505/cpybkc/irpb]. ir.binpb is for the plugin author
// whose language cannot run protoc; this is for the one who can, and who wants
// to generate bindings from the schema rather than decode against a
// FileDescriptorSet. Neither substitutes for the other, and both are attached to
// every release so that a version of the IR is one download away in whichever
// form the consumer's build already understands.
//
// # Why an archive rather than the file
//
// proto/ holds exactly one .proto today, so a single attached ir.proto would
// have been simpler and would have been wrong. A .proto's path is part of its
// identity: it is the name a FileDescriptorProto carries, the string an import
// in another schema resolves, and — because buf.yaml enforces
// PACKAGE_DIRECTORY_MATCH — the directory layout the protobuf package name
// requires. Flattening cpybkc/ir/v1/ir.proto to ir.proto hands a consumer a file
// that compiles and produces descriptors naming a path this project does not
// publish. The archive carries the layout, so unpacking it yields an include
// root a compiler can be pointed at directly:
//
//	tar xf ir-protos.tar.gz && protoc -I. --python_out=. cpybkc/ir/v1/ir.proto
//
// It also means a second .proto arrives in the artifact without anything here
// changing, which is the same reason the Dagger function that regenerates the Go
// stubs returns a directory.
//
// # Why the bytes are stable
//
// Every field a tar header could take from the filesystem — modification time,
// owner, group, permissions beyond the mode written here — is set to a constant,
// and the entries are emitted in sorted order rather than in the order a
// directory read happened to return. What is left is a function of the file
// names and the file contents, so two builds of one commit produce one artifact.
// The gzip layer contributes no timestamp and no original file name for the same
// reason; its compressed output is fixed by the Go release the pipeline's
// container pins, which is where this repository's toolchain version already
// lives.
//
// It lives under internal/ for the reason ir-descriptor-set does: nothing
// outside this repository has a reason to run it, and cmd/ is where a shipped
// command goes.
//
//	go run ./internal/tools/ir-protos -o ir-protos.tar.gz
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"time"
)

// epoch is the modification time every entry carries.
//
// A constant rather than SOURCE_DATE_EPOCH, because there is no timestamp in
// this artifact that means anything: the archive holds source files whose
// history is git's, and a build time recorded here would be the one field
// distinguishing two builds of the same commit. Zero is the conventional choice
// and the one a reader recognises as deliberate.
var epoch = time.Unix(0, 0).UTC()

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "ir-protos: %v\n", err)
		os.Exit(1)
	}
}

// run is separated from main so that the exit path is the only thing main owns,
// and so that a test can drive the whole program without ending the test binary.
func run(args []string) error {
	flags := flag.NewFlagSet("ir-protos", flag.ContinueOnError)
	out := flags.String("o", "", "path to write the archive to (required)")
	source := flags.String("source", "proto", "the schema root to archive; its layout is preserved inside the archive")

	if err := flags.Parse(args); err != nil {
		return err
	}

	if *out == "" {
		return fmt.Errorf("-o is required: name the file to write")
	}

	if rest := flags.Args(); len(rest) > 0 {
		return fmt.Errorf("unexpected arguments %v", rest)
	}

	// The parent directory is created rather than required, for the reason
	// ir-descriptor-set gives: the caller is a pipeline naming a path in a fresh
	// container filesystem.
	if dir := filepath.Dir(*out); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create %s: %w", dir, err)
		}
	}

	// Built in memory and written once, so that a failure part-way through
	// leaves no half-written archive for a release job to upload. The IR schema
	// is kilobytes; buffering it is cheaper than the ordering this would
	// otherwise need.
	var buf bytes.Buffer

	if err := writeArchive(&buf, os.DirFS(*source)); err != nil {
		return err
	}

	if err := os.WriteFile(*out, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", *out, err)
	}

	return nil
}

// writeArchive writes every .proto under schema to w as a gzipped tar,
// preserving each file's path relative to schema.
//
// It takes an [io/fs.FS] rather than a directory name so that the determinism
// the package comment claims can be asserted against a tree a test builds,
// rather than only against the one on disk beside it.
//
// Only .proto files are archived. The schema root holds nothing else today, and
// naming what goes in rather than what stays out is what keeps a future
// generated file or an editor's leavings from being published as part of the
// contract.
func writeArchive(w io.Writer, schema fs.FS) error {
	names, err := protoFiles(schema)
	if err != nil {
		return err
	}

	if len(names) == 0 {
		return fmt.Errorf("no .proto files found: an empty archive would be published as though it were the schema")
	}

	// BestCompression rather than the default, because the artifact is written
	// once per release and downloaded many times, and because a fixed level is
	// one more thing the output does not vary with.
	gz, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("failed to start the gzip stream: %w", err)
	}

	// Neither field is set from the artifact being written: a name here would
	// record whatever the caller passed to -o, and a modification time would be
	// the build's clock.
	gz.Name = ""
	gz.ModTime = time.Time{}

	archive := tar.NewWriter(gz)

	for _, name := range names {
		if err := writeEntry(archive, schema, name); err != nil {
			return err
		}
	}

	if err := archive.Close(); err != nil {
		return fmt.Errorf("failed to finish the archive: %w", err)
	}

	if err := gz.Close(); err != nil {
		return fmt.Errorf("failed to finish the gzip stream: %w", err)
	}

	return nil
}

// protoFiles returns every .proto under schema, as slash-separated paths
// relative to it, in sorted order.
//
// [io/fs.WalkDir] already walks lexically, so the sort is belt and braces — but
// the ordering is what makes the archive a function of its contents rather than
// of a directory read, and a property that load-bearing is worth stating where a
// reader can see it.
func protoFiles(schema fs.FS) ([]string, error) {
	var names []string

	err := fs.WalkDir(schema, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() || path.Ext(name) != ".proto" {
			return nil
		}

		names = append(names, name)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read the schema root: %w", err)
	}

	slices.Sort(names)

	return names, nil
}

// writeEntry writes one file into the archive under a header carrying nothing
// the filesystem supplied.
//
// The header's Format is left unset, so the tar writer picks the narrowest
// encoding each header fits in — USTAR for the paths this schema has, PAX for
// one long enough to need it. That is a function of the header, so it costs
// nothing in determinism and it means a deeply nested .proto is archived rather
// than refused.
func writeEntry(archive *tar.Writer, schema fs.FS, name string) error {
	b, err := fs.ReadFile(schema, name)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", name, err)
	}

	header := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     name,
		Size:     int64(len(b)),
		Mode:     0o644,
		ModTime:  epoch,
	}

	if err := archive.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write the header for %s: %w", name, err)
	}

	if _, err := archive.Write(b); err != nil {
		return fmt.Errorf("failed to write %s: %w", name, err)
	}

	return nil
}
