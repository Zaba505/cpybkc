// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package plugin

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The tests below build a PATH out of real directories and real files rather
// than out of a filesystem abstraction, because what is under test is a
// question about a mode bit and a directory entry — an abstraction would let
// this package agree with a fake about what an execute bit is. They are POSIX
// tests for a POSIX contract; docs/plugin/SPEC.md, "Host platform", is where
// that is decided.

// pathOf spells a PATH the way the host does, so that a test states the
// directories and not the separator between them.
func pathOf(dirs ...string) string { return strings.Join(dirs, string(os.PathListSeparator)) }

// executable writes a file that is a candidate: a regular file with an execute
// bit. It is a shell script because docs/plugin/SPEC.md says one is a
// first-class plugin, and a test whose fixture was a compiled binary would be
// testing something the contract does not require.
func executable(t *testing.T, dir, name string) string {
	t.Helper()

	path := filepath.Join(dir, name)

	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	return path
}

// plain writes a file of the right name with no execute bit — the generator
// somebody built and forgot to chmod.
func plain(t *testing.T, dir, name string) string {
	t.Helper()

	path := filepath.Join(dir, name)

	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	return path
}

func TestFilenameIsTheNameUnderThePrefix(t *testing.T) {
	if got, want := Filename("go"), "cpybkc-gen-go"; got != want {
		t.Errorf("a generator named go is %q, want %q", got, want)
	}

	if got, want := Filename(""), Prefix; got != want {
		t.Errorf("the empty name is %q, want %q", got, want)
	}
}

// TestResolveFindsTheExecutableTheNameSpells is the whole of discovery: a name
// and a PATH give the file whose name is the name under the prefix.
func TestResolveFindsTheExecutableTheNameSpells(t *testing.T) {
	dir := t.TempDir()
	want := executable(t, dir, "cpybkc-gen-go")

	// A neighbour of the right shape and the wrong name, so that a search that
	// took whatever it found first would be caught rather than passed.
	executable(t, dir, "cpybkc-gen-json-schema")

	got, err := Resolve("go", pathOf(dir))
	if err != nil {
		t.Fatalf("resolving go on %s: %v", dir, err)
	}

	if got != want {
		t.Errorf("go resolved to %s, want %s", got, want)
	}

	// Determinism, in the sense a caller depends on: one PATH and one name give
	// one answer, and the answer moves when the filesystem does rather than
	// between two calls.
	if again, err := Resolve("go", pathOf(dir)); err != nil || again != got {
		t.Errorf("resolving go again gave %s, %v; want %s and no error", again, err, got)
	}
}

// TestTheEarliestMatchOnPATHWins pins the rule docs/plugin/SPEC.md states as
// "in order, and the earliest match wins", from the gesture it argues from: a
// plugin under development shadows an installed one by being in a directory
// earlier on PATH, and the same two directories in the other order resolve to
// the other file.
func TestTheEarliestMatchOnPATHWins(t *testing.T) {
	installed := t.TempDir()
	development := t.TempDir()

	installedPlugin := executable(t, installed, "cpybkc-gen-go")
	developmentPlugin := executable(t, development, "cpybkc-gen-go")

	got, err := Resolve("go", pathOf(development, installed))
	if err != nil {
		t.Fatalf("resolving go with the development directory first: %v", err)
	}

	if got != developmentPlugin {
		t.Errorf("go resolved to %s, want the shadowing %s", got, developmentPlugin)
	}

	got, err = Resolve("go", pathOf(installed, development))
	if err != nil {
		t.Fatalf("resolving go with the installed directory first: %v", err)
	}

	if got != installedPlugin {
		t.Errorf("go resolved to %s, want the installed %s", got, installedPlugin)
	}
}

// TestAPathElementIsSearchedWhereItSaysAndSpelledAsItSays covers the relative
// element, which is the working directory when and only when PATH writes it
// out. The answer is spelled the way PATH spelled the directory, because making
// it absolute would be this package deciding a relative element meant somewhere
// else than it said.
func TestAPathElementIsSearchedWhereItSaysAndSpelledAsItSays(t *testing.T) {
	dir := t.TempDir()
	executable(t, dir, "cpybkc-gen-go")

	t.Chdir(dir)

	got, err := Resolve("go", pathOf("."))
	if err != nil {
		t.Fatalf("resolving go on a PATH of .: %v", err)
	}

	if want := "cpybkc-gen-go"; got != want {
		t.Errorf("go resolved to %s, want the relative %s", got, want)
	}
}

// TestAnEmptyPathElementIsNotTheWorkingDirectory is the one place this search
// departs from POSIX and from [os/exec.LookPath], and it departs on purpose:
// docs/plugin/SPEC.md forbids it, because a generator picked up from whatever
// directory a user happened to be standing in is an execution surface nobody
// chose.
func TestAnEmptyPathElementIsNotTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	executable(t, dir, "cpybkc-gen-go")

	t.Chdir(dir)

	for _, searchPath := range []string{"", pathOf("", ""), pathOf("", t.TempDir())} {
		got, err := Resolve("go", searchPath)
		if got != "" {
			t.Errorf("go resolved to %s on a PATH of %q, want nothing", got, searchPath)
		}

		var notFound *NotFoundError
		if !errors.As(err, &notFound) {
			t.Fatalf("resolving go on a PATH of %q gave %v, want a NotFoundError", searchPath, err)
		}

		// An empty element names no directory, so it is not reported as one
		// that was searched either.
		for _, searched := range notFound.Searched {
			if searched == "" || searched == "." {
				t.Errorf("the search on %q reports looking in %q", searchPath, searched)
			}
		}
	}
}

// TestOnlyARegularFileWithAnExecuteBitIsACandidate walks past the two files an
// adopter actually produces — a directory of that name, and a script nobody
// chmod'd — and takes the one further along PATH, which is what makes the rule
// a search rule rather than a check on the first hit.
func TestOnlyARegularFileWithAnExecuteBitIsACandidate(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	third := t.TempDir()

	directory := filepath.Join(first, "cpybkc-gen-go")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatalf("making %s a directory: %v", directory, err)
	}

	unchmodded := plain(t, second, "cpybkc-gen-go")
	want := executable(t, third, "cpybkc-gen-go")

	got, err := Resolve("go", pathOf(first, second, third))
	if err != nil {
		t.Fatalf("resolving go past two files of the same name: %v", err)
	}

	if got != want {
		t.Errorf("go resolved to %s, want %s", got, want)
	}

	// The same two files with nothing behind them are a fault that names both,
	// because "it is there and cpybkc would not take it" is not a problem an
	// adopter solves by rereading their PATH.
	_, err = Resolve("go", pathOf(first, second))

	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("resolving go past both gave %v, want a NotFoundError", err)
	}

	wantPassed := []PassedOver{
		{Path: directory, Fault: "it is a directory"},
		{Path: unchmodded, Fault: "it carries no execute bit"},
	}

	if len(notFound.PassedOver) != len(wantPassed) {
		t.Fatalf("the fault passed over %v, want %v", notFound.PassedOver, wantPassed)
	}

	for i, passed := range notFound.PassedOver {
		if passed != wantPassed[i] {
			t.Errorf("the fault passed over %v, want %v", passed, wantPassed[i])
		}
	}
}

// TestASymlinkToAnExecutableResolves is how a plugin installed by a package
// manager usually appears, and the mode test has to see the file at the end of
// the chain because that is the file that would be executed.
func TestASymlinkToAnExecutableResolves(t *testing.T) {
	store := t.TempDir()
	bin := t.TempDir()

	target := executable(t, store, "cpybkc-gen-go-1.2.3")
	link := filepath.Join(bin, "cpybkc-gen-go")

	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("linking %s to %s: %v", link, target, err)
	}

	got, err := Resolve("go", pathOf(bin))
	if err != nil {
		t.Fatalf("resolving go through a symlink: %v", err)
	}

	if got != link {
		t.Errorf("go resolved to %s, want the link %s", got, link)
	}
}

// TestASymlinkToADirectoryIsPassedOver is the same following, seen from the
// other side: what a symlink points at is what the rule is applied to.
func TestASymlinkToADirectoryIsPassedOver(t *testing.T) {
	bin := t.TempDir()
	link := filepath.Join(bin, "cpybkc-gen-go")

	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatalf("linking %s to a directory: %v", link, err)
	}

	_, err := Resolve("go", pathOf(bin))

	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("resolving go through a link to a directory gave %v, want a NotFoundError", err)
	}

	if len(notFound.PassedOver) != 1 || notFound.PassedOver[0].Fault != "it is a directory" {
		t.Errorf("the fault passed over %v, want the link reported as a directory", notFound.PassedOver)
	}
}

// TestAGeneratorInTheImagesPluginDirectoryResolves is the container case
// (docs/container/SPEC.md, #54), and it is deliberately the same test as every
// other one here: the image's contribution is a directory on PATH that a
// derived image copies a generator into, so a plugin in it is found under the
// rules a plugin on a laptop is found by. There is no container path in this
// package to keep in step with that document.
func TestAGeneratorInTheImagesPluginDirectoryResolves(t *testing.T) {
	plugins := t.TempDir()
	want := executable(t, plugins, "cpybkc-gen-go")

	// The shape of a PATH inside an image: the system directories, and the one
	// a derived image's COPY writes into.
	image := pathOf("/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin", plugins)

	got, err := Resolve("go", image)
	if err != nil {
		t.Fatalf("resolving go on an image-shaped PATH: %v", err)
	}

	if got != want {
		t.Errorf("go resolved to %s, want %s", got, want)
	}
}

// TestAnUnresolvableNameNamesTheExecutableItLookedFor is what the fault owes an
// adopter: the file they have to install, which is not the string their
// manifest names, and every directory that was looked in.
func TestAnUnresolvableNameNamesTheExecutableItLookedFor(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()

	_, err := Resolve("go", pathOf(first, second))

	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("resolving a generator nobody installed gave %v, want a NotFoundError", err)
	}

	if notFound.Name != "go" || notFound.File != "cpybkc-gen-go" {
		t.Errorf("the fault is about %q and %q, want go and cpybkc-gen-go", notFound.Name, notFound.File)
	}

	if got := notFound.Searched; len(got) != 2 || got[0] != first || got[1] != second {
		t.Errorf("the fault reports searching %v, want %v then %v", got, first, second)
	}

	if !strings.Contains(err.Error(), "cpybkc-gen-go") {
		t.Errorf("the fault reads %q, want the executable named in it", err)
	}
}

// TestANameThatCannotBeAFilenameIsRefused enforces docs/plugin/SPEC.md's two
// MUSTs about a name at the point resolution happens, whatever route the name
// arrived by. The empty name is refused even though `cpybkc-gen-` is on PATH:
// what is on PATH is a file whose name carries no generator's name, and taking
// it would let a manifest with an empty name run something.
func TestANameThatCannotBeAFilenameIsRefused(t *testing.T) {
	dir := t.TempDir()
	executable(t, dir, Prefix)
	executable(t, dir, "cpybkc-gen-go")

	for _, name := range []string{"", "z5labs/go", "../go", "go/"} {
		got, err := Resolve(name, pathOf(dir))
		if got != "" {
			t.Errorf("%q resolved to %s, want nothing", name, got)
		}

		var invalid *InvalidNameError
		if !errors.As(err, &invalid) {
			t.Errorf("resolving %q gave %v, want an InvalidNameError", name, err)

			continue
		}

		if invalid.Name != name {
			t.Errorf("the fault is about %q, want %q", invalid.Name, name)
		}
	}
}

// stat is a file's mode and nothing else, which is all [unusable] reads. It is
// here because two of the four answers — a device, a socket — are things a test
// cannot portably make in a temporary directory, and a rule with an untested
// arm is a rule that has been written down twice rather than checked once.
type stat struct{ mode fs.FileMode }

func (s stat) Name() string       { return "cpybkc-gen-go" }
func (s stat) Size() int64        { return 0 }
func (s stat) Mode() fs.FileMode  { return s.mode }
func (s stat) ModTime() time.Time { return time.Time{} }
func (s stat) IsDir() bool        { return s.mode.IsDir() }
func (s stat) Sys() any           { return nil }

func TestUnusableSaysWhichRuleAFileFails(t *testing.T) {
	tests := []struct {
		mode fs.FileMode
		want string
	}{
		{mode: 0o755, want: ""},
		{mode: 0o700, want: ""},
		{mode: 0o111, want: ""},
		{mode: 0o001, want: ""},
		{mode: 0o644, want: "it carries no execute bit"},
		{mode: 0o000, want: "it carries no execute bit"},
		{mode: fs.ModeDir | 0o755, want: "it is a directory"},
		{mode: fs.ModeSocket | 0o755, want: "it is not a regular file"},
		{mode: fs.ModeNamedPipe | 0o755, want: "it is not a regular file"},
		{mode: fs.ModeDevice | 0o755, want: "it is not a regular file"},
	}

	for _, test := range tests {
		if got := unusable(stat{mode: test.mode}); got != test.want {
			t.Errorf("a file of mode %v is rejected as %q, want %q", test.mode, got, test.want)
		}
	}
}
