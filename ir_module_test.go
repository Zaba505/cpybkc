// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package cpybkc_test

import (
	"reflect"
	"testing"

	"github.com/Zaba505/cpybkc/irpb"
	"google.golang.org/protobuf/proto"
)

// irModulePath is the published IR module, as this module requires it.
const irModulePath = "github.com/Zaba505/cpybkc/irpb"

// TestTheCLIConsumesThePublishedIRModule asserts the direction of the
// dependency between this repository's two modules.
//
// The IR is not a package of this module that also happens to be exported: it
// is a module of its own, and this one is a consumer of it on the same terms as
// any generator plugin. That is what the require and the replace in go.mod say,
// and what this test compiles into a fact — the type below is reachable from
// here, and its package path is the published module's rather than any path
// inside this one.
//
// It was written because nothing else in this module imported the IR, which
// made the requirement in go.mod a line no build depended on and so a line
// `go mod tidy` removes without anybody noticing that the arrow went with it.
// That hazard is gone: internal/emit encodes and writes a descriptor (#20), so
// the import is load-bearing and the arrow is asserted by the build. What is
// left here is the same claim stated in one place rather than inferred from an
// import, and it goes when resolve assembles a descriptor (#38) and the whole
// CLI puts one through this module in the ordinary way.
func TestTheCLIConsumesThePublishedIRModule(t *testing.T) {
	d := &irpb.Descriptor{Version: irpb.IrVersion_IR_VERSION_1}

	if got := reflect.TypeFor[irpb.Descriptor]().PkgPath(); got != irModulePath {
		t.Errorf("IR types resolve to %q, want %q: the IR a plugin author imports and the IR this module builds against must be the same package", got, irModulePath)
	}

	b, err := proto.Marshal(d)
	if err != nil {
		t.Fatalf("marshal a descriptor: %v", err)
	}

	if len(b) == 0 {
		t.Error("marshalling a descriptor at IR version 1 produced no bytes, so the version field this module set did not survive")
	}
}
