// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"errors"
	"strings"
	"testing"

	"github.com/Zaba505/cpybkc/internal/layout"
)

// readingOf is the whole pipeline a caller runs: parse the source, then read the
// `OCCURS DEPENDING ON` reading out of it.
func readingOf(t *testing.T, source string) (Reading, error) {
	t.Helper()

	file, err := layout.Parse("layout.sexpr", strings.NewReader(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	return ReadCopybookReading(file)
}

// TestBothReadingsAreRead is the closed set, each one written the way the
// compiler that produced the file spells it.
func TestBothReadingsAreRead(t *testing.T) {
	t.Parallel()

	for _, want := range []Reading{ODOSlide, NoODOSlide} {
		t.Run(string(want), func(t *testing.T) {
			t.Parallel()

			got, err := readingOf(t, "(copybook-reading\n  (occurs-depending-on "+string(want)+"))\n")
			if err != nil {
				t.Fatalf("reading: %v", err)
			}
			if got != want {
				t.Fatalf("the layout reads as %s, want %s", got, want)
			}
			if !got.Stated() {
				t.Error("a layout that states a reading reads as unstated")
			}
			if slides := got == ODOSlide; got.Slides() != slides {
				t.Errorf("%s slides: %v, want %v", got, got.Slides(), slides)
			}
		})
	}
}

// TestALayoutStatingNoReadingIsNotAFaultHere is the division of labour this
// reader is built on.
//
// Whether the layout owed a reading is a question about the copybooks it binds —
// only a record carrying the clause needs the answer — and this package never
// opens a copybook. So an absent form reads as unstated, and `resolve` is what
// refuses it against the table that needed it.
func TestALayoutStatingNoReadingIsNotAFaultHere(t *testing.T) {
	t.Parallel()

	got, err := readingOf(t, "(record ORDER-HEADER\n  (copybook \"orders.cpy\" ORDER-HEADER))\n")
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if got != ReadingUnstated {
		t.Fatalf("a layout carrying no copybook-reading form reads as %s, want unstated", got)
	}
	if got.Stated() {
		t.Error("an unstated reading reports itself stated")
	}
	if got.Slides() {
		t.Error("an unstated reading slides, which is one of the two readings taken by default")
	}
}

// TestASecondReadingIsReported: the form is one statement per layout because the
// reading is a property of the compiler that wrote the file rather than of any
// record in it, so a second one is not a refinement of the first.
func TestASecondReadingIsReported(t *testing.T) {
	t.Parallel()

	_, err := readingOf(t, `(copybook-reading
  (occurs-depending-on odoslide))
(copybook-reading
  (occurs-depending-on noodoslide))
`)

	var count *ReadingCountError
	if !errors.As(err, &count) {
		t.Fatalf("reading reported %v, want a ReadingCountError", err)
	}
	if count.Count != 2 {
		t.Errorf("the diagnostic counts %d forms, want 2", count.Count)
	}

	// The second is where the fault is, and the first is named because
	// deciding between them is what the adopter has to do.
	if count.Pos.Line != 3 || count.First.Line != 1 {
		t.Errorf("the diagnostic points at %s and %s, want line 3 and line 1", count.Pos, count.First)
	}
}

// TestAReadingOutsideTheSetIsReported names the whole set, because this is a
// spelling question an adopter answers from the message.
func TestAReadingOutsideTheSetIsReported(t *testing.T) {
	t.Parallel()

	_, err := readingOf(t, "(copybook-reading\n  (occurs-depending-on slide))\n")

	var value *ReadingValueError
	if !errors.As(err, &value) {
		t.Fatalf("reading reported %v, want a ReadingValueError", err)
	}
	for _, want := range []string{"odoslide", "noodoslide", `"slide"`, "layout.sexpr:2:24"} {
		if !strings.Contains(value.Error(), want) {
			t.Errorf("the diagnostic does not name %s: %s", want, value.Error())
		}
	}
}

// TestAMalformedReadingFormIsReported is the shortfall that is not a value with
// something wrong with it: a form stating nothing, and one stating both readings
// at once.
func TestAMalformedReadingFormIsReported(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		source string
		child  string
	}{
		{name: "no child", source: "(copybook-reading)\n", child: ""},
		{
			name:   "two children",
			source: "(copybook-reading\n  (occurs-depending-on odoslide)\n  (occurs-depending-on noodoslide))\n",
			child:  "",
		},
		{name: "no value", source: "(copybook-reading\n  (occurs-depending-on))\n", child: "occurs-depending-on"},
		{
			name:   "two values",
			source: "(copybook-reading\n  (occurs-depending-on odoslide noodoslide))\n",
			child:  "occurs-depending-on",
		},
		{name: "value is text", source: "(copybook-reading\n  (occurs-depending-on \"odoslide\"))\n", child: "occurs-depending-on"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reading, err := readingOf(t, tc.source)

			var form *ReadingFormError
			if !errors.As(err, &form) {
				t.Fatalf("reading reported %v, want a ReadingFormError", err)
			}
			if form.Child != tc.child {
				t.Errorf("the diagnostic is about child %q, want %q", form.Child, tc.child)
			}
			if reading != ReadingUnstated {
				t.Errorf("a rejected layout reads as %s, want unstated", reading)
			}
		})
	}
}

// TestAReadingWrittenAsSomethingOtherThanItsChildIsReported: the form takes one
// child, and what stands there is neither a value with something wrong with it
// nor a second `copybook-reading`.
func TestAReadingWrittenAsSomethingOtherThanItsChildIsReported(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"(copybook-reading odoslide)\n",
		"(copybook-reading\n  (odoslide))\n",
	} {
		_, err := readingOf(t, source)

		var child *ChildError
		if !errors.As(err, &child) {
			t.Fatalf("reading %q reported %v, want a ChildError", source, err)
		}
		if !strings.Contains(child.Error(), "occurs-depending-on") {
			t.Errorf("the diagnostic does not name what the form admits: %s", child.Error())
		}
	}
}

// TestReadingRendersUnstatedAsAWord: the zero value is a value rather than an
// absence, and a message quoting it says so rather than printing nothing.
func TestReadingRendersUnstatedAsAWord(t *testing.T) {
	t.Parallel()

	if got := ReadingUnstated.String(); got != "unstated" {
		t.Errorf("the unstated reading renders as %q, want %q", got, "unstated")
	}
	if got := ODOSlide.String(); got != "odoslide" {
		t.Errorf("the sliding reading renders as %q, want %q", got, "odoslide")
	}
}
