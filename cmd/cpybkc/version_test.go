// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"strings"
	"testing"
)

// TestAnUnstampedBuildReportsTheDevelopmentVersion is docs/cli/SPEC.md's other
// half: `0.0.0-dev` for a build made outside a release.
//
// Asserted on the package variable's own value rather than through the line, so
// that it is about what a build nobody stamped carries — a `go build` from a
// checkout, and every stage of the pipeline that is not publishing a release.
// The literal is written out because it is the fact: derived from the variable
// it is checking, this test would pass on whatever the variable became.
func TestAnUnstampedBuildReportsTheDevelopmentVersion(t *testing.T) {
	t.Parallel()

	if got := reportedVersion(); got != "0.0.0-dev" {
		t.Errorf("an unstamped build reports %q, want %q: docs/cli/SPEC.md requires 0.0.0-dev for a build made "+
			"outside a release, and this build was not stamped", got, "0.0.0-dev")
	}
}

// TestTheReportedVersionDropsTheTagsLeadingV is the one difference between the
// version the pipeline stamps and the version the line carries.
//
// The pipeline states a version as an OCI image tag, `v0.2.0`, because that is
// what docs/container/SPEC.md's tag table publishes and what the shared
// archetype is handed. docs/cli/SPEC.md states it as a SemVer 2.0.0 string.
// SemVer has no `v`, so a released build printing the tag verbatim would be
// answering `--version` in the wrong vocabulary — and it is the vocabulary a
// script comparing two releases reads.
//
// The prerelease case is here because it is the one a release candidate takes,
// and the already-trimmed case because a stamp passed by hand is not obliged to
// look like a tag.
//
// Not parallel, here or below: the stamp is a package variable, so a test that
// moves it has to be one nothing else in the package is reading at the time.
// Go resumes a paused parallel test only once every serial test has returned,
// which is what makes a serial test that restores what it moved safe beside the
// parallel ones that read the same variable.
func TestTheReportedVersionDropsTheTagsLeadingV(t *testing.T) {
	for _, test := range []struct {
		name    string
		stamped string
		want    string
	}{
		{"a release", "v0.2.0", "0.2.0"},
		{"a release candidate", "v0.3.0-rc.1", "0.3.0-rc.1"},
		{"the development version the pipeline builds under", "v0.0.0-dev", "0.0.0-dev"},
		{"a version already in the reported form", "0.2.0", "0.2.0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			restore := version
			t.Cleanup(func() { version = restore })

			version = test.stamped

			if got := reportedVersion(); got != test.want {
				t.Errorf("a build stamped %q reports %q, want %q", test.stamped, got, test.want)
			}
		})
	}
}

// TestTheVersionLineCarriesTheReportedVersionAndNotTheStamp is the same rule
// seen from the line, which is the surface docs/cli/SPEC.md actually covers.
//
// [reportedVersion] being right is worth nothing if the line is composed from
// the variable beside it, and the two are one character apart — which is exactly
// the sort of edit that gets made while reading this file and not while reading
// its test.
func TestTheVersionLineCarriesTheReportedVersionAndNotTheStamp(t *testing.T) {
	restore := version
	t.Cleanup(func() { version = restore })

	version = "v9.8.7"

	line := versionLine()

	if !strings.Contains(line, " 9.8.7 ") {
		t.Errorf("a build stamped %q wrote %q, and the line carries the version without the tag's `v`", version, line)
	}

	if strings.Contains(line, "v9.8.7") {
		t.Errorf("a build stamped %q wrote %q, which is the image tag rather than the SemVer string "+
			"docs/cli/SPEC.md requires", version, line)
	}
}
