// These tests cover the vectors the curated functions hand the CLI, which are
// public API in the same way imageref's default reference is — below 1.0.0 any
// release may change them, and none should by accident: a caller who
// passes no manifest gets an invocation of no arguments, and what that means —
// the mounted project's own cpybkc.json, read from the working directory — is a
// promise docs/cli/SPEC.md makes and this module relies on rather than restates.
// A scaffolding run's vector carries three more of them: that the subcommand
// leads, that the copybooks reach cpybkc in the order they were given, which
// is the order the scaffold then holds their records in, and that --out names
// the layout by the name the caller chose, in the directory the module hands
// back with the definitions files beside it. Pinning the vectors here
// turns a later accidental flag, or a quietly reordered list, into a red build.
// An emitting run's vector carries one more: that a caller who names no format
// gets no --emit-ir-format at all, so the encoding a descriptor arrives in is
// the CLI's default rather than a spelling this module would have to keep in
// step with it.
//
// What is not tested here is anything needing an engine: whether the exec
// succeeds, whether the project mounted at the working directory has a manifest
// in it, or what the CLI does with one. Those are the CLI's own tests and, for
// this module driven over a real image, #64's.

package argv

import (
	"slices"
	"strings"
	"testing"
)

func TestGenerateWithNoManifest(t *testing.T) {
	got, err := Generate("")
	if err != nil {
		t.Fatalf("Generate(\"\"): unexpected error: %v", err)
	}
	// No arguments at all, and not a --manifest naming the default. Spelling the
	// default here would be this module's second statement of where cpybkc looks
	// for a manifest, and the two would drift the moment that document changed.
	if len(got) != 0 {
		t.Errorf("Generate(\"\") = %q, want no arguments", got)
	}
}

func TestGenerateWithAManifest(t *testing.T) {
	for _, tc := range []struct {
		name     string
		manifest string
		want     []string
	}{
		{
			// The ordinary case: a project keeping its manifest below its root.
			name:     "a path below the project root",
			manifest: "build/cpybkc.json",
			want:     []string{"--manifest", "build/cpybkc.json"},
		},
		{
			name:     "a manifest named something else",
			manifest: "orders.json",
			want:     []string{"--manifest", "orders.json"},
		},
		{
			// Separated rather than joined, so a value carrying an equals sign
			// needs no thought about which one cpybkc cuts on.
			name:     "a path carrying an equals sign",
			manifest: "odd=name/cpybkc.json",
			want:     []string{"--manifest", "odd=name/cpybkc.json"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Generate(tc.manifest)
			if err != nil {
				t.Fatalf("Generate(%q): unexpected error: %v", tc.manifest, err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("Generate(%q) = %q, want %q", tc.manifest, got, tc.want)
			}
		})
	}
}

func TestInitAssemblesTheVector(t *testing.T) {
	for _, tc := range []struct {
		name        string
		copybooks   []string
		copyMembers []string
		dir         string
		layout      string
		want        []string
	}{
		{
			// The subcommand leads, because docs/cli/SPEC.md reads a subcommand
			// name at the first argument and nowhere else; --out names the
			// layout, in the directory the module hands back.
			name:      "one copybook",
			copybooks: []string{"posting.cpy"},
			dir:       "/scaffold",
			layout:    "ledger.sexpr",
			want:      []string{"init", "--copybook", "posting.cpy", "--out", "/scaffold/ledger.sexpr"},
		},
		{
			// The order is pinned rather than incidental: the scaffold holds one
			// record per 01-level in the order the copybooks were read, so a
			// vector that reordered them would reorder somebody's layout.
			name:      "three copybooks, in the order they were given",
			copybooks: []string{"header.cpy", "posting.cpy", "trailer.cpy"},
			dir:       "/scaffold",
			layout:    "layout.sexpr",
			want: []string{
				"init",
				"--copybook", "header.cpy",
				"--copybook", "posting.cpy",
				"--copybook", "trailer.cpy",
				"--out", "/scaffold/layout.sexpr",
			},
		},
		{
			// Each member is a flag of its own, after the copybooks, passed
			// through exactly as given: splitting one at its = and reading the
			// member as a COPY writes it are the CLI's, and a value with a space
			// in it is one argument here however a shell would have split it.
			name:        "members a COPY names",
			copybooks:   []string{"cpy/order-detail.cpy"},
			copyMembers: []string{"STDHDR=cpy/stdhdr.cpy", "STDTRL OF GLLIB=cpy/gllib/stdtrl.cpy"},
			dir:         "/scaffold",
			layout:      "orders.sexpr",
			want: []string{
				"init",
				"--copybook", "cpy/order-detail.cpy",
				"--copy-member", "STDHDR=cpy/stdhdr.cpy",
				"--copy-member", "STDTRL OF GLLIB=cpy/gllib/stdtrl.cpy",
				"--out", "/scaffold/orders.sexpr",
			},
		},
		{
			// A path below the project root, resolved by the CLI against the
			// mounted project because that is its working directory, and named
			// as it was typed in the definitions file converted from it.
			name:      "a path below the project root",
			copybooks: []string{"copybooks/posting.cpy"},
			dir:       "/scaffold/",
			layout:    "layout.sexpr",
			want: []string{
				"init", "--copybook", "copybooks/posting.cpy", "--out", "/scaffold/layout.sexpr",
			},
		},
		{
			// Separated rather than joined, so a value carrying an equals sign
			// needs no thought about which one cpybkc cuts on.
			name:      "a path carrying an equals sign",
			copybooks: []string{"odd=name/posting.cpy"},
			dir:       "/scaffold",
			layout:    "odd=name.sexpr",
			want: []string{
				"init", "--copybook", "odd=name/posting.cpy", "--out", "/scaffold/odd=name.sexpr",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Init(tc.copybooks, tc.copyMembers, tc.dir, tc.layout)
			if err != nil {
				t.Fatalf("Init(%q, %q, %q): unexpected error: %v", tc.copybooks, tc.dir, tc.layout, err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("Init(%q, %q, %q) = %q, want %q", tc.copybooks, tc.dir, tc.layout, got, tc.want)
			}
		})
	}
}

func TestInitRefusesNoCopybooksAtAll(t *testing.T) {
	for _, tc := range []struct {
		name      string
		copybooks []string
	}{
		{name: "nil", copybooks: nil},
		{name: "empty", copybooks: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Init(tc.copybooks, nil, "/scaffold", "layout.sexpr")
			if err == nil {
				t.Fatalf("Init(%q, …) = %q, want an error", tc.copybooks, got)
			}
			if got != nil {
				t.Errorf("Init(%q, …) returned %q beside its error, want no vector", tc.copybooks, got)
			}
			// Checked for saying what a scaffold is derived from rather than
			// merely for being non-nil: the caller has to learn that the
			// copybooks are the input and not an optional narrowing of one.
			if !strings.Contains(err.Error(), "01-levels") {
				t.Errorf("Init(%q, …) error = %q, want it to say what a scaffold is derived from", tc.copybooks, err)
			}
		})
	}
}

func TestInitRefusesTheDashAmongTheCopybooks(t *testing.T) {
	for _, tc := range []struct {
		name      string
		copybooks []string
	}{
		{name: "alone", copybooks: []string{"-"}},
		{name: "beside real paths", copybooks: []string{"header.cpy", "-", "trailer.cpy"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Init(tc.copybooks, nil, "/scaffold", "layout.sexpr")
			if err == nil {
				t.Fatalf("Init(%q, …) = %q, want an error", tc.copybooks, got)
			}
			if got != nil {
				t.Errorf("Init(%q, …) returned %q beside its error, want no vector", tc.copybooks, got)
			}
			// The reason, for [TestGenerateRefusesTheDash]'s reason: a caller who
			// reached for the dash was thinking of a stream, and why a copybook
			// cannot arrive on one is the whole of what the message has to teach.
			if !strings.Contains(err.Error(), "no name to name one after") {
				t.Errorf("Init(%q, …) error = %q, want it to say why a copybook cannot arrive on a stream",
					tc.copybooks, err)
			}
		})
	}
}

// TestInitTakesTheLayoutsNameAndNothingElse pins the choice #408 made for
// Init's destination: the caller names the layout, as `--out` does on the
// command line, and the module chooses the directory — the one it hands back,
// holding the layout and its definitions files and nothing else. So a name is a
// bare file name, and a stream or a path is refused before the vector leaves.
func TestInitTakesTheLayoutsNameAndNothingElse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		layout string
		says   string
	}{
		{name: "a stream", layout: "-", says: "a definitions file per copybook beside it"},
		{name: "no name", layout: "", says: "not a file name"},
		{name: "a path", layout: "layouts/ledger.sexpr", says: "not a file name"},
		{name: "an absolute path", layout: "/tmp/ledger.sexpr", says: "not a file name"},
		{name: "the directory itself", layout: ".", says: "not a file name"},
		{name: "the directory above", layout: "..", says: "not a file name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Init([]string{"posting.cpy"}, nil, "/scaffold", tc.layout)
			if err == nil {
				t.Fatalf("Init(…, %q) = %q, want an error", tc.layout, got)
			}
			if got != nil {
				t.Errorf("Init(…, %q) returned %q beside its error, want no vector", tc.layout, got)
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("Init(…, %q) error = %q, want it to say %q", tc.layout, err, tc.says)
			}
		})
	}
}

func TestEmitIRAssemblesTheVector(t *testing.T) {
	const dest = "/ir/descriptor"

	for _, tc := range []struct {
		name     string
		manifest string
		format   string
		want     []string
	}{
		{
			// The ordinary case, and the one a bug report is built out of: the
			// project's own cpybkc.json, the CLI's own default encoding, and one
			// flag saying the run emits rather than generates.
			name: "no manifest and no format",
			want: []string{"--emit-ir", dest},
		},
		{
			// No --emit-ir-format naming the default, for the reason
			// [TestGenerateWithNoManifest] gives about --manifest: which encoding
			// a run writes when nobody says is docs/cli/SPEC.md's, and a second
			// statement of it here would drift.
			name:   "the format named by name",
			format: "binary",
			want:   []string{"--emit-ir", dest, "--emit-ir-format", "binary"},
		},
		{
			name:   "the JSON a person pastes into an issue",
			format: "json",
			want:   []string{"--emit-ir", dest, "--emit-ir-format", "json"},
		},
		{
			// Passed through rather than refused, so the diagnostic is the CLI's:
			// it names the spellings there are, from the parser that decides them.
			name:   "a format this package does not recognise",
			format: "xml",
			want:   []string{"--emit-ir", dest, "--emit-ir-format", "xml"},
		},
		{
			// Which descriptor is emitted follows from which manifest was read,
			// so a project keeping its manifest below its root can still emit one.
			name:     "a manifest below the project root",
			manifest: "build/cpybkc.json",
			want:     []string{"--manifest", "build/cpybkc.json", "--emit-ir", dest},
		},
		{
			// The manifest leads, exactly as it does for a generating run: this is
			// that vector plus the flag that replaces the generating.
			name:     "a manifest and a format together",
			manifest: "build/cpybkc.json",
			format:   "json",
			want: []string{
				"--manifest", "build/cpybkc.json",
				"--emit-ir", dest,
				"--emit-ir-format", "json",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EmitIR(tc.manifest, dest, tc.format)
			if err != nil {
				t.Fatalf("EmitIR(%q, %q, %q): unexpected error: %v", tc.manifest, dest, tc.format, err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("EmitIR(%q, %q, %q) = %q, want %q", tc.manifest, dest, tc.format, got, tc.want)
			}
		})
	}
}

func TestEmitIRRefusesTheDashAsAManifest(t *testing.T) {
	// Every format, because the refusal is about the manifest and a vector that
	// escaped it for one spelling of an unrelated argument would be exactly the
	// kind of hole a table stops.
	for _, format := range []string{"", "binary", "json"} {
		t.Run("format "+format, func(t *testing.T) {
			got, err := EmitIR("-", "/ir/descriptor", format)
			if err == nil {
				t.Fatalf("EmitIR(\"-\", …, %q) = %q, want an error", format, got)
			}
			if got != nil {
				t.Errorf("EmitIR(\"-\", …, %q) returned %q beside its error, want no vector", format, got)
			}
			// The same reason [TestGenerateRefusesTheDash] checks for, because it
			// is the same rule read once rather than a second copy of it here.
			if !strings.Contains(err.Error(), "in no directory") {
				t.Errorf("EmitIR(\"-\", …, %q) error = %q, want it to say why a manifest cannot arrive on a stream",
					format, err)
			}
		})
	}
}

func TestGenerateRefusesTheDash(t *testing.T) {
	got, err := Generate("-")
	if err == nil {
		t.Fatalf("Generate(\"-\") = %q, want an error", got)
	}
	if got != nil {
		t.Errorf("Generate(\"-\") returned %q beside its error, want no vector", got)
	}
	// Checked for saying why rather than merely for being non-nil: a caller who
	// reached for the dash was thinking of a stream, and the reason they cannot
	// have one is the whole of what the message has to teach.
	if !strings.Contains(err.Error(), "in no directory") {
		t.Errorf("Generate(\"-\") error = %q, want it to say why a manifest cannot arrive on a stream", err)
	}
}
