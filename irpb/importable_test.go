// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package irpb_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/irpb"
)

// TestImportPathHasNoInternalElement guards the property this package's
// placement was for: Go's internal/ rule is a rule about the import path, so a
// single path element named "internal" anywhere in it puts the IR out of reach
// of the third-party generators it exists for.
func TestImportPathHasNoInternalElement(t *testing.T) {
	pkgPath := reflect.TypeFor[irpb.Descriptor]().PkgPath()

	for elem := range strings.SplitSeq(pkgPath, "/") {
		if elem == "internal" {
			t.Fatalf("IR package %q is unimportable from outside this module: no element of its import path may be %q", pkgPath, "internal")
		}
	}

	if pkgPath != irModulePath {
		t.Errorf("IR package is at %q, want %q: the module root is the import path plugin authors are given", pkgPath, irModulePath)
	}
}

// TestOutOfTreeModuleBuildsAgainstTheIR compiles a module that is not this one
// against the IR types.
//
// The path check above is necessary and not sufficient — it says the compiler
// will allow the import, not that the package presents a usable surface to a
// caller who has only the module — so the contract is asserted the way its
// audience meets it: a separate module, a `require` on this one, a plain import,
// go build. It is the whole of what a generator plugin does before it does
// anything interesting, and nothing short of a second module tests it, because
// every package inside this one can reach the IR whether or not it is published.
func TestOutOfTreeModuleBuildsAgainstTheIR(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go toolchain on PATH to build an out-of-tree module with: %v", err)
	}

	mod := parseGoMod(t, "go.mod")

	protobuf, ok := mod.requires[protobufModulePath]
	if !ok {
		t.Fatalf("go.mod does not require %s, so there is no version to build the out-of-tree module against", protobufModulePath)
	}

	root := moduleRoot(t)
	dir := t.TempDir()

	// A replace against the working tree, so this asserts the IR in hand
	// rather than whichever version happens to be published. The protobuf
	// requirement is read out of this module's go.mod rather than written
	// here, because a consumer resolves the same version this module names
	// and a second copy of it would be a second thing to bump.
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/cpybkc-gen-outoftree\n\n"+
		"go "+mod.goVersion+"\n\n"+
		"require (\n"+
		"\t"+irModulePath+" v0.0.0\n"+
		"\t"+protobufModulePath+" "+protobuf+"\n"+
		")\n\n"+
		"replace "+irModulePath+" => "+root+"\n")

	// A consumer of the IR ends up with the same hashes for the same
	// transitive dependencies, so this module's go.sum is the one the
	// throwaway module would have written for itself. Copying it supplies the
	// checksums without a checksum-database lookup; the module sources
	// themselves come from whatever the surrounding environment resolves them
	// from, which in practice is the cache this module's own build has already
	// filled.
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatalf("read go.sum: %v", err)
	}

	writeFile(t, filepath.Join(dir, "go.sum"), string(sum))

	// Deliberately more than an import: a plugin's first act is to unmarshal a
	// descriptor and switch on a node's kind, and both of those touch parts of
	// the generated surface — the oneof wrapper types, the enum constants —
	// that an import alone would not prove are exported.
	writeFile(t, filepath.Join(dir, "main.go"), `package main

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	"`+irModulePath+`"
)

func main() {
	d := &irpb.Descriptor{
		Version: irpb.IrVersion_IR_VERSION_1,
		Nodes: []*irpb.Node{
			{
				Id: 0,
				Kind: &irpb.Node_File{
					File: &irpb.File{
						Framing:      &irpb.File_Unframed{Unframed: &irpb.Unframed{}},
						StartStateId: 1,
					},
				},
			},
			{
				Id:   1,
				Kind: &irpb.Node_State{State: &irpb.State{Accepts: true}},
			},
		},
	}

	b, err := proto.Marshal(d)
	if err != nil {
		panic(err)
	}

	var got irpb.Descriptor
	if err := proto.Unmarshal(b, &got); err != nil {
		panic(err)
	}

	for _, n := range got.GetNodes() {
		switch n.GetKind().(type) {
		case *irpb.Node_File:
		case *irpb.Node_State:
		default:
			panic("unknown node kind")
		}
	}

	fmt.Println(got.GetVersion(), len(got.GetNodes()), len(b))
}
`)

	// -mod=mod lets the throwaway module record the indirect requirements it
	// picks up from this one, which a consumer's `go get` would record for
	// them. It goes on the command line rather than into GOFLAGS: a
	// command-line flag wins over a GOFLAGS the surrounding environment may
	// already have set, where a second GOFLAGS entry in the child's
	// environment would only shadow the first.
	cmd := exec.Command(goBin, "build", "-mod=mod", "./...")
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("out-of-tree module failed to build against the IR: %v\n%s", err, out)
	}
}

// moduleRoot walks up from the test's working directory to the directory
// holding go.mod.
func moduleRoot(t *testing.T) string {
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
