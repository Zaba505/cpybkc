// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package plugin

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Prefix is what the name of every generator executable begins with.
//
// docs/plugin/SPEC.md: a generator's executable MUST be named
// `cpybkc-gen-<name>`, and the suffix of that filename is the generator's name.
// It is a constant because the same string is the file that is searched for and
// the file a diagnostic reports missing, and a message naming a filename cpybkc
// did not look for is worse than no message.
const Prefix = "cpybkc-gen-"

// Filename is the executable a generator name resolves to.
//
// It is the only direction that exists. Nothing inside an executable is
// consulted to discover its name, and nothing here reads a name back out of a
// filename: the manifest states the name, and the filename follows from it.
func Filename(name string) string { return Prefix + name }

// Resolve finds the executable name resolves to by searching searchPath for
// [Filename](name), and hands back the first one that is a regular file
// carrying an execute bit.
//
// searchPath is a PATH — the value of the environment variable, colon-separated
// on the POSIX hosts this project targets. It is passed rather than read, so
// that the search is a function of its arguments; see the package comment.
//
// Elements are searched in the order they are written and the earliest match
// wins. An empty element is skipped rather than treated as the working
// directory, which docs/plugin/SPEC.md requires; a relative element is searched
// where it says, against the working directory of the calling process.
//
// A name that cannot be a filename component is an [InvalidNameError] and is
// refused before anything is stat'd. A name nothing resolves is a
// [NotFoundError] naming the executable that was looked for, every directory it
// was looked for in, and any file of that name that was passed over.
func Resolve(name, searchPath string) (string, error) {
	if err := checkName(name); err != nil {
		return "", err
	}

	file := Filename(name)
	notFound := &NotFoundError{Name: name, File: file}

	for _, dir := range filepath.SplitList(searchPath) {
		// docs/plugin/SPEC.md: an empty PATH element MUST NOT be treated as the
		// current working directory, though POSIX permits it to be. It is not
		// recorded as searched either — a diagnostic listing an empty directory
		// would be reporting a place nobody named.
		if dir == "" {
			continue
		}

		notFound.Searched = append(notFound.Searched, dir)

		candidate := filepath.Join(dir, file)

		info, err := os.Stat(candidate)
		if err != nil {
			// Why the error is dropped: the reasons a stat fails here — the
			// file is not there, the directory is not searchable, the path is
			// too long — are one thing to a search that has to carry on either
			// way, and a shell resolving the same PATH does not distinguish
			// them either. What the adopter needs is the whole search's answer,
			// which [NotFoundError] carries.
			continue
		}

		if fault := unusable(info); fault != "" {
			notFound.PassedOver = append(notFound.PassedOver, PassedOver{Path: candidate, Fault: fault})

			continue
		}

		return candidate, nil
	}

	return "", notFound
}

// unusable says why info is not a candidate, or "" if it is one.
//
// docs/plugin/SPEC.md: a candidate MUST be a regular file carrying an execute
// bit, and there is no second test. The wording of each answer is what a
// diagnostic shows beside the file, so the three cases are three sentences
// rather than one with a hole in it — a directory named cpybkc-gen-go and a
// script somebody forgot to chmod are different mistakes with different fixes.
//
// The mode is read rather than the kernel asked. See the package comment,
// "Why not exec.LookPath".
func unusable(info fs.FileInfo) string {
	mode := info.Mode()

	switch {
	case mode.IsDir():
		return "it is a directory"
	case !mode.IsRegular():
		return "it is not a regular file"
	case mode.Perm()&0o111 == 0:
		return "it carries no execute bit"
	default:
		return ""
	}
}

// checkName refuses a name that cannot be a filename component.
func checkName(name string) error {
	if name == "" || strings.Contains(name, "/") {
		return &InvalidNameError{Name: name}
	}

	return nil
}
