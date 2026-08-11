// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package manifest

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/diag"
)

// refuse hands [Read] a manifest that has something wrong with it and gives back
// what the faults render as. It fails the test on a manifest that reads cleanly,
// because a rule nobody enforces passes a test that only ever asserts messages.
func refuse(t *testing.T, source string) string {
	t.Helper()

	m, err := Read(Name, strings.NewReader(source))
	if err == nil {
		t.Fatalf("the manifest read cleanly as %+v, want a fault", m)
	}

	if m != nil {
		t.Errorf("a refused manifest came back as %+v, want nothing", m)
	}

	return diag.Render(err)
}

// TestReadRefusesAManifestWithSomethingWrongWithIt is the golden set: one
// manifest per rule, and the diagnostic it produces in full.
//
// The whole rendering is asserted rather than a substring of it, because the
// position is half of what a diagnostic owes its reader and a message asserted
// by substring keeps passing while pointing at the wrong line.
func TestReadRefusesAManifestWithSomethingWrongWithIt(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "a field no manifest has",
			source: `{
  "layout": "orders.sexpr",
  "generators": [{"name": "go", "out": "gen"}],
  "generatorz": []
}`,
			want: `cpybkc.json:4:3: a manifest has no field named "generatorz"; it carries layout and generators`,
		},
		{
			name: "a field no generator entry has",
			source: `{
  "layout": "orders.sexpr",
  "generators": [
    {"name": "go", "out": "gen", "version": "v1"}
  ]
}`,
			want: `cpybkc.json:4:34: a generator entry has no field named "version"; ` +
				`it carries name, out and options`,
		},
		{
			name: "a field written twice",
			source: `{
  "layout": "orders.sexpr",
  "layout": "payments.sexpr",
  "generators": [{"name": "go", "out": "gen"}]
}`,
			want: `cpybkc.json:3:3: a manifest carries "layout" twice
  cpybkc.json:2:3: the first one is here`,
		},
		{
			name: "an option written twice",
			source: `{
  "layout": "orders.sexpr",
  "generators": [
    {"name": "go", "out": "gen", "options": {"package_name": "a", "package_name": "b"}}
  ]
}`,
			want: `cpybkc.json:4:67: a generator's option set carries "package_name" twice
  cpybkc.json:4:46: the first one is here`,
		},
		{
			name: "no layout",
			source: `{
  "generators": [{"name": "go", "out": "gen"}]
}`,
			want: `cpybkc.json:1:1: a manifest carries no layout; a project resolves its records against exactly one`,
		},
		{
			name:   "no generators",
			source: `{"layout": "orders.sexpr"}`,
			want: `cpybkc.json:1:1: a manifest carries no generators; ` +
				`there is nothing for a run to do without one`,
		},
		{
			name: "generators declared and empty",
			source: `{
  "layout": "orders.sexpr",
  "generators": []
}`,
			want: `cpybkc.json:3:17: generators is empty; there is nothing for a run to do without one`,
		},
		{
			name: "a generator with no name and no out",
			source: `{
  "layout": "orders.sexpr",
  "generators": [{"options": {"package_name": "orders"}}]
}`,
			want: `cpybkc.json:3:18: a generator entry carries no name; a generator is found on PATH as cpybkc-gen-<name>
cpybkc.json:3:18: a generator entry carries no out; its output lands in the directory out names`,
		},
		{
			name: "an empty layout",
			source: `{
  "layout": "",
  "generators": [{"name": "go", "out": "gen"}]
}`,
			want: `cpybkc.json:2:13: layout is empty; a project resolves its records against exactly one`,
		},
		{
			name: "an empty generator name",
			source: `{
  "layout": "orders.sexpr",
  "generators": [{"name": "", "out": "gen"}]
}`,
			want: `cpybkc.json:3:27: generators[0].name is empty; a generator is found on PATH as cpybkc-gen-<name>`,
		},
		{
			name: "a generator name that is not a filename component",
			source: `{
  "layout": "orders.sexpr",
  "generators": [{"name": "z5labs/go", "out": "gen"}]
}`,
			want: `cpybkc.json:3:27: a generator name is the suffix of the cpybkc-gen-<name> executable ` +
				`it resolves to, and "z5labs/go" contains a /`,
		},
		{
			name: "one generator where a list of them belongs",
			source: `{
  "layout": "orders.sexpr",
  "generators": {"name": "go", "out": "gen"}
}`,
			want: `cpybkc.json:3:17: generators is a list of generator entries, and this one is an object`,
		},
		{
			name: "a layout written as a number",
			source: `{
  "layout": 12,
  "generators": [{"name": "go", "out": "gen"}]
}`,
			want: `cpybkc.json:2:13: layout is the layout file written as text, and this one is a number`,
		},
		{
			name: "a generator entry that is not an object",
			source: `{
  "layout": "orders.sexpr",
  "generators": ["go"]
}`,
			want: `cpybkc.json:3:18: generators[0] is a generator entry, which is a JSON object, and this one is text`,
		},
		{
			name: "an option value that is not text",
			source: `{
  "layout": "orders.sexpr",
  "generators": [
    {"name": "go", "out": "gen", "options": {"width": 80}}
  ]
}`,
			want: `cpybkc.json:4:55: generators[0].options.width is an option value written as text, ` +
				`and this one is a number`,
		},
		{
			name: "an option key a generator could not be handed",
			source: `{
  "layout": "orders.sexpr",
  "generators": [
    {"name": "go", "out": "gen", "options": {"package=name": "orders"}}
  ]
}`,
			want: `cpybkc.json:4:46: an option key contains no =, and "package=name" does`,
		},
		{
			name: "an option key with nothing in it",
			source: `{
  "layout": "orders.sexpr",
  "generators": [
    {"name": "go", "out": "gen", "options": {"": "orders"}}
  ]
}`,
			want: `cpybkc.json:4:46: an option key is empty, and a generator is handed each option as k=v`,
		},
		{
			name: "a layout written as a boolean",
			source: `{
  "layout": true,
  "generators": [{"name": "go", "out": "gen"}]
}`,
			want: `cpybkc.json:2:13: layout is the layout file written as text, and this one is a boolean`,
		},
		{
			name: "generators written as nothing at all",
			source: `{
  "layout": "orders.sexpr",
  "generators": null
}`,
			want: `cpybkc.json:3:17: generators is a list of generator entries, and this one is null`,
		},
		{
			name:   "a manifest that is not an object",
			source: `["orders.sexpr"]`,
			want:   `cpybkc.json:1:1: a manifest is a JSON object, and this one is a list`,
		},
		{
			name:   "a manifest that is empty",
			source: "\n  \n",
			want: `cpybkc.json:1:1: the manifest is empty; ` +
				`it is a JSON object naming the layout and the generators to run`,
		},
		{
			name:   "a manifest that is not JSON",
			source: `{"layout" = "orders.sexpr"}`,
			want: `cpybkc.json:1:11: the manifest is not valid JSON: ` +
				`invalid character '=' after object key`,
		},
		{
			name: "a manifest that stops in the middle",
			source: `{
  "layout": "orders.sexpr",
  "generators": [{"name": "go",`,
			want: `cpybkc.json:3:32: the manifest ends before the JSON in it does`,
		},
		{
			name:   "more than one JSON value",
			source: `{"layout": "orders.sexpr", "generators": [{"name": "go", "out": "gen"}]} {}`,
			want: `cpybkc.json:1:74: the manifest carries an object after the object it opens with; ` +
				`a manifest is one JSON object and nothing else`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := refuse(t, test.source); got != test.want {
				t.Errorf("the manifest reads:\n%s\nwant:\n%s", got, test.want)
			}
		})
	}
}

// TestReadStopsWhereverTheJSONDoes is the walk's one fatal fault, met at every
// depth a manifest has: a file that ends mid-object leaves nothing after it to
// read, whether it stopped in a list of copybooks, in a generator's options or
// before the first field of all.
func TestReadStopsWhereverTheJSONDoes(t *testing.T) {
	tests := map[string]string{
		"before the first field":  `{`,
		"in the generators list":  `{"layout": "orders.sexpr", "generators": [`,
		"in a generator entry":    `{"layout": "orders.sexpr", "generators": [{`,
		"in a generator's option": `{"layout": "o.sexpr", "generators": [{"name": "go", "options": {"a":`,
		"in a field nobody knows": `{"generatorz": [1, [2,`,
	}

	const want = "the manifest ends before the JSON in it does"

	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			if got := refuse(t, source); !strings.HasSuffix(got, want) {
				t.Errorf("the manifest reads:\n%s\nwant it to end with %q", got, want)
			}
		})
	}
}

// TestReadReportsEveryFaultRatherThanTheFirst is why the faults accumulate: a
// manifest is wrong in the same way in several places at once, and a reader
// reporting one of them per run is a reader run once per fault.
func TestReadReportsEveryFaultRatherThanTheFirst(t *testing.T) {
	got := refuse(t, `{
  "input": "cpy/orders.cpy",
  "layout": "orders.sexpr",
  "generators": [
    {"name": "go", "outt": "gen"},
    {"name": "", "out": "schema"}
  ]
}`)

	want := `cpybkc.json:2:3: a manifest has no field named "input"; it carries layout and generators
cpybkc.json:5:20: a generator entry has no field named "outt"; it carries name, out and options
cpybkc.json:5:5: a generator entry carries no out; its output lands in the directory out names
cpybkc.json:6:14: generators[1].name is empty; a generator is found on PATH as cpybkc-gen-<name>`

	if got != want {
		t.Errorf("the manifest reads:\n%s\nwant:\n%s", got, want)
	}
}

// TestReadKeepsWhatItFoundBesideAFaultItCannotReadPast is the other half of
// that: malformed JSON ends the walk, because there is no way to know where the
// value that failed to parse was meant to end — but the faults already found
// are still the adopter's to fix.
func TestReadKeepsWhatItFoundBesideAFaultItCannotReadPast(t *testing.T) {
	got := refuse(t, `{
  "layoutt": "orders.sexpr",
  "generators": [{"name": "go", "out" = "gen"}]
}`)

	want := `cpybkc.json:2:3: a manifest has no field named "layoutt"; it carries layout and generators
cpybkc.json:3:39: the manifest is not valid JSON: invalid character '=' after object key`

	if got != want {
		t.Errorf("the manifest reads:\n%s\nwant:\n%s", got, want)
	}
}

// TestASyntaxFaultKeepsWhatEncodingJSONSaid is what makes a truncated manifest
// distinguishable from a malformed one without reading the sentence.
func TestASyntaxFaultKeepsWhatEncodingJSONSaid(t *testing.T) {
	_, truncated := Read(Name, strings.NewReader(`{"layout": "orders.sexpr"`))
	if !errors.Is(truncated, io.EOF) {
		t.Errorf("a truncated manifest gave %v, want it to carry io.EOF", truncated)
	}

	_, malformed := Read(Name, strings.NewReader(`{"layout" = "orders.sexpr"}`))

	var syntax *json.SyntaxError
	if !errors.As(malformed, &syntax) {
		t.Errorf("a malformed manifest gave %v, want it to carry a *json.SyntaxError", malformed)
	}
}

// TestAFaultIsAssertableByType is what a caller that wants to act on a fault
// rather than print it does, and it is why these are types rather than one
// error with a sentence in it.
func TestAFaultIsAssertableByType(t *testing.T) {
	_, err := Read(Name, strings.NewReader(`{
  "layout": "orders.sexpr",
  "generators": [{"name": "go", "out": "gen", "options": {"a=b": ""}}]
}`))

	var key *OptionKeyError
	if !errors.As(err, &key) {
		t.Fatalf("the manifest gave %v, want an *OptionKeyError", err)
	}

	if key.Key != "a=b" {
		t.Errorf("the fault names the key %q, want %q", key.Key, "a=b")
	}
}

// TestReadSkipsPastAFieldItCannotUse is what keeps a manifest with one bad field
// reporting the fields after it: a value of the wrong shape is read to its end
// rather than leaving the walk standing inside it.
func TestReadSkipsPastAFieldItCannotUse(t *testing.T) {
	got := refuse(t, `{
  "layoutt": {"first": ["cpy/orders.cpy"]},
  "layout": "orders.sexpr",
  "generators": [{"name": "go", "out": "gen"}],
  "generatorz": [[1, 2], {"a": {"b": []}}]
}`)

	want := `cpybkc.json:2:3: a manifest has no field named "layoutt"; it carries layout and generators
cpybkc.json:5:3: a manifest has no field named "generatorz"; it carries layout and generators`

	if got != want {
		t.Errorf("the manifest reads:\n%s\nwant:\n%s", got, want)
	}
}
