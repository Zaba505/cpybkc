// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Command conformance-bundle writes the conformance archive — the published
// cpybkc-conformance.tar.gz — to a file.
//
// It is the fourth asset a release attaches, and the first that is not a
// description of an interface but a program somebody runs. It carries the
// corpus, a digest of the corpus, and cpybkc-conform built for every platform
// this repository builds it for, under one directory:
//
//	cpybkc-conformance/README.md
//	cpybkc-conformance/corpus.sha256
//	cpybkc-conformance/corpus/...
//	cpybkc-conformance/bin/cpybkc-conform-linux-amd64
//	cpybkc-conformance/bin/...
//
// # Why an archive of binaries, rather than an image
//
// A generator author checking their work has to get the engine and the corpus
// onto a machine, and until this existed the only route was cloning this
// repository. The natural next thought is a container image, and it fails a
// specific and common adopter: an engineer whose builders have no egress and
// whose images come from an internal mirror with an allowlist, where adding an
// external image is a ticket with a security review and a named internal owner.
// Conformance is what an outsider runs once, on a whim, before they are
// invested — the worst possible place to spend a procurement ticket, and a
// first-day bounce costs the adoption entirely (#202).
//
// So the offline path is a download and an `--exec`: no registry, no daemon and
// no image. A container door is the other door and is #203's, and it is where
// the properties that make a result believable live.
//
// # Why the binaries are in it
//
// The corpus alone is what a third party already had — a set of files with the
// right answer written down, and nothing that would ask their generator about
// it. What makes the archive worth downloading is the program on the other side
// of that question, and a program that has to be compiled first is one that
// needs a Go toolchain the adopter has no other reason to have.
//
// One archive rather than one per platform, because the whole point is that a
// person on a machine with no egress can be handed a file. Which platforms are
// in it is the pipeline's (.dagger/main.go, conformPlatforms), not this
// command's: it archives every regular file it is given under -bin, so a
// platform is added there and arrives here without this file changing.
//
// # Why the bytes are stable
//
// Every tar field the filesystem could have supplied — modification time,
// owner, group, and any permission beyond the two modes written here — is a
// constant, and the entries are emitted in sorted order rather than in the
// order a directory read happened to return. The gzip layer contributes no
// timestamp and no original file name for the same reason. What is left is a
// function of the paths and the contents, so two builds of one commit produce
// one archive, exactly as internal/tools/ir-protos does for the schema.
//
// That is a property of the *wrapper*. Whether the binaries inside are
// themselves reproducible is the Go toolchain's, and holds for the same reason
// the published CLI's does: they are built CGO-free and -trimpath by a
// container that pins its toolchain from go.mod.
//
// It lives under internal/ for the reason ir-protos does: nothing outside this
// repository has a reason to run it, and cmd/ is where a shipped command goes.
//
//	go run ./internal/tools/conformance-bundle -bin ./bin -o cpybkc-conformance.tar.gz
package main

import (
	"archive/tar"
	"compress/gzip"
	"embed"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Zaba505/cpybkc/internal/conformance"
)

// bundle is the archive's own content: what is in it that is neither the corpus
// nor a binary.
//
// A directory rather than a single embedded file, so that a second file arrives
// in the artifact by being added to it — the same reason
// internal/tools/ir-protos archives a tree rather than naming one .proto. It is
// committed rather than generated because it is prose somebody wrote, and the
// place to review it is a diff.
//
// The all: prefix is what makes that claim true rather than nearly true. A bare
// //go:embed of a directory silently drops every file whose name begins with a
// dot or an underscore, so a .well-known or a _template added here would be
// reviewed, committed, and then not be in the archive.
//
//go:embed all:bundle
var bundle embed.FS

const (
	// archiveRoot is the one directory everything unpacks into, so that the
	// archive can be opened anywhere without scattering files where it landed.
	// It is also what makes the documented invocation a cd and a command.
	archiveRoot = "cpybkc-conformance"

	// binDir is where the engines go inside the archive.
	binDir = "bin"

	// The two modes an entry carries. Nothing in here is anything else: the
	// corpus and the documentation are read, and an engine is run.
	fileMode = 0o644
	execMode = 0o755
)

// epoch is the modification time every entry carries.
//
// A constant rather than SOURCE_DATE_EPOCH, for the reason
// internal/tools/ir-protos gives: there is no timestamp in this artifact that
// means anything, and a build time recorded here would be the one field
// distinguishing two builds of the same commit.
var epoch = time.Unix(0, 0).UTC()

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "conformance-bundle: %v\n", err)
		os.Exit(1)
	}
}

// run is separated from main so that the exit path is the only thing main owns,
// and so that a test can drive the whole program without ending the test
// binary.
func run(args []string) error {
	flags := flag.NewFlagSet("conformance-bundle", flag.ContinueOnError)
	out := flags.String("o", "", "path to write the archive to (required)")
	corpus := flags.String("corpus", filepath.FromSlash(conformance.CorpusFile), "the corpus to publish")
	bin := flags.String("bin", "", "a directory of built cpybkc-conform executables (required)")

	if err := flags.Parse(args); err != nil {
		return err
	}

	if *out == "" {
		return fmt.Errorf("-o is required: name the file to write")
	}

	if *bin == "" {
		return fmt.Errorf("-bin is required: name the directory holding the built engines")
	}

	if rest := flags.Args(); len(rest) > 0 {
		return fmt.Errorf("unexpected arguments %v", rest)
	}

	// The parent directory is created rather than required, for the reason
	// ir-descriptor-set gives: the caller is a pipeline naming a path in a fresh
	// container filesystem.
	if dir := filepath.Dir(*out); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create %s: %w", dir, err)
		}
	}

	// Written beside the destination and renamed into place, so that a failure
	// part-way through leaves no half-written archive for a release job to
	// upload. ir-protos buys the same property by buffering in memory, which is
	// right for an artifact of kilobytes; this one carries an executable per
	// platform and is tens of megabytes, so it is streamed and the rename is
	// what makes the file appear whole or not at all.
	temporary := *out + ".part"

	file, err := os.Create(temporary)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", temporary, err)
	}

	if err := writeArchive(file, os.DirFS(*corpus), os.DirFS(*bin)); err != nil {
		_ = file.Close()
		_ = os.Remove(temporary)

		return err
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(temporary)

		return fmt.Errorf("failed to finish %s: %w", temporary, err)
	}

	if err := os.Rename(temporary, *out); err != nil {
		_ = os.Remove(temporary)

		return fmt.Errorf("failed to write %s: %w", *out, err)
	}

	return nil
}

// writeArchive writes the corpus, its digest, the embedded documentation and
// every engine to w as a gzipped tar.
//
// Both trees are [io/fs.FS] rather than directory names so that the determinism
// the package comment claims can be asserted against trees a test builds,
// rather than only against the ones on disk beside it.
func writeArchive(w io.Writer, corpus, engines fs.FS) error {
	documentation, err := fs.Sub(bundle, "bundle")
	if err != nil {
		return fmt.Errorf("failed to read the archive's own documentation: %w", err)
	}

	entries, err := contents(documentation, corpus, engines)
	if err != nil {
		return err
	}

	// BestCompression rather than the default, because the artifact is written
	// once per release and downloaded many times, and because a fixed level is
	// one more thing the output does not vary with.
	gz, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("failed to start the gzip stream: %w", err)
	}

	// Neither field is set from the artifact being written: a name here would
	// record whatever the caller passed to -o, and a modification time would be
	// the build's clock.
	gz.Name = ""
	gz.ModTime = time.Time{}

	archive := tar.NewWriter(gz)

	for _, entry := range entries {
		if err := writeEntry(archive, entry); err != nil {
			return err
		}
	}

	if err := archive.Close(); err != nil {
		return fmt.Errorf("failed to finish the archive: %w", err)
	}

	if err := gz.Close(); err != nil {
		return fmt.Errorf("failed to finish the gzip stream: %w", err)
	}

	return nil
}

// entry is one file of the archive: where it goes, how it is read, and whether
// it is run.
type entry struct {
	// name is the path inside the archive, under [archiveRoot], slash
	// separated.
	name string

	// mode is [fileMode] or [execMode].
	mode int64

	// read is the bytes, read when the entry is written rather than when the
	// listing is built, so that the whole archive is never held at once.
	read func() ([]byte, error)
}

// contents is every entry of the archive, in the order they are written.
//
// Sorted by the path inside the archive, so that the listing is a function of
// what goes in rather than of the order three trees were walked in. That is the
// same property internal/tools/ir-protos states of its own sort, and it is what
// makes the artifact comparable across releases at all.
// The documentation tree is a parameter rather than read from [bundle] here so
// that the collision check below can be exercised. It is the only one of the
// three whose contents this repository controls, so it is the only one a test
// can make collide with another.
func contents(documentation, corpus, engines fs.FS) ([]entry, error) {
	// The digest is taken over the same filesystem that is about to be
	// archived, and not over the directory again: two walks are two reads, and
	// an edit between them would publish a digest for a corpus the archive does
	// not hold.
	digest, err := conformance.DigestFS(corpus)
	if err != nil {
		return nil, fmt.Errorf("failed to digest the corpus: %w", err)
	}

	entries := []entry{{
		name: conformance.PublishedCorpusDir + conformance.DigestExt,
		mode: fileMode,
		read: func() ([]byte, error) { return conformance.FormatDigest(digest), nil },
	}}

	from := []struct {
		tree   fs.FS
		under  string
		mode   int64
		reason string
	}{
		{tree: documentation, under: "", mode: fileMode, reason: "the archive's own documentation"},
		{tree: corpus, under: conformance.PublishedCorpusDir, mode: fileMode, reason: "the corpus"},
		{tree: engines, under: binDir, mode: execMode, reason: "the engines"},
	}

	for _, source := range from {
		names, err := files(source.tree)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", source.reason, err)
		}

		if len(names) == 0 {
			// Named rather than tolerated. An archive missing its engines, its
			// corpus or its documentation is well-formed, uploads as happily as
			// a good one, and is discovered by whoever downloaded it.
			return nil, fmt.Errorf("%s is empty, and an archive without it is not the published artifact",
				source.reason)
		}

		for _, name := range names {
			entries = append(entries, entry{
				name: path.Join(source.under, name),
				mode: source.mode,
				read: func() ([]byte, error) { return fs.ReadFile(source.tree, name) },
			})
		}
	}

	slices.SortFunc(entries, func(a, b entry) int { return strings.Compare(a.name, b.name) })

	// A path contributed by two of the three trees would be written twice, and
	// what a consumer then has is decided by whichever entry their tar extracted
	// last. Nothing collides today — the documentation is at the root, the corpus
	// under corpus/ and the engines under bin/ — but that is a property of three
	// trees somebody can add a file to, and the way it would go wrong is a file
	// whose contents depend on the order it was unpacked in. Adjacent
	// comparison is enough because the entries are sorted.
	for i := 1; i < len(entries); i++ {
		if entries[i].name == entries[i-1].name {
			return nil, fmt.Errorf("%s would be archived twice, and a consumer's tar would keep whichever "+
				"copy it extracted last", entries[i].name)
		}
	}

	return entries, nil
}

// files returns every regular file under tree, as slash-separated paths
// relative to it, in sorted order.
//
// Anything that is not a regular file is an error rather than something to
// skip: a symbolic link in a published archive is a file whose content depends
// on where it was unpacked, which is the same rule the corpus digest holds
// itself to.
func files(tree fs.FS) ([]string, error) {
	var names []string

	err := fs.WalkDir(tree, ".", func(name string, item fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if item.IsDir() {
			return nil
		}

		if !item.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", name)
		}

		names = append(names, name)

		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.Sort(names)

	return names, nil
}

// writeEntry writes one file into the archive under a header carrying nothing
// the filesystem supplied.
//
// The header's Format is left unset, so the tar writer picks the narrowest
// encoding each header fits in — USTAR for the paths in here, PAX for one long
// enough to need it. That is a function of the header, so it costs nothing in
// determinism and it means a deeply nested corpus entry is archived rather than
// refused.
func writeEntry(archive *tar.Writer, item entry) error {
	b, err := item.read()
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", item.name, err)
	}

	header := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     path.Join(archiveRoot, item.name),
		Size:     int64(len(b)),
		Mode:     item.mode,
		ModTime:  epoch,
	}

	if err := archive.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write the header for %s: %w", header.Name, err)
	}

	if _, err := archive.Write(b); err != nil {
		return fmt.Errorf("failed to write %s: %w", header.Name, err)
	}

	return nil
}
