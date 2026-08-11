// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// Package manifest reads the cpybkc.json a project is driven by.
//
// A manifest names the layout a project resolves against and the generators to
// run over it. It is checked in beside them, so that generator selection and
// options are diffable, reviewable and the same on a laptop and in CI — which is
// the whole reason the file exists rather than the flags it would otherwise be:
//
//	{
//	  "layout": "orders.sexpr",
//	  "generators": [
//	    {
//	      "name": "go",
//	      "out": "gen",
//	      "options": {"package_name": "orders"}
//	    }
//	  ]
//	}
//
// # Why this is not a spec
//
// docs/plugin/SPEC.md's "The `cpybkc.json` project manifest" excludes it by
// name, on the grounds of a different audience: the manifest is what a *user*
// writes to drive the CLI, and a plugin never reads one — it receives the
// options the manifest selected, already resolved, on its command line. So the
// schema below is documented for the person who writes the file, in the
// README, and stays free to change without touching an interface a third party
// builds against.
//
// # What a manifest carries
//
// Two fields at the top level. `layout` names the layout file, is required, and
// there is one of it: docs/layout/SPEC.md is what the records of a run are
// framed by, and a project resolving against two of them is two runs.
// `generators` is the list of generators to run and carries at least one entry,
// because a manifest declaring none asks for nothing to happen.
//
// A generator entry carries `name` and `out`, both required, and `options`,
// optional. `name` is the whole of how a generator is identified — it resolves
// to the `cpybkc-gen-<name>` executable on PATH (#41) — so there is no source
// and no version beside it, and nothing in this package looks for the
// executable. `out` is where that generator's output lands, and what happens
// between a generator's scratch directory and that directory is #43's, #44's
// and #45's.
//
// A path is kept exactly as it was written. Resolving one — against the
// manifest's own directory, into the absolute path a plugin is handed
// (docs/plugin/SPEC.md, "Invocation") — belongs to the stage that opens the
// file, and a reader that resolved paths would put a second answer beside it.
//
// # Why a manifest names no copybook
//
// It used to carry an `inputs` list, at the top level and again on each
// generator entry, and #157 removed both. The reason is that neither could
// affect anything: a run's descriptor is assembled from the layout and the
// copybooks that layout's `record` forms name (docs/cli/SPEC.md, "Which
// descriptor is emitted"), and a generator is invoked with a descriptor, an
// output directory and its options and never with a copybook path at all
// (docs/plugin/SPEC.md, "Invocation"). A per-generator list therefore promised
// something the pipeline cannot do — two generators of one run reading two
// different sets of copybooks, and so being handed two different descriptors —
// and a top-level list restated what the layout already said, in a second file,
// against a second base directory.
//
// A manifest still carrying one is not special-cased: `inputs` is an unknown
// field, and [Read] reports it with the line and column it is at, which is the
// migration an adopter can act on rather than a field that quietly stopped
// meaning anything.
//
// # Why the options are a list and not a map
//
// docs/plugin/SPEC.md requires cpybkc to pass a generator's options "in the
// order the manifest declares them, so that the vector is a function of the
// manifest rather than of a map iteration". A Go map cannot hold that order, so
// [Generator.Options] is a slice in the order the file writes them, and the
// manifest is walked as a token stream rather than unmarshalled into a struct
// to keep it.
//
// An option value is text. An option reaches a generator as `k=v` on a command
// line, so the manifest writes the value the generator will actually see;
// accepting `80` and handing over `80` would make this package the place that
// decides how a number is spelled, which is a decision a generator's own
// vocabulary owns.
//
// # Why an unknown field is a fault
//
// A manifest is a file a **person** wrote, and an unknown field in one is a
// typo they want reported: a line that reads as configuration and does nothing
// is found by noticing that the output never changed. So every object here
// admits a closed set of fields and reports anything else by name, along with
// the fields it does admit.
//
// The contrast worth keeping in view is the IR descriptor, which takes the
// opposite rule — an unknown protobuf field is ignored (#17) — because that is a
// message a **program** wrote, where an unknown field is a newer producer. Same
// CLI, opposite rules, and the author is the reason.
//
// The same argument covers a field written twice, an option key written twice,
// and an empty string where a path or a name belongs: each is something the
// author did not mean, and none of them has a reading this package could choose
// on their behalf.
//
// # Why faults accumulate, and where they stop
//
// [Read] reports every fault it found rather than the first, joined with
// [errors.Join] through [github.com/Zaba505/cpybkc/internal/diag.List], because
// a manifest is wrong in the same way in several places at once and a reader
// that reports one fault per run is a reader run once per fault. Each carries a
// [github.com/Zaba505/cpybkc/internal/diag.Span] into the manifest, so a
// diagnostic points at the line and column an editor can open.
//
// Malformed JSON is the exception, and it stops the walk: there is no way to
// know where the value that failed to parse was meant to end, so everything
// after it would be a fault invented by the parser rather than found in the
// file. What was already collected is reported beside it.
package manifest
