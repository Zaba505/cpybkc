// Copyright (c) 2026 Richard Carson Derr
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Zaba505/cpybkc/irpb"
)

// fileTestFile is the file tier of the generated tests, beside the file it
// covers.
//
// Named after file.go for the reason [recordsTestFile] is named after
// records.go: what it asserts is what that file does — the framing around a
// record, and the order records come in. README.md's "The names, and which side
// moves" is where the whole of that decision is, including which of this
// repository's own files moved out of the way of it.
const fileTestFile = "file_test.go"

// runLength is how many times a case walks a record type where the automaton offers
// the same edge again from the state it arrives in.
//
// Two. One is enough to reach the predicate and is shortest to read, and it is
// still not enough to see: a counted run walked once cannot tell a register that
// was decremented from one that was tested, and a file of one record under a
// separator placement carries no separator at all — which is half of the framing
// the golden packages exist to exercise. Two costs one more record in the
// literal and shows both.
//
// A constant rather than an option, because the file a case carries is the
// artifact: a number a run could set is a literal an adopter cannot predict from
// their own layout. It is one line to change.
const runLength = 2

// The identifiers a file-tier case spends, beyond the ones it derives from the
// path it walks.
//
// Same rule as [caseIdentifiers], and a list of its own because the two files
// spell different cases: this one reads a whole file, so it names a slice of
// records and the end of the input, and it never declares the `record` a record
// case does.
var fileCaseIdentifiers = map[string]struct{}{
	"t": {}, "in": {}, "r": {}, "rec": {}, "records": {}, "ok": {}, "err": {},
	"out": {}, "w": {},
	"bytes": {}, "errors": {}, "io": {}, "testing": {}, lastElement(bigIntImport): {},
}

// fileTests is the source of [fileTestFile] for this descriptor, or the empty
// string where its automaton admits no record.
//
// One case per file, and between them they satisfy every transition predicate
// the descriptor carries and every literal of a set-membership one. Each case
// holds a whole file inline — the framing as well as the records — reads it with
// the generated Reader, asserts the record types come back in the order the
// automaton admits them, writes them back with the generated Writer and asserts
// the bytes are the bytes it started from.
//
// Absent for a descriptor whose automaton admits no record, exactly as
// [fileMachine] is: a tier covering a file that is not emitted has nothing to
// cover. Absent too where no field of the descriptor gives an encoding to read.
func fileTests(d *irpb.Descriptor, opts options) (string, error) {
	t, err := newFiletest(d, opts)
	if err != nil {
		return "", err
	}

	// Absent where the automaton admits no record, and where no field of the
	// descriptor gives an encoding to read a case's bytes under. Asked of the
	// built emitter rather than ahead of building one, so that the emission
	// rules are the only thing this function adds to [newFiletest] and the two
	// cannot come to disagree about what a descriptor is.
	if t.file == nil || !t.admits() || t.synth == nil {
		return "", nil
	}

	if opts.importPath == "" {
		return "", fmt.Errorf(
			"%s %s=<path> is required for a descriptor whose automaton admits a record: the generated tests are an external test package, so they import the package beside them by path, and %s names a scratch directory rather than where the files end up",
			optFlag, importPathOption, outFlag)
	}

	return t.source()
}

// newFiletest is the emitter over one descriptor, indexed and with every state's
// transitions resolved.
//
// Separate from [fileTests] so that this package's own tests reach the same
// construction the generator makes rather than a second copy of it, which is a
// copy that drifts. What is left in [fileTests] is the emission rules — when a
// file is written at all — and nothing else.
//
// [filetest.synth] is nil where the descriptor gives no encoding to lay bytes out
// under, which is a descriptor carrying no field.
func newFiletest(d *irpb.Descriptor, opts options) (*filetest, error) {
	e, err := newEmitter(d)
	if err != nil {
		return nil, err
	}

	f := &filer{emitter: e, opts: opts, index: make(map[uint64]int)}

	if err := f.collect(d); err != nil {
		return nil, err
	}

	t := &filetest{filer: f}

	// Nothing to resolve where the descriptor carries no file node: [filer.collect]
	// returns before it indexes the states, so a transition's next state cannot be
	// looked up and the walk would refuse a descriptor that simply has no automaton.
	if f.file == nil {
		return t, nil
	}

	for _, state := range f.states {
		walk, err := f.transitionsOf(state.GetState())
		if err != nil {
			return nil, err
		}

		t.walks = append(t.walks, walk)
	}

	enc, err := descriptorEncoding(d)
	if err != nil {
		return nil, err
	}

	if enc == nil {
		return t, nil
	}

	t.synth, err = newSynth(&coder{emitter: e, receiver: opts.receiverName()}, d, enc)
	if err != nil {
		return nil, err
	}

	return t, nil
}

// filetest writes one descriptor's file-tier cases.
type filetest struct {
	*filer

	// synth lays out each record of a case's file, which is the record tier's
	// synthesizer told what the automaton has already decided.
	synth *synth

	// walks is each state's transitions in the order the state carries them,
	// indexed as [filer.states] is.
	walks [][]transition
}

// source is the whole of [fileTestFile]: the header, the external test
// package's clause, the imports and the cases.
func (t *filetest) source() (string, error) {
	files, err := t.cover()
	if err != nil {
		return "", err
	}

	alias := t.opts.packageName
	if t.spends(alias, files) {
		alias = shadowAlias
	}

	var (
		funcs []string
		used  = make(map[string]struct{})
	)

	for _, one := range files {
		source, err := t.testCase(one, alias, used)
		if err != nil {
			return "", err
		}

		funcs = append(funcs, source)
	}

	// The imports every case takes, and the ones an accepted case asked for.
	// Per accepted case rather than off the shared synthesizer, because
	// [filetest.cover] lays out candidate paths it then refuses: an import a
	// discarded candidate reached for is an import nothing in this file uses,
	// which is generated code that does not compile. Deduplicated for the same
	// reason from the other side — one named twice is the same error.
	imports := map[string]struct{}{
		"bytes": {}, "errors": {}, "io": {}, "testing": {}, t.opts.importPath: {},
	}

	for _, one := range files {
		for _, path := range one.needs {
			imports[path] = struct{}{}
		}
	}

	sorted := make([]string, 0, len(imports))
	for path := range imports {
		sorted = append(sorted, path)
	}

	slices.Sort(sorted)

	var b strings.Builder

	b.WriteString(generatedBy)
	b.WriteString("\n\n")
	b.WriteString(commentLines(fmt.Sprintf(`The file tier of this package's generated tests: one case per path through the
automaton this layout describes, and between them every transition predicate it
carries and every literal of a set-membership one.

Each case carries a whole file as a literal — the framing as well as the records
— reads it with the generated Reader, checks that the record types came back in
the order the automaton admits them, writes them back with the generated Writer
and checks that the bytes are the bytes it started from. The file is synthesized
from the layout rather than read from a dataset, so it is what to hold against
the file on your desk: the comment column names each run, the offset inside its
record, and the picture.

Nothing here is yours to edit — this directory is regenerated whole. Put your
own tests in a package of your own that imports %s.`, t.opts.packageName)))
	b.WriteString("package ")
	b.WriteString(t.opts.packageName)
	b.WriteString("_test\n\nimport (\n")

	for _, path := range sorted {
		if path == t.opts.importPath && alias != lastElement(path) {
			fmt.Fprintf(&b, "%s %q\n", alias, path)

			continue
		}

		fmt.Fprintf(&b, "%q\n", path)
	}

	b.WriteString(")\n")

	for _, source := range funcs {
		b.WriteString("\n")
		b.WriteString(source)
		b.WriteString("\n")
	}

	return b.String(), nil
}

// spends is whether a case's file already writes the identifier, which is what
// the generated package may not be imported under.
//
// The fixed list, and the one identifier that is not fixed: a case declares one
// holder per record of the file it reads, so the names it spends depend on how
// long its longest file is. A generated package's name is the adopter's, so
// `package record2` is a package this generator has to be able to write tests
// for rather than one it may refuse.
func (t *filetest) spends(name string, files []*laid) bool {
	if _, taken := fileCaseIdentifiers[name]; taken {
		return true
	}

	for _, one := range files {
		for at := range one.records {
			if t.holder(at) == name {
				return true
			}
		}
	}

	return false
}

// goal is one thing the cases between them have to exercise: a transition
// predicate, and one literal of it where it tests a set.
//
// By predicate rather than by edge, which is a bound on what these cases prove
// and is stated in README.md for that reason. What an adopter is checking is
// *this record type is selected on these bytes*, which is a property of the
// predicate; the guards that make two edges carrying it distinct are the
// automaton's business, and cpybkc-gen-graph has already drawn them.
type goal struct {
	// predicate is the predicate node, and pick which of its literals. A
	// predicate testing equality carries one, at index zero.
	predicate uint64
	pick      int

	// automaton is set on the one goal a descriptor whose transitions carry no
	// predicate at all has: the file itself.
	automaton bool
}

// goals is everything the cases have to reach, in the order the automaton
// carries it: states in ascending identifier order, and each state's transitions
// in the order it evaluates them.
//
// A descriptor whose transitions carry no predicate has one goal all the same —
// the automaton itself. Its file is told apart by the order records come in
// rather than by any byte in one, and a tier that wrote nothing for it would
// leave the framing of four of the six golden packages uncovered.
func (t *filetest) goals() ([]goal, error) {
	var (
		out  []goal
		seen = make(map[uint64]struct{})
	)

	for _, walk := range t.walks {
		for _, one := range walk {
			if one.node.PredicateId == nil {
				continue
			}

			id := one.node.GetPredicateId()
			if _, done := seen[id]; done {
				continue
			}

			seen[id] = struct{}{}

			values, err := t.literalsOf(id)
			if err != nil {
				return nil, err
			}

			for at := range values {
				out = append(out, goal{predicate: id, pick: at})
			}
		}
	}

	if len(out) == 0 {
		return []goal{{automaton: true}}, nil
	}

	return out, nil
}

// literalsOf is the bytes a predicate admits: one value where it tests equality
// and every member of the set where it tests membership.
func (t *filetest) literalsOf(id uint64) ([][]byte, error) {
	node, ok := t.nodes[id]
	if !ok {
		return nil, unresolved(id)
	}

	predicate := node.GetPredicate()
	if predicate == nil {
		return nil, malformed(fmt.Sprintf("node %d selects a transition and is not a predicate node", id),
			"a transition names the predicate that selects it where it carries one; see docs/ir/SPEC.md, \"Discriminator predicates\"")
	}

	switch test := predicate.GetTest().(type) {
	case *irpb.Predicate_BytesEqual:
		return [][]byte{test.BytesEqual.GetValue()}, nil
	case *irpb.Predicate_BytesOneOf:
		return test.BytesOneOf.GetValues(), nil
	default:
		return nil, malformed("a predicate carries no test",
			"the set is closed and a predicate carries one member of it; see docs/ir/SPEC.md, \"Discriminator predicates\"")
	}
}

// cover is one file per case, in the order the goals were met.
//
// Shortest first, and greedy: each goal not already reached is given the
// shortest file that reaches it, and every goal that file passes through is
// struck off with it. That is what keeps a case readable — a file is as long as
// the path it walks, and the path is the shortest the automaton offers — and it
// is why two predicates on one path cost one case rather than two.
//
// A goal no file reaches is refused rather than skipped. A predicate no case
// covers is one whose spelling an adopter finds out about from a production
// file, and an automaton carrying an edge nothing can take is a bug in whatever
// produced the descriptor rather than a gap this generator may write around.
func (t *filetest) cover() ([]*laid, error) {
	all, err := t.goals()
	if err != nil {
		return nil, err
	}

	var (
		out  []*laid
		done = make(map[goal]struct{})
	)

	for _, want := range all {
		if _, reached := done[want]; reached {
			continue
		}

		one, err := t.reach(want)
		if err != nil {
			return nil, err
		}

		for _, met := range one.meets {
			done[met] = struct{}{}
		}

		out = append(out, one)
	}

	return out, nil
}

// reach is the file that covers one goal, or the diagnostic saying no file does.
func (t *filetest) reach(want goal) (*laid, error) {
	paths, err := t.paths(want)
	if err != nil {
		return nil, err
	}

	var refused []string

	for _, path := range paths {
		one, why, err := t.lay(path)
		if err != nil {
			return nil, err
		}

		if one != nil {
			return one, nil
		}

		refused = append(refused, why)
	}

	return nil, t.unreachable(want, refused)
}

// unreachable is the diagnostic for a goal no path reaches.
func (t *filetest) unreachable(want goal, refused []string) error {
	what := "the automaton offers no path from its start state to a state that accepts"

	if !want.automaton {
		node, ok := t.nodes[want.predicate]
		if !ok {
			return unresolved(want.predicate)
		}

		values, err := t.literalsOf(want.predicate)
		if err != nil {
			return err
		}

		field := "a field"
		if target, ok := t.nodes[node.GetPredicate().GetFieldId()]; ok {
			field = originalOf(target)
		}

		what = fmt.Sprintf("no file this layout describes reaches the predicate the descriptor carries as node %d, which selects a record on %s holding %q",
			want.predicate, field, string(values[want.pick]))
	}

	rule := "every transition of the automaton is one some file takes: a predicate no path reaches selects a record no file can hold, which is a bug in whatever produced the descriptor rather than a gap the generated tests may be written around"
	if len(refused) != 0 {
		rule += "\nthe paths tried, and what each could not satisfy:\n- " + strings.Join(refused, "\n- ")
	}

	return malformed(what, rule)
}

// step is one record of a synthesized file: which state it was admitted from,
// which of that state's transitions took it, and which literal of a
// set-membership predicate its discriminated field spells.
type step struct {
	from int
	at   int
	pick int
}

// paths is every candidate file for one goal, in the order they are preferred.
//
// A candidate is three pieces: the shortest walk from the start state to a state
// offering the transition being covered, that transition, and the shortest walk
// from where it lands to a state that accepts. Between the second and the third
// the same transition is taken again where the state it lands in offers one
// carrying the same predicate — see [run] for why twice rather than once, and
// why the singular is tried after it rather than instead of it.
func (t *filetest) paths(want goal) ([][]step, error) {
	// Two lists, because they answer different questions. preferred is the file
	// this generator means to write for each edge that reaches the goal — the run
	// walked [runLength] times where the automaton offers it again — and shortened
	// is the same edge walked once, which is what is left where the longer file
	// cannot be laid out. Every preferred candidate is tried before any shortened
	// one, so a file does not lose its second record to a state that happens to be
	// carried first.
	var preferred, shortened [][]step

	for from, walk := range t.walks {
		for at, one := range walk {
			// The goal a descriptor whose transitions carry no predicate has is
			// the automaton itself, so every edge reaches it and the first
			// candidate is the shortest file the automaton admits.
			if !want.automaton &&
				(one.node.PredicateId == nil || one.node.GetPredicateId() != want.predicate) {
				continue
			}

			prefix, reached := t.shortestTo(from)
			if !reached {
				continue
			}

			core := append(append([]step(nil), prefix...), step{from: from, at: at, pick: want.pick})

			extended, landed := t.extend(core, one.next, want.pick)

			// The run walked once is the preferred file only where the automaton
			// offers no second walk of it; where it does, the singular form is
			// what is left when the longer one cannot be laid out.
			whole := &preferred
			if len(extended) > len(core) {
				whole = &shortened

				if suffix, accepts := t.shortestAccepting(landed); accepts {
					preferred = append(preferred, append(extended, suffix...))
				}
			}

			if suffix, accepts := t.shortestAccepting(one.next); accepts {
				*whole = append(*whole, append(append([]step(nil), core...), suffix...))
			}
		}
	}

	// Shortest first inside each list, not only inside one candidate: each piece
	// of a candidate is a shortest walk, but a goal two states offer would
	// otherwise be answered by whichever state the descriptor happens to carry
	// first. Stable, so that two candidates of one length stay in the order the
	// automaton carries them and a run is a run two of them make alike.
	sort.SliceStable(preferred, func(i, j int) bool { return len(preferred[i]) < len(preferred[j]) })
	sort.SliceStable(shortened, func(i, j int) bool { return len(shortened[i]) < len(shortened[j]) })

	return append(preferred, shortened...), nil
}

// extend walks the run out to [runLength] records where the automaton offers the same
// edge again, and is the walk itself and the state it lands in.
func (t *filetest) extend(core []step, from, pick int) ([]step, int) {
	out := append([]step(nil), core...)

	for range runLength - 1 {
		again, offered := t.repeat(from, t.walks[out[len(out)-1].from][out[len(out)-1].at])
		if !offered {
			break
		}

		out = append(out, step{from: from, at: again, pick: pick})
		from = t.walks[from][again].next
	}

	return out, from
}

// repeat is the transition of a state that walks the run one further, and
// whether the state offers one at all.
//
// The same predicate rather than the same record type or the same transition
// node: a run is a record type selected on the same bytes twice, and the edge
// carrying it a second time leaves a different state and is a different node.
// Where the transition carries no predicate there is nothing to match on but the
// record it admits, which is the shape a file told apart by order alone has.
func (t *filetest) repeat(from int, taken transition) (int, bool) {
	for at, one := range t.walks[from] {
		if taken.node.PredicateId == nil {
			if one.node.PredicateId == nil && one.node.GetRecordId() == taken.node.GetRecordId() {
				return at, true
			}

			continue
		}

		if one.node.PredicateId != nil && one.node.GetPredicateId() == taken.node.GetPredicateId() {
			return at, true
		}
	}

	return 0, false
}

// shortestTo is the shortest walk from the start state to the state at target,
// and whether one exists at all.
func (t *filetest) shortestTo(target int) ([]step, bool) {
	start := t.index[t.file.GetStartStateId()]
	if start == target {
		return nil, true
	}

	return t.search(start, func(at int) bool { return at == target })
}

// shortestAccepting is the shortest walk from a state to one that accepts.
func (t *filetest) shortestAccepting(from int) ([]step, bool) {
	if t.states[from].GetState().GetAccepts() {
		return nil, true
	}

	return t.search(from, func(at int) bool { return t.states[at].GetState().GetAccepts() })
}

// search is a breadth-first walk from a state to the first one the predicate
// admits, in the order the automaton carries its transitions.
//
// Breadth first, so the walk it finds is the shortest; in transition order, so
// that two runs over one descriptor find the same one. The state a walk lands in
// is visited once — a shortest walk never returns to a state it has left.
func (t *filetest) search(from int, admits func(int) bool) ([]step, bool) {
	type entry struct {
		at   int
		walk []step
	}

	var (
		seen  = map[int]struct{}{from: {}}
		queue = []entry{{at: from}}
	)

	for len(queue) > 0 {
		head := queue[0]
		queue = queue[1:]

		for at, one := range t.walks[head.at] {
			if _, been := seen[one.next]; been {
				continue
			}

			walk := append(append([]step(nil), head.walk...), step{from: head.at, at: at})
			if admits(one.next) {
				return walk, true
			}

			seen[one.next] = struct{}{}
			queue = append(queue, entry{at: one.next, walk: walk})
		}
	}

	return nil, false
}

// held is what a register holds at a point along a path: the binding that last
// wrote it, and how many decrements have been applied since.
//
// Symbolic rather than a number, because the binding that wrote it read a field
// of a record this generator has not laid out yet — and what that field holds is
// exactly what the guards further along the path decide. See [filetest.solve].
type heldValue struct {
	// site is the binding that wrote it, as an index into the path's sites, and
	// is -1 for a register nothing has bound.
	site int

	// down is how many decrements have been applied since.
	down int
}

// site is one write of a register out of a field of the record a transition
// admitted, along one path.
type site struct {
	// step is where in the path the binding happened.
	step int

	register uint64
	field    uint64

	// integer is whether the register holds a number rather than bytes.
	integer bool

	// pinned is whether the field's bytes are already fixed by the predicate
	// that admitted the record, so this binding chooses nothing. Where it is,
	// number and body are what the field holds.
	pinned bool
	number int64
	body   []byte

	// chosen is whether anything along the path constrained the value, and
	// number and body are then what was chosen for it.
	chosen bool
}

// need is one thing a path requires of the value a binding put in a register.
//
// Every guard the path evaluates is one of these, and so is every table a record
// of the path carries whose count is a register: the reader sizes such a table
// from the register before it decodes the record, so a register outside the
// bounds the copybook declared is a record the reader refuses.
type need struct {
	// site is the binding whose value is constrained, and down how many
	// decrements stand between that binding and the point the need is felt.
	site int
	down int

	// numbers and values are the values the register may hold, either of which
	// is empty where the need is a bound rather than a set.
	numbers []int64
	values  [][]byte

	// atLeast and atMost bound an integer register, and bounded says whether
	// they were set at all. table says the bound came from a table's declared
	// occurrences, which is what makes an empty one worth one occurrence rather
	// than none.
	atLeast int64
	atMost  int64
	bounded bool
	table   bool

	// why names the guard or the table, for the diagnostic a path that cannot
	// satisfy it produces.
	why string
}

// laid is one case: the path it walks, the goals it meets, the file's bytes and
// what each record of it asserts.
type laid struct {
	path  []step
	meets []goal

	// runs is the whole file divided by what wrote each part of it — the
	// framing as well as the items — in the order the bytes stand in.
	runs []chunk

	// records is one entry per record of the file.
	records []laidRecord

	// needs are the import paths this case's assertions asked for beyond the
	// ones every case takes.
	needs []string
}

// laidRecord is one record of a synthesized file.
type laidRecord struct {
	// typ is its Go type and original the name the copybook gave it.
	typ      string
	original string

	// raw is its own bytes, without the framing around them.
	raw []byte

	// checks are the assertions the case makes about it, which is the field its
	// transition's predicate names and nothing else.
	checks []string
}

// lay is one candidate path solved and laid out, the reason it was refused, or
// the diagnostic that stops the run.
//
// Three passes, and they are in this order because each needs the one before it.
// The needs are structural, so they are collected first, over symbols rather
// than numbers. Solving them fixes what every binding has to put in its
// register, which is what the layout then writes into the fields. And the walk
// the generated reader will actually make can only be checked once the bytes
// exist, because what excludes an earlier transition of a state may be its guard
// or may be the bytes in front of it.
func (t *filetest) lay(path []step) (*laid, string, error) {
	sites, needs, why, err := t.demands(path)
	if err != nil || why != "" {
		return nil, why, err
	}

	if why, err := t.solve(sites, needs); err != nil || why != "" {
		return nil, why, err
	}

	one, err := t.write(path, sites)
	if err != nil {
		return nil, "", err
	}

	if why, err := t.walked(path, sites, one); err != nil || why != "" {
		return nil, why, err
	}

	one.meets = t.met(path)

	return one, "", nil
}

// met is every goal a path passes through, which is what makes one case cover
// more than the goal it was built for.
func (t *filetest) met(path []step) []goal {
	var out []goal

	for _, at := range path {
		one := t.walks[at.from][at.at]

		if one.node.PredicateId == nil {
			out = append(out, goal{automaton: true})

			continue
		}

		out = append(out, goal{predicate: one.node.GetPredicateId(), pick: at.pick})
	}

	return out
}

// demands is the path's binding sites and everything the path needs of them.
//
// Named for what it collects rather than `collect`, which is [filer.collect] —
// indexing a descriptor's nodes — and would be shadowed by a method of this type.
func (t *filetest) demands(path []step) ([]site, []need, string, error) {
	var (
		sites []site
		needs []need
		env   = make(map[uint64]heldValue)
	)

	for at, one := range path {
		taken := t.walks[one.from][one.at]

		// The guards first: a transition's are evaluated on entry, against the
		// register file as the record before it left it.
		for _, id := range taken.node.GetGuardIds() {
			want, why, err := t.needOf(id, env)
			if err != nil || why != "" {
				return nil, nil, why, err
			}

			needs = append(needs, want)
		}

		// Then the tables of the record it admits whose count is a register,
		// which the reader sizes before it decodes a byte of the record.
		counts, err := t.registerCounts(taken.record)
		if err != nil {
			return nil, nil, "", err
		}

		for _, count := range counts {
			held, bound := env[count.register]
			if !bound || held.site < 0 {
				return nil, nil, fmt.Sprintf("%s is counted by the register the descriptor carries as node %d, and nothing along this path has bound it",
					count.item, count.register), nil
			}

			needs = append(needs, need{
				site: held.site, down: held.down,
				atLeast: int64(count.minimum), atMost: int64(count.maximum), bounded: true, table: true,
				why: fmt.Sprintf("%s occurs %d to %d times", count.item, count.minimum, count.maximum),
			})
		}

		// And the bindings last, which is where the reader applies them: after
		// the record has been admitted and read.
		for _, id := range taken.node.GetBindingIds() {
			why, err := t.bind(id, at, one.pick, taken, env, &sites)
			if err != nil || why != "" {
				return nil, nil, why, err
			}
		}
	}

	last := t.walks[path[len(path)-1].from][path[len(path)-1].at].next

	for _, id := range t.states[last].GetState().GetAcceptanceGuardIds() {
		want, why, err := t.needOf(id, env)
		if err != nil || why != "" {
			return nil, nil, why, err
		}

		needs = append(needs, want)
	}

	return sites, needs, "", nil
}

// bind records one binding of a transition, and what the register holds after
// it.
func (t *filetest) bind(id uint64, at, pick int, taken transition, env map[uint64]heldValue, sites *[]site) (string, error) {
	node, ok := t.nodes[id]
	if !ok {
		return "", unresolved(id)
	}

	binding := node.GetBinding()
	if binding == nil {
		return "", malformed(fmt.Sprintf("node %d is a transition's binding and is not a binding node", id),
			"a transition's binding list names binding nodes; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
	}

	kind, err := t.registerKind(binding.GetRegisterId())
	if err != nil {
		return "", err
	}

	switch value := binding.GetValue().(type) {
	case *irpb.Binding_FieldId:
		one := site{
			step:     at,
			register: binding.GetRegisterId(), field: value.FieldId, integer: kind == "int64",
		}

		if err := t.pin(&one, taken, pick); err != nil {
			return "", err
		}

		env[binding.GetRegisterId()] = heldValue{site: len(*sites)}
		*sites = append(*sites, one)
	case *irpb.Binding_Decrement:
		if kind != "int64" {
			return "", malformed(fmt.Sprintf("a binding takes one off node %d, which holds bytes", binding.GetRegisterId()),
				"the decrement member writes an integer register; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
		}

		held, bound := env[binding.GetRegisterId()]
		if !bound || held.site < 0 {
			return fmt.Sprintf("the register the descriptor carries as node %d is decremented and nothing along this path has bound it",
				binding.GetRegisterId()), nil
		}

		held.down++
		env[binding.GetRegisterId()] = held
	default:
		return "", malformed("a binding writes a register and says nothing about what with",
			"a binding names the value written: a field of the record the transition admits, or the register's own value less one")
	}

	return "", nil
}

// pin settles whether a binding chooses anything: a field the predicate that
// admitted the record already fixes holds what the predicate said, and this
// binding reads it rather than deciding it.
func (t *filetest) pin(one *site, taken transition, pick int) error {
	if taken.node.PredicateId == nil {
		return nil
	}

	node, ok := t.nodes[taken.node.GetPredicateId()]
	if !ok {
		return unresolved(taken.node.GetPredicateId())
	}

	if node.GetPredicate().GetFieldId() != one.field {
		return nil
	}

	values, err := t.literalsOf(taken.node.GetPredicateId())
	if err != nil {
		return err
	}

	source, ok := t.nodes[one.field]
	if !ok {
		return unresolved(one.field)
	}

	if pick < 0 || pick >= len(values) {
		pick = 0
	}

	one.pinned, one.body = true, values[pick]

	if !one.integer {
		return nil
	}

	value, err := t.synth.readBack(source.GetField(), one.body)
	if err != nil {
		return err
	}

	number, numeric := integral(value)
	if !numeric {
		return malformed(fmt.Sprintf("%s is bound into an integer register and a predicate pins it to bytes that are not a number",
			originalOf(source)),
			"a binding writing an integer register names a numeric field; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
	}

	one.number = int64(number)

	return nil
}

// needOf is what one guard requires of the register it reads.
func (t *filetest) needOf(id uint64, env map[uint64]heldValue) (need, string, error) {
	node, ok := t.nodes[id]
	if !ok {
		return need{}, "", unresolved(id)
	}

	guard := node.GetGuard()
	if guard == nil {
		return need{}, "", malformed(fmt.Sprintf("node %d guards a transition or a state and is not a guard node", id),
			"a guard reads a register and decides whether the transition carrying it is eligible; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
	}

	kind, err := t.registerKind(guard.GetRegisterId())
	if err != nil {
		return need{}, "", err
	}

	held, bound := env[guard.GetRegisterId()]
	if !bound || held.site < 0 {
		return need{}, fmt.Sprintf("the guard the descriptor carries as node %d reads the register it carries as node %d, and nothing along this path has bound it",
			id, guard.GetRegisterId()), nil
	}

	want := need{
		site: held.site, down: held.down,
		why: fmt.Sprintf("the guard the descriptor carries as node %d", id),
	}

	switch test := guard.GetTest().(type) {
	case *irpb.Guard_Equals:
		if err := t.wants(&want, kind, test.Equals); err != nil {
			return need{}, "", err
		}
	case *irpb.Guard_OneOf:
		values := test.OneOf.GetValues()
		if len(values) == 0 {
			return need{}, "", malformed("a one-of guard carries no literals",
				"a guard tests the register against a set, and a set of nothing excludes every transition carrying it")
		}

		for _, value := range values {
			if err := t.wants(&want, kind, value); err != nil {
				return need{}, "", err
			}
		}
	case *irpb.Guard_GreaterThanZero:
		if kind != "int64" {
			return need{}, "", malformed(fmt.Sprintf("a guard asks whether node %d is greater than zero and it holds bytes", guard.GetRegisterId()),
				"the greater-than-zero test is over an integer register; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
		}

		want.atLeast, want.atMost, want.bounded = 1, math.MaxInt64, true
	default:
		return need{}, "", malformed("a guard carries no test",
			"the set is closed and a guard carries one member of it; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
	}

	return want, "", nil
}

// wants adds one literal a guard tests against to what it needs of the
// register.
func (t *filetest) wants(want *need, kind string, l *irpb.Literal) error {
	switch value := l.GetValue().(type) {
	case *irpb.Literal_BytesValue:
		if kind != "[]byte" {
			return malformed("a guard compares an integer register against a byte string",
				"which member of a literal is set MUST match the kind of the register tested; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
		}

		want.values = append(want.values, value.BytesValue)
	case *irpb.Literal_Integer:
		if kind != "int64" {
			return malformed("a guard compares a bytes register against a number",
				"which member of a literal is set MUST match the kind of the register tested; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
		}

		want.numbers = append(want.numbers, value.Integer)
	default:
		return malformed("a guard carries a literal holding no value",
			"a literal carries the bytes a bytes register is compared against or the number an integer one is")
	}

	return nil
}

// solve fixes what every binding of the path has to put in its register, or says
// which need no value satisfies.
//
// The needs of one binding do not interact with those of another — a register's
// value moves only by being bound again or by being decremented, and both start a
// need of their own — so this is one closed search per binding rather than a
// solver over the path. Each need is either a set the value must be in or a
// bound it must be inside, and both are shifted by the decrements standing
// between the binding and the point the need is felt.
//
// Where nothing constrains a binding at all it is left alone, and the field
// takes the value the record tier's own rule gives it. That is not a gap: a
// register no guard reads and no table is counted by is a register whose value
// nothing along the path can tell apart.
func (t *filetest) solve(sites []site, needs []need) (string, error) {
	for at := range sites {
		one := &sites[at]

		var mine []need

		for _, want := range needs {
			if want.site == at {
				mine = append(mine, want)
			}
		}

		if len(mine) == 0 {
			continue
		}

		if one.integer {
			why, err := t.solveNumber(one, mine)
			if err != nil || why != "" {
				return why, err
			}

			continue
		}

		if why := t.solveBytes(one, mine); why != "" {
			return why, nil
		}
	}

	return "", nil
}

// solveNumber is [filetest.solve] over an integer register.
func (t *filetest) solveNumber(one *site, mine []need) (string, error) {
	var (
		candidates []int64
		narrowed   bool
		lower      = int64(math.MinInt64)
		upper      = int64(math.MaxInt64)
		table      bool
	)

	for _, want := range mine {
		if want.bounded {
			lower = max(lower, shift(want.atLeast, want.down))
			upper = min(upper, shift(want.atMost, want.down))
			table = table || want.table

			continue
		}

		shifted := make([]int64, 0, len(want.numbers))
		for _, value := range want.numbers {
			shifted = append(shifted, shift(value, want.down))
		}

		if !narrowed {
			candidates, narrowed = shifted, true

			continue
		}

		candidates = intersect(candidates, shifted)
	}

	if one.pinned {
		if !fits(one.number, candidates, narrowed, lower, upper) {
			return t.refused(one, mine), nil
		}

		return "", nil
	}

	if narrowed {
		for _, value := range candidates {
			if fits(value, nil, false, lower, upper) {
				one.chosen, one.number = true, value

				return "", nil
			}
		}

		return t.refused(one, mine), nil
	}

	// A table whose declared minimum is zero takes one occurrence rather than
	// none, which is the record tier's own rule: a table nobody lays an
	// occurrence of is a table whose item widths nobody checks.
	value := max(lower, int64(0))
	if table && value == 0 && upper >= 1 {
		value = 1
	}

	if value > upper {
		return t.refused(one, mine), nil
	}

	one.chosen, one.number = true, value

	return "", nil
}

// solveBytes is [filetest.solve] over a register holding bytes.
func (t *filetest) solveBytes(one *site, mine []need) string {
	var (
		candidates [][]byte
		narrowed   bool
	)

	for _, want := range mine {
		if want.down != 0 {
			return fmt.Sprintf("a register holding bytes was decremented before %s read it", want.why)
		}

		if !narrowed {
			candidates, narrowed = want.values, true

			continue
		}

		candidates = intersectBytes(candidates, want.values)
	}

	if !narrowed {
		return ""
	}

	if one.pinned {
		for _, value := range candidates {
			if bytes.Equal(value, one.body) {
				return ""
			}
		}

		return t.refused(one, mine)
	}

	if len(candidates) == 0 {
		return t.refused(one, mine)
	}

	one.chosen, one.body = true, candidates[0]

	return ""
}

// refused names the binding a path could not satisfy and everything that was
// asked of it.
func (t *filetest) refused(one *site, mine []need) string {
	names := make([]string, 0, len(mine))
	for _, want := range mine {
		names = append(names, want.why)
	}

	field := fmt.Sprintf("node %d", one.field)
	if node, ok := t.nodes[one.field]; ok {
		field = originalOf(node)
	}

	held := "no value of " + field + " satisfies"
	if one.pinned {
		held = "a predicate pins " + field + ", and what it holds does not satisfy"
	}

	return fmt.Sprintf("%s %s", held, strings.Join(names, " and "))
}

// shift is a bound moved back over the decrements standing between the binding
// and the point the bound is felt, and it saturates.
//
// A guard asking only that a register be greater than zero has no upper bound at
// all, and the largest int64 moved up by one decrement is a bound smaller than
// every value — which would refuse every path carrying such a guard. That is the
// arithmetic, not the automaton.
func shift(bound int64, down int) int64 {
	switch {
	case bound == math.MaxInt64 || bound == math.MinInt64:
		return bound
	case bound > math.MaxInt64-int64(down):
		return math.MaxInt64
	default:
		return bound + int64(down)
	}
}

// fits is whether one value is in the set a path narrowed to, where it narrowed
// to one, and inside the bounds it collected.
func fits(value int64, candidates []int64, narrowed bool, lower, upper int64) bool {
	if value < lower || value > upper {
		return false
	}

	if !narrowed {
		return true
	}

	return slices.Contains(candidates, value)
}

// intersect is the numbers in both, in the order of the first.
func intersect(a, b []int64) []int64 {
	var out []int64

	for _, value := range a {
		if slices.Contains(b, value) {
			out = append(out, value)
		}
	}

	return out
}

// intersectBytes is [intersect] over byte strings.
func intersectBytes(a, b [][]byte) [][]byte {
	var out [][]byte

	for _, value := range a {
		if slices.ContainsFunc(b, func(other []byte) bool { return bytes.Equal(value, other) }) {
			out = append(out, value)
		}
	}

	return out
}

// write lays the file's bytes out, record by record, with the framing around
// each and the values the solved bindings require in the fields they were read
// out of.
func (t *filetest) write(path []step, sites []site) (*laid, error) {
	one := &laid{path: path}

	// The synthesizer is shared across every candidate and this one may yet be
	// refused, so what it reaches for is taken here and not left on it.
	t.synth.needs = make(map[string]struct{})

	for at, taken := range path {
		edge := t.walks[taken.from][taken.at]

		if err := t.record(one, at, taken, edge, sites); err != nil {
			return nil, err
		}
	}

	one.needs = make([]string, 0, len(t.synth.needs))
	for path := range t.synth.needs {
		one.needs = append(one.needs, path)
	}

	slices.Sort(one.needs)

	return one, nil
}

// record lays out one record of the file and the framing standing around it.
func (t *filetest) record(one *laid, at int, taken step, edge transition, sites []site) error {
	s := t.synth

	s.admitting = edge.node
	s.pick = map[uint64]int{}
	s.pinned = map[uint64][]byte{}
	s.forced = map[uint64]int{}
	s.registers = map[uint64]int{}
	s.only = map[uint64]struct{}{}

	if edge.node.PredicateId != nil {
		node, ok := t.nodes[edge.node.GetPredicateId()]
		if !ok {
			return unresolved(edge.node.GetPredicateId())
		}

		s.pick[edge.node.GetPredicateId()] = taken.pick
		s.only[node.GetPredicate().GetFieldId()] = struct{}{}
	}

	for _, bound := range sites {
		if bound.step != at || bound.pinned || !bound.chosen {
			continue
		}

		if bound.integer {
			s.forced[bound.field] = int(bound.number)

			continue
		}

		s.pinned[bound.field] = bound.body
	}

	all, err := t.reachedWith(one.path[:at], sites)
	if err != nil {
		return err
	}

	for _, reached := range all {
		if reached.integer {
			s.registers[reached.register] = int(reached.value)
		}
	}

	node, ok := t.nodes[edge.node.GetRecordId()]
	if !ok {
		return unresolved(edge.node.GetRecordId())
	}

	if err := s.layOut(node, t.holder(at), map[uint64]int{}); err != nil {
		return err
	}

	raw := append([]byte(nil), s.out.Bytes()...)

	parts := make([]chunk, 0, len(s.runs))
	for _, part := range s.runs {
		parts = append(parts, chunk{body: part.body, note: fmt.Sprintf("record %d: %s", at+1, part.note)})
	}

	framed, err := t.framed(at, parts)
	if err != nil {
		return err
	}

	one.runs = append(one.runs, framed...)

	one.records = append(one.records, laidRecord{
		typ: edge.typ, original: edge.record.GetNames().GetOriginal(),
		raw: raw, checks: append([]string(nil), s.checks...),
	})

	return nil
}

// holder is the identifier one record of a case is decoded into.
func (t *filetest) holder(at int) string { return "record" + strconv.Itoa(at+1) }

// reachedRegister is a register and what it holds at a point along a path.
//
// A register holding bytes is never decremented, so it holds what the binding
// put there; an integer one holds that less the decrements since.
type reachedRegister struct {
	register uint64
	value    int64
	body     []byte
	integer  bool
}

// reachedWith is what every register holds after the walk given, which is what
// sizes a table the next record counts by one and what decides whether an
// earlier transition of a state would have been eligible.
//
// Recomputed from the path rather than carried, because the value a binding put
// in a register is decided by [filetest.solve] and [filetest.demands], which
// collected the needs, ran before that. It is one forward walk in step order,
// which is [filetest.demands]' own shape and the reader's: a register nothing has
// bound is absent, a binding replaces what was there and resets the decrements
// with it, and a decrement takes one off what the *latest* binding put there. Two
// passes counting decrements against the earliest binding would disagree with the
// solver about any register a path binds twice.
//
// A binding nothing constrained is left out rather than guessed at. Nothing along
// the path reads it, so nothing needs a number for it — and [filetest.eligible]
// is written to treat an absent register as one it cannot rule a transition out
// on.
func (t *filetest) reachedWith(walk []step, sites []site) ([]reachedRegister, error) {
	var (
		held = make(map[uint64]reachedRegister)
		open = make(map[uint64]struct{})
	)

	for at, taken := range walk {
		for _, id := range t.walks[taken.from][taken.at].node.GetBindingIds() {
			node, ok := t.nodes[id]
			if !ok {
				return nil, unresolved(id)
			}

			binding := node.GetBinding()
			if binding == nil {
				return nil, malformed(fmt.Sprintf("node %d is a transition's binding and is not a binding node", id),
					"a transition's binding list names binding nodes; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
			}

			register := binding.GetRegisterId()

			if _, takes := binding.GetValue().(*irpb.Binding_Decrement); takes {
				if one, bound := held[register]; bound {
					one.value--
					held[register] = one
				}

				continue
			}

			delete(held, register)
			delete(open, register)

			bound, ok := siteAt(sites, at, register)
			if !ok || (!bound.pinned && !bound.chosen) {
				open[register] = struct{}{}

				continue
			}

			held[register] = reachedRegister{
				register: register, value: bound.number, body: bound.body, integer: bound.integer,
			}
		}
	}

	out := make([]reachedRegister, 0, len(held))

	for _, id := range sortedKeys(held) {
		out = append(out, held[id])
	}

	return out, nil
}

// siteAt is the binding of one register made at one step of the path.
func siteAt(sites []site, at int, register uint64) (site, bool) {
	for _, one := range sites {
		if one.step == at && one.register == register {
			return one, true
		}
	}

	return site{}, false
}

// framed is one record's runs with this file's framing around them, at the same
// points [filer.emitEmit] emits it.
//
// The same arithmetic on both sides and deliberately so: a case asserts that the
// file writes back the bytes it was read from, and a descriptor word this file
// computed differently from the one the writer emits would fail that assertion
// for a reason that has nothing to do with the walk.
//
// The framing stands in the literal rather than being elided, because it is half
// of what an adopter is holding the literal against: a record descriptor word is
// four bytes they will see in a hexdump and want to recognise.
func (t *filetest) framed(at int, parts []chunk) ([]chunk, error) {
	width := 0
	for _, part := range parts {
		width += len(part.body)
	}

	switch t.how {
	case descriptorWord:
		stated := width + segmentDescriptorWidth

		// The emitted writer refuses a record whose stated length will not fit
		// the two bytes a record descriptor word carries, and so does this. A
		// truncated word here would be a literal that fails its own byte
		// equality inside generated code somebody else has to debug, which is a
		// long way from the descriptor that caused it.
		if stated > 0xFFFF {
			return nil, malformed(fmt.Sprintf("record %d of a synthesized file is %s and this file states a record's length in a record descriptor word",
				at+1, plural(width, "byte")),
				"a record descriptor word states a length in two bytes, so a record it stands in front of is at most 65531 bytes")
		}

		return append([]chunk{{
			body: []byte{byte(stated >> 8), byte(stated), 0, 0},
			note: fmt.Sprintf("record %d: the record descriptor word — %s, itself included", at+1, plural(stated, "byte")),
		}}, parts...), nil
	case segmented:
		return t.segmented(at, parts, width), nil
	case delimited:
		return t.delimited(at, parts), nil
	default:
		return parts, nil
	}
}

// delimited is one record with the delimiter this file carries where the
// placement puts it.
func (t *filetest) delimited(at int, parts []chunk) []chunk {
	if t.placement == irpb.DelimiterPlacement_DELIMITER_PLACEMENT_SEPARATOR {
		if at == 0 {
			return parts
		}

		return append([]chunk{{
			body: append([]byte(nil), t.delimiter...),
			note: fmt.Sprintf("the delimiter standing between record %d and record %d", at, at+1),
		}}, parts...)
	}

	// A terminator follows every record, the last included, and an optional
	// terminator is emitted rather than chosen about — see [filer.emitEmit].
	return append(parts, chunk{
		body: append([]byte(nil), t.delimiter...),
		note: fmt.Sprintf("the delimiter behind record %d", at+1),
	})
}

// segmented is one record cut into as few segments as this file node's largest
// segment allows, each behind the segment descriptor word describing it.
//
// The only framing that stands *inside* a record rather than around it, so the
// record's runs are cut at the segment boundaries and a run the cut falls inside
// is split. The half behind the boundary keeps the item's note and says it is a
// continuation, because a reader counting bytes against a hexdump needs both
// halves labelled.
func (t *filetest) segmented(at int, parts []chunk, width int) []chunk {
	most := int(t.maxSegment) - segmentDescriptorWidth

	var (
		out  []chunk
		from int
		n    int
	)

	for {
		size := min(width-from, most)

		code := byte(0x03)

		switch {
		case from == 0 && size == width:
			code = 0x00
		case from == 0:
			code = 0x01
		case from+size == width:
			code = 0x02
		}

		stated := size + segmentDescriptorWidth

		out = append(out, chunk{
			body: []byte{byte(stated >> 8), byte(stated), code, 0},
			note: fmt.Sprintf("record %d: segment %d's descriptor word — %s, itself included", at+1, n+1, plural(stated, "byte")),
		})
		out = append(out, cut(parts, from, from+size)...)

		from += size
		n++

		if from >= width {
			return out
		}
	}
}

// cut is the runs covering the bytes between from and to, with a run the
// boundary falls inside split in two.
func cut(parts []chunk, from, to int) []chunk {
	var (
		out []chunk
		at  int
	)

	for _, part := range parts {
		end := at + len(part.body)

		if end <= from || at >= to {
			at = end

			continue
		}

		note := part.note
		if at < from {
			note += " (continued)"
		}

		out = append(out, chunk{body: part.body[max(from, at)-at : min(to, end)-at], note: note})

		at = end
	}

	return out
}

// walked checks that the generated reader takes the path this case was built
// for, and says which record it would take a different edge at where it does
// not.
//
// Both directions evaluate the transitions of a state in the order the state
// carries them and take the first that is eligible, so a path is only a path if
// every earlier transition of every state along it is excluded — by a guard that
// does not hold, or by a predicate the record's own bytes do not satisfy. That is
// a fact about the bytes as well as about the registers, which is why it is
// checked here rather than while the path was being chosen.
func (t *filetest) walked(path []step, sites []site, one *laid) (string, error) {
	for at, taken := range path {
		for earlier := range taken.at {
			held, err := t.eligible(t.walks[taken.from][earlier], path[:at], sites, one.records[at].raw)
			if err != nil {
				return "", err
			}

			if held {
				return fmt.Sprintf("the reader takes transition %d of the state the descriptor carries as node %d for record %d, not transition %d",
					earlier+1, t.states[taken.from].GetId(), at+1, taken.at+1), nil
			}
		}
	}

	return "", nil
}

// eligible is whether one transition would take the record in front of it.
func (t *filetest) eligible(edge transition, walk []step, sites []site, raw []byte) (bool, error) {
	all, err := t.reachedWith(walk, sites)
	if err != nil {
		return false, err
	}

	reached := make(map[uint64]reachedRegister)
	for _, one := range all {
		reached[one.register] = one
	}

	for _, id := range edge.node.GetGuardIds() {
		node, ok := t.nodes[id]
		if !ok {
			return false, unresolved(id)
		}

		guard := node.GetGuard()
		if guard == nil {
			return false, malformed(fmt.Sprintf("node %d guards a transition or a state and is not a guard node", id),
				"a guard reads a register and decides whether the transition carrying it is eligible; see docs/ir/SPEC.md, \"The automaton remembers, in registers\"")
		}

		// A register whose value nothing along the path fixed cannot be
		// evaluated here, and a transition that might be eligible is treated as
		// one that is. The path is refused rather than laid out on a guess,
		// which costs a case that could have been shorter and never costs a case
		// that cannot pass.
		held, bound := reached[guard.GetRegisterId()]
		if !bound {
			return true, nil
		}

		holds, known := guardHolds(guard, held)
		if !known {
			return true, nil
		}

		if !holds {
			return false, nil
		}
	}

	return t.matches(edge, raw)
}

// guardHolds is whether a register's value satisfies a guard, and whether that
// could be decided here at all.
func guardHolds(guard *irpb.Guard, held reachedRegister) (holds, known bool) {
	switch test := guard.GetTest().(type) {
	case *irpb.Guard_Equals:
		return matchesLiteral(test.Equals, held)
	case *irpb.Guard_OneOf:
		for _, one := range test.OneOf.GetValues() {
			holds, known := matchesLiteral(one, held)
			if !known {
				return false, false
			}

			if holds {
				return true, true
			}
		}

		return false, true
	case *irpb.Guard_GreaterThanZero:
		return held.integer && held.value > 0, held.integer
	default:
		return false, false
	}
}

// matchesLiteral is whether a register holds one carried value, and whether the
// two are even comparable.
func matchesLiteral(l *irpb.Literal, held reachedRegister) (holds, known bool) {
	switch value := l.GetValue().(type) {
	case *irpb.Literal_Integer:
		return held.integer && value.Integer == held.value, held.integer
	case *irpb.Literal_BytesValue:
		return !held.integer && bytes.Equal(value.BytesValue, held.body), !held.integer
	default:
		return false, false
	}
}

// matches is whether a transition's predicate admits the bytes in front of it,
// which is [filer.predicate]'s test made here rather than emitted.
func (t *filetest) matches(edge transition, raw []byte) (bool, error) {
	if edge.node.PredicateId == nil {
		return true, nil
	}

	values, err := t.literalsOf(edge.node.GetPredicateId())
	if err != nil {
		return false, err
	}

	// A target that is not wholly inside the bytes it is handed does not match,
	// which is the emitted matcher's own first line.
	if len(raw) < edge.reads {
		return false, nil
	}

	for _, value := range values {
		if edge.reads < len(value) {
			continue
		}

		if bytes.Equal(raw[edge.reads-len(value):edge.reads], value) {
			return true, nil
		}
	}

	return false, nil
}

// testCase is one case, written.
func (t *filetest) testCase(one *laid, alias string, used map[string]struct{}) (string, error) {
	name := unique("Test"+t.name(one), used)

	doc, err := t.doc(name, one)
	if err != nil {
		return "", err
	}

	var b strings.Builder

	b.WriteString(commentLines(doc))
	fmt.Fprintf(&b, "func %s(t *testing.T) {\nt.Parallel()\n\n", name)
	b.WriteString(t.synth.literalOf(one.runs))
	fmt.Fprintf(&b, "\nr, err := %s.%s(bytes.NewReader(in), %s.%s())\n", alias, newReaderFunc, alias, encodingFunc)
	fmt.Fprintf(&b, "if err != nil {\nt.Fatalf(\"%s: %%v\", err)\n}\n\n", newReaderFunc)
	fmt.Fprintf(&b, "var records []%s.%s\n\n", alias, recordInterface)
	b.WriteString("for {\nrec, err := r.Next()\nif errors.Is(err, io.EOF) {\nbreak\n}\n\n")
	b.WriteString("if err != nil {\nt.Fatalf(\"Next: %v\", err)\n}\n\n")
	b.WriteString("records = append(records, rec)\n}\n\n")
	fmt.Fprintf(&b, "if len(records) != %d {\nt.Fatalf(%q, len(records))\n}\n",
		len(one.records),
		fmt.Sprintf("the file holds %s and the reader produced %%d", plural(len(one.records), "record")))

	for at, record := range one.records {
		b.WriteString("\n")

		want := fmt.Sprintf("*%s.%s", alias, record.typ)
		failed := fmt.Sprintf("record %d is %s and came back as a %%T, want a %s", at+1, record.original, want)

		if len(record.checks) == 0 {
			fmt.Fprintf(&b, "if _, ok := records[%d].(%s); !ok {\nt.Fatalf(%q, records[%d])\n}\n",
				at, want, failed, at)

			continue
		}

		fmt.Fprintf(&b, "%s, ok := records[%d].(%s)\nif !ok {\nt.Fatalf(%q, records[%d])\n}\n",
			t.holder(at), at, want, failed, at)

		for _, check := range record.checks {
			b.WriteString("\n")
			b.WriteString(check)
			b.WriteString("\n")
		}
	}

	b.WriteString("\nvar out bytes.Buffer\n\n")
	fmt.Fprintf(&b, "w, err := %s.%s(&out, %s.%s())\n", alias, newWriterFunc, alias, encodingFunc)
	fmt.Fprintf(&b, "if err != nil {\nt.Fatalf(\"%s: %%v\", err)\n}\n\n", newWriterFunc)
	b.WriteString("for _, rec := range records {\nif err := w.Write(rec); err != nil {\nt.Fatalf(\"Write: %v\", err)\n}\n}\n\n")
	b.WriteString("if err := w.Close(); err != nil {\nt.Fatalf(\"Close: %v\", err)\n}\n\n")
	b.WriteString("if !bytes.Equal(out.Bytes(), in) {\n")
	b.WriteString("t.Errorf(\"the file does not write back the bytes it was read from\\n got: % x\\nwant: % x\", out.Bytes(), in)\n")
	b.WriteString("}\n}")

	return b.String(), nil
}

// doc is a case's comment: what file it holds, and which of the automaton's
// discriminators it walks.
func (t *filetest) doc(name string, one *laid) (string, error) {
	records := make([]string, 0, len(one.records))
	for _, record := range one.records {
		records = append(records, record.original)
	}

	doc := wrapped(fmt.Sprintf("%s is a file of %s: the bytes below, the records they read back as, and the same bytes written back.",
		name, joinNames(collapse(records))))

	var covered []string

	for _, met := range one.meets {
		if met.automaton {
			continue
		}

		node, ok := t.nodes[met.predicate]
		if !ok {
			return "", unresolved(met.predicate)
		}

		// Reported rather than dropped from the comment. Both of these are
		// invariants that held once already — [filetest.goals] resolved this
		// predicate and counted its literals — so either failing here means the
		// path and the goal disagree about which literal the case covers, which
		// is exactly the gap the coverage rule exists to catch. A comment that
		// quietly grew shorter is the one outcome that would hide it.
		values, err := t.literalsOf(met.predicate)
		if err != nil {
			return "", err
		}

		if met.pick < 0 || met.pick >= len(values) {
			return "", malformed(fmt.Sprintf("a case covers literal %d of the predicate the descriptor carries as node %d, which carries %s",
				met.pick, met.predicate, plural(len(values), "literal")),
				"a case covers one literal of the predicate that admits its record, and the set it is drawn from is the set that predicate carries")
		}

		field := fmt.Sprintf("node %d", node.GetPredicate().GetFieldId())
		if target, ok := t.nodes[node.GetPredicate().GetFieldId()]; ok {
			field = originalOf(target)
		}

		phrase := fmt.Sprintf("%s holding %q", field, string(values[met.pick]))
		if !slices.Contains(covered, phrase) {
			covered = append(covered, phrase)
		}
	}

	if len(covered) != 0 {
		doc += "\n\n" + wrapped("The discriminators it walks: "+joinNames(covered)+".")
	}

	return doc, nil
}

// name is what a case is called, which is the file it holds spelled as a
// sentence.
//
// Long, and that is the trade: the name is the first thing a reader sees in a
// failure and the only summary of the path the case walks, and a numbered one
// would be short and say nothing. Runs of one record type are collapsed, so that
// a counted run reads as the run it is.
func (t *filetest) name(one *laid) string {
	types := make([]string, 0, len(one.records))
	for _, record := range one.records {
		types = append(types, record.typ)
	}

	var parts []string

	for _, one := range runsOf(types) {
		if one.n == 1 {
			parts = append(parts, article(one.what)+one.what)

			continue
		}

		parts = append(parts, cardinal(one.n)+one.what+"s")
	}

	return strings.Join(parts, "Then")
}

// stretch is a run of one thing repeated.
type stretch struct {
	what string
	n    int
}

// runsOf is a list collapsed into its runs, in order.
func runsOf(all []string) []stretch {
	var out []stretch

	for _, one := range all {
		if len(out) != 0 && out[len(out)-1].what == one {
			out[len(out)-1].n++

			continue
		}

		out = append(out, stretch{what: one, n: 1})
	}

	return out
}

// collapse is a list of names with its runs spelled as runs, for a sentence.
func collapse(all []string) []string {
	out := make([]string, 0, len(all))

	for _, one := range runsOf(all) {
		if one.n == 1 {
			out = append(out, one.what)

			continue
		}

		out = append(out, strings.ToLower(cardinal(one.n))+" "+one.what+" records")
	}

	return out
}

// article is A or An, by whether the word opens on a vowel.
func article(word string) string {
	if word == "" {
		return "A"
	}

	if strings.ContainsRune("AEIOUaeiou", rune(word[0])) {
		return "An"
	}

	return "A"
}

// cardinals are the numbers a case's name spells as words.
//
// As far as twelve, which is where English stops being shorter than the digits.
// Beyond it the digits stand, which is legal inside an identifier and is what a
// reader would rather see anyway.
var cardinals = []string{
	"Zero", "One", "Two", "Three", "Four", "Five", "Six",
	"Seven", "Eight", "Nine", "Ten", "Eleven", "Twelve",
}

// cardinal is a count as a case's name spells it.
func cardinal(n int) string {
	if n >= 0 && n < len(cardinals) {
		return cardinals[n]
	}

	return strconv.Itoa(n)
}
