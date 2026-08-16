// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package irpb_test

import (
	"os"
	"sort"
	"strings"
	"testing"
)

const (
	// irModulePath is where a plugin author imports the IR from. It is a
	// constant here so that a change to it fails a test that names it rather
	// than quietly renaming the contract.
	irModulePath = "github.com/Zaba505/cpybkc/irpb"

	// protobufModulePath is the one dependency this module is allowed.
	protobufModulePath = "google.golang.org/protobuf"

	// cliModulePath is the module this one must never require. It is the
	// direction of the arrow, written down: the CLI depends on the IR.
	cliModulePath = "github.com/Zaba505/cpybkc"
)

// TestModuleDependsOnlyOnTheProtobufRuntime asserts the property the module
// boundary was drawn for.
//
// A plugin author importing the IR takes on this module's build list, and the
// promise doc.go makes is that the build list is the protobuf runtime and
// nothing else. Nothing in the Go toolchain enforces that promise — a stray
// import in a future file would add a require and no build would fail — so it is
// enforced here, over the one artifact that decides it.
//
// Reading go.mod as text rather than with golang.org/x/mod is not squeamishness
// about parsers. x/mod would itself be a second require in the file this test
// exists to keep at one, and a test dependency counts: `go get` on this module
// puts it in a plugin author's module graph exactly like any other.
func TestModuleDependsOnlyOnTheProtobufRuntime(t *testing.T) {
	mod := parseGoMod(t, "go.mod")

	if mod.module != irModulePath {
		t.Errorf("module path is %q, want %q: the import path in doc.go, ir.proto's go_package option and this module's own go.mod are one decision", mod.module, irModulePath)
	}

	if got := sortedKeys(mod.requires); len(got) != 1 || got[0] != protobufModulePath {
		t.Errorf("go.mod requires %v, want exactly [%s]: a second requirement here lands in the module graph of every generator plugin that imports the IR", got, protobufModulePath)
	}

	if _, ok := mod.requires[cliModulePath]; ok {
		t.Errorf("go.mod requires %s: the IR module must not depend on the CLI, or a plugin author importing twelve node kinds builds the whole generator", cliModulePath)
	}

	for _, r := range mod.replaces {
		t.Errorf("go.mod carries a replace directive (%q): a replace is ignored in a consumer's build, so a published module that needs one to resolve does not resolve for the people it was published for", r)
	}
}

// goMod is as much of a go.mod as asserting a dependency boundary needs: what
// the module is called, what it requires, and whether it points anywhere local.
type goMod struct {
	module    string
	goVersion string
	requires  map[string]string
	replaces  []string
}

// parseGoMod reads the directives above out of a go.mod.
//
// go.mod's grammar is line-oriented with one comment form, so this handles the
// whole of it that matters here: a directive on its own line, or a parenthesised
// block of arguments under one. Anything it does not recognise it ignores, which
// is the right failure mode for a test whose subject is the require list.
func parseGoMod(t *testing.T, path string) goMod {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	mod := goMod{requires: make(map[string]string)}

	block := ""
	for raw := range strings.SplitSeq(string(b), "\n") {
		line := raw
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if block != "" {
			if line == ")" {
				block = ""
				continue
			}

			mod.add(block, strings.Fields(line))

			continue
		}

		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[len(fields)-1] == "(" {
			block = fields[0]
			continue
		}

		mod.add(fields[0], fields[1:])
	}

	return mod
}

// add records one directive's arguments.
func (m *goMod) add(directive string, args []string) {
	switch directive {
	case "module":
		if len(args) > 0 {
			m.module = args[0]
		}
	case "go":
		if len(args) > 0 {
			m.goVersion = args[0]
		}
	case "require":
		if len(args) > 1 {
			m.requires[args[0]] = args[1]
		}
	case "replace":
		m.replaces = append(m.replaces, strings.Join(args, " "))
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}
