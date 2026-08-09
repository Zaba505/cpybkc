// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package layoutmodel

import (
	"slices"
	"strconv"
	"strings"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
)

// The tags this layer reads. `sequence` is the top-level form; the seven below
// it are the operators, and each is a tag in the `expression` sort rather than a
// form a layout writes anywhere else.
const (
	tagSequence   = "sequence"
	tagSeq        = "seq"
	tagAlt        = "alt"
	tagZeroOrMore = "*"
	tagOneOrMore  = "+"
	tagOptional   = "?"
	tagTimes      = "times"
	tagWhen       = "when"
)

// ExpressionKind is one member of the closed set a sequencing expression is
// built out of: a record name, or one of the seven operators.
//
// The set is docs/layout/SPEC.md's "The operators" and this type is that set as
// data. It is closed for the reason [StrategyKind] is: an eighth operator is a
// fourth guard test in docs/ir/SPEC.md's "The automaton remembers in registers",
// which is breaking there, and a change to what a layout may say besides.
//
// The zero value is not a kind. An [Expression] handed back by [ReadSequence]
// always names one.
type ExpressionKind string

// The eight, in docs/layout/SPEC.md's order. Every operator's constant is the
// tag a layout writes it as, which is what lets [Expression.String] print one
// without a second table saying how each is spelled.
const (
	// RecordName is a bare record name: one record of that type. It is the only
	// member that is not a form, and so the only one whose constant is a name
	// for the position rather than a tag — it is the published schema's
	// `record-name` sort, which is where the name comes from.
	RecordName ExpressionKind = "record-name"

	// Seq is `(seq <e> …)`: each in turn, left to right.
	Seq ExpressionKind = tagSeq

	// Alt is `(alt <e> …)`: exactly one of them.
	Alt ExpressionKind = tagAlt

	// ZeroOrMore is `(* <e>)`. An empty file is one of these accepting, which is
	// the whole of the difference between it and [OneOrMore].
	ZeroOrMore ExpressionKind = tagZeroOrMore

	// OneOrMore is `(+ <e>)`.
	OneOrMore ExpressionKind = tagOneOrMore

	// Optional is `(? <e>)`: zero or one.
	Optional ExpressionKind = tagOptional

	// Times is `(times <e> <item-ref>)`: exactly as many as the named item
	// holds. It lowers into a register holding an integer and a guard reading it
	// (docs/ir/SPEC.md's "The automaton remembers in registers", #77).
	Times ExpressionKind = tagTimes

	// When is `(when <item-ref> <match> <e>)`: the subexpression only where the
	// item holds that value. It lowers into a register and one of the two guard
	// tests a [Match] names.
	When ExpressionKind = tagWhen
)

// operators is the set an expression admits as a form, in the order every
// message listing them renders them. A record name is not among them because it
// is not a form: what a message about a form names is the seven.
var operators = []ExpressionKind{Seq, Alt, ZeroOrMore, OneOrMore, Optional, Times, When}

// branches is the subset taking two or more subexpressions.
var branches = []ExpressionKind{Seq, Alt}

// repetitions is the subset taking exactly one subexpression and nothing else.
var repetitions = []ExpressionKind{ZeroOrMore, OneOrMore, Optional}

// Match is what a `when` tests an item's value against: one literal, or one of
// several.
//
// [Match.Kind] is the guard test it lowers into, and it is a [StrategyKind]
// rather than a set of its own because there is no third: docs/layout/SPEC.md's
// "Two operators read a value" closes the tests at the register test `times`
// carries and the two an ordinary predicate carries, and those two are [Equals]
// and [OneOf] already. A bare literal is [Equals] and `(one-of …)` is [OneOf],
// whichever way round the layout happens to have written a single value.
type Match struct {
	// Pos is what was written: the literal itself, or the `(one-of …)` form
	// around several.
	Pos layout.Pos

	// Kind is the test it lowers into, and is [Equals] or [OneOf].
	Kind StrategyKind

	// Literals are what the item's value is compared against, in the order the
	// layout writes them. It holds exactly one under [Equals] and at least one
	// under [OneOf].
	Literals []Literal
}

// String renders a match the way a layout writes it, which is what a diagnostic
// naming one quotes and what [Expression.String] prints inside a `when`.
func (m Match) String() string {
	literals := make([]string, 0, len(m.Literals))
	for _, literal := range m.Literals {
		literals = append(literals, literal.String())
	}

	if m.Kind == OneOf {
		return "(" + tagOneOf + " " + strings.Join(literals, " ") + ")"
	}

	if len(literals) == 0 {
		return "nothing"
	}

	return literals[0]
}

// Expression is one node of a sequencing expression.
//
// One type carries all eight kinds, and [Expression.Kind] says which, for the
// reason [Strategy] carries three: the set is closed, so a switch over it is
// exhaustive and stays exhaustive, and a caller compiling one reaches for the
// same fields whatever it turns out to be.
//
// Which fields a value carries follows from its kind, and a value handed back by
// [ReadSequence] always carries exactly those: [RecordName] carries a record and
// nothing else; [Seq] and [Alt] carry two or more subexpressions; [ZeroOrMore],
// [OneOrMore] and [Optional] carry one; [Times] carries one and the item
// counting it; [When] carries one, the item it reads and the [Match] against it.
type Expression struct {
	// Pos is where it was written: the form, or the record name itself.
	Pos layout.Pos

	// Kind is which member of the closed set it is.
	Kind ExpressionKind

	// Record is the record named, under [RecordName] alone.
	Record string

	// Item is what [Times] counts by and what [When] reads, and is the zero
	// [ItemRef] under every other kind.
	Item ItemRef

	// Match is what [When] compares that item against, and is the zero [Match]
	// under every other kind.
	Match Match

	// Sub are the subexpressions, in the order the layout writes them.
	Sub []Expression
}

// String renders an expression the way a layout writes it.
//
// It is the printer half of this layer, and it is total over the model rather
// than over the source: what comes back is one line, whatever the layout's own
// line breaks were, because an expression is a term and the line it was broken
// across says nothing about it. Reading that line back yields the same
// expression with new positions on it, which is what
// [github.com/Zaba505/cpybkc/internal/layoutmodel]'s round-trip test asserts.
func (e Expression) String() string {
	switch {
	case e.Kind == RecordName:
		return e.Record
	case slices.Contains(branches, e.Kind), slices.Contains(repetitions, e.Kind):
		parts := make([]string, 0, len(e.Sub)+1)
		parts = append(parts, string(e.Kind))

		for _, sub := range e.Sub {
			parts = append(parts, sub.String())
		}

		return "(" + strings.Join(parts, " ") + ")"
	case e.Kind == Times:
		return "(" + tagTimes + " " + e.subexpression() + " " + e.Item.String() + ")"
	case e.Kind == When:
		return "(" + tagWhen + " " + e.Item.String() + " " + e.Match.String() + " " + e.subexpression() + ")"
	default:
		return "nothing"
	}
}

// subexpression renders the one subexpression an operator taking exactly one
// carries.
//
// A value handed back by [ReadSequence] always carries it. The guard is for a
// value somebody built by hand, which is [Literal.String]'s reason for having a
// default case: a printer that panics is one nothing can use in a diagnostic.
func (e Expression) subexpression() string {
	if len(e.Sub) == 0 {
		return "nothing"
	}

	return e.Sub[0].String()
}

// Sequence is a layout's sequencing layer: the one `sequence` form, and the
// expression under it.
//
// It is the whole of what a layout says about the order records may appear in.
// What that order *means* — the automaton, its start state, the registers the
// value-reading operators compile into — is docs/ir/SPEC.md's and `resolve`'s
// (#36).
type Sequence struct {
	// Pos is the `sequence` form.
	Pos layout.Pos

	// Expression is the expression it carries.
	Expression Expression
}

// String renders the layer the way a layout writes it.
func (s *Sequence) String() string {
	return "(" + tagSequence + " " + s.Expression.String() + ")"
}

// ReadSequence reads the sequencing layer out of a parsed layout.
//
// It reports every fault it finds, joined, and returns nothing when it found
// one, for [ReadProfile]'s reason: a sequence an adopter cannot act on is worse
// than none.
//
// What it enforces is what a declaration cannot state and a copybook is not
// needed for: that the layout carries exactly one `sequence`, that every node of
// the expression is a member of the closed set carrying what that member takes,
// that every record name is one the layout defines, and that every record the
// layout defines is named somewhere in the expression. The last is
// docs/layout/SPEC.md's in as many words — "a record defined and never sequenced
// is a record type nothing can ever admit" — and it counts one form against
// another, which is why the published schema cannot state it.
//
// What it does not enforce is the pair of rules on `times` and `when` that need
// more than the layout: that the item is admitted strictly earlier on every path,
// that it does not repeat, and that a counted item decodes to an integer. Each
// needs the copybook or the compiled graph, and docs/layout/SPEC.md's "Validation
// and diagnostics" files all three under `resolve` (#36, #37, #88). The half
// checkable here is the reference's own spelling, which [readItemRef] holds it
// to, and the record it is rooted at.
//
// Top-level forms belonging to other layers are not read here and are not
// faults. `record` forms are read for their names alone, because "every record
// is sequenced" counts one form against another and there is nowhere else to
// count it.
func ReadSequence(file *layout.File) (*Sequence, error) {
	read := &sequenceReader{records: recordNames(file)}
	sequence := &Sequence{Pos: file.Start()}

	// Every `sequence` form is read and not only the one that will be kept, so
	// that a layout carrying two malformed sequences is told about both rather
	// than about the count alone.
	var forms []layout.Form

	for _, form := range file.Forms {
		if form.Tag != tagSequence {
			continue
		}

		forms = append(forms, form)

		one, ok := read.sequence(form)
		if ok && len(forms) == 1 {
			sequence = one
		}
	}

	if len(forms) != 1 {
		// The second form is what the diagnostic points at where there is one:
		// the first is a sequence an adopter meant, and the second is the line
		// making it ambiguous.
		pos := file.Start()
		if len(forms) > 1 {
			pos = forms[1].Pos
		}

		read.Fail(&SequenceCountError{Pos: pos, Count: len(forms)})
	}

	// A record the expression never names is a fact about the layout rather than
	// about any one form, so it is reported after everything the forms
	// themselves were wrong about, and in source order for the same reason they
	// are read in it.
	//
	// It is reported only where the layout states a sequence at all. A layout
	// carrying none names no record anywhere, and saying so once per record
	// would describe the missing form as many times as the layout has record
	// types.
	if len(forms) > 0 {
		for _, record := range read.records {
			if _, sequenced := read.sequenced[record.name]; !sequenced {
				read.Fail(&UnsequencedRecordError{Pos: record.pos, Record: record.name})
			}
		}
	}

	if read.Failed() {
		return nil, read.Err()
	}

	return sequence, nil
}

// sequenceReader holds the state one [ReadSequence] accumulates.
type sequenceReader struct {
	diag.List

	// records are the records the layout defines, which is what makes "a record
	// name the layout defines" and "a record the expression never names"
	// answerable.
	records []recordDefinition

	// sequenced is where each record name was first written in an expression,
	// which is the other half of the completeness rule.
	sequenced map[string]layout.Pos
}

// sequence reads one `sequence` form.
func (r *sequenceReader) sequence(form layout.Form) (*Sequence, bool) {
	if len(form.Elements) != 1 {
		r.Fail(&SequenceFormError{Pos: form.Pos, Found: count(len(form.Elements))})

		// Every element is read anyway, for the reason a discriminator's
		// strategy is read after a misspelled record name: a `sequence` carrying
		// two expressions is one expression too many *and* possibly two
		// expressions with something wrong inside them, and the second is a
		// thing to fix rather than something to discover on the next run.
		for _, element := range form.Elements {
			_, _ = r.expression(element, form.Tag)
		}

		return nil, false
	}

	expression, ok := r.expression(form.Elements[0], form.Tag)
	if !ok {
		return nil, false
	}

	return &Sequence{Pos: form.Pos, Expression: expression}, true
}

// expression reads one node of the expression: a record name, or an operator.
//
// within is the tag of the form the node stands in, so that a diagnostic about a
// record name names the operator it was written under rather than the layer.
func (r *sequenceReader) expression(node layout.Node, within string) (Expression, bool) {
	switch node := node.(type) {
	case layout.Symbol:
		r.name(node, within)

		return Expression{Pos: node.Pos, Kind: RecordName, Record: node.Value}, true
	case layout.Form:
		return r.operator(node)
	default:
		r.Fail(&ExpressionError{Pos: node.Position(), Found: describe(node), Admits: operatorNames()})

		return Expression{}, false
	}
}

// operator reads one of the seven forms an expression admits.
func (r *sequenceReader) operator(form layout.Form) (Expression, bool) {
	kind := ExpressionKind(form.Tag)

	switch {
	case slices.Contains(branches, kind):
		return r.branch(form, kind)
	case slices.Contains(repetitions, kind):
		return r.repetition(form, kind)
	case kind == Times:
		return r.times(form)
	case kind == When:
		return r.when(form)
	default:
		r.Fail(&ExpressionError{Pos: form.TagPos, Found: describe(form), Admits: operatorNames()})

		return Expression{}, false
	}
}

// branch reads a `seq` or an `alt`, which take two or more subexpressions.
func (r *sequenceReader) branch(form layout.Form, kind ExpressionKind) (Expression, bool) {
	expression := Expression{Pos: form.Pos, Kind: kind}

	for _, element := range form.Elements {
		sub, ok := r.expression(element, form.Tag)
		if !ok {
			continue
		}

		expression.Sub = append(expression.Sub, sub)
	}

	// The count is checked against the subexpressions that were read rather than
	// against the elements written, for [VariantArmCountError]'s reason: a `seq`
	// of two one of which is malformed is reported against that subexpression
	// and against this rule, and not twice against this one.
	if len(expression.Sub) < 2 {
		r.Fail(&ExpressionFormError{Pos: form.Pos, Kind: kind, Found: subexpressions(len(expression.Sub))})

		return Expression{}, false
	}

	return expression, true
}

// repetition reads a `*`, a `+` or a `?`, which take exactly one subexpression.
func (r *sequenceReader) repetition(form layout.Form, kind ExpressionKind) (Expression, bool) {
	if len(form.Elements) != 1 {
		r.Fail(&ExpressionFormError{Pos: form.Pos, Kind: kind, Found: subexpressions(len(form.Elements))})

		for _, element := range form.Elements {
			_, _ = r.expression(element, form.Tag)
		}

		return Expression{}, false
	}

	sub, ok := r.expression(form.Elements[0], form.Tag)
	if !ok {
		return Expression{}, false
	}

	return Expression{Pos: form.Pos, Kind: kind, Sub: []Expression{sub}}, true
}

// times reads `(times <e> <item-ref>)`.
func (r *sequenceReader) times(form layout.Form) (Expression, bool) {
	if len(form.Elements) != 2 {
		r.Fail(&ExpressionFormError{Pos: form.Pos, Kind: Times, Found: timesShortfall(form.Elements)})

		// What was written is read anyway, for the reason a `sequence` carrying
		// two expressions has both read: an operator with the wrong number of
		// arguments may also have something wrong inside one of them, and the
		// second is a thing to fix rather than something to discover on the next
		// run.
		r.timesParts(form)

		return Expression{}, false
	}

	sub, ok := r.expression(form.Elements[0], form.Tag)

	// The count is read whatever was wrong with the subexpression, because the
	// two are independent halves of the operator and an adopter fixing one
	// should not have to run again to be told about the other.
	item, err := readItemRef(form.Elements[1])
	if err != nil {
		r.Fail(err)

		return Expression{}, false
	}

	r.itemRecord(item, form.Tag)

	if !ok {
		return Expression{}, false
	}

	return Expression{Pos: form.Pos, Kind: Times, Item: item, Sub: []Expression{sub}}, true
}

// when reads `(when <item-ref> <match> <e>)`.
func (r *sequenceReader) when(form layout.Form) (Expression, bool) {
	if len(form.Elements) != 3 {
		r.Fail(&ExpressionFormError{Pos: form.Pos, Kind: When, Found: whenShortfall(form.Elements)})

		// As in [sequenceReader.times]: the arity is one fault and what stands
		// inside the operator may be another.
		r.whenParts(form)

		return Expression{}, false
	}

	item, err := readItemRef(form.Elements[0])
	if err != nil {
		r.Fail(err)

		return Expression{}, false
	}

	r.itemRecord(item, form.Tag)

	match, matched := r.match(form.Elements[1])

	sub, ok := r.expression(form.Elements[2], form.Tag)
	if !matched || !ok {
		return Expression{}, false
	}

	return Expression{Pos: form.Pos, Kind: When, Item: item, Match: match, Sub: []Expression{sub}}, true
}

// timesParts reads whatever a malformed `times` carries, so that a fault inside
// one of its arguments is reported beside the arity rather than after it.
//
// Each element is read in the position the operator declares for it, with one
// exception: where a `times` carries a single element, which position that
// element stands in is what the element itself says. An item reference is the
// count and anything else is the subexpression, because reading a count as a
// term would name the same line twice and describe it wrongly the second time.
func (r *sequenceReader) timesParts(form layout.Form) {
	for i, element := range form.Elements {
		if i > 1 {
			return
		}

		if i == 1 || (len(form.Elements) == 1 && isItemRef(element)) {
			r.count(element, form.Tag)

			continue
		}

		_, _ = r.expression(element, form.Tag)
	}
}

// whenParts is the same for a malformed `when`.
//
// It needs no exception: a `when` names the item it reads first, so the position
// an element stands in is what it is, however few of the three were written.
func (r *sequenceReader) whenParts(form layout.Form) {
	for i, element := range form.Elements {
		switch i {
		case 0:
			r.count(element, form.Tag)
		case 1:
			_, _ = r.match(element)
		case 2:
			_, _ = r.expression(element, form.Tag)
		default:
			return
		}
	}
}

// count reads the item reference a value-reading operator stands on, and holds
// the record it is rooted at to the records the layout defines.
func (r *sequenceReader) count(node layout.Node, within string) {
	item, err := readItemRef(node)
	if err != nil {
		r.Fail(err)

		return
	}

	r.itemRecord(item, within)
}

// isItemRef reports whether a node is written as an item reference.
//
// It is what tells "a count and no subexpression" from "a subexpression and no
// count": the two are one arity and two different mistakes.
func isItemRef(node layout.Node) bool {
	form, ok := node.(layout.Form)

	return ok && form.Tag == tagItem
}

// match reads what a `when` tests a value against: one literal, or `(one-of
// <literal> …)`.
func (r *sequenceReader) match(node layout.Node) (Match, bool) {
	form, ok := node.(layout.Form)
	if ok && form.Tag == tagOneOf {
		if len(form.Elements) == 0 {
			r.Fail(&MatchFormError{Pos: form.Pos, Found: "no literal"})

			return Match{}, false
		}

		match := Match{Pos: form.Pos, Kind: OneOf}

		for _, element := range form.Elements {
			literal, err := readLiteral(element)
			if err != nil {
				r.Fail(err)

				return Match{}, false
			}

			match.Literals = append(match.Literals, literal)
		}

		return match, true
	}

	// Everything else is read as a literal, including a form: `(bytes "F0")` is
	// one of the three spellings, and a form that is neither that nor `one-of`
	// is a [LiteralError] naming what was written.
	literal, err := readLiteral(node)
	if err != nil {
		r.Fail(err)

		return Match{}, false
	}

	return Match{Pos: literal.Pos, Kind: Equals, Literals: []Literal{literal}}, true
}

// name holds a record name written in the expression to the records the layout
// defines, and records that the name was written at all.
func (r *sequenceReader) name(symbol layout.Symbol, within string) {
	if r.sequenced == nil {
		r.sequenced = make(map[string]layout.Pos)
	}

	if _, already := r.sequenced[symbol.Value]; !already {
		r.sequenced[symbol.Value] = symbol.Pos
	}

	if !r.defines(symbol.Value) {
		r.Fail(&UnknownRecordError{Pos: symbol.Pos, Record: symbol.Value, Form: within})
	}
}

// itemRecord holds the record an operator's item reference is rooted at to the
// records the layout defines.
//
// It does not count as the record being sequenced. A `times` reads a value out
// of a record the expression has already admitted somewhere else; naming it here
// admits nothing, and treating it as an appearance would let a record be
// referred to by an operator and never actually sequenced.
func (r *sequenceReader) itemRecord(item ItemRef, within string) {
	if !r.defines(item.Record) {
		r.Fail(&UnknownRecordError{Pos: item.Pos, Record: item.Record, Form: within})
	}
}

// defines reports whether the layout defines a record of that name.
func (r *sequenceReader) defines(name string) bool {
	return slices.ContainsFunc(r.records, func(record recordDefinition) bool { return record.name == name })
}

// operatorNames is the operators as a layout spells them, for a message that has
// to list them.
func operatorNames() []string {
	names := make([]string, 0, len(operators))

	for _, operator := range operators {
		names = append(names, string(operator))
	}

	return names
}

// subexpressions names how many subexpressions stood where a fixed number
// belongs.
func subexpressions(read int) string {
	switch read {
	case 0:
		return "none"
	case 1:
		return "one"
	default:
		return strconv.Itoa(read)
	}
}

// timesShortfall names what a `times` carries where a subexpression and a count
// belong.
//
// One element is two different mistakes, and which one it is is what the element
// says: a message telling an adopter they wrote a subexpression when they wrote
// a count sends them to add the wrong half back.
func timesShortfall(elements []layout.Node) string {
	switch {
	case len(elements) == 0:
		return "neither"
	case len(elements) > 1:
		return "several"
	case isItemRef(elements[0]):
		return "a count and no subexpression"
	default:
		return "a subexpression and no count"
	}
}

// whenShortfall is the same for a `when`, which takes three things rather than
// two.
func whenShortfall(elements []layout.Node) string {
	switch len(elements) {
	case 0:
		return "none of the three"
	case 1:
		return "an item and nothing to test it against"
	case 2:
		return "an item and a value and no subexpression"
	default:
		return "several"
	}
}
