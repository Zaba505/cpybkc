// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
)

// The tags this layer reads. `framing` is the top-level form; the rest are its
// children and are not read anywhere else.
const (
	tagFraming    = "framing"
	tagRECFM      = "recfm"
	tagLRECL      = "lrecl"
	tagBLKSIZE    = "blksize"
	tagBlocks     = "blocks"
	tagMaxSegment = "max-segment"
	tagDelimiter  = "delimiter"
	tagPlacement  = "placement"
)

// framingChildren is what a `framing` form admits, in the order
// docs/layout/SPEC.md's table states them, which is also the order the
// conditional rules are checked and reported in.
var framingChildren = []string{
	tagRECFM,
	tagLRECL,
	tagBLKSIZE,
	tagBlocks,
	tagMaxSegment,
	tagDelimiter,
	tagPlacement,
}

// blockDescriptorWord is the width in bytes of the block descriptor word a
// blocked variable-length dataset carries.
//
// docs/layout/SPEC.md states the rule it is used in — a `blksize` under `VB` and
// `VBS` "MUST be at least `lrecl` plus the width of a block descriptor word" —
// and explicitly does not restate the width, "which DFSMS defines". DFSMS is a
// governing source of both specs, the width comes with the definition, and this
// is the one place in the repository it is written down.
const blockDescriptorWord = 4

// FramingKind is one of the four framings a file node carries: what a *consumer*
// does, as against the RECFM letters an adopter writes in JCL.
//
// The set is closed and is docs/ir/SPEC.md's "Four framings, and none of them is
// a RECFM" unchanged; this type is that set as data, and [RECFM.Framing] is the
// mapping from the adopter's spelling onto it. Nothing here adds a member: a
// framing added to the IR is a breaking change there, and a second reading of
// the set here would be a second answer to what a consumer has to implement.
//
// The zero value is not a framing. A [Framing] handed back by [ReadFraming]
// always resolves to one, because the one spelling that resolves to nothing —
// RECFM U — never survives the read.
type FramingKind string

// The four framings, in docs/ir/SPEC.md's order.
const (
	// Unframed is a record at its extent, beginning at the byte after the
	// record before it. It is what RECFM F and FB resolve to and what nothing
	// else does, which is why a rule about a fixed-length dataset is keyed on
	// it — see [Framing.LRECLBound].
	Unframed FramingKind = "unframed"

	// DescriptorWord is a record at its extent, preceded by a record descriptor
	// word. RECFM V and VB resolve to it.
	DescriptorWord FramingKind = "descriptor-word"

	// Segmented is a record split across segments, each preceded by a segment
	// descriptor word. RECFM VBS resolves to it, and it is the one framing that
	// carries a number: the largest segment a writer may emit, which is
	// [Framing.MaxSegment].
	Segmented FramingKind = "segmented"

	// Delimited is a record at its extent with the delimiter around it, as the
	// placement says. A line-sequential file resolves to it.
	//
	// A delimited file whose records stop *before* their extent — the trailing
	// spaces GnuCOBOL's and Micro Focus's line-sequential organisations drop on
	// the way out — is not describable, and there is deliberately no spelling
	// for one in a layout. docs/ir/SPEC.md's "A record shorter than its extent"
	// puts that diagnostic on the consumer, which reports a delimiter found
	// before the record's extent as malformed data and must not accept the
	// short record by filling in the bytes it never reached (#52). There is
	// nothing for this layer to reject, because there is nothing an adopter can
	// write here that claims it.
	Delimited FramingKind = "delimited"
)

// RECFM is a dataset's record format in the adopter's own spelling — the letters
// out of the JCL that allocated the file, plus the one name for the file that
// has no RECFM at all.
//
// docs/layout/SPEC.md's "The adopter's spelling, and what each one resolves to"
// is why the format keeps these rather than the framings: they are what an
// adopter can look up. [RECFM.Framing] is the other half of that sentence.
type RECFM string

// The record formats a `recfm` admits, in docs/layout/SPEC.md's order.
const (
	// RECFMF is fixed-length, unblocked.
	RECFMF RECFM = "F"

	// RECFMFB is fixed-length, blocked.
	RECFMFB RECFM = "FB"

	// RECFMV is variable-length, unblocked.
	RECFMV RECFM = "V"

	// RECFMVB is variable-length, blocked.
	RECFMVB RECFM = "VB"

	// RECFMVBS is variable-length, blocked and spanned.
	RECFMVBS RECFM = "VBS"

	// RECFMU is undefined-length, and is admitted as a spelling in order to be
	// rejected by name: an [UndefinedLengthError] rather than a value nobody
	// recognises. A format with no spelling for U would leave the adopter who
	// has one describing their file as something else, and finding out at the
	// first record.
	RECFMU RECFM = "U"

	// LineSequential is the file that has no RECFM: a delimiter behind each
	// record and nothing in front, which is what GnuCOBOL and Micro Focus write
	// and what their documentation confusingly also calls RECFM=V. A layout
	// says which of the two it has by which spelling it writes, and neither
	// vendor is the default.
	LineSequential RECFM = "line-sequential"
)

// recfms is the set a `recfm` admits, in the order every message listing them
// renders them.
var recfms = []RECFM{RECFMF, RECFMFB, RECFMV, RECFMVB, RECFMVBS, RECFMU, LineSequential}

// Framing resolves the record format onto the framing a consumer implements.
//
// The mapping is docs/ir/SPEC.md's, read from the layout side, and it is the one
// statement of it here. RECFM U resolves to nothing, which is what the false
// reports: it names a dataset whose record extents are not in the byte stream
// at all.
func (r RECFM) Framing() (FramingKind, bool) {
	switch r {
	case RECFMF, RECFMFB:
		return Unframed, true
	case RECFMV, RECFMVB:
		return DescriptorWord, true
	case RECFMVBS:
		return Segmented, true
	case LineSequential:
		return Delimited, true
	default:
		return "", false
	}
}

// Blocks says whether the byte stream still carries block descriptor words.
type Blocks string

// The two spellings `blocks` admits.
const (
	// Deblocked is a run of records with no block descriptor words, which is
	// what a transfer that preserves record boundaries produces and what the
	// absence of a `blocks` child means.
	Deblocked Blocks = "deblocked"

	// InStream is a dataset image rather than a record stream, and is admitted
	// on the same terms as [RECFMU]: as a spelling to be rejected by name, so
	// that an adopter whose transfer preserved the blocks is told what is wrong
	// with their file rather than misreading it from the first byte.
	InStream Blocks = "in-stream"
)

// blocksSpellings is the set `blocks` admits, in docs/layout/SPEC.md's order.
var blocksSpellings = []Blocks{Deblocked, InStream}

// Placement is where a delimited file's delimiter sits relative to its records.
//
// The three are docs/ir/SPEC.md's "Terminator, separator and the last record"
// unchanged. None of them is a default: the distinction is what makes the end of
// a file checkable, and an adopter who guesses gives up a diagnostic without
// being told.
type Placement string

// The three placements a `placement` admits.
const (
	// Terminator is a delimiter after every record including the last.
	Terminator Placement = "terminator"

	// Separator is a delimiter between two records and none after the last.
	Separator Placement = "separator"

	// OptionalTerminator is a delimiter after every record except that the file
	// may end without one.
	OptionalTerminator Placement = "optional-terminator"
)

// placements is the set `placement` admits, in docs/layout/SPEC.md's order.
var placements = []Placement{Terminator, Separator, OptionalTerminator}

// Size is a number of bytes a framing states, or nothing.
//
// The zero value states nothing, which is what [Size.Stated] reports. Every
// position in the format that takes one takes a *positive* number, so zero is
// not a size an adopter can write and is free to mean "nobody said".
type Size struct {
	// Pos is the number itself, which is what a diagnostic about its value
	// points at.
	Pos layout.Pos

	// Value is how many bytes.
	Value int64
}

// Stated reports whether a size was written at all.
func (s Size) Stated() bool { return s.Value != 0 }

// LRECLBound is what a stated `lrecl` requires of a record type's extent.
//
// The two are not interchangeable and the difference is the whole reason this is
// a type rather than a number beside one: under a fixed-length dataset the next
// record begins at that distance whatever the record was, and under a
// variable-length one the record's own descriptor word states its length.
type LRECLBound int

// The bounds, and the absence of one.
const (
	// LRECLUnstated is a framing that states no `lrecl`. There is nothing to
	// check a record type against, which is a layout that is still readable
	// everywhere `lrecl` is optional.
	LRECLUnstated LRECLBound = iota

	// LRECLExact is `lrecl` under **unframed**: every record type accounts for
	// all of it. A record type whose items stop short carries the difference as
	// slack (#34), and one that does not describes a file whose reader
	// misaligns after the first record with nothing in the file to disagree
	// with it.
	LRECLExact

	// LRECLMaximum is `lrecl` under **descriptor-word** and **segmented**: a
	// record type's greatest extent is at most this. It is a maximum rather
	// than a requirement because each record states its own length, and the
	// check is worth having anyway, because a maximum the copybooks exceed is a
	// copybook bound to the wrong dataset.
	LRECLMaximum
)

// String names the bound the way a message about one does.
func (b LRECLBound) String() string {
	switch b {
	case LRECLUnstated:
		return "unstated"
	case LRECLExact:
		return "exact"
	case LRECLMaximum:
		return "maximum"
	default:
		return fmt.Sprintf("LRECLBound(%d)", int(b))
	}
}

// Framing is a layout's physical framing layer: the one `framing` form, read.
//
// There is exactly one of these per layout. What it states is the half of
// physical framing that stays on the adopter's side of `resolve` — the RECFM
// spelling out of their JCL, the LRECL and BLKSIZE beside it, and the delimiter
// and placement of a file that has no RECFM at all. What a consumer does with it
// is the IR's four framings, and [Framing.Kind] is the mapping.
//
// A [Framing] handed back by [ReadFraming] is one the format admits: its
// [Framing.Kind] is one of the four, every child its record format requires is
// stated, and no child its record format refuses is. What is *not* answered here
// is anything needing a copybook — the extents `lrecl` is checked against are
// `resolve`'s, per docs/layout/SPEC.md's "`resolve` checks each record type's
// extent against `lrecl` and reports the difference it cannot account for"
// (#33–#35). [Framing.LRECLBound] is what that check is told.
type Framing struct {
	// Pos is the `framing` form.
	Pos layout.Pos

	// RECFM is the record format the layout writes.
	RECFM RECFM

	// LRECL is the dataset's logical record length, or the zero [Size] where
	// the layout states none. What it requires is [Framing.LRECLBound].
	LRECL Size

	// BlockSize is the dataset's block size, or the zero [Size]. It is checked
	// against [Framing.LRECL] and the record format here and then reaches no IR
	// node, because the stream carries no blocks for a size to describe.
	BlockSize Size

	// Blocks is always [Deblocked] on a value handed back: [InStream] is
	// rejected, and the absence of a `blocks` child means the same thing as
	// writing `deblocked`.
	Blocks Blocks

	// MaxSegment is the largest segment a writer may emit. It is stated under
	// RECFM VBS and nowhere else, and it is the one framing number that is not
	// a check: it reaches the IR's **segmented** framing and a writer obeys it.
	// A reader has no use for it, since every segment states its own length.
	MaxSegment Size

	// Delimiter is the bytes around a delimited file's records, or the zero
	// [ByteString] under every other record format.
	Delimiter ByteString

	// Placement is where those bytes sit, or the empty string under every other
	// record format.
	Placement Placement
}

// Kind is the framing a consumer implements for this dataset.
//
// It is derived rather than stored so that the record format and the framing
// cannot disagree, and it is total on a value [ReadFraming] handed back: the one
// spelling that resolves to no framing is rejected while the layout is read.
func (f *Framing) Kind() FramingKind {
	kind, _ := f.RECFM.Framing()

	return kind
}

// LRECLBound is what this framing's `lrecl` requires of a record type's extent,
// and [LRECLUnstated] where the layout states none.
//
// It is the layout-side half of a check `resolve` performs, and the half that
// can be answered without a copybook. The other half needs one and is not this
// package's:
//
//   - Under [LRECLExact] every record type accounts for all of LRECL, with the
//     difference carried as slack (#34).
//   - A record type resolved under the non-sliding reading of `OCCURS DEPENDING
//     ON` has a constant extent — its table resolves to a constant repetition
//     of the copybook's declared maximum — so it meets [LRECLExact] exactly as
//     a record type with no table does (#87). Which reading applies is the
//     `copybook-reading` form's, a record-definitions statement rather than a
//     framing one.
//   - A record type whose extent really is data-dependent has no single number
//     of bytes to pad and does not fit an [Unframed] dataset at all; `resolve`
//     rejects it, naming the record and the repeating item (#92). It is not
//     validated against a maximum extent, because under the sliding reading it
//     has none — which is why [LRECLExact] and [LRECLMaximum] are different
//     answers rather than one number with a comparison to pick.
func (f *Framing) LRECLBound() LRECLBound {
	if !f.LRECL.Stated() {
		return LRECLUnstated
	}

	if f.Kind() == Unframed {
		return LRECLExact
	}

	return LRECLMaximum
}

// ReadFraming reads the physical framing layer out of a parsed layout.
//
// It reports every fault it finds, joined, and returns no framing when it found
// one, for [ReadProfile]'s reason: a framing an adopter cannot act on is worse
// than none, and a half-built one would leave a caller to notice.
//
// Top-level forms belonging to other layers are not read here and are not
// faults.
func ReadFraming(file *layout.File) (*Framing, error) {
	read := &framingReader{}
	framing := &Framing{Pos: file.Start(), Blocks: Deblocked}

	// Every `framing` form is read and not only the one that will be kept, so
	// that a layout carrying two malformed framings is told about both rather
	// than about the count alone.
	var forms []layout.Form

	for _, form := range file.Forms {
		if form.Tag != tagFraming {
			continue
		}

		forms = append(forms, form)

		one := read.framing(form)
		if len(forms) == 1 {
			framing = one
		}
	}

	// The count is a fact about the layout rather than about any one form, so it
	// is reported after everything the forms themselves were wrong about.
	if len(forms) != 1 {
		// The second form is what the diagnostic points at where there is one:
		// the first is a framing an adopter meant, and the second is the line
		// making it ambiguous.
		pos := file.Start()
		if len(forms) > 1 {
			pos = forms[1].Pos
		}

		read.Fail(&FramingCountError{Pos: pos, Count: len(forms)})
	}

	if read.Failed() {
		return nil, read.Err()
	}

	return framing, nil
}

// framingReader holds the state one [ReadFraming] accumulates.
type framingReader struct {
	diag.List
}

// framing reads one `framing` form.
func (r *framingReader) framing(form layout.Form) *Framing {
	framing := &Framing{Pos: form.Pos, Blocks: Deblocked}

	var (
		// stated is where each child was written, which makes a repeated one
		// reportable against the first and a conditional rule answerable.
		stated = make(map[string]layout.Pos)

		// usable is the children whose value was read. A child written with a
		// value nothing admits is stated and unusable, and a rule keyed on that
		// value has nothing to say about it.
		usable = make(map[string]bool)
	)

	for _, element := range form.Elements {
		child, ok := element.(layout.Form)
		if !ok {
			r.Fail(&ChildError{Pos: element.Position(), Form: form.Tag, Found: describe(element), Admits: framingChildren})

			continue
		}

		if !slices.Contains(framingChildren, child.Tag) {
			r.Fail(&ChildError{Pos: child.TagPos, Form: form.Tag, Found: describe(child), Admits: framingChildren})

			continue
		}

		if pos, repeated := stated[child.Tag]; repeated {
			r.Fail(&RepeatedChildError{Pos: child.Pos, First: pos, Child: child.Tag, Form: form.Tag})

			continue
		}

		stated[child.Tag] = child.Pos

		if r.value(framing, child) {
			usable[child.Tag] = true
		}
	}

	if _, ok := stated[tagRECFM]; !ok {
		r.Fail(&MissingRECFMError{Pos: form.Pos})
	}

	// Which children a framing requires and which it refuses follows from the
	// record format, so a record format nothing could be made of leaves those
	// rules with nothing to key on. The children have already been checked
	// against their own sorts, which is everything that can be said about them
	// without one.
	if !usable[tagRECFM] {
		return framing
	}

	r.children(form, framing.RECFM, stated)
	r.blockSize(framing, usable)

	return framing
}

// children holds the framing to the rules its record format states: what it
// requires, and what it refuses.
func (r *framingReader) children(form layout.Form, recfm RECFM, stated map[string]layout.Pos) {
	for _, child := range framingChildren {
		if child == tagRECFM {
			continue
		}

		pos, present := stated[child]

		switch admits(child, recfm) {
		case arityRequired:
			if !present {
				r.Fail(&RequiredChildError{Pos: form.Pos, Child: child, RECFM: recfm})
			}
		case arityRefused:
			if present {
				r.Fail(&UnadmittedChildError{Pos: pos, Child: child, RECFM: recfm})
			}
		case arityAdmitted:
		}
	}
}

// blockSize checks a stated `blksize` against the `lrecl` and the record format
// beside it.
//
// It runs only where both numbers were read, and only under the record formats
// that admit a `blksize` at all: everywhere else the fault has already been
// reported, and a second message about the same two lines would describe one of
// them wrongly.
func (r *framingReader) blockSize(framing *Framing, usable map[string]bool) {
	if !usable[tagBLKSIZE] || !usable[tagLRECL] {
		return
	}

	switch framing.RECFM {
	case RECFMFB:
		if framing.BlockSize.Value%framing.LRECL.Value != 0 {
			r.Fail(&BlockSizeNotAMultipleError{
				Pos:       framing.BlockSize.Pos,
				RECFM:     framing.RECFM,
				BlockSize: framing.BlockSize.Value,
				LRECL:     framing.LRECL.Value,
			})
		}
	case RECFMVB, RECFMVBS:
		if framing.BlockSize.Value < framing.LRECL.Value+blockDescriptorWord {
			r.Fail(&BlockSizeTooSmallError{
				Pos:       framing.BlockSize.Pos,
				RECFM:     framing.RECFM,
				BlockSize: framing.BlockSize.Value,
				LRECL:     framing.LRECL.Value,
			})
		}
	}
}

// value reads one child's value into the framing, reporting whether it read one.
func (r *framingReader) value(framing *Framing, child layout.Form) bool {
	switch child.Tag {
	case tagRECFM:
		return r.recfm(framing, child)
	case tagLRECL:
		return r.size(&framing.LRECL, child)
	case tagBLKSIZE:
		return r.size(&framing.BlockSize, child)
	case tagBlocks:
		return r.blocks(framing, child)
	case tagMaxSegment:
		return r.size(&framing.MaxSegment, child)
	case tagDelimiter:
		return r.delimiter(framing, child)
	case tagPlacement:
		return r.placement(framing, child)
	default:
		return false
	}
}

// recfm reads the record format, and rejects the two spellings that are admitted
// in order to be rejected: RECFM U, and a record format carrying a carriage
// control.
func (r *framingReader) recfm(framing *Framing, child layout.Form) bool {
	symbol, ok := r.symbol(child)
	if !ok {
		return false
	}

	value := RECFM(symbol.Value)

	if slices.Contains(recfms, value) {
		if value == RECFMU {
			r.Fail(&UndefinedLengthError{Pos: symbol.Pos})

			return false
		}

		framing.RECFM = value

		return true
	}

	if base, control, ok := carriageControl(symbol.Value); ok {
		r.Fail(&CarriageControlError{Pos: symbol.Pos, Value: symbol.Value, Control: control, RECFM: base})

		return false
	}

	r.Fail(&FramingValueError{Pos: symbol.Pos, Child: child.Tag, Value: symbol.Value, Admits: recfmNames()})

	return false
}

// blocks reads whether the stream still carries block descriptor words.
func (r *framingReader) blocks(framing *Framing, child layout.Form) bool {
	symbol, ok := r.symbol(child)
	if !ok {
		return false
	}

	switch Blocks(symbol.Value) {
	case Deblocked:
		framing.Blocks = Deblocked

		return true
	case InStream:
		r.Fail(&BlockedStreamError{Pos: symbol.Pos})

		return false
	}

	r.Fail(&FramingValueError{Pos: symbol.Pos, Child: child.Tag, Value: symbol.Value, Admits: blocksNames()})

	return false
}

// placement reads where a delimited file's delimiter sits.
func (r *framingReader) placement(framing *Framing, child layout.Form) bool {
	symbol, ok := r.symbol(child)
	if !ok {
		return false
	}

	value := Placement(symbol.Value)
	if !slices.Contains(placements, value) {
		r.Fail(&FramingValueError{Pos: symbol.Pos, Child: child.Tag, Value: symbol.Value, Admits: placementNames()})

		return false
	}

	framing.Placement = value

	return true
}

// delimiter reads the bytes a delimited file's records are wrapped in.
func (r *framingReader) delimiter(framing *Framing, child layout.Form) bool {
	if len(child.Elements) != 1 {
		r.Fail(&FramingFormError{Pos: child.Pos, Child: child.Tag, Takes: "one byte string", Found: count(len(child.Elements))})

		return false
	}

	value, err := readByteString(child.Elements[0])
	if err != nil {
		r.Fail(err)

		return false
	}

	framing.Delimiter = value

	return true
}

// size reads a child carrying a positive number of bytes.
func (r *framingReader) size(into *Size, child layout.Form) bool {
	const takes = "one positive number of bytes"

	if len(child.Elements) != 1 {
		r.Fail(&FramingFormError{Pos: child.Pos, Child: child.Tag, Takes: takes, Found: count(len(child.Elements))})

		return false
	}

	switch value := child.Elements[0].(type) {
	case layout.Int:
		if value.Value <= 0 {
			r.Fail(&SizeError{Pos: value.Pos, Child: child.Tag, Value: value.Value})

			return false
		}

		*into = Size{Pos: value.Pos, Value: value.Value}

		return true
	case layout.Float:
		// A number with a fraction is not a number of bytes, and describe would
		// call it "a number", which is what the position takes.
		r.Fail(&FramingFormError{Pos: value.Pos, Child: child.Tag, Takes: takes, Found: "a number with a fraction"})
	default:
		r.Fail(&FramingFormError{
			Pos:   child.Elements[0].Position(),
			Child: child.Tag,
			Takes: takes,
			Found: describe(child.Elements[0]),
		})
	}

	return false
}

// symbol reads the one symbol a child carrying a value out of a closed set is
// written with.
func (r *framingReader) symbol(child layout.Form) (layout.Symbol, bool) {
	const takes = "one symbol naming its value"

	if len(child.Elements) != 1 {
		r.Fail(&FramingFormError{Pos: child.Pos, Child: child.Tag, Takes: takes, Found: count(len(child.Elements))})

		return layout.Symbol{}, false
	}

	symbol, ok := child.Elements[0].(layout.Symbol)
	if !ok {
		r.Fail(&FramingFormError{
			Pos:   child.Elements[0].Position(),
			Child: child.Tag,
			Takes: takes,
			Found: describe(child.Elements[0]),
		})

		return layout.Symbol{}, false
	}

	return symbol, true
}

// arity is what a record format does with one of the `framing` children.
type arity int

const (
	// arityAdmitted is a child the record format takes and does not require.
	arityAdmitted arity = iota

	// arityRequired is a child the record format cannot do without.
	arityRequired

	// arityRefused is a child that says nothing about this record format, and
	// whose presence is a layout describing two different files at once.
	arityRefused
)

// admits is docs/layout/SPEC.md's conditional arities, which the published
// schema declares at their widest and leaves here: which children a `framing`
// requires "follows from the `recfm` value", and an arity that depends on a
// sibling's value is not something a declaration can state.
func admits(child string, recfm RECFM) arity {
	switch child {
	case tagLRECL:
		switch recfm {
		case RECFMF, RECFMFB:
			return arityRequired
		case LineSequential:
			return arityRefused
		}

		return arityAdmitted
	case tagBLKSIZE:
		switch recfm {
		case RECFMFB, RECFMVB, RECFMVBS:
			return arityAdmitted
		}

		return arityRefused
	case tagMaxSegment:
		if recfm == RECFMVBS {
			return arityRequired
		}

		return arityRefused
	case tagDelimiter, tagPlacement:
		if recfm == LineSequential {
			return arityRequired
		}

		return arityRefused
	default:
		// `blocks` is the one child no record format conditions: a stream either
		// still carries block descriptor words or it does not, and that is a
		// property of the transfer rather than of the dataset it came from.
		return arityAdmitted
	}
}

// carriageControl splits a record format carrying an ASA or machine carriage
// control into the format underneath it and the control it carries.
//
// `FBA` and `VBM` are the shapes an adopter has in their JCL, and neither is a
// record format this document admits: the control character is a byte of the
// record, and docs/layout/SPEC.md's "The adopter's spelling" says where it
// belongs instead. Recognising the suffix here is what turns "that is not a
// record format" into a diagnostic naming what was written.
func carriageControl(value string) (RECFM, string, bool) {
	var control string

	switch {
	case strings.HasSuffix(value, "A"):
		control = "an ASA"
	case strings.HasSuffix(value, "M"):
		control = "a machine"
	default:
		return "", "", false
	}

	base := RECFM(value[:len(value)-1])
	if base == LineSequential || !slices.Contains(recfms, base) {
		return "", "", false
	}

	return base, control, true
}

// count names how many things stood where one belongs.
func count(elements int) string {
	if elements > 1 {
		return "several"
	}

	return "no value"
}

// recfmNames is the record formats as a layout spells them, for a message that
// has to list them.
func recfmNames() []string {
	names := make([]string, 0, len(recfms))

	for _, recfm := range recfms {
		names = append(names, string(recfm))
	}

	return names
}

// blocksNames is the same for `blocks`.
func blocksNames() []string {
	names := make([]string, 0, len(blocksSpellings))

	for _, blocks := range blocksSpellings {
		names = append(names, string(blocks))
	}

	return names
}

// placementNames is the same for `placement`.
func placementNames() []string {
	names := make([]string, 0, len(placements))

	for _, placement := range placements {
		names = append(names, string(placement))
	}

	return names
}
