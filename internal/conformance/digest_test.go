// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package conformance

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// tree is the corpus every case below varies from.
func tree() fstest.MapFS {
	return fstest.MapFS{
		"README.md":                 &fstest.MapFile{Data: []byte("the corpus\n")},
		"orders-fixed/entry.json":   &fstest.MapFile{Data: []byte(`{"description":"x"}`)},
		"orders-fixed/input.bin":    &fstest.MapFile{Data: []byte{0x00, 0xff, 0x00}},
		"orders-fixed/values.json":  &fstest.MapFile{Data: []byte(`{"records":[]}`)},
		"zoned-ebcdic/entry.json":   &fstest.MapFile{Data: []byte(`{"description":"y"}`)},
		"zoned-ebcdic/input.bin":    &fstest.MapFile{Data: []byte{0xf1}},
		"zoned-ebcdic/nested/a.cpy": &fstest.MapFile{Data: []byte("01 A.\n")},
	}
}

// TestDigestIsAFunctionOfTheCorpus is the property the published digest rests
// on: the same corpus digests to the same thing, twice, through two independent
// walks. A producer that memoised would pass a comparison of one call against
// itself while still varying between the release that wrote the digest and the
// download that checks it.
func TestDigestIsAFunctionOfTheCorpus(t *testing.T) {
	first := mustDigest(t, tree())
	second := mustDigest(t, tree())

	if first != second {
		t.Fatalf("two digests of one corpus differ: %s then %s", first, second)
	}

	if len(first) != digestLen {
		t.Errorf("digested to %q, which is %d characters and not %d", first, len(first), digestLen)
	}
}

// TestDigestMovesWhenTheCorpusDoes covers the three ways a corpus can change,
// each of which a downloader has to be able to detect: a byte of an entry, the
// path a file is at, and a file arriving or going missing. The second is why the
// path is hashed at all — a digest over contents alone would call a file moved
// between two entries the same corpus.
func TestDigestMovesWhenTheCorpusDoes(t *testing.T) {
	base := mustDigest(t, tree())

	testCases := []struct {
		name   string
		change func(fstest.MapFS)
	}{
		{
			name: "a byte of an entry",
			change: func(corpus fstest.MapFS) {
				corpus["orders-fixed/input.bin"] = &fstest.MapFile{Data: []byte{0x00, 0xfe, 0x00}}
			},
		},
		{
			name: "a file at a different path",
			change: func(corpus fstest.MapFS) {
				corpus["zoned-ascii/input.bin"] = corpus["zoned-ebcdic/input.bin"]
				delete(corpus, "zoned-ebcdic/input.bin")
			},
		},
		{
			name: "one more file",
			change: func(corpus fstest.MapFS) {
				corpus["orders-fixed/layout.sexpr"] = &fstest.MapFile{Data: []byte("(layout)")}
			},
		},
		{
			name: "one fewer file",
			change: func(corpus fstest.MapFS) {
				delete(corpus, "README.md")
			},
		},
		{
			name: "a file emptied",
			change: func(corpus fstest.MapFS) {
				corpus["orders-fixed/input.bin"] = &fstest.MapFile{Data: nil}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			corpus := tree()
			testCase.change(corpus)

			if got := mustDigest(t, corpus); got == base {
				t.Errorf("changing %s left the digest at %s", testCase.name, got)
			}
		})
	}
}

// TestDigestSeparatesTheFilesItCovers is the length delimiter's own test, and
// the two corpora below are chosen so that it is: without the length, both
// flatten to exactly `a\0xb\0` and the digest cannot tell them apart. Remove
// the length from [DigestFS] and this test goes red, which is the only way a
// test of that property is worth having.
//
// It takes a NUL inside a file's contents to build the collision, which is
// exactly the case docs/conformance/SPEC.md's justification names: `input.bin`
// is arbitrary bytes by definition, so the contents cannot be their own
// delimiter.
func TestDigestSeparatesTheFilesItCovers(t *testing.T) {
	first := mustDigest(t, fstest.MapFS{
		"a": &fstest.MapFile{Data: []byte("xb\x00")},
	})

	second := mustDigest(t, fstest.MapFS{
		"a": &fstest.MapFile{Data: []byte("x")},
		"b": &fstest.MapFile{Data: []byte{}},
	})

	if first == second {
		t.Errorf("two corpora holding different files digest to the same %s", first)
	}
}

// TestDigestRefusesWhatIsNotAFile keeps a link out of a published corpus. Its
// content is a property of where the archive was unpacked rather than of the
// corpus, so a digest that followed one would move between machines and a digest
// that skipped one would certify a member it never read.
func TestDigestRefusesWhatIsNotAFile(t *testing.T) {
	corpus := tree()
	corpus["orders-fixed/link.bin"] = &fstest.MapFile{Mode: fs.ModeSymlink, Data: []byte("input.bin")}

	if _, err := DigestFS(corpus); err == nil {
		t.Error("digest accepted a corpus holding something that is not a regular file")
	}
}

// TestDigestRefusesAnEmptyCorpus keeps a misdirected path from producing a
// perfectly good digest of nothing. That failure is invisible where it happens
// and arrives as a downloader whose empty directory checked out fine.
func TestDigestRefusesAnEmptyCorpus(t *testing.T) {
	if _, err := DigestFS(fstest.MapFS{}); err == nil {
		t.Error("digest accepted a corpus holding no file")
	}
}

// TestDigestCoversTheCorpusOnDisk is the staleness gate, and it is what the
// tests above cannot be: they all run over a tree this file built, which cannot
// notice that the digest has stopped covering the corpus this repository
// actually publishes. So this one digests testdata/conformance and requires
// every file under it to have contributed.
func TestDigestCoversTheCorpusOnDisk(t *testing.T) {
	dir := CorpusPath(repoRoot(t))

	if _, err := Digest(dir); err != nil {
		t.Fatalf("Digest: %v", err)
	}

	covered, err := digestFiles(os.DirFS(dir))
	if err != nil {
		t.Fatalf("digestFiles: %v", err)
	}

	var want int

	err = filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !entry.IsDir() {
			want++
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}

	if want == 0 {
		t.Fatal("the corpus on disk holds no file: the check would pass vacuously")
	}

	if len(covered) != want {
		t.Errorf("the digest covers %d of the corpus's %d files", len(covered), want)
	}
}

// TestCheckDigestReadsThePublishedDigest covers the three states a downloaded
// corpus can be in, because they are three different things to tell somebody:
// checked and right, checked and wrong, and nothing to check against.
func TestCheckDigestReadsThePublishedDigest(t *testing.T) {
	dir := writeCorpus(t)

	computed, err := Digest(dir)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}

	t.Run("no digest beside it", func(t *testing.T) {
		got, checked, err := CheckDigest(dir)
		if err != nil {
			t.Fatalf("CheckDigest: %v", err)
		}

		if checked {
			t.Error("CheckDigest reported a check against a digest that is not there")
		}

		if got != computed {
			t.Errorf("CheckDigest computed %s, want %s", got, computed)
		}
	})

	t.Run("the published digest agrees", func(t *testing.T) {
		writeFile(t, DigestPath(dir), FormatDigest(computed))

		got, checked, err := CheckDigest(dir)
		if err != nil {
			t.Fatalf("CheckDigest: %v", err)
		}

		if !checked {
			t.Error("CheckDigest read a digest file and reported no check")
		}

		if got != computed {
			t.Errorf("CheckDigest computed %s, want %s", got, computed)
		}
	})

	t.Run("the published digest disagrees", func(t *testing.T) {
		writeFile(t, DigestPath(dir), FormatDigest("0000000000000000000000000000000000000000000000000000000000000000"))

		if _, _, err := CheckDigest(dir); err == nil {
			t.Error("CheckDigest passed a corpus that does not match its published digest")
		}
	})
}

// TestReadDigestRefusesWhatIsNotADigest keeps a truncated or rewritten digest
// file from being read as agreement. A file that arrived half-downloaded is
// exactly the case the digest is there to catch, and it is the same download
// that carried the corpus.
func TestReadDigestRefusesWhatIsNotADigest(t *testing.T) {
	testCases := []struct {
		name    string
		content string
	}{
		{name: "empty", content: ""},
		{name: "truncated", content: "abc123\n"},
		{name: "not hexadecimal", content: "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz\n"},
		{
			name:    "uppercase",
			content: "0000000000000000000000000000000000000000000000000000000000ABCDEF\n",
		},
		{
			name:    "an sha256sum line",
			content: "0000000000000000000000000000000000000000000000000000000000000000  corpus\n",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "corpus.sha256")
			writeFile(t, path, []byte(testCase.content))

			if _, err := ReadDigest(path); err == nil {
				t.Errorf("ReadDigest accepted %q", testCase.content)
			}
		})
	}
}

// TestFormatDigestIsReadBack pins the two halves of the file format to each
// other. They are a producer in one command and a reader in another, and a
// trailing newline one of them stopped writing would be a mismatch reported as
// a corrupted download.
func TestFormatDigestIsReadBack(t *testing.T) {
	const want = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	path := filepath.Join(t.TempDir(), "corpus.sha256")
	writeFile(t, path, FormatDigest(want))

	got, err := ReadDigest(path)
	if err != nil {
		t.Fatalf("ReadDigest: %v", err)
	}

	if got != want {
		t.Errorf("read back %s, want %s", got, want)
	}
}

// TestDigestPathSitsBesideTheCorpus states the naming rule a downloader follows
// to find the file at all, including that a trailing separator does not change
// where it looks.
func TestDigestPathSitsBesideTheCorpus(t *testing.T) {
	want := filepath.Join("a", "corpus") + DigestExt

	for _, dir := range []string{filepath.Join("a", "corpus"), filepath.Join("a", "corpus") + string(filepath.Separator)} {
		if got := DigestPath(dir); got != want {
			t.Errorf("DigestPath(%q) is %q, want %q", dir, got, want)
		}
	}
}

// TestDigestPathNamesACorpusThatNamesItselfNothing is the case a downloader
// reaches by working inside the corpus: `--corpus .`. Cleaning leaves a
// directory with no name for a file to sit beside, and appending would produce
// `..sha256` — a path nobody can have created, so the digest reads as absent and
// the run proceeds unchecked, which is the one outcome the digest exists to
// prevent.
func TestDigestPathNamesACorpusThatNamesItselfNothing(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	for _, corpus := range []string{".", "", "a" + string(filepath.Separator) + ".."} {
		got := DigestPath(corpus)

		if !filepath.IsAbs(got) {
			t.Errorf("DigestPath(%q) is %q, which names no corpus to sit beside", corpus, got)

			continue
		}

		if base := filepath.Base(got); base == DigestExt || base == "."+DigestExt {
			t.Errorf("DigestPath(%q) is %q, which is a file nobody could have written", corpus, got)
		}
	}
}

// mustDigest digests corpus through the unexported walk and fails the test if it
// cannot.
func mustDigest(t *testing.T, corpus fstest.MapFS) string {
	t.Helper()

	got, err := DigestFS(corpus)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}

	return got
}

// writeCorpus writes a small corpus to a fresh directory and returns its path.
func writeCorpus(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "corpus")

	for name, file := range tree() {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(name)), file.Data)
	}

	return dir
}

// writeFile writes b to path, creating the directories above it.
func writeFile(t *testing.T, path string, b []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
