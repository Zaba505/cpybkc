// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package gorunner

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Zaba505/cpybkc/internal/conformance"
	"github.com/Zaba505/cpybkc/internal/emit"
	"github.com/Zaba505/cpybkc/internal/plugin"
	"github.com/Zaba505/cpybkc/irpb"
)

const (
	// modulePath is this repository's module path, which is half of the import
	// path the driver names the generated package by.
	//
	// It is written out rather than read from go.mod because the scratch tree is
	// under this package's own directory: the constant and the directory below
	// are one fact about where this file sits, and reading go.mod would answer
	// only half of it.
	modulePath = "github.com/Zaba505/cpybkc"

	// scratchDir is where a run builds, relative to the repository root and in
	// the slash-separated spelling.
	//
	// The leading underscore is load-bearing: the go tool ignores a directory
	// whose name begins with one when it expands ./..., so a scratch package
	// never reaches `go build ./...`, `go vet ./...` or the linter, while
	// `go run ./internal/.../_scratch/x/driver` still builds it.
	scratchDir = "internal/conformance/gorunner/_scratch"

	// packageName is the Go package the generated code is emitted as, and the
	// name the driver imports it under.
	packageName = "corpus"

	// packageOption is the option cpybkc-gen-go takes the package name as. It is
	// this package's to supply rather than a caller's: the driver imports the
	// generated package, so a caller free to name it would be a caller able to
	// make the driver not compile.
	packageOption = "package_name"

	// descriptorName is what the descriptor the driver reads is written as,
	// inside the scratch tree. It is the binary encoding, which is the form
	// docs/plugin/SPEC.md calls canonical and the form a generator is handed.
	descriptorName = "ir.binpb"

	// recordsName is the file cpybkc-gen-go emits the record structs into.
	recordsName = "records.go"
)

// Runner runs corpus entries through one Go generator.
//
// The generator is a path rather than a name on PATH, because a run is nearly
// always against a generator just built from the tree under test, and resolving
// a name would find whichever one an author happened to have installed.
type Runner struct {
	// Root is the repository root: where the scratch tree is made and where the
	// go tool is run.
	Root string

	// Name is the generator's name, as a diagnostic about the invocation should
	// spell it — the `<name>` of a cpybkc-gen-<name> executable.
	Name string

	// Generator is the executable to run.
	Generator string

	// Options are the options to pass beyond the package name, in the order
	// they are to be passed.
	Options []plugin.Option
}

// Run generates code for the entry, compiles it, reads the entry's bytes with
// it, and writes the records it read back out with it.
//
// What comes back is what the generated code made of the entry in both
// directions, in the corpus's own value language, which is what
// [github.com/Zaba505/cpybkc/internal/conformance.CompareAnswer] holds against
// the entry. A file the generated reader refused is not an error here, and
// neither is a record the generated writer refused: each is a
// [github.com/Zaba505/cpybkc/internal/conformance.Values] carrying a failure,
// because an entry is allowed to expect one and only the comparison knows
// whether this entry did.
//
// An error is this harness failing rather than the generator disagreeing: the
// generator exited non-zero, its output did not compile, or the driver could not
// be run. Those are not conformance failures and are deliberately not reported
// as one.
func (r *Runner) Run(ctx context.Context, entry *conformance.Entry) (*conformance.Answer, error) {
	if err := r.check(entry); err != nil {
		return nil, err
	}

	scratch, err := r.scratch(entry)
	if err != nil {
		return nil, err
	}

	defer func() {
		// The removal is not reported. It happens after the run, so there is
		// nothing left to fail, and a run that answered and could not tidy up
		// afterwards has not failed at what it was asked.
		_ = os.RemoveAll(scratch)
	}()

	generated := filepath.Join(scratch, packageName)
	if err := os.MkdirAll(generated, 0o755); err != nil {
		return nil, fmt.Errorf("failed to make the directory the generator writes into: %w", err)
	}

	if err := r.generate(ctx, entry, generated); err != nil {
		return nil, err
	}

	descriptor, err := emit.Marshal(entry.Descriptor)
	if err != nil {
		return nil, err
	}

	descriptorPath := filepath.Join(scratch, descriptorName)
	if err := os.WriteFile(descriptorPath, descriptor, 0o600); err != nil {
		return nil, fmt.Errorf("failed to write the descriptor the driver reads: %w", err)
	}

	if err := r.writeDriver(scratch, generated, entry.Descriptor); err != nil {
		return nil, err
	}

	return r.drive(ctx, scratch, descriptorPath, filepath.Join(entry.Dir, conformance.InputName))
}

// check holds a run to what it needs before anything is created, so that a
// missing field is reported as itself rather than as a compilation that failed
// for no visible reason.
func (r *Runner) check(entry *conformance.Entry) error {
	switch {
	case entry == nil:
		return fmt.Errorf("there is no entry to run")
	case r.Root == "":
		return fmt.Errorf("conformance entry %s: Root is where the scratch tree is made, and is required", entry.Name)
	case r.Generator == "":
		return fmt.Errorf("conformance entry %s: Generator is the executable to run, and is required", entry.Name)
	default:
		return nil
	}
}

// scratch makes the directory this run builds in.
func (r *Runner) scratch(entry *conformance.Entry) (string, error) {
	parent := filepath.Join(r.Root, filepath.FromSlash(scratchDir))
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("failed to make the scratch tree: %w", err)
	}

	dir, err := os.MkdirTemp(parent, entry.Name+"-")
	if err != nil {
		return "", fmt.Errorf("failed to make the scratch tree: %w", err)
	}

	return dir, nil
}

// generate invokes the generator on the entry's descriptor.
//
// It goes through [github.com/Zaba505/cpybkc/internal/plugin] rather than
// starting the process here, so that the corpus exercises the invocation cpybkc
// performs — the vector, the absolute paths, the descriptor written for one
// invocation and removed with it — instead of a second arrangement that happens
// to work.
func (r *Runner) generate(ctx context.Context, entry *conformance.Entry, out string) error {
	runner := &plugin.Runner{}

	options := append([]plugin.Option{{Key: packageOption, Value: packageName}}, r.Options...)

	invocation := plugin.Invocation{
		Name:    r.Name,
		Path:    r.Generator,
		Out:     out,
		Options: options,
	}

	if err := runner.Run(ctx, entry.Descriptor, []plugin.Invocation{invocation}); err != nil {
		return fmt.Errorf("conformance entry %s: %w", entry.Name, err)
	}

	return nil
}

// drive builds and runs the driver, and reads the answer it wrote.
//
// The go tool is run from the repository root so that it resolves the module
// there: the generated package imports what this module already requires, which
// is what lets a run need no network and no module of its own.
func (r *Runner) drive(ctx context.Context, scratch, descriptor, input string) (*conformance.Answer, error) {
	rel, err := filepath.Rel(r.Root, filepath.Join(scratch, driverDir))
	if err != nil {
		return nil, fmt.Errorf("failed to name the driver to the go tool: %w", err)
	}

	var out, errs bytes.Buffer

	cmd := exec.CommandContext(ctx, "go", "run", "./"+filepath.ToSlash(rel), descriptor, input)
	cmd.Dir = r.Root
	cmd.Stdout = &out
	cmd.Stderr = &errs

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("the generated code did not compile or did not run: %w\n%s", err, errs.String())
	}

	answer, err := conformance.ParseAnswer(out.Bytes())
	if err != nil {
		return nil, fmt.Errorf("the driver wrote an answer this harness cannot read: %w", err)
	}

	return answer, nil
}

// recordTypes pairs each record node of the descriptor with the Go type the
// generator emitted for it.
//
// The pairing is by position, which rests on the one promise cmd/cpybkc-gen-go's
// README makes about its output: one exported struct per record, in the order
// the descriptor's node list carries them. Reading the types out of the source
// rather than munging the copybook's names here is what keeps the harness from
// carrying its own copy of the generator's identifier rule.
func recordTypes(generated string, descriptor *irpb.Descriptor) (map[string]uint64, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, filepath.Join(generated, recordsName), nil, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to read the generated record structs: %w", err)
	}

	var types []string

	for _, decl := range file.Decls {
		declaration, ok := decl.(*ast.GenDecl)
		if !ok || declaration.Tok != token.TYPE {
			continue
		}

		for _, spec := range declaration.Specs {
			declared, ok := spec.(*ast.TypeSpec)
			if !ok || !declared.Name.IsExported() {
				continue
			}

			if _, ok := declared.Type.(*ast.StructType); ok {
				types = append(types, declared.Name.Name)
			}
		}
	}

	var records []uint64

	for _, node := range descriptor.GetNodes() {
		if node.GetRecord() != nil {
			records = append(records, node.GetId())
		}
	}

	if len(types) != len(records) {
		return nil, fmt.Errorf("the descriptor carries %d record types and %s declares %d structs",
			len(records), recordsName, len(types))
	}

	paired := make(map[string]uint64, len(types))

	for i, name := range types {
		paired[name] = records[i]
	}

	return paired, nil
}
