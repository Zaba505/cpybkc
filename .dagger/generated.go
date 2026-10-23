// This file checks that the code `dagger generate` writes is committed as the
// pinned engine writes it.
//
// Both modules commit their generated bindings, their go.mod and go.sum, and
// their dagger-module.toml — and every one of those is codegen output on
// Dagger v1: `dagger generate` rewrites all of them. Before #429 nothing compared
// the committed files against what the engine would write. A stale binding
// failed only once it stopped compiling, and two of them were edited by hand
// (#414, #425) because the installed CLI could not regenerate a v0.21.8 tree at
// all. Generated is the comparison.
package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"dagger/cpybkc/internal/dagger"
)

// generatedSDK is the SDK dagger.toml installs to write both modules' code, by
// the name it is installed under there.
const generatedSDK = "go"

// generatedProbeFile is the binding Generated's proof makes stale. It is the
// root module's client for the companion, which is one of the two files #414 and
// #425 edited by hand — the exact failure the check exists for.
const generatedProbeFile = pipelineModuleDir + "/internal/dagger/companion.gen.go"

// Generated checks that every file `dagger generate` writes — both modules'
// bindings, their go.mod and go.sum, and their dagger-module.toml — is committed
// as v1.0.0-beta.15 writes it.
//
// It makes no comparison of its own. The Go SDK that dagger.toml installs
// declares a staleness check, dagger-go-sdk's `stale`, which regenerates every
// module and client the SDK manages and fails when the committed tree differs.
// That was measured on beta.15 against this tree before it was relied on
// (#429): a binding with a line appended, a go.mod with a require removed, a
// dagger-module.toml with a key restored that codegen drops, and a companion
// function added without regenerating the root's client each turned it red. So
// this runs that check rather than a second sweep that would have to agree
// with it.
//
// What it adds is the proof that a green run means something. Alongside the
// real run it makes the root module's client for the companion stale on
// purpose, in a copy of the workspace that is never written back, and runs the
// same check over that; unless the copy fails, so does this. A staleness check
// that cannot fail looks exactly like a tree that is never stale, and the
// generated files are the ones nobody reads.
//
// On a stale tree it also lists the paths regeneration would change, which the
// SDK's own message does not.
//
// callingWorkspace is the workspace the call was made in, and the Dagger CLI
// fills it in; it is not called "workspace" because --workspace is one of the
// CLI's own global flags. It is never cached: the workspace is a live view of
// the checkout rather than a snapshot a cache key can describe, so a cached pass
// would be a pass for a tree this never looked at.
//
// +check
// +cache="never"
func (m *Cpybkc) Generated(ctx context.Context, callingWorkspace *dagger.Workspace) error {
	if callingWorkspace == nil {
		return errors.New("no workspace to check: the Dagger CLI fills one in from the checkout it runs in, so a caller from another module has to pass the one it was handed")
	}

	module, err := sdkModule(ctx, callingWorkspace, generatedSDK)
	if err != nil {
		return err
	}

	contents, err := callingWorkspace.File("/" + generatedProbeFile).Contents(ctx)
	if err != nil {
		return fmt.Errorf("reading %s for the stale-binding proof: %w", generatedProbeFile, err)
	}
	stale := callingWorkspace.WithNewFile("/"+generatedProbeFile, contents+"\n// Generated: deliberately stale\n")

	var real, proof []string
	var realErr, proofErr error

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		real, realErr = failingChecks(ctx, callingWorkspace, module)
	}()

	go func() {
		defer wg.Done()
		proof, proofErr = failingChecks(ctx, stale, module)
	}()

	wg.Wait()

	if err := errors.Join(realErr, proofErr); err != nil {
		return err
	}

	if len(real) > 0 {
		return staleError(ctx, callingWorkspace, real)
	}

	if len(proof) == 0 {
		return fmt.Errorf("a workspace whose %s was made stale on purpose passed every check of %q, so a green staleness check verifies nothing", generatedProbeFile, module)
	}

	return nil
}

// staleError is Generated's failure on a stale tree: the SDK check's own
// messages, the paths regeneration would change, and the fix.
func staleError(ctx context.Context, ws *dagger.Workspace, failures []string) error {
	msg := fmt.Sprintf("committed codegen is stale:\n  %s", strings.Join(failures, "\n  "))

	if paths, err := regeneratedPaths(ctx, ws); err != nil {
		msg += fmt.Sprintf("\n(could not list the files regeneration changes: %v)", err)
	} else if len(paths) > 0 {
		msg += fmt.Sprintf("\nregeneration changes:\n  %s", strings.Join(paths, "\n  "))
	}

	return errors.New(msg + "\nrun `dagger generate -y` and commit what it writes, dagger.lock included (CONTRIBUTING.md, \"After changing the module\")")
}

// regeneratedPaths lists the files the Go SDK's codegen would add, change or
// remove in ws, sorted.
func regeneratedPaths(ctx context.Context, ws *dagger.Workspace) ([]string, error) {
	changes := ws.SDK(generatedSDK).Generate()

	var paths []string
	for _, list := range []func(context.Context) ([]string, error){changes.AddedPaths, changes.ModifiedPaths, changes.RemovedPaths} {
		p, err := list(ctx)
		if err != nil {
			return nil, err
		}
		paths = append(paths, p...)
	}
	slices.Sort(paths)

	return paths, nil
}

// sdkModule is the name ws installs the module an SDK came from under, which is
// the name that SDK's checks are addressed by. It is looked up rather than
// written down because dagger.toml chooses it: `dagger workspace migrate` called
// the Go SDK's module dagger-go-sdk, and nothing stops the next migration
// calling it something else.
func sdkModule(ctx context.Context, ws *dagger.Workspace, sdk string) (string, error) {
	ref, err := ws.SDK(sdk).Ref(ctx)
	if err != nil {
		return "", fmt.Errorf("reading the module the %s SDK is installed from: %w", sdk, err)
	}

	modules, err := ws.Modules(ctx)
	if err != nil {
		return "", fmt.Errorf("listing the modules the workspace installs: %w", err)
	}

	for i := range modules {
		source, err := modules[i].Source(ctx)
		if err != nil {
			return "", fmt.Errorf("reading an installed module's source: %w", err)
		}
		if source == ref {
			return modules[i].Name(ctx)
		}
	}

	return "", fmt.Errorf("no module installed in dagger.toml is the %s SDK's own %q, so there is no staleness check to run", sdk, ref)
}

// failingChecks runs every check module declares in ws and returns the message
// of each that fails. A module that declares no checks is an error rather than
// a pass: there is then nothing whose passing could prove anything.
func failingChecks(ctx context.Context, ws *dagger.Workspace, module string) ([]string, error) {
	checks, err := ws.Artifacts(dagger.WorkspaceArtifactsOpts{Include: []string{module}}).
		FilterTypes([]string{"Check"}).
		AsChecks(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing the checks of %q: %w", module, err)
	}
	if len(checks) == 0 {
		return nil, fmt.Errorf("%q declares no checks, so there is no staleness check to run", module)
	}

	var failed []string
	for i := range checks {
		// Error is selected once and Message read off that result: selecting
		// Error twice would run the check twice.
		failure, err := checks[i].Error(ctx)
		if err != nil {
			return nil, fmt.Errorf("running a check of %q: %w", module, err)
		}
		if failure == nil {
			continue
		}
		msg, err := failure.Message(ctx)
		if err != nil {
			return nil, fmt.Errorf("reading a failing check's message: %w", err)
		}
		failed = append(failed, msg)
	}

	return failed, nil
}
