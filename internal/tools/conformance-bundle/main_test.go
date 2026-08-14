// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Zaba505/cpybkc/internal/conformance"
)

// corpus is a stand-in for testdata/conformance: enough of an entry's shape for
// the layout claims below, and nothing the loader would have to accept.
func corpus() fstest.MapFS {
	return fstest.MapFS{
		"README.md":                &fstest.MapFile{Data: []byte("the corpus\n")},
		"orders-fixed/entry.json":  &fstest.MapFile{Data: []byte(`{"description":"x"}`)},
		"orders-fixed/input.bin":   &fstest.MapFile{Data: []byte{0x00, 0xff}},
		"orders-fixed/values.json": &fstest.MapFile{Data: []byte(`{"records":[]}`)},
	}
}

// engines is a stand-in for a directory of built executables.
func engines() fstest.MapFS {
	return fstest.MapFS{
		"cpybkc-conform-linux-amd64":  &fstest.MapFile{Data: []byte("\x7fELF linux")},
		"cpybkc-conform-darwin-arm64": &fstest.MapFile{Data: []byte("\xcf\xfa\xed\xfe darwin")},
	}
}

// TestArchiveIsReproducible is the acceptance criterion for this artifact: the
// same corpus and the same engines produce the same bytes. It archives one pair
// twice through two independent writers, because a producer that memoised its
// output would pass a comparison of one run against itself while still varying
// between builds.
func TestArchiveIsReproducible(t *testing.T) {
	first := archive(t, corpus(), engines())
	second := archive(t, corpus(), engines())

	if len(first) == 0 {
		t.Fatal("the archive is empty")
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("two archives of one corpus differ: %d bytes then %d bytes", len(first), len(second))
	}
}

// TestArchiveCarriesNothingFromTheFilesystem asserts what makes the property
// above hold across machines rather than only across two calls in one process. A
// modification time, an owner or a group taken from the trees being archived
// would make the artifact a function of the checkout as well as of its contents,
// and the two builds that disagree would be on different machines, where nobody
// is comparing.
func TestArchiveCarriesNothingFromTheFilesystem(t *testing.T) {
	for _, header := range headers(t, archive(t, corpus(), engines())) {
		if got := header.ModTime.Unix(); got != 0 {
			t.Errorf("%s carries modification time %d, want 0", header.Name, got)
		}

		if header.Uid != 0 || header.Gid != 0 {
			t.Errorf("%s carries uid/gid %d/%d, want 0/0", header.Name, header.Uid, header.Gid)
		}

		if header.Uname != "" || header.Gname != "" {
			t.Errorf("%s carries owner names %q/%q, want empty", header.Name, header.Uname, header.Gname)
		}

		want := int64(fileMode)
		if strings.HasPrefix(header.Name, path.Join(archiveRoot, binDir)+"/") {
			want = execMode
		}

		if header.Mode != want {
			t.Errorf("%s carries mode %o, want %o", header.Name, header.Mode, want)
		}
	}
}

// TestArchiveUnpacksIntoOneDirectory states the layout the archive's README
// documents and the invocation in it depends on: one root everything is under,
// the corpus where cpybkc-conform looks for it by default, the digest beside
// that rather than inside it, and the engines executable.
func TestArchiveUnpacksIntoOneDirectory(t *testing.T) {
	want := []string{
		archiveRoot + "/README.md",
		archiveRoot + "/bin/cpybkc-conform-darwin-arm64",
		archiveRoot + "/bin/cpybkc-conform-linux-amd64",
		archiveRoot + "/corpus.sha256",
		archiveRoot + "/corpus/README.md",
		archiveRoot + "/corpus/orders-fixed/entry.json",
		archiveRoot + "/corpus/orders-fixed/input.bin",
		archiveRoot + "/corpus/orders-fixed/values.json",
	}

	if got := entryNames(t, archive(t, corpus(), engines())); !slices.Equal(got, want) {
		t.Errorf("archived\n got: %v\nwant: %v", got, want)
	}
}

// TestArchiveCarriesTheDigestOfTheCorpusInIt is the claim the whole digest rests
// on. The number published in the archive has to be the number a downloader
// computes from the corpus in the same archive, and the two are written by
// different programs — so a producer that digested a different tree, or a
// different reading of the same one, would ship an artifact that fails its own
// check on arrival.
func TestArchiveCarriesTheDigestOfTheCorpusInIt(t *testing.T) {
	unpacked := unpack(t, archive(t, corpus(), engines()))

	published, ok := unpacked[archiveRoot+"/corpus.sha256"]
	if !ok {
		t.Fatal("the archive carries no corpus.sha256")
	}

	held := fstest.MapFS{}

	for name, b := range unpacked {
		if inside, ok := strings.CutPrefix(name, archiveRoot+"/"+conformance.PublishedCorpusDir+"/"); ok {
			held[inside] = &fstest.MapFile{Data: b}
		}
	}

	want, err := conformance.DigestFS(held)
	if err != nil {
		t.Fatalf("digest the archived corpus: %v", err)
	}

	if got := string(published); got != string(conformance.FormatDigest(want)) {
		t.Errorf("corpus.sha256 holds %q, and the corpus in the archive digests to %s", got, want)
	}
}

// TestArchiveRefusesToShipWithoutAPart keeps a well-formed archive that is
// missing a third of itself from being published. Each of these is invisible at
// the point it happens — a misdirected flag, a build step that wrote nowhere —
// and arrives as a consumer's release download that cannot do what it says.
func TestArchiveRefusesToShipWithoutAPart(t *testing.T) {
	testCases := []struct {
		name    string
		corpus  fs.FS
		engines fs.FS
	}{
		{name: "no corpus", corpus: fstest.MapFS{}, engines: engines()},
		{name: "no engines", corpus: corpus(), engines: fstest.MapFS{}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := writeArchive(io.Discard, testCase.corpus, testCase.engines); err == nil {
				t.Error("writeArchive published an archive missing a part of itself")
			}
		})
	}
}

// TestArchiveRefusesToCarryOnePathTwice is about a failure that would be silent
// on both sides. Two of the three trees contributing one path writes two entries
// under it, and which one a consumer ends up with is decided by whichever their
// tar extracted last — so the artifact would be well-formed, uploadable, and
// hold a file whose contents depend on how it was unpacked.
//
// Nothing collides today. What this pins is that adding a file to the archive's
// own documentation cannot quietly shadow the corpus digest.
func TestArchiveRefusesToCarryOnePathTwice(t *testing.T) {
	documentation := fstest.MapFS{
		"README.md": &fstest.MapFile{Data: []byte("how to run it\n")},
		conformance.PublishedCorpusDir + conformance.DigestExt: &fstest.MapFile{
			Data: []byte("not the digest\n"),
		},
	}

	if _, err := contents(documentation, corpus(), engines()); err == nil {
		t.Error("contents archived one path twice")
	}
}

// TestArchiveRefusesWhatIsNotAFile keeps a link out of a published archive, for
// the reason the corpus digest refuses one: its content is a property of where
// the archive was unpacked rather than of what was archived.
func TestArchiveRefusesWhatIsNotAFile(t *testing.T) {
	built := engines()
	built["cpybkc-conform"] = &fstest.MapFile{Mode: fs.ModeSymlink, Data: []byte("cpybkc-conform-linux-amd64")}

	if err := writeArchive(io.Discard, corpus(), built); err == nil {
		t.Error("writeArchive accepted a tree holding something that is not a regular file")
	}
}

// TestRunArchivesTheCorpusOnDisk is the staleness gate. Everything above works
// on trees a test built, which cannot notice that the archive has stopped
// carrying the corpus this repository actually publishes. So this one runs the
// program over testdata/conformance and requires every file in it to be in the
// archive, at its own path under corpus/.
func TestRunArchivesTheCorpusOnDisk(t *testing.T) {
	root := repoRoot(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "cpybkc-conformance.tar.gz")

	// A stand-in for the engines, because building five of them here would make
	// this test a cross-compilation. What is being checked is the corpus half;
	// that the real engines are built and put where this reads them is
	// .dagger/main.go's ConformanceBundle, and that they are non-empty is
	// ConformanceArtifact.
	built := filepath.Join(dir, "bin")
	if err := os.MkdirAll(built, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", built, err)
	}

	if err := os.WriteFile(filepath.Join(built, "cpybkc-conform-linux-amd64"), []byte("elf"), 0o755); err != nil {
		t.Fatalf("write the stand-in engine: %v", err)
	}

	if err := run([]string{"-corpus", conformance.CorpusPath(root), "-bin", built, "-o", out}); err != nil {
		t.Fatalf("run: %v", err)
	}

	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read %s: %v", out, err)
	}

	archived := entryNames(t, b)

	var want int

	err = filepath.WalkDir(conformance.CorpusPath(root), func(name string, item fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if item.IsDir() {
			return nil
		}

		want++

		relative, err := filepath.Rel(conformance.CorpusPath(root), name)
		if err != nil {
			return err
		}

		inside := path.Join(archiveRoot, conformance.PublishedCorpusDir, filepath.ToSlash(relative))

		if !slices.Contains(archived, inside) {
			t.Errorf("the archive does not carry %s", inside)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk the corpus: %v", err)
	}

	if want == 0 {
		t.Fatal("the corpus on disk holds no file: the check would pass vacuously")
	}
}

// TestRunLeavesNothingBehindWhenItFails is what the rename buys. A release job
// uploads whatever is at the path it asked for, so a half-written archive left
// by a failed build is one that ships.
func TestRunLeavesNothingBehindWhenItFails(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "cpybkc-conformance.tar.gz")

	// An empty engines directory, which is a failure contents refuses after the
	// destination has already been created.
	built := filepath.Join(dir, "bin")
	if err := os.MkdirAll(built, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", built, err)
	}

	if err := run([]string{"-corpus", conformance.CorpusPath(repoRoot(t)), "-bin", built, "-o", out}); err == nil {
		t.Fatal("run published an archive with no engine in it")
	}

	for _, name := range []string{out, out + ".part"} {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Errorf("%s is still there after a failed build", name)
		}
	}
}

// TestRunRejectsBadUsage keeps the failure modes failures, for the reason
// ir-protos' copy of this test gives: a release job that uploads nothing and
// reports success is the one outcome nobody notices.
func TestRunRejectsBadUsage(t *testing.T) {
	dir := t.TempDir()

	testCases := []struct {
		name string
		args []string
	}{
		{name: "no output path", args: []string{"-bin", dir}},
		{name: "no engines directory", args: []string{"-o", filepath.Join(dir, "a.tar.gz")}},
		{name: "empty output path", args: []string{"-o", "", "-bin", dir}},
		{
			name: "unexpected operand",
			args: []string{"-o", filepath.Join(dir, "b.tar.gz"), "-bin", dir, "extra"},
		},
		{
			name: "missing corpus",
			args: []string{"-o", filepath.Join(dir, "c.tar.gz"), "-bin", dir, "-corpus", filepath.Join(dir, "nowhere")},
		},
		{
			name: "missing engines directory",
			args: []string{"-o", filepath.Join(dir, "d.tar.gz"), "-bin", filepath.Join(dir, "nowhere")},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := run(testCase.args); err == nil {
				t.Error("run accepted arguments it should have refused")
			}
		})
	}
}

// archive writes one corpus and one set of engines through writeArchive and
// returns the bytes.
func archive(t *testing.T, corpus, engines fs.FS) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := writeArchive(&buf, corpus, engines); err != nil {
		t.Fatalf("writeArchive: %v", err)
	}

	return buf.Bytes()
}

// entryNames reads the entry paths out of a gzipped archive, in the order they
// were written.
func entryNames(t *testing.T, b []byte) []string {
	t.Helper()

	var names []string
	for _, header := range headers(t, b) {
		names = append(names, header.Name)
	}

	return names
}

// unpack reads a gzipped archive into its entries' contents, by path.
func unpack(t *testing.T, b []byte) map[string][]byte {
	t.Helper()

	held := map[string][]byte{}

	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("open the gzip stream: %v", err)
	}

	reader := tar.NewReader(gz)

	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			t.Fatalf("read the archive: %v", err)
		}

		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read %s: %v", header.Name, err)
		}

		held[header.Name] = content
	}

	if err := gz.Close(); err != nil {
		t.Fatalf("close the gzip stream: %v", err)
	}

	return held
}

// headers reads every tar header out of a gzipped archive, in order.
func headers(t *testing.T, b []byte) []*tar.Header {
	t.Helper()

	var got []*tar.Header

	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("open the gzip stream: %v", err)
	}

	reader := tar.NewReader(gz)

	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			t.Fatalf("read the archive: %v", err)
		}

		got = append(got, header)
	}

	if err := gz.Close(); err != nil {
		t.Fatalf("close the gzip stream: %v", err)
	}

	return got
}

// repoRoot walks up from the test's working directory to the directory holding
// go.mod, which for this module is the repository root and so the directory
// testdata/ sits in.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found above %q", dir)
		}

		dir = parent
	}
}
