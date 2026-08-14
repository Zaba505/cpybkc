// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// DigestExt is appended to a corpus directory's path to name the file its
// digest is published in: the corpus at `corpus/` is checked against
// `corpus.sha256`.
//
// Beside the corpus rather than inside it, and that is not a filing preference.
// A digest of a directory cannot live in that directory: it would have to cover
// itself. Putting it inside would mean naming one member every reader has to be
// told to leave out — which is a change to the corpus format
// (docs/conformance/SPEC.md, *An entry*) and a change [Load] would have to
// carry, for the sake of a file that is not part of the corpus but a statement
// about it.
const DigestExt = ".sha256"

// digestLen is how many characters a SHA-256 digest is written in.
const digestLen = sha256.Size * 2

// DigestPath is where the digest of the corpus at dir is published.
//
// A corpus named `.` — which is what `--corpus .` from inside an unpacked
// corpus means, and a plausible thing for somebody to type — has no name for a
// file to sit beside, and appending to it produces `..sha256`: a path nobody
// can have created, so the digest reads as absent and the run proceeds
// unchecked. Resolving against the working directory is what gives it one. It
// is done only in that case, because a relative path that already has a name
// reads far better in a diagnostic than the absolute one it would become.
func DigestPath(dir string) string {
	cleaned := filepath.Clean(dir)

	if base := filepath.Base(cleaned); base == "." || base == ".." {
		if absolute, err := filepath.Abs(cleaned); err == nil {
			cleaned = absolute
		}
	}

	return cleaned + DigestExt
}

// Digest reduces the corpus at dir to one SHA-256, written lowercase
// hexadecimal.
//
// # The rule, so that a third party can recompute it
//
// Every regular file under dir contributes, in ascending order of its
// slash-separated path relative to dir. For each one the hash is fed
//
//	the path, a zero byte, the length in decimal, a zero byte, then the bytes
//
// and the digest is the sum. Directories contribute nothing of their own, so an
// empty one is not part of a corpus; anything that is not a regular file is an
// error rather than something to skip, because a symbolic link in a published
// corpus is a file whose content depends on where it was unpacked.
//
// The length is in there because a corpus holds arbitrary bytes — `input.bin`
// is bytes by definition — so the contents cannot be their own delimiter.
// Without it two different corpora whose concatenations happened to coincide
// would agree, which is the one property a digest exists to deny. The path goes
// in for the same reason: a file that moved between entries is a different
// corpus, and a digest over contents alone would not say so.
//
// Nothing about a *file* other than its path and its bytes contributes. A
// corpus is text and bytes an author wrote down, so a mode, an owner or a
// modification time here would make the digest a function of the unpacking as
// well as of the corpus — and the same archive unpacked twice would fail its own
// check.
func Digest(dir string) (string, error) {
	digest, err := DigestFS(os.DirFS(dir))
	if err != nil {
		return "", fmt.Errorf("failed to digest the conformance corpus at %s: %w", dir, err)
	}

	return digest, nil
}

// DigestFS is [Digest] over an [io/fs.FS].
//
// It is exported for the producer's sake rather than for testing. The tool that
// writes cpybkc-conformance.tar.gz has to digest exactly the tree it is
// archiving, and a second walk of the same directory is a second read that a
// concurrent edit can make disagree with the first — which would publish a
// digest for a corpus the archive does not hold. One filesystem, archived and
// digested, is what makes the two the same corpus by construction.
func DigestFS(corpus fs.FS) (string, error) {
	names, err := digestFiles(corpus)
	if err != nil {
		return "", err
	}

	if len(names) == 0 {
		return "", fmt.Errorf("it holds no file, and a digest of nothing would certify an empty download")
	}

	sum := sha256.New()

	for _, name := range names {
		b, err := fs.ReadFile(corpus, name)
		if err != nil {
			return "", fmt.Errorf("failed to read %s: %w", name, err)
		}

		// Writing to a hash cannot fail — sha256's Write never returns an error
		// — so the errors are dropped rather than checked into noise.
		_, _ = sum.Write([]byte(name))
		_, _ = sum.Write([]byte{0})
		_, _ = sum.Write([]byte(strconv.Itoa(len(b))))
		_, _ = sum.Write([]byte{0})
		_, _ = sum.Write(b)
	}

	return hex.EncodeToString(sum.Sum(nil)), nil
}

// digestFiles returns every regular file under corpus, as slash-separated paths
// relative to it, in sorted order.
//
// The sort is load-bearing and is not a belt-and-braces repeat of the walk.
// [io/fs.WalkDir] orders each *directory's* entries, which is a different order
// from sorting the full paths: beside a directory `a/` holding `b.txt`, a file
// `a.txt` is visited second, because the walk compares `a` against `a.txt` —
// so the walk emits `a/b.txt` first while `.` (0x2E) sorting below `/` (0x2F)
// puts `a.txt` first. Only the sorted order is the one
// docs/conformance/SPEC.md's *The corpus digest* states, so removing this line
// would silently stop the digest being the published rule.
func digestFiles(corpus fs.FS) ([]string, error) {
	var names []string

	err := fs.WalkDir(corpus, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return nil
		}

		if !entry.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file, and a corpus is the files somebody wrote", name)
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

// FormatDigest is what a digest file holds: the digest and a newline, and
// nothing else.
//
// Deliberately not sha256sum's `<digest>  <name>` line. That format names a
// file, this digest covers a directory, and a line that looked like sha256sum's
// would invite `sha256sum -c` — which would check one file that is not there
// and report success for a corpus it never read.
func FormatDigest(digest string) []byte {
	return []byte(digest + "\n")
}

// ReadDigest reads a digest file and returns the digest it holds.
func ReadDigest(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	digest := strings.TrimSpace(string(b))

	if len(digest) != digestLen {
		return "", fmt.Errorf("%s holds %q, and a digest is %d hexadecimal characters", path, digest, digestLen)
	}

	if _, err := hex.DecodeString(digest); err != nil {
		return "", fmt.Errorf("%s holds %q, which is not hexadecimal", path, digest)
	}

	if strings.ToLower(digest) != digest {
		return "", fmt.Errorf("%s holds %q, and a digest is written in lowercase", path, digest)
	}

	return digest, nil
}

// CheckDigest digests the corpus at dir and holds it against the digest
// published beside it, at [DigestPath].
//
// The digest it computed comes back either way, so that a caller can report
// which corpus it ran against whether or not there was anything to check it
// against.
//
// checked is false where no digest file is there at all, and that is not an
// error. The corpus in this repository's own tree has none and cannot: the
// digest is a function of the corpus, so a committed copy would be a second
// statement of it for an entry to be added without — the same reason
// CONTRIBUTING.md, *The release artifacts*, gives for ir.binpb not being
// committed. A caller that ran against an unchecked corpus should say so rather
// than report a bare pass.
func CheckDigest(dir string) (string, bool, error) {
	computed, err := Digest(dir)
	if err != nil {
		return "", false, err
	}

	path := DigestPath(dir)

	published, err := ReadDigest(path)
	if os.IsNotExist(err) {
		return computed, false, nil
	}

	if err != nil {
		return computed, false, fmt.Errorf("failed to read the corpus digest: %w", err)
	}

	if published != computed {
		return computed, true, fmt.Errorf(
			"the corpus at %s digests to %s and %s says it should be %s: the download is incomplete, was "+
				"unpacked over something else, or has been edited — and a run against it would be a run "+
				"against a corpus nobody published",
			dir, computed, path, published)
	}

	return computed, true, nil
}
