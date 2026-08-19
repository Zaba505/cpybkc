// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package resolve

import (
	"errors"

	"github.com/Zaba505/cobol-go/copybook"
	"github.com/Zaba505/cobol-go/picture"

	"github.com/Zaba505/cpybkc/internal/diag"
	"github.com/Zaba505/cpybkc/internal/layout"
	"github.com/Zaba505/cpybkc/internal/layoutmodel"
)

// ErrNilRecord is returned by [Resolve] when it is handed no record.
var ErrNilRecord = errors.New("resolve: nil record")

// Options are what resolving a record needs beyond the copybook item itself.
type Options struct {
	// Copybook is the copybook's path as the layout spells it, carried so
	// that a diagnostic can name the file the fault is in. It is not resolved
	// against anything here: where a copybook was looked for is the CLI's,
	// and a diagnostic naming a path the adopter never wrote sends them
	// looking for a file they have not got.
	Copybook string

	// Dialect is the compiler-side half of the layout: the binary width
	// staircase, whether SYNCHRONIZED inserts slack, what an oversized
	// REDEFINES is, and the widths of INDEX and POINTER. It has no default
	// here for the reason `cobol-go` gives it none: a wrong layout setting is
	// not visible in the result.
	//
	// One member of it survives this package: the binary width staircase is
	// copied onto every [Record] and travels into the IR from there
	// (docs/ir/SPEC.md, "A binary item's width is the staircase, not the
	// digits"). The rest do not, and the difference is not an oversight.
	// SYNCHRONIZED, the REDEFINES rule and the two item widths are settled
	// *here* — they decide where slack goes, which alternatives become records
	// and how many bytes an INDEX item takes, and every one of those decisions
	// leaves the resolved tree fully describing itself. The staircase is
	// settled here too, but a consumer has to apply it again to read the bytes,
	// and a consumer that had to be told it a second way is one that could be
	// told it differently.
	Dialect copybook.Dialect

	// Framing is the layout's physical framing, or nil where the caller has
	// none to state. What is read here is what the dataset requires of a
	// record's extent and nothing else: [layoutmodel.Framing.LRECLBound],
	// and the `lrecl` behind it.
	//
	// The rest of the framing is not this package's. A delimiter, a block
	// size and a maximum segment describe the bytes around a record or the
	// dataset it came out of, and neither moves an item inside one; they
	// reach the file node, which is #38's. The whole value is taken all the
	// same, because the bound is derived from the record format and the
	// `lrecl` together and a caller decomposing it here would be a second
	// reading of docs/layout/SPEC.md's table.
	//
	// It is a pointer because a layout that states no framing at all is a
	// layout this package still resolves: nothing about a record's members
	// depends on one, and the check below is simply not run. A zero
	// [layoutmodel.Framing] states no `lrecl` and means the same thing.
	Framing *layoutmodel.Framing

	// Reading is which of the two vendor readings of `OCCURS DEPENDING ON`
	// the layout says its file was written under.
	//
	// It has no default, and [layoutmodel.ReadingUnstated] is what a layout
	// that stated neither hands over. Under `odoslide` a table becomes a
	// repetition whose count is a reference and every item behind it moves
	// with that count; under `noodoslide` the same clause describes a fixed
	// table at the copybook's declared maximum beside a field saying how many
	// entries the writing program filled. The two put every item behind the
	// table somewhere different and nothing in the file disagrees with the
	// wrong one, so a record carrying the clause under an unstated reading is
	// rejected rather than resolved either way (docs/ir/SPEC.md, "An item
	// after a table slides, and the other reading is a fixed table", #87).
	//
	// A copybook carrying no such clause needs no reading, and one stated for
	// it is ignored: the setting is about tables and a record with none is
	// resolved identically under both.
	Reading layoutmodel.Reading

	// Redefines says how each REDEFINES inside a repeating group is told
	// apart. A redefine outside one needs no entry: its alternatives become
	// records, and [Resolve] returns one per combination.
	Redefines []Redefine

	// Encoding is the layout's encoding profile: the four axes every field of
	// the record is resolved against.
	//
	// It has no default, for the reason [Options.Dialect] has none and
	// `codec/SPEC.md` requires all four of them from its caller. Each axis
	// fails silently when it is wrong — a charset yields a plausible string
	// and a byte order a plausible number, with nothing in the file to
	// disagree — so a profile stating fewer than four is rejected here rather
	// than completed.
	Encoding layoutmodel.Axes

	// EncodingOverrides are the layout's per-item encoding overrides, each
	// already resolved to the copybook item it names.
	//
	// They arrive against fields rather than as the layout's item references
	// for [Redefine]'s reason: resolving a reference against a copybook is one
	// step and resolving the copybook is another, and a package doing both
	// would read a layout's spelling of an item in two places. An override
	// naming an item of some other record is one this record never meets, so
	// a caller may hand over the layout's whole list.
	EncodingOverrides []EncodingOverride
}

// EncodingOverride is what a layout restates about one item's encoding.
//
// docs/layout/SPEC.md's "An override is per item, and there is no second
// profile" is the whole of it: the axes it states replace the profile's for the
// item it names, and the ones it leaves unstated leave the profile's alone.
type EncodingOverride struct {
	// Pos is the `encoding-override` form in the layout.
	//
	// It is here because one fault about an override is about the item it
	// reached rather than about the form itself — `(charset none)` over an item
	// whose bytes are characters — and a diagnostic naming only the copybook
	// would name the half of the pair an adopter may not own. A caller that
	// assembled [Options] by hand and left it unstated gets a diagnostic that
	// names the copybook alone, which is all there is to name.
	Pos layout.Pos

	// Item is the copybook item the override names.
	//
	// It **MAY** be a group, in which case the override reaches every
	// elementary item under it, and it **MAY** repeat, in which case it
	// reaches every occurrence: an encoding is a property of an item's bytes
	// wherever those bytes sit.
	Item *copybook.Field

	// Axes are the axes it restates. An axis it leaves unstated is not
	// restated and is not reset — [layoutmodel.Axes.Over] is the application
	// — so an override naming one axis leaves the other three where they
	// were.
	Axes layoutmodel.Axes
}

// Redefine is what a layout says about one REDEFINES inside a repeating group.
//
// It is input rather than something inferred from the copybook because the one
// thing a layout says about a redefine is which alternative to read — never what
// a redefine is, where its bytes are, or which items overlay which. Two or more
// alternatives make a variant. Exactly one says every occurrence takes that
// alternative, and resolves to that alternative's items with no variant emitted
// at all (docs/ir/SPEC.md, "A variant is chosen once per occurrence").
type Redefine struct {
	// Item is the redefined item — the first alternative, the one every
	// REDEFINES of it names. It is a field of the record being resolved.
	Item *copybook.Field

	// Alternatives are the alternatives, in the order they are to be
	// evaluated.
	Alternatives []Alternative
}

// Alternative is one alternative of a [Redefine].
type Alternative struct {
	// Name is the name the copybook gives the item: the redefined item's own
	// name for the first alternative, and a redefining item's for the rest.
	Name string

	// Predicate is the strategy that selects it. It is required where the
	// redefine has two or more alternatives — an arm chosen by nothing is not
	// a thing an alternation can mean — and ignored where it has one, because
	// nothing is being chosen.
	Predicate layoutmodel.Strategy
}

// Resolve resolves record into one [Record] per combination of the alternatives
// its REDEFINES clauses admit outside a repeating group.
//
// record is a field [github.com/Zaba505/cobol-go/copybook.Build] returned: a
// level-01 record or a level-77 standalone item. Widths come from each
// elementary item's PICTURE, USAGE and the dialect, per codec/SPEC.md, "Storage
// Widths", and an item carrying no logical value a generator can use —
// numeric-edited, national, INDEX, POINTER — still carries one, so that the
// position sum stays correct across it.
//
// A record whose copybook holds no REDEFINES outside a repeating group resolves
// to exactly one Record. The order is the order the alternatives are met walking
// the record, outermost and earliest first.
//
// Every field node comes back with all four encoding axes stated, per
// docs/ir/SPEC.md, "The encoding profile, applied": [Options.Encoding] with the
// overrides reaching that item applied over it. A profile stating fewer than
// four is refused before the walk starts, and nothing that leaves here states
// fewer.
//
// Every byte of a record that no item occupies is a slack node in the
// containment order, whatever left it uncovered — the gap ahead of a
// SYNCHRONIZED item, the storage a REDEFINES alternative does not fill, the
// padding between one occurrence of a table and the next, and the tail of a
// record whose items stop short of the `lrecl` [Options.Framing] states. One
// node per maximal run: two of those that abut are one node of the summed
// width, and the one edge a run does not cross is an arm's (docs/ir/SPEC.md,
// "Slack is a node, not a rule").
//
// Every fault it finds is reported, joined with [errors.Join] and assertable
// with [errors.As], rather than the first: a copybook and the layout over it go
// wrong in the same way in several places at once. A record it found a fault in
// is not returned — a half-resolved record is one a caller can read, and there
// is no reading of a copybook that was rejected.
func Resolve(record *copybook.Field, opts Options) ([]*Record, error) {
	if record == nil {
		return nil, ErrNilRecord
	}

	// The profile is held to before anything is laid out, and on its own
	// rather than joined with what the copybook turns out to be wrong about.
	// An axis nobody stated is a hole in every field of the record at once, so
	// resolving anyway would report the one fault once per field and bury the
	// copybook's own faults among them.
	if missing := opts.Encoding.Missing(); len(missing) > 0 {
		return nil, &IncompleteProfileError{Record: record.Name, Axes: missing}
	}

	layout, err := copybook.NewLayout(record, opts.Dialect)
	if err != nil {
		return nil, err
	}

	r := &resolver{opts: opts, record: record, layout: layout}

	items := layout.Items()
	if odo := tables(items); len(odo) > 0 {
		// The reading is held to before anything is laid out and on its
		// own, for the profile's reason: it decides what every item behind
		// every table in the record resolves to, so resolving without one
		// would report a resolution nobody chose and bury the copybook's
		// own faults among its consequences.
		if !opts.Reading.Stated() {
			r.requireReading(odo)

			return nil, r.faults.Err()
		}

		// The count-reference rules are ordinary faults and are joined with
		// whatever else the walk finds, rather than returned on their own.
		// A copybook and the layout over it go wrong in the same way in
		// several places at once, and a count in the wrong place does not
		// stop the record being laid out — `cobol-go` already computed it —
		// so stopping here would be one fault per run over a copybook this
		// package can say more about.
		if opts.Reading.Slides() {
			r.checkCounts(items, odo)
		}
	}

	options := r.item(layout.Record, opts.Encoding)
	if r.faults.Failed() {
		return nil, r.faults.Err()
	}

	records := make([]*Record, 0, len(options))
	for _, option := range options {
		root := option.node
		if root.Kind != KindGroup {
			// A level-77 standalone item is a record whose top level is
			// elementary, and the IR's top level is a group whatever the
			// copybook wrote: it is where the slack goes.
			root = &Node{Kind: KindGroup, Field: record, Members: []*Node{root}}
		}
		records = append(records, &Record{
			Root:         root,
			Item:         record,
			Alternatives: option.alternatives,
			Binary:       opts.Dialect.Binary,
		})
	}

	r.frame(records)
	r.assertResolved(records)
	if r.faults.Failed() {
		return nil, r.faults.Err()
	}
	return records, nil
}

// frame holds each resolved record to what the dataset's framing requires of its
// extent, and carries the difference as slack where that is what the requirement
// means.
//
// Two rules, and they are keyed on different things. The variable-extent
// rejection is keyed on the framing being **unframed**, because it holds
// whatever `lrecl` a layout states and whether or not it states one; the extent
// check is keyed on [layoutmodel.Framing.LRECLBound], which is the `lrecl` and
// the record format read together (docs/layout/SPEC.md, "`lrecl` and `blksize`
// describe the dataset, not the stream").
//
// Under [layoutmodel.LRECLExact] a record type accounts for all of LRECL, so a
// record type whose items stop short carries the difference as a trailing slack
// node — the fourth producer of slack, beside an alignment gap, a REDEFINES tail
// and a table's stride padding, and indistinguishable from any of them once
// emitted (#26, #34). It is appended through [mergeSlack] for that reason: a
// record whose last member is already slack ends in one run of the summed width
// and not in two nodes that abut.
func (r *resolver) frame(records []*Record) {
	framing := r.opts.Framing
	if framing == nil {
		return
	}

	fixedLength := framing.Kind() == layoutmodel.Unframed
	bound, lrecl := framing.LRECLBound(), int(framing.LRECL.Value)

	for _, record := range records {
		if node, item := variableTerm(record); fixedLength && node != nil {
			// One fault per record: a record type that does not fit the
			// dataset at all has no extent for the check below to be
			// about.
			r.faults.Fail(&VariableExtentError{
				Pos:    layoutSpan(framing.Pos),
				Item:   r.span(item),
				Record: r.record.Name,
				RECFM:  framing.RECFM,
				Table:  itemName(item),
				Count:  itemName(node.Repetition.DependingOn),
			})
			continue
		}

		if bound == layoutmodel.LRECLUnstated {
			continue
		}

		extent := record.Extent()
		switch {
		case extent > lrecl:
			r.faults.Fail(&LRECLExtentError{
				Pos:          layoutSpan(framing.LRECL.Pos),
				Item:         r.span(r.record),
				Record:       r.record.Name,
				Alternatives: alternativeNames(record),
				Bound:        bound,
				Extent:       extent,
				LRECL:        lrecl,
			})
		case extent < lrecl && bound == layoutmodel.LRECLExact:
			// A record type shorter than LRECL is padded rather than
			// reported: the bytes are in the file whatever the copybook
			// says, and the next record begins after them.
			members := record.Root.Members
			record.Root.Members = mergeSlack(append(members[:len(members):len(members)], slackNode(lrecl-extent)))
		}
	}
}

// variableTerm returns the first node of the record whose repetition count is
// read out of the record rather than written in the copybook, together with the
// copybook item it stands for. Both are nil where the record's extent is a
// constant.
//
// The item is not always the node's own. An elementary item padded out to its
// stride is wrapped in a group this package introduced, and it is the wrapper
// that carries the repetition while the field inside it carries the name.
func variableTerm(record *Record) (*Node, *copybook.Field) {
	var found *Node
	var item *copybook.Field

	record.Walk(func(node *Node) {
		if found != nil || !node.Repetition.Reference() {
			return
		}

		found = node
		item = node.Field
		if item == nil && len(node.Members) > 0 {
			item = node.Members[0].Field
		}
	})
	return found, item
}

// alternativeNames is what the record's alternatives are called, so that a
// diagnostic about one alternative of a copybook says which. It is empty for a
// copybook holding no REDEFINES outside a repeating group, where the record's
// own name is already unambiguous.
func alternativeNames(record *Record) []string {
	out := make([]string, 0, len(record.Alternatives))
	for _, alternative := range record.Alternatives {
		out = append(out, itemName(alternative))
	}
	return out
}

// assertResolved holds every field of the records to docs/ir/SPEC.md's "The
// encoding profile, applied": all four axes stated, on every field node, with
// none left unset.
//
// It cannot fire against a profile this package accepted — [Options.Encoding] is
// complete before the walk starts and [layoutmodel.Axes.Over] never unsets an
// axis — and running it anyway is the point. The requirement is on what leaves
// resolution rather than on what entered it, and the repair a consumer would
// otherwise reach for is the one docs/ir/SPEC.md forbids: a field missing an
// axis is a malformed descriptor and a generator **MUST NOT** fill it in, so an
// unresolved axis that escapes here has nowhere downstream to be caught. The
// assertion is therefore over the result, in the terms the requirement is
// written in, and a resolution this package grows later is checked by it without
// being asked to remember.
func (r *resolver) assertResolved(records []*Record) {
	for _, record := range records {
		record.Walk(func(node *Node) {
			if node.Kind != KindField {
				return
			}

			missing := node.Encoding.Missing()
			if len(missing) == 0 {
				return
			}

			r.faults.Fail(&UnresolvedEncodingError{
				Pos:    r.span(node.Field),
				Record: r.record.Name,
				Item:   itemName(node.Field),
				Axes:   missing,
			})
		})
	}
}

// resolver carries what the walk needs: the options it was given, the record it
// is resolving — which every diagnostic names — and the faults found so far.
type resolver struct {
	opts   Options
	record *copybook.Field
	faults diag.List

	// layout is the record laid out, which is what an arm's predicate target
	// is resolved against: a reference names a path of data names and only the
	// layout says which item that is, at what offset and of what width.
	layout *copybook.Layout
}

// option is one resolution of one item: the node it became, and the alternatives
// chosen to get there.
//
// An item resolves to more than one option exactly where a REDEFINES outside a
// repeating group stands under it, which is what makes a record's alternatives
// multiply rather than a record carrying a choice.
type option struct {
	node         *Node
	alternatives []*copybook.Field
}

// item resolves one laid-out item into every option it admits, under the
// encoding governing the item above it.
func (r *resolver) item(item *copybook.Item, in layoutmodel.Axes) []option {
	in = r.encoding(item.Field, in)

	if item.Field.Kind != copybook.KindGroup {
		return []option{{node: r.elementary(item, in)}}
	}
	return r.group(item, in)
}

// encoding is the encoding governing field: the override naming it, where the
// layout wrote one, over the encoding already in effect where it sits.
//
// Over the encoding in effect and not over the profile, which is the one thing
// about overrides docs/layout/SPEC.md leaves to be settled here. An override
// reaches every elementary item under the group it names, so a field under an
// overridden group is already governed by that override when its own is met;
// applying the inner one over the profile instead would let an override naming
// one axis silently undo an enclosing override of another, which is the opposite
// of what "leaves the other three as the profile states them" is for. Composed,
// that sentence holds at every depth, and an axis nobody restated anywhere is
// still the profile's.
//
// The first entry naming an item wins, as [resolver.redefine] takes the first.
// `layoutmodel` already refuses a layout that overrides one item twice, so a
// second entry here is a caller that assembled one by hand, and reporting it as
// the adopter's fault would name a line the adopter wrote correctly.
func (r *resolver) encoding(field *copybook.Field, in layoutmodel.Axes) layoutmodel.Axes {
	for i := range r.opts.EncodingOverrides {
		if r.opts.EncodingOverrides[i].Item == field {
			return r.opts.EncodingOverrides[i].Axes.Over(in)
		}
	}
	return in
}

// elementary builds the node for an item that holds no others, under the
// encoding governing it.
//
// A repeating item whose stride exceeds its width has padding between one
// occurrence and the next, and an elementary node has nowhere to put a slack
// node. So it becomes a group that repeats, holding the item and the padding —
// which is the same shape the IR would need for it, since alignment reaches a
// generator as bytes it already has rather than as a rule it applies. The
// encoding stays on the field node inside it: the wrapper is this package's own
// and stands for no copybook item, and the padding is not a field's bytes.
func (r *resolver) elementary(item *copybook.Item, in layoutmodel.Axes) *Node {
	r.checkCharsetNone(item.Field, in)

	field := &Node{
		Kind:       KindField,
		Field:      item.Field,
		width:      item.Length,
		Repetition: r.repetitionOf(item),
		Encoding:   in,
	}
	if item.Stride == item.Length {
		return field
	}

	repetition := field.Repetition
	field.Repetition = nil
	return &Node{
		Kind:       KindGroup,
		Members:    []*Node{field, slackNode(item.Stride - item.Length)},
		Repetition: repetition,
	}
}

// checkCharsetNone holds an item whose charset resolved to [layoutmodel.None] to
// being an item that charset value can mean something for.
//
// It is `resolve`'s rather than `layoutmodel`'s because it is the one question
// about an override that needs the copybook open: whether the item the layout
// named is a payload or is characters is decided by its USAGE and its PICTURE,
// and `layoutmodel` never opens a copybook. It is checked here rather than over
// the finished records because the walk visits each item once and arrives
// carrying the encoding in effect at it, which is what an inner override
// restoring a code page has already changed.
//
// Three answers, and which one an item gets is [CharsetNoneError]'s subject:
// legal on an alphanumeric DISPLAY item, a fault on any other DISPLAY item, and
// inert on every usage the charset does not govern — where `none` is as
// unremarkable as `cp037` is, which is what lets an override name a group and
// reach the text under it without the packed and binary items beside it
// becoming errors.
//
// An item with no PICTURE at all takes the inert answer. USAGE INDEX, POINTER,
// COMP-1 and COMP-2 are the items COBOL declares without one, and every one of
// them is a usage the charset does not govern anyway.
func (r *resolver) checkCharsetNone(field *copybook.Field, in layoutmodel.Axes) {
	if in.Charset != layoutmodel.None || field == nil {
		return
	}

	// A FILLER is skipped for the reason [CharsetNoneError] gives: nothing reads
	// its charset and no item reference can name it, so the diagnostic would be
	// about an item the adopter has no way to say anything about.
	if field.Filler || field.Name == "" {
		return
	}

	if field.Usage != copybook.UsageDisplay || field.Picture == nil {
		return
	}

	if field.Picture.Category == picture.CategoryAlphanumeric {
		return
	}

	r.faults.Fail(&CharsetNoneError{
		Pos:      layoutSpan(r.charsetOrigin(field)),
		Item:     r.span(field),
		Record:   r.record.Name,
		Name:     itemName(field),
		Category: field.Picture.Category,
	})
}

// charsetOrigin is where the layout wrote the charset governing field: the
// nearest `encoding-override` at or above it that states the axis.
//
// It walks the entry tree the way [resolver.encoding] composes down it, and
// takes the first entry naming each item for the reason that one does. The
// nearest override stating the axis is the one whose value survived, so an
// override deeper in the tree that restored a code page is never the one named:
// its item is not the one being reported.
//
// It comes back unstated where nothing above the field states a charset, which
// is a caller who built [Options] by hand — the profile cannot say `none`.
func (r *resolver) charsetOrigin(field *copybook.Field) layout.Pos {
	for item := field; item != nil; item = item.Parent {
		for i := range r.opts.EncodingOverrides {
			if r.opts.EncodingOverrides[i].Item != item {
				continue
			}

			if r.opts.EncodingOverrides[i].Axes.Charset != "" {
				return r.opts.EncodingOverrides[i].Pos
			}

			break
		}
	}

	return layout.Pos{}
}

// run is what one redefine cluster contributes to its group's member list: the
// nodes themselves, and the alternatives chosen to arrive at them.
//
// A cluster contributes more than one node where the alternative chosen does not
// fill the storage the cluster reserved, which is where the slack behind it goes.
type run struct {
	nodes        []*Node
	alternatives []*copybook.Field
}

// group resolves a group item into every option its member list admits, under
// the encoding governing the group.
//
// The member list is built cluster by cluster — an item and the items redefining
// it are one cluster — and every byte of the group's extent no cluster covers
// becomes slack, so that the members sum to the extent exactly and the position
// of everything behind them is the sum and nothing else.
func (r *resolver) group(item *copybook.Item, in layoutmodel.Axes) []option {
	lists := []run{{}}
	cursor := item.Offset

	for _, c := range clustersOf(item) {
		gap := c.start() - cursor
		choices := r.cluster(c, in)
		cursor = c.start() + c.extent()

		next := make([]run, 0, len(lists)*len(choices))
		for _, list := range lists {
			for _, choice := range choices {
				members := make([]*Node, 0, len(list.nodes)+len(choice.nodes)+1)
				members = append(members, list.nodes...)
				if gap > 0 {
					members = append(members, slackNode(gap))
				}
				members = append(members, choice.nodes...)

				chosen := make([]*copybook.Field, 0, len(list.alternatives)+len(choice.alternatives))
				chosen = append(chosen, list.alternatives...)
				chosen = append(chosen, choice.alternatives...)

				next = append(next, run{nodes: members, alternatives: chosen})
			}
		}
		lists = next
	}

	// One occurrence of the group is its stride, not its length: a repeating
	// group padded out so that the next occurrence starts on its boundary
	// carries that padding inside itself, where the sum can see it.
	trailing := item.Offset + item.Stride - cursor

	options := make([]option, 0, len(lists))
	for _, list := range lists {
		members := list.nodes
		if trailing > 0 {
			members = append(members[:len(members):len(members)], slackNode(trailing))
		}
		options = append(options, option{
			node: &Node{
				Kind:       KindGroup,
				Field:      item.Field,
				Members:    mergeSlack(members),
				Repetition: r.repetitionOf(item),
			},
			alternatives: list.alternatives,
		})
	}
	return options
}

// cluster resolves one redefine cluster into every run its group's member list
// admits, under the encoding governing the group holding it.
func (r *resolver) cluster(c cluster, in layoutmodel.Axes) []run {
	if len(c.members) == 1 {
		options := r.item(c.members[0], in)
		runs := make([]run, 0, len(options))
		for _, chosen := range options {
			runs = append(runs, run{nodes: []*Node{chosen.node}, alternatives: chosen.alternatives})
		}
		return runs
	}

	if inTable(c.members[0]) {
		return []run{r.variant(c, in)}
	}

	// Outside a repeating group every alternative becomes its own record, so
	// the cluster multiplies the options rather than becoming a node.
	var runs []run
	for _, member := range c.members {
		for _, chosen := range r.item(member, in) {
			alternatives := make([]*copybook.Field, 0, len(chosen.alternatives)+1)
			alternatives = append(alternatives, chosen.alternatives...)
			alternatives = append(alternatives, member.Field)
			runs = append(runs, run{
				nodes:        pad(chosen.node, c.extent()),
				alternatives: alternatives,
			})
		}
	}
	return runs
}

// variant resolves a redefine cluster inside a repeating group.
//
// The layout says which alternatives there are and what selects each one. Two or
// more make a variant; one says every occurrence takes it, and resolves to that
// alternative's items with no variant at all.
func (r *resolver) variant(c cluster, in layoutmodel.Axes) run {
	redefined := c.members[0]
	table := enclosingTable(redefined)

	spec := r.redefine(redefined.Field)
	if spec == nil {
		r.faults.Fail(&UndiscriminatedRedefineError{
			Pos:       r.span(redefined.Field),
			Record:    r.record.Name,
			Group:     groupName(table),
			Redefined: itemName(redefined.Field),
			Names:     names(c.members[1:]),
		})
		return r.base(c, in)
	}

	// The redefined item's storage is what an occurrence reserved, and it is
	// what every arm has to fit in. A dialect lenient enough to let a
	// redefinition grow its group cannot make that true one entry at a time,
	// so an arm needing more bytes is the layout this package rejects rather
	// than one it pads.
	extent := redefined.Total()

	arms := make([]Arm, 0, len(spec.Alternatives))
	targets := make([]*copybook.Item, 0, len(spec.Alternatives))

	for _, alternative := range spec.Alternatives {
		member := c.find(alternative.Name)
		if member == nil {
			r.faults.Fail(&UnknownAlternativeError{
				Pos:         r.span(redefined.Field),
				Record:      r.record.Name,
				Redefined:   itemName(redefined.Field),
				Alternative: alternative.Name,
				Names:       names(c.members),
			})
			continue
		}

		switch counted := r.referenceCount(member); {
		case member.Total() > extent:
			r.faults.Fail(&ArmExtentError{
				Pos:       r.span(member.Field),
				Record:    r.record.Name,
				Group:     groupName(table),
				Redefined: itemName(redefined.Field),
				Arm:       itemName(member.Field),
				Extent:    member.Total(),
				Want:      extent,
			})
			continue

		case counted != nil:
			r.faults.Fail(&ArmVariableCountError{
				Pos:       r.span(counted.Field),
				Record:    r.record.Name,
				Group:     groupName(table),
				Redefined: itemName(redefined.Field),
				Arm:       itemName(member.Field),
				Item:      itemName(counted.Field),
				Count:     itemName(counted.DependingOn.Field),
			})
			continue

		case len(spec.Alternatives) > 1 && !alternative.Predicate.Predicate():
			r.faults.Fail(&ArmPredicateError{
				Pos:       r.span(member.Field),
				Record:    r.record.Name,
				Group:     groupName(table),
				Redefined: itemName(redefined.Field),
				Arm:       itemName(member.Field),
			})
			continue
		}

		// A redefine with one alternative chooses nothing, so its strategy is
		// ignored rather than compiled: there is no arm for a predicate to
		// select and a target held to an arm's rules would be held to rules
		// about a choice nobody is making.
		var predicate *Predicate
		var target *copybook.Item

		if len(spec.Alternatives) > 1 {
			if predicate, target = r.armPredicate(c, table, alternative); predicate == nil {
				continue
			}
		}

		arms = append(arms, Arm{
			Alternative: alternative.Name,
			Predicate:   predicate,
			Body:        armBody(r.first(member, in), extent),
		})
		targets = append(targets, target)
	}

	switch {
	case len(spec.Alternatives) == 0:
		r.faults.Fail(&ArmCountError{
			Pos:       r.span(redefined.Field),
			Record:    r.record.Name,
			Group:     groupName(table),
			Redefined: itemName(redefined.Field),
		})
		return r.base(c, in)

	case len(spec.Alternatives) == 1:
		// Nothing is chosen, so there is no alternation to carry and no
		// predicate to carry it under: the one alternative's items stand
		// where the cluster stood, padded out like a record's.
		if len(arms) == 0 {
			return r.base(c, in)
		}
		member := c.find(spec.Alternatives[0].Name)
		return run{
			nodes:        pad(r.first(member, in), c.extent()),
			alternatives: []*copybook.Field{member.Field},
		}
	}

	if len(arms) < len(spec.Alternatives) {
		// A fault was already reported for the arms that are missing, and a
		// variant short of them would be a second, less useful one.
		return r.base(c, in)
	}

	r.checkArmOverlap(c, table, arms, targets, spec)

	return run{nodes: pad(&Node{Kind: KindVariant, Arms: arms}, c.extent())}
}

// base is the run a cluster contributes when a fault stopped it from resolving:
// the redefined item, which is the copybook's own first description of those
// bytes. It keeps the walk going so that a second fault is reported beside the
// first, and nothing is returned to a caller while a fault stands.
func (r *resolver) base(c cluster, in layoutmodel.Axes) run {
	return run{nodes: pad(r.first(c.members[0], in), c.extent())}
}

// first resolves an item to its one option.
//
// Inside a repeating group there is only ever one: a redefine down there is
// itself in a repeating group, so it becomes a variant rather than multiplying
// the record's alternatives.
func (r *resolver) first(item *copybook.Item, in layoutmodel.Axes) *Node {
	options := r.item(item, in)
	if len(options) == 0 {
		return &Node{Kind: KindGroup, Field: item.Field}
	}
	return options[0].node
}

// redefine finds the layout's word on a redefined item.
func (r *resolver) redefine(field *copybook.Field) *Redefine {
	for i := range r.opts.Redefines {
		if r.opts.Redefines[i].Item == field {
			return &r.opts.Redefines[i]
		}
	}
	return nil
}

// layoutSpan is where in the layout a form is: a framing's `lrecl`, a
// sequencing operator's item reference, the `sequence` form itself.
//
// It is the other file a fault about a record implicates: a number comes from
// the dataset the adopter wrote down and an extent from a copybook they may not
// own, so a diagnostic naming only one of the two names the one they cannot
// change (docs/layout/SPEC.md, "Every diagnostic carries a span").
func layoutSpan(pos layout.Pos) diag.Span {
	return diag.Span{File: pos.File, Line: pos.Line, Column: pos.Column}
}

// span is where in the copybook a field is.
//
// A node this package introduced stands for no field and has no line: what comes
// back for one is the copybook itself, which diag renders as the file alone
// rather than as a line zero nothing can jump to.
func (r *resolver) span(field *copybook.Field) diag.Span {
	span := diag.Span{File: r.opts.Copybook}
	if field != nil {
		span.Line, span.Column = field.Pos.Line, field.Pos.Column
	}
	return span
}

// pad returns the nodes a member list gets for node, followed by the slack
// covering the bytes of extent it does not occupy.
//
// This is the record-level half of resolving a redefine away: the alternative's
// items stand where the cluster stood, and the storage the copybook gave the
// alternatives jointly that this one does not use is slack. Those bytes are not
// padding — in a file written by a program that used another alternative they
// hold that alternative's data.
func pad(node *Node, extent int) []*Node {
	if gap := extent - node.Extent(); gap > 0 {
		return []*Node{node, slackNode(gap)}
	}
	return []*Node{node}
}

// armBody builds an arm's body, padded to the variant's extent.
//
// The slack has to sit inside the arm rather than behind it: a run of uncovered
// bytes stops at the edge of an arm, and one node covering both would belong to
// neither list and would make the arm wider than its siblings. A group that does
// not repeat can hold it among its own members; anything else — an elementary
// alternative above all — is wrapped in a group that carries no names, because
// the alternative's name is on the node inside it.
func armBody(node *Node, extent int) *Node {
	gap := extent - node.Extent()
	if gap <= 0 {
		return node
	}
	if node.Kind == KindGroup && node.Repetition == nil {
		node.Members = mergeSlack(append(node.Members, slackNode(gap)))
		return node
	}
	return &Node{Kind: KindGroup, Members: []*Node{node, slackNode(gap)}}
}

// mergeSlack collapses each run of abutting slack nodes into one, so that two
// runs of different origin that abut are one node and not two.
//
// It costs nothing today and stops costing something later: a record carries one
// run of bytes per slack node, so how a producer divides a run of eight into
// nodes would otherwise decide the shape of every record a generator emits.
func mergeSlack(nodes []*Node) []*Node {
	merged := make([]*Node, 0, len(nodes))
	for _, node := range nodes {
		last := len(merged) - 1
		if node.Kind == KindSlack && last >= 0 && merged[last].Kind == KindSlack {
			merged[last] = slackNode(merged[last].width + node.width)
			continue
		}
		merged = append(merged, node)
	}
	return merged
}

// inTable reports whether item is contained, at any depth, in a group that
// repeats. It is the whole of what decides which resolution a REDEFINES takes.
func inTable(item *copybook.Item) bool { return enclosingTable(item) != nil }

// enclosingTable returns the innermost group above item that repeats, or nil
// where none does.
func enclosingTable(item *copybook.Item) *copybook.Item {
	for parent := item.Parent; parent != nil; parent = parent.Parent {
		if parent.MaxOccurs > 1 {
			return parent
		}
	}
	return nil
}

// cluster is an item and the items redefining it: one run of storage with more
// than one description of it.
type cluster struct {
	members []*copybook.Item
}

// start is the byte the cluster's storage begins at. Every member of a cluster
// starts there — that is what REDEFINES means.
func (c cluster) start() int { return c.members[0].Offset }

// extent is the bytes the cluster's storage covers: the longest description of
// it, which under a strict dialect is always the redefined item's own.
func (c cluster) extent() int {
	widest := 0
	for _, member := range c.members {
		if total := member.Total(); total > widest {
			widest = total
		}
	}
	return widest
}

// find returns the member named name, or nil.
func (c cluster) find(name string) *copybook.Item {
	for _, member := range c.members {
		if !member.Field.Filler && member.Field.Name == name {
			return member
		}
	}
	return nil
}

// clustersOf partitions a group's children into clusters, in the order the
// redefined items appear.
//
// A REDEFINES names a preceding sibling, and a chain of them — B redefining A
// and C redefining B — describes one run of storage three ways, so the chain is
// walked to its root rather than each link being its own cluster.
func clustersOf(item *copybook.Item) []cluster {
	var clusters []cluster
	index := make(map[*copybook.Item]int, len(item.Children))

	for _, child := range item.Children {
		base := child
		for base.Redefines != nil {
			base = base.Redefines
		}
		if at, ok := index[base]; ok {
			clusters[at].members = append(clusters[at].members, child)
			continue
		}
		index[base] = len(clusters)
		clusters = append(clusters, cluster{members: []*copybook.Item{child}})
	}
	return clusters
}

// itemName is what a diagnostic calls a field: its name, or FILLER where it has
// none.
func itemName(field *copybook.Field) string {
	if field == nil {
		return ""
	}
	if field.Filler || field.Name == "" {
		return "FILLER"
	}
	return field.Name
}

// groupName is what a diagnostic calls the repeating group a variant sits in.
func groupName(item *copybook.Item) string {
	if item == nil {
		return ""
	}
	return itemName(item.Field)
}

// names is what the items are called, for a diagnostic listing the alternatives
// an adopter could have meant.
func names(items []*copybook.Item) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, itemName(item.Field))
	}
	return out
}
