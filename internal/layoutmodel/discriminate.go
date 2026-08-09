// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"slices"
	"strconv"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
)

// The tags this layer reads. `discriminate` and `discriminate-variant` are the
// top-level forms; the rest stand inside them. `record` is read for its name
// alone, because a rule counting one form against another needs both.
const (
	tagDiscriminate        = "discriminate"
	tagDiscriminateVariant = "discriminate-variant"
	tagArm                 = "arm"
	tagEquals              = "equals"
	tagOneOf               = "one-of"
	tagRecord              = "record"
)

// StrategyKind is one member of the closed set of discrimination strategies.
//
// The set is docs/layout/SPEC.md's "Three strategies, and the set is closed for
// v1" and this type is that set as data. Nothing here adds a member: a fourth
// strategy is a fourth member of the IR's predicate set, which is breaking under
// docs/ir/SPEC.md's "Versioning and compatibility", and a change to what a
// layout may say besides.
//
// The zero value is not a strategy. A [Strategy] handed back by
// [ReadDiscrimination] always names one.
type StrategyKind string

// The three strategies, in docs/layout/SPEC.md's order.
const (
	// Equals is `(equals <item-ref> <literal>)`: the item's value is the
	// literal. It lowers into the IR's **bytes equal** predicate.
	Equals StrategyKind = "equals"

	// OneOf is `(one-of <item-ref> <literal> …)`: the item's value is one of
	// the literals. It lowers into the IR's **bytes one of** predicate, which is
	// a member of that set rather than a shorthand for several **bytes equal**
	// ones — a transition carries at most one predicate and an arm exactly one,
	// so there is nowhere to put the others.
	OneOf StrategyKind = "one-of"

	// SingleRecordType is the record that carries nothing a predicate may test.
	// It lowers into the *absence* of a predicate
	// (docs/ir/SPEC.md's "A transition may carry no predicate") and not into a
	// member of the set testing nothing, which is why [Strategy.Predicate]
	// reports whether a strategy lowers into one at all.
	//
	// It is not a default arm: it is not tried last and it does not catch what
	// the others miss. Where two records carrying it may appear at the same
	// point and nothing separates them, `resolve` rejects the layout.
	SingleRecordType StrategyKind = "single-record-type"
)

// strategies is the set a record's discriminator admits, in the order every
// message listing them renders them.
var strategies = []StrategyKind{Equals, OneOf, SingleRecordType}

// predicates is the set an arm admits: the strategies that lower into a
// predicate, and no third.
//
// An arm carries exactly one predicate. An arm selected by nothing at all would
// be the default arm docs/ir/SPEC.md's "A predicate on an arm reads one
// occurrence" refuses in as many words, so [SingleRecordType] is not among them.
var predicates = []StrategyKind{Equals, OneOf}

// LiteralKind is which of the three spellings a literal was written in.
//
// Which one it is decides what the literal is compared against, and all three
// resolutions are `resolve`'s: a layout says which *value* tells a record apart
// and the IR carries the *bytes* a consumer compares
// (docs/layout/SPEC.md's "Literals").
type LiteralKind string

// The three spellings, in docs/layout/SPEC.md's order.
const (
	// TextLiteral is `"01"`: resolved to bytes through the item's own charset
	// and padded to the item's width.
	TextLiteral LiteralKind = "text"

	// NumberLiteral is `12`: resolved to bytes through the item's `PICTURE`,
	// `USAGE` and axes.
	NumberLiteral LiteralKind = "number"

	// BytesLiteral is `(bytes "F0F1")`: taken literally, with no charset and no
	// padding. It is the one spelling that says exactly what is in the file, and
	// the one that is wrong if the file is converted.
	BytesLiteral LiteralKind = "bytes"
)

// Literal is one value a strategy compares an item against.
//
// Exactly one of the three fields below carries the value, and [Literal.Kind]
// says which. What the bytes are is `resolve`'s under every spelling.
type Literal struct {
	// Pos is the literal itself, which is what a diagnostic about its value
	// points at.
	Pos layout.Pos

	// Kind is the spelling it was written in.
	Kind LiteralKind

	// Text is what a [TextLiteral] said, with the grammar's escapes already
	// applied.
	Text string

	// Number is what a [NumberLiteral] said, rendered back from the value the
	// grammar read. Two spellings of one number — `12` and `12.0` — render the
	// same, which is what makes two of them one literal for [Literal.Identity].
	Number string

	// Bytes is what a [BytesLiteral] said.
	Bytes ByteString
}

// String renders a literal the way a layout writes it, which is what a
// diagnostic naming one quotes.
func (l Literal) String() string {
	switch l.Kind {
	case TextLiteral:
		return strconv.Quote(l.Text)
	case NumberLiteral:
		return l.Number
	case BytesLiteral:
		return l.Bytes.String()
	default:
		return "nothing"
	}
}

// Identity is what makes two literals the same one.
//
// The spelling *and* the sort, because the sort decides the resolution: `"01"`
// is text through the item's charset and `01` is a number through its `PICTURE`,
// and whether the two end up as the same bytes is a question only `resolve` can
// answer. Two literals that are the same here are the same under every charset,
// which is what makes an overlap decided on this identity one decidable from the
// layout alone.
//
// It is exported because the overlap this layer can decide is not the only one.
// `resolve` decides two more against the same identity and for the same reason —
// whether two transitions leaving one state can both match a record, and whether
// two guards over one register can hold at once — and a second reading of what
// makes two literals equal is a second answer for them to disagree about (#36,
// #37).
func (l Literal) Identity() string { return string(l.Kind) + " " + l.String() }

// Strategy is one discrimination strategy, read.
//
// A value handed back by [ReadDiscrimination] names a member of the closed set,
// and carries what that member needs: [SingleRecordType] carries neither an item
// nor a literal, [Equals] carries an item and one literal, and [OneOf] carries
// an item and at least one.
type Strategy struct {
	// Pos is the strategy — the `(equals …)` or `(one-of …)` form, or the
	// `single-record-type` symbol.
	Pos layout.Pos

	// Kind is which strategy it is.
	Kind StrategyKind

	// Item is what it tests, and is the zero [ItemRef] under
	// [SingleRecordType].
	Item ItemRef

	// Literals are what the item's value is compared against, in the order the
	// layout writes them. It is empty under [SingleRecordType] and holds exactly
	// one literal under [Equals].
	Literals []Literal
}

// Predicate reports whether this strategy lowers into an IR predicate, which
// every one but [SingleRecordType] does.
//
// It is a question about the value rather than a switch a caller writes for
// itself, because the answer is the whole of the difference between the two
// halves of the set: docs/ir/SPEC.md's "A transition may carry no predicate"
// makes selecting on nothing the *absence* of a predicate rather than a member
// testing nothing, so a caller compiling one has to ask before it reaches for
// [Strategy.Item].
func (s Strategy) Predicate() bool { return slices.Contains(predicates, s.Kind) }

// RecordDiscriminator is one `discriminate` form: a record, and what tells it
// apart.
type RecordDiscriminator struct {
	// Pos is the `discriminate` form.
	Pos layout.Pos

	// Record is the name of the `record` form it discriminates.
	Record string

	// RecordPos is the record name inside the form, which is what a diagnostic
	// about the name rather than about the strategy points at.
	RecordPos layout.Pos

	// Strategy is what tells the record apart.
	Strategy Strategy
}

// VariantDiscriminator is one `discriminate-variant` form: a redefine inside a
// repeating group, and the arms it may take.
//
// docs/layout/SPEC.md's "A discriminator for a redefine inside a table" is the
// whole of it. The alternative is chosen once per occurrence rather than once
// per record, which is why it is not a set of record types
// (docs/ir/SPEC.md's "A variant is chosen once per occurrence").
type VariantDiscriminator struct {
	// Pos is the `discriminate-variant` form.
	Pos layout.Pos

	// Variant is the item the copybook redefines — the first alternative, the
	// one every `REDEFINES` of it names.
	Variant ItemRef

	// Arms are the alternatives, in the order the layout writes them. There are
	// always at least two on a value handed back, and no two name one
	// alternative.
	Arms []Arm
}

// Arm is one alternative of a variant, and the predicate selecting it.
type Arm struct {
	// Pos is the `arm` form.
	Pos layout.Pos

	// Alternative is the name the copybook gives this alternative. That the
	// name is one the copybook declares at the variant's position is `resolve`'s
	// (#31, #35).
	Alternative string

	// Predicate is what selects it. Its [Strategy.Predicate] is always true: an
	// arm selected by nothing at all is a default arm, and there is none.
	Predicate Strategy
}

// Discrimination is a layout's discrimination layer: every `discriminate` form,
// and every `discriminate-variant` beside them.
//
// The two are the two scopes a discriminator is written in — a record, and an
// alternative inside one occurrence of a table — and the strategies are one
// closed set lowering into both.
type Discrimination struct {
	// Records are the record discriminators, in the order the layout writes
	// them. On a value handed back there is exactly one per `record` form the
	// layout defines.
	Records []RecordDiscriminator

	// Variants are the variant discriminators, in the order the layout writes
	// them, and no two of them name one item.
	Variants []VariantDiscriminator
}

// ReadDiscrimination reads the discrimination layer out of a parsed layout.
//
// It reports every fault it finds, joined, and returns nothing when it found
// one, for [ReadProfile]'s reason: a discriminator an adopter cannot act on is
// worse than none.
//
// What it enforces is what a declaration cannot state and a copybook is not
// needed for: that every `record` is named by exactly one `discriminate`, that a
// discriminator tests an item of the record it discriminates, that an arm's
// target stands where an arm's target may stand, and that two arms of one
// variant do not name one alternative or one literal. Everything else about
// containment and overlap needs a copybook and is `resolve`'s
// (docs/layout/SPEC.md's "Validation and diagnostics").
//
// Top-level forms belonging to other layers are not read here and are not
// faults. `record` forms are read for their names alone, because "exactly one
// per record" counts one form against another and there is nowhere else to count
// it.
func ReadDiscrimination(file *layout.File) (*Discrimination, error) {
	read := &discriminationReader{records: recordNames(file)}
	discrimination := &Discrimination{}

	for _, form := range file.Forms {
		switch form.Tag {
		case tagDiscriminate:
			read.discriminate(discrimination, form)
		case tagDiscriminateVariant:
			read.variant(discrimination, form)
		}
	}

	// A record nobody discriminated is a fact about the layout rather than about
	// any one form, so it is reported after everything the forms themselves were
	// wrong about. The records are walked in source order for the same reason
	// the forms are.
	for _, record := range read.records {
		if _, discriminated := read.discriminated[record.name]; !discriminated {
			read.Fail(&MissingDiscriminatorError{Pos: record.pos, Record: record.name})
		}
	}

	if read.Failed() {
		return nil, read.Err()
	}

	return discrimination, nil
}

// recordDefinition is a `record` form as this layer sees one: the name it
// defines, and where.
type recordDefinition struct {
	name string
	pos  layout.Pos
}

// recordNames gathers the records a layout defines, in source order.
//
// Only the name is read. What a record is bound to is the record-definitions
// layer's (#27, #30), and a `record` form this cannot read a name out of is that
// layer's fault to report: a second message about it here would name the same
// line twice.
func recordNames(file *layout.File) []recordDefinition {
	var records []recordDefinition

	for _, form := range file.Forms {
		if form.Tag != tagRecord || len(form.Elements) == 0 {
			continue
		}

		if symbol, ok := form.Elements[0].(layout.Symbol); ok {
			records = append(records, recordDefinition{name: symbol.Value, pos: form.Pos})
		}
	}

	return records
}

// discriminationReader holds the state one [ReadDiscrimination] accumulates.
type discriminationReader struct {
	diag.List

	// records are the records the layout defines, which is what makes "one
	// discriminator per record" and "a discriminator on a record nobody defined"
	// answerable.
	records []recordDefinition

	// discriminated is where each record was first discriminated, which makes a
	// second `discriminate` on one record reportable against the first.
	discriminated map[string]layout.Pos

	// variants is the same for `discriminate-variant`, keyed by the variant
	// reference's identity.
	variants map[string]layout.Pos
}

// discriminate reads one `discriminate` form.
func (r *discriminationReader) discriminate(into *Discrimination, form layout.Form) {
	if len(form.Elements) != 2 {
		r.Fail(&DiscriminateFormError{Pos: form.Pos, Found: count(len(form.Elements))})

		return
	}

	name, ok := form.Elements[0].(layout.Symbol)
	if !ok {
		r.Fail(&DiscriminateFormError{Pos: form.Elements[0].Position(), Found: describe(form.Elements[0])})

		// The strategy is read anyway, for the reason an override's axes are
		// read after a misspelled item reference: a discriminator whose record
		// is written as text is still a discriminator, and a literal misspelled
		// underneath it is a second thing to fix rather than something to
		// discover on the next run.
		_, _ = r.strategy(form.Elements[1], subjectRecord)

		return
	}

	if !slices.ContainsFunc(r.records, func(record recordDefinition) bool { return record.name == name.Value }) {
		r.Fail(&UnknownRecordError{Pos: name.Pos, Record: name.Value, Form: form.Tag})
	} else if first, already := r.discriminated[name.Value]; already {
		r.Fail(&DuplicateDiscriminatorError{Pos: form.Pos, First: first, Record: name.Value})
	} else {
		if r.discriminated == nil {
			r.discriminated = make(map[string]layout.Pos)
		}

		r.discriminated[name.Value] = form.Pos
	}

	// The strategy is read whatever was wrong with the name: a misspelled record
	// with a misspelled charset in its literal is two things to fix rather than
	// one to discover on the next run.
	strategy, ok := r.strategy(form.Elements[1], subjectRecord)
	if !ok {
		return
	}

	// A discriminator tests an item of the record it discriminates. The half of
	// that rule needing a copybook — that the path names an item of it at all —
	// is `resolve`'s; this half is the reference's own spelling and is checkable
	// here (#84).
	if strategy.Predicate() && strategy.Item.Record != name.Value {
		r.Fail(&ForeignTargetError{Pos: strategy.Item.Pos, Item: strategy.Item, Record: name.Value})

		return
	}

	into.Records = append(into.Records, RecordDiscriminator{
		Pos:       form.Pos,
		Record:    name.Value,
		RecordPos: name.Pos,
		Strategy:  strategy,
	})
}

// variant reads one `discriminate-variant` form.
func (r *discriminationReader) variant(into *Discrimination, form layout.Form) {
	if len(form.Elements) == 0 {
		r.Fail(&VariantFormError{Pos: form.Pos, Found: "a variant discriminator naming no item at all"})

		return
	}

	item, err := readItemRef(form.Elements[0])
	if err != nil {
		r.Fail(err)

		return
	}

	discriminator := VariantDiscriminator{Pos: form.Pos, Variant: item}

	r.variantItem(form, item)

	for _, element := range form.Elements[1:] {
		arm, ok := r.arm(element, item)
		if !ok {
			continue
		}

		if first, already := armNamed(discriminator.Arms, arm.Alternative); already {
			r.Fail(&DuplicateArmError{Pos: arm.Pos, First: first, Variant: item, Alternative: arm.Alternative})

			continue
		}

		r.overlap(discriminator.Arms, arm, item)

		discriminator.Arms = append(discriminator.Arms, arm)
	}

	// The count is checked against the arms that were read rather than against
	// the elements written, so a variant carrying two arms one of which is
	// malformed is not also reported as a variant carrying one.
	if len(discriminator.Arms) < 2 {
		r.Fail(&VariantArmCountError{Pos: form.Pos, Variant: item, Count: len(discriminator.Arms)})

		return
	}

	into.Variants = append(into.Variants, discriminator)
}

// variantItem holds the item a `discriminate-variant` names to what a variant
// reference can be checked against without a copybook.
func (r *discriminationReader) variantItem(form layout.Form, item ItemRef) {
	if !slices.ContainsFunc(r.records, func(record recordDefinition) bool { return record.name == item.Record }) {
		r.Fail(&UnknownRecordError{Pos: item.Pos, Record: item.Record, Form: form.Tag})
	}

	if first, already := r.variants[item.identity()]; already {
		r.Fail(&DuplicateVariantError{Pos: form.Pos, First: first, Variant: item})
	} else {
		if r.variants == nil {
			r.variants = make(map[string]layout.Pos)
		}

		r.variants[item.identity()] = form.Pos
	}

	// A variant sits inside a group that repeats. A reference carrying one name
	// names an item directly under the record's top-level item, whose only
	// ancestor is that item — and a record does not repeat, so no copybook can
	// make such a reference name a variant.
	if len(item.Path) < 2 {
		r.Fail(&VariantDepthError{Pos: item.Pos, Variant: item})
	}
}

// arm reads one `(arm <name> <predicate>)`.
func (r *discriminationReader) arm(node layout.Node, variant ItemRef) (Arm, bool) {
	form, ok := node.(layout.Form)
	if !ok {
		r.Fail(&ArmFormError{Pos: node.Position(), Found: describe(node)})

		return Arm{}, false
	}

	if form.Tag != tagArm {
		r.Fail(&ArmFormError{Pos: form.TagPos, Found: "form " + quote(form.Tag)})

		return Arm{}, false
	}

	if len(form.Elements) != 2 {
		r.Fail(&ArmFormError{Pos: form.Pos, Found: count(len(form.Elements))})

		return Arm{}, false
	}

	name, ok := form.Elements[0].(layout.Symbol)
	if !ok {
		r.Fail(&ArmFormError{Pos: form.Elements[0].Position(), Found: describe(form.Elements[0])})

		return Arm{}, false
	}

	predicate, ok := r.strategy(form.Elements[1], subjectArm)
	if !ok {
		return Arm{}, false
	}

	if !predicate.Predicate() {
		r.Fail(&StrategyError{
			Pos:     predicate.Pos,
			Subject: subjectArm,
			Found:   "the symbol " + quote(string(predicate.Kind)),
			Admits:  predicateNames(),
		})

		return Arm{}, false
	}

	if found, ok := armTargetFault(predicate.Item, variant, name.Value); ok {
		r.Fail(&ArmTargetError{
			Pos:         predicate.Item.Pos,
			Alternative: name.Value,
			Item:        predicate.Item,
			Variant:     variant,
			Found:       found,
		})

		return Arm{}, false
	}

	return Arm{Pos: form.Pos, Alternative: name.Value, Predicate: predicate}, true
}

// overlap reports two arms of one variant that name one target and one literal.
//
// It is the smallest overlap and the only one decidable from the layout alone:
// two arms testing one item for one value both match every occurrence the other
// does, whatever the copybook says the item is. Everything else — one literal
// written as text and another as bytes, two literals that resolve to the same
// bytes under the item's charset and width — needs the copybook and is
// `resolve`'s.
func (r *discriminationReader) overlap(read []Arm, arm Arm, variant ItemRef) {
	for _, earlier := range read {
		if earlier.Predicate.Item.identity() != arm.Predicate.Item.identity() {
			continue
		}

		for _, literal := range arm.Predicate.Literals {
			index := slices.IndexFunc(earlier.Predicate.Literals, func(other Literal) bool {
				return other.Identity() == literal.Identity()
			})
			if index < 0 {
				continue
			}

			r.Fail(&ArmOverlapError{
				Pos:     literal.Pos,
				First:   earlier.Predicate.Literals[index].Pos,
				Variant: variant,
				Arms:    [2]string{earlier.Alternative, arm.Alternative},
				Item:    arm.Predicate.Item,
				Literal: literal,
			})

			return
		}
	}
}

// strategy reads a strategy: the `single-record-type` symbol, or one of the two
// forms that lower into a predicate.
//
// subject names what the strategy was written for, so that a diagnostic about
// one says whether a record or an arm was being selected — the sets differ, and
// a message naming the wrong one sends an adopter to write a strategy the
// position does not take.
func (r *discriminationReader) strategy(node layout.Node, subject string) (Strategy, bool) {
	admits := strategyNames()
	if subject == subjectArm {
		admits = predicateNames()
	}

	if symbol, ok := node.(layout.Symbol); ok {
		if StrategyKind(symbol.Value) == SingleRecordType {
			return Strategy{Pos: symbol.Pos, Kind: SingleRecordType}, true
		}

		r.Fail(&StrategyError{Pos: symbol.Pos, Subject: subject, Found: describe(symbol), Admits: admits})

		return Strategy{}, false
	}

	form, ok := node.(layout.Form)
	if !ok {
		r.Fail(&StrategyError{Pos: node.Position(), Subject: subject, Found: describe(node), Admits: admits})

		return Strategy{}, false
	}

	kind := StrategyKind(form.Tag)
	if !slices.Contains(predicates, kind) {
		r.Fail(&StrategyError{Pos: form.TagPos, Subject: subject, Found: describe(form), Admits: admits})

		return Strategy{}, false
	}

	if len(form.Elements) < 2 {
		r.Fail(&StrategyFormError{Pos: form.Pos, Kind: kind, Found: strategyShortfall(form.Elements)})

		return Strategy{}, false
	}

	if kind == Equals && len(form.Elements) > 2 {
		r.Fail(&StrategyFormError{Pos: form.Elements[2].Position(), Kind: kind, Found: "several"})

		return Strategy{}, false
	}

	item, err := readItemRef(form.Elements[0])
	if err != nil {
		r.Fail(err)

		return Strategy{}, false
	}

	strategy := Strategy{Pos: form.Pos, Kind: kind, Item: item}

	for _, element := range form.Elements[1:] {
		literal, err := readLiteral(element)
		if err != nil {
			r.Fail(err)

			return Strategy{}, false
		}

		strategy.Literals = append(strategy.Literals, literal)
	}

	return strategy, true
}

// readLiteral reads one of the three spellings a literal takes.
//
// It is a free function rather than a reader's method for [readItemRef]'s
// reason: sequencing's `when` compares a value against literals too
// (docs/layout/SPEC.md's "Two operators read a value"), and one reading of a
// literal is what keeps the two layers spelling them alike.
func readLiteral(node layout.Node) (Literal, error) {
	switch value := node.(type) {
	case layout.Text:
		return Literal{Pos: value.Pos, Kind: TextLiteral, Text: value.Value}, nil
	case layout.Int:
		return Literal{Pos: value.Pos, Kind: NumberLiteral, Number: strconv.FormatInt(value.Value, 10)}, nil
	case layout.Float:
		return Literal{
			Pos:    value.Pos,
			Kind:   NumberLiteral,
			Number: strconv.FormatFloat(value.Value, 'f', -1, 64),
		}, nil
	case layout.Form:
		bytes, err := readByteString(value)
		if err != nil {
			return Literal{}, err
		}

		return Literal{Pos: bytes.Pos, Kind: BytesLiteral, Bytes: bytes}, nil
	default:
		return Literal{}, &LiteralError{Pos: node.Position(), Found: describe(node)}
	}
}

// armTargetFault reports what is wrong with an arm's target, where anything is.
//
// The three it answers are the halves of docs/layout/SPEC.md's rule that need no
// copybook, and they are the three an adopter gets wrong while holding one open.
// That the group the target shares with the variant is the *innermost repeating*
// one, and that the names exist at all, are `resolve`'s.
func armTargetFault(item ItemRef, variant ItemRef, alternative string) (string, bool) {
	if item.Record != variant.Record {
		return "rooted at record " + quote(item.Record) + ", and the variant is rooted at " + quote(variant.Record), true
	}

	if len(variant.Path) < 2 {
		// The variant reference is already a fault of its own, and every answer
		// this could give about a target under it would be about a variant that
		// cannot exist.
		return "", false
	}

	if len(item.Path) == 0 || item.Path[0] != variant.Path[0] {
		return "outside " + quote(variant.Path[0]) + ", which is the outermost group the variant sits in", true
	}

	if slices.Equal(item.Path[:min(len(item.Path), len(variant.Path))], variant.Path) {
		return "inside the variant itself", true
	}

	// An arm's own name stands where the variant's does, since every alternative
	// redefines the same bytes. A target descending through one is a target
	// inside an alternative, which has bytes only where that alternative was
	// selected.
	inside := variant.Path[:len(variant.Path)-1]
	if len(item.Path) > len(inside) && slices.Equal(item.Path[:len(inside)], inside) && item.Path[len(inside)] == alternative {
		return "inside the arm it selects", true
	}

	return "", false
}

// armNamed reports where an alternative was first named, among the arms already
// read.
func armNamed(read []Arm, alternative string) (layout.Pos, bool) {
	index := slices.IndexFunc(read, func(arm Arm) bool { return arm.Alternative == alternative })
	if index < 0 {
		return layout.Pos{}, false
	}

	return read[index].Pos, true
}

// strategyShortfall names what a strategy form carries where an item and a
// literal belong.
func strategyShortfall(elements []layout.Node) string {
	if len(elements) == 1 {
		return "an item and no literal"
	}

	return "no value"
}

// strategyNames is the strategies as a layout spells them, for a message that
// has to list them.
func strategyNames() []string {
	names := make([]string, 0, len(strategies))

	for _, strategy := range strategies {
		names = append(names, string(strategy))
	}

	return names
}

// predicateNames is the same for the strategies an arm admits.
func predicateNames() []string {
	names := make([]string, 0, len(predicates))

	for _, predicate := range predicates {
		names = append(names, string(predicate))
	}

	return names
}
