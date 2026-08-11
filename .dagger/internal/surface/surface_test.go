// These tests are the whole reason this package exists apart from the pipeline
// that uses it. What is built on it is CliSurface, a drift guard, and a drift
// guard is the one kind of check whose failure mode is staying green — so every
// shape it claims to see, and every shape it claims not to, is a row here rather
// than a sentence in a doc comment.
//
// Each escape below was a real hole in the first version of the guard, found in
// review: a flag written with one hyphen, a constant declared inside a function
// body, a constant built by concatenating another, and a diagnostic hoisted to a
// constant being read as a flag.

package surface

import (
	"slices"
	"strings"
	"testing"
)

func TestFlagsFindsEveryShapeOfConstant(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		want   []string
	}{
		{
			name: "a package-level block, the ordinary case",
			source: `package main
const (
	manifestFlag = "--manifest"
	versionFlag  = "--version"
)`,
			want: []string{"--manifest", "--version"},
		},
		{
			// A short flag that is not a synonym of a long one. A prefix test for
			// "--" would let this reach the default branch with the guard green.
			name: "a single-hyphen flag",
			source: `package main
const outFlag = "-o"`,
			want: []string{"-o"},
		},
		{
			// Hoisting a constant to package scope is a style choice, not a
			// statement about whether the flag is real.
			name: "a constant declared inside a function body",
			source: `package main
func parse(args []string) error {
	const jobsFlag = "--jobs"
	_ = jobsFlag
	return nil
}`,
			want: []string{"--jobs"},
		},
		{
			// The natural way to write a family: --emit-ir and --emit-ir-format.
			name: "a constant built from another",
			source: `package main
const (
	emitIRFlag       = "--emit-ir"
	emitIRFormatFlag = emitIRFlag + "-format"
)`,
			want: []string{"--emit-ir", "--emit-ir-format"},
		},
		{
			// Declaration order is not evaluation order, so the resolution has to
			// reach a fixed point rather than make one pass.
			name: "a constant built from one declared later",
			source: `package main
const derived = base + "-format"
const base = "--emit-ir"`,
			want: []string{"--emit-ir", "--emit-ir-format"},
		},
		{
			name: "a parenthesised concatenation",
			source: `package main
const (
	prefix = "--"
	flag   = prefix + ("emit" + "-ir")
)`,
			want: []string{"--emit-ir"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flags, unresolved, err := Flags(map[string]string{"args.go": tc.source})
			if err != nil {
				t.Fatalf("Flags: unexpected error: %v", err)
			}
			if len(unresolved) != 0 {
				t.Errorf("Flags reported %q unresolved, want nothing unresolved", unresolved)
			}
			if !slices.Equal(flags, tc.want) {
				t.Errorf("Flags = %q, want %q", flags, tc.want)
			}
		})
	}
}

func TestFlagsRefusesWhatIsNotAFlag(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
	}{
		{
			// POSIX's end of options. cpybkc honours it and it takes no value.
			name: "the end-of-options delimiter",
			source: `package main
const endOfOptions = "--"`,
		},
		{
			// POSIX's standard-stream operand, which cpybkc reads as a destination
			// rather than as a flag.
			name: "a lone dash",
			source: `package main
const standardOutput = "-"`,
		},
		{
			// The false positive that would produce a red build whose suggested
			// remedy — "record this flag" — is not a thing anyone should do.
			name: "a diagnostic that quotes a flag",
			source: `package main
const complaint = "--emit-ir-format may only be given beside --emit-ir"`,
		},
		{
			name: "a whole usage block",
			source: `package main
const usage = ` + "`" + `Usage:
  cpybkc [--manifest <path>]
` + "`",
		},
		{
			name: "an ordinary path",
			source: `package main
const defaultManifest = "cpybkc.json"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flags, unresolved, err := Flags(map[string]string{"args.go": tc.source})
			if err != nil {
				t.Fatalf("Flags: unexpected error: %v", err)
			}
			if len(unresolved) != 0 {
				t.Errorf("Flags reported %q unresolved, want nothing unresolved", unresolved)
			}
			if len(flags) != 0 {
				t.Errorf("Flags = %q, want nothing", flags)
			}
		})
	}
}

func TestFlagsNamesWhatItCouldNotRead(t *testing.T) {
	// A constant whose value has a string in it and cannot be evaluated is the
	// one case where "not a flag" and "I could not tell" differ, and reporting it
	// is what stops the second being read as the first.
	source := `package main
import "strings"
const prefix = "--"
var flags = []string{prefix}
const derived = elsewhere + "-ir"
const shouted = "--emit" + strings.ToUpper("x")`

	flags, unresolved, err := Flags(map[string]string{"args.go": source})
	if err != nil {
		t.Fatalf("Flags: unexpected error: %v", err)
	}
	if len(flags) != 0 {
		t.Errorf("Flags = %q, want nothing readable", flags)
	}
	if !slices.Equal(unresolved, []string{"derived", "shouted"}) {
		t.Errorf("Flags reported %q unresolved, want [derived shouted]", unresolved)
	}
}

func TestFlagsIgnoresArithmetic(t *testing.T) {
	// Nothing here is a candidate, so nothing here is a complaint either: an
	// unresolved integer constant reported as unreadable would make the guard
	// noisy about code that was never about flags.
	source := `package main
const width = columns + 2
const answer = 40 + 2`

	flags, unresolved, err := Flags(map[string]string{"args.go": source})
	if err != nil {
		t.Fatalf("Flags: unexpected error: %v", err)
	}
	if len(flags) != 0 || len(unresolved) != 0 {
		t.Errorf("Flags = %q, unresolved %q, want both empty", flags, unresolved)
	}
}

func TestFlagsAcrossFiles(t *testing.T) {
	// The guard reads a package, not a file: a flag introduced in a new file
	// beside the parser, or in a subpackage the caller globbed in, has to count.
	files := map[string]string{
		"args.go": `package main
const emitIRFlag = "--emit-ir"`,
		"internal/flags/flags.go": `package flags
const EmitIRFormatFlag = "--emit-ir" + "-format"`,
	}

	flags, _, err := Flags(files)
	if err != nil {
		t.Fatalf("Flags: unexpected error: %v", err)
	}
	if !slices.Equal(flags, []string{"--emit-ir", "--emit-ir-format"}) {
		t.Errorf("Flags = %q, want [--emit-ir --emit-ir-format]", flags)
	}
}

func TestFlagsReportsAParseError(t *testing.T) {
	_, _, err := Flags(map[string]string{"broken.go": "package main\nconst = "})
	if err == nil {
		t.Fatal("Flags over unparseable source: want an error")
	}
	// Named, because a guard that could not read a file has to say which one.
	if !strings.Contains(err.Error(), "broken.go") {
		t.Errorf("Flags error = %q, want it to name the file", err)
	}
}

func TestFunctions(t *testing.T) {
	files := map[string]string{
		"main.go": `package main
type Cpybkc struct{}
func (m *Cpybkc) Generate() {}
func (m Cpybkc) Image() {}
func (m *Cpybkc) project() {}
func (m *Other) Generate() {}
func New() *Cpybkc { return nil }`,
	}

	got, err := Functions(files, "Cpybkc")
	if err != nil {
		t.Fatalf("Functions: unexpected error: %v", err)
	}
	// Exported methods on the type, by pointer or by value; not its unexported
	// ones, not another type's, and not a plain function.
	if !slices.Equal(got, []string{"Generate", "Image"}) {
		t.Errorf("Functions = %q, want [Generate Image]", got)
	}
}
