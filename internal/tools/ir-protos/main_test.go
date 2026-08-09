// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"
)

// TestArchiveIsReproducible is the acceptance criterion for this artifact: the
// same schema produces the same bytes. It archives one tree twice through two
// independent writers, because a producer that memoised its output would pass a
// comparison of one run against itself while still varying between builds.
func TestArchiveIsReproducible(t *testing.T) {
	schema := fstest.MapFS{
		"cpybkc/ir/v1/ir.proto": &fstest.MapFile{Data: []byte("syntax = \"proto3\";\n")},
		"cpybkc/ir/v1/b.proto":  &fstest.MapFile{Data: []byte("syntax = \"proto3\";\n")},
	}

	first := archive(t, schema)
	second := archive(t, schema)

	if len(first) == 0 {
		t.Fatal("the archive is empty")
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("two archives of the same schema differ: %d bytes then %d bytes", len(first), len(second))
	}
}

// TestArchiveCarriesNothingFromTheFilesystem asserts what makes the property
// above hold across machines rather than only across two calls in one process.
// A modification time, an owner or a group taken from the tree being archived
// would make the artifact a function of the checkout as well as of the schema,
// and the two builds that disagree would be on different machines, where nobody
// is comparing.
func TestArchiveCarriesNothingFromTheFilesystem(t *testing.T) {
	for _, header := range headers(t, archive(t, fstest.MapFS{
		"cpybkc/ir/v1/ir.proto": &fstest.MapFile{Data: []byte("syntax = \"proto3\";\n")},
	})) {
		if got := header.ModTime.Unix(); got != 0 {
			t.Errorf("%s carries modification time %d, want 0", header.Name, got)
		}

		if header.Uid != 0 || header.Gid != 0 {
			t.Errorf("%s carries uid/gid %d/%d, want 0/0", header.Name, header.Uid, header.Gid)
		}

		if header.Uname != "" || header.Gname != "" {
			t.Errorf("%s carries owner names %q/%q, want empty", header.Name, header.Uname, header.Gname)
		}

		if got := header.Mode; got != 0o644 {
			t.Errorf("%s carries mode %o, want 644", header.Name, got)
		}
	}
}

// TestArchivePreservesTheSchemaLayout asserts the reason this is an archive at
// all. A .proto's path is part of its identity — it is the name a
// FileDescriptorProto carries and the string an import resolves — so an entry
// flattened to its base name would hand a consumer a file that compiles into
// descriptors naming a path this project does not publish.
func TestArchivePreservesTheSchemaLayout(t *testing.T) {
	schema := fstest.MapFS{
		"cpybkc/ir/v1/ir.proto":    &fstest.MapFile{Data: []byte("a")},
		"cpybkc/ir/v1/other.proto": &fstest.MapFile{Data: []byte("b")},
		"README.md":                &fstest.MapFile{Data: []byte("not a schema")},
	}

	want := []string{"cpybkc/ir/v1/ir.proto", "cpybkc/ir/v1/other.proto"}

	if got := entryNames(t, archive(t, schema)); !slices.Equal(got, want) {
		t.Errorf("archived %v, want %v: only .proto files are published, at their schema-relative paths and in sorted order", got, want)
	}
}

// TestArchiveRefusesASchemaRootWithNoProtos keeps a misdirected -source from
// producing a well-formed empty archive. That failure is invisible at the point
// it happens and arrives as a consumer's release download containing nothing.
func TestArchiveRefusesASchemaRootWithNoProtos(t *testing.T) {
	if err := writeArchive(io.Discard, fstest.MapFS{"README.md": &fstest.MapFile{Data: []byte("x")}}); err == nil {
		t.Error("writeArchive accepted a schema root holding no .proto files")
	}
}

// TestRunArchivesTheSchemaOnDisk is the staleness gate. Everything above works
// on a tree a test built, which cannot notice that the artifact has stopped
// covering the schema this repository actually publishes. So this one runs the
// program over proto/ and requires the archive to hold every .proto in it and
// nothing else.
//
// It is here rather than in irpb because proto/ is not in that module. A test
// there could only reach this directory by climbing out of its own module, which
// works in a checkout and not in the module zip a plugin author downloads.
func TestRunArchivesTheSchemaOnDisk(t *testing.T) {
	root := repoRoot(t)
	out := filepath.Join(t.TempDir(), "ir-protos.tar.gz")

	if err := run([]string{"-source", filepath.Join(root, "proto"), "-o", out}); err != nil {
		t.Fatalf("run: %v", err)
	}

	want, err := protoFiles(os.DirFS(filepath.Join(root, "proto")))
	if err != nil {
		t.Fatalf("read proto/: %v", err)
	}

	if len(want) == 0 {
		t.Fatal("no .proto files found under proto/: the check would pass vacuously")
	}

	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read %s: %v", out, err)
	}

	if got := entryNames(t, b); !slices.Equal(got, want) {
		t.Errorf("the published archive does not cover proto/:\n got: %v\nwant: %v", got, want)
	}
}

// TestRunRejectsBadUsage keeps the failure modes failures, for the reason
// ir-descriptor-set's copy of this test gives: a release job that uploads
// nothing and reports success is the one outcome nobody notices.
func TestRunRejectsBadUsage(t *testing.T) {
	testCases := []struct {
		name string
		args []string
	}{
		{name: "no output path", args: nil},
		{name: "empty output path", args: []string{"-o", ""}},
		{name: "unexpected operand", args: []string{"-o", filepath.Join(t.TempDir(), "a.tar.gz"), "extra"}},
		{name: "missing schema root", args: []string{"-o", filepath.Join(t.TempDir(), "b.tar.gz"), "-source", filepath.Join(t.TempDir(), "nowhere")}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := run(testCase.args); err == nil {
				t.Error("run accepted arguments it should have refused")
			}
		})
	}
}

// archive writes schema through writeArchive and returns the bytes.
func archive(t *testing.T, schema fs.FS) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := writeArchive(&buf, schema); err != nil {
		t.Fatalf("writeArchive: %v", err)
	}

	return buf.Bytes()
}

// headers reads every tar header out of a gzipped archive.
func headers(t *testing.T, b []byte) []*tar.Header {
	t.Helper()

	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("open the gzip stream: %v", err)
	}

	var got []*tar.Header

	reader := tar.NewReader(gz)

	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			t.Fatalf("read the archive: %v", err)
		}

		got = append(got, header)
	}

	if err := gz.Close(); err != nil {
		t.Fatalf("close the gzip stream: %v", err)
	}

	return got
}

// entryNames reads the entry paths out of a gzipped archive, in the order they
// were written.
func entryNames(t *testing.T, b []byte) []string {
	t.Helper()

	var names []string
	for _, header := range headers(t, b) {
		names = append(names, header.Name)
	}

	return names
}

// repoRoot walks up from the test's working directory to the directory holding
// go.mod, which for this module is the repository root and so the directory
// proto/ sits in.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found above %q", dir)
		}

		dir = parent
	}
}
