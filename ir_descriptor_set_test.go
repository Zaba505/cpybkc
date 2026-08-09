// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package cpybkc_test

import (
	"io/fs"
	"os"
	"path"
	"slices"
	"testing"

	"github.com/Zaba505/cpybkc/irpb"
)

// TestPublishedFileDescriptorSetCoversTheSchema is the staleness gate on the
// published ir.binpb.
//
// The set is computed from the descriptors protoc-gen-go compiled into irpb, so
// it cannot drift from them. What it can do is quietly stop covering proto/,
// which is what happens when a new IR message lands in a file nothing in the
// descriptor's import graph reaches: the artifact still builds, still decodes
// and still looks complete, and a consumer decoding dynamically meets a field
// whose type it does not describe.
//
// So the assertion is an equality against the directory rather than against
// another copy of the same descriptors, and it carries no exclusions. proto/ is
// the IR and the IR defines exactly one message a plugin consumes, so there is
// nothing a file there can be that the descriptor does not reach — a .proto that
// fails this is a mistake in the schema, not something a later change should be
// able to name its way past.
//
// It lives in this module rather than in irpb because proto/ is not part of that
// module. A test there could reach this directory only by climbing out of its
// own module root, which works in a checkout and not in the module zip a plugin
// author downloads. This module already asserts cross-module facts about irpb —
// see TestTheCLIConsumesThePublishedIRModule — and its root is the directory
// proto/ sits in.
func TestPublishedFileDescriptorSetCoversTheSchema(t *testing.T) {
	want, err := schemaFiles()
	if err != nil {
		t.Fatalf("read proto/: %v", err)
	}

	if len(want) == 0 {
		t.Fatal("no .proto files found under proto/: the check would pass vacuously")
	}

	var got []string
	for _, file := range irpb.FileDescriptorSet().GetFile() {
		got = append(got, file.GetName())
	}

	slices.Sort(got)

	if !slices.Equal(got, want) {
		t.Errorf("the published FileDescriptorSet does not cover proto/:\n got: %v\nwant: %v\n\nA .proto reachable from cpybkc.ir.v1.Descriptor is published automatically; one that is not reachable is a mistake.", got, want)
	}
}

// TestSchemaRootIsWhereTheGateLooks keeps the check above honest about where it
// is looking, so a moved proto/ shows up as this failure rather than as an empty
// glob somewhere else.
func TestSchemaRootIsWhereTheGateLooks(t *testing.T) {
	info, err := os.Stat("proto")
	if err != nil {
		t.Fatalf("stat proto/: %v", err)
	}

	if !info.IsDir() {
		t.Fatal("proto/ is not a directory")
	}
}

// schemaFiles returns every .proto under proto/, as the slash-separated paths
// relative to it that a FileDescriptorProto carries in its name field, sorted.
//
// The path is the comparison and not the base name: buf.yaml enforces
// PACKAGE_DIRECTORY_MATCH, so the directory layout under proto/ is what the
// protobuf package name requires, and a set naming a file by any other path
// would not be describing the schema this repository publishes.
func schemaFiles() ([]string, error) {
	var names []string

	err := fs.WalkDir(os.DirFS("proto"), ".", func(name string, entry fs.DirEntry, err error) error {
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
		return nil, err
	}

	slices.Sort(names)

	return names, nil
}
