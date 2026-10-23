// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"os"
	"path/filepath"
	"slices"
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

// The layout where --out names it, and the copybook's definitions file beside
// it — beside the layout, not beside the copybook, named after the copybook's
// final path element — and nothing else anywhere.
func TestInitWritesTheLayoutWhereOutNamesAndADefinitionsFileBesideIt(t *testing.T) {
	t.Parallel()

	_, copybook := copybookIn(t)
	out := t.TempDir()
	dest := filepath.Join(out, "ledger.sexpr")

	stdout, _, code := invoke(initSubcommand, copybookFlag, copybook, outFlag, dest)

	if code != statusOK {
		t.Fatalf("`cpybkc %s` exited %d, want %d", initSubcommand, code, statusOK)
	}

	// `init` writes nothing to standard output at all.
	if stdout != "" {
		t.Errorf("a run wrote %q to standard output", stdout)
	}

	written, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading back the scaffold: %v", err)
	}

	for _, want := range []string{
		"(record POSTING-RECORD-PST-DEBIT-PST-TAIL",
		`(definitions "posting.cpy.sexpr" POSTING-RECORD)`,
		";; (encoding",
		";; (sequence",
	} {
		if !strings.Contains(string(written), want) {
			t.Errorf("the scaffold does not carry %q:\n%s", want, written)
		}
	}

	transcribed, err := os.ReadFile(filepath.Join(out, "posting.cpy.sexpr"))
	if err != nil {
		t.Fatalf("reading back the definitions file: %v", err)
	}

	for _, want := range []string{
		`; The entries of the copybook "` + copybook + `"`,
		"(entry 01 POSTING-RECORD)",
		`  (entry 05 PST-TAIL-REF (redefines PST-TAIL) (pic "X(8)"))`,
	} {
		if !strings.Contains(string(transcribed), want) {
			t.Errorf("the definitions file does not carry %q:\n%s", want, transcribed)
		}
	}

	if left := entriesIn(t, out); !slices.Equal(left, []string{"ledger.sexpr", "posting.cpy.sexpr"}) {
		t.Errorf("the run left %v in the layout's directory, want the layout and its definitions file", left)
	}
}

// entriesIn is the names in dir, sorted.
func entriesIn(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
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

// docs/cli/SPEC.md: `--out -` is a usage error, and a 2 means cpybkc did
// nothing at all — no copybook opened, no file created, nothing on standard
// output.
func TestInitRefusesADashForOutAndWritesNothing(t *testing.T) {
	t.Parallel()

	dir, copybook := copybookIn(t)

	stdout, stderr, code := invoke(initSubcommand, copybookFlag, copybook, outFlag, "-")

	if code != statusUsage {
		t.Fatalf("`cpybkc %s --out -` exited %d, want %d", initSubcommand, code, statusUsage)
	}

	if stdout != "" {
		t.Errorf("a refused run wrote %q to standard output", stdout)
	}

	if !strings.Contains(stderr, outFlag) {
		t.Errorf("the diagnostic does not name %s: %q", outFlag, stderr)
	}

	if left := entriesIn(t, dir); !slices.Equal(left, []string{"posting.cpy"}) {
		t.Errorf("a refused run left %v beside the copybook, want only the copybook", left)
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

	// Every destination is checked before anything is written, so the
	// definitions file the layout would have named was not written either.
	if left := entriesIn(t, dir); !slices.Equal(left, []string{"layout.sexpr", "posting.cpy"}) {
		t.Errorf("a refused run left %v, want only what was there", left)
	}
}

// A definitions file is held to the same rule as the layout: something already
// at its place beside the layout fails the run, and nothing is written — not
// the layout, and not the file that was there.
func TestAnOccupiedDefinitionsFileFailsTheRunAndNothingIsWritten(t *testing.T) {
	t.Parallel()

	dir, copybook := copybookIn(t)
	dest := filepath.Join(dir, "layout.sexpr")
	beside := filepath.Join(dir, "posting.cpy.sexpr")

	// A transcription the adopter has committed, and the reason the file is
	// held to the rule: their copybook may not be on this machine to
	// recompute it from.
	committed := "(entry 01 POSTING-RECORD)\n"
	writeFile(t, beside, committed)

	_, stderr, code := invoke(initSubcommand, copybookFlag, copybook, outFlag, dest)

	if code != statusFailed {
		t.Fatalf("writing over a definitions file exited %d, want %d", code, statusFailed)
	}

	if !strings.Contains(stderr, beside) {
		t.Errorf("the diagnostic does not name the definitions file's path: %q", stderr)
	}

	if _, err := os.Lstat(dest); err == nil {
		t.Error("the layout was written beside a definitions file the run refused to write")
	}

	if kept, err := os.ReadFile(beside); err != nil || string(kept) != committed {
		t.Errorf("the definitions file now holds %q (%v), want what was there", kept, err)
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

	_, copybook := copybookIn(t)

	// Two scratch directories, which is how an adopter checking for drift runs
	// it: the second run's definitions file would otherwise be refused beside
	// the first's.
	first, second := t.TempDir(), t.TempDir()

	for _, dir := range []string{first, second} {
		dest := filepath.Join(dir, "layout.sexpr")
		if _, _, code := invoke(initSubcommand, copybookFlag, copybook, outFlag, dest); code != statusOK {
			t.Fatalf("writing %s exited %d, want %d", dest, code, statusOK)
		}
	}

	for _, name := range []string{"layout.sexpr", "posting.cpy.sexpr"} {
		one, err := os.ReadFile(filepath.Join(first, name))
		if err != nil {
			t.Fatalf("reading back the first run's %s: %v", name, err)
		}

		two, err := os.ReadFile(filepath.Join(second, name))
		if err != nil {
			t.Fatalf("reading back the second run's %s: %v", name, err)
		}

		if string(one) != string(two) {
			t.Errorf("two runs over one copybook wrote different %s files", name)
		}
	}
}

// Two spellings of one file sharing a final path element would write two
// definitions files of one name, and are refused from the vector before
// anything is opened (docs/cli/SPEC.md, "The vector `init` takes"). This
// replaces #406's test, under which the pair was read and every record they
// derived was noted.
func TestTwoSpellingsOfOneFileAreAUsageErrorAndNothingIsWritten(t *testing.T) {
	t.Parallel()

	dir, copybook := copybookIn(t)
	// Built by hand rather than with filepath.Join, which would clean the `.`
	// away and hand back the first spelling.
	again := dir + string(filepath.Separator) + "." + string(filepath.Separator) + "posting.cpy"
	dest := filepath.Join(dir, "layout.sexpr")

	_, stderr, code := invoke(initSubcommand, copybookFlag, copybook, copybookFlag, again, outFlag, dest)

	if code != statusUsage {
		t.Fatalf("`cpybkc %s` over two spellings of one file exited %d, want %d:\n%s",
			initSubcommand, code, statusUsage, stderr)
	}

	for _, path := range []string{copybook, again} {
		if !strings.Contains(stderr, path) {
			t.Errorf("the diagnostic does not name %s:\n%s", path, stderr)
		}
	}

	if left := entriesIn(t, dir); !slices.Equal(left, []string{"posting.cpy"}) {
		t.Errorf("a refused run left %v beside the copybook", left)
	}
}

// Two names for one file that do not share a final path element — a symbolic
// link by another name — are two copybooks with two definitions files, and
// every record they declare is derived twice: a derived record-name collision,
// noted rather than refused. Both definitions files are written, no colliding
// record is, and the run exits 0 (docs/cli/SPEC.md, "Where the copybooks come
// from").
func TestTwoNamesForOneFileWriteTwoDefinitionsFilesAndNoteEveryRecord(t *testing.T) {
	t.Parallel()

	dir, copybook := copybookIn(t)
	linked := filepath.Join(dir, "linked.cpy")

	if err := os.Symlink(copybook, linked); err != nil {
		t.Fatalf("preparing the link: %v", err)
	}

	out := t.TempDir()
	dest := filepath.Join(out, "layout.sexpr")

	_, stderr, code := invoke(initSubcommand, copybookFlag, copybook, copybookFlag, linked, outFlag, dest)

	if code != statusOK {
		t.Fatalf("`cpybkc %s` over two names for one file exited %d, want %d:\n%s",
			initSubcommand, code, statusOK, stderr)
	}

	// Six record types, each derived once per name.
	if got := strings.Count(stderr, "none of them is written"); got != 12 {
		t.Errorf("noted %d colliding record types, want all twelve:\n%s", got, stderr)
	}

	for _, path := range []string{copybook, linked} {
		if !strings.Contains(stderr, path+": no record is derived from this copybook") {
			t.Errorf("no note says %s contributed no record:\n%s", path, stderr)
		}
	}

	if left := entriesIn(t, out); !slices.Equal(left, []string{"layout.sexpr", "linked.cpy.sexpr", "posting.cpy.sexpr"}) {
		t.Errorf("the run wrote %v, want the layout and both definitions files", left)
	}

	written, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading back the scaffold: %v", err)
	}

	if strings.Contains(string(written), "\n(record ") {
		t.Errorf("a colliding record was written:\n%s", written)
	}
}

// docs/cli/SPEC.md, "`init`'s streams and exit codes": a copybook no record
// derives from — here, nothing but a level-77 work item — still gets its
// definitions file, and a note, and the run exits 0.
func TestACopybookDeclaringNoNamedLevelIsTranscribedAndNoted(t *testing.T) {
	t.Parallel()

	dir, copybook := copybookIn(t)
	work := filepath.Join(dir, "work.cpy")
	dest := filepath.Join(dir, "layout.sexpr")

	writeFile(t, work, "       77  WORK-COUNT              PIC 9(4).\n")

	_, stderr, code := invoke(initSubcommand, copybookFlag, copybook, copybookFlag, work, outFlag, dest)

	if code != statusOK {
		t.Fatalf("a run over a copybook declaring no named 01-level exited %d, want %d:\n%s", code, statusOK, stderr)
	}

	if !strings.Contains(stderr, work+": no record is derived from this copybook") {
		t.Errorf("no note names the copybook as typed: %q", stderr)
	}

	transcribed, err := os.ReadFile(filepath.Join(dir, "work.cpy.sexpr"))
	if err != nil {
		t.Fatalf("reading back the definitions file: %v", err)
	}

	if !strings.Contains(string(transcribed), `(entry 77 WORK-COUNT (pic "9(4)"))`) {
		t.Errorf("the definitions file does not carry the 77-level:\n%s", transcribed)
	}
}

// A copybook declaring no entry at all has nothing to transcribe: the run fails
// with status 1, names the copybook, and writes nothing — not the layout, and
// not the other copybook's definitions file.
func TestACopybookDeclaringNoEntryFailsTheRunAndNothingIsWritten(t *testing.T) {
	t.Parallel()

	dir, copybook := copybookIn(t)
	empty := filepath.Join(dir, "empty.cpy")
	dest := filepath.Join(dir, "layout.sexpr")

	writeFile(t, empty, "      * a member that describes nothing\n")

	_, stderr, code := invoke(initSubcommand, copybookFlag, copybook, copybookFlag, empty, outFlag, dest)

	if code != statusFailed {
		t.Fatalf("a run over a copybook declaring no entry exited %d, want %d", code, statusFailed)
	}

	if !strings.Contains(stderr, empty) {
		t.Errorf("the diagnostic does not name the copybook: %q", stderr)
	}

	if left := entriesIn(t, dir); !slices.Equal(left, []string{"empty.cpy", "posting.cpy"}) {
		t.Errorf("a failed run left %v", left)
	}
}
