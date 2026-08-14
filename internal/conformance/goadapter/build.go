// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package goadapter

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/Zaba505/cpybkc/internal/plugin"
	"github.com/Zaba505/cpybkc/irpb"
)

const (
	// modulePath is this repository's module path, which is half of the import
	// path a codec program names the generated package by.
	//
	// It is written out rather than read from go.mod because the scratch tree is
	// under this package's own directory: the constant and the directory below
	// are one fact about where this file sits, and reading go.mod would answer
	// only half of it.
	modulePath = "github.com/Zaba505/cpybkc"

	// scratchDir is where a conversation builds, relative to the repository root
	// and in the slash-separated spelling.
	//
	// The leading underscore is load-bearing: the go tool ignores a directory
	// whose name begins with one when it expands ./..., so a scratch package
	// never reaches `go build ./...`, `go vet ./...` or the linter, while an
	// explicit path still builds it.
	scratchDir = "internal/conformance/goadapter/_scratch"

	// packageName is the Go package the generated code is emitted as, and the
	// name a codec program imports it under.
	packageName = "corpus"

	// packageOption is the option cpybkc-gen-go takes the package name as. It is
	// this package's to supply rather than a caller's: a codec program imports
	// the generated package, so a caller free to name it would be a caller able
	// to make that program not compile.
	packageOption = "package_name"

	// descriptorName is what the descriptor a codec program reads is written as.
	// It is the binary encoding, which is the form docs/plugin/SPEC.md calls
	// canonical and the form the frame carried it in.
	descriptorName = "ir.binpb"

	// inputName is what the bytes a decode frame carried are written as, inside
	// this adapter's own tree and never beside the entry they came from.
	inputName = "input.bin"
)

// The four trees a conversation's scratch directory holds, one entry to a
// directory in each.
//
// They are four rather than one directory per entry holding everything, because
// the go tool names a compiled program after the directory its package sits in:
// one directory of codec programs, each named for its entry, is what lets a
// single `go build` write every program into one place without two of them
// colliding.
const (
	generatedDir = "gen"
	programDir   = "codec"
	workDir      = "work"
	binDir       = "bin"
)

// built is one entry this conversation has code for: where its descriptor and
// its bytes are written, what the go tool calls its codec program, and where
// that program was built.
type built struct {
	name string

	descriptor string
	input      string

	pkg     string
	program string
}

// generate hands every entry's descriptor to the generator, writes a codec
// program beside each one, and compiles the lot in one invocation of the Go
// toolchain.
//
// All of them at once is what the operation is for: compiling is the most
// expensive thing in a Go run by a wide margin, and an adapter handed the corpus
// one entry at a time pays that cost once per entry.
//
// A failure here is per entry and the run continues: an entry whose descriptor
// the generator would not accept, or whose generated code would not compile,
// comes back ok: false with a diagnostic while this adapter stays alive and
// serves the rest. Only something that costs every entry — a scratch tree that
// cannot be made — refuses the operation itself.
func (c *conversation) generate(ctx context.Context, id int, req *request) *response {
	if len(req.Entries) == 0 {
		return refuse(id, "this generate names no entry, so there is nothing to generate code for")
	}

	if c.scratch != "" {
		// generate is sent exactly once, and this is one of the few ordering
		// preconditions worth checking rather than attempting: a second one
		// would have to decide what became of the first one's entries, and
		// rebuild is the operation for regenerating one — which this adapter
		// declares no capability for.
		return refuse(id, "this adapter serves generate once, and it has already served one")
	}

	if err := c.prepare(); err != nil {
		return refuse(id, "%v", err)
	}

	var (
		results   = make([]entryResult, len(req.Entries))
		compiling = make([]*built, 0, len(req.Entries))
		failed    = map[string]string{}
	)

	for i, asked := range req.Entries {
		results[i].Entry = asked.Entry

		entry, err := c.emit(ctx, i, asked)
		if err != nil {
			failed[asked.Entry] = err.Error()

			continue
		}

		compiling = append(compiling, entry)
	}

	for name, why := range c.compile(ctx, compiling) {
		failed[name] = why

		// An entry that would not compile has no code to read it with, so it is
		// not one this adapter will serve a decode for.
		delete(c.built, name)
	}

	// The results are filled in afterwards so that every entry the request
	// named gets exactly one, in the order it was asked about, whether it broke
	// at the generator, at the compiler or not at all.
	for i := range results {
		why, broke := failed[results[i].Entry]

		results[i].OK = !broke
		results[i].Error = why
	}

	return &response{ID: id, OK: true, Entries: results}
}

// prepare makes the tree this conversation generates and builds in, which is
// also what says a generate has been served.
func (c *conversation) prepare() error {
	parent := filepath.Join(c.root, filepath.FromSlash(scratchDir))
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("failed to make the scratch tree: %w", err)
	}

	// A directory inside the parent this adapter was given, and never one the
	// machine named: Root is required and refused when it is empty, so no run of
	// this reaches an ambient temporary directory (#184).
	scratch, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		return fmt.Errorf("failed to make the scratch tree: %w", err)
	}

	c.scratch = scratch
	c.built = map[string]*built{}

	return nil
}

// emit generates one entry's code and writes the codec program that drives it.
//
// The descriptor is unmarshalled and handed to
// [github.com/Zaba505/cpybkc/internal/plugin] rather than written out and named
// on a command line here, so that the corpus exercises the invocation cpybkc
// performs — the vector, the absolute paths, the descriptor written for one
// invocation and removed with it — instead of a second arrangement that happens
// to work.
func (c *conversation) emit(ctx context.Context, i int, asked requestEntry) (*built, error) {
	if asked.Entry == "" {
		return nil, fmt.Errorf("an entry of this generate carries no name, and a name is its identity")
	}

	if _, taken := c.built[asked.Entry]; taken {
		return nil, fmt.Errorf("this generate names %q twice", asked.Entry)
	}

	var descriptor irpb.Descriptor
	if err := proto.Unmarshal(asked.Descriptor, &descriptor); err != nil {
		return nil, fmt.Errorf("the descriptor cannot be read: %w", err)
	}

	// The entry's own name never becomes a path component. A name is the
	// engine's to choose and a directory is this adapter's to make, and the
	// index in front is what keeps two names that slug alike from sharing a
	// tree.
	held := fmt.Sprintf("%03d-%s", i, slug(asked.Entry))

	generated := filepath.Join(c.scratch, generatedDir, held)
	work := filepath.Join(c.scratch, workDir, held)

	for _, dir := range []string{generated, work} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("failed to make the directory the generator writes into: %w", err)
		}
	}

	invocation := plugin.Invocation{
		Name:    c.adapter.Name,
		Path:    c.adapter.Generator,
		Out:     generated,
		Options: append([]plugin.Option{{Key: packageOption, Value: packageName}}, c.adapter.Options...),
	}

	// A generator's own output is surfaced on standard error, where it is a
	// diagnostic rather than a corruption of the frames on standard output.
	runner := &plugin.Runner{Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}

	if err := runner.Run(ctx, &descriptor, []plugin.Invocation{invocation}); err != nil {
		return nil, err
	}

	entry := &built{
		name:       asked.Entry,
		descriptor: filepath.Join(work, descriptorName),
		input:      filepath.Join(work, inputName),
		program:    filepath.Join(c.scratch, binDir, held+exeSuffix()),
	}

	// The bytes the frame carried, written out unchanged: what a codec program
	// reads is what the generator was handed.
	if err := os.WriteFile(entry.descriptor, asked.Descriptor, 0o600); err != nil {
		return nil, fmt.Errorf("failed to write the descriptor: %w", err)
	}

	program := filepath.Join(c.scratch, programDir, held)

	if err := c.writeProgram(program, generated); err != nil {
		return nil, err
	}

	named, err := c.name(program)
	if err != nil {
		return nil, err
	}

	entry.pkg = named

	c.built[asked.Entry] = entry

	return entry, nil
}

// compile builds every codec program in one invocation, and says which entries
// would not build.
//
// One invocation because that is the whole reason generate carries the corpus
// rather than one entry: the Go toolchain compiles what the programs share once
// and links each of them against it. When it fails, they are built one at a time
// to find out which entries the failure belongs to — a per-entry diagnostic is
// what the contract asks for, and a combined build reports every package's
// errors together without saying which programs it managed.
func (c *conversation) compile(ctx context.Context, entries []*built) map[string]string {
	failed := map[string]string{}

	if len(entries) == 0 {
		return failed
	}

	bin := filepath.Join(c.scratch, binDir)
	if err := os.MkdirAll(bin, 0o755); err != nil {
		for _, entry := range entries {
			failed[entry.name] = fmt.Sprintf("failed to make the directory the codec programs are built into: %v", err)
		}

		return failed
	}

	packages := make([]string, 0, len(entries))
	for _, entry := range entries {
		packages = append(packages, entry.pkg)
	}

	if _, err := c.build(ctx, bin, packages...); err == nil {
		return failed
	}

	for _, entry := range entries {
		if said, err := c.build(ctx, bin, entry.pkg); err != nil {
			failed[entry.name] = fmt.Sprintf("the generated code did not compile: %v\n%s", err, said)
		}
	}

	return failed
}

// build runs the go tool over the packages named.
//
// From the repository root, so that it resolves the module there: the generated
// package imports what this module already requires, which is what lets a run
// need no network and no module of its own.
func (c *conversation) build(ctx context.Context, bin string, packages ...string) (string, error) {
	var said bytes.Buffer

	cmd := exec.CommandContext(ctx, "go", append([]string{"build", "-o", bin}, packages...)...)
	cmd.Dir = c.root
	cmd.Stdout = &said
	cmd.Stderr = &said

	err := cmd.Run()

	return said.String(), err
}

// name is a directory of the scratch tree as the go tool names a package in it:
// a path relative to the repository root, in the slash-separated spelling.
func (c *conversation) name(dir string) (string, error) {
	rel, err := filepath.Rel(c.root, dir)
	if err != nil {
		return "", fmt.Errorf("failed to name a package to the go tool: %w", err)
	}

	return "./" + filepath.ToSlash(rel), nil
}

// exeSuffix is what the go tool appends to the program it builds out of a
// package, which is what this adapter has to run afterwards. The go tool names
// it after the package's directory, and on Windows it adds this.
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}

	return ""
}

// slug is an entry's name as a directory of the scratch tree spells it: the
// characters a path and an import path both take, and a dash for everything
// else.
//
// It is not required to be unique or reversible. What names the directory is the
// index in front of it; this is there so that a build failure quotes a path
// somebody can connect to the entry it is about.
func slug(name string) string {
	var b strings.Builder

	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}

	return b.String()
}
