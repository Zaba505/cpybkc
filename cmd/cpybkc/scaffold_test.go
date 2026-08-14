// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scaffoldCopybook is two independent REDEFINES runs over one 01-level: six
// record types, which is what makes a run of `init` over it say something on
// standard error as well as writing a file.
//
// Fixed source format, which is the one `init` reads.
const scaffoldCopybook = `       01  POSTING-RECORD.
           05  PST-TYPE            PIC X(2).
           05  PST-BODY            PIC X(28).
           05  PST-DEBIT           REDEFINES PST-BODY PIC X(28).
           05  PST-CREDIT          REDEFINES PST-BODY PIC X(28).
           05  PST-TAIL            PIC X(8).
           05  PST-TAIL-REF        REDEFINES PST-TAIL PIC X(8).
`

// copybookIn writes the copybook a scaffolding test derives from, and returns
// the directory it is in and the path to it.
func copybookIn(t *testing.T) (dir, path string) {
	t.Helper()

	dir = t.TempDir()
	path = filepath.Join(dir, "posting.cpy")

	writeFile(t, path, scaffoldCopybook)

	return dir, path
}

func TestInitWritesAScaffoldWhereOutNames(t *testing.T) {
	t.Parallel()

	dir, copybook := copybookIn(t)
	dest := filepath.Join(dir, "layout.sexpr")

	stdout, _, code := invoke(initSubcommand, copybookFlag, copybook, outFlag, dest)

	if code != statusOK {
		t.Fatalf("`cpybkc %s` exited %d, want %d", initSubcommand, code, statusOK)
	}

	// A run writing to a path writes nothing to standard output at all.
	if stdout != "" {
		t.Errorf("a run writing to a path wrote %q to standard output", stdout)
	}

	written, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading back the scaffold: %v", err)
	}

	for _, want := range []string{
		"(record POSTING-RECORD-PST-DEBIT-PST-TAIL",
		`(copybook "` + copybook + `" POSTING-RECORD)`,
		";; (encoding",
		";; (sequence",
	} {
		if !strings.Contains(string(written), want) {
			t.Errorf("the scaffold does not carry %q:\n%s", want, written)
		}
	}
}

// docs/cli/SPEC.md: the combination count is reported, not bounded, and it
// reaches standard error because it is the size of the work in front of the
// reader.
func TestTheCombinationCountReachesStandardErrorAsANote(t *testing.T) {
	t.Parallel()

	dir, copybook := copybookIn(t)

	_, stderr, code := invoke(initSubcommand, copybookFlag, copybook, outFlag, filepath.Join(dir, "layout.sexpr"))

	if code != statusOK {
		t.Fatalf("`cpybkc %s` exited %d, want %d", initSubcommand, code, statusOK)
	}

	if !strings.HasPrefix(stderr, severityNote+severitySeparator) {
		t.Fatalf("the line reads %q, want a %s%s line", stderr, severityNote, severitySeparator)
	}

	for _, want := range []string{"POSTING-RECORD", "3 REDEFINES", "6 record types"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the note does not carry %q: %q", want, stderr)
		}
	}
}

func TestInitWritesTheScaffoldToStandardOutputForADash(t *testing.T) {
	t.Parallel()

	dir, copybook := copybookIn(t)

	stdout, stderr, code := invoke(initSubcommand, copybookFlag, copybook, outFlag, "-")

	if code != statusOK {
		t.Fatalf("`cpybkc %s` exited %d, want %d", initSubcommand, code, statusOK)
	}

	if !strings.HasPrefix(stdout, ";; A layout scaffold") {
		t.Errorf("standard output does not open with the scaffold: %q", stdout)
	}

	// Nothing else goes there. The note the run owes is on the other stream,
	// which is what keeps `cpybkc init --out - > layout.sexpr` a file that
	// parses.
	if strings.Contains(stdout, severityNote+severitySeparator) {
		t.Errorf("a diagnostic reached standard output:\n%s", stdout)
	}

	if !strings.Contains(stderr, severityNote+severitySeparator) {
		t.Errorf("the note did not reach standard error: %q", stderr)
	}

	// And no file appeared: "-" is never a relative path.
	if entries, err := os.ReadDir(dir); err != nil {
		t.Fatalf("reading the copybook's directory: %v", err)
	} else if len(entries) != 1 {
		t.Errorf("the run left %d entries beside the copybook, want only the copybook", len(entries))
	}
}

// The one unrecoverable act this command could perform, refused: the derived
// half of a layout is recomputable and the discriminators and the sequence are
// not.
func TestInitNeverWritesOverWhatIsAlreadyThere(t *testing.T) {
	t.Parallel()

	dir, copybook := copybookIn(t)
	dest := filepath.Join(dir, "layout.sexpr")

	edited := "(sequence (seq A B))\n"
	writeFile(t, dest, edited)

	stdout, stderr, code := invoke(initSubcommand, copybookFlag, copybook, outFlag, dest)

	if code != statusFailed {
		t.Fatalf("writing over an existing layout exited %d, want %d", code, statusFailed)
	}

	if stdout != "" {
		t.Errorf("a refused run wrote %q to standard output", stdout)
	}

	// Both spellings of the path: the one that was typed, and the one cpybkc
	// looked at.
	absolute, err := filepath.Abs(dest)
	if err != nil {
		t.Fatalf("resolving the destination: %v", err)
	}

	if !strings.Contains(stderr, dest) || !strings.Contains(stderr, absolute) {
		t.Errorf("the diagnostic names neither the path as typed nor the absolute path: %q", stderr)
	}

	kept, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading back the destination: %v", err)
	}

	if string(kept) != edited {
		t.Errorf("the destination now holds %q, want what was there", kept)
	}
}

// docs/cli/SPEC.md: a --copybook naming a directory fails the run rather than
// the vector, because it is not decidable from the line.
func TestACopybookNamingADirectoryFailsTheRun(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	named := filepath.Join(dir, "copybooks")

	if err := os.Mkdir(named, 0o755); err != nil {
		t.Fatalf("preparing the directory: %v", err)
	}

	stdout, stderr, code := invoke(initSubcommand,
		copybookFlag, named, outFlag, filepath.Join(dir, "layout.sexpr"))

	if code != statusFailed {
		t.Fatalf("a --copybook naming a directory exited %d, want %d", code, statusFailed)
	}

	if stdout != "" {
		t.Errorf("a failed run wrote %q to standard output", stdout)
	}

	if !strings.Contains(stderr, named) {
		t.Errorf("the diagnostic does not name the path as it was typed: %q", stderr)
	}

	// A failure of the work is not a failure of the vector, so the command set
	// is not what the reader needs in front of them.
	if strings.Contains(stderr, "Usage:") {
		t.Errorf("a status 1 was answered with usage: %q", stderr)
	}

	if _, err := os.Stat(filepath.Join(dir, "layout.sexpr")); err == nil {
		t.Error("a scaffold was written for a run that failed")
	}
}

// Nothing is written where anything failed, which is what keeps a run's
// incompleteness the deliberate kind.
func TestNothingIsWrittenWhereOneCopybookCouldNotBeRead(t *testing.T) {
	t.Parallel()

	dir, copybook := copybookIn(t)
	dest := filepath.Join(dir, "layout.sexpr")

	_, stderr, code := invoke(initSubcommand,
		copybookFlag, copybook,
		copybookFlag, filepath.Join(dir, "absent.cpy"),
		outFlag, dest,
	)

	if code != statusFailed {
		t.Fatalf("a run naming a copybook that is not there exited %d, want %d", code, statusFailed)
	}

	if !strings.Contains(stderr, "absent.cpy") {
		t.Errorf("the diagnostic does not name the copybook: %q", stderr)
	}

	if _, err := os.Stat(dest); err == nil {
		t.Error("a scaffold was written over a copybook that could not be read")
	}
}

// docs/cli/SPEC.md requires determinism rather than hoping for it: a scaffold
// that reordered itself between runs would be undiffable in exactly the review
// where an adopter is deciding what to keep.
func TestTwoRunsOverOneSetOfCopybooksWriteByteIdenticalFiles(t *testing.T) {
	t.Parallel()

	dir, copybook := copybookIn(t)

	first := filepath.Join(dir, "first.sexpr")
	second := filepath.Join(dir, "second.sexpr")

	for _, dest := range []string{first, second} {
		if _, _, code := invoke(initSubcommand, copybookFlag, copybook, outFlag, dest); code != statusOK {
			t.Fatalf("writing %s exited %d, want %d", dest, code, statusOK)
		}
	}

	one, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("reading back the first scaffold: %v", err)
	}

	two, err := os.ReadFile(second)
	if err != nil {
		t.Fatalf("reading back the second scaffold: %v", err)
	}

	if string(one) != string(two) {
		t.Error("two runs over one copybook wrote different files")
	}
}
