// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutschema

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/schema"
)

// Published is the schema this binary was built with: the bytes of
// [SchemaFile], embedded, which are the bytes a release publishes as
// layout-schema.sexpr.
//
// It is loaded once per process and shared, which [Schema]'s immutability is
// what makes safe. An error is a binary built from a schema this package will
// not load; the Go tests and the release tool both refuse such a schema before
// it can be built into anything, so a caller reports it rather than recovering
// from it.
func Published() (*Schema, error) { return published() }

var published = sync.OnceValues(func() (*Schema, error) {
	s, err := Load(strings.NewReader(schema.Layout()))
	if err != nil {
		return nil, fmt.Errorf("failed to load the embedded layout schema: %w", err)
	}

	return s, nil
})

// Error makes a diagnostic an error, so that a run can report the schema's
// faults through the same joined error every layer reader reports through.
//
// It renders exactly what [Diagnostic.String] does: a fault the schema found
// reads in the schema's words wherever it surfaces.
func (d Diagnostic) Error() string { return d.String() }

// Diagnostic is what the fault says, and where, in the shape
// [github.com/Zaba505/cpybkc/internal/diag] renders every fault in.
func (d Diagnostic) Diagnostic() diag.Diagnostic {
	return diag.Diagnostic{Message: d.Message, Spans: []diag.Span{d.Span}}
}

// Err joins diagnostics into one error, in the order they were found, or nil
// when there are none.
//
// Each stays assertable with [errors.As] against [Diagnostic], and
// [github.com/Zaba505/cpybkc/internal/diag.Diagnostics] walks every one of them.
func Err(diagnostics []Diagnostic) error {
	errs := make([]error, 0, len(diagnostics))

	for _, d := range diagnostics {
		errs = append(errs, d)
	}

	return errors.Join(errs...)
}
