// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
)

// The tags this layer reads. Everything else at the top level belongs to another
// layer and is left alone.
const (
	tagEncoding         = "encoding"
	tagEncodingOverride = "encoding-override"
	tagItem             = "item"
)

// Profile is a layout's encoding layer: the one profile, and the overrides over
// it.
//
// There is exactly one of these per layout, and its [Axes] is always complete —
// a [Profile] handed back by [ReadProfile] states all four axes, because a
// layout stating three is an error rather than a profile with a hole in it.
//
// It is the whole of the layer. docs/layout/SPEC.md's "An override is per item,
// and there is no second profile" is explicit that overrides "are the whole of
// the mechanism: there is no second profile to inherit from, no per-record
// profile, and no profile named and referred to by name", so there is nothing
// here for a record to refer to and no name for it to refer to it by.
type Profile struct {
	// Pos is the `encoding` form.
	Pos layout.Pos

	// Axes is the four axes the profile states.
	Axes Axes

	// Overrides are the per-item overrides, in the order the layout writes
	// them. Each names a distinct item and states at least one axis.
	Overrides []Override
}

// Override is one `encoding-override`: an item, and the axes restated for it.
//
// The item **MAY** be a group, in which case the override reaches every
// elementary item under it, and **MAY** repeat, in which case it reaches every
// occurrence. Both of those need a copybook to act on and are `resolve`'s (#33);
// what is here is the statement.
type Override struct {
	// Pos is the `encoding-override` form.
	Pos layout.Pos

	// Item is what it applies to.
	Item ItemRef

	// Axes are the axes it restates. At least one is stated, and the ones that
	// are not leave the profile's alone — [Axes.Over] is that application.
	Axes Axes
}

// Applied is the encoding governing this override's item: the override over the
// profile, axis by axis.
//
// The result is complete, because the profile is.
func (p *Profile) Applied(o Override) Axes {
	return o.Axes.Over(p.Axes)
}

// ReadProfile reads the encoding layer out of a parsed layout.
//
// It reports every fault it finds, joined, and returns no profile when it found
// one: a profile missing an axis is the failure the whole layer exists to
// prevent, and handing one back with the axis defaulted or blank would leave a
// caller to notice.
//
// Top-level forms belonging to other layers are not read here and are not
// faults. A layout's forms are a set that refer to one another by name, and
// nothing orders them.
func ReadProfile(file *layout.File) (*Profile, error) {
	read := &profileReader{}
	profile := &Profile{Pos: file.Start()}

	// The forms are read in source order, so that a layout wrong in several
	// places is reported the way it is read. Every `encoding` form is read and
	// not only the one that will be kept: a layout carrying two malformed
	// profiles is told about both rather than about the count alone.
	var encodings []layout.Form

	for _, form := range file.Forms {
		switch form.Tag {
		case tagEncoding:
			encodings = append(encodings, form)

			axes, stated := read.axes(form, form.Elements, inProfile)

			// Only an axis the profile says nothing at all about is missing. An
			// axis stated with a value the axis does not admit has already been
			// reported against the value, and reporting it again as unstated
			// would name the same line twice and describe it wrongly the second
			// time.
			for _, axis := range allAxes {
				if _, ok := stated[axis]; !ok {
					read.Fail(&MissingAxisError{Pos: form.Pos, Axis: axis})
				}
			}

			if len(encodings) == 1 {
				profile.Pos, profile.Axes = form.Pos, axes
			}
		case tagEncodingOverride:
			read.override(profile, form)
		}
	}

	// The count is a fact about the layout rather than about any one form, so it
	// is reported after everything the forms themselves were wrong about.
	if len(encodings) != 1 {
		// The second form is what the diagnostic points at where there is one:
		// the first is a profile an adopter meant, and the second is the line
		// making it ambiguous.
		pos := file.Start()
		if len(encodings) > 1 {
			pos = encodings[1].Pos
		}

		read.Fail(&ProfileCountError{Pos: pos, Count: len(encodings)})
	}

	if read.Failed() {
		return nil, read.Err()
	}

	return profile, nil
}

// profileReader holds the state one [ReadProfile] accumulates: the faults found
// so far, and the items already overridden.
type profileReader struct {
	diag.List

	// overridden is where each item was first overridden, keyed by the
	// reference's identity. It is what makes a second override on one item
	// reportable against the first.
	overridden map[string]layout.Pos
}

// override reads one `encoding-override` and appends it to the profile.
func (r *profileReader) override(profile *Profile, form layout.Form) {
	if len(form.Elements) == 0 {
		r.Fail(&ItemReferenceError{Pos: form.Pos, Found: "an override naming no item at all"})

		return
	}

	item, err := readItemRef(form.Elements[0])
	if err != nil {
		r.Fail(err)

		// The axes are read anyway. An override whose reference is misspelled is
		// still an override, and a charset misspelled underneath it is a second
		// thing to fix rather than something to discover on the next run.
		_, _ = r.axes(form, form.Elements[1:], inOverride)

		return
	}

	if first, already := r.overridden[item.identity()]; already {
		r.Fail(&DuplicateOverrideError{Pos: form.Pos, First: first, Item: item})
	} else {
		if r.overridden == nil {
			r.overridden = make(map[string]layout.Pos)
		}

		r.overridden[item.identity()] = form.Pos
	}

	axes, stated := r.axes(form, form.Elements[1:], inOverride)

	// An override states an axis when it carries one of the four forms, whatever
	// the form turned out to say. One whose only axis was rejected on its own
	// account is not also an override that names no axis: the layout plainly
	// names one, and the fault is already reported against the value.
	if len(stated) == 0 {
		r.Fail(&EmptyOverrideError{Pos: form.Pos, Item: item})

		return
	}

	if len(axes.Stated()) == 0 {
		return
	}

	profile.Overrides = append(profile.Overrides, Override{Pos: form.Pos, Item: item, Axes: axes})
}

// axes reads the axis children of a form, whichever of the four are there.
//
// It returns what the form says and where it said each axis — including an axis
// whose value was rejected, which is stated and unusable rather than unstated.
// How many are required is the caller's to judge: `encoding` takes all four and
// `encoding-override` takes at least one, and neither rule is visible from the
// children alone.
//
// where says which of the two forms these children are under. What an axis
// admits depends on it — `(charset none)` is a statement about one item, and an
// override is the only form that names one — so the position travels down with
// the children rather than being rediscovered from the tag at the bottom.
func (r *profileReader) axes(form layout.Form, elements []layout.Node, where position) (Axes, map[Axis]layout.Pos) {
	var (
		axes  Axes
		first = make(map[Axis]layout.Pos)
	)

	for _, element := range elements {
		child, ok := element.(layout.Form)
		if !ok {
			r.Fail(&ChildError{Pos: element.Position(), Form: form.Tag, Found: describe(element), Admits: axisNames()})

			continue
		}

		axis, ok := lookupAxis(child.Tag)
		if !ok {
			r.Fail(&ChildError{Pos: child.TagPos, Form: form.Tag, Found: describe(child), Admits: axisNames()})

			continue
		}

		if pos, repeated := first[axis]; repeated {
			r.Fail(&RepeatedAxisError{Pos: child.Pos, First: pos, Axis: axis, Form: form.Tag})

			continue
		}

		first[axis] = child.Pos

		value, ok := r.axisValue(child, axis, where)
		if !ok {
			continue
		}

		axes.set(axis, value)
	}

	return axes, first
}

// axisValue reads the one symbol an axis form carries, holding it to the set the
// axis admits where it stands.
func (r *profileReader) axisValue(form layout.Form, axis Axis, where position) (string, bool) {
	if len(form.Elements) != 1 {
		found := "no value"
		if len(form.Elements) > 1 {
			found = "several"
		}

		r.Fail(&AxisFormError{Pos: form.Pos, Axis: axis, Found: found})

		return "", false
	}

	symbol, ok := form.Elements[0].(layout.Symbol)
	if !ok {
		r.Fail(&AxisFormError{Pos: form.Elements[0].Position(), Axis: axis, Found: describe(form.Elements[0])})

		return "", false
	}

	// `(charset none)` on the profile is a fault of its own rather than a
	// misspelling: the value is one the axis has, written where there is no item
	// for it to be about, and a message listing the code pages would send an
	// adopter looking for a typo in a word they spelled correctly.
	if where == inProfile && axis == AxisCharset && symbol.Value == string(None) {
		r.Fail(&ProfileCharsetNoneError{Pos: symbol.Pos})

		return "", false
	}

	if !axis.admits(symbol.Value, where) {
		r.Fail(&AxisValueError{Pos: symbol.Pos, Axis: axis, Value: symbol.Value, Position: where})

		return "", false
	}

	return symbol.Value, true
}

// lookupAxis resolves an axis from the tag a layout writes it as.
func lookupAxis(tag string) (Axis, bool) {
	for _, axis := range allAxes {
		if axis.String() == tag {
			return axis, true
		}
	}

	return 0, false
}
